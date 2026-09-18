# ON Flash F6 — stats (design spec)

F6 adds a dedicated stats page: streaks, retention rate, cards mastered,
cards due, and per-deck review load — visible, data-forward numbers with no
points, badges, or leaderboards, per the features doc's design goal. This
closes out ON Flash's originally planned feature set (F1–F6).

This mirrors ON Reader's own R7 stats page (`internal/apps/reader/stats.go`,
`view.go`, `templates/stats.html`) closely enough to reuse its proven shape —
a daily-snapshot table, server-computed SVG bar chart, and a plain-number
tile row — adapted for Flash's per-card FSRS data instead of Reader's
per-article read/backlog counts.

## Scope

- One new page, `GET /flash/stats`, linked from the deck list header
  alongside New deck/Import/Review.
- Four top-line tiles: current streak (consecutive days with at least one
  review, any deck), retention rate over the last 30 days, cards mastered
  (total, across all decks), cards due today (total, across all decks).
- A 30-day bar chart of daily review volume (new + review cards combined),
  every calendar day present including zero-review days.
- A per-deck breakdown table below the chart: deck name, cards mastered,
  cards due, reviews in the last 30 days.
- **Streak** and **mastered** are computed live from existing data — no new
  columns needed for either:
  - Streak walks `flash_review_counts` backward from today, across every
    deck, counting consecutive days with `new_count + review_count > 0`,
    stopping at the first gap.
  - Mastered is `state = 'review'` in `flash_card_state` — a card that has
    graduated out of Learning/Relearning into FSRS's long-term Review state.
    No stability/day threshold on top of that; FSRS's own state transition
    is the bar.
- **Retention rate** needs new data: `flash_review_counts` (one row per
  `user_id, deck_id, day`, already written on every grade via
  `review.go`'s `bumpDailyCounts`) gains four columns —
  `again_count`, `hard_count`, `good_count`, `easy_count` — bumped alongside
  the existing `new_count`/`review_count` on every grade. Retention rate for
  a window is `(hard+good+easy) / (again+hard+good+easy)` summed across the
  window's rows. Rows written before this migration have all four new
  columns at 0, which reads as "no ratings recorded that day" — accurate for
  genuinely idle days, and a harmless, un-reconstructable gap for the small
  number of pre-migration days, the same tradeoff Reader's own daily-stats
  table already accepts for its `Reconstructed` flag (Flash doesn't need an
  equivalent flag: the retention window is short (30 days) so any
  pre-migration gap ages out quickly, unlike Reader's 90-day charts).
- Out of scope: per-review event log (rejected in favor of the cheaper daily
  counters — nothing in this feature needs single-review granularity),
  export of stats data, stats for deleted decks (their `flash_review_counts`
  rows cascade-delete with the deck, same as everything else in this app).

## Architecture

One migration:

```sql
ALTER TABLE flash_review_counts ADD COLUMN again_count INTEGER NOT NULL DEFAULT 0;
ALTER TABLE flash_review_counts ADD COLUMN hard_count  INTEGER NOT NULL DEFAULT 0;
ALTER TABLE flash_review_counts ADD COLUMN good_count  INTEGER NOT NULL DEFAULT 0;
ALTER TABLE flash_review_counts ADD COLUMN easy_count  INTEGER NOT NULL DEFAULT 0;
```

`review.go`'s existing `bumpDailyCounts` (called from the grading path) is
extended to take the rating that was just recorded and increment the
matching column, in the same statement/transaction it already uses for
`new_count`/`review_count` — no new transaction, no new call site beyond
threading the rating value through.

New files, following the established one-concern-per-file split:

- **`stats.go`** — pure store queries, no HTTP knowledge: `Streak(ctx,
  userID, now) (int, error)`, `RetentionRate(ctx, userID, since) (float64,
  error)`, `CardsMastered(ctx, userID) (int, error)`, `DailyReviewCounts(ctx,
  userID, since, until) ([]DayCount, error)` (one row per calendar day,
  zero-filled, mirroring Reader's `DailyStats`), `PerDeckLoad(ctx, userID)
  ([]DeckLoad, error)` (mastered/due/30-day-review-count per deck).
- **`handlers_stats.go`** — `GET /flash/stats`, assembling the tile row,
  chart, and per-deck table into one view.
- **`templates/stats.html`** — tiles grid, inline-SVG bar chart with a
  `<details>` table fallback, per-deck table — structurally the same as
  `internal/apps/reader/templates/stats.html`.

Reused from Reader's `view.go` pattern (re-implemented locally in Flash,
since apps don't share code): a package-local `statTile{Label, Value
string}` for the tile row (not `internal/platform/app`'s `Stat` type, which
is a different, admin-dashboard-only concept), and `chartBar`/`chartView`/
`buildChart` for the bar chart — geometry computed in Go because
`html/template` cannot do arithmetic, one hue, `<title>` tooltips for
hover values with no JavaScript.

## Computing "cards due today"

Reuses the existing due-queue logic the review engine already has (the same
query `DueQueue`/`dueReviewCards` in `review.go` uses to build the actual
review session), called once per deck and summed, rather than re-deriving
due-ness with separate SQL — this guarantees the stats page's "due today"
number always agrees with what the review screen would actually show.

## UI

- Deck list header gains a "Stats" link alongside New deck/Import/Review.
- Tiles render in the same CSS grid Reader's stats page already uses.
- The bar chart is one series (review volume), matching Reader's own
  one-hue constraint — a second series (e.g. splitting new vs. review) is
  not in scope; the per-deck table already gives volume broken down by
  deck, and rating breakdown is expressed in the retention tile, not a
  second chart line.
- Per-deck table has one row per deck the account owns, sorted by name;
  no pagination — matches the deck list's own lack of pagination at this
  scale.

## Zero state

A brand-new account with no review history: tiles show streak 0, retention
rate 0%, mastered 0, due 0; the chart renders 30 empty-day bars (all-zero,
same as `buildChart`'s existing `Empty` handling in Reader, which Flash
reuses verbatim); the per-deck table is simply empty if there are no decks
yet. No dedicated empty-state copy needed — real zeros already read
correctly.

## Error handling

Every value on this page is a `COUNT`/`SUM`/arithmetic computation over the
signed-in account's own rows — there is no "not found," ownership check, or
user-input validation anywhere on this page, unlike every other Flash
screen so far. The only failure mode is a genuine DB error, handled the
same generic way `a.deps.Errors.Internal` handles any other unexpected
store error elsewhere in this app.

## Testing

- `stats_test.go`: `Streak` across a gap (including a day with multiple
  reviews still counting once, and "no reviews ever" returning 0);
  `RetentionRate` against seeded per-rating counts (including a window with
  zero reviews, which must not divide by zero); `CardsMastered` scoped
  correctly per user (unaffected by another account's cards in the same
  fixture) and per FSRS state (only `"review"` counts, not `"learning"`);
  `DailyReviewCounts` zero-fills every day in range; `PerDeckLoad`
  aggregates correctly across multiple decks.
- `handlers_stats_test.go`: the page renders for a signed-in user via
  `apptest`, covering both a populated case (seeded reviews across several
  days and decks) and the brand-new zero-state case; sign-in required.
- Extend `review_test.go` for `bumpDailyCounts`'s new rating-counter
  behavior — each of the four ratings increments the correct column and
  leaves the other three at their prior value.

## Open items deferred to later

- Any notion of a stats "reconstruction" flag for pre-migration days (Reader
  has one; Flash's short 30-day retention window makes it unnecessary, as
  explained above — worth revisiting only if a future stats window grows
  long enough for pre-migration gaps to matter again).
- Export of stats data (CSV, etc.).
- Any goal-setting or notification tied to streaks (e.g. "you're about to
  lose your streak") — the features doc is explicit that this stays
  data-forward, not game-forward, and reminders are a different feature
  entirely.
