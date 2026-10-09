package books_test

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/iliafrenkel/on-suite/internal/apps/books"
	"github.com/iliafrenkel/on-suite/internal/apptest"
)

// newServerWithOL is newServer with the app's Open Library client pointed
// at a fake one, whose URL it also returns.
func newServerWithOL(t *testing.T) (*server, string) {
	t.Helper()
	a := books.New()
	s := apptest.NewServer(t, a, books.NewStore)
	ol := fakeOpenLibrary(t).URL
	a.UseOpenLibraryForTest(ol)
	return s, ol
}

func get(t *testing.T, s *server, sess *apptest.Session, path string, header ...string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest("GET", path, nil)
	for i := 0; i+1 < len(header); i += 2 {
		req.Header.Set(header[i], header[i+1])
	}
	return s.Do(t, sess, req)
}

func TestCoverIsServedWithLongCaching(t *testing.T) {
	s := newServer(t)
	id := add(t, s, s.Alice.User.ID, titled("Piranesi", "", books.ShelfWant))
	if err := s.Store.SetCover(context.Background(), s.Alice.User.ID, id, "image/png", onePNG, books.CoverUpload); err != nil {
		t.Fatal(err)
	}
	c, _ := s.Store.Cover(context.Background(), s.Alice.User.ID, id)
	path := fmt.Sprintf("/books/cover/%d?v=%s", id, c.Version)

	rec := get(t, s, s.Alice, path)
	if rec.Code != http.StatusOK || !bytes.Equal(rec.Body.Bytes(), onePNG) {
		t.Fatalf("GET cover = %d, %d bytes", rec.Code, rec.Body.Len())
	}
	h := rec.Header()
	for name, want := range map[string]string{
		"Content-Type":           "image/png",
		"Cache-Control":          "private, max-age=31536000, immutable",
		"ETag":                   `"` + c.Version + `"`,
		"X-Content-Type-Options": "nosniff",
	} {
		if got := h.Get(name); got != want {
			t.Errorf("%s = %q, want %q", name, got, want)
		}
	}
	if rec := get(t, s, s.Alice, path, "If-None-Match", `"`+c.Version+`"`); rec.Code != http.StatusNotModified {
		t.Errorf("conditional GET = %d, want 304", rec.Code)
	}
}

func TestCoverIsNotFoundForOthersOrWhenMissing(t *testing.T) {
	s := newServer(t)
	with := add(t, s, s.Alice.User.ID, titled("Piranesi", "", books.ShelfWant))
	without := add(t, s, s.Alice.User.ID, titled("Emma", "", books.ShelfWant))
	if err := s.Store.SetCover(context.Background(), s.Alice.User.ID, with, "image/png", onePNG, books.CoverUpload); err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		sess *apptest.Session
		path string
	}{
		{s.Bob, fmt.Sprintf("/books/cover/%d", with)},
		{s.Alice, fmt.Sprintf("/books/cover/%d", without)},
		{s.Alice, "/books/cover/x"},
	} {
		if rec := get(t, s, tt.sess, tt.path); rec.Code != http.StatusNotFound {
			t.Errorf("GET %s = %d, want 404", tt.path, rec.Code)
		}
	}
}

func TestThumbnailsAreProxied(t *testing.T) {
	s, _ := newServerWithOL(t)
	rec := get(t, s, s.Alice, "/books/olcover/10226290")
	if rec.Code != http.StatusOK || !bytes.Equal(rec.Body.Bytes(), onePNG) {
		t.Fatalf("GET thumbnail = %d, %d bytes", rec.Code, rec.Body.Len())
	}
	if got := rec.Header().Get("Content-Type"); got != "image/png" {
		t.Errorf("Content-Type = %q, want the sniffed image/png", got)
	}
	if got := rec.Header().Get("Cache-Control"); got != "private, max-age=86400" {
		t.Errorf("Cache-Control = %q", got)
	}
	for _, path := range []string{"/books/olcover/abc", "/books/olcover/0", "/books/olcover/1234567890123", "/books/olcover/42"} {
		if rec := get(t, s, s.Alice, path); rec.Code != http.StatusNotFound {
			t.Errorf("GET %s = %d, want 404", path, rec.Code)
		}
	}
	if rec := s.Do(t, nil, httptest.NewRequest("GET", "/books/olcover/10226290", nil)); rec.Code != http.StatusSeeOther {
		t.Errorf("anonymous thumbnail = %d, want the sign-in redirect", rec.Code)
	}
}
