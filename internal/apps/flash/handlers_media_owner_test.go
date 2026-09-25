// internal/apps/flash/handlers_media_owner_test.go
package flash_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/iliafrenkel/on-suite/internal/apps/flash"
	"github.com/iliafrenkel/on-suite/internal/apptest"
	"github.com/iliafrenkel/on-suite/internal/htmlassert"
)

// aliceCardWithImage creates a deck and a card of Alice's with onePNG
// attached as the card's image, and returns them with the media path.
func aliceCardWithImage(t *testing.T, s *apptest.Server[*flash.Store]) (flash.Deck, flash.Card, string) {
	t.Helper()
	ctx := t.Context()
	deck, err := s.Store.CreateDeck(ctx, s.Alice.User.ID, "Animals", "", flash.DefaultDeckColor)
	if err != nil {
		t.Fatal(err)
	}
	c, err := s.Store.CreateCard(ctx, s.Alice.User.ID, deck.ID, flash.CardTypeBasic, "cat", "gato", "")
	if err != nil {
		t.Fatal(err)
	}
	hash, err := s.Store.AttachCardUpload(ctx, s.Alice.User.ID, deck.ID, c.ID, flash.MediaKindImage, "image/png", onePNG)
	if err != nil {
		t.Fatal(err)
	}
	return deck, c, "/flash/media/" + hash
}

func getMedia(t *testing.T, s *apptest.Server[*flash.Store], sess *apptest.Session, path string) *httptest.ResponseRecorder {
	t.Helper()
	return s.Do(t, sess, httptest.NewRequest(http.MethodGet, path, nil))
}

// TestMediaIsServedOnlyToSomeoneWhoseCardUsesIt covers #302.4. Another
// account's file must look exactly like a hash that doesn't exist, so the
// route can't be used to learn what someone else has attached.
func TestMediaIsServedOnlyToSomeoneWhoseCardUsesIt(t *testing.T) {
	s := newServer(t)
	_, _, path := aliceCardWithImage(t, s)

	if rec := getMedia(t, s, s.Alice, path); rec.Code != http.StatusOK {
		t.Fatalf("alice, whose card uses it = %d, want 200", rec.Code)
	}

	notYours := getMedia(t, s, s.Bob, path)
	if notYours.Code != http.StatusNotFound {
		t.Fatalf("bob, who has no card using it = %d, want 404", notYours.Code)
	}
	missing := getMedia(t, s, s.Bob, "/flash/media/"+strings.Repeat("0", 64))
	if missing.Code != http.StatusNotFound {
		t.Fatalf("unknown hash = %d, want 404", missing.Code)
	}
	if notYours.Body.String() != missing.Body.String() {
		t.Error("someone else's media must get the same 404 body as a hash that doesn't exist")
	}
	for _, h := range []string{"ETag", "Cache-Control", "X-Content-Type-Options", "Content-Type", "Content-Length"} {
		if got := notYours.Header().Get(h); got != missing.Header().Get(h) {
			t.Errorf("header %s = %q for someone else's media, %q for a missing hash", h, got, missing.Header().Get(h))
		}
	}
}

// TestMediaConditionalRequestFromNonOwnerIsNotFound covers #302.4's
// conditional-GET corner: bob learning alice's ETag (say, from a shared
// screen) and replaying it in If-None-Match must not turn the 404 he'd
// otherwise get into a 304 — a 304 would confirm the hash exists and is
// alice's, exactly the leak the plain 404 is meant to prevent.
func TestMediaConditionalRequestFromNonOwnerIsNotFound(t *testing.T) {
	s := newServer(t)
	_, _, path := aliceCardWithImage(t, s)

	ownReq := httptest.NewRequest(http.MethodGet, path, nil)
	etag := s.Do(t, s.Alice, ownReq).Header().Get("ETag")
	if etag == "" {
		t.Fatal("alice's own GET returned no ETag")
	}

	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.Header.Set("If-None-Match", etag)
	rec := s.Do(t, s.Bob, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("bob, If-None-Match: alice's etag = %d, want 404", rec.Code)
	}
}

// TestMediaNoCardUsesIsNotServedEvenToItsUploader: a row waiting for the
// daily purge (its card was deleted) is nobody's any more.
func TestMediaNoCardUsesIsNotServedEvenToItsUploader(t *testing.T) {
	s := newServer(t)
	hash, err := s.Store.SaveMediaUpload(t.Context(), flash.MediaKindImage, "image/png", onePNG, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if rec := getMedia(t, s, s.Alice, "/flash/media/"+hash); rec.Code != http.StatusNotFound {
		t.Errorf("media no card uses = %d, want 404", rec.Code)
	}
}

// TestAdoptedDeckMediaIsServedToTheRecipient: a gift isn't Bob's until he
// adopts it. Adoption copies Alice's hashes into Bob's own cards, so from
// then on the file is his too, and stays his after Alice deletes her card.
func TestAdoptedDeckMediaIsServedToTheRecipient(t *testing.T) {
	s := newShareServer(t)
	deck, card, path := aliceCardWithImage(t, s)
	shareID := shareToBob(t, s, deck.ID)

	// The pending gift's preview draws no media, so nothing legitimate
	// asks for the file before adoption.
	preview := s.Do(t, s.Bob, httptest.NewRequest(http.MethodGet, "/flash/shared/"+shareID, nil))
	if preview.Code != http.StatusOK {
		t.Fatalf("gift preview = %d", preview.Code)
	}
	if strings.Contains(preview.Body.String(), "/flash/media/") {
		t.Error("the gift preview links to media Bob can't see yet")
	}
	if rec := getMedia(t, s, s.Bob, path); rec.Code != http.StatusNotFound {
		t.Fatalf("bob before adopting = %d, want 404", rec.Code)
	}

	if rec := s.PostHX(t, s.Bob, "/flash/shared/adopt", url.Values{"share_id": {shareID}}); rec.Code != http.StatusOK {
		t.Fatalf("adopt = %d; body: %s", rec.Code, rec.Body.String())
	}
	if rec := getMedia(t, s, s.Bob, path); rec.Code != http.StatusOK {
		t.Fatalf("bob after adopting = %d, want 200", rec.Code)
	}

	if err := s.Store.DeleteCard(t.Context(), s.Alice.User.ID, deck.ID, card.ID); err != nil {
		t.Fatal(err)
	}
	if rec := getMedia(t, s, s.Bob, path); rec.Code != http.StatusOK {
		t.Errorf("bob after alice deleted her card = %d, want 200 (his copy still uses it)", rec.Code)
	}
	if rec := getMedia(t, s, s.Alice, path); rec.Code != http.StatusNotFound {
		t.Errorf("alice after deleting her card = %d, want 404", rec.Code)
	}
}

// TestEveryMediaURLTheOwnerIsShownIsServed: the editor's preview of the
// saved image, the opened card and the review card all draw the same
// /flash/media/ URL, and it serves.
func TestEveryMediaURLTheOwnerIsShownIsServed(t *testing.T) {
	s := newServer(t)
	deck, err := s.Store.CreateDeck(t.Context(), s.Alice.User.ID, "Animals", "", flash.DefaultDeckColor)
	if err != nil {
		t.Fatal(err)
	}
	rec := postCardForm(t, s, s.Alice, "/flash/"+itoa(deck.ID)+"/cards/new",
		url.Values{"card_type": {"basic"}, "front": {"cat"}, "back": {"gato"}},
		map[string][]byte{"image": onePNG})
	if rec.Code != http.StatusCreated {
		t.Fatalf("create = %d; body: %s", rec.Code, rec.Body.String())
	}
	cards, err := s.Store.ListCards(t.Context(), s.Alice.User.ID, deck.ID)
	if err != nil || len(cards) != 1 {
		t.Fatalf("ListCards = %v, %v", cards, err)
	}
	c := cards[0]

	pages := []struct{ name, path, selector string }{
		{"editor preview", "/flash/" + itoa(deck.ID) + "/cards/edit/" + itoa(c.ID), "form#card-detail-edit img.flash-drop-preview"},
		{"opened card", "/flash/" + itoa(deck.ID) + "/cards/" + itoa(c.ID), "img.flash-card-image"},
		{"review card", "/flash/review/" + itoa(deck.ID), "#review-card img.flash-card-image"},
	}
	for _, p := range pages {
		doc := s.Get(t, s.Alice, p.path)
		src, ok := htmlassert.Attr(doc.MustHave(p.selector), "src")
		if !ok || !strings.HasPrefix(src, "/flash/media/") {
			t.Fatalf("%s: img src = %q", p.name, src)
		}
		got := getMedia(t, s, s.Alice, src)
		if got.Code != http.StatusOK || got.Header().Get("Content-Type") != "image/png" {
			t.Errorf("%s: GET %s = %d %q, want 200 image/png", p.name, src, got.Code, got.Header().Get("Content-Type"))
		}
	}
}
