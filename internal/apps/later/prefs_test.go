package later_test

import (
	"context"
	"errors"
	"math"
	"testing"

	"github.com/iliafrenkel/on-suite/internal/apps/later"
)

func TestPrefsDefaultWhenUnset(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	got, err := f.store.Prefs(ctx, f.alice.ID)
	if err != nil {
		t.Fatalf("Prefs = %v", err)
	}
	if got != later.DefaultPrefs {
		t.Errorf("got %+v, want %+v", got, later.DefaultPrefs)
	}
}

func TestSetPrefsRoundTripsPerUser(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	alicePrefs := later.Prefs{Font: "sans", Size: 5, Width: "wide", Align: "justify"}
	if err := f.store.SetPrefs(ctx, f.alice.ID, alicePrefs); err != nil {
		t.Fatalf("SetPrefs(alice) = %v", err)
	}

	got, err := f.store.Prefs(ctx, f.alice.ID)
	if err != nil {
		t.Fatalf("Prefs(alice) = %v", err)
	}
	if got != alicePrefs {
		t.Errorf("alice prefs = %+v, want %+v", got, alicePrefs)
	}

	// Bob still gets defaults
	bob, err := f.store.Prefs(ctx, f.bob.ID)
	if err != nil {
		t.Fatalf("Prefs(bob) = %v", err)
	}
	if bob != later.DefaultPrefs {
		t.Errorf("bob prefs = %+v, want %+v", bob, later.DefaultPrefs)
	}
}

func TestSetPrefsRejectsInvalid(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	// Store a valid prefs first for alice
	valid := later.Prefs{Font: "serif", Size: 3, Width: "medium", Align: "left"}
	if err := f.store.SetPrefs(ctx, f.alice.ID, valid); err != nil {
		t.Fatalf("initial SetPrefs = %v", err)
	}

	invalidCases := []later.Prefs{
		{Font: "mono", Size: 3, Width: "medium", Align: "left"},    // bad font
		{Font: "serif", Size: 0, Width: "medium", Align: "left"},   // size too low
		{Font: "serif", Size: 6, Width: "medium", Align: "left"},   // size too high
		{Font: "serif", Size: 3, Width: "huge", Align: "left"},     // bad width
		{Font: "serif", Size: 3, Width: "medium", Align: "center"}, // bad align
		{Font: "serif", Size: 3, Width: "medium"},                  // missing align
	}

	for _, p := range invalidCases {
		if err := f.store.SetPrefs(ctx, f.alice.ID, p); !errors.Is(err, later.ErrInvalid) {
			t.Errorf("SetPrefs(%+v) = %v, want ErrInvalid", p, err)
		}
	}

	// Verify stored prefs unchanged
	got, _ := f.store.Prefs(ctx, f.alice.ID)
	if got != valid {
		t.Errorf("after invalid attempts, stored prefs = %+v, want %+v", got, valid)
	}
}

func TestSetProgressClampsAndKeepsState(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	a := f.save(t, "https://example.com/1", "One")
	if a.State != later.StateUnread || a.Progress != 0 {
		t.Fatalf("initial article: state=%q, progress=%v", a.State, a.Progress)
	}

	// Set normal progress
	if err := f.store.SetProgress(ctx, f.alice.ID, a.ID, 0.42); err != nil {
		t.Fatalf("SetProgress(0.42) = %v", err)
	}
	got, _ := f.store.Article(ctx, f.alice.ID, a.ID)
	if got.Progress != 0.42 || got.State != later.StateUnread {
		t.Errorf("after progress 0.42: progress=%v, state=%q", got.Progress, got.State)
	}

	// Clamp above 1
	if err := f.store.SetProgress(ctx, f.alice.ID, a.ID, 1.7); err != nil {
		t.Fatalf("SetProgress(1.7) = %v", err)
	}
	got, _ = f.store.Article(ctx, f.alice.ID, a.ID)
	if got.Progress != 1 {
		t.Errorf("after progress 1.7: progress=%v, want 1", got.Progress)
	}

	// Clamp below 0
	if err := f.store.SetProgress(ctx, f.alice.ID, a.ID, -0.3); err != nil {
		t.Fatalf("SetProgress(-0.3) = %v", err)
	}
	got, _ = f.store.Article(ctx, f.alice.ID, a.ID)
	if got.Progress != 0 {
		t.Errorf("after progress -0.3: progress=%v, want 0", got.Progress)
	}

	// NaN is invalid
	if err := f.store.SetProgress(ctx, f.alice.ID, a.ID, 0.5); err != nil {
		t.Fatalf("reset to valid = %v", err)
	}
	if err := f.store.SetProgress(ctx, f.alice.ID, a.ID, math.NaN()); err == nil {
		t.Errorf("SetProgress(NaN) = nil, want ErrInvalid")
	}
	got, _ = f.store.Article(ctx, f.alice.ID, a.ID)
	if got.Progress != 0.5 {
		t.Errorf("after NaN attempt, progress=%v, want 0.5 (unchanged)", got.Progress)
	}

	// Another user's article returns ErrNotFound
	if err := f.store.SetProgress(ctx, f.bob.ID, a.ID, 0.5); !errors.Is(err, later.ErrNotFound) {
		t.Errorf("SetProgress for other user = %v, want ErrNotFound", err)
	}
}

func TestMoveToUnreadResetsProgress(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	a := f.save(t, "https://example.com/1", "One")

	// Set progress
	if err := f.store.SetProgress(ctx, f.alice.ID, a.ID, 0.6); err != nil {
		t.Fatalf("SetProgress = %v", err)
	}
	got, _ := f.store.Article(ctx, f.alice.ID, a.ID)
	if got.Progress != 0.6 {
		t.Errorf("after progress 0.6: progress=%v", got.Progress)
	}

	// Archive it
	if err := f.store.SetState(ctx, f.alice.ID, a.ID, later.StateArchived); err != nil {
		t.Fatalf("SetState(archived) = %v", err)
	}

	// Move back to unread
	if err := f.store.SetState(ctx, f.alice.ID, a.ID, later.StateUnread); err != nil {
		t.Fatalf("SetState(unread) = %v", err)
	}

	// Progress should be reset to 0
	got, _ = f.store.Article(ctx, f.alice.ID, a.ID)
	if got.State != later.StateUnread || got.Progress != 0 {
		t.Errorf("after move to unread: state=%q, progress=%v; want unread/0", got.State, got.Progress)
	}
}
