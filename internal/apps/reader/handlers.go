package reader

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/iliafrenkel/on-suite/internal/platform/web"
)

// userID is the signed-in user. Every route is registered with Handle, so a
// missing user is a programming error rather than a bad request.
func (a *App) userID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	u, ok := web.UserFrom(r.Context())
	if !ok {
		a.deps.Errors.Status(w, r, http.StatusUnauthorized)
		return 0, false
	}
	return u.ID, true
}

func (a *App) pathID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		a.deps.Errors.Status(w, r, http.StatusNotFound)
		return 0, false
	}
	return id, true
}

// fail maps a store error onto a response. ErrNotFound is a 404 whether the row
// is missing or simply somebody else's, so the two are indistinguishable from
// outside — a 403 would confirm the id exists.
func (a *App) fail(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, ErrNotFound):
		a.deps.Errors.Status(w, r, http.StatusNotFound)
	case errors.Is(err, ErrInvalidURL), errors.Is(err, ErrInvalid):
		a.deps.Errors.Status(w, r, http.StatusBadRequest)
	default:
		a.deps.Errors.Internal(w, r, err)
	}
}

// index draws the whole page, or over HTMX just the panes that changed.
func (a *App) index(w http.ResponseWriter, r *http.Request) {
	userID, ok := a.userID(w, r)
	if !ok {
		return
	}
	var subID int64
	if r.PathValue("id") != "" {
		if subID, ok = a.pathID(w, r); !ok {
			return
		}
	}
	a.renderIndex(w, r, userID, subID, "")
}

// pageTitle is the <title> and shell-crumb-tail segment for a render of the
// index: the selected subscription's name when one is selected, "ON Reader"
// otherwise. Mirrors paste's own pageTitle, which does the same thing keyed
// off its detail pane's mode instead of a selected id.
func pageTitle(sub Subscription, subID int64) string {
	if subID == 0 {
		return "ON Reader"
	}
	if name := sub.DisplayName(); name != "" {
		return name
	}
	return "ON Reader"
}

// renderIndex draws the whole three-pane page, or — over HTMX — just the
// panes that changed. Every handler that can land the user on a different
// tree/list/article state (selecting a feed, subscribing, unsubscribing,
// managing a folder, or refreshing) goes through here.
func (a *App) renderIndex(w http.ResponseWriter, r *http.Request, userID, subID int64, formErr string) {
	tree, err := a.store.Tree(r.Context(), userID)
	if err != nil {
		a.deps.Errors.Internal(w, r, err)
		return
	}

	sub := findSub(tree, subID)
	view := indexView{Tree: viewTree(tree, subID), Error: formErr}
	if subID != 0 {
		items, err := a.store.ItemsForSubscription(r.Context(), userID, subID, 200)
		if err != nil {
			a.fail(w, r, err)
			return
		}
		view.List = viewList(items, sub, 0)
	}

	title := pageTitle(sub, subID)

	if web.IsHTMX(r) && !web.IsHTMXHistoryRestore(r) {
		page := a.deps.Page(r, title)
		view.Title, view.Shell = page.Title, page.Shell
		// Always 200 for a fragment: htmx's default responseHandling only
		// swaps 2xx/3xx, so a 400 would silently discard the re-rendered form
		// and its error message. The status code carries no swap-relevant
		// meaning for a partial the way it does for a navigation.
		//
		// "panes-oob" (not "panes") is the block rendered here: it wraps the
		// panes with the <title> and hx-swap-oob shell-crumb-tail span that
		// keep the page chrome in sync with what the panes now show — see
		// the doc comment on "panes-oob" in panes.partial.html, and issue
		// #205 / commit c96a03f, which is the regression this exists to
		// prevent from coming back.
		if err := a.deps.Render.Fragment(w, http.StatusOK, "reader/index", "panes-oob", view); err != nil {
			a.deps.Errors.Internal(w, r, err)
		}
		return
	}

	page := a.deps.Page(r, title)
	page.Data = view
	if err := a.deps.Render.Page(w, http.StatusOK, "reader/index", page); err != nil {
		a.deps.Errors.Internal(w, r, err)
	}
}

func findSub(t Tree, id int64) Subscription {
	if id == 0 {
		return Subscription{}
	}
	for _, s := range t.Root {
		if s.ID == id {
			return s
		}
	}
	for _, f := range t.Folders {
		for _, s := range f.Subs {
			if s.ID == id {
				return s
			}
		}
	}
	return Subscription{}
}

// article renders one item into the right-hand pane.
func (a *App) article(w http.ResponseWriter, r *http.Request) {
	userID, ok := a.userID(w, r)
	if !ok {
		return
	}
	itemID, ok := a.pathID(w, r)
	if !ok {
		return
	}
	item, err := a.store.Item(r.Context(), userID, itemID)
	if err != nil {
		a.fail(w, r, err)
		return
	}

	view := viewArticle(item)
	page := a.deps.Page(r, view.Title)
	// "article-oob" (not "article") is the block rendered here: it wraps the
	// article pane with the <title> and hx-swap-oob shell-crumb-tail span, the
	// same reason renderIndex renders "panes-oob" rather than "panes" — see
	// its doc comment and issue #205 / commit c96a03f.
	frag := articleFragment{Article: view, Title: page.Title, Shell: page.Shell}
	if err := a.deps.Render.Fragment(w, http.StatusOK, "reader/index", "article-oob", frag); err != nil {
		a.deps.Errors.Internal(w, r, err)
	}
}

// subscribe adds a feed. A rejected URL re-renders with the message on the
// form rather than replacing the page with an error, because a typo'd URL is
// an ordinary thing to do.
func (a *App) subscribe(w http.ResponseWriter, r *http.Request) {
	userID, ok := a.userID(w, r)
	if !ok {
		return
	}
	sub, err := a.store.Subscribe(r.Context(), userID, r.FormValue("url"), folderParam(r))
	if err != nil {
		if errors.Is(err, ErrInvalidURL) {
			a.renderIndex(w, r, userID, 0, "That is not a feed address. It needs to start with http:// or https://.")
			return
		}
		a.fail(w, r, err)
		return
	}
	a.renderIndex(w, r, userID, sub.ID, "")
}

// folderParam reads an optional folder id from the form. Absent or unparseable
// means the root of the tree, which is the harmless default.
func folderParam(r *http.Request) *int64 {
	raw := r.FormValue("folder_id")
	if raw == "" {
		return nil
	}
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || id <= 0 {
		return nil
	}
	return &id
}

func (a *App) unsubscribe(w http.ResponseWriter, r *http.Request) {
	userID, ok := a.userID(w, r)
	if !ok {
		return
	}
	subID, ok := a.pathID(w, r)
	if !ok {
		return
	}
	if err := a.store.Unsubscribe(r.Context(), userID, subID); err != nil {
		a.fail(w, r, err)
		return
	}
	a.renderIndex(w, r, userID, 0, "")
}

func (a *App) createFolder(w http.ResponseWriter, r *http.Request) {
	userID, ok := a.userID(w, r)
	if !ok {
		return
	}
	if _, err := a.store.CreateFolder(r.Context(), userID, r.FormValue("name")); err != nil {
		if errors.Is(err, ErrInvalid) {
			a.renderIndex(w, r, userID, 0, "A folder needs a name.")
			return
		}
		a.fail(w, r, err)
		return
	}
	a.renderIndex(w, r, userID, 0, "")
}

func (a *App) deleteFolder(w http.ResponseWriter, r *http.Request) {
	userID, ok := a.userID(w, r)
	if !ok {
		return
	}
	folderID, ok := a.pathID(w, r)
	if !ok {
		return
	}
	if err := a.store.DeleteFolder(r.Context(), userID, folderID); err != nil {
		a.fail(w, r, err)
		return
	}
	a.renderIndex(w, r, userID, 0, "")
}

// refresh polls now. It calls the same PollDue the scheduled job calls, which
// is the reason there is one fetch code path rather than two.
func (a *App) refresh(w http.ResponseWriter, r *http.Request) {
	userID, ok := a.userID(w, r)
	if !ok {
		return
	}
	if err := a.poller.PollDue(r.Context()); err != nil {
		a.deps.Errors.Internal(w, r, err)
		return
	}
	a.renderIndex(w, r, userID, 0, "")
}
