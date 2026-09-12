package reader

import (
	"context"
	"database/sql"
	"fmt"
)

// exportedFolder, exportedSubscription and exportedItem are the shapes
// `onsuite export` writes. They are declared here, separately from the store's
// own types, so the backup format is a deliberate thing that changes when
// someone edits this file — not a mirror of whatever the schema happens to
// look like.
type exportedFolder struct {
	Name string `json:"name"`
}

type exportedSubscription struct {
	FeedURL string `json:"feed_url"`
	Title   string `json:"title,omitempty"`
	SiteURL string `json:"site_url,omitempty"`
	Folder  string `json:"folder,omitempty"`
}

type exportedItem struct {
	Title       string `json:"title"`
	URL         string `json:"url,omitempty"`
	FeedURL     string `json:"feed_url"`
	PublishedAt string `json:"published_at,omitempty"`
	StarredAt   string `json:"starred_at,omitempty"`
}

type exportPayload struct {
	Folders       []exportedFolder       `json:"folders"`
	Subscriptions []exportedSubscription `json:"subscriptions"`
	// Starred holds saved articles only. A backup of every article would be a
	// copy of other people's writing, most of it re-fetchable from the feed;
	// what is not re-creatable is which ones this person chose to keep.
	Starred []exportedItem `json:"starred"`
}

// Export implements app.Exporter, joining ON Reader to onsuite export's
// whole-account JSON backup. It takes the database directly, like every
// Exporter, so a backup works from the command line without an HTTP stack.
func (a *App) Export(ctx context.Context, handle *sql.DB, userID int64) (any, error) {
	return NewStore(handle).Export(ctx, userID)
}

// Export gathers one user's reader data.
func (s *Store) Export(ctx context.Context, userID int64) (exportPayload, error) {
	tree, err := s.Tree(ctx, userID)
	if err != nil {
		return exportPayload{}, err
	}

	out := exportPayload{
		Folders:       []exportedFolder{},
		Subscriptions: []exportedSubscription{},
		Starred:       []exportedItem{},
	}
	for _, f := range tree.Folders {
		out.Folders = append(out.Folders, exportedFolder{Name: f.Name})
		for _, sub := range f.Subs {
			out.Subscriptions = append(out.Subscriptions, exportedSub(sub, f.Name))
		}
	}
	for _, sub := range tree.Root {
		out.Subscriptions = append(out.Subscriptions, exportedSub(sub, ""))
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT i.title, i.url, f.url, i.published_at, st.starred_at
		  FROM reader_item_state st
		  JOIN reader_items i ON i.id = st.item_id
		  JOIN reader_feeds f ON f.id = i.feed_id
		 WHERE st.user_id = ? AND st.starred_at IS NOT NULL
		 ORDER BY st.starred_at DESC`, userID)
	if err != nil {
		return exportPayload{}, fmt.Errorf("reader: export starred: %w", err)
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		var it exportedItem
		var url, published sql.NullString
		if err := rows.Scan(&it.Title, &url, &it.FeedURL, &published, &it.StarredAt); err != nil {
			return exportPayload{}, fmt.Errorf("reader: scan starred: %w", err)
		}
		it.URL = url.String
		it.PublishedAt = published.String
		out.Starred = append(out.Starred, it)
	}
	if err := rows.Err(); err != nil {
		return exportPayload{}, fmt.Errorf("reader: iterate starred: %w", err)
	}
	return out, nil
}

func exportedSub(s Subscription, folder string) exportedSubscription {
	return exportedSubscription{
		FeedURL: s.FeedURL,
		Title:   s.DisplayName(),
		SiteURL: s.SiteURL,
		Folder:  folder,
	}
}
