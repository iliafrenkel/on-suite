package reader_test

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/iliafrenkel/on-suite/internal/apps/reader"
	"github.com/iliafrenkel/on-suite/internal/apptest"
	"github.com/iliafrenkel/on-suite/internal/platform/app"
	"github.com/iliafrenkel/on-suite/internal/platform/article"
)

const readerNotFoundMsg = "No article with that ID in your feeds"

type toolFixture struct {
	s     *apptest.Server[*reader.Store]
	tools map[string]app.Tool
}

func newToolFixture(t *testing.T) *toolFixture {
	t.Helper()
	a := reader.New()
	tf := &toolFixture{
		s:     apptest.NewServer(t, a, reader.NewStore),
		tools: map[string]app.Tool{},
	}
	for _, tool := range a.Tools(app.Deps{}) {
		tf.tools[tool.Name] = tool
	}
	return tf
}

func (tf *toolFixture) call(t *testing.T, name string, userID int64, args string) (string, error) {
	t.Helper()
	tool, ok := tf.tools[name]
	if !ok {
		t.Fatalf("no tool %q", name)
	}
	return tool.Call(context.Background(), userID, json.RawMessage(args))
}

func (tf *toolFixture) mustCall(t *testing.T, name string, userID int64, args string) string {
	t.Helper()
	out, err := tf.call(t, name, userID, args)
	if err != nil {
		t.Fatalf("%s %s: %v", name, args, err)
	}
	return out
}

func (tf *toolFixture) wantToolErr(t *testing.T, name string, userID int64, args, msg string) {
	t.Helper()
	_, err := tf.call(t, name, userID, args)
	var te app.ToolError
	if !errors.As(err, &te) || te.Msg != msg {
		t.Errorf("%s %s: err = %v, want ToolError %q", name, args, err, msg)
	}
}

// subscribe makes userID follow a feed and stores items for it, returning the
// subscription id and the item ids in the order given.
func (tf *toolFixture) subscribe(t *testing.T, userID int64, feedURL string, items ...reader.ParsedItem) (int64, []int64) {
	t.Helper()
	ctx := context.Background()
	sub, err := tf.s.Store.Subscribe(ctx, userID, feedURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	fetched := time.Now().UTC().Add(time.Hour)
	if _, err := tf.s.Store.SaveItems(ctx, sub.FeedID, items, fetched); err != nil {
		t.Fatal(err)
	}
	listed, err := tf.s.Store.ItemsForScope(ctx, userID, reader.ScopeFeed, sub.ID, reader.FilterAll, "", 100)
	if err != nil {
		t.Fatal(err)
	}
	byGUID := map[string]int64{}
	for _, it := range listed {
		byGUID[it.GUID] = it.ID
	}
	ids := make([]int64, len(items))
	for i, it := range items {
		ids[i] = byGUID[it.GUID]
	}
	return sub.ID, ids
}

func pi(guid, title, html string, day int) reader.ParsedItem {
	return reader.ParsedItem{
		GUID: guid, URL: "https://www.example.com/" + guid, Title: title,
		ContentHTML: html, PublishedAt: time.Date(2026, 11, day, 9, 0, 0, 0, time.UTC),
	}
}

func i64(n int64) string { return strconv.FormatInt(n, 10) }

type listedArticle struct {
	ID        int64  `json:"id"`
	Title     string `json:"title"`
	Feed      string `json:"feed"`
	Published string `json:"published"`
	URL       string `json:"url"`
	Read      bool   `json:"read"`
	Starred   bool   `json:"starred"`
}

type listResult struct {
	Notice string `json:"notice"`
	Feeds  []struct {
		ID     int64  `json:"id"`
		Name   string `json:"name"`
		Unread int    `json:"unread"`
	} `json:"feeds"`
	Articles []listedArticle `json:"articles"`
}

func (tf *toolFixture) list(t *testing.T, userID int64, args string) listResult {
	t.Helper()
	var r listResult
	if err := json.Unmarshal([]byte(tf.mustCall(t, "reader_list", userID, args)), &r); err != nil {
		t.Fatal(err)
	}
	return r
}

func TestReaderToolsDeclared(t *testing.T) {
	tf := newToolFixture(t)
	if len(tf.tools) != 5 {
		t.Fatalf("%d tools, want 5", len(tf.tools))
	}
	for _, name := range []string{"reader_list", "reader_search", "reader_get_article"} {
		tool := tf.tools[name]
		if tool.Scope != "reader:read" || !tool.ReadOnly || tool.Input == nil || tool.Description == "" {
			t.Errorf("%s = %+v, want read-only reader:read with input and description", name, tool)
		}
	}
	for _, name := range []string{"reader_mark_read", "reader_star"} {
		tool := tf.tools[name]
		if tool.Scope != "reader:write" || tool.ReadOnly || !tool.Idempotent || tool.Input == nil || tool.Description == "" {
			t.Errorf("%s = %+v, want idempotent reader:write with input and description", name, tool)
		}
	}
}

func TestReaderListUnreadStarredAndFeed(t *testing.T) {
	tf := newToolFixture(t)
	alice := tf.s.Alice.User.ID
	ctx := context.Background()
	subA, a := tf.subscribe(t, alice, "https://one.example.com/feed.xml",
		pi("a1", "Alpha", "<p>a</p>", 1), pi("a2", "Beta", "<p>b</p>", 2), pi("a3", "Gamma", "<p>c</p>", 3))
	subB, _ := tf.subscribe(t, alice, "https://two.example.com/feed.xml", pi("b1", "Delta", "<p>d</p>", 4))
	if err := tf.s.Store.RenameSubscription(ctx, alice, subB, "My Second Feed"); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	if err := tf.s.Store.SetRead(ctx, alice, a[0], true, now); err != nil {
		t.Fatal(err)
	}
	if err := tf.s.Store.SetStarred(ctx, alice, a[1], true, now); err != nil {
		t.Fatal(err)
	}

	r := tf.list(t, alice, `{}`)
	if r.Notice != app.UntrustedNotice {
		t.Errorf("notice = %q", r.Notice)
	}
	if len(r.Articles) != 3 { // Beta, Gamma, Delta
		t.Errorf("default list has %d articles, want 3 unread", len(r.Articles))
	}
	counts := map[int64]int{}
	names := map[int64]string{}
	for _, f := range r.Feeds {
		counts[f.ID], names[f.ID] = f.Unread, f.Name
	}
	if len(r.Feeds) != 2 || counts[subA] != 2 || counts[subB] != 1 || names[subB] != "My Second Feed" {
		t.Errorf("feeds = %+v", r.Feeds)
	}

	r = tf.list(t, alice, `{"filter":"starred"}`)
	if len(r.Articles) != 1 || r.Articles[0].Title != "Beta" || !r.Articles[0].Starred || r.Articles[0].Published != "2026-11-02" {
		t.Fatalf("starred = %+v", r.Articles)
	}
	if r.Articles[0].URL != "https://www.example.com/a2" {
		t.Errorf("url = %q", r.Articles[0].URL)
	}

	r = tf.list(t, alice, `{"feed":`+i64(subA)+`}`)
	if len(r.Articles) != 2 {
		t.Errorf("feed list has %d articles, want 2 unread", len(r.Articles))
	}
	r = tf.list(t, alice, `{"filter":"starred","feed":`+i64(subB)+`}`)
	if len(r.Articles) != 0 {
		t.Errorf("starred in feed B = %+v, want none", r.Articles)
	}
	r = tf.list(t, alice, `{"limit":1}`)
	if len(r.Articles) != 1 {
		t.Errorf("limit 1 gave %d", len(r.Articles))
	}
}

func TestReaderListBadFeedAndFilter(t *testing.T) {
	tf := newToolFixture(t)
	alice, bob := tf.s.Alice.User.ID, tf.s.Bob.User.ID
	subBob, _ := tf.subscribe(t, bob, "https://bob.example.com/feed.xml", pi("x", "Bob's", "<p>x</p>", 1))
	tf.wantToolErr(t, "reader_list", alice, `{"feed":`+i64(subBob)+`}`, "No feed with that ID in your subscriptions.")
	tf.wantToolErr(t, "reader_list", alice, `{"feed":999999}`, "No feed with that ID in your subscriptions.")
	tf.wantToolErr(t, "reader_list", alice, `{"filter":"old"}`, "Filter must be unread or starred.")
}

func TestReaderSearch(t *testing.T) {
	tf := newToolFixture(t)
	alice, bob := tf.s.Alice.User.ID, tf.s.Bob.User.ID
	tf.subscribe(t, alice, "https://one.example.com/feed.xml",
		pi("a1", "Gardening", "<p>tomatoes and basil</p>", 1), pi("a2", "Cars", "<p>engines</p>", 2))
	tf.subscribe(t, bob, "https://bob.example.com/feed.xml", pi("b1", "Bob's", "<p>tomatoes too</p>", 1))

	var r struct {
		Notice   string          `json:"notice"`
		Articles []listedArticle `json:"articles"`
	}
	if err := json.Unmarshal([]byte(tf.mustCall(t, "reader_search", alice, `{"query":"tomatoes"}`)), &r); err != nil {
		t.Fatal(err)
	}
	if r.Notice != app.UntrustedNotice || len(r.Articles) != 1 || r.Articles[0].Title != "Gardening" {
		t.Errorf("search = %+v", r)
	}
	tf.wantToolErr(t, "reader_search", alice, `{"query":""}`, "Give some words to search for.")
	tf.wantToolErr(t, "reader_search", alice, `{}`, "Give some words to search for.")
}

func TestReaderGetArticleWrapsText(t *testing.T) {
	tf := newToolFixture(t)
	alice := tf.s.Alice.User.ID
	_, ids := tf.subscribe(t, alice, "https://one.example.com/feed.xml", pi("a1", "Greeting", "<p>Hello</p><p>world</p>", 5))

	got := tf.mustCall(t, "reader_get_article", alice, `{"id":`+i64(ids[0])+`}`)
	header, rest, _ := strings.Cut(got, "\n")
	wantHeader := "Article " + i64(ids[0]) + " from https://one.example.com/feed.xml, published 2026-11-05. Read: no. Starred: no. Link: https://www.example.com/a1"
	if header != wantHeader {
		t.Errorf("header = %q\nwant %q", header, wantHeader)
	}
	if !strings.HasPrefix(rest, app.UntrustedNotice) || !strings.Contains(rest, `source="example.com"`) ||
		!strings.Contains(rest, "Greeting") || !strings.Contains(rest, "Hello") || !strings.Contains(rest, "world") {
		t.Errorf("body = %q", rest)
	}
	read, _, err := tf.s.Store.ItemState(context.Background(), alice, ids[0])
	if err != nil || read {
		t.Errorf("read = %v, err = %v; getting an article must not mark it read", read, err)
	}
}

func TestReaderGetArticlePrefersFullThenSummary(t *testing.T) {
	tf := newToolFixture(t)
	alice := tf.s.Alice.User.ID
	withSummary := pi("s", "Sum", "", 1)
	withSummary.SummaryHTML = "<p>just the summary</p>"
	_, ids := tf.subscribe(t, alice, "https://one.example.com/feed.xml",
		pi("f", "Full", "<p>feed content</p>", 2), withSummary, pi("e", "Empty", "", 3))

	ex := article.Extracted{HTML: "<p>the full article</p>"}
	if err := tf.s.Store.SaveFullArticle(context.Background(), alice, ids[0], ex, time.Now()); err != nil {
		t.Fatal(err)
	}
	got := tf.mustCall(t, "reader_get_article", alice, `{"id":`+i64(ids[0])+`}`)
	if !strings.Contains(got, "the full article") || strings.Contains(got, "feed content") {
		t.Errorf("full = %q, want the fetched article to win", got)
	}
	got = tf.mustCall(t, "reader_get_article", alice, `{"id":`+i64(ids[1])+`}`)
	if !strings.Contains(got, "just the summary") {
		t.Errorf("summary = %q", got)
	}
	got = tf.mustCall(t, "reader_get_article", alice, `{"id":`+i64(ids[2])+`}`)
	if !strings.Contains(got, "There's no article text in the feed. Open the link to read it.") || strings.Contains(got, "<untrusted_article") {
		t.Errorf("empty = %q", got)
	}
}

func TestReaderGetArticleNotFound(t *testing.T) {
	tf := newToolFixture(t)
	alice, bob := tf.s.Alice.User.ID, tf.s.Bob.User.ID
	_, ids := tf.subscribe(t, bob, "https://bob.example.com/feed.xml", pi("b", "Bob's", "<p>x</p>", 1))
	for _, id := range []int64{ids[0], 999999} {
		tf.wantToolErr(t, "reader_get_article", alice, `{"id":`+i64(id)+`}`, readerNotFoundMsg)
	}
}

func TestReaderMarkReadAndStar(t *testing.T) {
	tf := newToolFixture(t)
	alice, bob := tf.s.Alice.User.ID, tf.s.Bob.User.ID
	ctx := context.Background()
	_, ids := tf.subscribe(t, alice, "https://one.example.com/feed.xml", pi("a", "Mine", "<p>x</p>", 1))
	_, bobIDs := tf.subscribe(t, bob, "https://bob.example.com/feed.xml", pi("b", "Bob's", "<p>x</p>", 1))
	id := i64(ids[0])

	state := func() (bool, bool) {
		it, err := tf.s.Store.Item(ctx, alice, ids[0])
		if err != nil {
			t.Fatal(err)
		}
		return it.Read, it.Starred
	}
	if got := tf.mustCall(t, "reader_mark_read", alice, `{"id":`+id+`,"read":true}`); got != `{"id":`+id+`,"read":true}` {
		t.Errorf("mark_read = %s", got)
	}
	tf.mustCall(t, "reader_mark_read", alice, `{"id":`+id+`,"read":true}`) // repeat is fine
	if r, _ := state(); !r {
		t.Error("not read")
	}
	tf.mustCall(t, "reader_mark_read", alice, `{"id":`+id+`,"read":false}`)
	if r, _ := state(); r {
		t.Error("still read")
	}
	if got := tf.mustCall(t, "reader_star", alice, `{"id":`+id+`,"starred":true}`); got != `{"id":`+id+`,"starred":true}` {
		t.Errorf("star = %s", got)
	}
	if _, s := state(); !s {
		t.Error("not starred")
	}
	tf.mustCall(t, "reader_star", alice, `{"id":`+id+`,"starred":false}`)
	if _, s := state(); s {
		t.Error("still starred")
	}

	tf.wantToolErr(t, "reader_mark_read", alice, `{"id":`+id+`}`, "Say whether it's read, true or false.")
	tf.wantToolErr(t, "reader_star", alice, `{"id":`+id+`}`, "Say whether it's starred, true or false.")

	b := i64(bobIDs[0])
	tf.wantToolErr(t, "reader_mark_read", alice, `{"id":`+b+`,"read":true}`, readerNotFoundMsg)
	tf.wantToolErr(t, "reader_star", alice, `{"id":`+b+`,"starred":true}`, readerNotFoundMsg)
	it, err := tf.s.Store.Item(ctx, bob, bobIDs[0])
	if err != nil || it.Read || it.Starred {
		t.Errorf("bob's item = %+v, %v; must be unchanged", it, err)
	}
}
