package books_test

import (
	"context"
	"errors"
	"testing"

	"github.com/iliafrenkel/on-suite/internal/apps/books"
)

func TestPrefsAreTheDefaultsUntilSet(t *testing.T) {
	f := newFixture(t)
	if want := (books.Prefs{View: books.ViewGrid, Sort: books.SortDefault}); books.DefaultPrefs != want {
		t.Errorf("DefaultPrefs = %+v, want grid, Default, not reversed (spec \"Preferences\")", books.DefaultPrefs)
	}
	got, err := f.store.Prefs(context.Background(), f.alice.ID)
	if err != nil || got != books.DefaultPrefs {
		t.Errorf("Prefs before any save = %+v, %v; want the defaults", got, err)
	}
}

func TestSetPrefsIsPerUserAndReplaces(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	want := books.Prefs{View: books.ViewList, Sort: books.SortTitle, Reversed: true}
	if err := f.store.SetPrefs(ctx, f.alice.ID, want); err != nil {
		t.Fatal(err)
	}
	if got, err := f.store.Prefs(ctx, f.alice.ID); err != nil || got != want {
		t.Errorf("Alice's prefs = %+v, %v; want %+v", got, err, want)
	}
	if got, _ := f.store.Prefs(ctx, f.bob.ID); got != books.DefaultPrefs {
		t.Errorf("Bob's prefs = %+v, want the defaults: Alice's are her own", got)
	}
	again := books.Prefs{View: books.ViewGrid, Sort: books.SortAuthor}
	if err := f.store.SetPrefs(ctx, f.alice.ID, again); err != nil {
		t.Fatal(err)
	}
	if got, _ := f.store.Prefs(ctx, f.alice.ID); got != again {
		t.Errorf("after a second save = %+v, want %+v", got, again)
	}
}

func TestSetPrefsRefusesWhatTheHomePageHasNoControlFor(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	for _, p := range []books.Prefs{
		{View: "table", Sort: books.SortDefault},
		{View: books.ViewGrid, Sort: "rating"},
		{},
	} {
		if err := f.store.SetPrefs(ctx, f.alice.ID, p); !errors.Is(err, books.ErrInvalid) {
			t.Errorf("SetPrefs(%+v) = %v, want ErrInvalid", p, err)
		}
	}
	if got, _ := f.store.Prefs(ctx, f.alice.ID); got != books.DefaultPrefs {
		t.Errorf("prefs after refused saves = %+v, want the defaults", got)
	}
}

func TestPrefsGoWithTheirUser(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	if err := f.store.SetPrefs(ctx, f.bob.ID, books.Prefs{View: books.ViewList, Sort: books.SortAdded}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Exec(`DELETE FROM users WHERE id = ?`, f.bob.ID); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := f.db.QueryRow(`SELECT count(*) FROM books_prefs`).Scan(&n); err != nil || n != 0 {
		t.Errorf("books_prefs rows after deleting Bob = %d, %v; want 0 (ON DELETE CASCADE)", n, err)
	}
}
