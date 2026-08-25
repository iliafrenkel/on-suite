// Package notes implements ON Notes, a hierarchical outliner: one infinite
// tree per user, where every node is a bullet with a title, an optional
// secondary note, and children.
//
// It depends only on internal/platform/*. It never imports another app, and no
// platform package imports it: the whole coupling is the app.App interface plus
// one line in cmd/onsuite/main.go.
//
// The design is in docs/superpowers/specs/2026-08-25-on-notes-design.md. This
// package is chunk N1 of that spec: schema and store, with no HTTP.
package notes

import (
	"embed"
	"errors"
	"fmt"
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
)

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
