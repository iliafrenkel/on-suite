// internal/apps/flash/deck_summary_internal_test.go
package flash

import (
	"testing"
	"time"
)

func TestNextCardsLabel(t *testing.T) {
	now := time.Date(2026, 9, 23, 10, 0, 0, 0, time.UTC)
	at := func(d time.Duration) *time.Time { v := now.Add(d); return &v }

	tests := []struct {
		name string
		s    DeckSummary
		want string
	}{
		{"no cards", DeckSummary{CardCount: 0}, ""},
		{"new cards held back by today's limit", DeckSummary{CardCount: 5, NewUnseen: 5, NewToday: 0}, "Next cards tomorrow"},
		{"reviews held back by today's limit", DeckSummary{CardCount: 5, DueTotal: 4, DueToday: 0}, "Next cards tomorrow"},
		{"later today", DeckSummary{CardCount: 5, NextDueAt: at(3 * time.Hour)}, "More cards later today"},
		{"tomorrow", DeckSummary{CardCount: 5, NextDueAt: at(20 * time.Hour)}, "Next cards tomorrow"},
		{"in 3 days", DeckSummary{CardCount: 5, NextDueAt: at(3 * 24 * time.Hour)}, "Next cards in 3 days"},
		{"nothing scheduled", DeckSummary{CardCount: 5}, ""},
	}
	for _, tt := range tests {
		if got := nextCardsLabel(tt.s, now); got != tt.want {
			t.Errorf("%s: nextCardsLabel = %q, want %q", tt.name, got, tt.want)
		}
	}
}
