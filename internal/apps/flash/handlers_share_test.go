// internal/apps/flash/handlers_share_test.go
package flash_test

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/iliafrenkel/on-suite/internal/apps/flash"
	"github.com/iliafrenkel/on-suite/internal/apptest"
	"github.com/iliafrenkel/on-suite/internal/htmlassert"
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

// TestSharedWithMeRefreshesOutOfBandOnAdoptAndDecline covers finding #2: the
// "Shared with me" section lives in the full-page "content" block only, so
// every HTMX share/revoke/adopt/decline response (which re-renders just
// "deck-detail-with-list") must carry its own out-of-band copy of that
// section, keeping the resolved offer's row off the recipient's screen
// without a full reload.
func TestSharedWithMeRefreshesOutOfBandOnAdoptAndDecline(t *testing.T) {
	s := newShareServer(t)
	deckID := createDeckHX(t, s, s.Alice, "Spanish")
	s.PostHX(t, s.Alice, "/flash/"+strconv.FormatInt(deckID, 10)+"/share",
		url.Values{"to_user_id": {strconv.FormatInt(s.Bob.User.ID, 10)}})
	offers, err := s.Store.SharesForRecipient(t.Context(), s.Bob.User.ID)
	if err != nil || len(offers) != 1 {
		t.Fatalf("setup: offers = %+v, err = %v", offers, err)
	}
	shareID := strconv.FormatInt(offers[0].ID, 10)

	rec := s.PostHX(t, s.Bob, "/flash/shared/decline", url.Values{"share_id": {shareID}})
	if rec.Code != 200 {
		t.Fatalf("decline: %d; body: %s", rec.Code, rec.Body.String())
	}
	doc := htmlassert.Parse(t, rec.Body.String())
	oob := doc.MustHave("#shared-with-me")
	if _, ok := htmlassert.Attr(oob, "hx-swap-oob"); !ok {
		t.Error("#shared-with-me in the fragment response is not marked hx-swap-oob")
	}
	if strings.Contains(htmlassert.Text(oob), "Spanish") {
		t.Errorf("declined offer for Spanish still present in #shared-with-me: %s", htmlassert.Text(oob))
	}

	// Re-share, then adopt, and check the same thing on the adopt path.
	s.PostHX(t, s.Alice, "/flash/"+strconv.FormatInt(deckID, 10)+"/share",
		url.Values{"to_user_id": {strconv.FormatInt(s.Bob.User.ID, 10)}})
	offers, err = s.Store.SharesForRecipient(t.Context(), s.Bob.User.ID)
	if err != nil || len(offers) != 1 {
		t.Fatalf("setup 2: offers = %+v, err = %v", offers, err)
	}
	shareID = strconv.FormatInt(offers[0].ID, 10)

	rec = s.PostHX(t, s.Bob, "/flash/shared/adopt", url.Values{"share_id": {shareID}})
	if rec.Code != 200 {
		t.Fatalf("adopt: %d; body: %s", rec.Code, rec.Body.String())
	}
	doc = htmlassert.Parse(t, rec.Body.String())
	oob = doc.MustHave("#shared-with-me")
	if _, ok := htmlassert.Attr(oob, "hx-swap-oob"); !ok {
		t.Error("#shared-with-me in the adopt fragment response is not marked hx-swap-oob")
	}
	if strings.Contains(htmlassert.Text(oob), "Spanish") {
		t.Errorf("adopted offer for Spanish still present in #shared-with-me: %s", htmlassert.Text(oob))
	}
}

// TestSharedWithMeFormsCarryCSRFTokenWithNoDeckSelected covers finding #3:
// on GET /flash/ with no deck selected, deckDetailView{}.CSRFToken is empty,
// so the Adopt/Dismiss forms must fall back to the shell's own CSRF token
// (always populated) rather than the empty per-deck one.
func TestSharedWithMeFormsCarryCSRFTokenWithNoDeckSelected(t *testing.T) {
	s := newShareServer(t)
	deckID := createDeckHX(t, s, s.Alice, "Spanish")
	s.PostHX(t, s.Alice, "/flash/"+strconv.FormatInt(deckID, 10)+"/share",
		url.Values{"to_user_id": {strconv.FormatInt(s.Bob.User.ID, 10)}})

	doc := s.Get(t, s.Bob, "/flash/")
	for _, sel := range []string{
		`form[action="/flash/shared/adopt"] input[name=csrf_token]`,
		`form[action="/flash/shared/decline"] input[name=csrf_token]`,
	} {
		n := doc.MustHave(sel)
		v, _ := htmlassert.Attr(n, "value")
		if v == "" {
			t.Errorf("%s has an empty CSRF token value with no deck selected", sel)
		}
	}
}

// TestSharedWithMeShowsSharerUsername covers finding #4: the recipient's
// list must show who shared a deck, not just its name, since with more than
// one other account on the instance the name alone is ambiguous.
func TestSharedWithMeShowsSharerUsername(t *testing.T) {
	s := newShareServer(t)
	deckID := createDeckHX(t, s, s.Alice, "Spanish")
	s.PostHX(t, s.Alice, "/flash/"+strconv.FormatInt(deckID, 10)+"/share",
		url.Values{"to_user_id": {strconv.FormatInt(s.Bob.User.ID, 10)}})

	doc := s.Get(t, s.Bob, "/flash/")
	shared := doc.MustHave("#shared-with-me")
	text := htmlassert.Text(shared)
	if !strings.Contains(text, "alice") {
		t.Errorf("shared-with-me text = %q, want it to include the sharer's username %q", text, "alice")
	}
	if !strings.Contains(text, "Spanish") {
		t.Errorf("shared-with-me text = %q, want it to include the deck name %q", text, "Spanish")
	}
}

// TestFullShareCycleThroughHTTP covers finding #5: it drives a full
// share -> adopt -> re-share -> merge cycle entirely through the HTTP
// layer (the real POST routes), rather than calling the store methods
// directly the way TestAdoptShareMergeCopiesOnlyNewCards does, and also
// checks a plain (non-HTMX) GET /flash/ renders the offer through the
// real full-page template path — the test that would have caught findings
// #2 and #3 had it existed before this review.
func TestFullShareCycleThroughHTTP(t *testing.T) {
	s := newShareServer(t)
	deckID := createDeckHX(t, s, s.Alice, "Spanish")
	deckIDStr := strconv.FormatInt(deckID, 10)

	// Alice adds a card to her deck.
	rec := s.PostHX(t, s.Alice, "/flash/"+deckIDStr+"/cards/new",
		url.Values{"card_type": {"basic"}, "front": {"hola"}, "back": {"hello"}, "notes": {""}})
	if rec.Code != http.StatusCreated {
		t.Fatalf("create card: %d; body: %s", rec.Code, rec.Body.String())
	}

	// Alice shares with Bob.
	rec = s.PostHX(t, s.Alice, "/flash/"+deckIDStr+"/share",
		url.Values{"to_user_id": {strconv.FormatInt(s.Bob.User.ID, 10)}})
	if rec.Code != http.StatusOK {
		t.Fatalf("share: %d; body: %s", rec.Code, rec.Body.String())
	}

	// A plain, non-HTMX GET /flash/ as Bob renders the offer, deck name and
	// sharer's username, through the real full-page template.
	doc := s.Get(t, s.Bob, "/flash/")
	sharedText := htmlassert.Text(doc.MustHave("#shared-with-me"))
	if !strings.Contains(sharedText, "alice") || !strings.Contains(sharedText, "Spanish") {
		t.Fatalf("shared-with-me text = %q, want alice's username and the deck name", sharedText)
	}

	offers, err := s.Store.SharesForRecipient(t.Context(), s.Bob.User.ID)
	if err != nil || len(offers) != 1 {
		t.Fatalf("setup: offers = %+v, err = %v", offers, err)
	}
	shareID := strconv.FormatInt(offers[0].ID, 10)

	// Bob adopts via the HTTP route.
	rec = s.PostHX(t, s.Bob, "/flash/shared/adopt", url.Values{"share_id": {shareID}})
	if rec.Code != http.StatusOK {
		t.Fatalf("adopt: %d; body: %s", rec.Code, rec.Body.String())
	}
	bobDecks, err := s.Store.ListDecks(t.Context(), s.Bob.User.ID)
	if err != nil || len(bobDecks) != 1 {
		t.Fatalf("bob's decks = %+v, err = %v", bobDecks, err)
	}
	bobDeckID := bobDecks[0].ID
	bobCards, err := s.Store.ListCards(t.Context(), s.Bob.User.ID, bobDeckID)
	if err != nil || len(bobCards) != 1 {
		t.Fatalf("bob's cards after adopt = %+v, err = %v", bobCards, err)
	}

	// Alice adds another card and re-shares.
	rec = s.PostHX(t, s.Alice, "/flash/"+deckIDStr+"/cards/new",
		url.Values{"card_type": {"basic"}, "front": {"adios"}, "back": {"goodbye"}, "notes": {""}})
	if rec.Code != http.StatusCreated {
		t.Fatalf("create second card: %d; body: %s", rec.Code, rec.Body.String())
	}
	rec = s.PostHX(t, s.Alice, "/flash/"+deckIDStr+"/share",
		url.Values{"to_user_id": {strconv.FormatInt(s.Bob.User.ID, 10)}})
	if rec.Code != http.StatusOK {
		t.Fatalf("re-share: %d; body: %s", rec.Code, rec.Body.String())
	}

	offers, err = s.Store.SharesForRecipient(t.Context(), s.Bob.User.ID)
	if err != nil || len(offers) != 1 {
		t.Fatalf("setup 2: offers = %+v, err = %v", offers, err)
	}
	mergeShareID := strconv.FormatInt(offers[0].ID, 10)

	// Bob merges via the HTTP route.
	rec = s.PostHX(t, s.Bob, "/flash/shared/adopt", url.Values{"share_id": {mergeShareID}})
	if rec.Code != http.StatusOK {
		t.Fatalf("merge: %d; body: %s", rec.Code, rec.Body.String())
	}

	bobDecks, err = s.Store.ListDecks(t.Context(), s.Bob.User.ID)
	if err != nil || len(bobDecks) != 1 || bobDecks[0].ID != bobDeckID {
		t.Fatalf("bob's decks after merge = %+v, err = %v, want still just deck %d", bobDecks, err, bobDeckID)
	}
	bobCards, err = s.Store.ListCards(t.Context(), s.Bob.User.ID, bobDeckID)
	if err != nil {
		t.Fatal(err)
	}
	if len(bobCards) != 2 {
		t.Fatalf("bob's cards after merge = %+v, want 2", bobCards)
	}
}
