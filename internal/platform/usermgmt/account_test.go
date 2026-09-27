package usermgmt_test

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/iliafrenkel/on-suite/internal/apptest"
)

func TestAnonymousAccountRedirectsToLogin(t *testing.T) {
	s := newServer(t)
	rec := s.get(t, nil, "/account")
	if rec.Code != http.StatusSeeOther || !strings.HasPrefix(rec.Header().Get("Location"), "/login") {
		t.Fatalf("status = %d, Location = %q", rec.Code, rec.Header().Get("Location"))
	}
}

func TestTheAccountPageShowsWhoYouAreAndTheHeaderLinksToIt(t *testing.T) {
	s := newServer(t)
	rec := s.get(t, s.plain, "/account")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	doc := s.doc(t, rec)
	if !strings.Contains(doc.Text(), "ilia") {
		t.Error("the page does not show the username")
	}
	doc.MustHave(`.shell-user a[href="/account"]`)
	doc.MustHave(`form[action="/account/password"]`)
}

func TestChangingYourPasswordKeepsThisSessionAndEndsTheOthers(t *testing.T) {
	s := newServer(t)
	other := s.logIn(t, "ilia", apptest.Password)
	const next = "a-brand-new-password"

	rec := s.post(t, s.plain, "/account/password", url.Values{
		"current": {apptest.Password}, "new": {next}, "confirm": {next},
	})
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/account?changed=1" {
		t.Fatalf("status = %d → %q, want 303 → /account?changed=1", rec.Code, rec.Header().Get("Location"))
	}

	page := s.get(t, s.plain, "/account?changed=1")
	if page.Code != http.StatusOK || !strings.Contains(page.Body.String(), "Password changed") {
		t.Errorf("this session lost access or shows no confirmation (status %d)", page.Code)
	}
	if s.get(t, other, "/account").Code != http.StatusSeeOther {
		t.Error("the other session survived the change")
	}
	if s.tryLogIn(t, "ilia", next).Code != http.StatusSeeOther {
		t.Error("the new password does not sign in")
	}
	if !strings.Contains(s.logs.String(), "action=account.change_password") {
		t.Error("the change was not logged")
	}
	if strings.Contains(s.logs.String(), next) {
		t.Error("the new password was logged")
	}
}

func TestChangePasswordRejections(t *testing.T) {
	tests := []struct {
		name                  string
		current, next, repeat string
	}{
		{"wrong current password", "not-my-password", "a-brand-new-password", "a-brand-new-password"},
		{"confirmation does not match", apptest.Password, "a-brand-new-password", "a-different-password"},
		{"new password too short", apptest.Password, "short", "short"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := newServer(t)
			rec := s.post(t, s.plain, "/account/password", url.Values{
				"current": {tt.current}, "new": {tt.next}, "confirm": {tt.repeat},
			})
			if rec.Code != http.StatusUnprocessableEntity {
				t.Fatalf("status = %d, want 422", rec.Code)
			}
			doc := s.doc(t, rec)
			doc.MustHave(".notice-error")
			for _, pw := range []string{tt.current, tt.next} {
				if strings.Contains(rec.Body.String(), `value="`+pw+`"`) {
					t.Errorf("a password (%q) was echoed back into the form", pw)
				}
			}
			if s.user(t, "ilia").PasswordHash != apptest.PasswordHash {
				t.Error("the password changed despite the rejection")
			}
		})
	}
}
