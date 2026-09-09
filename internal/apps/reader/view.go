package reader

import (
	"html/template"
	"strconv"
	"time"

	"github.com/iliafrenkel/on-suite/internal/platform/render"
)

// indexView is the whole three-pane page. Each pane is also renderable on its
// own, which is what lets a handler answer an HTMX request with just the pane
// that changed.
type indexView struct {
	Tree    treeView
	List    listView
	Article articleView
	// Error is a validation message for the add-feed form, empty when there
	// is nothing to say.
	Error string
	// Title and Shell are only populated by the HTMX fragment path, for the
	// shell-crumb-tail OOB block "panes-oob" emits — a full page render's
	// shell crumb comes from render.Page directly (via app.Deps.Page), which
	// indexView never touches.
	Title string
	Shell render.Shell
}

type treeView struct {
	Folders  []TreeFolder
	Root     []Subscription
	ActiveID int64
	// Empty is true when the user has no subscriptions at all, which is a
	// different thing from a folder having none.
	Empty bool
}

type listView struct {
	Items      []listItem
	ActiveID   int64
	FeedTitle  string
	Selected   bool
	EmptyState string
}

type listItem struct {
	ID        int64
	Title     string
	FeedName  string
	Published string
}

type articleView struct {
	Selected bool
	Title    string
	URL      string
	Author   string
	FeedName string
	When     string
	// Body is publisher HTML that SanitizeHTML has already been through.
	//
	// This is the ONLY template.HTML conversion in the app. Never convert a
	// string here that has not come out of SanitizeHTML: html/template's
	// escaping is the last thing standing between a feed and a stored XSS in
	// a signed-in page, and template.HTML switches it off.
	Body template.HTML
}

// articleFragment is the whole HTMX response for GET /item/{id}: the article
// pane plus the shell-crumb-tail OOB block "article-oob" emits, the same
// reason indexView carries Title/Shell for "panes-oob" — see that field's
// doc comment.
type articleFragment struct {
	Article articleView
	Title   string
	Shell   render.Shell
}

func viewTree(t Tree, activeID int64) treeView {
	empty := len(t.Root) == 0
	for _, f := range t.Folders {
		if len(f.Subs) > 0 {
			empty = false
		}
	}
	return treeView{Folders: t.Folders, Root: t.Root, ActiveID: activeID, Empty: empty}
}

func viewList(items []Item, sub Subscription, activeID int64) listView {
	out := listView{
		ActiveID:  activeID,
		FeedTitle: sub.DisplayName(),
		Selected:  true,
	}
	for _, it := range items {
		out.Items = append(out.Items, listItem{
			ID:        it.ID,
			Title:     firstNonEmpty(it.Title, it.URL, "(untitled)"),
			FeedName:  it.FeedName,
			Published: humanTime(it.PublishedAt),
		})
	}
	if len(out.Items) == 0 {
		out.EmptyState = "Nothing here yet. This feed has not been fetched, or it published nothing."
	}
	return out
}

func viewArticle(it Item) articleView {
	return articleView{
		Selected: true,
		Title:    firstNonEmpty(it.Title, "(untitled)"),
		URL:      it.URL,
		Author:   it.Author,
		FeedName: it.FeedName,
		When:     humanTime(it.PublishedAt),
		// Safe: Body() returns content that ParseFeed sanitized on the way in.
		Body: template.HTML(it.Body()),
	}
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

// humanTime is short for anything recent and absolute beyond a week, which is
// the resolution someone scanning a list actually reads.
func humanTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	switch d := time.Since(t); {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return itoaMinutes(int(d.Minutes())) + "m"
	case d < 24*time.Hour:
		return itoaMinutes(int(d.Hours())) + "h"
	case d < 7*24*time.Hour:
		return itoaMinutes(int(d.Hours()/24)) + "d"
	default:
		return t.Format("2 Jan 2006")
	}
}

// itoaMinutes exists only so the switch above reads cleanly.
func itoaMinutes(n int) string { return strconv.Itoa(n) }
