package books

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
)

// Shelf is where a book sits. It is never stored: it follows from the
// book's latest reading (spec "Derived values").
type Shelf string

// The shelves, as the sidebar lists them.
const (
	ShelfReading Shelf = "reading"
	ShelfWant    Shelf = "want"
	ShelfRead    Shelf = "read"
	ShelfDNF     Shelf = "dnf"
	ShelfAll     Shelf = "all"
)

// Shelves is every shelf in sidebar order.
var Shelves = []Shelf{ShelfReading, ShelfWant, ShelfRead, ShelfDNF, ShelfAll}

// ParseShelf maps a stored or posted name to its Shelf.
func ParseShelf(s string) (Shelf, bool) {
	for _, sh := range Shelves {
		if string(sh) == s {
			return sh, true
		}
	}
	return "", false
}

// Status is where one reading stands.
type Status string

// Reading statuses.
const (
	StatusReading  Status = "reading"
	StatusFinished Status = "finished"
	StatusDNF      Status = "dnf"
)

// Reading is one time through a book. Dates are YYYY-MM-DD in the server's
// local day, "" when unknown.
type Reading struct {
	ID         int64
	Status     Status
	Format     string // "", "paper", "ebook" or "audio"
	StartedOn  string
	FinishedOn string
}

// Book is one book with its tags and the reading that decides its shelf.
type Book struct {
	ID int64
	BookInput
	Rating             int    // 1–5, 0 for none (set from B2)
	Review             string // Markdown (set from B2)
	Tags               []string
	Shelf              Shelf
	Latest             Reading  // zero ID before the first reading
	Progress           Progress // the latest reading's latest progress
	CoverVersion       string   // "" when the book has no cover
	AddedAt, UpdatedAt time.Time
}

// NewBook is a book being added: its details, where it goes and its tags.
type NewBook struct {
	BookInput
	// Shelf is ShelfWant (no reading), ShelfReading (a reading started
	// today) or ShelfRead (a finished reading with no start date).
	Shelf Shelf
	// FinishedOn is the ShelfRead finish date, YYYY-MM-DD; "" means today.
	FinishedOn string
	Tags       []string // raw names; Create cleans them
	// OLWorkID and OLEditionID come from an Open Library pick; Create keeps
	// them only if they look like Open Library keys.
	OLWorkID, OLEditionID string
}

// latestJoin attaches each book's latest reading as r — the one that
// decides its shelf. Ties on created_at go to the newer id.
const latestJoin = `LEFT JOIN books_readings r ON r.id = (
	SELECT id FROM books_readings WHERE book_id = b.id ORDER BY created_at DESC, id DESC LIMIT 1)`

// shelfExpr is a book's shelf from r (see latestJoin): the one place the
// shelf rule is written down in SQL.
const shelfExpr = `CASE WHEN r.id IS NULL THEN 'want'
	WHEN r.status = 'reading' THEN 'reading'
	WHEN r.status = 'finished' THEN 'read'
	ELSE 'dnf' END`

// nullInt and nullText store 0 and "" as NULL.
func nullInt(n int) any {
	if n == 0 {
		return nil
	}
	return n
}

func nullText(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// olIDPattern is an Open Library key: "OL", a number, and W (work) or M
// (edition).
var olIDPattern = regexp.MustCompile(`^OL[1-9][0-9]{0,11}[WM]$`)

// olID returns s if it is an Open Library key of the given kind ('W' or
// 'M'), otherwise "".
func olID(s string, kind byte) string {
	if olIDPattern.MatchString(s) && s[len(s)-1] == kind {
		return s
	}
	return ""
}

// Create adds a book, its first reading if it goes on Reading or Read, and
// its tags, in one transaction. Details failing Validate are a
// *ValidationError, a bad finish date a *Refusal, any other shelf
// ErrInvalid.
func (st *Store) Create(ctx context.Context, userID int64, nb NewBook) (int64, error) {
	in := nb.BookInput.Normalize()
	if errs := in.Validate(); errs != nil {
		return 0, &ValidationError{Fields: errs}
	}
	finished := ""
	switch nb.Shelf {
	case ShelfWant, ShelfReading:
	case ShelfRead:
		finished = nb.FinishedOn
		if finished == "" {
			finished = st.Today()
		}
		if err := st.checkDay(finished); err != nil {
			return 0, err
		}
	default:
		return 0, ErrInvalid
	}

	now := formatTime(st.now())
	tx, err := st.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("books: begin create: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	res, err := tx.ExecContext(ctx, `
		INSERT INTO books_books (user_id, title, subtitle, authors, year, pages, isbn13,
			ol_work_id, ol_edition_id, series_name, series_number, description, added_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		userID, in.Title, in.Subtitle, in.Authors, nullInt(in.Year), nullInt(in.Pages), nullText(in.ISBN),
		olID(nb.OLWorkID, 'W'), olID(nb.OLEditionID, 'M'),
		in.SeriesName, in.SeriesNumber, in.Description, now, now)
	if err != nil {
		return 0, fmt.Errorf("books: create: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("books: create: %w", err)
	}
	switch nb.Shelf {
	case ShelfReading:
		_, err = tx.ExecContext(ctx,
			`INSERT INTO books_readings (book_id, status, started_on, created_at) VALUES (?, 'reading', ?, ?)`,
			id, st.Today(), now)
	case ShelfRead:
		_, err = tx.ExecContext(ctx,
			`INSERT INTO books_readings (book_id, status, finished_on, created_at) VALUES (?, 'finished', ?, ?)`,
			id, finished, now)
	}
	if err != nil {
		return 0, fmt.Errorf("books: first reading: %w", err)
	}
	if err := linkTags(ctx, tx, userID, id, cleanTags(nb.Tags)); err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("books: commit create: %w", err)
	}
	return id, nil
}

// Get returns one of userID's books with its tags and latest reading.
func (st *Store) Get(ctx context.Context, userID, id int64) (Book, error) {
	var b Book
	var year, pages, rating, rid, atPage, atPercent sql.NullInt64
	var isbn, status, format, started, finished, cover, recorded sql.NullString
	var added, updated, shelf string
	err := st.db.QueryRowContext(ctx, `
		SELECT b.id, b.title, b.subtitle, b.authors, b.year, b.pages, b.isbn13, b.series_name,
		       b.series_number, b.description, b.rating, b.review, b.added_at, b.updated_at,
		       r.id, r.status, r.format, r.started_on, r.finished_on, `+shelfExpr+`, c.fetched_at,
		       p.page, p.percent, p.recorded_at
		  FROM books_books b `+latestJoin+`
		  `+progressJoin+`
		  LEFT JOIN books_covers c ON c.book_id = b.id
		 WHERE b.id = ? AND b.user_id = ?`, id, userID).Scan(
		&b.ID, &b.Title, &b.Subtitle, &b.Authors, &year, &pages, &isbn, &b.SeriesName,
		&b.SeriesNumber, &b.Description, &rating, &b.Review, &added, &updated,
		&rid, &status, &format, &started, &finished, &shelf, &cover,
		&atPage, &atPercent, &recorded)
	if errors.Is(err, sql.ErrNoRows) {
		return Book{}, ErrNotFound
	}
	if err != nil {
		return Book{}, fmt.Errorf("books: get: %w", err)
	}
	b.Year, b.Pages, b.Rating = int(year.Int64), int(pages.Int64), int(rating.Int64)
	b.ISBN = isbn.String
	b.Latest = Reading{ID: rid.Int64, Status: Status(status.String), Format: format.String,
		StartedOn: started.String, FinishedOn: finished.String}
	b.Shelf = Shelf(shelf)
	b.CoverVersion = coverVersion(cover.String)
	if b.Progress, err = scanProgress(atPage, atPercent, recorded); err != nil {
		return Book{}, err
	}
	if b.AddedAt, err = parseTime(added); err != nil {
		return Book{}, fmt.Errorf("books: added_at: %w", err)
	}
	if b.UpdatedAt, err = parseTime(updated); err != nil {
		return Book{}, fmt.Errorf("books: updated_at: %w", err)
	}
	if b.Tags, err = st.BookTags(ctx, userID, id); err != nil {
		return Book{}, err
	}
	return b, nil
}

// Update replaces a book's details. Readings and tags are untouched.
func (st *Store) Update(ctx context.Context, userID, id int64, in BookInput) error {
	in = in.Normalize()
	if errs := in.Validate(); errs != nil {
		return &ValidationError{Fields: errs}
	}
	res, err := st.db.ExecContext(ctx, `
		UPDATE books_books
		   SET title = ?, subtitle = ?, authors = ?, year = ?, pages = ?, isbn13 = ?,
		       series_name = ?, series_number = ?, description = ?, updated_at = ?
		 WHERE id = ? AND user_id = ?`,
		in.Title, in.Subtitle, in.Authors, nullInt(in.Year), nullInt(in.Pages), nullText(in.ISBN),
		in.SeriesName, in.SeriesNumber, in.Description, formatTime(st.now()), id, userID)
	if err != nil {
		return fmt.Errorf("books: update: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// Delete removes a book for good, with its readings and tag links (ON
// DELETE CASCADE), and any tag nothing else uses.
func (st *Store) Delete(ctx context.Context, userID, id int64) error {
	tx, err := st.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("books: begin delete: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	res, err := tx.ExecContext(ctx, `DELETE FROM books_books WHERE id = ? AND user_id = ?`, id, userID)
	if err != nil {
		return fmt.Errorf("books: delete: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	if err := gcTags(ctx, tx, userID); err != nil {
		return err
	}
	return tx.Commit()
}

// ListItem is the slice of a book a list row needs.
type ListItem struct {
	ID                                       int64
	Title, Authors, SeriesName, SeriesNumber string
	Pages                                    int
	Shelf                                    Shelf
	StartedOn, FinishedOn, Format            string   // the latest reading's
	Progress                                 Progress // the latest reading's latest progress
	AddedAt                                  time.Time
	CoverVersion                             string // "" when the book has no cover
}

// ListQuery picks the books a list shows. Shelf "" or ShelfAll is every
// shelf; Tag is one tag name; Q matches title, subtitle, authors or series
// name (SQLite LIKE: case-insensitive for ASCII).
type ListQuery struct {
	Shelf Shelf
	Tag   string
	Q     string
}

// listOrder is each shelf's sort (spec "Layout"): Reading by the latest
// progress (a reading with none yet by when it started), Read by finish,
// Want to read by date added, DNF and All by the latest change. NULL dates
// sort last.
func listOrder(s Shelf) string {
	switch s {
	case ShelfReading:
		return `COALESCE(p.recorded_at, r.created_at) DESC, r.id DESC`
	case ShelfRead:
		return `r.finished_on DESC, r.id DESC`
	case ShelfWant:
		return `b.added_at DESC, b.id DESC`
	default:
		return `b.updated_at DESC, b.id DESC`
	}
}

// likeEscape makes s match itself literally in a LIKE ... ESCAPE '\'.
func likeEscape(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
}

// List returns userID's books matching q, in the shelf's order.
func (st *Store) List(ctx context.Context, userID int64, q ListQuery) ([]ListItem, error) {
	where := []string{"b.user_id = ?"}
	args := []any{userID}
	if q.Shelf != "" && q.Shelf != ShelfAll {
		where = append(where, shelfExpr+" = ?")
		args = append(args, string(q.Shelf))
	}
	if tag := strings.ToLower(strings.TrimSpace(q.Tag)); tag != "" {
		where = append(where, `EXISTS (SELECT 1 FROM books_book_tags x JOIN books_tags t ON t.id = x.tag_id
			WHERE x.book_id = b.id AND t.name = ?)`)
		args = append(args, tag)
	}
	if text := strings.TrimSpace(q.Q); text != "" {
		pat := "%" + likeEscape(text) + "%"
		where = append(where, `(b.title LIKE ? ESCAPE '\' OR b.subtitle LIKE ? ESCAPE '\'
			OR b.authors LIKE ? ESCAPE '\' OR b.series_name LIKE ? ESCAPE '\')`)
		args = append(args, pat, pat, pat, pat)
	}
	rows, err := st.db.QueryContext(ctx, `
		SELECT b.id, b.title, b.authors, b.series_name, b.series_number, b.pages, b.added_at,
		       r.started_on, r.finished_on, r.format, `+shelfExpr+`, c.fetched_at,
		       p.page, p.percent, p.recorded_at
		  FROM books_books b `+latestJoin+`
		  `+progressJoin+`
		  LEFT JOIN books_covers c ON c.book_id = b.id
		 WHERE `+strings.Join(where, " AND ")+`
		 ORDER BY `+listOrder(q.Shelf), args...)
	if err != nil {
		return nil, fmt.Errorf("books: list: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []ListItem
	for rows.Next() {
		var it ListItem
		var added, shelf string
		var pages, atPage, atPercent sql.NullInt64
		var started, finished, format, cover, recorded sql.NullString
		if err := rows.Scan(&it.ID, &it.Title, &it.Authors, &it.SeriesName, &it.SeriesNumber, &pages, &added,
			&started, &finished, &format, &shelf, &cover, &atPage, &atPercent, &recorded); err != nil {
			return nil, fmt.Errorf("books: scan list: %w", err)
		}
		if it.AddedAt, err = parseTime(added); err != nil {
			return nil, fmt.Errorf("books: added_at: %w", err)
		}
		if it.Progress, err = scanProgress(atPage, atPercent, recorded); err != nil {
			return nil, err
		}
		it.Pages = int(pages.Int64)
		it.StartedOn, it.FinishedOn, it.Format, it.Shelf = started.String, finished.String, format.String, Shelf(shelf)
		it.CoverVersion = coverVersion(cover.String)
		out = append(out, it)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("books: list: %w", err)
	}
	return out, nil
}

// ShelfCounts returns how many of userID's books are on each shelf, with
// ShelfAll the total. Empty shelves are absent (zero).
func (st *Store) ShelfCounts(ctx context.Context, userID int64) (map[Shelf]int, error) {
	rows, err := st.db.QueryContext(ctx, `
		SELECT `+shelfExpr+` AS shelf, count(*)
		  FROM books_books b `+latestJoin+`
		 WHERE b.user_id = ?
		 GROUP BY shelf`, userID)
	if err != nil {
		return nil, fmt.Errorf("books: shelf counts: %w", err)
	}
	defer func() { _ = rows.Close() }()
	counts := map[Shelf]int{}
	for rows.Next() {
		var shelf string
		var n int
		if err := rows.Scan(&shelf, &n); err != nil {
			return nil, fmt.Errorf("books: scan shelf count: %w", err)
		}
		counts[Shelf(shelf)] = n
		counts[ShelfAll] += n
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("books: shelf counts: %w", err)
	}
	return counts, nil
}
