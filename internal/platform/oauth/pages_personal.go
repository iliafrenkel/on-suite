package oauth

import (
	"errors"
	"net/http"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/iliafrenkel/on-suite/internal/platform/web"
)

// The create-a-token pages (spec 2026-11-26-personal-tokens-design.md,
// "Account page").

// maxTokenName is the longest name a personal token may have, in characters.
const maxTokenName = 60

const tooManyTokens = "You already have 10 personal tokens. Disconnect one you no longer use first."

type newTokenPage struct {
	FormID, Name, Error string
	Rows                []consentRow
}

type tokenCreatedPage struct {
	Token, ServerURL string
}

// tokenRows is the consent screen's table for every scope, ticked as
// checked says.
func tokenRows(checked func(scope string) bool) []consentRow {
	rows := consentRows(AllScopes)
	for _, row := range rows {
		for _, box := range []*consentBox{row.Read, row.Write} {
			if box != nil {
				box.Checked = checked(box.Scope)
			}
		}
	}
	return rows
}

// newTokenForm shows the empty form. Reads start ticked and writes unticked,
// as at consent. form_id is a fresh grant ID: the POST creates the grant
// under it, so a re-sent POST finds it taken and creates nothing.
func (s *Server) newTokenForm(d PageDeps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := newGrantID()
		if err != nil {
			d.Errors.Internal(w, r, err)
			return
		}
		s.renderPage(w, r, d, http.StatusOK, "account_new_token", "Create a personal token", "", newTokenPage{
			FormID: id,
			Rows:   tokenRows(func(sc string) bool { return strings.HasSuffix(sc, ":read") }),
		})
	}
}

// createToken stores a personal token and shows it, once.
func (s *Server) createToken(d PageDeps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			d.Errors.Status(w, r, http.StatusBadRequest)
			return
		}
		formID := r.PostFormValue("form_id")
		if !validGrantID(formID) {
			d.Errors.Status(w, r, http.StatusBadRequest)
			return
		}
		name := strings.TrimSpace(r.PostFormValue("name"))
		scopes := r.PostForm["grant_scope"]
		page := newTokenPage{
			FormID: formID, Name: r.PostFormValue("name"),
			Rows: tokenRows(func(sc string) bool { return slices.Contains(scopes, sc) }),
		}
		refuse := func(msg string) {
			page.Error = msg
			s.renderPage(w, r, d, http.StatusUnprocessableEntity, "account_new_token", "Create a personal token", "", page)
		}
		switch {
		case name == "":
			refuse("Give the token a name, so you can tell it apart later.")
			return
		case utf8.RuneCountInString(name) > maxTokenName:
			refuse("Keep the name to 60 characters or fewer.")
			return
		case len(scopes) == 0:
			refuse("Tick at least one box.")
			return
		}
		for _, sc := range scopes {
			if !slices.Contains(AllScopes, sc) {
				refuse("Tick only the boxes shown.")
				return
			}
		}
		granted := slices.Clone(scopes)
		slices.Sort(granted)
		granted = slices.Compact(granted)

		token, hash, err := newPersonalToken()
		if err != nil {
			d.Errors.Internal(w, r, err)
			return
		}
		me, _ := web.UserFrom(r.Context())
		err = s.store.CreatePersonalToken(r.Context(), Grant{
			ID: formID, UserID: me.ID, ClientID: PersonalClientID, ClientName: name,
			Requested: granted, Granted: granted,
		}, hash)
		switch {
		case errors.Is(err, ErrGrantExists):
			http.Redirect(w, r, "/account/assistants", http.StatusSeeOther)
			return
		case errors.Is(err, ErrTooManyTokens):
			refuse(tooManyTokens)
			return
		case err != nil:
			d.Errors.Internal(w, r, err)
			return
		}
		// The token is in this response only: never in a URL, never cached.
		w.Header().Set("Cache-Control", "no-store")
		s.renderPage(w, r, d, http.StatusOK, "account_token_created", "Your token is ready", "",
			tokenCreatedPage{Token: token, ServerURL: s.ResourceURL()})
	}
}
