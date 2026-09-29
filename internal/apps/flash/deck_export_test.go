// internal/apps/flash/deck_export_test.go
package flash_test

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/iliafrenkel/on-suite/internal/apps/flash"
	"github.com/iliafrenkel/on-suite/internal/platform/app"
)

// exportSeed builds one of Alice's decks the way a user would: two cards
// imported (one with an image URL, one cloze with tags and notes) and one
// made in the editor with an uploaded image.
func exportSeed(t *testing.T, store *flash.Store, userID int64) flash.Deck {
	t.Helper()
	ctx := context.Background()
	parsed, err := flash.ParseImport(`{
		"deck": {"name": "Spanish", "description": "Everyday words"},
		"cards": [
			{"front": "perro", "back": "dog", "image": "https://example.com/dog.png"},
			{"type": "cloze", "front": "El {{c1::gato}} duerme", "notes": "gato = cat", "tags": ["animals", "a1"]}
		]
	}`, "json")
	if err != nil {
		t.Fatal(err)
	}
	deck, err := store.ImportDeck(ctx, userID, parsed.Name, parsed.Description, parsed.Cards)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.SaveCardForm(ctx, userID, deck.ID, 0, flash.CardForm{
		CardType: flash.CardTypeBasic, Front: "casa", Back: "house",
		Image: &flash.CardUpload{ContentType: "image/png", Data: onePNG},
	}); err != nil {
		t.Fatal(err)
	}
	return deck
}

// The in-app export is the Import format itself (#426), so a deck exported
// here can be imported back, or handed to someone, as is.
func TestDeckExportRoundTripsThroughImport(t *testing.T) {
	s := newServer(t)
	deck := exportSeed(t, s.Store, s.Alice.User.ID)

	rec := s.Do(t, s.Alice, httpGet(t, "/flash/export/"+itoa(deck.ID)))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /flash/export = %d; body: %s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Content-Type"); !strings.HasPrefix(got, "application/json") {
		t.Errorf("Content-Type = %q, want application/json", got)
	}
	// A generic name, never one derived from the deck's own title.
	if got := rec.Header().Get("Content-Disposition"); got != `attachment; filename="flash-deck.json"` {
		t.Errorf("Content-Disposition = %q", got)
	}

	parsed, err := flash.ParseImport(rec.Body.String(), "json")
	if err != nil {
		t.Fatalf("the export does not import back: %v\n%s", err, rec.Body.String())
	}
	if parsed.Name != "Spanish" || parsed.Description != "Everyday words" {
		t.Errorf("deck = %q / %q", parsed.Name, parsed.Description)
	}
	if len(parsed.Cards) != 3 {
		t.Fatalf("got %d cards, want 3", len(parsed.Cards))
	}
	byFront := map[string]int{}
	for i, c := range parsed.Cards {
		byFront[c.Front] = i
	}
	dog := parsed.Cards[byFront["perro"]]
	if dog.Back != "dog" || dog.ImageURL != "https://example.com/dog.png" {
		t.Errorf("perro card = %+v, want back dog and its image URL", dog)
	}
	cat := parsed.Cards[byFront["El {{c1::gato}} duerme"]]
	if cat.CardType != flash.CardTypeCloze || cat.Notes != "gato = cat" || strings.Join(cat.Tags, ",") != "a1,animals" {
		t.Errorf("cloze card = %+v", cat)
	}
	// An uploaded image has no URL, and Import only takes URLs, so it cannot
	// travel; the card itself still does.
	house := parsed.Cards[byFront["casa"]]
	if house.Back != "house" || house.ImageURL != "" {
		t.Errorf("casa card = %+v, want no image", house)
	}
}

func TestDeckExportOfAnotherUsersDeckIs404(t *testing.T) {
	s := newServer(t)
	deck := exportSeed(t, s.Store, s.Bob.User.ID)

	rec := s.Do(t, s.Alice, httpGet(t, "/flash/export/"+itoa(deck.ID)))
	if rec.Code != http.StatusNotFound {
		t.Errorf("GET /flash/export of bob's deck = %d, want 404", rec.Code)
	}
}

func TestDeckToolbarHasAnExportLink(t *testing.T) {
	s := newServer(t)
	deck := exportSeed(t, s.Store, s.Alice.User.ID)

	doc := s.Get(t, s.Alice, "/flash/"+itoa(deck.ID))
	doc.MustHave(`.flash-deck-toolbar a[href="/flash/export/` + itoa(deck.ID) + `"]`)
}

func TestFlashImplementsTheExporterInterface(t *testing.T) {
	var _ app.Exporter = flash.New()
}

// backup mirrors the JSON App.Export produces, for reading it back in tests.
type backup struct {
	Decks []struct {
		Name           string  `json:"name"`
		Description    string  `json:"description"`
		Color          string  `json:"color"`
		NewCardsPerDay int     `json:"new_cards_per_day"`
		ReviewsPerDay  *int    `json:"reviews_per_day"`
		SnoozedUntil   *string `json:"snoozed_until"`
		Cards          []struct {
			Type          string   `json:"type"`
			Front         string   `json:"front"`
			Tags          []string `json:"tags"`
			ImageURL      string   `json:"image_url"`
			ImageUploaded bool     `json:"image_uploaded"`
			Schedule      *struct {
				State string  `json:"state"`
				DueAt string  `json:"due_at"`
				Reps  float64 `json:"reps"`
			} `json:"schedule"`
		} `json:"cards"`
		ReviewDays []struct {
			Day  string `json:"day"`
			New  int    `json:"new"`
			Good int    `json:"good"`
		} `json:"review_days"`
	} `json:"decks"`
}

// onsuite export's Flash section (#426): decks with their settings, cards
// with tags and media, each card's review schedule, and the per-day review
// counts behind the streak and chart. Media is a URL or a flag, never bytes.
func TestJSONExportCarriesDecksCardsScheduleAndReviewDays(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	deck := exportSeed(t, f.store, f.alice.ID)
	exportSeed(t, f.store, f.bob.ID) // must not appear in Alice's export

	limit := 50
	if _, err := f.store.UpdateDeck(ctx, f.alice.ID, deck.ID, deck.Name, deck.Description, "coral", 10, &limit); err != nil {
		t.Fatal(err)
	}
	cards, err := f.store.ListCards(ctx, f.alice.ID, deck.ID)
	if err != nil {
		t.Fatal(err)
	}
	var dogID int64
	for _, c := range cards {
		if c.Front == "perro" {
			dogID = c.ID
		}
	}
	reviewedAt := time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)
	if _, err := f.store.GradeCard(ctx, f.alice.ID, dogID, flash.RatingGood, reviewedAt); err != nil {
		t.Fatal(err)
	}

	data, err := flash.New().Export(ctx, f.db, f.alice.ID)
	if err != nil {
		t.Fatalf("Export: %v", err)
	}
	raw, err := json.Marshal(data)
	if err != nil {
		t.Fatal(err)
	}
	var got backup
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}

	if len(got.Decks) != 1 {
		t.Fatalf("got %d decks, want Alice's one:\n%s", len(got.Decks), raw)
	}
	d := got.Decks[0]
	if d.Name != "Spanish" || d.Color != "coral" || d.NewCardsPerDay != 10 || d.ReviewsPerDay == nil || *d.ReviewsPerDay != 50 {
		t.Errorf("deck settings = %+v", d)
	}
	if d.SnoozedUntil != nil {
		t.Errorf("snoozed_until = %q, want absent for a deck that isn't snoozed", *d.SnoozedUntil)
	}
	if len(d.Cards) != 3 {
		t.Fatalf("got %d cards, want 3:\n%s", len(d.Cards), raw)
	}
	for _, c := range d.Cards {
		switch c.Front {
		case "perro":
			if c.ImageURL != "https://example.com/dog.png" || c.ImageUploaded {
				t.Errorf("perro media = %q uploaded=%v", c.ImageURL, c.ImageUploaded)
			}
			if c.Schedule == nil || c.Schedule.DueAt == "" || c.Schedule.Reps != 1 {
				t.Errorf("perro schedule = %+v, want one review", c.Schedule)
			}
		case "casa":
			if c.ImageURL != "" || !c.ImageUploaded {
				t.Errorf("casa media = %q uploaded=%v, want flagged as an upload with no URL", c.ImageURL, c.ImageUploaded)
			}
			if c.Schedule != nil {
				t.Errorf("casa schedule = %+v, want none for a never-reviewed card", c.Schedule)
			}
		case "El {{c1::gato}} duerme":
			if c.Type != flash.CardTypeCloze || strings.Join(c.Tags, ",") != "a1,animals" {
				t.Errorf("cloze card = %+v", c)
			}
		}
	}
	if len(d.ReviewDays) != 1 || d.ReviewDays[0].Day != "2026-09-25" || d.ReviewDays[0].New != 1 || d.ReviewDays[0].Good != 1 {
		t.Errorf("review_days = %+v, want one new Good review on 2026-09-25", d.ReviewDays)
	}
	if strings.Contains(string(raw), "iVBOR") {
		t.Error("the export embeds media bytes; it should carry only URLs")
	}
}

func TestJSONExportOfAUserWithNoDecksIsEmpty(t *testing.T) {
	f := newFixture(t)
	data, err := flash.New().Export(context.Background(), f.db, f.alice.ID)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(data)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != `{"decks":[]}` {
		t.Errorf("export = %s, want an empty deck list", raw)
	}
}
