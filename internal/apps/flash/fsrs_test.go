package flash

import (
	"reflect"
	"testing"
	"time"
)

func TestNewCardScheduleIsUnreviewed(t *testing.T) {
	now := time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC)
	c := newCardSchedule(now)
	if c.State != "new" {
		t.Errorf("State = %q, want %q", c.State, "new")
	}
	if !c.DueAt.Equal(now) {
		t.Errorf("DueAt = %v, want %v", c.DueAt, now)
	}
	if c.Reps != 0 {
		t.Errorf("Reps = %d, want 0", c.Reps)
	}
	if !c.LastReviewAt.IsZero() {
		t.Errorf("LastReviewAt = %v, want zero", c.LastReviewAt)
	}
}

func TestGradeCardRejectsInvalidRating(t *testing.T) {
	now := time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC)
	c := newCardSchedule(now)
	for _, rating := range []int{0, 5, -1} {
		if _, _, err := gradeCard(c, rating, now); err == nil {
			t.Errorf("gradeCard(rating=%d) = nil error, want an error", rating)
		}
	}
}

// TestGradeThenRollbackRestoresOriginalSchedule is the property that matters
// here: our wrapper is correctly wired to go-fsrs's own Next/Rollback pair,
// for every one of the four ratings. It deliberately does not assert on
// go-fsrs's internal stability/difficulty math (that is the library's own
// test suite's job) — only that grading a card and then rolling that single
// grade back reproduces the exact schedule that was there before.
func TestGradeThenRollbackRestoresOriginalSchedule(t *testing.T) {
	now := time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC)
	original := newCardSchedule(now)

	for _, rating := range []int{RatingAgain, RatingHard, RatingGood, RatingEasy} {
		graded, log, err := gradeCard(original, rating, now)
		if err != nil {
			t.Fatalf("gradeCard(rating=%d): %v", rating, err)
		}
		if graded.Reps != original.Reps+1 {
			t.Errorf("gradeCard(rating=%d): Reps = %d, want %d", rating, graded.Reps, original.Reps+1)
		}
		if graded.DueAt.Before(now) {
			t.Errorf("gradeCard(rating=%d): DueAt = %v, want at or after %v", rating, graded.DueAt, now)
		}

		restored, err := rollbackCard(graded, log)
		if err != nil {
			t.Fatalf("rollbackCard(rating=%d): %v", rating, err)
		}
		if !reflect.DeepEqual(restored, original) {
			t.Errorf("rollbackCard(rating=%d) = %+v, want original %+v", rating, restored, original)
		}
	}
}

// TestGradeTwiceThenRollbackRestoresTheIntermediateMemory proves rollback
// only undoes the most recent grade, not the whole history. It does not
// assert DueAt equality against the intermediate schedule: go-fsrs's own
// Rollback deliberately sets the reverted card's Due to the timestamp of
// the review being undone (log.Review), not the due date the card carried
// immediately before that review — a card being rolled back was, by
// definition, already due at that review's time, and go-fsrs has no way to
// recover what its due date was before it became overdue. Reps, State,
// Stability, Difficulty, and LastReviewAt are exactly recoverable and are
// what this test checks; only a rollback of a card's very first-ever
// review (from New, see TestGradeThenRollbackRestoresOriginalSchedule)
// reconstructs DueAt exactly too, because New's own rollback branch uses
// log.Due (the pre-review due date) rather than log.Review. (This
// distinction was found by actually running go-fsrs during plan
// validation, not derived from its docs — the library's rollback.go
// switches behavior by State for exactly this reason.)
func TestGradeTwiceThenRollbackRestoresTheIntermediateMemory(t *testing.T) {
	now := time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC)
	later := now.Add(24 * time.Hour)
	original := newCardSchedule(now)

	firstGrade, _, err := gradeCard(original, RatingGood, now)
	if err != nil {
		t.Fatalf("first gradeCard: %v", err)
	}
	secondGrade, secondLog, err := gradeCard(firstGrade, RatingGood, later)
	if err != nil {
		t.Fatalf("second gradeCard: %v", err)
	}

	restored, err := rollbackCard(secondGrade, secondLog)
	if err != nil {
		t.Fatalf("rollbackCard: %v", err)
	}
	if restored.Reps != firstGrade.Reps {
		t.Errorf("Reps = %d, want %d", restored.Reps, firstGrade.Reps)
	}
	if restored.State != firstGrade.State {
		t.Errorf("State = %q, want %q", restored.State, firstGrade.State)
	}
	if restored.Stability != firstGrade.Stability {
		t.Errorf("Stability = %v, want %v", restored.Stability, firstGrade.Stability)
	}
	if restored.Difficulty != firstGrade.Difficulty {
		t.Errorf("Difficulty = %v, want %v", restored.Difficulty, firstGrade.Difficulty)
	}
	if !restored.LastReviewAt.Equal(firstGrade.LastReviewAt) {
		t.Errorf("LastReviewAt = %v, want %v", restored.LastReviewAt, firstGrade.LastReviewAt)
	}
}
