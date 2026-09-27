package usermgmt_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/iliafrenkel/on-suite/internal/htmlassert"
	"github.com/iliafrenkel/on-suite/internal/platform/auth"
)

func TestAnonymousIsSentToLoginFromTheUsersPage(t *testing.T) {
	s := newServer(t)
	rec := s.get(t, nil, "/admin/users")
	if rec.Code != http.StatusSeeOther || !strings.HasPrefix(rec.Header().Get("Location"), "/login") {
		t.Fatalf("status = %d, Location = %q; want 303 to /login", rec.Code, rec.Header().Get("Location"))
	}
}

func TestANonAdminGetsTheSame404AsAMissingPage(t *testing.T) {
	s := newServer(t)
	missing := s.get(t, s.plain, "/no-such-page")
	got := s.get(t, s.plain, "/admin/users")
	if got.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", got.Code)
	}
	if got.Body.String() != missing.Body.String() {
		t.Error("/admin/users renders differently from a missing page for a non-admin")
	}

	missingPost := s.post(t, s.plain, "/no-such-page", url.Values{})
	if rec := s.post(t, s.plain, "/admin/users", url.Values{"username": {"mallory"}}); rec.Code != http.StatusNotFound || rec.Body.String() != missingPost.Body.String() {
		t.Errorf("POST /admin/users as non-admin = %d, want the missing-page 404", rec.Code)
	}
	if _, err := s.users.UserByUsername(t.Context(), "mallory"); !errors.Is(err, auth.ErrNotFound) {
		t.Error("a non-admin created an account")
	}
}

func TestTheUsersPageGivesOthersAMenuAndYouALinkToAccount(t *testing.T) {
	s := newServer(t)
	rec := s.get(t, s.root, "/admin/users")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	doc := s.doc(t, rec)
	doc.MustHave(`tr[data-user="ilia"] details`)
	doc.MustNotHave(`tr[data-user="root"] details`)
	doc.MustHave(`tr[data-user="root"] a[href="/account"]`)
}

// generated pulls the one-time password out of a response.
func generated(t *testing.T, doc *htmlassert.Doc) string {
	t.Helper()
	return strings.TrimSpace(htmlassert.Text(doc.MustHave(`[data-generated-password]`)))
}

func TestAddingAUserShowsAWorkingPasswordExactlyOnce(t *testing.T) {
	s := newServer(t)
	rec := s.post(t, s.root, "/admin/users", url.Values{"username": {"alice"}})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d; body: %s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store", got)
	}
	password := generated(t, s.doc(t, rec))

	if s.user(t, "alice").IsAdmin {
		t.Error("alice was made an admin without asking")
	}
	if login := s.tryLogIn(t, "alice", password); login.Code != http.StatusSeeOther {
		t.Errorf("signing in with the generated password = %d, want 303", login.Code)
	}
	if again := s.get(t, s.root, "/admin/users"); strings.Contains(again.Body.String(), password) {
		t.Error("the password is shown a second time")
	}
}

func TestAddingAnAdministrator(t *testing.T) {
	s := newServer(t)
	if rec := s.post(t, s.root, "/admin/users", url.Values{"username": {"alice"}, "admin": {"1"}}); rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if !s.user(t, "alice").IsAdmin {
		t.Error("the Administrator box was ignored")
	}
}

func TestAddingABadUsernameShowsANoticeAndKeepsTheForm(t *testing.T) {
	for _, name := range []string{"a", "ILIA", "has space"} {
		t.Run(name, func(t *testing.T) {
			s := newServer(t)
			rec := s.post(t, s.root, "/admin/users", url.Values{"username": {name}, "admin": {"1"}})
			if rec.Code != http.StatusUnprocessableEntity {
				t.Fatalf("status = %d, want 422", rec.Code)
			}
			doc := s.doc(t, rec)
			doc.MustHave(".notice-error")
			doc.MustNotHave("[data-generated-password]")
			input := doc.MustHave(`input[name="username"]`)
			if v, _ := htmlassert.Attr(input, "value"); v != name {
				t.Errorf("username field = %q, want %q kept", v, name)
			}
			doc.MustHave(`input[checked]`) // the Administrator box, the only checkbox
		})
	}
}

// The session's CSRF cookie is sent but the form carries no token: exactly
// the shape of a cross-site forged submission.
func TestAddWithoutACSRFTokenIsRejected(t *testing.T) {
	s := newServer(t)
	req := httptest.NewRequest("POST", "/admin/users", strings.NewReader("username=alice"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if rec := s.do(t, s.root, req); rec.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403", rec.Code)
	}
	if _, err := s.users.UserByUsername(t.Context(), "alice"); !errors.Is(err, auth.ErrNotFound) {
		t.Error("an account was created without a CSRF token")
	}
}

func TestAccountChangesAreLoggedWithoutPasswords(t *testing.T) {
	s := newServer(t)
	rec := s.post(t, s.root, "/admin/users", url.Values{"username": {"alice"}})
	password := generated(t, s.doc(t, rec))

	logs := s.logs.String()
	for _, want := range []string{"action=user.add", "actor=root", "target=alice"} {
		if !strings.Contains(logs, want) {
			t.Errorf("logs lack %q", want)
		}
	}
	if strings.Contains(logs, password) {
		t.Error("the generated password was logged")
	}
}
