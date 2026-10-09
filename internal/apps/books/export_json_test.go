package books_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/iliafrenkel/on-suite/internal/apps/books"
	"github.com/iliafrenkel/on-suite/internal/platform/app"
)

// exportDoc is the JSON onsuite export writes for ON Books, read back
// the way a person restoring from it would.
type exportDoc struct {
	Books []struct {
		Title, Authors, ISBN13, Shelf, Review string
		SeriesName                            string `json:"series_name"`
		Rating                                int
		Tags                                  []string
		Readings                              []struct {
			Status, Format string
			StartedOn      string `json:"started_on"`
			FinishedOn     string `json:"finished_on"`
			Progress       []struct{ Page, Percent *int }
		}
		Notes []struct {
			Page int
			Body string
		}
		Quotes []struct {
			Page          int
			Text, Comment string
		}
	}
	Goals []struct{ Year, Target int }
}

func exportFor(t *testing.T, f *fixture, userID int64) (exportDoc, string) {
	t.Helper()
	var e app.Exporter = books.New()
	data, err := e.Export(context.Background(), f.db, userID)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(data)
	if err != nil {
		t.Fatal(err)
	}
	var doc exportDoc
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	return doc, string(raw)
}

func TestExportHasEverythingButCovers(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	uid := f.alice.ID
	nb := onShelf("Dune", books.ShelfReading)
	nb.Authors, nb.Pages, nb.ISBN, nb.Tags = "Frank Herbert", 600, "9780441013593", []string{"sf", "classics"}
	id := addBook(t, f, uid, nb)
	if err := f.store.RecordProgress(ctx, uid, id, 120); err != nil {
		t.Fatal(err)
	}
	if err := f.store.SetRating(ctx, uid, id, 5); err != nil {
		t.Fatal(err)
	}
	if err := f.store.SetReview(ctx, uid, id, "The *spice*."); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.AddNote(ctx, uid, id, books.NoteInput{Page: 112, Body: "Paul and the box."}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.AddQuote(ctx, uid, id, books.QuoteInput{Text: "Fear is the mind-killer.", Comment: "The litany."}); err != nil {
		t.Fatal(err)
	}
	if err := f.store.SetCover(ctx, uid, id, "image/png", onePNG, books.CoverUpload); err != nil {
		t.Fatal(err)
	}
	if err := f.store.SetGoal(ctx, uid, 2026, 24); err != nil {
		t.Fatal(err)
	}
	addBook(t, f, f.bob.ID, onShelf("Bob's book", books.ShelfWant))

	doc, raw := exportFor(t, f, uid)
	if len(doc.Books) != 1 {
		t.Fatalf("%d books, want Alice's one: %s", len(doc.Books), raw)
	}
	b := doc.Books[0]
	if b.Title != "Dune" || b.Authors != "Frank Herbert" || b.ISBN13 != "9780441013593" || b.Shelf != "reading" ||
		b.Rating != 5 || b.Review != "The *spice*." || len(b.Tags) != 2 || b.Tags[0] != "classics" {
		t.Errorf("book = %+v", b)
	}
	if len(b.Readings) != 1 || b.Readings[0].Status != "reading" || b.Readings[0].StartedOn != "2026-10-10" ||
		len(b.Readings[0].Progress) != 1 || b.Readings[0].Progress[0].Page == nil || *b.Readings[0].Progress[0].Page != 120 ||
		b.Readings[0].Progress[0].Percent != nil {
		t.Errorf("readings = %+v", b.Readings)
	}
	if len(b.Notes) != 1 || b.Notes[0].Page != 112 || len(b.Quotes) != 1 || b.Quotes[0].Comment != "The litany." {
		t.Errorf("notes = %+v, quotes = %+v", b.Notes, b.Quotes)
	}
	if len(doc.Goals) != 1 || doc.Goals[0].Year != 2026 || doc.Goals[0].Target != 24 {
		t.Errorf("goals = %+v", doc.Goals)
	}
	var keys map[string]any
	if err := json.Unmarshal([]byte(raw), &keys); err != nil {
		t.Fatal(err)
	}
	book := keys["books"].([]any)[0].(map[string]any)
	for _, k := range []string{"cover", "covers", "bytes", "cover_checked_at"} {
		if _, ok := book[k]; ok {
			t.Errorf("an exported book has %q; covers stay out of the export", k)
		}
	}
}

func TestExportOfAnEmptyLibraryIsEmptyLists(t *testing.T) {
	f := newFixture(t)
	if _, raw := exportFor(t, f, f.alice.ID); raw != `{"books":[],"goals":[]}` {
		t.Errorf("empty export = %s", raw)
	}
}

func TestExportKeepsUndatedImportedReadings(t *testing.T) {
	f := newFixture(t)
	importFixture(t, f, f.alice.ID)
	doc, _ := exportFor(t, f, f.alice.ID)
	for _, b := range doc.Books {
		if b.Title != "Leviathan Wakes" {
			continue
		}
		if len(b.Readings) != 2 || b.Readings[0].FinishedOn != "" || b.Readings[1].FinishedOn != "2024-03-14" ||
			b.SeriesName != "The Expanse" {
			t.Errorf("Leviathan Wakes = %+v, want the undated reading first, then the dated one", b)
		}
		return
	}
	t.Error("Leviathan Wakes isn't in the export")
}
