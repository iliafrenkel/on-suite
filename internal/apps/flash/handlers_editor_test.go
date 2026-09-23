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
