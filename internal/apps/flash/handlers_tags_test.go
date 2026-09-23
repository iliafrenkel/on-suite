// internal/apps/flash/handlers_tags_test.go
package flash_test

import (
	"net/url"
	"strings"
	"testing"

	"github.com/iliafrenkel/on-suite/internal/apps/flash"
	"github.com/iliafrenkel/on-suite/internal/htmlassert"
)

func TestCreatingACardSetsItsTags(t *testing.T) {
	s := newServer(t)
	deck, err := s.Store.CreateDeck(t.Context(), s.Alice.User.ID, "Spanish", "")
	if err != nil {
		t.Fatal(err)
	}
	s.Submit(t, s.Alice, "/flash/"+itoa(deck.ID)+"/cards/new",
		url.Values{"card_type": {"basic"}, "front": {"hola"}, "back": {"hello"}, "tags": {"greetings, beginner"}},
		"/flash/"+itoa(deck.ID)+"/cards/1")

	cards, err := s.Store.ListCards(t.Context(), s.Alice.User.ID, deck.ID)
	if err != nil || len(cards) != 1 {
		t.Fatalf("ListCards = %v, %v", cards, err)
	}
	tags, err := s.Store.TagsForCard(t.Context(), s.Alice.User.ID, cards[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(tags) != 2 {
		t.Errorf("TagsForCard = %v, want 2 tags", tags)
	}
}

func TestTagFilterViewListsCardsAcrossDecks(t *testing.T) {
	s := newServer(t)
	deckA, _ := s.Store.CreateDeck(t.Context(), s.Alice.User.ID, "A", "")
	deckB, _ := s.Store.CreateDeck(t.Context(), s.Alice.User.ID, "B", "")
	s.Submit(t, s.Alice, "/flash/"+itoa(deckA.ID)+"/cards/new",
		url.Values{"card_type": {"basic"}, "front": {"a1"}, "back": {"x"}, "tags": {"hard"}},
		"/flash/"+itoa(deckA.ID)+"/cards/1")
	s.Submit(t, s.Alice, "/flash/"+itoa(deckB.ID)+"/cards/new",
		url.Values{"card_type": {"basic"}, "front": {"b1"}, "back": {"x"}, "tags": {"hard"}},
		"/flash/"+itoa(deckB.ID)+"/cards/2")

	doc := s.Get(t, s.Alice, "/flash/tags/hard")
	items := doc.QueryAll(".tag-filter-item")
	if len(items) != 2 {
		t.Errorf("GET /flash/tags/hard shows %d cards, want 2", len(items))
	}
}

// TestCreateCardRejectsOverlongTagWithoutPersisting guards against the tag
// name check running only after the card is already written: an invalid tag
// must re-render the new-card form with an error, exactly like an invalid
// card field does, and must leave no card behind to retry into a duplicate.
func TestCreateCardRejectsOverlongTagWithoutPersisting(t *testing.T) {
	s := newServer(t)
	deck, err := s.Store.CreateDeck(t.Context(), s.Alice.User.ID, "Spanish", "")
	if err != nil {
		t.Fatal(err)
	}
	tooLong := strings.Repeat("a", flash.MaxTagNameRunes+1)

	rec := s.Post(t, s.Alice, "/flash/"+itoa(deck.ID)+"/cards/new",
		url.Values{"card_type": {"basic"}, "front": {"hola"}, "back": {"hello"}, "tags": {tooLong}})
	if rec.Code != 400 {
		t.Errorf("POST .../cards/new with an overlong tag = %d, want 400", rec.Code)
	}
	doc := htmlassert.Parse(t, rec.Body.String())
	doc.MustHave(".notice-error")

	cards, err := s.Store.ListCards(t.Context(), s.Alice.User.ID, deck.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(cards) != 0 {
		t.Errorf("ListCards = %v, want no card created after the tag rejection", cards)
	}
}

// TestUpdateCardRejectsOverlongTagWithoutMutating mirrors the create-side
// test above for the edit path: the existing card must be left untouched
// when its new tags are invalid.
func TestUpdateCardRejectsOverlongTagWithoutMutating(t *testing.T) {
	s := newServer(t)
	deck, err := s.Store.CreateDeck(t.Context(), s.Alice.User.ID, "Spanish", "")
	if err != nil {
		t.Fatal(err)
	}
	s.Submit(t, s.Alice, "/flash/"+itoa(deck.ID)+"/cards/new",
		url.Values{"card_type": {"basic"}, "front": {"hola"}, "back": {"hello"}, "tags": {"greetings"}},
		"/flash/"+itoa(deck.ID)+"/cards/1")
	tooLong := strings.Repeat("a", flash.MaxTagNameRunes+1)

	rec := s.Post(t, s.Alice, "/flash/"+itoa(deck.ID)+"/cards/1",
		url.Values{"card_type": {"basic"}, "front": {"changed"}, "back": {"changed"}, "tags": {tooLong}})
	if rec.Code != 400 {
		t.Errorf("POST .../cards/1 with an overlong tag = %d, want 400", rec.Code)
	}
	doc := htmlassert.Parse(t, rec.Body.String())
	doc.MustHave(".notice-error")

	cards, err := s.Store.ListCards(t.Context(), s.Alice.User.ID, deck.ID)
	if err != nil || len(cards) != 1 {
		t.Fatalf("ListCards = %v, %v", cards, err)
	}
	if cards[0].Front != "hola" {
		t.Errorf("card front = %q, want unchanged %q", cards[0].Front, "hola")
	}
	tags, err := s.Store.TagsForCard(t.Context(), s.Alice.User.ID, cards[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(tags) != 1 || tags[0].Name != "greetings" {
		t.Errorf("TagsForCard = %v, want unchanged [greetings]", tags)
	}
}

// TestTagChipHrefEscapesSlash guards against a tag name containing a "/"
// producing a broken link: html/template's auto-escaping percent-escapes
// quotes but leaves "/" alone, so the href must be built (and escaped) in Go
// rather than interpolated straight into the template.
func TestTagChipHrefEscapesSlash(t *testing.T) {
	s := newServer(t)
	deck, err := s.Store.CreateDeck(t.Context(), s.Alice.User.ID, "Spanish", "")
	if err != nil {
		t.Fatal(err)
	}
	s.Submit(t, s.Alice, "/flash/"+itoa(deck.ID)+"/cards/new",
		url.Values{"card_type": {"basic"}, "front": {"hola"}, "back": {"hello"}, "tags": {"a/b"}},
		"/flash/"+itoa(deck.ID)+"/cards/1")

	doc := s.Get(t, s.Alice, "/flash/"+itoa(deck.ID)+"/cards/1")
	link := doc.MustHave(".flash-tag-links a")
	href, ok := htmlassert.Attr(link, "href")
	if !ok {
		t.Fatal("tag chip link has no href")
	}
	// Tag links now filter this deck's own grid (UI overhaul spec §3); the
	// tag still has to be escaped, as a query value.
	if want := "/flash/" + itoa(deck.ID) + "/cards/?tag=a%2Fb"; href != want {
		t.Errorf("tag chip href = %q, want %q", href, want)
	}
}

func TestTagFilterPageShowsMiniCardsWithDeckNames(t *testing.T) {
	s := newServer(t)
	deckA, _ := s.Store.CreateDeck(t.Context(), s.Alice.User.ID, "Alpha", "")
	s.Submit(t, s.Alice, "/flash/"+itoa(deckA.ID)+"/cards/new",
		url.Values{"card_type": {"basic"}, "front": {"a1"}, "back": {"x"}, "tags": {"hard"}},
		"/flash/"+itoa(deckA.ID)+"/cards/1")
	doc := s.Get(t, s.Alice, "/flash/tags/hard")
	card := doc.MustHave(".tag-filter-item a.flash-mini-card")
	if href, _ := htmlassert.Attr(card, "href"); href != "/flash/"+itoa(deckA.ID)+"/cards/1" {
		t.Errorf("mini card href = %q", href)
	}
	if got := htmlassert.Text(doc.MustHave(".tag-filter-item .flash-mini-deck")); got != "Alpha" {
		t.Errorf("deck label = %q, want Alpha", got)
	}
}
