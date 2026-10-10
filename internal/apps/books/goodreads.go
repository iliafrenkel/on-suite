package books

import (
	"encoding/csv"
	"errors"
	"fmt"
	"html"
	"io"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// MaxImportBytes bounds an uploaded Goodreads export (decided 2026-10-10
// while planning B5): a library of a few thousand books with long
// reviews is a few megabytes, so ten is room to spare.
const MaxImportBytes = 10 << 20

// MaxReadCount is the largest Read Count a row may give. More is a broken
// file, not a reader: each read past the first becomes a reading.
const MaxReadCount = 100

// ImportBook is one row of a Goodreads "Export Library" CSV, mapped to ON
// Books (spec "Import (B5)"). BookInput is already normalized and valid.
type ImportBook struct {
	Line int // where the row starts in the file, for messages
	BookInput
	Rating int    // 1–5, 0 for none
	Review string // plain text, line breaks kept
	// Note is the row's Private Notes, plain text: one book note, dated
	// Date Added. "" for none.
	Note string
	// Shelf is where the book goes: ShelfRead (a finished reading dated
	// FinishedOn), ShelfReading (a reading started on StartedOn),
	// ShelfDNF (a reading not finished, stopped on FinishedOn) or
	// ShelfWant (no reading).
	Shelf      Shelf
	FinishedOn string // YYYY-MM-DD, "" when Goodreads has no Date Read
	StartedOn  string // YYYY-MM-DD, the Date Added of a book being read
	// EarlierReads is how many finished readings with no dates come
	// before the main one: Read Count less one.
	EarlierReads int
	Format       string   // the main reading's: "", "paper", "ebook" or "audio"
	Tags         []string // clean
	AddedOn      string   // YYYY-MM-DD, "" when the row has no Date Added
}

// ImportError is a file the import refuses. Line is where in the file,
// 0 for the file as a whole; Error is the message shown to the person.
type ImportError struct {
	Line int
	Msg  string
}

func (e *ImportError) Error() string {
	if e.Line == 0 {
		return e.Msg
	}
	return fmt.Sprintf("Line %d: %s", e.Line, e.Msg)
}

// Unwrap makes an ImportError an ErrInvalid for errors.Is.
func (e *ImportError) Unwrap() error { return ErrInvalid }

// Goodreads' export columns this import reads, by header name: the
// header row is looked up, not counted, so an older export with extra
// columns reads the same.
const (
	grTitle      = "Title"
	grAuthor     = "Author"
	grMoreAuth   = "Additional Authors"
	grISBN       = "ISBN"
	grISBN13     = "ISBN13"
	grRating     = "My Rating"
	grBinding    = "Binding"
	grPages      = "Number of Pages"
	grYear       = "Year Published"
	grOrigYear   = "Original Publication Year"
	grDateRead   = "Date Read"
	grDateAdded  = "Date Added"
	grShelves    = "Bookshelves"
	grExclusive  = "Exclusive Shelf"
	grReview     = "My Review"
	grNotes      = "Private Notes"
	grReadCount  = "Read Count"
	grDateLayout = "2006/01/02"
)

// grSeries is Goodreads' way of putting a series into a title:
// "Leviathan Wakes (The Expanse, #1)". A title with several series, or a
// number that isn't one ("#1-3"), is left as it is.
var grSeries = regexp.MustCompile(`^(.+?)\s*\(([^()#;]+?),\s*#(\d+(?:\.\d+)?)\)$`)

// grBindings maps a Binding to a format where it is obvious (spec "Import
// (B5)"); anything else is no format.
var grBindings = map[string]string{
	"kindle edition": "ebook", "ebook": "ebook", "nook": "ebook",
	"audible audio": "audio", "audio cd": "audio", "audiobook": "audio", "audio cassette": "audio", "mp3 cd": "audio",
	"paperback": "paper", "hardcover": "paper", "mass market paperback": "paper",
}

// grBuiltIn are Goodreads' three built-in exclusive shelves, which become
// shelves here, never tags.
var grBuiltIn = map[string]Shelf{"read": ShelfRead, "currently-reading": ShelfReading, "to-read": ShelfWant}

// grDNF are the shelf names people give books they gave up on (decided
// 2026-10-10 while planning B5): as the Exclusive Shelf or among the
// other shelves, any case, they make the book a DNF, not a tag.
var grDNF = map[string]bool{"dnf": true, "did-not-finish": true, "abandoned": true}

// fieldLabels names a book field by its Goodreads column, for messages.
var fieldLabels = map[string]string{"title": grTitle, "subtitle": grTitle, "authors": grAuthor,
	"year": grOrigYear, "pages": grPages, "isbn": grISBN13, "series_name": grTitle, "series_number": grTitle}

// ParseGoodreads reads a whole Goodreads library export before anything is
// written (spec "Import (B5)"), so a bad file leaves nothing behind. Any
// problem is an *ImportError naming the line it is on.
func ParseGoodreads(r io.Reader) ([]ImportBook, error) {
	cr := csv.NewReader(r)
	header, err := cr.Read()
	if errors.Is(err, io.EOF) {
		return nil, &ImportError{Msg: "That file is empty."}
	}
	if err != nil {
		return nil, csvError(err)
	}
	cols := map[string]int{}
	for i, name := range header {
		cols[strings.TrimSpace(strings.TrimPrefix(name, "\uFEFF"))] = i
	}
	for _, need := range []string{grTitle, grAuthor, grExclusive} {
		if _, ok := cols[need]; !ok {
			return nil, &ImportError{Msg: "That doesn't look like a Goodreads library export: it has no “" + need + "” column."}
		}
	}
	var out []ImportBook
	for {
		rec, err := cr.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, csvError(err)
		}
		line, _ := cr.FieldPos(0)
		get := func(name string) string {
			if i, ok := cols[name]; ok && i < len(rec) {
				return strings.TrimSpace(rec[i])
			}
			return ""
		}
		b, msg := grRow(get)
		if msg != "" {
			return nil, &ImportError{Line: line, Msg: msg}
		}
		b.Line = line
		out = append(out, b)
	}
	if len(out) == 0 {
		return nil, &ImportError{Msg: "That file has no books in it."}
	}
	return out, nil
}

// csvError words encoding/csv's errors for people.
func csvError(err error) error {
	var pe *csv.ParseError
	if errors.As(err, &pe) {
		return &ImportError{Line: pe.Line, Msg: "this line can't be read as CSV (" + pe.Err.Error() + ")."}
	}
	return &ImportError{Msg: "That file can't be read."}
}

// grRow maps one row (spec "Import (B5)": the mapping). get returns a
// column's trimmed text, "" when the file has no such column. A message
// is for the person, and means the row can't be imported.
func grRow(get func(string) string) (ImportBook, string) {
	var b ImportBook
	b.Title, b.SeriesName, b.SeriesNumber = grTitleSeries(get(grTitle))
	b.Authors = get(grAuthor)
	if more := get(grMoreAuth); more != "" {
		b.Authors += ", " + more
	}
	b.ISBN = grISBNOf(get(grISBN13), get(grISBN))
	b.Year = grYearOf(get(grOrigYear), get(grYear))
	var msg string
	if b.Pages, msg = grNumber(get(grPages), grPages, MaxPages); msg != "" {
		return b, msg
	}
	b.BookInput = b.BookInput.Normalize()
	if errs := b.BookInput.Validate(); errs != nil {
		return b, fieldMessage(errs)
	}

	if b.Rating, msg = grNumber(get(grRating), grRating, 5); msg != "" {
		return b, msg
	}
	b.Review = grReviewText(get(grReview))
	if utf8.RuneCountInString(b.Review) > MaxReviewRunes {
		return b, fmt.Sprintf("%s: Keep it to %d characters or fewer.", grReview, MaxReviewRunes)
	}
	b.Note = grReviewText(get(grNotes))
	if utf8.RuneCountInString(b.Note) > MaxNoteRunes {
		return b, fmt.Sprintf("%s: Keep it to %d characters or fewer.", grNotes, MaxNoteRunes)
	}
	if b.AddedOn, msg = grDate(get(grDateAdded), grDateAdded); msg != "" {
		return b, msg
	}
	read, msg := grDate(get(grDateRead), grDateRead)
	if msg != "" {
		return b, msg
	}
	count, msg := grNumber(get(grReadCount), grReadCount, MaxReadCount)
	if msg != "" {
		return b, msg
	}

	exclusive := strings.ToLower(get(grExclusive))
	others := strings.Split(get(grShelves), ",")
	shelf, builtIn := grBuiltIn[exclusive]
	if !builtIn {
		shelf = ShelfWant // a shelf of the person's own: no reading, and a tag
	}
	for _, name := range append(others, exclusive) {
		if grDNF[strings.ToLower(strings.TrimSpace(name))] {
			shelf = ShelfDNF // given up on, whatever else it says
		}
	}
	b.Shelf = shelf
	switch shelf {
	case ShelfRead, ShelfDNF:
		b.FinishedOn = read
	case ShelfReading:
		b.StartedOn = b.AddedOn
	}
	if shelf != ShelfWant {
		b.EarlierReads = max(0, count-1)
		b.Format = grBindings[strings.ToLower(get(grBinding))]
	}

	var tags []string
	for _, name := range others {
		lower := strings.ToLower(strings.TrimSpace(name))
		if _, ok := grBuiltIn[lower]; !ok && !grDNF[lower] {
			tags = append(tags, name)
		}
	}
	if !builtIn && exclusive != "" && !grDNF[exclusive] {
		tags = append(tags, exclusive)
	}
	b.Tags = ParseTags(strings.Join(tags, ","))
	return b, ""
}

// grTitleSeries splits "Leviathan Wakes (The Expanse, #1)" into the title,
// the series and its number.
func grTitleSeries(s string) (title, series, number string) {
	if m := grSeries.FindStringSubmatch(s); m != nil {
		return m[1], m[2], m[3]
	}
	return s, "", ""
}

// grISBNOf is the row's ISBN-13: its ISBN13, else its ISBN converted, else
// none. Goodreads wraps both as ="…" so spreadsheets keep the digits.
func grISBNOf(isbn13, isbn10 string) string {
	for _, s := range []string{isbn13, isbn10} {
		s = strings.TrimSuffix(strings.TrimPrefix(s, `="`), `"`)
		if v, ok := ISBN13(s); ok && s != "" {
			return v
		}
	}
	return ""
}

// grYearOf is the Original Publication Year, else the Year Published,
// whichever is a year ON Books keeps (1 to MaxYear: Goodreads gives
// ancient books negative years).
func grYearOf(years ...string) int {
	for _, s := range years {
		if y, err := strconv.Atoi(s); err == nil && y >= 1 && y <= MaxYear {
			return y
		}
	}
	return 0
}

// grNumber reads a whole number from 0 to most; "" is 0.
func grNumber(s, column string, most int) (int, string) {
	if s == "" {
		return 0, ""
	}
	n, err := strconv.Atoi(s)
	if err != nil || n < 0 || n > most {
		return 0, fmt.Sprintf("%s must be a whole number from 0 to %d.", column, most)
	}
	return n, ""
}

// grDate turns Goodreads' 2024/03/14 into 2024-03-14; "" stays "".
func grDate(s, column string) (string, string) {
	if s == "" {
		return "", ""
	}
	t, err := time.Parse(grDateLayout, s)
	if err != nil || t.Year() < MinYear {
		return "", column + " must be a date like 2024/03/14."
	}
	return t.Format(dayLayout), ""
}

var (
	grBreak = regexp.MustCompile(`(?i)<br\s*/?>`)
	grTag   = regexp.MustCompile(`<[^>]*>`)
)

// grReviewText turns a review's HTML into text (spec "Import (B5)"): <br/>
// becomes a line break, every other tag goes, and entities are decoded.
func grReviewText(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = grBreak.ReplaceAllString(s, "\n")
	s = grTag.ReplaceAllString(s, "")
	return strings.TrimSpace(html.UnescapeString(s))
}

// fieldMessage is the first of a book's field errors, by field name so
// the message is always the same one, named by its Goodreads column.
func fieldMessage(errs FieldErrors) string {
	fields := make([]string, 0, len(errs))
	for f := range errs {
		fields = append(fields, f)
	}
	sort.Strings(fields)
	return fieldLabels[fields[0]] + ": " + errs[fields[0]]
}
