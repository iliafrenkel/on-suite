package books

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/iliafrenkel/on-suite/internal/platform/app"
)

var _ app.ToolProvider = (*App)(nil)

const (
	defaultListLimit = 50
	maxListLimit     = 200
	// recentExtras is how many notes and quotes books_get returns: enough
	// to show what the reader has been thinking, not to dump a whole book's
	// margins into the model's context (spec, "Tool rules").
	recentExtras = 5
)

// Tools returns ON Books' read-only MCP tools (spec
// 2026-11-24-ai-access-mcp-design.md, "How apps contribute tools"). They
// read a.store when called, so they work on whatever Mount set up; every
// call is scoped to the token's user, never to an argument.
func (a *App) Tools(deps app.Deps) []app.Tool {
	return []app.Tool{
		{
			Name:  "books_list",
			Title: "List books",
			Description: "List the user's books by shelf. Use shelf=want for books they plan to read and shelf=read with ratings " +
				"to learn their taste; that's how to answer 'what should I read next?'. The result also counts the books on every shelf.",
			Scope: "books:read", ReadOnly: true,
			Input: listToolArgs{},
			Call:  a.listTool,
		},
		{
			Name:  "books_get",
			Title: "Get a book",
			Description: "Get one book by the id books_list returned: its details, review, tags, and its five newest notes and quotes. " +
				"Use it to see what the user thought of a book or what they highlighted.",
			Scope: "books:read", ReadOnly: true,
			Input: getToolArgs{},
			Call:  a.getTool,
		},
		{
			Name:  "books_stats",
			Title: "Reading stats",
			Description: "Reading stats for a year (default: this year): books and pages finished, average rating, books finished per month, " +
				"the reading goal if one is set, and all-time totals. Use it to answer how much the user reads.",
			Scope: "books:read", ReadOnly: true,
			Input: statsToolArgs{},
			Call:  a.statsTool,
		},
		{
			Name:  "books_start_reading",
			Title: "Start reading",
			Description: "Starts a new reading today: the first, or a re-read of a finished book. " +
				"Refused if the book is already being read.",
			Scope:      "books:write",
			Idempotent: true,
			Input:      idToolArgs{},
			Call:       a.startReadingTool,
		},
		{
			Name:  "books_update_progress",
			Title: "Update progress",
			Description: "Records where the user is in a book they're reading. Give exactly one of page or percent. " +
				"Reaching the end doesn't finish the book; use books_finish.",
			Scope: "books:write",
			Input: progressToolArgs{},
			Call:  a.progressTool,
		},
		{
			Name:  "books_finish",
			Title: "Finish a book",
			Description: "Marks the book being read as finished, today unless day (YYYY-MM-DD) is given. " +
				"rating 1-5 rates it; leave it out to keep the current rating.",
			Scope:      "books:write",
			Idempotent: true,
			Input:      finishToolArgs{},
			Call:       a.finishTool,
		},
		{
			Name:        "books_mark_dnf",
			Title:       "Stop reading (DNF)",
			Description: "Marks the book being read as did-not-finish, today unless day (YYYY-MM-DD) is given.",
			Scope:       "books:write",
			Idempotent:  true,
			Input:       dnfToolArgs{},
			Call:        a.dnfTool,
		},
	}
}

// Optional arguments carry omitempty so the generated schema does not list
// them as required; only books_get's id is.
type listToolArgs struct {
	Shelf string `json:"shelf,omitempty" jsonschema:"want, reading, read, dnf or all (default all)"`
	Query string `json:"query,omitempty" jsonschema:"words to search for in titles, authors, reviews, notes and quotes"`
	Tag   string `json:"tag,omitempty" jsonschema:"only books with this tag"`
	Limit int    `json:"limit,omitempty" jsonschema:"most books to return (default 50, at most 200)"`
}

type getToolArgs struct {
	ID int64 `json:"id" jsonschema:"the book's id, from books_list"`
}

type statsToolArgs struct {
	Year int `json:"year,omitempty" jsonschema:"the year to count (default: the current year)"`
}

// toolBook is a book as the model sees it: compact, with empty fields left
// out (PATTERNS.md, "View-model projection").
type toolBook struct {
	ID         int64  `json:"id"`
	Title      string `json:"title"`
	Authors    string `json:"authors,omitempty"`
	Series     string `json:"series,omitempty"`
	Shelf      Shelf  `json:"shelf"`
	Rating     int    `json:"rating,omitempty"` // 0 is unrated: left out
	Pages      int    `json:"pages,omitempty"`
	StartedOn  string `json:"started_on,omitempty"`
	FinishedOn string `json:"finished_on,omitempty"`
	Progress   string `json:"progress,omitempty"`
}

// bookToolView projects a book for the model; books_get and every write
// tool's result share it.
func bookToolView(b Book) toolBook {
	return toolBook{
		ID: b.ID, Title: b.Title, Authors: b.Authors, Series: toolSeries(b.SeriesName, b.SeriesNumber),
		Shelf: b.Shelf, Rating: b.Rating, Pages: b.Pages, StartedOn: b.Latest.StartedOn, FinishedOn: b.Latest.FinishedOn,
		Progress: toolProgress(b.Shelf, b.Progress, b.Latest.Format, b.Pages),
	}
}

// toolSeries is "Name #3", or the name alone.
func toolSeries(name, number string) string {
	if name != "" && number != "" {
		return name + " #" + number
	}
	return name
}

// toolProgress is a book's progress on the Reading shelf, the way the tile
// shows it: "p. 212/412" when counted in pages, else "45%". "" for a book
// not being read or with nothing recorded.
func toolProgress(shelf Shelf, p Progress, format string, pages int) string {
	if shelf != ShelfReading || !p.Set() {
		return ""
	}
	if UnitFor(format, pages) == UnitPage {
		return "p. " + strconv.Itoa(p.In(UnitPage, pages)) + "/" + strconv.Itoa(pages)
	}
	return strconv.Itoa(p.Percent(pages)) + "%"
}

func (a *App) listTool(ctx context.Context, userID int64, args json.RawMessage) (string, error) {
	var in listToolArgs
	if err := parseToolArgs(args, &in); err != nil {
		return "", err
	}
	shelf := ShelfAll
	if in.Shelf != "" {
		s, ok := ParseShelf(in.Shelf)
		if !ok {
			return "", app.ToolError{Msg: "shelf must be one of want, reading, read, dnf or all."}
		}
		shelf = s
	}
	limit := in.Limit
	if limit <= 0 {
		limit = defaultListLimit
	}
	limit = min(limit, maxListLimit)

	items, err := a.store.List(ctx, userID, ListQuery{Shelf: shelf, Tag: in.Tag, Q: in.Query})
	if err != nil {
		return "", err
	}
	counts, err := a.store.ShelfCounts(ctx, userID)
	if err != nil {
		return "", err
	}
	out := struct {
		Counts map[Shelf]int `json:"counts"`
		Books  []toolBook    `json:"books"`
	}{Counts: map[Shelf]int{}, Books: []toolBook{}}
	for _, sh := range Shelves {
		out.Counts[sh] = counts[sh]
	}
	for _, it := range items[:min(limit, len(items))] {
		out.Books = append(out.Books, toolBook{
			ID: it.ID, Title: it.Title, Authors: it.Authors, Series: toolSeries(it.SeriesName, it.SeriesNumber),
			Shelf: it.Shelf, Rating: it.Rating, Pages: it.Pages, StartedOn: it.StartedOn, FinishedOn: it.FinishedOn,
			Progress: toolProgress(it.Shelf, it.Progress, it.Format, it.Pages),
		})
	}
	return marshalTool(out)
}

func (a *App) getTool(ctx context.Context, userID int64, args json.RawMessage) (string, error) {
	var in getToolArgs
	if err := parseToolArgs(args, &in); err != nil {
		return "", err
	}
	b, err := a.store.Get(ctx, userID, in.ID)
	if err != nil {
		return "", toolErr(err)
	}
	notes, err := a.store.Notes(ctx, userID, b.ID)
	if err != nil {
		return "", err
	}
	quotes, err := a.store.Quotes(ctx, userID, b.ID)
	if err != nil {
		return "", err
	}
	type note struct {
		Page int    `json:"page,omitempty"`
		Body string `json:"body"`
	}
	type quote struct {
		Page int    `json:"page,omitempty"`
		Text string `json:"text"`
	}
	out := struct {
		toolBook
		Subtitle    string   `json:"subtitle,omitempty"`
		Year        int      `json:"year,omitempty"`
		Tags        []string `json:"tags,omitempty"`
		Review      string   `json:"review,omitempty"`
		Description string   `json:"description,omitempty"`
		Notes       []note   `json:"notes,omitempty"`
		Quotes      []quote  `json:"quotes,omitempty"`
	}{
		toolBook: bookToolView(b),
		Subtitle: b.Subtitle, Year: b.Year, Tags: b.Tags, Review: b.Review, Description: b.Description,
	}
	for _, n := range notes[:min(recentExtras, len(notes))] {
		out.Notes = append(out.Notes, note{Page: n.Page, Body: n.Body})
	}
	for _, q := range quotes[:min(recentExtras, len(quotes))] {
		out.Quotes = append(out.Quotes, quote{Page: q.Page, Text: q.Text})
	}
	return marshalTool(out)
}

func (a *App) statsTool(ctx context.Context, userID int64, args json.RawMessage) (string, error) {
	var in statsToolArgs
	if err := parseToolArgs(args, &in); err != nil {
		return "", err
	}
	year := in.Year
	if year == 0 {
		// The store's clock, not time.Now: tests pin it (TestAppsReadTheirStoreClock).
		year = a.store.now().Local().Year()
	}
	if year < MinYear || year > 9999 {
		return "", app.ToolError{Msg: fmt.Sprintf("year must be between %d and 9999.", MinYear)}
	}
	st, err := a.store.Stats(ctx, userID, year)
	if err != nil {
		return "", err
	}
	g, err := a.store.Goal(ctx, userID, year)
	if err != nil {
		return "", err
	}
	out := struct {
		Year      int     `json:"year"`
		Goal      *int    `json:"goal"` // null when no goal is set
		Finished  int     `json:"finished"`
		Pages     int     `json:"pages"`
		AvgRating float64 `json:"avg_rating,omitempty"`
		ByMonth   [12]int `json:"by_month"`
		AllTime   struct {
			Finished int `json:"finished"`
			Pages    int `json:"pages"`
		} `json:"all_time"`
	}{
		Year: year, Finished: st.Year.Finished, Pages: st.Year.Pages, AvgRating: st.Year.Rating, ByMonth: st.Year.ByMonth,
	}
	if g.Target > 0 {
		out.Goal = &g.Target
	}
	out.AllTime.Finished, out.AllTime.Pages = st.All.Finished, st.All.Pages
	return marshalTool(out)
}

// parseToolArgs decodes a call's arguments; a client may send none at all
// for a tool whose arguments are all optional.
func parseToolArgs(args json.RawMessage, into any) error {
	if len(args) == 0 {
		return nil
	}
	if err := json.Unmarshal(args, into); err != nil {
		return app.ToolError{Msg: "The arguments are not valid."}
	}
	return nil
}

func marshalTool(v any) (string, error) {
	b, err := json.Marshal(v)
	return string(b), err
}
