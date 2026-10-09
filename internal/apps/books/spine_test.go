package books_test

import (
	"slices"
	"testing"

	"github.com/iliafrenkel/on-suite/internal/apps/books"
)

func TestSpineColorIsStableAndFromThePalette(t *testing.T) {
	c := books.SpineColor("Piranesi")
	if c != books.SpineColor("piranesi") || c != books.SpineColor("Piranesi") {
		t.Errorf("SpineColor is not stable across calls and case")
	}
	seen := map[string]bool{}
	for _, title := range []string{"Piranesi", "Dune", "Leviathan Wakes", "The Dispossessed", "Emma", "Middlemarch", "Ulysses", "Beloved"} {
		color := books.SpineColor(title)
		if !slices.Contains(books.Colors, color) {
			t.Errorf("SpineColor(%q) = %q, not in the palette", title, color)
		}
		seen[color] = true
	}
	if len(seen) < 3 {
		t.Errorf("eight titles got only %d colours: %v", len(seen), seen)
	}
}
