package focus

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/iliafrenkel/on-suite/internal/platform/db"
)

// MinFocusSeconds is the least focus a session needs to be recorded (spec:
// "What gets recorded"). The browser doesn't send shorter ones; the server
// refuses them anyway, and the schema's CHECK backs it up.
const MinFocusSeconds = 60

const (
	// maxClientIDLen bounds the browser's random session id: a UUID is 36.
	maxClientIDLen = 64
	// clockSkew is how far in the future started_at and ended_at may be before it is
	// refused: the browser's clock and the server's can disagree a little
	// (F3 plan).
	clockSkew = 5 * time.Minute
	// fallbackColor is what an unknown colour is stored as (spec:
	// "Recording endpoint").
	fallbackColor = "gray"
)

// SessionInput is one ended session as the running page reports it
// (spec: "Recording endpoint"). TimerID 0 means none.
type SessionInput struct {
	ClientID     string
	TimerID      int64
	TimerName    string
	Color        string
	StartedAt    time.Time
	EndedAt      time.Time
	FocusSeconds int
	RoundsDone   int
	Completed    bool
}

// Session is one recorded session. TimerID is 0 once its timer is deleted;
// TimerName and Color are the copies taken when it started.
type Session struct {
	ID, UserID   int64
	TimerID      int64
	TimerName    string
	Color        string
	ClientID     string
	StartedAt    time.Time
	EndedAt      time.Time
	FocusSeconds int
	RoundsDone   int
	Completed    bool
}

// sessionColumns is every column scanSession reads, in order.
const sessionColumns = `id, user_id, timer_id, timer_name, color, client_id, started_at,
	ended_at, focus_seconds, rounds_done, completed`

func scanSession(row rowScanner) (Session, error) {
	var s Session
	var timerID sql.NullInt64
	var started, ended string
	err := row.Scan(&s.ID, &s.UserID, &timerID, &s.TimerName, &s.Color, &s.ClientID,
		&started, &ended, &s.FocusSeconds, &s.RoundsDone, &s.Completed)
	if errors.Is(err, sql.ErrNoRows) {
		return Session{}, ErrNotFound
	}
	if err != nil {
		return Session{}, fmt.Errorf("focus: load session: %w", err)
	}
	s.TimerID = timerID.Int64
	if s.StartedAt, err = db.ParseTime(started); err != nil {
		return Session{}, fmt.Errorf("focus: session started_at: %w", err)
	}
	if s.EndedAt, err = db.ParseTime(ended); err != nil {
		return Session{}, fmt.Errorf("focus: session ended_at: %w", err)
	}
	return s, nil
}

// checkSession tidies what can be tidied (name, colour) and refuses what
// can't be true (spec: "Recording endpoint").
func checkSession(in SessionInput, now time.Time) (SessionInput, error) {
	in.ClientID = strings.TrimSpace(in.ClientID)
	in.TimerName = strings.TrimSpace(in.TimerName)
	if r := []rune(in.TimerName); len(r) > MaxNameRunes {
		in.TimerName = strings.TrimSpace(string(r[:MaxNameRunes]))
	}
	if !slices.Contains(Colors, in.Color) {
		in.Color = fallbackColor
	}
	var why string
	switch {
	case in.ClientID == "" || len(in.ClientID) > maxClientIDLen:
		why = fmt.Sprintf("client_id must be 1 to %d characters", maxClientIDLen)
	case in.TimerName == "":
		why = "timer_name is empty"
	case in.FocusSeconds < MinFocusSeconds:
		why = fmt.Sprintf("under %d seconds of focus", MinFocusSeconds)
	case in.RoundsDone < 0 || in.RoundsDone > maxRounds:
		why = fmt.Sprintf("rounds_done must be 0 to %d", maxRounds)
	case in.StartedAt.IsZero() || in.EndedAt.IsZero():
		why = "started_at and ended_at are required"
	case in.EndedAt.Before(in.StartedAt):
		why = "ended_at is before started_at"
	case in.StartedAt.After(now.Add(clockSkew)):
		why = "started_at is in the future"
	case in.EndedAt.After(now.Add(clockSkew)):
		why = "ended_at is in the future"
	case time.Duration(in.FocusSeconds)*time.Second > in.EndedAt.Sub(in.StartedAt):
		why = "more focus than the session lasted"
	}
	if why != "" {
		return in, fmt.Errorf("%w: %s", ErrInvalid, why)
	}
	return in, nil
}

// RecordSession stores one of the user's ended sessions. The browser keeps
// trying until it hears back, so the same session can arrive twice: a
// client_id the user already has returns that row unchanged, with created
// false.
func (st *Store) RecordSession(ctx context.Context, userID int64, in SessionInput) (Session, bool, error) {
	in, err := checkSession(in, st.now())
	if err != nil {
		return Session{}, false, err
	}
	// A timer deleted mid-session, or an id that isn't this user's, is
	// stored as no timer — never an error (spec: "Recording endpoint").
	var timerID sql.NullInt64
	if in.TimerID > 0 {
		err := st.db.QueryRowContext(ctx,
			`SELECT id FROM focus_timers WHERE user_id = ? AND id = ?`, userID, in.TimerID).Scan(&timerID)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return Session{}, false, fmt.Errorf("focus: record session: %w", err)
		}
	}
	res, err := st.db.ExecContext(ctx, `
		INSERT INTO focus_sessions (user_id, timer_id, timer_name, color, client_id,
			started_at, ended_at, focus_seconds, rounds_done, completed)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (user_id, client_id) DO NOTHING`,
		userID, timerID, in.TimerName, in.Color, in.ClientID,
		db.FormatTime(in.StartedAt), db.FormatTime(in.EndedAt),
		in.FocusSeconds, in.RoundsDone, flag(in.Completed))
	if err != nil {
		return Session{}, false, fmt.Errorf("focus: record session: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return Session{}, false, fmt.Errorf("focus: record session: %w", err)
	}
	s, err := scanSession(st.db.QueryRowContext(ctx,
		`SELECT `+sessionColumns+` FROM focus_sessions WHERE user_id = ? AND client_id = ?`,
		userID, in.ClientID))
	if err != nil {
		return Session{}, false, err
	}
	return s, n == 1, nil
}

// DeleteSession removes one of the user's recorded sessions.
func (st *Store) DeleteSession(ctx context.Context, userID, id int64) error {
	res, err := st.db.ExecContext(ctx, `DELETE FROM focus_sessions WHERE user_id = ? AND id = ?`, userID, id)
	if err != nil {
		return fmt.Errorf("focus: delete session: %w", err)
	}
	if n, err := res.RowsAffected(); err != nil {
		return fmt.Errorf("focus: delete session: %w", err)
	} else if n == 0 {
		return ErrNotFound
	}
	return nil
}
