package flash_test

import (
	"context"
	"errors"
	"slices"
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

	newCount, reviewCount, err := f.store.DailyCounts(ctx, f.alice.ID, deck.ID, now)
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

	newCount, reviewCount, err := f.store.DailyCounts(ctx, f.alice.ID, deck.ID, now)
	if err != nil {
		t.Fatal(err)
	}
	if newCount != 0 || reviewCount != 0 {
		t.Errorf("counts after undoing the only grade = new=%d review=%d, want 0/0", newCount, reviewCount)
	}
}

// TestUndoLastGradeDecrementsTheGradeDaysCounterAcrossMidnight guards against
// a bug where UndoLastGrade decremented the daily counter for the day the
// undo happened, rather than the day the original grade happened. A grade
// just before UTC midnight, undone just after, must zero out the grade day's
// counter and must not leave a negative count on the undo day.
func TestUndoLastGradeDecrementsTheGradeDaysCounterAcrossMidnight(t *testing.T) {
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
	now := time.Date(2026, 9, 17, 23, 0, 0, 0, time.UTC)
	laterAcrossMidnight := now.Add(2 * time.Hour)

	if _, err := f.store.GradeCard(ctx, f.alice.ID, card.ID, flash.RatingGood, now); err != nil {
		t.Fatal(err)
	}
	if _, undone, err := f.store.UndoLastGrade(ctx, f.alice.ID, card.ID, laterAcrossMidnight); err != nil || !undone {
		t.Fatalf("UndoLastGrade: undone=%v, err=%v", undone, err)
	}

	gradeDayNew, gradeDayReview, err := f.store.DailyCounts(ctx, f.alice.ID, deck.ID, now)
	if err != nil {
		t.Fatal(err)
	}
	if gradeDayNew != 0 || gradeDayReview != 0 {
		t.Errorf("grade day counts after undo = new=%d review=%d, want 0/0", gradeDayNew, gradeDayReview)
	}

	undoDayNew, undoDayReview, err := f.store.DailyCounts(ctx, f.alice.ID, deck.ID, laterAcrossMidnight)
	if err != nil {
		t.Fatal(err)
	}
	if undoDayNew != 0 || undoDayReview != 0 {
		t.Errorf("undo day counts after undo = new=%d review=%d, want 0/0 (no negative or stray counter)", undoDayNew, undoDayReview)
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

func ratingCount(t *testing.T, f *fixture, userID, deckID int64, day time.Time, column string) int {
	t.Helper()
	var n int
	err := f.db.QueryRowContext(context.Background(),
		`SELECT `+column+` FROM flash_review_counts WHERE user_id = ? AND deck_id = ? AND day = ?`,
		userID, deckID, day.UTC().Format("2006-01-02")).Scan(&n)
	if err != nil {
		t.Fatalf("read %s: %v", column, err)
	}
	return n
}

func TestGradeCardBumpsTheMatchingRatingCounter(t *testing.T) {
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
	now := time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)

	if _, err := f.store.GradeCard(ctx, f.alice.ID, card.ID, flash.RatingAgain, now); err != nil {
		t.Fatal(err)
	}
	if got := ratingCount(t, f, f.alice.ID, deck.ID, now, "again_count"); got != 1 {
		t.Errorf("again_count = %d, want 1", got)
	}
	for _, col := range []string{"hard_count", "good_count", "easy_count"} {
		if got := ratingCount(t, f, f.alice.ID, deck.ID, now, col); got != 0 {
			t.Errorf("%s = %d, want 0", col, got)
		}
	}
}

func TestUndoLastGradeDecrementsTheMatchingRatingCounter(t *testing.T) {
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
	now := time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)
	later := now.Add(time.Hour)

	if _, err := f.store.GradeCard(ctx, f.alice.ID, card.ID, flash.RatingEasy, now); err != nil {
		t.Fatal(err)
	}
	if _, undone, err := f.store.UndoLastGrade(ctx, f.alice.ID, card.ID, later); err != nil || !undone {
		t.Fatalf("UndoLastGrade: undone=%v, err=%v", undone, err)
	}
	if got := ratingCount(t, f, f.alice.ID, deck.ID, now, "easy_count"); got != 0 {
		t.Errorf("easy_count after undo = %d, want 0", got)
	}
}

// TestBumpDailyCountsFloorsRatingColumnAtZero proves bumpDailyCounts' UPDATE
// clamps a rating column at 0 rather than letting it go negative. A card
// graded just before migration 0009 shipped, whose flash_review_counts row
// predates the new columns, can leave a rating column at 0 for a day that
// still has an undo-able grade logged against it; undoing that grade must
// not drive the column negative, since RetentionRate sums these columns
// directly and a negative summand can push its ratio over 100%.
func TestBumpDailyCountsFloorsRatingColumnAtZero(t *testing.T) {
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
	now := time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)

	if _, err := f.store.GradeCard(ctx, f.alice.ID, card.ID, flash.RatingAgain, now); err != nil {
		t.Fatal(err)
	}
	if got := ratingCount(t, f, f.alice.ID, deck.ID, now, "again_count"); got != 1 {
		t.Fatalf("again_count after grade = %d, want 1", got)
	}

	// Simulate the pre-existing-row edge case: force again_count back to 0
	// (as if this row predated migration 0009, or was otherwise out of
	// sync with the still-undo-able grade log) while leaving the grade
	// itself undone.
	if _, err := f.db.ExecContext(ctx,
		`UPDATE flash_review_counts SET again_count = 0 WHERE user_id = ? AND deck_id = ? AND day = ?`,
		f.alice.ID, deck.ID, now.UTC().Format("2006-01-02"),
	); err != nil {
		t.Fatal(err)
	}

	// Undoing the Again grade now tries to decrement again_count from 0,
	// which must floor at 0 instead of going to -1.
	if _, undone, err := f.store.UndoLastGrade(ctx, f.alice.ID, card.ID, now.Add(time.Hour)); err != nil || !undone {
		t.Fatalf("UndoLastGrade: undone=%v, err=%v", undone, err)
	}
	if got := ratingCount(t, f, f.alice.ID, deck.ID, now, "again_count"); got != 0 {
		t.Errorf("again_count after undo of an already-zeroed column = %d, want 0 (floored, not negative)", got)
	}
}

func TestTodayTally(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	now := time.Date(2026, 9, 23, 10, 0, 0, 0, time.UTC)
	a, err := f.store.CreateDeck(ctx, f.alice.ID, "A", "")
	if err != nil {
		t.Fatal(err)
	}
	b, err := f.store.CreateDeck(ctx, f.alice.ID, "B", "")
	if err != nil {
		t.Fatal(err)
	}
	grade := func(deckID int64, rating int, at time.Time) {
		t.Helper()
		c, err := f.store.CreateCard(ctx, f.alice.ID, deckID, flash.CardTypeBasic, "q", "a", "")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.store.GradeCard(ctx, f.alice.ID, c.ID, rating, at); err != nil {
			t.Fatal(err)
		}
	}
	grade(a.ID, flash.RatingGood, now)
	grade(a.ID, flash.RatingGood, now)
	grade(a.ID, flash.RatingAgain, now)
	grade(b.ID, flash.RatingEasy, now)
	grade(b.ID, flash.RatingHard, now.AddDate(0, 0, -1)) // yesterday: not counted

	got, err := f.store.TodayTally(ctx, f.alice.ID, &a.ID, now)
	if err != nil {
		t.Fatal(err)
	}
	if want := (flash.ReviewTally{Reviewed: 3, Again: 1, Good: 2}); got != want {
		t.Errorf("deck A tally = %+v, want %+v", got, want)
	}
	all, err := f.store.TodayTally(ctx, f.alice.ID, nil, now)
	if err != nil {
		t.Fatal(err)
	}
	if want := (flash.ReviewTally{Reviewed: 4, Again: 1, Good: 2, Easy: 1}); all != want {
		t.Errorf("all-decks tally = %+v, want %+v", all, want)
	}
	bob, err := f.store.TodayTally(ctx, f.bob.ID, nil, now)
	if err != nil {
		t.Fatal(err)
	}
	if bob != (flash.ReviewTally{}) {
		t.Errorf("bob's tally = %+v, want zero", bob)
	}
}

func queueIDs(q []flash.QueueCard) []int64 {
	ids := make([]int64, len(q))
	for i, qc := range q {
		ids[i] = qc.Card.ID
	}
	return ids
}

func TestDueQueueOrdersReviewsByDueDateAcrossDecks(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	now := time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC)
	older, err := f.store.CreateDeck(ctx, f.alice.ID, "Older", "")
	if err != nil {
		t.Fatal(err)
	}
	newer, err := f.store.CreateDeck(ctx, f.alice.ID, "Newer", "") // listed first by ListDecks
	if err != nil {
		t.Fatal(err)
	}
	overdue, err := f.store.CreateCard(ctx, f.alice.ID, older.ID, flash.CardTypeBasic, "overdue", "x", "")
	if err != nil {
		t.Fatal(err)
	}
	recent, err := f.store.CreateCard(ctx, f.alice.ID, newer.ID, flash.CardTypeBasic, "recent", "x", "")
	if err != nil {
		t.Fatal(err)
	}
	fresh, err := f.store.CreateCard(ctx, f.alice.ID, newer.ID, flash.CardTypeBasic, "fresh", "x", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.GradeCard(ctx, f.alice.ID, overdue.ID, flash.RatingAgain, now.AddDate(0, 0, -10)); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.GradeCard(ctx, f.alice.ID, recent.ID, flash.RatingAgain, now.AddDate(0, 0, -1)); err != nil {
		t.Fatal(err)
	}

	queue, err := f.store.DueQueue(ctx, f.alice.ID, nil, now)
	if err != nil {
		t.Fatal(err)
	}
	// The older deck's card is ten days overdue, so it goes first even though
	// the newer deck is listed first; new cards still come after every review.
	if got, want := queueIDs(queue), []int64{overdue.ID, recent.ID, fresh.ID}; !slices.Equal(got, want) {
		t.Errorf("DueQueue order = %v, want %v (overdue, recent, fresh)", got, want)
	}
	if queue[0].DueAt.IsZero() || !queue[2].DueAt.IsZero() {
		t.Errorf("DueAt: review = %v, new = %v; want set for a review, zero for a new card", queue[0].DueAt, queue[2].DueAt)
	}
}

func TestDueQueueBreaksDueDateTiesByDeckOrder(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	now := time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC)
	past := now.AddDate(0, 0, -2)
	older, err := f.store.CreateDeck(ctx, f.alice.ID, "Older", "")
	if err != nil {
		t.Fatal(err)
	}
	newer, err := f.store.CreateDeck(ctx, f.alice.ID, "Newer", "")
	if err != nil {
		t.Fatal(err)
	}
	a, err := f.store.CreateCard(ctx, f.alice.ID, older.ID, flash.CardTypeBasic, "a", "x", "")
	if err != nil {
		t.Fatal(err)
	}
	b, err := f.store.CreateCard(ctx, f.alice.ID, newer.ID, flash.CardTypeBasic, "b", "x", "")
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []int64{a.ID, b.ID} {
		if _, err := f.store.GradeCard(ctx, f.alice.ID, id, flash.RatingAgain, past); err != nil {
			t.Fatal(err)
		}
	}

	queue, err := f.store.DueQueue(ctx, f.alice.ID, nil, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(queue) != 2 || !queue[0].DueAt.Equal(queue[1].DueAt) {
		t.Fatalf("setup: want two reviews due at the same instant, got %+v", queue)
	}
	if got, want := queueIDs(queue), []int64{b.ID, a.ID}; !slices.Equal(got, want) {
		t.Errorf("tie order = %v, want %v (the newer deck, as ListDecks lists it)", got, want)
	}
}

// queueFixture helpers keep TestQueueFrontAgreesWithDueQueue's cases short.
func qDeck(t *testing.T, f *fixture, name string) flash.Deck {
	t.Helper()
	d, err := f.store.CreateDeck(context.Background(), f.alice.ID, name, "")
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func qCard(t *testing.T, f *fixture, deckID int64, front string) flash.Card {
	t.Helper()
	c, err := f.store.CreateCard(context.Background(), f.alice.ID, deckID, flash.CardTypeBasic, front, "x", "")
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func qGrade(t *testing.T, f *fixture, cardID int64, rating int, at time.Time) {
	t.Helper()
	if _, err := f.store.GradeCard(context.Background(), f.alice.ID, cardID, rating, at); err != nil {
		t.Fatal(err)
	}
}

func qSettings(t *testing.T, f *fixture, deckID int64, newPerDay int, reviewsPerDay *int) {
	t.Helper()
	if _, err := f.store.UpdateDeckSettings(context.Background(), f.alice.ID, deckID, newPerDay, reviewsPerDay); err != nil {
		t.Fatal(err)
	}
}

// TestQueueFrontAgreesWithDueQueue pins the review screen's shortcut to the
// full queue: its head is DueQueue's first card and its count is
// len(DueQueue), across scopes, limits, snoozes and mixed decks.
func TestQueueFrontAgreesWithDueQueue(t *testing.T) {
	now := time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC)
	one := 1
	cases := []struct {
		name     string
		setup    func(t *testing.T, f *fixture) *int64 // returns the scope; nil = every deck
		wantHead string                                // front of the expected head; "" = empty queue
		wantLeft int
	}{
		{"no decks", func(t *testing.T, f *fixture) *int64 { return nil }, "", 0},
		{"new cards only", func(t *testing.T, f *fixture) *int64 {
			d := qDeck(t, f, "A")
			qCard(t, f, d.ID, "n1")
			qCard(t, f, d.ID, "n2")
			return nil
		}, "n1", 2},
		{"review before new, one deck", func(t *testing.T, f *fixture) *int64 {
			d := qDeck(t, f, "A")
			qCard(t, f, d.ID, "new")
			due := qCard(t, f, d.ID, "due")
			qGrade(t, f, due.ID, flash.RatingAgain, now.AddDate(0, 0, -3))
			return &d.ID
		}, "due", 2},
		{"new-card limit", func(t *testing.T, f *fixture) *int64 {
			d := qDeck(t, f, "A")
			qSettings(t, f, d.ID, 1, nil)
			qCard(t, f, d.ID, "n1")
			qCard(t, f, d.ID, "n2")
			qCard(t, f, d.ID, "n3")
			return &d.ID
		}, "n1", 1},
		{"review limit already spent today", func(t *testing.T, f *fixture) *int64 {
			d := qDeck(t, f, "A")
			qSettings(t, f, d.ID, 5, &one)
			a := qCard(t, f, d.ID, "a")
			b := qCard(t, f, d.ID, "b")
			qGrade(t, f, a.ID, flash.RatingAgain, now.AddDate(0, 0, -3))
			qGrade(t, f, b.ID, flash.RatingAgain, now.AddDate(0, 0, -3))
			qGrade(t, f, a.ID, flash.RatingGood, now) // today's one review; b stays due but capped
			qCard(t, f, d.ID, "new")
			return &d.ID
		}, "new", 1},
		{"snoozed deck, scoped", func(t *testing.T, f *fixture) *int64 {
			d := qDeck(t, f, "A")
			qCard(t, f, d.ID, "a")
			if _, err := f.store.SnoozeDeck(context.Background(), f.alice.ID, d.ID, now.AddDate(0, 0, 7)); err != nil {
				t.Fatal(err)
			}
			return &d.ID
		}, "", 0},
		{"snoozed deck skipped in Review all", func(t *testing.T, f *fixture) *int64 {
			a := qDeck(t, f, "A")
			due := qCard(t, f, a.ID, "snoozed-due")
			qGrade(t, f, due.ID, flash.RatingAgain, now.AddDate(0, 0, -3))
			if _, err := f.store.SnoozeDeck(context.Background(), f.alice.ID, a.ID, now.AddDate(0, 0, 7)); err != nil {
				t.Fatal(err)
			}
			b := qDeck(t, f, "B")
			qCard(t, f, b.ID, "b-new")
			return nil
		}, "b-new", 1},
		{"overdue card in an older deck goes first", func(t *testing.T, f *fixture) *int64 {
			older := qDeck(t, f, "Older")
			newer := qDeck(t, f, "Newer")
			o := qCard(t, f, older.ID, "overdue")
			r := qCard(t, f, newer.ID, "recent")
			qCard(t, f, newer.ID, "fresh")
			qGrade(t, f, o.ID, flash.RatingAgain, now.AddDate(0, 0, -10))
			qGrade(t, f, r.ID, flash.RatingAgain, now.AddDate(0, 0, -1))
			return nil
		}, "overdue", 3},
		{"an older deck's review beats a newer deck's new card", func(t *testing.T, f *fixture) *int64 {
			older := qDeck(t, f, "Older")
			newer := qDeck(t, f, "Newer")
			r := qCard(t, f, older.ID, "review")
			qGrade(t, f, r.ID, flash.RatingAgain, now.AddDate(0, 0, -1))
			qCard(t, f, newer.ID, "new")
			return nil
		}, "review", 2},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			ctx := context.Background()
			scope := tc.setup(t, f)

			queue, err := f.store.DueQueue(ctx, f.alice.ID, scope, now)
			if err != nil {
				t.Fatal(err)
			}
			front, err := f.store.QueueFront(ctx, f.alice.ID, scope, now)
			if err != nil {
				t.Fatal(err)
			}
			if front.Remaining != len(queue) || front.Remaining != tc.wantLeft {
				t.Errorf("Remaining = %d, len(DueQueue) = %d, want both %d", front.Remaining, len(queue), tc.wantLeft)
			}
			if front.HasHead != (len(queue) > 0) {
				t.Fatalf("HasHead = %v with len(DueQueue) = %d", front.HasHead, len(queue))
			}
			if !front.HasHead {
				if tc.wantHead != "" {
					t.Errorf("no head, want %q", tc.wantHead)
				}
				return
			}
			got, want := front.Head, queue[0]
			if got.Card.ID != want.Card.ID || got.Deck.ID != want.Deck.ID || got.IsNew != want.IsNew || !got.DueAt.Equal(want.DueAt) {
				t.Errorf("head = %+v, want DueQueue[0] = %+v", got, want)
			}
			if got.Card.Front != tc.wantHead {
				t.Errorf("head front = %q, want %q", got.Card.Front, tc.wantHead)
			}
		})
	}
}
