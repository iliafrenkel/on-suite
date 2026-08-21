package paste_test

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"github.com/iliafrenkel/on-suite/internal/apps/paste"
	"github.com/iliafrenkel/on-suite/internal/htmlassert"
	"github.com/iliafrenkel/on-suite/internal/platform/app"
	"github.com/iliafrenkel/on-suite/internal/platform/auth"
	"github.com/iliafrenkel/on-suite/internal/platform/db"
	"github.com/iliafrenkel/on-suite/internal/platform/render"
	"github.com/iliafrenkel/on-suite/internal/platform/web"
	"github.com/iliafrenkel/on-suite/internal/ui"
)

const testPassword = "a-sufficiently-long-password"

// server is the whole stack over a real database, with two accounts.
type server struct {
	handler http.Handler
	store   *paste.Store
	alice   *session
	bob     *session
}

// session holds one signed-in browser's cookies.
type session struct {
	user    auth.User
	cookies []*http.Cookie
}

func newServer(t *testing.T) *server {
	t.Helper()
	ctx := context.Background()

	handle, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = handle.Close() })

	registry, err := app.NewRegistry(paste.New())
	if err != nil {
		t.Fatal(err)
	}

	migrations, err := db.Collect(auth.Namespace, auth.Migrations())
	if err != nil {
		t.Fatal(err)
	}
	appMigrations, err := registry.Migrations()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Apply(ctx, handle, append(migrations, appMigrations...)); err != nil {
		t.Fatal(err)
	}

	users := auth.NewStore(handle)
	assets, err := web.NewAssets(ui.Static(), "/static")
	if err != nil {
		t.Fatal(err)
	}
	rend, err := render.NewRenderer(render.Options{Layouts: ui.Templates(), AssetURL: assets.URL})
	if err != nil {
		t.Fatal(err)
	}

	log := slog.New(slog.DiscardHandler)
	errs := web.NewErrors(rend, log)
	csrf := web.NewCSRF(false, errs)
	authn := web.NewAuth(web.AuthOptions{
		Users: users, Render: rend, Errors: errs, CSRF: csrf, Log: log, Secure: false,
	})

	mux := http.NewServeMux()
	authn.Routes(mux)
	if err := registry.Mount(mux, app.Deps{
		DB: handle, Render: rend, Users: users, Errors: errs, Log: log,
	}, authn.RequireUser); err != nil {
		t.Fatalf("Mount: %v", err)
	}
	mux.Handle("/", http.HandlerFunc(errs.NotFound))

	s := &server{
		handler: web.Stack(mux, log, errs, csrf, authn),
		store:   paste.NewStore(handle),
	}

	hash, err := auth.HashPassword(testPassword)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"alice", "bob"} {
		u, err := users.CreateUser(ctx, name, hash, false)
		if err != nil {
			t.Fatal(err)
		}
		sess := s.logIn(t, u)
		if name == "alice" {
			s.alice = sess
		} else {
			s.bob = sess
		}
	}
	return s
}

// do issues a request carrying a session's cookies.
func (s *server) do(t *testing.T, sess *session, req *http.Request) *httptest.ResponseRecorder {
	t.Helper()
	if sess != nil {
		for _, c := range sess.cookies {
			req.AddCookie(c)
		}
	}
	rec := httptest.NewRecorder()
	s.handler.ServeHTTP(rec, req)
	return rec
}

// post submits a form with the session's CSRF token attached.
func (s *server) post(t *testing.T, sess *session, path string, form url.Values) *httptest.ResponseRecorder {
	t.Helper()
	form.Set(web.CSRFFormField, s.csrfToken(t, sess))
	req := httptest.NewRequest("POST", path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return s.do(t, sess, req)
}

func (s *server) csrfToken(t *testing.T, sess *session) string {
	t.Helper()
	for _, c := range sess.cookies {
		if c.Name == web.CSRFCookieName {
			return c.Value
		}
	}
	t.Fatal("session has no CSRF cookie")
	return ""
}

// logIn performs a real login so the tests use genuine session cookies.
func (s *server) logIn(t *testing.T, u auth.User) *session {
	t.Helper()

	page := s.do(t, nil, httptest.NewRequest("GET", "/login", nil))
	var csrfCookie *http.Cookie
	for _, c := range page.Result().Cookies() {
		if c.Name == web.CSRFCookieName {
			csrfCookie = c
		}
	}
	if csrfCookie == nil {
		t.Fatal("GET /login issued no CSRF cookie")
	}

	form := url.Values{
		"username":        {u.Username},
		"password":        {testPassword},
		web.CSRFFormField: {csrfCookie.Value},
	}
	req := httptest.NewRequest("POST", "/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := s.do(t, &session{cookies: []*http.Cookie{csrfCookie}}, req)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("login for %s = %d", u.Username, rec.Code)
	}
	return &session{user: u, cookies: rec.Result().Cookies()}
}

// createSnippet is the shortcut other tests use.
func (s *server) createSnippet(t *testing.T, sess *session, title, language, body string) int64 {
	t.Helper()
	rec := s.post(t, sess, "/paste/new", url.Values{
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
		rec := s.do(t, nil, httptest.NewRequest("GET", path, nil))
		if rec.Code != http.StatusSeeOther {
			t.Errorf("GET %s anonymous = %d, want a 303 to the login page", path, rec.Code)
		}
	}
}

func TestNewFormRenders(t *testing.T) {
	s := newServer(t)
	rec := s.do(t, s.alice, httptest.NewRequest("GET", "/paste/new", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	doc := htmlassert.Parse(t, rec.Body.String())
	doc.MustHave("textarea[name=body]")
	doc.MustHave("input[name=title]")
	doc.MustHave("select[name=language]")
	doc.MustHave("input[name=" + web.CSRFFormField + "]")

	// The active app must be marked in the nav, which the router sets.
	nav := doc.MustHave(`nav.shell-nav a[aria-current=page]`)
	if got := htmlassert.Text(nav); got != "ON Paste" {
		t.Errorf("the marked nav item is %q, want ON Paste", got)
	}
	if n := len(doc.QueryAll("select[name=language] option")); n < 10 {
		t.Errorf("the language picker has only %d options", n)
	}
}

func TestCreateThenView(t *testing.T) {
	s := newServer(t)
	id := s.createSnippet(t, s.alice, "My config", "yaml", "key: value\nother: 2\n")

	rec := s.do(t, s.alice, httptest.NewRequest("GET", "/paste/"+itoa(id), nil))
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

// TestViewingSomeoneElsesSnippetIs404 — not a 403, which would confirm it
// exists.
func TestViewingSomeoneElsesSnippetIs404(t *testing.T) {
	s := newServer(t)
	id := s.createSnippet(t, s.alice, "alice's", "go", "secret\n")

	rec := s.do(t, s.bob, httptest.NewRequest("GET", "/paste/"+itoa(id), nil))
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
			rec := s.post(t, s.alice, "/paste/new", tt.form)
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

	rec := s.post(t, s.alice, "/paste/new", url.Values{
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

	rec := s.do(t, s.alice, req) // no token in the form
	if rec.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403", rec.Code)
	}
}

func TestViewRejectsNonNumericAndMissingIDs(t *testing.T) {
	s := newServer(t)
	for _, path := range []string{"/paste/abc", "/paste/0", "/paste/-1", "/paste/99999"} {
		rec := s.do(t, s.alice, httptest.NewRequest("GET", path, nil))
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
	id := s.createSnippet(t, s.alice, "<img src=x onerror=alert(1)>", "html", hostile)

	rec := s.do(t, s.alice, httptest.NewRequest("GET", "/paste/"+itoa(id), nil))
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
	rec := s.do(t, nil, httptest.NewRequest("GET", "/paste/highlight.css", nil))
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
	rec = s.do(t, nil, req)
	if rec.Code != http.StatusNotModified {
		t.Errorf("revalidation = %d, want 304", rec.Code)
	}
}

func TestListShowsOnlyYourSnippetsNewestFirst(t *testing.T) {
	s := newServer(t)

	s.createSnippet(t, s.alice, "alice one", "go", "package one\n")
	s.createSnippet(t, s.alice, "alice two", "go", "package two\n")
	s.createSnippet(t, s.bob, "bob's secret", "go", "package bob\n")

	rec := s.do(t, s.alice, httptest.NewRequest("GET", "/paste/", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	doc := htmlassert.Parse(t, rec.Body.String())

	links := doc.QueryAll("ul.snippet-list li a")
	if len(links) != 2 {
		t.Fatalf("the list has %d entries, want 2 (alice's only)", len(links))
	}
	if got := htmlassert.Text(links[0]); got != "alice two" {
		t.Errorf("first entry = %q, want the newest", got)
	}
	if text := doc.Text(); strings.Contains(text, "bob") {
		t.Error("another user's snippet appeared in the list")
	}
}

func TestListWhenEmpty(t *testing.T) {
	s := newServer(t)
	rec := s.do(t, s.alice, httptest.NewRequest("GET", "/paste/", nil))

	doc := htmlassert.Parse(t, rec.Body.String())
	doc.MustNotHave("ul.snippet-list")
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
	s.createSnippet(t, s.alice, "tall", "plaintext",
		strings.Repeat("a line of text that goes on\n", 40))

	rec := s.do(t, s.alice, httptest.NewRequest("GET", "/paste/", nil))
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
	id := s.createSnippet(t, s.alice, "doomed", "go", "package main\n")

	rec := s.post(t, s.alice, "/paste/"+itoa(id)+"/delete", url.Values{})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("delete = %d, want 303", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "/paste/" {
		t.Errorf("Location = %q, want /paste/", loc)
	}

	rec = s.do(t, s.alice, httptest.NewRequest("GET", "/paste/"+itoa(id), nil))
	if rec.Code != http.StatusNotFound {
		t.Errorf("the snippet is still viewable: %d", rec.Code)
	}
}

// TestDeleteSomeoneElsesSnippetFails is the one that would matter most if it
// regressed.
func TestDeleteSomeoneElsesSnippetFails(t *testing.T) {
	s := newServer(t)
	id := s.createSnippet(t, s.alice, "alice's", "go", "package main\n")

	rec := s.post(t, s.bob, "/paste/"+itoa(id)+"/delete", url.Values{})
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}

	// And it is genuinely still there.
	rec = s.do(t, s.alice, httptest.NewRequest("GET", "/paste/"+itoa(id), nil))
	if rec.Code != http.StatusOK {
		t.Error("the owner's snippet was destroyed by another user's request")
	}
}

func TestDeleteRequiresCSRFAndPOST(t *testing.T) {
	s := newServer(t)
	id := s.createSnippet(t, s.alice, "safe", "go", "package main\n")

	// No token.
	req := httptest.NewRequest("POST", "/paste/"+itoa(id)+"/delete", nil)
	if rec := s.do(t, s.alice, req); rec.Code != http.StatusForbidden {
		t.Errorf("delete without a CSRF token = %d, want 403", rec.Code)
	}

	// A GET must not delete: the route is POST-only, so ServeMux refuses it.
	rec := s.do(t, s.alice, httptest.NewRequest("GET", "/paste/"+itoa(id)+"/delete", nil))
	if rec.Code == http.StatusSeeOther {
		t.Error("a GET performed the deletion")
	}

	if rec := s.do(t, s.alice, httptest.NewRequest("GET", "/paste/"+itoa(id), nil)); rec.Code != http.StatusOK {
		t.Error("the snippet was deleted by a request that should have been refused")
	}
}

// shareAndGetSlug shares a snippet and returns its public slug, read back from
// the store so the test does not have to scrape it out of the page.
func (s *server) shareAndGetSlug(t *testing.T, sess *session, id int64) string {
	t.Helper()

	rec := s.post(t, sess, "/paste/"+itoa(id)+"/share", url.Values{})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("share = %d, want 303", rec.Code)
	}
	snippet, err := s.store.ByID(context.Background(), sess.user.ID, id)
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
	id := s.createSnippet(t, s.alice, "Shared config", "yaml", "key: value\n")
	slug := s.shareAndGetSlug(t, s.alice, id)

	// nil session: no cookies at all.
	rec := s.do(t, nil, httptest.NewRequest("GET", "/paste/s/"+slug, nil))
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

// TestSharedPageOffersNoDestructiveControls is why the shared view has its own
// template rather than reusing the owner's with conditionals.
func TestSharedPageOffersNoDestructiveControls(t *testing.T) {
	s := newServer(t)
	id := s.createSnippet(t, s.alice, "Shared", "go", "package main\n")
	slug := s.shareAndGetSlug(t, s.alice, id)

	rec := s.do(t, nil, httptest.NewRequest("GET", "/paste/s/"+slug, nil))
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

// TestUnshareKillsTheLink is the point of choosing a revocable share model.
func TestUnshareKillsTheLink(t *testing.T) {
	s := newServer(t)
	id := s.createSnippet(t, s.alice, "Shared", "go", "package main\n")
	slug := s.shareAndGetSlug(t, s.alice, id)

	if rec := s.do(t, nil, httptest.NewRequest("GET", "/paste/s/"+slug, nil)); rec.Code != http.StatusOK {
		t.Fatal("the link did not work before revoking it")
	}

	rec := s.post(t, s.alice, "/paste/"+itoa(id)+"/unshare", url.Values{})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("unshare = %d", rec.Code)
	}

	if rec := s.do(t, nil, httptest.NewRequest("GET", "/paste/s/"+slug, nil)); rec.Code != http.StatusNotFound {
		t.Errorf("the revoked link still works: %d", rec.Code)
	}
	if rec := s.do(t, nil, httptest.NewRequest("GET", "/paste/s/"+slug+"/raw", nil)); rec.Code != http.StatusNotFound {
		t.Errorf("the revoked raw link still works: %d", rec.Code)
	}

	// Re-sharing must produce a different link, leaving the old one dead.
	second := s.shareAndGetSlug(t, s.alice, id)
	if second == slug {
		t.Fatal("re-sharing reissued the revoked slug")
	}
	if rec := s.do(t, nil, httptest.NewRequest("GET", "/paste/s/"+slug, nil)); rec.Code != http.StatusNotFound {
		t.Error("the old link came back to life after re-sharing")
	}
}

func TestUnsharedSnippetIsNotPubliclyReachable(t *testing.T) {
	s := newServer(t)
	id := s.createSnippet(t, s.alice, "Private", "go", "top secret\n")

	// The owner's own URL must not work for an anonymous visitor.
	rec := s.do(t, nil, httptest.NewRequest("GET", "/paste/"+itoa(id), nil))
	if rec.Code != http.StatusSeeOther {
		t.Errorf("anonymous access to a private snippet = %d, want a redirect to login", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "top secret") {
		t.Fatal("a private snippet leaked to an anonymous request")
	}

	// And a guessed share URL must not either.
	for _, slug := range []string{itoa(id), "aaaaaaaaaaaaaaaaaaaaaa", ""} {
		rec := s.do(t, nil, httptest.NewRequest("GET", "/paste/s/"+slug, nil))
		if rec.Code == http.StatusOK {
			t.Errorf("GET /paste/s/%q returned 200", slug)
		}
	}
}

func TestShareStateIsShownToTheOwner(t *testing.T) {
	s := newServer(t)
	id := s.createSnippet(t, s.alice, "Shared", "go", "package main\n")
	slug := s.shareAndGetSlug(t, s.alice, id)

	rec := s.do(t, s.alice, httptest.NewRequest("GET", "/paste/"+itoa(id), nil))
	doc := htmlassert.Parse(t, rec.Body.String())

	// The link is shown so it can be copied, and the control now revokes.
	if got := htmlassert.Text(doc.MustHave(".notice code")); !strings.Contains(got, slug) {
		t.Errorf("the share link is not displayed; got %q", got)
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
	id := s.createSnippet(t, s.alice, "Raw", "plaintext", body)
	slug := s.shareAndGetSlug(t, s.alice, id)

	cases := []struct {
		name    string
		path    string
		session *session
	}{
		{"owner", "/paste/raw/" + itoa(id), s.alice},
		{"shared, signed out", "/paste/s/" + slug + "/raw", nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := s.do(t, tc.session, httptest.NewRequest("GET", tc.path, nil))
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
	id := s.createSnippet(t, s.alice, "evil", "html",
		"<html><body><script>alert(1)</script></body></html>\n")

	rec := s.do(t, s.alice, httptest.NewRequest("GET", "/paste/raw/"+itoa(id), nil))
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
	id := s.createSnippet(t, s.alice, "alice's", "go", "secret\n")

	rec := s.do(t, s.bob, httptest.NewRequest("GET", "/paste/raw/"+itoa(id), nil))
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "secret") {
		t.Error("the snippet leaked")
	}
}

func TestShareRequiresCSRF(t *testing.T) {
	s := newServer(t)
	id := s.createSnippet(t, s.alice, "t", "go", "package main\n")

	for _, action := range []string{"share", "unshare"} {
		req := httptest.NewRequest("POST", "/paste/"+itoa(id)+"/"+action, nil)
		if rec := s.do(t, s.alice, req); rec.Code != http.StatusForbidden {
			t.Errorf("%s without a CSRF token = %d, want 403", action, rec.Code)
		}
	}
}

// TestPublicSurfaceIsExactlyThreeRoutes is the backstop on the whole sharing
// design. If a future change makes a fourth path anonymous, this fails first.
func TestPublicSurfaceIsExactlyThreeRoutes(t *testing.T) {
	s := newServer(t)
	id := s.createSnippet(t, s.alice, "shared", "go", "package main\n")
	slug := s.shareAndGetSlug(t, s.alice, id)

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
		if rec := s.do(t, nil, httptest.NewRequest("GET", path, nil)); rec.Code != http.StatusOK {
			t.Errorf("public path %s = %d, want 200", path, rec.Code)
		}
	}
	for _, path := range blocked {
		if rec := s.do(t, nil, httptest.NewRequest("GET", path, nil)); rec.Code == http.StatusOK {
			t.Errorf("%s is reachable anonymously and must not be", path)
		}
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
