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
