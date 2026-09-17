// internal/apps/flash/handlers_tags_test.go
package flash_test

import (
	"net/url"
	"testing"

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
	link := doc.MustHave(".tag-list a")
	href, ok := htmlassert.Attr(link, "href")
	if !ok {
		t.Fatal("tag chip link has no href")
	}
	if want := "/flash/tags/a%2Fb"; href != want {
		t.Errorf("tag chip href = %q, want %q", href, want)
	}
}
