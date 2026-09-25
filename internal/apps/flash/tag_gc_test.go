// internal/apps/flash/tag_gc_test.go
package flash_test

import (
	"context"
	"strings"
	"testing"

	"github.com/iliafrenkel/on-suite/internal/apps/flash"
)

// TestCardTagsAreIndexedByTag pins migration 0013: the orphan-tag NOT
// EXISTS (both SetCardTags' inline cleanup and PurgeOrphanTags' sweep),
// CardsByTag's cross-deck filter, and SQLite's own foreign-key check on
// every flash_tags row deleted all look flash_card_tags up by tag_id —
// the column that is not the leading key of its WITHOUT ROWID primary key.
func TestCardTagsAreIndexedByTag(t *testing.T) {
	f := newFixture(t)
	var def string
	err := f.db.QueryRowContext(context.Background(),
		`SELECT sql FROM sqlite_master WHERE type = 'index' AND tbl_name = 'flash_card_tags' AND name = ?`,
		"flash_card_tags_tag_idx").Scan(&def)
	if err != nil {
		t.Fatalf("index on flash_card_tags(tag_id): %v", err)
	}
	if !strings.Contains(def, "tag_id") {
		t.Errorf("index def = %q, want it to cover tag_id", def)
	}
}

// tagRowExists reports whether userID has a flash_tags row named name,
// regardless of whether anything still references it — the raw row count,
// not TagsForCard/CardsByTag, which both join through flash_card_tags and so
// can never see an orphan.
func tagRowExists(t *testing.T, f *fixture, userID int64, name string) bool {
	t.Helper()
	var n int
	if err := f.db.QueryRowContext(context.Background(),
		`SELECT COUNT(*) FROM flash_tags WHERE user_id = ? AND name = ?`, userID, name,
	).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n > 0
}

// TestSetCardTagsGarbageCollectsNowUnusedTags pins #289: replacing a card's
// tag set must not leave behind a flash_tags row nothing references any
// more, but must keep a tag another of the same user's cards still carries,
// and must never touch another user's tag of the same name.
func TestSetCardTagsGarbageCollectsNowUnusedTags(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	deck := qDeck(t, f, "Spanish")
	card1 := qCard(t, f, deck.ID, "hola")
	card2 := qCard(t, f, deck.ID, "adios")

	// Bob has his own "hard" tag, unrelated to alice's.
	bobDeck, err := f.store.CreateDeck(ctx, f.bob.ID, "Bob's deck", "", flash.DefaultDeckColor)
	if err != nil {
		t.Fatal(err)
	}
	bobCard, err := f.store.CreateCard(ctx, f.bob.ID, bobDeck.ID, flash.CardTypeBasic, "x", "y", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := f.store.SetCardTags(ctx, f.bob.ID, bobCard.ID, []string{"hard"}); err != nil {
		t.Fatal(err)
	}

	if err := f.store.SetCardTags(ctx, f.alice.ID, card1.ID, []string{"hard", "greetings"}); err != nil {
		t.Fatal(err)
	}
	if err := f.store.SetCardTags(ctx, f.alice.ID, card2.ID, []string{"hard"}); err != nil {
		t.Fatal(err)
	}

	// Dropping "greetings" from card1 (alice's only card carrying it) must
	// remove alice's "greetings" tag row, but "hard" stays: card2 still
	// carries it, and so does bob's own, unrelated "hard" tag.
	if err := f.store.SetCardTags(ctx, f.alice.ID, card1.ID, []string{"hard"}); err != nil {
		t.Fatal(err)
	}

	if tagRowExists(t, f, f.alice.ID, "greetings") {
		t.Error(`alice's "greetings" tag row still exists after nothing references it`)
	}
	if !tagRowExists(t, f, f.alice.ID, "hard") {
		t.Error(`alice's "hard" tag row was removed, but card2 still carries it`)
	}
	if !tagRowExists(t, f, f.bob.ID, "hard") {
		t.Error(`bob's "hard" tag row was removed by alice's edit`)
	}
}

// TestPurgeOrphanTagsSweepsAcrossUsersAfterCardAndDeckDeletes pins #289's
// sweep half: DeleteCard and DeleteDeck cascade flash_card_tags links away
// (ON DELETE CASCADE) but leave the flash_tags row itself, since no write
// path there is scoped to "this specific tag." PurgeOrphanTags is the
// catch-all that removes what SetCardTags' own inline cleanup cannot reach.
func TestPurgeOrphanTagsSweepsAcrossUsersAfterCardAndDeckDeletes(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	deck := qDeck(t, f, "Spanish")
	doomedCard := qCard(t, f, deck.ID, "hola")
	survivingCard := qCard(t, f, deck.ID, "adios")
	if err := f.store.SetCardTags(ctx, f.alice.ID, doomedCard.ID, []string{"doomed-by-card"}); err != nil {
		t.Fatal(err)
	}
	if err := f.store.SetCardTags(ctx, f.alice.ID, survivingCard.ID, []string{"kept"}); err != nil {
		t.Fatal(err)
	}

	doomedDeck, err := f.store.CreateDeck(ctx, f.alice.ID, "French", "", flash.DefaultDeckColor)
	if err != nil {
		t.Fatal(err)
	}
	doomedDeckCard, err := f.store.CreateCard(ctx, f.alice.ID, doomedDeck.ID, flash.CardTypeBasic, "oui", "yes", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := f.store.SetCardTags(ctx, f.alice.ID, doomedDeckCard.ID, []string{"doomed-by-deck"}); err != nil {
		t.Fatal(err)
	}

	if err := f.store.DeleteCard(ctx, f.alice.ID, deck.ID, doomedCard.ID); err != nil {
		t.Fatal(err)
	}
	if err := f.store.DeleteDeck(ctx, f.alice.ID, doomedDeck.ID); err != nil {
		t.Fatal(err)
	}

	// The rows are orphaned now, but SetCardTags was never called again, so
	// they must still be sitting there until the sweep runs.
	if !tagRowExists(t, f, f.alice.ID, "doomed-by-card") {
		t.Fatal(`"doomed-by-card" tag row is already gone before PurgeOrphanTags ran`)
	}
	if !tagRowExists(t, f, f.alice.ID, "doomed-by-deck") {
		t.Fatal(`"doomed-by-deck" tag row is already gone before PurgeOrphanTags ran`)
	}

	n, err := f.store.PurgeOrphanTags(ctx)
	if err != nil {
		t.Fatalf("PurgeOrphanTags: %v", err)
	}
	if n != 2 {
		t.Errorf("PurgeOrphanTags deleted %d rows, want 2", n)
	}

	if tagRowExists(t, f, f.alice.ID, "doomed-by-card") {
		t.Error(`"doomed-by-card" tag row survived PurgeOrphanTags`)
	}
	if tagRowExists(t, f, f.alice.ID, "doomed-by-deck") {
		t.Error(`"doomed-by-deck" tag row survived PurgeOrphanTags`)
	}
	if !tagRowExists(t, f, f.alice.ID, "kept") {
		t.Error(`"kept" tag row (still referenced) was removed by PurgeOrphanTags`)
	}

	// Running it again finds nothing left to do.
	n2, err := f.store.PurgeOrphanTags(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if n2 != 0 {
		t.Errorf("second PurgeOrphanTags deleted %d rows, want 0", n2)
	}
}
