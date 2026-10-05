package later

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/iliafrenkel/on-suite/internal/platform/web"
)

// userID is the signed-in user; every route is registered with Handle.
func (a *App) userID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	u, ok := web.UserFrom(r.Context())
	if !ok {
		a.deps.Errors.Status(w, r, http.StatusUnauthorized)
		return 0, false
	}
	return u.ID, true
}

// fail maps a store error to its response: someone else's row is a 404,
// bad input a 400, anything else a logged 500.
func (a *App) fail(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, ErrNotFound):
		a.deps.Errors.Status(w, r, http.StatusNotFound)
	case errors.Is(err, ErrInvalid):
		a.deps.Errors.Status(w, r, http.StatusBadRequest)
	default:
		a.deps.Errors.Internal(w, r, err)
	}
}

// indexData is the list page's view model; Task 5 grows it.
type indexData struct {
	FormError string
	FormValue string
}

func (a *App) index(w http.ResponseWriter, r *http.Request) {
	userID, ok := a.userID(w, r)
	if !ok {
		return
	}
	a.renderIndex(w, r, userID, StateUnread, http.StatusOK, "", "")
}

// renderIndex draws the list page. Task 5 will use userID and tab to load
// the rows; for now they only select what the page would show.
func (a *App) renderIndex(w http.ResponseWriter, r *http.Request, userID int64, tab State, status int, formError, formValue string) {
	_, _ = userID, tab
	page := a.deps.Page(r, "")
	page.Data = indexData{FormError: formError, FormValue: formValue}
	if err := a.deps.Render.Page(w, status, "later/index", page); err != nil {
		a.deps.Errors.Internal(w, r, err)
	}
}

// badURLMessage is shown in the save form for a URL Later can't use.
const badURLMessage = "That doesn't look like a web address. It needs to start with http:// or https://."

func (a *App) save(w http.ResponseWriter, r *http.Request) {
	userID, ok := a.userID(w, r)
	if !ok {
		return
	}
	pageURL, err := NormalizeURL(r.PostFormValue("url"))
	if err != nil {
		a.renderIndex(w, r, userID, StateUnread, http.StatusUnprocessableEntity, badURLMessage, r.PostFormValue("url"))
		return
	}
	if existing, err := a.store.ArticleByURL(r.Context(), userID, pageURL); err == nil {
		http.Redirect(w, r, fmt.Sprintf("/later/a/%d?existing=1", existing.ID), http.StatusSeeOther)
		return
	} else if !errors.Is(err, ErrNotFound) {
		a.fail(w, r, err)
		return
	}
	n := a.fetchArticle(r.Context(), pageURL)
	saved, created, err := a.store.Save(r.Context(), userID, n)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	target := fmt.Sprintf("/later/a/%d", saved.ID)
	if !created {
		target += "?existing=1"
	}
	http.Redirect(w, r, target, http.StatusSeeOther)
}
