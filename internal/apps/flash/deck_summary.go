// internal/apps/flash/deck_summary.go
package flash

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// DeckSummary is everything the home screen shows about one deck: its list
// row (stack, status line, due badge) and its pane (Review button, tiles,
// "next cards" line). See the UI overhaul spec §2.
type DeckSummary struct {
	Deck      Deck
	CardCount int // every card in the deck
	Mastered  int // cards in FSRS state "review" — the stats page's own rule
	DueTotal  int // review cards due now, before the daily review limit
	NewUnseen int // cards never reviewed, before the daily new-card limit
	DueToday  int // review cards in today's queue, after the limit; 0 when snoozed
	NewToday  int // new cards still allowed today, after the limit; 0 when snoozed
	// ReviewNow is DueToday + NewToday: exactly len(DueQueue) for this deck
	// (TestDeckSummariesAgreeWithDueQueue pins that).
	ReviewNow int
	NextDueAt *time.Time // earliest due_at strictly after now; nil if none
	Snoozed   bool
}

// DeckSummaries returns one DeckSummary per deck userID owns, in ListDecks
// order (newest first). One aggregate query per deck: a household has a
// handful of decks, and each query reads only that deck's cards.
func (st *Store) DeckSummaries(ctx context.Context, userID int64, now time.Time) ([]DeckSummary, error) {
	decks, err := st.ListDecks(ctx, userID)
	if err != nil {
		return nil, err
	}
	out := make([]DeckSummary, 0, len(decks))
	for _, d := range decks {
		s, err := st.deckSummary(ctx, userID, d, now)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, nil
}

// deckSummary builds one deck's DeckSummary. QueueFront counts today's
// queue with it too, so the Review button, the review screen's "N of M" and
// DueQueue's length all follow one rule.
func (st *Store) deckSummary(ctx context.Context, userID int64, d Deck, now time.Time) (DeckSummary, error) {
	s := DeckSummary{Deck: d, Snoozed: d.IsSnoozed(now)}
	var nextDue sql.NullString
	err := st.db.QueryRowContext(ctx, `
		SELECT count(*),
		       coalesce(sum(CASE WHEN s.state = 'review' THEN 1 ELSE 0 END), 0),
		       coalesce(sum(CASE WHEN s.card_id IS NOT NULL AND s.due_at <= ? THEN 1 ELSE 0 END), 0),
		       coalesce(sum(CASE WHEN s.card_id IS NULL THEN 1 ELSE 0 END), 0),
		       min(CASE WHEN s.due_at > ? THEN s.due_at END)
		  FROM flash_cards c
		  LEFT JOIN flash_card_state s ON s.card_id = c.id AND s.user_id = c.user_id
		 WHERE c.deck_id = ? AND c.user_id = ?`,
		formatTime(now), formatTime(now), d.ID, userID,
	).Scan(&s.CardCount, &s.Mastered, &s.DueTotal, &s.NewUnseen, &nextDue)
	if err != nil {
		return DeckSummary{}, fmt.Errorf("flash: deck summaries: %w", err)
	}
	if nextDue.Valid {
		t, err := parseTime(nextDue.String)
		if err != nil {
			return DeckSummary{}, err
		}
		s.NextDueAt = &t
	}

	if !s.Snoozed {
		newCount, reviewCount, err := st.dailyCounts(ctx, userID, d.ID, now)
		if err != nil {
			return DeckSummary{}, err
		}
		reviewsRemaining, newRemaining := dailyBudget(d, newCount, reviewCount)
		s.DueToday = s.DueTotal
		if reviewsRemaining >= 0 && s.DueToday > reviewsRemaining {
			s.DueToday = reviewsRemaining
		}
		s.NewToday = min(s.NewUnseen, newRemaining)
		s.ReviewNow = s.DueToday + s.NewToday
	}
	return s, nil
}

// nextCardsLabel is the second line of the deck pane's "All done for today"
// panel: when this deck will next have something to review. "" means there
// is nothing useful to say (no cards, or nothing scheduled at all). Only
// meaningful when s.ReviewNow is 0 and the deck is not snoozed.
//
// Days are UTC calendar days, the same day boundary flash_review_counts and
// the daily limits use (formatDay).
func nextCardsLabel(s DeckSummary, now time.Time) string {
	switch {
	case s.CardCount == 0:
		return ""
	case s.DueTotal > s.DueToday || s.NewUnseen > s.NewToday:
		// Cards are waiting but today's limits hold them back; the budgets
		// reset at the next UTC day.
		return "Next cards tomorrow"
	case s.NextDueAt == nil:
		return ""
	}
	today, _ := time.Parse("2006-01-02", formatDay(now))
	next, _ := time.Parse("2006-01-02", formatDay(*s.NextDueAt))
	switch days := int(next.Sub(today).Hours() / 24); {
	case days <= 0:
		return "More cards later today"
	case days == 1:
		return "Next cards tomorrow"
	default:
		return fmt.Sprintf("Next cards in %d days", days)
	}
}
