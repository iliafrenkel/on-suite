package books_test

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/iliafrenkel/on-suite/internal/apps/books"
	"github.com/iliafrenkel/on-suite/internal/apptest"
	"github.com/iliafrenkel/on-suite/internal/platform/app"
)

// newBooksApp is newServer with the app itself, its Open Library client
// pointed at fakeOpenLibrary and the backfill's pause made instant
// (counted in pauses).
func newBooksApp(t *testing.T) (*server, *books.App, *int) {
	t.Helper()
	a := books.New()
	s := apptest.NewServer(t, a, books.NewStore)
	a.UseOpenLibraryForTest(fakeOpenLibrary(t).URL)
	pauses := new(int)
	a.NoPauseForTest(pauses)
	return s, a, pauses
}

// withISBN adds one of userID's books with an ISBN to read.
func withISBN(t *testing.T, s *server, userID int64, title, isbn string) int64 {
	t.Helper()
	nb := titled(title, "", books.ShelfWant)
	nb.ISBN = isbn
	return add(t, s, userID, nb)
}

// checkedAt is a book's cover_checked_at, "" for NULL.
func checkedAt(t *testing.T, s *server, id int64) string {
	t.Helper()
	var at sql.NullString
	if err := s.Store.DBForTest().QueryRow(`SELECT cover_checked_at FROM books_books WHERE id = ?`, id).Scan(&at); err != nil {
		t.Fatal(err)
	}
	return at.String
}

func runBackfill(t *testing.T, a *books.App) error {
	t.Helper()
	jobs := a.Jobs(app.Deps{})
	if len(jobs) != 1 || jobs[0].Name != "fetch book covers" {
		t.Fatalf("jobs = %+v, want the cover backfill", jobs)
	}
	return jobs[0].Run(context.Background())
}

func TestBooksRegistersTheCoverBackfill(t *testing.T) {
	jobs := books.New().Jobs(app.Deps{}) // callable before Mount
	if len(jobs) != 1 {
		t.Fatalf("got %d jobs, want 1", len(jobs))
	}
	j := jobs[0]
	if j.Every != 5*time.Minute || j.Description == "" || j.Run == nil {
		t.Errorf("job = %q every %v, description %q", j.Name, j.Every, j.Description)
	}
}

func TestBackfillFetchesMissingCoversByISBN(t *testing.T) {
	s, a, pauses := newBooksApp(t)
	ctx := context.Background()
	uid := s.Alice.User.ID
	found := withISBN(t, s, uid, "Piranesi", "9781635575637")
	missing := withISBN(t, s, uid, "Leviathan Wakes", "9780316129084")
	none := add(t, s, uid, titled("No ISBN", "", books.ShelfWant))
	uploaded := withISBN(t, s, uid, "Uploaded", "9781635575637")
	if err := s.Store.SetCover(ctx, uid, uploaded, "image/gif", []byte("GIF89a"), books.CoverUpload); err != nil {
		t.Fatal(err)
	}
	notImage := withISBN(t, s, s.Bob.User.ID, "The Hobbit", "9780547928227") // any user's
	before := getBookAt(t, s, uid, found).UpdatedAt
	s.Clock.Advance(time.Hour)

	if err := runBackfill(t, a); err != nil {
		t.Fatal(err)
	}
	c, err := s.Store.Cover(ctx, uid, found)
	if err != nil || !bytes.Equal(c.Bytes, onePNG) || coverSource(t, s, found) != books.CoverFromOL {
		t.Errorf("Piranesi's cover = %d bytes, %v, source %q; want Open Library's", len(c.Bytes), err, coverSource(t, s, found))
	}
	if checkedAt(t, s, found) != "" {
		t.Error("a found cover marked the book checked")
	}
	if after := getBookAt(t, s, uid, found).UpdatedAt; !after.Equal(before) {
		t.Errorf("updated_at moved from %v to %v: a background cover is not the person's change", before, after)
	}
	for name, id := range map[string]int64{"a 404": missing, "not an image": notImage} {
		if checkedAt(t, s, id) == "" {
			t.Errorf("%s: book not marked checked", name)
		}
	}
	if checkedAt(t, s, none) != "" || coverSource(t, s, uploaded) != books.CoverUpload {
		t.Error("the backfill touched a book with no ISBN, or one with a cover")
	}
	if *pauses != 2 {
		t.Errorf("%d pauses for three lookups, want one between each", *pauses)
	}
	if cs, err := s.Store.CoverCandidates(ctx, 25); err != nil || len(cs) != 0 {
		t.Errorf("candidates after a run = %+v, %v; want none", cs, err)
	}
}

func TestBackfillReportsAFailedLookup(t *testing.T) {
	s, a, _ := newBooksApp(t)
	down := withISBN(t, s, s.Alice.User.ID, "Down", "9780306406157")
	if err := runBackfill(t, a); err == nil {
		t.Error("run with Open Library down = nil, want its error on the jobs page")
	}
	if checkedAt(t, s, down) != "" {
		t.Error("a failed lookup marked the book checked; it should be tried again")
	}
}

func TestBackfillGoesPastOneBadBook(t *testing.T) {
	s, a, _ := newBooksApp(t)
	ctx := context.Background()
	uid := s.Alice.User.ID
	bad := withISBN(t, s, uid, "Forbidden", "9780140449136")
	good := withISBN(t, s, uid, "Piranesi", "9781635575637")
	miss := withISBN(t, s, uid, "Leviathan Wakes", "9780316129084")

	if err := runBackfill(t, a); err == nil {
		t.Error("run with a failed lookup = nil, want the failure on the jobs page")
	}
	if _, err := s.Store.Cover(ctx, uid, good); err != nil {
		t.Errorf("the book after the failing one got no cover: %v", err)
	}
	if checkedAt(t, s, miss) == "" {
		t.Error("the miss after the failing one wasn't marked checked")
	}
	if checkedAt(t, s, bad) != "" {
		t.Error("the failing book was marked checked; it should be retried")
	}
}

func TestBackfillStopsAfterThreeFailuresInARow(t *testing.T) {
	s, a, _ := newBooksApp(t)
	ctx := context.Background()
	uid := s.Alice.User.ID
	for i := 0; i < 3; i++ {
		withISBN(t, s, uid, "Down", "9780306406157")
	}
	later := withISBN(t, s, uid, "Piranesi", "9781635575637")
	if err := runBackfill(t, a); err == nil {
		t.Fatal("run = nil, want the failures")
	}
	if _, err := s.Store.Cover(ctx, uid, later); err == nil {
		t.Error("the run went on past three failures in a row")
	}
}

func TestBackfillResetsTheFailureCountOnASuccess(t *testing.T) {
	s, a, _ := newBooksApp(t)
	ctx := context.Background()
	uid := s.Alice.User.ID
	for _, isbn := range []string{"9780306406157", "9780306406157", "9781635575637", "9780306406157", "9780306406157"} {
		withISBN(t, s, uid, "Book", isbn)
	}
	last := withISBN(t, s, uid, "Piranesi", "9781635575637")
	if err := runBackfill(t, a); err == nil {
		t.Fatal("run = nil, want the failures")
	}
	if _, err := s.Store.Cover(ctx, uid, last); err != nil {
		t.Errorf("two failures, a success, two failures stopped the run: %v", err)
	}
}

func TestBackfillStopsWhenCancelled(t *testing.T) {
	s, a, _ := newBooksApp(t)
	uid := s.Alice.User.ID
	first := withISBN(t, s, uid, "Piranesi", "9781635575637")
	withISBN(t, s, uid, "Leviathan Wakes", "9780316129084")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := a.BackfillCovers(ctx, 25); err == nil {
		t.Error("a cancelled run = nil, want its error")
	}
	if checkedAt(t, s, first) != "" {
		t.Error("a cancelled run marked a book checked")
	}
}

func TestBackfillLeavesRemovedCoversAlone(t *testing.T) {
	s, _, _ := newBooksApp(t)
	ctx := context.Background()
	uid := s.Alice.User.ID
	id := withISBN(t, s, uid, "Piranesi", "9781635575637")
	if err := s.Store.SetCover(ctx, uid, id, "image/png", onePNG, books.CoverUpload); err != nil {
		t.Fatal(err)
	}
	if err := s.Store.RemoveCover(ctx, uid, id); err != nil {
		t.Fatal(err)
	}
	if checkedAt(t, s, id) == "" {
		t.Fatal("removing a cover didn't mark the book checked; the backfill would bring it back")
	}
	in := getBookAt(t, s, uid, id).BookInput
	if err := s.Store.Update(ctx, uid, id, in); err != nil {
		t.Fatal(err)
	}
	if checkedAt(t, s, id) == "" {
		t.Error("saving the same ISBN cleared cover_checked_at")
	}
	in.ISBN = "9780316129084"
	if err := s.Store.Update(ctx, uid, id, in); err != nil {
		t.Fatal(err)
	}
	if checkedAt(t, s, id) != "" {
		t.Error("a new ISBN kept cover_checked_at; the backfill should look again")
	}
}

func TestCoverByISBNSaysWhenThereIsNone(t *testing.T) {
	ol := newOpenLibrary(t)
	ctx := context.Background()
	if ct, data, err := ol.CoverByISBN(ctx, "9781635575637"); err != nil || ct != "image/png" || !bytes.Equal(data, onePNG) {
		t.Errorf("found = %q, %d bytes, %v", ct, len(data), err)
	}
	for _, isbn := range []string{"9780316129084", "9780547928227"} {
		if _, _, err := ol.CoverByISBN(ctx, isbn); !errors.Is(err, books.ErrNoCover) {
			t.Errorf("%s = %v, want ErrNoCover", isbn, err)
		}
	}
	if _, _, err := ol.CoverByISBN(ctx, "9780306406157"); err == nil || errors.Is(err, books.ErrNoCover) {
		t.Errorf("a 503 = %v, want an error that isn't ErrNoCover", err)
	}
	if _, _, err := ol.CoverByISBN(ctx, "../etc"); !errors.Is(err, books.ErrInvalid) {
		t.Errorf("a bad ISBN = %v, want ErrInvalid", err)
	}
}

// getBookAt is one of userID's books through the server's store.
func getBookAt(t *testing.T, s *server, userID, id int64) books.Book {
	t.Helper()
	b, err := s.Store.Get(context.Background(), userID, id)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
