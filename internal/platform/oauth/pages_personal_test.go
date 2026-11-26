package oauth

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/iliafrenkel/on-suite/internal/htmlassert"
	"github.com/iliafrenkel/on-suite/internal/platform/web"
)

const newTokenPath = "/account/assistants/new-token"

// newTokenForm reads the create form as who, and returns the values the
// page's own form would send before the user fills anything in.
func (e *pagesEnv) newTokenForm(t *testing.T, who string) url.Values {
	t.Helper()
	page := e.get(t, who, newTokenPath)
	if page.Code != http.StatusOK {
		t.Fatalf("GET %s as %s = %d", newTokenPath, who, page.Code)
	}
	doc := htmlassert.Parse(t, page.Body.String())
	csrf, _ := htmlassert.Attr(doc.MustHave(`input[name=`+web.CSRFFormField+`]`), "value")
	formID, _ := htmlassert.Attr(doc.MustHave(`input[name=form_id]`), "value")
	return url.Values{web.CSRFFormField: {csrf}, "form_id": {formID}}
}

// newToken fills the create form with extra and posts it as who. It returns
// the response and the exact values sent, so a test can send them again.
func (e *pagesEnv) newToken(t *testing.T, who string, extra url.Values) (*http.Response, string, url.Values) {
	t.Helper()
	form := e.newTokenForm(t, who)
	for k, v := range extra {
		form[k] = v
	}
	rec := e.do(t, http.MethodPost, newTokenPath, form, e.cookies[who])
	return rec.Result(), rec.Body.String(), form
}

func (e *pagesEnv) personalCount(t *testing.T, who string) int {
	t.Helper()
	gs, err := e.srv.store.Grants(context.Background(), e.users[who].ID)
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, g := range gs {
		if g.ClientID == PersonalClientID {
			n++
		}
	}
	return n
}

// storePersonal adds a personal token for who straight into the store.
func (e *pagesEnv) storePersonal(t *testing.T, who string) (token, id string) {
	t.Helper()
	token, hash, err := newPersonalToken()
	if err != nil {
		t.Fatal(err)
	}
	id, _ = newGrantID()
	if err := e.srv.store.CreatePersonalToken(context.Background(), Grant{
		ID: id, UserID: e.users[who].ID, ClientID: PersonalClientID, ClientName: "Pebble ring",
		Requested: []string{"notes:read"}, Granted: []string{"notes:read"},
	}, hash); err != nil {
		t.Fatal(err)
	}
	return token, id
}

var tokenInPage = regexp.MustCompile(`onst_[A-Za-z0-9_-]{43}`)

const idleWarning = "A token that isn't used for 90 days is removed automatically."

func TestNewTokenFormRequiresSignIn(t *testing.T) {
	e := newPagesEnv(t)
	for _, method := range []string{http.MethodGet, http.MethodPost} {
		var form url.Values
		if method == http.MethodPost {
			form = url.Values{}
		}
		rec := e.do(t, method, newTokenPath, form, nil)
		if method == http.MethodGet && (rec.Code != http.StatusSeeOther || !strings.HasPrefix(rec.Header().Get("Location"), "/login")) {
			t.Errorf("anonymous GET = %d %q, want a redirect to /login", rec.Code, rec.Header().Get("Location"))
		}
		if method == http.MethodPost && rec.Code < 300 {
			t.Errorf("anonymous POST = %d, want refused", rec.Code)
		}
	}
}

func TestNewTokenFormDefaults(t *testing.T) {
	e := newPagesEnv(t)
	rec := e.get(t, "ilia", newTokenPath)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	doc := htmlassert.Parse(t, rec.Body.String())
	name := doc.MustHave(`input[name=name]`)
	if ml, _ := htmlassert.Attr(name, "maxlength"); ml != "60" {
		t.Errorf("name maxlength = %q, want 60", ml)
	}
	formID, _ := htmlassert.Attr(doc.MustHave(`input[name=form_id]`), "value")
	if !validGrantID(formID) {
		t.Errorf("form_id = %q, want a valid grant ID", formID)
	}
	for _, sc := range AllScopes {
		box := doc.MustHave(`input[value="` + sc + `"]`)
		_, checked := htmlassert.Attr(box, "checked")
		if want := strings.HasSuffix(sc, ":read"); checked != want {
			t.Errorf("%s checked = %v, want %v", sc, checked, want)
		}
	}
	if !strings.Contains(doc.Text(), idleWarning) {
		t.Errorf("form lacks the 90-day warning:\n%s", doc.Text())
	}
}

func TestNewTokenCreatesAndShowsOnce(t *testing.T) {
	e := newPagesEnv(t)
	res, body, _ := e.newToken(t, "ilia", url.Values{
		"name":        {"Pebble ring"},
		"grant_scope": {"notes:read", "notes:write", "books:write"},
	})
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status = %d:\n%s", res.StatusCode, body)
	}
	if cc := res.Header.Get("Cache-Control"); cc != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store", cc)
	}
	tokens := slices.Compact(tokenInPage.FindAllString(body, -1))
	if len(tokens) != 1 {
		t.Fatalf("page shows %d distinct tokens, want 1", len(tokens))
	}
	text := htmlassert.Parse(t, body).Text()
	for _, want := range []string{e.srv.ResourceURL(), idleWarning} {
		if !strings.Contains(text, want) {
			t.Errorf("page lacks %q:\n%s", want, text)
		}
	}
	list := e.get(t, "ilia", "/account/assistants").Body.String()
	listText := htmlassert.Parse(t, list).Text()
	for _, want := range []string{"Pebble ring", "Personal token", "Not used yet"} {
		if !strings.Contains(listText, want) {
			t.Errorf("list lacks %q:\n%s", want, listText)
		}
	}
	if strings.Contains(list, tokens[0]) {
		t.Error("the list page shows the token")
	}

	// After the list, since Verify records a use.
	c, err := e.srv.Verify(context.Background(), tokens[0])
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if want := []string{"books:write", "notes:read", "notes:write"}; !slices.Equal(c.Scopes, want) {
		t.Errorf("scopes = %v, want %v", c.Scopes, want)
	}
}

func TestNewTokenRefreshCreatesNoSecond(t *testing.T) {
	e := newPagesEnv(t)
	_, _, sent := e.newToken(t, "ilia", url.Values{"name": {"Pebble ring"}, "grant_scope": {"notes:read"}})
	rec := e.do(t, http.MethodPost, newTokenPath, sent, e.cookies["ilia"])
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/account/assistants" {
		t.Errorf("re-sent POST = %d %q, want 303 to /account/assistants", rec.Code, rec.Header().Get("Location"))
	}
	if tokenInPage.MatchString(rec.Body.String()) {
		t.Error("the re-sent POST shows a token")
	}
	if n := e.personalCount(t, "ilia"); n != 1 {
		t.Errorf("personal tokens = %d, want 1", n)
	}
}

func TestNewTokenRejectsBadInput(t *testing.T) {
	cases := map[string]url.Values{
		"empty name":    {"name": {""}, "grant_scope": {"notes:read"}},
		"spaces only":   {"name": {"   "}, "grant_scope": {"notes:read"}},
		"61 runes":      {"name": {strings.Repeat("é", 61)}, "grant_scope": {"notes:read"}},
		"no scopes":     {"name": {"Pebble ring"}},
		"unknown scope": {"name": {"Pebble ring"}, "grant_scope": {"admin:write"}},
	}
	for name, form := range cases {
		t.Run(name, func(t *testing.T) {
			e := newPagesEnv(t)
			res, body, _ := e.newToken(t, "ilia", form)
			if res.StatusCode != http.StatusUnprocessableEntity {
				t.Errorf("status = %d, want 422", res.StatusCode)
			}
			doc := htmlassert.Parse(t, body)
			in := doc.MustHave(`input[name=name]`)
			if v, _ := htmlassert.Attr(in, "value"); v != form.Get("name") {
				t.Errorf("name kept as %q, want %q", v, form.Get("name"))
			}
			if tokenInPage.MatchString(body) {
				t.Error("a refused form shows a token")
			}
			if n := e.personalCount(t, "ilia"); n != 0 {
				t.Errorf("personal tokens = %d, want 0", n)
			}
		})
	}
}

func TestNewTokenRefusedFormKeepsTicks(t *testing.T) {
	e := newPagesEnv(t)
	_, body, _ := e.newToken(t, "ilia", url.Values{"name": {""}, "grant_scope": {"notes:write"}})
	doc := htmlassert.Parse(t, body)
	for sc, want := range map[string]bool{"notes:write": true, "notes:read": false, "books:read": false} {
		_, checked := htmlassert.Attr(doc.MustHave(`input[value="`+sc+`"]`), "checked")
		if checked != want {
			t.Errorf("%s checked = %v, want %v", sc, checked, want)
		}
	}
}

func TestNewTokenBadFormIDIs400(t *testing.T) {
	e := newPagesEnv(t)
	res, _, _ := e.newToken(t, "ilia", url.Values{"form_id": {"nope"}, "name": {"Pebble ring"}, "grant_scope": {"notes:read"}})
	if res.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", res.StatusCode)
	}
	if n := e.personalCount(t, "ilia"); n != 0 {
		t.Errorf("personal tokens = %d, want 0", n)
	}
}

func TestNewTokenLimit(t *testing.T) {
	e := newPagesEnv(t)
	for range maxPersonalTokens {
		e.storePersonal(t, "ilia")
	}
	res, body, _ := e.newToken(t, "ilia", url.Values{"name": {"Pebble ring"}, "grant_scope": {"notes:read"}})
	if res.StatusCode != http.StatusUnprocessableEntity {
		t.Errorf("status = %d, want 422", res.StatusCode)
	}
	if !strings.Contains(htmlassert.Parse(t, body).Text(), "You already have 10 personal tokens.") {
		t.Errorf("no limit message:\n%s", body)
	}
	if n := e.personalCount(t, "ilia"); n != maxPersonalTokens {
		t.Errorf("personal tokens = %d, want %d", n, maxPersonalTokens)
	}
}

func TestNewTokenCSRF(t *testing.T) {
	e := newPagesEnv(t)
	form := e.newTokenForm(t, "ilia")
	form.Del(web.CSRFFormField)
	form.Set("name", "Pebble ring")
	form.Set("grant_scope", "notes:read")
	rec := e.do(t, http.MethodPost, newTokenPath, form, e.cookies["ilia"])
	if rec.Code != http.StatusForbidden {
		t.Errorf("POST without CSRF = %d, want 403", rec.Code)
	}
	if n := e.personalCount(t, "ilia"); n != 0 {
		t.Errorf("personal tokens = %d, want 0", n)
	}
}

func TestDisconnectPersonalTokenOwnOnly(t *testing.T) {
	e := newPagesEnv(t)
	token, id := e.storePersonal(t, "ilia")
	if rec := e.post(t, "mia", "/account/assistants", "/account/assistants/"+id+"/disconnect"); rec.Code != http.StatusNotFound {
		t.Errorf("mia's disconnect = %d, want 404", rec.Code)
	}
	if _, err := e.srv.Verify(context.Background(), token); err != nil {
		t.Fatalf("token after mia's attempt: %v", err)
	}
	if rec := e.post(t, "ilia", "/account/assistants", "/account/assistants/"+id+"/disconnect"); rec.Code != http.StatusSeeOther {
		t.Errorf("ilia's disconnect = %d, want 303", rec.Code)
	}
	if _, err := e.srv.Verify(context.Background(), token); !errors.Is(err, ErrInvalidToken) {
		t.Errorf("token after disconnect: %v, want ErrInvalidToken", err)
	}
}

func TestAdminAIPageLabelsPersonalTokens(t *testing.T) {
	e := newPagesEnv(t)
	e.storePersonal(t, "ilia")
	text := htmlassert.Parse(t, e.get(t, "root", "/admin/ai").Body.String()).Text()
	for _, want := range []string{"Pebble ring", "Personal token"} {
		if !strings.Contains(text, want) {
			t.Errorf("admin page lacks %q:\n%s", want, text)
		}
	}
}
