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
	// Rows is the visible outline, nested. Empty means the outline shows one
	// empty bullet instead — spec §6.
	Rows      []*outlineRow
	CSRFToken string
}

// outlineRow is one bullet, and exactly the inputs of the one form that edits
// it. Root and CSRFToken are stamped onto every row rather than read from a
// shared parent because the template renders rows through a block that calls
// itself, and a recursive block can only be handed one argument: the slice of
// children. Denormalising two fields is the cheaper half of that trade.
type outlineRow struct {
	// Node carries ID, Title, Note, Position, Depth, Collapsed and
	// HasChildren, all set by Store.Outline.
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
func nest(flat []Node, root int64, csrfToken string) []*outlineRow {
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
