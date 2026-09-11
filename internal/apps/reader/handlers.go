package reader

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
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
	a.renderIndexWith(w, r, userID, lc, formErr, "", nil)
}

// renderIndexWithNotice is renderIndex with a neutral message attached.
//
// It sets the field after building the view rather than widening renderIndex's
// signature, so the eight existing call sites stay as they are.
func (a *App) renderIndexWithNotice(w http.ResponseWriter, r *http.Request, userID int64, lc listContext, notice string) {
	a.renderIndexWith(w, r, userID, lc, "", notice, nil)
}

// renderIndexWith is renderIndex with its full parameter set: a form error, a
// neutral notice, and the discovery chooser's candidates (Task 4). Task 2 only
// ever passes "" and nil for the last two from renderIndex itself; the OPML
// import handler is the first caller to use notice for real.
func (a *App) renderIndexWith(w http.ResponseWriter, r *http.Request, userID int64, lc listContext, formErr, notice string, candidates []FeedCandidate) {
	a.renderPanes(w, r, userID, lc, formErr, notice, candidates, articleView{})
}

// renderPanes is renderIndex with an optional third pane already loaded. Only
// the JavaScript-less item routes pass an article — opening one, starring it,
// or marking it unread: with htmx they answer with the article fragment, and
// without it they have to answer with a whole page or the browser lands on a
// bare <article> with no shell.
func (a *App) renderPanes(w http.ResponseWriter, r *http.Request, userID int64, lc listContext, formErr, notice string, candidates []FeedCandidate, art articleView) {
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

	view := indexView{
		Tree:       viewTree(tree, lc.SubID, lc.Scope, counts),
		Article:    art,
		Error:      formErr,
		Notice:     notice,
		Candidates: candidates,
	}

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

	// The full article wins by default once it exists; ?view=feed is the way
	// back, because extraction sometimes does worse than the publisher's own
	// summary.
	showFull := r.URL.Query().Get("view") != "feed"

	// Anything that did not come from htmx is a plain browser navigation and
	// gets the whole page back: a form submission from the star or unread
	// forms (which carry method/action for exactly that case), or a click on
	// an item link with JavaScript off. Without the GET half of this, reading
	// an article — the one thing this app is for — was the only action a
	// no-JS reader could not do: the response was a bare <article> with a
	// stray out-of-band checkbox and no shell around either.
	// An htmx request, GET or POST, still gets the one-pane fragment.
	if !web.IsHTMX(r) {
		a.renderPanes(w, r, userID, lc, "", "", nil, viewArticle(item, page.Shell, lc, showFull))
		return
	}

	view := indexView{
		Tree:    viewTree(tree, lc.SubID, lc.Scope, counts),
		List:    listView{Scope: lc.Scope, SubID: lc.SubID, Filter: lc.Filter, Shell: page.Shell},
		Article: viewArticle(item, page.Shell, lc, showFull),
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

	raw := strings.TrimSpace(r.FormValue("url"))
	if _, err := NormalizeFeedURL(raw); err != nil {
		a.renderIndex(w, r, userID, lc, "That is not a web address. It needs to start with http:// or https://.")
		return
	}

	feedURL, candidates, err := a.resolveFeedURL(r.Context(), raw)
	switch {
	case errors.Is(err, ErrNoFeedFound):
		a.renderIndex(w, r, userID, lc, "No feed found at that address.")
		return
	case err != nil:
		// A fetch that failed — offline, refused, a private address — is
		// ordinary input, not a server error.
		a.deps.Log.Info("reader feed discovery failed", "url", raw, "error", err)
		a.renderIndex(w, r, userID, lc, "That address could not be reached.")
		return
	case len(candidates) > 0:
		a.renderChooser(w, r, userID, lc, raw, candidates)
		return
	}

	sub, err := a.store.Subscribe(r.Context(), userID, feedURL, folderParam(r))
	if err != nil {
		if errors.Is(err, ErrInvalidURL) {
			a.renderIndex(w, r, userID, lc, "That is not a feed address.")
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

// resolveFeedURL turns whatever someone pasted into a feed URL.
//
// Order matters: the pasted URL is fetched once and tried as a feed first, so
// pasting an actual feed address costs exactly one request and never triggers
// discovery. Only when that fails is the response treated as a web page.
//
// Every fetch goes through a.client, so the SSRF dialer guard, redirect cap
// and size caps apply to discovery exactly as they do to polling — this is a
// server fetching a URL a user typed, which is the case that guard exists for.
func (a *App) resolveFeedURL(ctx context.Context, raw string) (string, []FeedCandidate, error) {
	res, err := a.client.Get(ctx, raw, GetOptions{MaxBytes: MaxFeedBytes})
	if err != nil {
		return "", nil, err
	}

	// Already a feed? Then we are done, and the poller will refetch it on its
	// own schedule.
	if _, err := ParseFeed(res.Body, res.FinalURL); err == nil {
		return res.FinalURL, nil, nil
	}

	candidates := FeedsInPage(res.Body, res.FinalURL)
	if len(candidates) == 1 {
		return candidates[0].URL, nil, nil
	}
	if len(candidates) > 1 {
		// Ranked best-first, but let the person choose: a site with several
		// feeds usually means several topics, and guessing wrong is worse than
		// asking.
		return "", candidates, nil
	}

	if found, ok := a.probeForFeed(ctx, res.FinalURL); ok {
		return found, nil, nil
	}
	return "", nil, ErrNoFeedFound
}

// probeForFeed tries the handful of conventional paths, in order, stopping at
// the first that parses as a feed.
//
// Sequential rather than concurrent on purpose: this sends requests to
// somebody's server on the strength of a guess, and firing five at once to
// save a second is not a trade worth making against a stranger's bandwidth.
func (a *App) probeForFeed(ctx context.Context, pageURL string) (string, bool) {
	base, err := url.Parse(pageURL)
	if err != nil {
		return "", false
	}
	for _, p := range ProbePaths {
		ref, err := url.Parse(p)
		if err != nil {
			continue
		}
		candidate := base.ResolveReference(ref).String()
		res, err := a.client.Get(ctx, candidate, GetOptions{MaxBytes: MaxFeedBytes})
		if err != nil {
			continue
		}
		if _, err := ParseFeed(res.Body, res.FinalURL); err == nil {
			return res.FinalURL, true
		}
	}
	return "", false
}

// renderChooser redraws the page with the feeds a pasted page advertised.
//
// Each candidate posts back to /reader/subscribe with a plain feed URL, so the
// second pass through resolveFeedURL parses it as a feed on the first request
// and no chooser reappears — the branch is naturally non-recursive.
func (a *App) renderChooser(w http.ResponseWriter, r *http.Request, userID int64, lc listContext, pasted string, candidates []FeedCandidate) {
	a.deps.Log.Info("reader offering feed candidates", "url", pasted, "count", len(candidates))
	a.renderIndexWith(w, r, userID, lc, "", "", candidates)
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

// exportOPML sends this user's subscriptions as a file.
func (a *App) exportOPML(w http.ResponseWriter, r *http.Request) {
	userID, ok := a.userID(w, r)
	if !ok {
		return
	}
	tree, err := a.store.Tree(r.Context(), userID)
	if err != nil {
		a.deps.Errors.Internal(w, r, err)
		return
	}
	doc, err := BuildOPML(tree, "ON Reader subscriptions")
	if err != nil {
		a.deps.Errors.Internal(w, r, err)
		return
	}

	w.Header().Set("Content-Type", "text/x-opml+xml; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="reader-subscriptions.opml"`)
	if _, err := w.Write(doc); err != nil {
		a.deps.Log.Info("reader writing an OPML export failed", "error", err)
	}
}

// importOPML adds every subscription in an uploaded file.
//
// No http.MaxBytesReader here: CSRF.Middleware has already called
// ParseMultipartForm on this request looking for the token, so r.Body is
// consumed by the time this runs — wrapping it now would protect nothing.
// MaxOPMLBytes is enforced against the upload's own reported size instead,
// exactly as ON Notes' import does.
func (a *App) importOPML(w http.ResponseWriter, r *http.Request) {
	userID, ok := a.userID(w, r)
	if !ok {
		return
	}
	lc := formContext(r, 0)

	if err := r.ParseMultipartForm(web.DefaultMaxBodyBytes); err != nil {
		a.renderIndex(w, r, userID, lc, "That upload could not be read.")
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		a.renderIndex(w, r, userID, lc, "Choose an OPML file to import.")
		return
	}
	defer func() { _ = file.Close() }()

	if header.Size > MaxOPMLBytes {
		a.renderIndex(w, r, userID, lc, "That file is too large to be a subscription list.")
		return
	}
	data, err := io.ReadAll(file)
	if err != nil {
		a.renderIndex(w, r, userID, lc, "That upload could not be read.")
		return
	}

	entries, err := ParseOPML(data)
	if err != nil {
		// A wrong file is ordinary user input, not a server error.
		a.renderIndex(w, r, userID, lc, "That does not look like an OPML file.")
		return
	}
	res, err := a.store.ImportOPML(r.Context(), userID, entries)
	if err != nil {
		a.deps.Errors.Internal(w, r, err)
		return
	}

	a.deps.Log.Info("reader imported OPML",
		"added", res.Added, "existing", res.Existing, "failed", res.Failed)
	a.renderIndexWithNotice(w, r, userID, lc, importMessage(res))
}

// importMessage phrases a result for a person: what happened, in one line.
func importMessage(res ImportResult) string {
	parts := []string{fmt.Sprintf("Added %d", res.Added)}
	if res.Existing > 0 {
		parts = append(parts, fmt.Sprintf("%d already subscribed", res.Existing))
	}
	if res.Failed > 0 {
		parts = append(parts, fmt.Sprintf("%d could not be added", res.Failed))
	}
	return strings.Join(parts, ", ") + "."
}

// fetchFull retrieves an article's own page and extracts its body.
//
// A page that will not extract — a paywall, a listing, a JavaScript-rendered
// shell — is an ordinary outcome rather than an error: the failure is recorded
// against the item and the pane keeps showing the feed body with an
// explanation. Only a genuine server-side problem produces a 5xx.
func (a *App) fetchFull(w http.ResponseWriter, r *http.Request) {
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
	if item.URL == "" {
		a.recordFullFailure(r, userID, itemID, "this article has no link to fetch")
		a.renderArticle(w, r, userID, itemID)
		return
	}

	if err := a.extractInto(r, userID, item); err != nil {
		a.deps.Log.Info("reader full-article fetch failed", "url", item.URL, "error", err)
		a.recordFullFailure(r, userID, itemID, fullFailureMessage(err))
	}
	a.renderArticle(w, r, userID, itemID)
}

// recordFullFailure stores a failure, logging rather than surfacing an error if
// even that fails — the user is about to get a rendered pane either way.
func (a *App) recordFullFailure(r *http.Request, userID, itemID int64, msg string) {
	if err := a.store.SaveFullArticleFailure(r.Context(), userID, itemID, msg, time.Now().UTC()); err != nil {
		a.deps.Log.Error("reader recording a full-article failure failed", "error", err)
	}
}

// fullFailureMessage turns an error into something worth showing a person.
func fullFailureMessage(err error) string {
	if errors.Is(err, ErrNotExtractable) {
		return "could not find an article in that page — it may be a paywall, or built by JavaScript"
	}
	return "could not fetch the page"
}

// extractInto fetches, extracts and stores. It is separate from the handler so
// the handler reads as the decision tree it is.
func (a *App) extractInto(r *http.Request, userID int64, item Item) error {
	select {
	case a.fullSem <- struct{}{}:
		defer func() { <-a.fullSem }()
	case <-r.Context().Done():
		return r.Context().Err()
	}

	res, err := a.client.Get(r.Context(), item.URL, GetOptions{
		MaxBytes: MaxArticleBytes,
		Accept:   "text/html, application/xhtml+xml;q=0.9, */*;q=0.5",
	})
	if err != nil {
		return err
	}
	// Only HTML extracts. A PDF or an image behind an article link is a
	// perfectly ordinary thing to find and not something to hand to a parser.
	if ct := res.ContentType; ct != "" && !strings.Contains(ct, "html") {
		return fmt.Errorf("%w: content type %s", ErrNotExtractable, ct)
	}

	ex, err := ExtractArticle(res.Body, res.FinalURL)
	if err != nil {
		return err
	}
	a.deps.Log.Info("reader extracted a full article",
		"url", item.URL, "chars", ex.TextLength, "images", len(ex.Images))
	return a.store.SaveFullArticle(r.Context(), userID, item.ID, ex, time.Now().UTC())
}
