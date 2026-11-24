package later

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"strconv"
	"strings"

	"github.com/iliafrenkel/on-suite/internal/platform/app"
)

var _ app.ToolProvider = (*App)(nil)

const (
	defaultListLimit = 20
	maxListLimit     = 100
)

// articleNotFound is what every tool says for a missing or someone else's
// article, so the answer never reveals which of the two it was.
const articleNotFound = "No article with that ID in Later"

// Tools returns ON Later's MCP tools. They read a.store when called, so they
// work on whatever Mount set up; every call is scoped to the token's user,
// never to an argument.
func (a *App) Tools(deps app.Deps) []app.Tool {
	return []app.Tool{
		{
			Name:  "later_list",
			Title: "List saved articles",
			Description: "List the articles the user saved in ON Later: unread ones by default, or reading or archived ones with state. " +
				"Give tag to look at one tag. The result also gives the counts for each state and all the user's tag names. " +
				"Each article's id is what to pass to later_get_article.",
			Scope: "later:read", ReadOnly: true,
			Input: laterListArgs{},
			Call:  a.listTool,
		},
		{
			Name:  "later_search",
			Title: "Search saved articles",
			Description: "Search the user's saved articles by words in the title, text, note or highlights, in any state. " +
				"Use it to find an article when you don't know where it is; each result's id is what to pass to later_get_article.",
			Scope: "later:read", ReadOnly: true,
			Input: laterSearchArgs{},
			Call:  a.searchTool,
		},
		{
			Name:  "later_get_article",
			Title: "Read a saved article",
			Description: "Read one saved article's text, to read it or summarise it for the user. It doesn't change the article's state. " +
				"Get the id from later_list or later_search.",
			Scope: "later:read", ReadOnly: true,
			Input: laterGetArgs{},
			Call:  a.getArticleTool,
		},
	}
}

// Optional arguments carry omitempty so the generated schema does not list
// them as required.
type laterListArgs struct {
	State string `json:"state,omitempty" jsonschema:"unread (the default), reading or archived"`
	Tag   string `json:"tag,omitempty" jsonschema:"a tag name from this tool's tags, to list just that tag"`
	Limit int    `json:"limit,omitempty" jsonschema:"most articles to return (default 20, at most 100)"`
}

type laterSearchArgs struct {
	Query string `json:"query" jsonschema:"words to search for in titles, text, notes and highlights"`
	Tag   string `json:"tag,omitempty" jsonschema:"a tag name, to search just that tag"`
	Limit int    `json:"limit,omitempty" jsonschema:"most articles to return (default 20, at most 100)"`
}

type laterGetArgs struct {
	ID int64 `json:"id" jsonschema:"the article's id, from later_list or later_search"`
}

type toolArticle struct {
	ID       int64    `json:"id"`
	Title    string   `json:"title"`
	Site     string   `json:"site"`
	State    State    `json:"state"`
	Words    int      `json:"words,omitempty"`
	Progress int      `json:"progress,omitempty"`
	Tags     []string `json:"tags,omitempty"`
}

// toolEntry is one article as the list and search tools show it. Progress is
// a whole percent.
func toolEntry(it ListItem) toolArticle {
	return toolArticle{
		ID: it.ID, Title: it.Title, Site: it.SiteHost, State: it.State,
		Words: it.WordCount, Progress: int(math.Round(it.Progress * 100)), Tags: it.Tags,
	}
}

// toolErr turns a missing article into the tool's own message and passes
// anything else through.
func toolErr(err error) error {
	if errors.Is(err, ErrNotFound) {
		return app.ToolError{Msg: articleNotFound}
	}
	return err
}

func toolLimit(n int) int {
	switch {
	case n <= 0:
		return defaultListLimit
	case n > maxListLimit:
		return maxListLimit
	}
	return n
}

type stateCounts struct {
	Unread   int `json:"unread"`
	Reading  int `json:"reading"`
	Archived int `json:"archived"`
}

func (a *App) listTool(ctx context.Context, userID int64, args json.RawMessage) (string, error) {
	var in laterListArgs
	if err := app.ParseToolArgs(args, &in); err != nil {
		return "", err
	}
	state := State(in.State)
	switch state {
	case "":
		state = StateUnread
	case StateUnread, StateReading, StateArchived:
	default:
		return "", app.ToolError{Msg: "State must be unread, reading or archived."}
	}
	tag := strings.TrimSpace(in.Tag)

	items, err := a.store.List(ctx, userID, state, tag, 0, toolLimit(in.Limit))
	if err != nil {
		return "", err
	}
	counts, err := a.store.Counts(ctx, userID, tag)
	if err != nil {
		return "", err
	}
	tags, err := a.store.TagNames(ctx, userID)
	if err != nil {
		return "", err
	}
	if tags == nil {
		tags = []string{}
	}
	articles := make([]toolArticle, 0, len(items))
	for _, it := range items {
		articles = append(articles, toolEntry(it))
	}
	return app.MarshalTool(struct {
		Notice   string        `json:"notice"`
		Counts   stateCounts   `json:"counts"`
		Tags     []string      `json:"tags"`
		Articles []toolArticle `json:"articles"`
	}{
		app.UntrustedNotice,
		stateCounts{counts[StateUnread], counts[StateReading], counts[StateArchived]},
		tags, articles,
	})
}

func (a *App) searchTool(ctx context.Context, userID int64, args json.RawMessage) (string, error) {
	var in laterSearchArgs
	if err := app.ParseToolArgs(args, &in); err != nil {
		return "", err
	}
	query := strings.TrimSpace(in.Query)
	if query == "" {
		return "", app.ToolError{Msg: "Give some words to search for."}
	}
	hits, err := a.store.Search(ctx, userID, query, strings.TrimSpace(in.Tag), 0, toolLimit(in.Limit))
	if err != nil {
		return "", err
	}
	type hitEntry struct {
		toolArticle
		Snippet string `json:"snippet,omitempty"`
	}
	strip := strings.NewReplacer(SnippetOpen, "", SnippetClose, "")
	out := make([]hitEntry, 0, len(hits))
	for _, h := range hits {
		out = append(out, hitEntry{toolEntry(h.ListItem), strip.Replace(h.Snippet)})
	}
	return app.MarshalTool(struct {
		Notice   string     `json:"notice"`
		Articles []hitEntry `json:"articles"`
	}{app.UntrustedNotice, out})
}

func (a *App) getArticleTool(ctx context.Context, userID int64, args json.RawMessage) (string, error) {
	var in laterGetArgs
	if err := app.ParseToolArgs(args, &in); err != nil {
		return "", err
	}
	art, err := a.store.Article(ctx, userID, in.ID)
	if err != nil {
		return "", toolErr(err)
	}
	tags, err := a.store.ArticleTags(ctx, userID, in.ID)
	if err != nil {
		return "", err
	}
	tagText := "none"
	if len(tags) > 0 {
		tagText = strings.Join(tags, ", ")
	}

	// Only our own facts and the user's own note stay outside the block: the
	// link and the title come from the publisher.
	out := "Article " + strconv.FormatInt(art.ID, 10) + ", saved " + art.SavedAt.Format("2006-01-02") +
		", state " + string(art.State) + ", " + strconv.Itoa(art.WordCount) + " words. Tags: " + tagText + "."
	if art.Note != "" {
		out += "\nYour note: " + art.Note
	}
	source := art.SiteHost
	if source == "" {
		source = "unknown"
	}
	facts := "Link: " + art.URL + "\n\n" + art.Title

	if art.Content == ContentLinkOnly {
		out += "\n" + app.WrapUntrusted(source, facts) + "\nOnly the link was saved, so there's no text to read."
		if art.ExtractError != "" {
			out += " (" + art.ExtractError + ")"
		}
		return out, nil
	}
	return out + "\n" + app.WrapUntrusted(source, facts+"\n\n"+art.ContentText), nil
}
