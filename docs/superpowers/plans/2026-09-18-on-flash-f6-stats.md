# ON Flash F6 — Stats Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `GET /flash/stats` page showing a review streak, 30-day retention rate, cards mastered, cards due today, a 30-day review-volume chart, and a per-deck breakdown table.

**Architecture:** One migration adds four per-rating daily counters to the existing `flash_review_counts` table (no new table, no cron job — unlike ON Reader's stats, which needs a persisted-and-backfilled daily table because Reader purges old rows; Flash never purges `flash_card_state`/`flash_review_counts`, so every stat is computed live). A new `stats.go` holds pure store queries; a new `handlers_stats.go` and `templates/stats.html` render the page, mirroring Reader's own stats-page shape (tiles + one SVG bar chart + accessible `<details>` table + per-row breakdown table).

**Tech Stack:** Go, `database/sql` against SQLite, the existing `internal/platform/app`/`internal/platform/web`/`internal/platform/render` stack, `apptest` for handler tests.

## Global Constraints

- Day keys are `"2006-01-02"` UTC, via the existing unexported `formatDay(t time.Time) string` in `review.go` — reuse it, don't redefine it.
- `flash_review_counts` is never purged, so every stat is computed live by querying it and `flash_card_state` directly — no new persisted daily-stats table, no backfill job (unlike Reader's `reader_daily_stats`).
- "Mastered" = `flash_card_state.state = 'review'` (the exact lowercase string FSRS state constant already used everywhere in this package) — no stability/day threshold on top.
- "Cards due today" counts only cards with an existing `flash_card_state` row whose `due_at <= now`, excluding cards in a currently-snoozed deck (`snoozed_until IS NULL OR snoozed_until <= now` — the same "not `.After(now)`" rule `Deck.IsSnoozed` already encodes). This does **not** include new (never-reviewed) cards — "due" means "due for review," not "eligible to be introduced," which is a separate, uncapped count from `DueQueue`'s budget-capped new-card allowance.
- Streak semantics (a genuine ambiguity in the design spec's own wording, resolved here explicitly): walk backward from **today**; if today has no review yet, that does not by itself break the streak — start the backward walk from **yesterday** instead, so a still-unbroken streak from previous days keeps showing correctly before the account has studied today. Only a full calendar day with zero reviews anywhere breaks the chain.
- Every new query function takes `now time.Time` as an explicit parameter (never reads `st.now()` internally) — this matches every existing Flash review/store function (`GradeCard`, `UndoLastGrade`, `DueQueue` all take `now` explicitly), a deliberate departure from Reader's own `DailyStats`, which hardcodes `time.Now()` — Flash's own convention wins since this package already threads `now` everywhere for testability.
- Retention rate window: 30 days. Cards-due-today: no window, current moment. Chart: 30 days, zero-filled for every calendar day including idle ones (mirroring Reader's own `DailyStats` zero-fill loop exactly).
- Route: `GET /flash/stats`, a literal single-segment path — safe alongside the existing `GET /{deckID}` wildcard route per this file's own established route-shape rules (a literal never conflicts with a same-position wildcard). Renders as its own full page (`a.deps.Render.Page`), not an HTMX fragment swap — matches Reader's own `/reader/stats`, which is a plain navigation target, not wired into any `hx-target`.
- Test fixture: `internal/apps/flash/deck_test.go`'s `newFixture(t) *fixture` (real SQLite, `alice`/`bob` accounts) for store-level tests; `apptest.NewServer(t, flash.New(), flash.NewStore)` for handler tests. Multi-day grading in tests is done by calling `Store.GradeCard`/`Store.UndoLastGrade` directly with hand-constructed `time.Date(...)` values — there is no reusable "seed N days of history" helper in this package yet, and this plan does not add one (each test's own loop is short enough not to need it).

---

### Task 1: Per-rating daily counters

**Files:**
- Create: `internal/apps/flash/migrations/0009_review_ratings.sql`
- Modify: `internal/apps/flash/review.go` (`bumpDailyCounts` and its two call sites, `GradeCard` and `UndoLastGrade`)
- Test: `internal/apps/flash/review_test.go` (extend)

**Interfaces:**
- Consumes: `RatingAgain`/`RatingHard`/`RatingGood`/`RatingEasy` (`fsrs.go`, values 1–4), `ErrInvalid` (`store.go`), the existing `flash_review_counts` table (`migrations/0004_review_state.sql`).
- Produces: `flash_review_counts.again_count`/`hard_count`/`good_count`/`easy_count` columns, and `bumpDailyCounts`'s new signature `(st *Store) bumpDailyCounts(ctx context.Context, tx *sql.Tx, userID, deckID int64, now time.Time, newDelta, reviewDelta, rating, ratingDelta int) error` — Task 2's `RetentionRate` reads these columns directly, no Go-level accessor needed beyond the raw columns.

- [ ] **Step 1: Write the migration**

```sql
-- internal/apps/flash/migrations/0009_review_ratings.sql
-- Four per-rating counters alongside flash_review_counts' existing
-- new_count/review_count, bumped on every grade (and un-bumped on undo) so
-- retention rate — the fraction of reviews NOT graded Again — can be
-- computed for any window without a separate per-review event log. Rows
-- written before this migration simply have all four at 0, which reads as
-- "no ratings recorded that day" — correct for genuinely idle days, and a
-- harmless, short-lived gap for the handful of pre-migration days given the
-- stats page's 30-day retention window.
ALTER TABLE flash_review_counts ADD COLUMN again_count INTEGER NOT NULL DEFAULT 0;
ALTER TABLE flash_review_counts ADD COLUMN hard_count  INTEGER NOT NULL DEFAULT 0;
ALTER TABLE flash_review_counts ADD COLUMN good_count  INTEGER NOT NULL DEFAULT 0;
ALTER TABLE flash_review_counts ADD COLUMN easy_count  INTEGER NOT NULL DEFAULT 0;
```

- [ ] **Step 2: Write the failing tests**

Read `internal/apps/flash/review_test.go`'s existing imports and the
`newFixture`/`fixture` type in `internal/apps/flash/deck_test.go` first (same
package, `flash_test`) — these tests use that exact fixture. Append:

```go
// append to internal/apps/flash/review_test.go

func ratingCount(t *testing.T, f *fixture, userID, deckID int64, day time.Time, column string) int {
	t.Helper()
	var n int
	err := f.db.QueryRowContext(context.Background(),
		`SELECT `+column+` FROM flash_review_counts WHERE user_id = ? AND deck_id = ? AND day = ?`,
		userID, deckID, day.UTC().Format("2006-01-02")).Scan(&n)
	if err != nil {
		t.Fatalf("read %s: %v", column, err)
	}
	return n
}

func TestGradeCardBumpsTheMatchingRatingCounter(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	deck, err := f.store.CreateDeck(ctx, f.alice.ID, "Spanish", "")
	if err != nil {
		t.Fatal(err)
	}
	card, err := f.store.CreateCard(ctx, f.alice.ID, deck.ID, flash.CardTypeBasic, "hola", "hello", "")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)

	if _, err := f.store.GradeCard(ctx, f.alice.ID, card.ID, flash.RatingAgain, now); err != nil {
		t.Fatal(err)
	}
	if got := ratingCount(t, f, f.alice.ID, deck.ID, now, "again_count"); got != 1 {
		t.Errorf("again_count = %d, want 1", got)
	}
	for _, col := range []string{"hard_count", "good_count", "easy_count"} {
		if got := ratingCount(t, f, f.alice.ID, deck.ID, now, col); got != 0 {
			t.Errorf("%s = %d, want 0", col, got)
		}
	}
}

func TestUndoLastGradeDecrementsTheMatchingRatingCounter(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	deck, err := f.store.CreateDeck(ctx, f.alice.ID, "Spanish", "")
	if err != nil {
		t.Fatal(err)
	}
	card, err := f.store.CreateCard(ctx, f.alice.ID, deck.ID, flash.CardTypeBasic, "hola", "hello", "")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)
	later := now.Add(time.Hour)

	if _, err := f.store.GradeCard(ctx, f.alice.ID, card.ID, flash.RatingEasy, now); err != nil {
		t.Fatal(err)
	}
	if _, undone, err := f.store.UndoLastGrade(ctx, f.alice.ID, card.ID, later); err != nil || !undone {
		t.Fatalf("UndoLastGrade: undone=%v, err=%v", undone, err)
	}
	if got := ratingCount(t, f, f.alice.ID, deck.ID, now, "easy_count"); got != 0 {
		t.Errorf("easy_count after undo = %d, want 0", got)
	}
}
```

- [ ] **Step 3: Run the tests to verify they fail**

Run: `go test ./internal/apps/flash/... -run 'TestGradeCardBumpsTheMatchingRatingCounter|TestUndoLastGradeDecrementsTheMatchingRatingCounter' -v`
Expected: FAIL — the migration from Step 1 already adds the columns, so the
column reads `0` where the test expects `1`: a plain assertion failure, not
a missing-column error, since `bumpDailyCounts` doesn't write the new
columns until Step 4.

- [ ] **Step 4: Extend `bumpDailyCounts` and its two call sites**

In `internal/apps/flash/review.go`, replace the existing `bumpDailyCounts` function:

```go
// bumpDailyCounts adds newDelta/reviewDelta (either can be negative, for
// UndoLastGrade) to userID's counters for deckID on now's calendar day,
// creating the row if this is the first count of the day. ratingDelta
// (+1 for a grade, -1 for undoing one) is added to whichever of the four
// per-rating columns rating names, so retention rate can be computed later
// without a separate per-review event log.
func (st *Store) bumpDailyCounts(ctx context.Context, tx *sql.Tx, userID, deckID int64, now time.Time, newDelta, reviewDelta, rating, ratingDelta int) error {
	column, err := ratingCountColumn(rating)
	if err != nil {
		return err
	}
	day := formatDay(now)
	// Ensure the row exists before applying the delta. This can't be a
	// single INSERT ... ON CONFLICT DO UPDATE that adds the deltas directly:
	// SQLite evaluates flash_review_counts' CHECK constraint against the row
	// that WOULD be inserted before it even considers the conflict, so a
	// negative delta (UndoLastGrade decrementing a counter) fails the check
	// even though the actual write is an UPDATE against an existing,
	// non-negative row. Inserting zeros first, then updating with the real
	// delta in a second statement, sidesteps that: the insert candidate is
	// always non-negative, and the UPDATE's CHECK is evaluated against the
	// real post-update row.
	if _, err := tx.ExecContext(ctx, `
        INSERT INTO flash_review_counts (user_id, deck_id, day, new_count, review_count)
        VALUES (?, ?, ?, 0, 0)
        ON CONFLICT (user_id, deck_id, day) DO NOTHING`,
		userID, deckID, day,
	); err != nil {
		return fmt.Errorf("flash: update daily counts: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
        UPDATE flash_review_counts SET
            new_count = new_count + ?,
            review_count = review_count + ?,
            `+column+` = `+column+` + ?
        WHERE user_id = ? AND deck_id = ? AND day = ?`,
		newDelta, reviewDelta, ratingDelta, userID, deckID, day,
	); err != nil {
		return fmt.Errorf("flash: update daily counts: %w", err)
	}
	return nil
}

// ratingCountColumn maps a rating constant to its flash_review_counts
// column. rating is always one of the four fixed RatingX constants from
// this package's own callers (GradeCard passes the rating it just
// validated; UndoLastGrade passes a rating it previously wrote itself), so
// this is not user-input-driven string building — it's a closed, four-way
// switch, the same shape share.go's resolveShare already uses for a column
// name selected from a fixed internal set.
func ratingCountColumn(rating int) (string, error) {
	switch rating {
	case RatingAgain:
		return "again_count", nil
	case RatingHard:
		return "hard_count", nil
	case RatingGood:
		return "good_count", nil
	case RatingEasy:
		return "easy_count", nil
	default:
		return "", fmt.Errorf("%w: %d is not a rating I know", ErrInvalid, rating)
	}
}
```

Update `GradeCard`'s call site (the line currently reading
`if err := st.bumpDailyCounts(ctx, tx, userID, deckID, now, newInc, reviewInc); err != nil {`):

```go
	if err := st.bumpDailyCounts(ctx, tx, userID, deckID, now, newInc, reviewInc, rating, 1); err != nil {
		return cardSchedule{}, err
	}
```

Update `UndoLastGrade`'s call site (the line currently reading
`if err := st.bumpDailyCounts(ctx, tx, userID, deckID, log.Review, newDec, reviewDec); err != nil {`):

```go
	if err := st.bumpDailyCounts(ctx, tx, userID, deckID, log.Review, newDec, reviewDec, log.Rating, -1); err != nil {
		return cardSchedule{}, false, err
	}
```

`log.Rating` is already available in `UndoLastGrade` at that point (scanned
by `scanCardStateWithLog` earlier in the same function, before the log
columns are cleared) — no new field or query needed.

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go test ./internal/apps/flash/... -run 'TestGradeCardBumpsTheMatchingRatingCounter|TestUndoLastGradeDecrementsTheMatchingRatingCounter' -v`
Expected: both PASS.

- [ ] **Step 6: Run the whole package's tests**

Run: `go test ./internal/apps/flash/... -race -count=1`
Expected: PASS — this also confirms `bumpDailyCounts`'s signature change didn't miss a call site (there are exactly two: `GradeCard` and `UndoLastGrade`, both updated above; a missed one would be a compile error, not a silent bug).

- [ ] **Step 7: Commit**

```bash
git add internal/apps/flash/migrations/0009_review_ratings.sql internal/apps/flash/review.go internal/apps/flash/review_test.go
git commit -m "feat(flash): F6 stats — per-rating daily counters"
```

---

### Task 2: Stats store queries

**Files:**
- Create: `internal/apps/flash/stats.go`
- Create: `internal/apps/flash/stats_test.go`

**Interfaces:**
- Consumes: `formatDay` (`review.go`, unexported, same package), `Deck`/`Store.ListDecks` (`deck.go`), `ErrNotFound`/`rowScanner` (`store.go`), `flash_review_counts`'s new rating columns (Task 1), `flash_card_state.state` (lowercase `"new"|"learning"|"review"|"relearning"`, `fsrs.go`).
- Produces: `(st *Store) Streak(ctx, userID int64, now time.Time) (int, error)`, `(st *Store) RetentionRate(ctx, userID int64, since time.Time) (float64, error)`, `(st *Store) CardsMastered(ctx, userID int64) (int, error)`, `(st *Store) CardsDueToday(ctx, userID int64, now time.Time) (int, error)`, `DayCount{Day time.Time; Count int}`, `(st *Store) DailyReviewCounts(ctx, userID int64, days int, now time.Time) ([]DayCount, error)`, `DeckLoad{Deck Deck; Mastered, Due, ReviewsLast30Days int}`, `(st *Store) PerDeckLoad(ctx, userID int64, now time.Time) ([]DeckLoad, error)` — Task 3's handler calls all of these.

- [ ] **Step 1: Write the failing tests**

```go
// internal/apps/flash/stats_test.go
package flash_test

import (
	"context"
	"testing"
	"time"

	"github.com/iliafrenkel/on-suite/internal/apps/flash"
)

func TestStreakCountsConsecutiveDaysAndStopsAtAGap(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	deck, err := f.store.CreateDeck(ctx, f.alice.ID, "Spanish", "")
	if err != nil {
		t.Fatal(err)
	}
	card, err := f.store.CreateCard(ctx, f.alice.ID, deck.ID, flash.CardTypeBasic, "a", "b", "")
	if err != nil {
		t.Fatal(err)
	}
	day1 := time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)
	day2 := day1.AddDate(0, 0, 1)
	// day3 (day1+2) is a gap
	day4 := day1.AddDate(0, 0, 3)

	if _, err := f.store.GradeCard(ctx, f.alice.ID, card.ID, flash.RatingGood, day1); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.GradeCard(ctx, f.alice.ID, card.ID, flash.RatingGood, day2); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.GradeCard(ctx, f.alice.ID, card.ID, flash.RatingGood, day4); err != nil {
		t.Fatal(err)
	}

	// "now" = day4 itself: today (day4) has a review, so the walk starts there.
	streak, err := f.store.Streak(ctx, f.alice.ID, day4)
	if err != nil {
		t.Fatal(err)
	}
	if streak != 1 {
		t.Errorf("streak at day4 = %d, want 1 (day3 gap breaks the chain)", streak)
	}

	// "now" = day2 itself: two consecutive days (day1, day2).
	streak, err = f.store.Streak(ctx, f.alice.ID, day2)
	if err != nil {
		t.Fatal(err)
	}
	if streak != 2 {
		t.Errorf("streak at day2 = %d, want 2", streak)
	}
}

func TestStreakDoesNotBreakBeforeTodaysReview(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	deck, err := f.store.CreateDeck(ctx, f.alice.ID, "Spanish", "")
	if err != nil {
		t.Fatal(err)
	}
	card, err := f.store.CreateCard(ctx, f.alice.ID, deck.ID, flash.CardTypeBasic, "a", "b", "")
	if err != nil {
		t.Fatal(err)
	}
	yesterday := time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)
	if _, err := f.store.GradeCard(ctx, f.alice.ID, card.ID, flash.RatingGood, yesterday); err != nil {
		t.Fatal(err)
	}

	// "now" is the next calendar day, before any review has happened today.
	today := yesterday.AddDate(0, 0, 1).Add(2 * time.Hour)
	streak, err := f.store.Streak(ctx, f.alice.ID, today)
	if err != nil {
		t.Fatal(err)
	}
	if streak != 1 {
		t.Errorf("streak = %d, want 1 (yesterday's review still counts before today's review happens)", streak)
	}
}

func TestStreakIsZeroWithNoReviewsEver(t *testing.T) {
	f := newFixture(t)
	streak, err := f.store.Streak(context.Background(), f.alice.ID, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if streak != 0 {
		t.Errorf("streak = %d, want 0", streak)
	}
}

func TestRetentionRateComputesFromRatingCounters(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	deck, err := f.store.CreateDeck(ctx, f.alice.ID, "Spanish", "")
	if err != nil {
		t.Fatal(err)
	}
	c1, err := f.store.CreateCard(ctx, f.alice.ID, deck.ID, flash.CardTypeBasic, "a", "b", "")
	if err != nil {
		t.Fatal(err)
	}
	c2, err := f.store.CreateCard(ctx, f.alice.ID, deck.ID, flash.CardTypeBasic, "c", "d", "")
	if err != nil {
		t.Fatal(err)
	}
	c3, err := f.store.CreateCard(ctx, f.alice.ID, deck.ID, flash.CardTypeBasic, "e", "f", "")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)
	if _, err := f.store.GradeCard(ctx, f.alice.ID, c1.ID, flash.RatingAgain, now); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.GradeCard(ctx, f.alice.ID, c2.ID, flash.RatingGood, now); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.GradeCard(ctx, f.alice.ID, c3.ID, flash.RatingEasy, now); err != nil {
		t.Fatal(err)
	}

	rate, err := f.store.RetentionRate(ctx, f.alice.ID, now.AddDate(0, 0, -30))
	if err != nil {
		t.Fatal(err)
	}
	want := 2.0 / 3.0
	if rate < want-0.001 || rate > want+0.001 {
		t.Errorf("rate = %v, want %v", rate, want)
	}
}

func TestRetentionRateIsZeroWithNoReviewsInWindow(t *testing.T) {
	f := newFixture(t)
	rate, err := f.store.RetentionRate(context.Background(), f.alice.ID, time.Now().AddDate(0, 0, -30))
	if err != nil {
		t.Fatal(err)
	}
	if rate != 0 {
		t.Errorf("rate = %v, want 0", rate)
	}
}

func TestCardsMasteredCountsOnlyReviewStateForThatUser(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	deck, err := f.store.CreateDeck(ctx, f.alice.ID, "Spanish", "")
	if err != nil {
		t.Fatal(err)
	}
	card, err := f.store.CreateCard(ctx, f.alice.ID, deck.ID, flash.CardTypeBasic, "a", "b", "")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)
	// First-ever grade lands in "learning", not "review" yet — not mastered.
	if _, err := f.store.GradeCard(ctx, f.alice.ID, card.ID, flash.RatingGood, now); err != nil {
		t.Fatal(err)
	}
	mastered, err := f.store.CardsMastered(ctx, f.alice.ID)
	if err != nil {
		t.Fatal(err)
	}
	if mastered != 0 {
		t.Errorf("mastered = %d, want 0 (card is still in learning)", mastered)
	}

	// Bob has no cards at all — must not be affected by or counted with Alice's.
	bobMastered, err := f.store.CardsMastered(ctx, f.bob.ID)
	if err != nil {
		t.Fatal(err)
	}
	if bobMastered != 0 {
		t.Errorf("bob's mastered = %d, want 0", bobMastered)
	}
}

func TestDailyReviewCountsZeroFillsEveryDay(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	deck, err := f.store.CreateDeck(ctx, f.alice.ID, "Spanish", "")
	if err != nil {
		t.Fatal(err)
	}
	card, err := f.store.CreateCard(ctx, f.alice.ID, deck.ID, flash.CardTypeBasic, "a", "b", "")
	if err != nil {
		t.Fatal(err)
	}
	day1 := time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)
	if _, err := f.store.GradeCard(ctx, f.alice.ID, card.ID, flash.RatingGood, day1); err != nil {
		t.Fatal(err)
	}

	now := day1.AddDate(0, 0, 2)
	counts, err := f.store.DailyReviewCounts(ctx, f.alice.ID, 3, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(counts) != 3 {
		t.Fatalf("len(counts) = %d, want 3", len(counts))
	}
	if counts[0].Count != 1 {
		t.Errorf("counts[0] (day1) = %d, want 1", counts[0].Count)
	}
	if counts[1].Count != 0 || counts[2].Count != 0 {
		t.Errorf("counts[1], counts[2] = %d, %d, want 0, 0", counts[1].Count, counts[2].Count)
	}
}

func TestPerDeckLoadAggregatesAcrossDecks(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	deckA, err := f.store.CreateDeck(ctx, f.alice.ID, "Spanish", "")
	if err != nil {
		t.Fatal(err)
	}
	deckB, err := f.store.CreateDeck(ctx, f.alice.ID, "French", "")
	if err != nil {
		t.Fatal(err)
	}
	cardA, err := f.store.CreateCard(ctx, f.alice.ID, deckA.ID, flash.CardTypeBasic, "a", "b", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.CreateCard(ctx, f.alice.ID, deckB.ID, flash.CardTypeBasic, "c", "d", ""); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)
	if _, err := f.store.GradeCard(ctx, f.alice.ID, cardA.ID, flash.RatingGood, now); err != nil {
		t.Fatal(err)
	}

	loads, err := f.store.PerDeckLoad(ctx, f.alice.ID, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(loads) != 2 {
		t.Fatalf("len(loads) = %d, want 2", len(loads))
	}
	var spanish, french flash.DeckLoad
	for _, l := range loads {
		if l.Deck.ID == deckA.ID {
			spanish = l
		}
		if l.Deck.ID == deckB.ID {
			french = l
		}
	}
	if spanish.ReviewsLast30Days != 1 {
		t.Errorf("spanish.ReviewsLast30Days = %d, want 1", spanish.ReviewsLast30Days)
	}
	if french.ReviewsLast30Days != 0 {
		t.Errorf("french.ReviewsLast30Days = %d, want 0", french.ReviewsLast30Days)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/apps/flash/... -run 'TestStreak|TestRetentionRate|TestCardsMastered|TestDailyReviewCounts|TestPerDeckLoad' -v`
Expected: FAIL — `stats.go` doesn't exist yet, so `Store.Streak` etc. are undefined.

- [ ] **Step 3: Implement `stats.go`**

```go
// internal/apps/flash/stats.go
package flash

import (
	"context"
	"fmt"
	"time"
)

// Streak returns the number of consecutive calendar days, ending at now (or
// at the most recent day with a review, if today has none yet), on which
// userID reviewed at least one card in any deck. A single day with zero
// reviews anywhere breaks the chain.
func (st *Store) Streak(ctx context.Context, userID int64, now time.Time) (int, error) {
	rows, err := st.db.QueryContext(ctx, `
        SELECT DISTINCT day FROM flash_review_counts
        WHERE user_id = ? AND (new_count + review_count) > 0`, userID)
	if err != nil {
		return 0, fmt.Errorf("flash: streak: %w", err)
	}
	defer func() { _ = rows.Close() }()

	days := map[string]bool{}
	for rows.Next() {
		var day string
		if err := rows.Scan(&day); err != nil {
			return 0, fmt.Errorf("flash: streak: %w", err)
		}
		days[day] = true
	}
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("flash: streak: %w", err)
	}

	cursor := now.UTC().Truncate(24 * time.Hour)
	if !days[formatDay(cursor)] {
		// Today has no review yet — that alone must not break a streak that
		// is still alive as of yesterday.
		cursor = cursor.AddDate(0, 0, -1)
	}
	count := 0
	for days[formatDay(cursor)] {
		count++
		cursor = cursor.AddDate(0, 0, -1)
	}
	return count, nil
}

// RetentionRate is the fraction of reviews rated Hard, Good, or Easy (i.e.
// not Again) across every day since (inclusive) through now, across every
// deck userID owns. Returns 0 when there were no reviews in the window at
// all, rather than dividing by zero.
func (st *Store) RetentionRate(ctx context.Context, userID int64, since time.Time) (float64, error) {
	var again, hard, good, easy int
	err := st.db.QueryRowContext(ctx, `
        SELECT coalesce(sum(again_count), 0), coalesce(sum(hard_count), 0),
               coalesce(sum(good_count), 0), coalesce(sum(easy_count), 0)
          FROM flash_review_counts
         WHERE user_id = ? AND day >= ?`,
		userID, formatDay(since)).Scan(&again, &hard, &good, &easy)
	if err != nil {
		return 0, fmt.Errorf("flash: retention rate: %w", err)
	}
	total := again + hard + good + easy
	if total == 0 {
		return 0, nil
	}
	return float64(hard+good+easy) / float64(total), nil
}

// CardsMastered counts userID's cards currently in FSRS's "review" state —
// cards that have graduated out of Learning/Relearning into long-term
// maintenance.
func (st *Store) CardsMastered(ctx context.Context, userID int64) (int, error) {
	var n int
	err := st.db.QueryRowContext(ctx,
		`SELECT count(*) FROM flash_card_state WHERE user_id = ? AND state = 'review'`,
		userID).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("flash: cards mastered: %w", err)
	}
	return n, nil
}

// CardsDueToday counts userID's cards with an existing review schedule
// whose due date has passed, across every deck that is not currently
// snoozed. It is uncapped by any deck's daily review budget — unlike
// DueQueue, which stops at each deck's ReviewsPerDay limit — so it answers
// "how much is waiting," not "how much will the review screen show me
// right now."
func (st *Store) CardsDueToday(ctx context.Context, userID int64, now time.Time) (int, error) {
	var n int
	err := st.db.QueryRowContext(ctx, `
        SELECT count(*)
          FROM flash_card_state cs
          JOIN flash_cards c ON c.id = cs.card_id
          JOIN flash_decks d ON d.id = c.deck_id
         WHERE cs.user_id = ? AND cs.due_at <= ?
           AND (d.snoozed_until IS NULL OR d.snoozed_until <= ?)`,
		userID, formatTime(now), formatTime(now)).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("flash: cards due today: %w", err)
	}
	return n, nil
}

// DayCount is one calendar day's combined new+review volume for one
// account, across every deck.
type DayCount struct {
	Day   time.Time
	Count int
}

// DailyReviewCounts returns the last days calendar days ending at now,
// oldest first, including days with zero reviews — a chart that omitted
// quiet days would compress its x-axis and show a busier habit than the
// real one.
func (st *Store) DailyReviewCounts(ctx context.Context, userID int64, days int, now time.Time) ([]DayCount, error) {
	if days <= 0 {
		return nil, nil
	}
	end := now.UTC().Truncate(24 * time.Hour)
	start := end.AddDate(0, 0, -(days - 1))

	rows, err := st.db.QueryContext(ctx, `
        SELECT day, sum(new_count + review_count)
          FROM flash_review_counts
         WHERE user_id = ? AND day >= ? AND day <= ?
         GROUP BY day`,
		userID, formatDay(start), formatDay(end))
	if err != nil {
		return nil, fmt.Errorf("flash: daily review counts: %w", err)
	}
	defer func() { _ = rows.Close() }()

	byDay := map[string]int{}
	for rows.Next() {
		var day string
		var count int
		if err := rows.Scan(&day, &count); err != nil {
			return nil, fmt.Errorf("flash: daily review counts: %w", err)
		}
		byDay[day] = count
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("flash: daily review counts: %w", err)
	}

	out := make([]DayCount, 0, days)
	for d := start; !d.After(end); d = d.AddDate(0, 0, 1) {
		out = append(out, DayCount{Day: d, Count: byDay[formatDay(d)]})
	}
	return out, nil
}

// DeckLoad is one deck's numbers for the per-deck breakdown table.
type DeckLoad struct {
	Deck              Deck
	Mastered          int
	Due               int
	ReviewsLast30Days int
}

// PerDeckLoad returns one DeckLoad per deck userID owns, in the same order
// ListDecks does (newest first).
func (st *Store) PerDeckLoad(ctx context.Context, userID int64, now time.Time) ([]DeckLoad, error) {
	decks, err := st.ListDecks(ctx, userID)
	if err != nil {
		return nil, err
	}
	since := formatDay(now.AddDate(0, 0, -29))

	out := make([]DeckLoad, 0, len(decks))
	for _, d := range decks {
		load := DeckLoad{Deck: d}

		if err := st.db.QueryRowContext(ctx, `
            SELECT count(*) FROM flash_card_state cs
              JOIN flash_cards c ON c.id = cs.card_id
             WHERE cs.user_id = ? AND c.deck_id = ? AND cs.state = 'review'`,
			userID, d.ID).Scan(&load.Mastered); err != nil {
			return nil, fmt.Errorf("flash: per-deck load: mastered: %w", err)
		}

		if err := st.db.QueryRowContext(ctx, `
            SELECT count(*) FROM flash_card_state cs
              JOIN flash_cards c ON c.id = cs.card_id
             WHERE cs.user_id = ? AND c.deck_id = ? AND cs.due_at <= ?`,
			userID, d.ID, formatTime(now)).Scan(&load.Due); err != nil {
			return nil, fmt.Errorf("flash: per-deck load: due: %w", err)
		}

		var reviews *int
		if err := st.db.QueryRowContext(ctx, `
            SELECT sum(new_count + review_count) FROM flash_review_counts
             WHERE user_id = ? AND deck_id = ? AND day >= ?`,
			userID, d.ID, since).Scan(&reviews); err != nil {
			return nil, fmt.Errorf("flash: per-deck load: reviews: %w", err)
		}
		if reviews != nil {
			load.ReviewsLast30Days = *reviews
		}

		out = append(out, load)
	}
	return out, nil
}
```

Note on `PerDeckLoad`'s `Due` query: deliberately **not** filtering by
`snoozed_until` here, unlike `CardsDueToday`'s account-wide total — the
per-deck table already shows which deck a number belongs to, so a snoozed
deck's own due count is still informative context for that row, whereas the
account-wide tile is meant to answer "how much should I expect to review
right now," which a snoozed deck should not inflate.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/apps/flash/... -run 'TestStreak|TestRetentionRate|TestCardsMastered|TestDailyReviewCounts|TestPerDeckLoad' -v`
Expected: all PASS.

- [ ] **Step 5: Run the whole package's tests**

Run: `go test ./internal/apps/flash/... -race -count=1`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/apps/flash/stats.go internal/apps/flash/stats_test.go
git commit -m "feat(flash): F6 stats — store queries"
```

---

### Task 3: Stats page — handler, template, route, nav link

**Files:**
- Create: `internal/apps/flash/handlers_stats.go`
- Create: `internal/apps/flash/handlers_stats_test.go`
- Create: `internal/apps/flash/templates/stats.html`
- Modify: `internal/apps/flash/flash.go` (route registration)
- Modify: `internal/apps/flash/templates/decks.html` (nav link)

**Interfaces:**
- Consumes: `Store.Streak`/`RetentionRate`/`CardsMastered`/`CardsDueToday`/`DailyReviewCounts`/`PerDeckLoad`, `DayCount`, `DeckLoad` (Task 2); `a.userID`, `a.deps.Page`, `a.deps.Render.Page`, `a.deps.Errors.Internal` (existing handler helpers, `handlers_decks.go`/`flash.go`).
- Produces: nothing further downstream — this is the last task.

- [ ] **Step 1: Implement `handlers_stats.go`**

```go
// internal/apps/flash/handlers_stats.go
package flash

import (
	"fmt"
	"net/http"
)

// statTile is one of the plain-number tiles at the top of the stats page —
// the same minimal shape ON Reader's own stats page tiles use
// (internal/apps/reader/view.go), re-declared here since apps don't share
// code.
type statTile struct {
	Label string
	Value string
}

// chartBar is one already-positioned bar. Geometry is computed in Go
// because html/template cannot do arithmetic — mirrors
// internal/apps/reader/view.go's chartBar exactly.
type chartBar struct {
	X, Y, Width, Height float64
	Label               string
}

type chartView struct {
	Title  string
	Bars   []chartBar
	Width  float64
	Height float64
	Max    int
	Empty  bool
}

// buildChart lays out one series of daily counts, mirroring
// internal/apps/reader/view.go's buildChart exactly.
func buildChart(title string, days []DayCount, value func(DayCount) int) chartView {
	const (
		height = 120.0
		barW   = 3.0
		barGap = 2.0
	)
	out := chartView{Title: title, Height: height}
	if len(days) == 0 {
		out.Empty = true
		return out
	}
	for _, d := range days {
		if v := value(d); v > out.Max {
			out.Max = v
		}
	}
	if out.Max == 0 {
		out.Empty = true
	}
	out.Width = float64(len(days)) * (barW + barGap)
	for i, d := range days {
		v := value(d)
		h := 0.0
		if out.Max > 0 {
			h = float64(v) / float64(out.Max) * height
		}
		out.Bars = append(out.Bars, chartBar{
			X: float64(i) * (barW + barGap), Y: height - h, Width: barW, Height: h,
			Label: fmt.Sprintf("%s: %d", d.Day.Format("2 Jan"), v),
		})
	}
	return out
}

// statsView is the whole stats page.
type statsView struct {
	Tiles    []statTile
	Chart    chartView
	Days     []DayCount
	PerDeck  []DeckLoad
}

func (a *App) stats(w http.ResponseWriter, r *http.Request) {
	userID, ok := a.userID(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	now := a.store.now()

	streak, err := a.store.Streak(ctx, userID, now)
	if err != nil {
		a.deps.Errors.Internal(w, r, err)
		return
	}
	rate, err := a.store.RetentionRate(ctx, userID, now.AddDate(0, 0, -30))
	if err != nil {
		a.deps.Errors.Internal(w, r, err)
		return
	}
	mastered, err := a.store.CardsMastered(ctx, userID)
	if err != nil {
		a.deps.Errors.Internal(w, r, err)
		return
	}
	due, err := a.store.CardsDueToday(ctx, userID, now)
	if err != nil {
		a.deps.Errors.Internal(w, r, err)
		return
	}
	days, err := a.store.DailyReviewCounts(ctx, userID, 30, now)
	if err != nil {
		a.deps.Errors.Internal(w, r, err)
		return
	}
	perDeck, err := a.store.PerDeckLoad(ctx, userID, now)
	if err != nil {
		a.deps.Errors.Internal(w, r, err)
		return
	}

	view := statsView{
		Tiles: []statTile{
			{Label: "Streak", Value: fmt.Sprintf("%d day(s)", streak)},
			{Label: "Retention (30 days)", Value: fmt.Sprintf("%.0f%%", rate*100)},
			{Label: "Cards mastered", Value: fmt.Sprintf("%d", mastered)},
			{Label: "Cards due", Value: fmt.Sprintf("%d", due)},
		},
		Chart:   buildChart("Reviews per day", days, func(d DayCount) int { return d.Count }),
		Days:    days,
		PerDeck: perDeck,
	}

	page := a.deps.Page(r, "Stats")
	page.Data = view
	if err := a.deps.Render.Page(w, http.StatusOK, "flash/stats", page); err != nil {
		a.deps.Errors.Internal(w, r, err)
	}
}
```

`a.store.now()` is the same clock accessor `snoozeDeck` already calls
(`handlers_decks.go:524`, `until := a.store.now().AddDate(0, 0, days)`) —
confirmed still present.

- [ ] **Step 2: Register the route in `flash.go`**

Add, right after the existing `r.HandleFunc("GET /flash-review.js", a.script)` line and before the media routes:

```go
	r.HandleFunc("GET /stats", a.stats)
```

- [ ] **Step 3: Add the template**

```html
{{/* internal/apps/flash/templates/stats.html */}}
{{define "content"}}
<div class="stack">
	<div class="row list-head">
		<h2>Stats</h2>
		<a class="toolbar-btn" href="/flash/">Back to decks</a>
	</div>

	<div class="stat-tiles">
		{{range .Data.Tiles}}
		<div class="stat-tile">
			<div class="value">{{.Value}}</div>
			<div class="label">{{.Label}}</div>
		</div>
		{{end}}
	</div>

	<figure class="flash-chart">
		<figcaption>{{.Data.Chart.Title}}</figcaption>
		{{if .Data.Chart.Empty}}
		<p class="empty">Nothing yet.</p>
		{{else}}
		<svg viewBox="0 0 {{.Data.Chart.Width}} {{.Data.Chart.Height}}" role="img"
		     aria-label="{{.Data.Chart.Title}}, daily, peak {{.Data.Chart.Max}}" preserveAspectRatio="none">
			{{range .Data.Chart.Bars}}
			<rect x="{{.X}}" y="{{.Y}}" width="{{.Width}}" height="{{.Height}}" rx="1">
				<title>{{.Label}}</title>
			</rect>
			{{end}}
		</svg>
		{{end}}
	</figure>

	{{if .Data.Days}}
	<details class="flash-stats-daily">
		<summary>Daily figures (text alternative to the chart above)</summary>
		<div class="table-scroll">
			<table class="flash-stats-table">
				<thead><tr><th>Date</th><th>Reviews</th></tr></thead>
				<tbody>
					{{range .Data.Days}}
					<tr><td>{{.Day.Format "2 Jan 2006"}}</td><td>{{.Count}}</td></tr>
					{{end}}
				</tbody>
			</table>
		</div>
	</details>
	{{end}}

	<h3>Per deck</h3>
	{{if .Data.PerDeck}}
	<div class="table-scroll">
		<table class="flash-stats-table">
			<thead><tr><th>Deck</th><th>Mastered</th><th>Due</th><th>Reviews (30 days)</th></tr></thead>
			<tbody>
				{{range .Data.PerDeck}}
				<tr>
					<td>{{.Deck.Name}}</td>
					<td>{{.Mastered}}</td>
					<td>{{.Due}}</td>
					<td>{{.ReviewsLast30Days}}</td>
				</tr>
				{{end}}
			</tbody>
		</table>
	</div>
	{{else}}
	<p class="empty">No decks yet.</p>
	{{end}}
</div>
{{end}}
```

- [ ] **Step 4: Add the "Stats" nav link to `templates/decks.html`**

In the `content` block's header row (`<div class="row list-head">`), add a
fourth link alongside the existing New deck/Import/Review:

```html
		<a class="toolbar-btn" href="/flash/stats">Stats</a>
```

placed after the existing `Review` link, before the row's closing `</div>`.

- [ ] **Step 5: Write the failing handler tests**

```go
// internal/apps/flash/handlers_stats_test.go
package flash_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/iliafrenkel/on-suite/internal/apps/flash"
	"github.com/iliafrenkel/on-suite/internal/apptest"
)

func TestStatsPageZeroStateForBrandNewAccount(t *testing.T) {
	s := apptest.NewServer(t, flash.New(), flash.NewStore)
	rec := s.Do(t, s.Alice, httptest.NewRequest(http.MethodGet, "/flash/stats", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /flash/stats = %d; body: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, "Streak") || !strings.Contains(body, "Cards due") {
		t.Errorf("stats page missing expected tile labels; body: %s", body)
	}
	if !strings.Contains(body, "No decks yet.") {
		t.Errorf("stats page should show the empty per-deck state; body: %s", body)
	}
}

func TestStatsPageRequiresSignIn(t *testing.T) {
	s := apptest.NewServer(t, flash.New(), flash.NewStore)
	rec := s.Do(t, nil, httptest.NewRequest(http.MethodGet, "/flash/stats", nil))
	if rec.Code == http.StatusOK {
		t.Fatalf("GET /flash/stats with no session = 200, want a redirect/401")
	}
}

func TestStatsPageShowsPopulatedData(t *testing.T) {
	s := apptest.NewServer(t, flash.New(), flash.NewStore)
	deck, err := s.Store.CreateDeck(t.Context(), s.Alice.User.ID, "Spanish", "")
	if err != nil {
		t.Fatal(err)
	}
	card, err := s.Store.CreateCard(t.Context(), s.Alice.User.ID, deck.ID, flash.CardTypeBasic, "hola", "hello", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Store.GradeCard(t.Context(), s.Alice.User.ID, card.ID, flash.RatingGood, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}

	rec := s.Do(t, s.Alice, httptest.NewRequest(http.MethodGet, "/flash/stats", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /flash/stats = %d; body: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "Spanish") {
		t.Errorf("stats page should list the Spanish deck in the per-deck table; body: %s", rec.Body.String())
	}
}
```

- [ ] **Step 6: Run the tests to verify they fail, then implement, then verify they pass**

Run: `go test ./internal/apps/flash/... -run TestStatsPage -v`
Expected: FAIL first (route/template don't exist), then PASS once Steps 1-4
are in place.

- [ ] **Step 7: Run the whole repo's tests**

Run: `go test ./... -race -count=1`
Expected: PASS — confirms the new route doesn't conflict with any existing
one and no other test was broken by the `decks.html` nav-link addition.

- [ ] **Step 8: Run gofmt, vet, and the arch test**

```bash
gofmt -l .
go vet ./...
go test ./internal/arch/...
```
Expected: all clean.

- [ ] **Step 9: Commit**

```bash
git add internal/apps/flash/handlers_stats.go internal/apps/flash/handlers_stats_test.go \
        internal/apps/flash/templates/stats.html internal/apps/flash/flash.go \
        internal/apps/flash/templates/decks.html
git commit -m "feat(flash): F6 stats — page, route, and nav link"
```

---

## Final Checks

- [ ] `go build ./...`
- [ ] `gofmt -l .` (no output)
- [ ] `go vet ./...`
- [ ] `go mod tidy` (no diff)
- [ ] `go test ./... -race -count=1`
- [ ] `go test ./internal/arch/...`
