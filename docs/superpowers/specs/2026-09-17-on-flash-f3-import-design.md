# ON Flash F3 — import (design spec)

F3 lets a user create a whole deck at once by pasting Markdown or JSON into
an import screen, matching the feature spec's "generate elsewhere, paste
here" workflow: the user prompts an LLM of their choice in one of ON
Flash's two documented formats, then pastes the result in.

## Scope

- Import always creates a **new** deck; there is no "append to an existing
  deck" path in F3.
- Import is **all-or-nothing**: the first invalid card aborts the whole
  import and nothing is written. There is no partial/best-effort mode.
- Media is **out of scope**. The JSON schema accepts `image`/`audio` fields
  (so LLM output written against the feature spec's original examples isn't
  rejected) but F3 parses and discards them. Fetching, caching, and serving
  media locally is F4's work.
- The only input surface is a pasted-text `<textarea>`. There is no file
  upload.
- Format is auto-detected (valid JSON → JSON path, otherwise Markdown), with
  a manual override control (`Auto` / `Markdown` / `JSON`) on the import
  form for the rare case auto-detection guesses wrong.

## Architecture

Three new files, following the existing flash package's per-concern split:

- **`import.go`** — pure parsing and validation, no database access.
  `ParseImport(payload, format string) (parsedDeck, error)` returns a
  fully-validated `parsedDeck{Name, Description string; Cards
  []parsedCard}` or the first validation error found. Reuses
  `ValidateCard` and `ValidateTagNames` from `card.go`/`tag.go` rather than
  duplicating field rules.
- **`Store.ImportDeck`** (added to `deck.go` or a new `import_store.go`,
  implementer's call at plan time) — `ImportDeck(ctx, userID int64, name,
  description string, cards []parsedCard) (Deck, error)`. Opens one
  transaction with `st.db.BeginTx(ctx, nil)` (the same inline pattern
  `review.go` and `tag.go` already use — no new transaction abstraction),
  inserts the deck (via the same insert `CreateDeck` uses, including
  `NewCardsPerDay: DefaultNewCardsPerDay`), inserts every card, then calls
  `SetCardTags` per card. Any failure rolls back the whole transaction.
- **`handlers_import.go`** + **`templates/import.html`** — `GET /import`
  (the form) and `POST /import` (parse, then write). The handler calls
  `ParseImport` first; the transaction only opens if parsing succeeds, so a
  malformed payload never touches the database.

## JSON schema

```json
{
  "deck": {
    "name": "Spanish travel phrases",
    "description": "Optional, may be omitted"
  },
  "cards": [
    {
      "type": "basic",
      "front": "How do you say 'thank you'?",
      "back": "Gracias",
      "notes": "Optional, shown only after the answer is revealed",
      "tags": ["travel", "spanish"],
      "image": "https://example.com/thanks.jpg"
    },
    {
      "type": "cloze",
      "front": "The capital of France is {{c1::Paris}}.",
      "tags": ["geography"]
    }
  ]
}
```

Rules:

- `deck.name` is required and non-blank (same rule `ValidateDeckSettings`
  applies to a deck name). `deck.description` is optional, defaults to `""`.
- `cards` is required and must be a non-empty array.
- Each card's `type` is `"basic"` or `"cloze"` (`CardTypeBasic` /
  `CardTypeCloze`); `front`, `back`, `notes`, `tags` are validated with the
  exact same rules F1 already enforces for hand-created cards
  (`ValidateCard`, `validateCardNotes`, `ValidateTagNames`) — no new field
  rules for imported cards.
- `image` / `audio` fields (top-level on a card) are accepted by the
  parser and discarded — reserved for F4.
- Unknown/extra fields anywhere in the payload are ignored, not rejected —
  forward-compatible with LLM output that includes fields not yet
  implemented.

## Markdown schema

```markdown
# Spanish travel phrases
Optional deck description line(s), ending at the first `## Card`.

## Card
Type: basic
Front: How do you say 'thank you'?
Back: Gracias
Tags: travel, spanish
Notes: Optional, shown only after the answer is revealed

## Card
Type: cloze
Front: The capital of France is {{c1::Paris}}.
Tags: geography
```

Rules:

- The first line must be a `# ` heading — the deck name. Everything between
  it and the first `## Card` line is the deck description (trimmed to `""`
  if there is nothing there).
- Each card is a `## Card` block of `Key: value` lines. Recognized keys:
  `Type` (`basic`/`cloze`; defaults to `basic` if the key is omitted),
  `Front`, `Back`, `Tags` (comma-separated), `Notes`. Unrecognized keys are
  ignored, same forward-compatibility reasoning as JSON's extra fields.
- `Front`, `Back`, and `Notes` values may span multiple lines: a value
  continues on subsequent lines until the next recognized `Key:` line, the
  next `## Card`, or end of input.
- There are no `Image`/`Audio` keys in the Markdown format for F3 — no
  convention is reserved here the way JSON reserves `image`/`audio`; adding
  one is F4's decision alongside the fetch pipeline.

## Cloze cards

Cloze cards use Anki-style `{{c1::answer}}` markers. This matches F1/F2's
existing model exactly: a cloze card is stored as **one row** with all its
`{{cN::...}}` markers in `Front` and `Back` left empty — multiple numbered
deletions in one card's text are revealed together as one review card, not
split into separately-scheduled cards per number. Import does not change
this model; `ValidateCard`'s existing cloze rule (`front` must contain at
least one `{{...}}` deletion, `back` must be empty) is reused unchanged.

## Validation & error handling

- All-or-nothing: the first invalid card aborts parsing with one
  `ErrInvalid`-wrapped error naming the card's 1-based position, e.g. `card
  3: a basic card needs a back`. This mirrors ON Notes' `ParseMarkdown`
  first-error convention (`internal/apps/notes/import.go`).
- `ParseImport` is a pure function — it never touches the database, so a
  malformed payload never opens a transaction and never leaves a
  half-written deck behind.
- The HTTP handler maps errors through `a.fail` exactly like every other
  flash mutation: `ErrInvalid` → 400, error rendered in the form's
  `.notice-error`, with the originally pasted text preserved in the
  textarea so the user can fix and resubmit without retyping (mirrors the
  existing deck-new form's error-preserving behavior).

## Routes & UI

- `GET /import` — the import form: a format selector (`Auto` / `Markdown` /
  `JSON`, radio or select), one `<textarea name="payload">`, and an Import
  button.
- `POST /import` — reads `payload` and `format`, runs `ParseImport` then
  `Store.ImportDeck`. On success, PRG-redirects to `/flash/{newDeckID}`
  (same pattern as `createDeck`). On failure, re-renders the import form at
  200 with the error and the pasted text preserved.
- The deck index's `.list-head` row gets a new "Import" link alongside the
  existing "New deck" and "Review" links.
- No new UI machinery beyond what deck forms already use: `hx-post` /
  `hx-target="#deck-detail"` wired the same way `deck-detail-new` is.

## Testing

- `import_test.go` (pure, no DB): table-driven tests for `ParseImport`
  covering both formats — valid decks, missing/blank deck name, empty
  cards array, bad card type, cloze front missing `{{}}`, tag validation
  failure, multi-line Markdown values, unknown-field forward-compatibility,
  and the auto-detect sniffing logic itself.
- `handlers_import_test.go` (HTTP, via `apptest.Server`, mirroring
  `handlers_decks_test.go`): sign-in requirement, CSRF requirement, a
  happy-path JSON import and a happy-path Markdown import each landing on
  the new deck's page with its cards visible, and a malformed-payload case
  asserting nothing was written (deck count unchanged) — proving the
  all-or-nothing transaction actually rolls back over HTTP, not just in the
  pure parser tests.

## Open items deferred to later

- Media fetch/cache pipeline for `image`/`audio` fields (F4).
- Appending an import to an existing deck, rather than always creating a
  new one.
- A file-upload alternative to pasting.
