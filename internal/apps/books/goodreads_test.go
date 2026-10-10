package books_test

import (
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/iliafrenkel/on-suite/internal/apps/books"
)

// grFixture is a real Goodreads "Export Library" header with rows that
// are awkward on purpose (spec "Testing"): quoted commas and line breaks,
// ="…" ISBNs and an empty one, Read Count 2, a Kindle and an Audible
// binding, rating 0, a series in a title, a negative year, a shelf of the
// person's own that means "gave up" (did-not-finish), Private Notes, and
// the same book twice.
func grFixture(t *testing.T) []byte {
	t.Helper()
	data, err := os.ReadFile("testdata/goodreads_library_export.csv")
	if err != nil {
		t.Fatal(err)
	}
	return data
}

const grHeader = "Book Id,Title,Author,Author l-f,Additional Authors,ISBN,ISBN13,My Rating,Average Rating,Publisher,Binding,Number of Pages,Year Published,Original Publication Year,Date Read,Date Added,Bookshelves,Bookshelves with positions,Exclusive Shelf,My Review,Spoiler,Private Notes,Read Count,Owned Copies\n"

// grLine is one row of the export with the columns a test cares about;
// the rest are Goodreads' usual.
func grLine(title, rating, binding, dateRead, shelves, exclusive, review, readCount string) string {
	q := func(s string) string { return `"` + strings.ReplaceAll(s, `"`, `""`) + `"` }
	return strings.Join([]string{"1", q(title), "Ann Author", q("Author, Ann"), "", `"="""""`, `"="""""`, rating, "4.00",
		"Pub", binding, "300", "2001", "2000", dateRead, "2024/01/02", q(shelves), "", exclusive, q(review), "", "", readCount, "0"}, ",") + "\n"
}

func parse(t *testing.T, csv string) []books.ImportBook {
	t.Helper()
	rows, err := books.ParseGoodreads(strings.NewReader(csv))
	if err != nil {
		t.Fatal(err)
	}
	return rows
}

func TestParseGoodreadsMapsTheFixture(t *testing.T) {
	rows, err := books.ParseGoodreads(strings.NewReader(string(grFixture(t))))
	if err != nil {
		t.Fatal(err)
	}
	goodOmens := books.ImportBook{Line: 4, BookInput: books.BookInput{
		Title: "Good Omens: The Nice and Accurate Prophecies of Agnes Nutter, Witch", Authors: "Terry Pratchett, Neil Gaiman",
		Year: 1990, Pages: 491}, Shelf: books.ShelfWant, AddedOn: "2025-06-30"}
	want := []books.ImportBook{
		{Line: 2, BookInput: books.BookInput{Title: "Leviathan Wakes", Authors: "James S.A. Corey", Year: 2011, Pages: 592,
			ISBN: "9780316129084", SeriesName: "The Expanse", SeriesNumber: "1"},
			Rating: 4, Review: "Great fun.\n\nLoved Miller & Holden.", Note: "Lent to Sam in May.", Shelf: books.ShelfRead, FinishedOn: "2024-03-14",
			EarlierReads: 1, Format: "paper", Tags: []string{"sf", "space"}, AddedOn: "2024-02-01"},
		{Line: 3, BookInput: books.BookInput{Title: "Piranesi", Authors: "Susanna Clarke", Year: 2020, Pages: 272, ISBN: "9781635575637"},
			Shelf: books.ShelfReading, StartedOn: "2025-01-05", Format: "ebook", AddedOn: "2025-01-05"},
		goodOmens,
		{Line: 5, BookInput: books.BookInput{Title: "The Remains of the Day", Authors: "Kazuo Ishiguro", Year: 1989, Pages: 245,
			ISBN: "9780679731726"}, Rating: 5, Review: "Stevens on \"dignity\".\nSo quiet, so sad.\n\nThe best ending.",
			Shelf: books.ShelfRead, FinishedOn: "2023-11-02", Format: "audio", Tags: []string{"favourites"}, AddedOn: "2023-10-01"},
		{Line: 9, BookInput: books.BookInput{Title: goodOmens.Title, Authors: goodOmens.Authors, Year: 1990},
			Shelf: books.ShelfWant, AddedOn: "2025-07-01"},
		{Line: 10, BookInput: books.BookInput{Title: "The Odyssey", Authors: "Homer, Robert Fagles", Year: 1999, Pages: 541,
			ISBN: "9780140268867"}, Shelf: books.ShelfWant, Tags: []string{"classics"}, AddedOn: "2022-08-15"},
		{Line: 11, BookInput: books.BookInput{Title: "Infinite Jest", Authors: "David Foster Wallace", Year: 1996, Pages: 1079,
			ISBN: "9780316066525"}, Rating: 2, Shelf: books.ShelfDNF, FinishedOn: "2021-06-01", Format: "paper", AddedOn: "2021-04-02"},
	}
	if len(rows) != len(want) {
		t.Fatalf("%d rows, want %d: %+v", len(rows), len(want), rows)
	}
	for i := range want {
		if !reflect.DeepEqual(rows[i], want[i]) {
			t.Errorf("row %d:\n got %+v\nwant %+v", i, rows[i], want[i])
		}
	}
}

func TestParseGoodreadsBindings(t *testing.T) {
	tests := map[string]string{
		"Kindle Edition": "ebook", "ebook": "ebook", "Nook": "ebook",
		"Audible Audio": "audio", "Audio CD": "audio", "Audiobook": "audio", "MP3 CD": "audio",
		"Paperback": "paper", "Hardcover": "paper", "Mass Market Paperback": "paper",
		"Library Binding": "", "Unknown Binding": "", "": "",
	}
	for binding, want := range tests {
		rows := parse(t, grHeader+grLine("A Book", "0", binding, "2024/05/01", "", "read", "", "1"))
		if rows[0].Format != want {
			t.Errorf("Binding %q = format %q, want %q", binding, rows[0].Format, want)
		}
	}
	// A book still to read has no reading, so no format.
	if rows := parse(t, grHeader+grLine("A Book", "0", "Paperback", "", "", "to-read", "", "0")); rows[0].Format != "" {
		t.Errorf("to-read format = %q, want none", rows[0].Format)
	}
}

func TestParseGoodreadsReadCounts(t *testing.T) {
	tests := []struct {
		exclusive, count string
		want             int
	}{
		{"read", "", 0},
		{"read", "0", 0}, // Goodreads says 0 for some books on read
		{"read", "1", 0},
		{"read", "3", 2},
		{"currently-reading", "2", 1}, // read once before, and again now
		{"to-read", "2", 0},           // no readings on Want to read
	}
	for _, tt := range tests {
		rows := parse(t, grHeader+grLine("A Book", "0", "", "", "", tt.exclusive, "", tt.count))
		if rows[0].EarlierReads != tt.want {
			t.Errorf("%s with Read Count %q: %d earlier readings, want %d", tt.exclusive, tt.count, rows[0].EarlierReads, tt.want)
		}
	}
}

func TestParseGoodreadsReviews(t *testing.T) {
	tests := map[string]string{
		"One.<br/>Two.<br />Three.<BR>Four.":     "One.\nTwo.\nThree.\nFour.",
		"<b>Bold</b> and <a href=\"x\">link</a>": "Bold and link",
		"Fish &amp; chips &lt;3":                 "Fish & chips <3",
		"  <br/>Trimmed<br/>  ":                  "Trimmed",
	}
	for review, want := range tests {
		rows := parse(t, grHeader+grLine("A Book", "0", "", "", "", "read", review, "1"))
		if rows[0].Review != want {
			t.Errorf("review %q = %q, want %q", review, rows[0].Review, want)
		}
	}
}

func TestParseGoodreadsTitlesAndTags(t *testing.T) {
	tests := []struct {
		title                  string
		wantTitle, series, num string
	}{
		{"Leviathan Wakes (The Expanse, #1)", "Leviathan Wakes", "The Expanse", "1"},
		{"Edgedancer (The Stormlight Archive, #2.5)", "Edgedancer", "The Stormlight Archive", "2.5"},
		{"Mort (Discworld, #4; Death, #1)", "Mort (Discworld, #4; Death, #1)", "", ""},
		{"The Expanse Omnibus (The Expanse, #1-3)", "The Expanse Omnibus (The Expanse, #1-3)", "", ""},
		{"Dune", "Dune", "", ""},
	}
	for _, tt := range tests {
		b := parse(t, grHeader+grLine(tt.title, "0", "", "", "", "to-read", "", "0"))[0]
		if b.Title != tt.wantTitle || b.SeriesName != tt.series || b.SeriesNumber != tt.num {
			t.Errorf("%q = %q, %q #%q", tt.title, b.Title, b.SeriesName, b.SeriesNumber)
		}
	}
	b := parse(t, grHeader+grLine("A Book", "0", "", "", "Read, to-read, SF, currently-reading, Book Club, sf", "to-read", "", "0"))[0]
	if want := []string{"sf", "book club"}; !reflect.DeepEqual(b.Tags, want) {
		t.Errorf("tags = %q, want %q: Goodreads' own shelves aren't tags", b.Tags, want)
	}
}

func TestParseGoodreadsGaveUpShelves(t *testing.T) {
	tests := []struct {
		name, shelves, exclusive, dateRead string
		wantShelf                          books.Shelf
		wantFinished                       string
		wantTags                           []string
	}{
		{"a DNF exclusive shelf", "DNF, sf", "DNF", "2024/05/01", books.ShelfDNF, "2024-05-01", []string{"sf"}},
		{"abandoned among the others", "Abandoned, sf", "to-read", "", books.ShelfDNF, "", []string{"sf"}},
		{"did-not-finish beats read", "did-not-finish", "read", "2024/05/01", books.ShelfDNF, "2024-05-01", nil},
		{"another shelf of their own", "owned", "owned", "", books.ShelfWant, "", []string{"owned"}},
	}
	for _, tt := range tests {
		b := parse(t, grHeader+grLine("A Book", "0", "Paperback", tt.dateRead, tt.shelves, tt.exclusive, "", "1"))[0]
		if b.Shelf != tt.wantShelf || b.FinishedOn != tt.wantFinished || !reflect.DeepEqual(b.Tags, tt.wantTags) {
			t.Errorf("%s: shelf %s, finished %q, tags %q; want %s, %q, %q", tt.name, b.Shelf, b.FinishedOn, b.Tags,
				tt.wantShelf, tt.wantFinished, tt.wantTags)
		}
	}
}

func TestParseGoodreadsRefusesBadFiles(t *testing.T) {
	tests := []struct {
		name, csv, want string
	}{
		{"empty", "", "That file is empty."},
		{"not an export", "Name,Email\nAnn,ann@example.com\n",
			"That doesn't look like a Goodreads library export: it has no “Title” column."},
		{"header only", grHeader, "That file has no books in it."},
		{"no title", grHeader + grLine("", "0", "", "", "", "to-read", "", "0"), "Line 2: Title: Enter the book's title."},
		{"bad rating", grHeader + grLine("A", "0", "", "", "", "read", "", "1") + grLine("B", "7", "", "", "", "read", "", "1"),
			"Line 3: My Rating must be a whole number from 0 to 5."},
		{"bad date", grHeader + grLine("A", "0", "", "14/03/2024", "", "read", "", "1"),
			"Line 2: Date Read must be a date like 2024/03/14."},
		{"bad read count", grHeader + grLine("A", "0", "", "", "", "read", "", "many"),
			"Line 2: Read Count must be a whole number from 0 to 100."},
		{"too many reads", grHeader + grLine("A", "0", "", "", "", "read", "", "101"),
			"Line 2: Read Count must be a whole number from 0 to 100."},
		{"short row", grHeader + "1,Dune,Frank Herbert\n", "Line 2: this line can't be read as CSV (wrong number of fields)."},
		{"stray quote", grHeader + grLine("A", "0", "", "", "", "read", "", "1") + "1,\"Du\"ne,x\n",
			`Line 3: this line can't be read as CSV (extraneous or missing " in quoted-field).`},
	}
	for _, tt := range tests {
		_, err := books.ParseGoodreads(strings.NewReader(tt.csv))
		var ie *books.ImportError
		if !errors.As(err, &ie) || ie.Error() != tt.want {
			t.Errorf("%s: err = %v, want %q", tt.name, err, tt.want)
		}
		if !errors.Is(err, books.ErrInvalid) {
			t.Errorf("%s: err is not ErrInvalid", tt.name)
		}
	}
}

func TestParseGoodreadsReadsAByteOrderMark(t *testing.T) {
	rows := parse(t, "\uFEFF"+grHeader+grLine("Dune", "0", "", "", "", "to-read", "", "0"))
	if len(rows) != 1 || rows[0].Title != "Dune" {
		t.Errorf("rows = %+v, want Dune", rows)
	}
}
