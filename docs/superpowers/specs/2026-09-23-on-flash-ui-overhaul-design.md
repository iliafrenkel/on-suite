# ON Flash — UI/UX overhaul (design spec)

ON Flash works end to end (F1–F6: decks, cards, FSRS review, import, media,
sharing, stats), but its UI is a set of unstyled lists and bare forms. This
spec redesigns every Flash screen so that a child (roughly 9–14) can use the
app unassisted: at no point should a user stop and wonder "what do I do
next, where do I click?".

Visual reference: [2026-09-23-on-flash-ui-overhaul-mockups.html](2026-09-23-on-flash-ui-overhaul-mockups.html)
— the mockups agreed during brainstorming (home screen, cards grid, card
editor, review screen). Open it in a browser; the review mockup is
interactive. The mockups show layout and hierarchy; the colours in them are
illustrative — the tokens in this spec are authoritative.

## Goals

- Friendly for older kids, not childish for adults. Generous sizes, clear
  words, one obvious primary action per screen.
- Visually consistent with the rest of the suite (warm palette, existing
  toolbar buttons, Pastes-style split view). Cards use a **clean, modern**
  look — white face, deck-colour stripe — not a skeuomorphic one.
- Cards look like real cards and flip with an animation, both in review and
  when browsing a deck.
- Works on laptops and tablets (touch targets ≥ 44px; list/detail collapse
  below 900px).
- Every interaction works without JavaScript (JS only adds keyboard
  shortcuts and conveniences), and everything stays inside the strict CSP:
  no inline `<script>`, no `style=` attributes.

## Non-goals (for now)

Practice mode (reviewing with nothing due), swipe gestures, a per-user
"simple mode", an import preview step, emoji/icon deck covers, card-grid
pagination, audio autoplay, splitting one cloze card into one card per
deletion number.

## Delivery: six phased PRs

One spec, six implementation plans (one per PR), each shippable on its own
and each small enough to hold in context. Order matters — U2's card
component is reused by U3 and U4. Plans:
[U1](../plans/2026-09-23-flash-ui-u1-foundation.md) ·
[U2](../plans/2026-09-23-flash-ui-u2-cards.md) ·
[U3](../plans/2026-09-23-flash-ui-u3-editor.md) ·
[U4](../plans/2026-09-23-flash-ui-u4-review.md) ·
[U5](../plans/2026-09-23-flash-ui-u5-sharing.md) ·
[U6](../plans/2026-09-23-flash-ui-u6-import-stats.md).

| Phase | Scope |
|---|---|
| **U1** Foundation | deck colour (migration + swatch picker), home split view, app toolbar, deck list stacks, deck pane with big Review button, first-run welcome, `flash.js` skeleton |
| **U2** Card component & Cards mode | `flash-card` template + CSS-only flip, cloze renderer, cards grid in the right pane, search + tag pills, opened card with prev/next, restyled cross-deck tag page |
| **U3** Card editor | card-face editor, type toggle, Make blank, tag-pill input, media drop zones merged into the card form, Save and add another |
| **U4** Review | focused review screen, new grade labels, progress bar, session summary with streak and celebration |
| **U5** Sharing & snooze | share popover, gift decks + preview pane, snooze moved into Edit deck, break banner |
| **U6** Import & stats | 3-step import helper with Copy prompt, stats in the right pane |

## 1. Shared foundations (all phases)

### 1.1 Visual language

- Reuse the suite tokens (`--c-*`, `--s-*`, `--fs-*`, `--radius`) and
  existing components: `.toolbar-btn` (with `toolbar-btn-active` for the
  primary action and `danger` for destructive ones), `{{ticon "name"}}` icons
  from [internal/ui/toolbar_icons.go](../../../internal/ui/toolbar_icons.go)
  (add any missing icon there, not inline in Flash templates), `.notice`,
  `.field`.
- All new Flash CSS goes in the existing Flash section of
  [internal/ui/static/app.css](../../../internal/ui/static/app.css), with
  `flash-` prefixed class names. Every new colour token is defined for light
  mode on `:root` **and** in app.css's single dark-mode block,
  `:root[data-theme="dark"]` (the theme is always set server-side, so there
  is no `prefers-color-scheme` fallback to maintain).
- **No `style=` attributes, ever** (CSP). Anything whose size varies per row
  — progress bars, the review breakdown bar, the mastered bar — is an inline
  `<svg>` whose `<rect>` `width` attribute is computed in Go, the same
  technique as `.reader-ratio` in app.css.
- Copy style: sentence case, verb-first buttons, no "please", no
  "successfully". Friendly but plain.

### 1.2 Deck colours

- Migration `0010_deck_color.sql`:
  `ALTER TABLE flash_decks ADD COLUMN color TEXT NOT NULL DEFAULT 'teal';`
- Go: a fixed, ordered list of eight colour names and a validator
  (`ValidDeckColor`). `CreateDeck`/`UpdateDeck` take a colour; an unknown
  value is `ErrInvalid`. Import creates decks with the default `teal`.
  Adopting a shared deck copies the source deck's colour.
- The CSP forbids `style=`, so colour is applied by class: a deck element
  carries `deck-c-<name>`, which sets two custom properties that every
  deck-coloured element reads (`var(--deck)`, `var(--deck-soft)`):

| name | `--deck` (light & dark) | `--deck-soft` light | `--deck-soft` dark |
|---|---|---|---|
| teal | `#1D9E75` | `#E1F5EE` | `#085041` |
| blue | `#378ADD` | `#E6F1FB` | `#0C447C` |
| purple | `#7F77DD` | `#EEEDFE` | `#3C3489` |
| pink | `#D4537E` | `#FBEAF0` | `#72243E` |
| coral | `#D85A30` | `#FAECE7` | `#712B13` |
| amber | `#BA7517` | `#FAEEDA` | `#633806` |
| green | `#639922` | `#EAF3DE` | `#27500A` |
| gray | `#888780` | `#F1EFE8` | `#444441` |

- **Deck stack** (`.flash-stack`, sizes `-sm` for list rows and `-lg` for
  the deck hero): three stacked rounded rectangles, the back two rotated
  (−8° and +4°) and tinted with
  `color-mix(in srgb, var(--deck) 45%, var(--c-bg))` and `70%`, the front one
  solid `var(--deck)`. Pure CSS (pseudo-elements or three empty spans).
- **Swatch picker** (New deck, Edit deck): eight radio inputs styled as
  round swatches (the input visually hidden, the `<label>` is the swatch, a
  checked swatch gets a ring). No JS. Each has an accessible name ("Teal").

### 1.3 The card component

One template, `flash-card`, draws a card everywhere a card appears: grid
(`size: mini`), opened card and review (`size: large`). Inputs: the card's
rendered front and back (see 1.4), notes, tags, image/audio URLs, deck
colour, and a unique id suffix.

- Face: `--flash-card-bg` (`#ffffff` light; `var(--c-bg-inset)` dark),
  `1px solid var(--c-border)`, radius 14px (10px mini), an 8px stripe of
  `var(--deck)` across the top (5px mini), content centred.
- **Flip is CSS-only**: a visually hidden checkbox precedes the card; the
  card is its `<label>`. `.flash-flip:checked + .flash-card .flash-card-inner`
  rotates 180° on Y (`perspective` on the card, `transform-style:
  preserve-3d`, `backface-visibility: hidden` on both faces, ~0.55s ease with
  a slight overshoot). This follows the `#paste-detail-open` checkbox
  precedent — works without JS and inside the CSP. Keyboard users can focus
  the checkbox (Space toggles it natively); a visible focus ring is drawn on
  the card when the checkbox has focus.
- `prefers-reduced-motion: reduce`: no rotation — the faces cross-fade.
- Only non-interactive content goes inside the flip `<label>` (question,
  answer, notes, image, tag names as plain pills). Interactive extras —
  `<audio controls>` and tag *links* — render in a `.flash-card-extras`
  block right after the card, shown only once the card is flipped (the same
  `:checked ~` sibling selector), because clicking a control inside a
  `<label>` would also toggle the checkbox.
- Mini cards do not flip; they are links (front only, plus a status corner
  label — see U2).

### 1.4 Cloze rendering

A Go helper (e.g. `renderCloze(front string) (question, answer template.HTML)`)
used everywhere a card face is drawn, including mini cards:

- Recognises `{{c1::text}}`, `{{c1::text::hint}}`, and bare `{{text}}`.
- Question side: each deletion becomes
  `<span class="flash-blank">hint-or-empty</span>` — a blank with an
  underline in `var(--deck)`; when a hint exists it is shown inside the
  blank in faint text.
- Answer side: each deletion becomes
  `<mark class="flash-fill">text</mark>` (background `var(--deck-soft)`).
- All deletions blank at once (one card, not one per number — matches the
  existing data model, see the F3 spec).
- All text is HTML-escaped **before** markers are substituted; the helper is
  the only place that produces `template.HTML` from card text. Table-driven
  tests cover: single, multiple, hint, bare braces, unmatched braces
  (rendered literally), and HTML injection in text and hints.
- Basic cards: question = escaped front, answer = escaped back.

### 1.5 Labels

Presentation only — stored values do not change:

- Grades: 1 **Forgot**, 2 **Hard**, 3 **Got it**, 4 **Easy**.
- Card types: `basic` → **Question and answer**, `cloze` → **Fill in the
  blank**.

### 1.6 JavaScript

`static/flash.js` replaces `flash-review.js` (route `GET /flash/flash.js`,
same sign-in requirement, loaded with `defer` from each Flash page's `head`
block). It is a set of small, independent `init*` functions keyed off data
attributes, each a no-op when its elements are absent:

| Feature | Phase | No-JS fallback |
|---|---|---|
| Review shortcuts: Space flip, 1–4 grade, U undo, Esc stop | U4 (existing keys move here in U1) | buttons |
| Card viewer shortcuts: ← → prev/next, Space flip, E edit | U2 | links |
| Make blank button (the type toggle itself is CSS-only, via `:has()`) | U3 | type the markers by hand |
| Make blank (wrap selection as `{{cN::…}}`) | U3 | type the markers |
| Tag-pill input | U3 | plain comma-separated text input |
| Media drag-and-drop + filename/thumbnail preview | U3 | plain file inputs |
| Copy prompt to clipboard | U6 | expandable read-only textarea |

Shortcuts ignore key presses while typing in an input/textarea/contenteditable
and when a modifier key is held (the existing `isTyping` guard).

## 2. U1 — Foundation and home screen

### Layout

```
┌──────────────────────────────────────────────────────────────┐
│ [+ New deck]  [Import]  [Review all (17)]  [Stats]            │  app toolbar
├───────────────────┬──────────────────────────────────────────┤
│ ▣ Spanish travel 12│ [Cards] [Edit] [Share] [Delete]          │  deck toolbar
│   30 cards         │                                          │
│ ▣ Times tables   5 │  ▣▣▣  Spanish travel                      │  hero
│ ▣ Planets          │       Phrases for our trip               │
│   all done         │       [ Review 12 cards → ]              │
│ ▣ Capitals (dim)   │                                          │
│   taking a break   │  [12 due today][5 new today][30][8]      │  tiles
└───────────────────┴──────────────────────────────────────────┘
```

- Page structure mirrors Pastes (`paste-shell` → `flash-shell`): a
  visually-hidden `#flash-detail-open` checkbox, `.flash-list-pane`,
  `#deck-detail.flash-detail-pane`. The app toolbar sits above both panes.
- **App toolbar**: New deck (primary), Import, Review all (label includes the
  total due count when > 0; omitted when 0), Stats. New deck and Import load
  into `#deck-detail` via HTMX with `hx-push-url` (as today). Review all is a
  normal link to `/flash/review`. Stats loads into the pane (U6; until then
  it stays a link to the existing page).
- **Deck list row**: `.flash-stack-sm` in the deck's colour, name, a status
  line (`N cards`, `all done` when nothing is due, `taking a break` when
  snoozed — row dimmed), and an orange due badge (`--c-accent-attention` on
  `--c-accent-attention-bg`) when due > 0. The active row gets a
  `var(--c-accent)` ring. Rows are ≥ 44px tall.
- **Deck pane (view mode)**:
  - Deck toolbar: Cards (U2; until then links to the existing cards page),
    Edit, Share (U5; until then the existing share form stays below the
    hero), Delete (`danger`, `data-confirm` — "Delete “Name” and all its
    cards? This can't be undone.").
  - Hero: `.flash-stack-lg`, name (the page's single `<h1>`), description.
  - **Review button** (the one primary action): a large button in
    `--c-accent-attention` — "Review N cards →", linking to
    `/flash/review/{deckID}`. When nothing is due it is replaced by a calm
    panel: "All done for today" plus when the next card is due ("Next cards
    tomorrow" / "in 3 days"; omit the second line if nothing is scheduled).
    No practice mode.
  - Tiles: **due today** (review cards in today's queue), **new today** (new
    cards still available within today's new-card limit), **cards** (total
    in the deck), **mastered** (same definition the stats page uses). Due
    today + new today = the N on the Review button (the length of
    `DueQueue` for this deck). Same tile style U6 uses.
  - Snooze controls stay where they are until U5 moves them.
- **First run** (the user has no decks and no pending offers): the pane
  shows a welcome — a short heading ("Make your first deck"), one line of
  explanation, and two large buttons: "Create a deck" and "Import a deck".
  With decks but none selected, the pane shows "Pick a deck on the left".
- **New deck / Edit deck forms**: restyled fields plus the swatch picker.
  Edit keeps the daily-limit fields, under a "Daily limits" subheading with
  helper text.
- **Narrow screens** (< 900px): the checkbox shows either the list or the
  detail pane; the detail pane gets a "← Decks" back button. Same mechanism
  as Pastes.

### Data needed

Deck list rows need, per deck: card count, due count, snoozed flag. The deck
pane needs due today / new today / total / mastered and the next due date.
Reuse existing store methods (`DueQueue`, `PerDeckLoad`, `DailyCounts`,
`CardsMastered`-style queries) where they fit; add a single batched
`DeckSummaries(userID, now)` query rather than one query per deck if the
existing methods would require N+1 calls.

## 3. U2 — Card component and Cards mode

- Build `flash-card` (1.3) and the cloze renderer (1.4) first; both are
  reused by U3 and U4.
- **Cards mode lives in the right pane.** `GET /flash/{deckID}/cards/` now
  renders the home layout (deck list left, grid right) on a full page load,
  and just the pane on an HTMX request (same full-page/partial split
  `deckIndex` uses, including the `<title>` + `#shell-crumb-tail` OOB swap).
  The Cards toolbar button shows as active.
- **Grid** (`#card-grid`): responsive (`repeat(auto-fill, minmax(9rem,
  1fr))`), first tile always "+ New card" (dashed outline), then mini cards
  in `ListCards` order (newest first). A mini card shows its question side and a corner label:
  **new** (the user has never reviewed it) or **due** (due now). A new store
  method `CardStatuses(ctx, userID, deckID, now) (map[int64]string, error)`
  returns these in one query.
- **Search and tag filter**: a search box and a row of tag pills ("All" plus
  every tag used in this deck, alphabetical). Both are plain GET parameters
  (`?q=…&tag=…`) on `GET /flash/{deckID}/cards/`, which is what the form
  submits without JS. With JS, the search input uses the existing
  live-search pattern (`hx-trigger="input changed delay:300ms, search"`,
  `hx-include="closest form"`) against a new fragment route,
  `GET /flash/{deckID}/cards/grid`, which returns only the grid tiles
  (`hx-target="#card-grid"`) plus an out-of-band copy of the tag-pill row
  (so the pills' links carry the new `q`), and sets `HX-Replace-Url` to the
  canonical `/flash/{deckID}/cards/?q=…&tag=…`. Tag pills are links to the
  full cards pane (`hx-target="#deck-detail"`, `hx-push-url="true"`) that
  set/clear `tag` and keep `q`. `q` matches front, back and notes,
  case-insensitively. Empty result: "No cards match" plus a "Clear filters"
  link. A filtered-by-tag grid shows a small "Show “tag” in all decks" link
  to `/flash/tags/{tag}`.
- **Opened card**: `GET /flash/{deckID}/cards/{cardID}` shows the large
  `flash-card` (flippable), with a toolbar: "← All cards" (back to the grid,
  keeping `q`/`tag`), and at the end Edit card and Delete card (`danger`,
  `data-confirm`). Previous/next arrow buttons beside the card step through
  the current filtered order (they carry `q`/`tag`); hidden at the ends. The
  back face shows the answer, notes, tag pills (links to the in-deck tag
  filter), image and audio (`<audio controls>`). The image appears on the
  front face.
- **Cross-deck tag page** `/flash/tags/{tag}`: restyled as a grid of mini
  cards, each carrying its own deck's colour class and a small deck-name
  label; each links to that card in its deck.
- The old `#card-list` / `card-list-items` templates are removed; tests that
  assert on them are updated.

## 4. U3 — Card editor

- New card and Edit card open in the right pane (replacing the grid or the
  opened card). Cancel returns to where the user came from (grid, or the
  opened card).
- **Layout** (see mockup): type toggle; two card-shaped faces side by side
  (stack vertically below 640px) — "Front: the question" and "Back: the
  answer", each a textarea styled as the card face with the deck stripe;
  "Extra note (shown after the answer)"; Tags; image and audio drop zones;
  actions **Save card** (primary), **Save and add another**, **Cancel**.
- **Type toggle**: two radios (`card_type` = `basic` / `cloze`) styled as a
  segmented control. Choosing "Fill in the blank" hides the Back face with
  CSS alone (`.flash-editor:has(input[value=cloze]:checked)`), so it works
  without JS. With JS, a **Make blank** button appears beside the Front
  face; it wraps the current selection in `{{cN::…}}`, where N is one more
  than the highest existing number in the textarea. A cloze card's Back is
  ignored on save, so text typed there before switching type can't trip
  validation.
- **One form for everything**: create (`POST /flash/{deckID}/cards/new`) and
  update (`POST /flash/{deckID}/cards/{cardID}`) become
  `multipart/form-data`, wrapped in `web.LimitBody` with the same limit the
  media route uses, and accept the text fields plus optional `image`,
  `audio`, `remove_image`, `remove_audio`, reusing the existing media
  handling code from `handlers_media.go`. The separate "Update media" form on
  the card view is removed. The `POST …/media` route and its tests stay as
  they are (it remains a valid endpoint and its tests keep covering the
  media pipeline); the upload/remove logic is factored into a helper shared
  by that route and the create/update handlers. No template links to the
  route any more.
- **Tag-pill input**: with JS, a pill editor (Enter or comma adds a pill,
  Backspace in an empty input removes the last, × removes one) that keeps a
  hidden `tags` input in sync as the existing comma-separated value. Without
  JS the plain `tags` text input is shown.
- **Drop zones**: each file input is wrapped in a `<label>` styled as a
  dashed drop zone ("Drop an image or click to choose" / "Drop a sound or
  click to choose"). With JS, drag-and-drop sets the input's files and shows
  the filename (and an image thumbnail). When the card already has media, the
  zone shows the current image/audio and a "Remove" checkbox styled as ×.
- **Save and add another**: a second submit button, `name="next"
  value="new"`. On success the handler responds with an empty new-card form
  for the same deck, a "Card saved" notice, and the card type and tags
  carried over (without JS: a redirect to `…/cards/new?saved=1&type=…&tags=…`).
  The deck list refreshes out of band as with every pane change.
- **Errors**: `.notice-error` at the top of the form; every field keeps its
  value (current behaviour, restyled). An upload error does not lose the
  text fields.

## 5. U4 — Review

- **Focused layout**: `/flash/review` and `/flash/review/{deckID}` keep the
  suite header but have no deck list. Top bar: "← Stop" (to the deck pane
  for a deck review, to home for Review all), deck name (or "All decks"), a
  progress bar in `var(--deck)` (accent colour for Review all), and "N of M".
  M = cards graded today in this scope + cards remaining in the queue; N = graded
  today + 1. Both come from existing data, so a reload keeps the count.
- **Card**: `flash-card` at large size, max-width ~28rem. Front: the
  question (cloze as blanks), image if any, a faint "new" label for new
  cards. Back: answer, notes, tag pills, audio. A large "Show answer
  (Space)" button under the card, or tapping the card, flips it.
- **Grade buttons** appear only once the card is flipped (CSS sibling
  selector off the flip checkbox, so no JS is needed): four large buttons in
  a row — Forgot (red), Hard (amber), Got it (green/teal), Easy (blue) —
  each tinted background with a matching border and dark text, showing its
  key (1–4) underneath. New `--flash-grade-*` tokens, light and dark.
- **Undo** (U) stays as a small text button under the grades.
- Grading and undo post as today (`hx-target="#review-body"`); the next card
  arrives face up. The deck scope query parameter behaviour is unchanged.
- **Session summary** (queue empty), replacing "All done for now":
  - "Nice work! You reviewed N cards today" (N from today's
    `flash_review_counts` for this scope; if N is 0, "Nothing to review
    right now" instead, with no breakdown or celebration).
  - A stacked breakdown bar (got it / hard / forgot / easy) from today's
    `again/hard/good/easy_count` columns, with a text legend.
  - "🔥 N days in a row" from `Store.Streak` (omit when the streak is 0 or 1).
  - One big next-step button: "Review *Deck* next (N due)" linking to the
    deck with the most due cards (excluding snoozed decks) if any, otherwise
    "Back to decks".
  - A short CSS-only celebration (a burst of five or six small card shapes
    in deck colours, ~1s, plays once on render); disabled under
    `prefers-reduced-motion`.
  - Stateless by design: "today's" counts, not a per-session tally, so it is
    reload-safe and needs no session storage.

## 6. U5 — Sharing and snooze

- **Share popover**: the deck toolbar's Share button is a `<details>`
  disclosure (the same no-JS mechanism as the Notes shortcuts menu) whose
  panel has a person picker + Share button, and a "Shared with" list with
  **one row per person**. Each row has a status pill for their latest offer
  ("waiting" / "added" / "said no thanks"), and Revoke while waiting.
  Hidden entirely when there is no other account to share with. After a
  share or revoke, the pane re-renders with the popover open.
  - A re-share replaces the person's row with "waiting".
  - A revoke cancels an offer; it isn't an answer. Revoked offers are
    ignored, so revoking a re-offer puts the row back to the person's last
    real answer, and someone whose offers were all revoked isn't listed.
  - "Latest" is `created_at DESC, id DESC`, computed in SQL
    (`SharesForDeck`). At most one offer per person is ever pending, so a
    waiting row is always the pending offer Revoke acts on. (#304)
- **Gift decks**: every pending offer (`SharesForRecipient`) is shown at the
  top of the deck list as a gift row — a dashed-outline stack in the source
  deck's colour, the deck name, and "From Name". Clicking it loads a new
  route, `GET /flash/shared/{shareID}` (recipient-scoped: 404 unless the
  share is addressed to the signed-in user and pending), into the right
  pane:
  - First-time offer: "Name shared this deck with you", name, description,
    card count, the first four cards as mini cards, and **Add to my decks**
    (primary) / **No thanks**. The name shown is the source deck's — that's
    the deck actually being offered.
  - Merge offer: "Name added N new cards to *Deck*", with **Add the new
    cards** / **No thanks**. The gift row's badge reads "+N". *Deck* here is
    the recipient's own already-adopted copy, not the sender's source deck —
    if that copy was auto-suffixed on first adoption (e.g. "Spanish (from
    alice)"), the merge row and pane use that name, since it's the deck
    cards are actually being added to.
  - Merge offer with nothing new (the recipient already has every card):
    the gift row has no badge, and the pane says "You already have every
    card in *Deck*" with a single **Got it**, again naming the recipient's
    own adopted copy. Got it adopts the offer, which copies nothing, so it
    leaves the list. (#346)
  - Buttons post to the existing adopt/decline routes. After adopting, the
    pane opens the (new or merged) deck with a notice: "“*Planets*” is now
    in your decks", "N new cards added to “*Planets*”", or, after Got it,
    "“*Planets*” is already up to date". After declining, the pane shows the
    default state.
  - Adding a first-time offer never fails over a name the recipient
    already uses. The copy is named "Spanish (from alice)", or "Spanish
    (from alice) (2)", "(3)"… if that's taken too. Only the base name is
    shortened, so the result stays within the 120-character name limit. A
    later merge finds the copy by id, whatever it's called by then. (#304)
  - Reading the sample cards needs a new store method that reads cards from
    the share's source deck **only** after verifying the share is pending
    and addressed to the caller (the existing `…IgnoringOwner` helpers read
    without an owner check, so the guard must be explicit and tested).
- The old "Shared with me" list above the decks is removed.
- **Snooze** moves into the Edit deck pane as a "Take a break" section below
  the form (separate forms, not nested): "1 week" and "1 month" buttons
  posting to the existing snooze route, or, when snoozed, "Taking a break
  until 30 Sep" with **End break** (existing unsnooze route).
- **Snoozed deck pane**: a calm banner, "Taking a break until 30 Sep", with
  **End break**, shown in place of the Review button.

## 7. U6 — Import and stats

- **Import pane** (`GET /flash/import`, already in the right pane):
  - A three-step strip: ① "Copy the prompt" → ② "Ask your AI for cards on
    any topic" → ③ "Paste the answer below".
  - **Copy prompt** button: copies a ready-made LLM prompt describing the ON
    Flash JSON format (deck name/description, basic and cloze cards,
    `{{c1::…}}` markers, notes, tags, optional image URL, "reply with JSON
    only"). The prompt is an embedded text file next to `import.go`
    (`import_prompt.txt`) with a `[TOPIC]` placeholder; a test checks it
    mentions every JSON field the importer accepts, so it can't drift. With
    JS it copies and shows "Copied"; without JS the prompt is in a
    `<details>` with a read-only, select-all textarea.
  - A large paste area; format auto-detect by default, the format `<select>`
    moved into a small "Choose format" `<details>`.
  - On success, the pane opens the new deck with "Imported N cards".
- **Stats in the right pane**: `GET /flash/stats` renders the home layout
  with stats in the pane on a full page load, and just the pane over HTMX;
  the app toolbar's Stats button targets the pane. Content:
  - Restyled tiles (same style as the deck pane tiles).
  - The daily reviews chart, restyled; the daily-figures `<details>` table
    stays as the text alternative.
  - Per-deck rows replacing the table: small stack, name, a "mastered"
    progress bar (mastered / total cards, in `var(--deck)`), due badge,
    "taking a break" when snoozed. Each row loads that deck into the pane.

## 8. Testing

- Handler tests keep asserting structure with `internal/htmlassert` (ids,
  roles, links, form actions), not CSS. Every existing Flash test either
  keeps passing or is updated deliberately in the same PR.
- New unit tests: cloze renderer (1.4), `ValidDeckColor`, `CardStatuses`,
  `DeckSummaries` (if added), the share-scoped sample-card reader (including
  the "not your share" and "not pending" cases), import prompt field
  coverage, and the "Save and add another" handler path.
- Migration 0010 applies cleanly on a database with existing decks (they
  become teal).
- `go test ./internal/arch/...` stays green (no new imports across app
  boundaries).
- Manual check per PR in the browser preview: light and dark theme, desktop
  and tablet width (768px), keyboard-only flow, and one no-JS pass on the
  flip and the forms. Attach screenshots to the PR.
