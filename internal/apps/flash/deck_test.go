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

	created, err := f.store.CreateDeck(ctx, f.alice.ID, "  Spanish  ", "Travel phrases", flash.DefaultDeckColor)
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

	if _, err := f.store.CreateDeck(ctx, f.alice.ID, "Spanish", "", flash.DefaultDeckColor); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.CreateDeck(ctx, f.alice.ID, "Spanish", "", flash.DefaultDeckColor); !errors.Is(err, flash.ErrInvalid) {
		t.Errorf("second CreateDeck with the same name = %v, want ErrInvalid", err)
	}
	// Another user may still use the same name.
	if _, err := f.store.CreateDeck(ctx, f.bob.ID, "Spanish", "", flash.DefaultDeckColor); err != nil {
		t.Errorf("bob could not create a deck named the same as alice's: %v", err)
	}
}

func TestUpdateDeck(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	created, err := f.store.CreateDeck(ctx, f.alice.ID, "Original", "", flash.DefaultDeckColor)
	if err != nil {
		t.Fatal(err)
	}
	updated, err := f.store.UpdateDeck(ctx, f.alice.ID, created.ID, "  Renamed  ", "new description", "purple", 5, nil)
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

	created, err := f.store.CreateDeck(ctx, f.alice.ID, "alice's", "", flash.DefaultDeckColor)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.UpdateDeck(ctx, f.bob.ID, created.ID, "hijacked", "", flash.DefaultDeckColor, 20, nil); !errors.Is(err, flash.ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}

// TestUpdateDeckChangesEveryFieldInOneCall is the #297 guarantee's positive
// case: name, description, colour and pace all change together in a single
// UpdateDeck call.
func TestUpdateDeckChangesEveryFieldInOneCall(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	created, err := f.store.CreateDeck(ctx, f.alice.ID, "Original", "old description", flash.DefaultDeckColor)
	if err != nil {
		t.Fatal(err)
	}
	reviews := 30
	updated, err := f.store.UpdateDeck(ctx, f.alice.ID, created.ID, "Renamed", "new description", "purple", 5, &reviews)
	if err != nil {
		t.Fatalf("UpdateDeck: %v", err)
	}
	if updated.Name != "Renamed" || updated.Description != "new description" || updated.Color != "purple" ||
		updated.NewCardsPerDay != 5 || updated.ReviewsPerDay == nil || *updated.ReviewsPerDay != 30 {
		t.Errorf("UpdateDeck = %+v, want every field group changed", updated)
	}
}

// TestUpdateDeckRejectedChangesLeaveEveryFieldUnchanged is the #297
// guarantee's negative case: a rejected update — for any one of the reasons
// below — must not partially apply. It writes nothing at all.
func TestUpdateDeckRejectedChangesLeaveEveryFieldUnchanged(t *testing.T) {
	tests := []struct {
		name           string
		newName        string
		description    string
		color          string
		newCardsPerDay int
		reviewsPerDay  *int
	}{
		{"invalid colour", "New name", "new description", "chartreuse", 5, nil},
		{"invalid pace", "New name", "new description", "purple", -1, nil},
		{"empty name", "   ", "new description", "purple", 5, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture(t)
			ctx := context.Background()
			created, err := f.store.CreateDeck(ctx, f.alice.ID, "Original", "old description", flash.DefaultDeckColor)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := f.store.UpdateDeck(ctx, f.alice.ID, created.ID, tt.newName, tt.description, tt.color, tt.newCardsPerDay, tt.reviewsPerDay); !errors.Is(err, flash.ErrInvalid) {
				t.Fatalf("UpdateDeck(%s) err = %v, want ErrInvalid", tt.name, err)
			}
			unchanged, err := f.store.DeckByID(ctx, f.alice.ID, created.ID)
			if err != nil {
				t.Fatal(err)
			}
			if unchanged.Name != "Original" || unchanged.Description != "old description" ||
				unchanged.Color != flash.DefaultDeckColor || unchanged.NewCardsPerDay != flash.DefaultNewCardsPerDay ||
				unchanged.ReviewsPerDay != nil {
				t.Errorf("a rejected update (%s) changed the deck: %+v", tt.name, unchanged)
			}
		})
	}
}

// TestUpdateDeckRejectsDuplicateNameLeavingDeckUnchanged is the #297
// guarantee's duplicate-name case: it maps the unique-constraint violation
// to ErrInvalid exactly as CreateDeck does, and writes nothing.
func TestUpdateDeckRejectsDuplicateNameLeavingDeckUnchanged(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	if _, err := f.store.CreateDeck(ctx, f.alice.ID, "Taken", "", flash.DefaultDeckColor); err != nil {
		t.Fatal(err)
	}
	created, err := f.store.CreateDeck(ctx, f.alice.ID, "Original", "old description", flash.DefaultDeckColor)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.UpdateDeck(ctx, f.alice.ID, created.ID, "Taken", "new description", "purple", 5, nil); !errors.Is(err, flash.ErrInvalid) {
		t.Fatalf("UpdateDeck with a duplicate name err = %v, want ErrInvalid", err)
	}
	unchanged, err := f.store.DeckByID(ctx, f.alice.ID, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if unchanged.Name != "Original" || unchanged.Description != "old description" || unchanged.Color != flash.DefaultDeckColor {
		t.Errorf("a rejected duplicate-name update changed the deck: %+v", unchanged)
	}
}

// TestDeckOwnerScoping is the security property of this store.
func TestDeckOwnerScoping(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	d, err := f.store.CreateDeck(ctx, f.alice.ID, "alice's", "", flash.DefaultDeckColor)
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
		if _, err := f.store.CreateDeck(ctx, f.alice.ID, name, "", flash.DefaultDeckColor); err != nil {
			t.Fatal(err)
		}
	}
	f.store.SetClock(func() time.Time { return base })
	if _, err := f.store.CreateDeck(ctx, f.bob.ID, "bob's", "", flash.DefaultDeckColor); err != nil {
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

	d, err := f.store.CreateDeck(ctx, f.alice.ID, "doomed", "", flash.DefaultDeckColor)
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

// TestUpdateDeckPace exercises UpdateDeck's pace fields, keeping the
// name/description/colour unchanged — the combined store method's
// replacement for the retired UpdateDeckSettings.
func TestUpdateDeckPace(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	created, err := f.store.CreateDeck(ctx, f.alice.ID, "Spanish", "", flash.DefaultDeckColor)
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
	updated := setDeckPace(t, f.store, f.alice.ID, created.ID, 10, &reviewCap)
	if updated.NewCardsPerDay != 10 {
		t.Errorf("NewCardsPerDay = %d, want 10", updated.NewCardsPerDay)
	}
	if updated.ReviewsPerDay == nil || *updated.ReviewsPerDay != 50 {
		t.Errorf("ReviewsPerDay = %v, want 50", updated.ReviewsPerDay)
	}

	// Setting it back to nil restores "unlimited."
	unlimited := setDeckPace(t, f.store, f.alice.ID, created.ID, 10, nil)
	if unlimited.ReviewsPerDay != nil {
		t.Errorf("ReviewsPerDay after clearing = %v, want nil", unlimited.ReviewsPerDay)
	}
}

func TestUpdateDeckRejectsNegativePaceValues(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	created, err := f.store.CreateDeck(ctx, f.alice.ID, "Spanish", "", flash.DefaultDeckColor)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.UpdateDeck(ctx, f.alice.ID, created.ID, created.Name, created.Description, created.Color, -1, nil); !errors.Is(err, flash.ErrInvalid) {
		t.Errorf("UpdateDeck(new=-1) = %v, want ErrInvalid", err)
	}
	neg := -5
	if _, err := f.store.UpdateDeck(ctx, f.alice.ID, created.ID, created.Name, created.Description, created.Color, 10, &neg); !errors.Is(err, flash.ErrInvalid) {
		t.Errorf("UpdateDeck(reviews=-5) = %v, want ErrInvalid", err)
	}
}

func TestSnoozeAndUnsnoozeDeck(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	created, err := f.store.CreateDeck(ctx, f.alice.ID, "Spanish", "", flash.DefaultDeckColor)
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
	created, err := f.store.CreateDeck(ctx, f.alice.ID, "alice's", "", flash.DefaultDeckColor)
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

func TestNewDeckIsTealByDefault(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	d, err := f.store.CreateDeck(ctx, f.alice.ID, "Spanish", "", flash.DefaultDeckColor)
	if err != nil {
		t.Fatal(err)
	}
	if d.Color != flash.DefaultDeckColor {
		t.Errorf("CreateDeck Color = %q, want %q", d.Color, flash.DefaultDeckColor)
	}
	got, err := f.store.DeckByID(ctx, f.alice.ID, d.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Color != flash.DefaultDeckColor {
		t.Errorf("DeckByID Color = %q, want %q", got.Color, flash.DefaultDeckColor)
	}
}

// TestUpdateDeckColor exercises UpdateDeck's colour field, keeping the
// name/description/pace unchanged — the combined store method's
// replacement for the retired SetDeckColor.
func TestUpdateDeckColor(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	d, err := f.store.CreateDeck(ctx, f.alice.ID, "Spanish", "", flash.DefaultDeckColor)
	if err != nil {
		t.Fatal(err)
	}

	updated := setDeckColor(t, f.store, f.alice.ID, d.ID, "purple")
	if updated.Color != "purple" {
		t.Errorf("Color = %q, want purple", updated.Color)
	}
	decks, err := f.store.ListDecks(ctx, f.alice.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(decks) != 1 || decks[0].Color != "purple" {
		t.Errorf("ListDecks = %+v, want one purple deck", decks)
	}
}

func TestUpdateDeckStoreRejectsUnknownColor(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	d, err := f.store.CreateDeck(ctx, f.alice.ID, "Spanish", "", flash.DefaultDeckColor)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.UpdateDeck(ctx, f.alice.ID, d.ID, d.Name, d.Description, "chartreuse", d.NewCardsPerDay, d.ReviewsPerDay); !errors.Is(err, flash.ErrInvalid) {
		t.Errorf("UpdateDeck(chartreuse) err = %v, want ErrInvalid", err)
	}
}

// TestCreateDeckRejectsUnknownColorWritesNothing is the #313 negative case:
// an invalid colour on create must not insert a deck row at all.
func TestCreateDeckRejectsUnknownColorWritesNothing(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	if _, err := f.store.CreateDeck(ctx, f.alice.ID, "Spanish", "", "chartreuse"); !errors.Is(err, flash.ErrInvalid) {
		t.Errorf("CreateDeck(chartreuse) err = %v, want ErrInvalid", err)
	}
	decks, err := f.store.ListDecks(ctx, f.alice.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(decks) != 0 {
		t.Errorf("a rejected create left %d decks behind, want 0", len(decks))
	}
}

// TestCreateDeckStoresNonDefaultColorInOneWrite is the #313 positive case:
// a non-default colour lands on the deck row right after CreateDeck, with
// no follow-up write needed.
func TestCreateDeckStoresNonDefaultColorInOneWrite(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	d, err := f.store.CreateDeck(ctx, f.alice.ID, "Planets", "", "purple")
	if err != nil {
		t.Fatal(err)
	}
	if d.Color != "purple" {
		t.Errorf("CreateDeck Color = %q, want purple", d.Color)
	}
	got, err := f.store.DeckByID(ctx, f.alice.ID, d.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Color != "purple" {
		t.Errorf("DeckByID Color = %q, want purple", got.Color)
	}
}

func TestValidDeckColor(t *testing.T) {
	for _, c := range flash.DeckColors {
		if !flash.ValidDeckColor(c) {
			t.Errorf("ValidDeckColor(%q) = false, want true", c)
		}
	}
	for _, c := range []string{"", "Teal", "red", "#1D9E75", "teal "} {
		if flash.ValidDeckColor(c) {
			t.Errorf("ValidDeckColor(%q) = true, want false", c)
		}
	}
	if len(flash.DeckColors) != 8 || flash.DeckColors[0] != flash.DefaultDeckColor {
		t.Errorf("DeckColors = %v, want 8 names starting with the default", flash.DeckColors)
	}
}

// setDeckPace is a test-only helper standing in for the retired
// UpdateDeckSettings: it reads the deck's current name/description/colour
// and calls the combined UpdateDeck with only the pace changed, so tests
// that only care about pace stay short.
func setDeckPace(t *testing.T, store *flash.Store, userID, deckID int64, newCardsPerDay int, reviewsPerDay *int) flash.Deck {
	t.Helper()
	d, err := store.DeckByID(context.Background(), userID, deckID)
	if err != nil {
		t.Fatalf("setDeckPace: DeckByID: %v", err)
	}
	updated, err := store.UpdateDeck(context.Background(), userID, deckID, d.Name, d.Description, d.Color, newCardsPerDay, reviewsPerDay)
	if err != nil {
		t.Fatalf("setDeckPace: UpdateDeck: %v", err)
	}
	return updated
}

// setDeckColor is a test-only helper standing in for the retired
// SetDeckColor: it reads the deck's current name/description/pace and calls
// the combined UpdateDeck with only the colour changed.
func setDeckColor(t *testing.T, store *flash.Store, userID, deckID int64, color string) flash.Deck {
	t.Helper()
	d, err := store.DeckByID(context.Background(), userID, deckID)
	if err != nil {
		t.Fatalf("setDeckColor: DeckByID: %v", err)
	}
	updated, err := store.UpdateDeck(context.Background(), userID, deckID, d.Name, d.Description, color, d.NewCardsPerDay, d.ReviewsPerDay)
	if err != nil {
		t.Fatalf("setDeckColor: UpdateDeck: %v", err)
	}
	return updated
}
