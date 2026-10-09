# ON Books B3 — Notes and quotes Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let people keep dated notes and quotes on a book, edit and delete them in place, and find any book by what they wrote about it: the list's filter box becomes full-text search over titles, authors, series, reviews, notes, quotes and comments, with a third line on a matching row showing where it matched.

**Architecture:** Two new tables, `books_notes` and `books_quotes` (migration 0004), scoped through their book and deleted with it; their store methods run in one transaction that is also the owner check and the page check (`inBook`). One FTS5 table, `books_search` (migration 0005), a regular table with its own copy of the text kept in step by triggers, as ON Later's `later_search` is; `List` joins it when the filter is set, keeps the shelf's own order, and returns a snippet of the first review/note/quote/comment that matched. The book pane gets a Notes section and a Quotes section above the reading history, each with a "+ Add" disclosure and an Edit disclosure and Delete on every entry. All of it is plain form posts that work without JavaScript; with htmx a form swaps only its own section, with the list out of band (`notes-swap`, `quotes-swap`), and a refused form comes back open inside that section with its message and what was typed.

**Tech Stack:** Go 1.22+ `ServeMux`, `html/template`, htmx 2, SQLite FTS5 via `modernc.org/sqlite`.

**Spec:** [docs/superpowers/specs/2026-10-09-on-books-design.md](../specs/2026-10-09-on-books-design.md) — "Data model" (`books_notes`, `books_quotes`, `books_search`), "Screens → Layout / Book pane / Notes, quotes and search / Keyboard", "Errors", "Phases" (B3 row). Builds on B2 ([plan](2026-10-09-on-books-b2-progress-history.md), #577). Issue #489.

## Global Constraints

Decisions Ilia made on 2026-10-09 (binding; already recorded in the spec by this plan's own PR — "Notes, quotes and search"):

- **One B3 PR.** The tasks below are separate commits on branch `feat/books-b3-notes-quotes` in the worktree `../on-suite-books-b3`.
- **Edit and delete inline.** Each note and quote has an Edit `<details>` disclosure, like B2's reading history, and a Delete button that goes through the existing confirm dialog (the #551 dialog pattern, `hx-confirm`). Everything works without JavaScript: plain form posts with a real `action` and an identical `hx-post`.
- **Search snippet line.** When the filter matches inside a review, note, quote or quote comment, the list row gets a third line with a label and the highlighted words ("Quote: The spice must flow."), rendered as parts so html/template escapes every character (Later's approach). A match only on title, subtitle, authors or series shows no snippet. Search applies within the current shelf, tag and series, as the old `Q` filter did. Prefix matching works while a word is still being typed. **The shelf's own sort order is kept** (no relevance ranking): filtering never reshuffles a shelf.
- **Stacked sections** in the book pane, in the spec's order: Notes (dated entries, newest first), then Quotes as cards, then Reading history. Each section has a "+ Add note" / "+ Add quote" disclosure. Notes show their date and optional page ("p. 112").

Defaults chosen while planning — **ask Ilia to confirm** (each is a one-line change if he wants another):

- Note bodies render with the existing `RenderReview` (inline Markdown plus paragraphs).
- Quote text is plain text with its line breaks kept (`white-space: pre-line`); quote comments render with `RenderReview`.
- Quotes are listed newest first, like notes (not in page order).
- Keyboard: `n` opens the add-note box and puts the cursor in it; `q` does the same for the add-quote box.
- Length caps: `MaxNoteRunes = 20000` (as `MaxReviewRunes`); `MaxQuoteRunes = 5000` for a quote's text and, separately, its comment.
- A page is a whole number from 1 to the book's page count, or any positive number when the book has none; empty means no page. Anything else ("0", "-3", "2.5", "abc") is refused inline: "Enter a page from 1 to 600." / "Enter a page number of 1 or more." A note or quote with no text is refused inline: "Write something in the note first." / "Type the quote first."
- Snippet labels: "Quote:", "Quote comment:", "Note:", "Review:"; when several match, the first in that order is shown. A snippet is up to 12 tokens.
- With htmx, a note or quote form swaps only its section (and the list out of band) rather than the whole panes, so the book pane keeps its scroll position for these forms (see #578 below).
- A snippet shows a note's or review's Markdown as typed — "The *protomolecule*." keeps its asterisks — as Later's snippets show text as stored. Stripping Markdown from snippets would be a follow-up.
- Deleting or editing the note or quote a filter matched updates the list out of band, so the open book can drop out of a filtered list while it stays open in the book pane.

From the spec:

- `books_notes` (`id`, `book_id`, `page` nullable, `body` Markdown, `created_at`, `updated_at`) and `books_quotes` (`id`, `book_id`, `page` nullable, `text`, `comment`, `created_at`, `updated_at`), both scoped through their book and deleted with it (`ON DELETE CASCADE`). Timestamps through `formatTime` (`db.FormatTime`).
- `books_search` is FTS5 over title, subtitle, authors, series name, review, note bodies, quote text and comments, kept in step in the same transactions that change those fields — triggers, as in Later (`internal/apps/later/migrations/0006_search.sql`).
- The list filter box becomes full-text. `ftsQuery` and `snippetParts` are **mirrored** from ON Later with a comment saying so; apps never import each other (`internal/arch` enforces it).
- Adding, editing or deleting a note or quote is a change to the book: it bumps `updated_at` (DNF / All sort by the latest change).
- Missing or someone else's book, note or quote → 404, as everywhere; a bad `{nid}`/`{qid}` path segment → 404.
- **B5 owns export and the admin card.** `books` implements neither `Exporter` nor `Stater` today, so B3 adds nothing there; `export_test.go` holds test hooks only and needs nothing.
- CSP: no inline `<script>`, no `style=""`. App CSS goes in the "ON Books" section of `internal/ui/static/app.css`, classes prefixed `books-`. Every new POST form has a real `action` and an identical `hx-post` (PATTERNS.md).
- Full check must stay green on every commit:
  ```bash
  gofmt -l .
  go vet ./...
  go run honnef.co/go/tools/cmd/staticcheck@v0.8.0 ./...
  go mod tidy && git diff --exit-code go.mod go.sum
  go test ./... -race -count=1
  ```
- Open the PR with `env -u GH_TOKEN gh …`; never merge.

## Open issues in the ON Books milestone that touch B3

None is fixed here; the plan only avoids making them worse.

- **#578** (saving in the book pane scrolls it back to the top). Notes and quotes sit low in the pane, so a whole-panes swap would hit this on every save. Their forms swap only their own section instead (`notes-swap`, `quotes-swap`), so B3 doesn't add to it. Delete buttons do the same. The older controls (stars, review, tags, format, history) still swap the panes.
- **#569** (B1a test gaps) lists "Tag + Q combined" and "the '_' LIKE escape". B3 replaces the LIKE filter with FTS (and removes `likeEscape`), so the LIKE-escape item goes away and Tag + Q is covered by `TestFilterStaysWithinTheList`. Update #569 after merging.
- #566 (collapse long descriptions) and #579 (stars wrap) are about the same pane but don't collide.

## Lessons from earlier plans (read before starting)

- staticcheck U1000 fails on an unexported helper (or struct field) added before its first use — every helper here comes in the task that first uses it. That is why `entryDraft`/`entryForm` gain `Comment` only in Task 5, and why `likeEscape` is deleted in the same task (Task 2) that stops using it. staticcheck S1016 wants a type conversion instead of a struct literal copying identical fields — `listCtx` and `ListQuery` keep the same fields in the same order (`ListQuery(c)`); B3 doesn't touch either.
- `internal/htmlassert` supports one qualifier per selector: `#books-list .is-active` works, `.books-row.is-active` does not, and neither does `#books-list[hx-swap-oob="true"]` (the trial hit exactly this: "no element matches"). Use `htmlassert.Attr` for a second condition. Descendant selectors and a single `[attr="value"]` work.
- `go vet` rejects unkeyed composite literals of another package's struct types in `_test` packages — every `books.NoteInput{…}` / `books.QuoteInput{…}` here is keyed.
- `TestMain` pins `time.Local` to Melbourne for this package; the fixture's default clock is already the next day there (10 October). Note dates are shown in the local day, so tests that check a date set the clock explicitly.
- When order depends on timestamps (newest-first notes and quotes), advance the pinned clock between steps — equal timestamps fall back to the id.
- Inside `{{range}}` in a template, `$` is the template's data — in the `notes`/`quotes` templates that is the bookView, which is what `post-ctx` and the action URLs need.
- `docs/screenshots/seed` is compiled by `go vet ./...` and tested by `go test ./...`: Task 6 changes it, and nothing earlier changes a signature it calls.
- gofmt re-aligns a whole struct when a longer field joins it; where that happens the plan shows the whole struct (`paneOpts`, `entryDraft`, `entryForm`, `ListItem`'s tail).
- FTS5 tables are named, never aliased, in queries using `MATCH` and `snippet()` (Later's lesson); `searchJoin` follows it.
- `snippet()` returns a column's opening words even when that column didn't match, so a column matched only if its snippet holds `SnippetOpen` (Later's lesson; `matchOf`).
- The FTS prefix filter is word-based: "100%" now matches "100% Real" **and** "1000 Years", and a match no longer happens mid-word ("guin" matches "Guin", "uin" doesn't). Task 2 updates the one existing test that relied on the LIKE behaviour (`TestListFiltersByTagAndText`).
- This plan's code was trial-run on 2026-10-09. It was written and tested task by task in a scratch worktree; then a script applied every "Create" and "replace … with" block of this document, in order, to a fresh worktree from the plan branch. Each task's tests, `go vet ./...` and staticcheck passed at its own commit; each Step 2 failed with the messages written there; the full check passed at the end; and the result was identical to the scratch tree. A browser walk through the seeded demo (Task 8's list, desktop and phone width) found nothing to fix. What the trial changed: the out-of-band list check uses `htmlassert.Attr` (a two-qualifier selector matched nothing); the "100%" case of `TestListFiltersByTagAndText` now expects the prefix match; the book's Delete confirm no longer says only "readings and tags"; the walkthrough searches "jane", because the demo note on Persuasion mentions "Austen's" and so an "austen" search shows a Note snippet.

## File map

| File | Responsibility |
|---|---|
| `internal/apps/books/migrations/0004_notes_quotes.sql` | `books_notes`, `books_quotes` |
| `internal/apps/books/notes.go` | `MaxNoteRunes`, `MaxQuoteRunes`, `Note`, `NoteInput`, `cleanText`, `checkLength`, `checkPage`, `inBook`, `execOne`, `checkNote`, `AddNote`, `UpdateNote`, `DeleteNote`, `Notes` |
| `internal/apps/books/quotes.go` | `Quote`, `QuoteInput`, `checkQuote`, `AddQuote`, `UpdateQuote`, `DeleteQuote`, `Quotes` |
| `internal/apps/books/migrations/0005_search.sql` | `books_search` (FTS5), backfill, triggers |
| `internal/apps/books/search.go` | `ftsQuery` (mirrors Later's), `MatchIn`, `SnippetOpen`/`SnippetClose`, `searchJoin`, `snippetCols`, `noSnippets`, `matchOf` |
| `internal/apps/books/library.go` | `ListItem.Match`/`Snippet`; `List` searches `books_search`; `likeEscape` goes |
| `internal/apps/books/snippet.go` | `snippetParts` (mirrors Later's), `snippetView`, `matchLabels`, `newSnippet` |
| `internal/apps/books/view.go` | `rowView.Snippet`; `entryDraft`, `entryForm`, `formFor`, `noteView`, `viewNotes`, `quoteView`, `viewQuotes`, `pageText`, `pageValue`; `bookView.Pages/Notes/NewNote/Quotes/NewQuote` |
| `internal/apps/books/handlers.go` | `paneOpts.Draft`; `renderPanes` loads notes and quotes, `notes-swap`/`quotes-swap`, 422 for a refused form |
| `internal/apps/books/actions.go` | `childID` (replaces `readingID`), `pageField`, `entrySave`, `saveEntry`, `noteDraft`, `addNote`, `editNote`, `deleteNote`, `quoteDraft`, `addQuote`, `editQuote`, `deleteQuote` |
| `internal/apps/books/books.go` | routes |
| `internal/apps/books/templates/panes.partial.html` | snippet line; filter box wording; `notes`, `note-form`, `notes-swap`, `quotes`, `quote-form`, `quotes-swap`; delete-book confirm text |
| `internal/apps/books/static/books.js` | `openEntry`; `n` and `q` |
| `internal/ui/static/app.css` | snippet line; notes; quote cards |
| `internal/apps/books/*_test.go` | tests |
| `docs/screenshots/seed/books.go`, `seed_test.go` | demo notes and quotes (quotes only from a public-domain book) |
| `docs/user/books.md`, `AGENTS.md` | guide, app list |

---

### Task 0: Worktree and branch

- [ ] **Step 1: Create the worktree**

```bash
cd /Users/iliaf/src/WEB/on-suite
git fetch origin
git worktree add ../on-suite-books-b3 -b feat/books-b3-notes-quotes origin/main
cd ../on-suite-books-b3
go build ./cmd/onsuite && rm -f onsuite
```
Expected: builds with no output. All later commands run in `../on-suite-books-b3`. `origin/main` must already include this plan's PR (it carries the spec's "Notes, quotes and search" section that Task 7's guide text matches).

---

### Task 1: Notes and quotes in the store

**Files:**
- Create: `internal/apps/books/migrations/0004_notes_quotes.sql`, `internal/apps/books/notes.go`, `internal/apps/books/quotes.go`
- Test: `internal/apps/books/notes_test.go`

**Interfaces:**
- Consumes: `touch`, `formatTime`, `parseTime`, `Refusal`, `ErrNotFound` (store.go); `nullInt` (library.go); test helpers `newFixture`, `addBook`, `onShelf`, `getBook` (store_test.go, library_test.go).
- Produces:
  - `const MaxNoteRunes = 20000`, `const MaxQuoteRunes = 5000`
  - `type Note struct { ID int64; Page int; Body string; CreatedAt, UpdatedAt time.Time }`; `type NoteInput struct { Page int; Body string }`
  - `type Quote struct { ID int64; Page int; Text, Comment string; CreatedAt, UpdatedAt time.Time }`; `type QuoteInput struct { Page int; Text, Comment string }`
  - `(*Store).AddNote(ctx, userID, bookID int64, in NoteInput) (int64, error)`, `UpdateNote(ctx, userID, bookID, noteID int64, in NoteInput) error`, `DeleteNote(ctx, userID, bookID, noteID int64) error`, `Notes(ctx, userID, bookID int64) ([]Note, error)` (newest first)
  - `(*Store).AddQuote(ctx, userID, bookID int64, in QuoteInput) (int64, error)`, `UpdateQuote(ctx, userID, bookID, quoteID int64, in QuoteInput) error`, `DeleteQuote(ctx, userID, bookID, quoteID int64) error`, `Quotes(ctx, userID, bookID int64) ([]Quote, error)` (newest first)
  - Page 0 is "no page"; a negative page is refused (`*Refusal`); so is a page past the book's page count. Text is trimmed and its line endings tidied (`cleanText`), line breaks inside kept.
  - test helpers: `withPages(t, f, title, pages) int64` (Alice's Want to read book), `notes(t, f, userID, id) []books.Note`, `quotes(t, f, userID, id) []books.Quote`, `wantRefusal(t, what string, err error, msg string)`

- [ ] **Step 1: Write the failing tests**

Create `internal/apps/books/notes_test.go`:

```go
package books_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/iliafrenkel/on-suite/internal/apps/books"
)

// withPages is one of Alice's Want to read books with a page count (0:
// unknown).
func withPages(t *testing.T, f *fixture, title string, pages int) int64 {
	t.Helper()
	nb := onShelf(title, books.ShelfWant)
	nb.Pages = pages
	return addBook(t, f, f.alice.ID, nb)
}

func notes(t *testing.T, f *fixture, userID, id int64) []books.Note {
	t.Helper()
	ns, err := f.store.Notes(context.Background(), userID, id)
	if err != nil {
		t.Fatal(err)
	}
	return ns
}

func quotes(t *testing.T, f *fixture, userID, id int64) []books.Quote {
	t.Helper()
	qs, err := f.store.Quotes(context.Background(), userID, id)
	if err != nil {
		t.Fatal(err)
	}
	return qs
}

// wantRefusal fails the test unless err is a Refusal saying msg.
func wantRefusal(t *testing.T, what string, err error, msg string) {
	t.Helper()
	var ref *books.Refusal
	if !errors.As(err, &ref) || ref.Msg != msg {
		t.Errorf("%s = %v, want a Refusal %q", what, err, msg)
	}
}

func TestNotesAreDatedAndNewestFirst(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	id := withPages(t, f, "Dune", 600)
	first := f.now
	if _, err := f.store.AddNote(ctx, f.alice.ID, id, books.NoteInput{Page: 112, Body: "  The *spice*.\r\nAgain.  "}); err != nil {
		t.Fatal(err)
	}
	f.now = f.now.Add(time.Hour)
	if _, err := f.store.AddNote(ctx, f.alice.ID, id, books.NoteInput{Body: "No page."}); err != nil {
		t.Fatal(err)
	}
	ns := notes(t, f, f.alice.ID, id)
	if len(ns) != 2 {
		t.Fatalf("%d notes, want 2", len(ns))
	}
	if ns[0].Body != "No page." || ns[0].Page != 0 || !ns[0].CreatedAt.Equal(f.now) {
		t.Errorf("newest note = %+v", ns[0])
	}
	if ns[1].Body != "The *spice*.\nAgain." || ns[1].Page != 112 || !ns[1].CreatedAt.Equal(first) {
		t.Errorf("first note = %+v, want its text tidied and page 112", ns[1])
	}
	if b := getBook(t, f, f.alice.ID, id); !b.UpdatedAt.Equal(f.now) {
		t.Errorf("book updated_at = %v, want %v: a note is a change", b.UpdatedAt, f.now)
	}
	if got := notes(t, f, f.bob.ID, id); len(got) != 0 {
		t.Errorf("Bob sees %d of Alice's notes", len(got))
	}
}

func TestNotesRefuseBadInput(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	dune := withPages(t, f, "Dune", 600)
	unknown := withPages(t, f, "Emma", 0)
	tests := []struct {
		name string
		id   int64
		in   books.NoteInput
		want string
	}{
		{"empty", dune, books.NoteInput{Body: " \n "}, "Write something in the note first."},
		{"too long", dune, books.NoteInput{Body: strings.Repeat("x", books.MaxNoteRunes+1)}, "Keep your note to 20000 characters or fewer."},
		{"page past the end", dune, books.NoteInput{Page: 601, Body: "x"}, "Enter a page from 1 to 600."},
		{"not a page", dune, books.NoteInput{Page: -1, Body: "x"}, "Enter a page from 1 to 600."},
		{"not a page, no page count", unknown, books.NoteInput{Page: -1, Body: "x"}, "Enter a page number of 1 or more."},
	}
	for _, tt := range tests {
		_, err := f.store.AddNote(ctx, f.alice.ID, tt.id, tt.in)
		wantRefusal(t, tt.name, err, tt.want)
	}
	if n := len(notes(t, f, f.alice.ID, dune)); n != 0 {
		t.Errorf("%d notes saved by refused adds", n)
	}
	if _, err := f.store.AddNote(ctx, f.alice.ID, unknown, books.NoteInput{Page: 5000, Body: "x"}); err != nil {
		t.Errorf("any page of a book with no page count = %v, want it accepted", err)
	}
	if _, err := f.store.AddNote(ctx, f.alice.ID, dune, books.NoteInput{Page: 600, Body: "x"}); err != nil {
		t.Errorf("the last page = %v, want it accepted", err)
	}
	if _, err := f.store.AddNote(ctx, f.bob.ID, dune, books.NoteInput{Body: "x"}); !errors.Is(err, books.ErrNotFound) {
		t.Errorf("Bob adding a note to Alice's book = %v, want ErrNotFound", err)
	}
}

func TestEditingAndDeletingANote(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	dune := withPages(t, f, "Dune", 600)
	emma := withPages(t, f, "Emma", 300)
	nid, err := f.store.AddNote(ctx, f.alice.ID, dune, books.NoteInput{Page: 10, Body: "Draft."})
	if err != nil {
		t.Fatal(err)
	}
	added := f.now
	f.now = f.now.Add(time.Hour)
	if err := f.store.UpdateNote(ctx, f.alice.ID, dune, nid, books.NoteInput{Body: "Better."}); err != nil {
		t.Fatal(err)
	}
	n := notes(t, f, f.alice.ID, dune)[0]
	if n.Body != "Better." || n.Page != 0 || !n.CreatedAt.Equal(added) || !n.UpdatedAt.Equal(f.now) {
		t.Errorf("edited note = %+v, want new text, no page, the old date", n)
	}
	err = f.store.UpdateNote(ctx, f.alice.ID, dune, nid, books.NoteInput{Body: ""})
	wantRefusal(t, "emptying a note", err, "Write something in the note first.")

	for _, tt := range []struct {
		name         string
		user, bookID int64
	}{
		{"Bob", f.bob.ID, dune},
		{"another book", f.alice.ID, emma},
	} {
		if err := f.store.UpdateNote(ctx, tt.user, tt.bookID, nid, books.NoteInput{Body: "x"}); !errors.Is(err, books.ErrNotFound) {
			t.Errorf("update via %s = %v, want ErrNotFound", tt.name, err)
		}
		if err := f.store.DeleteNote(ctx, tt.user, tt.bookID, nid); !errors.Is(err, books.ErrNotFound) {
			t.Errorf("delete via %s = %v, want ErrNotFound", tt.name, err)
		}
	}
	if err := f.store.DeleteNote(ctx, f.alice.ID, dune, nid); err != nil {
		t.Fatal(err)
	}
	if n := len(notes(t, f, f.alice.ID, dune)); n != 0 {
		t.Errorf("%d notes after deleting the only one", n)
	}
}

func TestQuotesKeepTheirLineBreaks(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	id := withPages(t, f, "Dune", 600)
	if _, err := f.store.AddQuote(ctx, f.alice.ID, id, books.QuoteInput{Page: 8,
		Text: "I must not fear.\r\nFear is the mind-killer.\n", Comment: " The litany. "}); err != nil {
		t.Fatal(err)
	}
	f.now = f.now.Add(time.Hour)
	if _, err := f.store.AddQuote(ctx, f.alice.ID, id, books.QuoteInput{Text: "The spice must flow."}); err != nil {
		t.Fatal(err)
	}
	qs := quotes(t, f, f.alice.ID, id)
	if len(qs) != 2 {
		t.Fatalf("%d quotes, want 2", len(qs))
	}
	if qs[0].Text != "The spice must flow." || qs[0].Page != 0 || qs[0].Comment != "" {
		t.Errorf("newest quote = %+v", qs[0])
	}
	if qs[1].Text != "I must not fear.\nFear is the mind-killer." || qs[1].Page != 8 || qs[1].Comment != "The litany." {
		t.Errorf("first quote = %+v", qs[1])
	}
	if got := quotes(t, f, f.bob.ID, id); len(got) != 0 {
		t.Errorf("Bob sees %d of Alice's quotes", len(got))
	}
}

func TestQuotesRefuseBadInput(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	id := withPages(t, f, "Dune", 600)
	tests := []struct {
		name string
		in   books.QuoteInput
		want string
	}{
		{"no text", books.QuoteInput{Comment: "Only a comment."}, "Type the quote first."},
		{"text too long", books.QuoteInput{Text: strings.Repeat("x", books.MaxQuoteRunes+1)}, "Keep the quote to 5000 characters or fewer."},
		{"comment too long", books.QuoteInput{Text: "x", Comment: strings.Repeat("x", books.MaxQuoteRunes+1)}, "Keep your comment to 5000 characters or fewer."},
		{"page past the end", books.QuoteInput{Page: 601, Text: "x"}, "Enter a page from 1 to 600."},
	}
	for _, tt := range tests {
		_, err := f.store.AddQuote(ctx, f.alice.ID, id, tt.in)
		wantRefusal(t, tt.name, err, tt.want)
	}
	if n := len(quotes(t, f, f.alice.ID, id)); n != 0 {
		t.Errorf("%d quotes saved by refused adds", n)
	}
}

func TestEditingAndDeletingAQuote(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	dune := withPages(t, f, "Dune", 600)
	qid, err := f.store.AddQuote(ctx, f.alice.ID, dune, books.QuoteInput{Text: "Draft."})
	if err != nil {
		t.Fatal(err)
	}
	if err := f.store.UpdateQuote(ctx, f.alice.ID, dune, qid, books.QuoteInput{Page: 3, Text: "Better.", Comment: "Why."}); err != nil {
		t.Fatal(err)
	}
	if q := quotes(t, f, f.alice.ID, dune)[0]; q.Text != "Better." || q.Page != 3 || q.Comment != "Why." {
		t.Errorf("edited quote = %+v", q)
	}
	if err := f.store.UpdateQuote(ctx, f.bob.ID, dune, qid, books.QuoteInput{Text: "x"}); !errors.Is(err, books.ErrNotFound) {
		t.Errorf("Bob's update = %v, want ErrNotFound", err)
	}
	if err := f.store.DeleteQuote(ctx, f.bob.ID, dune, qid); !errors.Is(err, books.ErrNotFound) {
		t.Errorf("Bob's delete = %v, want ErrNotFound", err)
	}
	if err := f.store.DeleteQuote(ctx, f.alice.ID, dune, qid+1); !errors.Is(err, books.ErrNotFound) {
		t.Errorf("deleting a quote that isn't there = %v, want ErrNotFound", err)
	}
	if err := f.store.DeleteQuote(ctx, f.alice.ID, dune, qid); err != nil {
		t.Fatal(err)
	}
	if n := len(quotes(t, f, f.alice.ID, dune)); n != 0 {
		t.Errorf("%d quotes after deleting the only one", n)
	}
}

func TestDeletingABookTakesItsNotesAndQuotes(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	id := withPages(t, f, "Dune", 600)
	if _, err := f.store.AddNote(ctx, f.alice.ID, id, books.NoteInput{Body: "x"}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.AddQuote(ctx, f.alice.ID, id, books.QuoteInput{Text: "x"}); err != nil {
		t.Fatal(err)
	}
	if err := f.store.Delete(ctx, f.alice.ID, id); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"books_notes", "books_quotes"} {
		var n int
		if err := f.db.QueryRow(`SELECT count(*) FROM ` + table).Scan(&n); err != nil || n != 0 {
			t.Errorf("%s has %d rows after the book went, %v", table, n, err)
		}
	}
}
```

- [ ] **Step 2: Run them to see them fail**

Run: `go test ./internal/apps/books/... -count=1`
Expected: FAIL — the package doesn't build: `undefined: books.NoteInput`, `f.store.AddNote undefined`, and so on.

- [ ] **Step 3: The tables**

Create `internal/apps/books/migrations/0004_notes_quotes.sql`:

```sql
-- Notes and quotes (spec "Data model"): dated notes with an optional page,
-- and quotes copied out of the book with an optional page and comment.
-- Both are scoped through their book and go with it. page is NULL when not
-- given; the store keeps it between 1 and the book's page count. Quotes
-- are Books' own, not Later's highlights (spec "Quotes are not Later's
-- highlights").
CREATE TABLE books_notes (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    book_id    INTEGER NOT NULL REFERENCES books_books (id) ON DELETE CASCADE,
    page       INTEGER CHECK (page > 0),
    body       TEXT    NOT NULL CHECK (body <> ''),
    created_at TEXT    NOT NULL,
    updated_at TEXT    NOT NULL
) STRICT;

CREATE INDEX books_notes_book_idx ON books_notes (book_id, created_at);

CREATE TABLE books_quotes (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    book_id    INTEGER NOT NULL REFERENCES books_books (id) ON DELETE CASCADE,
    page       INTEGER CHECK (page > 0),
    text       TEXT    NOT NULL CHECK (text <> ''),
    comment    TEXT    NOT NULL DEFAULT '',
    created_at TEXT    NOT NULL,
    updated_at TEXT    NOT NULL
) STRICT;

CREATE INDEX books_quotes_book_idx ON books_quotes (book_id, created_at);
```

- [ ] **Step 4: Notes**

Create `internal/apps/books/notes.go`:

```go
package books

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

// MaxNoteRunes bounds a note; MaxQuoteRunes bounds a quote's text and its
// comment, each. Room for long ones, as MaxReviewRunes is for a review.
const (
	MaxNoteRunes  = 20000
	MaxQuoteRunes = 5000
)

// Note is a dated note on a book (spec "Data model": books_notes).
type Note struct {
	ID                   int64
	Page                 int    // 0 when not given
	Body                 string // Markdown
	CreatedAt, UpdatedAt time.Time
}

// NoteInput is a note as typed. Page 0 is no page; below 0 is a page the
// store refuses (the handler sends -1 for anything that isn't a positive
// whole number).
type NoteInput struct {
	Page int
	Body string
}

// cleanText tidies line endings and trims the ends, as SetReview does.
// Line breaks inside are kept.
func cleanText(s string) string { return strings.TrimSpace(strings.ReplaceAll(s, "\r\n", "\n")) }

// checkLength refuses text over max runes, naming it ("your note").
func checkLength(s string, max int, what string) error {
	if utf8.RuneCountInString(s) > max {
		return &Refusal{Msg: fmt.Sprintf("Keep %s to %d characters or fewer.", what, max)}
	}
	return nil
}

// checkPage accepts page 0 (none) or a page of the book: 1 to its page
// count, or any positive page when it has none (decided 2026-10-09).
func checkPage(ctx context.Context, tx *sql.Tx, bookID int64, page int) error {
	if page == 0 {
		return nil
	}
	var pages sql.NullInt64
	if err := tx.QueryRowContext(ctx, `SELECT pages FROM books_books WHERE id = ?`, bookID).Scan(&pages); err != nil {
		return fmt.Errorf("books: check page: %w", err)
	}
	switch {
	case pages.Int64 > 0 && (page < 1 || page > int(pages.Int64)):
		return &Refusal{Msg: fmt.Sprintf("Enter a page from 1 to %d.", pages.Int64)}
	case page < 1:
		return &Refusal{Msg: "Enter a page number of 1 or more."}
	}
	return nil
}

// inBook runs write in one transaction after the owner check (touch, which
// also marks the book changed) and the page check, so a note or quote
// never lands on someone else's book or on a page it doesn't have. now is
// the write's timestamp.
func (st *Store) inBook(ctx context.Context, userID, bookID int64, page int, write func(tx *sql.Tx, now string) error) error {
	tx, err := st.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("books: begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if err := st.touch(ctx, tx, userID, bookID); err != nil {
		return err
	}
	if err := checkPage(ctx, tx, bookID, page); err != nil {
		return err
	}
	if err := write(tx, formatTime(st.now())); err != nil {
		return err
	}
	return tx.Commit()
}

// execOne runs a statement that must change exactly one row: none is
// ErrNotFound — a note or quote that isn't on this book.
func execOne(ctx context.Context, tx *sql.Tx, query string, args ...any) error {
	res, err := tx.ExecContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("books: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// checkNote cleans a note and refuses an empty or over-long one.
func checkNote(in NoteInput) (NoteInput, error) {
	in.Body = cleanText(in.Body)
	if in.Body == "" {
		return in, &Refusal{Msg: "Write something in the note first."}
	}
	return in, checkLength(in.Body, MaxNoteRunes, "your note")
}

// AddNote adds a note to one of userID's books, dated now.
func (st *Store) AddNote(ctx context.Context, userID, bookID int64, in NoteInput) (int64, error) {
	in, err := checkNote(in)
	if err != nil {
		return 0, err
	}
	var id int64
	err = st.inBook(ctx, userID, bookID, in.Page, func(tx *sql.Tx, now string) error {
		res, err := tx.ExecContext(ctx,
			`INSERT INTO books_notes (book_id, page, body, created_at, updated_at) VALUES (?, ?, ?, ?, ?)`,
			bookID, nullInt(in.Page), in.Body, now, now)
		if err != nil {
			return fmt.Errorf("books: add note: %w", err)
		}
		id, err = res.LastInsertId()
		return err
	})
	return id, err
}

// UpdateNote changes a note's page and text; it keeps its date.
func (st *Store) UpdateNote(ctx context.Context, userID, bookID, noteID int64, in NoteInput) error {
	in, err := checkNote(in)
	if err != nil {
		return err
	}
	return st.inBook(ctx, userID, bookID, in.Page, func(tx *sql.Tx, now string) error {
		return execOne(ctx, tx, `UPDATE books_notes SET page = ?, body = ?, updated_at = ? WHERE id = ? AND book_id = ?`,
			nullInt(in.Page), in.Body, now, noteID, bookID)
	})
}

// DeleteNote removes a note for good.
func (st *Store) DeleteNote(ctx context.Context, userID, bookID, noteID int64) error {
	return st.inBook(ctx, userID, bookID, 0, func(tx *sql.Tx, _ string) error {
		return execOne(ctx, tx, `DELETE FROM books_notes WHERE id = ? AND book_id = ?`, noteID, bookID)
	})
}

// Notes is a book's notes, newest first (spec "Book pane"). A book that
// isn't userID's has none.
func (st *Store) Notes(ctx context.Context, userID, bookID int64) ([]Note, error) {
	rows, err := st.db.QueryContext(ctx, `
		SELECT n.id, n.page, n.body, n.created_at, n.updated_at
		  FROM books_notes n JOIN books_books b ON b.id = n.book_id
		 WHERE b.id = ? AND b.user_id = ?
		 ORDER BY n.created_at DESC, n.id DESC`, bookID, userID)
	if err != nil {
		return nil, fmt.Errorf("books: notes: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []Note
	for rows.Next() {
		var n Note
		var page sql.NullInt64
		var created, updated string
		if err := rows.Scan(&n.ID, &page, &n.Body, &created, &updated); err != nil {
			return nil, fmt.Errorf("books: scan note: %w", err)
		}
		n.Page = int(page.Int64)
		if n.CreatedAt, err = parseTime(created); err != nil {
			return nil, fmt.Errorf("books: note created_at: %w", err)
		}
		if n.UpdatedAt, err = parseTime(updated); err != nil {
			return nil, fmt.Errorf("books: note updated_at: %w", err)
		}
		out = append(out, n)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("books: notes: %w", err)
	}
	return out, nil
}
```

- [ ] **Step 5: Quotes**

Create `internal/apps/books/quotes.go`:

```go
package books

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// Quote is a passage copied out of a book (spec "Data model":
// books_quotes): plain text, its line breaks kept, with an optional page
// and an optional Markdown comment.
type Quote struct {
	ID                   int64
	Page                 int // 0 when not given
	Text                 string
	Comment              string
	CreatedAt, UpdatedAt time.Time
}

// QuoteInput is a quote as typed; Page as in NoteInput.
type QuoteInput struct {
	Page          int
	Text, Comment string
}

// checkQuote cleans a quote and refuses one with no text, or with text or
// a comment over MaxQuoteRunes.
func checkQuote(in QuoteInput) (QuoteInput, error) {
	in.Text, in.Comment = cleanText(in.Text), cleanText(in.Comment)
	if in.Text == "" {
		return in, &Refusal{Msg: "Type the quote first."}
	}
	if err := checkLength(in.Text, MaxQuoteRunes, "the quote"); err != nil {
		return in, err
	}
	return in, checkLength(in.Comment, MaxQuoteRunes, "your comment")
}

// AddQuote adds a quote to one of userID's books.
func (st *Store) AddQuote(ctx context.Context, userID, bookID int64, in QuoteInput) (int64, error) {
	in, err := checkQuote(in)
	if err != nil {
		return 0, err
	}
	var id int64
	err = st.inBook(ctx, userID, bookID, in.Page, func(tx *sql.Tx, now string) error {
		res, err := tx.ExecContext(ctx,
			`INSERT INTO books_quotes (book_id, page, text, comment, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?)`,
			bookID, nullInt(in.Page), in.Text, in.Comment, now, now)
		if err != nil {
			return fmt.Errorf("books: add quote: %w", err)
		}
		id, err = res.LastInsertId()
		return err
	})
	return id, err
}

// UpdateQuote changes a quote's page, text and comment.
func (st *Store) UpdateQuote(ctx context.Context, userID, bookID, quoteID int64, in QuoteInput) error {
	in, err := checkQuote(in)
	if err != nil {
		return err
	}
	return st.inBook(ctx, userID, bookID, in.Page, func(tx *sql.Tx, now string) error {
		return execOne(ctx, tx, `UPDATE books_quotes SET page = ?, text = ?, comment = ?, updated_at = ? WHERE id = ? AND book_id = ?`,
			nullInt(in.Page), in.Text, in.Comment, now, quoteID, bookID)
	})
}

// DeleteQuote removes a quote for good.
func (st *Store) DeleteQuote(ctx context.Context, userID, bookID, quoteID int64) error {
	return st.inBook(ctx, userID, bookID, 0, func(tx *sql.Tx, _ string) error {
		return execOne(ctx, tx, `DELETE FROM books_quotes WHERE id = ? AND book_id = ?`, quoteID, bookID)
	})
}

// Quotes is a book's quotes, newest first. A book that isn't userID's has
// none.
func (st *Store) Quotes(ctx context.Context, userID, bookID int64) ([]Quote, error) {
	rows, err := st.db.QueryContext(ctx, `
		SELECT q.id, q.page, q.text, q.comment, q.created_at, q.updated_at
		  FROM books_quotes q JOIN books_books b ON b.id = q.book_id
		 WHERE b.id = ? AND b.user_id = ?
		 ORDER BY q.created_at DESC, q.id DESC`, bookID, userID)
	if err != nil {
		return nil, fmt.Errorf("books: quotes: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []Quote
	for rows.Next() {
		var q Quote
		var page sql.NullInt64
		var created, updated string
		if err := rows.Scan(&q.ID, &page, &q.Text, &q.Comment, &created, &updated); err != nil {
			return nil, fmt.Errorf("books: scan quote: %w", err)
		}
		q.Page = int(page.Int64)
		if q.CreatedAt, err = parseTime(created); err != nil {
			return nil, fmt.Errorf("books: quote created_at: %w", err)
		}
		if q.UpdatedAt, err = parseTime(updated); err != nil {
			return nil, fmt.Errorf("books: quote updated_at: %w", err)
		}
		out = append(out, q)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("books: quotes: %w", err)
	}
	return out, nil
}
```

- [ ] **Step 6: Run the tests, vet and staticcheck**

```bash
go test ./internal/apps/books/... -count=1
go vet ./...
go run honnef.co/go/tools/cmd/staticcheck@v0.8.0 ./...
```
Expected: all pass.

- [ ] **Step 7: Commit**

```bash
git add internal/apps/books
git commit -m "feat(books): notes and quotes in the store (#489)"
```

---

### Task 2: Full-text search in the store

**Files:**
- Create: `internal/apps/books/migrations/0005_search.sql`, `internal/apps/books/search.go`
- Modify: `internal/apps/books/library.go` (`ListItem`, `ListQuery` comment, `likeEscape` removed, `List`), `internal/apps/books/shelf_test.go` (one case of `TestListFiltersByTagAndText`)
- Test: `internal/apps/books/filter_test.go`

**Interfaces:**
- Consumes: Task 1's `AddNote`, `UpdateNote`, `DeleteNote`, `AddQuote`, `UpdateQuote`, `DeleteQuote`, `NoteInput`, `QuoteInput` and test helpers `notes`, `quotes`; `SetReview`, `SetTags`, `Update`, `Delete`; test helpers `titles` (shelf_test.go), `newFixture`, `addBook`, `onShelf`.
- Produces:
  - `type MatchIn string`; `MatchBook MatchIn = ""`, `MatchQuote = "quote"`, `MatchComment = "comment"`, `MatchNote = "note"`, `MatchReview = "review"`
  - `const SnippetOpen = "\x02"`, `SnippetClose = "\x03"`
  - `ListItem.Match MatchIn`, `ListItem.Snippet string` — set when `ListQuery.Q` is; `""`/`MatchBook` otherwise
  - `ListQuery.Q` is now full-text: every word must match, each as a prefix, within the shelf/tag/series; the shelf's order is kept
  - unexported: `ftsQuery(q string) string`, `searchJoin`, `snippetCols`, `noSnippets`, `matchOf(quote, comment, note, review string) (MatchIn, string)`
  - test helpers: `filtered(t, f, shelf, q) []books.ListItem`, `shelfOfNotes(t, f) (dune, emma int64)`

- [ ] **Step 1: Write the failing tests**

Create `internal/apps/books/filter_test.go`:

```go
package books_test

import (
	"context"
	"path/filepath"
	"slices"
	"testing"

	"github.com/iliafrenkel/on-suite/internal/apps/books"
	"github.com/iliafrenkel/on-suite/internal/apptest"
	"github.com/iliafrenkel/on-suite/internal/platform/auth"
	"github.com/iliafrenkel/on-suite/internal/platform/db"
)

// filtered is Alice's books on shelf matching q.
func filtered(t *testing.T, f *fixture, shelf books.Shelf, q string) []books.ListItem {
	t.Helper()
	items, err := f.store.List(context.Background(), f.alice.ID, books.ListQuery{Shelf: shelf, Q: q})
	if err != nil {
		t.Fatalf("List(%q) = %v", q, err)
	}
	return items
}

// shelfOfNotes is a small library with something written in each place
// the filter searches.
func shelfOfNotes(t *testing.T, f *fixture) (dune, emma int64) {
	t.Helper()
	ctx := context.Background()
	nb := onShelf("Dune", books.ShelfRead)
	nb.Authors = "Frank Herbert"
	dune = addBook(t, f, f.alice.ID, nb)
	emma = addBook(t, f, f.alice.ID, onShelf("Emma", books.ShelfWant))
	if err := f.store.SetReview(ctx, f.alice.ID, dune, "So much sand, and all of it **worth** it."); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.AddNote(ctx, f.alice.ID, emma, books.NoteInput{Body: "Mr Knightley is right about the picnic."}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.AddQuote(ctx, f.alice.ID, dune, books.QuoteInput{Text: "The spice must flow.",
		Comment: "Said by nobody in the book, as it happens."}); err != nil {
		t.Fatal(err)
	}
	return dune, emma
}

func TestFilterSearchesEverythingWritten(t *testing.T) {
	f := newFixture(t)
	shelfOfNotes(t, f)
	bob := addBook(t, f, f.bob.ID, onShelf("Bob's book", books.ShelfWant))
	if _, err := f.store.AddNote(context.Background(), f.bob.ID, bob, books.NoteInput{Body: "The spice, again."}); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		q       string
		title   string
		in      books.MatchIn
		snippet string
	}{
		{"herbert", "Dune", books.MatchBook, ""},
		{"spice", "Dune", books.MatchQuote, "The \x02spice\x03 must flow."},
		{"nobody", "Dune", books.MatchComment, "Said by \x02nobody\x03 in the book, as it happens."},
		{"knight", "Emma", books.MatchNote, "Mr \x02Knightley\x03 is right about the picnic."},
		{"sand worth", "Dune", books.MatchReview, "So much \x02sand\x03, and all of it **\x02worth\x03** it."},
		{"SPICE", "Dune", books.MatchQuote, "The \x02spice\x03 must flow."},
	}
	for _, tt := range tests {
		items := filtered(t, f, books.ShelfAll, tt.q)
		if len(items) != 1 {
			t.Errorf("%q matched %v, want just %s", tt.q, titles(items), tt.title)
			continue
		}
		if it := items[0]; it.Title != tt.title || it.Match != tt.in || it.Snippet != tt.snippet {
			t.Errorf("%q = %s in %q: %q; want %s in %q: %q", tt.q, it.Title, it.Match, it.Snippet, tt.title, tt.in, tt.snippet)
		}
	}
	if got := filtered(t, f, books.ShelfAll, "picnic spice"); len(got) != 0 {
		t.Errorf("words from two books matched %v: every word must match the same book", titles(got))
	}
	if got := filtered(t, f, books.ShelfAll, ""); len(got) != 2 || got[0].Snippet != "" {
		t.Errorf("no filter = %+v, want both books without snippets", got)
	}
}

func TestFilterStaysWithinTheList(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	_, emma := shelfOfNotes(t, f)
	persuasion := addBook(t, f, f.alice.ID, onShelf("Persuasion", books.ShelfWant))
	if _, err := f.store.AddNote(ctx, f.alice.ID, persuasion, books.NoteInput{Body: "A picnic of sorts."}); err != nil {
		t.Fatal(err)
	}
	if err := f.store.SetTags(ctx, f.alice.ID, emma, []string{"austen"}); err != nil {
		t.Fatal(err)
	}
	if got := titles(filtered(t, f, books.ShelfWant, "picnic")); !slices.Equal(got, []string{"Persuasion", "Emma"}) {
		t.Errorf("Want to read for picnic = %v, want the shelf's own order (newest added first)", got)
	}
	if got := titles(filtered(t, f, books.ShelfRead, "picnic")); len(got) != 0 {
		t.Errorf("Read for picnic = %v, want none: Emma and Persuasion aren't read", got)
	}
	items, err := f.store.List(ctx, f.alice.ID, books.ListQuery{Shelf: books.ShelfAll, Tag: "austen", Q: "picnic"})
	if err != nil || !slices.Equal(titles(items), []string{"Emma"}) {
		t.Errorf("tag austen for picnic = %v, %v; want Emma", titles(items), err)
	}
}

func TestTheSearchIndexFollowsEveryChange(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	dune, emma := shelfOfNotes(t, f)
	found := func(q string) []string { return titles(filtered(t, f, books.ShelfAll, q)) }

	if err := f.store.Update(ctx, f.alice.ID, dune, books.BookInput{Title: "Children of Dune", SeriesName: "Dune Chronicles"}); err != nil {
		t.Fatal(err)
	}
	if got := found("children"); !slices.Equal(got, []string{"Children of Dune"}) {
		t.Errorf("new title: %v", got)
	}
	if got := found("herbert"); len(got) != 0 {
		t.Errorf("removed author still matches %v", got)
	}
	if got := found("chronicles"); len(got) != 1 {
		t.Errorf("series: %v", got)
	}

	ns := notes(t, f, f.alice.ID, emma)
	if err := f.store.UpdateNote(ctx, f.alice.ID, emma, ns[0].ID, books.NoteInput{Body: "Box Hill."}); err != nil {
		t.Fatal(err)
	}
	if got := found("picnic"); len(got) != 0 {
		t.Errorf("edited-away note still matches %v", got)
	}
	if got := found("box hill"); !slices.Equal(got, []string{"Emma"}) {
		t.Errorf("edited note: %v", got)
	}
	if err := f.store.DeleteNote(ctx, f.alice.ID, emma, ns[0].ID); err != nil {
		t.Fatal(err)
	}
	if got := found("box"); len(got) != 0 {
		t.Errorf("deleted note still matches %v", got)
	}

	qs := quotes(t, f, f.alice.ID, dune)
	if err := f.store.UpdateQuote(ctx, f.alice.ID, dune, qs[0].ID, books.QuoteInput{Text: "Fear is the mind-killer."}); err != nil {
		t.Fatal(err)
	}
	if got := found("nobody"); len(got) != 0 {
		t.Errorf("removed comment still matches %v", got)
	}
	if got := found("mind"); len(got) != 1 {
		t.Errorf("edited quote: %v", got)
	}
	if err := f.store.DeleteQuote(ctx, f.alice.ID, dune, qs[0].ID); err != nil {
		t.Fatal(err)
	}
	if got := found("fear"); len(got) != 0 {
		t.Errorf("deleted quote still matches %v", got)
	}
	if err := f.store.SetReview(ctx, f.alice.ID, dune, ""); err != nil {
		t.Fatal(err)
	}
	if got := found("sand"); len(got) != 0 {
		t.Errorf("removed review still matches %v", got)
	}

	if err := f.store.Delete(ctx, f.alice.ID, emma); err != nil {
		t.Fatal(err)
	}
	var rows int
	if err := f.db.QueryRow(`SELECT count(*) FROM books_search`).Scan(&rows); err != nil || rows != 1 {
		t.Errorf("index rows = %d, %v; want 1 once Emma is gone", rows, err)
	}
}

func TestFilterIgnoresSearchSyntax(t *testing.T) {
	f := newFixture(t)
	shelfOfNotes(t, f)
	for _, q := range []string{`AND`, `"`, `(`, `title:dune`, `NEAR(a b)`, `*`, `-dune`, `100%`} {
		if _, err := f.store.List(context.Background(), f.alice.ID, books.ListQuery{Shelf: books.ShelfAll, Q: q}); err != nil {
			t.Errorf("List(%q) = %v, want no error", q, err)
		}
	}
}

// TestTheIndexIsBuiltForExistingBooks applies the migrations up to
// 0004, writes a book with a note and a quote, then applies the rest: the
// index must hold what was there before it.
func TestTheIndexIsBuiltForExistingBooks(t *testing.T) {
	ctx := context.Background()
	handle, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = handle.Close() })
	ms, err := db.Collect(auth.Namespace, auth.Migrations())
	if err != nil {
		t.Fatal(err)
	}
	app, err := db.Collect(books.ID, books.Migrations())
	if err != nil {
		t.Fatal(err)
	}
	var before []db.Migration
	for _, m := range app {
		if m.ID <= "0004" {
			before = append(before, m)
		}
	}
	if _, err := db.Apply(ctx, handle, append(ms, before...)); err != nil {
		t.Fatal(err)
	}
	u, err := auth.NewStore(handle).CreateUser(ctx, "alice", apptest.PasswordHash, true)
	if err != nil {
		t.Fatal(err)
	}
	st := books.NewStore(handle)
	id, err := st.Create(ctx, u.ID, books.NewBook{BookInput: books.BookInput{Title: "Dune"}, Shelf: books.ShelfWant})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.AddNote(ctx, u.ID, id, books.NoteInput{Body: "Sandworms."}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.AddQuote(ctx, u.ID, id, books.QuoteInput{Text: "The spice must flow.", Comment: "Melange."}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Apply(ctx, handle, append(ms, app...)); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{"dune", "sandworms", "spice", "melange"} {
		items, err := st.List(ctx, u.ID, books.ListQuery{Shelf: books.ShelfAll, Q: q})
		if err != nil || len(items) != 1 {
			t.Errorf("after the migration, %q found %d books, %v; want Dune", q, len(items), err)
		}
	}
}
```

The old LIKE filter matched inside words and treated "%" literally; the FTS filter matches whole words by prefix and ignores punctuation, so one existing case changes:

In `internal/apps/books/shelf_test.go`, replace:

```go
		{books.ListQuery{Shelf: books.ShelfAll, Q: "le guin"}, []string{"A Wizard of Earthsea", "The Dispossessed"}},
		{books.ListQuery{Shelf: books.ShelfWant, Q: "le guin"}, []string{"The Dispossessed"}},
		{books.ListQuery{Shelf: books.ShelfAll, Q: "earthsea"}, []string{"A Wizard of Earthsea"}},
		{books.ListQuery{Shelf: books.ShelfAll, Q: "100%"}, []string{"100% Real"}},
		{books.ListQuery{Shelf: books.ShelfAll, Tag: "SF"}, []string{"The Dispossessed"}},
		{books.ListQuery{Shelf: books.ShelfAll, Tag: "nope"}, nil},
	}
```

with:

```go
		{books.ListQuery{Shelf: books.ShelfAll, Q: "le guin"}, []string{"A Wizard of Earthsea", "The Dispossessed"}},
		{books.ListQuery{Shelf: books.ShelfWant, Q: "le guin"}, []string{"The Dispossessed"}},
		{books.ListQuery{Shelf: books.ShelfAll, Q: "earthsea"}, []string{"A Wizard of Earthsea"}},
		// Punctuation separates words, and a word matches as a prefix
		// (full-text search, B3).
		{books.ListQuery{Shelf: books.ShelfAll, Q: "100%"}, []string{"100% Real", "1000 Years"}},
		{books.ListQuery{Shelf: books.ShelfAll, Tag: "SF"}, []string{"The Dispossessed"}},
		{books.ListQuery{Shelf: books.ShelfAll, Tag: "nope"}, nil},
	}
```

- [ ] **Step 2: Run them to see them fail**

Run: `go test ./internal/apps/books/... -count=1`
Expected: FAIL — `undefined: books.MatchBook`, `undefined: books.MatchQuote`, … (the package's tests don't build).

- [ ] **Step 3: The index**

Create `internal/apps/books/migrations/0005_search.sql`:

```sql
-- B3: full-text search over a book's title, subtitle, authors, series name,
-- review, notes, quotes and quote comments (spec "Data model":
-- books_search). The list's filter box searches it.
--
-- A regular FTS5 table with its own copy of the text, as ON Later's
-- later_search is (internal/apps/later/migrations/0006_search.sql): three
-- of the columns are gathered from other tables, which external content
-- can't express, and snippet() needs the text. For one household the copy
-- is cheap.
--
-- rowid is the book id. Only the triggers below write to it, so every
-- change lands in the same transaction as the write that caused it,
-- including the cascade from deleting a book. prefix='2 3 4' is what lets
-- the filter match a word still being typed.
CREATE VIRTUAL TABLE books_search USING fts5(
    title, subtitle, authors, series, review, notes, quotes, comments,
    tokenize='unicode61', prefix='2 3 4'
);

INSERT INTO books_search (rowid, title, subtitle, authors, series, review, notes, quotes, comments)
SELECT b.id, b.title, b.subtitle, b.authors, b.series_name, b.review,
       COALESCE((SELECT group_concat(n.body, char(10) ORDER BY n.created_at, n.id)
                   FROM books_notes n WHERE n.book_id = b.id), ''),
       COALESCE((SELECT group_concat(q.text, char(10) ORDER BY q.created_at, q.id)
                   FROM books_quotes q WHERE q.book_id = b.id), ''),
       COALESCE((SELECT group_concat(q.comment, char(10) ORDER BY q.created_at, q.id)
                   FROM books_quotes q WHERE q.book_id = b.id AND q.comment <> ''), '')
  FROM books_books b;

CREATE TRIGGER books_search_ai AFTER INSERT ON books_books BEGIN
    INSERT INTO books_search (rowid, title, subtitle, authors, series, review, notes, quotes, comments)
    VALUES (new.id, new.title, new.subtitle, new.authors, new.series_name, new.review, '', '', '');
END;

-- Only the indexed columns: rating, progress and touch() leave the index
-- alone.
CREATE TRIGGER books_search_au AFTER UPDATE OF title, subtitle, authors, series_name, review ON books_books BEGIN
    UPDATE books_search
       SET title = new.title, subtitle = new.subtitle, authors = new.authors,
           series = new.series_name, review = new.review
     WHERE rowid = new.id;
END;

CREATE TRIGGER books_search_ad AFTER DELETE ON books_books BEGIN
    DELETE FROM books_search WHERE rowid = old.id;
END;

-- A note change rebuilds its book's whole notes column.
CREATE TRIGGER books_search_note_ai AFTER INSERT ON books_notes BEGIN
    UPDATE books_search SET notes = COALESCE((
        SELECT group_concat(n.body, char(10) ORDER BY n.created_at, n.id)
          FROM books_notes n WHERE n.book_id = new.book_id), '')
     WHERE rowid = new.book_id;
END;

CREATE TRIGGER books_search_note_au AFTER UPDATE OF body ON books_notes BEGIN
    UPDATE books_search SET notes = COALESCE((
        SELECT group_concat(n.body, char(10) ORDER BY n.created_at, n.id)
          FROM books_notes n WHERE n.book_id = new.book_id), '')
     WHERE rowid = new.book_id;
END;

CREATE TRIGGER books_search_note_ad AFTER DELETE ON books_notes BEGIN
    UPDATE books_search SET notes = COALESCE((
        SELECT group_concat(n.body, char(10) ORDER BY n.created_at, n.id)
          FROM books_notes n WHERE n.book_id = old.book_id), '')
     WHERE rowid = old.book_id;
END;

-- A quote change rebuilds its book's quotes and comments columns.
CREATE TRIGGER books_search_quote_ai AFTER INSERT ON books_quotes BEGIN
    UPDATE books_search
       SET quotes = COALESCE((SELECT group_concat(q.text, char(10) ORDER BY q.created_at, q.id)
                                FROM books_quotes q WHERE q.book_id = new.book_id), ''),
           comments = COALESCE((SELECT group_concat(q.comment, char(10) ORDER BY q.created_at, q.id)
                                  FROM books_quotes q WHERE q.book_id = new.book_id AND q.comment <> ''), '')
     WHERE rowid = new.book_id;
END;

CREATE TRIGGER books_search_quote_au AFTER UPDATE OF text, comment ON books_quotes BEGIN
    UPDATE books_search
       SET quotes = COALESCE((SELECT group_concat(q.text, char(10) ORDER BY q.created_at, q.id)
                                FROM books_quotes q WHERE q.book_id = new.book_id), ''),
           comments = COALESCE((SELECT group_concat(q.comment, char(10) ORDER BY q.created_at, q.id)
                                  FROM books_quotes q WHERE q.book_id = new.book_id AND q.comment <> ''), '')
     WHERE rowid = new.book_id;
END;

CREATE TRIGGER books_search_quote_ad AFTER DELETE ON books_quotes BEGIN
    UPDATE books_search
       SET quotes = COALESCE((SELECT group_concat(q.text, char(10) ORDER BY q.created_at, q.id)
                                FROM books_quotes q WHERE q.book_id = old.book_id), ''),
           comments = COALESCE((SELECT group_concat(q.comment, char(10) ORDER BY q.created_at, q.id)
                                  FROM books_quotes q WHERE q.book_id = old.book_id AND q.comment <> ''), '')
     WHERE rowid = old.book_id;
END;
```

- [ ] **Step 4: The query helpers**

Create `internal/apps/books/search.go`:

```go
package books

import "strings"

// ftsQuery turns free text into an FTS5 MATCH expression that can never be
// a syntax error.
//
// Mirrors ON Later's ftsQuery (internal/apps/later/search.go), itself a
// copy of ON Reader's and ON Notes' — copied rather than shared because
// apps never import each other; see "Cross-app mirroring" in PATTERNS.md.
//
// Each word becomes its own quoted phrase, doubling any embedded quote, so
// anything a person types — an operator like AND, a bare quote, a
// parenthesis — lands inside the quotes as inert phrase text instead of
// breaking the query. The trailing * is FTS5's prefix operator, which is
// what makes the filter match while a word is still being typed.
func ftsQuery(q string) string {
	words := strings.Fields(q)
	quoted := make([]string, len(words))
	for i, w := range words {
		quoted[i] = `"` + strings.ReplaceAll(w, `"`, `""`) + `"*`
	}
	return strings.Join(quoted, " ")
}

// MatchIn says where a filtered book matched, for its snippet's label
// (decided 2026-10-09: a third line on the row with the match).
type MatchIn string

// Where a book can match. A match on what the row already shows — title,
// subtitle, authors, series — has no snippet.
const (
	MatchBook    MatchIn = ""
	MatchQuote   MatchIn = "quote"
	MatchComment MatchIn = "comment"
	MatchNote    MatchIn = "note"
	MatchReview  MatchIn = "review"
)

// SnippetOpen and SnippetClose wrap each matched word in a snippet, as in
// ON Later. Control characters never occur in what people type into a
// form, so they can't be confused with real text.
const (
	SnippetOpen  = "\x02"
	SnippetClose = "\x03"
)

// searchJoin attaches the index to a list query. books_search is named,
// never aliased: the driver resolves MATCH and snippet() against the real
// name.
const searchJoin = `JOIN books_search ON books_search.rowid = b.id`

// snippetCols are a filtered list's four snippets, in the order matchOf
// takes them. Column numbers: 0 title, 1 subtitle, 2 authors, 3 series,
// 4 review, 5 notes, 6 quotes, 7 comments. Twelve tokens fit a list row.
const snippetCols = `snippet(books_search, 6, char(2), char(3), '…', 12),
		       snippet(books_search, 7, char(2), char(3), '…', 12),
		       snippet(books_search, 5, char(2), char(3), '…', 12),
		       snippet(books_search, 4, char(2), char(3), '…', 12)`

// noSnippets stands in for snippetCols in an unfiltered list.
const noSnippets = `'', '', '', ''`

// matchOf picks the snippet a row shows: the first of the quote, comment,
// note and review snippets that holds a match. snippet() returns a
// column's opening words even when it didn't match, so a column matched
// only if its snippet has a marker. None is MatchBook, with no snippet.
func matchOf(quote, comment, note, review string) (MatchIn, string) {
	for _, c := range []struct {
		in      MatchIn
		snippet string
	}{{MatchQuote, quote}, {MatchComment, comment}, {MatchNote, note}, {MatchReview, review}} {
		if strings.Contains(c.snippet, SnippetOpen) {
			return c.in, c.snippet
		}
	}
	return MatchBook, ""
}
```

- [ ] **Step 5: Search from List**

In `internal/apps/books/library.go`, replace:

```go
	StartedOn, FinishedOn, Format            string   // the latest reading's
	Progress                                 Progress // the latest reading's latest progress
	AddedAt                                  time.Time
	CoverVersion                             string // "" when the book has no cover
}

// ListQuery picks the books a list shows. Shelf "" or ShelfAll is every
// shelf; Tag is one tag name; Q matches title, subtitle, authors or series
// name (SQLite LIKE: case-insensitive for ASCII); Series is one series'
// name, matched whole and ignoring case.
type ListQuery struct {
	Shelf  Shelf
	Tag    string
```

with:

```go
	StartedOn, FinishedOn, Format            string   // the latest reading's
	Progress                                 Progress // the latest reading's latest progress
	AddedAt                                  time.Time
	CoverVersion                             string  // "" when the book has no cover
	Match                                    MatchIn // where a filtered book matched
	Snippet                                  string  // the match, between SnippetOpen/SnippetClose; "" for MatchBook
}

// ListQuery picks the books a list shows. Shelf "" or ShelfAll is every
// shelf; Tag is one tag name; Q is full-text search over books_search —
// title, subtitle, authors, series, review, notes, quotes and comments —
// every word matching, the last one or any as a prefix; Series is one
// series' name, matched whole and ignoring case.
type ListQuery struct {
	Shelf  Shelf
	Tag    string
```

In `internal/apps/books/library.go`, replace:

```go
	}
}

// likeEscape makes s match itself literally in a LIKE ... ESCAPE '\'.
func likeEscape(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
}

// List returns userID's books matching q, in the shelf's order.
func (st *Store) List(ctx context.Context, userID int64, q ListQuery) ([]ListItem, error) {
	where := []string{"b.user_id = ?"}
	args := []any{userID}
```

with:

```go
	}
}

// List returns userID's books matching q, in the shelf's order — a search
// keeps it too (decided 2026-10-09), so filtering never reshuffles a shelf.
func (st *Store) List(ctx context.Context, userID int64, q ListQuery) ([]ListItem, error) {
	where := []string{"b.user_id = ?"}
	args := []any{userID}
```

In `internal/apps/books/library.go`, replace:

```go
		where = append(where, `b.series_name = ? COLLATE NOCASE`)
		args = append(args, series)
	}
	if text := strings.TrimSpace(q.Q); text != "" {
		pat := "%" + likeEscape(text) + "%"
		where = append(where, `(b.title LIKE ? ESCAPE '\' OR b.subtitle LIKE ? ESCAPE '\'
			OR b.authors LIKE ? ESCAPE '\' OR b.series_name LIKE ? ESCAPE '\')`)
		args = append(args, pat, pat, pat, pat)
	}
	rows, err := st.db.QueryContext(ctx, `
		SELECT b.id, b.title, b.authors, b.series_name, b.series_number, b.pages, b.rating, b.added_at,
		       r.started_on, r.finished_on, r.format, `+shelfExpr+`, c.fetched_at,
		       p.page, p.percent, p.recorded_at
		  FROM books_books b `+latestJoin+`
		  `+progressJoin+`
		  LEFT JOIN books_covers c ON c.book_id = b.id
		 WHERE `+strings.Join(where, " AND ")+`
```

with:

```go
		where = append(where, `b.series_name = ? COLLATE NOCASE`)
		args = append(args, series)
	}
	search, snippets := "", noSnippets
	if match := ftsQuery(q.Q); match != "" {
		search, snippets = searchJoin, snippetCols
		where = append(where, `books_search MATCH ?`)
		args = append(args, match)
	}
	rows, err := st.db.QueryContext(ctx, `
		SELECT b.id, b.title, b.authors, b.series_name, b.series_number, b.pages, b.rating, b.added_at,
		       r.started_on, r.finished_on, r.format, `+shelfExpr+`, c.fetched_at,
		       p.page, p.percent, p.recorded_at,
		       `+snippets+`
		  FROM books_books b `+search+`
		  `+latestJoin+`
		  `+progressJoin+`
		  LEFT JOIN books_covers c ON c.book_id = b.id
		 WHERE `+strings.Join(where, " AND ")+`
```

In `internal/apps/books/library.go`, replace:

```go
		var added, shelf string
		var pages, rating, atPage, atPercent sql.NullInt64
		var started, finished, format, cover, recorded sql.NullString
		if err := rows.Scan(&it.ID, &it.Title, &it.Authors, &it.SeriesName, &it.SeriesNumber, &pages, &rating, &added,
			&started, &finished, &format, &shelf, &cover, &atPage, &atPercent, &recorded); err != nil {
			return nil, fmt.Errorf("books: scan list: %w", err)
		}
		if it.AddedAt, err = parseTime(added); err != nil {
			return nil, fmt.Errorf("books: added_at: %w", err)
		}
```

with:

```go
		var added, shelf string
		var pages, rating, atPage, atPercent sql.NullInt64
		var started, finished, format, cover, recorded sql.NullString
		var inQuote, inComment, inNote, inReview string
		if err := rows.Scan(&it.ID, &it.Title, &it.Authors, &it.SeriesName, &it.SeriesNumber, &pages, &rating, &added,
			&started, &finished, &format, &shelf, &cover, &atPage, &atPercent, &recorded,
			&inQuote, &inComment, &inNote, &inReview); err != nil {
			return nil, fmt.Errorf("books: scan list: %w", err)
		}
		it.Match, it.Snippet = matchOf(inQuote, inComment, inNote, inReview)
		if it.AddedAt, err = parseTime(added); err != nil {
			return nil, fmt.Errorf("books: added_at: %w", err)
		}
```

- [ ] **Step 6: Run the tests, vet and staticcheck**

```bash
go test ./internal/apps/books/... -count=1
go vet ./...
go run honnef.co/go/tools/cmd/staticcheck@v0.8.0 ./...
```
Expected: all pass — including the B1/B2 filter tests (`TestShelfAndFilterQueriesPickTheBooks`, `TestListCarriesItsContextForTheBookPane`), which now go through the index.

- [ ] **Step 7: Commit**

```bash
git add internal/apps/books
git commit -m "feat(books): full-text search over books, reviews, notes and quotes (#489)"
```

---

### Task 3: Where a filtered book matched, on its row

**Files:**
- Create: `internal/apps/books/snippet.go`
- Modify: `internal/apps/books/view.go` (`rowView`, `viewList`), `internal/apps/books/templates/panes.partial.html` (filter box, `list`), `internal/ui/static/app.css`
- Test: `internal/apps/books/snippet_view_test.go`

**Interfaces:**
- Consumes: Task 2's `ListItem.Match`/`Snippet`, `MatchIn` constants, `SnippetOpen`/`SnippetClose`; Task 1's `AddQuote`, `AddNote`; test helpers `newServer`, `add`, `titled`, `rowTitles`, `hx` (handlers_test.go).
- Produces:
  - `type snippetPart struct { Text string; Hit bool }`, `snippetParts(s string) []snippetPart`, `type snippetView struct { In string; Parts []snippetPart }`, `matchLabels`, `newSnippet(it ListItem) *snippetView`
  - `rowView.Snippet *snippetView`; the row's `.books-row-snippet` line with a `.books-snippet-in` label and `<mark>` hits
  - the filter box: label "Search your books", placeholder "Search books, notes and quotes…"

- [ ] **Step 1: Write the failing tests**

Create `internal/apps/books/snippet_view_test.go`:

```go
package books_test

import (
	"context"
	"testing"

	"github.com/iliafrenkel/on-suite/internal/apps/books"
	"github.com/iliafrenkel/on-suite/internal/htmlassert"
)

func TestFilteredRowsSayWhereTheyMatched(t *testing.T) {
	s := newServer(t)
	ctx := context.Background()
	uid := s.Alice.User.ID
	id := add(t, s, uid, titled("Dune", "Frank Herbert", books.ShelfWant))
	if _, err := s.Store.AddQuote(ctx, uid, id, books.QuoteInput{Text: "The <b>spice</b> must flow."}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Store.AddNote(ctx, uid, id, books.NoteInput{Body: "Sandworms."}); err != nil {
		t.Fatal(err)
	}

	doc := s.Get(t, s.Alice, "/books/?shelf=all&q=spice")
	if got := htmlassert.Text(doc.MustHave(".books-row-snippet")); got != "Quote: The <b>spice</b> must flow." {
		t.Errorf("snippet = %q", got)
	}
	if got := htmlassert.Text(doc.MustHave(".books-row-snippet mark")); got != "spice" {
		t.Errorf("highlight = %q", got)
	}
	doc.MustNotHave(".books-row-snippet b") // the quote's own markup is text

	doc = htmlassert.Parse(t, hx(t, s, "/books/?shelf=all&q=sandw", "books-list"))
	if got := htmlassert.Text(doc.MustHave(".books-snippet-in")); got != "Note:" {
		t.Errorf("label after typing part of a word = %q, want Note:", got)
	}

	for _, q := range []string{"dune", "herbert", ""} {
		doc = s.Get(t, s.Alice, "/books/?shelf=all&q="+q)
		if got := rowTitles(doc); len(got) != 1 {
			t.Errorf("q=%s rows = %v, want Dune", q, got)
		}
		doc.MustNotHave(".books-row-snippet")
	}
}

func TestTheFilterBoxSearchesEverything(t *testing.T) {
	s := newServer(t)
	doc := s.Get(t, s.Alice, "/books/")
	if got, _ := htmlassert.Attr(doc.MustHave("input#books-q"), "placeholder"); got != "Search books, notes and quotes…" {
		t.Errorf("placeholder = %q", got)
	}
}
```

- [ ] **Step 2: Run them to see them fail**

Run: `go test ./internal/apps/books/... -run 'TestFilteredRowsSayWhereTheyMatched|TestTheFilterBoxSearchesEverything' -count=1`
Expected: FAIL — `no element matches ".books-row-snippet"` and `placeholder = "Filter by title or author…"`.

- [ ] **Step 3: The snippet view**

Create `internal/apps/books/snippet.go`:

```go
package books

import "strings"

// snippetPart is a run of a search snippet; Hit runs are drawn as <mark>.
// Rendering parts, rather than converting the snippet to template.HTML,
// keeps every character escaped by html/template.
//
// Mirrors ON Later's snippetParts (internal/apps/later/snippet.go); apps
// never import each other, so this is an independent copy.
type snippetPart struct {
	Text string
	Hit  bool
}

// snippetParts splits a List snippet at its SnippetOpen/SnippetClose
// markers, collapsing whitespace (the notes, quotes and comments columns
// separate entries with newlines). An unclosed marker runs to the end.
func snippetParts(s string) []snippetPart {
	s = strings.Join(strings.Fields(s), " ")
	var parts []snippetPart
	for s != "" {
		i := strings.Index(s, SnippetOpen)
		if i < 0 {
			parts = append(parts, snippetPart{Text: s})
			break
		}
		if i > 0 {
			parts = append(parts, snippetPart{Text: s[:i]})
		}
		s = s[i+len(SnippetOpen):]
		j := strings.Index(s, SnippetClose)
		if j < 0 {
			j = len(s)
		}
		if j > 0 {
			parts = append(parts, snippetPart{Text: s[:j], Hit: true})
		}
		s = s[min(len(s), j+len(SnippetClose)):]
	}
	return parts
}

// snippetView is a filtered row's third line: where the filter matched
// and the words around it (decided 2026-10-09).
type snippetView struct {
	In    string
	Parts []snippetPart
}

var matchLabels = map[MatchIn]string{
	MatchQuote:   "Quote:",
	MatchComment: "Quote comment:",
	MatchNote:    "Note:",
	MatchReview:  "Review:",
}

// newSnippet is it's snippet line, or nil when the filter matched only
// what the row already shows.
func newSnippet(it ListItem) *snippetView {
	if it.Match == MatchBook {
		return nil
	}
	return &snippetView{In: matchLabels[it.Match], Parts: snippetParts(it.Snippet)}
}
```

In `internal/apps/books/view.go`, replace:

```go
	Stars   string // Rating drawn: "★★★★☆"
	Spine   string // swatch colour name
	Initial string
	Cover   string // the stored cover; "" draws the mini spine
	Active  bool
}
```

with:

```go
	Stars   string // Rating drawn: "★★★★☆"
	Spine   string // swatch colour name
	Initial string
	Cover   string       // the stored cover; "" draws the mini spine
	Snippet *snippetView // where a filtered row matched; nil when it shows already
	Active  bool
}
```

In `internal/apps/books/view.go`, replace:

```go
	for _, it := range items {
		row := rowView{ID: it.ID, URL: c.BookURL(it.ID), Title: it.Title,
			Byline: byline(it.Authors, seriesText(it.SeriesName, it.SeriesNumber)), Note: rowNote(it),
			Spine: SpineColor(it.Title), Initial: initial(it.Title), Cover: coverURL(it.ID, it.CoverVersion), Active: it.ID == openID}
		switch it.Shelf {
		case ShelfReading:
			row.Bar, row.Percent = it.Progress.Set(), it.Progress.Percent(it.Pages)
```

with:

```go
	for _, it := range items {
		row := rowView{ID: it.ID, URL: c.BookURL(it.ID), Title: it.Title,
			Byline: byline(it.Authors, seriesText(it.SeriesName, it.SeriesNumber)), Note: rowNote(it),
			Spine: SpineColor(it.Title), Initial: initial(it.Title), Cover: coverURL(it.ID, it.CoverVersion),
			Snippet: newSnippet(it), Active: it.ID == openID}
		switch it.Shelf {
		case ShelfReading:
			row.Bar, row.Percent = it.Progress.Set(), it.Progress.Percent(it.Pages)
```

- [ ] **Step 4: The row's third line and the filter box**

In `internal/apps/books/templates/panes.partial.html`, replace:

```html
			     replaces the box being typed in (PATTERNS.md, SyncSearch). */}}
			<form class="books-filter" method="get" action="/books/" role="search">
				{{template "filter-ctx" (dict "Ctx" .Ctx "OOB" false)}}
				<label class="visually-hidden" for="books-q">Filter by title or author</label>
				<input type="search" id="books-q" name="q" value="{{.Ctx.Q}}" placeholder="Filter by title or author…"
				       hx-get="/books/" hx-target="#books-list" hx-swap="outerHTML"
				       hx-include="closest form" hx-trigger="input changed delay:300ms, search"
				       hx-replace-url="true">
```

with:

```html
			     replaces the box being typed in (PATTERNS.md, SyncSearch). */}}
			<form class="books-filter" method="get" action="/books/" role="search">
				{{template "filter-ctx" (dict "Ctx" .Ctx "OOB" false)}}
				<label class="visually-hidden" for="books-q">Search your books</label>
				<input type="search" id="books-q" name="q" value="{{.Ctx.Q}}" placeholder="Search books, notes and quotes…"
				       hx-get="/books/" hx-target="#books-list" hx-swap="outerHTML"
				       hx-include="closest form" hx-trigger="input changed delay:300ms, search"
				       hx-replace-url="true">
```

In `internal/apps/books/templates/panes.partial.html`, replace:

```html
					<span class="books-row-title">{{.Title}}</span>
					{{with .Byline}}<span class="books-row-byline">{{.}}</span>{{end}}
					<span class="books-row-note">{{if .Bar}}<progress class="books-row-bar" max="100" value="{{.Percent}}" aria-hidden="true"></progress> {{end}}{{if .Stars}}<span class="books-stars" role="img" aria-label="Rated {{.Rating}} of 5">{{.Stars}}</span> {{end}}{{.Note}}</span>
				</span>
			</a>
		</li>
```

with:

```html
					<span class="books-row-title">{{.Title}}</span>
					{{with .Byline}}<span class="books-row-byline">{{.}}</span>{{end}}
					<span class="books-row-note">{{if .Bar}}<progress class="books-row-bar" max="100" value="{{.Percent}}" aria-hidden="true"></progress> {{end}}{{if .Stars}}<span class="books-stars" role="img" aria-label="Rated {{.Rating}} of 5">{{.Stars}}</span> {{end}}{{.Note}}</span>
					{{with .Snippet}}<span class="books-row-snippet"><span class="books-snippet-in">{{.In}}</span> {{range .Parts}}{{if .Hit}}<mark>{{.Text}}</mark>{{else}}{{.Text}}{{end}}{{end}}</span>{{end}}
				</span>
			</a>
		</li>
```

In `internal/ui/static/app.css`, replace:

```css
.books-row-note { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.books-row-byline,
.books-row-note { color: var(--c-text-dim); font-size: var(--fs-sm); }

/* Book */
.books-book-head { display: flex; flex-wrap: wrap; align-items: flex-start; gap: var(--s-4); margin-bottom: var(--s-4); }
```

with:

```css
.books-row-note { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.books-row-byline,
.books-row-note { color: var(--c-text-dim); font-size: var(--fs-sm); }
/* A filtered row's third line: where the filter matched (B3), as ON Later
 * shows it, held to two lines. */
.books-row-snippet { display: -webkit-box; -webkit-box-orient: vertical; -webkit-line-clamp: 2; overflow: hidden; color: var(--c-text-dim); font-size: var(--fs-sm); overflow-wrap: anywhere; }
.books-row-snippet mark { background: var(--c-accent-attention-bg); color: inherit; border-radius: 2px; }
.books-snippet-in { color: var(--c-text-faint); }

/* Book */
.books-book-head { display: flex; flex-wrap: wrap; align-items: flex-start; gap: var(--s-4); margin-bottom: var(--s-4); }
```

- [ ] **Step 5: Run the tests, vet and staticcheck**

```bash
go test ./internal/apps/books/... -count=1
go vet ./...
go run honnef.co/go/tools/cmd/staticcheck@v0.8.0 ./...
```
Expected: all pass.

- [ ] **Step 6: Commit**

```bash
git add internal/apps/books internal/ui/static/app.css
git commit -m "feat(books): show where a filtered book matched (#489)"
```

---

### Task 4: Notes in the book pane

**Files:**
- Modify: `internal/apps/books/handlers.go` (`paneOpts`, `renderPanes`), `internal/apps/books/view.go` (`bookView`, `viewBook`, new note view types), `internal/apps/books/actions.go` (`readingID` → `childID`, note handlers), `internal/apps/books/books.go` (routes), `internal/apps/books/templates/panes.partial.html` (`notes-swap`, `book`, `notes`, `note-form`), `internal/apps/books/static/books.js` (`n`), `internal/ui/static/app.css`
- Test: `internal/apps/books/notes_view_test.go`

**Interfaces:**
- Consumes: Task 1's `Notes`, `AddNote`, `UpdateNote`, `DeleteNote`, `NoteInput`, `Note`; `RenderReview`; `act`, `renderPanes`, `ctxFrom`, `post-ctx`; test helpers `postHXTo`, `httptestGet`, `add`, `titled`, `newServer`.
- Produces:
  - `paneOpts.Draft entryDraft` — a refused note or quote form; `renderPanes` answers 422 (no JS) when `Draft.Error` is set
  - `type entryDraft struct { Form, Error string; Page, Body string }` — `Form` is `"note-new"` or `"note-<id>"`
  - `type entryForm struct { Open bool; Error string; Page, Body string }`; `formFor(key string, d entryDraft, stored entryForm) entryForm`
  - `type noteView struct { ID int64; Date, Page string; HTML template.HTML; Form entryForm }`; `viewNotes(ns []Note, d entryDraft) []noteView`; `pageText(page int) string` ("p. 112"), `pageValue(page int) string`
  - `bookView.Pages int`, `bookView.Notes []noteView`, `bookView.NewNote entryForm`
  - `childID(r *http.Request, name string) (int64, error)` (replaces `readingID`; `ErrNotFound` for a bad segment); `pageField(r) int`; `type entrySave func(r *http.Request, userID, id int64) (entryDraft, error)`; `(*App).saveEntry(save entrySave) http.HandlerFunc`
  - routes `POST /books/notes/{id}` (fields `body`, `page`), `POST /books/notes/{id}/{nid}`, `POST /books/notes/{id}/{nid}/delete`
  - templates `notes` (section `#books-notes`; add disclosure `details#books-note-new`, textarea `#books-note-new-body`), `note-form`, `notes-swap`; htmx target `#books-notes` → block `notes-swap` (the section, and the list out of band)
  - test helpers: `readBook(t, s, title, pages) int64`, `noteIDs(t, s, id) []int64`, `texts(doc, selector) []string`, `isOpen(doc, selector) bool`

- [ ] **Step 1: Write the failing tests**

Create `internal/apps/books/notes_view_test.go`:

```go
package books_test

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/iliafrenkel/on-suite/internal/apps/books"
	"github.com/iliafrenkel/on-suite/internal/htmlassert"
)

// readBook adds one of Alice's books, already read, with pages pages.
func readBook(t *testing.T, s *server, title string, pages int) int64 {
	t.Helper()
	nb := titled(title, "", books.ShelfRead)
	nb.Pages = pages
	return add(t, s, s.Alice.User.ID, nb)
}

func noteIDs(t *testing.T, s *server, id int64) []int64 {
	t.Helper()
	ns, err := s.Store.Notes(context.Background(), s.Alice.User.ID, id)
	if err != nil {
		t.Fatal(err)
	}
	var out []int64
	for _, n := range ns {
		out = append(out, n.ID)
	}
	return out
}

// texts is the text of every element selector matches, in page order.
func texts(doc *htmlassert.Doc, selector string) []string {
	var out []string
	for _, n := range doc.QueryAll(selector) {
		out = append(out, htmlassert.Text(n))
	}
	return out
}

func isOpen(doc *htmlassert.Doc, selector string) bool {
	_, ok := htmlassert.Attr(doc.MustHave(selector), "open")
	return ok
}

func TestTheBookPaneShowsNotesNewestFirst(t *testing.T) {
	s := newServer(t)
	ctx := context.Background()
	uid := s.Alice.User.ID
	s.Clock.Set(time.Date(2026, 10, 1, 1, 0, 0, 0, time.UTC))
	id := readBook(t, s, "Dune", 600)
	if _, err := s.Store.AddNote(ctx, uid, id, books.NoteInput{Page: 112, Body: "The **spice**."}); err != nil {
		t.Fatal(err)
	}
	s.Clock.Set(time.Date(2026, 10, 5, 1, 0, 0, 0, time.UTC))
	if _, err := s.Store.AddNote(ctx, uid, id, books.NoteInput{Body: "No page."}); err != nil {
		t.Fatal(err)
	}
	rec := s.Do(t, s.Alice, httptestGet(fmt.Sprintf("/books/b/%d", id)))
	doc := htmlassert.Parse(t, rec.Body.String())
	if got := strings.Join(texts(doc, ".books-note .books-entry-meta"), "|"); got != "5 Oct 2026|1 Oct 2026 · p. 112" {
		t.Errorf("note dates = %q", got)
	}
	if got := htmlassert.Text(doc.MustHave(".books-note strong")); got != "spice" {
		t.Errorf("Markdown in a note = %q", got)
	}
	if isOpen(doc, "details#books-note-new") {
		t.Error("the add-note box is open on a plain page load")
	}
	if v, _ := htmlassert.Attr(doc.MustHave("input#books-note-new-page"), "max"); v != "600" {
		t.Errorf("page box max = %q, want 600", v)
	}
	if n := len(doc.QueryAll(".books-note button[hx-confirm]")); n != 2 {
		t.Errorf("%d delete buttons, want one per note", n)
	}
	body := rec.Body.String()
	if !(strings.Index(body, `id="books-notes"`) < strings.Index(body, `id="books-history-head"`)) {
		t.Error("notes come after the reading history; want them before (spec order)")
	}

	other := add(t, s, uid, titled("Emma", "", books.ShelfWant))
	doc = s.Get(t, s.Alice, fmt.Sprintf("/books/b/%d", other))
	doc.MustHave("details#books-note-new")
	doc.MustNotHave(".books-note")
}

func TestAddingANote(t *testing.T) {
	s := newServer(t)
	id := readBook(t, s, "Dune", 600)
	path := fmt.Sprintf("/books/notes/%d", id)
	s.Submit(t, s.Alice, path, url.Values{"shelf": {"read"}, "page": {" 112 "}, "body": {"Without JavaScript."}},
		fmt.Sprintf("/books/b/%d?shelf=read", id))

	rec := postHXTo(t, s, path, "books-notes", url.Values{"shelf": {"read"}, "body": {"With htmx."}})
	if rec.Code != http.StatusOK {
		t.Fatalf("htmx add = %d, want 200", rec.Code)
	}
	doc := htmlassert.Parse(t, rec.Body.String())
	doc.MustHave("section#books-notes")
	if v, _ := htmlassert.Attr(doc.MustHave("#books-list"), "hx-swap-oob"); v != "true" {
		t.Errorf("list hx-swap-oob = %q, want the list out of band", v)
	}
	doc.MustNotHave("#books-panes")
	if got := strings.Join(texts(doc, ".books-entry-text"), "|"); got != "With htmx.|Without JavaScript." {
		t.Errorf("notes = %q", got)
	}
	if n := len(noteIDs(t, s, id)); n != 2 {
		t.Errorf("%d notes stored, want 2", n)
	}
}

func TestARefusedNoteComesBackInItsForm(t *testing.T) {
	s := newServer(t)
	id := readBook(t, s, "Dune", 600)
	path := fmt.Sprintf("/books/notes/%d", id)
	form := url.Values{"shelf": {"read"}, "page": {"0"}, "body": {"Keep what I typed."}}
	rec := postHXTo(t, s, path, "books-notes", form)
	if rec.Code != http.StatusOK {
		t.Fatalf("htmx refusal = %d, want a 200 fragment", rec.Code)
	}
	doc := htmlassert.Parse(t, rec.Body.String())
	if !isOpen(doc, "details#books-note-new") {
		t.Error("the add-note box is closed; want it open with the message")
	}
	if got := htmlassert.Text(doc.MustHave("#books-note-new-error")); got != "Enter a page from 1 to 600." {
		t.Errorf("message = %q", got)
	}
	if got := htmlassert.Text(doc.MustHave("textarea#books-note-new-body")); got != "Keep what I typed." {
		t.Errorf("textarea = %q, want what was typed", got)
	}
	if v, _ := htmlassert.Attr(doc.MustHave("input#books-note-new-page"), "value"); v != "0" {
		t.Errorf("page box = %q, want what was typed", v)
	}

	rec = s.Post(t, s.Alice, path, url.Values{"shelf": {"read"}, "body": {"  "}})
	if rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("refused without JavaScript = %d, want 422", rec.Code)
	}
	doc = htmlassert.Parse(t, rec.Body.String())
	if got := htmlassert.Text(doc.MustHave("#books-note-new-error")); got != "Write something in the note first." {
		t.Errorf("message = %q", got)
	}
	for _, page := range []string{"abc", "-3", "2.5"} {
		rec = postHXTo(t, s, path, "books-notes", url.Values{"page": {page}, "body": {"x"}})
		doc = htmlassert.Parse(t, rec.Body.String())
		if got := htmlassert.Text(doc.MustHave("#books-note-new-error")); got != "Enter a page from 1 to 600." {
			t.Errorf("page %q: message = %q", page, got)
		}
	}
	if n := len(noteIDs(t, s, id)); n != 0 {
		t.Errorf("%d notes stored by refused posts", n)
	}
}

func TestEditingAndDeletingANoteInThePane(t *testing.T) {
	s := newServer(t)
	id := readBook(t, s, "Dune", 600)
	nid, err := s.Store.AddNote(context.Background(), s.Alice.User.ID, id, books.NoteInput{Page: 5, Body: "Draft."})
	if err != nil {
		t.Fatal(err)
	}
	doc := s.Get(t, s.Alice, fmt.Sprintf("/books/b/%d", id))
	if got := htmlassert.Text(doc.MustHave(fmt.Sprintf("textarea#books-note-%d-body", nid))); got != "Draft." {
		t.Errorf("edit box = %q", got)
	}
	if v, _ := htmlassert.Attr(doc.MustHave(fmt.Sprintf("input#books-note-%d-page", nid)), "value"); v != "5" {
		t.Errorf("edit page box = %q", v)
	}

	edit := fmt.Sprintf("/books/notes/%d/%d", id, nid)
	s.Submit(t, s.Alice, edit, url.Values{"shelf": {"read"}, "page": {""}, "body": {"Final."}}, fmt.Sprintf("/books/b/%d?shelf=read", id))
	ns, _ := s.Store.Notes(context.Background(), s.Alice.User.ID, id)
	if len(ns) != 1 || ns[0].Body != "Final." || ns[0].Page != 0 {
		t.Errorf("edited notes = %+v", ns)
	}

	rec := postHXTo(t, s, edit, "books-notes", url.Values{"body": {""}})
	doc = htmlassert.Parse(t, rec.Body.String())
	if !isOpen(doc, ".books-entry-edit") {
		t.Error("the refused note's Edit box is closed")
	}
	if got := htmlassert.Text(doc.MustHave(fmt.Sprintf("#books-note-%d-error", nid))); got != "Write something in the note first." {
		t.Errorf("message = %q", got)
	}
	if isOpen(doc, "details#books-note-new") {
		t.Error("the add-note box opened for an edit's refusal")
	}

	rec = postHXTo(t, s, edit+"/delete", "books-notes", url.Values{"shelf": {"read"}})
	if rec.Code != http.StatusOK {
		t.Fatalf("htmx delete = %d", rec.Code)
	}
	htmlassert.Parse(t, rec.Body.String()).MustNotHave(".books-note")
	if n := len(noteIDs(t, s, id)); n != 0 {
		t.Errorf("%d notes after delete", n)
	}
}

func TestNoteRoutesAreNotFoundForOthers(t *testing.T) {
	s := newServer(t)
	id := readBook(t, s, "Dune", 600)
	nid, err := s.Store.AddNote(context.Background(), s.Alice.User.ID, id, books.NoteInput{Body: "Mine."})
	if err != nil {
		t.Fatal(err)
	}
	other := add(t, s, s.Alice.User.ID, titled("Emma", "", books.ShelfWant))
	for _, tt := range []struct {
		sess string
		path string
	}{
		{"bob", fmt.Sprintf("/books/notes/%d", id)},
		{"bob", fmt.Sprintf("/books/notes/%d/%d", id, nid)},
		{"bob", fmt.Sprintf("/books/notes/%d/%d/delete", id, nid)},
		{"alice", fmt.Sprintf("/books/notes/%d/%d", other, nid)},
		{"alice", fmt.Sprintf("/books/notes/%d/%d/delete", other, nid)},
		{"alice", fmt.Sprintf("/books/notes/%d/x", id)},
	} {
		sess := s.Alice
		if tt.sess == "bob" {
			sess = s.Bob
		}
		if rec := s.Post(t, sess, tt.path, url.Values{"body": {"x"}}); rec.Code != http.StatusNotFound {
			t.Errorf("%s POST %s = %d, want 404", tt.sess, tt.path, rec.Code)
		}
	}
	if ns, _ := s.Store.Notes(context.Background(), s.Alice.User.ID, id); len(ns) != 1 || ns[0].Body != "Mine." {
		t.Errorf("notes = %+v, want Alice's note untouched", ns)
	}
}
```

- [ ] **Step 2: Run them to see them fail**

Run: `go test ./internal/apps/books/... -count=1`
Expected: FAIL — no `.books-note` elements or `details#books-note-new`, and `POST /books/notes/… = 404, want 303` (no route yet).

- [ ] **Step 3: Load the notes and pick the swap**

In `internal/apps/books/handlers.go`, replace:

```go
// paneOpts is what a render shows besides list c: the open book (0: none)
// and a refusal — in the banner, or for a progress update inside the
// progress box, with what was typed (spec "Errors": an inline message).
type paneOpts struct {
	BookID        int64
	Banner        string
	ProgressError string
	ProgressInput string
}

// renderPanes draws the panes for list c as opts says. A normal request
```

with:

```go
// paneOpts is what a render shows besides list c: the open book (0: none)
// and a refusal — in the banner, or for a progress update inside the
// progress box, or for a note or quote inside its form, with what was
// typed (spec "Errors": an inline message).
type paneOpts struct {
	BookID        int64
	Banner        string
	ProgressError string
	ProgressInput string
	Draft         entryDraft
}

// renderPanes draws the panes for list c as opts says. A normal request
```

In `internal/apps/books/handlers.go`, replace:

```go
// it targeted, as Reader's renderPanes does (#453): #books-list → list-swap
// (the list and its out-of-band companions, the book pane untouched),
// #books-book → book-swap, #books-progress → progress-swap (the box, and
// the list out of band), anything else (#books-panes) → the whole panes.
// Fragments are always 200: htmx's default responseHandling only swaps
// 2xx/3xx.
func (a *App) renderPanes(w http.ResponseWriter, r *http.Request, userID int64, c listCtx, opts paneOpts) {
```

with:

```go
// it targeted, as Reader's renderPanes does (#453): #books-list → list-swap
// (the list and its out-of-band companions, the book pane untouched),
// #books-book → book-swap, #books-progress → progress-swap (the box, and
// the list out of band), #books-notes → notes-swap (the same for the
// notes), anything else (#books-panes) → the whole panes.
// Fragments are always 200: htmx's default responseHandling only swaps
// 2xx/3xx.
func (a *App) renderPanes(w http.ResponseWriter, r *http.Request, userID int64, c listCtx, opts paneOpts) {
```

In `internal/apps/books/handlers.go`, replace:

```go
			a.fail(w, r, err)
			return
		}
		bv = viewBook(b, c, a.store.Today())
		bv.History = viewHistory(rs)
		title = b.Title
		if opts.ProgressError != "" {
			bv.Progress.Error, bv.Progress.Value = opts.ProgressError, opts.ProgressInput
```

with:

```go
			a.fail(w, r, err)
			return
		}
		ns, err := a.store.Notes(ctx, userID, opts.BookID)
		if err != nil {
			a.fail(w, r, err)
			return
		}
		bv = viewBook(b, c, a.store.Today())
		bv.History = viewHistory(rs)
		bv.Notes, bv.NewNote = viewNotes(ns, opts.Draft), formFor("note-new", opts.Draft, entryForm{})
		title = b.Title
		if opts.ProgressError != "" {
			bv.Progress.Error, bv.Progress.Value = opts.ProgressError, opts.ProgressInput
```

In `internal/apps/books/handlers.go`, replace:

```go
		case "books-progress":
			block = "progress-swap"
			v.List.OOB = true
		}
		if err := a.deps.Render.Fragment(w, http.StatusOK, "books/index", block, v); err != nil {
			a.deps.Errors.Internal(w, r, err)
```

with:

```go
		case "books-progress":
			block = "progress-swap"
			v.List.OOB = true
		case "books-notes":
			block = "notes-swap"
			v.List.OOB = true
		}
		if err := a.deps.Render.Fragment(w, http.StatusOK, "books/index", block, v); err != nil {
			a.deps.Errors.Internal(w, r, err)
```

In `internal/apps/books/handlers.go`, replace:

```go
		return
	}
	status := http.StatusOK
	if opts.Banner != "" || opts.ProgressError != "" {
		status = http.StatusUnprocessableEntity
	}
	page.Data = v
```

with:

```go
		return
	}
	status := http.StatusOK
	if opts.Banner != "" || opts.ProgressError != "" || opts.Draft.Error != "" {
		status = http.StatusUnprocessableEntity
	}
	page.Data = v
```

- [ ] **Step 4: The note view model**

In `internal/apps/books/view.go`, replace:

```go
	Review        string       // Markdown, for the edit box
	ReviewHTML    template.HTML
	History       []historyView // every reading, newest first
	Ctx           listCtx
	Shell         render.Shell
}
```

with:

```go
	Review        string       // Markdown, for the edit box
	ReviewHTML    template.HTML
	History       []historyView // every reading, newest first
	Pages         int           // the book's page count (0: unknown), the page boxes' max
	Notes         []noteView    // newest first
	NewNote       entryForm     // the "+ Add note" form
	Ctx           listCtx
	Shell         render.Shell
}
```

In `internal/apps/books/view.go`, replace:

```go
func viewBook(b Book, c listCtx, today string) bookView {
	v := bookView{Selected: true, ID: b.ID, Title: b.Title, Subtitle: b.Subtitle, Authors: b.Authors,
		Description: b.Description, Spine: SpineColor(b.Title), Cover: coverURL(b.ID, b.CoverVersion),
		ShelfLabel: b.Shelf.Label(), Tags: b.Tags, TagsValue: strings.Join(b.Tags, ", "), Today: today,
		Rating: b.Rating, Stars: starButtons(b.Rating), Review: b.Review, ReviewHTML: RenderReview(b.Review), Ctx: c}
	if b.SeriesName != "" {
		v.Series = seriesText(b.SeriesName, b.SeriesNumber)
```

with:

```go
func viewBook(b Book, c listCtx, today string) bookView {
	v := bookView{Selected: true, ID: b.ID, Title: b.Title, Subtitle: b.Subtitle, Authors: b.Authors,
		Description: b.Description, Spine: SpineColor(b.Title), Cover: coverURL(b.ID, b.CoverVersion),
		ShelfLabel: b.Shelf.Label(), Tags: b.Tags, TagsValue: strings.Join(b.Tags, ", "), Today: today, Pages: b.Pages,
		Rating: b.Rating, Stars: starButtons(b.Rating), Review: b.Review, ReviewHTML: RenderReview(b.Review), Ctx: c}
	if b.SeriesName != "" {
		v.Series = seriesText(b.SeriesName, b.SeriesNumber)
```

In `internal/apps/books/view.go`, replace:

```go
	return "No dates"
}

// progressView is the progress box: an input in the reading's unit, a
// bar and a note. Error and a typed Value come from a refused update.
type progressView struct {
```

with:

```go
	return "No dates"
}

// entryDraft is a note or quote form the store refused, to show again
// with its message and what was typed (spec "Errors": an inline message,
// as for progress). Form names the form: "note-new" or "note-12".
type entryDraft struct {
	Form, Error string
	Page, Body  string
}

// entryForm is a note or quote form's state: what its boxes hold, and
// whether it opens with a message.
type entryForm struct {
	Open       bool
	Error      string
	Page, Body string
}

// formFor is the form named key: what was typed into it when it is the
// one refused (opened, with the message), its stored values otherwise.
func formFor(key string, d entryDraft, stored entryForm) entryForm {
	if d.Form != key {
		return stored
	}
	return entryForm{Open: true, Error: d.Error, Page: d.Page, Body: d.Body}
}

// noteView is one note in the book pane: dated, with its page, its
// Markdown drawn as a review is (decided 2026-10-09), and its Edit form.
type noteView struct {
	ID   int64
	Date string // "9 Oct 2026"
	Page string // "p. 112"; "" for none
	HTML template.HTML
	Form entryForm
}

func viewNotes(ns []Note, d entryDraft) []noteView {
	var out []noteView
	for _, n := range ns {
		out = append(out, noteView{ID: n.ID, Date: n.CreatedAt.Local().Format("2 Jan 2006"), Page: pageText(n.Page),
			HTML: RenderReview(n.Body),
			Form: formFor("note-"+strconv.FormatInt(n.ID, 10), d, entryForm{Page: pageValue(n.Page), Body: n.Body})})
	}
	return out
}

// pageText is a page for people: "p. 112", "" for none.
func pageText(page int) string {
	if page == 0 {
		return ""
	}
	return "p. " + strconv.Itoa(page)
}

// pageValue is a page for a form box, "" for none.
func pageValue(page int) string {
	if page == 0 {
		return ""
	}
	return strconv.Itoa(page)
}

// progressView is the progress box: an input in the reading's unit, a
// bar and a note. Error and a typed Value come from a refused update.
type progressView struct {
```

- [ ] **Step 5: The note handlers and routes**

`readingID` becomes `childID(r, name)`, so readings, notes and quotes share it.

In `internal/apps/books/actions.go`, replace:

```go
	return a.store.SetReview(r.Context(), userID, id, r.PostFormValue("review"))
}

// readingID is the {rid} path segment. Anything but a positive integer is
// ErrNotFound, so act answers 404, as for a reading that isn't there.
func readingID(r *http.Request) (int64, error) {
	rid, err := strconv.ParseInt(r.PathValue("rid"), 10, 64)
	if err != nil || rid <= 0 {
		return 0, ErrNotFound
	}
	return rid, nil
}

func (a *App) editReading(r *http.Request, userID, id int64) error {
	rid, err := readingID(r)
	if err != nil {
		return err
	}
```

with:

```go
	return a.store.SetReview(r.Context(), userID, id, r.PostFormValue("review"))
}

// childID is the path segment name — {rid}, {nid}, {qid}: a reading, note
// or quote of the book. Anything but a positive integer is ErrNotFound, so
// it answers 404, as for one that isn't there.
func childID(r *http.Request, name string) (int64, error) {
	cid, err := strconv.ParseInt(r.PathValue(name), 10, 64)
	if err != nil || cid <= 0 {
		return 0, ErrNotFound
	}
	return cid, nil
}

func (a *App) editReading(r *http.Request, userID, id int64) error {
	rid, err := childID(r, "rid")
	if err != nil {
		return err
	}
```

In `internal/apps/books/actions.go`, replace:

```go
}

func (a *App) deleteReading(r *http.Request, userID, id int64) error {
	rid, err := readingID(r)
	if err != nil {
		return err
	}
	return a.store.DeleteReading(r.Context(), userID, id, rid)
}

func (a *App) setTags(r *http.Request, userID, id int64) error {
	return a.store.SetTags(r.Context(), userID, id, ParseTags(r.PostFormValue("tags")))
}
```

with:

```go
}

func (a *App) deleteReading(r *http.Request, userID, id int64) error {
	rid, err := childID(r, "rid")
	if err != nil {
		return err
	}
	return a.store.DeleteReading(r.Context(), userID, id, rid)
}

// pageField reads a note's or quote's optional page: 0 when empty, -1
// when it isn't a whole number of 1 or more, which the store refuses with
// its own message (decided 2026-10-09).
func pageField(r *http.Request) int {
	s := strings.TrimSpace(r.PostFormValue("page"))
	if s == "" {
		return 0
	}
	n, err := strconv.Atoi(s)
	if err != nil || n < 1 {
		return -1
	}
	return n
}

// entrySave is a note or quote form's save: what was typed, kept to show
// again if the store refuses it, and the store's answer.
type entrySave func(r *http.Request, userID, id int64) (entryDraft, error)

// saveEntry answers a note or quote form. htmx aims it at the section, so
// the answer is the section with the list out of band (renderPanes);
// without JavaScript it is a redirect back to the book. A refusal comes
// back inside the form, opened, with what was typed — a 200 fragment for
// htmx, a 422 page without.
func (a *App) saveEntry(save entrySave) http.HandlerFunc {
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
		d, err := save(r, uid, id)
		var ref *Refusal
		switch {
		case errors.As(err, &ref):
			d.Error = ref.Msg
			a.renderPanes(w, r, uid, c, paneOpts{BookID: id, Draft: d})
			return
		case err != nil:
			a.fail(w, r, err)
			return
		}
		if web.IsHTMX(r) {
			a.renderPanes(w, r, uid, c, paneOpts{BookID: id})
			return
		}
		http.Redirect(w, r, c.BookURL(id), http.StatusSeeOther)
	}
}

// noteDraft is a posted note form, named form.
func noteDraft(r *http.Request, form string) entryDraft {
	return entryDraft{Form: form, Page: strings.TrimSpace(r.PostFormValue("page")), Body: r.PostFormValue("body")}
}

func (a *App) addNote(r *http.Request, userID, id int64) (entryDraft, error) {
	d := noteDraft(r, "note-new")
	_, err := a.store.AddNote(r.Context(), userID, id, NoteInput{Page: pageField(r), Body: d.Body})
	return d, err
}

func (a *App) editNote(r *http.Request, userID, id int64) (entryDraft, error) {
	nid, err := childID(r, "nid")
	if err != nil {
		return entryDraft{}, err
	}
	d := noteDraft(r, "note-"+strconv.FormatInt(nid, 10))
	return d, a.store.UpdateNote(r.Context(), userID, id, nid, NoteInput{Page: pageField(r), Body: d.Body})
}

func (a *App) deleteNote(r *http.Request, userID, id int64) error {
	nid, err := childID(r, "nid")
	if err != nil {
		return err
	}
	return a.store.DeleteNote(r.Context(), userID, id, nid)
}

func (a *App) setTags(r *http.Request, userID, id int64) error {
	return a.store.SetTags(r.Context(), userID, id, ParseTags(r.PostFormValue("tags")))
}
```

In `internal/apps/books/books.go`, replace:

```go
	r.HandleFunc("POST /review/{id}", a.act(a.setReview, false))
	r.HandleFunc("POST /readings/{id}/{rid}", a.act(a.editReading, false))
	r.HandleFunc("POST /readings/{id}/{rid}/delete", a.act(a.deleteReading, false))
	r.HandleFunc("POST /tags/{id}", a.act(a.setTags, false))
	r.HandleFunc("POST /delete/{id}", a.act(a.remove, true))
	r.HandleFunc("GET /cover/{id}", a.cover)
```

with:

```go
	r.HandleFunc("POST /review/{id}", a.act(a.setReview, false))
	r.HandleFunc("POST /readings/{id}/{rid}", a.act(a.editReading, false))
	r.HandleFunc("POST /readings/{id}/{rid}/delete", a.act(a.deleteReading, false))
	r.HandleFunc("POST /notes/{id}", a.saveEntry(a.addNote))
	r.HandleFunc("POST /notes/{id}/{nid}", a.saveEntry(a.editNote))
	r.HandleFunc("POST /notes/{id}/{nid}/delete", a.act(a.deleteNote, false))
	r.HandleFunc("POST /tags/{id}", a.act(a.setTags, false))
	r.HandleFunc("POST /delete/{id}", a.act(a.remove, true))
	r.HandleFunc("GET /cover/{id}", a.cover)
```

- [ ] **Step 6: The Notes section**

In `internal/apps/books/templates/panes.partial.html`, replace:

```html
     shelf's order follow (spec "Progress and finishing"). */}}
{{define "progress-swap"}}{{template "progress" .Book}}{{template "list" .List}}{{end}}

{{/* book-swap answers opening a book (target #books-book). The checkbox
     out of band is what makes a phone drill into the book. */}}
{{define "book-swap"}}{{template "shell-oob" .}}{{template "book" .Book}}{{template "pane-toggle" (dict "ID" "books-book-open" "Label" "Books" "Checked" true "OOB" true)}}{{end}}
```

with:

```html
     shelf's order follow (spec "Progress and finishing"). */}}
{{define "progress-swap"}}{{template "progress" .Book}}{{template "list" .List}}{{end}}

{{/* notes-swap answers a note being added, edited or deleted (target
     #books-notes): the section, and the list out of band — the row's
     snippet and its place on a shelf sorted by the latest change can both
     move. */}}
{{define "notes-swap"}}{{template "notes" .Book}}{{template "list" .List}}{{end}}

{{/* book-swap answers opening a book (target #books-book). The checkbox
     out of band is what makes a phone drill into the book. */}}
{{define "book-swap"}}{{template "shell-oob" .}}{{template "book" .Book}}{{template "pane-toggle" (dict "ID" "books-book-open" "Label" "Books" "Checked" true "OOB" true)}}{{end}}
```

In `internal/apps/books/templates/panes.partial.html`, replace:

```html
	{{template "review" .}}
	{{with .Description}}<div class="books-description">{{.}}</div>{{end}}
	{{template "tags-form" .}}
	{{template "history" .}}
{{else}}
	<p class="empty">Pick a book, or add one.</p>
```

with:

```html
	{{template "review" .}}
	{{with .Description}}<div class="books-description">{{.}}</div>{{end}}
	{{template "tags-form" .}}
	{{template "notes" .}}
	{{template "history" .}}
{{else}}
	<p class="empty">Pick a book, or add one.</p>
```

In `internal/apps/books/templates/panes.partial.html`, replace:

```html
</form>
{{end}}

{{/* history is every reading of the book, newest first (spec "Book
     pane"), each with a no-JS <details> to change its dates and format,
     and Delete (decided 2026-10-09: a reading's status isn't editable;
```

with:

```html
</form>
{{end}}

{{/* notes is the book's notes, newest first (spec "Book pane"): a
     "+ Add note" disclosure, then each note with its date and page, an
     Edit disclosure and Delete (decided 2026-10-09). Every form works
     without JavaScript; htmx swaps just this section (notes-swap). Takes a
     bookView. */}}
{{define "notes"}}
<section class="books-notes" id="books-notes" aria-labelledby="books-notes-head">
	<h2 class="books-section-head" id="books-notes-head">Notes</h2>
	<details class="books-entry-add" id="books-note-new"{{if .NewNote.Open}} open{{end}}>
		<summary class="button">+ Add note</summary>
		{{template "note-form" (dict "Book" . "Key" "new" "Action" (printf "/books/notes/%d" .ID) "Form" .NewNote "Submit" "Add note")}}
	</details>
	{{with .Notes}}
	<ul class="books-entries">
		{{range .}}
		<li class="books-note">
			<p class="books-entry-meta">{{.Date}}{{with .Page}} · {{.}}{{end}}</p>
			<div class="books-entry-text">{{.HTML}}</div>
			<div class="books-entry-actions">
				<details class="books-entry-edit"{{if .Form.Open}} open{{end}}>
					<summary class="button quiet">Edit</summary>
					{{template "note-form" (dict "Book" $ "Key" .ID "Action" (printf "/books/notes/%d/%d" $.ID .ID) "Form" .Form "Submit" "Save note")}}
				</details>
				<form method="post" action="/books/notes/{{$.ID}}/{{.ID}}/delete">
					{{template "post-ctx" $}}
					<button type="submit" class="quiet"
					        hx-post="/books/notes/{{$.ID}}/{{.ID}}/delete" hx-target="#books-notes" hx-swap="outerHTML"
					        hx-confirm="Delete this note?">Delete</button>
				</form>
			</div>
		</li>
		{{end}}
	</ul>
	{{end}}
</section>
{{end}}

{{/* note-form adds or edits a note. Pass a dict with Book (a bookView),
     Key ("new" or the note's id, for element ids), Action, Form (an
     entryForm) and Submit. novalidate, so the store's message shows rather
     than the browser's. */}}
{{define "note-form"}}
<form class="books-entry-form" novalidate method="post" action="{{.Action}}"
      hx-post="{{.Action}}" hx-target="#books-notes" hx-swap="outerHTML">
	{{template "post-ctx" .Book}}
	<label for="books-note-{{.Key}}-body">Note</label>
	<textarea id="books-note-{{.Key}}-body" name="body" rows="4"{{if .Form.Error}} aria-describedby="books-note-{{.Key}}-error"{{end}}>{{.Form.Body}}</textarea>
	<p class="books-hint">Markdown works: **bold**, *italic*, [a link](https://…).</p>
	<label for="books-note-{{.Key}}-page">Page (optional)</label>
	<input id="books-note-{{.Key}}-page" name="page" type="number" inputmode="numeric" min="1"{{with .Book.Pages}} max="{{.}}"{{end}} value="{{.Form.Page}}"{{if .Form.Error}} aria-describedby="books-note-{{.Key}}-error"{{end}}>
	{{with .Form.Error}}<p class="books-field-error" id="books-note-{{$.Key}}-error" role="alert">{{.}}</p>{{end}}
	<button type="submit" class="primary">{{.Submit}}</button>
</form>
{{end}}

{{/* history is every reading of the book, newest first (spec "Book
     pane"), each with a no-JS <details> to change its dates and format,
     and Delete (decided 2026-10-09: a reading's status isn't editable;
```

- [ ] **Step 7: `n` opens the add-note box**

The existing Esc handling already closes any open disclosure in the book pane from a field inside it, so the note boxes need nothing more.

In `internal/apps/books/static/books.js`, replace:

```js
	// --- Keyboard shortcuts --------------------------------------------------

	// Keys are ignored while typing, so "/" in the filter box is a slash.
	function isTyping(el) {
		if (!el) return false;
```

with:

```js
	// --- Keyboard shortcuts --------------------------------------------------

	// openEntry opens an add disclosure in the book pane ("+ Add note") and
	// puts the cursor in its box; false when the pane has none.
	function openEntry(detailsId, inputId) {
		var details = document.getElementById(detailsId);
		var input = document.getElementById(inputId);
		if (!details || !input) return false;
		details.open = true;
		input.focus();
		return true;
	}

	// Keys are ignored while typing, so "/" in the filter box is a slash.
	function isTyping(el) {
		if (!el) return false;
```

In `internal/apps/books/static/books.js`, replace:

```js
			progress.focus();
			progress.select();
			break;
		case "Escape":
			document.querySelectorAll("#books-book details[open]").forEach(function (d) {
				d.open = false;
```

with:

```js
			progress.focus();
			progress.select();
			break;
		case "n":
			if (!openEntry("books-note-new", "books-note-new-body")) return;
			break;
		case "Escape":
			document.querySelectorAll("#books-book details[open]").forEach(function (d) {
				d.open = false;
```

- [ ] **Step 8: Styles**

In `internal/ui/static/app.css`, replace:

```css
.books-reading-edit > summary::-webkit-details-marker { display: none; }
.books-reading-edit-form { display: flex; flex-wrap: wrap; align-items: flex-end; gap: var(--s-2); margin-top: var(--s-2); }
.books-reading-edit-form label { margin: 0; flex-basis: 100%; }
.books-dialog { width: min(28rem, calc(100vw - 2rem)); padding: 1rem; border: var(--border); border-radius: var(--radius); color: var(--c-text); background: var(--c-bg); }
.books-dialog::backdrop { background: rgba(0, 0, 0, 0.35); }
```

with:

```css
.books-reading-edit > summary::-webkit-details-marker { display: none; }
.books-reading-edit-form { display: flex; flex-wrap: wrap; align-items: flex-end; gap: var(--s-2); margin-top: var(--s-2); }
.books-reading-edit-form label { margin: 0; flex-basis: 100%; }
/* Notes and quotes (B3): "+ Add" and Edit disclosures, as the review's */
.books-notes { max-width: 38rem; margin-top: var(--s-4); }
.books-entry-add > summary,
.books-entry-edit > summary { display: inline-flex; list-style: none; cursor: pointer; }
.books-entry-add > summary::-webkit-details-marker,
.books-entry-edit > summary::-webkit-details-marker { display: none; }
.books-entry-form { display: flex; flex-direction: column; gap: var(--s-2); margin-top: var(--s-2); }
.books-entry-form label { margin: 0; }
.books-entry-form textarea { width: 100%; }
.books-entry-form input[type="number"] { width: 6rem; }
.books-entry-form button { align-self: flex-start; }
.books-entries { list-style: none; margin: var(--s-2) 0 0; padding: 0; }
.books-note { padding: var(--s-2) 0; border-bottom: 1px solid var(--c-border); }
.books-entry-meta { margin: 0; color: var(--c-text-dim); font-size: var(--fs-sm); }
.books-entry-text p { margin: var(--s-1) 0; }
.books-entry-actions { display: flex; flex-wrap: wrap; align-items: flex-start; gap: var(--s-2); }
.books-entry-edit[open] { flex-basis: 100%; }
.books-dialog { width: min(28rem, calc(100vw - 2rem)); padding: 1rem; border: var(--border); border-radius: var(--radius); color: var(--c-text); background: var(--c-bg); }
.books-dialog::backdrop { background: rgba(0, 0, 0, 0.35); }
```

- [ ] **Step 9: Run the tests, vet and staticcheck**

```bash
go test ./internal/apps/books/... -count=1
go vet ./...
go run honnef.co/go/tools/cmd/staticcheck@v0.8.0 ./...
```
Expected: all pass — including B2's `TestReadingRoutesAreNotFoundForOthers`, which now goes through `childID`.

- [ ] **Step 10: Commit**

```bash
git add internal/apps/books internal/ui/static/app.css
git commit -m "feat(books): notes in the book pane (#489)"
```

---

### Task 5: Quotes in the book pane

**Files:**
- Modify: `internal/apps/books/view.go` (`bookView`, `entryDraft`, `entryForm`, `formFor`, quote view types), `internal/apps/books/handlers.go` (`renderPanes`), `internal/apps/books/actions.go` (quote handlers), `internal/apps/books/books.go` (routes), `internal/apps/books/templates/panes.partial.html` (`quotes-swap`, `book`, `quotes`, `quote-form`, the delete-book confirm text), `internal/apps/books/static/books.js` (`q`), `internal/ui/static/app.css`
- Test: `internal/apps/books/quotes_view_test.go`

**Interfaces:**
- Consumes: Task 1's `Quotes`, `AddQuote`, `UpdateQuote`, `DeleteQuote`, `QuoteInput`, `Quote`; Task 4's `entryDraft`, `entryForm`, `formFor`, `pageText`, `pageValue`, `childID`, `pageField`, `saveEntry`, `openEntry`, test helpers `readBook`, `texts`, `isOpen`.
- Produces:
  - `entryDraft.Comment`, `entryForm.Comment` (a quote's comment; `Body` holds a quote's text)
  - `type quoteView struct { ID int64; Text, Page string; CommentHTML template.HTML; Form entryForm }`; `viewQuotes(qs []Quote, d entryDraft) []quoteView`
  - `bookView.Quotes []quoteView`, `bookView.NewQuote entryForm`
  - routes `POST /books/quotes/{id}` (fields `text`, `comment`, `page`), `POST /books/quotes/{id}/{qid}`, `POST /books/quotes/{id}/{qid}/delete`
  - templates `quotes` (section `#books-quotes`; `details#books-quote-new`, textarea `#books-quote-new-text`), `quote-form`, `quotes-swap`
  - test helper `quoteIDs(t, s, id) []int64`

- [ ] **Step 1: Write the failing tests**

Create `internal/apps/books/quotes_view_test.go`:

```go
package books_test

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/iliafrenkel/on-suite/internal/apps/books"
	"github.com/iliafrenkel/on-suite/internal/htmlassert"
)

func quoteIDs(t *testing.T, s *server, id int64) []int64 {
	t.Helper()
	qs, err := s.Store.Quotes(context.Background(), s.Alice.User.ID, id)
	if err != nil {
		t.Fatal(err)
	}
	var out []int64
	for _, q := range qs {
		out = append(out, q.ID)
	}
	return out
}

func TestQuotesShowAsCards(t *testing.T) {
	s := newServer(t)
	ctx := context.Background()
	uid := s.Alice.User.ID
	id := readBook(t, s, "Dune", 600)
	if _, err := s.Store.AddQuote(ctx, uid, id, books.QuoteInput{Page: 8,
		Text: "I must not fear.\nFear is the mind-killer.", Comment: "The *litany*."}); err != nil {
		t.Fatal(err)
	}
	s.Clock.Advance(time.Hour)
	if _, err := s.Store.AddQuote(ctx, uid, id, books.QuoteInput{Text: "The spice must flow."}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Store.AddNote(ctx, uid, id, books.NoteInput{Body: "A note."}); err != nil {
		t.Fatal(err)
	}
	rec := s.Do(t, s.Alice, httptestGet(fmt.Sprintf("/books/b/%d", id)))
	doc := htmlassert.Parse(t, rec.Body.String())
	cards := doc.QueryAll(".books-quote-text")
	if len(cards) != 2 {
		t.Fatalf("%d quote cards, want 2", len(cards))
	}
	if got := cards[0].FirstChild.Data; got != "The spice must flow." {
		t.Errorf("newest quote = %q", got)
	}
	if got := cards[1].FirstChild.Data; got != "I must not fear.\nFear is the mind-killer." {
		t.Errorf("older quote = %q, want its line break kept", got)
	}
	if got := strings.Join(texts(doc, ".books-quote .books-entry-meta"), "|"); got != "p. 8" {
		t.Errorf("pages = %q, want only the older quote's", got)
	}
	if got := htmlassert.Text(doc.MustHave(".books-quote-comment em")); got != "litany" {
		t.Errorf("Markdown in a comment = %q", got)
	}
	if n := len(doc.QueryAll(".books-quote-comment")); n != 1 {
		t.Errorf("%d comments, want 1: no empty comment box", n)
	}
	body := rec.Body.String()
	notes, quotes, history := strings.Index(body, `id="books-notes"`), strings.Index(body, `id="books-quotes"`), strings.Index(body, `id="books-history-head"`)
	if !(notes < quotes && quotes < history) {
		t.Errorf("section order: notes at %d, quotes at %d, history at %d; want Notes, Quotes, Reading history", notes, quotes, history)
	}
}

func TestAddingAQuote(t *testing.T) {
	s := newServer(t)
	id := readBook(t, s, "Dune", 600)
	path := fmt.Sprintf("/books/quotes/%d", id)
	s.Submit(t, s.Alice, path, url.Values{"shelf": {"read"}, "page": {"8"}, "text": {"Fear is the mind-killer."}, "comment": {"Litany."}},
		fmt.Sprintf("/books/b/%d?shelf=read", id))

	rec := postHXTo(t, s, path, "books-quotes", url.Values{"shelf": {"read"}, "text": {"The spice must flow."}})
	if rec.Code != http.StatusOK {
		t.Fatalf("htmx add = %d, want 200", rec.Code)
	}
	doc := htmlassert.Parse(t, rec.Body.String())
	doc.MustHave("section#books-quotes")
	if v, _ := htmlassert.Attr(doc.MustHave("#books-list"), "hx-swap-oob"); v != "true" {
		t.Errorf("list hx-swap-oob = %q, want the list out of band", v)
	}
	doc.MustNotHave("#books-panes")
	if got := strings.Join(texts(doc, ".books-quote-text"), "|"); got != "The spice must flow.|Fear is the mind-killer." {
		t.Errorf("quotes = %q", got)
	}
	qs, _ := s.Store.Quotes(context.Background(), s.Alice.User.ID, id)
	if len(qs) != 2 || qs[1].Page != 8 || qs[1].Comment != "Litany." {
		t.Errorf("stored quotes = %+v", qs)
	}
}

func TestARefusedQuoteComesBackInItsForm(t *testing.T) {
	s := newServer(t)
	id := readBook(t, s, "Dune", 600)
	path := fmt.Sprintf("/books/quotes/%d", id)
	rec := postHXTo(t, s, path, "books-quotes", url.Values{"shelf": {"read"}, "page": {"12"}, "text": {" "}, "comment": {"Keep me."}})
	if rec.Code != http.StatusOK {
		t.Fatalf("htmx refusal = %d, want a 200 fragment", rec.Code)
	}
	doc := htmlassert.Parse(t, rec.Body.String())
	if !isOpen(doc, "details#books-quote-new") {
		t.Error("the add-quote box is closed; want it open with the message")
	}
	if got := htmlassert.Text(doc.MustHave("#books-quote-new-error")); got != "Type the quote first." {
		t.Errorf("message = %q", got)
	}
	if got := htmlassert.Text(doc.MustHave("textarea#books-quote-new-comment")); got != "Keep me." {
		t.Errorf("comment box = %q, want what was typed", got)
	}
	if v, _ := htmlassert.Attr(doc.MustHave("input#books-quote-new-page"), "value"); v != "12" {
		t.Errorf("page box = %q, want what was typed", v)
	}
	if rec := s.Post(t, s.Alice, path, url.Values{"text": {"x"}, "page": {"601"}}); rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("refused without JavaScript = %d, want 422", rec.Code)
	}
	if n := len(quoteIDs(t, s, id)); n != 0 {
		t.Errorf("%d quotes stored by refused posts", n)
	}
}

func TestEditingAndDeletingAQuoteInThePane(t *testing.T) {
	s := newServer(t)
	id := readBook(t, s, "Dune", 600)
	qid, err := s.Store.AddQuote(context.Background(), s.Alice.User.ID, id, books.QuoteInput{Text: "Draft.", Comment: "Hm."})
	if err != nil {
		t.Fatal(err)
	}
	doc := s.Get(t, s.Alice, fmt.Sprintf("/books/b/%d", id))
	if got := htmlassert.Text(doc.MustHave(fmt.Sprintf("textarea#books-quote-%d-comment", qid))); got != "Hm." {
		t.Errorf("edit comment box = %q", got)
	}

	edit := fmt.Sprintf("/books/quotes/%d/%d", id, qid)
	s.Submit(t, s.Alice, edit, url.Values{"shelf": {"read"}, "page": {"3"}, "text": {"Final."}, "comment": {""}},
		fmt.Sprintf("/books/b/%d?shelf=read", id))
	qs, _ := s.Store.Quotes(context.Background(), s.Alice.User.ID, id)
	if len(qs) != 1 || qs[0].Text != "Final." || qs[0].Page != 3 || qs[0].Comment != "" {
		t.Errorf("edited quotes = %+v", qs)
	}

	rec := postHXTo(t, s, edit, "books-quotes", url.Values{"text": {"Final."}, "page": {"x"}})
	doc = htmlassert.Parse(t, rec.Body.String())
	if !isOpen(doc, ".books-entry-edit") {
		t.Error("the refused quote's Edit box is closed")
	}
	if got := htmlassert.Text(doc.MustHave(fmt.Sprintf("#books-quote-%d-error", qid))); got != "Enter a page from 1 to 600." {
		t.Errorf("message = %q", got)
	}

	rec = postHXTo(t, s, edit+"/delete", "books-quotes", url.Values{"shelf": {"read"}})
	if rec.Code != http.StatusOK {
		t.Fatalf("htmx delete = %d", rec.Code)
	}
	htmlassert.Parse(t, rec.Body.String()).MustNotHave(".books-quote")
	if n := len(quoteIDs(t, s, id)); n != 0 {
		t.Errorf("%d quotes after delete", n)
	}
}

func TestQuoteRoutesAreNotFoundForOthers(t *testing.T) {
	s := newServer(t)
	id := readBook(t, s, "Dune", 600)
	qid, err := s.Store.AddQuote(context.Background(), s.Alice.User.ID, id, books.QuoteInput{Text: "Mine."})
	if err != nil {
		t.Fatal(err)
	}
	other := add(t, s, s.Alice.User.ID, titled("Emma", "", books.ShelfWant))
	for _, tt := range []struct {
		sess string
		path string
	}{
		{"bob", fmt.Sprintf("/books/quotes/%d", id)},
		{"bob", fmt.Sprintf("/books/quotes/%d/%d", id, qid)},
		{"bob", fmt.Sprintf("/books/quotes/%d/%d/delete", id, qid)},
		{"alice", fmt.Sprintf("/books/quotes/%d/%d", other, qid)},
		{"alice", fmt.Sprintf("/books/quotes/%d/%d/delete", other, qid)},
		{"alice", fmt.Sprintf("/books/quotes/%d/0", id)},
	} {
		sess := s.Alice
		if tt.sess == "bob" {
			sess = s.Bob
		}
		if rec := s.Post(t, sess, tt.path, url.Values{"text": {"x"}}); rec.Code != http.StatusNotFound {
			t.Errorf("%s POST %s = %d, want 404", tt.sess, tt.path, rec.Code)
		}
	}
	if qs, _ := s.Store.Quotes(context.Background(), s.Alice.User.ID, id); len(qs) != 1 || qs[0].Text != "Mine." {
		t.Errorf("quotes = %+v, want Alice's quote untouched", qs)
	}
}
```

- [ ] **Step 2: Run them to see them fail**

Run: `go test ./internal/apps/books/... -count=1`
Expected: FAIL — `0 quote cards, want 2`, and `POST /books/quotes/… = 404, want 303`.

- [ ] **Step 3: The quote view model**

`entryDraft` and `entryForm` gain `Comment`; gofmt re-aligns both, so they are shown whole.

In `internal/apps/books/view.go`, replace:

```go
	Pages         int           // the book's page count (0: unknown), the page boxes' max
	Notes         []noteView    // newest first
	NewNote       entryForm     // the "+ Add note" form
	Ctx           listCtx
	Shell         render.Shell
}
```

with:

```go
	Pages         int           // the book's page count (0: unknown), the page boxes' max
	Notes         []noteView    // newest first
	NewNote       entryForm     // the "+ Add note" form
	Quotes        []quoteView   // newest first
	NewQuote      entryForm     // the "+ Add quote" form
	Ctx           listCtx
	Shell         render.Shell
}
```

In `internal/apps/books/view.go`, replace:

```go
// entryDraft is a note or quote form the store refused, to show again
// with its message and what was typed (spec "Errors": an inline message,
// as for progress). Form names the form: "note-new" or "note-12".
type entryDraft struct {
	Form, Error string
	Page, Body  string
}

// entryForm is a note or quote form's state: what its boxes hold, and
// whether it opens with a message.
type entryForm struct {
	Open       bool
	Error      string
	Page, Body string
}

// formFor is the form named key: what was typed into it when it is the
```

with:

```go
// entryDraft is a note or quote form the store refused, to show again
// with its message and what was typed (spec "Errors": an inline message,
// as for progress). Form names the form: "note-new", "note-12",
// "quote-new", "quote-5". Body is a note's text or a quote's.
type entryDraft struct {
	Form, Error         string
	Page, Body, Comment string
}

// entryForm is a note or quote form's state: what its boxes hold, and
// whether it opens with a message.
type entryForm struct {
	Open                bool
	Error               string
	Page, Body, Comment string
}

// formFor is the form named key: what was typed into it when it is the
```

In `internal/apps/books/view.go`, replace:

```go
	if d.Form != key {
		return stored
	}
	return entryForm{Open: true, Error: d.Error, Page: d.Page, Body: d.Body}
}

// noteView is one note in the book pane: dated, with its page, its
```

with:

```go
	if d.Form != key {
		return stored
	}
	return entryForm{Open: true, Error: d.Error, Page: d.Page, Body: d.Body, Comment: d.Comment}
}

// noteView is one note in the book pane: dated, with its page, its
```

In `internal/apps/books/view.go`, replace:

```go
	return out
}

// pageText is a page for people: "p. 112", "" for none.
func pageText(page int) string {
	if page == 0 {
```

with:

```go
	return out
}

// quoteView is one quote card: the text as typed, its line breaks kept
// (decided 2026-10-09: plain text, drawn with white-space: pre-line), its
// page, its comment drawn as Markdown, and its Edit form.
type quoteView struct {
	ID          int64
	Text        string
	Page        string // "p. 112"; "" for none
	CommentHTML template.HTML
	Form        entryForm
}

func viewQuotes(qs []Quote, d entryDraft) []quoteView {
	var out []quoteView
	for _, q := range qs {
		out = append(out, quoteView{ID: q.ID, Text: q.Text, Page: pageText(q.Page), CommentHTML: RenderReview(q.Comment),
			Form: formFor("quote-"+strconv.FormatInt(q.ID, 10), d,
				entryForm{Page: pageValue(q.Page), Body: q.Text, Comment: q.Comment})})
	}
	return out
}

// pageText is a page for people: "p. 112", "" for none.
func pageText(page int) string {
	if page == 0 {
```

- [ ] **Step 4: Load the quotes and pick the swap**

In `internal/apps/books/handlers.go`, replace:

```go
// it targeted, as Reader's renderPanes does (#453): #books-list → list-swap
// (the list and its out-of-band companions, the book pane untouched),
// #books-book → book-swap, #books-progress → progress-swap (the box, and
// the list out of band), #books-notes → notes-swap (the same for the
// notes), anything else (#books-panes) → the whole panes.
// Fragments are always 200: htmx's default responseHandling only swaps
// 2xx/3xx.
func (a *App) renderPanes(w http.ResponseWriter, r *http.Request, userID int64, c listCtx, opts paneOpts) {
```

with:

```go
// it targeted, as Reader's renderPanes does (#453): #books-list → list-swap
// (the list and its out-of-band companions, the book pane untouched),
// #books-book → book-swap, #books-progress → progress-swap (the box, and
// the list out of band), #books-notes → notes-swap and #books-quotes →
// quotes-swap (the same for the notes and the quotes), anything else
// (#books-panes) → the whole panes.
// Fragments are always 200: htmx's default responseHandling only swaps
// 2xx/3xx.
func (a *App) renderPanes(w http.ResponseWriter, r *http.Request, userID int64, c listCtx, opts paneOpts) {
```

In `internal/apps/books/handlers.go`, replace:

```go
			a.fail(w, r, err)
			return
		}
		bv = viewBook(b, c, a.store.Today())
		bv.History = viewHistory(rs)
		bv.Notes, bv.NewNote = viewNotes(ns, opts.Draft), formFor("note-new", opts.Draft, entryForm{})
		title = b.Title
		if opts.ProgressError != "" {
			bv.Progress.Error, bv.Progress.Value = opts.ProgressError, opts.ProgressInput
```

with:

```go
			a.fail(w, r, err)
			return
		}
		qs, err := a.store.Quotes(ctx, userID, opts.BookID)
		if err != nil {
			a.fail(w, r, err)
			return
		}
		bv = viewBook(b, c, a.store.Today())
		bv.History = viewHistory(rs)
		bv.Notes, bv.NewNote = viewNotes(ns, opts.Draft), formFor("note-new", opts.Draft, entryForm{})
		bv.Quotes, bv.NewQuote = viewQuotes(qs, opts.Draft), formFor("quote-new", opts.Draft, entryForm{})
		title = b.Title
		if opts.ProgressError != "" {
			bv.Progress.Error, bv.Progress.Value = opts.ProgressError, opts.ProgressInput
```

In `internal/apps/books/handlers.go`, replace:

```go
		case "books-notes":
			block = "notes-swap"
			v.List.OOB = true
		}
		if err := a.deps.Render.Fragment(w, http.StatusOK, "books/index", block, v); err != nil {
			a.deps.Errors.Internal(w, r, err)
```

with:

```go
		case "books-notes":
			block = "notes-swap"
			v.List.OOB = true
		case "books-quotes":
			block = "quotes-swap"
			v.List.OOB = true
		}
		if err := a.deps.Render.Fragment(w, http.StatusOK, "books/index", block, v); err != nil {
			a.deps.Errors.Internal(w, r, err)
```

- [ ] **Step 5: The quote handlers and routes**

In `internal/apps/books/actions.go`, replace:

```go
	return a.store.DeleteNote(r.Context(), userID, id, nid)
}

func (a *App) setTags(r *http.Request, userID, id int64) error {
	return a.store.SetTags(r.Context(), userID, id, ParseTags(r.PostFormValue("tags")))
}
```

with:

```go
	return a.store.DeleteNote(r.Context(), userID, id, nid)
}

// quoteDraft is a posted quote form, named form.
func quoteDraft(r *http.Request, form string) entryDraft {
	return entryDraft{Form: form, Page: strings.TrimSpace(r.PostFormValue("page")),
		Body: r.PostFormValue("text"), Comment: r.PostFormValue("comment")}
}

func (a *App) addQuote(r *http.Request, userID, id int64) (entryDraft, error) {
	d := quoteDraft(r, "quote-new")
	_, err := a.store.AddQuote(r.Context(), userID, id, QuoteInput{Page: pageField(r), Text: d.Body, Comment: d.Comment})
	return d, err
}

func (a *App) editQuote(r *http.Request, userID, id int64) (entryDraft, error) {
	qid, err := childID(r, "qid")
	if err != nil {
		return entryDraft{}, err
	}
	d := quoteDraft(r, "quote-"+strconv.FormatInt(qid, 10))
	return d, a.store.UpdateQuote(r.Context(), userID, id, qid, QuoteInput{Page: pageField(r), Text: d.Body, Comment: d.Comment})
}

func (a *App) deleteQuote(r *http.Request, userID, id int64) error {
	qid, err := childID(r, "qid")
	if err != nil {
		return err
	}
	return a.store.DeleteQuote(r.Context(), userID, id, qid)
}

func (a *App) setTags(r *http.Request, userID, id int64) error {
	return a.store.SetTags(r.Context(), userID, id, ParseTags(r.PostFormValue("tags")))
}
```

In `internal/apps/books/books.go`, replace:

```go
	r.HandleFunc("POST /notes/{id}", a.saveEntry(a.addNote))
	r.HandleFunc("POST /notes/{id}/{nid}", a.saveEntry(a.editNote))
	r.HandleFunc("POST /notes/{id}/{nid}/delete", a.act(a.deleteNote, false))
	r.HandleFunc("POST /tags/{id}", a.act(a.setTags, false))
	r.HandleFunc("POST /delete/{id}", a.act(a.remove, true))
	r.HandleFunc("GET /cover/{id}", a.cover)
```

with:

```go
	r.HandleFunc("POST /notes/{id}", a.saveEntry(a.addNote))
	r.HandleFunc("POST /notes/{id}/{nid}", a.saveEntry(a.editNote))
	r.HandleFunc("POST /notes/{id}/{nid}/delete", a.act(a.deleteNote, false))
	r.HandleFunc("POST /quotes/{id}", a.saveEntry(a.addQuote))
	r.HandleFunc("POST /quotes/{id}/{qid}", a.saveEntry(a.editQuote))
	r.HandleFunc("POST /quotes/{id}/{qid}/delete", a.act(a.deleteQuote, false))
	r.HandleFunc("POST /tags/{id}", a.act(a.setTags, false))
	r.HandleFunc("POST /delete/{id}", a.act(a.remove, true))
	r.HandleFunc("GET /cover/{id}", a.cover)
```

- [ ] **Step 6: The Quotes section**

In `internal/apps/books/templates/panes.partial.html`, replace:

```html
     move. */}}
{{define "notes-swap"}}{{template "notes" .Book}}{{template "list" .List}}{{end}}

{{/* book-swap answers opening a book (target #books-book). The checkbox
     out of band is what makes a phone drill into the book. */}}
{{define "book-swap"}}{{template "shell-oob" .}}{{template "book" .Book}}{{template "pane-toggle" (dict "ID" "books-book-open" "Label" "Books" "Checked" true "OOB" true)}}{{end}}
```

with:

```html
     move. */}}
{{define "notes-swap"}}{{template "notes" .Book}}{{template "list" .List}}{{end}}

{{/* quotes-swap is notes-swap for the quotes (target #books-quotes). */}}
{{define "quotes-swap"}}{{template "quotes" .Book}}{{template "list" .List}}{{end}}

{{/* book-swap answers opening a book (target #books-book). The checkbox
     out of band is what makes a phone drill into the book. */}}
{{define "book-swap"}}{{template "shell-oob" .}}{{template "book" .Book}}{{template "pane-toggle" (dict "ID" "books-book-open" "Label" "Books" "Checked" true "OOB" true)}}{{end}}
```

In `internal/apps/books/templates/panes.partial.html`, replace:

```html
	{{with .Description}}<div class="books-description">{{.}}</div>{{end}}
	{{template "tags-form" .}}
	{{template "notes" .}}
	{{template "history" .}}
{{else}}
	<p class="empty">Pick a book, or add one.</p>
```

with:

```html
	{{with .Description}}<div class="books-description">{{.}}</div>{{end}}
	{{template "tags-form" .}}
	{{template "notes" .}}
	{{template "quotes" .}}
	{{template "history" .}}
{{else}}
	<p class="empty">Pick a book, or add one.</p>
```

In `internal/apps/books/templates/panes.partial.html`, replace:

```html
</form>
{{end}}

{{/* history is every reading of the book, newest first (spec "Book
     pane"), each with a no-JS <details> to change its dates and format,
     and Delete (decided 2026-10-09: a reading's status isn't editable;
```

with:

```html
</form>
{{end}}

{{/* quotes is the book's quotes as cards, newest first (spec "Book
     pane"): a "+ Add quote" disclosure, then each quote — its text with
     its line breaks, its page and its comment — with an Edit disclosure
     and Delete, as the notes have. htmx swaps just this section
     (quotes-swap). Takes a bookView. */}}
{{define "quotes"}}
<section class="books-quotes" id="books-quotes" aria-labelledby="books-quotes-head">
	<h2 class="books-section-head" id="books-quotes-head">Quotes</h2>
	<details class="books-entry-add" id="books-quote-new"{{if .NewQuote.Open}} open{{end}}>
		<summary class="button">+ Add quote</summary>
		{{template "quote-form" (dict "Book" . "Key" "new" "Action" (printf "/books/quotes/%d" .ID) "Form" .NewQuote "Submit" "Add quote")}}
	</details>
	{{with .Quotes}}
	<ul class="books-entries">
		{{range .}}
		<li class="books-quote">
			<blockquote class="books-quote-text">{{.Text}}</blockquote>
			{{with .Page}}<p class="books-entry-meta">{{.}}</p>{{end}}
			{{with .CommentHTML}}<div class="books-entry-text books-quote-comment">{{.}}</div>{{end}}
			<div class="books-entry-actions">
				<details class="books-entry-edit"{{if .Form.Open}} open{{end}}>
					<summary class="button quiet">Edit</summary>
					{{template "quote-form" (dict "Book" $ "Key" .ID "Action" (printf "/books/quotes/%d/%d" $.ID .ID) "Form" .Form "Submit" "Save quote")}}
				</details>
				<form method="post" action="/books/quotes/{{$.ID}}/{{.ID}}/delete">
					{{template "post-ctx" $}}
					<button type="submit" class="quiet"
					        hx-post="/books/quotes/{{$.ID}}/{{.ID}}/delete" hx-target="#books-quotes" hx-swap="outerHTML"
					        hx-confirm="Delete this quote?">Delete</button>
				</form>
			</div>
		</li>
		{{end}}
	</ul>
	{{end}}
</section>
{{end}}

{{/* quote-form adds or edits a quote: its text, an optional comment and
     an optional page. Pass a dict as for note-form. */}}
{{define "quote-form"}}
<form class="books-entry-form" novalidate method="post" action="{{.Action}}"
      hx-post="{{.Action}}" hx-target="#books-quotes" hx-swap="outerHTML">
	{{template "post-ctx" .Book}}
	<label for="books-quote-{{.Key}}-text">Quote</label>
	<textarea id="books-quote-{{.Key}}-text" name="text" rows="4"{{if .Form.Error}} aria-describedby="books-quote-{{.Key}}-error"{{end}}>{{.Form.Body}}</textarea>
	<label for="books-quote-{{.Key}}-comment">Comment (optional)</label>
	<textarea id="books-quote-{{.Key}}-comment" name="comment" rows="2"{{if .Form.Error}} aria-describedby="books-quote-{{.Key}}-error"{{end}}>{{.Form.Comment}}</textarea>
	<p class="books-hint">The quote is kept as typed, line breaks and all. Markdown works in the comment.</p>
	<label for="books-quote-{{.Key}}-page">Page (optional)</label>
	<input id="books-quote-{{.Key}}-page" name="page" type="number" inputmode="numeric" min="1"{{with .Book.Pages}} max="{{.}}"{{end}} value="{{.Form.Page}}"{{if .Form.Error}} aria-describedby="books-quote-{{.Key}}-error"{{end}}>
	{{with .Form.Error}}<p class="books-field-error" id="books-quote-{{$.Key}}-error" role="alert">{{.}}</p>{{end}}
	<button type="submit" class="primary">{{.Submit}}</button>
</form>
{{end}}

{{/* history is every reading of the book, newest first (spec "Book
     pane"), each with a no-JS <details> to change its dates and format,
     and Delete (decided 2026-10-09: a reading's status isn't editable;
```

Deleting a book now takes its notes and quotes too; say so in its confirm:

In `internal/apps/books/templates/panes.partial.html`, replace:

```html
			{{template "post-ctx" .}}
			<button type="submit" class="outline-menu-delete"
			        hx-post="/books/delete/{{.ID}}" hx-target="#books-panes" hx-swap="outerHTML"
			        hx-confirm="Delete “{{.Title}}” for good? Its readings and tags go too.">Delete</button>
		</form>
	</div>
</details>
```

with:

```html
			{{template "post-ctx" .}}
			<button type="submit" class="outline-menu-delete"
			        hx-post="/books/delete/{{.ID}}" hx-target="#books-panes" hx-swap="outerHTML"
			        hx-confirm="Delete “{{.Title}}” for good? Its readings, notes and quotes go too.">Delete</button>
		</form>
	</div>
</details>
```

- [ ] **Step 7: `q` opens the add-quote box**

In `internal/apps/books/static/books.js`, replace:

```js
	// --- Keyboard shortcuts --------------------------------------------------

	// openEntry opens an add disclosure in the book pane ("+ Add note") and
	// puts the cursor in its box; false when the pane has none.
	function openEntry(detailsId, inputId) {
		var details = document.getElementById(detailsId);
		var input = document.getElementById(inputId);
```

with:

```js
	// --- Keyboard shortcuts --------------------------------------------------

	// openEntry opens an add disclosure in the book pane ("+ Add note",
	// "+ Add quote") and puts the cursor in its box; false when the pane
	// has none.
	function openEntry(detailsId, inputId) {
		var details = document.getElementById(detailsId);
		var input = document.getElementById(inputId);
```

In `internal/apps/books/static/books.js`, replace:

```js
		case "n":
			if (!openEntry("books-note-new", "books-note-new-body")) return;
			break;
		case "Escape":
			document.querySelectorAll("#books-book details[open]").forEach(function (d) {
				d.open = false;
```

with:

```js
		case "n":
			if (!openEntry("books-note-new", "books-note-new-body")) return;
			break;
		case "q":
			if (!openEntry("books-quote-new", "books-quote-new-text")) return;
			break;
		case "Escape":
			document.querySelectorAll("#books-book details[open]").forEach(function (d) {
				d.open = false;
```

- [ ] **Step 8: Quote cards**

In `internal/ui/static/app.css`, replace:

```css
.books-reading-edit-form { display: flex; flex-wrap: wrap; align-items: flex-end; gap: var(--s-2); margin-top: var(--s-2); }
.books-reading-edit-form label { margin: 0; flex-basis: 100%; }
/* Notes and quotes (B3): "+ Add" and Edit disclosures, as the review's */
.books-notes { max-width: 38rem; margin-top: var(--s-4); }
.books-entry-add > summary,
.books-entry-edit > summary { display: inline-flex; list-style: none; cursor: pointer; }
.books-entry-add > summary::-webkit-details-marker,
```

with:

```css
.books-reading-edit-form { display: flex; flex-wrap: wrap; align-items: flex-end; gap: var(--s-2); margin-top: var(--s-2); }
.books-reading-edit-form label { margin: 0; flex-basis: 100%; }
/* Notes and quotes (B3): "+ Add" and Edit disclosures, as the review's */
.books-notes,
.books-quotes { max-width: 38rem; margin-top: var(--s-4); }
.books-entry-add > summary,
.books-entry-edit > summary { display: inline-flex; list-style: none; cursor: pointer; }
.books-entry-add > summary::-webkit-details-marker,
```

In `internal/ui/static/app.css`, replace:

```css
.books-entry-text p { margin: var(--s-1) 0; }
.books-entry-actions { display: flex; flex-wrap: wrap; align-items: flex-start; gap: var(--s-2); }
.books-entry-edit[open] { flex-basis: 100%; }
.books-dialog { width: min(28rem, calc(100vw - 2rem)); padding: 1rem; border: var(--border); border-radius: var(--radius); color: var(--c-text); background: var(--c-bg); }
.books-dialog::backdrop { background: rgba(0, 0, 0, 0.35); }
```

with:

```css
.books-entry-text p { margin: var(--s-1) 0; }
.books-entry-actions { display: flex; flex-wrap: wrap; align-items: flex-start; gap: var(--s-2); }
.books-entry-edit[open] { flex-basis: 100%; }
/* Quotes are cards; the text keeps the line breaks it was typed with. */
.books-quote { margin-top: var(--s-2); padding: var(--s-3); border: var(--border); border-radius: var(--radius); background: var(--c-bg-subtle); }
.books-quote-text { margin: 0 0 var(--s-1); padding-left: var(--s-3); border-left: 3px solid var(--c-accent); font-style: italic; white-space: pre-line; overflow-wrap: anywhere; }
.books-quote-comment { margin-bottom: var(--s-1); }
.books-dialog { width: min(28rem, calc(100vw - 2rem)); padding: 1rem; border: var(--border); border-radius: var(--radius); color: var(--c-text); background: var(--c-bg); }
.books-dialog::backdrop { background: rgba(0, 0, 0, 0.35); }
```

- [ ] **Step 9: Run the tests, vet and staticcheck**

```bash
go test ./internal/apps/books/... -count=1
go vet ./...
go run honnef.co/go/tools/cmd/staticcheck@v0.8.0 ./...
```
Expected: all pass.

- [ ] **Step 10: Commit**

```bash
git add internal/apps/books internal/ui/static/app.css
git commit -m "feat(books): quotes in the book pane (#489)"
```

---

### Task 6: Demo data for the screenshots

The screenshot seed (`docs/screenshots/seed`) builds the demo account the browser check and B5's screenshots use. Give two books notes and add a public-domain book with quotes — no quotes from books still in copyright.

**Files:**
- Modify: `docs/screenshots/seed/books.go`, `docs/screenshots/seed/seed_test.go`

**Interfaces:**
- Consumes: `AddNote`, `AddQuote`, `NoteInput`, `QuoteInput`, `List` with `Q`, `ListItem.Match`, `MatchNote`, `MatchQuote`.
- Produces: nothing for later tasks.

- [ ] **Step 1: Write the failing test**

In `docs/screenshots/seed/seed_test.go`, replace:

```go
			t.Errorf("read %q has no rating", it.Title)
		}
	}

	// Flash: decks with cards, a review history and a streak.
	fs := flash.NewStore(handle)
```

with:

```go
			t.Errorf("read %q has no rating", it.Title)
		}
	}
	// ... and notes and quotes the filter finds.
	for q, want := range map[string]books.MatchIn{"protomolecule": books.MatchNote, "agony": books.MatchQuote} {
		found, err := bst.List(ctx, demo.ID, books.ListQuery{Shelf: books.ShelfAll, Q: q})
		if err != nil || len(found) != 1 || found[0].Match != want {
			t.Errorf("books search %q = %+v, %v; want one book matched in a %s", q, found, err, want)
		}
	}

	// Flash: decks with cards, a review history and a streak.
	fs := flash.NewStore(handle)
```

- [ ] **Step 2: Run it to see it fail**

Run: `go test ./docs/screenshots/seed/ -count=1`
Expected: FAIL — `books search "protomolecule" = [], <nil>; want one book matched in a note` (and the same for "agony").

- [ ] **Step 3: Seed the notes and quotes**

In `docs/screenshots/seed/books.go`, replace:

```go
// seedBooks gives the demo account a small library across every shelf:
// two books on the go (one on paper, one an audiobook, each with a few
// days of progress), three waiting, two finished and rated (one with a
// review) and one put down part-way, with a few tags and two books of a
// series, so each shelf and the book pane have something to show. Dates
// are offsets from now, so nothing is ever in the future.
func seedBooks(ctx context.Context, st *books.Store, userID int64, now time.Time) error {
	at := func(daysAgo int) time.Time { return now.AddDate(0, 0, -daysAgo) }
	day := func(daysAgo int) string { return at(daysAgo).Local().Format("2006-01-02") }
```

with:

```go
// seedBooks gives the demo account a small library across every shelf:
// two books on the go (one on paper, one an audiobook, each with a few
// days of progress), three waiting, three finished and rated (one with a
// review) and one put down part-way, with a few tags, two books of a
// series, notes on two books and quotes from one, so each shelf, the book
// pane and the search have something to show. Quotes come only from a
// book long out of copyright. Dates are offsets from now, so nothing is
// ever in the future.
func seedBooks(ctx context.Context, st *books.Store, userID int64, now time.Time) error {
	at := func(daysAgo int) time.Time { return now.AddDate(0, 0, -daysAgo) }
	day := func(daysAgo int) string { return at(daysAgo).Local().Format("2006-01-02") }
```

In `docs/screenshots/seed/books.go`, replace:

```go
		stopped  int // where a DNF stopped; 0 = not said
		rating   int
		review   string
	}
	library := []seed{
		{in: books.BookInput{Title: "Leviathan Wakes", Authors: "James S. A. Corey", Year: 2011, Pages: 592,
			SeriesName: "The Expanse", SeriesNumber: "1",
			Description: "A detective and a ship's officer find the same missing woman at the edge of the solar system."},
			tags: []string{"sf", "space"}, started: 9, format: "paper", progress: []int{48, 120, 205, 260, 344}, finished: -1},
		{in: books.BookInput{Title: "Piranesi", Authors: "Susanna Clarke", Year: 2020, Pages: 272},
			tags: []string{"fantasy"}, started: 3, format: "audio", progress: []int{18, 41}, finished: -1},
		{in: books.BookInput{Title: "The Dispossessed", Subtitle: "An Ambiguous Utopia", Authors: "Ursula K. Le Guin",
```

with:

```go
		stopped  int // where a DNF stopped; 0 = not said
		rating   int
		review   string
		notes    []books.NoteInput  // one a day from the day after it was started
		quotes   []books.QuoteInput // likewise
	}
	library := []seed{
		{in: books.BookInput{Title: "Leviathan Wakes", Authors: "James S. A. Corey", Year: 2011, Pages: 592,
			SeriesName: "The Expanse", SeriesNumber: "1",
			Description: "A detective and a ship's officer find the same missing woman at the edge of the solar system."},
			tags: []string{"sf", "space"}, started: 9, format: "paper", progress: []int{48, 120, 205, 260, 344}, finished: -1,
			notes: []books.NoteInput{
				{Page: 120, Body: "Miller and Holden finally meet. The two voices work better together than apart."},
				{Page: 260, Body: "The *protomolecule*. Didn't see that coming — the detective story was cover for something much bigger."},
			}},
		{in: books.BookInput{Title: "Piranesi", Authors: "Susanna Clarke", Year: 2020, Pages: 272},
			tags: []string{"fantasy"}, started: 3, format: "audio", progress: []int{18, 41}, finished: -1},
		{in: books.BookInput{Title: "The Dispossessed", Subtitle: "An Ambiguous Utopia", Authors: "Ursula K. Le Guin",
```

In `docs/screenshots/seed/books.go`, replace:

```go
			started: 35, format: "paper", finished: 20, rating: 4},
		{in: books.BookInput{Title: "Infinite Jest", Authors: "David Foster Wallace", Year: 1996, Pages: 1079},
			started: 120, format: "paper", finished: 90, dnf: true, stopped: 312},
	}
	for i, b := range library {
		// Added in this order, a day apart, before anything was started.
```

with:

```go
			started: 35, format: "paper", finished: 20, rating: 4},
		{in: books.BookInput{Title: "Infinite Jest", Authors: "David Foster Wallace", Year: 1996, Pages: 1079},
			started: 120, format: "paper", finished: 90, dnf: true, stopped: 312},
		{in: books.BookInput{Title: "Persuasion", Authors: "Jane Austen", Year: 1817, Pages: 249},
			tags: []string{"classics"}, started: 48, format: "ebook", finished: 40, rating: 5,
			notes: []books.NoteInput{{Body: "Anne is the quietest of Austen's heroines, and the best."}},
			quotes: []books.QuoteInput{
				{Page: 229, Text: "All the privilege I claim for my own sex (it is not a very enviable one; you need not covet it), is that of loving longest, when existence or when hope is gone."},
				{Page: 231, Text: "You pierce my soul. I am half agony, half hope.", Comment: "**The letter.** Worth the whole book."},
			}},
	}
	for i, b := range library {
		// Added in this order, a day apart, before anything was started.
```

In `docs/screenshots/seed/books.go`, replace:

```go
				return err
			}
		}
		if b.finished < 0 {
			continue
		}
```

with:

```go
				return err
			}
		}
		for j, n := range b.notes {
			st.SetClock(func() time.Time { return at(b.started - j - 1) })
			if _, err := st.AddNote(ctx, userID, id, n); err != nil {
				return err
			}
		}
		for j, q := range b.quotes {
			st.SetClock(func() time.Time { return at(b.started - j - 1) })
			if _, err := st.AddQuote(ctx, userID, id, q); err != nil {
				return err
			}
		}
		if b.finished < 0 {
			continue
		}
```

- [ ] **Step 4: Run the test, vet and staticcheck**

```bash
go test ./docs/... -count=1
go vet ./...
go run honnef.co/go/tools/cmd/staticcheck@v0.8.0 ./...
```
Expected: all pass.

- [ ] **Step 5: Commit**

```bash
git add docs/screenshots/seed
git commit -m "docs(books): seed notes and quotes for the demo (#489)"
```

---

### Task 7: User guide and app list

The spec already records B3's decisions and the `n`/`q` keys: this plan's own PR added "Notes, quotes and search" to it. Only the guide and the app list change here.

**Files:**
- Modify: `docs/user/books.md`, `AGENTS.md`

- [ ] **Step 1: Update the guide**

In `docs/user/books.md`, replace:

````markdown
review** changes it later. A book has one rating and one review, however
many times you read it.

## Reading history

At the bottom of a book is every time you've read it, newest first: when
````

with:

````markdown
review** changes it later. A book has one rating and one review, however
many times you read it.

## Notes and quotes

Under the review are your notes on the book, newest first, each with the
day you wrote it. **+ Add note** opens a box for one; give it a page if you
like, and it shows as "p. 112". Notes take the same little Markdown as
reviews.

**Quotes** are passages you want to keep. **+ Add quote** opens a box for
the words, an optional comment and an optional page. A quote keeps its
line breaks exactly as you typed them, so poetry stays poetry; the comment
takes Markdown.

**Edit** on a note or quote changes it in place; **Delete** removes it,
after asking. A page has to be one the book has — from 1 to its page
count.

## Reading history

At the bottom of a book is every time you've read it, newest first: when
````

In `docs/user/books.md`, replace:

````markdown
## Finding a book

Type in the box above the list to narrow it to books whose title,
subtitle, author or series contains what you typed. The filter stays as you
switch shelves; clear the box to see everything again.

## Editing and deleting

Open a book and use the **⋯** menu: **Edit details** changes the title,
author and the rest; **Delete** removes the book for good, with its
readings and tags.

## Keyboard shortcuts
````

with:

````markdown
## Finding a book

Type in the box above the list to search your books: their titles,
subtitles, authors and series, and everything you've written about them —
reviews, notes, quotes and quote comments. Every word you type has to
match, and the list narrows while you're still typing a word. When a book
matched inside a review, note or quote, its row gets a third line showing
where, with the matching words highlighted — "Quote: The spice must
flow."

The search stays within the shelf, tag or series you're looking at, and
stays as you switch shelves; clear the box to see everything again.

## Editing and deleting

Open a book and use the **⋯** menu: **Edit details** changes the title,
author and the rest; **Delete** removes the book for good, with its
readings, notes and quotes.

## Keyboard shortcuts
````

In `docs/user/books.md`, replace:

````markdown
- **/** — jump to the filter box
- **a** — add a book
- **p** — jump to the progress box of the book you're reading
- **Esc** — close an open box in the book (Finish, Did not finish, a
  menu, the review), or leave the filter or the progress box

## On a phone
````

with:

````markdown
- **/** — jump to the filter box
- **a** — add a book
- **p** — jump to the progress box of the book you're reading
- **n** — add a note to the open book
- **q** — add a quote to the open book
- **Esc** — close an open box in the book (Finish, Did not finish, a
  menu, the review, a note or quote), or leave the filter or the
  progress box

## On a phone
````

- [ ] **Step 2: Update the app list**

In `AGENTS.md`, replace:

````markdown
extraction, and reading-stats), **ON Later** (a read-it-later app: save
pages, read them in a calm view, highlight, comment and tag them, export as
Markdown), **ON Books** (a private reading log: shelves, readings, tags,
Open Library search, progress, ratings and reviews, with notes and stats
to come),
**ON Flash** (flash cards with FSRS review, import from
AI-written Markdown/JSON, media, household sharing and stats), and **ON
Focus** (saved, reusable focus timers — single blocks or Pomodoro-style
````

with:

````markdown
extraction, and reading-stats), **ON Later** (a read-it-later app: save
pages, read them in a calm view, highlight, comment and tag them, export as
Markdown), **ON Books** (a private reading log: shelves, readings, tags,
Open Library search, progress, ratings and reviews, notes and quotes, and
full-text search, with stats to come),
**ON Flash** (flash cards with FSRS review, import from
AI-written Markdown/JSON, media, household sharing and stats), and **ON
Focus** (saved, reusable focus timers — single blocks or Pomodoro-style
````

- [ ] **Step 3: Run the full check and commit**

Run the full check from Global Constraints (the help tests load the guide).

```bash
git add docs AGENTS.md
git commit -m "docs(books): notes, quotes and search in the guide (#489)"
```

---

### Task 8: Check it in a browser and open the PR

- [ ] **Step 1: Seed a demo directory and start the server**

```bash
SEED=$(mktemp -d)/books-b3
go run ./docs/screenshots/seed --data-dir $SEED
go build -o $SEED/onsuite ./cmd/onsuite
echo $SEED
```
Add a `.claude/launch.json` configuration named `onsuite-books` (in the main checkout, `/Users/iliaf/src/WEB/on-suite`) with `"runtimeExecutable": "<SEED>/onsuite"`, `"runtimeArgs": ["serve", "--addr", ":8096", "--data-dir", "<SEED>"]` and `"port": 8096`, start it with the preview tools and sign in as `demo` (password in `docs/screenshots/README.md`).

- [ ] **Step 2: Walk through it**

1. Open Leviathan Wakes: Notes (two, newest first, "p. 260" and "p. 120", the *protomolecule* in italics), then Quotes (none yet, just "+ Add quote"), then Reading history.
2. Press `n`: the add-note box opens with the cursor in it. Type a note and page 9999, Add note: "Enter a page from 1 to 592." inside the box, what was typed kept, the pane not scrolled to the top. Fix the page, Add note: it appears at the top, the box closes, no console errors.
3. Edit a note, Save note; Delete a note (the confirm dialog, then gone). Esc closes an open Edit box.
4. All books → Persuasion: two quote cards; the long one keeps its parentheses, the letter quote shows "p. 231" and a bold comment. Press `q`, add a quote with two lines: the line break shows on the card.
5. Filter "protomolecule": Leviathan Wakes with "Note: … The protomolecule …" highlighted; "agon" (half a word) finds Persuasion with "Quote: …"; "jane" finds Persuasion with no third line (an author match); switch shelves with the filter on — it narrows within each.
6. Turn JavaScript off (or post a form by hand) for a note add, a refused quote, an edit and a delete: each comes back to the book (the refusal as a page with the form open).
7. Phone width: the sections, cards and snippet lines don't overflow.

Fix anything found (with a test where one can be written), re-run the full check, commit.

- [ ] **Step 3: Remove the launch entry and push**

Remove the `onsuite-books` entry from `.claude/launch.json` (do not commit it), stop the server, then:

```bash
git push -u origin feat/books-b3-notes-quotes
env -u GH_TOKEN gh pr create --title "feat(books): ON Books B3 — notes and quotes (#489)" --body "$(cat <<'EOF'
B3 of ON Books. Dated notes (Markdown, optional page) and quotes (plain text with line breaks, optional page and Markdown comment) on every book, each added from a "+ Add" disclosure and edited or deleted in place; without JavaScript every form is a plain post. The list's filter box is now full-text search (FTS5, kept in step by triggers) over titles, authors, series, reviews, notes, quotes and comments, within the current shelf/tag/series and in the shelf's own order; a row that matched inside a review, note or quote shows where, highlighted. `n` and `q` open the add boxes.

Decisions from 2026-10-09 are in the spec (added by the plan PR). Note and quote forms swap only their own section with htmx, so they don't scroll the book pane to the top (#578 still applies to the older controls).

Spec: docs/superpowers/specs/2026-10-09-on-books-design.md
Plan: docs/superpowers/plans/2026-10-09-on-books-b3-notes-quotes.md
Closes #489.
EOF
)"
```
Never merge it.
