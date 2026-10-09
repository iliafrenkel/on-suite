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
	"sync/atomic"
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

func TestAFailingEditDoesNotFetchTheCover(t *testing.T) {
	s, _ := newServerWithOL(t)
	var hits atomic.Int32
	img := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		_, _ = w.Write(onePNG)
	}))
	defer img.Close()
	id := add(t, s, s.Alice.User.ID, titled("Piranesi", "", books.ShelfWant))

	form := details("") // no title: the text fields fail
	form.Set("cover_url", img.URL+"/cover.png")
	rec := postMultipart(t, s, s.Alice, fmt.Sprintf("/books/edit/%d", id), form, "", nil)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("= %d, want 422", rec.Code)
	}
	if n := hits.Load(); n != 0 {
		t.Errorf("the pasted address was fetched %d times, want 0", n)
	}
	if got := attr(t, htmlassert.Parse(t, rec.Body.String()), `input[name="cover_url"]`, "value"); got != img.URL+"/cover.png" {
		t.Errorf("cover_url echoed as %q", got)
	}
}
