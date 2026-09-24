# ON Flash F2 — Review Engine Design

This is the design for F2 of ON Flash (see the feature list at
[2026-09-17-on-flash-features.md](2026-09-17-on-flash-features.md) and F1's
plan at
[docs/superpowers/plans/2026-09-17-on-flash-f1-deck-card-crud.md](../plans/2026-09-17-on-flash-f1-deck-card-crud.md),
merged). F1 built deck/card CRUD with tags; F2 adds the actual review loop:
FSRS-based scheduling, 4-point grading, per-deck daily limits, undo-last-
rating, and the two manual overrides (snooze a deck, adjust its pace).
Import, media, and sharing remain out of scope (F3–F5).

## Goals

- Reviewing a card should feel instant and keyboard-first: reveal, read,
  grade, next — no mouse required.
- Scheduling is automatic (FSRS) by default, with two escape hatches for
  real life: snoozing a deck, and undoing an accidental grade.
- A big freshly-imported deck must not be able to dump an unbounded number
  of cards into one day's session.

## Data model

### Per-account card scheduling state

```sql
CREATE TABLE flash_card_state (
    user_id         INTEGER NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    card_id         INTEGER NOT NULL REFERENCES flash_cards (id) ON DELETE CASCADE,
    state           TEXT    NOT NULL, -- "new" | "learning" | "review" | "relearning"
    due_at          TEXT    NOT NULL,
    stability       REAL    NOT NULL,
    difficulty      REAL    NOT NULL,
    reps            INTEGER NOT NULL,
    lapses          INTEGER NOT NULL,
    last_review_at  TEXT,
    -- Undo buffer: every column above, snapshotted immediately before the
    -- most recent grade was applied. Overwritten on every review; only one
    -- step of undo is kept ("undo last rating" is singular by design).
    -- NULL prev_state means there is nothing to undo (never reviewed, or
    -- already undone once).
    prev_state          TEXT,
    prev_due_at         TEXT,
    prev_stability      REAL,
    prev_difficulty     REAL,
    prev_reps           INTEGER,
    prev_lapses         INTEGER,
    prev_last_review_at TEXT,
    PRIMARY KEY (user_id, card_id)
) STRICT, WITHOUT ROWID;
```

Absence of a row means "never reviewed" — a card starts in the FSRS "new"
state implicitly, and a row is only inserted on its first grade. This
mirrors ON Reader's `reader_item_state`: subscribing to (or in this case,
being handed a shared) deck full of cards costs nothing per account until
you actually study one.

### Per-deck scheduling settings

Three columns added to the existing `flash_decks` table (migration, not a
new table — these are deck properties, not a separate concern):

```sql
ALTER TABLE flash_decks ADD COLUMN new_cards_per_day INTEGER NOT NULL DEFAULT 20;
ALTER TABLE flash_decks ADD COLUMN reviews_per_day INTEGER; -- NULL = unlimited
ALTER TABLE flash_decks ADD COLUMN snoozed_until TEXT;      -- NULL = not snoozed
```

`new_cards_per_day` and `reviews_per_day` are account-wide per deck (not
per reviewing account) since a deck's "pace" is a property the deck's owner
sets, same as its name — F1 already treats a shared deck's copy as fully
independent per adopter (per the feature spec), so this is consistent:
each account's adopted copy has its own pace settings, editable by whoever
owns that copy.

### Per-account, per-deck, per-day consumption counters

```sql
CREATE TABLE flash_review_counts (
    user_id     INTEGER NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    deck_id     INTEGER NOT NULL REFERENCES flash_decks (id) ON DELETE CASCADE,
    day         TEXT    NOT NULL, -- "2026-09-17", UTC calendar day
    new_count   INTEGER NOT NULL DEFAULT 0,
    review_count INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (user_id, deck_id, day)
) STRICT, WITHOUT ROWID;
```

Tracking consumption explicitly (rather than recomputing "how many cards
became due today and were already reviewed" from `flash_card_state` alone)
keeps the daily-limit query cheap and correct across multiple short
sessions in the same day, and makes undo's "decrement the count" step a
simple, direct operation.

## FSRS integration

`github.com/open-spaced-repetition/go-fsrs` is added as a dependency and
imported from exactly one file, `internal/apps/flash/fsrs.go`. Nothing else
in the package references the library's types directly — this file exposes
a small wrapper:

```go
func scheduleCard(current cardSchedule, rating Rating, now time.Time) cardSchedule
```

where `cardSchedule` is this package's own struct (the columns of
`flash_card_state` minus the `prev_*` undo buffer), decoupling the rest of
the app from the library's own type shapes. This is contained the same way
`internal/apps/reader` contains `go-shiori/go-readability`: an
`internal/arch` test asserts only `fsrs.go` imports the third-party
package, so a future replacement or hand-rolled implementation only
touches one file.

Default FSRS weights (the library's built-in defaults) are used for every
account in F2 — there is no per-user parameter optimization. That would
require exporting and analyzing review history, which is a distinct,
larger feature outside F2's scope.

The four ratings (Again/Hard/Good/Easy) map directly to FSRS's own
four-point `Rating` enum — no translation layer needed there.

## Review session flow

### Queue construction

Given an account and either "all decks" or one specific deck:

1. Exclude any deck where `snoozed_until` is in the future.
2. For each remaining deck, look up today's `flash_review_counts` row
   (creating an implicit zero if absent).
3. Due reviews: cards with a `flash_card_state` row where `due_at <= now`,
   up to `reviews_per_day - review_count` (unlimited if `reviews_per_day`
   is NULL).
4. New cards: cards with no `flash_card_state` row at all, up to
   `new_cards_per_day - new_count`.
5. Serve every due review first, ordered by `due_at` across all included
   decks (most overdue first; ties go to deck order, newest deck first,
   then card id), then new cards deck by deck in that same deck order. Each
   deck's own limits from steps 3–4 still cap its share. Serve one card at
   a time. (Revised by #296: this used to concatenate deck by deck, so the
   newest deck always went first.)

### Routes

- `GET /flash/review` — cross-deck queue, built from every non-snoozed
  deck.
- `GET /flash/{deckID}/review` — same queue construction, scoped to one
  deck (still respects that deck's own snooze state and limits).
- `POST /flash/review/{cardID}/grade` — grades the current card (form field
  `rating` = 1..4), applies `scheduleCard`, updates the day's counter,
  saves the pre-grade snapshot into `prev_*`, and returns the next card in
  the queue (or an empty-state "all done for now" view). A no-JS request
  (no HTMX header) instead gets a 303 redirect back to the review page
  (POST-redirect-GET), with `?undo=<cardID>` on the query string so the
  reloaded page can still offer Undo for the card that was just graded.
- `POST /flash/review/{cardID}/undo` — restores that card's `prev_*`
  columns back into the live columns, decrements the counter that grading
  it had incremented, and clears `prev_*` (so undoing the same card twice
  in a row is a no-op the second time — only one step is ever kept). The
  review session UI always calls this with the ID of whichever card it
  just showed a "graded — undo?" affordance for, immediately after that
  card's own grade response; it is not a blind "undo whatever was last
  graded across the account," since a card's `prev_*` snapshot persists
  until either undone or overwritten by that same card's next review, so
  more than one card can have a stale, no-longer-relevant snapshot sitting
  around at once. A request naming a card whose `prev_state` is already
  NULL (nothing to undo) is a no-op that reports as much, not an error. A
  no-JS request also gets the 303 redirect back to the review page,
  mirroring the grade route.

### Card presentation

One card at a time: front is shown; revealing the answer (Space, or a
click) shows the back (for Basic) or the fully-filled-in text (for Cloze),
plus the notes field if present. Grading buttons (Again/Hard/Good/Easy)
appear only after reveal, each bound to a number key 1–4, matching the
convention players of existing spaced-repetition tools already know. An
"Undo" control is available immediately after grading the previous card,
before advancing further — pressing U (or the button) reverts it and
re-shows that same card for re-grading.

## Manual overrides

**Snooze a deck**: a form action sets `snoozed_until` to a chosen date (or
a quick "for 1 week" / "for 1 month" shortcut); the deck list shows a
"snoozed until X" badge, with an "unsnooze now" action that clears the
column. A snoozed deck's cards keep accumulating real due dates in the
background (FSRS doesn't know or care that it's snoozed) — snoozing only
affects what the queue construction step above pulls in.

**Adjust pace**: `new_cards_per_day` and `reviews_per_day` become two more
fields on the existing deck edit form from F1, alongside name and
description. Leaving `reviews_per_day` blank means unlimited.

## Testing

- `internal/apps/flash/fsrs_test.go`: narrow unit tests pinning
  `scheduleCard`'s behavior against known go-fsrs input/output pairs for
  each of the four ratings, so a future library upgrade that changes
  scheduling behavior is caught explicitly rather than silently.
- Store tests for the queue-construction query, daily-limit enforcement,
  and snooze filtering, against real SQLite, using `Store.SetClock` (the
  same pattern F1 already uses) to control "today" without sleeping in
  tests.
- Handler tests for the full review loop (grade → next card → undo →
  re-grade) via the existing `apptest` harness, plus the two manual
  overrides.
- An `internal/arch` test asserting `go-fsrs` is imported only from
  `fsrs.go`, mirroring the existing `go-readability` containment test.

## Out of scope for F2

- Per-user FSRS parameter optimization (needs review-history export/
  analysis; a distinct future feature).
- Multi-step undo history (only the single most recent grade is
  reversible, by design — "undo last rating," not a full history).
- Import, media, and sharing (F3–F5).
