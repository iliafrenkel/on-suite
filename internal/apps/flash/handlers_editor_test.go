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

// TestCreateCardWithNearMaxImageAndAudioSucceeds is the regression test for
// #330: cardFormMaxBytes used to be exactly
// MaxImageFetchBytes+MaxAudioFetchBytes, with no allowance for the form's
// text fields or multipart encoding overhead (per-part boundaries and
// headers). A request carrying a full-size image and a full-size audio
// file — each individually within its own per-field cap — already
// consumes the entire old budget on file bytes alone, so the multipart
// overhead and text fields pushed the total over the old cap and the
// request failed even though every part was individually valid. The new
// cap adds a 1 MiB allowance for exactly that overhead.
func TestCreateCardWithNearMaxImageAndAudioSucceeds(t *testing.T) {
	s := newServer(t)
	deck, err := s.Store.CreateDeck(t.Context(), s.Alice.User.ID, "Animals", "")
	if err != nil {
		t.Fatal(err)
	}

	image := make([]byte, flash.MaxImageFetchBytes)
	copy(image, onePNG)
	audio := make([]byte, flash.MaxAudioFetchBytes)
	copy(audio, []byte("ID3"))

	rec := postCardForm(t, s, s.Alice, "/flash/"+itoa(deck.ID)+"/cards/new",
		url.Values{"card_type": {"basic"}, "front": {"cat"}, "back": {"gato"}, "notes": {"a note"}, "tags": {"animals, pets"}},
		map[string][]byte{"image": image, "audio": audio})
	if rec.Code != http.StatusCreated {
		t.Fatalf("create with near-max image+audio = %d; body: %s", rec.Code, rec.Body.String())
	}
	cards, err := s.Store.ListCards(t.Context(), s.Alice.User.ID, deck.ID)
	if err != nil || len(cards) != 1 {
		t.Fatalf("ListCards = %v, %v", cards, err)
	}
	if cards[0].ImageHash == nil || cards[0].AudioHash == nil {
		t.Error("both the image and the audio sent with the card form should be attached")
	}
}

// TestCreateCardRequiresCSRF and the two tests below were moved from
// handlers_media_test.go's now-removed uploadCardMedia route (#327): that
// route was dead (no UI has called it since media started traveling in the
// create/update card form, U3), but its protections — CSRF, an oversized
// file, and a request over the platform's global 1MB body cap — need to
// keep being exercised on the route that actually carries media today.
func TestCreateCardRequiresCSRF(t *testing.T) {
	s := newServer(t)
	deck, err := s.Store.CreateDeck(t.Context(), s.Alice.User.ID, "Spanish", "")
	if err != nil {
		t.Fatal(err)
	}
	req := httpPost(t, "/flash/"+itoa(deck.ID)+"/cards/new", url.Values{"card_type": {"basic"}, "front": {"a"}, "back": {"b"}})
	rec := s.Do(t, s.Alice, req)
	if rec.Code != http.StatusForbidden {
		t.Errorf("create card without CSRF = %d, want 403", rec.Code)
	}
}

// TestCreateCardWithOversizedImageIsRejected is
// TestUploadCardImageRejectsOversizedFile, moved to the card-form route.
func TestCreateCardWithOversizedImageIsRejected(t *testing.T) {
	s := newServer(t)
	deck, err := s.Store.CreateDeck(t.Context(), s.Alice.User.ID, "Spanish", "")
	if err != nil {
		t.Fatal(err)
	}

	// One byte over the per-field image cap (MaxImageFetchBytes = 5MB), but
	// still under cardFormMaxBytes, so this request reaches the app's own
	// per-field size check in readUpload rather than being rejected earlier
	// by the platform layer.
	oversized := make([]byte, flash.MaxImageFetchBytes+1)
	copy(oversized, onePNG)

	rec := postCardForm(t, s, s.Alice, "/flash/"+itoa(deck.ID)+"/cards/new",
		url.Values{"card_type": {"basic"}, "front": {"cat"}, "back": {"gato"}},
		map[string][]byte{"image": oversized})
	if rec.Code != http.StatusOK {
		t.Fatalf("oversized image over HTMX = %d, want 200 with a notice-error fragment", rec.Code)
	}
	doc := htmlassert.Parse(t, rec.Body.String())
	doc.MustHave("#card-detail-new .notice-error")

	if cards, _ := s.Store.ListCards(t.Context(), s.Alice.User.ID, deck.ID); len(cards) != 0 {
		t.Errorf("a rejected oversized upload still created %d card(s)", len(cards))
	}
}

// TestCreateCardWithImageOverGlobalCapSucceeds is
// TestUploadCardImageOverGlobalCapSucceeds, moved to the card-form route:
// the regression test for the platform's global 1MB body cap
// (web.DefaultMaxBodyBytes) running ahead of this route's own raised
// cardFormMaxBytes cap. A ~2MB file sits strictly between the two, so it
// only succeeds once the route's own body-limit override (flash.go's
// Mount) is in effect. The card-form route previously had no test for
// this at all (#330/#327).
func TestCreateCardWithImageOverGlobalCapSucceeds(t *testing.T) {
	s := newServer(t)
	deck, err := s.Store.CreateDeck(t.Context(), s.Alice.User.ID, "Spanish", "")
	if err != nil {
		t.Fatal(err)
	}

	const size = 2 << 20
	big := make([]byte, size)
	copy(big, onePNG)

	rec := postCardForm(t, s, s.Alice, "/flash/"+itoa(deck.ID)+"/cards/new",
		url.Values{"card_type": {"basic"}, "front": {"cat"}, "back": {"gato"}},
		map[string][]byte{"image": big})
	if rec.Code != http.StatusCreated {
		t.Fatalf("create with a ~2MB image = %d; body: %s", rec.Code, rec.Body.String())
	}
	cards, err := s.Store.ListCards(t.Context(), s.Alice.User.ID, deck.ID)
	if err != nil || len(cards) != 1 {
		t.Fatalf("ListCards = %v, %v", cards, err)
	}
	if cards[0].ImageHash == nil {
		t.Fatal("ImageHash is nil after a ~2MB upload that should have succeeded")
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

// onePNG2 is a second, distinct valid PNG fixture (a different final byte),
// so a test can tell "the image changed" apart from "the image stayed the
// same" by comparing hashes.
var onePNG2 = []byte{
	0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A,
	0x00, 0x00, 0x00, 0x0D, 0x49, 0x48, 0x44, 0x53,
}

// TestNewImageWinsOverRemoveFlag is the regression test for #328: a card
// form used to silently drop a newly chosen file whenever its Remove
// checkbox was ticked (readCardUploads skipped reading that field
// entirely), even though the two controls are not mutually exclusive in
// the UI — flash.js only unticks Remove when a file is chosen, it does not
// prevent both being present in one submission (e.g. a no-JS submission,
// or a race with the checkbox). The rule is: a newly uploaded file always
// wins over Remove for that kind.
func TestNewImageWinsOverRemoveFlag(t *testing.T) {
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
	original, err := s.Store.CardByID(t.Context(), s.Alice.User.ID, deck.ID, 1)
	if err != nil {
		t.Fatal(err)
	}

	rec = postCardForm(t, s, s.Alice, "/flash/"+itoa(deck.ID)+"/cards/1",
		url.Values{"card_type": {"basic"}, "front": {"cat"}, "back": {"gato"}, "remove_image": {"1"}},
		map[string][]byte{"image": onePNG2})
	if rec.Code != http.StatusOK {
		t.Fatalf("update = %d; body: %s", rec.Code, rec.Body.String())
	}
	updated, err := s.Store.CardByID(t.Context(), s.Alice.User.ID, deck.ID, 1)
	if err != nil {
		t.Fatal(err)
	}
	if updated.ImageHash == nil {
		t.Fatal("a new image sent alongside remove_image should still be attached")
	}
	if *updated.ImageHash == *original.ImageHash {
		t.Error("the card's image was not replaced by the new file")
	}
}

// TestBadNewImageStillValidatesWithRemoveTicked is the other half of #328:
// remove must not silently swallow a bad new file. A bad file sent
// alongside remove_image is still checked, and still produces the normal
// validation error, leaving the card's existing image untouched.
func TestBadNewImageStillValidatesWithRemoveTicked(t *testing.T) {
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
	original, err := s.Store.CardByID(t.Context(), s.Alice.User.ID, deck.ID, 1)
	if err != nil {
		t.Fatal(err)
	}

	rec = postCardForm(t, s, s.Alice, "/flash/"+itoa(deck.ID)+"/cards/1",
		url.Values{"card_type": {"basic"}, "front": {"cat"}, "back": {"gato"}, "remove_image": {"1"}},
		map[string][]byte{"image": []byte("<html>not an image</html>")})
	if rec.Code != http.StatusOK {
		t.Fatalf("bad image over HTMX = %d, want 200 with the form re-rendered", rec.Code)
	}
	doc := htmlassert.Parse(t, rec.Body.String())
	doc.MustHave("#card-detail-edit .notice-error")

	unchanged, err := s.Store.CardByID(t.Context(), s.Alice.User.ID, deck.ID, 1)
	if err != nil {
		t.Fatal(err)
	}
	if unchanged.ImageHash == nil || *unchanged.ImageHash != *original.ImageHash {
		t.Error("a rejected new image (even with remove ticked) must leave the existing image untouched")
	}
}

// oneMP3 is the smallest thing http.DetectContentType calls audio/mpeg (an
// "ID3" tag header) — contentTypeMatchesKind needs a sniffed "audio/…"
// content type, and DetectContentType sniffs "OggS" as "application/ogg"
// rather than "audio/ogg", so ID3 is the fixture that actually validates.
var oneMP3 = []byte("ID3\x03\x00\x00\x00\x00\x00\x00")

// TestUpdateCardWithBadImageLeavesTheCardUnchanged is #331's first gap: a
// bad image upload on updateCard must not touch the card's stored fields —
// uploadErr is checked before store.UpdateCard is ever called, so the
// text/tags typed in the same, otherwise-valid submission never reach the
// database.
func TestUpdateCardWithBadImageLeavesTheCardUnchanged(t *testing.T) {
	s := newServer(t)
	deck, err := s.Store.CreateDeck(t.Context(), s.Alice.User.ID, "Animals", "")
	if err != nil {
		t.Fatal(err)
	}
	rec := postCardForm(t, s, s.Alice, "/flash/"+itoa(deck.ID)+"/cards/new",
		url.Values{"card_type": {"basic"}, "front": {"cat"}, "back": {"gato"}, "notes": {"a note"}},
		nil)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create = %d", rec.Code)
	}

	rec = postCardForm(t, s, s.Alice, "/flash/"+itoa(deck.ID)+"/cards/1",
		url.Values{"card_type": {"basic"}, "front": {"dog"}, "back": {"perro"}, "notes": {"a different note"}},
		map[string][]byte{"image": []byte("<html>not an image</html>")})
	if rec.Code != http.StatusOK {
		t.Fatalf("bad image over HTMX = %d, want 200 with the form re-rendered", rec.Code)
	}
	doc := htmlassert.Parse(t, rec.Body.String())
	doc.MustHave("#card-detail-edit .notice-error")

	c, err := s.Store.CardByID(t.Context(), s.Alice.User.ID, deck.ID, 1)
	if err != nil {
		t.Fatal(err)
	}
	if c.Front != "cat" || c.Back != "gato" || c.Notes != "a note" {
		t.Errorf("card fields changed despite the rejected upload: Front=%q Back=%q Notes=%q", c.Front, c.Back, c.Notes)
	}
}

// TestCreateCardWithAudioInOneForm is #331's second gap: audio has never
// had its own test through the unified card form (only image has), and it
// needs different, audio-sniffable bytes.
func TestCreateCardWithAudioInOneForm(t *testing.T) {
	s := newServer(t)
	deck, err := s.Store.CreateDeck(t.Context(), s.Alice.User.ID, "Music", "")
	if err != nil {
		t.Fatal(err)
	}
	rec := postCardForm(t, s, s.Alice, "/flash/"+itoa(deck.ID)+"/cards/new",
		url.Values{"card_type": {"basic"}, "front": {"forte"}, "back": {"loud"}},
		map[string][]byte{"audio": oneMP3})
	if rec.Code != http.StatusCreated {
		t.Fatalf("create = %d; body: %s", rec.Code, rec.Body.String())
	}
	cards, err := s.Store.ListCards(t.Context(), s.Alice.User.ID, deck.ID)
	if err != nil || len(cards) != 1 {
		t.Fatalf("ListCards = %v, %v", cards, err)
	}
	if cards[0].AudioHash == nil {
		t.Error("the audio sent with the card form was not attached")
	}
}

// TestNewAudioWinsOverRemoveFlag is #331's third gap (the audio half of
// #328/TestNewImageWinsOverRemoveFlag above): cheap to add since it is the
// same rule, just for the other kind.
func TestNewAudioWinsOverRemoveFlag(t *testing.T) {
	s := newServer(t)
	deck, err := s.Store.CreateDeck(t.Context(), s.Alice.User.ID, "Music", "")
	if err != nil {
		t.Fatal(err)
	}
	rec := postCardForm(t, s, s.Alice, "/flash/"+itoa(deck.ID)+"/cards/new",
		url.Values{"card_type": {"basic"}, "front": {"forte"}, "back": {"loud"}},
		map[string][]byte{"audio": oneMP3})
	if rec.Code != http.StatusCreated {
		t.Fatalf("create = %d", rec.Code)
	}
	original, err := s.Store.CardByID(t.Context(), s.Alice.User.ID, deck.ID, 1)
	if err != nil {
		t.Fatal(err)
	}

	newAudio := append([]byte("ID3\x04"), oneMP3...)
	rec = postCardForm(t, s, s.Alice, "/flash/"+itoa(deck.ID)+"/cards/1",
		url.Values{"card_type": {"basic"}, "front": {"forte"}, "back": {"loud"}, "remove_audio": {"1"}},
		map[string][]byte{"audio": newAudio})
	if rec.Code != http.StatusOK {
		t.Fatalf("update = %d; body: %s", rec.Code, rec.Body.String())
	}
	updated, err := s.Store.CardByID(t.Context(), s.Alice.User.ID, deck.ID, 1)
	if err != nil {
		t.Fatal(err)
	}
	if updated.AudioHash == nil {
		t.Fatal("a new audio file sent alongside remove_audio should still be attached")
	}
	if *updated.AudioHash == *original.AudioHash {
		t.Error("the card's audio was not replaced by the new file")
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
