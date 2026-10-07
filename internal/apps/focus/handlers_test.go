package focus_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/iliafrenkel/on-suite/internal/apps/focus"
	"github.com/iliafrenkel/on-suite/internal/apptest"
)

type server = apptest.Server[*focus.Store]

// newServer mounts ON Focus with its own store as the test's handle.
func newServer(t *testing.T) *server {
	t.Helper()
	return apptest.NewServer(t, focus.New(), focus.NewStore)
}

func TestFocusRequiresSignIn(t *testing.T) {
	s := newServer(t)
	rec := s.Do(t, nil, httptest.NewRequest("GET", "/focus/", nil))
	if rec.Code != http.StatusSeeOther {
		t.Errorf("GET /focus/ anonymous = %d, want a 303 to the login page", rec.Code)
	}
}

func TestIndexShowsTheEmptyState(t *testing.T) {
	s := newServer(t)
	doc := s.Get(t, s.Alice, "/focus/")
	doc.MustHave(".focus-page")
	doc.MustHave(".focus-empty")
	doc.MustHave(`a[href="/focus/new"]`)
}
