// internal/apps/flash/card_test.go
package flash_test

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/iliafrenkel/on-suite/internal/apps/flash"
)

// applyCardForm re-saves cardID's existing fields through SaveCardForm,
// changing only what form itself sets (Tags/Image/Audio/RemoveImage/
// RemoveAudio) — Front/Back/Notes/CardType always come from c, not the
// caller. SetCardTags, SetCardMedia, AttachCardUpload and UpdateCard used to
// give tests a one-call way to change just tags or just media; now that the
// card form only ever saves through SaveCardForm (#363), this is that same
// one-call shape for tests (#382).
func applyCardForm(t *testing.T, ctx context.Context, store *flash.Store, userID, deckID int64, c flash.Card, form flash.CardForm) (flash.Card, error) {
	t.Helper()
	form.CardType, form.Front, form.Back, form.Notes = c.CardType, c.Front, c.Back, c.Notes
	return store.SaveCardForm(ctx, userID, deckID, c.ID, form)
}

// setCardMediaHash points cardID's image or audio column directly at an
// already-existing flash_media hash. SaveCardForm can only attach a new
// upload's bytes (it hashes them itself via SaveMediaUpload), so it cannot
// express attaching media that already exists under a known hash — the
// shape SetCardMedia used to serve for tests seeding URL-sourced or shared
// media (#382).
func setCardMediaHash(t *testing.T, db *sql.DB, cardID int64, kind string, hash *string) {
	t.Helper()
	column := "image_hash"
	if kind == flash.MediaKindAudio {
		column = "audio_hash"
	}
	if _, err := db.ExecContext(context.Background(),
		`UPDATE flash_cards SET `+column+` = ? WHERE id = ?`, hash, cardID); err != nil {
		t.Fatal(err)
	}
}

func TestCreateAndFetchCard(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	deck, err := f.store.CreateDeck(ctx, f.alice.ID, "Spanish", "", flash.DefaultDeckColor)
	if err != nil {
		t.Fatal(err)
	}

	created, err := f.store.CreateCard(ctx, f.alice.ID, deck.ID, flash.CardTypeBasic, "hola", "hello", "greeting")
	if err != nil {
		t.Fatalf("CreateCard: %v", err)
	}
	if created.ID == 0 {
		t.Error("CreateCard returned id 0")
	}

	got, err := f.store.CardByID(ctx, f.alice.ID, deck.ID, created.ID)
	if err != nil {
		t.Fatalf("CardByID: %v", err)
	}
	if got.Front != "hola" || got.Back != "hello" || got.Notes != "greeting" {
		t.Errorf("round trip lost data: %+v", got)
	}
}

func TestCreateCardRejectsUnknownDeck(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	if _, err := f.store.CreateCard(ctx, f.alice.ID, 999999, flash.CardTypeBasic, "a", "b", ""); !errors.Is(err, flash.ErrNotFound) {
		t.Errorf("CreateCard into a missing deck = %v, want ErrNotFound", err)
	}
}

func TestCreateCardRejectsSomeoneElsesDeck(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	deck, err := f.store.CreateDeck(ctx, f.alice.ID, "alice's", "", flash.DefaultDeckColor)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.CreateCard(ctx, f.bob.ID, deck.ID, flash.CardTypeBasic, "a", "b", ""); !errors.Is(err, flash.ErrNotFound) {
		t.Errorf("CreateCard into another user's deck = %v, want ErrNotFound", err)
	}
}

func TestValidateCard(t *testing.T) {
	tests := []struct {
		name     string
		cardType string
		front    string
		back     string
		wantErr  bool
	}{
		{"ordinary basic", flash.CardTypeBasic, "q", "a", false},
		{"basic missing back", flash.CardTypeBasic, "q", "", true},
		{"basic missing front", flash.CardTypeBasic, "", "a", true},
		{"unknown type", "essay", "q", "a", true},
		{"cloze needs markers", flash.CardTypeCloze, "no markers here", "", true},
		{"ordinary cloze", flash.CardTypeCloze, "The capital of France is {{c1::Paris}}.", "", false},
		{"cloze must not carry a back", flash.CardTypeCloze, "{{c1::x}}", "unexpected", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := flash.ValidateCard(tt.cardType, tt.front, tt.back)
			if tt.wantErr && !errors.Is(err, flash.ErrInvalid) {
				t.Errorf("ValidateCard = %v, want ErrInvalid", err)
			}
			if !tt.wantErr && err != nil {
				t.Errorf("ValidateCard rejected a valid card: %v", err)
			}
		})
	}
}

func TestListCardsIsScopedToItsDeck(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	deckA, err := f.store.CreateDeck(ctx, f.alice.ID, "A", "", flash.DefaultDeckColor)
	if err != nil {
		t.Fatal(err)
	}
	deckB, err := f.store.CreateDeck(ctx, f.alice.ID, "B", "", flash.DefaultDeckColor)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.CreateCard(ctx, f.alice.ID, deckA.ID, flash.CardTypeBasic, "a1", "x", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.CreateCard(ctx, f.alice.ID, deckB.ID, flash.CardTypeBasic, "b1", "x", ""); err != nil {
		t.Fatal(err)
	}

	got, err := f.store.ListCards(ctx, f.alice.ID, deckA.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Front != "a1" {
		t.Errorf("ListCards(deckA) = %+v, want exactly a1", got)
	}
}

// TestCardOwnerScoping mirrors TestDeckOwnerScoping: bob must not reach
// alice's card even by guessing the right deck id.
func TestCardOwnerScoping(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	deck, err := f.store.CreateDeck(ctx, f.alice.ID, "alice's", "", flash.DefaultDeckColor)
	if err != nil {
		t.Fatal(err)
	}
	card, err := f.store.CreateCard(ctx, f.alice.ID, deck.ID, flash.CardTypeBasic, "secret", "x", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.CardByID(ctx, f.bob.ID, deck.ID, card.ID); !errors.Is(err, flash.ErrNotFound) {
		t.Errorf("CardByID as another user = %v, want ErrNotFound", err)
	}
	if err := f.store.DeleteCard(ctx, f.bob.ID, deck.ID, card.ID); !errors.Is(err, flash.ErrNotFound) {
		t.Errorf("DeleteCard as another user = %v, want ErrNotFound", err)
	}
}

// TestSaveCardFormUpdateKeepsCreatedAt pins what used to be UpdateCard's own
// test: updating a card's fields through SaveCardForm changes Front/Back/
// Notes but must never touch CreatedAt (#382).
func TestSaveCardFormUpdateKeepsCreatedAt(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	deck, err := f.store.CreateDeck(ctx, f.alice.ID, "test", "", flash.DefaultDeckColor)
	if err != nil {
		t.Fatal(err)
	}

	created, err := f.store.CreateCard(ctx, f.alice.ID, deck.ID, flash.CardTypeBasic, "original q", "original a", "original note")
	if err != nil {
		t.Fatal(err)
	}
	originalCreatedAt := created.CreatedAt

	updated, err := f.store.SaveCardForm(ctx, f.alice.ID, deck.ID, created.ID, flash.CardForm{
		CardType: flash.CardTypeBasic, Front: "new q", Back: "new a", Notes: "new note",
	})
	if err != nil {
		t.Fatalf("SaveCardForm: %v", err)
	}
	if updated.Front != "new q" || updated.Back != "new a" || updated.Notes != "new note" {
		t.Errorf("SaveCardForm update = %+v, fields not updated", updated)
	}
	if !updated.CreatedAt.Equal(originalCreatedAt) {
		t.Errorf("CreatedAt changed: got %v, want %v", updated.CreatedAt, originalCreatedAt)
	}

	fetched, err := f.store.CardByID(ctx, f.alice.ID, deck.ID, created.ID)
	if err != nil {
		t.Fatalf("CardByID: %v", err)
	}
	if fetched.Front != "new q" || fetched.Back != "new a" || fetched.Notes != "new note" {
		t.Errorf("CardByID after update = %+v, fields not persisted", fetched)
	}
}

// TestSaveCardFormUpdateRejectsInvalidInput is what used to be
// UpdateCardRejectsInvalidInput: TestSaveCardFormValidates (below) only
// covers create (cardID == 0); an update's invalid input must be rejected,
// leaving the existing card untouched, the same way (#382).
func TestSaveCardFormUpdateRejectsInvalidInput(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	deck, err := f.store.CreateDeck(ctx, f.alice.ID, "test", "", flash.DefaultDeckColor)
	if err != nil {
		t.Fatal(err)
	}
	created, err := f.store.CreateCard(ctx, f.alice.ID, deck.ID, flash.CardTypeBasic, "original q", "original a", "")
	if err != nil {
		t.Fatal(err)
	}

	// Empty back for a basic card type is invalid.
	if _, err := f.store.SaveCardForm(ctx, f.alice.ID, deck.ID, created.ID, flash.CardForm{
		CardType: flash.CardTypeBasic, Front: "q", Back: "",
	}); !errors.Is(err, flash.ErrInvalid) {
		t.Errorf("SaveCardForm update with invalid input = %v, want ErrInvalid", err)
	}

	// Verify card is unchanged
	fetched, err := f.store.CardByID(ctx, f.alice.ID, deck.ID, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if fetched.Front != "original q" || fetched.Back != "original a" {
		t.Errorf("card was modified: %+v", fetched)
	}
}

func TestDeletingADeckRemovesItsCards(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	deck, err := f.store.CreateDeck(ctx, f.alice.ID, "doomed", "", flash.DefaultDeckColor)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.CreateCard(ctx, f.alice.ID, deck.ID, flash.CardTypeBasic, "a", "b", ""); err != nil {
		t.Fatal(err)
	}
	if err := f.store.DeleteDeck(ctx, f.alice.ID, deck.ID); err != nil {
		t.Fatal(err)
	}
	got, err := f.store.ListCards(ctx, f.alice.ID, deck.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("%d cards survived their deck", len(got))
	}
}

func TestCardStatuses(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	now := time.Date(2026, 9, 23, 10, 0, 0, 0, time.UTC)
	d, err := f.store.CreateDeck(ctx, f.alice.ID, "Spanish", "", flash.DefaultDeckColor)
	if err != nil {
		t.Fatal(err)
	}
	fresh, err := f.store.CreateCard(ctx, f.alice.ID, d.ID, flash.CardTypeBasic, "new one", "x", "")
	if err != nil {
		t.Fatal(err)
	}
	due, err := f.store.CreateCard(ctx, f.alice.ID, d.ID, flash.CardTypeBasic, "due one", "x", "")
	if err != nil {
		t.Fatal(err)
	}
	later, err := f.store.CreateCard(ctx, f.alice.ID, d.ID, flash.CardTypeBasic, "later one", "x", "")
	if err != nil {
		t.Fatal(err)
	}
	// Again puts a card in a learning step minutes away; Easy days away.
	if _, err := f.store.GradeCard(ctx, f.alice.ID, due.ID, flash.RatingAgain, now.Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.GradeCard(ctx, f.alice.ID, later.ID, flash.RatingEasy, now); err != nil {
		t.Fatal(err)
	}

	got, err := f.store.CardStatuses(ctx, f.alice.ID, d.ID, now)
	if err != nil {
		t.Fatal(err)
	}
	if got[fresh.ID] != flash.CardStatusNew || got[due.ID] != flash.CardStatusDue {
		t.Errorf("CardStatuses = %v, want fresh=new and due=due", got)
	}
	if s, ok := got[later.ID]; ok {
		t.Errorf("a card scheduled days out has status %q, want none", s)
	}
}

// saveCardFormDecks gives Alice two decks, so SaveCardForm's deck scoping
// has somewhere wrong to be pointed at.
func saveCardFormDecks(t *testing.T, f *fixture) (flash.Deck, flash.Deck) {
	t.Helper()
	ctx := context.Background()
	d1, err := f.store.CreateDeck(ctx, f.alice.ID, "Spanish", "", flash.DefaultDeckColor)
	if err != nil {
		t.Fatal(err)
	}
	d2, err := f.store.CreateDeck(ctx, f.alice.ID, "French", "", flash.DefaultDeckColor)
	if err != nil {
		t.Fatal(err)
	}
	return d1, d2
}

func cardTagNames(t *testing.T, f *fixture, userID, cardID int64) []string {
	t.Helper()
	tags, err := f.store.TagsForCard(context.Background(), userID, cardID)
	if err != nil {
		t.Fatalf("TagsForCard: %v", err)
	}
	var out []string
	for _, tg := range tags {
		out = append(out, tg.Name)
	}
	return out
}

func countTags(t *testing.T, f *fixture, userID int64) int {
	t.Helper()
	var n int
	if err := f.db.QueryRowContext(context.Background(),
		`SELECT COUNT(*) FROM flash_tags WHERE user_id = ?`, userID).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestSaveCardFormCreatesCardTagsAndImage(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	deck, _ := saveCardFormDecks(t, f)

	c, err := f.store.SaveCardForm(ctx, f.alice.ID, deck.ID, 0, flash.CardForm{
		CardType: flash.CardTypeBasic, Front: "hola", Back: "hello",
		Tags:  []string{"verbs", "a1"},
		Image: &flash.CardUpload{ContentType: "image/png", Data: onePNG},
	})
	if err != nil {
		t.Fatalf("SaveCardForm: %v", err)
	}
	if c.ID == 0 || c.Front != "hola" || c.Back != "hello" {
		t.Errorf("card = %+v", c)
	}
	if c.ImageHash == nil {
		t.Error("ImageHash is nil; the upload was not attached")
	}
	if c.AudioHash != nil {
		t.Errorf("AudioHash = %q, want nil", *c.AudioHash)
	}
	if got := cardTagNames(t, f, f.alice.ID, c.ID); len(got) != 2 || got[0] != "a1" || got[1] != "verbs" {
		t.Errorf("tags = %v, want [a1 verbs]", got)
	}
}

func TestSaveCardFormUpdatesAndRemoves(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	deck, _ := saveCardFormDecks(t, f)

	c, err := f.store.SaveCardForm(ctx, f.alice.ID, deck.ID, 0, flash.CardForm{
		CardType: flash.CardTypeBasic, Front: "hola", Back: "hello",
		Tags:  []string{"x"},
		Image: &flash.CardUpload{ContentType: "image/png", Data: onePNG},
		Audio: &flash.CardUpload{ContentType: "audio/mpeg", Data: oneMP3},
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if c.ImageHash == nil || c.AudioHash == nil {
		t.Fatalf("created card is missing media: %+v", c)
	}
	image := *c.ImageHash

	got, err := f.store.SaveCardForm(ctx, f.alice.ID, deck.ID, c.ID, flash.CardForm{
		CardType: flash.CardTypeBasic, Front: "adios", Back: "bye",
		Tags:        []string{"y"},
		RemoveAudio: true,
	})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if got.ID != c.ID || got.Front != "adios" || got.Back != "bye" {
		t.Errorf("updated card = %+v", got)
	}
	if got.ImageHash == nil || *got.ImageHash != image {
		t.Errorf("ImageHash = %v, want %q kept", got.ImageHash, image)
	}
	if got.AudioHash != nil {
		t.Errorf("AudioHash = %q, want removed", *got.AudioHash)
	}
	if tags := cardTagNames(t, f, f.alice.ID, c.ID); len(tags) != 1 || tags[0] != "y" {
		t.Errorf("tags = %v, want [y]", tags)
	}
	var n int
	if err := f.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM flash_tags WHERE user_id = ? AND name = 'x'`, f.alice.ID).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Error("tag x is still in flash_tags; the replaced tag was not garbage collected")
	}
}

func TestSaveCardFormNewFileWinsOverRemove(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	deck, _ := saveCardFormDecks(t, f)

	c, err := f.store.SaveCardForm(ctx, f.alice.ID, deck.ID, 0, flash.CardForm{
		CardType: flash.CardTypeBasic, Front: "hola", Back: "hello",
		Image: &flash.CardUpload{ContentType: "image/png", Data: onePNG},
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	got, err := f.store.SaveCardForm(ctx, f.alice.ID, deck.ID, c.ID, flash.CardForm{
		CardType: flash.CardTypeBasic, Front: "hola", Back: "hello",
		Image:       &flash.CardUpload{ContentType: "image/png", Data: onePNG2},
		RemoveImage: true,
	})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	fresh, err := f.store.SaveMediaUpload(ctx, flash.MediaKindImage, "image/png", onePNG2, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if got.ImageHash == nil || *got.ImageHash != fresh {
		t.Errorf("ImageHash = %v, want the new file's %q (#328)", got.ImageHash, fresh)
	}
}

// failMediaInserts makes every flash_media INSERT abort, so a SaveCardForm
// with an upload fails at its last write step — after the card and tag
// writes that must then be rolled back with it.
func failMediaInserts(t *testing.T, f *fixture) {
	t.Helper()
	if _, err := f.db.ExecContext(context.Background(), `CREATE TRIGGER fail_media BEFORE INSERT ON flash_media
		BEGIN SELECT RAISE(ABORT, 'boom'); END`); err != nil {
		t.Fatal(err)
	}
}

func TestSaveCardFormIsAtomic(t *testing.T) {
	upload := &flash.CardUpload{ContentType: "image/png", Data: onePNG}

	t.Run("create", func(t *testing.T) {
		f := newFixture(t)
		ctx := context.Background()
		deck, _ := saveCardFormDecks(t, f)
		failMediaInserts(t, f)

		if _, err := f.store.SaveCardForm(ctx, f.alice.ID, deck.ID, 0, flash.CardForm{
			CardType: flash.CardTypeBasic, Front: "hola", Back: "hello", Tags: []string{"t"}, Image: upload,
		}); err == nil {
			t.Fatal("create with a failing media insert succeeded")
		}
		cards, err := f.store.ListCards(ctx, f.alice.ID, deck.ID)
		if err != nil {
			t.Fatal(err)
		}
		if len(cards) != 0 {
			t.Errorf("deck has %d cards after a failed create, want 0", len(cards))
		}
		if n := countTags(t, f, f.alice.ID); n != 0 {
			t.Errorf("Alice has %d flash_tags rows after a failed create, want 0", n)
		}
	})

	t.Run("update", func(t *testing.T) {
		f := newFixture(t)
		ctx := context.Background()
		deck, _ := saveCardFormDecks(t, f)
		c, err := f.store.SaveCardForm(ctx, f.alice.ID, deck.ID, 0, flash.CardForm{
			CardType: flash.CardTypeBasic, Front: "hola", Back: "hello", Tags: []string{"old"},
		})
		if err != nil {
			t.Fatalf("seed card: %v", err)
		}
		failMediaInserts(t, f)

		if _, err := f.store.SaveCardForm(ctx, f.alice.ID, deck.ID, c.ID, flash.CardForm{
			CardType: flash.CardTypeBasic, Front: "adios", Back: "bye", Tags: []string{"new"}, Image: upload,
		}); err == nil {
			t.Fatal("update with a failing media insert succeeded")
		}
		got, err := f.store.CardByID(ctx, f.alice.ID, deck.ID, c.ID)
		if err != nil {
			t.Fatal(err)
		}
		if got.Front != "hola" {
			t.Errorf("Front = %q after a failed update, want %q", got.Front, "hola")
		}
		if tags := cardTagNames(t, f, f.alice.ID, c.ID); len(tags) != 1 || tags[0] != "old" {
			t.Errorf("tags = %v after a failed update, want [old]", tags)
		}
	})
}

func TestSaveCardFormOwnership(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	deck, other := saveCardFormDecks(t, f)
	form := flash.CardForm{CardType: flash.CardTypeBasic, Front: "hola", Back: "hello"}

	if _, err := f.store.SaveCardForm(ctx, f.bob.ID, deck.ID, 0, form); !errors.Is(err, flash.ErrNotFound) {
		t.Errorf("Bob creating in Alice's deck = %v, want ErrNotFound", err)
	}
	cards, err := f.store.ListCards(ctx, f.alice.ID, deck.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(cards) != 0 {
		t.Errorf("Bob's failed create left %d cards in Alice's deck", len(cards))
	}

	c, err := f.store.SaveCardForm(ctx, f.alice.ID, deck.ID, 0, form)
	if err != nil {
		t.Fatal(err)
	}
	changed := flash.CardForm{CardType: flash.CardTypeBasic, Front: "pwned", Back: "pwned", Tags: []string{"bob"}}
	if _, err := f.store.SaveCardForm(ctx, f.bob.ID, deck.ID, c.ID, changed); !errors.Is(err, flash.ErrNotFound) {
		t.Errorf("Bob updating Alice's card = %v, want ErrNotFound", err)
	}
	if _, err := f.store.SaveCardForm(ctx, f.alice.ID, other.ID, c.ID, changed); !errors.Is(err, flash.ErrNotFound) {
		t.Errorf("Alice updating her card via her other deck = %v, want ErrNotFound", err)
	}
	got, err := f.store.CardByID(ctx, f.alice.ID, deck.ID, c.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Front != "hola" {
		t.Errorf("Front = %q, want the card unchanged", got.Front)
	}
	if tags := cardTagNames(t, f, f.alice.ID, c.ID); len(tags) != 0 {
		t.Errorf("tags = %v, want none", tags)
	}
}

func TestSaveCardFormValidates(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	deck, _ := saveCardFormDecks(t, f)

	if _, err := f.store.SaveCardForm(ctx, f.alice.ID, deck.ID, 0, flash.CardForm{
		CardType: flash.CardTypeBasic, Front: "  ", Back: "hello", Tags: []string{"t"},
	}); !errors.Is(err, flash.ErrInvalid) {
		t.Errorf("empty front = %v, want ErrInvalid", err)
	}
	if _, err := f.store.SaveCardForm(ctx, f.alice.ID, deck.ID, 0, flash.CardForm{
		CardType: flash.CardTypeBasic, Front: "hola", Back: "hello",
		Tags: []string{strings.Repeat("x", flash.MaxTagNameRunes+1)},
	}); !errors.Is(err, flash.ErrInvalid) {
		t.Errorf("over-long tag = %v, want ErrInvalid", err)
	}
	cards, err := f.store.ListCards(ctx, f.alice.ID, deck.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(cards) != 0 {
		t.Errorf("invalid saves left %d cards", len(cards))
	}
	if n := countTags(t, f, f.alice.ID); n != 0 {
		t.Errorf("invalid saves left %d tags", n)
	}
}
