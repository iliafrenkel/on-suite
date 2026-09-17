//
// This is the only file in the codebase that imports go-fsrs. Every other
// file in this package works with cardSchedule and reviewLog, this file's
// own plain-Go types — internal/arch/arch_test.go's TestFSRSIsContained
// enforces that a second importer never sneaks in, the same way
// TestReadabilityIsContained does for go-readability in internal/apps/reader.
package flash

import (
	"fmt"
	"time"

	"github.com/open-spaced-repetition/go-fsrs/v4"
)

// scheduler runs FSRS with the library's default weights. There is no
// per-user parameter optimization in F2 — see the design doc's "Out of
// scope" section.
var scheduler = fsrs.NewFSRS(fsrs.DefaultParam())

// The four ratings a review session can apply, matching go-fsrs's own
// Rating enum values exactly (Again=1 .. Easy=4), so no translation table
// is needed anywhere that passes a plain int across this file's boundary.
const (
	RatingAgain = 1
	RatingHard  = 2
	RatingGood  = 3
	RatingEasy  = 4
)

// cardSchedule is this package's own representation of a card's FSRS
// scheduling state, decoupled from go-fsrs's Card type.
type cardSchedule struct {
	State          string // "new" | "learning" | "review" | "relearning"
	DueAt          time.Time
	Stability      float64
	Difficulty     float64
	ScheduledDays  uint64
	Reps           uint64
	Lapses         uint64
	RemainingSteps int
	LastReviewAt   time.Time // zero value: never reviewed
}

// reviewLog is this package's own representation of go-fsrs's ReviewLog: the
// undo buffer produced by one grade, consumed by rollbackCard to reconstruct
// the schedule immediately before that grade.
type reviewLog struct {
	Rating         int
	Due            time.Time
	ScheduledDays  uint64
	Review         time.Time
	State          string
	Stability      float64
	Difficulty     float64
	RemainingSteps int
}

// newCardSchedule is the state of a card that has never been reviewed.
func newCardSchedule(now time.Time) cardSchedule {
	return fromFSRSCard(fsrs.NewCard(now))
}

// gradeCard applies rating (RatingAgain..RatingEasy) to current's schedule at
// now, returning the new schedule and the log needed to undo this one grade
// via rollbackCard.
func gradeCard(current cardSchedule, rating int, now time.Time) (cardSchedule, reviewLog, error) {
	info, err := scheduler.Next(toFSRSCard(current), now, fsrs.Rating(rating))
	if err != nil {
		return cardSchedule{}, reviewLog{}, fmt.Errorf("flash: grade card: %w", err)
	}
	return fromFSRSCard(info.Card), fromFSRSLog(info.ReviewLog), nil
}

// rollbackCard reverses the single grade described by log, reconstructing
// current's schedule as it was immediately before that grade was applied.
func rollbackCard(current cardSchedule, log reviewLog) (cardSchedule, error) {
	reverted, err := scheduler.Rollback(toFSRSCard(current), toFSRSLog(log))
	if err != nil {
		return cardSchedule{}, fmt.Errorf("flash: rollback card: %w", err)
	}
	return fromFSRSCard(reverted), nil
}

func toFSRSState(s string) fsrs.State {
	switch s {
	case "learning":
		return fsrs.Learning
	case "review":
		return fsrs.Review
	case "relearning":
		return fsrs.Relearning
	default:
		return fsrs.New
	}
}

func fromFSRSState(s fsrs.State) string {
	switch s {
	case fsrs.Learning:
		return "learning"
	case fsrs.Review:
		return "review"
	case fsrs.Relearning:
		return "relearning"
	default:
		return "new"
	}
}

func toFSRSCard(c cardSchedule) fsrs.Card {
	return fsrs.Card{
		Due:            c.DueAt,
		Stability:      c.Stability,
		Difficulty:     c.Difficulty,
		ScheduledDays:  c.ScheduledDays,
		Reps:           c.Reps,
		Lapses:         c.Lapses,
		State:          toFSRSState(c.State),
		LastReview:     c.LastReviewAt,
		RemainingSteps: c.RemainingSteps,
	}
}

func fromFSRSCard(c fsrs.Card) cardSchedule {
	return cardSchedule{
		State:          fromFSRSState(c.State),
		DueAt:          c.Due,
		Stability:      c.Stability,
		Difficulty:     c.Difficulty,
		ScheduledDays:  c.ScheduledDays,
		Reps:           c.Reps,
		Lapses:         c.Lapses,
		RemainingSteps: c.RemainingSteps,
		LastReviewAt:   c.LastReview,
	}
}

func toFSRSLog(l reviewLog) fsrs.ReviewLog {
	return fsrs.ReviewLog{
		Rating:         fsrs.Rating(l.Rating),
		Due:            l.Due,
		ScheduledDays:  l.ScheduledDays,
		Review:         l.Review,
		State:          toFSRSState(l.State),
		Stability:      l.Stability,
		Difficulty:     l.Difficulty,
		RemainingSteps: l.RemainingSteps,
	}
}

func fromFSRSLog(l fsrs.ReviewLog) reviewLog {
	return reviewLog{
		Rating:         int(l.Rating),
		Due:            l.Due,
		ScheduledDays:  l.ScheduledDays,
		Review:         l.Review,
		State:          fromFSRSState(l.State),
		Stability:      l.Stability,
		Difficulty:     l.Difficulty,
		RemainingSteps: l.RemainingSteps,
	}
}
