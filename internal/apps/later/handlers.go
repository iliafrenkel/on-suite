package later

import (
	"errors"
	"fmt"
	"html/template"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

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

// pathID parses the {id} path segment; anything but a positive integer is a
// 404, the same as an article that isn't there.
func (a *App) pathID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		a.deps.Errors.Status(w, r, http.StatusNotFound)
		return 0, false
	}
	return id, true
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
	Saved      *savedView // the note after a save, or nil
}

// savedView is the status note shown above the tabs after saving a URL.
type savedView struct {
	ID       int64
	Title    string
	LinkOnly bool
	Existing bool
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
	a.renderListPage(w, r, userID, tab, offset, http.StatusOK, "", "", a.savedNote(r, userID))
}

// savedNote loads the article named by ?saved= for the note after a save.
// A missing, malformed or someone else's id shows no note.
func (a *App) savedNote(r *http.Request, userID int64) *savedView {
	id, err := strconv.ParseInt(r.URL.Query().Get("saved"), 10, 64)
	if err != nil || id <= 0 {
		return nil
	}
	art, err := a.store.Article(r.Context(), userID, id)
	if err != nil {
		return nil
	}
	return &savedView{
		ID:       art.ID,
		Title:    art.Title,
		LinkOnly: art.Content == ContentLinkOnly,
		Existing: r.URL.Query().Get("existing") == "1",
	}
}

func (a *App) renderIndex(w http.ResponseWriter, r *http.Request, userID int64, tab State, status int, formError, formValue string) {
	a.renderListPage(w, r, userID, tab, 0, status, formError, formValue, nil)
}

// renderListPage draws the list page, or just its rows when HTMX asks for the
// next page.
func (a *App) renderListPage(w http.ResponseWriter, r *http.Request, userID int64, tab State, offset, status int, formError, formValue string, saved *savedView) {
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
	view := indexView{Tab: tab, FormError: formError, FormValue: formValue, Saved: saved}
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
	page := a.deps.Page(r, "ON Later")
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
		http.Redirect(w, r, fmt.Sprintf("/later/?tab=%s&saved=%d&existing=1", existing.State, existing.ID), http.StatusSeeOther)
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
	target := fmt.Sprintf("/later/?saved=%d", saved.ID)
	if !created {
		target = fmt.Sprintf("/later/?tab=%s&saved=%d&existing=1", saved.State, saved.ID)
	}
	http.Redirect(w, r, target, http.StatusSeeOther)
}

type articleView struct {
	ID      int64
	Title   string
	URL     string
	Site    string
	Byline  string
	Minutes int
	SavedAt time.Time
	// Body is the stored snapshot. This is the only template.HTML conversion
	// in the app, and it is safe because ContentHTML is only ever
	// article.SanitizeWithImages output (the save path) or PastedHTML output
	// (every character escaped); nothing else can write the column.
	Body      template.HTML
	LinkOnly  bool
	Reason    string // ExtractError, for link-only
	Archived  bool
	TextError string
}

// blankTextMessage is shown when the paste-text form is submitted empty.
const blankTextMessage = "Paste some text first."

func (a *App) renderArticle(w http.ResponseWriter, r *http.Request, art Article, status int, textError string) {
	view := articleView{
		ID:        art.ID,
		Title:     art.Title,
		URL:       art.URL,
		Site:      art.SiteName,
		Byline:    art.Byline,
		Minutes:   ReadingMinutes(art.WordCount),
		SavedAt:   art.SavedAt,
		Body:      template.HTML(art.ContentHTML),
		LinkOnly:  art.Content == ContentLinkOnly,
		Reason:    art.ExtractError,
		Archived:  art.State == StateArchived,
		TextError: textError,
	}
	if view.Site == "" {
		view.Site = art.SiteHost
	}
	page := a.deps.Page(r, art.Title)
	page.Data = view
	if err := a.deps.Render.Page(w, status, "later/article", page); err != nil {
		a.deps.Errors.Internal(w, r, err)
	}
}

// view shows an article, marking it opened first so the page reflects the
// new state.
func (a *App) view(w http.ResponseWriter, r *http.Request) {
	userID, ok := a.userID(w, r)
	if !ok {
		return
	}
	id, ok := a.pathID(w, r)
	if !ok {
		return
	}
	if err := a.store.MarkOpened(r.Context(), userID, id); err != nil {
		a.fail(w, r, err)
		return
	}
	art, err := a.store.Article(r.Context(), userID, id)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	a.renderArticle(w, r, art, http.StatusOK, "")
}

// setState archives or un-archives an article, then goes to where the user
// will want to be next.
func (a *App) setState(state State, redirect func(id int64) string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID, ok := a.userID(w, r)
		if !ok {
			return
		}
		id, ok := a.pathID(w, r)
		if !ok {
			return
		}
		if err := a.store.SetState(r.Context(), userID, id, state); err != nil {
			a.fail(w, r, err)
			return
		}
		http.Redirect(w, r, redirect(id), http.StatusSeeOther)
	}
}

// delete removes an article and goes back to the list it was in. The article
// is loaded first both to learn that list and to 404 for someone else's.
func (a *App) delete(w http.ResponseWriter, r *http.Request) {
	userID, ok := a.userID(w, r)
	if !ok {
		return
	}
	id, ok := a.pathID(w, r)
	if !ok {
		return
	}
	art, err := a.store.Article(r.Context(), userID, id)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	if err := a.store.Delete(r.Context(), userID, id); err != nil {
		a.fail(w, r, err)
		return
	}
	http.Redirect(w, r, "/later/?tab="+string(art.State), http.StatusSeeOther)
}

// pasteText gives a link-only article the text the user pasted.
func (a *App) pasteText(w http.ResponseWriter, r *http.Request) {
	userID, ok := a.userID(w, r)
	if !ok {
		return
	}
	id, ok := a.pathID(w, r)
	if !ok {
		return
	}
	art, err := a.store.Article(r.Context(), userID, id)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	text := r.PostFormValue("text")
	if strings.TrimSpace(text) == "" {
		a.renderArticle(w, r, art, http.StatusUnprocessableEntity, blankTextMessage)
		return
	}
	if err := a.store.SetPastedText(r.Context(), userID, id, text); err != nil {
		a.fail(w, r, err)
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/later/a/%d", id), http.StatusSeeOther)
}
