package books_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/iliafrenkel/on-suite/internal/apps/books"
	"github.com/iliafrenkel/on-suite/internal/htmlassert"
)

// wantMarkdown is the download of the library TestMarkdownDownload
// builds: books by title, each with its details, description, readings
// (newest first), rating and review, notes and quotes.
const wantMarkdown = `# My books

Exported from ON Books on 9 Oct 2026: 2 books.

Reading goals — 2026: 24 books.

## Dune

*Book One*

- Author: Frank Herbert
- Series: Dune #1
- First published: 1965
- Pages: 600
- ISBN: 9780441013593
- Shelf: Read
- Tags: classics, sf
- Rating: ★★★★★ (5 of 5)
- Added: 1 Oct 2026

### Description

A desert planet.
Spice.

### Readings

- Read · Paper · 1 Oct 2026 – 9 Oct 2026

### Review

The *spice*.

Second paragraph.

### Notes

**9 Oct 2026 · p. 112**

Paul and the box.

### Quotes

> Fear is the mind-killer.
> Fear is the little-death.

— p. 8

The **litany**.

## emma

- Shelf: Want to read
- Added: 9 Oct 2026
`

func TestMarkdownDownload(t *testing.T) {
	s := newServer(t)
	ctx := context.Background()
	uid := s.Alice.User.ID
	s.Clock.Set(noon("2026-10-01"))
	nb := titled("Dune", "Frank Herbert", books.ShelfReading)
	nb.Subtitle, nb.Year, nb.Pages, nb.ISBN = "Book One", 1965, 600, "9780441013593"
	nb.SeriesName, nb.SeriesNumber, nb.Tags = "Dune", "1", []string{"sf", "classics"}
	nb.Description = "A desert planet.\nSpice."
	id := add(t, s, uid, nb)
	if err := s.Store.SetFormat(ctx, uid, id, "paper"); err != nil {
		t.Fatal(err)
	}
	s.Clock.Set(noon("2026-10-09"))
	add(t, s, uid, titled("emma", "", books.ShelfWant)) // sorted ignoring case
	add(t, s, s.Bob.User.ID, titled("Bob's book", "", books.ShelfWant))
	if err := s.Store.FinishReading(ctx, uid, id, "2026-10-09", 5); err != nil {
		t.Fatal(err)
	}
	if err := s.Store.SetReview(ctx, uid, id, "The *spice*.\n\nSecond paragraph."); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Store.AddNote(ctx, uid, id, books.NoteInput{Page: 112, Body: "Paul and the box."}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Store.AddQuote(ctx, uid, id, books.QuoteInput{Page: 8,
		Text: "Fear is the mind-killer.\nFear is the little-death.", Comment: "The **litany**."}); err != nil {
		t.Fatal(err)
	}
	if err := s.Store.SetGoal(ctx, uid, 2026, 24); err != nil {
		t.Fatal(err)
	}

	rec := s.Do(t, s.Alice, httptest.NewRequest("GET", "/books/export", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /books/export = %d", rec.Code)
	}
	if got := rec.Header().Get("Content-Disposition"); got != `attachment; filename="books-export.md"` {
		t.Errorf("Content-Disposition = %q, want the generic books-export.md", got)
	}
	if got := rec.Header().Get("Content-Type"); got != "text/markdown; charset=utf-8" {
		t.Errorf("Content-Type = %q", got)
	}
	if got := rec.Body.String(); got != wantMarkdown {
		t.Errorf("download =\n%s\nwant\n%s", got, wantMarkdown)
	}
}

func TestMarkdownDownloadIsLinked(t *testing.T) {
	s := newServer(t)
	link := s.Get(t, s.Alice, "/books/").MustHave(`.books-side a[href="/books/export"]`)
	if _, ok := htmlassert.Attr(link, "download"); !ok {
		t.Error("the Export link has no download attribute")
	}
}
