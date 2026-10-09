package books_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/iliafrenkel/on-suite/internal/apps/books"
	"github.com/iliafrenkel/on-suite/internal/apptest"
)

type server = apptest.Server[*books.Store]

// newServer mounts ON Books with its own store as the test's handle.
func newServer(t *testing.T) *server {
	t.Helper()
	return apptest.NewServer(t, books.New(), books.NewStore)
}

func TestBooksRequiresSignIn(t *testing.T) {
	s := newServer(t)
	rec := s.Do(t, nil, httptest.NewRequest("GET", "/books/", nil))
	if rec.Code != http.StatusSeeOther {
		t.Errorf("GET /books/ anonymous = %d, want a 303 to the login page", rec.Code)
	}
}

func TestIndexRenders(t *testing.T) {
	s := newServer(t)
	s.Get(t, s.Alice, "/books/") // fails the test unless 200
}
