package flash_test

import (
	"context"
	"errors"
	"testing"

	"github.com/iliafrenkel/on-suite/internal/apps/flash"
)

func TestImportDeckCreatesDeckCardsAndTags(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	d, err := flash.ParseImport(`{
		"deck": {"name": "Imported", "description": "from a test"},
		"cards": [
			{"type": "basic", "front": "Q1", "back": "A1", "tags": ["x", "y"]},
			{"type": "cloze", "front": "The {{c1::answer}}."}
		]
	}`, "json")
	if err != nil {
		t.Fatalf("ParseImport: %v", err)
	}

	deck, err := f.store.ImportDeck(ctx, f.alice.ID, d.Name, d.Description, d.Cards)
	if err != nil {
		t.Fatalf("ImportDeck: %v", err)
	}
	if deck.ID == 0 {
		t.Fatal("ImportDeck returned id 0")
	}
	if deck.NewCardsPerDay != flash.DefaultNewCardsPerDay {
		t.Errorf("NewCardsPerDay = %d, want %d", deck.NewCardsPerDay, flash.DefaultNewCardsPerDay)
	}

	cards, err := f.store.ListCards(ctx, f.alice.ID, deck.ID)
	if err != nil {
		t.Fatalf("ListCards: %v", err)
	}
	if len(cards) != 2 {
		t.Fatalf("len(cards) = %d, want 2", len(cards))
	}

	var basic flash.Card
	for _, c := range cards {
		if c.CardType == flash.CardTypeBasic {
			basic = c
		}
	}
	if basic.ID == 0 {
		t.Fatal("no basic card found")
	}
	tags, err := f.store.TagsForCard(ctx, f.alice.ID, basic.ID)
	if err != nil {
		t.Fatalf("TagsForCard: %v", err)
	}
	if len(tags) != 2 {
		t.Fatalf("len(tags) = %d, want 2", len(tags))
	}
}

func TestImportDeckRejectsDuplicateName(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	if _, err := f.store.CreateDeck(ctx, f.alice.ID, "Existing", ""); err != nil {
		t.Fatal(err)
	}

	d, err := flash.ParseImport(`{"deck": {"name": "Existing"}, "cards": [{"front": "Q", "back": "A"}]}`, "json")
	if err != nil {
		t.Fatalf("ParseImport: %v", err)
	}

	_, err = f.store.ImportDeck(ctx, f.alice.ID, d.Name, d.Description, d.Cards)
	if !errors.Is(err, flash.ErrInvalid) {
		t.Fatalf("ImportDeck err = %v, want ErrInvalid", err)
	}

	decks, err := f.store.ListDecks(ctx, f.alice.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(decks) != 1 {
		t.Errorf("len(decks) = %d, want 1 (the original, nothing partially imported)", len(decks))
	}
}
