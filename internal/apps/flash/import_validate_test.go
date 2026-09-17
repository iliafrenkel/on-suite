package flash

import (
	"errors"
	"strings"
	"testing"
)

func TestParseImportJSONHappyPath(t *testing.T) {
	payload := `{
		"deck": {"name": "Spanish travel phrases", "description": "Travel basics"},
		"cards": [
			{"type": "basic", "front": "Thank you", "back": "Gracias", "tags": ["travel", "spanish"], "image": "http://x/y.jpg"},
			{"type": "cloze", "front": "The capital of France is {{c1::Paris}}.", "tags": ["geography"]}
		]
	}`
	d, err := ParseImport(payload, "json")
	if err != nil {
		t.Fatalf("ParseImport: %v", err)
	}
	if d.Name != "Spanish travel phrases" || d.Description != "Travel basics" {
		t.Errorf("deck = %+v", d)
	}
	if len(d.Cards) != 2 {
		t.Fatalf("len(Cards) = %d, want 2", len(d.Cards))
	}
	if d.Cards[0].CardType != CardTypeBasic || d.Cards[0].Back != "Gracias" {
		t.Errorf("card 0 = %+v", d.Cards[0])
	}
	if d.Cards[1].CardType != CardTypeCloze || d.Cards[1].Back != "" {
		t.Errorf("card 1 = %+v", d.Cards[1])
	}
}

func TestParseImportJSONDefaultsTypeToBasic(t *testing.T) {
	payload := `{"deck": {"name": "D"}, "cards": [{"front": "Q", "back": "A"}]}`
	d, err := ParseImport(payload, "json")
	if err != nil {
		t.Fatalf("ParseImport: %v", err)
	}
	if d.Cards[0].CardType != CardTypeBasic {
		t.Errorf("CardType = %q, want basic", d.Cards[0].CardType)
	}
}

func TestParseImportJSONMissingDeckName(t *testing.T) {
	payload := `{"deck": {}, "cards": [{"front": "Q", "back": "A"}]}`
	_, err := ParseImport(payload, "json")
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("err = %v, want ErrInvalid", err)
	}
}

func TestParseImportJSONEmptyCards(t *testing.T) {
	payload := `{"deck": {"name": "D"}, "cards": []}`
	_, err := ParseImport(payload, "json")
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("err = %v, want ErrInvalid", err)
	}
}

func TestParseImportJSONBadCardTypeNamesTheCard(t *testing.T) {
	payload := `{"deck": {"name": "D"}, "cards": [
		{"front": "Q", "back": "A"},
		{"type": "essay", "front": "Q2", "back": "A2"}
	]}`
	_, err := ParseImport(payload, "json")
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("err = %v, want ErrInvalid", err)
	}
	if !strings.Contains(err.Error(), "card 2") {
		t.Errorf("err = %v, want it to name card 2", err)
	}
}

func TestParseImportJSONMalformedIsAllOrNothing(t *testing.T) {
	payload := `{"deck": {"name": "D"}, "cards": [
		{"front": "Q", "back": "A"},
		{"front": "", "back": "A2"}
	]}`
	d, err := ParseImport(payload, "json")
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("err = %v, want ErrInvalid", err)
	}
	if len(d.Cards) != 0 {
		t.Errorf("d.Cards = %v, want none on error", d.Cards)
	}
}

func TestParseImportJSONClozeWithoutMarkerFails(t *testing.T) {
	payload := `{"deck": {"name": "D"}, "cards": [{"type": "cloze", "front": "no marker here"}]}`
	_, err := ParseImport(payload, "json")
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("err = %v, want ErrInvalid", err)
	}
}

func TestParseImportJSONBadTagFails(t *testing.T) {
	longTag := strings.Repeat("a", MaxTagNameRunes+1)
	payload := `{"deck": {"name": "D"}, "cards": [{"front": "Q", "back": "A", "tags": ["` + longTag + `"]}]}`
	_, err := ParseImport(payload, "json")
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("err = %v, want ErrInvalid", err)
	}
}

func TestParseImportJSONUnknownFieldsIgnored(t *testing.T) {
	payload := `{"deck": {"name": "D", "future_field": 1}, "cards": [{"front": "Q", "back": "A", "future_field": true}]}`
	d, err := ParseImport(payload, "json")
	if err != nil {
		t.Fatalf("ParseImport: %v", err)
	}
	if len(d.Cards) != 1 {
		t.Fatalf("len(Cards) = %d, want 1", len(d.Cards))
	}
}

func TestParseImportInvalidJSONSyntax(t *testing.T) {
	_, err := ParseImport(`{not json`, "json")
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("err = %v, want ErrInvalid", err)
	}
}

func TestParseImportAutoDetectsJSON(t *testing.T) {
	payload := `  {"deck": {"name": "D"}, "cards": [{"front": "Q", "back": "A"}]}`
	d, err := ParseImport(payload, "auto")
	if err != nil {
		t.Fatalf("ParseImport: %v", err)
	}
	if d.Name != "D" {
		t.Errorf("Name = %q, want D", d.Name)
	}
}

func TestParseImportAutoDetectsMarkdown(t *testing.T) {
	payload := "# D\n\n## Card\nFront: Q\nBack: A\n"
	d, err := ParseImport(payload, "auto")
	if err != nil {
		t.Fatalf("ParseImport: %v", err)
	}
	if d.Name != "D" {
		t.Errorf("Name = %q, want D", d.Name)
	}
}

func TestParseImportUnknownFormat(t *testing.T) {
	_, err := ParseImport("anything", "yaml")
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("err = %v, want ErrInvalid", err)
	}
}

func TestParseImportMarkdownHappyPath(t *testing.T) {
	payload := "# Spanish travel phrases\n" +
		"Travel basics\n" +
		"\n" +
		"## Card\n" +
		"Type: basic\n" +
		"Front: Thank you\n" +
		"Back: Gracias\n" +
		"Tags: travel, spanish\n" +
		"Notes: Informal register\n" +
		"\n" +
		"## Card\n" +
		"Type: cloze\n" +
		"Front: The capital of France is {{c1::Paris}}.\n" +
		"Tags: geography\n"

	d, err := ParseImport(payload, "markdown")
	if err != nil {
		t.Fatalf("ParseImport: %v", err)
	}
	if d.Name != "Spanish travel phrases" || d.Description != "Travel basics" {
		t.Errorf("deck = %+v", d)
	}
	if len(d.Cards) != 2 {
		t.Fatalf("len(Cards) = %d, want 2", len(d.Cards))
	}
	c0 := d.Cards[0]
	if c0.CardType != CardTypeBasic || c0.Front != "Thank you" || c0.Back != "Gracias" || c0.Notes != "Informal register" {
		t.Errorf("card 0 = %+v", c0)
	}
	if len(c0.Tags) != 2 || c0.Tags[0] != "travel" || c0.Tags[1] != "spanish" {
		t.Errorf("card 0 tags = %v", c0.Tags)
	}
	c1 := d.Cards[1]
	if c1.CardType != CardTypeCloze || !strings.Contains(c1.Front, "{{c1::Paris}}") {
		t.Errorf("card 1 = %+v", c1)
	}
}

func TestParseImportMarkdownDefaultsTypeToBasic(t *testing.T) {
	payload := "# D\n\n## Card\nFront: Q\nBack: A\n"
	d, err := ParseImport(payload, "markdown")
	if err != nil {
		t.Fatalf("ParseImport: %v", err)
	}
	if d.Cards[0].CardType != CardTypeBasic {
		t.Errorf("CardType = %q, want basic", d.Cards[0].CardType)
	}
}

func TestParseImportMarkdownMultilineFront(t *testing.T) {
	payload := "# D\n\n## Card\nFront: Line one\nLine two\nBack: A\n"
	d, err := ParseImport(payload, "markdown")
	if err != nil {
		t.Fatalf("ParseImport: %v", err)
	}
	want := "Line one\nLine two"
	if d.Cards[0].Front != want {
		t.Errorf("Front = %q, want %q", d.Cards[0].Front, want)
	}
}

func TestParseImportMarkdownMissingHeading(t *testing.T) {
	payload := "## Card\nFront: Q\nBack: A\n"
	_, err := ParseImport(payload, "markdown")
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("err = %v, want ErrInvalid", err)
	}
}

func TestParseImportMarkdownNoCards(t *testing.T) {
	payload := "# D\nSome description\n"
	_, err := ParseImport(payload, "markdown")
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("err = %v, want ErrInvalid", err)
	}
}

func TestParseImportMarkdownBadCardTypeNamesTheCard(t *testing.T) {
	payload := "# D\n\n## Card\nFront: Q\nBack: A\n\n## Card\nType: essay\nFront: Q2\nBack: A2\n"
	_, err := ParseImport(payload, "markdown")
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("err = %v, want ErrInvalid", err)
	}
	if !strings.Contains(err.Error(), "card 2") {
		t.Errorf("err = %v, want it to name card 2", err)
	}
}

func TestParseImportMarkdownUnknownKeyIgnored(t *testing.T) {
	payload := "# D\n\n## Card\nFront: Q\nBack: A\nImage: http://example.com/x.jpg\n"
	d, err := ParseImport(payload, "markdown")
	if err != nil {
		t.Fatalf("ParseImport: %v", err)
	}
	if d.Cards[0].Back != "A" {
		t.Errorf("Back = %q, want unaffected by the unknown Image key", d.Cards[0].Back)
	}
}

func TestParseImportMarkdownClozeWithoutMarkerFails(t *testing.T) {
	payload := "# D\n\n## Card\nType: cloze\nFront: no marker here\n"
	_, err := ParseImport(payload, "markdown")
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("err = %v, want ErrInvalid", err)
	}
}
