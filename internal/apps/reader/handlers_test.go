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
