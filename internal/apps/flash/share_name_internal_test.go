package flash

import (
	"strings"
	"testing"
	"unicode/utf8"
)

// TestAdoptedDeckName pins #304 bullet 5's naming rule: the sharer's name
// as a suffix, then a counter, and only ever the base shortened so the
// whole name stays within MaxDeckNameRunes.
func TestAdoptedDeckName(t *testing.T) {
	long := strings.Repeat("é", MaxDeckNameRunes)
	spaced := strings.Repeat("a", 106) + " tail" // cut lands right after the space
	tests := []struct {
		base, sharer string
		n            int
		want         string
	}{
		{"Spanish", "alice", 1, "Spanish (from alice)"},
		{"Spanish", "alice", 2, "Spanish (from alice) (2)"},
		{"Spanish", "alice", 3, "Spanish (from alice) (3)"},
		{"Spanish", "", 1, "Spanish (shared)"},
		{"Spanish", "", 2, "Spanish (shared) (2)"},
		// " (from alice) (3)" is 17 runes, so 103 of the base survive.
		{long, "alice", 3, strings.Repeat("é", 103) + " (from alice) (3)"},
		// 107 runes survive, ending in a space, which is trimmed.
		{spaced, "alice", 1, strings.Repeat("a", 106) + " (from alice)"},
	}
	for _, tt := range tests {
		got := adoptedDeckName(tt.base, tt.sharer, tt.n)
		if got != tt.want {
			t.Errorf("adoptedDeckName(%.10q…, %q, %d) = %q, want %q", tt.base, tt.sharer, tt.n, got, tt.want)
		}
		if n := utf8.RuneCountInString(got); n > MaxDeckNameRunes {
			t.Errorf("adoptedDeckName(%.10q…, %q, %d) is %d runes, over %d", tt.base, tt.sharer, tt.n, n, MaxDeckNameRunes)
		}
		if err := ValidateDeck(got, ""); err != nil {
			t.Errorf("adoptedDeckName(%.10q…, %q, %d) = %q fails ValidateDeck: %v", tt.base, tt.sharer, tt.n, got, err)
		}
	}
}
