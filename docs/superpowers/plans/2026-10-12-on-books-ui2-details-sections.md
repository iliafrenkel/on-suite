# ON Books UI2: the details page sections, and Tags and Find cover on Edit. Implementation plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Turn the book page's body into the spec's six sections (Reading, Your take, About, Notes, Quotes, Reading history), each with one look and smaller swaps. Move tag editing to the Edit page. Give Add and Edit a page bar. Make Find cover keep unsaved edits (#591). Then update the guide and the screenshots.

**Architecture:** The data model doesn't change, and there is no migration.

The book page keeps its page (`book.html`, `book-page`, `book-body`). Its body is rebuilt as `<section class="books-section">` blocks. Each section's one action is the `<summary>` of that section's own `<details>`, lifted into the heading row by CSS, so every disclosure still works without JavaScript.

The swaps get smaller:
- **`reading-swap`** answers progress and swaps the whole Reading section.
- **`take-swap`** answers rating and review. It swaps Your take, and brings the Reading section along out of band while a reading is open.
- **`notes-swap`** and **`quotes-swap`** stay as they are.
- **`book-swap`** answers the rest.

A refusal's banner always lands on the page body, through `HX-Retarget`.

Other changes:
- **Tags:** `Store.SaveEdit` replaces `UpdateWithCover` and saves the details, the cover and the tags in one transaction. The tags route and form go.
- **Find cover** becomes a second submit button of the Edit form (`formaction`), so its post carries every field (#591).
- **Add** carries the list it came from, but not that list's search: on the Add page, `q` is the Open Library search.

**Tech Stack:** Go `net/http`, `html/template`, htmx (vendored), SQLite (modernc), and vanilla CSP-clean JS. There is no build step.

**Spec:** [docs/superpowers/specs/2026-10-12-on-books-ui-redesign-design.md](../specs/2026-10-12-on-books-ui-redesign-design.md). The mockups are next to it. This plan is phase **UI2** of the spec's "Phases" table. It builds on UI1, which was merged in #590; its plan is [2026-10-12-on-books-ui1-home-and-book-page.md](2026-10-12-on-books-ui1-home-and-book-page.md).

**Also in this plan:** the UI1 follow-ups #591 (Task 6), #592 (Task 8), #593 (Task 7) and #594 (Tasks 5, 6, 7 and 9).

**Decided with Ilia on 2026-10-12** (they refine the spec):
- **#591:** Find cover posts the whole Edit form, and the page comes back with what was typed. The lookup uses the typed ISBN, title and author when the form would save.
- **Your take has one opener.** With a review, ✎ sits in the heading. With none, "Write a review" sits under the stars and the heading has no icon. A no-JS `<details>` can't have two openers.
- **The format select shows an icon** for each format: `book-open` (paper), `tablet` (ebook) and `headphones` (audiobook). These are three new `ticon` icons. No icon shows while the format isn't set.

## Global Constraints

- **Tokens only.** Add no new colours or spacing values (app.css header: "Apps must not introduce new colours or spacing values"). Use `--c-*`, `--s-*`, `--fs-*`, `--radius`, `--border` and `--ring`.
- **No inline `<script>` and no `style=`.** The strict CSP forbids them. JS goes in `internal/apps/books/static/books.js`, CSS in `internal/ui/static/app.css`.
- **No JavaScript or CSS build step**, no new Go dependency, and no migration.
- **Every form works without JavaScript.** Give it a real `method`/`action`, with `hx-post` identical to `action` (PATTERNS.md, "hx-post mirrors formaction"). A disclosure is a `<details>` (PATTERNS.md, "No-JS `<details>` disclosure menu").
- **Apps never import each other.**
- **Time:** app code reads time only through `st.now()`. Handler tests pin it with `s.Clock`.
- **Full check stays green on every commit:**
  - `gofmt -l .` prints nothing
  - `go vet ./...`
  - `go run honnef.co/go/tools/cmd/staticcheck@v0.8.0 ./...`
  - `go test ./... -race -count=1`
- **Introduce a helper in the task that first calls it.** staticcheck U1000 rejects unused unexported code.
- **`internal/htmlassert` takes one qualifier per selector part.** `select#books-format option[selected]` is fine. Never write `.a.b`, `#a.b` or `[a][b]`.
- **Section headings are verbatim:** `Reading`, `Your take`, `About`, `Notes`, `Quotes`, `Reading history`. A count follows as ` · n` (in `<span class="books-section-count">`) on Notes and Quotes only when n > 0, and always on Reading history, which is left out when empty.
- **Button labels are verbatim:** `Finish…` and `Did not finish…` (with the ellipsis character), `Write a review`. The start button keeps `StartLabel` (`Start reading`, `Read again` or `Start again`).
- **Every icon-only control** has an `aria-label` and the same text as its `title`. These are: `Edit details`, `Delete book`, `Save progress`, `Edit review`, `Add a note`, `Edit note`, `Delete note`, `Add a quote`, `Edit quote`, `Delete quote`, `Edit reading`, `Delete reading`.
- **Keep the ids and classes the tests already use** unless a step says otherwise: `#books-book`, `#books-progress`, `#books-progress-input`, `#books-notes`, `#books-quotes`, `#books-note-new`, `#books-quote-new`, `#books-history-head`, `.books-note`, `.books-quote`, `.books-entry-meta`, `.books-entry-text`, `.books-entry-edit`, `.books-history-item`, `.books-history-line`, `details.books-close`, `.books-start`.
- **Commits:** Conventional Commits with scope `books` (`ui` for the icon map and the `.visually-hidden` merge, `docs` for the guide). Work on a branch (the executing skill creates it). Never push to `main`.

## Review Focus

Each of these has a test in the task named:

- **Re-rating a book you're reading, then finishing it** from the Finish… popover, keeps the new stars. The popover's rating select must follow the stars (Task 2, `TestRatingOverHTMXRefreshesTheFinishRating`).
- **Updating progress, then Did not finish…,** stops at the new page, not at the one the page loaded with (Task 1, `TestProgressSwapsTheWholeReadingSection`).
- **Find cover after typing in the form,** even with an image chosen in the upload box (a post over 1 MiB), comes back with what was typed, not a 413 (Task 6, `TestFindCoverKeepsWhatWasTyped`, `TestFindCoverTakesTheEditFormsBody`).
- **Clearing the Tags box on Edit** removes every tag, and a tag nobody uses goes (Task 4, `TestEditSavesTags`).
- **Add opened from a searched tag list** goes back to that tag list, through an Open Library search and a pick. The list's search never turns into the Open Library search (Task 5, `TestAddKeepsItsListButNotItsSearch`).

---

## File structure

| File | Change | Responsibility |
|---|---|---|
| `internal/apps/books/templates/book.partial.html` | modify (Tasks 1–4) | the sections, their swaps, the header pills |
| `internal/apps/books/view.go` | modify | `bookView.FormatIcon`, `HistoryOpen` and `Tags []tagLink`; `tagLinks`; `listCtx.values`/`AddURL`; the progress note; the pressed view while searching (Task 9) |
| `internal/apps/books/handlers.go` | modify | `renderBook`'s block choice, the banner retarget, `HistoryOpen` |
| `internal/apps/books/actions.go` | modify | `setTags` removed; doc comments |
| `internal/apps/books/library.go` | modify (Task 4) | `SaveEdit` replaces `UpdateWithCover` |
| `internal/apps/books/tag.go` | modify (Task 4) | `replaceTags`, shared by `SetTags` and `SaveEdit` |
| `internal/apps/books/form.go` | modify (Tasks 4–6) | tags on Edit; `Back`/`BackLabel`; `addCtx`; `editBookForm(b, …)`; `pickURL`'s list |
| `internal/apps/books/findcover.go` | modify (Task 6) | Find cover answers with what was typed |
| `internal/apps/books/books.go` | modify | the tags route removed; Find cover's body limit |
| `internal/apps/books/templates/form.html` | modify (Tasks 4–6) | Tags on Edit, the page bar, the list through the search, Find cover as `formaction` |
| `internal/apps/books/templates/home.partial.html` | modify (Task 5) | `add-link`, out of band |
| `internal/apps/books/static/books.js` | modify (Task 8) | focus kept in the format select, Esc and the drawer (#592), `a` |
| `internal/ui/toolbar_icons.go`, `internal/ui/toolbar_icons_test.go` | modify (Task 1) | `book-open`, `tablet`, `headphones` |
| `internal/ui/static/app.css` | modify (Task 7) | the sections, the entries' icons, the popovers, Add/Edit, #593, #594 |
| `PATTERNS.md` | modify (Task 7) | the lifted-summary section action and the hover-revealed entry icons |
| `docs/user/books.md`, `README.md`, `docs/screenshots/capture/shots.go`, `docs/user/images/*`, `docs/images/app-books-*.png` | modify (Task 10) | the guide and the pictures |
| `internal/apps/books/*_test.go` | modify/create | as each task says |

---
### Task 1: The Reading section

The reading box becomes the **Reading** section. The format changes from a pill menu to a native `<select>` with an icon. The progress row gets a small ✓ save button, and **Finish…** and **Did not finish…** sit at the end of that row. A progress update now swaps the whole section, so the Did not finish… popover always offers the page just saved.

**Files:**
- Modify: `internal/ui/toolbar_icons.go`, `internal/ui/toolbar_icons_test.go`
- Modify: `internal/apps/books/view.go` (`bookView`, `formatIcons`, `viewBook`, `progressView`, `viewProgress`)
- Modify: `internal/apps/books/handlers.go` (`renderBook`)
- Modify: `internal/apps/books/templates/book.partial.html`
- Test: `internal/apps/books/reading_box_test.go`, `internal/apps/books/progress_view_test.go`, `internal/apps/books/actions_test.go`

**Interfaces:**
- Consumes: UI1's `bookView`, `renderBook`, the `post-ctx` block and `dict`.
- Produces:
  - **Icons:** `ticon` names `book-open`, `tablet` and `headphones`.
  - **`bookView.FormatIcon string`**, the `ticon` name for the format, or `""` while it isn't set. `bookView.FormatLabel` is removed.
  - **Template blocks:**
    - `reading` takes `(dict "Book" bookView "OOB" bool)` and renders `section#books-reading`.
    - `reading-swap` is the htmx answer for target `#books-reading`.
    - `format-select`, `progress` and `close-reading` take a bookView.
  - **CSS classes for Task 7:** `books-section`, `books-section-head`, `books-reading-head`, `books-reading-since`, `books-format`, `books-format-control`, `books-format-save`, `books-progress`, `books-progress-bar`, `books-progress-row`, `books-progress-form`, `books-progress-of`, `books-progress-note`, `books-progress-save`, `books-reading-actions`, `books-close`, `books-close-form`, `books-small-btn`.

- [ ] **Step 1: Write the failing tests**

In `internal/ui/toolbar_icons_test.go`, in `TestToolbarIconForKnownNames`, add the three names to the end of the `names` list:

```go
		"arrow-down", "arrow-up", "grid", "list",
		"book-open", "tablet", "headphones",
```

Replace the whole of `internal/apps/books/reading_box_test.go` with:

```go
package books_test

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/iliafrenkel/on-suite/internal/htmlassert"
)

// formatIcon is the icon drawn before the format select: the markup
// between the control's opening tag and the select.
func formatIcon(body string) string {
	_, after, _ := strings.Cut(body, `class="books-format-control">`)
	icon, _, _ := strings.Cut(after, "<select")
	return strings.TrimSpace(icon)
}

func TestTheFormatSelectSetsTheReadingsFormat(t *testing.T) {
	s := newServer(t)
	id := readingBook(t, s, "Dune", 600)
	page := fmt.Sprintf("/books/b/%d?shelf=reading", id)
	doc := s.Get(t, s.Alice, page)
	doc.MustNotHave(".books-format-pill") // a control, not a tag (spec "Sections")
	form := doc.MustHave("form.books-format")
	for name, want := range map[string]string{
		"action": fmt.Sprintf("/books/format/%d", id), "hx-post": fmt.Sprintf("/books/format/%d", id),
		"hx-trigger": "change", "hx-target": "#books-book",
	} {
		if v, _ := htmlassert.Attr(form, name); v != want {
			t.Errorf("format form %s = %q, want %q", name, v, want)
		}
	}
	doc.MustHave("form.books-format noscript") // its Save button, without JavaScript
	var values []string
	for _, o := range doc.QueryAll("select#books-format option") {
		v, _ := htmlassert.Attr(o, "value")
		values = append(values, v)
	}
	if fmt.Sprint(values) != "[paper ebook audio ]" {
		t.Errorf("format options = %q", values)
	}
	if got := attr(t, doc, "select#books-format option[selected]", "value"); got != "" {
		t.Errorf("selected format = %q, want Not set", got)
	}
	if icon := formatIcon(s.Do(t, s.Alice, httptestGet(page)).Body.String()); icon != "" {
		t.Errorf("icon while the format isn't set = %q, want none", icon)
	}

	s.Submit(t, s.Alice, fmt.Sprintf("/books/format/%d", id), url.Values{"shelf": {"reading"}, "format": {"audio"}}, page)
	doc = s.Get(t, s.Alice, page)
	if got := attr(t, doc, "select#books-format option[selected]", "value"); got != "audio" {
		t.Errorf("selected format after choosing audio = %q", got)
	}
	if got := htmlassert.Text(doc.MustHave(".books-progress-of")); got != "%" {
		t.Errorf("progress unit after audio = %q, want %%", got)
	}

	if rec := s.Post(t, s.Alice, fmt.Sprintf("/books/format/%d", id), url.Values{"format": {"vinyl"}}); rec.Code != http.StatusBadRequest {
		t.Errorf("format vinyl = %d, want 400", rec.Code)
	}
	if rec := s.Post(t, s.Bob, fmt.Sprintf("/books/format/%d", id), url.Values{"format": {"paper"}}); rec.Code != http.StatusNotFound {
		t.Errorf("Bob's format = %d, want 404", rec.Code)
	}
}

func TestEachFormatHasItsOwnIcon(t *testing.T) {
	s := newServer(t)
	id := readingBook(t, s, "Dune", 600)
	seen := map[string]string{}
	for _, f := range []string{"paper", "ebook", "audio"} {
		if err := s.Store.SetFormat(context.Background(), s.Alice.User.ID, id, f); err != nil {
			t.Fatal(err)
		}
		icon := formatIcon(s.Do(t, s.Alice, httptestGet(fmt.Sprintf("/books/b/%d", id))).Body.String())
		if !strings.HasPrefix(icon, "<svg") {
			t.Errorf("%s: icon = %q, want an svg", f, icon)
		}
		if other, ok := seen[icon]; ok {
			t.Errorf("%s draws the same icon as %s", f, other)
		}
		seen[icon] = f
	}
}

func TestFinishOffersARatingAndDNFAPlace(t *testing.T) {
	s := newServer(t)
	id := readingBook(t, s, "Dune", 600)
	ctx := context.Background()
	if err := s.Store.SetRating(ctx, s.Alice.User.ID, id, 3); err != nil {
		t.Fatal(err)
	}
	if err := s.Store.RecordProgress(ctx, s.Alice.User.ID, id, 200); err != nil {
		t.Fatal(err)
	}
	doc := s.Get(t, s.Alice, fmt.Sprintf("/books/b/%d", id))
	if got := strings.Join(texts(doc, "details.books-close summary"), "|"); got != "Finish…|Did not finish…" {
		t.Errorf("close buttons = %q, want Finish… and Did not finish…", got)
	}
	if n := len(doc.QueryAll("select#books-finish-rating option")); n != 6 {
		t.Errorf("%d rating options, want No rating and 1–5", n)
	}
	if got := attr(t, doc, "select#books-finish-rating option[selected]", "value"); got != "3" {
		t.Errorf("selected rating = %q, want the book's 3", got)
	}
	for _, f := range doc.QueryAll("details.books-close form") {
		if _, ok := htmlassert.Attr(f, "novalidate"); !ok {
			t.Errorf("close form %q lacks novalidate", htmlassert.Text(f))
		}
	}
	at := doc.MustHave("input#books-dnf-at")
	if v, _ := htmlassert.Attr(at, "max"); v != "600" {
		t.Errorf("DNF page max = %q, want 600", v)
	}
	if v, _ := htmlassert.Attr(at, "value"); v != "200" {
		t.Errorf("DNF page = %q, want the current page 200", v)
	}
}
```

In `internal/apps/books/progress_view_test.go`:

1. In `TestTheProgressBoxIsInTheUnitOfTheReading`, after the line `t.Errorf("note = %q", got)` and its closing `}`, add:

```go
	if got := attr(t, doc, "form.books-progress-form", "hx-target"); got != "#books-reading" {
		t.Errorf("progress form hx-target = %q, want the whole Reading section", got)
	}
	if got := attr(t, doc, "button.books-progress-save", "aria-label"); got != "Save progress" {
		t.Errorf("save button aria-label = %q: the iOS number pad has no Enter, so it needs a named button", got)
	}
```

   Then, after the audio checks (just before `want := add(...)`), add:

```go
	s.Clock.Set(time.Date(2026, 10, 9, 1, 0, 0, 0, time.UTC))
	if err := s.Store.RecordProgress(context.Background(), s.Alice.User.ID, audio, 40); err != nil {
		t.Fatal(err)
	}
	doc = s.Get(t, s.Alice, fmt.Sprintf("/books/b/%d", audio))
	if got := htmlassert.Text(doc.MustHave(".books-progress-note")); got != "updated 9 Oct 2026" {
		t.Errorf("percent note = %q, want just the date: the box says the %% already", got)
	}
```

2. In `TestProgressOverHTMXSwapsTheBox`, change the target `"books-progress"` to `"books-reading"`. After `doc := htmlassert.Parse(t, rec.Body.String())`, add `doc.MustHave("section#books-reading")`.

3. In `TestBadProgressIsRefusedInsideTheBox`, change the target `"books-progress"` to `"books-reading"`.

4. Add a new test at the end of the file:

```go
func TestProgressSwapsTheWholeReadingSection(t *testing.T) {
	s := newServer(t)
	id := readingBook(t, s, "Dune", 600)
	rec := postHXTo(t, s, fmt.Sprintf("/books/progress/%d", id), "books-reading", url.Values{"shelf": {"reading"}, "at": {"212"}})
	if rec.Code != http.StatusOK {
		t.Fatalf("htmx progress = %d, want 200", rec.Code)
	}
	doc := htmlassert.Parse(t, rec.Body.String())
	doc.MustHave("section#books-reading")
	doc.MustNotHave("#books-book")
	if got := attr(t, doc, "input#books-dnf-at", "value"); got != "212" {
		t.Errorf("Did not finish… stops at %q, want the page just saved, 212", got)
	}
}
```

In `internal/apps/books/actions_test.go`, in `TestBookPaneOffersTheRightReadingActions`, after the first `doc := s.Get(...)` line (the want-to-read book), add:

```go
	doc.MustHave("section#books-reading .books-start") // the Reading section holds just the button
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/ui/... ./internal/apps/books/... -count=1 2>&1 | tail -40`

Expected: FAIL.
- The icon test fails on `book-open` ("want it to contain <svg").
- The Books tests fail with missing `form.books-format`, `select#books-format`, `input#books-dnf-at`, `section#books-reading` and `button.books-progress-save`, and the old pill and note text.

- [ ] **Step 3: Add the three icons**

In `internal/ui/toolbar_icons.go`, after the `"list"` entry, add:

```go
	"book-open": `<svg class="toolbar-icon" viewBox="0 0 24 24" aria-hidden="true">
		<path d="M3 5.5A1.5 1.5 0 0 1 4.5 4H9a3 3 0 0 1 3 3v13a2.5 2.5 0 0 0-2.5-2.5h-5A1.5 1.5 0 0 1 3 16z"/>
		<path d="M21 5.5A1.5 1.5 0 0 0 19.5 4H15a3 3 0 0 0-3 3v13a2.5 2.5 0 0 1 2.5-2.5h5a1.5 1.5 0 0 0 1.5-1.5z"/>
	</svg>`,
	"tablet": `<svg class="toolbar-icon" viewBox="0 0 24 24" aria-hidden="true">
		<rect x="5" y="2" width="14" height="20" rx="2"/>
		<path d="M11 18h2"/>
	</svg>`,
	"headphones": `<svg class="toolbar-icon" viewBox="0 0 24 24" aria-hidden="true">
		<path d="M3 18v-6a9 9 0 0 1 18 0v6"/>
		<path d="M21 19a2 2 0 0 1-2 2h-1a2 2 0 0 1-2-2v-3a2 2 0 0 1 2-2h3z"/>
		<path d="M3 19a2 2 0 0 0 2 2h1a2 2 0 0 0 2-2v-3a2 2 0 0 0-2-2H3z"/>
	</svg>`,
```

- [ ] **Step 4: Update the view model**

In `internal/apps/books/view.go`:

1. In `bookView`, replace these two lines:

```go
	FormatLabel   string       // its format, for the pill: "Paper", "Format not set"
	FormatChoices []choice     // the format menu
```

   with:

```go
	FormatIcon    string       // its format's icon (ticon); "" while the format isn't set
	FormatChoices []choice     // the format select
```

2. Directly after the `formatLabels` var, add:

```go
// formatIcons are drawn before the format select (UI redesign spec
// "Sections", decided 2026-10-12); a reading with no format has none.
var formatIcons = map[string]string{"paper": "book-open", "ebook": "tablet", "audio": "headphones"}
```

3. In `viewBook`, replace:

```go
		v.FormatLabel, v.FormatChoices = "Format not set", formatChoices(b.Latest.Format)
		if b.Latest.Format != "" {
			v.FormatLabel = formatLabels[b.Latest.Format]
		}
```

   with:

```go
		v.FormatIcon, v.FormatChoices = formatIcons[b.Latest.Format], formatChoices(b.Latest.Format)
```

4. In `progressView`, change the `Note` comment to `// "20% · updated 9 Oct 2026"; in percent just "updated 9 Oct 2026"`. In `viewProgress`, replace:

```go
	if b.Progress.Set() {
		v.Value = strconv.Itoa(b.Progress.In(u, b.Pages))
		v.Note = strconv.Itoa(v.Percent) + "% · updated " + b.Progress.RecordedAt.Local().Format("2 Jan 2006")
	}
```

   with:

```go
	if b.Progress.Set() {
		v.Value = strconv.Itoa(b.Progress.In(u, b.Pages))
		v.Note = "updated " + b.Progress.RecordedAt.Local().Format("2 Jan 2006")
		if u == UnitPage { // in percent, the box itself says it
			v.Note = strconv.Itoa(v.Percent) + "% · " + v.Note
		}
	}
```

- [ ] **Step 5: Send progress to the whole section**

In `internal/apps/books/handlers.go`, in `renderBook`:

- In the doc comment, replace `#books-progress → progress-swap` with `#books-reading → reading-swap (the whole Reading section, so Did not finish… offers the page just saved)`.
- In the `switch web.HTMXTarget(r)`, replace:

```go
		case "books-progress":
			block = "progress-swap"
```

  with:

```go
		case "books-reading":
			block = "reading-swap"
```

In `internal/apps/books/actions.go`, in the doc comment of `progress`, replace `htmx aims it at the progress box, so the answer is the box;` with `htmx aims it at the Reading section, so the answer is the section;`.

- [ ] **Step 6: Rewrite the reading blocks**

In `internal/apps/books/templates/book.partial.html`:

1. Replace:

```
{{/* progress-swap answers a progress update (target #books-progress). */}}
{{define "progress-swap"}}{{template "progress" .Book}}{{end}}
```

   with:

```
{{/* reading-swap answers a progress update (target #books-reading): the
     whole Reading section, so Did not finish… offers the page just saved. */}}
{{define "reading-swap"}}{{template "reading" (dict "Book" .Book "OOB" false)}}{{end}}
```

2. In `book-body`, replace `{{template "reading-box" .}}` with `{{template "reading" (dict "Book" . "OOB" false)}}`.

3. Replace everything from the line `{{/* reading-box is the current reading: Finish / Did not finish while one` down to, and including, the `{{end}}` that closes `{{define "close-reading"}}` with:

```
{{/* reading is the Reading section (UI redesign spec "Sections"). With a
     reading in progress it shows since when, the format, the progress row
     and Finish… / Did not finish…; otherwise just the button that starts
     a reading. Takes a dict: Book (a bookView) and OOB, true when it rides
     along with a rating (take-swap) so the Finish… popover's rating
     follows the stars. */}}
{{define "reading"}}{{with .Book}}
<section class="books-section books-reading" id="books-reading" aria-labelledby="books-reading-head"{{if $.OOB}} hx-swap-oob="true"{{end}}>
	<h2 class="books-section-head" id="books-reading-head">Reading</h2>
	{{if .Reading}}
	<div class="books-reading-head">
		{{with .StartedOn}}<span class="books-reading-since">Since {{.}}</span>{{end}}
		{{template "format-select" .}}
	</div>
	{{template "progress" .}}
	{{else}}
	<form method="post" action="/books/start/{{.ID}}" hx-post="/books/start/{{.ID}}" hx-target="#books-book" hx-swap="outerHTML">
		{{template "post-ctx" .}}
		<button type="submit" class="primary books-small-btn books-start">{{.StartLabel}}</button>
	</form>
	{{end}}
</section>
{{end}}{{end}}

{{/* format-select sets the format of the reading in progress: a native
     select, so it reads as a control rather than a tag, with the format's
     icon before it. It saves on change through htmx (the shelf pill and
     the progress unit can follow, so the answer is the page body); without
     JavaScript a small Save button shows. Takes a bookView. */}}
{{define "format-select"}}
<form class="books-format" method="post" action="/books/format/{{.ID}}"
      hx-post="/books/format/{{.ID}}" hx-trigger="change" hx-target="#books-book" hx-swap="outerHTML">
	{{template "post-ctx" .}}
	<label class="visually-hidden" for="books-format">Format</label>
	<span class="books-format-control">{{with .FormatIcon}}{{ticon .}}{{end}}<select id="books-format" name="format">
		{{range .FormatChoices}}<option value="{{.Value}}"{{if .Current}} selected{{end}}>{{.Label}}</option>{{end}}
	</select></span>
	<noscript><button type="submit" class="books-format-save">Save</button></noscript>
</form>
{{end}}

{{/* progress is a thin bar, then one row: "Page [212] of 482 · 44% ·
     updated 9 Oct 2026" with a small ✓ save button (the iOS number pad has
     no Enter), and Finish… / Did not finish… at its end. Enter saves too.
     htmx swaps the whole Reading section (reading-swap); without
     JavaScript it posts and comes back. Takes a bookView. */}}
{{define "progress"}}
<div class="books-progress" id="books-progress">
	<progress class="books-progress-bar" max="100" value="{{.Progress.Percent}}" aria-label="Progress">{{.Progress.Percent}}%</progress>
	<div class="books-progress-row">
		<form class="books-progress-form" novalidate method="post" action="/books/progress/{{.ID}}"
		      hx-post="/books/progress/{{.ID}}" hx-target="#books-reading" hx-swap="outerHTML">
			{{template "post-ctx" .}}
			<label for="books-progress-input">{{if eq .Progress.Unit "page"}}Page{{else}}Percent read{{end}}</label>
			<input id="books-progress-input" name="at" type="number" inputmode="numeric" min="0" max="{{.Progress.Max}}" value="{{.Progress.Value}}" required{{if .Progress.Error}} aria-invalid="true" aria-describedby="books-progress-error"{{end}}>
			<span class="books-progress-of">{{if eq .Progress.Unit "page"}}of {{.Progress.Max}}{{else}}%{{end}}</span>
			<span class="books-progress-note">{{.Progress.Note}}</span>
			<button type="submit" class="toolbar-btn books-icon-btn books-progress-save" aria-label="Save progress" title="Save progress">{{ticon "check"}}</button>
			{{with .Progress.Error}}<p class="books-field-error" id="books-progress-error" role="alert">{{.}}</p>{{end}}
		</form>
		<div class="books-reading-actions">
			{{template "close-reading" (dict "Book" . "Action" "finish" "Label" "Finish…" "DayLabel" "Finished on" "Submit" "Mark as read" "Primary" true)}}
			{{template "close-reading" (dict "Book" . "Action" "dnf" "Label" "Did not finish…" "DayLabel" "Stopped on" "Submit" "Put it down" "Primary" false)}}
		</div>
	</div>
</div>
{{end}}

{{/* close-reading is Finish… or Did not finish…: a small button opening a
     no-JS <details> popover (PATTERNS.md) that holds the date — today
     unless changed, never before the start — then an optional rating
     (Finish) or the place it stopped (Did not finish), and the button that
     sends it. */}}
{{define "close-reading"}}
<details class="books-close">
	<summary class="button books-small-btn{{if .Primary}} primary{{end}}">{{.Label}}</summary>
	<form class="books-close-form" novalidate method="post" action="/books/{{.Action}}/{{.Book.ID}}"
	      hx-post="/books/{{.Action}}/{{.Book.ID}}" hx-target="#books-book" hx-swap="outerHTML">
		{{template "post-ctx" .Book}}
		<label for="books-{{.Action}}-day">{{.DayLabel}}</label>
		<input id="books-{{.Action}}-day" name="day" type="date" value="{{.Book.Today}}" max="{{.Book.Today}}"{{with .Book.MinDay}} min="{{.}}"{{end}}>
		{{if eq .Action "finish"}}
		<label for="books-finish-rating">Rating (optional)</label>
		<select id="books-finish-rating" name="rating">
			<option value="">No rating</option>
			{{range .Book.RatingChoices}}<option value="{{.Value}}"{{if .Current}} selected{{end}}>{{.Label}}</option>{{end}}
		</select>
		{{else}}
		<label for="books-dnf-at">Stopped at {{if eq .Book.Progress.Unit "page"}}page{{else}}percent{{end}} (optional)</label>
		<input id="books-dnf-at" name="at" type="number" inputmode="numeric" min="0" max="{{.Book.Progress.Max}}" value="{{.Book.Progress.Value}}">
		{{end}}
		<button type="submit"{{if .Primary}} class="primary"{{end}}>{{.Submit}}</button>
	</form>
</details>
{{end}}
```

Then check that nothing still refers to the old blocks:

Run: `grep -n 'reading-box\|format-menu\|progress-swap\|FormatLabel\|books-format-pill' -r internal/apps/books`
Expected: no output.

- [ ] **Step 7: Run the tests to verify they pass**

Run: `go test ./internal/ui/... ./internal/apps/books/... -count=1`
Expected: PASS.

- [ ] **Step 8: Run the full check and commit**

```bash
gofmt -l . && go vet ./... && go run honnef.co/go/tools/cmd/staticcheck@v0.8.0 ./... && go test ./... -race -count=1
git add internal/ui/toolbar_icons.go internal/ui/toolbar_icons_test.go internal/apps/books
git commit -m "feat(books): the Reading section — format select, progress row, Finish… popovers"
```

---
### Task 2: Your take and About

The stars and the review become **Your take**, which swaps on its own. While a reading is in progress, a rating also brings the Reading section along out of band, so the Finish… popover's rating select shows the new stars rather than overwriting them. The description becomes **About**.

A refusal's banner (a review that's too long, say) sits at the top of the page body, outside every section. So whatever the form targeted, `renderBook` aims a banner answer at `#books-book` with `HX-Retarget`.

**Files:**
- Modify: `internal/apps/books/handlers.go` (`renderBook`)
- Modify: `internal/apps/books/templates/book.partial.html`
- Test: `internal/apps/books/rating_view_test.go`, `internal/apps/books/polish_test.go`

**Interfaces:**
- Consumes: the `reading` block with `(dict "Book" … "OOB" true)` from Task 1.
- Produces:
  - **Template blocks:**
    - `take` takes a bookView and renders `section#books-take`.
    - `take-swap` is the htmx answer for target `#books-take`.
    - `description` now renders `section.books-about`.
  - **The banner rule:** with `bookOpts.Banner` set, an htmx answer is always `book-swap`. When the request targeted anything but `books-book`, the answer carries `HX-Retarget: #books-book` and `HX-Reswap: outerHTML`.
  - **CSS classes for Task 7:** `books-take`, `books-review-text`, `books-review-edit`, `books-review-write`, `books-review-form`, `books-section-action`, `books-about`.

- [ ] **Step 1: Write the failing tests**

In `internal/apps/books/rating_view_test.go`, add `"strings"` to the imports, and replace `TestReviewIsWrittenAndShown` with:

```go
func TestReviewIsWrittenAndShown(t *testing.T) {
	s := newServer(t)
	id := add(t, s, s.Alice.User.ID, titled("Dune", "", books.ShelfRead))
	page := fmt.Sprintf("/books/b/%d?shelf=read", id)
	doc := s.Get(t, s.Alice, page)
	doc.MustNotHave(".books-review-text")
	if got := htmlassert.Text(doc.MustHave("#books-take-head")); got != "Your take" {
		t.Errorf("section heading = %q", got)
	}
	// With no review, the one opener is the quiet link under the stars and
	// the heading has no icon (decided 2026-10-12).
	if got := htmlassert.Text(doc.MustHave(".books-review-edit summary")); got != "Write a review" {
		t.Errorf("summary = %q", got)
	}
	doc.MustNotHave("#books-take .books-section-action")

	rec := postHXTo(t, s, fmt.Sprintf("/books/review/%d", id), "books-take",
		url.Values{"shelf": {"read"}, "review": {"So much **sand**.\n\nWorth it."}})
	if rec.Code != http.StatusOK {
		t.Fatalf("htmx review = %d, want 200", rec.Code)
	}
	doc = htmlassert.Parse(t, rec.Body.String())
	doc.MustHave("section#books-take")
	doc.MustNotHave("#books-book") // just its own section (spec "Swaps")
	if got := htmlassert.Text(doc.MustHave(".books-review-text strong")); got != "sand" {
		t.Errorf("bold = %q", got)
	}
	if n := len(doc.QueryAll(".books-review-text p")); n != 2 {
		t.Errorf("%d paragraphs, want 2", n)
	}
	// With a review, the opener is the ✎ in the heading.
	if got := attr(t, doc, ".books-review-edit summary", "aria-label"); got != "Edit review" {
		t.Errorf("summary aria-label = %q, want Edit review", got)
	}
	doc.MustHave("#books-take .books-section-action")
	doc.MustNotHave(".books-review-write")
	if got := htmlassert.Text(doc.MustHave("textarea#books-review-input")); got != "So much **sand**. Worth it." {
		t.Errorf("textarea = %q", got)
	}
	if got := attr(t, doc, "form.books-review-form", "hx-target"); got != "#books-take" {
		t.Errorf("review form hx-target = %q", got)
	}
	// Without JavaScript the form posts and comes back.
	s.Submit(t, s.Alice, fmt.Sprintf("/books/review/%d", id), url.Values{"shelf": {"read"}, "review": {""}}, page)
	s.Get(t, s.Alice, page).MustNotHave(".books-review-text")
	if rec := s.Post(t, s.Bob, fmt.Sprintf("/books/review/%d", id), url.Values{"review": {"x"}}); rec.Code != http.StatusNotFound {
		t.Errorf("Bob's review = %d, want 404", rec.Code)
	}
}

func TestRatingOverHTMXRefreshesTheFinishRating(t *testing.T) {
	s := newServer(t)
	id := readingBook(t, s, "Dune", 600)
	if err := s.Store.SetRating(context.Background(), s.Alice.User.ID, id, 3); err != nil {
		t.Fatal(err)
	}
	rec := postHXTo(t, s, fmt.Sprintf("/books/rating/%d", id), "books-take", url.Values{"shelf": {"reading"}, "rating": {"5"}})
	if rec.Code != http.StatusOK {
		t.Fatalf("htmx rating = %d, want 200", rec.Code)
	}
	doc := htmlassert.Parse(t, rec.Body.String())
	doc.MustHave("section#books-take")
	doc.MustNotHave("#books-book")
	if n := len(doc.QueryAll("#books-take .is-on")); n != 5 {
		t.Errorf("%d stars on, want 5", n)
	}
	if got := attr(t, doc, "section#books-reading", "hx-swap-oob"); got != "true" {
		t.Errorf("Reading section hx-swap-oob = %q, want it along out of band", got)
	}
	if got := attr(t, doc, "select#books-finish-rating option[selected]", "value"); got != "5" {
		t.Errorf("Finish… rating = %q, want the new 5: the old 3 would overwrite it on finishing", got)
	}

	read := add(t, s, s.Alice.User.ID, titled("Emma", "", books.ShelfRead))
	rec = postHXTo(t, s, fmt.Sprintf("/books/rating/%d", read), "books-take", url.Values{"shelf": {"read"}, "rating": {"4"}})
	htmlassert.Parse(t, rec.Body.String()).MustNotHave("#books-reading") // nothing to finish, nothing to refresh
}

func TestARefusedReviewShowsTheBannerOnThePage(t *testing.T) {
	s := newServer(t)
	id := add(t, s, s.Alice.User.ID, titled("Dune", "", books.ShelfRead))
	rec := postHXTo(t, s, fmt.Sprintf("/books/review/%d", id), "books-take",
		url.Values{"shelf": {"read"}, "review": {strings.Repeat("a", books.MaxReviewRunes+1)}})
	if rec.Code != http.StatusOK {
		t.Fatalf("htmx refused review = %d, want 200 so htmx swaps it", rec.Code)
	}
	if got := rec.Header().Get("HX-Retarget"); got != "#books-book" {
		t.Errorf("HX-Retarget = %q, want #books-book: the banner sits above the sections", got)
	}
	if got := rec.Header().Get("HX-Reswap"); got != "outerHTML" {
		t.Errorf("HX-Reswap = %q, want outerHTML", got)
	}
	htmlassert.Parse(t, rec.Body.String()).MustHave("#books-book .books-banner")
}
```

In `TestStarsSetAndClearTheRating`, after the first `doc.MustNotHave(".is-on")`, add:

```go
	if got := attr(t, doc, "form.books-rating", "hx-target"); got != "#books-take" {
		t.Errorf("rating form hx-target = %q, want Your take", got)
	}
```

In `internal/apps/books/polish_test.go`, in `TestLongDescriptionsFold`, after `doc.MustNotHave(".is-clamped")`, add:

```go
	if got := htmlassert.Text(doc.MustHave("section.books-about h2")); got != "About" {
		t.Errorf("description heading = %q, want About", got)
	}
	none := add(t, s, uid, titled("Bare", "", books.ShelfWant))
	s.Get(t, s.Alice, fmt.Sprintf("/books/b/%d", none)).MustNotHave(".books-about") // no description, no section
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/apps/books/ -run 'Review|Rating|Description' -count=1`
Expected: FAIL. The failures are missing `#books-take-head`, `section#books-take`, `HX-Retarget` and `section.books-about`, and an hx-target of `#books-book`.

- [ ] **Step 3: Choose the block, and retarget a banner**

In `internal/apps/books/handlers.go`, in `renderBook`:

Replace the doc comment's sentence that starts `An htmx request gets the block for what it targeted:` (down to `anything else → book-swap (#books-book).`) with:

```go
// An htmx request gets the block for what it targeted:
// #books-reading → reading-swap (the whole Reading section, so Did not
// finish… offers the page just saved), #books-take → take-swap,
// #books-notes → notes-swap, #books-quotes → quotes-swap, anything else
// → book-swap (#books-book). A refusal's banner tops the page body,
// outside every section, so it is always book-swap, retargeted there.
```

Then replace the block-choosing code:

```go
		block := "book-swap"
		switch web.HTMXTarget(r) {
		case "books-reading":
			block = "reading-swap"
		case "books-notes":
			block = "notes-swap"
		case "books-quotes":
			block = "quotes-swap"
		}
```

with:

```go
		block := "book-swap"
		switch target := web.HTMXTarget(r); {
		case opts.Banner != "":
			if target != "books-book" {
				w.Header().Set("HX-Retarget", "#books-book")
				w.Header().Set("HX-Reswap", "outerHTML")
			}
		case target == "books-reading":
			block = "reading-swap"
		case target == "books-take":
			block = "take-swap"
		case target == "books-notes":
			block = "notes-swap"
		case target == "books-quotes":
			block = "quotes-swap"
		}
```

- [ ] **Step 4: Rewrite the blocks**

In `internal/apps/books/templates/book.partial.html`:

1. Replace the comment above `{{define "book-swap"}}` with:

```
{{/* book-swap answers a change that moves the shelf pill or more than one
     section — start, finish, did not finish, the format, a reading edited
     or deleted — and a refusal's banner: the page body (target
     #books-book). */}}
```

2. After the `reading-swap` line, add:

```
{{/* take-swap answers a rating or a review (target #books-take). While a
     reading is in progress the Reading section rides along out of band:
     its Finish… popover offers the book's rating, which has just changed. */}}
{{define "take-swap"}}{{template "take" .Book}}{{if .Book.Reading}}{{template "reading" (dict "Book" .Book "OOB" true)}}{{end}}{{end}}
```

3. In `book-body`, replace:

```
	{{template "rating" .}}
	{{template "review" .}}
	{{template "description" .}}
```

   with:

```
	{{template "take" .}}
	{{template "description" .}}
```

4. Replace the whole `rating` block and its comment, and the whole `review` block and its comment, with:

```
{{/* rating is the book's stars: five submit buttons, so it works without
     JavaScript; the current rating's own star clears it. The stars wrap
     together, never one by one (#579). htmx swaps Your take (take-swap).
     Takes a bookView. */}}
{{define "rating"}}
<form class="books-rating" method="post" action="/books/rating/{{.ID}}"
      hx-post="/books/rating/{{.ID}}" hx-target="#books-take" hx-swap="outerHTML">
	{{template "post-ctx" .}}
	<span class="books-star-row">{{range .Stars}}<button type="submit" class="books-star{{if .On}} is-on{{end}}" name="rating" value="{{.Value}}" aria-label="{{.Label}}" title="{{.Label}}">{{if .On}}{{ticon "star-filled"}}{{else}}{{ticon "star-outline"}}{{end}}</button>{{end}}</span>
</form>
{{end}}

{{/* take is Your take (spec "Sections"): the stars, then the review and
     the no-JS <details> that writes it. It has one opener (decided
     2026-10-12): with a review, the ✎ in the heading (its summary, lifted
     there by CSS), and the form stands in for the review while it is open;
     with none yet, the quiet "Write a review" under the stars. htmx swaps
     just this section (take-swap). Takes a bookView. */}}
{{define "take"}}
<section class="books-section books-take" id="books-take" aria-labelledby="books-take-head">
	<h2 class="books-section-head" id="books-take-head">Your take</h2>
	{{template "rating" .}}
	{{with .ReviewHTML}}<div class="books-review-text">{{.}}</div>{{end}}
	<details class="books-review-edit">
		{{if .Review}}<summary class="toolbar-btn books-icon-btn books-section-action" aria-label="Edit review" title="Edit review">{{ticon "edit"}}</summary>
		{{else}}<summary class="books-review-write">Write a review</summary>{{end}}
		<form class="books-review-form" method="post" action="/books/review/{{.ID}}"
		      hx-post="/books/review/{{.ID}}" hx-target="#books-take" hx-swap="outerHTML">
			{{template "post-ctx" .}}
			<label for="books-review-input">Review</label>
			<textarea id="books-review-input" name="review" rows="8">{{.Review}}</textarea>
			<p class="books-hint">Markdown works: **bold**, *italic*, [a link](https://…). A blank line starts a new paragraph.</p>
			<button type="submit" class="primary">Save review</button>
		</form>
	</details>
</section>
{{end}}
```

5. Replace the whole `description` block and its comment with:

```
{{/* description is About: the Open Library description, held to four
     lines when it is long, with a More / Less toggle (#566) — the checkbox
     technique (PATTERNS.md), so it needs no JavaScript. Left out when there
     is no description (spec "Sections"). Takes a bookView. */}}
{{define "description"}}
{{with .Description}}
<section class="books-section books-about" aria-labelledby="books-about-head">
	<h2 class="books-section-head" id="books-about-head">About</h2>
	{{if $.DescriptionLong}}<input type="checkbox" id="books-description-more" class="visually-hidden books-description-toggle">{{end}}
	<p class="books-description-text{{if $.DescriptionLong}} is-clamped{{end}}">{{.}}</p>
	{{if $.DescriptionLong}}<label class="books-description-more" for="books-description-more"><span class="books-more-label">More</span><span class="books-less-label">Less</span></label>{{end}}
</section>
{{end}}
{{end}}
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go test ./internal/apps/books/... -count=1`
Expected: PASS.

- [ ] **Step 6: Run the full check and commit**

```bash
gofmt -l . && go vet ./... && go run honnef.co/go/tools/cmd/staticcheck@v0.8.0 ./... && go test ./... -race -count=1
git add internal/apps/books
git commit -m "feat(books): Your take and About sections, rating and review swap only their own"
```

---
### Task 3: Notes, Quotes and Reading history

These three sections share one look:
- **The heading** has a count. Notes and Quotes have a round **+** that opens the add form at the top of the section.
- **Each entry** has ✎ and 🗑 at its top right. ✎ turns the entry into its form in place, and 🗑 goes through the confirm dialog.
- **Reading history** is a `<details>`, collapsed by default. A form inside it posts `open=history`, so saving or deleting a reading doesn't fold the history away.

**Files:**
- Modify: `internal/apps/books/view.go` (`bookView.HistoryOpen`)
- Modify: `internal/apps/books/handlers.go` (`renderBook`)
- Modify: `internal/apps/books/templates/book.partial.html` (`notes`, `quotes`, `history`)
- Create: `internal/apps/books/details_test.go`
- Test: `internal/apps/books/notes_view_test.go`, `internal/apps/books/quotes_view_test.go`, `internal/apps/books/history_view_test.go`

**Interfaces:**
- Consumes: `readTwiceOverHTTP`, `historyIDs` (`history_view_test.go`), `texts` and `isOpen` (`notes_view_test.go`), `attr` (`search_test.go`).
- Produces:
  - **`bookView.HistoryOpen bool`**, set by `renderBook` from the posted `open=history`.
  - **`fullBook(t, s) int64`** in `details_test.go`, Alice's book with something in every section. Task 4 reuses it.
  - **CSS classes for Task 7:** `books-section-count`, `books-section-add`, `books-entry-add`, `books-entries`, `books-entry`, `books-entry-edit`, `books-entry-delete`, `books-entry-icon`, `is-danger`, `books-history`, `books-history-summary`, `books-reading-edit-form`.

- [ ] **Step 1: Write the failing tests**

Create `internal/apps/books/details_test.go`:

```go
package books_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/iliafrenkel/on-suite/internal/apps/books"
	"github.com/iliafrenkel/on-suite/internal/htmlassert"
)

// fullBook is Alice's book with something in every section of its page: a
// reading in progress and an earlier one, a description, a rating and a
// review, a note and a quote.
func fullBook(t *testing.T, s *server) int64 {
	t.Helper()
	ctx := context.Background()
	uid := s.Alice.User.ID
	id := readTwiceOverHTTP(t, s)
	b, err := s.Store.Get(ctx, uid, id)
	if err != nil {
		t.Fatal(err)
	}
	in := b.BookInput
	in.Description = "Spice and sand."
	if err := s.Store.Update(ctx, uid, id, in); err != nil {
		t.Fatal(err)
	}
	if err := s.Store.SetRating(ctx, uid, id, 4); err != nil {
		t.Fatal(err)
	}
	if err := s.Store.SetReview(ctx, uid, id, "So much sand."); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Store.AddNote(ctx, uid, id, books.NoteInput{Body: "A note."}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Store.AddQuote(ctx, uid, id, books.QuoteInput{Text: "A quote."}); err != nil {
		t.Fatal(err)
	}
	return id
}

func TestDetailsSectionsComeInSpecOrder(t *testing.T) {
	s := newServer(t)
	doc := s.Get(t, s.Alice, fmt.Sprintf("/books/b/%d", fullBook(t, s)))
	got := texts(doc, ".books-section-head")
	want := []string{"Reading", "Your take", "About", "Notes · 1", "Quotes · 1", "Reading history · 2"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("section headings = %q, want %q (spec \"Sections\")", got, want)
	}
	if isOpen(doc, "details#books-history") {
		t.Error("the reading history is open on a plain page load; want it collapsed")
	}
}

func TestDetailsIconButtonsHaveNames(t *testing.T) {
	s := newServer(t)
	doc := s.Get(t, s.Alice, fmt.Sprintf("/books/b/%d", fullBook(t, s)))
	icons := doc.QueryAll(".books-icon-btn")
	// The page bar's two, Save progress, Edit review, + / ✎ / 🗑 for the note
	// and for the quote, and ✎ / 🗑 for each of the two readings.
	if len(icons) != 14 {
		t.Errorf("%d icon buttons, want 14", len(icons))
	}
	for _, n := range icons {
		label, _ := htmlassert.Attr(n, "aria-label")
		title, _ := htmlassert.Attr(n, "title")
		if label == "" || title != label {
			t.Errorf("icon <%s> has aria-label %q and title %q; want a name, shown as its tooltip too", n.Data, label, title)
		}
	}
}
```

In `internal/apps/books/notes_view_test.go`, in `TestTheBookPaneShowsNotesNewestFirst`:

- After the `isOpen(doc, "details#books-note-new")` check, add:

```go
	if got := htmlassert.Text(doc.MustHave("#books-notes-head")); got != "Notes · 2" {
		t.Errorf("notes heading = %q, want the count", got)
	}
	if got := attr(t, doc, "details#books-note-new summary", "aria-label"); got != "Add a note" {
		t.Errorf("+ aria-label = %q", got)
	}
	if n := len(doc.QueryAll(".books-note .books-entry-edit summary")); n != 2 {
		t.Errorf("%d ✎ buttons, want one per note", n)
	}
```

- At the end, after `doc.MustNotHave(".books-note")`, add:

```go
	if got := htmlassert.Text(doc.MustHave("#books-notes-head")); got != "Notes" {
		t.Errorf("notes heading with none = %q, want no count", got)
	}
```

In `internal/apps/books/quotes_view_test.go`, in `TestQuotesShowAsCards`, after the `doc.MustHave(".books-quote button[hx-confirm]")` line, add:

```go
	if got := htmlassert.Text(doc.MustHave("#books-quotes-head")); got != "Quotes · 2" {
		t.Errorf("quotes heading = %q, want the count", got)
	}
	if got := attr(t, doc, "details#books-quote-new summary", "aria-label"); got != "Add a quote" {
		t.Errorf("+ aria-label = %q", got)
	}
```

In `internal/apps/books/history_view_test.go`:

- In `TestTheBookPaneListsEveryReading`, after the `.books-history-item button[hx-confirm]` check, add:

```go
	if got := htmlassert.Text(doc.MustHave("#books-history-head")); got != "Reading history · 2" {
		t.Errorf("history heading = %q", got)
	}
```

- Add a new test at the end of the file:

```go
func TestEditingAReadingKeepsTheHistoryOpen(t *testing.T) {
	s := newServer(t)
	id := readTwiceOverHTTP(t, s)
	past := historyIDs(t, s, id)[1]
	path := fmt.Sprintf("/books/readings/%d/%d", id, past)
	doc := s.Get(t, s.Alice, fmt.Sprintf("/books/b/%d", id))
	for _, sel := range []string{
		fmt.Sprintf(`form[action="%s"] input[name="open"]`, path),
		fmt.Sprintf(`form[action="%s/delete"] input[name="open"]`, path),
	} {
		if got := attr(t, doc, sel, "value"); got != "history" {
			t.Errorf("%s = %q, want history", sel, got)
		}
	}
	rec := s.PostHX(t, s.Alice, path, url.Values{"shelf": {"read"}, "open": {"history"},
		"started_on": {"2026-09-20"}, "finished_on": {"2026-09-28"}, "format": {"paper"}})
	if !isOpen(htmlassert.Parse(t, rec.Body.String()), "details#books-history") {
		t.Error("the history folded away after saving one of its readings")
	}
	rec = s.PostHX(t, s.Alice, fmt.Sprintf("/books/format/%d", id), url.Values{"shelf": {"read"}, "format": {"ebook"}})
	if isOpen(htmlassert.Parse(t, rec.Body.String()), "details#books-history") {
		t.Error("the history opened for a change made outside it")
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/apps/books/ -run 'Details|Notes|Quotes|Reading|History' -count=1`
Expected: FAIL. The headings have no counts, the `+` summaries have no `aria-label`, the icon count is wrong, `details#books-history` is missing, and there's no `open` field.

- [ ] **Step 3: Carry the history's open state**

In `internal/apps/books/view.go`, in `bookView`, after the `History []historyView` line, add:

```go
	HistoryOpen   bool          // the history disclosure starts open: the request came from inside it
```

In `internal/apps/books/handlers.go`, in `renderBook`, after `bv.History = viewHistory(rs)`, add:

```go
	// A history form posts open=history, so saving or deleting a reading
	// doesn't fold the history away (it is collapsed by default).
	bv.HistoryOpen = r.PostFormValue("open") == "history"
```

- [ ] **Step 4: Rewrite the three sections**

In `internal/apps/books/templates/book.partial.html`, replace the whole `notes` block and its comment with:

```
{{/* notes is Notes · n, newest first (spec "Sections"). The + in the
     heading (its summary, lifted there by CSS) opens the add form at the
     top of the section. Each note shows its date and page, with ✎ / 🗑 at
     its top right: ✎ turns the note into its form in place, 🗑 goes through
     the confirm dialog. Every form works without JavaScript; htmx swaps
     just this section (notes-swap). Takes a bookView. */}}
{{define "notes"}}
<section class="books-section books-notes" id="books-notes" aria-labelledby="books-notes-head">
	<h2 class="books-section-head" id="books-notes-head">Notes{{with .Notes}} <span class="books-section-count">· {{len .}}</span>{{end}}</h2>
	<details class="books-entry-add" id="books-note-new"{{if .NewNote.Open}} open{{end}}>
		<summary class="toolbar-btn books-icon-btn books-section-action books-section-add" aria-label="Add a note" title="Add a note">{{ticon "plus"}}</summary>
		{{template "note-form" (dict "Book" . "Key" "new" "Action" (printf "/books/notes/%d" .ID) "Form" .NewNote "Submit" "Add note")}}
	</details>
	{{with .Notes}}
	<ul class="books-entries">
		{{range .}}
		<li class="books-entry books-note">
			<p class="books-entry-meta">{{.Date}}{{with .Page}} · {{.}}{{end}}</p>
			<div class="books-entry-text">{{.HTML}}</div>
			<details class="books-entry-edit"{{if .Form.Open}} open{{end}}>
				<summary class="toolbar-btn books-icon-btn books-entry-icon" aria-label="Edit note" title="Edit note">{{ticon "edit"}}</summary>
				{{template "note-form" (dict "Book" $ "Key" .ID "Action" (printf "/books/notes/%d/%d" $.ID .ID) "Form" .Form "Submit" "Save note")}}
			</details>
			<form class="books-entry-delete" method="post" action="/books/notes/{{$.ID}}/{{.ID}}/delete">
				{{template "post-ctx" $}}
				<button type="submit" class="toolbar-btn books-icon-btn books-entry-icon is-danger" aria-label="Delete note" title="Delete note"
				        hx-post="/books/notes/{{$.ID}}/{{.ID}}/delete" hx-target="#books-notes" hx-swap="outerHTML"
				        hx-confirm="Delete this note?">{{ticon "trash"}}</button>
			</form>
		</li>
		{{end}}
	</ul>
	{{end}}
</section>
{{end}}
```

Replace the whole `quotes` block and its comment with:

```
{{/* quotes is Quotes · n, newest first, in the same pattern as the notes:
     each quote keeps its blockquote with its line breaks, then its page and
     its comment. htmx swaps just this section (quotes-swap). Takes a
     bookView. */}}
{{define "quotes"}}
<section class="books-section books-quotes" id="books-quotes" aria-labelledby="books-quotes-head">
	<h2 class="books-section-head" id="books-quotes-head">Quotes{{with .Quotes}} <span class="books-section-count">· {{len .}}</span>{{end}}</h2>
	<details class="books-entry-add" id="books-quote-new"{{if .NewQuote.Open}} open{{end}}>
		<summary class="toolbar-btn books-icon-btn books-section-action books-section-add" aria-label="Add a quote" title="Add a quote">{{ticon "plus"}}</summary>
		{{template "quote-form" (dict "Book" . "Key" "new" "Action" (printf "/books/quotes/%d" .ID) "Form" .NewQuote "Submit" "Add quote")}}
	</details>
	{{with .Quotes}}
	<ul class="books-entries">
		{{range .}}
		<li class="books-entry books-quote">
			<blockquote class="books-quote-text">{{.Text}}</blockquote>
			{{with .Page}}<p class="books-entry-meta">{{.}}</p>{{end}}
			{{with .CommentHTML}}<div class="books-entry-text books-quote-comment">{{.}}</div>{{end}}
			<details class="books-entry-edit"{{if .Form.Open}} open{{end}}>
				<summary class="toolbar-btn books-icon-btn books-entry-icon" aria-label="Edit quote" title="Edit quote">{{ticon "edit"}}</summary>
				{{template "quote-form" (dict "Book" $ "Key" .ID "Action" (printf "/books/quotes/%d/%d" $.ID .ID) "Form" .Form "Submit" "Save quote")}}
			</details>
			<form class="books-entry-delete" method="post" action="/books/quotes/{{$.ID}}/{{.ID}}/delete">
				{{template "post-ctx" $}}
				<button type="submit" class="toolbar-btn books-icon-btn books-entry-icon is-danger" aria-label="Delete quote" title="Delete quote"
				        hx-post="/books/quotes/{{$.ID}}/{{.ID}}/delete" hx-target="#books-quotes" hx-swap="outerHTML"
				        hx-confirm="Delete this quote?">{{ticon "trash"}}</button>
			</form>
		</li>
		{{end}}
	</ul>
	{{end}}
</section>
{{end}}
```

Replace the whole `history` block and its comment with:

```
{{/* history is Reading history · n, newest first (spec "Sections"): a
     <details>, collapsed unless the request came from one of its own forms
     (HistoryOpen, from their open=history field). Each reading has ✎ (its
     dates and format, in place) and 🗑 (decided 2026-10-09: a reading's
     status isn't editable; deleting it is how a wrong one is put right).
     Left out for a book never started. Takes a bookView. */}}
{{define "history"}}
{{with .History}}
<details class="books-section books-history" id="books-history"{{if $.HistoryOpen}} open{{end}}>
	<summary class="books-history-summary"><h2 class="books-section-head" id="books-history-head">Reading history <span class="books-section-count">· {{len .}}</span></h2></summary>
	<ul class="books-entries">
		{{range .}}
		<li class="books-entry books-history-item">
			<p class="books-history-line"><span class="books-pill">{{.Status}}</span> {{.Dates}}{{with .Format}} · {{.}}{{end}}</p>
			<details class="books-entry-edit">
				<summary class="toolbar-btn books-icon-btn books-entry-icon" aria-label="Edit reading" title="Edit reading">{{ticon "edit"}}</summary>
				<form class="books-reading-edit-form" method="post" action="/books/readings/{{$.ID}}/{{.ID}}"
				      hx-post="/books/readings/{{$.ID}}/{{.ID}}" hx-target="#books-book" hx-swap="outerHTML">
					{{template "post-ctx" $}}
					<input type="hidden" name="open" value="history">
					<label for="books-r{{.ID}}-started">Started on</label>
					<input id="books-r{{.ID}}-started" name="started_on" type="date" value="{{.StartedOn}}" max="{{$.Today}}">
					{{if not .InProgress}}
					<label for="books-r{{.ID}}-finished">{{.FinishLabel}}</label>
					<input id="books-r{{.ID}}-finished" name="finished_on" type="date" value="{{.FinishedOn}}" max="{{$.Today}}">
					{{end}}
					<label for="books-r{{.ID}}-format">Format</label>
					<select id="books-r{{.ID}}-format" name="format">
						{{range .Formats}}<option value="{{.Value}}"{{if .Current}} selected{{end}}>{{.Label}}</option>{{end}}
					</select>
					<button type="submit" class="primary">Save</button>
				</form>
			</details>
			<form class="books-entry-delete" method="post" action="/books/readings/{{$.ID}}/{{.ID}}/delete">
				{{template "post-ctx" $}}
				<input type="hidden" name="open" value="history">
				<button type="submit" class="toolbar-btn books-icon-btn books-entry-icon is-danger" aria-label="Delete reading" title="Delete reading"
				        hx-post="/books/readings/{{$.ID}}/{{.ID}}/delete" hx-target="#books-book" hx-swap="outerHTML"
				        hx-confirm="Delete this reading? Its progress goes with it.">{{ticon "trash"}}</button>
			</form>
		</li>
		{{end}}
	</ul>
</details>
{{end}}
{{end}}
```

Then check that nothing still uses the old classes:

Run: `grep -rn 'books-entry-actions\|books-history-actions\|books-reading-edit"\|books-history-list\|books-entry-add > summary' internal/apps/books/templates`
Expected: no output.

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go test ./internal/apps/books/... -count=1`
Expected: PASS. The existing tests use the same ids and classes as before: `.books-note button[hx-confirm]`, `.books-history-item button[hx-confirm]`, `#books-note-new`, `.books-entry-edit` and `.books-history-line`.

- [ ] **Step 6: Run the full check and commit**

```bash
gofmt -l . && go vet ./... && go run honnef.co/go/tools/cmd/staticcheck@v0.8.0 ./... && go test ./... -race -count=1
git add internal/apps/books
git commit -m "feat(books): Notes, Quotes and Reading history sections with ✎ and 🗑 on each entry"
```

---
### Task 4: Tags move to the Edit page

Tags are a fact about the book, so they are now changed on Edit (spec "Add and Edit pages"). The changes:
- **Store:** `Store.SaveEdit` replaces `UpdateWithCover` and saves the details, the cover change and the tags in one transaction.
- **Removed:** the book page's tags form, `POST /books/tags/{id}` and `setTags`.
- **Header:** the header's tag pills become links to each tag's list.

**Files:**
- Modify: `internal/apps/books/tag.go` (`replaceTags`, `SetTags`)
- Modify: `internal/apps/books/library.go` (`SaveEdit` replaces `UpdateWithCover`)
- Modify: `internal/apps/books/form.go` (`formValues` comment, `editForm`, `update`)
- Modify: `internal/apps/books/view.go` (`bookView.Tags`, `TagsValue`, `tagLinks`, `viewBook`)
- Modify: `internal/apps/books/books.go`, `internal/apps/books/actions.go`
- Modify: `internal/apps/books/templates/form.html`, `internal/apps/books/templates/book.partial.html`
- Test: `internal/apps/books/cover_test.go`, `internal/apps/books/form_test.go`, `internal/apps/books/details_test.go`, `internal/apps/books/actions_test.go`, `internal/apps/books/review_gaps_b1a_test.go`, `internal/apps/books/series_test.go`

**Interfaces:**
- Consumes: `fullBook` (Task 3), and `linkTags`, `cleanTags` and `gcTags` (`tag.go`).
- Produces:
  - `func (*Store) SaveEdit(ctx, userID, id int64, in BookInput, ch CoverChange, tags []string) error`. The tags replace the book's tags, and nil or empty clears them.
  - `func replaceTags(ctx, tx *sql.Tx, userID, id int64, names []string) error`
  - `bookView.Tags []tagLink`, built by `func tagLinks(names []string) []tagLink`
  - The Edit page's `input#books-tags` (name `tags`)

- [ ] **Step 1: Write the failing tests**

In `internal/apps/books/cover_test.go`:

- Rename `TestUpdateWithCoverSavesBothTogether` to `TestSaveEditSavesBothTogether` and `TestUpdateWithCoverIsAllOrNothing` to `TestSaveEditIsAllOrNothing`.
- In both, change every `f.store.UpdateWithCover(` call to `f.store.SaveEdit(`, and add a last argument `nil` (no tags) to each. There are five calls.
- Exception: in the first call of `TestSaveEditIsAllOrNothing`, the one the trigger makes fail, pass `[]string{"new"}` instead of `nil`.
- In that test, change the message `"UpdateWithCover succeeded, want the cover failure"` to `"SaveEdit succeeded, want the cover failure"`. After the `b.Title != "Piranesi"` check, add:

```go
	if tags, _ := f.store.BookTags(ctx, f.alice.ID, id); len(tags) != 0 {
		t.Errorf("tags = %v after a failed cover write, want none: the tags are in the same transaction", tags)
	}
```

In `internal/apps/books/form_test.go`, add these tests (the file already imports `context`, `slices` and `url`):

```go
func TestEditSavesTags(t *testing.T) {
	s := newServer(t)
	nb := titled("Dune", "", books.ShelfWant)
	nb.Tags = []string{"sf"}
	id := add(t, s, s.Alice.User.ID, nb)
	doc := s.Get(t, s.Alice, fmt.Sprintf("/books/edit/%d?shelf=want", id))
	if got := attr(t, doc, "input#books-tags", "value"); got != "sf" {
		t.Errorf("tags box = %q, want the book's tags", got)
	}
	edit := func(tags string) []string {
		t.Helper()
		s.Submit(t, s.Alice, fmt.Sprintf("/books/edit/%d", id), url.Values{"title": {"Dune"}, "shelf": {"want"}, "tags": {tags}},
			fmt.Sprintf("/books/b/%d?shelf=want", id))
		got, err := s.Store.BookTags(context.Background(), s.Alice.User.ID, id)
		if err != nil {
			t.Fatal(err)
		}
		return got
	}
	if got := edit("Space, sf, classics"); !slices.Equal(got, []string{"classics", "sf", "space"}) {
		t.Errorf("tags = %v, want [classics sf space]", got)
	}
	if got := edit("  "); len(got) != 0 {
		t.Errorf("tags after clearing the box = %v, want none", got)
	}
	if names, _ := s.Store.TagNames(context.Background(), s.Alice.User.ID); len(names) != 0 {
		t.Errorf("tag names = %v, want none: a tag nobody uses goes", names)
	}
}

func TestEditLeavesTheShelfAlone(t *testing.T) {
	s := newServer(t)
	id := add(t, s, s.Alice.User.ID, titled("Dune", "", books.ShelfReading))
	s.Submit(t, s.Alice, fmt.Sprintf("/books/edit/%d", id), url.Values{
		"title": {"Dune, revised"}, "shelf": {"reading"}, "tags": {"sf"},
		"add_to": {"read"}, // a tampered form: the edit form has no such field
	}, fmt.Sprintf("/books/b/%d?shelf=reading", id))
	b, err := s.Store.Get(context.Background(), s.Alice.User.ID, id)
	if err != nil {
		t.Fatal(err)
	}
	if b.Title != "Dune, revised" || !slices.Equal(b.Tags, []string{"sf"}) {
		t.Fatalf("title %q, tags %v; the edit was not saved", b.Title, b.Tags)
	}
	if b.Shelf != books.ShelfReading {
		t.Errorf("shelf = %q, want reading unchanged", b.Shelf)
	}
}
```

In `internal/apps/books/details_test.go`, add `"net/http"` and `"net/url"` to the imports and add:

```go
func TestTheBookPageHasNoTagsForm(t *testing.T) {
	s := newServer(t)
	nb := titled("Dune", "", books.ShelfWant)
	nb.Tags = []string{"book club", "sf"}
	id := add(t, s, s.Alice.User.ID, nb)
	doc := s.Get(t, s.Alice, fmt.Sprintf("/books/b/%d?shelf=want", id))
	doc.MustNotHave(".books-tags-form")
	doc.MustNotHave(fmt.Sprintf(`form[action="/books/tags/%d"]`, id))
	var hrefs []string
	for _, a := range doc.QueryAll("a.books-pill-link") {
		href, _ := htmlassert.Attr(a, "href")
		hrefs = append(hrefs, htmlassert.Text(a)+" "+href)
	}
	if want := "book club /books/?shelf=all&tag=book+club|sf /books/?shelf=all&tag=sf"; strings.Join(hrefs, "|") != want {
		t.Errorf("tag pills = %q, want %q: each links to its list", hrefs, want)
	}
	if rec := s.Post(t, s.Alice, fmt.Sprintf("/books/tags/%d", id), url.Values{"tags": {"x"}}); rec.Code != http.StatusNotFound {
		t.Errorf("POST /books/tags = %d, want 404: tags are edited on the Edit page now", rec.Code)
	}
}
```

- [ ] **Step 2: Remove the tests of the old route**

- In `internal/apps/books/actions_test.go`, delete `TestSetTagsFromTheBookPane`. In `TestActionsOnSomeoneElsesBookAreNotFound`, change the action list to `[]string{"start", "finish", "dnf", "delete"}`.
- In `internal/apps/books/review_gaps_b1a_test.go`, delete `TestSetTagsOverHTMX` and `TestEditLeavesTagsAndShelfAlone`. `TestEditLeavesTheShelfAlone` above replaces the second. Remove any import the file no longer uses; `go vet` names it.
- In `internal/apps/books/series_test.go`, in `TestTheSeriesLinkFiltersTheList`, the last check reads the series field from the tags form. Point it at the page bar's delete form, which carries the same context:

```go
	if v, _ := htmlassert.Attr(doc.MustHave(`.books-page-bar input[name="series"]`), "value"); v != "The Expanse" {
		t.Errorf("page bar series field = %q", v)
	}
```

- [ ] **Step 3: Run the tests to verify they fail**

Run: `go test ./internal/apps/books/ -count=1 2>&1 | tail -30`
Expected: FAIL to compile, because `f.store.SaveEdit` is undefined.

- [ ] **Step 4: Save the tags with the edit**

In `internal/apps/books/tag.go`, replace `SetTags` with:

```go
// SetTags replaces a book's tags with names (cleaned here).
func (st *Store) SetTags(ctx context.Context, userID, id int64, names []string) error {
	tx, err := st.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("books: begin set tags: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if err := st.touch(ctx, tx, userID, id); err != nil {
		return err
	}
	if err := replaceTags(ctx, tx, userID, id, names); err != nil {
		return err
	}
	return tx.Commit()
}

// replaceTags makes names (cleaned here) the book's only tags and drops
// every tag of userID's that no book uses any more. The caller has checked
// the book is userID's.
func replaceTags(ctx context.Context, tx *sql.Tx, userID, id int64, names []string) error {
	if _, err := tx.ExecContext(ctx, `DELETE FROM books_book_tags WHERE book_id = ?`, id); err != nil {
		return fmt.Errorf("books: clear tags: %w", err)
	}
	if err := linkTags(ctx, tx, userID, id, cleanTags(names)); err != nil {
		return err
	}
	return gcTags(ctx, tx, userID)
}
```

In `internal/apps/books/library.go`, replace `UpdateWithCover` and its doc comment with:

```go
// SaveEdit saves the Edit page (UI redesign spec "Add and Edit pages"):
// the details, a cover change (a new image, a removal or nothing) and the
// tags, in one transaction, so if any part can't be written none is. The
// tags replace the book's; none clears them. The image is already in hand;
// nothing here fetches.
func (st *Store) SaveEdit(ctx context.Context, userID, id int64, in BookInput, ch CoverChange, tags []string) error {
	if ch.Source != "" && !validCoverSource(ch.Source) {
		return ErrInvalid
	}
	tx, err := st.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("books: begin update: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if err := st.updateBook(ctx, tx, userID, id, in); err != nil {
		return err
	}
	switch {
	case ch.Source != "":
		err = st.setCoverTx(ctx, tx, id, ch.ContentType, ch.Bytes, ch.Source)
	case ch.Remove:
		err = st.removeCoverTx(ctx, tx, userID, id)
	}
	if err != nil {
		return err
	}
	if err := replaceTags(ctx, tx, userID, id, tags); err != nil {
		return err
	}
	return tx.Commit()
}
```

`updateBook` checks ownership first and returns `ErrNotFound` for someone else's book, so `replaceTags` never touches another user's rows. Keep the rest of `library.go` as it is.

In `internal/apps/books/form.go`:

1. Replace the `formValues` doc comment with:

```go
// formValues is what the book form shows: the raw text, so a mistyped
// number comes back exactly as typed. AddTo and FinishedOn are for a new
// book only; so are OLWork, OLEdition and CoverID, which carry an Open
// Library pick through the form in hidden fields. Tags are on both forms
// (UI redesign spec "Add and Edit pages").
```

2. In `editForm`, replace:

```go
	v := editBookForm(id, valuesOf(b.BookInput), nil, ctxFrom(r.FormValue), coverURL(b.ID, b.CoverVersion))
```

   with:

```go
	vals := valuesOf(b.BookInput)
	vals.Tags = strings.Join(b.Tags, ", ")
	v := editBookForm(id, vals, nil, ctxFrom(r.FormValue), coverURL(b.ID, b.CoverVersion))
```

3. In `update`, replace `a.store.UpdateWithCover(r.Context(), uid, id, in, change)` with `a.store.SaveEdit(r.Context(), uid, id, in, change, ParseTags(vals.Tags))`.

- [ ] **Step 5: Remove the tags route and form, and link the pills**

- In `internal/apps/books/books.go`, delete `r.HandleFunc("POST /tags/{id}", a.act(a.setTags, false))`.
- In `internal/apps/books/actions.go`, delete `func (a *App) setTags`.

In `internal/apps/books/view.go`:

1. In `bookView`, replace:

```go
	Tags                             []string
	TagsValue                        string // the tags box: "classics, sf"
```

   with:

```go
	Tags                             []tagLink // the header's pills, each its tag's list
```

2. In `viewBook`, replace `Tags: b.Tags, TagsValue: strings.Join(b.Tags, ", "),` with `Tags: tagLinks(b.Tags),`.

3. After `viewSidebar`, add:

```go
// tagLinks are a book's header pills: read-only, each a link to every book
// with that tag (UI redesign spec "Header"). Tags are edited on the Edit
// page.
func tagLinks(names []string) []tagLink {
	var out []tagLink
	for _, n := range names {
		out = append(out, tagLink{Name: n, URL: listCtx{Shelf: ShelfAll, Tag: n}.ListURL()})
	}
	return out
}
```

In `internal/apps/books/templates/book.partial.html`:

1. In `book-body`, replace the pills line with:

```
			<p class="books-pills"><span class="books-pill books-pill-shelf">{{.ShelfLabel}}</span>{{range .Tags}} <a class="books-pill books-pill-link" href="{{.URL}}">{{.Name}}</a>{{end}}</p>
```

2. Delete the line `{{template "tags-form" .}}` from `book-body`. Delete the whole `tags-form` block and its comment.

In `internal/apps/books/templates/form.html`, directly after the Description `<div class="field">…</div>`, add:

```
		{{if not $d.New}}{{template "text-field" (dict "Name" "tags" "Label" "Tags" "Placeholder" "sf, favourites" "Value" $d.Values.Tags "Error" "")}}{{end}}
```

The Add page keeps its own Tags field where it is, after "Add it to" (spec: "Add is unchanged apart from the page bar"). Only one of the two is ever drawn.

Then check that nothing is left of the old route or form:

Run: `grep -rn 'setTags\|tags-form\|TagsValue\|UpdateWithCover\|/books/tags' internal docs/screenshots`
Expected: no output.

- [ ] **Step 6: Run the tests to verify they pass**

Run: `go test ./internal/apps/books/... -count=1`
Expected: PASS.

- [ ] **Step 7: Run the full check and commit**

```bash
gofmt -l . && go vet ./... && go run honnef.co/go/tools/cmd/staticcheck@v0.8.0 ./... && go test ./... -race -count=1
git add internal/apps/books
git commit -m "feat(books): edit tags on the Edit page; the header's tag pills link to their lists"
```

---
### Task 5: A page bar on Add and Edit, and Add keeps its list

Both form pages get the book page's page bar, with a back link and no right-hand actions (spec "Add and Edit pages"):
- **Edit** shows "← *Book title*", back to the book page with its list context.
- **Add** shows "← *list heading*", back to the list it was opened from.
- **Cancel** goes to the same place on both.

So **+ Add book** now carries the list. It's swapped out of band with every list change, so it never points at a stale list. The list's search is left behind: on the Add page, `q` is the Open Library search, so a list search carried there would turn into one. The list travels through the Open Library search and a "Use this" pick. It also travels through a refused form.

This task also covers one item from #594: the list-swap test now checks that the search box is never swapped.

**Files:**
- Modify: `internal/apps/books/view.go` (`listCtx.values`, `Query`, `AddURL`)
- Modify: `internal/apps/books/form.go` (`formView`, `addCtx`, `newBookForm`, `editBookForm`, `newForm`, `create`, `editForm`, `update`, `search`, `pickURL`)
- Modify: `internal/apps/books/findcover.go` (the `editBookForm` call)
- Modify: `internal/apps/books/templates/form.html`, `internal/apps/books/templates/home.partial.html`
- Test: `internal/apps/books/form_test.go`, `internal/apps/books/handlers_test.go`, `internal/apps/books/home_test.go`

**Interfaces:**
- Consumes: `listHeading`, `ctxFrom` and the `ctx-fields` block.
- Produces:
  - `func (listCtx) values() url.Values`
  - `func (listCtx) AddURL() string`, which is `/books/new?` plus the shelf, tag and series, never `q`
  - `func addCtx(get func(string) string) listCtx`
  - `formView.Back` and `formView.BackLabel`. `formView.Cancel` is removed.
  - `func editBookForm(b Book, v formValues, errs FieldErrors, c listCtx) formView`. Task 6 calls it.
  - `func (a *App) newBookForm(v formValues, errs FieldErrors, c listCtx) formView`
  - `func pickURL(c Candidate, back listCtx) string`
  - `func (a *App) search(ctx context.Context, q string, back listCtx) searchView`
  - Template block `add-link`, taking `(dict "Ctx" listCtx "OOB" bool)` and rendering `a#books-add`
  - CSS for Task 7: `.books-form-page > .books-page-bar`

- [ ] **Step 1: Write the failing tests**

In `internal/apps/books/form_test.go`, add:

```go
func TestAddAndEditHaveAPageBar(t *testing.T) {
	s := newServer(t)
	id := add(t, s, s.Alice.User.ID, titled("Dune", "", books.ShelfWant))

	doc := s.Get(t, s.Alice, "/books/new?shelf=all&tag=sf")
	back := doc.MustHave(".books-page-bar .books-back")
	if href, _ := htmlassert.Attr(back, "href"); href != "/books/?shelf=all&tag=sf" {
		t.Errorf("Add's back link = %q, want the list it came from", href)
	}
	if got := htmlassert.Text(back); got != "Tagged “sf”" {
		t.Errorf("Add's back link text = %q, want the list's heading", got)
	}
	if got := attr(t, doc, ".books-form-actions a", "href"); got != "/books/?shelf=all&tag=sf" {
		t.Errorf("Add's Cancel = %q, want the same place as the back link", got)
	}
	doc.MustNotHave(".books-page-actions") // no right-hand actions

	doc = s.Get(t, s.Alice, fmt.Sprintf("/books/edit/%d?shelf=want&q=du", id))
	back = doc.MustHave(".books-page-bar .books-back")
	want := fmt.Sprintf("/books/b/%d?q=du&shelf=want", id)
	if href, _ := htmlassert.Attr(back, "href"); href != want {
		t.Errorf("Edit's back link = %q, want the book page %q", href, want)
	}
	if got := htmlassert.Text(back); got != "Dune" {
		t.Errorf("Edit's back link text = %q, want the book's title", got)
	}
	if got := attr(t, doc, ".books-form-actions a", "href"); got != want {
		t.Errorf("Edit's Cancel = %q, want %q", got, want)
	}
	doc.MustNotHave(".books-page-actions")
}

func TestAddKeepsItsListButNotItsSearch(t *testing.T) {
	s, _ := newServerWithOL(t)
	// The toolbar's Add link leaves the list's search behind.
	doc := s.Get(t, s.Alice, "/books/?shelf=all&tag=sf&q=dune")
	if got := attr(t, doc, "a#books-add", "href"); got != "/books/new?shelf=all&tag=sf" {
		t.Errorf("Add link = %q, want the tag list without its search", got)
	}
	// The Open Library search and a pick keep the list.
	doc = s.Get(t, s.Alice, "/books/new?shelf=all&tag=sf&q=piranesi")
	if got := attr(t, doc, `.books-search-form input[name="tag"]`, "value"); got != "sf" {
		t.Errorf("search form's tag = %q, want the list carried", got)
	}
	use, err := url.Parse(attr(t, doc, "a.books-result-use", "href"))
	if err != nil {
		t.Fatal(err)
	}
	if q := use.Query(); q.Get("shelf") != "all" || q.Get("tag") != "sf" || q.Has("q") {
		t.Errorf("Use this = %s, want the list's shelf and tag and no q", use)
	}
	doc = s.Get(t, s.Alice, use.String())
	if got := attr(t, doc, ".books-back", "href"); got != "/books/?shelf=all&tag=sf" {
		t.Errorf("back link after a pick = %q", got)
	}
	doc.MustNotHave(`form.books-form input[name="q"]`) // the list's search never posts with the book
	if got := attr(t, doc, `form.books-form input[name="tag"]`, "value"); got != "sf" {
		t.Errorf("Add form's tag field = %q, want the list carried", got)
	}
	// A refused add comes back to the same list.
	rec := s.Post(t, s.Alice, "/books/new", url.Values{"title": {""}, "add_to": {"want"}, "shelf": {"all"}, "tag": {"sf"}})
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("refused add = %d, want 422", rec.Code)
	}
	if got := attr(t, htmlassert.Parse(t, rec.Body.String()), ".books-back", "href"); got != "/books/?shelf=all&tag=sf" {
		t.Errorf("back link after a refused add = %q", got)
	}
}
```

Update the tests that pinned the Add link's old address:

- In `internal/apps/books/handlers_test.go`, `TestIndexRenders`: replace `doc.MustHave(`.books-toolbar a[href="/books/new"]`)` with `doc.MustHave(`.books-toolbar a[href="/books/new?shelf=reading"]`)`.
- In `internal/apps/books/home_test.go`, `TestTheSidebarHasTheLinksAndNoAddButton`: replace its two Add-link lines with:

```go
	doc.MustNotHave(".books-side .books-add") // Add book is in the toolbar
	doc.MustHave(`.books-toolbar a[href="/books/new?shelf=reading"]`)
```

- In `internal/apps/books/home_test.go`, `TestIconOnlyButtonsHaveNames`: replace `attr(t, doc, `.books-toolbar a[href="/books/new"]`, "aria-label")` with `attr(t, doc, "a#books-add", "aria-label")`.
- In `internal/apps/books/handlers_test.go`, `TestHTMXListNavigationSwapsTheListOnly`:
  - Add `"books-add"` to the list of ids that must be swapped out of band.
  - After `doc.MustNotHave("#books-home")`, add `doc.MustNotHave("#books-q") // the search box is never swapped (PATTERNS.md, SyncSearch; #594)`.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/apps/books/ -run 'PageBar|AddKeeps|IndexRenders|Sidebar|IconOnly|HTMXListNavigation' -count=1`
Expected: FAIL. The failures are a missing `.books-page-bar`, a missing `a#books-add`, and Add hrefs without the list.

- [ ] **Step 3: Give the list context its Add address**

In `internal/apps/books/view.go`, replace `func (c listCtx) Query() string { … }` and its doc comment with:

```go
// values is the context as query values.
func (c listCtx) values() url.Values {
	v := url.Values{}
	v.Set("shelf", string(c.Shelf))
	if c.Tag != "" {
		v.Set("tag", c.Tag)
	}
	if c.Q != "" {
		v.Set("q", c.Q)
	}
	if c.Series != "" {
		v.Set("series", c.Series)
	}
	return v
}

// Query is the context as a query string. Templates use the URL methods
// below rather than this: a whole URL dropped into href is left alone by
// html/template, a query string after a literal "?" gets percent-encoded.
func (c listCtx) Query() string { return c.values().Encode() }
```

After `BookURL`, add:

```go
// AddURL is the Add book page, coming back to this list afterwards — but
// not to its search: on the Add page q is the Open Library search (UI
// redesign spec "Add and Edit pages").
func (c listCtx) AddURL() string {
	return "/books/new?" + listCtx{Shelf: c.Shelf, Tag: c.Tag, Series: c.Series}.Query()
}
```

- [ ] **Step 4: Carry the list through Add, and name both pages' way back**

In `internal/apps/books/form.go`:

1. In `formView`, replace `Cancel  string` with:

```go
	Back      string // the page bar's way back, and Cancel's: the list (Add) or the book (Edit)
	BackLabel string // "Reading", "Tagged “sf”", or the book's title
```

2. Replace `pickURL` with:

```go
// pickURL is the Add book page pre-filled with c, coming back to list
// back. Everything travels in the link: the values are only a starting
// point the person edits anyway.
func pickURL(c Candidate, back listCtx) string {
	v := back.values()
	v.Set("pick", "1")
	for k, val := range map[string]string{"title": c.Title, "subtitle": c.Subtitle, "authors": c.Authors,
		"year": numText(c.Year), "pages": numText(c.Pages), "isbn": c.ISBN,
		"ol_work": c.WorkID, "ol_edition": c.EditionID} {
		if val != "" {
			v.Set(k, val)
		}
	}
	if c.CoverID > 0 {
		v.Set("cover", strconv.FormatInt(c.CoverID, 10))
	}
	return "/books/new?" + v.Encode()
}
```

3. In `search`, change the signature to `func (a *App) search(ctx context.Context, q string, back listCtx) searchView`. Change its doc comment's first line to `// search asks Open Library for q, for the Add page of list back. A failure`. Change `PickURL: pickURL(c)` to `PickURL: pickURL(c, back)`.

4. Replace `newBookForm` with:

```go
// addCtx is the list the Add page came from and goes back to. On the Add
// page q is the Open Library search, not the list's, so the list's search
// stays behind (AddURL never carries one).
func addCtx(get func(string) string) listCtx {
	c := ctxFrom(get)
	c.Q = ""
	return c
}

func (a *App) newBookForm(v formValues, errs FieldErrors, c listCtx) formView {
	return formView{New: true, Action: "/books/new", Heading: "Add a book", Submit: "Add book",
		Back: c.ListURL(), BackLabel: listHeading(c), Values: v, Errors: errs, Today: a.store.Today(), Ctx: c}
}
```

5. In `newForm`:
   - After `q := r.URL.Query()`, add `c := addCtx(q.Get)`.
   - Change `view := a.newBookForm(v, nil)` to `view := a.newBookForm(v, nil, c)`.
   - Change `view.Search = a.search(r.Context(), text)` to `view.Search = a.search(r.Context(), text, c)`.

6. In `create`:
   - After `in, vals, errs := parseForm(r.PostFormValue)`, add `c := addCtx(r.PostFormValue)`.
   - Change every `a.newBookForm(vals, …)` call to pass `c` as a third argument. There are four.

7. Replace `editBookForm` with:

```go
// editBookForm is the Edit page of b showing v; its page bar goes back to
// b's page, named after b's saved title.
func editBookForm(b Book, v formValues, errs FieldErrors, c listCtx) formView {
	return formView{Action: "/books/edit/" + strconv.FormatInt(b.ID, 10), Heading: "Edit book", Submit: "Save",
		Back: c.BookURL(b.ID), BackLabel: b.Title, Values: v, Errors: errs, Ctx: c,
		Cover: coverURL(b.ID, b.CoverVersion), Spine: SpineColor(v.Title), ID: b.ID}
}
```

8. In `editForm`, change the `editBookForm(id, vals, nil, ctxFrom(r.FormValue), coverURL(b.ID, b.CoverVersion))` call to `editBookForm(b, vals, nil, ctxFrom(r.FormValue))`.

9. In `update`, change `editBookForm(id, vals, errs, c, coverURL(b.ID, b.CoverVersion))` to `editBookForm(b, vals, errs, c)`.

In `internal/apps/books/findcover.go`, change `editBookForm(id, valuesOf(b.BookInput), nil, c, coverURL(b.ID, b.CoverVersion))` to `editBookForm(b, valuesOf(b.BookInput), nil, c)`. Task 6 rewrites this handler.

- [ ] **Step 5: Draw the page bar, and send the list along**

In `internal/apps/books/templates/form.html`:

1. Replace:

```
<div class="books-form-page stack">
	<h1>{{$d.Heading}}</h1>
	{{if $d.New}}{{template "ol-search" $d.Search}}{{end}}
```

   with:

```
<div class="books-form-page stack">
	{{/* The book page's page bar, with the way back and no actions (UI
	     redesign spec "Add and Edit pages"). */}}
	<div class="books-page-bar">
		<a class="books-back" href="{{$d.Back}}">{{ticon "arrow-left"}}{{$d.BackLabel}}</a>
	</div>
	<h1>{{$d.Heading}}</h1>
	{{if $d.New}}{{template "ol-search" (dict "Search" $d.Search "Ctx" $d.Ctx)}}{{end}}
```

2. Replace `{{if not $d.New}}{{template "ctx-fields" $d.Ctx}}{{end}}` with `{{template "ctx-fields" $d.Ctx}}`. On Add, the context has no `q`, so no `q` field is drawn.

3. Replace `<a href="{{$d.Cancel}}">Cancel</a>` with `<a class="button" href="{{$d.Back}}">Cancel</a>`.

4. Replace the whole `ol-search` block and its comment with:

```
{{/* ol-search is the Open Library search on the Add book page: a plain
     GET form, so it works without JavaScript. It carries the list the page
     came from, so the results and "Use this" do too. Takes a dict: Search
     (a searchView), Ctx (the list, without its search). */}}
{{define "ol-search"}}
{{$s := .Search}}
<section class="books-search stack" aria-label="Find a book on Open Library">
	<form class="books-search-form" method="get" action="/books/new" role="search">
		{{template "ctx-fields" .Ctx}}
		<label for="books-search-q">Find it on Open Library</label>
		<div class="books-search-row">
			<input id="books-search-q" name="q" type="search" value="{{$s.Query}}" placeholder="Title, author or ISBN">
			<button type="submit">Search</button>
		</div>
	</form>
	{{with $s.Error}}<p class="notice notice-error" role="alert">{{.}}</p>{{end}}
	{{if $s.Results}}
	<ul class="books-results">
		{{range $s.Results}}
		<li class="books-result">
			{{if .Thumb}}<img class="books-result-cover" src="{{.Thumb}}" alt="" width="40" height="60" loading="lazy">{{else}}<span class="books-result-cover" aria-hidden="true"></span>{{end}}
			<span class="books-result-text"><span class="books-result-title">{{.Title}}</span>{{with .Byline}}<span class="books-result-byline">{{.}}</span>{{end}}</span>
			<a class="button books-result-use" href="{{.PickURL}}" aria-label="Use {{.Title}}">Use this</a>
		</li>
		{{end}}
	</ul>
	{{else if and $s.Searched (not $s.Error)}}
	<p class="empty">Open Library has nothing for “{{$s.Query}}”. Fill it in yourself below.</p>
	{{end}}
</section>
{{end}}
```

In `internal/apps/books/templates/home.partial.html`:

1. In `home`, replace the `<a class="button primary books-add" …>…</a>` line with `{{template "add-link" (dict "Ctx" .Ctx "OOB" false)}}`.

2. In `list-swap`, add `{{template "add-link" (dict "Ctx" .Ctx "OOB" true)}}` just before `{{template "drawer-toggle" true}}`. Add `+ Add book` to the list of out-of-band pieces in its comment.

3. After the `drawer-toggle` block, add:

```
{{/* add-link is + Add book, carrying the list (but not its search), so
     Add comes back to it (spec "Add and Edit pages"). It follows every
     list swap out of band. Takes a dict: Ctx, OOB. */}}
{{define "add-link"}}<a class="button primary books-add" id="books-add" href="{{.Ctx.AddURL}}" aria-label="Add book"{{if .OOB}} hx-swap-oob="true"{{end}}>{{ticon "plus"}}<span class="books-add-label">Add book</span></a>{{end}}
```

- [ ] **Step 6: Run the tests to verify they pass**

Run: `go test ./internal/apps/books/... -count=1`
Expected: PASS. The existing Open Library tests (`TestAddPageSearchesOpenLibrary` and the rest) check each pick parameter separately, so a `shelf` added to "Use this" doesn't break them.

- [ ] **Step 7: Run the full check and commit**

```bash
gofmt -l . && go vet ./... && go run honnef.co/go/tools/cmd/staticcheck@v0.8.0 ./... && go test ./... -race -count=1
git add internal/apps/books
git commit -m "feat(books): a page bar on Add and Edit; Add comes back to the list it was opened from"
```

---
### Task 6: Find cover keeps what was typed (#591)

Today Find cover on Edit is a small form of its own. If you edit a field and then press it, the page comes back with the saved values, and your edit is silently lost. Ilia decided on 2026-10-12 that the button becomes a second submit button of the Edit form (`formaction="/books/find-cover/{id}"`), so its post carries every field:
- **The answer** is the Edit page again, drawn with what was typed. On success it shows the new cover and "Found a cover on Open Library.". When nothing is found, it is a 422 with the reason by the cover.
- **Nothing is saved but the cover.** The rest waits for Save.
- **The lookup uses what the form says now** (ISBN, title and author) when the form would save. Otherwise it uses the saved book.
- **A file chosen in the upload box isn't kept.** A browser can't refill a file input.

The post is now the Edit form's multipart body, so Find cover gets the same body budget as Save. This task also covers one item from #594: a test pins the CSRF field on the form Find cover posts.

**Files:**
- Modify: `internal/apps/books/findcover.go` (`findCover`)
- Modify: `internal/apps/books/form.go` (`editForm`: the `found=1` read goes)
- Modify: `internal/apps/books/books.go` (the body limit)
- Modify: `internal/apps/books/templates/form.html`
- Test: `internal/apps/books/findcover_test.go`

**Interfaces:**
- Consumes: `editBookForm(b Book, v formValues, errs FieldErrors, c listCtx)` (Task 5), `parseForm` and `merge` (`form.go`), and `lookUpCover` (`findcover.go`).
- Produces: `POST /books/find-cover/{id}`, which takes the whole Edit form, has a body limit of `editFormMaxBytes`, and answers with a page (200 found, 422 not) instead of a redirect.

- [ ] **Step 1: Write the failing tests**

In `internal/apps/books/findcover_test.go`, add `"mime/multipart"`, `"strings"` and `"github.com/iliafrenkel/on-suite/internal/platform/web"` to the imports.

Replace `TestFindCoverIsOnTheEditPageOfABookWithout` with:

```go
func TestFindCoverIsOnTheEditPageOfABookWithout(t *testing.T) {
	s := newServer(t)
	uid := s.Alice.User.ID
	id := add(t, s, uid, titled("Piranesi", "", books.ShelfWant))
	path := fmt.Sprintf("/books/edit/%d?shelf=want", id)
	doc := s.Get(t, s.Alice, path)
	btn := doc.MustHave(".books-cover-field button.books-find-cover")
	if got, _ := htmlassert.Attr(btn, "formaction"); got != fmt.Sprintf("/books/find-cover/%d", id) {
		t.Errorf("Find cover formaction = %q", got)
	}
	if _, ok := htmlassert.Attr(btn, "form"); ok {
		t.Error("Find cover names another form; it should post the Edit form itself (#591)")
	}
	doc.MustNotHave("form#books-find-cover")
	// What it posts carries the CSRF token and the list (#594).
	doc.MustHave(fmt.Sprintf(`form.books-form input[name="%s"]`, web.CSRFFormField))
	if got := attr(t, doc, `form.books-form input[name="shelf"]`, "value"); got != "want" {
		t.Errorf("the Edit form's shelf = %q, want the list the page came from", got)
	}
	if err := s.Store.SetCover(context.Background(), uid, id, "image/png", onePNG, books.CoverUpload); err != nil {
		t.Fatal(err)
	}
	s.Get(t, s.Alice, path).MustNotHave("button.books-find-cover")
}
```

In `TestFindCoverByISBNThenByTitle`, replace the redirect check inside the loop:

```go
		if want := fmt.Sprintf("/books/edit/%d?shelf=want&found=1", id); rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != want {
			t.Errorf("%s: Find cover = %d to %q, want 303 to %q", name, rec.Code, rec.Header().Get("Location"), want)
		}
```

with:

```go
		if rec.Code != http.StatusOK {
			t.Errorf("%s: Find cover = %d, want 200: the Edit page again, with the cover", name, rec.Code)
		}
		doc := htmlassert.Parse(t, rec.Body.String())
		if got := htmlassert.Text(doc.MustHave(".books-cover-field .notice")); got != "Found a cover on Open Library." {
			t.Errorf("%s: notice = %q", name, got)
		}
		doc.MustHave(".books-cover-field img.books-cover")
```

Then delete the lines after the loop, from `doc := s.Get(t, s.Alice, fmt.Sprintf("/books/edit/%d?shelf=want&found=1", byTitle))` to the end of that `if`. The notice is now checked inside the loop.

Add these tests and a helper at the end of the file:

```go
func TestFindCoverKeepsWhatWasTyped(t *testing.T) {
	s, _, _ := newBooksApp(t)
	ctx := context.Background()
	uid := s.Alice.User.ID
	for _, tt := range []struct {
		title string
		code  int
	}{
		{"Piranesi", http.StatusOK},                       // found
		{"Nothing like it", http.StatusUnprocessableEntity}, // none to be had
	} {
		id := add(t, s, uid, titled(tt.title, "", books.ShelfWant))
		rec := s.Post(t, s.Alice, fmt.Sprintf("/books/find-cover/%d", id), url.Values{"shelf": {"want"},
			"title": {tt.title}, "subtitle": {"Typed, not saved"}, "tags": {"typed, tags"}, "description": {"A typed description."}})
		if rec.Code != tt.code {
			t.Errorf("%s: Find cover = %d, want %d", tt.title, rec.Code, tt.code)
		}
		doc := htmlassert.Parse(t, rec.Body.String())
		if got := attr(t, doc, "input#books-subtitle", "value"); got != "Typed, not saved" {
			t.Errorf("%s: subtitle = %q, want what was typed (#591)", tt.title, got)
		}
		if got := attr(t, doc, "input#books-tags", "value"); got != "typed, tags" {
			t.Errorf("%s: tags = %q, want what was typed", tt.title, got)
		}
		if got := htmlassert.Text(doc.MustHave("textarea#books-description")); got != "A typed description." {
			t.Errorf("%s: description = %q, want what was typed", tt.title, got)
		}
		if got := htmlassert.Text(doc.MustHave(".books-back")); got != tt.title {
			t.Errorf("%s: back link = %q, want the saved title", tt.title, got)
		}
		if b, _ := s.Store.Get(ctx, uid, id); b.Subtitle != "" || len(b.Tags) != 0 {
			t.Errorf("%s: Find cover saved the form (subtitle %q, tags %v); only Save does", tt.title, b.Subtitle, b.Tags)
		}
	}
}

func TestFindCoverLooksUpWhatTheFormSays(t *testing.T) {
	s, _, _ := newBooksApp(t)
	// Saved without an ISBN, under a title Open Library has nothing for;
	// the ISBN typed into the form has a cover.
	id := add(t, s, s.Alice.User.ID, titled("Nothing like it", "", books.ShelfWant))
	rec := s.Post(t, s.Alice, fmt.Sprintf("/books/find-cover/%d", id), url.Values{"shelf": {"want"},
		"title": {"Nothing like it"}, "isbn": {"9781635575637"}})
	if rec.Code != http.StatusOK {
		t.Errorf("Find cover with a typed ISBN = %d, want 200: it looks up what the form says", rec.Code)
	}
	// A form that wouldn't save (no title) falls back to the saved book.
	rec = s.Post(t, s.Alice, fmt.Sprintf("/books/find-cover/%d", add(t, s, s.Alice.User.ID, titled("Piranesi", "", books.ShelfWant))),
		url.Values{"shelf": {"want"}, "title": {""}})
	if rec.Code != http.StatusOK {
		t.Errorf("Find cover with an empty title box = %d, want 200 from the saved title", rec.Code)
	}
}

// postEditForm posts Alice's Edit form as a browser does, multipart with
// a file in the upload box, to path.
func postEditForm(t *testing.T, s *server, path string, fields url.Values, file []byte) *httptest.ResponseRecorder {
	t.Helper()
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	fields.Set(web.CSRFFormField, s.CSRFToken(t, s.Alice))
	for k, vs := range fields {
		for _, v := range vs {
			if err := mw.WriteField(k, v); err != nil {
				t.Fatal(err)
			}
		}
	}
	part, err := mw.CreateFormFile("cover_file", "cover.jpg")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(file); err != nil {
		t.Fatal(err)
	}
	if err := mw.Close(); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, path, &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	return s.Do(t, s.Alice, req)
}

func TestFindCoverTakesTheEditFormsBody(t *testing.T) {
	s, _, _ := newBooksApp(t)
	id := add(t, s, s.Alice.User.ID, titled("Piranesi", "", books.ShelfWant))
	big := bytes.Repeat([]byte{0}, 3<<19) // 1.5 MiB: over the suite's 1 MiB default
	rec := postEditForm(t, s, fmt.Sprintf("/books/find-cover/%d", id), url.Values{"shelf": {"want"}, "title": {"Piranesi"}}, big)
	if rec.Code != http.StatusOK {
		t.Errorf("Find cover with an image chosen = %d, want 200: it posts the Edit form, so it has Save's body budget", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "Found a cover on Open Library.") {
		t.Error("the chosen file got in the way of the lookup")
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/apps/books/ -run FindCover -count=1`
Expected: FAIL.
- The button is the old `button[form="books-find-cover"]`.
- The answers are 303 redirects to the stored values.
- The typed ISBN isn't looked up.
- The 1.5 MiB post is refused with 413.

- [ ] **Step 3: Give Find cover the Edit form's body budget**

In `internal/apps/books/books.go`:

1. Change the comment above `editFormMaxBytes` to:

```go
// editFormMaxBytes is the Edit form's body budget: one cover plus the
// form's text fields and multipart overhead (the suite default is 1 MiB).
// Save and Find cover both post that form (#591).
```

2. Replace `r.HandleFunc("POST /find-cover/{id}", a.findCover)` with:

```go
	r.RegisterBodyLimit("POST /find-cover/{id}", editFormMaxBytes)
	r.HandleFunc("POST /find-cover/{id}", a.findCover)
```

- [ ] **Step 4: Answer with what was typed**

In `internal/apps/books/findcover.go`, replace `findCover` and its doc comment with:

```go
// findCover is the Edit page's Find cover (UI redesign spec "Add and Edit
// pages"): the lookup, now. Its button posts the whole Edit form, so the
// page comes back with what was typed in it, saved or not (#591): found,
// with the new cover and a notice; finding none, or Open Library not
// answering, with the reason by the cover, as a 422. Only the cover is
// saved. The lookup uses the form's ISBN, title and author when the form
// would save, the saved book's otherwise. A file chosen in the upload box
// isn't kept: a browser can't refill one.
func (a *App) findCover(w http.ResponseWriter, r *http.Request) {
	uid, ok := a.userID(w, r)
	if !ok {
		return
	}
	id, ok := a.pathID(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	b, err := a.store.Get(ctx, uid, id)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	c := ctxFrom(r.PostFormValue)
	in, typed, errs := parseForm(r.PostFormValue)
	look := b
	if len(merge(errs, in.Normalize().Validate())) == 0 {
		look.BookInput = in.Normalize()
	}
	ct, data, err := a.lookUpCover(ctx, look)
	var msg string
	switch {
	case errors.Is(err, ErrNoCover):
		if err := a.store.MarkCoverChecked(ctx, uid, id); err != nil {
			a.fail(w, r, err)
			return
		}
		msg = "Open Library has no cover for this book. You can upload one, or paste the address of one."
	case err != nil:
		a.deps.Log.Info("books find cover failed", "book", id, "error", err)
		msg = "Open Library didn't answer. Try again in a minute."
	}
	if msg != "" {
		v := editBookForm(b, typed, nil, c)
		v.CoverNotice = msg
		a.renderForm(w, r, http.StatusUnprocessableEntity, v)
		return
	}
	if err := a.store.SetCover(ctx, uid, id, ct, data, CoverFromOL); err != nil {
		a.fail(w, r, err)
		return
	}
	if b, err = a.store.Get(ctx, uid, id); err != nil { // the new cover's version
		a.fail(w, r, err)
		return
	}
	v := editBookForm(b, typed, nil, c)
	v.Found = true
	a.renderForm(w, r, http.StatusOK, v)
}
```

`ISBN13` and the ISBN check live in `Normalize` and `Validate`, so a typed ISBN reaches `CoverByISBN` as 13 digits.

In `internal/apps/books/form.go`:
- In `editForm`, delete the line `v.Found = r.FormValue("found") == "1" && b.CoverVersion != ""`.
- In `formView`, change the `Found` comment to `// Found says the Edit page is back from a successful Find cover;`.
- `editForm` now ends `a.renderForm(w, r, http.StatusOK, editBookForm(b, vals, nil, ctxFrom(r.FormValue)))`. Inline `v` if it's no longer needed.

- [ ] **Step 5: Make the button part of the Edit form**

In `internal/apps/books/templates/form.html`:

1. Replace:

```
					{{if not $d.Cover}}<p><button type="submit" form="books-find-cover" class="button">Find a cover on Open Library</button></p>{{end}}
```

   with:

```
					{{/* Find cover posts this whole form, so what was typed comes
					     back with the cover; nothing but the cover is saved
					     (#591). */}}
					{{if not $d.Cover}}<p><button type="submit" class="button books-find-cover" formaction="/books/find-cover/{{$d.ID}}">Find a cover on Open Library</button></p>{{end}}
```

2. Delete the separate form at the end of the page, from `{{if and (not $d.New) (not $d.Cover)}}` through its `{{end}}`.

- [ ] **Step 6: Run the tests to verify they pass**

Run: `go test ./internal/apps/books/... -count=1`
Expected: PASS. `TestFindCoverSaysWhenItCant` still passes: its helper posts only `shelf`, so the form wouldn't save and the lookup uses the saved title.

- [ ] **Step 7: Run the full check and commit**

```bash
gofmt -l . && go vet ./... && go run honnef.co/go/tools/cmd/staticcheck@v0.8.0 ./... && go test ./... -race -count=1
git add internal/apps/books
git commit -m "fix(books): Find cover on the Edit page keeps what was typed (#591)"
```

---
### Task 7: Style the sections, the entries' icons, the popovers and the form pages

No Go tests cover CSS, so this task is checked in the browser (Step 6). It also covers two follow-ups:
- **#593:** merge `.visually-hidden` into one rule.
- **#594:** the phone's icon-only **+ Add book** gets narrower padding.

Then add the new pattern to PATTERNS.md.

**Files:**
- Modify: `internal/ui/static/app.css`: the ON Books block (from about line 4129), and the second `.visually-hidden` rule (about line 612)
- Modify: `PATTERNS.md`

**Interfaces:**
- Consumes: the classes Tasks 1–6 list under "CSS classes for Task 7".
- Produces: nothing new for later tasks.

- [ ] **Step 1: One `.visually-hidden` (#593), committed on its own**

`app.css` defines `.visually-hidden` twice: near line 150 with nine properties, and near line 612 without `padding`, `margin` or `border`. Both apply today, so the effective rule is the first one's set. `rect(0 0 0 0)` and `rect(0, 0, 0, 0)` are the same clip.

Delete the second rule, from `.visually-hidden {` near line 612 through its closing `}` and the blank line after it. Nothing changes on screen.

Run: `grep -n '^\.visually-hidden' internal/ui/static/app.css`
Expected: one line, near 150.

Check the apps that use the class still hide what they should. They are Notes' import input, the Books drawer checkbox and headings, Flash's card checkbox and Paste's detail toggle. Find them all with `grep -rln 'visually-hidden' internal --include='*.html' --include='*.go'`. Look at each one in Step 6's browser pass.

```bash
git add internal/ui/static/app.css
git commit -m "fix(ui): one .visually-hidden rule (#593)"
```

- [ ] **Step 2: Restyle the book header and About**

In `internal/ui/static/app.css`, in the ON Books block, under `/* Book */`:

1. Replace `.books-description { max-width: 38rem; margin-bottom: var(--s-4); color: var(--c-text-dim); }` with:

```css
.books-about { color: var(--c-text-dim); }
.books-about .books-description-text { max-width: 38rem; }
```

2. After the `.books-pill-shelf { … }` line, add:

```css
/* The header's tag pills are read-only links to each tag's list (spec
 * "Header"). */
.books-pill-link { text-decoration: none; }
.books-pill-link:hover { background: var(--c-bg-inset); color: var(--c-text); }
```

- [ ] **Step 3: Replace the book page's section styles**

Replace everything from the line `/* Reading box, tags, confirm dialog */` up to (not including) the line `/* Add book page: Open Library search */` with:

```css
/* Sections (UI redesign spec "Sections"): a hairline, a small uppercase
 * heading with its count, and at most one action on the heading's right.
 * That action is the summary of the section's own <details>, lifted into
 * the heading row, so every disclosure works without JavaScript
 * (PATTERNS.md). */
.books-section { position: relative; margin-top: var(--s-5); padding-top: var(--s-3); border-top: 1px solid var(--c-border); }
.books-section-head { margin: 0 0 var(--s-2); min-height: 1.75rem; font-size: var(--fs-xs); font-weight: 600; line-height: 1.75rem; letter-spacing: 0.07em; text-transform: uppercase; color: var(--c-text-faint); }
.books-section-count { font-weight: 400; }
.books-section-action { position: absolute; top: var(--s-3); right: 0; justify-content: center; width: 1.75rem; height: 1.75rem; padding: 0; list-style: none; }
.books-section-action::-webkit-details-marker { display: none; }
.books-section-add { border-color: var(--c-border-firm); border-radius: 999px; color: var(--c-accent); }
.books-section-add:hover { color: var(--c-accent); }
.books-icon-btn:focus-visible { outline: var(--ring); outline-offset: 1px; }
.books-small-btn { padding: var(--s-1) var(--s-3); font-size: var(--fs-sm); line-height: 1.4; }

/* Reading */
.books-reading-head { display: flex; flex-wrap: wrap; align-items: center; gap: var(--s-2); margin-bottom: var(--s-2); color: var(--c-text-dim); font-size: var(--fs-sm); }
.books-format { display: flex; align-items: center; gap: var(--s-1); margin: 0; }
/* A select, not a tag: the format's icon, the select and its chevron in
 * one outlined control. */
.books-format-control { display: inline-flex; align-items: center; gap: var(--s-1); padding-left: var(--s-2); border: 1px solid var(--c-border-firm); border-radius: var(--radius); background: var(--c-bg); color: var(--c-text-dim); }
.books-format-control:focus-within { outline: var(--ring); }
.books-format-control select { width: auto; padding: var(--s-1) var(--s-2) var(--s-1) 0; border: 0; background: transparent; color: var(--c-text); font-size: var(--fs-sm); }
.books-format-control select:focus { outline: none; }
.books-format-save { padding: var(--s-1) var(--s-2); font-size: var(--fs-sm); }
.books-progress { margin: 0; }
.books-progress-row { display: flex; flex-wrap: wrap; align-items: center; gap: var(--s-2) var(--s-3); }
.books-progress-form { display: flex; flex-wrap: wrap; align-items: center; gap: var(--s-1) var(--s-2); margin: 0; font-size: var(--fs-sm); }
.books-progress-form label { margin: 0; color: var(--c-text); font-size: var(--fs-sm); }
.books-progress-form input[type="number"] { width: 4.5rem; padding: var(--s-1) var(--s-2); }
.books-progress-of,
.books-progress-note { color: var(--c-text-dim); }
.books-progress-note::before { content: "· "; }
.books-progress-save { border-color: var(--c-border-firm); }
.books-progress-form .books-field-error { flex-basis: 100%; }
/* Finish… and Did not finish… sit at the row's end. Their popovers are
 * anchored to the pair rather than to each button, so on a phone neither
 * runs off the left edge. */
.books-reading-actions { position: relative; display: flex; flex-wrap: wrap; gap: var(--s-2); margin-left: auto; }
.books-close > summary { display: inline-flex; list-style: none; cursor: pointer; }
.books-close > summary::-webkit-details-marker { display: none; }
.books-close-form { position: absolute; top: calc(100% + var(--s-1)); right: 0; z-index: 2; display: flex; flex-wrap: wrap; align-items: flex-end; gap: var(--s-2); width: max-content; max-width: min(20rem, calc(100vw - 2rem)); padding: var(--s-3); border: var(--border); border-radius: var(--radius); background: var(--c-bg); box-shadow: 0 4px 16px rgba(0, 0, 0, 0.15); }
.books-close-form label { margin: 0; flex-basis: 100%; }
/* Progress bars: the Reading section's, the goal's and a tile's */
.books-progress-bar,
.books-bar { appearance: none; border: 0; border-radius: 999px; overflow: hidden; background: var(--c-bg-inset); }
.books-progress-bar { display: block; width: 100%; max-width: 24rem; height: 6px; margin-top: var(--s-2); }
.books-reading .books-progress-bar { max-width: none; height: 4px; margin: 0 0 var(--s-2); }
.books-progress-bar::-webkit-progress-bar,
.books-bar::-webkit-progress-bar { background: var(--c-bg-inset); }
.books-progress-bar::-webkit-progress-value,
.books-bar::-webkit-progress-value { background: var(--c-accent); }
.books-progress-bar::-moz-progress-bar,
.books-bar::-moz-progress-bar { background: var(--c-accent); }
.books-stars { color: var(--c-accent); letter-spacing: 0.05em; }

/* Your take */
.books-rating { display: flex; flex-wrap: wrap; align-items: center; gap: var(--s-1); margin: 0 0 var(--s-2); }
/* The five stars wrap as one, never one by one (#579). */
.books-star-row { display: inline-flex; flex-wrap: nowrap; gap: var(--s-1); }
.books-star { display: inline-flex; padding: var(--s-1); border: 0; background: none; color: var(--c-text-faint); cursor: pointer; }
.books-star:hover,
.books-star.is-on { color: var(--c-accent); }
.books-review-text { max-width: 38rem; }
.books-review-text p { margin: 0 0 var(--s-2); }
/* ✎ turns the review into its textarea in place. */
.books-take:has(> .books-review-edit[open]) > .books-review-text { display: none; }
.books-review-edit > summary::-webkit-details-marker { display: none; }
.books-review-write { display: inline-block; list-style: none; color: var(--c-accent); font-size: var(--fs-sm); cursor: pointer; }
.books-review-write::-webkit-details-marker { display: none; }
.books-review-write:hover { text-decoration: underline; }
.books-review-form { display: flex; flex-direction: column; gap: var(--s-2); max-width: 38rem; margin-top: var(--s-2); }
.books-review-form label { margin: 0; }
.books-review-form textarea { width: 100%; }
.books-review-form button { align-self: flex-start; }
.books-hint { margin: 0; color: var(--c-text-dim); font-size: var(--fs-sm); }

/* Entries: notes, quotes and readings. ✎ and 🗑 sit at an entry's top
 * right. They show on hover and on keyboard focus, and always on a touch
 * screen, which has no hover (spec "Sections"). They're hidden with
 * opacity, so they stay in the tab order. */
.books-entries { list-style: none; margin: 0; padding: 0; }
.books-entry { position: relative; padding: var(--s-2) 4rem var(--s-2) 0; border-bottom: 1px solid var(--c-border); }
.books-entry:last-child { border-bottom: 0; }
.books-entry-edit > summary,
.books-entry-delete { position: absolute; top: var(--s-2); margin: 0; opacity: 0; transition: opacity 0.15s ease; }
.books-entry-edit > summary { right: 2rem; list-style: none; }
.books-entry-edit > summary::-webkit-details-marker { display: none; }
.books-entry-delete { right: 0; }
.books-entry-icon { justify-content: center; width: 1.75rem; height: 1.75rem; padding: 0; }
.books-entry-icon.is-danger { color: var(--c-danger); }
.books-entry-icon.is-danger:hover { background: var(--c-danger-bg); color: var(--c-danger); }
.books-entry:hover > .books-entry-edit > summary,
.books-entry:hover > .books-entry-delete,
.books-entry:focus-within > .books-entry-edit > summary,
.books-entry:focus-within > .books-entry-delete,
.books-entry-edit[open] > summary { opacity: 1; }
@media (hover: none) {
	.books-entry-edit > summary,
	.books-entry-delete { opacity: 1; }
}
@media (prefers-reduced-motion: reduce) {
	.books-entry-edit > summary,
	.books-entry-delete { transition: none; }
}
/* ✎ turns the entry into its form in place. */
.books-entry:has(> .books-entry-edit[open]) > :is(.books-entry-meta, .books-entry-text, .books-quote-text, .books-history-line) { display: none; }
.books-entry-meta { margin: 0; color: var(--c-text-dim); font-size: var(--fs-sm); }
.books-entry-text p { margin: var(--s-1) 0; }
.books-entry-form { display: flex; flex-direction: column; gap: var(--s-2); max-width: 38rem; margin: var(--s-2) 0; }
.books-entry-form label { margin: 0; }
.books-entry-form textarea { width: 100%; }
.books-entry-form input[type="number"] { width: 6rem; }
.books-entry-form button { align-self: flex-start; }
/* A quote keeps its blockquote, and the line breaks it was typed with. */
.books-quote-text { margin: 0 0 var(--s-1); padding-left: var(--s-3); border-left: 3px solid var(--c-accent); font-style: italic; white-space: pre-line; overflow-wrap: anywhere; }
.books-quote-comment { margin-top: var(--s-1); }

/* Reading history: a disclosure, collapsed until opened */
.books-history-summary { list-style: none; cursor: pointer; }
.books-history-summary::-webkit-details-marker { display: none; }
.books-history-summary:focus-visible { outline: var(--ring); }
.books-history-summary .books-section-head { margin: 0; }
.books-history-summary .books-section-head::before { content: "▸ "; }
.books-history[open] > .books-history-summary .books-section-head::before { content: "▾ "; }
.books-history[open] > .books-entries { margin-top: var(--s-2); }
.books-history-line { margin: 0; }
.books-reading-edit-form { display: flex; flex-wrap: wrap; align-items: flex-end; gap: var(--s-2); margin-top: var(--s-2); }
.books-reading-edit-form label { margin: 0; flex-basis: 100%; }

/* The one confirm dialog */
.books-dialog { width: min(28rem, calc(100vw - 2rem)); padding: 1rem; border: var(--border); border-radius: var(--radius); color: var(--c-text); background: var(--c-bg); }
.books-dialog::backdrop { background: rgba(0, 0, 0, 0.35); }

/* The header's cover and titles stack on a phone (spec "Book details
 * page"); the sections are one column at every width already. */
@media (max-width: 640px) {
	.books-book-head { flex-direction: column; }
}

```

- [ ] **Step 4: The Add and Edit pages**

1. In the `/* Add / edit form */` part, replace `.books-form legend { font-weight: 600; margin-bottom: var(--s-2); }` with these lines. The fieldsets read like the book page's sections:

```css
.books-form fieldset { padding-top: var(--s-3); border-top: 1px solid var(--c-border); }
.books-form legend { float: left; width: 100%; margin-bottom: var(--s-2); font-size: var(--fs-xs); font-weight: 600; letter-spacing: 0.07em; text-transform: uppercase; color: var(--c-text-faint); }
.books-form legend + * { clear: both; }
```

   The `float` keeps the legend inside the fieldset's top border rather than cutting through it.

2. In the same part, replace `.books-form-actions { display: flex; align-items: center; gap: var(--s-3); }` with:

```css
.books-form-actions { display: flex; align-items: center; gap: var(--s-2); }
```

   Save and Cancel are now both buttons of the same height.

3. In the `/* Add book page: Open Library search */` part, replace `.books-form-page { max-width: var(--measure); padding: var(--s-4) 0; }` with:

```css
/* The Add and Edit pages: the book page's column and page bar. */
.books-form-page { max-width: 48rem; margin: 0 auto; padding: 0 0 var(--s-6); }
.books-form-page > h1 { margin: 0; }
```

   The fields keep `.books-form { max-width: var(--measure); }`, so they don't stretch across the wider column.

- [ ] **Step 5: The phone's Add book (#594)**

In the `@media (max-width: 640px)` block of the home, after `.books-add-label { display: none; }`, add:

```css
	/* Icon-only, so it doesn't need the label's side padding (#594). */
	.books-toolbar .books-add { padding: var(--s-1) var(--s-2); }
```

- [ ] **Step 6: Build, seed and check in the browser**

```bash
go build -o /tmp/onsuite-bin ./cmd/onsuite
rm -rf /tmp/onsuite-manual-verify
go run ./docs/screenshots/seed --data-dir /tmp/onsuite-manual-verify
```

Start the `onsuite` preview (`.claude/launch.json`) and sign in as `demo`. The password is the published demo value in `docs/screenshots/README.md`. Open `/books/b/1?shelf=reading`, which is Leviathan Wakes: on the go, on paper, with notes. Check each of these:

- **Desktop, light:**
  - **Sections:** six of them, each with a hairline and a small uppercase faint heading. The headings are Reading, Your take, About, Notes · n, Quotes (no count), and ▸ Reading history · n, which is collapsed.
  - **Reading:** "Since …" and an outlined format control showing an open-book icon and a chevron. Under it, a thin bar. Then one row: "Page [n] of 592 · 18% · updated …" with a small ✓, and **Finish…** (primary) and **Did not finish…** at its right end.
  - **Finish…** opens a small popover under the pair, right-aligned, with the date and rating. Esc closes it (after Task 8). For now, a second click closes it.
  - **Your take:** the stars, then "Write a review" (the seed has no review on this book). Write one and save it. Only that section changes, and ✎ now sits at the heading's right. ✎ swaps the review for its textarea.
  - **Notes:** a round accent **+** at the heading's right opens the add form at the top. Hovering a note shows ✎ and a red 🗑 at its top right. ✎ turns the note into its form, and 🗑 opens the confirm dialog.
  - **Reading history:** clicking the heading opens it. Each reading has ✎ / 🗑 on hover. Editing a reading's date keeps the history open.
  - **Keyboard:** Tab onto a note's ✎. The icons show while focus is inside the note.
- **A book with quotes:** open Persuasion from the Read shelf. The quotes keep their accent blockquote, then the page, then the comment.
- **Format:** change it to Audiobook. The page body swaps, the icon becomes headphones, and the progress box turns to percent.
- **Edit page** (✎ in the page bar):
  - The page bar reads "← Leviathan Wakes".
  - The column matches the book page's, and the Cover legend looks like a section heading.
  - Tags sit under Description.
  - Save and Cancel are the same height.
- **Add page** (from the home toolbar on a tag list): "← Tagged “…”".
- **Phone width** (`resize_window` preset `mobile`):
  - The header's cover sits above the titles.
  - The progress row wraps, with Finish… / Did not finish… right-aligned on their own line. Their popovers stay on screen.
  - The ✎ / 🗑 icons are always visible (touch).
  - There's no horizontal scroll, and the home's **+** is a compact square.
- **Dark** (`colorScheme: "dark"`): the headings, the format control, the popovers and the red 🗑 stay readable.
- **#593:** Notes' import button, Flash's card flip and Paste's detail toggle look and work as before.

Fix any CSS problem you find in `app.css`, using tokens only. Then reset the viewport with preset `desktop`.

- [ ] **Step 7: Note the pattern in PATTERNS.md**

In `PATTERNS.md`, after the "No-JS `<details>` disclosure menu" entry, add:

```markdown
- **A section's one action as its own disclosure's lifted summary** —
  reach for this when a section heading needs a single action (+ add, ✎
  edit) that opens a form in the section, without JavaScript: put the
  `<details>` right after the heading and give its `<summary>`
  `position: absolute` in the heading row (the section is
  `position: relative`), so the closed disclosure takes no room and the
  open form flows under the heading. Entries use the same trick for ✎,
  with 🗑 a separate delete form beside it. Both are shown on `:hover`
  and `:focus-within`, and always under `(hover: none)`, and they're
  hidden with `opacity` so they stay in the tab order. `:has(> details[open])`
  hides the entry's text while its form stands in. Canonical:
  `internal/apps/books/templates/book.partial.html`'s `notes` and `take`
  blocks, and the `.books-section` / `.books-entry` rules in
  `internal/ui/static/app.css`.
```

- [ ] **Step 8: Run the full check and commit**

```bash
gofmt -l . && go vet ./... && go run honnef.co/go/tools/cmd/staticcheck@v0.8.0 ./... && go test ./... -race -count=1
git add internal/ui/static/app.css PATTERNS.md
git commit -m "style(books): the book page's sections, entry icons and popovers; Add and Edit match it"
```

---
### Task 8: `books.js` for the sections, and Esc in the drawer (#592)

The keyboard shortcuts from UI1 mostly carry over: the ids they use are unchanged. Four things change:
- **Focus survives a format change.** Picking a format swaps the page body, so focus goes back to the new select.
- **Esc leaves the Reading history alone.** It closes the popovers and the edit disclosures, but not the history, which is a section rather than a popup.
- **`a` follows the Add link,** which carries the list now (Task 5).
- **Esc in the phone drawer (#592).** Esc closes the drawer from anywhere in it, a shelf, tag or footer link included, and focus goes back to the drawer's checkbox. The drawer button labels that checkbox and is outlined while it has focus.

**Files:**
- Modify: `internal/apps/books/static/books.js`

**Interfaces:**
- Consumes: `#books-format` (Task 1), `details.books-history` (Task 3), `a#books-add` (Task 5), and `#books-drawer` / `#books-side` (UI1).
- Produces: nothing for later tasks.

- [ ] **Step 1: Reproduce #592 first**

Build and start the preview as in Task 7, Step 6. Then:
1. Set the viewport to the `mobile` preset and open `/books/`.
2. Tap the drawer button, then Tab (or click) onto a shelf link inside the drawer and press Esc.
3. Note what happens. Does the drawer close? Where does focus land? Check with `javascript_tool`: `document.getElementById("books-drawer").checked` and `document.activeElement.outerHTML`.

The issue says the drawer stays open. If it does close for you, the remaining bug is that focus is left on a link that has just been hidden. The fix below covers both, because it handles Esc for the drawer before anything else can intercept it.

- [ ] **Step 2: Keep focus in the format select across its swap**

In `internal/apps/books/static/books.js`:

1. In the header comment, replace `focus kept in the sort and view controls across a list swap,` with `focus kept in the sort, view and format controls across their swaps,`.

2. Replace the "Focus in the sort and view controls" comment and the `htmx:beforeSwap` listener with:

```js
	// --- Focus in the sort, view and format controls -------------------------
	//
	// Choosing a sort or a view swaps the whole controls form out of band
	// (it carries the pressed state and the list's context), and choosing a
	// format swaps the book page's body (the shelf pill and the progress
	// unit can follow). Either would drop focus to <body>; put it back on
	// the control that had it.
	var keptFocus = null;
	document.addEventListener("htmx:beforeSwap", function () {
		var el = document.activeElement;
		keptFocus = el && el.id && el.closest && el.closest("#books-prefs, .books-format") ? el.id : null;
	});
```

   Keep the `htmx:afterSettle` listener under it unchanged.

- [ ] **Step 3: Esc, `a` and the drawer**

1. Replace the "Keyboard shortcuts" comment block (the one starting `// Home: j/k move through the books,`) with:

```js
	// Home: j/k move through the books, Enter (natively) or o opens one,
	// / searches, a adds a book, Esc closes the drawer. Book page: p goes to
	// the progress box, n and q open the add-note and add-quote forms, e
	// edits, Esc closes an open popover or edit form — or, with none open,
	// goes back to the list (spec "Keyboard").
```

2. Replace `closeDisclosures` with:

```js
	// closeDisclosures closes every open popover and edit form on the book
	// page; false when none was open. The Reading history is a section, not
	// a popup, so Esc leaves it as it is.
	function closeDisclosures() {
		var open = document.querySelectorAll("#books-book details[open]:not(.books-history)");
		open.forEach(function (d) {
			d.open = false;
		});
		return open.length > 0;
	}

	// closeDrawer closes the phone drawer; false when it isn't open. Focus
	// that was in the drawer, or on its checkbox, goes back to the checkbox:
	// the drawer button labels it and is outlined while it has focus, and
	// the link that had focus has just been hidden (#592).
	function closeDrawer(from) {
		var drawer = document.getElementById("books-drawer");
		if (!drawer || !drawer.checked) return false;
		drawer.checked = false;
		var side = document.getElementById("books-side");
		if (from === drawer || (side && side.contains(from))) drawer.focus();
		return true;
	}
```

3. In `homeKey`:
   - Replace the `case "a":` branch with the one below. The Add link carries the list it was opened from (Task 5):

```js
		case "a":
			var add = document.getElementById("books-add");
			if (!add) return false;
			window.location.href = add.href;
			return true;
```

   - Delete the `case "Escape":` branch. `closeDrawer` handles it now.

4. In the `keydown` listener, replace everything from `if (isTyping(e.target)) {` up to the closing `}` of that `if` with:

```js
		// Esc closes the phone drawer from anywhere, its own checkbox and the
		// links inside it included, before anything else sees the key (#592).
		if (e.key === "Escape" && closeDrawer(e.target)) {
			e.preventDefault();
			return;
		}
		if (isTyping(e.target)) {
			if (e.key !== "Escape") return;
			if (e.target.id === "books-q" || e.target.id === "books-progress-input") {
				e.target.blur();
				return;
			}
			// Esc from a field in an open popover or edit form of the book
			// page (Finish…, Did not finish…, the review, an entry's ✎) closes
			// it and returns focus to its summary.
			var open = e.target.closest("#books-book details[open]:not(.books-history)");
			if (open) {
				open.open = false;
				var summary = open.querySelector("summary");
				if (summary) summary.focus();
			}
			return;
		}
```

Then check:

Run: `grep -n 'books-drawer\|Escape\|books/new' internal/apps/books/static/books.js`
Expected:
- `books-drawer` appears only in `closeDrawer`.
- `Escape` appears in the keydown listener and in `bookKey`.
- No `/books/new` is left.

- [ ] **Step 4: Check it in the browser**

Rebuild (`go build -o /tmp/onsuite-bin ./cmd/onsuite`) and restart the preview. On `/books/b/1?shelf=reading`:

- **Format:** focus the format select with the keyboard and pick another format with the arrow keys. After the swap, focus is back on the select.
- **Finish…:** open it, focus its date and press Esc. The popover closes and focus is on **Finish…**. Press Esc again, and the page goes back to the list.
- **Reading history:** open it and press Esc (focus on the page, not a field). The history stays open and the page goes back to the list. Open a reading's ✎ and press Esc. Only that form closes.
- **Notes and quotes:** `n` opens the note form with the cursor in it, and `q` opens the quote form. `p` focuses the progress box. Type a page, then press Enter. The section swaps and focus is back in the box, selected.
- **Home, desktop:** on a tag list, press `a`. Add opens with "← Tagged “…”".
- **Home, `mobile` preset:**
  - Open the drawer, Tab onto a shelf link and press Esc. The drawer closes, and the drawer button shows its focus ring.
  - Open it again, focus the checkbox (the button) and press Esc. Same result.
  - With the drawer closed, Esc in the search box only blurs it.

Reset the viewport with preset `desktop`. Check `read_console_messages` for errors.

- [ ] **Step 5: Run the full check and commit**

```bash
gofmt -l . && go vet ./... && go run honnef.co/go/tools/cmd/staticcheck@v0.8.0 ./... && go test ./... -race -count=1
git add internal/apps/books/static/books.js
git commit -m "fix(books): Esc closes the phone drawer from inside it (#592); keys for the new sections"
```

---
### Task 9: The rest of #594: test gaps, test names, and the pressed view while searching

Earlier tasks already covered three #594 items:
- the search box is never swapped (Task 5)
- the CSRF field on the form Find cover posts (Task 6)
- the phone's Add padding (Task 7)

This task does the rest:
- **One fix.** While a search shows the list, the disabled view switch shows the list as pressed, not the saved view. `aria-pressed` describes what's on screen.
- **Coverage gaps.** A missing Default-reversed test for the other four shelves, and a length check that panics instead of failing.
- **Tidying.** A wrong failure message, and test names that still say "pane".

**Files:**
- Modify: `internal/apps/books/view.go` (`prefsView`, `viewPrefs`)
- Test: `internal/apps/books/prefs_view_test.go`, `internal/apps/books/sort_test.go`, `internal/apps/books/progress_view_test.go`, and the files with "pane" test names (Step 4)

**Interfaces:**
- Consumes: `prefsView` and `viewPrefs` (UI1); `newFixture`, `addedInTurn`, `onShelf` and `list` (store tests).
- Produces: `prefsView.View`, which now means "the view the switch shows as pressed".

- [ ] **Step 1: Write the failing test for the pressed view**

In `internal/apps/books/prefs_view_test.go`, add:

```go
func TestSearchingPressesTheListButton(t *testing.T) {
	s := newServer(t)
	add(t, s, s.Alice.User.ID, titled("Dune", "", books.ShelfWant))
	doc := s.Get(t, s.Alice, "/books/?shelf=all&q=dune") // saved view: the default grid
	if got := attr(t, doc, "#books-view-list", "aria-pressed"); got != "true" {
		t.Errorf("list button aria-pressed while searching = %q, want true: the list is what shows (#594)", got)
	}
	if got := attr(t, doc, "#books-view-grid", "aria-pressed"); got != "false" {
		t.Errorf("grid button aria-pressed while searching = %q, want false", got)
	}
	if p := prefsOf(t, s); p.View != books.ViewGrid {
		t.Errorf("saved view = %q after searching, want grid unchanged", p.View)
	}
}
```

Run: `go test ./internal/apps/books/ -run TestSearchingPressesTheListButton -count=1`
Expected: FAIL. `#books-view-list` aria-pressed is `"false"`.

- [ ] **Step 2: Press what shows**

In `internal/apps/books/view.go`, in `prefsView`, change the `View` field's comment to `// the view the switch shows as pressed: the saved one, or the list while a search shows it`. In `viewPrefs`, after the `for` loop, before `return v`, add:

```go
	if v.ViewFixed { // a search shows the list whatever is saved (#594)
		v.View = ViewList
	}
```

Run: `go test ./internal/apps/books/ -run 'Pressed|Prefs|Search' -count=1`
Expected: PASS.

- [ ] **Step 3: Close the store-test gaps**

In `internal/apps/books/sort_test.go`:

1. In `TestSortEqualTitlesKeepAddedOrder`, replace:

```go
			if len(items) != 2 || items[0].ID != ids[0] || items[1].ID != ids[1] {
```

   with:

```go
			if len(items) != 2 {
				t.Fatalf("%s (reversed %v): %d books, want 2", by, reversed, len(items))
			}
			if items[0].ID != ids[0] || items[1].ID != ids[1] {
```

2. Add:

```go
// reversedCopy is s back to front, leaving s alone.
func reversedCopy(s []string) []string {
	r := slices.Clone(s)
	slices.Reverse(r)
	return r
}

func TestDefaultSortReversedOnEveryShelf(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	uid := f.alice.ID
	// Reading goes by the latest progress: A was started first, read last.
	ra := addedInTurn(t, f, onShelf("Reading A", books.ShelfReading))
	addedInTurn(t, f, onShelf("Reading B", books.ShelfReading))
	if err := f.store.RecordProgress(ctx, uid, ra, 10); err != nil {
		t.Fatal(err)
	}
	f.now = f.now.Add(time.Minute)
	// Want to read goes by date added.
	addedInTurn(t, f, onShelf("Want A", books.ShelfWant))
	addedInTurn(t, f, onShelf("Want B", books.ShelfWant))
	// Did not finish goes by the latest change: A was put down first, then
	// tagged, which makes it the latest.
	da := addedInTurn(t, f, onShelf("DNF A", books.ShelfReading))
	db := addedInTurn(t, f, onShelf("DNF B", books.ShelfReading))
	for _, id := range []int64{da, db} {
		if err := f.store.MarkDNF(ctx, uid, id, "", 0); err != nil {
			t.Fatal(err)
		}
		f.now = f.now.Add(time.Minute)
	}
	if err := f.store.SetTags(ctx, uid, da, []string{"x"}); err != nil {
		t.Fatal(err)
	}

	for _, tt := range []struct {
		shelf books.Shelf
		want  []string
	}{
		{books.ShelfReading, []string{"Reading A", "Reading B"}},
		{books.ShelfWant, []string{"Want B", "Want A"}},
		{books.ShelfDNF, []string{"DNF A", "DNF B"}},
	} {
		q := books.ListQuery{Shelf: tt.shelf}
		if got := list(t, f, q); !slices.Equal(got, tt.want) {
			t.Errorf("%s, Default = %v, want %v", tt.shelf, got, tt.want)
		}
		q.Reversed = true
		if got, want := list(t, f, q), reversedCopy(tt.want); !slices.Equal(got, want) {
			t.Errorf("%s, Default reversed = %v, want %v", tt.shelf, got, want)
		}
	}
	// All goes by the latest change across the shelves.
	all := list(t, f, books.ListQuery{Shelf: books.ShelfAll})
	if len(all) != 6 || all[0] != "DNF A" {
		t.Fatalf("All, Default = %v, want 6 books with DNF A (tagged last) first", all)
	}
	if got, want := list(t, f, books.ListQuery{Shelf: books.ShelfAll, Reversed: true}), reversedCopy(all); !slices.Equal(got, want) {
		t.Errorf("All, Default reversed = %v, want %v", got, want)
	}
}
```

Run: `go test ./internal/apps/books/ -run 'TestDefaultSortReversed|TestSortEqualTitles' -count=1`
Expected: PASS. These tests cover behaviour that is already right; they pin it.

In `internal/apps/books/progress_view_test.go`, in `TestRowsShowProgressAndStars`, change the message `"%d row bars, want 1: no bar before any progress"` to `"%d cover bars, want 1: no bar before any progress"`.

- [ ] **Step 4: Rename the "pane" tests**

The book has a page of its own now. Rename the tests that still say "pane":

| File | Old name | New name |
|---|---|---|
| `actions_test.go` | `TestBookPaneOffersTheRightReadingActions` | `TestBookPageOffersTheRightReadingActions` |
| `covers_test.go` | `TestCoversShowInTheListAndTheBookPane` | `TestCoversShowInTheListAndOnTheBookPage` |
| `form_test.go` | `TestBookPaneLinksToEdit` | `TestBookPageLinksToEdit` |
| `notes_view_test.go` | `TestTheBookPaneShowsNotesNewestFirst` | `TestTheBookPageShowsNotesNewestFirst` |
| `notes_view_test.go` | `TestEditingAndDeletingANoteInThePane` | `TestEditingAndDeletingANoteInPlace` |
| `quotes_view_test.go` | `TestEditingAndDeletingAQuoteInThePane` | `TestEditingAndDeletingAQuoteInPlace` |
| `history_view_test.go` | `TestTheBookPaneListsEveryReading` | `TestTheBookPageListsEveryReading` |

```bash
cd internal/apps/books
perl -pi -e 's/TestBookPaneOffers/TestBookPageOffers/; s/TestCoversShowInTheListAndTheBookPane/TestCoversShowInTheListAndOnTheBookPage/; s/TestBookPaneLinksToEdit/TestBookPageLinksToEdit/; s/TestTheBookPaneShows/TestTheBookPageShows/; s/ANoteInThePane/ANoteInPlace/; s/AQuoteInThePane/AQuoteInPlace/; s/TestTheBookPaneLists/TestTheBookPageLists/' *_test.go
cd -
```

Run: `grep -rn 'BookPane\|InThePane' internal/apps/books/*_test.go`
Expected: no output.

- [ ] **Step 5: Run the full check and commit**

```bash
gofmt -l . && go vet ./... && go run honnef.co/go/tools/cmd/staticcheck@v0.8.0 ./... && go test ./... -race -count=1
git add internal/apps/books
git commit -m "test(books): the rest of #594 — Default reversed on every shelf, pressed view while searching, names"
```

---
### Task 10: The user guide, the screenshots and the README

The guide still describes three panes and the ⋯ menu. This task rewrites the library and book parts for the two-pane home and the book page. It then retakes the Books screenshots: `books-panes.png` and `books-menu.png` are replaced by `books-home.png`, `books-home-list.png` and `books-book.png`, and `books-add.png` is retaken for its page bar. The README's ON Books picture moves to the home, which shows the app best now (spec "Docs").

**Files:**
- Modify: `docs/user/books.md`
- Modify: `docs/screenshots/capture/shots.go`
- Modify: `README.md`
- Create: `docs/user/images/books-home.png`, `docs/user/images/books-home-list.png`, `docs/user/images/books-book.png`
- Modify: `docs/user/images/books-add.png`, `docs/images/app-books-light.png`, `docs/images/app-books-dark.png`
- Delete: `docs/user/images/books-panes.png`, `docs/user/images/books-menu.png`

**Interfaces:**
- Consumes: everything Tasks 1–9 built, as it looks in the browser.
- Produces: nothing for later tasks.

- [ ] **Step 1: Rewrite the library and book parts of the guide**

In `docs/user/books.md`, replace everything from the first line down to (not including) `## Importing from Goodreads` with:

```markdown
# ON Books

ON Books is a private reading log: what you're reading, what you've read,
what you want to read next and what you gave up on. No friends, no feed —
just your own books.

![ON Books: the shelves on the left and the books you're reading as a grid of covers](images/books-home.png)

## Shelves

Every book sits on one shelf:

- **Reading** — books you've started. ON Books opens here.
- **Want to read** — books you haven't started yet.
- **Read** — books you finished.
- **Did not finish** — books you put down.

**All books** shows every shelf at once. The number next to each shelf is
how many books are on it.

You never move a book between shelves yourself: its shelf follows what you
do with it. Start reading it and it moves to Reading; finish it and it moves
to Read.

## Your library

The sidebar lists your shelves. Under them is this year's reading goal, if
you've set one, and then your tags. At the bottom are **Stats**, **Import
from Goodreads** and **Export as Markdown**.

The books on the shelf you picked fill the rest of the page. Each one shows
its cover, title and author, and how it's going: "Reading · 45%" for a book
on the go, your stars for one you've read. Then a few of its tags, and the
day you added it (on the Read shelf, the day you finished it). Click a book
to open it.

Above the books is a toolbar with the search box, the sort, the view and
**+ Add book**:

- **Grid or list.** The two buttons by **+ Add book** show the books as a
  grid of covers or as a list. ON Books remembers your choice.
- **Sort.** Choose **Default**, **Date added**, **Title** or **Author**.
  The arrow next to it reverses the order. Default is each shelf's own
  order: Reading puts the book you read most recently first, Read the one
  you finished most recently, Want to read the one you added most recently,
  and the others the one you changed most recently. **Title** ignores a
  leading "The", "A" or "An", and **Author** goes by the first author's
  surname. ON Books remembers your sort too. A series is always listed in
  reading order.

![The list view: a row for each book, with its progress or stars on the right](images/books-home-list.png)

## Adding a book

Click **+ Add book** in the toolbar. The arrow at the top of the page, and
**Cancel**, take you back to the list you came from.

The quickest way is to find the book on [Open Library](https://openlibrary.org),
a free catalogue of books: type its title, its author or its ISBN into
**Find it on Open Library** and click **Search**. Pick the right one from
the results with **Use this**. The form below fills in with what Open
Library knows — title, author, the year it first came out, the number of
pages, the ISBN and a short description — and the book's cover comes with
it when you add it. Change anything you like before you do.

If the book isn't there, or Open Library doesn't answer, fill the form in
yourself. Only the title is required. An ISBN can be the 10- or 13-digit
kind, with or without hyphens; ON Books keeps it as 13 digits.

![The Add book page: an Open Library search above the book form](images/books-add.png)

Then choose where the book goes:

- **Want to read** — it waits on that shelf.
- **Reading now** — you started it today.
- **Already read** — pick the day you finished it.

You can add tags at the same time, separated by commas.

## A book

A book has a page of its own. The arrow at the top left takes you back to
the list you opened it from. At the top right, ✎ edits the book's details
and 🗑 deletes it.

Under the title are the book's shelf and its tags. Click a tag to see every
book with it. Then come the book's sections: **Reading**, **Your take**,
**About** (its description), **Notes**, **Quotes** and **Reading history**.
A long description is cut short after a few lines; **More** shows the rest.

The rule is simple. The facts about a book — its title, author, series,
cover, tags and description — are changed with ✎, on the Edit page.
Everything about your reading of it is changed right on its page.

![A book's page: its title and tags, then Reading, Your take, About and Notes](images/books-book.png)

## Reading a book

The **Reading** section starts and finishes your readings:

- **Start reading** starts it today and moves it to Reading.
- **Finish…** asks for the day you finished — today, unless you change
  it — and, if you like, a rating; then it moves the book to Read.
- **Did not finish…** asks for the day you stopped and, if you like, the
  page you got to, and moves the book to Did not finish.

A finished book offers **Read again**, and a book you put down offers
**Start again**: either starts a new reading, and the earlier one is kept.
You read a book one time at a time: while you're reading it there's no start
button; finish it or put it down first.

### Progress

While you're reading a book, type the page you're on into the **Page** box
and press Enter, or click ✓. The thin bar above it, and the book's cover on
the Reading shelf, show how far through you are.

For an audiobook — or a book with no page count — the box asks for a
percentage instead. Reaching the last page doesn't finish the book: click
**Finish…** when you're done.

ON Books keeps every update you make. You only see the latest, but they're
what [your stats](#reading-goal-and-stats) count pages from.

### Format

The menu next to "Since…" says how you're reading the book: on paper, as an
ebook, or as an audiobook. Pick another to change it. A new reading starts
with the format of the one before.

## Rating and review

Click a star in **Your take** to rate a book from one to five; click the
same star again to clear it. You can also rate a book as you finish it. The
Read shelf shows each book's stars and the day you finished it.

**Write a review** opens a box for what you thought. You can use a little
Markdown: `**bold**`, `*italic*`, `[a link](https://example.com)`, and a
blank line to start a new paragraph. **Save review** keeps it. Once there's
a review, ✎ at the right of Your take changes it. A book has one rating and
one review, however many times you read it.

## Notes and quotes

**+** at the right of **Notes** opens a box for a note. Give it a page if
you like, and it shows as "p. 112". Notes take the same little Markdown as
reviews, and the newest comes first, with the day you wrote it.

**Quotes** are passages you want to keep. **+** at the right of Quotes opens
a box for the words, an optional comment and an optional page. A quote
keeps its line breaks exactly as you typed them, so poetry stays poetry;
the comment takes Markdown.

Point at a note or quote to see ✎ and 🗑 at its top right. On a phone or
tablet they're always there. ✎ changes it in place; 🗑 removes it, after
asking. A page has to be one the book has — from 1 to its page count.

## Reading history

At the bottom of a book is **Reading history**, folded away; click it to
see every time you've read the book, newest first: when you started and
finished, the format, and how it ended. ✎ changes a reading's dates and
format. To fix a reading that ended the wrong way — marked as read when you
gave up, say — delete it with 🗑; the book's shelf follows whichever reading
is left.

## Covers

A book added from Open Library brings its cover. A book without one shows
a coloured spine with its title instead.

To change a cover, open the book, click ✎, and under **Cover** either
upload an image or paste the address of one on the web. JPEG, PNG, GIF and
WebP images up to 2 MB work. **Remove the cover** goes back to the spine.
ON Books keeps its own copy of every cover, so they show even if the
original disappears.

A book without a cover offers **Find a cover on Open Library** under
**Cover** on the Edit page. It asks Open Library for one straight away: by
the ISBN, or by the title and author when there's no ISBN or Open Library
has no cover for it. It uses what's in the form, even if you haven't saved
it yet. The cover is saved as soon as it's found; anything else you've
changed stays in the form, waiting for **Save**. If there's no cover to be
had, ON Books says so, and you can add one yourself.

Books with an ISBN but no cover — books you imported, say — get theirs in
the background, a few every few minutes, so a big import fills in over a
few hours. A cover you remove stays removed.

## Series

If a book is part of a series, its series and number show under the
author — "The Expanse #1 · 2 books". Click it to list the series in
order. Add a series name and number with ✎ on the book's page.

## Tags

Tags are your own labels — *sf*, *book club*, *favourites*. Add them as you
add a book, or change them with ✎ on the book's page: the **Tags** box
takes them separated by commas. Your tags are listed in the sidebar; click
one, or a tag on a book's page, to see every book with it. A tag nobody
uses any more disappears.

## Finding a book

Type in the search box above your books to search them: their titles,
subtitles, authors and series, and everything you've written about them —
reviews, notes, quotes and quote comments. Every word you type has to
match, and the list narrows while you're still typing a word.

While you're searching, the books show as a list, whichever view you
picked, so there's room to show where each one matched. When a book
matched inside a review, note or quote, its row gets a line showing where,
with the matching words highlighted — "Quote: The spice must flow."
Clear the box to get your view back.

The search stays within the shelf, tag or series you're looking at, and
stays as you switch shelves.

## Editing and deleting

On a book's page, ✎ at the top right changes its details: the title,
author and the rest, its cover and its tags. 🗑 removes the book for good,
with its readings, notes and quotes, after asking.

```

Then:

1. In `### A yearly goal`, replace the paragraph `While this year has a goal, it also shows at the top of the Reading shelf. Click it to go to your stats.` with:

```markdown
While this year has a goal, a small card in the sidebar shows how you're
doing. Click it to go to your stats.
```

2. Replace everything from `## Keyboard shortcuts` to the end of the file with:

```markdown
## Keyboard shortcuts

In your library:

- **j** / **k** — move to the next / previous book
- **Enter** or **o** — open it
- **/** — jump to the search box
- **a** — add a book

On a book's page:

- **p** — jump to the progress box
- **n** — add a note
- **q** — add a quote
- **e** — edit the book's details
- **Esc** — close an open box (Finish…, Did not finish…, the review, or a
  note, quote or reading you're changing); with none open, go back to your
  library

## On a phone

On a narrow screen the sidebar folds away. The button at the top left,
named after the shelf you're on, slides it in; pick a shelf or tag, tap
beside it or press Esc to close it again. A book's page is the same single
column as on a computer, with the cover above the title.
```

Then check that nothing of the old layout is left in the guide:

Run: `grep -n '⋯\|books-panes\|books-menu\|Save tags\|top of the sidebar\|open it on the right' docs/user/books.md`
Expected: no output.

- [ ] **Step 2: Update the screenshot list**

In `docs/screenshots/capture/shots.go`:

1. After the `readerScrollToActive` const, add:

```go
// booksListView shows a Books shelf as the list the way a person would:
// it presses the list button (htmx saves the view and swaps the list in).
// It then saves grid again without redrawing, so the other Books shots,
// and a later --only run, still find the seeded default.
const booksListView = `
		document.getElementById('books-view-list').click();
		await new Promise(r => setTimeout(r, 1200));
		const form = document.getElementById('books-prefs');
		const data = new URLSearchParams(new FormData(form));
		data.set('view', 'grid');
		await fetch(form.action, {method: 'POST', body: data});`
```

2. Replace the `// docs/user/books.md` group with:

```go
	// docs/user/books.md
	// Book 1 is Leviathan Wakes, on the Reading shelf in the seed. Nothing
	// here imports a file or presses Find cover, so no request reaches
	// Open Library and the library stays as seeded; the list shot puts the
	// saved view back to grid (booksListView).
	{Name: "docs/user/images/books-home.png", URL: "/books/?shelf=reading", Height: 640},
	{Name: "docs/user/images/books-home-list.png", URL: "/books/?shelf=read", Height: 560, Setup: booksListView},
	{Name: "docs/user/images/books-book.png", URL: "/books/b/1?shelf=reading", Height: 1100},
	{Name: "docs/user/images/books-add.png", URL: "/books/new?shelf=reading", Height: 720},
	{Name: "docs/user/images/books-stats.png", URL: "/books/stats", Height: 980},
	{Name: "docs/user/images/books-import.png", URL: "/books/import", Height: 560},
```

3. In the README group, change both `app-books-*` URLs from `/books/b/1?shelf=reading` to `/books/?shelf=reading`.

In `README.md`, change the ON Books image's alt text to `ON Books: the shelves on the left and the books being read as a grid of covers, each with its progress`.

- [ ] **Step 3: Capture the pictures**

From the repository root, following `docs/screenshots/README.md`. Use the latest release without its `v` as the footer version (`5.0.0` at the time of writing; check `git tag --sort=-v:refname | head -1`):

```bash
VERSION=5.0.0
SEED=$(mktemp -d)/demo
go run ./docs/screenshots/seed --data-dir $SEED
go build -ldflags "-X main.version=$VERSION" -o $SEED/onsuite ./cmd/onsuite
$SEED/onsuite serve --addr :8308 --data-dir $SEED &
SERVER=$!
until curl -fs http://localhost:8308/healthz >/dev/null; do sleep 0.2; done
go run ./docs/screenshots/capture --session-file $SEED/demo-session \
  --only books-home.png,books-home-list.png,books-book.png,books-add.png,app-books-light.png,app-books-dark.png
kill $SERVER
git rm docs/user/images/books-panes.png docs/user/images/books-menu.png
```

Look at each new PNG:
- **`books-home.png`:** the sidebar and a grid of covers, cropped below the first row or two.
- **`books-home-list.png`:** rows on the Read shelf, with stars.
- **`books-book.png`:** the header and at least Reading, Your take and the start of Notes.
- **`books-add.png`:** the page bar reads "← Reading".
- **The README pair:** both themes.

If a crop cuts something awkwardly, adjust that shot's `Height` and capture it again with `--only`.

Run: `go test ./docs/screenshots/... -count=1`
Expected: PASS. `TestShotIDsPointAtTheIntendedItems` still finds book 1, which is Leviathan Wakes.

- [ ] **Step 4: Run the full check and commit**

```bash
gofmt -l . && go vet ./... && go run honnef.co/go/tools/cmd/staticcheck@v0.8.0 ./... && go test ./... -race -count=1
git add docs/user/books.md docs/screenshots/capture/shots.go README.md docs/user/images docs/images/app-books-light.png docs/images/app-books-dark.png
git commit -m "docs(books): the guide and screenshots for the two-pane home and the book page"
```

---
### Task 11: Final pass

- [ ] **Step 1: Read the diff with the spec at hand**

Run `git diff main --stat` and read the whole diff. Against the spec's UI2 row, check that each of these is in place:

- **The six sections** in order, each with a hairline, its heading, its count and at most one action.
- **The new swaps:**
  - progress → Reading
  - rating and review → Your take
  - notes and quotes → their own sections
  - start, finish, DNF, format and readings → the page body
  - delete → `HX-Redirect`
- **The entries' ✎ / 🗑 icons:** shown on hover and focus, and always on touch.
- **Tags on Edit:** the tags route and form are gone, and the header pills are links.
- **Find cover on Edit** keeps what was typed (#591).
- **The page bar on Add and Edit:** Add comes back to its list.
- **The keyboard:** `p`, `n`, `q`, `e`, and Esc closing popovers before going back, plus #592.
- **The user guide and screenshots.**

Also check the spec's "Testing" list:
- Every icon-only button has a name (`TestDetailsIconButtonsHaveNames`).
- The details page has no ⋯ menu and no tags form (`TestBookPageShowsTheBook`, `TestTheBookPageHasNoTagsForm`).
- Edit saves tags (`TestEditSavesTags`).
- Find cover comes back to Edit (`TestFindCoverKeepsWhatWasTyped`).
- The removed route 404s.

Then check that nothing refers to what this phase removed:

Run: `grep -rn 'reading-box\|format-menu\|progress-swap\|tags-form\|setTags\|UpdateWithCover\|form="books-find-cover"\|found=1\|FormatLabel\|TagsValue\|Cancel:' internal/apps/books internal/ui docs/screenshots`
Expected: no output.

- [ ] **Step 2: Run the full check**

```bash
gofmt -l .
go vet ./...
go run honnef.co/go/tools/cmd/staticcheck@v0.8.0 ./...
go mod tidy && git diff --exit-code go.mod go.sum
go test ./... -race -count=1
```

Expected: no gofmt output, and everything else ok.

- [ ] **Step 3: One last look in both themes**

With the seeded preview from Task 7:
1. Open a book in light and dark, at desktop and `mobile` widths.
2. On a phone width, save progress with ✓ only, without Enter. The iOS number pad has no Enter key.
3. Open the drawer and close it with Esc.
4. Reset the viewport to `desktop`.

- [ ] **Step 4: Hand over**

Use `superpowers:finishing-a-development-branch`. The PR title is `feat(books): the book page's sections, tags and Find cover on Edit (UI2)`.

The body summarises the tasks and links the spec and this plan. It attaches the new `books-book.png` and a phone screenshot of the book page. Use one `closes` keyword per issue: `closes #591`, `closes #592`, `closes #593`, `closes #594`.

It also notes the decisions made on 2026-10-12:
- Find cover carries the form, and looks up what the form says.
- Your take has one opener.
- The format icons.
- Two additions beyond the spec:
  - a rating brings the Reading section along out of band, so the Finish… popover's rating stays current;
  - progress swaps the whole Reading section, so Did not finish… offers the page just saved.

Don't merge: Ilia merges.
