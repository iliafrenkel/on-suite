package notes

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/iliafrenkel/on-suite/internal/platform/render"
	"github.com/iliafrenkel/on-suite/internal/platform/web"
)

// userID is the signed-in user's id. Every route is registered with Handle,
// so a missing user is a programming error rather than a bad request.
func (a *App) userID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	u, ok := web.UserFrom(r.Context())
	if !ok {
		a.deps.Errors.Status(w, r, http.StatusUnauthorized)
		return 0, false
	}
	return u.ID, true
}

func (a *App) render(w http.ResponseWriter, r *http.Request, status int, name string, page render.Page) {
	if err := a.deps.Render.Page(w, status, name, page); err != nil {
		a.deps.Errors.Internal(w, r, err)
	}
}

// fail maps a store error onto a response.
//
// ErrNotFound is a 404 whether the bullet is missing or simply someone
// else's: a 403 would confirm that it exists. ErrCycle and ErrTooDeep are
// requests that are well-formed but not satisfiable against this tree, which
// is what 400 means here.
func (a *App) fail(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, ErrNotFound):
		a.deps.Errors.Status(w, r, http.StatusNotFound)
	case errors.Is(err, ErrInvalid), errors.Is(err, ErrCycle), errors.Is(err, ErrTooDeep):
		a.deps.Errors.Status(w, r, http.StatusBadRequest)
	default:
		a.deps.Errors.Internal(w, r, err)
	}
}

// outline renders the top-level outline.
func (a *App) outline(w http.ResponseWriter, r *http.Request) {
	a.renderOutline(w, r, RootID)
}

// nodeID parses the {id} wildcard. A path that is not a positive integer is a
// 404 rather than a 400: from outside, "there is nothing at that address" is
// the same answer either way, and it is the true one.
func (a *App) nodeID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		a.deps.Errors.Status(w, r, http.StatusNotFound)
		return 0, false
	}
	return id, true
}

// outlineZoomed renders the outline rooted at one node — spec §6: zoom is the
// URL, and the only difference from the top level is which node the recursive
// query starts at.
func (a *App) outlineZoomed(w http.ResponseWriter, r *http.Request) {
	id, ok := a.nodeID(w, r)
	if !ok {
		return
	}
	a.renderOutline(w, r, id)
}

// renderOutline draws the outline rooted at rootID: the breadcrumb, the
// visible rows, and nothing else. RootID means the top level.
//
// Every query here runs on the pool, outside any transaction, which is the
// only safe place for them — see the warning on mutate.
func (a *App) renderOutline(w http.ResponseWriter, r *http.Request, rootID int64) {
	userID, ok := a.userID(w, r)
	if !ok {
		return
	}

	showCompleted := showCompletedFrom(r)
	view := outlineView{CSRFToken: web.CSRFToken(r.Context()), ShowCompleted: showCompleted}

	// An empty title leaves the shell's breadcrumb reading "Home / ON Notes",
	// which is what the top level is. A zoomed outline names its root.
	title := ""
	if rootID != RootID {
		root, err := a.store.ByID(r.Context(), userID, rootID)
		if err != nil {
			a.fail(w, r, err)
			return
		}
		crumbs, err := a.store.Ancestors(r.Context(), userID, rootID)
		if err != nil {
			a.deps.Errors.Internal(w, r, err)
			return
		}
		view.Root, view.Zoomed, view.Crumbs = root, true, crumbs
		title = root.DisplayTitle()
	}

	flat, err := a.store.Outline(r.Context(), userID, rootID, showCompleted)
	if err != nil {
		a.deps.Errors.Internal(w, r, err)
		return
	}
	visible := hideDone(flat, showCompleted)
	view.HiddenCount = len(flat) - len(visible)
	view.Rows = nest(visible, rootID, view.CSRFToken, time.Now().Format("2006-01-02"))

	page := a.deps.Page(r, title)
	page.Data = view
	a.render(w, r, http.StatusOK, "notes/outline", page)
}

// renderOutlineFragment re-renders #outline's own content for an HTMX swap.
// It shares renderOutline's query but not its shell: a structural response
// never changes which node the page is zoomed to, so the breadcrumb and
// heading stay exactly as the browser already has them, and there is no
// need to look the root node up — Root.ID is all outline-body reads, and
// the caller already has it as a plain int64.
//
// The response also carries the toolbar's show-completed toggle out of band:
// that button lives outside #outline, so the swap cannot reach it, and after
// a prefs toggle its label and value would otherwise stay stale.
func (a *App) renderOutlineFragment(w http.ResponseWriter, r *http.Request, userID, rootID int64, showCompleted bool) {
	flat, err := a.store.Outline(r.Context(), userID, rootID, showCompleted)
	if err != nil {
		a.deps.Errors.Internal(w, r, err)
		return
	}
	visible := hideDone(flat, showCompleted)
	view := outlineView{
		CSRFToken:     web.CSRFToken(r.Context()),
		Root:          Node{ID: rootID},
		ShowCompleted: showCompleted,
		HiddenCount:   len(flat) - len(visible),
		OOB:           true,
	}
	view.Rows = nest(visible, rootID, view.CSRFToken, time.Now().Format("2006-01-02"))
	if err := a.deps.Render.Fragment(w, http.StatusOK, "notes/outline", "outline-swap", view); err != nil {
		a.deps.Errors.Internal(w, r, err)
	}
}

// outlinePath is the zoom URL for a root, and the only place in this package
// where a path is built from an id. It is always "/notes/" followed by
// decimal digits, so a redirect through it can never leave the site.
func outlinePath(root int64) string {
	if root == RootID {
		return "/notes/"
	}
	return "/notes/" + strconv.FormatInt(root, 10)
}

// mutation is a structural request already parsed: the fields spec §7 defines,
// plus the ids the route supplies.
type mutation struct {
	UserID int64
	// NodeID is the {id} in the path — the bullet the operation acts on. It
	// is RootID on POST /notes/new, which has no {id}.
	NodeID int64
	// FocusID is the bullet the caret was in, or RootID for "none". It is
	// deliberately independent of NodeID: in this chunk they always coincide,
	// because each row is its own form, but from N3 a click on one bullet's
	// control while the caret is in another makes them differ.
	FocusID int64
	// Root is the zoom the request was issued from: the redirect target, and
	// also — see create — the parent a focus-less new bullet is appended
	// under.
	Root int64
}

// mutate is spec §7's single write.
//
// It saves the focused bullet's text and performs the structural operation
// inside one transaction, so the two cannot interleave with anything and
// cannot half-apply. Without it there are two writes, and a user who types and
// then presses Tab loses whatever landed after the last save.
//
// op must reach the database only through the *Ops it is handed. The platform
// opens SQLite with SetMaxOpenConns(1): a closure that calls a *Store method —
// a.store.Outline, a.store.Indent, anything — waits for the connection its own
// transaction is holding, and waits for ever. There is no write timeout in the
// server, so that is a frozen process rather than a failed request. Rendering
// and redirecting therefore happen out here, after Do has returned.
func (a *App) mutate(w http.ResponseWriter, r *http.Request, op func(context.Context, *Ops, mutation) error) {
	userID, ok := a.userID(w, r)
	if !ok {
		return
	}

	// Everything the request carries is read up front, before the transaction
	// opens, so nothing inside it depends on parsing that might fail.
	root, ok := formID(r, "root")
	if !ok {
		a.deps.Errors.Status(w, r, http.StatusBadRequest)
		return
	}
	focusID, ok := formID(r, "focus_id")
	if !ok {
		a.deps.Errors.Status(w, r, http.StatusBadRequest)
		return
	}
	title, note := r.PostFormValue("title"), r.PostFormValue("note")

	// A path with no {id} — POST /notes/new — leaves NodeID at RootID. Every
	// other route's pattern guarantees the wildcard is there, and a value that
	// is present but not a positive integer is a 404, as it is for a GET.
	// RootID is an untyped constant, so the type is written out: := would
	// infer int here, and mutation.NodeID is an int64.
	var nodeID int64 = RootID
	if raw := r.PathValue("id"); raw != "" {
		id, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || id <= 0 {
			a.deps.Errors.Status(w, r, http.StatusNotFound)
			return
		}
		nodeID = id
	}

	m := mutation{UserID: userID, NodeID: nodeID, FocusID: focusID, Root: root}
	ctx := r.Context()

	err := a.store.Do(ctx, func(o *Ops) error {
		if m.FocusID != RootID {
			if err := o.SetText(ctx, m.UserID, m.FocusID, title, note); err != nil {
				return err
			}
		}
		return op(ctx, o, m)
	})
	if err != nil {
		a.fail(w, r, err)
		return
	}

	if web.IsHTMX(r) {
		a.renderOutlineFragment(w, r, userID, root, showCompletedFrom(r))
		return
	}
	http.Redirect(w, r, outlinePath(root), http.StatusSeeOther)
}

// formID reads a form field as a node id.
//
// An absent or empty field is RootID, which every caller reads as "none", and
// so is a literal RootID: the row form renders its hidden root field from the
// zoom it was drawn at, which is RootID at the top level, so "0" is the
// commonest value this ever sees and rejecting it would 400 every mutation
// made from the top-level outline. A field that is present but is neither of
// those nor a positive integer is a malformed request, reported rather than
// silently treated as absent: for focus_id, treating it as absent would drop
// the user's text on the floor without saying so.
func formID(r *http.Request, name string) (int64, bool) {
	raw := r.PostFormValue(name)
	if raw == "" {
		return RootID, true
	}
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || id < 0 {
		return RootID, false
	}
	return id, true
}

// create adds a bullet.
//
// It is Enter, written before the keyboard that will press it: title is what
// stays on the focused bullet, new_title is what moves to the new one, and
// both happen in the one transaction, so a split can never lose half of a
// line. N2's "+" button is the same request with new_title empty.
//
// Placement follows from the focus. With one, the bullet becomes the focused
// one's next sibling — Enter puts a line below the line you are on, not at the
// bottom of the document. Without one, which is the empty outline's form, it
// is appended to the zoom root the request came from.
func (a *App) create(w http.ResponseWriter, r *http.Request) {
	newTitle := r.PostFormValue("new_title")

	a.mutate(w, r, func(ctx context.Context, o *Ops, m mutation) error {
		parentID, afterPos := m.Root, maxPosition

		if m.FocusID != RootID {
			// Read through the transaction, so the sibling list this lands in
			// is the one mutate's text update has already touched.
			focus, err := o.ByID(ctx, m.UserID, m.FocusID)
			if err != nil {
				return err
			}
			parentID, afterPos = focus.ParentID, focus.Position
		}

		_, err := o.Create(ctx, m.UserID, parentID, afterPos, newTitle, "")
		return err
	})
}

// setText saves one bullet's text.
//
// It is the one route that does not go through mutate, and the one that
// ignores focus_id: its subject is the path id, so target and focus cannot
// differ, and there is only one write to make. The row form still sends the
// hidden focus_id field, because the same form's other buttons need it.
func (a *App) setText(w http.ResponseWriter, r *http.Request) {
	userID, ok := a.userID(w, r)
	if !ok {
		return
	}
	id, ok := a.nodeID(w, r)
	if !ok {
		return
	}
	root, ok := formID(r, "root")
	if !ok {
		a.deps.Errors.Status(w, r, http.StatusBadRequest)
		return
	}

	// trimTitle, not an inline TrimRight: the same call Ops.SetText makes
	// before saving, so the Markdown rendered below can never drift from
	// what the database now holds — issue #72.
	title := trimTitle(r.PostFormValue("title"))
	note := r.PostFormValue("note")

	if err := a.store.SetText(r.Context(), userID, id, title, note); err != nil {
		a.fail(w, r, err)
		return
	}
	if web.IsHTMX(r) {
		row := &outlineRow{Node: Node{ID: id}, OOB: true, RenderedTitle: Render(title), RenderedNote: Render(note)}
		if err := a.deps.Render.Fragment(w, http.StatusOK, "notes/outline", "text-update", row); err != nil {
			a.deps.Errors.Internal(w, r, err)
		}
		return
	}
	http.Redirect(w, r, outlinePath(root), http.StatusSeeOther)
}

// indent makes a bullet the last child of the sibling above it. Being already
// first is a no-op in the store, not an error — see Ops.Indent.
func (a *App) indent(w http.ResponseWriter, r *http.Request) {
	a.mutate(w, r, func(ctx context.Context, o *Ops, m mutation) error {
		return o.Indent(ctx, m.UserID, m.NodeID)
	})
}

// outdent makes a bullet the next sibling of its own parent.
//
// The template disables this on a direct child of the zoom root, because the
// result would be a bullet that is still there and no longer on screen. The
// handler does not enforce that: it is a UI courtesy, not a rule about the
// tree, and the store's answer is correct either way.
func (a *App) outdent(w http.ResponseWriter, r *http.Request) {
	a.mutate(w, r, func(ctx context.Context, o *Ops, m mutation) error {
		return o.Outdent(ctx, m.UserID, m.NodeID)
	})
}

// move swaps a bullet with the sibling above or below it.
//
// dir is exactly "up" or "down". The general Move — arbitrary parent, arbitrary
// position — stays out of the HTTP surface until something needs it: N10's
// drag-to-move is the only thing in the design that does.
func (a *App) move(w http.ResponseWriter, r *http.Request) {
	dir := r.PostFormValue("dir")
	if dir != "up" && dir != "down" {
		a.deps.Errors.Status(w, r, http.StatusBadRequest)
		return
	}

	a.mutate(w, r, func(ctx context.Context, o *Ops, m mutation) error {
		if dir == "up" {
			return o.MoveUp(ctx, m.UserID, m.NodeID)
		}
		return o.MoveDown(ctx, m.UserID, m.NodeID)
	})
}

// collapse sets a bullet's collapse state.
//
// The field names the state to arrive at rather than asking for a toggle, so a
// double submit, a refresh or a stale page cannot flip it back. The rendered
// chevron already knows the current state and sends its opposite.
func (a *App) collapse(w http.ResponseWriter, r *http.Request) {
	raw := r.PostFormValue("collapsed")
	if raw != "0" && raw != "1" {
		a.deps.Errors.Status(w, r, http.StatusBadRequest)
		return
	}
	collapsed := raw == "1"

	a.mutate(w, r, func(ctx context.Context, o *Ops, m mutation) error {
		return o.SetCollapsed(ctx, m.UserID, m.NodeID, collapsed)
	})
}

// remove deletes a bullet and everything under it.
//
// Named remove rather than delete because delete is a builtin, and shadowing a
// builtin in a method name reads worse than the one-word mismatch with the
// route.
//
// The subtree goes with it through ON DELETE CASCADE. It is the only
// irreversible thing in this chunk, which is why the template gives it a form
// of its own with data-confirm on it.
//
// Deleting the bullet the page is zoomed to would redirect to a URL that no
// longer resolves, and answer 404. The outline never renders its own root as a
// row, so that cannot be reached from the UI; a hand-made request that does it
// gets an honest "there is nothing at that address" rather than a 500.
func (a *App) remove(w http.ResponseWriter, r *http.Request) {
	a.mutate(w, r, func(ctx context.Context, o *Ops, m mutation) error {
		if err := o.Delete(ctx, m.UserID, m.NodeID); err != nil {
			return err
		}
		a.deps.Log.Info("bullet deleted", "app", ID, "user_id", m.UserID, "node_id", m.NodeID)
		return nil
	})
}

// done marks a bullet done or not. The field names the state to arrive at,
// exactly like collapsed's, so a double submit or a stale page cannot flip
// it back.
func (a *App) done(w http.ResponseWriter, r *http.Request) {
	raw := r.PostFormValue("done")
	if raw != "0" && raw != "1" {
		a.deps.Errors.Status(w, r, http.StatusBadRequest)
		return
	}
	done := raw == "1"

	a.mutate(w, r, func(ctx context.Context, o *Ops, m mutation) error {
		return o.SetDone(ctx, m.UserID, m.NodeID, done)
	})
}

// due sets or clears a bullet's due date. An empty value clears it, which is
// what a native <input type="date">'s own clear affordance sends — there is
// no separate "remove due date" control. ValidateDue's error already maps
// to a 400 through fail, so there is nothing to check ahead of mutate here.
func (a *App) due(w http.ResponseWriter, r *http.Request) {
	due := r.PostFormValue("due")
	a.mutate(w, r, func(ctx context.Context, o *Ops, m mutation) error {
		return o.SetDue(ctx, m.UserID, m.NodeID, due)
	})
}

// prefs sets the show-completed preference — spec §11. This is a plain POST
// rather than a JS cookie write, and picks up the platform's CSRF
// protection for exactly that reason (see prefs.go).
func (a *App) prefs(w http.ResponseWriter, r *http.Request) {
	raw := r.PostFormValue("show_completed")
	if raw != "0" && raw != "1" {
		a.deps.Errors.Status(w, r, http.StatusBadRequest)
		return
	}
	root, ok := formID(r, "root")
	if !ok {
		a.deps.Errors.Status(w, r, http.StatusBadRequest)
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     ShowCompletedCookie,
		Value:    raw,
		Path:     "/notes/",
		HttpOnly: true,
		// Issue #79: a.deps.Secure, not hardcoded true — this cookie is not
		// a secret, but a hardcoded true would make it silently stop
		// persisting on any real (non-localhost) deployment that isn't
		// served over TLS, the same reason the platform's own session and
		// CSRF cookies read this from configuration rather than assuming it.
		Secure:   a.deps.Secure,
		SameSite: http.SameSiteLaxMode,
		// A preference, not a session: without MaxAge this would reset
		// every time the browser closes. A year is long enough to feel
		// permanent and short enough that an abandoned browser forgets.
		MaxAge: showCompletedCookieMaxAge,
	})

	if web.IsHTMX(r) {
		userID, ok := a.userID(w, r)
		if !ok {
			return
		}
		// The value just computed, not showCompletedFrom(r): r still
		// carries whatever the browser sent on this request, before the
		// SetCookie above, which the browser will only start sending back
		// on its *next* one.
		a.renderOutlineFragment(w, r, userID, root, raw == "1")
		return
	}
	http.Redirect(w, r, prefsRedirectTarget(r, root), http.StatusSeeOther)
}

// prefsRedirectTarget is where a non-HTMX prefs toggle sends the browser
// back to. The outline's own toggle is HTMX (handled above); /notes/search's
// plain-form toggle (issue #88) is not, since that page does no partial
// swapping of its own, so it needs a real redirect back to itself — with
// its query string preserved, or the toggle would silently reset the search.
//
// page is a closed enum read from a hidden field, not an arbitrary URL:
// a forged value can only ever select one of these known-safe destinations,
// never something open-redirect-shaped.
func prefsRedirectTarget(r *http.Request, root int64) string {
	switch r.PostFormValue("page") {
	case "search":
		q := r.PostFormValue("q")
		if q == "" {
			return "/notes/search"
		}
		return "/notes/search?q=" + url.QueryEscape(q)
	default:
		return outlinePath(root)
	}
}

// idsOf is the ID column of a node slice — issue #77: the shared shape
// dueList, buildArchiveView, and search each hand to Store.AncestorsMany to
// fetch every row's breadcrumb in one batched query instead of one per row.
func idsOf(nodes []Node) []int64 {
	ids := make([]int64, len(nodes))
	for i, n := range nodes {
		ids[i] = n.ID
	}
	return ids
}

// dueList renders every one of the user's due bullets, grouped by urgency —
// spec §11.
func (a *App) dueList(w http.ResponseWriter, r *http.Request) {
	userID, ok := a.userID(w, r)
	if !ok {
		return
	}
	nodes, err := a.store.Due(r.Context(), userID)
	if err != nil {
		a.deps.Errors.Internal(w, r, err)
		return
	}

	crumbs, err := a.store.AncestorsMany(r.Context(), userID, idsOf(nodes))
	if err != nil {
		a.deps.Errors.Internal(w, r, err)
		return
	}
	rows := make([]DueRow, len(nodes))
	for i, n := range nodes {
		rows[i] = DueRow{Node: n, Crumbs: crumbs[n.ID]}
	}

	page := a.deps.Page(r, "Due")
	page.Data = GroupByDue(rows, time.Now())
	a.render(w, r, http.StatusOK, "notes/due", page)
}

// archiveList renders every one of the user's archived subtree roots —
// spec §13.
func (a *App) archiveList(w http.ResponseWriter, r *http.Request) {
	userID, ok := a.userID(w, r)
	if !ok {
		return
	}
	view, err := a.buildArchiveView(r.Context(), userID)
	if err != nil {
		a.deps.Errors.Internal(w, r, err)
		return
	}
	view.CSRFToken = web.CSRFToken(r.Context())
	page := a.deps.Page(r, "Archive")
	page.Data = view
	a.render(w, r, http.StatusOK, "notes/archive", page)
}

// renderArchiveFragment re-renders /notes/archive's own list for an HTMX
// restore — the equivalent of renderOutlineFragment, but targeting this
// page's own swap target instead of #outline.
func (a *App) renderArchiveFragment(w http.ResponseWriter, r *http.Request, userID int64) {
	view, err := a.buildArchiveView(r.Context(), userID)
	if err != nil {
		a.deps.Errors.Internal(w, r, err)
		return
	}
	view.CSRFToken = web.CSRFToken(r.Context())
	if err := a.deps.Render.Fragment(w, http.StatusOK, "notes/archive", "archive-list", view); err != nil {
		a.deps.Errors.Internal(w, r, err)
	}
}

// buildArchiveView is the query behind both of the above: the full page and
// the HTMX fragment render exactly the same rows, so they share the one
// place that fetches them.
func (a *App) buildArchiveView(ctx context.Context, userID int64) (archiveView, error) {
	nodes, err := a.store.Archive(ctx, userID)
	if err != nil {
		return archiveView{}, err
	}
	crumbs, err := a.store.AncestorsMany(ctx, userID, idsOf(nodes))
	if err != nil {
		return archiveView{}, err
	}
	rows := make([]ArchiveRow, len(nodes))
	for i, n := range nodes {
		rows[i] = ArchiveRow{Node: n, Crumbs: crumbs[n.ID]}
	}
	return archiveView{Rows: rows}, nil
}

// archive marks a bullet archived, or restores it — spec §13. The field
// names the state to arrive at, exactly like done's and collapsed's, so a
// double submit or a stale page cannot flip it back.
//
// Archiving happens from a row in the outline (Task 4's new menu item),
// which may carry unsaved text in its title or note, so it goes through
// mutate exactly like every other structural op. Restoring happens from a
// row on /notes/archive, which is never mid-edit — there is no outline row
// to save text from, and the response that page needs back is its own
// list, not the outline — so it forks to restore instead. See this task's
// design note.
func (a *App) archive(w http.ResponseWriter, r *http.Request) {
	raw := r.PostFormValue("archived")
	if raw != "0" && raw != "1" {
		a.deps.Errors.Status(w, r, http.StatusBadRequest)
		return
	}
	if raw == "0" {
		a.restore(w, r)
		return
	}

	a.mutate(w, r, func(ctx context.Context, o *Ops, m mutation) error {
		return o.SetArchived(ctx, m.UserID, m.NodeID, true)
	})
}

// restore un-archives a bullet from its row on /notes/archive. See archive.
func (a *App) restore(w http.ResponseWriter, r *http.Request) {
	userID, ok := a.userID(w, r)
	if !ok {
		return
	}
	id, ok := a.nodeID(w, r)
	if !ok {
		return
	}
	if err := a.store.SetArchived(r.Context(), userID, id, false); err != nil {
		a.fail(w, r, err)
		return
	}
	if web.IsHTMX(r) {
		a.renderArchiveFragment(w, r, userID)
		return
	}
	http.Redirect(w, r, "/notes/archive", http.StatusSeeOther)
}

// search runs spec §12's full-text search across the whole tree. An empty
// query shows just the search box, with nothing to list — there is nothing
// sensible to prefill a fresh search with, unlike the outline's own empty
// bullet.
func (a *App) search(w http.ResponseWriter, r *http.Request) {
	userID, ok := a.userID(w, r)
	if !ok {
		return
	}
	// Trimmed once, here, rather than leaving the raw value to reach the
	// template — issue #92: search.html's own "no matches" branch used to
	// check the untrimmed Query, so a whitespace-only q rendered
	// "No matches for '  '" instead of the bare search box a genuinely
	// empty query gets, even though this guard already correctly skipped
	// running a search for it.
	query := strings.TrimSpace(r.URL.Query().Get("q"))

	var rows []SearchRow
	if query != "" {
		nodes, err := a.store.Search(r.Context(), userID, query, showCompletedFrom(r))
		if err != nil {
			a.deps.Errors.Internal(w, r, err)
			return
		}
		crumbs, err := a.store.AncestorsMany(r.Context(), userID, idsOf(nodes))
		if err != nil {
			a.deps.Errors.Internal(w, r, err)
			return
		}
		rows = make([]SearchRow, len(nodes))
		for i, n := range nodes {
			rows[i] = SearchRow{Node: n, Crumbs: crumbs[n.ID]}
		}
	}

	page := a.deps.Page(r, "Search")
	page.Data = searchView{Query: query, Rows: rows, ShowCompleted: showCompletedFrom(r)}
	a.render(w, r, http.StatusOK, "notes/search", page)
}

// export downloads userID's whole tree, or one subtree, as spec §14's
// Markdown outline format.
func (a *App) export(w http.ResponseWriter, r *http.Request) {
	userID, ok := a.userID(w, r)
	if !ok {
		return
	}
	rootID, ok := exportRootFrom(r)
	if !ok {
		a.deps.Errors.Status(w, r, http.StatusNotFound)
		return
	}
	if rootID != RootID {
		if _, err := a.store.ByID(r.Context(), userID, rootID); err != nil {
			a.fail(w, r, err)
			return
		}
	}

	flat, err := a.store.Export(r.Context(), userID, rootID)
	if err != nil {
		a.deps.Errors.Internal(w, r, err)
		return
	}

	w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="notes-export.md"`)
	_, _ = w.Write([]byte(ExportMarkdown(flat)))
}

// exportRootFrom parses export's ?root= query parameter: RootID (the
// whole tree) when absent, exactly like formID's "absent means none" rule
// for a hidden form field, adapted to a query string instead of a POST
// body.
func exportRootFrom(r *http.Request) (int64, bool) {
	raw := r.URL.Query().Get("root")
	if raw == "" {
		return RootID, true
	}
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || id < 0 {
		return 0, false
	}
	return id, true
}

// importNotes parses an uploaded Markdown file (spec §14) and creates its
// bullets as new children of root — the outline's current zoom, per the
// hidden root field every structural form already carries. It does not go
// through mutate: uploading a file from the toolbar happens with no
// outline row being edited, the same situation prefs and restore are
// already in, so there is no focused bullet's text to save alongside it.
//
// No http.MaxBytesReader here: CSRF.Middleware (Task 1,
// internal/platform/web/csrf.go) has already called r.ParseMultipartForm
// on this request looking for the token, so r.Body is already fully
// consumed by the time this handler runs — wrapping it now would protect
// nothing. MaxImportFileBytes is enforced below by checking the uploaded
// file's own reported size instead.
func (a *App) importNotes(w http.ResponseWriter, r *http.Request) {
	userID, ok := a.userID(w, r)
	if !ok {
		return
	}

	if err := r.ParseMultipartForm(web.DefaultMaxBodyBytes); err != nil {
		a.deps.Errors.Status(w, r, http.StatusBadRequest)
		return
	}

	root, ok := formID(r, "root")
	if !ok {
		a.deps.Errors.Status(w, r, http.StatusBadRequest)
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		a.deps.Errors.Status(w, r, http.StatusBadRequest)
		return
	}
	defer func() { _ = file.Close() }()
	if header.Size > MaxImportFileBytes {
		a.deps.Errors.Status(w, r, http.StatusBadRequest)
		return
	}
	data, err := io.ReadAll(file)
	if err != nil {
		a.deps.Errors.Status(w, r, http.StatusBadRequest)
		return
	}

	parsed, err := ParseMarkdown(string(data))
	if err != nil {
		a.fail(w, r, err)
		return
	}
	if _, err := a.store.ImportUnder(r.Context(), userID, root, parsed); err != nil {
		a.fail(w, r, err)
		return
	}

	if web.IsHTMX(r) {
		a.renderOutlineFragment(w, r, userID, root, showCompletedFrom(r))
		return
	}
	http.Redirect(w, r, outlinePath(root), http.StatusSeeOther)
}

// paste creates a subtree under NodeID from a clipboard block — spec §14,
// "the same code path, reached from the editor instead of from a file"
// as POST /notes/import (Task 5). See this task's own design notes in the
// plan for why the shape decision is entirely notes.js's, why this goes
// through mutate while import does not, and why there is no
// http.MaxBytesReader here.
func (a *App) paste(w http.ResponseWriter, r *http.Request) {
	text := r.PostFormValue("text")
	if len(text) > MaxPasteTextBytes {
		a.deps.Errors.Status(w, r, http.StatusBadRequest)
		return
	}

	a.mutate(w, r, func(ctx context.Context, o *Ops, m mutation) error {
		parsed, err := ParseMarkdown(text)
		if err != nil {
			return err
		}
		_, err = o.ImportUnder(ctx, m.UserID, m.NodeID, parsed)
		return err
	})
}
