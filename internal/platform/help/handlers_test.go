package help_test

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/iliafrenkel/on-suite/internal/htmlassert"
)

func TestHelpIsPublic(t *testing.T) {
	s := newServer(t)
	for _, path := range []string{"/help", "/help/notes", "/help/admin"} {
		rec := s.get(t, nil, path)
		if rec.Code != http.StatusOK {
			t.Errorf("GET %s signed out = %d, want 200", path, rec.Code)
		}
	}
}

func TestHelpPageRendersInTheShellWithASidebar(t *testing.T) {
	s := newServer(t)
	d := doc(t, s.get(t, s.user, "/help/notes"))
	d.MustHave(".shell-bar")
	d.MustHave(".shell-user") // signed in, the visitor keeps their user menu
	d.MustHave(".help-nav")
	cur := d.MustHave(`.help-nav a[aria-current="page"]`)
	if href, _ := htmlassert.Attr(cur, "href"); href != "/help/notes" {
		t.Errorf("current sidebar link = %q, want /help/notes", href)
	}
	if got := len(d.QueryAll(".help-nav a")); got != 11 {
		t.Errorf("sidebar has %d links, want 11", got)
	}
	d.MustHave(".help-body h1")
}

// Signed out there is no app sidebar or user menu; the help sidebar is
// still there on its own.
func TestSignedOutHelpStillHasItsSidebar(t *testing.T) {
	s := newServer(t)
	d := doc(t, s.get(t, nil, "/help"))
	d.MustNotHave(".shell-user")
	d.MustHave(".help-nav")
	cur := d.MustHave(`.help-nav a[aria-current="page"]`)
	if href, _ := htmlassert.Attr(cur, "href"); href != "/help" {
		t.Errorf("current sidebar link = %q, want /help", href)
	}
}

func TestHelpPageTitles(t *testing.T) {
	s := newServer(t)
	for path, want := range map[string]string{
		"/help":           "Help · ON Suite",
		"/help/index":     "Help · ON Suite",
		"/help/notes":     "ON Notes help · ON Suite",
		"/help/admin":     "Administration help · ON Suite",
		"/help/reader":    "ON Reader help · ON Suite",
		"/help/changelog": "What's new · ON Suite",
	} {
		d := doc(t, s.get(t, nil, path))
		if got := strings.TrimSpace(htmlassert.Text(d.MustHave("title"))); got != want {
			t.Errorf("GET %s title = %q, want %q", path, got, want)
		}
	}
}

func TestUnknownHelpPageIsTheNormal404(t *testing.T) {
	s := newServer(t)
	// One browser for every request: without a CSRF cookie each response
	// mints a fresh token, and the bodies would differ by that alone.
	anon := s.anonymous(t)
	missing := s.get(t, anon, "/no-such-page")
	for _, path := range []string{"/help/no-such-page", "/help/images", "/help/"} {
		got := s.get(t, anon, path)
		if got.Code != http.StatusNotFound || got.Body.String() != missing.Body.String() {
			t.Errorf("GET %s = %d, want the standard 404", path, got.Code)
		}
	}
}

func TestHelpImagesServeWithAnImageType(t *testing.T) {
	s := newServer(t)
	rec := s.get(t, nil, "/help/images/dashboard.png")
	if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "image/png" {
		t.Errorf("image = %d %q", rec.Code, rec.Header().Get("Content-Type"))
	}
	if cc := rec.Header().Get("Cache-Control"); cc != "public, max-age=86400" {
		t.Errorf("image Cache-Control = %q", cc)
	}
	for _, bad := range []string{"/help/images/nope.png", "/help/images/..%2Findex.md", "/help/images/a%2Fb.png", "/help/images/%2E%2E"} {
		if rec := s.get(t, nil, bad); rec.Code != http.StatusNotFound {
			t.Errorf("GET %s = %d, want 404", bad, rec.Code)
		}
	}
	// ServeMux cleans a literal "..", redirecting rather than serving; all
	// that matters is that nothing outside images/ comes back.
	for _, path := range []string{"/help/images/../index.md", "/help/images/.."} {
		rec := s.get(t, nil, path)
		if rec.Code == http.StatusOK {
			t.Errorf("GET %s = 200, want anything else", path)
		}
		if strings.Contains(rec.Body.String(), "# Welcome") {
			t.Errorf("GET %s served the Markdown source", path)
		}
	}
}

func TestWhatsNewBadgeShowsForAnUnseenVersion(t *testing.T) {
	s := newServer(t)
	d := doc(t, s.get(t, s.user, "/help/notes"))
	d.MustHave(".shell-user-toggle .whats-new-dot")
	d.MustHave(`.shell-user-menu-panel a[href="/help/changelog"] .whats-new-dot`)
}

func TestFooterVersionLinksToTheChangelog(t *testing.T) {
	s := newServer(t)
	for _, sess := range []*session{nil, s.user} {
		d := doc(t, s.get(t, sess, "/help/notes"))
		a := d.MustHave(`.app-footer a[href="/help/changelog"]`)
		if got := strings.TrimSpace(htmlassert.Text(a)); got != "6.5.0" {
			t.Errorf("footer link text = %q, want 6.5.0", got)
		}
	}
}

func TestDevFooterVersionIsPlainText(t *testing.T) {
	s := newServerAt(t, "dev")
	d := doc(t, s.get(t, s.user, "/help/notes"))
	d.MustNotHave(".app-footer a")
	d.MustNotHave(".whats-new-dot")
}

func TestErrorPageKeepsTheBadge(t *testing.T) {
	s := newServer(t)
	rec := s.get(t, s.user, "/help/no-such-page")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("GET /help/no-such-page = %d, want 404", rec.Code)
	}
	doc(t, rec).MustHave(".shell-user-toggle .whats-new-dot")
}

func TestOpeningTheChangelogClearsTheBadge(t *testing.T) {
	s := newServer(t)
	doc(t, s.get(t, s.user, "/help/notes")).MustHave(".whats-new-dot")

	// The changelog itself already renders without the dot...
	d := doc(t, s.get(t, s.user, "/help/changelog"))
	d.MustNotHave(".whats-new-dot")
	d.MustHave(".help-body h1")
	// ...and so does every page after it.
	doc(t, s.get(t, s.user, "/help/notes")).MustNotHave(".whats-new-dot")

	u, err := s.users.UserByUsername(context.Background(), "ilia")
	if err != nil {
		t.Fatal(err)
	}
	if u.SeenVersion != "6.5.0" {
		t.Errorf("SeenVersion = %q, want 6.5.0", u.SeenVersion)
	}
}

func TestSignedOutChangelogRenders(t *testing.T) {
	s := newServer(t)
	rec := s.get(t, nil, "/help/changelog")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /help/changelog signed out = %d, want 200", rec.Code)
	}
	if got := strings.TrimSpace(htmlassert.Text(doc(t, rec).MustHave("title"))); !strings.HasPrefix(got, "What's new") {
		t.Errorf("title = %q, want it to start with What's new", got)
	}
}

func TestDevBuildDoesNotMarkSeen(t *testing.T) {
	s := newServerAt(t, "dev")
	s.get(t, s.user, "/help/changelog")
	u, err := s.users.UserByUsername(context.Background(), "ilia")
	if err != nil {
		t.Fatal(err)
	}
	if u.SeenVersion != "" {
		t.Errorf("SeenVersion = %q after a dev build's changelog, want it left empty", u.SeenVersion)
	}
}
