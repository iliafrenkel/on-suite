package books_test

import (
	"bytes"
	"context"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/iliafrenkel/on-suite/internal/apps/books"
	"github.com/iliafrenkel/on-suite/internal/apptest"
	"github.com/iliafrenkel/on-suite/internal/htmlassert"
	"github.com/iliafrenkel/on-suite/internal/platform/web"
)

// postImport uploads content as the Import form does without JavaScript:
// multipart, the CSRF token as a field, the file as "file" (none when
// name is "").
func postImport(t *testing.T, s *server, sess *apptest.Session, name string, content []byte) *httptest.ResponseRecorder {
	t.Helper()
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	if err := mw.WriteField(web.CSRFFormField, s.CSRFToken(t, sess)); err != nil {
		t.Fatal(err)
	}
	if name != "" {
		part, err := mw.CreateFormFile("file", name)
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
	req := httptest.NewRequest("POST", "/books/import", &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	return s.Do(t, sess, req)
}

func TestImportPageIsLinkedAndTakesAFile(t *testing.T) {
	s := newServer(t)
	s.Get(t, s.Alice, "/books/").MustHave(`.books-side a[href="/books/import"]`)
	doc := s.Get(t, s.Alice, "/books/import")
	if got := attr(t, doc, "form.books-import-form", "enctype"); got != "multipart/form-data" {
		t.Errorf("enctype = %q, want multipart/form-data", got)
	}
	if got := attr(t, doc, "input#books-import-file", "name"); got != "file" {
		t.Errorf("file input name = %q, want file", got)
	}
	doc.MustHave(`a[href="/books/"]`)
	doc.MustNotHave(".books-import-result")
}

func TestImportUploadShowsWhatItDid(t *testing.T) {
	s := newServer(t)
	uid := s.Alice.User.ID
	add(t, s, uid, titled("Infinite Jest", "David Foster Wallace", books.ShelfRead))
	rec := postImport(t, s, s.Alice, "goodreads_library_export.csv", grFixture(t))
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /books/import = %d; body: %s", rec.Code, rec.Body.String())
	}
	doc := htmlassert.Parse(t, rec.Body.String())
	// Infinite Jest is already here, typed in without an ISBN: the row
	// matches it by title and author.
	if got := htmlassert.Text(doc.MustHave(".books-import-count")); got != "Imported 5 books." {
		t.Errorf("summary = %q", got)
	}
	if got := texts(doc, ".books-import-skipped li"); len(got) != 2 || got[0] != goodOmens || got[1] != "Infinite Jest" {
		t.Errorf("skipped = %q, want the second Good Omens and Infinite Jest", got)
	}
	if !strings.Contains(htmlassert.Text(doc.MustHave(".books-import-result")), "Skipped 2 books already in your library") {
		t.Errorf("result = %q", htmlassert.Text(doc.MustHave(".books-import-result")))
	}
	counts, err := s.Store.ShelfCounts(context.Background(), uid)
	if err != nil || counts[books.ShelfAll] != 6 {
		t.Errorf("Alice has %v books, %v; want 6", counts, err)
	}
	if counts, _ := s.Store.ShelfCounts(context.Background(), s.Bob.User.ID); counts[books.ShelfAll] != 0 {
		t.Errorf("Bob has %d books, want none", counts[books.ShelfAll])
	}
}

func TestImportUploadRefusesWhatItCantUse(t *testing.T) {
	s := newServer(t)
	tests := []struct {
		name, file string
		content    []byte
		want       string
	}{
		{"no file", "", nil, "Choose your Goodreads export first."},
		{"not an export", "people.csv", []byte("Name,Email\nAnn,ann@example.com\n"),
			"That doesn't look like a Goodreads library export: it has no “Title” column."},
		{"a bad row", "goodreads.csv", []byte(grHeader + grLine("A Book", "9", "", "", "", "read", "", "1")),
			"Line 2: My Rating must be a whole number from 0 to 5."},
		{"too big", "huge.csv", bytes.Repeat([]byte("x"), books.MaxImportBytes+1), "That file is larger than 10 MB."},
	}
	for _, tt := range tests {
		rec := postImport(t, s, s.Alice, tt.file, tt.content)
		if rec.Code != http.StatusUnprocessableEntity {
			t.Errorf("%s: status %d, want 422", tt.name, rec.Code)
			continue
		}
		doc := htmlassert.Parse(t, rec.Body.String())
		if got := htmlassert.Text(doc.MustHave("#books-import-error")); got != tt.want {
			t.Errorf("%s: error = %q, want %q", tt.name, got, tt.want)
		}
		if got := attr(t, doc, "#books-import-file", "aria-describedby"); got != "books-import-error" {
			t.Errorf("%s: the file input isn't described by the error", tt.name)
		}
	}
	if counts, _ := s.Store.ShelfCounts(context.Background(), s.Alice.User.ID); counts[books.ShelfAll] != 0 {
		t.Errorf("%d books after refused imports, want none", counts[books.ShelfAll])
	}
}
