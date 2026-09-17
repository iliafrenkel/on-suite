package flash_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/iliafrenkel/on-suite/internal/apps/flash"
)

func TestCardStateIsUnreviewedForANeverGradedCard(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	deck, err := f.store.CreateDeck(ctx, f.alice.ID, "Spanish", "")
	if err != nil {
		t.Fatal(err)
	}
	card, err := f.store.CreateCard(ctx, f.alice.ID, deck.ID, flash.CardTypeBasic, "hola", "hello", "")
	if err != nil {
		t.Fatal(err)
	}

	_, reviewed, err := f.store.CardState(ctx, f.alice.ID, card.ID)
	if err != nil {
		t.Fatalf("CardState: %v", err)
	}
	if reviewed {
		t.Error("CardState reports a never-graded card as reviewed")
	}
}

func TestCardStateRejectsSomeoneElsesCard(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	deck, err := f.store.CreateDeck(ctx, f.alice.ID, "alice's", "")
	if err != nil {
		t.Fatal(err)
	}
	card, err := f.store.CreateCard(ctx, f.alice.ID, deck.ID, flash.CardTypeBasic, "a", "b", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := f.store.CardState(ctx, f.bob.ID, card.ID); !errors.Is(err, flash.ErrNotFound) {
		t.Errorf("CardState as another user = %v, want ErrNotFound", err)
	}
}

func TestGradeCardCreatesStateOnFirstReview(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	deck, err := f.store.CreateDeck(ctx, f.alice.ID, "Spanish", "")
	if err != nil {
		t.Fatal(err)
	}
	card, err := f.store.CreateCard(ctx, f.alice.ID, deck.ID, flash.CardTypeBasic, "hola", "hello", "")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC)

	if _, err := f.store.GradeCard(ctx, f.alice.ID, card.ID, flash.RatingGood, now); err != nil {
		t.Fatalf("GradeCard: %v", err)
	}

	_, reviewed, err := f.store.CardState(ctx, f.alice.ID, card.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !reviewed {
		t.Error("CardState still reports unreviewed after grading")
	}
}

func TestGradeCardRejectsSomeoneElsesCard(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	deck, err := f.store.CreateDeck(ctx, f.alice.ID, "alice's", "")
	if err != nil {
		t.Fatal(err)
	}
	card, err := f.store.CreateCard(ctx, f.alice.ID, deck.ID, flash.CardTypeBasic, "a", "b", "")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC)
	if _, err := f.store.GradeCard(ctx, f.bob.ID, card.ID, flash.RatingGood, now); !errors.Is(err, flash.ErrNotFound) {
		t.Errorf("GradeCard as another user = %v, want ErrNotFound", err)
	}
}

func TestUndoLastGradeRestoresPreviousSchedule(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	deck, err := f.store.CreateDeck(ctx, f.alice.ID, "Spanish", "")
	if err != nil {
		t.Fatal(err)
	}
	card, err := f.store.CreateCard(ctx, f.alice.ID, deck.ID, flash.CardTypeBasic, "hola", "hello", "")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC)

	if _, err := f.store.GradeCard(ctx, f.alice.ID, card.ID, flash.RatingGood, now); err != nil {
		t.Fatal(err)
	}

	_, undone, err := f.store.UndoLastGrade(ctx, f.alice.ID, card.ID, now)
	if err != nil {
		t.Fatalf("UndoLastGrade: %v", err)
	}
	if !undone {
		t.Fatal("UndoLastGrade reported nothing to undo right after a grade")
	}

	_, reviewed, err := f.store.CardState(ctx, f.alice.ID, card.ID)
	if err != nil {
		t.Fatal(err)
	}
	if reviewed {
		t.Error("undoing a card's only grade should leave it unreviewed again")
	}
}

func TestUndoLastGradeIsANoOpWhenNothingToUndo(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	deck, err := f.store.CreateDeck(ctx, f.alice.ID, "Spanish", "")
	if err != nil {
		t.Fatal(err)
	}
	card, err := f.store.CreateCard(ctx, f.alice.ID, deck.ID, flash.CardTypeBasic, "hola", "hello", "")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC)

	if _, err := f.store.GradeCard(ctx, f.alice.ID, card.ID, flash.RatingGood, now); err != nil {
		t.Fatal(err)
	}
	if _, undone, err := f.store.UndoLastGrade(ctx, f.alice.ID, card.ID, now); err != nil || !undone {
		t.Fatalf("first UndoLastGrade = undone=%v, err=%v; want undone=true", undone, err)
	}
	// Nothing left to undo: the log was cleared by the undo above.
	if _, undone, err := f.store.UndoLastGrade(ctx, f.alice.ID, card.ID, now); err != nil || undone {
		t.Errorf("second UndoLastGrade = undone=%v, err=%v; want undone=false, err=nil", undone, err)
	}
}

func TestUndoLastGradeOnlyUndoesTheMostRecentGrade(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	deck, err := f.store.CreateDeck(ctx, f.alice.ID, "Spanish", "")
	if err != nil {
		t.Fatal(err)
	}
	card, err := f.store.CreateCard(ctx, f.alice.ID, deck.ID, flash.CardTypeBasic, "hola", "hello", "")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC)
	later := now.Add(24 * time.Hour)

	if _, err := f.store.GradeCard(ctx, f.alice.ID, card.ID, flash.RatingGood, now); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.GradeCard(ctx, f.alice.ID, card.ID, flash.RatingGood, later); err != nil {
		t.Fatal(err)
	}

	_, undone, err := f.store.UndoLastGrade(ctx, f.alice.ID, card.ID, later)
	if err != nil || !undone {
		t.Fatalf("UndoLastGrade = undone=%v, err=%v", undone, err)
	}
	_, reviewed, err := f.store.CardState(ctx, f.alice.ID, card.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !reviewed {
		t.Error("undoing the second grade should still leave the card reviewed once (from the first grade)")
	}
}

func TestGradeCardTracksDailyCounts(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	deck, err := f.store.CreateDeck(ctx, f.alice.ID, "Spanish", "")
	if err != nil {
		t.Fatal(err)
	}
	cardA, err := f.store.CreateCard(ctx, f.alice.ID, deck.ID, flash.CardTypeBasic, "a", "b", "")
	if err != nil {
		t.Fatal(err)
	}
	cardB, err := f.store.CreateCard(ctx, f.alice.ID, deck.ID, flash.CardTypeBasic, "c", "d", "")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC)

	// Grading a never-before-seen card counts as "new"; grading it again
	// later the same day counts as "review".
	if _, err := f.store.GradeCard(ctx, f.alice.ID, cardA.ID, flash.RatingGood, now); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.GradeCard(ctx, f.alice.ID, cardB.ID, flash.RatingGood, now); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.GradeCard(ctx, f.alice.ID, cardA.ID, flash.RatingGood, now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}

	newCount, reviewCount, err := f.store.DailyCountsForTest(ctx, f.alice.ID, deck.ID, now)
	if err != nil {
		t.Fatal(err)
	}
	if newCount != 2 {
		t.Errorf("new_count = %d, want 2", newCount)
	}
	if reviewCount != 1 {
		t.Errorf("review_count = %d, want 1", reviewCount)
	}
}

func TestUndoLastGradeDecrementsTheRightDailyCounter(t *testing.T) {
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
	now := time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC)

	if _, err := f.store.GradeCard(ctx, f.alice.ID, card.ID, flash.RatingGood, now); err != nil {
		t.Fatal(err)
	}
	if _, undone, err := f.store.UndoLastGrade(ctx, f.alice.ID, card.ID, now); err != nil || !undone {
		t.Fatalf("UndoLastGrade: undone=%v, err=%v", undone, err)
	}

	newCount, reviewCount, err := f.store.DailyCountsForTest(ctx, f.alice.ID, deck.ID, now)
	if err != nil {
		t.Fatal(err)
	}
	if newCount != 0 || reviewCount != 0 {
		t.Errorf("counts after undoing the only grade = new=%d review=%d, want 0/0", newCount, reviewCount)
	}
}

func TestDueQueueReturnsReviewsBeforeNewCards(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	deck, err := f.store.CreateDeck(ctx, f.alice.ID, "Spanish", "")
	if err != nil {
		t.Fatal(err)
	}
	dueCard, err := f.store.CreateCard(ctx, f.alice.ID, deck.ID, flash.CardTypeBasic, "due", "x", "")
	if err != nil {
		t.Fatal(err)
	}
	newCard, err := f.store.CreateCard(ctx, f.alice.ID, deck.ID, flash.CardTypeBasic, "new", "x", "")
	if err != nil {
		t.Fatal(err)
	}

	past := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	now := time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC)
	if _, err := f.store.GradeCard(ctx, f.alice.ID, dueCard.ID, flash.RatingAgain, past); err != nil {
		t.Fatal(err)
	}

	queue, err := f.store.DueQueue(ctx, f.alice.ID, nil, now)
	if err != nil {
		t.Fatalf("DueQueue: %v", err)
	}
	if len(queue) != 2 {
		t.Fatalf("DueQueue returned %d cards, want 2", len(queue))
	}
	if queue[0].Card.ID != dueCard.ID || queue[0].IsNew {
		t.Errorf("queue[0] = %+v, want the due review card marked IsNew=false", queue[0])
	}
	if queue[1].Card.ID != newCard.ID || !queue[1].IsNew {
		t.Errorf("queue[1] = %+v, want the new card marked IsNew=true", queue[1])
	}
}

func TestDueQueueExcludesNotYetDueCards(t *testing.T) {
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
	now := time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC)
	// Easy on a brand-new card schedules it well into the future.
	if _, err := f.store.GradeCard(ctx, f.alice.ID, card.ID, flash.RatingEasy, now); err != nil {
		t.Fatal(err)
	}

	queue, err := f.store.DueQueue(ctx, f.alice.ID, nil, now)
	if err != nil {
		t.Fatal(err)
	}
	for _, qc := range queue {
		if qc.Card.ID == card.ID {
			t.Errorf("DueQueue returned a card scheduled into the future: %+v", qc)
		}
	}
}

func TestDueQueueRespectsNewCardsPerDay(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	deck, err := f.store.CreateDeck(ctx, f.alice.ID, "Spanish", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.UpdateDeckSettings(ctx, f.alice.ID, deck.ID, 1, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.CreateCard(ctx, f.alice.ID, deck.ID, flash.CardTypeBasic, "a", "x", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.CreateCard(ctx, f.alice.ID, deck.ID, flash.CardTypeBasic, "b", "x", ""); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC)

	queue, err := f.store.DueQueue(ctx, f.alice.ID, nil, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(queue) != 1 {
		t.Fatalf("DueQueue with new_cards_per_day=1 returned %d cards, want 1", len(queue))
	}
}

func TestDueQueueRespectsReviewsPerDay(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	deck, err := f.store.CreateDeck(ctx, f.alice.ID, "Spanish", "")
	if err != nil {
		t.Fatal(err)
	}
	limit := 1
	if _, err := f.store.UpdateDeckSettings(ctx, f.alice.ID, deck.ID, 0, &limit); err != nil {
		t.Fatal(err)
	}
	past := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	now := time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC)
	cardA, err := f.store.CreateCard(ctx, f.alice.ID, deck.ID, flash.CardTypeBasic, "a", "x", "")
	if err != nil {
		t.Fatal(err)
	}
	cardB, err := f.store.CreateCard(ctx, f.alice.ID, deck.ID, flash.CardTypeBasic, "b", "x", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.GradeCard(ctx, f.alice.ID, cardA.ID, flash.RatingAgain, past); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.GradeCard(ctx, f.alice.ID, cardB.ID, flash.RatingAgain, past); err != nil {
		t.Fatal(err)
	}

	queue, err := f.store.DueQueue(ctx, f.alice.ID, nil, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(queue) != 1 {
		t.Fatalf("DueQueue with reviews_per_day=1 (both cards due) returned %d cards, want 1", len(queue))
	}
}

func TestDueQueueExcludesSnoozedDecks(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	deck, err := f.store.CreateDeck(ctx, f.alice.ID, "Spanish", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.CreateCard(ctx, f.alice.ID, deck.ID, flash.CardTypeBasic, "a", "x", ""); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC)
	if _, err := f.store.SnoozeDeck(ctx, f.alice.ID, deck.ID, now.AddDate(0, 0, 7)); err != nil {
		t.Fatal(err)
	}

	queue, err := f.store.DueQueue(ctx, f.alice.ID, nil, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(queue) != 0 {
		t.Errorf("DueQueue for an account with only a snoozed deck = %d cards, want 0", len(queue))
	}

	// Requesting that specific deck's review page directly is also empty,
	// per the design doc: snoozing affects queue construction the same way
	// whether it is cross-deck or scoped to one deck.
	scoped, err := f.store.DueQueue(ctx, f.alice.ID, &deck.ID, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(scoped) != 0 {
		t.Errorf("DueQueue(deckID) for a snoozed deck = %d cards, want 0", len(scoped))
	}
}

func TestDueQueueScopedToOneDeckIgnoresOtherDecks(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	deckA, err := f.store.CreateDeck(ctx, f.alice.ID, "A", "")
	if err != nil {
		t.Fatal(err)
	}
	deckB, err := f.store.CreateDeck(ctx, f.alice.ID, "B", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.CreateCard(ctx, f.alice.ID, deckA.ID, flash.CardTypeBasic, "a", "x", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.CreateCard(ctx, f.alice.ID, deckB.ID, flash.CardTypeBasic, "b", "x", ""); err != nil {
		t.Fatal(err)
	}

	queue, err := f.store.DueQueue(ctx, f.alice.ID, &deckA.ID, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if len(queue) != 1 || queue[0].Deck.ID != deckA.ID {
		t.Errorf("DueQueue(deckA) = %+v, want exactly one card from deckA", queue)
	}
}

func TestDueQueueIsOwnerScoped(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	deck, err := f.store.CreateDeck(ctx, f.alice.ID, "alice's", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.CreateCard(ctx, f.alice.ID, deck.ID, flash.CardTypeBasic, "a", "x", ""); err != nil {
		t.Fatal(err)
	}
	queue, err := f.store.DueQueue(ctx, f.bob.ID, nil, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if len(queue) != 0 {
		t.Errorf("bob's DueQueue sees alice's cards: %+v", queue)
	}
}
