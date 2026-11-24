package notes

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/iliafrenkel/on-suite/internal/platform/app"
)

var _ app.ToolProvider = (*App)(nil)

const (
	defaultSearchLimit = 20
	maxSearchLimit     = 50
	defaultGetDepth    = 3
	maxGetDepth        = 10
	// noteSnippetRunes is how much of a note a search result carries: enough
	// to tell hits apart, not to copy whole notes into the model's context.
	noteSnippetRunes = 200
)

// noteNotFound is what every tool says for a missing, someone else's or
// archived bullet, so the answer never reveals which of the three it was.
const noteNotFound = "No note with that ID in your outline"

// Tools returns ON Notes' MCP tools. They read a.store when called, so they
// work on whatever Mount set up; every call is scoped to the token's user,
// never to an argument.
func (a *App) Tools(deps app.Deps) []app.Tool {
	return []app.Tool{
		{
			Name:  "notes_search",
			Title: "Search notes",
			Description: "Search the user's notes by words in a bullet's text or note. Use it to find a bullet when you don't know where it is; " +
				"each result's id is what to pass to notes_get to read around it.",
			Scope: "notes:read", ReadOnly: true,
			Input: searchToolArgs{},
			Call:  a.searchTool,
		},
		{
			Name:  "notes_get",
			Title: "Read part of the outline",
			Description: "Read a list or project from the user's outline as indented text: give an id to read that bullet and what's under it, " +
				"or leave id out to read from the top. A bullet whose children are cut off by depth says how many more there are. " +
				"Use the ids it shows for notes_add, notes_set_done and notes_set_due.",
			Scope: "notes:read", ReadOnly: true,
			Input: getToolArgs{},
			Call:  a.getTool,
		},
		{
			Name:  "notes_due",
			Title: "Notes with due dates",
			Description: "The user's unfinished bullets that have a due date, grouped as overdue, today, this week and later. " +
				"Use it to answer 'what do I need to do?'.",
			Scope: "notes:read", ReadOnly: true,
			Input: dueToolArgs{},
			Call:  a.dueTool,
		},
		{
			Name:  "notes_add",
			Title: "Add a note",
			Description: "Add a bullet to the user's outline, at the end of a parent's children or at the top level. " +
				"Get a parent's id from notes_search or notes_get. Text after the first line becomes the bullet's note. " +
				"Give due (YYYY-MM-DD) to set a due date at the same time; work out dates like 'Friday' from today's date yourself.",
			Scope: "notes:write",
			Input: addToolArgs{},
			Call:  a.addTool,
		},
		{
			Name:  "notes_set_done",
			Title: "Tick a note done",
			Description: "Tick a bullet done, or un-tick it with done=false. " +
				"Ticking a parent doesn't tick its children.",
			Scope: "notes:write", Idempotent: true,
			Input: setDoneToolArgs{},
			Call:  a.setDoneTool,
		},
		{
			Name:  "notes_set_due",
			Title: "Set a due date",
			Description: "Set or change a bullet's due date (YYYY-MM-DD), or clear it with an empty due. " +
				"Work out dates like 'next Tuesday' from today's date yourself. Bullets with a due date show up in notes_due.",
			Scope: "notes:write", Idempotent: true,
			Input: setDueToolArgs{},
			Call:  a.setDueTool,
		},
	}
}

// Optional arguments carry omitempty so the generated schema does not list
// them as required; only notes_search's query is.
type searchToolArgs struct {
	Query string `json:"query" jsonschema:"words to search for in bullets and their notes"`
	Limit int    `json:"limit,omitempty" jsonschema:"most results to return (default 20, at most 50)"`
}

type getToolArgs struct {
	ID    int64 `json:"id,omitempty" jsonschema:"the bullet's id, from notes_search or an earlier notes_get; leave out to read from the top"`
	Depth int   `json:"depth,omitempty" jsonschema:"how many levels to show (default 3, from 1 to 10)"`
}

type dueToolArgs struct{}

// toolErr turns a store error into what the model should see (spec, "Tool
// rules"). Anything it doesn't recognise stays a plain error, which the
// server reports as "Something went wrong."
func toolErr(err error) error {
	switch {
	case errors.Is(err, ErrNotFound):
		return app.ToolError{Msg: noteNotFound}
	case errors.Is(err, ErrTooDeep):
		return app.ToolError{Msg: "That would nest the outline too deeply."}
	case errors.Is(err, ErrInvalid):
		detail, ok := strings.CutPrefix(err.Error(), ErrInvalid.Error()+": ")
		if !ok || detail == "" {
			return app.ToolError{Msg: "The note isn't valid."}
		}
		r, size := utf8.DecodeRuneInString(detail)
		return app.ToolError{Msg: string(unicode.ToUpper(r)) + detail[size:] + "."}
	}
	return err
}

// toolPath is a bullet's ancestors as the model reads them: titles joined
// with " › ", outermost first, or "" for a top-level bullet.
func toolPath(crumbs []Node) string {
	titles := make([]string, len(crumbs))
	for i, c := range crumbs {
		titles[i] = c.DisplayTitle()
	}
	return strings.Join(titles, " › ")
}

// visibleNode loads a bullet the model may see or change, with its ancestors
// outermost first. A missing, someone else's or archived bullet, or one under
// an archived ancestor (spec §13), gives the same not-found ToolError.
func (a *App) visibleNode(ctx context.Context, userID, id int64) (Node, []Node, error) {
	n, err := a.store.ByID(ctx, userID, id)
	if err != nil {
		return Node{}, nil, toolErr(err)
	}
	crumbs, err := a.store.AncestorsMany(ctx, userID, []int64{n.ID})
	if err != nil {
		return Node{}, nil, err
	}
	hidden := n.Archived
	for _, c := range crumbs[n.ID] {
		hidden = hidden || c.Archived
	}
	if hidden {
		return Node{}, nil, app.ToolError{Msg: noteNotFound}
	}
	return n, crumbs[n.ID], nil
}

func (a *App) searchTool(ctx context.Context, userID int64, args json.RawMessage) (string, error) {
	var in searchToolArgs
	if err := app.ParseToolArgs(args, &in); err != nil {
		return "", err
	}
	if strings.TrimSpace(in.Query) == "" {
		return "", app.ToolError{Msg: "Give some words to search for."}
	}
	limit := in.Limit
	if limit <= 0 {
		limit = defaultSearchLimit
	}
	limit = min(limit, maxSearchLimit)

	// Done bullets match too: finding what was already done is a fair question.
	found, err := a.store.Search(ctx, userID, in.Query, true)
	if err != nil {
		return "", err
	}
	found = found[:min(limit, len(found))]
	crumbs, err := a.store.AncestorsMany(ctx, userID, idsOf(found))
	if err != nil {
		return "", err
	}
	type result struct {
		ID    int64  `json:"id"`
		Title string `json:"title"`
		Note  string `json:"note,omitempty"`
		Done  bool   `json:"done,omitempty"`
		Due   string `json:"due,omitempty"`
		Path  string `json:"path,omitempty"`
	}
	out := struct {
		Results []result `json:"results"`
	}{Results: []result{}}
	for _, n := range found {
		out.Results = append(out.Results, result{
			ID: n.ID, Title: n.DisplayTitle(), Note: snippetOf(n.Note), Done: n.Done, Due: n.DueOn, Path: toolPath(crumbs[n.ID]),
		})
	}
	return app.MarshalTool(out)
}

// snippetOf is the first noteSnippetRunes runes of a note, with "…" when cut.
func snippetOf(note string) string {
	if utf8.RuneCountInString(note) <= noteSnippetRunes {
		return note
	}
	return string([]rune(note)[:noteSnippetRunes]) + "…"
}

func (a *App) getTool(ctx context.Context, userID int64, args json.RawMessage) (string, error) {
	var in getToolArgs
	if err := app.ParseToolArgs(args, &in); err != nil {
		return "", err
	}
	depth := in.Depth
	if depth == 0 {
		depth = defaultGetDepth
	}
	if depth < 1 || depth > maxGetDepth {
		return "", app.ToolError{Msg: "Give a depth from 1 to 10."}
	}

	var b strings.Builder
	rootID := int64(RootID)
	base := 0 // the indent of the outline's first level
	if in.ID != 0 {
		root, crumbs, err := a.visibleNode(ctx, userID, in.ID)
		if err != nil {
			return "", err
		}
		if path := toolPath(crumbs); path != "" {
			b.WriteString("Path: " + path + "\n")
		}
		// The root's children are listed (depth is at least 1), so it never
		// needs a "more" count.
		writeBullet(&b, root, 0, false)
		rootID, base = root.ID, 1
	}

	rows, err := a.store.Outline(ctx, userID, rootID, true, true)
	if err != nil {
		return "", err
	}
	shown := 0
	for _, n := range rows {
		if n.Depth >= depth {
			continue
		}
		writeBullet(&b, n, n.Depth+base, n.Depth+1 >= depth)
		shown++
	}
	if in.ID == 0 && shown == 0 {
		return "Your outline is empty.", nil
	}
	return app.CapToolText(strings.TrimRight(b.String(), "\n")), nil
}

// writeBullet writes one bullet's line, then its note. cut says the
// bullet's children are past the depth limit, so a count stands in for them.
func writeBullet(b *strings.Builder, n Node, indent int, cut bool) {
	pad := strings.Repeat("  ", indent)
	b.WriteString(pad + "- " + n.DisplayTitle() + " [id " + strconv.FormatInt(n.ID, 10) + "]")
	if n.Done {
		b.WriteString(" [done]")
	}
	if n.DueOn != "" {
		b.WriteString(" (due " + n.DueOn + ")")
	}
	if cut && n.ChildCount > 0 {
		b.WriteString(" (+" + strconv.Itoa(n.ChildCount) + " more)")
	}
	b.WriteByte('\n')
	if n.Note == "" {
		return
	}
	for line := range strings.SplitSeq(n.Note, "\n") {
		b.WriteString(pad + "  | " + line + "\n")
	}
}

func (a *App) dueTool(ctx context.Context, userID int64, args json.RawMessage) (string, error) {
	var in dueToolArgs
	if err := app.ParseToolArgs(args, &in); err != nil {
		return "", err
	}
	nodes, err := a.store.Due(ctx, userID, "")
	if err != nil {
		return "", err
	}
	crumbs, err := a.store.AncestorsMany(ctx, userID, idsOf(nodes))
	if err != nil {
		return "", err
	}
	rows := make([]DueRow, len(nodes))
	for i, n := range nodes {
		rows[i] = DueRow{Node: n, Crumbs: crumbs[n.ID]}
	}
	g := GroupByDue(rows, a.store.now().Local())

	type entry struct {
		ID    int64  `json:"id"`
		Title string `json:"title"`
		Due   string `json:"due"`
		Path  string `json:"path,omitempty"`
	}
	entries := func(rows []DueRow) []entry {
		out := make([]entry, 0, len(rows))
		for _, r := range rows {
			out = append(out, entry{ID: r.ID, Title: r.DisplayTitle(), Due: r.DueOn, Path: toolPath(r.Crumbs)})
		}
		return out
	}
	return app.MarshalTool(struct {
		Overdue  []entry `json:"overdue"`
		Today    []entry `json:"today"`
		ThisWeek []entry `json:"this_week"`
		Later    []entry `json:"later"`
	}{entries(g.Overdue), entries(g.Today), entries(g.ThisWeek), entries(g.Later)})
}
