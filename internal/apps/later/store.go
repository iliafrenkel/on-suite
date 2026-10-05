// Package later implements ON Later, a private read-it-later app: save a
// page, read it in a calm view, keep its images.
package later

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"math"
	"net/url"
	"strings"
	"time"

	"github.com/iliafrenkel/on-suite/internal/platform/db"
	"github.com/iliafrenkel/on-suite/internal/platform/webfetch"
)

// ID is the app id: URL prefix, migration namespace, table prefix.
const ID = "later"

//go:embed migrations/*.sql
var migrationFiles embed.FS

// Migrations is this app's schema, for the platform and for store tests.
func Migrations() fs.FS {
	sub, err := fs.Sub(migrationFiles, "migrations")
	if err != nil {
		panic("later: embedded migrations missing: " + err.Error()) // unreachable
	}
	return sub
}

var (
	// ErrNotFound is a missing row or somebody else's — indistinguishable
	// on purpose, so a handler answers 404 for both.
	ErrNotFound = errors.New("later: not found")
	// ErrInvalid is a request the store refuses (bad state, bad input).
	ErrInvalid = errors.New("later: invalid")
)

// Store is every SQL query ON Later runs.
type Store struct {
	db  *sql.DB
	now func() time.Time
}

// NewStore returns a store on handle reading the real clock in UTC.
func NewStore(handle *sql.DB) *Store {
	return &Store{db: handle, now: func() time.Time { return time.Now().UTC() }}
}

// SetClock replaces the time source, for tests.
func (st *Store) SetClock(now func() time.Time) { st.now = now }

// State is where an article sits in the reading flow.
type State string

// The three tabs of the list page.
const (
	StateUnread   State = "unread"
	StateReading  State = "reading"
	StateArchived State = "archived"
)

// Content says what the saved snapshot is.
type Content string

// How an article's text was obtained.
const (
	ContentExtracted Content = "extracted"
	ContentPasted    Content = "pasted"
	ContentLinkOnly  Content = "link_only"
)

// NewArticle is what saving a URL produced; ContentHTML "" means link-only.
type NewArticle struct {
	URL, Title, SiteName, Byline string
	ContentHTML                  string            // sanitised; "" means link-only
	Images                       map[string]string // hash -> source URL
	ExtractError                 string
	FaviconURL                   string   // absolute; "" when unknown
	Tags                         []string // raw names; Save cleans them
}

// Article is one saved article.
type Article struct {
	ID, UserID                   int64
	URL, Title, SiteName, Byline string
	SiteHost                     string
	Content                      Content
	ContentHTML, ContentText     string
	ExtractError                 string
	WordCount                    int
	State                        State
	Note                         string
	Progress                     float64
	SavedAt, UpdatedAt           time.Time
	OpenedAt, ArchivedAt         time.Time // zero when unset
}

// ListItem is the slice of an article a list row needs.
type ListItem struct {
	ID        int64
	Title     string
	SiteHost  string
	Content   Content
	WordCount int
	Progress  float64
	// FaviconHash is "" when the site has no favicon row; FaviconShown says
	// an <img> is worth emitting (cached, or not yet given up on).
	FaviconHash  string
	FaviconShown bool
	Highlights   int
	State        State
	Tags         []string // alphabetical
}

// articleColumns is every column scanArticle reads, in order.
const articleColumns = `id, user_id, url, title, site_name, byline, site_host, content,
	content_html, content_text, extract_error, word_count, state, note, progress,
	saved_at, opened_at, archived_at, updated_at`

type rowScanner interface{ Scan(dest ...any) error }

func scanArticle(row rowScanner) (Article, error) {
	var a Article
	var saved, updated string
	var opened, archived sql.NullString
	err := row.Scan(&a.ID, &a.UserID, &a.URL, &a.Title, &a.SiteName, &a.Byline, &a.SiteHost,
		&a.Content, &a.ContentHTML, &a.ContentText, &a.ExtractError, &a.WordCount, &a.State,
		&a.Note, &a.Progress, &saved, &opened, &archived, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return Article{}, ErrNotFound
	}
	if err != nil {
		return Article{}, fmt.Errorf("later: load article: %w", err)
	}
	if a.SavedAt, err = db.ParseTime(saved); err != nil {
		return Article{}, err
	}
	if a.UpdatedAt, err = db.ParseTime(updated); err != nil {
		return Article{}, err
	}
	if opened.Valid {
		if a.OpenedAt, err = db.ParseTime(opened.String); err != nil {
			return Article{}, err
		}
	}
	if archived.Valid {
		if a.ArchivedAt, err = db.ParseTime(archived.String); err != nil {
			return Article{}, err
		}
	}
	return a, nil
}

// siteHost is what the list shows as the site: the host, minus "www.".
func siteHost(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	return strings.TrimPrefix(u.Hostname(), "www.")
}

// Save stores a new article for userID, with its images, in one transaction.
// If the user already saved this URL nothing changes and the existing
// article comes back with created false.
func (st *Store) Save(ctx context.Context, userID int64, n NewArticle) (Article, bool, error) {
	now := db.FormatTime(st.now())
	host := siteHost(n.URL)
	title := strings.TrimSpace(n.Title)
	if title == "" {
		title = host
	}
	content, text, words := ContentLinkOnly, "", 0
	if n.ContentHTML != "" {
		content, text, words = ContentExtracted, ContentText(n.ContentHTML), WordCount(n.ContentHTML)
	}

	tx, err := st.db.BeginTx(ctx, nil)
	if err != nil {
		return Article{}, false, fmt.Errorf("later: begin save: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	res, err := tx.ExecContext(ctx, `
		INSERT INTO later_articles (user_id, url, title, site_name, byline, site_host, content,
			content_html, content_text, extract_error, word_count, saved_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (user_id, url) DO NOTHING`,
		userID, n.URL, title, n.SiteName, n.Byline, host, content,
		n.ContentHTML, text, n.ExtractError, words, now, now)
	if err != nil {
		return Article{}, false, fmt.Errorf("later: save article: %w", err)
	}
	if rows, _ := res.RowsAffected(); rows == 0 {
		_ = tx.Rollback()
		a, err := st.ArticleByURL(ctx, userID, n.URL)
		return a, false, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return Article{}, false, fmt.Errorf("later: save article id: %w", err)
	}
	for hash, src := range n.Images {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO later_images (hash, src_url) VALUES (?, ?) ON CONFLICT (hash) DO NOTHING`, hash, src); err != nil {
			return Article{}, false, fmt.Errorf("later: save image: %w", err)
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO later_article_images (article_id, hash) VALUES (?, ?) ON CONFLICT DO NOTHING`, id, hash); err != nil {
			return Article{}, false, fmt.Errorf("later: link image: %w", err)
		}
	}
	if n.FaviconURL != "" {
		h := webfetch.URLHash(n.FaviconURL)
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO later_favicons (hash, src_url) VALUES (?, ?) ON CONFLICT (hash) DO NOTHING`,
			h, n.FaviconURL); err != nil {
			return Article{}, false, fmt.Errorf("later: save favicon: %w", err)
		}
		if _, err := tx.ExecContext(ctx,
			// First icon wins, unless the mapped one has been given up on:
			// then a newly discovered icon repairs the site.
			`INSERT INTO later_site_favicons (site_host, hash) VALUES (?, ?)
			 ON CONFLICT (site_host) DO UPDATE SET hash = excluded.hash
			  WHERE excluded.hash <> later_site_favicons.hash
			    AND (SELECT error_count FROM later_favicons WHERE hash = later_site_favicons.hash) >= ?`,
			host, h, webfetch.MaxImageAttempts); err != nil {
			return Article{}, false, fmt.Errorf("later: link favicon: %w", err)
		}
	}
	if err := linkTags(ctx, tx, userID, id, cleanTags(n.Tags)); err != nil {
		return Article{}, false, err
	}
	if err := tx.Commit(); err != nil {
		return Article{}, false, fmt.Errorf("later: commit save: %w", err)
	}
	a, err := st.Article(ctx, userID, id)
	return a, true, err
}

// Article loads one of userID's articles.
func (st *Store) Article(ctx context.Context, userID, id int64) (Article, error) {
	return scanArticle(st.db.QueryRowContext(ctx,
		`SELECT `+articleColumns+` FROM later_articles WHERE id = ? AND user_id = ?`, id, userID))
}

// ArticleByURL loads the article userID saved under the (normalised) pageURL.
func (st *Store) ArticleByURL(ctx context.Context, userID int64, pageURL string) (Article, error) {
	return scanArticle(st.db.QueryRowContext(ctx,
		`SELECT `+articleColumns+` FROM later_articles WHERE user_id = ? AND url = ?`, userID, pageURL))
}

// listOrder is each tab's sort; the keys are the only states List accepts.
var listOrder = map[State]string{
	StateUnread:   "a.saved_at DESC, a.id DESC",
	StateReading:  "a.opened_at DESC, a.id DESC",
	StateArchived: "a.archived_at DESC, a.id DESC",
}

// listSelect is every column scanListItem reads: an article a, the favicon
// joins in listJoins, its highlight count and its tags joined by tagSep.
const listSelect = `a.id, a.title, a.site_host, a.content, a.word_count, a.progress, a.state,
	COALESCE(sf.hash, ''), f.bytes IS NOT NULL, COALESCE(f.error_count, 0), f.fetched_at,
	(SELECT count(*) FROM later_highlights h WHERE h.article_id = a.id),
	(SELECT COALESCE(group_concat(t.name, char(31) ORDER BY t.name), '')
	   FROM later_article_tags x JOIN later_tags t ON t.id = x.tag_id
	  WHERE x.article_id = a.id)`

// tagSep is char(31) in listSelect. ParseTags turns control characters into
// spaces, so no name contains it.
const tagSep = "\x1f"

const listJoins = `
	  LEFT JOIN later_site_favicons sf ON sf.site_host = a.site_host
	  LEFT JOIN later_favicons f ON f.hash = sf.hash`

// tagFilter keeps the articles a carrying one tag. It takes the tag name
// twice; "" keeps everything.
const tagFilter = `(? = '' OR EXISTS (
	SELECT 1 FROM later_article_tags x JOIN later_tags t ON t.id = x.tag_id
	 WHERE x.article_id = a.id AND t.name = ?))`

// scanListItem reads one listSelect row, then any extra columns into extra.
func scanListItem(row rowScanner, now time.Time, extra ...any) (ListItem, error) {
	var it ListItem
	var cached bool
	var errorCount int
	var fetched sql.NullString
	var tags string
	dest := append([]any{&it.ID, &it.Title, &it.SiteHost, &it.Content, &it.WordCount, &it.Progress, &it.State,
		&it.FaviconHash, &cached, &errorCount, &fetched, &it.Highlights, &tags}, extra...)
	if err := row.Scan(dest...); err != nil {
		return ListItem{}, fmt.Errorf("later: scan list: %w", err)
	}
	var attempt time.Time
	if fetched.Valid {
		var err error
		if attempt, err = db.ParseTime(fetched.String); err != nil {
			return ListItem{}, err
		}
	}
	it.FaviconShown = it.FaviconHash != "" && (cached || !webfetch.GivenUp(errorCount, attempt, now))
	if tags != "" {
		it.Tags = strings.Split(tags, tagSep)
	}
	return it, nil
}

// List returns one page of a tab, newest activity first, narrowed to the
// articles carrying tag unless tag is "".
func (st *Store) List(ctx context.Context, userID int64, state State, tag string, offset, limit int) ([]ListItem, error) {
	order, ok := listOrder[state]
	if !ok {
		return nil, ErrInvalid
	}
	// order comes only from the listOrder map above, never from input.
	rows, err := st.db.QueryContext(ctx, `
		SELECT `+listSelect+`
		  FROM later_articles a`+listJoins+`
		 WHERE a.user_id = ? AND a.state = ? AND `+tagFilter+`
		 ORDER BY `+order+`
		 LIMIT ? OFFSET ?`, userID, state, tag, tag, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("later: list: %w", err)
	}
	defer func() { _ = rows.Close() }()
	now := st.now()
	var items []ListItem
	for rows.Next() {
		it, err := scanListItem(rows, now)
		if err != nil {
			return nil, err
		}
		items = append(items, it)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("later: list: %w", err)
	}
	return items, nil
}

// Counts is how many articles userID has in each state, narrowed to tag
// unless it is ""; every state is present, 0 when empty.
func (st *Store) Counts(ctx context.Context, userID int64, tag string) (map[State]int, error) {
	counts := map[State]int{StateUnread: 0, StateReading: 0, StateArchived: 0}
	rows, err := st.db.QueryContext(ctx, `
		SELECT a.state, count(*) FROM later_articles a
		 WHERE a.user_id = ? AND `+tagFilter+`
		 GROUP BY a.state`, userID, tag, tag)
	if err != nil {
		return nil, fmt.Errorf("later: counts: %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var s State
		var n int
		if err := rows.Scan(&s, &n); err != nil {
			return nil, fmt.Errorf("later: scan counts: %w", err)
		}
		counts[s] = n
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("later: counts: %w", err)
	}
	return counts, nil
}

// exec runs one write and maps "no row matched" to ErrNotFound.
func (st *Store) exec(ctx context.Context, what, query string, args ...any) error {
	res, err := st.db.ExecContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("later: %s: %w", what, err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// MarkOpened records that the article was opened; an unread one becomes
// reading, a reading or archived one keeps its state.
func (st *Store) MarkOpened(ctx context.Context, userID, id int64) error {
	now := db.FormatTime(st.now())
	return st.exec(ctx, "mark opened", `
		UPDATE later_articles
		   SET opened_at = ?, updated_at = ?,
		       state = CASE state WHEN 'unread' THEN 'reading' ELSE state END
		 WHERE id = ? AND user_id = ?`, now, now, id, userID)
}

// SetState archives or un-archives an article. Reading is reached only by
// opening, so asking for it (or an unknown state) is ErrInvalid.
// Moving to unread also resets progress to 0.
func (st *Store) SetState(ctx context.Context, userID, id int64, s State) error {
	now := db.FormatTime(st.now())
	switch s {
	case StateArchived:
		return st.exec(ctx, "archive", `
			UPDATE later_articles SET state = 'archived', archived_at = ?, updated_at = ?
			 WHERE id = ? AND user_id = ?`, now, now, id, userID)
	case StateUnread:
		return st.exec(ctx, "unarchive", `
			UPDATE later_articles SET state = 'unread', progress = 0, archived_at = NULL, opened_at = NULL, updated_at = ?
			 WHERE id = ? AND user_id = ?`, now, id, userID)
	default:
		return ErrInvalid
	}
}

// SetProgress records how far through an article the reader has scrolled,
// 0..1. It never changes the article's state.
func (st *Store) SetProgress(ctx context.Context, userID, id int64, p float64) error {
	if math.IsNaN(p) || math.IsInf(p, 0) {
		return ErrInvalid
	}
	p = min(1, max(0, p))
	return st.exec(ctx, "progress", `
		UPDATE later_articles SET progress = ?, updated_at = ? WHERE id = ? AND user_id = ?`,
		p, db.FormatTime(st.now()), id, userID)
}

// SetNote replaces the article's note (spec: one plain-text note per
// article); blank removes it.
func (st *Store) SetNote(ctx context.Context, userID, id int64, note string) error {
	return st.exec(ctx, "note", `
		UPDATE later_articles SET note = ?, updated_at = ? WHERE id = ? AND user_id = ?`,
		cleanText(note), db.FormatTime(st.now()), id, userID)
}

// SetPastedText gives a link-only article the text the user pasted.
func (st *Store) SetPastedText(ctx context.Context, userID, id int64, text string) error {
	a, err := st.Article(ctx, userID, id)
	if err != nil {
		return err
	}
	if a.Content != ContentLinkOnly {
		return ErrInvalid
	}
	html := PastedHTML(text)
	if html == "" {
		return ErrInvalid
	}
	return st.exec(ctx, "paste text", `
		UPDATE later_articles
		   SET content = 'pasted', content_html = ?, content_text = ?, word_count = ?,
		       extract_error = '', updated_at = ?
		 WHERE id = ? AND user_id = ? AND content = 'link_only'`,
		html, ContentText(html), WordCount(html), db.FormatTime(st.now()), id, userID)
}

// Delete removes an article, its highlights, its tags that nothing else
// uses, and every image no other article still uses.
func (st *Store) Delete(ctx context.Context, userID, id int64) error {
	tx, err := st.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("later: begin delete: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	rows, err := tx.QueryContext(ctx, `
		SELECT ai.hash FROM later_article_images ai
		  JOIN later_articles a ON a.id = ai.article_id
		 WHERE a.id = ? AND a.user_id = ?`, id, userID)
	if err != nil {
		return fmt.Errorf("later: list article images: %w", err)
	}
	var hashes []string
	for rows.Next() {
		var h string
		if err := rows.Scan(&h); err != nil {
			_ = rows.Close()
			return fmt.Errorf("later: scan article image: %w", err)
		}
		hashes = append(hashes, h)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return fmt.Errorf("later: list article images: %w", err)
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("later: list article images: %w", err)
	}

	res, err := tx.ExecContext(ctx, `DELETE FROM later_articles WHERE id = ? AND user_id = ?`, id, userID)
	if err != nil {
		return fmt.Errorf("later: delete article: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	for _, h := range hashes {
		if _, err := tx.ExecContext(ctx, `
			DELETE FROM later_images WHERE hash = ?
			   AND NOT EXISTS (SELECT 1 FROM later_article_images WHERE hash = ?)`, h, h); err != nil {
			return fmt.Errorf("later: delete orphan image: %w", err)
		}
	}
	// The article's tag links went with it (ON DELETE CASCADE); its tags go
	// too if nothing else uses them (spec: "Delete").
	if err := gcTags(ctx, tx, userID); err != nil {
		return err
	}
	return tx.Commit()
}
