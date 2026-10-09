package books_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/iliafrenkel/on-suite/internal/apps/books"
	"github.com/iliafrenkel/on-suite/internal/htmlassert"
)

func TestLongDescriptionsFold(t *testing.T) {
	s := newServer(t)
	uid := s.Alice.User.ID
	short := titled("Short", "", books.ShelfWant)
	short.Description = "A detective and a ship's officer."
	long := titled("Long", "", books.ShelfWant)
	long.Description = strings.Repeat("A long description. ", 20) // 400 characters
	lines := titled("Lines", "", books.ShelfWant)
	lines.Description = "One\nTwo\nThree\nFour\nFive"
	for _, nb := range []books.NewBook{long, lines} {
		doc := s.Get(t, s.Alice, fmt.Sprintf("/books/b/%d?shelf=want", add(t, s, uid, nb)))
		doc.MustHave("input#books-description-more")
		doc.MustHave("p.is-clamped")
		if got := attr(t, doc, "label.books-description-more", "for"); got != "books-description-more" {
			t.Errorf("%s: More label is for %q", nb.Title, got)
		}
	}
	doc := s.Get(t, s.Alice, fmt.Sprintf("/books/b/%d?shelf=want", add(t, s, uid, short)))
	if got := htmlassert.Text(doc.MustHave(".books-description-text")); got != short.Description {
		t.Errorf("short description = %q", got)
	}
	doc.MustNotHave("input#books-description-more")
	doc.MustNotHave(".is-clamped")
}

func TestShelfCountsReadAsWords(t *testing.T) {
	s := newServer(t)
	uid := s.Alice.User.ID
	add(t, s, uid, titled("Emma", "", books.ShelfWant))
	add(t, s, uid, titled("Persuasion", "", books.ShelfWant))
	got := texts(s.Get(t, s.Alice, "/books/?shelf=want"), ".books-shelves .books-shelf")
	want := []string{"Reading", "Want to read, 2 books", "Read", "Did not finish", "All books, 2 books"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("shelf links = %q, want %q (#568)", got, want)
	}
}

func TestRatingStarsWrapTogether(t *testing.T) {
	s := newServer(t)
	id := add(t, s, s.Alice.User.ID, titled("Emma", "", books.ShelfRead))
	doc := s.Get(t, s.Alice, fmt.Sprintf("/books/b/%d?shelf=read", id))
	if n := len(doc.QueryAll(".books-star-row .books-star")); n != 5 {
		t.Errorf("%d stars in .books-star-row, want all 5 (#579)", n)
	}
}
