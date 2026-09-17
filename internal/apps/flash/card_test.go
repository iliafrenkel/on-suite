// internal/apps/flash/card_test.go
package flash_test

import (
	"context"
	"errors"
	"testing"

	"github.com/iliafrenkel/on-suite/internal/apps/flash"
)

func TestCreateAndFetchCard(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	deck, err := f.store.CreateDeck(ctx, f.alice.ID, "Spanish", "")
	if err != nil {
		t.Fatal(err)
	}

	created, err := f.store.CreateCard(ctx, f.alice.ID, deck.ID, flash.CardTypeBasic, "hola", "hello", "greeting")
	if err != nil {
		t.Fatalf("CreateCard: %v", err)
	}
	if created.ID == 0 {
		t.Error("CreateCard returned id 0")
	}

	got, err := f.store.CardByID(ctx, f.alice.ID, deck.ID, created.ID)
	if err != nil {
		t.Fatalf("CardByID: %v", err)
	}
	if got.Front != "hola" || got.Back != "hello" || got.Notes != "greeting" {
		t.Errorf("round trip lost data: %+v", got)
	}
}

func TestCreateCardRejectsUnknownDeck(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	if _, err := f.store.CreateCard(ctx, f.alice.ID, 999999, flash.CardTypeBasic, "a", "b", ""); !errors.Is(err, flash.ErrNotFound) {
		t.Errorf("CreateCard into a missing deck = %v, want ErrNotFound", err)
	}
}

func TestCreateCardRejectsSomeoneElsesDeck(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	deck, err := f.store.CreateDeck(ctx, f.alice.ID, "alice's", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.CreateCard(ctx, f.bob.ID, deck.ID, flash.CardTypeBasic, "a", "b", ""); !errors.Is(err, flash.ErrNotFound) {
		t.Errorf("CreateCard into another user's deck = %v, want ErrNotFound", err)
	}
}

func TestValidateCard(t *testing.T) {
	tests := []struct {
		name     string
		cardType string
		front    string
		back     string
		wantErr  bool
	}{
		{"ordinary basic", flash.CardTypeBasic, "q", "a", false},
		{"basic missing back", flash.CardTypeBasic, "q", "", true},
		{"basic missing front", flash.CardTypeBasic, "", "a", true},
		{"unknown type", "essay", "q", "a", true},
		{"cloze needs markers", flash.CardTypeCloze, "no markers here", "", true},
		{"ordinary cloze", flash.CardTypeCloze, "The capital of France is {{c1::Paris}}.", "", false},
		{"cloze must not carry a back", flash.CardTypeCloze, "{{c1::x}}", "unexpected", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := flash.ValidateCard(tt.cardType, tt.front, tt.back)
			if tt.wantErr && !errors.Is(err, flash.ErrInvalid) {
				t.Errorf("ValidateCard = %v, want ErrInvalid", err)
			}
			if !tt.wantErr && err != nil {
				t.Errorf("ValidateCard rejected a valid card: %v", err)
			}
		})
	}
}

func TestListCardsIsScopedToItsDeck(t *testing.T) {
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
	if _, err := f.store.CreateCard(ctx, f.alice.ID, deckA.ID, flash.CardTypeBasic, "a1", "x", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.CreateCard(ctx, f.alice.ID, deckB.ID, flash.CardTypeBasic, "b1", "x", ""); err != nil {
		t.Fatal(err)
	}

	got, err := f.store.ListCards(ctx, f.alice.ID, deckA.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Front != "a1" {
		t.Errorf("ListCards(deckA) = %+v, want exactly a1", got)
	}
}

// TestCardOwnerScoping mirrors TestDeckOwnerScoping: bob must not reach
// alice's card even by guessing the right deck id.
func TestCardOwnerScoping(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	deck, err := f.store.CreateDeck(ctx, f.alice.ID, "alice's", "")
	if err != nil {
		t.Fatal(err)
	}
	card, err := f.store.CreateCard(ctx, f.alice.ID, deck.ID, flash.CardTypeBasic, "secret", "x", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.CardByID(ctx, f.bob.ID, deck.ID, card.ID); !errors.Is(err, flash.ErrNotFound) {
		t.Errorf("CardByID as another user = %v, want ErrNotFound", err)
	}
	if err := f.store.DeleteCard(ctx, f.bob.ID, deck.ID, card.ID); !errors.Is(err, flash.ErrNotFound) {
		t.Errorf("DeleteCard as another user = %v, want ErrNotFound", err)
	}
}

func TestUpdateCard(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	deck, err := f.store.CreateDeck(ctx, f.alice.ID, "test", "")
	if err != nil {
		t.Fatal(err)
	}

	// Create a card
	created, err := f.store.CreateCard(ctx, f.alice.ID, deck.ID, flash.CardTypeBasic, "original q", "original a", "original note")
	if err != nil {
		t.Fatal(err)
	}
	originalCreatedAt := created.CreatedAt

	// Update the card
	updated, err := f.store.UpdateCard(ctx, f.alice.ID, deck.ID, created.ID, flash.CardTypeBasic, "new q", "new a", "new note")
	if err != nil {
		t.Fatalf("UpdateCard: %v", err)
	}
	if updated.Front != "new q" || updated.Back != "new a" || updated.Notes != "new note" {
		t.Errorf("UpdateCard = %+v, fields not updated", updated)
	}
	if !updated.CreatedAt.Equal(originalCreatedAt) {
		t.Errorf("CreatedAt changed: got %v, want %v", updated.CreatedAt, originalCreatedAt)
	}

	// Verify fresh fetch also has new values
	fetched, err := f.store.CardByID(ctx, f.alice.ID, deck.ID, created.ID)
	if err != nil {
		t.Fatalf("CardByID: %v", err)
	}
	if fetched.Front != "new q" || fetched.Back != "new a" || fetched.Notes != "new note" {
		t.Errorf("CardByID after update = %+v, fields not persisted", fetched)
	}
}

func TestUpdateCardRejectsSomeoneElsesDeck(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	// Create deck and card as alice
	deck, err := f.store.CreateDeck(ctx, f.alice.ID, "alice's", "")
	if err != nil {
		t.Fatal(err)
	}
	created, err := f.store.CreateCard(ctx, f.alice.ID, deck.ID, flash.CardTypeBasic, "original", "x", "")
	if err != nil {
		t.Fatal(err)
	}

	// Try to update as bob
	if _, err := f.store.UpdateCard(ctx, f.bob.ID, deck.ID, created.ID, flash.CardTypeBasic, "hijacked", "y", ""); !errors.Is(err, flash.ErrNotFound) {
		t.Errorf("UpdateCard as another user = %v, want ErrNotFound", err)
	}

	// Verify alice's card is unchanged
	fetched, err := f.store.CardByID(ctx, f.alice.ID, deck.ID, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if fetched.Front != "original" || fetched.Back != "x" {
		t.Errorf("card was modified: %+v", fetched)
	}
}

func TestUpdateCardRejectsInvalidInput(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	deck, err := f.store.CreateDeck(ctx, f.alice.ID, "test", "")
	if err != nil {
		t.Fatal(err)
	}
	created, err := f.store.CreateCard(ctx, f.alice.ID, deck.ID, flash.CardTypeBasic, "original q", "original a", "")
	if err != nil {
		t.Fatal(err)
	}

	// Try to update with empty back for basic card type (invalid)
	if _, err := f.store.UpdateCard(ctx, f.alice.ID, deck.ID, created.ID, flash.CardTypeBasic, "q", "", ""); !errors.Is(err, flash.ErrInvalid) {
		t.Errorf("UpdateCard with invalid input = %v, want ErrInvalid", err)
	}

	// Verify card is unchanged
	fetched, err := f.store.CardByID(ctx, f.alice.ID, deck.ID, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if fetched.Front != "original q" || fetched.Back != "original a" {
		t.Errorf("card was modified: %+v", fetched)
	}
}

func TestDeletingADeckRemovesItsCards(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	deck, err := f.store.CreateDeck(ctx, f.alice.ID, "doomed", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.CreateCard(ctx, f.alice.ID, deck.ID, flash.CardTypeBasic, "a", "b", ""); err != nil {
		t.Fatal(err)
	}
	if err := f.store.DeleteDeck(ctx, f.alice.ID, deck.ID); err != nil {
		t.Fatal(err)
	}
	got, err := f.store.ListCards(ctx, f.alice.ID, deck.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("%d cards survived their deck", len(got))
	}
}
