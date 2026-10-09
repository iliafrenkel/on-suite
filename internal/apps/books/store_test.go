package books_test

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/iliafrenkel/on-suite/internal/apps/books"
	"github.com/iliafrenkel/on-suite/internal/apptest"
	"github.com/iliafrenkel/on-suite/internal/platform/auth"
	"github.com/iliafrenkel/on-suite/internal/platform/db"
)

// fixture is a migrated database with two users and a clock the test moves.
// The default now, 15:00 UTC on 9 October 2026, is already 10 October in
// Melbourne (TestMain's zone), so a test that sees "2026-10-10" knows the
// local day was used.
type fixture struct {
	store *books.Store
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
	appMigrations, err := db.Collect(books.ID, books.Migrations())
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
	st := books.NewStore(handle)
	f := &fixture{store: st, db: handle, alice: alice, bob: bob, now: time.Date(2026, 10, 9, 15, 0, 0, 0, time.UTC)}
	st.SetClock(func() time.Time { return f.now })
	return f
}

func TestTodayIsTheLocalDay(t *testing.T) {
	f := newFixture(t)
	if got := f.store.Today(); got != "2026-10-10" {
		t.Errorf("Today() = %q, want 2026-10-10 (Melbourne, not UTC)", got)
	}
}

func TestSchemaAllowsOneReadingInProgressPerBook(t *testing.T) {
	f := newFixture(t)
	now := db.FormatTime(f.now)
	res, err := f.db.Exec(`INSERT INTO books_books (user_id, title, added_at, updated_at) VALUES (?, 'x', ?, ?)`,
		f.alice.ID, now, now)
	if err != nil {
		t.Fatal(err)
	}
	id, _ := res.LastInsertId()
	insert := `INSERT INTO books_readings (book_id, status, created_at) VALUES (?, 'reading', ?)`
	if _, err := f.db.Exec(insert, id, now); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Exec(insert, id, now); err == nil {
		t.Fatal("inserted a second reading in progress, want a unique-index failure")
	}
}
