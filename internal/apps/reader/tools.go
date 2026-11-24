package reader

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
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
const articleNotFound = "No article with that ID in your feeds"

// Tools returns ON Reader's MCP tools. They read a.store when called, so they
// work on whatever Mount set up; every call is scoped to the token's user,
// never to an argument.
func (a *App) Tools(deps app.Deps) []app.Tool {
	return []app.Tool{
		{
			Name:  "reader_list",
			Title: "List feed articles",
			Description: "List articles from the user's feeds: unread ones by default, or starred ones with filter=starred. " +
				"Use it to answer 'what's new?' or to show saved articles. Give feed (a subscription id) to look at one feed. " +
				"The result also lists the user's feeds with their ids and unread counts; use those ids for the feed argument. " +
				"Each article's id is what to pass to reader_get_article, reader_mark_read and reader_star.",
			Scope: "reader:read", ReadOnly: true,
			Input: listToolArgs{},
			Call:  a.listTool,
		},
		{
			Name:  "reader_search",
			Title: "Search feed articles",
			Description: "Search the articles in the user's feeds by words in the title or text, read or unread. " +
				"Use it to find an article when you don't know where it is; each result's id is what to pass to reader_get_article.",
			Scope: "reader:read", ReadOnly: true,
			Input: readerSearchToolArgs{},
			Call:  a.searchTool,
		},
		{
			Name:  "reader_get_article",
			Title: "Read a feed article",
			Description: "Read one article's text, to read it or summarise it for the user. It doesn't mark the article read. " +
				"Get the id from reader_list or reader_search.",
			Scope: "reader:read", ReadOnly: true,
			Input: getArticleToolArgs{},
			Call:  a.getArticleTool,
		},
		{
			Name:        "reader_mark_read",
			Title:       "Mark an article read",
			Description: "Mark an article read, or unread with read=false.",
			Scope:       "reader:write", Idempotent: true,
			Input: markReadToolArgs{},
			Call:  a.markReadTool,
		},
		{
			Name:        "reader_star",
			Title:       "Star an article",
			Description: "Star an article to save it, or remove the star with starred=false.",
			Scope:       "reader:write", Idempotent: true,
			Input: starToolArgs{},
			Call:  a.starTool,
		},
	}
}

// Optional arguments carry omitempty so the generated schema does not list
// them as required. The two booleans are pointers so a missing one can be told
// from false.
type listToolArgs struct {
	Filter string `json:"filter,omitempty" jsonschema:"unread (the default) or starred"`
	Feed   int64  `json:"feed,omitempty" jsonschema:"a subscription id from this tool's feeds, to list just that feed"`
	Limit  int    `json:"limit,omitempty" jsonschema:"most articles to return (default 20, at most 100)"`
}

type readerSearchToolArgs struct {
	Query string `json:"query" jsonschema:"words to search for in article titles and text"`
	Limit int    `json:"limit,omitempty" jsonschema:"most articles to return (default 20, at most 100)"`
}

type getArticleToolArgs struct {
	ID int64 `json:"id" jsonschema:"the article's id, from reader_list or reader_search"`
}

type markReadToolArgs struct {
	ID   int64 `json:"id" jsonschema:"the article's id"`
	Read *bool `json:"read" jsonschema:"true to mark it read, false to mark it unread"`
}

type starToolArgs struct {
	ID      int64 `json:"id" jsonschema:"the article's id"`
	Starred *bool `json:"starred" jsonschema:"true to star it, false to remove the star"`
}

type toolArticle struct {
	ID        int64  `json:"id"`
	Title     string `json:"title"`
	Feed      string `json:"feed"`
	Published string `json:"published,omitempty"`
	URL       string `json:"url"`
	Read      bool   `json:"read"`
	Starred   bool   `json:"starred"`
}

func readerToolErr(err error) error {
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

// feedNames maps each shared feed id to what the sidebar calls the user's
// subscription to it, and lists the subscriptions in sidebar order with
// folders flattened.
func (a *App) feedNames(ctx context.Context, userID int64) (map[int64]string, []Subscription, error) {
	tree, err := a.store.Tree(ctx, userID)
	if err != nil {
		return nil, nil, err
	}
	var subs []Subscription
	for _, f := range tree.Folders {
		subs = append(subs, f.Subs...)
	}
	subs = append(subs, tree.Root...)
	names := make(map[int64]string, len(subs))
	for _, s := range subs {
		names[s.FeedID] = s.DisplayName()
	}
	return names, subs, nil
}

func toolArticles(items []Item, names map[int64]string) []toolArticle {
	out := make([]toolArticle, 0, len(items))
	for _, it := range items {
		feed, ok := names[it.FeedID]
		if !ok {
			feed = it.FeedName
		}
		ta := toolArticle{ID: it.ID, Title: it.Title, Feed: feed, URL: it.URL, Read: it.Read, Starred: it.Starred}
		if !it.PublishedAt.IsZero() {
			ta.Published = it.PublishedAt.Format("2006-01-02")
		}
		out = append(out, ta)
	}
	return out
}

func (a *App) listTool(ctx context.Context, userID int64, args json.RawMessage) (string, error) {
	var in listToolArgs
	if err := app.ParseToolArgs(args, &in); err != nil {
		return "", err
	}
	if in.Filter != "" && in.Filter != string(FilterUnread) && in.Filter != string(FilterStarred) {
		return "", app.ToolError{Msg: "Filter must be unread or starred."}
	}

	scope, filter, subID := ScopeAll, FilterUnread, int64(0)
	switch {
	case in.Feed != 0:
		if err := a.store.ownsSubscription(ctx, userID, in.Feed); err != nil {
			if errors.Is(err, ErrNotFound) {
				return "", app.ToolError{Msg: "No feed with that ID in your subscriptions."}
			}
			return "", err
		}
		scope, subID = ScopeFeed, in.Feed
		if in.Filter == string(FilterStarred) {
			filter = FilterStarred
		}
	case in.Filter == string(FilterStarred):
		scope, filter = ScopeStarred, FilterAll
	}

	items, err := a.store.ItemsForScope(ctx, userID, scope, subID, filter, "", toolLimit(in.Limit))
	if err != nil {
		return "", err
	}
	names, subs, err := a.feedNames(ctx, userID)
	if err != nil {
		return "", err
	}
	counts, err := a.store.UnreadCounts(ctx, userID)
	if err != nil {
		return "", err
	}
	type feedEntry struct {
		ID     int64  `json:"id"`
		Name   string `json:"name"`
		Unread int    `json:"unread"`
	}
	feeds := make([]feedEntry, 0, len(subs))
	for _, s := range subs {
		feeds = append(feeds, feedEntry{ID: s.ID, Name: s.DisplayName(), Unread: counts.BySub[s.ID]})
	}
	return app.MarshalTool(struct {
		Notice   string        `json:"notice"`
		Feeds    []feedEntry   `json:"feeds"`
		Articles []toolArticle `json:"articles"`
	}{app.UntrustedNotice, feeds, toolArticles(items, names)})
}

func (a *App) searchTool(ctx context.Context, userID int64, args json.RawMessage) (string, error) {
	var in readerSearchToolArgs
	if err := app.ParseToolArgs(args, &in); err != nil {
		return "", err
	}
	query := strings.TrimSpace(in.Query)
	if query == "" {
		return "", app.ToolError{Msg: "Give some words to search for."}
	}
	items, err := a.store.ItemsForScope(ctx, userID, ScopeAll, 0, FilterAll, query, toolLimit(in.Limit))
	if err != nil {
		return "", err
	}
	names, _, err := a.feedNames(ctx, userID)
	if err != nil {
		return "", err
	}
	return app.MarshalTool(struct {
		Notice   string        `json:"notice"`
		Articles []toolArticle `json:"articles"`
	}{app.UntrustedNotice, toolArticles(items, names)})
}

// sourceHost is the article's site as the model sees it in the wrapper tag:
// the host of its link without "www.", or "unknown" when the link has no
// usable host. It is never the feed's name: that is publisher text too.
func sourceHost(link string) string {
	if u, err := url.Parse(link); err == nil && u.Hostname() != "" {
		return strings.TrimPrefix(u.Hostname(), "www.")
	}
	return "unknown"
}

func yesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}

func (a *App) getArticleTool(ctx context.Context, userID int64, args json.RawMessage) (string, error) {
	var in getArticleToolArgs
	if err := app.ParseToolArgs(args, &in); err != nil {
		return "", err
	}
	it, err := a.store.Item(ctx, userID, in.ID)
	if err != nil {
		return "", readerToolErr(err)
	}
	names, _, err := a.feedNames(ctx, userID)
	if err != nil {
		return "", err
	}
	feed, ok := names[it.FeedID]
	if !ok {
		feed = it.FeedName
	}

	// Only our own facts stay outside the block: the feed's name, the link
	// and the title all come from the publisher.
	header := "Article " + strconv.FormatInt(it.ID, 10)
	if !it.PublishedAt.IsZero() {
		header += ", published " + it.PublishedAt.Format("2006-01-02")
	}
	header += ". Read: " + yesNo(it.Read) + ". Starred: " + yesNo(it.Starred) + "."
	facts := "Feed: " + feed + "\nLink: " + it.URL + "\n\n" + it.Title

	body := it.FullHTML
	if body == "" {
		body = it.Body()
	}
	text := strings.TrimSpace(SearchText(body))
	if text == "" {
		return header + "\n" + app.WrapUntrusted(sourceHost(it.URL), facts) +
			"\nThere's no article text in the feed. Open the link to read it.", nil
	}
	return header + "\n" + app.WrapUntrusted(sourceHost(it.URL), facts+"\n\n"+text), nil
}

func (a *App) markReadTool(ctx context.Context, userID int64, args json.RawMessage) (string, error) {
	var in markReadToolArgs
	if err := app.ParseToolArgs(args, &in); err != nil {
		return "", err
	}
	if in.Read == nil {
		return "", app.ToolError{Msg: "Say whether it's read, true or false."}
	}
	if err := a.store.SetRead(ctx, userID, in.ID, *in.Read, a.store.now()); err != nil {
		return "", readerToolErr(err)
	}
	return app.MarshalTool(struct {
		ID   int64 `json:"id"`
		Read bool  `json:"read"`
	}{in.ID, *in.Read})
}

func (a *App) starTool(ctx context.Context, userID int64, args json.RawMessage) (string, error) {
	var in starToolArgs
	if err := app.ParseToolArgs(args, &in); err != nil {
		return "", err
	}
	if in.Starred == nil {
		return "", app.ToolError{Msg: "Say whether it's starred, true or false."}
	}
	if err := a.store.SetStarred(ctx, userID, in.ID, *in.Starred, a.store.now()); err != nil {
		return "", readerToolErr(err)
	}
	return app.MarshalTool(struct {
		ID      int64 `json:"id"`
		Starred bool  `json:"starred"`
	}{in.ID, *in.Starred})
}
