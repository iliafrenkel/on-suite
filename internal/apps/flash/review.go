// internal/apps/flash/review.go
package flash

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// formatDay is flash_review_counts' calendar-day convention: a plain UTC
// date, coarser than this package's usual RFC3339Nano timestamps, because a
// daily counter only ever needs to compare "same day or not."
func formatDay(t time.Time) string { return t.UTC().Format("2006-01-02") }

// CardState returns cardID's current schedule for userID, and whether it has
// ever been reviewed. A never-reviewed card returns a fresh cardSchedule
// (as newCardSchedule would build) and false, not an error.
func (st *Store) CardState(ctx context.Context, userID, cardID int64) (cardSchedule, bool, error) {
	if _, err := st.cardOwnerCheck(ctx, userID, cardID); err != nil {
		return cardSchedule{}, false, err
	}
	return st.scanCardState(ctx, userID, cardID)
}

func (st *Store) scanCardState(ctx context.Context, userID, cardID int64) (cardSchedule, bool, error) {
	var (
		c                   cardSchedule
		dueAt, lastReviewAt sql.NullString
	)
	err := st.db.QueryRowContext(ctx, `
		SELECT state, due_at, stability, difficulty, scheduled_days, reps, lapses, remaining_steps, last_review_at
		FROM flash_card_state WHERE user_id = ? AND card_id = ?`, userID, cardID,
	).Scan(&c.State, &dueAt, &c.Stability, &c.Difficulty, &c.ScheduledDays, &c.Reps, &c.Lapses, &c.RemainingSteps, &lastReviewAt)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return cardSchedule{}, false, nil
	case err != nil:
		return cardSchedule{}, false, fmt.Errorf("flash: scan card state: %w", err)
	}
	if c.DueAt, err = parseTime(dueAt.String); err != nil {
		return cardSchedule{}, false, err
	}
	if lastReviewAt.Valid {
		if c.LastReviewAt, err = parseTime(lastReviewAt.String); err != nil {
			return cardSchedule{}, false, err
		}
	}
	return c, true, nil
}

// formatNullableTime is like formatTime, but a zero time.Time (a card that
// has never actually been reviewed, which rollbackCard can produce by
// undoing a card's only grade) stores as SQL NULL instead of a formatted
// zero-value timestamp.
func formatNullableTime(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return formatTime(t)
}

// GradeCard applies rating to cardID's current schedule (starting a fresh
// one if this is its first-ever review), persists the result, saves the
// pre-grade log so the grade can be undone, and records it against today's
// daily limit for cardID's deck. All three writes happen in one transaction:
// a grade that updated the schedule but not the daily count (or vice versa)
// would leave the queue-construction logic in Task 4 lying about what is
// still due today.
func (st *Store) GradeCard(ctx context.Context, userID, cardID int64, rating int, now time.Time) (cardSchedule, error) {
	deckID, err := st.cardOwnerCheck(ctx, userID, cardID)
	if err != nil {
		return cardSchedule{}, err
	}

	current, hadState, err := st.scanCardState(ctx, userID, cardID)
	if err != nil {
		return cardSchedule{}, err
	}
	if !hadState {
		current = newCardSchedule(now)
	}

	graded, log, err := gradeCard(current, rating, now)
	if err != nil {
		return cardSchedule{}, fmt.Errorf("%w: %v", ErrInvalid, err)
	}

	tx, err := st.db.BeginTx(ctx, nil)
	if err != nil {
		return cardSchedule{}, fmt.Errorf("flash: grade card: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	wasNew := 0
	if !hadState {
		wasNew = 1
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO flash_card_state (
			user_id, card_id, state, due_at, stability, difficulty, scheduled_days, reps, lapses, remaining_steps, last_review_at,
			log_rating, log_due, log_scheduled_days, log_review, log_state, log_stability, log_difficulty, log_remaining_steps, log_was_new
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (user_id, card_id) DO UPDATE SET
			state = excluded.state, due_at = excluded.due_at, stability = excluded.stability,
			difficulty = excluded.difficulty, scheduled_days = excluded.scheduled_days,
			reps = excluded.reps, lapses = excluded.lapses, remaining_steps = excluded.remaining_steps,
			last_review_at = excluded.last_review_at,
			log_rating = excluded.log_rating, log_due = excluded.log_due,
			log_scheduled_days = excluded.log_scheduled_days, log_review = excluded.log_review,
			log_state = excluded.log_state, log_stability = excluded.log_stability,
			log_difficulty = excluded.log_difficulty, log_remaining_steps = excluded.log_remaining_steps,
			log_was_new = excluded.log_was_new`,
		userID, cardID, graded.State, formatTime(graded.DueAt), graded.Stability, graded.Difficulty,
		graded.ScheduledDays, graded.Reps, graded.Lapses, graded.RemainingSteps, formatNullableTime(graded.LastReviewAt),
		log.Rating, formatTime(log.Due), log.ScheduledDays, formatTime(log.Review), log.State, log.Stability, log.Difficulty, log.RemainingSteps, wasNew,
	); err != nil {
		return cardSchedule{}, fmt.Errorf("flash: grade card: %w", err)
	}

	newInc, reviewInc := 0, 1
	if !hadState {
		newInc, reviewInc = 1, 0
	}
	if err := st.bumpDailyCounts(ctx, tx, userID, deckID, now, newInc, reviewInc, rating, 1); err != nil {
		return cardSchedule{}, err
	}

	if err := tx.Commit(); err != nil {
		return cardSchedule{}, fmt.Errorf("flash: grade card: %w", err)
	}
	return graded, nil
}

// UndoLastGrade reverses cardID's most recent grade, if any. hasUndo is
// false, with no error, when there is nothing to undo — a card that has
// never been graded, or whose one undo slot was already spent.
func (st *Store) UndoLastGrade(ctx context.Context, userID, cardID int64, now time.Time) (schedule cardSchedule, hasUndo bool, err error) {
	deckID, err := st.cardOwnerCheck(ctx, userID, cardID)
	if err != nil {
		return cardSchedule{}, false, err
	}

	current, log, wasNew, hasLog, err := st.scanCardStateWithLog(ctx, userID, cardID)
	if err != nil {
		return cardSchedule{}, false, err
	}
	if !hasLog {
		return cardSchedule{}, false, nil
	}

	reverted, err := rollbackCard(current, log)
	if err != nil {
		return cardSchedule{}, false, fmt.Errorf("flash: undo last grade: %w", err)
	}

	tx, err := st.db.BeginTx(ctx, nil)
	if err != nil {
		return cardSchedule{}, false, fmt.Errorf("flash: undo last grade: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if wasNew {
		// Undoing a card's first-ever review must restore "never reviewed"
		// as an absent row, not a row full of New-state zero values — the
		// rest of this package (CardState, DueQueue's new-card query)
		// determines "never reviewed" by row absence alone. Confirmed by
		// plan validation: an UPDATE-only implementation here leaves
		// CardState reporting "reviewed" forever after undoing a card's
		// only grade, since the row still exists.
		if _, err := tx.ExecContext(ctx,
			`DELETE FROM flash_card_state WHERE user_id = ? AND card_id = ?`,
			userID, cardID,
		); err != nil {
			return cardSchedule{}, false, fmt.Errorf("flash: undo last grade: %w", err)
		}
	} else if _, err := tx.ExecContext(ctx, `
		UPDATE flash_card_state SET
			state = ?, due_at = ?, stability = ?, difficulty = ?, scheduled_days = ?, reps = ?, lapses = ?, remaining_steps = ?, last_review_at = ?,
			log_rating = NULL, log_due = NULL, log_scheduled_days = NULL, log_review = NULL,
			log_state = NULL, log_stability = NULL, log_difficulty = NULL, log_remaining_steps = NULL, log_was_new = NULL
		WHERE user_id = ? AND card_id = ?`,
		reverted.State, formatTime(reverted.DueAt), reverted.Stability, reverted.Difficulty,
		reverted.ScheduledDays, reverted.Reps, reverted.Lapses, reverted.RemainingSteps, formatNullableTime(reverted.LastReviewAt),
		userID, cardID,
	); err != nil {
		return cardSchedule{}, false, fmt.Errorf("flash: undo last grade: %w", err)
	}

	newDec, reviewDec := 0, -1
	if wasNew {
		newDec, reviewDec = -1, 0
	}
	if err := st.bumpDailyCounts(ctx, tx, userID, deckID, log.Review, newDec, reviewDec, log.Rating, -1); err != nil {
		return cardSchedule{}, false, err
	}

	if err := tx.Commit(); err != nil {
		return cardSchedule{}, false, fmt.Errorf("flash: undo last grade: %w", err)
	}
	return reverted, true, nil
}

func (st *Store) scanCardStateWithLog(ctx context.Context, userID, cardID int64) (current cardSchedule, log reviewLog, wasNew bool, hasLog bool, err error) {
	var (
		dueAt, lastReviewAt                                          sql.NullString
		logDue, logReview, logState                                  sql.NullString
		logRating, logScheduledDays, logRemainingSteps, logWasNewCol sql.NullInt64
		logStability, logDifficulty                                  sql.NullFloat64
	)
	err = st.db.QueryRowContext(ctx, `
		SELECT state, due_at, stability, difficulty, scheduled_days, reps, lapses, remaining_steps, last_review_at,
		       log_rating, log_due, log_scheduled_days, log_review, log_state, log_stability, log_difficulty, log_remaining_steps, log_was_new
		FROM flash_card_state WHERE user_id = ? AND card_id = ?`, userID, cardID,
	).Scan(
		&current.State, &dueAt, &current.Stability, &current.Difficulty, &current.ScheduledDays, &current.Reps, &current.Lapses, &current.RemainingSteps, &lastReviewAt,
		&logRating, &logDue, &logScheduledDays, &logReview, &logState, &logStability, &logDifficulty, &logRemainingSteps, &logWasNewCol,
	)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return cardSchedule{}, reviewLog{}, false, false, nil
	case err != nil:
		return cardSchedule{}, reviewLog{}, false, false, fmt.Errorf("flash: scan card state: %w", err)
	}
	if current.DueAt, err = parseTime(dueAt.String); err != nil {
		return cardSchedule{}, reviewLog{}, false, false, err
	}
	if lastReviewAt.Valid {
		if current.LastReviewAt, err = parseTime(lastReviewAt.String); err != nil {
			return cardSchedule{}, reviewLog{}, false, false, err
		}
	}
	if !logRating.Valid {
		return current, reviewLog{}, false, false, nil
	}
	log.Rating = int(logRating.Int64)
	if log.Due, err = parseTime(logDue.String); err != nil {
		return cardSchedule{}, reviewLog{}, false, false, err
	}
	log.ScheduledDays = uint64(logScheduledDays.Int64)
	if log.Review, err = parseTime(logReview.String); err != nil {
		return cardSchedule{}, reviewLog{}, false, false, err
	}
	log.State = logState.String
	log.Stability = logStability.Float64
	log.Difficulty = logDifficulty.Float64
	log.RemainingSteps = int(logRemainingSteps.Int64)
	return current, log, logWasNewCol.Int64 == 1, true, nil
}

// bumpDailyCounts adds newDelta/reviewDelta (either can be negative, for
// UndoLastGrade) to userID's counters for deckID on now's calendar day,
// creating the row if this is the first count of the day. ratingDelta
// (+1 for a grade, -1 for undoing one) is added to whichever of the four
// per-rating columns rating names, so retention rate can be computed later
// without a separate per-review event log.
func (st *Store) bumpDailyCounts(ctx context.Context, tx *sql.Tx, userID, deckID int64, now time.Time, newDelta, reviewDelta, rating, ratingDelta int) error {
	column, err := ratingCountColumn(rating)
	if err != nil {
		return err
	}
	day := formatDay(now)
	// Ensure the row exists before applying the delta. This can't be a
	// single INSERT ... ON CONFLICT DO UPDATE that adds the deltas directly:
	// SQLite evaluates flash_review_counts' CHECK constraint against the row
	// that WOULD be inserted before it even considers the conflict, so a
	// negative delta (UndoLastGrade decrementing a counter) fails the check
	// even though the actual write is an UPDATE against an existing,
	// non-negative row. Inserting zeros first, then updating with the real
	// delta in a second statement, sidesteps that: the insert candidate is
	// always non-negative, and the UPDATE's CHECK is evaluated against the
	// real post-update row.
	if _, err := tx.ExecContext(ctx, `
        INSERT INTO flash_review_counts (user_id, deck_id, day, new_count, review_count)
        VALUES (?, ?, ?, 0, 0)
        ON CONFLICT (user_id, deck_id, day) DO NOTHING`,
		userID, deckID, day,
	); err != nil {
		return fmt.Errorf("flash: update daily counts: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
        UPDATE flash_review_counts SET
            new_count = new_count + ?,
            review_count = review_count + ?,
            `+column+` = max(0, `+column+` + ?)
        WHERE user_id = ? AND deck_id = ? AND day = ?`,
		newDelta, reviewDelta, ratingDelta, userID, deckID, day,
	); err != nil {
		return fmt.Errorf("flash: update daily counts: %w", err)
	}
	return nil
}

// ratingCountColumn maps a rating constant to its flash_review_counts
// column. rating is always one of the four fixed RatingX constants from
// this package's own callers (GradeCard passes the rating it just
// validated; UndoLastGrade passes a rating it previously wrote itself), so
// this is not user-input-driven string building — it's a closed, four-way
// switch, the same shape share.go's resolveShare already uses for a column
// name selected from a fixed internal set.
func ratingCountColumn(rating int) (string, error) {
	switch rating {
	case RatingAgain:
		return "again_count", nil
	case RatingHard:
		return "hard_count", nil
	case RatingGood:
		return "good_count", nil
	case RatingEasy:
		return "easy_count", nil
	default:
		return "", fmt.Errorf("%w: %d is not a rating I know", ErrInvalid, rating)
	}
}

// dailyCounts reads flash_review_counts for userID/deckID on now's calendar
// day. It backs both DueQueue's production budget check and DailyCounts,
// the exported test accessor below, so there is exactly one query.
func (st *Store) dailyCounts(ctx context.Context, userID, deckID int64, now time.Time) (newCount, reviewCount int, err error) {
	err = st.db.QueryRowContext(ctx, `
		SELECT new_count, review_count FROM flash_review_counts
		WHERE user_id = ? AND deck_id = ? AND day = ?`, userID, deckID, formatDay(now),
	).Scan(&newCount, &reviewCount)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, 0, nil
	}
	if err != nil {
		return 0, 0, fmt.Errorf("flash: daily counts: %w", err)
	}
	return newCount, reviewCount, nil
}

// DailyCounts exposes flash_review_counts to tests outside this package, for
// asserting GradeCard/UndoLastGrade updated the right counter.
func (st *Store) DailyCounts(ctx context.Context, userID, deckID int64, now time.Time) (newCount, reviewCount int, err error) {
	return st.dailyCounts(ctx, userID, deckID, now)
}

// QueueCard is one card ready for review, with enough context to render it
// and to know which of a deck's two daily budgets grading it will spend.
type QueueCard struct {
	Card  Card
	Deck  Deck
	IsNew bool
}

// dailyBudget is how many more review and new cards deck d may put in
// today's queue, given what has already been graded today. A
// reviewsRemaining of -1 means unlimited. Shared by DueQueue and
// DeckSummaries so the Review button's count and the review page itself can
// never disagree about the limits.
func dailyBudget(d Deck, newCount, reviewCount int) (reviewsRemaining, newRemaining int) {
	reviewsRemaining = -1 // sentinel: unlimited
	if d.ReviewsPerDay != nil {
		reviewsRemaining = *d.ReviewsPerDay - reviewCount
		if reviewsRemaining < 0 {
			reviewsRemaining = 0
		}
	}
	newRemaining = d.NewCardsPerDay - newCount
	if newRemaining < 0 {
		newRemaining = 0
	}
	return reviewsRemaining, newRemaining
}

// DueQueue returns cards eligible for review right now, in review-then-new
// order. If deckID is non-nil, only that deck is considered — and only if
// it is not currently snoozed, the same rule applied to every deck when
// deckID is nil (every non-snoozed deck belonging to userID).
func (st *Store) DueQueue(ctx context.Context, userID int64, deckID *int64, now time.Time) ([]QueueCard, error) {
	decks, err := st.dueQueueDecks(ctx, userID, deckID, now)
	if err != nil {
		return nil, err
	}

	var reviews, fresh []QueueCard
	for _, d := range decks {
		newCount, reviewCount, err := st.dailyCounts(ctx, userID, d.ID, now)
		if err != nil {
			return nil, err
		}
		reviewsRemaining, newRemaining := dailyBudget(d, newCount, reviewCount)

		due, err := st.dueReviewCards(ctx, userID, d, now, reviewsRemaining)
		if err != nil {
			return nil, err
		}
		reviews = append(reviews, due...)

		newCards, err := st.newQueueCards(ctx, userID, d, newRemaining)
		if err != nil {
			return nil, err
		}
		fresh = append(fresh, newCards...)
	}
	return append(reviews, fresh...), nil
}

// dueQueueDecks resolves which decks DueQueue should consider: the one
// named by deckID (if it is not snoozed), or every one of userID's decks
// that are not currently snoozed.
func (st *Store) dueQueueDecks(ctx context.Context, userID int64, deckID *int64, now time.Time) ([]Deck, error) {
	if deckID != nil {
		d, err := st.DeckByID(ctx, userID, *deckID)
		if err != nil {
			return nil, err
		}
		if d.IsSnoozed(now) {
			return nil, nil
		}
		return []Deck{d}, nil
	}

	all, err := st.ListDecks(ctx, userID)
	if err != nil {
		return nil, err
	}
	out := make([]Deck, 0, len(all))
	for _, d := range all {
		if !d.IsSnoozed(now) {
			out = append(out, d)
		}
	}
	return out, nil
}

// dueReviewCards returns d's cards that are due at or before now, oldest
// due date first, up to limit (a negative limit means unlimited).
func (st *Store) dueReviewCards(ctx context.Context, userID int64, d Deck, now time.Time, limit int) ([]QueueCard, error) {
	if limit == 0 {
		return nil, nil
	}
	query := `
		SELECT c.id, c.deck_id, c.user_id, c.card_type, c.front, c.back, c.notes, c.created_at, c.image_hash, c.audio_hash
		FROM flash_cards c
		JOIN flash_card_state s ON s.card_id = c.id AND s.user_id = c.user_id
		WHERE c.deck_id = ? AND c.user_id = ? AND s.due_at <= ?
		ORDER BY s.due_at ASC`
	args := []any{d.ID, userID, formatTime(now)}
	if limit > 0 {
		query += ` LIMIT ?`
		args = append(args, limit)
	}
	return st.queryQueueCards(ctx, query, args, d, false)
}

// newQueueCards returns up to limit of d's cards that have never been
// reviewed, oldest-created first.
func (st *Store) newQueueCards(ctx context.Context, userID int64, d Deck, limit int) ([]QueueCard, error) {
	if limit <= 0 {
		return nil, nil
	}
	query := `
		SELECT c.id, c.deck_id, c.user_id, c.card_type, c.front, c.back, c.notes, c.created_at, c.image_hash, c.audio_hash
		FROM flash_cards c
		LEFT JOIN flash_card_state s ON s.card_id = c.id AND s.user_id = c.user_id
		WHERE c.deck_id = ? AND c.user_id = ? AND s.card_id IS NULL
		ORDER BY c.created_at ASC
		LIMIT ?`
	return st.queryQueueCards(ctx, query, []any{d.ID, userID, limit}, d, true)
}

func (st *Store) queryQueueCards(ctx context.Context, query string, args []any, d Deck, isNew bool) ([]QueueCard, error) {
	rows, err := st.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("flash: due queue: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []QueueCard
	for rows.Next() {
		c, err := scanCardRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, QueueCard{Card: c, Deck: d, IsNew: isNew})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("flash: due queue: %w", err)
	}
	return out, nil
}

// ReviewTally is how many cards were graded on one day, in total and per
// rating — the review screen's progress count and end-of-session summary
// (UI overhaul spec §5).
type ReviewTally struct {
	Reviewed                int // new_count + review_count
	Again, Hard, Good, Easy int
}

// TodayTally sums flash_review_counts for now's UTC day, for one of
// userID's decks or (deckID nil) all of them. It reads the same counters
// DueQueue's daily limits use, so it survives a reload and needs no
// per-session state.
func (st *Store) TodayTally(ctx context.Context, userID int64, deckID *int64, now time.Time) (ReviewTally, error) {
	query := `
		SELECT coalesce(sum(new_count + review_count), 0),
		       coalesce(sum(again_count), 0), coalesce(sum(hard_count), 0),
		       coalesce(sum(good_count), 0), coalesce(sum(easy_count), 0)
		  FROM flash_review_counts
		 WHERE user_id = ? AND day = ?`
	args := []any{userID, formatDay(now)}
	if deckID != nil {
		query += ` AND deck_id = ?`
		args = append(args, *deckID)
	}
	var t ReviewTally
	if err := st.db.QueryRowContext(ctx, query, args...).Scan(&t.Reviewed, &t.Again, &t.Hard, &t.Good, &t.Easy); err != nil {
		return ReviewTally{}, fmt.Errorf("flash: today tally: %w", err)
	}
	return t, nil
}
