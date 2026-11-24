package books_test

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/iliafrenkel/on-suite/internal/apps/books"
	"github.com/iliafrenkel/on-suite/internal/platform/app"
)

// toolBookOut is the result of a write tool.
type toolBookOut struct {
	ID         int64  `json:"id"`
	Shelf      string `json:"shelf"`
	Rating     int    `json:"rating"`
	Progress   string `json:"progress"`
	FinishedOn string `json:"finished_on"`
}

func idArgs(id int64) string { return `{"id":` + strconv.FormatInt(id, 10) + `}` }

// callOK runs a tool that must succeed and decodes its book.
func (tf *toolFixture) callOK(t *testing.T, name string, userID int64, args string) toolBookOut {
	t.Helper()
	out, err := tf.call(t, name, userID, args)
	if err != nil {
		t.Fatalf("%s %s: %v", name, args, err)
	}
	var b toolBookOut
	if err := json.Unmarshal([]byte(out), &b); err != nil {
		t.Fatalf("%v in %s", err, out)
	}
	return b
}

// wantToolErr fails unless the call is refused with exactly msg.
func (tf *toolFixture) wantToolErr(t *testing.T, name string, userID int64, args, msg string) {
	t.Helper()
	_, err := tf.call(t, name, userID, args)
	var te app.ToolError
	if !errors.As(err, &te) || te.Msg != msg {
		t.Errorf("%s %s: err = %v, want ToolError %q", name, args, err, msg)
	}
}

// reading adds a book being read, with the given page count.
func (tf *toolFixture) reading(t *testing.T, title string, pages int) int64 {
	t.Helper()
	return add(t, tf.s, tf.s.Alice.User.ID, books.NewBook{
		BookInput: books.BookInput{Title: title, Pages: pages}, Shelf: books.ShelfReading,
	})
}

func (tf *toolFixture) shelf(t *testing.T, userID, id int64) books.Shelf {
	t.Helper()
	b, err := tf.s.Store.Get(context.Background(), userID, id)
	if err != nil {
		t.Fatal(err)
	}
	return b.Shelf
}

// dayFromToday is the store's today plus n days, YYYY-MM-DD.
func (tf *toolFixture) dayFromToday(t *testing.T, n int) string {
	t.Helper()
	d, err := time.Parse("2006-01-02", tf.s.Store.Today())
	if err != nil {
		t.Fatal(err)
	}
	return d.AddDate(0, 0, n).Format("2006-01-02")
}

func TestBooksStartReading(t *testing.T) {
	tf := newToolFixture(t)
	uid := tf.s.Alice.User.ID
	id := add(t, tf.s, uid, onShelf("Dune", books.ShelfWant))
	if got := tf.callOK(t, "books_start_reading", uid, idArgs(id)); got.Shelf != "reading" {
		t.Errorf("shelf = %q, want reading", got.Shelf)
	}
	tf.wantToolErr(t, "books_start_reading", uid, idArgs(id), "You're already reading this book.")
}

func TestBooksWriteOtherUsersBookIsNotFound(t *testing.T) {
	tf := newToolFixture(t)
	bob := tf.s.Bob.User.ID
	id := add(t, tf.s, bob, books.NewBook{BookInput: books.BookInput{Title: "Secret", Pages: 100}, Shelf: books.ShelfReading})
	alice := tf.s.Alice.User.ID
	for name, args := range map[string]string{
		"books_start_reading":   idArgs(id),
		"books_update_progress": `{"id":` + strconv.FormatInt(id, 10) + `,"page":5}`,
		"books_finish":          idArgs(id),
		"books_mark_dnf":        idArgs(id),
	} {
		tf.wantToolErr(t, name, alice, args, "No book with that ID on your shelves")
	}
	if got := tf.shelf(t, bob, id); got != books.ShelfReading {
		t.Errorf("Bob's book moved to %q", got)
	}
}

func TestBooksUpdateProgressPage(t *testing.T) {
	tf := newToolFixture(t)
	id := tf.reading(t, "Dune", 412)
	got := tf.callOK(t, "books_update_progress", tf.s.Alice.User.ID, `{"id":`+strconv.FormatInt(id, 10)+`,"page":120}`)
	if got.Progress != "p. 120/412" {
		t.Errorf("progress = %q, want p. 120/412", got.Progress)
	}
}

func TestBooksUpdateProgressConverts(t *testing.T) {
	tf := newToolFixture(t)
	uid := tf.s.Alice.User.ID
	paper := tf.reading(t, "Dune", 400)
	got := tf.callOK(t, "books_update_progress", uid, `{"id":`+strconv.FormatInt(paper, 10)+`,"percent":25}`)
	if got.Progress != "p. 100/400" {
		t.Errorf("paper progress = %q, want p. 100/400", got.Progress)
	}
	audio := tf.reading(t, "Hyperion", 400)
	if err := tf.s.Store.SetFormat(context.Background(), uid, audio, "audio"); err != nil {
		t.Fatal(err)
	}
	got = tf.callOK(t, "books_update_progress", uid, `{"id":`+strconv.FormatInt(audio, 10)+`,"page":100}`)
	if got.Progress != "25%" {
		t.Errorf("audio progress = %q, want 25%%", got.Progress)
	}
}

func TestBooksUpdateProgressNoPageCount(t *testing.T) {
	tf := newToolFixture(t)
	id := tf.reading(t, "Mystery", 0)
	tf.wantToolErr(t, "books_update_progress", tf.s.Alice.User.ID,
		`{"id":`+strconv.FormatInt(id, 10)+`,"page":50}`, "This book has no page count, so give a percentage.")
}

func TestBooksUpdateProgressNeedsOneValue(t *testing.T) {
	tf := newToolFixture(t)
	uid := tf.s.Alice.User.ID
	id := strconv.FormatInt(tf.reading(t, "Dune", 412), 10)
	for _, args := range []string{`{"id":` + id + `}`, `{"id":` + id + `,"page":5,"percent":5}`} {
		tf.wantToolErr(t, "books_update_progress", uid, args, "Give either page or percent.")
	}
}

func TestBooksUpdateProgressOutOfRange(t *testing.T) {
	tf := newToolFixture(t)
	uid := tf.s.Alice.User.ID
	id := strconv.FormatInt(tf.reading(t, "Dune", 412), 10)
	tf.wantToolErr(t, "books_update_progress", uid, `{"id":`+id+`,"page":999}`, "Enter a page from 0 to 412.")
	tf.wantToolErr(t, "books_update_progress", uid, `{"id":`+id+`,"percent":101}`, "Enter a percentage from 0 to 100.")
}

func TestBooksWriteNotBeingRead(t *testing.T) {
	tf := newToolFixture(t)
	uid := tf.s.Alice.User.ID
	id := strconv.FormatInt(add(t, tf.s, uid, books.NewBook{BookInput: books.BookInput{Title: "Dune", Pages: 100}, Shelf: books.ShelfWant}), 10)
	for name, args := range map[string]string{
		"books_update_progress": `{"id":` + id + `,"page":5}`,
		"books_finish":          `{"id":` + id + `}`,
		"books_mark_dnf":        `{"id":` + id + `}`,
	} {
		tf.wantToolErr(t, name, uid, args, "This book isn't being read right now.")
	}
}

func TestBooksFinishRatesAndDates(t *testing.T) {
	tf := newToolFixture(t)
	uid := tf.s.Alice.User.ID
	id := tf.reading(t, "Dune", 412)
	// The reading started "today"; let two days pass so yesterday is after it.
	tf.s.Clock.Advance(48 * time.Hour)
	yesterday := tf.dayFromToday(t, -1)
	got := tf.callOK(t, "books_finish", uid, `{"id":`+strconv.FormatInt(id, 10)+`,"rating":4,"day":"`+yesterday+`"}`)
	if got.Shelf != "read" || got.Rating != 4 || got.FinishedOn != yesterday {
		t.Errorf("finish = %+v, want read, rating 4, finished %s", got, yesterday)
	}
}

func TestBooksFinishBadRatingOrDay(t *testing.T) {
	tf := newToolFixture(t)
	uid := tf.s.Alice.User.ID
	id := tf.reading(t, "Dune", 412)
	sid := strconv.FormatInt(id, 10)
	tf.wantToolErr(t, "books_finish", uid, `{"id":`+sid+`,"rating":6}`, "rating must be a whole number from 1 to 5.")
	tf.wantToolErr(t, "books_finish", uid, `{"id":`+sid+`,"rating":-1}`, "rating must be a whole number from 1 to 5.")
	tf.wantToolErr(t, "books_finish", uid, `{"id":`+sid+`,"day":"`+tf.dayFromToday(t, 1)+`"}`, "That date is in the future.")
	if got := tf.shelf(t, uid, id); got != books.ShelfReading {
		t.Errorf("shelf = %q after refused finishes, want reading", got)
	}
}

func TestBooksMarkDNF(t *testing.T) {
	tf := newToolFixture(t)
	id := tf.reading(t, "Dune", 412)
	if got := tf.callOK(t, "books_mark_dnf", tf.s.Alice.User.ID, idArgs(id)); got.Shelf != "dnf" {
		t.Errorf("shelf = %q, want dnf", got.Shelf)
	}
}

// idOut is a note or quote tool's result.
type idOut struct {
	ID     int64 `json:"id"`
	BookID int64 `json:"book_id"`
	Page   int   `json:"page"`
}

func (tf *toolFixture) callID(t *testing.T, name string, userID int64, args string) idOut {
	t.Helper()
	out, err := tf.call(t, name, userID, args)
	if err != nil {
		t.Fatalf("%s %s: %v", name, args, err)
	}
	var r idOut
	if err := json.Unmarshal([]byte(out), &r); err != nil {
		t.Fatalf("%v in %s", err, out)
	}
	return r
}

func TestBooksAddNote(t *testing.T) {
	tf := newToolFixture(t)
	uid := tf.s.Alice.User.ID
	id := tf.reading(t, "Dune", 412)
	r := tf.callID(t, "books_add_note", uid, `{"id":`+strconv.FormatInt(id, 10)+`,"text":"Loved the ending","page":12}`)
	if r.ID <= 0 || r.BookID != id || r.Page != 12 {
		t.Errorf("result = %+v, want id > 0, book %d, page 12", r, id)
	}
	notes, err := tf.s.Store.Notes(context.Background(), uid, id)
	if err != nil || len(notes) != 1 || notes[0].ID != r.ID || notes[0].Body != "Loved the ending" || notes[0].Page != 12 {
		t.Errorf("notes = %+v, %v", notes, err)
	}
}

func TestBooksAddQuote(t *testing.T) {
	tf := newToolFixture(t)
	uid := tf.s.Alice.User.ID
	id := tf.reading(t, "Dune", 412)
	r := tf.callID(t, "books_add_quote", uid, `{"id":`+strconv.FormatInt(id, 10)+`,"text":"Fear is the mind-killer."}`)
	if r.ID <= 0 || r.BookID != id || r.Page != 0 {
		t.Errorf("result = %+v, want id > 0, book %d, page 0", r, id)
	}
	quotes, err := tf.s.Store.Quotes(context.Background(), uid, id)
	if err != nil || len(quotes) != 1 || quotes[0].ID != r.ID || quotes[0].Text != "Fear is the mind-killer." || quotes[0].Page != 0 {
		t.Errorf("quotes = %+v, %v", quotes, err)
	}
}

func TestBooksAddNoteRefusals(t *testing.T) {
	tf := newToolFixture(t)
	alice, bob := tf.s.Alice.User.ID, tf.s.Bob.User.ID
	id := tf.reading(t, "Dune", 412)
	bobs := add(t, tf.s, bob, books.NewBook{BookInput: books.BookInput{Title: "Secret", Pages: 100}, Shelf: books.ShelfReading})
	sid := strconv.FormatInt(id, 10)
	tf.wantToolErr(t, "books_add_note", alice, `{"id":`+sid+`,"text":"   "}`, "Write something in the note first.")
	tf.wantToolErr(t, "books_add_note", alice, `{"id":`+sid+`,"text":"x","page":999}`, "Enter a page from 1 to 412.")
	tf.wantToolErr(t, "books_add_note", alice, `{"id":`+strconv.FormatInt(bobs, 10)+`,"text":"x"}`, "No book with that ID on your shelves")
	for _, uid := range []int64{alice, bob} {
		for _, b := range []int64{id, bobs} {
			if notes, err := tf.s.Store.Notes(context.Background(), uid, b); err == nil && len(notes) != 0 {
				t.Errorf("user %d book %d has %d notes, want none", uid, b, len(notes))
			}
		}
	}
}

func TestBooksAddQuoteTooLong(t *testing.T) {
	tf := newToolFixture(t)
	uid := tf.s.Alice.User.ID
	id := tf.reading(t, "Dune", 412)
	long := strings.Repeat("a", 5001)
	tf.wantToolErr(t, "books_add_quote", uid, `{"id":`+strconv.FormatInt(id, 10)+`,"text":"`+long+`"}`, "Keep the quote to 5000 characters or fewer.")
	if quotes, err := tf.s.Store.Quotes(context.Background(), uid, id); err != nil || len(quotes) != 0 {
		t.Errorf("quotes = %+v, %v, want none", quotes, err)
	}
}
