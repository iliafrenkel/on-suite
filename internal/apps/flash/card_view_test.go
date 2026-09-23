// internal/apps/flash/card_view_test.go
package flash

import (
	"strings"
	"testing"
)

func TestCardsURL(t *testing.T) {
	if got := cardsURL(7, "", ""); got != "/flash/7/cards/" {
		t.Errorf("cardsURL no filter = %q", got)
	}
	if got := cardsURL(7, "hola amigo", "a/b"); got != "/flash/7/cards/?q=hola+amigo&tag=a%2Fb" {
		t.Errorf("cardsURL with filter = %q", got)
	}
	if got := cardURL(7, 9, "", "food"); got != "/flash/7/cards/9?tag=food" {
		t.Errorf("cardURL = %q", got)
	}
}

func TestFilterCards(t *testing.T) {
	cards := []Card{
		{ID: 1, Front: "Hola", Back: "hello"},
		{ID: 2, Front: "pan", Back: "bread", Notes: "Also a PAN for cooking"},
		{ID: 3, Front: "agua", Back: "water"},
	}
	tags := map[int64][]string{1: {"greetings"}, 2: {"food"}, 3: {"food"}}

	ids := func(cs []Card) string {
		var b strings.Builder
		for _, c := range cs {
			b.WriteByte(byte('0' + c.ID))
		}
		return b.String()
	}
	for _, tt := range []struct{ q, tag, want string }{
		{"", "", "123"},
		{"hola", "", "1"},    // front, case-insensitive
		{"WATER", "", "3"},   // back
		{"cooking", "", "2"}, // notes
		{"", "food", "23"},
		{"", "FOOD ", "23"}, // tag is normalised like stored tag names
		{"agua", "food", "3"},
		{"zzz", "", ""},
	} {
		if got := ids(filterCards(cards, tags, tt.q, tt.tag)); got != tt.want {
			t.Errorf("filterCards(q=%q, tag=%q) = %q, want %q", tt.q, tt.tag, got, tt.want)
		}
	}
}

func TestNewCardFace(t *testing.T) {
	hash := "abc123"
	c := Card{ID: 4, DeckID: 2, CardType: CardTypeBasic, Front: "hola", Back: "hello", Notes: "informal", ImageHash: &hash}
	d := Deck{ID: 2, Color: "purple"}
	f := newCardFace(c, d, []string{"a/b"})
	if f.ID != 4 || f.DeckID != 2 || f.Color != "purple" || f.IsCloze {
		t.Errorf("face identity = %+v", f)
	}
	if string(f.Question) != "hola" || string(f.Answer) != "hello" || f.Notes != "informal" {
		t.Errorf("face text = %+v", f)
	}
	if f.ImageURL != "/flash/media/abc123" || f.AudioURL != "" {
		t.Errorf("media = %q / %q", f.ImageURL, f.AudioURL)
	}
	if len(f.Tags) != 1 || f.Tags[0].Name != "a/b" || f.Tags[0].Href != "/flash/2/cards/?tag=a%2Fb" {
		t.Errorf("tags = %+v", f.Tags)
	}
}
