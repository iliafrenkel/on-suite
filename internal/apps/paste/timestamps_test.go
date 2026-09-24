package paste_test

import (
	"context"
	"database/sql"
	"path/filepath"
	"slices"
	"strconv"
	"testing"
	"time"

	"github.com/iliafrenkel/on-suite/internal/apps/paste"
	"github.com/iliafrenkel/on-suite/internal/apptest"
	"github.com/iliafrenkel/on-suite/internal/platform/auth"
	"github.com/iliafrenkel/on-suite/internal/platform/db"
)

// pasteLegacyTimes are values as time.RFC3339Nano wrote them before #356,
// all inside one second and listed in time order. As text they sort
// differently ('Z' sorts after every digit), which is the bug.
var pasteLegacyTimes = []string{
	"2026-09-25T12:00:05Z",
	"2026-09-25T12:00:05.123456789Z",
	"2026-09-25T12:00:05.244Z",
	"2026-09-25T12:00:05.244124Z",
	"2026-09-25T12:00:05.5Z",
}

// openPasteThrough is newFixture with Paste's schema stopped after lastID,
// so a test can seed rows the way an older binary wrote them. It returns
// every migration for the test to finish applying.
func openPasteThrough(t *testing.T, lastID string) (*sql.DB, []db.Migration, auth.User) {
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
	app, err := db.Collect(paste.ID, paste.Migrations())
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

func TestFixedWidthMigrationRewritesPasteTimestamps(t *testing.T) {
	handle, ms, alice := openPasteThrough(t, "0001")
	ctx := context.Background()

	for i, ts := range pasteLegacyTimes {
		if _, err := handle.ExecContext(ctx,
			`INSERT INTO paste_snippets (id, user_id, title, language, body, created_at)
			 VALUES (?, ?, ?, '', 'x', ?)`, i+1, alice.ID, "s"+strconv.Itoa(i), ts); err != nil {
			t.Fatal(err)
		}
	}

	if _, err := db.Apply(ctx, handle, ms); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	for i, ts := range pasteLegacyTimes {
		parsed, err := db.ParseTime(ts)
		if err != nil {
			t.Fatal(err)
		}
		var got string
		if err := handle.QueryRowContext(ctx,
			`SELECT created_at FROM paste_snippets WHERE id = ?`, i+1).Scan(&got); err != nil {
			t.Fatal(err)
		}
		if want := db.FormatTime(parsed); got != want {
			t.Errorf("snippet %d created_at = %q, want %q (from %q)", i+1, got, want, ts)
		}
	}

	list, err := paste.NewStore(handle).List(ctx, alice.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	var order []string
	for _, s := range list {
		order = append(order, s.Title)
	}
	if want := []string{"s4", "s3", "s2", "s1", "s0"}; !slices.Equal(order, want) {
		t.Errorf("List (newest first) = %v, want %v", order, want)
	}
}

// TestListNewestFirstWithinOneSecond is #356 for Paste: "second" is saved
// 124µs after "first", at the instants RFC3339Nano wrote as "…12.244Z" and
// "…12.244124Z", and must list first.
func TestListNewestFirstWithinOneSecond(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	at := time.Date(2026, 9, 24, 11, 0, 12, 0, time.UTC)

	f.store.SetClock(func() time.Time { return at.Add(244 * time.Millisecond) })
	if _, err := f.store.Create(ctx, f.alice.ID, "first", "", "one"); err != nil {
		t.Fatal(err)
	}
	f.store.SetClock(func() time.Time { return at.Add(244124 * time.Microsecond) })
	if _, err := f.store.Create(ctx, f.alice.ID, "second", "", "two"); err != nil {
		t.Fatal(err)
	}

	list, err := f.store.List(ctx, f.alice.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 || list[0].Title != "second" || list[1].Title != "first" {
		t.Errorf("List = %+v, want [second first]", list)
	}
}

func migrationSQL(t *testing.T, ms []db.Migration, id string) string {
	t.Helper()
	for _, m := range ms {
		if m.ID == id {
			return m.SQL
		}
	}
	t.Fatalf("no paste migration %s", id)
	return ""
}

// TestFixedWidthMigrationIsIdempotent runs the paste:0002 migration SQL a
// second time (RowsAffected only reflects the last statement in a
// multi-statement Exec, so it can't tell us this on its own) and checks
// every rewritten column, plus a garbage value seeded alongside them,
// comes out identical both times.
func TestFixedWidthMigrationIsIdempotent(t *testing.T) {
	handle, ms, alice := openPasteThrough(t, "0001")
	ctx := context.Background()

	for i, ts := range pasteLegacyTimes {
		if _, err := handle.ExecContext(ctx,
			`INSERT INTO paste_snippets (id, user_id, title, language, body, created_at)
			 VALUES (?, ?, ?, '', 'x', ?)`, i+1, alice.ID, "s"+strconv.Itoa(i), ts); err != nil {
			t.Fatal(err)
		}
	}
	// A garbage value alongside the legacy ones: not the old writer's
	// shape, so the migration must leave it alone both times.
	if _, err := handle.ExecContext(ctx,
		`INSERT INTO paste_snippets (id, user_id, title, language, body, created_at)
		 VALUES (100, ?, 'garbage', '', 'x', 'not-a-timestamp')`, alice.ID); err != nil {
		t.Fatal(err)
	}

	if _, err := db.Apply(ctx, handle, ms); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	snapshot := func() map[string]string {
		rows := map[string]string{}
		r, err := handle.QueryContext(ctx, `SELECT id, created_at FROM paste_snippets ORDER BY id`)
		if err != nil {
			t.Fatal(err)
		}
		defer r.Close()
		for r.Next() {
			var k int64
			var val string
			if err := r.Scan(&k, &val); err != nil {
				t.Fatal(err)
			}
			rows["created_at#"+strconv.FormatInt(k, 10)] = val
		}
		return rows
	}

	before := snapshot()

	// Re-run paste:0002's SQL directly, a second time.
	if _, err := handle.ExecContext(ctx, migrationSQL(t, ms, "0002")); err != nil {
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
