package books

import (
	"html/template"
	"net/url"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

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
// reader-ctx. ListQuery has the same four fields, plus Sort and Reversed.
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

// values is the context as query values.
func (c listCtx) values() url.Values {
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
	return v
}

// Query is the context as a query string. Templates use the URL methods
// below rather than this: a whole URL dropped into href is left alone by
// html/template, a query string after a literal "?" gets percent-encoded.
func (c listCtx) Query() string { return c.values().Encode() }

// ListURL is this list.
func (c listCtx) ListURL() string { return "/books/?" + c.Query() }

// BookURL is a book opened from this list.
func (c listCtx) BookURL(id int64) string {
	return "/books/b/" + strconv.FormatInt(id, 10) + "?" + c.Query()
}

// AddURL is the Add book page, coming back to this list afterwards — but
// not to its search: on the Add page q is the Open Library search (UI
// redesign spec "Add and Edit pages").
func (c listCtx) AddURL() string {
	return "/books/new?" + listCtx{Shelf: c.Shelf, Tag: c.Tag, Series: c.Series}.Query()
}

// homeView is everything the home page draws (UI redesign spec "Home
// page"). Title and Shell are for fragments, which have no render.Page
// around them.
type homeView struct {
	Title   string
	Shell   render.Shell
	Ctx     listCtx
	Sidebar sidebarView
	Prefs   prefsView
	List    listView
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
	Goal    *goalView // this year's goal, when one is set (spec "Sidebar")
}

// viewSidebar keeps the filter text on every link: the filter box sits
// outside the list and keeps showing what was typed, so the lists it leads
// to keep applying it. A tag or series list highlights no shelf.
func viewSidebar(c listCtx, counts map[Shelf]int, tags []string, goal *goalView) sidebarView {
	v := sidebarView{Goal: goal}
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

// tagLinks are a book's header pills: read-only, each a link to every book
// with that tag (UI redesign spec "Header"). Tags are edited on the Edit
// page.
func tagLinks(names []string) []tagLink {
	var out []tagLink
	for _, n := range names {
		out = append(out, tagLink{Name: n, URL: listCtx{Shelf: ShelfAll, Tag: n}.ListURL()})
	}
	return out
}

// itemView is one book on the home page: a tile in the grid, a row in
// the list (UI redesign spec "Book tile", "Book row").
type itemView struct {
	ID       int64
	URL      string
	Title    string
	Authors  string
	Status   string // "Reading · 45%", "Want to read", "Read", "Did not finish"; "" when Stars says it
	Kind     string // the status line's colour: the shelf, "reading", "want", "read" or "dnf"
	Stars    string // a rated read book: "★★★★☆"
	Rating   int
	Bar      bool // a reading with progress: Percent as a bar
	Percent  int
	Date     string   // "Added 3 Oct 2026", or "Finished 2 Sep 2026" on the Read shelf
	Tags     []string // the first few: 3 in the grid, 5 in the list
	MoreTags int      // how many more, for the "+N" pill
	Spine    string   // swatch colour name
	Initial  string
	Cover    string       // the stored cover; "" draws the spine
	Snippet  *snippetView // where a searched book matched; nil when it shows already
}

// listView is the grid or the list. View is what it draws: the saved
// view, or the list while searching, so snippets have room (spec
// "Decisions at a glance").
type listView struct {
	View    View
	Heading string
	Count   int
	Items   []itemView
	Empty   string
}

// tagsShown is how many tags a tile shows, and a row (spec "Book tile").
var tagsShown = map[View]int{ViewGrid: 3, ViewList: 5}

func listHeading(c listCtx) string {
	switch {
	case c.Series != "":
		return "Series “" + c.Series + "”"
	case c.Tag != "":
		return "Tagged “" + c.Tag + "”"
	}
	return c.Shelf.Label()
}

// viewList draws items for list c in view, with each book's tags from
// tags (TagsByBook).
func viewList(items []ListItem, tags map[int64][]string, c listCtx, view View) listView {
	if c.Q != "" {
		view = ViewList
	}
	v := listView{View: view, Heading: listHeading(c), Count: len(items)}
	for _, it := range items {
		item := itemView{ID: it.ID, URL: c.BookURL(it.ID), Title: it.Title, Authors: it.Authors,
			Status: itemStatus(it), Kind: string(it.Shelf), Date: itemDate(it, c),
			Spine: SpineColor(it.Title), Initial: initial(it.Title), Cover: coverURL(it.ID, it.CoverVersion),
			Snippet: newSnippet(it)}
		if it.Shelf == ShelfReading && it.Progress.Set() {
			item.Bar, item.Percent = true, it.Progress.Percent(it.Pages)
		}
		if it.Shelf == ShelfRead && it.Rating > 0 {
			item.Status, item.Rating, item.Stars = "", it.Rating, stars(it.Rating)
		}
		item.Tags, item.MoreTags = firstTags(tags[it.ID], tagsShown[view])
		v.Items = append(v.Items, item)
	}
	if len(v.Items) == 0 {
		v.Empty = emptyText(c)
	}
	return v
}

// itemStatus is a tile's status line (spec "Book tile"): how far through
// a book being read is — in pages when that is how it is counted — or
// which shelf it is on.
func itemStatus(it ListItem) string {
	switch it.Shelf {
	case ShelfReading:
		switch {
		case !it.Progress.Set():
			return "Reading"
		case UnitFor(it.Format, it.Pages) == UnitPage:
			return "Reading · p. " + strconv.Itoa(it.Progress.In(UnitPage, it.Pages)) + " / " + strconv.Itoa(it.Pages)
		}
		return "Reading · " + strconv.Itoa(it.Progress.Percent(it.Pages)) + "%"
	case ShelfRead:
		return "Read"
	case ShelfDNF:
		return "Did not finish"
	}
	return "Want to read"
}

// itemDate is a tile's date: when the book was finished on the Read
// shelf, when it was added everywhere else (spec "Book tile").
func itemDate(it ListItem, c listCtx) string {
	if c.Shelf == ShelfRead && it.FinishedOn != "" {
		return "Finished " + ShowDay(it.FinishedOn)
	}
	return "Added " + it.AddedAt.Local().Format("2 Jan 2006")
}

// firstTags is at most n of tags, and how many are left over.
func firstTags(tags []string, n int) ([]string, int) {
	if len(tags) <= n {
		return tags, 0
	}
	return tags[:n], len(tags) - n
}

// prefsView is the toolbar's sort and view controls (spec "Toolbar").
type prefsView struct {
	Ctx       listCtx
	Sorts     []choice
	Reversed  bool
	DirLabel  string // the order now: "Newest first", "Oldest first", "A–Z", "Z–A"
	View      View   // the saved view
	SortFixed bool   // a series is in reading order: the sort is disabled
	ViewFixed bool   // a search shows the list: the view switch is disabled
}

func viewPrefs(p Prefs, c listCtx) prefsView {
	v := prefsView{Ctx: c, Reversed: p.Reversed, DirLabel: dirLabel(p.Sort, p.Reversed), View: p.View,
		SortFixed: c.Series != "", ViewFixed: c.Q != ""}
	for _, s := range Sorts {
		v.Sorts = append(v.Sorts, choice{Value: string(s), Label: s.Label(), Current: s == p.Sort})
	}
	return v
}

// dirLabel names the order a sort is in (spec "Sorting"): Title and
// Author run A–Z, the rest newest first, unless reversed.
func dirLabel(s Sort, reversed bool) string {
	alpha := s == SortTitle || s == SortAuthor
	switch {
	case alpha && reversed:
		return "Z–A"
	case alpha:
		return "A–Z"
	case reversed:
		return "Oldest first"
	}
	return "Newest first"
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

// bookView is the book page's book (UI redesign spec "Book details page").
type bookView struct {
	ID                               int64
	Title, Subtitle, Authors, Series string   // Series: "The Expanse #3 · 9 books"
	SeriesURL                        string   // the list of the book's series
	Facts                            []string // "2011", "592 pages", "ISBN 978…"
	Description                      string
	DescriptionLong                  bool // held to a few lines, with a More toggle (#566)
	Spine                            string
	Cover                            string // the stored cover; "" draws the spine
	ShelfLabel                       string
	BackLabel                        string    // the page bar's way back: the list's name, "Reading", "Tagged “sf”"
	Tags                             []tagLink // the header's pills, each its tag's list
	// The reading box.
	Reading       bool         // a reading is in progress
	Progress      progressView // its progress, when Reading
	FormatIcon    string       // its format's icon (ticon); "" while the format isn't set
	FormatChoices []choice     // the format select
	StartedOn     string       // "3 Oct 2026"; "" when unknown
	MinDay        string       // the earliest finish date allowed (the start), YYYY-MM-DD
	Today         string       // the latest date allowed, YYYY-MM-DD
	StartLabel    string       // "Start reading", "Read again" or "Start again"
	Rating        int          // 1–5, 0 for none
	RatingChoices []choice     // the Finish step's rating select
	Stars         []starButton // the rating buttons
	Review        string       // Markdown, for the edit box
	ReviewHTML    template.HTML
	History       []historyView // every reading, newest first
	HistoryOpen   bool          // the history disclosure starts open: the request came from inside it
	Pages         int           // the book's page count (0: unknown), the page boxes' max
	Notes         []noteView    // newest first
	NewNote       entryForm     // the "+ Add note" form
	Quotes        []quoteView   // newest first
	NewQuote      entryForm     // the "+ Add quote" form
	Ctx           listCtx
	Shell         render.Shell
}

// bookPageView is the book page (UI redesign spec "Book details page").
// Title and Shell are for fragments, which have no render.Page around
// them; Error is a refusal's banner.
type bookPageView struct {
	Title string
	Shell render.Shell
	Error string
	Book  bookView
}

// choice is one option of a menu or select.
type choice struct {
	Value, Label string
	Current      bool
}

var formatLabels = map[string]string{"paper": "Paper", "ebook": "Ebook", "audio": "Audiobook", "": "Not set"}

// formatIcons are drawn before the format select (UI redesign spec
// "Sections", decided 2026-10-12); a reading with no format has none.
var formatIcons = map[string]string{"paper": "book-open", "ebook": "tablet", "audio": "headphones"}

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
	v := bookView{ID: b.ID, Title: b.Title, Subtitle: b.Subtitle, Authors: b.Authors,
		Description: b.Description, Spine: SpineColor(b.Title), Cover: coverURL(b.ID, b.CoverVersion),
		ShelfLabel: b.Shelf.Label(), BackLabel: listHeading(c), Tags: tagLinks(b.Tags), Today: today, Pages: b.Pages,
		Rating: b.Rating, Stars: starButtons(b.Rating), Review: b.Review, ReviewHTML: RenderReview(b.Review), Ctx: c}
	v.DescriptionLong = longDescription(b.Description)
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
		v.FormatIcon, v.FormatChoices = formatIcons[b.Latest.Format], formatChoices(b.Latest.Format)
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

var statusLabels = map[Status]string{StatusReading: "Reading", StatusFinished: "Read", StatusDNF: "Did not finish"}

// historyView is one reading in the book pane's history, with what its
// edit form needs.
type historyView struct {
	ID          int64
	Status      string // "Reading", "Read", "Did not finish"
	Dates       string // "3 Oct 2026 – 9 Oct 2026"
	Format      string // "Paper"; "" when not set
	InProgress  bool   // no finish date to edit
	FinishLabel string // "Finished on" or "Stopped on"
	StartedOn   string // YYYY-MM-DD for the date inputs
	FinishedOn  string
	Formats     []choice
}

func viewHistory(rs []Reading) []historyView {
	var out []historyView
	for _, rd := range rs {
		h := historyView{ID: rd.ID, Status: statusLabels[rd.Status], Dates: readingDates(rd),
			InProgress: rd.Status == StatusReading, FinishLabel: "Finished on",
			StartedOn: rd.StartedOn, FinishedOn: rd.FinishedOn, Formats: formatChoices(rd.Format)}
		if rd.Format != "" {
			h.Format = formatLabels[rd.Format]
		}
		if rd.Status == StatusDNF {
			h.FinishLabel = "Stopped on"
		}
		out = append(out, h)
	}
	return out
}

// readingDates is a reading's dates for people; imported readings may
// have neither (B5).
func readingDates(rd Reading) string {
	switch {
	case rd.StartedOn != "" && rd.FinishedOn != "":
		return ShowDay(rd.StartedOn) + " – " + ShowDay(rd.FinishedOn)
	case rd.StartedOn != "":
		return "From " + ShowDay(rd.StartedOn)
	case rd.FinishedOn != "":
		return "Until " + ShowDay(rd.FinishedOn)
	}
	return "No dates"
}

// entryDraft is a note or quote form the store refused, to show again
// with its message and what was typed (spec "Errors": an inline message,
// as for progress). Form names the form: "note-new", "note-12",
// "quote-new", "quote-5". Body is a note's text or a quote's.
type entryDraft struct {
	Form, Error         string
	Page, Body, Comment string
}

// entryForm is a note or quote form's state: what its boxes hold, and
// whether it opens with a message.
type entryForm struct {
	Open                bool
	Error               string
	Page, Body, Comment string
}

// formFor is the form named key: what was typed into it when it is the
// one refused (opened, with the message), its stored values otherwise.
func formFor(key string, d entryDraft, stored entryForm) entryForm {
	if d.Form != key {
		return stored
	}
	return entryForm{Open: true, Error: d.Error, Page: d.Page, Body: d.Body, Comment: d.Comment}
}

// noteView is one note in the book pane: dated, with its page, its
// Markdown drawn as a review is (decided 2026-10-09), and its Edit form.
type noteView struct {
	ID   int64
	Date string // "9 Oct 2026"
	Page string // "p. 112"; "" for none
	HTML template.HTML
	Form entryForm
}

func viewNotes(ns []Note, d entryDraft) []noteView {
	var out []noteView
	for _, n := range ns {
		out = append(out, noteView{ID: n.ID, Date: n.CreatedAt.Local().Format("2 Jan 2006"), Page: pageText(n.Page),
			HTML: RenderReview(n.Body),
			Form: formFor("note-"+strconv.FormatInt(n.ID, 10), d, entryForm{Page: pageValue(n.Page), Body: n.Body})})
	}
	return out
}

// quoteView is one quote card: the text as typed, its line breaks kept
// (decided 2026-10-09: plain text, drawn with white-space: pre-line), its
// page, its comment drawn as Markdown, and its Edit form.
type quoteView struct {
	ID          int64
	Text        string
	Page        string // "p. 112"; "" for none
	CommentHTML template.HTML
	Form        entryForm
}

func viewQuotes(qs []Quote, d entryDraft) []quoteView {
	var out []quoteView
	for _, q := range qs {
		out = append(out, quoteView{ID: q.ID, Text: q.Text, Page: pageText(q.Page), CommentHTML: RenderReview(q.Comment),
			Form: formFor("quote-"+strconv.FormatInt(q.ID, 10), d,
				entryForm{Page: pageValue(q.Page), Body: q.Text, Comment: q.Comment})})
	}
	return out
}

// pageText is a page for people: "p. 112", "" for none.
func pageText(page int) string {
	if page == 0 {
		return ""
	}
	return "p. " + strconv.Itoa(page)
}

// pageValue is a page for a form box, "" for none.
func pageValue(page int) string {
	if page == 0 {
		return ""
	}
	return strconv.Itoa(page)
}

// progressView is the progress box: an input in the reading's unit, a
// bar and a note. Error and a typed Value come from a refused update.
type progressView struct {
	Unit    Unit
	Value   string // the input: the current progress in Unit ("" for none yet)
	Max     int    // the book's pages, or 100 for percent
	Percent int    // the bar
	Note    string // "20% · updated 9 Oct 2026"; in percent just "updated 9 Oct 2026"
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
		v.Note = "updated " + b.Progress.RecordedAt.Local().Format("2 Jan 2006")
		if u == UnitPage { // in percent, the box itself says it
			v.Note = strconv.Itoa(v.Percent) + "% · " + v.Note
		}
	}
	return v
}

// EditURL is a book's edit page, coming back to this list afterwards.
func (c listCtx) EditURL(id int64) string {
	return "/books/edit/" + strconv.FormatInt(id, 10) + "?" + c.Query()
}

// longDescription says whether a description is long enough to fold
// (#566): more than about four lines of the pane's width, or more than
// four lines of its own.
func longDescription(s string) bool {
	return utf8.RuneCountInString(s) > 300 || strings.Count(s, "\n") >= 4
}
