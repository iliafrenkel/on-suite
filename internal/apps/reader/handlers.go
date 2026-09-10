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
	a.renderIndex(w, r, userID, pathContext(r, subID), "")
}

// listContext is which list a render draws: the scope, the subscription that
// scope names, and this viewer's filter. It travels as one value because the
// three are only ever meaningful together — a ScopeFeed with no SubID is not a
// list, it is a bug.
type listContext struct {
	Scope  Scope
	SubID  int64
	Filter Filter
}

// parseScope maps a form or query value onto a Scope, reporting whether it was
// one. Unlike ParseFilter it does not default: a caller needs to know the
// difference between "no scope was sent" (fall back to the path) and "the
// scope sent was All".
func parseScope(raw string) (Scope, bool) {
	switch Scope(raw) {
	case ScopeAll:
		return ScopeAll, true
	case ScopeStarred:
		return ScopeStarred, true
	case ScopeFeed:
		return ScopeFeed, true
	default:
		return ScopeAll, false
	}
}

// pathContext is the GET context: the scope comes from the path, the filter
// from the query string, so a reader URL stays shareable.
//
// The scope keys off the subID the caller resolved rather than PathValue("id")
// directly, because several POST paths carry an id that is not a subscription
// (an item, a folder) and treating those as ScopeFeed would render — and, with
// the ownership gate in renderIndex, 404 — an empty feed list.
func pathContext(r *http.Request, subID int64) listContext {
	scope := ScopeAll
	switch {
	case strings.HasSuffix(r.URL.Path, "/starred"):
		scope = ScopeStarred
	case subID != 0:
		scope = ScopeFeed
	}
	return listContext{
		Scope:  scope,
		SubID:  subID,
		Filter: ParseFilter(r.URL.Query().Get("filter")),
	}
}

// formContext is pathContext overridden by the hidden fields a POST carries.
//
// A POST that redraws the panes has no query string and usually no path
// segment naming the current list, so without this "Mark all read" from
// Starred/All silently re-renders as All/Unread while the address bar (from
// the last hx-push-url) still says /reader/starred?filter=all. Only PostForm
// values are read: a GET has no body, so the path stays authoritative there.
func formContext(r *http.Request, subID int64) listContext {
	out := pathContext(r, subID)
	if s, ok := parseScope(r.PostFormValue("scope")); ok {
		out.Scope = s
		out.SubID = 0
		if s == ScopeFeed {
			if id, err := strconv.ParseInt(r.PostFormValue("sub"), 10, 64); err == nil && id > 0 {
				out.SubID = id
			} else {
				// A feed scope with no subscription is not a list. Fall back
				// rather than 404 on a form that lost a field.
				out.Scope = ScopeAll
			}
		}
	}
	if raw := r.PostFormValue("filter"); raw != "" {
		out.Filter = ParseFilter(raw)
	}
	return out
}

// articleContext is the list an article was opened from.
//
// The article's own path names an item, not a list, so the scope arrives in
// the item link's query string or the star/unread form's hidden fields. It is
// needed because the article response redraws the tree out of band (so unread
// counts stop going stale the moment you read something), and a tree drawn
// without this would move the selection to All under the reader's feet.
func articleContext(r *http.Request) listContext {
	out := listContext{Scope: ScopeAll, Filter: ParseFilter(r.FormValue("filter"))}
	if s, ok := parseScope(r.FormValue("scope")); ok {
		out.Scope = s
	}
	if out.Scope == ScopeFeed {
		if id, err := strconv.ParseInt(r.FormValue("sub"), 10, 64); err == nil && id > 0 {
			out.SubID = id
		} else {
			out.Scope = ScopeAll
		}
	}
	return out
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
func (a *App) renderIndex(w http.ResponseWriter, r *http.Request, userID int64, lc listContext, formErr string) {
	a.renderPanes(w, r, userID, lc, formErr, articleView{})
}

// renderPanes is renderIndex with an optional third pane already loaded. Only
// the JavaScript-less item routes pass an article — opening one, starring it,
// or marking it unread: with htmx they answer with the article fragment, and
// without it they have to answer with a whole page or the browser lands on a
// bare <article> with no shell.
func (a *App) renderPanes(w http.ResponseWriter, r *http.Request, userID int64, lc listContext, formErr string, art articleView) {
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

	sub := findSub(tree, lc.SubID)
	// The ownership gate ItemsForSubscription used to provide. ItemsForScope is
	// the single query path now, and it answers "not your subscription" with an
	// empty list rather than ErrNotFound — which would render a blank page with
	// a 200 for somebody else's feed id, where R1 returned 404. Tree is already
	// scoped to this user, so a subID missing from it is either nonexistent or
	// somebody else's, and both are correctly a 404.
	if lc.Scope == ScopeFeed && sub.ID == 0 {
		a.fail(w, r, ErrNotFound)
		return
	}

	view := indexView{Tree: viewTree(tree, lc.SubID, lc.Scope, counts), Article: art, Error: formErr}

	items, err := a.store.ItemsForScope(ctx, userID, lc.Scope, lc.SubID, lc.Filter, 200)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	listTitleStr := listTitle(lc.Scope, sub)
	view.List = viewList(items, listTitleStr, lc.Scope, lc.SubID, lc.Filter, basePathFor(lc.Scope, lc.SubID))

	// The shell crumb and <title> follow the selected feed, not the generic
	// "All articles" list heading — ScopeAll keeps the app's own name so the
	// tab and breadcrumb do not read "All articles · ON Suite" on the default
	// view.
	pageTitle := "ON Reader"
	if lc.Scope != ScopeAll {
		pageTitle = listTitleStr
	}
	if art.Selected {
		pageTitle = art.Title
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
// It also redraws the sidebar's unread counts out of band. Reading or starring
// an article changes a count, and without this the sidebar keeps the pre-click
// numbers until some unrelated navigation happens to reconcile them. Only the
// count spans are swapped, not the whole tree: the counts are the only thing
// that changed, and replacing the <nav> would discard a half-typed feed URL or
// folder name, re-expand every collapsed folder and reset the pane's scroll —
// on the app's most frequent interaction. They come from the same Counts the
// tree renders from, so there is still one source for every number.
//
// The list pane is deliberately *not* swapped along with it: under the Unread
// filter a fresh list would drop the article out from under the reader the
// instant opening it marked it read. Its row keeps the styling it had until
// the next list render, which is the lesser of the two staleness problems.
func (a *App) renderArticle(w http.ResponseWriter, r *http.Request, userID, itemID int64) {
	ctx := r.Context()

	item, err := a.store.Item(ctx, userID, itemID)
	if err != nil {
		a.fail(w, r, err)
		return
	}
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

	lc := articleContext(r)
	page := a.deps.Page(r, item.Title)

	// Anything that did not come from htmx is a plain browser navigation and
	// gets the whole page back: a form submission from the star or unread
	// forms (which carry method/action for exactly that case), or a click on
	// an item link with JavaScript off. Without the GET half of this, reading
	// an article — the one thing this app is for — was the only action a
	// no-JS reader could not do: the response was a bare <article> with a
	// stray out-of-band checkbox and no shell around either.
	// An htmx request, GET or POST, still gets the one-pane fragment.
	if !web.IsHTMX(r) {
		a.renderPanes(w, r, userID, lc, "", viewArticle(item, page.Shell, lc))
		return
	}

	view := indexView{
		Tree:    viewTree(tree, lc.SubID, lc.Scope, counts),
		List:    listView{Scope: lc.Scope, SubID: lc.SubID, Filter: lc.Filter, Shell: page.Shell},
		Article: viewArticle(item, page.Shell, lc),
		Shell:   page.Shell,
	}
	if err := a.deps.Render.Fragment(w, http.StatusOK, "reader/index", "article-swap", view); err != nil {
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
	// The form carries the list it fired from, so the re-render stays there
	// instead of resetting to All/Unread.
	lc := formContext(r, 0)
	if _, err := a.store.MarkAllRead(r.Context(), userID, lc.Scope, lc.SubID, time.Now().UTC()); err != nil {
		a.fail(w, r, err)
		return
	}
	a.renderIndex(w, r, userID, lc, "")
}

// subscribe adds a feed. A rejected URL re-renders with the message on the
// form rather than replacing the page with an error, because a typo'd URL is
// an ordinary thing to do.
func (a *App) subscribe(w http.ResponseWriter, r *http.Request) {
	userID, ok := a.userID(w, r)
	if !ok {
		return
	}
	lc := formContext(r, 0)
	sub, err := a.store.Subscribe(r.Context(), userID, r.FormValue("url"), folderParam(r))
	if err != nil {
		if errors.Is(err, ErrInvalidURL) {
			// Stay on whatever list the form was submitted from: a typo'd URL
			// should not also throw away the reader's place.
			a.renderIndex(w, r, userID, lc, "That is not a feed address. It needs to start with http:// or https://.")
			return
		}
		a.fail(w, r, err)
		return
	}
	// Adding a feed selects it, which is the one case where the new state wins
	// over the list the form came from.
	lc.Scope, lc.SubID = ScopeFeed, sub.ID
	a.renderIndex(w, r, userID, lc, "")
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
	lc := formContext(r, 0)
	if err := a.store.Unsubscribe(r.Context(), userID, subID); err != nil {
		a.fail(w, r, err)
		return
	}
	// Unsubscribing from the feed you are reading has to fall back to All:
	// the list the form named no longer exists, and rendering it would 404.
	if lc.Scope == ScopeFeed && lc.SubID == subID {
		lc.Scope, lc.SubID = ScopeAll, 0
	}
	a.renderIndex(w, r, userID, lc, "")
}

func (a *App) createFolder(w http.ResponseWriter, r *http.Request) {
	userID, ok := a.userID(w, r)
	if !ok {
		return
	}
	lc := formContext(r, 0)
	if _, err := a.store.CreateFolder(r.Context(), userID, r.FormValue("name")); err != nil {
		if errors.Is(err, ErrInvalid) {
			a.renderIndex(w, r, userID, lc, "A folder needs a name.")
			return
		}
		a.fail(w, r, err)
		return
	}
	a.renderIndex(w, r, userID, lc, "")
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
	lc := formContext(r, 0)
	if err := a.store.DeleteFolder(r.Context(), userID, folderID); err != nil {
		a.fail(w, r, err)
		return
	}
	// Deleting a folder moves its feeds to the root rather than removing them,
	// so a feed scope the form named is still a valid list.
	a.renderIndex(w, r, userID, lc, "")
}

// refresh polls now. It calls the same PollDue the scheduled job calls, which
// is the reason there is one fetch code path rather than two.
func (a *App) refresh(w http.ResponseWriter, r *http.Request) {
	userID, ok := a.userID(w, r)
	if !ok {
		return
	}
	lc := formContext(r, 0)
	if err := a.poller.PollDue(r.Context()); err != nil {
		a.deps.Errors.Internal(w, r, err)
		return
	}
	a.renderIndex(w, r, userID, lc, "")
}
