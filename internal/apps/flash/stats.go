// internal/apps/flash/stats.go
package flash

import (
	"context"
	"fmt"
	"time"
)

// Streak returns the number of consecutive calendar days, ending at now (or
// at the most recent day with a review, if today has none yet), on which
// userID reviewed at least one card in any deck. A single day with zero
// reviews anywhere breaks the chain.
func (st *Store) Streak(ctx context.Context, userID int64, now time.Time) (int, error) {
	rows, err := st.db.QueryContext(ctx, `
        SELECT DISTINCT day FROM flash_review_counts
        WHERE user_id = ? AND (new_count + review_count) > 0`, userID)
	if err != nil {
		return 0, fmt.Errorf("flash: streak: %w", err)
	}
	defer func() { _ = rows.Close() }()

	days := map[string]bool{}
	for rows.Next() {
		var day string
		if err := rows.Scan(&day); err != nil {
			return 0, fmt.Errorf("flash: streak: %w", err)
		}
		days[day] = true
	}
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("flash: streak: %w", err)
	}

	cursor := now.UTC().Truncate(24 * time.Hour)
	if !days[formatDay(cursor)] {
		// Today has no review yet — that alone must not break a streak that
		// is still alive as of yesterday.
		cursor = cursor.AddDate(0, 0, -1)
	}
	count := 0
	for days[formatDay(cursor)] {
		count++
		cursor = cursor.AddDate(0, 0, -1)
	}
	return count, nil
}

// RetentionRate is the fraction of reviews rated Hard, Good, or Easy (i.e.
// not Again) across every day since (inclusive) through now, across every
// deck userID owns. Returns 0 when there were no reviews in the window at
// all, rather than dividing by zero.
func (st *Store) RetentionRate(ctx context.Context, userID int64, since time.Time) (float64, error) {
	var again, hard, good, easy int
	err := st.db.QueryRowContext(ctx, `
        SELECT coalesce(sum(again_count), 0), coalesce(sum(hard_count), 0),
               coalesce(sum(good_count), 0), coalesce(sum(easy_count), 0)
          FROM flash_review_counts
         WHERE user_id = ? AND day >= ?`,
		userID, formatDay(since)).Scan(&again, &hard, &good, &easy)
	if err != nil {
		return 0, fmt.Errorf("flash: retention rate: %w", err)
	}
	total := again + hard + good + easy
	if total == 0 {
		return 0, nil
	}
	return float64(hard+good+easy) / float64(total), nil
}

// CardsMastered counts userID's cards currently in FSRS's "review" state —
// cards that have graduated out of Learning/Relearning into long-term
// maintenance.
func (st *Store) CardsMastered(ctx context.Context, userID int64) (int, error) {
	var n int
	err := st.db.QueryRowContext(ctx,
		`SELECT count(*) FROM flash_card_state WHERE user_id = ? AND state = 'review'`,
		userID).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("flash: cards mastered: %w", err)
	}
	return n, nil
}

// CardsDueToday counts userID's cards with an existing review schedule
// whose due date has passed, across every deck that is not currently
// snoozed. It is uncapped by any deck's daily review budget — unlike
// DueQueue, which stops at each deck's ReviewsPerDay limit — so it answers
// "how much is waiting," not "how much will the review screen show me
// right now."
func (st *Store) CardsDueToday(ctx context.Context, userID int64, now time.Time) (int, error) {
	var n int
	err := st.db.QueryRowContext(ctx, `
        SELECT count(*)
          FROM flash_card_state cs
          JOIN flash_cards c ON c.id = cs.card_id
          JOIN flash_decks d ON d.id = c.deck_id
         WHERE cs.user_id = ? AND cs.due_at <= ?
           AND (d.snoozed_until IS NULL OR d.snoozed_until <= ?)`,
		userID, formatTime(now), formatTime(now)).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("flash: cards due today: %w", err)
	}
	return n, nil
}

// DayCount is one calendar day's combined new+review volume for one
// account, across every deck.
type DayCount struct {
	Day   time.Time
	Count int
}

// DailyReviewCounts returns the last days calendar days ending at now,
// oldest first, including days with zero reviews — a chart that omitted
// quiet days would compress its x-axis and show a busier habit than the
// real one.
func (st *Store) DailyReviewCounts(ctx context.Context, userID int64, days int, now time.Time) ([]DayCount, error) {
	if days <= 0 {
		return nil, nil
	}
	end := now.UTC().Truncate(24 * time.Hour)
	start := end.AddDate(0, 0, -(days - 1))

	rows, err := st.db.QueryContext(ctx, `
        SELECT day, sum(new_count + review_count)
          FROM flash_review_counts
         WHERE user_id = ? AND day >= ? AND day <= ?
         GROUP BY day`,
		userID, formatDay(start), formatDay(end))
	if err != nil {
		return nil, fmt.Errorf("flash: daily review counts: %w", err)
	}
	defer func() { _ = rows.Close() }()

	byDay := map[string]int{}
	for rows.Next() {
		var day string
		var count int
		if err := rows.Scan(&day, &count); err != nil {
			return nil, fmt.Errorf("flash: daily review counts: %w", err)
		}
		byDay[day] = count
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("flash: daily review counts: %w", err)
	}

	out := make([]DayCount, 0, days)
	for d := start; !d.After(end); d = d.AddDate(0, 0, 1) {
		out = append(out, DayCount{Day: d, Count: byDay[formatDay(d)]})
	}
	return out, nil
}

// DeckLoad is one deck's numbers for the per-deck breakdown table.
type DeckLoad struct {
	Deck              Deck
	Mastered          int
	Due               int
	ReviewsLast30Days int
}

// PerDeckLoad returns one DeckLoad per deck userID owns, in the same order
// ListDecks does (newest first).
func (st *Store) PerDeckLoad(ctx context.Context, userID int64, now time.Time) ([]DeckLoad, error) {
	decks, err := st.ListDecks(ctx, userID)
	if err != nil {
		return nil, err
	}
	since := formatDay(now.AddDate(0, 0, -29))

	out := make([]DeckLoad, 0, len(decks))
	for _, d := range decks {
		load := DeckLoad{Deck: d}

		if err := st.db.QueryRowContext(ctx, `
            SELECT count(*) FROM flash_card_state cs
              JOIN flash_cards c ON c.id = cs.card_id
             WHERE cs.user_id = ? AND c.deck_id = ? AND cs.state = 'review'`,
			userID, d.ID).Scan(&load.Mastered); err != nil {
			return nil, fmt.Errorf("flash: per-deck load: mastered: %w", err)
		}

		if err := st.db.QueryRowContext(ctx, `
            SELECT count(*) FROM flash_card_state cs
              JOIN flash_cards c ON c.id = cs.card_id
             WHERE cs.user_id = ? AND c.deck_id = ? AND cs.due_at <= ?`,
			userID, d.ID, formatTime(now)).Scan(&load.Due); err != nil {
			return nil, fmt.Errorf("flash: per-deck load: due: %w", err)
		}

		var reviews *int
		if err := st.db.QueryRowContext(ctx, `
            SELECT sum(new_count + review_count) FROM flash_review_counts
             WHERE user_id = ? AND deck_id = ? AND day >= ?`,
			userID, d.ID, since).Scan(&reviews); err != nil {
			return nil, fmt.Errorf("flash: per-deck load: reviews: %w", err)
		}
		if reviews != nil {
			load.ReviewsLast30Days = *reviews
		}

		out = append(out, load)
	}
	return out, nil
}
