package notes_test

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"

	"github.com/iliafrenkel/on-suite/internal/apps/notes"
	"github.com/iliafrenkel/on-suite/internal/platform/auth"
	"github.com/iliafrenkel/on-suite/internal/platform/db"
)

// fixture is a migrated database with two users, so every owner-scoping test
// has somebody else to be confused with.
type fixture struct {
	store *notes.Store
	db    *sql.DB
	alice auth.User
	bob   auth.User
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	ctx := context.Background()

	// A real file in a temp dir, not :memory: — the bugs that matter here
	// live in SQL, recursive CTEs and transaction behaviour.
	handle, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = handle.Close() })

	platform, err := db.Collect(auth.Namespace, auth.Migrations())
	if err != nil {
		t.Fatal(err)
	}
	appSchema, err := db.Collect(notes.ID, notes.Migrations())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Apply(ctx, handle, append(platform, appSchema...)); err != nil {
		t.Fatal(err)
	}

	users := auth.NewStore(handle)
	hash, err := auth.HashPassword("a-sufficiently-long-password")
	if err != nil {
		t.Fatal(err)
	}
	alice, err := users.CreateUser(ctx, "alice", hash, true)
	if err != nil {
		t.Fatal(err)
	}
	bob, err := users.CreateUser(ctx, "bob", hash, false)
	if err != nil {
		t.Fatal(err)
	}
	return &fixture{store: notes.NewStore(handle), db: handle, alice: alice, bob: bob}
}

func TestAFreshDatabaseHasNoViolations(t *testing.T) {
	f := newFixture(t)
	checkInvariants(t, f.db)
}

func TestByIDOnAMissingNode(t *testing.T) {
	f := newFixture(t)
	if _, err := f.store.ByID(context.Background(), f.alice.ID, 12345); !errors.Is(err, notes.ErrNotFound) {
		t.Fatalf("ByID on a missing node = %v; want ErrNotFound", err)
	}
}

// childTitles reads a parent's children straight from the table, in position
// order. Structural assertions deliberately bypass the store, so a test of
// Create cannot be fooled by a matching bug in a read method.
func (f *fixture) childTitles(t *testing.T, userID, parentID int64) []string {
	t.Helper()

	var parent any
	if parentID != notes.RootID {
		parent = parentID
	}
	rows, err := f.db.QueryContext(context.Background(),
		`SELECT title FROM notes_nodes
		  WHERE user_id = ? AND parent_id IS ? ORDER BY position`, userID, parent)
	if err != nil {
		t.Fatalf("reading child titles: %v", err)
	}
	defer func() { _ = rows.Close() }()

	var out []string
	for rows.Next() {
		var title string
		if err := rows.Scan(&title); err != nil {
			t.Fatalf("scanning title: %v", err)
		}
		out = append(out, title)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("reading child titles: %v", err)
	}
	return out
}
