// internal/apps/flash/deck_test.go
package flash_test

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/iliafrenkel/on-suite/internal/apps/flash"
	"github.com/iliafrenkel/on-suite/internal/apptest"
	"github.com/iliafrenkel/on-suite/internal/platform/auth"
	"github.com/iliafrenkel/on-suite/internal/platform/db"
)

// fixture is a migrated database with two users, so every owner-scoping test
// has somebody else to be confused with.
type fixture struct {
	store *flash.Store
	db    *sql.DB
	alice auth.User
	bob   auth.User
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	ctx := context.Background()

	handle, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = handle.Close() })

	migrations, err := db.Collect(auth.Namespace, auth.Migrations())
	if err != nil {
		t.Fatal(err)
	}
	appMigrations, err := db.Collect(flash.ID, flash.Migrations())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Apply(ctx, handle, append(migrations, appMigrations...)); err != nil {
		t.Fatal(err)
	}

	users := auth.NewStore(handle)
	alice, err := users.CreateUser(ctx, "alice", apptest.PasswordHash, true)
	if err != nil {
		t.Fatal(err)
	}
	bob, err := users.CreateUser(ctx, "bob", apptest.PasswordHash, false)
	if err != nil {
		t.Fatal(err)
	}
	return &fixture{store: flash.NewStore(handle), db: handle, alice: alice, bob: bob}
}

func TestCreateAndFetchDeck(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	created, err := f.store.CreateDeck(ctx, f.alice.ID, "  Spanish  ", "Travel phrases")
	if err != nil {
		t.Fatalf("CreateDeck: %v", err)
	}
	if created.ID == 0 {
		t.Error("CreateDeck returned id 0")
	}
	if created.Name != "Spanish" {
		t.Errorf("Name = %q; whitespace should be trimmed", created.Name)
	}
	if created.CreatedAt.IsZero() {
		t.Error("CreatedAt is zero")
	}

	got, err := f.store.DeckByID(ctx, f.alice.ID, created.ID)
	if err != nil {
		t.Fatalf("DeckByID: %v", err)
	}
	if got.Description != "Travel phrases" {
		t.Errorf("round trip lost data: %+v", got)
	}
}

func TestCreateDeckRejectsDuplicateName(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	if _, err := f.store.CreateDeck(ctx, f.alice.ID, "Spanish", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.CreateDeck(ctx, f.alice.ID, "Spanish", ""); !errors.Is(err, flash.ErrInvalid) {
		t.Errorf("second CreateDeck with the same name = %v, want ErrInvalid", err)
	}
	// Another user may still use the same name.
	if _, err := f.store.CreateDeck(ctx, f.bob.ID, "Spanish", ""); err != nil {
		t.Errorf("bob could not create a deck named the same as alice's: %v", err)
	}
}

func TestUpdateDeck(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	created, err := f.store.CreateDeck(ctx, f.alice.ID, "Original", "")
	if err != nil {
		t.Fatal(err)
	}
	updated, err := f.store.UpdateDeck(ctx, f.alice.ID, created.ID, "  Renamed  ", "new description")
	if err != nil {
		t.Fatalf("UpdateDeck: %v", err)
	}
	if updated.Name != "Renamed" || updated.Description != "new description" {
		t.Errorf("UpdateDeck = %+v", updated)
	}
	if !updated.CreatedAt.Equal(created.CreatedAt) {
		t.Errorf("CreatedAt changed: got %v, want %v", updated.CreatedAt, created.CreatedAt)
	}
}

func TestUpdateDeckRejectsSomeoneElsesDeck(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	created, err := f.store.CreateDeck(ctx, f.alice.ID, "alice's", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.UpdateDeck(ctx, f.bob.ID, created.ID, "hijacked", ""); !errors.Is(err, flash.ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}

// TestDeckOwnerScoping is the security property of this store.
func TestDeckOwnerScoping(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	d, err := f.store.CreateDeck(ctx, f.alice.ID, "alice's", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.DeckByID(ctx, f.bob.ID, d.ID); !errors.Is(err, flash.ErrNotFound) {
		t.Errorf("DeckByID as another user = %v, want ErrNotFound", err)
	}
	if err := f.store.DeleteDeck(ctx, f.bob.ID, d.ID); !errors.Is(err, flash.ErrNotFound) {
		t.Errorf("DeleteDeck as another user = %v, want ErrNotFound", err)
	}
	if _, err := f.store.DeckByID(ctx, f.alice.ID, d.ID); err != nil {
		t.Errorf("the owner lost access: %v", err)
	}
}

func TestListDecksIsNewestFirstAndOwnerScoped(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	base := time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC)
	for i, name := range []string{"first", "second", "third"} {
		f.store.SetClock(func() time.Time { return base.Add(time.Duration(i) * time.Minute) })
		if _, err := f.store.CreateDeck(ctx, f.alice.ID, name, ""); err != nil {
			t.Fatal(err)
		}
	}
	f.store.SetClock(func() time.Time { return base })
	if _, err := f.store.CreateDeck(ctx, f.bob.ID, "bob's", ""); err != nil {
		t.Fatal(err)
	}

	got, err := f.store.ListDecks(ctx, f.alice.ID)
	if err != nil {
		t.Fatalf("ListDecks: %v", err)
	}
	var names []string
	for _, d := range got {
		names = append(names, d.Name)
	}
	if want := "third,second,first"; strings.Join(names, ",") != want {
		t.Errorf("ListDecks order = %q, want %q", strings.Join(names, ","), want)
	}
}

func TestDeleteDeck(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	d, err := f.store.CreateDeck(ctx, f.alice.ID, "doomed", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := f.store.DeleteDeck(ctx, f.alice.ID, d.ID); err != nil {
		t.Fatalf("DeleteDeck: %v", err)
	}
	if _, err := f.store.DeckByID(ctx, f.alice.ID, d.ID); !errors.Is(err, flash.ErrNotFound) {
		t.Error("the deck survived deletion")
	}
	if err := f.store.DeleteDeck(ctx, f.alice.ID, d.ID); !errors.Is(err, flash.ErrNotFound) {
		t.Errorf("second DeleteDeck = %v, want ErrNotFound", err)
	}
}

func TestUpdateDeckSettings(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	created, err := f.store.CreateDeck(ctx, f.alice.ID, "Spanish", "")
	if err != nil {
		t.Fatal(err)
	}
	if created.NewCardsPerDay != 20 {
		t.Errorf("default NewCardsPerDay = %d, want 20", created.NewCardsPerDay)
	}
	if created.ReviewsPerDay != nil {
		t.Errorf("default ReviewsPerDay = %v, want nil (unlimited)", created.ReviewsPerDay)
	}

	reviewCap := 50
	updated, err := f.store.UpdateDeckSettings(ctx, f.alice.ID, created.ID, 10, &reviewCap)
	if err != nil {
		t.Fatalf("UpdateDeckSettings: %v", err)
	}
	if updated.NewCardsPerDay != 10 {
		t.Errorf("NewCardsPerDay = %d, want 10", updated.NewCardsPerDay)
	}
	if updated.ReviewsPerDay == nil || *updated.ReviewsPerDay != 50 {
		t.Errorf("ReviewsPerDay = %v, want 50", updated.ReviewsPerDay)
	}

	// Setting it back to nil restores "unlimited."
	unlimited, err := f.store.UpdateDeckSettings(ctx, f.alice.ID, created.ID, 10, nil)
	if err != nil {
		t.Fatal(err)
	}
	if unlimited.ReviewsPerDay != nil {
		t.Errorf("ReviewsPerDay after clearing = %v, want nil", unlimited.ReviewsPerDay)
	}
}

func TestUpdateDeckSettingsRejectsNegativeValues(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	created, err := f.store.CreateDeck(ctx, f.alice.ID, "Spanish", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.UpdateDeckSettings(ctx, f.alice.ID, created.ID, -1, nil); !errors.Is(err, flash.ErrInvalid) {
		t.Errorf("UpdateDeckSettings(new=-1) = %v, want ErrInvalid", err)
	}
	neg := -5
	if _, err := f.store.UpdateDeckSettings(ctx, f.alice.ID, created.ID, 10, &neg); !errors.Is(err, flash.ErrInvalid) {
		t.Errorf("UpdateDeckSettings(reviews=-5) = %v, want ErrInvalid", err)
	}
}

func TestUpdateDeckSettingsRejectsSomeoneElsesDeck(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	created, err := f.store.CreateDeck(ctx, f.alice.ID, "alice's", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.UpdateDeckSettings(ctx, f.bob.ID, created.ID, 10, nil); !errors.Is(err, flash.ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}

func TestSnoozeAndUnsnoozeDeck(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	created, err := f.store.CreateDeck(ctx, f.alice.ID, "Spanish", "")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC)
	until := now.AddDate(0, 0, 7)

	snoozed, err := f.store.SnoozeDeck(ctx, f.alice.ID, created.ID, until)
	if err != nil {
		t.Fatalf("SnoozeDeck: %v", err)
	}
	if !snoozed.IsSnoozed(now) {
		t.Error("IsSnoozed(now) = false right after snoozing until a week from now")
	}
	if snoozed.IsSnoozed(until.AddDate(0, 0, 1)) {
		t.Error("IsSnoozed should be false once the snooze period has passed")
	}

	unsnoozed, err := f.store.UnsnoozeDeck(ctx, f.alice.ID, created.ID)
	if err != nil {
		t.Fatalf("UnsnoozeDeck: %v", err)
	}
	if unsnoozed.IsSnoozed(now) {
		t.Error("IsSnoozed(now) = true after unsnoozing")
	}
}

func TestSnoozeDeckRejectsSomeoneElsesDeck(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	created, err := f.store.CreateDeck(ctx, f.alice.ID, "alice's", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.SnoozeDeck(ctx, f.bob.ID, created.ID, time.Now()); !errors.Is(err, flash.ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}

func TestValidateDeck(t *testing.T) {
	tests := []struct {
		name    string
		deck    string
		wantErr bool
	}{
		{"ordinary", "Spanish", false},
		{"empty name", "", true},
		{"whitespace-only name", "   ", true},
		{"name at the limit", strings.Repeat("a", flash.MaxDeckNameRunes), false},
		{"name over the limit", strings.Repeat("a", flash.MaxDeckNameRunes+1), true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := flash.ValidateDeck(tt.deck, "")
			if tt.wantErr && !errors.Is(err, flash.ErrInvalid) {
				t.Errorf("ValidateDeck = %v, want ErrInvalid", err)
			}
			if !tt.wantErr && err != nil {
				t.Errorf("ValidateDeck rejected a valid deck: %v", err)
			}
		})
	}
}
