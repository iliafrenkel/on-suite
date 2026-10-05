package later_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/iliafrenkel/on-suite/internal/apps/later"
)

// saveDoc saves a readable article for userID; its ContentText is the
// paragraph's text.
func (f *fixture) saveDoc(t *testing.T, userID int64, pageURL, html string) later.Article {
	t.Helper()
	a, created, err := f.store.Save(context.Background(), userID, later.NewArticle{URL: pageURL, Title: "T", ContentHTML: html})
	if err != nil || !created {
		t.Fatalf("Save(%s) = %v, created %v", pageURL, err, created)
	}
	return a
}

const helloHTML = "<p>Hello brave new world</p>"

func TestValidSpan(t *testing.T) {
	for _, c := range []struct {
		name       string
		text       string
		start, end int
		quote      string
		want       bool
	}{
		{"ascii", "Hello brave world", 6, 11, "brave", true},
		{"cyrillic", "Привет мир", 7, 10, "мир", true},
		{"hebrew", "שלום עולם", 5, 9, "עולם", true},
		{"emoji is one code point", "a👋b", 1, 2, "👋", true},
		{"utf-16 offsets are wrong", "👋👋x", 4, 5, "x", false},
		{"after two emoji", "👋👋x", 2, 3, "x", true},
		{"combining mark is its own code point", "e\u0301x", 0, 2, "e\u0301", true},
		{"half a combining sequence", "e\u0301x", 0, 1, "e", true},
		{"whole text", "abc", 0, 3, "abc", true},
		{"end past the text", "abc", 1, 4, "bc", false},
		{"negative start", "abc", -1, 2, "ab", false},
		{"empty range", "abc", 1, 1, "", false},
		{"reversed", "abc", 2, 1, "b", false},
		{"quote mismatch", "abc", 0, 2, "ax", false},
		{"blank quote", "a  b", 1, 3, "  ", false},
	} {
		if got := later.ValidSpan(c.text, c.start, c.end, c.quote); got != c.want {
			t.Errorf("%s: ValidSpan(%q, %d, %d, %q) = %v, want %v", c.name, c.text, c.start, c.end, c.quote, got, c.want)
		}
	}
}

func TestAddHighlightStoresAndListsInTextOrder(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	doc := f.saveDoc(t, f.alice.ID, "https://a.example/1", helloHTML)

	if _, err := f.store.AddHighlight(ctx, doc.ID, doc.ContentText, 16, 21, "world", " Nice \r\nidea "); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.AddHighlight(ctx, doc.ID, doc.ContentText, 6, 11, "brave", ""); err != nil {
		t.Fatal(err)
	}
	got, err := f.store.Highlights(ctx, doc.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Quote != "brave" || got[1].Quote != "world" {
		t.Fatalf("Highlights = %+v, want brave then world", got)
	}
	brave, world := got[0], got[1]
	if world.Comment != "Nice \nidea" {
		t.Errorf("world.Comment = %q", world.Comment)
	}
	if brave.DocID != doc.ID || brave.Start != 6 || brave.End != 11 || brave.Comment != "" {
		t.Errorf("brave = %+v", brave)
	}
	if world.DocID != doc.ID || world.Start != 16 || world.End != 21 {
		t.Errorf("world = %+v", world)
	}
	for _, h := range got {
		if h.ID == 0 || !h.CreatedAt.Equal(f.now) || !h.UpdatedAt.Equal(f.now) {
			t.Errorf("highlight %+v: want an ID and CreatedAt == UpdatedAt == %v", h, f.now)
		}
	}
}

func TestAddHighlightRefusesOverlaps(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	doc := f.saveDoc(t, f.alice.ID, "https://a.example/1", helloHTML)
	if _, err := f.store.AddHighlight(ctx, doc.ID, doc.ContentText, 6, 15, "brave new", ""); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		name       string
		start, end int
		quote      string
		want       error
	}{
		{"straddling the end", 12, 21, "new world", later.ErrOverlap},
		{"inside", 8, 10, "av", later.ErrOverlap},
		{"around", 0, 21, "Hello brave new world", later.ErrOverlap},
		{"touching the start", 0, 6, "Hello ", nil},
		{"after it", 16, 21, "world", nil},
	} {
		_, err := f.store.AddHighlight(ctx, doc.ID, doc.ContentText, c.start, c.end, c.quote, "")
		if !errors.Is(err, c.want) {
			t.Errorf("%s: err = %v, want %v", c.name, err, c.want)
		}
	}
}

func TestAddHighlightRefusesBadSpans(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	doc := f.saveDoc(t, f.alice.ID, "https://a.example/1", helloHTML)
	if _, err := f.store.AddHighlight(ctx, doc.ID, doc.ContentText, 6, 11, "brane", ""); !errors.Is(err, later.ErrInvalid) {
		t.Errorf("wrong quote: err = %v, want ErrInvalid", err)
	}
	if _, err := f.store.AddHighlight(ctx, doc.ID, doc.ContentText, 16, 99, "world", ""); !errors.Is(err, later.ErrInvalid) {
		t.Errorf("past the end: err = %v, want ErrInvalid", err)
	}
	if n := f.countRows(t, `SELECT count(*) FROM later_highlights`); n != 0 {
		t.Errorf("later_highlights rows = %d, want 0", n)
	}
}

func TestHighlightsBelongToTheirDocument(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	a := f.saveDoc(t, f.alice.ID, "https://a.example/a", helloHTML)
	b := f.saveDoc(t, f.alice.ID, "https://a.example/b", helloHTML)
	hA, err := f.store.AddHighlight(ctx, a.ID, a.ContentText, 6, 11, "brave", "keep")
	if err != nil {
		t.Fatal(err)
	}
	if got, err := f.store.Highlights(ctx, b.ID); err != nil || len(got) != 0 {
		t.Errorf("Highlights(B) = %v, %v, want none", got, err)
	}
	if err := f.store.SetHighlightComment(ctx, b.ID, hA.ID, "changed"); !errors.Is(err, later.ErrNotFound) {
		t.Errorf("SetHighlightComment via B: err = %v, want ErrNotFound", err)
	}
	if err := f.store.DeleteHighlight(ctx, b.ID, hA.ID); !errors.Is(err, later.ErrNotFound) {
		t.Errorf("DeleteHighlight via B: err = %v, want ErrNotFound", err)
	}
	got, err := f.store.Highlights(ctx, a.ID)
	if err != nil || len(got) != 1 || got[0].Comment != "keep" {
		t.Errorf("Highlights(A) = %+v, %v, want hA untouched", got, err)
	}
}

func TestSetHighlightComment(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	doc := f.saveDoc(t, f.alice.ID, "https://a.example/1", helloHTML)
	h, err := f.store.AddHighlight(ctx, doc.ID, doc.ContentText, 6, 11, "brave", "first")
	if err != nil {
		t.Fatal(err)
	}
	f.now = f.now.Add(time.Hour)
	if err := f.store.SetHighlightComment(ctx, doc.ID, h.ID, " Second\r\nthought "); err != nil {
		t.Fatal(err)
	}
	got, _ := f.store.Highlights(ctx, doc.ID)
	if len(got) != 1 || got[0].Comment != "Second\nthought" || !got[0].UpdatedAt.Equal(f.now) {
		t.Errorf("after edit = %+v, want comment %q, UpdatedAt %v", got, "Second\nthought", f.now)
	}
	if err := f.store.SetHighlightComment(ctx, doc.ID, h.ID, "   "); err != nil {
		t.Fatal(err)
	}
	got, _ = f.store.Highlights(ctx, doc.ID)
	if len(got) != 1 || got[0].Comment != "" {
		t.Errorf("after blanking = %+v, want empty comment", got)
	}
}

func TestDeleteHighlight(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	doc := f.saveDoc(t, f.alice.ID, "https://a.example/1", helloHTML)
	h, err := f.store.AddHighlight(ctx, doc.ID, doc.ContentText, 6, 11, "brave", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := f.store.DeleteHighlight(ctx, doc.ID, h.ID); err != nil {
		t.Fatal(err)
	}
	if got, _ := f.store.Highlights(ctx, doc.ID); len(got) != 0 {
		t.Errorf("Highlights = %+v, want none", got)
	}
	if err := f.store.DeleteHighlight(ctx, doc.ID, h.ID); !errors.Is(err, later.ErrNotFound) {
		t.Errorf("second delete: err = %v, want ErrNotFound", err)
	}
}

func TestDeletingAnArticleRemovesItsHighlights(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	doc := f.saveDoc(t, f.alice.ID, "https://a.example/1", helloHTML)
	if _, err := f.store.AddHighlight(ctx, doc.ID, doc.ContentText, 6, 11, "brave", ""); err != nil {
		t.Fatal(err)
	}
	if err := f.store.Delete(ctx, f.alice.ID, doc.ID); err != nil {
		t.Fatal(err)
	}
	if n := f.countRows(t, `SELECT count(*) FROM later_highlights`); n != 0 {
		t.Errorf("later_highlights rows = %d, want 0", n)
	}
}

func TestSetNote(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	doc := f.saveDoc(t, f.alice.ID, "https://a.example/1", helloHTML)
	if err := f.store.SetNote(ctx, f.alice.ID, doc.ID, "  First line\r\nsecond  "); err != nil {
		t.Fatal(err)
	}
	if a, _ := f.store.Article(ctx, f.alice.ID, doc.ID); a.Note != "First line\nsecond" {
		t.Errorf("Note = %q", a.Note)
	}
	if err := f.store.SetNote(ctx, f.bob.ID, doc.ID, "mine now"); !errors.Is(err, later.ErrNotFound) {
		t.Errorf("bob: err = %v, want ErrNotFound", err)
	}
	if a, _ := f.store.Article(ctx, f.alice.ID, doc.ID); a.Note != "First line\nsecond" {
		t.Errorf("Note after bob = %q, want unchanged", a.Note)
	}
	if err := f.store.SetNote(ctx, f.alice.ID, doc.ID, " \n "); err != nil {
		t.Fatal(err)
	}
	if a, _ := f.store.Article(ctx, f.alice.ID, doc.ID); a.Note != "" {
		t.Errorf("Note after blanking = %q", a.Note)
	}
}
