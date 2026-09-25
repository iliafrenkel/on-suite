// internal/apps/flash/import.go
package flash

import (
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

// parsedCard is one card extracted from an import payload, already validated
// against the same rules ValidateCard/ValidateTagNames apply to hand-created
// cards. ImageURL/AudioURL are "" when the card has no media attached; when
// set, they are validated for shape only (see validateMediaURL) — the URL is
// not fetched at parse time. See media.go/import_store.go for what happens
// to them next.
type parsedCard struct {
	CardType string
	Front    string
	Back     string
	Notes    string
	Tags     []string
	ImageURL string
	AudioURL string
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
		Image string   `json:"image"`
		Audio string   `json:"audio"`
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

func validateParsedCard(cardType, front, back, notes string, tags []string, imageURL, audioURL string, cardNum int) error {
	if err := ValidateCard(cardType, front, back); err != nil {
		return fmt.Errorf("%w: card %d: %s", ErrInvalid, cardNum, stripErrInvalid(err))
	}
	if err := validateCardNotes(notes); err != nil {
		return fmt.Errorf("%w: card %d: %s", ErrInvalid, cardNum, stripErrInvalid(err))
	}
	if err := ValidateTagNames(tags); err != nil {
		return fmt.Errorf("%w: card %d: %s", ErrInvalid, cardNum, stripErrInvalid(err))
	}
	if err := validateMediaURL(imageURL); err != nil {
		return fmt.Errorf("%w: card %d: image: %s", ErrInvalid, cardNum, stripErrInvalid(err))
	}
	if err := validateMediaURL(audioURL); err != nil {
		return fmt.Errorf("%w: card %d: audio: %s", ErrInvalid, cardNum, stripErrInvalid(err))
	}
	return nil
}

// validateMediaURL checks a card's image/audio URL for shape only — it must
// be an absolute http/https URL. An empty string (no media attached) is
// valid. The URL is never fetched here; see FetchAndCacheMedia.
func validateMediaURL(rawURL string) error {
	if rawURL == "" {
		return nil
	}
	u, err := url.Parse(rawURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return fmt.Errorf("%w: %q is not a valid http(s) URL", ErrInvalid, rawURL)
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
		if err := validateParsedCard(cardType, c.Front, c.Back, c.Notes, c.Tags, c.Image, c.Audio, i+1); err != nil {
			return parsedDeck{}, err
		}
		deck.Cards = append(deck.Cards, parsedCard{
			CardType: cardType, Front: c.Front, Back: c.Back, Notes: c.Notes, Tags: c.Tags,
			ImageURL: c.Image, AudioURL: c.Audio,
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
			case "type", "front", "back", "tags", "notes", "image", "audio":
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
	// front/back/notes (and any other multi-line text field) may have
	// started with no same-line value, leaving a stray leading "\n" from
	// the first continuation line appended above; TrimSpace removes that
	// (and any trailing whitespace) while preserving internal newlines
	// between continuation lines.
	for _, key := range []string{"front", "back", "notes"} {
		if v, ok := fields[key]; ok {
			fields[key] = strings.TrimSpace(v)
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
		imageURL, audioURL := strings.TrimSpace(fields["image"]), strings.TrimSpace(fields["audio"])

		if err := validateParsedCard(cardType, front, back, notes, tags, imageURL, audioURL, cardNum); err != nil {
			return parsedDeck{}, err
		}
		deck.Cards = append(deck.Cards, parsedCard{
			CardType: cardType, Front: front, Back: back, Notes: notes, Tags: tags,
			ImageURL: imageURL, AudioURL: audioURL,
		})
	}

	if len(deck.Cards) == 0 {
		return parsedDeck{}, fmt.Errorf("%w: the import needs at least one '## Card' block", ErrInvalid)
	}
	return deck, nil
}
