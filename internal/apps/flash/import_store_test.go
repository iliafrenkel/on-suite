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

// TestImportDeckTrimsNameAndDescription is #300 item 5: ImportDeck did not
// TrimSpace the deck name/description the way CreateDeck/UpdateDeck do,
// relying only on both parser paths already trimming them. Call it
// directly with a padded name/description (bypassing the parser) to prove
// ImportDeck itself trims defensively.
func TestImportDeckTrimsNameAndDescription(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	d, err := flash.ParseImport(`{"deck": {"name": "D"}, "cards": [{"front": "Q", "back": "A"}]}`, "json")
	if err != nil {
		t.Fatalf("ParseImport: %v", err)
	}

	deck, err := f.store.ImportDeck(ctx, f.alice.ID, "  Padded Deck  ", "  Padded description  ", d.Cards)
	if err != nil {
		t.Fatalf("ImportDeck: %v", err)
	}
	if deck.Name != "Padded Deck" {
		t.Errorf("Name = %q, want %q", deck.Name, "Padded Deck")
	}
	if deck.Description != "Padded description" {
		t.Errorf("Description = %q, want %q", deck.Description, "Padded description")
	}

	stored, err := f.store.DeckByID(ctx, f.alice.ID, deck.ID)
	if err != nil {
		t.Fatalf("DeckByID: %v", err)
	}
	if stored.Name != "Padded Deck" || stored.Description != "Padded description" {
		t.Errorf("stored deck = %+v, want trimmed name/description", stored)
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

func TestImportDeckSetsMediaHashesWithoutFetching(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	d, err := flash.ParseImport(`{
		"deck": {"name": "Imported"},
		"cards": [
			{"front": "Q1", "back": "A1", "image": "https://example.com/cat.jpg", "audio": "https://example.com/meow.mp3"}
		]
	}`, "json")
	if err != nil {
		t.Fatalf("ParseImport: %v", err)
	}

	deck, err := f.store.ImportDeck(ctx, f.alice.ID, d.Name, d.Description, d.Cards)
	if err != nil {
		t.Fatalf("ImportDeck: %v", err)
	}
	cards, err := f.store.ListCards(ctx, f.alice.ID, deck.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(cards) != 1 {
		t.Fatalf("len(cards) = %d, want 1", len(cards))
	}
	c := cards[0]
	if c.ImageHash == nil {
		t.Fatal("ImageHash is nil, want it set")
	}
	if c.AudioHash == nil {
		t.Fatal("AudioHash is nil, want it set")
	}

	img, err := f.store.MediaByHash(ctx, *c.ImageHash)
	if err != nil {
		t.Fatalf("MediaByHash(image): %v", err)
	}
	if img.Cached() {
		t.Error("import must not fetch media eagerly — image should be unfetched")
	}
	if img.SourceURL != "https://example.com/cat.jpg" {
		t.Errorf("SourceURL = %q", img.SourceURL)
	}

	aud, err := f.store.MediaByHash(ctx, *c.AudioHash)
	if err != nil {
		t.Fatalf("MediaByHash(audio): %v", err)
	}
	if aud.Cached() {
		t.Error("import must not fetch media eagerly — audio should be unfetched")
	}
}

func TestImportDeckWithoutMediaLeavesHashesNil(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	d, err := flash.ParseImport(`{"deck": {"name": "D"}, "cards": [{"front": "Q", "back": "A"}]}`, "json")
	if err != nil {
		t.Fatal(err)
	}
	deck, err := f.store.ImportDeck(ctx, f.alice.ID, d.Name, d.Description, d.Cards)
	if err != nil {
		t.Fatal(err)
	}
	cards, err := f.store.ListCards(ctx, f.alice.ID, deck.ID)
	if err != nil {
		t.Fatal(err)
	}
	if cards[0].ImageHash != nil || cards[0].AudioHash != nil {
		t.Errorf("card = %+v, want no media hashes", cards[0])
	}
}

func TestImportDeckSharesOneMediaRowForRepeatedURL(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	d, err := flash.ParseImport(`{
		"deck": {"name": "Imported"},
		"cards": [
			{"front": "Q1", "back": "A1", "image": "https://example.com/flag.jpg"},
			{"front": "Q2", "back": "A2", "image": "https://example.com/flag.jpg"}
		]
	}`, "json")
	if err != nil {
		t.Fatal(err)
	}
	deck, err := f.store.ImportDeck(ctx, f.alice.ID, d.Name, d.Description, d.Cards)
	if err != nil {
		t.Fatal(err)
	}
	cards, err := f.store.ListCards(ctx, f.alice.ID, deck.ID)
	if err != nil {
		t.Fatal(err)
	}
	if *cards[0].ImageHash != *cards[1].ImageHash {
		t.Errorf("same URL produced two different media rows: %q vs %q", *cards[0].ImageHash, *cards[1].ImageHash)
	}
}
