// internal/apps/flash/deck_summary_test.go
package flash_test

import (
	"context"
	"testing"
	"time"

	"github.com/iliafrenkel/on-suite/internal/apps/flash"
)

// summaryFor returns the one summary for deckID, failing the test if it is
// missing.
func summaryFor(t *testing.T, sums []flash.DeckSummary, deckID int64) flash.DeckSummary {
	t.Helper()
	for _, s := range sums {
		if s.Deck.ID == deckID {
			return s
		}
	}
	t.Fatalf("no summary for deck %d in %+v", deckID, sums)
	return flash.DeckSummary{}
}

func TestDeckSummariesCountsAFreshDeck(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	now := time.Date(2026, 9, 23, 10, 0, 0, 0, time.UTC)
	d, err := f.store.CreateDeck(ctx, f.alice.ID, "Spanish", "")
	if err != nil {
		t.Fatal(err)
	}
	for _, front := range []string{"hola", "adiós", "gracias"} {
		if _, err := f.store.CreateCard(ctx, f.alice.ID, d.ID, flash.CardTypeBasic, front, "x", ""); err != nil {
			t.Fatal(err)
		}
	}

	sums, err := f.store.DeckSummaries(ctx, f.alice.ID, now)
	if err != nil {
		t.Fatal(err)
	}
	s := summaryFor(t, sums, d.ID)
	if s.CardCount != 3 || s.NewUnseen != 3 || s.NewToday != 3 || s.DueTotal != 0 || s.DueToday != 0 || s.ReviewNow != 3 {
		t.Errorf("summary = %+v, want 3 cards, all new and all allowed today", s)
	}
	if s.NextDueAt != nil || s.Mastered != 0 || s.Snoozed {
		t.Errorf("summary = %+v, want no next due, nothing mastered, not snoozed", s)
	}
}

// TestDeckSummariesAgreeWithDueQueue is the invariant the whole deck pane
// depends on: the number on the Review button must be exactly how many cards
// the review page will then show.
func TestDeckSummariesAgreeWithDueQueue(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	t0 := time.Date(2026, 9, 23, 10, 0, 0, 0, time.UTC)

	d, err := f.store.CreateDeck(ctx, f.alice.ID, "Spanish", "")
	if err != nil {
		t.Fatal(err)
	}
	limit := 2
	if _, err := f.store.UpdateDeckSettings(ctx, f.alice.ID, d.ID, 3, &limit); err != nil {
		t.Fatal(err)
	}
	var cards []flash.Card
	for i := 0; i < 6; i++ {
		c, err := f.store.CreateCard(ctx, f.alice.ID, d.ID, flash.CardTypeBasic, "front", "back", "")
		if err != nil {
			t.Fatal(err)
		}
		cards = append(cards, c)
	}
	// Grade three cards now: they leave the "new" pool and come back due
	// minutes later (learning steps), and they use up the whole new-card
	// budget of 3 for today.
	for _, c := range cards[:3] {
		if _, err := f.store.GradeCard(ctx, f.alice.ID, c.ID, flash.RatingAgain, t0); err != nil {
			t.Fatal(err)
		}
	}

	for _, now := range []time.Time{t0, t0.Add(30 * time.Minute), t0.Add(2 * time.Hour)} {
		sums, err := f.store.DeckSummaries(ctx, f.alice.ID, now)
		if err != nil {
			t.Fatal(err)
		}
		s := summaryFor(t, sums, d.ID)
		queue, err := f.store.DueQueue(ctx, f.alice.ID, &d.ID, now)
		if err != nil {
			t.Fatal(err)
		}
		if s.ReviewNow != len(queue) {
			t.Errorf("at %v: ReviewNow = %d, len(DueQueue) = %d; summary = %+v", now, s.ReviewNow, len(queue), s)
		}
		if s.ReviewNow != s.DueToday+s.NewToday {
			t.Errorf("at %v: ReviewNow %d != DueToday %d + NewToday %d", now, s.ReviewNow, s.DueToday, s.NewToday)
		}
		if s.NewUnseen != 3 || s.NewToday != 0 {
			t.Errorf("at %v: NewUnseen = %d, NewToday = %d; want 3 unseen and none left in today's budget", now, s.NewUnseen, s.NewToday)
		}
		if s.DueToday > limit {
			t.Errorf("at %v: DueToday = %d exceeds the review limit %d", now, s.DueToday, limit)
		}
	}
}

func TestDeckSummariesZeroTodayForASnoozedDeck(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	now := time.Date(2026, 9, 23, 10, 0, 0, 0, time.UTC)
	d, err := f.store.CreateDeck(ctx, f.alice.ID, "Spanish", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.CreateCard(ctx, f.alice.ID, d.ID, flash.CardTypeBasic, "hola", "hello", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.SnoozeDeck(ctx, f.alice.ID, d.ID, now.Add(48*time.Hour)); err != nil {
		t.Fatal(err)
	}
	sums, err := f.store.DeckSummaries(ctx, f.alice.ID, now)
	if err != nil {
		t.Fatal(err)
	}
	s := summaryFor(t, sums, d.ID)
	if !s.Snoozed || s.ReviewNow != 0 || s.NewToday != 0 || s.DueToday != 0 {
		t.Errorf("summary = %+v, want snoozed with nothing to review today", s)
	}
	if s.CardCount != 1 || s.NewUnseen != 1 {
		t.Errorf("summary = %+v, want the card still counted", s)
	}
}

func TestDeckSummariesAreOwnerScopedAndInListOrder(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	now := time.Now().UTC()
	older, err := f.store.CreateDeck(ctx, f.alice.ID, "Older", "")
	if err != nil {
		t.Fatal(err)
	}
	newer, err := f.store.CreateDeck(ctx, f.alice.ID, "Newer", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.CreateDeck(ctx, f.bob.ID, "Bob's", ""); err != nil {
		t.Fatal(err)
	}
	sums, err := f.store.DeckSummaries(ctx, f.alice.ID, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(sums) != 2 || sums[0].Deck.ID != newer.ID || sums[1].Deck.ID != older.ID {
		t.Errorf("DeckSummaries = %+v, want alice's two decks newest first", sums)
	}
}
