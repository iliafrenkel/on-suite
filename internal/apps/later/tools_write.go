package later

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"

	"github.com/iliafrenkel/on-suite/internal/platform/app"
)

func writeTools(a *App) []app.Tool {
	return []app.Tool{
		{
			Name:  "later_save",
			Title: "Save a web page to read later",
			Description: "Save a web page to read later. It fetches the page now, which can take up to 15 seconds. " +
				"Add tags to file it. If the page is already saved, its tags are added.",
			Scope: "later:write",
			Input: laterSaveArgs{},
			Call:  a.saveTool,
		},
		{
			Name:        "later_set_state",
			Title:       "Archive or restore an article",
			Description: "Archive an article, or put it back in unread.",
			Scope:       "later:write", Idempotent: true,
			Input: laterSetStateArgs{},
			Call:  a.setStateTool,
		},
		{
			Name:  "later_tag",
			Title: "Tag an article",
			Description: "Add or remove tags on an article. " +
				"Use the tags later_list shows rather than inventing near-duplicates.",
			Scope: "later:write", Idempotent: true,
			Input: laterTagArgs{},
			Call:  a.tagTool,
		},
	}
}

type laterSaveArgs struct {
	URL  string   `json:"url" jsonschema:"the web address to save, starting with http:// or https://"`
	Tags []string `json:"tags,omitempty" jsonschema:"tags to file it under"`
}

type laterSetStateArgs struct {
	ID    int64  `json:"id" jsonschema:"the article's id, from later_list or later_search"`
	State string `json:"state" jsonschema:"unread or archived"`
}

type laterTagArgs struct {
	ID     int64    `json:"id" jsonschema:"the article's id, from later_list or later_search"`
	Add    []string `json:"add,omitempty" jsonschema:"tags to add"`
	Remove []string `json:"remove,omitempty" jsonschema:"tags to remove"`
}

func (a *App) saveTool(ctx context.Context, userID int64, args json.RawMessage) (string, error) {
	var in laterSaveArgs
	if err := app.ParseToolArgs(args, &in); err != nil {
		return "", err
	}
	pageURL, err := NormalizeURL(in.URL)
	if err != nil {
		return "", app.ToolError{Msg: badURLMessage}
	}

	var (
		art     Article
		existed bool
	)
	switch existing, err := a.store.ArticleByURL(ctx, userID, pageURL); {
	case err == nil:
		// Already saved: nothing is fetched again, the given tags are added.
		art, existed = existing, true
		if len(in.Tags) > 0 {
			have, err := a.store.ArticleTags(ctx, userID, art.ID)
			if err != nil {
				return "", err
			}
			if err := a.store.SetTags(ctx, userID, art.ID, append(have, in.Tags...)); err != nil {
				return "", toolErr(err)
			}
		}
	case errors.Is(err, ErrNotFound):
		n := a.fetchArticle(ctx, pageURL)
		n.Tags = ParseTags(strings.Join(in.Tags, ","))
		art, _, err = a.store.Save(ctx, userID, n)
		if err != nil {
			return "", err
		}
	default:
		return "", err
	}

	tags, err := a.store.ArticleTags(ctx, userID, art.ID)
	if err != nil {
		return "", err
	}
	if tags == nil {
		tags = []string{}
	}
	// The reason is one of our own fixed strings, so it is safe to show.
	linkOnly := art.Content == ContentLinkOnly
	reason := ""
	if linkOnly {
		reason = art.ExtractError
	}
	// The title is the publisher's text; the notice comes first in the JSON.
	return app.MarshalTool(struct {
		Notice       string   `json:"notice"`
		ID           int64    `json:"id"`
		Title        string   `json:"title"`
		Site         string   `json:"site"`
		State        State    `json:"state"`
		Tags         []string `json:"tags"`
		AlreadySaved bool     `json:"already_saved,omitempty"`
		LinkOnly     bool     `json:"link_only,omitempty"`
		Reason       string   `json:"reason,omitempty"`
	}{
		app.UntrustedNotice, art.ID, art.Title, art.SiteHost, art.State, tags,
		existed, linkOnly, reason,
	})
}

func (a *App) setStateTool(ctx context.Context, userID int64, args json.RawMessage) (string, error) {
	var in laterSetStateArgs
	if err := app.ParseToolArgs(args, &in); err != nil {
		return "", err
	}
	state := State(in.State)
	switch state {
	case StateUnread, StateArchived:
	case StateReading:
		return "", app.ToolError{Msg: "An article becomes reading when it's opened. Use unread or archived."}
	default:
		return "", app.ToolError{Msg: "State must be unread or archived."}
	}
	if err := a.store.SetState(ctx, userID, in.ID, state); err != nil {
		return "", toolErr(err)
	}
	return app.MarshalTool(struct {
		ID    int64 `json:"id"`
		State State `json:"state"`
	}{in.ID, state})
}

// sameSet reports whether a and b hold the same tags, ignoring order.
func sameSet(a, b []string) bool {
	return len(a) == len(b) && !slices.ContainsFunc(a, func(t string) bool { return !slices.Contains(b, t) })
}

func (a *App) tagTool(ctx context.Context, userID int64, args json.RawMessage) (string, error) {
	var in laterTagArgs
	if err := app.ParseToolArgs(args, &in); err != nil {
		return "", err
	}
	add := ParseTags(strings.Join(in.Add, ","))
	remove := ParseTags(strings.Join(in.Remove, ","))
	if len(add) == 0 && len(remove) == 0 {
		return "", app.ToolError{Msg: "Give tags to add or remove."}
	}
	// ArticleTags says nothing for a missing article, so check it exists.
	if _, err := a.store.Article(ctx, userID, in.ID); err != nil {
		return "", toolErr(err)
	}
	have, err := a.store.ArticleTags(ctx, userID, in.ID)
	if err != nil {
		return "", err
	}
	want := slices.Clone(have)
	for _, t := range add {
		if !slices.Contains(want, t) {
			want = append(want, t)
		}
	}
	want = slices.DeleteFunc(want, func(t string) bool { return slices.Contains(remove, t) })
	// Existing tags keep their place ahead of the new ones, so the cap
	// never drops one of them for an added tag.
	if !sameSet(want, have) {
		if err := a.store.SetTags(ctx, userID, in.ID, want); err != nil {
			return "", toolErr(err)
		}
	}
	tags, err := a.store.ArticleTags(ctx, userID, in.ID)
	if err != nil {
		return "", err
	}
	if tags == nil {
		tags = []string{}
	}
	return app.MarshalTool(struct {
		ID   int64    `json:"id"`
		Tags []string `json:"tags"`
	}{in.ID, tags})
}
