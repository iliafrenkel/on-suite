package books_test

import (
	"bytes"
	"context"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/iliafrenkel/on-suite/internal/apps/books"
	"github.com/iliafrenkel/on-suite/internal/htmlassert"
	"github.com/iliafrenkel/on-suite/internal/platform/web"
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
	btn := doc.MustHave("form.books-form button.books-find-cover")
	if got, _ := htmlassert.Attr(btn, "formaction"); got != fmt.Sprintf("/books/find-cover/%d", id) {
		t.Errorf("Find cover formaction = %q", got)
	}
	if _, ok := htmlassert.Attr(btn, "form"); ok {
		t.Error("Find cover names another form; it should post the Edit form itself (#591)")
	}
	doc.MustNotHave("form#books-find-cover")
	// What it posts carries the CSRF token and the list (#594).
	doc.MustHave(fmt.Sprintf(`form.books-form input[name="%s"]`, web.CSRFFormField))
	if got := attr(t, doc, `form.books-form input[name="shelf"]`, "value"); got != "want" {
		t.Errorf("the Edit form's shelf = %q, want the list the page came from", got)
	}
	// Enter in a text field submits with the first submit button, which
	// must be Save, not Find cover.
	first := doc.QueryAll("form.books-form button")[0]
	if _, ok := htmlassert.Attr(first, "formaction"); ok {
		t.Error("the Edit form's first button has a formaction; Enter would not save")
	}
	if got, _ := htmlassert.Attr(first, "type"); got != "submit" {
		t.Errorf("the Edit form's first button has type %q, want submit", got)
	}
	if err := s.Store.SetCover(context.Background(), uid, id, "image/png", onePNG, books.CoverUpload); err != nil {
		t.Fatal(err)
	}
	doc = s.Get(t, s.Alice, path)
	doc.MustNotHave("button.books-find-cover")
	// The default Save is drawn only alongside Find cover.
	if got, _ := htmlassert.Attr(doc.QueryAll("form.books-form button")[0], "class"); got == "visually-hidden" {
		t.Error("the hidden default Save is drawn on a book with a cover")
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
		rec := findCover(t, s, id)
		if rec.Code != http.StatusOK {
			t.Errorf("%s: Find cover = %d, want 200: the Edit page again, with the cover", name, rec.Code)
		}
		doc := htmlassert.Parse(t, rec.Body.String())
		if got := htmlassert.Text(doc.MustHave(".books-cover-field .notice")); got != "Found a cover on Open Library." {
			t.Errorf("%s: notice = %q", name, got)
		}
		doc.MustHave(".books-cover-field img.books-cover")
		c, err := s.Store.Cover(context.Background(), uid, id)
		if err != nil || !bytes.Equal(c.Bytes, onePNG) || coverSource(t, s, id) != books.CoverFromOL {
			t.Errorf("%s: cover = %d bytes, %v", name, len(c.Bytes), err)
		}
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

func TestFindCoverKeepsWhatWasTyped(t *testing.T) {
	s, _, _ := newBooksApp(t)
	ctx := context.Background()
	uid := s.Alice.User.ID
	for _, tt := range []struct {
		title string
		code  int
	}{
		{"Piranesi", http.StatusOK},                         // found
		{"Nothing like it", http.StatusUnprocessableEntity}, // none to be had
	} {
		id := add(t, s, uid, titled(tt.title, "", books.ShelfWant))
		rec := s.Post(t, s.Alice, fmt.Sprintf("/books/find-cover/%d", id), url.Values{"shelf": {"want"},
			"title": {tt.title}, "subtitle": {"Typed, not saved"}, "tags": {"typed, tags"}, "description": {"A typed description."}})
		if rec.Code != tt.code {
			t.Errorf("%s: Find cover = %d, want %d", tt.title, rec.Code, tt.code)
		}
		doc := htmlassert.Parse(t, rec.Body.String())
		if got := attr(t, doc, "input#books-subtitle", "value"); got != "Typed, not saved" {
			t.Errorf("%s: subtitle = %q, want what was typed (#591)", tt.title, got)
		}
		if got := attr(t, doc, "input#books-tags", "value"); got != "typed, tags" {
			t.Errorf("%s: tags = %q, want what was typed", tt.title, got)
		}
		if got := htmlassert.Text(doc.MustHave("textarea#books-description")); got != "A typed description." {
			t.Errorf("%s: description = %q, want what was typed", tt.title, got)
		}
		if got := htmlassert.Text(doc.MustHave(".books-back")); got != tt.title {
			t.Errorf("%s: back link = %q, want the saved title", tt.title, got)
		}
		if b, _ := s.Store.Get(ctx, uid, id); b.Subtitle != "" || len(b.Tags) != 0 {
			t.Errorf("%s: Find cover saved the form (subtitle %q, tags %v); only Save does", tt.title, b.Subtitle, b.Tags)
		}
	}
}

func TestFindCoverLooksUpWhatTheFormSays(t *testing.T) {
	s, _, _ := newBooksApp(t)
	uid := s.Alice.User.ID
	find := func(id int64, form url.Values) int {
		t.Helper()
		form.Set("shelf", "want")
		return s.Post(t, s.Alice, fmt.Sprintf("/books/find-cover/%d", id), form).Code
	}

	// Saved without an ISBN, under a title Open Library has nothing for;
	// the ISBN typed into the form has a cover.
	nothing := add(t, s, uid, titled("Nothing like it", "", books.ShelfWant))
	if code := find(nothing, url.Values{"title": {"Nothing like it"}, "isbn": {"9781635575637"}}); code != http.StatusOK {
		t.Errorf("Find cover with a typed ISBN = %d, want 200: it looks up the typed ISBN", code)
	}

	// Saved without its author, which Open Library needs for this title;
	// the author is typed into the form.
	wind := add(t, s, uid, titled("The Name of the Wind", "", books.ShelfWant))
	if code := find(wind, url.Values{"title": {"The Name of the Wind"}}); code != http.StatusUnprocessableEntity {
		t.Fatalf("Find cover without the author = %d, want 422 (the fake needs the author)", code)
	}
	if code := find(wind, url.Values{"title": {"The Name of the Wind"}, "authors": {"Patrick Rothfuss"}}); code != http.StatusOK {
		t.Errorf("Find cover with a typed author = %d, want 200: it looks up the typed author", code)
	}

	// A form that wouldn't save (no title) falls back to the saved book.
	piranesi := add(t, s, uid, titled("Piranesi", "", books.ShelfWant))
	if code := find(piranesi, url.Values{"title": {""}}); code != http.StatusOK {
		t.Errorf("Find cover with an empty title box = %d, want 200 from the saved title", code)
	}
}

// A miss marks the book checked only when the saved ISBN is the one looked
// up: the backfill looks up the saved ISBN, so a miss on an ISBN that was
// typed and never saved (the Edit page cancelled) mustn't stop it (#598).
func TestFindCoverMarksCheckedOnlyForTheSavedISBN(t *testing.T) {
	s, _, _ := newBooksApp(t)
	uid := s.Alice.User.ID
	find := func(id int64, isbn string) {
		t.Helper()
		rec := s.Post(t, s.Alice, fmt.Sprintf("/books/find-cover/%d", id), url.Values{"shelf": {"want"},
			"title": {"Nothing like it"}, "isbn": {isbn}})
		if rec.Code != http.StatusUnprocessableEntity {
			t.Fatalf("Find cover with no cover to find = %d, want 422", rec.Code)
		}
	}

	// Saved with an ISBN that has a cover; the typed one has none.
	typed := withISBN(t, s, uid, "Nothing like it", "9781635575637")
	find(typed, "9780316129084")
	if at := checkedAt(t, s, typed); at != "" {
		t.Errorf("a miss on a typed, unsaved ISBN marked the book checked (%s): the backfill would skip the saved one", at)
	}

	// The typed ISBN is the saved one, written differently.
	saved := withISBN(t, s, uid, "Nothing like it", "9780316129084")
	find(saved, "978-0-316-12908-4")
	if checkedAt(t, s, saved) == "" {
		t.Error("a miss on the saved ISBN didn't mark the book checked")
	}
}

// postEditForm posts Alice's Edit form as a browser does, multipart with
// a file in the upload box, to path.
func postEditForm(t *testing.T, s *server, path string, fields url.Values, file []byte) *httptest.ResponseRecorder {
	t.Helper()
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	fields.Set(web.CSRFFormField, s.CSRFToken(t, s.Alice))
	for k, vs := range fields {
		for _, v := range vs {
			if err := mw.WriteField(k, v); err != nil {
				t.Fatal(err)
			}
		}
	}
	part, err := mw.CreateFormFile("cover_file", "cover.jpg")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(file); err != nil {
		t.Fatal(err)
	}
	if err := mw.Close(); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, path, &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	return s.Do(t, s.Alice, req)
}

func TestFindCoverTakesTheEditFormsBody(t *testing.T) {
	s, _, _ := newBooksApp(t)
	id := add(t, s, s.Alice.User.ID, titled("Piranesi", "", books.ShelfWant))
	big := bytes.Repeat([]byte{0}, 3<<19) // 1.5 MiB: over the suite's 1 MiB default
	rec := postEditForm(t, s, fmt.Sprintf("/books/find-cover/%d", id), url.Values{"shelf": {"want"}, "title": {"Piranesi"}}, big)
	if rec.Code != http.StatusOK {
		t.Errorf("Find cover with an image chosen = %d, want 200: it posts the Edit form, so it has Save's body budget", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "Found a cover on Open Library.") {
		t.Error("the chosen file got in the way of the lookup")
	}
}
