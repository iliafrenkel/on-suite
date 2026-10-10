package books_test

import (
	"bytes"
	"context"
	"reflect"
	"testing"

	"github.com/iliafrenkel/on-suite/internal/apps/books"
	"github.com/iliafrenkel/on-suite/internal/platform/app"
)

func TestAdminCardCountsEveryUsersBooks(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	importFixture(t, f, f.alice.ID) // 2 read (3 readings), 1 reading, 1 DNF, 2 want, 1 note
	id := addBook(t, f, f.bob.ID, onShelf("Dune", books.ShelfReading))
	if err := f.store.MarkDNF(ctx, f.bob.ID, id, "", 0); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.AddNote(ctx, f.bob.ID, id, books.NoteInput{Body: "Too slow."}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.AddQuote(ctx, f.bob.ID, id, books.QuoteInput{Text: "Fear is the mind-killer."}); err != nil {
		t.Fatal(err)
	}
	if err := f.store.SetCover(ctx, f.bob.ID, id, "image/png", bytes.Repeat(onePNG, 128), books.CoverUpload); err != nil {
		t.Fatal(err)
	}

	var s app.Stater = books.New()
	stats, err := s.Stats(ctx, f.db)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, st := range stats {
		got[st.Label] = st.Value
	}
	want := map[string]string{"Books": "7", "Reading": "1", "Want to read": "2", "Read": "2", "Did not finish": "2",
		"Readings": "6", "Notes": "2", "Quotes": "1", "Stored covers": "2.0 KiB"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("admin card = %v, want %v", got, want)
	}
}
