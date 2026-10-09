package books

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// The shapes `onsuite export` writes for ON Books (spec "Export (B5)"),
// declared apart from the store's types so the backup format changes only
// when someone edits this file, as ON Later's are. Cover bytes are left
// out; the Markdown download (exportMarkdown) is drawn from the same
// payload.
type exportedProgress struct {
	Page       *int      `json:"page,omitempty"`
	Percent    *int      `json:"percent,omitempty"`
	RecordedAt time.Time `json:"recorded_at"`
}

type exportedReading struct {
	Status     Status             `json:"status"`
	Format     string             `json:"format,omitempty"`
	StartedOn  string             `json:"started_on,omitempty"`
	FinishedOn string             `json:"finished_on,omitempty"`
	CreatedAt  time.Time          `json:"created_at"`
	Progress   []exportedProgress `json:"progress"`
}

type exportedNote struct {
	Page      int       `json:"page,omitempty"`
	Body      string    `json:"body"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type exportedQuote struct {
	Page      int       `json:"page,omitempty"`
	Text      string    `json:"text"`
	Comment   string    `json:"comment,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type exportedBook struct {
	Title        string            `json:"title"`
	Subtitle     string            `json:"subtitle,omitempty"`
	Authors      string            `json:"authors,omitempty"`
	Year         int               `json:"year,omitempty"`
	Pages        int               `json:"pages,omitempty"`
	ISBN13       string            `json:"isbn13,omitempty"`
	OLWorkID     string            `json:"ol_work_id,omitempty"`
	OLEditionID  string            `json:"ol_edition_id,omitempty"`
	Description  string            `json:"description,omitempty"`
	SeriesName   string            `json:"series_name,omitempty"`
	SeriesNumber string            `json:"series_number,omitempty"`
	Rating       int               `json:"rating,omitempty"`
	Review       string            `json:"review,omitempty"`
	Shelf        Shelf             `json:"shelf"`
	Tags         []string          `json:"tags"`
	Readings     []exportedReading `json:"readings"` // oldest first
	Notes        []exportedNote    `json:"notes"`    // oldest first
	Quotes       []exportedQuote   `json:"quotes"`   // oldest first
	AddedAt      time.Time         `json:"added_at"`
	UpdatedAt    time.Time         `json:"updated_at"`
}

type exportedGoal struct {
	Year   int `json:"year"`
	Target int `json:"target"`
}

type exportPayload struct {
	Books []exportedBook `json:"books"` // oldest added first
	Goals []exportedGoal `json:"goals"`
}

// Export implements app.Exporter, joining ON Books to onsuite export's
// whole-account JSON backup.
func (a *App) Export(ctx context.Context, handle *sql.DB, userID int64) (any, error) {
	return NewStore(handle).Export(ctx, userID)
}

// eachRow runs query and scan on every row, closing the rows before it
// returns: the database has one connection, and Export queries again.
func (st *Store) eachRow(ctx context.Context, what, query string, args []any, scan func(*sql.Rows) error) error {
	rows, err := st.db.QueryContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("books: export %s: %w", what, err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		if err := scan(rows); err != nil {
			return fmt.Errorf("books: export %s: %w", what, err)
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("books: export %s: %w", what, err)
	}
	return nil
}

// Export gathers everything userID keeps in ON Books (spec "Export
// (B5)"): each table read once, then put together by book.
func (st *Store) Export(ctx context.Context, userID int64) (exportPayload, error) {
	uid := []any{userID}
	out := exportPayload{Books: []exportedBook{}, Goals: []exportedGoal{}}

	progress := map[int64][]exportedProgress{}
	err := st.eachRow(ctx, "progress", `
		SELECT p.reading_id, p.page, p.percent, p.recorded_at
		  FROM books_progress p JOIN books_readings r ON r.id = p.reading_id JOIN books_books b ON b.id = r.book_id
		 WHERE b.user_id = ? ORDER BY p.recorded_at, p.id`, uid, func(rows *sql.Rows) error {
		var rid int64
		var page, percent sql.NullInt64
		var at string
		if err := rows.Scan(&rid, &page, &percent, &at); err != nil {
			return err
		}
		p := exportedProgress{}
		if page.Valid {
			n := int(page.Int64)
			p.Page = &n
		}
		if percent.Valid {
			n := int(percent.Int64)
			p.Percent = &n
		}
		var err error
		p.RecordedAt, err = parseTime(at)
		progress[rid] = append(progress[rid], p)
		return err
	})
	if err != nil {
		return out, err
	}

	readings := map[int64][]exportedReading{}
	err = st.eachRow(ctx, "readings", `
		SELECT r.id, r.book_id, r.status, COALESCE(r.format, ''), COALESCE(r.started_on, ''),
		       COALESCE(r.finished_on, ''), r.created_at
		  FROM books_readings r JOIN books_books b ON b.id = r.book_id
		 WHERE b.user_id = ? ORDER BY r.created_at, r.id`, uid, func(rows *sql.Rows) error {
		var rid, bid int64
		var rd exportedReading
		var created string
		if err := rows.Scan(&rid, &bid, &rd.Status, &rd.Format, &rd.StartedOn, &rd.FinishedOn, &created); err != nil {
			return err
		}
		var err error
		rd.CreatedAt, err = parseTime(created)
		rd.Progress = append([]exportedProgress{}, progress[rid]...)
		readings[bid] = append(readings[bid], rd)
		return err
	})
	if err != nil {
		return out, err
	}

	notes := map[int64][]exportedNote{}
	err = st.eachRow(ctx, "notes", `
		SELECT n.book_id, COALESCE(n.page, 0), n.body, n.created_at, n.updated_at
		  FROM books_notes n JOIN books_books b ON b.id = n.book_id
		 WHERE b.user_id = ? ORDER BY n.created_at, n.id`, uid, func(rows *sql.Rows) error {
		var bid int64
		var n exportedNote
		var created, updated string
		if err := rows.Scan(&bid, &n.Page, &n.Body, &created, &updated); err != nil {
			return err
		}
		var err error
		n.CreatedAt, n.UpdatedAt, err = parseStamps(created, updated)
		notes[bid] = append(notes[bid], n)
		return err
	})
	if err != nil {
		return out, err
	}

	quotes := map[int64][]exportedQuote{}
	err = st.eachRow(ctx, "quotes", `
		SELECT q.book_id, COALESCE(q.page, 0), q.text, q.comment, q.created_at, q.updated_at
		  FROM books_quotes q JOIN books_books b ON b.id = q.book_id
		 WHERE b.user_id = ? ORDER BY q.created_at, q.id`, uid, func(rows *sql.Rows) error {
		var bid int64
		var q exportedQuote
		var created, updated string
		if err := rows.Scan(&bid, &q.Page, &q.Text, &q.Comment, &created, &updated); err != nil {
			return err
		}
		var err error
		q.CreatedAt, q.UpdatedAt, err = parseStamps(created, updated)
		quotes[bid] = append(quotes[bid], q)
		return err
	})
	if err != nil {
		return out, err
	}

	tags := map[int64][]string{}
	err = st.eachRow(ctx, "tags", `
		SELECT x.book_id, t.name FROM books_book_tags x JOIN books_tags t ON t.id = x.tag_id
		 WHERE t.user_id = ? ORDER BY t.name`, uid, func(rows *sql.Rows) error {
		var bid int64
		var name string
		err := rows.Scan(&bid, &name)
		tags[bid] = append(tags[bid], name)
		return err
	})
	if err != nil {
		return out, err
	}

	err = st.eachRow(ctx, "books", `
		SELECT b.id, b.title, b.subtitle, b.authors, COALESCE(b.year, 0), COALESCE(b.pages, 0),
		       COALESCE(b.isbn13, ''), b.ol_work_id, b.ol_edition_id, b.description, b.series_name,
		       b.series_number, COALESCE(b.rating, 0), b.review, `+shelfExpr+`, b.added_at, b.updated_at
		  FROM books_books b `+latestJoin+`
		 WHERE b.user_id = ? ORDER BY b.added_at, b.id`, uid, func(rows *sql.Rows) error {
		var id int64
		var b exportedBook
		var added, updated string
		if err := rows.Scan(&id, &b.Title, &b.Subtitle, &b.Authors, &b.Year, &b.Pages, &b.ISBN13, &b.OLWorkID,
			&b.OLEditionID, &b.Description, &b.SeriesName, &b.SeriesNumber, &b.Rating, &b.Review, &b.Shelf,
			&added, &updated); err != nil {
			return err
		}
		var err error
		b.AddedAt, b.UpdatedAt, err = parseStamps(added, updated)
		b.Tags = append([]string{}, tags[id]...)
		b.Readings = append([]exportedReading{}, readings[id]...)
		b.Notes = append([]exportedNote{}, notes[id]...)
		b.Quotes = append([]exportedQuote{}, quotes[id]...)
		out.Books = append(out.Books, b)
		return err
	})
	if err != nil {
		return out, err
	}

	err = st.eachRow(ctx, "goals", `SELECT year, target FROM books_goals WHERE user_id = ? ORDER BY year`, uid,
		func(rows *sql.Rows) error {
			var g exportedGoal
			err := rows.Scan(&g.Year, &g.Target)
			out.Goals = append(out.Goals, g)
			return err
		})
	return out, err
}
