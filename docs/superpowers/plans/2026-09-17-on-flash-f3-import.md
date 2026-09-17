# ON Flash F3 — Import Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let a user paste Markdown or JSON into an import screen and get a whole new deck (with cards, tags, and notes) created in one shot.

**Architecture:** A pure parser (`import.go`) turns a pasted string into a validated `parsedDeck`/`parsedCard` value with no database access. `Store.ImportDeck` (`import_store.go`) writes that value to the database inside one transaction — a new deck, its cards, and their tags — so a failure partway through leaves nothing behind. `handlers_import.go` wires an HTTP form to the two: `GET /import` renders the form, `POST /import` parses then writes, redirecting to the new deck on success.

**Tech Stack:** Go standard library only (`encoding/json`, `regexp`, `database/sql`) — no new dependencies.

## Global Constraints

- Import always creates a **new** deck. There is no "append to an existing deck" path.
- Import is **all-or-nothing**: the first invalid card aborts the whole import and writes nothing.
- No media handling. `image`/`audio` fields in the JSON schema are accepted (ignored by `encoding/json`'s default unknown-field behavior) but never read.
- The only input surface is a pasted-text `<textarea>` — no file upload.
- Format is auto-detected (valid JSON → JSON path, else Markdown), with a manual override (`auto`/`markdown`/`json`) the form can force.
- Reuse existing validation exactly: `ValidateCard`, `validateCardNotes`, `ValidateTagNames`, `ValidateDeck` from `card.go`/`tag.go`/`deck.go`. Do not duplicate field rules.
- flash's SQLite handle has `SetMaxOpenConns(1)` (`internal/platform/db/db.go`): a transaction must never call a method that opens its own transaction (e.g. `SetCardTags`, `CreateCard`) — it will deadlock waiting for the connection its own transaction is holding. Any insert done inside `ImportDeck`'s transaction must use the transaction's own `*sql.Tx` directly.
- **A new page-level template file must be named `*.partial.html`, not `*.html`, to be visible from another page's template set.** `internal/platform/render/render.go`'s `AddApp`/`addPage` registers every non-partial `*.html` file in `templates/` as its own independent, cloned template set containing only that file plus every `*.partial.html` file. A `{{template "name" .}}` call from `decks.html` referencing a definition that lives in a plain `.html` file (not `.partial.html`) fails at render time with `no such template "name"` — found during this plan's own validation, not a hypothetical.

---

### Task 1: Import parser

**Files:**
- Create: `internal/apps/flash/import.go`
- Test: `internal/apps/flash/import_validate_test.go`

**Interfaces:**
- Consumes: `ErrInvalid` (`flash.go`), `CardTypeBasic`/`CardTypeCloze`, `ValidateCard`, `validateCardNotes` (`card.go`), `ValidateTagNames` (`tag.go`), `ValidateDeck` (`deck.go`).
- Produces: `parsedCard{CardType, Front, Back, Notes string; Tags []string}`, `parsedDeck{Name, Description string; Cards []parsedCard}`, `func ParseImport(payload, format string) (parsedDeck, error)` — used by Task 3's handler and Task 2's tests.

- [ ] **Step 1: Write the failing tests**

Create `internal/apps/flash/import_validate_test.go`:

```go
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
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/apps/flash/... -run TestParseImport -v`
Expected: FAIL — `ParseImport`, `parsedDeck`, etc. are undefined.

- [ ] **Step 3: Write the implementation**

Create `internal/apps/flash/import.go`:

```go
// internal/apps/flash/import.go
package flash

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

// parsedCard is one card extracted from an import payload, already validated
// against the same rules ValidateCard/ValidateTagNames apply to hand-created
// cards.
type parsedCard struct {
	CardType string
	Front    string
	Back     string
	Notes    string
	Tags     []string
}

// parsedDeck is a whole import payload, parsed and validated.
type parsedDeck struct {
	Name        string
	Description string
	Cards       []parsedCard
}

// importJSON mirrors the documented JSON schema. Fields this package does
// not know about (image, audio, or anything future) are silently ignored by
// json.Unmarshal rather than rejected — see the design spec's
// forward-compatibility rule.
type importJSON struct {
	Deck struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	} `json:"deck"`
	Cards []struct {
		Type  string   `json:"type"`
		Front string   `json:"front"`
		Back  string   `json:"back"`
		Notes string   `json:"notes"`
		Tags  []string `json:"tags"`
	} `json:"cards"`
}

// ParseImport parses payload as format ("json", "markdown", or "auto") and
// returns a fully-validated deck, or the first validation error found.
// format=="" is treated the same as "auto".
func ParseImport(payload, format string) (parsedDeck, error) {
	switch format {
	case "json":
		return parseImportJSON(payload)
	case "markdown":
		return parseImportMarkdown(payload)
	case "auto", "":
		if looksLikeJSON(payload) {
			return parseImportJSON(payload)
		}
		return parseImportMarkdown(payload)
	default:
		return parsedDeck{}, fmt.Errorf("%w: unknown import format %q", ErrInvalid, format)
	}
}

func looksLikeJSON(payload string) bool {
	return strings.HasPrefix(strings.TrimSpace(payload), "{")
}

// stripErrInvalid removes ErrInvalid's own "flash: invalid input: " prefix so
// per-card messages can be composed as "card N: <reason>" without a
// duplicated prefix appearing in the middle of the sentence.
func stripErrInvalid(err error) string {
	const prefix = "flash: invalid input: "
	msg := err.Error()
	if strings.HasPrefix(msg, prefix) {
		return msg[len(prefix):]
	}
	return msg
}

func validateParsedCard(cardType, front, back, notes string, tags []string, cardNum int) error {
	if err := ValidateCard(cardType, front, back); err != nil {
		return fmt.Errorf("%w: card %d: %s", ErrInvalid, cardNum, stripErrInvalid(err))
	}
	if err := validateCardNotes(notes); err != nil {
		return fmt.Errorf("%w: card %d: %s", ErrInvalid, cardNum, stripErrInvalid(err))
	}
	if err := ValidateTagNames(tags); err != nil {
		return fmt.Errorf("%w: card %d: %s", ErrInvalid, cardNum, stripErrInvalid(err))
	}
	return nil
}

func parseImportJSON(payload string) (parsedDeck, error) {
	var raw importJSON
	if err := json.Unmarshal([]byte(payload), &raw); err != nil {
		return parsedDeck{}, fmt.Errorf("%w: invalid JSON: %v", ErrInvalid, err)
	}
	if len(raw.Cards) == 0 {
		return parsedDeck{}, fmt.Errorf("%w: the import needs at least one card", ErrInvalid)
	}

	deck := parsedDeck{
		Name:        strings.TrimSpace(raw.Deck.Name),
		Description: raw.Deck.Description,
	}
	if err := ValidateDeck(deck.Name, deck.Description); err != nil {
		return parsedDeck{}, err
	}

	for i, c := range raw.Cards {
		cardType := c.Type
		if cardType == "" {
			cardType = CardTypeBasic
		}
		if err := validateParsedCard(cardType, c.Front, c.Back, c.Notes, c.Tags, i+1); err != nil {
			return parsedDeck{}, err
		}
		deck.Cards = append(deck.Cards, parsedCard{
			CardType: cardType, Front: c.Front, Back: c.Back, Notes: c.Notes, Tags: c.Tags,
		})
	}
	return deck, nil
}

// markdownKeyLine matches a "Key: value" line anchored at the start of the
// line (no leading whitespace), so an indented continuation line is never
// mistaken for a new key even if it happens to contain a colon.
var markdownKeyLine = regexp.MustCompile(`^([A-Za-z]+):[ \t]*(.*)$`)

// parseCardBlock turns the lines inside one "## Card" block into a
// key->value map. A line matching markdownKeyLine with a recognized key
// starts (or restarts) the current field; a line matching it with an
// unrecognized key is dropped, along with any lines that follow until the
// next recognized key, so an unknown key never gets attributed to whatever
// field preceded it. Any other non-blank line is appended, as a
// continuation, to the current field.
func parseCardBlock(lines []string) map[string]string {
	fields := map[string]string{}
	currentKey := ""
	for _, line := range lines {
		if m := markdownKeyLine.FindStringSubmatch(line); m != nil {
			key := strings.ToLower(m[1])
			switch key {
			case "type", "front", "back", "tags", "notes":
				currentKey = key
				fields[key] = m[2]
			default:
				currentKey = ""
			}
			continue
		}
		if currentKey != "" && strings.TrimSpace(line) != "" {
			fields[currentKey] += "\n" + line
		}
	}
	return fields
}

func splitMarkdownTags(raw string) []string {
	if raw == "" {
		return nil
	}
	var tags []string
	for _, t := range strings.Split(raw, ",") {
		if t = strings.TrimSpace(t); t != "" {
			tags = append(tags, t)
		}
	}
	return tags
}

func parseImportMarkdown(payload string) (parsedDeck, error) {
	lines := strings.Split(strings.ReplaceAll(payload, "\r\n", "\n"), "\n")

	i := 0
	for i < len(lines) && strings.TrimSpace(lines[i]) == "" {
		i++
	}
	if i >= len(lines) || !strings.HasPrefix(lines[i], "# ") {
		return parsedDeck{}, fmt.Errorf("%w: the import needs a '# Deck Name' heading on the first line", ErrInvalid)
	}
	name := strings.TrimSpace(strings.TrimPrefix(lines[i], "# "))
	i++

	var descLines []string
	for i < len(lines) && strings.TrimSpace(lines[i]) != "## Card" {
		descLines = append(descLines, lines[i])
		i++
	}
	description := strings.TrimSpace(strings.Join(descLines, "\n"))

	deck := parsedDeck{Name: name, Description: description}
	if err := ValidateDeck(deck.Name, deck.Description); err != nil {
		return parsedDeck{}, err
	}

	cardNum := 0
	for i < len(lines) {
		if strings.TrimSpace(lines[i]) != "## Card" {
			i++
			continue
		}
		cardNum++
		i++
		start := i
		for i < len(lines) && strings.TrimSpace(lines[i]) != "## Card" {
			i++
		}
		fields := parseCardBlock(lines[start:i])

		cardType := fields["type"]
		if cardType == "" {
			cardType = CardTypeBasic
		}
		front, back, notes := fields["front"], fields["back"], fields["notes"]
		tags := splitMarkdownTags(fields["tags"])

		if err := validateParsedCard(cardType, front, back, notes, tags, cardNum); err != nil {
			return parsedDeck{}, err
		}
		deck.Cards = append(deck.Cards, parsedCard{CardType: cardType, Front: front, Back: back, Notes: notes, Tags: tags})
	}

	if len(deck.Cards) == 0 {
		return parsedDeck{}, fmt.Errorf("%w: the import needs at least one '## Card' block", ErrInvalid)
	}
	return deck, nil
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/apps/flash/... -run TestParseImport -v`
Expected: PASS (21 subtests).

- [ ] **Step 5: Commit**

```bash
git add internal/apps/flash/import.go internal/apps/flash/import_validate_test.go
git commit -m "feat(flash): add the import payload parser"
```

---

### Task 2: `Store.ImportDeck`

**Files:**
- Create: `internal/apps/flash/import_store.go`
- Test: `internal/apps/flash/import_store_test.go`

**Interfaces:**
- Consumes: `parsedCard`, `parsedDeck`, `ParseImport` (Task 1); `Deck`, `DefaultNewCardsPerDay`, `formatTime`, `isUniqueViolation` (`deck.go`/`flash.go`); `normalizeTagName` (`tag.go`); the package-level `newFixture(t)` test helper already defined in `deck_test.go`.
- Produces: `func (st *Store) ImportDeck(ctx context.Context, userID int64, name, description string, cards []parsedCard) (Deck, error)` — used by Task 3's handler.

- [ ] **Step 1: Write the failing tests**

Create `internal/apps/flash/import_store_test.go`:

```go
package flash_test

import (
	"context"
	"errors"
	"testing"

	"github.com/iliafrenkel/on-suite/internal/apps/flash"
)

func TestImportDeckCreatesDeckCardsAndTags(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	d, err := flash.ParseImport(`{
		"deck": {"name": "Imported", "description": "from a test"},
		"cards": [
			{"type": "basic", "front": "Q1", "back": "A1", "tags": ["x", "y"]},
			{"type": "cloze", "front": "The {{c1::answer}}."}
		]
	}`, "json")
	if err != nil {
		t.Fatalf("ParseImport: %v", err)
	}

	deck, err := f.store.ImportDeck(ctx, f.alice.ID, d.Name, d.Description, d.Cards)
	if err != nil {
		t.Fatalf("ImportDeck: %v", err)
	}
	if deck.ID == 0 {
		t.Fatal("ImportDeck returned id 0")
	}
	if deck.NewCardsPerDay != flash.DefaultNewCardsPerDay {
		t.Errorf("NewCardsPerDay = %d, want %d", deck.NewCardsPerDay, flash.DefaultNewCardsPerDay)
	}

	cards, err := f.store.ListCards(ctx, f.alice.ID, deck.ID)
	if err != nil {
		t.Fatalf("ListCards: %v", err)
	}
	if len(cards) != 2 {
		t.Fatalf("len(cards) = %d, want 2", len(cards))
	}

	var basic flash.Card
	for _, c := range cards {
		if c.CardType == flash.CardTypeBasic {
			basic = c
		}
	}
	if basic.ID == 0 {
		t.Fatal("no basic card found")
	}
	tags, err := f.store.TagsForCard(ctx, f.alice.ID, basic.ID)
	if err != nil {
		t.Fatalf("TagsForCard: %v", err)
	}
	if len(tags) != 2 {
		t.Fatalf("len(tags) = %d, want 2", len(tags))
	}
}

func TestImportDeckRejectsDuplicateName(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	if _, err := f.store.CreateDeck(ctx, f.alice.ID, "Existing", ""); err != nil {
		t.Fatal(err)
	}

	d, err := flash.ParseImport(`{"deck": {"name": "Existing"}, "cards": [{"front": "Q", "back": "A"}]}`, "json")
	if err != nil {
		t.Fatalf("ParseImport: %v", err)
	}

	_, err = f.store.ImportDeck(ctx, f.alice.ID, d.Name, d.Description, d.Cards)
	if !errors.Is(err, flash.ErrInvalid) {
		t.Fatalf("ImportDeck err = %v, want ErrInvalid", err)
	}

	decks, err := f.store.ListDecks(ctx, f.alice.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(decks) != 1 {
		t.Errorf("len(decks) = %d, want 1 (the original, nothing partially imported)", len(decks))
	}
}
```

`newFixture` is already defined in `internal/apps/flash/deck_test.go` (same `flash_test` package) — it migrates a fresh SQLite database and creates users `f.alice`/`f.bob`. Do not redefine it.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/apps/flash/... -run TestImportDeck -v`
Expected: FAIL — `ImportDeck` is undefined.

- [ ] **Step 3: Write the implementation**

Create `internal/apps/flash/import_store.go`:

```go
// internal/apps/flash/import_store.go
package flash

import (
	"context"
	"database/sql"
	"fmt"
)

// ImportDeck creates a new deck for userID and populates it with cards, all
// inside one transaction: if any insert fails, nothing is left behind. cards
// must already be validated (see ParseImport) — ImportDeck only persists,
// it does not re-validate field contents.
func (st *Store) ImportDeck(ctx context.Context, userID int64, name, description string, cards []parsedCard) (Deck, error) {
	tx, err := st.db.BeginTx(ctx, nil)
	if err != nil {
		return Deck{}, fmt.Errorf("flash: import deck: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	d := Deck{UserID: userID, Name: name, Description: description, CreatedAt: st.now(), NewCardsPerDay: DefaultNewCardsPerDay}
	err = tx.QueryRowContext(ctx,
		`INSERT INTO flash_decks (user_id, name, description, created_at)
		 VALUES (?, ?, ?, ?)
		 RETURNING id`,
		d.UserID, d.Name, d.Description, formatTime(d.CreatedAt),
	).Scan(&d.ID)
	if err != nil {
		if isUniqueViolation(err) {
			return Deck{}, fmt.Errorf("%w: you already have a deck named %q", ErrInvalid, name)
		}
		return Deck{}, fmt.Errorf("flash: import deck: %w", err)
	}

	for _, c := range cards {
		var cardID int64
		err = tx.QueryRowContext(ctx,
			`INSERT INTO flash_cards (deck_id, user_id, card_type, front, back, notes, created_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?)
			 RETURNING id`,
			d.ID, userID, c.CardType, c.Front, c.Back, c.Notes, formatTime(st.now()),
		).Scan(&cardID)
		if err != nil {
			return Deck{}, fmt.Errorf("flash: import deck: %w", err)
		}
		if err := importCardTags(ctx, tx, userID, cardID, c.Tags); err != nil {
			return Deck{}, err
		}
	}

	if err := tx.Commit(); err != nil {
		return Deck{}, fmt.Errorf("flash: import deck: %w", err)
	}
	return d, nil
}

// importCardTags is SetCardTags' insert logic (tag.go), run against the
// transaction ImportDeck already holds. SetCardTags cannot be called
// directly here: it opens (and commits) its own transaction via st.db, and
// flash's SQLite handle is opened with a single connection
// (internal/platform/db.Open sets MaxOpenConns(1)), so a second BeginTx
// from inside an already-open transaction would deadlock waiting for a
// connection the first transaction is still holding.
func importCardTags(ctx context.Context, tx *sql.Tx, userID, cardID int64, names []string) error {
	seen := make(map[string]bool)
	for _, raw := range names {
		name := normalizeTagName(raw)
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true

		var tagID int64
		err := tx.QueryRowContext(ctx, `SELECT id FROM flash_tags WHERE user_id = ? AND name = ?`, userID, name).Scan(&tagID)
		if err != nil {
			if err := tx.QueryRowContext(ctx,
				`INSERT INTO flash_tags (user_id, name) VALUES (?, ?) RETURNING id`, userID, name,
			).Scan(&tagID); err != nil {
				return fmt.Errorf("flash: import deck: create tag %q: %w", name, err)
			}
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO flash_card_tags (card_id, tag_id) VALUES (?, ?)`, cardID, tagID); err != nil {
			return fmt.Errorf("flash: import deck: link tag %q: %w", name, err)
		}
	}
	return nil
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/apps/flash/... -run TestImportDeck -v`
Expected: PASS (2 subtests).

- [ ] **Step 5: Commit**

```bash
git add internal/apps/flash/import_store.go internal/apps/flash/import_store_test.go
git commit -m "feat(flash): add Store.ImportDeck"
```

---

### Task 3: Import routes, handlers, and UI

**Files:**
- Create: `internal/apps/flash/handlers_import.go`
- Create: `internal/apps/flash/templates/import.partial.html`
- Modify: `internal/apps/flash/handlers_decks.go` (add `deckModeImport`, two new `deckDetailView` fields, one `deckPageTitle` case)
- Modify: `internal/apps/flash/templates/decks.html` (one new `deck-detail-body` branch, one new toolbar link)
- Modify: `internal/apps/flash/flash.go` (two new routes)
- Test: `internal/apps/flash/handlers_import_test.go`

**Interfaces:**
- Consumes: `ParseImport` (Task 1), `Store.ImportDeck` (Task 2), and `handlers_decks.go`'s existing helpers: `a.userID`, `a.fail`, `userMessage`, `a.renderDeckIndex`, `a.renderDeckDetailWithList`, `a.viewDeckDetail`, `deckDetailView`, `deckModeView`/`deckModeNew`.
- Produces: `GET /import`, `POST /import` routes; nothing later tasks depend on (this is the last task).

- [ ] **Step 1: Modify `handlers_decks.go`**

In `internal/apps/flash/handlers_decks.go`, change:

```go
const (
	deckModeView = "view"
	deckModeNew  = "new"
)
```

to:

```go
const (
	deckModeView   = "view"
	deckModeNew    = "new"
	deckModeImport = "import"
)
```

Change:

```go
	NewCardsPerDayValue string
	ReviewsPerDayValue  string
}
```

to:

```go
	NewCardsPerDayValue string
	ReviewsPerDayValue  string

	PayloadValue string
	FormatValue  string
}
```

Change:

```go
	case deckModeNew:
		return "New deck"
	default:
```

to:

```go
	case deckModeNew:
		return "New deck"
	case deckModeImport:
		return "Import"
	default:
```

- [ ] **Step 2: Create the handler**

Create `internal/apps/flash/handlers_import.go`:

```go
// internal/apps/flash/handlers_import.go
package flash

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/iliafrenkel/on-suite/internal/platform/web"
)

func (a *App) importDeckDetail(r *http.Request, errMsg, payload, format string) deckDetailView {
	if format == "" {
		format = "auto"
	}
	return deckDetailView{
		Mode: deckModeImport, PayloadValue: payload, FormatValue: format,
		Error: errMsg, CSRFToken: web.CSRFToken(r.Context()),
	}
}

func (a *App) importForm(w http.ResponseWriter, r *http.Request) {
	userID, ok := a.userID(w, r)
	if !ok {
		return
	}
	a.renderDeckIndex(w, r, userID, http.StatusOK, a.importDeckDetail(r, "", "", "auto"))
}

func (a *App) importDeck(w http.ResponseWriter, r *http.Request) {
	userID, ok := a.userID(w, r)
	if !ok {
		return
	}
	payload := r.PostFormValue("payload")
	format := r.PostFormValue("format")

	parsed, err := ParseImport(payload, format)
	if err != nil {
		a.renderDeckIndex(w, r, userID, http.StatusBadRequest, a.importDeckDetail(r, userMessage(err), payload, format))
		return
	}
	d, err := a.store.ImportDeck(r.Context(), userID, parsed.Name, parsed.Description, parsed.Cards)
	if err != nil {
		if errors.Is(err, ErrInvalid) {
			a.renderDeckIndex(w, r, userID, http.StatusBadRequest, a.importDeckDetail(r, userMessage(err), payload, format))
			return
		}
		a.deps.Errors.Internal(w, r, err)
		return
	}

	if !web.IsHTMX(r) {
		http.Redirect(w, r, "/flash/"+strconv.FormatInt(d.ID, 10), http.StatusSeeOther)
		return
	}
	w.Header().Set("HX-Push-Url", "/flash/"+strconv.FormatInt(d.ID, 10))
	a.renderDeckDetailWithList(w, r, userID, http.StatusCreated, a.viewDeckDetail(r, d))
}
```

- [ ] **Step 3: Create the template**

Create `internal/apps/flash/templates/import.partial.html`. **The `.partial.html` suffix is required** — see this plan's Global Constraints section: `decks.html`'s `{{template "deck-detail-import" .}}` call (Step 5, below) can only resolve a definition that lives in a `*.partial.html` file, since `internal/platform/render`'s `AddApp` registers every plain `*.html` file as its own isolated template set.

```html
{{define "deck-detail-import"}}
<form class="stack" id="deck-detail-import" method="post" action="/flash/import" hx-post="/flash/import" hx-target="#deck-detail" hx-swap="innerHTML">
	<input type="hidden" name="{{csrfField}}" value="{{.CSRFToken}}">
	<h1>Import</h1>
	{{with .Error}}<div class="notice notice-error" role="alert">{{.}}</div>{{end}}
	<div class="field">
		<label for="import-format">Format</label>
		<select id="import-format" name="format">
			<option value="auto"{{if eq .FormatValue "auto"}} selected{{end}}>Auto-detect</option>
			<option value="markdown"{{if eq .FormatValue "markdown"}} selected{{end}}>Markdown</option>
			<option value="json"{{if eq .FormatValue "json"}} selected{{end}}>JSON</option>
		</select>
	</div>
	<div class="field">
		<label for="import-payload">Paste your deck</label>
		<textarea id="import-payload" name="payload" rows="16" autofocus required>{{.PayloadValue}}</textarea>
	</div>
	<div class="row">
		<button type="submit" class="toolbar-btn toolbar-btn-active">Import</button>
		<a class="toolbar-btn" href="/flash/" hx-get="/flash/" hx-target="#deck-detail" hx-push-url="true">Cancel</a>
	</div>
</form>
{{end}}
```

- [ ] **Step 4: Add the routes**

In `internal/apps/flash/flash.go`, in `Mount`, change:

```go
	r.HandleFunc("GET /{$}", a.deckIndex)
	r.HandleFunc("GET /new", a.newDeckForm)
	r.HandleFunc("POST /new", a.createDeck)
```

to:

```go
	r.HandleFunc("GET /{$}", a.deckIndex)
	r.HandleFunc("GET /new", a.newDeckForm)
	r.HandleFunc("POST /new", a.createDeck)

	// "import" is a literal single segment, the same non-ambiguity shape as
	// "new" and "review" alongside the existing wildcard single-segment
	// routes (/{deckID}) below — literals never conflict with each other or
	// with a same-position wildcard.
	r.HandleFunc("GET /import", a.importForm)
	r.HandleFunc("POST /import", a.importDeck)
```

- [ ] **Step 5: Wire the template dispatch and toolbar link**

In `internal/apps/flash/templates/decks.html`, change:

```
{{else if eq .Mode "new"}}{{template "deck-detail-new" .}}
```

to:

```
{{else if eq .Mode "new"}}{{template "deck-detail-new" .}}
{{else if eq .Mode "import"}}{{template "deck-detail-import" .}}
```

Change:

```
		<a class="toolbar-btn" href="/flash/new" hx-get="/flash/new" hx-target="#deck-detail" hx-push-url="true">New deck</a>
		<a class="toolbar-btn" href="/flash/review">Review</a>
```

to:

```
		<a class="toolbar-btn" href="/flash/new" hx-get="/flash/new" hx-target="#deck-detail" hx-push-url="true">New deck</a>
		<a class="toolbar-btn" href="/flash/import" hx-get="/flash/import" hx-target="#deck-detail" hx-push-url="true">Import</a>
		<a class="toolbar-btn" href="/flash/review">Review</a>
```

- [ ] **Step 6: Write the failing HTTP tests**

Create `internal/apps/flash/handlers_import_test.go`:

```go
package flash_test

import (
	"net/url"
	"strings"
	"testing"

	"github.com/iliafrenkel/on-suite/internal/htmlassert"
)

func TestImportRequiresSignIn(t *testing.T) {
	s := newServer(t)
	rec := s.Do(t, nil, httpGet(t, "/flash/import"))
	if rec.Code != 303 && rec.Code != 401 {
		t.Errorf("GET /flash/import signed out = %d, want a redirect-to-login or 401", rec.Code)
	}
}

func TestImportRequiresCSRF(t *testing.T) {
	s := newServer(t)
	req := httpPost(t, "/flash/import", url.Values{"payload": {"{}"}, "format": {"json"}})
	rec := s.Do(t, s.Alice, req)
	if rec.Code != 403 {
		t.Errorf("POST /flash/import without CSRF = %d, want 403", rec.Code)
	}
}

func TestImportJSONOverHTTP(t *testing.T) {
	s := newServer(t)
	payload := `{
		"deck": {"name": "Spanish travel phrases", "description": "Travel basics"},
		"cards": [{"type": "basic", "front": "Thank you", "back": "Gracias", "tags": ["travel"]}]
	}`
	rec := s.Post(t, s.Alice, "/flash/import", url.Values{"payload": {payload}, "format": {"json"}})
	if rec.Code != 303 {
		t.Fatalf("POST /flash/import (json) = %d, want 303", rec.Code)
	}

	doc := s.Get(t, s.Alice, rec.Header().Get("Location"))
	doc.MustHave(".deck-list")
	body := htmlassert.Text(doc.MustHave("body"))
	if !strings.Contains(body, "Spanish travel phrases") {
		t.Errorf("deck page does not show the imported deck's name: %q", body)
	}
}

func TestImportMarkdownOverHTTP(t *testing.T) {
	s := newServer(t)
	payload := "# Geography\n\n## Card\nFront: The capital of France is {{c1::Paris}}.\nType: cloze\n"
	rec := s.Post(t, s.Alice, "/flash/import", url.Values{"payload": {payload}, "format": {"markdown"}})
	if rec.Code != 303 {
		t.Fatalf("POST /flash/import (markdown) = %d, want 303", rec.Code)
	}

	doc := s.Get(t, s.Alice, rec.Header().Get("Location"))
	body := htmlassert.Text(doc.MustHave("body"))
	if !strings.Contains(body, "Geography") {
		t.Errorf("deck page does not show the imported deck's name: %q", body)
	}
}

func TestImportMalformedWritesNothing(t *testing.T) {
	s := newServer(t)

	rec := s.Post(t, s.Alice, "/flash/import", url.Values{
		"payload": {`{"deck": {"name": "Bad"}, "cards": [{"front": "", "back": "A"}]}`},
		"format":  {"json"},
	})
	if rec.Code != 400 {
		t.Fatalf("POST /flash/import with a blank front = %d, want 400", rec.Code)
	}
	doc := htmlassert.Parse(t, rec.Body.String())
	doc.MustHave(".notice-error")

	after := s.Get(t, s.Alice, "/flash/")
	afterBody := htmlassert.Text(after.MustHave("body"))
	if strings.Contains(afterBody, "Bad") {
		t.Errorf("a deck named %q should not exist after a rejected import", "Bad")
	}
}
```

`newServer`, `httpGet`, `httpPost` are already defined in `internal/apps/flash/handlers_decks_test.go` (same `flash_test` package) — do not redefine them.

- [ ] **Step 7: Run all the new tests**

Run: `go test ./internal/apps/flash/... -run TestImport -v`
Expected: PASS (10 subtests: 5 from Task 1's suite matched by the `TestImport` prefix won't match — this filters to the 8 `handlers_import_test.go` + `import_store_test.go` tests named `TestImport*`).

Run the whole package to catch any regression in the templates or routes touched:

Run: `go test ./internal/apps/flash/...`
Expected: PASS, all tests (including F1/F2's existing suites).

Run the app-boundary and route-registration checks:

Run: `go test ./internal/arch/...`
Expected: PASS — confirms `Mount`'s two new routes don't panic against the existing route set (a startup panic on an ambiguous pattern would surface as a build/test failure the moment any test constructs the app).

- [ ] **Step 8: Full check**

Run, in order:

```bash
go build ./...
gofmt -l internal/apps/flash/
go vet ./...
go test ./... -race -count=1
```

Expected: `gofmt -l` prints nothing (no unformatted files); everything else passes with no failures.

- [ ] **Step 9: Commit**

```bash
git add internal/apps/flash/handlers_import.go internal/apps/flash/handlers_import_test.go \
        internal/apps/flash/handlers_decks.go internal/apps/flash/templates/decks.html \
        internal/apps/flash/templates/import.partial.html internal/apps/flash/flash.go
git commit -m "feat(flash): add the import screen and routes"
```
