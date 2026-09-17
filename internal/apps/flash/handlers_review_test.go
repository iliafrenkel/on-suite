// internal/apps/flash/handlers_review_test.go
package flash_test

import (
	"net/url"
	"testing"

	"github.com/iliafrenkel/on-suite/internal/apps/flash"
	"github.com/iliafrenkel/on-suite/internal/htmlassert"
)

func TestReviewShowsACardAcrossAllDecks(t *testing.T) {
	s := newServer(t)
	deck, err := s.Store.CreateDeck(t.Context(), s.Alice.User.ID, "Spanish", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Store.CreateCard(t.Context(), s.Alice.User.ID, deck.ID, flash.CardTypeBasic, "hola", "hello", ""); err != nil {
		t.Fatal(err)
	}

	doc := s.Get(t, s.Alice, "/flash/review")
	doc.MustHave(".flash-review-front")
}

func TestReviewShowsNothingDueWhenQueueIsEmpty(t *testing.T) {
	s := newServer(t)
	doc := s.Get(t, s.Alice, "/flash/review")
	doc.MustNotHave(".flash-review-front")
}

func TestReviewScopedToOneDeck(t *testing.T) {
	s := newServer(t)
	deckA, err := s.Store.CreateDeck(t.Context(), s.Alice.User.ID, "A", "")
	if err != nil {
		t.Fatal(err)
	}
	deckB, err := s.Store.CreateDeck(t.Context(), s.Alice.User.ID, "B", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Store.CreateCard(t.Context(), s.Alice.User.ID, deckB.ID, flash.CardTypeBasic, "b1", "x", ""); err != nil {
		t.Fatal(err)
	}

	// deckA has no cards, so its own review page shows nothing due even
	// though deckB (a different deck) has one.
	doc := s.Get(t, s.Alice, "/flash/review/"+itoa(deckA.ID))
	doc.MustNotHave(".flash-review-front")
}

func TestGradingACardAdvancesTheQueue(t *testing.T) {
	s := newServer(t)
	deck, err := s.Store.CreateDeck(t.Context(), s.Alice.User.ID, "Spanish", "")
	if err != nil {
		t.Fatal(err)
	}
	card, err := s.Store.CreateCard(t.Context(), s.Alice.User.ID, deck.ID, flash.CardTypeBasic, "hola", "hello", "")
	if err != nil {
		t.Fatal(err)
	}

	rec := s.PostHX(t, s.Alice, "/flash/review/grade", url.Values{"card_id": {itoa(card.ID)}, "rating": {"3"}})
	if rec.Code != 200 {
		t.Fatalf("grade = %d, want 200", rec.Code)
	}
	doc := htmlassert.Parse(t, rec.Body.String())
	doc.MustNotHave(".flash-review-front") // the only card was just graded; nothing left due
	doc.MustHave(".flash-undo-btn")
}

func TestGradingRequiresCSRF(t *testing.T) {
	s := newServer(t)
	deck, err := s.Store.CreateDeck(t.Context(), s.Alice.User.ID, "Spanish", "")
	if err != nil {
		t.Fatal(err)
	}
	card, err := s.Store.CreateCard(t.Context(), s.Alice.User.ID, deck.ID, flash.CardTypeBasic, "hola", "hello", "")
	if err != nil {
		t.Fatal(err)
	}
	req := httpPost(t, "/flash/review/grade", url.Values{"card_id": {itoa(card.ID)}, "rating": {"3"}})
	rec := s.Do(t, s.Alice, req)
	if rec.Code != 403 {
		t.Errorf("grade without CSRF = %d, want 403", rec.Code)
	}
}

func TestGradingRejectsAnInvalidRating(t *testing.T) {
	s := newServer(t)
	deck, err := s.Store.CreateDeck(t.Context(), s.Alice.User.ID, "Spanish", "")
	if err != nil {
		t.Fatal(err)
	}
	card, err := s.Store.CreateCard(t.Context(), s.Alice.User.ID, deck.ID, flash.CardTypeBasic, "hola", "hello", "")
	if err != nil {
		t.Fatal(err)
	}
	rec := s.PostHX(t, s.Alice, "/flash/review/grade", url.Values{"card_id": {itoa(card.ID)}, "rating": {"7"}})
	if rec.Code != 400 {
		t.Errorf("grade with rating=7 = %d, want 400", rec.Code)
	}
}

func TestGradingSomeoneElsesCardIs404(t *testing.T) {
	s := newServer(t)
	deck, err := s.Store.CreateDeck(t.Context(), s.Alice.User.ID, "alice's", "")
	if err != nil {
		t.Fatal(err)
	}
	card, err := s.Store.CreateCard(t.Context(), s.Alice.User.ID, deck.ID, flash.CardTypeBasic, "a", "b", "")
	if err != nil {
		t.Fatal(err)
	}
	rec := s.PostHX(t, s.Bob, "/flash/review/grade", url.Values{"card_id": {itoa(card.ID)}, "rating": {"3"}})
	if rec.Code != 404 {
		t.Errorf("grading someone else's card = %d, want 404", rec.Code)
	}
}

func TestUndoAfterGradingReshowsTheCard(t *testing.T) {
	s := newServer(t)
	deck, err := s.Store.CreateDeck(t.Context(), s.Alice.User.ID, "Spanish", "")
	if err != nil {
		t.Fatal(err)
	}
	card, err := s.Store.CreateCard(t.Context(), s.Alice.User.ID, deck.ID, flash.CardTypeBasic, "hola", "hello", "")
	if err != nil {
		t.Fatal(err)
	}
	s.PostHX(t, s.Alice, "/flash/review/grade", url.Values{"card_id": {itoa(card.ID)}, "rating": {"3"}})

	rec := s.PostHX(t, s.Alice, "/flash/review/undo", url.Values{"card_id": {itoa(card.ID)}})
	if rec.Code != 200 {
		t.Fatalf("undo = %d, want 200", rec.Code)
	}
	doc := htmlassert.Parse(t, rec.Body.String())
	doc.MustHave(".flash-review-front")
	doc.MustNotHave(".flash-undo-btn") // the undo slot was just spent
}

func TestUndoingTwiceIsRejected(t *testing.T) {
	s := newServer(t)
	deck, err := s.Store.CreateDeck(t.Context(), s.Alice.User.ID, "Spanish", "")
	if err != nil {
		t.Fatal(err)
	}
	card, err := s.Store.CreateCard(t.Context(), s.Alice.User.ID, deck.ID, flash.CardTypeBasic, "hola", "hello", "")
	if err != nil {
		t.Fatal(err)
	}
	s.PostHX(t, s.Alice, "/flash/review/grade", url.Values{"card_id": {itoa(card.ID)}, "rating": {"3"}})
	s.PostHX(t, s.Alice, "/flash/review/undo", url.Values{"card_id": {itoa(card.ID)}})

	rec := s.PostHX(t, s.Alice, "/flash/review/undo", url.Values{"card_id": {itoa(card.ID)}})
	if rec.Code != 400 {
		t.Errorf("second undo = %d, want 400", rec.Code)
	}
}

func TestReviewScriptIsServed(t *testing.T) {
	s := newServer(t)
	rec := s.Do(t, s.Alice, httpGet(t, "/flash/flash-review.js"))
	if rec.Code != 200 {
		t.Fatalf("GET /flash/flash-review.js = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "text/javascript; charset=utf-8" {
		t.Errorf("Content-Type = %q", ct)
	}
}
