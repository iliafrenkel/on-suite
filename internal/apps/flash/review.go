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
	if err := st.bumpDailyCounts(ctx, tx, userID, deckID, now, newInc, reviewInc); err != nil {
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
	if err := st.bumpDailyCounts(ctx, tx, userID, deckID, now, newDec, reviewDec); err != nil {
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
// creating the row if this is the first count of the day.
func (st *Store) bumpDailyCounts(ctx context.Context, tx *sql.Tx, userID, deckID int64, now time.Time, newDelta, reviewDelta int) error {
	_, err := tx.ExecContext(ctx, `
		INSERT INTO flash_review_counts (user_id, deck_id, day, new_count, review_count)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT (user_id, deck_id, day) DO UPDATE SET
			new_count = new_count + excluded.new_count,
			review_count = review_count + excluded.review_count`,
		userID, deckID, formatDay(now), newDelta, reviewDelta)
	if err != nil {
		return fmt.Errorf("flash: update daily counts: %w", err)
	}
	return nil
}

// DailyCountsForTest exposes flash_review_counts to tests outside this
// package, for asserting GradeCard/UndoLastGrade updated the right counter.
func (st *Store) DailyCountsForTest(ctx context.Context, userID, deckID int64, now time.Time) (newCount, reviewCount int, err error) {
	err = st.db.QueryRowContext(ctx, `
		SELECT new_count, review_count FROM flash_review_counts
		WHERE user_id = ? AND deck_id = ? AND day = ?`, userID, deckID, formatDay(now),
	).Scan(&newCount, &reviewCount)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, 0, nil
	}
	if err != nil {
		return 0, 0, fmt.Errorf("flash: daily counts for test: %w", err)
	}
	return newCount, reviewCount, nil
}
