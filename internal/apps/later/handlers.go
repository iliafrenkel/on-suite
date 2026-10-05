package later

import (
	"errors"
	"fmt"
	"math"
	"net/http"
	"strconv"

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

const pageSize = 50

type tabView struct {
	State   State
	Label   string
	Count   int
	Current bool
}

type rowView struct {
	ID       int64
	Title    string
	Site     string
	Minutes  int
	LinkOnly bool
	Progress int // percent, 0-100
}

type indexView struct {
	Tabs       []tabView
	Tab        State
	Rows       []rowView
	NextOffset int // 0 when there are no more rows
	FormError  string
	FormValue  string
	EmptyText  string
}

var tabs = []struct {
	state State
	label string
	empty string
}{
	{StateUnread, "Unread", "Nothing to read. Paste a URL above to save an article."},
	{StateReading, "Reading", "Nothing in progress."},
	{StateArchived, "Archived", "Nothing archived yet."},
}

// parseTab maps the tab query value to a state; anything else is Unread.
func parseTab(v string) State {
	for _, t := range tabs {
		if string(t.state) == v {
			return t.state
		}
	}
	return StateUnread
}

func (a *App) index(w http.ResponseWriter, r *http.Request) {
	userID, ok := a.userID(w, r)
	if !ok {
		return
	}
	tab := parseTab(r.URL.Query().Get("tab"))
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	if offset < 0 {
		offset = 0
	}
	a.renderListPage(w, r, userID, tab, offset, http.StatusOK, "", "")
}

func (a *App) renderIndex(w http.ResponseWriter, r *http.Request, userID int64, tab State, status int, formError, formValue string) {
	a.renderListPage(w, r, userID, tab, 0, status, formError, formValue)
}

// renderListPage draws the list page, or just its rows when HTMX asks for the
// next page.
func (a *App) renderListPage(w http.ResponseWriter, r *http.Request, userID int64, tab State, offset, status int, formError, formValue string) {
	ctx := r.Context()
	items, err := a.store.List(ctx, userID, tab, offset, pageSize+1)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	counts, err := a.store.Counts(ctx, userID)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	view := indexView{Tab: tab, FormError: formError, FormValue: formValue}
	if len(items) > pageSize {
		items = items[:pageSize]
		view.NextOffset = offset + pageSize
	}
	for _, it := range items {
		view.Rows = append(view.Rows, rowView{
			ID:       it.ID,
			Title:    it.Title,
			Site:     it.SiteHost,
			Minutes:  ReadingMinutes(it.WordCount),
			LinkOnly: it.Content == ContentLinkOnly,
			Progress: int(math.Round(it.Progress * 100)),
		})
	}
	for _, t := range tabs {
		view.Tabs = append(view.Tabs, tabView{State: t.state, Label: t.label, Count: counts[t.state], Current: t.state == tab})
		if t.state == tab {
			view.EmptyText = t.empty
		}
	}
	page := a.deps.Page(r, "")
	page.Data = view
	if web.IsHTMX(r) && !web.IsHTMXHistoryRestore(r) && offset > 0 {
		if err := a.deps.Render.Fragment(w, status, "later/index", "rows", page); err != nil {
			a.deps.Errors.Internal(w, r, err)
		}
		return
	}
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
