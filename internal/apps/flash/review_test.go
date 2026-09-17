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
