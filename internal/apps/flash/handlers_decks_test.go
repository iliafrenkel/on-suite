package flash_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/iliafrenkel/on-suite/internal/apps/flash"
	"github.com/iliafrenkel/on-suite/internal/apptest"
	"github.com/iliafrenkel/on-suite/internal/htmlassert"
)

func newServer(t *testing.T) *apptest.Server[*flash.Store] {
	t.Helper()
	return apptest.NewServer(t, flash.New(), flash.NewStore)
}

func TestFlashRequiresSignIn(t *testing.T) {
	s := newServer(t)
	rec := s.Do(t, nil, httpGet(t, "/flash/"))
	if rec.Code != 303 && rec.Code != 401 {
		t.Errorf("GET /flash/ signed out = %d, want a redirect-to-login or 401", rec.Code)
	}
}

func TestCreateDeckRequiresCSRF(t *testing.T) {
	s := newServer(t)
	req := httpPost(t, "/flash/new", url.Values{"name": {"Spanish"}})
	rec := s.Do(t, s.Alice, req)
	if rec.Code != 403 {
		t.Errorf("POST /flash/new without CSRF = %d, want 403", rec.Code)
	}
}

func TestCreateAndViewDeck(t *testing.T) {
	s := newServer(t)
	s.Submit(t, s.Alice, "/flash/new", url.Values{"name": {"Spanish"}, "description": {"Travel"}}, "/flash/1")

	doc := s.Get(t, s.Alice, "/flash/")
	doc.MustHave(".deck-list")
	row := doc.MustHave(`a.deck-row`)
	if got := htmlassert.Text(row); got == "" {
		t.Error("the new deck does not appear in the list")
	}
}

func TestCreateDeckValidation(t *testing.T) {
	s := newServer(t)
	rec := s.Post(t, s.Alice, "/flash/new", url.Values{"name": {"  "}})
	if rec.Code != 400 {
		t.Errorf("POST /flash/new with a blank name = %d, want 400", rec.Code)
	}
	doc := htmlassert.Parse(t, rec.Body.String())
	doc.MustHave(".notice-error")
}

func TestViewingSomeoneElsesDeckIs404(t *testing.T) {
	s := newServer(t)
	deck, err := s.Store.CreateDeck(t.Context(), s.Alice.User.ID, "alice's", "")
	if err != nil {
		t.Fatal(err)
	}
	rec := s.Do(t, s.Bob, httpGet(t, "/flash/"+itoa(deck.ID)))
	if rec.Code != 404 {
		t.Errorf("GET another user's deck = %d, want 404", rec.Code)
	}
}

func TestDeleteDeckRequiresCSRFAndPOST(t *testing.T) {
	s := newServer(t)
	deck, err := s.Store.CreateDeck(t.Context(), s.Alice.User.ID, "doomed", "")
	if err != nil {
		t.Fatal(err)
	}
	req := httpPost(t, "/flash/"+itoa(deck.ID)+"/delete", url.Values{})
	rec := s.Do(t, s.Alice, req)
	if rec.Code != 403 {
		t.Errorf("delete without CSRF = %d, want 403", rec.Code)
	}
}

func httpGet(t *testing.T, path string) *http.Request {
	t.Helper()
	return httptest.NewRequest(http.MethodGet, path, nil)
}

func httpPost(t *testing.T, path string, form url.Values) *http.Request {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return req
}

func itoa(id int64) string { return strconv.FormatInt(id, 10) }
