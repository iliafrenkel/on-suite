package books_test

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/iliafrenkel/on-suite/internal/apps/books"
)

func TestParseTags(t *testing.T) {
	tests := []struct {
		raw  string
		want []string
	}{
		{"", nil},
		{"SF, classics", []string{"sf", "classics"}},
		{" #sf ,, sf, Science  Fiction ", []string{"sf", "science fiction"}},
		{"a\tb, c\nd", []string{"a b", "c d"}},
		{strings.Repeat("x", 45), []string{strings.Repeat("x", 40)}},
	}
	for _, tt := range tests {
		if got := books.ParseTags(tt.raw); !slices.Equal(got, tt.want) {
			t.Errorf("ParseTags(%q) = %q, want %q", tt.raw, got, tt.want)
		}
	}
	many := strings.Repeat("t,", 30)
	for i := 0; i < 30; i++ {
		many += string(rune('a'+i%26)) + string(rune('a'+i/26)) + ","
	}
	if got := books.ParseTags(many); len(got) != books.MaxTags {
		t.Errorf("ParseTags kept %d tags, want %d", len(got), books.MaxTags)
	}
}

func TestSetTagsReplacesTheBooksTags(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	nb := onShelf("Dune", books.ShelfWant)
	nb.Tags = []string{"sf", "classics"}
	id := addBook(t, f, f.alice.ID, nb)

	if err := f.store.SetTags(ctx, f.alice.ID, id, []string{"Desert", "sf"}); err != nil {
		t.Fatal(err)
	}
	got, err := f.store.BookTags(ctx, f.alice.ID, id)
	if err != nil || !slices.Equal(got, []string{"desert", "sf"}) {
		t.Errorf("BookTags = %v, %v; want [desert sf]", got, err)
	}
	all, err := f.store.TagNames(ctx, f.alice.ID)
	if err != nil || !slices.Equal(all, []string{"desert", "sf"}) {
		t.Errorf("TagNames = %v, %v; want [desert sf] (classics unused, so gone)", all, err)
	}
}

func TestSetTagsIsScopedToItsOwner(t *testing.T) {
	f := newFixture(t)
	id := addBook(t, f, f.alice.ID, onShelf("Dune", books.ShelfWant))
	if err := f.store.SetTags(context.Background(), f.bob.ID, id, []string{"x"}); !errors.Is(err, books.ErrNotFound) {
		t.Errorf("Bob's SetTags = %v, want ErrNotFound", err)
	}
	if names, _ := f.store.TagNames(context.Background(), f.bob.ID); len(names) != 0 {
		t.Errorf("Bob has tags %v after a refused SetTags", names)
	}
}
