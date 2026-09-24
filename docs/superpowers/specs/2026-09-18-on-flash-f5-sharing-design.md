# ON Flash F5 — sharing (design spec)

F5 lets a deck's creator share it directly with another account on the same
on-suite instance — no link, no token, no auth-bypass route. Since on-suite
is self-hosted per household, "share within the household" is simply "share
with any other account on this instance." The recipient sees it in a
"shared with me" list and explicitly adopts it, which copies the deck's
cards into a brand-new deck they own. From that point on, review progress
(FSRS state) is entirely private per account, including from the original
creator — a shared deck can be at a completely different stage of learning
for each person reviewing it.

This is a different shape from ON Notes' N9 public-link sharing
(`internal/apps/notes/share.go`): Notes shares an unguessable public URL
that anyone with the link can view, with no target account and no copy —
one live document, read-only. Flash's sharing is account-to-account, push
rather than link, and produces an independent, editable copy rather than a
shared view.

## Scope

- One deck at a time, shared to one or more specific accounts on the
  instance — not a public link, not a "make discoverable" toggle.
- The recipient dropdown is populated from the platform's existing account
  list (the same data `internal/platform/admin` already reads via
  `auth.Store.ListAccounts`), excluding the current account. There is no
  new household/grouping concept — every other account on the instance is a
  valid recipient.
- Adoption always **copies** cards into a new deck the recipient owns. There
  is no live/linked view of the creator's deck from the recipient's side.
- Media (`image_hash`/`audio_hash`) is carried over by reference only — the
  content-addressed `flash_media` row is shared as-is, never re-fetched or
  duplicated, since it's immutable and keyed by hash. Because the hashes
  land on the recipient's own cards, the media route (which serves a hash
  only to someone whose own card uses it) serves them the files from the
  moment they adopt, not before. The row outlives the sharer's card for as
  long as the copy uses it (F4 spec, "Orphan cleanup"; #302).
- Re-sharing (creator adds cards, shares again) produces a second offer the
  recipient can merge — only genuinely new cards are added to their
  existing copy; cards and progress they already have are untouched.
- No expiry, no permission levels (sharing is always read/copy, never
  "collaborate live"), no per-recipient customization of what's shared —
  the whole deck's current card set goes every time.
- Out of scope: unsharing a deck the recipient has *already adopted*
  (their copy is theirs now, deleting it is an ordinary deck delete);
  passing a share on to a third account (the recipient's adopted deck is
  just an ordinary owned deck — nothing stops them from sharing it onward
  themselves, and nothing needs to, since it behaves identically to any
  other deck they own).

## Architecture

One migration:

- `flash_shares` — one row per share offer:
  ```sql
  CREATE TABLE flash_shares (
      id INTEGER PRIMARY KEY,
      deck_id INTEGER NOT NULL REFERENCES flash_decks (id) ON DELETE CASCADE,
      from_user_id INTEGER NOT NULL,
      to_user_id INTEGER NOT NULL,
      status TEXT NOT NULL CHECK (status IN ('pending','adopted','declined','revoked')),
      adopted_deck_id INTEGER REFERENCES flash_decks (id),
      created_at TEXT NOT NULL,
      responded_at TEXT
  ) STRICT;
  ```
  `ON DELETE CASCADE` on `deck_id`: if the creator deletes the source deck,
  every share row referencing it (pending, adopted, declined, or revoked)
  is removed along with it. This never touches an adopted recipient's own
  deck (`adopted_deck_id` points at a separate, independent row) — it only
  removes the now-meaningless tracking/offer row. A still-pending offer for
  a deleted deck simply disappears from the recipient's "shared with me"
  list rather than becoming a dangling or erroring entry.
- One nullable column added to `flash_cards`: `origin_card_id INTEGER
  REFERENCES flash_cards (id) ON DELETE SET NULL`. Set on every card copied
  during adoption or merge, to the source card's own ID. This is the merge
  mechanism: a later re-share's merge step copies only source cards whose ID
  does not already appear as some existing card's `origin_card_id` in the
  target deck. `ON DELETE SET NULL` means deleting the original source card
  later doesn't touch the adopter's already-copied card, it just loses the
  now-meaningless back-reference.

New file, following the established one-file-per-concern split:

- **`share.go`** — the whole feature: `Store.ShareDeck`, `Store.RevokeShare`,
  `Store.DeclineShare`, `Store.AdoptShare` (handles both first-adoption and
  merge, since the copy logic is identical either way — only the
  destination deck differs), `Store.SharesForDeck` (creator's "shared with"
  list), `Store.SharesForRecipient` (recipient's "shared with me" list),
  plus `handlers_share.go` for the HTMX routes and `templates` additions to
  `decks.html`/`cards.html`.

## Sharing, revoking, declining

`ShareDeck(ctx, fromUserID, deckID, toUserID)`:
1. Confirm `deckID` belongs to `fromUserID` (same ownership check pattern as
   every other deck-scoped store method).
2. Reject `toUserID == fromUserID`.
3. Look up any existing row for `(deckID, fromUserID, toUserID)` with
   `status = 'pending'`. If found, this call is a no-op (idempotent —
   re-clicking Share while an offer is still outstanding doesn't create a
   second entry in the recipient's list).
4. Otherwise insert a new `pending` row. This covers both "first share" and
   "re-share for merge" — the two are distinguished later, at display and
   adopt time, purely by whether an `adopted` row already exists for that
   same `(deckID, fromUserID, toUserID)` triple, not by anything stored on
   the new row itself.

`RevokeShare(ctx, fromUserID, shareID)`: confirms the row's `from_user_id`
matches, confirms `status = 'pending'` (revoking an already-adopted or
already-resolved share is a no-op error, `ErrInvalid`), sets
`status = 'revoked'`, `responded_at = now`.

`DeclineShare(ctx, toUserID, shareID)`: confirms the row's `to_user_id`
matches and `status = 'pending'`, sets `status = 'declined'`,
`responded_at = now`. A declined share never blocks a future fresh
`ShareDeck` call from the same creator to the same recipient — step 3 above
only checks for an existing *pending* row, and a declined row isn't one.

## Adoption & merge

`AdoptShare(ctx, toUserID, shareID)`:
1. Confirm the row's `to_user_id` matches and `status = 'pending'`.
2. Look up the most recent `adopted` row for the same
   `(deck_id, from_user_id, to_user_id)` triple, if any. Its presence is
   what distinguishes first-adoption from merge.
3. **First adoption** (no prior `adopted` row): in one transaction, create a
   new `flash_decks` row owned by `toUserID` (name and description copied
   from the source deck; `NewCardsPerDay`/`ReviewsPerDay`/`SnoozedUntil` set
   to their normal defaults, not copied — this is a fresh deck with its own
   pace). Copy every card from the source deck into it: `card_type`,
   `front`, `back`, `notes`, `image_hash`, `audio_hash` copied as-is, tags
   copied by name via the existing `upsertCardTags` helper (creating the
   tag under `toUserID`'s account if they don't already have one with that
   name), and `origin_card_id` set to the source card's own ID. No FSRS
   review state is copied — every copied card starts as brand new in the
   recipient's review queue, identical to a freshly imported card. Set this
   share row's `status = 'adopted'`, `adopted_deck_id` to the new deck,
   `responded_at = now`.
4. **Merge** (a prior `adopted` row exists): same per-card copy logic,
   targeting that prior row's `adopted_deck_id` instead of a new deck, and
   restricted to source cards whose ID is not already present as an
   `origin_card_id` among the target deck's cards. Cards and their progress
   already in the target deck are never touched. Set this row's
   `status = 'adopted'`, `adopted_deck_id` to that same existing deck,
   `responded_at = now`.

The number of new cards a pending merge offer represents — shown in the
recipient's list as e.g. "4 new cards to merge" — is computed at display
time as `COUNT(source deck's cards) - COUNT(matching origin_card_id rows in
the target deck)`, not stored anywhere, so it's always accurate even if
more cards were adopted or deleted between offers.

## UI

- **Deck detail page** (creator's view): a "Share" control opens a
  dropdown of every other account on the instance (excluding the viewer),
  populated via a small platform-level accessor Flash's `App` is
  constructed with (mirroring how other cross-cutting platform services are
  injected — no new cross-app import). Below it, a "Shared with" list:
  recipient username, status (`Pending`, `Adopted`), and for pending rows a
  Revoke button.
- **Deck list page**: a new "Shared with me" section above or alongside the
  account's own decks, listing every `pending` share addressed to the
  viewer: creator's username, and either "New deck" (first-time offer) or
  "N new cards to merge" (an offer where an adopted row already exists),
  each with an Adopt/Merge button and a Dismiss button.
- Once adopted, the resulting deck is an entirely ordinary owned deck
  everywhere else in Flash (review, card list, tags, stats) — nothing
  marks it as having originated from a share, since editing and progress on
  it are fully independent of the source from this point on.

## Error handling & edge cases

- **Self-share**: the recipient dropdown excludes the viewer's own account;
  `ShareDeck` also rejects `to_user_id == from_user_id` server-side as
  defense in depth (`ErrInvalid`), in case of a stale/tampered form.
- **Single-account instance**: the dropdown is simply empty and the Share
  control has nothing to offer — no special-casing needed, this falls out
  of the query naturally.
- **Double-adopt race** (two tabs, same pending row): `AdoptShare`'s status
  check (`status = 'pending'`) inside the transaction means a second
  concurrent call finds the row already `adopted` and fails with
  `ErrNotFound`/`ErrInvalid` rather than double-copying cards.
- **Revoking a merge offer**: identical to revoking a first-time offer —
  it's still just a `pending` row; the recipient's already-adopted deck
  from the earlier share is untouched either way.

## Testing

- `share_test.go` — the full state machine against a real SQLite fixture:
  share → adopt (verifies deck copy, tag copy, media-hash reuse, zero FSRS
  state, `origin_card_id` set correctly) → re-share → merge (verifies only
  new cards copied, existing cards/progress untouched, correct "N new"
  count) → decline → re-share-after-decline → revoke. Also: self-share
  rejection, double-adopt race (sequential calls simulating the race),
  ownership checks (wrong `from_user_id`/`to_user_id` on revoke/decline/
  adopt), and deck deletion cascading its share rows without touching an
  adopted recipient's own deck.
- `handlers_share_test.go` — the HTMX routes via `apptest.Server`: Share
  (recipient dropdown populated, offer created), Revoke, Decline, Adopt,
  Merge, sign-in/CSRF requirements, and that an adopted deck appears
  correctly in the recipient's ordinary deck list afterward.

## Open items deferred to later

- Any notification beyond the "shared with me" list itself (e.g. email or
  in-app badge/unread count).
- Passing a share on to a third account, or any multi-hop sharing chain.
- Per-recipient partial sharing (choosing a subset of cards or tags to
  share rather than the whole deck).
