package focus_test

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

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
