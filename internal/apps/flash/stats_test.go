// internal/apps/flash/stats_test.go
package flash_test

import (
	"context"
	"testing"
	"time"

	"github.com/iliafrenkel/on-suite/internal/apps/flash"
)

func TestStreakCountsConsecutiveDaysAndStopsAtAGap(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	deck, err := f.store.CreateDeck(ctx, f.alice.ID, "Spanish", "")
	if err != nil {
		t.Fatal(err)
	}
	card, err := f.store.CreateCard(ctx, f.alice.ID, deck.ID, flash.CardTypeBasic, "a", "b", "")
	if err != nil {
		t.Fatal(err)
	}
	day1 := time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)
	day2 := day1.AddDate(0, 0, 1)
	// day3 (day1+2) is a gap
	day4 := day1.AddDate(0, 0, 3)

	if _, err := f.store.GradeCard(ctx, f.alice.ID, card.ID, flash.RatingGood, day1); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.GradeCard(ctx, f.alice.ID, card.ID, flash.RatingGood, day2); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.GradeCard(ctx, f.alice.ID, card.ID, flash.RatingGood, day4); err != nil {
		t.Fatal(err)
	}

	// "now" = day4 itself: today (day4) has a review, so the walk starts there.
	streak, err := f.store.Streak(ctx, f.alice.ID, day4)
	if err != nil {
		t.Fatal(err)
	}
	if streak != 1 {
		t.Errorf("streak at day4 = %d, want 1 (day3 gap breaks the chain)", streak)
	}

	// "now" = day2 itself: two consecutive days (day1, day2).
	streak, err = f.store.Streak(ctx, f.alice.ID, day2)
	if err != nil {
		t.Fatal(err)
	}
	if streak != 2 {
		t.Errorf("streak at day2 = %d, want 2", streak)
	}
}

func TestStreakDoesNotBreakBeforeTodaysReview(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	deck, err := f.store.CreateDeck(ctx, f.alice.ID, "Spanish", "")
	if err != nil {
		t.Fatal(err)
	}
	card, err := f.store.CreateCard(ctx, f.alice.ID, deck.ID, flash.CardTypeBasic, "a", "b", "")
	if err != nil {
		t.Fatal(err)
	}
	yesterday := time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)
	if _, err := f.store.GradeCard(ctx, f.alice.ID, card.ID, flash.RatingGood, yesterday); err != nil {
		t.Fatal(err)
	}

	// "now" is the next calendar day, before any review has happened today.
	today := yesterday.AddDate(0, 0, 1).Add(2 * time.Hour)
	streak, err := f.store.Streak(ctx, f.alice.ID, today)
	if err != nil {
		t.Fatal(err)
	}
	if streak != 1 {
		t.Errorf("streak = %d, want 1 (yesterday's review still counts before today's review happens)", streak)
	}
}

func TestStreakIsZeroWithNoReviewsEver(t *testing.T) {
	f := newFixture(t)
	streak, err := f.store.Streak(context.Background(), f.alice.ID, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if streak != 0 {
		t.Errorf("streak = %d, want 0", streak)
	}
}

func TestRetentionRateComputesFromRatingCounters(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	deck, err := f.store.CreateDeck(ctx, f.alice.ID, "Spanish", "")
	if err != nil {
		t.Fatal(err)
	}
	c1, err := f.store.CreateCard(ctx, f.alice.ID, deck.ID, flash.CardTypeBasic, "a", "b", "")
	if err != nil {
		t.Fatal(err)
	}
	c2, err := f.store.CreateCard(ctx, f.alice.ID, deck.ID, flash.CardTypeBasic, "c", "d", "")
	if err != nil {
		t.Fatal(err)
	}
	c3, err := f.store.CreateCard(ctx, f.alice.ID, deck.ID, flash.CardTypeBasic, "e", "f", "")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)
	if _, err := f.store.GradeCard(ctx, f.alice.ID, c1.ID, flash.RatingAgain, now); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.GradeCard(ctx, f.alice.ID, c2.ID, flash.RatingGood, now); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.GradeCard(ctx, f.alice.ID, c3.ID, flash.RatingEasy, now); err != nil {
		t.Fatal(err)
	}

	rate, err := f.store.RetentionRate(ctx, f.alice.ID, now.AddDate(0, 0, -30))
	if err != nil {
		t.Fatal(err)
	}
	want := 2.0 / 3.0
	if rate < want-0.001 || rate > want+0.001 {
		t.Errorf("rate = %v, want %v", rate, want)
	}
}

func TestRetentionRateIsZeroWithNoReviewsInWindow(t *testing.T) {
	f := newFixture(t)
	rate, err := f.store.RetentionRate(context.Background(), f.alice.ID, time.Now().AddDate(0, 0, -30))
	if err != nil {
		t.Fatal(err)
	}
	if rate != 0 {
		t.Errorf("rate = %v, want 0", rate)
	}
}

func TestCardsMasteredCountsOnlyReviewStateForThatUser(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	deck, err := f.store.CreateDeck(ctx, f.alice.ID, "Spanish", "")
	if err != nil {
		t.Fatal(err)
	}
	card, err := f.store.CreateCard(ctx, f.alice.ID, deck.ID, flash.CardTypeBasic, "a", "b", "")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)
	// First-ever grade lands in "learning", not "review" yet — not mastered.
	if _, err := f.store.GradeCard(ctx, f.alice.ID, card.ID, flash.RatingGood, now); err != nil {
		t.Fatal(err)
	}
	mastered, err := f.store.CardsMastered(ctx, f.alice.ID)
	if err != nil {
		t.Fatal(err)
	}
	if mastered != 0 {
		t.Errorf("mastered = %d, want 0 (card is still in learning)", mastered)
	}

	// Bob has no cards at all — must not be affected by or counted with Alice's.
	bobMastered, err := f.store.CardsMastered(ctx, f.bob.ID)
	if err != nil {
		t.Fatal(err)
	}
	if bobMastered != 0 {
		t.Errorf("bob's mastered = %d, want 0", bobMastered)
	}

	// go-fsrs transitions a card from "new" to "learning" on its first Good
	// grade (already applied above) and from "learning" to "review" on the
	// next Good grade, independent of elapsed time — no learning-step delay
	// to satisfy. The grade after that (Review + Good) simply stays in
	// "review", so grading twice more here is a belt-and-suspenders check,
	// not a requirement for reaching "review".
	if _, err := f.store.GradeCard(ctx, f.alice.ID, card.ID, flash.RatingGood, now.Add(20*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.GradeCard(ctx, f.alice.ID, card.ID, flash.RatingGood, now.Add(40*time.Minute)); err != nil {
		t.Fatal(err)
	}
	mastered, err = f.store.CardsMastered(ctx, f.alice.ID)
	if err != nil {
		t.Fatal(err)
	}
	if mastered != 1 {
		t.Errorf("mastered after graduating = %d, want 1", mastered)
	}

	// Bob still has nothing, even though Alice now has a mastered card.
	bobMastered, err = f.store.CardsMastered(ctx, f.bob.ID)
	if err != nil {
		t.Fatal(err)
	}
	if bobMastered != 0 {
		t.Errorf("bob's mastered = %d, want 0 (unaffected by alice's card)", bobMastered)
	}
}

func TestDailyReviewCountsZeroFillsEveryDay(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	deck, err := f.store.CreateDeck(ctx, f.alice.ID, "Spanish", "")
	if err != nil {
		t.Fatal(err)
	}
	card, err := f.store.CreateCard(ctx, f.alice.ID, deck.ID, flash.CardTypeBasic, "a", "b", "")
	if err != nil {
		t.Fatal(err)
	}
	day1 := time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)
	if _, err := f.store.GradeCard(ctx, f.alice.ID, card.ID, flash.RatingGood, day1); err != nil {
		t.Fatal(err)
	}

	now := day1.AddDate(0, 0, 2)
	counts, err := f.store.DailyReviewCounts(ctx, f.alice.ID, 3, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(counts) != 3 {
		t.Fatalf("len(counts) = %d, want 3", len(counts))
	}
	if counts[0].Count != 1 {
		t.Errorf("counts[0] (day1) = %d, want 1", counts[0].Count)
	}
	if counts[1].Count != 0 || counts[2].Count != 0 {
		t.Errorf("counts[1], counts[2] = %d, %d, want 0, 0", counts[1].Count, counts[2].Count)
	}
}

func TestPerDeckLoadAggregatesAcrossDecks(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	deckA, err := f.store.CreateDeck(ctx, f.alice.ID, "Spanish", "")
	if err != nil {
		t.Fatal(err)
	}
	deckB, err := f.store.CreateDeck(ctx, f.alice.ID, "French", "")
	if err != nil {
		t.Fatal(err)
	}
	cardA, err := f.store.CreateCard(ctx, f.alice.ID, deckA.ID, flash.CardTypeBasic, "a", "b", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.CreateCard(ctx, f.alice.ID, deckB.ID, flash.CardTypeBasic, "c", "d", ""); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)
	if _, err := f.store.GradeCard(ctx, f.alice.ID, cardA.ID, flash.RatingGood, now); err != nil {
		t.Fatal(err)
	}

	loads, err := f.store.PerDeckLoad(ctx, f.alice.ID, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(loads) != 2 {
		t.Fatalf("len(loads) = %d, want 2", len(loads))
	}
	var spanish, french flash.DeckLoad
	for _, l := range loads {
		if l.Deck.ID == deckA.ID {
			spanish = l
		}
		if l.Deck.ID == deckB.ID {
			french = l
		}
	}
	if spanish.ReviewsLast30Days != 1 {
		t.Errorf("spanish.ReviewsLast30Days = %d, want 1", spanish.ReviewsLast30Days)
	}
	if french.ReviewsLast30Days != 0 {
		t.Errorf("french.ReviewsLast30Days = %d, want 0", french.ReviewsLast30Days)
	}
	// cardA has been graded once and is still in "learning" (a single Good
	// grade doesn't graduate it), and its due_at is in the future, so
	// neither Mastered nor Due should count it yet.
	if spanish.Mastered != 0 {
		t.Errorf("spanish.Mastered = %d, want 0", spanish.Mastered)
	}
	if spanish.Due != 0 {
		t.Errorf("spanish.Due = %d, want 0", spanish.Due)
	}
	if french.Mastered != 0 {
		t.Errorf("french.Mastered = %d, want 0", french.Mastered)
	}
	if french.Due != 0 {
		t.Errorf("french.Due = %d, want 0", french.Due)
	}
}

func TestPerDeckLoadSortsByName(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	// Create decks in an order that is neither alphabetical nor its own
	// reverse, so a bug that returns creation order (ListDecks' own,
	// newest-first order) or reverses it would both be caught.
	if _, err := f.store.CreateDeck(ctx, f.alice.ID, "Zebra", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.CreateDeck(ctx, f.alice.ID, "Apple", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.CreateDeck(ctx, f.alice.ID, "Mango", ""); err != nil {
		t.Fatal(err)
	}

	loads, err := f.store.PerDeckLoad(ctx, f.alice.ID, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if len(loads) != 3 {
		t.Fatalf("len(loads) = %d, want 3", len(loads))
	}
	var names []string
	for _, l := range loads {
		names = append(names, l.Deck.Name)
	}
	want := []string{"Apple", "Mango", "Zebra"}
	for i, n := range want {
		if names[i] != n {
			t.Errorf("names = %v, want %v", names, want)
			break
		}
	}
}

func TestPerDeckLoadMarksSnoozedDecks(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	active, err := f.store.CreateDeck(ctx, f.alice.ID, "Active", "")
	if err != nil {
		t.Fatal(err)
	}
	snoozed, err := f.store.CreateDeck(ctx, f.alice.ID, "Snoozed", "")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)
	if _, err := f.store.SnoozeDeck(ctx, f.alice.ID, snoozed.ID, now.AddDate(0, 0, 1)); err != nil {
		t.Fatal(err)
	}

	loads, err := f.store.PerDeckLoad(ctx, f.alice.ID, now)
	if err != nil {
		t.Fatal(err)
	}
	var gotActive, gotSnoozed flash.DeckLoad
	for _, l := range loads {
		if l.Deck.ID == active.ID {
			gotActive = l
		}
		if l.Deck.ID == snoozed.ID {
			gotSnoozed = l
		}
	}
	if gotActive.Snoozed {
		t.Error("active deck reported as Snoozed")
	}
	if !gotSnoozed.Snoozed {
		t.Error("snoozed deck not reported as Snoozed")
	}
}

func TestCardsDueTodayExcludesSnoozedDecksAndNeverReviewedCards(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	deck, err := f.store.CreateDeck(ctx, f.alice.ID, "Spanish", "")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)

	// (a) A never-graded card has no flash_card_state row at all — must not
	// count as due.
	if _, err := f.store.CreateCard(ctx, f.alice.ID, deck.ID, flash.CardTypeBasic, "never", "graded", ""); err != nil {
		t.Fatal(err)
	}
	due, err := f.store.CardsDueToday(ctx, f.alice.ID, now)
	if err != nil {
		t.Fatal(err)
	}
	if due != 0 {
		t.Errorf("due (never-graded only) = %d, want 0", due)
	}

	// (b) A graded card whose due_at has passed counts, once its deck is
	// not snoozed.
	graded, err := f.store.CreateCard(ctx, f.alice.ID, deck.ID, flash.CardTypeBasic, "a", "b", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.GradeCard(ctx, f.alice.ID, graded.ID, flash.RatingGood, now); err != nil {
		t.Fatal(err)
	}
	later := now.Add(24 * time.Hour)
	due, err = f.store.CardsDueToday(ctx, f.alice.ID, later)
	if err != nil {
		t.Fatal(err)
	}
	if due != 1 {
		t.Errorf("due (graded card past due) = %d, want 1", due)
	}

	// (c) The same card, but now its deck is snoozed — must not count.
	if _, err := f.store.SnoozeDeck(ctx, f.alice.ID, deck.ID, later.AddDate(0, 0, 1)); err != nil {
		t.Fatal(err)
	}
	due, err = f.store.CardsDueToday(ctx, f.alice.ID, later)
	if err != nil {
		t.Fatal(err)
	}
	if due != 0 {
		t.Errorf("due (deck snoozed) = %d, want 0", due)
	}
}

func TestCardsDueTodayIsUncappedUnlikeDueQueue(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	deck, err := f.store.CreateDeck(ctx, f.alice.ID, "Spanish", "")
	if err != nil {
		t.Fatal(err)
	}
	limit := 1
	if _, err := f.store.UpdateDeckSettings(ctx, f.alice.ID, deck.ID, deck.NewCardsPerDay, &limit); err != nil {
		t.Fatal(err)
	}

	now := time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)
	var cardIDs []int64
	for i := 0; i < 3; i++ {
		c, err := f.store.CreateCard(ctx, f.alice.ID, deck.ID, flash.CardTypeBasic, "a", "b", "")
		if err != nil {
			t.Fatal(err)
		}
		cardIDs = append(cardIDs, c.ID)
	}
	// Grade all three on an earlier day so today's daily budget hasn't
	// been spent yet, then let them all age past due.
	for _, id := range cardIDs {
		if _, err := f.store.GradeCard(ctx, f.alice.ID, id, flash.RatingGood, now); err != nil {
			t.Fatal(err)
		}
	}
	later := now.Add(48 * time.Hour)

	queue, err := f.store.DueQueue(ctx, f.alice.ID, nil, later)
	if err != nil {
		t.Fatal(err)
	}
	if len(queue) != 1 {
		t.Errorf("len(DueQueue) = %d, want 1 (capped by ReviewsPerDay)", len(queue))
	}

	due, err := f.store.CardsDueToday(ctx, f.alice.ID, later)
	if err != nil {
		t.Fatal(err)
	}
	if due != 3 {
		t.Errorf("CardsDueToday = %d, want 3 (uncapped)", due)
	}
}
