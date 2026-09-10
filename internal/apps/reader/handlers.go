package reader

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

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

// scopeOf derives the list scope from the request path.
func scopeOf(r *http.Request) Scope {
	switch {
	case strings.HasSuffix(r.URL.Path, "/starred"):
		return ScopeStarred
	case r.PathValue("id") != "":
		return ScopeFeed
	default:
		return ScopeAll
	}
}

// listTitle names the pane for a scope.
func listTitle(scope Scope, sub Subscription) string {
	switch scope {
	case ScopeStarred:
		return "Starred"
	case ScopeFeed:
		return sub.DisplayName()
	default:
		return "All articles"
	}
}

// basePathFor is the path the filter links point at, so switching filter keeps
// you on the list you are reading rather than dropping you back to All.
func basePathFor(scope Scope, subID int64) string {
	switch scope {
	case ScopeStarred:
		return "/reader/starred"
	case ScopeFeed:
		return "/reader/feed/" + strconv.FormatInt(subID, 10)
	default:
		return "/reader/"
	}
}

// renderIndex draws the whole three-pane page, or — over HTMX — just the
// panes that changed. Every handler that can land the user on a different
// tree/list/article state (selecting a feed, subscribing, unsubscribing,
// managing a folder, or refreshing) goes through here.
func (a *App) renderIndex(w http.ResponseWriter, r *http.Request, userID, subID int64, formErr string) {
	ctx := r.Context()

	tree, err := a.store.Tree(ctx, userID)
	if err != nil {
		a.deps.Errors.Internal(w, r, err)
		return
	}
	counts, err := a.store.UnreadCounts(ctx, userID)
	if err != nil {
		a.deps.Errors.Internal(w, r, err)
		return
	}

	scope := scopeOf(r)
	// A POST that names a subscription (mark-all-read, subscribe) is a feed
	// scope even though its path has no {id}.
	if scope == ScopeAll && subID != 0 {
		scope = ScopeFeed
	}
	filter := ParseFilter(r.URL.Query().Get("filter"))

	view := indexView{Tree: viewTree(tree, subID, scope, counts), Error: formErr}

	items, err := a.store.ItemsForScope(ctx, userID, scope, subID, filter, 200)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	listTitleStr := listTitle(scope, findSub(tree, subID))
	view.List = viewList(items, listTitleStr, scope, subID, filter, basePathFor(scope, subID))

	// The shell crumb and <title> follow the selected feed, not the generic
	// "All articles" list heading — ScopeAll keeps the app's own name so the
	// tab and breadcrumb do not read "All articles · ON Suite" on the default
	// view.
	pageTitle := "ON Reader"
	if scope != ScopeAll {
		pageTitle = listTitleStr
	}
	page := a.deps.Page(r, pageTitle)
	view.List.Shell = page.Shell
	if web.IsHTMX(r) && !web.IsHTMXHistoryRestore(r) {
		view.Title, view.Shell = page.Title, page.Shell
		// Always 200 for a fragment: htmx's default responseHandling only
		// swaps 2xx/3xx, so a 400 would silently discard the re-rendered form
		// and its error message.
		if err := a.deps.Render.Fragment(w, http.StatusOK, "reader/index", "panes-oob", view); err != nil {
			a.deps.Errors.Internal(w, r, err)
		}
		return
	}

	view.Shell = page.Shell
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

// renderArticle draws the article pane. article, setRead and toggleStar all go
// through here: three copies of one fragment render is exactly the drift this
// plan's single-query-path rule exists to prevent.
//
// It renders the "article-oob" block, not "article", so the pane-state
// checkbox rides along out of band. That checkbox is what makes the narrow
// layout drill down into the article; without it, opening an article on a
// phone swaps a pane the viewport is not showing. (R1 had an article OOB swap
// reverted as unrequested — this one is requested, deliberately, and
// TestArticleResponseCarriesTheOOBPaneState pins it.)
func (a *App) renderArticle(w http.ResponseWriter, r *http.Request, userID, itemID int64) {
	item, err := a.store.Item(r.Context(), userID, itemID)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	page := a.deps.Page(r, item.Title)
	if err := a.deps.Render.Fragment(w, http.StatusOK, "reader/index", "article-oob",
		viewArticle(item, page.Shell)); err != nil {
		a.deps.Errors.Internal(w, r, err)
	}
}

// article opens an item and marks it read.
//
// Marking read on open, rather than on scroll or only by hand, is the design
// decision from section 7 of the spec. The pane offers Mark unread to undo it,
// so the automatic write is always reversible in one click.
func (a *App) article(w http.ResponseWriter, r *http.Request) {
	userID, ok := a.userID(w, r)
	if !ok {
		return
	}
	itemID, ok := a.pathID(w, r)
	if !ok {
		return
	}
	if err := a.store.SetRead(r.Context(), userID, itemID, true, time.Now().UTC()); err != nil {
		a.fail(w, r, err)
		return
	}
	a.renderArticle(w, r, userID, itemID)
}

// setRead serves both /read and /unread; the path suffix decides which.
func (a *App) setRead(w http.ResponseWriter, r *http.Request) {
	userID, ok := a.userID(w, r)
	if !ok {
		return
	}
	itemID, ok := a.pathID(w, r)
	if !ok {
		return
	}
	read := strings.HasSuffix(r.URL.Path, "/read")
	if err := a.store.SetRead(r.Context(), userID, itemID, read, time.Now().UTC()); err != nil {
		a.fail(w, r, err)
		return
	}
	a.renderArticle(w, r, userID, itemID)
}

func (a *App) toggleStar(w http.ResponseWriter, r *http.Request) {
	userID, ok := a.userID(w, r)
	if !ok {
		return
	}
	itemID, ok := a.pathID(w, r)
	if !ok {
		return
	}
	_, starred, err := a.store.ItemState(r.Context(), userID, itemID)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	// ItemState reports false for an item that does not exist or is not
	// visible, so SetStarred's own visibility check is what turns that into a
	// 404 rather than silently starring nothing.
	if err := a.store.SetStarred(r.Context(), userID, itemID, !starred, time.Now().UTC()); err != nil {
		a.fail(w, r, err)
		return
	}
	a.renderArticle(w, r, userID, itemID)
}

func (a *App) markAllRead(w http.ResponseWriter, r *http.Request) {
	userID, ok := a.userID(w, r)
	if !ok {
		return
	}
	scope := Scope(r.FormValue("scope"))
	if scope == "" {
		scope = ScopeAll
	}
	subID, _ := strconv.ParseInt(r.FormValue("sub"), 10, 64)
	if _, err := a.store.MarkAllRead(r.Context(), userID, scope, subID, time.Now().UTC()); err != nil {
		a.fail(w, r, err)
		return
	}
	a.renderIndex(w, r, userID, subID, "")
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
