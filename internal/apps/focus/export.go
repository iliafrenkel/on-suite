package focus

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"time"

	"github.com/iliafrenkel/on-suite/internal/platform/app"
)

// exportedTimer, exportedSession and exportPayload are the shapes
// `onsuite export` writes (spec: "Export and admin"). They are declared
// apart from the store's types so the backup format changes only when
// someone edits this file. Every field but user_id.
type exportedTimer struct {
	ID               int64     `json:"id"`
	Name             string    `json:"name"`
	Color            string    `json:"color"`
	Kind             Kind      `json:"kind"`
	FocusMinutes     int       `json:"focus_minutes"`
	BreakMinutes     int       `json:"break_minutes,omitempty"`
	LongBreakMinutes int       `json:"long_break_minutes,omitempty"`
	Rounds           int       `json:"rounds,omitempty"`
	LongBreakEvery   int       `json:"long_break_every,omitempty"`
	AutoAdvance      bool      `json:"auto_advance"`
	Chime            string    `json:"chime"`
	KeepHistory      bool      `json:"keep_history"`
	Position         int       `json:"position"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

type exportedSession struct {
	ID           int64     `json:"id"`
	TimerID      *int64    `json:"timer_id"` // null once the timer is deleted
	TimerName    string    `json:"timer_name"`
	Color        string    `json:"color"`
	ClientID     string    `json:"client_id"`
	StartedAt    time.Time `json:"started_at"`
	EndedAt      time.Time `json:"ended_at"`
	FocusSeconds int       `json:"focus_seconds"`
	RoundsDone   int       `json:"rounds_done"`
	Completed    bool      `json:"completed"`
}

type exportPayload struct {
	Timers   []exportedTimer   `json:"timers"`
	Sessions []exportedSession `json:"sessions"`
}

// Export implements app.Exporter, joining ON Focus to onsuite export's
// whole-account JSON backup.
func (a *App) Export(ctx context.Context, handle *sql.DB, userID int64) (any, error) {
	return NewStore(handle).Export(ctx, userID)
}

// Export gathers one user's timers in tile order and sessions oldest first.
func (st *Store) Export(ctx context.Context, userID int64) (exportPayload, error) {
	out := exportPayload{Timers: []exportedTimer{}, Sessions: []exportedSession{}}
	timers, err := st.Timers(ctx, userID)
	if err != nil {
		return exportPayload{}, err
	}
	for _, t := range timers {
		out.Timers = append(out.Timers, exportedTimer{
			ID: t.ID, Name: t.Name, Color: t.Color, Kind: t.Kind, FocusMinutes: t.FocusMinutes,
			BreakMinutes: t.BreakMinutes, LongBreakMinutes: t.LongBreakMinutes, Rounds: t.Rounds,
			LongBreakEvery: t.LongBreakEvery, AutoAdvance: t.AutoAdvance, Chime: t.Chime,
			KeepHistory: t.KeepHistory, Position: t.Position, CreatedAt: t.CreatedAt, UpdatedAt: t.UpdatedAt,
		})
	}
	sessions, err := st.querySessions(ctx, `SELECT `+sessionColumns+` FROM focus_sessions
		 WHERE user_id = ? ORDER BY started_at, id`, userID)
	if err != nil {
		return exportPayload{}, err
	}
	for _, s := range sessions {
		e := exportedSession{
			ID: s.ID, TimerName: s.TimerName, Color: s.Color, ClientID: s.ClientID,
			StartedAt: s.StartedAt, EndedAt: s.EndedAt, FocusSeconds: s.FocusSeconds,
			RoundsDone: s.RoundsDone, Completed: s.Completed,
		}
		if s.TimerID != 0 {
			id := s.TimerID
			e.TimerID = &id
		}
		out.Sessions = append(out.Sessions, e)
	}
	return out, nil
}

// Stats implements app.Stater: ON Focus's card on the admin page.
func (a *App) Stats(ctx context.Context, handle *sql.DB) ([]app.Stat, error) {
	return NewStore(handle).Stats(ctx)
}

// Stats describes ON Focus across every user (spec: "Export and admin").
func (st *Store) Stats(ctx context.Context) ([]app.Stat, error) {
	var timers, sessions, seconds int64
	if err := st.db.QueryRowContext(ctx, `SELECT count(*) FROM focus_timers`).Scan(&timers); err != nil {
		return nil, fmt.Errorf("focus: stats timers: %w", err)
	}
	if err := st.db.QueryRowContext(ctx,
		`SELECT count(*), coalesce(sum(focus_seconds), 0) FROM focus_sessions`).Scan(&sessions, &seconds); err != nil {
		return nil, fmt.Errorf("focus: stats sessions: %w", err)
	}
	n := func(v int64) string { return strconv.FormatInt(v, 10) }
	return []app.Stat{
		{Label: "Timers", Value: n(timers)},
		{Label: "Sessions recorded", Value: n(sessions)},
		{Label: "Focus time", Value: FormatFocus(int(seconds)), Hint: "recorded sessions, for everyone"},
	}, nil
}
