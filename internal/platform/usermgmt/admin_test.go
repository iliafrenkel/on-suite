package usermgmt_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/iliafrenkel/on-suite/internal/apptest"
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
	const adminBox = `form.usermgmt-add input[name="admin"]`
	for _, name := range []string{"a", "ILIA", "has space"} {
		for _, admin := range []bool{true, false} {
			t.Run(fmt.Sprintf("%s/admin=%v", name, admin), func(t *testing.T) {
				s := newServer(t)
				form := url.Values{"username": {name}}
				if admin {
					form.Set("admin", "1")
				}
				rec := s.post(t, s.root, "/admin/users", form)
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
				if _, checked := htmlassert.Attr(doc.MustHave(adminBox), "checked"); checked != admin {
					t.Errorf("Administrator box checked = %v, want %v kept", checked, admin)
				}
			})
		}
	}
}

func TestAddingAUserTrimsTheUsername(t *testing.T) {
	s := newServer(t)
	rec := s.post(t, s.root, "/admin/users", url.Values{"username": {"  alice \t"}})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d; body: %s", rec.Code, rec.Body.String())
	}
	password := generated(t, s.doc(t, rec))
	s.user(t, "alice") // fails the test if alice wasn't created under the trimmed name
	if login := s.tryLogIn(t, "alice", password); login.Code != http.StatusSeeOther {
		t.Errorf("signing in as the trimmed name = %d, want 303", login.Code)
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

func path(format string, id int64) string { return fmt.Sprintf(format, id) }

func TestResettingAPasswordShowsANewOneAndSignsTheUserOut(t *testing.T) {
	s := newServer(t)
	ilia := s.plain.user

	rec := s.post(t, s.root, path("/admin/users/%d/password", ilia.ID), url.Values{})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d; body: %s", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("Cache-Control") != "no-store" {
		t.Error("the reset response may be cached")
	}
	password := generated(t, s.doc(t, rec))

	if again := s.get(t, s.plain, "/admin/users"); again.Code != http.StatusSeeOther {
		t.Errorf("ilia's old session still works: status = %d", again.Code)
	}
	if s.tryLogIn(t, "ilia", password).Code != http.StatusSeeOther {
		t.Error("the new password does not sign in")
	}
	if s.tryLogIn(t, "ilia", apptest.Password).Code == http.StatusSeeOther {
		t.Error("the old password still signs in")
	}
}

func TestPromotingAndDemoting(t *testing.T) {
	s := newServer(t)
	id := s.plain.user.ID

	rec := s.post(t, s.root, path("/admin/users/%d/role", id), url.Values{"admin": {"1"}})
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/admin/users" {
		t.Fatalf("promote = %d → %q, want 303 → /admin/users", rec.Code, rec.Header().Get("Location"))
	}
	if !s.user(t, "ilia").IsAdmin {
		t.Fatal("promote did not take")
	}
	// The new role applies to ilia's existing session at once.
	if got := s.get(t, s.plain, "/admin/users"); got.Code != http.StatusOK {
		t.Errorf("promoted ilia gets %d on /admin/users, want 200", got.Code)
	}

	s.post(t, s.root, path("/admin/users/%d/role", id), url.Values{"admin": {"0"}})
	if s.user(t, "ilia").IsAdmin {
		t.Error("demote did not take")
	}
}

func TestDeletingAUserAsksFirstThenRemovesThem(t *testing.T) {
	s := newServer(t)
	id := s.plain.user.ID

	confirm := s.get(t, s.root, path("/admin/users/%d/delete", id))
	if confirm.Code != http.StatusOK {
		t.Fatalf("confirm page = %d", confirm.Code)
	}
	doc := s.doc(t, confirm)
	if !strings.Contains(doc.Text(), "Permanently delete") || !strings.Contains(doc.Text(), "ilia") {
		t.Error("the confirmation page does not say who and what is deleted")
	}
	doc.MustHave(fmt.Sprintf(`form[action="/admin/users/%d/delete"]`, id))
	if _, err := s.users.UserByID(context.Background(), id); err != nil {
		t.Fatal("merely viewing the confirmation deleted the user")
	}

	rec := s.post(t, s.root, path("/admin/users/%d/delete", id), url.Values{})
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/admin/users" {
		t.Fatalf("delete = %d → %q", rec.Code, rec.Header().Get("Location"))
	}
	if _, err := s.users.UserByID(context.Background(), id); !errors.Is(err, auth.ErrNotFound) {
		t.Errorf("user still exists: err = %v", err)
	}
	if !strings.Contains(s.logs.String(), "action=user.delete") {
		t.Error("the delete was not logged")
	}
}

func TestAnAdminCannotActOnThemselvesHere(t *testing.T) {
	s := newServer(t)
	id := s.root.user.ID
	before := s.user(t, "root").PasswordHash

	for _, tc := range []struct{ method, path string }{
		{"POST", path("/admin/users/%d/password", id)},
		{"POST", path("/admin/users/%d/role", id)},
		{"GET", path("/admin/users/%d/delete", id)},
		{"POST", path("/admin/users/%d/delete", id)},
	} {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			var rec *httptest.ResponseRecorder
			if tc.method == "GET" {
				rec = s.get(t, s.root, tc.path)
			} else {
				rec = s.post(t, s.root, tc.path, url.Values{"admin": {"0"}})
			}
			if rec.Code != http.StatusUnprocessableEntity {
				t.Fatalf("status = %d, want 422", rec.Code)
			}
			s.doc(t, rec).MustHave(".notice-error")
		})
	}

	root := s.user(t, "root")
	if !root.IsAdmin || root.PasswordHash != before {
		t.Error("a self-action changed the account")
	}
	if s.get(t, s.root, "/admin/users").Code != http.StatusOK {
		t.Error("a self-action ended root's session")
	}
}

// The table used to sit in a .scroll-x wrapper so narrow screens could
// scroll it instead of the page, but overflow-x: auto also computes
// overflow-y as auto, which clipped each row's absolutely-positioned menu.
func TestTheAccountsTableIsNotInAnOverflowContainer(t *testing.T) {
	s := newServer(t)
	rec := s.get(t, s.root, "/admin/users")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	doc := s.doc(t, rec)
	doc.MustNotHave("#accounts .scroll-x")
}

func TestUnknownOrMalformedIDsAre404(t *testing.T) {
	s := newServer(t)
	for _, p := range []string{"/admin/users/9999/password", "/admin/users/abc/role", "/admin/users/9999/delete"} {
		if rec := s.post(t, s.root, p, url.Values{}); rec.Code != http.StatusNotFound {
			t.Errorf("POST %s = %d, want 404", p, rec.Code)
		}
	}
	if rec := s.get(t, s.root, "/admin/users/9999/delete"); rec.Code != http.StatusNotFound {
		t.Errorf("GET confirm for a missing user = %d, want 404", rec.Code)
	}
}

func TestEveryMutatingRouteIs404ForANonAdmin(t *testing.T) {
	s := newServer(t)
	id := s.root.user.ID
	for _, p := range []string{
		path("/admin/users/%d/password", id),
		path("/admin/users/%d/role", id),
		path("/admin/users/%d/delete", id),
	} {
		if rec := s.post(t, s.plain, p, url.Values{"admin": {"0"}}); rec.Code != http.StatusNotFound {
			t.Errorf("POST %s as non-admin = %d, want 404", p, rec.Code)
		}
	}
	if rec := s.get(t, s.plain, path("/admin/users/%d/delete", id)); rec.Code != http.StatusNotFound {
		t.Errorf("GET confirm as non-admin = %d, want 404", rec.Code)
	}
	if !s.user(t, "root").IsAdmin {
		t.Error("a non-admin demoted root")
	}
}
