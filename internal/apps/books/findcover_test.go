package books_test

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/url"
	"testing"

	"github.com/iliafrenkel/on-suite/internal/apps/books"
	"github.com/iliafrenkel/on-suite/internal/htmlassert"
)

func findCover(t *testing.T, s *server, id int64) *htmlassert.Doc {
	t.Helper()
	rec := s.PostHX(t, s.Alice, fmt.Sprintf("/books/find-cover/%d", id), url.Values{"shelf": {"want"}})
	if rec.Code != http.StatusOK {
		t.Fatalf("Find cover = %d; body: %s", rec.Code, rec.Body.String())
	}
	return htmlassert.Parse(t, rec.Body.String())
}

func TestFindCoverIsInTheMenuOfABookWithout(t *testing.T) {
	s := newServer(t)
	uid := s.Alice.User.ID
	id := add(t, s, uid, titled("Piranesi", "", books.ShelfWant))
	path := fmt.Sprintf("/books/b/%d?shelf=want", id)
	doc := s.Get(t, s.Alice, path)
	btn := doc.MustHave(".books-menu button[hx-post]")
	if got, _ := htmlassert.Attr(btn, "hx-post"); got != fmt.Sprintf("/books/find-cover/%d", id) {
		t.Errorf("first menu button posts to %q, want Find cover", got)
	}
	if got := attr(t, doc, ".books-menu form", "action"); got != fmt.Sprintf("/books/find-cover/%d", id) {
		t.Errorf("its form posts to %q without JavaScript", got)
	}
	if err := s.Store.SetCover(context.Background(), uid, id, "image/png", onePNG, books.CoverUpload); err != nil {
		t.Fatal(err)
	}
	for _, text := range texts(s.Get(t, s.Alice, path), ".books-menu button") {
		if text == "Find cover" {
			t.Error("a book with a cover still offers Find cover")
		}
	}
}

func TestFindCoverByISBNThenByTitle(t *testing.T) {
	s, _, _ := newBooksApp(t)
	uid := s.Alice.User.ID
	byISBN := withISBN(t, s, uid, "Anything", "9781635575637")
	byTitle := add(t, s, uid, titled("Piranesi", "Susanna Clarke", books.ShelfWant))
	// An ISBN Open Library has no cover for falls back to the title.
	fallback := withISBN(t, s, uid, "Piranesi", "9780316129084")
	for name, id := range map[string]int64{"by ISBN": byISBN, "by title": byTitle, "ISBN, then title": fallback} {
		doc := findCover(t, s, id)
		c, err := s.Store.Cover(context.Background(), uid, id)
		if err != nil || !bytes.Equal(c.Bytes, onePNG) || coverSource(t, s, id) != books.CoverFromOL {
			t.Errorf("%s: cover = %d bytes, %v", name, len(c.Bytes), err)
		}
		doc.MustHave("#books-book img.books-cover")
		doc.MustNotHave(".books-banner")
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
		{nothing, "Open Library has no cover for this book. You can add one with Edit details."},
		{broken, "Open Library didn't answer. Try again in a minute."},
	}
	for _, tt := range tests {
		doc := findCover(t, s, tt.id)
		if got := htmlassert.Text(doc.MustHave(".books-banner")); got != tt.want {
			t.Errorf("banner = %q, want %q", got, tt.want)
		}
	}
	if checkedAt(t, s, nothing) == "" || checkedAt(t, s, broken) != "" {
		t.Error("only a book Open Library has no cover for is marked checked")
	}
	// Without JavaScript the banner comes on the page, as a 422.
	rec := s.Post(t, s.Alice, fmt.Sprintf("/books/find-cover/%d", nothing), url.Values{"shelf": {"want"}})
	if rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("no-JS Find cover with none = %d, want 422", rec.Code)
	}
}

func TestFindCoverOfSomeoneElsesBookIsNotFound(t *testing.T) {
	s, _, _ := newBooksApp(t)
	id := withISBN(t, s, s.Bob.User.ID, "Piranesi", "9781635575637")
	rec := s.PostHX(t, s.Alice, fmt.Sprintf("/books/find-cover/%d", id), url.Values{})
	if rec.Code != http.StatusNotFound {
		t.Errorf("Alice's Find cover on Bob's book = %d, want 404", rec.Code)
	}
	if _, err := s.Store.Cover(context.Background(), s.Bob.User.ID, id); err == nil {
		t.Error("Bob's book got a cover")
	}
}
