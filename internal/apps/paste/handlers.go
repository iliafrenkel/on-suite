package paste

import (
	"errors"
	"html/template"
	"net/http"
	"strconv"
	"strings"

	"github.com/iliafrenkel/on-suite/internal/platform/render"
	"github.com/iliafrenkel/on-suite/internal/platform/web"
)

// viewModel is what the view and shared templates render. It carries the
// already-highlighted body so a template never calls into Chroma.
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
	a.renderNew(w, r, http.StatusOK, "", "", "", "")
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
		a.renderNew(w, r, http.StatusBadRequest, "That is not a language I know.", title, language, body)
		return
	}
	if err := Validate(title, body); err != nil {
		a.renderNew(w, r, http.StatusBadRequest, userMessage(err), title, language, body)
		return
	}

	s, err := a.store.Create(r.Context(), userID, title, language, body)
	if err != nil {
		if errors.Is(err, ErrInvalid) {
			a.renderNew(w, r, http.StatusBadRequest, userMessage(err), title, language, body)
			return
		}
		a.deps.Errors.Internal(w, r, err)
		return
	}

	http.Redirect(w, r, "/paste/"+strconv.FormatInt(s.ID, 10), http.StatusSeeOther)
}

func (a *App) view(w http.ResponseWriter, r *http.Request) {
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

	page := a.deps.Page(r, s.DisplayTitle())
	page.Data = viewModel{
		Snippet:   s,
		Highlight: Highlight(s.Body, s.Language),
		Language:  LanguageLabel(s.Language),
		RawURL:    "/paste/raw/" + strconv.FormatInt(s.ID, 10),
		ShareURL:  shareURL(s),
		Owner:     true,
	}
	a.render(w, r, http.StatusOK, "paste/view", page)
}

// renderNew draws the create form, carrying back whatever the user typed so a
// validation failure never loses their snippet.
func (a *App) renderNew(w http.ResponseWriter, r *http.Request, status int, message, title, language, body string) {
	page := a.deps.Page(r, "New snippet")
	page.Data = map[string]any{
		"Error":     message,
		"Title":     title,
		"Language":  language,
		"Body":      body,
		"Languages": Languages(),
	}
	a.render(w, r, status, "paste/new", page)
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

func (a *App) list(w http.ResponseWriter, r *http.Request) {
	userID, ok := a.userID(w, r)
	if !ok {
		return
	}

	snippets, err := a.store.List(r.Context(), userID, 0)
	if err != nil {
		a.deps.Errors.Internal(w, r, err)
		return
	}

	items := make([]listItem, 0, len(snippets))
	for _, s := range snippets {
		items = append(items, listItem{
			Snippet:  s,
			Preview:  preview(s.Body, previewRunes),
			Language: LanguageLabel(s.Language),
		})
	}

	page := a.deps.Page(r, "Snippets")
	page.Data = map[string]any{"Items": items}
	a.render(w, r, http.StatusOK, "paste/list", page)
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

// Stubs replaced in Task 18. They exist so this task compiles and its
// routes are registered in the shape the route map specifies.
func (a *App) raw(w http.ResponseWriter, r *http.Request) {
	a.deps.Errors.Status(w, r, http.StatusNotFound)
}
func (a *App) share(w http.ResponseWriter, r *http.Request) {
	a.deps.Errors.Status(w, r, http.StatusNotFound)
}
func (a *App) unshare(w http.ResponseWriter, r *http.Request) {
	a.deps.Errors.Status(w, r, http.StatusNotFound)
}
func (a *App) viewShared(w http.ResponseWriter, r *http.Request) {
	a.deps.Errors.Status(w, r, http.StatusNotFound)
}
func (a *App) rawShared(w http.ResponseWriter, r *http.Request) {
	a.deps.Errors.Status(w, r, http.StatusNotFound)
}
