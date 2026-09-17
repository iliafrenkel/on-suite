// internal/apps/flash/handlers_cards_test.go
package flash_test

import (
	"net/url"
	"testing"

	"github.com/iliafrenkel/on-suite/internal/apps/flash"
)

func TestCreateAndViewCard(t *testing.T) {
	s := newServer(t)
	deck, err := s.Store.CreateDeck(t.Context(), s.Alice.User.ID, "Spanish", "")
	if err != nil {
		t.Fatal(err)
	}

	s.Submit(t, s.Alice,
		"/flash/"+itoa(deck.ID)+"/cards/new",
		url.Values{"card_type": {flash.CardTypeBasic}, "front": {"hola"}, "back": {"hello"}},
		"/flash/"+itoa(deck.ID)+"/cards/1")

	doc := s.Get(t, s.Alice, "/flash/"+itoa(deck.ID)+"/cards/")
	doc.MustHave(".card-list")
}

func TestCreateCardValidation(t *testing.T) {
	s := newServer(t)
	deck, err := s.Store.CreateDeck(t.Context(), s.Alice.User.ID, "Spanish", "")
	if err != nil {
		t.Fatal(err)
	}
	rec := s.Post(t, s.Alice, "/flash/"+itoa(deck.ID)+"/cards/new",
		url.Values{"card_type": {flash.CardTypeBasic}, "front": {"hola"}})
	if rec.Code != 400 {
		t.Errorf("creating a basic card with no back = %d, want 400", rec.Code)
	}
}

func TestCreatingACardInSomeoneElsesDeckIs404(t *testing.T) {
	s := newServer(t)
	deck, err := s.Store.CreateDeck(t.Context(), s.Alice.User.ID, "alice's", "")
	if err != nil {
		t.Fatal(err)
	}
	rec := s.PostHX(t, s.Bob, "/flash/"+itoa(deck.ID)+"/cards/new",
		url.Values{"card_type": {flash.CardTypeBasic}, "front": {"a"}, "back": {"b"}})
	if rec.Code != 404 {
		t.Errorf("creating a card in someone else's deck = %d, want 404", rec.Code)
	}
}
