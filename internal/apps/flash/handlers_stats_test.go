// internal/apps/flash/handlers_stats_test.go
package flash_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/iliafrenkel/on-suite/internal/apps/flash"
	"github.com/iliafrenkel/on-suite/internal/apptest"
)

func TestStatsPageZeroStateForBrandNewAccount(t *testing.T) {
	s := apptest.NewServer(t, flash.New(), flash.NewStore)
	rec := s.Do(t, s.Alice, httptest.NewRequest(http.MethodGet, "/flash/stats", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /flash/stats = %d; body: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, "Streak") || !strings.Contains(body, "Cards due") {
		t.Errorf("stats page missing expected tile labels; body: %s", body)
	}
	if !strings.Contains(body, "No decks yet.") {
		t.Errorf("stats page should show the empty per-deck state; body: %s", body)
	}
}

func TestStatsPageRequiresSignIn(t *testing.T) {
	s := apptest.NewServer(t, flash.New(), flash.NewStore)
	rec := s.Do(t, nil, httptest.NewRequest(http.MethodGet, "/flash/stats", nil))
	if rec.Code == http.StatusOK {
		t.Fatalf("GET /flash/stats with no session = 200, want a redirect/401")
	}
}

func TestStatsPageShowsPopulatedData(t *testing.T) {
	s := apptest.NewServer(t, flash.New(), flash.NewStore)
	deck, err := s.Store.CreateDeck(t.Context(), s.Alice.User.ID, "Spanish", "")
	if err != nil {
		t.Fatal(err)
	}
	card, err := s.Store.CreateCard(t.Context(), s.Alice.User.ID, deck.ID, flash.CardTypeBasic, "hola", "hello", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Store.GradeCard(t.Context(), s.Alice.User.ID, card.ID, flash.RatingGood, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}

	rec := s.Do(t, s.Alice, httptest.NewRequest(http.MethodGet, "/flash/stats", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /flash/stats = %d; body: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "Spanish") {
		t.Errorf("stats page should list the Spanish deck in the per-deck table; body: %s", rec.Body.String())
	}
}
