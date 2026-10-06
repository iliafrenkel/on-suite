package later

import (
	"context"
	"database/sql"
	"fmt"
	"html"
	"strings"
	"time"
)

// exportedArticle, exportedHighlight and exportPayload are the shapes
// `onsuite export` writes. They are declared here, apart from the store's
// types, so the backup format changes only when someone edits this file.
type exportedHighlight struct {
	Quote     string    `json:"quote"`
	Comment   string    `json:"comment,omitempty"`
	Start     int       `json:"start"` // code-point offsets into the article's text
	End       int       `json:"end"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type exportedArticle struct {
	URL          string              `json:"url"`
	Title        string              `json:"title"`
	SiteName     string              `json:"site_name,omitempty"`
	Byline       string              `json:"byline,omitempty"`
	State        State               `json:"state"`
	Content      Content             `json:"content"`
	ExtractError string              `json:"extract_error,omitempty"`
	ContentHTML  string              `json:"content_html,omitempty"`
	Note         string              `json:"note,omitempty"`
	Tags         []string            `json:"tags"`
	Highlights   []exportedHighlight `json:"highlights"`
	SavedAt      time.Time           `json:"saved_at"`
	OpenedAt     *time.Time          `json:"opened_at,omitempty"`
	ArchivedAt   *time.Time          `json:"archived_at,omitempty"`
	UpdatedAt    time.Time           `json:"updated_at"`
}

type exportPayload struct {
	Articles []exportedArticle `json:"articles"`
	Tags     []string          `json:"tags"`
}

// Export implements app.Exporter, joining ON Later to onsuite export's
// whole-account JSON backup. Image bytes are left out; each image's
// source URL is kept in the article's HTML (spec: "Export").
func (a *App) Export(ctx context.Context, handle *sql.DB, userID int64) (any, error) {
	return NewStore(handle).Export(ctx, userID)
}

// Export gathers one user's articles, oldest saved first.
func (st *Store) Export(ctx context.Context, userID int64) (exportPayload, error) {
	arts, err := st.allArticles(ctx, userID)
	if err != nil {
		return exportPayload{}, err
	}
	out := exportPayload{Articles: []exportedArticle{}}
	for _, art := range arts {
		tags, err := st.ArticleTags(ctx, userID, art.ID)
		if err != nil {
			return exportPayload{}, err
		}
		hls, err := st.Highlights(ctx, art.ID)
		if err != nil {
			return exportPayload{}, err
		}
		sources, err := st.ImageSources(ctx, art.ID)
		if err != nil {
			return exportPayload{}, err
		}
		e := exportedArticle{
			URL: art.URL, Title: art.Title, SiteName: art.SiteName, Byline: art.Byline,
			State: art.State, Content: art.Content, ExtractError: art.ExtractError,
			ContentHTML: exportHTML(art.ContentHTML, sources), Note: art.Note,
			Tags: append([]string{}, tags...), Highlights: []exportedHighlight{},
			SavedAt: art.SavedAt, OpenedAt: optionalTime(art.OpenedAt),
			ArchivedAt: optionalTime(art.ArchivedAt), UpdatedAt: art.UpdatedAt,
		}
		for _, h := range hls {
			e.Highlights = append(e.Highlights, exportedHighlight{
				Quote: h.Quote, Comment: h.Comment, Start: h.Start, End: h.End,
				CreatedAt: h.CreatedAt, UpdatedAt: h.UpdatedAt,
			})
		}
		out.Articles = append(out.Articles, e)
	}
	tags, err := st.TagNames(ctx, userID)
	if err != nil {
		return exportPayload{}, err
	}
	out.Tags = append([]string{}, tags...)
	return out, nil
}

// allArticles loads every one of userID's articles, closing its rows
// before returning: the database has one connection, and Export queries
// again per article.
func (st *Store) allArticles(ctx context.Context, userID int64) ([]Article, error) {
	rows, err := st.db.QueryContext(ctx,
		`SELECT `+articleColumns+` FROM later_articles WHERE user_id = ? ORDER BY saved_at, id`, userID)
	if err != nil {
		return nil, fmt.Errorf("later: export articles: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []Article
	for rows.Next() {
		a, err := scanArticle(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("later: export articles: %w", err)
	}
	return out, nil
}

// exportHTML points a snapshot's images back at their sources, so an
// exported article reads on its own, without ON Suite. Matching the quoted
// attribute value keeps every other byte of the snapshot as stored.
func exportHTML(contentHTML string, sources map[string]string) string {
	if len(sources) == 0 {
		return contentHTML
	}
	pairs := make([]string, 0, 2*len(sources))
	for hash, src := range sources {
		pairs = append(pairs, `"`+ImagePathPrefix+hash+`"`, `"`+html.EscapeString(src)+`"`)
	}
	return strings.NewReplacer(pairs...).Replace(contentHTML)
}

func optionalTime(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	return &t
}
