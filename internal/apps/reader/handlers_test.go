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

	"golang.org/x/net/html"

	"github.com/iliafrenkel/on-suite/internal/apps/reader"
	"github.com/iliafrenkel/on-suite/internal/apptest"
	"github.com/iliafrenkel/on-suite/internal/htmlassert"
	"github.com/iliafrenkel/on-suite/internal/platform/web"
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
	s, a := newServerWithApp(t)

	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/rss+xml")
		_, _ = w.Write(fixture(t, "rss2.xml"))
	}))
	defer origin.Close()
	a.AllowPrivateFetchesForTest()

	rec := s.PostHX(t, s.Alice, "/reader/subscribe", url.Values{
		"url": {origin.URL + "/feed.xml"},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("subscribe returned %d: %s", rec.Code, rec.Body.String())
	}

	doc := s.Get(t, s.Alice, "/reader/")
	if !strings.Contains(doc.Text(), origin.URL+"/feed.xml") {
		t.Errorf("new subscription is not in the tree:\n%s", doc.Text())
	}
}

func TestSubscribeRejectsANonHTTPURL(t *testing.T) {
	s := newServer(t)

	rec := s.PostHX(t, s.Alice, "/reader/subscribe", url.Values{
		"url": {"file:///etc/passwd"},
	})
	if !strings.Contains(rec.Body.String(), "not a web address") {
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
	s, a := newServerWithApp(t)
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

	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/rss+xml")
		_, _ = w.Write(fixture(t, "rss2.xml"))
	}))
	defer origin.Close()
	a.AllowPrivateFetchesForTest()

	rec := s.PostHX(t, s.Alice, "/reader/subscribe", url.Values{
		"url":       {origin.URL + "/feed.xml"},
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
	s, a := newServerWithApp(t)
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
	a.AllowPrivateFetchesForTest()

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

// seedOne subscribes Alice to a feed and stores n items, returning them
// newest-first as the list pane would show them.
func seedOne(t *testing.T, s *apptest.Server[*reader.Store], guids ...string) (int64, []reader.Item) {
	t.Helper()
	ctx := context.Background()

	sub, err := s.Store.Subscribe(ctx, s.Alice.User.ID, "https://example.com/feed.xml", nil)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	var parsed []reader.ParsedItem
	for i, g := range guids {
		parsed = append(parsed, reader.ParsedItem{
			GUID:        g,
			Title:       "Article " + g,
			ContentHTML: "<p>Body of " + g + ".</p>",
			PublishedAt: now.Add(-time.Duration(i+1) * time.Hour),
		})
	}
	if _, err := s.Store.SaveItems(ctx, sub.FeedID, parsed, now); err != nil {
		t.Fatal(err)
	}
	items, err := s.Store.ItemsForSubscription(ctx, s.Alice.User.ID, sub.ID, 50)
	if err != nil {
		t.Fatal(err)
	}
	return sub.ID, items
}

func TestOpeningAnArticleMarksItReadAndOffersUndo(t *testing.T) {
	s := newServer(t)
	ctx := context.Background()
	_, items := seedOne(t, s, "g1")

	rec := s.Do(t, s.Alice, httptest.NewRequest(http.MethodGet, "/reader/item/"+itoa(items[0].ID), nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("article returned %d", rec.Code)
	}

	read, _, err := s.Store.ItemState(ctx, s.Alice.User.ID, items[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if !read {
		t.Error("opening an article did not mark it read")
	}
	if !strings.Contains(rec.Body.String(), "/unread") {
		t.Errorf("no undo control in the article pane:\n%s", rec.Body.String())
	}
}

// The drill-down layout depends on this: the article pane is swapped on its
// own, so the pane-state checkbox has to ride along out of band or a phone
// stays on the list after opening an article.
//
// The request has to actually claim to be an htmx one. The fragment-with-OOB
// shape this asserts is the htmx response; a request without HX-Request is a
// plain browser navigation and now correctly gets a whole page back (see
// TestPlainGetOfAnItemRendersAWholePage), which has no reason to carry an
// out-of-band anything. Asserting fragment markup on a non-htmx request was
// pinning the right shape through the wrong door.
func TestArticleResponseCarriesTheOOBPaneState(t *testing.T) {
	s := newServer(t)
	_, items := seedOne(t, s, "g1")

	req := httptest.NewRequest(http.MethodGet, "/reader/item/"+itoa(items[0].ID), nil)
	req.Header.Set("HX-Request", "true")
	rec := s.Do(t, s.Alice, req)
	body := rec.Body.String()
	if !strings.Contains(body, `id="reader-article-open"`) {
		t.Errorf("article response has no reader-article-open input:\n%s", body)
	}
	if !strings.Contains(body, `hx-swap-oob`) {
		t.Errorf("pane-state input is not swapped out of band:\n%s", body)
	}
}

func TestStarToggleRoundTrips(t *testing.T) {
	s := newServer(t)
	ctx := context.Background()
	_, items := seedOne(t, s, "g1")
	path := "/reader/item/" + itoa(items[0].ID) + "/star"

	s.PostHX(t, s.Alice, path, url.Values{})
	if _, starred, _ := s.Store.ItemState(ctx, s.Alice.User.ID, items[0].ID); !starred {
		t.Fatal("first star post did not star the item")
	}
	s.PostHX(t, s.Alice, path, url.Values{})
	if _, starred, _ := s.Store.ItemState(ctx, s.Alice.User.ID, items[0].ID); starred {
		t.Error("second star post did not unstar the item")
	}
}

func TestStateRoutesRefuseAnotherUsersItem(t *testing.T) {
	s := newServer(t)
	_, items := seedOne(t, s, "g1")

	for _, path := range []string{
		"/reader/item/" + itoa(items[0].ID) + "/read",
		"/reader/item/" + itoa(items[0].ID) + "/unread",
		"/reader/item/" + itoa(items[0].ID) + "/star",
	} {
		rec := s.PostHX(t, s.Bob, path, url.Values{})
		if rec.Code != http.StatusNotFound {
			t.Errorf("%s as bob returned %d, want 404 — a 403 would confirm the id exists", path, rec.Code)
		}
	}
}

func TestUnreadCountAppearsInTheTree(t *testing.T) {
	s := newServer(t)
	seedOne(t, s, "a", "b")

	doc := s.Get(t, s.Alice, "/reader/")
	if doc.Query(".reader-count") == nil {
		t.Fatalf("no .reader-count element in the tree:\n%s", doc.Text())
	}
	if !strings.Contains(doc.Text(), "2") {
		t.Errorf("unread count of 2 is not rendered:\n%s", doc.Text())
	}
}

func TestFilterNarrowsTheListToUnread(t *testing.T) {
	s := newServer(t)
	ctx := context.Background()
	subID, items := seedOne(t, s, "a", "b")

	if err := s.Store.SetRead(ctx, s.Alice.User.ID, items[0].ID, true, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}

	unread := s.Get(t, s.Alice, "/reader/feed/"+itoa(subID)+"?filter=unread")
	if n := len(unread.QueryAll(".reader-row")); n != 1 {
		t.Errorf("unread filter shows %d rows, want 1", n)
	}
	all := s.Get(t, s.Alice, "/reader/feed/"+itoa(subID)+"?filter=all")
	if n := len(all.QueryAll(".reader-row")); n != 2 {
		t.Errorf("all filter shows %d rows, want 2", n)
	}
}

func TestMarkAllReadClearsTheFeed(t *testing.T) {
	s := newServer(t)
	ctx := context.Background()
	subID, _ := seedOne(t, s, "a", "b")

	s.PostHX(t, s.Alice, "/reader/read-all", url.Values{
		"scope": {"feed"},
		"sub":   {itoa(subID)},
	})

	counts, err := s.Store.UnreadCounts(ctx, s.Alice.User.ID)
	if err != nil {
		t.Fatal(err)
	}
	if counts.Total != 0 {
		t.Errorf("%d items still unread after mark-all-read", counts.Total)
	}
}

func TestStarredScopeHasItsOwnPage(t *testing.T) {
	s := newServer(t)
	ctx := context.Background()
	_, items := seedOne(t, s, "a", "b")

	if err := s.Store.SetStarred(ctx, s.Alice.User.ID, items[0].ID, true, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	doc := s.Get(t, s.Alice, "/reader/starred?filter=all")
	if n := len(doc.QueryAll(".reader-row")); n != 1 {
		t.Errorf("starred page shows %d rows, want 1", n)
	}
}

// ---- Whole-branch review fixes ------------------------------------------

// R1 answered a GET for somebody else's subscription with 404, via
// ItemsForSubscription's ownership check. R2 moved the list onto
// ItemsForScope, which answers "not your subscription" with an empty list —
// so the page came back 200 with an empty list and a blank heading, quietly
// confirming nothing but also dropping the contract. The store-level test
// still passed because it exercises the wrapper nothing calls any more.
func TestFeedScopeIsNotFoundForAnotherUsersSubscription(t *testing.T) {
	s := newServer(t)
	subID, _ := seedOne(t, s, "a")

	rec := s.Do(t, s.Bob, httptest.NewRequest(http.MethodGet, "/reader/feed/"+itoa(subID), nil))
	if rec.Code != http.StatusNotFound {
		t.Errorf("GET another user's feed returned %d, want 404 — a 200 with an empty list still says the id exists", rec.Code)
	}

	rec = s.Do(t, s.Alice, httptest.NewRequest(http.MethodGet, "/reader/feed/999999", nil))
	if rec.Code != http.StatusNotFound {
		t.Errorf("GET a nonexistent feed returned %d, want 404", rec.Code)
	}
}

// A POST carries no query string and its path names an item, a folder or a
// subscription rather than the list on screen, so without the hidden fields
// the re-render silently fell back to All/Unread while the address bar still
// showed /reader/starred?filter=all.
func TestMarkAllReadStaysOnTheListItFiredFrom(t *testing.T) {
	s := newServer(t)
	ctx := context.Background()
	_, items := seedOne(t, s, "a", "b")

	if err := s.Store.SetStarred(ctx, s.Alice.User.ID, items[0].ID, true, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}

	rec := s.PostHX(t, s.Alice, "/reader/read-all", url.Values{
		"scope":  {"starred"},
		"filter": {"all"},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("mark-all-read returned %d: %s", rec.Code, rec.Body.String())
	}
	doc := htmlassert.Parse(t, rec.Body.String())

	if got := strings.TrimSpace(htmlassert.Text(doc.MustHave("h2"))); got != "Starred" {
		t.Errorf("re-render shows the %q list, want Starred", got)
	}
	if got := strings.TrimSpace(htmlassert.Text(doc.MustHave(".reader-filters a[aria-current=page]"))); got != "All" {
		t.Errorf("re-render is on the %q filter, want All", got)
	}
	if n := len(doc.QueryAll(".reader-row")); n != 1 {
		t.Errorf("starred/all re-render shows %d rows, want 1 (the starred item, now read)", n)
	}
}

// The mark-all-read form is the one that already carried scope and sub; every
// other POST control has to carry them too or it drops the reader back to All.
func TestEveryReaderFormCarriesTheCurrentList(t *testing.T) {
	s := newServer(t)
	subID, _ := seedOne(t, s, "a")

	doc := s.Get(t, s.Alice, "/reader/feed/"+itoa(subID)+"?filter=all")
	for _, sel := range []string{
		`#reader-panes input[name=scope]`,
		`#reader-panes input[name=sub]`,
		`#reader-panes input[name=filter]`,
	} {
		got := doc.QueryAll(sel)
		if len(got) < 4 {
			t.Errorf("%s appears %d times; every POST control in the panes needs it", sel, len(got))
			continue
		}
		for _, n := range got {
			v, _ := htmlassert.Attr(n, "value")
			if v == "" {
				t.Errorf("%s has an empty value; the re-render would fall back to All/Unread", sel)
			}
		}
	}
	if got, _ := htmlassert.Attr(doc.MustHave(`#reader-panes input[name=filter]`), "value"); got != "all" {
		t.Errorf("hidden filter field = %q, want all", got)
	}
}

// An active search must survive a full click-through round trip with
// JavaScript off: list (searching) -> click an article row -> the article's
// own forms (star, mark read) -> back to a list re-render (mark all read).
// Losing "q" anywhere in that loop silently reloads the unfiltered list, the
// same failure mode TestEveryReaderFormCarriesTheCurrentList pins for
// scope/sub/filter.
func TestSearchQuerySurvivesArticleOpenAndFormPost(t *testing.T) {
	s := newServer(t)
	ctx := context.Background()

	sub, err := s.Store.Subscribe(ctx, s.Alice.User.ID, "https://example.com/feed.xml", nil)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if _, err := s.Store.SaveItems(ctx, sub.FeedID, []reader.ParsedItem{
		{GUID: "a", Title: "Aerodynamics explained", PublishedAt: now.Add(-2 * time.Hour)},
		{GUID: "b", Title: "Table tennis grips", PublishedAt: now.Add(-time.Hour)},
	}, now); err != nil {
		t.Fatal(err)
	}

	// A plain (non-htmx) search: this is what a JavaScript-less GET of the
	// search form looks like.
	list := s.Get(t, s.Alice, "/reader/?filter=all&q=aero")
	if n := len(list.QueryAll(".reader-row")); n != 1 {
		t.Fatalf("search list has %d rows, want 1", n)
	}
	row := list.MustHave(".reader-row a")
	href, _ := htmlassert.Attr(row, "href")
	if !strings.Contains(href, "q=aero") {
		t.Fatalf("article row link %q does not carry the search query", href)
	}

	// Click the row with JavaScript off: a plain GET, no HX-Request header.
	article := s.Get(t, s.Alice, href)
	qInput := article.MustHave(`#reader-panes input[name=q]`)
	if v, _ := htmlassert.Attr(qInput, "value"); v != "aero" {
		t.Errorf("article view's hidden q field = %q, want aero", v)
	}

	// Submit the article's own star form exactly as a JavaScript-less browser
	// would: read every field the form itself carries — nothing supplied by
	// the test — and POST them to the form's own action. If reader-ctx ever
	// stops carrying q, this form simply would not have it to submit.
	starBtn := article.MustHave(".reader-article-star")
	starForm := formFields(t, starBtn)
	if got := starForm.Get("q"); got != "aero" {
		t.Fatalf("star form's own q field = %q, want aero — it cannot submit what it does not carry", got)
	}
	starAction, _ := htmlassert.Attr(ancestorForm(t, starBtn), "action")
	rec := s.Post(t, s.Alice, starAction, starForm)
	if rec.Code != http.StatusOK {
		t.Fatalf("star post returned %d: %s", rec.Code, rec.Body.String())
	}

	// Finally, a list-redrawing POST (mark all read) submitted the same way —
	// reading whatever fields the rendered form actually carries — must still
	// come back filtered and with the search box still showing the query, not
	// silently reset to the unfiltered list.
	list2 := s.Get(t, s.Alice, "/reader/?filter=all&q=aero")
	markAllBtn := list2.MustHave(".reader-mark-all button")
	markAllForm := formFields(t, markAllBtn)
	if got := markAllForm.Get("q"); got != "aero" {
		t.Fatalf("mark-all-read form's own q field = %q, want aero", got)
	}
	markAllAction, _ := htmlassert.Attr(ancestorForm(t, markAllBtn), "action")
	rec = s.Post(t, s.Alice, markAllAction, markAllForm)
	if rec.Code != http.StatusOK {
		t.Fatalf("mark-all-read returned %d: %s", rec.Code, rec.Body.String())
	}
	doc := htmlassert.Parse(t, rec.Body.String())
	if n := len(doc.QueryAll(".reader-row")); n != 1 {
		t.Errorf("mark-all-read re-render shows %d rows, want 1 (search still applied)", n)
	}
	input := doc.MustHave(`input[name="q"]`)
	if v, _ := htmlassert.Attr(input, "value"); v != "aero" {
		t.Errorf("search box value after mark-all-read = %q, want aero", v)
	}
}

// ancestorForm walks up from n to the <form> that contains it — the way a
// real browser knows which form a button submits.
func ancestorForm(t *testing.T, n *html.Node) *html.Node {
	t.Helper()
	for cur := n; cur != nil; cur = cur.Parent {
		if cur.Type == html.ElementNode && cur.Data == "form" {
			return cur
		}
	}
	t.Fatal("no ancestor <form> found")
	return nil
}

// formFields collects every <input name=... value=...> inside n's ancestor
// form, the way a browser assembles a submission — so a test exercises
// whatever the template actually rendered rather than fields the test made up
// itself.
func formFields(t *testing.T, n *html.Node) url.Values {
	t.Helper()
	form := ancestorForm(t, n)
	out := url.Values{}
	var walk func(*html.Node)
	walk = func(cur *html.Node) {
		if cur.Type == html.ElementNode && cur.Data == "input" {
			name, hasName := htmlassert.Attr(cur, "name")
			value, _ := htmlassert.Attr(cur, "value")
			if hasName {
				out.Set(name, value)
			}
		}
		for c := cur.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(form)
	return out
}

// With JavaScript off, hx-post on its own submits nowhere — which also makes
// the CSRF hidden fields decorative, since the token otherwise only travels in
// the header base.html sets for htmx.
func TestEveryReaderFormHasANonJSSubmitPath(t *testing.T) {
	s := newServer(t)
	ctx := context.Background()
	if _, err := s.Store.CreateFolder(ctx, s.Alice.User.ID, "Tech"); err != nil {
		t.Fatal(err)
	}
	seedOne(t, s, "a")

	doc := s.Get(t, s.Alice, "/reader/")
	forms := doc.QueryAll("#reader-panes form")
	if len(forms) < 5 {
		t.Fatalf("only %d forms in the panes; expected add-feed, add-folder, refresh, folder-delete, unsubscribe and mark-all-read", len(forms))
	}
	// The search box is a real form too, but a GET one: it has nowhere to
	// carry a CSRF token and needs none, the same exception ON Notes' own
	// search form makes (TestSearchFormWorksWithoutJavaScript-equivalent).
	csrfForms := 0
	for _, f := range forms {
		method, _ := htmlassert.Attr(f, "method")
		if strings.EqualFold(method, "get") {
			continue
		}
		csrfForms++
		action, _ := htmlassert.Attr(f, "action")
		if !strings.EqualFold(method, "post") || action == "" {
			t.Errorf(`form has method=%q action=%q; without both it submits nowhere with JavaScript off`, method, action)
		}
		if hx, ok := htmlassert.Attr(f, "hx-post"); ok && hx != action {
			t.Errorf("form action %q and hx-post %q disagree", action, hx)
		}
	}
	if n := len(doc.QueryAll(`#reader-panes input[name=` + web.CSRFFormField + `]`)); n < csrfForms {
		t.Errorf("%d CSRF fields for %d POST forms; a plain submission would be rejected", n, csrfForms)
	}
}

// Below 640px the server checks #reader-list-open on every render, so the CSS
// hid the tree — the feed list, both pseudo-nodes, both forms and Refresh —
// with nothing on screen able to bring it back. The back controls are plain
// labels for the drill-down checkboxes, which is what makes them work with no
// script.
func TestNarrowViewportHasABackControlAtEachDrillDownLevel(t *testing.T) {
	s := newServer(t)
	subID, items := seedOne(t, s, "a")

	doc := s.Get(t, s.Alice, "/reader/")
	doc.MustHave("input#reader-list-open")
	doc.MustHave("input#reader-article-open")
	back := doc.MustHave(".reader-list label[for=reader-list-open]")
	if _, ok := htmlassert.Attr(back, "class"); !ok {
		t.Error("the back control has no class, so no media query can reveal it")
	}

	rec := s.Do(t, s.Alice, httptest.NewRequest(http.MethodGet,
		"/reader/item/"+itoa(items[0].ID)+"?scope=feed&sub="+itoa(subID), nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("article returned %d", rec.Code)
	}
	htmlassert.Parse(t, rec.Body.String()).MustHave(".reader-article label[for=reader-article-open]")
}

// Reading an article changes an unread count, so the counts have to come back
// with the article or the sidebar keeps the pre-click numbers until some
// unrelated navigation reconciles them.
//
// And *only* the counts. Opening an article is the most frequent action in the
// app; sending the whole <nav> back would throw away a half-typed feed URL or
// folder name, re-expand every folder the reader had collapsed and reset the
// tree's scroll position, every single time. The absence assertions below are
// the direct proof the swap is narrow — a Go http test cannot simulate
// unsubmitted client-side form state, but markup that never reaches the
// browser cannot overwrite anything.
func TestArticleResponseSwapsOnlyTheSidebarCounts(t *testing.T) {
	s := newServer(t)
	subID, items := seedOne(t, s, "a", "b")

	req := httptest.NewRequest(http.MethodGet,
		"/reader/item/"+itoa(items[0].ID)+"?scope=feed&sub="+itoa(subID)+"&filter=unread", nil)
	req.Header.Set("HX-Request", "true")
	rec := s.Do(t, s.Alice, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("article returned %d: %s", rec.Code, rec.Body.String())
	}
	doc := htmlassert.Parse(t, rec.Body.String())

	// The counts that changed, addressed by their stable ids.
	for _, id := range []string{"#reader-count-all", "#reader-count-sub-" + itoa(subID)} {
		span := doc.MustHave(id)
		if got, _ := htmlassert.Attr(span, "hx-swap-oob"); got != "true" {
			t.Errorf("%s hx-swap-oob = %q, want true — htmx would discard it", id, got)
		}
		if got := strings.TrimSpace(htmlassert.Text(span)); got != "1" {
			t.Errorf("%s = %q after reading one of two articles, want 1", id, got)
		}
	}
	// Zero still renders, as an empty span: an element that disappears at
	// zero cannot be swapped back when the count rises again.
	starred := doc.MustHave("#reader-count-starred")
	if got := strings.TrimSpace(htmlassert.Text(starred)); got != "" {
		t.Errorf("#reader-count-starred = %q with nothing starred, want no visible number", got)
	}

	// Nothing else from the tree is in the response.
	doc.MustNotHave("nav")
	doc.MustNotHave("#feed-url")
	doc.MustNotHave("#folder-name")
	doc.MustNotHave("details")
	doc.MustNotHave("form.reader-add")
}

// The star form's own POST has to keep the counts in step too, and the article
// it sends back has to stay pointed at the list it was opened from — that
// context lives only in the form's hidden fields, so a round trip that drops
// it strands the next star or unread post on All/Unread.
func TestStarResponseKeepsTheCountsAndTheListContext(t *testing.T) {
	s := newServer(t)
	subID, items := seedOne(t, s, "a", "b")

	rec := s.PostHX(t, s.Alice, "/reader/item/"+itoa(items[0].ID)+"/star", url.Values{
		"scope":  {"feed"},
		"sub":    {itoa(subID)},
		"filter": {"all"},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("star returned %d: %s", rec.Code, rec.Body.String())
	}
	doc := htmlassert.Parse(t, rec.Body.String())

	starred := doc.MustHave("#reader-count-starred")
	if got := strings.TrimSpace(htmlassert.Text(starred)); got != "1" {
		t.Errorf("#reader-count-starred = %q after starring one item, want 1", got)
	}
	doc.MustNotHave("nav")

	for sel, want := range map[string]string{
		`input[name=scope]`:  "feed",
		`input[name=sub]`:    itoa(subID),
		`input[name=filter]`: "all",
	} {
		field := doc.MustHave(sel)
		if got, _ := htmlassert.Attr(field, "value"); got != want {
			t.Errorf("%s value = %q, want %q — the article forms lost their list context", sel, got, want)
		}
	}
}

// A plain item link click with JavaScript off must land on the whole app, not
// a floating <article>. Reading an article is what this app is for, so this is
// the one place a bare fragment would have made the no-JS path useless.
func TestPlainGetOfAnItemRendersAWholePage(t *testing.T) {
	s := newServer(t)
	ctx := context.Background()
	_, items := seedOne(t, s, "a", "b")

	rec := s.Do(t, s.Alice, httptest.NewRequest(http.MethodGet, "/reader/item/"+itoa(items[0].ID), nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("plain GET of an item = %d: %s", rec.Code, rec.Body.String())
	}
	doc := htmlassert.Parse(t, rec.Body.String())
	doc.MustHave("main")
	doc.MustHave("nav.reader-tree")
	doc.MustHave("section.reader-list")
	doc.MustHave("article.reader-article .reader-body")

	// Marking read on open is the R2 design's core behaviour and does not
	// depend on which shape the response takes.
	if read, _, err := s.Store.ItemState(ctx, s.Alice.User.ID, items[0].ID); err != nil {
		t.Fatal(err)
	} else if !read {
		t.Error("the whole-page render skipped marking the item read")
	}
}

// A plain form POST — no htmx — must come back as a whole page, not a bare
// fragment. This is the other half of giving the forms method/action: the
// non-JS path has to land somewhere usable.
func TestPlainPostRendersAWholePage(t *testing.T) {
	s := newServer(t)
	subID, items := seedOne(t, s, "a")

	for _, tt := range []struct {
		name string
		path string
		form url.Values
	}{
		{"read-all", "/reader/read-all", url.Values{"scope": {"feed"}, "sub": {itoa(subID)}, "filter": {"all"}}},
		{"star", "/reader/item/" + itoa(items[0].ID) + "/star", url.Values{"scope": {"feed"}, "sub": {itoa(subID)}, "filter": {"all"}}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			rec := s.Post(t, s.Alice, tt.path, tt.form)
			if rec.Code != http.StatusOK {
				t.Fatalf("POST %s = %d: %s", tt.path, rec.Code, rec.Body.String())
			}
			doc := htmlassert.Parse(t, rec.Body.String())
			doc.MustHave("nav.reader-tree")
			doc.MustHave("section.reader-list")
			if doc.Query("main") == nil {
				t.Errorf("POST %s answered with a fragment, not a page:\n%s", tt.path, rec.Body.String())
			}
		})
	}
}

// This pins accepted behaviour rather than asking for new behaviour: the
// added_at cutoff in ItemsForScope's base WHERE applies before the filter
// switch, so a second household member who subscribes to an already-fetched
// feed sees nothing from before their subscription under *any* filter, not
// just Unread. The existing store-level test only covers Unread, which is why
// nothing pinned the All case through the HTTP layer.
func TestASecondSubscriberSeesNoBacklogUnderAnyFilter(t *testing.T) {
	s := newServer(t)
	ctx := context.Background()
	now := time.Now().UTC()

	// The cutoff compares an item's fetched_at with a subscription's added_at,
	// both of which the store stamps from its own clock. Setting added_at
	// directly is what makes "alice was subscribed when this was fetched, bob
	// was not" a fact of the fixture rather than a race on wall-clock order.
	aliceSub, err := s.Store.Subscribe(ctx, s.Alice.User.ID, "https://example.com/feed.xml", nil)
	if err != nil {
		t.Fatal(err)
	}
	fetchedAt := now.Add(-time.Hour)
	setAddedAt(t, s, aliceSub.ID, now.Add(-2*time.Hour))

	if _, err := s.Store.SaveItems(ctx, aliceSub.FeedID, []reader.ParsedItem{{
		GUID:        "old",
		Title:       "Published before bob subscribed",
		PublishedAt: now.Add(-30 * 24 * time.Hour),
	}}, fetchedAt); err != nil {
		t.Fatal(err)
	}
	bobSub, err := s.Store.Subscribe(ctx, s.Bob.User.ID, "https://example.com/feed.xml", nil)
	if err != nil {
		t.Fatal(err)
	}
	setAddedAt(t, s, bobSub.ID, now)

	for _, filter := range []string{"unread", "all"} {
		doc := s.Get(t, s.Bob, "/reader/feed/"+itoa(bobSub.ID)+"?filter="+filter)
		if n := len(doc.QueryAll(".reader-row")); n != 0 {
			t.Errorf("filter=%s shows bob %d items from before he subscribed, want 0", filter, n)
		}
		doc = s.Get(t, s.Alice, "/reader/feed/"+itoa(aliceSub.ID)+"?filter="+filter)
		if n := len(doc.QueryAll(".reader-row")); n != 1 {
			t.Errorf("filter=%s shows alice %d items, want 1 — the cutoff must not hide the original subscriber's own backlog", filter, n)
		}
	}
}

// setAddedAt pins when a subscription started, the way retention_test.go does,
// so a cutoff test does not depend on the order two wall-clock reads happen to
// land in.
func setAddedAt(t *testing.T, s *apptest.Server[*reader.Store], subID int64, at time.Time) {
	t.Helper()
	if _, err := s.Store.DB().ExecContext(context.Background(),
		`UPDATE reader_subs SET added_at = ? WHERE id = ?`,
		at.UTC().Format(time.RFC3339Nano), subID); err != nil {
		t.Fatal(err)
	}
}

// TestArticleResponseSwapsOnlyTheSidebarCounts pins the OOB fragment's ids
// (#reader-count-all, #reader-count-starred, #reader-count-sub-N); this test
// pins the other half of that contract — that the full page render emits the
// SAME ids, for a subscription inside a folder as well as one at the root.
// The tree template has two near-identical per-subscription count spans, one
// in the folder loop and one in the root loop, so a future edit to either
// could drift from the OOB side without either test failing on its own.
func TestReaderPageEmitsTheSidebarCountIdsTheOOBSwapTargets(t *testing.T) {
	s := newServer(t)
	ctx := context.Background()

	folder, err := s.Store.CreateFolder(ctx, s.Alice.User.ID, "Tech")
	if err != nil {
		t.Fatal(err)
	}
	folderSub, err := s.Store.Subscribe(ctx, s.Alice.User.ID, "https://example.com/folder-feed.xml", &folder.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Store.SaveItems(ctx, folderSub.FeedID, []reader.ParsedItem{{
		GUID: "f1", Title: "In folder", PublishedAt: time.Now().UTC(),
	}}, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}

	rootSubID, _ := seedOne(t, s, "r1")

	doc := s.Get(t, s.Alice, "/reader/")
	doc.MustHave("#reader-count-all")
	starred := doc.MustHave("#reader-count-starred")
	if got := strings.TrimSpace(htmlassert.Text(starred)); got != "" {
		t.Errorf("#reader-count-starred = %q with nothing starred, want an empty span (present, not absent)", got)
	}
	doc.MustHave("#reader-count-sub-" + itoa(folderSub.ID))
	doc.MustHave("#reader-count-sub-" + itoa(rootSubID))
}

// The whole of R3 in one assertion: an article that arrived full of remote
// images renders with none of them, and with proxy URLs instead.
func TestArticlePaneNeverEmitsAPublisherImageHost(t *testing.T) {
	s := newServer(t)
	ctx := context.Background()

	sub, err := s.Store.Subscribe(ctx, s.Alice.User.ID, "https://example.com/feed.xml", nil)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := reader.ParseFeed([]byte(`<?xml version="1.0"?>
<rss version="2.0" xmlns:content="http://purl.org/rss/1.0/modules/content/">
  <channel><title>T</title><link>https://example.com/</link>
    <item>
      <title>Pictures</title>
      <link>https://example.com/post</link>
      <guid>g1</guid>
      <content:encoded><![CDATA[
        <p>Words</p>
        <img src="https://tracker.example/pixel.gif">
        <img src="/relative.png">
      ]]></content:encoded>
    </item>
  </channel>
</rss>`), "https://example.com/feed.xml")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Store.SaveItems(ctx, sub.FeedID, parsed.Items, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	items, err := s.Store.ItemsForSubscription(ctx, s.Alice.User.ID, sub.ID, 10)
	if err != nil {
		t.Fatal(err)
	}

	rec := s.Do(t, s.Alice, httptest.NewRequest(http.MethodGet, "/reader/item/"+itoa(items[0].ID), nil))
	body := rec.Body.String()

	for _, host := range []string{"tracker.example", "/relative.png"} {
		if strings.Contains(body, host) {
			t.Errorf("rendered page still references %q:\n%s", host, body)
		}
	}
	if n := strings.Count(body, "/reader/img/"); n != 2 {
		t.Errorf("page has %d proxy image URLs, want 2:\n%s", n, body)
	}
	if !strings.Contains(body, `loading="lazy"`) {
		t.Error("proxied images are not lazy-loaded")
	}
}

func TestFetchFullArticleStoresAndShowsIt(t *testing.T) {
	s, a := newServerWithApp(t)
	ctx := context.Background()

	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(articlePage))
	}))
	defer origin.Close()
	a.AllowPrivateFetchesForTest()

	sub, err := s.Store.Subscribe(ctx, s.Alice.User.ID, "https://example.com/feed.xml", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Store.SaveItems(ctx, sub.FeedID, []reader.ParsedItem{{
		GUID: "g1", Title: "Teaser", URL: origin.URL + "/post",
		SummaryHTML: "<p>Only a teaser.</p>",
		PublishedAt: time.Now().UTC().Add(-time.Hour),
	}}, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	items, err := s.Store.ItemsForSubscription(ctx, s.Alice.User.ID, sub.ID, 10)
	if err != nil {
		t.Fatal(err)
	}

	rec := s.PostHX(t, s.Alice, "/reader/item/"+itoa(items[0].ID)+"/full", url.Values{})
	if rec.Code != http.StatusOK {
		t.Fatalf("fetch returned %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "First real paragraph") {
		t.Errorf("the extracted body is not in the response:\n%s", rec.Body.String())
	}

	got, err := s.Store.Item(ctx, s.Alice.User.ID, items[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if !got.HasFull() {
		t.Error("the extracted article was not stored")
	}
}

func TestFullArticleTogglesBackToTheFeedBody(t *testing.T) {
	s := newServer(t)
	ctx := context.Background()

	sub, err := s.Store.Subscribe(ctx, s.Alice.User.ID, "https://example.com/feed.xml", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Store.SaveItems(ctx, sub.FeedID, []reader.ParsedItem{{
		GUID: "g1", Title: "T", SummaryHTML: "<p>THE FEED VERSION.</p>",
		PublishedAt: time.Now().UTC().Add(-time.Hour),
	}}, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	items, err := s.Store.ItemsForSubscription(ctx, s.Alice.User.ID, sub.ID, 10)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Store.SaveFullArticle(ctx, s.Alice.User.ID, items[0].ID, reader.Extracted{
		HTML: "<p>THE FULL VERSION.</p>", TextLength: 500,
	}, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	path := "/reader/item/" + itoa(items[0].ID)

	// With a full article stored, that is what an open shows.
	full := s.Do(t, s.Alice, httptest.NewRequest(http.MethodGet, path, nil))
	if !strings.Contains(full.Body.String(), "THE FULL VERSION") {
		t.Errorf("stored full article is not shown by default:\n%s", full.Body.String())
	}

	// ...and the toggle goes back.
	feed := s.Do(t, s.Alice, httptest.NewRequest(http.MethodGet, path+"?view=feed", nil))
	if !strings.Contains(feed.Body.String(), "THE FEED VERSION") {
		t.Errorf("?view=feed did not show the feed body:\n%s", feed.Body.String())
	}
	if strings.Contains(feed.Body.String(), "THE FULL VERSION") {
		t.Error("?view=feed showed the full body as well")
	}
}

func TestFetchFullArticleReportsAFailureInThePane(t *testing.T) {
	s, a := newServerWithApp(t)
	ctx := context.Background()

	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nope", http.StatusPaymentRequired)
	}))
	defer origin.Close()
	a.AllowPrivateFetchesForTest()

	sub, err := s.Store.Subscribe(ctx, s.Alice.User.ID, "https://example.com/feed.xml", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Store.SaveItems(ctx, sub.FeedID, []reader.ParsedItem{{
		GUID: "g1", Title: "T", URL: origin.URL + "/post",
		SummaryHTML: "<p>Teaser.</p>", PublishedAt: time.Now().UTC().Add(-time.Hour),
	}}, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	items, err := s.Store.ItemsForSubscription(ctx, s.Alice.User.ID, sub.ID, 10)
	if err != nil {
		t.Fatal(err)
	}

	rec := s.PostHX(t, s.Alice, "/reader/item/"+itoa(items[0].ID)+"/full", url.Values{})
	// A page that will not extract is an ordinary outcome, not a server error:
	// the pane keeps the feed body and says what happened.
	if rec.Code != http.StatusOK {
		t.Fatalf("failed fetch returned %d, want 200 with a message", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "Teaser.") {
		t.Errorf("the feed body was lost when the fetch failed:\n%s", body)
	}
	if !strings.Contains(strings.ToLower(body), "could not") {
		t.Errorf("no explanation shown for the failed fetch:\n%s", body)
	}
}

func TestFetchFullArticleRefusesAnotherUsersItem(t *testing.T) {
	s := newServer(t)
	ctx := context.Background()

	sub, err := s.Store.Subscribe(ctx, s.Alice.User.ID, "https://example.com/feed.xml", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Store.SaveItems(ctx, sub.FeedID, []reader.ParsedItem{{
		GUID: "g1", Title: "Alice's", URL: "https://example.com/post",
		PublishedAt: time.Now().UTC().Add(-time.Hour),
	}}, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	items, err := s.Store.ItemsForSubscription(ctx, s.Alice.User.ID, sub.ID, 10)
	if err != nil {
		t.Fatal(err)
	}

	rec := s.PostHX(t, s.Bob, "/reader/item/"+itoa(items[0].ID)+"/full", url.Values{})
	if rec.Code != http.StatusNotFound {
		t.Errorf("bob got %d fetching a full article for a feed he does not subscribe to, want 404", rec.Code)
	}
}

func TestExportOPMLDownloads(t *testing.T) {
	s := newServer(t)
	ctx := context.Background()

	folder, err := s.Store.CreateFolder(ctx, s.Alice.User.ID, "Tech")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Store.Subscribe(ctx, s.Alice.User.ID, "https://a.example/feed.xml", &folder.ID); err != nil {
		t.Fatal(err)
	}

	rec := s.Do(t, s.Alice, httptest.NewRequest(http.MethodGet, "/reader/opml", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("export returned %d", rec.Code)
	}
	if cd := rec.Header().Get("Content-Disposition"); !strings.Contains(cd, "attachment") {
		t.Errorf("Content-Disposition = %q, want an attachment", cd)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "xml") {
		t.Errorf("Content-Type = %q", ct)
	}

	entries, err := reader.ParseOPML(rec.Body.Bytes())
	if err != nil {
		t.Fatalf("exported document does not parse: %v\n%s", err, rec.Body.String())
	}
	if len(entries) != 1 || entries[0].Folder != "Tech" {
		t.Errorf("exported %+v, want one entry in Tech", entries)
	}
}

func TestExportOPMLShowsOnlyYourOwnFeeds(t *testing.T) {
	s := newServer(t)
	ctx := context.Background()

	if _, err := s.Store.Subscribe(ctx, s.Bob.User.ID, "https://bobs.example/feed.xml", nil); err != nil {
		t.Fatal(err)
	}
	rec := s.Do(t, s.Alice, httptest.NewRequest(http.MethodGet, "/reader/opml", nil))
	if strings.Contains(rec.Body.String(), "bobs.example") {
		t.Errorf("alice's export contains bob's feed:\n%s", rec.Body.String())
	}
}

func TestImportOPMLUpload(t *testing.T) {
	s := newServer(t)
	ctx := context.Background()

	const doc = `<?xml version="1.0"?><opml version="2.0"><body>
		<outline text="Tech"><outline type="rss" text="A" xmlUrl="https://a.example/feed.xml"/></outline>
	</body></opml>`

	rec := s.UploadHX(t, s.Alice, "/reader/opml", "file", "subs.opml", []byte(doc))
	if rec.Code != http.StatusOK {
		t.Fatalf("import returned %d: %s", rec.Code, rec.Body.String())
	}

	tree, err := s.Store.Tree(ctx, s.Alice.User.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(tree.Folders) != 1 || len(tree.Folders[0].Subs) != 1 {
		t.Errorf("import produced %+v", tree)
	}
	if !strings.Contains(rec.Body.String(), "1") {
		t.Errorf("no count reported back to the user:\n%s", rec.Body.String())
	}
}

func TestImportOPMLRejectsARubbishFile(t *testing.T) {
	s := newServer(t)

	rec := s.UploadHX(t, s.Alice, "/reader/opml", "file", "notes.md", []byte("# not opml"))
	if rec.Code == http.StatusInternalServerError {
		t.Fatalf("a bad upload produced a 500; it is ordinary user input")
	}
	if !strings.Contains(strings.ToLower(rec.Body.String()), "opml") {
		t.Errorf("no explanation of what was wrong:\n%s", rec.Body.String())
	}
}

func TestSubscribeAcceptsASiteURLAndFindsTheFeed(t *testing.T) {
	s, a := newServerWithApp(t)
	ctx := context.Background()

	mux := http.NewServeMux()
	mux.HandleFunc("/feed.xml", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/rss+xml")
		_, _ = w.Write(fixture(t, "rss2.xml"))
	})
	origin := httptest.NewServer(mux)
	defer origin.Close()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`<html><head><link rel="alternate" type="application/rss+xml" href="` +
			origin.URL + `/feed.xml"></head><body>hi</body></html>`))
	})
	a.AllowPrivateFetchesForTest()

	rec := s.PostHX(t, s.Alice, "/reader/subscribe", url.Values{"url": {origin.URL + "/"}})
	if rec.Code != http.StatusOK {
		t.Fatalf("subscribe returned %d: %s", rec.Code, rec.Body.String())
	}

	tree, err := s.Store.Tree(ctx, s.Alice.User.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(tree.Root) != 1 {
		t.Fatalf("subscribed to %d feeds, want 1", len(tree.Root))
	}
	if tree.Root[0].FeedURL != origin.URL+"/feed.xml" {
		t.Errorf("subscribed to %q, want the discovered feed URL", tree.Root[0].FeedURL)
	}
}

// A URL that is already a feed must not need discovery, and must not cost an
// extra request.
func TestSubscribeToADirectFeedURLStillWorks(t *testing.T) {
	s, a := newServerWithApp(t)
	ctx := context.Background()

	var hits int
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		w.Header().Set("Content-Type", "application/rss+xml")
		_, _ = w.Write(fixture(t, "rss2.xml"))
	}))
	defer origin.Close()
	a.AllowPrivateFetchesForTest()

	if rec := s.PostHX(t, s.Alice, "/reader/subscribe", url.Values{"url": {origin.URL + "/feed.xml"}}); rec.Code != http.StatusOK {
		t.Fatalf("subscribe returned %d", rec.Code)
	}
	if hits != 1 {
		t.Errorf("origin was fetched %d times for a direct feed URL, want 1", hits)
	}

	tree, err := s.Store.Tree(ctx, s.Alice.User.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(tree.Root) != 1 {
		t.Errorf("subscribed to %d feeds, want 1", len(tree.Root))
	}
}

func TestSubscribeReportsWhenNoFeedCanBeFound(t *testing.T) {
	s, a := newServerWithApp(t)

	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`<html><body>No feed here at all.</body></html>`))
	}))
	defer origin.Close()
	a.AllowPrivateFetchesForTest()

	rec := s.PostHX(t, s.Alice, "/reader/subscribe", url.Values{"url": {origin.URL + "/"}})
	if rec.Code != http.StatusOK {
		t.Fatalf("returned %d, want 200 with a message", rec.Code)
	}
	if !strings.Contains(strings.ToLower(rec.Body.String()), "feed") {
		t.Errorf("no explanation shown:\n%s", rec.Body.String())
	}
}

// The SSRF guard must still apply to discovery: it is a fetch of a URL a user
// typed, which is exactly the case the dialer guard exists for.
func TestSubscribeDiscoveryRefusesAPrivateAddress(t *testing.T) {
	s := newServer(t) // no AllowPrivateFetchesForTest: the real guard is live

	rec := s.PostHX(t, s.Alice, "/reader/subscribe", url.Values{
		"url": {"http://169.254.169.254/latest/meta-data/"},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("returned %d, want 200 with a message", rec.Code)
	}
	tree, err := s.Store.Tree(context.Background(), s.Alice.User.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(tree.Root) != 0 {
		t.Errorf("a link-local address was subscribed to: %+v", tree.Root)
	}
}

// TestChooserPreservesTheSelectedFolder pins the fix for the folder the user
// picked on the add-feed form surviving the discovery chooser round trip: a
// pasted site with several feeds must not silently drop the folder in favour
// of the root.
func TestChooserPreservesTheSelectedFolder(t *testing.T) {
	s, a := newServerWithApp(t)
	ctx := context.Background()

	folder, err := s.Store.CreateFolder(ctx, s.Alice.User.ID, "Tech")
	if err != nil {
		t.Fatal(err)
	}

	mux := http.NewServeMux()
	origin := httptest.NewServer(mux)
	defer origin.Close()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`<html><head>` +
			`<link rel="alternate" type="application/rss+xml" title="Posts" href="` + origin.URL + `/feed1.xml">` +
			`<link rel="alternate" type="application/atom+xml" title="Comments" href="` + origin.URL + `/feed2.xml">` +
			`</head><body>hi</body></html>`))
	})
	mux.HandleFunc("/feed1.xml", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/rss+xml")
		_, _ = w.Write(fixture(t, "rss2.xml"))
	})
	mux.HandleFunc("/feed2.xml", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/rss+xml")
		_, _ = w.Write(fixture(t, "rss2.xml"))
	})
	a.AllowPrivateFetchesForTest()

	rec := s.PostHX(t, s.Alice, "/reader/subscribe", url.Values{
		"url":       {origin.URL + "/"},
		"folder_id": {itoa(folder.ID)},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("subscribe returned %d: %s", rec.Code, rec.Body.String())
	}

	doc := htmlassert.Parse(t, rec.Body.String())
	forms := doc.QueryAll(".reader-candidates form")
	if len(forms) < 2 {
		t.Fatalf("chooser rendered %d candidate forms, want at least 2:\n%s", len(forms), rec.Body.String())
	}

	urlIn := doc.Query(".reader-candidates form input[name=url]")
	candidateURL, ok := htmlassert.Attr(urlIn, "value")
	if !ok || candidateURL == "" {
		t.Fatalf("chooser candidate form has no url field:\n%s", rec.Body.String())
	}
	folderIn := doc.Query(".reader-candidates form input[name=folder_id]")
	folderVal, ok := htmlassert.Attr(folderIn, "value")
	if !ok {
		t.Fatalf("chooser candidate form did not carry the selected folder along:\n%s", rec.Body.String())
	}
	if folderVal != itoa(folder.ID) {
		t.Errorf("chooser folder_id = %q, want %q", folderVal, itoa(folder.ID))
	}

	// Click the candidate: the second POST a real browser would make.
	rec = s.PostHX(t, s.Alice, "/reader/subscribe", url.Values{
		"url":       {candidateURL},
		"folder_id": {folderVal},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("subscribe (from chooser) returned %d: %s", rec.Code, rec.Body.String())
	}

	tree, err := s.Store.Tree(ctx, s.Alice.User.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(tree.Folders) != 1 || len(tree.Folders[0].Subs) != 1 {
		t.Fatalf("subscription did not land in the selected folder: %+v", tree)
	}
	sub := tree.Folders[0].Subs[0]
	if sub.FolderID == nil || *sub.FolderID != folder.ID {
		t.Errorf("subscription FolderID = %v, want %d", sub.FolderID, folder.ID)
	}
}

// TestSubscribeFindsAFeedAtAConventionalProbePath pins probeForFeed's success
// path: every other discovery test has every probe path fail, so nothing
// proved a feed actually reachable at one of them gets found.
func TestSubscribeFindsAFeedAtAConventionalProbePath(t *testing.T) {
	s, a := newServerWithApp(t)
	ctx := context.Background()

	if len(reader.ProbePaths) == 0 {
		t.Fatal("reader.ProbePaths is empty")
	}
	probePath := reader.ProbePaths[0]

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`<html><body>Just a homepage, no feed link here.</body></html>`))
	})
	mux.HandleFunc(probePath, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/rss+xml")
		_, _ = w.Write(fixture(t, "rss2.xml"))
	})
	origin := httptest.NewServer(mux)
	defer origin.Close()
	a.AllowPrivateFetchesForTest()

	rec := s.PostHX(t, s.Alice, "/reader/subscribe", url.Values{"url": {origin.URL + "/"}})
	if rec.Code != http.StatusOK {
		t.Fatalf("subscribe returned %d: %s", rec.Code, rec.Body.String())
	}

	tree, err := s.Store.Tree(ctx, s.Alice.User.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(tree.Root) != 1 {
		t.Fatalf("subscribed to %d feeds, want 1", len(tree.Root))
	}
	if want := origin.URL + probePath; tree.Root[0].FeedURL != want {
		t.Errorf("subscribed to %q, want the probed feed URL %q", tree.Root[0].FeedURL, want)
	}
}

// TestDiscoveryEnforcesAnOverallDeadline pins resolveFeedURL's discovery
// timeout: a stalling origin must not hold the request open past the
// (test-shrunk) deadline, and must fail with an ordinary message rather than
// hang.
func TestDiscoveryEnforcesAnOverallDeadline(t *testing.T) {
	s, a := newServerWithApp(t)

	restore := reader.SetDiscoveryTimeoutForTest(300 * time.Millisecond)
	defer restore()

	// blocked is never closed until after the server shuts down: closing it
	// first would let the held request finish, and origin.Close() waits for
	// exactly that request to finish, so the defer order below matters —
	// close(blocked) must run before origin.Close() (LIFO: registered last).
	blocked := make(chan struct{})
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-blocked // held open past the shrunk discovery deadline
	}))
	defer origin.Close()
	defer close(blocked)
	a.AllowPrivateFetchesForTest()

	start := time.Now()
	rec := s.PostHX(t, s.Alice, "/reader/subscribe", url.Values{"url": {origin.URL + "/"}})
	elapsed := time.Since(start)

	if elapsed > 5*time.Second {
		t.Fatalf("subscribe took %s, want it bounded by the discovery deadline", elapsed)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("returned %d, want 200 with a message", rec.Code)
	}
	if !strings.Contains(strings.ToLower(rec.Body.String()), "reach") {
		t.Errorf("no explanation shown for the timed-out fetch:\n%s", rec.Body.String())
	}
}

func TestSearchNarrowsTheList(t *testing.T) {
	s := newServer(t)
	ctx := context.Background()

	sub, err := s.Store.Subscribe(ctx, s.Alice.User.ID, "https://example.com/feed.xml", nil)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if _, err := s.Store.SaveItems(ctx, sub.FeedID, []reader.ParsedItem{
		{GUID: "a", Title: "Aerodynamics", PublishedAt: now.Add(-2 * time.Hour)},
		{GUID: "b", Title: "Table tennis", PublishedAt: now.Add(-time.Hour)},
	}, now); err != nil {
		t.Fatal(err)
	}

	all := s.Get(t, s.Alice, "/reader/?filter=all")
	if n := len(all.QueryAll(".reader-row")); n != 2 {
		t.Fatalf("unfiltered list has %d rows, want 2", n)
	}
	hit := s.Get(t, s.Alice, "/reader/?filter=all&q=aero")
	if n := len(hit.QueryAll(".reader-row")); n != 1 {
		t.Errorf("search returned %d rows, want 1", n)
	}
}

// The query has to survive in the box, or a live filter clears itself on every
// keystroke's response.
func TestSearchQueryIsPrefilledInTheBox(t *testing.T) {
	s := newServer(t)

	doc := s.Get(t, s.Alice, "/reader/?q=aero")
	input := doc.Query(`input[name="q"]`)
	if input == nil {
		t.Fatal("no search input in the list pane")
	}
	var value string
	for _, a := range input.Attr {
		if a.Key == "value" {
			value = a.Val
		}
	}
	if value != "aero" {
		t.Errorf("search box value = %q, want the current query", value)
	}
}

// Search must not silently widen the scope: searching inside one feed stays
// inside it.
func TestSearchStaysWithinTheCurrentFeed(t *testing.T) {
	s := newServer(t)
	ctx := context.Background()

	a, err := s.Store.Subscribe(ctx, s.Alice.User.ID, "https://a.example/feed.xml", nil)
	if err != nil {
		t.Fatal(err)
	}
	b, err := s.Store.Subscribe(ctx, s.Alice.User.ID, "https://b.example/feed.xml", nil)
	if err != nil {
		t.Fatal(err)
	}
	// Captured after both Subscribe calls, not before: an item's fetched_at
	// must land at or after its subscription's added_at (ItemsForScope's
	// cutoff), and capturing "now" first — the brief's original ordering —
	// races that cutoff against whatever Subscribe's own internal clock read
	// a moment later, which is flaky whenever the two happen to fall in the
	// same instant.
	now := time.Now().UTC()
	for _, x := range []struct {
		feed int64
		guid string
	}{{a.FeedID, "a"}, {b.FeedID, "b"}} {
		if _, err := s.Store.SaveItems(ctx, x.feed, []reader.ParsedItem{
			{GUID: x.guid, Title: "Racing report", PublishedAt: now.Add(-time.Hour)},
		}, now); err != nil {
			t.Fatal(err)
		}
	}

	doc := s.Get(t, s.Alice, "/reader/feed/"+itoa(a.ID)+"?filter=all&q=racing")
	if n := len(doc.QueryAll(".reader-row")); n != 1 {
		t.Errorf("searching within one feed returned %d rows, want 1", n)
	}
}

func TestScriptIsServed(t *testing.T) {
	s := newServer(t)

	rec := s.Do(t, s.Alice, httptest.NewRequest(http.MethodGet, "/reader/reader.js", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("reader.js returned %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "javascript") {
		t.Errorf("Content-Type = %q", ct)
	}
}

// The whole reason prefetch needed a variant: arrowing past an article must
// not mark it read.
func TestPrefetchDoesNotMarkAnArticleRead(t *testing.T) {
	s := newServer(t)
	ctx := context.Background()
	_, items := seedOne(t, s, "g1")

	rec := s.Do(t, s.Alice, httptest.NewRequest(http.MethodGet,
		"/reader/item/"+itoa(items[0].ID)+"?prefetch=1", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("prefetch returned %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "Body of g1") {
		t.Errorf("prefetch did not render the article:\n%s", rec.Body.String())
	}

	read, _, err := s.Store.ItemState(ctx, s.Alice.User.ID, items[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if read {
		t.Error("a prefetch marked the article read")
	}
}

// reader.js's keyboard fast path installs a prefetched response with a plain
// outerHTML assignment, not an htmx-processed swap, so the OOB checkbox and
// OOB count spans a normal article response carries would land as permanent,
// duplicate-id sibling markup instead of being specially handled. The
// prefetch response must be just the bare article fragment.
func TestPrefetchResponseHasNoOOBMarkup(t *testing.T) {
	s := newServer(t)
	_, items := seedOne(t, s, "g1")

	req := httptest.NewRequest(http.MethodGet, "/reader/item/"+itoa(items[0].ID)+"?prefetch=1", nil)
	req.Header.Set("HX-Request", "true")
	rec := s.Do(t, s.Alice, req)
	body := rec.Body.String()

	if !strings.Contains(body, "Body of g1") {
		t.Fatalf("prefetch response did not render the article:\n%s", body)
	}
	if strings.Contains(body, `id="reader-article-open"`) {
		t.Errorf("prefetch response carries the OOB pane-state checkbox:\n%s", body)
	}
	if strings.Contains(body, "reader-count") {
		t.Errorf("prefetch response carries OOB sidebar count spans:\n%s", body)
	}
	if strings.Contains(body, "hx-swap-oob") {
		t.Errorf("prefetch response carries out-of-band markup:\n%s", body)
	}
}

// A normal (non-prefetch) open is the control: it must still carry the OOB
// markup the prefetch response above must not have.
func TestNormalOpenStillCarriesOOBMarkup(t *testing.T) {
	s := newServer(t)
	_, items := seedOne(t, s, "g1")

	req := httptest.NewRequest(http.MethodGet, "/reader/item/"+itoa(items[0].ID), nil)
	req.Header.Set("HX-Request", "true")
	rec := s.Do(t, s.Alice, req)
	body := rec.Body.String()

	if !strings.Contains(body, `id="reader-article-open"`) {
		t.Errorf("normal open response has no OOB pane-state checkbox:\n%s", body)
	}
	if !strings.Contains(body, "reader-count") {
		t.Errorf("normal open response has no OOB count spans:\n%s", body)
	}
}

// ...and a normal open still does.
func TestNormalOpenStillMarksRead(t *testing.T) {
	s := newServer(t)
	ctx := context.Background()
	_, items := seedOne(t, s, "g1")

	s.Do(t, s.Alice, httptest.NewRequest(http.MethodGet, "/reader/item/"+itoa(items[0].ID), nil))

	read, _, err := s.Store.ItemState(ctx, s.Alice.User.ID, items[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if !read {
		t.Error("a normal open no longer marks the article read")
	}
}

func TestStatsPageRenders(t *testing.T) {
	s := newServer(t)
	ctx := context.Background()
	now := time.Now().UTC()

	sub, err := s.Store.Subscribe(ctx, s.Alice.User.ID, "https://a.example/feed.xml", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Store.SaveItems(ctx, sub.FeedID, []reader.ParsedItem{
		{GUID: "a", Title: "A", PublishedAt: now.Add(-time.Hour)},
		{GUID: "b", Title: "B", PublishedAt: now.Add(-2 * time.Hour)},
	}, now); err != nil {
		t.Fatal(err)
	}
	if err := s.Store.RecordDailyStats(ctx, now); err != nil {
		t.Fatal(err)
	}

	doc := s.Get(t, s.Alice, "/reader/stats")
	text := doc.Text()
	if !strings.Contains(text, "a.example") {
		t.Errorf("per-feed table missing:\n%s", text)
	}
	// The accessible view the charts lean on must be real markup, not an image.
	if doc.Query("table") == nil {
		t.Error("no table on the stats page; the charts have no accessible equivalent")
	}
	if len(doc.QueryAll("svg rect")) == 0 {
		t.Error("no chart bars rendered")
	}
}

func TestStatsPageIsPerUser(t *testing.T) {
	s := newServer(t)
	ctx := context.Background()

	if _, err := s.Store.Subscribe(ctx, s.Bob.User.ID, "https://bobs.example/feed.xml", nil); err != nil {
		t.Fatal(err)
	}
	doc := s.Get(t, s.Alice, "/reader/stats")
	if strings.Contains(doc.Text(), "bobs.example") {
		t.Errorf("alice's stats page shows bob's feed:\n%s", doc.Text())
	}
}

func TestStatsPageIsBehindAuth(t *testing.T) {
	s := newServer(t)
	rec := s.Do(t, s.Anonymous(t), httptest.NewRequest(http.MethodGet, "/reader/stats", nil))
	if rec.Code == http.StatusOK {
		t.Error("anonymous request got the stats page")
	}
}

// A brand-new account must get an empty state, not a division by zero or a
// chart with no bars and no explanation.
func TestStatsPageOnAnEmptyAccount(t *testing.T) {
	s := newServer(t)
	doc := s.Get(t, s.Alice, "/reader/stats")
	// Assert on the element, not on wording: a substring match for "no " would
	// pass on the word "normal" and fail the moment the copy is reworded.
	if doc.Query(".empty") == nil {
		t.Errorf("no empty state on a fresh account:\n%s", doc.Text())
	}
}
