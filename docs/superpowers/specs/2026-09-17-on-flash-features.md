# ON Flash — feature list & usage workflows

This is a pre-spec brainstorm for **ON Flash**, the reserved-but-unbuilt app
in the suite (see [AGENTS.md](../../../AGENTS.md)). It captures the desired
feature set and a few usage workflows, gathered through Q&A, to guide the
implementation plan and eventual design spec. This is intentionally a list,
not a detailed spec — architecture, data model, and UI details are worked
out in the plan/spec that follows.

## Design goals

- Simple, elegant UI — approachable enough for a young student to use
  unassisted.
- Cards are expected to be authored primarily by pasting LLM-generated
  content, not typed by hand.
- Scheduling should be automatic by default, adjustable when life gets busy,
  and should avoid the inconsistent-difficulty and heavy-gamification
  problems seen in some existing apps.
- Keyboard-first interaction, matching the rest of the suite (ON Notes, ON
  Reader) — reviewing and grading a card should never require reaching for
  the mouse.

## Feature list

**Decks & cards**
- Flat decks (no nesting); cards can carry tags for cross-deck filtering.
- Card types: Basic (front/back) and Cloze deletion. Typed-input recall is a
  noted future addition, not v1.
- Media per card: images and audio. Attachable two ways — by URL at import
  time, or by manual upload/attach afterward for polishing. Media referenced
  by URL is fetched once and stored/served locally (not hotlinked), the same
  approach ON Reader uses for article images — avoids CORS/CSP issues and
  keeps cards working if the source URL later disappears.
- Optional notes/explanation field per card, shown only after the answer is
  revealed — extra context to reinforce learning without cluttering the
  question/answer flow.
- Personal decks (private to one account) and shared decks (see Sharing).

**Import / authoring**
- Import screen accepts either Markdown or JSON (auto-detected or picked by
  the user), following a documented ON Flash format.
- Workflow is "generate elsewhere, paste here": the user prompts an LLM of
  their choice using the documented format, then pastes the result into the
  import screen.
- In-app LLM generation (type a topic, get cards back with no copy-paste
  step) is a possible future addition, not v1 — keeps the app free of
  API-key/cost management for now.

**Scheduling & review**
- FSRS-based scheduling, tracked per card *per account* — a shared deck can
  be at a different stage of learning for each person reviewing it.
- Self-grading on a 4-point scale each review: Again / Hard / Good / Easy.
  Labels can be presented more simply for younger users without changing
  the underlying data.
- Manual overrides: snooze or pause a deck, adjust its pace.
- Per-deck configurable daily limits (max new cards/day, max reviews/day) so
  a freshly imported large deck doesn't overwhelm day one.
- Undo last rating — one step back to correct an accidental click, double
  tap, or immediate regret on a grade, restoring the card's prior scheduling
  state.

**Sharing**
- Direct account-to-account sharing within the household — no link step.
  The recipient sees it in a "shared with me" list and adopts it.
- Once adopted, review/scoring/scheduling is entirely private to each
  account, with no visibility into another account's progress on a shared
  deck (including for the original sharer).
- Adoption creates an independent copy of the deck's cards, not a live link
  to the original — the creator editing or deleting cards afterward doesn't
  touch an adopter's copy or their progress.
- Re-sharing (creator adds cards, shares again) lets an adopter merge only
  the new cards into their existing copy, without disturbing progress on
  cards already adopted. This requires tracking each adopted card's origin
  card so the merge can tell new from already-adopted.

**Progress & motivation**
- Visible, data-forward stats: streaks, retention rate, cards due/mastered,
  per-deck review load. Deliberately not game-forward — no points, badges,
  or leaderboards.

## Example usage workflows

**Workflow A — Learning a new language**
1. Prompt an LLM: "Give me 30 flashcards for common travel phrases in
   [language], as JSON in ON Flash's schema, with an image URL for each
   noun."
2. Paste the JSON into ON Flash's import screen — a new deck appears with
   cards and images already attached.
3. Each day, ON Flash shows cards due (capped by the configured daily
   limit); the user grades recall on the 4-point scale.
4. Over time, the stats page shows retention trending and a review streak.

**Workflow B — Sharing a deck with another household member**
1. Prompt an LLM for cloze-deletion cards covering a school subject, in
   Markdown.
2. Import creates the deck under the creator's account; after reviewing it,
   share it directly to another account in the household.
3. The recipient sees it under "shared with me", adopts it, and reviews on
   their own schedule and daily limit, with grading labels suited to their
   level. Their progress stays private to their account.

**Workflow C — Polishing an imported deck with audio**
1. Import a text-only deck via Markdown (LLM output can't include audio).
2. Afterward, attach an audio clip (uploaded file) to each card — e.g. for
   ear-training or pronunciation practice alongside the term.

## Open items deferred to later

- In-app LLM generation (v2 candidate).
- Typed-input card type (v2 candidate).
- Exact Markdown/JSON import schema (to be defined in the implementation
  plan/spec).
