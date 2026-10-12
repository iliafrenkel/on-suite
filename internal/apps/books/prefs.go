package books

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
)

// View is how the home page lays its books out (UI redesign spec
// "Preferences", docs/superpowers/specs/2026-10-12-on-books-ui-redesign-design.md).
type View string

// The two views.
const (
	ViewGrid View = "grid"
	ViewList View = "list"
)

// Sort is the home page's order (spec "Sorting"). SortDefault is each
// shelf's own order, the one the shelves had before the redesign.
type Sort string

// The sorts.
const (
	SortDefault Sort = "default"
	SortAdded   Sort = "added"
	SortTitle   Sort = "title"
	SortAuthor  Sort = "author"
)

// Sorts is every sort, in the sort menu's order.
var Sorts = []Sort{SortDefault, SortAdded, SortTitle, SortAuthor}

var sortLabels = map[Sort]string{SortDefault: "Default", SortAdded: "Date added", SortTitle: "Title", SortAuthor: "Author"}

// Label is how a sort is named in the menu.
func (s Sort) Label() string { return sortLabels[s] }

// Prefs are one reader's home-page settings.
type Prefs struct {
	View     View
	Sort     Sort
	Reversed bool // the sort's natural direction turned round
}

// DefaultPrefs apply until the reader changes something.
var DefaultPrefs = Prefs{View: ViewGrid, Sort: SortDefault}

// Valid reports whether p names a view and a sort the home page has.
func (p Prefs) Valid() bool {
	return (p.View == ViewGrid || p.View == ViewList) && slices.Contains(Sorts, p.Sort)
}

// Prefs returns userID's home-page settings, or DefaultPrefs if they never
// changed any. It mirrors ON Later's Store.Prefs; apps never import each
// other, so this is an independent implementation.
func (st *Store) Prefs(ctx context.Context, userID int64) (Prefs, error) {
	var view, sort string
	var reversed int
	err := st.db.QueryRowContext(ctx, `SELECT view, sort, reversed FROM books_prefs WHERE user_id = ?`, userID).
		Scan(&view, &sort, &reversed)
	if errors.Is(err, sql.ErrNoRows) {
		return DefaultPrefs, nil
	}
	if err != nil {
		return Prefs{}, fmt.Errorf("books: load prefs: %w", err)
	}
	return Prefs{View: View(view), Sort: Sort(sort), Reversed: reversed == 1}, nil
}

// SetPrefs saves userID's home-page settings, replacing any earlier ones.
// A view or sort the home page has no control for is ErrInvalid.
func (st *Store) SetPrefs(ctx context.Context, userID int64, p Prefs) error {
	if !p.Valid() {
		return ErrInvalid
	}
	reversed := 0
	if p.Reversed {
		reversed = 1
	}
	_, err := st.db.ExecContext(ctx, `
		INSERT INTO books_prefs (user_id, view, sort, reversed, updated_at) VALUES (?, ?, ?, ?, ?)
		ON CONFLICT (user_id) DO UPDATE SET view = excluded.view, sort = excluded.sort,
			reversed = excluded.reversed, updated_at = excluded.updated_at`,
		userID, string(p.View), string(p.Sort), reversed, formatTime(st.now()))
	if err != nil {
		return fmt.Errorf("books: save prefs: %w", err)
	}
	return nil
}
