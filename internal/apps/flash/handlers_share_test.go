// internal/apps/flash/handlers_share_test.go
package flash_test

import (
	"net/url"
	"strconv"
	"testing"

	"github.com/iliafrenkel/on-suite/internal/apps/flash"
	"github.com/iliafrenkel/on-suite/internal/apptest"
)

func newShareServer(t *testing.T) *apptest.Server[*flash.Store] {
	t.Helper()
	return apptest.NewServer(t, flash.New(), flash.NewStore)
}

func createDeckHX(t *testing.T, s *apptest.Server[*flash.Store], sess *apptest.Session, name string) int64 {
	t.Helper()
	rec := s.PostHX(t, sess, "/flash/new", url.Values{"name": {name}, "description": {""}})
	if rec.Code != 201 {
		t.Fatalf("create deck: %d; body: %s", rec.Code, rec.Body.String())
	}
	loc := rec.Header().Get("HX-Push-Url")
	if loc == "" {
		t.Fatalf("create deck: no HX-Push-Url header")
	}
	parts := loc[len("/flash/"):]
	id, err := strconv.ParseInt(parts, 10, 64)
	if err != nil {
		t.Fatalf("parse deck id from %q: %v", loc, err)
	}
	return id
}

func TestShareDeckHandlerCreatesOfferForRecipient(t *testing.T) {
	s := newShareServer(t)
	deckID := createDeckHX(t, s, s.Alice, "Spanish")

	rec := s.PostHX(t, s.Alice, "/flash/"+strconv.FormatInt(deckID, 10)+"/share",
		url.Values{"to_user_id": {strconv.FormatInt(s.Bob.User.ID, 10)}})
	if rec.Code != 200 {
		t.Fatalf("share: %d; body: %s", rec.Code, rec.Body.String())
	}

	offers, err := s.Store.SharesForRecipient(t.Context(), s.Bob.User.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(offers) != 1 || offers[0].DeckName != "Spanish" {
		t.Fatalf("offers = %+v, want one offer for Spanish", offers)
	}
}

func TestShareDeckHandlerRejectsNonOwner(t *testing.T) {
	s := newShareServer(t)
	deckID := createDeckHX(t, s, s.Alice, "Spanish")

	rec := s.PostHX(t, s.Bob, "/flash/"+strconv.FormatInt(deckID, 10)+"/share",
		url.Values{"to_user_id": {strconv.FormatInt(s.Alice.User.ID, 10)}})
	if rec.Code != 404 {
		t.Fatalf("share by non-owner: %d, want 404; body: %s", rec.Code, rec.Body.String())
	}
}

func TestRevokeAndAdoptShareHandlers(t *testing.T) {
	s := newShareServer(t)
	deckID := createDeckHX(t, s, s.Alice, "Spanish")
	deckIDStr := strconv.FormatInt(deckID, 10)

	s.PostHX(t, s.Alice, "/flash/"+deckIDStr+"/share", url.Values{"to_user_id": {strconv.FormatInt(s.Bob.User.ID, 10)}})
	offers, err := s.Store.SharesForRecipient(t.Context(), s.Bob.User.ID)
	if err != nil || len(offers) != 1 {
		t.Fatalf("setup: offers = %+v, err = %v", offers, err)
	}
	shareIDStr := strconv.FormatInt(offers[0].ID, 10)

	// Bob adopts.
	rec := s.PostHX(t, s.Bob, "/flash/shared/adopt", url.Values{"share_id": {shareIDStr}})
	if rec.Code != 200 {
		t.Fatalf("adopt: %d; body: %s", rec.Code, rec.Body.String())
	}
	bobDecks, err := s.Store.ListDecks(t.Context(), s.Bob.User.ID)
	if err != nil || len(bobDecks) != 1 || bobDecks[0].Name != "Spanish" {
		t.Fatalf("bob's decks = %+v, err = %v", bobDecks, err)
	}

	// Revoking the now-adopted share fails.
	rec = s.PostHX(t, s.Alice, "/flash/"+deckIDStr+"/share/"+shareIDStr+"/revoke", url.Values{})
	if rec.Code != 400 {
		t.Fatalf("revoke adopted share: %d, want 400; body: %s", rec.Code, rec.Body.String())
	}
}

func TestDeclineShareHandler(t *testing.T) {
	s := newShareServer(t)
	deckID := createDeckHX(t, s, s.Alice, "Spanish")
	s.PostHX(t, s.Alice, "/flash/"+strconv.FormatInt(deckID, 10)+"/share", url.Values{"to_user_id": {strconv.FormatInt(s.Bob.User.ID, 10)}})
	offers, err := s.Store.SharesForRecipient(t.Context(), s.Bob.User.ID)
	if err != nil || len(offers) != 1 {
		t.Fatalf("setup: offers = %+v, err = %v", offers, err)
	}

	rec := s.PostHX(t, s.Bob, "/flash/shared/decline", url.Values{"share_id": {strconv.FormatInt(offers[0].ID, 10)}})
	if rec.Code != 200 {
		t.Fatalf("decline: %d; body: %s", rec.Code, rec.Body.String())
	}
	offers, err = s.Store.SharesForRecipient(t.Context(), s.Bob.User.ID)
	if err != nil || len(offers) != 0 {
		t.Fatalf("offers after decline = %+v, want none", offers)
	}
}
