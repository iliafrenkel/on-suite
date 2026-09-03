package paste

import (
	"context"
	"errors"
	"html/template"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/iliafrenkel/on-suite/internal/platform/render"
	"github.com/iliafrenkel/on-suite/internal/platform/web"
)

// viewModel is what the shared template renders. It carries the
// already-highlighted body so the template never calls into Chroma.
type viewModel struct {
	Snippet   Snippet
	Highlight template.HTML
	Language  string
	ShareURL  string
	RawURL    string
	// Owner is true on the owner's own view, false on a shared page, which is
	// what decides whether the destructive controls render at all.
	Owner bool
}

// userID is the signed-in user's id. Handlers registered with Handle are
// guarded, so a missing user is a programming error rather than a bad request.
func (a *App) userID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	u, ok := web.UserFrom(r.Context())
	if !ok {
		a.deps.Errors.Status(w, r, http.StatusUnauthorized)
		return 0, false
	}
	return u.ID, true
}

// snippetID parses the {id} wildcard.
func (a *App) snippetID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		a.deps.Errors.Status(w, r, http.StatusNotFound)
		return 0, false
	}
	return id, true
}

// fail maps a store error onto a response. ErrNotFound becomes a 404 whether
// the snippet is missing or simply someone else's, so the two are
// indistinguishable from outside.
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

func (a *App) newForm(w http.ResponseWriter, r *http.Request) {
	userID, ok := a.userID(w, r)
	if !ok {
		return
	}
	a.renderIndex(w, r, userID, http.StatusOK, a.newDetail(r, "", "", "", ""))
}

func (a *App) create(w http.ResponseWriter, r *http.Request) {
	userID, ok := a.userID(w, r)
	if !ok {
		return
	}

	title := r.PostFormValue("title")
	language := r.PostFormValue("language")
	body := r.PostFormValue("body")

	// Reject a language that is not on the offered list, so a hand-crafted
	// post cannot store an arbitrary string that later renders as plain text
	// with no explanation.
	if !IsLanguage(language) {
		a.renderIndex(w, r, userID, http.StatusBadRequest, a.newDetail(r, "That is not a language I know.", title, language, body))
		return
	}
	if err := Validate(title, body); err != nil {
		a.renderIndex(w, r, userID, http.StatusBadRequest, a.newDetail(r, userMessage(err), title, language, body))
		return
	}

	s, err := a.store.Create(r.Context(), userID, title, language, body)
	if err != nil {
		if errors.Is(err, ErrInvalid) {
			a.renderIndex(w, r, userID, http.StatusBadRequest, a.newDetail(r, userMessage(err), title, language, body))
			return
		}
		a.deps.Errors.Internal(w, r, err)
		return
	}

	if !web.IsHTMX(r) {
		http.Redirect(w, r, "/paste/"+strconv.FormatInt(s.ID, 10), http.StatusSeeOther)
		return
	}
	a.renderDetailWithList(w, r, userID, http.StatusCreated, a.viewDetail(r, s))
}

func (a *App) render(w http.ResponseWriter, r *http.Request, status int, name string, page render.Page) {
	if err := a.deps.Render.Page(w, status, name, page); err != nil {
		a.deps.Errors.Internal(w, r, err)
	}
}

// shareURL is the public path for a shared snippet, or "" when it is private.
func shareURL(s Snippet) string {
	if !s.Shared() {
		return ""
	}
	return "/paste/s/" + s.ShareSlug
}

// userMessage strips the error's package prefix so the wording reads as a
// sentence to the person who typed the form.
func userMessage(err error) string {
	msg := err.Error()
	for _, prefix := range []string{"paste: invalid snippet: ", "paste: "} {
		if len(msg) > len(prefix) && msg[:len(prefix)] == prefix {
			return upperFirst(msg[len(prefix):])
		}
	}
	return upperFirst(msg)
}

func upperFirst(s string) string {
	if s == "" {
		return s
	}
	b := []rune(s)
	if b[0] >= 'a' && b[0] <= 'z' {
		b[0] -= 32
	}
	return string(b)
}

// listItem is one row on the list page. The preview is computed here rather
// than in the template so the truncation rules are testable.
type listItem struct {
	Snippet  Snippet
	Preview  string
	Language string
}

const previewRunes = 100

// paneMode values for detailView.Mode. modeView is added here; Task 3 adds
// modeNew, Task 4 adds modeEdit.
const modeView = "view"
const modeNew = "new"

// detailView is what the detail pane renders, in any mode. Fields below
// Language belong to the edit/new forms (Tasks 3-4); they are zero-valued in
// view mode.
type detailView struct {
	Mode      string
	Snippet   Snippet
	Highlight template.HTML
	Language  string
	ShareURL  string
	RawURL    string
	CSRFToken string

	TitleValue    string
	LanguageValue string
	BodyValue     string
	Languages     []Language
	Error         string
}

// listFragment is what "list-items" renders. OOB marks a response that rides
// along with a detail-pane update rather than the initial page load.
type listFragment struct {
	Items    []listItem
	ActiveID int64
	OOB      bool
}

// indexView is the whole split-view page: the list plus whichever thing the
// detail pane is showing.
type indexView struct {
	List   listFragment
	Detail detailView
}

// viewDetail builds the detail pane's view-mode data for one snippet.
func (a *App) viewDetail(r *http.Request, s Snippet) detailView {
	return detailView{
		Mode:      modeView,
		Snippet:   s,
		Highlight: Highlight(s.Body, s.Language),
		Language:  LanguageLabel(s.Language),
		RawURL:    "/paste/raw/" + strconv.FormatInt(s.ID, 10),
		ShareURL:  shareURL(s),
		CSRFToken: web.CSRFToken(r.Context()),
	}
}

// newDetail builds the detail pane's data for the new-snippet form.
func (a *App) newDetail(r *http.Request, errMsg, title, language, body string) detailView {
	return detailView{
		Mode:          modeNew,
		TitleValue:    title,
		LanguageValue: language,
		BodyValue:     body,
		Languages:     Languages(),
		Error:         errMsg,
		CSRFToken:     web.CSRFToken(r.Context()),
	}
}

// listItems loads userID's snippets as the list pane's rows.
func (a *App) listItems(ctx context.Context, userID int64) ([]listItem, error) {
	snippets, err := a.store.List(ctx, userID, 0)
	if err != nil {
		return nil, err
	}
	items := make([]listItem, 0, len(snippets))
	for _, s := range snippets {
		items = append(items, listItem{
			Snippet:  s,
			Preview:  preview(s.Body, previewRunes),
			Language: LanguageLabel(s.Language),
		})
	}
	return items, nil
}

// pageTitle is the <title> for a full-page render of the index, which
// depends on what the detail pane is showing.
func pageTitle(d detailView) string {
	switch d.Mode {
	case modeView:
		return d.Snippet.DisplayTitle()
	case modeNew:
		return "New snippet"
	default:
		return "Snippets"
	}
}

// index renders the split-view page: the list on the left, and whichever
// snippet {id} selects (or nothing) on the right. It backs both GET /{$}
// (PathValue("id") is "") and GET /{id}.
func (a *App) index(w http.ResponseWriter, r *http.Request) {
	userID, ok := a.userID(w, r)
	if !ok {
		return
	}

	var detail detailView
	if r.PathValue("id") != "" {
		id, ok := a.snippetID(w, r)
		if !ok {
			return
		}
		s, err := a.store.ByID(r.Context(), userID, id)
		if err != nil {
			a.fail(w, r, err)
			return
		}
		detail = a.viewDetail(r, s)
	}

	a.renderIndex(w, r, userID, http.StatusOK, detail)
}

// renderIndex draws the whole split-view page, or — over HTMX — just the
// detail pane. Every handler that lands the user on some detail pane
// (selecting a snippet, opening the new-snippet form, opening the editor, or
// failing validation on either form) goes through here.
func (a *App) renderIndex(w http.ResponseWriter, r *http.Request, userID int64, status int, detail detailView) {
	items, err := a.listItems(r.Context(), userID)
	if err != nil {
		a.deps.Errors.Internal(w, r, err)
		return
	}
	if web.IsHTMX(r) {
		view := indexView{
			List:   listFragment{Items: items, ActiveID: detail.Snippet.ID, OOB: true},
			Detail: detail,
		}
		if err := a.deps.Render.Fragment(w, status, "paste/index", "detail-with-list", view); err != nil {
			a.deps.Errors.Internal(w, r, err)
		}
		return
	}

	view := indexView{
		List:   listFragment{Items: items, ActiveID: detail.Snippet.ID},
		Detail: detail,
	}

	page := a.deps.Page(r, pageTitle(detail))
	page.Data = view
	a.render(w, r, status, "paste/index", page)
}

// renderDetailWithList replies for anything that can also change a row in
// the list: creating, saving an edit, sharing, unsharing, or deleting a
// snippet (Tasks 3-5). Callers only reach this over HTMX — a non-HTMX
// request redirects instead, before renderDetailWithList is ever called.
func (a *App) renderDetailWithList(w http.ResponseWriter, r *http.Request, userID int64, status int, detail detailView) {
	items, err := a.listItems(r.Context(), userID)
	if err != nil {
		a.deps.Errors.Internal(w, r, err)
		return
	}
	view := indexView{
		List:   listFragment{Items: items, ActiveID: detail.Snippet.ID, OOB: true},
		Detail: detail,
	}
	if err := a.deps.Render.Fragment(w, status, "paste/index", "detail-with-list", view); err != nil {
		a.deps.Errors.Internal(w, r, err)
	}
}

func (a *App) delete(w http.ResponseWriter, r *http.Request) {
	userID, ok := a.userID(w, r)
	if !ok {
		return
	}
	id, ok := a.snippetID(w, r)
	if !ok {
		return
	}

	if err := a.store.Delete(r.Context(), userID, id); err != nil {
		a.fail(w, r, err)
		return
	}
	a.deps.Log.Info("snippet deleted", "app", ID, "user_id", userID, "snippet_id", id)

	// Back to the list: the snippet the user was looking at no longer exists,
	// so there is nowhere else sensible to land.
	http.Redirect(w, r, "/paste/", http.StatusSeeOther)
}

// preview is a single-line excerpt for the list page. Newlines and runs of
// whitespace collapse to single spaces so a row cannot grow to the height of
// the whole snippet.
func preview(body string, limit int) string {
	collapsed := strings.Join(strings.Fields(body), " ")
	runes := []rune(collapsed)
	if len(runes) <= limit {
		return collapsed
	}
	return strings.TrimRight(string(runes[:limit]), " ") + "…"
}

func (a *App) share(w http.ResponseWriter, r *http.Request) {
	userID, ok := a.userID(w, r)
	if !ok {
		return
	}
	id, ok := a.snippetID(w, r)
	if !ok {
		return
	}

	if _, err := a.store.Share(r.Context(), userID, id); err != nil {
		a.fail(w, r, err)
		return
	}
	// The slug itself is a credential, so it is not written to the log.
	a.deps.Log.Info("snippet shared", "app", ID, "user_id", userID, "snippet_id", id)

	http.Redirect(w, r, "/paste/"+strconv.FormatInt(id, 10), http.StatusSeeOther)
}

func (a *App) unshare(w http.ResponseWriter, r *http.Request) {
	userID, ok := a.userID(w, r)
	if !ok {
		return
	}
	id, ok := a.snippetID(w, r)
	if !ok {
		return
	}

	if err := a.store.Unshare(r.Context(), userID, id); err != nil {
		a.fail(w, r, err)
		return
	}
	a.deps.Log.Info("snippet unshared", "app", ID, "user_id", userID, "snippet_id", id)

	http.Redirect(w, r, "/paste/"+strconv.FormatInt(id, 10), http.StatusSeeOther)
}

// viewShared renders a snippet for anyone holding its slug. Note the separate
// template: the owner's view carries delete and unshare controls, and the way
// to guarantee those never reach a public page is for the public template not
// to contain them at all.
func (a *App) viewShared(w http.ResponseWriter, r *http.Request) {
	s, err := a.store.ByShareSlug(r.Context(), r.PathValue("slug"))
	if err != nil {
		a.fail(w, r, err)
		return
	}

	// A share slug is a revocable credential: a cache or a crawler holding
	// onto this page after the link is revoked would defeat the revocation.
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Robots-Tag", "noindex")

	page := a.deps.Page(r, s.DisplayTitle())
	page.Data = viewModel{
		Snippet:   s,
		Highlight: Highlight(s.Body, s.Language),
		Language:  LanguageLabel(s.Language),
		RawURL:    "/paste/s/" + s.ShareSlug + "/raw",
		Owner:     false,
	}
	a.render(w, r, http.StatusOK, "paste/shared", page)
}

// raw serves the owner's own snippet as plain text.
func (a *App) raw(w http.ResponseWriter, r *http.Request) {
	userID, ok := a.userID(w, r)
	if !ok {
		return
	}
	id, ok := a.snippetID(w, r)
	if !ok {
		return
	}

	s, err := a.store.ByID(r.Context(), userID, id)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	a.writeRaw(w, r, s)
}

// rawShared serves a shared snippet as plain text, so it can be piped straight
// into a terminal.
func (a *App) rawShared(w http.ResponseWriter, r *http.Request) {
	s, err := a.store.ByShareSlug(r.Context(), r.PathValue("slug"))
	if err != nil {
		a.fail(w, r, err)
		return
	}
	// See viewShared: a share slug is a revocable credential, so this public
	// endpoint must not be indexed. writeRaw already covers Cache-Control.
	w.Header().Set("X-Robots-Tag", "noindex")
	a.writeRaw(w, r, s)
}

// writeRaw sends a snippet body verbatim.
//
// text/plain plus the platform's global nosniff header means a snippet that
// happens to contain HTML cannot be coaxed into executing as a page on this
// origin. Content-Disposition names the download without forcing one, so a
// browser still shows it and curl still prints it. The filename is built
// from the snippet id rather than its title, which is arbitrary user text
// that would need escaping to go safely into a header value.
func (a *App) writeRaw(w http.ResponseWriter, r *http.Request, s Snippet) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Content-Disposition", `inline; filename="paste-`+strconv.FormatInt(s.ID, 10)+`.txt"`)
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)

	if _, err := io.WriteString(w, s.Body); err != nil {
		a.deps.Log.Error("writing a raw snippet failed", "error", err, "snippet_id", s.ID)
	}
}
