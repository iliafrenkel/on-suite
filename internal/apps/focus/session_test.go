package focus_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/iliafrenkel/on-suite/internal/apps/focus"
)

// sessionInput is a finished, completed session of minutes of focus that
// started at start.
func sessionInput(clientID, name string, start time.Time, minutes int) focus.SessionInput {
	return focus.SessionInput{
		ClientID: clientID, TimerName: name, Color: "teal",
		StartedAt: start, EndedAt: start.Add(time.Duration(minutes) * time.Minute),
		FocusSeconds: minutes * 60, RoundsDone: 1, Completed: true,
	}
}

func countSessions(t *testing.T, f *fixture) int {
	t.Helper()
	var n int
	if err := f.db.QueryRow(`SELECT count(*) FROM focus_sessions`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestRecordSessionStoresEveryField(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	tm, err := f.store.CreateTimer(ctx, f.alice.ID, validIntervals())
	if err != nil {
		t.Fatal(err)
	}
	start := f.now.Add(-4 * time.Hour)
	in := focus.SessionInput{
		ClientID: "c-1", TimerID: tm.ID, TimerName: "Deep work", Color: "blue",
		StartedAt: start, EndedAt: start.Add(3 * time.Hour),
		FocusSeconds: 2*3000 + 1200, RoundsDone: 2, Completed: false,
	}
	got, created, err := f.store.RecordSession(ctx, f.alice.ID, in)
	if err != nil {
		t.Fatal(err)
	}
	if !created || got.ID == 0 || got.UserID != f.alice.ID || got.TimerID != tm.ID ||
		got.TimerName != "Deep work" || got.Color != "blue" || got.ClientID != "c-1" ||
		!got.StartedAt.Equal(start) || !got.EndedAt.Equal(start.Add(3*time.Hour)) ||
		got.FocusSeconds != 7200 || got.RoundsDone != 2 || got.Completed {
		t.Errorf("recorded %+v, created %v", got, created)
	}
}

func TestRecordSessionIsIdempotent(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	first, created, err := f.store.RecordSession(ctx, f.alice.ID, sessionInput("same", "Reading", f.now.Add(-time.Hour), 30))
	if err != nil || !created {
		t.Fatalf("first: created %v, err %v", created, err)
	}
	// A retry of the same session, even with different numbers, changes nothing.
	again, created, err := f.store.RecordSession(ctx, f.alice.ID, sessionInput("same", "Other", f.now.Add(-time.Hour), 45))
	if err != nil {
		t.Fatal(err)
	}
	if created || again.ID != first.ID || again.TimerName != "Reading" || again.FocusSeconds != 1800 {
		t.Errorf("retry: created %v, got %+v", created, again)
	}
	// Bob's browser may well pick the same id; it is his own row.
	if _, created, err := f.store.RecordSession(ctx, f.bob.ID, sessionInput("same", "Bob's", f.now.Add(-time.Hour), 30)); err != nil || !created {
		t.Errorf("bob: created %v, err %v", created, err)
	}
	if n := countSessions(t, f); n != 2 {
		t.Errorf("%d rows, want 2", n)
	}
}

func TestRecordSessionRefusesImpossibleSessions(t *testing.T) {
	f := newFixture(t)
	start := f.now.Add(-time.Hour)
	cases := map[string]func(*focus.SessionInput){
		"under a minute":       func(in *focus.SessionInput) { in.FocusSeconds = 59 },
		"no client id":         func(in *focus.SessionInput) { in.ClientID = "  " },
		"long client id":       func(in *focus.SessionInput) { in.ClientID = strings.Repeat("x", 65) },
		"no name":              func(in *focus.SessionInput) { in.TimerName = " " },
		"no start":             func(in *focus.SessionInput) { in.StartedAt = time.Time{} },
		"no end":               func(in *focus.SessionInput) { in.EndedAt = time.Time{} },
		"ends before start":    func(in *focus.SessionInput) { in.EndedAt = start.Add(-time.Minute) },
		"more focus than time": func(in *focus.SessionInput) { in.FocusSeconds = 30*60 + 1 },
		"negative rounds":      func(in *focus.SessionInput) { in.RoundsDone = -1 },
		"too many rounds":      func(in *focus.SessionInput) { in.RoundsDone = 13 },
		"starts in the future": func(in *focus.SessionInput) {
			in.StartedAt = f.now.Add(6 * time.Minute)
			in.EndedAt = in.StartedAt.Add(30 * time.Minute)
		},
	}
	for name, mutate := range cases {
		in := sessionInput("c-"+name, "Reading", start, 30)
		mutate(&in)
		if _, _, err := f.store.RecordSession(context.Background(), f.alice.ID, in); !errors.Is(err, focus.ErrInvalid) {
			t.Errorf("%s: err = %v, want ErrInvalid", name, err)
		}
	}
	if n := countSessions(t, f); n != 0 {
		t.Errorf("%d rows stored, want none", n)
	}
}

func TestRecordSessionAllowsALittleClockSkew(t *testing.T) {
	f := newFixture(t)
	in := sessionInput("fast-clock", "Reading", f.now.Add(4*time.Minute), 30)
	if _, _, err := f.store.RecordSession(context.Background(), f.alice.ID, in); err != nil {
		t.Errorf("a browser clock 4 minutes fast: %v", err)
	}
}

func TestRecordSessionTidiesNameAndColour(t *testing.T) {
	f := newFixture(t)
	in := sessionInput("c-1", "  "+strings.Repeat("é", 90)+"  ", f.now.Add(-time.Hour), 30)
	in.Color = "chartreuse"
	got, _, err := f.store.RecordSession(context.Background(), f.alice.ID, in)
	if err != nil {
		t.Fatal(err)
	}
	if got.TimerName != strings.Repeat("é", focus.MaxNameRunes) {
		t.Errorf("name = %q (%d runes)", got.TimerName, len([]rune(got.TimerName)))
	}
	if got.Color != "gray" {
		t.Errorf("colour = %q, want gray", got.Color)
	}
}

func TestRecordSessionDropsATimerThatIsNotTheUsers(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	bobs, err := f.store.CreateTimer(ctx, f.bob.ID, validIntervals())
	if err != nil {
		t.Fatal(err)
	}
	for i, timerID := range []int64{bobs.ID, 99999} {
		in := sessionInput("c-"+itoa(int64(i)), "Deep work", f.now.Add(-time.Hour), 30)
		in.TimerID = timerID
		got, _, err := f.store.RecordSession(ctx, f.alice.ID, in)
		if err != nil {
			t.Fatalf("timer %d: %v", timerID, err)
		}
		if got.TimerID != 0 {
			t.Errorf("timer %d stored as %d, want none", timerID, got.TimerID)
		}
	}
}

func TestDeletingATimerKeepsItsSessions(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	tm, err := f.store.CreateTimer(ctx, f.alice.ID, validIntervals())
	if err != nil {
		t.Fatal(err)
	}
	in := sessionInput("c-1", "Deep work", f.now.Add(-time.Hour), 50)
	in.TimerID, in.Color = tm.ID, "blue"
	if _, _, err := f.store.RecordSession(ctx, f.alice.ID, in); err != nil {
		t.Fatal(err)
	}
	if err := f.store.DeleteTimer(ctx, f.alice.ID, tm.ID); err != nil {
		t.Fatal(err)
	}
	var timerID *int64
	var name, color string
	if err := f.db.QueryRow(`SELECT timer_id, timer_name, color FROM focus_sessions`).Scan(&timerID, &name, &color); err != nil {
		t.Fatal(err)
	}
	if timerID != nil || name != "Deep work" || color != "blue" {
		t.Errorf("after delete: timer_id %v, %q, %q", timerID, name, color)
	}
}

func TestDeleteSession(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	s, _, err := f.store.RecordSession(ctx, f.alice.ID, sessionInput("c-1", "Reading", f.now.Add(-time.Hour), 30))
	if err != nil {
		t.Fatal(err)
	}
	if err := f.store.DeleteSession(ctx, f.bob.ID, s.ID); !errors.Is(err, focus.ErrNotFound) {
		t.Errorf("bob deletes alice's session: %v, want ErrNotFound", err)
	}
	if err := f.store.DeleteSession(ctx, f.alice.ID, s.ID); err != nil {
		t.Fatal(err)
	}
	if err := f.store.DeleteSession(ctx, f.alice.ID, s.ID); !errors.Is(err, focus.ErrNotFound) {
		t.Errorf("second delete: %v, want ErrNotFound", err)
	}
	if n := countSessions(t, f); n != 0 {
		t.Errorf("%d rows left", n)
	}
}
