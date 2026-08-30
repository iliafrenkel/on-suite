package notes_test

import (
	"bytes"
	"context"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"golang.org/x/net/html"

	"github.com/iliafrenkel/on-suite/internal/apps/notes"
	"github.com/iliafrenkel/on-suite/internal/apptest"
	"github.com/iliafrenkel/on-suite/internal/htmlassert"
	"github.com/iliafrenkel/on-suite/internal/platform/web"
)

// server is notes' own test harness, built on the shared apptest.Server —
// issue #50: this and internal/apps/paste's own handlers_test.go used to
// each carry an almost line-for-line duplicate of newServer/session/do/
// get/post/submit/logIn. seed and titlesAt below are notes-specific
// shortcuts that stay here, alongside the rest of what only this package's
// tests need — apptest has no notion of a "bullet".
type server struct {
	*apptest.Server[*notes.Store]
}

// session holds one signed-in browser's cookies.
type session = apptest.Session

func newServer(t *testing.T) *server {
	t.Helper()
	return &server{apptest.NewServer(t, notes.New(), notes.NewStore)}
}

// seed creates a bullet straight through the store, for tests about reading.
// Tests about writing go through the routes instead.
func (s *server) seed(t *testing.T, sess *session, parentID int64, title string) int64 {
	t.Helper()
	n, err := s.Store.Create(context.Background(), sess.User.ID, parentID, 1<<30, title, "")
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
	children, err := s.Store.Children(context.Background(), sess.User.ID, parentID)
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

// within reports whether node is ancestor or a descendant of it — used to
// check which container (menu vs. overlay) a given button actually lives
// in, since both contain buttons matching the same CSS selectors.
func within(node, ancestor *html.Node) bool {
	for n := node; n != nil; n = n.Parent {
		if n == ancestor {
			return true
		}
	}
	return false
}

// ---- tests ----------------------------------------------------------------

// TestNotesRequiresSignIn confirms the default-deny router covers this
// chunk's GET routes. A route accidentally registered with Public would show
// up here as a 200 instead of a redirect to the login page. The POST routes
// are covered separately by TestEveryMutationRequiresSignIn.
func TestNotesRequiresSignIn(t *testing.T) {
	s := newServer(t)

	for _, path := range []string{"/notes/", "/notes/1"} {
		rec := s.Do(t, nil, httptest.NewRequest("GET", path, nil))
		if rec.Code != http.StatusSeeOther {
			t.Errorf("GET %s anonymous = %d, want a 303 to the login page", path, rec.Code)
		}
	}
}

func TestOutlineRendersInsideTheShell(t *testing.T) {
	s := newServer(t)
	doc := s.Get(t, s.Alice, "/notes/")

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
	parent := s.seed(t, s.Alice, notes.RootID, "Projects")
	s.seed(t, s.Alice, parent, "AtBudget")
	s.seed(t, s.Alice, notes.RootID, "Reading")

	doc := s.Get(t, s.Alice, "/notes/")

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

	// The row's main form still carries its CSRF token — delete moved into
	// this form in an earlier commit, and this is the only render-level
	// assertion that a token is actually on the page.
	doc.MustHave("form.outline-main input[name=csrf_token]")

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

// TestRowFormDegradesEnterToAppend is issue #57: with JS disabled, pressing
// Enter in a title field implicitly submits the form's first non-disabled
// submit button in tree order — regardless of whether that button sits
// inside a closed <details> menu. A hidden append button must therefore be
// the form's first submit-capable child, or Enter silently fires whatever
// menu action happens to come first (e.g. "Mark done").
func TestRowFormDegradesEnterToAppend(t *testing.T) {
	s := newServer(t)
	s.seed(t, s.Alice, notes.RootID, "Projects")

	doc := s.Get(t, s.Alice, "/notes/")

	buttons := doc.QueryAll("form.outline-main button[type=submit]")
	if len(buttons) == 0 {
		t.Fatal("row form has no submit buttons")
	}
	if got, _ := htmlassert.Attr(buttons[0], "formaction"); got != "/notes/new" {
		t.Errorf("first submit button in the row form has formaction %q, want /notes/new (issue #57: Enter must degrade to append)", got)
	}
}

func TestOutlineRendersAnotherUsersTreeNowhere(t *testing.T) {
	s := newServer(t)
	s.seed(t, s.Bob, notes.RootID, "bob's secret")

	doc := s.Get(t, s.Alice, "/notes/")
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
	parent := s.seed(t, s.Alice, notes.RootID, "Projects")
	s.seed(t, s.Alice, parent, "AtBudget")

	if err := s.Store.SetCollapsed(context.Background(), s.Alice.User.ID, parent, true); err != nil {
		t.Fatal(err)
	}

	doc := s.Get(t, s.Alice, "/notes/")
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

// TestParentOfOnlyDoneChildRendersNoChevron is issue #81, end to end: with
// "show completed" off, a parent whose only child is done must render no
// expand chevron at all — one that did would toggle with no visible effect,
// since hideDone (view.go) has already dropped that child from the render.
func TestParentOfOnlyDoneChildRendersNoChevron(t *testing.T) {
	s := newServer(t)
	parent := s.seed(t, s.Alice, notes.RootID, "parent")
	child := s.seed(t, s.Alice, parent, "child")
	s.Submit(t, s.Alice, "/notes/"+itoa(child)+"/done", url.Values{
		"root": {"0"}, "done": {"1"},
	}, "/notes/")

	doc := s.Get(t, s.Alice, "/notes/")
	doc.MustNotHave("button.outline-chevron")

	req := httptest.NewRequest("GET", "/notes/", nil)
	req.AddCookie(&http.Cookie{Name: notes.ShowCompletedCookie, Value: "1"})
	rec := s.Do(t, s.Alice, req)
	got := htmlassert.Parse(t, rec.Body.String())
	got.MustHave("button.outline-chevron")
}

// TestLeafBulletRendersNoChevronButton is issue #63: notes.js's
// collapseButton (and maybeDeleteEmptyBullet, which uses it to detect "has
// children") finds a row's chevron via row.querySelector("button.outline-chevron")
// — it depends on outline.html's childless-bullet branch rendering a plain
// span, never a button. If that branch ever became a <button>, collapseButton
// would report "has children" for every leaf and Backspace-to-delete would
// silently stop working everywhere, with every other current test staying
// green — nothing else pins this half of the contract.
func TestLeafBulletRendersNoChevronButton(t *testing.T) {
	s := newServer(t)
	s.seed(t, s.Alice, notes.RootID, "leaf")

	doc := s.Get(t, s.Alice, "/notes/")
	doc.MustNotHave("button.outline-chevron")
}

// TestOutlinePageLoadsTheScript. notes.js was dead code for three commits
// because nothing on the page loaded it: the route served it correctly the
// whole time. Serving it and loading it are separate claims, so this asserts
// the second one.
func TestOutlinePageLoadsTheScript(t *testing.T) {
	s := newServer(t)
	doc := s.Get(t, s.Alice, "/notes/")
	doc.MustHave("script[src=/notes/notes.js]")
}

// TestBulletControlsAreDisabledWhereTheOperationIsANoOp. The store treats all
// four as no-ops rather than errors, so this is honesty rather than
// enforcement: a button that cannot do anything should not look like it can.
func TestBulletControlsAreDisabledAtTheEdges(t *testing.T) {
	s := newServer(t)
	s.seed(t, s.Alice, notes.RootID, "first")
	s.seed(t, s.Alice, notes.RootID, "second")

	doc := s.Get(t, s.Alice, "/notes/")

	// Two flat bullets, each with move buttons in the "···" menu
	// (.outline-menu-list). Rather than assume a DOM order for the flat list of
	// matches (brittle to markup reordering), within() pins each button to
	// its actual row and container.
	rows := doc.QueryAll(".outline-row")
	menus := doc.QueryAll(".outline-menu-list")
	ups := doc.QueryAll(`button[value=up]`)
	downs := doc.QueryAll(`button[value=down]`)
	if len(rows) != 2 || len(menus) != 2 || len(ups) != 2 || len(downs) != 2 {
		t.Fatalf("got %d rows, %d menus, %d move-up and %d move-down buttons, want 2, 2, 2, 2",
			len(rows), len(menus), len(ups), len(downs))
	}

	withinAll := func(nodes []*html.Node, ancestor *html.Node) []*html.Node {
		var out []*html.Node
		for _, n := range nodes {
			if within(n, ancestor) {
				out = append(out, n)
			}
		}
		return out
	}
	find := func(t *testing.T, nodes []*html.Node, container *html.Node, what string) *html.Node {
		t.Helper()
		matches := withinAll(nodes, container)
		if len(matches) != 1 {
			t.Fatalf("got %d %s buttons in container, want 1", len(matches), what)
		}
		return matches[0]
	}

	check := func(label string, row *html.Node, wantUpDisabled, wantDownDisabled bool) {
		menu := find(t, menus, row, "menu")

		menuUp := find(t, ups, menu, "menu move-up")
		menuDown := find(t, downs, menu, "menu move-down")

		if _, ok := htmlassert.Attr(menuUp, "disabled"); ok != wantUpDisabled {
			t.Errorf("the %s bullet's menu move-up disabled=%v, want %v", label, ok, wantUpDisabled)
		}
		if _, ok := htmlassert.Attr(menuDown, "disabled"); ok != wantDownDisabled {
			t.Errorf("the %s bullet's menu move-down disabled=%v, want %v", label, ok, wantDownDisabled)
		}
	}

	check("first", rows[0], true, false)
	check("second", rows[1], false, true)
}

// TestOutlineUsesNoInlineStyles. The CSP has no unsafe-inline: a style
// attribute would simply not apply, so indentation must come from nesting.
func TestOutlineUsesNoInlineStyles(t *testing.T) {
	s := newServer(t)
	parent := s.seed(t, s.Alice, notes.RootID, "Projects")
	s.seed(t, s.Alice, parent, "AtBudget")

	rec := s.Do(t, s.Alice, httptest.NewRequest("GET", "/notes/", nil))
	if strings.Contains(rec.Body.String(), "style=") {
		t.Error("the outline contains an inline style attribute, which the CSP blocks")
	}
}

// TestEmptyOutlineUsesNoInlineStyles is issue #54: TestOutlineUsesNoInlineStyles
// above only ever renders a populated outline, so it never exercises
// outline-first, the empty-outline "Start typing…" placeholder added in N2
// Task 6. Manually grep-verified to have no style= attribute, but nothing
// pinned it.
func TestEmptyOutlineUsesNoInlineStyles(t *testing.T) {
	s := newServer(t)

	rec := s.Do(t, s.Alice, httptest.NewRequest("GET", "/notes/", nil))
	if strings.Contains(rec.Body.String(), "style=") {
		t.Error("the empty outline contains an inline style attribute, which the CSP blocks")
	}
}

func TestOutlineEscapesBulletText(t *testing.T) {
	s := newServer(t)
	s.seed(t, s.Alice, notes.RootID, `<script>alert(1)</script>`)

	rec := s.Do(t, s.Alice, httptest.NewRequest("GET", "/notes/", nil))
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
	id := s.seed(t, s.Alice, notes.RootID, "**bold** and #tag")

	rec := s.Do(t, s.Alice, httptest.NewRequest("GET", "/notes/", nil))
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
	first := s.seed(t, s.Alice, notes.RootID, "**first**")
	second := s.seed(t, s.Alice, notes.RootID, "second")

	page := s.Do(t, s.Alice, httptest.NewRequest("GET", "/notes/", nil)).Body.String()
	if !strings.Contains(page, `id="rendered-title-`+itoa(first)+`"`) {
		t.Fatalf("no rendered overlay on the page for bullet %d", first)
	}
	if oobOverlays(t, page) {
		t.Error("a full page load carries hx-swap-oob on the rendered overlays")
	}

	// The same rows, this time as the fragment a structural operation returns.
	frag := s.PostHX(t, s.Alice, "/notes/"+itoa(second)+"/indent", url.Values{
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
	s.seed(t, s.Alice, notes.RootID, `<script>alert(1)</script>`)

	rec := s.Do(t, s.Alice, httptest.NewRequest("GET", "/notes/", nil))
	if strings.Contains(rec.Body.String(), "<script>alert(1)</script>") {
		t.Error("bullet text reached the rendered overlay unescaped")
	}
}

func TestZoomShowsOnlyTheSubtree(t *testing.T) {
	s := newServer(t)
	projects := s.seed(t, s.Alice, notes.RootID, "Projects")
	s.seed(t, s.Alice, projects, "AtBudget")
	s.seed(t, s.Alice, notes.RootID, "Reading")

	doc := s.Get(t, s.Alice, "/notes/"+itoa(projects))

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
	projects := s.seed(t, s.Alice, notes.RootID, "Projects")
	budget := s.seed(t, s.Alice, projects, "AtBudget")
	api := s.seed(t, s.Alice, budget, "API")

	doc := s.Get(t, s.Alice, "/notes/"+itoa(api))
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

// TestZoomedHeadingRendersMarkdown is issue #65: the zoomed heading and the
// current (non-link) breadcrumb segment are not themselves links, so
// rendering a bullet's Markdown there can't nest an <a> — unlike the
// breadcrumb's ancestor links, which stay on plain DisplayTitle (see the
// template comment beside them).
func TestZoomedHeadingRendersMarkdown(t *testing.T) {
	s := newServer(t)
	id := s.seed(t, s.Alice, notes.RootID, "**Projects**")

	doc := s.Get(t, s.Alice, "/notes/"+itoa(id))

	h1 := doc.MustHave("h1")
	if got := htmlassert.Text(h1); got != "Projects" {
		t.Errorf("h1 text = %q, want the rendered form", got)
	}
	rec := s.Do(t, s.Alice, httptest.NewRequest("GET", "/notes/"+itoa(id), nil))
	if !strings.Contains(rec.Body.String(), "<h1><strong>Projects</strong></h1>") {
		t.Error("the zoomed heading did not render as bold HTML")
	}

	current := doc.MustHave(".outline-crumb-current")
	if got := htmlassert.Text(current); got != "Projects" {
		t.Errorf("current crumb text = %q, want the rendered form", got)
	}
}

// TestBreadcrumbAncestorLinksStayPlainText is issue #65's own noted tension:
// an ancestor breadcrumb is a link, and Render can itself emit an <a> (for a
// link, autolink, or #tag/@mention) — nesting anchors is invalid HTML, so
// these deliberately do not render Markdown.
func TestBreadcrumbAncestorLinksStayPlainText(t *testing.T) {
	s := newServer(t)
	projects := s.seed(t, s.Alice, notes.RootID, "**Projects**")
	child := s.seed(t, s.Alice, projects, "child")

	doc := s.Get(t, s.Alice, "/notes/"+itoa(child))
	links := doc.QueryAll("nav.outline-crumbs a")
	if len(links) < 2 {
		t.Fatalf("got %d breadcrumb links, want at least 2", len(links))
	}
	if got := htmlassert.Text(links[1]); got != "**Projects**" {
		t.Errorf("ancestor crumb link text = %q, want the literal source", got)
	}
}

func TestTopLevelHasNoBreadcrumb(t *testing.T) {
	s := newServer(t)
	s.seed(t, s.Alice, notes.RootID, "Projects")

	s.Get(t, s.Alice, "/notes/").MustNotHave("nav.outline-crumbs")
}

func TestZoomingIntoAnotherUsersNodeIs404(t *testing.T) {
	s := newServer(t)
	id := s.seed(t, s.Bob, notes.RootID, "bob's secret")

	rec := s.Do(t, s.Alice, httptest.NewRequest("GET", "/notes/"+itoa(id), nil))
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
		rec := s.Do(t, s.Alice, httptest.NewRequest("GET", path, nil))
		if rec.Code != http.StatusNotFound {
			t.Errorf("GET %s = %d, want 404", path, rec.Code)
		}
	}
}

func TestBulletDotZoomsIn(t *testing.T) {
	s := newServer(t)
	id := s.seed(t, s.Alice, notes.RootID, "Projects")

	doc := s.Get(t, s.Alice, "/notes/")
	href, _ := htmlassert.Attr(doc.MustHave("a.outline-dot"), "href")
	if href != "/notes/"+itoa(id) {
		t.Errorf("the bullet dot points at %q, want /notes/%d", href, id)
	}
}

func TestSetTextSavesTheBullet(t *testing.T) {
	s := newServer(t)
	id := s.seed(t, s.Alice, notes.RootID, "old")

	s.Submit(t, s.Alice, "/notes/"+itoa(id)+"/text", url.Values{
		"root": {"0"}, "focus_id": {itoa(id)},
		"title": {"new"}, "note": {"a note"},
	}, "/notes/")

	n, err := s.Store.ByID(context.Background(), s.Alice.User.ID, id)
	if err != nil {
		t.Fatal(err)
	}
	if n.Title != "new" || n.Note != "a note" {
		t.Errorf("saved %q / %q, want new / a note", n.Title, n.Note)
	}
}

func TestSetTextReturnsToTheZoomItCameFrom(t *testing.T) {
	s := newServer(t)
	root := s.seed(t, s.Alice, notes.RootID, "Projects")
	child := s.seed(t, s.Alice, root, "AtBudget")

	s.Submit(t, s.Alice, "/notes/"+itoa(child)+"/text", url.Values{
		"root": {itoa(root)}, "title": {"renamed"}, "note": {""},
	}, "/notes/"+itoa(root))
}

func TestSetTextOnAnotherUsersBulletIs404(t *testing.T) {
	s := newServer(t)
	id := s.seed(t, s.Bob, notes.RootID, "bob's")

	rec := s.Post(t, s.Alice, "/notes/"+itoa(id)+"/text", url.Values{
		"root": {"0"}, "title": {"stolen"}, "note": {""},
	})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}

	n, err := s.Store.ByID(context.Background(), s.Bob.User.ID, id)
	if err != nil {
		t.Fatal(err)
	}
	if n.Title != "bob's" {
		t.Errorf("bob's bullet is now %q", n.Title)
	}
}

func TestMutationsWithoutACSRFTokenAreRejected(t *testing.T) {
	s := newServer(t)
	id := s.seed(t, s.Alice, notes.RootID, "Projects")

	form := url.Values{"root": {"0"}, "title": {"changed"}, "note": {""}}
	req := httptest.NewRequest("POST", "/notes/"+itoa(id)+"/text", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := s.Do(t, s.Alice, req)

	if rec.Code == http.StatusSeeOther {
		t.Fatal("a POST with no CSRF token succeeded")
	}
	n, err := s.Store.ByID(context.Background(), s.Alice.User.ID, id)
	if err != nil {
		t.Fatal(err)
	}
	if n.Title != "Projects" {
		t.Errorf("the bullet was changed to %q", n.Title)
	}
}

func TestMalformedRootIsRejected(t *testing.T) {
	s := newServer(t)
	id := s.seed(t, s.Alice, notes.RootID, "Projects")

	rec := s.Post(t, s.Alice, "/notes/"+itoa(id)+"/text", url.Values{
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
	id := s.seed(t, s.Alice, notes.RootID, "Projects")

	rec := s.Post(t, s.Alice, "/notes/"+itoa(id)+"/text", url.Values{
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
	doc := s.Get(t, s.Alice, "/notes/")

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

// TestAllDoneOutlineDoesNotShowTheEmptyPlaceholder is issue #75: an outline
// whose every top-level bullet is done and hidden must not look identical to
// a genuinely empty one — that reads as data loss, not "nothing left to do".
func TestAllDoneOutlineDoesNotShowTheEmptyPlaceholder(t *testing.T) {
	s := newServer(t)
	id := s.seed(t, s.Alice, notes.RootID, "task")
	s.Submit(t, s.Alice, "/notes/"+itoa(id)+"/done", url.Values{
		"root": {"0"}, "done": {"1"},
	}, "/notes/")

	doc := s.Get(t, s.Alice, "/notes/")
	doc.MustNotHave(`input[name=new_title]`)
	doc.MustHave(".outline-all-done")

	// The inline toggle must work with JS off: a real form posting to the
	// same /notes/prefs route the toolbar's own toggle uses.
	form := doc.MustHave(".outline-all-done form")
	if got, _ := htmlassert.Attr(form, "action"); got != "/notes/prefs" {
		t.Errorf("inline toggle form action = %q, want /notes/prefs", got)
	}
	doc.MustHave(`.outline-all-done button[name=show_completed]`)

	// Showing completed must bring the bullet back.
	req := httptest.NewRequest("GET", "/notes/", nil)
	req.AddCookie(&http.Cookie{Name: notes.ShowCompletedCookie, Value: "1"})
	rec := s.Do(t, s.Alice, req)
	got := htmlassert.Parse(t, rec.Body.String())
	got.MustHave(`input[value=task]`)
	got.MustNotHave(".outline-all-done")
}

func TestCreateFromTheEmptyOutline(t *testing.T) {
	s := newServer(t)

	s.Submit(t, s.Alice, "/notes/new", url.Values{
		"root": {"0"}, "new_title": {"first"},
	}, "/notes/")

	children, err := s.Store.Children(context.Background(), s.Alice.User.ID, notes.RootID)
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
	root := s.seed(t, s.Alice, notes.RootID, "Projects")

	s.Submit(t, s.Alice, "/notes/new", url.Values{
		"root": {itoa(root)}, "new_title": {"AtBudget"},
	}, "/notes/"+itoa(root))

	children, err := s.Store.Children(context.Background(), s.Alice.User.ID, root)
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
	first := s.seed(t, s.Alice, notes.RootID, "first")
	s.seed(t, s.Alice, notes.RootID, "third")

	s.Submit(t, s.Alice, "/notes/new", url.Values{
		"root": {"0"}, "focus_id": {itoa(first)},
		"title": {"first"}, "note": {""},
		"new_title": {"second"},
	}, "/notes/")

	if got := s.titlesAt(t, s.Alice, notes.RootID); !equalStrings(got, []string{"first", "second", "third"}) {
		t.Fatalf("children = %v, want [first second third]", got)
	}
}

// TestCreateSplitsTheFocusedBulletsText is spec §8's Enter, ahead of the
// keyboard that will send it: what stays and what moves are one write.
func TestCreateSplitsTheFocusedBulletsText(t *testing.T) {
	s := newServer(t)
	id := s.seed(t, s.Alice, notes.RootID, "hello world")

	s.Submit(t, s.Alice, "/notes/new", url.Values{
		"root": {"0"}, "focus_id": {itoa(id)},
		"title": {"hello"}, "note": {""},
		"new_title": {"world"},
	}, "/notes/")

	children, err := s.Store.Children(context.Background(), s.Alice.User.ID, notes.RootID)
	if err != nil {
		t.Fatal(err)
	}
	if len(children) != 2 || children[0].Title != "hello" || children[1].Title != "world" {
		t.Fatalf("children = %+v, want hello then world", children)
	}
}

func TestCreateUnderAnotherUsersFocusIs404(t *testing.T) {
	s := newServer(t)
	bobs := s.seed(t, s.Bob, notes.RootID, "bob's")

	rec := s.Post(t, s.Alice, "/notes/new", url.Values{
		"root": {"0"}, "focus_id": {itoa(bobs)},
		"title": {"stolen"}, "note": {""}, "new_title": {"x"},
	})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}

	// Nothing was created for alice, and nothing was changed for bob: the
	// whole transaction rolled back.
	alices, err := s.Store.Children(context.Background(), s.Alice.User.ID, notes.RootID)
	if err != nil {
		t.Fatal(err)
	}
	if len(alices) != 0 {
		t.Errorf("alice gained %d bullets", len(alices))
	}
	n, err := s.Store.ByID(context.Background(), s.Bob.User.ID, bobs)
	if err != nil {
		t.Fatal(err)
	}
	if n.Title != "bob's" {
		t.Errorf("bob's bullet is now %q", n.Title)
	}
}

// TestCreateUnderAnotherUsersForgedRootIs404 is issue #47, mirroring
// TestCreateUnderAnotherUsersFocusIs404 above: POST /notes/new with no
// focus_id uses the form-supplied root as the insertion parent directly
// (mutation.Root), so a forged root needs its own ownership-check test, not
// just the forged-focus_id case.
func TestCreateUnderAnotherUsersForgedRootIs404(t *testing.T) {
	s := newServer(t)
	bobs := s.seed(t, s.Bob, notes.RootID, "bob's")

	rec := s.Post(t, s.Alice, "/notes/new", url.Values{
		"root": {itoa(bobs)}, "new_title": {"stolen"},
	})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}

	alices, err := s.Store.Children(context.Background(), s.Alice.User.ID, notes.RootID)
	if err != nil {
		t.Fatal(err)
	}
	if len(alices) != 0 {
		t.Errorf("alice gained %d bullets", len(alices))
	}
	n, err := s.Store.ByID(context.Background(), s.Bob.User.ID, bobs)
	if err != nil {
		t.Fatal(err)
	}
	if n.Title != "bob's" {
		t.Errorf("bob's bullet is now %q", n.Title)
	}
}

func TestPlusButtonAddsAnEmptyBulletBelow(t *testing.T) {
	s := newServer(t)
	s.seed(t, s.Alice, notes.RootID, "Projects")

	doc := s.Get(t, s.Alice, "/notes/")
	btn := doc.MustHave(`button[formaction=/notes/new]`)
	if btn == nil {
		t.Fatal("a bullet offers no way to add one below it")
	}
}

func TestIndentNestsUnderThePreviousSibling(t *testing.T) {
	s := newServer(t)
	first := s.seed(t, s.Alice, notes.RootID, "first")
	second := s.seed(t, s.Alice, notes.RootID, "second")

	s.Submit(t, s.Alice, "/notes/"+itoa(second)+"/indent", url.Values{
		"root": {"0"}, "focus_id": {itoa(second)},
		"title": {"second"}, "note": {""},
	}, "/notes/")

	children, err := s.Store.Children(context.Background(), s.Alice.User.ID, first)
	if err != nil {
		t.Fatal(err)
	}
	if len(children) != 1 || children[0].ID != second {
		t.Fatalf("children of first = %+v, want just second", children)
	}

	// And the page now shows it nested.
	doc := s.Get(t, s.Alice, "/notes/")
	if n := len(doc.QueryAll(".outline-item .outline-item")); n != 1 {
		t.Errorf("got %d nested bullets on the page, want 1", n)
	}
}

func TestIndentOfTheFirstSiblingDoesNothing(t *testing.T) {
	s := newServer(t)
	first := s.seed(t, s.Alice, notes.RootID, "first")

	// Not an error: the caller is a keypress, and Tab on the first line of an
	// outline should do nothing rather than complain.
	s.Submit(t, s.Alice, "/notes/"+itoa(first)+"/indent", url.Values{
		"root": {"0"}, "focus_id": {itoa(first)}, "title": {"first"}, "note": {""},
	}, "/notes/")

	n, err := s.Store.ByID(context.Background(), s.Alice.User.ID, first)
	if err != nil {
		t.Fatal(err)
	}
	if n.ParentID != notes.RootID || n.Position != 0 {
		t.Errorf("first moved to parent %d position %d", n.ParentID, n.Position)
	}
}

func TestOutdentPromotesToTheParentsNextSibling(t *testing.T) {
	s := newServer(t)
	parent := s.seed(t, s.Alice, notes.RootID, "parent")
	child := s.seed(t, s.Alice, parent, "child")
	s.seed(t, s.Alice, notes.RootID, "after")

	s.Submit(t, s.Alice, "/notes/"+itoa(child)+"/outdent", url.Values{
		"root": {"0"}, "focus_id": {itoa(child)}, "title": {"child"}, "note": {""},
	}, "/notes/")

	top, err := s.Store.Children(context.Background(), s.Alice.User.ID, notes.RootID)
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
	id := s.seed(t, s.Alice, notes.RootID, "only")

	s.Submit(t, s.Alice, "/notes/"+itoa(id)+"/outdent", url.Values{
		"root": {"0"}, "focus_id": {itoa(id)}, "title": {"only"}, "note": {""},
	}, "/notes/")

	n, err := s.Store.ByID(context.Background(), s.Alice.User.ID, id)
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
		n, err := s.Store.Create(ctx, s.Alice.User.ID, parent, 1<<30, "d", "")
		if err != nil {
			t.Fatalf("building the chain at depth %d: %v", d, err)
		}
		parent, deepest = n.ID, n.ID
	}
	// A sibling of the deepest node; indenting it would put it one past the cap.
	deepestNode, err := s.Store.ByID(ctx, s.Alice.User.ID, deepest)
	if err != nil {
		t.Fatal(err)
	}
	sibling, err := s.Store.Create(ctx, s.Alice.User.ID, deepestNode.ParentID, 1<<30, "sibling", "")
	if err != nil {
		t.Fatal(err)
	}

	rec := s.Post(t, s.Alice, "/notes/"+itoa(sibling.ID)+"/indent", url.Values{
		"root": {"0"}, "focus_id": {itoa(sibling.ID)}, "title": {"sibling"}, "note": {""},
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}

	n, err := s.Store.ByID(ctx, s.Alice.User.ID, sibling.ID)
	if err != nil {
		t.Fatal(err)
	}
	if n.ParentID != deepestNode.ParentID {
		t.Errorf("the bullet moved despite the rejection")
	}
}

func TestIndentingAnotherUsersBulletIs404(t *testing.T) {
	s := newServer(t)
	s.seed(t, s.Bob, notes.RootID, "bob's first")
	second := s.seed(t, s.Bob, notes.RootID, "bob's second")

	rec := s.Post(t, s.Alice, "/notes/"+itoa(second)+"/indent", url.Values{
		"root": {"0"},
	})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}

	n, err := s.Store.ByID(context.Background(), s.Bob.User.ID, second)
	if err != nil {
		t.Fatal(err)
	}
	if n.ParentID != notes.RootID {
		t.Error("bob's bullet was indented by alice")
	}
}

func TestMoveUpAndDown(t *testing.T) {
	s := newServer(t)
	a := s.seed(t, s.Alice, notes.RootID, "a")
	b := s.seed(t, s.Alice, notes.RootID, "b")
	s.seed(t, s.Alice, notes.RootID, "c")

	s.Submit(t, s.Alice, "/notes/"+itoa(b)+"/move", url.Values{
		"root": {"0"}, "focus_id": {itoa(b)}, "title": {"b"}, "note": {""},
		"dir": {"up"},
	}, "/notes/")
	if got := s.titlesAt(t, s.Alice, notes.RootID); !equalStrings(got, []string{"b", "a", "c"}) {
		t.Fatalf("after move up: %v, want [b a c]", got)
	}

	s.Submit(t, s.Alice, "/notes/"+itoa(a)+"/move", url.Values{
		"root": {"0"}, "focus_id": {itoa(a)}, "title": {"a"}, "note": {""},
		"dir": {"down"},
	}, "/notes/")
	if got := s.titlesAt(t, s.Alice, notes.RootID); !equalStrings(got, []string{"b", "c", "a"}) {
		t.Fatalf("after move down: %v, want [b c a]", got)
	}
}

func TestMoveAtTheEdgesDoesNothing(t *testing.T) {
	s := newServer(t)
	a := s.seed(t, s.Alice, notes.RootID, "a")
	b := s.seed(t, s.Alice, notes.RootID, "b")

	for _, tc := range []struct {
		id  int64
		dir string
	}{{a, "up"}, {b, "down"}} {
		s.Submit(t, s.Alice, "/notes/"+itoa(tc.id)+"/move", url.Values{
			"root": {"0"}, "dir": {tc.dir},
		}, "/notes/")
	}
	if got := s.titlesAt(t, s.Alice, notes.RootID); !equalStrings(got, []string{"a", "b"}) {
		t.Fatalf("order changed to %v", got)
	}
}

func TestMoveRejectsAnUnknownDirection(t *testing.T) {
	s := newServer(t)
	id := s.seed(t, s.Alice, notes.RootID, "a")

	for _, dir := range []string{"", "sideways", "UP", "1"} {
		rec := s.Post(t, s.Alice, "/notes/"+itoa(id)+"/move", url.Values{
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
	parent := s.seed(t, s.Alice, notes.RootID, "parent")
	s.seed(t, s.Alice, parent, "child")

	s.Submit(t, s.Alice, "/notes/"+itoa(parent)+"/collapse", url.Values{
		"root": {"0"}, "focus_id": {itoa(parent)}, "title": {"parent"}, "note": {""},
		"collapsed": {"1"},
	}, "/notes/")

	n, err := s.Store.ByID(ctx, s.Alice.User.ID, parent)
	if err != nil {
		t.Fatal(err)
	}
	if !n.Collapsed {
		t.Fatal("the bullet is not collapsed")
	}
	if strings.Contains(s.Get(t, s.Alice, "/notes/").Text(), "child") {
		t.Error("a collapsed bullet's child is still in the response")
	}

	s.Submit(t, s.Alice, "/notes/"+itoa(parent)+"/collapse", url.Values{
		"root": {"0"}, "collapsed": {"0"},
	}, "/notes/")

	n, err = s.Store.ByID(ctx, s.Alice.User.ID, parent)
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
	parent := s.seed(t, s.Alice, notes.RootID, "parent")
	s.seed(t, s.Alice, parent, "child")

	for range 2 {
		s.Submit(t, s.Alice, "/notes/"+itoa(parent)+"/collapse", url.Values{
			"root": {"0"}, "collapsed": {"1"},
		}, "/notes/")
	}
	n, err := s.Store.ByID(context.Background(), s.Alice.User.ID, parent)
	if err != nil {
		t.Fatal(err)
	}
	if !n.Collapsed {
		t.Error("collapsing twice left the bullet expanded")
	}
}

func TestCollapseRejectsAnUnknownValue(t *testing.T) {
	s := newServer(t)
	id := s.seed(t, s.Alice, notes.RootID, "a")

	for _, v := range []string{"", "true", "yes", "2"} {
		rec := s.Post(t, s.Alice, "/notes/"+itoa(id)+"/collapse", url.Values{
			"root": {"0"}, "collapsed": {v},
		})
		if rec.Code != http.StatusBadRequest {
			t.Errorf("collapsed=%q gave %d, want 400", v, rec.Code)
		}
	}
}

func TestMovingAnotherUsersBulletIs404(t *testing.T) {
	s := newServer(t)
	s.seed(t, s.Bob, notes.RootID, "bob's a")
	second := s.seed(t, s.Bob, notes.RootID, "bob's b")

	for _, path := range []string{"/move", "/collapse"} {
		form := url.Values{"root": {"0"}, "dir": {"up"}, "collapsed": {"1"}}
		rec := s.Post(t, s.Alice, "/notes/"+itoa(second)+path, form)
		if rec.Code != http.StatusNotFound {
			t.Errorf("POST %s = %d, want 404", path, rec.Code)
		}
	}
	if got := s.titlesAt(t, s.Bob, notes.RootID); !equalStrings(got, []string{"bob's a", "bob's b"}) {
		t.Errorf("bob's outline is now %v", got)
	}
}

func TestDeleteRemovesTheBulletAndItsSubtree(t *testing.T) {
	s := newServer(t)
	ctx := context.Background()
	parent := s.seed(t, s.Alice, notes.RootID, "parent")
	child := s.seed(t, s.Alice, parent, "child")
	s.seed(t, s.Alice, notes.RootID, "survivor")

	s.Submit(t, s.Alice, "/notes/"+itoa(parent)+"/delete", url.Values{
		"root": {"0"},
	}, "/notes/")

	if got := s.titlesAt(t, s.Alice, notes.RootID); !equalStrings(got, []string{"survivor"}) {
		t.Fatalf("top level = %v, want [survivor]", got)
	}
	if _, err := s.Store.ByID(ctx, s.Alice.User.ID, child); err == nil {
		t.Error("the child outlived its parent")
	}
}

// TestDeleteRenumbersTheSurvivors: I1 says sibling positions are contiguous
// from zero, and a delete that leaves a gap makes every later clamp land one
// place off — silently, three moves later.
func TestDeleteRenumbersTheSurvivors(t *testing.T) {
	s := newServer(t)
	s.seed(t, s.Alice, notes.RootID, "a")
	b := s.seed(t, s.Alice, notes.RootID, "b")
	s.seed(t, s.Alice, notes.RootID, "c")

	s.Submit(t, s.Alice, "/notes/"+itoa(b)+"/delete", url.Values{"root": {"0"}}, "/notes/")

	children, err := s.Store.Children(context.Background(), s.Alice.User.ID, notes.RootID)
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
	root := s.seed(t, s.Alice, notes.RootID, "Projects")
	child := s.seed(t, s.Alice, root, "AtBudget")

	s.Submit(t, s.Alice, "/notes/"+itoa(child)+"/delete", url.Values{
		"root": {itoa(root)},
	}, "/notes/"+itoa(root))
}

func TestDeletingAnotherUsersBulletIs404(t *testing.T) {
	s := newServer(t)
	id := s.seed(t, s.Bob, notes.RootID, "bob's")

	rec := s.Post(t, s.Alice, "/notes/"+itoa(id)+"/delete", url.Values{"root": {"0"}})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
	if _, err := s.Store.ByID(context.Background(), s.Bob.User.ID, id); err != nil {
		t.Errorf("bob's bullet was deleted by alice: %v", err)
	}
}

// TestEveryStructuralButtonMirrorsItsFormactionAsHTMX. Progressive
// enhancement: hx-post always equals formaction, so a JS-disabled browser
// and an HTMX one issue the exact same request the button already
// declares — nothing in notes.js needs to know a URL.
func TestEveryStructuralButtonMirrorsItsFormactionAsHTMX(t *testing.T) {
	s := newServer(t)
	s.seed(t, s.Alice, notes.RootID, "a")

	doc := s.Get(t, s.Alice, "/notes/")
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
	id := s.seed(t, s.Alice, notes.RootID, "a")

	doc := s.Get(t, s.Alice, "/notes/")
	row := doc.MustHave(".outline-row")
	if got, _ := htmlassert.Attr(row, "data-id"); got != itoa(id) {
		t.Errorf("row data-id = %q, want %q", got, itoa(id))
	}
}

// TestTextInputsAutosaveOverHTMX. hx-swap=none: nothing on screen needs to
// change from a text-only save, the input already shows what was typed.
func TestTextInputsAutosaveOverHTMX(t *testing.T) {
	s := newServer(t)
	id := s.seed(t, s.Alice, notes.RootID, "a")

	doc := s.Get(t, s.Alice, "/notes/")
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

// TestEmptyOutlineFormIsHTMXWired is issue #59: outline.html's empty-outline
// form actually sets the full hx-post/hx-target="#outline"/hx-swap="innerHTML"
// triple, matching every other structural form — but until now this only
// asserted hx-post, leaving the other two an unpinned coverage gap.
func TestEmptyOutlineFormIsHTMXWired(t *testing.T) {
	s := newServer(t)
	doc := s.Get(t, s.Alice, "/notes/")
	form := doc.MustHave(`form[action=/notes/new]`)
	if got, _ := htmlassert.Attr(form, "hx-post"); got != "/notes/new" {
		t.Errorf("empty-outline form hx-post = %q", got)
	}
	if got, _ := htmlassert.Attr(form, "hx-target"); got != "#outline" {
		t.Errorf("empty-outline form hx-target = %q, want #outline", got)
	}
	if got, _ := htmlassert.Attr(form, "hx-swap"); got != "innerHTML" {
		t.Errorf("empty-outline form hx-swap = %q, want innerHTML", got)
	}
}

// TestDeleteButtonIsIndividuallyConfirmed. hx-confirm lives on the delete
// button itself, not on a wrapping form: htmx scopes hx-confirm to the
// element that issues the request, so putting it on the button (rather
// than an ancestor) is what keeps every other button in the same menu from
// asking for confirmation too.
func TestDeleteButtonIsIndividuallyConfirmed(t *testing.T) {
	s := newServer(t)
	id := s.seed(t, s.Alice, notes.RootID, "Projects")

	doc := s.Get(t, s.Alice, "/notes/")
	btn := doc.MustHave(`button[formaction=/notes/` + itoa(id) + `/delete]`)
	if _, ok := htmlassert.Attr(btn, "hx-confirm"); !ok {
		t.Error("the delete button asks for no confirmation")
	}
	if got, _ := htmlassert.Attr(btn, "hx-post"); got != "/notes/"+itoa(id)+"/delete" {
		t.Errorf("delete button hx-post = %q", got)
	}
	if got, _ := htmlassert.Attr(btn, "hx-target"); got != "#outline" {
		t.Errorf("delete button hx-target = %q, want #outline", got)
	}
	if cls, _ := htmlassert.Attr(btn, "class"); !strings.Contains(cls, "outline-menu-delete") {
		t.Errorf("the delete button's class is %q", cls)
	}

	// Neighboring menu buttons must not inherit the confirmation.
	for _, sel := range []string{
		`button[formaction=/notes/` + itoa(id) + `/done]`,
		`button[value=up]`,
	} {
		b := doc.MustHave(sel)
		if _, ok := htmlassert.Attr(b, "hx-confirm"); ok {
			t.Errorf("%s unexpectedly asks for confirmation", sel)
		}
	}
}

// TestOutlineMenuHoldsEveryAction. The "···" menu is the comprehensive,
// touch-reachable home for every row action — including the four that also
// get a hover-overlay shortcut — so nothing is unreachable without a mouse
// hovering the row.
func TestOutlineMenuHoldsEveryAction(t *testing.T) {
	s := newServer(t)
	id := s.seed(t, s.Alice, notes.RootID, "Projects")

	doc := s.Get(t, s.Alice, "/notes/")
	menu := doc.MustHave(".outline-menu")
	summary := doc.MustHave(".outline-menu-toggle")
	if tag := summary.Data; tag != "summary" {
		t.Errorf("menu toggle is a <%s>, want <summary>", tag)
	}
	if !within(summary, menu) {
		t.Error("the toggle is not inside .outline-menu")
	}
	list := doc.MustHave(".outline-menu-list")
	if !within(list, menu) {
		t.Error(".outline-menu-list is not inside .outline-menu")
	}

	for _, sel := range []string{
		`button.outline-done`,
		`input.outline-due-input`,
		`button[value=up]`,
		`button[value=down]`,
		`button[formaction=/notes/` + itoa(id) + `/indent]`,
		`button[formaction=/notes/` + itoa(id) + `/outdent]`,
		`button[formaction=/notes/new]`,
		`button.outline-menu-delete`,
	} {
		found := false
		for _, n := range doc.QueryAll(sel) {
			if within(n, list) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("menu is missing %s", sel)
		}
	}
}

// TestHoverOverlayDuplicatesStructuralActions. The overlay is the
// fast-mouse-access shortcut for the four actions used often enough to
// justify a hover target — it must not include done, due-date editing, or
// delete, which stay menu-only.
func TestHoverOverlayIsRemoved(t *testing.T) {
	s := newServer(t)
	s.seed(t, s.Alice, notes.RootID, "Projects")

	doc := s.Get(t, s.Alice, "/notes/")
	doc.MustNotHave(".outline-overlay")
}

// TestStructuralRequestSavesTheFocusedText is spec §7. Every structural POST
// carries the focused bullet's text so the text update and the structural
// operation are one write. Without it, a user who types and then presses Tab
// loses whatever landed after the last save.
func TestStructuralRequestSavesTheFocusedText(t *testing.T) {
	s := newServer(t)
	ctx := context.Background()
	first := s.seed(t, s.Alice, notes.RootID, "first")
	second := s.seed(t, s.Alice, notes.RootID, "second")

	s.Submit(t, s.Alice, "/notes/"+itoa(second)+"/indent", url.Values{
		"root": {"0"}, "focus_id": {itoa(second)},
		"title": {"typed after the last save"}, "note": {"and a note"},
	}, "/notes/")

	n, err := s.Store.ByID(ctx, s.Alice.User.ID, second)
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
	focused := s.seed(t, s.Alice, notes.RootID, "focused")
	parent := s.seed(t, s.Alice, notes.RootID, "parent")
	s.seed(t, s.Alice, parent, "child")

	s.Submit(t, s.Alice, "/notes/"+itoa(parent)+"/collapse", url.Values{
		"root": {"0"}, "focus_id": {itoa(focused)},
		"title": {"edited elsewhere"}, "note": {""},
		"collapsed": {"1"},
	}, "/notes/")

	f, err := s.Store.ByID(ctx, s.Alice.User.ID, focused)
	if err != nil {
		t.Fatal(err)
	}
	if f.Title != "edited elsewhere" {
		t.Errorf("the focused bullet is %q", f.Title)
	}
	p, err := s.Store.ByID(ctx, s.Alice.User.ID, parent)
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
	mine := s.seed(t, s.Alice, notes.RootID, "mine")
	bobs := s.seed(t, s.Bob, notes.RootID, "bob's")

	// Indenting bob's bullet fails; alice's own text update came first and
	// must not survive.
	rec := s.Post(t, s.Alice, "/notes/"+itoa(bobs)+"/indent", url.Values{
		"root": {"0"}, "focus_id": {itoa(mine)},
		"title": {"should not be saved"}, "note": {""},
	})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}

	n, err := s.Store.ByID(ctx, s.Alice.User.ID, mine)
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
	bobs := s.seed(t, s.Bob, notes.RootID, "bob's")
	s.seed(t, s.Alice, notes.RootID, "a")
	b := s.seed(t, s.Alice, notes.RootID, "b")

	rec := s.Post(t, s.Alice, "/notes/"+itoa(b)+"/move", url.Values{
		"root": {"0"}, "focus_id": {itoa(bobs)},
		"title": {"overwritten"}, "note": {""}, "dir": {"up"},
	})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
	if n, err := s.Store.ByID(ctx, s.Bob.User.ID, bobs); err != nil || n.Title != "bob's" {
		t.Errorf("bob's bullet is now %+v (err %v)", n, err)
	}
	if got := s.titlesAt(t, s.Alice, notes.RootID); !equalStrings(got, []string{"a", "b"}) {
		t.Errorf("the move happened anyway: %v", got)
	}
}

func TestAMalformedFocusIsA400(t *testing.T) {
	s := newServer(t)
	id := s.seed(t, s.Alice, notes.RootID, "a")

	for _, v := range []string{"abc", "-1", "0.5"} {
		rec := s.Post(t, s.Alice, "/notes/"+itoa(id)+"/indent", url.Values{
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
	id := s.seed(t, s.Alice, notes.RootID, "a")

	paths := []string{
		"/notes/new",
		"/notes/" + itoa(id) + "/text",
		"/notes/" + itoa(id) + "/indent",
		"/notes/" + itoa(id) + "/outdent",
		"/notes/" + itoa(id) + "/move",
		"/notes/" + itoa(id) + "/collapse",
		"/notes/" + itoa(id) + "/delete",
		"/notes/" + itoa(id) + "/archive",
	}

	// No CSRF token: the CSRF middleware is outermost of the two and answers.
	for _, path := range paths {
		req := httptest.NewRequest("POST", path, strings.NewReader(""))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		rec := s.Do(t, nil, req)
		if rec.Code != http.StatusForbidden {
			t.Errorf("POST %s anonymous and tokenless = %d, want 403 from the CSRF check",
				path, rec.Code)
		}
	}

	// A valid token, still anonymous: CSRF is satisfied, so the auth guard is
	// what rejects, and it sends the browser to the login page.
	anon := s.Anonymous(t)
	for _, path := range paths {
		rec := s.Post(t, anon, path, url.Values{"root": {"0"}})
		if rec.Code != http.StatusSeeOther {
			t.Errorf("POST %s anonymous with a valid token = %d, want 303", path, rec.Code)
			continue
		}
		if got := rec.Header().Get("Location"); got != "/login" {
			t.Errorf("POST %s anonymous redirected to %q, not the login page", path, got)
		}
	}

	n, err := s.Store.ByID(context.Background(), s.Alice.User.ID, id)
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
	first := s.seed(t, s.Alice, notes.RootID, "first")
	second := s.seed(t, s.Alice, notes.RootID, "second")

	rec := s.PostHX(t, s.Alice, "/notes/"+itoa(second)+"/indent", url.Values{
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

	children, err := s.Store.Children(context.Background(), s.Alice.User.ID, first)
	if err != nil {
		t.Fatal(err)
	}
	if len(children) != 1 || children[0].ID != second {
		t.Fatalf("the indent did not apply: children of first = %+v", children)
	}
}

func TestCreateRespondsWithAFragmentForHTMX(t *testing.T) {
	s := newServer(t)

	rec := s.PostHX(t, s.Alice, "/notes/new", url.Values{
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
	id := s.seed(t, s.Alice, notes.RootID, "old")

	rec := s.PostHX(t, s.Alice, "/notes/"+itoa(id)+"/text", url.Values{
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

	n, err := s.Store.ByID(context.Background(), s.Alice.User.ID, id)
	if err != nil {
		t.Fatal(err)
	}
	if n.Title != "**new**" {
		t.Errorf("saved title = %q, want the raw source", n.Title)
	}
}

func TestAFailedStructuralOperationUnderHTMXIsAFragment(t *testing.T) {
	s := newServer(t)
	bobs := s.seed(t, s.Bob, notes.RootID, "bob's")

	rec := s.PostHX(t, s.Alice, "/notes/"+itoa(bobs)+"/indent", url.Values{"root": {"0"}})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "<html") {
		t.Error("an HTMX error response carries whole-document chrome")
	}
}

func TestDoneTogglesTheBullet(t *testing.T) {
	s := newServer(t)
	id := s.seed(t, s.Alice, notes.RootID, "task")

	s.Submit(t, s.Alice, "/notes/"+itoa(id)+"/done", url.Values{
		"root": {"0"}, "done": {"1"},
	}, "/notes/")

	n, err := s.Store.ByID(context.Background(), s.Alice.User.ID, id)
	if err != nil {
		t.Fatal(err)
	}
	if !n.Done {
		t.Fatal("the bullet is not done")
	}

	s.Submit(t, s.Alice, "/notes/"+itoa(id)+"/done", url.Values{
		"root": {"0"}, "done": {"0"},
	}, "/notes/")

	n, err = s.Store.ByID(context.Background(), s.Alice.User.ID, id)
	if err != nil {
		t.Fatal(err)
	}
	if n.Done {
		t.Fatal("the bullet is still done")
	}
}

func TestDoneRejectsAnUnknownValue(t *testing.T) {
	s := newServer(t)
	id := s.seed(t, s.Alice, notes.RootID, "a")

	for _, v := range []string{"", "true", "yes", "2"} {
		rec := s.Post(t, s.Alice, "/notes/"+itoa(id)+"/done", url.Values{
			"root": {"0"}, "done": {v},
		})
		if rec.Code != http.StatusBadRequest {
			t.Errorf("done=%q gave %d, want 400", v, rec.Code)
		}
	}
}

func TestDoneOnAnotherUsersBulletIs404(t *testing.T) {
	s := newServer(t)
	id := s.seed(t, s.Bob, notes.RootID, "bob's")

	rec := s.Post(t, s.Alice, "/notes/"+itoa(id)+"/done", url.Values{
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
	id := s.seed(t, s.Alice, notes.RootID, "task")
	if err := s.Store.SetDone(context.Background(), s.Alice.User.ID, id, true); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest("GET", "/notes/", nil)
	req.AddCookie(&http.Cookie{Name: notes.ShowCompletedCookie, Value: "1"})
	rec := s.Do(t, s.Alice, req)
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
	id := s.seed(t, s.Alice, notes.RootID, "task")

	// A relative future date, not a hardcoded one — this test is about the
	// chip existing and showing what was set, not about overdue status
	// (that's TestOverdueChipIsMarked/TestAFutureDueChipIsNotMarkedOverdue),
	// so a hardcoded date must never drift into the past and start being
	// treated as overdue (issue #78 added a leading "!" for that state).
	future := time.Now().AddDate(1, 0, 0).Format("2006-01-02")
	s.Submit(t, s.Alice, "/notes/"+itoa(id)+"/due", url.Values{
		"root": {"0"}, "due": {future},
	}, "/notes/")

	doc := s.Get(t, s.Alice, "/notes/")
	chip := doc.MustHave(".outline-due-chip")
	if got := htmlassert.Text(chip); got != future {
		t.Errorf("chip text = %q, want %s", got, future)
	}

	s.Submit(t, s.Alice, "/notes/"+itoa(id)+"/due", url.Values{
		"root": {"0"}, "due": {""},
	}, "/notes/")
	s.Get(t, s.Alice, "/notes/").MustNotHave(".outline-due-chip")
}

func TestDueRejectsBadFormat(t *testing.T) {
	s := newServer(t)
	id := s.seed(t, s.Alice, notes.RootID, "task")

	rec := s.Post(t, s.Alice, "/notes/"+itoa(id)+"/due", url.Values{
		"root": {"0"}, "due": {"not-a-date"},
	})
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

func TestDueOnAnotherUsersBulletIs404(t *testing.T) {
	s := newServer(t)
	id := s.seed(t, s.Bob, notes.RootID, "bob's")

	rec := s.Post(t, s.Alice, "/notes/"+itoa(id)+"/due", url.Values{
		"root": {"0"}, "due": {"2026-03-05"},
	})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

// TestDueDateHasANoJSSubmissionPath is issue #74: the date input must not
// rely solely on hx-trigger=change, and must not implicitly submit through
// outline-main's own default button (#57's hidden append button) when the
// user presses Enter. Both are satisfied by giving the input and its Save
// button a form="due-form-{id}" attribute pointing at a genuinely separate
// <form>, rather than leaving them owned by outline-main.
func TestDueDateHasANoJSSubmissionPath(t *testing.T) {
	s := newServer(t)
	id := s.seed(t, s.Alice, notes.RootID, "task")

	doc := s.Get(t, s.Alice, "/notes/")

	input := doc.MustHave("input.outline-due-input")
	dueForm, ok := htmlassert.Attr(input, "form")
	if !ok || dueForm != "due-form-"+itoa(id) {
		t.Errorf(`due-date input form=%q, ok=%v, want "due-form-%d"`, dueForm, ok, id)
	}

	save := doc.MustHave("button.outline-due-save")
	if got, _ := htmlassert.Attr(save, "form"); got != dueForm {
		t.Errorf("save button form=%q, want %q (same form as the input)", got, dueForm)
	}
	if got, _ := htmlassert.Attr(save, "type"); got != "submit" {
		t.Errorf("save button type=%q, want submit", got)
	}

	owner := doc.MustHave("form#" + dueForm)
	if got, _ := htmlassert.Attr(owner, "action"); got != "/notes/"+itoa(id)+"/due" {
		t.Errorf("due-form action=%q, want /notes/%d/due", got, id)
	}
	if got := len(doc.QueryAll("form#" + dueForm + " input[name=root]")); got != 1 {
		t.Errorf("due-form has %d root fields, want 1", got)
	}
	if got := len(doc.QueryAll("form#" + dueForm + " input[name=csrf_token]")); got != 1 {
		t.Errorf("due-form has %d csrf_token fields, want 1", got)
	}
}

// TestOverdueChipIsMarked doesn't depend on the real clock: it sets a due
// date far enough in the past (year 2000) that it will read as overdue for
// the entire lifetime of this test suite.
func TestOverdueChipIsMarked(t *testing.T) {
	s := newServer(t)
	id := s.seed(t, s.Alice, notes.RootID, "task")
	if err := s.Store.SetDue(context.Background(), s.Alice.User.ID, id, "2000-01-01"); err != nil {
		t.Fatal(err)
	}

	doc := s.Get(t, s.Alice, "/notes/")
	doc.MustHave(".outline-due-overdue")
}

func TestAFutureDueChipIsNotMarkedOverdue(t *testing.T) {
	s := newServer(t)
	id := s.seed(t, s.Alice, notes.RootID, "task")
	future := time.Now().AddDate(1, 0, 0).Format("2006-01-02")
	if err := s.Store.SetDue(context.Background(), s.Alice.User.ID, id, future); err != nil {
		t.Fatal(err)
	}

	s.Get(t, s.Alice, "/notes/").MustNotHave(".outline-due-overdue")
}

// TestOverdueChipIsNotSignalledByColourAlone is issue #78 (WCAG 1.4.1): an
// overdue chip must carry a non-colour signal too, for a colour-blind or
// greyscale-display reader (the leading "!") and a screen reader user (the
// aria-label overriding the announced text) — not just .outline-due-overdue's
// colour.
func TestOverdueChipIsNotSignalledByColourAlone(t *testing.T) {
	s := newServer(t)
	id := s.seed(t, s.Alice, notes.RootID, "task")
	if err := s.Store.SetDue(context.Background(), s.Alice.User.ID, id, "2000-01-01"); err != nil {
		t.Fatal(err)
	}

	chip := s.Get(t, s.Alice, "/notes/").MustHave(".outline-due-overdue")
	if got := htmlassert.Text(chip); !strings.HasPrefix(got, "!") {
		t.Errorf("overdue chip text = %q, want a leading \"!\"", got)
	}
	if got, ok := htmlassert.Attr(chip, "aria-label"); !ok || got != "Overdue: 2000-01-01" {
		t.Errorf("overdue chip aria-label = %q, ok=%v, want %q", got, ok, "Overdue: 2000-01-01")
	}
}

// TestShowCompletedHidesAndReveals is spec §11 end to end: a done bullet's
// whole subtree disappears from the outline until the preference is on.
func TestShowCompletedHidesAndReveals(t *testing.T) {
	s := newServer(t)
	ctx := context.Background()
	parent := s.seed(t, s.Alice, notes.RootID, "parent")
	s.seed(t, s.Alice, parent, "child")
	if err := s.Store.SetDone(ctx, s.Alice.User.ID, parent, true); err != nil {
		t.Fatal(err)
	}

	doc := s.Get(t, s.Alice, "/notes/")
	if strings.Contains(doc.Text(), "child") {
		t.Error("a done bullet's child is visible with show-completed off")
	}
	doc.MustNotHave(`input[name=title]`) // the done parent itself is gone too

	req := httptest.NewRequest("GET", "/notes/", nil)
	req.AddCookie(&http.Cookie{Name: notes.ShowCompletedCookie, Value: "1"})
	rec := s.Do(t, s.Alice, req)
	if !strings.Contains(rec.Body.String(), "child") {
		t.Error("show-completed=1 still hides the done bullet's child")
	}
}

// TestPrefsCookieSecureFollowsAppDeps is issue #79: ShowCompletedCookie must
// mark Secure from app.Deps.Secure, the platform's own configured
// secure-cookie flag — not hardcode it, which would either make the cookie
// silently stop persisting on a real non-TLS deployment (hardcoded true) or
// send it over a plain connection on a TLS one (hardcoded false).
func TestPrefsCookieSecureFollowsAppDeps(t *testing.T) {
	plain := &server{apptest.NewServer(t, notes.New(), notes.NewStore)}
	secure := &server{apptest.NewServer(t, notes.New(), notes.NewStore, apptest.WithSecureCookies())}

	for _, tc := range []struct {
		name string
		s    *server
		want bool
	}{
		{"plain deployment", plain, false},
		{"secure-cookies deployment", secure, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := tc.s.Post(t, tc.s.Alice, "/notes/prefs", url.Values{
				"root": {"0"}, "show_completed": {"1"},
			})
			var got *http.Cookie
			for _, c := range rec.Result().Cookies() {
				if c.Name == notes.ShowCompletedCookie {
					got = c
				}
			}
			if got == nil {
				t.Fatal("no show-completed cookie set")
			}
			if got.Secure != tc.want {
				t.Errorf("Secure = %v, want %v", got.Secure, tc.want)
			}
		})
	}
}

func TestPrefsTogglesTheCookie(t *testing.T) {
	s := newServer(t)

	rec := s.Post(t, s.Alice, "/notes/prefs", url.Values{
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

// TestPrefsRedirectsBackToSearchWithItsQuery is issue #88: /notes/search's
// own show-completed toggle is a plain (non-HTMX) form, so prefs must send
// the browser back to /notes/search with its query preserved — not always
// the outline, which is what every prefs redirect did before this.
func TestPrefsRedirectsBackToSearchWithItsQuery(t *testing.T) {
	s := newServer(t)
	s.Submit(t, s.Alice, "/notes/prefs", url.Values{
		"show_completed": {"1"}, "page": {"search"}, "q": {"foo"},
	}, "/notes/search?q=foo")
}

// TestPrefsRedirectsToOutlineWithNoPage: the default (no page field, the
// outline's own shape) must be unchanged — every existing outline test
// submitting to /notes/prefs relies on this.
func TestPrefsRedirectsToOutlineWithNoPage(t *testing.T) {
	s := newServer(t)
	s.Submit(t, s.Alice, "/notes/prefs", url.Values{
		"root": {"0"}, "show_completed": {"1"},
	}, "/notes/")
}

// TestSearchHasAShowCompletedToggle is issue #88: unlike /notes/due (which
// excludes done nodes unconditionally, no preference to toggle), Search
// actually takes showCompleted, so a completed match was silently
// unfindable here with no way to see or change why. End to end: a done
// bullet that matches is invisible until the toggle is used, and visible
// once it is.
func TestSearchHasAShowCompletedToggle(t *testing.T) {
	s := newServer(t)
	id := s.seed(t, s.Alice, notes.RootID, "finished task")
	s.Submit(t, s.Alice, "/notes/"+itoa(id)+"/done", url.Values{
		"root": {"0"}, "done": {"1"},
	}, "/notes/")

	doc := s.Get(t, s.Alice, "/notes/search?q=finished")
	doc.MustNotHave(".notes-search-item")
	toggle := doc.MustHave("#show-completed-toggle")
	if got, _ := htmlassert.Attr(toggle, "value"); got != "1" {
		t.Errorf("toggle value = %q, want 1 (currently off)", got)
	}

	s.Submit(t, s.Alice, "/notes/prefs", url.Values{
		"show_completed": {"1"}, "page": {"search"}, "q": {"finished"},
	}, "/notes/search?q=finished")

	req := httptest.NewRequest("GET", "/notes/search?q=finished", nil)
	req.AddCookie(&http.Cookie{Name: notes.ShowCompletedCookie, Value: "1"})
	rec := s.Do(t, s.Alice, req)
	htmlassert.Parse(t, rec.Body.String()).MustHave(".notes-search-item")
}

// TestPrefsRespondsWithTheFreshValueOverHTMX guards the staleness trap: the
// fragment this returns must reflect the setting just toggled, not whatever
// the request's own (pre-toggle) cookie said.
func TestPrefsRespondsWithTheFreshValueOverHTMX(t *testing.T) {
	s := newServer(t)
	ctx := context.Background()
	parent := s.seed(t, s.Alice, notes.RootID, "parent")
	s.seed(t, s.Alice, parent, "child")
	if err := s.Store.SetDone(ctx, s.Alice.User.ID, parent, true); err != nil {
		t.Fatal(err)
	}

	rec := s.PostHX(t, s.Alice, "/notes/prefs", url.Values{
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

	rec := s.PostHX(t, s.Alice, "/notes/prefs", url.Values{
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
	rec = s.PostHX(t, s.Alice, "/notes/prefs", url.Values{
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
	btn := s.Get(t, s.Alice, "/notes/").MustHave("#show-completed-toggle")
	if _, ok := htmlassert.Attr(btn, "hx-swap-oob"); ok {
		t.Error("the page render's toggle carries hx-swap-oob")
	}
}

// TestMutationFragmentCarriesTheToggleUnchanged: a structural op does not
// touch the preference, so its out-of-band toggle must repeat the state the
// request came in with rather than flip it.
func TestMutationFragmentCarriesTheToggleUnchanged(t *testing.T) {
	s := newServer(t)
	id := s.seed(t, s.Alice, notes.RootID, "a")

	req := httptest.NewRequest("POST", "/notes/"+itoa(id)+"/indent",
		strings.NewReader(url.Values{"root": {"0"}, web.CSRFFormField: {s.CSRFToken(t, s.Alice)}}.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("HX-Request", "true")
	req.AddCookie(&http.Cookie{Name: notes.ShowCompletedCookie, Value: "1"})
	rec := s.Do(t, s.Alice, req)

	btn := htmlassert.Parse(t, rec.Body.String()).MustHave("#show-completed-toggle")
	if got, _ := htmlassert.Attr(btn, "value"); got != "0" {
		t.Errorf("toggle value = %q, want \"0\" — show-completed was on for this request", got)
	}
}

func TestPrefsRejectsAnUnknownValue(t *testing.T) {
	s := newServer(t)
	for _, v := range []string{"", "true", "2"} {
		rec := s.Post(t, s.Alice, "/notes/prefs", url.Values{
			"root": {"0"}, "show_completed": {v},
		})
		if rec.Code != http.StatusBadRequest {
			t.Errorf("show_completed=%q gave %d, want 400", v, rec.Code)
		}
	}
}

func TestEveryMutationRequiresSignInIncludesDoneAndDue(t *testing.T) {
	s := newServer(t)
	id := s.seed(t, s.Alice, notes.RootID, "a")

	for _, path := range []string{
		"/notes/" + itoa(id) + "/done",
		"/notes/" + itoa(id) + "/due",
	} {
		req := httptest.NewRequest("POST", path, strings.NewReader(""))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		rec := s.Do(t, nil, req)
		if rec.Code != http.StatusForbidden {
			t.Errorf("POST %s anonymous and tokenless = %d, want 403 from the CSRF check", path, rec.Code)
		}
	}
}

func TestScriptIsServedWithAJavaScriptContentType(t *testing.T) {
	s := newServer(t)
	rec := s.Do(t, s.Alice, httptest.NewRequest("GET", "/notes/notes.js", nil))
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
	parent := s.seed(t, s.Alice, notes.RootID, "Projects")
	child := s.seed(t, s.Alice, parent, "AtBudget")
	if err := s.Store.SetDue(ctx, s.Alice.User.ID, child, "2000-01-01"); err != nil {
		t.Fatal(err)
	}

	doc := s.Get(t, s.Alice, "/notes/due")
	doc.MustHave(".outline-due-overdue")
	if !strings.Contains(doc.Text(), "Projects") {
		t.Error("the due bullet's ancestor breadcrumb is missing")
	}
	link := doc.MustHave(`a[href=/notes/` + itoa(child) + `]`)
	if got := htmlassert.Text(link); got != "AtBudget" {
		t.Errorf("due list link text = %q, want AtBudget", got)
	}

	// Issue #78: /notes/due's own chip carries the same non-colour signal
	// as the outline's, since a screen reader could still land on the chip
	// on its own even with the "Overdue" section heading above it.
	chip := doc.MustHave(".outline-due-overdue")
	if got := htmlassert.Text(chip); !strings.HasPrefix(got, "!") {
		t.Errorf("due-list overdue chip text = %q, want a leading \"!\"", got)
	}
	if got, ok := htmlassert.Attr(chip, "aria-label"); !ok || got != "Overdue: 2000-01-01" {
		t.Errorf("due-list overdue chip aria-label = %q, ok=%v, want %q", got, ok, "Overdue: 2000-01-01")
	}
}

// TestDueListCrumbsRenderMarkdownButRowTitleStaysPlain is issue #76: the
// crumb spans aren't links, so they render a bullet's Markdown; the row's
// own title IS a link (to its zoom), so — same tension as #65's breadcrumb
// ancestors — it deliberately stays on plain DisplayTitle rather than risk
// nesting an <a> inside it.
func TestDueListCrumbsRenderMarkdownButRowTitleStaysPlain(t *testing.T) {
	s := newServer(t)
	ctx := context.Background()
	parent := s.seed(t, s.Alice, notes.RootID, "**Projects**")
	child := s.seed(t, s.Alice, parent, "**Milk**")
	if err := s.Store.SetDue(ctx, s.Alice.User.ID, child, "2000-01-01"); err != nil {
		t.Fatal(err)
	}

	doc := s.Get(t, s.Alice, "/notes/due")
	crumb := doc.MustHave(".notes-crumb-item")
	if got := htmlassert.Text(crumb); got != "Projects" {
		t.Errorf("due-list crumb text = %q, want the rendered form", got)
	}

	link := doc.MustHave(`a[href=/notes/` + itoa(child) + `]`)
	if got := htmlassert.Text(link); got != "**Milk**" {
		t.Errorf("due-list row title = %q, want the literal source (it's a link)", got)
	}
}

func TestDueListRendersAnotherUsersNodesNowhere(t *testing.T) {
	s := newServer(t)
	ctx := context.Background()
	bobs := s.seed(t, s.Bob, notes.RootID, "bob's task")
	if err := s.Store.SetDue(ctx, s.Bob.User.ID, bobs, "2000-01-01"); err != nil {
		t.Fatal(err)
	}

	if strings.Contains(s.Get(t, s.Alice, "/notes/due").Text(), "bob's task") {
		t.Error("another user's due bullet is on the page")
	}
}

func TestDueListRequiresSignIn(t *testing.T) {
	s := newServer(t)
	rec := s.Do(t, nil, httptest.NewRequest("GET", "/notes/due", nil))
	if rec.Code != http.StatusSeeOther {
		t.Errorf("GET /notes/due anonymous = %d, want a 303 to the login page", rec.Code)
	}
}

func TestDueListWithNothingDue(t *testing.T) {
	s := newServer(t)
	s.seed(t, s.Alice, notes.RootID, "no date on this one")

	if got := s.Get(t, s.Alice, "/notes/due").Text(); !strings.Contains(got, "Nothing is due") {
		t.Errorf("empty due list text = %q, want the empty-state line", got)
	}
}

func TestArchiveHidesTheBulletAndRedirectsToTheOutline(t *testing.T) {
	s := newServer(t)
	id := s.seed(t, s.Alice, notes.RootID, "put away")

	s.Submit(t, s.Alice, "/notes/"+itoa(id)+"/archive", url.Values{
		"root": {"0"}, "archived": {"1"},
	}, "/notes/")

	n, err := s.Store.ByID(context.Background(), s.Alice.User.ID, id)
	if err != nil {
		t.Fatal(err)
	}
	if !n.Archived {
		t.Fatal("the bullet is not archived")
	}

	// htmlassert only matches one qualifier per selector part (see its own
	// doc comment), so ".outline-row[data-id=...]" is not a valid selector
	// here — this walks the (unqualified-by-id) rows instead and checks
	// each one's own data-id attribute.
	doc := s.Get(t, s.Alice, "/notes/")
	for _, row := range doc.QueryAll(".outline-row") {
		if got, _ := htmlassert.Attr(row, "data-id"); got == itoa(id) {
			t.Fatal("the archived bullet still appears in the outline")
		}
	}
}

func TestArchiveRejectsAnUnknownValue(t *testing.T) {
	s := newServer(t)
	id := s.seed(t, s.Alice, notes.RootID, "a")

	for _, v := range []string{"", "true", "yes", "2"} {
		rec := s.Post(t, s.Alice, "/notes/"+itoa(id)+"/archive", url.Values{
			"root": {"0"}, "archived": {v},
		})
		if rec.Code != http.StatusBadRequest {
			t.Errorf("archived=%q gave %d, want 400", v, rec.Code)
		}
	}
}

func TestArchiveOnAnotherUsersBulletIs404(t *testing.T) {
	s := newServer(t)
	id := s.seed(t, s.Bob, notes.RootID, "bob's")

	rec := s.Post(t, s.Alice, "/notes/"+itoa(id)+"/archive", url.Values{
		"root": {"0"}, "archived": {"1"},
	})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

// TestRestoreBringsTheBulletBackAndRedirectsToTheArchivePage: the field's
// own value (archived=0) is what routes this request to restore rather
// than mutate — see archive's design note in this plan.
func TestRestoreBringsTheBulletBackAndRedirectsToTheArchivePage(t *testing.T) {
	s := newServer(t)
	id := s.seed(t, s.Alice, notes.RootID, "put away")
	if err := s.Store.SetArchived(context.Background(), s.Alice.User.ID, id, true); err != nil {
		t.Fatal(err)
	}

	rec := s.Post(t, s.Alice, "/notes/"+itoa(id)+"/archive", url.Values{"archived": {"0"}})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303; body: %s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Location"); got != "/notes/archive" {
		t.Fatalf("redirected to %q, want /notes/archive", got)
	}

	n, err := s.Store.ByID(context.Background(), s.Alice.User.ID, id)
	if err != nil {
		t.Fatal(err)
	}
	if n.Archived {
		t.Fatal("the bullet is still archived")
	}
}

func TestRestoreOnAnotherUsersBulletIs404(t *testing.T) {
	s := newServer(t)
	id := s.seed(t, s.Bob, notes.RootID, "bob's")
	if err := s.Store.SetArchived(context.Background(), s.Bob.User.ID, id, true); err != nil {
		t.Fatal(err)
	}

	rec := s.Post(t, s.Alice, "/notes/"+itoa(id)+"/archive", url.Values{"archived": {"0"}})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestRestoreRespondsWithTheArchiveListFragmentForHTMX(t *testing.T) {
	s := newServer(t)
	id := s.seed(t, s.Alice, notes.RootID, "put away")
	if err := s.Store.SetArchived(context.Background(), s.Alice.User.ID, id, true); err != nil {
		t.Fatal(err)
	}

	rec := s.PostHX(t, s.Alice, "/notes/"+itoa(id)+"/archive", url.Values{"archived": {"0"}})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "<html") {
		t.Error("an HTMX response carries whole-document chrome")
	}
	if strings.Contains(rec.Body.String(), "put away") {
		t.Error("the restored bullet still appears in the archive list fragment")
	}
}

func TestArchiveListShowsArchivedNodesWithCrumbs(t *testing.T) {
	s := newServer(t)
	ctx := context.Background()
	parent := s.seed(t, s.Alice, notes.RootID, "Projects")
	child := s.seed(t, s.Alice, parent, "old project")
	if err := s.Store.SetArchived(ctx, s.Alice.User.ID, child, true); err != nil {
		t.Fatal(err)
	}

	doc := s.Get(t, s.Alice, "/notes/archive")
	// htmlassert only matches one qualifier per selector part, so the class
	// and the href check are two steps rather than one compound selector.
	title := doc.MustHave("a.notes-archive-title")
	if got, _ := htmlassert.Attr(title, "href"); got != "/notes/"+itoa(child) {
		t.Errorf("archive title href = %q, want /notes/%s", got, itoa(child))
	}
	crumbs := doc.MustHave(".notes-archive-crumbs")
	if !strings.Contains(htmlassert.Text(crumbs), "Projects") {
		t.Errorf("crumbs = %q, want it to mention Projects", htmlassert.Text(crumbs))
	}
}

// TestArchiveCrumbsRenderMarkdownButRowTitleStaysPlain is issue #120: the
// crumb spans aren't links, so they render a bullet's Markdown; the row's
// own title IS a link (to its zoom), so it deliberately stays on plain
// DisplayTitle rather than risk nesting an <a> inside it.
func TestArchiveCrumbsRenderMarkdownButRowTitleStaysPlain(t *testing.T) {
	s := newServer(t)
	ctx := context.Background()
	parent := s.seed(t, s.Alice, notes.RootID, "**Projects**")
	child := s.seed(t, s.Alice, parent, "**Milk**")
	if err := s.Store.SetArchived(ctx, s.Alice.User.ID, child, true); err != nil {
		t.Fatal(err)
	}

	doc := s.Get(t, s.Alice, "/notes/archive")
	crumb := doc.MustHave(".notes-crumb-item")
	if got := htmlassert.Text(crumb); got != "Projects" {
		t.Errorf("archive crumb text = %q, want the rendered form", got)
	}

	title := doc.MustHave("a.notes-archive-title")
	if got := htmlassert.Text(title); got != "**Milk**" {
		t.Errorf("archive row title = %q, want the literal source (it's a link)", got)
	}
}

func TestArchiveListWithNothingArchived(t *testing.T) {
	s := newServer(t)
	s.seed(t, s.Alice, notes.RootID, "never archived")

	doc := s.Get(t, s.Alice, "/notes/archive")
	doc.MustHave("body") // page renders at all
	if strings.Contains(htmlassert.Text(doc.MustHave(".notes")), "never archived") {
		t.Error("an unarchived bullet appears on the archive page")
	}
}

func TestArchiveListRequiresSignIn(t *testing.T) {
	s := newServer(t)
	rec := s.Do(t, nil, httptest.NewRequest("GET", "/notes/archive", nil))
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("GET /notes/archive anonymous = %d, want a 303 to the login page", rec.Code)
	}
}

// TestZoomingIntoAnArchivedNodeShowsABannerAndItsChildren covers the finding
// that Store.Outline's archived_at exclusion applies to the rows it returns,
// not to rootID itself: navigating straight to an archived node's own URL —
// exactly what archive.html's own title link does — renders a completely
// normal, editable outline of its still-visible children, with no built-in
// indication that the root is archived. The chosen fix is a banner, not
// re-filtering the root, so this asserts the banner appears and the child is
// still there, not that the child gets hidden too.
func TestZoomingIntoAnArchivedNodeShowsABannerAndItsChildren(t *testing.T) {
	s := newServer(t)
	ctx := context.Background()
	parent := s.seed(t, s.Alice, notes.RootID, "put away")
	child := s.seed(t, s.Alice, parent, "still visible")
	if err := s.Store.SetArchived(ctx, s.Alice.User.ID, parent, true); err != nil {
		t.Fatal(err)
	}

	doc := s.Get(t, s.Alice, "/notes/"+itoa(parent))
	banner := doc.MustHave(".notes-archived-banner")
	if !strings.Contains(htmlassert.Text(banner), "archived") {
		t.Errorf("banner text = %q, want it to mention the node is archived", htmlassert.Text(banner))
	}
	found := false
	for _, row := range doc.QueryAll(".outline-row") {
		if got, _ := htmlassert.Attr(row, "data-id"); got == itoa(child) {
			found = true
		}
	}
	if !found {
		t.Error("the non-archived child does not appear when zoomed into its archived parent")
	}
}

// TestZoomingIntoANonArchivedNodeShowsNoBanner: the banner must not appear
// just because the outline is zoomed — only when the root itself is
// archived.
func TestZoomingIntoANonArchivedNodeShowsNoBanner(t *testing.T) {
	s := newServer(t)
	parent := s.seed(t, s.Alice, notes.RootID, "ordinary")
	s.seed(t, s.Alice, parent, "child")

	s.Get(t, s.Alice, "/notes/"+itoa(parent)).MustNotHave(".notes-archived-banner")
}

// TestTopLevelOutlineShowsNoArchivedBanner: RootID is never archived, so the
// top-level view must never carry the banner either.
func TestTopLevelOutlineShowsNoArchivedBanner(t *testing.T) {
	s := newServer(t)
	s.seed(t, s.Alice, notes.RootID, "top level")

	s.Get(t, s.Alice, "/notes/").MustNotHave(".notes-archived-banner")
}

func TestTheOutlineLinksToTheDueList(t *testing.T) {
	s := newServer(t)
	s.Get(t, s.Alice, "/notes/").MustHave(`.notes-toolbar a[href=/notes/due]`)
}

func TestSearchFindsABulletAndShowsItsBreadcrumb(t *testing.T) {
	s := newServer(t)
	parent := s.seed(t, s.Alice, notes.RootID, "Projects")
	// A word FTS5's default unicode61 tokenizer treats as its own token, not
	// "AtBudget report" (the plan's literal example): that camelCase run
	// tokenizes as one "atbudget" token, which "budget" alone never matches.
	// See task-2-report.md for the full account of this deviation.
	child := s.seed(t, s.Alice, parent, "Budget report")

	doc := s.Get(t, s.Alice, "/notes/search?q=budget")
	if !strings.Contains(doc.Text(), "Projects") {
		t.Error("the hit's ancestor breadcrumb is missing")
	}
	link := doc.MustHave(`a[href=/notes/` + itoa(child) + `]`)
	if got := htmlassert.Text(link); got != "Budget report" {
		t.Errorf("search hit link text = %q", got)
	}
}

// TestSearchCrumbsRenderMarkdownButRowTitleStaysPlain is issue #120: same
// split as TestArchiveCrumbsRenderMarkdownButRowTitleStaysPlain — the crumb
// spans aren't links and render Markdown; the row's own title IS a link, so
// it stays on plain DisplayTitle.
func TestSearchCrumbsRenderMarkdownButRowTitleStaysPlain(t *testing.T) {
	s := newServer(t)
	parent := s.seed(t, s.Alice, notes.RootID, "**Projects**")
	child := s.seed(t, s.Alice, parent, "**Milk**")

	doc := s.Get(t, s.Alice, "/notes/search?q=milk")
	crumb := doc.MustHave(".notes-crumb-item")
	if got := htmlassert.Text(crumb); got != "Projects" {
		t.Errorf("search crumb text = %q, want the rendered form", got)
	}

	link := doc.MustHave(`a[href=/notes/` + itoa(child) + `]`)
	if got := htmlassert.Text(link); got != "**Milk**" {
		t.Errorf("search hit link text = %q, want the literal source (it's a link)", got)
	}
}

// TestSearchBoxPrefillsTheQueryAndAutofocuses is issue #90: search.html's
// own copy of the search box carried value="{{.Data.Query}}" and autofocus,
// attributes the outline's and due's copies don't have — neither was
// pinned by a handler test before this, the least-protected of the (now
// shared, per #89) copies against drift.
func TestSearchBoxPrefillsTheQueryAndAutofocuses(t *testing.T) {
	s := newServer(t)
	in := s.Get(t, s.Alice, "/notes/search?q=foo").MustHave("#notes-search-input")
	if got, _ := htmlassert.Attr(in, "value"); got != "foo" {
		t.Errorf("search box value = %q, want foo", got)
	}
	if _, ok := htmlassert.Attr(in, "autofocus"); !ok {
		t.Error("search.html's search box does not autofocus")
	}
}

func TestSearchWithNoQueryShowsNoResults(t *testing.T) {
	s := newServer(t)
	s.seed(t, s.Alice, notes.RootID, "anything")

	doc := s.Get(t, s.Alice, "/notes/search")
	doc.MustNotHave(".notes-search-item")
}

// TestSearchWithWhitespaceOnlyQueryShowsNoResults is issue #92: a
// whitespace-only q must render exactly like a genuinely empty one — just
// the bare search box — not "No matches for '  '", which is what happened
// when the untrimmed query reached the template while the handler's own
// guard (strings.TrimSpace(query) != "") correctly skipped running a search.
func TestSearchWithWhitespaceOnlyQueryShowsNoResults(t *testing.T) {
	s := newServer(t)
	s.seed(t, s.Alice, notes.RootID, "anything")

	doc := s.Get(t, s.Alice, "/notes/search?q=%20%20")
	doc.MustNotHave(".notes-search-item")
	if strings.Contains(doc.Text(), "No matches") {
		t.Error("a whitespace-only query rendered the no-matches message")
	}
}

func TestSearchWithNoMatchesSaysSo(t *testing.T) {
	s := newServer(t)
	doc := s.Get(t, s.Alice, "/notes/search?q=nonexistent")
	if !strings.Contains(doc.Text(), "No matches") {
		t.Error("an empty result set shows no feedback")
	}
}

func TestSearchDoesNotRenderAnotherUsersNodes(t *testing.T) {
	s := newServer(t)
	s.seed(t, s.Bob, notes.RootID, "bob's secret plan")

	doc := s.Get(t, s.Alice, "/notes/search?q=secret")
	doc.MustNotHave(".notes-search-item")
}

func TestSearchRequiresSignIn(t *testing.T) {
	s := newServer(t)
	rec := s.Do(t, nil, httptest.NewRequest("GET", "/notes/search", nil))
	if rec.Code != http.StatusSeeOther {
		t.Errorf("GET /notes/search anonymous = %d, want a 303 to the login page", rec.Code)
	}
}

// TestTagChipNowResolves closes N4's own follow-up: the #tag chip has
// always linked to /notes/search?q=..., 404ing until this chunk existed to
// answer it.
func TestTagChipNowResolves(t *testing.T) {
	s := newServer(t)
	s.seed(t, s.Alice, notes.RootID, "check #urgent today")

	doc := s.Get(t, s.Alice, "/notes/")
	chip := doc.MustHave(".outline-tag")
	href, _ := htmlassert.Attr(chip, "href")

	rec := s.Do(t, s.Alice, httptest.NewRequest("GET", href, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET %s = %d, want 200", href, rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "check") {
		t.Error("following the tag chip does not find the bullet that produced it")
	}
}

// TestOutlineToolbarHasASearchBox: search has to be reachable from the page
// the user is actually on, not just by typing the URL. The box is a plain GET
// form, so it needs no CSRF token and works with JavaScript off.
func TestOutlineToolbarHasASearchBox(t *testing.T) {
	s := newServer(t)
	doc := s.Get(t, s.Alice, "/notes/")
	in := doc.MustHave("#notes-search-input")
	if got, _ := htmlassert.Attr(in, "name"); got != "q" {
		t.Errorf("search input name = %q, want q", got)
	}
	form := doc.MustHave("form.notes-search")
	if got, _ := htmlassert.Attr(form, "action"); got != "/notes/search" {
		t.Errorf("search form action = %q", got)
	}
	if got, _ := htmlassert.Attr(form, "method"); !strings.EqualFold(got, "get") {
		t.Errorf("search form method = %q, want get (no CSRF token needed)", got)
	}
	if got, _ := htmlassert.Attr(form, "role"); got != "search" {
		t.Errorf("search form role = %q, want search", got)
	}
}

func TestDueToolbarHasASearchBox(t *testing.T) {
	s := newServer(t)
	s.Get(t, s.Alice, "/notes/due").MustHave("#notes-search-input")
}

// TestOutlineMenuHasAnArchiveAction extends the existing comprehensive-menu
// check (TestOutlineMenuHoldsEveryAction) with this task's new action.
func TestOutlineMenuHasAnArchiveAction(t *testing.T) {
	s := newServer(t)
	id := s.seed(t, s.Alice, notes.RootID, "Projects")

	doc := s.Get(t, s.Alice, "/notes/")
	menu := doc.MustHave(".outline-menu")
	list := doc.MustHave(".outline-menu-list")

	found := false
	for _, n := range doc.QueryAll(`button[formaction=/notes/` + itoa(id) + `/archive]`) {
		if within(n, list) && within(n, menu) {
			found = true
		}
	}
	if !found {
		t.Error("the menu is missing an Archive action")
	}
}

func TestArchiveToolbarHasASearchBox(t *testing.T) {
	s := newServer(t)
	s.Get(t, s.Alice, "/notes/archive").MustHave("#notes-search-input")
}

func TestExportDownloadsTheWholeTree(t *testing.T) {
	s := newServer(t)
	s.seed(t, s.Alice, notes.RootID, "top level bullet")

	rec := s.Do(t, s.Alice, httptest.NewRequest("GET", "/notes/export", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Content-Type"); !strings.HasPrefix(got, "text/markdown") {
		t.Errorf("Content-Type = %q, want text/markdown", got)
	}
	if got := rec.Header().Get("Content-Disposition"); !strings.Contains(got, "attachment") {
		t.Errorf("Content-Disposition = %q, want an attachment", got)
	}
	if !strings.Contains(rec.Body.String(), "- top level bullet\n") {
		t.Errorf("export body = %q, missing the bullet", rec.Body.String())
	}
}

func TestExportOfASubtree(t *testing.T) {
	s := newServer(t)
	root := s.seed(t, s.Alice, notes.RootID, "root")
	s.seed(t, s.Alice, root, "child")
	s.seed(t, s.Alice, notes.RootID, "unrelated")

	rec := s.Do(t, s.Alice, httptest.NewRequest("GET", "/notes/export?root="+itoa(root), nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, "- child\n") {
		t.Errorf("export body = %q, missing the child", body)
	}
	if strings.Contains(body, "root") || strings.Contains(body, "unrelated") {
		t.Errorf("export body = %q, a subtree export must not include the root or unrelated bullets", body)
	}
}

func TestExportOnAnotherUsersRootIs404(t *testing.T) {
	s := newServer(t)
	id := s.seed(t, s.Bob, notes.RootID, "bob's")

	rec := s.Do(t, s.Alice, httptest.NewRequest("GET", "/notes/export?root="+itoa(id), nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestExportRequiresSignIn(t *testing.T) {
	s := newServer(t)
	rec := s.Do(t, nil, httptest.NewRequest("GET", "/notes/export", nil))
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("GET /notes/export anonymous = %d, want a 303 to the login page", rec.Code)
	}
}

// TestOutlineToolbarHasAnExportLink looks the link up by its text rather
// than by a compound selector: htmlassert matches only one qualifier per
// selector part (see its own doc comment), so a combined
// tag+class+attribute-prefix selector is not expressible here.
func TestOutlineToolbarHasAnExportLink(t *testing.T) {
	s := newServer(t)
	doc := s.Get(t, s.Alice, "/notes/")

	var link *html.Node
	for _, a := range doc.QueryAll("a.toolbar-btn-nav") {
		if htmlassert.Text(a) == "Export" {
			link = a
		}
	}
	if link == nil {
		t.Fatal("no toolbar-btn-nav link reads \"Export\"")
	}
	if got, _ := htmlassert.Attr(link, "href"); got != "/notes/export?root=0" {
		t.Errorf("export link href = %q, want /notes/export?root=0", got)
	}
}

func TestOutlineToolbarHasAnArchiveLink(t *testing.T) {
	s := newServer(t)
	doc := s.Get(t, s.Alice, "/notes/")
	link := doc.MustHave(`a[href="/notes/archive"]`)
	if htmlassert.Text(link) != "Archive" {
		t.Errorf("archive link text = %q, want Archive", htmlassert.Text(link))
	}
}

// multipartMarkdownRequest builds a POST /notes/import request carrying
// content as a file named "file", plus root and a valid CSRF token — the
// shape s.Post's url.Values-based helper cannot produce, since this route
// needs multipart/form-data rather than urlencoded.
func (s *server) multipartMarkdownRequest(t *testing.T, sess *session, root, content string) *http.Request {
	t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	if err := w.WriteField("root", root); err != nil {
		t.Fatal(err)
	}
	if err := w.WriteField("csrf_token", s.CSRFToken(t, sess)); err != nil {
		t.Fatal(err)
	}
	part, err := w.CreateFormFile("file", "notes.md")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write([]byte(content)); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest("POST", "/notes/import", &buf)
	req.Header.Set("Content-Type", w.FormDataContentType())
	return req
}

func TestImportCreatesNodesUnderTheZoomRoot(t *testing.T) {
	s := newServer(t)
	req := s.multipartMarkdownRequest(t, s.Alice, "0", "- imported\n")

	rec := s.Do(t, s.Alice, req)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303; body: %s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Location"); got != "/notes/" {
		t.Fatalf("redirected to %q, want /notes/", got)
	}

	got := s.titlesAt(t, s.Alice, notes.RootID)
	if len(got) != 1 || got[0] != "imported" {
		t.Fatalf("top level = %v, want just imported", got)
	}
}

func TestImportUnderAZoomedRoot(t *testing.T) {
	s := newServer(t)
	root := s.seed(t, s.Alice, notes.RootID, "zoomed root")
	req := s.multipartMarkdownRequest(t, s.Alice, itoa(root), "- child\n")

	rec := s.Do(t, s.Alice, req)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303; body: %s", rec.Code, rec.Body.String())
	}

	got := s.titlesAt(t, s.Alice, root)
	if len(got) != 1 || got[0] != "child" {
		t.Fatalf("children of root = %v, want just child", got)
	}
}

func TestImportRespondsWithAFragmentForHTMX(t *testing.T) {
	s := newServer(t)
	req := s.multipartMarkdownRequest(t, s.Alice, "0", "- imported\n")
	req.Header.Set("HX-Request", "true")

	rec := s.Do(t, s.Alice, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "imported") {
		t.Error("the imported bullet is not in the fragment")
	}
}

func TestImportRejectsMalformedMarkdown(t *testing.T) {
	s := newServer(t)
	req := s.multipartMarkdownRequest(t, s.Alice, "0", "stray text with no bullet\n")

	rec := s.Do(t, s.Alice, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body: %s", rec.Code, rec.Body.String())
	}
}

// TestImportRejectsAnOversizedFile checks the body against
// MaxImportFileBytes specifically, not against the platform's own larger
// 1 MiB ceiling — a file just over MaxImportFileBytes must still fail
// even though it is comfortably under the platform's own cap.
func TestImportRejectsAnOversizedFile(t *testing.T) {
	s := newServer(t)
	oversized := strings.Repeat("x", notes.MaxImportFileBytes+1)
	req := s.multipartMarkdownRequest(t, s.Alice, "0", "- "+oversized+"\n")

	rec := s.Do(t, s.Alice, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body: %s", rec.Code, rec.Body.String())
	}
}

// TestImportRequiresSignIn: like TestEveryMutationRequiresSignIn, a
// tokenless anonymous POST never reaches the auth guard at all — CSRF's
// middleware is outermost and answers first with a 403, regardless of
// sign-in state. To exercise sign-in specifically, this uses a valid CSRF
// token (from an anonymous GET) so CSRF passes and RequireUser is the one
// that answers, with its usual 303 to the login page.
func TestImportRequiresSignIn(t *testing.T) {
	s := newServer(t)
	anon := s.Anonymous(t)
	req := s.multipartMarkdownRequest(t, anon, "0", "- x\n")

	rec := s.Do(t, anon, req)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("POST /notes/import anonymous = %d, want a 303 to the login page", rec.Code)
	}
	if got := rec.Header().Get("Location"); got != "/login" {
		t.Errorf("POST /notes/import anonymous redirected to %q, not the login page", got)
	}
}

func TestOutlineToolbarHasAnImportForm(t *testing.T) {
	s := newServer(t)
	doc := s.Get(t, s.Alice, "/notes/")
	form := doc.MustHave("form.notes-import")
	if got, _ := htmlassert.Attr(form, "action"); got != "/notes/import" {
		t.Errorf("import form action = %q, want /notes/import", got)
	}
	if got, _ := htmlassert.Attr(form, "enctype"); got != "multipart/form-data" {
		t.Errorf("import form enctype = %q, want multipart/form-data", got)
	}
	doc.MustHave("form.notes-import input[type=file]")
}
