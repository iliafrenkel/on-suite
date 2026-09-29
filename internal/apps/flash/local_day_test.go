// internal/apps/flash/local_day_test.go
package flash_test

import (
	"context"
	"testing"
	"time"

	"github.com/iliafrenkel/on-suite/internal/apps/flash"
)

// A flash day is the server's local calendar day, not the UTC one (#424).
// main_test.go pins time.Local to Australia/Melbourne (UTC+10 in September),
// where the UTC date changes at 10 am. Each case below sits across that
// boundary, so a UTC day would put its two instants on different days, or
// the same day where they should differ.

// localAt is a wall-clock time on the pinned local zone.
func localAt(month time.Month, day, hour, minute int) time.Time {
	return time.Date(2026, month, day, hour, minute, 0, 0, time.Local)
}

func TestDailyCountsFollowTheLocalDay(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	deck, err := f.store.CreateDeck(ctx, f.alice.ID, "Spanish", "", flash.DefaultDeckColor)
	if err != nil {
		t.Fatal(err)
	}
	card, err := f.store.CreateCard(ctx, f.alice.ID, deck.ID, flash.CardTypeBasic, "a", "b", "")
	if err != nil {
		t.Fatal(err)
	}

	// 8:30 am and 11 am on 25 Sep, local: one day, though 10 am local is
	// midnight UTC between them.
	if _, err := f.store.GradeCard(ctx, f.alice.ID, card.ID, flash.RatingGood, localAt(9, 25, 8, 30)); err != nil {
		t.Fatal(err)
	}
	newCount, _, err := f.store.DailyCounts(ctx, f.alice.ID, deck.ID, localAt(9, 25, 11, 0))
	if err != nil {
		t.Fatal(err)
	}
	if newCount != 1 {
		t.Errorf("new_count at 11 am = %d, want 1: the 8:30 am review was the same local day", newCount)
	}

	// Just after local midnight the budget is fresh.
	newCount, _, err = f.store.DailyCounts(ctx, f.alice.ID, deck.ID, localAt(9, 26, 0, 5))
	if err != nil {
		t.Fatal(err)
	}
	if newCount != 0 {
		t.Errorf("new_count just after local midnight = %d, want 0", newCount)
	}
}

func TestStreakFollowsTheLocalDay(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	deck, err := f.store.CreateDeck(ctx, f.alice.ID, "Spanish", "", flash.DefaultDeckColor)
	if err != nil {
		t.Fatal(err)
	}
	card, err := f.store.CreateCard(ctx, f.alice.ID, deck.ID, flash.CardTypeBasic, "a", "b", "")
	if err != nil {
		t.Fatal(err)
	}

	// Two reviews on one local day that straddle midnight UTC.
	for _, at := range []time.Time{localAt(9, 1, 8, 0), localAt(9, 1, 20, 0)} {
		if _, err := f.store.GradeCard(ctx, f.alice.ID, card.ID, flash.RatingGood, at); err != nil {
			t.Fatal(err)
		}
	}
	streak, err := f.store.Streak(ctx, f.alice.ID, localAt(9, 1, 21, 0))
	if err != nil {
		t.Fatal(err)
	}
	if streak != 1 {
		t.Errorf("streak = %d, want 1: both reviews were on 1 Sep, local", streak)
	}
}

func TestDailyReviewCountsBucketByLocalDay(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	deck, err := f.store.CreateDeck(ctx, f.alice.ID, "Spanish", "", flash.DefaultDeckColor)
	if err != nil {
		t.Fatal(err)
	}
	card, err := f.store.CreateCard(ctx, f.alice.ID, deck.ID, flash.CardTypeBasic, "a", "b", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.GradeCard(ctx, f.alice.ID, card.ID, flash.RatingGood, localAt(9, 25, 8, 30)); err != nil {
		t.Fatal(err)
	}

	days, err := f.store.DailyReviewCounts(ctx, f.alice.ID, 2, localAt(9, 25, 12, 0))
	if err != nil {
		t.Fatal(err)
	}
	if len(days) != 2 {
		t.Fatalf("got %d days, want 2", len(days))
	}
	last := days[1]
	if !last.Day.Equal(localAt(9, 25, 0, 0)) {
		t.Errorf("last day = %v, want local midnight of 25 Sep", last.Day)
	}
	if last.Count != 1 {
		t.Errorf("25 Sep count = %d, want 1", last.Count)
	}
	if days[0].Count != 0 {
		t.Errorf("24 Sep count = %d, want 0", days[0].Count)
	}
}
