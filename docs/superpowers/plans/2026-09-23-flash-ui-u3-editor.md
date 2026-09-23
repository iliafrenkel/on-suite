# ON Flash UI U3 — Card editor Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the plain card forms with a friendly editor: the two faces drawn as cards, a "Question and answer / Fill in the blank" toggle, a Make blank button, tag pills, image and sound drop zones in the same form, and "Save and add another".

**Architecture:** The media upload code is split into *read and check* (before any write) and *save* (after the card exists), so the create and update handlers can accept the same multipart form as the media route, with a bad file never leaving a half-saved card. A new `editor.partial.html` replaces the F1 forms that U2 moved into `cards.partial.html`. The type toggle is pure CSS (`:has()`); JS in `flash.js` adds Make blank, the tag-pill editor and drag-and-drop.

**Tech Stack:** Go, `html/template`, HTMX (`hx-encoding="multipart/form-data"`), CSS `:has()`, `flash.js`.

**Spec:** [docs/superpowers/specs/2026-09-23-on-flash-ui-overhaul-design.md](../specs/2026-09-23-on-flash-ui-overhaul-design.md) — §1.6, §4. Mockups: section 3, "Card editor".

**This is PR 3 of 6.** It requires U1 and U2 on `main`.

## Global Constraints

- Branch: `feat/flash-ui-u3` off an up-to-date `main` containing U1 and U2. Never push to `main`.
- Full check before each Go commit:
  ```bash
  gofmt -l .
  go vet ./...
  go run honnef.co/go/tools/cmd/staticcheck@v0.8.0 ./...
  go mod tidy && git diff --exit-code go.mod go.sum
  go test ./... -race -count=1
  ```
- No new dependencies. CSP: no inline `<script>`, no `style=`. CSP `img-src 'self'` means **no `blob:` image previews** — show the chosen file's *name*, not a thumbnail.
- Every `*.partial.html` is parsed into every Flash page; define each template name once, never `content`/`head` in a partial.
- `htmlassert` selectors: descendant chains of simple parts only (`tag`, `.class`, `#id`, `[attr]`, `[attr=value]`, or `tag` + one of those). No compound parts like `a.x[y]` — select by one, check the rest with `htmlassert.Attr`.
- An element with the `hidden` attribute must not be un-hidden by a CSS `display` rule: whenever you give such an element a `display` value, also add `.that-class[hidden] { display: none; }`.
- From U1/U2 (do not rename): `renderCardIndex`, `renderCardDetailWithList`, `cardPaneDetail`, `cardDetailView`, `newCardDetail`, `editCardDetail`, `cardBasePath`, `cardsURL`, `cardURL`, pane modes `card-new`/`card-edit`, templates `deck-detail-card-new`, `deck-detail-card-edit`, `deck-detail-card`, `flash-pane-back`; CSS `.deck-c-*`, `--deck`, `--flash-card-bg`, `.flash-pill`.
- Tests: `newServer(t)`, `s.Get`, `s.Post`, `s.PostHX`, `s.Submit`, `s.Do`, `s.CSRFToken`, `httpGet`, `itoa`, `onePNG` (a valid PNG, in `handlers_media_test.go`).
- Commits: Conventional Commits, scope `flash`.

---

## File map

| File | Change |
|---|---|
| `internal/apps/flash/handlers_media.go` | split `attachUpload` into `readCardUploads`/`readUpload` + `saveCardUploads`; `uploadCardMedia` uses them |
| `internal/apps/flash/handlers_cards.go` | create/update read uploads first, save them after; cloze ignores back; Save and add another; `newCardForm` reads `?type=&tags=&saved=` |
| `internal/apps/flash/flash.go` | body limits on the two card POST routes |
| `internal/apps/flash/templates/editor.partial.html` | **Create** — `card-editor`, `card-drop`, new `deck-detail-card-new`/`-edit` |
| `internal/apps/flash/templates/cards.partial.html` | remove the F1 forms and the opened card's media `<details>` |
| `internal/apps/flash/static/flash.js` | Make blank, tag-pill editor, drop zones, `enhance()` on load and `htmx:load` |
| `internal/ui/static/app.css` | editor CSS |
| Tests | `handlers_cards_test.go`, `handlers_editor_test.go` (new) |

---

### Task 1: Split media handling into check and save

**Files:**
- Modify: `internal/apps/flash/handlers_media.go`
- Test: existing `internal/apps/flash/handlers_media_test.go` (must keep passing unchanged)

**Interfaces:**
- Produces:
  ```go
  type pendingUpload struct { Kind, ContentType string; Data []byte }
  type cardUploads struct {
      Image, Audio             *pendingUpload // nil = no new file
      RemoveImage, RemoveAudio bool
  }
  func readCardUploads(w http.ResponseWriter, r *http.Request) (cardUploads, string) // "" = ok, else a user-facing message
  func readUpload(w http.ResponseWriter, r *http.Request, kind, field string, maxBytes int64) (*pendingUpload, string)
  func (a *App) saveCardUploads(ctx context.Context, userID, deckID, cardID int64, u cardUploads) error
  ```
- `readCardUploads` **must be the first thing** a handler does with the request body: it parses the form (multipart or not), so later `r.PostFormValue` calls read from the parsed form.

- [ ] **Step 1: Confirm the media tests pass before the refactor**

Run: `go test ./internal/apps/flash/ -run 'Upload|RemoveCard' -count=1`
Expected: PASS. These tests are the safety net for this task.

- [ ] **Step 2: Replace `attachUpload` and the loop in `uploadCardMedia`**

In `internal/apps/flash/handlers_media.go`, delete `attachUpload` and add:

```go
// pendingUpload is one checked, not-yet-saved media file from a card form.
type pendingUpload struct {
	Kind        string // MediaKindImage or MediaKindAudio
	ContentType string // sniffed, never the client's claim
	Data        []byte
}

// cardUploads is a card form's whole media part. It is read and checked
// before anything is written, so a bad file can never leave a
// half-updated card behind (UI overhaul spec §4).
type cardUploads struct {
	Image, Audio             *pendingUpload // nil = no new file for that kind
	RemoveImage, RemoveAudio bool
}

// readCardUploads parses a card form — multipart/form-data when it carries
// files, a plain urlencoded POST when it doesn't — and checks its optional
// "image"/"audio" parts and "remove_image"/"remove_audio" flags. It must be
// the first thing a handler does with the body. A non-empty string is a
// message for the person who submitted the form.
func readCardUploads(w http.ResponseWriter, r *http.Request) (cardUploads, string) {
	// ParseMultipartForm's argument is only a maxMemory hint, not a hard cap
	// on bytes read: without an outer limit it would read the entire body
	// (spilling to a temp file) before any size check below runs. The
	// budget covers one image plus one audio part in the same request.
	r.Body = http.MaxBytesReader(w, r.Body, MaxImageFetchBytes+MaxAudioFetchBytes)
	// A text-only or remove-only form may arrive urlencoded; ErrNotMultipart
	// is expected then — ParseMultipartForm still fills r.PostForm.
	if err := r.ParseMultipartForm(MaxAudioFetchBytes); err != nil && !errors.Is(err, http.ErrNotMultipart) {
		return cardUploads{}, "That upload could not be read."
	}

	u := cardUploads{
		RemoveImage: r.PostFormValue("remove_image") != "",
		RemoveAudio: r.PostFormValue("remove_audio") != "",
	}
	var msg string
	if !u.RemoveImage {
		if u.Image, msg = readUpload(w, r, MediaKindImage, "image", MaxImageFetchBytes); msg != "" {
			return cardUploads{}, msg
		}
	}
	if !u.RemoveAudio {
		if u.Audio, msg = readUpload(w, r, MediaKindAudio, "audio", MaxAudioFetchBytes); msg != "" {
			return cardUploads{}, msg
		}
	}
	return u, ""
}

// readUpload reads and checks one optional file part. No part at all (or
// an empty one — a file input left blank) is not an error: it returns nil
// and "".
func readUpload(w http.ResponseWriter, r *http.Request, kind, field string, maxBytes int64) (*pendingUpload, string) {
	file, header, err := r.FormFile(field)
	if err != nil {
		return nil, ""
	}
	defer func() { _ = file.Close() }()
	if header.Size == 0 {
		return nil, ""
	}

	tooBig := "That file is larger than the " + strconv.FormatInt(maxBytes>>20, 10) + "MB limit."
	if header.Size > maxBytes {
		return nil, tooBig
	}
	data, err := io.ReadAll(http.MaxBytesReader(w, file, maxBytes))
	if err != nil {
		return nil, tooBig
	}
	ct := http.DetectContentType(data)
	if !contentTypeMatchesKind(ct, kind) {
		return nil, "That file does not look like " + kind + " content."
	}
	return &pendingUpload{Kind: kind, ContentType: ct, Data: data}, ""
}

// saveCardUploads applies checked uploads to a card that now exists:
// removals first win over a new file of the same kind (the form offers
// either, not both).
func (a *App) saveCardUploads(ctx context.Context, userID, deckID, cardID int64, u cardUploads) error {
	for _, m := range []struct {
		kind   string
		remove bool
		up     *pendingUpload
	}{
		{MediaKindImage, u.RemoveImage, u.Image},
		{MediaKindAudio, u.RemoveAudio, u.Audio},
	} {
		switch {
		case m.remove:
			if err := a.store.SetCardMedia(ctx, userID, deckID, cardID, m.kind, nil); err != nil {
				return err
			}
		case m.up != nil:
			hash, err := a.store.SaveMediaUpload(ctx, m.up.Kind, m.up.ContentType, m.up.Data, a.store.now())
			if err != nil {
				return err
			}
			if err := a.store.SetCardMedia(ctx, userID, deckID, cardID, m.kind, &hash); err != nil {
				return err
			}
		}
	}
	return nil
}
```

In `uploadCardMedia`, replace everything from the `r.Body = http.MaxBytesReader(...)` line down to (and including) the closing brace of the `for _, spec := range ...` loop with:

```go
	uploads, errMsg := readCardUploads(w, r)
	if errMsg != "" {
		a.renderCardIndex(w, r, userID, deck, http.StatusBadRequest,
			a.viewCardDetailWithMediaError(r, userID, deck, c, errMsg))
		return
	}
	if err := a.saveCardUploads(r.Context(), userID, deck.ID, cardID, uploads); err != nil {
		a.fail(w, r, err)
		return
	}
```

Keep the doc comment on `uploadCardMedia`, and add one sentence to it: `The same form fields are accepted by the card create/update routes (UI overhaul U3), which is why the checking lives in readCardUploads.`

- [ ] **Step 3: Run the media tests again**

Run: `go test ./internal/apps/flash/ -count=1`
Expected: PASS, unchanged.

- [ ] **Step 4: Commit**

```bash
git add internal/apps/flash/handlers_media.go
git commit -m "refactor(flash): check card uploads before saving them"
```

---

### Task 2: One card form for text and media, plus "Save and add another"

**Files:**
- Modify: `internal/apps/flash/handlers_cards.go`, `internal/apps/flash/flash.go`
- Test: create `internal/apps/flash/handlers_editor_test.go`

**Interfaces:**
- Consumes: Task 1.
- Produces:
  - `cardDetailView.Notice string` (a success message shown above the form).
  - `editCardDetail` fills `ImageMediaURL`/`AudioMediaURL` from the card.
  - `const cardFormMaxBytes = MaxImageFetchBytes + MaxAudioFetchBytes`.
  - `func newCardFormURL(deckID int64, cardType, tags string) string` → `/flash/{id}/cards/new?saved=1&tags=…&type=…`.
  - `const cardSavedNotice = "Card saved. Add the next one."`

- [ ] **Step 1: Write the failing tests**

Create `internal/apps/flash/handlers_editor_test.go`:

```go
// internal/apps/flash/handlers_editor_test.go
package flash_test

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/iliafrenkel/on-suite/internal/apps/flash"
	"github.com/iliafrenkel/on-suite/internal/apptest"
	"github.com/iliafrenkel/on-suite/internal/htmlassert"
	"github.com/iliafrenkel/on-suite/internal/platform/web"
)

// postCardForm submits a card form as multipart/form-data over HTMX — the
// way the U3 editor does — with optional file parts (field name → bytes).
func postCardForm(t *testing.T, s *apptest.Server[*flash.Store], sess *apptest.Session, path string, fields url.Values, files map[string][]byte) *httptest.ResponseRecorder {
	t.Helper()
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	if err := mw.WriteField(web.CSRFFormField, s.CSRFToken(t, sess)); err != nil {
		t.Fatal(err)
	}
	for name, values := range fields {
		for _, v := range values {
			if err := mw.WriteField(name, v); err != nil {
				t.Fatal(err)
			}
		}
	}
	for name, data := range files {
		part, err := mw.CreateFormFile(name, name+".bin")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := part.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	if err := mw.Close(); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, path, &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("HX-Request", "true")
	return s.Do(t, sess, req)
}

func TestCreateCardWithImageInOneForm(t *testing.T) {
	s := newServer(t)
	deck, err := s.Store.CreateDeck(t.Context(), s.Alice.User.ID, "Animals", "")
	if err != nil {
		t.Fatal(err)
	}
	rec := postCardForm(t, s, s.Alice, "/flash/"+itoa(deck.ID)+"/cards/new",
		url.Values{"card_type": {"basic"}, "front": {"cat"}, "back": {"gato"}},
		map[string][]byte{"image": onePNG})
	if rec.Code != http.StatusCreated {
		t.Fatalf("create = %d; body: %s", rec.Code, rec.Body.String())
	}
	cards, err := s.Store.ListCards(t.Context(), s.Alice.User.ID, deck.ID)
	if err != nil || len(cards) != 1 {
		t.Fatalf("ListCards = %v, %v", cards, err)
	}
	if cards[0].ImageHash == nil {
		t.Error("the image sent with the card form was not attached")
	}
}

func TestCreateCardWithBadImageKeepsTheFormAndCreatesNothing(t *testing.T) {
	s := newServer(t)
	deck, err := s.Store.CreateDeck(t.Context(), s.Alice.User.ID, "Animals", "")
	if err != nil {
		t.Fatal(err)
	}
	rec := postCardForm(t, s, s.Alice, "/flash/"+itoa(deck.ID)+"/cards/new",
		url.Values{"card_type": {"basic"}, "front": {"cat"}, "back": {"gato"}},
		map[string][]byte{"image": []byte("<html>not an image</html>")})
	if rec.Code != http.StatusOK {
		t.Fatalf("bad image over HTMX = %d, want 200 with the form re-rendered", rec.Code)
	}
	doc := htmlassert.Parse(t, rec.Body.String())
	doc.MustHave("#card-detail-new .notice-error")
	front := doc.MustHave(`#card-detail-new textarea[name=front]`)
	if got := htmlassert.Text(front); got != "cat" {
		t.Errorf("front textarea = %q, want the typed text kept", got)
	}
	if cards, _ := s.Store.ListCards(t.Context(), s.Alice.User.ID, deck.ID); len(cards) != 0 {
		t.Errorf("a rejected upload still created %d card(s)", len(cards))
	}
}

func TestUpdateCardCanRemoveItsImage(t *testing.T) {
	s := newServer(t)
	deck, err := s.Store.CreateDeck(t.Context(), s.Alice.User.ID, "Animals", "")
	if err != nil {
		t.Fatal(err)
	}
	rec := postCardForm(t, s, s.Alice, "/flash/"+itoa(deck.ID)+"/cards/new",
		url.Values{"card_type": {"basic"}, "front": {"cat"}, "back": {"gato"}},
		map[string][]byte{"image": onePNG})
	if rec.Code != http.StatusCreated {
		t.Fatalf("create = %d", rec.Code)
	}
	rec = postCardForm(t, s, s.Alice, "/flash/"+itoa(deck.ID)+"/cards/1",
		url.Values{"card_type": {"basic"}, "front": {"cat"}, "back": {"gato"}, "remove_image": {"1"}}, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("update = %d; body: %s", rec.Code, rec.Body.String())
	}
	c, err := s.Store.CardByID(t.Context(), s.Alice.User.ID, deck.ID, 1)
	if err != nil {
		t.Fatal(err)
	}
	if c.ImageHash != nil {
		t.Error("remove_image on the edit form did not remove the image")
	}
}

func TestClozeCardIgnoresAStaleBack(t *testing.T) {
	s := newServer(t)
	deck, err := s.Store.CreateDeck(t.Context(), s.Alice.User.ID, "Geography", "")
	if err != nil {
		t.Fatal(err)
	}
	// The editor hides the Back face for fill-in-the-blank cards, but a
	// value typed before switching type still submits; it must not trip
	// ValidateCard's "leave the back empty" rule.
	s.Submit(t, s.Alice, "/flash/"+itoa(deck.ID)+"/cards/new",
		url.Values{"card_type": {"cloze"}, "front": {"{{c1::Paris}} is in France."}, "back": {"left over"}},
		"/flash/"+itoa(deck.ID)+"/cards/1")
	c, err := s.Store.CardByID(t.Context(), s.Alice.User.ID, deck.ID, 1)
	if err != nil {
		t.Fatal(err)
	}
	if c.Back != "" {
		t.Errorf("cloze card Back = %q, want empty", c.Back)
	}
}

func TestSaveAndAddAnotherOverHTMX(t *testing.T) {
	s := newServer(t)
	deck, err := s.Store.CreateDeck(t.Context(), s.Alice.User.ID, "Spanish", "")
	if err != nil {
		t.Fatal(err)
	}
	rec := s.PostHX(t, s.Alice, "/flash/"+itoa(deck.ID)+"/cards/new", url.Values{
		"card_type": {"basic"}, "front": {"pan"}, "back": {"bread"}, "tags": {"food"}, "next": {"new"},
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("save and add another = %d; body: %s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("HX-Push-Url"); got != "/flash/"+itoa(deck.ID)+"/cards/new" {
		t.Errorf("HX-Push-Url = %q", got)
	}
	doc := htmlassert.Parse(t, rec.Body.String())
	saved := doc.MustHave("#card-detail-new .flash-editor-saved")
	if !strings.Contains(htmlassert.Text(saved), "Card saved") {
		t.Errorf("notice = %q", htmlassert.Text(saved))
	}
	if got := htmlassert.Text(doc.MustHave(`#card-detail-new textarea[name=front]`)); got != "" {
		t.Errorf("front = %q, want an empty form for the next card", got)
	}
	tags := doc.MustHave(`#card-detail-new input[name=tags]`)
	if v, _ := htmlassert.Attr(tags, "value"); v != "food" {
		t.Errorf("tags = %q, want them kept for the next card", v)
	}
	if cards, _ := s.Store.ListCards(t.Context(), s.Alice.User.ID, deck.ID); len(cards) != 1 {
		t.Errorf("ListCards has %d cards, want the first one saved", len(cards))
	}
}

func TestSaveAndAddAnotherWithoutJS(t *testing.T) {
	s := newServer(t)
	deck, err := s.Store.CreateDeck(t.Context(), s.Alice.User.ID, "Spanish", "")
	if err != nil {
		t.Fatal(err)
	}
	want := "/flash/" + itoa(deck.ID) + "/cards/new?saved=1&tags=food&type=cloze"
	s.Submit(t, s.Alice, "/flash/"+itoa(deck.ID)+"/cards/new", url.Values{
		"card_type": {"cloze"}, "front": {"{{c1::pan}} means bread"}, "tags": {"food"}, "next": {"new"},
	}, want)

	doc := s.Get(t, s.Alice, want)
	doc.MustHave("#card-detail-new .flash-editor-saved")
	cloze := doc.MustHave(`#card-detail-new input[value=cloze]`)
	if _, ok := htmlassert.Attr(cloze, "checked"); !ok {
		t.Error("the card type was not carried over to the next card")
	}
}
```

- [ ] **Step 2: Run to verify they fail**

Run: `go test ./internal/apps/flash/ -run 'OneForm|BadImage|RemoveItsImage|StaleBack|SaveAndAddAnother' -count=1`
Expected: FAIL (image ignored, no notice, cloze back rejected). `TestCreateCardWithBadImage…` may pass the "creates nothing" part by accident — the notice/front assertions fail.

- [ ] **Step 3: Raise the body limit on the two card POST routes**

In `internal/apps/flash/flash.go`, add near the top (after `mediaFetchConcurrency`):

```go
// cardFormMaxBytes is the body budget for the card create/update forms,
// which carry an optional image and sound file since UI overhaul U3 — the
// same budget as the media upload route below.
const cardFormMaxBytes = MaxImageFetchBytes + MaxAudioFetchBytes
```

Replace the two lines

```go
	r.HandleFunc("POST /{deckID}/cards/new", a.createCard)
```
```go
	r.HandleFunc("POST /{deckID}/cards/{cardID}", a.updateCard)
```

with (keep each at its current position in `Mount`):

```go
	// The card form carries optional media since UI overhaul U3, so it
	// needs the same raised body cap as the media upload route — see that
	// route's comment below for why both RegisterBodyLimit and LimitBody
	// are needed.
	r.RegisterBodyLimit("POST /{deckID}/cards/new", cardFormMaxBytes)
	r.Handle("POST /{deckID}/cards/new", web.LimitBody(cardFormMaxBytes)(http.HandlerFunc(a.createCard)))
```

```go
	r.RegisterBodyLimit("POST /{deckID}/cards/{cardID}", cardFormMaxBytes)
	r.Handle("POST /{deckID}/cards/{cardID}", web.LimitBody(cardFormMaxBytes)(http.HandlerFunc(a.updateCard)))
```

- [ ] **Step 4: Update the card handlers**

In `internal/apps/flash/handlers_cards.go`:

Add to `cardDetailView` (after `Error string`):

```go
	Notice string // a success message above the form ("Card saved…")
```

Add near `cardBasePath`:

```go
// cardSavedNotice is shown above the empty form after "Save and add
// another".
const cardSavedNotice = "Card saved. Add the next one."

// newCardFormURL is the new-card form pre-filled for the next card after
// "Save and add another" without JavaScript: same type, same tags, and the
// saved notice.
func newCardFormURL(deckID int64, cardType, tags string) string {
	v := url.Values{"saved": {"1"}, "type": {cardType}}
	if tags != "" {
		v.Set("tags", tags)
	}
	return cardBasePath(deckID) + "new?" + v.Encode()
}
```

Make `editCardDetail` fill the media URLs (the editor shows current media):

```go
func (a *App) editCardDetail(r *http.Request, d Deck, c Card, errMsg, cardType, front, back, notes, tags string) cardDetailView {
	return cardDetailView{
		Mode: cardModeEdit, Deck: d, Card: c, CardTypeValue: cardType, FrontValue: front, BackValue: back, NotesValue: notes,
		TagsValue: tags, Error: errMsg, CSRFToken: web.CSRFToken(r.Context()),
		ImageMediaURL: mediaURL(c.ImageHash), AudioMediaURL: mediaURL(c.AudioHash),
	}
}
```

Replace `newCardForm`:

```go
func (a *App) newCardForm(w http.ResponseWriter, r *http.Request) {
	userID, ok := a.userID(w, r)
	if !ok {
		return
	}
	deck, ok := a.cardDeck(w, r, userID)
	if !ok {
		return
	}
	q := r.URL.Query()
	cardType := q.Get("type")
	if !isKnownCardType(cardType) {
		cardType = CardTypeBasic
	}
	detail := a.newCardDetail(r, deck, "", cardType, "", "", "", q.Get("tags"))
	if q.Get("saved") != "" {
		detail.Notice = cardSavedNotice
	}
	a.renderCardIndex(w, r, userID, deck, http.StatusOK, detail)
}
```

Replace `createCard`:

```go
func (a *App) createCard(w http.ResponseWriter, r *http.Request) {
	userID, ok := a.userID(w, r)
	if !ok {
		return
	}
	deck, ok := a.cardDeck(w, r, userID)
	if !ok {
		return
	}
	// First: parses the (possibly multipart) form and checks any files.
	uploads, uploadErr := readCardUploads(w, r)

	cardType := r.PostFormValue("card_type")
	front := r.PostFormValue("front")
	back := r.PostFormValue("back")
	notes := r.PostFormValue("notes")
	tags := r.PostFormValue("tags")
	if cardType == CardTypeCloze {
		// The editor hides Back for fill-in-the-blank; anything typed there
		// before switching type is not part of the card.
		back = ""
	}
	reject := func(msg string) {
		a.renderCardIndex(w, r, userID, deck, http.StatusBadRequest, a.newCardDetail(r, deck, msg, cardType, front, back, notes, tags))
	}

	if uploadErr != "" {
		reject(uploadErr)
		return
	}
	tagList := parseTagList(tags)
	if err := ValidateCard(cardType, front, back); err != nil {
		reject(userMessage(err))
		return
	}
	if err := ValidateTagNames(tagList); err != nil {
		reject(userMessage(err))
		return
	}
	c, err := a.store.CreateCard(r.Context(), userID, deck.ID, cardType, front, back, notes)
	if err != nil {
		if errors.Is(err, ErrInvalid) {
			reject(userMessage(err))
			return
		}
		a.fail(w, r, err)
		return
	}
	if err := a.store.SetCardTags(r.Context(), userID, c.ID, tagList); err != nil {
		a.fail(w, r, err)
		return
	}
	if err := a.saveCardUploads(r.Context(), userID, deck.ID, c.ID, uploads); err != nil {
		a.fail(w, r, err)
		return
	}

	if r.PostFormValue("next") == "new" {
		if !web.IsHTMX(r) {
			http.Redirect(w, r, newCardFormURL(deck.ID, cardType, tags), http.StatusSeeOther)
			return
		}
		w.Header().Set("HX-Push-Url", cardBasePath(deck.ID)+"new")
		next := a.newCardDetail(r, deck, "", cardType, "", "", "", tags)
		next.Notice = cardSavedNotice
		a.renderCardDetailWithList(w, r, userID, deck, http.StatusCreated, next)
		return
	}

	if !web.IsHTMX(r) {
		http.Redirect(w, r, cardBasePath(deck.ID)+strconv.FormatInt(c.ID, 10), http.StatusSeeOther)
		return
	}
	w.Header().Set("HX-Push-Url", cardBasePath(deck.ID)+strconv.FormatInt(c.ID, 10))
	saved, err := a.store.CardByID(r.Context(), userID, deck.ID, c.ID)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	a.renderCardDetailWithList(w, r, userID, deck, http.StatusCreated, a.viewCardDetail(r, userID, deck, saved))
}
```

(`saved` re-reads the card so its just-attached media shows.)

In `updateCard`, make the same three changes:

1. Right after loading `c` with `CardByID`, add `uploads, uploadErr := readCardUploads(w, r)` **before** the first `r.PostFormValue` call.
2. After reading the fields, add the `if cardType == CardTypeCloze { back = "" }` block, and before `parseTagList`:
   ```go
	if uploadErr != "" {
		a.renderCardIndex(w, r, userID, deck, http.StatusBadRequest, a.editCardDetail(r, deck, c, uploadErr, cardType, front, back, notes, tags))
		return
	}
   ```
3. After `SetCardTags` succeeds, add:
   ```go
	if err := a.saveCardUploads(r.Context(), userID, deck.ID, id, uploads); err != nil {
		a.fail(w, r, err)
		return
	}
	updated, err = a.store.CardByID(r.Context(), userID, deck.ID, id)
	if err != nil {
		a.fail(w, r, err)
		return
	}
   ```

- [ ] **Step 5: Run the tests**

Run: `go test ./internal/apps/flash/ -count=1`
Expected: the new tests still FAIL on the template assertions (`.flash-editor-saved`, `#card-detail-new input[value=cloze]`) — the editor template arrives in Task 3 — but no *other* test regresses. `TestCreateCardWithImageInOneForm`, `TestUpdateCardCanRemoveItsImage` and `TestClozeCardIgnoresAStaleBack` should already PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/apps/flash/handlers_cards.go internal/apps/flash/flash.go internal/apps/flash/handlers_editor_test.go
git commit -m "feat(flash): card form takes media and supports save-and-add-another"
```

---

### Task 3: The editor template

**Files:**
- Create: `internal/apps/flash/templates/editor.partial.html`
- Modify: `internal/apps/flash/templates/cards.partial.html`
- Test: `internal/apps/flash/handlers_editor_test.go`

**Interfaces:**
- Consumes: `cardDetailView` (with `Notice`, `ImageMediaURL`, `AudioMediaURL`), `deckDetailView.CardForm`.
- Produces: templates `card-editor` (dict: `Form`, `FormID`, `Action`, `Heading`, `IDSuffix`, `CancelURL`, `CancelLabel`, `ShowAddAnother`), `card-drop`, and new definitions of `deck-detail-card-new` / `deck-detail-card-edit`. JS hooks: `.flash-make-blank[data-target]`, `input[data-tag-input]`, `label[data-drop]`, `.flash-drop-file`.

- [ ] **Step 1: Add the structure tests**

Append to `internal/apps/flash/handlers_editor_test.go`:

```go
func TestNewCardEditorStructure(t *testing.T) {
	s := newServer(t)
	deck, err := s.Store.CreateDeck(t.Context(), s.Alice.User.ID, "Spanish", "")
	if err != nil {
		t.Fatal(err)
	}
	doc := s.Get(t, s.Alice, "/flash/"+itoa(deck.ID)+"/cards/new")
	form := doc.MustHave("form#card-detail-new")
	if v, _ := htmlassert.Attr(form, "enctype"); v != "multipart/form-data" {
		t.Errorf("enctype = %q", v)
	}
	if v, _ := htmlassert.Attr(form, "hx-encoding"); v != "multipart/form-data" {
		t.Errorf("hx-encoding = %q", v)
	}
	if n := len(doc.QueryAll(`#card-detail-new input[name=card_type]`)); n != 2 {
		t.Errorf("%d card_type radios, want 2", n)
	}
	doc.MustHave(`.flash-editor-front textarea[name=front]`)
	doc.MustHave(`.flash-editor-back textarea[name=back]`)
	doc.MustHave(`label.flash-drop input[name=image]`)
	doc.MustHave(`label.flash-drop input[name=audio]`)
	doc.MustHave(`#card-detail-new input[data-tag-input]`)
	next := doc.MustHave(`#card-detail-new button[name=next]`)
	if v, _ := htmlassert.Attr(next, "value"); v != "new" {
		t.Errorf("Save and add another value = %q", v)
	}
	blank := doc.MustHave(`.flash-make-blank`)
	if _, ok := htmlassert.Attr(blank, "hidden"); !ok {
		t.Error("Make blank must start hidden; flash.js reveals it")
	}
}

func TestEditCardEditorOffersToRemoveExistingImage(t *testing.T) {
	s := newServer(t)
	deck, err := s.Store.CreateDeck(t.Context(), s.Alice.User.ID, "Animals", "")
	if err != nil {
		t.Fatal(err)
	}
	postCardForm(t, s, s.Alice, "/flash/"+itoa(deck.ID)+"/cards/new",
		url.Values{"card_type": {"basic"}, "front": {"cat"}, "back": {"gato"}},
		map[string][]byte{"image": onePNG})
	doc := s.Get(t, s.Alice, "/flash/"+itoa(deck.ID)+"/cards/edit/1")
	doc.MustHave(`form#card-detail-edit input[name=remove_image]`)
	doc.MustNotHave(`form#card-detail-edit input[name=remove_audio]`)
	doc.MustNotHave(`form#card-detail-edit button[name=next]`)
}

func TestOpenedCardHasNoSeparateMediaForm(t *testing.T) {
	s := newServer(t)
	deck, err := s.Store.CreateDeck(t.Context(), s.Alice.User.ID, "Animals", "")
	if err != nil {
		t.Fatal(err)
	}
	c, err := s.Store.CreateCard(t.Context(), s.Alice.User.ID, deck.ID, flash.CardTypeBasic, "cat", "gato", "")
	if err != nil {
		t.Fatal(err)
	}
	doc := s.Get(t, s.Alice, "/flash/"+itoa(deck.ID)+"/cards/"+itoa(c.ID))
	doc.MustNotHave("#card-media-form")
}
```

- [ ] **Step 2: Run to verify they fail**

Run: `go test ./internal/apps/flash/ -run 'Editor|SeparateMediaForm|SaveAndAddAnother' -count=1`
Expected: FAIL.

- [ ] **Step 3: Remove the F1 forms and media details from `cards.partial.html`**

In `internal/apps/flash/templates/cards.partial.html`, delete:
- the comment block starting `{{/* The card forms below are F1's, moved here unchanged…`,
- the `card-form-fields`, `deck-detail-card-new` and `deck-detail-card-edit` definitions,
- inside `deck-detail-card`, the comment `{{/* F4's media form, kept until U3 … */}}` and the whole `<details class="flash-media-details"> … </details>` block.

- [ ] **Step 4: Create `editor.partial.html`**

```html
{{/* internal/apps/flash/templates/editor.partial.html — the card editor
     (UI overhaul spec §4). One multipart form carries the text, the tags
     and any media. The type toggle is CSS-only (:has on the checked
     radio); flash.js adds Make blank, the tag-pill editor and
     drag-and-drop, and everything works without it. */}}

{{/* card-editor draws the form. Pass a dict:
       Form           — the cardDetailView (values, Error, Notice, media URLs)
       FormID         — "card-detail-new" or "card-detail-edit"
       Action         — the POST URL
       Heading        — "New card" / "Edit card"
       IDSuffix       — keeps element ids unique
       CancelURL, CancelLabel — where Cancel and the back button go
       ShowAddAnother — show "Save and add another" (new cards only) */}}
{{define "card-editor"}}
<form class="flash-editor deck-c-{{.Form.Deck.Color}}" id="{{.FormID}}" method="post" action="{{.Action}}" enctype="multipart/form-data"
      hx-post="{{.Action}}" hx-encoding="multipart/form-data" hx-target="#deck-detail" hx-swap="innerHTML">
	<input type="hidden" name="{{csrfField}}" value="{{.Form.CSRFToken}}">
	<div class="notes-toolbar flash-deck-toolbar">
		<a class="toolbar-btn" href="{{.CancelURL}}" hx-get="{{.CancelURL}}" hx-target="#deck-detail" hx-push-url="true">{{ticon "arrow-left"}}{{.CancelLabel}}</a>
	</div>
	<h1>{{.Heading}}</h1>
	{{with .Form.Notice}}<div class="notice flash-editor-saved" role="status">{{.}}</div>{{end}}
	{{with .Form.Error}}<div class="notice notice-error" role="alert">{{.}}</div>{{end}}

	<fieldset class="flash-type-toggle">
		<legend class="visually-hidden">Card type</legend>
		<input type="radio" class="visually-hidden" name="card_type" value="basic" id="card-type-{{.IDSuffix}}-basic"{{if ne .Form.CardTypeValue "cloze"}} checked{{end}}>
		<label for="card-type-{{.IDSuffix}}-basic">Question and answer</label>
		<input type="radio" class="visually-hidden" name="card_type" value="cloze" id="card-type-{{.IDSuffix}}-cloze"{{if eq .Form.CardTypeValue "cloze"}} checked{{end}}>
		<label for="card-type-{{.IDSuffix}}-cloze">Fill in the blank</label>
	</fieldset>

	<div class="flash-editor-faces">
		<div class="flash-editor-face flash-editor-front">
			<label for="card-front-{{.IDSuffix}}">
				<span class="flash-when-basic">Front: the question</span>
				<span class="flash-when-cloze">The sentence, with a blank</span>
			</label>
			<textarea id="card-front-{{.IDSuffix}}" name="front" rows="4" required>{{.Form.FrontValue}}</textarea>
			<button type="button" class="toolbar-btn flash-make-blank" data-target="card-front-{{.IDSuffix}}" hidden>Make blank</button>
		</div>
		<div class="flash-editor-face flash-editor-back">
			<label for="card-back-{{.IDSuffix}}">Back: the answer</label>
			<textarea id="card-back-{{.IDSuffix}}" name="back" rows="4">{{.Form.BackValue}}</textarea>
		</div>
	</div>
	<p class="faint flash-when-cloze">Select a word and press Make blank — or type it like {{"{{"}}c1::Paris}}.</p>

	<div class="field">
		<label for="card-notes-{{.IDSuffix}}">Extra note (shown after the answer)</label>
		<textarea id="card-notes-{{.IDSuffix}}" name="notes" rows="2">{{.Form.NotesValue}}</textarea>
	</div>
	<div class="field">
		<label for="card-tags-{{.IDSuffix}}">Tags</label>
		<input id="card-tags-{{.IDSuffix}}" type="text" name="tags" value="{{.Form.TagsValue}}" placeholder="food, places" data-tag-input>
	</div>

	<div class="flash-drops">
		{{template "card-drop" (dict "Kind" "image" "ID" (printf "card-image-%v" .IDSuffix) "Accept" "image/*" "Prompt" "Drop a picture here, or click to choose one" "Current" .Form.ImageMediaURL "RemoveLabel" "Remove the picture")}}
		{{template "card-drop" (dict "Kind" "audio" "ID" (printf "card-audio-%v" .IDSuffix) "Accept" "audio/*" "Prompt" "Drop a sound here, or click to choose one" "Current" .Form.AudioMediaURL "RemoveLabel" "Remove the sound")}}
	</div>

	<div class="row flash-editor-actions">
		<button type="submit" class="primary">Save card</button>
		{{if .ShowAddAnother}}<button type="submit" class="button" name="next" value="new">Save and add another</button>{{end}}
		<a class="toolbar-btn" href="{{.CancelURL}}" hx-get="{{.CancelURL}}" hx-target="#deck-detail" hx-push-url="true">Cancel</a>
	</div>
</form>
{{end}}

{{/* card-drop is one media drop zone: a <label> around a visually hidden
     file input, so clicking anywhere on it opens the file picker, and
     flash.js makes it accept a dropped file too. Pass a dict: Kind
     ("image"/"audio", also the field name), ID, Accept, Prompt, Current
     (the attached file's URL, or ""), RemoveLabel. The audio player and the
     remove checkbox sit outside the label: controls inside a label would
     also open the picker. */}}
{{define "card-drop"}}
<div class="flash-drop-field">
	<label class="flash-drop" for="{{.ID}}" data-drop>
		<input type="file" class="visually-hidden" id="{{.ID}}" name="{{.Kind}}" accept="{{.Accept}}">
		{{if and .Current (eq .Kind "image")}}<img class="flash-drop-preview" src="{{.Current}}" alt="Current picture">{{end}}
		<span class="flash-drop-text">{{.Prompt}}</span>
		<span class="flash-drop-file" aria-live="polite"></span>
	</label>
	{{if .Current}}
	{{if eq .Kind "audio"}}<audio controls src="{{.Current}}"></audio>{{end}}
	<label class="flash-drop-remove"><input type="checkbox" name="remove_{{.Kind}}" value="1"> {{.RemoveLabel}}</label>
	{{end}}
</div>
{{end}}

{{define "deck-detail-card-new"}}
{{template "card-editor" (dict
	"Form" .CardForm
	"FormID" "card-detail-new"
	"Action" (printf "/flash/%d/cards/new" .Deck.ID)
	"Heading" "New card"
	"IDSuffix" "new"
	"CancelURL" (printf "/flash/%d/cards/" .Deck.ID)
	"CancelLabel" "All cards"
	"ShowAddAnother" true)}}
{{end}}

{{define "deck-detail-card-edit"}}
{{template "card-editor" (dict
	"Form" .CardForm
	"FormID" "card-detail-edit"
	"Action" (printf "/flash/%d/cards/%d" .Deck.ID .CardForm.Card.ID)
	"Heading" "Edit card"
	"IDSuffix" .CardForm.Card.ID
	"CancelURL" (printf "/flash/%d/cards/%d" .Deck.ID .CardForm.Card.ID)
	"CancelLabel" "Back to the card"
	"ShowAddAnother" false)}}
{{end}}
```

> Go templates allow a multi-line action inside `(...)`/`{{ }}` as long as
> it is one pipeline, as above. If your Go version rejects it, put the
> `dict` call on one line.
>
> `{{"{{"}}` prints a literal `{{`; the following `c1::Paris}}` is plain
> text because `}}` outside an action is not special.

- [ ] **Step 5: Run the tests**

Run: `go test ./internal/apps/flash/ -count=1`
Expected: PASS — all of `handlers_editor_test.go` and every existing test (`TestCreateCardValidation`, the overlong-tag tests, `#card-detail-new`/`#card-detail-edit` ids are unchanged).

- [ ] **Step 6: Commit**

```bash
git add -A internal/apps/flash/templates internal/apps/flash/handlers_editor_test.go
git commit -m "feat(flash): card-face editor with type toggle and media drop zones"
```

---

### Task 4: Editor JavaScript

**Files:**
- Modify: `internal/apps/flash/static/flash.js`

**Interfaces:**
- Consumes: `.flash-make-blank[data-target]`, `input[data-tag-input]`, `label[data-drop]` with a file input and `.flash-drop-file` inside.
- Produces: `enhance(root)` — idempotent; runs on startup and on every `htmx:load` (so forms swapped into the pane get it too). Each enhanced element is marked `data-enhanced="1"`.

- [ ] **Step 1: Add the editor code**

Add a line to the header comment: `// Card editor (U3): Make blank, tag pills, and drag-and-drop for media.`

Inside the IIFE, **before** the `document.addEventListener("keydown", …)` call, add:

```js
	// ---- Card editor (U3) -------------------------------------------------

	// nextClozeNumber is one more than the highest {{cN::…}} already in text.
	function nextClozeNumber(text) {
		var max = 0;
		var re = /\{\{c(\d+)::/g;
		var m;
		while ((m = re.exec(text)) !== null) {
			var n = parseInt(m[1], 10);
			if (n > max) max = n;
		}
		return max + 1;
	}

	// makeBlank wraps the textarea's selection (or a placeholder word) in
	// the next cloze marker and selects the word, ready to overtype.
	function makeBlank(btn) {
		var ta = document.getElementById(btn.getAttribute("data-target"));
		if (!ta) return;
		var start = ta.selectionStart, end = ta.selectionEnd;
		var word = ta.value.slice(start, end) || "answer";
		var open = "{{c" + nextClozeNumber(ta.value) + "::";
		ta.value = ta.value.slice(0, start) + open + word + "}}" + ta.value.slice(end);
		ta.focus();
		ta.setSelectionRange(start + open.length, start + open.length + word.length);
		ta.dispatchEvent(new Event("input", { bubbles: true }));
	}

	// initTagInput turns the comma-separated tags field into pills. The
	// original input stays in the form (as type=hidden) and keeps the same
	// comma-separated value the server already parses.
	function initTagInput(input) {
		var names = input.value.split(",").map(function (s) { return s.trim(); }).filter(Boolean);
		var box = document.createElement("div");
		box.className = "flash-tag-editor";
		var entry = document.createElement("input");
		entry.type = "text";
		entry.className = "flash-tag-entry";
		entry.placeholder = names.length ? "" : (input.placeholder || "Add a tag");
		entry.setAttribute("aria-label", "Add a tag");
		if (input.id) {
			// Keep the field's <label for=…> pointing at something focusable.
			entry.id = input.id;
			input.id = input.id + "-value";
		}

		function sync() {
			input.value = names.join(", ");
		}
		function render() {
			while (box.firstChild !== entry && box.firstChild) box.removeChild(box.firstChild);
			names.forEach(function (name, i) {
				var pill = document.createElement("span");
				pill.className = "flash-pill flash-tag-pill";
				pill.textContent = name;
				var x = document.createElement("button");
				x.type = "button";
				x.className = "flash-tag-remove";
				x.setAttribute("aria-label", "Remove tag " + name);
				x.textContent = "×";
				x.addEventListener("click", function () {
					names.splice(i, 1);
					sync();
					render();
					entry.focus();
				});
				pill.appendChild(x);
				box.insertBefore(pill, entry);
			});
		}
		function add(raw) {
			raw.split(",").forEach(function (part) {
				var name = part.trim();
				if (name && names.indexOf(name) === -1) names.push(name);
			});
			entry.value = "";
			sync();
			render();
		}

		entry.addEventListener("keydown", function (e) {
			if (e.key === "Enter" || e.key === ",") {
				e.preventDefault();
				add(entry.value);
			} else if (e.key === "Backspace" && entry.value === "" && names.length) {
				names.pop();
				sync();
				render();
			}
		});
		entry.addEventListener("blur", function () {
			if (entry.value.trim()) add(entry.value);
		});
		if (input.form) {
			input.form.addEventListener("submit", function () {
				if (entry.value.trim()) add(entry.value);
			});
		}

		input.type = "hidden";
		input.parentNode.insertBefore(box, input);
		box.appendChild(entry);
		render();
	}

	// initDropZone lets a file be dropped onto the zone's label, and shows
	// the chosen file's name (the CSP's img-src 'self' rules out a blob:
	// thumbnail).
	function initDropZone(zone) {
		var input = zone.querySelector("input[type=file]");
		var label = zone.querySelector(".flash-drop-file");
		if (!input) return;
		function show() {
			if (label) label.textContent = input.files.length ? input.files[0].name : "";
			zone.classList.toggle("flash-drop-chosen", input.files.length > 0);
		}
		zone.addEventListener("dragover", function (e) {
			e.preventDefault();
			zone.classList.add("flash-drop-over");
		});
		zone.addEventListener("dragleave", function () {
			zone.classList.remove("flash-drop-over");
		});
		zone.addEventListener("drop", function (e) {
			e.preventDefault();
			zone.classList.remove("flash-drop-over");
			if (e.dataTransfer && e.dataTransfer.files.length) {
				input.files = e.dataTransfer.files;
				show();
			}
		});
		input.addEventListener("change", show);
	}

	// enhance wires every not-yet-enhanced editor element under root. It is
	// idempotent, so running it on each htmx:load is safe.
	function enhance(root) {
		function each(selector, fn) {
			var list = root.querySelectorAll(selector);
			for (var i = 0; i < list.length; i++) {
				if (list[i].getAttribute("data-enhanced")) continue;
				list[i].setAttribute("data-enhanced", "1");
				fn(list[i]);
			}
		}
		each(".flash-make-blank", function (btn) { btn.hidden = false; });
		each("input[data-tag-input]", initTagInput);
		each("label[data-drop]", initDropZone);
	}

	document.addEventListener("click", function (e) {
		var btn = e.target.closest && e.target.closest(".flash-make-blank");
		if (btn) makeBlank(btn);
	});
	document.addEventListener("htmx:load", function (e) { enhance(e.target); });
	enhance(document);
```

> `flash.js` loads with `defer`, so the document is parsed when
> `enhance(document)` runs. htmx fires `htmx:load` on every swapped-in
> element, which covers the pane swaps.

- [ ] **Step 2: Check it is still served**

Run: `go test ./internal/apps/flash/ -run TestFlashScriptIsServed -count=1`
Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git add internal/apps/flash/static/flash.js
git commit -m "feat(flash): make-blank, tag pills and drag-and-drop in the card editor"
```

---

### Task 5: Editor CSS

**Files:**
- Modify: `internal/ui/static/app.css`

- [ ] **Step 1: Append the editor styles** (end of the file, after U2's block)

```css
/* ---- ON Flash: card editor (UI overhaul U3) ----------------------------- */

.flash-editor { max-width: 52rem; }
.flash-editor h1 { margin: 0 0 var(--s-3); }

/* Type toggle: two radios drawn as one segmented control. */
.flash-type-toggle {
	display: inline-flex;
	margin: 0 0 var(--s-4);
	padding: 0;
	border: 1px solid var(--c-border-firm);
	border-radius: 8px;
	overflow: hidden;
}
.flash-type-toggle label {
	margin: 0;
	padding: var(--s-2) var(--s-4);
	min-height: 2.75rem;
	display: inline-flex;
	align-items: center;
	color: var(--c-text);
	font-size: var(--fs-sm);
	cursor: pointer;
}
.flash-type-toggle input:checked + label { background: var(--c-accent); color: #fff; }
:root[data-theme="dark"] .flash-type-toggle input:checked + label { color: #10141a; }
.flash-type-toggle input:focus-visible + label { outline: var(--ring); outline-offset: -3px; }

/* The two faces, drawn as cards. */
.flash-editor-faces {
	display: grid;
	grid-template-columns: repeat(2, minmax(0, 1fr));
	gap: var(--s-4);
	margin-bottom: var(--s-3);
}
.flash-editor-face {
	position: relative;
	display: flex;
	flex-direction: column;
	gap: var(--s-2);
	padding: var(--s-5) var(--s-4) var(--s-4);
	border-radius: 14px;
	border: 1px solid var(--c-border);
	background: var(--flash-card-bg);
	overflow: hidden;
}
.flash-editor-face::before {
	content: "";
	position: absolute;
	top: 0; left: 0; right: 0;
	height: 8px;
	background: var(--deck, var(--c-border-firm));
}
.flash-editor-back { background: color-mix(in srgb, var(--flash-card-bg) 92%, var(--c-bg-subtle)); }
.flash-editor-face label { margin: 0; font-size: var(--fs-xs); color: var(--c-text-faint); }
.flash-editor-face textarea {
	flex: 1;
	min-height: 7rem;
	border: none;
	background: transparent;
	font-family: var(--font-ui);
	font-size: var(--fs-lg);
	text-align: center;
	resize: vertical;
}
.flash-editor-face textarea:focus-visible { outline: var(--ring); border-radius: var(--radius); }
.flash-make-blank { align-self: center; }
.flash-make-blank[hidden] { display: none; }

/* Question and answer vs fill in the blank, with no JS: :has() reads the
 * checked radio. */
.flash-when-cloze { display: none; }
.flash-editor:has(input[name="card_type"][value="cloze"]:checked) .flash-when-cloze { display: inline; }
.flash-editor:has(input[name="card_type"][value="cloze"]:checked) p.flash-when-cloze { display: block; }
.flash-editor:has(input[name="card_type"][value="cloze"]:checked) .flash-when-basic { display: none; }
.flash-editor:has(input[name="card_type"][value="cloze"]:checked) .flash-editor-back { display: none; }
.flash-editor:has(input[name="card_type"][value="cloze"]:checked) .flash-editor-faces { grid-template-columns: minmax(0, 1fr); }
.flash-editor:not(:has(input[name="card_type"][value="cloze"]:checked)) .flash-make-blank { display: none; }

/* Tag pills editor (flash.js). */
.flash-tag-editor {
	display: flex;
	flex-wrap: wrap;
	align-items: center;
	gap: var(--s-1);
	padding: var(--s-1) var(--s-2);
	border: 1px solid var(--c-border-firm);
	border-radius: var(--radius);
	background: var(--c-bg);
}
.flash-tag-editor:focus-within { outline: var(--ring); }
.flash-tag-pill { gap: var(--s-1); background: var(--c-accent-bg); color: var(--c-accent); }
.flash-tag-remove {
	padding: 0 var(--s-1);
	border: none;
	background: none;
	color: inherit;
	font-size: var(--fs-base);
	line-height: 1;
	cursor: pointer;
}
input.flash-tag-entry {
	flex: 1;
	min-width: 8rem;
	width: auto;
	padding: var(--s-1);
	border: none;
	background: transparent;
}
input.flash-tag-entry:focus { outline: none; }

/* Media drop zones. */
.flash-drops {
	display: grid;
	grid-template-columns: repeat(2, minmax(0, 1fr));
	gap: var(--s-4);
	margin-bottom: var(--s-4);
}
.flash-drop-field { display: flex; flex-direction: column; gap: var(--s-2); }
.flash-drop {
	display: flex;
	flex-direction: column;
	align-items: center;
	justify-content: center;
	gap: var(--s-2);
	min-height: 5.5rem;
	margin: 0;
	padding: var(--s-3);
	border: 2px dashed var(--c-border-firm);
	border-radius: 10px;
	color: var(--c-text-dim);
	text-align: center;
	cursor: pointer;
}
.flash-drop:hover,
.flash-drop-over { border-color: var(--c-accent); background: var(--c-accent-bg); }
.flash-drop:focus-within { outline: var(--ring); outline-offset: 2px; }
.flash-drop-chosen { border-style: solid; border-color: var(--c-accent); }
.flash-drop-file { font-weight: 500; color: var(--c-text); }
.flash-drop-file:empty { display: none; }
.flash-drop-preview { max-height: 5rem; max-width: 100%; border-radius: var(--radius); }
.flash-drop-remove { display: flex; align-items: center; gap: var(--s-1); margin: 0; }
.flash-drop-field audio { width: 100%; }

.flash-editor-actions { flex-wrap: wrap; }

@media (max-width: 640px) {
	.flash-editor-faces,
	.flash-drops { grid-template-columns: minmax(0, 1fr); }
}
```

> The `@media (max-width: 640px)` rule above is Flash-specific and sits in
> the Flash section on purpose (U1 set that precedent for the Flash
> 900px block), rather than in the platform's shared 640px block.

- [ ] **Step 2: Full check, then commit**

```bash
git add internal/ui/static/app.css
git commit -m "style(flash): card editor"
```

---

### Task 6: Manual verification

- [ ] Build, start the `onsuite` preview (ask the user to sign in), open a deck → Cards → "+ New card". Check with screenshots:
  1. Faces look like cards with the deck stripe; "Question and answer" is selected.
  2. Switch to "Fill in the blank": the Back face disappears, the label changes, Make blank appears; select a word, press Make blank → `{{c1::word}}` with the word selected; press again on another word → `c2`.
  3. Tags: type `food`, Enter → pill; `,` also adds; Backspace on empty removes the last; × removes one; submit keeps them.
  4. Drag a PNG onto the picture zone → its name shows; save → the opened card shows the picture on the front.
  5. "Save and add another" → empty form, same type and tags, "Card saved" notice; the new card is in the grid afterwards.
  6. Edit that card: the current picture shows with "Remove the picture"; tick it, save → gone.
  7. Upload a text file as the picture → friendly error, typed text kept, no card created.
  8. Disable JS (browser settings, or check the HTML by reading the page with scripts off if available) — the plain tags field, file inputs and both faces still work.
  9. Dark theme; 640px width (faces stack); console has no CSP errors.
- [ ] Fix, full check, commit.

### Task 7: Open the PR

```bash
git push -u origin feat/flash-ui-u3
gh pr create --title "feat(flash): UI overhaul U3 — card editor" --body "$(cat <<'EOF'
## Summary
- One card form for text, tags and media (the create/update routes accept the media route's fields; files are checked before anything is saved).
- Card-face editor: faces drawn as cards, CSS-only "Question and answer / Fill in the blank" toggle, Make blank, tag pills, drop zones.
- "Save and add another" keeps the card type and tags (with and without JS).
- The opened card's separate "Update media" form is gone.

Spec §4. Plan: docs/superpowers/plans/2026-09-23-flash-ui-u3-editor.md.

## Test plan
- [ ] `go test ./... -race -count=1`
- [ ] Manual checklist from the plan's Task 6 (screenshots attached)
EOF
)"
```
