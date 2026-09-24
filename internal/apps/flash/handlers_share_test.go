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

// TestShareDeckHandlerRejectsUnknownRecipient covers #304 bullet 3: a
// tampered form naming a to_user_id that isn't a real account must 400
// rather than creating an offer to nobody.
func TestShareDeckHandlerRejectsUnknownRecipient(t *testing.T) {
	s := newShareServer(t)
	deckID := createDeckHX(t, s, s.Alice, "Spanish")

	rec := s.PostHX(t, s.Alice, "/flash/"+strconv.FormatInt(deckID, 10)+"/share",
		url.Values{"to_user_id": {"999999"}})
	if rec.Code != 400 {
		t.Fatalf("share with unknown to_user_id: %d, want 400; body: %s", rec.Code, rec.Body.String())
	}

	shares, err := s.Store.SharesForDeck(t.Context(), s.Alice.User.ID, deckID)
	if err != nil || len(shares) != 0 {
		t.Fatalf("shares after rejected share = %+v, err = %v, want none", shares, err)
	}
}

// TestShareDeckHandlerNoJSRedirects covers #342: a no-JS POST must 303
// redirect back to the deck rather than returning a bare HTML fragment.
func TestShareDeckHandlerNoJSRedirects(t *testing.T) {
	s := newShareServer(t)
	deckID := createDeckHX(t, s, s.Alice, "Spanish")
	deckIDStr := strconv.FormatInt(deckID, 10)

	s.Submit(t, s.Alice, "/flash/"+deckIDStr+"/share",
		url.Values{"to_user_id": {strconv.FormatInt(s.Bob.User.ID, 10)}}, "/flash/"+deckIDStr)
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

// TestRevokeShareHandlerRequiresMatchingDeckID covers #304 bullet 1: the
// {deckID} in the revoke route isn't decorative — a mismatched (but still
// alice-owned) deckID must 404 rather than revoking a share that actually
// belongs to a different deck.
func TestRevokeShareHandlerRequiresMatchingDeckID(t *testing.T) {
	s := newShareServer(t)
	deckID := createDeckHX(t, s, s.Alice, "Spanish")
	otherDeckID := createDeckHX(t, s, s.Alice, "French")
	deckIDStr := strconv.FormatInt(deckID, 10)
	otherDeckIDStr := strconv.FormatInt(otherDeckID, 10)
	shareIDStr := shareToBob(t, s, deckID)

	rec := s.PostHX(t, s.Alice, "/flash/"+otherDeckIDStr+"/share/"+shareIDStr+"/revoke", url.Values{})
	if rec.Code != 404 {
		t.Fatalf("revoke with mismatched deckID: %d, want 404; body: %s", rec.Code, rec.Body.String())
	}

	offers, err := s.Store.SharesForRecipient(t.Context(), s.Bob.User.ID)
	if err != nil || len(offers) != 1 || offers[0].Status != flash.ShareStatusPending {
		t.Fatalf("offers after mismatched revoke = %+v, err = %v, want the share still pending", offers, err)
	}

	// The real deckID still revokes it.
	rec = s.PostHX(t, s.Alice, "/flash/"+deckIDStr+"/share/"+shareIDStr+"/revoke", url.Values{})
	if rec.Code != 200 {
		t.Fatalf("revoke with correct deckID: %d; body: %s", rec.Code, rec.Body.String())
	}
}

// TestRevokeShareHandlerNoJSRedirects covers #342 for revoke.
func TestRevokeShareHandlerNoJSRedirects(t *testing.T) {
	s := newShareServer(t)
	deckID := createDeckHX(t, s, s.Alice, "Spanish")
	deckIDStr := strconv.FormatInt(deckID, 10)
	shareIDStr := shareToBob(t, s, deckID)

	s.Submit(t, s.Alice, "/flash/"+deckIDStr+"/share/"+shareIDStr+"/revoke", url.Values{}, "/flash/"+deckIDStr)
}

// TestDeclineShareHandlerNoJSRedirects covers #342 for decline.
func TestDeclineShareHandlerNoJSRedirects(t *testing.T) {
	s := newShareServer(t)
	deckID := createDeckHX(t, s, s.Alice, "Spanish")
	shareIDStr := shareToBob(t, s, deckID)

	s.Submit(t, s.Bob, "/flash/shared/decline", url.Values{"share_id": {shareIDStr}}, "/flash/")
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
	if got := rec.Header().Get("HX-Push-Url"); got != "/flash/" {
		t.Errorf("decline HX-Push-Url = %q, want /flash/ so the URL doesn't dead-end on the gone share", got)
	}
	offers, err = s.Store.SharesForRecipient(t.Context(), s.Bob.User.ID)
	if err != nil || len(offers) != 0 {
		t.Fatalf("offers after decline = %+v, want none", offers)
	}
}

// shareToBob shares one of alice's decks with bob and returns the share id.
func shareToBob(t *testing.T, s *apptest.Server[*flash.Store], deckID int64) string {
	t.Helper()
	s.PostHX(t, s.Alice, "/flash/"+strconv.FormatInt(deckID, 10)+"/share",
		url.Values{"to_user_id": {strconv.FormatInt(s.Bob.User.ID, 10)}})
	offers, err := s.Store.SharesForRecipient(t.Context(), s.Bob.User.ID)
	if err != nil || len(offers) == 0 {
		t.Fatalf("setup: offers = %+v, err = %v", offers, err)
	}
	return strconv.FormatInt(offers[0].ID, 10)
}

func TestGiftRowShowsSharerAndDeck(t *testing.T) {
	s := newShareServer(t)
	deckID := createDeckHX(t, s, s.Alice, "Spanish")
	shareID := shareToBob(t, s, deckID)

	doc := s.Get(t, s.Bob, "/flash/")
	gift := doc.MustHave("#deck-list a.deck-row-gift")
	text := htmlassert.Text(gift)
	if !strings.Contains(text, "Spanish") || !strings.Contains(text, "From alice") {
		t.Errorf("gift row = %q, want the deck name and From alice", text)
	}
	if href, _ := htmlassert.Attr(gift, "href"); href != "/flash/shared/"+shareID {
		t.Errorf("gift row href = %q", href)
	}
	doc.MustNotHave("#shared-with-me")
	doc.MustNotHave(".flash-welcome") // a pending gift is not a first run
}

func TestGiftPreviewPane(t *testing.T) {
	s := newShareServer(t)
	deckID := createDeckHX(t, s, s.Alice, "Spanish")
	for _, front := range []string{"hola", "adiós"} {
		s.PostHX(t, s.Alice, "/flash/"+strconv.FormatInt(deckID, 10)+"/cards/new",
			url.Values{"card_type": {"basic"}, "front": {front}, "back": {"x"}})
	}
	shareID := shareToBob(t, s, deckID)

	doc := s.Get(t, s.Bob, "/flash/shared/"+shareID)
	pane := doc.MustHave("#deck-detail .flash-gift")
	text := htmlassert.Text(pane)
	if !strings.Contains(text, "alice shared this deck with you") || !strings.Contains(text, "2 cards") {
		t.Errorf("gift pane = %q", text)
	}
	if n := len(doc.QueryAll(".flash-gift .flash-mini-card")); n != 2 {
		t.Errorf("gift pane shows %d sample cards, want 2", n)
	}
	for _, sel := range []string{
		`form[action="/flash/shared/adopt"] input[name=csrf_token]`,
		`form[action="/flash/shared/decline"] input[name=csrf_token]`,
	} {
		v, _ := htmlassert.Attr(doc.MustHave(sel), "value")
		if v == "" {
			t.Errorf("%s has an empty CSRF token", sel)
		}
	}
	doc.MustHave(`#deck-list a.deck-row-active`) // the gift row is highlighted
}

func TestGiftPreviewIsRecipientOnly(t *testing.T) {
	s := newShareServer(t)
	deckID := createDeckHX(t, s, s.Alice, "Spanish")
	shareID := shareToBob(t, s, deckID)

	if rec := s.Do(t, s.Alice, httpGet(t, "/flash/shared/"+shareID)); rec.Code != 404 {
		t.Errorf("sharer viewing the gift = %d, want 404", rec.Code)
	}
	s.PostHX(t, s.Bob, "/flash/shared/decline", url.Values{"share_id": {shareID}})
	if rec := s.Do(t, s.Bob, httpGet(t, "/flash/shared/"+shareID)); rec.Code != 404 {
		t.Errorf("gift after declining = %d, want 404", rec.Code)
	}
}

func TestGiftRowLeavesTheListOnAdoptAndDecline(t *testing.T) {
	s := newShareServer(t)
	deckID := createDeckHX(t, s, s.Alice, "Spanish")
	shareID := shareToBob(t, s, deckID)

	rec := s.PostHX(t, s.Bob, "/flash/shared/decline", url.Values{"share_id": {shareID}})
	doc := htmlassert.Parse(t, rec.Body.String())
	list := doc.MustHave("#deck-list")
	if _, ok := htmlassert.Attr(list, "hx-swap-oob"); !ok {
		t.Error("#deck-list is not refreshed out of band on decline")
	}
	doc.MustNotHave("#deck-list .deck-row-gift")

	shareID = shareToBob(t, s, deckID)
	rec = s.PostHX(t, s.Bob, "/flash/shared/adopt", url.Values{"share_id": {shareID}})
	doc = htmlassert.Parse(t, rec.Body.String())
	doc.MustNotHave("#deck-list .deck-row-gift")
	notice := doc.MustHave("#deck-detail-view .flash-notice")
	if !strings.Contains(htmlassert.Text(notice), "Spanish") {
		t.Errorf("adopt notice = %q, want the deck name", htmlassert.Text(notice))
	}
}

// TestDeclineOneOfTwoGiftsLeavesTheOtherInTheOOBList covers #304 bullet 8:
// the out-of-band #deck-list refresh after declining one gift must still
// show every other still-pending gift, not just drop the whole gift section.
func TestDeclineOneOfTwoGiftsLeavesTheOtherInTheOOBList(t *testing.T) {
	s := newShareServer(t)
	spanishID := createDeckHX(t, s, s.Alice, "Spanish")
	frenchID := createDeckHX(t, s, s.Alice, "French")
	spanishShareID := shareToBob(t, s, spanishID)
	frenchShareID := shareToBob(t, s, frenchID)

	rec := s.PostHX(t, s.Bob, "/flash/shared/decline", url.Values{"share_id": {spanishShareID}})
	if rec.Code != 200 {
		t.Fatalf("decline: %d; body: %s", rec.Code, rec.Body.String())
	}
	doc := htmlassert.Parse(t, rec.Body.String())
	list := doc.MustHave("#deck-list")
	if _, ok := htmlassert.Attr(list, "hx-swap-oob"); !ok {
		t.Error("#deck-list is not refreshed out of band on decline")
	}

	gifts := doc.QueryAll("#deck-list a.deck-row-gift")
	if len(gifts) != 1 {
		t.Fatalf("gift rows after decline = %d, want exactly 1 (French)", len(gifts))
	}
	text := htmlassert.Text(gifts[0])
	if !strings.Contains(text, "French") {
		t.Errorf("remaining gift row = %q, want French", text)
	}
	if strings.Contains(text, "Spanish") {
		t.Errorf("remaining gift row = %q, should not mention the declined Spanish deck", text)
	}
	if href, _ := htmlassert.Attr(gifts[0], "href"); href != "/flash/shared/"+frenchShareID {
		t.Errorf("remaining gift row href = %q, want the French share %q", href, "/flash/shared/"+frenchShareID)
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
	giftText := htmlassert.Text(doc.MustHave("#deck-list a.deck-row-gift"))
	if !strings.Contains(giftText, "alice") || !strings.Contains(giftText, "Spanish") {
		t.Fatalf("gift row text = %q, want alice's username and the deck name", giftText)
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

func TestShareMenuListsSharesWithFriendlyStatus(t *testing.T) {
	s := newShareServer(t)
	deckID := createDeckHX(t, s, s.Alice, "Spanish")
	rec := s.PostHX(t, s.Alice, "/flash/"+strconv.FormatInt(deckID, 10)+"/share",
		url.Values{"to_user_id": {strconv.FormatInt(s.Bob.User.ID, 10)}})
	doc := htmlassert.Parse(t, rec.Body.String())
	menu := doc.MustHave("details.flash-share-menu")
	if _, open := htmlassert.Attr(menu, "open"); !open {
		t.Error("the Share popover should stay open after sharing")
	}
	pill := doc.MustHave(".flash-share-menu .flash-status-pill")
	if got := htmlassert.Text(pill); got != "waiting" {
		t.Errorf("status pill = %q, want waiting", got)
	}

	page := s.Get(t, s.Alice, "/flash/"+strconv.FormatInt(deckID, 10))
	closed := page.MustHave("details.flash-share-menu")
	if _, open := htmlassert.Attr(closed, "open"); open {
		t.Error("the Share popover should start closed on a normal page load")
	}
}

func TestCardsModeToolbarAlsoHasShare(t *testing.T) {
	s := newShareServer(t)
	deckID := createDeckHX(t, s, s.Alice, "Spanish")
	doc := s.Get(t, s.Alice, "/flash/"+strconv.FormatInt(deckID, 10)+"/cards/")
	doc.MustHave(".flash-deck-toolbar details.flash-share-menu")
}

// TestShareAndRevokeFromCardsModeSyncURL covers finding #2 from the U5
// final review: sharing or revoking from Cards mode always renders the
// deck-view pane, so HX-Push-Url must redirect the URL there too, even
// though the request came from /flash/{id}/cards/.
func TestShareAndRevokeFromCardsModeSyncURL(t *testing.T) {
	s := newShareServer(t)
	deckID := createDeckHX(t, s, s.Alice, "Spanish")
	deckIDStr := strconv.FormatInt(deckID, 10)
	wantURL := "/flash/" + deckIDStr

	s.Get(t, s.Alice, "/flash/"+deckIDStr+"/cards/") // land in Cards mode first

	rec := s.PostHX(t, s.Alice, "/flash/"+deckIDStr+"/share",
		url.Values{"to_user_id": {strconv.FormatInt(s.Bob.User.ID, 10)}})
	if rec.Code != 200 {
		t.Fatalf("share: %d; body: %s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("HX-Push-Url"); got != wantURL {
		t.Errorf("share HX-Push-Url = %q, want %q to match the deck-view pane it renders", got, wantURL)
	}

	offers, err := s.Store.SharesForRecipient(t.Context(), s.Bob.User.ID)
	if err != nil || len(offers) != 1 {
		t.Fatalf("setup: offers = %+v, err = %v", offers, err)
	}
	shareIDStr := strconv.FormatInt(offers[0].ID, 10)

	rec = s.PostHX(t, s.Alice, "/flash/"+deckIDStr+"/share/"+shareIDStr+"/revoke", url.Values{})
	if rec.Code != 200 {
		t.Fatalf("revoke: %d; body: %s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("HX-Push-Url"); got != wantURL {
		t.Errorf("revoke HX-Push-Url = %q, want %q to match the deck-view pane it renders", got, wantURL)
	}
}
