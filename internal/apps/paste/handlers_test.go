package paste_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/iliafrenkel/on-suite/internal/apps/paste"
	"github.com/iliafrenkel/on-suite/internal/apptest"
	"github.com/iliafrenkel/on-suite/internal/htmlassert"
	"github.com/iliafrenkel/on-suite/internal/platform/web"
)

// server is paste's own test harness, built on the shared apptest.Server —
// issue #50: this and internal/apps/notes' own handlers_test.go used to
// each carry an almost line-for-line duplicate of newServer/session/do/
// post/logIn. createSnippet and shareAndGetSlug below are paste-specific
// shortcuts that stay here, alongside the rest of what only this package's
// tests need.
type server struct {
	*apptest.Server[*paste.Store]
}

// session holds one signed-in browser's cookies.
type session = apptest.Session

func newServer(t *testing.T) *server {
	t.Helper()
	return &server{apptest.NewServer(t, paste.New(), paste.NewStore)}
}

// createSnippet is the shortcut other tests use.
func (s *server) createSnippet(t *testing.T, sess *session, title, language, body string) int64 {
	t.Helper()
	rec := s.Post(t, sess, "/paste/new", url.Values{
		"title": {title}, "language": {language}, "body": {body},
	})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("create = %d, want 303; body: %s", rec.Code, rec.Body.String())
	}
	loc := rec.Header().Get("Location")
	var id int64
	if _, err := fmtSscan(loc, &id); err != nil {
		t.Fatalf("cannot read an id out of Location %q: %v", loc, err)
	}
	return id
}

// fmtSscan pulls the trailing id out of "/paste/12".
func fmtSscan(location string, id *int64) (int, error) {
	idx := strings.LastIndex(location, "/")
	if idx < 0 {
		return 0, errNoID
	}
	var n int64
	for _, r := range location[idx+1:] {
		if r < '0' || r > '9' {
			return 0, errNoID
		}
		n = n*10 + int64(r-'0')
	}
	if n == 0 {
		return 0, errNoID
	}
	*id = n
	return 1, nil
}

var errNoID = errorString("no snippet id in the redirect")

type errorString string

func (e errorString) Error() string { return string(e) }

// ---- tests ----------------------------------------------------------------

// TestPasteRequiresSignIn confirms the default-deny router is doing its job for
// a real app, not just the fake one in the framework's own tests.
func TestPasteRequiresSignIn(t *testing.T) {
	s := newServer(t)
	for _, path := range []string{"/paste/", "/paste/new", "/paste/1", "/paste/raw/1"} {
		rec := s.Do(t, nil, httptest.NewRequest("GET", path, nil))
		if rec.Code != http.StatusSeeOther {
			t.Errorf("GET %s anonymous = %d, want a 303 to the login page", path, rec.Code)
		}
	}
}

func TestNewFormRendersInsideTheSplitView(t *testing.T) {
	s := newServer(t)
	rec := s.Do(t, s.Alice, httptest.NewRequest("GET", "/paste/new", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	doc := htmlassert.Parse(t, rec.Body.String())
	doc.MustHave(".paste-list-pane")
	doc.MustHave("#detail textarea[name=body]")
	doc.MustHave("#detail input[name=title]")
	doc.MustHave("#detail select[name=language]")
	doc.MustHave("input[name=" + web.CSRFFormField + "]")

	nav := doc.MustHave(`nav.shell-nav a[aria-current=page]`)
	if got := htmlassert.Text(nav); got != "ON Paste" {
		t.Errorf("the marked nav item is %q, want ON Paste", got)
	}
	if n := len(doc.QueryAll("select[name=language] option")); n < 10 {
		t.Errorf("the language picker has only %d options", n)
	}
}

func TestNewFormOverHTMXReturnsOnlyTheFragment(t *testing.T) {
	s := newServer(t)
	req := httptest.NewRequest("GET", "/paste/new", nil)
	req.Header.Set("HX-Request", "true")
	rec := s.Do(t, s.Alice, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	body := rec.Body.String()
	if strings.Contains(body, "shell-bar") {
		t.Error("an HTMX fragment response repeated the page shell")
	}
	htmlassert.Parse(t, "<html><body>"+body+"</body></html>").MustHave("textarea[name=body]")
}

// TestCreateOverHTMXUpdatesListAndDetailTogether: the new row must appear in
// the list at the same time the detail pane shows the new snippet.
func TestCreateOverHTMXUpdatesListAndDetailTogether(t *testing.T) {
	s := newServer(t)
	rec := s.PostHX(t, s.Alice, "/paste/new", url.Values{
		"title": {"Fresh"}, "language": {"go"}, "body": {"package c\n"},
	})

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, "Fresh") {
		t.Error("the new snippet is not in the detail response")
	}
	if !strings.Contains(body, `hx-swap-oob="true"`) {
		t.Error("the list was not refreshed out of band")
	}
}

// TestCreateValidationFailureOverHTMXReturns200: htmx's default
// responseHandling config does not swap 4xx responses into the DOM, so a
// validation failure rendered at 400 over HTMX would silently vanish
// instead of showing the user why Save did nothing. The fragment must come
// back at 200 so htmx actually swaps the re-populated form and its error.
func TestCreateValidationFailureOverHTMXReturns200(t *testing.T) {
	s := newServer(t)
	rec := s.PostHX(t, s.Alice, "/paste/new", url.Values{
		"title": {"t"}, "language": {"go"}, "body": {""},
	})

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 so htmx will swap the error in", rec.Code)
	}
	doc := htmlassert.Parse(t, "<html><body>"+rec.Body.String()+"</body></html>")
	doc.MustHave(".notice-error")
}

// TestUpdateValidationFailureOverHTMXReturns200 is TestCreateValidationFailureOverHTMXReturns200's
// counterpart for the edit form.
func TestUpdateValidationFailureOverHTMXReturns200(t *testing.T) {
	s := newServer(t)
	id := s.createSnippet(t, s.Alice, "Original", "go", "package a\n")

	rec := s.PostHX(t, s.Alice, "/paste/"+itoa(id), url.Values{
		"title": {"Original"}, "language": {"go"}, "body": {"   \n"},
	})

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 so htmx will swap the error in", rec.Code)
	}
	doc := htmlassert.Parse(t, "<html><body>"+rec.Body.String()+"</body></html>")
	doc.MustHave(".notice-error")
}

// TestCreateOverHTMXPushesTheNewURL: the design requires /paste/{id} to be
// bookmarkable after any action. Without HX-Push-Url, the address bar stays
// at /paste/new after a create over HTMX, and reloading throws the user
// back to a blank form.
func TestCreateOverHTMXPushesTheNewURL(t *testing.T) {
	s := newServer(t)
	rec := s.PostHX(t, s.Alice, "/paste/new", url.Values{
		"title": {"Fresh"}, "language": {"go"}, "body": {"package c\n"},
	})

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body: %s", rec.Code, rec.Body.String())
	}
	push := rec.Header().Get("HX-Push-Url")
	if push == "" || push == "/paste/new" || !strings.HasPrefix(push, "/paste/") {
		t.Errorf("HX-Push-Url = %q, want /paste/{new-id}", push)
	}
	if push == "/paste/" {
		t.Errorf("HX-Push-Url = %q, want it to include the new snippet's id", push)
	}
}

func TestCreateThenView(t *testing.T) {
	s := newServer(t)
	id := s.createSnippet(t, s.Alice, "My config", "yaml", "key: value\nother: 2\n")

	rec := s.Do(t, s.Alice, httptest.NewRequest("GET", "/paste/"+itoa(id), nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("view = %d", rec.Code)
	}
	doc := htmlassert.Parse(t, rec.Body.String())

	if got := htmlassert.Text(doc.MustHave("h1")); got != "My config" {
		t.Errorf("title = %q", got)
	}
	if text := doc.Text(); !strings.Contains(text, "key") {
		t.Errorf("the body is not on the page: %q", text)
	}
	// Highlighted, and via classes rather than inline styles.
	doc.MustHave(".chroma")
	if strings.Contains(rec.Body.String(), "style=") {
		t.Error("the page contains an inline style attribute, which the CSP blocks")
	}
	// The stylesheet the highlighting depends on must be linked.
	if href, _ := htmlassert.Attr(doc.MustHave("link[href=/paste/highlight.css]"), "href"); href == "" {
		t.Error("the highlight stylesheet is not linked")
	}
	// A private snippet offers sharing and shows no link yet.
	doc.MustHave(`form[action=/paste/` + itoa(id) + `/share]`)
	if strings.Contains(doc.Text(), "Anyone with this link") {
		t.Error("a private snippet advertises a share link")
	}
}

func TestIndexShowsListAndEmptyDetail(t *testing.T) {
	s := newServer(t)
	s.createSnippet(t, s.Alice, "First", "go", "package a\n")
	s.createSnippet(t, s.Alice, "Second", "python", "print(1)\n")

	rec := s.Do(t, s.Alice, httptest.NewRequest("GET", "/paste/", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	doc := htmlassert.Parse(t, rec.Body.String())
	rows := doc.QueryAll(".snippet-row")
	if len(rows) != 2 {
		t.Fatalf("got %d snippet rows, want 2", len(rows))
	}
	if doc.Query(".snippet-row-active") != nil {
		t.Error("nothing is selected, but a row is marked active")
	}
	doc.MustHave(".paste-detail-empty")
}

func TestIndexSelectsSnippetAndMarksActiveRow(t *testing.T) {
	s := newServer(t)
	id := s.createSnippet(t, s.Alice, "My config", "yaml", "key: value\n")
	s.createSnippet(t, s.Alice, "Other", "go", "package b\n")

	rec := s.Do(t, s.Alice, httptest.NewRequest("GET", "/paste/"+itoa(id), nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	doc := htmlassert.Parse(t, rec.Body.String())
	if got := htmlassert.Text(doc.MustHave("h1")); got != "My config" {
		t.Errorf("title = %q", got)
	}
	doc.MustHave(".chroma")

	active := doc.MustHave(".snippet-row-active")
	if !strings.Contains(htmlassert.Text(active), "My config") {
		t.Errorf("the active row is not the selected snippet: %q", htmlassert.Text(active))
	}
	// It is still one page: the list must be present alongside the detail.
	if len(doc.QueryAll(".snippet-row")) != 2 {
		t.Error("the list pane is missing on the detail page")
	}
}

// TestSelectingOverHTMXReturnsOnlyTheFragment: a fragment response must not
// repeat the shell (nav, sidebar) — only what belongs inside #detail.
func TestSelectingOverHTMXReturnsOnlyTheFragment(t *testing.T) {
	s := newServer(t)
	id := s.createSnippet(t, s.Alice, "My config", "yaml", "key: value\n")

	req := httptest.NewRequest("GET", "/paste/"+itoa(id), nil)
	req.Header.Set("HX-Request", "true")
	rec := s.Do(t, s.Alice, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	body := rec.Body.String()
	if strings.Contains(body, "shell-bar") || strings.Contains(body, "app-sidebar") {
		t.Error("an HTMX fragment response repeated the page shell")
	}
	if !strings.Contains(body, "My config") {
		t.Error("the fragment does not contain the snippet")
	}
}

// TestSelectingOverHTMXRefreshesTheListsActiveRow: selecting a second snippet
// over HTMX must ride the list along out of band, so the active row moves off
// the first snippet and onto the second one — otherwise the list still shows
// the previous selection until the next full page load.
func TestSelectingOverHTMXRefreshesTheListsActiveRow(t *testing.T) {
	s := newServer(t)
	firstID := s.createSnippet(t, s.Alice, "First", "go", "package a\n")
	secondID := s.createSnippet(t, s.Alice, "Second", "python", "print(1)\n")

	// Select the first snippet.
	req := httptest.NewRequest("GET", "/paste/"+itoa(firstID), nil)
	req.Header.Set("HX-Request", "true")
	rec := s.Do(t, s.Alice, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("selecting the first snippet = %d", rec.Code)
	}

	// Now select the second snippet over HTMX.
	req = httptest.NewRequest("GET", "/paste/"+itoa(secondID), nil)
	req.Header.Set("HX-Request", "true")
	rec = s.Do(t, s.Alice, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("selecting the second snippet = %d", rec.Code)
	}

	body := rec.Body.String()
	if !strings.Contains(body, `hx-swap-oob="true"`) {
		t.Fatalf("the response does not carry the list out of band: %s", body)
	}

	doc := htmlassert.Parse(t, "<html><body>"+body+"</body></html>")
	active := doc.MustHave(".snippet-row-active")
	if got := htmlassert.Text(active); !strings.Contains(got, "Second") {
		t.Errorf("the active row is %q, want it to be Second", got)
	}
	if strings.Contains(htmlassert.Text(active), "First") {
		t.Error("the active row is still First")
	}
	rows := doc.QueryAll(".snippet-row-active")
	if len(rows) != 1 {
		t.Fatalf("got %d active rows, want exactly 1", len(rows))
	}
}

func TestIndexingSomeoneElsesSnippetIs404(t *testing.T) {
	s := newServer(t)
	id := s.createSnippet(t, s.Alice, "alice's", "go", "secret\n")

	rec := s.Do(t, s.Bob, httptest.NewRequest("GET", "/paste/"+itoa(id), nil))
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "secret") {
		t.Error("the snippet body leaked to another user")
	}
}

// TestViewingSomeoneElsesSnippetIs404 — not a 403, which would confirm it
// exists.
func TestViewingSomeoneElsesSnippetIs404(t *testing.T) {
	s := newServer(t)
	id := s.createSnippet(t, s.Alice, "alice's", "go", "secret\n")

	rec := s.Do(t, s.Bob, httptest.NewRequest("GET", "/paste/"+itoa(id), nil))
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "secret") {
		t.Error("the snippet body leaked to another user")
	}
}

func TestCreateRejectsBadInput(t *testing.T) {
	s := newServer(t)

	tests := []struct {
		name string
		form url.Values
	}{
		{"empty body", url.Values{"title": {"t"}, "language": {"go"}, "body": {""}}},
		{"whitespace body", url.Values{"title": {"t"}, "language": {"go"}, "body": {"  \n"}}},
		{"unknown language", url.Values{"title": {"t"}, "language": {"klingon"}, "body": {"x\n"}}},
		{"oversized title", url.Values{"title": {strings.Repeat("a", paste.MaxTitleRunes+1)}, "language": {"go"}, "body": {"x\n"}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := s.Post(t, s.Alice, "/paste/new", tt.form)
			if rec.Code == http.StatusSeeOther {
				t.Fatal("the snippet was created")
			}
			doc := htmlassert.Parse(t, rec.Body.String())
			doc.MustHave(".notice-error")
			// The form must come back populated, or the user loses their work.
			doc.MustHave("textarea[name=body]")
		})
	}
}

// TestCreatePreservesTypedInputOnFailure: losing a long snippet to a
// validation error would be the single most annoying possible bug here.
func TestCreatePreservesTypedInputOnFailure(t *testing.T) {
	s := newServer(t)
	const body = "a long snippet the user does not want to retype\n"

	rec := s.Post(t, s.Alice, "/paste/new", url.Values{
		"title":    {strings.Repeat("a", paste.MaxTitleRunes+1)},
		"language": {"go"}, "body": {body},
	})
	doc := htmlassert.Parse(t, rec.Body.String())
	if got := htmlassert.Text(doc.MustHave("textarea[name=body]")); !strings.Contains(got, "does not want to retype") {
		t.Errorf("the body was not carried back: %q", got)
	}
}

func TestCreateRequiresCSRF(t *testing.T) {
	s := newServer(t)
	form := url.Values{"title": {"t"}, "language": {"go"}, "body": {"x\n"}}
	req := httptest.NewRequest("POST", "/paste/new", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	rec := s.Do(t, s.Alice, req) // no token in the form
	if rec.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403", rec.Code)
	}
}

func TestViewRejectsNonNumericAndMissingIDs(t *testing.T) {
	s := newServer(t)
	for _, path := range []string{"/paste/abc", "/paste/0", "/paste/-1", "/paste/99999"} {
		rec := s.Do(t, s.Alice, httptest.NewRequest("GET", path, nil))
		if rec.Code != http.StatusNotFound {
			t.Errorf("GET %s = %d, want 404", path, rec.Code)
		}
	}
}

// TestSnippetBodyIsEscapedInTheView is the XSS test. A snippet is arbitrary
// text from a user and is rendered through template.HTML, so this must hold.
func TestSnippetBodyIsEscapedInTheView(t *testing.T) {
	s := newServer(t)
	const hostile = "<script>alert('xss')</script>\n"
	id := s.createSnippet(t, s.Alice, "<img src=x onerror=alert(1)>", "html", hostile)

	rec := s.Do(t, s.Alice, httptest.NewRequest("GET", "/paste/"+itoa(id), nil))
	body := rec.Body.String()

	if strings.Contains(body, "<script>alert('xss')</script>") {
		t.Fatal("the snippet body was rendered as live markup")
	}
	// Assert on the tag rather than the payload: html/template escapes the
	// angle brackets, so the inner text survives as inert text and looking for
	// it would fail on correct output.
	if strings.Contains(body, "<img src=x") {
		t.Fatal("the title was rendered as live markup")
	}
	// It must still be visible as text.
	if !strings.Contains(htmlassert.Parse(t, body).Text(), "alert('xss')") {
		t.Error("the escaped snippet disappeared from the page")
	}
}

func TestHighlightStylesheetIsServedAndCacheable(t *testing.T) {
	s := newServer(t)

	// Public: it must load on the shared page, where nobody is signed in.
	rec := s.Do(t, nil, httptest.NewRequest("GET", "/paste/highlight.css", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/css") {
		t.Errorf("Content-Type = %q", ct)
	}
	if !strings.Contains(rec.Body.String(), ".chroma") {
		t.Error("the stylesheet has no .chroma rules")
	}

	etag := rec.Header().Get("ETag")
	if etag == "" {
		t.Fatal("no ETag")
	}
	req := httptest.NewRequest("GET", "/paste/highlight.css", nil)
	req.Header.Set("If-None-Match", etag)
	rec = s.Do(t, nil, req)
	if rec.Code != http.StatusNotModified {
		t.Errorf("revalidation = %d, want 304", rec.Code)
	}
}

func TestListShowsOnlyYourSnippetsNewestFirst(t *testing.T) {
	s := newServer(t)

	s.createSnippet(t, s.Alice, "alice one", "go", "package one\n")
	s.createSnippet(t, s.Alice, "alice two", "go", "package two\n")
	s.createSnippet(t, s.Bob, "bob's secret", "go", "package bob\n")

	rec := s.Do(t, s.Alice, httptest.NewRequest("GET", "/paste/", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	doc := htmlassert.Parse(t, rec.Body.String())

	// The split-view list wraps each row's whole title/preview/meta block in
	// one .snippet-row link now (index.html's "list-items" block), so the
	// row's title is only a prefix of its text rather than all of it.
	rows := doc.QueryAll(".snippet-row")
	if len(rows) != 2 {
		t.Fatalf("the list has %d entries, want 2 (alice's only)", len(rows))
	}
	if got := htmlassert.Text(rows[0]); !strings.HasPrefix(got, "alice two") {
		t.Errorf("first entry = %q, want the newest", got)
	}
	if text := doc.Text(); strings.Contains(text, "bob") {
		t.Error("another user's snippet appeared in the list")
	}
}

func TestListWhenEmpty(t *testing.T) {
	s := newServer(t)
	rec := s.Do(t, s.Alice, httptest.NewRequest("GET", "/paste/", nil))

	doc := htmlassert.Parse(t, rec.Body.String())
	// The split-view list always renders its <ul> (index.html's "list-items"
	// block falls back to a single "No snippets yet" <li> instead of omitting
	// the list), so assert on the absence of rows rather than the <ul> itself.
	if rows := doc.QueryAll(".snippet-row"); len(rows) != 0 {
		t.Errorf("got %d snippet rows, want 0", len(rows))
	}
	if !strings.Contains(doc.Text(), "No snippets yet") {
		t.Errorf("no empty-state message: %q", doc.Text())
	}
	// The way to get started must still be offered.
	doc.MustHave(`a[href=/paste/new]`)
}

// TestListPreviewIsOneLine: a snippet is arbitrarily tall, and a list row
// must not be.
func TestListPreviewIsOneLine(t *testing.T) {
	s := newServer(t)
	s.createSnippet(t, s.Alice, "tall", "plaintext",
		strings.Repeat("a line of text that goes on\n", 40))

	rec := s.Do(t, s.Alice, httptest.NewRequest("GET", "/paste/", nil))
	doc := htmlassert.Parse(t, rec.Body.String())

	got := htmlassert.Text(doc.MustHave(".snippet-preview"))
	if strings.Contains(got, "\n") {
		t.Error("the preview contains a newline")
	}
	if len([]rune(got)) > 120 {
		t.Errorf("the preview is %d characters, which is too long: %q", len([]rune(got)), got)
	}
	if !strings.HasSuffix(got, "…") {
		t.Errorf("a truncated preview should be marked with an ellipsis: %q", got)
	}
}

// TestDeleteHandler is TestDelete's HTTP-level counterpart: store_test.go
// already covers Store.Delete directly, so this one is named to avoid
// colliding with it while exercising the handler's redirect and status code.
func TestDeleteHandler(t *testing.T) {
	s := newServer(t)
	id := s.createSnippet(t, s.Alice, "doomed", "go", "package main\n")

	rec := s.Post(t, s.Alice, "/paste/"+itoa(id)+"/delete", url.Values{})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("delete = %d, want 303", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "/paste/" {
		t.Errorf("Location = %q, want /paste/", loc)
	}

	rec = s.Do(t, s.Alice, httptest.NewRequest("GET", "/paste/"+itoa(id), nil))
	if rec.Code != http.StatusNotFound {
		t.Errorf("the snippet is still viewable: %d", rec.Code)
	}
}

// TestDeleteSomeoneElsesSnippetFails is the one that would matter most if it
// regressed.
func TestDeleteSomeoneElsesSnippetFails(t *testing.T) {
	s := newServer(t)
	id := s.createSnippet(t, s.Alice, "alice's", "go", "package main\n")

	rec := s.Post(t, s.Bob, "/paste/"+itoa(id)+"/delete", url.Values{})
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}

	// And it is genuinely still there.
	rec = s.Do(t, s.Alice, httptest.NewRequest("GET", "/paste/"+itoa(id), nil))
	if rec.Code != http.StatusOK {
		t.Error("the owner's snippet was destroyed by another user's request")
	}
}

func TestDeleteRequiresCSRFAndPOST(t *testing.T) {
	s := newServer(t)
	id := s.createSnippet(t, s.Alice, "safe", "go", "package main\n")

	// No token.
	req := httptest.NewRequest("POST", "/paste/"+itoa(id)+"/delete", nil)
	if rec := s.Do(t, s.Alice, req); rec.Code != http.StatusForbidden {
		t.Errorf("delete without a CSRF token = %d, want 403", rec.Code)
	}

	// A GET must not delete: the route is POST-only, so ServeMux refuses it.
	rec := s.Do(t, s.Alice, httptest.NewRequest("GET", "/paste/"+itoa(id)+"/delete", nil))
	if rec.Code == http.StatusSeeOther {
		t.Error("a GET performed the deletion")
	}

	if rec := s.Do(t, s.Alice, httptest.NewRequest("GET", "/paste/"+itoa(id), nil)); rec.Code != http.StatusOK {
		t.Error("the snippet was deleted by a request that should have been refused")
	}
}

// shareAndGetSlug shares a snippet and returns its public slug, read back from
// the store so the test does not have to scrape it out of the page.
func (s *server) shareAndGetSlug(t *testing.T, sess *session, id int64) string {
	t.Helper()

	rec := s.Post(t, sess, "/paste/"+itoa(id)+"/share", url.Values{})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("share = %d, want 303", rec.Code)
	}
	snippet, err := s.Store.ByID(context.Background(), sess.User.ID, id)
	if err != nil {
		t.Fatal(err)
	}
	if !snippet.Shared() {
		t.Fatal("the snippet is not shared after sharing it")
	}
	return snippet.ShareSlug
}

func TestSharedSnippetIsReadableWhileSignedOut(t *testing.T) {
	s := newServer(t)
	id := s.createSnippet(t, s.Alice, "Shared config", "yaml", "key: value\n")
	slug := s.shareAndGetSlug(t, s.Alice, id)

	// nil session: no cookies at all.
	rec := s.Do(t, nil, httptest.NewRequest("GET", "/paste/s/"+slug, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("anonymous shared view = %d, want 200", rec.Code)
	}
	doc := htmlassert.Parse(t, rec.Body.String())

	if got := htmlassert.Text(doc.MustHave("h1")); got != "Shared config" {
		t.Errorf("title = %q", got)
	}
	if !strings.Contains(doc.Text(), "key") {
		t.Error("the snippet body is not on the shared page")
	}
	doc.MustHave(".chroma") // highlighted for anonymous readers too
}

// TestSharedEndpointsAreNotCachedOrIndexed guards a revocable credential: a
// share slug that a cache or crawler holds onto after the link is revoked
// would defeat the revocation.
func TestSharedEndpointsAreNotCachedOrIndexed(t *testing.T) {
	s := newServer(t)
	id := s.createSnippet(t, s.Alice, "Shared config", "yaml", "key: value\n")
	slug := s.shareAndGetSlug(t, s.Alice, id)

	cases := []struct {
		name string
		path string
	}{
		{"view", "/paste/s/" + slug},
		{"raw", "/paste/s/" + slug + "/raw"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := s.Do(t, nil, httptest.NewRequest("GET", tc.path, nil))
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d", rec.Code)
			}
			if cc := rec.Header().Get("Cache-Control"); cc != "no-store" {
				t.Errorf("Cache-Control = %q, want no-store", cc)
			}
			if rt := rec.Header().Get("X-Robots-Tag"); rt != "noindex" {
				t.Errorf("X-Robots-Tag = %q, want noindex", rt)
			}
		})
	}
}

// TestSharedPageOffersNoDestructiveControls is why the shared view has its own
// template rather than reusing the owner's with conditionals.
func TestSharedPageOffersNoDestructiveControls(t *testing.T) {
	s := newServer(t)
	id := s.createSnippet(t, s.Alice, "Shared", "go", "package main\n")
	slug := s.shareAndGetSlug(t, s.Alice, id)

	rec := s.Do(t, nil, httptest.NewRequest("GET", "/paste/s/"+slug, nil))
	doc := htmlassert.Parse(t, rec.Body.String())

	doc.MustNotHave(`form[action=/paste/` + itoa(id) + `/delete]`)
	doc.MustNotHave(`form[action=/paste/` + itoa(id) + `/unshare]`)
	doc.MustNotHave(".shell-user") // nobody is signed in
	for _, word := range []string{"Delete", "Stop sharing"} {
		if strings.Contains(doc.Text(), word) {
			t.Errorf("the shared page offers %q", word)
		}
	}
}

// TestSharedPageBreadcrumbHasNoLoginGatedLink guards the same invariant as
// notes' TestSharedPageHasNoLinksIntoThePrivateTree: a signed-out visitor on
// a public page must not get a link back into an app's login-gated index
// (here, "Paste" in the breadcrumb pointing at /paste/).
func TestSharedPageBreadcrumbHasNoLoginGatedLink(t *testing.T) {
	s := newServer(t)
	id := s.createSnippet(t, s.Alice, "Shared config", "yaml", "key: value\n")
	slug := s.shareAndGetSlug(t, s.Alice, id)

	rec := s.Do(t, nil, httptest.NewRequest("GET", "/paste/s/"+slug, nil))
	doc := htmlassert.Parse(t, rec.Body.String())
	for _, a := range doc.QueryAll("a") {
		href, _ := htmlassert.Attr(a, "href")
		if href == "/paste/" {
			t.Errorf("the shared page's breadcrumb links to %q, the login-gated index", href)
		}
	}
}

// TestUnshareKillsTheLink is the point of choosing a revocable share model.
func TestUnshareKillsTheLink(t *testing.T) {
	s := newServer(t)
	id := s.createSnippet(t, s.Alice, "Shared", "go", "package main\n")
	slug := s.shareAndGetSlug(t, s.Alice, id)

	if rec := s.Do(t, nil, httptest.NewRequest("GET", "/paste/s/"+slug, nil)); rec.Code != http.StatusOK {
		t.Fatal("the link did not work before revoking it")
	}

	rec := s.Post(t, s.Alice, "/paste/"+itoa(id)+"/unshare", url.Values{})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("unshare = %d", rec.Code)
	}

	if rec := s.Do(t, nil, httptest.NewRequest("GET", "/paste/s/"+slug, nil)); rec.Code != http.StatusNotFound {
		t.Errorf("the revoked link still works: %d", rec.Code)
	}
	if rec := s.Do(t, nil, httptest.NewRequest("GET", "/paste/s/"+slug+"/raw", nil)); rec.Code != http.StatusNotFound {
		t.Errorf("the revoked raw link still works: %d", rec.Code)
	}

	// Re-sharing must produce a different link, leaving the old one dead.
	second := s.shareAndGetSlug(t, s.Alice, id)
	if second == slug {
		t.Fatal("re-sharing reissued the revoked slug")
	}
	if rec := s.Do(t, nil, httptest.NewRequest("GET", "/paste/s/"+slug, nil)); rec.Code != http.StatusNotFound {
		t.Error("the old link came back to life after re-sharing")
	}
}

func TestUnsharedSnippetIsNotPubliclyReachable(t *testing.T) {
	s := newServer(t)
	id := s.createSnippet(t, s.Alice, "Private", "go", "top secret\n")

	// The owner's own URL must not work for an anonymous visitor.
	rec := s.Do(t, nil, httptest.NewRequest("GET", "/paste/"+itoa(id), nil))
	if rec.Code != http.StatusSeeOther {
		t.Errorf("anonymous access to a private snippet = %d, want a redirect to login", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "top secret") {
		t.Fatal("a private snippet leaked to an anonymous request")
	}

	// And a guessed share URL must not either.
	for _, slug := range []string{itoa(id), "aaaaaaaaaaaaaaaaaaaaaa", ""} {
		rec := s.Do(t, nil, httptest.NewRequest("GET", "/paste/s/"+slug, nil))
		if rec.Code == http.StatusOK {
			t.Errorf("GET /paste/s/%q returned 200", slug)
		}
	}
}

func TestShareStateIsShownToTheOwner(t *testing.T) {
	s := newServer(t)
	id := s.createSnippet(t, s.Alice, "Shared", "go", "package main\n")
	slug := s.shareAndGetSlug(t, s.Alice, id)

	rec := s.Do(t, s.Alice, httptest.NewRequest("GET", "/paste/"+itoa(id), nil))
	doc := htmlassert.Parse(t, rec.Body.String())

	// The link is shown as a real, clickable link with a copy button, and
	// the control now revokes.
	link := doc.MustHave(".notice a")
	if got := htmlassert.Text(link); !strings.Contains(got, slug) {
		t.Errorf("the share link is not displayed; got %q", got)
	}
	if href, _ := htmlassert.Attr(link, "href"); !strings.Contains(href, slug) {
		t.Errorf("the share link's href = %q, want it to contain %q", href, slug)
	}
	if copyPath, _ := htmlassert.Attr(doc.MustHave("[data-copy-link]"), "data-copy-link"); !strings.Contains(copyPath, slug) {
		t.Errorf("the copy button's data-copy-link = %q, want it to contain %q", copyPath, slug)
	}
	doc.MustHave(`form[action=/paste/` + itoa(id) + `/unshare]`)
	doc.MustNotHave(`form[action=/paste/` + itoa(id) + `/share]`)
}

func TestRawIsPlainTextForOwnerAndShared(t *testing.T) {
	s := newServer(t)
	const body = "line one\nline two\n"
	// "plaintext" is the offered language value for plain text; "text" is not
	// on the list and IsLanguage would reject it, so createSnippet would 400
	// instead of storing the snippet.
	id := s.createSnippet(t, s.Alice, "Raw", "plaintext", body)
	slug := s.shareAndGetSlug(t, s.Alice, id)

	cases := []struct {
		name    string
		path    string
		session *session
	}{
		{"owner", "/paste/raw/" + itoa(id), s.Alice},
		{"shared, signed out", "/paste/s/" + slug + "/raw", nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := s.Do(t, tc.session, httptest.NewRequest("GET", tc.path, nil))
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d", rec.Code)
			}
			if got := rec.Body.String(); got != body {
				t.Errorf("body = %q, want the snippet verbatim %q", got, body)
			}
			if ct := rec.Header().Get("Content-Type"); ct != "text/plain; charset=utf-8" {
				t.Errorf("Content-Type = %q, want text/plain; charset=utf-8", ct)
			}
			wantDisposition := `inline; filename="paste-` + itoa(id) + `.txt"`
			if cd := rec.Header().Get("Content-Disposition"); cd != wantDisposition {
				t.Errorf("Content-Disposition = %q, want %q", cd, wantDisposition)
			}
			// No HTML anywhere: this is the response a terminal consumes.
			if strings.Contains(rec.Body.String(), "<html") {
				t.Error("the raw response contains markup")
			}
		})
	}
}

// TestRawOfHTMLIsNotServedAsHTML: a snippet containing a page must not become
// one on this origin.
func TestRawOfHTMLIsNotServedAsHTML(t *testing.T) {
	s := newServer(t)
	id := s.createSnippet(t, s.Alice, "evil", "html",
		"<html><body><script>alert(1)</script></body></html>\n")

	rec := s.Do(t, s.Alice, httptest.NewRequest("GET", "/paste/raw/"+itoa(id), nil))
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/plain") {
		t.Fatalf("Content-Type = %q; the browser could render this as a page", ct)
	}
	// The body is verbatim on purpose — nosniff plus text/plain is what makes
	// that safe, so assert the header rather than mangling the content.
	if !strings.Contains(rec.Body.String(), "<script>") {
		t.Error("the raw response altered the snippet")
	}
}

func TestRawOfAnotherUsersSnippetIs404(t *testing.T) {
	s := newServer(t)
	id := s.createSnippet(t, s.Alice, "alice's", "go", "secret\n")

	rec := s.Do(t, s.Bob, httptest.NewRequest("GET", "/paste/raw/"+itoa(id), nil))
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "secret") {
		t.Error("the snippet leaked")
	}
}

func TestShareRequiresCSRF(t *testing.T) {
	s := newServer(t)
	id := s.createSnippet(t, s.Alice, "t", "go", "package main\n")

	for _, action := range []string{"share", "unshare"} {
		req := httptest.NewRequest("POST", "/paste/"+itoa(id)+"/"+action, nil)
		if rec := s.Do(t, s.Alice, req); rec.Code != http.StatusForbidden {
			t.Errorf("%s without a CSRF token = %d, want 403", action, rec.Code)
		}
	}
}

// TestPublicSurfaceIsExactlyThreeRoutes is the backstop on the whole sharing
// design. If a future change makes a fourth path anonymous, this fails first.
func TestPublicSurfaceIsExactlyThreeRoutes(t *testing.T) {
	s := newServer(t)
	id := s.createSnippet(t, s.Alice, "shared", "go", "package main\n")
	slug := s.shareAndGetSlug(t, s.Alice, id)

	reachable := []string{
		"/paste/highlight.css",
		"/paste/s/" + slug,
		"/paste/s/" + slug + "/raw",
	}
	blocked := []string{
		"/paste/",
		"/paste/new",
		"/paste/" + itoa(id),
		"/paste/raw/" + itoa(id),
	}

	for _, path := range reachable {
		if rec := s.Do(t, nil, httptest.NewRequest("GET", path, nil)); rec.Code != http.StatusOK {
			t.Errorf("public path %s = %d, want 200", path, rec.Code)
		}
	}
	for _, path := range blocked {
		if rec := s.Do(t, nil, httptest.NewRequest("GET", path, nil)); rec.Code == http.StatusOK {
			t.Errorf("%s is reachable anonymously and must not be", path)
		}
	}
}

func TestEditFormRendersExistingValues(t *testing.T) {
	s := newServer(t)
	id := s.createSnippet(t, s.Alice, "My config", "yaml", "key: value\n")

	rec := s.Do(t, s.Alice, httptest.NewRequest("GET", "/paste/edit/"+itoa(id), nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	doc := htmlassert.Parse(t, rec.Body.String())
	if got, _ := htmlassert.Attr(doc.MustHave("input[name=title]"), "value"); got != "My config" {
		t.Errorf("title value = %q", got)
	}
	if got := htmlassert.Text(doc.MustHave("textarea[name=body]")); !strings.Contains(got, "key: value") {
		t.Errorf("body = %q", got)
	}
	// htmlassert's selector syntax does not chain two bracket qualifiers
	// (`[value=yaml][selected]` is not supported), so this checks the value
	// match and the boolean attribute as two separate steps.
	if _, ok := htmlassert.Attr(doc.MustHave("option[value=yaml]"), "selected"); !ok {
		t.Error("the current language is not selected")
	}
}

func TestEditingSomeoneElsesSnippetIs404(t *testing.T) {
	s := newServer(t)
	id := s.createSnippet(t, s.Alice, "alice's", "go", "secret\n")

	rec := s.Do(t, s.Bob, httptest.NewRequest("GET", "/paste/edit/"+itoa(id), nil))
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
}

func TestSaveEditUpdatesSnippetAndListRow(t *testing.T) {
	s := newServer(t)
	id := s.createSnippet(t, s.Alice, "Original", "go", "package a\n")

	rec := s.Post(t, s.Alice, "/paste/"+itoa(id), url.Values{
		"title": {"Renamed"}, "language": {"python"}, "body": {"print(1)\n"},
	})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303; body: %s", rec.Code, rec.Body.String())
	}

	rec2 := s.Do(t, s.Alice, httptest.NewRequest("GET", "/paste/"+itoa(id), nil))
	doc := htmlassert.Parse(t, rec2.Body.String())
	if got := htmlassert.Text(doc.MustHave("h1")); got != "Renamed" {
		t.Errorf("title = %q", got)
	}
	if !strings.Contains(doc.Text(), "print(1)") {
		t.Error("the body was not saved")
	}
}

func TestSaveEditRejectsBadInputAndStaysInEditMode(t *testing.T) {
	s := newServer(t)
	id := s.createSnippet(t, s.Alice, "Original", "go", "package a\n")

	rec := s.Post(t, s.Alice, "/paste/"+itoa(id), url.Values{
		"title": {"Original"}, "language": {"go"}, "body": {"   \n"},
	})
	if rec.Code == http.StatusSeeOther {
		t.Fatal("an empty body was accepted")
	}
	doc := htmlassert.Parse(t, rec.Body.String())
	doc.MustHave(".notice-error")
	doc.MustHave("textarea[name=body]")
}

func TestSaveEditRejectsSomeoneElsesSnippet(t *testing.T) {
	s := newServer(t)
	id := s.createSnippet(t, s.Alice, "alice's", "go", "secret\n")

	rec := s.Post(t, s.Bob, "/paste/"+itoa(id), url.Values{
		"title": {"hijacked"}, "language": {"go"}, "body": {"x\n"},
	})
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
}

func TestShareOverHTMXUpdatesDetailAndListTag(t *testing.T) {
	s := newServer(t)
	id := s.createSnippet(t, s.Alice, "My config", "yaml", "key: value\n")

	rec := s.PostHX(t, s.Alice, "/paste/"+itoa(id)+"/share", url.Values{})

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, "Stop sharing") {
		t.Error("the detail pane does not reflect the new shared state")
	}
	if !strings.Contains(body, `hx-swap-oob="true"`) || !strings.Contains(body, "shared") {
		t.Error("the list's \"shared\" tag was not refreshed")
	}
}

// TestUnshareOverHTMXUpdatesDetailAndListTag is unshare's counterpart to
// TestShareOverHTMXUpdatesDetailAndListTag: the same renderDetailWithList
// path, but nothing previously asserted the list's "shared" tag actually
// disappears once a snippet is unshared over HTMX.
func TestUnshareOverHTMXUpdatesDetailAndListTag(t *testing.T) {
	s := newServer(t)
	id := s.createSnippet(t, s.Alice, "My config", "yaml", "key: value\n")
	s.shareAndGetSlug(t, s.Alice, id)

	rec := s.PostHX(t, s.Alice, "/paste/"+itoa(id)+"/unshare", url.Values{})

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if strings.Contains(body, "Stop sharing") {
		t.Error("the detail pane still reflects the shared state")
	}
	if !strings.Contains(body, `hx-swap-oob="true"`) || strings.Contains(body, "shared") {
		t.Error("the list's \"shared\" tag was not cleared")
	}
}

func TestDeleteOverHTMXClearsDetailAndRemovesRow(t *testing.T) {
	s := newServer(t)
	id := s.createSnippet(t, s.Alice, "Doomed", "go", "package a\n")

	rec := s.PostHX(t, s.Alice, "/paste/"+itoa(id)+"/delete", url.Values{})

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if strings.Contains(body, "Doomed") {
		t.Error("the deleted snippet is still in the response")
	}
	doc := htmlassert.Parse(t, "<html><body>"+body+"</body></html>")
	doc.MustHave(".paste-detail-empty")
}

// TestDeleteOverHTMXPushesTheListURL: the browser was at /paste/{id}, but
// that snippet no longer exists after the delete, so without HX-Push-Url a
// reload would 404 instead of landing back on the list.
func TestDeleteOverHTMXPushesTheListURL(t *testing.T) {
	s := newServer(t)
	id := s.createSnippet(t, s.Alice, "Doomed", "go", "package a\n")

	rec := s.PostHX(t, s.Alice, "/paste/"+itoa(id)+"/delete", url.Values{})

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body: %s", rec.Code, rec.Body.String())
	}
	if push := rec.Header().Get("HX-Push-Url"); push != "/paste/" {
		t.Errorf("HX-Push-Url = %q, want /paste/", push)
	}
}

func itoa(n int64) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}
