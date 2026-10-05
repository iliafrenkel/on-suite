package later_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/iliafrenkel/on-suite/internal/apps/later"
	"github.com/iliafrenkel/on-suite/internal/apptest"
)

type server = apptest.Server[*later.Store]

// newServer mounts ON Later with its own store as the test's handle.
func newServer(t *testing.T) *server {
	t.Helper()
	return apptest.NewServer(t, later.New(), later.NewStore)
}

func TestLaterRequiresSignIn(t *testing.T) {
	s := newServer(t)
	rec := s.Do(t, nil, httptest.NewRequest("GET", "/later/", nil))
	if rec.Code != http.StatusSeeOther {
		t.Errorf("GET /later/ anonymous = %d, want a 303 to the login page", rec.Code)
	}
}

func TestIndexRendersForASignedInUser(t *testing.T) {
	s := newServer(t)
	doc := s.Get(t, s.Alice, "/later/")
	doc.MustHave(".later-page")
}
