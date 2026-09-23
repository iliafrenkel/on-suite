# ON Flash UI U4 — Review screen Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make reviewing feel like flipping real cards: a focused screen with a progress bar, the shared flippable card, big coloured Forgot / Hard / Got it / Easy buttons that appear after the flip, and a friendly end-of-session summary with a streak and a small celebration.

**Architecture:** A new store query tallies today's grades for a scope (one deck or all). The review handler builds a richer view — progress from "graded today + still queued", the current card as a U2 `cardFace`, or a summary when the queue is empty. `review.html` is rewritten around U2's `flash-card` template; the grade buttons are `:checked ~` siblings of its flip checkbox, so they appear with no JS. `flash.js` changes Space to "flip to the answer" and only lets 1–4 grade once the answer shows.

**Tech Stack:** Go, SQLite, `html/template`, HTMX, CSS (inline SVG for bars — the CSP forbids `style=`), `flash.js`.

**Spec:** [docs/superpowers/specs/2026-09-23-on-flash-ui-overhaul-design.md](../specs/2026-09-23-on-flash-ui-overhaul-design.md) — §1.3, §1.5, §5. Mockups: section 4, "Review screen" (interactive).

**This is PR 4 of 6.** It requires U1–U3 on `main`.

## Global Constraints

- Branch: `feat/flash-ui-u4` off an up-to-date `main` containing U1–U3. Never push to `main`.
- Full check before each Go commit:
  ```bash
  gofmt -l .
  go vet ./...
  go run honnef.co/go/tools/cmd/staticcheck@v0.8.0 ./...
  go mod tidy && git diff --exit-code go.mod go.sum
  go test ./... -race -count=1
  ```
- No new dependencies. CSP: no inline `<script>`, no `style=`. Variable-width bars are `<svg>` with `<rect width="…">` computed in Go.
- Grade labels (spec §1.5): rating 1 **Forgot**, 2 **Hard**, 3 **Got it**, 4 **Easy**. Stored ratings don't change. Keep the button classes `flash-grade-again`, `flash-grade-hard`, `flash-grade-good`, `flash-grade-easy` and the undo class `flash-undo-btn` (JS and tests use them).
- The HTMX swap target stays `#review-body` with `hx-swap="innerHTML"`, and grade/undo routes, form fields (`card_id`, `rating`) and the `?deck=` scope parameter are unchanged.
- `htmlassert` selectors: descendant chains of simple parts only; no compound parts like `a.x[y]`.
- From U1/U2 (do not rename): `DeckSummaries`, `DeckSummary.ReviewNow`, `newCardFace`, `cardFace`, `flash-card` template (dict `Face`, `FlipID`, `Hint`), `.flash-flip`, `.flash-card-front`, `.flash-card-back`, `.flash-card-extras`, `.flash-review-cta`, `.deck-c-*`, `--deck`, icon `arrow-left`, `flash.js`'s `press()` and keydown handler.
- Tests: `newServer(t)`, `s.Get`, `s.PostHX`, `httpGet`, `itoa`; store tests `newFixture(t)`; `flash.RatingAgain…RatingEasy`.
- Commits: Conventional Commits, scope `flash`.

---

## File map

| File | Change |
|---|---|
| `internal/apps/flash/review.go` | add `ReviewTally`, `TodayTally` |
| `internal/apps/flash/review_summary.go` | **Create** — `reviewSummary`, `summarySegment`, `buildSummarySegments`, `summaryLegend` |
| `internal/apps/flash/handlers_review.go` | new `reviewView`/`reviewCardView`, `renderReview` rewrite, `reviewSummaryFor` |
| `internal/apps/flash/templates/card.partial.html` | `flash-card` gains optional `Corner` and `Static` |
| `internal/apps/flash/templates/review.html` | **Rewrite** |
| `internal/apps/flash/static/flash.js` | review Space/grade/Esc behaviour; remove `reveal()` |
| `internal/ui/static/app.css` | review, grade buttons, progress, summary, celebration |
| Tests | `review_test.go`, `review_summary_test.go` (new, internal), `handlers_review_test.go` |

---

### Task 1: Today's tally

**Files:**
- Modify: `internal/apps/flash/review.go`
- Test: `internal/apps/flash/review_test.go`

**Interfaces:**
- Produces:
  ```go
  type ReviewTally struct {
      Reviewed int // new_count + review_count
      Again, Hard, Good, Easy int
  }
  func (st *Store) TodayTally(ctx context.Context, userID int64, deckID *int64, now time.Time) (ReviewTally, error)
  ```
  `deckID == nil` sums every deck; `now`'s UTC day is "today" (the same `formatDay` key the counters use).

- [ ] **Step 1: Write the failing test**

Append to `internal/apps/flash/review_test.go`:

```go
func TestTodayTally(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	now := time.Date(2026, 9, 23, 10, 0, 0, 0, time.UTC)
	a, err := f.store.CreateDeck(ctx, f.alice.ID, "A", "")
	if err != nil {
		t.Fatal(err)
	}
	b, err := f.store.CreateDeck(ctx, f.alice.ID, "B", "")
	if err != nil {
		t.Fatal(err)
	}
	grade := func(deckID int64, rating int, at time.Time) {
		t.Helper()
		c, err := f.store.CreateCard(ctx, f.alice.ID, deckID, flash.CardTypeBasic, "q", "a", "")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.store.GradeCard(ctx, f.alice.ID, c.ID, rating, at); err != nil {
			t.Fatal(err)
		}
	}
	grade(a.ID, flash.RatingGood, now)
	grade(a.ID, flash.RatingGood, now)
	grade(a.ID, flash.RatingAgain, now)
	grade(b.ID, flash.RatingEasy, now)
	grade(b.ID, flash.RatingHard, now.AddDate(0, 0, -1)) // yesterday: not counted

	got, err := f.store.TodayTally(ctx, f.alice.ID, &a.ID, now)
	if err != nil {
		t.Fatal(err)
	}
	if want := (flash.ReviewTally{Reviewed: 3, Again: 1, Good: 2}); got != want {
		t.Errorf("deck A tally = %+v, want %+v", got, want)
	}
	all, err := f.store.TodayTally(ctx, f.alice.ID, nil, now)
	if err != nil {
		t.Fatal(err)
	}
	if want := (flash.ReviewTally{Reviewed: 4, Again: 1, Good: 2, Easy: 1}); all != want {
		t.Errorf("all-decks tally = %+v, want %+v", all, want)
	}
	bob, err := f.store.TodayTally(ctx, f.bob.ID, nil, now)
	if err != nil {
		t.Fatal(err)
	}
	if bob != (flash.ReviewTally{}) {
		t.Errorf("bob's tally = %+v, want zero", bob)
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/apps/flash/ -run TestTodayTally -count=1`
Expected: build failure — `flash.ReviewTally undefined`.

- [ ] **Step 3: Implement**

Append to `internal/apps/flash/review.go`:

```go
// ReviewTally is how many cards were graded on one day, in total and per
// rating — the review screen's progress count and end-of-session summary
// (UI overhaul spec §5).
type ReviewTally struct {
	Reviewed               int // new_count + review_count
	Again, Hard, Good, Easy int
}

// TodayTally sums flash_review_counts for now's UTC day, for one of
// userID's decks or (deckID nil) all of them. It reads the same counters
// DueQueue's daily limits use, so it survives a reload and needs no
// per-session state.
func (st *Store) TodayTally(ctx context.Context, userID int64, deckID *int64, now time.Time) (ReviewTally, error) {
	query := `
		SELECT coalesce(sum(new_count + review_count), 0),
		       coalesce(sum(again_count), 0), coalesce(sum(hard_count), 0),
		       coalesce(sum(good_count), 0), coalesce(sum(easy_count), 0)
		  FROM flash_review_counts
		 WHERE user_id = ? AND day = ?`
	args := []any{userID, formatDay(now)}
	if deckID != nil {
		query += ` AND deck_id = ?`
		args = append(args, *deckID)
	}
	var t ReviewTally
	if err := st.db.QueryRowContext(ctx, query, args...).Scan(&t.Reviewed, &t.Again, &t.Hard, &t.Good, &t.Easy); err != nil {
		return ReviewTally{}, fmt.Errorf("flash: today tally: %w", err)
	}
	return t, nil
}
```

- [ ] **Step 4: Run to verify it passes**

Run: `go test ./internal/apps/flash/ -run TestTodayTally -count=1`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/apps/flash/review.go internal/apps/flash/review_test.go
git commit -m "feat(flash): tally today's grades per deck or overall"
```

---

### Task 2: Summary geometry and wording

**Files:**
- Create: `internal/apps/flash/review_summary.go`
- Test: create `internal/apps/flash/review_summary_test.go` (package `flash`)

**Interfaces:**
- Consumes: `ReviewTally` (Task 1).
- Produces:
  ```go
  type summarySegment struct{ Class string; X, Width int } // X/Width in 0..100
  func buildSummarySegments(t ReviewTally) []summarySegment // order: got it, easy, hard, forgot; zero counts omitted; widths sum to 100
  func summaryLegend(t ReviewTally) string                  // "2 got it · 1 easy · 1 forgot"
  func progressPercent(done, total int) int                 // 0..100, 0 when total is 0
  type reviewSummary struct {
      Reviewed int
      Segments []summarySegment
      Legend   string
      Streak   int
      NextName string // "" = no other deck has cards due
      NextDue  int
      NextURL  string
  }
  ```
  Segment classes: `flash-seg-good`, `flash-seg-easy`, `flash-seg-hard`, `flash-seg-again`.

- [ ] **Step 1: Write the failing tests**

```go
// internal/apps/flash/review_summary_test.go
package flash

import (
	"reflect"
	"testing"
)

func TestBuildSummarySegments(t *testing.T) {
	got := buildSummarySegments(ReviewTally{Reviewed: 12, Good: 9, Hard: 2, Again: 1})
	want := []summarySegment{
		{Class: "flash-seg-good", X: 0, Width: 75},
		{Class: "flash-seg-hard", X: 75, Width: 17},
		{Class: "flash-seg-again", X: 92, Width: 8},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("segments = %+v, want %+v", got, want)
	}

	sum := 0
	for _, s := range buildSummarySegments(ReviewTally{Good: 1, Easy: 1, Hard: 1}) {
		sum += s.Width
	}
	if sum != 100 {
		t.Errorf("thirds sum to %d, want exactly 100 (no gap at the end)", sum)
	}
	if segs := buildSummarySegments(ReviewTally{}); len(segs) != 0 {
		t.Errorf("empty tally = %+v, want no segments", segs)
	}
}

func TestSummaryLegend(t *testing.T) {
	if got := summaryLegend(ReviewTally{Good: 2, Easy: 1, Again: 1}); got != "2 got it · 1 easy · 1 forgot" {
		t.Errorf("legend = %q", got)
	}
	if got := summaryLegend(ReviewTally{}); got != "" {
		t.Errorf("empty legend = %q", got)
	}
}

func TestProgressPercent(t *testing.T) {
	for _, tt := range []struct{ done, total, want int }{
		{0, 0, 0}, {0, 5, 0}, {1, 3, 33}, {2, 3, 67}, {5, 5, 100},
	} {
		if got := progressPercent(tt.done, tt.total); got != tt.want {
			t.Errorf("progressPercent(%d, %d) = %d, want %d", tt.done, tt.total, got, tt.want)
		}
	}
}
```

- [ ] **Step 2: Run to verify they fail**

Run: `go test ./internal/apps/flash/ -run 'SummarySegments|SummaryLegend|ProgressPercent' -count=1`
Expected: build failure.

- [ ] **Step 3: Implement `review_summary.go`**

```go
// internal/apps/flash/review_summary.go
package flash

import (
	"fmt"
	"strings"
)

// reviewSummary is the end-of-session screen (UI overhaul spec §5).
type reviewSummary struct {
	Reviewed int
	Segments []summarySegment
	Legend   string
	Streak   int
	NextName string // "" when no other deck has cards to review
	NextDue  int
	NextURL  string
}

// summarySegment is one coloured part of the summary's breakdown bar, in
// SVG user units of a 100-wide viewBox. Geometry is computed here because
// the CSP forbids style="" and templates can't do arithmetic.
type summarySegment struct {
	Class    string
	X, Width int
}

// summaryParts is the breakdown's fixed order and wording: best first.
func summaryParts(t ReviewTally) []struct {
	class, label string
	n            int
} {
	return []struct {
		class, label string
		n            int
	}{
		{"flash-seg-good", "got it", t.Good},
		{"flash-seg-easy", "easy", t.Easy},
		{"flash-seg-hard", "hard", t.Hard},
		{"flash-seg-again", "forgot", t.Again},
	}
}

// buildSummarySegments lays the four rating counts end to end across 100
// units. Edges are rounded from the running total, so the widths always
// add up to exactly 100.
func buildSummarySegments(t ReviewTally) []summarySegment {
	total := t.Good + t.Easy + t.Hard + t.Again
	if total == 0 {
		return nil
	}
	var out []summarySegment
	cum, x := 0, 0
	for _, p := range summaryParts(t) {
		if p.n == 0 {
			continue
		}
		cum += p.n
		end := (cum*100 + total/2) / total
		out = append(out, summarySegment{Class: p.class, X: x, Width: end - x})
		x = end
	}
	return out
}

// summaryLegend is the breakdown in words, e.g. "9 got it · 2 hard".
func summaryLegend(t ReviewTally) string {
	var parts []string
	for _, p := range summaryParts(t) {
		if p.n > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", p.n, p.label))
		}
	}
	return strings.Join(parts, " · ")
}

// progressPercent is done/total as a rounded percentage, for the review
// screen's progress bar.
func progressPercent(done, total int) int {
	if total <= 0 {
		return 0
	}
	return (done*100 + total/2) / total
}
```

- [ ] **Step 4: Run to verify they pass**

Run: `go test ./internal/apps/flash/ -run 'SummarySegments|SummaryLegend|ProgressPercent' -count=1`
Expected: PASS. (Check: 9/12 → 75, (11·100+6)/12 = 92 → hard width 17, again 8.)

- [ ] **Step 5: Commit**

```bash
git add internal/apps/flash/review_summary.go internal/apps/flash/review_summary_test.go
git commit -m "feat(flash): review summary breakdown and progress maths"
```

---

### Task 3: Review handler and template

**Files:**
- Modify: `internal/apps/flash/handlers_review.go`, `internal/apps/flash/templates/card.partial.html`
- Rewrite: `internal/apps/flash/templates/review.html`
- Test: `internal/apps/flash/handlers_review_test.go`

**Interfaces:**
- Consumes: Tasks 1–2; `newCardFace`, `DeckSummaries`, `Store.Streak`, `Store.TagsForCard`.
- Produces:
  - `reviewView` fields: `Current *reviewCardView`, `Summary *reviewSummary`, `DeckScope`, `LastGradedCardID`, `CSRFToken`, `ScopeName`, `StopURL`, `Color`, `Done`, `Position`, `Total`, `ProgressPct`, `CelebrationColors []string`.
  - `reviewCardView{Face cardFace; IsNew bool; DeckName string}`.
  - `flash-card` template accepts optional `Corner` (string shown in the front's corner) and `Static` (true = no tag links in the extras).
  - Template hooks: `#review-card`, `#review-flip` (the flip checkbox id), `.flash-show-answer`, `.flash-grades`, `.flash-review-stop`, `.flash-review-count`, `.flash-review-summary`, `.flash-celebrate`, `.flash-next-deck`, `.flash-back-to-decks`, `.flash-streak`, `.flash-breakdown-legend`.

- [ ] **Step 1: Update and add the handler tests**

In `internal/apps/flash/handlers_review_test.go`, replace **every** `".flash-review-front"` with `".flash-review-card"` (six occurrences: the review page no longer has a front-only element; the card wrapper exists exactly when a card is showing). Then append:

```go
func TestReviewCardFlipsAndGradesWithFriendlyLabels(t *testing.T) {
	s := newServer(t)
	deck, err := s.Store.CreateDeck(t.Context(), s.Alice.User.ID, "Spanish", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Store.CreateCard(t.Context(), s.Alice.User.ID, deck.ID, flash.CardTypeBasic, "hola", "hello", ""); err != nil {
		t.Fatal(err)
	}
	doc := s.Get(t, s.Alice, "/flash/review/"+itoa(deck.ID))
	doc.MustHave("#review-card input#review-flip")
	show := doc.MustHave("#review-card .flash-show-answer")
	if f, _ := htmlassert.Attr(show, "for"); f != "review-flip" {
		t.Errorf("Show answer label for=%q, want review-flip", f)
	}
	var labels []string
	for _, b := range doc.QueryAll("#review-card .flash-grades button") {
		labels = append(labels, htmlassert.Text(b))
	}
	want := []string{"Forgot 1", "Hard 2", "Got it 3", "Easy 4"}
	if strings.Join(labels, "|") != strings.Join(want, "|") {
		t.Errorf("grade buttons = %v, want %v", labels, want)
	}
	if got := htmlassert.Text(doc.MustHave("#review-card .flash-card-corner")); got != "new" {
		t.Errorf("corner = %q, want new", got)
	}
	doc.MustNotHave("#review-card .flash-tag-links")
	stop := doc.MustHave("a.flash-review-stop")
	if href, _ := htmlassert.Attr(stop, "href"); href != "/flash/"+itoa(deck.ID) {
		t.Errorf("Stop href = %q, want the deck", href)
	}
}

func TestReviewShowsProgress(t *testing.T) {
	s := newServer(t)
	deck, err := s.Store.CreateDeck(t.Context(), s.Alice.User.ID, "Spanish", "")
	if err != nil {
		t.Fatal(err)
	}
	var first flash.Card
	for i, front := range []string{"uno", "dos", "tres"} {
		c, err := s.Store.CreateCard(t.Context(), s.Alice.User.ID, deck.ID, flash.CardTypeBasic, front, "x", "")
		if err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			first = c
		}
	}
	doc := s.Get(t, s.Alice, "/flash/review/"+itoa(deck.ID))
	if got := htmlassert.Text(doc.MustHave(".flash-review-count")); got != "1 of 3" {
		t.Errorf("count before grading = %q, want 1 of 3", got)
	}

	// Got it on a new card schedules it minutes away, so it leaves today's
	// queue for now.
	rec := s.PostHX(t, s.Alice, "/flash/review/grade?deck="+itoa(deck.ID), url.Values{"card_id": {itoa(first.ID)}, "rating": {"3"}})
	if rec.Code != 200 {
		t.Fatalf("grade = %d", rec.Code)
	}
	after := htmlassert.Parse(t, rec.Body.String())
	if got := htmlassert.Text(after.MustHave(".flash-review-count")); got != "2 of 3" {
		t.Errorf("count after one grade = %q, want 2 of 3", got)
	}
	fill := after.MustHave(".flash-progress rect.fill")
	if w, _ := htmlassert.Attr(fill, "width"); w != "33" {
		t.Errorf("progress width = %q, want 33", w)
	}
}

func TestReviewSummaryAfterTheLastCard(t *testing.T) {
	s := newServer(t)
	deck, err := s.Store.CreateDeck(t.Context(), s.Alice.User.ID, "Spanish", "")
	if err != nil {
		t.Fatal(err)
	}
	c, err := s.Store.CreateCard(t.Context(), s.Alice.User.ID, deck.ID, flash.CardTypeBasic, "hola", "hello", "")
	if err != nil {
		t.Fatal(err)
	}
	rec := s.PostHX(t, s.Alice, "/flash/review/grade?deck="+itoa(deck.ID), url.Values{"card_id": {itoa(c.ID)}, "rating": {"3"}})
	doc := htmlassert.Parse(t, rec.Body.String())
	summary := doc.MustHave(".flash-review-summary")
	if text := htmlassert.Text(summary); !strings.Contains(text, "You reviewed 1 card today") {
		t.Errorf("summary = %q", text)
	}
	if got := htmlassert.Text(doc.MustHave(".flash-breakdown-legend")); got != "1 got it" {
		t.Errorf("legend = %q", got)
	}
	doc.MustHave(".flash-celebrate")
	back := doc.MustHave("a.flash-back-to-decks")
	if href, _ := htmlassert.Attr(back, "href"); href != "/flash/" {
		t.Errorf("Back to decks href = %q", href)
	}
	doc.MustNotHave(".flash-streak") // a 1-day streak isn't mentioned
	doc.MustHave(".flash-undo-btn")  // the last grade can still be undone
}

func TestReviewSummarySuggestsTheNextDeck(t *testing.T) {
	s := newServer(t)
	a, err := s.Store.CreateDeck(t.Context(), s.Alice.User.ID, "Alpha", "")
	if err != nil {
		t.Fatal(err)
	}
	b, err := s.Store.CreateDeck(t.Context(), s.Alice.User.ID, "Beta", "")
	if err != nil {
		t.Fatal(err)
	}
	ca, err := s.Store.CreateCard(t.Context(), s.Alice.User.ID, a.ID, flash.CardTypeBasic, "a", "x", "")
	if err != nil {
		t.Fatal(err)
	}
	for _, front := range []string{"b1", "b2"} {
		if _, err := s.Store.CreateCard(t.Context(), s.Alice.User.ID, b.ID, flash.CardTypeBasic, front, "x", ""); err != nil {
			t.Fatal(err)
		}
	}
	rec := s.PostHX(t, s.Alice, "/flash/review/grade?deck="+itoa(a.ID), url.Values{"card_id": {itoa(ca.ID)}, "rating": {"3"}})
	doc := htmlassert.Parse(t, rec.Body.String())
	next := doc.MustHave("a.flash-next-deck")
	if href, _ := htmlassert.Attr(next, "href"); href != "/flash/review/"+itoa(b.ID) {
		t.Errorf("next deck href = %q", href)
	}
	if text := htmlassert.Text(next); !strings.Contains(text, "Beta") || !strings.Contains(text, "2 due") {
		t.Errorf("next deck text = %q", text)
	}
}

func TestReviewSummaryShowsAStreak(t *testing.T) {
	s := newServer(t)
	other, err := s.Store.CreateDeck(t.Context(), s.Alice.User.ID, "Yesterday", "")
	if err != nil {
		t.Fatal(err)
	}
	old, err := s.Store.CreateCard(t.Context(), s.Alice.User.ID, other.ID, flash.CardTypeBasic, "old", "x", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Store.GradeCard(t.Context(), s.Alice.User.ID, old.ID, flash.RatingEasy, time.Now().UTC().AddDate(0, 0, -1)); err != nil {
		t.Fatal(err)
	}
	deck, err := s.Store.CreateDeck(t.Context(), s.Alice.User.ID, "Today", "")
	if err != nil {
		t.Fatal(err)
	}
	c, err := s.Store.CreateCard(t.Context(), s.Alice.User.ID, deck.ID, flash.CardTypeBasic, "new", "x", "")
	if err != nil {
		t.Fatal(err)
	}
	rec := s.PostHX(t, s.Alice, "/flash/review/grade?deck="+itoa(deck.ID), url.Values{"card_id": {itoa(c.ID)}, "rating": {"3"}})
	doc := htmlassert.Parse(t, rec.Body.String())
	if got := htmlassert.Text(doc.MustHave(".flash-streak")); !strings.Contains(got, "2 days in a row") {
		t.Errorf("streak = %q, want 2 days in a row", got)
	}
}

func TestReviewWithNothingToDoHasNoCelebration(t *testing.T) {
	s := newServer(t)
	doc := s.Get(t, s.Alice, "/flash/review")
	summary := doc.MustHave(".flash-review-summary")
	if !strings.Contains(htmlassert.Text(summary), "Nothing to review right now") {
		t.Errorf("summary = %q", htmlassert.Text(summary))
	}
	doc.MustNotHave(".flash-celebrate")
	stop := doc.MustHave("a.flash-review-stop")
	if href, _ := htmlassert.Attr(stop, "href"); href != "/flash/" {
		t.Errorf("Stop href for Review all = %q, want /flash/", href)
	}
}
```

Add `"strings"` to the file's imports if it is not already there.

- [ ] **Step 2: Run to verify they fail**

Run: `go test ./internal/apps/flash/ -run Review -count=1`
Expected: FAIL.

- [ ] **Step 3: Extend `flash-card`**

In `internal/apps/flash/templates/card.partial.html`, update the `flash-card` doc comment's parameter list with:

```
       Corner — optional word for the front's corner ("new")
       Static — true leaves the tag links out of the extras (the review
                page has no deck pane for them to open in)
```

Inside the front face, as its first child:

```html
			{{with $.Corner}}<span class="flash-card-corner">{{.}}</span>{{end}}
```

and change the extras' tag-links line to:

```html
	{{if not $.Static}}{{with .Face.Tags}}<p class="flash-tag-links">{{range .}}<a class="flash-pill" href="{{.Href}}" hx-get="{{.Href}}" hx-target="#deck-detail" hx-push-url="true">{{.Name}}</a>{{end}}</p>{{end}}{{end}}
```

- [ ] **Step 4: Rewrite the view model and `renderReview` in `handlers_review.go`**

Replace the `reviewCardView` and `reviewView` types with:

```go
// reviewCardView is the card on screen.
type reviewCardView struct {
	Face     cardFace
	IsNew    bool
	DeckName string
}

// celebrationColors are the deck colours the summary's little burst of
// cards uses — any fixed handful of DeckColors will do.
var celebrationColors = []string{"teal", "amber", "pink", "blue", "purple", "green"}

// reviewView is what templates/review.html's "review-body" block renders,
// both as a full page and as an HTMX fragment after grading or undoing.
type reviewView struct {
	Current          *reviewCardView // nil once the queue is empty
	Summary          *reviewSummary  // set exactly when Current is nil
	DeckScope        string          // "" (all decks) or a deck id, threaded into every form action
	LastGradedCardID int64           // 0 = nothing to undo yet
	CSRFToken        string

	ScopeName string // the deck's name, or "All decks"
	StopURL   string // the deck pane, or the home screen for Review all
	Color     string // deck colour for the progress bar; "" uses the accent

	// Progress: Done cards graded today in this scope, Total = Done plus
	// what is still queued, Position = which card this is (Done + 1).
	Done, Position, Total, ProgressPct int

	CelebrationColors []string
}
```

Replace `renderReview` with:

```go
func (a *App) renderReview(w http.ResponseWriter, r *http.Request, userID int64, deckID *int64, status int, lastGradedCardID int64) {
	ctx := r.Context()
	now := a.store.now()
	queue, err := a.store.DueQueue(ctx, userID, deckID, now)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	tally, err := a.store.TodayTally(ctx, userID, deckID, now)
	if err != nil {
		a.deps.Errors.Internal(w, r, err)
		return
	}

	view := reviewView{
		DeckScope:         deckScopeString(deckID),
		LastGradedCardID:  lastGradedCardID,
		CSRFToken:         web.CSRFToken(ctx),
		ScopeName:         "All decks",
		StopURL:           "/flash/",
		Done:              tally.Reviewed,
		Total:             tally.Reviewed + len(queue),
		CelebrationColors: celebrationColors,
	}
	view.ProgressPct = progressPercent(view.Done, view.Total)
	if deckID != nil {
		d, err := a.store.DeckByID(ctx, userID, *deckID)
		if err != nil {
			a.fail(w, r, err)
			return
		}
		view.ScopeName, view.StopURL, view.Color = d.Name, "/flash/"+strconv.FormatInt(d.ID, 10), d.Color
	}

	if len(queue) > 0 {
		qc := queue[0]
		tags, err := a.store.TagsForCard(ctx, userID, qc.Card.ID)
		if err != nil {
			a.fail(w, r, err)
			return
		}
		names := make([]string, len(tags))
		for i, tg := range tags {
			names[i] = tg.Name
		}
		view.Current = &reviewCardView{Face: newCardFace(qc.Card, qc.Deck, names), IsNew: qc.IsNew, DeckName: qc.Deck.Name}
		view.Position = view.Done + 1
		if deckID == nil {
			view.Color = qc.Deck.Color
		}
	} else {
		summary, err := a.reviewSummaryFor(ctx, userID, deckID, tally, now)
		if err != nil {
			a.deps.Errors.Internal(w, r, err)
			return
		}
		view.Summary = &summary
	}

	if web.IsHTMX(r) {
		if err := a.deps.Render.Fragment(w, status, "flash/review", "review-body", view); err != nil {
			a.deps.Errors.Internal(w, r, err)
		}
		return
	}
	page := a.deps.Page(r, "Review")
	page.Data = view
	a.render(w, r, status, "flash/review", page)
}

// reviewSummaryFor builds the end-of-session screen: today's tally for the
// scope, the account-wide streak, and — for a one-deck review — the other
// deck with the most cards waiting, if any.
func (a *App) reviewSummaryFor(ctx context.Context, userID int64, deckID *int64, tally ReviewTally, now time.Time) (reviewSummary, error) {
	s := reviewSummary{
		Reviewed: tally.Reviewed,
		Segments: buildSummarySegments(tally),
		Legend:   summaryLegend(tally),
	}
	streak, err := a.store.Streak(ctx, userID, now)
	if err != nil {
		return reviewSummary{}, err
	}
	s.Streak = streak

	sums, err := a.store.DeckSummaries(ctx, userID, now)
	if err != nil {
		return reviewSummary{}, err
	}
	for _, d := range sums {
		if deckID != nil && d.Deck.ID == *deckID {
			continue
		}
		if d.ReviewNow > s.NextDue {
			s.NextName, s.NextDue = d.Deck.Name, d.ReviewNow
			s.NextURL = "/flash/review/" + strconv.FormatInt(d.Deck.ID, 10)
		}
	}
	return s, nil
}
```

Update imports: add `"context"` and `"time"`.

- [ ] **Step 5: Rewrite `templates/review.html`**

```html
{{/* internal/apps/flash/templates/review.html — the focused review screen
     (UI overhaul spec §5). No deck list: just progress, the card, and the
     grade buttons. The flip is U2's CSS-only checkbox; the Show answer
     label and the grade form are later siblings of that checkbox, so
     `.flash-flip:checked ~` rules swap one for the other with no JS. */}}
{{define "head"}}
<script src="/flash/flash.js" defer></script>
{{end}}

{{define "review-body"}}
<div class="flash-review{{with .Color}} deck-c-{{.}}{{end}}">
	<div class="flash-review-top">
		<a class="toolbar-btn flash-review-stop" href="{{.StopURL}}">{{ticon "arrow-left"}}Stop</a>
		<span class="flash-review-scope">{{.ScopeName}}</span>
		{{if .Total}}
		<svg class="flash-progress" viewBox="0 0 100 8" preserveAspectRatio="none" role="img" aria-label="{{.Done}} of {{.Total}} reviewed">
			<rect class="track" x="0" y="0" width="100" height="8" rx="4"/>
			<rect class="fill" x="0" y="0" width="{{.ProgressPct}}" height="8" rx="4"/>
		</svg>
		{{if .Current}}<span class="flash-review-count">{{.Position}} of {{.Total}}</span>{{end}}
		{{end}}
	</div>

	{{if .Current}}
	<div class="flash-review-card" id="review-card">
		{{template "flash-card" (dict "Face" .Current.Face "FlipID" "review-flip" "Hint" "Tap the card or press Space" "Corner" (and .Current.IsNew "new") "Static" true)}}
		<label for="review-flip" class="flash-show-answer">Show answer <kbd>Space</kbd></label>
		<form class="flash-grades" method="post"
		      action="/flash/review/grade{{if .DeckScope}}?deck={{.DeckScope}}{{end}}"
		      hx-post="/flash/review/grade{{if .DeckScope}}?deck={{.DeckScope}}{{end}}"
		      hx-target="#review-body" hx-swap="innerHTML">
			<input type="hidden" name="{{csrfField}}" value="{{.CSRFToken}}">
			<input type="hidden" name="card_id" value="{{.Current.Face.ID}}">
			<button type="submit" name="rating" value="1" class="flash-grade flash-grade-again">Forgot <kbd>1</kbd></button>
			<button type="submit" name="rating" value="2" class="flash-grade flash-grade-hard">Hard <kbd>2</kbd></button>
			<button type="submit" name="rating" value="3" class="flash-grade flash-grade-good">Got it <kbd>3</kbd></button>
			<button type="submit" name="rating" value="4" class="flash-grade flash-grade-easy">Easy <kbd>4</kbd></button>
		</form>
	</div>
	{{else}}
	{{template "review-summary" .}}
	{{end}}

	{{if .LastGradedCardID}}
	<form class="flash-undo" method="post"
	      action="/flash/review/undo{{if .DeckScope}}?deck={{.DeckScope}}{{end}}"
	      hx-post="/flash/review/undo{{if .DeckScope}}?deck={{.DeckScope}}{{end}}"
	      hx-target="#review-body" hx-swap="innerHTML">
		<input type="hidden" name="{{csrfField}}" value="{{.CSRFToken}}">
		<input type="hidden" name="card_id" value="{{.LastGradedCardID}}">
		<button type="submit" class="toolbar-btn flash-undo-btn">Undo last <kbd>U</kbd></button>
	</form>
	{{end}}
</div>
{{end}}

{{define "review-summary"}}
{{with .Summary}}
<div class="flash-review-summary" role="status">
	{{if .Reviewed}}
	<div class="flash-celebrate" aria-hidden="true">{{range $.CelebrationColors}}<span class="deck-c-{{.}}"></span>{{end}}</div>
	<h1>Nice work!</h1>
	<p class="flash-summary-lead">You reviewed {{.Reviewed}} card{{if ne .Reviewed 1}}s{{end}} today.</p>
	{{if .Segments}}
	<svg class="flash-breakdown" viewBox="0 0 100 10" preserveAspectRatio="none" role="img" aria-label="{{.Legend}}">
		{{range .Segments}}<rect class="{{.Class}}" x="{{.X}}" y="0" width="{{.Width}}" height="10"/>{{end}}
	</svg>
	<p class="flash-breakdown-legend">{{.Legend}}</p>
	{{end}}
	{{if ge .Streak 2}}<p class="flash-streak">🔥 {{.Streak}} days in a row</p>{{end}}
	{{else}}
	<h1>Nothing to review right now</h1>
	<p class="dim">New cards and reviews come back over time. Check again later.</p>
	{{end}}
	{{if .NextURL}}
	<a class="flash-review-cta flash-next-deck" href="{{.NextURL}}">Review {{.NextName}} next ({{.NextDue}} due) <span aria-hidden="true">→</span></a>
	{{else}}
	<a class="flash-review-cta flash-back-to-decks" href="/flash/">Back to decks</a>
	{{end}}
</div>
{{end}}
{{end}}

{{define "content"}}
<div id="review-body">
	{{template "review-body" .Data}}
</div>
{{end}}
```

> `(and .Current.IsNew "new")` evaluates to `"new"` for a new card and to
> `false` otherwise, so `{{with $.Corner}}` shows the word only for new cards.

- [ ] **Step 6: Run the tests**

Run: `go test ./internal/apps/flash/ -count=1`
Expected: PASS — the new tests and every existing review test (grade, undo, CSRF, daily-limit ones) with the `.flash-review-card` rename.

- [ ] **Step 7: Commit**

```bash
git add internal/apps/flash/handlers_review.go internal/apps/flash/templates/review.html internal/apps/flash/templates/card.partial.html internal/apps/flash/handlers_review_test.go
git commit -m "feat(flash): focused review screen with progress and a session summary"
```

---

### Task 4: Review keyboard behaviour

**Files:**
- Modify: `internal/apps/flash/static/flash.js`

**Interfaces:**
- Consumes: `#review-card .flash-flip`, `.flash-grade-*`, `.flash-undo-btn`, `.flash-review-stop`.
- Produces: Space flips the review card to its answer (never back); 1–4 grade only once the answer shows; U undoes; Esc stops. The card viewer (U2) keeps Space-toggles.

- [ ] **Step 1: Edit `flash.js`**

1. Update the header comment's review line to: `// Review shortcuts: Space shows the answer, 1-4 grade it (Forgot/Hard/Got it/Easy) once it shows, U undoes the last grade, Esc stops.`
2. Delete the `reveal()` function and the `document.addEventListener("click", …)` block that called it for `.flash-reveal-btn` (the review page no longer has that button). **Keep** the separate click listener for `.flash-make-blank` from U3.
3. Add, next to `toggleFlip`:

```js
	// flipReview shows the review card's answer. It never flips back: once
	// the answer is showing, Space has nothing more to do.
	function flipReview() {
		var flip = document.querySelector("#review-card .flash-flip");
		if (!flip) return false;
		flip.checked = true;
		return true;
	}

	// grade presses a grade button, but only once the answer is showing —
	// the buttons are hidden until then, and a stray key must not grade a
	// card nobody has looked at.
	function grade(selector) {
		var flip = document.querySelector("#review-card .flash-flip");
		if (!flip || !flip.checked) return false;
		return press(selector);
	}
```

4. In the keydown `switch`, change these cases:

```js
			case " ":
				handled = flipReview() || toggleFlip();
				break;
			case "1":
				handled = grade(".flash-grade-again");
				break;
			case "2":
				handled = grade(".flash-grade-hard");
				break;
			case "3":
				handled = grade(".flash-grade-good");
				break;
			case "4":
				handled = grade(".flash-grade-easy");
				break;
			case "Escape":
				handled = press(".flash-review-stop");
				break;
```

(`u`/`U`, arrows and `e`/`E` stay as they are.)

- [ ] **Step 2: Check the script is served, commit**

Run: `go test ./internal/apps/flash/ -run TestFlashScriptIsServed -count=1` → PASS.

```bash
git add internal/apps/flash/static/flash.js
git commit -m "feat(flash): review keys flip first, then grade"
```

---

### Task 5: Review CSS

**Files:**
- Modify: `internal/ui/static/app.css`

- [ ] **Step 1: Replace the old review rules and append the new block**

Search app.css for any `.flash-review-card`, `.flash-review-front`, `.flash-review-back`, `.flash-reveal-btn`, `.flash-review-image` rules left from F2/F4 (`grep -n "flash-review\|flash-reveal" internal/ui/static/app.css`) and delete them. Then append:

```css
/* ---- ON Flash: review (UI overhaul U4) --------------------------------- */

:root {
	--flash-grade-again: #E24B4A; --flash-grade-again-bg: #FCEBEB; --flash-grade-again-text: #791F1F;
	--flash-grade-hard:  #EF9F27; --flash-grade-hard-bg:  #FAEEDA; --flash-grade-hard-text:  #633806;
	--flash-grade-good:  #1D9E75; --flash-grade-good-bg:  #E1F5EE; --flash-grade-good-text:  #085041;
	--flash-grade-easy:  #378ADD; --flash-grade-easy-bg:  #E6F1FB; --flash-grade-easy-text:  #0C447C;
}
:root[data-theme="dark"] {
	--flash-grade-again-bg: #501313; --flash-grade-again-text: #F7C1C1;
	--flash-grade-hard-bg:  #412402; --flash-grade-hard-text:  #FAC775;
	--flash-grade-good-bg:  #04342C; --flash-grade-good-text:  #9FE1CB;
	--flash-grade-easy-bg:  #042C53; --flash-grade-easy-text:  #B5D4F4;
}

.flash-review { max-width: 40rem; margin: 0 auto; }

.flash-review-top {
	display: flex;
	align-items: center;
	gap: var(--s-3);
	margin-bottom: var(--s-4);
	font-size: var(--fs-sm);
	color: var(--c-text-dim);
}
.flash-review-scope { white-space: nowrap; overflow: hidden; text-overflow: ellipsis; max-width: 12rem; }
.flash-progress { flex: 1; height: 8px; min-width: 4rem; }
.flash-progress .track { fill: var(--c-bg-inset); }
.flash-progress .fill { fill: var(--deck, var(--c-accent)); }
.flash-review-count { white-space: nowrap; }

.flash-review-card .flash-card { max-width: 28rem; margin: 0 auto; }
.flash-review-card .flash-card-inner { min-height: 16rem; }
.flash-card-corner {
	position: absolute;
	top: var(--s-4);
	left: var(--s-4);
	font-size: var(--fs-xs);
	color: var(--c-text-faint);
}

.flash-show-answer {
	display: flex;
	align-items: center;
	justify-content: center;
	gap: var(--s-2);
	width: fit-content;
	min-height: 2.75rem;
	margin: var(--s-4) auto 0;
	padding: var(--s-2) var(--s-6);
	border-radius: 10px;
	background: var(--c-accent);
	color: #fff;
	font-size: var(--fs-lg);
	font-weight: 500;
	cursor: pointer;
}
:root[data-theme="dark"] .flash-show-answer { color: #10141a; }
.flash-show-answer kbd,
.flash-grade kbd,
.flash-undo-btn kbd {
	font-family: var(--font-ui);
	font-size: var(--fs-2xs);
	opacity: 0.75;
}

/* Grade buttons: hidden until the card is flipped (CSS only). */
.flash-grades {
	display: none;
	grid-template-columns: repeat(4, minmax(0, 1fr));
	gap: var(--s-3);
	max-width: 34rem;
	margin: var(--s-4) auto 0;
}
.flash-flip:checked ~ .flash-show-answer { display: none; }
.flash-flip:checked ~ .flash-grades { display: grid; }
.flash-grade {
	display: flex;
	flex-direction: column;
	align-items: center;
	gap: 2px;
	min-height: 3.5rem;
	padding: var(--s-2);
	border-radius: 10px;
	border: 2px solid;
	font-size: var(--fs-base);
	font-weight: 600;
	cursor: pointer;
}
.flash-grade-again { border-color: var(--flash-grade-again); background: var(--flash-grade-again-bg); color: var(--flash-grade-again-text); }
.flash-grade-hard  { border-color: var(--flash-grade-hard);  background: var(--flash-grade-hard-bg);  color: var(--flash-grade-hard-text); }
.flash-grade-good  { border-color: var(--flash-grade-good);  background: var(--flash-grade-good-bg);  color: var(--flash-grade-good-text); }
.flash-grade-easy  { border-color: var(--flash-grade-easy);  background: var(--flash-grade-easy-bg);  color: var(--flash-grade-easy-text); }
.flash-grade:hover { filter: brightness(0.97); }
.flash-grade:focus-visible { outline: var(--ring); outline-offset: 2px; }

.flash-undo { display: flex; justify-content: center; margin-top: var(--s-4); }

/* End-of-session summary. */
.flash-review-summary {
	position: relative;
	display: flex;
	flex-direction: column;
	align-items: center;
	gap: var(--s-3);
	padding: var(--s-6) var(--s-4);
	text-align: center;
}
.flash-review-summary h1 { margin: 0; }
.flash-summary-lead { margin: 0; font-size: var(--fs-lg); }
.flash-breakdown { width: 100%; max-width: 24rem; height: 12px; border-radius: 6px; overflow: hidden; }
.flash-seg-good  { fill: var(--flash-grade-good); }
.flash-seg-easy  { fill: var(--flash-grade-easy); }
.flash-seg-hard  { fill: var(--flash-grade-hard); }
.flash-seg-again { fill: var(--flash-grade-again); }
.flash-breakdown-legend { margin: 0; color: var(--c-text-dim); font-size: var(--fs-sm); }
.flash-streak { margin: 0; font-weight: 500; }

/* A short burst of little cards, played once when the summary appears. */
.flash-celebrate {
	position: absolute;
	top: var(--s-5);
	left: 50%;
	width: 0;
	height: 0;
	pointer-events: none;
}
.flash-celebrate span {
	position: absolute;
	width: 14px;
	height: 18px;
	border-radius: 3px;
	background: var(--deck);
	opacity: 0;
	animation: flash-burst 1s ease-out forwards;
}
.flash-celebrate span:nth-child(1) { --dx: -110px; --dy: -30px; --rot: -40deg; }
.flash-celebrate span:nth-child(2) { --dx: -60px;  --dy: -70px; --rot: -15deg; animation-delay: 0.05s; }
.flash-celebrate span:nth-child(3) { --dx: -10px;  --dy: -90px; --rot: 10deg;  animation-delay: 0.1s; }
.flash-celebrate span:nth-child(4) { --dx: 40px;   --dy: -80px; --rot: 25deg;  animation-delay: 0.05s; }
.flash-celebrate span:nth-child(5) { --dx: 90px;   --dy: -50px; --rot: 45deg;  animation-delay: 0.1s; }
.flash-celebrate span:nth-child(6) { --dx: 120px;  --dy: -10px; --rot: 70deg;  animation-delay: 0.15s; }
@keyframes flash-burst {
	0%   { opacity: 0; transform: translate(0, 0) rotate(0) scale(0.4); }
	20%  { opacity: 1; }
	100% { opacity: 0; transform: translate(var(--dx), var(--dy)) rotate(var(--rot)) scale(1); }
}

@media (max-width: 640px) {
	.flash-grades { grid-template-columns: repeat(2, minmax(0, 1fr)); }
}

@media (prefers-reduced-motion: reduce) {
	.flash-celebrate { display: none; }
}
```

- [ ] **Step 2: Full check, commit**

```bash
git add internal/ui/static/app.css
git commit -m "style(flash): review screen, grade buttons and summary"
```

---

### Task 6: Manual verification

- [ ] Build, start the `onsuite` preview (ask the user to sign in), use a deck with ~5 cards including a cloze card and one with an image and audio. Check with screenshots:
  1. Review from the deck pane's big button: top bar shows Stop, deck name, progress and "1 of 5"; the card has the deck stripe and "new" in the corner.
  2. Press Space: the card flips (animation), "Show answer" is replaced by the four coloured buttons with their keys; audio controls appear under the card.
  3. Pressing 3 before flipping does nothing; after flipping it grades; the next card arrives face up and "2 of 5" / the bar advance.
  4. Cloze card: blank on the front, highlighted answer on the back.
  5. U undoes; Esc returns to the deck.
  6. Finish the deck: "Nice work!", the count, the breakdown bar and legend, the burst plays once, and either "Review *X* next" or "Back to decks".
  7. Review all from the toolbar: progress bar uses each card's deck colour.
  8. Dark theme, 640px width (grades in 2×2), reduced motion (no burst, no rotation); no console/CSP errors.
- [ ] Fix, full check, commit.

### Task 7: Open the PR

```bash
git push -u origin feat/flash-ui-u4
gh pr create --title "feat(flash): UI overhaul U4 — review screen and session summary" --body "$(cat <<'EOF'
## Summary
- Focused review screen: Stop, deck name, progress bar and "N of M" (from today's grades, so it survives a reload).
- The shared flippable card; Show answer, then Forgot / Hard / Got it / Easy buttons (CSS-only reveal).
- Keys: Space shows the answer, 1–4 grade only once it shows, U undoes, Esc stops.
- Session summary: today's count, breakdown bar, streak (2+ days), a small CSS celebration (off under reduced motion), and the next deck to review.

Spec §5. Plan: docs/superpowers/plans/2026-09-23-flash-ui-u4-review.md.

## Test plan
- [ ] `go test ./... -race -count=1`
- [ ] Manual checklist from the plan's Task 6 (screenshots attached)
EOF
)"
```
