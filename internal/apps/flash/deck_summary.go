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
// order (newest first). It runs three queries however many decks there are
// (#319): ListDecks, one card aggregate grouped by deck (deckAggregates) and
// one read of today's review counters for every deck (todayCounts). The
// budget arithmetic stays in Go, in summarise, which the single-deck path
// shares.
func (st *Store) DeckSummaries(ctx context.Context, userID int64, now time.Time) ([]DeckSummary, error) {
	decks, err := st.ListDecks(ctx, userID)
	if err != nil {
		return nil, err
	}
	aggs, err := st.deckAggregates(ctx, userID, now)
	if err != nil {
		return nil, err
	}
	counts, err := st.todayCounts(ctx, userID, now)
	if err != nil {
		return nil, err
	}
	out := make([]DeckSummary, 0, len(decks))
	for _, d := range decks {
		// A deck with no cards has a zero aggregate, and one with nothing
		// graded today has zero counts — the map lookups' zero values.
		out = append(out, summarise(d, aggs[d.ID], counts[d.ID], now))
	}
	return out, nil
}

// deckAggregate is one deck's raw card numbers, before any daily limit.
type deckAggregate struct {
	CardCount int
	Mastered  int
	DueTotal  int
	NewUnseen int
	NextDueAt *time.Time
}

// dailyCount is one deck's flash_review_counts row for one day: how many new
// and review cards have been graded so far.
type dailyCount struct {
	New, Review int
}

// summarise turns one deck's raw numbers into its DeckSummary. It is the one
// place the daily limits are applied to a summary (via dailyBudget, which
// DueQueue shares), for both DeckSummaries and the single-deck path, so the
// Review button, the review screen's "N of M" and DueQueue's length all
// agree (pinned by TestQueueFrontAgreesWithDueQueue and
// TestDeckSummariesAgreeWithDueQueue). A snoozed deck has nothing to review
// today whatever its counts say.
func summarise(d Deck, agg deckAggregate, today dailyCount, now time.Time) DeckSummary {
	s := DeckSummary{
		Deck:      d,
		CardCount: agg.CardCount,
		Mastered:  agg.Mastered,
		DueTotal:  agg.DueTotal,
		NewUnseen: agg.NewUnseen,
		NextDueAt: agg.NextDueAt,
		Snoozed:   d.IsSnoozed(now),
	}
	if s.Snoozed {
		return s
	}
	reviewsRemaining, newRemaining := dailyBudget(d, today.New, today.Review)
	s.DueToday = s.DueTotal
	if reviewsRemaining >= 0 && s.DueToday > reviewsRemaining {
		s.DueToday = reviewsRemaining
	}
	s.NewToday = min(s.NewUnseen, newRemaining)
	s.ReviewNow = s.DueToday + s.NewToday
	return s
}

// deckSummary builds one deck's DeckSummary with two single-deck queries —
// QueueFront's path for a one-deck scope. It deliberately keeps its own
// per-deck SQL rather than filtering deckAggregates' grouped query: the two
// formulations are cross-checked by TestDeckSummariesMatchPerDeckReference.
func (st *Store) deckSummary(ctx context.Context, userID int64, d Deck, now time.Time) (DeckSummary, error) {
	agg, err := st.deckAggregate(ctx, userID, d.ID, now)
	if err != nil {
		return DeckSummary{}, err
	}
	var today dailyCount
	if !d.IsSnoozed(now) {
		if today.New, today.Review, err = st.dailyCounts(ctx, userID, d.ID, now); err != nil {
			return DeckSummary{}, err
		}
	}
	return summarise(d, agg, today, now), nil
}

// deckAggregate reads one deck's raw card numbers.
func (st *Store) deckAggregate(ctx context.Context, userID, deckID int64, now time.Time) (deckAggregate, error) {
	var agg deckAggregate
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
		formatTime(now), formatTime(now), deckID, userID,
	).Scan(&agg.CardCount, &agg.Mastered, &agg.DueTotal, &agg.NewUnseen, &nextDue)
	if err != nil {
		return deckAggregate{}, fmt.Errorf("flash: deck summary: %w", err)
	}
	if agg.NextDueAt, err = parseNullTime(nextDue); err != nil {
		return deckAggregate{}, err
	}
	return agg, nil
}

// deckAggregates is deckAggregate for every deck userID owns, in one query
// grouped by deck. Cards are reached through flash_decks (its user index,
// then flash_cards' deck index) rather than filtered by flash_cards.user_id,
// which has no index. A deck with no cards gets no row; DeckSummaries reads
// that as a zero aggregate.
func (st *Store) deckAggregates(ctx context.Context, userID int64, now time.Time) (map[int64]deckAggregate, error) {
	rows, err := st.db.QueryContext(ctx, `
		SELECT c.deck_id,
		       count(*),
		       coalesce(sum(CASE WHEN s.state = 'review' THEN 1 ELSE 0 END), 0),
		       coalesce(sum(CASE WHEN s.card_id IS NOT NULL AND s.due_at <= ? THEN 1 ELSE 0 END), 0),
		       coalesce(sum(CASE WHEN s.card_id IS NULL THEN 1 ELSE 0 END), 0),
		       min(CASE WHEN s.due_at > ? THEN s.due_at END)
		  FROM flash_decks d
		  JOIN flash_cards c ON c.deck_id = d.id AND c.user_id = d.user_id
		  LEFT JOIN flash_card_state s ON s.card_id = c.id AND s.user_id = c.user_id
		 WHERE d.user_id = ?
		 GROUP BY c.deck_id`,
		formatTime(now), formatTime(now), userID)
	if err != nil {
		return nil, fmt.Errorf("flash: deck summaries: %w", err)
	}
	defer func() { _ = rows.Close() }()

	out := map[int64]deckAggregate{}
	for rows.Next() {
		var (
			deckID  int64
			agg     deckAggregate
			nextDue sql.NullString
		)
		if err := rows.Scan(&deckID, &agg.CardCount, &agg.Mastered, &agg.DueTotal, &agg.NewUnseen, &nextDue); err != nil {
			return nil, fmt.Errorf("flash: deck summaries: %w", err)
		}
		if agg.NextDueAt, err = parseNullTime(nextDue); err != nil {
			return nil, err
		}
		out[deckID] = agg
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("flash: deck summaries: %w", err)
	}
	return out, nil
}

// todayCounts is dailyCounts for every deck userID owns, in one query. It
// walks userID's decks and seeks each deck's row for today by the full
// (user_id, deck_id, day) primary key; filtering flash_review_counts by
// user_id alone would read every day that user has ever reviewed. CROSS
// JOIN is SQLite's way to fix that join order. A deck with nothing graded
// today gets no row. Snoozed decks' counts are read too; summarise ignores
// them.
func (st *Store) todayCounts(ctx context.Context, userID int64, now time.Time) (map[int64]dailyCount, error) {
	rows, err := st.db.QueryContext(ctx, `
		SELECT r.deck_id, r.new_count, r.review_count
		  FROM flash_decks d
		 CROSS JOIN flash_review_counts r
		    ON r.user_id = d.user_id AND r.deck_id = d.id AND r.day = ?
		 WHERE d.user_id = ?`, formatDay(now), userID)
	if err != nil {
		return nil, fmt.Errorf("flash: daily counts: %w", err)
	}
	defer func() { _ = rows.Close() }()

	out := map[int64]dailyCount{}
	for rows.Next() {
		var (
			deckID int64
			c      dailyCount
		)
		if err := rows.Scan(&deckID, &c.New, &c.Review); err != nil {
			return nil, fmt.Errorf("flash: daily counts: %w", err)
		}
		out[deckID] = c
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("flash: daily counts: %w", err)
	}
	return out, nil
}

// parseNullTime parses an optional stored timestamp; NULL is nil.
func parseNullTime(s sql.NullString) (*time.Time, error) {
	if !s.Valid {
		return nil, nil
	}
	t, err := parseTime(s.String)
	if err != nil {
		return nil, err
	}
	return &t, nil
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
