# ON Flash UI U1 — Foundation and home screen Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Give ON Flash its new home screen: user-chosen deck colours, a Pastes-style split view (deck list left, deck pane right) under an app toolbar, coloured deck "stacks" with due badges, a deck pane led by one big "Review N cards" button, and a first-run welcome.

**Architecture:** A migration adds `flash_decks.color`; the store gains a colour setter and a `DeckSummaries` query that computes, per deck, exactly the numbers the new UI shows (and that always agree with `DueQueue`). The handlers build one view model (`buildDeckIndex`) shared by the full-page and HTMX paths, and `templates/decks.html` is rewritten around it. All styling is new CSS in app.css's Flash section; `flash-review.js` is renamed `flash.js` so later phases have one script to extend.

**Tech Stack:** Go 1.x, `database/sql` + SQLite (modernc), `html/template`, HTMX (vendored), plain CSS. No new dependencies.

**Spec:** [docs/superpowers/specs/2026-09-23-on-flash-ui-overhaul-design.md](../specs/2026-09-23-on-flash-ui-overhaul-design.md) — sections 1.1, 1.2, 1.5, 1.6 and 2. Mockups: [2026-09-23-on-flash-ui-overhaul-mockups.html](../specs/2026-09-23-on-flash-ui-overhaul-mockups.html) (section 1, "Home screen").

**This is PR 1 of 6** (U1 → U6). Later plans build on the names this one introduces — keep them exactly as written.

## Global Constraints

- Branch: `feat/flash-ui-u1` off `main`. `main` is protected — never push to it; open a PR.
- Full check must pass before every commit that touches Go (mirrors CI):
  ```bash
  gofmt -l .                                              # must print nothing
  go vet ./...
  go run honnef.co/go/tools/cmd/staticcheck@v0.8.0 ./...
  go mod tidy && git diff --exit-code go.mod go.sum
  go test ./... -race -count=1
  ```
- No new Go dependencies, no Node/npm, no build step. No CGO.
- **CSP:** no inline `<script>`, no `style="…"` attributes anywhere in templates. Colour and size come from classes or SVG presentation attributes.
- Migrations are forward-only; the next Flash migration number is `0010`.
- Apps never import each other; `internal/ui` must not import anything. Run `go test ./internal/arch/...` if you touch imports.
- Commit subjects use Conventional Commits with scope `flash` (e.g. `feat(flash): add deck colour column`). End each commit message with the attribution trailer your harness requires, if any.
- Copy style (spec 1.1): sentence case, verb-first buttons, no "please", no "successfully".
- Deck colour names, in this exact order: `teal, blue, purple, pink, coral, amber, green, gray`. Default `teal`.
- Existing test hooks that must survive this PR (other tests rely on them): `.deck-list`, `a.deck-row`, `#deck-detail`, `#shared-with-me` (with `hx-swap-oob` in fragment responses), `.notice-error`, the Adopt/Dismiss forms posting to `/flash/shared/adopt` and `/flash/shared/decline` with a non-empty CSRF input.
- `htmlassert` selectors: descendant chains of simple parts only — `tag`, `.class`, `#id`, `[attr]`, `[attr=value]`, or `tag` + one of those (`a.deck-row`, `a[href="/x"]`). **No compound parts** like `a.class[attr]` or `.a.b`: select by one qualifier, then check others with `htmlassert.Attr`.
- Test fixtures: store tests use `newFixture(t)` from `internal/apps/flash/deck_test.go` (package `flash_test`, users `f.alice`/`f.bob`); handler tests use `newServer(t)` from `handlers_decks_test.go` (sessions `s.Alice`/`s.Bob`, helpers `s.Get`, `s.Post`, `s.PostHX`, `s.Submit`, `httpGet`, `itoa`). Package-internal tests (for unexported helpers) use `package flash`, like `fsrs_test.go`.

---

## File map

| File | Change |
|---|---|
| `internal/apps/flash/migrations/0010_deck_color.sql` | **Create** — adds `color` column |
| `internal/apps/flash/deck.go` | `Deck.Color`, `DeckColors`, `DefaultDeckColor`, `ValidDeckColor`, `SetDeckColor`, shared `deckColumns` |
| `internal/apps/flash/share.go` | `deckByIDIgnoringOwner` uses `deckColumns`; `AdoptShare` copies colour |
| `internal/apps/flash/deck_summary.go` | **Create** — `DeckSummary`, `DeckSummaries`, `nextCardsLabel` |
| `internal/apps/flash/review.go` | extract `dailyBudget` from `DueQueue` (no behaviour change) |
| `internal/apps/flash/handlers_decks.go` | colour in forms, `buildDeckIndex`, new view-model fields |
| `internal/apps/flash/templates/decks.html` | **Rewrite** — home layout |
| `internal/apps/flash/templates/components.partial.html` | **Create** — `flash-stack`, `flash-pane-back` (shared by every Flash page) |
| `internal/apps/flash/templates/import.partial.html` | small restyle (back button, primary button) |
| `internal/apps/flash/static/flash-review.js` → `static/flash.js` | **Rename** |
| `internal/apps/flash/flash.go` | embed + route rename to `/flash/flash.js` |
| `internal/apps/flash/templates/review.html` | script URL |
| `internal/ui/toolbar_icons.go` (+ test) | new icons: `edit`, `trash`, `cards`, `play`, `arrow-left` |
| `internal/ui/static/app.css` | new Flash home CSS + deck colour tokens |
| Tests | `deck_test.go`, `share_test.go`, `deck_summary_test.go` (new), `deck_summary_internal_test.go` (new), `handlers_decks_test.go`, `handlers_review_test.go`, `internal/ui/toolbar_icons_test.go` |

---

### Task 1: Deck colour storage

**Files:**
- Create: `internal/apps/flash/migrations/0010_deck_color.sql`
- Modify: `internal/apps/flash/deck.go`, `internal/apps/flash/share.go`
- Test: `internal/apps/flash/deck_test.go`, `internal/apps/flash/share_test.go`

**Interfaces:**
- Consumes: existing `Deck`, `scanDeckRow`, `ErrInvalid`, `ErrNotFound`, `AdoptShare`.
- Produces (later tasks and plans rely on these exact names):
  - `Deck.Color string`
  - `var DeckColors = []string{"teal", "blue", "purple", "pink", "coral", "amber", "green", "gray"}`
  - `const DefaultDeckColor = "teal"`
  - `func ValidDeckColor(c string) bool`
  - `func (st *Store) SetDeckColor(ctx context.Context, userID, id int64, color string) (Deck, error)` — `ErrInvalid` for an unknown colour, `ErrNotFound` for a missing/foreign deck.
  - `const deckColumns` — the SELECT column list every `scanDeckRow` caller uses.

- [ ] **Step 1: Write the migration**

```sql
-- internal/apps/flash/migrations/0010_deck_color.sql
-- A deck's colour, picked by its owner from a fixed palette of eight names
-- (deck.go's DeckColors). Stored as the name, not a hex value: the name is
-- what templates turn into a deck-c-<name> class, and app.css owns what each
-- name looks like in light and dark mode — see the UI overhaul spec §1.2.
-- Existing decks become teal, the palette's first entry.
ALTER TABLE flash_decks ADD COLUMN color TEXT NOT NULL DEFAULT 'teal';
```

- [ ] **Step 2: Write the failing store tests**

Append to `internal/apps/flash/deck_test.go`:

```go
func TestNewDeckIsTealByDefault(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	d, err := f.store.CreateDeck(ctx, f.alice.ID, "Spanish", "")
	if err != nil {
		t.Fatal(err)
	}
	if d.Color != flash.DefaultDeckColor {
		t.Errorf("CreateDeck Color = %q, want %q", d.Color, flash.DefaultDeckColor)
	}
	got, err := f.store.DeckByID(ctx, f.alice.ID, d.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Color != flash.DefaultDeckColor {
		t.Errorf("DeckByID Color = %q, want %q", got.Color, flash.DefaultDeckColor)
	}
}

func TestSetDeckColor(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	d, err := f.store.CreateDeck(ctx, f.alice.ID, "Spanish", "")
	if err != nil {
		t.Fatal(err)
	}

	updated, err := f.store.SetDeckColor(ctx, f.alice.ID, d.ID, "purple")
	if err != nil {
		t.Fatalf("SetDeckColor: %v", err)
	}
	if updated.Color != "purple" {
		t.Errorf("Color = %q, want purple", updated.Color)
	}
	decks, err := f.store.ListDecks(ctx, f.alice.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(decks) != 1 || decks[0].Color != "purple" {
		t.Errorf("ListDecks = %+v, want one purple deck", decks)
	}
}

func TestSetDeckColorRejectsUnknownColor(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	d, err := f.store.CreateDeck(ctx, f.alice.ID, "Spanish", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.SetDeckColor(ctx, f.alice.ID, d.ID, "chartreuse"); !errors.Is(err, flash.ErrInvalid) {
		t.Errorf("SetDeckColor(chartreuse) err = %v, want ErrInvalid", err)
	}
}

func TestSetDeckColorRejectsSomeoneElsesDeck(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	d, err := f.store.CreateDeck(ctx, f.alice.ID, "Spanish", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.SetDeckColor(ctx, f.bob.ID, d.ID, "blue"); !errors.Is(err, flash.ErrNotFound) {
		t.Errorf("SetDeckColor as bob err = %v, want ErrNotFound", err)
	}
}

func TestValidDeckColor(t *testing.T) {
	for _, c := range flash.DeckColors {
		if !flash.ValidDeckColor(c) {
			t.Errorf("ValidDeckColor(%q) = false, want true", c)
		}
	}
	for _, c := range []string{"", "Teal", "red", "#1D9E75", "teal "} {
		if flash.ValidDeckColor(c) {
			t.Errorf("ValidDeckColor(%q) = true, want false", c)
		}
	}
	if len(flash.DeckColors) != 8 || flash.DeckColors[0] != flash.DefaultDeckColor {
		t.Errorf("DeckColors = %v, want 8 names starting with the default", flash.DeckColors)
	}
}
```

Append to `internal/apps/flash/share_test.go`:

```go
func TestAdoptShareCopiesDeckColor(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	d, err := f.store.CreateDeck(ctx, f.alice.ID, "Planets", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.SetDeckColor(ctx, f.alice.ID, d.ID, "pink"); err != nil {
		t.Fatal(err)
	}
	sh, err := f.store.ShareDeck(ctx, f.alice.ID, d.ID, f.bob.ID)
	if err != nil {
		t.Fatal(err)
	}
	adopted, err := f.store.AdoptShare(ctx, f.bob.ID, sh.ID)
	if err != nil {
		t.Fatal(err)
	}
	if adopted.Color != "pink" {
		t.Errorf("adopted deck Color = %q, want the source deck's pink", adopted.Color)
	}
}
```

- [ ] **Step 3: Run the tests to verify they fail**

Run: `go test ./internal/apps/flash/ -run 'DeckColor|TealByDefault|AdoptShareCopiesDeckColor' -count=1`
Expected: build failure — `d.Color undefined`, `flash.DefaultDeckColor undefined`, `f.store.SetDeckColor undefined`.

- [ ] **Step 4: Implement colour in `deck.go`**

In `internal/apps/flash/deck.go`, add after the existing `const (...)` block (the one holding `MaxDeckNameRunes`):

```go
// DeckColors is the fixed palette a deck's owner picks from, in the order
// the swatch picker shows them. The names — not hex values — are stored:
// templates turn one into a deck-c-<name> class and app.css decides what
// that looks like in light and dark mode (UI overhaul spec §1.2).
var DeckColors = []string{"teal", "blue", "purple", "pink", "coral", "amber", "green", "gray"}

// DefaultDeckColor must match migrations/0010_deck_color.sql's column
// default, for the same reason DefaultNewCardsPerDay must match 0005's:
// CreateDeck never re-reads the row it inserts.
const DefaultDeckColor = "teal"

// ValidDeckColor reports whether c is one of DeckColors, exactly (the
// comparison is case-sensitive and does not trim).
func ValidDeckColor(c string) bool {
	for _, name := range DeckColors {
		if c == name {
			return true
		}
	}
	return false
}

// deckColumns is the column list every query feeding scanDeckRow selects,
// in scanDeckRow's own Scan order. One constant, so adding a column can't
// leave one of the three SELECTs (DeckByID, ListDecks, share.go's
// deckByIDIgnoringOwner) behind.
const deckColumns = `id, user_id, name, description, created_at, new_cards_per_day, reviews_per_day, snoozed_until, color`
```

Add the field to `Deck` (after `SnoozedUntil`):

```go
	// Color is one of DeckColors.
	Color string
```

In `CreateDeck`, set the default on the returned value — change the `d := Deck{...}` line to:

```go
	d := Deck{UserID: userID, Name: name, Description: description, CreatedAt: st.now(), NewCardsPerDay: DefaultNewCardsPerDay, Color: DefaultDeckColor}
```

Replace the two SELECT strings in `DeckByID` and `ListDecks`:

```go
// DeckByID fetches one of userID's own decks.
func (st *Store) DeckByID(ctx context.Context, userID, id int64) (Deck, error) {
	return scanDeck(st.db.QueryRowContext(ctx,
		`SELECT `+deckColumns+` FROM flash_decks WHERE id = ? AND user_id = ?`, id, userID))
}
```

```go
	rows, err := st.db.QueryContext(ctx,
		`SELECT `+deckColumns+` FROM flash_decks WHERE user_id = ?
		 ORDER BY created_at DESC, id DESC`, userID)
```

In `scanDeckRow`, add `&d.Color` as the last Scan destination:

```go
	err := row.Scan(&d.ID, &d.UserID, &d.Name, &d.Description, &createdAt,
		&d.NewCardsPerDay, &reviewsPerDay, &snoozedUntil, &d.Color)
```

Add the setter at the end of the file:

```go
// SetDeckColor changes userID's own deck's colour.
func (st *Store) SetDeckColor(ctx context.Context, userID, id int64, color string) (Deck, error) {
	if !ValidDeckColor(color) {
		return Deck{}, fmt.Errorf("%w: %q is not a deck colour", ErrInvalid, color)
	}
	res, err := st.db.ExecContext(ctx,
		`UPDATE flash_decks SET color = ? WHERE id = ? AND user_id = ?`, color, id, userID)
	if err != nil {
		return Deck{}, fmt.Errorf("flash: set deck colour: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return Deck{}, fmt.Errorf("flash: set deck colour: %w", err)
	}
	if n == 0 {
		return Deck{}, ErrNotFound
	}
	return st.DeckByID(ctx, userID, id)
}
```

- [ ] **Step 5: Update `share.go`**

In `deckByIDIgnoringOwner`, replace the SELECT with:

```go
	return scanDeck(tx.QueryRowContext(ctx,
		`SELECT `+deckColumns+` FROM flash_decks WHERE id = ?`, id))
```

In `AdoptShare`, the first-time branch's INSERT becomes (adds `color`):

```go
		err = tx.QueryRowContext(ctx,
			`INSERT INTO flash_decks (user_id, name, description, created_at, color)
			 VALUES (?, ?, ?, ?, ?)
			 RETURNING id`,
			toUserID, src.Name, src.Description, formatTime(st.now()), src.Color,
		).Scan(&targetDeckID)
```

(A merge into an existing adopted deck keeps the adopter's own colour — only the first-time copy takes the source colour.)

- [ ] **Step 6: Run the tests to verify they pass**

Run: `go test ./internal/apps/flash/ -count=1`
Expected: PASS (all existing Flash tests too — the migration and scan change must not break them).

- [ ] **Step 7: Commit**

```bash
git add internal/apps/flash/migrations/0010_deck_color.sql internal/apps/flash/deck.go internal/apps/flash/share.go internal/apps/flash/deck_test.go internal/apps/flash/share_test.go
git commit -m "feat(flash): store a colour per deck"
```

---

### Task 2: Deck summaries

**Files:**
- Create: `internal/apps/flash/deck_summary.go`
- Modify: `internal/apps/flash/review.go` (extract `dailyBudget`)
- Test: create `internal/apps/flash/deck_summary_test.go` (package `flash_test`) and `internal/apps/flash/deck_summary_internal_test.go` (package `flash`)

**Interfaces:**
- Consumes: `Store.ListDecks`, `Store.dailyCounts`, `Deck.IsSnoozed`, `formatDay`, `formatTime`, `parseTime` (all existing).
- Produces:
  ```go
  type DeckSummary struct {
      Deck       Deck
      CardCount  int        // every card in the deck
      Mastered   int        // cards in FSRS state "review" (same rule as the stats page)
      DueTotal   int        // review cards due now, before the daily review limit
      NewUnseen  int        // cards never reviewed, before the daily new-card limit
      DueToday   int        // review cards in today's queue (after the limit); 0 when snoozed
      NewToday   int        // new cards still allowed today (after the limit); 0 when snoozed
      ReviewNow  int        // DueToday + NewToday == len(DueQueue) for this deck
      NextDueAt  *time.Time // earliest due_at strictly after now, nil if none
      Snoozed    bool
  }
  func (st *Store) DeckSummaries(ctx context.Context, userID int64, now time.Time) ([]DeckSummary, error) // ListDecks order
  func dailyBudget(d Deck, newCount, reviewCount int) (reviewsRemaining, newRemaining int) // reviewsRemaining -1 = unlimited
  func nextCardsLabel(s DeckSummary, now time.Time) string
  ```

- [ ] **Step 1: Write the failing store test**

Create `internal/apps/flash/deck_summary_test.go`:

```go
// internal/apps/flash/deck_summary_test.go
package flash_test

import (
	"context"
	"testing"
	"time"

	"github.com/iliafrenkel/on-suite/internal/apps/flash"
)

// summaryFor returns the one summary for deckID, failing the test if it is
// missing.
func summaryFor(t *testing.T, sums []flash.DeckSummary, deckID int64) flash.DeckSummary {
	t.Helper()
	for _, s := range sums {
		if s.Deck.ID == deckID {
			return s
		}
	}
	t.Fatalf("no summary for deck %d in %+v", deckID, sums)
	return flash.DeckSummary{}
}

func TestDeckSummariesCountsAFreshDeck(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	now := time.Date(2026, 9, 23, 10, 0, 0, 0, time.UTC)
	d, err := f.store.CreateDeck(ctx, f.alice.ID, "Spanish", "")
	if err != nil {
		t.Fatal(err)
	}
	for _, front := range []string{"hola", "adiós", "gracias"} {
		if _, err := f.store.CreateCard(ctx, f.alice.ID, d.ID, flash.CardTypeBasic, front, "x", ""); err != nil {
			t.Fatal(err)
		}
	}

	sums, err := f.store.DeckSummaries(ctx, f.alice.ID, now)
	if err != nil {
		t.Fatal(err)
	}
	s := summaryFor(t, sums, d.ID)
	if s.CardCount != 3 || s.NewUnseen != 3 || s.NewToday != 3 || s.DueTotal != 0 || s.DueToday != 0 || s.ReviewNow != 3 {
		t.Errorf("summary = %+v, want 3 cards, all new and all allowed today", s)
	}
	if s.NextDueAt != nil || s.Mastered != 0 || s.Snoozed {
		t.Errorf("summary = %+v, want no next due, nothing mastered, not snoozed", s)
	}
}

// TestDeckSummariesAgreeWithDueQueue is the invariant the whole deck pane
// depends on: the number on the Review button must be exactly how many cards
// the review page will then show.
func TestDeckSummariesAgreeWithDueQueue(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	t0 := time.Date(2026, 9, 23, 10, 0, 0, 0, time.UTC)

	d, err := f.store.CreateDeck(ctx, f.alice.ID, "Spanish", "")
	if err != nil {
		t.Fatal(err)
	}
	limit := 2
	if _, err := f.store.UpdateDeckSettings(ctx, f.alice.ID, d.ID, 3, &limit); err != nil {
		t.Fatal(err)
	}
	var cards []flash.Card
	for i := 0; i < 6; i++ {
		c, err := f.store.CreateCard(ctx, f.alice.ID, d.ID, flash.CardTypeBasic, "front", "back", "")
		if err != nil {
			t.Fatal(err)
		}
		cards = append(cards, c)
	}
	// Grade three cards now: they leave the "new" pool and come back due
	// minutes later (learning steps), and they use up the whole new-card
	// budget of 3 for today.
	for _, c := range cards[:3] {
		if _, err := f.store.GradeCard(ctx, f.alice.ID, c.ID, flash.RatingAgain, t0); err != nil {
			t.Fatal(err)
		}
	}

	for _, now := range []time.Time{t0, t0.Add(30 * time.Minute), t0.Add(2 * time.Hour)} {
		sums, err := f.store.DeckSummaries(ctx, f.alice.ID, now)
		if err != nil {
			t.Fatal(err)
		}
		s := summaryFor(t, sums, d.ID)
		queue, err := f.store.DueQueue(ctx, f.alice.ID, &d.ID, now)
		if err != nil {
			t.Fatal(err)
		}
		if s.ReviewNow != len(queue) {
			t.Errorf("at %v: ReviewNow = %d, len(DueQueue) = %d; summary = %+v", now, s.ReviewNow, len(queue), s)
		}
		if s.ReviewNow != s.DueToday+s.NewToday {
			t.Errorf("at %v: ReviewNow %d != DueToday %d + NewToday %d", now, s.ReviewNow, s.DueToday, s.NewToday)
		}
		if s.NewUnseen != 3 || s.NewToday != 0 {
			t.Errorf("at %v: NewUnseen = %d, NewToday = %d; want 3 unseen and none left in today's budget", now, s.NewUnseen, s.NewToday)
		}
		if s.DueToday > limit {
			t.Errorf("at %v: DueToday = %d exceeds the review limit %d", now, s.DueToday, limit)
		}
	}
}

func TestDeckSummariesZeroTodayForASnoozedDeck(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	now := time.Date(2026, 9, 23, 10, 0, 0, 0, time.UTC)
	d, err := f.store.CreateDeck(ctx, f.alice.ID, "Spanish", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.CreateCard(ctx, f.alice.ID, d.ID, flash.CardTypeBasic, "hola", "hello", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.SnoozeDeck(ctx, f.alice.ID, d.ID, now.Add(48*time.Hour)); err != nil {
		t.Fatal(err)
	}
	sums, err := f.store.DeckSummaries(ctx, f.alice.ID, now)
	if err != nil {
		t.Fatal(err)
	}
	s := summaryFor(t, sums, d.ID)
	if !s.Snoozed || s.ReviewNow != 0 || s.NewToday != 0 || s.DueToday != 0 {
		t.Errorf("summary = %+v, want snoozed with nothing to review today", s)
	}
	if s.CardCount != 1 || s.NewUnseen != 1 {
		t.Errorf("summary = %+v, want the card still counted", s)
	}
}

func TestDeckSummariesAreOwnerScopedAndInListOrder(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	now := time.Now().UTC()
	older, err := f.store.CreateDeck(ctx, f.alice.ID, "Older", "")
	if err != nil {
		t.Fatal(err)
	}
	newer, err := f.store.CreateDeck(ctx, f.alice.ID, "Newer", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.CreateDeck(ctx, f.bob.ID, "Bob's", ""); err != nil {
		t.Fatal(err)
	}
	sums, err := f.store.DeckSummaries(ctx, f.alice.ID, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(sums) != 2 || sums[0].Deck.ID != newer.ID || sums[1].Deck.ID != older.ID {
		t.Errorf("DeckSummaries = %+v, want alice's two decks newest first", sums)
	}
}
```

- [ ] **Step 2: Write the failing label test**

Create `internal/apps/flash/deck_summary_internal_test.go`:

```go
// internal/apps/flash/deck_summary_internal_test.go
package flash

import (
	"testing"
	"time"
)

func TestNextCardsLabel(t *testing.T) {
	now := time.Date(2026, 9, 23, 10, 0, 0, 0, time.UTC)
	at := func(d time.Duration) *time.Time { v := now.Add(d); return &v }

	tests := []struct {
		name string
		s    DeckSummary
		want string
	}{
		{"no cards", DeckSummary{CardCount: 0}, ""},
		{"new cards held back by today's limit", DeckSummary{CardCount: 5, NewUnseen: 5, NewToday: 0}, "Next cards tomorrow"},
		{"reviews held back by today's limit", DeckSummary{CardCount: 5, DueTotal: 4, DueToday: 0}, "Next cards tomorrow"},
		{"later today", DeckSummary{CardCount: 5, NextDueAt: at(3 * time.Hour)}, "More cards later today"},
		{"tomorrow", DeckSummary{CardCount: 5, NextDueAt: at(20 * time.Hour)}, "Next cards tomorrow"},
		{"in 3 days", DeckSummary{CardCount: 5, NextDueAt: at(3 * 24 * time.Hour)}, "Next cards in 3 days"},
		{"nothing scheduled", DeckSummary{CardCount: 5}, ""},
	}
	for _, tt := range tests {
		if got := nextCardsLabel(tt.s, now); got != tt.want {
			t.Errorf("%s: nextCardsLabel = %q, want %q", tt.name, got, tt.want)
		}
	}
}
```

- [ ] **Step 3: Run the tests to verify they fail**

Run: `go test ./internal/apps/flash/ -run 'DeckSummar|NextCardsLabel' -count=1`
Expected: build failure — `flash.DeckSummary undefined`, `nextCardsLabel undefined`.

- [ ] **Step 4: Extract `dailyBudget` in `review.go`**

In `internal/apps/flash/review.go`, add this function right above `DueQueue`:

```go
// dailyBudget is how many more review and new cards deck d may put in
// today's queue, given what has already been graded today. A
// reviewsRemaining of -1 means unlimited. Shared by DueQueue and
// DeckSummaries so the Review button's count and the review page itself can
// never disagree about the limits.
func dailyBudget(d Deck, newCount, reviewCount int) (reviewsRemaining, newRemaining int) {
	reviewsRemaining = -1 // sentinel: unlimited
	if d.ReviewsPerDay != nil {
		reviewsRemaining = *d.ReviewsPerDay - reviewCount
		if reviewsRemaining < 0 {
			reviewsRemaining = 0
		}
	}
	newRemaining = d.NewCardsPerDay - newCount
	if newRemaining < 0 {
		newRemaining = 0
	}
	return reviewsRemaining, newRemaining
}
```

Then replace the body of the `for _, d := range decks` loop in `DueQueue` with:

```go
	for _, d := range decks {
		newCount, reviewCount, err := st.dailyCounts(ctx, userID, d.ID, now)
		if err != nil {
			return nil, err
		}
		reviewsRemaining, newRemaining := dailyBudget(d, newCount, reviewCount)

		due, err := st.dueReviewCards(ctx, userID, d, now, reviewsRemaining)
		if err != nil {
			return nil, err
		}
		reviews = append(reviews, due...)

		newCards, err := st.newQueueCards(ctx, userID, d, newRemaining)
		if err != nil {
			return nil, err
		}
		fresh = append(fresh, newCards...)
	}
```

- [ ] **Step 5: Implement `deck_summary.go`**

Create `internal/apps/flash/deck_summary.go`:

```go
// internal/apps/flash/deck_summary.go
package flash

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// DeckSummary is everything the home screen shows about one deck: its list
// row (stack, status line, due badge) and its pane (Review button, tiles,
// "next cards" line). See the UI overhaul spec §2.
type DeckSummary struct {
	Deck      Deck
	CardCount int // every card in the deck
	Mastered  int // cards in FSRS state "review" — the stats page's own rule
	DueTotal  int // review cards due now, before the daily review limit
	NewUnseen int // cards never reviewed, before the daily new-card limit
	DueToday  int // review cards in today's queue, after the limit; 0 when snoozed
	NewToday  int // new cards still allowed today, after the limit; 0 when snoozed
	// ReviewNow is DueToday + NewToday: exactly len(DueQueue) for this deck
	// (TestDeckSummariesAgreeWithDueQueue pins that).
	ReviewNow int
	NextDueAt *time.Time // earliest due_at strictly after now; nil if none
	Snoozed   bool
}

// DeckSummaries returns one DeckSummary per deck userID owns, in ListDecks
// order (newest first). One aggregate query per deck: a household has a
// handful of decks, and each query reads only that deck's cards.
func (st *Store) DeckSummaries(ctx context.Context, userID int64, now time.Time) ([]DeckSummary, error) {
	decks, err := st.ListDecks(ctx, userID)
	if err != nil {
		return nil, err
	}
	out := make([]DeckSummary, 0, len(decks))
	for _, d := range decks {
		s := DeckSummary{Deck: d, Snoozed: d.IsSnoozed(now)}
		var nextDue sql.NullString
		err := st.db.QueryRowContext(ctx, `
			SELECT count(*),
			       coalesce(sum(CASE WHEN s.state = 'review' THEN 1 ELSE 0 END), 0),
			       coalesce(sum(CASE WHEN s.card_id IS NOT NULL AND s.due_at <= ? THEN 1 ELSE 0 END), 0),
			       coalesce(sum(CASE WHEN s.card_id IS NULL THEN 1 ELSE 0 END), 0),
			       min(CASE WHEN s.due_at > ? THEN s.due_at END)
			  FROM flash_cards c
			  LEFT JOIN flash_card_state s ON s.card_id = c.id AND s.user_id = c.user_id
			 WHERE c.deck_id = ? AND c.user_id = ?`,
			formatTime(now), formatTime(now), d.ID, userID,
		).Scan(&s.CardCount, &s.Mastered, &s.DueTotal, &s.NewUnseen, &nextDue)
		if err != nil {
			return nil, fmt.Errorf("flash: deck summaries: %w", err)
		}
		if nextDue.Valid {
			t, err := parseTime(nextDue.String)
			if err != nil {
				return nil, err
			}
			s.NextDueAt = &t
		}

		if !s.Snoozed {
			newCount, reviewCount, err := st.dailyCounts(ctx, userID, d.ID, now)
			if err != nil {
				return nil, err
			}
			reviewsRemaining, newRemaining := dailyBudget(d, newCount, reviewCount)
			s.DueToday = s.DueTotal
			if reviewsRemaining >= 0 && s.DueToday > reviewsRemaining {
				s.DueToday = reviewsRemaining
			}
			s.NewToday = min(s.NewUnseen, newRemaining)
			s.ReviewNow = s.DueToday + s.NewToday
		}
		out = append(out, s)
	}
	return out, nil
}

// nextCardsLabel is the second line of the deck pane's "All done for today"
// panel: when this deck will next have something to review. "" means there
// is nothing useful to say (no cards, or nothing scheduled at all). Only
// meaningful when s.ReviewNow is 0 and the deck is not snoozed.
//
// Days are UTC calendar days, the same day boundary flash_review_counts and
// the daily limits use (formatDay).
func nextCardsLabel(s DeckSummary, now time.Time) string {
	switch {
	case s.CardCount == 0:
		return ""
	case s.DueTotal > s.DueToday || s.NewUnseen > s.NewToday:
		// Cards are waiting but today's limits hold them back; the budgets
		// reset at the next UTC day.
		return "Next cards tomorrow"
	case s.NextDueAt == nil:
		return ""
	}
	today, _ := time.Parse("2006-01-02", formatDay(now))
	next, _ := time.Parse("2006-01-02", formatDay(*s.NextDueAt))
	switch days := int(next.Sub(today).Hours() / 24); {
	case days <= 0:
		return "More cards later today"
	case days == 1:
		return "Next cards tomorrow"
	default:
		return fmt.Sprintf("Next cards in %d days", days)
	}
}
```

(`min` is the Go 1.21+ builtin; check `go.mod`'s `go` line is ≥ 1.21 — it is, the repo already uses newer features. If `go vet` complains, replace with an `if`.)

- [ ] **Step 6: Run the tests to verify they pass**

Run: `go test ./internal/apps/flash/ -count=1`
Expected: PASS, including every existing `DueQueue`/review test (the `dailyBudget` extraction must not change behaviour).

- [ ] **Step 7: Commit**

```bash
git add internal/apps/flash/deck_summary.go internal/apps/flash/deck_summary_test.go internal/apps/flash/deck_summary_internal_test.go internal/apps/flash/review.go
git commit -m "feat(flash): add per-deck summaries for the home screen"
```

---

### Task 3: Toolbar icons

**Files:**
- Modify: `internal/ui/toolbar_icons.go`, `internal/ui/toolbar_icons_test.go`

**Interfaces:**
- Produces: `{{ticon "edit"}}`, `{{ticon "trash"}}`, `{{ticon "cards"}}`, `{{ticon "play"}}`, `{{ticon "arrow-left"}}` — used by U1 templates and every later phase.

- [ ] **Step 1: Extend the icon test**

In `internal/ui/toolbar_icons_test.go`, `TestToolbarIconForKnownNames`, extend the `names` slice:

```go
	names := []string{
		"more", "plus", "refresh", "import", "export", "folder",
		"keyboard", "stats", "close", "check", "check-circle", "star-filled",
		"star-outline", "external", "doc", "expand", "inbox",
		"edit", "trash", "cards", "play", "arrow-left",
	}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `go test ./internal/ui/ -run TestToolbarIconForKnownNames -count=1`
Expected: FAIL — `ToolbarIconFor("edit") = "", want it to contain <svg`.

- [ ] **Step 3: Add the icons**

In `internal/ui/toolbar_icons.go`, add these entries to the `toolbarIcons` map (anywhere inside it; keep the existing entries). They use the same 24×24 stroke style as the rest; `app.css`'s `.toolbar-icon` rule supplies stroke and size:

```go
	"edit": `<svg class="toolbar-icon" viewBox="0 0 24 24" aria-hidden="true">
		<path d="M12 20h9"/>
		<path d="M16.5 3.5a2.1 2.1 0 0 1 3 3L7 19l-4 1 1-4Z"/>
	</svg>`,
	"trash": `<svg class="toolbar-icon" viewBox="0 0 24 24" aria-hidden="true">
		<path d="M3 6h18"/>
		<path d="M8 6V4h8v2"/>
		<path d="M19 6l-1 14H6L5 6"/>
	</svg>`,
	"cards": `<svg class="toolbar-icon" viewBox="0 0 24 24" aria-hidden="true">
		<rect x="3" y="7" width="13" height="14" rx="2"/>
		<path d="M8 3h11a2 2 0 0 1 2 2v12"/>
	</svg>`,
	"play": `<svg class="toolbar-icon" viewBox="0 0 24 24" aria-hidden="true">
		<path d="M7 4l13 8-13 8Z"/>
	</svg>`,
	"arrow-left": `<svg class="toolbar-icon" viewBox="0 0 24 24" aria-hidden="true">
		<path d="M19 12H5"/>
		<path d="M12 19l-7-7 7-7"/>
	</svg>`,
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./internal/ui/ -count=1`
Expected: PASS (the test also checks no two icons are identical).

- [ ] **Step 5: Commit**

```bash
git add internal/ui/toolbar_icons.go internal/ui/toolbar_icons_test.go
git commit -m "feat(ui): add edit, trash, cards, play and arrow-left toolbar icons"
```

---

### Task 4: Rename the Flash script to `flash.js`

**Files:**
- Rename: `internal/apps/flash/static/flash-review.js` → `internal/apps/flash/static/flash.js`
- Modify: `internal/apps/flash/flash.go`, `internal/apps/flash/templates/review.html`, `internal/apps/flash/handlers_review_test.go`

**Interfaces:**
- Produces: `GET /flash/flash.js` (signed-in only) — the single Flash script every later phase extends. Its file header must describe it as the app-wide script.

- [ ] **Step 1: Update the test first**

In `internal/apps/flash/handlers_review_test.go`, replace `TestReviewScriptIsServed` with:

```go
func TestFlashScriptIsServed(t *testing.T) {
	s := newServer(t)
	rec := s.Do(t, s.Alice, httpGet(t, "/flash/flash.js"))
	if rec.Code != 200 {
		t.Fatalf("GET /flash/flash.js = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "text/javascript; charset=utf-8" {
		t.Errorf("Content-Type = %q", ct)
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `go test ./internal/apps/flash/ -run TestFlashScriptIsServed -count=1`
Expected: FAIL — `GET /flash/flash.js = 404`.

- [ ] **Step 3: Rename and rewire**

```bash
git mv internal/apps/flash/static/flash-review.js internal/apps/flash/static/flash.js
```

Replace the first four comment lines of `internal/apps/flash/static/flash.js` (everything above `(function () {`) with:

```js
// internal/apps/flash/static/flash.js
//
// ON Flash's only script, loaded by every Flash page. Each feature is a
// small, independent piece keyed off elements that only exist on the page
// that needs them, so it is a no-op everywhere else (UI overhaul spec §1.6).
// Everything here is progressive enhancement: every page works without it.
//
// Review keyboard shortcuts: Space reveals the answer, 1-4 grade it
// (Again/Hard/Good/Easy), U undoes the last grade. Mirrors
// internal/apps/reader/static/reader.js's press()/keydown pattern.
```

In `internal/apps/flash/flash.go`:

- change `//go:embed static/flash-review.js` to `//go:embed static/flash.js`
- in `script`, change the doc comment's first line to `// script serves flash.js, behind the same sign-in requirement as` and the served path to `"static/flash.js"`:
  ```go
  	http.ServeFileFS(w, r, scriptFiles, "static/flash.js")
  ```
- change the route `r.HandleFunc("GET /flash-review.js", a.script)` to:
  ```go
  	r.HandleFunc("GET /flash.js", a.script)
  ```
- in the long review-route comment above it, replace the words `"review" and "flash-review.js" are literal single` with `"review" and "flash.js" are literal single`.

In `internal/apps/flash/templates/review.html`, change the head block to:

```html
{{define "head"}}
<script src="/flash/flash.js" defer></script>
{{end}}
```

- [ ] **Step 4: Run the Flash tests**

Run: `go test ./internal/apps/flash/ -count=1`
Expected: PASS. Also run `grep -rn "flash-review.js" internal` — expected: no output.

- [ ] **Step 5: Commit**

```bash
git add -A internal/apps/flash/static internal/apps/flash/flash.go internal/apps/flash/templates/review.html internal/apps/flash/handlers_review_test.go
git commit -m "refactor(flash): rename flash-review.js to flash.js"
```

---

### Task 5: Home screen handlers and templates

This task rewrites `decks.html` and the view model behind it. It is one task because the handler tests assert on the markup.

**Files:**
- Create: `internal/apps/flash/templates/components.partial.html`
- Modify: `internal/apps/flash/handlers_decks.go`, `internal/apps/flash/handlers_import.go` (only if needed — see Step 4), `internal/apps/flash/templates/decks.html` (rewrite), `internal/apps/flash/templates/import.partial.html`
- Test: `internal/apps/flash/handlers_decks_test.go`

**Interfaces:**
- Consumes: `DeckSummary`, `DeckSummaries`, `nextCardsLabel` (Task 2); `DeckColors`, `ValidDeckColor`, `SetDeckColor`, `DefaultDeckColor` (Task 1); icons (Task 3).
- Produces (later plans rely on these):
  - `func (a *App) buildDeckIndex(r *http.Request, userID int64, detail deckDetailView, oob bool) (deckIndexView, error)` — the one place the home view model is assembled.
  - `deckDetailView` gains `Summary DeckSummary`, `NextLabel string`, `ColorValue string`, `Colors []deckColorOption`, `HasDecks bool`.
  - `deckListItem` is now `{Deck Deck; CardCount, Due int; Status string; Snoozed bool}`.
  - `deckIndexView` gains `TotalDue int`.
  - `type deckColorOption struct{ Name, Label string }` and `func deckColorOptions() []deckColorOption`.
  - Template names: in `decks.html` — `flash-home-toolbar`, `flash-review-all` (OOB-able, id `flash-review-all`), `deck-list-items` (id `deck-list`), `deck-detail-body` (dispatcher, unchanged name), `deck-detail-view`, `deck-detail-empty`, `deck-detail-new`, `deck-detail-edit`, `deck-color-field`, `deck-detail-with-list`, and the mobile checkbox `#flash-detail-open`; in `components.partial.html` — `flash-stack`, `flash-pane-back`.
  - CSS class names listed in Task 6.

- [ ] **Step 1: Write the failing handler tests**

Append to `internal/apps/flash/handlers_decks_test.go` (add `"strings"` is already imported; also add `"github.com/iliafrenkel/on-suite/internal/apps/flash"` only if not already imported — it is):

```go
func TestCreateDeckWithColor(t *testing.T) {
	s := newServer(t)
	s.Submit(t, s.Alice, "/flash/new", url.Values{"name": {"Planets"}, "description": {""}, "color": {"purple"}}, "/flash/1")
	d, err := s.Store.DeckByID(t.Context(), s.Alice.User.ID, 1)
	if err != nil {
		t.Fatal(err)
	}
	if d.Color != "purple" {
		t.Errorf("Color = %q, want purple", d.Color)
	}
}

func TestCreateDeckWithoutColorIsTeal(t *testing.T) {
	s := newServer(t)
	s.Submit(t, s.Alice, "/flash/new", url.Values{"name": {"Planets"}}, "/flash/1")
	d, err := s.Store.DeckByID(t.Context(), s.Alice.User.ID, 1)
	if err != nil {
		t.Fatal(err)
	}
	if d.Color != flash.DefaultDeckColor {
		t.Errorf("Color = %q, want the default", d.Color)
	}
}

func TestCreateDeckRejectsUnknownColor(t *testing.T) {
	s := newServer(t)
	rec := s.Post(t, s.Alice, "/flash/new", url.Values{"name": {"Planets"}, "color": {"chartreuse"}})
	if rec.Code != 400 {
		t.Fatalf("unknown colour = %d, want 400", rec.Code)
	}
	htmlassert.Parse(t, rec.Body.String()).MustHave(".notice-error")
	if decks, _ := s.Store.ListDecks(t.Context(), s.Alice.User.ID); len(decks) != 0 {
		t.Errorf("a rejected create left %d decks behind", len(decks))
	}
}

func TestUpdateDeckChangesColor(t *testing.T) {
	s := newServer(t)
	deck, err := s.Store.CreateDeck(t.Context(), s.Alice.User.ID, "Spanish", "")
	if err != nil {
		t.Fatal(err)
	}
	s.Submit(t, s.Alice, "/flash/"+itoa(deck.ID), url.Values{
		"name": {"Spanish"}, "description": {""}, "color": {"amber"},
		"new_cards_per_day": {"20"}, "reviews_per_day": {""},
	}, "/flash/"+itoa(deck.ID))
	d, err := s.Store.DeckByID(t.Context(), s.Alice.User.ID, deck.ID)
	if err != nil {
		t.Fatal(err)
	}
	if d.Color != "amber" {
		t.Errorf("Color = %q, want amber", d.Color)
	}
}

func TestNewDeckFormHasEverySwatch(t *testing.T) {
	s := newServer(t)
	doc := s.Get(t, s.Alice, "/flash/new")
	swatches := doc.QueryAll(`input[name=color]`)
	if len(swatches) != len(flash.DeckColors) {
		t.Fatalf("new deck form has %d colour inputs, want %d", len(swatches), len(flash.DeckColors))
	}
	checked := doc.MustHave(`input[value=teal]`)
	if _, ok := htmlassert.Attr(checked, "checked"); !ok {
		t.Error("the default colour's swatch is not pre-checked")
	}
}

func TestDeckListShowsColorAndDueBadge(t *testing.T) {
	s := newServer(t)
	deck, err := s.Store.CreateDeck(t.Context(), s.Alice.User.ID, "Spanish", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Store.SetDeckColor(t.Context(), s.Alice.User.ID, deck.ID, "blue"); err != nil {
		t.Fatal(err)
	}
	for _, front := range []string{"hola", "adiós"} {
		if _, err := s.Store.CreateCard(t.Context(), s.Alice.User.ID, deck.ID, flash.CardTypeBasic, front, "x", ""); err != nil {
			t.Fatal(err)
		}
	}

	doc := s.Get(t, s.Alice, "/flash/")
	doc.MustHave(`#deck-list a.deck-row`)
	doc.MustHave(`#deck-list .deck-c-blue`)
	badge := doc.MustHave(`#deck-list .flash-due-badge`)
	if got := htmlassert.Text(badge); got != "2" {
		t.Errorf("due badge = %q, want 2", got)
	}
	reviewAll := doc.MustHave(`#flash-review-all`)
	if !strings.Contains(htmlassert.Text(reviewAll), "2") {
		t.Errorf("Review all = %q, want it to show the 2 cards due", htmlassert.Text(reviewAll))
	}
}

func TestDeckPaneShowsReviewButtonWithCount(t *testing.T) {
	s := newServer(t)
	deck, err := s.Store.CreateDeck(t.Context(), s.Alice.User.ID, "Spanish", "")
	if err != nil {
		t.Fatal(err)
	}
	for _, front := range []string{"hola", "adiós", "gracias"} {
		if _, err := s.Store.CreateCard(t.Context(), s.Alice.User.ID, deck.ID, flash.CardTypeBasic, front, "x", ""); err != nil {
			t.Fatal(err)
		}
	}

	doc := s.Get(t, s.Alice, "/flash/"+itoa(deck.ID))
	cta := doc.MustHave(`a.flash-review-cta`)
	if href, _ := htmlassert.Attr(cta, "href"); href != "/flash/review/"+itoa(deck.ID) {
		t.Errorf("Review button href = %q", href)
	}
	if got := htmlassert.Text(cta); !strings.Contains(got, "Review 3 cards") {
		t.Errorf("Review button text = %q, want it to say Review 3 cards", got)
	}
	doc.MustNotHave(`.flash-all-done`)
	if tiles := doc.QueryAll(`.flash-deck-tiles .flash-stat-tile`); len(tiles) != 4 {
		t.Errorf("deck pane has %d tiles, want 4", len(tiles))
	}
}

func TestDeckPaneShowsAllDoneWhenNothingIsDue(t *testing.T) {
	s := newServer(t)
	deck, err := s.Store.CreateDeck(t.Context(), s.Alice.User.ID, "Spanish", "")
	if err != nil {
		t.Fatal(err)
	}
	card, err := s.Store.CreateCard(t.Context(), s.Alice.User.ID, deck.ID, flash.CardTypeBasic, "hola", "hello", "")
	if err != nil {
		t.Fatal(err)
	}
	// Easy on a new card schedules it days out, so nothing is due now.
	if _, err := s.Store.GradeCard(t.Context(), s.Alice.User.ID, card.ID, flash.RatingEasy, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}

	doc := s.Get(t, s.Alice, "/flash/"+itoa(deck.ID))
	doc.MustNotHave(`a.flash-review-cta`)
	done := doc.MustHave(`.flash-all-done`)
	if !strings.Contains(htmlassert.Text(done), "All done for today") {
		t.Errorf("all-done panel = %q", htmlassert.Text(done))
	}
}

func TestDeckPaneForAnEmptyDeckOffersToAddCards(t *testing.T) {
	s := newServer(t)
	deck, err := s.Store.CreateDeck(t.Context(), s.Alice.User.ID, "Spanish", "")
	if err != nil {
		t.Fatal(err)
	}
	doc := s.Get(t, s.Alice, "/flash/"+itoa(deck.ID))
	add := doc.MustHave(`a.flash-add-first-card`)
	if href, _ := htmlassert.Attr(add, "href"); href != "/flash/"+itoa(deck.ID)+"/cards/new" {
		t.Errorf("add-cards href = %q", href)
	}
}

func TestFirstRunShowsWelcome(t *testing.T) {
	s := newServer(t)
	doc := s.Get(t, s.Alice, "/flash/")
	welcome := doc.MustHave(`.flash-welcome`)
	if !strings.Contains(htmlassert.Text(welcome), "Make your first deck") {
		t.Errorf("welcome text = %q", htmlassert.Text(welcome))
	}
	doc.MustHave(`.flash-welcome a[href="/flash/new"]`)
	doc.MustHave(`.flash-welcome a[href="/flash/import"]`)
}

func TestHomeWithDecksButNoneSelectedAsksToPick(t *testing.T) {
	s := newServer(t)
	if _, err := s.Store.CreateDeck(t.Context(), s.Alice.User.ID, "Spanish", ""); err != nil {
		t.Fatal(err)
	}
	doc := s.Get(t, s.Alice, "/flash/")
	doc.MustNotHave(`.flash-welcome`)
	doc.MustHave(`.flash-pane-hint`)
}

func TestHomeToolbarLinks(t *testing.T) {
	s := newServer(t)
	doc := s.Get(t, s.Alice, "/flash/")
	for _, sel := range []string{
		`.flash-home-toolbar a[href="/flash/new"]`,
		`.flash-home-toolbar a[href="/flash/import"]`,
		`.flash-home-toolbar a[href="/flash/review"]`,
		`.flash-home-toolbar a[href="/flash/stats"]`,
	} {
		doc.MustHave(sel)
	}
}

func TestDeckFragmentCarriesOutOfBandListAndToolbar(t *testing.T) {
	s := newServer(t)
	rec := s.PostHX(t, s.Alice, "/flash/new", url.Values{"name": {"Spanish"}, "color": {"green"}})
	if rec.Code != 201 {
		t.Fatalf("create over HTMX = %d, want 201; body: %s", rec.Code, rec.Body.String())
	}
	doc := htmlassert.Parse(t, rec.Body.String())
	for _, id := range []string{"#deck-list", "#flash-review-all", "#flash-detail-open", "#shared-with-me"} {
		n := doc.MustHave(id)
		if _, ok := htmlassert.Attr(n, "hx-swap-oob"); !ok {
			t.Errorf("%s in the fragment is not marked hx-swap-oob", id)
		}
	}
	doc.MustHave(`#deck-list .deck-c-green`)
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/apps/flash/ -run 'Color|DueBadge|DeckPane|FirstRun|NoneSelected|HomeToolbar|OutOfBandListAndToolbar' -count=1`
Expected: FAIL (missing classes/fields; colour is ignored).

- [ ] **Step 3: Update the view model in `handlers_decks.go`**

Replace the `deckDetailView`, `deckListItem`, `deckListFragment` and `deckIndexView` type declarations with:

```go
// deckDetailView is what the deck detail pane renders, in any mode.
type deckDetailView struct {
	Mode      string
	Deck      Deck
	CSRFToken string

	// Summary and NextLabel are filled in by buildDeckIndex for Mode
	// "view" — the Review button, tiles, and "next cards" line.
	Summary   DeckSummary
	NextLabel string

	// HasDecks is filled in by buildDeckIndex for the empty mode: false
	// shows the first-run welcome, true a "pick a deck" hint.
	HasDecks bool

	ShareRecipients []auth.Account      // every other account, for the Share dropdown
	SharedWith      []shareWithUsername // this deck's own share offers, for the creator's list

	NameValue        string
	DescriptionValue string
	ColorValue       string
	Colors           []deckColorOption
	Error            string

	NewCardsPerDayValue string
	ReviewsPerDayValue  string

	PayloadValue string
	FormatValue  string
}

// deckColorOption is one swatch in the colour picker.
type deckColorOption struct {
	Name  string // a DeckColors entry, the radio value and the deck-c-* class suffix
	Label string // its accessible name, e.g. "Teal"
}

// deckColorOptions lists every DeckColors entry with a label.
func deckColorOptions() []deckColorOption {
	out := make([]deckColorOption, len(DeckColors))
	for i, name := range DeckColors {
		out[i] = deckColorOption{Name: name, Label: upperFirst(name)}
	}
	return out
}

// deckListItem is one row in the deck list: a projection of DeckSummary
// down to what the row shows.
type deckListItem struct {
	Deck      Deck
	CardCount int
	Due       int    // ReviewNow; the badge shows when > 0
	Status    string // the row's second line
	Snoozed   bool
}

func newDeckListItem(s DeckSummary) deckListItem {
	item := deckListItem{Deck: s.Deck, CardCount: s.CardCount, Due: s.ReviewNow, Snoozed: s.Snoozed}
	switch {
	case s.Snoozed:
		item.Status = "taking a break"
	case s.CardCount == 0:
		item.Status = "no cards yet"
	case s.ReviewNow == 0:
		item.Status = "all done"
	case s.CardCount == 1:
		item.Status = "1 card"
	default:
		item.Status = strconv.Itoa(s.CardCount) + " cards"
	}
	return item
}

type deckListFragment struct {
	Items    []deckListItem
	ActiveID int64
	OOB      bool
}

type deckIndexView struct {
	List         deckListFragment
	Detail       deckDetailView
	Title        string
	Shell        render.Shell
	SharedWithMe []shareOfferWithUsername // pending offers addressed to the viewer
	TotalDue     int                      // sum of every deck's ReviewNow: the "Review all" count
}
```

Delete the old `deckListItems` method (it is replaced by `buildDeckIndex` below).

Change `newDeckDetail` and `editDeckDetail` to carry the colour:

```go
func (a *App) newDeckDetail(r *http.Request, errMsg, name, description, color string) deckDetailView {
	if color == "" {
		color = DefaultDeckColor
	}
	return deckDetailView{
		Mode: deckModeNew, NameValue: name, DescriptionValue: description,
		ColorValue: color, Colors: deckColorOptions(),
		Error: errMsg, CSRFToken: web.CSRFToken(r.Context()),
	}
}

func (a *App) editDeckDetail(r *http.Request, d Deck, errMsg, name, description, color, newCardsPerDay, reviewsPerDay string) deckDetailView {
	if color == "" {
		color = d.Color
	}
	return deckDetailView{
		Mode: "edit", Deck: d, NameValue: name, DescriptionValue: description,
		ColorValue: color, Colors: deckColorOptions(),
		NewCardsPerDayValue: newCardsPerDay, ReviewsPerDayValue: reviewsPerDay,
		Error: errMsg, CSRFToken: web.CSRFToken(r.Context()),
	}
}
```

Replace `renderDeckIndex` and `renderDeckDetailWithList` with these three functions:

```go
// buildDeckIndex assembles the home screen's whole view model — list,
// pane, toolbar count, gift offers — for both the full-page and HTMX paths,
// so the two can never compute any of it differently. oob marks the list
// for an out-of-band swap (every fragment response sets it).
func (a *App) buildDeckIndex(r *http.Request, userID int64, detail deckDetailView, oob bool) (deckIndexView, error) {
	ctx := r.Context()
	now := a.store.now()
	sums, err := a.store.DeckSummaries(ctx, userID, now)
	if err != nil {
		return deckIndexView{}, err
	}
	offers, err := a.sharedWithMeForViewer(ctx, userID)
	if err != nil {
		return deckIndexView{}, err
	}

	items := make([]deckListItem, 0, len(sums))
	total := 0
	for _, s := range sums {
		items = append(items, newDeckListItem(s))
		total += s.ReviewNow
		if detail.Mode == deckModeView && s.Deck.ID == detail.Deck.ID {
			detail.Summary = s
			detail.NextLabel = nextCardsLabel(s, now)
		}
	}
	if detail.Mode == "" {
		detail.HasDecks = len(sums) > 0 || len(offers) > 0
	}

	return deckIndexView{
		List:         deckListFragment{Items: items, ActiveID: detail.Deck.ID, OOB: oob},
		Detail:       detail,
		SharedWithMe: offers,
		TotalDue:     total,
	}, nil
}

// renderDeckIndex renders the home screen: the whole page on a normal
// request, or just the pane (plus out-of-band list/toolbar/checkbox) on an
// HTMX one. status is only used for the full page; an HTMX navigation is
// always 200 (an HTMX 4xx would not be swapped in).
func (a *App) renderDeckIndex(w http.ResponseWriter, r *http.Request, userID int64, status int, detail deckDetailView) {
	if web.IsHTMX(r) && !web.IsHTMXHistoryRestore(r) {
		a.renderDeckDetailWithList(w, r, userID, http.StatusOK, detail)
		return
	}
	view, err := a.buildDeckIndex(r, userID, detail, false)
	if err != nil {
		a.deps.Errors.Internal(w, r, err)
		return
	}
	page := a.deps.Page(r, deckPageTitle(detail))
	page.Data = view
	a.render(w, r, status, "flash/decks", page)
}

// renderDeckDetailWithList is the HTMX response for anything that changes
// the pane: the pane itself plus out-of-band copies of everything outside it
// that may have changed with it.
func (a *App) renderDeckDetailWithList(w http.ResponseWriter, r *http.Request, userID int64, status int, detail deckDetailView) {
	view, err := a.buildDeckIndex(r, userID, detail, true)
	if err != nil {
		a.deps.Errors.Internal(w, r, err)
		return
	}
	page := a.deps.Page(r, deckPageTitle(detail))
	view.Title, view.Shell = page.Title, page.Shell
	if err := a.deps.Render.Fragment(w, status, "flash/decks", "deck-detail-with-list", view); err != nil {
		a.deps.Errors.Internal(w, r, err)
	}
}
```

> Note: the old `renderDeckIndex` HTMX branch always answered 200 even when
> `status` was 400; this keeps that behaviour (the validation tests post
> without `HX-Request`, so they still see 400).

Update the callers:

- `newDeckForm`: `a.renderDeckIndex(w, r, userID, http.StatusOK, a.newDeckDetail(r, "", "", "", ""))`
- `editDeckForm`: `a.editDeckDetail(r, d, "", d.Name, d.Description, d.Color, strconv.Itoa(d.NewCardsPerDay), reviewsStr)`

Replace `createDeck` with:

```go
func (a *App) createDeck(w http.ResponseWriter, r *http.Request) {
	userID, ok := a.userID(w, r)
	if !ok {
		return
	}
	name := r.PostFormValue("name")
	description := r.PostFormValue("description")
	color := r.PostFormValue("color")
	if color == "" {
		color = DefaultDeckColor
	}
	reject := func(err error) {
		a.renderDeckIndex(w, r, userID, http.StatusBadRequest, a.newDeckDetail(r, userMessage(err), name, description, color))
	}

	if err := ValidateDeck(name, description); err != nil {
		reject(err)
		return
	}
	if !ValidDeckColor(color) {
		reject(fmt.Errorf("%w: pick one of the colours shown", ErrInvalid))
		return
	}
	d, err := a.store.CreateDeck(r.Context(), userID, name, description)
	if err != nil {
		if errors.Is(err, ErrInvalid) {
			reject(err)
			return
		}
		a.deps.Errors.Internal(w, r, err)
		return
	}
	if color != d.Color {
		if d, err = a.store.SetDeckColor(r.Context(), userID, d.ID, color); err != nil {
			a.fail(w, r, err)
			return
		}
	}

	if !web.IsHTMX(r) {
		http.Redirect(w, r, "/flash/"+strconv.FormatInt(d.ID, 10), http.StatusSeeOther)
		return
	}
	w.Header().Set("HX-Push-Url", "/flash/"+strconv.FormatInt(d.ID, 10))
	recipients, shares, err := a.shareContext(r.Context(), userID, d.ID)
	if err != nil {
		a.deps.Errors.Internal(w, r, err)
		return
	}
	a.renderDeckDetailWithList(w, r, userID, http.StatusCreated, a.viewDeckDetail(r, userID, d, recipients, shares))
}
```

In `updateDeck`:

- read the colour next to the other fields: `color := r.PostFormValue("color")` (blank means "keep the current colour": `if color == "" { color = d.Color }`).
- change every `a.editDeckDetail(r, d, userMessage(err), name, description, newCardsPerDayStr, reviewsPerDayStr)` call to pass `color` after `description`: `a.editDeckDetail(r, d, userMessage(err), name, description, color, newCardsPerDayStr, reviewsPerDayStr)`.
- after the `ValidateDeckSettings` check and before `a.store.UpdateDeck(...)`, add:

```go
	if !ValidDeckColor(color) {
		a.renderDeckIndex(w, r, userID, http.StatusBadRequest,
			a.editDeckDetail(r, d, "Pick one of the colours shown.", name, description, d.Color, newCardsPerDayStr, reviewsPerDayStr))
		return
	}
```

- after the `UpdateDeckSettings` call succeeds, add:

```go
	if updated.Color != color {
		if updated, err = a.store.SetDeckColor(r.Context(), userID, id, color); err != nil {
			a.fail(w, r, err)
			return
		}
	}
```

`deleteDeck`, `declineShareHandler` and the others that render `deckDetailView{}` need no change: an empty Mode becomes the welcome or the "pick a deck" hint.

- [ ] **Step 4: Check the import handler still compiles**

`handlers_import.go` builds its own `deckDetailView` (Mode `import`) and calls `renderDeckIndex`/`renderDeckDetailWithList` with unchanged signatures — no change needed. Run `go build ./...` — expected: success. If the compiler reports `a.deckListItems undefined` anywhere, that caller must switch to `buildDeckIndex`.

- [ ] **Step 5: Create the shared components partial**

Every `*.partial.html` is parsed into **every** Flash page's template set
(see `render.AddApp`), so small pieces more than one page needs live here —
never in a page file, or the other pages can't call them. Create
`internal/apps/flash/templates/components.partial.html`:

```html
{{/* internal/apps/flash/templates/components.partial.html — small pieces
     shared by more than one Flash page. A *.partial.html is parsed into
     every one of this app's page template sets (render.AddApp). */}}

{{/* flash-stack is a deck drawn as three stacked cards in the deck's
     colour. Pass a dict: Size is "sm" (list rows) or "lg" (hero); Color is
     optional — without it the colour comes from a deck-c-* class on an
     ancestor. */}}
{{define "flash-stack"}}<span class="flash-stack flash-stack-{{.Size}}{{with .Color}} deck-c-{{.}}{{end}}" aria-hidden="true"><span></span><span></span><span></span></span>{{end}}

{{/* flash-pane-back is the "← Decks" button a narrow screen needs to get
     from the pane back to the list; app.css hides it on wide screens. */}}
{{define "flash-pane-back"}}<a class="toolbar-btn flash-back-btn" href="/flash/" hx-get="/flash/" hx-target="#deck-detail" hx-push-url="true">{{ticon "arrow-left"}}Decks</a>{{end}}
```

- [ ] **Step 6: Rewrite `templates/decks.html`**

Replace the whole file with (note it does **not** define `flash-stack` or
`flash-pane-back` — those come from the partial above; defining them twice
is a template parse error):

```html
{{/* internal/apps/flash/templates/decks.html — the ON Flash home screen
     (UI overhaul spec §2): app toolbar, deck list on the left, the deck
     pane on the right. Mirrors internal/apps/paste's split view, including
     its CSS-only narrow-screen toggle (#flash-detail-open). */}}

{{define "head"}}
<script src="/flash/flash.js" defer></script>
{{end}}

{{/* deck-detail-body dispatches on the pane's mode. */}}
{{define "deck-detail-body"}}
{{if eq .Mode "view"}}{{template "deck-detail-view" .}}
{{else if eq .Mode "edit"}}{{template "deck-detail-edit" .}}
{{else if eq .Mode "new"}}{{template "deck-detail-new" .}}
{{else if eq .Mode "import"}}{{template "deck-detail-import" .}}
{{else}}{{template "deck-detail-empty" .}}
{{end}}
{{end}}

{{/* deck-detail-with-list is the whole HTMX response for anything that
     changes the pane. Everything outside #deck-detail that can change with
     it rides along out of band: the crumb, the deck list, the Review-all
     count, the gift/shared list, and the narrow-screen checkbox (checked
     whenever the pane has something to show, so a phone or portrait tablet
     flips to the pane). */}}
{{define "deck-detail-with-list"}}<title>{{if .Title}}{{.Title}} · {{end}}ON Suite</title><span id="shell-crumb-tail" hx-swap-oob="true">{{template "shell-crumb-tail" (dict "Title" .Title "Shell" .Shell)}}</span>{{template "deck-detail-body" .Detail}}{{template "deck-list-items" .List}}{{template "flash-review-all" (dict "TotalDue" .TotalDue "OOB" true)}}<div id="shared-with-me" class="flash-shared-with-me" hx-swap-oob="true">{{template "shared-with-me" (dict "Offers" .SharedWithMe "CSRFToken" .Shell.CSRFToken)}}</div><input type="checkbox" id="flash-detail-open" class="visually-hidden" hx-swap-oob="true"{{if .Detail.Mode}} checked{{end}} aria-hidden="true" tabindex="-1">{{end}}

{{/* flash-review-all is the toolbar's Review-all button. It shows the
     total due count, and is re-sent out of band with every pane change. */}}
{{define "flash-review-all"}}<a id="flash-review-all" class="toolbar-btn" href="/flash/review"{{if .OOB}} hx-swap-oob="true"{{end}}>{{ticon "play"}}Review all{{if .TotalDue}} <span class="flash-due-badge">{{.TotalDue}}</span>{{end}}</a>{{end}}

{{define "flash-home-toolbar"}}
<div class="notes-toolbar flash-home-toolbar">
	<div class="notes-toolbar-actions">
		<a class="toolbar-btn toolbar-btn-active" href="/flash/new" hx-get="/flash/new" hx-target="#deck-detail" hx-push-url="true">{{ticon "plus"}}New deck</a>
		<a class="toolbar-btn" href="/flash/import" hx-get="/flash/import" hx-target="#deck-detail" hx-push-url="true">{{ticon "import"}}Import</a>
		{{template "flash-review-all" (dict "TotalDue" .TotalDue "OOB" false)}}
		<a class="toolbar-btn" href="/flash/stats">{{ticon "stats"}}Stats</a>
	</div>
</div>
{{end}}

{{define "deck-detail-empty"}}
{{if .HasDecks}}
<p class="dim flash-pane-hint">Pick a deck on the left to see it here.</p>
{{else}}
<div class="flash-welcome">
	{{template "flash-stack" (dict "Size" "lg" "Color" "teal")}}
	<h1>Make your first deck</h1>
	<p class="dim">A deck is a pile of cards about one topic. Make one yourself, or paste in cards an AI wrote for you.</p>
	<div class="flash-welcome-actions">
		<a class="flash-big-btn flash-big-btn-primary" href="/flash/new" hx-get="/flash/new" hx-target="#deck-detail" hx-push-url="true">{{ticon "plus"}}Create a deck</a>
		<a class="flash-big-btn" href="/flash/import" hx-get="/flash/import" hx-target="#deck-detail" hx-push-url="true">{{ticon "import"}}Import a deck</a>
	</div>
</div>
{{end}}
{{end}}

{{define "deck-detail-view"}}
<div class="flash-deck-view deck-c-{{.Deck.Color}}" id="deck-detail-view">
	<div class="notes-toolbar flash-deck-toolbar">
		{{template "flash-pane-back"}}
		<div class="notes-toolbar-actions">
			<a class="toolbar-btn" href="/flash/{{.Deck.ID}}/cards/">{{ticon "cards"}}Cards</a>
			<a class="toolbar-btn" href="/flash/edit/{{.Deck.ID}}" hx-get="/flash/edit/{{.Deck.ID}}" hx-target="#deck-detail" hx-push-url="true">{{ticon "edit"}}Edit</a>
			<form method="post" action="/flash/{{.Deck.ID}}/delete" hx-post="/flash/{{.Deck.ID}}/delete" hx-target="#deck-detail" hx-swap="innerHTML"
			      data-confirm="Delete “{{.Deck.Name}}” and all its cards? This can't be undone.">
				<input type="hidden" name="{{csrfField}}" value="{{.CSRFToken}}">
				<button type="submit" class="toolbar-btn danger">{{ticon "trash"}}Delete</button>
			</form>
		</div>
	</div>

	<div class="flash-deck-hero">
		{{template "flash-stack" (dict "Size" "lg")}}
		<div class="flash-deck-hero-text">
			<h1>{{.Deck.Name}}</h1>
			{{with .Deck.Description}}<p class="dim">{{.}}</p>{{end}}
			{{if .Summary.Snoozed}}
			{{/* U5 replaces this with the "taking a break" banner. */}}
			<div class="notice row">
				<span>Snoozed until {{.Deck.SnoozedUntil.Format "2 Jan 2006"}}.</span>
				<form method="post" action="/flash/{{.Deck.ID}}/unsnooze" hx-post="/flash/{{.Deck.ID}}/unsnooze" hx-target="#deck-detail" hx-swap="innerHTML">
					<input type="hidden" name="{{csrfField}}" value="{{.CSRFToken}}">
					<button type="submit" class="button">Unsnooze</button>
				</form>
			</div>
			{{else if .Summary.ReviewNow}}
			<a class="flash-review-cta" href="/flash/review/{{.Deck.ID}}">Review {{.Summary.ReviewNow}} card{{if ne .Summary.ReviewNow 1}}s{{end}} <span aria-hidden="true">→</span></a>
			{{else if eq .Summary.CardCount 0}}
			<div class="flash-all-done" role="status">
				<strong>This deck has no cards yet</strong>
				<a class="flash-big-btn flash-big-btn-primary flash-add-first-card" href="/flash/{{.Deck.ID}}/cards/new">{{ticon "plus"}}Add cards</a>
			</div>
			{{else}}
			<div class="flash-all-done" role="status">
				<strong>All done for today</strong>
				{{with .NextLabel}}<span class="dim">{{.}}</span>{{end}}
			</div>
			{{end}}
		</div>
	</div>

	<div class="flash-stat-tiles flash-deck-tiles">
		<div class="flash-stat-tile"><div class="value">{{.Summary.DueToday}}</div><div class="label">due today</div></div>
		<div class="flash-stat-tile"><div class="value">{{.Summary.NewToday}}</div><div class="label">new today</div></div>
		<div class="flash-stat-tile"><div class="value">{{.Summary.CardCount}}</div><div class="label">cards</div></div>
		<div class="flash-stat-tile"><div class="value">{{.Summary.Mastered}}</div><div class="label">mastered</div></div>
	</div>

	{{/* Share and snooze keep their F5/F2 controls until U5 moves them
	     into the toolbar popover and the Edit pane. */}}
	{{if .ShareRecipients}}
	<section class="flash-deck-section">
		<h2 class="flash-section-title">Share</h2>
		<form class="row" method="post" action="/flash/{{.Deck.ID}}/share" hx-post="/flash/{{.Deck.ID}}/share" hx-target="#deck-detail" hx-swap="innerHTML">
			<input type="hidden" name="{{csrfField}}" value="{{.CSRFToken}}">
			<label class="visually-hidden" for="share-recipient-{{.Deck.ID}}">Share with</label>
			<select id="share-recipient-{{.Deck.ID}}" name="to_user_id">
				{{range .ShareRecipients}}<option value="{{.ID}}">{{.Username}}</option>{{end}}
			</select>
			<button type="submit" class="button">Share</button>
		</form>
		{{if .SharedWith}}
		<ul class="flash-share-list">
			{{range .SharedWith}}
			<li class="row">
				<span>{{.ToUsername}} — {{.Status}}</span>
				{{if eq .Status "pending"}}
				<form method="post" action="/flash/{{$.Deck.ID}}/share/{{.ID}}/revoke" hx-post="/flash/{{$.Deck.ID}}/share/{{.ID}}/revoke" hx-target="#deck-detail" hx-swap="innerHTML">
					<input type="hidden" name="{{csrfField}}" value="{{$.CSRFToken}}">
					<button type="submit" class="toolbar-btn">Revoke</button>
				</form>
				{{end}}
			</li>
			{{end}}
		</ul>
		{{end}}
	</section>
	{{end}}
	{{if not .Summary.Snoozed}}
	<section class="flash-deck-section">
		<h2 class="flash-section-title">Take a break</h2>
		<div class="row">
			<form method="post" action="/flash/{{.Deck.ID}}/snooze" hx-post="/flash/{{.Deck.ID}}/snooze" hx-target="#deck-detail" hx-swap="innerHTML">
				<input type="hidden" name="{{csrfField}}" value="{{.CSRFToken}}">
				<input type="hidden" name="days" value="7">
				<button type="submit" class="toolbar-btn">1 week</button>
			</form>
			<form method="post" action="/flash/{{.Deck.ID}}/snooze" hx-post="/flash/{{.Deck.ID}}/snooze" hx-target="#deck-detail" hx-swap="innerHTML">
				<input type="hidden" name="{{csrfField}}" value="{{.CSRFToken}}">
				<input type="hidden" name="days" value="30">
				<button type="submit" class="toolbar-btn">1 month</button>
			</form>
		</div>
	</section>
	{{end}}
</div>
{{end}}

{{/* deck-color-field is the swatch picker: one visually-hidden radio per
     colour, each followed by the <label> that is its visible swatch. Pass
     a dict with Colors ([]deckColorOption), Value (the checked name) and
     IDSuffix (keeps ids unique between the New and Edit forms). */}}
{{define "deck-color-field"}}
<fieldset class="field flash-color-field">
	<legend>Colour</legend>
	<div class="flash-swatches">
		{{range .Colors}}
		<input type="radio" class="visually-hidden flash-swatch-input" name="color" value="{{.Name}}" id="deck-color-{{$.IDSuffix}}-{{.Name}}"{{if eq .Name $.Value}} checked{{end}}>
		<label class="flash-swatch deck-c-{{.Name}}" for="deck-color-{{$.IDSuffix}}-{{.Name}}"><span class="visually-hidden">{{.Label}}</span></label>
		{{end}}
	</div>
</fieldset>
{{end}}

{{define "deck-detail-new"}}
<form class="stack flash-deck-form" id="deck-detail-new" method="post" action="/flash/new" hx-post="/flash/new" hx-target="#deck-detail" hx-swap="innerHTML">
	<input type="hidden" name="{{csrfField}}" value="{{.CSRFToken}}">
	<div class="notes-toolbar flash-deck-toolbar">{{template "flash-pane-back"}}</div>
	<h1>New deck</h1>
	{{with .Error}}<div class="notice notice-error" role="alert">{{.}}</div>{{end}}
	<div class="field">
		<label for="deck-name">Name</label>
		<input id="deck-name" type="text" name="name" value="{{.NameValue}}" placeholder="Spanish for our trip" autofocus required>
	</div>
	<div class="field">
		<label for="deck-description">What's it about? (optional)</label>
		<textarea id="deck-description" name="description" rows="3">{{.DescriptionValue}}</textarea>
	</div>
	{{template "deck-color-field" (dict "Colors" .Colors "Value" .ColorValue "IDSuffix" "new")}}
	<div class="row">
		<button type="submit" class="primary">Create deck</button>
		<a class="toolbar-btn" href="/flash/" hx-get="/flash/" hx-target="#deck-detail" hx-push-url="true">Cancel</a>
	</div>
</form>
{{end}}

{{define "deck-detail-edit"}}
<form class="stack flash-deck-form" id="deck-detail-edit" method="post" action="/flash/{{.Deck.ID}}" hx-post="/flash/{{.Deck.ID}}" hx-target="#deck-detail" hx-swap="innerHTML">
	<input type="hidden" name="{{csrfField}}" value="{{.CSRFToken}}">
	<div class="notes-toolbar flash-deck-toolbar">{{template "flash-pane-back"}}</div>
	<h1>Edit deck</h1>
	{{with .Error}}<div class="notice notice-error" role="alert">{{.}}</div>{{end}}
	<div class="field">
		<label for="deck-name-{{.Deck.ID}}">Name</label>
		<input id="deck-name-{{.Deck.ID}}" type="text" name="name" value="{{.NameValue}}" autofocus required>
	</div>
	<div class="field">
		<label for="deck-description-{{.Deck.ID}}">What's it about? (optional)</label>
		<textarea id="deck-description-{{.Deck.ID}}" name="description" rows="3">{{.DescriptionValue}}</textarea>
	</div>
	{{template "deck-color-field" (dict "Colors" .Colors "Value" .ColorValue "IDSuffix" .Deck.ID)}}
	<h2 class="flash-section-title">Daily limits</h2>
	<p class="faint">How many cards this deck may show each day, so a big deck doesn't arrive all at once.</p>
	<div class="row field-row">
		<div class="field">
			<label for="deck-new-per-day-{{.Deck.ID}}">New cards per day</label>
			<input id="deck-new-per-day-{{.Deck.ID}}" type="number" min="0" name="new_cards_per_day" value="{{.NewCardsPerDayValue}}" required>
		</div>
		<div class="field">
			<label for="deck-reviews-per-day-{{.Deck.ID}}">Reviews per day (blank for no limit)</label>
			<input id="deck-reviews-per-day-{{.Deck.ID}}" type="number" min="0" name="reviews_per_day" value="{{.ReviewsPerDayValue}}">
		</div>
	</div>
	<div class="row">
		<button type="submit" class="primary">Save</button>
		<a class="toolbar-btn" href="/flash/{{.Deck.ID}}" hx-get="/flash/{{.Deck.ID}}" hx-target="#deck-detail" hx-push-url="true">Cancel</a>
	</div>
</form>
{{end}}

{{define "deck-list-items"}}
<ul class="deck-list" id="deck-list"{{if .OOB}} hx-swap-oob="true"{{end}}>
	{{range .Items}}
	{{$d := .Deck}}
	<li>
		<a class="deck-row deck-c-{{$d.Color}}{{if eq $d.ID $.ActiveID}} deck-row-active{{end}}{{if .Snoozed}} deck-row-snoozed{{end}}" href="/flash/{{$d.ID}}" hx-get="/flash/{{$d.ID}}" hx-target="#deck-detail" hx-push-url="true"{{if eq $d.ID $.ActiveID}} aria-current="true"{{end}}>
			{{template "flash-stack" (dict "Size" "sm")}}
			<span class="deck-row-text">
				<span class="deck-row-name">{{$d.Name}}</span>
				<span class="deck-row-status">{{.Status}}</span>
			</span>
			{{if and .Due (not .Snoozed)}}<span class="flash-due-badge" aria-label="{{.Due}} to review">{{.Due}}</span>{{end}}
		</a>
	</li>
	{{end}}
</ul>
{{end}}

{{/* shared-with-me is the recipient's "shared with me" list. It is defined
     on its own, without its own id wrapper, because it is used from two
     places that need different wrappers around the same markup: "content"
     renders it plain (a normal part of the full page), and
     "deck-detail-with-list" re-renders it wrapped in an
     hx-swap-oob="true" div with the same id, so every share/revoke/
     adopt/decline HTMX response keeps this list in sync too. Both callers
     pass a dict with Offers ([]ShareOffer, enriched with FromUsername —
     see shareOfferWithUsername) and CSRFToken (always the page-level
     Shell's token, populated regardless of which deck, if any, is
     selected). U5 replaces this list with gift rows. */}}
{{define "shared-with-me"}}
{{if .Offers}}
<h2 class="flash-list-title">Shared with you</h2>
<ul class="flash-share-list">
	{{range .Offers}}
	<li>
		<span>{{.FromUsername}} — {{.DeckName}} — {{if .PriorAdoptedDeckID}}{{.NewCardCount}} new card{{if ne .NewCardCount 1}}s{{end}} to merge{{else}}new deck{{end}}</span>
		<span class="row">
			<form method="post" action="/flash/shared/adopt" hx-post="/flash/shared/adopt" hx-target="#deck-detail" hx-swap="innerHTML">
				<input type="hidden" name="{{csrfField}}" value="{{$.CSRFToken}}">
				<input type="hidden" name="share_id" value="{{.ID}}">
				<button type="submit" class="toolbar-btn toolbar-btn-active">{{if .PriorAdoptedDeckID}}Merge{{else}}Adopt{{end}}</button>
			</form>
			<form method="post" action="/flash/shared/decline" hx-post="/flash/shared/decline" hx-target="#deck-detail" hx-swap="innerHTML">
				<input type="hidden" name="{{csrfField}}" value="{{$.CSRFToken}}">
				<input type="hidden" name="share_id" value="{{.ID}}">
				<button type="submit" class="toolbar-btn">Dismiss</button>
			</form>
		</span>
	</li>
	{{end}}
</ul>
{{end}}
{{end}}

{{define "content"}}
<div class="flash-home">
	{{template "flash-home-toolbar" .Data}}
	<div class="flash-shell">
		{{/* Checked whenever the pane has something to show; below 900px
		     app.css shows the pane in place of the list. Replaced out of
		     band by deck-detail-with-list, like Pastes' #paste-detail-open. */}}
		<input type="checkbox" id="flash-detail-open" class="visually-hidden"{{if .Data.Detail.Mode}} checked{{end}} aria-hidden="true" tabindex="-1">
		<div class="flash-list-pane">
			<h2 class="flash-list-title">Your decks</h2>
			<div id="shared-with-me" class="flash-shared-with-me">{{template "shared-with-me" (dict "Offers" .Data.SharedWithMe "CSRFToken" .Shell.CSRFToken)}}</div>
			{{template "deck-list-items" .Data.List}}
		</div>
		<div id="deck-detail" class="flash-detail-pane">
			{{template "deck-detail-body" .Data.Detail}}
		</div>
	</div>
</div>
{{end}}
```

> The `flash-home-toolbar` template receives `.Data` (a `deckIndexView`), so
> `.TotalDue` resolves. `dict` values like `.Deck.ID` (an `int64`) work as
> `IDSuffix` because templates print them with `fmt`.

- [ ] **Step 7: Restyle the import partial's buttons**

In `internal/apps/flash/templates/import.partial.html`, add the back button right after the CSRF input, and make Import the primary button:

```html
	<input type="hidden" name="{{csrfField}}" value="{{.CSRFToken}}">
	<div class="notes-toolbar flash-deck-toolbar">{{template "flash-pane-back"}}</div>
```

and

```html
	<div class="row">
		<button type="submit" class="primary">Import</button>
		<a class="toolbar-btn" href="/flash/" hx-get="/flash/" hx-target="#deck-detail" hx-push-url="true">Cancel</a>
	</div>
```

(`flash-pane-back` comes from `components.partial.html`. U6 redesigns this form fully.)

- [ ] **Step 8: Run all Flash tests**

Run: `go test ./internal/apps/flash/ -count=1`
Expected: PASS — the new tests and every existing one (`.deck-list`, `a.deck-row`, `#shared-with-me`, CSRF-on-adopt-form tests included).

If `TestSharedWithMeRefreshesOutOfBandOnAdoptAndDecline` fails because `#shared-with-me` appears twice, check that "content" is not part of the fragment — `deck-detail-with-list` must only contain the OOB copy.

- [ ] **Step 9: Commit**

```bash
git add internal/apps/flash/handlers_decks.go internal/apps/flash/templates/decks.html internal/apps/flash/templates/components.partial.html internal/apps/flash/templates/import.partial.html internal/apps/flash/handlers_decks_test.go
git commit -m "feat(flash): new home screen with deck colours and a big Review button"
```

---

### Task 6: Home screen CSS

**Files:**
- Modify: `internal/ui/static/app.css`

**Interfaces:**
- Consumes: the class names from Task 5's templates.
- Produces: the deck colour tokens (`.deck-c-*` → `--deck`, `--deck-soft`), `.flash-stack*`, `.flash-due-badge`, `.flash-big-btn*`, `.flash-section-title` — reused by U2–U6.

- [ ] **Step 1: Add the Flash home CSS**

Append this block at the **end** of `internal/ui/static/app.css` (after the existing `.flash-snoozed-marker` rule). Keep it in one place so later phases can extend the same section:

```css
/* ---- ON Flash: home screen (UI overhaul U1) ------------------------------
 * Spec: docs/superpowers/specs/2026-09-23-on-flash-ui-overhaul-design.md.
 * The CSP forbids style="", so a deck's colour arrives as a deck-c-<name>
 * class that sets --deck/--deck-soft for everything inside it. */

.deck-c-teal   { --deck: #1D9E75; --deck-soft: #E1F5EE; }
.deck-c-blue   { --deck: #378ADD; --deck-soft: #E6F1FB; }
.deck-c-purple { --deck: #7F77DD; --deck-soft: #EEEDFE; }
.deck-c-pink   { --deck: #D4537E; --deck-soft: #FBEAF0; }
.deck-c-coral  { --deck: #D85A30; --deck-soft: #FAECE7; }
.deck-c-amber  { --deck: #BA7517; --deck-soft: #FAEEDA; }
.deck-c-green  { --deck: #639922; --deck-soft: #EAF3DE; }
.deck-c-gray   { --deck: #888780; --deck-soft: #F1EFE8; }
:root[data-theme="dark"] .deck-c-teal   { --deck-soft: #085041; }
:root[data-theme="dark"] .deck-c-blue   { --deck-soft: #0C447C; }
:root[data-theme="dark"] .deck-c-purple { --deck-soft: #3C3489; }
:root[data-theme="dark"] .deck-c-pink   { --deck-soft: #72243E; }
:root[data-theme="dark"] .deck-c-coral  { --deck-soft: #712B13; }
:root[data-theme="dark"] .deck-c-amber  { --deck-soft: #633806; }
:root[data-theme="dark"] .deck-c-green  { --deck-soft: #27500A; }
:root[data-theme="dark"] .deck-c-gray   { --deck-soft: #444441; }

/* A deck drawn as three stacked cards. */
.flash-stack {
	position: relative;
	display: inline-block;
	flex: none;
}
.flash-stack-sm { width: 1.9rem; height: 2.3rem; }
.flash-stack-lg { width: 5.5rem; height: 6.5rem; }
.flash-stack > span {
	position: absolute;
	inset: 0;
	border-radius: 5px;
	border: 1px solid color-mix(in srgb, var(--deck, #888780) 60%, #000);
}
.flash-stack-lg > span { border-radius: 10px; }
.flash-stack > span:nth-child(1) {
	background: color-mix(in srgb, var(--deck, #888780) 45%, var(--c-bg));
	transform: rotate(-8deg);
}
.flash-stack > span:nth-child(2) {
	background: color-mix(in srgb, var(--deck, #888780) 70%, var(--c-bg));
	transform: rotate(4deg);
}
.flash-stack > span:nth-child(3) { background: var(--deck, #888780); }

.flash-due-badge {
	display: inline-block;
	min-width: 1.5rem;
	padding: 0 var(--s-2);
	border-radius: 999px;
	background: var(--c-accent-attention-bg);
	color: var(--c-accent-attention);
	font-size: var(--fs-xs);
	font-weight: 600;
	text-align: center;
	line-height: 1.5;
}

/* Layout: toolbar above a two-pane split, like ON Paste. */
.flash-home-toolbar { margin-bottom: var(--s-4); justify-content: flex-start; }
.flash-home-toolbar .notes-toolbar-actions { flex-wrap: wrap; }

.flash-shell {
	display: flex;
	align-items: flex-start;
	gap: var(--s-5);
}
.flash-list-pane { flex: 0 0 17rem; min-width: 0; }
.flash-detail-pane { flex: 1; min-width: 0; }

.flash-list-title {
	margin: 0 0 var(--s-2);
	font-size: var(--fs-sm);
	font-weight: 500;
	color: var(--c-text-dim);
}

/* Deck list rows. Tall enough to be a comfortable touch target (44px+). */
.deck-list { margin: 0; padding: 0; list-style: none; }
.deck-row {
	display: flex;
	align-items: center;
	gap: var(--s-3);
	min-height: 3.25rem;
	padding: var(--s-2) var(--s-3);
	margin-bottom: var(--s-1);
	border-radius: 8px;
	color: inherit;
	text-decoration: none;
}
.deck-row:hover { background: var(--c-bg-subtle); }
.deck-row-active,
.deck-row-active:hover {
	background: var(--c-bg-subtle);
	box-shadow: inset 0 0 0 2px var(--c-accent);
}
.deck-row-snoozed { opacity: 0.6; }
.deck-row-text { flex: 1; min-width: 0; display: flex; flex-direction: column; }
.deck-row-name {
	font-weight: 500;
	overflow: hidden;
	text-overflow: ellipsis;
	white-space: nowrap;
}
.deck-row-status { font-size: var(--fs-xs); color: var(--c-text-dim); }

/* The narrow-screen back button only exists below the breakpoint. */
.flash-back-btn { display: none; }

.flash-deck-toolbar { margin-bottom: var(--s-4); }
.flash-deck-toolbar .notes-toolbar-actions { margin-left: auto; flex-wrap: wrap; }
.flash-deck-toolbar form { margin: 0; }

.flash-deck-hero {
	display: flex;
	align-items: center;
	gap: var(--s-5);
	margin-bottom: var(--s-5);
}
.flash-deck-hero-text { min-width: 0; }
.flash-deck-hero-text h1 { margin: 0 0 var(--s-1); }
.flash-deck-hero-text p { margin: 0; }

/* The one primary action on the deck pane. */
.flash-review-cta {
	display: inline-flex;
	align-items: center;
	gap: var(--s-2);
	margin-top: var(--s-4);
	padding: var(--s-3) var(--s-5);
	min-height: 2.75rem;
	border-radius: 10px;
	background: var(--c-accent-attention);
	color: #fff;
	font-size: var(--fs-lg);
	font-weight: 600;
	text-decoration: none;
}
.flash-review-cta:hover { filter: brightness(1.08); }
.flash-review-cta:focus-visible { outline: var(--ring); outline-offset: 2px; }

.flash-all-done {
	display: flex;
	flex-direction: column;
	align-items: flex-start;
	gap: var(--s-2);
	margin-top: var(--s-4);
	padding: var(--s-3) var(--s-4);
	border-radius: 10px;
	background: var(--deck-soft, var(--c-bg-subtle));
}

/* Big, friendly buttons for the welcome and empty states. */
.flash-big-btn {
	display: inline-flex;
	align-items: center;
	gap: var(--s-2);
	min-height: 2.75rem;
	padding: var(--s-2) var(--s-4);
	border-radius: 10px;
	border: 1px solid var(--c-border-firm);
	background: var(--c-bg);
	color: var(--c-text);
	font-weight: 500;
	text-decoration: none;
}
.flash-big-btn:hover { background: var(--c-bg-subtle); }
.flash-big-btn-primary {
	background: var(--c-accent);
	border-color: var(--c-accent);
	color: #fff;
}
.flash-big-btn-primary:hover { background: var(--c-accent); filter: brightness(1.1); }
:root[data-theme="dark"] .flash-big-btn-primary { color: #10141a; }

.flash-welcome {
	display: flex;
	flex-direction: column;
	align-items: center;
	text-align: center;
	gap: var(--s-3);
	padding: var(--s-6) var(--s-4);
}
.flash-welcome h1 { margin: var(--s-3) 0 0; }
.flash-welcome p { max-width: 28rem; margin: 0; }
.flash-welcome-actions { display: flex; flex-wrap: wrap; justify-content: center; gap: var(--s-3); margin-top: var(--s-2); }

.flash-pane-hint { padding: var(--s-4) 0; }

.flash-deck-tiles { margin-bottom: var(--s-5); }

.flash-deck-section { margin-top: var(--s-5); }
.flash-section-title {
	margin: 0 0 var(--s-2);
	font-size: var(--fs-base);
	font-weight: 500;
}
.flash-share-list { margin: var(--s-2) 0 0; padding: 0; list-style: none; }
.flash-share-list li { display: flex; flex-wrap: wrap; align-items: center; justify-content: space-between; gap: var(--s-2); padding: var(--s-1) 0; }
.flash-share-list form { margin: 0; }
.flash-shared-with-me:not(:empty) { margin-bottom: var(--s-4); }

/* Colour swatches: the radio is visually hidden, its label is the swatch. */
.flash-color-field { border: none; padding: 0; margin: 0 0 var(--s-4); }
.flash-color-field legend { margin-bottom: var(--s-2); font-size: var(--fs-sm); color: var(--c-text-dim); padding: 0; }
.flash-swatches { display: flex; flex-wrap: wrap; gap: var(--s-2); }
.flash-swatch {
	width: 2.75rem;
	height: 2.75rem;
	margin: 0;
	border-radius: 50%;
	background: var(--deck);
	border: 3px solid var(--c-bg);
	box-shadow: 0 0 0 1px var(--c-border-firm);
	cursor: pointer;
}
.flash-swatch-input:checked + .flash-swatch { box-shadow: 0 0 0 3px var(--c-text); }
.flash-swatch-input:focus-visible + .flash-swatch { outline: var(--ring); outline-offset: 3px; }

.flash-deck-form { max-width: var(--measure); }

/* Below 900px the list and the pane can't sit side by side: show one at a
 * time, driven by #flash-detail-open (checked by the server whenever the
 * pane has something to show). Same mechanism as #paste-detail-open. */
@media (max-width: 900px) {
	.flash-shell { flex-direction: column; }
	.flash-list-pane,
	.flash-detail-pane { flex: none; width: 100%; }
	.flash-detail-pane { display: none; }
	#flash-detail-open:checked ~ .flash-list-pane { display: none; }
	#flash-detail-open:checked ~ .flash-detail-pane { display: block; }
	.flash-back-btn { display: inline-flex; }
	.flash-deck-hero { flex-direction: column; align-items: flex-start; }
}
```

- [ ] **Step 2: Build and run the full check**

```bash
gofmt -l .
go vet ./...
go run honnef.co/go/tools/cmd/staticcheck@v0.8.0 ./...
go test ./... -race -count=1
```

Expected: no gofmt output, no vet/staticcheck findings, all tests PASS.

- [ ] **Step 3: Commit**

```bash
git add internal/ui/static/app.css
git commit -m "style(flash): home screen, deck stacks and colour swatches"
```

---

### Task 7: Manual verification in the browser

No code unless something is broken. Use the preview browser (`.claude/launch.json` has an `onsuite` configuration that runs `/tmp/onsuite-bin serve` with data in `/tmp/onsuite-manual-verify`).

- [ ] **Step 1: Build and prepare data**

```bash
go build -o /tmp/onsuite-bin ./cmd/onsuite
rm -rf /tmp/onsuite-manual-verify && mkdir -p /tmp/onsuite-manual-verify
```

Ask the user to create an account and sign in (you must not type passwords yourself):

```bash
/tmp/onsuite-bin user add ilia --admin --data-dir /tmp/onsuite-manual-verify
```

- [ ] **Step 2: Start the preview and check each state**

Start the `onsuite` preview, open `/flash/`, and check (take a screenshot of each):

1. First run: the welcome with two big buttons.
2. Create three decks in different colours; one with 3 cards (add them via the existing cards page), one empty. List rows show coloured stacks, status lines and a due badge only where cards are due; "Review all" shows the total.
3. Open the deck with cards: hero stack in its colour, orange "Review 3 cards →", four tiles.
4. Open the empty deck: "This deck has no cards yet" and "Add cards".
5. Edit a deck: swatches, change the colour, save — list and hero update without a reload.
6. Snooze a deck: row dims and says "taking a break"; its pane shows the Unsnooze notice.
7. Dark theme (settings menu): stacks, badges and swatches still read well.
8. Tablet width (`resize_window` 768×1024): list only; selecting a deck shows the pane with "← Decks"; back returns to the list. Reset with preset `desktop` after.
9. `read_console_messages` with `onlyErrors: true` — expected: no CSP violations or JS errors.

- [ ] **Step 3: Fix anything broken, re-run the full check, commit fixes**

---

### Task 8: Open the PR

- [ ] **Step 1: Push and open the PR**

```bash
git push -u origin feat/flash-ui-u1
gh pr create --title "feat(flash): UI overhaul U1 — home screen, deck colours, big Review button" --body "$(cat <<'EOF'
## Summary
- Deck colours: migration 0010, eight-colour swatch picker on New/Edit deck, adopted decks keep the sharer's colour.
- `DeckSummaries`: per-deck numbers for the home screen, guaranteed to match `DueQueue`.
- New home screen: app toolbar (New deck / Import / Review all / Stats), deck list with coloured stacks and due badges, deck pane with one big "Review N cards" button, tiles, and a first-run welcome.
- `flash-review.js` renamed to `flash.js` (the app-wide script later phases extend).

Spec: docs/superpowers/specs/2026-09-23-on-flash-ui-overhaul-design.md (§1, §2). Plan: docs/superpowers/plans/2026-09-23-flash-ui-u1-foundation.md.

## Test plan
- [ ] `go test ./... -race -count=1`
- [ ] Manual: first run, colours, due badges, Review button, empty deck, snoozed deck, dark mode, tablet width (screenshots attached)
EOF
)"
```

- [ ] **Step 2: Attach the Task 7 screenshots to the PR description or a comment.**
