package notes_test

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/iliafrenkel/on-suite/internal/apps/notes"
	"github.com/iliafrenkel/on-suite/internal/htmlassert"
	"github.com/iliafrenkel/on-suite/internal/platform/app"
	"github.com/iliafrenkel/on-suite/internal/platform/auth"
	"github.com/iliafrenkel/on-suite/internal/platform/db"
	"github.com/iliafrenkel/on-suite/internal/platform/render"
	"github.com/iliafrenkel/on-suite/internal/platform/web"
	"github.com/iliafrenkel/on-suite/internal/ui"
)

const testPassword = "a-sufficiently-long-password"

// server is the whole stack over a real database file, with two accounts.
// A real file, not ":memory:", for the reason given in N1's store tests: the
// bugs worth catching live in SQLite's own behaviour.
type server struct {
	handler http.Handler
	store   *notes.Store
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

	registry, err := app.NewRegistry(notes.New())
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
	authn.Routes(mux, nil)
	if err := registry.Mount(mux, app.Deps{
		DB: handle, Render: rend, Users: users, Errors: errs, Log: log,
	}, authn.RequireUser); err != nil {
		t.Fatalf("Mount: %v", err)
	}
	mux.Handle("/", http.HandlerFunc(errs.NotFound))

	s := &server{
		handler: web.Stack(mux, log, errs, csrf, authn),
		store:   notes.NewStore(handle),
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

// get fetches a page and fails the test unless it is a 200.
func (s *server) get(t *testing.T, sess *session, path string) *htmlassert.Doc {
	t.Helper()
	rec := s.do(t, sess, httptest.NewRequest("GET", path, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET %s = %d, want 200; body: %s", path, rec.Code, rec.Body.String())
	}
	return htmlassert.Parse(t, rec.Body.String())
}

// post submits a form with the session's CSRF token attached.
func (s *server) post(t *testing.T, sess *session, path string, form url.Values) *httptest.ResponseRecorder {
	t.Helper()
	form.Set(web.CSRFFormField, s.csrfToken(t, sess))
	req := httptest.NewRequest("POST", path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return s.do(t, sess, req)
}

// submit posts a structural request and asserts it redirected to wantLocation.
// Almost every write test in this file goes through it, because "did it come
// back to the outline I was looking at" is half of what each one checks.
func (s *server) submit(t *testing.T, sess *session, path string, form url.Values, wantLocation string) {
	t.Helper()
	rec := s.post(t, sess, path, form)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("POST %s = %d, want 303; body: %s", path, rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Location"); got != wantLocation {
		t.Fatalf("POST %s redirected to %q, want %q", path, got, wantLocation)
	}
}

// postHX submits a form the way notes.js's own requests always will: HTMX
// itself sets this header on every request, and the platform's CSRF check
// already accepts it in place of the hidden form field.
func (s *server) postHX(t *testing.T, sess *session, path string, form url.Values) *httptest.ResponseRecorder {
	t.Helper()
	form.Set(web.CSRFFormField, s.csrfToken(t, sess))
	req := httptest.NewRequest("POST", path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("HX-Request", "true")
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

// anonymous returns a "session" holding a valid CSRF cookie and no login
// cookie. It is how a test gets past the CSRF middleware in order to assert on
// the auth guard behind it, which is otherwise unreachable: a tokenless POST
// never gets that far.
func (s *server) anonymous(t *testing.T) *session {
	t.Helper()

	page := s.do(t, nil, httptest.NewRequest("GET", "/login", nil))
	for _, c := range page.Result().Cookies() {
		if c.Name == web.CSRFCookieName {
			return &session{cookies: []*http.Cookie{c}}
		}
	}
	t.Fatal("GET /login issued no CSRF cookie")
	return nil
}

// seed creates a bullet straight through the store, for tests about reading.
// Tests about writing go through the routes instead.
func (s *server) seed(t *testing.T, sess *session, parentID int64, title string) int64 {
	t.Helper()
	n, err := s.store.Create(context.Background(), sess.user.ID, parentID, 1<<30, title, "")
	if err != nil {
		t.Fatalf("seeding %q: %v", title, err)
	}
	return n.ID
}

func itoa(id int64) string { return strconv.FormatInt(id, 10) }

// titlesAt reads a sibling list's titles in order, which is what almost every
// assertion about creating, moving and deleting is really about.
func (s *server) titlesAt(t *testing.T, sess *session, parentID int64) []string {
	t.Helper()
	children, err := s.store.Children(context.Background(), sess.user.ID, parentID)
	if err != nil {
		t.Fatal(err)
	}
	out := make([]string, 0, len(children))
	for _, c := range children {
		out = append(out, c.Title)
	}
	return out
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// ---- tests ----------------------------------------------------------------

// TestNotesRequiresSignIn confirms the default-deny router covers this
// chunk's GET routes. A route accidentally registered with Public would show
// up here as a 200 instead of a redirect to the login page. The POST routes
// are covered separately by TestEveryMutationRequiresSignIn.
func TestNotesRequiresSignIn(t *testing.T) {
	s := newServer(t)

	for _, path := range []string{"/notes/", "/notes/1"} {
		rec := s.do(t, nil, httptest.NewRequest("GET", path, nil))
		if rec.Code != http.StatusSeeOther {
			t.Errorf("GET %s anonymous = %d, want a 303 to the login page", path, rec.Code)
		}
	}
}

func TestOutlineRendersInsideTheShell(t *testing.T) {
	s := newServer(t)
	doc := s.get(t, s.alice, "/notes/")

	doc.MustHave("#outline")

	// The router marks the active app; the shell reads it. If this fails the
	// app is serving pages but is not part of the suite.
	nav := doc.MustHave(`nav.shell-nav a[aria-current=page]`)
	if got := htmlassert.Text(nav); got != "ON Notes" {
		t.Errorf("the marked nav item is %q, want ON Notes", got)
	}
}

func TestOutlineRendersTheTreeNested(t *testing.T) {
	s := newServer(t)
	parent := s.seed(t, s.alice, notes.RootID, "Projects")
	s.seed(t, s.alice, parent, "AtBudget")
	s.seed(t, s.alice, notes.RootID, "Reading")

	doc := s.get(t, s.alice, "/notes/")

	// Two top-level bullets, and one of them has a nested list under it.
	// htmlassert has descendant selectors but no child combinator, so
	// "top level" is expressed as "every bullet, less the nested ones".
	all := doc.QueryAll(".outline-item")
	nested := doc.QueryAll(".outline-item .outline-item")
	if len(all)-len(nested) != 2 {
		t.Errorf("got %d top-level bullets, want 2", len(all)-len(nested))
	}
	if len(nested) != 1 {
		t.Fatalf("got %d nested bullets, want 1", len(nested))
	}

	// Every bullet's text is an input, so the page is already editable.
	titles := doc.QueryAll("input.outline-title")
	if len(titles) != 3 {
		t.Fatalf("got %d title inputs, want 3", len(titles))
	}
	var got []string
	for _, in := range titles {
		v, _ := htmlassert.Attr(in, "value")
		got = append(got, v)
	}
	want := []string{"Projects", "AtBudget", "Reading"}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("bullet %d is %q, want %q (pre-order: %v)", i, got[i], want[i], got)
		}
	}
}

func TestOutlineRendersAnotherUsersTreeNowhere(t *testing.T) {
	s := newServer(t)
	s.seed(t, s.bob, notes.RootID, "bob's secret")

	doc := s.get(t, s.alice, "/notes/")
	if strings.Contains(doc.Text(), "bob's secret") {
		t.Error("another user's bullet is on the page")
	}
	// input.outline-title also matches the empty-outline's new_title field
	// (both share the class for styling), so a real bullet is identified by
	// name=title specifically.
	if n := len(doc.QueryAll("input[name=title]")); n != 0 {
		t.Errorf("alice's empty outline rendered %d bullets", n)
	}
}

// TestCollapsedBulletHidesItsChildren is spec §6: the payload is bounded by
// collapse state, so a collapsed subtree is not merely hidden by CSS — it is
// not in the response at all.
func TestCollapsedBulletHidesItsChildren(t *testing.T) {
	s := newServer(t)
	parent := s.seed(t, s.alice, notes.RootID, "Projects")
	s.seed(t, s.alice, parent, "AtBudget")

	if err := s.store.SetCollapsed(context.Background(), s.alice.user.ID, parent, true); err != nil {
		t.Fatal(err)
	}

	doc := s.get(t, s.alice, "/notes/")
	if strings.Contains(doc.Text(), "AtBudget") {
		t.Error("a collapsed bullet's child is in the response")
	}
	// The chevron is a button whenever the bullet has children, collapsed or
	// not — collapsing removes the child list from the render, never the
	// chevron. notes.js depends on exactly that: with no child list in the
	// DOM, button.outline-chevron is the only remaining evidence that a
	// collapsed bullet has a subtree, and Backspace-to-delete refuses on it.
	// Drop the button here and an empty collapsed parent would be silently
	// deleted along with its hidden children.
	chevron := doc.MustHave("button.outline-chevron")
	if _, ok := htmlassert.Attr(chevron, "aria-expanded"); !ok {
		t.Error("a collapsed bullet renders no expand control")
	}
	if got, _ := htmlassert.Attr(chevron, "aria-expanded"); got != "false" {
		t.Errorf("a collapsed bullet's chevron is aria-expanded=%q, want false", got)
	}
}

// TestOutlinePageLoadsTheScript. notes.js was dead code for three commits
// because nothing on the page loaded it: the route served it correctly the
// whole time. Serving it and loading it are separate claims, so this asserts
// the second one.
func TestOutlinePageLoadsTheScript(t *testing.T) {
	s := newServer(t)
	doc := s.get(t, s.alice, "/notes/")
	doc.MustHave("script[src=/notes/notes.js]")
}

// TestBulletControlsAreDisabledWhereTheOperationIsANoOp. The store treats all
// four as no-ops rather than errors, so this is honesty rather than
// enforcement: a button that cannot do anything should not look like it can.
func TestBulletControlsAreDisabledAtTheEdges(t *testing.T) {
	s := newServer(t)
	s.seed(t, s.alice, notes.RootID, "first")
	s.seed(t, s.alice, notes.RootID, "second")

	doc := s.get(t, s.alice, "/notes/")

	// Two flat bullets, so document order is outline order: [0] is first,
	// [1] is second.
	ups := doc.QueryAll(`button[value=up]`)
	downs := doc.QueryAll(`button[value=down]`)
	if len(ups) != 2 || len(downs) != 2 {
		t.Fatalf("got %d move-up and %d move-down buttons, want 2 of each", len(ups), len(downs))
	}
	if _, ok := htmlassert.Attr(ups[0], "disabled"); !ok {
		t.Error("the first bullet's move-up is not disabled")
	}
	if _, ok := htmlassert.Attr(downs[0], "disabled"); ok {
		t.Error("the first bullet's move-down is disabled but a sibling follows it")
	}
	if _, ok := htmlassert.Attr(ups[1], "disabled"); ok {
		t.Error("the second bullet's move-up is disabled but a sibling precedes it")
	}
	if _, ok := htmlassert.Attr(downs[1], "disabled"); !ok {
		t.Error("the last bullet's move-down is not disabled")
	}
}

// TestOutlineUsesNoInlineStyles. The CSP has no unsafe-inline: a style
// attribute would simply not apply, so indentation must come from nesting.
func TestOutlineUsesNoInlineStyles(t *testing.T) {
	s := newServer(t)
	parent := s.seed(t, s.alice, notes.RootID, "Projects")
	s.seed(t, s.alice, parent, "AtBudget")

	rec := s.do(t, s.alice, httptest.NewRequest("GET", "/notes/", nil))
	if strings.Contains(rec.Body.String(), "style=") {
		t.Error("the outline contains an inline style attribute, which the CSP blocks")
	}
}

func TestOutlineEscapesBulletText(t *testing.T) {
	s := newServer(t)
	s.seed(t, s.alice, notes.RootID, `<script>alert(1)</script>`)

	rec := s.do(t, s.alice, httptest.NewRequest("GET", "/notes/", nil))
	if strings.Contains(rec.Body.String(), "<script>alert(1)</script>") {
		t.Error("bullet text reached the page unescaped")
	}
	doc := htmlassert.Parse(t, rec.Body.String())
	v, _ := htmlassert.Attr(doc.MustHave("input.outline-title"), "value")
	if v != `<script>alert(1)</script>` {
		t.Errorf("the round-tripped title is %q", v)
	}
}

// TestBulletRendersMarkdown covers the full path: a saved title with
// Markdown in it reaches the page as rendered HTML inside the overlay span,
// while the input underneath still carries the raw source untouched — the
// no-JS fallback keeps editing the literal text spec §7 already proved.
func TestBulletRendersMarkdown(t *testing.T) {
	s := newServer(t)
	id := s.seed(t, s.alice, notes.RootID, "**bold** and #tag")

	rec := s.do(t, s.alice, httptest.NewRequest("GET", "/notes/", nil))
	body := rec.Body.String()
	if !strings.Contains(body, `id="rendered-title-`+itoa(id)+`"`) {
		t.Fatalf("no rendered overlay for bullet %d in:\n%s", id, body)
	}
	doc := htmlassert.Parse(t, body)
	overlay := doc.MustHave(`#rendered-title-` + itoa(id))
	if got := htmlassert.Text(overlay); got != "bold and #tag" {
		t.Errorf("rendered overlay text = %q", got)
	}
	if n := len(doc.QueryAll(`#rendered-title-` + itoa(id) + ` strong`)); n != 1 {
		t.Errorf("got %d <strong> in the overlay, want 1", n)
	}
	if n := len(doc.QueryAll(`#rendered-title-` + itoa(id) + ` a`)); n != 1 {
		t.Errorf("got %d tag chip in the overlay, want 1", n)
	}

	// The raw <input> still carries the literal, unrendered source.
	raw := doc.MustHave("input.outline-title")
	if v, _ := htmlassert.Attr(raw, "value"); v != "**bold** and #tag" {
		t.Errorf("input value = %q, want the raw source untouched", v)
	}
}

// TestRenderedOverlayIsNotOutOfBandOnAnOrdinaryRender. The overlay markup is
// shared with setText's out-of-band response, but a row rendered the ordinary
// way must never carry hx-swap-oob: every structural operation answers with
// the whole outline fragment over AJAX, and htmx lifts each hx-swap-oob
// element out of such a response before swapping it in — which would strip
// every overlay and leave the bullets blank until a full reload.
func TestRenderedOverlayIsNotOutOfBandOnAnOrdinaryRender(t *testing.T) {
	s := newServer(t)
	first := s.seed(t, s.alice, notes.RootID, "**first**")
	second := s.seed(t, s.alice, notes.RootID, "second")

	page := s.do(t, s.alice, httptest.NewRequest("GET", "/notes/", nil)).Body.String()
	if !strings.Contains(page, `id="rendered-title-`+itoa(first)+`"`) {
		t.Fatalf("no rendered overlay on the page for bullet %d", first)
	}
	if oobOverlays(t, page) {
		t.Error("a full page load carries hx-swap-oob on the rendered overlays")
	}

	// The same rows, this time as the fragment a structural operation returns.
	frag := s.postHX(t, s.alice, "/notes/"+itoa(second)+"/indent", url.Values{
		"root": {"0"}, "focus_id": {itoa(second)}, "title": {"second"}, "note": {""},
	}).Body.String()
	if !strings.Contains(frag, `id="rendered-title-`+itoa(first)+`"`) {
		t.Fatalf("no rendered overlay in the structural fragment for bullet %d", first)
	}
	if oobOverlays(t, frag) {
		t.Error("a structural fragment carries hx-swap-oob, so htmx would strip the overlays out of it")
	}
	assertOnlyToggleIsOOB(t, frag)
}

// assertOnlyToggleIsOOB asserts that the show-completed toggle is the only
// element anywhere in body carrying hx-swap-oob. A structural fragment
// legitimately marks that one toolbar button out of band — see
// renderOutlineFragment — but nothing else should ever be: an accidental
// hx-swap-oob on, say, .outline-list or an .outline-row would make htmx
// silently strip that chunk out of the response before swapping it in.
func assertOnlyToggleIsOOB(t *testing.T, body string) {
	t.Helper()
	for _, n := range htmlassert.Parse(t, body).QueryAll("[hx-swap-oob]") {
		if id, _ := htmlassert.Attr(n, "id"); id != "show-completed-toggle" {
			t.Errorf("unexpected hx-swap-oob element (id=%q); only show-completed-toggle may be out of band", id)
		}
	}
}

// oobOverlays reports whether any rendered overlay in body is marked for an
// out-of-band swap. The check is per-overlay rather than a substring search
// for "hx-swap-oob" anywhere, because a fragment does legitimately carry one
// out-of-band element: the show-completed toggle, which lives outside
// #outline and could not be updated any other way.
func oobOverlays(t *testing.T, body string) bool {
	t.Helper()
	for _, n := range htmlassert.Parse(t, body).QueryAll(".outline-rendered") {
		if _, ok := htmlassert.Attr(n, "hx-swap-oob"); ok {
			return true
		}
	}
	return false
}

// TestRenderedOverlayEscapesBulletText: the overlay is real HTML the browser
// parses, so it must never carry unescaped user text even when nothing in
// it looks like Markdown.
func TestRenderedOverlayEscapesBulletText(t *testing.T) {
	s := newServer(t)
	s.seed(t, s.alice, notes.RootID, `<script>alert(1)</script>`)

	rec := s.do(t, s.alice, httptest.NewRequest("GET", "/notes/", nil))
	if strings.Contains(rec.Body.String(), "<script>alert(1)</script>") {
		t.Error("bullet text reached the rendered overlay unescaped")
	}
}

func TestZoomShowsOnlyTheSubtree(t *testing.T) {
	s := newServer(t)
	projects := s.seed(t, s.alice, notes.RootID, "Projects")
	s.seed(t, s.alice, projects, "AtBudget")
	s.seed(t, s.alice, notes.RootID, "Reading")

	doc := s.get(t, s.alice, "/notes/"+itoa(projects))

	text := doc.Text()
	if !strings.Contains(text, "Projects") {
		t.Error("the zoom root is not named on its own page")
	}
	if strings.Contains(text, "Reading") {
		t.Error("a sibling of the zoom root is on the page")
	}
	titles := doc.QueryAll("input.outline-title")
	if len(titles) != 1 {
		t.Fatalf("got %d bullets, want just the one child", len(titles))
	}
	if v, _ := htmlassert.Attr(titles[0], "value"); v != "AtBudget" {
		t.Errorf("the visible bullet is %q, want AtBudget", v)
	}
}

func TestZoomRendersTheBreadcrumb(t *testing.T) {
	s := newServer(t)
	projects := s.seed(t, s.alice, notes.RootID, "Projects")
	budget := s.seed(t, s.alice, projects, "AtBudget")
	api := s.seed(t, s.alice, budget, "API")

	doc := s.get(t, s.alice, "/notes/"+itoa(api))
	crumbs := doc.MustHave("nav.outline-crumbs")

	// Outermost first, and every ancestor is a link back to its own zoom.
	links := doc.QueryAll("nav.outline-crumbs a")
	var got []string
	for _, l := range links {
		got = append(got, htmlassert.Text(l))
	}
	want := []string{"All notes", "Projects", "AtBudget"}
	if len(got) != len(want) {
		t.Fatalf("breadcrumb links are %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("breadcrumb link %d is %q, want %q", i, got[i], want[i])
		}
	}
	if href, _ := htmlassert.Attr(links[1], "href"); href != "/notes/"+itoa(projects) {
		t.Errorf("the Projects crumb points at %q", href)
	}
	// The node you are on is not a link to where you already are.
	if !strings.Contains(htmlassert.Text(crumbs), "API") {
		t.Error("the breadcrumb does not name the current root")
	}
}

func TestTopLevelHasNoBreadcrumb(t *testing.T) {
	s := newServer(t)
	s.seed(t, s.alice, notes.RootID, "Projects")

	s.get(t, s.alice, "/notes/").MustNotHave("nav.outline-crumbs")
}

func TestZoomingIntoAnotherUsersNodeIs404(t *testing.T) {
	s := newServer(t)
	id := s.seed(t, s.bob, notes.RootID, "bob's secret")

	rec := s.do(t, s.alice, httptest.NewRequest("GET", "/notes/"+itoa(id), nil))
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "bob's secret") {
		t.Error("another user's bullet leaked through the zoom route")
	}
}

func TestZoomingIntoNonsenseIs404(t *testing.T) {
	s := newServer(t)
	for _, path := range []string{"/notes/0", "/notes/-1", "/notes/abc", "/notes/999999"} {
		rec := s.do(t, s.alice, httptest.NewRequest("GET", path, nil))
		if rec.Code != http.StatusNotFound {
			t.Errorf("GET %s = %d, want 404", path, rec.Code)
		}
	}
}

func TestBulletDotZoomsIn(t *testing.T) {
	s := newServer(t)
	id := s.seed(t, s.alice, notes.RootID, "Projects")

	doc := s.get(t, s.alice, "/notes/")
	href, _ := htmlassert.Attr(doc.MustHave("a.outline-dot"), "href")
	if href != "/notes/"+itoa(id) {
		t.Errorf("the bullet dot points at %q, want /notes/%d", href, id)
	}
}

func TestSetTextSavesTheBullet(t *testing.T) {
	s := newServer(t)
	id := s.seed(t, s.alice, notes.RootID, "old")

	s.submit(t, s.alice, "/notes/"+itoa(id)+"/text", url.Values{
		"root": {"0"}, "focus_id": {itoa(id)},
		"title": {"new"}, "note": {"a note"},
	}, "/notes/")

	n, err := s.store.ByID(context.Background(), s.alice.user.ID, id)
	if err != nil {
		t.Fatal(err)
	}
	if n.Title != "new" || n.Note != "a note" {
		t.Errorf("saved %q / %q, want new / a note", n.Title, n.Note)
	}
}

func TestSetTextReturnsToTheZoomItCameFrom(t *testing.T) {
	s := newServer(t)
	root := s.seed(t, s.alice, notes.RootID, "Projects")
	child := s.seed(t, s.alice, root, "AtBudget")

	s.submit(t, s.alice, "/notes/"+itoa(child)+"/text", url.Values{
		"root": {itoa(root)}, "title": {"renamed"}, "note": {""},
	}, "/notes/"+itoa(root))
}

func TestSetTextOnAnotherUsersBulletIs404(t *testing.T) {
	s := newServer(t)
	id := s.seed(t, s.bob, notes.RootID, "bob's")

	rec := s.post(t, s.alice, "/notes/"+itoa(id)+"/text", url.Values{
		"root": {"0"}, "title": {"stolen"}, "note": {""},
	})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}

	n, err := s.store.ByID(context.Background(), s.bob.user.ID, id)
	if err != nil {
		t.Fatal(err)
	}
	if n.Title != "bob's" {
		t.Errorf("bob's bullet is now %q", n.Title)
	}
}

func TestMutationsWithoutACSRFTokenAreRejected(t *testing.T) {
	s := newServer(t)
	id := s.seed(t, s.alice, notes.RootID, "Projects")

	form := url.Values{"root": {"0"}, "title": {"changed"}, "note": {""}}
	req := httptest.NewRequest("POST", "/notes/"+itoa(id)+"/text", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := s.do(t, s.alice, req)

	if rec.Code == http.StatusSeeOther {
		t.Fatal("a POST with no CSRF token succeeded")
	}
	n, err := s.store.ByID(context.Background(), s.alice.user.ID, id)
	if err != nil {
		t.Fatal(err)
	}
	if n.Title != "Projects" {
		t.Errorf("the bullet was changed to %q", n.Title)
	}
}

func TestMalformedRootIsRejected(t *testing.T) {
	s := newServer(t)
	id := s.seed(t, s.alice, notes.RootID, "Projects")

	rec := s.post(t, s.alice, "/notes/"+itoa(id)+"/text", url.Values{
		"root": {"not-a-number"}, "title": {"changed"}, "note": {""},
	})
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

// TestOversizeTextIsRejected: unreachable from a browser, because the inputs
// carry maxlength. This is the backstop for anything that is not a browser.
func TestOversizeTextIsRejected(t *testing.T) {
	s := newServer(t)
	id := s.seed(t, s.alice, notes.RootID, "Projects")

	rec := s.post(t, s.alice, "/notes/"+itoa(id)+"/text", url.Values{
		"root": {"0"}, "title": {strings.Repeat("a", notes.MaxTitleRunes+1)}, "note": {""},
	})
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

// TestEmptyOutlineOffersOneBullet is spec §6: a new user's first keystroke
// lands in the outline, not on a "create your first note" button.
func TestEmptyOutlineOffersOneBullet(t *testing.T) {
	s := newServer(t)
	doc := s.get(t, s.alice, "/notes/")

	form := doc.MustHave(`form[action=/notes/new]`)
	if _, ok := htmlassert.Attr(form, "action"); !ok {
		t.Fatal("the empty outline offers no way to start")
	}
	in := doc.MustHave(`input[name=new_title]`)
	if _, ok := htmlassert.Attr(in, "autofocus"); !ok {
		t.Error("the first bullet is not focused")
	}
	doc.MustNotHave(`input[name=title]`)
}

func TestCreateFromTheEmptyOutline(t *testing.T) {
	s := newServer(t)

	s.submit(t, s.alice, "/notes/new", url.Values{
		"root": {"0"}, "new_title": {"first"},
	}, "/notes/")

	children, err := s.store.Children(context.Background(), s.alice.user.ID, notes.RootID)
	if err != nil {
		t.Fatal(err)
	}
	if len(children) != 1 || children[0].Title != "first" {
		t.Fatalf("children = %+v, want one bullet titled first", children)
	}
}

// TestCreateWithNoFocusAppendsToTheZoomRoot: the empty-outline form carries a
// root and no focus, and the bullet has to land inside the zoom the user is
// looking at, not at the top level.
func TestCreateWithNoFocusAppendsToTheZoomRoot(t *testing.T) {
	s := newServer(t)
	root := s.seed(t, s.alice, notes.RootID, "Projects")

	s.submit(t, s.alice, "/notes/new", url.Values{
		"root": {itoa(root)}, "new_title": {"AtBudget"},
	}, "/notes/"+itoa(root))

	children, err := s.store.Children(context.Background(), s.alice.user.ID, root)
	if err != nil {
		t.Fatal(err)
	}
	if len(children) != 1 || children[0].Title != "AtBudget" {
		t.Fatalf("children of the zoom root = %+v", children)
	}
}

// TestCreateAfterTheFocusedBullet is Enter: the new bullet is the focused
// one's next sibling, not the last child of anything.
func TestCreateAfterTheFocusedBullet(t *testing.T) {
	s := newServer(t)
	first := s.seed(t, s.alice, notes.RootID, "first")
	s.seed(t, s.alice, notes.RootID, "third")

	s.submit(t, s.alice, "/notes/new", url.Values{
		"root": {"0"}, "focus_id": {itoa(first)},
		"title": {"first"}, "note": {""},
		"new_title": {"second"},
	}, "/notes/")

	if got := s.titlesAt(t, s.alice, notes.RootID); !equalStrings(got, []string{"first", "second", "third"}) {
		t.Fatalf("children = %v, want [first second third]", got)
	}
}

// TestCreateSplitsTheFocusedBulletsText is spec §8's Enter, ahead of the
// keyboard that will send it: what stays and what moves are one write.
func TestCreateSplitsTheFocusedBulletsText(t *testing.T) {
	s := newServer(t)
	id := s.seed(t, s.alice, notes.RootID, "hello world")

	s.submit(t, s.alice, "/notes/new", url.Values{
		"root": {"0"}, "focus_id": {itoa(id)},
		"title": {"hello"}, "note": {""},
		"new_title": {"world"},
	}, "/notes/")

	children, err := s.store.Children(context.Background(), s.alice.user.ID, notes.RootID)
	if err != nil {
		t.Fatal(err)
	}
	if len(children) != 2 || children[0].Title != "hello" || children[1].Title != "world" {
		t.Fatalf("children = %+v, want hello then world", children)
	}
}

func TestCreateUnderAnotherUsersFocusIs404(t *testing.T) {
	s := newServer(t)
	bobs := s.seed(t, s.bob, notes.RootID, "bob's")

	rec := s.post(t, s.alice, "/notes/new", url.Values{
		"root": {"0"}, "focus_id": {itoa(bobs)},
		"title": {"stolen"}, "note": {""}, "new_title": {"x"},
	})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}

	// Nothing was created for alice, and nothing was changed for bob: the
	// whole transaction rolled back.
	alices, err := s.store.Children(context.Background(), s.alice.user.ID, notes.RootID)
	if err != nil {
		t.Fatal(err)
	}
	if len(alices) != 0 {
		t.Errorf("alice gained %d bullets", len(alices))
	}
	n, err := s.store.ByID(context.Background(), s.bob.user.ID, bobs)
	if err != nil {
		t.Fatal(err)
	}
	if n.Title != "bob's" {
		t.Errorf("bob's bullet is now %q", n.Title)
	}
}

func TestPlusButtonAddsAnEmptyBulletBelow(t *testing.T) {
	s := newServer(t)
	s.seed(t, s.alice, notes.RootID, "Projects")

	doc := s.get(t, s.alice, "/notes/")
	btn := doc.MustHave(`button[formaction=/notes/new]`)
	if btn == nil {
		t.Fatal("a bullet offers no way to add one below it")
	}
}

func TestIndentNestsUnderThePreviousSibling(t *testing.T) {
	s := newServer(t)
	first := s.seed(t, s.alice, notes.RootID, "first")
	second := s.seed(t, s.alice, notes.RootID, "second")

	s.submit(t, s.alice, "/notes/"+itoa(second)+"/indent", url.Values{
		"root": {"0"}, "focus_id": {itoa(second)},
		"title": {"second"}, "note": {""},
	}, "/notes/")

	children, err := s.store.Children(context.Background(), s.alice.user.ID, first)
	if err != nil {
		t.Fatal(err)
	}
	if len(children) != 1 || children[0].ID != second {
		t.Fatalf("children of first = %+v, want just second", children)
	}

	// And the page now shows it nested.
	doc := s.get(t, s.alice, "/notes/")
	if n := len(doc.QueryAll(".outline-item .outline-item")); n != 1 {
		t.Errorf("got %d nested bullets on the page, want 1", n)
	}
}

func TestIndentOfTheFirstSiblingDoesNothing(t *testing.T) {
	s := newServer(t)
	first := s.seed(t, s.alice, notes.RootID, "first")

	// Not an error: the caller is a keypress, and Tab on the first line of an
	// outline should do nothing rather than complain.
	s.submit(t, s.alice, "/notes/"+itoa(first)+"/indent", url.Values{
		"root": {"0"}, "focus_id": {itoa(first)}, "title": {"first"}, "note": {""},
	}, "/notes/")

	n, err := s.store.ByID(context.Background(), s.alice.user.ID, first)
	if err != nil {
		t.Fatal(err)
	}
	if n.ParentID != notes.RootID || n.Position != 0 {
		t.Errorf("first moved to parent %d position %d", n.ParentID, n.Position)
	}
}

func TestOutdentPromotesToTheParentsNextSibling(t *testing.T) {
	s := newServer(t)
	parent := s.seed(t, s.alice, notes.RootID, "parent")
	child := s.seed(t, s.alice, parent, "child")
	s.seed(t, s.alice, notes.RootID, "after")

	s.submit(t, s.alice, "/notes/"+itoa(child)+"/outdent", url.Values{
		"root": {"0"}, "focus_id": {itoa(child)}, "title": {"child"}, "note": {""},
	}, "/notes/")

	top, err := s.store.Children(context.Background(), s.alice.user.ID, notes.RootID)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, n := range top {
		got = append(got, n.Title)
	}
	if len(got) != 3 || got[0] != "parent" || got[1] != "child" || got[2] != "after" {
		t.Fatalf("top level = %v, want [parent child after]", got)
	}
}

func TestOutdentAtTheTopLevelDoesNothing(t *testing.T) {
	s := newServer(t)
	id := s.seed(t, s.alice, notes.RootID, "only")

	s.submit(t, s.alice, "/notes/"+itoa(id)+"/outdent", url.Values{
		"root": {"0"}, "focus_id": {itoa(id)}, "title": {"only"}, "note": {""},
	}, "/notes/")

	n, err := s.store.ByID(context.Background(), s.alice.user.ID, id)
	if err != nil {
		t.Fatal(err)
	}
	if n.ParentID != notes.RootID {
		t.Errorf("a top-level bullet was reparented to %d", n.ParentID)
	}
}

// TestIndentPastTheDepthLimitIsRejected. MaxDepth exists so that a runaway
// import cannot produce an outline no UI can render; a hand-driven indent has
// to hit the same wall, and hit it as a 400 rather than a 500.
func TestIndentPastTheDepthLimitIsRejected(t *testing.T) {
	s := newServer(t)
	ctx := context.Background()

	// A chain exactly MaxDepth deep: depths 0..MaxDepth.
	var parent int64 = notes.RootID
	var deepest int64
	for d := 0; d <= notes.MaxDepth; d++ {
		n, err := s.store.Create(ctx, s.alice.user.ID, parent, 1<<30, "d", "")
		if err != nil {
			t.Fatalf("building the chain at depth %d: %v", d, err)
		}
		parent, deepest = n.ID, n.ID
	}
	// A sibling of the deepest node; indenting it would put it one past the cap.
	deepestNode, err := s.store.ByID(ctx, s.alice.user.ID, deepest)
	if err != nil {
		t.Fatal(err)
	}
	sibling, err := s.store.Create(ctx, s.alice.user.ID, deepestNode.ParentID, 1<<30, "sibling", "")
	if err != nil {
		t.Fatal(err)
	}

	rec := s.post(t, s.alice, "/notes/"+itoa(sibling.ID)+"/indent", url.Values{
		"root": {"0"}, "focus_id": {itoa(sibling.ID)}, "title": {"sibling"}, "note": {""},
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}

	n, err := s.store.ByID(ctx, s.alice.user.ID, sibling.ID)
	if err != nil {
		t.Fatal(err)
	}
	if n.ParentID != deepestNode.ParentID {
		t.Errorf("the bullet moved despite the rejection")
	}
}

func TestIndentingAnotherUsersBulletIs404(t *testing.T) {
	s := newServer(t)
	s.seed(t, s.bob, notes.RootID, "bob's first")
	second := s.seed(t, s.bob, notes.RootID, "bob's second")

	rec := s.post(t, s.alice, "/notes/"+itoa(second)+"/indent", url.Values{
		"root": {"0"},
	})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}

	n, err := s.store.ByID(context.Background(), s.bob.user.ID, second)
	if err != nil {
		t.Fatal(err)
	}
	if n.ParentID != notes.RootID {
		t.Error("bob's bullet was indented by alice")
	}
}

func TestMoveUpAndDown(t *testing.T) {
	s := newServer(t)
	a := s.seed(t, s.alice, notes.RootID, "a")
	b := s.seed(t, s.alice, notes.RootID, "b")
	s.seed(t, s.alice, notes.RootID, "c")

	s.submit(t, s.alice, "/notes/"+itoa(b)+"/move", url.Values{
		"root": {"0"}, "focus_id": {itoa(b)}, "title": {"b"}, "note": {""},
		"dir": {"up"},
	}, "/notes/")
	if got := s.titlesAt(t, s.alice, notes.RootID); !equalStrings(got, []string{"b", "a", "c"}) {
		t.Fatalf("after move up: %v, want [b a c]", got)
	}

	s.submit(t, s.alice, "/notes/"+itoa(a)+"/move", url.Values{
		"root": {"0"}, "focus_id": {itoa(a)}, "title": {"a"}, "note": {""},
		"dir": {"down"},
	}, "/notes/")
	if got := s.titlesAt(t, s.alice, notes.RootID); !equalStrings(got, []string{"b", "c", "a"}) {
		t.Fatalf("after move down: %v, want [b c a]", got)
	}
}

func TestMoveAtTheEdgesDoesNothing(t *testing.T) {
	s := newServer(t)
	a := s.seed(t, s.alice, notes.RootID, "a")
	b := s.seed(t, s.alice, notes.RootID, "b")

	for _, tc := range []struct {
		id  int64
		dir string
	}{{a, "up"}, {b, "down"}} {
		s.submit(t, s.alice, "/notes/"+itoa(tc.id)+"/move", url.Values{
			"root": {"0"}, "dir": {tc.dir},
		}, "/notes/")
	}
	if got := s.titlesAt(t, s.alice, notes.RootID); !equalStrings(got, []string{"a", "b"}) {
		t.Fatalf("order changed to %v", got)
	}
}

func TestMoveRejectsAnUnknownDirection(t *testing.T) {
	s := newServer(t)
	id := s.seed(t, s.alice, notes.RootID, "a")

	for _, dir := range []string{"", "sideways", "UP", "1"} {
		rec := s.post(t, s.alice, "/notes/"+itoa(id)+"/move", url.Values{
			"root": {"0"}, "dir": {dir},
		})
		if rec.Code != http.StatusBadRequest {
			t.Errorf("dir=%q gave %d, want 400", dir, rec.Code)
		}
	}
}

func TestCollapseAndExpand(t *testing.T) {
	s := newServer(t)
	ctx := context.Background()
	parent := s.seed(t, s.alice, notes.RootID, "parent")
	s.seed(t, s.alice, parent, "child")

	s.submit(t, s.alice, "/notes/"+itoa(parent)+"/collapse", url.Values{
		"root": {"0"}, "focus_id": {itoa(parent)}, "title": {"parent"}, "note": {""},
		"collapsed": {"1"},
	}, "/notes/")

	n, err := s.store.ByID(ctx, s.alice.user.ID, parent)
	if err != nil {
		t.Fatal(err)
	}
	if !n.Collapsed {
		t.Fatal("the bullet is not collapsed")
	}
	if strings.Contains(s.get(t, s.alice, "/notes/").Text(), "child") {
		t.Error("a collapsed bullet's child is still in the response")
	}

	s.submit(t, s.alice, "/notes/"+itoa(parent)+"/collapse", url.Values{
		"root": {"0"}, "collapsed": {"0"},
	}, "/notes/")

	n, err = s.store.ByID(ctx, s.alice.user.ID, parent)
	if err != nil {
		t.Fatal(err)
	}
	if n.Collapsed {
		t.Error("the bullet is still collapsed")
	}
}

// TestCollapseIsIdempotent: the field says what the state should become, not
// "flip it", so a double submit or a stale page cannot toggle it back.
func TestCollapseIsIdempotent(t *testing.T) {
	s := newServer(t)
	parent := s.seed(t, s.alice, notes.RootID, "parent")
	s.seed(t, s.alice, parent, "child")

	for range 2 {
		s.submit(t, s.alice, "/notes/"+itoa(parent)+"/collapse", url.Values{
			"root": {"0"}, "collapsed": {"1"},
		}, "/notes/")
	}
	n, err := s.store.ByID(context.Background(), s.alice.user.ID, parent)
	if err != nil {
		t.Fatal(err)
	}
	if !n.Collapsed {
		t.Error("collapsing twice left the bullet expanded")
	}
}

func TestCollapseRejectsAnUnknownValue(t *testing.T) {
	s := newServer(t)
	id := s.seed(t, s.alice, notes.RootID, "a")

	for _, v := range []string{"", "true", "yes", "2"} {
		rec := s.post(t, s.alice, "/notes/"+itoa(id)+"/collapse", url.Values{
			"root": {"0"}, "collapsed": {v},
		})
		if rec.Code != http.StatusBadRequest {
			t.Errorf("collapsed=%q gave %d, want 400", v, rec.Code)
		}
	}
}

func TestMovingAnotherUsersBulletIs404(t *testing.T) {
	s := newServer(t)
	s.seed(t, s.bob, notes.RootID, "bob's a")
	second := s.seed(t, s.bob, notes.RootID, "bob's b")

	for _, path := range []string{"/move", "/collapse"} {
		form := url.Values{"root": {"0"}, "dir": {"up"}, "collapsed": {"1"}}
		rec := s.post(t, s.alice, "/notes/"+itoa(second)+path, form)
		if rec.Code != http.StatusNotFound {
			t.Errorf("POST %s = %d, want 404", path, rec.Code)
		}
	}
	if got := s.titlesAt(t, s.bob, notes.RootID); !equalStrings(got, []string{"bob's a", "bob's b"}) {
		t.Errorf("bob's outline is now %v", got)
	}
}

func TestDeleteRemovesTheBulletAndItsSubtree(t *testing.T) {
	s := newServer(t)
	ctx := context.Background()
	parent := s.seed(t, s.alice, notes.RootID, "parent")
	child := s.seed(t, s.alice, parent, "child")
	s.seed(t, s.alice, notes.RootID, "survivor")

	s.submit(t, s.alice, "/notes/"+itoa(parent)+"/delete", url.Values{
		"root": {"0"},
	}, "/notes/")

	if got := s.titlesAt(t, s.alice, notes.RootID); !equalStrings(got, []string{"survivor"}) {
		t.Fatalf("top level = %v, want [survivor]", got)
	}
	if _, err := s.store.ByID(ctx, s.alice.user.ID, child); err == nil {
		t.Error("the child outlived its parent")
	}
}

// TestDeleteRenumbersTheSurvivors: I1 says sibling positions are contiguous
// from zero, and a delete that leaves a gap makes every later clamp land one
// place off — silently, three moves later.
func TestDeleteRenumbersTheSurvivors(t *testing.T) {
	s := newServer(t)
	s.seed(t, s.alice, notes.RootID, "a")
	b := s.seed(t, s.alice, notes.RootID, "b")
	s.seed(t, s.alice, notes.RootID, "c")

	s.submit(t, s.alice, "/notes/"+itoa(b)+"/delete", url.Values{"root": {"0"}}, "/notes/")

	children, err := s.store.Children(context.Background(), s.alice.user.ID, notes.RootID)
	if err != nil {
		t.Fatal(err)
	}
	for i, c := range children {
		if c.Position != i {
			t.Errorf("%q is at position %d, want %d", c.Title, c.Position, i)
		}
	}
}

func TestDeleteReturnsToTheZoomItCameFrom(t *testing.T) {
	s := newServer(t)
	root := s.seed(t, s.alice, notes.RootID, "Projects")
	child := s.seed(t, s.alice, root, "AtBudget")

	s.submit(t, s.alice, "/notes/"+itoa(child)+"/delete", url.Values{
		"root": {itoa(root)},
	}, "/notes/"+itoa(root))
}

func TestDeletingAnotherUsersBulletIs404(t *testing.T) {
	s := newServer(t)
	id := s.seed(t, s.bob, notes.RootID, "bob's")

	rec := s.post(t, s.alice, "/notes/"+itoa(id)+"/delete", url.Values{"root": {"0"}})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
	if _, err := s.store.ByID(context.Background(), s.bob.user.ID, id); err != nil {
		t.Errorf("bob's bullet was deleted by alice: %v", err)
	}
}

// TestEveryStructuralButtonMirrorsItsFormactionAsHTMX. Progressive
// enhancement: hx-post always equals formaction, so a JS-disabled browser
// and an HTMX one issue the exact same request the button already
// declares — nothing in notes.js needs to know a URL.
func TestEveryStructuralButtonMirrorsItsFormactionAsHTMX(t *testing.T) {
	s := newServer(t)
	s.seed(t, s.alice, notes.RootID, "a")

	doc := s.get(t, s.alice, "/notes/")
	buttons := doc.QueryAll("button[formaction]")
	if len(buttons) == 0 {
		t.Fatal("no formaction buttons found")
	}
	for _, b := range buttons {
		action, _ := htmlassert.Attr(b, "formaction")
		hxPost, ok := htmlassert.Attr(b, "hx-post")
		if !ok || hxPost != action {
			t.Errorf("button formaction=%q has hx-post=%q", action, hxPost)
		}
		if got, _ := htmlassert.Attr(b, "hx-target"); got != "#outline" {
			t.Errorf("button formaction=%q has hx-target=%q, want #outline", action, got)
		}
		if got, _ := htmlassert.Attr(b, "hx-swap"); got != "innerHTML" {
			t.Errorf("button formaction=%q has hx-swap=%q, want innerHTML", action, got)
		}
	}
}

func TestRowsCarryTheirNodeID(t *testing.T) {
	s := newServer(t)
	id := s.seed(t, s.alice, notes.RootID, "a")

	doc := s.get(t, s.alice, "/notes/")
	row := doc.MustHave(".outline-row")
	if got, _ := htmlassert.Attr(row, "data-id"); got != itoa(id) {
		t.Errorf("row data-id = %q, want %q", got, itoa(id))
	}
}

// TestTextInputsAutosaveOverHTMX. hx-swap=none: nothing on screen needs to
// change from a text-only save, the input already shows what was typed.
func TestTextInputsAutosaveOverHTMX(t *testing.T) {
	s := newServer(t)
	id := s.seed(t, s.alice, notes.RootID, "a")

	doc := s.get(t, s.alice, "/notes/")
	for _, sel := range []string{"input.outline-title", "input.outline-note"} {
		in := doc.MustHave(sel)
		if got, _ := htmlassert.Attr(in, "hx-post"); got != "/notes/"+itoa(id)+"/text" {
			t.Errorf("%s hx-post = %q", sel, got)
		}
		if got, _ := htmlassert.Attr(in, "hx-swap"); got != "none" {
			t.Errorf("%s hx-swap = %q, want none", sel, got)
		}
		if _, ok := htmlassert.Attr(in, "hx-trigger"); !ok {
			t.Errorf("%s has no hx-trigger", sel)
		}
	}
}

func TestEmptyOutlineFormIsHTMXWired(t *testing.T) {
	s := newServer(t)
	doc := s.get(t, s.alice, "/notes/")
	form := doc.MustHave(`form[action=/notes/new]`)
	if got, _ := htmlassert.Attr(form, "hx-post"); got != "/notes/new" {
		t.Errorf("empty-outline form hx-post = %q", got)
	}
}

// TestDeleteIsItsOwnConfirmedForm. hx-confirm, not data-confirm: once the
// form is hx-post-driven, HTMX's own confirmation runs before it builds the
// request at all, so there is no dependency on theme.js's generic
// data-confirm listener or any question of which one runs first.
func TestDeleteIsItsOwnConfirmedForm(t *testing.T) {
	s := newServer(t)
	id := s.seed(t, s.alice, notes.RootID, "Projects")

	doc := s.get(t, s.alice, "/notes/")
	form := doc.MustHave(`form[action=/notes/` + itoa(id) + `/delete]`)
	if _, ok := htmlassert.Attr(form, "hx-confirm"); !ok {
		t.Error("the delete form asks for no confirmation")
	}
	if got, _ := htmlassert.Attr(form, "hx-post"); got != "/notes/"+itoa(id)+"/delete" {
		t.Errorf("delete form hx-post = %q", got)
	}
	if got, _ := htmlassert.Attr(form, "hx-target"); got != "#outline" {
		t.Errorf("delete form hx-target = %q, want #outline", got)
	}
	// It must not be the row's main form, or every button would be confirmed.
	if cls, _ := htmlassert.Attr(form, "class"); !strings.Contains(cls, "outline-delete") {
		t.Errorf("the delete form's class is %q", cls)
	}
	doc.MustHave(`form.outline-delete input[name=csrf_token]`)
	doc.MustHave(`form.outline-delete input[name=root]`)
}

// TestStructuralRequestSavesTheFocusedText is spec §7. Every structural POST
// carries the focused bullet's text so the text update and the structural
// operation are one write. Without it, a user who types and then presses Tab
// loses whatever landed after the last save.
func TestStructuralRequestSavesTheFocusedText(t *testing.T) {
	s := newServer(t)
	ctx := context.Background()
	first := s.seed(t, s.alice, notes.RootID, "first")
	second := s.seed(t, s.alice, notes.RootID, "second")

	s.submit(t, s.alice, "/notes/"+itoa(second)+"/indent", url.Values{
		"root": {"0"}, "focus_id": {itoa(second)},
		"title": {"typed after the last save"}, "note": {"and a note"},
	}, "/notes/")

	n, err := s.store.ByID(ctx, s.alice.user.ID, second)
	if err != nil {
		t.Fatal(err)
	}
	if n.Title != "typed after the last save" || n.Note != "and a note" {
		t.Errorf("the focused bullet is %q / %q; the keystrokes were lost", n.Title, n.Note)
	}
	if n.ParentID != first {
		t.Errorf("the bullet was not indented (parent %d, want %d)", n.ParentID, first)
	}
}

// TestFocusAndTargetCanDiffer is the case N2's forms never produce and N3's
// keyboard will: the caret is in one bullet and the operation acts on another.
func TestFocusAndTargetCanDiffer(t *testing.T) {
	s := newServer(t)
	ctx := context.Background()
	focused := s.seed(t, s.alice, notes.RootID, "focused")
	parent := s.seed(t, s.alice, notes.RootID, "parent")
	s.seed(t, s.alice, parent, "child")

	s.submit(t, s.alice, "/notes/"+itoa(parent)+"/collapse", url.Values{
		"root": {"0"}, "focus_id": {itoa(focused)},
		"title": {"edited elsewhere"}, "note": {""},
		"collapsed": {"1"},
	}, "/notes/")

	f, err := s.store.ByID(ctx, s.alice.user.ID, focused)
	if err != nil {
		t.Fatal(err)
	}
	if f.Title != "edited elsewhere" {
		t.Errorf("the focused bullet is %q", f.Title)
	}
	p, err := s.store.ByID(ctx, s.alice.user.ID, parent)
	if err != nil {
		t.Fatal(err)
	}
	if !p.Collapsed {
		t.Error("the targeted bullet was not collapsed")
	}
}

// TestAFailedStructuralOperationSavesNoText proves the two really are one
// transaction rather than two writes that usually both happen. The text update
// succeeds and the structural operation then fails, so the text must be rolled
// back with it.
func TestAFailedStructuralOperationSavesNoText(t *testing.T) {
	s := newServer(t)
	ctx := context.Background()
	mine := s.seed(t, s.alice, notes.RootID, "mine")
	bobs := s.seed(t, s.bob, notes.RootID, "bob's")

	// Indenting bob's bullet fails; alice's own text update came first and
	// must not survive.
	rec := s.post(t, s.alice, "/notes/"+itoa(bobs)+"/indent", url.Values{
		"root": {"0"}, "focus_id": {itoa(mine)},
		"title": {"should not be saved"}, "note": {""},
	})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}

	n, err := s.store.ByID(ctx, s.alice.user.ID, mine)
	if err != nil {
		t.Fatal(err)
	}
	if n.Title != "mine" {
		t.Errorf("the text survived a failed operation: %q", n.Title)
	}
}

// TestAForgedFocusIsRejectedWholesale: naming someone else's bullet as the
// focus fails the whole request rather than quietly skipping the text update.
func TestAForgedFocusIsRejectedWholesale(t *testing.T) {
	s := newServer(t)
	ctx := context.Background()
	bobs := s.seed(t, s.bob, notes.RootID, "bob's")
	s.seed(t, s.alice, notes.RootID, "a")
	b := s.seed(t, s.alice, notes.RootID, "b")

	rec := s.post(t, s.alice, "/notes/"+itoa(b)+"/move", url.Values{
		"root": {"0"}, "focus_id": {itoa(bobs)},
		"title": {"overwritten"}, "note": {""}, "dir": {"up"},
	})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
	if n, err := s.store.ByID(ctx, s.bob.user.ID, bobs); err != nil || n.Title != "bob's" {
		t.Errorf("bob's bullet is now %+v (err %v)", n, err)
	}
	if got := s.titlesAt(t, s.alice, notes.RootID); !equalStrings(got, []string{"a", "b"}) {
		t.Errorf("the move happened anyway: %v", got)
	}
}

func TestAMalformedFocusIsA400(t *testing.T) {
	s := newServer(t)
	id := s.seed(t, s.alice, notes.RootID, "a")

	for _, v := range []string{"abc", "-1", "0.5"} {
		rec := s.post(t, s.alice, "/notes/"+itoa(id)+"/indent", url.Values{
			"root": {"0"}, "focus_id": {v}, "title": {"x"}, "note": {""},
		})
		if rec.Code != http.StatusBadRequest {
			t.Errorf("focus_id=%q gave %d, want 400", v, rec.Code)
		}
	}
}

// TestEveryMutationRequiresSignIn covers the routes the first sign-in test
// could not, because they are POSTs.
//
// Two layers stand between an anonymous POST and a handler, and the platform's
// middleware order (web.Stack: csrf.Middleware, then authn.LoadUser, then the
// router's RequireUser) makes which one answers deterministic, so both are
// asserted exactly rather than loosely:
//
//   - with no CSRF token, CSRF.Middleware answers first, with a 403. It never
//     reaches the auth guard, so on its own it says nothing about sign-in;
//   - with a valid token taken from an anonymous GET, CSRF passes and
//     RequireUser answers, with a 303 to /login. That is the assertion this
//     test is named for.
func TestEveryMutationRequiresSignIn(t *testing.T) {
	s := newServer(t)
	id := s.seed(t, s.alice, notes.RootID, "a")

	paths := []string{
		"/notes/new",
		"/notes/" + itoa(id) + "/text",
		"/notes/" + itoa(id) + "/indent",
		"/notes/" + itoa(id) + "/outdent",
		"/notes/" + itoa(id) + "/move",
		"/notes/" + itoa(id) + "/collapse",
		"/notes/" + itoa(id) + "/delete",
	}

	// No CSRF token: the CSRF middleware is outermost of the two and answers.
	for _, path := range paths {
		req := httptest.NewRequest("POST", path, strings.NewReader(""))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		rec := s.do(t, nil, req)
		if rec.Code != http.StatusForbidden {
			t.Errorf("POST %s anonymous and tokenless = %d, want 403 from the CSRF check",
				path, rec.Code)
		}
	}

	// A valid token, still anonymous: CSRF is satisfied, so the auth guard is
	// what rejects, and it sends the browser to the login page.
	anon := s.anonymous(t)
	for _, path := range paths {
		rec := s.post(t, anon, path, url.Values{"root": {"0"}})
		if rec.Code != http.StatusSeeOther {
			t.Errorf("POST %s anonymous with a valid token = %d, want 303", path, rec.Code)
			continue
		}
		if got := rec.Header().Get("Location"); got != "/login" {
			t.Errorf("POST %s anonymous redirected to %q, not the login page", path, got)
		}
	}

	n, err := s.store.ByID(context.Background(), s.alice.user.ID, id)
	if err != nil {
		t.Fatalf("the bullet is gone: %v", err)
	}
	if n.Title != "a" {
		t.Errorf("an anonymous request changed the bullet to %q", n.Title)
	}
}

// TestStructuralMutationRespondsWithAFragmentForHTMX. Once notes.js exists,
// every structural button issues this same request over AJAX; the response
// must be the swap target's own content, not a redirect the browser would
// have to follow with a second round trip.
func TestStructuralMutationRespondsWithAFragmentForHTMX(t *testing.T) {
	s := newServer(t)
	first := s.seed(t, s.alice, notes.RootID, "first")
	second := s.seed(t, s.alice, notes.RootID, "second")

	rec := s.postHX(t, s.alice, "/notes/"+itoa(second)+"/indent", url.Values{
		"root": {"0"}, "focus_id": {itoa(second)}, "title": {"second"}, "note": {""},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "<html") {
		t.Error("an HTMX response carries whole-document chrome")
	}
	doc := htmlassert.Parse(t, rec.Body.String())
	if n := len(doc.QueryAll(".outline-item .outline-item")); n != 1 {
		t.Errorf("got %d nested bullets in the fragment, want 1", n)
	}

	children, err := s.store.Children(context.Background(), s.alice.user.ID, first)
	if err != nil {
		t.Fatal(err)
	}
	if len(children) != 1 || children[0].ID != second {
		t.Fatalf("the indent did not apply: children of first = %+v", children)
	}
}

func TestCreateRespondsWithAFragmentForHTMX(t *testing.T) {
	s := newServer(t)

	rec := s.postHX(t, s.alice, "/notes/new", url.Values{
		"root": {"0"}, "new_title": {"first"},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "first") {
		t.Error("the new bullet is not in the fragment")
	}
}

// TestSetTextRespondsWithRenderedMarkdownForHTMX supersedes N3's 204: once a
// field can show rendered Markdown, an edit has to update it, and there is
// no swap that can happen without a response body to swap in.
func TestSetTextRespondsWithRenderedMarkdownForHTMX(t *testing.T) {
	s := newServer(t)
	id := s.seed(t, s.alice, notes.RootID, "old")

	rec := s.postHX(t, s.alice, "/notes/"+itoa(id)+"/text", url.Values{
		"root": {"0"}, "focus_id": {itoa(id)}, "title": {"**new**"}, "note": {"*n*"},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `id="rendered-title-`+itoa(id)+`"`) ||
		!strings.Contains(body, "<strong>new</strong>") {
		t.Errorf("title OOB fragment missing or wrong: %s", body)
	}
	if !strings.Contains(body, `id="rendered-note-`+itoa(id)+`"`) ||
		!strings.Contains(body, "<em>n</em>") {
		t.Errorf("note OOB fragment missing or wrong: %s", body)
	}
	if !strings.Contains(body, `hx-swap-oob="true"`) {
		t.Error("the response does not mark itself as an out-of-band swap")
	}

	n, err := s.store.ByID(context.Background(), s.alice.user.ID, id)
	if err != nil {
		t.Fatal(err)
	}
	if n.Title != "**new**" {
		t.Errorf("saved title = %q, want the raw source", n.Title)
	}
}

func TestAFailedStructuralOperationUnderHTMXIsAFragment(t *testing.T) {
	s := newServer(t)
	bobs := s.seed(t, s.bob, notes.RootID, "bob's")

	rec := s.postHX(t, s.alice, "/notes/"+itoa(bobs)+"/indent", url.Values{"root": {"0"}})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "<html") {
		t.Error("an HTMX error response carries whole-document chrome")
	}
}

func TestDoneTogglesTheBullet(t *testing.T) {
	s := newServer(t)
	id := s.seed(t, s.alice, notes.RootID, "task")

	s.submit(t, s.alice, "/notes/"+itoa(id)+"/done", url.Values{
		"root": {"0"}, "done": {"1"},
	}, "/notes/")

	n, err := s.store.ByID(context.Background(), s.alice.user.ID, id)
	if err != nil {
		t.Fatal(err)
	}
	if !n.Done {
		t.Fatal("the bullet is not done")
	}

	s.submit(t, s.alice, "/notes/"+itoa(id)+"/done", url.Values{
		"root": {"0"}, "done": {"0"},
	}, "/notes/")

	n, err = s.store.ByID(context.Background(), s.alice.user.ID, id)
	if err != nil {
		t.Fatal(err)
	}
	if n.Done {
		t.Fatal("the bullet is still done")
	}
}

func TestDoneRejectsAnUnknownValue(t *testing.T) {
	s := newServer(t)
	id := s.seed(t, s.alice, notes.RootID, "a")

	for _, v := range []string{"", "true", "yes", "2"} {
		rec := s.post(t, s.alice, "/notes/"+itoa(id)+"/done", url.Values{
			"root": {"0"}, "done": {v},
		})
		if rec.Code != http.StatusBadRequest {
			t.Errorf("done=%q gave %d, want 400", v, rec.Code)
		}
	}
}

func TestDoneOnAnotherUsersBulletIs404(t *testing.T) {
	s := newServer(t)
	id := s.seed(t, s.bob, notes.RootID, "bob's")

	rec := s.post(t, s.alice, "/notes/"+itoa(id)+"/done", url.Values{
		"root": {"0"}, "done": {"1"},
	})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

// TestDoneBulletRendersStruckThrough covers the row-level CSS hook rather
// than CSS itself, which no Go test can see: the row carries a class the
// stylesheet keys off, and the checkbox reflects the current state.
//
// It asks for the outline with show-completed on (N5): without the cookie a
// done bullet is not rendered at all, so there would be no row to inspect.
func TestDoneBulletRendersStruckThrough(t *testing.T) {
	s := newServer(t)
	id := s.seed(t, s.alice, notes.RootID, "task")
	if err := s.store.SetDone(context.Background(), s.alice.user.ID, id, true); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest("GET", "/notes/", nil)
	req.AddCookie(&http.Cookie{Name: notes.ShowCompletedCookie, Value: "1"})
	rec := s.do(t, s.alice, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /notes/ = %d, want 200", rec.Code)
	}
	doc := htmlassert.Parse(t, rec.Body.String())
	row := doc.MustHave(".outline-row-done")
	if got, _ := htmlassert.Attr(row, "data-id"); got != itoa(id) {
		t.Errorf("the done row is %q, want %d", got, id)
	}
	btn := doc.MustHave(`button[formaction=/notes/` + itoa(id) + `/done]`)
	if got, _ := htmlassert.Attr(btn, "value"); got != "0" {
		t.Errorf("a done bullet's toggle sends value=%q, want 0 (mark not done)", got)
	}
}

func TestDueSetsAndClearsTheChip(t *testing.T) {
	s := newServer(t)
	id := s.seed(t, s.alice, notes.RootID, "task")

	s.submit(t, s.alice, "/notes/"+itoa(id)+"/due", url.Values{
		"root": {"0"}, "due": {"2026-03-05"},
	}, "/notes/")

	doc := s.get(t, s.alice, "/notes/")
	chip := doc.MustHave(".outline-due-chip")
	if got := htmlassert.Text(chip); got != "2026-03-05" {
		t.Errorf("chip text = %q, want 2026-03-05", got)
	}

	s.submit(t, s.alice, "/notes/"+itoa(id)+"/due", url.Values{
		"root": {"0"}, "due": {""},
	}, "/notes/")
	s.get(t, s.alice, "/notes/").MustNotHave(".outline-due-chip")
}

func TestDueRejectsBadFormat(t *testing.T) {
	s := newServer(t)
	id := s.seed(t, s.alice, notes.RootID, "task")

	rec := s.post(t, s.alice, "/notes/"+itoa(id)+"/due", url.Values{
		"root": {"0"}, "due": {"not-a-date"},
	})
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

func TestDueOnAnotherUsersBulletIs404(t *testing.T) {
	s := newServer(t)
	id := s.seed(t, s.bob, notes.RootID, "bob's")

	rec := s.post(t, s.alice, "/notes/"+itoa(id)+"/due", url.Values{
		"root": {"0"}, "due": {"2026-03-05"},
	})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

// TestOverdueChipIsMarked doesn't depend on the real clock: it sets a due
// date far enough in the past (year 2000) that it will read as overdue for
// the entire lifetime of this test suite.
func TestOverdueChipIsMarked(t *testing.T) {
	s := newServer(t)
	id := s.seed(t, s.alice, notes.RootID, "task")
	if err := s.store.SetDue(context.Background(), s.alice.user.ID, id, "2000-01-01"); err != nil {
		t.Fatal(err)
	}

	doc := s.get(t, s.alice, "/notes/")
	doc.MustHave(".outline-due-overdue")
}

func TestAFutureDueChipIsNotMarkedOverdue(t *testing.T) {
	s := newServer(t)
	id := s.seed(t, s.alice, notes.RootID, "task")
	future := time.Now().AddDate(1, 0, 0).Format("2006-01-02")
	if err := s.store.SetDue(context.Background(), s.alice.user.ID, id, future); err != nil {
		t.Fatal(err)
	}

	s.get(t, s.alice, "/notes/").MustNotHave(".outline-due-overdue")
}

// TestShowCompletedHidesAndReveals is spec §11 end to end: a done bullet's
// whole subtree disappears from the outline until the preference is on.
func TestShowCompletedHidesAndReveals(t *testing.T) {
	s := newServer(t)
	ctx := context.Background()
	parent := s.seed(t, s.alice, notes.RootID, "parent")
	s.seed(t, s.alice, parent, "child")
	if err := s.store.SetDone(ctx, s.alice.user.ID, parent, true); err != nil {
		t.Fatal(err)
	}

	doc := s.get(t, s.alice, "/notes/")
	if strings.Contains(doc.Text(), "child") {
		t.Error("a done bullet's child is visible with show-completed off")
	}
	doc.MustNotHave(`input[name=title]`) // the done parent itself is gone too

	req := httptest.NewRequest("GET", "/notes/", nil)
	req.AddCookie(&http.Cookie{Name: notes.ShowCompletedCookie, Value: "1"})
	rec := s.do(t, s.alice, req)
	if !strings.Contains(rec.Body.String(), "child") {
		t.Error("show-completed=1 still hides the done bullet's child")
	}
}

func TestPrefsTogglesTheCookie(t *testing.T) {
	s := newServer(t)

	rec := s.post(t, s.alice, "/notes/prefs", url.Values{
		"root": {"0"}, "show_completed": {"1"},
	})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303", rec.Code)
	}
	var got *http.Cookie
	for _, c := range rec.Result().Cookies() {
		if c.Name == notes.ShowCompletedCookie {
			got = c
		}
	}
	if got == nil || got.Value != "1" {
		t.Fatalf("show-completed cookie = %+v, want value 1", got)
	}
	// A preference, not a session fact: it has to survive the browser
	// closing, which a cookie with no MaxAge does not.
	if got.MaxAge <= 0 {
		t.Errorf("show-completed cookie MaxAge = %d, want a durable positive value", got.MaxAge)
	}
}

// TestPrefsRespondsWithTheFreshValueOverHTMX guards the staleness trap: the
// fragment this returns must reflect the setting just toggled, not whatever
// the request's own (pre-toggle) cookie said.
func TestPrefsRespondsWithTheFreshValueOverHTMX(t *testing.T) {
	s := newServer(t)
	ctx := context.Background()
	parent := s.seed(t, s.alice, notes.RootID, "parent")
	s.seed(t, s.alice, parent, "child")
	if err := s.store.SetDone(ctx, s.alice.user.ID, parent, true); err != nil {
		t.Fatal(err)
	}

	rec := s.postHX(t, s.alice, "/notes/prefs", url.Values{
		"root": {"0"}, "show_completed": {"1"},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "child") {
		t.Error("toggling show-completed on did not reveal the child in the same response")
	}
}

// TestPrefsFragmentRefreshesTheToggleOutOfBand guards the other half of the
// staleness trap. The toggle button sits outside #outline, so the response's
// normal swap cannot reach it; without an out-of-band copy the label and
// value would still say "Show completed" / 1 after the toggle had already
// turned completed bullets on, and a second click would re-send 1 and look
// like it did nothing.
func TestPrefsFragmentRefreshesTheToggleOutOfBand(t *testing.T) {
	s := newServer(t)

	rec := s.postHX(t, s.alice, "/notes/prefs", url.Values{
		"root": {"0"}, "show_completed": {"1"},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	doc := htmlassert.Parse(t, rec.Body.String())
	btn := doc.MustHave("#show-completed-toggle")
	if got, _ := htmlassert.Attr(btn, "hx-swap-oob"); got != "true" {
		t.Errorf("toggle hx-swap-oob = %q, want \"true\"", got)
	}
	if got, _ := htmlassert.Attr(btn, "value"); got != "0" {
		t.Errorf("toggle value = %q, want \"0\" — it still offers to turn completed back on", got)
	}
	if got := strings.TrimSpace(htmlassert.Text(btn)); got != "Hide completed" {
		t.Errorf("toggle label = %q, want \"Hide completed\"", got)
	}

	// And back off again, so the block is not simply hardcoded one way.
	rec = s.postHX(t, s.alice, "/notes/prefs", url.Values{
		"root": {"0"}, "show_completed": {"0"},
	})
	btn = htmlassert.Parse(t, rec.Body.String()).MustHave("#show-completed-toggle")
	if got, _ := htmlassert.Attr(btn, "value"); got != "1" {
		t.Errorf("toggle value after turning off = %q, want \"1\"", got)
	}
	if got := strings.TrimSpace(htmlassert.Text(btn)); got != "Show completed" {
		t.Errorf("toggle label after turning off = %q, want \"Show completed\"", got)
	}
}

// TestOutlinePageToggleIsNotOutOfBand is the counterpart discipline the N4
// rendered-title block already follows: a full page render must not carry
// hx-swap-oob, or htmx would lift the toggle out of a page it never swaps.
func TestOutlinePageToggleIsNotOutOfBand(t *testing.T) {
	s := newServer(t)
	btn := s.get(t, s.alice, "/notes/").MustHave("#show-completed-toggle")
	if _, ok := htmlassert.Attr(btn, "hx-swap-oob"); ok {
		t.Error("the page render's toggle carries hx-swap-oob")
	}
}

// TestMutationFragmentCarriesTheToggleUnchanged: a structural op does not
// touch the preference, so its out-of-band toggle must repeat the state the
// request came in with rather than flip it.
func TestMutationFragmentCarriesTheToggleUnchanged(t *testing.T) {
	s := newServer(t)
	id := s.seed(t, s.alice, notes.RootID, "a")

	req := httptest.NewRequest("POST", "/notes/"+itoa(id)+"/indent",
		strings.NewReader(url.Values{"root": {"0"}, web.CSRFFormField: {s.csrfToken(t, s.alice)}}.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("HX-Request", "true")
	req.AddCookie(&http.Cookie{Name: notes.ShowCompletedCookie, Value: "1"})
	rec := s.do(t, s.alice, req)

	btn := htmlassert.Parse(t, rec.Body.String()).MustHave("#show-completed-toggle")
	if got, _ := htmlassert.Attr(btn, "value"); got != "0" {
		t.Errorf("toggle value = %q, want \"0\" — show-completed was on for this request", got)
	}
}

func TestPrefsRejectsAnUnknownValue(t *testing.T) {
	s := newServer(t)
	for _, v := range []string{"", "true", "2"} {
		rec := s.post(t, s.alice, "/notes/prefs", url.Values{
			"root": {"0"}, "show_completed": {v},
		})
		if rec.Code != http.StatusBadRequest {
			t.Errorf("show_completed=%q gave %d, want 400", v, rec.Code)
		}
	}
}

func TestEveryMutationRequiresSignInIncludesDoneAndDue(t *testing.T) {
	s := newServer(t)
	id := s.seed(t, s.alice, notes.RootID, "a")

	for _, path := range []string{
		"/notes/" + itoa(id) + "/done",
		"/notes/" + itoa(id) + "/due",
	} {
		req := httptest.NewRequest("POST", path, strings.NewReader(""))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		rec := s.do(t, nil, req)
		if rec.Code != http.StatusForbidden {
			t.Errorf("POST %s anonymous and tokenless = %d, want 403 from the CSRF check", path, rec.Code)
		}
	}
}

func TestScriptIsServedWithAJavaScriptContentType(t *testing.T) {
	s := newServer(t)
	rec := s.do(t, s.alice, httptest.NewRequest("GET", "/notes/notes.js", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /notes/notes.js = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/javascript") {
		t.Errorf("Content-Type = %q, want text/javascript", ct)
	}
	if !strings.Contains(rec.Body.String(), "use strict") {
		t.Error("the served script does not look like notes.js")
	}
}

func TestDueListGroupsAcrossTheWholeTree(t *testing.T) {
	s := newServer(t)
	ctx := context.Background()
	parent := s.seed(t, s.alice, notes.RootID, "Projects")
	child := s.seed(t, s.alice, parent, "AtBudget")
	if err := s.store.SetDue(ctx, s.alice.user.ID, child, "2000-01-01"); err != nil {
		t.Fatal(err)
	}

	doc := s.get(t, s.alice, "/notes/due")
	doc.MustHave(".outline-due-overdue")
	if !strings.Contains(doc.Text(), "Projects") {
		t.Error("the due bullet's ancestor breadcrumb is missing")
	}
	link := doc.MustHave(`a[href=/notes/` + itoa(child) + `]`)
	if got := htmlassert.Text(link); got != "AtBudget" {
		t.Errorf("due list link text = %q, want AtBudget", got)
	}
}

func TestDueListRendersAnotherUsersNodesNowhere(t *testing.T) {
	s := newServer(t)
	ctx := context.Background()
	bobs := s.seed(t, s.bob, notes.RootID, "bob's task")
	if err := s.store.SetDue(ctx, s.bob.user.ID, bobs, "2000-01-01"); err != nil {
		t.Fatal(err)
	}

	if strings.Contains(s.get(t, s.alice, "/notes/due").Text(), "bob's task") {
		t.Error("another user's due bullet is on the page")
	}
}

func TestDueListRequiresSignIn(t *testing.T) {
	s := newServer(t)
	rec := s.do(t, nil, httptest.NewRequest("GET", "/notes/due", nil))
	if rec.Code != http.StatusSeeOther {
		t.Errorf("GET /notes/due anonymous = %d, want a 303 to the login page", rec.Code)
	}
}

func TestDueListWithNothingDue(t *testing.T) {
	s := newServer(t)
	s.seed(t, s.alice, notes.RootID, "no date on this one")

	if got := s.get(t, s.alice, "/notes/due").Text(); !strings.Contains(got, "Nothing is due") {
		t.Errorf("empty due list text = %q, want the empty-state line", got)
	}
}

func TestTheOutlineLinksToTheDueList(t *testing.T) {
	s := newServer(t)
	s.get(t, s.alice, "/notes/").MustHave(`.notes-toolbar a[href=/notes/due]`)
}

func TestSearchFindsABulletAndShowsItsBreadcrumb(t *testing.T) {
	s := newServer(t)
	parent := s.seed(t, s.alice, notes.RootID, "Projects")
	// A word FTS5's default unicode61 tokenizer treats as its own token, not
	// "AtBudget report" (the plan's literal example): that camelCase run
	// tokenizes as one "atbudget" token, which "budget" alone never matches.
	// See task-2-report.md for the full account of this deviation.
	child := s.seed(t, s.alice, parent, "Budget report")

	doc := s.get(t, s.alice, "/notes/search?q=budget")
	if !strings.Contains(doc.Text(), "Projects") {
		t.Error("the hit's ancestor breadcrumb is missing")
	}
	link := doc.MustHave(`a[href=/notes/` + itoa(child) + `]`)
	if got := htmlassert.Text(link); got != "Budget report" {
		t.Errorf("search hit link text = %q", got)
	}
}

func TestSearchWithNoQueryShowsNoResults(t *testing.T) {
	s := newServer(t)
	s.seed(t, s.alice, notes.RootID, "anything")

	doc := s.get(t, s.alice, "/notes/search")
	doc.MustNotHave(".notes-search-item")
}

func TestSearchWithNoMatchesSaysSo(t *testing.T) {
	s := newServer(t)
	doc := s.get(t, s.alice, "/notes/search?q=nonexistent")
	if !strings.Contains(doc.Text(), "No matches") {
		t.Error("an empty result set shows no feedback")
	}
}

func TestSearchDoesNotRenderAnotherUsersNodes(t *testing.T) {
	s := newServer(t)
	s.seed(t, s.bob, notes.RootID, "bob's secret plan")

	doc := s.get(t, s.alice, "/notes/search?q=secret")
	doc.MustNotHave(".notes-search-item")
}

func TestSearchRequiresSignIn(t *testing.T) {
	s := newServer(t)
	rec := s.do(t, nil, httptest.NewRequest("GET", "/notes/search", nil))
	if rec.Code != http.StatusSeeOther {
		t.Errorf("GET /notes/search anonymous = %d, want a 303 to the login page", rec.Code)
	}
}

// TestTagChipNowResolves closes N4's own follow-up: the #tag chip has
// always linked to /notes/search?q=..., 404ing until this chunk existed to
// answer it.
func TestTagChipNowResolves(t *testing.T) {
	s := newServer(t)
	s.seed(t, s.alice, notes.RootID, "check #urgent today")

	doc := s.get(t, s.alice, "/notes/")
	chip := doc.MustHave(".outline-tag")
	href, _ := htmlassert.Attr(chip, "href")

	rec := s.do(t, s.alice, httptest.NewRequest("GET", href, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET %s = %d, want 200", href, rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "check") {
		t.Error("following the tag chip does not find the bullet that produced it")
	}
}
