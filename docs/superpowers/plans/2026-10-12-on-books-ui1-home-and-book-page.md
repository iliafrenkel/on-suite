# ON Books UI1: two-pane home and a book page of its own. Implementation plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace ON Books' three panes with a two-pane home (sidebar plus a grid or list, with saved view and sort) and turn the book into a full-width page with a page bar.

**Architecture:** The data model doesn't change. One new table (`books_prefs`) holds each user's view and sort. `Store.List` learns the four sorts, and a new `Store.TagsByBook` feeds tags to the tiles. `renderPanes` is split in two:

- **`renderHome`** draws the home page. Its htmx fragment is `list-swap`, which brings the sidebar, toolbar and drawer checkbox along out of band.
- **`renderBook`** draws `/books/b/{id}` as its own page. Its htmx fragments are `book-swap`, `progress-swap`, `notes-swap` and `quotes-swap`, and none of them carries the list any more.

Templates split the same way:

- `home.partial.html` replaces `panes.partial.html`.
- `book.partial.html` holds the book blocks, moved verbatim apart from the header and the page bar.
- `book.html` is the new page.

Find cover moves from the old ⋯ menu to the Edit page.

**Tech Stack:** Go `net/http`, `html/template`, htmx (vendored), SQLite (modernc), and vanilla CSP-clean JS. There is no build step.

**Spec:** [docs/superpowers/specs/2026-10-12-on-books-ui-redesign-design.md](../specs/2026-10-12-on-books-ui-redesign-design.md). The mockups are next to it. This plan is phase **UI1** of the spec's "Phases" table.

## Global Constraints

- **Tokens only.** Add no new colours or spacing values (app.css header: "Apps must not introduce new colours or spacing values"). Use `--c-*`, `--s-*`, `--fs-*`, `--radius` and `swatch-c-*`.
- **No inline `<script>` and no `style=`.** The strict CSP forbids them. JS goes in `internal/apps/books/static/books.js`, CSS in `internal/ui/static/app.css`.
- **No JavaScript or CSS build step**, and no new Go dependency.
- **Every form works without JavaScript.** Give it a real `method`/`action`, with `hx-post` identical to `action` (PATTERNS.md, "hx-post mirrors formaction").
- **Apps never import each other.** Mirroring ON Later's prefs is done by copying, and the doc comment says so.
- **Migrations are forward-only.** The new one is `internal/apps/books/migrations/0008_prefs.sql`.
- **Time:** app code reads time only through `st.now()`. Stored timestamps go through `formatTime` (`db.TimeLayout`).
- **Full check stays green on every commit:**
  - `gofmt -l .` prints nothing
  - `go vet ./...`
  - `go run honnef.co/go/tools/cmd/staticcheck@v0.8.0 ./...`
  - `go test ./... -race -count=1`
- **Introduce a helper in the task that first calls it.** staticcheck U1000 rejects unused unexported code.
- **`internal/htmlassert` takes one qualifier per selector part.** Write `li[data-book-id="3"] .books-item-status`, never `.books-item[data-book-id="3"]`, `#a.b` or `[a][b]`.
- **Sort option labels are verbatim:** `Default`, `Date added`, `Title`, `Author`.
- **Natural directions:** Default and Date added are newest first; Title and Author are A–Z. The direction labels read `Newest first` / `Oldest first` and `A–Z` / `Z–A`.
- **Tag limits:** a grid tile shows at most **3** tags and a list row at most **5**, then a `+N` pill.
- **Tile date:** `Added 3 Oct 2026`, or on the Read shelf `Finished 2 Sep 2026`.
- **Commits:** Conventional Commits with scope `books` (`ui` for the icon map). Work on a branch (the executing skill creates it). Never push to `main`.

## Review Focus

Each of these has a test in the task named:

- **Clearing a search over htmx** (`q=` empty, target `#books-list`) brings the saved grid back, and searching never changes the saved view (Task 4, `TestSearchShowsTheListWithItsSnippets`).
- **A tampered prefs post** (`view=table`, `sort=rating`, `reversed=2`) is a 400 and saves nothing (Task 5, `TestPrefsRefuseWhatTheToolbarNeverSends`).
- **A series list keeps reading order** whatever the saved sort, and its sort controls are disabled (Task 2, `TestASeriesIgnoresTheSort`; Task 4, `TestSortControlsFollowThePrefs`).
- **Deleting from the book page over htmx** lands on the list it came from, search and tag intact (Task 3, `TestDeleteOverHTMXGoesBackToTheList`).
- **The back and Edit links keep the list context**, including a search with a space in it (Task 3, `TestBookPageGoesBackToItsList`).

---

## File structure

| File | Change | Responsibility |
|---|---|---|
| `internal/apps/books/migrations/0008_prefs.sql` | create | the `books_prefs` table |
| `internal/apps/books/prefs.go` | create | `View`, `Sort`, `Prefs`, `Store.Prefs`/`SetPrefs`, and (Task 5) the `setPrefs` handler |
| `internal/apps/books/sort.go` | create | the Title and Author sort keys and `sortItems` |
| `internal/apps/books/library.go` | modify | `ListQuery.Sort`/`Reversed`, `listOrder`, and the Go sort in `List` |
| `internal/apps/books/tag.go` | modify | `Store.TagsByBook` |
| `internal/apps/books/handlers.go` | modify | `renderHome` and `renderBook` replace `renderPanes`; `sidebarGoal` |
| `internal/apps/books/actions.go` | modify | `act`, `progress` and `saveEntry` answer with the book page; delete is an `HX-Redirect` |
| `internal/apps/books/view.go` | modify | `homeView`, `itemView`, `prefsView` and `bookPageView`; `rowView`/`panesView` removed |
| `internal/apps/books/findcover.go` | modify | `findCover` becomes a handler that returns to the Edit page |
| `internal/apps/books/form.go` | modify | `formView.ID`, `Found` and `CoverNotice` |
| `internal/apps/books/books.go` | modify | routes: `POST /prefs`, `POST /find-cover/{id}` |
| `internal/apps/books/templates/home.partial.html` | create (Task 4) | the home page blocks |
| `internal/apps/books/templates/book.partial.html` | create (Task 3) | the book page blocks |
| `internal/apps/books/templates/book.html` | create (Task 3) | the book page |
| `internal/apps/books/templates/panes.partial.html` | trim (Task 3), delete (Task 4) | |
| `internal/apps/books/templates/index.html` | modify (Task 4) | renders `home` |
| `internal/apps/books/templates/form.html` | modify (Task 6) | Find cover in the Cover fieldset |
| `internal/apps/books/static/books.js` | rewrite (Task 8) | confirm, sidebar gutter, prefs focus, keyboard |
| `internal/ui/static/app.css` | modify (Task 7) | the ON Books block |
| `internal/ui/toolbar_icons.go`, `internal/ui/toolbar_icons_test.go` | modify (Task 4) | the `grid`, `list`, `arrow-up` and `arrow-down` icons |
| `internal/apps/books/*_test.go` | modify/create | as each task says |

---

### Task 1: Preferences store

**Files:**
- Create: `internal/apps/books/migrations/0008_prefs.sql`
- Create: `internal/apps/books/prefs.go`
- Test: `internal/apps/books/prefs_test.go`

**Interfaces:**
- Produces:
  - `type View string`, with `ViewGrid` and `ViewList`
  - `type Sort string`, with `SortDefault`, `SortAdded`, `SortTitle` and `SortAuthor`
  - `var Sorts []Sort` and `func (Sort) Label() string`
  - `type Prefs struct{ View View; Sort Sort; Reversed bool }` and `var DefaultPrefs`
  - `func (Prefs) Valid() bool`
  - `func (*Store) Prefs(ctx, userID int64) (Prefs, error)`
  - `func (*Store) SetPrefs(ctx, userID int64, p Prefs) error`, which returns `ErrInvalid` for invalid prefs

- [ ] **Step 1: Write the failing tests**

Create `internal/apps/books/prefs_test.go`:

```go
package books_test

import (
	"context"
	"errors"
	"testing"

	"github.com/iliafrenkel/on-suite/internal/apps/books"
)

func TestPrefsAreTheDefaultsUntilSet(t *testing.T) {
	f := newFixture(t)
	if want := (books.Prefs{View: books.ViewGrid, Sort: books.SortDefault}); books.DefaultPrefs != want {
		t.Errorf("DefaultPrefs = %+v, want grid, Default, not reversed (spec \"Preferences\")", books.DefaultPrefs)
	}
	got, err := f.store.Prefs(context.Background(), f.alice.ID)
	if err != nil || got != books.DefaultPrefs {
		t.Errorf("Prefs before any save = %+v, %v; want the defaults", got, err)
	}
}

func TestSetPrefsIsPerUserAndReplaces(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	want := books.Prefs{View: books.ViewList, Sort: books.SortTitle, Reversed: true}
	if err := f.store.SetPrefs(ctx, f.alice.ID, want); err != nil {
		t.Fatal(err)
	}
	if got, err := f.store.Prefs(ctx, f.alice.ID); err != nil || got != want {
		t.Errorf("Alice's prefs = %+v, %v; want %+v", got, err, want)
	}
	if got, _ := f.store.Prefs(ctx, f.bob.ID); got != books.DefaultPrefs {
		t.Errorf("Bob's prefs = %+v, want the defaults: Alice's are her own", got)
	}
	again := books.Prefs{View: books.ViewGrid, Sort: books.SortAuthor}
	if err := f.store.SetPrefs(ctx, f.alice.ID, again); err != nil {
		t.Fatal(err)
	}
	if got, _ := f.store.Prefs(ctx, f.alice.ID); got != again {
		t.Errorf("after a second save = %+v, want %+v", got, again)
	}
}

func TestSetPrefsRefusesWhatTheHomePageHasNoControlFor(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	for _, p := range []books.Prefs{
		{View: "table", Sort: books.SortDefault},
		{View: books.ViewGrid, Sort: "rating"},
		{},
	} {
		if err := f.store.SetPrefs(ctx, f.alice.ID, p); !errors.Is(err, books.ErrInvalid) {
			t.Errorf("SetPrefs(%+v) = %v, want ErrInvalid", p, err)
		}
	}
	if got, _ := f.store.Prefs(ctx, f.alice.ID); got != books.DefaultPrefs {
		t.Errorf("prefs after refused saves = %+v, want the defaults", got)
	}
}

func TestPrefsGoWithTheirUser(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	if err := f.store.SetPrefs(ctx, f.bob.ID, books.Prefs{View: books.ViewList, Sort: books.SortAdded}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Exec(`DELETE FROM users WHERE id = ?`, f.bob.ID); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := f.db.QueryRow(`SELECT count(*) FROM books_prefs`).Scan(&n); err != nil || n != 0 {
		t.Errorf("books_prefs rows after deleting Bob = %d, %v; want 0 (ON DELETE CASCADE)", n, err)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/apps/books/ -run 'Prefs' -count=1`
Expected: a build failure, `undefined: books.Prefs` (and the other new names).

- [ ] **Step 3: Write the migration**

Create `internal/apps/books/migrations/0008_prefs.sql`:

```sql
-- The home page's view and sort, one row per user (UI redesign spec
-- "Preferences"). No row means the defaults: grid, Default, not reversed.
CREATE TABLE books_prefs (
    user_id    INTEGER PRIMARY KEY REFERENCES users (id) ON DELETE CASCADE,
    view       TEXT    NOT NULL CHECK (view IN ('grid', 'list')),
    sort       TEXT    NOT NULL CHECK (sort IN ('default', 'added', 'title', 'author')),
    reversed   INTEGER NOT NULL CHECK (reversed IN (0, 1)),
    updated_at TEXT    NOT NULL
) STRICT;
```

- [ ] **Step 4: Write the store code**

Create `internal/apps/books/prefs.go`:

```go
package books

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
)

// View is how the home page lays its books out (UI redesign spec
// "Preferences", docs/superpowers/specs/2026-10-12-on-books-ui-redesign-design.md).
type View string

// The two views.
const (
	ViewGrid View = "grid"
	ViewList View = "list"
)

// Sort is the home page's order (spec "Sorting"). SortDefault is each
// shelf's own order, the one the shelves had before the redesign.
type Sort string

// The sorts.
const (
	SortDefault Sort = "default"
	SortAdded   Sort = "added"
	SortTitle   Sort = "title"
	SortAuthor  Sort = "author"
)

// Sorts is every sort, in the sort menu's order.
var Sorts = []Sort{SortDefault, SortAdded, SortTitle, SortAuthor}

var sortLabels = map[Sort]string{SortDefault: "Default", SortAdded: "Date added", SortTitle: "Title", SortAuthor: "Author"}

// Label is how a sort is named in the menu.
func (s Sort) Label() string { return sortLabels[s] }

// Prefs are one reader's home-page settings.
type Prefs struct {
	View     View
	Sort     Sort
	Reversed bool // the sort's natural direction turned round
}

// DefaultPrefs apply until the reader changes something.
var DefaultPrefs = Prefs{View: ViewGrid, Sort: SortDefault}

// Valid reports whether p names a view and a sort the home page has.
func (p Prefs) Valid() bool {
	return (p.View == ViewGrid || p.View == ViewList) && slices.Contains(Sorts, p.Sort)
}

// Prefs returns userID's home-page settings, or DefaultPrefs if they never
// changed any. It mirrors ON Later's Store.Prefs; apps never import each
// other, so this is an independent implementation.
func (st *Store) Prefs(ctx context.Context, userID int64) (Prefs, error) {
	var view, sort string
	var reversed int
	err := st.db.QueryRowContext(ctx, `SELECT view, sort, reversed FROM books_prefs WHERE user_id = ?`, userID).
		Scan(&view, &sort, &reversed)
	if errors.Is(err, sql.ErrNoRows) {
		return DefaultPrefs, nil
	}
	if err != nil {
		return Prefs{}, fmt.Errorf("books: load prefs: %w", err)
	}
	return Prefs{View: View(view), Sort: Sort(sort), Reversed: reversed == 1}, nil
}

// SetPrefs saves userID's home-page settings, replacing any earlier ones.
// A view or sort the home page has no control for is ErrInvalid.
func (st *Store) SetPrefs(ctx context.Context, userID int64, p Prefs) error {
	if !p.Valid() {
		return ErrInvalid
	}
	reversed := 0
	if p.Reversed {
		reversed = 1
	}
	_, err := st.db.ExecContext(ctx, `
		INSERT INTO books_prefs (user_id, view, sort, reversed, updated_at) VALUES (?, ?, ?, ?, ?)
		ON CONFLICT (user_id) DO UPDATE SET view = excluded.view, sort = excluded.sort,
			reversed = excluded.reversed, updated_at = excluded.updated_at`,
		userID, string(p.View), string(p.Sort), reversed, formatTime(st.now()))
	if err != nil {
		return fmt.Errorf("books: save prefs: %w", err)
	}
	return nil
}
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go test ./internal/apps/books/ -run 'Prefs' -count=1 -v`
Expected: all four PASS.

- [ ] **Step 6: Run the package and arch tests, then commit**

Run: `go test ./internal/apps/books/ ./internal/arch/ -count=1`
Expected: ok.

```bash
git add internal/apps/books/migrations/0008_prefs.sql internal/apps/books/prefs.go internal/apps/books/prefs_test.go
git commit -m "feat(books): store each user's home view and sort (books_prefs)"
```

---

### Task 2: The four sorts and tags for the tiles

**Files:**
- Create: `internal/apps/books/sort.go`
- Modify: `internal/apps/books/library.go`: `ListQuery` (around line 338), `listOrder` (around line 350) and `List` (around line 375)
- Modify: `internal/apps/books/tag.go`: add `TagsByBook` after `TagNames`
- Modify: `internal/apps/books/handlers.go:102`. `ListQuery(c)` no longer converts, because the field sets differ.
- Test: `internal/apps/books/sort_test.go`

**Interfaces:**
- Consumes: `Sort` and its constants (Task 1).
- Produces:
  - `ListQuery{Shelf, Tag, Q, Series string-ish; Sort Sort; Reversed bool}`. A zero `Sort` means Default.
  - `func (*Store) TagsByBook(ctx, userID int64) (map[int64][]string, error)`

- [ ] **Step 1: Write the failing tests**

Create `internal/apps/books/sort_test.go`:

```go
package books_test

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/iliafrenkel/on-suite/internal/apps/books"
)

// addedInTurn adds Alice's book, then moves the clock on a minute, so
// every book's added_at differs.
func addedInTurn(t *testing.T, f *fixture, nb books.NewBook) int64 {
	t.Helper()
	id := addBook(t, f, f.alice.ID, nb)
	f.now = f.now.Add(time.Minute)
	return id
}

func TestSortByDateAdded(t *testing.T) {
	f := newFixture(t)
	first := addedInTurn(t, f, onShelf("First", books.ShelfWant))
	addedInTurn(t, f, onShelf("Second", books.ShelfWant))
	addedInTurn(t, f, onShelf("Third", books.ShelfWant))
	// Tagging First bumps its updated_at, so the Default order of All
	// would put it on top. Date added must not.
	if err := f.store.SetTags(context.Background(), f.alice.ID, first, []string{"x"}); err != nil {
		t.Fatal(err)
	}
	if got := list(t, f, books.ListQuery{Shelf: books.ShelfAll}); got[0] != "First" {
		t.Fatalf("Default All = %v, want First on top (latest change)", got)
	}
	q := books.ListQuery{Shelf: books.ShelfAll, Sort: books.SortAdded}
	if got, want := list(t, f, q), []string{"Third", "Second", "First"}; !slices.Equal(got, want) {
		t.Errorf("Date added = %v, want %v (newest first)", got, want)
	}
	q.Reversed = true
	if got, want := list(t, f, q), []string{"First", "Second", "Third"}; !slices.Equal(got, want) {
		t.Errorf("Date added, reversed = %v, want %v", got, want)
	}
}

func TestSortByTitleSkipsALeadingArticle(t *testing.T) {
	f := newFixture(t)
	for _, title := range []string{"A Wizard of Earthsea", "Theodore Boone", "The Left Hand of Darkness",
		"An Instance of the Fingerpost", "Anathem"} {
		addedInTurn(t, f, onShelf(title, books.ShelfWant))
	}
	want := []string{"Anathem", "An Instance of the Fingerpost", "The Left Hand of Darkness", "Theodore Boone", "A Wizard of Earthsea"}
	q := books.ListQuery{Shelf: books.ShelfAll, Sort: books.SortTitle}
	if got := list(t, f, q); !slices.Equal(got, want) {
		t.Errorf("Title = %v, want %v (\"The\", \"A\", \"An\" skipped; \"Theodore\" kept)", got, want)
	}
	q.Reversed = true
	slices.Reverse(want)
	if got := list(t, f, q); !slices.Equal(got, want) {
		t.Errorf("Title, reversed = %v, want %v", got, want)
	}
}

func TestSortByAuthorSurname(t *testing.T) {
	f := newFixture(t)
	for _, b := range []struct{ title, authors string }{
		{"Dune", "Frank Herbert"},
		{"Good Omens", "Terry Pratchett, Neil Gaiman"},
		{"Anonymous diary", ""},
		{"The Dispossessed", "Ursula K. Le Guin"},
		{"Children of Dune", "Frank Herbert"},
	} {
		nb := onShelf(b.title, books.ShelfWant)
		nb.Authors = b.authors
		addedInTurn(t, f, nb)
	}
	q := books.ListQuery{Shelf: books.ShelfAll, Sort: books.SortAuthor}
	// guin < herbert < pratchett (the first author's last word); a tie goes
	// by title; no author is last.
	want := []string{"The Dispossessed", "Children of Dune", "Dune", "Good Omens", "Anonymous diary"}
	if got := list(t, f, q); !slices.Equal(got, want) {
		t.Errorf("Author = %v, want %v", got, want)
	}
	q.Reversed = true
	want = []string{"Good Omens", "Dune", "Children of Dune", "The Dispossessed", "Anonymous diary"}
	if got := list(t, f, q); !slices.Equal(got, want) {
		t.Errorf("Author, reversed = %v, want %v (still no author last)", got, want)
	}
}

func TestDefaultSortReversedKeepsUndatedLast(t *testing.T) {
	f := newFixture(t)
	undated := addBook(t, f, f.alice.ID, onShelf("Undated", books.ShelfRead))
	early := onShelf("Early", books.ShelfRead)
	early.FinishedOn = "2026-01-05"
	addBook(t, f, f.alice.ID, early)
	late := onShelf("Late", books.ShelfRead)
	late.FinishedOn = "2026-03-05"
	addBook(t, f, f.alice.ID, late)
	if _, err := f.db.Exec(`UPDATE books_readings SET finished_on = NULL WHERE book_id = ?`, undated); err != nil {
		t.Fatal(err)
	}
	q := books.ListQuery{Shelf: books.ShelfRead}
	if got, want := list(t, f, q), []string{"Late", "Early", "Undated"}; !slices.Equal(got, want) {
		t.Errorf("Read, Default = %v, want %v", got, want)
	}
	q.Reversed = true
	if got, want := list(t, f, q), []string{"Early", "Late", "Undated"}; !slices.Equal(got, want) {
		t.Errorf("Read, Default reversed = %v, want %v (undated stays last)", got, want)
	}
}

func TestASeriesIgnoresTheSort(t *testing.T) {
	f := newFixture(t)
	addBook(t, f, f.alice.ID, inSeries("Caliban's War", "The Expanse", "2"))
	addBook(t, f, f.alice.ID, inSeries("Leviathan Wakes", "The Expanse", "1"))
	for _, s := range books.Sorts {
		for _, reversed := range []bool{false, true} {
			got := list(t, f, books.ListQuery{Shelf: books.ShelfAll, Series: "The Expanse", Sort: s, Reversed: reversed})
			if want := []string{"Leviathan Wakes", "Caliban's War"}; !slices.Equal(got, want) {
				t.Errorf("series by %s (reversed %v) = %v, want reading order %v", s, reversed, got, want)
			}
		}
	}
}

func TestTagsByBook(t *testing.T) {
	f := newFixture(t)
	tagged := onShelf("Dune", books.ShelfWant)
	tagged.Tags = []string{"sf", "classics"}
	dune := addBook(t, f, f.alice.ID, tagged)
	plain := addBook(t, f, f.alice.ID, onShelf("Emma", books.ShelfWant))
	bobs := onShelf("Bob's", books.ShelfWant)
	bobs.Tags = []string{"secret"}
	bob := addBook(t, f, f.bob.ID, bobs)

	got, err := f.store.TagsByBook(context.Background(), f.alice.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(got[dune], []string{"classics", "sf"}) {
		t.Errorf("Dune's tags = %v, want [classics sf]", got[dune])
	}
	if _, ok := got[plain]; ok {
		t.Error("a book with no tags has an entry")
	}
	if _, ok := got[bob]; ok {
		t.Error("Bob's book's tags came back for Alice")
	}
}
```

`inSeries` is the existing helper in `series_test.go`.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/apps/books/ -run 'Sort|TagsByBook|ASeries' -count=1`
Expected: a build failure, `unknown field Sort in struct literal of type books.ListQuery`.

- [ ] **Step 3: Write the sort keys**

Create `internal/apps/books/sort.go`:

```go
package books

import (
	"cmp"
	"slices"
	"strings"
)

// titleKey is a title as the Title sort reads it: lower case, without a
// leading English article, so "The Left Hand of Darkness" files under L
// (UI redesign spec "Sorting"). A title that is only an article keeps it.
func titleKey(title string) string {
	t := strings.ToLower(strings.TrimSpace(title))
	for _, article := range []string{"the ", "a ", "an "} {
		if rest, ok := strings.CutPrefix(t, article); ok && strings.TrimSpace(rest) != "" {
			return strings.TrimSpace(rest)
		}
	}
	return t
}

// authorKey is the Author sort's key: the last word of the first author
// (authors are free text, "Terry Pratchett, Neil Gaiman"), lower case; ""
// for a book with no author. "Ursula K. Le Guin" files under G: a
// "sort as" field would fix that, and the spec chose not to have one.
func authorKey(authors string) string {
	first, _, _ := strings.Cut(authors, ",")
	words := strings.Fields(first)
	if len(words) == 0 {
		return ""
	}
	return strings.ToLower(words[len(words)-1])
}

// sortItems puts items in Title or Author order (by), A–Z or, reversed,
// Z–A. Under Author a book with no author is last either way, and a tie
// goes by title. Equal titles keep the order they were added in.
func sortItems(items []ListItem, by Sort, reversed bool) {
	dir := 1
	if reversed {
		dir = -1
	}
	slices.SortStableFunc(items, func(a, b ListItem) int {
		if by == SortAuthor {
			ka, kb := authorKey(a.Authors), authorKey(b.Authors)
			switch {
			case ka == "" && kb != "":
				return 1
			case ka != "" && kb == "":
				return -1
			}
			if c := strings.Compare(ka, kb); c != 0 {
				return dir * c
			}
		}
		if c := strings.Compare(titleKey(a.Title), titleKey(b.Title)); c != 0 {
			return dir * c
		}
		return dir * cmp.Compare(a.ID, b.ID)
	})
}
```

- [ ] **Step 4: Teach `ListQuery`, `listOrder` and `List` the sorts**

In `internal/apps/books/library.go`, replace the `ListQuery` doc comment and struct with:

```go
// ListQuery picks the books a list shows and their order. Shelf "" or
// ShelfAll is every shelf; Tag is one tag name; Q is full-text search over
// books_search — title, subtitle, authors, series, review, notes, quotes
// and comments — every word having to match, each as a prefix; Series is
// one series' name, matched whole and ignoring case. Sort "" is
// SortDefault; Reversed turns the sort's natural direction round (UI
// redesign spec "Sorting"). A series is always in reading order.
type ListQuery struct {
	Shelf    Shelf
	Tag      string
	Q        string
	Series   string
	Sort     Sort
	Reversed bool
}
```

Before replacing anything, check that the field list above matches the current struct (`Shelf`, `Tag`, `Q`, `Series`) and keep any field this plan doesn't mention.

Replace `listOrder` (doc comment included) with:

```go
// listOrder is a list's SQL order (UI redesign spec "Sorting"). A series
// is in reading order (by number, as a number where it is one: "2.5" sits
// between 2 and 3), whatever the sort. Default is each shelf's own order:
// Reading by the latest progress (a reading with none yet by when it
// started), Read by finish, Want to read by date added, DNF and All by the
// latest change. Date added is added_at. All of these are newest first,
// oldest first when reversed, and an undated finish is last either way.
// Title and Author are put in order in Go (sortItems), so here they are
// only a stable base.
func listOrder(q ListQuery) string {
	if q.Series != "" {
		// Unnumbered books sort after numbered ones; a non-numeric number
		// casts to 0 and so sorts first among the numbered.
		return `b.series_number = '', CAST(b.series_number AS REAL), b.series_number, b.title, b.id`
	}
	dir := "DESC"
	if q.Reversed {
		dir = "ASC"
	}
	switch q.Sort {
	case SortAdded:
		return `b.added_at ` + dir + `, b.id ` + dir
	case SortTitle, SortAuthor:
		return `b.id`
	}
	switch q.Shelf {
	case ShelfReading:
		return `COALESCE(p.recorded_at, r.created_at) ` + dir + `, r.id ` + dir
	case ShelfRead:
		return `r.finished_on IS NULL, r.finished_on ` + dir + `, r.id ` + dir
	case ShelfWant:
		return `b.added_at ` + dir + `, b.id ` + dir
	default:
		return `b.updated_at ` + dir + `, b.id ` + dir
	}
}
```

In `List`, just before the final `return out, nil`, add:

```go
	if q.Series == "" && (q.Sort == SortTitle || q.Sort == SortAuthor) {
		sortItems(out, q.Sort, q.Reversed)
	}
```

Update `List`'s doc comment, which begins "List returns userID's books matching q, in the shelf's order", to read: `// List returns userID's books matching q, in q's order (listOrder, sortItems) — a search keeps it too (decided 2026-10-09), so filtering never reshuffles a list.`

- [ ] **Step 5: Add `TagsByBook`**

In `internal/apps/books/tag.go`, after `TagNames`, add:

```go
// TagsByBook returns the names of the tags on each of userID's books,
// alphabetically, keyed by book id; a book with no tags has no entry. The
// home page's tiles show them (UI redesign spec "Book tile"). One query
// for the whole library, which is a personal one: a few hundred books.
func (st *Store) TagsByBook(ctx context.Context, userID int64) (map[int64][]string, error) {
	rows, err := st.db.QueryContext(ctx, `
		SELECT x.book_id, t.name FROM books_book_tags x
		  JOIN books_tags t ON t.id = x.tag_id
		 WHERE t.user_id = ?
		 ORDER BY x.book_id, t.name`, userID)
	if err != nil {
		return nil, fmt.Errorf("books: tags by book: %w", err)
	}
	defer func() { _ = rows.Close() }()
	out := map[int64][]string{}
	for rows.Next() {
		var id int64
		var name string
		if err := rows.Scan(&id, &name); err != nil {
			return nil, fmt.Errorf("books: scan tags by book: %w", err)
		}
		out[id] = append(out[id], name)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("books: tags by book: %w", err)
	}
	return out, nil
}
```

- [ ] **Step 6: Fix the conversion in `handlers.go`**

`ListQuery(c)` no longer compiles. In `internal/apps/books/handlers.go`, replace

```go
	items, err := a.store.List(ctx, userID, ListQuery(c)) // same fields, by design
```

with

```go
	items, err := a.store.List(ctx, userID, ListQuery{Shelf: c.Shelf, Tag: c.Tag, Q: c.Q, Series: c.Series})
```

Also in `view.go`, change the `listCtx` doc comment's last sentence "Its fields are ListQuery's, in the same order." to "ListQuery has these fields too, and the order." Leave the rest of the comment alone.

- [ ] **Step 7: Run the tests to verify they pass**

Run: `go test ./internal/apps/books/ -count=1`
Expected: ok. The new tests pass, and the existing shelf, series and filter tests still pass because a zero `Sort` is Default.

- [ ] **Step 8: Run vet and staticcheck, then commit**

Run: `go vet ./internal/apps/books/ && go run honnef.co/go/tools/cmd/staticcheck@v0.8.0 ./internal/apps/books/`
Expected: no output.

```bash
git add internal/apps/books/sort.go internal/apps/books/sort_test.go internal/apps/books/library.go internal/apps/books/tag.go internal/apps/books/handlers.go internal/apps/books/view.go
git commit -m "feat(books): sort lists by date added, title or author, either way"
```

---

### Task 3: The book gets a page of its own

The book pane leaves the panes and becomes `/books/b/{id}`: a full-width page whose page bar holds a back link, Edit and Delete. Its sections stay exactly as they are today, minus the ⋯ menu (UI2 reorganises them). The home is left as two panes, shelves and list, until Task 4 replaces it.

**Files:**
- Create: `internal/apps/books/templates/book.html`
- Create: `internal/apps/books/templates/book.partial.html`
- Rewrite: `internal/apps/books/templates/panes.partial.html`
- Modify: `internal/apps/books/handlers.go`, `internal/apps/books/actions.go`, `internal/apps/books/view.go`
- Test: `handlers_test.go`, `actions_test.go`, `progress_view_test.go`, `notes_view_test.go`, `quotes_view_test.go`, `rating_view_test.go`, `review_gaps_b1a_test.go`, `review_gaps_b1b_test.go`, `covers_test.go`, `series_test.go`, `findcover_test.go` (all under `internal/apps/books/`)

**Interfaces:**
- Produces:
  - `type bookOpts struct{ Banner, ProgressError, ProgressInput string; Draft entryDraft }`
  - `func (a *App) renderBook(w, r, userID, id int64, c listCtx, opts bookOpts)`
  - `type bookPageView struct{ Title string; Shell render.Shell; Error string; Book bookView }`
  - `bookView.BackLabel string`, which replaces `bookView.Selected`
  - `func (a *App) renderPanes(w, r, userID int64, c listCtx)`, list only. Task 4 deletes it.
- Template blocks:
  - `book-page`, `book-body` (`#books-book`), `page-bar`
  - the swaps `book-swap`, `progress-swap`, `notes-swap` and `quotes-swap`, which all take a `bookPageView`

- [ ] **Step 1: Rewrite the tests for the book page (failing)**

In `internal/apps/books/handlers_test.go`:

1. In `TestIndexOpensOnTheReadingShelf`, replace

   ```go
   	doc.MustHave(".books-book .empty")
   	if !isChecked(t, doc, "books-list-open") || isChecked(t, doc, "books-book-open") {
   		t.Error("phone drill-down: want the list showing and no book open")
   	}
   ```

   with

   ```go
   	doc.MustNotHave("#books-book") // a book has a page of its own now
   	if !isChecked(t, doc, "books-list-open") {
   		t.Error("phone drill-down: want the list showing")
   	}
   ```

2. Replace the whole of `TestBookPaneShowsTheBook` with:

   ```go
   func TestBookPageShowsTheBook(t *testing.T) {
   	s := newServer(t)
   	s.Clock.Set(time.Date(2026, 10, 9, 1, 0, 0, 0, time.UTC))
   	nb := titled("Leviathan Wakes", "James S. A. Corey", books.ShelfReading)
   	nb.SeriesName, nb.SeriesNumber, nb.Year, nb.Pages, nb.ISBN = "The Expanse", "1", 2011, 592, "0306406152"
   	nb.Tags = []string{"sf"}
   	id := add(t, s, s.Alice.User.ID, nb)

   	doc := s.Get(t, s.Alice, fmt.Sprintf("/books/b/%d?shelf=reading", id))
   	book := doc.MustHave("#books-book")
   	if got := htmlassert.Text(doc.MustHave(".books-book h1")); got != "Leviathan Wakes" {
   		t.Errorf("h1 = %q", got)
   	}
   	if got := htmlassert.Text(doc.MustHave(".books-series")); got != "The Expanse #1" {
   		t.Errorf("series = %q", got)
   	}
   	if got := htmlassert.Text(doc.MustHave(".books-facts")); got != "2011 · 592 pages · ISBN 9780306406157" {
   		t.Errorf("facts = %q", got)
   	}
   	if v, _ := htmlassert.Attr(book, "data-book-id"); v != fmt.Sprint(id) {
   		t.Errorf("data-book-id = %q, want %d", v, id)
   	}
   	doc.MustNotHave("#books-list")  // full width: no list beside it (spec "Book details page")
   	doc.MustNotHave(".books-menu")  // Edit and Delete are in the page bar now
   	doc.MustHave(".books-page-bar button[hx-confirm]")
   	doc.MustHave("#books-confirm-dialog")
   	if title := htmlassert.Text(doc.MustHave("title")); !strings.HasPrefix(title, "Leviathan Wakes") {
   		t.Errorf("<title> = %q, want the book's title first", title)
   	}
   }
   ```

3. In `TestHTMXListNavigationSwapsTheListOnly`, change the id list to `[]string{"books-side", "books-filter-ctx", "books-list-open", "shell-crumb-tail"}`.

4. Replace `TestListCarriesItsContextForTheBookPane` with:

   ```go
   func TestRowsOpenTheBookWithTheirList(t *testing.T) {
   	s := newServer(t)
   	id := add(t, s, s.Alice.User.ID, titled("Emma", "", books.ShelfWant))
   	doc := htmlassert.Parse(t, hx(t, s, "/books/?shelf=all&q=em", "books-list"))
   	link := doc.MustHave(fmt.Sprintf(`a[href="/books/b/%d?q=em&shelf=all"]`, id))
   	if _, ok := htmlassert.Attr(link, "hx-get"); ok {
   		t.Error("a book opens with a plain link: it has a page of its own")
   	}
   }
   ```

5. Replace `TestHTMXBookOpenSwapsTheBookOnly` with:

   ```go
   func TestBookPageGoesBackToItsList(t *testing.T) {
   	s := newServer(t)
   	id := add(t, s, s.Alice.User.ID, titled("Emma", "", books.ShelfWant))
   	doc := s.Get(t, s.Alice, fmt.Sprintf("/books/b/%d?shelf=all&tag=sf&q=le+guin", id))
   	back := doc.MustHave(".books-back")
   	if href, _ := htmlassert.Attr(back, "href"); href != "/books/?q=le+guin&shelf=all&tag=sf" {
   		t.Errorf("back link = %q, want the tagged, searched list it came from", href)
   	}
   	if got := htmlassert.Text(back); got != "Tagged “sf”" {
   		t.Errorf("back link text = %q, want the list's name", got)
   	}
   	if href := attr(t, doc, ".books-edit-link", "href"); href != fmt.Sprintf("/books/edit/%d?q=le+guin&shelf=all&tag=sf", id) {
   		t.Errorf("edit link = %q, want the same list context", href)
   	}
   }
   ```

In `internal/apps/books/actions_test.go`:

6. Replace `TestActionsOverHTMXReturnThePanes` with:

   ```go
   func TestActionsOverHTMXReturnTheBook(t *testing.T) {
   	s := newServer(t)
   	id := add(t, s, s.Alice.User.ID, titled("Emma", "", books.ShelfWant))
   	rec := s.PostHX(t, s.Alice, fmt.Sprintf("/books/start/%d", id), url.Values{"shelf": {"want"}})
   	if rec.Code != http.StatusOK {
   		t.Fatalf("htmx start = %d, want 200", rec.Code)
   	}
   	doc := htmlassert.Parse(t, rec.Body.String())
   	doc.MustHave("#books-book")
   	doc.MustNotHave("#books-list")
   	if got := htmlassert.Text(doc.MustHave(".books-book h1")); got != "Emma" {
   		t.Errorf("book = %q, want Emma", got)
   	}
   	if got := htmlassert.Text(doc.MustHave(".books-pill-shelf")); got != "Reading" {
   		t.Errorf("shelf pill = %q, want Reading after starting it", got)
   	}
   }
   ```

7. Replace `TestDeleteOverHTMXFixesTheAddressBar` with:

   ```go
   func TestDeleteOverHTMXGoesBackToTheList(t *testing.T) {
   	s := newServer(t)
   	id := add(t, s, s.Alice.User.ID, titled("Dune", "", books.ShelfWant))
   	rec := s.PostHX(t, s.Alice, fmt.Sprintf("/books/delete/%d", id), url.Values{"shelf": {"all"}, "tag": {"sf"}, "q": {"le guin"}})
   	if rec.Code != http.StatusOK {
   		t.Fatalf("htmx delete = %d, want 200", rec.Code)
   	}
   	if got := rec.Header().Get("HX-Redirect"); got != "/books/?q=le+guin&shelf=all&tag=sf" {
   		t.Errorf("HX-Redirect = %q, want the list the book was opened from", got)
   	}
   	if rec.Body.Len() != 0 {
   		t.Errorf("body = %q, want none: the page goes away", rec.Body.String())
   	}
   }
   ```

8. Delete `TestActionOverHTMXKeepsTheAddressOnTheBook`. The address is already the book's, so there's nothing left to fix.

In `internal/apps/books/progress_view_test.go`:

9. Rename `TestProgressOverHTMXSwapsTheBoxAndTheList` to `TestProgressOverHTMXSwapsTheBox`. Replace its `doc.MustNotHave("#books-panes")` with `doc.MustNotHave("#books-book")`. Then delete everything from `list := doc.MustHave("#books-list")` through `doc.MustHave("#books-list .is-active")` and put this in its place:

   ```go
   	doc.MustNotHave("#books-list") // no list beside the book any more
   ```

In `internal/apps/books/notes_view_test.go` (`TestAddingANote`) and `internal/apps/books/quotes_view_test.go` (`TestAddingAQuote`):

10. Replace

    ```go
    	if v, _ := htmlassert.Attr(doc.MustHave("#books-list"), "hx-swap-oob"); v != "true" {
    		t.Errorf("list hx-swap-oob = %q, want the list out of band", v)
    	}
    	doc.MustNotHave("#books-panes")
    ```

    with

    ```go
    	doc.MustNotHave("#books-list")
    	doc.MustNotHave("#books-book")
    ```

11. In `internal/apps/books/rating_view_test.go`, change `doc.MustHave("#books-panes")` to `doc.MustHave("#books-book")`.

12. In `internal/apps/books/review_gaps_b1a_test.go` (`TestSetTagsOverHTMX`):
    - Delete the `HX-Replace-Url` check (the `if got, want := rec.Header().Get("HX-Replace-Url"), …` block).
    - Change `doc.MustHave("#books-panes")` to `doc.MustHave("#books-book")`.
    - Replace the sidebar block (`sidebar := strings.Join(texts(doc, ".books-side a"), "|")` and the `if` after it) with:

      ```go
      	if got := strings.Join(texts(doc, ".books-pills .books-pill"), "|"); got != "Want to read|sf|space" {
      		t.Errorf("pills = %q, want the shelf, then the new tags", got)
      	}
      ```

13. In `internal/apps/books/review_gaps_b1b_test.go` (`TestTheHTMXPanesShowTheCoverOnlyToItsOwner`), replace

    ```go
    	for _, target := range []string{"books-book", "books-panes"} {
    		doc := htmlassert.Parse(t, hx(t, s, path, target))
    		if got := attr(t, doc, "img.books-cover", "src"); got != want {
    			t.Errorf("target %s: cover src = %q, want %q", target, got, want)
    		}
    	}
    ```

    with

    ```go
    	for name, doc := range map[string]*htmlassert.Doc{
    		"page": s.Get(t, s.Alice, path),
    		"htmx": htmlassert.Parse(t, hx(t, s, path, "books-book")),
    	} {
    		if got := attr(t, doc, "img.books-cover", "src"); got != want {
    			t.Errorf("%s: cover src = %q, want %q", name, got, want)
    		}
    	}
    ```

14. In `internal/apps/books/covers_test.go` (`TestCoversShowInTheListAndTheBookPane`), the mini covers now come from the list page. After `want := …` add `list := s.Get(t, s.Alice, "/books/?shelf=want")`. Then change `doc.QueryAll("img.books-mini-cover")` to `list.QueryAll("img.books-mini-cover")`, and `doc.QueryAll(".books-mini-spine")` to `list.QueryAll(".books-mini-spine")`. Keep `doc` (the book page) for the big cover.

15. In `internal/apps/books/series_test.go` (`TestTheSeriesLinkFiltersTheList`), replace

    ```go
    	if v, _ := htmlassert.Attr(link, "hx-target"); v != "#books-list" {
    		t.Errorf("series link hx-target = %q, want the list", v)
    	}
    ```

    with

    ```go
    	if _, ok := htmlassert.Attr(link, "hx-get"); ok {
    		t.Error("the series link is a plain link: the book page has no list to swap")
    	}
    ```

16. In `internal/apps/books/findcover_test.go`, delete `TestFindCoverIsInTheMenuOfABookWithout`. The menu is gone, and Task 6 tests Find cover on the Edit page.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/apps/books/ -count=1 2>&1 | tail -30`
Expected: FAIL in `TestBookPageShowsTheBook` (`.books-page-bar` missing, `#books-list` present), `TestBookPageGoesBackToItsList`, `TestDeleteOverHTMXGoesBackToTheList` (no `HX-Redirect`), and the swap tests that now forbid `#books-list`.

- [ ] **Step 3: Create the book page template**

Create `internal/apps/books/templates/book.html`:

```html
{{define "head"}}<script src="/books/books.js" defer></script>{{end}}

{{define "content"}}{{template "book-page" .Data}}{{end}}
```

- [ ] **Step 4: Create `book.partial.html` from the book blocks**

First write the new head of the file. Create `internal/apps/books/templates/book.partial.html` with exactly this content, ending with one blank line:

```html
{{/* ON Books' book page: a full-width page of its own with a page bar back
     to the list it was opened from (UI redesign spec "Book details page",
     docs/superpowers/specs/2026-10-12-on-books-ui-redesign-design.md). The
     page blocks take a bookPageView; the sections below them take its Book
     (a bookView), as their comments say. */}}

{{/* book-swap answers a change that can move more than one section —
     start, finish, did not finish, the format, the rating, the review, the
     tags, a reading edited or deleted — and a refusal's banner: the page
     body (target #books-book). */}}
{{define "book-swap"}}{{template "book-body" .}}{{end}}

{{/* progress-swap answers a progress update (target #books-progress). */}}
{{define "progress-swap"}}{{template "progress" .Book}}{{end}}

{{/* notes-swap and quotes-swap answer a note or a quote being added,
     edited or deleted (targets #books-notes, #books-quotes). */}}
{{define "notes-swap"}}{{template "notes" .Book}}{{end}}
{{define "quotes-swap"}}{{template "quotes" .Book}}{{end}}

{{define "book-page"}}
<div class="books-page" id="books-page">
	{{template "book-body" .}}
	{{template "confirm"}}
</div>
{{end}}

{{define "book-body"}}
<article class="books-book" id="books-book" data-book-id="{{.Book.ID}}" aria-label="Book">
	{{template "page-bar" .Book}}
	{{with .Error}}<p class="notice notice-error books-banner" role="alert">{{.}}</p>{{end}}
	{{with .Book}}
	<header class="books-book-head">
		{{if .Cover}}<img class="books-cover" src="{{.Cover}}" alt="Cover of {{.Title}}" width="96" height="144">{{else}}<div class="books-spine swatch-c-{{.Spine}}" aria-hidden="true"><span>{{.Title}}</span></div>{{end}}
		<div class="books-book-titles">
			<h1>{{.Title}}</h1>
			{{with .Subtitle}}<p class="books-subtitle">{{.}}</p>{{end}}
			{{with .Authors}}<p class="books-authors">{{.}}</p>{{end}}
			{{with .Series}}<p class="books-series"><a href="{{$.Book.SeriesURL}}">{{.}}</a></p>{{end}}
			{{with .Facts}}<p class="books-facts">{{range $i, $f := .}}{{if $i}} · {{end}}{{$f}}{{end}}</p>{{end}}
			<p class="books-pills"><span class="books-pill books-pill-shelf">{{.ShelfLabel}}</span>{{range .Tags}} <span class="books-pill">{{.}}</span>{{end}}</p>
		</div>
	</header>
	{{template "reading-box" .}}
	{{template "rating" .}}
	{{template "review" .}}
	{{template "description" .}}
	{{template "tags-form" .}}
	{{template "notes" .}}
	{{template "quotes" .}}
	{{template "history" .}}
	{{end}}
</article>
{{end}}

{{/* page-bar is the way back to the list the book was opened from, and the
     book's Edit and Delete as small icon buttons (spec "Page bar"). Delete
     goes through the confirm dialog (books.js), then to the list
     (HX-Redirect); without JavaScript it simply submits. Takes a bookView. */}}
{{define "page-bar"}}
<nav class="books-page-bar" aria-label="Book actions">
	<a class="books-back" href="{{.Ctx.ListURL}}">{{ticon "arrow-left"}}{{.BackLabel}}</a>
	<span class="books-page-actions">
		<a class="toolbar-btn books-icon-btn books-edit-link" href="{{.Ctx.EditURL .ID}}" aria-label="Edit details" title="Edit details">{{ticon "edit"}}</a>
		<form method="post" action="/books/delete/{{.ID}}">
			{{template "post-ctx" .}}
			<button type="submit" class="toolbar-btn books-icon-btn books-delete" aria-label="Delete book" title="Delete book"
			        hx-post="/books/delete/{{.ID}}" hx-target="#books-book" hx-swap="outerHTML"
			        hx-confirm="Delete “{{.Title}}” for good? Its readings, notes and quotes go too.">{{ticon "trash"}}</button>
		</form>
	</span>
</nav>
{{end}}

```

Then append the old file's book blocks to it. Everything from the `text-field` comment to the end of `panes.partial.html` is a book block. Drop the ⋯ menu, and point every form that swapped the whole panes at the book body. Run from the repository root:

```bash
python3 -I - <<'EOF'
import pathlib
d = pathlib.Path("internal/apps/books/templates")
src = (d / "panes.partial.html").read_text()
tail = src[src.index("{{/* text-field is one labelled"):]
# The ⋯ menu goes: Edit and Delete are in the page bar now.
m0 = tail.index("{{/* book-menu is the book pane")
m1 = tail.index("{{end}}\n", tail.index("</details>", m0)) + len("{{end}}\n")
tail = tail[:m0] + tail[m1:].lstrip("\n")
tail = tail.replace('hx-target="#books-panes"', 'hx-target="#books-book"')
with (d / "book.partial.html").open("a") as f:
    f.write(tail)
EOF
grep -c 'books-panes' internal/apps/books/templates/book.partial.html
grep -c 'define "book-menu"' internal/apps/books/templates/book.partial.html
```

Expected: both counts are `0`. Open the file and check that the `post-ctx`, `reading-box`, `format-menu`, `progress`, `close-reading`, `rating`, `review`, `description`, `tags-form`, `notes`, `note-form`, `quotes`, `quote-form`, `history` and `confirm` defines are all there. Change the first line of each doc comment that says "the book pane" to say "the book page" (wording only).

- [ ] **Step 5: Trim `panes.partial.html` to shelves and list**

Replace the whole of `internal/apps/books/templates/panes.partial.html` with the following. It is temporary: Task 4 deletes it.

```html
{{/* ON Books' shelves and list, as two panes now that a book has a page of
     its own (UI redesign, UI1 Task 3). Task 4 replaces these with the home
     page. Every block here takes a panesView (or the part named in its
     comment). */}}

{{/* The <title> and crumb ride along with every fragment, so the page
     chrome follows the list (#205). */}}
{{define "shell-oob"}}<title>{{if .Title}}{{.Title}} · {{end}}ON Suite</title><span id="shell-crumb-tail" hx-swap-oob="true">{{template "shell-crumb-tail" (dict "Title" .Title "Shell" .Shell)}}</span>{{end}}

{{/* list-swap answers a shelf, tag or filter change (target #books-list):
     the list, with the sidebar, the filter's hidden fields and the phone
     drill-down box out of band. */}}
{{define "list-swap"}}{{template "shell-oob" .}}{{template "list" .List}}{{template "sidebar" (dict "Sidebar" .Sidebar "OOB" true)}}{{template "filter-ctx" (dict "Ctx" .Ctx "OOB" true)}}{{template "pane-toggle" (dict "ID" "books-list-open" "Label" "Shelves" "Checked" true "OOB" true)}}{{end}}

{{define "panes"}}
<div class="books-panes" id="books-panes">
	<div class="books-panes-row" id="books-panes-row">
		{{template "pane-toggle" (dict "ID" "books-list-open" "Label" "Shelves" "Checked" true "OOB" false)}}
		{{template "sidebar" (dict "Sidebar" .Sidebar "OOB" false)}}
		<div class="pane-gutter" data-gutter-for="side" role="separator"
		     aria-orientation="vertical" aria-label="Resize the shelves" tabindex="0"></div>
		<section class="books-listpane" aria-label="Books">
			{{template "back" (dict "For" "books-list-open" "Label" "Shelves")}}
			{{/* The filter lives outside #books-list so a list swap never
			     replaces the box being typed in (PATTERNS.md, SyncSearch). */}}
			<form class="books-filter" method="get" action="/books/" role="search">
				{{template "filter-ctx" (dict "Ctx" .Ctx "OOB" false)}}
				<label class="visually-hidden" for="books-q">Search your books</label>
				<input type="search" id="books-q" name="q" value="{{.Ctx.Q}}" placeholder="Search books, notes and quotes…"
				       hx-get="/books/" hx-target="#books-list" hx-swap="outerHTML"
				       hx-include="closest form" hx-trigger="input changed delay:300ms, search"
				       hx-replace-url="true">
			</form>
			{{template "list" .List}}
		</section>
	</div>
</div>
{{end}}

{{define "pane-toggle"}}<input type="checkbox" id="{{.ID}}" class="visually-hidden books-pane-toggle"{{if .Checked}} checked{{end}}{{if .OOB}} hx-swap-oob="true"{{end}} aria-label="{{.Label}}">{{end}}

{{define "back"}}<label class="toolbar-btn books-back-btn" for="{{.For}}">{{ticon "arrow-left"}}{{.Label}}</label>{{end}}

{{/* filter-ctx is the shelf, tag and series the filter box searches
     within. */}}
{{define "filter-ctx"}}<span id="books-filter-ctx"{{if .OOB}} hx-swap-oob="true"{{end}}><input type="hidden" name="shelf" value="{{.Ctx.Shelf}}">{{with .Ctx.Tag}}<input type="hidden" name="tag" value="{{.}}">{{end}}{{with .Ctx.Series}}<input type="hidden" name="series" value="{{.}}">{{end}}</span>{{end}}

{{/* ctx-fields is a listCtx as hidden fields, for POSTs. */}}
{{define "ctx-fields"}}<input type="hidden" name="shelf" value="{{.Shelf}}">{{with .Tag}}<input type="hidden" name="tag" value="{{.}}">{{end}}{{with .Q}}<input type="hidden" name="q" value="{{.}}">{{end}}{{with .Series}}<input type="hidden" name="series" value="{{.}}">{{end}}{{end}}

{{define "sidebar"}}
<nav class="books-side" id="books-side" aria-label="Shelves"{{if .OOB}} hx-swap-oob="true"{{end}}>
	<a class="button primary books-add" href="/books/new">{{ticon "plus"}}Add book</a>
	<ul class="books-shelves">
		{{range .Sidebar.Shelves}}
		<li><a class="books-shelf" href="{{.URL}}" hx-get="{{.URL}}" hx-target="#books-list" hx-swap="outerHTML" hx-push-url="true"{{if .Current}} aria-current="page"{{end}}>{{.Label}}<span class="books-count">{{if .Count}}<span class="visually-hidden">, </span>{{.Count}}<span class="visually-hidden"> {{if eq .Count 1}}book{{else}}books{{end}}</span>{{end}}</span></a></li>
		{{end}}
	</ul>
	{{with .Sidebar.Tags}}
	<h2 class="books-side-head">Tags</h2>
	<ul class="books-shelves">
		{{range .}}
		<li><a class="books-shelf" href="{{.URL}}" hx-get="{{.URL}}" hx-target="#books-list" hx-swap="outerHTML" hx-push-url="true"{{if .Current}} aria-current="page"{{end}}>{{.Name}}</a></li>
		{{end}}
	</ul>
	{{end}}
	<a class="books-stats-link" href="/books/stats">{{ticon "stats"}}Stats</a>
	<a class="books-side-link" href="/books/import">{{ticon "import"}}Import from Goodreads</a>
	<a class="books-side-link" href="/books/export" download>{{ticon "export"}}Export as Markdown</a>
</nav>
{{end}}

{{/* list takes a listView. A row is a plain link to the book's page. */}}
{{define "list"}}
<div class="books-list" id="books-list">
	{{with .Goal}}
	<a class="books-goal-card" href="/books/stats">
		<span class="books-goal-card-head">{{.Year}} goal</span>
		<span class="books-goal-card-text">{{.Text}}{{with .Pace}} · {{.}}{{end}}</span>
		<progress class="books-row-bar" max="100" value="{{.Percent}}" aria-hidden="true"></progress>
	</a>
	{{end}}
	<h2 class="books-list-head">{{.Heading}}</h2>
	{{if .Rows}}
	<ul class="books-rows">
		{{range .Rows}}
		<li class="books-row" data-book-id="{{.ID}}">
			<a class="books-row-link" href="{{.URL}}">
				{{if .Cover}}<img class="books-mini-cover" src="{{.Cover}}" alt="" width="32" height="48" loading="lazy">{{else}}<span class="books-mini-spine swatch-c-{{.Spine}}" aria-hidden="true">{{.Initial}}</span>{{end}}
				<span class="books-row-text">
					<span class="books-row-title">{{.Title}}</span>
					{{with .Byline}}<span class="books-row-byline">{{.}}</span>{{end}}
					<span class="books-row-note">{{if .Bar}}<progress class="books-row-bar" max="100" value="{{.Percent}}" aria-hidden="true"></progress> {{end}}{{if .Stars}}<span class="books-stars" role="img" aria-label="Rated {{.Rating}} of 5">{{.Stars}}</span> {{end}}{{.Note}}</span>
					{{with .Snippet}}<span class="books-row-snippet"><span class="books-snippet-in">{{.In}}</span> {{range .Parts}}{{if .Hit}}<mark>{{.Text}}</mark>{{else}}{{.Text}}{{end}}{{end}}</span>{{end}}
				</span>
			</a>
		</li>
		{{end}}
	</ul>
	{{else}}
	<p class="empty">{{.Empty}}</p>
	{{end}}
</div>
{{end}}
```

- [ ] **Step 6: Update the view types**

In `internal/apps/books/view.go`:

1. Replace the `panesView` doc comment and struct with:

   ```go
   // panesView is what the shelves and the list draw (UI1 Task 3; Task 4
   // replaces it with homeView). Title and Shell are for fragments, which
   // have no render.Page around them.
   type panesView struct {
   	Title   string
   	Shell   render.Shell
   	Ctx     listCtx
   	Sidebar sidebarView
   	List    listView
   }
   ```

2. In `rowView`, delete the `Active bool` field. In `listView`, delete the `Ctx` and `OOB` fields and their comments.

3. Replace `viewList`'s signature and first line with:

   ```go
   func viewList(items []ListItem, c listCtx) listView {
   	v := listView{Heading: listHeading(c)}
   ```

   Then remove `, Active: it.ID == openID` from the `rowView` literal inside the loop.

4. In `bookView`, change the doc comment to `// bookView is the book page's book (UI redesign spec "Book details page").` Delete the `Selected bool` field and add, after `ShelfLabel string`:

   ```go
   	BackLabel                        string // the page bar's way back: the list's name, "Reading", "Tagged “sf”"
   ```

   Re-run `gofmt` afterwards, because the field alignment changes.

5. In `viewBook`, change `v := bookView{Selected: true, ID: b.ID, …` to `v := bookView{ID: b.ID, …`, and add `BackLabel: listHeading(c),` to the same literal (for example after `ShelfLabel: b.Shelf.Label(),`).

6. Add, after `bookView`'s closing brace:

   ```go
   // bookPageView is the book page (UI redesign spec "Book details page").
   // Title and Shell are for fragments, which have no render.Page around
   // them; Error is a refusal's banner.
   type bookPageView struct {
   	Title string
   	Shell render.Shell
   	Error string
   	Book  bookView
   }
   ```

- [ ] **Step 7: Split the rendering**

In `internal/apps/books/handlers.go`:

- Replace `index`, `book`, `paneOpts` and `renderPanes` (everything from `// index is a list with no book open.` down to, but not including, `// shelfGoal is the goal card`) with:

```go
// index is the list.
func (a *App) index(w http.ResponseWriter, r *http.Request) {
	uid, ok := a.userID(w, r)
	if !ok {
		return
	}
	a.renderPanes(w, r, uid, ctxFrom(r.FormValue))
}

// book is a book's page, opened from list c (UI redesign spec "Book
// details page").
func (a *App) book(w http.ResponseWriter, r *http.Request) {
	uid, ok := a.userID(w, r)
	if !ok {
		return
	}
	id, ok := a.pathID(w, r)
	if !ok {
		return
	}
	a.renderBook(w, r, uid, id, ctxFrom(r.FormValue), bookOpts{})
}

// renderPanes draws the shelves and the list for c. An htmx request aimed
// at #books-list gets list-swap (the list, with the sidebar and the
// filter's fields out of band); anything else the whole page.
func (a *App) renderPanes(w http.ResponseWriter, r *http.Request, userID int64, c listCtx) {
	ctx := r.Context()
	counts, err := a.store.ShelfCounts(ctx, userID)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	tags, err := a.store.TagNames(ctx, userID)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	items, err := a.store.List(ctx, userID, ListQuery{Shelf: c.Shelf, Tag: c.Tag, Q: c.Q, Series: c.Series})
	if err != nil {
		a.fail(w, r, err)
		return
	}
	goal, err := a.shelfGoal(r, userID, c)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	page := a.deps.Page(r, listHeading(c))
	v := panesView{Title: page.Title, Shell: page.Shell, Ctx: c,
		Sidebar: viewSidebar(c, counts, tags), List: viewList(items, c)}
	v.List.Goal = goal
	if web.IsHTMX(r) && !web.IsHTMXHistoryRestore(r) && web.HTMXTarget(r) == "books-list" {
		if err := a.deps.Render.Fragment(w, http.StatusOK, "books/index", "list-swap", v); err != nil {
			a.deps.Errors.Internal(w, r, err)
		}
		return
	}
	page.Data = v
	if err := a.deps.Render.Page(w, http.StatusOK, "books/index", page); err != nil {
		a.deps.Errors.Internal(w, r, err)
	}
}

// bookOpts is what a book page shows besides the book: a refusal — in the
// banner, or for a progress update inside the progress box, or for a note
// or quote inside its form, with what was typed (spec "Errors": an inline
// message).
type bookOpts struct {
	Banner        string
	ProgressError string
	ProgressInput string
	Draft         entryDraft
}

// renderBook draws book id's page, opened from list c (UI redesign spec
// "Book details page"). A normal request gets the whole page — 422 when
// there is a refusal, so a refused form post without JavaScript isn't a
// 200. An htmx request gets the block for what it targeted:
// #books-progress → progress-swap, #books-notes → notes-swap,
// #books-quotes → quotes-swap, anything else → book-swap (#books-book).
// There is no list on the page, so none of them carries one. Fragments are
// always 200: htmx's default responseHandling only swaps 2xx/3xx.
func (a *App) renderBook(w http.ResponseWriter, r *http.Request, userID, id int64, c listCtx, opts bookOpts) {
	ctx := r.Context()
	b, err := a.store.Get(ctx, userID, id)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	rs, err := a.store.Readings(ctx, userID, id)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	ns, err := a.store.Notes(ctx, userID, id)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	qs, err := a.store.Quotes(ctx, userID, id)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	bv := viewBook(b, c, a.store.Today())
	bv.History = viewHistory(rs)
	bv.Notes, bv.NewNote = viewNotes(ns, opts.Draft), formFor("note-new", opts.Draft, entryForm{})
	bv.Quotes, bv.NewQuote = viewQuotes(qs, opts.Draft), formFor("quote-new", opts.Draft, entryForm{})
	if opts.ProgressError != "" {
		bv.Progress.Error, bv.Progress.Value = opts.ProgressError, opts.ProgressInput
	}
	page := a.deps.Page(r, b.Title)
	bv.Shell = page.Shell
	v := bookPageView{Title: page.Title, Shell: page.Shell, Error: opts.Banner, Book: bv}

	if web.IsHTMX(r) && !web.IsHTMXHistoryRestore(r) {
		block := "book-swap"
		switch web.HTMXTarget(r) {
		case "books-progress":
			block = "progress-swap"
		case "books-notes":
			block = "notes-swap"
		case "books-quotes":
			block = "quotes-swap"
		}
		if err := a.deps.Render.Fragment(w, http.StatusOK, "books/book", block, v); err != nil {
			a.deps.Errors.Internal(w, r, err)
		}
		return
	}
	status := http.StatusOK
	if opts.Banner != "" || opts.ProgressError != "" || opts.Draft.Error != "" {
		status = http.StatusUnprocessableEntity
	}
	page.Data = v
	if err := a.deps.Render.Page(w, status, "books/book", page); err != nil {
		a.deps.Errors.Internal(w, r, err)
	}
}
```

- [ ] **Step 8: Point the actions at the book page**

In `internal/apps/books/actions.go`:

1. Replace `act`'s doc comment and body with:

   ```go
   // act runs a change and answers with the book page: htmx gets its body
   // (book-swap), without JavaScript it is a redirect back to the book. Once
   // the book is gone (gone), its page is too: back to the list it was
   // opened from, by HX-Redirect for htmx (UI redesign spec "Swaps"). A
   // Refusal is the banner on the page rather than an error page.
   func (a *App) act(do change, gone bool) http.HandlerFunc {
   	return func(w http.ResponseWriter, r *http.Request) {
   		uid, ok := a.userID(w, r)
   		if !ok {
   			return
   		}
   		id, ok := a.pathID(w, r)
   		if !ok {
   			return
   		}
   		c := ctxFrom(r.PostFormValue)
   		err := do(r, uid, id)
   		var ref *Refusal
   		switch {
   		case errors.As(err, &ref):
   			a.renderBook(w, r, uid, id, c, bookOpts{Banner: ref.Msg})
   			return
   		case err != nil:
   			a.fail(w, r, err)
   			return
   		}
   		switch {
   		case gone && web.IsHTMX(r):
   			w.Header().Set("HX-Redirect", c.ListURL())
   		case gone:
   			http.Redirect(w, r, c.ListURL(), http.StatusSeeOther)
   		case web.IsHTMX(r):
   			a.renderBook(w, r, uid, id, c, bookOpts{})
   		default:
   			http.Redirect(w, r, c.BookURL(id), http.StatusSeeOther)
   		}
   	}
   }
   ```

2. In `progress`, replace both `a.renderPanes(w, r, uid, c, paneOpts{BookID: id, ProgressError: ref.Msg, ProgressInput: typed})` and `a.renderPanes(w, r, uid, c, paneOpts{BookID: id})`. The first becomes `a.renderBook(w, r, uid, id, c, bookOpts{ProgressError: ref.Msg, ProgressInput: typed})` and the second `a.renderBook(w, r, uid, id, c, bookOpts{})`. In its doc comment, change "so the answer is the box with the list out of band;" to "so the answer is the box;".

3. In `saveEntry`, make the same replacements: `paneOpts{BookID: id, Draft: d}` becomes `bookOpts{Draft: d}` and `paneOpts{BookID: id}` becomes `bookOpts{}`, both through `a.renderBook(w, r, uid, id, c, …)`. In its doc comment, change "the answer is the section with the list out of band (renderPanes);" to "the answer is the section (renderBook);".

4. In `internal/apps/books/findcover.go`, change the `findCover` doc comment's "⋯ menu's Find cover" to "Find cover" and "the banner over the panes" to "the banner on the book page". Task 6 changes this function.

- [ ] **Step 9: Run the package tests to verify they pass**

Run: `go test ./internal/apps/books/ -count=1`
Expected: ok. If a template error names a missing define, check that Step 4's script appended the blocks and that `book-menu` is not referenced anywhere (`grep -rn book-menu internal/apps/books/templates` prints nothing).

- [ ] **Step 10: Run the full check, then commit**

Run: `gofmt -l . && go vet ./... && go run honnef.co/go/tools/cmd/staticcheck@v0.8.0 ./... && go test ./... -race -count=1`
Expected: no gofmt output, and everything ok.

```bash
git add internal/apps/books
git commit -m "feat(books): a book page of its own, with a page bar back to its list"
```

---

### Task 4: The two-pane home: sidebar, toolbar, grid or list, drawer

**Files:**
- Create: `internal/apps/books/templates/home.partial.html`
- Delete: `internal/apps/books/templates/panes.partial.html`
- Modify: `internal/apps/books/templates/index.html`, `internal/apps/books/handlers.go` and `internal/apps/books/view.go`
- Modify: `internal/ui/toolbar_icons.go` and `internal/ui/toolbar_icons_test.go`
- Test: create `internal/apps/books/home_test.go`; modify `handlers_test.go`, `goal_card_test.go`, `snippet_view_test.go`, `series_test.go`, `progress_view_test.go`, `covers_test.go` and `review_gaps_b1b_test.go`

**Interfaces:**
- Consumes:
  - `Store.Prefs`, `Prefs`, `View`, `Sort`, `Sorts`, `Sort.Label` (Task 1)
  - `ListQuery.Sort`/`Reversed` and `Store.TagsByBook` (Task 2)
  - `renderBook`, `bookPageView` and `book.partial.html`'s `ctx-fields` users (Task 3). `post-ctx` uses `ctx-fields`, so `home.partial.html` must define it.
- Produces:
  - `func (a *App) renderHome(w, r, userID int64, c listCtx)`, which Task 5's handler calls
  - `homeView`, `prefsView`, `itemView`, and `listView{View, Heading, Count, Items, Empty}`
  - DOM ids: `#books-home`, `#books-drawer`, `#books-side`, `#books-toolbar-head`, `#books-prefs`, `#books-sort`, `#books-reverse`, `#books-view-grid`, `#books-view-list`, `#books-q`, `#books-filter-ctx` and `#books-list`
  - Classes: `.books-list-grid`/`.books-list-list`, `.books-item`, `.books-item-link`, `.books-item-cover`, `.books-item-cover-bar`, `.books-item-spine`, `.books-item-title`, `.books-item-authors`, `.books-item-tags`, `.books-item-status`, `.books-status-{reading,want,read,dnf}`, `.books-item-bar`, `.books-item-date`, `.books-item-snippet`, `.books-heading`, `.books-heading-text`, `.books-drawer-btn` and `.books-drawer-dim`

- [ ] **Step 1: Write the failing tests**

Create `internal/apps/books/home_test.go`:

```go
package books_test

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/iliafrenkel/on-suite/internal/apps/books"
	"github.com/iliafrenkel/on-suite/internal/htmlassert"
)

// setPrefs saves one of userID's home-page settings through the store.
func setPrefs(t *testing.T, s *server, userID int64, p books.Prefs) {
	t.Helper()
	if err := s.Store.SetPrefs(context.Background(), userID, p); err != nil {
		t.Fatal(err)
	}
}

// itemSel is a selector for something inside book id's tile or row.
func itemSel(id int64, inner string) string {
	return fmt.Sprintf(`li[data-book-id="%d"] %s`, id, inner)
}

func TestHomeDrawsTheSavedView(t *testing.T) {
	s := newServer(t)
	add(t, s, s.Alice.User.ID, titled("Emma", "", books.ShelfReading))

	doc := s.Get(t, s.Alice, "/books/")
	doc.MustHave(".books-list-grid")
	doc.MustNotHave(".books-list-list")
	if v := attr(t, doc, "#books-view-grid", "aria-pressed"); v != "true" {
		t.Errorf("grid button aria-pressed = %q, want true (the default view)", v)
	}

	setPrefs(t, s, s.Alice.User.ID, books.Prefs{View: books.ViewList, Sort: books.SortDefault})
	doc = s.Get(t, s.Alice, "/books/")
	doc.MustHave(".books-list-list")
	if v := attr(t, doc, "#books-view-list", "aria-pressed"); v != "true" {
		t.Errorf("list button aria-pressed = %q, want true", v)
	}
	s.Get(t, s.Bob, "/books/").MustHave(".books-list-grid") // Bob's view is his own
}

func TestSearchShowsTheListWithItsSnippets(t *testing.T) {
	s := newServer(t)
	uid := s.Alice.User.ID
	add(t, s, uid, titled("Dune", "Frank Herbert", books.ShelfWant))

	doc := s.Get(t, s.Alice, "/books/?shelf=all&q=dune")
	doc.MustHave(".books-list-list")
	for _, id := range []string{"books-view-grid", "books-view-list"} {
		if _, ok := htmlassert.Attr(doc.MustHave("#"+id), "disabled"); !ok {
			t.Errorf("#%s enabled while searching, want disabled: results always show as the list", id)
		}
	}
	// Clearing the search (the box sends q=) brings the saved grid back.
	doc = htmlassert.Parse(t, hx(t, s, "/books/?shelf=all&q=", "books-list"))
	doc.MustHave(".books-list-grid")
	if _, ok := htmlassert.Attr(doc.MustHave("#books-view-grid"), "disabled"); ok {
		t.Error("the view switch is still disabled after clearing the search")
	}
	if got, _ := s.Store.Prefs(context.Background(), uid); got.View != books.ViewGrid {
		t.Errorf("saved view = %q after a search, want grid: a search never changes it", got.View)
	}
}

func TestTilesSayWhereEachBookStands(t *testing.T) {
	s := newServer(t)
	s.Clock.Set(time.Date(2026, 10, 9, 1, 0, 0, 0, time.UTC))
	ctx := context.Background()
	uid := s.Alice.User.ID
	paper := readingBook(t, s, "Dune", 600)
	audio := readingBook(t, s, "Emma", 300)
	if err := s.Store.SetFormat(ctx, uid, audio, "audio"); err != nil {
		t.Fatal(err)
	}
	for id, at := range map[int64]int{paper: 120, audio: 45} {
		if err := s.Store.RecordProgress(ctx, uid, id, at); err != nil {
			t.Fatal(err)
		}
	}
	fresh := readingBook(t, s, "Persuasion", 0)
	piranesi := add(t, s, uid, titled("Piranesi", "", books.ShelfWant))
	rated := add(t, s, uid, titled("Ulysses", "", books.ShelfRead))
	if err := s.Store.SetRating(ctx, uid, rated, 4); err != nil {
		t.Fatal(err)
	}
	unrated := add(t, s, uid, titled("Middlemarch", "", books.ShelfRead))
	dnf := readingBook(t, s, "Moby-Dick", 0)
	if err := s.Store.MarkDNF(ctx, uid, dnf, "", 0); err != nil {
		t.Fatal(err)
	}

	doc := s.Get(t, s.Alice, "/books/?shelf=all")
	for id, want := range map[int64]string{
		paper:    "Reading · p. 120 / 600",
		audio:    "Reading · 45%",
		fresh:    "Reading",
		piranesi: "Want to read",
		unrated:  "Read",
		dnf:      "Did not finish",
	} {
		if got := htmlassert.Text(doc.MustHave(itemSel(id, ".books-item-status"))); got != want {
			t.Errorf("book %d status = %q, want %q", id, got, want)
		}
	}
	if got := htmlassert.Text(doc.MustHave(itemSel(rated, ".books-stars"))); got != "★★★★☆" {
		t.Errorf("rated book's stars = %q", got)
	}
	doc.MustNotHave(itemSel(rated, ".books-item-status")) // the stars say it
	if v := attr(t, doc, itemSel(paper, ".books-item-cover-bar"), "value"); v != "20" {
		t.Errorf("Dune's cover bar = %q, want 20", v)
	}
	doc.MustNotHave(itemSel(fresh, ".books-item-cover-bar")) // no progress yet, no bar
	if c := attr(t, doc, itemSel(dnf, ".books-item-status"), "class"); !strings.Contains(c, "books-status-dnf") {
		t.Errorf("DNF status class = %q, want books-status-dnf", c)
	}
}

func TestGridShowsThreeTagsAndTheListFive(t *testing.T) {
	s := newServer(t)
	nb := titled("Dune", "", books.ShelfReading)
	nb.Tags = []string{"a1", "b2", "c3", "d4", "e5", "f6"}
	id := add(t, s, s.Alice.User.ID, nb)
	pills := func() string {
		return strings.Join(texts(s.Get(t, s.Alice, "/books/"), itemSel(id, ".books-pill")), "|")
	}
	if got := pills(); got != "a1|b2|c3|+3" {
		t.Errorf("grid tags = %q, want three and +3", got)
	}
	setPrefs(t, s, s.Alice.User.ID, books.Prefs{View: books.ViewList, Sort: books.SortDefault})
	if got := pills(); got != "a1|b2|c3|d4|e5|+1" {
		t.Errorf("list tags = %q, want five and +1", got)
	}
}

func TestTheReadShelfDatesBooksByTheirFinish(t *testing.T) {
	s := newServer(t)
	s.Clock.Set(time.Date(2026, 10, 9, 1, 0, 0, 0, time.UTC))
	nb := titled("Ulysses", "", books.ShelfRead)
	nb.FinishedOn = "2026-09-02"
	add(t, s, s.Alice.User.ID, nb)
	for path, want := range map[string]string{
		"/books/?shelf=read": "Finished 2 Sep 2026",
		"/books/?shelf=all":  "Added 9 Oct 2026",
	} {
		if got := htmlassert.Text(s.Get(t, s.Alice, path).MustHave(".books-item-date")); got != want {
			t.Errorf("%s date = %q, want %q", path, got, want)
		}
	}
}

func TestSortControlsFollowThePrefs(t *testing.T) {
	s := newServer(t)
	setPrefs(t, s, s.Alice.User.ID, books.Prefs{View: books.ViewGrid, Sort: books.SortTitle, Reversed: true})
	doc := s.Get(t, s.Alice, "/books/")
	if got := htmlassert.Text(doc.MustHave("#books-sort option[selected]")); got != "Title" {
		t.Errorf("selected sort = %q, want Title", got)
	}
	if got := strings.Join(texts(doc, "#books-sort option"), "|"); got != "Default|Date added|Title|Author" {
		t.Errorf("sort options = %q", got)
	}
	rev := doc.MustHave("#books-reverse")
	for name, want := range map[string]string{"value": "0", "title": "Z–A", "aria-pressed": "true"} {
		if got, _ := htmlassert.Attr(rev, name); got != want {
			t.Errorf("reverse button %s = %q, want %q", name, got, want)
		}
	}
	// A series keeps its reading order: there is no sort to change there.
	doc = s.Get(t, s.Alice, "/books/?shelf=all&series=Dune")
	for _, id := range []string{"books-sort", "books-reverse"} {
		if _, ok := htmlassert.Attr(doc.MustHave("#"+id), "disabled"); !ok {
			t.Errorf("#%s enabled on a series list, want disabled", id)
		}
	}
}

func TestHomeListsInTheSavedOrder(t *testing.T) {
	s := newServer(t)
	uid := s.Alice.User.ID
	add(t, s, uid, titled("Dune", "Frank Herbert", books.ShelfWant))
	add(t, s, uid, titled("Ulysses", "James Joyce", books.ShelfWant))
	add(t, s, uid, titled("Emma", "Jane Austen", books.ShelfWant))
	setPrefs(t, s, uid, books.Prefs{View: books.ViewList, Sort: books.SortAuthor})
	if got, want := rowTitles(s.Get(t, s.Alice, "/books/?shelf=want")), []string{"Emma", "Dune", "Ulysses"}; !slices.Equal(got, want) {
		t.Errorf("by author = %v, want %v", got, want)
	}
}

func TestTheSidebarHasTheLinksAndNoAddButton(t *testing.T) {
	s := newServer(t)
	doc := s.Get(t, s.Alice, "/books/")
	doc.MustNotHave(`.books-side a[href="/books/new"]`) // Add book is in the toolbar
	doc.MustHave(`.books-toolbar a[href="/books/new"]`)
	for _, href := range []string{"/books/stats", "/books/import", "/books/export"} {
		doc.MustHave(fmt.Sprintf(`.books-side-foot a[href="%s"]`, href))
	}
}
```

Then update the existing tests:

1. `handlers_test.go`:
   - In `rowTitles`, change the selector from `".books-row-title"` to `".books-item-title"`, and its comment to `// rowTitles is the home page's titles, in order.`
   - In `TestIndexOpensOnTheReadingShelf`, replace `doc.MustHave(`a[href="/books/new"]`)` and the following `doc.MustNotHave("#books-book")` / `isChecked(… "books-list-open")` block with:

     ```go
     	doc.MustHave(`.books-toolbar a[href="/books/new"]`)
     	doc.MustNotHave("#books-book")
     	if isChecked(t, doc, "books-drawer") {
     		t.Error("the phone drawer starts closed")
     	}
     	if got := htmlassert.Text(doc.MustHave(".books-heading-text")); got != "Reading" {
     		t.Errorf("heading = %q, want Reading", got)
     	}
     ```

   - In `TestEmptyShelfSaysSo`, change `.books-row` to `.books-item`.
   - In `TestHTMXListNavigationSwapsTheListOnly`, change the id list to `[]string{"books-side", "books-filter-ctx", "books-toolbar-head", "books-prefs", "books-drawer", "shell-crumb-tail"}` and `doc.MustNotHave("#books-panes")` to `doc.MustNotHave("#books-home")`.
2. `goal_card_test.go`:
   - In `TestReadingShelfShowsThisYearsGoal`, change `doc.MustHave("#books-list .books-goal-card")` to `doc.MustHave("#books-side .books-goal-card")`.
   - Replace `TestGoalCardIsOnlyOnTheReadingShelf` with:

     ```go
     func TestGoalCardIsInTheSidebarOfEveryList(t *testing.T) {
     	s := newServer(t)
     	s.Clock.Set(noon("2026-10-10"))
     	setGoal(t, s, s.Alice.User.ID, 2026, 30)
     	for _, path := range []string{"/books/", "/books/?shelf=want", "/books/?shelf=read", "/books/?shelf=all",
     		"/books/?shelf=all&tag=sf", "/books/?shelf=all&series=Dune"} {
     		s.Get(t, s.Alice, path).MustHave("#books-side .books-goal-card")
     	}
     }
     ```

3. `snippet_view_test.go`: change every `.books-row-snippet` to `.books-item-snippet` (four places).
4. `series_test.go` (`TestTheSeriesLinkFiltersTheList`):
   - Change `.books-list-head` to `.books-heading-text`.
   - Delete the `data-series` check (`if v, _ := htmlassert.Attr(doc.MustHave("#books-list"), "data-series"); …`).
5. `progress_view_test.go`, in the test that lists the Reading shelf (the one asserting `"25%"`):
   - Replace the `.books-row-note` check with:

     ```go
     	if got := htmlassert.Text(doc.MustHave(".books-item-status")); got != "Reading · p. 150 / 600" {
     		t.Errorf("first reading tile = %q, want Dune's page (the one with progress first)", got)
     	}
     ```

   - Change `"progress.books-row-bar"` to `"progress.books-item-cover-bar"`.
6. `covers_test.go`: change `list.QueryAll("img.books-mini-cover")` to `list.QueryAll(".books-item-cover img")`, and `".books-mini-spine"` to `".books-item-spine"`.
7. `review_gaps_b1b_test.go`: change both `img.books-mini-cover` selectors to `.books-item-cover img`.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/apps/books/ -count=1 2>&1 | tail -20`
Expected: FAIL. The new home tests can't find `.books-list-grid` or `#books-view-grid`.

- [ ] **Step 3: Add the four icons**

In `internal/ui/toolbar_icons.go`, add these entries to `toolbarIcons` after `"chevron-down"`:

```go
	"arrow-down": `<svg class="toolbar-icon" viewBox="0 0 24 24" aria-hidden="true">
		<path d="M12 5v14"/>
		<path d="M19 12l-7 7-7-7"/>
	</svg>`,
	"arrow-up": `<svg class="toolbar-icon" viewBox="0 0 24 24" aria-hidden="true">
		<path d="M12 19V5"/>
		<path d="M5 12l7-7 7 7"/>
	</svg>`,
	"grid": `<svg class="toolbar-icon" viewBox="0 0 24 24" aria-hidden="true">
		<rect x="4" y="4" width="7" height="7" rx="1"/>
		<rect x="13" y="4" width="7" height="7" rx="1"/>
		<rect x="4" y="13" width="7" height="7" rx="1"/>
		<rect x="13" y="13" width="7" height="7" rx="1"/>
	</svg>`,
	"list": `<svg class="toolbar-icon" viewBox="0 0 24 24" aria-hidden="true">
		<path d="M9 6h11M9 12h11M9 18h11"/>
		<path d="M4 6h.01M4 12h.01M4 18h.01"/>
	</svg>`,
```

In `internal/ui/toolbar_icons_test.go`, add `"arrow-down", "arrow-up", "grid", "list",` to the `names` slice in `TestToolbarIconForKnownNames`.

- [ ] **Step 4: Replace the list and sidebar views**

In `internal/apps/books/view.go`:

1. Replace `panesView` with:

   ```go
   // homeView is everything the home page draws (UI redesign spec "Home
   // page"). Title and Shell are for fragments, which have no render.Page
   // around them.
   type homeView struct {
   	Title   string
   	Shell   render.Shell
   	Ctx     listCtx
   	Sidebar sidebarView
   	Prefs   prefsView
   	List    listView
   }
   ```

2. In `sidebarView`, add a third field:

   ```go
   	Goal    *goalView // this year's goal, when one is set (spec "Sidebar")
   ```

   Then change `viewSidebar`'s signature to `func viewSidebar(c listCtx, counts map[Shelf]int, tags []string, goal *goalView) sidebarView`, and its first line to `v := sidebarView{Goal: goal}`.

3. Replace `rowView`, `listView`, `viewList` and `rowNote` (doc comments included) with:

   ```go
   // itemView is one book on the home page: a tile in the grid, a row in
   // the list (UI redesign spec "Book tile", "Book row").
   type itemView struct {
   	ID       int64
   	URL      string
   	Title    string
   	Authors  string
   	Status   string // "Reading · 45%", "Want to read", "Read", "Did not finish"; "" when Stars says it
   	Kind     string // the status line's colour: the shelf, "reading", "want", "read" or "dnf"
   	Stars    string // a rated read book: "★★★★☆"
   	Rating   int
   	Bar      bool // a reading with progress: Percent as a bar
   	Percent  int
   	Date     string   // "Added 3 Oct 2026", or "Finished 2 Sep 2026" on the Read shelf
   	Tags     []string // the first few: 3 in the grid, 5 in the list
   	MoreTags int      // how many more, for the "+N" pill
   	Spine    string   // swatch colour name
   	Initial  string
   	Cover    string       // the stored cover; "" draws the spine
   	Snippet  *snippetView // where a searched book matched; nil when it shows already
   }

   // listView is the grid or the list. View is what it draws: the saved
   // view, or the list while searching, so snippets have room (spec
   // "Decisions at a glance").
   type listView struct {
   	View    View
   	Heading string
   	Count   int
   	Items   []itemView
   	Empty   string
   }

   // tagsShown is how many tags a tile shows, and a row (spec "Book tile").
   var tagsShown = map[View]int{ViewGrid: 3, ViewList: 5}

   func listHeading(c listCtx) string {
   	switch {
   	case c.Series != "":
   		return "Series “" + c.Series + "”"
   	case c.Tag != "":
   		return "Tagged “" + c.Tag + "”"
   	}
   	return c.Shelf.Label()
   }

   // viewList draws items for list c in view, with each book's tags from
   // tags (TagsByBook).
   func viewList(items []ListItem, tags map[int64][]string, c listCtx, view View) listView {
   	if c.Q != "" {
   		view = ViewList
   	}
   	v := listView{View: view, Heading: listHeading(c), Count: len(items)}
   	for _, it := range items {
   		item := itemView{ID: it.ID, URL: c.BookURL(it.ID), Title: it.Title, Authors: it.Authors,
   			Status: itemStatus(it), Kind: string(it.Shelf), Date: itemDate(it, c),
   			Spine: SpineColor(it.Title), Initial: initial(it.Title), Cover: coverURL(it.ID, it.CoverVersion),
   			Snippet: newSnippet(it)}
   		if it.Shelf == ShelfReading && it.Progress.Set() {
   			item.Bar, item.Percent = true, it.Progress.Percent(it.Pages)
   		}
   		if it.Shelf == ShelfRead && it.Rating > 0 {
   			item.Status, item.Rating, item.Stars = "", it.Rating, stars(it.Rating)
   		}
   		item.Tags, item.MoreTags = firstTags(tags[it.ID], tagsShown[view])
   		v.Items = append(v.Items, item)
   	}
   	if len(v.Items) == 0 {
   		v.Empty = emptyText(c)
   	}
   	return v
   }

   // itemStatus is a tile's status line (spec "Book tile"): how far through
   // a book being read is — in pages when that is how it is counted — or
   // which shelf it is on.
   func itemStatus(it ListItem) string {
   	switch it.Shelf {
   	case ShelfReading:
   		switch {
   		case !it.Progress.Set():
   			return "Reading"
   		case UnitFor(it.Format, it.Pages) == UnitPage:
   			return "Reading · p. " + strconv.Itoa(it.Progress.In(UnitPage, it.Pages)) + " / " + strconv.Itoa(it.Pages)
   		}
   		return "Reading · " + strconv.Itoa(it.Progress.Percent(it.Pages)) + "%"
   	case ShelfRead:
   		return "Read"
   	case ShelfDNF:
   		return "Did not finish"
   	}
   	return "Want to read"
   }

   // itemDate is a tile's date: when the book was finished on the Read
   // shelf, when it was added everywhere else (spec "Book tile").
   func itemDate(it ListItem, c listCtx) string {
   	if c.Shelf == ShelfRead && it.FinishedOn != "" {
   		return "Finished " + ShowDay(it.FinishedOn)
   	}
   	return "Added " + it.AddedAt.Local().Format("2 Jan 2006")
   }

   // firstTags is at most n of tags, and how many are left over.
   func firstTags(tags []string, n int) ([]string, int) {
   	if len(tags) <= n {
   		return tags, 0
   	}
   	return tags[:n], len(tags) - n
   }

   // prefsView is the toolbar's sort and view controls (spec "Toolbar").
   type prefsView struct {
   	Ctx       listCtx
   	Sorts     []choice
   	Reversed  bool
   	DirLabel  string // the order now: "Newest first", "Oldest first", "A–Z", "Z–A"
   	View      View   // the saved view
   	SortFixed bool   // a series is in reading order: the sort is disabled
   	ViewFixed bool   // a search shows the list: the view switch is disabled
   }

   func viewPrefs(p Prefs, c listCtx) prefsView {
   	v := prefsView{Ctx: c, Reversed: p.Reversed, DirLabel: dirLabel(p.Sort, p.Reversed), View: p.View,
   		SortFixed: c.Series != "", ViewFixed: c.Q != ""}
   	for _, s := range Sorts {
   		v.Sorts = append(v.Sorts, choice{Value: string(s), Label: s.Label(), Current: s == p.Sort})
   	}
   	return v
   }

   // dirLabel names the order a sort is in (spec "Sorting"): Title and
   // Author run A–Z, the rest newest first, unless reversed.
   func dirLabel(s Sort, reversed bool) string {
   	alpha := s == SortTitle || s == SortAuthor
   	switch {
   	case alpha && reversed:
   		return "Z–A"
   	case alpha:
   		return "A–Z"
   	case reversed:
   		return "Oldest first"
   	}
   	return "Newest first"
   }
   ```

   Keep `listHeading` defined only once: if it already sits above `viewList`, delete the old copy. Keep `byline` and `seriesText`, which `viewBook` and `form.go` still use.

- [ ] **Step 5: Render the home page**

In `internal/apps/books/handlers.go`:

- Change `index`'s body to call `a.renderHome(w, r, uid, ctxFrom(r.FormValue))`, and its doc comment to `// index is the home page (UI redesign spec "Home page").`
- Replace `renderPanes` and `shelfGoal` (doc comments included) with:

```go
// renderHome draws the home page for list c (UI redesign spec "Home
// page"). An htmx request aimed at #books-list — a shelf, tag or series
// link, the search box, the sort and view controls — gets list-swap: the
// list, with the sidebar, the toolbar's heading and controls, the search's
// hidden fields and the drawer's checkbox out of band. Anything else is
// the whole page.
func (a *App) renderHome(w http.ResponseWriter, r *http.Request, userID int64, c listCtx) {
	ctx := r.Context()
	counts, err := a.store.ShelfCounts(ctx, userID)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	tagNames, err := a.store.TagNames(ctx, userID)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	prefs, err := a.store.Prefs(ctx, userID)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	items, err := a.store.List(ctx, userID, ListQuery{Shelf: c.Shelf, Tag: c.Tag, Q: c.Q, Series: c.Series,
		Sort: prefs.Sort, Reversed: prefs.Reversed})
	if err != nil {
		a.fail(w, r, err)
		return
	}
	byBook, err := a.store.TagsByBook(ctx, userID)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	goal, err := a.sidebarGoal(r, userID)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	page := a.deps.Page(r, listHeading(c))
	v := homeView{Title: page.Title, Shell: page.Shell, Ctx: c,
		Sidebar: viewSidebar(c, counts, tagNames, goal), Prefs: viewPrefs(prefs, c),
		List: viewList(items, byBook, c, prefs.View)}
	if web.IsHTMX(r) && !web.IsHTMXHistoryRestore(r) && web.HTMXTarget(r) == "books-list" {
		if err := a.deps.Render.Fragment(w, http.StatusOK, "books/index", "list-swap", v); err != nil {
			a.deps.Errors.Internal(w, r, err)
		}
		return
	}
	page.Data = v
	if err := a.deps.Render.Page(w, http.StatusOK, "books/index", page); err != nil {
		a.deps.Errors.Internal(w, r, err)
	}
}

// sidebarGoal is the sidebar's goal card: this year's goal, when one is
// set (UI redesign spec "Sidebar"). Read-only — it links to the Stats
// page, where the goal is set.
func (a *App) sidebarGoal(r *http.Request, userID int64) (*goalView, error) {
	today := a.store.now().Local()
	g, err := a.store.Goal(r.Context(), userID, today.Year())
	if err != nil || g.Target == 0 {
		return nil, err
	}
	v := viewGoal(g, today)
	return &v, nil
}
```

- [ ] **Step 6: Write the home templates**

Run `git rm internal/apps/books/templates/panes.partial.html`, then create `internal/apps/books/templates/home.partial.html`:

```html
{{/* ON Books' home page: the sidebar, and a grid or list of books with a
     toolbar above it (UI redesign spec "Home page",
     docs/superpowers/specs/2026-10-12-on-books-ui-redesign-design.md). On
     a phone the sidebar is a drawer: #books-drawer, a checkbox the toolbar
     button and the dim layer are labels for (PATTERNS.md, checkbox-driven
     CSS state). Every block takes a homeView unless its comment says
     otherwise. */}}

{{/* The <title> and crumb ride along with every fragment that changes the
     page, so the chrome follows it (#205). */}}
{{define "shell-oob"}}<title>{{if .Title}}{{.Title}} · {{end}}ON Suite</title><span id="shell-crumb-tail" hx-swap-oob="true">{{template "shell-crumb-tail" (dict "Title" .Title "Shell" .Shell)}}</span>{{end}}

{{/* list-swap answers anything aimed at #books-list: a shelf, tag or
     series link, the search box, the sort and view controls. The sidebar,
     the toolbar's heading and controls and the search's hidden fields come
     along out of band, and the drawer's checkbox comes back unchecked,
     which closes the drawer on a phone. The search box itself is never
     swapped (PATTERNS.md, SyncSearch). */}}
{{define "list-swap"}}{{template "shell-oob" .}}{{template "list" .List}}{{template "sidebar" (dict "Sidebar" .Sidebar "OOB" true)}}{{template "toolbar-head" (dict "List" .List "OOB" true)}}{{template "prefs" (dict "Prefs" .Prefs "Shell" .Shell "OOB" true)}}{{template "filter-ctx" (dict "Ctx" .Ctx "OOB" true)}}{{template "drawer-toggle" true}}{{end}}

{{define "home"}}
<div class="books-home" id="books-home">
	{{template "drawer-toggle" false}}
	{{template "sidebar" (dict "Sidebar" .Sidebar "OOB" false)}}
	<div class="pane-gutter" data-gutter-for="side" role="separator"
	     aria-orientation="vertical" aria-label="Resize the shelves" tabindex="0"></div>
	<label class="books-drawer-dim" for="books-drawer" aria-hidden="true"></label>
	<section class="books-main" aria-label="Books">
		<div class="books-toolbar">
			{{template "toolbar-head" (dict "List" .List "OOB" false)}}
			{{/* The search lives outside #books-list so a list swap never
			     replaces the box being typed in (PATTERNS.md, SyncSearch). */}}
			<form class="books-filter" method="get" action="/books/" role="search">
				{{template "filter-ctx" (dict "Ctx" .Ctx "OOB" false)}}
				<label class="visually-hidden" for="books-q">Search your books</label>
				<input type="search" id="books-q" name="q" value="{{.Ctx.Q}}" placeholder="Search books, notes and quotes…"
				       hx-get="/books/" hx-target="#books-list" hx-swap="outerHTML"
				       hx-include="closest form" hx-trigger="input changed delay:300ms, search"
				       hx-replace-url="true">
			</form>
			{{template "prefs" (dict "Prefs" .Prefs "Shell" .Shell "OOB" false)}}
			<a class="button primary books-add" href="/books/new">{{ticon "plus"}}Add book</a>
		</div>
		{{template "list" .List}}
	</section>
</div>
{{end}}

{{/* drawer-toggle is the phone drawer's checkbox; a sibling before the
     sidebar so the CSS reaches it with ~. Takes a bool: out of band. */}}
{{define "drawer-toggle"}}<input type="checkbox" id="books-drawer" class="visually-hidden books-drawer-toggle" aria-label="Show the shelves"{{if .}} hx-swap-oob="true"{{end}}>{{end}}

{{/* toolbar-head is the list's name and count, and on a phone the button
     that opens the drawer, named after the list. Takes a dict: List (a
     listView), OOB. */}}
{{define "toolbar-head"}}
<div class="books-toolbar-head" id="books-toolbar-head"{{if .OOB}} hx-swap-oob="true"{{end}}>
	<label class="toolbar-btn books-drawer-btn" for="books-drawer">{{ticon "sidebar-toggle"}}<span class="books-drawer-label">{{.List.Heading}}</span>{{ticon "chevron-down"}}</label>
	<h1 class="books-heading"><span class="books-heading-text">{{.List.Heading}}</span> <span class="books-count"><span class="visually-hidden">, </span>{{.List.Count}}<span class="visually-hidden"> {{if eq .List.Count 1}}book{{else}}books{{end}}</span></span></h1>
</div>
{{end}}

{{/* prefs is the toolbar's sort and view controls, saved to the account
     (spec "Preferences"). One form: the select posts on change, each button
     posts its own name and value; without JavaScript the select has a Sort
     button. Takes a dict: Prefs (a prefsView), Shell, OOB. */}}
{{define "prefs"}}
<form class="books-prefs" id="books-prefs" method="post" action="/books/prefs"
      hx-post="/books/prefs" hx-trigger="change, submit" hx-target="#books-list" hx-swap="outerHTML"{{if .OOB}} hx-swap-oob="true"{{end}}>
	<input type="hidden" name="{{csrfField}}" value="{{.Shell.CSRFToken}}">
	{{template "ctx-fields" .Prefs.Ctx}}
	<label class="visually-hidden" for="books-sort">Sort by</label>
	<select id="books-sort" name="sort"{{if .Prefs.SortFixed}} disabled title="A series is in reading order"{{end}}>
		{{range .Prefs.Sorts}}<option value="{{.Value}}"{{if .Current}} selected{{end}}>{{.Label}}</option>{{end}}
	</select>
	<noscript><button type="submit" class="toolbar-btn">Sort</button></noscript>
	<button type="submit" id="books-reverse" class="toolbar-btn books-icon-btn" name="reversed" value="{{if .Prefs.Reversed}}0{{else}}1{{end}}"
	        aria-pressed="{{if .Prefs.Reversed}}true{{else}}false{{end}}" aria-label="Reverse the order (now {{.Prefs.DirLabel}})" title="{{.Prefs.DirLabel}}"{{if .Prefs.SortFixed}} disabled{{end}}>{{if .Prefs.Reversed}}{{ticon "arrow-up"}}{{else}}{{ticon "arrow-down"}}{{end}}</button>
	<span class="books-views" role="group" aria-label="View">
		<button type="submit" id="books-view-grid" class="toolbar-btn books-icon-btn" name="view" value="grid" aria-label="Grid" title="{{if .Prefs.ViewFixed}}Search results show as a list{{else}}Grid{{end}}"
		        aria-pressed="{{if eq .Prefs.View "grid"}}true{{else}}false{{end}}"{{if .Prefs.ViewFixed}} disabled{{end}}>{{ticon "grid"}}</button>
		<button type="submit" id="books-view-list" class="toolbar-btn books-icon-btn" name="view" value="list" aria-label="List" title="{{if .Prefs.ViewFixed}}Search results show as a list{{else}}List{{end}}"
		        aria-pressed="{{if eq .Prefs.View "list"}}true{{else}}false{{end}}"{{if .Prefs.ViewFixed}} disabled{{end}}>{{ticon "list"}}</button>
	</span>
</form>
{{end}}

{{/* filter-ctx is the shelf, tag and series the search box searches
     within. */}}
{{define "filter-ctx"}}<span id="books-filter-ctx"{{if .OOB}} hx-swap-oob="true"{{end}}><input type="hidden" name="shelf" value="{{.Ctx.Shelf}}">{{with .Ctx.Tag}}<input type="hidden" name="tag" value="{{.}}">{{end}}{{with .Ctx.Series}}<input type="hidden" name="series" value="{{.}}">{{end}}</span>{{end}}

{{/* ctx-fields is a listCtx as hidden fields, for POSTs. */}}
{{define "ctx-fields"}}<input type="hidden" name="shelf" value="{{.Shelf}}">{{with .Tag}}<input type="hidden" name="tag" value="{{.}}">{{end}}{{with .Q}}<input type="hidden" name="q" value="{{.}}">{{end}}{{with .Series}}<input type="hidden" name="series" value="{{.}}">{{end}}{{end}}

{{/* sidebar: the shelves, the goal, the tags, and the links at its foot
     (spec "Sidebar"). Takes a dict: Sidebar (a sidebarView), OOB. */}}
{{define "sidebar"}}
<nav class="books-side" id="books-side" aria-label="Shelves"{{if .OOB}} hx-swap-oob="true"{{end}}>
	<ul class="books-shelves">
		{{range .Sidebar.Shelves}}
		<li><a class="books-shelf" href="{{.URL}}" hx-get="{{.URL}}" hx-target="#books-list" hx-swap="outerHTML" hx-push-url="true"{{if .Current}} aria-current="page"{{end}}>{{.Label}}<span class="books-count">{{if .Count}}<span class="visually-hidden">, </span>{{.Count}}<span class="visually-hidden"> {{if eq .Count 1}}book{{else}}books{{end}}</span>{{end}}</span></a></li>
		{{end}}
	</ul>
	{{with .Sidebar.Goal}}
	<a class="books-goal-card" href="/books/stats">
		<span class="books-goal-card-head">{{.Year}} goal</span>
		<span class="books-goal-card-text">{{.Text}}{{with .Pace}} · {{.}}{{end}}</span>
		<progress class="books-bar books-goal-bar" max="100" value="{{.Percent}}" aria-hidden="true"></progress>
	</a>
	{{end}}
	{{with .Sidebar.Tags}}
	<h2 class="books-side-head">Tags</h2>
	<ul class="books-shelves">
		{{range .}}
		<li><a class="books-shelf" href="{{.URL}}" hx-get="{{.URL}}" hx-target="#books-list" hx-swap="outerHTML" hx-push-url="true"{{if .Current}} aria-current="page"{{end}}>{{.Name}}</a></li>
		{{end}}
	</ul>
	{{end}}
	<div class="books-side-foot">
		<a class="books-side-link" href="/books/stats">{{ticon "stats"}}Stats</a>
		<a class="books-side-link" href="/books/import">{{ticon "import"}}Import from Goodreads</a>
		<a class="books-side-link" href="/books/export" download>{{ticon "export"}}Export as Markdown</a>
	</div>
</nav>
{{end}}

{{/* list takes a listView: tiles in a grid or rows in a list, one markup
     laid out by the books-list-grid / books-list-list class. */}}
{{define "list"}}
<div class="books-list books-list-{{.View}}" id="books-list">
	{{if .Items}}
	<ul class="books-items">
		{{range .Items}}{{template "item" .}}{{end}}
	</ul>
	{{else}}
	<p class="empty">{{.Empty}}</p>
	{{end}}
</div>
{{end}}

{{/* item is one book: a plain link to its page (spec "Book tile", "Book
     row"). The cover's bar shows in the grid, the meta bar in the list.
     Takes an itemView. */}}
{{define "item"}}
<li class="books-item" data-book-id="{{.ID}}">
	<a class="books-item-link" href="{{.URL}}">
		<span class="books-item-cover">
			{{if .Cover}}<img src="{{.Cover}}" alt="" width="96" height="144" loading="lazy">{{else}}<span class="books-item-spine swatch-c-{{.Spine}}" aria-hidden="true"><span class="books-item-spine-title">{{.Title}}</span><span class="books-item-initial">{{.Initial}}</span></span>{{end}}
			{{if .Bar}}<progress class="books-bar books-item-cover-bar" max="100" value="{{.Percent}}" aria-hidden="true"></progress>{{end}}
		</span>
		<span class="books-item-text">
			<span class="books-item-title">{{.Title}}</span>
			{{with .Authors}}<span class="books-item-authors">{{.}}</span>{{end}}
			{{with .Tags}}<span class="books-item-tags">{{range .}}<span class="books-pill">{{.}}</span>{{end}}{{with $.MoreTags}}<span class="books-pill">+{{.}}</span>{{end}}</span>{{end}}
			{{with .Snippet}}<span class="books-item-snippet"><span class="books-snippet-in">{{.In}}</span> {{range .Parts}}{{if .Hit}}<mark>{{.Text}}</mark>{{else}}{{.Text}}{{end}}{{end}}</span>{{end}}
		</span>
		<span class="books-item-meta">
			{{if .Stars}}<span class="books-stars" role="img" aria-label="Rated {{.Rating}} of 5">{{.Stars}}</span>{{else}}<span class="books-item-status books-status-{{.Kind}}">{{.Status}}</span>{{end}}
			{{if .Bar}}<progress class="books-bar books-item-bar" max="100" value="{{.Percent}}" aria-hidden="true"></progress>{{end}}
			<span class="books-item-date">{{.Date}}</span>
		</span>
	</a>
</li>
{{end}}
```

Replace `internal/apps/books/templates/index.html` with:

```html
{{define "head"}}<script src="/books/books.js" defer></script>{{end}}

{{define "content"}}{{template "home" .Data}}{{end}}
```

- [ ] **Step 7: Run the tests to verify they pass**

Run: `go test ./internal/apps/books/ ./internal/ui/ -count=1`
Expected: ok. If `TestTilesSayWhereEachBookStands` fails on `"Reading · 45%"`, check that `itemStatus` reads `UnitFor(it.Format, it.Pages)` and that `ListItem.Format` is filled. `List` already scans `r.format`.

- [ ] **Step 8: Run the full check, then commit**

Run: `gofmt -l . && go vet ./... && go run honnef.co/go/tools/cmd/staticcheck@v0.8.0 ./... && go test ./... -race -count=1`
Expected: clean. A U1000 on `countText` or `initial` means a caller was removed by mistake. `countText` is used by `viewBook`'s series text, and `initial` by `viewList`.

```bash
git add internal/apps/books internal/ui/toolbar_icons.go internal/ui/toolbar_icons_test.go
git commit -m "feat(books): two-pane home with a grid or list, sort controls and a phone drawer"
```

---

### Task 5: Save the view and sort (`POST /books/prefs`)

**Files:**
- Modify: `internal/apps/books/prefs.go` (add the handler) and `internal/apps/books/books.go` (the route)
- Test: create `internal/apps/books/prefs_view_test.go`

**Interfaces:**
- Consumes: `Store.Prefs`/`SetPrefs` (Task 1), `renderHome` and the `#books-prefs` form (Task 4), and `postHXTo` (in `progress_view_test.go`).
- Produces: `POST /books/prefs`, which takes `view`, `sort`, `reversed` and the list ctx fields.

- [ ] **Step 1: Write the failing tests**

Create `internal/apps/books/prefs_view_test.go`:

```go
package books_test

import (
	"context"
	"net/http"
	"net/url"
	"testing"

	"github.com/iliafrenkel/on-suite/internal/apps/books"
	"github.com/iliafrenkel/on-suite/internal/htmlassert"
)

func prefsOf(t *testing.T, s *server) books.Prefs {
	t.Helper()
	p, err := s.Store.Prefs(context.Background(), s.Alice.User.ID)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestPrefsSaveTheViewAndAnswerWithTheList(t *testing.T) {
	s := newServer(t)
	add(t, s, s.Alice.User.ID, titled("Emma", "", books.ShelfWant))
	add(t, s, s.Alice.User.ID, titled("Dune", "", books.ShelfReading))
	rec := postHXTo(t, s, "/books/prefs", "books-list", url.Values{"shelf": {"want"}, "view": {"list"}})
	if rec.Code != http.StatusOK {
		t.Fatalf("htmx prefs = %d, want 200", rec.Code)
	}
	doc := htmlassert.Parse(t, rec.Body.String())
	doc.MustHave(".books-list-list")
	if got := rowTitles(doc); len(got) != 1 || got[0] != "Emma" {
		t.Errorf("rows = %v, want the Want to read shelf the form posted from", got)
	}
	if v := attr(t, doc, "#books-prefs", "hx-swap-oob"); v != "true" {
		t.Errorf("prefs form hx-swap-oob = %q, want it swapped out of band", v)
	}
	if v := attr(t, doc, "#books-view-list", "aria-pressed"); v != "true" {
		t.Errorf("list button aria-pressed = %q after choosing it", v)
	}
	if got := prefsOf(t, s); got.View != books.ViewList {
		t.Errorf("saved view = %q, want list", got.View)
	}
}

func TestPrefsWithoutJavaScriptGoBackToTheList(t *testing.T) {
	s := newServer(t)
	s.Submit(t, s.Alice, "/books/prefs", url.Values{"shelf": {"all"}, "tag": {"sf"}, "sort": {"title"}},
		"/books/?shelf=all&tag=sf")
	if got, want := prefsOf(t, s), (books.Prefs{View: books.ViewGrid, Sort: books.SortTitle}); got != want {
		t.Errorf("prefs = %+v, want %+v", got, want)
	}
}

func TestANewSortStartsInItsNaturalDirection(t *testing.T) {
	s := newServer(t)
	setPrefs(t, s, s.Alice.User.ID, books.Prefs{View: books.ViewGrid, Sort: books.SortAdded, Reversed: true})
	steps := []struct {
		form url.Values
		want books.Prefs
	}{
		// The select always sends the current sort; that alone changes nothing.
		{url.Values{"sort": {"added"}}, books.Prefs{View: books.ViewGrid, Sort: books.SortAdded, Reversed: true}},
		{url.Values{"sort": {"title"}}, books.Prefs{View: books.ViewGrid, Sort: books.SortTitle}},
		{url.Values{"sort": {"title"}, "reversed": {"1"}}, books.Prefs{View: books.ViewGrid, Sort: books.SortTitle, Reversed: true}},
		{url.Values{"sort": {"title"}, "reversed": {"0"}}, books.Prefs{View: books.ViewGrid, Sort: books.SortTitle}},
		{url.Values{"view": {"list"}}, books.Prefs{View: books.ViewList, Sort: books.SortTitle}},
	}
	for _, st := range steps {
		st.form.Set("shelf", "reading")
		s.Submit(t, s.Alice, "/books/prefs", st.form, "/books/?shelf=reading")
		if got := prefsOf(t, s); got != st.want {
			t.Errorf("after %v: prefs = %+v, want %+v", st.form, got, st.want)
		}
	}
}

func TestPrefsRefuseWhatTheToolbarNeverSends(t *testing.T) {
	s := newServer(t)
	for _, form := range []url.Values{{"view": {"table"}}, {"sort": {"rating"}}, {"reversed": {"2"}}} {
		if rec := s.Post(t, s.Alice, "/books/prefs", form); rec.Code != http.StatusBadRequest {
			t.Errorf("POST %v = %d, want 400", form, rec.Code)
		}
	}
	if got := prefsOf(t, s); got != books.DefaultPrefs {
		t.Errorf("prefs after refused posts = %+v, want the defaults", got)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/apps/books/ -run 'Prefs|NewSort' -count=1`
Expected: FAIL with 404s, because `/books/prefs` is not routed.

- [ ] **Step 3: Write the handler**

Append to `internal/apps/books/prefs.go`, and add `"net/http"` and `"github.com/iliafrenkel/on-suite/internal/platform/web"` to its imports:

```go
// setPrefs saves whichever of view, sort and reversed the toolbar's form
// carries (UI redesign spec "Preferences"). The select always sends the
// current sort, so only a different one changes it — and a new sort
// starts in its natural direction. htmx gets the list back
// (renderHome's list-swap); without JavaScript it is a redirect to the
// list the form posted from. Anything the toolbar never sends is a 400.
func (a *App) setPrefs(w http.ResponseWriter, r *http.Request) {
	uid, ok := a.userID(w, r)
	if !ok {
		return
	}
	p, err := a.store.Prefs(r.Context(), uid)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	if v := r.PostFormValue("view"); v != "" {
		p.View = View(v)
	}
	if v := Sort(r.PostFormValue("sort")); v != "" && v != p.Sort {
		p.Sort, p.Reversed = v, false
	}
	switch r.PostFormValue("reversed") {
	case "":
	case "1":
		p.Reversed = true
	case "0":
		p.Reversed = false
	default:
		a.fail(w, r, ErrInvalid)
		return
	}
	if err := a.store.SetPrefs(r.Context(), uid, p); err != nil {
		a.fail(w, r, err)
		return
	}
	c := ctxFrom(r.PostFormValue)
	if web.IsHTMX(r) {
		a.renderHome(w, r, uid, c)
		return
	}
	http.Redirect(w, r, c.ListURL(), http.StatusSeeOther)
}
```

In `internal/apps/books/books.go`, add the route after `r.HandleFunc("GET /b/{id}", a.book)`:

```go
	r.HandleFunc("POST /prefs", a.setPrefs)
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/apps/books/ -count=1`
Expected: ok.

- [ ] **Step 5: Commit**

```bash
git add internal/apps/books/prefs.go internal/apps/books/books.go internal/apps/books/prefs_view_test.go
git commit -m "feat(books): save the home view and sort from the toolbar"
```

---

### Task 6: Find cover moves to the Edit page

The ⋯ menu went in Task 3. The spec puts Find cover on the Edit page's Cover section, so it moves there now instead of waiting for UI2. Otherwise the feature would be missing in between.

**Files:**
- Modify: `internal/apps/books/findcover.go`, `internal/apps/books/form.go`, `internal/apps/books/books.go` and `internal/apps/books/templates/form.html`
- Test: `internal/apps/books/findcover_test.go`

**Interfaces:**
- Consumes: `editBookForm`, `renderForm`, `valuesOf`, `coverURL`, `lookUpCover` and `listCtx.EditURL` (all existing).
- Produces:
  - `func (a *App) findCover(w http.ResponseWriter, r *http.Request)`, a plain handler where it used to be a `change`
  - `formView.ID int64`, `formView.Found bool` and `formView.CoverNotice string`

- [ ] **Step 1: Rewrite the Find cover tests (failing)**

In `internal/apps/books/findcover_test.go`, replace the `findCover` helper and the three remaining tests (`TestFindCoverByISBNThenByTitle`, `TestFindCoverSaysWhenItCant` and `TestFindCoverOfSomeoneElsesBookIsNotFound`) with the code below. Keep the file's other helpers (`withISBN`, `coverSource`, `checkedAt` and the like) as they are, and adjust the imports so that only what is used remains.

```go
// findCover presses Find cover on Alice's book id, from the Edit page of
// the Want to read shelf.
func findCover(t *testing.T, s *server, id int64) *httptest.ResponseRecorder {
	t.Helper()
	return s.Post(t, s.Alice, fmt.Sprintf("/books/find-cover/%d", id), url.Values{"shelf": {"want"}})
}

func TestFindCoverIsOnTheEditPageOfABookWithout(t *testing.T) {
	s := newServer(t)
	uid := s.Alice.User.ID
	id := add(t, s, uid, titled("Piranesi", "", books.ShelfWant))
	path := fmt.Sprintf("/books/edit/%d?shelf=want", id)
	doc := s.Get(t, s.Alice, path)
	doc.MustHave(`.books-cover-field button[form="books-find-cover"]`)
	if got := attr(t, doc, "form#books-find-cover", "action"); got != fmt.Sprintf("/books/find-cover/%d", id) {
		t.Errorf("Find cover form posts to %q", got)
	}
	if got := attr(t, doc, `form#books-find-cover input[name="shelf"]`, "value"); got != "want" {
		t.Errorf("Find cover form's shelf = %q, want the list the page came from", got)
	}
	if err := s.Store.SetCover(context.Background(), uid, id, "image/png", onePNG, books.CoverUpload); err != nil {
		t.Fatal(err)
	}
	doc = s.Get(t, s.Alice, path)
	doc.MustNotHave("form#books-find-cover")
	doc.MustNotHave(`button[form="books-find-cover"]`)
}

func TestFindCoverByISBNThenByTitle(t *testing.T) {
	s, _, _ := newBooksApp(t)
	uid := s.Alice.User.ID
	byISBN := withISBN(t, s, uid, "Anything", "9781635575637")
	byTitle := add(t, s, uid, titled("Piranesi", "Susanna Clarke", books.ShelfWant))
	// An ISBN Open Library has no cover for falls back to the title.
	fallback := withISBN(t, s, uid, "Piranesi", "9780316129084")
	for name, id := range map[string]int64{"by ISBN": byISBN, "by title": byTitle, "ISBN, then title": fallback} {
		rec := findCover(t, s, id)
		if want := fmt.Sprintf("/books/edit/%d?shelf=want&found=1", id); rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != want {
			t.Errorf("%s: Find cover = %d to %q, want 303 to %q", name, rec.Code, rec.Header().Get("Location"), want)
		}
		c, err := s.Store.Cover(context.Background(), uid, id)
		if err != nil || !bytes.Equal(c.Bytes, onePNG) || coverSource(t, s, id) != books.CoverFromOL {
			t.Errorf("%s: cover = %d bytes, %v", name, len(c.Bytes), err)
		}
	}
	doc := s.Get(t, s.Alice, fmt.Sprintf("/books/edit/%d?shelf=want&found=1", byTitle))
	if got := htmlassert.Text(doc.MustHave(".books-cover-field .notice")); got != "Found a cover on Open Library." {
		t.Errorf("notice = %q", got)
	}
}

func TestFindCoverSaysWhenItCant(t *testing.T) {
	s, _, _ := newBooksApp(t)
	uid := s.Alice.User.ID
	nothing := add(t, s, uid, titled("Nothing like it", "", books.ShelfWant))
	broken := add(t, s, uid, titled("Broken", "", books.ShelfWant))
	tests := []struct {
		id   int64
		want string
	}{
		{nothing, "Open Library has no cover for this book. You can upload one, or paste the address of one."},
		{broken, "Open Library didn't answer. Try again in a minute."},
	}
	for _, tt := range tests {
		rec := findCover(t, s, tt.id)
		if rec.Code != http.StatusUnprocessableEntity {
			t.Errorf("Find cover with none = %d, want 422 (the Edit page again)", rec.Code)
		}
		doc := htmlassert.Parse(t, rec.Body.String())
		if got := htmlassert.Text(doc.MustHave(".books-cover-field .notice-error")); got != tt.want {
			t.Errorf("notice = %q, want %q", got, tt.want)
		}
		doc.MustHave("form.books-form") // the Edit page itself
	}
	if checkedAt(t, s, nothing) == "" || checkedAt(t, s, broken) != "" {
		t.Error("only a book Open Library has no cover for is marked checked")
	}
}

func TestFindCoverOfSomeoneElsesBookIsNotFound(t *testing.T) {
	s, _, _ := newBooksApp(t)
	id := withISBN(t, s, s.Bob.User.ID, "Piranesi", "9781635575637")
	if rec := findCover(t, s, id); rec.Code != http.StatusNotFound {
		t.Errorf("Alice's Find cover on Bob's book = %d, want 404", rec.Code)
	}
	if _, err := s.Store.Cover(context.Background(), s.Bob.User.ID, id); err == nil {
		t.Error("Bob's book got a cover")
	}
}
```

Add `"net/http/httptest"` to the imports.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/apps/books/ -run FindCover -count=1`
Expected: FAIL. The Edit page has no `form#books-find-cover`, and Find cover answers 200 with the book page, not 303.

- [ ] **Step 3: Give the form view what it needs**

In `internal/apps/books/form.go`, add these fields to `formView`, after `Spine string`:

```go
	// ID is the edited book's id; 0 on the Add page.
	ID int64
	// Found says the Edit page is back from a successful Find cover;
	// CoverNotice is why Find cover found nothing (UI redesign spec "Add
	// and Edit pages").
	Found       bool
	CoverNotice string
```

In `editBookForm`, add `ID: id,` to the returned literal. In `editForm`, replace the final `a.renderForm(…)` line with:

```go
	v := editBookForm(id, valuesOf(b.BookInput), nil, ctxFrom(r.FormValue), coverURL(b.ID, b.CoverVersion))
	v.Found = r.FormValue("found") == "1" && b.CoverVersion != ""
	a.renderForm(w, r, http.StatusOK, v)
```

- [ ] **Step 4: Make Find cover a handler that returns to the Edit page**

In `internal/apps/books/findcover.go`, replace `findCover` (doc comment included) with:

```go
// findCover is the Edit page's Find cover (UI redesign spec "Add and Edit
// pages"): the lookup, now. Found, it is back to the Edit page, which
// says so; finding none, or Open Library not answering, is the Edit page
// again with the reason by the cover, as a 422.
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
	ct, data, err := a.lookUpCover(ctx, b)
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
		v := editBookForm(id, valuesOf(b.BookInput), nil, c, coverURL(b.ID, b.CoverVersion))
		v.CoverNotice = msg
		a.renderForm(w, r, http.StatusUnprocessableEntity, v)
		return
	}
	if err := a.store.SetCover(ctx, uid, id, ct, data, CoverFromOL); err != nil {
		a.fail(w, r, err)
		return
	}
	http.Redirect(w, r, c.EditURL(id)+"&found=1", http.StatusSeeOther)
}
```

Add `"net/http"` to `findcover.go`'s imports if it isn't there. Leave the `Refusal` type alone, because other actions use it.

In `internal/apps/books/books.go`, change `r.HandleFunc("POST /find-cover/{id}", a.act(a.findCover, false))` to `r.HandleFunc("POST /find-cover/{id}", a.findCover)`.

- [ ] **Step 5: Put the button on the Edit page**

In `internal/apps/books/templates/form.html`, inside the Cover fieldset:

1. After the `{{if $d.Cover}}<label class="books-choice">…Remove the cover</label>{{end}}` line (still inside the inner `.stack` div), add:

   ```html
   					{{if not $d.Cover}}<p><button type="submit" form="books-find-cover" class="button">Find a cover on Open Library</button></p>{{end}}
   ```

2. Just before `{{template "field-error" (dict "Name" "cover" …)}}`, add:

   ```html
   			{{if $d.Found}}<p class="notice" role="status">Found a cover on Open Library.</p>{{end}}
   			{{with $d.CoverNotice}}<p class="notice notice-error" role="alert">{{.}}</p>{{end}}
   ```

3. After the main form's closing `</form>`, before the page's closing `</div>`, add the small form the button belongs to. A form can't sit inside another, which is why it is separate:

   ```html
   	{{if and (not $d.New) (not $d.Cover)}}
   	<form id="books-find-cover" method="post" action="/books/find-cover/{{$d.ID}}" hidden>
   		<input type="hidden" name="{{csrfField}}" value="{{.Shell.CSRFToken}}">
   		{{template "ctx-fields" $d.Ctx}}
   	</form>
   	{{end}}
   ```

- [ ] **Step 6: Run the tests to verify they pass**

Run: `go test ./internal/apps/books/ -count=1`
Expected: ok.

- [ ] **Step 7: Run the full check, then commit**

Run: `gofmt -l . && go vet ./... && go run honnef.co/go/tools/cmd/staticcheck@v0.8.0 ./... && go test ./... -race -count=1`
Expected: clean.

```bash
git add internal/apps/books
git commit -m "feat(books): Find cover on the Edit page, now that the book menu is gone"
```

---

### Task 7: Style the home and the book page

No Go tests cover CSS, so this task is checked in the browser (Step 4).

**Files:**
- Modify: `internal/ui/static/app.css`, in the `/* ---- ON Books ----` block (around line 4129)

- [ ] **Step 1: Replace the layout part of the ON Books block**

In `internal/ui/static/app.css`, replace everything from the line `/* ---- ON Books ----------------------------------------------------------` up to (not including) the line `/* Add / edit form */` with:

```css
/* ---- ON Books ----------------------------------------------------------
 * Two panes — the sidebar and a grid or list of books — and a book page of
 * its own (UI redesign spec,
 * docs/superpowers/specs/2026-10-12-on-books-ui-redesign-design.md). The
 * sidebar keeps Reader's .pane-gutter, its width a custom property on
 * <html> (books.js); up to 640px it is a drawer driven by #books-drawer
 * (PATTERNS.md, checkbox-driven CSS state). */
main:has(.books-home) { max-width: none; padding: 0; }

.books-home {
	--books-chrome: calc(4rem + var(--s-6) * 2);
	position: relative;
	display: flex;
	height: calc(100vh - var(--books-chrome));
	height: calc(100dvh - var(--books-chrome));
	min-height: 0;
	overflow: hidden;
}
.books-side { flex: 0 0 var(--books-side-w, 14rem); display: flex; flex-direction: column; min-width: 0; min-height: 0; overflow-y: auto; padding: var(--s-3); background: var(--c-bg-subtle); border-right: 1px solid var(--c-border); }
.books-main { flex: 1 1 auto; display: flex; flex-direction: column; min-width: 0; min-height: 0; overflow-y: auto; background: var(--c-bg); }
.books-drawer-dim,
.books-drawer-btn { display: none; }

/* Sidebar */
.books-shelves { list-style: none; margin: 0; padding: 0; }
.books-shelf { display: flex; justify-content: space-between; gap: var(--s-2); padding: var(--s-1) var(--s-2); border-radius: var(--radius); color: var(--c-text); text-decoration: none; }
.books-shelf:hover { background: var(--c-bg-inset); }
.books-shelf[aria-current="page"] { background: var(--c-accent-bg); color: var(--c-accent); font-weight: 600; }
.books-count { color: var(--c-text-dim); font-size: var(--fs-sm); }
.books-side-head { margin: var(--s-4) 0 var(--s-1); padding: 0 var(--s-2); font-size: var(--fs-xs); letter-spacing: 0.05em; text-transform: uppercase; color: var(--c-text-dim); }
.books-side-foot { margin-top: auto; padding-top: var(--s-3); border-top: 1px solid var(--c-border); }
.books-goal-card { display: block; margin-top: var(--s-3); padding: var(--s-2) var(--s-3); border: var(--border); border-radius: var(--radius); background: var(--c-bg); color: var(--c-text-dim); font-size: var(--fs-sm); text-decoration: none; }
.books-goal-card:hover { border-color: var(--c-accent); }
.books-goal-card-head { display: block; color: var(--c-text); font-weight: 600; }
.books-goal-card-text { display: block; }
.books-goal-bar { display: block; width: 100%; height: 4px; margin-top: var(--s-1); }

/* Toolbar */
.books-toolbar { position: sticky; top: 0; z-index: 1; display: flex; flex-wrap: wrap; align-items: center; gap: var(--s-2); padding: var(--s-2) var(--s-4); border-bottom: 1px solid var(--c-border); background: var(--c-bg); }
.books-toolbar-head { display: flex; align-items: center; min-width: 0; }
.books-heading { margin: 0 var(--s-2) 0 0; font-size: var(--fs-lg); white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
.books-heading .books-count { font-weight: 400; }
.books-filter { flex: 1 1 12rem; min-width: 0; margin: 0; }
.books-filter input[type="search"] { width: 100%; }
.books-prefs { display: flex; align-items: center; gap: var(--s-1); margin: 0; }
.books-prefs select { width: auto; }
.books-views { display: inline-flex; border: 1px solid var(--c-border-firm); border-radius: var(--radius); overflow: hidden; }
.books-views .toolbar-btn { border: 0; border-radius: 0; }
.books-icon-btn[aria-pressed="true"] { background: var(--c-accent-bg); color: var(--c-accent); }
.books-icon-btn:disabled { opacity: 0.5; cursor: default; }
.books-add { display: inline-flex; align-items: center; gap: var(--s-1); white-space: nowrap; }

/* Items: one markup, a tile in the grid or a row in the list */
.books-list { padding: var(--s-4); }
.books-items { list-style: none; margin: 0; padding: 0; }
.books-item-link { color: var(--c-text); text-decoration: none; }
.books-item-link:focus-visible { outline: var(--ring); outline-offset: 2px; border-radius: var(--radius); }
.books-item-cover { position: relative; display: block; flex: 0 0 auto; }
.books-item-cover img,
.books-item-spine { display: block; width: 100%; height: auto; aspect-ratio: 2 / 3; border-radius: 3px; box-shadow: 0 1px 3px rgba(0, 0, 0, 0.15); object-fit: cover; }
.books-item-spine { display: flex; align-items: flex-end; padding: var(--s-2); overflow: hidden; border-left: 6px solid var(--swatch); background: var(--swatch-soft); color: var(--c-text); font-size: var(--fs-xs); font-weight: 600; line-height: 1.2; overflow-wrap: anywhere; }
.books-item-initial { display: none; }
.books-item-cover-bar { position: absolute; left: 0; right: 0; bottom: 0; width: 100%; height: 4px; border-radius: 0 0 3px 3px; }
.books-item-title { font-weight: 600; }
.books-item-authors { color: var(--c-text-dim); font-size: var(--fs-sm); }
.books-item-tags { display: flex; flex-wrap: wrap; gap: var(--s-1); }
.books-item-status { color: var(--c-text-dim); font-size: var(--fs-xs); font-weight: 600; }
.books-status-reading { color: var(--c-accent); }
.books-status-dnf { color: var(--c-accent-attention); }
.books-item-date { color: var(--c-text-faint); font-size: var(--fs-xs); }
.books-item-bar { display: block; width: 7rem; height: 4px; margin: var(--s-1) 0; }
/* A searched row's third line: where the search matched (B3), held to
 * two lines. */
.books-item-snippet { display: -webkit-box; -webkit-box-orient: vertical; -webkit-line-clamp: 2; overflow: hidden; color: var(--c-text-dim); font-size: var(--fs-sm); overflow-wrap: anywhere; }
.books-item-snippet mark { background: var(--c-accent-attention-bg); color: inherit; border-radius: 2px; }
.books-snippet-in { color: var(--c-text-faint); }

/* Grid: tiles. The text and meta spans step aside (display: contents) so
 * the tile can order its lines as the mockup does. */
.books-list-grid .books-items { display: grid; grid-template-columns: repeat(auto-fill, minmax(8.5rem, 1fr)); gap: var(--s-5) var(--s-4); }
.books-list-grid .books-item-link { display: flex; flex-direction: column; gap: 2px; }
.books-list-grid .books-item-text,
.books-list-grid .books-item-meta { display: contents; }
.books-list-grid .books-item-cover { margin-bottom: var(--s-1); }
.books-list-grid .books-item-title { order: 1; display: -webkit-box; -webkit-box-orient: vertical; -webkit-line-clamp: 2; overflow: hidden; line-height: 1.25; }
.books-list-grid .books-item-authors { order: 2; }
.books-list-grid .books-item-status,
.books-list-grid .books-stars { order: 3; }
.books-list-grid .books-item-tags { order: 4; margin-top: 2px; }
.books-list-grid .books-item-date { order: 5; }
.books-list-grid .books-item-bar,
.books-list-grid .books-item-snippet { display: none; }
.books-list-grid .books-item-link:hover .books-item-title { color: var(--c-accent); }

/* List: rows */
.books-list-list .books-item-link { display: flex; align-items: center; gap: var(--s-3); padding: var(--s-2); border-bottom: 1px solid var(--c-border); }
.books-list-list .books-item-link:hover { background: var(--c-bg-subtle); }
.books-list-list .books-item-cover { width: 2.5rem; }
.books-list-list .books-item-spine { justify-content: center; align-items: center; padding: 0; border-left-width: 4px; }
.books-list-list .books-item-spine-title,
.books-list-list .books-item-cover-bar { display: none; }
.books-list-list .books-item-initial { display: inline; color: var(--swatch); font-size: var(--fs-base); font-weight: 700; }
.books-list-list .books-item-text { display: flex; flex-direction: column; gap: 2px; flex: 1 1 auto; min-width: 0; }
.books-list-list .books-item-title,
.books-list-list .books-item-authors { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.books-list-list .books-item-meta { display: flex; flex-direction: column; align-items: flex-end; flex: 0 0 auto; max-width: 40%; text-align: right; }

/* Phone: the sidebar is a drawer over the books */
#books-drawer:focus-visible ~ .books-main .books-drawer-btn { outline: var(--ring); }
@media (max-width: 640px) {
	.books-home { --books-chrome: calc(8rem + var(--s-4) * 2); }
	.books-home > .pane-gutter,
	.books-heading { display: none; }
	.books-side { position: absolute; z-index: 3; top: 0; bottom: 0; left: 0; width: min(18rem, 85%); transform: translateX(-105%); transition: transform 0.2s ease; box-shadow: 4px 0 16px rgba(0, 0, 0, 0.2); }
	.books-drawer-toggle:checked ~ .books-side { transform: none; }
	.books-drawer-toggle:checked ~ .books-drawer-dim { display: block; position: absolute; inset: 0; z-index: 2; background: rgba(0, 0, 0, 0.35); }
	.books-drawer-btn { display: inline-flex; max-width: 100%; }
	.books-drawer-label { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
	.books-toolbar { padding: var(--s-2) var(--s-3); }
	.books-filter { order: 5; flex-basis: 100%; }
	.books-list { padding: var(--s-3); }
	.books-list-grid .books-items { grid-template-columns: repeat(auto-fill, minmax(7rem, 1fr)); gap: var(--s-4) var(--s-3); }
}
@media (prefers-reduced-motion: reduce) {
	.books-side { transition: none; }
}

/* The book page: one centred column (spec "Book details page") */
.books-page { max-width: 48rem; margin: 0 auto; }
.books-book { padding-bottom: var(--s-6); }
.books-page-bar { display: flex; align-items: center; justify-content: space-between; gap: var(--s-3); margin-bottom: var(--s-4); padding: var(--s-2) 0; border-bottom: 1px solid var(--c-border); }
.books-back { display: inline-flex; align-items: center; gap: var(--s-1); color: var(--c-accent); font-size: var(--fs-sm); text-decoration: none; }
.books-back:hover { text-decoration: underline; }
.books-page-actions { display: flex; align-items: center; gap: var(--s-1); }
.books-page-actions form { margin: 0; }
.books-delete { color: var(--c-danger); }
.books-banner { margin: 0 0 var(--s-3); }

/* Book */
.books-book-head { display: flex; flex-wrap: wrap; align-items: flex-start; gap: var(--s-4); margin-bottom: var(--s-4); }
.books-spine { flex: 0 0 auto; display: flex; align-items: flex-end; width: 6rem; height: 9rem; padding: var(--s-2); overflow: hidden; border-left: 8px solid var(--swatch); border-radius: 3px; background: var(--swatch-soft); color: var(--c-text); font-size: var(--fs-xs); font-weight: 600; line-height: 1.2; overflow-wrap: anywhere; }
.books-book-titles { flex: 1 1 12rem; min-width: 0; }
.books-book-titles h1 { margin: 0; font-size: var(--fs-xl); }
.books-subtitle { margin: var(--s-1) 0 0; color: var(--c-text-dim); }
.books-authors { margin: var(--s-2) 0 0; font-weight: 600; }
.books-series,
.books-facts { margin: var(--s-1) 0 0; color: var(--c-text-dim); font-size: var(--fs-sm); }
.books-pills { display: flex; flex-wrap: wrap; gap: var(--s-1); margin: var(--s-2) 0 0; }
.books-pill { padding: 0 var(--s-2); border-radius: 999px; background: var(--c-bg-subtle); color: var(--c-text-dim); font-size: var(--fs-xs); }
.books-pill-shelf { background: var(--c-accent-bg); color: var(--c-accent); }
.books-description { max-width: 38rem; margin-bottom: var(--s-4); color: var(--c-text-dim); }
/* A long description is held to four lines until More is ticked (#566). */
.books-description-text { margin: 0; white-space: pre-line; }
.books-description-text.is-clamped { display: -webkit-box; -webkit-box-orient: vertical; -webkit-line-clamp: 4; overflow: hidden; }
.books-description-toggle:checked ~ .books-description-text { display: block; -webkit-line-clamp: none; overflow: visible; }
.books-description-more { display: inline-block; margin: var(--s-1) 0 0; color: var(--c-accent); font-size: var(--fs-sm); cursor: pointer; }
.books-less-label,
.books-description-toggle:checked ~ .books-description-more .books-more-label { display: none; }
.books-description-toggle:checked ~ .books-description-more .books-less-label { display: inline; }
.books-description-toggle:focus-visible ~ .books-description-more { outline: var(--ring); }

```

- [ ] **Step 2: Remove the three-pane leftovers further down the block**

In the same ON Books block:

1. Delete the `.books-menu { margin-left: auto; }` rule, its comment `/* "Edit details" is a link among the menu's buttons; match their look. */`, and the two `.books-menu .outline-menu-list a…` rules after it.
2. In the progress-bar rules, replace every `.books-row-bar` selector with `.books-bar`. There are four selector lists: the base rule, `::-webkit-progress-bar`, `::-webkit-progress-value` and `::-moz-progress-bar`. Then delete the line `.books-row-bar { display: inline-block; width: 3rem; height: 4px; vertical-align: middle; }`.
3. Delete the `.books-mini-cover { … }` rule, and keep `.books-cover`.
4. Delete the `.books-stats-link { … }` and `.books-stats-link:hover { … }` rules. Stats is a `.books-side-link` now.
5. Delete the old goal-card block: its comment `/* The goal card at the top of the Reading shelf: …` and the five `.books-goal-card…` rules under it. The new ones are in Step 1.

Then check that nothing still refers to the removed classes:

Run: `grep -rn 'books-row\|books-mini\|books-listpane\|books-panes\|books-menu\|books-stats-link\|books-back-btn\|books-pane-toggle\|books-list-head' internal/apps/books/templates internal/ui/static/app.css`
Expected: no output.

- [ ] **Step 3: Build and seed a server for a look**

```bash
go build -o /tmp/onsuite-bin ./cmd/onsuite
rm -rf /tmp/onsuite-manual-verify
go run ./docs/screenshots/seed --data-dir /tmp/onsuite-manual-verify
```

The seed creates the `demo` user with ON Books data. Its password is the published local demo value in `docs/screenshots/README.md`.

- [ ] **Step 4: Check it in the browser**

Start the `onsuite` preview (`.claude/launch.json`), sign in as `demo`, and open `/books/`. Check each of these:

- **Desktop, light:**
  - The home has a tinted sidebar with shelves, the goal card, tags, and Stats, Import and Export at the foot.
  - The toolbar has the heading and count, search, sort, ↓, the grid/list switch and **+ Add book** on one row.
  - Tiles have 2:3 covers, a 2-line title, the author, the status line, tags and the date.
  - The list view shows rows with a small cover on the left, the title, author and tags in the middle, and the status and date on the right.
- **Search:** type a word from a note. The list view shows with a highlighted snippet, and the view buttons are greyed. Clearing the box brings the grid back.
- **Book page:**
  - The column is centred and about 48rem wide.
  - The page bar has "← Reading" on the left and the ✎ and red 🗑 icons on the right.
  - The header, sections and confirm dialog look as before.
- **Phone width** (`resize_window` preset `mobile`):
  - The toolbar shows the "Reading ▾" button first and the search on its own row.
  - Tapping the button slides the sidebar in over a dimmed grid.
  - Tapping a shelf swaps the list and closes the drawer.
  - The book page has no horizontal scroll.
- **Dark** (`colorScheme: "dark"`): text and pills stay readable, and the drawer and dim layer look right.

Fix any CSS problem you find in `app.css`, using tokens only. Then reset the viewport with preset `desktop`.

- [ ] **Step 5: Commit**

```bash
git add internal/ui/static/app.css
git commit -m "style(books): the two-pane home, tiles and rows, the drawer and the book page"
```

---

### Task 8: `books.js` for the new layout

**Files:**
- Rewrite: `internal/apps/books/static/books.js`

**Interfaces:**
- Consumes these DOM ids and classes:
  - Task 3: `#books-page`, `#books-book`, `#books-confirm-dialog`, `#books-confirm-message`, `#books-confirm-ok`, `.books-dialog-cancel`, `.books-back`, `.books-edit-link`, `#books-progress-input`, `.books-progress-form`, `#books-note-new`, `#books-note-new-body`, `#books-quote-new` and `#books-quote-new-text`
  - Task 4: `#books-home`, `#books-side`, `.pane-gutter[data-gutter-for="side"]`, `#books-drawer`, `#books-prefs`, `#books-q` and `.books-item-link`

- [ ] **Step 1: Replace the script**

Replace the whole of `internal/apps/books/static/books.js` with:

```js
// ON Books' script: the delete confirmation, the sidebar's resize gutter,
// focus kept in the sort and view controls across a list swap, and
// keyboard shortcuts (UI redesign spec,
// docs/superpowers/specs/2026-10-12-on-books-ui-redesign-design.md).
// Vanilla and CSP-clean; the sections mirror reader.js's (apps never share
// app scripts).
(function () {
	"use strict";

	// --- Confirm dialog, replacing window.confirm for hx-confirm ----------
	//
	// Mirrors reader.js's, including the #551 fix: the OK listener belongs
	// to one opening through an AbortController and goes as soon as OK or
	// Cancel is pressed — not on "close", which Chrome can hold back in a
	// hidden tab, so a declined delete would otherwise fire with the next.
	var confirmController = null;
	document.addEventListener("htmx:confirm", function (e) {
		var page = document.getElementById("books-page");
		if (!page || !e.target || !page.contains(e.target)) return;
		var msg = e.target.getAttribute("hx-confirm");
		if (!msg) return;
		var dialog = document.getElementById("books-confirm-dialog");
		if (!dialog || typeof dialog.showModal !== "function") return; // htmx falls back to window.confirm

		e.preventDefault();
		document.getElementById("books-confirm-message").textContent = msg;
		var ok = document.getElementById("books-confirm-ok");
		var cancel = dialog.querySelector(".books-dialog-cancel");

		if (confirmController) confirmController.abort();
		var controller = confirmController = new AbortController();
		dialog.addEventListener("close", function () {
			if (dialog.open) return;
			controller.abort();
		}, { signal: controller.signal });
		cancel.addEventListener("click", function () {
			controller.abort();
			dialog.close();
		}, { signal: controller.signal });
		ok.addEventListener("click", function () {
			controller.abort();
			dialog.close();
			// true: skip the confirm gate, or htmx would ask again natively.
			e.detail.issueRequest(true);
		}, { signal: controller.signal });
		dialog.showModal();
	});

	// --- Resizable sidebar -------------------------------------------------
	//
	// reader.js's gutter, for the one pane Books has left (spec "Sidebar").
	// Above the 640px drawer breakpoint only; below it the stored width is
	// cleared rather than left to misapply. The width lives on <html>, not
	// on the home, so a swap never snaps it back (#453). The sidebar is
	// swapped out of band on every list change, so it is looked up each
	// time rather than held.
	var SIDE_STORE_KEY = "books.sideWidth";
	var SIDE_MIN = 10; // rem
	var SIDE_MAX = 22; // rem
	var DESKTOP_QUERY = window.matchMedia("(min-width: 641px)");

	try {
		window.localStorage.removeItem("books.paneWidths"); // the three panes' widths, before UI1
	} catch (e) {
		// Storage blocked: nothing to tidy.
	}

	function remToPx(rem) {
		var rootPx = parseFloat(getComputedStyle(document.documentElement).fontSize) || 16;
		return rem * rootPx;
	}

	function setSideWidth(px) {
		var clamped = Math.min(remToPx(SIDE_MAX), Math.max(remToPx(SIDE_MIN), px));
		document.documentElement.style.setProperty("--books-side-w", clamped + "px");
	}

	function sideWidth() {
		var side = document.getElementById("books-side");
		return side ? side.getBoundingClientRect().width : 0;
	}

	function saveSideWidth() {
		try {
			window.localStorage.setItem(SIDE_STORE_KEY, String(Math.round(sideWidth())));
		} catch (e) {
			// Private browsing or a full quota: the drag worked, it just won't be remembered.
		}
	}

	function syncSideWidth() {
		if (!DESKTOP_QUERY.matches) {
			document.documentElement.style.removeProperty("--books-side-w");
			return;
		}
		var stored = 0;
		try {
			stored = Number(window.localStorage.getItem(SIDE_STORE_KEY)) || 0;
		} catch (e) {
			stored = 0;
		}
		if (stored > 0) setSideWidth(stored);
	}

	function initGutter() {
		var home = document.getElementById("books-home");
		var gutter = home && home.querySelector('.pane-gutter[data-gutter-for="side"]');
		if (!gutter || gutter.getAttribute("data-bound") === "1") return;
		gutter.setAttribute("data-bound", "1");
		var dragging = null; // { startX, startWidth }

		gutter.addEventListener("pointerdown", function (e) {
			if (!DESKTOP_QUERY.matches) return;
			dragging = { startX: e.clientX, startWidth: sideWidth() };
			gutter.classList.add("is-dragging");
			gutter.setPointerCapture(e.pointerId);
			e.preventDefault();
		});
		gutter.addEventListener("pointermove", function (e) {
			if (!dragging) return;
			setSideWidth(dragging.startWidth + (e.clientX - dragging.startX));
		});
		function endDrag() {
			if (!dragging) return;
			dragging = null;
			gutter.classList.remove("is-dragging");
			saveSideWidth();
		}
		gutter.addEventListener("pointerup", endDrag);
		gutter.addEventListener("pointercancel", endDrag);
		// The WAI-ARIA separator pattern: arrows nudge the pane.
		gutter.addEventListener("keydown", function (e) {
			if (!DESKTOP_QUERY.matches) return;
			if (e.key !== "ArrowLeft" && e.key !== "ArrowRight") return;
			setSideWidth(sideWidth() + (e.key === "ArrowRight" ? 16 : -16));
			saveSideWidth();
			e.preventDefault();
		});
	}

	document.addEventListener("DOMContentLoaded", function () {
		syncSideWidth();
		DESKTOP_QUERY.addEventListener("change", syncSideWidth);
		initGutter();
	});
	// Back/forward restores <body> from htmx's history cache: a new gutter,
	// no DOMContentLoaded (#456).
	document.addEventListener("htmx:historyRestore", initGutter);

	// --- Focus in the sort and view controls ---------------------------------
	//
	// Choosing a sort or a view swaps the whole controls form out of band
	// (it carries the pressed state and the list's context), which would
	// drop focus to <body>. Put it back on the control that had it.
	var keptFocus = null;
	document.addEventListener("htmx:beforeSwap", function () {
		var el = document.activeElement;
		keptFocus = el && el.id && el.closest && el.closest("#books-prefs") ? el.id : null;
	});
	document.addEventListener("htmx:afterSettle", function () {
		if (!keptFocus) return;
		var el = document.getElementById(keptFocus);
		keptFocus = null;
		if (el && !el.disabled) el.focus();
	});

	// The progress form swaps its whole box (input included), so focus would
	// drop to <body> after Enter. Put it back on the new input, selected, so
	// the next number replaces the old one — on success and on the inline
	// error alike. Only for swaps the progress form itself made.
	document.addEventListener("htmx:afterSwap", function (e) {
		var src = (e.detail && (e.detail.requestConfig && e.detail.requestConfig.elt || e.detail.elt)) || e.target;
		if (!src || !src.classList || !src.classList.contains("books-progress-form")) return;
		var input = document.getElementById("books-progress-input");
		if (!input) return;
		input.focus();
		input.select();
	});

	// --- Keyboard shortcuts --------------------------------------------------
	//
	// Home: j/k move through the books, Enter (natively) or o opens one,
	// / searches, a adds a book, Esc closes the drawer. Book page: p goes to
	// the progress box, n and q open the add-note and add-quote forms, e
	// edits, Esc closes an open disclosure — or, with none open, goes back
	// to the list (spec "Keyboard").

	// openEntry opens an add disclosure ("+ Add note", "+ Add quote") and
	// puts the cursor in its box; false when the page has none.
	function openEntry(detailsId, inputId) {
		var details = document.getElementById(detailsId);
		var input = document.getElementById(inputId);
		if (!details || !input) return false;
		details.open = true;
		input.focus();
		return true;
	}

	// Keys are ignored while typing, so "/" in the search box is a slash.
	function isTyping(el) {
		if (!el) return false;
		var tag = el.tagName;
		return tag === "INPUT" || tag === "TEXTAREA" || tag === "SELECT" || el.isContentEditable;
	}

	function itemLinks() {
		return Array.prototype.slice.call(document.querySelectorAll(".books-item-link"));
	}

	function step(delta) {
		var links = itemLinks();
		if (!links.length) return;
		var i = links.indexOf(document.activeElement);
		var next = i === -1 ? 0 : Math.min(links.length - 1, Math.max(0, i + delta));
		links[next].focus();
		links[next].scrollIntoView({ block: "nearest" });
	}

	// closeDisclosures closes every open <details> on the book page; false
	// when none was open.
	function closeDisclosures() {
		var open = document.querySelectorAll("#books-book details[open]");
		open.forEach(function (d) {
			d.open = false;
		});
		return open.length > 0;
	}

	function homeKey(e) {
		switch (e.key) {
		case "j":
			step(1);
			return true;
		case "k":
			step(-1);
			return true;
		case "o":
			if (itemLinks().indexOf(document.activeElement) === -1) return false;
			document.activeElement.click();
			return true;
		case "/":
			var q = document.getElementById("books-q");
			if (!q) return false;
			q.focus();
			q.select();
			return true;
		case "a":
			window.location.href = "/books/new";
			return true;
		case "Escape":
			var drawer = document.getElementById("books-drawer");
			if (!drawer || !drawer.checked) return false;
			drawer.checked = false;
			return true;
		}
		return false;
	}

	function bookKey(e) {
		switch (e.key) {
		case "p":
			var progress = document.getElementById("books-progress-input");
			if (!progress) return false;
			progress.focus();
			progress.select();
			return true;
		case "n":
			return openEntry("books-note-new", "books-note-new-body");
		case "q":
			return openEntry("books-quote-new", "books-quote-new-text");
		case "e":
			var edit = document.querySelector(".books-edit-link");
			if (!edit) return false;
			window.location.href = edit.href;
			return true;
		case "Escape":
			if (closeDisclosures()) return true;
			var back = document.querySelector(".books-back");
			if (!back) return false;
			window.location.href = back.href;
			return true;
		}
		return false;
	}

	document.addEventListener("keydown", function (e) {
		if (e.defaultPrevented || e.repeat || e.altKey || e.ctrlKey || e.metaKey) return;
		// The modal confirm owns the keyboard (Escape still closes it natively).
		if (document.querySelector("dialog[open]")) return;
		var home = document.getElementById("books-home");
		var page = document.getElementById("books-page");
		if (!home && !page) return;
		if (isTyping(e.target)) {
			if (e.key !== "Escape") return;
			if (e.target.id === "books-q" || e.target.id === "books-progress-input") {
				e.target.blur();
				return;
			}
			// Esc from a field in an open disclosure of the book page
			// (Finish, Did not finish, an edit form) closes it and returns
			// focus to its summary.
			var open = e.target.closest("#books-book details[open]");
			if (open) {
				open.open = false;
				var summary = open.querySelector("summary");
				if (summary) summary.focus();
			}
			return;
		}
		if (home ? homeKey(e) : bookKey(e)) e.preventDefault();
	});
})();
```

- [ ] **Step 2: Check it in the browser**

Rebuild and restart the preview (`go build -o /tmp/onsuite-bin ./cmd/onsuite`, then restart the `onsuite` preview server). On `/books/`:

- **j/k:** pressing `j` three times focuses the third tile, `k` goes back, and `o` or Enter opens it.
- **`/`, Esc and `a`:** `/` focuses search, Esc leaves it, and `a` opens Add book.
- **Sort select:** changing it reorders the books, and keyboard focus stays on the select.
- **↓ button:** clicking it flips the order, turns the icon to ↑, and keeps focus on the button.
- **Sidebar gutter:** dragging it resizes the sidebar, and the width survives a reload.

On a book page:

- `p` focuses the progress box, `n` and `q` open the add forms, and `e` goes to Edit.
- Esc closes an open form; a second Esc goes back to the list.
- 🗑 opens the app's confirm dialog. Cancel keeps the book, and Delete lands on the list it came from.

Check `read_console_messages` for errors: there should be none.

- [ ] **Step 3: Run the full check, then commit**

Run: `gofmt -l . && go vet ./... && go run honnef.co/go/tools/cmd/staticcheck@v0.8.0 ./... && go test ./... -race -count=1`
Expected: clean.

```bash
git add internal/apps/books/static/books.js
git commit -m "feat(books): keyboard, sidebar gutter and confirm for the two-pane home and book page"
```

---

### Task 9: Final pass

- [ ] **Step 1: Read the diff with the spec at hand**

Run: `git diff main --stat` and read the whole diff. Against the spec's UI1 row, check that each of these is in place:

- `books_prefs` and `POST /books/prefs`
- the two-pane home: toolbar, grid/list, sort, search shows the list, phone drawer
- `/books/b/{id}` as a full page with the page bar
- nothing left of the three panes

Also check the spec's "Home page" bullets: the goal card is in the sidebar, Stats, Import and Export are at its foot, and Add book is in the toolbar.

- [ ] **Step 2: Run the full check**

```bash
gofmt -l .
go vet ./...
go run honnef.co/go/tools/cmd/staticcheck@v0.8.0 ./...
go mod tidy && git diff --exit-code go.mod go.sum
go test ./... -race -count=1
```

Expected: no gofmt output, and everything else ok.

- [ ] **Step 3: Take screenshots for the PR**

With the seeded preview from Task 7, take screenshots of:

- the grid home (desktop)
- the list home
- a search with a snippet
- the phone home with the drawer open
- a book page

Attach them to the PR description. The user guide and `docs/user/images` are left to UI2, as the spec's Phases table says, so don't commit any screenshots.

- [ ] **Step 4: Hand over**

Use `superpowers:finishing-a-development-branch`. The PR title is `feat(books): two-pane home and a book page of its own (UI1)`. The body summarises the tasks and links the spec and this plan. It should also note two things:

- Find cover moved to the Edit page in UI1 rather than UI2, because the ⋯ menu was already gone.
- The keyboard shortcuts were rewritten in UI1 because the layout they drove changed.

Don't merge: Ilia merges.
