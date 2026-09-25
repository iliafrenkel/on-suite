// internal/apps/flash/tag_test.go
package flash_test

import (
	"context"
	"errors"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/iliafrenkel/on-suite/internal/apps/flash"
)

func TestValidateTagNames(t *testing.T) {
	tooLong := strings.Repeat("a", flash.MaxTagNameRunes+1)
	tests := []struct {
		name    string
		tags    []string
		wantErr bool
	}{
		{"empty list", nil, false},
		{"ordinary tags", []string{"hard", "beginner"}, false},
		{"blank entries are ignored", []string{"", "  ", "hard"}, false},
		{"name at the limit", []string{strings.Repeat("a", flash.MaxTagNameRunes)}, false},
		{"name over the limit", []string{tooLong}, true},
		{"one bad name among good ones", []string{"hard", tooLong}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := flash.ValidateTagNames(tt.tags)
			if tt.wantErr && !errors.Is(err, flash.ErrInvalid) {
				t.Errorf("ValidateTagNames(%v) = %v, want ErrInvalid", tt.tags, err)
			}
			if !tt.wantErr && err != nil {
				t.Errorf("ValidateTagNames(%v) rejected valid tags: %v", tt.tags, err)
			}
		})
	}
}

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
	deck, err := f.store.CreateDeck(ctx, f.alice.ID, "Spanish", "", flash.DefaultDeckColor)
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
	deck, err := f.store.CreateDeck(ctx, f.alice.ID, "alice's", "", flash.DefaultDeckColor)
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
	deckA, err := f.store.CreateDeck(ctx, f.alice.ID, "A", "", flash.DefaultDeckColor)
	if err != nil {
		t.Fatal(err)
	}
	deckB, err := f.store.CreateDeck(ctx, f.alice.ID, "B", "", flash.DefaultDeckColor)
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

func TestCardTagsInDeck(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	d, err := f.store.CreateDeck(ctx, f.alice.ID, "Spanish", "", flash.DefaultDeckColor)
	if err != nil {
		t.Fatal(err)
	}
	c1, err := f.store.CreateCard(ctx, f.alice.ID, d.ID, flash.CardTypeBasic, "hola", "hello", "")
	if err != nil {
		t.Fatal(err)
	}
	c2, err := f.store.CreateCard(ctx, f.alice.ID, d.ID, flash.CardTypeBasic, "pan", "bread", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.CreateCard(ctx, f.alice.ID, d.ID, flash.CardTypeBasic, "untagged", "x", ""); err != nil {
		t.Fatal(err)
	}
	if err := f.store.SetCardTags(ctx, f.alice.ID, c1.ID, []string{"phrases", "greetings"}); err != nil {
		t.Fatal(err)
	}
	if err := f.store.SetCardTags(ctx, f.alice.ID, c2.ID, []string{"food"}); err != nil {
		t.Fatal(err)
	}

	got, err := f.store.CardTagsInDeck(ctx, f.alice.ID, d.ID)
	if err != nil {
		t.Fatal(err)
	}
	want := map[int64][]string{c1.ID: {"greetings", "phrases"}, c2.ID: {"food"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("CardTagsInDeck = %v, want %v", got, want)
	}

	other, err := f.store.CardTagsInDeck(ctx, f.bob.ID, d.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(other) != 0 {
		t.Errorf("bob sees alice's tags: %v", other)
	}
}
