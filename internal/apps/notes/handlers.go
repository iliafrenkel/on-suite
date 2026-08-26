package notes

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"

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

	view := outlineView{CSRFToken: web.CSRFToken(r.Context())}

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

	flat, err := a.store.Outline(r.Context(), userID, rootID)
	if err != nil {
		a.deps.Errors.Internal(w, r, err)
		return
	}
	view.Rows = nest(flat, rootID, view.CSRFToken)

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
func (a *App) renderOutlineFragment(w http.ResponseWriter, r *http.Request, userID, rootID int64) {
	flat, err := a.store.Outline(r.Context(), userID, rootID)
	if err != nil {
		a.deps.Errors.Internal(w, r, err)
		return
	}
	view := outlineView{
		CSRFToken: web.CSRFToken(r.Context()),
		Root:      Node{ID: rootID},
	}
	view.Rows = nest(flat, rootID, view.CSRFToken)
	if err := a.deps.Render.Fragment(w, http.StatusOK, "notes/outline", "outline-body", view); err != nil {
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
		a.renderOutlineFragment(w, r, userID, root)
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

	// Trimmed exactly the way Ops.SetText trims title before saving, so the
	// Markdown rendered below matches what the database now holds.
	title := strings.TrimRight(r.PostFormValue("title"), " \t")
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
