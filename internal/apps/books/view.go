package books

import (
	"html/template"
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
// tag, one series and a title/author filter. GETs carry it in the query
// string and POSTs in hidden fields (ctx-fields), so whatever a change
// re-renders comes back to the same list — the lesson of Reader's
// reader-ctx. Its fields are ListQuery's, in the same order.
type listCtx struct {
	Shelf  Shelf
	Tag    string
	Q      string
	Series string
}

// ctxFrom reads a list context; a missing or unknown shelf is Reading, the
// default shelf (spec "Layout").
func ctxFrom(get func(string) string) listCtx {
	sh, ok := ParseShelf(get("shelf"))
	if !ok {
		sh = ShelfReading
	}
	return listCtx{Shelf: sh, Tag: strings.ToLower(strings.TrimSpace(get("tag"))), Q: strings.TrimSpace(get("q")),
		Series: strings.TrimSpace(get("series"))}
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
	if c.Series != "" {
		v.Set("series", c.Series)
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
// to keep applying it. A tag or series list highlights no shelf.
func viewSidebar(c listCtx, counts map[Shelf]int, tags []string) sidebarView {
	var v sidebarView
	for _, s := range Shelves {
		to := listCtx{Shelf: s, Q: c.Q}
		v.Shelves = append(v.Shelves, shelfLink{Label: s.Label(), URL: to.ListURL(), Count: counts[s],
			Current: c.Tag == "" && c.Series == "" && c.Shelf == s})
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
	Note    string // what the book's shelf says about it: "Started 3 Oct 2026", "20%"
	Bar     bool   // a reading with progress: draw Percent as a bar before Note
	Percent int
	Rating  int    // 1–5 on a read book, 0 otherwise
	Stars   string // Rating drawn: "★★★★☆"
	Spine   string // swatch colour name
	Initial string
	Cover   string // the stored cover; "" draws the mini spine
	Active  bool
}

type listView struct {
	Ctx     listCtx // the list's own context, for books.js to sync the book pane
	Heading string
	Rows    []rowView
	Empty   string
	OOB     bool // swapped out of band, alongside a progress update
}

func listHeading(c listCtx) string {
	switch {
	case c.Series != "":
		return "Series “" + c.Series + "”"
	case c.Tag != "":
		return "Tagged “" + c.Tag + "”"
	}
	return c.Shelf.Label()
}

func viewList(items []ListItem, c listCtx, openID int64) listView {
	v := listView{Ctx: c, Heading: listHeading(c)}
	for _, it := range items {
		row := rowView{ID: it.ID, URL: c.BookURL(it.ID), Title: it.Title,
			Byline: byline(it.Authors, seriesText(it.SeriesName, it.SeriesNumber)), Note: rowNote(it),
			Spine: SpineColor(it.Title), Initial: initial(it.Title), Cover: coverURL(it.ID, it.CoverVersion), Active: it.ID == openID}
		switch it.Shelf {
		case ShelfReading:
			row.Bar, row.Percent = it.Progress.Set(), it.Progress.Percent(it.Pages)
		case ShelfRead:
			row.Rating, row.Stars = it.Rating, stars(it.Rating)
		}
		v.Rows = append(v.Rows, row)
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

// coverURL is a book's stored cover; the version makes a new cover a new
// URL, so the old one can be cached for a year. "" draws the spine.
func coverURL(id int64, version string) string {
	if version == "" {
		return ""
	}
	return "/books/cover/" + strconv.FormatInt(id, 10) + "?v=" + version
}

// rowNote is the text on the right-hand side of a row (spec "Layout"):
// how far through a book being read is, when a read book was finished.
func rowNote(it ListItem) string {
	switch it.Shelf {
	case ShelfReading:
		if it.Progress.Set() {
			return strconv.Itoa(it.Progress.Percent(it.Pages)) + "%"
		}
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

// stars draws a 1–5 rating as five stars, "" for none.
func stars(rating int) string {
	if rating < 1 || rating > 5 {
		return ""
	}
	return strings.Repeat("★", rating) + strings.Repeat("☆", 5-rating)
}

// countText is "1 book" or "9 books".
func countText(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return strconv.Itoa(n) + " " + many
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
	case c.Series != "":
		return "No books in the series “" + c.Series + "”."
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
	Title, Subtitle, Authors, Series string   // Series: "The Expanse #3 · 9 books"
	SeriesURL                        string   // the list of the book's series
	Facts                            []string // "2011", "592 pages", "ISBN 978…"
	Description                      string
	Spine                            string
	Cover                            string // the stored cover; "" draws the spine
	ShelfLabel                       string
	Tags                             []string
	TagsValue                        string // the tags box: "classics, sf"
	// The reading box.
	Reading       bool         // a reading is in progress
	Progress      progressView // its progress, when Reading
	FormatLabel   string       // its format, for the pill: "Paper", "Format not set"
	FormatChoices []choice     // the format menu
	StartedOn     string       // "3 Oct 2026"; "" when unknown
	MinDay        string       // the earliest finish date allowed (the start), YYYY-MM-DD
	Today         string       // the latest date allowed, YYYY-MM-DD
	StartLabel    string       // "Start reading", "Read again" or "Start again"
	Rating        int          // 1–5, 0 for none
	RatingChoices []choice     // the Finish step's rating select
	Stars         []starButton // the rating buttons
	Review        string       // Markdown, for the edit box
	ReviewHTML    template.HTML
	Ctx           listCtx
	Shell         render.Shell
}

// choice is one option of a menu or select.
type choice struct {
	Value, Label string
	Current      bool
}

var formatLabels = map[string]string{"paper": "Paper", "ebook": "Ebook", "audio": "Audiobook", "": "Not set"}

// formatChoices is the format menu: every format, then "not set".
func formatChoices(current string) []choice {
	var out []choice
	for _, f := range Formats {
		out = append(out, choice{Value: f, Label: formatLabels[f], Current: f == current})
	}
	return append(out, choice{Value: "", Label: formatLabels[""], Current: current == ""})
}

// starButton is one of the book pane's five rating buttons.
type starButton struct {
	Value int // what clicking it sets: its number, or 0 to clear
	Label string
	On    bool // drawn filled
}

// starButtons are the book pane's rating (spec "Book pane": click to set,
// click again to clear): star n sets the rating to n, except the current
// rating's own star, which clears it.
func starButtons(rating int) []starButton {
	var out []starButton
	for n := 1; n <= 5; n++ {
		b := starButton{Value: n, Label: "Rate it " + strconv.Itoa(n) + " of 5", On: n <= rating}
		if n == rating {
			b.Value, b.Label = 0, "Clear the rating ("+strconv.Itoa(n)+" of 5)"
		}
		out = append(out, b)
	}
	return out
}

// ratingChoices is the Finish step's rating select, best first; the book's
// rating is picked already, so finishing a re-read keeps it unless changed.
func ratingChoices(current int) []choice {
	var out []choice
	for n := 5; n >= 1; n-- {
		out = append(out, choice{Value: strconv.Itoa(n), Label: stars(n) + " " + strconv.Itoa(n) + " of 5", Current: n == current})
	}
	return out
}

// viewBook draws a book; today bounds the reading box's date fields.
func viewBook(b Book, c listCtx, today string) bookView {
	v := bookView{Selected: true, ID: b.ID, Title: b.Title, Subtitle: b.Subtitle, Authors: b.Authors,
		Description: b.Description, Spine: SpineColor(b.Title), Cover: coverURL(b.ID, b.CoverVersion),
		ShelfLabel: b.Shelf.Label(), Tags: b.Tags, TagsValue: strings.Join(b.Tags, ", "), Today: today,
		Rating: b.Rating, Stars: starButtons(b.Rating), Review: b.Review, ReviewHTML: RenderReview(b.Review), Ctx: c}
	if b.SeriesName != "" {
		v.Series = seriesText(b.SeriesName, b.SeriesNumber)
		if b.SeriesBooks > 1 { // the count only says something once there are two
			v.Series += " · " + countText(b.SeriesBooks, "book", "books")
		}
		v.SeriesURL = listCtx{Shelf: ShelfAll, Series: b.SeriesName}.ListURL()
	}
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
		v.Progress = viewProgress(b)
		v.FormatLabel, v.FormatChoices = "Format not set", formatChoices(b.Latest.Format)
		if b.Latest.Format != "" {
			v.FormatLabel = formatLabels[b.Latest.Format]
		}
		v.RatingChoices = ratingChoices(b.Rating)
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

// progressView is the progress box: an input in the reading's unit, a
// bar and a note. Error and a typed Value come from a refused update.
type progressView struct {
	Unit    Unit
	Value   string // the input: the current progress in Unit ("" for none yet)
	Max     int    // the book's pages, or 100 for percent
	Percent int    // the bar
	Note    string // "20% · updated 9 Oct 2026"
	Error   string
}

// viewProgress is the progress box of b's reading in progress, in the unit
// it is counted in now (UnitFor); an older row in the other unit converts.
func viewProgress(b Book) progressView {
	u := UnitFor(b.Latest.Format, b.Pages)
	v := progressView{Unit: u, Max: 100, Percent: b.Progress.Percent(b.Pages), Note: "No progress yet."}
	if u == UnitPage {
		v.Max = b.Pages
	}
	if b.Progress.Set() {
		v.Value = strconv.Itoa(b.Progress.In(u, b.Pages))
		v.Note = strconv.Itoa(v.Percent) + "% · updated " + b.Progress.RecordedAt.Local().Format("2 Jan 2006")
	}
	return v
}

// EditURL is a book's edit page, coming back to this list afterwards.
func (c listCtx) EditURL(id int64) string {
	return "/books/edit/" + strconv.FormatInt(id, 10) + "?" + c.Query()
}
