// Package notes implements ON Notes, a hierarchical outliner: one infinite
// tree per user, where every node is a bullet with a title, an optional
// secondary note, and children.
//
// It depends only on internal/platform/*. It never imports another app, and no
// platform package imports it: the whole coupling is the app.App interface plus
// one line in cmd/onsuite/main.go.
//
// The design is in docs/superpowers/specs/2026-08-25-on-notes-design.md. This
// package spans chunk N1 (schema and store) through N8 (export and
// import): app.App, routes, templates, handlers, the keyboard layer in
// static/notes.js, the inline Markdown renderer in markdown.go, task
// tracking in tree.go, prefs.go and due.go, full-text search in
// search.go, archiving in archive.go, and Markdown/JSON export plus
// Markdown import in export.go and import.go.
package notes

import (
	"embed"
	"errors"
	"fmt"
	"html/template"
	"io/fs"
	"strings"
	"time"
	"unicode/utf8"
)

// ID is the app id: the URL prefix, the migration namespace, and the prefix on
// every table this app owns.
const ID = "notes"

const (
	// MaxDepth is the deepest permitted depth, counting a top-level bullet as
	// 0. It exists for two reasons: a tree that has somehow acquired a cycle
	// must make a recursive query terminate rather than run forever, and a
	// runaway import must not produce an outline no UI can render. 64 is far
	// past any outline written by hand.
	MaxDepth = 64
	// MaxTitleRunes bounds one bullet's title. A bullet is a line, not a
	// document; anything longer belongs in the note or in a child.
	MaxTitleRunes = 2000
	// MaxNoteRunes bounds the secondary note under a bullet.
	MaxNoteRunes = 10000
	// RootID is the ParentID meaning "top level". Node ids come from
	// AUTOINCREMENT and start at 1, so 0 is never a real id, and callers never
	// have to handle a nullable parent.
	RootID = 0

	// MaxImportNodes bounds how many bullets a single import or a pasted
	// block may create — spec §14: rejected with a clear error rather
	// than truncated silently.
	MaxImportNodes = 5000
	// MaxImportFileBytes bounds an uploaded Markdown file (POST
	// /notes/import) — spec §14. web.DefaultMaxBodyBytes already caps
	// every request body at 1 MiB before this app's handler ever runs
	// (internal/platform/web/middleware.go); a multipart file part
	// carries almost no encoding overhead of its own, so 768 KiB of file
	// content leaves comfortable headroom under that ceiling for the
	// multipart boundaries and the request's other fields.
	MaxImportFileBytes = 768 << 10
	// MaxPasteTextBytes bounds a pasted block posted as a plain form
	// field (POST /notes/{id}/paste) — spec §14, the same bound applied
	// to the other way this app's Markdown parser is reached. Unlike a
	// file upload this field is application/x-www-form-urlencoded, which
	// percent-encodes every non-ASCII UTF-8 byte as "%XX" — up to 3x
	// expansion for Cyrillic, Hebrew or CJK text (see
	// internal/apps/paste/store.go's MaxBodyBytes for the same reasoning
	// applied to a snippet body — apps do not import each other, so this
	// is an independent constant with the same justification, not a
	// shared symbol). 256 KiB keeps the worst case (768 KiB encoded)
	// safely under the platform's 1 MiB limit, with room left for the
	// request's other fields.
	MaxPasteTextBytes = 256 << 10
	// shareSlugBytes is 16 bytes, 128 bits, base64url encoded to 22
	// characters — spec §15: "an unguessable slug". Mirrors
	// internal/apps/paste/store.go's own shareSlugBytes exactly; apps
	// never import each other, so this is an independent constant with
	// the same justification, not a shared symbol.
	shareSlugBytes = 16
)

// parentArg converts the RootID sentinel to the NULL the column actually
// stores, both for the value an INSERT or UPDATE writes and for the one a
// WHERE clause matches on. In a WHERE clause it is always paired with
// "parent_id IS ?", which SQLite treats as = for a non-NULL value and as a
// NULL test otherwise — so one query shape covers both top-level and nested
// nodes, with no branching anywhere.
//
// It sits beside RootID, rather than in store.go or tree.go, because reads and
// writes both need it and neither owns it.
func parentArg(parentID int64) any {
	if parentID == RootID {
		return nil
	}
	return parentID
}

//go:embed migrations/*.sql
var migrationFiles embed.FS

// Migrations returns this app's schema with filenames at the root, which is
// what db.Collect expects.
func Migrations() fs.FS {
	sub, err := fs.Sub(migrationFiles, "migrations")
	if err != nil {
		// Unreachable: the path is a compile-time constant checked by go:embed.
		panic("notes: embedded migrations missing: " + err.Error())
	}
	return sub
}

var (
	// ErrNotFound covers both "no such node" and "not yours". They are
	// deliberately indistinguishable: a distinct error for someone else's
	// node would confirm that it exists.
	ErrNotFound = errors.New("notes: not found")
	// ErrInvalid is text that fails Validate.
	ErrInvalid = errors.New("notes: invalid note")
	// ErrCycle is a move that would place a bullet inside its own subtree.
	// The result is unreachable from any root: still in the table, gone from
	// the outline.
	ErrCycle = errors.New("notes: a note cannot be moved inside itself")
	// ErrTooDeep is a create or move that would pass MaxDepth.
	ErrTooDeep = errors.New("notes: the outline would nest too deeply")
)

// Node is one bullet.
type Node struct {
	ID     int64
	UserID int64
	// ParentID is RootID for a top-level bullet.
	ParentID  int64
	Position  int
	Title     string
	Note      string
	Collapsed bool
	// Done and DueOn are done_at/due_on's Go projections — spec §11. Done
	// hides the underlying timestamp: nothing in this app ever needs to
	// show *when* a bullet was completed, only whether it is. DueOn is the
	// raw 'YYYY-MM-DD' string, or "" for none — a due date is a calendar
	// date, not an instant, so there is no time.Time here either.
	Done  bool
	DueOn string
	// Archived is archived_at's Go projection — spec §13. Same reasoning as
	// Done: nothing in this app ever needs to show *when* a bullet was put
	// away, only whether it is.
	Archived  bool
	// ShareSlug is "" when the bullet is not shared — spec §15. Unlike
	// Done/DueOn/Archived it is not itself an *_at timestamp's Go
	// projection: the slug's actual value is what a visitor's URL must
	// match, so it is carried as-is rather than reduced to a bool.
	ShareSlug string
	CreatedAt time.Time
	UpdatedAt time.Time

	// Depth and HasChildren are filled in by Outline and are zero elsewhere.
	// Depth is relative to the outline's root: its direct children are 0.
	Depth       int
	HasChildren bool
}

// DisplayTitle is what to show for a bullet saved with no text. An empty
// bullet is legitimate — pressing Enter creates one — so this is a display
// concern, not a validation failure.
func (n Node) DisplayTitle() string {
	if strings.TrimSpace(n.Title) == "" {
		return "Untitled"
	}
	return n.Title
}

// DisplayTitleHTML is DisplayTitle run through Render — spec §10 — for use
// anywhere a bullet's title is shown outside the outline's own rows (the
// zoomed heading, a non-link breadcrumb segment). Only outlineRow.RenderedTitle
// had this before N5/N6 added other places a title appears — issues #65, #76.
//
// Deliberately not used inside an <a>: Render can itself emit an <a> for a
// link, a bare autolink, or a #tag/@mention chip, and nesting anchors is
// invalid HTML — a link whose visible text is a title (a breadcrumb
// ancestor, a due-list row) stays on DisplayTitle instead.
func (n Node) DisplayTitleHTML() template.HTML {
	return Render(n.DisplayTitle())
}

// Shared reports whether the bullet currently has a live public link.
func (n Node) Shared() bool { return n.ShareSlug != "" }

// Validate bounds a bullet's user-supplied text. Exported because the handler
// reports these messages back to the user.
//
// Unlike ON Paste, an empty title is valid: the first thing Enter does is
// create a bullet with nothing in it.
func Validate(title, note string) error {
	if !utf8.ValidString(title) {
		return fmt.Errorf("%w: the title is not valid UTF-8", ErrInvalid)
	}
	if !utf8.ValidString(note) {
		return fmt.Errorf("%w: the note is not valid UTF-8", ErrInvalid)
	}
	if utf8.RuneCountInString(title) > MaxTitleRunes {
		return fmt.Errorf("%w: the title is longer than %d characters", ErrInvalid, MaxTitleRunes)
	}
	if utf8.RuneCountInString(note) > MaxNoteRunes {
		return fmt.Errorf("%w: the note is longer than %d characters", ErrInvalid, MaxNoteRunes)
	}
	return nil
}

// ValidateDue bounds a due date's format — spec §11: due_on is a date, not
// an instant. "" clears it. The round trip through Format catches what
// Parse alone would not: time.Parse silently normalises an impossible date
// like 2026-02-30 into March 2nd rather than rejecting it, and a normalised
// date does not equal the string it was parsed from.
func ValidateDue(due string) error {
	if due == "" {
		return nil
	}
	t, err := time.Parse("2006-01-02", due)
	if err != nil || t.Format("2006-01-02") != due {
		return fmt.Errorf("%w: due date must be YYYY-MM-DD", ErrInvalid)
	}
	return nil
}
