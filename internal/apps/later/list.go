package later

import (
	"fmt"
	"math"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/iliafrenkel/on-suite/internal/platform/web"
)

// listQuery is what the list page shows: a tab, optionally narrowed to one
// tag (spec: "Tag chips filter by one tag, combined with the current tab").
type listQuery struct {
	Tab State
	Tag string // "" means every tag
}

// url is the list page for q. Parameters come in a fixed order (tab, tag,
// offset) so links are stable and tests can pin them.
func (q listQuery) url(offset int) string {
	var b strings.Builder
	b.WriteString("/later/?tab=" + string(q.Tab))
	if q.Tag != "" {
		b.WriteString("&tag=" + url.QueryEscape(q.Tag))
	}
	if offset > 0 {
		fmt.Fprintf(&b, "&offset=%d", offset)
	}
	return b.String()
}

func parseListQuery(v url.Values) listQuery {
	return listQuery{Tab: parseTab(v.Get("tab")), Tag: tagParam(v.Get("tag"))}
}

// tagParam is the one tag a ?tag= names, cleaned like a stored name.
func tagParam(v string) string {
	if ts := ParseTags(v); len(ts) > 0 {
		return ts[0]
	}
	return ""
}

type chipView struct {
	Name, URL string
	Current   bool
}

// tagChips is one chip per tag. The selected one links back to the whole
// tab, so clicking it again clears the filter. A ?tag= that names no tag
// any more still gets a chip, or nothing on the page could clear it.
func tagChips(q listQuery, names []string) []chipView {
	if q.Tag != "" && !slices.Contains(names, q.Tag) {
		names = append([]string{q.Tag}, names...)
	}
	var out []chipView
	for _, n := range names {
		next := q
		next.Tag = n
		if n == q.Tag {
			next.Tag = ""
		}
		out = append(out, chipView{Name: n, URL: next.url(0), Current: n == q.Tag})
	}
	return out
}

type tabView struct {
	State   State
	Label   string
	Count   int
	Current bool
	URL     string
}

type rowView struct {
	ID        int64
	Title     string
	Site      string
	Minutes   int
	LinkOnly  bool
	Progress  int // percent, 0-100
	State     State
	Archived  bool
	Tags      []string
	TagsValue string // the ⋯ menu's tags field

	Highlights int

	FaviconSrc string // "" when no <img> should be emitted
	Initial    string // the site's first letter, for the badge
}

// saveForm is what the save box shows again after a refused save.
type saveForm struct {
	Error, URL, Tags string
}

type indexView struct {
	Tabs      []tabView
	Tab       State
	Tag       string
	Chips     []chipView
	Back      string // this list, for the row forms' back field
	Rows      []rowView
	NextURL   string // Load more; "" when there are no more rows
	Form      saveForm
	EmptyText string
	Saved     *savedView // the note after a save, or nil
}

// newRow is the list row for it.
func newRow(it ListItem) rowView {
	row := rowView{
		ID:        it.ID,
		Title:     it.Title,
		Site:      it.SiteHost,
		Minutes:   ReadingMinutes(it.WordCount),
		LinkOnly:  it.Content == ContentLinkOnly,
		Progress:  int(math.Round(it.Progress * 100)),
		State:     it.State,
		Archived:  it.State == StateArchived,
		Tags:      it.Tags,
		TagsValue: strings.Join(it.Tags, ", "),
		Initial:   siteInitial(it.SiteHost),

		Highlights: it.Highlights,
	}
	if it.FaviconShown {
		row.FaviconSrc = "/later/favicon/" + it.FaviconHash
	}
	return row
}

var tabs = []struct {
	state State
	label string
	empty string
}{
	{StateUnread, "Unread", "Nothing to read. Paste a URL above to save an article."},
	{StateReading, "Reading", "Nothing in progress."},
	{StateArchived, "Archived", "Nothing archived yet."},
}

// parseTab maps the tab query value to a state; anything else is Unread.
func parseTab(v string) State {
	for _, t := range tabs {
		if string(t.state) == v {
			return t.state
		}
	}
	return StateUnread
}

func (a *App) index(w http.ResponseWriter, r *http.Request) {
	userID, ok := a.userID(w, r)
	if !ok {
		return
	}
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	if offset < 0 {
		offset = 0
	}
	a.renderListPage(w, r, userID, parseListQuery(r.URL.Query()), offset, http.StatusOK, saveForm{}, a.savedNote(r, userID))
}

// savedNote loads the article named by ?saved= for the note after a save.
// A missing, malformed or someone else's id shows no note.
func (a *App) savedNote(r *http.Request, userID int64) *savedView {
	id, err := strconv.ParseInt(r.URL.Query().Get("saved"), 10, 64)
	if err != nil || id <= 0 {
		return nil
	}
	art, err := a.store.Article(r.Context(), userID, id)
	if err != nil {
		return nil
	}
	return &savedView{
		ID:       art.ID,
		Title:    art.Title,
		LinkOnly: art.Content == ContentLinkOnly,
		Existing: r.URL.Query().Get("existing") == "1",
	}
}

func (a *App) renderIndex(w http.ResponseWriter, r *http.Request, userID int64, tab State, status int, form saveForm) {
	a.renderListPage(w, r, userID, listQuery{Tab: tab}, 0, status, form, nil)
}

// renderListPage draws the list page, or just its rows when HTMX asks for the
// next page.
func (a *App) renderListPage(w http.ResponseWriter, r *http.Request, userID int64, q listQuery, offset, status int, form saveForm, saved *savedView) {
	ctx := r.Context()
	items, err := a.store.List(ctx, userID, q.Tab, q.Tag, offset, pageSize+1)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	counts, err := a.store.Counts(ctx, userID, q.Tag)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	names, err := a.store.TagNames(ctx, userID)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	view := indexView{
		Tab: q.Tab, Tag: q.Tag, Chips: tagChips(q, names), Back: q.url(0),
		Form: form, Saved: saved,
	}
	if len(items) > pageSize {
		items = items[:pageSize]
		view.NextURL = q.url(offset + pageSize)
	}
	for _, it := range items {
		view.Rows = append(view.Rows, newRow(it))
	}
	for _, t := range tabs {
		tq := listQuery{Tab: t.state, Tag: q.Tag}
		view.Tabs = append(view.Tabs, tabView{State: t.state, Label: t.label, Count: counts[t.state], Current: t.state == q.Tab, URL: tq.url(0)})
		if t.state == q.Tab {
			view.EmptyText = t.empty
		}
	}
	if q.Tag != "" {
		view.EmptyText = fmt.Sprintf("Nothing tagged “%s” here.", q.Tag)
	}
	page := a.deps.Page(r, "ON Later")
	page.Data = view
	if web.IsHTMX(r) && !web.IsHTMXHistoryRestore(r) && offset > 0 {
		if err := a.deps.Render.Fragment(w, status, "later/index", "rows", page); err != nil {
			a.deps.Errors.Internal(w, r, err)
		}
		return
	}
	if err := a.deps.Render.Page(w, status, "later/index", page); err != nil {
		a.deps.Errors.Internal(w, r, err)
	}
}

// siteInitial is the upper-cased first letter of a site, for the badge shown
// when it has no icon.
func siteInitial(site string) string {
	r, _ := utf8.DecodeRuneInString(site)
	if r == utf8.RuneError {
		return ""
	}
	return string(unicode.ToUpper(r))
}
