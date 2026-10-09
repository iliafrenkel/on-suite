package books_test

import (
	"fmt"
	"slices"
	"testing"

	"github.com/iliafrenkel/on-suite/internal/apps/books"
	"github.com/iliafrenkel/on-suite/internal/htmlassert"
)

// inSeries is a want-to-read book in a series.
func inSeries(title, series, number string) books.NewBook {
	nb := onShelf(title, books.ShelfWant)
	nb.SeriesName, nb.SeriesNumber = series, number
	return nb
}

func TestListASeriesInReadingOrder(t *testing.T) {
	f := newFixture(t)
	for _, nb := range []books.NewBook{
		inSeries("Caliban's War", "The Expanse", "2"),
		inSeries("The Churn", "the expanse", "2.5"),
		inSeries("Abaddon's Gate", "The Expanse", "3"),
		inSeries("Leviathan Wakes", "The Expanse", "1"),
		inSeries("Gods of Risk", "The Expanse", ""),
		inSeries("A Wizard of Earthsea", "Earthsea", "1"),
	} {
		addBook(t, f, f.alice.ID, nb)
	}
	addBook(t, f, f.bob.ID, inSeries("Bob's Expanse", "The Expanse", "4"))

	got := list(t, f, books.ListQuery{Shelf: books.ShelfAll, Series: "The Expanse"})
	want := []string{"Leviathan Wakes", "Caliban's War", "The Churn", "Abaddon's Gate", "Gods of Risk"}
	if !slices.Equal(got, want) {
		t.Errorf("series list = %v, want %v", got, want)
	}
	if got := list(t, f, books.ListQuery{Shelf: books.ShelfReading, Series: "The Expanse"}); len(got) != 0 {
		t.Errorf("series on the Reading shelf = %v, want none: the shelf still applies", got)
	}
}

func TestABookKnowsHowManyAreInItsSeries(t *testing.T) {
	f := newFixture(t)
	id := addBook(t, f, f.alice.ID, inSeries("Leviathan Wakes", "The Expanse", "1"))
	addBook(t, f, f.alice.ID, inSeries("Caliban's War", "THE EXPANSE", "2"))
	addBook(t, f, f.bob.ID, inSeries("Bob's Expanse", "The Expanse", "3"))
	alone := addBook(t, f, f.alice.ID, onShelf("Piranesi", books.ShelfWant))
	if n := getBook(t, f, f.alice.ID, id).SeriesBooks; n != 2 {
		t.Errorf("SeriesBooks = %d, want 2 (Alice's, any case)", n)
	}
	if n := getBook(t, f, f.alice.ID, alone).SeriesBooks; n != 0 {
		t.Errorf("SeriesBooks of a book in no series = %d, want 0", n)
	}
}

func TestTheSeriesLinkFiltersTheList(t *testing.T) {
	s := newServer(t)
	uid := s.Alice.User.ID
	nb := titled("Leviathan Wakes", "James S. A. Corey", books.ShelfReading)
	nb.SeriesName, nb.SeriesNumber = "The Expanse", "1"
	id := add(t, s, uid, nb)
	nb = titled("Caliban's War", "James S. A. Corey", books.ShelfWant)
	nb.SeriesName, nb.SeriesNumber = "The Expanse", "2"
	add(t, s, uid, nb)
	add(t, s, uid, titled("Piranesi", "Susanna Clarke", books.ShelfWant))

	doc := s.Get(t, s.Alice, fmt.Sprintf("/books/b/%d?shelf=reading", id))
	link := doc.MustHave(".books-series a")
	if got := htmlassert.Text(link); got != "The Expanse #1 · 2 books" {
		t.Errorf("series link = %q", got)
	}
	href, _ := htmlassert.Attr(link, "href")
	if want := "/books/?series=The+Expanse&shelf=all"; href != want {
		t.Errorf("series link href = %q, want %q", href, want)
	}
	if v, _ := htmlassert.Attr(link, "hx-target"); v != "#books-list" {
		t.Errorf("series link hx-target = %q, want the list", v)
	}

	doc = htmlassert.Parse(t, hx(t, s, href, "books-list"))
	if got := rowTitles(doc); !slices.Equal(got, []string{"Leviathan Wakes", "Caliban's War"}) {
		t.Errorf("series rows = %v", got)
	}
	if got := htmlassert.Text(doc.MustHave(".books-list-head")); got != "Series “The Expanse”" {
		t.Errorf("heading = %q", got)
	}
	doc.MustNotHave(`.books-side a[aria-current="page"]`)
	if v, _ := htmlassert.Attr(doc.MustHave("#books-list"), "data-series"); v != "The Expanse" {
		t.Errorf("data-series = %q", v)
	}
	if v, _ := htmlassert.Attr(doc.MustHave(`#books-filter-ctx input[name="series"]`), "value"); v != "The Expanse" {
		t.Errorf("the filter's series field = %q", v)
	}

	// A book opened from the series list posts back into it.
	doc = s.Get(t, s.Alice, fmt.Sprintf("/books/b/%d?shelf=all&series=The+Expanse", id))
	if v, _ := htmlassert.Attr(doc.MustHave(`.books-tags-form input[name="series"]`), "value"); v != "The Expanse" {
		t.Errorf("tags form series field = %q", v)
	}
}

func TestABookInNoSeriesHasNoSeriesLink(t *testing.T) {
	s := newServer(t)
	id := add(t, s, s.Alice.User.ID, titled("Piranesi", "", books.ShelfWant))
	s.Get(t, s.Alice, fmt.Sprintf("/books/b/%d", id)).MustNotHave(".books-series")
}
