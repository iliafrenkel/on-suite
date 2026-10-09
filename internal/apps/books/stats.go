package books

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// BookPages is a book the Stats page names, with its length.
type BookPages struct {
	ID    int64
	Title string
	Pages int
}

// FormatCount is how many readings finished in a format ("" for not set).
type FormatCount struct {
	Format string
	N      int
}

// YearCount is how many readings finished in a year.
type YearCount struct{ Year, N int }

// YearStats is one year on the Stats page (spec "Stats (B4)").
type YearStats struct {
	Year     int
	Finished int // readings finished in the year; a re-read counts again
	Pages    int // PagesRead over the year's days
	// Rating is the mean rating of the distinct rated books finished in
	// the year; 0 when none is rated.
	Rating float64
	// Formats counts the year's finished readings by format, in Formats
	// order and then not set; a format with none is left out.
	Formats []FormatCount
	// Longest and Shortest are among the books finished in the year that
	// have a page count; a tie goes to the one finished first. Zero ID
	// when there is none.
	Longest, Shortest BookPages
	ByMonth           [12]int // finished readings, January first
}

// AllTime is the Stats page's all-time row.
type AllTime struct {
	Finished int     // every finished reading, dated or not
	Pages    int     // PagesRead over every day
	Rating   float64 // the mean rating of every rated book ever finished
	// ByYear is the dated finishes per year from the first one's year to
	// this year, oldest first, quiet years included; nil when there are none.
	ByYear []YearCount
	// First is the earliest year with a dated finish or a progress update;
	// this year when there is neither.
	First int
}

// Stats is everything the Stats page counts for one year.
type Stats struct {
	Year YearStats
	All  AllTime
}

// finish is one finished reading with what the stats need of its book.
type finish struct {
	BookID        int64
	Title         string
	Pages, Rating int
	Format        string
	FinishedOn    string // "" for an undated (imported) reading
}

// Stats counts userID's reading for year, and all time (spec "Stats
// (B4)", "Derived values"). The counting is done here in Go, over every
// finished reading and every progress row: one household's reading is
// small, and the pages-read rules don't fit SQL well.
func (st *Store) Stats(ctx context.Context, userID int64, year int) (Stats, error) {
	fs, err := st.finishes(ctx, userID)
	if err != nil {
		return Stats{}, err
	}
	logs, err := st.readingLogs(ctx, userID)
	if err != nil {
		return Stats{}, err
	}
	return summarise(fs, logs, year, st.now().Local().Year()), nil
}

// finishes is every finished reading of userID's, in finish order
// (undated first).
func (st *Store) finishes(ctx context.Context, userID int64) ([]finish, error) {
	rows, err := st.db.QueryContext(ctx, `
		SELECT b.id, b.title, COALESCE(b.pages, 0), COALESCE(b.rating, 0), COALESCE(r.format, ''),
		       COALESCE(r.finished_on, '')
		  FROM books_readings r JOIN books_books b ON b.id = r.book_id
		 WHERE b.user_id = ? AND r.status = 'finished'
		 ORDER BY r.finished_on, r.id`, userID)
	if err != nil {
		return nil, fmt.Errorf("books: finishes: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []finish
	for rows.Next() {
		var f finish
		if err := rows.Scan(&f.BookID, &f.Title, &f.Pages, &f.Rating, &f.Format, &f.FinishedOn); err != nil {
			return nil, fmt.Errorf("books: scan finish: %w", err)
		}
		out = append(out, f)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("books: finishes: %w", err)
	}
	return out, nil
}

// readingLogs is every reading of userID's with its progress, for
// PagesRead. Each row's day is its recorded_at in the local zone.
func (st *Store) readingLogs(ctx context.Context, userID int64) ([]ReadingLog, error) {
	rows, err := st.db.QueryContext(ctx, `
		SELECT r.id, r.status, COALESCE(r.finished_on, ''), COALESCE(b.pages, 0), p.page, p.percent, p.recorded_at
		  FROM books_readings r JOIN books_books b ON b.id = r.book_id
		  LEFT JOIN books_progress p ON p.reading_id = r.id
		 WHERE b.user_id = ?
		 ORDER BY r.id, p.recorded_at, p.id`, userID)
	if err != nil {
		return nil, fmt.Errorf("books: reading logs: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []ReadingLog
	var last int64
	for rows.Next() {
		var id int64
		var status, finishedOn string
		var pages int
		var page, percent sql.NullInt64
		var at sql.NullString
		if err := rows.Scan(&id, &status, &finishedOn, &pages, &page, &percent, &at); err != nil {
			return nil, fmt.Errorf("books: scan reading log: %w", err)
		}
		if len(out) == 0 || id != last {
			out = append(out, ReadingLog{Pages: pages, Status: Status(status), FinishedOn: finishedOn})
			last = id
		}
		p, err := scanProgress(page, percent, at)
		if err != nil {
			return nil, err
		}
		if p.Set() {
			r := &out[len(out)-1]
			r.Steps = append(r.Steps, Step{Unit: p.Unit, Value: p.Value, Day: p.RecordedAt.Local().Format(dayLayout)})
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("books: reading logs: %w", err)
	}
	return out, nil
}

// summarise counts the finishes and the pages read for year and all time.
// thisYear ends ByYear.
func summarise(fs []finish, logs []ReadingLog, year, thisYear int) Stats {
	s := Stats{Year: YearStats{Year: year}, All: AllTime{First: thisYear}}
	yearRated, allRated := map[int64]int{}, map[int64]int{}
	formats := map[string]int{}
	perYear := map[int]int{}
	firstFinish := 0
	for _, f := range fs {
		s.All.Finished++
		if f.Rating > 0 {
			allRated[f.BookID] = f.Rating
		}
		day, err := time.Parse(dayLayout, f.FinishedOn)
		if err != nil { // undated: all time only
			continue
		}
		if day.Year() < MinYear { // counted in All.Finished, but no year of its own
			continue
		}
		perYear[day.Year()]++
		if firstFinish == 0 || day.Year() < firstFinish {
			firstFinish = day.Year()
		}
		if day.Year() != year {
			continue
		}
		y := &s.Year
		y.Finished++
		y.ByMonth[day.Month()-1]++
		formats[f.Format]++
		if f.Rating > 0 {
			yearRated[f.BookID] = f.Rating
		}
		if f.Pages > 0 {
			book := BookPages{ID: f.BookID, Title: f.Title, Pages: f.Pages}
			if y.Longest.ID == 0 || f.Pages > y.Longest.Pages {
				y.Longest = book
			}
			if y.Shortest.ID == 0 || f.Pages < y.Shortest.Pages {
				y.Shortest = book
			}
		}
	}
	for _, f := range append(append([]string{}, Formats...), "") {
		if n := formats[f]; n > 0 {
			s.Year.Formats = append(s.Year.Formats, FormatCount{Format: f, N: n})
		}
	}
	s.Year.Rating, s.All.Rating = mean(yearRated), mean(allRated)
	if firstFinish != 0 {
		for y := firstFinish; y <= thisYear; y++ {
			s.All.ByYear = append(s.All.ByYear, YearCount{Year: y, N: perYear[y]})
		}
		s.All.First = min(s.All.First, firstFinish)
	}
	prefix := strconv.Itoa(year) + "-"
	for _, r := range logs {
		for day, n := range PagesRead(r) {
			s.All.Pages += n
			if strings.HasPrefix(day, prefix) {
				s.Year.Pages += n
			}
			if y, err := strconv.Atoi(day[:4]); err == nil && y >= MinYear {
				s.All.First = min(s.All.First, y)
			}
		}
	}
	return s
}

// mean is the average of a set of ratings, 0 for none.
func mean(ratings map[int64]int) float64 {
	if len(ratings) == 0 {
		return 0
	}
	sum := 0
	for _, r := range ratings {
		sum += r
	}
	return float64(sum) / float64(len(ratings))
}
