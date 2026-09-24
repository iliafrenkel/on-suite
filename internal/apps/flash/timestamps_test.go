package flash_test

import (
	"context"
	"database/sql"
	"path/filepath"
	"slices"
	"strconv"
	"testing"
	"time"

	"github.com/iliafrenkel/on-suite/internal/apps/flash"
	"github.com/iliafrenkel/on-suite/internal/apptest"
	"github.com/iliafrenkel/on-suite/internal/platform/auth"
	"github.com/iliafrenkel/on-suite/internal/platform/db"
)

// flashLegacyTimes are values as time.RFC3339Nano wrote them before #356,
// all inside one second and listed in time order. As text they sort
// differently ('Z' sorts after every digit), which is the bug.
var flashLegacyTimes = []string{
	"2026-09-25T12:00:05Z",
	"2026-09-25T12:00:05.123456789Z",
	"2026-09-25T12:00:05.244Z",
	"2026-09-25T12:00:05.244124Z",
	"2026-09-25T12:00:05.5Z",
}

// openFlashThrough is newFixture with Flash's schema stopped after lastID,
// so a test can seed rows the way an older binary wrote them. It returns
// every migration for the test to finish applying.
func openFlashThrough(t *testing.T, lastID string) (*sql.DB, []db.Migration, auth.User, auth.User) {
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
	app, err := db.Collect(flash.ID, flash.Migrations())
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
	users := auth.NewStore(handle)
	alice, err := users.CreateUser(ctx, "alice", apptest.PasswordHash, true)
	if err != nil {
		t.Fatal(err)
	}
	bob, err := users.CreateUser(ctx, "bob", apptest.PasswordHash, false)
	if err != nil {
		t.Fatal(err)
	}
	return handle, append(platform, app...), alice, bob
}

func flashRewritten(t *testing.T, legacy string) string {
	t.Helper()
	parsed, err := db.ParseTime(legacy)
	if err != nil {
		t.Fatalf("setup: %q does not parse: %v", legacy, err)
	}
	return db.FormatTime(parsed)
}

// orNull is legacy on odd rows and NULL on even ones, so every nullable
// column is checked both ways.
func orNull(i int, legacy string) any {
	if i%2 == 0 {
		return nil
	}
	return legacy
}

func TestFixedWidthMigrationRewritesFlashTimestamps(t *testing.T) {
	handle, ms, alice, bob := openFlashThrough(t, "0011")
	ctx := context.Background()

	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := handle.ExecContext(ctx, query, args...); err != nil {
			t.Fatalf("%s: %v", query, err)
		}
	}
	for i, ts := range flashLegacyTimes {
		id := strconv.Itoa(i)
		exec(`INSERT INTO flash_decks (id, user_id, name, created_at, snoozed_until) VALUES (?, ?, ?, ?, ?)`,
			i+1, alice.ID, "d"+id, ts, orNull(i, ts))
		exec(`INSERT INTO flash_cards (id, deck_id, user_id, card_type, front, created_at) VALUES (?, 1, ?, 'basic', ?, ?)`,
			i+1, alice.ID, "c"+id, ts)
		exec(`INSERT INTO flash_card_state (user_id, card_id, state, due_at, stability, difficulty,
		        scheduled_days, reps, lapses, remaining_steps, last_review_at, log_due, log_review)
		      VALUES (?, ?, 'review', ?, 1, 5, 1, 1, 0, 0, ?, ?, ?)`,
			alice.ID, i+1, ts, orNull(i, ts), orNull(i, ts), orNull(i, ts))
		exec(`INSERT INTO flash_media (hash, kind, fetched_at) VALUES (?, 'image', ?)`, "h"+id, orNull(i, ts))
		exec(`INSERT INTO flash_shares (id, deck_id, from_user_id, to_user_id, status, created_at, responded_at)
		      VALUES (?, 1, ?, ?, 'pending', ?, ?)`, i+1, alice.ID, bob.ID, ts, orNull(i, ts))
	}
	exec(`INSERT INTO flash_review_counts (user_id, deck_id, day, new_count) VALUES (?, 1, '2026-09-25', 3)`, alice.ID)

	if _, err := db.Apply(ctx, handle, ms); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	for i, ts := range flashLegacyTimes {
		want := flashRewritten(t, ts)
		key := i + 1
		for _, c := range []struct {
			query    string
			arg      any
			nullable bool
		}{
			{`SELECT created_at FROM flash_decks WHERE id = ?`, key, false},
			{`SELECT snoozed_until FROM flash_decks WHERE id = ?`, key, true},
			{`SELECT created_at FROM flash_cards WHERE id = ?`, key, false},
			{`SELECT due_at FROM flash_card_state WHERE card_id = ?`, key, false},
			{`SELECT last_review_at FROM flash_card_state WHERE card_id = ?`, key, true},
			{`SELECT log_due FROM flash_card_state WHERE card_id = ?`, key, true},
			{`SELECT log_review FROM flash_card_state WHERE card_id = ?`, key, true},
			{`SELECT fetched_at FROM flash_media WHERE hash = ?`, "h" + strconv.Itoa(i), true},
			{`SELECT created_at FROM flash_shares WHERE id = ?`, key, false},
			{`SELECT responded_at FROM flash_shares WHERE id = ?`, key, true},
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

	var day string
	var newCount int
	if err := handle.QueryRowContext(ctx,
		`SELECT day, new_count FROM flash_review_counts WHERE deck_id = 1`).Scan(&day, &newCount); err != nil {
		t.Fatal(err)
	}
	if day != "2026-09-25" || newCount != 3 {
		t.Errorf("flash_review_counts row = (%q, %d), want the date-only day untouched", day, newCount)
	}

	// The store reads the rewritten rows, and text order is time order.
	decks, err := flash.NewStore(handle).ListDecks(ctx, alice.ID)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, d := range decks {
		names = append(names, d.Name)
	}
	if want := []string{"d4", "d3", "d2", "d1", "d0"}; !slices.Equal(names, want) {
		t.Errorf("ListDecks (newest first) = %v, want %v", names, want)
	}
}

// migrationSQL returns the body of one flash migration.
func migrationSQL(t *testing.T, ms []db.Migration, id string) string {
	t.Helper()
	for _, m := range ms {
		if m.ID == id {
			return m.SQL
		}
	}
	t.Fatalf("no flash migration %s", id)
	return ""
}

// TestFixedWidthMigrationIsIdempotent runs the flash:0012 migration SQL a
// second time (RowsAffected only reflects the last statement in a
// multi-statement Exec, so it can't tell us this on its own) and checks
// every rewritten column, plus the odd/garbage values seeded alongside them,
// comes out identical both times.
func TestFixedWidthMigrationIsIdempotent(t *testing.T) {
	handle, ms, alice, bob := openFlashThrough(t, "0011")
	ctx := context.Background()

	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := handle.ExecContext(ctx, query, args...); err != nil {
			t.Fatalf("%s: %v", query, err)
		}
	}
	for i, ts := range flashLegacyTimes {
		id := strconv.Itoa(i)
		exec(`INSERT INTO flash_decks (id, user_id, name, created_at, snoozed_until) VALUES (?, ?, ?, ?, ?)`,
			i+1, alice.ID, "d"+id, ts, orNull(i, ts))
		exec(`INSERT INTO flash_cards (id, deck_id, user_id, card_type, front, created_at) VALUES (?, 1, ?, 'basic', ?, ?)`,
			i+1, alice.ID, "c"+id, ts)
		exec(`INSERT INTO flash_card_state (user_id, card_id, state, due_at, stability, difficulty,
		        scheduled_days, reps, lapses, remaining_steps, last_review_at, log_due, log_review)
		      VALUES (?, ?, 'review', ?, 1, 5, 1, 1, 0, 0, ?, ?, ?)`,
			alice.ID, i+1, ts, orNull(i, ts), orNull(i, ts), orNull(i, ts))
		exec(`INSERT INTO flash_media (hash, kind, fetched_at) VALUES (?, 'image', ?)`, "h"+id, orNull(i, ts))
		exec(`INSERT INTO flash_shares (id, deck_id, from_user_id, to_user_id, status, created_at, responded_at)
		      VALUES (?, 1, ?, ?, 'pending', ?, ?)`, i+1, alice.ID, bob.ID, ts, orNull(i, ts))
	}
	// A garbage value alongside the legacy ones: not the old writer's
	// shape, so the migration must leave it alone both times.
	exec(`INSERT INTO flash_decks (id, user_id, name, created_at, snoozed_until) VALUES (?, ?, ?, ?, ?)`,
		100, alice.ID, "garbage", "not-a-timestamp", nil)
	exec(`INSERT INTO flash_review_counts (user_id, deck_id, day, new_count) VALUES (?, 1, '2026-09-25', 3)`, alice.ID)

	if _, err := db.Apply(ctx, handle, ms); err != nil {
		t.Fatalf("Apply: %v", err)
	}

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
		}
		add("flash_decks", "id", "created_at")
		add("flash_decks", "id", "snoozed_until")
		add("flash_cards", "id", "created_at")
		add("flash_card_state", "card_id", "due_at")
		add("flash_card_state", "card_id", "last_review_at")
		add("flash_card_state", "card_id", "log_due")
		add("flash_card_state", "card_id", "log_review")
		add("flash_media", "hash", "fetched_at")
		add("flash_shares", "id", "created_at")
		add("flash_shares", "id", "responded_at")
		return rows
	}

	before := snapshot()

	// Re-run flash:0012's SQL directly, a second time.
	if _, err := handle.ExecContext(ctx, migrationSQL(t, ms, "0012")); err != nil {
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

// TestNewCardsQueueInCreationOrderWithinOneSecond is the flake behind #356,
// made deterministic: n1 is created 124µs before n2 in the same second, at
// the instants RFC3339Nano wrote as "…12.244Z" and "…12.244124Z". The
// queue's head must be n1, and DueQueue and QueueFront must agree.
func TestNewCardsQueueInCreationOrderWithinOneSecond(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	second := time.Date(2026, 9, 24, 11, 0, 12, 0, time.UTC)

	f.store.SetClock(func() time.Time { return second })
	d := qDeck(t, f, "A")
	f.store.SetClock(func() time.Time { return second.Add(244 * time.Millisecond) })
	qCard(t, f, d.ID, "n1")
	f.store.SetClock(func() time.Time { return second.Add(244124 * time.Microsecond) })
	qCard(t, f, d.ID, "n2")

	now := second.Add(time.Hour)
	queue, err := f.store.DueQueue(ctx, f.alice.ID, nil, now)
	if err != nil {
		t.Fatal(err)
	}
	var fronts []string
	for _, q := range queue {
		fronts = append(fronts, q.Card.Front)
	}
	if want := []string{"n1", "n2"}; !slices.Equal(fronts, want) {
		t.Errorf("DueQueue = %v, want %v", fronts, want)
	}
	front, err := f.store.QueueFront(ctx, f.alice.ID, nil, now)
	if err != nil {
		t.Fatal(err)
	}
	if !front.HasHead || front.Head.Card.Front != "n1" {
		t.Errorf("QueueFront head = %+v, want n1", front.Head.Card)
	}

	cards, err := f.store.ListCards(ctx, f.alice.ID, d.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(cards) != 2 || cards[0].Front != "n2" || cards[1].Front != "n1" {
		t.Errorf("ListCards (newest first) = %+v, want [n2 n1]", cards)
	}
}

func TestListDecksNewestFirstWithinOneSecond(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	second := time.Date(2026, 9, 24, 11, 0, 12, 0, time.UTC)

	f.store.SetClock(func() time.Time { return second.Add(244 * time.Millisecond) })
	qDeck(t, f, "older")
	f.store.SetClock(func() time.Time { return second.Add(244124 * time.Microsecond) })
	qDeck(t, f, "newer")

	decks, err := f.store.ListDecks(ctx, f.alice.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(decks) != 2 || decks[0].Name != "newer" || decks[1].Name != "older" {
		t.Errorf("ListDecks = %+v, want [newer older]", decks)
	}
}

// TestImportedCardsKeepTheirOrderWithinOneSecond: ImportDeck stamps each
// card with st.now() inside one transaction, microseconds apart, so the
// whole deck usually lands inside one second. Its new cards must queue in
// the order they were written.
func TestImportedCardsKeepTheirOrderWithinOneSecond(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	second := time.Date(2026, 9, 24, 11, 0, 12, 0, time.UTC)
	ticks := []time.Time{
		second,                                    // the deck
		second.Add(244 * time.Millisecond),        // q1
		second.Add(244124 * time.Microsecond),     // q2
		second.Add(245 * time.Millisecond),        // q3
		second.Add(245 * time.Millisecond).Add(1), // anything after
	}
	calls := 0
	f.store.SetClock(func() time.Time {
		at := ticks[min(calls, len(ticks)-1)]
		calls++
		return at
	})

	parsed, err := flash.ParseImport(`{
		"deck": {"name": "Imported"},
		"cards": [
			{"type": "basic", "front": "q1", "back": "a"},
			{"type": "basic", "front": "q2", "back": "a"},
			{"type": "basic", "front": "q3", "back": "a"}
		]
	}`, "json")
	if err != nil {
		t.Fatal(err)
	}
	deck, err := f.store.ImportDeck(ctx, f.alice.ID, parsed.Name, parsed.Description, parsed.Cards)
	if err != nil {
		t.Fatal(err)
	}

	queue, err := f.store.DueQueue(ctx, f.alice.ID, &deck.ID, second.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	var fronts []string
	for _, q := range queue {
		fronts = append(fronts, q.Card.Front)
	}
	if want := []string{"q1", "q2", "q3"}; !slices.Equal(fronts, want) {
		t.Errorf("imported deck queues as %v, want %v", fronts, want)
	}
}

// TestReviewDueHalfASecondAgoIsDue: due_at <= ? compares stored text. A
// card due on a whole second must be in the queue half a second later —
// with RFC3339Nano, "…:00Z" <= "…:00.5Z" was false.
func TestReviewDueHalfASecondAgoIsDue(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	d := qDeck(t, f, "A")
	c := qCard(t, f, d.ID, "review me")

	gradedAt := time.Date(2026, 9, 24, 11, 0, 0, 0, time.UTC)
	sched, err := f.store.GradeCard(ctx, f.alice.ID, c.ID, flash.RatingAgain, gradedAt)
	if err != nil {
		t.Fatal(err)
	}
	if sched.DueAt.Nanosecond() != 0 {
		t.Fatalf("setup: DueAt %v is not on a whole second; this test needs one", sched.DueAt)
	}

	queue, err := f.store.DueQueue(ctx, f.alice.ID, &d.ID, sched.DueAt.Add(500*time.Millisecond))
	if err != nil {
		t.Fatal(err)
	}
	if len(queue) != 1 || queue[0].Card.ID != c.ID || queue[0].IsNew {
		t.Errorf("DueQueue half a second after DueAt = %+v, want the one review card", queue)
	}
}
