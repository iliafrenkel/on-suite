# ON Flash UI U5 — Sharing and snooze Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Tidy the deck pane: sharing moves into a Share popover in the deck toolbar, decks shared *with* you appear as "gift" decks at the top of the list with a preview pane, and snoozing moves into Edit deck as "Take a break", with a calm banner on a snoozed deck.

**Architecture:** `SharesForRecipient` gains the source deck's colour, and a new recipient-guarded `SharePreview` store method reads a pending share's source deck and a few sample cards. The deck list renders gift rows from the offers `buildDeckIndex` already loads; a new `GET /flash/shared/{shareID}` route shows the preview in the pane (mode `gift`). The old "Shared with me" block is removed. No schema change.

**Tech Stack:** Go, SQLite, `html/template`, HTMX, `<details>` popover (no JS), CSS.

**Spec:** [docs/superpowers/specs/2026-09-23-on-flash-ui-overhaul-design.md](../specs/2026-09-23-on-flash-ui-overhaul-design.md) — §6. Mockups: section 1 (the dashed "From Dad" row).

**This is PR 5 of 6.** It requires U1–U4 on `main`.

## Global Constraints

- Branch: `feat/flash-ui-u5` off an up-to-date `main` containing U1–U4. Never push to `main`.
- Full check before each Go commit:
  ```bash
  gofmt -l .
  go vet ./...
  go run honnef.co/go/tools/cmd/staticcheck@v0.8.0 ./...
  go mod tidy && git diff --exit-code go.mod go.sum
  go test ./... -race -count=1
  ```
- No new dependencies; CSP: no inline `<script>`, no `style=`.
- **Security:** a share's source deck belongs to someone else. `SharePreview` must return `ErrNotFound` unless the share is **pending** and **addressed to the caller** — the same indistinguishable 404 the rest of the app uses. The existing `…IgnoringOwner` helpers do no owner check; never call them without that guard first.
- Routes: adopt/decline stay `POST /flash/shared/adopt` and `POST /flash/shared/decline` with `share_id` in the body (see the long route comment in `flash.go` for why). The new preview route is `GET /flash/shared/{shareID}` — a literal-then-wildcard GET, the same safe shape as `GET /edit/{deckID}`.
- `htmlassert` selectors: descendant chains of simple parts only; no compound parts.
- From U1–U4 (do not rename): `buildDeckIndex`, `deckDetailView`, `deckListFragment`, `newDeckListItem`, `shareContext`, `sharedWithMeForViewer`, `shareWithUsername`, `shareOfferWithUsername`, templates `deck-toolbar`, `deck-list-items`, `deck-detail-with-list`, `flash-stack`, `flash-mini-card`, `deck-detail-view`, `deck-detail-edit`; CSS `.deck-row*`, `.flash-big-btn*`, `.flash-share-list`, `.flash-section-title`.
- Tests: `newShareServer(t)`, `createDeckHX(t, s, sess, name)` (in `handlers_share_test.go`), `newServer(t)`, `s.Get`, `s.PostHX`, `s.Do`, `httpGet`, `itoa`; store tests `newFixture(t)`.
- Commits: Conventional Commits, scope `flash`.

---

## File map

| File | Change |
|---|---|
| `internal/apps/flash/share.go` | `ShareOffer.DeckColor`; `SharePreview` type + method |
| `internal/apps/flash/handlers_share.go` | `giftPreview` handler; adopt sets a notice; share/revoke open the popover |
| `internal/apps/flash/handlers_decks.go` | `deckDetailView` gains `Gift`, `Notice`, `ShareOpen`, `Snoozed`; `deckListFragment.Gifts`; `giftRow`; share context for cards mode; status labels |
| `internal/apps/flash/flash.go` | route `GET /shared/{shareID}` |
| `internal/ui/toolbar_icons.go` (+ test) | icon `share` |
| `internal/apps/flash/templates/decks.html` | gift rows; remove "Shared with me"; break banner; snooze into Edit; notice; `gift` mode |
| `internal/apps/flash/templates/cards.partial.html` | `deck-toolbar` gets the Share popover |
| `internal/apps/flash/templates/card.partial.html` | `flash-mini-card` renders a non-link when `Href` is empty |
| `internal/apps/flash/templates/gift.partial.html` | **Create** — `deck-detail-gift` |
| `internal/ui/static/app.css` | popover, gift rows, gift pane, break banner, status pills |
| Tests | `share_test.go`, `handlers_share_test.go`, `handlers_decks_test.go`, `internal/ui/toolbar_icons_test.go` |

---

### Task 1: Store — offer colour and a guarded preview

**Files:**
- Modify: `internal/apps/flash/share.go`
- Test: `internal/apps/flash/share_test.go`

**Interfaces:**
- Produces:
  - `ShareOffer.DeckColor string` (the source deck's colour).
  - ```go
    type SharePreview struct {
        Offer     ShareOffer
        Deck      Deck   // the SOURCE deck; only Name, Description and Color are meant for display
        CardCount int    // cards the recipient would get: all of them, or only the new ones for a merge
        Samples   []Card // up to four of those cards, oldest first
    }
    func (st *Store) SharePreview(ctx context.Context, toUserID, shareID int64) (SharePreview, error)
    ```
  - `const sharePreviewSamples = 4`

- [ ] **Step 1: Write the failing tests**

Append to `internal/apps/flash/share_test.go`:

```go
func TestSharesForRecipientCarriesDeckColor(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	d, err := f.store.CreateDeck(ctx, f.alice.ID, "Planets", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.SetDeckColor(ctx, f.alice.ID, d.ID, "purple"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.ShareDeck(ctx, f.alice.ID, d.ID, f.bob.ID); err != nil {
		t.Fatal(err)
	}
	offers, err := f.store.SharesForRecipient(ctx, f.bob.ID)
	if err != nil || len(offers) != 1 {
		t.Fatalf("offers = %+v, %v", offers, err)
	}
	if offers[0].DeckColor != "purple" {
		t.Errorf("DeckColor = %q, want purple", offers[0].DeckColor)
	}
}

func TestSharePreviewForTheRecipient(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	d, err := f.store.CreateDeck(ctx, f.alice.ID, "Planets", "The solar system")
	if err != nil {
		t.Fatal(err)
	}
	for _, front := range []string{"Mercury", "Venus", "Earth", "Mars", "Jupiter"} {
		if _, err := f.store.CreateCard(ctx, f.alice.ID, d.ID, flash.CardTypeBasic, front, "a planet", ""); err != nil {
			t.Fatal(err)
		}
	}
	sh, err := f.store.ShareDeck(ctx, f.alice.ID, d.ID, f.bob.ID)
	if err != nil {
		t.Fatal(err)
	}

	p, err := f.store.SharePreview(ctx, f.bob.ID, sh.ID)
	if err != nil {
		t.Fatalf("SharePreview: %v", err)
	}
	if p.Deck.Name != "Planets" || p.Deck.Description != "The solar system" {
		t.Errorf("preview deck = %+v", p.Deck)
	}
	if p.CardCount != 5 {
		t.Errorf("CardCount = %d, want 5", p.CardCount)
	}
	if len(p.Samples) != 4 || p.Samples[0].Front != "Mercury" {
		t.Errorf("Samples = %+v, want the first four, oldest first", p.Samples)
	}
}

func TestSharePreviewIsOnlyForThePendingRecipient(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	d, err := f.store.CreateDeck(ctx, f.alice.ID, "Planets", "")
	if err != nil {
		t.Fatal(err)
	}
	sh, err := f.store.ShareDeck(ctx, f.alice.ID, d.ID, f.bob.ID)
	if err != nil {
		t.Fatal(err)
	}
	// The sharer is not the recipient.
	if _, err := f.store.SharePreview(ctx, f.alice.ID, sh.ID); !errors.Is(err, flash.ErrNotFound) {
		t.Errorf("preview as the sharer: err = %v, want ErrNotFound", err)
	}
	// An unknown share.
	if _, err := f.store.SharePreview(ctx, f.bob.ID, sh.ID+100); !errors.Is(err, flash.ErrNotFound) {
		t.Errorf("preview of a missing share: err = %v, want ErrNotFound", err)
	}
	// A resolved share is no longer previewable.
	if err := f.store.DeclineShare(ctx, f.bob.ID, sh.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.SharePreview(ctx, f.bob.ID, sh.ID); !errors.Is(err, flash.ErrNotFound) {
		t.Errorf("preview of a declined share: err = %v, want ErrNotFound", err)
	}
}

func TestSharePreviewOfAMergeCountsOnlyNewCards(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	d, err := f.store.CreateDeck(ctx, f.alice.ID, "Planets", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.CreateCard(ctx, f.alice.ID, d.ID, flash.CardTypeBasic, "Mercury", "x", ""); err != nil {
		t.Fatal(err)
	}
	sh, err := f.store.ShareDeck(ctx, f.alice.ID, d.ID, f.bob.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.AdoptShare(ctx, f.bob.ID, sh.ID); err != nil {
		t.Fatal(err)
	}
	for _, front := range []string{"Venus", "Earth"} {
		if _, err := f.store.CreateCard(ctx, f.alice.ID, d.ID, flash.CardTypeBasic, front, "x", ""); err != nil {
			t.Fatal(err)
		}
	}
	again, err := f.store.ShareDeck(ctx, f.alice.ID, d.ID, f.bob.ID)
	if err != nil {
		t.Fatal(err)
	}
	p, err := f.store.SharePreview(ctx, f.bob.ID, again.ID)
	if err != nil {
		t.Fatal(err)
	}
	if p.Offer.PriorAdoptedDeckID == nil || p.CardCount != 2 {
		t.Errorf("merge preview = %+v, want a merge of 2 new cards", p)
	}
	for _, c := range p.Samples {
		if c.Front == "Mercury" {
			t.Errorf("merge samples include the already-adopted card: %+v", p.Samples)
		}
	}
}
```

- [ ] **Step 2: Run to verify they fail**

Run: `go test ./internal/apps/flash/ -run 'DeckColor|SharePreview' -count=1`
Expected: build failure — `DeckColor`/`SharePreview` undefined.

- [ ] **Step 3: Implement**

In `internal/apps/flash/share.go`:

Add `DeckColor string` to `ShareOffer`, right after `DeckName string`:

```go
	DeckName  string
	DeckColor string // the source deck's colour, for the gift row
```

In `SharesForRecipient`, select the colour after the name — change `d.name,` in the SELECT list to `d.name, d.color,`, and change the Scan to:

```go
		err := rows.Scan(&o.ID, &o.DeckID, &o.FromUserID, &o.ToUserID, &o.Status, &adoptedDeckID, &createdAt, &respondedAt,
			&o.DeckName, &o.DeckColor, &priorAdoptedDeckID)
```

Append:

```go
// sharePreviewSamples is how many cards a gift deck's preview shows.
const sharePreviewSamples = 4

// SharePreview is what the recipient sees before adopting a share: the
// source deck's name, description and colour, how many cards they would
// get, and a few of them. Deck is the *source* deck — someone else's — so
// only its display fields are meant to be shown.
type SharePreview struct {
	Offer     ShareOffer
	Deck      Deck
	CardCount int
	Samples   []Card
}

// SharePreview loads the preview of one of toUserID's pending offers. Any
// other share id — missing, addressed to someone else, or already
// resolved — is ErrNotFound. That guard is SharesForRecipient itself (it
// only returns pending offers addressed to toUserID), and it runs before
// the source deck is read without an owner check.
func (st *Store) SharePreview(ctx context.Context, toUserID, shareID int64) (SharePreview, error) {
	offers, err := st.SharesForRecipient(ctx, toUserID)
	if err != nil {
		return SharePreview{}, err
	}
	var p SharePreview
	found := false
	for _, o := range offers {
		if o.ID == shareID {
			p.Offer, found = o, true
			break
		}
	}
	if !found {
		return SharePreview{}, ErrNotFound
	}

	p.Deck, err = scanDeck(st.db.QueryRowContext(ctx,
		`SELECT `+deckColumns+` FROM flash_decks WHERE id = ?`, p.Offer.DeckID))
	if err != nil {
		return SharePreview{}, err
	}

	// A merge only brings the cards not already adopted; a first-time
	// adopt brings them all. Same NOT EXISTS rule as newCardCount.
	where := `c.deck_id = ?`
	args := []any{p.Offer.DeckID}
	if p.Offer.PriorAdoptedDeckID != nil {
		where += ` AND NOT EXISTS (SELECT 1 FROM flash_cards tc WHERE tc.deck_id = ? AND tc.origin_card_id = c.id)`
		args = append(args, *p.Offer.PriorAdoptedDeckID)
	}
	if err := st.db.QueryRowContext(ctx,
		`SELECT count(*) FROM flash_cards c WHERE `+where, args...).Scan(&p.CardCount); err != nil {
		return SharePreview{}, fmt.Errorf("flash: share preview: %w", err)
	}
	rows, err := st.db.QueryContext(ctx,
		`SELECT c.id, c.deck_id, c.user_id, c.card_type, c.front, c.back, c.notes, c.created_at, c.image_hash, c.audio_hash
		   FROM flash_cards c WHERE `+where+`
		  ORDER BY c.created_at ASC, c.id ASC LIMIT ?`,
		append(args, sharePreviewSamples)...)
	if err != nil {
		return SharePreview{}, fmt.Errorf("flash: share preview: %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		c, err := scanCardRow(rows)
		if err != nil {
			return SharePreview{}, err
		}
		p.Samples = append(p.Samples, c)
	}
	return p, rows.Err()
}
```

- [ ] **Step 4: Run to verify they pass**

Run: `go test ./internal/apps/flash/ -count=1`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/apps/flash/share.go internal/apps/flash/share_test.go
git commit -m "feat(flash): recipient-only share preview with sample cards"
```

---

### Task 2: Share icon

**Files:** `internal/ui/toolbar_icons.go`, `internal/ui/toolbar_icons_test.go`

- [ ] **Step 1:** Add `"share"` to the `names` slice in `TestToolbarIconForKnownNames`; run `go test ./internal/ui/ -count=1` → FAIL.
- [ ] **Step 2:** Add to the `toolbarIcons` map:

```go
	"share": `<svg class="toolbar-icon" viewBox="0 0 24 24" aria-hidden="true">
		<circle cx="18" cy="5" r="3"/>
		<circle cx="6" cy="12" r="3"/>
		<circle cx="18" cy="19" r="3"/>
		<path d="M8.6 13.5l6.8 4M15.4 6.5l-6.8 4"/>
	</svg>`,
```

- [ ] **Step 3:** `go test ./internal/ui/ -count=1` → PASS. Commit: `git commit -am "feat(ui): add share toolbar icon"` (stage only the two ui files).

---

### Task 3: Gift decks and the preview pane

**Files:**
- Create: `internal/apps/flash/templates/gift.partial.html`
- Modify: `internal/apps/flash/handlers_decks.go`, `internal/apps/flash/handlers_share.go`, `internal/apps/flash/flash.go`, `internal/apps/flash/templates/decks.html`, `internal/apps/flash/templates/card.partial.html`
- Test: `internal/apps/flash/handlers_share_test.go`, `internal/apps/flash/handlers_decks_test.go`

**Interfaces:**
- Consumes: `SharePreview` (Task 1), `newCardFace`, `cardGridItem`.
- Produces:
  - `const deckModeGift = "gift"`; `deckDetailView.Gift giftView`; `deckDetailView.Notice string`.
  - `type giftRow struct { ShareID int64; DeckName, DeckColor, FromUsername string; IsMerge bool; NewCardCount int }`; `deckListFragment.Gifts []giftRow`, `deckListFragment.ActiveGiftID int64`.
  - `type giftView struct { ShareID int64; DeckName, Description, Color, FromUsername string; IsMerge bool; CardCount int; Samples []cardGridItem; CSRFToken string }`.
  - Handler `giftPreview` on `GET /flash/shared/{shareID}`.
  - CSS hooks: `.deck-row-gift`, `.flash-gift`, `.flash-gift-badge`, `.flash-notice`.

- [ ] **Step 1: Rewrite the four "Shared with me" handler tests and add the gift ones**

In `internal/apps/flash/handlers_share_test.go`, **delete** `TestSharedWithMeRefreshesOutOfBandOnAdoptAndDecline`, `TestSharedWithMeFormsCarryCSRFTokenWithNoDeckSelected` and `TestSharedWithMeShowsSharerUsername` (the list they test no longer exists), and add:

```go
// shareToBob shares one of alice's decks with bob and returns the share id.
func shareToBob(t *testing.T, s *apptest.Server[*flash.Store], deckID int64) string {
	t.Helper()
	s.PostHX(t, s.Alice, "/flash/"+strconv.FormatInt(deckID, 10)+"/share",
		url.Values{"to_user_id": {strconv.FormatInt(s.Bob.User.ID, 10)}})
	offers, err := s.Store.SharesForRecipient(t.Context(), s.Bob.User.ID)
	if err != nil || len(offers) == 0 {
		t.Fatalf("setup: offers = %+v, err = %v", offers, err)
	}
	return strconv.FormatInt(offers[0].ID, 10)
}

func TestGiftRowShowsSharerAndDeck(t *testing.T) {
	s := newShareServer(t)
	deckID := createDeckHX(t, s, s.Alice, "Spanish")
	shareID := shareToBob(t, s, deckID)

	doc := s.Get(t, s.Bob, "/flash/")
	gift := doc.MustHave("#deck-list a.deck-row-gift")
	text := htmlassert.Text(gift)
	if !strings.Contains(text, "Spanish") || !strings.Contains(text, "From alice") {
		t.Errorf("gift row = %q, want the deck name and From alice", text)
	}
	if href, _ := htmlassert.Attr(gift, "href"); href != "/flash/shared/"+shareID {
		t.Errorf("gift row href = %q", href)
	}
	doc.MustNotHave("#shared-with-me")
	doc.MustNotHave(".flash-welcome") // a pending gift is not a first run
}

func TestGiftPreviewPane(t *testing.T) {
	s := newShareServer(t)
	deckID := createDeckHX(t, s, s.Alice, "Spanish")
	for _, front := range []string{"hola", "adiós"} {
		s.PostHX(t, s.Alice, "/flash/"+strconv.FormatInt(deckID, 10)+"/cards/new",
			url.Values{"card_type": {"basic"}, "front": {front}, "back": {"x"}})
	}
	shareID := shareToBob(t, s, deckID)

	doc := s.Get(t, s.Bob, "/flash/shared/"+shareID)
	pane := doc.MustHave("#deck-detail .flash-gift")
	text := htmlassert.Text(pane)
	if !strings.Contains(text, "alice shared this deck with you") || !strings.Contains(text, "2 cards") {
		t.Errorf("gift pane = %q", text)
	}
	if n := len(doc.QueryAll(".flash-gift .flash-mini-card")); n != 2 {
		t.Errorf("gift pane shows %d sample cards, want 2", n)
	}
	for _, sel := range []string{
		`form[action="/flash/shared/adopt"] input[name=csrf_token]`,
		`form[action="/flash/shared/decline"] input[name=csrf_token]`,
	} {
		v, _ := htmlassert.Attr(doc.MustHave(sel), "value")
		if v == "" {
			t.Errorf("%s has an empty CSRF token", sel)
		}
	}
	doc.MustHave(`#deck-list a.deck-row-active`) // the gift row is highlighted
}

func TestGiftPreviewIsRecipientOnly(t *testing.T) {
	s := newShareServer(t)
	deckID := createDeckHX(t, s, s.Alice, "Spanish")
	shareID := shareToBob(t, s, deckID)

	if rec := s.Do(t, s.Alice, httpGet(t, "/flash/shared/"+shareID)); rec.Code != 404 {
		t.Errorf("sharer viewing the gift = %d, want 404", rec.Code)
	}
	s.PostHX(t, s.Bob, "/flash/shared/decline", url.Values{"share_id": {shareID}})
	if rec := s.Do(t, s.Bob, httpGet(t, "/flash/shared/"+shareID)); rec.Code != 404 {
		t.Errorf("gift after declining = %d, want 404", rec.Code)
	}
}

func TestGiftRowLeavesTheListOnAdoptAndDecline(t *testing.T) {
	s := newShareServer(t)
	deckID := createDeckHX(t, s, s.Alice, "Spanish")
	shareID := shareToBob(t, s, deckID)

	rec := s.PostHX(t, s.Bob, "/flash/shared/decline", url.Values{"share_id": {shareID}})
	doc := htmlassert.Parse(t, rec.Body.String())
	list := doc.MustHave("#deck-list")
	if _, ok := htmlassert.Attr(list, "hx-swap-oob"); !ok {
		t.Error("#deck-list is not refreshed out of band on decline")
	}
	doc.MustNotHave("#deck-list .deck-row-gift")

	shareID = shareToBob(t, s, deckID)
	rec = s.PostHX(t, s.Bob, "/flash/shared/adopt", url.Values{"share_id": {shareID}})
	doc = htmlassert.Parse(t, rec.Body.String())
	doc.MustNotHave("#deck-list .deck-row-gift")
	notice := doc.MustHave("#deck-detail-view .flash-notice")
	if !strings.Contains(htmlassert.Text(notice), "Spanish") {
		t.Errorf("adopt notice = %q, want the deck name", htmlassert.Text(notice))
	}
}
```

In `TestFullShareCycleThroughHTTP`, replace:

```go
	doc := s.Get(t, s.Bob, "/flash/")
	sharedText := htmlassert.Text(doc.MustHave("#shared-with-me"))
	if !strings.Contains(sharedText, "alice") || !strings.Contains(sharedText, "Spanish") {
		t.Fatalf("shared-with-me text = %q, want alice's username and the deck name", sharedText)
	}
```

with:

```go
	doc := s.Get(t, s.Bob, "/flash/")
	giftText := htmlassert.Text(doc.MustHave("#deck-list a.deck-row-gift"))
	if !strings.Contains(giftText, "alice") || !strings.Contains(giftText, "Spanish") {
		t.Fatalf("gift row text = %q, want alice's username and the deck name", giftText)
	}
```

In `internal/apps/flash/handlers_decks_test.go`, `TestDeckFragmentCarriesOutOfBandListAndToolbar`: remove `"#shared-with-me"` from the id list (the block no longer exists).

- [ ] **Step 2: Run to verify they fail**

Run: `go test ./internal/apps/flash/ -run 'Gift|FullShareCycle|OutOfBandListAndToolbar' -count=1`
Expected: FAIL.

- [ ] **Step 3: View model changes in `handlers_decks.go`**

Add the mode constant `deckModeGift = "gift"` to the mode block, and `case deckModeGift: return "Shared with you"` to `deckPageTitle`.

Add to `deckDetailView` (after `NextLabel`):

```go
	// Notice is a one-off success message at the top of the deck view
	// ("Planets is now in your decks").
	Notice string

	// Gift is set for Mode "gift": a pending share's preview.
	Gift giftView
```

Add the types:

```go
// giftRow is a pending share shown at the top of the deck list, like a
// present waiting to be opened (UI overhaul spec §6).
type giftRow struct {
	ShareID      int64
	DeckName     string
	DeckColor    string
	FromUsername string
	IsMerge      bool // the recipient already has this deck; only new cards come
	NewCardCount int
}

// giftView is the pane for one gift.
type giftView struct {
	ShareID      int64
	DeckName     string
	Description  string
	Color        string
	FromUsername string
	IsMerge      bool
	CardCount    int
	Samples      []cardGridItem
	CSRFToken    string
}
```

Add `Gifts []giftRow` and `ActiveGiftID int64` to `deckListFragment`.

In `buildDeckIndex`, after the `offers` are loaded and before the `return`, add:

```go
	gifts := make([]giftRow, len(offers))
	for i, o := range offers {
		gifts[i] = giftRow{
			ShareID: o.ID, DeckName: o.DeckName, DeckColor: o.DeckColor, FromUsername: o.FromUsername,
			IsMerge: o.PriorAdoptedDeckID != nil, NewCardCount: o.NewCardCount,
		}
	}
```

and set them on the list:

```go
		List: deckListFragment{Items: items, Gifts: gifts, ActiveID: detail.Deck.ID, ActiveGiftID: detail.Gift.ShareID, OOB: oob},
```

- [ ] **Step 4: The preview handler and the adopt notice in `handlers_share.go`**

Add:

```go
// giftPreview backs GET /shared/{shareID}: a pending share's preview in the
// deck pane. Anything but the recipient's own pending share is a 404.
func (a *App) giftPreview(w http.ResponseWriter, r *http.Request) {
	userID, ok := a.userID(w, r)
	if !ok {
		return
	}
	shareID, ok := a.shareIDFromPath(w, r)
	if !ok {
		return
	}
	p, err := a.store.SharePreview(r.Context(), userID, shareID)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	_, byID, err := a.usernamesByID(r.Context())
	if err != nil {
		a.deps.Errors.Internal(w, r, err)
		return
	}
	gift := giftView{
		ShareID: shareID, DeckName: p.Deck.Name, Description: p.Deck.Description, Color: p.Deck.Color,
		FromUsername: byID[p.Offer.FromUserID], IsMerge: p.Offer.PriorAdoptedDeckID != nil,
		CardCount: p.CardCount, CSRFToken: web.CSRFToken(r.Context()),
	}
	for _, c := range p.Samples {
		gift.Samples = append(gift.Samples, cardGridItem{Face: newCardFace(c, p.Deck, nil)})
	}
	a.renderDeckIndex(w, r, userID, http.StatusOK, deckDetailView{Mode: deckModeGift, Gift: gift})
}
```

In `adoptShareHandler`, look the offer up **before** adopting (to word the notice), and set the notice on the view. Replace its body after `shareIDFromForm` with:

```go
	wasMerge, newCards := false, 0
	if offers, err := a.store.SharesForRecipient(r.Context(), userID); err == nil {
		for _, o := range offers {
			if o.ID == shareID {
				wasMerge, newCards = o.PriorAdoptedDeckID != nil, o.NewCardCount
			}
		}
	}
	d, err := a.store.AdoptShare(r.Context(), userID, shareID)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	a.deps.Log.Info("share adopted", "app", ID, "user_id", userID, "share_id", shareID, "deck_id", d.ID)

	if !web.IsHTMX(r) {
		http.Redirect(w, r, "/flash/"+strconv.FormatInt(d.ID, 10), http.StatusSeeOther)
		return
	}
	w.Header().Set("HX-Push-Url", "/flash/"+strconv.FormatInt(d.ID, 10))
	recipients, shares, err := a.shareContext(r.Context(), userID, d.ID)
	if err != nil {
		a.deps.Errors.Internal(w, r, err)
		return
	}
	view := a.viewDeckDetail(r, userID, d, recipients, shares)
	view.Notice = "“" + d.Name + "” is now in your decks."
	if wasMerge {
		view.Notice = fmt.Sprintf("%d new card%s added to “%s”.", newCards, plural(newCards), d.Name)
	}
	a.renderDeckDetailWithList(w, r, userID, http.StatusOK, view)
```

and add the small helper (and `"fmt"` to the imports):

```go
// plural is "s" unless n is 1.
func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}
```

- [ ] **Step 5: Register the route**

In `internal/apps/flash/flash.go`, after `r.HandleFunc("POST /shared/decline", a.declineShareHandler)`:

```go
	// A gift's preview. Literal-then-wildcard GET, the same shape as GET
	// /edit/{deckID} and GET /review/{deckID}, differing in its literal, so
	// it can't collide with them; it is a GET, so it can't collide with the
	// POST /shared/* routes above either.
	r.HandleFunc("GET /shared/{shareID}", a.giftPreview)
```

- [ ] **Step 6: Templates**

**`card.partial.html`** — let a mini card be a plain tile when it has no `Href` (gift samples aren't links). Replace the `flash-mini-card` definition with:

```html
{{/* flash-mini-card is one grid tile: the question side only, plus a
     status corner label. Pass a cardGridItem. With an Href it is a link
     (InPane makes it an HTMX swap into the deck pane); without one it is
     a plain tile (a gift's sample cards). */}}
{{define "flash-mini-card"}}{{if .Href}}<a class="flash-mini-card deck-c-{{.Face.Color}}" href="{{.Href}}"{{if .InPane}} hx-get="{{.Href}}" hx-target="#deck-detail" hx-push-url="true"{{end}}>{{template "flash-mini-card-body" .}}</a>{{else}}<span class="flash-mini-card deck-c-{{.Face.Color}}">{{template "flash-mini-card-body" .}}</span>{{end}}{{end}}

{{define "flash-mini-card-body"}}
	<span class="flash-mini-text">{{.Face.Question}}</span>
	{{with .Status}}<span class="flash-mini-status">{{.}}</span>{{end}}
	{{with .DeckName}}<span class="flash-mini-deck">{{.}}</span>{{end}}
{{end}}
```

**Create `gift.partial.html`:**

```html
{{/* internal/apps/flash/templates/gift.partial.html — a deck shared with
     you, before you add it (UI overhaul spec §6). */}}
{{define "deck-detail-gift"}}
{{with .Gift}}
<div class="flash-gift deck-c-{{.Color}}" id="deck-detail-gift">
	<div class="notes-toolbar flash-deck-toolbar">{{template "flash-pane-back"}}</div>
	<div class="flash-deck-hero">
		{{template "flash-stack" (dict "Size" "lg")}}
		<div class="flash-deck-hero-text">
			<p class="flash-gift-from">{{if .IsMerge}}{{.FromUsername}} added {{.CardCount}} new card{{if ne .CardCount 1}}s{{end}} to{{else}}{{.FromUsername}} shared this deck with you{{end}}</p>
			<h1>{{.DeckName}}</h1>
			{{with .Description}}<p class="dim">{{.}}</p>{{end}}
			{{if not .IsMerge}}<p class="faint">{{.CardCount}} card{{if ne .CardCount 1}}s{{end}}</p>{{end}}
			<div class="flash-gift-actions">
				<form method="post" action="/flash/shared/adopt" hx-post="/flash/shared/adopt" hx-target="#deck-detail" hx-swap="innerHTML">
					<input type="hidden" name="{{csrfField}}" value="{{.CSRFToken}}">
					<input type="hidden" name="share_id" value="{{.ShareID}}">
					<button type="submit" class="flash-big-btn flash-big-btn-primary">{{ticon "plus"}}{{if .IsMerge}}Add the new cards{{else}}Add to my decks{{end}}</button>
				</form>
				<form method="post" action="/flash/shared/decline" hx-post="/flash/shared/decline" hx-target="#deck-detail" hx-swap="innerHTML">
					<input type="hidden" name="{{csrfField}}" value="{{.CSRFToken}}">
					<input type="hidden" name="share_id" value="{{.ShareID}}">
					<button type="submit" class="flash-big-btn">No thanks</button>
				</form>
			</div>
		</div>
	</div>
	{{if .Samples}}
	<h2 class="flash-section-title">{{if .IsMerge}}The new cards{{else}}A few of the cards{{end}}</h2>
	<div class="flash-card-grid">{{range .Samples}}{{template "flash-mini-card" .}}{{end}}</div>
	{{end}}
</div>
{{end}}
{{end}}
```

**`decks.html`:**

1. In `deck-detail-body`, add `{{else if eq .Mode "gift"}}{{template "deck-detail-gift" .}}` before the final `{{else}}`.
2. In `deck-detail-with-list`, delete the `<div id="shared-with-me" … hx-swap-oob="true">…</div>` part.
3. In `content`, delete the `<div id="shared-with-me" class="flash-shared-with-me">…</div>` line.
4. Delete the whole `shared-with-me` definition and its comment.
5. In `deck-list-items`, right after `<ul …>`, add the gift rows before `{{range .Items}}`:

```html
	{{range .Gifts}}
	<li>
		<a class="deck-row deck-row-gift deck-c-{{.DeckColor}}{{if eq .ShareID $.ActiveGiftID}} deck-row-active{{end}}" href="/flash/shared/{{.ShareID}}" hx-get="/flash/shared/{{.ShareID}}" hx-target="#deck-detail" hx-push-url="true">
			{{template "flash-stack" (dict "Size" "sm")}}
			<span class="deck-row-text">
				<span class="deck-row-name">{{.DeckName}}</span>
				<span class="deck-row-status">From {{.FromUsername}}</span>
			</span>
			<span class="flash-gift-badge">{{if .IsMerge}}+{{.NewCardCount}}{{else}}new{{end}}</span>
		</a>
	</li>
	{{end}}
```

6. In `deck-detail-view`, directly after the `{{template "deck-toolbar" …}}` line, add:

```html
	{{with .Notice}}<div class="notice flash-notice" role="status">{{.}}</div>{{end}}
```

> The deck `view` pane's `ActiveID` is the adopted deck after adopting, so
> the adopted deck's row is highlighted; `ActiveGiftID` is 0 outside gift
> mode, and share ids and deck ids never meet in one comparison.

- [ ] **Step 7: Run all Flash tests**

Run: `go test ./internal/apps/flash/ -count=1`
Expected: PASS.

- [ ] **Step 8: Commit**

```bash
git add -A internal/apps/flash
git commit -m "feat(flash): shared decks arrive as gift decks with a preview"
```

---

### Task 4: Share popover and "Take a break"

**Files:**
- Modify: `internal/apps/flash/handlers_decks.go`, `internal/apps/flash/handlers_share.go`, `internal/apps/flash/templates/cards.partial.html`, `internal/apps/flash/templates/decks.html`
- Test: `internal/apps/flash/handlers_share_test.go`, `internal/apps/flash/handlers_decks_test.go`

**Interfaces:**
- Produces: `deckDetailView.ShareOpen bool`, `deckDetailView.Snoozed bool` (edit mode); `shareWithUsername.StatusLabel string`; `deck-toolbar` takes a fourth dict key `View` (the `deckDetailView`).

- [ ] **Step 1: Write the failing tests**

Append to `internal/apps/flash/handlers_share_test.go`:

```go
func TestShareMenuListsSharesWithFriendlyStatus(t *testing.T) {
	s := newShareServer(t)
	deckID := createDeckHX(t, s, s.Alice, "Spanish")
	rec := s.PostHX(t, s.Alice, "/flash/"+strconv.FormatInt(deckID, 10)+"/share",
		url.Values{"to_user_id": {strconv.FormatInt(s.Bob.User.ID, 10)}})
	doc := htmlassert.Parse(t, rec.Body.String())
	menu := doc.MustHave("details.flash-share-menu")
	if _, open := htmlassert.Attr(menu, "open"); !open {
		t.Error("the Share popover should stay open after sharing")
	}
	pill := doc.MustHave(".flash-share-menu .flash-status-pill")
	if got := htmlassert.Text(pill); got != "waiting" {
		t.Errorf("status pill = %q, want waiting", got)
	}

	page := s.Get(t, s.Alice, "/flash/"+strconv.FormatInt(deckID, 10))
	closed := page.MustHave("details.flash-share-menu")
	if _, open := htmlassert.Attr(closed, "open"); open {
		t.Error("the Share popover should start closed on a normal page load")
	}
}

func TestCardsModeToolbarAlsoHasShare(t *testing.T) {
	s := newShareServer(t)
	deckID := createDeckHX(t, s, s.Alice, "Spanish")
	doc := s.Get(t, s.Alice, "/flash/"+strconv.FormatInt(deckID, 10)+"/cards/")
	doc.MustHave(".flash-deck-toolbar details.flash-share-menu")
}
```

Append to `internal/apps/flash/handlers_decks_test.go`:

```go
func TestTakeABreakLivesInTheEditPane(t *testing.T) {
	s := newServer(t)
	deck, err := s.Store.CreateDeck(t.Context(), s.Alice.User.ID, "Spanish", "")
	if err != nil {
		t.Fatal(err)
	}
	view := s.Get(t, s.Alice, "/flash/"+itoa(deck.ID))
	view.MustNotHave(`form[action="/flash/` + itoa(deck.ID) + `/snooze"]`)

	edit := s.Get(t, s.Alice, "/flash/edit/"+itoa(deck.ID))
	if n := len(edit.QueryAll(`form[action="/flash/` + itoa(deck.ID) + `/snooze"]`)); n != 2 {
		t.Errorf("edit pane has %d snooze forms, want 2 (1 week, 1 month)", n)
	}
}

func TestSnoozedDeckShowsABreakBanner(t *testing.T) {
	s := newServer(t)
	deck, err := s.Store.CreateDeck(t.Context(), s.Alice.User.ID, "Spanish", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Store.CreateCard(t.Context(), s.Alice.User.ID, deck.ID, flash.CardTypeBasic, "hola", "hello", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Store.SnoozeDeck(t.Context(), s.Alice.User.ID, deck.ID, time.Now().Add(72*time.Hour)); err != nil {
		t.Fatal(err)
	}
	doc := s.Get(t, s.Alice, "/flash/"+itoa(deck.ID))
	banner := doc.MustHave(".flash-break-banner")
	if !strings.Contains(htmlassert.Text(banner), "Taking a break until") {
		t.Errorf("banner = %q", htmlassert.Text(banner))
	}
	doc.MustHave(`.flash-break-banner form[action="/flash/` + itoa(deck.ID) + `/unsnooze"]`)
	doc.MustNotHave("a.flash-review-cta")

	edit := s.Get(t, s.Alice, "/flash/edit/"+itoa(deck.ID))
	edit.MustHave(`form[action="/flash/` + itoa(deck.ID) + `/unsnooze"]`)
}
```

- [ ] **Step 2: Run to verify they fail**

Run: `go test ./internal/apps/flash/ -run 'ShareMenu|AlsoHasShare|TakeABreak|BreakBanner' -count=1`
Expected: FAIL.

- [ ] **Step 3: Go changes**

In `handlers_decks.go`:

- Add to `deckDetailView` (next to `Notice`):
  ```go
	ShareOpen bool // render the Share popover open (after a share or revoke)
	Snoozed   bool // edit mode: whether "Take a break" shows End break
  ```
- Add `StatusLabel string` to `shareWithUsername`, and a label function:
  ```go
  // shareStatusLabel is how a share's status reads in the Share popover.
  func shareStatusLabel(status string) string {
  	switch status {
  	case ShareStatusPending:
  		return "waiting"
  	case ShareStatusAdopted:
  		return "added"
  	case ShareStatusDeclined:
  		return "said no thanks"
  	default:
  		return status
  	}
  }
  ```
  and in `shareContext` build rows with it: `withNames[i] = shareWithUsername{Share: sh, ToUsername: byID[sh.ToUserID], StatusLabel: shareStatusLabel(sh.Status)}`.
- In `editDeckDetail`, set `Snoozed: d.IsSnoozed(a.store.now())` on the returned view.
- In `buildDeckIndex`, before building `items`, fill the share context for the two modes whose toolbar shows Share, when the caller didn't:
  ```go
	if (detail.Mode == deckModeView || detail.Mode == deckModeCards) && detail.Deck.ID != 0 && detail.ShareRecipients == nil {
		recipients, shares, err := a.shareContext(ctx, userID, detail.Deck.ID)
		if err != nil {
			return deckIndexView{}, err
		}
		detail.ShareRecipients, detail.SharedWith = recipients, shares
	}
  ```

In `handlers_share.go`, in both `shareDeck` and `revokeShareHandler`, open the popover on the re-rendered view — replace their last line with:

```go
	view := a.viewDeckDetail(r, userID, d, recipients, shares)
	view.ShareOpen = true
	a.renderDeckDetailWithList(w, r, userID, http.StatusOK, view)
```

- [ ] **Step 4: Template changes**

**`cards.partial.html`** — replace the `deck-toolbar` definition (and its comment) with:

```html
{{/* deck-toolbar is the deck pane's toolbar, shared by the deck view and
     cards mode. Pass a dict: Deck, Active ("cards" highlights Cards),
     CSRFToken, and View (the deckDetailView, for the Share popover). The
     popover is a <details>, the same no-JS disclosure as Notes' shortcuts
     menu; it is left out when there is nobody to share with. */}}
{{define "deck-toolbar"}}
<div class="notes-toolbar flash-deck-toolbar">
	{{template "flash-pane-back"}}
	<div class="notes-toolbar-actions">
		<a class="toolbar-btn{{if eq .Active "cards"}} toolbar-btn-active{{end}}" href="/flash/{{.Deck.ID}}/cards/" hx-get="/flash/{{.Deck.ID}}/cards/" hx-target="#deck-detail" hx-push-url="true">{{ticon "cards"}}Cards</a>
		<a class="toolbar-btn" href="/flash/edit/{{.Deck.ID}}" hx-get="/flash/edit/{{.Deck.ID}}" hx-target="#deck-detail" hx-push-url="true">{{ticon "edit"}}Edit</a>
		{{with .View}}{{if .ShareRecipients}}
		<details class="flash-share-menu"{{if .ShareOpen}} open{{end}}>
			<summary class="toolbar-btn">{{ticon "share"}}Share</summary>
			<div class="flash-share-panel">
				<form class="flash-share-form" method="post" action="/flash/{{.Deck.ID}}/share" hx-post="/flash/{{.Deck.ID}}/share" hx-target="#deck-detail" hx-swap="innerHTML">
					<input type="hidden" name="{{csrfField}}" value="{{.CSRFToken}}">
					<label for="share-recipient-{{.Deck.ID}}">Share this deck with</label>
					<div class="row">
						<select id="share-recipient-{{.Deck.ID}}" name="to_user_id">
							{{range .ShareRecipients}}<option value="{{.ID}}">{{.Username}}</option>{{end}}
						</select>
						<button type="submit" class="primary">Share</button>
					</div>
				</form>
				{{if .SharedWith}}
				<ul class="flash-share-list">
					{{range .SharedWith}}{{if ne .Status "revoked"}}
					<li>
						<span>{{.ToUsername}}</span>
						<span class="flash-status-pill flash-status-{{.Status}}">{{.StatusLabel}}</span>
						{{if eq .Status "pending"}}
						<form method="post" action="/flash/{{$.View.Deck.ID}}/share/{{.ID}}/revoke" hx-post="/flash/{{$.View.Deck.ID}}/share/{{.ID}}/revoke" hx-target="#deck-detail" hx-swap="innerHTML">
							<input type="hidden" name="{{csrfField}}" value="{{$.View.CSRFToken}}">
							<button type="submit" class="toolbar-btn">Revoke</button>
						</form>
						{{end}}
					</li>
					{{end}}{{end}}
				</ul>
				{{end}}
			</div>
		</details>
		{{end}}{{end}}
		<form method="post" action="/flash/{{.Deck.ID}}/delete" hx-post="/flash/{{.Deck.ID}}/delete" hx-target="#deck-detail" hx-swap="innerHTML"
		      data-confirm="Delete “{{.Deck.Name}}” and all its cards? This can't be undone.">
			<input type="hidden" name="{{csrfField}}" value="{{.CSRFToken}}">
			<button type="submit" class="toolbar-btn danger">{{ticon "trash"}}Delete</button>
		</form>
	</div>
</div>
{{end}}
```

Update both callers to pass `View`:
- `cards.partial.html`, `deck-detail-cards`: `{{template "deck-toolbar" (dict "Deck" .Deck "Active" "cards" "CSRFToken" .CSRFToken "View" .)}}`
- `decks.html`, `deck-detail-view`: `{{template "deck-toolbar" (dict "Deck" .Deck "Active" "" "CSRFToken" .CSRFToken "View" .)}}`

**`decks.html`, `deck-detail-view`:**

1. Replace the `{{if .Summary.Snoozed}} … notice row … {{else if .Summary.ReviewNow}}` opening branch with the banner:

```html
			{{if .Summary.Snoozed}}
			<div class="flash-break-banner" role="status">
				<span>Taking a break until {{.Deck.SnoozedUntil.Format "2 Jan"}}.</span>
				<form method="post" action="/flash/{{.Deck.ID}}/unsnooze" hx-post="/flash/{{.Deck.ID}}/unsnooze" hx-target="#deck-detail" hx-swap="innerHTML">
					<input type="hidden" name="{{csrfField}}" value="{{.CSRFToken}}">
					<button type="submit" class="flash-big-btn">End break</button>
				</form>
			</div>
			{{else if .Summary.ReviewNow}}
```

2. Delete the old Share `<section>` (`{{if .ShareRecipients}} <section class="flash-deck-section"> … {{end}}`) and the "Take a break" `<section>` (`{{if not .Summary.Snoozed}} … {{end}}`) and the comment above them.

**`decks.html`, `deck-detail-edit`:** wrap the existing `<form … id="deck-detail-edit">…</form>` in `<div class="flash-deck-edit">…</div>`, and add after the form, inside the div:

```html
	<section class="flash-deck-section flash-break-section">
		<h2 class="flash-section-title">Take a break</h2>
		{{if .Snoozed}}
		<div class="flash-break-banner">
			<span>Taking a break until {{.Deck.SnoozedUntil.Format "2 Jan"}}.</span>
			<form method="post" action="/flash/{{.Deck.ID}}/unsnooze" hx-post="/flash/{{.Deck.ID}}/unsnooze" hx-target="#deck-detail" hx-swap="innerHTML">
				<input type="hidden" name="{{csrfField}}" value="{{.CSRFToken}}">
				<button type="submit" class="flash-big-btn">End break</button>
			</form>
		</div>
		{{else}}
		<p class="faint">Hide this deck from reviews for a while — handy for holidays. Nothing is lost.</p>
		<div class="row">
			<form method="post" action="/flash/{{.Deck.ID}}/snooze" hx-post="/flash/{{.Deck.ID}}/snooze" hx-target="#deck-detail" hx-swap="innerHTML">
				<input type="hidden" name="{{csrfField}}" value="{{.CSRFToken}}">
				<input type="hidden" name="days" value="7">
				<button type="submit" class="button">1 week</button>
			</form>
			<form method="post" action="/flash/{{.Deck.ID}}/snooze" hx-post="/flash/{{.Deck.ID}}/snooze" hx-target="#deck-detail" hx-swap="innerHTML">
				<input type="hidden" name="{{csrfField}}" value="{{.CSRFToken}}">
				<input type="hidden" name="days" value="30">
				<button type="submit" class="button">1 month</button>
			</form>
		</div>
		{{end}}
	</section>
```

- [ ] **Step 5: Run all Flash tests**

Run: `go test ./internal/apps/flash/ -count=1`
Expected: PASS (including `TestSnoozeAndUnsnoozeDeckOverHTTP`, which posts to the unchanged routes).

- [ ] **Step 6: Commit**

```bash
git add -A internal/apps/flash
git commit -m "feat(flash): Share popover, and Take a break moves into Edit deck"
```

---

### Task 5: CSS

**Files:** `internal/ui/static/app.css`

- [ ] **Step 1:** Delete U1's now-unused `.flash-shared-with-me:not(:empty)` rule, then append:

```css
/* ---- ON Flash: sharing and breaks (UI overhaul U5) ----------------------- */

/* Gift decks: a dashed, see-through stack, like a present not yet opened. */
.deck-row-gift { border: 1px dashed var(--c-border-firm); }
.deck-row-gift .flash-stack > span {
	background: color-mix(in srgb, var(--deck) 18%, var(--c-bg));
	border-style: dashed;
	border-color: var(--deck);
}
.flash-gift .flash-stack > span { border-style: dashed; border-color: var(--deck); }
.flash-gift-badge {
	padding: 0 var(--s-2);
	border-radius: 999px;
	background: var(--deck-soft);
	color: var(--c-text);
	font-size: var(--fs-xs);
	font-weight: 600;
}
.flash-gift-from { margin: 0 0 var(--s-1); color: var(--c-text-dim); font-size: var(--fs-sm); }
.flash-gift-actions { display: flex; flex-wrap: wrap; gap: var(--s-3); margin-top: var(--s-4); }
.flash-gift-actions form { margin: 0; }
.flash-gift .flash-card-grid { margin-top: var(--s-2); }

/* Share popover. */
.flash-share-menu { position: relative; }
.flash-share-menu > summary { list-style: none; }
.flash-share-menu > summary::-webkit-details-marker { display: none; }
.flash-share-menu[open] > summary { color: var(--c-accent); background: var(--c-accent-bg); }
.flash-share-panel {
	position: absolute;
	right: 0;
	top: calc(100% + var(--s-1));
	z-index: 5;
	width: min(20rem, 80vw);
	padding: var(--s-3);
	border: 1px solid var(--c-border);
	border-radius: 10px;
	background: var(--c-bg);
	box-shadow: 0 8px 24px rgb(0 0 0 / 0.12);
}
.flash-share-form { margin: 0; }
.flash-share-form .row { align-items: stretch; }
.flash-share-form select { flex: 1; }
.flash-share-panel .flash-share-list li { justify-content: flex-start; }
.flash-share-panel .flash-share-list form { margin-left: auto; }
.flash-status-pill {
	padding: 0 var(--s-2);
	border-radius: 999px;
	background: var(--c-bg-subtle);
	color: var(--c-text-dim);
	font-size: var(--fs-xs);
}
.flash-status-adopted { background: var(--c-accent-bg); color: var(--c-accent); }

/* Taking a break: calm, not a warning. */
.flash-break-banner {
	display: flex;
	flex-wrap: wrap;
	align-items: center;
	gap: var(--s-3);
	margin-top: var(--s-4);
	padding: var(--s-3) var(--s-4);
	border-radius: 10px;
	background: var(--deck-soft, var(--c-bg-subtle));
}
.flash-break-banner form { margin: 0; }

.flash-notice { margin-bottom: var(--s-4); background: var(--c-accent-bg); color: var(--c-accent); }
```

- [ ] **Step 2:** Full check; commit `style(flash): gift decks, share popover and break banner`.

---

### Task 6: Manual verification

- [ ] You need two accounts. Ask the user to create a second one (`/tmp/onsuite-bin user add kid --data-dir /tmp/onsuite-manual-verify`) and sign in as each in turn — never type passwords yourself.
  1. As the first account, open a coloured deck → Share → pick the other account → Share: the popover stays open showing "waiting".
  2. As the second account: a dashed gift row "From …" at the top of the list with a "new" badge; clicking it shows the preview with sample cards; "Add to my decks" adds it (notice appears, deck keeps the sharer's colour); the gift row is gone.
  3. First account adds two cards and shares again; second account sees "+2" and "… added 2 new cards to"; "Add the new cards" merges.
  4. "No thanks" on a gift removes it.
  5. Edit deck → Take a break → 1 week: the deck pane shows the calm banner and no Review button; the list row is dimmed; End break restores it.
  6. Dark theme, tablet width (popover fits on screen), no console errors.
- [ ] Fix, full check, commit.

### Task 7: Open the PR

```bash
git push -u origin feat/flash-ui-u5
gh pr create --title "feat(flash): UI overhaul U5 — gift decks, Share popover, Take a break" --body "$(cat <<'EOF'
## Summary
- Decks shared with you appear as dashed "gift" decks at the top of the list, with a preview pane (sender, description, sample cards) and Add / No thanks. Merges read "… added N new cards".
- Recipient-only `SharePreview` store method (pending + addressed to you, else 404).
- Sharing moves into a Share popover in the deck toolbar, with friendly statuses.
- Snooze moves into Edit deck as "Take a break"; a snoozed deck shows a calm banner with End break.

Spec §6. Plan: docs/superpowers/plans/2026-09-23-flash-ui-u5-sharing.md.

## Test plan
- [ ] `go test ./... -race -count=1`
- [ ] Manual two-account flow from the plan's Task 6 (screenshots attached)
EOF
)"
```
