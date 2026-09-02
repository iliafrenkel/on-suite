package notes

import "html/template"

// outlineView is what the outline template renders.
type outlineView struct {
	// Root is the node the outline is zoomed to. At the top level it is the
	// zero Node, whose ID is RootID — so the template can write
	// .Root.ID into a hidden field without a special case.
	Root   Node
	Zoomed bool
	// Crumbs are Root's ancestors, outermost first. Empty unless Zoomed.
	Crumbs []Node
	// Rows is the visible outline, nested. Empty with HiddenCount == 0 means
	// the outline shows one empty bullet instead — spec §6. Empty with
	// HiddenCount > 0 means there are bullets here, but hideDone hid every
	// one of them — issue #75: without this, that state was indistinguishable
	// from a genuinely empty outline, and looked like data loss.
	Rows        []*outlineRow
	HiddenCount int
	CSRFToken   string
	// ShowCompleted is spec §11's preference, read once per request so the
	// toolbar's toggle button can show its own opposite action.
	ShowCompleted bool
	// OOB marks this view as an HTMX fragment response, which is what makes
	// the toolbar's toggle carry hx-swap-oob. The toggle lives outside
	// #outline, so a normal swap can never reach it; without the
	// out-of-band copy its label and value would still describe the state
	// the page was loaded in. A full page render must leave this false —
	// see the note above "show-completed-toggle" in outline.html.
	OOB bool
	// ShareURL is "/notes/s/{slug}" when Zoomed and Root is shared, ""
	// otherwise. Computed once here so the template does not concatenate
	// a path from user-controlled data itself.
	ShareURL string
	// DueCount is DueBadgeCount's result, computed once per request for the
	// toolbar's Due button badge — see renderOutline/renderOutlineFragment
	// (handlers.go).
	DueCount int
}

// outlineRow is one bullet, and exactly the inputs of the one form that edits
// it. Root and CSRFToken are stamped onto every row rather than read from a
// shared parent because the template renders rows through a block that calls
// itself, and a recursive block can only be handed one argument: the slice of
// children. Denormalising two fields is the cheaper half of that trade.
type outlineRow struct {
	// Node carries ID, Title, Note, Position, Depth, Collapsed,
	// HasChildren, ChildCount and DoneChildCount, all set by Store.Outline.
	Node
	// Last is true for the final row of a sibling list, which is what decides
	// whether "move down" renders disabled.
	Last bool
	// RootID is the zoom root this outline was loaded at, for the hidden
	// field that sends the mutation back to the right page. It is an id, not
	// a Node, which is the whole difference from outlineView.Root — hence the
	// different name.
	RootID    int64
	CSRFToken string
	Children  []*outlineRow
	// RenderedTitle and RenderedNote are Title and Note run through Render —
	// spec §10. The template shows these by default and the raw input only
	// while it has focus; computing them here, once, keeps that a template
	// concern rather than something outline-rows has to call out to.
	RenderedTitle template.HTML
	RenderedNote  template.HTML
	// Overdue is DueOn set and in the past, relative to the day nest was
	// called — spec §11: comparison is against the server's local date.
	// Computed once here, not in the template, for the same reason
	// RenderedTitle/RenderedNote are: a recursive block gets one argument.
	Overdue bool
	// OOB marks this row as the subject of an out-of-band swap, which is
	// what makes the shared overlay blocks emit hx-swap-oob. Only setText's
	// response sets it; nest never does, because htmx strips every
	// hx-swap-oob element out of the outline fragment that the structural
	// operations return, blanking the overlays if a plain row carried it.
	OOB bool
}

// nest turns Outline's flat pre-order slice into the tree the template renders
// as nested <ul>s.
//
// The nesting is not decoration. The CSP has no unsafe-inline, so a row cannot
// carry its indentation in a style attribute and cannot set a --depth custom
// property inline either; real markup nesting is what lets the stylesheet
// alone produce the indent and the vertical guide lines.
//
// It relies on exactly the ordering Outline guarantees: a parent immediately
// precedes its subtree, and depth rises by at most one from a row to the next.
// A row that breaks that has no correct parent — and inventing one would put a
// bullet somewhere the user never left it — so it is dropped, along with
// everything that would have hung beneath it.
func nest(flat []Node, root int64, csrfToken, today string) []*outlineRow {
	var top []*outlineRow

	// open is the ancestor chain of the row most recently added: open[d] is
	// that chain's row at depth d, so a row at depth d attaches to open[d-1].
	// It holds pointers, not indices into a slice that append may move.
	open := make([]*outlineRow, 0, MaxDepth+1)

	for _, n := range flat {
		row := &outlineRow{
			Node: n, RootID: root, CSRFToken: csrfToken,
			RenderedTitle: Render(n.Title),
			RenderedNote:  Render(n.Note),
			Overdue:       n.DueOn != "" && n.DueOn < today,
		}

		switch d := n.Depth; {
		case d == 0:
			top = append(top, row)
			open = open[:0]
		case d > 0 && d <= len(open):
			parent := open[d-1]
			parent.Children = append(parent.Children, row)
			open = open[:d]
		default:
			// A depth that skips a level, or a negative one. Leaving open
			// untouched is what makes this row's own descendants fall into
			// this branch too.
			continue
		}
		open = append(open, row)
	}

	markLast(top)
	return top
}

// markLast flags the final row of every sibling list.
func markLast(rows []*outlineRow) {
	for i, r := range rows {
		r.Last = i == len(rows)-1
		markLast(r.Children)
	}
}

// hideDone drops a done node and everything under it, unless showCompleted
// is true — spec §11. Completing a parent does not complete its children
// (Task 1), so a child's own Done never matters once an ancestor's already
// hides it: this walks flat in the pre-order Outline guarantees, skipping
// everything deeper than the most recently hidden node until depth returns
// to that node's own level or shallower.
func hideDone(flat []Node, showCompleted bool) []Node {
	if showCompleted {
		return flat
	}
	var out []Node
	skipBelow := -1
	for _, n := range flat {
		if skipBelow >= 0 && n.Depth > skipBelow {
			continue
		}
		skipBelow = -1
		if n.Done {
			skipBelow = n.Depth
			continue
		}
		out = append(out, n)
	}
	return out
}
