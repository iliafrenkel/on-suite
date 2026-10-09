package books

import (
	"context"
	"fmt"
	"strings"
	"unicode/utf8"
)

// MaxReviewRunes bounds a review: room for a long one, not for a book.
const MaxReviewRunes = 20000

// checkRating accepts 0 (no rating) to 5. Ratings come from buttons and a
// select, never typed, so anything else is a tampered form: ErrInvalid.
func checkRating(rating int) error {
	if rating < 0 || rating > 5 {
		return ErrInvalid
	}
	return nil
}

// SetRating rates one of userID's books 1–5, or clears its rating with 0.
// One rating per book, not per reading (spec "Decisions at a glance").
func (st *Store) SetRating(ctx context.Context, userID, id int64, rating int) error {
	if err := checkRating(rating); err != nil {
		return err
	}
	res, err := st.db.ExecContext(ctx,
		`UPDATE books_books SET rating = ?, updated_at = ? WHERE id = ? AND user_id = ?`,
		nullInt(rating), formatTime(st.now()), id, userID)
	if err != nil {
		return fmt.Errorf("books: set rating: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// SetReview replaces a book's review, Markdown as typed (line endings
// tidied, ends trimmed). "" removes it.
func (st *Store) SetReview(ctx context.Context, userID, id int64, review string) error {
	review = strings.TrimSpace(strings.ReplaceAll(review, "\r\n", "\n"))
	if utf8.RuneCountInString(review) > MaxReviewRunes {
		return &Refusal{Msg: fmt.Sprintf("Keep your review to %d characters or fewer.", MaxReviewRunes)}
	}
	res, err := st.db.ExecContext(ctx,
		`UPDATE books_books SET review = ?, updated_at = ? WHERE id = ? AND user_id = ?`,
		review, formatTime(st.now()), id, userID)
	if err != nil {
		return fmt.Errorf("books: set review: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}
