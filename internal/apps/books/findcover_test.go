package books_test

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/iliafrenkel/on-suite/internal/apps/books"
	"github.com/iliafrenkel/on-suite/internal/htmlassert"
)

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
