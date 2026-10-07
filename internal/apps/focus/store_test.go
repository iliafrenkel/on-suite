package focus_test

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/iliafrenkel/on-suite/internal/apps/focus"
	"github.com/iliafrenkel/on-suite/internal/apptest"
	"github.com/iliafrenkel/on-suite/internal/platform/auth"
	"github.com/iliafrenkel/on-suite/internal/platform/db"
)

// fixture is a migrated database with two users and a clock the test moves.
type fixture struct {
	store *focus.Store
	db    *sql.DB
	alice auth.User
	bob   auth.User
	now   time.Time
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	ctx := context.Background()

	handle, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = handle.Close() })

	migrations, err := db.Collect(auth.Namespace, auth.Migrations())
	if err != nil {
		t.Fatal(err)
	}
	appMigrations, err := db.Collect(focus.ID, focus.Migrations())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Apply(ctx, handle, append(migrations, appMigrations...)); err != nil {
		t.Fatal(err)
	}

	users := auth.NewStore(handle)
	alice, err := users.CreateUser(ctx, "alice", apptest.PasswordHash, true)
	if err != nil {
		t.Fatal(err)
	}
	bob, err := users.CreateUser(ctx, "bob", apptest.PasswordHash, false)
	if err != nil {
		t.Fatal(err)
	}
	st := focus.NewStore(handle)
	f := &fixture{store: st, db: handle, alice: alice, bob: bob, now: time.Date(2026, 10, 7, 9, 0, 0, 0, time.UTC)}
	st.SetClock(func() time.Time { return f.now })
	return f
}

func TestSchemaRejectsASingleTimerWithIntervalColumns(t *testing.T) {
	f := newFixture(t)
	now := db.FormatTime(f.now)
	_, err := f.db.Exec(`INSERT INTO focus_timers
		(user_id, name, kind, focus_minutes, rounds, position, created_at, updated_at)
		VALUES (?, 'x', 'single', 15, 4, 0, ?, ?)`, f.alice.ID, now, now)
	if err == nil {
		t.Fatal("inserted a single timer with rounds set, want a CHECK failure")
	}
}

func TestSchemaRejectsIntervalsWithoutIntervalColumns(t *testing.T) {
	f := newFixture(t)
	now := db.FormatTime(f.now)
	_, err := f.db.Exec(`INSERT INTO focus_timers
		(user_id, name, kind, focus_minutes, position, created_at, updated_at)
		VALUES (?, 'x', 'intervals', 50, 0, ?, ?)`, f.alice.ID, now, now)
	if err == nil {
		t.Fatal("inserted an intervals timer with no break/rounds, want a CHECK failure")
	}
}

func (f *fixture) tick() { f.now = f.now.Add(time.Minute) }

func (f *fixture) create(t *testing.T, userID int64, name string) focus.Timer {
	t.Helper()
	in := focus.DefaultInput()
	in.Name = name
	tm, err := f.store.CreateTimer(context.Background(), userID, in)
	if err != nil {
		t.Fatal(err)
	}
	return tm
}

func names(ts []focus.Timer) []string {
	var out []string
	for _, t := range ts {
		out = append(out, t.Name)
	}
	return out
}

func TestCreateTimerStoresEveryField(t *testing.T) {
	f := newFixture(t)
	in := validIntervals()
	in.Name = "  Deep work "
	got, err := f.store.CreateTimer(context.Background(), f.alice.ID, in)
	if err != nil {
		t.Fatal(err)
	}
	back, err := f.store.Timer(context.Background(), f.alice.ID, got.ID)
	if err != nil {
		t.Fatal(err)
	}
	want := in.Normalize()
	if back.TimerInput != want {
		t.Errorf("stored %+v, want %+v", back.TimerInput, want)
	}
	if !back.CreatedAt.Equal(f.now) || !back.UpdatedAt.Equal(f.now) || back.UserID != f.alice.ID {
		t.Errorf("timestamps/user = %v %v %d", back.CreatedAt, back.UpdatedAt, back.UserID)
	}
}

func TestCreateTimerSingleStoresNullIntervals(t *testing.T) {
	f := newFixture(t)
	tm := f.create(t, f.alice.ID, "Reading")
	var rounds sql.NullInt64
	if err := f.db.QueryRow(`SELECT rounds FROM focus_timers WHERE id = ?`, tm.ID).Scan(&rounds); err != nil {
		t.Fatal(err)
	}
	if rounds.Valid {
		t.Errorf("rounds = %d, want NULL for a single timer", rounds.Int64)
	}
}

func TestCreateTimerAppendsInOrder(t *testing.T) {
	f := newFixture(t)
	f.create(t, f.alice.ID, "A")
	f.create(t, f.alice.ID, "B")
	f.create(t, f.bob.ID, "Bob's")
	f.create(t, f.alice.ID, "C")
	ts, err := f.store.Timers(context.Background(), f.alice.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got := names(ts); !slices.Equal(got, []string{"A", "B", "C"}) {
		t.Errorf("Timers = %v, want [A B C]", got)
	}
}

func TestCreateTimerRejectsInvalidInput(t *testing.T) {
	f := newFixture(t)
	in := focus.DefaultInput() // no name
	_, err := f.store.CreateTimer(context.Background(), f.alice.ID, in)
	var ve *focus.ValidationError
	if !errors.As(err, &ve) || ve.Fields["name"] == "" || !errors.Is(err, focus.ErrInvalid) {
		t.Fatalf("CreateTimer(no name) = %v, want a ValidationError for name", err)
	}
}

func TestTimerIsNotFoundForSomeoneElse(t *testing.T) {
	f := newFixture(t)
	tm := f.create(t, f.alice.ID, "Mine")
	if _, err := f.store.Timer(context.Background(), f.bob.ID, tm.ID); !errors.Is(err, focus.ErrNotFound) {
		t.Errorf("bob reading alice's timer = %v, want ErrNotFound", err)
	}
	if _, err := f.store.Timer(context.Background(), f.alice.ID, 9999); !errors.Is(err, focus.ErrNotFound) {
		t.Errorf("missing timer = %v, want ErrNotFound", err)
	}
}

func TestUpdateTimerChangesFieldsAndKeepsPosition(t *testing.T) {
	f := newFixture(t)
	f.create(t, f.alice.ID, "First")
	tm := f.create(t, f.alice.ID, "Second")
	f.tick()
	in := validIntervals()
	got, err := f.store.UpdateTimer(context.Background(), f.alice.ID, tm.ID, in)
	if err != nil {
		t.Fatal(err)
	}
	if got.TimerInput != in.Normalize() || got.Position != tm.Position {
		t.Errorf("updated = %+v (pos %d), want %+v (pos %d)", got.TimerInput, got.Position, in, tm.Position)
	}
	if !got.UpdatedAt.Equal(f.now) || !got.CreatedAt.Equal(tm.CreatedAt) {
		t.Errorf("updated_at %v created_at %v", got.UpdatedAt, got.CreatedAt)
	}
}

func TestUpdateTimerToSingleClearsIntervals(t *testing.T) {
	f := newFixture(t)
	tm, err := f.store.CreateTimer(context.Background(), f.alice.ID, validIntervals())
	if err != nil {
		t.Fatal(err)
	}
	in := tm.TimerInput
	in.Kind = focus.KindSingle
	got, err := f.store.UpdateTimer(context.Background(), f.alice.ID, tm.ID, in)
	if err != nil {
		t.Fatal(err)
	}
	if got.Rounds != 0 || got.BreakMinutes != 0 {
		t.Errorf("single timer kept intervals: %+v", got.TimerInput)
	}
}

func TestUpdateTimerRefusesSomeoneElsesAndInvalid(t *testing.T) {
	f := newFixture(t)
	tm := f.create(t, f.alice.ID, "Mine")
	if _, err := f.store.UpdateTimer(context.Background(), f.bob.ID, tm.ID, validSingle()); !errors.Is(err, focus.ErrNotFound) {
		t.Errorf("bob updating = %v, want ErrNotFound", err)
	}
	bad := validSingle()
	bad.FocusMinutes = 0
	if _, err := f.store.UpdateTimer(context.Background(), f.alice.ID, tm.ID, bad); !errors.Is(err, focus.ErrInvalid) {
		t.Errorf("invalid update = %v, want ErrInvalid", err)
	}
}

func TestDeleteTimer(t *testing.T) {
	f := newFixture(t)
	tm := f.create(t, f.alice.ID, "Gone")
	if err := f.store.DeleteTimer(context.Background(), f.bob.ID, tm.ID); !errors.Is(err, focus.ErrNotFound) {
		t.Errorf("bob deleting = %v, want ErrNotFound", err)
	}
	if err := f.store.DeleteTimer(context.Background(), f.alice.ID, tm.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.Timer(context.Background(), f.alice.ID, tm.ID); !errors.Is(err, focus.ErrNotFound) {
		t.Errorf("after delete = %v, want ErrNotFound", err)
	}
}

func timerIDs(ts []focus.Timer) []int64 {
	var out []int64
	for _, t := range ts {
		out = append(out, t.ID)
	}
	return out
}

func TestDuplicateTimerPlacesTheCopyAfterTheOriginal(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	a := f.create(t, f.alice.ID, "A")
	orig, err := f.store.CreateTimer(ctx, f.alice.ID, validIntervals())
	if err != nil {
		t.Fatal(err)
	}
	c := f.create(t, f.alice.ID, "C")
	dup, err := f.store.DuplicateTimer(ctx, f.alice.ID, orig.ID)
	if err != nil {
		t.Fatal(err)
	}
	wantIn := orig.TimerInput
	wantIn.Name = "Deep work (copy)"
	if dup.TimerInput != wantIn {
		t.Errorf("copy = %+v, want %+v", dup.TimerInput, wantIn)
	}
	ts, err := f.store.Timers(ctx, f.alice.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := timerIDs(ts), []int64{a.ID, orig.ID, dup.ID, c.ID}; !slices.Equal(got, want) {
		t.Errorf("order = %v, want %v", got, want)
	}
}

func TestDuplicateTimerKeepsLongNamesWithinTheLimit(t *testing.T) {
	f := newFixture(t)
	orig := f.create(t, f.alice.ID, strings.Repeat("é", 80))
	dup, err := f.store.DuplicateTimer(context.Background(), f.alice.ID, orig.ID)
	if err != nil {
		t.Fatal(err)
	}
	if n := utf8.RuneCountInString(dup.Name); n != 80 || !strings.HasSuffix(dup.Name, " (copy)") {
		t.Errorf("copy name %q has %d characters, want 80 ending in (copy)", dup.Name, n)
	}
}

func TestDuplicateTimerIsNotFoundForSomeoneElse(t *testing.T) {
	f := newFixture(t)
	orig := f.create(t, f.alice.ID, "Mine")
	if _, err := f.store.DuplicateTimer(context.Background(), f.bob.ID, orig.ID); !errors.Is(err, focus.ErrNotFound) {
		t.Errorf("bob duplicating = %v, want ErrNotFound", err)
	}
}

func TestReorderTimers(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	a, b, c := f.create(t, f.alice.ID, "A"), f.create(t, f.alice.ID, "B"), f.create(t, f.alice.ID, "C")
	if err := f.store.ReorderTimers(ctx, f.alice.ID, []int64{c.ID, a.ID, b.ID}); err != nil {
		t.Fatal(err)
	}
	ts, err := f.store.Timers(ctx, f.alice.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got := names(ts); !slices.Equal(got, []string{"C", "A", "B"}) {
		t.Errorf("order = %v, want [C A B]", got)
	}
}

func TestReorderTimersRefusesAnythingButTheExactSet(t *testing.T) {
	f := newFixture(t)
	a, b := f.create(t, f.alice.ID, "A"), f.create(t, f.alice.ID, "B")
	other := f.create(t, f.bob.ID, "Bob's")
	for name, ids := range map[string][]int64{
		"missing one": {a.ID},
		"duplicate":   {a.ID, a.ID},
		"foreign":     {a.ID, b.ID, other.ID},
		"swapped in":  {a.ID, other.ID},
		"empty":       {},
	} {
		if err := f.store.ReorderTimers(context.Background(), f.alice.ID, ids); !errors.Is(err, focus.ErrInvalid) {
			t.Errorf("%s: ReorderTimers = %v, want ErrInvalid", name, err)
		}
	}
}
