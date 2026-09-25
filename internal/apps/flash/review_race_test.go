package flash_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/iliafrenkel/on-suite/internal/apps/flash"
)

// concurrently runs fn n times at once, each in its own goroutine, released
// together by a start barrier so the calls genuinely overlap (#294). It
// returns fn's results in goroutine order.
func concurrently[T any](n int, fn func() T) []T {
	var (
		start = make(chan struct{})
		wg    sync.WaitGroup
		out   = make([]T, n)
	)
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			out[i] = fn()
		}()
	}
	close(start)
	wg.Wait()
	return out
}

// TestGradeCardConcurrentGradesOfANewCardCountItOnce pins #294: every grade
// of one card must see the previous grade's state, so only the first counts
// as new and every later one builds on the schedule before it.
func TestGradeCardConcurrentGradesOfANewCardCountItOnce(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	deck := qDeck(t, f, "Spanish")
	card := qCard(t, f, deck.ID, "hola")
	now := time.Date(2026, 9, 25, 10, 0, 0, 0, time.UTC)

	const n = 20
	errs := concurrently(n, func() error {
		_, err := f.store.GradeCard(ctx, f.alice.ID, card.ID, flash.RatingGood, now)
		return err
	})
	for i, err := range errs {
		if err != nil {
			t.Errorf("grade %d: %v", i, err)
		}
	}

	newCount, reviewCount, err := f.store.DailyCounts(ctx, f.alice.ID, deck.ID, now)
	if err != nil {
		t.Fatal(err)
	}
	if newCount != 1 || reviewCount != n-1 {
		t.Errorf("daily counts = new %d, review %d; want new 1, review %d", newCount, reviewCount, n-1)
	}
	var reps int
	if err := f.db.QueryRowContext(ctx,
		`SELECT reps FROM flash_card_state WHERE user_id = ? AND card_id = ?`, f.alice.ID, card.ID,
	).Scan(&reps); err != nil {
		t.Fatal(err)
	}
	if reps != n {
		t.Errorf("reps = %d, want %d (a concurrent grade overwrote another's schedule)", reps, n)
	}
}

type undoResult struct {
	undone bool
	err    error
}

// TestUndoLastGradeConcurrentUndosUndoOnce pins #294: two undos of the same
// grade racing each other must behave like two sequential ones — the first
// undoes it, the second finds nothing to undo — rather than both reversing
// it (which drives new_count below zero and fails its CHECK).
func TestUndoLastGradeConcurrentUndosUndoOnce(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	deck := qDeck(t, f, "Spanish")
	card := qCard(t, f, deck.ID, "hola")
	now := time.Date(2026, 9, 25, 10, 0, 0, 0, time.UTC)
	qGrade(t, f, card.ID, flash.RatingGood, now)

	results := concurrently(2, func() undoResult {
		_, undone, err := f.store.UndoLastGrade(ctx, f.alice.ID, card.ID, now)
		return undoResult{undone, err}
	})
	undoneCount := 0
	for i, r := range results {
		if r.err != nil {
			t.Errorf("undo %d: %v, want nil (nothing to undo is not an error)", i, r.err)
		}
		if r.undone {
			undoneCount++
		}
	}
	if undoneCount != 1 {
		t.Errorf("%d undos reported undone, want exactly 1", undoneCount)
	}

	newCount, reviewCount, err := f.store.DailyCounts(ctx, f.alice.ID, deck.ID, now)
	if err != nil {
		t.Fatal(err)
	}
	if newCount != 0 || reviewCount != 0 {
		t.Errorf("daily counts = new %d, review %d; want 0, 0", newCount, reviewCount)
	}
	if got := ratingCount(t, f, f.alice.ID, deck.ID, now, "good_count"); got != 0 {
		t.Errorf("good_count = %d, want 0", got)
	}
}

// TestTodayTallyStaysConsistentUnderConcurrentUndos pins #335: Reviewed
// (new_count + review_count) and the rating breakdown must still agree after
// two undos of one grade race each other. With another card's grade on the
// same day, a double undo used to take new_count down twice (no CHECK
// failure, so silently) while the undone rating's column floored at zero,
// leaving the headline one short of its legend.
func TestTodayTallyStaysConsistentUnderConcurrentUndos(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	deck := qDeck(t, f, "Spanish")
	other := qCard(t, f, deck.ID, "adiós")
	card := qCard(t, f, deck.ID, "hola")
	now := time.Date(2026, 9, 25, 10, 0, 0, 0, time.UTC)
	qGrade(t, f, other.ID, flash.RatingAgain, now)
	qGrade(t, f, card.ID, flash.RatingGood, now)

	results := concurrently(2, func() undoResult {
		_, undone, err := f.store.UndoLastGrade(ctx, f.alice.ID, card.ID, now)
		return undoResult{undone, err}
	})
	for i, r := range results {
		if r.err != nil {
			t.Errorf("undo %d: %v", i, r.err)
		}
	}

	tally, err := f.store.TodayTally(ctx, f.alice.ID, &deck.ID, now)
	if err != nil {
		t.Fatal(err)
	}
	if sum := tally.Again + tally.Hard + tally.Good + tally.Easy; tally.Reviewed != sum {
		t.Errorf("tally = %+v: Reviewed %d, rating columns sum to %d", tally, tally.Reviewed, sum)
	}
	if want := (flash.ReviewTally{Reviewed: 1, Again: 1}); tally != want {
		t.Errorf("tally = %+v, want %+v", tally, want)
	}
}
