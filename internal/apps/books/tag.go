package books

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"unicode"
)

// MaxTagRunes bounds one tag name; longer names are cut, not refused.
const MaxTagRunes = 40

// MaxTags bounds how many tags one book keeps; the rest are dropped.
const MaxTags = 20

// ParseTags turns a comma-separated tags field into clean names, in the
// order typed: control characters become spaces, whitespace collapses,
// leading '#' go, names are lowercased and cut to MaxTagRunes, blanks and
// repeats are skipped, and at most MaxTags are kept. It mirrors ON Later's
// own ParseTags — apps never import each other, so this is an independent
// copy (PATTERNS.md "Cross-app mirroring").
func ParseTags(raw string) []string {
	var out []string
	seen := make(map[string]bool)
	for _, part := range strings.Split(raw, ",") {
		part = strings.Map(func(r rune) rune {
			if unicode.IsControl(r) {
				return ' '
			}
			return r
		}, part)
		name := strings.ToLower(strings.Join(strings.Fields(part), " "))
		name = strings.TrimSpace(strings.TrimLeft(name, "#"))
		if r := []rune(name); len(r) > MaxTagRunes {
			name = strings.TrimSpace(string(r[:MaxTagRunes]))
		}
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		out = append(out, name)
		if len(out) == MaxTags {
			break
		}
	}
	return out
}

// cleanTags applies ParseTags to names that may not have come through it.
func cleanTags(names []string) []string { return ParseTags(strings.Join(names, ",")) }

// linkTags finds or creates each of userID's tags and links bookID to
// them, inside the caller's transaction. names must already be clean.
func linkTags(ctx context.Context, tx *sql.Tx, userID, bookID int64, names []string) error {
	for _, name := range names {
		var tagID int64
		// DO UPDATE (a no-op) rather than DO NOTHING, so RETURNING gives the
		// id of an existing tag too.
		if err := tx.QueryRowContext(ctx, `
			INSERT INTO books_tags (user_id, name) VALUES (?, ?)
			ON CONFLICT (user_id, name) DO UPDATE SET name = excluded.name
			RETURNING id`, userID, name).Scan(&tagID); err != nil {
			return fmt.Errorf("books: tag %q: %w", name, err)
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO books_book_tags (book_id, tag_id) VALUES (?, ?) ON CONFLICT DO NOTHING`,
			bookID, tagID); err != nil {
			return fmt.Errorf("books: link tag %q: %w", name, err)
		}
	}
	return nil
}

// gcTags deletes userID's tags that no book uses any more.
func gcTags(ctx context.Context, tx *sql.Tx, userID int64) error {
	if _, err := tx.ExecContext(ctx, `
		DELETE FROM books_tags
		 WHERE user_id = ?
		   AND NOT EXISTS (SELECT 1 FROM books_book_tags WHERE tag_id = books_tags.id)`, userID); err != nil {
		return fmt.Errorf("books: remove unused tags: %w", err)
	}
	return nil
}

// SetTags replaces a book's tags with names (cleaned here).
func (st *Store) SetTags(ctx context.Context, userID, id int64, names []string) error {
	tx, err := st.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("books: begin set tags: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if err := st.touch(ctx, tx, userID, id); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM books_book_tags WHERE book_id = ?`, id); err != nil {
		return fmt.Errorf("books: clear tags: %w", err)
	}
	if err := linkTags(ctx, tx, userID, id, cleanTags(names)); err != nil {
		return err
	}
	if err := gcTags(ctx, tx, userID); err != nil {
		return err
	}
	return tx.Commit()
}

// BookTags returns the names of a book's tags, alphabetically; none for a
// missing or someone else's book.
func (st *Store) BookTags(ctx context.Context, userID, id int64) ([]string, error) {
	return st.names(ctx, `
		SELECT t.name FROM books_book_tags x
		  JOIN books_tags t ON t.id = x.tag_id
		  JOIN books_books b ON b.id = x.book_id
		 WHERE b.id = ? AND b.user_id = ?
		 ORDER BY t.name`, id, userID)
}

// TagNames returns every tag userID has, alphabetically.
func (st *Store) TagNames(ctx context.Context, userID int64) ([]string, error) {
	return st.names(ctx, `SELECT name FROM books_tags WHERE user_id = ? ORDER BY name`, userID)
}

func (st *Store) names(ctx context.Context, query string, args ...any) ([]string, error) {
	rows, err := st.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("books: tags: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			return nil, fmt.Errorf("books: scan tag: %w", err)
		}
		out = append(out, n)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("books: tags: %w", err)
	}
	return out, nil
}
