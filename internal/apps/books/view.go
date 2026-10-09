package books

import (
	"net/url"
	"strconv"
	"strings"
	"unicode"

	"github.com/iliafrenkel/on-suite/internal/platform/render"
)

var shelfLabels = map[Shelf]string{
	ShelfReading: "Reading",
	ShelfWant:    "Want to read",
	ShelfRead:    "Read",
	ShelfDNF:     "Did not finish",
	ShelfAll:     "All books",
}

// Label is how a shelf is named on screen.
func (s Shelf) Label() string { return shelfLabels[s] }

// listCtx is the list the panes show: a shelf, optionally narrowed to one
// tag and a title/author filter. GETs carry it in the query string and
// POSTs in hidden fields (ctx-fields), so whatever a change re-renders
// comes back to the same list — the lesson of Reader's reader-ctx.
type listCtx struct {
	Shelf Shelf
	Tag   string
	Q     string
}

// ctxFrom reads a list context; a missing or unknown shelf is Reading, the
// default shelf (spec "Layout").
func ctxFrom(get func(string) string) listCtx {
	sh, ok := ParseShelf(get("shelf"))
	if !ok {
		sh = ShelfReading
	}
	return listCtx{Shelf: sh, Tag: strings.ToLower(strings.TrimSpace(get("tag"))), Q: strings.TrimSpace(get("q"))}
}

// Query is the context as a query string. Templates use the URL methods
// below rather than this: a whole URL dropped into href is left alone by
// html/template, a query string after a literal "?" gets percent-encoded.
func (c listCtx) Query() string {
	v := url.Values{}
	v.Set("shelf", string(c.Shelf))
	if c.Tag != "" {
		v.Set("tag", c.Tag)
	}
	if c.Q != "" {
		v.Set("q", c.Q)
	}
	return v.Encode()
}

// ListURL is this list.
func (c listCtx) ListURL() string { return "/books/?" + c.Query() }

// BookURL is a book opened from this list.
func (c listCtx) BookURL(id int64) string {
	return "/books/b/" + strconv.FormatInt(id, 10) + "?" + c.Query()
}

// panesView is everything the three panes draw. Title and Shell are for
// fragments, which have no render.Page around them.
type panesView struct {
	Title   string
	Shell   render.Shell
	Ctx     listCtx
	Error   string // the banner, for a refused action
	Sidebar sidebarView
	List    listView
	Book    bookView
}

type shelfLink struct {
	Label   string
	URL     string
	Count   int
	Current bool
}

type tagLink struct {
	Name    string
	URL     string
	Current bool
}

type sidebarView struct {
	Shelves []shelfLink
	Tags    []tagLink
}

// viewSidebar keeps the filter text on every link: the filter box sits
// outside the list and keeps showing what was typed, so the lists it leads
// to keep applying it.
func viewSidebar(c listCtx, counts map[Shelf]int, tags []string) sidebarView {
	var v sidebarView
	for _, s := range Shelves {
		to := listCtx{Shelf: s, Q: c.Q}
		v.Shelves = append(v.Shelves, shelfLink{Label: s.Label(), URL: to.ListURL(), Count: counts[s],
			Current: c.Tag == "" && c.Shelf == s})
	}
	for _, name := range tags {
		to := listCtx{Shelf: ShelfAll, Tag: name, Q: c.Q}
		v.Tags = append(v.Tags, tagLink{Name: name, URL: to.ListURL(), Current: c.Tag == name})
	}
	return v
}

type rowView struct {
	ID      int64
	URL     string
	Title   string
	Byline  string // "Authors · Series #3"
	Note    string // what the book's shelf says about it: "Started 3 Oct 2026"
	Spine   string // swatch colour name
	Initial string
	Active  bool
}

type listView struct {
	Heading string
	Rows    []rowView
	Empty   string
}

func listHeading(c listCtx) string {
	if c.Tag != "" {
		return "Tagged “" + c.Tag + "”"
	}
	return c.Shelf.Label()
}

func viewList(items []ListItem, c listCtx, openID int64) listView {
	v := listView{Heading: listHeading(c)}
	for _, it := range items {
		v.Rows = append(v.Rows, rowView{ID: it.ID, URL: c.BookURL(it.ID), Title: it.Title,
			Byline: byline(it.Authors, seriesText(it.SeriesName, it.SeriesNumber)), Note: rowNote(it),
			Spine: SpineColor(it.Title), Initial: initial(it.Title), Active: it.ID == openID})
	}
	if len(v.Rows) == 0 {
		v.Empty = emptyText(c)
	}
	return v
}

func byline(authors, series string) string {
	var parts []string
	for _, p := range []string{authors, series} {
		if p != "" {
			parts = append(parts, p)
		}
	}
	return strings.Join(parts, " · ")
}

func seriesText(name, number string) string {
	switch {
	case name == "":
		return ""
	case number == "":
		return name
	}
	return name + " #" + number
}

// rowNote is the right-hand side of a row. B2 replaces the Reading and
// Read notes with a progress bar and stars.
func rowNote(it ListItem) string {
	switch it.Shelf {
	case ShelfReading:
		if it.StartedOn != "" {
			return "Started " + ShowDay(it.StartedOn)
		}
		return "Reading"
	case ShelfRead:
		if it.FinishedOn != "" {
			return "Finished " + ShowDay(it.FinishedOn)
		}
		return "Read"
	case ShelfDNF:
		return "Did not finish"
	}
	return "Added " + it.AddedAt.Local().Format("2 Jan 2006")
}

// initial is the first letter or digit of a title, for the mini spine.
func initial(title string) string {
	for _, r := range title {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return strings.ToUpper(string(r))
		}
	}
	return "?"
}

func emptyText(c listCtx) string {
	switch {
	case c.Q != "":
		return "No books match “" + c.Q + "”."
	case c.Tag != "":
		return "No books tagged “" + c.Tag + "”."
	}
	switch c.Shelf {
	case ShelfReading:
		return "Nothing on the go. Start something from Want to read, or add a book."
	case ShelfWant:
		return "Nothing waiting. Add a book you'd like to read."
	case ShelfRead:
		return "No finished books yet."
	case ShelfDNF:
		return "Nothing abandoned."
	}
	return "No books yet. Click Add book to start."
}

// bookView is the book pane. Selected is false when no book is open.
type bookView struct {
	Selected                         bool
	ID                               int64
	Title, Subtitle, Authors, Series string
	Facts                            []string // "2011", "592 pages", "ISBN 978…"
	Description                      string
	Spine                            string
	ShelfLabel                       string
	Tags                             []string
	TagsValue                        string // the tags box: "classics, sf"
	// The reading box.
	Reading    bool   // a reading is in progress
	StartedOn  string // "3 Oct 2026"; "" when unknown
	MinDay     string // the earliest finish date allowed (the start), YYYY-MM-DD
	Today      string // the latest date allowed, YYYY-MM-DD
	StartLabel string // "Start reading", "Read again" or "Start again"
	Ctx        listCtx
	Shell      render.Shell
}

// viewBook draws a book; today bounds the reading box's date fields.
func viewBook(b Book, c listCtx, today string) bookView {
	v := bookView{Selected: true, ID: b.ID, Title: b.Title, Subtitle: b.Subtitle, Authors: b.Authors,
		Series: seriesText(b.SeriesName, b.SeriesNumber), Description: b.Description,
		Spine: SpineColor(b.Title), ShelfLabel: b.Shelf.Label(),
		Tags: b.Tags, TagsValue: strings.Join(b.Tags, ", "), Today: today, Ctx: c}
	if b.Year > 0 {
		v.Facts = append(v.Facts, strconv.Itoa(b.Year))
	}
	if b.Pages > 0 {
		v.Facts = append(v.Facts, strconv.Itoa(b.Pages)+" pages")
	}
	if b.ISBN != "" {
		v.Facts = append(v.Facts, "ISBN "+b.ISBN)
	}
	switch {
	case b.Latest.Status == StatusReading:
		v.Reading = true
		v.MinDay = b.Latest.StartedOn
		if b.Latest.StartedOn != "" {
			v.StartedOn = ShowDay(b.Latest.StartedOn)
		}
	case b.Shelf == ShelfRead:
		v.StartLabel = "Read again"
	case b.Shelf == ShelfDNF:
		v.StartLabel = "Start again"
	default:
		v.StartLabel = "Start reading"
	}
	return v
}

// EditURL is a book's edit page, coming back to this list afterwards.
func (c listCtx) EditURL(id int64) string {
	return "/books/edit/" + strconv.FormatInt(id, 10) + "?" + c.Query()
}
