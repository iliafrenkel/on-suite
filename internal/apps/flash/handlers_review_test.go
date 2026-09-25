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
	deck, err := s.Store.CreateDeck(t.Context(), s.Alice.User.ID, "Spanish", "", flash.DefaultDeckColor)
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
	deckA, err := s.Store.CreateDeck(t.Context(), s.Alice.User.ID, "A", "", flash.DefaultDeckColor)
	if err != nil {
		t.Fatal(err)
	}
	deckB, err := s.Store.CreateDeck(t.Context(), s.Alice.User.ID, "B", "", flash.DefaultDeckColor)
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
	deck, err := s.Store.CreateDeck(t.Context(), s.Alice.User.ID, "Spanish", "", flash.DefaultDeckColor)
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
	deck, err := s.Store.CreateDeck(t.Context(), s.Alice.User.ID, "Spanish", "", flash.DefaultDeckColor)
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
	deck, err := s.Store.CreateDeck(t.Context(), s.Alice.User.ID, "Spanish", "", flash.DefaultDeckColor)
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
	deck, err := s.Store.CreateDeck(t.Context(), s.Alice.User.ID, "alice's", "", flash.DefaultDeckColor)
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
	deck, err := s.Store.CreateDeck(t.Context(), s.Alice.User.ID, "Spanish", "", flash.DefaultDeckColor)
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
	deck, err := s.Store.CreateDeck(t.Context(), s.Alice.User.ID, "Spanish", "", flash.DefaultDeckColor)
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
	bobDeck, err := s.Store.CreateDeck(t.Context(), s.Bob.User.ID, "bob's", "", flash.DefaultDeckColor)
	if err != nil {
		t.Fatal(err)
	}
	// Alice's own card, which she is otherwise allowed to grade.
	deck, err := s.Store.CreateDeck(t.Context(), s.Alice.User.ID, "alice's", "", flash.DefaultDeckColor)
	if err != nil {
		t.Fatal(err)
	}
	card, err := s.Store.CreateCard(t.Context(), s.Alice.User.ID, deck.ID, flash.CardTypeBasic, "a", "b", "")
	if err != nil {
		t.Fatal(err)
	}

	// The card is Alice's own, but the ?deck= scope is Bob's deck: that must
	// 404 before anything is graded, not grade first and then fail to
	// re-render.
	rec := s.PostHX(t, s.Alice, "/flash/review/grade?deck="+itoa(bobDeck.ID),
		url.Values{"card_id": {itoa(card.ID)}, "rating": {"3"}})
	if rec.Code != 404 {
		t.Errorf("grade scoped to someone else's deck = %d, want 404", rec.Code)
	}
	if _, reviewed, err := s.Store.CardState(t.Context(), s.Alice.User.ID, card.ID); err != nil || reviewed {
		t.Errorf("after a 404 grade: reviewed = %v (err %v), want the card left ungraded", reviewed, err)
	}
}

func TestReviewPageScopedToSomeoneElsesDeckIs404(t *testing.T) {
	s := newServer(t)
	bobDeck, err := s.Store.CreateDeck(t.Context(), s.Bob.User.ID, "bob's", "", flash.DefaultDeckColor)
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

	deck, err := s.Store.CreateDeck(t.Context(), s.Alice.User.ID, "Spanish", "", flash.DefaultDeckColor)
	if err != nil {
		t.Fatal(err)
	}
	setDeckPace(t, s.Store, s.Alice.User.ID, deck.ID, 1, nil)
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

// TestReviewCardShowsTagsAsBackFacePills guards the Static side of #324's
// fix: the review screen has no deck pane for tag links to open in, so a
// tagged card's tags must still render as plain back-face pills there, and
// never as the clickable links the opened-card view uses instead.
func TestReviewCardShowsTagsAsBackFacePills(t *testing.T) {
	s := newServer(t)
	deck, err := s.Store.CreateDeck(t.Context(), s.Alice.User.ID, "Spanish", "", flash.DefaultDeckColor)
	if err != nil {
		t.Fatal(err)
	}
	c, err := s.Store.CreateCard(t.Context(), s.Alice.User.ID, deck.ID, flash.CardTypeBasic, "hola", "hello", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Store.SetCardTags(t.Context(), s.Alice.User.ID, c.ID, []string{"greetings"}); err != nil {
		t.Fatal(err)
	}

	doc := s.Get(t, s.Alice, "/flash/review/"+itoa(deck.ID))
	pill := doc.MustHave("#review-card .flash-card-back .flash-pill")
	if got := htmlassert.Text(pill); got != "greetings" {
		t.Errorf("back-face pill text = %q, want greetings", got)
	}
	doc.MustNotHave("#review-card .flash-tag-links")
}

func TestReviewCardFlipsAndGradesWithFriendlyLabels(t *testing.T) {
	s := newServer(t)
	deck, err := s.Store.CreateDeck(t.Context(), s.Alice.User.ID, "Spanish", "", flash.DefaultDeckColor)
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
	deck, err := s.Store.CreateDeck(t.Context(), s.Alice.User.ID, "Spanish", "", flash.DefaultDeckColor)
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
	deck, err := s.Store.CreateDeck(t.Context(), s.Alice.User.ID, "Spanish", "", flash.DefaultDeckColor)
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
	a, err := s.Store.CreateDeck(t.Context(), s.Alice.User.ID, "Alpha", "", flash.DefaultDeckColor)
	if err != nil {
		t.Fatal(err)
	}
	b, err := s.Store.CreateDeck(t.Context(), s.Alice.User.ID, "Beta", "", flash.DefaultDeckColor)
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
	other, err := s.Store.CreateDeck(t.Context(), s.Alice.User.ID, "Yesterday", "", flash.DefaultDeckColor)
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
	deck, err := s.Store.CreateDeck(t.Context(), s.Alice.User.ID, "Today", "", flash.DefaultDeckColor)
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

func TestReviewOfASnoozedDeckSaysItIsOnABreak(t *testing.T) {
	// s.Store.SetClock has no effect here: the app under test builds its own
	// Store via NewStore(deps.DB) (see flash.go's App.Mount), so this test
	// works off the real wall clock instead, like
	// TestSnoozeDaysMustBeBetweenOneAndAYear.
	s := newServer(t)
	now := time.Now().UTC()
	until := now.AddDate(0, 0, 7)
	deck, err := s.Store.CreateDeck(t.Context(), s.Alice.User.ID, "Spanish", "", flash.DefaultDeckColor)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Store.CreateCard(t.Context(), s.Alice.User.ID, deck.ID, flash.CardTypeBasic, "hola", "hello", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Store.SnoozeDeck(t.Context(), s.Alice.User.ID, deck.ID, until); err != nil {
		t.Fatal(err)
	}

	doc := s.Get(t, s.Alice, "/flash/review/"+itoa(deck.ID))
	brk := doc.MustHave(".flash-review-break")
	wantUntil := "Taking a break until " + until.Format("2 Jan") + "."
	if text := htmlassert.Text(brk); !strings.Contains(text, wantUntil) {
		t.Errorf("break panel = %q, want it to contain %q", text, wantUntil)
	}
	end := doc.MustHave(`.flash-review-break form[action="/flash/` + itoa(deck.ID) + `/unsnooze"]`)
	if got := htmlassert.Text(end); !strings.Contains(got, "End break") {
		t.Errorf("end-break form text = %q", got)
	}
	doc.MustNotHave(".flash-review-summary")
	doc.MustNotHave(".flash-review-card")

	// The review page also shows the break panel when the deck is scoped
	// via ?deck= instead of the /flash/review/{id} path.
	scoped := s.Get(t, s.Alice, "/flash/review?deck="+itoa(deck.ID))
	scoped.MustHave(".flash-review-break")

	// Review all is unaffected: a snoozed deck is simply left out there.
	all := s.Get(t, s.Alice, "/flash/review")
	all.MustNotHave(".flash-review-break")
	all.MustHave(".flash-review-summary")

	// Ending the break (no JS) lands on the deck pane; the card is back.
	s.Submit(t, s.Alice, "/flash/"+itoa(deck.ID)+"/unsnooze", url.Values{}, "/flash/"+itoa(deck.ID))
	back := s.Get(t, s.Alice, "/flash/review/"+itoa(deck.ID))
	back.MustHave(".flash-review-card")
	back.MustNotHave(".flash-review-break")
}

func TestReviewAllShowsTheMostOverdueCardFirst(t *testing.T) {
	// s.Store.SetClock has no effect here (see the note on
	// TestReviewOfASnoozedDeckSaysItIsOnABreak above), so this uses the real
	// wall clock and anchors every card relative to it.
	s := newServer(t)
	now := time.Now().UTC()
	older, err := s.Store.CreateDeck(t.Context(), s.Alice.User.ID, "Older", "", flash.DefaultDeckColor)
	if err != nil {
		t.Fatal(err)
	}
	newer, err := s.Store.CreateDeck(t.Context(), s.Alice.User.ID, "Newer", "", flash.DefaultDeckColor)
	if err != nil {
		t.Fatal(err)
	}
	overdue, err := s.Store.CreateCard(t.Context(), s.Alice.User.ID, older.ID, flash.CardTypeBasic, "overdue", "x", "")
	if err != nil {
		t.Fatal(err)
	}
	recent, err := s.Store.CreateCard(t.Context(), s.Alice.User.ID, newer.ID, flash.CardTypeBasic, "recent", "x", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Store.CreateCard(t.Context(), s.Alice.User.ID, newer.ID, flash.CardTypeBasic, "fresh", "x", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Store.GradeCard(t.Context(), s.Alice.User.ID, overdue.ID, flash.RatingAgain, now.AddDate(0, 0, -10)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Store.GradeCard(t.Context(), s.Alice.User.ID, recent.ID, flash.RatingAgain, now.AddDate(0, 0, -1)); err != nil {
		t.Fatal(err)
	}

	doc := s.Get(t, s.Alice, "/flash/review")
	if got := htmlassert.Text(doc.MustHave("#review-card .flash-card-front")); !strings.Contains(got, "overdue") {
		t.Errorf("first card front = %q, want the older deck's overdue card", got)
	}
	// Nothing graded today, three cards queued: M is still exact.
	if got := htmlassert.Text(doc.MustHave(".flash-review-count")); got != "1 of 3" {
		t.Errorf("count = %q, want 1 of 3", got)
	}
}

func TestGradingWithoutHTMXRedirectsBackToTheReview(t *testing.T) {
	s := newServer(t)
	deck, err := s.Store.CreateDeck(t.Context(), s.Alice.User.ID, "Spanish", "", flash.DefaultDeckColor)
	if err != nil {
		t.Fatal(err)
	}
	first, err := s.Store.CreateCard(t.Context(), s.Alice.User.ID, deck.ID, flash.CardTypeBasic, "uno", "one", "")
	if err != nil {
		t.Fatal(err)
	}
	second, err := s.Store.CreateCard(t.Context(), s.Alice.User.ID, deck.ID, flash.CardTypeBasic, "dos", "two", "")
	if err != nil {
		t.Fatal(err)
	}

	// Scoped to one deck: back to that deck's review page.
	s.Submit(t, s.Alice, "/flash/review/grade?deck="+itoa(deck.ID),
		url.Values{"card_id": {itoa(first.ID)}, "rating": {"3"}},
		"/flash/review/"+itoa(deck.ID)+"?undo="+itoa(first.ID))
	if _, reviewed, err := s.Store.CardState(t.Context(), s.Alice.User.ID, first.ID); err != nil || !reviewed {
		t.Fatalf("after a no-JS grade: reviewed = %v (err %v), want graded", reviewed, err)
	}

	// Review all: back to Review all.
	s.Submit(t, s.Alice, "/flash/review/grade",
		url.Values{"card_id": {itoa(second.ID)}, "rating": {"3"}},
		"/flash/review?undo="+itoa(second.ID))
}

func TestReviewOffersUndoFromTheRedirect(t *testing.T) {
	s := newServer(t)
	deck, err := s.Store.CreateDeck(t.Context(), s.Alice.User.ID, "Spanish", "", flash.DefaultDeckColor)
	if err != nil {
		t.Fatal(err)
	}
	card, err := s.Store.CreateCard(t.Context(), s.Alice.User.ID, deck.ID, flash.CardTypeBasic, "hola", "hello", "")
	if err != nil {
		t.Fatal(err)
	}
	s.Submit(t, s.Alice, "/flash/review/grade?deck="+itoa(deck.ID),
		url.Values{"card_id": {itoa(card.ID)}, "rating": {"3"}},
		"/flash/review/"+itoa(deck.ID)+"?undo="+itoa(card.ID))

	doc := s.Get(t, s.Alice, "/flash/review/"+itoa(deck.ID)+"?undo="+itoa(card.ID))
	doc.MustHave(".flash-undo-btn")
	id := doc.MustHave(".flash-undo input[name=card_id]")
	if v, _ := htmlassert.Attr(id, "value"); v != itoa(card.ID) {
		t.Errorf("undo card_id = %q, want %s", v, itoa(card.ID))
	}
	form := doc.MustHave(".flash-review form.flash-undo")
	if action, _ := htmlassert.Attr(form, "action"); action != "/flash/review/undo?deck="+itoa(deck.ID) {
		t.Errorf("undo action = %q, want it to keep the deck scope", action)
	}
}

func TestUndoWithoutHTMXRedirectsBackToTheReview(t *testing.T) {
	s := newServer(t)
	deck, err := s.Store.CreateDeck(t.Context(), s.Alice.User.ID, "Spanish", "", flash.DefaultDeckColor)
	if err != nil {
		t.Fatal(err)
	}
	card, err := s.Store.CreateCard(t.Context(), s.Alice.User.ID, deck.ID, flash.CardTypeBasic, "hola", "hello", "")
	if err != nil {
		t.Fatal(err)
	}
	s.PostHX(t, s.Alice, "/flash/review/grade?deck="+itoa(deck.ID), url.Values{"card_id": {itoa(card.ID)}, "rating": {"3"}})

	s.Submit(t, s.Alice, "/flash/review/undo?deck="+itoa(deck.ID),
		url.Values{"card_id": {itoa(card.ID)}}, "/flash/review/"+itoa(deck.ID))
	doc := s.Get(t, s.Alice, "/flash/review/"+itoa(deck.ID))
	doc.MustHave(".flash-review-card") // the card is back
	doc.MustNotHave(".flash-undo-btn") // and the undo slot is spent
}

func TestReviewIgnoresAMalformedUndoParam(t *testing.T) {
	s := newServer(t)
	for _, v := range []string{"abc", "0", "-3", ""} {
		doc := s.Get(t, s.Alice, "/flash/review?undo="+url.QueryEscape(v))
		doc.MustNotHave(".flash-undo-btn")
	}
}

// mustOneVisuallyHiddenH1 asserts the page has exactly one <h1>, that it is
// visually-hidden, and that its accessible text is scope. It exists so the
// review screen's card, break and summary states all get the same check
// (#334): a single steady heading, not one per swapped panel.
func mustOneVisuallyHiddenH1(t *testing.T, doc *htmlassert.Doc, scope string) {
	t.Helper()
	hs := doc.QueryAll("h1")
	if len(hs) != 1 {
		t.Fatalf("page has %d <h1> elements, want exactly 1", len(hs))
	}
	h1 := hs[0]
	class, _ := htmlassert.Attr(h1, "class")
	if !containsClass(class, "visually-hidden") {
		t.Errorf("h1 class = %q, want it to include visually-hidden", class)
	}
	if got := htmlassert.Text(h1); got != scope {
		t.Errorf("h1 text = %q, want %q", got, scope)
	}
}

func TestReviewPageHasOneHiddenH1InCardState(t *testing.T) {
	s := newServer(t)
	deck, err := s.Store.CreateDeck(t.Context(), s.Alice.User.ID, "Spanish", "", flash.DefaultDeckColor)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Store.CreateCard(t.Context(), s.Alice.User.ID, deck.ID, flash.CardTypeBasic, "hola", "hello", ""); err != nil {
		t.Fatal(err)
	}

	doc := s.Get(t, s.Alice, "/flash/review/"+itoa(deck.ID))
	doc.MustHave(".flash-review-card")
	mustOneVisuallyHiddenH1(t, doc, "Spanish")
}

func TestReviewPageHasOneHiddenH1InSummaryState(t *testing.T) {
	s := newServer(t)
	doc := s.Get(t, s.Alice, "/flash/review")
	doc.MustHave(".flash-review-summary")
	mustOneVisuallyHiddenH1(t, doc, "All decks")
	if h2s := doc.QueryAll(".flash-review-summary h2"); len(h2s) != 1 {
		t.Errorf(".flash-review-summary has %d <h2>, want 1", len(h2s))
	}
	doc.MustNotHave(".flash-review-summary h1")
}

func TestReviewPageHasOneHiddenH1InBreakState(t *testing.T) {
	s := newServer(t)
	now := time.Now().UTC()
	deck, err := s.Store.CreateDeck(t.Context(), s.Alice.User.ID, "Spanish", "", flash.DefaultDeckColor)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Store.CreateCard(t.Context(), s.Alice.User.ID, deck.ID, flash.CardTypeBasic, "hola", "hello", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Store.SnoozeDeck(t.Context(), s.Alice.User.ID, deck.ID, now.AddDate(0, 0, 7)); err != nil {
		t.Fatal(err)
	}

	doc := s.Get(t, s.Alice, "/flash/review/"+itoa(deck.ID))
	doc.MustHave(".flash-review-break")
	mustOneVisuallyHiddenH1(t, doc, "Spanish")
	if h2s := doc.QueryAll(".flash-review-break h2"); len(h2s) != 1 {
		t.Errorf(".flash-review-break has %d <h2>, want 1", len(h2s))
	}
	doc.MustNotHave(".flash-review-break h1")
}

func TestReviewLiveRegionIsOutsideReviewBodyAndEmptyOnFullLoad(t *testing.T) {
	s := newServer(t)
	doc := s.Get(t, s.Alice, "/flash/review")
	region := doc.MustHave("#review-announce")
	if v, _ := htmlassert.Attr(region, "role"); v != "status" {
		t.Errorf("review-announce role = %q, want status", v)
	}
	if v, _ := htmlassert.Attr(region, "aria-live"); v != "polite" {
		t.Errorf("review-announce aria-live = %q, want polite", v)
	}
	class, _ := htmlassert.Attr(region, "class")
	if !containsClass(class, "visually-hidden") {
		t.Errorf("review-announce class = %q, want it to include visually-hidden", class)
	}
	if got := htmlassert.Text(region); got != "" {
		t.Errorf("review-announce text on full load = %q, want empty (avoid double announcement)", got)
	}
	doc.MustNotHave("#review-body #review-announce") // it lives outside the swapped area
}

func TestGradeAnnouncesCardPosition(t *testing.T) {
	s := newServer(t)
	deck, err := s.Store.CreateDeck(t.Context(), s.Alice.User.ID, "Spanish", "", flash.DefaultDeckColor)
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

	rec := s.PostHX(t, s.Alice, "/flash/review/grade?deck="+itoa(deck.ID), url.Values{"card_id": {itoa(first.ID)}, "rating": {"3"}})
	doc := htmlassert.Parse(t, rec.Body.String())
	region := doc.MustHave("#review-announce")
	if got := htmlassert.Text(region); got != "Card 2 of 3" {
		t.Errorf("announce = %q, want %q", got, "Card 2 of 3")
	}
}

func TestGradeFinishingQueueAnnouncesSummary(t *testing.T) {
	s := newServer(t)
	deck, err := s.Store.CreateDeck(t.Context(), s.Alice.User.ID, "Spanish", "", flash.DefaultDeckColor)
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
	headline := htmlassert.Text(doc.MustHave(".flash-review-summary h2"))
	region := doc.MustHave("#review-announce")
	if got := htmlassert.Text(region); got != headline {
		t.Errorf("announce = %q, want the summary headline %q", got, headline)
	}
	_ = summary
}

func TestGradeIntoBreakStateAnnouncesBreak(t *testing.T) {
	s := newServer(t)
	deck, err := s.Store.CreateDeck(t.Context(), s.Alice.User.ID, "Spanish", "", flash.DefaultDeckColor)
	if err != nil {
		t.Fatal(err)
	}
	c, err := s.Store.CreateCard(t.Context(), s.Alice.User.ID, deck.ID, flash.CardTypeBasic, "hola", "hello", "")
	if err != nil {
		t.Fatal(err)
	}
	// Snooze the deck first: grading its card still succeeds (grading
	// doesn't check snooze), but the deck drops out of its own queue, so
	// the post-grade response lands on the break panel, not the summary.
	if _, err := s.Store.SnoozeDeck(t.Context(), s.Alice.User.ID, deck.ID, time.Now().UTC().AddDate(0, 0, 7)); err != nil {
		t.Fatal(err)
	}

	rec := s.PostHX(t, s.Alice, "/flash/review/grade?deck="+itoa(deck.ID), url.Values{"card_id": {itoa(c.ID)}, "rating": {"3"}})
	doc := htmlassert.Parse(t, rec.Body.String())
	doc.MustHave(".flash-review-break")
	region := doc.MustHave("#review-announce")
	if got := htmlassert.Text(region); got != "This deck is taking a break" {
		t.Errorf("announce = %q, want %q", got, "This deck is taking a break")
	}
}
