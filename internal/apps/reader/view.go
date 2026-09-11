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
	// Notice is a neutral result message — an import's counts, say. It is
	// separate from Error because a success rendered in red reads as a
	// failure, whatever the words say.
	Notice string
	// Candidates is the discovery chooser's list, populated by Task 4's
	// handler. FeedCandidate is a forward-reference placeholder here: it has
	// no fields yet because discovery (Task 3/4) has not landed. It exists
	// only so renderIndexWith's final signature can be written once.
	Candidates []FeedCandidate
	// Title and Shell are only populated by the HTMX fragment path, for the
	// shell-crumb-tail OOB block "panes-oob" emits — a full page render's
	// shell crumb comes from render.Page directly (via app.Deps.Page), which
	// indexView never touches.
	Title string
	Shell render.Shell
}

// FeedCandidate is a forward-reference placeholder for Task 3/4's discovery
// feature (internal/apps/reader/discover.go, not yet written). It exists only
// so this task can write renderIndexWith's final signature —
// (formErr, notice string, candidates []FeedCandidate) — once, instead of
// changing it twice. Task 3/4 is expected to give it real fields.
type FeedCandidate struct{}

type treeView struct {
	Folders []TreeFolder
	Root    []Subscription
	// ActiveID is the selected subscription, 0 for the All and Starred nodes.
	ActiveID int64
	// Scope marks which pseudo-node is active, so All and Starred can be
	// highlighted the same way a subscription is.
	Scope Scope
	// Counts drives every number in the sidebar.
	Counts Counts
	// Empty is true when the user has no subscriptions at all, which is a
	// different thing from a folder having none.
	Empty bool
}

type listView struct {
	Items      []listItem
	ActiveID   int64
	Title      string
	Selected   bool
	EmptyState string
	// Scope and SubID are echoed back into the filter links and the
	// mark-all-read form, so those controls stay on the list you are looking
	// at rather than resetting to All.
	Scope  Scope
	SubID  int64
	Filter Filter
	// BasePath is the path the filter links point at, without the query.
	BasePath string
	// Shell carries the CSRF token the mark-all-read form needs. It is set by
	// renderIndex rather than viewList, which has no request to read it from.
	Shell render.Shell
}

type listItem struct {
	ID        int64
	Title     string
	FeedName  string
	Published string
	Read      bool
	Starred   bool
}

type articleView struct {
	ID       int64
	Selected bool
	Title    string
	URL      string
	Author   string
	FeedName string
	When     string
	Read     bool
	Starred  bool
	// Body is publisher HTML that SanitizeArticleHTML has already been
	// through. This is the only template.HTML conversion in the app; never
	// convert a string here that has not been through SanitizeArticleHTML.
	Body template.HTML
	// Shell is carried so the article fragment can render the CSRF field its
	// star and unread forms need.
	Shell render.Shell
	// Scope, SubID and Filter are the list this article was opened from,
	// echoed back into the star and unread forms. The article's own path names
	// an item, so without them a state-change POST has no way to say which
	// list the tree redraw riding along with it should keep selected.
	Scope  Scope
	SubID  int64
	Filter Filter
	// HasFull reports that an extracted body is stored, which is what decides
	// whether the toggle renders at all.
	HasFull bool
	// ShowingFull is which body Body currently holds.
	ShowingFull bool
	// FullError explains a failed fetch, empty when there is nothing to say.
	FullError string
}

func viewTree(t Tree, activeID int64, scope Scope, counts Counts) treeView {
	empty := len(t.Root) == 0
	for _, f := range t.Folders {
		if len(f.Subs) > 0 {
			empty = false
		}
	}
	return treeView{
		Folders:  t.Folders,
		Root:     t.Root,
		ActiveID: activeID,
		Scope:    scope,
		Counts:   counts,
		Empty:    empty,
	}
}

func viewList(items []Item, title string, scope Scope, subID int64, filter Filter, basePath string) listView {
	out := listView{
		Title:    title,
		Selected: true,
		Scope:    scope,
		SubID:    subID,
		Filter:   filter,
		BasePath: basePath,
	}
	for _, it := range items {
		out.Items = append(out.Items, listItem{
			ID:        it.ID,
			Title:     firstNonEmpty(it.Title, it.URL, "(untitled)"),
			FeedName:  it.FeedName,
			Published: humanTime(it.PublishedAt),
			Read:      it.Read,
			Starred:   it.Starred,
		})
	}
	if len(out.Items) == 0 {
		switch filter {
		case FilterUnread:
			out.EmptyState = "Nothing unread here. Try the All filter."
		case FilterStarred:
			out.EmptyState = "Nothing saved here yet."
		default:
			out.EmptyState = "Nothing here yet. This feed has not been fetched, or it published nothing."
		}
	}
	return out
}

func viewArticle(it Item, shell render.Shell, lc listContext, showFull bool) articleView {
	// showFull is honoured only when there is a full article to show, so a
	// stale ?view= on an item nobody has fetched still renders the feed body.
	body := it.Body()
	if showFull && it.HasFull() {
		body = it.FullHTML
	}
	return articleView{
		Scope:    lc.Scope,
		SubID:    lc.SubID,
		Filter:   lc.Filter,
		ID:       it.ID,
		Selected: true,
		Title:    firstNonEmpty(it.Title, "(untitled)"),
		URL:      it.URL,
		Author:   it.Author,
		FeedName: it.FeedName,
		When:     humanTime(it.PublishedAt),
		Read:     it.Read,
		Starred:  it.Starred,
		// Safe: both bodies have been through SanitizeArticleHTML — the feed
		// one in ParseFeed, the full one in ExtractArticle.
		Body:        template.HTML(body),
		Shell:       shell,
		HasFull:     it.HasFull(),
		ShowingFull: showFull && it.HasFull(),
		FullError:   it.FullError,
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
