# ON Flash UI U6 — Import helper and stats in the pane Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make importing self-explanatory with a three-step strip and a "Copy the prompt" button that hands parents a ready-made AI prompt, and move Stats into the home screen's right pane with deck-coloured per-deck rows.

**Architecture:** The AI prompt is an embedded text file (`import_prompt.txt`) whose coverage of the importer's JSON fields is pinned by a reflection test. The import pane template is rewritten around it; `flash.js` gains a small clipboard helper. The stats page (`stats.html`) becomes a pane mode (`stats`) rendered through `buildDeckIndex`, with the per-deck table replaced by rows that reuse U1's stacks and an SVG mastery bar.

**Tech Stack:** Go (`embed`, `reflect` in a test), `html/template`, HTMX, `navigator.clipboard`, CSS.

**Spec:** [docs/superpowers/specs/2026-09-23-on-flash-ui-overhaul-design.md](../specs/2026-09-23-on-flash-ui-overhaul-design.md) — §7.

**This is PR 6 of 6.** It requires U1–U5 on `main`.

## Global Constraints

- Branch: `feat/flash-ui-u6` off an up-to-date `main` containing U1–U5. Never push to `main`.
- Full check before each Go commit:
  ```bash
  gofmt -l .
  go vet ./...
  go run honnef.co/go/tools/cmd/staticcheck@v0.8.0 ./...
  go mod tidy && git diff --exit-code go.mod go.sum
  go test ./... -race -count=1
  ```
- No new dependencies; CSP: no inline `<script>`, no `style=`. The mastery bar is an SVG `<rect width>`.
- An element with the `hidden` attribute must not be un-hidden by a CSS `display` rule — add `.x[hidden] { display: none; }` for any such element you style.
- `htmlassert` selectors: descendant chains of simple parts only; no compound parts.
- The stats tile labels `Streak`, `Retention (30 days)`, `Cards mastered`, `Cards due` and the empty text `No decks yet.` stay exactly as they are (tests depend on them).
- From U1–U5 (do not rename): `buildDeckIndex`, `renderDeckIndex`, `deckDetailView` (with `Notice`), `deckModeImport`, `importDeckDetail`, `plural`, `DeckSummaries`, templates `deck-detail-import`, `flash-home-toolbar`, `flash-stack`, `flash-pane-back`, `deck-detail-view`; CSS `.flash-stat-tiles`, `.flash-stat-tile`, `.flash-chart`, `.flash-due-badge`, `.flash-big-btn*`, `.deck-c-*`.
- Tests: `newServer(t)`, `apptest.NewServer`, `s.Get`, `s.Post`, `s.Do`, `htmlassert`; internal tests `package flash`.
- Commits: Conventional Commits, scope `flash`.

---

## File map

| File | Change |
|---|---|
| `internal/apps/flash/import_prompt.txt` | **Create** — the AI prompt |
| `internal/apps/flash/import_prompt.go` | **Create** — `//go:embed` + `importPrompt` |
| `internal/apps/flash/handlers_import.go` | pane gets the prompt; success notice |
| `internal/apps/flash/templates/import.partial.html` | **Rewrite** |
| `internal/apps/flash/handlers_stats.go` | stats render in the pane; `deckLoadRow` |
| `internal/apps/flash/handlers_decks.go` | `deckModeStats`, `deckDetailView.Stats`, `ImportPrompt`, title |
| `internal/apps/flash/templates/stats.html` | **Delete** |
| `internal/apps/flash/templates/stats.partial.html` | **Create** — `deck-detail-stats` |
| `internal/apps/flash/templates/decks.html` | dispatcher case; Stats toolbar button targets the pane |
| `internal/apps/flash/static/flash.js` | Copy prompt |
| `internal/ui/static/app.css` | import strip, stats rows |
| Tests | `import_prompt_test.go` (new, internal), `handlers_import_test.go`, `handlers_stats_test.go` |

---

### Task 1: The AI prompt

**Files:**
- Create: `internal/apps/flash/import_prompt.txt`, `internal/apps/flash/import_prompt.go`
- Test: create `internal/apps/flash/import_prompt_test.go` (package `flash`)

**Interfaces:**
- Produces: `var importPrompt string` (the embedded file); the file contains the placeholder `[TOPIC]`.

- [ ] **Step 1: Write the failing test**

```go
// internal/apps/flash/import_prompt_test.go
package flash

import (
	"reflect"
	"strings"
	"testing"
)

// jsonFieldNames collects every json tag name in t, recursing into structs
// and slices of structs — the importer's whole accepted schema.
func jsonFieldNames(t reflect.Type) []string {
	for t.Kind() == reflect.Slice || t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct {
		return nil
	}
	var names []string
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if tag, _, _ := strings.Cut(f.Tag.Get("json"), ","); tag != "" && tag != "-" {
			names = append(names, tag)
		}
		names = append(names, jsonFieldNames(f.Type)...)
	}
	return names
}

// TestImportPromptCoversTheSchema keeps the AI prompt from drifting away
// from what the importer accepts: every JSON field importJSON knows must
// be named in the prompt (UI overhaul spec §7).
func TestImportPromptCoversTheSchema(t *testing.T) {
	names := jsonFieldNames(reflect.TypeOf(importJSON{}))
	if len(names) < 5 {
		t.Fatalf("found only %v json fields; the reflection walk is broken", names)
	}
	for _, name := range names {
		if !strings.Contains(importPrompt, `"`+name+`"`) {
			t.Errorf("the import prompt never mentions the %q field", name)
		}
	}
	for _, must := range []string{"[TOPIC]", "{{c1::", `"basic"`, `"cloze"`} {
		if !strings.Contains(importPrompt, must) {
			t.Errorf("the import prompt is missing %q", must)
		}
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/apps/flash/ -run TestImportPromptCoversTheSchema -count=1`
Expected: build failure — `undefined: importPrompt`.

- [ ] **Step 3: Write the prompt and embed it**

Create `internal/apps/flash/import_prompt.txt` with exactly:

```text
Please make flash cards for ON Flash, a flash-card app.

Topic: [TOPIC]

Reply with JSON only: no explanation before or after it, and no code fences. Use exactly this shape:

{
  "deck": {
    "name": "A short name for the deck",
    "description": "One sentence about what the deck covers"
  },
  "cards": [
    {
      "type": "basic",
      "front": "The question",
      "back": "The answer",
      "notes": "Optional: a short hint or fun fact, shown after the answer",
      "tags": ["optional", "topic", "words"],
      "image": "Optional: a direct https:// link to a picture (.jpg, .png, .gif or .webp)"
    },
    {
      "type": "cloze",
      "front": "A sentence with the hidden words marked like {{c1::this}}.",
      "tags": ["optional"]
    }
  ]
}

Rules:
- Make 20 cards unless the topic says a different number.
- Use "basic" for question-and-answer cards.
- Use "cloze" for fill-in-the-blank cards: put {{c1::...}} around the hidden words in "front" and leave out "back". Use {{c2::...}} for a second blank in the same sentence.
- Keep each "front" and "back" short: a few words or one sentence.
- Only add "image" when you know a real, working link to a picture. Leave it out otherwise.
- "audio" works like "image", for a direct link to a sound file (.mp3 or .ogg). Leave it out unless you know a real link.
- Write for a student aged about 9 to 14 unless the topic says otherwise.
```

Create `internal/apps/flash/import_prompt.go`:

```go
// internal/apps/flash/import_prompt.go
package flash

import _ "embed"

// importPrompt is the ready-made AI prompt the import pane's "Copy the
// prompt" button copies (UI overhaul spec §7): it describes the JSON
// schema importJSON accepts, with a [TOPIC] placeholder for the person to
// fill in. TestImportPromptCoversTheSchema keeps it in step with the
// schema.
//
//go:embed import_prompt.txt
var importPrompt string
```

- [ ] **Step 4: Run to verify it passes**

Run: `go test ./internal/apps/flash/ -run TestImportPromptCoversTheSchema -count=1`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/apps/flash/import_prompt.txt internal/apps/flash/import_prompt.go internal/apps/flash/import_prompt_test.go
git commit -m "feat(flash): a ready-made AI prompt for importing decks"
```

---

### Task 2: The import pane

**Files:**
- Modify: `internal/apps/flash/handlers_import.go`, `internal/apps/flash/handlers_decks.go`, `internal/apps/flash/templates/import.partial.html`, `internal/apps/flash/static/flash.js`
- Test: `internal/apps/flash/handlers_import_test.go`

**Interfaces:**
- Produces: `deckDetailView.ImportPrompt string`; JS hook `button.flash-copy-prompt[data-copy-target]` (starts `hidden`; `flash.js` reveals it); textarea `#import-prompt`.

- [ ] **Step 1: Write the failing tests**

Append to `internal/apps/flash/handlers_import_test.go` (it already imports `url`, `strings`, `testing`, `htmlassert`):

```go
func TestImportPaneHasTheThreeStepsAndThePrompt(t *testing.T) {
	s := newServer(t)
	doc := s.Get(t, s.Alice, "/flash/import")
	if n := len(doc.QueryAll(".flash-import-steps li")); n != 3 {
		t.Errorf("import pane shows %d steps, want 3", n)
	}
	prompt := doc.MustHave("textarea#import-prompt")
	if !strings.Contains(htmlassert.Text(prompt), "[TOPIC]") {
		t.Error("the prompt box does not contain the AI prompt")
	}
	if _, ok := htmlassert.Attr(prompt, "readonly"); !ok {
		t.Error("the prompt box should be read-only")
	}
	copyBtn := doc.MustHave("button.flash-copy-prompt")
	if target, _ := htmlassert.Attr(copyBtn, "data-copy-target"); target != "import-prompt" {
		t.Errorf("Copy button data-copy-target = %q", target)
	}
	if _, ok := htmlassert.Attr(copyBtn, "hidden"); !ok {
		t.Error("the Copy button must start hidden; flash.js reveals it")
	}
	// The format picker is tucked away; auto-detect is the default.
	doc.MustHave(`details.flash-import-format select[name=format]`)
	doc.MustHave(`textarea[name=payload]`)
}

func TestImportSuccessShowsANotice(t *testing.T) {
	s := newServer(t)
	payload := `{"deck":{"name":"Planets"},"cards":[{"type":"basic","front":"Mars","back":"red"},{"type":"basic","front":"Earth","back":"home"}]}`
	rec := s.PostHX(t, s.Alice, "/flash/import", url.Values{"payload": {payload}, "format": {"auto"}})
	if rec.Code != 201 {
		t.Fatalf("import over HTMX = %d; body: %s", rec.Code, rec.Body.String())
	}
	doc := htmlassert.Parse(t, rec.Body.String())
	notice := doc.MustHave("#deck-detail-view .flash-notice")
	if got := htmlassert.Text(notice); !strings.Contains(got, "Imported 2 cards") {
		t.Errorf("notice = %q, want Imported 2 cards", got)
	}
}
```

- [ ] **Step 2: Run to verify they fail**

Run: `go test ./internal/apps/flash/ -run 'ImportPane|ImportSuccess' -count=1`
Expected: FAIL.

- [ ] **Step 3: Go changes**

In `handlers_decks.go`, add to `deckDetailView` next to `PayloadValue`/`FormatValue`:

```go
	ImportPrompt string // the AI prompt shown in the import pane
```

In `handlers_import.go`, set it in `importDeckDetail`:

```go
	return deckDetailView{
		Mode: deckModeImport, PayloadValue: payload, FormatValue: format, ImportPrompt: importPrompt,
		Error: errMsg, CSRFToken: web.CSRFToken(r.Context()),
	}
```

and in `importDeck`, replace the final `a.renderDeckDetailWithList(...)` call with:

```go
	view := a.viewDeckDetail(r, userID, d, recipients, shares)
	view.Notice = fmt.Sprintf("Imported %d card%s into “%s”.", len(parsed.Cards), plural(len(parsed.Cards)), d.Name)
	a.renderDeckDetailWithList(w, r, userID, http.StatusCreated, view)
```

(add `"fmt"` to the imports).

- [ ] **Step 4: Rewrite `import.partial.html`**

```html
{{/* internal/apps/flash/templates/import.partial.html — the import pane
     (UI overhaul spec §7). Three steps: copy a ready-made prompt, ask any
     AI, paste its answer. Without JS the prompt is in a read-only box to
     select and copy by hand; flash.js adds the Copy button. */}}
{{define "deck-detail-import"}}
<form class="stack flash-import" id="deck-detail-import" method="post" action="/flash/import" hx-post="/flash/import" hx-target="#deck-detail" hx-swap="innerHTML">
	<input type="hidden" name="{{csrfField}}" value="{{.CSRFToken}}">
	<div class="notes-toolbar flash-deck-toolbar">{{template "flash-pane-back"}}</div>
	<h1>Import a deck</h1>
	{{with .Error}}<div class="notice notice-error" role="alert">{{.}}</div>{{end}}

	<ol class="flash-import-steps">
		<li>
			<strong>Copy the prompt</strong>
			<span class="dim">It tells the AI exactly how to write the cards.</span>
			<button type="button" class="flash-big-btn flash-copy-prompt" data-copy-target="import-prompt" hidden>{{ticon "doc"}}<span class="flash-copy-label">Copy the prompt</span></button>
		</li>
		<li>
			<strong>Ask your AI</strong>
			<span class="dim">Paste the prompt into any AI chat and replace [TOPIC] with what you want to learn.</span>
		</li>
		<li>
			<strong>Paste the answer below</strong>
			<span class="dim">Then press Import.</span>
		</li>
	</ol>

	<details class="flash-import-prompt">
		<summary>Show the prompt</summary>
		<label class="visually-hidden" for="import-prompt">The AI prompt</label>
		<textarea id="import-prompt" rows="14" readonly>{{.ImportPrompt}}</textarea>
	</details>

	<div class="field">
		<label for="import-payload">The AI's answer</label>
		<textarea id="import-payload" name="payload" rows="12" placeholder='{"deck": {"name": "…"}, "cards": […]}' required>{{.PayloadValue}}</textarea>
	</div>

	<details class="flash-import-format"{{if and .FormatValue (ne .FormatValue "auto")}} open{{end}}>
		<summary>Choose format (usually not needed)</summary>
		<div class="field">
			<label for="import-format">Format</label>
			<select id="import-format" name="format">
				<option value="auto"{{if eq .FormatValue "auto"}} selected{{end}}>Work it out for me</option>
				<option value="json"{{if eq .FormatValue "json"}} selected{{end}}>JSON</option>
				<option value="markdown"{{if eq .FormatValue "markdown"}} selected{{end}}>Markdown</option>
			</select>
		</div>
	</details>

	<div class="row">
		<button type="submit" class="primary">Import</button>
		<a class="toolbar-btn" href="/flash/" hx-get="/flash/" hx-target="#deck-detail" hx-push-url="true">Cancel</a>
	</div>
</form>
{{end}}
```

> The `doc` icon exists already in `internal/ui/toolbar_icons.go`.

- [ ] **Step 5: Copy button in `flash.js`**

Add a header comment line: `// Import (U6): Copy the prompt.` Inside the IIFE, in `enhance(root)` add:

```js
		each(".flash-copy-prompt", function (btn) { btn.hidden = false; });
```

and, next to the other document-level click listeners:

```js
	// Copy the import prompt to the clipboard, confirming on the button.
	// navigator.clipboard needs a secure context (https or localhost); where
	// it is missing, open the prompt box and select its text instead.
	document.addEventListener("click", function (e) {
		var btn = e.target.closest && e.target.closest(".flash-copy-prompt");
		if (!btn) return;
		var box = document.getElementById(btn.getAttribute("data-copy-target"));
		if (!box) return;
		var label = btn.querySelector(".flash-copy-label") || btn;
		function done(text) {
			label.textContent = text;
			setTimeout(function () { label.textContent = "Copy the prompt"; }, 2000);
		}
		function selectIt() {
			var details = box.closest("details");
			if (details) details.open = true;
			box.focus();
			box.select();
			done("Press Ctrl+C to copy");
		}
		if (navigator.clipboard && navigator.clipboard.writeText) {
			navigator.clipboard.writeText(box.value).then(function () { done("Copied!"); }, selectIt);
		} else {
			selectIt();
		}
	});
```

- [ ] **Step 6: Run the tests, commit**

Run: `go test ./internal/apps/flash/ -count=1` → PASS (including the existing import tests: `.deck-list`, `.notice-error` on bad input, the redirect for non-HTMX posts).

```bash
git add -A internal/apps/flash
git commit -m "feat(flash): three-step import with a Copy the prompt button"
```

---

### Task 3: Stats in the right pane

**Files:**
- Delete: `internal/apps/flash/templates/stats.html`
- Create: `internal/apps/flash/templates/stats.partial.html`
- Modify: `internal/apps/flash/handlers_stats.go`, `internal/apps/flash/handlers_decks.go`, `internal/apps/flash/templates/decks.html`
- Test: `internal/apps/flash/handlers_stats_test.go`

**Interfaces:**
- Consumes: `PerDeckLoad`, `DeckSummaries`, existing `statsView`/`buildChart`.
- Produces: `const deckModeStats = "stats"`; `deckDetailView.Stats statsView`; `statsView.Rows []deckLoadRow`; `type deckLoadRow struct { Deck Deck; Mastered, Due, Reviews, CardCount, MasteredPct int; Snoozed bool }`; template `deck-detail-stats`; CSS hooks `.flash-load-row`, `.flash-load-name`, `.flash-load-mastered`, `.flash-load-due`, `.flash-load-reviews-count`.

- [ ] **Step 1: Update the stats tests**

In `internal/apps/flash/handlers_stats_test.go`:

In `TestStatsPageRendersConsistentNumbers`, replace everything from `// Per-deck table: rows come out sorted by name` to the end of the function with:

```go
	// Per-deck rows: sorted by name (Alpha, then Beta), each with its own
	// mastered / due / reviews-in-30-days numbers.
	texts := func(sel string) []string {
		var out []string
		for _, n := range doc.QueryAll(sel) {
			out = append(out, htmlassert.Text(n))
		}
		return out
	}
	if names := texts(".flash-load-row .flash-load-name"); strings.Join(names, ",") != "Alpha,Beta" {
		t.Fatalf("row names = %v, want [Alpha Beta]", names)
	}
	if got := texts(".flash-load-row .flash-load-mastered"); strings.Join(got, ",") != "1,0" {
		t.Errorf("mastered = %v, want [1 0]", got)
	}
	if got := texts(".flash-load-row .flash-load-due"); strings.Join(got, ",") != "all done,1 due" {
		t.Errorf("due = %v, want [all done, 1 due]", got)
	}
	if got := texts(".flash-load-row .flash-load-reviews-count"); strings.Join(got, ",") != "3,1" {
		t.Errorf("reviews = %v, want [3 1]", got)
	}
}
```

In `TestStatsPageMarksSnoozedDeckInPerDeckTable`, change the selector `".flash-per-deck-table tbody tr"` to `".flash-load-row"`, and the two `"snoozed"` substring checks to `"taking a break"`:

```go
	rows := doc.QueryAll(".flash-load-row")
	…
	if strings.Contains(activeRow, "taking a break") {
		t.Errorf("active deck's row wrongly marked as taking a break: %q", activeRow)
	}
	if !strings.Contains(snoozedRow, "taking a break") {
		t.Errorf("snoozed deck's row not marked as taking a break: %q", snoozedRow)
	}
```

Append:

```go
func TestStatsRenderInsideTheHomeLayout(t *testing.T) {
	s := newServer(t)
	deck, err := s.Store.CreateDeck(t.Context(), s.Alice.User.ID, "Spanish", "")
	if err != nil {
		t.Fatal(err)
	}
	doc := s.Get(t, s.Alice, "/flash/stats")
	doc.MustHave("#deck-list")
	doc.MustHave("#deck-detail .flash-stats")
	row := doc.MustHave(".flash-load-row")
	if href, _ := htmlassert.Attr(row, "href"); href != "/flash/"+itoa(deck.ID) {
		t.Errorf("stats row href = %q, want the deck", href)
	}
	if target, _ := htmlassert.Attr(row, "hx-target"); target != "#deck-detail" {
		t.Errorf("stats row hx-target = %q", target)
	}
	btn := doc.MustHave(`.flash-home-toolbar a[href="/flash/stats"]`)
	if target, _ := htmlassert.Attr(btn, "hx-target"); target != "#deck-detail" {
		t.Errorf("toolbar Stats hx-target = %q, want the pane", target)
	}
}
```

- [ ] **Step 2: Run to verify they fail**

Run: `go test ./internal/apps/flash/ -run Stats -count=1`
Expected: FAIL.

- [ ] **Step 3: Go changes**

In `handlers_decks.go`: add `deckModeStats = "stats"` to the mode constants, `case deckModeStats: return "Stats"` to `deckPageTitle`, and `Stats statsView` to `deckDetailView` (next to `Gift`).

In `handlers_stats.go`, add the row type and replace the tail of `stats` (from `view := statsView{` to the end):

```go
// deckLoadRow is one deck's line on the stats pane.
type deckLoadRow struct {
	Deck        Deck
	Mastered    int
	Due         int
	Reviews     int // reviews in the last 30 days
	CardCount   int
	MasteredPct int // Mastered / CardCount, for the SVG bar's width
	Snoozed     bool
}
```

Add `Rows []deckLoadRow` to `statsView` (keep `PerDeck` for now only if other code reads it — nothing does; remove it and use `Rows`).

```go
	sums, err := a.store.DeckSummaries(ctx, userID, now)
	if err != nil {
		a.deps.Errors.Internal(w, r, err)
		return
	}
	cardCounts := make(map[int64]int, len(sums))
	for _, s := range sums {
		cardCounts[s.Deck.ID] = s.CardCount
	}
	rows := make([]deckLoadRow, len(perDeck))
	for i, l := range perDeck {
		n := cardCounts[l.Deck.ID]
		rows[i] = deckLoadRow{
			Deck: l.Deck, Mastered: l.Mastered, Due: l.Due, Reviews: l.ReviewsLast30Days,
			CardCount: n, MasteredPct: progressPercent(l.Mastered, n), Snoozed: l.Snoozed,
		}
	}

	view := statsView{
		Tiles: []statTile{
			{Label: "Streak", Value: fmt.Sprintf("%d day(s)", streak)},
			{Label: "Retention (30 days)", Value: fmt.Sprintf("%.0f%%", rate*100)},
			{Label: "Cards mastered", Value: fmt.Sprintf("%d", mastered)},
			{Label: "Cards due", Value: fmt.Sprintf("%d", due)},
		},
		Chart: buildChart("Reviews per day", days, func(d DayCount) int { return d.Count }),
		Days:  days,
		Rows:  rows,
	}
	a.renderDeckIndex(w, r, userID, http.StatusOK, deckDetailView{Mode: deckModeStats, Stats: view})
}
```

(`progressPercent` is U4's helper in `review_summary.go`.)

- [ ] **Step 4: Templates**

`git rm internal/apps/flash/templates/stats.html`, then create `internal/apps/flash/templates/stats.partial.html`:

```html
{{/* internal/apps/flash/templates/stats.partial.html — stats in the home
     screen's right pane (UI overhaul spec §7). Same numbers as F6's page;
     the per-deck table is now one row per deck, in its colour. */}}
{{define "deck-detail-stats"}}
{{with .Stats}}
<div class="flash-stats" id="deck-detail-stats">
	<div class="notes-toolbar flash-deck-toolbar">{{template "flash-pane-back"}}</div>
	<h1>Stats</h1>

	<div class="flash-stat-tiles">
		{{range .Tiles}}
		<div class="flash-stat-tile">
			<div class="value">{{.Value}}</div>
			<div class="label">{{.Label}}</div>
		</div>
		{{end}}
	</div>

	<figure class="flash-chart">
		<figcaption>{{.Chart.Title}}</figcaption>
		{{if .Chart.Empty}}
		<p class="empty">Nothing yet.</p>
		{{else}}
		<svg viewBox="0 0 {{.Chart.Width}} {{.Chart.Height}}" role="img"
		     aria-label="{{.Chart.Title}}, daily, peak {{.Chart.Max}}" preserveAspectRatio="none">
			{{range .Chart.Bars}}
			<rect x="{{.X}}" y="{{.Y}}" width="{{.Width}}" height="{{.Height}}" rx="1">
				<title>{{.Label}}</title>
			</rect>
			{{end}}
		</svg>
		{{end}}
	</figure>

	{{if .Days}}
	<details class="flash-stats-daily">
		<summary>Daily figures (text alternative to the chart above)</summary>
		<div class="flash-table-scroll">
			<table class="flash-stats-table">
				<thead><tr><th>Date</th><th>Reviews</th></tr></thead>
				<tbody>
					{{range .Days}}
					<tr><td>{{.Day.Format "2 Jan 2006"}}</td><td>{{.Count}}</td></tr>
					{{end}}
				</tbody>
			</table>
		</div>
	</details>
	{{end}}

	<h2 class="flash-section-title">Your decks</h2>
	{{if .Rows}}
	<ul class="flash-load-list">
		{{range .Rows}}
		<li>
			<a class="flash-load-row deck-c-{{.Deck.Color}}" href="/flash/{{.Deck.ID}}" hx-get="/flash/{{.Deck.ID}}" hx-target="#deck-detail" hx-push-url="true">
				{{template "flash-stack" (dict "Size" "sm")}}
				<span class="flash-load-main">
					<span class="flash-load-name">{{.Deck.Name}}</span>
					{{if .Snoozed}}<span class="flash-snoozed-marker">taking a break</span>{{end}}
					<span class="flash-load-mastery">
						<svg class="flash-mastery-bar" viewBox="0 0 100 6" preserveAspectRatio="none" aria-hidden="true">
							<rect class="track" x="0" y="0" width="100" height="6" rx="3"/>
							<rect class="fill" x="0" y="0" width="{{.MasteredPct}}" height="6" rx="3"/>
						</svg>
						<span><span class="flash-load-mastered">{{.Mastered}}</span> of {{.CardCount}} mastered · <span class="flash-load-reviews-count">{{.Reviews}}</span> reviews in 30 days</span>
					</span>
				</span>
				<span class="flash-load-due{{if .Due}} flash-due-badge{{end}}">{{if .Due}}{{.Due}} due{{else}}all done{{end}}</span>
			</a>
		</li>
		{{end}}
	</ul>
	{{else}}
	<p class="empty">No decks yet.</p>
	{{end}}
</div>
{{end}}
{{end}}
```

In `decks.html`:
- add `{{else if eq .Mode "stats"}}{{template "deck-detail-stats" .}}` to `deck-detail-body`;
- in `flash-home-toolbar`, change the Stats link to target the pane:
  ```html
		<a class="toolbar-btn" href="/flash/stats" hx-get="/flash/stats" hx-target="#deck-detail" hx-push-url="true">{{ticon "stats"}}Stats</a>
  ```

> The "Snoozed" row text is now "taking a break", matching the deck list's
> own wording.

- [ ] **Step 5: Run the tests, commit**

Run: `go test ./internal/apps/flash/ -count=1` → PASS (all stats tests, including the zero-state one that looks for `Streak`, `Cards due` and `No decks yet.`).

```bash
git add -A internal/apps/flash
git commit -m "feat(flash): stats live in the home screen's right pane"
```

---

### Task 4: CSS

**Files:** `internal/ui/static/app.css`

- [ ] **Step 1:** Delete the old `.flash-per-deck-table` rules if any remain (`grep -n "flash-per-deck" internal/ui/static/app.css`), then append:

```css
/* ---- ON Flash: import and stats (UI overhaul U6) ------------------------ */

.flash-import { max-width: var(--measure); }
.flash-import-steps {
	display: grid;
	gap: var(--s-3);
	margin: 0 0 var(--s-4);
	padding: 0;
	list-style: none;
	counter-reset: flash-step;
}
.flash-import-steps li {
	position: relative;
	display: flex;
	flex-direction: column;
	align-items: flex-start;
	gap: var(--s-1);
	padding: var(--s-3) var(--s-4) var(--s-3) 3.25rem;
	border-radius: 10px;
	background: var(--c-bg-subtle);
	counter-increment: flash-step;
}
.flash-import-steps li::before {
	content: counter(flash-step);
	position: absolute;
	left: var(--s-3);
	top: var(--s-3);
	display: inline-flex;
	align-items: center;
	justify-content: center;
	width: 1.75rem;
	height: 1.75rem;
	border-radius: 50%;
	background: var(--c-accent);
	color: #fff;
	font-weight: 600;
	font-size: var(--fs-sm);
}
:root[data-theme="dark"] .flash-import-steps li::before { color: #10141a; }
.flash-copy-prompt { margin-top: var(--s-2); }
.flash-copy-prompt[hidden] { display: none; }
.flash-import-prompt summary,
.flash-import-format summary { cursor: pointer; color: var(--c-text-dim); font-size: var(--fs-sm); }
.flash-import-prompt textarea { margin-top: var(--s-2); font-size: var(--fs-sm); }

.flash-stats h1 { margin: 0 0 var(--s-4); }
.flash-load-list { margin: 0; padding: 0; list-style: none; }
.flash-load-row {
	display: flex;
	align-items: center;
	gap: var(--s-3);
	min-height: 3.5rem;
	padding: var(--s-2) var(--s-3);
	margin-bottom: var(--s-1);
	border-radius: 8px;
	color: inherit;
	text-decoration: none;
}
.flash-load-row:hover { background: var(--c-bg-subtle); }
.flash-load-main { flex: 1; min-width: 0; display: flex; flex-direction: column; gap: 2px; }
.flash-load-name { font-weight: 500; }
.flash-load-mastery { display: flex; align-items: center; gap: var(--s-2); font-size: var(--fs-xs); color: var(--c-text-dim); }
.flash-mastery-bar { width: 5rem; height: 6px; flex: none; }
.flash-mastery-bar .track { fill: var(--c-bg-inset); }
.flash-mastery-bar .fill { fill: var(--deck, var(--c-accent)); }
.flash-load-due { font-size: var(--fs-xs); color: var(--c-text-dim); white-space: nowrap; }
.flash-load-due.flash-due-badge { color: var(--c-accent-attention); }
```

- [ ] **Step 2:** Full check; commit `style(flash): import steps and stats rows`.

---

### Task 5: Manual verification, docs, PR

- [ ] **Step 1:** Build, start the `onsuite` preview (ask the user to sign in). Check with screenshots:
  1. Import from the toolbar: three numbered steps; "Copy the prompt" copies (paste it somewhere to confirm) and says "Copied!"; "Show the prompt" reveals the read-only box; pasting the JSON from U2's Task 7 imports and opens the deck with "Imported 4 cards into …".
  2. A broken paste shows a friendly error with the text kept.
  3. Stats from the toolbar opens in the pane (deck list stays); tiles and chart as before; each deck row has its stack, mastery bar and due badge; clicking a row opens the deck.
  4. Dark theme; tablet width; no console errors.
- [ ] **Step 2: Update AGENTS.md's app description.** Its "What this is" paragraph still says ON Flash is "still being built out — deck and card CRUD with tags so far, no review/scheduling yet". Replace that parenthetical with: `**ON Flash** (flash cards with FSRS review, import from AI-written Markdown/JSON, media, household sharing and stats)`.
- [ ] **Step 3:** Full check, commit (`docs: describe ON Flash as complete in AGENTS.md`), push, and open the PR:

```bash
git push -u origin feat/flash-ui-u6
gh pr create --title "feat(flash): UI overhaul U6 — import helper and stats in the pane" --body "$(cat <<'EOF'
## Summary
- Import pane: three numbered steps, a ready-made AI prompt (embedded file, kept in step with the importer's JSON fields by a reflection test) with a Copy button, the format picker tucked away, and a success notice.
- Stats open in the home screen's right pane; the per-deck table becomes deck-coloured rows with a mastery bar and due badge.
- AGENTS.md no longer calls ON Flash unfinished.

Completes the UI overhaul (U1–U6). Spec §7. Plan: docs/superpowers/plans/2026-09-23-flash-ui-u6-import-stats.md.

## Test plan
- [ ] `go test ./... -race -count=1`
- [ ] Manual checklist from the plan's Task 5 (screenshots attached)
EOF
)"
```
