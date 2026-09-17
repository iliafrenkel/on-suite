// internal/apps/flash/tag_test.go
package flash_test

import (
	"context"
	"sort"
	"testing"

	"github.com/iliafrenkel/on-suite/internal/apps/flash"
)

func tagNames(tags []flash.Tag) []string {
	names := make([]string, len(tags))
	for i, tg := range tags {
		names[i] = tg.Name
	}
	sort.Strings(names)
	return names
}

func TestSetAndFetchCardTags(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	deck, err := f.store.CreateDeck(ctx, f.alice.ID, "Spanish", "")
	if err != nil {
		t.Fatal(err)
	}
	card, err := f.store.CreateCard(ctx, f.alice.ID, deck.ID, flash.CardTypeBasic, "hola", "hello", "")
	if err != nil {
		t.Fatal(err)
	}

	if err := f.store.SetCardTags(ctx, f.alice.ID, card.ID, []string{"greetings", "beginner"}); err != nil {
		t.Fatalf("SetCardTags: %v", err)
	}
	got, err := f.store.TagsForCard(ctx, f.alice.ID, card.ID)
	if err != nil {
		t.Fatalf("TagsForCard: %v", err)
	}
	if want := []string{"beginner", "greetings"}; !equalStrings(tagNames(got), want) {
		t.Errorf("TagsForCard = %v, want %v", tagNames(got), want)
	}

	// Replacing the set drops "beginner" entirely.
	if err := f.store.SetCardTags(ctx, f.alice.ID, card.ID, []string{"greetings"}); err != nil {
		t.Fatal(err)
	}
	got, err = f.store.TagsForCard(ctx, f.alice.ID, card.ID)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"greetings"}; !equalStrings(tagNames(got), want) {
		t.Errorf("TagsForCard after replace = %v, want %v", tagNames(got), want)
	}
}

func TestSetCardTagsRejectsSomeoneElsesCard(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	deck, err := f.store.CreateDeck(ctx, f.alice.ID, "alice's", "")
	if err != nil {
		t.Fatal(err)
	}
	card, err := f.store.CreateCard(ctx, f.alice.ID, deck.ID, flash.CardTypeBasic, "a", "b", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := f.store.SetCardTags(ctx, f.bob.ID, card.ID, []string{"hijacked"}); err == nil {
		t.Error("bob was able to tag alice's card")
	}
}

func TestCardsByTagIsCrossDeckAndOwnerScoped(t *testing.T) {
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
	cardA, err := f.store.CreateCard(ctx, f.alice.ID, deckA.ID, flash.CardTypeBasic, "a1", "x", "")
	if err != nil {
		t.Fatal(err)
	}
	cardB, err := f.store.CreateCard(ctx, f.alice.ID, deckB.ID, flash.CardTypeBasic, "b1", "x", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := f.store.SetCardTags(ctx, f.alice.ID, cardA.ID, []string{"hard"}); err != nil {
		t.Fatal(err)
	}
	if err := f.store.SetCardTags(ctx, f.alice.ID, cardB.ID, []string{"hard"}); err != nil {
		t.Fatal(err)
	}

	got, err := f.store.CardsByTag(ctx, f.alice.ID, "hard")
	if err != nil {
		t.Fatalf("CardsByTag: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("CardsByTag(hard) returned %d cards, want 2 across both decks", len(got))
	}

	bobsGot, err := f.store.CardsByTag(ctx, f.bob.ID, "hard")
	if err != nil {
		t.Fatal(err)
	}
	if len(bobsGot) != 0 {
		t.Errorf("bob can see alice's tagged cards: %+v", bobsGot)
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
