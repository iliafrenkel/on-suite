// internal/apps/flash/card_view.go
package flash

import (
	"html/template"
	"net/url"
	"strconv"
	"strings"
)

// cardFace is everything any card template draws — the grid's mini card,
// the opened card, the editor preview and the review card all take one
// (UI overhaul spec §1.3). A projection, not the Card itself: Question and
// Answer are already-safe HTML from renderCardFaces.
type cardFace struct {
	ID, DeckID int64
	Color      string // the deck's colour name, for a deck-c-* class
	IsCloze    bool
	Question   template.HTML
	Answer     template.HTML
	Notes      string
	Tags       []tagChip // Href filters this deck's grid by the tag
	ImageURL   string
	AudioURL   string
}

func newCardFace(c Card, d Deck, tagNames []string) cardFace {
	q, a := renderCardFaces(c)
	chips := make([]tagChip, len(tagNames))
	for i, name := range tagNames {
		chips[i] = tagChip{Name: name, Href: cardsURL(d.ID, "", name)}
	}
	return cardFace{
		ID: c.ID, DeckID: d.ID, Color: d.Color, IsCloze: c.CardType == CardTypeCloze,
		Question: q, Answer: a, Notes: c.Notes, Tags: chips,
		ImageURL: mediaURL(c.ImageHash), AudioURL: mediaURL(c.AudioHash),
	}
}

// filterQuery is the ?q=…&tag=… suffix shared by cardsURL and cardURL.
func filterQuery(q, tag string) string {
	v := url.Values{}
	if q != "" {
		v.Set("q", q)
	}
	if tag != "" {
		v.Set("tag", tag)
	}
	if len(v) == 0 {
		return ""
	}
	return "?" + v.Encode()
}

// cardsURL is a deck's cards grid, optionally filtered.
func cardsURL(deckID int64, q, tag string) string {
	return cardBasePath(deckID) + filterQuery(q, tag)
}

// cardURL is one opened card, carrying the grid's filter so previous/next
// and "All cards" keep it.
func cardURL(deckID, cardID int64, q, tag string) string {
	return cardBasePath(deckID) + strconv.FormatInt(cardID, 10) + filterQuery(q, tag)
}

// filterCards keeps the cards matching q (a case-insensitive substring of
// front, back or notes) and tag (one of the card's tags, compared the way
// tag names are stored). Empty q or tag means no filter on that part.
func filterCards(cards []Card, tags map[int64][]string, q, tag string) []Card {
	q = strings.ToLower(strings.TrimSpace(q))
	tag = normalizeTagName(tag)
	var out []Card
	for _, c := range cards {
		if tag != "" && !containsString(tags[c.ID], tag) {
			continue
		}
		if q != "" &&
			!strings.Contains(strings.ToLower(c.Front), q) &&
			!strings.Contains(strings.ToLower(c.Back), q) &&
			!strings.Contains(strings.ToLower(c.Notes), q) {
			continue
		}
		out = append(out, c)
	}
	return out
}

func containsString(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

// cardGridItem is one mini card. InPane makes its link an HTMX swap into
// the deck pane (the cards grid); the cross-deck tag page's items are plain
// links instead, and show DeckName.
type cardGridItem struct {
	Face     cardFace
	Status   string // CardStatusNew, CardStatusDue or ""
	Href     string
	DeckName string
	InPane   bool
}

// tagPill is one filter pill above the grid.
type tagPill struct {
	Label  string
	Href   string
	Active bool
}

// cardGridView is the right pane in "cards" mode.
type cardGridView struct {
	Deck           Deck
	Query, Tag     string
	Pills          []tagPill
	Items          []cardGridItem
	ClearURL       string // the unfiltered grid
	AllDecksTagURL string // the cross-deck tag page, when Tag is set
	OOB            bool   // set on the live-search fragment, for the pills' out-of-band copy
}

// openedCardView is the right pane in "card" mode.
type openedCardView struct {
	Deck      Deck
	Face      cardFace
	BackURL   string // the grid, with the filter kept
	PrevURL   string // "" at the start of the filtered order
	NextURL   string // "" at the end
	CSRFToken string
}
