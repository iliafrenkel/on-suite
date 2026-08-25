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
	if n := len(doc.QueryAll("input.outline-title")); n != 0 {
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
