// Package reader implements ON Reader, an RSS and Atom feed reader.
//
// It depends only on internal/platform/*. It never imports another app, and no
// platform package imports it: the whole coupling is the app.App interface plus
// one line in cmd/onsuite/main.go.
package reader

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"strings"
	"time"
)

// ID is the app id: the URL prefix, the migration namespace, and the prefix on
// every table this app owns.
const ID = "reader"

// DefaultFetchInterval is how often a feed is polled when it has no override.
const DefaultFetchInterval = 30 * time.Minute

// ErrNotFound is returned when a row does not exist, or exists but belongs to
// somebody else. The two cases are deliberately indistinguishable: a caller
// must not be able to probe for another user's subscription ids.
var ErrNotFound = errors.New("reader: not found")

//go:embed migrations/*.sql
var migrationFiles embed.FS

// Migrations holds this app's .sql files at the root of the returned FS.
func Migrations() fs.FS {
	sub, err := fs.Sub(migrationFiles, "migrations")
	if err != nil {
		// Unreachable: a compile-time constant path checked by go:embed.
		panic("reader: embedded migrations missing: " + err.Error())
	}
	return sub
}

// Store is the only thing in this package that touches SQL.
type Store struct{ db *sql.DB }

func NewStore(handle *sql.DB) *Store { return &Store{db: handle} }

// Feed is one globally shared feed row.
type Feed struct {
	ID            int64
	URL           string
	ResolvedURL   string
	Title         string
	SiteURL       string
	ETag          string
	LastModified  string
	LastStatus    int
	LastError     string
	ErrorCount    int
	NextFetchAt   time.Time
	FetchInterval time.Duration // zero means DefaultFetchInterval
}

// Interval is the effective polling interval for this feed.
func (f Feed) Interval() time.Duration {
	if f.FetchInterval <= 0 {
		return DefaultFetchInterval
	}
	return f.FetchInterval
}

// Folder is one user's folder. There is no nesting.
type Folder struct {
	ID       int64
	Name     string
	Position int
}

// Subscription is one user's link to a feed.
type Subscription struct {
	ID       int64
	FeedID   int64
	FolderID *int64
	Title    string // override; empty means use FeedTitle
	FeedURL  string
	FeedName string
	AddedAt  time.Time
}

// DisplayName is the override when set, then the feed's own title, then the
// URL — so a feed that has never been polled successfully is still nameable.
func (s Subscription) DisplayName() string {
	switch {
	case s.Title != "":
		return s.Title
	case s.FeedName != "":
		return s.FeedName
	default:
		return s.FeedURL
	}
}

// TreeFolder is a folder with the subscriptions inside it.
type TreeFolder struct {
	Folder
	Subs []Subscription
}

// Tree is the whole sidebar for one user: folders, then loose subscriptions.
type Tree struct {
	Folders []TreeFolder
	Root    []Subscription
}

// timeFmt is RFC3339 in UTC. Stored as TEXT because SQLite has no time type
// and lexical order then matches chronological order, which the due index
// depends on.
const timeFmt = time.RFC3339Nano

func formatTime(t time.Time) string { return t.UTC().Format(timeFmt) }

func parseTime(s string) time.Time {
	t, err := time.Parse(timeFmt, s)
	if err != nil {
		return time.Time{}
	}
	return t.UTC()
}

// NormalizeFeedURL trims a feed URL and rejects anything that is not an
// absolute http or https URL. Normalising here rather than at each call site
// is what makes "one row per feed URL" actually hold.
func NormalizeFeedURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", fmt.Errorf("%w: empty feed URL", ErrInvalidURL)
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("%w: %s", ErrInvalidURL, raw)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", fmt.Errorf("%w: scheme %q is not http or https", ErrInvalidURL, u.Scheme)
	}
	if u.Host == "" {
		return "", fmt.Errorf("%w: no host in %s", ErrInvalidURL, raw)
	}
	u.Fragment = ""
	return u.String(), nil
}

// ErrInvalidURL is a feed URL that is not an absolute http(s) URL.
var ErrInvalidURL = errors.New("reader: invalid feed URL")

// ErrInvalid is any other rejected input, such as an empty folder name. It is
// separate from ErrInvalidURL so a handler can put the message on the right
// form field instead of guessing.
var ErrInvalid = errors.New("reader: invalid input")

// Subscribe adds a subscription, creating the shared feed row if this is the
// first subscriber. It is idempotent: subscribing twice returns the existing
// subscription rather than failing, because the UI's "add feed" box is exactly
// the place someone pastes the same URL twice.
func (s *Store) Subscribe(ctx context.Context, userID int64, rawURL string, folderID *int64) (Subscription, error) {
	feedURL, err := NormalizeFeedURL(rawURL)
	if err != nil {
		return Subscription{}, err
	}
	now := time.Now().UTC()

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Subscription{}, fmt.Errorf("reader: begin subscribe: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	// A new feed is due immediately, so adding one shows articles on the next
	// tick rather than in half an hour.
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO reader_feeds (url, resolved_url, title, site_url, next_fetch_at)
		VALUES (?, ?, '', '', ?)
		ON CONFLICT (url) DO NOTHING`,
		feedURL, feedURL, formatTime(now)); err != nil {
		return Subscription{}, fmt.Errorf("reader: upsert feed: %w", err)
	}

	var feedID int64
	if err := tx.QueryRowContext(ctx,
		`SELECT id FROM reader_feeds WHERE url = ?`, feedURL).Scan(&feedID); err != nil {
		return Subscription{}, fmt.Errorf("reader: load feed: %w", err)
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO reader_subs (user_id, feed_id, folder_id, added_at)
		VALUES (?, ?, ?, ?)
		ON CONFLICT (user_id, feed_id) DO NOTHING`,
		userID, feedID, folderID, formatTime(now)); err != nil {
		return Subscription{}, fmt.Errorf("reader: insert subscription: %w", err)
	}

	var sub Subscription
	var added string
	if err := tx.QueryRowContext(ctx, `
		SELECT s.id, s.feed_id, s.folder_id, s.title, s.added_at, f.url, f.title
		  FROM reader_subs s JOIN reader_feeds f ON f.id = s.feed_id
		 WHERE s.user_id = ? AND s.feed_id = ?`,
		userID, feedID).Scan(&sub.ID, &sub.FeedID, &sub.FolderID, &sub.Title,
		&added, &sub.FeedURL, &sub.FeedName); err != nil {
		return Subscription{}, fmt.Errorf("reader: load subscription: %w", err)
	}
	sub.AddedAt = parseTime(added)

	if err := tx.Commit(); err != nil {
		return Subscription{}, fmt.Errorf("reader: commit subscribe: %w", err)
	}
	return sub, nil
}

// Unsubscribe removes one subscription and, if it was the last one, the shared
// feed with it. Items and (from R3) cached images cascade away with the feed.
func (s *Store) Unsubscribe(ctx context.Context, userID, subID int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("reader: begin unsubscribe: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var feedID int64
	// The user_id predicate is the ownership check. A subscription belonging
	// to somebody else is reported exactly as one that does not exist.
	if err := tx.QueryRowContext(ctx,
		`SELECT feed_id FROM reader_subs WHERE id = ? AND user_id = ?`,
		subID, userID).Scan(&feedID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		return fmt.Errorf("reader: load subscription: %w", err)
	}

	if _, err := tx.ExecContext(ctx,
		`DELETE FROM reader_subs WHERE id = ? AND user_id = ?`, subID, userID); err != nil {
		return fmt.Errorf("reader: delete subscription: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
		DELETE FROM reader_feeds
		 WHERE id = ? AND NOT EXISTS (SELECT 1 FROM reader_subs WHERE feed_id = ?)`,
		feedID, feedID); err != nil {
		return fmt.Errorf("reader: delete orphaned feed: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("reader: commit unsubscribe: %w", err)
	}
	return nil
}

// CreateFolder adds a folder. Names are unique per user.
func (s *Store) CreateFolder(ctx context.Context, userID int64, name string) (Folder, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return Folder{}, fmt.Errorf("%w: empty folder name", ErrInvalid)
	}
	res, err := s.db.ExecContext(ctx, `
		INSERT INTO reader_folders (user_id, name, position)
		VALUES (?, ?, (SELECT coalesce(max(position), 0) + 1 FROM reader_folders WHERE user_id = ?))`,
		userID, name, userID)
	if err != nil {
		return Folder{}, fmt.Errorf("reader: insert folder: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return Folder{}, fmt.Errorf("reader: folder id: %w", err)
	}
	return Folder{ID: id, Name: name}, nil
}

// DeleteFolder removes a folder. Its subscriptions fall back to the root of
// the tree rather than being deleted — that is the ON DELETE SET NULL on
// reader_subs.folder_id, not something this method has to do.
func (s *Store) DeleteFolder(ctx context.Context, userID, folderID int64) error {
	res, err := s.db.ExecContext(ctx,
		`DELETE FROM reader_folders WHERE id = ? AND user_id = ?`, folderID, userID)
	if err != nil {
		return fmt.Errorf("reader: delete folder: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("reader: delete folder rows: %w", err)
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// Tree returns one user's whole sidebar in two queries.
func (s *Store) Tree(ctx context.Context, userID int64) (Tree, error) {
	var out Tree

	rows, err := s.db.QueryContext(ctx,
		`SELECT id, name, position FROM reader_folders WHERE user_id = ? ORDER BY position, name`,
		userID)
	if err != nil {
		return Tree{}, fmt.Errorf("reader: list folders: %w", err)
	}
	defer func() { _ = rows.Close() }()

	byID := map[int64]int{}
	for rows.Next() {
		var f Folder
		if err := rows.Scan(&f.ID, &f.Name, &f.Position); err != nil {
			return Tree{}, fmt.Errorf("reader: scan folder: %w", err)
		}
		byID[f.ID] = len(out.Folders)
		out.Folders = append(out.Folders, TreeFolder{Folder: f})
	}
	if err := rows.Err(); err != nil {
		return Tree{}, fmt.Errorf("reader: iterate folders: %w", err)
	}

	subRows, err := s.db.QueryContext(ctx, `
		SELECT s.id, s.feed_id, s.folder_id, s.title, s.added_at, f.url, f.title
		  FROM reader_subs s JOIN reader_feeds f ON f.id = s.feed_id
		 WHERE s.user_id = ?
		 ORDER BY s.position, coalesce(nullif(s.title, ''), nullif(f.title, ''), f.url)`,
		userID)
	if err != nil {
		return Tree{}, fmt.Errorf("reader: list subscriptions: %w", err)
	}
	defer func() { _ = subRows.Close() }()

	for subRows.Next() {
		var sub Subscription
		var added string
		if err := subRows.Scan(&sub.ID, &sub.FeedID, &sub.FolderID, &sub.Title,
			&added, &sub.FeedURL, &sub.FeedName); err != nil {
			return Tree{}, fmt.Errorf("reader: scan subscription: %w", err)
		}
		sub.AddedAt = parseTime(added)
		if sub.FolderID != nil {
			if i, ok := byID[*sub.FolderID]; ok {
				out.Folders[i].Subs = append(out.Folders[i].Subs, sub)
				continue
			}
		}
		out.Root = append(out.Root, sub)
	}
	if err := subRows.Err(); err != nil {
		return Tree{}, fmt.Errorf("reader: iterate subscriptions: %w", err)
	}
	return out, nil
}

// Item is one stored article.
type Item struct {
	ID          int64
	FeedID      int64
	GUID        string
	URL         string
	Title       string
	Author      string
	PublishedAt time.Time
	FetchedAt   time.Time
	SummaryHTML string
	ContentHTML string
	FeedName    string
}

// Body is what the article pane renders: the full content when the publisher
// supplied it, the summary otherwise.
func (i Item) Body() string {
	if i.ContentHTML != "" {
		return i.ContentHTML
	}
	return i.SummaryHTML
}

// SaveItems inserts new items and updates ones whose GUID is already known,
// returning how many were newly inserted.
//
// Updating in place rather than inserting a duplicate is what stops a
// publisher's typo fix from showing up as a second article — and (from R2) it
// is why an edit does not clear read state, since the row's identity does not
// change.
func (s *Store) SaveItems(ctx context.Context, feedID int64, items []ParsedItem, now time.Time) (int, error) {
	if len(items) == 0 {
		return 0, nil
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("reader: begin save items: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO reader_items
			(feed_id, guid, url, title, author, published_at, fetched_at, summary_html, content_html)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (feed_id, guid) DO UPDATE SET
			url          = excluded.url,
			title        = excluded.title,
			author       = excluded.author,
			summary_html = excluded.summary_html,
			content_html = excluded.content_html`)
	if err != nil {
		return 0, fmt.Errorf("reader: prepare save item: %w", err)
	}
	defer func() { _ = stmt.Close() }()

	inserted := 0
	for _, it := range items {
		if it.GUID == "" {
			// parse.go always synthesises one; a caller that does not is a bug
			// worth failing on rather than storing an unaddressable row.
			return 0, fmt.Errorf("reader: item %q has no guid", it.Title)
		}
		published := it.PublishedAt
		if published.IsZero() {
			// An undated item still has to sort somewhere sensible.
			published = now
		}

		var existing int
		if err := tx.QueryRowContext(ctx,
			`SELECT count(*) FROM reader_items WHERE feed_id = ? AND guid = ?`,
			feedID, it.GUID).Scan(&existing); err != nil {
			return 0, fmt.Errorf("reader: check existing item: %w", err)
		}

		if _, err := stmt.ExecContext(ctx, feedID, it.GUID, it.URL, it.Title, it.Author,
			formatTime(published), formatTime(now), it.SummaryHTML, it.ContentHTML); err != nil {
			return 0, fmt.Errorf("reader: save item %q: %w", it.GUID, err)
		}
		if existing == 0 {
			inserted++
		}
	}

	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("reader: commit save items: %w", err)
	}
	return inserted, nil
}

// ItemsForSubscription lists one subscription's items, newest first.
//
// The subscription is looked up by (id, user_id) first so a subscription that
// belongs to somebody else is ErrNotFound rather than an empty list — an empty
// list would tell a caller the id exists.
func (s *Store) ItemsForSubscription(ctx context.Context, userID, subID int64, limit int) ([]Item, error) {
	var feedID int64
	if err := s.db.QueryRowContext(ctx,
		`SELECT feed_id FROM reader_subs WHERE id = ? AND user_id = ?`,
		subID, userID).Scan(&feedID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("reader: load subscription: %w", err)
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT i.id, i.feed_id, i.guid, i.url, i.title, i.author,
		       i.published_at, i.fetched_at, i.summary_html, i.content_html, f.title
		  FROM reader_items i JOIN reader_feeds f ON f.id = i.feed_id
		 WHERE i.feed_id = ?
		 ORDER BY i.published_at DESC, i.id DESC
		 LIMIT ?`, feedID, limit)
	if err != nil {
		return nil, fmt.Errorf("reader: list items: %w", err)
	}
	defer func() { _ = rows.Close() }()

	return scanItems(rows)
}

// Item loads one article, but only for a user who subscribes to its feed. The
// EXISTS clause is the authorisation check: item ids are global, so without it
// any signed-in user could read any item in the database.
func (s *Store) Item(ctx context.Context, userID, itemID int64) (Item, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT i.id, i.feed_id, i.guid, i.url, i.title, i.author,
		       i.published_at, i.fetched_at, i.summary_html, i.content_html, f.title
		  FROM reader_items i JOIN reader_feeds f ON f.id = i.feed_id
		 WHERE i.id = ?
		   AND EXISTS (SELECT 1 FROM reader_subs s
		                WHERE s.feed_id = i.feed_id AND s.user_id = ?)`,
		itemID, userID)
	if err != nil {
		return Item{}, fmt.Errorf("reader: load item: %w", err)
	}
	defer func() { _ = rows.Close() }()

	items, err := scanItems(rows)
	if err != nil {
		return Item{}, err
	}
	if len(items) == 0 {
		return Item{}, ErrNotFound
	}
	return items[0], nil
}

func scanItems(rows *sql.Rows) ([]Item, error) {
	var out []Item
	for rows.Next() {
		var it Item
		var published, fetched string
		if err := rows.Scan(&it.ID, &it.FeedID, &it.GUID, &it.URL, &it.Title, &it.Author,
			&published, &fetched, &it.SummaryHTML, &it.ContentHTML, &it.FeedName); err != nil {
			return nil, fmt.Errorf("reader: scan item: %w", err)
		}
		it.PublishedAt = parseTime(published)
		it.FetchedAt = parseTime(fetched)
		out = append(out, it)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("reader: iterate items: %w", err)
	}
	return out, nil
}
