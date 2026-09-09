package reader_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/iliafrenkel/on-suite/internal/apps/reader"
	"github.com/iliafrenkel/on-suite/internal/apptest"
	"github.com/iliafrenkel/on-suite/internal/htmlassert"
)

// The harness already creates Alice and Bob and signs them in: Server.Alice is
// a *apptest.Session, not a user, and its user id is Alice.User.ID.
func newServer(t *testing.T) *apptest.Server[*reader.Store] {
	t.Helper()
	return apptest.NewServer(t, reader.New(), reader.NewStore)
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }

func TestEveryReaderRouteIsBehindAuth(t *testing.T) {
	s := newServer(t)
	anon := s.Anonymous(t)

	tests := []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/reader/"},
		{http.MethodGet, "/reader/feed/1"},
		{http.MethodGet, "/reader/item/1"},
		{http.MethodPost, "/reader/subscribe"},
		{http.MethodPost, "/reader/sub/1/delete"},
		{http.MethodPost, "/reader/folder"},
		{http.MethodPost, "/reader/folder/1/delete"},
		{http.MethodPost, "/reader/refresh"},
	}

	for _, tt := range tests {
		t.Run(tt.method+" "+tt.path, func(t *testing.T) {
			rec := s.Do(t, anon, httptest.NewRequest(tt.method, tt.path, nil))
			if rec.Code == http.StatusOK {
				t.Fatalf("anonymous %s %s returned 200; the router is default-deny", tt.method, tt.path)
			}
		})
	}
}

func TestIndexShowsAnEmptyState(t *testing.T) {
	s := newServer(t)

	doc := s.Get(t, s.Alice, "/reader/")
	if !strings.Contains(strings.ToLower(doc.Text()), "no feeds") {
		t.Errorf("empty state does not tell the user what to do:\n%s", doc.Text())
	}
}

func TestSubscribeAddsAFeedToTheTree(t *testing.T) {
	s := newServer(t)

	rec := s.PostHX(t, s.Alice, "/reader/subscribe", url.Values{
		"url": {"https://example.com/feed.xml"},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("subscribe returned %d: %s", rec.Code, rec.Body.String())
	}

	doc := s.Get(t, s.Alice, "/reader/")
	if !strings.Contains(doc.Text(), "example.com/feed.xml") {
		t.Errorf("new subscription is not in the tree:\n%s", doc.Text())
	}
}

func TestSubscribeRejectsANonHTTPURL(t *testing.T) {
	s := newServer(t)

	rec := s.PostHX(t, s.Alice, "/reader/subscribe", url.Values{
		"url": {"file:///etc/passwd"},
	})
	if !strings.Contains(rec.Body.String(), "not a feed address") {
		t.Errorf("no validation message shown for a file:// URL:\n%s", rec.Body.String())
	}

	var feeds int
	if err := s.Store.DB().QueryRowContext(context.Background(),
		`SELECT count(*) FROM reader_feeds`).Scan(&feeds); err != nil {
		t.Fatal(err)
	}
	if feeds != 0 {
		t.Errorf("a file:// URL created %d feed rows", feeds)
	}
}

func TestArticlePaneRendersSanitizedContent(t *testing.T) {
	s := newServer(t)
	ctx := context.Background()

	sub, err := s.Store.Subscribe(ctx, s.Alice.User.ID, "https://example.com/feed.xml", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Store.SaveItems(ctx, sub.FeedID, []reader.ParsedItem{{
		GUID:        "g1",
		Title:       "An article",
		URL:         "https://example.com/1",
		ContentHTML: reader.SanitizeHTML(`<p>Body.</p><script>alert(1)</script>`),
	}}, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	items, err := s.Store.ItemsForSubscription(ctx, s.Alice.User.ID, sub.ID, 10)
	if err != nil {
		t.Fatal(err)
	}

	rec := s.Do(t, s.Alice, httptest.NewRequest(http.MethodGet, "/reader/item/"+itoa(items[0].ID), nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("article returned %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "Body.") {
		t.Errorf("article body missing:\n%s", body)
	}
	if strings.Contains(body, "<script>alert(1)") {
		t.Error("unsanitized script reached the page")
	}
}

func TestArticleFromAnotherUsersFeedIs404(t *testing.T) {
	s := newServer(t)
	ctx := context.Background()

	sub, err := s.Store.Subscribe(ctx, s.Alice.User.ID, "https://example.com/feed.xml", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Store.SaveItems(ctx, sub.FeedID, []reader.ParsedItem{{
		GUID: "g1", Title: "Alice's", ContentHTML: "<p>x</p>",
	}}, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	items, err := s.Store.ItemsForSubscription(ctx, s.Alice.User.ID, sub.ID, 10)
	if err != nil {
		t.Fatal(err)
	}

	rec := s.Do(t, s.Bob, httptest.NewRequest(http.MethodGet, "/reader/item/"+itoa(items[0].ID), nil))
	if rec.Code != http.StatusNotFound {
		t.Errorf("Bob got %d for Alice's article; it must be 404, not 403 — a 403 confirms the id exists", rec.Code)
	}
}

// TestSelectingAFeedUpdatesTheShellCrumb pins the shell OOB swap: without it,
// selecting a feed over HTMX would replace the panes while leaving the page
// <title> and breadcrumb showing whatever was there before — the exact
// regression fixed for issue #205 (commit c96a03f) elsewhere in this
// codebase. See paste's "detail-with-list" block and notes' "outline-swap"
// block for the pattern this mirrors.
func TestSelectingAFeedUpdatesTheShellCrumb(t *testing.T) {
	s := newServer(t)
	ctx := context.Background()

	sub, err := s.Store.Subscribe(ctx, s.Alice.User.ID, "https://example.com/feed.xml", nil)
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/reader/feed/"+itoa(sub.ID), nil)
	req.Header.Set("HX-Request", "true")
	rec := s.Do(t, s.Alice, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /reader/feed/%d over HTMX = %d: %s", sub.ID, rec.Code, rec.Body.String())
	}

	body := rec.Body.String()
	if !strings.Contains(body, "<title>") {
		t.Errorf("fragment response carries no <title>:\n%s", body)
	}

	tail := htmlassert.Parse(t, body).MustHave("#shell-crumb-tail")
	if got, _ := htmlassert.Attr(tail, "hx-swap-oob"); got != "true" {
		t.Errorf("shell-crumb-tail hx-swap-oob = %q, want true", got)
	}
	if tailText := htmlassert.Text(tail); !strings.Contains(tailText, "example.com/feed.xml") {
		t.Errorf("shell-crumb-tail does not show the selected feed: %q", tailText)
	}
}

// TestUnsubscribeControlRemovesASubscription pins the missing UI control the
// whole-branch review flagged: unsubscribe/createFolder/deleteFolder already
// worked over HTTP but nothing in the template posted to them. This traces
// the button in the tree all the way to the subscription disappearing.
func TestUnsubscribeControlRemovesASubscription(t *testing.T) {
	s := newServer(t)
	ctx := context.Background()

	sub, err := s.Store.Subscribe(ctx, s.Alice.User.ID, "https://example.com/feed.xml", nil)
	if err != nil {
		t.Fatal(err)
	}

	doc := s.Get(t, s.Alice, "/reader/")
	btn := doc.MustHave("button.reader-sub-delete")
	if got, _ := htmlassert.Attr(btn, "hx-post"); got != "/reader/sub/"+itoa(sub.ID)+"/delete" {
		t.Errorf("delete button hx-post = %q", got)
	}
	if got, _ := htmlassert.Attr(btn, "hx-target"); got != "#reader-panes" {
		t.Errorf("delete button hx-target = %q, want #reader-panes", got)
	}
	if _, ok := htmlassert.Attr(btn, "hx-confirm"); !ok {
		t.Error("unsubscribe is destructive but the button asks for no confirmation")
	}

	rec := s.PostHX(t, s.Alice, "/reader/sub/"+itoa(sub.ID)+"/delete", url.Values{})
	if rec.Code != http.StatusOK {
		t.Fatalf("unsubscribe returned %d: %s", rec.Code, rec.Body.String())
	}

	doc = s.Get(t, s.Alice, "/reader/")
	if strings.Contains(doc.Text(), "example.com/feed.xml") {
		t.Errorf("subscription still in the tree after unsubscribe:\n%s", doc.Text())
	}
}

// TestFolderCreateFormAddsAFolder pins the missing add-folder control.
func TestFolderCreateFormAddsAFolder(t *testing.T) {
	s := newServer(t)

	doc := s.Get(t, s.Alice, "/reader/")
	form := doc.MustHave("form.reader-add-folder")
	if got, _ := htmlassert.Attr(form, "hx-post"); got != "/reader/folder" {
		t.Errorf("folder form hx-post = %q, want /reader/folder", got)
	}
	doc.MustHave(`form.reader-add-folder input[name=name]`)

	rec := s.PostHX(t, s.Alice, "/reader/folder", url.Values{"name": {"Tech"}})
	if rec.Code != http.StatusOK {
		t.Fatalf("create folder returned %d: %s", rec.Code, rec.Body.String())
	}

	doc = s.Get(t, s.Alice, "/reader/")
	if !strings.Contains(doc.Text(), "Tech") {
		t.Errorf("new folder not in the tree:\n%s", doc.Text())
	}
}

// TestFolderDeleteControlRemovesAFolder pins the missing delete-folder
// control.
func TestFolderDeleteControlRemovesAFolder(t *testing.T) {
	s := newServer(t)
	ctx := context.Background()

	folder, err := s.Store.CreateFolder(ctx, s.Alice.User.ID, "Tech")
	if err != nil {
		t.Fatal(err)
	}

	doc := s.Get(t, s.Alice, "/reader/")
	btn := doc.MustHave("button.reader-folder-delete")
	if got, _ := htmlassert.Attr(btn, "hx-post"); got != "/reader/folder/"+itoa(folder.ID)+"/delete" {
		t.Errorf("folder delete button hx-post = %q", got)
	}
	if _, ok := htmlassert.Attr(btn, "hx-confirm"); !ok {
		t.Error("folder delete is destructive but the button asks for no confirmation")
	}

	rec := s.PostHX(t, s.Alice, "/reader/folder/"+itoa(folder.ID)+"/delete", url.Values{})
	if rec.Code != http.StatusOK {
		t.Fatalf("delete folder returned %d: %s", rec.Code, rec.Body.String())
	}

	doc = s.Get(t, s.Alice, "/reader/")
	if strings.Contains(doc.Text(), "Tech") {
		t.Errorf("folder still in the tree after delete:\n%s", doc.Text())
	}
}

// TestAddFeedFormHasFolderPicker pins folderParam's reachability from the UI:
// the handler already accepted folder_id, but nothing rendered a way to send
// one.
func TestAddFeedFormHasFolderPicker(t *testing.T) {
	s := newServer(t)
	ctx := context.Background()

	folder, err := s.Store.CreateFolder(ctx, s.Alice.User.ID, "Tech")
	if err != nil {
		t.Fatal(err)
	}

	doc := s.Get(t, s.Alice, "/reader/")
	doc.MustHave("select[name=folder_id]")
	opt := doc.MustHave(`select[name=folder_id] option[value="` + itoa(folder.ID) + `"]`)
	if got := htmlassert.Text(opt); got != "Tech" {
		t.Errorf("folder option text = %q, want Tech", got)
	}

	rec := s.PostHX(t, s.Alice, "/reader/subscribe", url.Values{
		"url":       {"https://example.com/feed.xml"},
		"folder_id": {itoa(folder.ID)},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("subscribe returned %d: %s", rec.Code, rec.Body.String())
	}

	tree, err := s.Store.Tree(ctx, s.Alice.User.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(tree.Folders) != 1 || len(tree.Folders[0].Subs) != 1 {
		t.Fatalf("subscription was not filed under the chosen folder: %+v", tree)
	}
}

// TestFailingSubscriptionShowsAMarker pins DoD item 2: a persistently-failing
// feed (which the poller already backs off correctly, per poll_test.go) must
// be visible in the tree, not silently invisible.
func TestFailingSubscriptionShowsAMarker(t *testing.T) {
	s := newServer(t)
	ctx := context.Background()

	sub, err := s.Store.Subscribe(ctx, s.Alice.User.ID, "https://example.com/feed.xml", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Store.DB().ExecContext(ctx,
		`UPDATE reader_feeds SET error_count = 3, last_error = 'connection refused' WHERE id = ?`,
		sub.FeedID); err != nil {
		t.Fatal(err)
	}

	doc := s.Get(t, s.Alice, "/reader/")
	marker := doc.MustHave("span.reader-failing")
	if got, _ := htmlassert.Attr(marker, "title"); got != "connection refused" {
		t.Errorf("failure marker title = %q, want the last error", got)
	}
}

// TestHealthySubscriptionShowsNoMarker is the negative case for the above: a
// feed that has never failed must not render the marker.
func TestHealthySubscriptionShowsNoMarker(t *testing.T) {
	s := newServer(t)
	ctx := context.Background()

	if _, err := s.Store.Subscribe(ctx, s.Alice.User.ID, "https://example.com/feed.xml", nil); err != nil {
		t.Fatal(err)
	}

	doc := s.Get(t, s.Alice, "/reader/")
	doc.MustNotHave("span.reader-failing")
}

// TestSubscribePollAndRenderComposedFlow is the seam no other test exercises:
// a real HTTP subscribe, a real poll against a fake feed server, then a real
// HTTP render of what the poll fetched — all through the same store the HTTP
// handlers themselves use.
func TestSubscribePollAndRenderComposedFlow(t *testing.T) {
	s := newServer(t)
	ctx := context.Background()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/rss+xml")
		_, _ = w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0"><channel>
  <title>Composed Blog</title>
  <link>https://example.com/</link>
  <item>
    <title>Composed Post</title>
    <link>https://example.com/composed</link>
    <guid isPermaLink="false">tag:example.com,2026:composed-1</guid>
    <description><![CDATA[<p>Composed body <script>alert(1)</script>.</p>]]></description>
  </item>
</channel></rss>`))
	}))
	defer srv.Close()

	rec := s.PostHX(t, s.Alice, "/reader/subscribe", url.Values{"url": {srv.URL + "/feed.xml"}})
	if rec.Code != http.StatusOK {
		t.Fatalf("subscribe returned %d: %s", rec.Code, rec.Body.String())
	}

	client := reader.NewClient("test")
	client.DenyAddr = func(string) error { return nil }
	if err := reader.NewPoller(s.Store, client, quietLogger()).PollDue(ctx); err != nil {
		t.Fatalf("PollDue: %v", err)
	}

	tree, err := s.Store.Tree(ctx, s.Alice.User.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(tree.Root) != 1 {
		t.Fatalf("expected one subscription in the tree, got %+v", tree)
	}
	subID := tree.Root[0].ID

	feedDoc := s.Get(t, s.Alice, "/reader/feed/"+itoa(subID))
	if !strings.Contains(feedDoc.Text(), "Composed Post") {
		t.Errorf("polled article title missing from the feed pane:\n%s", feedDoc.Text())
	}

	items, err := s.Store.ItemsForSubscription(ctx, s.Alice.User.ID, subID, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("stored %d items, want 1", len(items))
	}

	itemDoc := s.Get(t, s.Alice, "/reader/item/"+itoa(items[0].ID))
	if !strings.Contains(itemDoc.Text(), "Composed body") {
		t.Errorf("polled article body missing from the article pane:\n%s", itemDoc.Text())
	}
	if strings.Contains(itemDoc.Text(), "alert(1)") {
		t.Error("unsanitized script content reached the rendered article")
	}
}
