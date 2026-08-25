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

// TestNotesRequiresSignIn confirms the default-deny router covers every route
// this chunk adds. A route accidentally registered with Public would show up
// here as a 200 instead of a redirect to the login page.
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
	if _, ok := htmlassert.Attr(doc.MustHave(".outline-chevron"), "aria-expanded"); !ok {
		t.Error("a collapsed bullet renders no expand control")
	}
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
