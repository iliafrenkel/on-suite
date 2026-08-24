// Package paste implements ON Paste, a place to keep and share snippets of
// code or text.
//
// It depends only on internal/platform/*. It never imports another app, and no
// platform package imports it: the whole coupling is the app.App interface plus
// one line in cmd/onsuite/main.go.
package paste

import (
	"context"
	"crypto/rand"
	"database/sql"
	"embed"
	"encoding/base64"
	"errors"
	"fmt"
	"io/fs"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/iliafrenkel/on-suite/internal/platform/app"
)

// ID is the app id: the URL prefix, the migration namespace, and the prefix on
// every table this app owns.
const ID = "paste"

const (
	// MaxTitleRunes bounds a title so the list page stays readable.
	MaxTitleRunes = 120
	// MaxBodyBytes bounds a snippet. The form that carries it is
	// application/x-www-form-urlencoded, which percent-encodes every
	// non-ASCII UTF-8 byte as "%XX" — a 3x expansion in the worst case
	// (Cyrillic, Hebrew, CJK, ...). At the platform's 1 MiB request limit
	// (web.DefaultMaxBodyBytes), a naive "half the limit" bound can still be
	// blown by a snippet that is mostly non-Latin text, which makes the body
	// read fail and surfaces as a confusing CSRF-looking 403 rather than a
	// validation message. 256 KiB keeps the worst case (768 KiB) safely under
	// the limit with headroom for the form's other fields, and is still a
	// very large snippet.
	MaxBodyBytes = 256 << 10
	// shareSlugBytes is 16 bytes, 128 bits, base64url encoded to 22
	// characters. The slug is the only protection on a shared snippet, so it
	// must be unguessable rather than merely unique.
	shareSlugBytes = 16
	// DefaultListLimit caps the list page.
	DefaultListLimit = 200
)

//go:embed migrations/*.sql
var migrationFiles embed.FS

// Migrations returns this app's schema with filenames at the root, which is
// what db.Collect expects.
func Migrations() fs.FS {
	sub, err := fs.Sub(migrationFiles, "migrations")
	if err != nil {
		// Unreachable: the path is a compile-time constant checked by go:embed.
		panic("paste: embedded migrations missing: " + err.Error())
	}
	return sub
}

var (
	// ErrNotFound covers both "no such snippet" and "not yours". They are
	// deliberately indistinguishable: returning a 403 for someone else's
	// snippet would confirm that it exists.
	ErrNotFound = errors.New("paste: not found")
	ErrInvalid  = errors.New("paste: invalid snippet")
)

// Snippet is one stored snippet.
type Snippet struct {
	ID        int64
	UserID    int64
	Title     string
	Language  string // Chroma lexer name, or "" for detect-from-content
	Body      string
	ShareSlug string // "" when not shared
	CreatedAt time.Time
}

// Shared reports whether a public link currently exists.
func (s Snippet) Shared() bool { return s.ShareSlug != "" }

// Lines counts the lines in the body, for display.
func (s Snippet) Lines() int {
	if s.Body == "" {
		return 0
	}
	return strings.Count(strings.TrimSuffix(s.Body, "\n"), "\n") + 1
}

// LinesLabel is Lines with correct singular/plural wording, so a one-line
// snippet does not read "1 lines".
func (s Snippet) LinesLabel() string {
	n := s.Lines()
	if n == 1 {
		return "1 line"
	}
	return fmt.Sprintf("%d lines", n)
}

// DisplayTitle is what to show when a snippet was saved without a title.
func (s Snippet) DisplayTitle() string {
	if strings.TrimSpace(s.Title) == "" {
		return "Untitled"
	}
	return s.Title
}

// Store is all the SQL for ON Paste. It has no HTTP knowledge and does not
// import Chroma, so it can be tested against a real database on its own.
type Store struct {
	db  *sql.DB
	now func() time.Time
}

func NewStore(handle *sql.DB) *Store {
	return &Store{db: handle, now: func() time.Time { return time.Now().UTC() }}
}

// SetClock replaces the time source, for tests.
func (st *Store) SetClock(now func() time.Time) { st.now = now }

// Validate checks a snippet's user-supplied fields. Exported because the
// handler reports these messages back to the user.
func Validate(title, body string) error {
	if strings.TrimSpace(body) == "" {
		return fmt.Errorf("%w: the snippet is empty", ErrInvalid)
	}
	if len(body) > MaxBodyBytes {
		return fmt.Errorf("%w: the snippet is larger than %d KiB", ErrInvalid, MaxBodyBytes>>10)
	}
	if !utf8.ValidString(body) {
		return fmt.Errorf("%w: the snippet is not valid UTF-8", ErrInvalid)
	}
	if utf8.RuneCountInString(title) > MaxTitleRunes {
		return fmt.Errorf("%w: the title is longer than %d characters", ErrInvalid, MaxTitleRunes)
	}
	if !utf8.ValidString(title) {
		return fmt.Errorf("%w: the title is not valid UTF-8", ErrInvalid)
	}
	return nil
}

// Create stores a new snippet. It is never shared to begin with.
func (st *Store) Create(ctx context.Context, userID int64, title, language, body string) (Snippet, error) {
	title = strings.TrimSpace(title)
	if err := Validate(title, body); err != nil {
		return Snippet{}, err
	}

	s := Snippet{
		UserID:    userID,
		Title:     title,
		Language:  language,
		Body:      body,
		CreatedAt: st.now(),
	}
	err := st.db.QueryRowContext(ctx,
		`INSERT INTO paste_snippets (user_id, title, language, body, share_slug, created_at)
		 VALUES (?, ?, ?, ?, NULL, ?)
		 RETURNING id`,
		s.UserID, s.Title, s.Language, s.Body, formatTime(s.CreatedAt),
	).Scan(&s.ID)
	if err != nil {
		return Snippet{}, fmt.Errorf("paste: create: %w", err)
	}
	return s, nil
}

// ByID fetches one of userID's own snippets.
func (st *Store) ByID(ctx context.Context, userID, id int64) (Snippet, error) {
	return scanSnippet(st.db.QueryRowContext(ctx,
		`SELECT id, user_id, title, language, body, share_slug, created_at
		 FROM paste_snippets WHERE id = ? AND user_id = ?`, id, userID))
}

// ByShareSlug fetches a snippet by its public slug, with no owner check —
// possessing the slug is the authorisation.
func (st *Store) ByShareSlug(ctx context.Context, slug string) (Snippet, error) {
	if slug == "" {
		return Snippet{}, ErrNotFound
	}
	return scanSnippet(st.db.QueryRowContext(ctx,
		`SELECT id, user_id, title, language, body, share_slug, created_at
		 FROM paste_snippets WHERE share_slug = ?`, slug))
}

// List returns userID's snippets, newest first. Bodies are included because
// the list shows a preview; at this scale that is cheaper than a second query.
func (st *Store) List(ctx context.Context, userID int64, limit int) ([]Snippet, error) {
	if limit <= 0 || limit > DefaultListLimit {
		limit = DefaultListLimit
	}
	rows, err := st.db.QueryContext(ctx,
		`SELECT id, user_id, title, language, body, share_slug, created_at
		 FROM paste_snippets WHERE user_id = ?
		 ORDER BY created_at DESC, id DESC
		 LIMIT ?`, userID, limit)
	if err != nil {
		return nil, fmt.Errorf("paste: list: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []Snippet
	for rows.Next() {
		s, err := scanSnippetRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("paste: list: %w", err)
	}
	return out, nil
}

// All returns every one of userID's snippets, oldest first.
//
// It exists separately from List because List is capped for the web page,
// whereas an export must be complete or it is not an export.
func (st *Store) All(ctx context.Context, userID int64) ([]Snippet, error) {
	rows, err := st.db.QueryContext(ctx,
		`SELECT id, user_id, title, language, body, share_slug, created_at
		 FROM paste_snippets WHERE user_id = ?
		 ORDER BY created_at ASC, id ASC`, userID)
	if err != nil {
		return nil, fmt.Errorf("paste: all: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []Snippet
	for rows.Next() {
		s, err := scanSnippetRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("paste: all: %w", err)
	}
	return out, nil
}

// Delete removes one of userID's own snippets.
func (st *Store) Delete(ctx context.Context, userID, id int64) error {
	res, err := st.db.ExecContext(ctx,
		`DELETE FROM paste_snippets WHERE id = ? AND user_id = ?`, id, userID)
	if err != nil {
		return fmt.Errorf("paste: delete: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("paste: delete: %w", err)
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// Share mints a fresh slug and returns it.
//
// Re-sharing always generates a new slug rather than reusing the old one, so a
// link that was revoked stays dead even if sharing is turned back on.
func (st *Store) Share(ctx context.Context, userID, id int64) (string, error) {
	slug, err := newShareSlug()
	if err != nil {
		return "", err
	}
	res, err := st.db.ExecContext(ctx,
		`UPDATE paste_snippets SET share_slug = ? WHERE id = ? AND user_id = ?`,
		slug, id, userID)
	if err != nil {
		return "", fmt.Errorf("paste: share: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return "", fmt.Errorf("paste: share: %w", err)
	}
	if n == 0 {
		return "", ErrNotFound
	}
	return slug, nil
}

// Unshare revokes the public link.
func (st *Store) Unshare(ctx context.Context, userID, id int64) error {
	res, err := st.db.ExecContext(ctx,
		`UPDATE paste_snippets SET share_slug = NULL WHERE id = ? AND user_id = ?`,
		id, userID)
	if err != nil {
		return fmt.Errorf("paste: unshare: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("paste: unshare: %w", err)
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// rowScanner is satisfied by both *sql.Row and *sql.Rows.
type rowScanner interface {
	Scan(dest ...any) error
}

func scanSnippet(row *sql.Row) (Snippet, error) {
	s, err := scanSnippetRow(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Snippet{}, ErrNotFound
	}
	return s, err
}

func scanSnippetRow(row rowScanner) (Snippet, error) {
	var (
		s         Snippet
		slug      sql.NullString
		createdAt string
	)
	err := row.Scan(&s.ID, &s.UserID, &s.Title, &s.Language, &s.Body, &slug, &createdAt)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return Snippet{}, sql.ErrNoRows // translated by scanSnippet
	case err != nil:
		return Snippet{}, fmt.Errorf("paste: scan: %w", err)
	}
	s.ShareSlug = slug.String // "" when NULL, since Valid is false

	if s.CreatedAt, err = parseTime(createdAt); err != nil {
		return Snippet{}, err
	}
	return s, nil
}

func newShareSlug() (string, error) {
	buf := make([]byte, shareSlugBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("paste: generate share slug: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

// Timestamps match the platform's convention: RFC 3339 nanoseconds in UTC,
// which sorts chronologically as text.
func formatTime(t time.Time) string { return t.UTC().Format(time.RFC3339Nano) }

func parseTime(s string) (time.Time, error) {
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return time.Time{}, fmt.Errorf("paste: parse timestamp %q: %w", s, err)
	}
	return t.UTC(), nil
}

// Stats is the whole app in five numbers, for the admin page. It implements
// the data half of app.Stater; App.Stats is the thin wrapper the platform
// type-asserts for.
//
// Every value is formatted here rather than by the platform: only this
// package knows that a body length is bytes and should read as "1.2 MB".
func (st *Store) Stats(ctx context.Context) ([]app.Stat, error) {
	var (
		count, shared, totalBytes, largest int64
		newest                             sql.NullString
	)
	err := st.db.QueryRowContext(ctx,
		`SELECT count(*),
		        coalesce(sum(CASE WHEN share_slug IS NOT NULL THEN 1 ELSE 0 END), 0),
		        coalesce(sum(length(body)), 0),
		        coalesce(max(length(body)), 0),
		        max(created_at)
		   FROM paste_snippets`).
		Scan(&count, &shared, &totalBytes, &largest, &newest)
	if err != nil {
		return nil, fmt.Errorf("paste: stats: %w", err)
	}

	newestLabel := "never"
	if newest.Valid {
		t, err := parseTime(newest.String)
		if err != nil {
			return nil, err
		}
		newestLabel = t.Format("2006-01-02 15:04 MST")
	}

	return []app.Stat{
		{Label: "Snippets", Value: strconv.FormatInt(count, 10)},
		{Label: "Shared", Value: strconv.FormatInt(shared, 10),
			Hint: "readable by anyone holding the link"},
		{Label: "Total size", Value: humanBytes(totalBytes), Hint: "snippet bodies only"},
		{Label: "Largest", Value: humanBytes(largest)},
		{Label: "Newest", Value: newestLabel},
	}, nil
}

// humanBytes renders a byte count the way a person reads one.
func humanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return strconv.FormatInt(n, 10) + " B"
	}
	div, exp := int64(unit), 0
	for n/div >= unit && exp < 3 {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGT"[exp])
}
