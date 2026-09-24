package notes_test

import (
	"context"
	"database/sql"
	"path/filepath"
	"slices"
	"strconv"
	"testing"

	"github.com/iliafrenkel/on-suite/internal/apps/notes"
	"github.com/iliafrenkel/on-suite/internal/apptest"
	"github.com/iliafrenkel/on-suite/internal/platform/auth"
	"github.com/iliafrenkel/on-suite/internal/platform/db"
)

// notesLegacyTimes are values as time.RFC3339Nano wrote them before #356,
// all inside one second and listed in time order. As text they sort
// differently ('Z' sorts after every digit), which is the bug.
var notesLegacyTimes = []string{
	"2026-09-25T12:00:05Z",
	"2026-09-25T12:00:05.123456789Z",
	"2026-09-25T12:00:05.244Z",
	"2026-09-25T12:00:05.244124Z",
	"2026-09-25T12:00:05.5Z",
}

// openNotesThrough is newFixture with Notes' schema stopped after lastID,
// so a test can seed rows the way an older binary wrote them. It returns
// every migration for the test to finish applying.
func openNotesThrough(t *testing.T, lastID string) (*sql.DB, []db.Migration, auth.User) {
	t.Helper()
	ctx := context.Background()
	handle, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = handle.Close() })
	platform, err := db.Collect(auth.Namespace, auth.Migrations())
	if err != nil {
		t.Fatal(err)
	}
	app, err := db.Collect(notes.ID, notes.Migrations())
	if err != nil {
		t.Fatal(err)
	}
	early := slices.Clone(platform)
	for _, m := range app {
		if m.ID <= lastID {
			early = append(early, m)
		}
	}
	if _, err := db.Apply(ctx, handle, early); err != nil {
		t.Fatal(err)
	}
	alice, err := auth.NewStore(handle).CreateUser(ctx, "alice", apptest.PasswordHash, true)
	if err != nil {
		t.Fatal(err)
	}
	return handle, append(platform, app...), alice
}

func TestFixedWidthMigrationRewritesNotesTimestamps(t *testing.T) {
	handle, ms, alice := openNotesThrough(t, "0006")
	ctx := context.Background()

	for i, ts := range notesLegacyTimes {
		var done any // NULL on even rows, so NULLs are checked too
		if i%2 == 1 {
			done = ts
		}
		if _, err := handle.ExecContext(ctx,
			`INSERT INTO notes_nodes (id, user_id, parent_id, position, title, note,
			        created_at, updated_at, done_at, due_on, archived_at)
			 VALUES (?, ?, NULL, ?, ?, '', ?, ?, ?, '2026-09-30', ?)`,
			i+1, alice.ID, i, "archived "+strconv.Itoa(i), ts, ts, done, ts); err != nil {
			t.Fatal(err)
		}
	}
	// One live node, to prove the FTS index survives the rewrite's UPDATEs.
	if _, err := handle.ExecContext(ctx,
		`INSERT INTO notes_nodes (id, user_id, parent_id, position, title, note, created_at, updated_at)
		 VALUES (6, ?, NULL, 5, 'unarchived needle', '', '2026-09-25T12:00:05.7Z', '2026-09-25T12:00:05.7Z')`,
		alice.ID); err != nil {
		t.Fatal(err)
	}

	if _, err := db.Apply(ctx, handle, ms); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	for i, ts := range notesLegacyTimes {
		parsed, err := db.ParseTime(ts)
		if err != nil {
			t.Fatal(err)
		}
		want := db.FormatTime(parsed)
		var created, updated, archived, dueOn string
		var done sql.NullString
		if err := handle.QueryRowContext(ctx,
			`SELECT created_at, updated_at, done_at, archived_at, due_on FROM notes_nodes WHERE id = ?`,
			i+1).Scan(&created, &updated, &done, &archived, &dueOn); err != nil {
			t.Fatal(err)
		}
		for name, got := range map[string]string{"created_at": created, "updated_at": updated, "archived_at": archived} {
			if got != want {
				t.Errorf("node %d %s = %q, want %q (from %q)", i+1, name, got, want, ts)
			}
		}
		switch {
		case i%2 == 0 && done.Valid:
			t.Errorf("node %d done_at = %q, want NULL left alone", i+1, done.String)
		case i%2 == 1 && done.String != want:
			t.Errorf("node %d done_at = %q, want %q", i+1, done.String, want)
		}
		if dueOn != "2026-09-30" {
			t.Errorf("node %d due_on = %q, want the date-only value untouched", i+1, dueOn)
		}
	}

	st := notes.NewStore(handle)
	archive, err := st.Archive(ctx, alice.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	var order []int64
	for _, n := range archive {
		order = append(order, n.ID)
	}
	if want := []int64{5, 4, 3, 2, 1}; !slices.Equal(order, want) {
		t.Errorf("Archive (most recent first) = %v, want %v", order, want)
	}
	found, err := st.Search(ctx, alice.ID, "needle", true)
	if err != nil {
		t.Fatal(err)
	}
	if len(found) != 1 || found[0].ID != 6 {
		t.Errorf("Search(needle) after the rewrite = %+v, want node 6", found)
	}
}

// migrationSQL returns the body of one notes migration.
func migrationSQL(t *testing.T, ms []db.Migration, id string) string {
	t.Helper()
	for _, m := range ms {
		if m.ID == id {
			return m.SQL
		}
	}
	t.Fatalf("no notes migration %s", id)
	return ""
}

// TestFixedWidthMigrationIsIdempotent runs the notes:0007 migration SQL a
// second time (RowsAffected only reflects the last statement in a
// multi-statement Exec, so it can't tell us this on its own) and checks
// every rewritten column, plus the odd/garbage values seeded alongside
// them, comes out identical both times.
func TestFixedWidthMigrationIsIdempotent(t *testing.T) {
	handle, ms, alice := openNotesThrough(t, "0006")
	ctx := context.Background()

	for i, ts := range notesLegacyTimes {
		var done any
		if i%2 == 1 {
			done = ts
		}
		if _, err := handle.ExecContext(ctx,
			`INSERT INTO notes_nodes (id, user_id, parent_id, position, title, note,
			        created_at, updated_at, done_at, due_on, archived_at)
			 VALUES (?, ?, NULL, ?, ?, '', ?, ?, ?, '2026-09-30', ?)`,
			i+1, alice.ID, i, "archived "+strconv.Itoa(i), ts, ts, done, ts); err != nil {
			t.Fatal(err)
		}
	}
	// A garbage value alongside the legacy ones: not the old writer's
	// shape, so the migration must leave it alone both times.
	if _, err := handle.ExecContext(ctx,
		`INSERT INTO notes_nodes (id, user_id, parent_id, position, title, note, created_at, updated_at, archived_at)
		 VALUES (100, ?, NULL, 100, 'garbage', '', 'not-a-timestamp', 'not-a-timestamp', 'not-a-timestamp')`,
		alice.ID); err != nil {
		t.Fatal(err)
	}

	if _, err := db.Apply(ctx, handle, ms); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	// The garbage row is left alone by the first run.
	var garbageCreated, garbageUpdated, garbageArchived string
	if err := handle.QueryRowContext(ctx,
		`SELECT created_at, updated_at, archived_at FROM notes_nodes WHERE id = 100`).Scan(
		&garbageCreated, &garbageUpdated, &garbageArchived); err != nil {
		t.Fatal(err)
	}
	for name, got := range map[string]string{"created_at": garbageCreated, "updated_at": garbageUpdated, "archived_at": garbageArchived} {
		if got != "not-a-timestamp" {
			t.Errorf("garbage %s after the first run = %q, want unchanged", name, got)
		}
	}

	snapshot := func() map[string]string {
		rows := map[string]string{}
		add := func(col string) {
			r, err := handle.QueryContext(ctx, `SELECT id, `+col+` FROM notes_nodes ORDER BY id`)
			if err != nil {
				t.Fatal(err)
			}
			defer r.Close()
			for r.Next() {
				var k int64
				var val sql.NullString
				if err := r.Scan(&k, &val); err != nil {
					t.Fatal(err)
				}
				rows[col+"#"+strconv.FormatInt(k, 10)] = val.String + "|" + strconv.FormatBool(val.Valid)
			}
			if err := r.Err(); err != nil {
				t.Fatal(err)
			}
		}
		add("created_at")
		add("updated_at")
		add("done_at")
		add("archived_at")
		return rows
	}

	before := snapshot()

	// Re-run notes:0007's SQL directly, a second time.
	if _, err := handle.ExecContext(ctx, migrationSQL(t, ms, "0007")); err != nil {
		t.Fatalf("second run: %v", err)
	}

	after := snapshot()
	if len(before) != len(after) {
		t.Fatalf("snapshot sizes differ: before=%d after=%d", len(before), len(after))
	}
	for k, v := range before {
		if after[k] != v {
			t.Errorf("%s changed on second run: before=%q after=%q", k, v, after[k])
		}
	}
}
