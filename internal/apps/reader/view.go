package reader

import (
	"fmt"
	"html/template"
	"strconv"
	"strings"
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
	// handler. FeedCandidate is defined in discover.go.
	Candidates []FeedCandidate
	// SelectedFolderID is the folder the user had chosen on the add-feed form
	// when a pasted site turned out to offer several feeds, 0 for none. The
	// chooser's per-candidate forms carry it back as a hidden field so the
	// folder survives the extra round trip instead of silently landing the
	// subscription at the root.
	SelectedFolderID int64
	// Title and Shell are only populated by the HTMX fragment path, for the
	// shell-crumb-tail OOB block "panes-oob" emits — a full page render's
	// shell crumb comes from render.Page directly (via app.Deps.Page), which
	// indexView never touches.
	Title string
	Shell render.Shell
}

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
	// Query is the live search filter, echoed back into the search box so it
	// does not clear itself on every keystroke's response.
	Query string
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
	// Query is the active search filter, echoed back into the star/unread/
	// full-article forms' hidden fields (via reader-ctx) so submitting one
	// does not drop the search the article was opened from.
	Query string
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

func viewList(items []Item, title string, scope Scope, subID int64, filter Filter, basePath, query string) listView {
	out := listView{
		Title:    title,
		Selected: true,
		Scope:    scope,
		SubID:    subID,
		Filter:   filter,
		BasePath: basePath,
		Query:    query,
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
		switch {
		case query != "":
			out.EmptyState = "Nothing matches " + query + "."
		case filter == FilterUnread:
			out.EmptyState = "Nothing unread here. Try the All filter."
		case filter == FilterStarred:
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
		Query:    lc.Query,
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

// chartBar is one already-positioned bar. Geometry is computed in Go because
// html/template cannot do arithmetic, and a chart assembled out of template
// expressions is unreadable and wrong.
type chartBar struct {
	X      float64
	Y      float64
	Width  float64
	Height float64
	// Label is the native tooltip text — a <title> child, which is how this
	// gets a hover layer with no JavaScript and no CSP exemption.
	Label string
}

type chartView struct {
	Title  string
	Bars   []chartBar
	Width  float64
	Height float64
	Max    int
	Empty  bool
	// Reconstructed is true when any day shown was backfilled rather than
	// measured, so the caption can say so.
	Reconstructed bool
}

// buildChart lays out one series of daily counts.
//
// One series, one hue: this app has exactly one non-reserved accent, and a
// second series would need a categorical palette it does not have. Two
// measures means two charts.
func buildChart(title string, days []DayStat, value func(DayStat) int) chartView {
	const (
		height = 120.0
		barW   = 3.0
		barGap = 2.0 // the surface gap that keeps adjacent bars legible
	)
	out := chartView{Title: title, Height: height}
	if len(days) == 0 {
		out.Empty = true
		return out
	}

	for _, d := range days {
		if v := value(d); v > out.Max {
			out.Max = v
		}
		if d.Reconstructed {
			out.Reconstructed = true
		}
	}
	if out.Max == 0 {
		out.Empty = true
	}

	out.Width = float64(len(days)) * (barW + barGap)

	for i, d := range days {
		v := value(d)
		h := 0.0
		if out.Max > 0 {
			h = float64(v) / float64(out.Max) * height
		}
		out.Bars = append(out.Bars, chartBar{
			X:      float64(i) * (barW + barGap),
			Y:      height - h,
			Width:  barW,
			Height: h,
			Label:  fmt.Sprintf("%s: %d", d.Day.Format("2 Jan"), v),
		})
	}
	return out
}

// lineView is a stock over time: one polyline, one hue, no markers. Points is
// pre-formatted for the SVG points attribute because html/template cannot
// build it.
type lineView struct {
	Title  string
	Points string
	Width  float64
	Height float64
	Max    int
	Empty  bool
	// Last is the current value, shown as a number beside the line — the one
	// figure on this chart worth reading exactly.
	Last int
	// Reconstructed is true when any day shown was backfilled rather than
	// measured, so the caption can say so.
	Reconstructed bool
}

func buildLine(title string, days []DayStat, value func(DayStat) int) lineView {
	const (
		height = 120.0
		step   = 5.0
	)
	out := lineView{Title: title, Height: height}
	if len(days) == 0 {
		out.Empty = true
		return out
	}
	for _, d := range days {
		if v := value(d); v > out.Max {
			out.Max = v
		}
		if d.Reconstructed {
			out.Reconstructed = true
		}
	}
	out.Last = value(days[len(days)-1])
	if out.Max == 0 {
		out.Empty = true
		return out
	}
	out.Width = float64(len(days)-1) * step

	var b strings.Builder
	for i, d := range days {
		y := height - float64(value(d))/float64(out.Max)*height
		if i > 0 {
			b.WriteByte(' ')
		}
		fmt.Fprintf(&b, "%.1f,%.1f", float64(i)*step, y)
	}
	out.Points = b.String()
	return out
}

// statTile is one of the plain-number tiles at the top of the stats page.
type statTile struct {
	Label string
	Value string
}

// statsView is the reading-stats page.
type statsView struct {
	Tiles     []statTile
	Fetched   chartView
	Read      chartView
	Backlog   lineView
	Feeds     []FeedStat
	Quiet     []FeedStat
	Neglected []FeedStat
	// QuietAfterDays is QuietAfter expressed in days, for the "gone quiet"
	// copy — computed here rather than hardcoded in the template so the two
	// never drift apart if QuietAfter changes.
	QuietAfterDays int
}

// buildStatsView assembles the reading-stats page from its three inputs: the
// daily series (flows and stock), the per-feed rows, and the sidebar's own
// unread/starred counts, so the tiles never disagree with the tree.
func buildStatsView(days []DayStat, feeds []FeedStat, counts Counts) statsView {
	var articles int
	for _, f := range feeds {
		articles += f.Articles
	}

	out := statsView{
		Tiles: []statTile{
			{Label: "Feeds", Value: strconv.Itoa(len(feeds))},
			{Label: "Unread", Value: strconv.Itoa(counts.Total)},
			{Label: "Starred", Value: strconv.Itoa(counts.Starred)},
			{Label: "Articles stored", Value: strconv.Itoa(articles)},
		},
		Fetched:        buildChart("Articles arriving", days, func(d DayStat) int { return d.Fetched }),
		Read:           buildChart("Articles read", days, func(d DayStat) int { return d.Read }),
		Backlog:        buildLine("Backlog", days, func(d DayStat) int { return d.Backlog }),
		Feeds:          feeds,
		QuietAfterDays: int(QuietAfter / (24 * time.Hour)),
	}
	for _, f := range feeds {
		if f.Quiet() {
			out.Quiet = append(out.Quiet, f)
		}
		if f.Neglected() {
			out.Neglected = append(out.Neglected, f)
		}
	}
	return out
}
