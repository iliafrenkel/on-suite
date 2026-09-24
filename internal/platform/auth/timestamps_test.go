package auth

import (
	"context"
	"database/sql"
	"math/rand/v2"
	"path/filepath"
	"slices"
	"strconv"
	"testing"
	"time"

	"github.com/iliafrenkel/on-suite/internal/platform/db"
)

// legacyTimes are values as time.RFC3339Nano wrote them before #356, all
// inside one second, listed in time order. As text they sort in a
// different order ('Z' sorts after every digit), which is the bug.
var legacyTimes = []string{
	"2026-09-25T12:00:05Z",
	"2026-09-25T12:00:05.123456789Z",
	"2026-09-25T12:00:05.244Z",
	"2026-09-25T12:00:05.244124Z",
	"2026-09-25T12:00:05.5Z",
}

// openPlatformThrough migrates a fresh database only up to and including
// lastID, so a test can seed rows the way an older binary wrote them, and
// returns every platform migration for the test to finish applying.
func openPlatformThrough(t *testing.T, lastID string) (*sql.DB, []db.Migration) {
	t.Helper()
	handle, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = handle.Close() })
	ms, err := db.Collect(Namespace, Migrations())
	if err != nil {
		t.Fatal(err)
	}
	var early []db.Migration
	for _, m := range ms {
		if m.ID <= lastID {
			early = append(early, m)
		}
	}
	if _, err := db.Apply(context.Background(), handle, early); err != nil {
		t.Fatal(err)
	}
	return handle, ms
}

// migrationSQL returns the body of one platform migration.
func migrationSQL(t *testing.T, ms []db.Migration, id string) string {
	t.Helper()
	for _, m := range ms {
		if m.ID == id {
			return m.SQL
		}
	}
	t.Fatalf("no platform migration %s", id)
	return ""
}

func wantRewritten(t *testing.T, legacy string) string {
	t.Helper()
	parsed, err := db.ParseTime(legacy)
	if err != nil {
		t.Fatalf("setup: %q does not parse: %v", legacy, err)
	}
	return db.FormatTime(parsed)
}

func TestFixedWidthMigrationRewritesPlatformTimestamps(t *testing.T) {
	handle, ms := openPlatformThrough(t, "0001")
	ctx := context.Background()

	for i, ts := range legacyTimes {
		id := strconv.Itoa(i)
		if _, err := handle.ExecContext(ctx,
			`INSERT INTO users (id, username, username_fold, password_hash, created_at)
			 VALUES (?, ?, ?, 'x', ?)`, i+1, "u"+id, "u"+id, ts); err != nil {
			t.Fatal(err)
		}
		if _, err := handle.ExecContext(ctx,
			`INSERT INTO sessions (id, user_id, created_at, expires_at) VALUES (?, ?, ?, ?)`,
			"s"+id, i+1, ts, ts); err != nil {
			t.Fatal(err)
		}
		if _, err := handle.ExecContext(ctx,
			`INSERT INTO schema_migrations (key, namespace, id, name, applied_at)
			 VALUES (?, 'legacy', ?, 'seeded', ?)`, "legacy:"+id, id, ts); err != nil {
			t.Fatal(err)
		}
	}

	n, err := db.Apply(ctx, handle, ms)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if n != len(ms)-1 {
		t.Fatalf("applied %d migrations, want %d", n, len(ms)-1)
	}

	for i, ts := range legacyTimes {
		id := strconv.Itoa(i)
		want := wantRewritten(t, ts)
		for _, q := range []struct{ query, arg string }{
			{`SELECT created_at FROM users WHERE username = ?`, "u" + id},
			{`SELECT created_at FROM sessions WHERE id = ?`, "s" + id},
			{`SELECT expires_at FROM sessions WHERE id = ?`, "s" + id},
			{`SELECT applied_at FROM schema_migrations WHERE key = ?`, "legacy:" + id},
		} {
			var got string
			if err := handle.QueryRowContext(ctx, q.query, q.arg).Scan(&got); err != nil {
				t.Fatal(err)
			}
			if got != want {
				t.Errorf("%s [%s] = %q, want %q (from %q)", q.query, q.arg, got, want, ts)
			}
		}
	}

	// Text order is now time order, which legacyTimes is listed in.
	rows, err := handle.QueryContext(ctx, `SELECT username FROM users ORDER BY created_at`)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	var order []string
	for rows.Next() {
		var u string
		if err := rows.Scan(&u); err != nil {
			t.Fatal(err)
		}
		order = append(order, u)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if want := []string{"u0", "u1", "u2", "u3", "u4"}; !slices.Equal(order, want) {
		t.Errorf("users by created_at = %v, want %v", order, want)
	}

	// The store reads the rewritten rows.
	u, err := NewStore(handle).UserByUsername(ctx, "u2")
	if err != nil {
		t.Fatal(err)
	}
	if want := time.Date(2026, 9, 25, 12, 0, 5, 244_000_000, time.UTC); !u.CreatedAt.Equal(want) {
		t.Errorf("CreatedAt = %v, want %v", u.CreatedAt, want)
	}
}

// TestFixedWidthMigrationMatchesFormatTime runs the migration's SQL over
// many random RFC3339Nano values and checks each against db.FormatTime, so
// the padding expression — repeated in every app's own 0002/0007/0010/0012
// — is proved on this one copy against every fraction length.
func TestFixedWidthMigrationMatchesFormatTime(t *testing.T) {
	handle, ms := openPlatformThrough(t, "0001")
	ctx := context.Background()
	if _, err := handle.ExecContext(ctx,
		`INSERT INTO users (id, username, username_fold, password_hash, created_at)
		 VALUES (1, 'u', 'u', 'x', '2026-09-25T12:00:05Z')`); err != nil {
		t.Fatal(err)
	}

	r := rand.New(rand.NewPCG(356, 3))
	base := time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)
	want := map[string]time.Time{}
	for i := range 2000 {
		ns := r.Int64N(int64(3 * time.Second))
		switch r.IntN(4) { // whole, milli- and microsecond values are common in practice
		case 0:
			ns -= ns % int64(time.Second)
		case 1:
			ns -= ns % int64(time.Millisecond)
		case 2:
			ns -= ns % int64(time.Microsecond)
		}
		at := base.Add(time.Duration(ns))
		id := "r" + strconv.Itoa(i)
		want[id] = at
		if _, err := handle.ExecContext(ctx,
			`INSERT INTO sessions (id, user_id, created_at, expires_at) VALUES (?, 1, ?, ?)`,
			id, at.Format(time.RFC3339Nano), at.Format(time.RFC3339Nano)); err != nil {
			t.Fatal(err)
		}
	}

	if _, err := db.Apply(ctx, handle, ms); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	rows, err := handle.QueryContext(ctx, `SELECT id, expires_at FROM sessions ORDER BY expires_at, id`)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	var prev time.Time
	for rows.Next() {
		var id, got string
		if err := rows.Scan(&id, &got); err != nil {
			t.Fatal(err)
		}
		if w := db.FormatTime(want[id]); got != w {
			t.Fatalf("session %s expires_at = %q, want %q", id, got, w)
		}
		if want[id].Before(prev) {
			t.Fatalf("session %s (%v) sorted after a later %v", id, want[id], prev)
		}
		prev = want[id]
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
}

// TestFixedWidthMigrationIsIdempotentAndLeavesOddValuesAlone runs the
// platform:0002 migration SQL a second time (RowsAffected only reflects the
// last statement in a multi-statement Exec, so it can't tell us this on its
// own) and checks every rewritten column, plus the odd/garbage values seeded
// alongside them, comes out identical both times.
func TestFixedWidthMigrationIsIdempotentAndLeavesOddValuesAlone(t *testing.T) {
	handle, ms := openPlatformThrough(t, "0001")
	ctx := context.Background()
	if _, err := handle.ExecContext(ctx,
		`INSERT INTO users (id, username, username_fold, password_hash, created_at)
		 VALUES (1, 'u', 'u', 'x', '2026-09-25T12:00:05.5Z')`); err != nil {
		t.Fatal(err)
	}
	odd := []string{
		"2026-09-25T22:00:05+10:00",      // an offset: never written, still parses
		"2026-09-25T12:00:05.12a4Z",      // not digits
		"2026-09-25T12:00:05.Z",          // an empty fraction
		"2026-09-25T12:00:05.123456789Z", // already fixed width
		"not a time",
	}
	for i, v := range odd {
		if _, err := handle.ExecContext(ctx,
			`INSERT INTO sessions (id, user_id, created_at, expires_at) VALUES (?, 1, ?, ?)`,
			"o"+strconv.Itoa(i), v, v); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.Apply(ctx, handle, ms); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	// The odd/garbage values are left alone by the first run, on both
	// rewritten columns of the row they were seeded into.
	for i, v := range odd {
		var created, expires string
		if err := handle.QueryRowContext(ctx,
			`SELECT created_at, expires_at FROM sessions WHERE id = ?`, "o"+strconv.Itoa(i)).Scan(&created, &expires); err != nil {
			t.Fatal(err)
		}
		if created != v {
			t.Errorf("odd value %q: created_at was rewritten to %q", v, created)
		}
		if expires != v {
			t.Errorf("odd value %q: expires_at was rewritten to %q", v, expires)
		}
	}
	var created string
	if err := handle.QueryRowContext(ctx, `SELECT created_at FROM users WHERE id = 1`).Scan(&created); err != nil {
		t.Fatal(err)
	}
	if created != "2026-09-25T12:00:05.500000000Z" {
		t.Errorf("created_at after the first run = %q", created)
	}

	// Snapshot every rewritten platform column, keyed by its real primary
	// key, re-run the migration SQL directly, and compare: a RowsAffected
	// check on a multi-statement Exec only reports the last UPDATE, so it
	// can't tell us the earlier ones stayed put too.
	snapshot := func() map[string]string {
		rows := map[string]string{}
		add := func(table, key, col string) {
			r, err := handle.QueryContext(ctx, `SELECT `+key+`, `+col+` FROM `+table+` ORDER BY `+key)
			if err != nil {
				t.Fatal(err)
			}
			defer r.Close()
			for r.Next() {
				var k string
				var val sql.NullString
				if err := r.Scan(&k, &val); err != nil {
					t.Fatal(err)
				}
				rows[table+"."+col+"#"+k] = val.String + "|" + strconv.FormatBool(val.Valid)
			}
			if err := r.Err(); err != nil {
				t.Fatal(err)
			}
		}
		add("users", "id", "created_at")
		add("sessions", "id", "created_at")
		add("sessions", "id", "expires_at")
		add("schema_migrations", "key", "applied_at")
		return rows
	}

	before := snapshot()

	// Re-run platform:0002's SQL directly, a second time.
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

// TestDeleteExpiredSessionsWithinTheSameSecond is #356 for sessions: a
// session that expired on a whole second must be swept half a second later.
// With RFC3339Nano, "...:00Z" <= "...:00.5Z" was false as text.
func TestDeleteExpiredSessionsWithinTheSameSecond(t *testing.T) {
	s, _ := newStore(t)
	ctx := context.Background()
	u, err := s.CreateUser(ctx, "ilia", "$argon2id$fake", false)
	if err != nil {
		t.Fatal(err)
	}

	created := time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)
	s.SetClock(func() time.Time { return created })
	sess, err := s.CreateSession(ctx, u.ID)
	if err != nil {
		t.Fatal(err)
	}

	s.SetClock(func() time.Time { return sess.ExpiresAt.Add(500 * time.Millisecond) })
	n, err := s.DeleteExpiredSessions(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("DeleteExpiredSessions removed %d sessions, want 1", n)
	}
}
