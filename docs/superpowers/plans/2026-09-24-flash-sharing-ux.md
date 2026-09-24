# ON Flash — Sharing UX follow-ups Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Three rough edges in sharing. The creator's "Shared with" list shows one row per person with where their latest offer stands. Adopting a deck whose name the recipient already uses always works, and the copy gets a "(from alice)" suffix. A re-share with nothing new reads as "up to date" rather than "+0 / added 0 new cards".

**Architecture:** `SharesForDeck` becomes the list query itself. It uses a `ROW_NUMBER()` window over non-revoked rows, partitioned by recipient and ordered `created_at DESC, id DESC`, and keeps row 1 per recipient. The template's `revoked` filter goes away. `AdoptShare` gains a `usernames map[int64]string` parameter. On a first-time adoption, a new `freeDeckName` helper checks names inside the adoption transaction and picks the first free one of `Spanish`, `Spanish (from alice)`, `Spanish (from alice) (2)`, and so on. The pure helper `adoptedDeckName` builds each candidate and shortens the base so the result fits in `MaxDeckNameRunes`. `giftRow` and `giftView` gain `UpToDate` (a merge with 0 new cards). The templates drop the badge and show a "Got it"-only pane in that case, and the adopt handler words the notice "“Y” is already up to date."

**Tech Stack:** Go, SQLite (`modernc.org/sqlite`, window functions), `html/template`, HTMX.

**Issues:** [#304](https://github.com/iliafrenkel/on-suite/issues/304) bullets 2 and 5 (1, 3, 4, 6, 8 landed in PR #358; 7 is obsolete per the 2026-09-24 triage comment), [#346](https://github.com/iliafrenkel/on-suite/issues/346). Spec: [UI overhaul §6](../specs/2026-09-23-on-flash-ui-overhaul-design.md).

**Decisions already made (by the user):**
1. **#304.5 name collision → auto-suffix.** A first-time adopt of a deck whose name the recipient already uses gets "Spanish (from alice)". If that's taken too, it's "Spanish (from alice) (2)", then "(3)", and so on. It always succeeds. The merge path is unaffected. The name must pass `ValidateDeck`, and the notice uses the final name.
2. **#346 re-share with 0 new cards:** no "+0" badge, a pane that says "You already have every card in *Y*" with a single **Got it** that adopts through the existing route, and an "already up to date" notice. Also add a test for a first-time share preview of a 0-card deck.
3. **#304.2 "Shared with":** one row per recipient with the latest status. A re-share replaces the row with "waiting". Revoked stays hidden. "Latest" is deterministic (`created_at DESC, id DESC`), done in SQL, and Revoke still targets the pending share's id.
4. Spec §6 records all three rules.

**Choices this plan makes (flag in the PR, easy to change):**
- **Where the sharer's name comes from.** The handler passes `usernames map[int64]string` (from `usernamesByID`, which it already has) into `AdoptShare`. Rejected alternatives:
  - A lookup *callback* would run inside `AdoptShare`'s transaction. With `SetMaxOpenConns(1)` (`internal/platform/db/db.go:40`), anything it did against the database, `ListAccounts` for example, would wait forever for the one connection the transaction holds.
  - Passing a single `sharerName string` means the handler has to learn the share's `from_user_id` before the call, which needs a second store read.
  - Having the flash store read `users.username` itself would put SQL against an auth-owned table in an app.

  A missing entry (or a `nil` map, as the existing store tests pass) falls back to "Spanish (shared)". The cost is one `ListAccounts` per adopt click, which is rare.
- **Gift row for an up-to-date re-share:** **no badge at all**. The row keeps its gift styling and "From alice", and the pane explains.
- **Copy:**
  - Pane: `You already have every card in` + the deck name as the `<h1>`, the same "sentence runs into the heading" shape the merge pane already uses. It has one button, **Got it**, with no plus icon because nothing is added, and no **No thanks**.
  - Notice after Got it: `“Spanish” is already up to date.`
  - Notice after a renamed adopt: the existing sentence with the final name, `“Spanish (from alice)” is now in your decks.`, with no extra "renamed because…" clause. The suffix explains itself.
- **Shared-with rule:** revoked rows are ignored. Each recipient shows the status of their latest **non-revoked** share, so revoking a re-offer makes the row fall back to the person's last real answer ("added" / "said no thanks"). A recipient whose every row is revoked isn't listed. While a pending row exists it is always that recipient's latest row, because `ShareDeck` is idempotent while pending. So the row's `ID` *is* the pending share's id, and Revoke keeps targeting it.

## Verified against current code (branch `fix/304-sharing-ux` @ `7766bb3`)

| What | Where |
|---|---|
| adopt inserts `src.Name` verbatim; unique violation → `ErrInvalid` "you already have a deck named %q" | `internal/apps/flash/share.go:233-249` (`:237-242` INSERT, `:244-246` error) |
| `AdoptResult{Deck, Merged, CardsCopied}`; `AdoptShare(ctx, toUserID, shareID)` | `share.go:180-191`, `:202` |
| merge target found by id via `priorAdoptedDeck` (name-independent) | `share.go:227-235`, `:410-434` |
| `ErrInvalid` → bare 400 (HTMX doesn't swap 4xx: the click "does nothing") | `internal/apps/flash/handlers_decks.go:42-51` |
| unique index `(user_id, name)`, BINARY collation (exact match) | `internal/apps/flash/migrations/0001_decks.sql:15-16` |
| `MaxDeckNameRunes = 120`; `ValidateDeck` counts runes | `internal/apps/flash/deck.go:15`, `:79-97` (`:84`) |
| usernames are 3-32 ASCII chars, so the longest suffix is ` (from <32>) (NNN)` ≈ 46 runes | `internal/platform/auth/store.go:41` |
| `SharesForDeck` returns every row, any status, `created_at DESC, id DESC` | `share.go:436-462` |
| `SharesForDeck` callers: `shareContext`, `buildDeckIndex` (+ tests) | `handlers_decks.go:328`, `:442`; `share_test.go:130,564,572`; `handlers_share_test.go:71,175,202` |
| template hides `revoked` rows; Revoke form posts `/flash/{deck}/share/{.ID}/revoke` for `pending` | `internal/apps/flash/templates/cards.partial.html:29-44` (`:31`, `:35-40`) |
| status labels waiting / added / said no thanks | `handlers_decks.go:243-255` |
| `ShareDeck` returns the existing row while one is pending (at most one pending per triple) | `share.go:42-69` |
| `giftRow` / `giftView` structs | `handlers_decks.go:196-203`, `:206-216` |
| gift rows built in `buildDeckIndex` | `handlers_decks.go:482-488` |
| gift row badge `+N` / `new` | `internal/apps/flash/templates/decks.html:219` |
| gift pane copy, buttons, samples heading | `internal/apps/flash/templates/gift.partial.html:10`, `:13`, `:14-25`, `:28-31` |
| `giftPreview` builds `giftView` | `internal/apps/flash/handlers_share.go:164-192` (`:183-187`) |
| `adoptShareHandler` notice ("is now in your decks" / "%d new card%s added to") | `handlers_share.go:204-237` (`:232-235`) |
| `SharesForRecipient.NewCardCount`; `SharePreview.CardCount` = new cards for a merge | `share.go:480-553`, `:592-644` |
| existing notice test (must keep passing) | `internal/apps/flash/handlers_share_test.go` `TestAdoptNoticeWordingForFirstAdoptAndMerge` |
| handler harness: app builds its **own** store, so `s.Store.SetClock` doesn't reach handlers | `internal/apps/flash/flash.go:76`; `internal/apptest/apptest.go:117-194` (only alice + bob) |
| `AdoptShare(` call sites in tests: 17, all `AdoptShare(ctx, X, Y)` | `internal/apps/flash/share_test.go` |
| SQLite window function `ROW_NUMBER() OVER (PARTITION BY …)` works (checked with sqlite3 3.54) | — |

## Global Constraints

- Branch: `fix/304-sharing-ux` (already created off `main` at `7766bb3`). Never push to `main`. Open a PR at the end.
- Full check before each Go commit:
  ```bash
  gofmt -l .
  go vet ./...
  go run honnef.co/go/tools/cmd/staticcheck@v0.8.0 ./...
  go mod tidy && git diff --exit-code go.mod go.sum
  go test ./... -race -count=1
  ```
- **HTMX behaviour otherwise unchanged.** Routes (`POST /flash/shared/adopt`, `/flash/shared/decline`, `/flash/{deckID}/share`, `/flash/{deckID}/share/{shareID}/revoke`, `GET /flash/shared/{shareID}`), form fields (`share_id`, `to_user_id`), targets (`#deck-detail`, `innerHTML`), `HX-Push-Url`s, OOB swaps and no-JS 303s all stay as they are.
- **No migrations.** Everything is query and template logic. The unique index stays as the backstop.
- No new dependencies. CSP: no inline `<script>`, no `style=`. No CSS changes are needed, since every class used already exists.
- `htmlassert` selectors: descendant chains of simple parts only (`tag`, `.class`, `#id`, `tag[attr=value]`; `tag.class` single parts are fine). No compound parts like `a.x[y]`.
- **Handler tests never rely on `s.Store.SetClock`.** `flash.go:76` builds the app's own store, so the clock never reaches it. Use real time. Store tests (`newFixture`) may use `f.store.SetClock`.
- Keep names: `SharesForDeck`, `AdoptShare`, `AdoptResult`, `giftRow`, `giftView`, `shareWithUsername`, `shareStatusLabel`, `usernamesByID`, `shareContext`, `buildDeckIndex`; templates `deck-toolbar`, `deck-list-items`, `deck-detail-gift`; CSS `.flash-share-list`, `.flash-status-pill`, `.flash-gift-badge`, `.flash-gift-from`, `.flash-gift-actions`, `.flash-notice`.
- Test helpers (real): `newShareServer(t)`, `createDeckHX(t, s, sess, name) int64`, `shareToBob(t, s, deckID) string` (share id), `s.Get`, `s.PostHX`, `s.Submit`, `s.Store`, `htmlassert.Parse/Text/Attr`, `doc.MustHave/MustNotHave/QueryAll`, `itoa`; store tests `newFixture(t)` (`f.store`, `f.db`, `f.alice`, `f.bob`).
- Commits: Conventional Commits, scope `flash`, issue in the subject. Polish on shipped features is `fix`/`refactor`, not `feat`.

---

## File map

| File | Change |
|---|---|
| `internal/apps/flash/share.go` | `SharesForDeck` → one row per recipient (latest non-revoked); `AdoptShare(…, usernames map[int64]string)`; `adoptedDeckName`, `freeDeckName` |
| `internal/apps/flash/handlers_share.go` | `adoptShareHandler` passes usernames, words the up-to-date notice; `giftPreview` sets `UpToDate` |
| `internal/apps/flash/handlers_decks.go` | `giftRow.UpToDate`, `giftView.UpToDate`; `buildDeckIndex` sets it; doc comments for the Shared-with list |
| `internal/apps/flash/templates/cards.partial.html` | drop the `revoked` filter (SQL does it) |
| `internal/apps/flash/templates/decks.html` | no badge when `UpToDate` |
| `internal/apps/flash/templates/gift.partial.html` | up-to-date copy + "Got it" only |
| `docs/superpowers/specs/2026-09-23-on-flash-ui-overhaul-design.md` | §6 rules |
| Tests | `share_test.go`, `share_name_internal_test.go` (new, `package flash`), `handlers_share_test.go` |

---

### Task 1: "Shared with" shows one row per recipient (#304.2)

**Files:**
- Modify: `internal/apps/flash/share.go`, `internal/apps/flash/templates/cards.partial.html`, `internal/apps/flash/handlers_decks.go` (doc comments only)
- Test: `internal/apps/flash/share_test.go`, `internal/apps/flash/handlers_share_test.go`

**Interfaces:**
- Consumes: `ShareDeck`, `DeclineShare`, `RevokeShare`, `AdoptShare` (3-arg form in this task), `scanShare`.
- Produces: `func (st *Store) SharesForDeck(ctx context.Context, fromUserID, deckID int64) ([]Share, error)`. The signature is unchanged. It now returns at most one `Share` per `ToUserID`: that recipient's latest non-revoked row, where latest means `created_at DESC, id DESC`. The list is ordered by those rows, also `created_at DESC, id DESC`. It still returns `ErrNotFound` for a deck that isn't `fromUserID`'s.

- [ ] **Step 1: Write the failing store test**

In `internal/apps/flash/share_test.go`, add to the import block:

```go
	"github.com/iliafrenkel/on-suite/internal/apptest"
	"github.com/iliafrenkel/on-suite/internal/platform/auth"
```

Replace the whole `TestSharesForDeckListsAllOffersNewestFirst` function with:

```go
// TestSharesForDeckShowsEachRecipientsLatestStatus covers #304 bullet 2:
// the creator's "Shared with" list has one row per recipient — their
// latest share that wasn't revoked — newest first. A revoke falls back to
// the person's last real answer; someone whose only offers were revoked
// isn't listed at all.
func TestSharesForDeckShowsEachRecipientsLatestStatus(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	// One fixed clock: every row gets the same created_at, so "latest" and
	// the list order below come from the id tiebreak alone.
	f.store.SetClock(func() time.Time { return time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC) })
	carol, err := auth.NewStore(f.db).CreateUser(ctx, "carol", apptest.PasswordHash, false)
	if err != nil {
		t.Fatal(err)
	}
	d, err := f.store.CreateDeck(ctx, f.alice.ID, "Spanish", "")
	if err != nil {
		t.Fatal(err)
	}

	// Bob says no thanks, then gets offered again.
	first, err := f.store.ShareDeck(ctx, f.alice.ID, d.ID, f.bob.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.store.DeclineShare(ctx, f.bob.ID, first.ID); err != nil {
		t.Fatal(err)
	}
	again, err := f.store.ShareDeck(ctx, f.alice.ID, d.ID, f.bob.ID)
	if err != nil {
		t.Fatal(err)
	}
	// Carol's only offer is revoked.
	c1, err := f.store.ShareDeck(ctx, f.alice.ID, d.ID, carol.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.store.RevokeShare(ctx, f.alice.ID, d.ID, c1.ID); err != nil {
		t.Fatal(err)
	}

	shares, err := f.store.SharesForDeck(ctx, f.alice.ID, d.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(shares) != 1 || shares[0].ID != again.ID || shares[0].Status != flash.ShareStatusPending {
		t.Fatalf("shares = %+v, want just bob's re-offer (id %d), pending — his decline replaced, carol's revoked offer hidden", shares, again.ID)
	}

	// Carol adopts a fresh offer; bob's re-offer is revoked, so he falls
	// back to his last real answer.
	c2, err := f.store.ShareDeck(ctx, f.alice.ID, d.ID, carol.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.AdoptShare(ctx, carol.ID, c2.ID); err != nil {
		t.Fatal(err)
	}
	if err := f.store.RevokeShare(ctx, f.alice.ID, d.ID, again.ID); err != nil {
		t.Fatal(err)
	}

	shares, err = f.store.SharesForDeck(ctx, f.alice.ID, d.ID)
	if err != nil {
		t.Fatal(err)
	}
	type row struct {
		id     int64
		to     int64
		status string
	}
	var got []row
	for _, sh := range shares {
		got = append(got, row{sh.ID, sh.ToUserID, sh.Status})
	}
	want := []row{
		{c2.ID, carol.ID, flash.ShareStatusAdopted},     // newer row first
		{first.ID, f.bob.ID, flash.ShareStatusDeclined}, // revoke fell back to the decline
	}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("shares = %+v, want %+v", got, want)
	}

	if _, err := f.store.SharesForDeck(ctx, f.bob.ID, d.ID); !errors.Is(err, flash.ErrNotFound) {
		t.Errorf("SharesForDeck by non-owner: err = %v, want ErrNotFound", err)
	}
}
```

- [ ] **Step 2: Write the failing handler test, and update the no-JS revoke test**

In `internal/apps/flash/handlers_share_test.go`, replace the whole `TestRevokeShareHandlerNoJSRedirects` function with:

```go
// TestRevokeShareHandlerNoJSRedirects covers #342 for revoke.
func TestRevokeShareHandlerNoJSRedirects(t *testing.T) {
	s := newShareServer(t)
	deckID := createDeckHX(t, s, s.Alice, "Spanish")
	deckIDStr := strconv.FormatInt(deckID, 10)
	shareID := shareToBob(t, s, deckID)

	s.Submit(t, s.Alice, "/flash/"+deckIDStr+"/share/"+shareID+"/revoke", url.Values{}, "/flash/"+deckIDStr)

	// The offer is gone from bob's side and, its only row now revoked, bob
	// is no longer in alice's "Shared with" list (#304).
	offers, err := s.Store.SharesForRecipient(t.Context(), s.Bob.User.ID)
	if err != nil || len(offers) != 0 {
		t.Fatalf("bob's offers after no-JS revoke = %+v, err = %v, want none", offers, err)
	}
	shares, err := s.Store.SharesForDeck(t.Context(), s.Alice.User.ID, deckID)
	if err != nil || len(shares) != 0 {
		t.Fatalf("Shared with after no-JS revoke = %+v, err = %v, want nobody", shares, err)
	}
}
```

Append:

```go
// TestSharedWithListShowsOneRowPerRecipient covers #304 bullet 2 through
// the real templates: bob gets one row whatever his history, it reads his
// latest status, and Revoke targets the offer that is actually pending.
func TestSharedWithListShowsOneRowPerRecipient(t *testing.T) {
	s := newShareServer(t)
	deckID := createDeckHX(t, s, s.Alice, "Spanish")
	deckIDStr := itoa(deckID)

	// Declined, then offered again: one row, waiting, Revoke on the new offer.
	first := shareToBob(t, s, deckID)
	s.PostHX(t, s.Bob, "/flash/shared/decline", url.Values{"share_id": {first}})
	again := shareToBob(t, s, deckID)

	doc := s.Get(t, s.Alice, "/flash/"+deckIDStr)
	rows := doc.QueryAll(".flash-share-list li")
	if len(rows) != 1 {
		t.Fatalf("Shared with has %d rows, want 1 for bob", len(rows))
	}
	if text := htmlassert.Text(rows[0]); !strings.Contains(text, "bob") || !strings.Contains(text, "waiting") || strings.Contains(text, "said no thanks") {
		t.Errorf("bob's row = %q, want bob, waiting, and no old decline", text)
	}
	doc.MustHave(`.flash-share-list form[action="/flash/` + deckIDStr + `/share/` + again + `/revoke"]`)

	// Bob adds it: still one row, now added, no Revoke.
	s.PostHX(t, s.Bob, "/flash/shared/adopt", url.Values{"share_id": {again}})
	doc = s.Get(t, s.Alice, "/flash/"+deckIDStr)
	rows = doc.QueryAll(".flash-share-list li")
	if len(rows) != 1 || !strings.Contains(htmlassert.Text(rows[0]), "added") {
		t.Fatalf("Shared with after adopt = %d rows, want 1 reading added", len(rows))
	}
	doc.MustNotHave(".flash-share-list form")

	// A re-share replaces it with waiting; revoking that falls back to added.
	third := shareToBob(t, s, deckID)
	doc = s.Get(t, s.Alice, "/flash/"+deckIDStr)
	rows = doc.QueryAll(".flash-share-list li")
	if len(rows) != 1 || !strings.Contains(htmlassert.Text(rows[0]), "waiting") {
		t.Fatalf("Shared with after re-share = %d rows, want 1 reading waiting", len(rows))
	}
	rec := s.PostHX(t, s.Alice, "/flash/"+deckIDStr+"/share/"+third+"/revoke", url.Values{})
	if rec.Code != 200 {
		t.Fatalf("revoke: %d; body: %s", rec.Code, rec.Body.String())
	}
	doc = htmlassert.Parse(t, rec.Body.String())
	rows = doc.QueryAll(".flash-share-list li")
	if len(rows) != 1 {
		t.Fatalf("Shared with after revoke has %d rows, want 1", len(rows))
	}
	if text := htmlassert.Text(rows[0]); !strings.Contains(text, "added") || strings.Contains(text, "waiting") {
		t.Errorf("bob's row after revoke = %q, want it back to added", text)
	}
	doc.MustNotHave(".flash-share-list form")
}
```

- [ ] **Step 3: Run to verify they fail**

Run: `go test ./internal/apps/flash/ -run 'TestSharesForDeckShowsEachRecipientsLatestStatus|TestSharedWithListShowsOneRowPerRecipient|TestRevokeShareHandlerNoJSRedirects' -count=1`
Expected: FAIL.
- `TestSharesForDeckShowsEachRecipientsLatestStatus` fails with `shares = [...] want just bob's re-offer`: today it returns every row, including the decline and carol's revoked one.
- `TestSharedWithListShowsOneRowPerRecipient` fails with `Shared with has 2 rows, want 1 for bob`.
- `TestRevokeShareHandlerNoJSRedirects` fails with `Shared with after no-JS revoke = [{… revoked …}] … want nobody`.

- [ ] **Step 4: Implement in `share.go`**

Replace `SharesForDeck` (its doc comment and body) with:

```go
// SharesForDeck is the creator's "Shared with" list for one of their own
// decks (UI overhaul spec §6, #304): one row per recipient, that person's
// latest share that wasn't revoked, newest first. A revoke cancels an
// offer rather than being an answer, so revoked rows are skipped, and a
// revoked re-offer leaves the person showing their last real answer
// ("added" / "said no thanks"). Someone whose only offers were all revoked
// isn't listed. "Latest" is created_at DESC, id DESC, so rows with equal
// timestamps still resolve the same way every time. ShareDeck never creates
// a second pending row for the same triple, so a pending row is always its
// recipient's latest one. That makes a waiting row's ID the pending share
// Revoke has to target.
func (st *Store) SharesForDeck(ctx context.Context, fromUserID, deckID int64) ([]Share, error) {
	if _, err := st.DeckByID(ctx, fromUserID, deckID); err != nil {
		return nil, err
	}
	rows, err := st.db.QueryContext(ctx,
		`SELECT id, deck_id, from_user_id, to_user_id, status, adopted_deck_id, created_at, responded_at
		   FROM (SELECT id, deck_id, from_user_id, to_user_id, status, adopted_deck_id, created_at, responded_at,
		                ROW_NUMBER() OVER (PARTITION BY to_user_id ORDER BY created_at DESC, id DESC) AS rn
		           FROM flash_shares
		          WHERE deck_id = ? AND from_user_id = ? AND status <> ?)
		  WHERE rn = 1
		  ORDER BY created_at DESC, id DESC`,
		deckID, fromUserID, ShareStatusRevoked)
	if err != nil {
		return nil, fmt.Errorf("flash: shares for deck: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []Share
	for rows.Next() {
		sh, err := scanShare(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, sh)
	}
	return out, rows.Err()
}
```

- [ ] **Step 5: Drop the template's revoked filter**

In `internal/apps/flash/templates/cards.partial.html`, replace:

```
					{{range .SharedWith}}{{if ne .Status "revoked"}}
```

with:

```
					{{range .SharedWith}}
```

and replace (a few lines below, closing that range):

```
					{{end}}{{end}}
				</ul>
```

with:

```
					{{end}}
				</ul>
```

- [ ] **Step 6: Update doc comments in `handlers_decks.go`**

Replace the `shareWithUsername` doc comment:

```go
// shareWithUsername is one row of the creator's "Shared with" list: a
// Share plus the recipient's username, resolved via a.deps.Users since
// Share itself only carries a bare user ID.
```

with:

```go
// shareWithUsername is one row of the creator's "Shared with" list: one
// recipient's latest non-revoked Share (see SharesForDeck) plus their
// username, resolved via a.deps.Users since Share itself only carries a
// bare user ID.
```

Replace the `shareContext` doc comment:

```go
// shareContext loads everything the deck detail view's Share section
// needs: every other account on the instance (for the dropdown) and this
// deck's own share offers, each paired with its recipient's username (for
// the "Shared with" list).
```

with:

```go
// shareContext loads everything the deck detail view's Share section
// needs: every other account on the instance (for the dropdown) and the
// "Shared with" list (one row per recipient, see SharesForDeck), each row
// paired with its recipient's username.
```

- [ ] **Step 7: Run to verify they pass, then the package**

Run: `go test ./internal/apps/flash/ -run 'TestSharesForDeck|TestSharedWithList|TestRevokeShare|TestDeclineShare|TestShareMenu' -count=1 -v`
Expected: PASS for every listed test, including the unchanged `TestRevokeShareRequiresMatchingDeckID` (still one pending row) and `TestDeclineShareHandlerNoJSRedirects` (a declined row is still listed).

Run: `go test ./internal/apps/flash/... -race -count=1`
Expected: `ok`.

- [ ] **Step 8: Full check and commit**

Run the full check from Global Constraints. Expected: `gofmt` prints nothing, every package `ok`.

```bash
git add internal/apps/flash/share.go internal/apps/flash/handlers_decks.go internal/apps/flash/templates/cards.partial.html internal/apps/flash/share_test.go internal/apps/flash/handlers_share_test.go
git commit -m "fix(flash): one row per recipient in the Shared with list (#304)"
```

---

### Task 2: Adopting a deck whose name you already use auto-suffixes it (#304.5)

**Files:**
- Modify: `internal/apps/flash/share.go`, `internal/apps/flash/handlers_share.go`
- Create: `internal/apps/flash/share_name_internal_test.go`
- Test: `internal/apps/flash/share_test.go`, `internal/apps/flash/handlers_share_test.go`

**Interfaces:**
- Consumes: `MaxDeckNameRunes` (120), `ValidateDeck`, `usernamesByID`, `priorAdoptedDeck`, `isUniqueViolation`.
- Produces:
  ```go
  func (st *Store) AdoptShare(ctx context.Context, toUserID, shareID int64, usernames map[int64]string) (AdoptResult, error)
  // adoptedDeckName is candidate n (n >= 1) for a renamed copy: "base (from sharer)", "base (from sharer) (2)"…;
  // "base (shared)" when sharer is "". Always <= MaxDeckNameRunes runes; only the base is shortened.
  func adoptedDeckName(base, sharer string, n int) string
  // freeDeckName returns base if userID has no deck with that exact name, else the first free adoptedDeckName candidate.
  func freeDeckName(ctx context.Context, tx *sql.Tx, userID int64, base, sharer string) (string, error)
  ```
  `AdoptResult` is unchanged. `Deck.Name` carries the final name.

- [ ] **Step 1: Update the existing call sites to the new signature**

Run this *before* adding the new tests. The pattern only matches the 3-argument form, so it's safe to re-run:

```bash
sed -i '' -E 's/AdoptShare\(ctx, ([^,()]+), ([^,()]+)\)/AdoptShare(ctx, \1, \2, nil)/g' internal/apps/flash/share_test.go
grep -c 'AdoptShare(ctx, [^,]*, [^,]*, nil)' internal/apps/flash/share_test.go
```
Expected: `18`. That's the 17 original call sites plus the one Task 1 added in `TestSharesForDeckShowsEachRecipientsLatestStatus`.

- [ ] **Step 2: Write the failing tests**

Create `internal/apps/flash/share_name_internal_test.go`:

```go
package flash

import (
	"strings"
	"testing"
	"unicode/utf8"
)

// TestAdoptedDeckName pins #304 bullet 5's naming rule: the sharer's name
// as a suffix, then a counter, and only ever the base shortened so the
// whole name stays within MaxDeckNameRunes.
func TestAdoptedDeckName(t *testing.T) {
	long := strings.Repeat("é", MaxDeckNameRunes)
	spaced := strings.Repeat("a", 106) + " tail" // cut lands right after the space
	tests := []struct {
		base, sharer string
		n            int
		want         string
	}{
		{"Spanish", "alice", 1, "Spanish (from alice)"},
		{"Spanish", "alice", 2, "Spanish (from alice) (2)"},
		{"Spanish", "alice", 3, "Spanish (from alice) (3)"},
		{"Spanish", "", 1, "Spanish (shared)"},
		{"Spanish", "", 2, "Spanish (shared) (2)"},
		// " (from alice) (3)" is 17 runes, so 103 of the base survive.
		{long, "alice", 3, strings.Repeat("é", 103) + " (from alice) (3)"},
		// 107 runes survive, ending in a space, which is trimmed.
		{spaced, "alice", 1, strings.Repeat("a", 106) + " (from alice)"},
	}
	for _, tt := range tests {
		got := adoptedDeckName(tt.base, tt.sharer, tt.n)
		if got != tt.want {
			t.Errorf("adoptedDeckName(%.10q…, %q, %d) = %q, want %q", tt.base, tt.sharer, tt.n, got, tt.want)
		}
		if n := utf8.RuneCountInString(got); n > MaxDeckNameRunes {
			t.Errorf("adoptedDeckName(%.10q…, %q, %d) is %d runes, over %d", tt.base, tt.sharer, tt.n, n, MaxDeckNameRunes)
		}
		if err := ValidateDeck(got, ""); err != nil {
			t.Errorf("adoptedDeckName(%.10q…, %q, %d) = %q fails ValidateDeck: %v", tt.base, tt.sharer, tt.n, got, err)
		}
	}
}
```

In `internal/apps/flash/share_test.go`, add `"strings"` and `"unicode/utf8"` to the import block, then append:

```go
// TestAdoptShareRenamesOnNameCollision covers #304 bullet 5: adopting a
// deck whose name the recipient already uses never fails. The copy takes
// the first free "(from alice)" name, and a later re-share still merges
// into that renamed copy, because the merge finds it by id, not name.
func TestAdoptShareRenamesOnNameCollision(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	names := map[int64]string{f.alice.ID: "alice", f.bob.ID: "bob"}
	d, err := f.store.CreateDeck(ctx, f.alice.ID, "Spanish", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.CreateCard(ctx, f.alice.ID, d.ID, flash.CardTypeBasic, "hola", "hello", ""); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"Spanish", "Spanish (from alice)"} {
		if _, err := f.store.CreateDeck(ctx, f.bob.ID, name, ""); err != nil {
			t.Fatal(err)
		}
	}

	sh, err := f.store.ShareDeck(ctx, f.alice.ID, d.ID, f.bob.ID)
	if err != nil {
		t.Fatal(err)
	}
	first, err := f.store.AdoptShare(ctx, f.bob.ID, sh.ID, names)
	if err != nil {
		t.Fatalf("adopt with a name collision: %v", err)
	}
	if first.Merged || first.CardsCopied != 1 {
		t.Errorf("result = %+v, want a first-time adoption of 1 card", first)
	}
	if first.Deck.Name != "Spanish (from alice) (2)" {
		t.Errorf("adopted deck name = %q, want %q", first.Deck.Name, "Spanish (from alice) (2)")
	}

	if _, err := f.store.CreateCard(ctx, f.alice.ID, d.ID, flash.CardTypeBasic, "adios", "goodbye", ""); err != nil {
		t.Fatal(err)
	}
	sh2, err := f.store.ShareDeck(ctx, f.alice.ID, d.ID, f.bob.ID)
	if err != nil {
		t.Fatal(err)
	}
	merged, err := f.store.AdoptShare(ctx, f.bob.ID, sh2.ID, names)
	if err != nil {
		t.Fatal(err)
	}
	if !merged.Merged || merged.Deck.ID != first.Deck.ID || merged.CardsCopied != 1 {
		t.Errorf("re-share result = %+v, want a merge of 1 card into deck %d", merged, first.Deck.ID)
	}
	if merged.Deck.Name != "Spanish (from alice) (2)" {
		t.Errorf("merged deck name = %q, want the renamed copy untouched", merged.Deck.Name)
	}
	decks, err := f.store.ListDecks(ctx, f.bob.ID)
	if err != nil || len(decks) != 3 {
		t.Fatalf("bob's decks = %+v, err = %v, want his two plus one copy", decks, err)
	}
}

// TestAdoptShareRenameStaysWithinTheNameLimit: a max-length name that
// collides still produces a valid deck name.
func TestAdoptShareRenameStaysWithinTheNameLimit(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	long := strings.Repeat("é", flash.MaxDeckNameRunes)
	d, err := f.store.CreateDeck(ctx, f.alice.ID, long, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.CreateDeck(ctx, f.bob.ID, long, ""); err != nil {
		t.Fatal(err)
	}
	sh, err := f.store.ShareDeck(ctx, f.alice.ID, d.ID, f.bob.ID)
	if err != nil {
		t.Fatal(err)
	}
	result, err := f.store.AdoptShare(ctx, f.bob.ID, sh.ID, map[int64]string{f.alice.ID: "alice"})
	if err != nil {
		t.Fatal(err)
	}
	name := result.Deck.Name
	if n := utf8.RuneCountInString(name); n != flash.MaxDeckNameRunes || !strings.HasSuffix(name, " (from alice)") {
		t.Errorf("name = %q (%d runes), want %d runes ending in (from alice)", name, n, flash.MaxDeckNameRunes)
	}
	if err := flash.ValidateDeck(name, ""); err != nil {
		t.Errorf("renamed deck fails ValidateDeck: %v", err)
	}
}

// TestAdoptShareRenameWithoutASharerName: with no username to hand (a nil
// map), the suffix falls back to "(shared)" rather than "(from )".
func TestAdoptShareRenameWithoutASharerName(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	d, err := f.store.CreateDeck(ctx, f.alice.ID, "Spanish", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.CreateDeck(ctx, f.bob.ID, "Spanish", ""); err != nil {
		t.Fatal(err)
	}
	sh, err := f.store.ShareDeck(ctx, f.alice.ID, d.ID, f.bob.ID)
	if err != nil {
		t.Fatal(err)
	}
	result, err := f.store.AdoptShare(ctx, f.bob.ID, sh.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Deck.Name != "Spanish (shared)" {
		t.Errorf("name = %q, want %q", result.Deck.Name, "Spanish (shared)")
	}
}
```

In `internal/apps/flash/handlers_share_test.go`, append:

```go
// TestAdoptRenamesOnNameCollision covers #304 bullet 5 through HTTP: the
// click used to 400 (which HTMX doesn't swap, so nothing happened). Now it
// adds the deck under a suffixed name and the notice uses that name.
func TestAdoptRenamesOnNameCollision(t *testing.T) {
	s := newShareServer(t)
	aliceDeck := createDeckHX(t, s, s.Alice, "Spanish")
	createDeckHX(t, s, s.Bob, "Spanish")
	shareID := shareToBob(t, s, aliceDeck)

	rec := s.PostHX(t, s.Bob, "/flash/shared/adopt", url.Values{"share_id": {shareID}})
	if rec.Code != 200 {
		t.Fatalf("adopt with a name collision: %d, want 200; body: %s", rec.Code, rec.Body.String())
	}
	doc := htmlassert.Parse(t, rec.Body.String())
	if got := htmlassert.Text(doc.MustHave("#deck-detail-view .flash-notice")); got != "“Spanish (from alice)” is now in your decks." {
		t.Errorf("notice = %q", got)
	}
	if got := htmlassert.Text(doc.MustHave("#deck-detail-view h1")); got != "Spanish (from alice)" {
		t.Errorf("deck heading = %q", got)
	}
	doc.MustNotHave("#deck-list .deck-row-gift")

	decks, err := s.Store.ListDecks(t.Context(), s.Bob.User.ID)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, d := range decks {
		got[d.Name] = true
	}
	if len(decks) != 2 || !got["Spanish"] || !got["Spanish (from alice)"] {
		t.Errorf("bob's decks = %+v, want Spanish and Spanish (from alice)", decks)
	}
}
```

- [ ] **Step 3: Run to verify they fail**

Run: `go test ./internal/apps/flash/ -run 'TestAdoptedDeckName|TestAdoptShareRename|TestAdoptRenamesOnNameCollision' -count=1`
Expected: FAIL to build. The errors are `too many arguments in call to f.store.AdoptShare` in `share_test.go`, and `undefined: adoptedDeckName` in `share_name_internal_test.go`.

- [ ] **Step 4: Implement in `share.go`**

Replace the import block with:

```go
import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)
```

Replace `AdoptShare`'s doc comment and signature line:

```go
// AdoptShare resolves one of toUserID's own pending offers (shareID). If no
// earlier offer for the same (deck, creator, recipient) triple has ever been
// adopted, this is a first-time adoption: a brand-new deck is created for
// toUserID and every source card is copied into it. If an earlier offer for
// that triple was already adopted, this is a merge: cards are copied into
// that same existing deck, skipping any source card already represented
// there (by origin_card_id) — so cards and progress the recipient already
// has are never touched. Either way, no FSRS review state is ever copied:
// every copied card starts brand new in the recipient's review queue.
func (st *Store) AdoptShare(ctx context.Context, toUserID, shareID int64) (AdoptResult, error) {
```

with:

```go
// AdoptShare resolves one of toUserID's own pending offers (shareID). If no
// earlier offer for the same (deck, creator, recipient) triple has ever been
// adopted, this is a first-time adoption: a brand-new deck is created for
// toUserID and every source card is copied into it. If an earlier offer for
// that triple was already adopted, this is a merge: cards are copied into
// that same existing deck, skipping any source card already represented
// there (by origin_card_id) — so cards and progress the recipient already
// has are never touched. Either way, no FSRS review state is ever copied:
// every copied card starts brand new in the recipient's review queue.
//
// A first-time adoption never fails over a name the recipient already uses
// (#304): the copy takes the first free name freeDeckName finds, e.g.
// "Spanish (from alice)". usernames maps account ids to usernames for that
// suffix. The store can't see accounts, and a lookup callback would run
// inside this transaction on the database's only connection, so the caller
// passes plain data it already has (the handler's usernamesByID). A missing
// entry, or a nil map, gives "(shared)" instead.
func (st *Store) AdoptShare(ctx context.Context, toUserID, shareID int64, usernames map[int64]string) (AdoptResult, error) {
```

Inside `AdoptShare`, replace:

```go
	var targetDeckID int64
	if priorAdoptedDeckID != nil {
		targetDeckID = *priorAdoptedDeckID
	} else {
		err = tx.QueryRowContext(ctx,
			`INSERT INTO flash_decks (user_id, name, description, created_at, color)
			 VALUES (?, ?, ?, ?, ?)
			 RETURNING id`,
			toUserID, src.Name, src.Description, formatTime(st.now()), src.Color,
		).Scan(&targetDeckID)
		if err != nil {
			if isUniqueViolation(err) {
				return AdoptResult{}, fmt.Errorf("%w: you already have a deck named %q", ErrInvalid, src.Name)
			}
			return AdoptResult{}, fmt.Errorf("flash: adopt share: %w", err)
		}
	}
```

with:

```go
	var targetDeckID int64
	if priorAdoptedDeckID != nil {
		targetDeckID = *priorAdoptedDeckID
	} else {
		name, err := freeDeckName(ctx, tx, toUserID, src.Name, usernames[sh.FromUserID])
		if err != nil {
			return AdoptResult{}, err
		}
		err = tx.QueryRowContext(ctx,
			`INSERT INTO flash_decks (user_id, name, description, created_at, color)
			 VALUES (?, ?, ?, ?, ?)
			 RETURNING id`,
			toUserID, name, src.Description, formatTime(st.now()), src.Color,
		).Scan(&targetDeckID)
		if err != nil {
			// freeDeckName checked this exact name inside this transaction,
			// on the only connection, so the unique index is just the
			// backstop here.
			if isUniqueViolation(err) {
				return AdoptResult{}, fmt.Errorf("%w: you already have a deck named %q", ErrInvalid, name)
			}
			return AdoptResult{}, fmt.Errorf("flash: adopt share: %w", err)
		}
	}
```

Add after `priorAdoptedDeck`:

```go
// adoptedDeckName is the nth candidate name (n >= 1) for a first-time
// adoption's copy when the source deck's own name is taken (#304): "base
// (from alice)", then "base (from alice) (2)", "(3)"…, or "(shared)" in
// place of "(from alice)" when the sharer's name isn't known. The base is
// cut short, never the suffix, so the result always fits
// MaxDeckNameRunes. A username is at most 32 characters, so the suffix
// never comes close to eating the whole name.
func adoptedDeckName(base, sharer string, n int) string {
	suffix := " (shared)"
	if sharer != "" {
		suffix = " (from " + sharer + ")"
	}
	if n > 1 {
		suffix += " (" + strconv.Itoa(n) + ")"
	}
	keep := MaxDeckNameRunes - utf8.RuneCountInString(suffix)
	if r := []rune(base); len(r) > keep {
		base = strings.TrimRightFunc(string(r[:keep]), unicode.IsSpace)
	}
	return base + suffix
}

// freeDeckName is the name AdoptShare gives a first-time copy: base itself
// if userID has no deck called exactly that (the unique index compares
// bytes, and so does this), otherwise the first adoptedDeckName candidate
// they don't have. The loop always ends, because every taken candidate is
// a distinct deck the user already owns. It runs inside AdoptShare's
// transaction on the database's single connection (db.go's
// SetMaxOpenConns(1)), so nothing can take the name between this check
// and the INSERT.
func freeDeckName(ctx context.Context, tx *sql.Tx, userID int64, base, sharer string) (string, error) {
	name := base
	for n := 1; ; n++ {
		var one int
		err := tx.QueryRowContext(ctx,
			`SELECT 1 FROM flash_decks WHERE user_id = ? AND name = ?`, userID, name).Scan(&one)
		switch {
		case errors.Is(err, sql.ErrNoRows):
			return name, nil
		case err != nil:
			return "", fmt.Errorf("flash: adopt share: deck name: %w", err)
		}
		name = adoptedDeckName(base, sharer, n)
	}
}
```

- [ ] **Step 5: Pass usernames from the handler**

In `internal/apps/flash/handlers_share.go`, in `adoptShareHandler`, replace:

```go
	result, err := a.store.AdoptShare(r.Context(), userID, shareID)
	if err != nil {
		a.fail(w, r, err)
		return
	}
```

with:

```go
	// The sharer's username names a renamed copy ("Spanish (from alice)",
	// #304). It's looked up up front because AdoptShare can't call back
	// into the database from inside its transaction.
	_, byID, err := a.usernamesByID(r.Context())
	if err != nil {
		a.deps.Errors.Internal(w, r, err)
		return
	}
	result, err := a.store.AdoptShare(r.Context(), userID, shareID, byID)
	if err != nil {
		a.fail(w, r, err)
		return
	}
```

- [ ] **Step 6: Run to verify they pass, then the package**

Run: `go test ./internal/apps/flash/ -run 'TestAdoptedDeckName|TestAdoptShare|TestAdoptRenamesOnNameCollision|TestAdoptNotice|TestFullShareCycleThroughHTTP' -count=1 -v`
Expected: PASS, including the existing `TestAdoptShare*` tests (no collision, so the plain name "Spanish" is kept) and `TestAdoptNoticeWordingForFirstAdoptAndMerge`.

Run: `go test ./internal/apps/flash/... -race -count=1`
Expected: `ok`.

- [ ] **Step 7: Full check and commit**

Run the full check from Global Constraints. Expected: `gofmt` prints nothing, every package `ok`.

```bash
git add internal/apps/flash/share.go internal/apps/flash/handlers_share.go internal/apps/flash/share_name_internal_test.go internal/apps/flash/share_test.go internal/apps/flash/handlers_share_test.go
git commit -m "fix(flash): auto-suffix an adopted deck's name when it collides (#304)"
```

---

### Task 3: A re-share with nothing new reads "up to date" (#346)

**Files:**
- Modify: `internal/apps/flash/handlers_decks.go`, `internal/apps/flash/handlers_share.go`, `internal/apps/flash/templates/decks.html`, `internal/apps/flash/templates/gift.partial.html`
- Test: `internal/apps/flash/share_test.go`, `internal/apps/flash/handlers_share_test.go`

**Interfaces:**
- Consumes: `ShareOffer.PriorAdoptedDeckID`, `ShareOffer.NewCardCount`, `SharePreview.CardCount` (the new-card count for a merge), `AdoptResult.Merged`/`CardsCopied`, and `AdoptShare(…, usernames)` from Task 2.
- Produces:
  ```go
  // giftRow gains:
  UpToDate bool // a merge with 0 new cards: the row shows no badge (#346)
  // giftView gains:
  UpToDate bool // a merge with 0 new cards: "You already have every card in", Got it only (#346)
  ```
  Template hooks: `#deck-list .flash-gift-badge` is absent for an up-to-date row. `.flash-gift-actions` holds exactly one `form[action="/flash/shared/adopt"]` whose button reads "Got it". The notice after Got it is `“<Deck>” is already up to date.`

- [ ] **Step 1: Write the store coverage tests (#346)**

In `internal/apps/flash/share_test.go`, append:

```go
// TestSharePreviewOfAnEmptyDeck covers #346's first-share edge case: a
// deck with no cards previews as 0 cards with no samples.
func TestSharePreviewOfAnEmptyDeck(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	d, err := f.store.CreateDeck(ctx, f.alice.ID, "Empty", "")
	if err != nil {
		t.Fatal(err)
	}
	sh, err := f.store.ShareDeck(ctx, f.alice.ID, d.ID, f.bob.ID)
	if err != nil {
		t.Fatal(err)
	}
	p, err := f.store.SharePreview(ctx, f.bob.ID, sh.ID)
	if err != nil {
		t.Fatal(err)
	}
	if p.Offer.PriorAdoptedDeckID != nil || p.CardCount != 0 || len(p.Samples) != 0 {
		t.Errorf("preview = %+v, want a first-time offer of 0 cards and no samples", p)
	}
}

// TestUpToDateMergeOfferCountsZero covers #346's merge edge case: a
// re-share with nothing new since the last adoption is still a merge offer,
// with 0 new cards and no samples.
func TestUpToDateMergeOfferCountsZero(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	d, err := f.store.CreateDeck(ctx, f.alice.ID, "Spanish", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.CreateCard(ctx, f.alice.ID, d.ID, flash.CardTypeBasic, "hola", "hello", ""); err != nil {
		t.Fatal(err)
	}
	sh, err := f.store.ShareDeck(ctx, f.alice.ID, d.ID, f.bob.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.AdoptShare(ctx, f.bob.ID, sh.ID, nil); err != nil {
		t.Fatal(err)
	}
	again, err := f.store.ShareDeck(ctx, f.alice.ID, d.ID, f.bob.ID)
	if err != nil {
		t.Fatal(err)
	}

	offers, err := f.store.SharesForRecipient(ctx, f.bob.ID)
	if err != nil || len(offers) != 1 {
		t.Fatalf("offers = %+v, err = %v", offers, err)
	}
	if offers[0].PriorAdoptedDeckID == nil || offers[0].NewCardCount != 0 {
		t.Errorf("offer = %+v, want a merge offer with NewCardCount 0", offers[0])
	}
	p, err := f.store.SharePreview(ctx, f.bob.ID, again.ID)
	if err != nil {
		t.Fatal(err)
	}
	if p.Offer.PriorAdoptedDeckID == nil || p.CardCount != 0 || len(p.Samples) != 0 {
		t.Errorf("preview = %+v, want a merge of 0 cards and no samples", p)
	}
}
```

- [ ] **Step 2: Write the handler tests**

In `internal/apps/flash/handlers_share_test.go`, append:

```go
// TestGiftPreviewOfAnEmptyDeck covers #346's first-share edge case through
// the template: "0 cards", no "A few of the cards" section, and the normal
// first-time buttons and badge.
func TestGiftPreviewOfAnEmptyDeck(t *testing.T) {
	s := newShareServer(t)
	deckID := createDeckHX(t, s, s.Alice, "Empty")
	shareID := shareToBob(t, s, deckID)

	doc := s.Get(t, s.Bob, "/flash/shared/"+shareID)
	text := htmlassert.Text(doc.MustHave("#deck-detail .flash-gift"))
	if !strings.Contains(text, "alice shared this deck with you") || !strings.Contains(text, "0 cards") {
		t.Errorf("gift pane = %q, want the first-time wording and 0 cards", text)
	}
	if strings.Contains(text, "A few of the cards") {
		t.Errorf("gift pane = %q, should not offer samples of an empty deck", text)
	}
	doc.MustNotHave(".flash-gift .flash-section-title")
	doc.MustNotHave(".flash-gift .flash-mini-card")
	if n := len(doc.QueryAll(".flash-gift-actions button")); n != 2 {
		t.Errorf("gift pane has %d buttons, want Add to my decks and No thanks", n)
	}
	if got := htmlassert.Text(doc.MustHave("#deck-list .flash-gift-badge")); got != "new" {
		t.Errorf("gift badge = %q, want new", got)
	}
}

// TestUpToDateReshareSaysSo covers #346's merge edge case: a re-share with
// no new cards has no "+0" badge, a pane that says you already have every
// card with a single Got it, and an "already up to date" notice.
func TestUpToDateReshareSaysSo(t *testing.T) {
	s := newShareServer(t)
	deckID := createDeckHX(t, s, s.Alice, "Spanish")
	deckIDStr := itoa(deckID)
	addCard := func(front string) {
		t.Helper()
		rec := s.PostHX(t, s.Alice, "/flash/"+deckIDStr+"/cards/new",
			url.Values{"card_type": {"basic"}, "front": {front}, "back": {"x"}, "notes": {""}})
		if rec.Code != http.StatusCreated {
			t.Fatalf("create card: %d; body: %s", rec.Code, rec.Body.String())
		}
	}
	addCard("hola")
	s.PostHX(t, s.Bob, "/flash/shared/adopt", url.Values{"share_id": {shareToBob(t, s, deckID)}})

	// A merge with something new still shows its count.
	addCard("adios")
	withNew := shareToBob(t, s, deckID)
	if got := htmlassert.Text(s.Get(t, s.Bob, "/flash/").MustHave("#deck-list .flash-gift-badge")); got != "+1" {
		t.Errorf("merge badge = %q, want +1", got)
	}
	s.PostHX(t, s.Bob, "/flash/shared/adopt", url.Values{"share_id": {withNew}})

	// Nothing new since: the gift row is there, with no badge…
	upToDate := shareToBob(t, s, deckID)
	list := s.Get(t, s.Bob, "/flash/")
	list.MustHave("#deck-list a.deck-row-gift")
	list.MustNotHave("#deck-list .flash-gift-badge")

	// …the pane says so, with Got it as its only button…
	pane := s.Get(t, s.Bob, "/flash/shared/"+upToDate)
	text := htmlassert.Text(pane.MustHave("#deck-detail .flash-gift"))
	if !strings.Contains(text, "You already have every card in Spanish") {
		t.Errorf("gift pane = %q, want it to say you already have every card in Spanish", text)
	}
	if strings.Contains(text, "0 new card") {
		t.Errorf("gift pane = %q, should not count 0 new cards", text)
	}
	buttons := pane.QueryAll(".flash-gift-actions button")
	if len(buttons) != 1 || htmlassert.Text(buttons[0]) != "Got it" {
		t.Errorf("gift pane buttons = %d, want just Got it", len(buttons))
	}
	pane.MustHave(`.flash-gift-actions form[action="/flash/shared/adopt"]`)
	pane.MustNotHave(`.flash-gift-actions form[action="/flash/shared/decline"]`)

	// …and Got it resolves the offer with an "already up to date" notice.
	rec := s.PostHX(t, s.Bob, "/flash/shared/adopt", url.Values{"share_id": {upToDate}})
	if rec.Code != 200 {
		t.Fatalf("got it: %d; body: %s", rec.Code, rec.Body.String())
	}
	doc := htmlassert.Parse(t, rec.Body.String())
	if got := htmlassert.Text(doc.MustHave("#deck-detail-view .flash-notice")); got != "“Spanish” is already up to date." {
		t.Errorf("notice = %q", got)
	}
	doc.MustNotHave("#deck-list .deck-row-gift")
	decks, err := s.Store.ListDecks(t.Context(), s.Bob.User.ID)
	if err != nil || len(decks) != 1 {
		t.Fatalf("bob's decks = %+v, err = %v, want the one copy", decks, err)
	}
	cards, err := s.Store.ListCards(t.Context(), s.Bob.User.ID, decks[0].ID)
	if err != nil || len(cards) != 2 {
		t.Errorf("bob's cards = %d, err = %v, want 2 (nothing copied twice)", len(cards), err)
	}
}
```

- [ ] **Step 3: Run to verify**

Run: `go test ./internal/apps/flash/ -run 'TestSharePreviewOfAnEmptyDeck|TestUpToDateMergeOfferCountsZero|TestGiftPreviewOfAnEmptyDeck|TestUpToDateReshareSaysSo' -count=1`
Expected:
- `TestSharePreviewOfAnEmptyDeck`, `TestUpToDateMergeOfferCountsZero` and `TestGiftPreviewOfAnEmptyDeck` **PASS already**. They're the coverage #346 asks for on behaviour that is already right.
- `TestUpToDateReshareSaysSo` **FAILS** with `htmlassert: unexpected element matching "#deck-list .flash-gift-badge"`. The "+0" badge is still there.

- [ ] **Step 4: View models in `handlers_decks.go`**

Replace the `giftRow` struct with:

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
	UpToDate     bool // a merge with 0 new cards: the row shows no badge (#346)
}
```

Replace the `giftView` struct with:

```go
// giftView is the pane for one gift.
type giftView struct {
	ShareID      int64
	DeckName     string
	Description  string
	Color        string
	FromUsername string
	IsMerge      bool
	// UpToDate is a merge with 0 new cards (#346): the pane says the
	// recipient already has every card and offers only Got it, which
	// adopts the offer so it leaves the list.
	UpToDate  bool
	CardCount int
	Samples   []cardGridItem
	CSRFToken string
}
```

In `buildDeckIndex`, replace:

```go
		gifts[i] = giftRow{
			ShareID: o.ID, DeckName: o.DeckName, DeckColor: o.DeckColor, FromUsername: o.FromUsername,
			IsMerge: o.PriorAdoptedDeckID != nil, NewCardCount: o.NewCardCount,
		}
```

with:

```go
		isMerge := o.PriorAdoptedDeckID != nil
		gifts[i] = giftRow{
			ShareID: o.ID, DeckName: o.DeckName, DeckColor: o.DeckColor, FromUsername: o.FromUsername,
			IsMerge: isMerge, NewCardCount: o.NewCardCount, UpToDate: isMerge && o.NewCardCount == 0,
		}
```

- [ ] **Step 5: Handlers in `handlers_share.go`**

In `giftPreview`, replace:

```go
	gift := giftView{
		ShareID: shareID, DeckName: p.Deck.Name, Description: p.Deck.Description, Color: p.Deck.Color,
		FromUsername: byID[p.Offer.FromUserID], IsMerge: p.Offer.PriorAdoptedDeckID != nil,
		CardCount: p.CardCount, CSRFToken: web.CSRFToken(r.Context()),
	}
```

with:

```go
	isMerge := p.Offer.PriorAdoptedDeckID != nil
	gift := giftView{
		ShareID: shareID, DeckName: p.Deck.Name, Description: p.Deck.Description, Color: p.Deck.Color,
		FromUsername: byID[p.Offer.FromUserID], IsMerge: isMerge,
		// For a merge, CardCount is only the cards not yet adopted.
		UpToDate:  isMerge && p.CardCount == 0,
		CardCount: p.CardCount, CSRFToken: web.CSRFToken(r.Context()),
	}
```

In `adoptShareHandler`, replace:

```go
	view.Notice = "“" + d.Name + "” is now in your decks."
	if result.Merged {
		view.Notice = fmt.Sprintf("%d new card%s added to “%s”.", result.CardsCopied, plural(result.CardsCopied), d.Name)
	}
```

with:

```go
	switch {
	case result.Merged && result.CardsCopied == 0:
		// Got it on an up-to-date re-share (#346): nothing was copied.
		view.Notice = "“" + d.Name + "” is already up to date."
	case result.Merged:
		view.Notice = fmt.Sprintf("%d new card%s added to “%s”.", result.CardsCopied, plural(result.CardsCopied), d.Name)
	default:
		view.Notice = "“" + d.Name + "” is now in your decks."
	}
```

- [ ] **Step 6: Templates**

In `internal/apps/flash/templates/decks.html`, replace:

```
			<span class="flash-gift-badge">{{if .IsMerge}}+{{.NewCardCount}}{{else}}new{{end}}</span>
```

with:

```
			{{if not .UpToDate}}<span class="flash-gift-badge">{{if .IsMerge}}+{{.NewCardCount}}{{else}}new{{end}}</span>{{end}}
```

In `internal/apps/flash/templates/gift.partial.html`, replace:

```
			<p class="flash-gift-from">{{if .IsMerge}}{{.FromUsername}} added {{.CardCount}} new card{{if ne .CardCount 1}}s{{end}} to{{else}}{{.FromUsername}} shared this deck with you{{end}}</p>
```

with:

```
			<p class="flash-gift-from">{{if .UpToDate}}You already have every card in{{else if .IsMerge}}{{.FromUsername}} added {{.CardCount}} new card{{if ne .CardCount 1}}s{{end}} to{{else}}{{.FromUsername}} shared this deck with you{{end}}</p>
```

and replace:

```
					<button type="submit" class="flash-big-btn flash-big-btn-primary">{{ticon "plus"}}{{if .IsMerge}}Add the new cards{{else}}Add to my decks{{end}}</button>
				</form>
				<form method="post" action="/flash/shared/decline" hx-post="/flash/shared/decline" hx-target="#deck-detail" hx-swap="innerHTML">
					<input type="hidden" name="{{csrfField}}" value="{{.CSRFToken}}">
					<input type="hidden" name="share_id" value="{{.ShareID}}">
					<button type="submit" class="flash-big-btn">No thanks</button>
				</form>
```

with:

```
					<button type="submit" class="flash-big-btn flash-big-btn-primary">{{if .UpToDate}}Got it{{else}}{{ticon "plus"}}{{if .IsMerge}}Add the new cards{{else}}Add to my decks{{end}}{{end}}</button>
				</form>
				{{if not .UpToDate}}
				<form method="post" action="/flash/shared/decline" hx-post="/flash/shared/decline" hx-target="#deck-detail" hx-swap="innerHTML">
					<input type="hidden" name="{{csrfField}}" value="{{.CSRFToken}}">
					<input type="hidden" name="share_id" value="{{.ShareID}}">
					<button type="submit" class="flash-big-btn">No thanks</button>
				</form>
				{{end}}
```

(The `{{if .Samples}}` block below already hides the "The new cards" heading when there are none.)

- [ ] **Step 7: Run to verify they pass, then the package**

Run: `go test ./internal/apps/flash/ -run 'TestSharePreviewOfAnEmptyDeck|TestUpToDateMergeOfferCountsZero|TestGiftPreview|TestUpToDateReshareSaysSo|TestAdoptNotice|TestGiftRow' -count=1 -v`
Expected: PASS.

Run: `go test ./internal/apps/flash/... -race -count=1`
Expected: `ok`.

- [ ] **Step 8: Full check and commit**

Run the full check from Global Constraints. Expected: `gofmt` prints nothing, every package `ok`.

```bash
git add internal/apps/flash/handlers_decks.go internal/apps/flash/handlers_share.go internal/apps/flash/templates/decks.html internal/apps/flash/templates/gift.partial.html internal/apps/flash/share_test.go internal/apps/flash/handlers_share_test.go
git commit -m "fix(flash): a re-share with no new cards reads as up to date (#346)"
```

---

### Task 4: Record the rules in spec §6

**Files:**
- Modify: `docs/superpowers/specs/2026-09-23-on-flash-ui-overhaul-design.md`

**Interfaces:** none. This task is docs only.

- [ ] **Step 1: Share popover bullet**

Replace:

```
- **Share popover**: the deck toolbar's Share button is a `<details>`
  disclosure (the same no-JS mechanism as the Notes shortcuts menu) whose
  panel has a person picker + Share button, and the existing shares for the
  deck, each with a status pill ("waiting" / "added") and Revoke while
  pending. Hidden entirely when there is no other account to share with.
  After a share or revoke, the pane re-renders with the popover open.
```

with:

```
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
```

- [ ] **Step 2: Gift bullets**

Replace:

```
  - Merge offer: "Name added N new cards to *Deck*", with **Add the new
    cards** / **No thanks**.
  - Buttons post to the existing adopt/decline routes. After adopting, the
    pane opens the (new or merged) deck with a notice ("*Planets* added to
    your decks"); after declining, the pane shows the default state.
```

with:

```
  - Merge offer: "Name added N new cards to *Deck*", with **Add the new
    cards** / **No thanks**. The gift row's badge reads "+N".
  - Merge offer with nothing new (the recipient already has every card):
    the gift row has no badge, and the pane says "You already have every
    card in *Deck*" with a single **Got it**. Got it adopts the offer, which
    copies nothing, so it leaves the list. (#346)
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
```

- [ ] **Step 3: Commit**

```bash
git add docs/superpowers/specs/2026-09-23-on-flash-ui-overhaul-design.md
git commit -m "docs(flash): record sharing UX rules in UI overhaul spec §6 (#304, #346)"
```

---

### Task 5: Full check, manual verification, PR

- [ ] **Step 1: Full check**

```bash
gofmt -l .                                              # prints nothing
go vet ./...
go run honnef.co/go/tools/cmd/staticcheck@v0.8.0 ./...
go mod tidy && git diff --exit-code go.mod go.sum
go test ./internal/arch/... -count=1
go test ./... -race -count=1
```
Expected: no output from `gofmt`, and `ok` for every package.

- [ ] **Step 2: Manual verification** (build, start the `onsuite` preview, and ask the user to sign in. It needs two accounts, e.g. `ilia` and a second one made with `./onsuite user add`.)

1. As B, create a deck "Spanish". As A, create "Spanish" with two cards and share it with B. As B, open the gift and click **Add to my decks**. The pane opens "Spanish (from A)" with the notice "“Spanish (from A)” is now in your decks.", and both decks are in the list.
2. As A, open the Share popover. It has one row for B, "added", and no Revoke. Share again: still one row, now "waiting" with Revoke. Revoke: the row goes back to "added".
3. As A, share again without adding cards. As B, the gift row has **no badge**. The pane reads "You already have every card in Spanish" with only **Got it**. Got it shows "“Spanish (from A)” is already up to date." and the gift row is gone.
4. As A, add a card and re-share. B's gift row shows "+1", and Add the new cards merges into "Spanish (from A)".
5. With JS disabled, Got it and a colliding Add both redirect to the deck. Check dark theme, 640px width, and that there are no console or CSP errors.

- [ ] **Step 3: Open the PR**

```bash
git push -u origin fix/304-sharing-ux
gh pr create --title "fix(flash): sharing UX — Shared with list, name collisions, up-to-date re-shares (#304, #346)" --body "$(cat <<'EOF'
## Summary
- **Shared with** (Share popover) shows one row per person, with the status of their latest offer. A re-share replaces it with "waiting". Revoked offers are ignored, so revoking a re-offer falls back to the person's last answer, and someone whose offers were all revoked isn't listed. This is done in SQL in `SharesForDeck` (`ROW_NUMBER()` per recipient, `created_at DESC, id DESC`).
- **Name collisions on adopt** no longer dead-end. Before, the click returned a 400 that HTMX didn't swap, so nothing happened. A first-time copy now takes the first free name of "Spanish", "Spanish (from alice)", "Spanish (from alice) (2)" and so on, shortening only the base so it stays within 120 characters. Merges are unaffected, since they find the copy by id. `AdoptShare` takes a `usernames map[int64]string` for the suffix: plain data, because a callback would run inside the transaction on the only DB connection.
- **Re-share with nothing new** has no "+0" badge. The pane says "You already have every card in *Deck*" with a single Got it (the adopt route, which copies nothing), and the notice reads "“*Deck*” is already up to date."
- Tests for a 0-card first-time share preview and a 0-new-cards merge (#346).
- Spec §6 records the three rules.

No migrations, no new dependencies, no CSS changes.

Closes #304 (bullets 2 and 5 here; 1, 3, 4, 6, 8 in #358; 7 obsolete per triage)
Closes #346

Plan: docs/superpowers/plans/2026-09-24-flash-sharing-ux.md

## Test plan
- [ ] `go test ./... -race -count=1`
- [ ] Manual checklist from the plan's Task 5
EOF
)"
```

---

## Self-review

- **Coverage:**
  - #304.2 → Task 1. The store test covers each rule: decline replaced by a re-offer, revoked-only recipient hidden, revoke falls back to decline, newest-first order, id tiebreak under a fixed clock, non-owner 404. The handler test covers one row through decline → re-share → adopt → re-share → revoke, with Revoke's action naming the pending share id.
  - #304.5 → Task 2. The pure naming table covers the suffix, the counter, the "(shared)" fallback, and truncation with trailing-space trimming. The store tests cover the "(2)" collision chain, a merge into the renamed copy, the max-length name and the nil map. The handler test covers 200 instead of 400, the notice and heading using the final name, and both decks present.
  - #346 → Task 3. It adds a store and a handler test for a 0-card first share: "0 cards", no samples or "A few of the cards", two buttons, "new" badge. It adds a store test for a 0-new merge (NewCardCount 0, CardCount 0). It adds a handler test for the badge-free row, the pane copy, Got it only, the up-to-date notice, the offer resolved and nothing copied twice, plus a "+1" regression check.
  - Spec §6 → Task 4. PR → Task 5. The notice for an ordinary merge and a first adopt is unchanged (`TestAdoptNoticeWordingForFirstAdoptAndMerge` still passes).
- **Placeholders:** none. Every step has complete code or an exact before/after replacement, plus exact commands and expected results. Task 2 Step 1's sed count (18) includes the one call Task 1 adds.
- **Type consistency:**
  - `AdoptShare(ctx, toUserID, shareID int64, usernames map[int64]string) (AdoptResult, error)` has one production caller, `adoptShareHandler`, which passes `byID` from `usernamesByID`. Tests pass `nil` or a literal map.
  - Task 1's new test uses the 3-arg form and is rewritten by Task 2's sed.
  - Task 3's store test is written after Task 2 and uses the 4-arg form directly.
  - `adoptedDeckName(base, sharer string, n int) string` is used only by `freeDeckName(ctx, *sql.Tx, userID int64, base, sharer string) (string, error)`.
  - `giftRow.UpToDate` is set in `buildDeckIndex` and read by `deck-list-items`. `giftView.UpToDate` is set in `giftPreview` and read by `deck-detail-gift`.
  - `SharesForDeck`'s signature is unchanged, so `shareContext` and `buildDeckIndex` need no edits.
  - New test names don't collide with existing ones: `TestSharesForDeckListsAllOffersNewestFirst` is replaced, not duplicated. `TestGiftPreviewOfAnEmptyDeck` (handler) and `TestSharePreviewOfAnEmptyDeck` (store) are distinct.
- **Ordering:** each task builds and passes on its own. Task 2 depends on nothing in Task 1 except the extra call site its sed counts. Task 3 uses Task 2's 4-arg `AdoptShare`. Task 4 is docs only.
- **Harness gotcha respected:** no handler test calls `s.Store.SetClock`. Only the Task 1 store test sets a clock, on `f.store`.
