package reader_test

import (
	"context"
	"database/sql"
	"path/filepath"
	"slices"
	"strconv"
	"testing"
	"time"

	"github.com/iliafrenkel/on-suite/internal/apps/reader"
	"github.com/iliafrenkel/on-suite/internal/apptest"
	"github.com/iliafrenkel/on-suite/internal/platform/auth"
	"github.com/iliafrenkel/on-suite/internal/platform/db"
)

// readerLegacyTimes are values as time.RFC3339Nano wrote them before #356,
// all inside one second and listed in time order. As text they sort
// differently ('Z' sorts after every digit), which is the bug.
var readerLegacyTimes = []string{
	"2026-09-25T12:00:05Z",
	"2026-09-25T12:00:05.123456789Z",
	"2026-09-25T12:00:05.244Z",
	"2026-09-25T12:00:05.244124Z",
	"2026-09-25T12:00:05.5Z",
}

// openReaderThrough is newStoreFixture with Reader's schema stopped after
// lastID, so a test can seed rows the way an older binary wrote them. It
// returns every migration for the test to finish applying.
func openReaderThrough(t *testing.T, lastID string) (*sql.DB, []db.Migration, auth.User) {
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
	app, err := db.Collect(reader.ID, reader.Migrations())
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

// readerOrNull is legacy on odd rows and NULL on even ones, so every
// nullable column is checked both ways.
func readerOrNull(i int, legacy string) any {
	if i%2 == 0 {
		return nil
	}
	return legacy
}

func TestFixedWidthMigrationRewritesReaderTimestamps(t *testing.T) {
	handle, ms, alice := openReaderThrough(t, "0009")
	ctx := context.Background()

	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := handle.ExecContext(ctx, query, args...); err != nil {
			t.Fatalf("%s: %v", query, err)
		}
	}
	for i, ts := range readerLegacyTimes {
		id := strconv.Itoa(i)
		key := i + 1
		url := "https://f" + id + ".example/feed.xml"
		exec(`INSERT INTO reader_feeds (id, url, resolved_url, title, site_url, last_fetch_at, next_fetch_at)
		      VALUES (?, ?, ?, ?, '', ?, ?)`, key, url, url, "feed "+id, readerOrNull(i, ts), ts)
		exec(`INSERT INTO reader_subs (id, user_id, feed_id, added_at) VALUES (?, ?, ?, ?)`, key, alice.ID, key, ts)
		exec(`INSERT INTO reader_items (id, feed_id, guid, url, title, published_at, fetched_at, full_fetched_at)
		      VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, key, key, "g"+id, "https://f"+id+".example/1", "item "+id, ts, ts, readerOrNull(i, ts))
		exec(`INSERT INTO reader_images (url_hash, src_url, fetched_at) VALUES (?, ?, ?)`, "i"+id, "https://img.example/"+id, readerOrNull(i, ts))
		exec(`INSERT INTO reader_item_state (user_id, item_id, read_at, starred_at) VALUES (?, ?, ?, ?)`,
			alice.ID, key, readerOrNull(i, ts), ts)
		exec(`INSERT INTO reader_feed_icons (url_hash, src_url, fetched_at) VALUES (?, ?, ?)`, "fi"+id, "https://f"+id+".example/favicon.ico", readerOrNull(i, ts))
	}
	exec(`INSERT INTO reader_daily_stats (day, user_id, fetched) VALUES ('2026-09-25', ?, 4)`, alice.ID)
	exec(`UPDATE reader_feeds SET last_modified = 'Thu, 25 Sep 2026 12:00:05 GMT' WHERE id = 1`)

	if _, err := db.Apply(ctx, handle, ms); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	for i, ts := range readerLegacyTimes {
		parsed, err := db.ParseTime(ts)
		if err != nil {
			t.Fatal(err)
		}
		want := db.FormatTime(parsed)
		id := strconv.Itoa(i)
		key := i + 1
		for _, c := range []struct {
			query    string
			arg      any
			nullable bool
		}{
			{`SELECT last_fetch_at FROM reader_feeds WHERE id = ?`, key, true},
			{`SELECT next_fetch_at FROM reader_feeds WHERE id = ?`, key, false},
			{`SELECT added_at FROM reader_subs WHERE id = ?`, key, false},
			{`SELECT published_at FROM reader_items WHERE id = ?`, key, false},
			{`SELECT fetched_at FROM reader_items WHERE id = ?`, key, false},
			{`SELECT full_fetched_at FROM reader_items WHERE id = ?`, key, true},
			{`SELECT fetched_at FROM reader_images WHERE url_hash = ?`, "i" + id, true},
			{`SELECT read_at FROM reader_item_state WHERE item_id = ?`, key, true},
			{`SELECT starred_at FROM reader_item_state WHERE item_id = ?`, key, false},
			{`SELECT fetched_at FROM reader_feed_icons WHERE url_hash = ?`, "fi" + id, true},
		} {
			var got sql.NullString
			if err := handle.QueryRowContext(ctx, c.query, c.arg).Scan(&got); err != nil {
				t.Fatalf("%s [%v]: %v", c.query, c.arg, err)
			}
			if c.nullable && i%2 == 0 {
				if got.Valid {
					t.Errorf("%s [%v] = %q, want NULL left alone", c.query, c.arg, got.String)
				}
				continue
			}
			if got.String != want {
				t.Errorf("%s [%v] = %q, want %q (from %q)", c.query, c.arg, got.String, want, ts)
			}
		}
	}

	var day, lastModified string
	if err := handle.QueryRowContext(ctx, `SELECT day FROM reader_daily_stats`).Scan(&day); err != nil {
		t.Fatal(err)
	}
	if err := handle.QueryRowContext(ctx, `SELECT last_modified FROM reader_feeds WHERE id = 1`).Scan(&lastModified); err != nil {
		t.Fatal(err)
	}
	if day != "2026-09-25" || lastModified != "Thu, 25 Sep 2026 12:00:05 GMT" {
		t.Errorf("day = %q, last_modified = %q; want both untouched", day, lastModified)
	}

	st := reader.NewStore(handle)
	due, err := st.DueFeeds(ctx, time.Date(2026, 9, 25, 12, 0, 6, 0, time.UTC), 10)
	if err != nil {
		t.Fatal(err)
	}
	var order []int64
	for _, f := range due {
		order = append(order, f.ID)
	}
	if want := []int64{1, 2, 3, 4, 5}; !slices.Equal(order, want) {
		t.Errorf("DueFeeds (oldest first) = %v, want %v", order, want)
	}

	// The backup keeps the text it always had.
	payload, err := st.Export(ctx, alice.ID)
	if err != nil {
		t.Fatal(err)
	}
	var starred []string
	for _, it := range payload.Starred {
		starred = append(starred, it.StarredAt)
		if it.PublishedAt != it.StarredAt {
			t.Errorf("exported published_at %q != starred_at %q; both were seeded from one value", it.PublishedAt, it.StarredAt)
		}
	}
	want := slices.Clone(readerLegacyTimes)
	slices.Reverse(want) // most recently starred first
	if !slices.Equal(starred, want) {
		t.Errorf("exported starred_at = %v, want %v", starred, want)
	}
}

// migrationSQL returns the body of one reader migration.
func migrationSQL(t *testing.T, ms []db.Migration, id string) string {
	t.Helper()
	for _, m := range ms {
		if m.ID == id {
			return m.SQL
		}
	}
	t.Fatalf("no reader migration %s", id)
	return ""
}

// TestFixedWidthMigrationIsIdempotent runs the reader:0010 migration SQL a
// second time (RowsAffected only reflects the last statement in a
// multi-statement Exec, so it can't tell us this on its own) and checks
// every rewritten column, plus the odd/garbage values seeded alongside
// them, comes out identical both times.
func TestFixedWidthMigrationIsIdempotent(t *testing.T) {
	handle, ms, alice := openReaderThrough(t, "0009")
	ctx := context.Background()

	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := handle.ExecContext(ctx, query, args...); err != nil {
			t.Fatalf("%s: %v", query, err)
		}
	}
	for i, ts := range readerLegacyTimes {
		id := strconv.Itoa(i)
		key := i + 1
		url := "https://f" + id + ".example/feed.xml"
		exec(`INSERT INTO reader_feeds (id, url, resolved_url, title, site_url, last_fetch_at, next_fetch_at)
		      VALUES (?, ?, ?, ?, '', ?, ?)`, key, url, url, "feed "+id, readerOrNull(i, ts), ts)
		exec(`INSERT INTO reader_subs (id, user_id, feed_id, added_at) VALUES (?, ?, ?, ?)`, key, alice.ID, key, ts)
		exec(`INSERT INTO reader_items (id, feed_id, guid, url, title, published_at, fetched_at, full_fetched_at)
		      VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, key, key, "g"+id, "https://f"+id+".example/1", "item "+id, ts, ts, readerOrNull(i, ts))
		exec(`INSERT INTO reader_images (url_hash, src_url, fetched_at) VALUES (?, ?, ?)`, "i"+id, "https://img.example/"+id, readerOrNull(i, ts))
		exec(`INSERT INTO reader_item_state (user_id, item_id, read_at, starred_at) VALUES (?, ?, ?, ?)`,
			alice.ID, key, readerOrNull(i, ts), ts)
		exec(`INSERT INTO reader_feed_icons (url_hash, src_url, fetched_at) VALUES (?, ?, ?)`, "fi"+id, "https://f"+id+".example/favicon.ico", readerOrNull(i, ts))
	}
	// A garbage value alongside the legacy ones: not the old writer's shape,
	// so the migration must leave it alone both times.
	exec(`INSERT INTO reader_feeds (id, url, resolved_url, title, site_url, last_fetch_at, next_fetch_at)
	      VALUES (100, 'https://garbage.example/feed.xml', 'https://garbage.example/feed.xml', 'garbage', '', ?, ?)`,
		"not-a-timestamp", "not-a-timestamp")
	exec(`INSERT INTO reader_daily_stats (day, user_id, fetched) VALUES ('2026-09-25', ?, 4)`, alice.ID)

	if _, err := db.Apply(ctx, handle, ms); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	// The garbage feed is left alone by the first run.
	var garbageLastFetch, garbageNextFetch string
	if err := handle.QueryRowContext(ctx,
		`SELECT last_fetch_at, next_fetch_at FROM reader_feeds WHERE id = 100`).Scan(&garbageLastFetch, &garbageNextFetch); err != nil {
		t.Fatal(err)
	}
	if garbageLastFetch != "not-a-timestamp" {
		t.Errorf("garbage last_fetch_at after the first run = %q, want unchanged", garbageLastFetch)
	}
	if garbageNextFetch != "not-a-timestamp" {
		t.Errorf("garbage next_fetch_at after the first run = %q, want unchanged", garbageNextFetch)
	}

	snapshot := func() map[string]string {
		rows := map[string]string{}
		// keyExpr identifies each row by its real primary key: reader_feeds,
		// reader_subs and reader_items use their ordinary integer id;
		// reader_images and reader_feed_icons use url_hash; and
		// reader_item_state (a WITHOUT ROWID table) uses its (user_id,
		// item_id) composite key. url_hash and (user_id, item_id) are the
		// logical keys for those tables regardless of rowid, so snapshotting
		// by anything else would not reliably track the same logical row.
		add := func(table, keyExpr, col string) {
			r, err := handle.QueryContext(ctx, `SELECT `+keyExpr+` AS k, `+col+` FROM `+table+` ORDER BY k`)
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
		add("reader_feeds", "id", "last_fetch_at")
		add("reader_feeds", "id", "next_fetch_at")
		add("reader_subs", "id", "added_at")
		add("reader_items", "id", "published_at")
		add("reader_items", "id", "fetched_at")
		add("reader_items", "id", "full_fetched_at")
		// reader_images: url_hash is the real primary key.
		add("reader_images", "url_hash", "fetched_at")
		// reader_item_state is WITHOUT ROWID with a (user_id, item_id)
		// primary key.
		add("reader_item_state", "user_id || '|' || item_id", "read_at")
		add("reader_item_state", "user_id || '|' || item_id", "starred_at")
		// reader_feed_icons: url_hash is the real primary key.
		add("reader_feed_icons", "url_hash", "fetched_at")
		return rows
	}

	before := snapshot()

	// Re-run reader:0010's SQL directly, a second time.
	if _, err := handle.ExecContext(ctx, migrationSQL(t, ms, "0010")); err != nil {
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

// TestDueFeedsWithinTheSameSecond is #356 for Reader: a feed due on a whole
// second is due half a second later. With RFC3339Nano, "…:00Z" <= "…:00.5Z"
// was false as text.
func TestDueFeedsWithinTheSameSecond(t *testing.T) {
	f := newStoreFixture(t)
	ctx := context.Background()
	sub, err := f.store.Subscribe(ctx, f.alice.ID, "https://a.example/feed.xml", nil)
	if err != nil {
		t.Fatal(err)
	}
	next := time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)
	if err := f.store.SaveFetchResult(ctx, reader.FetchResult{
		FeedID: sub.FeedID, ResolvedURL: "https://a.example/feed.xml", Status: 200,
		FetchedAt: next.Add(-time.Hour), NextFetchAt: next,
	}); err != nil {
		t.Fatal(err)
	}

	due, err := f.store.DueFeeds(ctx, next.Add(500*time.Millisecond), 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(due) != 1 || due[0].ID != sub.FeedID {
		t.Errorf("DueFeeds half a second after next_fetch_at = %+v, want the feed", due)
	}
}

// TestExportKeepsTimestampText pins the backup format: the database stores
// db.TimeLayout since #356, but `onsuite export` still writes the
// RFC3339Nano text it always did.
func TestExportKeepsTimestampText(t *testing.T) {
	f := newStoreFixture(t)
	ctx := context.Background()
	sub, err := f.store.Subscribe(ctx, f.alice.ID, "https://a.example/feed.xml", nil)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC() // after Subscribe's own added_at, so the item is visible
	if _, err := f.store.SaveItems(ctx, sub.FeedID, []reader.ParsedItem{
		{GUID: "a", Title: "Saved", URL: "https://a.example/1", PublishedAt: time.Date(2026, 9, 20, 8, 0, 0, 0, time.UTC)},
	}, now); err != nil {
		t.Fatal(err)
	}
	items, err := f.store.ItemsForScope(ctx, f.alice.ID, reader.ScopeAll, 0, reader.FilterAll, "", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("setup: %d items, want 1", len(items))
	}
	if err := f.store.SetStarred(ctx, f.alice.ID, items[0].ID, true,
		time.Date(2026, 9, 21, 9, 30, 0, 500_000_000, time.UTC)); err != nil {
		t.Fatal(err)
	}

	var stored string
	if err := f.db.QueryRowContext(ctx,
		`SELECT starred_at FROM reader_item_state WHERE item_id = ?`, items[0].ID).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if stored != "2026-09-21T09:30:00.500000000Z" {
		t.Fatalf("stored starred_at = %q, want db.TimeLayout", stored)
	}

	payload, err := f.store.Export(ctx, f.alice.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(payload.Starred) != 1 {
		t.Fatalf("exported %d starred items, want 1", len(payload.Starred))
	}
	got := payload.Starred[0]
	if got.PublishedAt != "2026-09-20T08:00:00Z" || got.StarredAt != "2026-09-21T09:30:00.5Z" {
		t.Errorf("exported published_at = %q, starred_at = %q; want %q and %q",
			got.PublishedAt, got.StarredAt, "2026-09-20T08:00:00Z", "2026-09-21T09:30:00.5Z")
	}
}
