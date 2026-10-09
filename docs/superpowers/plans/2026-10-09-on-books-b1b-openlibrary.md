# ON Books B1b — Open Library search and covers Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let people find a book on Open Library from the Add book page and add it pre-filled with its cover; show stored covers in the list and the book pane; let people upload a cover, take one from an image address, or remove it.

**Architecture:** One `webfetch.Client` per app (SSRF guard, caps) behind a small exported `OpenLibrary` client (`openlibrary.go`: search, work description, cover by id). Covers live as BLOBs in `books_covers` (`cover.go` in the store) and are served same-origin from `/books/cover/{id}?v=…`. Search-result thumbnails are proxied through `/books/olcover/{coverID}` so the suite's `img-src 'self'` CSP stays as it is. Search lives on the existing Add book page (`/books/new?q=…`) as a plain GET form; "Use this" reloads the page pre-filled from the result. Everything works without JavaScript; no new script.

**Tech Stack:** Go 1.22+ `ServeMux`, `html/template`, `internal/platform/webfetch`, `encoding/json`, SQLite via `modernc.org/sqlite`.

**Spec:** [docs/superpowers/specs/2026-10-09-on-books-design.md](../specs/2026-10-09-on-books-design.md) — "Architecture" (Open Library client), "Data model" (`books_covers`), "Screens → Add book", "Errors", "Testing", "Phases" (B1b row). Builds on B1a ([plan](2026-10-09-on-books-b1a-library.md)), merged as #570.

## Global Constraints

- All Open Library traffic goes through `internal/platform/webfetch` (SSRF guard at dial time, redirect and size caps), User-Agent `onsuite/<version> (ON Books; +https://github.com/iliafrenkel/on-suite)`.
- Open Library endpoints: search `https://openlibrary.org/search.json?q=…&fields=key,title,subtitle,author_name,first_publish_year,number_of_pages_median,isbn,cover_i,cover_edition_key&limit=10`; work `https://openlibrary.org/works/<OL…W>.json` (`description` is a string or `{"type":…,"value":…}`); cover `https://covers.openlibrary.org/b/id/<cover id>-<S|M>.jpg` (usually redirects twice, to `archive.org` then `iaNNNN.us.archive.org` — webfetch follows them server-side).
- Timeouts: search and description 5 s; any image fetch (OL cover, thumbnail, pasted address) 10 s. Search returns at most 10 results. Saved OL covers are size `M`, thumbnails size `S`.
- Covers: at most 2 MiB (`MaxCoverBytes = 2 << 20`); accepted types, by sniffing the bytes (`http.DetectContentType`), never by the declared type: `image/jpeg`, `image/png`, `image/gif`, `image/webp`. One cover per book; `source` is `ol`, `upload` or `url`. A cover goes with its book (ON DELETE CASCADE).
- The CSP is **not** changed: `img-src 'self'` stays suite-wide. Thumbnails are proxied (decided with Ilia on 2026-10-09; the spec's "CSP gets that one image origin" line is replaced in Task 7).
- Search lives on the Add book page, not in a dialog (decided with Ilia on 2026-10-09). No new JavaScript.
- Open Library ids are kept only if they look like OL keys: work `^OL[1-9][0-9]{0,11}W$`, edition `^OL[1-9][0-9]{0,11}M$`. Cover ids are 1–12 digits.
- Failures: a failed search shows "Open Library didn't answer. Try again, or fill in the book yourself below." and pre-fills the form with what was typed (as ISBN when it is a valid ISBN, otherwise as the title). A failed cover fetch on save is logged and the book is saved without a cover (it shows its spine). A failed description fetch leaves the description empty. A bad upload or unreachable image address is a 422 on the edit form with a message by the Cover field, and nothing is saved.
- Cover URLs: `/books/cover/{id}?v=<version>` with `Cache-Control: private, max-age=31536000, immutable`, `ETag: "<version>"`, `X-Content-Type-Options: nosniff`, 304 on a matching `If-None-Match`. Thumbnails: `/books/olcover/{coverID}` with `Cache-Control: private, max-age=86400`, `nosniff`; at most 4 thumbnail fetches at once.
- Missing or someone else's book or cover → 404, as everywhere.
- CSP: no inline `<script>`, no `style=""`. App CSS in the "ON Books" section at the end of `internal/ui/static/app.css`, classes prefixed `books-`.
- Full check must stay green on every commit:
  ```bash
  gofmt -l .
  go vet ./...
  go run honnef.co/go/tools/cmd/staticcheck@v0.8.0 ./...
  go mod tidy && git diff --exit-code go.mod go.sum
  go test ./... -race -count=1
  ```
- Work on branch `feat/books-b1b-openlibrary` in a worktree, never on `main`. Open the PR with `env -u GH_TOKEN gh …`; never merge.

## Lessons from earlier plans (read before starting)

- staticcheck U1000 fails on an unexported helper added before its first caller — every helper is introduced in the task that first uses it. staticcheck S1016 wants a type conversion instead of a struct literal copying identical fields.
- `internal/htmlassert` supports one qualifier per selector (`img.books-cover` fine, `img.books-cover[src]` not); descendant selectors work; use `htmlassert.Attr` for a second condition.
- `go vet` rejects unkeyed composite literals of another package's struct types in `_test` packages.
- Tests reach a loopback httptest server only through the app's test hook (the `export_test.go` idiom Reader and Later use); one test must exercise the real guard.
- `TestMain` pins `time.Local` to Melbourne for this package.
- This plan's code was trial-run in a scratch worktree on 2026-10-09 (Tasks 1–7 applied as written): each task's tests, vet and staticcheck green at its own commit, the full check green at the end, and the search → pick → add → cover flow checked against the real Open Library in a browser. That run is where `plainDescription` came from: Open Library descriptions are Markdown.

## File map

| File | Responsibility |
|---|---|
| `internal/apps/books/migrations/0002_covers.sql` | `books_covers` |
| `internal/apps/books/cover.go` | `Cover`, cover sources, `MaxCoverBytes`, `SetCover`, `Cover`, `RemoveCover`, `coverVersion`, `coverType` |
| `internal/apps/books/library.go` | OL ids on `NewBook`, `olID`; cover version on `Book`/`ListItem` |
| `internal/apps/books/openlibrary.go` | `OpenLibrary`, `Candidate`, `Search`, `Description`, `Cover` |
| `internal/apps/books/covers.go` | HTTP: serve a cover, proxy a thumbnail, fetch a cover for a book or address |
| `internal/apps/books/export_test.go` | `UseOpenLibraryForTest` (test-only hook) |
| `internal/apps/books/books.go` | client, semaphore, routes, body limit |
| `internal/apps/books/form.go`, `templates/form.html` | search, pick, cover on save, cover on edit |
| `internal/apps/books/view.go`, `templates/panes.partial.html` | covers in rows and the book pane |
| `internal/ui/static/app.css` | search results, covers |
| `internal/apps/books/*_test.go` | tests |
| `docs/user/books.md`, the spec | guide, decisions |

---

### Task 0: Worktree and branch

- [ ] **Step 1: Create the worktree**

```bash
cd /Users/iliaf/src/WEB/on-suite
git fetch origin
git worktree add ../on-suite-books-b1b -b feat/books-b1b-openlibrary origin/main
cd ../on-suite-books-b1b
go build ./cmd/onsuite && rm -f onsuite
```
Expected: builds with no output. All later commands run in `../on-suite-books-b1b`.

---

### Task 1: Covers and Open Library ids in the store

**Files:**
- Create: `internal/apps/books/migrations/0002_covers.sql`, `internal/apps/books/cover.go`
- Modify: `internal/apps/books/library.go` (`NewBook`, `Create`, `Book`, `Get`, `ListItem`, `List`)
- Test: `internal/apps/books/cover_test.go`

**Interfaces:**
- Consumes: `touch`, `formatTime`, `parseTime`, `ErrNotFound`, `ErrInvalid` (store.go); test helpers `newFixture`, `addBook`, `onShelf`, `getBook`.
- Produces:
  - `const MaxCoverBytes = 2 << 20`; `const CoverFromOL = "ol"`, `CoverUpload = "upload"`, `CoverFromURL = "url"`
  - `type Cover struct { ContentType string; Bytes []byte; Version string }`
  - `(*Store).SetCover(ctx, userID, id int64, contentType string, data []byte, source string) error`, `Cover(ctx, userID, id int64) (Cover, error)`, `RemoveCover(ctx, userID, id int64) error`
  - `Book.CoverVersion string`, `ListItem.CoverVersion string` ("" = no cover)
  - `NewBook.OLWorkID`, `NewBook.OLEditionID string`
  - unexported for later tasks: `olID(s string, kind byte) string`, `coverVersion(fetchedAt string) string` (`coverType` comes in Task 3, with its first caller)

- [ ] **Step 1: Write the failing tests**

`internal/apps/books/cover_test.go`:

```go
package books_test

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/iliafrenkel/on-suite/internal/apps/books"
)

// onePNG is the smallest thing http.DetectContentType calls an image/png
// (the same bytes Reader's image tests use).
var onePNG = []byte{
	0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A,
	0x00, 0x00, 0x00, 0x0D, 0x49, 0x48, 0x44, 0x52,
}

func TestSetCoverAndReadItBack(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	id := addBook(t, f, f.alice.ID, onShelf("Piranesi", books.ShelfWant))
	if b := getBook(t, f, f.alice.ID, id); b.CoverVersion != "" {
		t.Fatalf("new book has cover version %q, want none", b.CoverVersion)
	}
	if err := f.store.SetCover(ctx, f.alice.ID, id, "image/png", onePNG, books.CoverUpload); err != nil {
		t.Fatal(err)
	}
	c, err := f.store.Cover(ctx, f.alice.ID, id)
	if err != nil {
		t.Fatal(err)
	}
	if c.ContentType != "image/png" || !bytes.Equal(c.Bytes, onePNG) || c.Version == "" {
		t.Errorf("Cover = %q, %d bytes, version %q", c.ContentType, len(c.Bytes), c.Version)
	}
	if b := getBook(t, f, f.alice.ID, id); b.CoverVersion != c.Version {
		t.Errorf("Book.CoverVersion = %q, want %q", b.CoverVersion, c.Version)
	}
	items, err := f.store.List(ctx, f.alice.ID, books.ListQuery{Shelf: books.ShelfAll})
	if err != nil || len(items) != 1 || items[0].CoverVersion != c.Version {
		t.Errorf("List = %+v, %v; want the cover version on the row", items, err)
	}
}

func TestReplacingACoverChangesItsVersion(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	id := addBook(t, f, f.alice.ID, onShelf("Piranesi", books.ShelfWant))
	if err := f.store.SetCover(ctx, f.alice.ID, id, "image/png", onePNG, books.CoverFromOL); err != nil {
		t.Fatal(err)
	}
	first, _ := f.store.Cover(ctx, f.alice.ID, id)
	f.now = f.now.Add(time.Second)
	if err := f.store.SetCover(ctx, f.alice.ID, id, "image/gif", []byte("GIF89a…"), books.CoverFromURL); err != nil {
		t.Fatal(err)
	}
	second, _ := f.store.Cover(ctx, f.alice.ID, id)
	if second.ContentType != "image/gif" || second.Version == first.Version {
		t.Errorf("after replacing: %q version %q (was %q)", second.ContentType, second.Version, first.Version)
	}
}

func TestCoversAreScopedToTheOwner(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	id := addBook(t, f, f.alice.ID, onShelf("Piranesi", books.ShelfWant))
	if _, err := f.store.Cover(ctx, f.alice.ID, id); !errors.Is(err, books.ErrNotFound) {
		t.Errorf("Cover of a book without one = %v, want ErrNotFound", err)
	}
	if err := f.store.SetCover(ctx, f.bob.ID, id, "image/png", onePNG, books.CoverUpload); !errors.Is(err, books.ErrNotFound) {
		t.Errorf("Bob's SetCover = %v, want ErrNotFound", err)
	}
	if err := f.store.SetCover(ctx, f.alice.ID, id, "image/png", onePNG, books.CoverUpload); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.Cover(ctx, f.bob.ID, id); !errors.Is(err, books.ErrNotFound) {
		t.Errorf("Bob's Cover = %v, want ErrNotFound", err)
	}
	if err := f.store.RemoveCover(ctx, f.bob.ID, id); !errors.Is(err, books.ErrNotFound) {
		t.Errorf("Bob's RemoveCover = %v, want ErrNotFound", err)
	}
}

func TestSetCoverRejectsAnUnknownSource(t *testing.T) {
	f := newFixture(t)
	id := addBook(t, f, f.alice.ID, onShelf("Piranesi", books.ShelfWant))
	if err := f.store.SetCover(context.Background(), f.alice.ID, id, "image/png", onePNG, "web"); !errors.Is(err, books.ErrInvalid) {
		t.Errorf("SetCover with source web = %v, want ErrInvalid", err)
	}
}

func TestRemoveCover(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	id := addBook(t, f, f.alice.ID, onShelf("Piranesi", books.ShelfWant))
	if err := f.store.SetCover(ctx, f.alice.ID, id, "image/png", onePNG, books.CoverUpload); err != nil {
		t.Fatal(err)
	}
	if err := f.store.RemoveCover(ctx, f.alice.ID, id); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.Cover(ctx, f.alice.ID, id); !errors.Is(err, books.ErrNotFound) {
		t.Errorf("Cover after remove = %v, want ErrNotFound", err)
	}
	if b := getBook(t, f, f.alice.ID, id); b.CoverVersion != "" {
		t.Errorf("CoverVersion after remove = %q", b.CoverVersion)
	}
}

func TestDeletingABookTakesItsCover(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	id := addBook(t, f, f.alice.ID, onShelf("Piranesi", books.ShelfWant))
	if err := f.store.SetCover(ctx, f.alice.ID, id, "image/png", onePNG, books.CoverUpload); err != nil {
		t.Fatal(err)
	}
	if err := f.store.Delete(ctx, f.alice.ID, id); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := f.db.QueryRow(`SELECT count(*) FROM books_covers`).Scan(&n); err != nil || n != 0 {
		t.Errorf("covers left = %d, %v; want 0", n, err)
	}
}

func TestCreateKeepsOnlyRealOpenLibraryIDs(t *testing.T) {
	f := newFixture(t)
	good := onShelf("Piranesi", books.ShelfWant)
	good.OLWorkID, good.OLEditionID = "OL20893680W", "OL28300471M"
	bad := onShelf("Other", books.ShelfWant)
	bad.OLWorkID, bad.OLEditionID = "OL28300471M", "/books/x" // an edition as the work, and junk
	for _, tt := range []struct {
		nb         books.NewBook
		work, edit string
	}{{good, "OL20893680W", "OL28300471M"}, {bad, "", ""}} {
		id := addBook(t, f, f.alice.ID, tt.nb)
		var work, edit string
		if err := f.db.QueryRow(`SELECT ol_work_id, ol_edition_id FROM books_books WHERE id = ?`, id).Scan(&work, &edit); err != nil {
			t.Fatal(err)
		}
		if work != tt.work || edit != tt.edit {
			t.Errorf("%s: stored %q / %q, want %q / %q", tt.nb.Title, work, edit, tt.work, tt.edit)
		}
	}
}
```

- [ ] **Step 2: Run them to see them fail**

Run: `go test ./internal/apps/books/... -count=1`
Expected: FAIL — `undefined: books.CoverUpload` and friends.

- [ ] **Step 3: Write the migration**

`internal/apps/books/migrations/0002_covers.sql`:

```sql
-- One cover per book, kept in the database like every other app's images
-- (spec "Data model"): fetched once from Open Library when a book is added
-- from a search result (source 'ol'), uploaded ('upload'), or fetched from
-- an image address someone pasted ('url'). fetched_at doubles as the
-- version in the cover's URL, so a new cover is a new URL.
CREATE TABLE books_covers (
    book_id      INTEGER PRIMARY KEY REFERENCES books_books (id) ON DELETE CASCADE,
    content_type TEXT    NOT NULL,
    bytes        BLOB    NOT NULL,
    source       TEXT    NOT NULL CHECK (source IN ('ol', 'upload', 'url')),
    fetched_at   TEXT    NOT NULL
) STRICT;
```

- [ ] **Step 4: Write `cover.go`**

```go
package books

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
)

// MaxCoverBytes bounds a cover, however it arrives: a book cover is a
// small picture, and an image this size is already far more than the
// pane draws.
const MaxCoverBytes = 2 << 20

// Where a cover came from (books_covers.source).
const (
	CoverFromOL  = "ol"
	CoverUpload  = "upload"
	CoverFromURL = "url"
)

// Cover is a stored cover. Version changes whenever the cover does.
type Cover struct {
	ContentType string
	Bytes       []byte
	Version     string
}

// coverVersion turns a cover's fetched_at into the short token its URL
// carries (?v=…). "" means no cover.
func coverVersion(fetchedAt string) string {
	if fetchedAt == "" {
		return ""
	}
	t, err := parseTime(fetchedAt)
	if err != nil {
		return ""
	}
	return strconv.FormatInt(t.UnixNano(), 36)
}

// SetCover stores (or replaces) one of userID's books' cover. The caller
// has already checked the bytes are a cover type and size.
func (st *Store) SetCover(ctx context.Context, userID, id int64, contentType string, data []byte, source string) error {
	switch source {
	case CoverFromOL, CoverUpload, CoverFromURL:
	default:
		return ErrInvalid
	}
	tx, err := st.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("books: begin set cover: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if err := st.touch(ctx, tx, userID, id); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO books_covers (book_id, content_type, bytes, source, fetched_at)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT (book_id) DO UPDATE SET content_type = excluded.content_type,
			bytes = excluded.bytes, source = excluded.source, fetched_at = excluded.fetched_at`,
		id, contentType, data, source, formatTime(st.now())); err != nil {
		return fmt.Errorf("books: set cover: %w", err)
	}
	return tx.Commit()
}

// Cover returns one of userID's books' cover; ErrNotFound when the book
// has none, or isn't theirs.
func (st *Store) Cover(ctx context.Context, userID, id int64) (Cover, error) {
	var c Cover
	var fetched string
	err := st.db.QueryRowContext(ctx, `
		SELECT c.content_type, c.bytes, c.fetched_at
		  FROM books_covers c JOIN books_books b ON b.id = c.book_id
		 WHERE b.id = ? AND b.user_id = ?`, id, userID).Scan(&c.ContentType, &c.Bytes, &fetched)
	if errors.Is(err, sql.ErrNoRows) {
		return Cover{}, ErrNotFound
	}
	if err != nil {
		return Cover{}, fmt.Errorf("books: cover: %w", err)
	}
	c.Version = coverVersion(fetched)
	return c, nil
}

// RemoveCover drops a book's cover, so it shows its spine again.
func (st *Store) RemoveCover(ctx context.Context, userID, id int64) error {
	tx, err := st.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("books: begin remove cover: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if err := st.touch(ctx, tx, userID, id); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM books_covers WHERE book_id = ?`, id); err != nil {
		return fmt.Errorf("books: remove cover: %w", err)
	}
	return tx.Commit()
}
```

- [ ] **Step 5: Teach `library.go` about OL ids and covers**

Add `"regexp"` to the imports.

In `NewBook`, after `Tags`, add:

```go
	// OLWorkID and OLEditionID come from an Open Library pick; Create keeps
	// them only if they look like Open Library keys.
	OLWorkID, OLEditionID string
```

After the `nullText` function, add:

```go
// olIDPattern is an Open Library key: "OL", a number, and W (work) or M
// (edition).
var olIDPattern = regexp.MustCompile(`^OL[1-9][0-9]{0,11}[WM]$`)

// olID returns s if it is an Open Library key of the given kind ('W' or
// 'M'), otherwise "".
func olID(s string, kind byte) string {
	if olIDPattern.MatchString(s) && s[len(s)-1] == kind {
		return s
	}
	return ""
}
```

In `Create`, replace the `INSERT INTO books_books` statement with:

```go
	res, err := tx.ExecContext(ctx, `
		INSERT INTO books_books (user_id, title, subtitle, authors, year, pages, isbn13,
			ol_work_id, ol_edition_id, series_name, series_number, description, added_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		userID, in.Title, in.Subtitle, in.Authors, nullInt(in.Year), nullInt(in.Pages), nullText(in.ISBN),
		olID(nb.OLWorkID, 'W'), olID(nb.OLEditionID, 'M'),
		in.SeriesName, in.SeriesNumber, in.Description, now, now)
```

In `Book`, after `Latest`, add:

```go
	CoverVersion       string  // "" when the book has no cover
```
(gofmt aligns the block.)

In `Get`, declare `var cover sql.NullString` with the other `sql.Null*` variables, and replace the query and its `Scan` with:

```go
	err := st.db.QueryRowContext(ctx, `
		SELECT b.id, b.title, b.subtitle, b.authors, b.year, b.pages, b.isbn13, b.series_name,
		       b.series_number, b.description, b.rating, b.review, b.added_at, b.updated_at,
		       r.id, r.status, r.format, r.started_on, r.finished_on, `+shelfExpr+`, c.fetched_at
		  FROM books_books b `+latestJoin+`
		  LEFT JOIN books_covers c ON c.book_id = b.id
		 WHERE b.id = ? AND b.user_id = ?`, id, userID).Scan(
		&b.ID, &b.Title, &b.Subtitle, &b.Authors, &year, &pages, &isbn, &b.SeriesName,
		&b.SeriesNumber, &b.Description, &rating, &b.Review, &added, &updated,
		&rid, &status, &format, &started, &finished, &shelf, &cover)
```
and after `b.Shelf = Shelf(shelf)` add `b.CoverVersion = coverVersion(cover.String)`.

In `ListItem`, add a last field:

```go
	CoverVersion                             string // "" when the book has no cover
```

In `List`, replace the query's first four lines and the scan:

```go
	rows, err := st.db.QueryContext(ctx, `
		SELECT b.id, b.title, b.authors, b.series_name, b.series_number, b.added_at,
		       r.started_on, r.finished_on, `+shelfExpr+`, c.fetched_at
		  FROM books_books b `+latestJoin+`
		  LEFT JOIN books_covers c ON c.book_id = b.id
		 WHERE `+strings.Join(where, " AND ")+`
		 ORDER BY `+listOrder(q.Shelf), args...)
```
```go
		var started, finished, cover sql.NullString
		if err := rows.Scan(&it.ID, &it.Title, &it.Authors, &it.SeriesName, &it.SeriesNumber, &added,
			&started, &finished, &shelf, &cover); err != nil {
			return nil, fmt.Errorf("books: scan list: %w", err)
		}
```
and set `it.CoverVersion = coverVersion(cover.String)` next to the other assignments.

- [ ] **Step 6: Run the tests, then the full check**

Run: `go test ./internal/apps/books/... -count=1`
Expected: PASS. Then the full check from Global Constraints.

- [ ] **Step 7: Commit**

```bash
git add internal/apps/books
git commit -m "feat(books): store covers and Open Library ids (#487)"
```

---

### Task 2: The Open Library client

**Files:**
- Create: `internal/apps/books/openlibrary.go`
- Test: `internal/apps/books/openlibrary_test.go`

**Interfaces:**
- Consumes: `olID`, `oneLine`, `ISBN13`, `MaxYear`, `MaxPages`, `MaxDescriptionRunes`, `MaxCoverBytes`, `ErrInvalid` (earlier tasks); `webfetch.Client.Get`, `GetImage`.
- Produces:
  - `type OpenLibrary struct { Web *webfetch.Client; Base, Covers string; Timeout time.Duration }`
  - `const MaxCandidates = 10`
  - `type Candidate struct { WorkID, EditionID, Title, Subtitle, Authors string; Year, Pages int; ISBN string; CoverID int64 }`
  - `(*OpenLibrary).Search(ctx, q string) ([]Candidate, error)`, `Description(ctx, workID string) (string, error)`, `Cover(ctx, coverID int64, size string) (contentType string, data []byte, err error)`
  - test helpers (in `openlibrary_test.go`, reused later): `fakeOpenLibrary(t) *httptest.Server` serving `/search.json`, `/works/OL20893680W.json`, `/works/OL27448W.json`, `/b/id/10226290-S.jpg`, `/b/id/10226290-M.jpg`, `/b/id/666-M.jpg` (HTML); `testWebClient() *webfetch.Client` (loopback allowed)

- [ ] **Step 1: Write the failing tests**

`internal/apps/books/openlibrary_test.go`:

```go
package books_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/iliafrenkel/on-suite/internal/apps/books"
	"github.com/iliafrenkel/on-suite/internal/platform/webfetch"
)

// searchJSON is a trimmed real search.json answer for "piranesi", plus a
// doc with no title (skipped) and one with only ISBN-10s and an absurd
// year (kept, cleaned).
const searchJSON = `{"numFound":3,"docs":[
 {"author_name":["Susanna Clarke"],"cover_edition_key":"OL28300471M","cover_i":10226290,
  "first_publish_year":2020,"isbn":["1635575648","9781526622440","9781635575637"],
  "key":"/works/OL20893680W","number_of_pages_median":272,"title":"Piranesi"},
 {"key":"/works/OL1W","title":"  "},
 {"author_name":["A. Writer","B. Writer"],"key":"/works/OL2W","title":"Old  Book",
  "subtitle":"A Story","first_publish_year":-5,"isbn":["0306406152"]}
]}`

// fakeOpenLibrary stands in for both openlibrary.org and
// covers.openlibrary.org. "broken" fails, "slow" answers late, anything
// else unknown finds nothing.
func fakeOpenLibrary(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /search.json", func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Query().Get("q") {
		case "broken":
			http.Error(w, "down", http.StatusServiceUnavailable)
		case "garbled":
			_, _ = w.Write([]byte(`{"docs":[`))
		case "slow":
			time.Sleep(300 * time.Millisecond)
			_, _ = w.Write([]byte(`{"docs":[]}`))
		case "piranesi":
			_, _ = w.Write([]byte(searchJSON))
		default:
			_, _ = w.Write([]byte(`{"docs":[]}`))
		}
	})
	mux.HandleFunc("GET /works/OL20893680W.json", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"title":"Piranesi","description":"Piranesi's house is no ordinary building.\r\nIts rooms are infinite."}`))
	})
	mux.HandleFunc("GET /works/OL27448W.json", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"title":"The Lord of the Rings","description":{"type":"/type/text","value":"An epic."}}`))
	})
	mux.HandleFunc("GET /works/OL3W.json", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"description":"**From the *New York Times* bestselling author.**\r\n\r\nFor fans of *Circe*.  \r\n\r\n----------\r\nAlso contained in:\r\n[The Collection](/works/OL4W)\r\n\r\n([source][1])\r\n\r\n  [1]: https://example.com/piranesi"}`))
	})
	png := func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg") // a lie the sniffing must see through
		_, _ = w.Write(onePNG)
	}
	mux.HandleFunc("GET /b/id/10226290-S.jpg", png)
	mux.HandleFunc("GET /b/id/10226290-M.jpg", png)
	mux.HandleFunc("GET /b/id/666-M.jpg", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write([]byte("<html><body>not an image</body></html>"))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

// testWebClient is a webfetch client allowed to reach httptest's loopback.
func testWebClient() *webfetch.Client {
	c := webfetch.New(webfetch.Config{UserAgent: "test"})
	c.DenyAddr = func(string) error { return nil }
	return c
}

func newOpenLibrary(t *testing.T) *books.OpenLibrary {
	t.Helper()
	srv := fakeOpenLibrary(t)
	return &books.OpenLibrary{Web: testWebClient(), Base: srv.URL, Covers: srv.URL, Timeout: 5 * time.Second}
}

func TestSearchMapsResults(t *testing.T) {
	ol := newOpenLibrary(t)
	got, err := ol.Search(context.Background(), " piranesi ")
	if err != nil {
		t.Fatal(err)
	}
	want := []books.Candidate{
		{WorkID: "OL20893680W", EditionID: "OL28300471M", Title: "Piranesi", Authors: "Susanna Clarke",
			Year: 2020, Pages: 272, ISBN: "9781526622440", CoverID: 10226290},
		{WorkID: "OL2W", Title: "Old Book", Subtitle: "A Story", Authors: "A. Writer, B. Writer",
			ISBN: "9780306406157"},
	}
	if len(got) != len(want) {
		t.Fatalf("Search = %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("result %d = %+v\nwant %+v", i, got[i], want[i])
		}
	}
}

func TestSearchAsksForTheRightThings(t *testing.T) {
	var gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		_, _ = w.Write([]byte(`{"docs":[]}`))
	}))
	defer srv.Close()
	ol := &books.OpenLibrary{Web: testWebClient(), Base: srv.URL, Covers: srv.URL, Timeout: time.Second}
	if _, err := ol.Search(context.Background(), "le guin"); err != nil {
		t.Fatal(err)
	}
	for _, part := range []string{"q=le+guin", "limit=10",
		"fields=key%2Ctitle%2Csubtitle%2Cauthor_name%2Cfirst_publish_year%2Cnumber_of_pages_median%2Cisbn%2Ccover_i%2Ccover_edition_key"} {
		if !strings.Contains(gotQuery, part) {
			t.Errorf("query %q lacks %q", gotQuery, part)
		}
	}
}

func TestSearchWithNothingTypedAsksNothing(t *testing.T) {
	ol := &books.OpenLibrary{Web: testWebClient(), Base: "http://127.0.0.1:1", Timeout: time.Second}
	if got, err := ol.Search(context.Background(), "   "); err != nil || got != nil {
		t.Errorf("Search(blank) = %v, %v; want nothing and no error", got, err)
	}
}

func TestSearchFailures(t *testing.T) {
	ol := newOpenLibrary(t)
	ol.Timeout = 100 * time.Millisecond
	for _, q := range []string{"broken", "garbled", "slow"} {
		if _, err := ol.Search(context.Background(), q); err == nil {
			t.Errorf("Search(%q) succeeded, want an error", q)
		}
	}
	if got, err := ol.Search(context.Background(), "nothing like it"); err != nil || len(got) != 0 {
		t.Errorf("Search with no hits = %v, %v; want none, no error", got, err)
	}
}

func TestDescription(t *testing.T) {
	ol := newOpenLibrary(t)
	ctx := context.Background()
	if got, err := ol.Description(ctx, "OL20893680W"); err != nil || got != "Piranesi's house is no ordinary building.\nIts rooms are infinite." {
		t.Errorf("string description = %q, %v", got, err)
	}
	if got, err := ol.Description(ctx, "OL27448W"); err != nil || got != "An epic." {
		t.Errorf("object description = %q, %v", got, err)
	}
	want := "From the New York Times bestselling author.\n\nFor fans of Circe.\n\nAlso contained in:\nThe Collection\n\n(source)"
	if got, err := ol.Description(ctx, "OL3W"); err != nil || got != want {
		t.Errorf("Markdown description = %q, %v\nwant %q", got, err, want)
	}
	if _, err := ol.Description(ctx, "OL404W"); err == nil {
		t.Error("missing work: want an error")
	}
	if _, err := ol.Description(ctx, "../../etc"); !errors.Is(err, books.ErrInvalid) {
		t.Errorf("bad work id = %v, want ErrInvalid", err)
	}
}

func TestCover(t *testing.T) {
	ol := newOpenLibrary(t)
	ctx := context.Background()
	ct, data, err := ol.Cover(ctx, 10226290, "M")
	if err != nil || ct != "image/png" || len(data) != len(onePNG) {
		t.Errorf("Cover = %q, %d bytes, %v; want the PNG, sniffed", ct, len(data), err)
	}
	if _, _, err := ol.Cover(ctx, 666, "M"); err == nil {
		t.Error("HTML posing as a cover: want an error")
	}
	if _, _, err := ol.Cover(ctx, 0, "M"); !errors.Is(err, books.ErrInvalid) {
		t.Errorf("cover id 0 = %v, want ErrInvalid", err)
	}
	if _, _, err := ol.Cover(ctx, 10226290, "XL"); !errors.Is(err, books.ErrInvalid) {
		t.Errorf("size XL = %v, want ErrInvalid", err)
	}
}

func TestTheRealGuardStillApplies(t *testing.T) {
	srv := fakeOpenLibrary(t)
	ol := &books.OpenLibrary{Web: webfetch.New(webfetch.Config{UserAgent: "test"}), Base: srv.URL, Covers: srv.URL, Timeout: time.Second}
	if _, err := ol.Search(context.Background(), "piranesi"); !errors.Is(err, webfetch.ErrBlockedAddress) {
		t.Errorf("Search against loopback with the real guard = %v, want ErrBlockedAddress", err)
	}
}
```

- [ ] **Step 2: Run them to see them fail**

Run: `go test ./internal/apps/books/... -count=1`
Expected: FAIL — `undefined: books.OpenLibrary`.

- [ ] **Step 3: Write `openlibrary.go`**

```go
package books

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/iliafrenkel/on-suite/internal/platform/webfetch"
)

// MaxCandidates is how many search results Open Library is asked for.
const MaxCandidates = 10

// coverTimeout bounds one image fetch: an Open Library cover (which usually
// redirects twice, to archive.org storage) or a pasted image address.
const coverTimeout = 10 * time.Second

// maxJSONBytes bounds a search or work answer; ten trimmed results are a
// few kilobytes.
const maxJSONBytes = 1 << 20

// searchFields is what search.json is asked to return — only what a
// Candidate needs.
const searchFields = "key,title,subtitle,author_name,first_publish_year,number_of_pages_median,isbn,cover_i,cover_edition_key"

// OpenLibrary is ON Books' client for openlibrary.org (search, work
// descriptions) and covers.openlibrary.org. Every request goes through Web,
// so the suite's SSRF guard and size caps apply (spec "Architecture").
// Base and Covers are fields so tests can point them at httptest.
type OpenLibrary struct {
	Web     *webfetch.Client
	Base    string        // "https://openlibrary.org"
	Covers  string        // "https://covers.openlibrary.org"
	Timeout time.Duration // per search or description request
}

// Candidate is one search result, cleaned up and ready to pre-fill the
// book form. Zero and "" mean Open Library didn't say.
type Candidate struct {
	WorkID, EditionID string
	Title, Subtitle   string
	Authors           string
	Year, Pages       int
	ISBN              string // ISBN-13
	CoverID           int64
}

type olDoc struct {
	Key              string   `json:"key"`
	Title            string   `json:"title"`
	Subtitle         string   `json:"subtitle"`
	AuthorName       []string `json:"author_name"`
	FirstPublishYear int      `json:"first_publish_year"`
	Pages            int      `json:"number_of_pages_median"`
	ISBN             []string `json:"isbn"`
	CoverI           int64    `json:"cover_i"`
	CoverEditionKey  string   `json:"cover_edition_key"`
}

// candidate cleans one doc; a doc with no title is no use and is skipped.
func (d olDoc) candidate() (Candidate, bool) {
	title := oneLine(d.Title)
	if title == "" {
		return Candidate{}, false
	}
	c := Candidate{
		WorkID:    olID(strings.TrimPrefix(d.Key, "/works/"), 'W'),
		EditionID: olID(d.CoverEditionKey, 'M'),
		Title:     title,
		Subtitle:  oneLine(d.Subtitle),
		Authors:   oneLine(strings.Join(d.AuthorName, ", ")),
		Year:      d.FirstPublishYear,
		Pages:     d.Pages,
		ISBN:      pickISBN(d.ISBN),
		CoverID:   d.CoverI,
	}
	if c.Year < 1 || c.Year > MaxYear {
		c.Year = 0
	}
	if c.Pages < 1 || c.Pages > MaxPages {
		c.Pages = 0
	}
	if c.CoverID < 0 {
		c.CoverID = 0
	}
	return c, true
}

// pickISBN prefers an ISBN-13 Open Library lists itself, then any ISBN-10
// it lists, converted.
func pickISBN(list []string) string {
	for _, s := range list {
		if len(s) == 13 {
			if v, ok := ISBN13(s); ok {
				return v
			}
		}
	}
	for _, s := range list {
		if v, ok := ISBN13(s); ok {
			return v
		}
	}
	return ""
}

// Search asks Open Library for books matching q — a title, an author, or
// an ISBN. Blank q asks nothing. A failure (timeout, error status, bad
// JSON) is an error; finding nothing is not.
func (o *OpenLibrary) Search(ctx context.Context, q string) ([]Candidate, error) {
	q = strings.TrimSpace(q)
	if q == "" {
		return nil, nil
	}
	ctx, cancel := context.WithTimeout(ctx, o.Timeout)
	defer cancel()
	v := url.Values{"q": {q}, "fields": {searchFields}, "limit": {strconv.Itoa(MaxCandidates)}}
	res, err := o.Web.Get(ctx, o.Base+"/search.json?"+v.Encode(),
		webfetch.GetOptions{Accept: "application/json", MaxBytes: maxJSONBytes})
	if err != nil {
		return nil, fmt.Errorf("books: open library search: %w", err)
	}
	var body struct {
		Docs []olDoc `json:"docs"`
	}
	if err := json.Unmarshal(res.Body, &body); err != nil {
		return nil, fmt.Errorf("books: open library search: %w", err)
	}
	out := []Candidate{}
	for _, d := range body.Docs {
		if c, ok := d.candidate(); ok {
			out = append(out, c)
		}
	}
	return out, nil
}

// Description returns a work's description as plain text ("" when it has
// none). Open Library stores it either as a string or as
// {"type": "/type/text", "value": "…"}.
func (o *OpenLibrary) Description(ctx context.Context, workID string) (string, error) {
	if olID(workID, 'W') == "" {
		return "", ErrInvalid
	}
	ctx, cancel := context.WithTimeout(ctx, o.Timeout)
	defer cancel()
	res, err := o.Web.Get(ctx, o.Base+"/works/"+workID+".json",
		webfetch.GetOptions{Accept: "application/json", MaxBytes: maxJSONBytes})
	if err != nil {
		return "", fmt.Errorf("books: open library work: %w", err)
	}
	var body struct {
		Description json.RawMessage `json:"description"`
	}
	if err := json.Unmarshal(res.Body, &body); err != nil {
		return "", fmt.Errorf("books: open library work: %w", err)
	}
	text := plainDescription(descriptionText(body.Description))
	if r := []rune(text); len(r) > MaxDescriptionRunes {
		text = strings.TrimSpace(string(r[:MaxDescriptionRunes]))
	}
	return text, nil
}

// Open Library descriptions are Markdown: emphasis, links, reference
// links with their definitions, and "----------" rules before an "Also
// contained in" list. A book's description here is plain text (spec "Data
// model"), so plainDescription keeps the words and drops the markup.
var (
	mdTrailing = regexp.MustCompile(`[ \t]+\n`)
	mdRefDef   = regexp.MustCompile(`(?m)^[ \t]*\[[^\]\n]+\]:[ \t]*\S.*$`) // [1]: https://…
	mdRule     = regexp.MustCompile(`(?m)^[ \t]*[-*_]{3,}[ \t]*$`)         // ----------
	mdLink     = regexp.MustCompile(`\[([^\]\n]+)\]\([^)\n]*\)`)           // [text](url)
	mdRefLink  = regexp.MustCompile(`\[([^\]\n]+)\]\[[^\]\n]*\]`)          // [text][1]
	mdItalic   = regexp.MustCompile(`\*([^*\n]+)\*`)                       // *text*
	mdBlank    = regexp.MustCompile(`\n{3,}`)
)

func plainDescription(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = mdTrailing.ReplaceAllString(s, "\n")
	s = mdRefDef.ReplaceAllString(s, "")
	s = mdRule.ReplaceAllString(s, "")
	s = mdLink.ReplaceAllString(s, "$1")
	s = mdRefLink.ReplaceAllString(s, "$1")
	s = strings.NewReplacer("**", "", "__", "").Replace(s)
	s = mdItalic.ReplaceAllString(s, "$1")
	s = mdBlank.ReplaceAllString(s, "\n\n")
	return strings.TrimSpace(s)
}

func descriptionText(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var obj struct {
		Value string `json:"value"`
	}
	if json.Unmarshal(raw, &obj) == nil {
		return obj.Value
	}
	return ""
}

// Cover fetches one Open Library cover by its id: size "S" for a search
// thumbnail, "M" for a book's stored cover. The content type is sniffed
// from the bytes (webfetch.GetImage), never taken from the server.
func (o *OpenLibrary) Cover(ctx context.Context, coverID int64, size string) (string, []byte, error) {
	if coverID <= 0 || (size != "S" && size != "M") {
		return "", nil, ErrInvalid
	}
	ctx, cancel := context.WithTimeout(ctx, coverTimeout)
	defer cancel()
	return o.Web.GetImage(ctx, fmt.Sprintf("%s/b/id/%d-%s.jpg", o.Covers, coverID, size), MaxCoverBytes)
}
```

- [ ] **Step 4: Run the tests, then the full check**

Run: `go test ./internal/apps/books/... -count=1`
Expected: PASS. Then the full check from Global Constraints.

- [ ] **Step 5: Commit**

```bash
git add internal/apps/books
git commit -m "feat(books): Open Library client — search, descriptions, covers (#487)"
```

---

### Task 3: Serve covers and proxy thumbnails

**Files:**
- Create: `internal/apps/books/covers.go`, `internal/apps/books/export_test.go`, `internal/apps/books/covers_test.go`
- Modify: `internal/apps/books/books.go` (fields, `Mount`), `internal/apps/books/cover.go` (`coverTypes`, `coverType`)

**Interfaces:**
- Consumes: `OpenLibrary`, `Cover`, `MaxCoverBytes` (Tasks 1–2); `userID`, `pathID`, `fail` (handlers.go); test helpers `newServer`, `add`, `titled`, `fakeOpenLibrary`, `onePNG`.
- Produces:
  - `App` fields `web *webfetch.Client`, `ol *OpenLibrary`, `thumbSem chan struct{}`
  - routes `GET /books/cover/{id}`, `GET /books/olcover/{id}`
  - `coverType(ct string) bool` (Task 4 adds `saveOLCover`, Task 6 `fetchCover`, each with its first caller)
  - test hook `(*App).UseOpenLibraryForTest(base string)`; test helper `newServerWithOL(t) (*server, string)` (the fake Open Library's URL)

- [ ] **Step 1: Write the test hook and the failing tests**

`internal/apps/books/export_test.go`:

```go
package books

import (
	"net"
	"net/netip"

	"github.com/iliafrenkel/on-suite/internal/platform/webfetch"
)

// UseOpenLibraryForTest points this app's Open Library client at base (an
// httptest server standing in for both openlibrary.org and
// covers.openlibrary.org) and lets its fetch client reach loopback — only
// loopback, so a test can never reach the real internet. Call it after
// Mount; apptest.NewServer has already mounted. It lives in a _test.go file
// (the export_test.go idiom Reader and Later use), so it never reaches the
// production binary.
func (a *App) UseOpenLibraryForTest(base string) {
	a.ol.Base, a.ol.Covers = base, base
	a.web.DenyAddr = func(address string) error {
		host, _, err := net.SplitHostPort(address)
		if err != nil {
			return webfetch.ErrBlockedAddress
		}
		if ip, err := netip.ParseAddr(host); err == nil && ip.Unmap().IsLoopback() {
			return nil
		}
		return webfetch.ErrBlockedAddress
	}
}
```

`internal/apps/books/covers_test.go`:

```go
package books_test

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/iliafrenkel/on-suite/internal/apps/books"
	"github.com/iliafrenkel/on-suite/internal/apptest"
)

// newServerWithOL is newServer with the app's Open Library client pointed
// at a fake one, whose URL it also returns.
func newServerWithOL(t *testing.T) (*server, string) {
	t.Helper()
	a := books.New()
	s := apptest.NewServer(t, a, books.NewStore)
	ol := fakeOpenLibrary(t).URL
	a.UseOpenLibraryForTest(ol)
	return s, ol
}

func get(t *testing.T, s *server, sess *apptest.Session, path string, header ...string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest("GET", path, nil)
	for i := 0; i+1 < len(header); i += 2 {
		req.Header.Set(header[i], header[i+1])
	}
	return s.Do(t, sess, req)
}

func TestCoverIsServedWithLongCaching(t *testing.T) {
	s := newServer(t)
	id := add(t, s, s.Alice.User.ID, titled("Piranesi", "", books.ShelfWant))
	if err := s.Store.SetCover(context.Background(), s.Alice.User.ID, id, "image/png", onePNG, books.CoverUpload); err != nil {
		t.Fatal(err)
	}
	c, _ := s.Store.Cover(context.Background(), s.Alice.User.ID, id)
	path := fmt.Sprintf("/books/cover/%d?v=%s", id, c.Version)

	rec := get(t, s, s.Alice, path)
	if rec.Code != http.StatusOK || !bytes.Equal(rec.Body.Bytes(), onePNG) {
		t.Fatalf("GET cover = %d, %d bytes", rec.Code, rec.Body.Len())
	}
	h := rec.Header()
	for name, want := range map[string]string{
		"Content-Type":           "image/png",
		"Cache-Control":          "private, max-age=31536000, immutable",
		"ETag":                   `"` + c.Version + `"`,
		"X-Content-Type-Options": "nosniff",
	} {
		if got := h.Get(name); got != want {
			t.Errorf("%s = %q, want %q", name, got, want)
		}
	}
	if rec := get(t, s, s.Alice, path, "If-None-Match", `"`+c.Version+`"`); rec.Code != http.StatusNotModified {
		t.Errorf("conditional GET = %d, want 304", rec.Code)
	}
}

func TestCoverIsNotFoundForOthersOrWhenMissing(t *testing.T) {
	s := newServer(t)
	with := add(t, s, s.Alice.User.ID, titled("Piranesi", "", books.ShelfWant))
	without := add(t, s, s.Alice.User.ID, titled("Emma", "", books.ShelfWant))
	if err := s.Store.SetCover(context.Background(), s.Alice.User.ID, with, "image/png", onePNG, books.CoverUpload); err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		sess *apptest.Session
		path string
	}{
		{s.Bob, fmt.Sprintf("/books/cover/%d", with)},
		{s.Alice, fmt.Sprintf("/books/cover/%d", without)},
		{s.Alice, "/books/cover/x"},
	} {
		if rec := get(t, s, tt.sess, tt.path); rec.Code != http.StatusNotFound {
			t.Errorf("GET %s = %d, want 404", tt.path, rec.Code)
		}
	}
}

func TestThumbnailsAreProxied(t *testing.T) {
	s, _ := newServerWithOL(t)
	rec := get(t, s, s.Alice, "/books/olcover/10226290")
	if rec.Code != http.StatusOK || !bytes.Equal(rec.Body.Bytes(), onePNG) {
		t.Fatalf("GET thumbnail = %d, %d bytes", rec.Code, rec.Body.Len())
	}
	if got := rec.Header().Get("Content-Type"); got != "image/png" {
		t.Errorf("Content-Type = %q, want the sniffed image/png", got)
	}
	if got := rec.Header().Get("Cache-Control"); got != "private, max-age=86400" {
		t.Errorf("Cache-Control = %q", got)
	}
	for _, path := range []string{"/books/olcover/abc", "/books/olcover/0", "/books/olcover/1234567890123", "/books/olcover/42"} {
		if rec := get(t, s, s.Alice, path); rec.Code != http.StatusNotFound {
			t.Errorf("GET %s = %d, want 404", path, rec.Code)
		}
	}
	if rec := s.Do(t, nil, httptest.NewRequest("GET", "/books/olcover/10226290", nil)); rec.Code != http.StatusSeeOther {
		t.Errorf("anonymous thumbnail = %d, want the sign-in redirect", rec.Code)
	}
}
```

- [ ] **Step 2: Run them to see them fail**

Run: `go test ./internal/apps/books/... -count=1`
Expected: FAIL — `a.ol undefined` in export_test.go, and 404s.

- [ ] **Step 3: Add `coverTypes` to `cover.go`**

After the source constants in `cover.go`, add:

```go
// coverTypes are the image types a cover may be, judged by sniffing the
// bytes rather than trusting what a server or browser claims — the same
// rule webfetch.GetImage follows, narrowed to formats every browser draws.
var coverTypes = map[string]bool{"image/jpeg": true, "image/png": true, "image/gif": true, "image/webp": true}

func coverType(ct string) bool { return coverTypes[ct] }
```

- [ ] **Step 4: Wire the client into `books.go`**

Add imports `"time"` and `"github.com/iliafrenkel/on-suite/internal/platform/webfetch"`. Extend `App`:

```go
// App is ON Books.
type App struct {
	store *Store
	deps  app.Deps
	// web is the only way this app reaches the network.
	web *webfetch.Client
	ol  *OpenLibrary
	// thumbSem bounds concurrent thumbnail fetches: a results page asks for
	// up to ten at once, and Open Library is a free service.
	thumbSem chan struct{}
}
```

In `Mount`, after the clock lines, add:

```go
	a.web = webfetch.New(webfetch.Config{
		UserAgent:       "onsuite/" + deps.Version + " (ON Books; +https://github.com/iliafrenkel/on-suite)",
		DefaultAccept:   "application/json",
		DefaultMaxBytes: webfetch.MaxPageBytes,
	})
	a.ol = &OpenLibrary{Web: a.web, Base: "https://openlibrary.org", Covers: "https://covers.openlibrary.org", Timeout: 5 * time.Second}
	a.thumbSem = make(chan struct{}, 4)
```
and with the routes:

```go
	r.HandleFunc("GET /cover/{id}", a.cover)
	r.HandleFunc("GET /olcover/{id}", a.olThumb)
```

- [ ] **Step 5: Write `covers.go`**

```go
package books

import (
	"net/http"
	"strconv"
)

// coverCacheControl: private (signed-in only) and long, because the URL
// carries the cover's version — a new cover is a new URL.
const coverCacheControl = "private, max-age=31536000, immutable"

// thumbCacheControl: Open Library's covers don't change, but these are
// throwaway search thumbnails; a day is plenty.
const thumbCacheControl = "private, max-age=86400"

// cover serves one of the viewer's books' stored cover.
func (a *App) cover(w http.ResponseWriter, r *http.Request) {
	uid, ok := a.userID(w, r)
	if !ok {
		return
	}
	id, ok := a.pathID(w, r)
	if !ok {
		return
	}
	c, err := a.store.Cover(r.Context(), uid, id)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	etag := `"` + c.Version + `"`
	h := w.Header()
	h.Set("Cache-Control", coverCacheControl)
	h.Set("ETag", etag)
	h.Set("X-Content-Type-Options", "nosniff")
	if r.Header.Get("If-None-Match") == etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	h.Set("Content-Type", c.ContentType)
	h.Set("Content-Length", strconv.Itoa(len(c.Bytes)))
	_, _ = w.Write(c.Bytes)
}

// olThumb proxies a small Open Library cover for the search results, so
// the suite's img-src stays 'self' (Open Library's cover URLs redirect to
// archive.org storage hosts, which a CSP would have to allow by wildcard).
// It takes a cover id — digits only — never a URL, so nothing here can be
// made to fetch anything but an Open Library cover.
func (a *App) olThumb(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.userID(w, r); !ok {
		return
	}
	raw := r.PathValue("id")
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || id <= 0 || len(raw) > 12 {
		a.deps.Errors.Status(w, r, http.StatusNotFound)
		return
	}
	select {
	case a.thumbSem <- struct{}{}:
		defer func() { <-a.thumbSem }()
	case <-r.Context().Done():
		return
	}
	ct, data, err := a.ol.Cover(r.Context(), id, "S")
	if err != nil || !coverType(ct) {
		if r.Context().Err() == nil {
			a.deps.Log.Info("books thumbnail fetch failed", "cover", id, "type", ct, "error", err)
		}
		a.deps.Errors.Status(w, r, http.StatusNotFound)
		return
	}
	h := w.Header()
	h.Set("Cache-Control", thumbCacheControl)
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Content-Type", ct)
	h.Set("Content-Length", strconv.Itoa(len(data)))
	_, _ = w.Write(data)
}
```

- [ ] **Step 6: Run the tests, then the full check**

Run: `go test ./internal/apps/books/... -count=1`
Expected: PASS. Then the full check from Global Constraints.

- [ ] **Step 7: Commit**

```bash
git add internal/apps/books
git commit -m "feat(books): serve stored covers and proxy Open Library thumbnails (#487)"
```

---
### Task 4: Search on the Add book page, pick a result, cover on save

**Files:**
- Modify: `internal/apps/books/form.go` (replace the file), `internal/apps/books/templates/form.html` (replace the file), `internal/apps/books/covers.go` (`errNotACover`, `saveOLCover`), `internal/ui/static/app.css`
- Test: `internal/apps/books/search_test.go`

**Interfaces:**
- Consumes: `OpenLibrary.Search/Description/Cover`, `Candidate`, `olID`, `coverType`, `SetCover`, `CoverFromOL`, `NewBook.OLWorkID/OLEditionID` (Tasks 1–3); `byline`, `numText`, `ISBN13`; test helpers `newServerWithOL`, `onePNG`.
- Produces: `GET /books/new?q=…` (results), `GET /books/new?pick=1&title=…&subtitle=…&authors=…&year=…&pages=…&isbn=…&ol_work=…&ol_edition=…&cover=…` (pre-filled form); hidden form fields `ol_work`, `ol_edition`, `cover_id`; `formValues.OLWork/OLEdition/CoverID`; `formView.Search searchView`; `(*App).saveOLCover`; element ids/classes `books-search-q`, `.books-search`, `.books-result`, `.books-result-cover`, `.books-result-use`, `.books-picked`.

- [ ] **Step 1: Write the failing tests**

`internal/apps/books/search_test.go`:

```go
package books_test

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"testing"

	"github.com/iliafrenkel/on-suite/internal/apps/books"
	"github.com/iliafrenkel/on-suite/internal/htmlassert"
)

func attr(t *testing.T, doc *htmlassert.Doc, selector, name string) string {
	t.Helper()
	v, _ := htmlassert.Attr(doc.MustHave(selector), name)
	return v
}

func TestAddPageSearchesOpenLibrary(t *testing.T) {
	s, _ := newServerWithOL(t)
	doc := s.Get(t, s.Alice, "/books/new?q=piranesi")
	if n := len(doc.QueryAll(".books-result")); n != 2 {
		t.Fatalf("%d results, want 2", n)
	}
	if got := htmlassert.Text(doc.MustHave(".books-result-title")); got != "Piranesi" {
		t.Errorf("first result = %q", got)
	}
	if got := attr(t, doc, "img.books-result-cover", "src"); got != "/books/olcover/10226290" {
		t.Errorf("thumbnail src = %q", got)
	}
	use, err := url.Parse(attr(t, doc, "a.books-result-use", "href"))
	if err != nil {
		t.Fatal(err)
	}
	q := use.Query()
	if use.Path != "/books/new" || q.Get("pick") != "1" || q.Get("title") != "Piranesi" ||
		q.Get("authors") != "Susanna Clarke" || q.Get("ol_work") != "OL20893680W" ||
		q.Get("ol_edition") != "OL28300471M" || q.Get("cover") != "10226290" || q.Get("isbn") != "9781526622440" {
		t.Errorf("Use this = %s", use)
	}
	if got := attr(t, doc, "input#books-search-q", "value"); got != "piranesi" {
		t.Errorf("search box = %q", got)
	}
	if got := attr(t, doc, "input#books-title", "value"); got != "" {
		t.Errorf("title = %q; the form stays empty while there are results to pick", got)
	}
}

func TestAFailedSearchFallsBackToTheForm(t *testing.T) {
	s, _ := newServerWithOL(t)
	doc := s.Get(t, s.Alice, "/books/new?q=broken")
	doc.MustHave(".books-search .notice-error")
	if got := attr(t, doc, "input#books-title", "value"); got != "broken" {
		t.Errorf("title = %q, want what was typed", got)
	}

	doc = s.Get(t, s.Alice, "/books/new?q=0-306-40615-2") // finds nothing
	doc.MustHave(".books-search .empty")
	if got := attr(t, doc, "input#books-isbn", "value"); got != "9780306406157" {
		t.Errorf("isbn = %q, want the typed ISBN as ISBN-13", got)
	}
	if got := attr(t, doc, "input#books-title", "value"); got != "" {
		t.Errorf("title = %q, want empty when an ISBN was typed", got)
	}
}

func TestPickingAResultFillsTheForm(t *testing.T) {
	s, _ := newServerWithOL(t)
	pick := url.Values{"pick": {"1"}, "title": {"Piranesi"}, "authors": {"Susanna Clarke"}, "year": {"2020"},
		"pages": {"272"}, "isbn": {"9781526622440"}, "ol_work": {"OL20893680W"}, "ol_edition": {"OL28300471M"},
		"cover": {"10226290"}}
	doc := s.Get(t, s.Alice, "/books/new?"+pick.Encode())
	for id, want := range map[string]string{"books-title": "Piranesi", "books-authors": "Susanna Clarke",
		"books-year": "2020", "books-pages": "272", "books-isbn": "9781526622440"} {
		if got := attr(t, doc, "input#"+id, "value"); got != want {
			t.Errorf("%s = %q, want %q", id, got, want)
		}
	}
	if got := htmlassert.Text(doc.MustHave("textarea#books-description")); !strings.HasPrefix(got, "Piranesi's house is no ordinary building.") {
		t.Errorf("description = %q, want Open Library's", got)
	}
	for name, want := range map[string]string{"ol_work": "OL20893680W", "ol_edition": "OL28300471M", "cover_id": "10226290"} {
		if got := attr(t, doc, `input[name="`+name+`"]`, "value"); got != want {
			t.Errorf("hidden %s = %q, want %q", name, got, want)
		}
	}
	if got := attr(t, doc, ".books-picked img", "src"); got != "/books/olcover/10226290" {
		t.Errorf("picked cover = %q", got)
	}

	doc = s.Get(t, s.Alice, "/books/new?pick=1&title=X&ol_work=..%2Fx&cover=12a")
	if attr(t, doc, `input[name="ol_work"]`, "value") != "" || attr(t, doc, `input[name="cover_id"]`, "value") != "" {
		t.Error("junk ids were carried into the form")
	}
	doc.MustNotHave(".books-picked")
}

func TestAddingAPickedBookFetchesItsCover(t *testing.T) {
	s, _ := newServerWithOL(t)
	s.Submit(t, s.Alice, "/books/new", url.Values{"title": {"Piranesi"}, "add_to": {"want"},
		"ol_work": {"OL20893680W"}, "ol_edition": {"OL28300471M"}, "cover_id": {"10226290"}}, "/books/b/1?shelf=want")
	c, err := s.Store.Cover(context.Background(), s.Alice.User.ID, 1)
	if err != nil || c.ContentType != "image/png" {
		t.Errorf("cover = %q, %v; want the fetched PNG", c.ContentType, err)
	}
}

func TestACoverThatWontComeStillAddsTheBook(t *testing.T) {
	s, _ := newServerWithOL(t)
	for i, cover := range []string{"666", "42"} { // HTML posing as a cover; missing
		s.Submit(t, s.Alice, "/books/new", url.Values{"title": {"Book " + cover}, "add_to": {"want"}, "cover_id": {cover}},
			fmt.Sprintf("/books/b/%d?shelf=want", i+1))
		if _, err := s.Store.Cover(context.Background(), s.Alice.User.ID, int64(i+1)); !errors.Is(err, books.ErrNotFound) {
			t.Errorf("cover %s: Cover = %v, want none", cover, err)
		}
	}
}
```

- [ ] **Step 2: Run them to see them fail**

Run: `go test ./internal/apps/books/... -run 'Search|Pick|Picked|Cover.*Come' -count=1`
Expected: FAIL — no `.books-result` elements; no cover stored.

- [ ] **Step 3: Add `saveOLCover` to `covers.go`**

Change its imports to:

```go
import (
	"context"
	"errors"
	"net/http"
	"strconv"
)
```
and append:

```go
// errNotACover is an image that isn't one of the cover types.
var errNotACover = errors.New("books: not a JPEG, PNG, GIF or WebP image")

// saveOLCover fetches a new book's Open Library cover and stores it. A
// failure is logged, not shown: the book is already saved and shows its
// spine (spec "Errors").
func (a *App) saveOLCover(ctx context.Context, userID, id, coverID int64) {
	ct, data, err := a.ol.Cover(ctx, coverID, "M")
	if err == nil && !coverType(ct) {
		err = errNotACover
	}
	if err == nil {
		err = a.store.SetCover(ctx, userID, id, ct, data, CoverFromOL)
	}
	if err != nil {
		a.deps.Log.Info("books cover fetch failed", "book", id, "cover", coverID, "error", err)
	}
}
```

- [ ] **Step 4: Replace `form.go`**

```go
package books

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// formValues is what the book form shows: the raw text, so a mistyped
// number comes back exactly as typed. AddTo, FinishedOn and Tags are for a
// new book only; so are OLWork, OLEdition and CoverID, which carry an Open
// Library pick through the form in hidden fields.
type formValues struct {
	Title, Subtitle, Authors, Year, Pages, ISBN string
	SeriesName, SeriesNumber, Description       string
	AddTo, FinishedOn, Tags                     string
	OLWork, OLEdition, CoverID                  string
}

func valuesOf(in BookInput) formValues {
	return formValues{Title: in.Title, Subtitle: in.Subtitle, Authors: in.Authors,
		Year: numText(in.Year), Pages: numText(in.Pages), ISBN: in.ISBN,
		SeriesName: in.SeriesName, SeriesNumber: in.SeriesNumber, Description: in.Description}
}

func numText(n int) string {
	if n == 0 {
		return ""
	}
	return strconv.Itoa(n)
}

// searchView is the Open Library search on the Add book page.
type searchView struct {
	Query    string
	Searched bool
	Error    string
	Results  []resultView
}

// resultView is one search result.
type resultView struct {
	Title   string
	Byline  string // "Authors · Year"
	Thumb   string // the proxied thumbnail; "" when Open Library has no cover
	PickURL string // the Add book page pre-filled with this result
}

// formView is the Add/Edit book page.
type formView struct {
	New     bool
	Action  string
	Heading string
	Submit  string
	Cancel  string
	Values  formValues
	Errors  FieldErrors
	Today   string // the latest finish date the form allows
	Ctx     listCtx
	Search  searchView
}

// parseForm reads a posted book form: the input, the raw values to echo
// back, and a message per number field that isn't a whole number.
func parseForm(get func(string) string) (BookInput, formValues, FieldErrors) {
	v := formValues{Title: get("title"), Subtitle: get("subtitle"), Authors: get("authors"),
		Year: get("year"), Pages: get("pages"), ISBN: get("isbn"),
		SeriesName: get("series_name"), SeriesNumber: get("series_number"), Description: get("description"),
		AddTo: get("add_to"), FinishedOn: strings.TrimSpace(get("finished_on")), Tags: get("tags"),
		OLWork: olID(get("ol_work"), 'W'), OLEdition: olID(get("ol_edition"), 'M'), CoverID: coverIDText(get("cover_id"))}
	in := BookInput{Title: v.Title, Subtitle: v.Subtitle, Authors: v.Authors, ISBN: v.ISBN,
		SeriesName: v.SeriesName, SeriesNumber: v.SeriesNumber, Description: v.Description}
	errs := FieldErrors{}
	number := func(field, raw string, dst *int) {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			return
		}
		n, err := strconv.Atoi(raw)
		if err != nil || n <= 0 {
			errs[field] = "Enter a whole number, or leave it empty."
			return
		}
		*dst = n
	}
	number("year", v.Year, &in.Year)
	number("pages", v.Pages, &in.Pages)
	return in, v, errs
}

// merge adds b's messages for fields a has nothing to say about: a "whole
// number" complaint beats the range check of the same field.
func merge(a, b FieldErrors) FieldErrors {
	for k, msg := range b {
		if _, ok := a[k]; !ok {
			a[k] = msg
		}
	}
	return a
}

// coverIDText keeps an Open Library cover id: 1–12 digits, not zero.
func coverIDText(s string) string {
	s = strings.TrimSpace(s)
	if s == "" || len(s) > 12 || strings.Trim(s, "0123456789") != "" || strings.TrimLeft(s, "0") == "" {
		return ""
	}
	return s
}

// thumbURL is a search result's proxied thumbnail.
func thumbURL(coverID int64) string {
	if coverID <= 0 {
		return ""
	}
	return "/books/olcover/" + strconv.FormatInt(coverID, 10)
}

// pickURL is the Add book page pre-filled with c. Everything travels in
// the link: the values are only a starting point the person edits anyway.
func pickURL(c Candidate) string {
	v := url.Values{"pick": {"1"}}
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

const searchFailed = "Open Library didn't answer. Try again, or fill in the book yourself below."

// search asks Open Library for q. A failure is logged and shown as a
// notice; the page still works.
func (a *App) search(ctx context.Context, q string) searchView {
	sv := searchView{Query: q, Searched: true}
	found, err := a.ol.Search(ctx, q)
	if err != nil {
		a.deps.Log.Info("books open library search failed", "error", err)
		sv.Error = searchFailed
		return sv
	}
	for _, c := range found {
		sv.Results = append(sv.Results, resultView{Title: c.Title, Byline: byline(c.Authors, numText(c.Year)),
			Thumb: thumbURL(c.CoverID), PickURL: pickURL(c)})
	}
	return sv
}

// picked is the form pre-filled from a "Use this" link, with the work's
// description fetched now (a failure leaves it empty).
func (a *App) picked(ctx context.Context, q url.Values) formValues {
	v := formValues{Title: q.Get("title"), Subtitle: q.Get("subtitle"), Authors: q.Get("authors"),
		Year: q.Get("year"), Pages: q.Get("pages"), ISBN: q.Get("isbn"), AddTo: string(ShelfWant),
		OLWork: olID(q.Get("ol_work"), 'W'), OLEdition: olID(q.Get("ol_edition"), 'M'), CoverID: coverIDText(q.Get("cover"))}
	if v.OLWork != "" {
		d, err := a.ol.Description(ctx, v.OLWork)
		if err != nil {
			a.deps.Log.Info("books open library description failed", "work", v.OLWork, "error", err)
		}
		v.Description = d
	}
	return v
}

func (a *App) renderForm(w http.ResponseWriter, r *http.Request, status int, v formView) {
	page := a.deps.Page(r, v.Heading)
	page.Data = v
	if err := a.deps.Render.Page(w, status, "books/form", page); err != nil {
		a.deps.Errors.Internal(w, r, err)
	}
}

func (a *App) newBookForm(v formValues, errs FieldErrors) formView {
	return formView{New: true, Action: "/books/new", Heading: "Add a book", Submit: "Add book",
		Cancel: "/books/", Values: v, Errors: errs, Today: a.store.Today()}
}

// newForm is the Add book page: an Open Library search (q), a result
// picked from it (pick), or an empty form.
func (a *App) newForm(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.userID(w, r); !ok {
		return
	}
	q := r.URL.Query()
	v := formValues{AddTo: string(ShelfWant)}
	if q.Get("pick") != "" {
		v = a.picked(r.Context(), q)
	}
	view := a.newBookForm(v, nil)
	if text := strings.TrimSpace(q.Get("q")); text != "" {
		view.Search = a.search(r.Context(), text)
		// Nothing to pick from: start the form with what was typed (spec
		// "Screens → Add book").
		if len(view.Search.Results) == 0 && view.Values.Title == "" && view.Values.ISBN == "" {
			if isbn, ok := ISBN13(text); ok {
				view.Values.ISBN = isbn
			} else {
				view.Values.Title = text
			}
		}
	}
	a.renderForm(w, r, http.StatusOK, view)
}

func (a *App) create(w http.ResponseWriter, r *http.Request) {
	uid, ok := a.userID(w, r)
	if !ok {
		return
	}
	in, vals, errs := parseForm(r.PostFormValue)
	errs = merge(errs, in.Normalize().Validate())
	shelf, _ := ParseShelf(vals.AddTo)
	if len(errs) > 0 {
		a.renderForm(w, r, http.StatusUnprocessableEntity, a.newBookForm(vals, errs))
		return
	}
	id, err := a.store.Create(r.Context(), uid, NewBook{BookInput: in, Shelf: shelf,
		FinishedOn: vals.FinishedOn, Tags: ParseTags(vals.Tags),
		OLWorkID: vals.OLWork, OLEditionID: vals.OLEdition})
	var verr *ValidationError
	var ref *Refusal
	switch {
	case errors.As(err, &verr):
		a.renderForm(w, r, http.StatusUnprocessableEntity, a.newBookForm(vals, verr.Fields))
		return
	case errors.As(err, &ref):
		a.renderForm(w, r, http.StatusUnprocessableEntity, a.newBookForm(vals, FieldErrors{"finished_on": ref.Msg}))
		return
	case errors.Is(err, ErrInvalid):
		a.renderForm(w, r, http.StatusUnprocessableEntity, a.newBookForm(vals, FieldErrors{"add_to": "Pick where the book goes."}))
		return
	case err != nil:
		a.fail(w, r, err)
		return
	}
	if coverID, err := strconv.ParseInt(vals.CoverID, 10, 64); err == nil {
		a.saveOLCover(r.Context(), uid, id, coverID)
	}
	http.Redirect(w, r, listCtx{Shelf: shelf}.BookURL(id), http.StatusSeeOther)
}

func editBookForm(id int64, v formValues, errs FieldErrors, c listCtx) formView {
	return formView{Action: "/books/edit/" + strconv.FormatInt(id, 10), Heading: "Edit book", Submit: "Save",
		Cancel: c.BookURL(id), Values: v, Errors: errs, Ctx: c}
}

func (a *App) editForm(w http.ResponseWriter, r *http.Request) {
	uid, ok := a.userID(w, r)
	if !ok {
		return
	}
	id, ok := a.pathID(w, r)
	if !ok {
		return
	}
	b, err := a.store.Get(r.Context(), uid, id)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	a.renderForm(w, r, http.StatusOK, editBookForm(id, valuesOf(b.BookInput), nil, ctxFrom(r.FormValue)))
}

func (a *App) update(w http.ResponseWriter, r *http.Request) {
	uid, ok := a.userID(w, r)
	if !ok {
		return
	}
	id, ok := a.pathID(w, r)
	if !ok {
		return
	}
	c := ctxFrom(r.PostFormValue)
	in, vals, errs := parseForm(r.PostFormValue)
	errs = merge(errs, in.Normalize().Validate())
	if len(errs) > 0 {
		// Someone else's book is a 404 even when the form is wrong too.
		if _, err := a.store.Get(r.Context(), uid, id); err != nil {
			a.fail(w, r, err)
			return
		}
		a.renderForm(w, r, http.StatusUnprocessableEntity, editBookForm(id, vals, errs, c))
		return
	}
	if err := a.store.Update(r.Context(), uid, id, in); err != nil {
		a.fail(w, r, err)
		return
	}
	http.Redirect(w, r, c.BookURL(id), http.StatusSeeOther)
}
```

- [ ] **Step 5: Replace `templates/form.html`**

```html
{{define "content"}}
{{$d := .Data}}
<div class="books-form-page stack">
	<h1>{{$d.Heading}}</h1>
	{{if $d.New}}{{template "ol-search" $d.Search}}{{end}}
	<form class="books-form stack" method="post" action="{{$d.Action}}" novalidate>
		<input type="hidden" name="{{csrfField}}" value="{{.Shell.CSRFToken}}">
		{{if not $d.New}}{{template "ctx-fields" $d.Ctx}}{{end}}
		{{if $d.New}}
		<input type="hidden" name="ol_work" value="{{$d.Values.OLWork}}">
		<input type="hidden" name="ol_edition" value="{{$d.Values.OLEdition}}">
		<input type="hidden" name="cover_id" value="{{$d.Values.CoverID}}">
		{{if $d.Search.Searched}}<h2 class="books-form-sub">Or fill it in yourself</h2>{{end}}
		{{with $d.Values.CoverID}}
		<div class="books-picked">
			<img class="books-result-cover" src="/books/olcover/{{.}}" alt="" width="40" height="60">
			<p>From Open Library — check the details, then add it. The cover comes too.</p>
		</div>
		{{end}}
		{{end}}
		{{if $d.Errors}}<div class="notice notice-error" role="alert">Please fix the fields marked below.</div>{{end}}

		{{template "text-field" (dict "Name" "title" "Label" "Title" "Value" $d.Values.Title "Error" (index $d.Errors "title"))}}
		{{template "text-field" (dict "Name" "subtitle" "Label" "Subtitle" "Value" $d.Values.Subtitle "Error" (index $d.Errors "subtitle"))}}
		{{template "text-field" (dict "Name" "authors" "Label" "Author(s)" "Placeholder" "Ursula K. Le Guin" "Value" $d.Values.Authors "Error" (index $d.Errors "authors"))}}
		<div class="books-form-row">
			{{template "text-field" (dict "Name" "year" "Label" "First published" "Mode" "numeric" "Value" $d.Values.Year "Error" (index $d.Errors "year"))}}
			{{template "text-field" (dict "Name" "pages" "Label" "Pages" "Mode" "numeric" "Value" $d.Values.Pages "Error" (index $d.Errors "pages"))}}
			{{template "text-field" (dict "Name" "isbn" "Label" "ISBN" "Value" $d.Values.ISBN "Error" (index $d.Errors "isbn"))}}
		</div>
		<div class="books-form-row">
			{{template "text-field" (dict "Name" "series_name" "Label" "Series" "Value" $d.Values.SeriesName "Error" (index $d.Errors "series_name"))}}
			{{template "text-field" (dict "Name" "series_number" "Label" "Number in series" "Value" $d.Values.SeriesNumber "Error" (index $d.Errors "series_number"))}}
		</div>
		<div class="field">
			<label for="books-description">Description</label>
			<textarea id="books-description" name="description" rows="5"{{if index $d.Errors "description"}} aria-invalid="true" aria-describedby="books-description-error"{{end}}>{{$d.Values.Description}}</textarea>
			{{template "field-error" (dict "Name" "description" "Error" (index $d.Errors "description"))}}
		</div>

		{{if $d.New}}
		<fieldset class="field books-add-to"{{if index $d.Errors "add_to"}} aria-describedby="books-add_to-error"{{end}}>
			<legend>Add it to</legend>
			<label class="books-choice"><input type="radio" name="add_to" value="want"{{if eq $d.Values.AddTo "want"}} checked{{end}}> Want to read</label>
			<label class="books-choice"><input type="radio" name="add_to" value="reading"{{if eq $d.Values.AddTo "reading"}} checked{{end}}> Reading now — started today</label>
			<label class="books-choice"><input type="radio" name="add_to" value="read"{{if eq $d.Values.AddTo "read"}} checked{{end}}> Already read</label>
			{{template "field-error" (dict "Name" "add_to" "Error" (index $d.Errors "add_to"))}}
		</fieldset>
		{{/* Shown only while "Already read" is picked (CSS :has(), no script). */}}
		<div class="field books-finished">
			<label for="books-finished_on">Finished on</label>
			<input id="books-finished_on" name="finished_on" type="date" max="{{$d.Today}}" value="{{if $d.Values.FinishedOn}}{{$d.Values.FinishedOn}}{{else}}{{$d.Today}}{{end}}"{{if index $d.Errors "finished_on"}} aria-invalid="true" aria-describedby="books-finished_on-error"{{end}}>
			{{template "field-error" (dict "Name" "finished_on" "Error" (index $d.Errors "finished_on"))}}
		</div>
		{{template "text-field" (dict "Name" "tags" "Label" "Tags" "Placeholder" "sf, favourites" "Value" $d.Values.Tags "Error" "")}}
		{{end}}

		<div class="books-form-actions">
			<button class="primary" type="submit">{{$d.Submit}}</button>
			<a href="{{$d.Cancel}}">Cancel</a>
		</div>
	</form>
</div>
{{end}}

{{/* ol-search is the Open Library search on the Add book page: a plain
     GET form, so it works without JavaScript. Takes a searchView. */}}
{{define "ol-search"}}
<section class="books-search stack" aria-label="Find a book on Open Library">
	<form class="books-search-form" method="get" action="/books/new" role="search">
		<label for="books-search-q">Find it on Open Library</label>
		<div class="books-search-row">
			<input id="books-search-q" name="q" type="search" value="{{.Query}}" placeholder="Title, author or ISBN">
			<button type="submit">Search</button>
		</div>
	</form>
	{{with .Error}}<p class="notice notice-error" role="alert">{{.}}</p>{{end}}
	{{if .Results}}
	<ul class="books-results">
		{{range .Results}}
		<li class="books-result">
			{{if .Thumb}}<img class="books-result-cover" src="{{.Thumb}}" alt="" width="40" height="60" loading="lazy">{{else}}<span class="books-result-cover" aria-hidden="true"></span>{{end}}
			<span class="books-result-text"><span class="books-result-title">{{.Title}}</span>{{with .Byline}}<span class="books-result-byline">{{.}}</span>{{end}}</span>
			<a class="button books-result-use" href="{{.PickURL}}">Use this</a>
		</li>
		{{end}}
	</ul>
	{{else if and .Searched (not .Error)}}
	<p class="empty">Open Library has nothing for “{{.Query}}”. Fill it in yourself below.</p>
	{{end}}
</section>
{{end}}
```

- [ ] **Step 6: Add the CSS**

Append to the ON Books section of `internal/ui/static/app.css`:

```css
/* Add book page: Open Library search */
.books-form-page { max-width: var(--measure); padding: var(--s-4) 0; }
.books-form-page .books-form { padding: 0; }
.books-search-row { display: flex; gap: var(--s-2); }
.books-search-row input { flex: 1 1 auto; min-width: 0; }
.books-results { list-style: none; margin: 0; padding: 0; }
.books-result { display: flex; align-items: center; gap: var(--s-3); padding: var(--s-2) 0; border-bottom: 1px solid var(--c-border); }
.books-result-cover { flex: 0 0 auto; display: block; width: 2.5rem; height: 3.75rem; object-fit: cover; border-radius: 2px; background: var(--c-bg-subtle); }
.books-result-text { display: flex; flex-direction: column; flex: 1 1 auto; min-width: 0; }
.books-result-title { font-weight: 600; }
.books-result-byline { color: var(--c-text-dim); font-size: var(--fs-sm); }
.books-result-use { flex: 0 0 auto; }
.books-form-sub { margin: var(--s-2) 0 0; font-size: var(--fs-base); }
.books-picked { display: flex; align-items: center; gap: var(--s-3); }
.books-picked p { margin: 0; color: var(--c-text-dim); font-size: var(--fs-sm); }
```

- [ ] **Step 7: Run the tests, then the full check**

Run: `go test ./internal/apps/books/... -count=1`
Expected: PASS (the B1a form tests too). Then the full check from Global Constraints.

- [ ] **Step 8: Commit**

```bash
git add -A internal/apps/books internal/ui/static/app.css
git commit -m "feat(books): find a book on Open Library from the Add book page (#487)"
```

---

### Task 5: Covers in the list and the book pane

**Files:**
- Modify: `internal/apps/books/view.go` (`rowView`, `bookView`, `viewList`, `viewBook`, new `coverURL`), `internal/apps/books/templates/panes.partial.html` (`list` and `book` blocks), `internal/ui/static/app.css`
- Test: `internal/apps/books/covers_test.go` (append)

**Interfaces:**
- Consumes: `Book.CoverVersion`, `ListItem.CoverVersion` (Task 1).
- Produces: `coverURL(id int64, version string) string`; `rowView.Cover`, `bookView.Cover` (URL, "" = draw the spine); classes `.books-mini-cover`, `.books-cover`.

- [ ] **Step 1: Write the failing test**

Append to `internal/apps/books/covers_test.go` (add `"strings"` and `"github.com/iliafrenkel/on-suite/internal/htmlassert"` to its imports):

```go
func TestCoversShowInTheListAndTheBookPane(t *testing.T) {
	s := newServer(t)
	uid := s.Alice.User.ID
	with := add(t, s, uid, titled("Piranesi", "", books.ShelfWant))
	without := add(t, s, uid, titled("Emma", "", books.ShelfWant))
	if err := s.Store.SetCover(context.Background(), uid, with, "image/png", onePNG, books.CoverUpload); err != nil {
		t.Fatal(err)
	}
	c, _ := s.Store.Cover(context.Background(), uid, with)
	want := fmt.Sprintf("/books/cover/%d?v=%s", with, c.Version)

	doc := s.Get(t, s.Alice, fmt.Sprintf("/books/b/%d?shelf=want", with))
	minis := doc.QueryAll("img.books-mini-cover")
	if len(minis) != 1 {
		t.Fatalf("%d mini covers, want 1 (Emma has none)", len(minis))
	}
	if src, _ := htmlassert.Attr(minis[0], "src"); src != want {
		t.Errorf("mini cover src = %q, want %q", src, want)
	}
	if n := len(doc.QueryAll(".books-mini-spine")); n != 1 {
		t.Errorf("%d mini spines, want 1 for the book without a cover", n)
	}
	big := doc.MustHave("img.books-cover")
	if src, _ := htmlassert.Attr(big, "src"); src != want {
		t.Errorf("cover src = %q, want %q", src, want)
	}
	if alt, _ := htmlassert.Attr(big, "alt"); !strings.Contains(alt, "Piranesi") {
		t.Errorf("cover alt = %q", alt)
	}
	doc.MustNotHave(".books-book .books-spine")

	doc = s.Get(t, s.Alice, fmt.Sprintf("/books/b/%d?shelf=want", without))
	doc.MustHave(".books-book .books-spine")
	doc.MustNotHave("img.books-cover")
}
```

- [ ] **Step 2: Run it to see it fail**

Run: `go test ./internal/apps/books/... -run TestCoversShow -count=1`
Expected: FAIL — `0 mini covers, want 1`.

- [ ] **Step 3: Update `view.go`**

Add after `seriesText`:

```go
// coverURL is a book's stored cover; the version makes a new cover a new
// URL, so the old one can be cached for a year. "" draws the spine.
func coverURL(id int64, version string) string {
	if version == "" {
		return ""
	}
	return "/books/cover/" + strconv.FormatInt(id, 10) + "?v=" + version
}
```

In `rowView`, after `Initial`, add `Cover   string // the stored cover; "" draws the mini spine`. In `viewList`'s `rowView{…}` literal add `Cover: coverURL(it.ID, it.CoverVersion),`.

In `bookView`, after `Spine`, add `Cover string // the stored cover; "" draws the spine`. In `viewBook`'s literal add `Cover: coverURL(b.ID, b.CoverVersion),`.

- [ ] **Step 4: Update the templates**

In `panes.partial.html`'s `list` block, replace

```html
				<span class="books-mini-spine swatch-c-{{.Spine}}" aria-hidden="true">{{.Initial}}</span>
```
with
```html
				{{if .Cover}}<img class="books-mini-cover" src="{{.Cover}}" alt="" width="32" height="48" loading="lazy">{{else}}<span class="books-mini-spine swatch-c-{{.Spine}}" aria-hidden="true">{{.Initial}}</span>{{end}}
```

In the `book` block, replace

```html
		<div class="books-spine swatch-c-{{.Spine}}" aria-hidden="true"><span>{{.Title}}</span></div>
```
with
```html
		{{if .Cover}}<img class="books-cover" src="{{.Cover}}" alt="Cover of {{.Title}}" width="96" height="144">{{else}}<div class="books-spine swatch-c-{{.Spine}}" aria-hidden="true"><span>{{.Title}}</span></div>{{end}}
```

- [ ] **Step 5: Add the CSS**

Append to the ON Books section of `internal/ui/static/app.css`:

```css
/* Covers: the same footprint as the spines they replace */
.books-mini-cover { flex: 0 0 auto; display: block; width: 2rem; height: 3rem; object-fit: cover; border-radius: 2px; background: var(--c-bg-subtle); }
.books-cover { flex: 0 0 auto; display: block; width: 6rem; height: auto; max-height: 9rem; object-fit: contain; border-radius: 3px; box-shadow: 0 1px 3px rgba(0, 0, 0, 0.15); }
```

- [ ] **Step 6: Run the tests, then the full check**

Run: `go test ./internal/apps/books/... -count=1`
Expected: PASS. Then the full check from Global Constraints.

- [ ] **Step 7: Commit**

```bash
git add -A internal/apps/books internal/ui/static/app.css
git commit -m "feat(books): show covers in the list and the book pane (#487)"
```

---

### Task 6: Change a cover on the Edit page

**Files:**
- Modify: `internal/apps/books/covers.go` (`fetchCover`, `coverChange`, `readCoverChange`, `readUploadedCover`, `applyCoverChange`), `internal/apps/books/form.go` (`formValues`, `formView`, `parseForm`, `editBookForm`, `editForm`, `update`), `internal/apps/books/templates/form.html` (multipart, cover fieldset), `internal/apps/books/books.go` (body limit), `internal/ui/static/app.css`
- Test: `internal/apps/books/cover_edit_test.go`

**Interfaces:**
- Consumes: `SetCover`, `RemoveCover`, `CoverUpload`, `CoverFromURL`, `MaxCoverBytes`, `coverType`, `coverURL`, `errNotACover`, `a.web` (Tasks 1–5); test helpers `newServer`, `newServerWithOL`, `add`, `titled`, `onePNG`, `attr`.
- Produces: edit form fields `cover_file` (file), `cover_url`, `remove_cover=1`; field error key `cover`; `const editFormMaxBytes = MaxCoverBytes + 1<<20`; `formValues.CoverURL`; `formView.Cover`, `formView.Spine`; test helper `postMultipart`.

- [ ] **Step 1: Write the failing tests**

`internal/apps/books/cover_edit_test.go`:

```go
package books_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/iliafrenkel/on-suite/internal/apps/books"
	"github.com/iliafrenkel/on-suite/internal/apptest"
	"github.com/iliafrenkel/on-suite/internal/htmlassert"
	"github.com/iliafrenkel/on-suite/internal/platform/web"
)

// postMultipart submits a form the way a browser does when it has a file
// input: multipart, with the CSRF token as a field, no JavaScript.
func postMultipart(t *testing.T, s *server, sess *apptest.Session, path string, fields url.Values, file string, content []byte) *httptest.ResponseRecorder {
	t.Helper()
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	if err := mw.WriteField(web.CSRFFormField, s.CSRFToken(t, sess)); err != nil {
		t.Fatal(err)
	}
	for k, vs := range fields {
		for _, v := range vs {
			if err := mw.WriteField(k, v); err != nil {
				t.Fatal(err)
			}
		}
	}
	if file != "" {
		part, err := mw.CreateFormFile("cover_file", file)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := part.Write(content); err != nil {
			t.Fatal(err)
		}
	}
	if err := mw.Close(); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest("POST", path, &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	return s.Do(t, sess, req)
}

// details is the edit form's minimum: the title and the list to go back to.
func details(title string) url.Values { return url.Values{"title": {title}, "shelf": {"want"}} }

func coverSource(t *testing.T, s *server, id int64) string {
	t.Helper()
	// The store has no getter for the source (nothing shows it yet), so
	// read it the way the schema tests do.
	var source string
	if err := s.Store.DBForTest().QueryRow(`SELECT source FROM books_covers WHERE book_id = ?`, id).Scan(&source); err != nil {
		return ""
	}
	return source
}

func TestEditFormOffersCoverChoices(t *testing.T) {
	s := newServer(t)
	id := add(t, s, s.Alice.User.ID, titled("Piranesi", "", books.ShelfWant))
	doc := s.Get(t, s.Alice, fmt.Sprintf("/books/edit/%d?shelf=want", id))
	if got := attr(t, doc, "form.books-form", "enctype"); got != "multipart/form-data" {
		t.Errorf("enctype = %q", got)
	}
	doc.MustHave(`input[name="cover_file"]`)
	doc.MustHave(`input[name="cover_url"]`)
	doc.MustHave(".books-cover-edit .books-spine")
	doc.MustNotHave(`input[name="remove_cover"]`)

	if err := s.Store.SetCover(context.Background(), s.Alice.User.ID, id, "image/png", onePNG, books.CoverUpload); err != nil {
		t.Fatal(err)
	}
	doc = s.Get(t, s.Alice, fmt.Sprintf("/books/edit/%d?shelf=want", id))
	doc.MustHave(".books-cover-edit img.books-cover")
	doc.MustHave(`input[name="remove_cover"]`)
}

func TestUploadingACover(t *testing.T) {
	s := newServer(t)
	id := add(t, s, s.Alice.User.ID, titled("Piranesi", "", books.ShelfWant))
	rec := postMultipart(t, s, s.Alice, fmt.Sprintf("/books/edit/%d", id), details("Piranesi"), "cover.png", onePNG)
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != fmt.Sprintf("/books/b/%d?shelf=want", id) {
		t.Fatalf("upload = %d → %q; body %s", rec.Code, rec.Header().Get("Location"), rec.Body.String())
	}
	c, err := s.Store.Cover(context.Background(), s.Alice.User.ID, id)
	if err != nil || !bytes.Equal(c.Bytes, onePNG) || coverSource(t, s, id) != books.CoverUpload {
		t.Errorf("cover = %d bytes, %v, source %q", len(c.Bytes), err, coverSource(t, s, id))
	}
}

func TestABadUploadSavesNothing(t *testing.T) {
	s := newServer(t)
	id := add(t, s, s.Alice.User.ID, titled("Piranesi", "", books.ShelfWant))
	tooBig := append(append([]byte{}, onePNG...), make([]byte, books.MaxCoverBytes)...)
	for name, content := range map[string][]byte{"notes.txt": []byte("just some text"), "huge.png": tooBig} {
		rec := postMultipart(t, s, s.Alice, fmt.Sprintf("/books/edit/%d", id), details("Renamed"), name, content)
		if rec.Code != http.StatusUnprocessableEntity {
			t.Fatalf("%s: = %d, want 422", name, rec.Code)
		}
		htmlassert.Parse(t, rec.Body.String()).MustHave("#books-cover-error")
	}
	b, _ := s.Store.Get(context.Background(), s.Alice.User.ID, id)
	if b.Title != "Piranesi" || b.CoverVersion != "" {
		t.Errorf("after refused uploads: title %q, cover %q; want nothing saved", b.Title, b.CoverVersion)
	}
}

func TestACoverFromAnAddress(t *testing.T) {
	s, ol := newServerWithOL(t)
	id := add(t, s, s.Alice.User.ID, titled("Piranesi", "", books.ShelfWant))
	path := fmt.Sprintf("/books/edit/%d", id)

	form := details("Piranesi")
	form.Set("cover_url", ol+"/b/id/666-M.jpg") // HTML, not an image
	if rec := postMultipart(t, s, s.Alice, path, form, "", nil); rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("HTML address = %d, want 422", rec.Code)
	}
	form.Set("cover_url", ol+"/b/id/10226290-M.jpg")
	if rec := postMultipart(t, s, s.Alice, path, form, "", nil); rec.Code != http.StatusSeeOther {
		t.Fatalf("image address = %d, want 303; body %s", rec.Code, rec.Body.String())
	}
	if coverSource(t, s, id) != books.CoverFromURL {
		t.Errorf("source = %q, want url", coverSource(t, s, id))
	}
}

func TestACoverAddressGoesThroughTheRealGuard(t *testing.T) {
	s := newServer(t) // no test hook: the production guard
	img := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write(onePNG) }))
	defer img.Close()
	id := add(t, s, s.Alice.User.ID, titled("Piranesi", "", books.ShelfWant))
	form := details("Piranesi")
	form.Set("cover_url", img.URL+"/cover.png")
	rec := postMultipart(t, s, s.Alice, fmt.Sprintf("/books/edit/%d", id), form, "", nil)
	if rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), "books-cover-error") {
		t.Errorf("loopback address = %d, want 422 with a cover error", rec.Code)
	}
}

func TestRemovingAndKeepingACover(t *testing.T) {
	s := newServer(t)
	id := add(t, s, s.Alice.User.ID, titled("Piranesi", "", books.ShelfWant))
	if err := s.Store.SetCover(context.Background(), s.Alice.User.ID, id, "image/png", onePNG, books.CoverUpload); err != nil {
		t.Fatal(err)
	}
	path := fmt.Sprintf("/books/edit/%d", id)
	if rec := postMultipart(t, s, s.Alice, path, details("Piranesi, edited"), "", nil); rec.Code != http.StatusSeeOther {
		t.Fatalf("plain edit = %d", rec.Code)
	}
	if _, err := s.Store.Cover(context.Background(), s.Alice.User.ID, id); err != nil {
		t.Errorf("a plain edit lost the cover: %v", err)
	}
	form := details("Piranesi")
	form.Set("remove_cover", "1")
	if rec := postMultipart(t, s, s.Alice, path, form, "", nil); rec.Code != http.StatusSeeOther {
		t.Fatalf("remove = %d", rec.Code)
	}
	if _, err := s.Store.Cover(context.Background(), s.Alice.User.ID, id); !errors.Is(err, books.ErrNotFound) {
		t.Errorf("after remove: %v, want no cover", err)
	}
}

func TestSomeoneElsesBookIsNotFetchedFor(t *testing.T) {
	s, ol := newServerWithOL(t)
	id := add(t, s, s.Alice.User.ID, titled("Piranesi", "", books.ShelfWant))
	form := details("Mine now")
	form.Set("cover_url", ol+"/b/id/10226290-M.jpg")
	if rec := postMultipart(t, s, s.Bob, fmt.Sprintf("/books/edit/%d", id), form, "", nil); rec.Code != http.StatusNotFound {
		t.Errorf("Bob = %d, want 404", rec.Code)
	}
	if coverSource(t, s, id) != "" {
		t.Error("Bob's request stored a cover on Alice's book")
	}
}
```

`coverSource` needs the database: add to `internal/apps/books/export_test.go`:

```go
// DBForTest is the store's handle, for tests that check a column no store
// method returns.
func (st *Store) DBForTest() *sql.DB { return st.db }
```
(with `"database/sql"` added to its imports).

- [ ] **Step 2: Run them to see them fail**

Run: `go test ./internal/apps/books/... -run 'Cover' -count=1`
Expected: FAIL — no enctype, no cover fields, no stored cover.

- [ ] **Step 3: Add the cover change to `covers.go`**

Add `"io"` and `"strings"` to its imports, and append:

```go
// fetchCover fetches an image someone pasted the address of, under the
// same guard as every other fetch (spec "Errors").
func (a *App) fetchCover(ctx context.Context, rawURL string) (string, []byte, error) {
	ctx, cancel := context.WithTimeout(ctx, coverTimeout)
	defer cancel()
	ct, data, err := a.web.GetImage(ctx, rawURL, MaxCoverBytes)
	if err != nil {
		return "", nil, err
	}
	if !coverType(ct) {
		return "", nil, errNotACover
	}
	return ct, data, nil
}

// coverChange is what the edit form asks of a cover: a new image (Source
// set), removal, or nothing.
type coverChange struct {
	ContentType string
	Bytes       []byte
	Source      string
	Remove      bool
}

var tooBigCover = "That image is larger than " + strconv.Itoa(MaxCoverBytes>>20) + " MB."

// readCoverChange reads the edit form's cover fields. An uploaded file wins
// over an image address, which wins over Remove. A message is for the
// person, and means nothing should be saved.
func (a *App) readCoverChange(r *http.Request) (coverChange, string) {
	file, header, err := r.FormFile("cover_file")
	switch {
	case err == nil:
		defer func() { _ = file.Close() }()
		if header.Size > 0 {
			return readUploadedCover(file, header.Size)
		}
	case !errors.Is(err, http.ErrMissingFile) && !errors.Is(err, http.ErrNotMultipart):
		return coverChange{}, "That upload could not be read."
	}
	if raw := strings.TrimSpace(r.PostFormValue("cover_url")); raw != "" {
		ct, data, err := a.fetchCover(r.Context(), raw)
		if err != nil {
			a.deps.Log.Info("books cover address failed", "error", err)
			return coverChange{}, "Couldn't get a JPEG, PNG, GIF or WebP image from that address."
		}
		return coverChange{ContentType: ct, Bytes: data, Source: CoverFromURL}, ""
	}
	return coverChange{Remove: r.PostFormValue("remove_cover") == "1"}, ""
}

// readUploadedCover checks an uploaded file by its bytes, not its name or
// declared type.
func readUploadedCover(f io.Reader, size int64) (coverChange, string) {
	if size > MaxCoverBytes {
		return coverChange{}, tooBigCover
	}
	data, err := io.ReadAll(io.LimitReader(f, MaxCoverBytes+1))
	if err != nil {
		return coverChange{}, "That upload could not be read."
	}
	if len(data) > MaxCoverBytes {
		return coverChange{}, tooBigCover
	}
	ct := http.DetectContentType(data)
	if !coverType(ct) {
		return coverChange{}, "That file isn't a JPEG, PNG, GIF or WebP image."
	}
	return coverChange{ContentType: ct, Bytes: data, Source: CoverUpload}, ""
}

// applyCoverChange stores what readCoverChange read.
func (a *App) applyCoverChange(ctx context.Context, userID, id int64, ch coverChange) error {
	switch {
	case ch.Source != "":
		return a.store.SetCover(ctx, userID, id, ch.ContentType, ch.Bytes, ch.Source)
	case ch.Remove:
		return a.store.RemoveCover(ctx, userID, id)
	}
	return nil
}
```

- [ ] **Step 4: Raise the edit route's body limit**

In `books.go`, add above `Mount`:

```go
// editFormMaxBytes is the edit form's body budget: one cover plus the
// form's text fields and multipart overhead (the suite default is 1 MiB).
const editFormMaxBytes = MaxCoverBytes + 1<<20
```
and in `Mount`, just before `r.HandleFunc("POST /edit/{id}", a.update)`:

```go
	r.RegisterBodyLimit("POST /edit/{id}", editFormMaxBytes)
```

- [ ] **Step 5: Update `form.go`**

In `formValues`, add `CoverURL string` (a line of its own after `OLWork, OLEdition, CoverID string`, comment: `// the edit form's image address, echoed back on an error`). In `parseForm`'s `formValues{…}` add `CoverURL: get("cover_url"),`.

In `formView`, add after `Search searchView`:

```go
	// Cover is the edit form's current cover ("" draws Spine instead).
	Cover string
	Spine string
```

Replace `editBookForm`, `editForm` and `update` with:

```go
func editBookForm(id int64, v formValues, errs FieldErrors, c listCtx, cover string) formView {
	return formView{Action: "/books/edit/" + strconv.FormatInt(id, 10), Heading: "Edit book", Submit: "Save",
		Cancel: c.BookURL(id), Values: v, Errors: errs, Ctx: c, Cover: cover, Spine: SpineColor(v.Title)}
}

func (a *App) editForm(w http.ResponseWriter, r *http.Request) {
	uid, ok := a.userID(w, r)
	if !ok {
		return
	}
	id, ok := a.pathID(w, r)
	if !ok {
		return
	}
	b, err := a.store.Get(r.Context(), uid, id)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	a.renderForm(w, r, http.StatusOK, editBookForm(id, valuesOf(b.BookInput), nil, ctxFrom(r.FormValue), coverURL(b.ID, b.CoverVersion)))
}

// update saves the edit form. The owner check comes first, before any
// cover address is fetched; a bad cover is a 422 like any other field, and
// then nothing is saved.
func (a *App) update(w http.ResponseWriter, r *http.Request) {
	uid, ok := a.userID(w, r)
	if !ok {
		return
	}
	id, ok := a.pathID(w, r)
	if !ok {
		return
	}
	b, err := a.store.Get(r.Context(), uid, id)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	c := ctxFrom(r.PostFormValue)
	in, vals, errs := parseForm(r.PostFormValue)
	errs = merge(errs, in.Normalize().Validate())
	change, msg := a.readCoverChange(r)
	if msg != "" {
		errs["cover"] = msg
	}
	if len(errs) > 0 {
		a.renderForm(w, r, http.StatusUnprocessableEntity, editBookForm(id, vals, errs, c, coverURL(b.ID, b.CoverVersion)))
		return
	}
	if err := a.store.Update(r.Context(), uid, id, in); err != nil {
		a.fail(w, r, err)
		return
	}
	if err := a.applyCoverChange(r.Context(), uid, id, change); err != nil {
		a.fail(w, r, err)
		return
	}
	http.Redirect(w, r, c.BookURL(id), http.StatusSeeOther)
}
```

- [ ] **Step 6: Update `form.html`**

Change the book form's opening tag to

```html
	<form class="books-form stack" method="post" action="{{$d.Action}}"{{if not $d.New}} enctype="multipart/form-data"{{end}} novalidate>
```
and, after the Description `<div class="field">…</div>`, add:

```html
		{{if not $d.New}}
		<fieldset class="field books-cover-field"{{if index $d.Errors "cover"}} aria-describedby="books-cover-error"{{end}}>
			<legend>Cover</legend>
			<div class="books-cover-edit">
				{{if $d.Cover}}<img class="books-cover" src="{{$d.Cover}}" alt="The current cover" width="96" height="144">{{else}}<div class="books-spine swatch-c-{{$d.Spine}}" aria-hidden="true"><span>{{$d.Values.Title}}</span></div>{{end}}
				<div class="stack">
					<div>
						<label for="books-cover_file">Upload an image</label>
						<input id="books-cover_file" name="cover_file" type="file" accept="image/jpeg,image/png,image/gif,image/webp">
					</div>
					<div>
						<label for="books-cover_url">…or paste the address of one</label>
						<input id="books-cover_url" name="cover_url" type="url" placeholder="https://…" value="{{$d.Values.CoverURL}}">
					</div>
					{{if $d.Cover}}<label class="books-choice"><input type="checkbox" name="remove_cover" value="1"> Remove the cover</label>{{end}}
				</div>
			</div>
			{{template "field-error" (dict "Name" "cover" "Error" (index $d.Errors "cover"))}}
		</fieldset>
		{{end}}
```

- [ ] **Step 7: Add the CSS**

Append to the ON Books section of `internal/ui/static/app.css`:

```css
/* Edit page: the cover */
.books-cover-edit { display: flex; flex-wrap: wrap; align-items: flex-start; gap: var(--s-4); }
.books-cover-edit > .stack { flex: 1 1 14rem; min-width: 0; }
.books-cover-edit input[type="url"] { width: 100%; }
```

- [ ] **Step 8: Run the tests, then the full check**

Run: `go test ./internal/apps/books/... -count=1`
Expected: PASS (B1a's edit tests too: they post urlencoded forms, which `readCoverChange` treats as "no cover change"). Then the full check from Global Constraints.

- [ ] **Step 9: Commit**

```bash
git add -A internal/apps/books internal/ui/static/app.css
git commit -m "feat(books): upload a cover, take one from an address, or remove it (#487)"
```

---

### Task 7: User guide and spec

**Files:**
- Modify: `docs/user/books.md`, `docs/superpowers/specs/2026-10-09-on-books-design.md`

- [ ] **Step 1: Update the guide**

In `docs/user/books.md`, replace the whole "## Adding a book" section (up to "## Reading a book") with:

```markdown
## Adding a book

Click **Add book** at the top of the sidebar.

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

Then choose where the book goes:

- **Want to read** — it waits on that shelf.
- **Reading now** — you started it today.
- **Already read** — pick the day you finished it.

You can add tags at the same time, separated by commas.

## Covers

A book added from Open Library brings its cover. A book without one shows
a coloured spine with its title instead.

To change a cover, open the book, choose **⋯ → Edit details**, and under
**Cover** either upload an image or paste the address of one on the web.
JPEG, PNG, GIF and WebP images up to 2 MB work. **Remove the cover** goes
back to the spine. ON Books keeps its own copy of every cover, so they show
even if the original disappears.

```
(Keep the rest of the guide as it is.)

- [ ] **Step 2: Update the spec**

In `docs/superpowers/specs/2026-10-09-on-books-design.md`:

1. Replace the whole "### Add book" subsection (its two paragraphs) with:

```markdown
### Add book

The Add book page (`/books/new`) starts with **Find it on Open Library**: a
plain search box (title, author or ISBN). Results come back on the page as
rows — cover thumbnail, title, author, year, **Use this**. **Use this**
reloads the page with the book form pre-filled from the result (and the
work's description); the person checks it and picks a shelf: *Want to
read* / *Reading now* / *Already read*. Saving creates the book (and a
reading if needed) and fetches its cover. The manual form is always right
there under the search; when Open Library fails or finds nothing, the page
says so and starts the form with what was typed.

Thumbnails in search results are proxied through `/books/olcover/{coverID}`:
Open Library's cover URLs redirect to archive.org storage hosts, which a
CSP would have to allow by wildcard, so the suite's `img-src 'self'` stays
as it is. Only digits reach the proxy, never a URL.

(Decided 2026-10-09 while planning B1b: search on the page rather than in a
dialog, and proxied thumbnails rather than a wider CSP.)
```

2. In the "Phases" table, replace the B1b row with:

```markdown
| B1b | #487 Library and shelves (part 2) | Open Library search on the Add book page; covers (`books_covers`): fetched on save from a pick, upload, image address, remove; covers in the list and book pane; thumbnail proxy |
```

- [ ] **Step 3: Run the full check and commit**

Run the full check from Global Constraints (the help tests load the guide).

```bash
git add docs
git commit -m "docs(books): Open Library search and covers in the guide and spec (#487)"
```

---

### Task 8: Check it in a browser and open the PR

- [ ] **Step 1: Seed a demo directory and start the server**

```bash
SEED=$(mktemp -d)/books-b1b
go run ./docs/screenshots/seed --data-dir $SEED
go build -o $SEED/onsuite ./cmd/onsuite
echo $SEED
```
Add a `.claude/launch.json` configuration named `onsuite-books` (in the main checkout) with `"runtimeExecutable": "<SEED>/onsuite"`, `"runtimeArgs": ["serve", "--addr", ":8096", "--data-dir", "<SEED>"]` and `"port": 8096`, start it with the preview tools and sign in as `demo` (password in `docs/screenshots/README.md`). This uses the real Open Library.

- [ ] **Step 2: Walk through it**

1. Add book → search "piranesi": results with thumbnails (served from `/books/olcover/…`, no CSP errors in the console).
2. Use this → the form is pre-filled, description included, "From Open Library" note with the cover; Add book → the book opens with its cover; the list row shows the mini cover.
3. Search by ISBN (`9781635575637`) → Piranesi.
4. Search for nonsense → "has nothing for …" and the title is pre-filled.
5. Edit details → upload a PNG/JPEG → the new cover shows (and is not a cached old one); paste an image address → it shows; Remove → the spine is back.
6. Upload a text file → the message by Cover, nothing changed.
7. Phone width: the search results and the edit page's cover section don't overflow.

Fix anything found (with a test where one can be written), re-run the full check, commit.

- [ ] **Step 3: Remove the launch entry and push**

Remove the `onsuite-books` entry from `.claude/launch.json` (do not commit it), stop the server, then:

```bash
git push -u origin feat/books-b1b-openlibrary
env -u GH_TOKEN gh pr create --title "feat(books): ON Books B1b — Open Library search and covers (#487)" --body "$(cat <<'EOF'
Second half of #487 (B1b). Find a book on Open Library from the Add book page and add it pre-filled, with its cover; covers in the list and the book pane; upload a cover, take one from an image address, or remove it. Thumbnails are proxied, so the suite's CSP is unchanged.

Spec: docs/superpowers/specs/2026-10-09-on-books-design.md
Plan: docs/superpowers/plans/2026-10-09-on-books-b1b-openlibrary.md
Closes #487.
EOF
)"
```
Never merge it.
