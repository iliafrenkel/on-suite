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
func TestArticleResponseCarriesTheOOBPaneState(t *testing.T) {
	s := newServer(t)
	_, items := seedOne(t, s, "g1")

	rec := s.Do(t, s.Alice, httptest.NewRequest(http.MethodGet, "/reader/item/"+itoa(items[0].ID), nil))
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
	for _, f := range forms {
		method, _ := htmlassert.Attr(f, "method")
		action, _ := htmlassert.Attr(f, "action")
		if !strings.EqualFold(method, "post") || action == "" {
			t.Errorf(`form has method=%q action=%q; without both it submits nowhere with JavaScript off`, method, action)
		}
		if hx, ok := htmlassert.Attr(f, "hx-post"); ok && hx != action {
			t.Errorf("form action %q and hx-post %q disagree", action, hx)
		}
	}
	if n := len(doc.QueryAll(`#reader-panes input[name=` + web.CSRFFormField + `]`)); n < len(forms) {
		t.Errorf("%d CSRF fields for %d forms; a plain submission would be rejected", n, len(forms))
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

// Reading an article changes an unread count, so the tree has to come back
// with the article or the sidebar keeps the pre-click numbers until some
// unrelated navigation reconciles them.
func TestArticleResponseRedrawsTheTreeCounts(t *testing.T) {
	s := newServer(t)
	subID, items := seedOne(t, s, "a", "b")

	rec := s.Do(t, s.Alice, httptest.NewRequest(http.MethodGet,
		"/reader/item/"+itoa(items[0].ID)+"?scope=feed&sub="+itoa(subID)+"&filter=unread", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("article returned %d: %s", rec.Code, rec.Body.String())
	}
	doc := htmlassert.Parse(t, rec.Body.String())

	tree := doc.MustHave("nav#reader-tree")
	if _, ok := htmlassert.Attr(tree, "hx-swap-oob"); !ok {
		t.Error("the tree rides along without hx-swap-oob, so htmx would discard it")
	}
	counts := doc.QueryAll(".reader-count")
	if len(counts) == 0 {
		t.Fatalf("no unread counts in the redrawn tree:\n%s", rec.Body.String())
	}
	for _, c := range counts {
		if got := strings.TrimSpace(htmlassert.Text(c)); got != "1" {
			t.Errorf("tree count = %q after reading one of two articles, want 1", got)
		}
	}
	if doc.Query("li.is-active") == nil {
		t.Error("the tree redraw lost the selected feed; the article link has to carry its list context")
	}
}

// The star form's own POST has to keep the tree in step too, and stay on the
// list the article was opened from.
func TestStarResponseRedrawsTheTreeForTheSameList(t *testing.T) {
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
	doc.MustHave("nav#reader-tree")
	if doc.Query("li.is-active") == nil {
		t.Error("starring redrew the tree with no selected feed; the form has to carry its list context")
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
