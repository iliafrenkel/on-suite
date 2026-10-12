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
