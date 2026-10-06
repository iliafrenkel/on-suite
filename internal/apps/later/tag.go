package later

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"unicode"

	"github.com/iliafrenkel/on-suite/internal/platform/db"
)

// MaxTagRunes bounds one tag name. Longer names are cut, not refused: a tag
// is a private label, and a form error inside a ⋯ menu would be worse than
// a shortened one.
const MaxTagRunes = 40

// MaxTags bounds how many tags one article keeps; the rest are dropped.
const MaxTags = 20

// ParseTags turns a comma-separated tags field into clean names, in the
// order typed: control characters become spaces, whitespace collapses,
// leading '#' go, names are lowercased and cut to MaxTagRunes, blanks and
// repeats are skipped, and at most MaxTags are kept.
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

// linkTags finds or creates each of userID's tags and links articleID to
// them, inside the caller's transaction. names must already be clean.
func linkTags(ctx context.Context, tx *sql.Tx, userID, articleID int64, names []string) error {
	for _, name := range names {
		var tagID int64
		// DO UPDATE (a no-op) rather than DO NOTHING, so RETURNING gives the
		// id of an existing tag too.
		if err := tx.QueryRowContext(ctx, `
			INSERT INTO later_tags (user_id, name) VALUES (?, ?)
			ON CONFLICT (user_id, name) DO UPDATE SET name = excluded.name
			RETURNING id`, userID, name).Scan(&tagID); err != nil {
			return fmt.Errorf("later: tag %q: %w", name, err)
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO later_article_tags (article_id, tag_id) VALUES (?, ?) ON CONFLICT DO NOTHING`,
			articleID, tagID); err != nil {
			return fmt.Errorf("later: link tag %q: %w", name, err)
		}
	}
	return nil
}

// gcTags deletes userID's tags that no article uses any more (spec: "Unused
// tags are removed when their last article loses them").
func gcTags(ctx context.Context, tx *sql.Tx, userID int64) error {
	if _, err := tx.ExecContext(ctx, `
		DELETE FROM later_tags
		 WHERE user_id = ?
		   AND NOT EXISTS (SELECT 1 FROM later_article_tags WHERE tag_id = later_tags.id)`, userID); err != nil {
		return fmt.Errorf("later: remove unused tags: %w", err)
	}
	return nil
}

// SetTags replaces an article's tags with names (cleaned here).
func (st *Store) SetTags(ctx context.Context, userID, id int64, names []string) error {
	tx, err := st.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("later: begin set tags: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	// The owner check runs inside the transaction, as Flash learned (#294):
	// with one connection, a check before it could race a delete.
	res, err := tx.ExecContext(ctx,
		`UPDATE later_articles SET updated_at = ? WHERE id = ? AND user_id = ?`,
		db.FormatTime(st.now()), id, userID)
	if err != nil {
		return fmt.Errorf("later: set tags: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM later_article_tags WHERE article_id = ?`, id); err != nil {
		return fmt.Errorf("later: clear tags: %w", err)
	}
	if err := linkTags(ctx, tx, userID, id, cleanTags(names)); err != nil {
		return err
	}
	if err := gcTags(ctx, tx, userID); err != nil {
		return err
	}
	return tx.Commit()
}

// ArticleTags returns the names of an article's tags, alphabetically; none
// for a missing or someone else's article.
func (st *Store) ArticleTags(ctx context.Context, userID, id int64) ([]string, error) {
	return st.names(ctx, `
		SELECT t.name FROM later_article_tags x
		  JOIN later_tags t ON t.id = x.tag_id
		  JOIN later_articles a ON a.id = x.article_id
		 WHERE a.id = ? AND a.user_id = ?
		 ORDER BY t.name`, id, userID)
}

// TagNames returns every tag userID has, alphabetically.
func (st *Store) TagNames(ctx context.Context, userID int64) ([]string, error) {
	return st.names(ctx, `SELECT name FROM later_tags WHERE user_id = ? ORDER BY name`, userID)
}

func (st *Store) names(ctx context.Context, query string, args ...any) ([]string, error) {
	rows, err := st.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("later: tags: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			return nil, fmt.Errorf("later: scan tag: %w", err)
		}
		out = append(out, n)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("later: tags: %w", err)
	}
	return out, nil
}
