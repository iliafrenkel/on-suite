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

// DB exposes the handle for tests that assert on rows no method returns.
func (s *Store) DB() *sql.DB { return s.db }

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
	// ErrorCount and LastError mirror the shared feed's own poller state
	// (Feed.ErrorCount / Feed.LastError), so the sidebar can show a
	// persistently-failing feed without a second query.
	ErrorCount int
	LastError  string
}

// Failing is true once the poller has recorded at least one consecutive
// failure fetching this subscription's feed. It is what the tree template
// checks to decide whether to render the failure marker.
func (s Subscription) Failing() bool { return s.ErrorCount > 0 }

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
		SELECT s.id, s.feed_id, s.folder_id, s.title, s.added_at, f.url, f.title,
		       f.error_count, f.last_error
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
			&added, &sub.FeedURL, &sub.FeedName, &sub.ErrorCount, &sub.LastError); err != nil {
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

	// Read and Starred are this viewer's state. They are populated by
	// ItemsForScope and Item, and are meaningless on a zero Item.
	Read    bool
	Starred bool

	// FullHTML is the extracted article body, empty until someone fetches it.
	// Unlike Read and Starred it is shared: one household member fetching an
	// article gives it to everybody.
	FullHTML string
	// FullError is why the last fetch failed, empty when none has failed.
	FullError string
	// FullFetchedAt is when a fetch was last attempted, zero if never. It is
	// what distinguishes "never tried" from "tried and got nothing".
	FullFetchedAt time.Time
}

// Body is what the article pane renders: the full content when the publisher
// supplied it, the summary otherwise.
func (i Item) Body() string {
	if i.ContentHTML != "" {
		return i.ContentHTML
	}
	return i.SummaryHTML
}

// HasFull reports whether an extracted article body is stored.
func (i Item) HasFull() bool { return i.FullHTML != "" }

// Scope is which set of items a list is drawn from.
type Scope string

const (
	// ScopeAll is every item in every feed the user subscribes to.
	ScopeAll Scope = "all"
	// ScopeStarred is everything the user saved, across feeds.
	ScopeStarred Scope = "starred"
	// ScopeFeed is one subscription.
	ScopeFeed Scope = "feed"
)

// Filter narrows a scope by this user's own state.
type Filter string

const (
	FilterUnread  Filter = "unread"
	FilterStarred Filter = "starred"
	FilterAll     Filter = "all"
)

// ParseScope and ParseFilter map a URL parameter onto the typed value,
// defaulting rather than erroring: a hand-edited query string should show a
// sensible list, not a 400.
func ParseFilter(raw string) Filter {
	switch Filter(raw) {
	case FilterStarred:
		return FilterStarred
	case FilterAll:
		return FilterAll
	default:
		// Unread is the default because the whole point of the read model is
		// that the list answers "what is new".
		return FilterUnread
	}
}

// itemColumns is the shared SELECT list. Every item query returns the same
// shape so scanItems stays one function.
const itemColumns = `
	i.id, i.feed_id, i.guid, i.url, i.title, i.author,
	i.published_at, i.fetched_at, i.summary_html, i.content_html, f.title,
	st.read_at IS NOT NULL, st.starred_at IS NOT NULL,
	i.full_html, i.full_error, i.full_fetched_at`

// ItemsForScope is the one query path for listing items.
//
// The unread predicate is a NOT EXISTS rather than the "count minus
// read-states" subtraction the design spec suggested. Subtraction is only
// correct if every read-state row falls inside the counted range, and
// subs.added_at means it does not: a user can hold a read-state row for an item
// published before they subscribed, and subtracting it would hide a genuinely
// unread article.
func (s *Store) ItemsForScope(ctx context.Context, userID int64, scope Scope, subID int64, filter Filter, limit int) ([]Item, error) {
	// Every scope is bounded by the user's own subscriptions and by each
	// subscription's added_at, so authorisation and the unread cutoff are the
	// same join rather than two things to keep in step.
	//
	// The cutoff compares against fetched_at, not published_at. published_at
	// is set by the feed publisher and has no relationship to when we
	// discovered the item; fetched_at is when this system learned of it,
	// which is the actual question a cutoff needs answered: did this item
	// exist in the feed before the user subscribed? A backfilled old article
	// discovered by a poll that ran before the subscription is backlog; one
	// discovered by a poll that ran after is not, regardless of its stated
	// publish date.
	query := `
		SELECT ` + itemColumns + `
		  FROM reader_items i
		  JOIN reader_feeds f ON f.id = i.feed_id
		  JOIN reader_subs sub ON sub.feed_id = i.feed_id AND sub.user_id = ?
		  LEFT JOIN reader_item_state st ON st.user_id = sub.user_id AND st.item_id = i.id
		 WHERE i.fetched_at >= sub.added_at`
	args := []any{userID}

	switch scope {
	case ScopeFeed:
		query += ` AND sub.id = ?`
		args = append(args, subID)
	case ScopeStarred:
		query += ` AND st.starred_at IS NOT NULL`
	case ScopeAll:
		// No extra predicate: the subscription join is the bound.
	default:
		return nil, fmt.Errorf("%w: unknown scope %q", ErrInvalid, scope)
	}

	switch filter {
	case FilterUnread:
		query += ` AND st.read_at IS NULL`
	case FilterStarred:
		query += ` AND st.starred_at IS NOT NULL`
	case FilterAll:
		// No extra predicate.
	default:
		return nil, fmt.Errorf("%w: unknown filter %q", ErrInvalid, filter)
	}

	query += ` ORDER BY i.published_at DESC, i.id DESC LIMIT ?`
	args = append(args, limit)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("reader: list items for scope %s: %w", scope, err)
	}
	defer func() { _ = rows.Close() }()

	return scanItems(rows)
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

		var itemID int64
		if err := tx.QueryRowContext(ctx,
			`SELECT id FROM reader_items WHERE feed_id = ? AND guid = ?`,
			feedID, it.GUID).Scan(&itemID); err != nil {
			return 0, fmt.Errorf("reader: load saved item %q: %w", it.GUID, err)
		}

		// Replace the links rather than adding to them: an edit that removed
		// an image must let that image become an orphan, or a picture the
		// publisher deleted stays cached forever.
		if _, err := tx.ExecContext(ctx,
			`DELETE FROM reader_item_images WHERE item_id = ?`, itemID); err != nil {
			return 0, fmt.Errorf("reader: clear image links: %w", err)
		}
		for hash, src := range it.Images {
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO reader_images (url_hash, src_url) VALUES (?, ?)
				ON CONFLICT (url_hash) DO NOTHING`, hash, src); err != nil {
				return 0, fmt.Errorf("reader: record image: %w", err)
			}
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO reader_item_images (item_id, url_hash) VALUES (?, ?)
				ON CONFLICT (item_id, url_hash) DO NOTHING`, itemID, hash); err != nil {
				return 0, fmt.Errorf("reader: link image: %w", err)
			}
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

// ItemsForSubscription lists one subscription's items regardless of state.
// It is a thin wrapper over ItemsForScope so there is exactly one item query.
func (s *Store) ItemsForSubscription(ctx context.Context, userID, subID int64, limit int) ([]Item, error) {
	// The scope query returns an empty list for a subscription that is not
	// this user's, where R1 returned ErrNotFound. Preserve the old contract:
	// callers rely on 404-not-403 for someone else's subscription.
	if err := s.ownsSubscription(ctx, userID, subID); err != nil {
		return nil, err
	}
	return s.ItemsForScope(ctx, userID, ScopeFeed, subID, FilterAll, limit)
}

// ownsSubscription is the ErrNotFound-or-nil ownership check R1's
// ItemsForSubscription did inline.
func (s *Store) ownsSubscription(ctx context.Context, userID, subID int64) error {
	var id int64
	err := s.db.QueryRowContext(ctx,
		`SELECT id FROM reader_subs WHERE id = ? AND user_id = ?`, subID, userID).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("reader: load subscription: %w", err)
	}
	return nil
}

// Item loads one article, but only for a user who subscribes to its feed. The
// EXISTS clause is the authorisation check: item ids are global, so without it
// any signed-in user could read any item in the database.
func (s *Store) Item(ctx context.Context, userID, itemID int64) (Item, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT `+itemColumns+`
		  FROM reader_items i
		  JOIN reader_feeds f ON f.id = i.feed_id
		  LEFT JOIN reader_item_state st ON st.user_id = ? AND st.item_id = i.id
		 WHERE i.id = ?
		   AND EXISTS (SELECT 1 FROM reader_subs s
		                WHERE s.feed_id = i.feed_id AND s.user_id = ?)`,
		userID, itemID, userID)
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
		var fullHTML, fullError, fullFetchedAt sql.NullString
		if err := rows.Scan(&it.ID, &it.FeedID, &it.GUID, &it.URL, &it.Title, &it.Author,
			&published, &fetched, &it.SummaryHTML, &it.ContentHTML, &it.FeedName,
			&it.Read, &it.Starred,
			&fullHTML, &fullError, &fullFetchedAt); err != nil {
			return nil, fmt.Errorf("reader: scan item: %w", err)
		}
		it.PublishedAt = parseTime(published)
		it.FetchedAt = parseTime(fetched)
		it.FullHTML = fullHTML.String
		it.FullError = fullError.String
		if fullFetchedAt.Valid {
			it.FullFetchedAt = parseTime(fullFetchedAt.String)
		}
		out = append(out, it)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("reader: iterate items: %w", err)
	}
	return out, nil
}

// DueFeeds returns feeds whose next_fetch_at has passed, oldest first.
func (s *Store) DueFeeds(ctx context.Context, now time.Time, limit int) ([]Feed, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, url, resolved_url, title, site_url, etag, last_modified,
		       last_status, last_error, error_count, next_fetch_at, fetch_interval
		  FROM reader_feeds
		 WHERE next_fetch_at <= ?
		 ORDER BY next_fetch_at
		 LIMIT ?`, formatTime(now), limit)
	if err != nil {
		return nil, fmt.Errorf("reader: list due feeds: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []Feed
	for rows.Next() {
		var f Feed
		var next string
		var interval sql.NullInt64
		if err := rows.Scan(&f.ID, &f.URL, &f.ResolvedURL, &f.Title, &f.SiteURL,
			&f.ETag, &f.LastModified, &f.LastStatus, &f.LastError, &f.ErrorCount,
			&next, &interval); err != nil {
			return nil, fmt.Errorf("reader: scan feed: %w", err)
		}
		f.NextFetchAt = parseTime(next)
		if interval.Valid {
			f.FetchInterval = time.Duration(interval.Int64) * time.Second
		}
		out = append(out, f)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("reader: iterate feeds: %w", err)
	}
	return out, nil
}

// FetchResult is one poll's outcome, written back in a single statement.
type FetchResult struct {
	FeedID       int64
	ResolvedURL  string
	Title        string
	SiteURL      string
	ETag         string
	LastModified string
	Status       int
	Err          string
	FetchedAt    time.Time
	NextFetchAt  time.Time
}

// SaveFetchResult records how a poll went.
//
// A failure keeps the last good title and site URL rather than blanking them:
// a feed that 500s for a day should stay recognisable in the sidebar.
func (s *Store) SaveFetchResult(ctx context.Context, r FetchResult) error {
	if r.Err != "" {
		_, err := s.db.ExecContext(ctx, `
			UPDATE reader_feeds
			   SET last_fetch_at = ?, last_status = ?, last_error = ?,
			       error_count = error_count + 1, next_fetch_at = ?
			 WHERE id = ?`,
			formatTime(r.FetchedAt), r.Status, r.Err, formatTime(r.NextFetchAt), r.FeedID)
		if err != nil {
			return fmt.Errorf("reader: save fetch failure: %w", err)
		}
		return nil
	}

	_, err := s.db.ExecContext(ctx, `
		UPDATE reader_feeds
		   SET resolved_url  = ?,
		       title         = CASE WHEN ? <> '' THEN ? ELSE title END,
		       site_url      = CASE WHEN ? <> '' THEN ? ELSE site_url END,
		       etag          = ?,
		       last_modified = ?,
		       last_fetch_at = ?,
		       last_status   = ?,
		       last_error    = '',
		       error_count   = 0,
		       next_fetch_at = ?
		 WHERE id = ?`,
		r.ResolvedURL, r.Title, r.Title, r.SiteURL, r.SiteURL,
		r.ETag, r.LastModified, formatTime(r.FetchedAt), r.Status,
		formatTime(r.NextFetchAt), r.FeedID)
	if err != nil {
		return fmt.Errorf("reader: save fetch result: %w", err)
	}
	return nil
}

// canSeeItem reports whether a user subscribes to the feed an item belongs to.
//
// Item ids are global, so every state change needs this: without it any
// signed-in user could mark any item in the database read or starred, and the
// row they wrote would be a durable record that they probed for it.
func (s *Store) canSeeItem(ctx context.Context, userID, itemID int64) error {
	var ok int
	err := s.db.QueryRowContext(ctx, `
		SELECT 1
		  FROM reader_items i
		 WHERE i.id = ?
		   AND EXISTS (SELECT 1 FROM reader_subs sub
		                WHERE sub.feed_id = i.feed_id AND sub.user_id = ?)`,
		itemID, userID).Scan(&ok)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("reader: check item visibility: %w", err)
	}
	return nil
}

// SetRead marks an item read or unread.
//
// The upsert writes only read_at, leaving starred_at alone: the two axes share
// a row, and a whole-row replace here would silently drop a star every time
// something was marked read.
func (s *Store) SetRead(ctx context.Context, userID, itemID int64, read bool, now time.Time) error {
	if err := s.canSeeItem(ctx, userID, itemID); err != nil {
		return err
	}
	var readAt any
	if read {
		readAt = formatTime(now)
	}
	if _, err := s.db.ExecContext(ctx, `
		INSERT INTO reader_item_state (user_id, item_id, read_at, starred_at)
		VALUES (?, ?, ?, NULL)
		ON CONFLICT (user_id, item_id) DO UPDATE SET read_at = excluded.read_at`,
		userID, itemID, readAt); err != nil {
		return fmt.Errorf("reader: set read state: %w", err)
	}
	return nil
}

// SetStarred stars or unstars an item, leaving read state alone for the same
// reason SetRead leaves the star alone.
func (s *Store) SetStarred(ctx context.Context, userID, itemID int64, starred bool, now time.Time) error {
	if err := s.canSeeItem(ctx, userID, itemID); err != nil {
		return err
	}
	var starredAt any
	if starred {
		starredAt = formatTime(now)
	}
	if _, err := s.db.ExecContext(ctx, `
		INSERT INTO reader_item_state (user_id, item_id, read_at, starred_at)
		VALUES (?, ?, NULL, ?)
		ON CONFLICT (user_id, item_id) DO UPDATE SET starred_at = excluded.starred_at`,
		userID, itemID, starredAt); err != nil {
		return fmt.Errorf("reader: set starred state: %w", err)
	}
	return nil
}

// ItemState reports one item's read and starred flags. A missing row is not an
// error: absence means unread and unstarred.
func (s *Store) ItemState(ctx context.Context, userID, itemID int64) (read, starred bool, err error) {
	var readAt, starredAt sql.NullString
	err = s.db.QueryRowContext(ctx,
		`SELECT read_at, starred_at FROM reader_item_state WHERE user_id = ? AND item_id = ?`,
		userID, itemID).Scan(&readAt, &starredAt)
	if errors.Is(err, sql.ErrNoRows) {
		return false, false, nil
	}
	if err != nil {
		return false, false, fmt.Errorf("reader: load item state: %w", err)
	}
	return readAt.Valid, starredAt.Valid, nil
}

// Counts is the sidebar's unread numbers in one round trip.
type Counts struct {
	// BySub has an entry for every subscription, including those at zero.
	BySub map[int64]int
	// Total is unread across every subscription.
	Total int
	// Starred is how many items the user has saved, read or not.
	Starred int
}

// UnreadCounts computes the whole sidebar in two queries.
//
// The LEFT JOIN is what keeps a subscription with nothing unread in the map at
// zero rather than dropping it — an inner join here silently removes feeds from
// the sidebar the moment they are all read, which looks exactly like data loss.
//
// The cutoff compares against fetched_at, not published_at, for the same
// reason ItemsForScope does: published_at is the publisher's own date and has
// no relationship to when we discovered the item, while fetched_at is when
// this system learned of it — the actual question a cutoff needs answered.
func (s *Store) UnreadCounts(ctx context.Context, userID int64) (Counts, error) {
	out := Counts{BySub: map[int64]int{}}

	rows, err := s.db.QueryContext(ctx, `
		SELECT sub.id, count(i.id)
		  FROM reader_subs sub
		  LEFT JOIN reader_items i
		         ON i.feed_id = sub.feed_id
		        AND i.fetched_at >= sub.added_at
		        AND NOT EXISTS (SELECT 1 FROM reader_item_state st
		                         WHERE st.user_id = sub.user_id
		                           AND st.item_id = i.id
		                           AND st.read_at IS NOT NULL)
		 WHERE sub.user_id = ?
		 GROUP BY sub.id`, userID)
	if err != nil {
		return Counts{}, fmt.Errorf("reader: unread counts: %w", err)
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		var subID int64
		var n int
		if err := rows.Scan(&subID, &n); err != nil {
			return Counts{}, fmt.Errorf("reader: scan unread count: %w", err)
		}
		out.BySub[subID] = n
		out.Total += n
	}
	if err := rows.Err(); err != nil {
		return Counts{}, fmt.Errorf("reader: iterate unread counts: %w", err)
	}

	// Starred is counted separately rather than folded into the grouped query
	// above, which counts only unread items — a starred article that has been
	// read still belongs in the Starred node.
	if err := s.db.QueryRowContext(ctx, `
		SELECT count(*)
		  FROM reader_item_state st
		 WHERE st.user_id = ? AND st.starred_at IS NOT NULL`,
		userID).Scan(&out.Starred); err != nil {
		return Counts{}, fmt.Errorf("reader: starred count: %w", err)
	}
	return out, nil
}

// MarkAllRead marks everything currently unread in a scope as read, returning
// how many rows it touched.
//
// It writes state rows for exactly the items the same predicate as
// ItemsForScope would list — including ItemsForScope's fetched_at cutoff — so
// "mark all read" and "what is unread" can never disagree.
func (s *Store) MarkAllRead(ctx context.Context, userID int64, scope Scope, subID int64, now time.Time) (int, error) {
	query := `
		INSERT INTO reader_item_state (user_id, item_id, read_at, starred_at)
		SELECT sub.user_id, i.id, ?, NULL
		  FROM reader_items i
		  JOIN reader_subs sub ON sub.feed_id = i.feed_id AND sub.user_id = ?
		 WHERE i.fetched_at >= sub.added_at`
	args := []any{formatTime(now), userID}

	switch scope {
	case ScopeFeed:
		query += ` AND sub.id = ?`
		args = append(args, subID)
	case ScopeAll:
		// Everything the user subscribes to.
	case ScopeStarred:
		query += ` AND EXISTS (SELECT 1 FROM reader_item_state st
		                        WHERE st.user_id = sub.user_id AND st.item_id = i.id
		                          AND st.starred_at IS NOT NULL)`
	default:
		return 0, fmt.Errorf("%w: unknown scope %q", ErrInvalid, scope)
	}

	query += ` ON CONFLICT (user_id, item_id) DO UPDATE SET read_at = excluded.read_at
	            WHERE reader_item_state.read_at IS NULL`

	res, err := s.db.ExecContext(ctx, query, args...)
	if err != nil {
		return 0, fmt.Errorf("reader: mark all read: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("reader: mark all read rows: %w", err)
	}
	return int(n), nil
}

// RetentionAge is how long a read, unstarred article is kept.
//
// A constant rather than a flag: the platform's config is a deliberately small,
// closed set of server settings, and adding per-app configuration is a separate
// design question this app should not answer on its own.
const RetentionAge = 60 * 24 * time.Hour

// PurgeItems deletes articles published before the cutoff, keeping anything
// anyone starred and anything anyone still has unread.
//
// The items are shared across the household but the state is not, so "read" has
// to mean read by every subscriber who can see it. Deleting an article one
// person finished while another has it waiting would be quiet data loss.
func (s *Store) PurgeItems(ctx context.Context, before time.Time) (int, error) {
	res, err := s.db.ExecContext(ctx, `
		DELETE FROM reader_items
		 WHERE published_at < ?
		   AND NOT EXISTS (
		       SELECT 1 FROM reader_item_state st
		        WHERE st.item_id = reader_items.id
		          AND st.starred_at IS NOT NULL)
		   AND NOT EXISTS (
		       SELECT 1 FROM reader_subs sub
		        WHERE sub.feed_id = reader_items.feed_id
		          AND reader_items.fetched_at >= sub.added_at
		          AND NOT EXISTS (
		              SELECT 1 FROM reader_item_state st2
		               WHERE st2.user_id = sub.user_id
		                 AND st2.item_id = reader_items.id
		                 AND st2.read_at IS NOT NULL))`,
		formatTime(before))
	if err != nil {
		return 0, fmt.Errorf("reader: purge items: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("reader: purge rows: %w", err)
	}
	return int(n), nil
}

// Image is one cached remote image.
type Image struct {
	Hash        string
	SrcURL      string
	ContentType string
	Bytes       []byte
	FetchedAt   time.Time
	ErrorCount  int
	LastError   string
}

// Cached reports whether the bytes are in hand. An image row exists from the
// moment an article mentions it; the bytes arrive on first view.
func (i Image) Cached() bool { return len(i.Bytes) > 0 }

// ImageByHash loads one image record. An unknown hash is ErrNotFound, which is
// what stops the proxy being asked to fetch a URL no feed ever delivered.
func (s *Store) ImageByHash(ctx context.Context, hash string) (Image, error) {
	var img Image
	var bytes []byte
	var fetched sql.NullString
	err := s.db.QueryRowContext(ctx, `
		SELECT url_hash, src_url, content_type, bytes, fetched_at, error_count, last_error
		  FROM reader_images WHERE url_hash = ?`, hash).
		Scan(&img.Hash, &img.SrcURL, &img.ContentType, &bytes, &fetched,
			&img.ErrorCount, &img.LastError)
	if errors.Is(err, sql.ErrNoRows) {
		return Image{}, ErrNotFound
	}
	if err != nil {
		return Image{}, fmt.Errorf("reader: load image: %w", err)
	}
	img.Bytes = bytes
	if fetched.Valid {
		img.FetchedAt = parseTime(fetched.String)
	}
	return img, nil
}

// SaveImageBytes caches a fetched image and clears any recorded failure.
func (s *Store) SaveImageBytes(ctx context.Context, hash, contentType string, data []byte, now time.Time) error {
	if _, err := s.db.ExecContext(ctx, `
		UPDATE reader_images
		   SET bytes = ?, content_type = ?, fetched_at = ?, last_error = '', error_count = 0
		 WHERE url_hash = ?`,
		data, contentType, formatTime(now), hash); err != nil {
		return fmt.Errorf("reader: cache image: %w", err)
	}
	return nil
}

// SaveImageFailure records that a fetch failed, so a dead image is not
// re-fetched on every page view.
func (s *Store) SaveImageFailure(ctx context.Context, hash, msg string, now time.Time) error {
	if _, err := s.db.ExecContext(ctx, `
		UPDATE reader_images
		   SET last_error = ?, error_count = error_count + 1, fetched_at = ?
		 WHERE url_hash = ?`,
		msg, formatTime(now), hash); err != nil {
		return fmt.Errorf("reader: record image failure: %w", err)
	}
	return nil
}

// SaveFullArticle stores an extracted article body and records its images.
//
// The image bookkeeping is deliberately identical to SaveItems': the same
// reader_images rows and the same reader_item_images links, so retention frees
// an image that only the full article used, and the proxy serves it with no
// special case.
func (s *Store) SaveFullArticle(ctx context.Context, userID, itemID int64, ex Extracted, now time.Time) error {
	if err := s.canSeeItem(ctx, userID, itemID); err != nil {
		return err
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("reader: begin save full article: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, `
		UPDATE reader_items
		   SET full_html = ?, full_fetched_at = ?, full_error = ''
		 WHERE id = ?`,
		ex.HTML, formatTime(now), itemID); err != nil {
		return fmt.Errorf("reader: save full article: %w", err)
	}

	for hash, src := range ex.Images {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO reader_images (url_hash, src_url) VALUES (?, ?)
			ON CONFLICT (url_hash) DO NOTHING`, hash, src); err != nil {
			return fmt.Errorf("reader: record full-article image: %w", err)
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO reader_item_images (item_id, url_hash) VALUES (?, ?)
			ON CONFLICT (item_id, url_hash) DO NOTHING`, itemID, hash); err != nil {
			return fmt.Errorf("reader: link full-article image: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("reader: commit full article: %w", err)
	}
	return nil
}

// SaveFullArticleFailure records why an extraction attempt produced nothing,
// so the button can explain itself instead of appearing to do nothing.
func (s *Store) SaveFullArticleFailure(ctx context.Context, userID, itemID int64, msg string, now time.Time) error {
	if err := s.canSeeItem(ctx, userID, itemID); err != nil {
		return err
	}
	if _, err := s.db.ExecContext(ctx, `
		UPDATE reader_items SET full_error = ?, full_fetched_at = ? WHERE id = ?`,
		msg, formatTime(now), itemID); err != nil {
		return fmt.Errorf("reader: record full-article failure: %w", err)
	}
	return nil
}

// PurgeOrphanImages deletes cached images no item references any more. The
// retention job calls it straight after PurgeItems.
func (s *Store) PurgeOrphanImages(ctx context.Context) (int, error) {
	res, err := s.db.ExecContext(ctx, `
		DELETE FROM reader_images
		 WHERE NOT EXISTS (SELECT 1 FROM reader_item_images li
		                    WHERE li.url_hash = reader_images.url_hash)`)
	if err != nil {
		return 0, fmt.Errorf("reader: purge orphan images: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("reader: purge orphan image rows: %w", err)
	}
	return int(n), nil
}
