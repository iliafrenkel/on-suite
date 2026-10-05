package later

import (
	"errors"
	"fmt"
	"html/template"
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

// savedView is the status note shown above the tabs after saving a URL.
type savedView struct {
	ID       int64
	Title    string
	LinkOnly bool
	Existing bool
	NewTab   bool // the Open link opens a new tab (the popup is about to close)
}

// badURLMessage is shown in the save form for a URL Later can't use.
const badURLMessage = "That doesn't look like a web address. It needs to start with http:// or https://."

func (a *App) save(w http.ResponseWriter, r *http.Request) {
	userID, ok := a.userID(w, r)
	if !ok {
		return
	}
	pageURL, err := NormalizeURL(r.PostFormValue("url"))
	popup := r.PostFormValue("popup") == "1"
	htmx := web.IsHTMX(r)
	if err != nil {
		if htmx {
			a.renderChip(w, r, http.StatusUnprocessableEntity, "chip-error", nil)
			return
		}
		if popup {
			a.renderPopup(w, r, http.StatusUnprocessableEntity, popupView{Error: badURLMessage})
			return
		}
		a.renderIndex(w, r, userID, StateUnread, http.StatusUnprocessableEntity, badURLMessage, r.PostFormValue("url"))
		return
	}
	if existing, err := a.store.ArticleByURL(r.Context(), userID, pageURL); err == nil {
		if htmx {
			a.renderChip(w, r, http.StatusOK, "chip-existing", existing.ID)
			return
		}
		if popup {
			http.Redirect(w, r, popupDoneURL(existing.ID, true), http.StatusSeeOther)
			return
		}
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
	if htmx {
		block := "chip-saved"
		if !created {
			block = "chip-existing"
		}
		a.renderChip(w, r, http.StatusOK, block, saved.ID)
		return
	}
	target := fmt.Sprintf("/later/?saved=%d", saved.ID)
	if popup {
		target = popupDoneURL(saved.ID, !created)
	} else if !created {
		target = fmt.Sprintf("/later/?tab=%s&saved=%d&existing=1", saved.State, saved.ID)
	}
	http.Redirect(w, r, target, http.StatusSeeOther)
}

// renderChip answers an HTMX save (ON Reader's button) with a small fragment
// that replaces the button.
func (a *App) renderChip(w http.ResponseWriter, r *http.Request, status int, block string, data any) {
	if err := a.deps.Render.Fragment(w, status, "later/chips", block, data); err != nil {
		a.deps.Errors.Internal(w, r, err)
	}
}

type articleView struct {
	ID      int64
	Title   string
	URL     string
	Site    string
	Byline  string
	Minutes int
	SavedAt time.Time
	// Body is ContentHTML or RenderHighlights over it. This is the only
	// template.HTML conversion in the app, and it is safe because ContentHTML
	// is only ever article.SanitizeWithImages output (the save path) or
	// PastedHTML output (every character escaped); nothing else can write the
	// column. RenderHighlights re-serialises that parsed tree and adds only
	// <mark> elements whose attributes this package builds from integers and
	// fixed class names.
	Body      template.HTML
	LinkOnly  bool
	Reason    string // ExtractError, for link-only
	Archived  bool
	TextError string

	Prefs Prefs
	// SizeDown and SizeUp are the sizes the A- and A+ buttons switch to; 0
	// when already at the bound.
	SizeDown, SizeUp int
	FontOptions      []prefOption
	WidthOptions     []prefOption
	Tab              State   // the list ← Later returns to
	Progress         float64 // 0-1, as stored
	Back             string  // this page, for the Aa forms

	Highlights []highlightView // text order
	Note       string
	OOB        bool // set on HTMX fragment responses
}

type highlightView struct {
	ID      int64
	Quote   string
	Comment string
	Drawn   bool // still matches the text, so it has a <mark id="later-h-{ID}">
}

// prefOption is one button in the Aa menu.
type prefOption struct {
	Value, Label string
	Current      bool
}

func options(current string, pairs ...string) []prefOption {
	var out []prefOption
	for i := 0; i < len(pairs); i += 2 {
		out = append(out, prefOption{Value: pairs[i], Label: pairs[i+1], Current: pairs[i] == current})
	}
	return out
}

// blankTextMessage is shown when the paste-text form is submitted empty.
const blankTextMessage = "Paste some text first."

// buildArticleView assembles everything the reading view shows for art.
func (a *App) buildArticleView(r *http.Request, userID int64, art Article, textError string) (articleView, error) {
	prefs, err := a.store.Prefs(r.Context(), userID)
	if err != nil {
		return articleView{}, err
	}
	view := articleView{
		Prefs:        prefs,
		FontOptions:  options(prefs.Font, "serif", "Serif", "sans", "Sans"),
		WidthOptions: options(prefs.Width, "narrow", "Narrow", "medium", "Medium", "wide", "Wide"),
		Tab:          art.State,
		Progress:     art.Progress,
		Back:         fmt.Sprintf("/later/a/%d", art.ID),
		ID:           art.ID,
		Title:        art.Title,
		URL:          art.URL,
		Site:         art.SiteName,
		Byline:       art.Byline,
		Minutes:      ReadingMinutes(art.WordCount),
		SavedAt:      art.SavedAt,
		LinkOnly:     art.Content == ContentLinkOnly,
		Reason:       art.ExtractError,
		Archived:     art.State == StateArchived,
		TextError:    textError,
	}
	if view.Site == "" {
		view.Site = art.SiteHost
	}
	if strings.EqualFold(strings.TrimSpace(view.Byline), strings.TrimSpace(view.Site)) {
		view.Byline = ""
	}
	if prefs.Size > 1 {
		view.SizeDown = prefs.Size - 1
	}
	if prefs.Size < 5 {
		view.SizeUp = prefs.Size + 1
	}
	hs, err := a.store.Highlights(r.Context(), art.ID)
	if err != nil {
		return articleView{}, err
	}
	runes := []rune(art.ContentText)
	for _, h := range hs {
		view.Highlights = append(view.Highlights, highlightView{
			ID: h.ID, Quote: h.Quote, Comment: h.Comment,
			Drawn: validSpan(runes, h.Start, h.End, h.Quote),
		})
	}
	view.Note = art.Note
	view.Body = template.HTML(RenderHighlights(art.ContentHTML, art.ContentText, hs))
	return view, nil
}

func (a *App) renderArticle(w http.ResponseWriter, r *http.Request, art Article, status int, textError string) {
	userID, ok := a.userID(w, r)
	if !ok {
		return
	}
	view, err := a.buildArticleView(r, userID, art, textError)
	if err != nil {
		a.fail(w, r, err)
		return
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
func (a *App) setState(state State, redirect string) http.HandlerFunc {
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
		http.Redirect(w, r, safeBack(r, redirect), http.StatusSeeOther)
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
	http.Redirect(w, r, safeBack(r, "/later/?tab="+string(art.State)), http.StatusSeeOther)
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

// progress quietly saves how far through an article the reader has scrolled.
// The reader's script posts it; there is nothing to show, so it answers 204.
func (a *App) progress(w http.ResponseWriter, r *http.Request) {
	userID, ok := a.userID(w, r)
	if !ok {
		return
	}
	id, ok := a.pathID(w, r)
	if !ok {
		return
	}
	p, err := strconv.ParseFloat(r.PostFormValue("progress"), 64)
	if err != nil {
		a.fail(w, r, ErrInvalid)
		return
	}
	if err := a.store.SetProgress(r.Context(), userID, id, p); err != nil {
		a.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
