# ON Flash — Review session queue follow-ups Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Tidy up the review session: a snoozed deck's review page says it's on a break, Review all serves the most overdue card first across decks, each render reads only the queue's head and length rather than the whole queue, no-JS grade/undo follow POST-redirect-GET, and a break is capped at a year.

**Architecture:** `renderReview` receives the deck its caller already loaded (#333) and gains a "taking a break" branch. `DueQueue` merges each deck's (limit-capped, due-ordered) reviews by `due_at` across decks, then appends new cards deck by deck as today. A new `Store.QueueFront` returns the same queue's first card plus its exact length. It reads at most one card per deck and counts through `deckSummary`, the rule the Review button already uses. `DueQueue` stays as the reference queue that tests compare `QueueFront` against. Non-HTMX grade/undo redirect with 303, and grade carries `?undo={cardID}` so the no-JS Undo button survives the redirect.

**Tech Stack:** Go, SQLite (`modernc.org/sqlite`), `html/template`, HTMX, CSS.

**Issues:** [#296](https://github.com/iliafrenkel/on-suite/issues/296) (items 1–5), [#333](https://github.com/iliafrenkel/on-suite/issues/333). Specs: [F2 review engine §Queue construction](../specs/2026-09-17-on-flash-f2-review-engine-design.md), [UI overhaul §5](../specs/2026-09-23-on-flash-ui-overhaul-design.md) ("N of M").

**Decisions already made (by the user):**
1. Review all orders **due reviews** globally by `due_at` (most overdue first). Ties go to deck order (`ListDecks`, newest first), then card id. New cards still come after every review, in the existing per-deck order. Per-deck daily limits apply per deck exactly as they do today. Single-deck scope is unaffected.
2. Stop loading the full queue on every render. "N of M" stays exact: M = graded today + cards remaining (= `len(DueQueue)`).
3. PRG for non-HTMX grade/undo: 303 to `/flash/review/{deckID}` or `/flash/review`, with grade adding `?undo={cardID}`.
4. A snoozed deck's own review page shows "Taking a break until …" and an End break button that reuses the existing unsnooze route.
5. `snooze` rejects `days > 365` with a 400.
6. #333: deviate from the U4 plan. `renderReview` takes the loaded deck instead of looking it up again.

## Verified against current code (branch `fix/296-review-session-queue` @ `bbc5aab`)

| What | Where |
|---|---|
| `review` looks the deck up, then `renderReview` does it again | `internal/apps/flash/handlers_review.go:89-95`, `:141-148` |
| `deckReview` looks the deck up, then `renderReview` does it again | `handlers_review.go:109-113`, `:141-148` |
| full queue loaded, only `queue[0]` used; `Total = tally.Reviewed + len(queue)` | `handlers_review.go:119`, `:137`, `:150-151` |
| grade/undo always render 200, even without HTMX | `handlers_review.go:217-241`, `:243-267` |
| grade applies the grade **before** the `?deck=` scope is checked (bad scope → 404 *after* grading) | `handlers_review.go:236` then `:240`→`:119/:142` |
| `DueQueue` concatenates reviews deck by deck, in `ListDecks` order | `internal/apps/flash/review.go:372-399` |
| `ListDecks` = `created_at DESC, id DESC` | `internal/apps/flash/deck.go:155-158` |
| due reviews `ORDER BY s.due_at ASC` (no id tiebreak); new `ORDER BY c.created_at ASC` (no id tiebreak) | `review.go:440`, `:460` |
| `QueueCard` has no due time | `review.go:342-346` |
| per-deck budget → `DueToday`/`NewToday`/`ReviewNow` | `internal/apps/flash/deck_summary.go:63-75` |
| `ReviewNow == len(DueQueue)` pinned | `internal/apps/flash/deck_summary_test.go:55-107` |
| `snoozeDeck` accepts any positive `days` | `internal/apps/flash/handlers_decks.go:725-729` |
| existing break banner + End break form (format `"2 Jan"`) | `internal/apps/flash/templates/decks.html:75-82`, `:183-190` |
| review body: card, else summary; undo form | `internal/apps/flash/templates/review.html:24-53` |
| other `DueQueue` callers (tests only) | `share_test.go:259,325`, `stats_test.go:459`, `deck_summary_test.go:91`, `review_test.go:298-505` |

## Global Constraints

- Branch: `fix/296-review-session-queue` (already created off `main` at `bbc5aab`). Never push to `main`. Open a PR at the end.
- Full check before each Go commit:
  ```bash
  gofmt -l .
  go vet ./...
  go run honnef.co/go/tools/cmd/staticcheck@v0.8.0 ./...
  go mod tidy && git diff --exit-code go.mod go.sum
  go test ./... -race -count=1
  ```
- **HTMX behaviour unchanged.** The swap target stays `#review-body` / `innerHTML`. Grade/undo routes, form fields (`card_id`, `rating`), the `?deck=` scope parameter and every HTMX response body stay as they are. The one intended difference is on an *error* path: a grade or undo with a `?deck=` the user doesn't own now returns 404 *without* applying the grade. The status code is the same as before.
- **"N of M" semantics unchanged:** M = `TodayTally.Reviewed` + cards remaining in today's queue for the scope (exactly `len(DueQueue)`), N = Reviewed + 1.
- **No migrations.** Nothing here needs a schema change.
- No new dependencies. CSP: no inline `<script>`, no `style=`.
- `htmlassert` selectors: descendant chains of simple parts only (`tag`, `.class`, `#id`, `tag[attr=value]`); no compound parts like `a.x[y]`.
- Keep names from U4/U5: `reviewView`, `reviewCardView`, `reviewSummary`, `.flash-review-summary`, `.flash-undo`, `.flash-undo-btn`, `.flash-break-banner`, `.flash-big-btn`, `.flash-back-to-decks`, `DeckSummary.ReviewNow`.
- Tests: `newServer(t)`, `s.Get`, `s.Post`, `s.PostHX`, `s.Submit`, `httpGet`, `itoa`; store tests `newFixture(t)` (`f.store`, `f.alice`); `flash.RatingAgain…RatingEasy`.
- Commits: Conventional Commits, scope `flash`, referencing the issue in the subject, e.g. `perf(flash): … (#296)`. Per CONTRIBUTING.md, polish on shipped features is `refactor`/`perf`/`fix`, not `feat`.

---

## File map

| File | Change |
|---|---|
| `internal/apps/flash/handlers_review.go` | `reviewScope`, `reviewBreak`, `reviewPath`, `undoFromQuery`; `renderReview(…, deck *Deck, …)`; grade/undo resolve scope first and redirect when not HTMX; render reads `QueueFront` |
| `internal/apps/flash/templates/review.html` | `review-break` block; `{{else if .Break}}` branch |
| `internal/ui/static/app.css` | `.flash-review-break` shares `.flash-review-summary`'s layout |
| `internal/apps/flash/review.go` | `QueueCard.DueAt`; `withDueAt`; `earliestDue`, `mergeByDue`; `DueQueue` merges by due date; id tiebreaks; `QueueFront` |
| `internal/apps/flash/deck_summary.go` | extract `deckSummary` from `DeckSummaries` |
| `internal/apps/flash/handlers_decks.go` | `maxSnoozeDays`; `snoozeDeck` rejects `days > 365` |
| `docs/superpowers/specs/2026-09-17-on-flash-f2-review-engine-design.md` | queue-construction step 5 records the ordering |
| Tests | `handlers_review_test.go`, `review_test.go`, `handlers_decks_test.go` |

---

### Task 1: Pass the loaded deck to `renderReview`; a snoozed deck's review page says it's on a break (#333, #296.4)

**Files:**
- Modify: `internal/apps/flash/handlers_review.go`, `internal/apps/flash/templates/review.html`, `internal/ui/static/app.css`
- Test: `internal/apps/flash/handlers_review_test.go`

**Interfaces:**
- Consumes: `Store.DeckByID`, `Deck.IsSnoozed`, `Deck.SnoozedUntil`, `POST /flash/{deckID}/unsnooze` (non-HTMX → 303 `/flash/{deckID}`).
- Produces:
  ```go
  func (a *App) reviewScope(w http.ResponseWriter, r *http.Request, userID int64) (*Deck, bool) // nil = every deck
  func (a *App) renderReview(w http.ResponseWriter, r *http.Request, userID int64, deck *Deck, status int, lastGradedCardID int64)
  type reviewBreak struct {
      DeckID int64
      Until  string // "2 Jan"
  }
  // reviewView gains: Break *reviewBreak — set instead of Summary on a snoozed deck's own review page.
  ```
  Template hooks: `.flash-review-break`, containing `.flash-break-banner` and `form[action=/flash/{id}/unsnooze]`.

- [ ] **Step 1: Write the failing tests**

In `internal/apps/flash/handlers_review_test.go`, replace `TestGradingScopedToSomeoneElsesDeckIs404`'s final block (the `rec := s.PostHX(... "?deck="+itoa(bobDeck.ID) ...)` call and its check) with:

```go
	// The card is Alice's own, but the ?deck= scope is Bob's deck: that must
	// 404 before anything is graded, not grade first and then fail to
	// re-render.
	rec := s.PostHX(t, s.Alice, "/flash/review/grade?deck="+itoa(bobDeck.ID),
		url.Values{"card_id": {itoa(card.ID)}, "rating": {"3"}})
	if rec.Code != 404 {
		t.Errorf("grade scoped to someone else's deck = %d, want 404", rec.Code)
	}
	if _, reviewed, err := s.Store.CardState(t.Context(), s.Alice.User.ID, card.ID); err != nil || reviewed {
		t.Errorf("after a 404 grade: reviewed = %v (err %v), want the card left ungraded", reviewed, err)
	}
```

Append:

```go
func TestReviewOfASnoozedDeckSaysItIsOnABreak(t *testing.T) {
	s := newServer(t)
	now := time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC)
	s.Store.SetClock(func() time.Time { return now })
	deck, err := s.Store.CreateDeck(t.Context(), s.Alice.User.ID, "Spanish", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Store.CreateCard(t.Context(), s.Alice.User.ID, deck.ID, flash.CardTypeBasic, "hola", "hello", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Store.SnoozeDeck(t.Context(), s.Alice.User.ID, deck.ID, now.AddDate(0, 0, 7)); err != nil {
		t.Fatal(err)
	}

	doc := s.Get(t, s.Alice, "/flash/review/"+itoa(deck.ID))
	brk := doc.MustHave(".flash-review-break")
	if text := htmlassert.Text(brk); !strings.Contains(text, "Taking a break until 1 Oct") {
		t.Errorf("break panel = %q, want it to say until 1 Oct", text)
	}
	end := doc.MustHave(`.flash-review-break form[action="/flash/` + itoa(deck.ID) + `/unsnooze"]`)
	if got := htmlassert.Text(end); !strings.Contains(got, "End break") {
		t.Errorf("end-break form text = %q", got)
	}
	doc.MustNotHave(".flash-review-summary")
	doc.MustNotHave(".flash-review-card")

	// Review all is unaffected: a snoozed deck is simply left out there.
	all := s.Get(t, s.Alice, "/flash/review")
	all.MustNotHave(".flash-review-break")
	all.MustHave(".flash-review-summary")

	// Ending the break (no JS) lands on the deck pane; the card is back.
	s.Submit(t, s.Alice, "/flash/"+itoa(deck.ID)+"/unsnooze", url.Values{}, "/flash/"+itoa(deck.ID))
	back := s.Get(t, s.Alice, "/flash/review/"+itoa(deck.ID))
	back.MustHave(".flash-review-card")
	back.MustNotHave(".flash-review-break")
}
```

- [ ] **Step 2: Run to verify they fail**

Run: `go test ./internal/apps/flash/ -run 'TestGradingScopedToSomeoneElsesDeckIs404|TestReviewOfASnoozedDeckSaysItIsOnABreak' -count=1`
Expected: FAIL. `TestGradingScopedToSomeoneElsesDeckIs404` reports `reviewed = true`, and `TestReviewOfASnoozedDeckSaysItIsOnABreak` fails with `htmlassert: no element matches ".flash-review-break"`.

- [ ] **Step 3: Implement in `handlers_review.go`**

Add after `deckScopeString`:

```go
// reviewScope resolves the optional ?deck= scope to the deck it names — nil
// means every deck. A malformed id, or a deck that isn't userID's, writes a
// 404 and returns false; grade and undo call it before touching anything,
// so a bad scope never half-applies a grade.
func (a *App) reviewScope(w http.ResponseWriter, r *http.Request, userID int64) (*Deck, bool) {
	deckID, ok := a.deckScopeFromQuery(w, r)
	if !ok || deckID == nil {
		return nil, ok
	}
	d, err := a.store.DeckByID(r.Context(), userID, *deckID)
	if err != nil {
		a.fail(w, r, err)
		return nil, false
	}
	return &d, true
}
```

Add after `reviewCardView`:

```go
// reviewBreak is what a snoozed deck's own review page shows in place of
// the end-of-session summary: the deck is resting, not finished.
type reviewBreak struct {
	DeckID int64
	Until  string // "2 Jan", the deck pane's own format
}
```

In `reviewView`, replace the `Summary` line with these two:

```go
	Summary          *reviewSummary  // set when Current and Break are both nil
	Break            *reviewBreak    // set instead of Summary on a snoozed deck's own review page
```

Replace `review` and `deckReview` with:

```go
// review backs GET /review: the cross-deck queue, or one deck's queue if
// ?deck= names it.
func (a *App) review(w http.ResponseWriter, r *http.Request) {
	userID, ok := a.userID(w, r)
	if !ok {
		return
	}
	deck, ok := a.reviewScope(w, r, userID)
	if !ok {
		return
	}
	a.renderReview(w, r, userID, deck, http.StatusOK, 0)
}

// deckReview backs GET /review/{deckID}: always scoped to the deck named in
// the path, regardless of any ?deck= query value.
func (a *App) deckReview(w http.ResponseWriter, r *http.Request) {
	userID, ok := a.userID(w, r)
	if !ok {
		return
	}
	id, ok := a.deckIDFromPath(w, r)
	if !ok {
		return
	}
	d, err := a.store.DeckByID(r.Context(), userID, id)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	a.renderReview(w, r, userID, &d, http.StatusOK, 0)
}
```

Replace `renderReview` with:

```go
// renderReview draws the review screen for deck (nil = every deck). The
// caller has already loaded and ownership-checked deck, so it is not looked
// up again here (#333).
func (a *App) renderReview(w http.ResponseWriter, r *http.Request, userID int64, deck *Deck, status int, lastGradedCardID int64) {
	ctx := r.Context()
	now := a.store.now()
	var deckID *int64
	if deck != nil {
		deckID = &deck.ID
	}
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
	if deck != nil {
		view.ScopeName, view.StopURL, view.Color = deck.Name, "/flash/"+strconv.FormatInt(deck.ID, 10), deck.Color
	}

	switch {
	case len(queue) > 0:
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
		if deck == nil {
			view.Color = qc.Deck.Color
		}
	case deck != nil && deck.IsSnoozed(now):
		view.Break = &reviewBreak{DeckID: deck.ID, Until: deck.SnoozedUntil.Format("2 Jan")}
	default:
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
```

In `gradeCardHandler`, replace the `deckID, ok := a.deckScopeFromQuery(w, r)` block with the lines below, and change its final line to `a.renderReview(w, r, userID, deck, http.StatusOK, cardID)`:

```go
	deck, ok := a.reviewScope(w, r, userID)
	if !ok {
		return
	}
```

In `undoGradeHandler`, make the same `deckScopeFromQuery` → `reviewScope` replacement, and change its final line to `a.renderReview(w, r, userID, deck, http.StatusOK, 0)`.

- [ ] **Step 4: Template, in `templates/review.html`**

Replace:

```
    {{else}}
    {{template "review-summary" .}}
    {{end}}
```

with:

```
    {{else if .Break}}
    {{template "review-break" .}}
    {{else}}
    {{template "review-summary" .}}
    {{end}}
```

Insert this block before `{{define "review-summary"}}`:

```
{{define "review-break"}}
{{with .Break}}
<div class="flash-review-break" role="status">
    <h1>This deck is taking a break</h1>
    <div class="flash-break-banner">
        <span>Taking a break until {{.Until}}.</span>
        {{/* A plain POST, deliberately without hx-post: unsnooze's HTMX
             response is the deck-pane fragment (#deck-detail), which has no
             target on this page. The no-JS 303 lands on the deck pane, where
             Review is one click away. */}}
        <form method="post" action="/flash/{{.DeckID}}/unsnooze">
            <input type="hidden" name="{{csrfField}}" value="{{$.CSRFToken}}">
            <button type="submit" class="flash-big-btn">End break</button>
        </form>
    </div>
    <a class="flash-review-cta flash-back-to-decks" href="/flash/">Back to decks</a>
</div>
{{end}}
{{end}}

```

- [ ] **Step 5: CSS, in `internal/ui/static/app.css`**

In the "End-of-session summary." block, replace:

```css
.flash-review-summary {
```

with:

```css
.flash-review-summary,
.flash-review-break {
```

and replace `.flash-review-summary h1 { margin: 0; }` with:

```css
.flash-review-summary h1,
.flash-review-break h1 { margin: 0; }
```

- [ ] **Step 6: Run the tests**

Run: `go test ./internal/apps/flash/ -count=1`
Expected: PASS (all flash tests, including the U4 review tests, which are otherwise untouched).

- [ ] **Step 7: Full check, commit**

```bash
git add internal/apps/flash/handlers_review.go internal/apps/flash/templates/review.html internal/ui/static/app.css internal/apps/flash/handlers_review_test.go
git commit -m "refactor(flash): show a snoozed deck's break on its review page, load the deck once (#296, #333)"
```

---

### Task 2: Order due reviews by due date across decks (#296.2)

**Files:**
- Modify: `internal/apps/flash/review.go`, `docs/superpowers/specs/2026-09-17-on-flash-f2-review-engine-design.md`
- Test: `internal/apps/flash/review_test.go`

**Interfaces:**
- Produces:
  ```go
  type QueueCard struct {
      Card  Card
      Deck  Deck
      IsNew bool
      DueAt time.Time // when a review card fell due; zero for a new card
  }
  func earliestDue(lists [][]QueueCard) int      // index of the list whose head is due soonest; earlier list wins a tie; -1 if all empty
  func mergeByDue(lists [][]QueueCard) []QueueCard // k-way merge of per-deck due-ordered lists; consumes lists
  ```
  `DueQueue` order: all reviews by `DueAt` ascending (ties: `ListDecks` order, then card id), then new cards per deck as before. Within a deck, `dueReviewCards` orders `s.due_at ASC, c.id ASC` and `newQueueCards` orders `c.created_at ASC, c.id ASC`.

- [ ] **Step 1: Write the failing tests**

Add `"slices"` to `internal/apps/flash/review_test.go`'s imports, then append:

```go
func queueIDs(q []flash.QueueCard) []int64 {
	ids := make([]int64, len(q))
	for i, qc := range q {
		ids[i] = qc.Card.ID
	}
	return ids
}

func TestDueQueueOrdersReviewsByDueDateAcrossDecks(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	now := time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC)
	older, err := f.store.CreateDeck(ctx, f.alice.ID, "Older", "")
	if err != nil {
		t.Fatal(err)
	}
	newer, err := f.store.CreateDeck(ctx, f.alice.ID, "Newer", "") // listed first by ListDecks
	if err != nil {
		t.Fatal(err)
	}
	overdue, err := f.store.CreateCard(ctx, f.alice.ID, older.ID, flash.CardTypeBasic, "overdue", "x", "")
	if err != nil {
		t.Fatal(err)
	}
	recent, err := f.store.CreateCard(ctx, f.alice.ID, newer.ID, flash.CardTypeBasic, "recent", "x", "")
	if err != nil {
		t.Fatal(err)
	}
	fresh, err := f.store.CreateCard(ctx, f.alice.ID, newer.ID, flash.CardTypeBasic, "fresh", "x", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.GradeCard(ctx, f.alice.ID, overdue.ID, flash.RatingAgain, now.AddDate(0, 0, -10)); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.GradeCard(ctx, f.alice.ID, recent.ID, flash.RatingAgain, now.AddDate(0, 0, -1)); err != nil {
		t.Fatal(err)
	}

	queue, err := f.store.DueQueue(ctx, f.alice.ID, nil, now)
	if err != nil {
		t.Fatal(err)
	}
	// The older deck's card is ten days overdue, so it goes first even though
	// the newer deck is listed first; new cards still come after every review.
	if got, want := queueIDs(queue), []int64{overdue.ID, recent.ID, fresh.ID}; !slices.Equal(got, want) {
		t.Errorf("DueQueue order = %v, want %v (overdue, recent, fresh)", got, want)
	}
	if queue[0].DueAt.IsZero() || !queue[2].DueAt.IsZero() {
		t.Errorf("DueAt: review = %v, new = %v; want set for a review, zero for a new card", queue[0].DueAt, queue[2].DueAt)
	}
}

func TestDueQueueBreaksDueDateTiesByDeckOrder(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	now := time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC)
	past := now.AddDate(0, 0, -2)
	older, err := f.store.CreateDeck(ctx, f.alice.ID, "Older", "")
	if err != nil {
		t.Fatal(err)
	}
	newer, err := f.store.CreateDeck(ctx, f.alice.ID, "Newer", "")
	if err != nil {
		t.Fatal(err)
	}
	a, err := f.store.CreateCard(ctx, f.alice.ID, older.ID, flash.CardTypeBasic, "a", "x", "")
	if err != nil {
		t.Fatal(err)
	}
	b, err := f.store.CreateCard(ctx, f.alice.ID, newer.ID, flash.CardTypeBasic, "b", "x", "")
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []int64{a.ID, b.ID} {
		if _, err := f.store.GradeCard(ctx, f.alice.ID, id, flash.RatingAgain, past); err != nil {
			t.Fatal(err)
		}
	}

	queue, err := f.store.DueQueue(ctx, f.alice.ID, nil, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(queue) != 2 || !queue[0].DueAt.Equal(queue[1].DueAt) {
		t.Fatalf("setup: want two reviews due at the same instant, got %+v", queue)
	}
	if got, want := queueIDs(queue), []int64{b.ID, a.ID}; !slices.Equal(got, want) {
		t.Errorf("tie order = %v, want %v (the newer deck, as ListDecks lists it)", got, want)
	}
}
```

- [ ] **Step 2: Run to verify they fail**

Run: `go test ./internal/apps/flash/ -run 'TestDueQueueOrdersReviewsByDueDateAcrossDecks|TestDueQueueBreaksDueDateTiesByDeckOrder' -count=1`
Expected: build failure, `queue[0].DueAt undefined (type flash.QueueCard has no field or method DueAt)`.

- [ ] **Step 3: Implement in `review.go`**

Replace the `QueueCard` type with:

```go
// QueueCard is one card ready for review, with enough context to render it
// and to know which of a deck's two daily budgets grading it will spend.
type QueueCard struct {
	Card  Card
	Deck  Deck
	IsNew bool
	// DueAt is when a review card fell due; zero for a new card. It is what
	// orders reviews across decks, most overdue first.
	DueAt time.Time
}
```

Replace `DueQueue` with:

```go
// DueQueue returns cards eligible for review right now: every due review
// first, most overdue first across all decks in scope, then new cards deck
// by deck in ListDecks order. Each deck's daily limits cap its own share of
// both. If deckID is non-nil, only that deck is considered — and only if it
// is not currently snoozed, the same rule applied to every deck when deckID
// is nil (every non-snoozed deck belonging to userID).
//
// The review screen reads QueueFront instead, which returns this queue's
// first card and length without loading the rest; DueQueue is the
// reference its tests compare against.
func (st *Store) DueQueue(ctx context.Context, userID int64, deckID *int64, now time.Time) ([]QueueCard, error) {
	decks, err := st.dueQueueDecks(ctx, userID, deckID, now)
	if err != nil {
		return nil, err
	}

	reviews := make([][]QueueCard, 0, len(decks))
	var fresh []QueueCard
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
		reviews = append(reviews, due)

		newCards, err := st.newQueueCards(ctx, userID, d, newRemaining)
		if err != nil {
			return nil, err
		}
		fresh = append(fresh, newCards...)
	}
	return append(mergeByDue(reviews), fresh...), nil
}

// earliestDue returns the index of the list whose first card fell due
// soonest, or -1 if every list is empty. On a tie the earlier list wins —
// lists are in ListDecks order, so the newer deck — and within one deck
// dueReviewCards has already ordered by due date, then card id. DueQueue's
// merge and QueueFront's head both pick with this, so they cannot disagree.
func earliestDue(lists [][]QueueCard) int {
	best := -1
	for i, l := range lists {
		if len(l) == 0 {
			continue
		}
		if best < 0 || l[0].DueAt.Before(lists[best][0].DueAt) {
			best = i
		}
	}
	return best
}

// mergeByDue interleaves per-deck review lists, each already in due order,
// into one list, most overdue first. A household has a handful of decks, so
// a linear scan per card is plenty. It consumes lists.
func mergeByDue(lists [][]QueueCard) []QueueCard {
	var out []QueueCard
	for i := earliestDue(lists); i >= 0; i = earliestDue(lists) {
		out = append(out, lists[i][0])
		lists[i] = lists[i][1:]
	}
	return out
}
```

In `dueReviewCards`, change the query to select `s.due_at` last and add an id tiebreak:

```go
	query := `
		SELECT c.id, c.deck_id, c.user_id, c.card_type, c.front, c.back, c.notes, c.created_at, c.image_hash, c.audio_hash, s.due_at
		FROM flash_cards c
		JOIN flash_card_state s ON s.card_id = c.id AND s.user_id = c.user_id
		WHERE c.deck_id = ? AND c.user_id = ? AND s.due_at <= ?
		ORDER BY s.due_at ASC, c.id ASC`
```

and update its doc comment's first line to: `// dueReviewCards returns d's cards that are due at or before now, oldest due date first (then card id), up to limit (a negative limit means unlimited).`

In `newQueueCards`, change `ORDER BY c.created_at ASC` to `ORDER BY c.created_at ASC, c.id ASC`, and its comment's last words to `oldest-created first (then card id).`

Replace `queryQueueCards` with:

```go
// queryQueueCards runs a due-queue query. Review queries (isNew false)
// select s.due_at after the card columns; new-card queries don't.
func (st *Store) queryQueueCards(ctx context.Context, query string, args []any, d Deck, isNew bool) ([]QueueCard, error) {
	rows, err := st.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("flash: due queue: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []QueueCard
	for rows.Next() {
		var (
			row   rowScanner = rows
			dueAt string
		)
		if !isNew {
			row = withDueAt{rows: rows, dueAt: &dueAt}
		}
		c, err := scanCardRow(row)
		if err != nil {
			return nil, err
		}
		qc := QueueCard{Card: c, Deck: d, IsNew: isNew}
		if !isNew {
			if qc.DueAt, err = parseTime(dueAt); err != nil {
				return nil, err
			}
		}
		out = append(out, qc)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("flash: due queue: %w", err)
	}
	return out, nil
}

// withDueAt lets scanCardRow read a review query's row, which carries
// s.due_at after the usual card columns.
type withDueAt struct {
	rows  *sql.Rows
	dueAt *string
}

func (w withDueAt) Scan(dest ...any) error { return w.rows.Scan(append(dest, w.dueAt)...) }
```

- [ ] **Step 4: Record the ordering in the F2 spec**

In `docs/superpowers/specs/2026-09-17-on-flash-f2-review-engine-design.md`, replace step 5 of "Queue construction":

```
5. Concatenate due-reviews-then-new across all included decks (reviews
   first, then new, per the chosen ordering) and serve one card at a time.
```

with:

```
5. Serve every due review first, ordered by `due_at` across all included
   decks (most overdue first; ties go to deck order, newest deck first,
   then card id), then new cards deck by deck in that same deck order. Each
   deck's own limits from steps 3–4 still cap its share. Serve one card at
   a time. (Revised by #296: this used to concatenate deck by deck, so the
   newest deck always went first.)
```

- [ ] **Step 5: Run the tests**

Run: `go test ./internal/apps/flash/ -count=1`
Expected: PASS. The new ordering tests pass, and every existing `DueQueue` test still passes because they're single-deck or order-insensitive. `TestDeckSummariesAgreeWithDueQueue` is unaffected because only the order changed, not the length.

- [ ] **Step 6: Full check, commit**

```bash
git add internal/apps/flash/review.go internal/apps/flash/review_test.go docs/superpowers/specs/2026-09-17-on-flash-f2-review-engine-design.md
git commit -m "refactor(flash): order due reviews by due date across decks (#296)"
```

---

### Task 3: Read only the queue's head and length on each render (#296.1)

**Files:**
- Modify: `internal/apps/flash/deck_summary.go`, `internal/apps/flash/review.go`, `internal/apps/flash/handlers_review.go`
- Test: `internal/apps/flash/review_test.go`, `internal/apps/flash/handlers_review_test.go`

**Interfaces:**
- Consumes: `earliestDue` (Task 2), `dueQueueDecks`, `dueReviewCards`, `newQueueCards`.
- Produces:
  ```go
  func (st *Store) deckSummary(ctx context.Context, userID int64, d Deck, now time.Time) (DeckSummary, error) // one deck; DeckSummaries loops over it
  type QueueFront struct {
      Head      QueueCard // valid only when HasHead
      HasHead   bool
      Remaining int // exactly len(DueQueue) for the same scope and time
  }
  func (st *Store) QueueFront(ctx context.Context, userID int64, deckID *int64, now time.Time) (QueueFront, error)
  ```
  Invariants, pinned by the test: `Head` is `DueQueue(...)[0]` (same card, deck, `IsNew`, `DueAt`), `HasHead == (len(DueQueue) > 0)`, and `Remaining == len(DueQueue)`.

- [ ] **Step 1: Write the failing tests**

Append to `internal/apps/flash/review_test.go`:

```go
// queueFixture helpers keep TestQueueFrontAgreesWithDueQueue's cases short.
func qDeck(t *testing.T, f *fixture, name string) flash.Deck {
	t.Helper()
	d, err := f.store.CreateDeck(context.Background(), f.alice.ID, name, "")
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func qCard(t *testing.T, f *fixture, deckID int64, front string) flash.Card {
	t.Helper()
	c, err := f.store.CreateCard(context.Background(), f.alice.ID, deckID, flash.CardTypeBasic, front, "x", "")
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func qGrade(t *testing.T, f *fixture, cardID int64, rating int, at time.Time) {
	t.Helper()
	if _, err := f.store.GradeCard(context.Background(), f.alice.ID, cardID, rating, at); err != nil {
		t.Fatal(err)
	}
}

func qSettings(t *testing.T, f *fixture, deckID int64, newPerDay int, reviewsPerDay *int) {
	t.Helper()
	if _, err := f.store.UpdateDeckSettings(context.Background(), f.alice.ID, deckID, newPerDay, reviewsPerDay); err != nil {
		t.Fatal(err)
	}
}

// TestQueueFrontAgreesWithDueQueue pins the review screen's shortcut to the
// full queue: its head is DueQueue's first card and its count is
// len(DueQueue), across scopes, limits, snoozes and mixed decks.
func TestQueueFrontAgreesWithDueQueue(t *testing.T) {
	now := time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC)
	one := 1
	cases := []struct {
		name     string
		setup    func(t *testing.T, f *fixture) *int64 // returns the scope; nil = every deck
		wantHead string                                // front of the expected head; "" = empty queue
		wantLeft int
	}{
		{"no decks", func(t *testing.T, f *fixture) *int64 { return nil }, "", 0},
		{"new cards only", func(t *testing.T, f *fixture) *int64 {
			d := qDeck(t, f, "A")
			qCard(t, f, d.ID, "n1")
			qCard(t, f, d.ID, "n2")
			return nil
		}, "n1", 2},
		{"review before new, one deck", func(t *testing.T, f *fixture) *int64 {
			d := qDeck(t, f, "A")
			qCard(t, f, d.ID, "new")
			due := qCard(t, f, d.ID, "due")
			qGrade(t, f, due.ID, flash.RatingAgain, now.AddDate(0, 0, -3))
			return &d.ID
		}, "due", 2},
		{"new-card limit", func(t *testing.T, f *fixture) *int64 {
			d := qDeck(t, f, "A")
			qSettings(t, f, d.ID, 1, nil)
			qCard(t, f, d.ID, "n1")
			qCard(t, f, d.ID, "n2")
			qCard(t, f, d.ID, "n3")
			return &d.ID
		}, "n1", 1},
		{"review limit already spent today", func(t *testing.T, f *fixture) *int64 {
			d := qDeck(t, f, "A")
			qSettings(t, f, d.ID, 5, &one)
			a := qCard(t, f, d.ID, "a")
			b := qCard(t, f, d.ID, "b")
			qGrade(t, f, a.ID, flash.RatingAgain, now.AddDate(0, 0, -3))
			qGrade(t, f, b.ID, flash.RatingAgain, now.AddDate(0, 0, -3))
			qGrade(t, f, a.ID, flash.RatingGood, now) // today's one review; b stays due but capped
			qCard(t, f, d.ID, "new")
			return &d.ID
		}, "new", 1},
		{"snoozed deck, scoped", func(t *testing.T, f *fixture) *int64 {
			d := qDeck(t, f, "A")
			qCard(t, f, d.ID, "a")
			if _, err := f.store.SnoozeDeck(context.Background(), f.alice.ID, d.ID, now.AddDate(0, 0, 7)); err != nil {
				t.Fatal(err)
			}
			return &d.ID
		}, "", 0},
		{"snoozed deck skipped in Review all", func(t *testing.T, f *fixture) *int64 {
			a := qDeck(t, f, "A")
			due := qCard(t, f, a.ID, "snoozed-due")
			qGrade(t, f, due.ID, flash.RatingAgain, now.AddDate(0, 0, -3))
			if _, err := f.store.SnoozeDeck(context.Background(), f.alice.ID, a.ID, now.AddDate(0, 0, 7)); err != nil {
				t.Fatal(err)
			}
			b := qDeck(t, f, "B")
			qCard(t, f, b.ID, "b-new")
			return nil
		}, "b-new", 1},
		{"overdue card in an older deck goes first", func(t *testing.T, f *fixture) *int64 {
			older := qDeck(t, f, "Older")
			newer := qDeck(t, f, "Newer")
			o := qCard(t, f, older.ID, "overdue")
			r := qCard(t, f, newer.ID, "recent")
			qCard(t, f, newer.ID, "fresh")
			qGrade(t, f, o.ID, flash.RatingAgain, now.AddDate(0, 0, -10))
			qGrade(t, f, r.ID, flash.RatingAgain, now.AddDate(0, 0, -1))
			return nil
		}, "overdue", 3},
		{"an older deck's review beats a newer deck's new card", func(t *testing.T, f *fixture) *int64 {
			older := qDeck(t, f, "Older")
			newer := qDeck(t, f, "Newer")
			r := qCard(t, f, older.ID, "review")
			qGrade(t, f, r.ID, flash.RatingAgain, now.AddDate(0, 0, -1))
			qCard(t, f, newer.ID, "new")
			return nil
		}, "review", 2},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			ctx := context.Background()
			scope := tc.setup(t, f)

			queue, err := f.store.DueQueue(ctx, f.alice.ID, scope, now)
			if err != nil {
				t.Fatal(err)
			}
			front, err := f.store.QueueFront(ctx, f.alice.ID, scope, now)
			if err != nil {
				t.Fatal(err)
			}
			if front.Remaining != len(queue) || front.Remaining != tc.wantLeft {
				t.Errorf("Remaining = %d, len(DueQueue) = %d, want both %d", front.Remaining, len(queue), tc.wantLeft)
			}
			if front.HasHead != (len(queue) > 0) {
				t.Fatalf("HasHead = %v with len(DueQueue) = %d", front.HasHead, len(queue))
			}
			if !front.HasHead {
				if tc.wantHead != "" {
					t.Errorf("no head, want %q", tc.wantHead)
				}
				return
			}
			got, want := front.Head, queue[0]
			if got.Card.ID != want.Card.ID || got.Deck.ID != want.Deck.ID || got.IsNew != want.IsNew || !got.DueAt.Equal(want.DueAt) {
				t.Errorf("head = %+v, want DueQueue[0] = %+v", got, want)
			}
			if got.Card.Front != tc.wantHead {
				t.Errorf("head front = %q, want %q", got.Card.Front, tc.wantHead)
			}
		})
	}
}
```

Append to `internal/apps/flash/handlers_review_test.go`:

```go
func TestReviewAllShowsTheMostOverdueCardFirst(t *testing.T) {
	s := newServer(t)
	now := time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC)
	s.Store.SetClock(func() time.Time { return now })
	older, err := s.Store.CreateDeck(t.Context(), s.Alice.User.ID, "Older", "")
	if err != nil {
		t.Fatal(err)
	}
	newer, err := s.Store.CreateDeck(t.Context(), s.Alice.User.ID, "Newer", "")
	if err != nil {
		t.Fatal(err)
	}
	overdue, err := s.Store.CreateCard(t.Context(), s.Alice.User.ID, older.ID, flash.CardTypeBasic, "overdue", "x", "")
	if err != nil {
		t.Fatal(err)
	}
	recent, err := s.Store.CreateCard(t.Context(), s.Alice.User.ID, newer.ID, flash.CardTypeBasic, "recent", "x", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Store.CreateCard(t.Context(), s.Alice.User.ID, newer.ID, flash.CardTypeBasic, "fresh", "x", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Store.GradeCard(t.Context(), s.Alice.User.ID, overdue.ID, flash.RatingAgain, now.AddDate(0, 0, -10)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Store.GradeCard(t.Context(), s.Alice.User.ID, recent.ID, flash.RatingAgain, now.AddDate(0, 0, -1)); err != nil {
		t.Fatal(err)
	}

	doc := s.Get(t, s.Alice, "/flash/review")
	if got := htmlassert.Text(doc.MustHave("#review-card .flash-card-front")); !strings.Contains(got, "overdue") {
		t.Errorf("first card front = %q, want the older deck's overdue card", got)
	}
	// Nothing graded today, three cards queued: M is still exact.
	if got := htmlassert.Text(doc.MustHave(".flash-review-count")); got != "1 of 3" {
		t.Errorf("count = %q, want 1 of 3", got)
	}
}
```

- [ ] **Step 2: Run to verify they fail**

Run: `go test ./internal/apps/flash/ -run 'TestQueueFrontAgreesWithDueQueue|TestReviewAllShowsTheMostOverdueCardFirst' -count=1`
Expected: build failure, `f.store.QueueFront undefined`. (Once `QueueFront` exists, the handler test already passes because of Task 2's ordering. It's here to guard the render switch-over.)

- [ ] **Step 3: Extract `deckSummary` in `deck_summary.go`**

Replace `DeckSummaries` with:

```go
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
		s, err := st.deckSummary(ctx, userID, d, now)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, nil
}

// deckSummary builds one deck's DeckSummary. QueueFront counts today's
// queue with it too, so the Review button, the review screen's "N of M" and
// DueQueue's length all follow one rule.
func (st *Store) deckSummary(ctx context.Context, userID int64, d Deck, now time.Time) (DeckSummary, error) {
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
		return DeckSummary{}, fmt.Errorf("flash: deck summaries: %w", err)
	}
	if nextDue.Valid {
		t, err := parseTime(nextDue.String)
		if err != nil {
			return DeckSummary{}, err
		}
		s.NextDueAt = &t
	}

	if !s.Snoozed {
		newCount, reviewCount, err := st.dailyCounts(ctx, userID, d.ID, now)
		if err != nil {
			return DeckSummary{}, err
		}
		reviewsRemaining, newRemaining := dailyBudget(d, newCount, reviewCount)
		s.DueToday = s.DueTotal
		if reviewsRemaining >= 0 && s.DueToday > reviewsRemaining {
			s.DueToday = reviewsRemaining
		}
		s.NewToday = min(s.NewUnseen, newRemaining)
		s.ReviewNow = s.DueToday + s.NewToday
	}
	return s, nil
}
```

- [ ] **Step 4: Add `QueueFront` in `review.go`** (after `mergeByDue`)

```go
// QueueFront is what the review screen needs from today's queue: the card
// to show and how many are left, without loading the rest.
type QueueFront struct {
	Head      QueueCard // valid only when HasHead
	HasHead   bool
	Remaining int // exactly len(DueQueue) for the same scope and time
}

// QueueFront returns DueQueue's first card and its length while reading at
// most one card per deck. The head is the most overdue of each deck's first
// review (when that deck's review budget allows one, picked by earliestDue
// exactly as DueQueue's merge does). Only if no deck has a review does it
// fall back to the first new card of the first deck, in ListDecks order,
// whose new-card budget allows one. Remaining sums deckSummary's ReviewNow,
// the Review button's own count.
func (st *Store) QueueFront(ctx context.Context, userID int64, deckID *int64, now time.Time) (QueueFront, error) {
	decks, err := st.dueQueueDecks(ctx, userID, deckID, now)
	if err != nil {
		return QueueFront{}, err
	}

	var front QueueFront
	heads := make([][]QueueCard, len(decks))
	newRoom := make([]bool, len(decks))
	for i, d := range decks {
		s, err := st.deckSummary(ctx, userID, d, now)
		if err != nil {
			return QueueFront{}, err
		}
		front.Remaining += s.ReviewNow
		newRoom[i] = s.NewToday > 0
		if s.DueToday > 0 {
			if heads[i], err = st.dueReviewCards(ctx, userID, d, now, 1); err != nil {
				return QueueFront{}, err
			}
		}
	}
	if i := earliestDue(heads); i >= 0 {
		front.Head, front.HasHead = heads[i][0], true
		return front, nil
	}
	for i, d := range decks {
		if !newRoom[i] {
			continue
		}
		fresh, err := st.newQueueCards(ctx, userID, d, 1)
		if err != nil {
			return QueueFront{}, err
		}
		if len(fresh) > 0 {
			front.Head, front.HasHead = fresh[0], true
			return front, nil
		}
	}
	return front, nil
}
```

- [ ] **Step 5: Switch `renderReview` to `QueueFront`** (in `handlers_review.go`)

Replace:

```go
	queue, err := a.store.DueQueue(ctx, userID, deckID, now)
	if err != nil {
```

with:

```go
	front, err := a.store.QueueFront(ctx, userID, deckID, now)
	if err != nil {
```

Replace `Total:             tally.Reviewed + len(queue),` with `Total:             tally.Reviewed + front.Remaining,`.

Replace:

```go
	case len(queue) > 0:
		qc := queue[0]
```

with:

```go
	case front.HasHead:
		qc := front.Head
```

- [ ] **Step 6: Run the tests**

Run: `go test ./internal/apps/flash/ -count=1`
Expected: PASS. `TestQueueFrontAgreesWithDueQueue`'s nine subtests and `TestReviewAllShowsTheMostOverdueCardFirst` pass. `TestReviewShowsProgress` ("1 of 3" → "2 of 3"), `TestReviewRespectsTheDailyNewCardLimit` and `TestDeckSummariesAgreeWithDueQueue` also pass unchanged.

- [ ] **Step 7: Full check, commit**

```bash
git add internal/apps/flash/deck_summary.go internal/apps/flash/review.go internal/apps/flash/handlers_review.go internal/apps/flash/review_test.go internal/apps/flash/handlers_review_test.go
git commit -m "perf(flash): read only the queue's head and count on each review render (#296)"
```

---

### Task 4: POST-redirect-GET for no-JS grade and undo (#296.3)

**Files:**
- Modify: `internal/apps/flash/handlers_review.go`
- Test: `internal/apps/flash/handlers_review_test.go`

**Interfaces:**
- Consumes: `reviewScope`, `renderReview(…, deck *Deck, …)` (Task 1).
- Produces:
  ```go
  func reviewPath(deck *Deck) string     // "/flash/review" or "/flash/review/{id}"
  func undoFromQuery(r *http.Request) int64 // ?undo= as a positive id, else 0
  ```
  Non-HTMX `POST /flash/review/grade[?deck=N]` → 303 `reviewPath(deck)?undo={cardID}`. Non-HTMX `POST /flash/review/undo[?deck=N]` → 303 `reviewPath(deck)`. `GET /flash/review` and `GET /flash/review/{deckID}` render the Undo form for `?undo=`. HTMX responses are unchanged.

- [ ] **Step 1: Write the failing tests**

Append to `internal/apps/flash/handlers_review_test.go`:

```go
func TestGradingWithoutHTMXRedirectsBackToTheReview(t *testing.T) {
	s := newServer(t)
	deck, err := s.Store.CreateDeck(t.Context(), s.Alice.User.ID, "Spanish", "")
	if err != nil {
		t.Fatal(err)
	}
	first, err := s.Store.CreateCard(t.Context(), s.Alice.User.ID, deck.ID, flash.CardTypeBasic, "uno", "one", "")
	if err != nil {
		t.Fatal(err)
	}
	second, err := s.Store.CreateCard(t.Context(), s.Alice.User.ID, deck.ID, flash.CardTypeBasic, "dos", "two", "")
	if err != nil {
		t.Fatal(err)
	}

	// Scoped to one deck: back to that deck's review page.
	s.Submit(t, s.Alice, "/flash/review/grade?deck="+itoa(deck.ID),
		url.Values{"card_id": {itoa(first.ID)}, "rating": {"3"}},
		"/flash/review/"+itoa(deck.ID)+"?undo="+itoa(first.ID))
	if _, reviewed, err := s.Store.CardState(t.Context(), s.Alice.User.ID, first.ID); err != nil || !reviewed {
		t.Fatalf("after a no-JS grade: reviewed = %v (err %v), want graded", reviewed, err)
	}

	// Review all: back to Review all.
	s.Submit(t, s.Alice, "/flash/review/grade",
		url.Values{"card_id": {itoa(second.ID)}, "rating": {"3"}},
		"/flash/review?undo="+itoa(second.ID))
}

func TestReviewOffersUndoFromTheRedirect(t *testing.T) {
	s := newServer(t)
	deck, err := s.Store.CreateDeck(t.Context(), s.Alice.User.ID, "Spanish", "")
	if err != nil {
		t.Fatal(err)
	}
	card, err := s.Store.CreateCard(t.Context(), s.Alice.User.ID, deck.ID, flash.CardTypeBasic, "hola", "hello", "")
	if err != nil {
		t.Fatal(err)
	}
	s.Submit(t, s.Alice, "/flash/review/grade?deck="+itoa(deck.ID),
		url.Values{"card_id": {itoa(card.ID)}, "rating": {"3"}},
		"/flash/review/"+itoa(deck.ID)+"?undo="+itoa(card.ID))

	doc := s.Get(t, s.Alice, "/flash/review/"+itoa(deck.ID)+"?undo="+itoa(card.ID))
	doc.MustHave(".flash-undo-btn")
	id := doc.MustHave(".flash-undo input[name=card_id]")
	if v, _ := htmlassert.Attr(id, "value"); v != itoa(card.ID) {
		t.Errorf("undo card_id = %q, want %s", v, itoa(card.ID))
	}
	form := doc.MustHave(".flash-review form.flash-undo")
	if action, _ := htmlassert.Attr(form, "action"); action != "/flash/review/undo?deck="+itoa(deck.ID) {
		t.Errorf("undo action = %q, want it to keep the deck scope", action)
	}
}

func TestUndoWithoutHTMXRedirectsBackToTheReview(t *testing.T) {
	s := newServer(t)
	deck, err := s.Store.CreateDeck(t.Context(), s.Alice.User.ID, "Spanish", "")
	if err != nil {
		t.Fatal(err)
	}
	card, err := s.Store.CreateCard(t.Context(), s.Alice.User.ID, deck.ID, flash.CardTypeBasic, "hola", "hello", "")
	if err != nil {
		t.Fatal(err)
	}
	s.PostHX(t, s.Alice, "/flash/review/grade?deck="+itoa(deck.ID), url.Values{"card_id": {itoa(card.ID)}, "rating": {"3"}})

	s.Submit(t, s.Alice, "/flash/review/undo?deck="+itoa(deck.ID),
		url.Values{"card_id": {itoa(card.ID)}}, "/flash/review/"+itoa(deck.ID))
	doc := s.Get(t, s.Alice, "/flash/review/"+itoa(deck.ID))
	doc.MustHave(".flash-review-card") // the card is back
	doc.MustNotHave(".flash-undo-btn") // and the undo slot is spent
}

func TestReviewIgnoresAMalformedUndoParam(t *testing.T) {
	s := newServer(t)
	for _, v := range []string{"abc", "0", "-3", ""} {
		doc := s.Get(t, s.Alice, "/flash/review?undo="+url.QueryEscape(v))
		doc.MustNotHave(".flash-undo-btn")
	}
}
```

- [ ] **Step 2: Run to verify they fail**

Run: `go test ./internal/apps/flash/ -run 'TestGradingWithoutHTMXRedirectsBackToTheReview|TestReviewOffersUndoFromTheRedirect|TestUndoWithoutHTMXRedirectsBackToTheReview|TestReviewIgnoresAMalformedUndoParam' -count=1`
Expected: FAIL. The first three fail with `POST /flash/review/grade?deck=1 = 200, want 303` (or the `/undo` equivalent). `TestReviewIgnoresAMalformedUndoParam` already passes and guards the parser.

- [ ] **Step 3: Implement in `handlers_review.go`**

Add after `reviewScope`:

```go
// reviewPath is the review page for a scope: one deck's, or Review all's.
func reviewPath(deck *Deck) string {
	if deck == nil {
		return "/flash/review"
	}
	return "/flash/review/" + strconv.FormatInt(deck.ID, 10)
}

// undoFromQuery reads the ?undo= card id a no-JS grade redirects with, so
// the page it lands on still offers Undo. Anything but a positive id is
// ignored: the value only decides whether an Undo button renders, and the
// undo route re-checks the card's owner and undo slot itself.
func undoFromQuery(r *http.Request) int64 {
	id, err := strconv.ParseInt(r.URL.Query().Get("undo"), 10, 64)
	if err != nil || id <= 0 {
		return 0
	}
	return id
}
```

In `review`, change the last line to `a.renderReview(w, r, userID, deck, http.StatusOK, undoFromQuery(r))`.
In `deckReview`, change the last line to `a.renderReview(w, r, userID, &d, http.StatusOK, undoFromQuery(r))`.

In `gradeCardHandler`, replace the final `a.renderReview(w, r, userID, deck, http.StatusOK, cardID)` with:

```go
	if !web.IsHTMX(r) {
		// POST-redirect-GET, like every other Flash form: refreshing the
		// page after a no-JS grade must not grade again. ?undo= keeps the
		// Undo button that the HTMX response would have shown.
		http.Redirect(w, r, reviewPath(deck)+"?undo="+strconv.FormatInt(cardID, 10), http.StatusSeeOther)
		return
	}
	a.renderReview(w, r, userID, deck, http.StatusOK, cardID)
```

In `undoGradeHandler`, replace the final `a.renderReview(w, r, userID, deck, http.StatusOK, 0)` with:

```go
	if !web.IsHTMX(r) {
		http.Redirect(w, r, reviewPath(deck), http.StatusSeeOther)
		return
	}
	a.renderReview(w, r, userID, deck, http.StatusOK, 0)
```

- [ ] **Step 4: Run the tests**

Run: `go test ./internal/apps/flash/ -count=1`
Expected: PASS. Every existing grade/undo test uses `PostHX`, so it still gets a 200 fragment. `TestGradingRequiresCSRF` still gets 403.

- [ ] **Step 5: Full check, commit**

```bash
git add internal/apps/flash/handlers_review.go internal/apps/flash/handlers_review_test.go
git commit -m "fix(flash): POST-redirect-GET for no-JS grade and undo (#296)"
```

---

### Task 5: Cap a break at 365 days (#296.5)

**Files:**
- Modify: `internal/apps/flash/handlers_decks.go`
- Test: `internal/apps/flash/handlers_decks_test.go`

**Interfaces:**
- Produces: `const maxSnoozeDays = 365`. `POST /flash/{deckID}/snooze` with `days` outside `1..365` (or not an integer) → 400 via `a.deps.Errors.Status`, the same response it already sends for `days <= 0`. The deck is left unchanged.

- [ ] **Step 1: Write the failing test**

Append to `internal/apps/flash/handlers_decks_test.go`:

```go
func TestSnoozeDaysMustBeBetweenOneAndAYear(t *testing.T) {
	s := newServer(t)
	now := time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC)
	s.Store.SetClock(func() time.Time { return now })
	deck, err := s.Store.CreateDeck(t.Context(), s.Alice.User.ID, "Spanish", "")
	if err != nil {
		t.Fatal(err)
	}
	path := "/flash/" + itoa(deck.ID) + "/snooze"

	for _, days := range []string{"", "abc", "0", "-1", "366", "100000", "9223372036854775807", "99999999999999999999"} {
		t.Run("rejects "+days, func(t *testing.T) {
			rec := s.Post(t, s.Alice, path, url.Values{"days": {days}})
			if rec.Code != 400 {
				t.Errorf("snooze days=%q = %d, want 400", days, rec.Code)
			}
			d, err := s.Store.DeckByID(t.Context(), s.Alice.User.ID, deck.ID)
			if err != nil {
				t.Fatal(err)
			}
			if d.SnoozedUntil != nil {
				t.Errorf("days=%q was rejected but the deck is snoozed until %v", days, d.SnoozedUntil)
			}
		})
	}

	s.Submit(t, s.Alice, path, url.Values{"days": {"365"}}, "/flash/"+itoa(deck.ID))
	d, err := s.Store.DeckByID(t.Context(), s.Alice.User.ID, deck.ID)
	if err != nil {
		t.Fatal(err)
	}
	if want := now.AddDate(0, 0, 365); d.SnoozedUntil == nil || !d.SnoozedUntil.Equal(want) {
		t.Errorf("365 days: snoozed until %v, want %v", d.SnoozedUntil, want)
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/apps/flash/ -run TestSnoozeDaysMustBeBetweenOneAndAYear -count=1`
Expected: FAIL. `rejects_366` and `rejects_100000` report `= 303, want 400` and a snoozed deck. `rejects_9223372036854775807` reports 303 or 500, depending on whether the overflowed date round-trips.

- [ ] **Step 3: Implement in `handlers_decks.go`**

Add above `snoozeDeck`:

```go
// maxSnoozeDays caps a break at a year. The pane only offers a week or a
// month; the cap stops a hand-edited form from pushing snoozed_until to an
// absurd (or overflowed) date.
const maxSnoozeDays = 365
```

In `snoozeDeck`, replace `if err != nil || days <= 0 {` with:

```go
	if err != nil || days <= 0 || days > maxSnoozeDays {
```

- [ ] **Step 4: Run the tests**

Run: `go test ./internal/apps/flash/ -count=1`
Expected: PASS (including `TestSnoozeAndUnsnoozeDeckOverHTTP`, which uses 7 days).

- [ ] **Step 5: Full check, commit**

```bash
git add internal/apps/flash/handlers_decks.go internal/apps/flash/handlers_decks_test.go
git commit -m "fix(flash): cap a deck's break at 365 days (#296)"
```

---

### Task 6: Full check, manual verification, PR

- [ ] **Step 1: Full check**

```bash
gofmt -l .                                              # prints nothing
go vet ./...
go run honnef.co/go/tools/cmd/staticcheck@v0.8.0 ./...
go mod tidy && git diff --exit-code go.mod go.sum
go test ./internal/arch/... -count=1
go test ./... -race -count=1
```
Expected: no output from `gofmt`, and `ok` for every package.

- [ ] **Step 2: Manual verification** (build, start the `onsuite` preview, ask the user to sign in; two decks, one with an old overdue card)

1. Review all serves the older deck's overdue card first; "1 of N" matches the Review button counts added up.
2. Grading with the keyboard/HTMX behaves exactly as before: the fragment swaps, the count advances, and U undoes.
3. With JS disabled: Got it lands on `/flash/review/{id}?undo=…` with an Undo button, and refreshing doesn't grade again. Undo lands on `/flash/review/{id}`, the card is back, and there's no Undo button.
4. Take a break on a deck, then open `/flash/review/{id}`: "This deck is taking a break", "Taking a break until 1 Oct.", End break. End break lands on the deck pane with the Review button back.
5. Dark theme, 640px width; no console/CSP errors.

- [ ] **Step 3: Open the PR**

```bash
git push -u origin fix/296-review-session-queue
gh pr create --title "fix(flash): review session queue follow-ups (#296, #333)" --body "$(cat <<'EOF'
## Summary
- Review all now serves due reviews most-overdue-first across decks. Ties go to deck order, then card id. New cards still come after every review, per deck, and per-deck daily limits are unchanged.
- Each review render reads only the queue's head and its length (`Store.QueueFront`) rather than the whole queue. "N of M" is unchanged: `QueueFront.Remaining` is pinned to `len(DueQueue)`.
- No-JS grade and undo now use POST-redirect-GET (303). Grade carries `?undo=` so the Undo button survives the redirect.
- A snoozed deck's own review page says "Taking a break until …" and has End break.
- `snooze` rejects `days` > 365 with a 400.
- `renderReview` takes the deck its caller already loaded, so it's looked up once. Grade and undo also check the `?deck=` scope before grading.
- F2 spec's queue-construction step 5 now records the ordering.

Closes #296
Closes #333

Plan: docs/superpowers/plans/2026-09-24-flash-review-session-queue.md

## Test plan
- [ ] `go test ./... -race -count=1`
- [ ] Manual checklist from the plan's Task 6
EOF
)"
```

---

## Self-review

- **Coverage:** #296.1 → Task 3. #296.2 → Task 2 (+ the Task 3 cases and handler test). #296.3 → Task 4. #296.4 → Task 1. #296.5 → Task 5. #333 → Task 1. PR closes both → Task 6. Every decision in "Decisions already made" maps to a test: global ordering and ties (`TestDueQueueOrdersReviewsByDueDateAcrossDecks`, `TestDueQueueBreaksDueDateTiesByDeckOrder`); head/count parity across scoped, all, limited, snoozed and mixed (`TestQueueFrontAgreesWithDueQueue`); unchanged M (`TestReviewShowsProgress`, `TestReviewAllShowsTheMostOverdueCardFirst`); PRG and undo param (Task 4's four tests); break page (`TestReviewOfASnoozedDeckSaysItIsOnABreak`); cap (`TestSnoozeDaysMustBeBetweenOneAndAYear`).
- **Placeholders:** none. Every step has complete code or an exact replacement, plus exact commands.
- **Type consistency:** `renderReview(w, r, userID int64, deck *Deck, status int, lastGradedCardID int64)` is used by `review`, `deckReview`, `gradeCardHandler` and `undoGradeHandler` from Task 1 on. `QueueCard.DueAt time.Time` is set only by `queryQueueCards`'s review branch. `earliestDue([][]QueueCard) int` is shared by `mergeByDue` and `QueueFront`. `QueueFront{Head, HasHead, Remaining}` is read by `renderReview` in Task 3. `reviewBreak{DeckID int64; Until string}` is read by the `review-break` template through `.Break`. `reviewPath(*Deck) string` and `undoFromQuery(*http.Request) int64` are used only in Task 4. Test helpers `qDeck`/`qCard`/`qGrade`/`qSettings`/`queueIDs` don't collide with existing `flash_test` helpers (`summaryFor`, `ratingCount`, `itoa`, `httpGet`, `httpPost`).
- **Ordering:** each task builds and passes on its own. Task 3's "older deck first" case depends on Task 2. Task 4 depends on Task 1's `deck *Deck` signature. Task 5 is independent.
