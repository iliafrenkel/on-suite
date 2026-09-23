// internal/apps/flash/handlers_review_test.go
package flash_test

import (
	"net/url"
	"strings"
	"testing"
	"time"

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
	doc.MustHave(".flash-review-card")
}

func TestReviewShowsNothingDueWhenQueueIsEmpty(t *testing.T) {
	s := newServer(t)
	doc := s.Get(t, s.Alice, "/flash/review")
	doc.MustNotHave(".flash-review-card")
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
	doc.MustNotHave(".flash-review-card")
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
	doc.MustNotHave(".flash-review-card") // the only card was just graded; nothing left due
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
	doc.MustHave(".flash-review-card")
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

func TestGradingScopedToSomeoneElsesDeckIs404(t *testing.T) {
	s := newServer(t)
	// Bob's deck: valid, but not Alice's to scope a review against.
	bobDeck, err := s.Store.CreateDeck(t.Context(), s.Bob.User.ID, "bob's", "")
	if err != nil {
		t.Fatal(err)
	}
	// Alice's own card, which she is otherwise allowed to grade.
	deck, err := s.Store.CreateDeck(t.Context(), s.Alice.User.ID, "alice's", "")
	if err != nil {
		t.Fatal(err)
	}
	card, err := s.Store.CreateCard(t.Context(), s.Alice.User.ID, deck.ID, flash.CardTypeBasic, "a", "b", "")
	if err != nil {
		t.Fatal(err)
	}

	// The card grades fine (it's Alice's own), but re-rendering the queue
	// scoped to ?deck=<bob's deck> must 404, not 500, since that deck isn't
	// Alice's.
	rec := s.PostHX(t, s.Alice, "/flash/review/grade?deck="+itoa(bobDeck.ID),
		url.Values{"card_id": {itoa(card.ID)}, "rating": {"3"}})
	if rec.Code != 404 {
		t.Errorf("grade scoped to someone else's deck = %d, want 404", rec.Code)
	}
}

func TestReviewPageScopedToSomeoneElsesDeckIs404(t *testing.T) {
	s := newServer(t)
	bobDeck, err := s.Store.CreateDeck(t.Context(), s.Bob.User.ID, "bob's", "")
	if err != nil {
		t.Fatal(err)
	}
	rec := s.Do(t, s.Alice, httpGet(t, "/flash/review?deck="+itoa(bobDeck.ID)))
	if rec.Code != 404 {
		t.Errorf("GET /flash/review?deck=<someone else's> = %d, want 404", rec.Code)
	}
}

func TestReviewRespectsTheDailyNewCardLimit(t *testing.T) {
	s := newServer(t)
	now := time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC)
	s.Store.SetClock(func() time.Time { return now })

	deck, err := s.Store.CreateDeck(t.Context(), s.Alice.User.ID, "Spanish", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Store.UpdateDeckSettings(t.Context(), s.Alice.User.ID, deck.ID, 1, nil); err != nil {
		t.Fatal(err)
	}
	cardA, err := s.Store.CreateCard(t.Context(), s.Alice.User.ID, deck.ID, flash.CardTypeBasic, "a", "b", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Store.CreateCard(t.Context(), s.Alice.User.ID, deck.ID, flash.CardTypeBasic, "c", "d", ""); err != nil {
		t.Fatal(err)
	}

	rec := s.PostHX(t, s.Alice, "/flash/review/grade", url.Values{"card_id": {itoa(cardA.ID)}, "rating": {"3"}})
	if rec.Code != 200 {
		t.Fatalf("grade = %d, want 200", rec.Code)
	}

	// The deck's new_cards_per_day is 1 and one card was just graded "new",
	// so the second (never-reviewed) card must not show up today, even
	// though it's otherwise due.
	doc := s.Get(t, s.Alice, "/flash/review/"+itoa(deck.ID))
	doc.MustNotHave(".flash-review-card")
}

func TestFlashScriptIsServed(t *testing.T) {
	s := newServer(t)
	rec := s.Do(t, s.Alice, httpGet(t, "/flash/flash.js"))
	if rec.Code != 200 {
		t.Fatalf("GET /flash/flash.js = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "text/javascript; charset=utf-8" {
		t.Errorf("Content-Type = %q", ct)
	}
}

func TestReviewCardFlipsAndGradesWithFriendlyLabels(t *testing.T) {
	s := newServer(t)
	deck, err := s.Store.CreateDeck(t.Context(), s.Alice.User.ID, "Spanish", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Store.CreateCard(t.Context(), s.Alice.User.ID, deck.ID, flash.CardTypeBasic, "hola", "hello", ""); err != nil {
		t.Fatal(err)
	}
	doc := s.Get(t, s.Alice, "/flash/review/"+itoa(deck.ID))
	doc.MustHave("#review-card input#review-flip")
	show := doc.MustHave("#review-card .flash-show-answer")
	if f, _ := htmlassert.Attr(show, "for"); f != "review-flip" {
		t.Errorf("Show answer label for=%q, want review-flip", f)
	}
	var labels []string
	for _, b := range doc.QueryAll("#review-card .flash-grades button") {
		labels = append(labels, htmlassert.Text(b))
	}
	want := []string{"Forgot 1", "Hard 2", "Got it 3", "Easy 4"}
	if strings.Join(labels, "|") != strings.Join(want, "|") {
		t.Errorf("grade buttons = %v, want %v", labels, want)
	}
	if got := htmlassert.Text(doc.MustHave("#review-card .flash-card-corner")); got != "new" {
		t.Errorf("corner = %q, want new", got)
	}
	doc.MustNotHave("#review-card .flash-tag-links")
	stop := doc.MustHave("a.flash-review-stop")
	if href, _ := htmlassert.Attr(stop, "href"); href != "/flash/"+itoa(deck.ID) {
		t.Errorf("Stop href = %q, want the deck", href)
	}
}

func TestReviewShowsProgress(t *testing.T) {
	s := newServer(t)
	deck, err := s.Store.CreateDeck(t.Context(), s.Alice.User.ID, "Spanish", "")
	if err != nil {
		t.Fatal(err)
	}
	var first flash.Card
	for i, front := range []string{"uno", "dos", "tres"} {
		c, err := s.Store.CreateCard(t.Context(), s.Alice.User.ID, deck.ID, flash.CardTypeBasic, front, "x", "")
		if err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			first = c
		}
	}
	doc := s.Get(t, s.Alice, "/flash/review/"+itoa(deck.ID))
	if got := htmlassert.Text(doc.MustHave(".flash-review-count")); got != "1 of 3" {
		t.Errorf("count before grading = %q, want 1 of 3", got)
	}

	// Got it on a new card schedules it minutes away, so it leaves today's
	// queue for now.
	rec := s.PostHX(t, s.Alice, "/flash/review/grade?deck="+itoa(deck.ID), url.Values{"card_id": {itoa(first.ID)}, "rating": {"3"}})
	if rec.Code != 200 {
		t.Fatalf("grade = %d", rec.Code)
	}
	after := htmlassert.Parse(t, rec.Body.String())
	if got := htmlassert.Text(after.MustHave(".flash-review-count")); got != "2 of 3" {
		t.Errorf("count after one grade = %q, want 2 of 3", got)
	}
	fill := after.MustHave(".flash-progress rect.fill")
	if w, _ := htmlassert.Attr(fill, "width"); w != "33" {
		t.Errorf("progress width = %q, want 33", w)
	}
}

func TestReviewSummaryAfterTheLastCard(t *testing.T) {
	s := newServer(t)
	deck, err := s.Store.CreateDeck(t.Context(), s.Alice.User.ID, "Spanish", "")
	if err != nil {
		t.Fatal(err)
	}
	c, err := s.Store.CreateCard(t.Context(), s.Alice.User.ID, deck.ID, flash.CardTypeBasic, "hola", "hello", "")
	if err != nil {
		t.Fatal(err)
	}
	rec := s.PostHX(t, s.Alice, "/flash/review/grade?deck="+itoa(deck.ID), url.Values{"card_id": {itoa(c.ID)}, "rating": {"3"}})
	doc := htmlassert.Parse(t, rec.Body.String())
	summary := doc.MustHave(".flash-review-summary")
	if text := htmlassert.Text(summary); !strings.Contains(text, "You reviewed 1 card today") {
		t.Errorf("summary = %q", text)
	}
	if got := htmlassert.Text(doc.MustHave(".flash-breakdown-legend")); got != "1 got it" {
		t.Errorf("legend = %q", got)
	}
	doc.MustHave(".flash-celebrate")
	back := doc.MustHave("a.flash-back-to-decks")
	if href, _ := htmlassert.Attr(back, "href"); href != "/flash/" {
		t.Errorf("Back to decks href = %q", href)
	}
	doc.MustNotHave(".flash-streak") // a 1-day streak isn't mentioned
	doc.MustHave(".flash-undo-btn")  // the last grade can still be undone
}

func TestReviewSummarySuggestsTheNextDeck(t *testing.T) {
	s := newServer(t)
	a, err := s.Store.CreateDeck(t.Context(), s.Alice.User.ID, "Alpha", "")
	if err != nil {
		t.Fatal(err)
	}
	b, err := s.Store.CreateDeck(t.Context(), s.Alice.User.ID, "Beta", "")
	if err != nil {
		t.Fatal(err)
	}
	ca, err := s.Store.CreateCard(t.Context(), s.Alice.User.ID, a.ID, flash.CardTypeBasic, "a", "x", "")
	if err != nil {
		t.Fatal(err)
	}
	for _, front := range []string{"b1", "b2"} {
		if _, err := s.Store.CreateCard(t.Context(), s.Alice.User.ID, b.ID, flash.CardTypeBasic, front, "x", ""); err != nil {
			t.Fatal(err)
		}
	}
	rec := s.PostHX(t, s.Alice, "/flash/review/grade?deck="+itoa(a.ID), url.Values{"card_id": {itoa(ca.ID)}, "rating": {"3"}})
	doc := htmlassert.Parse(t, rec.Body.String())
	next := doc.MustHave("a.flash-next-deck")
	if href, _ := htmlassert.Attr(next, "href"); href != "/flash/review/"+itoa(b.ID) {
		t.Errorf("next deck href = %q", href)
	}
	if text := htmlassert.Text(next); !strings.Contains(text, "Beta") || !strings.Contains(text, "2 due") {
		t.Errorf("next deck text = %q", text)
	}
}

func TestReviewSummaryShowsAStreak(t *testing.T) {
	s := newServer(t)
	other, err := s.Store.CreateDeck(t.Context(), s.Alice.User.ID, "Yesterday", "")
	if err != nil {
		t.Fatal(err)
	}
	old, err := s.Store.CreateCard(t.Context(), s.Alice.User.ID, other.ID, flash.CardTypeBasic, "old", "x", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Store.GradeCard(t.Context(), s.Alice.User.ID, old.ID, flash.RatingEasy, time.Now().UTC().AddDate(0, 0, -1)); err != nil {
		t.Fatal(err)
	}
	deck, err := s.Store.CreateDeck(t.Context(), s.Alice.User.ID, "Today", "")
	if err != nil {
		t.Fatal(err)
	}
	c, err := s.Store.CreateCard(t.Context(), s.Alice.User.ID, deck.ID, flash.CardTypeBasic, "new", "x", "")
	if err != nil {
		t.Fatal(err)
	}
	rec := s.PostHX(t, s.Alice, "/flash/review/grade?deck="+itoa(deck.ID), url.Values{"card_id": {itoa(c.ID)}, "rating": {"3"}})
	doc := htmlassert.Parse(t, rec.Body.String())
	if got := htmlassert.Text(doc.MustHave(".flash-streak")); !strings.Contains(got, "2 days in a row") {
		t.Errorf("streak = %q, want 2 days in a row", got)
	}
}

func TestReviewWithNothingToDoHasNoCelebration(t *testing.T) {
	s := newServer(t)
	doc := s.Get(t, s.Alice, "/flash/review")
	summary := doc.MustHave(".flash-review-summary")
	if !strings.Contains(htmlassert.Text(summary), "Nothing to review right now") {
		t.Errorf("summary = %q", htmlassert.Text(summary))
	}
	doc.MustNotHave(".flash-celebrate")
	stop := doc.MustHave("a.flash-review-stop")
	if href, _ := htmlassert.Attr(stop, "href"); href != "/flash/" {
		t.Errorf("Stop href for Review all = %q, want /flash/", href)
	}
}
