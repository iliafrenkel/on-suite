# ON Flash UI U2 — Card component and Cards mode Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Draw cards as real, flippable cards, and move a deck's cards into the home screen's right pane as a searchable, tag-filterable grid of mini cards; opening one shows it large with previous/next.

**Architecture:** A Go cloze renderer turns card text into safe HTML for both faces. One `flash-card` template (in a new `card.partial.html`) draws the flippable card with a CSS-only checkbox flip; `flash-mini-card` draws grid tiles. The separate cards page (`cards.html`) is deleted: every card route now renders the home layout through U1's `buildDeckIndex` with new pane modes (`cards`, `card`, `card-new`, `card-edit`). A small fragment route (`GET /{deckID}/cards/grid`) powers live search.

**Tech Stack:** Go, SQLite, `html/template`, HTMX, plain CSS, `flash.js`.

**Spec:** [docs/superpowers/specs/2026-09-23-on-flash-ui-overhaul-design.md](../specs/2026-09-23-on-flash-ui-overhaul-design.md) — §1.3, §1.4, §1.6, §3. Mockups: section 2, "Cards mode".

**This is PR 2 of 6.** It requires U1 to be merged (branch off the `main` that contains it).

## Global Constraints

- Branch: `feat/flash-ui-u2` off an up-to-date `main` that already contains U1. Never push to `main`.
- Full check before each commit that touches Go:
  ```bash
  gofmt -l .
  go vet ./...
  go run honnef.co/go/tools/cmd/staticcheck@v0.8.0 ./...
  go mod tidy && git diff --exit-code go.mod go.sum
  go test ./... -race -count=1
  ```
- No new dependencies. CSP: no inline `<script>`, no `style=` attributes.
- Every `*.partial.html` is parsed into **every** Flash page's template set (`render.AddApp`). A partial must never define `content` or `head`, and a template name must be defined exactly once across the page file plus all partials.
- The **only** place card text becomes `template.HTML` is `renderCardFaces`/`renderCloze` (Task 1). Everything else prints card text as plain strings so `html/template` escapes it.
- Inside the flip `<label>` put only phrasing, non-interactive content (`<span>`, `<img>`). Links and `<audio>` go in `.flash-card-extras` after the label (spec §1.3).
- From U1 (do not rename): `buildDeckIndex`, `renderDeckIndex`, `renderDeckDetailWithList`, `deckDetailView`, `deckModeView`, templates `deck-detail-body`, `deck-detail-with-list`, `flash-pane-back`, `flash-stack`, CSS `.deck-c-*`, `--deck`, `--deck-soft`, `.flash-deck-toolbar`, `.flash-section-title`, icons `edit`, `trash`, `cards`, `arrow-left`, `plus`.
- `htmlassert` selectors: descendant chains of simple parts only — `tag`, `.class`, `#id`, `[attr]`, `[attr=value]`, or `tag` + one of those (`a.deck-row`, `a[href="/x"]`). **No compound parts** like `a.class[attr]` or `.a.b`: select by one qualifier, then check others with `htmlassert.Attr`.
- Tests: store tests `newFixture(t)` (package `flash_test`); handler tests `newServer(t)`, `s.Get`, `s.Submit`, `s.Do`, `httpGet`, `itoa`; internal tests `package flash`. For an HTMX GET in a test: `req := httpGet(t, path); req.Header.Set("HX-Request", "true"); rec := s.Do(t, s.Alice, req)`.
- Commits: Conventional Commits, scope `flash`.

---

## File map

| File | Change |
|---|---|
| `internal/apps/flash/cloze.go` | **Create** — `renderCloze`, `renderCardFaces` |
| `internal/apps/flash/card_view.go` | **Create** — `cardFace`, `newCardFace`, `filterCards`, URL helpers, grid/opened view models |
| `internal/apps/flash/tag.go` | add `CardTagsInDeck` |
| `internal/apps/flash/card.go` | add `CardStatuses` |
| `internal/apps/flash/handlers_cards.go` | card routes render the home layout; grid fragment handler |
| `internal/apps/flash/handlers_decks.go` | new pane modes and titles; `deckDetailView` gains `Grid`, `Opened`, `CardForm` |
| `internal/apps/flash/handlers_tags.go` | tag page uses mini cards |
| `internal/apps/flash/flash.go` | route `GET /{deckID}/cards/grid` |
| `internal/apps/flash/templates/card.partial.html` | **Create** — `flash-card`, `flash-mini-card` |
| `internal/apps/flash/templates/cards.partial.html` | **Create** — `deck-toolbar`, cards pane, grid, pills, opened card, card forms (moved) |
| `internal/apps/flash/templates/cards.html` | **Delete** |
| `internal/apps/flash/templates/decks.html` | dispatcher cases; deck view uses `deck-toolbar` |
| `internal/apps/flash/templates/tag-filter.html` | restyle as a mini-card grid |
| `internal/apps/flash/static/flash.js` | card viewer keys; handled-only `preventDefault` |
| `internal/ui/static/app.css` | card, flip, grid, pills CSS |
| Tests | `cloze_test.go` (new, internal), `card_view_test.go` (new, internal), `tag_test.go`, `card_test.go`, `handlers_cards_test.go`, `handlers_tags_test.go` |

---

### Task 1: Cloze renderer and card faces

**Files:**
- Create: `internal/apps/flash/cloze.go`
- Test: create `internal/apps/flash/cloze_test.go` (package `flash`)

**Interfaces:**
- Consumes: `Card`, `CardTypeCloze`.
- Produces:
  - `func renderCloze(front string) (question, answer template.HTML)`
  - `func renderCardFaces(c Card) (question, answer template.HTML)`
  - HTML classes `flash-blank`, `flash-blank-hint`, `flash-fill`.

- [ ] **Step 1: Write the failing tests**

Create `internal/apps/flash/cloze_test.go`:

```go
// internal/apps/flash/cloze_test.go
package flash

import (
	"strings"
	"testing"
)

const blank = `<span class="flash-blank"><span class="visually-hidden">blank</span></span>`

func TestRenderCloze(t *testing.T) {
	tests := []struct {
		name, front, wantQ, wantA string
	}{
		{
			name:  "numbered deletion",
			front: "The capital of France is {{c1::Paris}}.",
			wantQ: "The capital of France is " + blank + ".",
			wantA: `The capital of France is <mark class="flash-fill">Paris</mark>.`,
		},
		{
			name:  "bare braces",
			front: "{{Water}} boils at 100°C.",
			wantQ: blank + " boils at 100°C.",
			wantA: `<mark class="flash-fill">Water</mark> boils at 100°C.`,
		},
		{
			name:  "hint",
			front: "{{c1::Madrid::a city}} is in Spain.",
			wantQ: `<span class="flash-blank"><span class="visually-hidden">blank</span><span class="flash-blank-hint">a city</span></span> is in Spain.`,
			wantA: `<mark class="flash-fill">Madrid</mark> is in Spain.`,
		},
		{
			name:  "several deletions blank together",
			front: "{{c1::Red}} and {{c2::blue}} make {{c3::purple}}.",
			wantQ: blank + " and " + blank + " make " + blank + ".",
			wantA: `<mark class="flash-fill">Red</mark> and <mark class="flash-fill">blue</mark> make <mark class="flash-fill">purple</mark>.`,
		},
	}
	for _, tt := range tests {
		q, a := renderCloze(tt.front)
		if string(q) != tt.wantQ {
			t.Errorf("%s: question =\n  %s\nwant\n  %s", tt.name, q, tt.wantQ)
		}
		if string(a) != tt.wantA {
			t.Errorf("%s: answer =\n  %s\nwant\n  %s", tt.name, a, tt.wantA)
		}
	}
}

func TestRenderClozeLeavesUnmatchedBraces(t *testing.T) {
	q, a := renderCloze("Opens with {{ but never closes")
	if string(q) != "Opens with {{ but never closes" || string(a) != string(q) {
		t.Errorf("unmatched: q=%q a=%q, want the text unchanged", q, a)
	}
	q, _ = renderCloze("Empty {{}} marker")
	if string(q) != "Empty {{}} marker" {
		t.Errorf("empty marker: q=%q, want it left literal", q)
	}
}

func TestRenderClozeEscapesHTML(t *testing.T) {
	q, a := renderCloze(`<b>bold</b> & {{c1::<script>x</script>::<i>hint</i>}}`)
	for _, got := range []string{string(q), string(a)} {
		if strings.Contains(got, "<b>") || strings.Contains(got, "<script>") || strings.Contains(got, "<i>") {
			t.Errorf("unescaped HTML in %q", got)
		}
	}
	if !strings.Contains(string(q), "&lt;b&gt;bold&lt;/b&gt; &amp; ") {
		t.Errorf("question = %q, want the surrounding text escaped", q)
	}
	if !strings.Contains(string(q), "&lt;i&gt;hint&lt;/i&gt;") {
		t.Errorf("question = %q, want the hint escaped", q)
	}
	if !strings.Contains(string(a), "&lt;script&gt;x&lt;/script&gt;") {
		t.Errorf("answer = %q, want the deletion text escaped", a)
	}
}

func TestRenderCardFaces(t *testing.T) {
	q, a := renderCardFaces(Card{CardType: CardTypeBasic, Front: "2 < 3?", Back: "yes & no"})
	if string(q) != "2 &lt; 3?" || string(a) != "yes &amp; no" {
		t.Errorf("basic: q=%q a=%q", q, a)
	}
	q, a = renderCardFaces(Card{CardType: CardTypeCloze, Front: "{{c1::Paris}}", Back: "ignored"})
	if string(q) != blank || string(a) != `<mark class="flash-fill">Paris</mark>` {
		t.Errorf("cloze: q=%q a=%q", q, a)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/apps/flash/ -run 'RenderCloze|RenderCardFaces' -count=1`
Expected: build failure — `undefined: renderCloze`.

- [ ] **Step 3: Implement `cloze.go`**

```go
// internal/apps/flash/cloze.go
package flash

import (
	"html"
	"html/template"
	"regexp"
	"strings"
)

// clozeMarker matches one {{...}} deletion. Non-greedy, and (?s) so a
// deletion may span a line break. "{{}}" (nothing inside) does not match
// and stays literal text.
var clozeMarker = regexp.MustCompile(`(?s)\{\{(.+?)\}\}`)

// clozeNumber is the optional Anki-style "c1::" prefix inside a marker.
var clozeNumber = regexp.MustCompile(`^c\d+::`)

// renderCloze turns a cloze card's front into its two faces: the question,
// where every deletion is a blank (showing its hint, if the marker has
// one), and the answer, where every deletion is filled in and highlighted.
// All deletions blank at once — one card, not one per number (see the F3
// import spec's "Cloze cards" section).
//
// This, with renderCardFaces, is the only place card text becomes
// template.HTML: everything outside the markers, and each marker's text and
// hint, is HTML-escaped before the fixed markup below is added.
func renderCloze(front string) (question, answer template.HTML) {
	var q, a strings.Builder
	last := 0
	for _, m := range clozeMarker.FindAllStringSubmatchIndex(front, -1) {
		plain := html.EscapeString(front[last:m[0]])
		q.WriteString(plain)
		a.WriteString(plain)

		inner := clozeNumber.ReplaceAllString(front[m[2]:m[3]], "")
		text, hint, _ := strings.Cut(inner, "::")

		q.WriteString(`<span class="flash-blank"><span class="visually-hidden">blank</span>`)
		if hint != "" {
			q.WriteString(`<span class="flash-blank-hint">` + html.EscapeString(hint) + `</span>`)
		}
		q.WriteString(`</span>`)
		a.WriteString(`<mark class="flash-fill">` + html.EscapeString(text) + `</mark>`)
		last = m[1]
	}
	rest := html.EscapeString(front[last:])
	q.WriteString(rest)
	a.WriteString(rest)
	return template.HTML(q.String()), template.HTML(a.String())
}

// renderCardFaces returns the question and answer faces for any card: a
// cloze card's come from renderCloze; a basic card's are its escaped front
// and back.
func renderCardFaces(c Card) (question, answer template.HTML) {
	if c.CardType == CardTypeCloze {
		return renderCloze(c.Front)
	}
	return template.HTML(html.EscapeString(c.Front)), template.HTML(html.EscapeString(c.Back))
}
```

> `html.EscapeString` escapes `<`, `>`, `&`, `'` and `"`. The expected
> strings in the tests contain none of the last two, so they match exactly.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/apps/flash/ -run 'RenderCloze|RenderCardFaces' -count=1`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/apps/flash/cloze.go internal/apps/flash/cloze_test.go
git commit -m "feat(flash): render cloze cards as blanks and highlighted answers"
```

---

### Task 2: Store queries for the grid

**Files:**
- Modify: `internal/apps/flash/tag.go`, `internal/apps/flash/card.go`
- Test: `internal/apps/flash/tag_test.go`, `internal/apps/flash/card_test.go`

**Interfaces:**
- Produces:
  - `func (st *Store) CardTagsInDeck(ctx context.Context, userID, deckID int64) (map[int64][]string, error)` — card id → its tag names, alphabetical; cards without tags are absent.
  - `func (st *Store) CardStatuses(ctx context.Context, userID, deckID int64, now time.Time) (map[int64]string, error)` — card id → `"new"` (never reviewed by userID) or `"due"` (due at or before now); other cards absent.
  - `const CardStatusNew = "new"`, `const CardStatusDue = "due"`.

- [ ] **Step 1: Write the failing tests**

Append to `internal/apps/flash/tag_test.go` (check its imports: it needs `context`, `testing`, `flash`; add `"reflect"`):

```go
func TestCardTagsInDeck(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	d, err := f.store.CreateDeck(ctx, f.alice.ID, "Spanish", "")
	if err != nil {
		t.Fatal(err)
	}
	c1, err := f.store.CreateCard(ctx, f.alice.ID, d.ID, flash.CardTypeBasic, "hola", "hello", "")
	if err != nil {
		t.Fatal(err)
	}
	c2, err := f.store.CreateCard(ctx, f.alice.ID, d.ID, flash.CardTypeBasic, "pan", "bread", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.CreateCard(ctx, f.alice.ID, d.ID, flash.CardTypeBasic, "untagged", "x", ""); err != nil {
		t.Fatal(err)
	}
	if err := f.store.SetCardTags(ctx, f.alice.ID, c1.ID, []string{"phrases", "greetings"}); err != nil {
		t.Fatal(err)
	}
	if err := f.store.SetCardTags(ctx, f.alice.ID, c2.ID, []string{"food"}); err != nil {
		t.Fatal(err)
	}

	got, err := f.store.CardTagsInDeck(ctx, f.alice.ID, d.ID)
	if err != nil {
		t.Fatal(err)
	}
	want := map[int64][]string{c1.ID: {"greetings", "phrases"}, c2.ID: {"food"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("CardTagsInDeck = %v, want %v", got, want)
	}

	other, err := f.store.CardTagsInDeck(ctx, f.bob.ID, d.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(other) != 0 {
		t.Errorf("bob sees alice's tags: %v", other)
	}
}
```

Append to `internal/apps/flash/card_test.go` (needs `context`, `testing`, `time`, `flash`; add `time` if missing):

```go
func TestCardStatuses(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	now := time.Date(2026, 9, 23, 10, 0, 0, 0, time.UTC)
	d, err := f.store.CreateDeck(ctx, f.alice.ID, "Spanish", "")
	if err != nil {
		t.Fatal(err)
	}
	fresh, err := f.store.CreateCard(ctx, f.alice.ID, d.ID, flash.CardTypeBasic, "new one", "x", "")
	if err != nil {
		t.Fatal(err)
	}
	due, err := f.store.CreateCard(ctx, f.alice.ID, d.ID, flash.CardTypeBasic, "due one", "x", "")
	if err != nil {
		t.Fatal(err)
	}
	later, err := f.store.CreateCard(ctx, f.alice.ID, d.ID, flash.CardTypeBasic, "later one", "x", "")
	if err != nil {
		t.Fatal(err)
	}
	// Again puts a card in a learning step minutes away; Easy days away.
	if _, err := f.store.GradeCard(ctx, f.alice.ID, due.ID, flash.RatingAgain, now.Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.GradeCard(ctx, f.alice.ID, later.ID, flash.RatingEasy, now); err != nil {
		t.Fatal(err)
	}

	got, err := f.store.CardStatuses(ctx, f.alice.ID, d.ID, now)
	if err != nil {
		t.Fatal(err)
	}
	if got[fresh.ID] != flash.CardStatusNew || got[due.ID] != flash.CardStatusDue {
		t.Errorf("CardStatuses = %v, want fresh=new and due=due", got)
	}
	if s, ok := got[later.ID]; ok {
		t.Errorf("a card scheduled days out has status %q, want none", s)
	}
}
```

- [ ] **Step 2: Run to verify they fail**

Run: `go test ./internal/apps/flash/ -run 'CardTagsInDeck|CardStatuses' -count=1`
Expected: build failure — methods undefined.

- [ ] **Step 3: Implement**

Append to `internal/apps/flash/tag.go`:

```go
// CardTagsInDeck returns every tagged card in one of userID's decks, mapped
// to its tag names in alphabetical order — one query for the whole cards
// grid rather than a TagsForCard call per card. Untagged cards are absent.
func (st *Store) CardTagsInDeck(ctx context.Context, userID, deckID int64) (map[int64][]string, error) {
	rows, err := st.db.QueryContext(ctx,
		`SELECT ct.card_id, t.name
		   FROM flash_card_tags ct
		   JOIN flash_tags t ON t.id = ct.tag_id
		   JOIN flash_cards c ON c.id = ct.card_id
		  WHERE c.deck_id = ? AND c.user_id = ?
		  ORDER BY t.name ASC`, deckID, userID)
	if err != nil {
		return nil, fmt.Errorf("flash: card tags in deck: %w", err)
	}
	defer func() { _ = rows.Close() }()

	out := map[int64][]string{}
	for rows.Next() {
		var (
			cardID int64
			name   string
		)
		if err := rows.Scan(&cardID, &name); err != nil {
			return nil, fmt.Errorf("flash: card tags in deck: %w", err)
		}
		out[cardID] = append(out[cardID], name)
	}
	return out, rows.Err()
}
```

Append to `internal/apps/flash/card.go`:

```go
// CardStatusNew and CardStatusDue are the corner labels a mini card in the
// cards grid can carry (UI overhaul spec §3).
const (
	CardStatusNew = "new" // userID has never reviewed it
	CardStatusDue = "due" // due for review at or before now
)

// CardStatuses returns the grid label for every card in one of userID's
// decks that has one; a card that is neither new nor due is absent.
func (st *Store) CardStatuses(ctx context.Context, userID, deckID int64, now time.Time) (map[int64]string, error) {
	rows, err := st.db.QueryContext(ctx,
		`SELECT c.id,
		        CASE WHEN s.card_id IS NULL THEN ? WHEN s.due_at <= ? THEN ? ELSE '' END
		   FROM flash_cards c
		   LEFT JOIN flash_card_state s ON s.card_id = c.id AND s.user_id = c.user_id
		  WHERE c.deck_id = ? AND c.user_id = ?`,
		CardStatusNew, formatTime(now), CardStatusDue, deckID, userID)
	if err != nil {
		return nil, fmt.Errorf("flash: card statuses: %w", err)
	}
	defer func() { _ = rows.Close() }()

	out := map[int64]string{}
	for rows.Next() {
		var (
			id     int64
			status string
		)
		if err := rows.Scan(&id, &status); err != nil {
			return nil, fmt.Errorf("flash: card statuses: %w", err)
		}
		if status != "" {
			out[id] = status
		}
	}
	return out, rows.Err()
}
```

- [ ] **Step 4: Run to verify they pass**

Run: `go test ./internal/apps/flash/ -count=1`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/apps/flash/tag.go internal/apps/flash/card.go internal/apps/flash/tag_test.go internal/apps/flash/card_test.go
git commit -m "feat(flash): batch tag and status lookups for the cards grid"
```

---

### Task 3: Card view models

**Files:**
- Create: `internal/apps/flash/card_view.go`
- Test: create `internal/apps/flash/card_view_test.go` (package `flash`)

**Interfaces:**
- Consumes: `renderCardFaces` (Task 1), `mediaURL`, `cardBasePath`, `tagChip`, `normalizeTagName` (existing).
- Produces (U3 and U4 rely on `cardFace` and `newCardFace` exactly):
  ```go
  type cardFace struct {
      ID, DeckID int64
      Color      string        // the deck's colour name
      IsCloze    bool
      Question   template.HTML // from renderCardFaces
      Answer     template.HTML
      Notes      string
      Tags       []tagChip     // Href = this deck's grid filtered by the tag
      ImageURL   string
      AudioURL   string
  }
  func newCardFace(c Card, d Deck, tagNames []string) cardFace
  func cardsURL(deckID int64, q, tag string) string          // "/flash/{id}/cards/?q=..&tag=.."
  func cardURL(deckID, cardID int64, q, tag string) string   // "/flash/{id}/cards/{cardID}?q=..&tag=.."
  func filterCards(cards []Card, tags map[int64][]string, q, tag string) []Card
  type cardGridItem struct { Face cardFace; Status, Href, DeckName string; InPane bool }
  type tagPill struct { Label, Href string; Active bool }
  type cardGridView struct {
      Deck Deck; Query, Tag string
      Pills []tagPill; Items []cardGridItem
      ClearURL, AllDecksTagURL string
      OOB bool
  }
  type openedCardView struct {
      Deck Deck; Face cardFace
      BackURL, PrevURL, NextURL string
      CSRFToken, MediaError string
  }
  ```

- [ ] **Step 1: Write the failing tests**

Create `internal/apps/flash/card_view_test.go`:

```go
// internal/apps/flash/card_view_test.go
package flash

import (
	"strings"
	"testing"
)

func TestCardsURL(t *testing.T) {
	if got := cardsURL(7, "", ""); got != "/flash/7/cards/" {
		t.Errorf("cardsURL no filter = %q", got)
	}
	if got := cardsURL(7, "hola amigo", "a/b"); got != "/flash/7/cards/?q=hola+amigo&tag=a%2Fb" {
		t.Errorf("cardsURL with filter = %q", got)
	}
	if got := cardURL(7, 9, "", "food"); got != "/flash/7/cards/9?tag=food" {
		t.Errorf("cardURL = %q", got)
	}
}

func TestFilterCards(t *testing.T) {
	cards := []Card{
		{ID: 1, Front: "Hola", Back: "hello"},
		{ID: 2, Front: "pan", Back: "bread", Notes: "Also a PAN for cooking"},
		{ID: 3, Front: "agua", Back: "water"},
	}
	tags := map[int64][]string{1: {"greetings"}, 2: {"food"}, 3: {"food"}}

	ids := func(cs []Card) string {
		var b strings.Builder
		for _, c := range cs {
			b.WriteByte(byte('0' + c.ID))
		}
		return b.String()
	}
	for _, tt := range []struct{ q, tag, want string }{
		{"", "", "123"},
		{"hola", "", "1"},    // front, case-insensitive
		{"WATER", "", "3"},   // back
		{"cooking", "", "2"}, // notes
		{"", "food", "23"},
		{"", "FOOD ", "23"}, // tag is normalised like stored tag names
		{"agua", "food", "3"},
		{"zzz", "", ""},
	} {
		if got := ids(filterCards(cards, tags, tt.q, tt.tag)); got != tt.want {
			t.Errorf("filterCards(q=%q, tag=%q) = %q, want %q", tt.q, tt.tag, got, tt.want)
		}
	}
}

func TestNewCardFace(t *testing.T) {
	hash := "abc123"
	c := Card{ID: 4, DeckID: 2, CardType: CardTypeBasic, Front: "hola", Back: "hello", Notes: "informal", ImageHash: &hash}
	d := Deck{ID: 2, Color: "purple"}
	f := newCardFace(c, d, []string{"a/b"})
	if f.ID != 4 || f.DeckID != 2 || f.Color != "purple" || f.IsCloze {
		t.Errorf("face identity = %+v", f)
	}
	if string(f.Question) != "hola" || string(f.Answer) != "hello" || f.Notes != "informal" {
		t.Errorf("face text = %+v", f)
	}
	if f.ImageURL != "/flash/media/abc123" || f.AudioURL != "" {
		t.Errorf("media = %q / %q", f.ImageURL, f.AudioURL)
	}
	if len(f.Tags) != 1 || f.Tags[0].Name != "a/b" || f.Tags[0].Href != "/flash/2/cards/?tag=a%2Fb" {
		t.Errorf("tags = %+v", f.Tags)
	}
}
```

- [ ] **Step 2: Run to verify they fail**

Run: `go test ./internal/apps/flash/ -run 'CardsURL|FilterCards|NewCardFace' -count=1`
Expected: build failure — undefined.

- [ ] **Step 3: Implement `card_view.go`**

```go
// internal/apps/flash/card_view.go
package flash

import (
	"html/template"
	"net/url"
	"strconv"
	"strings"
)

// cardFace is everything any card template draws — the grid's mini card,
// the opened card, the editor preview and the review card all take one
// (UI overhaul spec §1.3). A projection, not the Card itself: Question and
// Answer are already-safe HTML from renderCardFaces.
type cardFace struct {
	ID, DeckID int64
	Color      string // the deck's colour name, for a deck-c-* class
	IsCloze    bool
	Question   template.HTML
	Answer     template.HTML
	Notes      string
	Tags       []tagChip // Href filters this deck's grid by the tag
	ImageURL   string
	AudioURL   string
}

func newCardFace(c Card, d Deck, tagNames []string) cardFace {
	q, a := renderCardFaces(c)
	chips := make([]tagChip, len(tagNames))
	for i, name := range tagNames {
		chips[i] = tagChip{Name: name, Href: cardsURL(d.ID, "", name)}
	}
	return cardFace{
		ID: c.ID, DeckID: d.ID, Color: d.Color, IsCloze: c.CardType == CardTypeCloze,
		Question: q, Answer: a, Notes: c.Notes, Tags: chips,
		ImageURL: mediaURL(c.ImageHash), AudioURL: mediaURL(c.AudioHash),
	}
}

// filterQuery is the ?q=…&tag=… suffix shared by cardsURL and cardURL.
func filterQuery(q, tag string) string {
	v := url.Values{}
	if q != "" {
		v.Set("q", q)
	}
	if tag != "" {
		v.Set("tag", tag)
	}
	if len(v) == 0 {
		return ""
	}
	return "?" + v.Encode()
}

// cardsURL is a deck's cards grid, optionally filtered.
func cardsURL(deckID int64, q, tag string) string {
	return cardBasePath(deckID) + filterQuery(q, tag)
}

// cardURL is one opened card, carrying the grid's filter so previous/next
// and "All cards" keep it.
func cardURL(deckID, cardID int64, q, tag string) string {
	return cardBasePath(deckID) + strconv.FormatInt(cardID, 10) + filterQuery(q, tag)
}

// filterCards keeps the cards matching q (a case-insensitive substring of
// front, back or notes) and tag (one of the card's tags, compared the way
// tag names are stored). Empty q or tag means no filter on that part.
func filterCards(cards []Card, tags map[int64][]string, q, tag string) []Card {
	q = strings.ToLower(strings.TrimSpace(q))
	tag = normalizeTagName(tag)
	var out []Card
	for _, c := range cards {
		if tag != "" && !containsString(tags[c.ID], tag) {
			continue
		}
		if q != "" &&
			!strings.Contains(strings.ToLower(c.Front), q) &&
			!strings.Contains(strings.ToLower(c.Back), q) &&
			!strings.Contains(strings.ToLower(c.Notes), q) {
			continue
		}
		out = append(out, c)
	}
	return out
}

func containsString(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

// cardGridItem is one mini card. InPane makes its link an HTMX swap into
// the deck pane (the cards grid); the cross-deck tag page's items are plain
// links instead, and show DeckName.
type cardGridItem struct {
	Face     cardFace
	Status   string // CardStatusNew, CardStatusDue or ""
	Href     string
	DeckName string
	InPane   bool
}

// tagPill is one filter pill above the grid.
type tagPill struct {
	Label  string
	Href   string
	Active bool
}

// cardGridView is the right pane in "cards" mode.
type cardGridView struct {
	Deck           Deck
	Query, Tag     string
	Pills          []tagPill
	Items          []cardGridItem
	ClearURL       string // the unfiltered grid
	AllDecksTagURL string // the cross-deck tag page, when Tag is set
	OOB            bool   // set on the live-search fragment, for the pills' out-of-band copy
}

// openedCardView is the right pane in "card" mode.
type openedCardView struct {
	Deck       Deck
	Face       cardFace
	BackURL    string // the grid, with the filter kept
	PrevURL    string // "" at the start of the filtered order
	NextURL    string // "" at the end
	CSRFToken  string
	MediaError string
}
```

- [ ] **Step 4: Run to verify they pass**

Run: `go test ./internal/apps/flash/ -count=1`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/apps/flash/card_view.go internal/apps/flash/card_view_test.go
git commit -m "feat(flash): card face, grid and filter view models"
```

---

### Task 4: Cards mode in the right pane

**Files:**
- Create: `internal/apps/flash/templates/card.partial.html`, `internal/apps/flash/templates/cards.partial.html`
- Delete: `internal/apps/flash/templates/cards.html`
- Modify: `internal/apps/flash/handlers_cards.go`, `internal/apps/flash/handlers_decks.go`, `internal/apps/flash/flash.go`, `internal/apps/flash/templates/decks.html`
- Test: `internal/apps/flash/handlers_cards_test.go`, `internal/apps/flash/handlers_tags_test.go`

**Interfaces:**
- Consumes: everything from Tasks 1–3; U1's `buildDeckIndex`, `renderDeckIndex`, `renderDeckDetailWithList`.
- Produces:
  - Pane modes `deckModeCards = "cards"`, `deckModeCard = "card"`, `deckModeCardNew = "card-new"`, `deckModeCardEdit = "card-edit"`.
  - `deckDetailView` fields `Grid cardGridView`, `Opened openedCardView`, `CardForm cardDetailView`.
  - `func (a *App) cardGrid(ctx context.Context, userID int64, deck Deck, q, tag string) (cardGridView, []Card, map[int64][]string, error)`
  - `func (a *App) openedCard(ctx context.Context, r *http.Request, userID int64, deck Deck, c Card, q, tag string) (openedCardView, error)`
  - Route `GET /flash/{deckID}/cards/grid` → template `card-grid-fragment`.
  - Templates: `flash-card` (dict `Face`, `FlipID`, `Hint`), `flash-mini-card` (a `cardGridItem`), `deck-toolbar` (dict `Deck`, `Active`, `CSRFToken`), `deck-detail-cards`, `card-grid` (id `card-grid`), `card-tag-pills` (id `card-tag-pills`), `card-grid-fragment`, `deck-detail-card`, `deck-detail-card-new`, `deck-detail-card-edit`, `card-form-fields`.
  - JS hooks: `.flash-viewer .flash-flip`, `.flash-card-prev`, `.flash-card-next`, `.flash-card-edit`.

- [ ] **Step 1: Write the failing handler tests**

Replace `TestCreateAndViewCard` in `internal/apps/flash/handlers_cards_test.go` and add the rest. The file's imports become:

```go
import (
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/iliafrenkel/on-suite/internal/apps/flash"
	"github.com/iliafrenkel/on-suite/internal/htmlassert"
)
```

```go
func TestCreateAndViewCard(t *testing.T) {
	s := newServer(t)
	deck, err := s.Store.CreateDeck(t.Context(), s.Alice.User.ID, "Spanish", "")
	if err != nil {
		t.Fatal(err)
	}

	s.Submit(t, s.Alice,
		"/flash/"+itoa(deck.ID)+"/cards/new",
		url.Values{"card_type": {flash.CardTypeBasic}, "front": {"hola"}, "back": {"hello"}},
		"/flash/"+itoa(deck.ID)+"/cards/1")

	doc := s.Get(t, s.Alice, "/flash/"+itoa(deck.ID)+"/cards/")
	doc.MustHave("#card-grid")
}
```

Then add these tests (each creates its own cards inline — there is no shared seeding helper):

```go
func TestCardsModeRendersInsideTheHomeLayout(t *testing.T) {
	s := newServer(t)
	deck, err := s.Store.CreateDeck(t.Context(), s.Alice.User.ID, "Spanish", "")
	if err != nil {
		t.Fatal(err)
	}
	for _, front := range []string{"hola", "adiós"} {
		if _, err := s.Store.CreateCard(t.Context(), s.Alice.User.ID, deck.ID, flash.CardTypeBasic, front, "x", ""); err != nil {
			t.Fatal(err)
		}
	}
	doc := s.Get(t, s.Alice, "/flash/"+itoa(deck.ID)+"/cards/")
	doc.MustHave("#deck-list")
	doc.MustHave(`#deck-list a.deck-row-active`)
	tiles := doc.QueryAll("#card-grid .flash-mini-card")
	if len(tiles) != 3 {
		t.Fatalf("grid has %d tiles, want 3 (New card + 2 cards)", len(tiles))
	}
	if href, _ := htmlassert.Attr(tiles[0], "href"); href != "/flash/"+itoa(deck.ID)+"/cards/new" {
		t.Errorf("first tile href = %q, want the New card form", href)
	}
	// htmlassert has no compound selectors (a.class[attr]); select by class,
	// then check the attribute.
	active := doc.MustHave(`.flash-deck-toolbar .toolbar-btn-active`)
	if href, _ := htmlassert.Attr(active, "href"); href != "/flash/"+itoa(deck.ID)+"/cards/" {
		t.Errorf("active toolbar button href = %q, want the Cards button", href)
	}
}

func TestCardGridStatusLabels(t *testing.T) {
	s := newServer(t)
	deck, err := s.Store.CreateDeck(t.Context(), s.Alice.User.ID, "Spanish", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Store.CreateCard(t.Context(), s.Alice.User.ID, deck.ID, flash.CardTypeBasic, "fresh", "x", ""); err != nil {
		t.Fatal(err)
	}
	graded, err := s.Store.CreateCard(t.Context(), s.Alice.User.ID, deck.ID, flash.CardTypeBasic, "graded", "x", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Store.GradeCard(t.Context(), s.Alice.User.ID, graded.ID, flash.RatingAgain, time.Now().UTC().Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	doc := s.Get(t, s.Alice, "/flash/"+itoa(deck.ID)+"/cards/")
	var labels []string
	for _, n := range doc.QueryAll("#card-grid .flash-mini-status") {
		labels = append(labels, htmlassert.Text(n))
	}
	if strings.Join(labels, ",") != "due,new" { // ListCards is newest first
		t.Errorf("status labels = %v, want [due new]", labels)
	}
}

func TestCardsSearchAndTagFilter(t *testing.T) {
	s := newServer(t)
	deck, err := s.Store.CreateDeck(t.Context(), s.Alice.User.ID, "Spanish", "")
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct{ front, tags string }{{"hola", "greetings"}, {"pan", "food"}, {"agua", "food"}} {
		s.Post(t, s.Alice, "/flash/"+itoa(deck.ID)+"/cards/new",
			url.Values{"card_type": {"basic"}, "front": {c.front}, "back": {"x"}, "tags": {c.tags}})
	}
	count := func(path string) int {
		return len(s.Get(t, s.Alice, path).QueryAll("#card-grid .flash-mini-card")) - 1 // minus the New card tile
	}
	base := "/flash/" + itoa(deck.ID) + "/cards/"
	if n := count(base + "?q=HOL"); n != 1 {
		t.Errorf("q=HOL shows %d cards, want 1", n)
	}
	if n := count(base + "?tag=food"); n != 2 {
		t.Errorf("tag=food shows %d cards, want 2", n)
	}
	if n := count(base + "?tag=food&q=agua"); n != 1 {
		t.Errorf("tag=food&q=agua shows %d cards, want 1", n)
	}

	doc := s.Get(t, s.Alice, base+"?tag=food")
	active := doc.MustHave("#card-tag-pills .flash-pill-active")
	if htmlassert.Text(active) != "food" {
		t.Errorf("active pill = %q, want food", htmlassert.Text(active))
	}
	allDecks := doc.MustHave(`#card-tag-pills .flash-all-decks-link`)
	if href, _ := htmlassert.Attr(allDecks, "href"); href != "/flash/tags/food" {
		t.Errorf("all-decks link href = %q", href)
	}

	empty := s.Get(t, s.Alice, base+"?q=zzz")
	empty.MustHave("#card-grid .flash-grid-empty")
}

func TestCardGridFragmentForLiveSearch(t *testing.T) {
	s := newServer(t)
	deck, err := s.Store.CreateDeck(t.Context(), s.Alice.User.ID, "Spanish", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Store.CreateCard(t.Context(), s.Alice.User.ID, deck.ID, flash.CardTypeBasic, "hola", "hello", ""); err != nil {
		t.Fatal(err)
	}
	req := httpGet(t, "/flash/"+itoa(deck.ID)+"/cards/grid?q=hol")
	req.Header.Set("HX-Request", "true")
	rec := s.Do(t, s.Alice, req)
	if rec.Code != 200 {
		t.Fatalf("grid fragment = %d; body: %s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("HX-Replace-Url"); got != "/flash/"+itoa(deck.ID)+"/cards/?q=hol" {
		t.Errorf("HX-Replace-Url = %q", got)
	}
	doc := htmlassert.Parse(t, rec.Body.String())
	doc.MustHave("#card-grid")
	pills := doc.MustHave("#card-tag-pills")
	if _, ok := htmlassert.Attr(pills, "hx-swap-oob"); !ok {
		t.Error("#card-tag-pills in the grid fragment is not marked hx-swap-oob")
	}
	doc.MustNotHave("#deck-list")
}

func TestOpenedCardIsFlippable(t *testing.T) {
	s := newServer(t)
	deck, err := s.Store.CreateDeck(t.Context(), s.Alice.User.ID, "Spanish", "")
	if err != nil {
		t.Fatal(err)
	}
	c, err := s.Store.CreateCard(t.Context(), s.Alice.User.ID, deck.ID, flash.CardTypeBasic, "hola", "hello", "informal")
	if err != nil {
		t.Fatal(err)
	}
	doc := s.Get(t, s.Alice, "/flash/"+itoa(deck.ID)+"/cards/"+itoa(c.ID))
	flip := doc.MustHave(".flash-viewer input.flash-flip")
	id, _ := htmlassert.Attr(flip, "id")
	label := doc.MustHave(".flash-viewer label.flash-card")
	if f, _ := htmlassert.Attr(label, "for"); f != id {
		t.Errorf("card label for=%q, checkbox id=%q; they must match for the flip to work", f, id)
	}
	if got := htmlassert.Text(doc.MustHave(".flash-card-front")); !strings.Contains(got, "hola") {
		t.Errorf("front = %q", got)
	}
	if got := htmlassert.Text(doc.MustHave(".flash-card-back")); !strings.Contains(got, "hello") || !strings.Contains(got, "informal") {
		t.Errorf("back = %q, want the answer and the note", got)
	}
	edit := doc.MustHave(`a.flash-card-edit`)
	if href, _ := htmlassert.Attr(edit, "href"); href != "/flash/"+itoa(deck.ID)+"/cards/edit/"+itoa(c.ID) {
		t.Errorf("Edit card href = %q", href)
	}
}

func TestOpenedClozeCardShowsBlankThenAnswer(t *testing.T) {
	s := newServer(t)
	deck, err := s.Store.CreateDeck(t.Context(), s.Alice.User.ID, "Geography", "")
	if err != nil {
		t.Fatal(err)
	}
	c, err := s.Store.CreateCard(t.Context(), s.Alice.User.ID, deck.ID, flash.CardTypeCloze, "The capital of France is {{c1::Paris}}.", "", "")
	if err != nil {
		t.Fatal(err)
	}
	doc := s.Get(t, s.Alice, "/flash/"+itoa(deck.ID)+"/cards/"+itoa(c.ID))
	front := doc.MustHave(".flash-card-front")
	if strings.Contains(htmlassert.Text(front), "Paris") {
		t.Errorf("front shows the answer: %q", htmlassert.Text(front))
	}
	doc.MustHave(".flash-card-front .flash-blank")
	if got := htmlassert.Text(doc.MustHave(".flash-card-back mark.flash-fill")); got != "Paris" {
		t.Errorf("filled answer = %q, want Paris", got)
	}
}

func TestOpenedCardPrevNextFollowTheFilter(t *testing.T) {
	s := newServer(t)
	deck, err := s.Store.CreateDeck(t.Context(), s.Alice.User.ID, "Spanish", "")
	if err != nil {
		t.Fatal(err)
	}
	// Card ids are 1, 2, 3 in creation order (a fresh database).
	for _, c := range []struct{ front, tags string }{{"one", "food"}, {"two", "other"}, {"three", "food"}} {
		rec := s.Post(t, s.Alice, "/flash/"+itoa(deck.ID)+"/cards/new",
			url.Values{"card_type": {"basic"}, "front": {c.front}, "back": {"x"}, "tags": {c.tags}})
		if rec.Code != 303 {
			t.Fatalf("create: %d", rec.Code)
		}
	}
	// ListCards is newest first: three(3), two(2), one(1). Filtered by food:
	// three, one.
	doc := s.Get(t, s.Alice, "/flash/"+itoa(deck.ID)+"/cards/3?tag=food")
	doc.MustNotHave(".flash-card-prev")
	next := doc.MustHave("a.flash-card-next")
	if href, _ := htmlassert.Attr(next, "href"); href != "/flash/"+itoa(deck.ID)+"/cards/1?tag=food" {
		t.Errorf("next href = %q, want card 1 (skipping untagged card 2)", href)
	}
	back := doc.MustHave("a.flash-card-back-to-grid")
	if href, _ := htmlassert.Attr(back, "href"); href != "/flash/"+itoa(deck.ID)+"/cards/?tag=food" {
		t.Errorf("All cards href = %q, want the filtered grid", href)
	}
}

func TestDeckPaneCardsButtonSwapsThePane(t *testing.T) {
	s := newServer(t)
	deck, err := s.Store.CreateDeck(t.Context(), s.Alice.User.ID, "Spanish", "")
	if err != nil {
		t.Fatal(err)
	}
	doc := s.Get(t, s.Alice, "/flash/"+itoa(deck.ID))
	btn := doc.MustHave(`.flash-deck-toolbar a[href="/flash/` + itoa(deck.ID) + `/cards/"]`)
	if target, _ := htmlassert.Attr(btn, "hx-target"); target != "#deck-detail" {
		t.Errorf("Cards button hx-target = %q, want #deck-detail", target)
	}
}
```

In `internal/apps/flash/handlers_tags_test.go`, replace `TestTagChipHrefEscapesSlash`'s last block:

```go
	doc := s.Get(t, s.Alice, "/flash/"+itoa(deck.ID)+"/cards/1")
	link := doc.MustHave(".flash-tag-links a")
	href, ok := htmlassert.Attr(link, "href")
	if !ok {
		t.Fatal("tag chip link has no href")
	}
	// Tag links now filter this deck's own grid (UI overhaul spec §3); the
	// tag still has to be escaped, as a query value.
	if want := "/flash/" + itoa(deck.ID) + "/cards/?tag=a%2Fb"; href != want {
		t.Errorf("tag chip href = %q, want %q", href, want)
	}
```

and add, after `TestTagFilterViewListsCardsAcrossDecks`:

```go
func TestTagFilterPageShowsMiniCardsWithDeckNames(t *testing.T) {
	s := newServer(t)
	deckA, _ := s.Store.CreateDeck(t.Context(), s.Alice.User.ID, "Alpha", "")
	s.Submit(t, s.Alice, "/flash/"+itoa(deckA.ID)+"/cards/new",
		url.Values{"card_type": {"basic"}, "front": {"a1"}, "back": {"x"}, "tags": {"hard"}},
		"/flash/"+itoa(deckA.ID)+"/cards/1")
	doc := s.Get(t, s.Alice, "/flash/tags/hard")
	card := doc.MustHave(".tag-filter-item a.flash-mini-card")
	if href, _ := htmlassert.Attr(card, "href"); href != "/flash/"+itoa(deckA.ID)+"/cards/1" {
		t.Errorf("mini card href = %q", href)
	}
	if got := htmlassert.Text(doc.MustHave(".tag-filter-item .flash-mini-deck")); got != "Alpha" {
		t.Errorf("deck label = %q, want Alpha", got)
	}
}
```

- [ ] **Step 2: Run to verify they fail**

Run: `go test ./internal/apps/flash/ -run 'Card|Tag' -count=1`
Expected: FAIL / build errors (missing templates, route and fields).

- [ ] **Step 3: Pane modes and titles in `handlers_decks.go`**

Extend the mode constants:

```go
const (
	deckModeView   = "view"
	deckModeNew    = "new"
	deckModeImport = "import"
	// Card modes (UI overhaul U2): a deck's cards live in the same pane.
	deckModeCards    = "cards"
	deckModeCard     = "card"
	deckModeCardNew  = "card-new"
	deckModeCardEdit = "card-edit"
)
```

Add three fields to `deckDetailView` (after `NextLabel`):

```go
	// Card modes. Grid is set for "cards", Opened for "card", CardForm for
	// "card-new" and "card-edit".
	Grid     cardGridView
	Opened   openedCardView
	CardForm cardDetailView
```

Extend `deckPageTitle`:

```go
func deckPageTitle(d deckDetailView) string {
	switch d.Mode {
	case deckModeView:
		return d.Deck.Name
	case "edit":
		return "Edit " + d.Deck.Name
	case deckModeNew:
		return "New deck"
	case deckModeImport:
		return "Import"
	case deckModeCards:
		return "Cards · " + d.Deck.Name
	case deckModeCard:
		return "Card · " + d.Deck.Name
	case deckModeCardNew:
		return "New card · " + d.Deck.Name
	case deckModeCardEdit:
		return "Edit card · " + d.Deck.Name
	default:
		return "Decks"
	}
}
```

- [ ] **Step 4: Rewrite the rendering half of `handlers_cards.go`**

Delete these declarations: `cardListItem`, `cardListFragment`, `cardIndexView`, `cardListItems`, `cardPageTitle`, and `tagChips` (replaced by `newCardFace`'s own chips) and `tagFilterURL` **only if** nothing else uses it — `handlers_tags.go` will (Step 6), so keep `tagFilterURL`.

`viewCardDetail` used `tagChips`; change it to keep only what the pane translation needs:

```go
func (a *App) viewCardDetail(r *http.Request, userID int64, d Deck, c Card) cardDetailView {
	return cardDetailView{
		Mode: cardModeView, Deck: d, Card: c, CSRFToken: web.CSRFToken(r.Context()),
		ImageMediaURL: mediaURL(c.ImageHash), AudioMediaURL: mediaURL(c.AudioHash),
	}
}
```

(and remove the now-unused `Tags []tagChip` field from `cardDetailView`.)

Add, replacing the old `cardIndex`, `renderCardIndex` and `renderCardDetailWithList`:

```go
// cardFilterFromQuery reads the grid's ?q= and ?tag= parameters.
func cardFilterFromQuery(r *http.Request) (q, tag string) {
	v := r.URL.Query()
	return strings.TrimSpace(v.Get("q")), normalizeTagName(v.Get("tag"))
}

// cardGrid builds the cards pane for one deck, filtered by q and tag. It
// also returns the filtered cards and the deck's tag map, which openedCard
// needs for previous/next and the card's own tags.
func (a *App) cardGrid(ctx context.Context, userID int64, deck Deck, q, tag string) (cardGridView, []Card, map[int64][]string, error) {
	cards, err := a.store.ListCards(ctx, userID, deck.ID)
	if err != nil {
		return cardGridView{}, nil, nil, err
	}
	tags, err := a.store.CardTagsInDeck(ctx, userID, deck.ID)
	if err != nil {
		return cardGridView{}, nil, nil, err
	}
	statuses, err := a.store.CardStatuses(ctx, userID, deck.ID, a.store.now())
	if err != nil {
		return cardGridView{}, nil, nil, err
	}
	filtered := filterCards(cards, tags, q, tag)

	view := cardGridView{Deck: deck, Query: q, Tag: tag, ClearURL: cardsURL(deck.ID, "", "")}
	for _, c := range filtered {
		view.Items = append(view.Items, cardGridItem{
			Face:   newCardFace(c, deck, tags[c.ID]),
			Status: statuses[c.ID],
			Href:   cardURL(deck.ID, c.ID, q, tag),
			InPane: true,
		})
	}

	seen := map[string]bool{}
	var names []string
	for _, list := range tags {
		for _, name := range list {
			if !seen[name] {
				seen[name] = true
				names = append(names, name)
			}
		}
	}
	sort.Strings(names)
	if len(names) > 0 {
		view.Pills = append(view.Pills, tagPill{Label: "All", Href: cardsURL(deck.ID, q, ""), Active: tag == ""})
		for _, name := range names {
			view.Pills = append(view.Pills, tagPill{Label: name, Href: cardsURL(deck.ID, q, name), Active: tag == name})
		}
	}
	if tag != "" {
		view.AllDecksTagURL = tagFilterURL(tag)
	}
	return view, filtered, tags, nil
}

// openedCard builds the pane for one card opened from the grid. Previous
// and next step through the same filtered order the grid showed; if c is
// not in it (the filter changed, or no filter applies to it), both are
// empty.
func (a *App) openedCard(ctx context.Context, r *http.Request, userID int64, deck Deck, c Card, q, tag string) (openedCardView, error) {
	_, filtered, tags, err := a.cardGrid(ctx, userID, deck, q, tag)
	if err != nil {
		return openedCardView{}, err
	}
	view := openedCardView{
		Deck:      deck,
		Face:      newCardFace(c, deck, tags[c.ID]),
		BackURL:   cardsURL(deck.ID, q, tag),
		CSRFToken: web.CSRFToken(r.Context()),
	}
	for i, fc := range filtered {
		if fc.ID != c.ID {
			continue
		}
		if i > 0 {
			view.PrevURL = cardURL(deck.ID, filtered[i-1].ID, q, tag)
		}
		if i < len(filtered)-1 {
			view.NextURL = cardURL(deck.ID, filtered[i+1].ID, q, tag)
		}
	}
	return view, nil
}

// cardPaneDetail translates a card handler's cardDetailView into the deck
// pane's view: a form stays a form, a viewed card becomes the opened card,
// and anything else (after a delete) is the grid.
func (a *App) cardPaneDetail(r *http.Request, userID int64, deck Deck, cd cardDetailView) (deckDetailView, error) {
	detail := deckDetailView{Deck: deck, CSRFToken: web.CSRFToken(r.Context())}
	switch cd.Mode {
	case cardModeNew:
		detail.Mode, detail.CardForm = deckModeCardNew, cd
	case cardModeEdit:
		detail.Mode, detail.CardForm = deckModeCardEdit, cd
	case cardModeView:
		opened, err := a.openedCard(r.Context(), r, userID, deck, cd.Card, "", "")
		if err != nil {
			return deckDetailView{}, err
		}
		opened.MediaError = cd.MediaError
		detail.Mode, detail.Opened = deckModeCard, opened
	default:
		grid, _, _, err := a.cardGrid(r.Context(), userID, deck, "", "")
		if err != nil {
			return deckDetailView{}, err
		}
		detail.Mode, detail.Grid = deckModeCards, grid
	}
	return detail, nil
}

// renderCardIndex and renderCardDetailWithList keep the names every card
// handler already calls, but now draw the home layout (UI overhaul U2):
// a deck's cards live in the deck pane, not on a page of their own.
func (a *App) renderCardIndex(w http.ResponseWriter, r *http.Request, userID int64, deck Deck, status int, cd cardDetailView) {
	detail, err := a.cardPaneDetail(r, userID, deck, cd)
	if err != nil {
		a.deps.Errors.Internal(w, r, err)
		return
	}
	a.renderDeckIndex(w, r, userID, status, detail)
}

func (a *App) renderCardDetailWithList(w http.ResponseWriter, r *http.Request, userID int64, deck Deck, status int, cd cardDetailView) {
	detail, err := a.cardPaneDetail(r, userID, deck, cd)
	if err != nil {
		a.deps.Errors.Internal(w, r, err)
		return
	}
	a.renderDeckDetailWithList(w, r, userID, status, detail)
}

// cardIndex backs GET /{deckID}/cards/ (the grid) and GET
// /{deckID}/cards/{cardID} (one opened card), both keeping ?q=/?tag=.
func (a *App) cardIndex(w http.ResponseWriter, r *http.Request) {
	userID, ok := a.userID(w, r)
	if !ok {
		return
	}
	deck, ok := a.cardDeck(w, r, userID)
	if !ok {
		return
	}
	q, tag := cardFilterFromQuery(r)
	detail := deckDetailView{Deck: deck, CSRFToken: web.CSRFToken(r.Context())}

	if r.PathValue("cardID") == "" {
		grid, _, _, err := a.cardGrid(r.Context(), userID, deck, q, tag)
		if err != nil {
			a.deps.Errors.Internal(w, r, err)
			return
		}
		detail.Mode, detail.Grid = deckModeCards, grid
		a.renderDeckIndex(w, r, userID, http.StatusOK, detail)
		return
	}

	id, ok := a.cardIDFromPath(w, r)
	if !ok {
		return
	}
	c, err := a.store.CardByID(r.Context(), userID, deck.ID, id)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	opened, err := a.openedCard(r.Context(), r, userID, deck, c, q, tag)
	if err != nil {
		a.deps.Errors.Internal(w, r, err)
		return
	}
	detail.Mode, detail.Opened = deckModeCard, opened
	a.renderDeckIndex(w, r, userID, http.StatusOK, detail)
}

// cardGridFragment backs GET /{deckID}/cards/grid: just the grid tiles plus
// an out-of-band copy of the tag pills, for the search box's live filter.
// The URL bar gets the canonical grid URL (HX-Replace-Url), never this
// route's own.
func (a *App) cardGridFragment(w http.ResponseWriter, r *http.Request) {
	userID, ok := a.userID(w, r)
	if !ok {
		return
	}
	deck, ok := a.cardDeck(w, r, userID)
	if !ok {
		return
	}
	q, tag := cardFilterFromQuery(r)
	grid, _, _, err := a.cardGrid(r.Context(), userID, deck, q, tag)
	if err != nil {
		a.deps.Errors.Internal(w, r, err)
		return
	}
	grid.OOB = true
	w.Header().Set("HX-Replace-Url", cardsURL(deck.ID, q, tag))
	if err := a.deps.Render.Fragment(w, http.StatusOK, "flash/decks", "card-grid-fragment", grid); err != nil {
		a.deps.Errors.Internal(w, r, err)
	}
}
```

Update imports in `handlers_cards.go`: add `"sort"`; `"net/url"` is still used by nothing here after removing `tagFilterURL`? — `tagFilterURL` stays in this file and uses `url.PathEscape`, so keep `net/url`. Remove `render` if it is now unused (it was only used by `cardIndexView`).

In `createCard`, `updateCard`, `deleteCard` and `handlers_media.go`'s `uploadCardMedia`, **no change**: they already call `renderCardIndex`/`renderCardDetailWithList` with a `cardDetailView`, which `cardPaneDetail` now translates.

- [ ] **Step 5: Register the grid route**

In `internal/apps/flash/flash.go`, right after `r.HandleFunc("GET /{deckID}/cards/new", a.newCardForm)`, add:

```go
	// "grid" is a literal at the position where GET /{deckID}/cards/{cardID}
	// has a wildcard — the same non-ambiguity shape as "new" just above.
	r.HandleFunc("GET /{deckID}/cards/grid", a.cardGridFragment)
```

- [ ] **Step 6: Tag page items become mini cards**

Replace the body of `internal/apps/flash/handlers_tags.go` below the imports with:

```go
// tagFilterView is the cross-deck tag page: every card carrying one tag, as
// mini cards striped in their own deck's colour.
type tagFilterView struct {
	TagName string
	Items   []cardGridItem
}

// tagFilter renders the cross-deck filter page for one tag.
func (a *App) tagFilter(w http.ResponseWriter, r *http.Request) {
	userID, ok := a.userID(w, r)
	if !ok {
		return
	}
	tagName := r.PathValue("tagName")
	cards, err := a.store.CardsByTag(r.Context(), userID, tagName)
	if err != nil {
		a.deps.Errors.Internal(w, r, err)
		return
	}

	decks := map[int64]Deck{}
	items := make([]cardGridItem, 0, len(cards))
	for _, c := range cards {
		d, ok := decks[c.DeckID]
		if !ok {
			if d, err = a.store.DeckByID(r.Context(), userID, c.DeckID); err != nil {
				a.deps.Errors.Internal(w, r, err)
				return
			}
			decks[c.DeckID] = d
		}
		items = append(items, cardGridItem{
			Face:     newCardFace(c, d, nil),
			Href:     cardURL(d.ID, c.ID, "", ""),
			DeckName: d.Name,
		})
	}

	page := a.deps.Page(r, "Tag: "+tagName)
	page.Data = tagFilterView{TagName: tagName, Items: items}
	a.render(w, r, http.StatusOK, "flash/tag-filter", page)
}
```

(Keep the file's existing doc comments above `tagFilter` if they still read true; delete the old `tagFilterItem` type.)

- [ ] **Step 7: Create `card.partial.html`**

```html
{{/* internal/apps/flash/templates/card.partial.html — how a card is drawn
     (UI overhaul spec §1.3). Parsed into every Flash page's template set. */}}

{{/* flash-card is the large, flippable card. Pass a dict:
       Face   — a cardFace
       FlipID — a unique id for the flip checkbox
       Hint   — optional line under the question ("Tap the card to flip it")
     It emits three siblings: the visually-hidden checkbox, the card (the
     checkbox's <label>), and the extras. The flip is pure CSS on
     .flash-flip:checked, so a caller may put more `.flash-flip:checked ~`
     siblings after this template in the same parent (U4's grade buttons).
     Only non-interactive content goes inside the <label>; links and audio
     go in .flash-card-extras, shown once the card is flipped. */}}
{{define "flash-card"}}<input type="checkbox" class="visually-hidden flash-flip" id="{{.FlipID}}" aria-label="Show the answer">
<label class="flash-card deck-c-{{.Face.Color}}" for="{{.FlipID}}">
	<span class="flash-card-inner">
		<span class="flash-card-face flash-card-front">
			<span class="flash-card-text">{{.Face.Question}}</span>
			{{with .Face.ImageURL}}<img class="flash-card-image" src="{{.}}" alt="">{{end}}
			{{with $.Hint}}<span class="flash-card-hint">{{.}}</span>{{end}}
		</span>
		<span class="flash-card-face flash-card-back">
			<span class="flash-card-text">{{.Face.Answer}}</span>
			{{with .Face.Notes}}<span class="flash-card-notes">{{.}}</span>{{end}}
			{{with .Face.Tags}}<span class="flash-card-tags">{{range .}}<span class="flash-pill">{{.Name}}</span>{{end}}</span>{{end}}
		</span>
	</span>
</label>
<div class="flash-card-extras">
	{{with .Face.AudioURL}}<audio controls src="{{.}}"></audio>{{end}}
	{{with .Face.Tags}}<p class="flash-tag-links">{{range .}}<a class="flash-pill" href="{{.Href}}" hx-get="{{.Href}}" hx-target="#deck-detail" hx-push-url="true">{{.Name}}</a>{{end}}</p>{{end}}
</div>{{end}}

{{/* flash-mini-card is one grid tile: the question side only, plus a
     status corner label. Pass a cardGridItem. InPane swaps it into the deck
     pane; otherwise (the cross-deck tag page) it is a plain link and shows
     its deck's name. */}}
{{define "flash-mini-card"}}<a class="flash-mini-card deck-c-{{.Face.Color}}" href="{{.Href}}"{{if .InPane}} hx-get="{{.Href}}" hx-target="#deck-detail" hx-push-url="true"{{end}}>
	<span class="flash-mini-text">{{.Face.Question}}</span>
	{{with .Status}}<span class="flash-mini-status">{{.}}</span>{{end}}
	{{with .DeckName}}<span class="flash-mini-deck">{{.}}</span>{{end}}
</a>{{end}}
```

- [ ] **Step 8: Create `cards.partial.html`**

This holds the deck toolbar (shared with the deck view), the cards pane, and the card forms moved from the deleted `cards.html` (U3 redesigns the forms; here they only change target and structure).

```html
{{/* internal/apps/flash/templates/cards.partial.html — a deck's cards in
     the home screen's right pane (UI overhaul spec §3). */}}

{{/* deck-toolbar is the deck pane's toolbar, shared by the deck view and
     cards mode. Pass a dict: Deck, Active ("cards" highlights Cards; ""
     for none), CSRFToken. */}}
{{define "deck-toolbar"}}
<div class="notes-toolbar flash-deck-toolbar">
	{{template "flash-pane-back"}}
	<div class="notes-toolbar-actions">
		<a class="toolbar-btn{{if eq .Active "cards"}} toolbar-btn-active{{end}}" href="/flash/{{.Deck.ID}}/cards/" hx-get="/flash/{{.Deck.ID}}/cards/" hx-target="#deck-detail" hx-push-url="true">{{ticon "cards"}}Cards</a>
		<a class="toolbar-btn" href="/flash/edit/{{.Deck.ID}}" hx-get="/flash/edit/{{.Deck.ID}}" hx-target="#deck-detail" hx-push-url="true">{{ticon "edit"}}Edit</a>
		<form method="post" action="/flash/{{.Deck.ID}}/delete" hx-post="/flash/{{.Deck.ID}}/delete" hx-target="#deck-detail" hx-swap="innerHTML"
		      data-confirm="Delete “{{.Deck.Name}}” and all its cards? This can't be undone.">
			<input type="hidden" name="{{csrfField}}" value="{{.CSRFToken}}">
			<button type="submit" class="toolbar-btn danger">{{ticon "trash"}}Delete</button>
		</form>
	</div>
</div>
{{end}}

{{define "deck-detail-cards"}}
<div class="flash-cards-pane deck-c-{{.Deck.Color}}" id="deck-detail-cards">
	{{template "deck-toolbar" (dict "Deck" .Deck "Active" "cards" "CSRFToken" .CSRFToken)}}
	<h1 class="flash-pane-title">{{.Deck.Name}}</h1>
	{{/* Without JS this is a normal GET form to the grid itself. With JS,
	     typing live-filters just #card-grid through the fragment route. The
	     hidden tag input keeps the current tag pill while searching. */}}
	<form class="flash-card-filter" method="get" action="/flash/{{.Deck.ID}}/cards/" role="search">
		<input type="hidden" name="tag" value="{{.Grid.Tag}}">
		<input type="search" name="q" value="{{.Grid.Query}}" placeholder="Search cards…" aria-label="Search cards"
		       hx-get="/flash/{{.Deck.ID}}/cards/grid" hx-include="closest form"
		       hx-target="#card-grid" hx-swap="outerHTML"
		       hx-trigger="input changed delay:300ms, search">
	</form>
	{{template "card-tag-pills" .Grid}}
	{{template "card-grid" .Grid}}
</div>
{{end}}

{{define "card-tag-pills"}}<div id="card-tag-pills" class="flash-tag-pills"{{if .OOB}} hx-swap-oob="true"{{end}}>
	{{range .Pills}}<a class="flash-pill{{if .Active}} flash-pill-active{{end}}" href="{{.Href}}" hx-get="{{.Href}}" hx-target="#deck-detail" hx-push-url="true"{{if .Active}} aria-current="true"{{end}}>{{.Label}}</a>{{end}}
	{{with .AllDecksTagURL}}<a class="flash-all-decks-link" href="{{.}}">Show “{{$.Tag}}” in all decks</a>{{end}}
</div>{{end}}

{{define "card-grid"}}<div id="card-grid" class="flash-card-grid">
	<a class="flash-mini-card flash-mini-new" href="/flash/{{.Deck.ID}}/cards/new" hx-get="/flash/{{.Deck.ID}}/cards/new" hx-target="#deck-detail" hx-push-url="true">{{ticon "plus"}}New card</a>
	{{range .Items}}{{template "flash-mini-card" .}}{{end}}
	{{if and (not .Items) (or .Query .Tag)}}<p class="flash-grid-empty dim">No cards match. <a href="{{.ClearURL}}" hx-get="{{.ClearURL}}" hx-target="#deck-detail" hx-push-url="true">Clear filters</a></p>{{end}}
</div>{{end}}

{{/* card-grid-fragment is GET /{deckID}/cards/grid's whole response: the
     grid (swapped by the search box's hx-target) and the pills out of
     band, so their links carry the new search text. */}}
{{define "card-grid-fragment"}}{{template "card-grid" .}}{{template "card-tag-pills" .}}{{end}}

{{define "deck-detail-card"}}
<div class="flash-opened-card deck-c-{{.Deck.Color}}" id="deck-detail-card">
	<div class="notes-toolbar flash-deck-toolbar">
		<a class="toolbar-btn flash-card-back-to-grid" href="{{.Opened.BackURL}}" hx-get="{{.Opened.BackURL}}" hx-target="#deck-detail" hx-push-url="true">{{ticon "arrow-left"}}All cards</a>
		<div class="notes-toolbar-actions">
			<a class="toolbar-btn flash-card-edit" href="/flash/{{.Deck.ID}}/cards/edit/{{.Opened.Face.ID}}" hx-get="/flash/{{.Deck.ID}}/cards/edit/{{.Opened.Face.ID}}" hx-target="#deck-detail" hx-push-url="true">{{ticon "edit"}}Edit card</a>
			<form method="post" action="/flash/{{.Deck.ID}}/cards/{{.Opened.Face.ID}}/delete" hx-post="/flash/{{.Deck.ID}}/cards/{{.Opened.Face.ID}}/delete" hx-target="#deck-detail" hx-swap="innerHTML"
			      data-confirm="Delete this card? This can't be undone.">
				<input type="hidden" name="{{csrfField}}" value="{{.Opened.CSRFToken}}">
				<button type="submit" class="toolbar-btn danger">{{ticon "trash"}}Delete card</button>
			</form>
		</div>
	</div>
	{{with .Opened.MediaError}}<div class="notice notice-error" role="alert">{{.}}</div>{{end}}
	<div class="flash-viewer">
		{{if .Opened.PrevURL}}<a class="flash-nav-btn flash-card-prev" href="{{.Opened.PrevURL}}" hx-get="{{.Opened.PrevURL}}" hx-target="#deck-detail" hx-push-url="true" aria-label="Previous card">‹</a>{{else}}<span class="flash-nav-btn flash-nav-btn-empty" aria-hidden="true"></span>{{end}}
		<div class="flash-viewer-card">
			{{template "flash-card" (dict "Face" .Opened.Face "FlipID" (printf "flip-%d" .Opened.Face.ID) "Hint" "Tap the card to flip it")}}
		</div>
		{{if .Opened.NextURL}}<a class="flash-nav-btn flash-card-next" href="{{.Opened.NextURL}}" hx-get="{{.Opened.NextURL}}" hx-target="#deck-detail" hx-push-url="true" aria-label="Next card">›</a>{{else}}<span class="flash-nav-btn flash-nav-btn-empty" aria-hidden="true"></span>{{end}}
	</div>
	<p class="faint flash-viewer-keys">Space flips the card · ← → move between cards · E edits</p>

	{{/* F4's media form, kept until U3 merges it into the card editor. */}}
	<details class="flash-media-details">
		<summary>Picture and sound</summary>
		<form class="stack" id="card-media-form" method="post" action="/flash/{{.Deck.ID}}/cards/{{.Opened.Face.ID}}/media" hx-post="/flash/{{.Deck.ID}}/cards/{{.Opened.Face.ID}}/media" hx-target="#deck-detail" hx-swap="innerHTML" enctype="multipart/form-data">
			<input type="hidden" name="{{csrfField}}" value="{{.Opened.CSRFToken}}">
			<div class="field">
				<label for="card-image-{{.Opened.Face.ID}}">Image</label>
				<input id="card-image-{{.Opened.Face.ID}}" type="file" name="image" accept="image/*">
				{{if .Opened.Face.ImageURL}}<label><input type="checkbox" name="remove_image" value="1"> Remove image</label>{{end}}
			</div>
			<div class="field">
				<label for="card-audio-{{.Opened.Face.ID}}">Audio</label>
				<input id="card-audio-{{.Opened.Face.ID}}" type="file" name="audio" accept="audio/*">
				{{if .Opened.Face.AudioURL}}<label><input type="checkbox" name="remove_audio" value="1"> Remove audio</label>{{end}}
			</div>
			<button type="submit" class="button">Update media</button>
		</form>
	</details>
</div>
{{end}}

{{/* The card forms below are F1's, moved here unchanged except that they
     now live in the deck pane (hx-target #deck-detail). U3 replaces them
     with the card-face editor. Each is passed the deckDetailView; the
     form's own values are in .CardForm. */}}
{{define "card-form-fields"}}
<div class="field">
	<label for="card-type-{{.IDSuffix}}">Type</label>
	<select id="card-type-{{.IDSuffix}}" name="card_type">
		<option value="basic"{{if eq .CardTypeValue "basic"}} selected{{end}}>Question and answer</option>
		<option value="cloze"{{if eq .CardTypeValue "cloze"}} selected{{end}}>Fill in the blank</option>
	</select>
</div>
<div class="field">
	<label for="card-front-{{.IDSuffix}}">Front</label>
	<textarea id="card-front-{{.IDSuffix}}" name="front" rows="3" required>{{.FrontValue}}</textarea>
</div>
<div class="field">
	<label for="card-back-{{.IDSuffix}}">Back (leave empty for fill in the blank)</label>
	<textarea id="card-back-{{.IDSuffix}}" name="back" rows="3">{{.BackValue}}</textarea>
</div>
<div class="field">
	<label for="card-notes-{{.IDSuffix}}">Extra note (shown after the answer)</label>
	<textarea id="card-notes-{{.IDSuffix}}" name="notes" rows="3">{{.NotesValue}}</textarea>
</div>
<div class="field">
	<label for="card-tags-{{.IDSuffix}}">Tags (comma-separated)</label>
	<input id="card-tags-{{.IDSuffix}}" type="text" name="tags" value="{{.TagsValue}}">
</div>
{{end}}

{{define "deck-detail-card-new"}}
{{with .CardForm}}
<form class="stack flash-card-form" id="card-detail-new" method="post" action="/flash/{{.Deck.ID}}/cards/new" hx-post="/flash/{{.Deck.ID}}/cards/new" hx-target="#deck-detail" hx-swap="innerHTML">
	<input type="hidden" name="{{csrfField}}" value="{{.CSRFToken}}">
	<h1>New card</h1>
	{{with .Error}}<div class="notice notice-error" role="alert">{{.}}</div>{{end}}
	{{template "card-form-fields" (dict "IDSuffix" "new" "CardTypeValue" .CardTypeValue "FrontValue" .FrontValue "BackValue" .BackValue "NotesValue" .NotesValue "TagsValue" .TagsValue)}}
	<div class="row">
		<button type="submit" class="primary">Save card</button>
		<a class="toolbar-btn" href="/flash/{{.Deck.ID}}/cards/" hx-get="/flash/{{.Deck.ID}}/cards/" hx-target="#deck-detail" hx-push-url="true">Cancel</a>
	</div>
</form>
{{end}}
{{end}}

{{define "deck-detail-card-edit"}}
{{with .CardForm}}
<form class="stack flash-card-form" id="card-detail-edit" method="post" action="/flash/{{.Deck.ID}}/cards/{{.Card.ID}}" hx-post="/flash/{{.Deck.ID}}/cards/{{.Card.ID}}" hx-target="#deck-detail" hx-swap="innerHTML">
	<input type="hidden" name="{{csrfField}}" value="{{.CSRFToken}}">
	<h1>Edit card</h1>
	{{with .Error}}<div class="notice notice-error" role="alert">{{.}}</div>{{end}}
	{{template "card-form-fields" (dict "IDSuffix" .Card.ID "CardTypeValue" .CardTypeValue "FrontValue" .FrontValue "BackValue" .BackValue "NotesValue" .NotesValue "TagsValue" .TagsValue)}}
	<div class="row">
		<button type="submit" class="primary">Save card</button>
		<a class="toolbar-btn" href="/flash/{{.Deck.ID}}/cards/{{.Card.ID}}" hx-get="/flash/{{.Deck.ID}}/cards/{{.Card.ID}}" hx-target="#deck-detail" hx-push-url="true">Cancel</a>
	</div>
</form>
{{end}}
{{end}}
```

Then delete the old page: `git rm internal/apps/flash/templates/cards.html`.

- [ ] **Step 9: Wire the modes into `decks.html`**

Replace the `deck-detail-body` definition with:

```html
{{define "deck-detail-body"}}
{{if eq .Mode "view"}}{{template "deck-detail-view" .}}
{{else if eq .Mode "edit"}}{{template "deck-detail-edit" .}}
{{else if eq .Mode "new"}}{{template "deck-detail-new" .}}
{{else if eq .Mode "import"}}{{template "deck-detail-import" .}}
{{else if eq .Mode "cards"}}{{template "deck-detail-cards" .}}
{{else if eq .Mode "card"}}{{template "deck-detail-card" .}}
{{else if eq .Mode "card-new"}}{{template "deck-detail-card-new" .}}
{{else if eq .Mode "card-edit"}}{{template "deck-detail-card-edit" .}}
{{else}}{{template "deck-detail-empty" .}}
{{end}}
{{end}}
```

In `deck-detail-view`, replace the whole `<div class="notes-toolbar flash-deck-toolbar"> … </div>` block (the one with Cards/Edit/Delete) with:

```html
	{{template "deck-toolbar" (dict "Deck" .Deck "Active" "" "CSRFToken" .CSRFToken)}}
```

- [ ] **Step 10: Restyle the tag page**

Replace `internal/apps/flash/templates/tag-filter.html` with:

```html
{{define "head"}}
<script src="/flash/flash.js" defer></script>
{{end}}

{{define "content"}}
<div class="stack">
	<div class="notes-toolbar flash-deck-toolbar">
		<a class="toolbar-btn" href="/flash/">{{ticon "arrow-left"}}Decks</a>
	</div>
	<h1>Cards tagged <span class="flash-pill flash-pill-active">{{.Data.TagName}}</span></h1>
	{{if .Data.Items}}
	<ul class="flash-card-grid tag-filter-list">
		{{range .Data.Items}}<li class="tag-filter-item">{{template "flash-mini-card" .}}</li>{{end}}
	</ul>
	{{else}}
	<p class="dim">No cards carry this tag.</p>
	{{end}}
</div>
{{end}}
```

- [ ] **Step 11: Run all Flash tests**

Run: `go test ./internal/apps/flash/ -count=1`
Expected: PASS. Existing tests that used to hit `cards.html` (`TestCreateCardValidation`, the overlong-tag tests, the media upload tests) must pass unchanged: a validation failure still renders `.notice-error` (status 400 for a plain POST, 200 over HTMX).

- [ ] **Step 12: Commit**

```bash
git add -A internal/apps/flash
git commit -m "feat(flash): cards grid and flippable card inside the deck pane"
```

---

### Task 5: Card viewer keyboard shortcuts

**Files:**
- Modify: `internal/apps/flash/static/flash.js`

**Interfaces:**
- Consumes: `.flash-viewer .flash-flip`, `.flash-card-prev`, `.flash-card-next`, `.flash-card-edit` (Task 4); existing review hooks `.flash-review-answer`, `.flash-grade-*`, `.flash-undo-btn`, `.flash-reveal-btn`.
- Produces: `press(selector)` returning whether it clicked something, and a keydown handler that only calls `preventDefault` for keys it handled (so arrow keys still scroll pages without a card viewer). U4 extends this same handler.

- [ ] **Step 1: Replace everything from `(function () {` to the end of `flash.js`**

(Keep the header comment from U1; add one line to it: `// Card viewer shortcuts (U2): Space flips, ← → previous/next, E edits.`)

```js
(function () {
	"use strict";

	function isTyping(el) {
		if (!el) return false;
		var tag = el.tagName;
		return tag === "INPUT" || tag === "TEXTAREA" || el.isContentEditable;
	}

	// press clicks the first element matching selector and reports whether
	// there was one, so a shortcut only swallows its key where it applies.
	function press(selector) {
		var el = document.querySelector(selector);
		if (!el) return false;
		el.click();
		return true;
	}

	function reveal() {
		var answer = document.querySelector(".flash-review-answer");
		if (!answer) return false;
		answer.hidden = false;
		return true;
	}

	// toggleFlip turns the opened card over. The flip itself is CSS on the
	// checkbox's :checked state; this only changes the state.
	function toggleFlip() {
		var flip = document.querySelector(".flash-viewer .flash-flip");
		if (!flip) return false;
		flip.checked = !flip.checked;
		return true;
	}

	document.addEventListener("keydown", function (e) {
		if (e.metaKey || e.ctrlKey || e.altKey) return;
		// The flip checkbox is an <input>, but Space on it should still flip
		// — natively, which is why this returns before isTyping and before
		// our own Space handling (doing both would flip twice).
		if (e.target.classList && e.target.classList.contains("flash-flip")) return;
		if (isTyping(e.target)) return;

		var handled = false;
		switch (e.key) {
			case " ":
				handled = toggleFlip() || reveal();
				break;
			case "1":
				handled = press(".flash-grade-again");
				break;
			case "2":
				handled = press(".flash-grade-hard");
				break;
			case "3":
				handled = press(".flash-grade-good");
				break;
			case "4":
				handled = press(".flash-grade-easy");
				break;
			case "u":
			case "U":
				handled = press(".flash-undo-btn");
				break;
			case "ArrowLeft":
				handled = press(".flash-card-prev");
				break;
			case "ArrowRight":
				handled = press(".flash-card-next");
				break;
			case "e":
			case "E":
				handled = press(".flash-card-edit");
				break;
		}
		if (handled) e.preventDefault();
	});

	document.addEventListener("click", function (e) {
		if (e.target.closest && e.target.closest(".flash-reveal-btn")) reveal();
	});
})();
```

- [ ] **Step 2: Verify the script is still served**

Run: `go test ./internal/apps/flash/ -run TestFlashScriptIsServed -count=1`
Expected: PASS. (Behaviour is checked by hand in Task 7.)

- [ ] **Step 3: Commit**

```bash
git add internal/apps/flash/static/flash.js
git commit -m "feat(flash): keyboard shortcuts for the card viewer"
```

---

### Task 6: Card CSS

**Files:**
- Modify: `internal/ui/static/app.css`

- [ ] **Step 1: Append the card styles**

Append after U1's Flash block (end of file):

```css
/* ---- ON Flash: cards (UI overhaul U2) ---------------------------------- */

:root { --flash-card-bg: #ffffff; }
:root[data-theme="dark"] { --flash-card-bg: var(--c-bg-inset); }

.flash-pane-title { margin: 0 0 var(--s-3); font-size: var(--fs-xl); }

/* Search and pills above the grid. */
.flash-card-filter { margin-bottom: var(--s-3); }
.flash-card-filter input[type="search"] {
	width: 100%;
	max-width: 24rem;
	padding: var(--s-2) var(--s-3);
	border: 1px solid var(--c-border-firm);
	border-radius: var(--radius);
	background: var(--c-bg);
	color: var(--c-text);
	font: inherit;
}
.flash-tag-pills { display: flex; flex-wrap: wrap; align-items: center; gap: var(--s-2); margin-bottom: var(--s-3); }
.flash-tag-pills:empty { display: none; }

.flash-pill {
	display: inline-flex;
	align-items: center;
	min-height: 2rem;
	padding: 0 var(--s-3);
	border-radius: 999px;
	border: 1px solid transparent;
	background: var(--c-bg-subtle);
	color: var(--c-text-dim);
	font-size: var(--fs-sm);
	text-decoration: none;
}
a.flash-pill:hover { color: var(--c-text); border-color: var(--c-border-firm); }
.flash-pill-active {
	background: var(--c-accent-bg);
	color: var(--c-accent);
	border-color: var(--c-accent);
}
.flash-all-decks-link { font-size: var(--fs-sm); margin-left: var(--s-2); }

/* The grid of mini cards. */
.flash-card-grid {
	display: grid;
	grid-template-columns: repeat(auto-fill, minmax(9rem, 1fr));
	gap: var(--s-3);
	margin: 0;
	padding: 0;
	list-style: none;
}
.flash-mini-card {
	position: relative;
	display: flex;
	align-items: center;
	justify-content: center;
	gap: var(--s-1);
	min-height: 5.5rem;
	padding: var(--s-4) var(--s-2) var(--s-3);
	overflow: hidden;
	border-radius: 10px;
	border: 1px solid var(--c-border);
	background: var(--flash-card-bg);
	color: var(--c-text);
	text-align: center;
	text-decoration: none;
	font-size: var(--fs-sm);
	transition: transform 0.12s, box-shadow 0.12s;
}
.flash-mini-card::before {
	content: "";
	position: absolute;
	top: 0; left: 0; right: 0;
	height: 5px;
	background: var(--deck, var(--c-border-firm));
}
.flash-mini-card:hover { transform: translateY(-2px); box-shadow: 0 4px 12px rgb(0 0 0 / 0.08); }
.flash-mini-card:focus-visible { outline: var(--ring); outline-offset: 2px; }
.flash-mini-text {
	display: -webkit-box;
	-webkit-line-clamp: 3;
	-webkit-box-orient: vertical;
	overflow: hidden;
	white-space: pre-line;
}
.flash-mini-status,
.flash-mini-deck {
	position: absolute;
	bottom: var(--s-1);
	font-size: var(--fs-2xs);
	color: var(--c-text-faint);
}
.flash-mini-status { right: var(--s-2); }
.flash-mini-deck { left: var(--s-2); }
.flash-mini-new {
	border-style: dashed;
	border-color: var(--c-border-firm);
	background: transparent;
	color: var(--c-text-dim);
}
.flash-mini-new::before { display: none; }
.flash-grid-empty { grid-column: 1 / -1; }

/* The large, flippable card. The checkbox before it holds the state. */
.flash-viewer {
	display: flex;
	align-items: center;
	justify-content: center;
	gap: var(--s-3);
	margin: var(--s-4) 0 var(--s-2);
}
.flash-viewer-card { flex: 0 1 26rem; min-width: 0; }
.flash-nav-btn {
	display: inline-flex;
	align-items: center;
	justify-content: center;
	flex: none;
	width: 2.75rem;
	height: 2.75rem;
	border-radius: 50%;
	border: 1px solid var(--c-border-firm);
	color: var(--c-text-dim);
	font-size: 1.5rem;
	text-decoration: none;
}
.flash-nav-btn:hover { background: var(--c-bg-subtle); color: var(--c-text); }
.flash-nav-btn-empty { visibility: hidden; }
.flash-viewer-keys { text-align: center; }

.flash-card {
	display: block;
	margin: 0;
	perspective: 1000px;
	cursor: pointer;
	color: var(--c-text);
	font-size: var(--fs-base);
}
.flash-card-inner {
	position: relative;
	display: block;
	min-height: 15rem;
	transition: transform 0.55s cubic-bezier(0.3, 0.7, 0.3, 1.2);
	transform-style: preserve-3d;
}
.flash-flip:checked + .flash-card .flash-card-inner { transform: rotateY(180deg); }
.flash-card-face {
	position: absolute;
	inset: 0;
	display: flex;
	flex-direction: column;
	align-items: center;
	justify-content: center;
	gap: var(--s-3);
	padding: var(--s-6) var(--s-4) var(--s-4);
	overflow: auto;
	border-radius: 14px;
	border: 1px solid var(--c-border);
	background: var(--flash-card-bg);
	text-align: center;
	backface-visibility: hidden;
	-webkit-backface-visibility: hidden;
}
.flash-card-face::before {
	content: "";
	position: absolute;
	top: 0; left: 0; right: 0;
	height: 8px;
	background: var(--deck, var(--c-border-firm));
}
.flash-card-back { transform: rotateY(180deg); }
.flash-card-text { font-size: var(--fs-xl); font-weight: 500; white-space: pre-line; overflow-wrap: anywhere; }
.flash-card-notes { color: var(--c-text-dim); font-size: var(--fs-sm); white-space: pre-line; }
.flash-card-hint { color: var(--c-text-faint); font-size: var(--fs-xs); }
.flash-card-image { max-width: 100%; max-height: 8rem; border-radius: var(--radius); }
.flash-card-tags { display: flex; flex-wrap: wrap; justify-content: center; gap: var(--s-1); }
.flash-flip:focus-visible + .flash-card .flash-card-face { outline: var(--ring); outline-offset: 3px; }

/* Links and audio sit outside the <label> so clicking them can't also flip
 * the card; they appear once it has been flipped. */
.flash-card-extras { display: none; margin-top: var(--s-3); text-align: center; }
.flash-flip:checked ~ .flash-card-extras { display: block; }
.flash-tag-links { display: flex; flex-wrap: wrap; justify-content: center; gap: var(--s-2); margin: var(--s-2) 0 0; }

/* Cloze blanks and fills. */
.flash-blank {
	display: inline-block;
	min-width: 4em;
	border-bottom: 3px solid var(--deck, var(--c-accent));
	color: var(--c-text-faint);
	font-size: 0.7em;
	vertical-align: baseline;
}
.flash-fill {
	padding: 0 0.2em;
	border-radius: 4px;
	background: var(--deck-soft, var(--c-accent-bg));
	color: inherit;
}

.flash-media-details { margin-top: var(--s-5); }
.flash-media-details summary { cursor: pointer; color: var(--c-text-dim); font-size: var(--fs-sm); }

.flash-card-form { max-width: var(--measure); }

@media (prefers-reduced-motion: reduce) {
	.flash-card-inner { transition: none; }
	.flash-flip:checked + .flash-card .flash-card-inner { transform: none; }
	.flash-card-back { transform: none; opacity: 0; transition: opacity 0.2s; }
	.flash-card-front { transition: opacity 0.2s; }
	.flash-flip:checked + .flash-card .flash-card-back { opacity: 1; }
	.flash-flip:checked + .flash-card .flash-card-front { opacity: 0; }
	.flash-mini-card { transition: none; }
	.flash-mini-card:hover { transform: none; }
}
```

- [ ] **Step 2: Full check**

Run the full check from Global Constraints. Expected: clean.

- [ ] **Step 3: Commit**

```bash
git add internal/ui/static/app.css
git commit -m "style(flash): mini cards, flippable card, pills and cloze blanks"
```

---

### Task 7: Manual verification

- [ ] **Step 1:** Build (`go build -o /tmp/onsuite-bin ./cmd/onsuite`), start the `onsuite` preview (ask the user to sign in if needed), and import this deck through Import so there is realistic data:

```json
{"deck":{"name":"Spanish travel","description":"Trip phrases"},"cards":[
 {"type":"basic","front":"hola","back":"hello","notes":"informal","tags":["greetings"]},
 {"type":"basic","front":"¿Dónde está la playa?","back":"Where is the beach?","tags":["phrases","places"]},
 {"type":"cloze","front":"The capital of Spain is {{c1::Madrid::a city}}.","tags":["places"]},
 {"type":"basic","front":"la cuenta, por favor","back":"the bill, please","tags":["food"]}]}
```

- [ ] **Step 2:** Check, with a screenshot of each: Cards button swaps the pane (URL updates); grid shows "+ New card" first and mini cards striped in the deck colour; typing in search filters live without losing focus and the URL bar changes; pills filter and highlight; "Show “places” in all decks" opens the tag page; opening the cloze card shows a blank with "a city", clicking flips it with the animation to a highlighted "Madrid"; Space flips, ← → move, E opens the form; tag links under a flipped card filter the grid; dark theme; tablet width (768px) with "← Decks"; reduced motion (`prefers-reduced-motion` in the browser's rendering settings, if available) cross-fades instead of rotating; `read_console_messages` shows no CSP errors.

- [ ] **Step 3:** Fix what's broken, full check, commit.

- [ ] **Step 4: Record the pattern.** The CSS-only flip is the third use of
  "a visually hidden checkbox drives a `:checked ~` CSS state" (after
  `#paste-detail-open` and Reader's pane toggles). Add an entry to
  `PATTERNS.md`, in the same style as its neighbours:

  ```markdown
  - **Checkbox-driven CSS state instead of JS** — reach for this when a
    purely visual state (which pane shows, which face of a card is up)
    should work without JavaScript and inside the CSP: a visually hidden
    checkbox, a `<label for>` as the control, and `:checked + / ~` rules.
    Canonical: `internal/apps/flash/templates/card.partial.html`'s
    `flash-card` (the flip), also `#paste-detail-open` in
    `internal/apps/paste/templates/index.html`.
  ```

  Commit: `docs: add the checkbox-driven CSS state pattern`.

### Task 8: Open the PR

```bash
git push -u origin feat/flash-ui-u2
gh pr create --title "feat(flash): UI overhaul U2 — flippable cards and a cards grid in the deck pane" --body "$(cat <<'EOF'
## Summary
- Cloze cards now render as blanks (with hints) and highlighted answers everywhere.
- One `flash-card` component with a CSS-only flip; mini cards for the grid.
- A deck's cards live in the deck pane: grid with "+ New card", live search, tag pills, opened card with previous/next and keyboard shortcuts.
- The cross-deck tag page is a mini-card grid; the old cards page is gone.

Spec §1.3, §1.4, §3. Plan: docs/superpowers/plans/2026-09-23-flash-ui-u2-cards.md.

## Test plan
- [ ] `go test ./... -race -count=1`
- [ ] Manual: grid, search, pills, flip, cloze, keys, dark mode, tablet width (screenshots attached)
EOF
)"
```
