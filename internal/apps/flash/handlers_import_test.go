package flash_test

import (
	"net/url"
	"strings"
	"testing"

	"github.com/iliafrenkel/on-suite/internal/htmlassert"
)

func TestImportRequiresSignIn(t *testing.T) {
	s := newServer(t)
	rec := s.Do(t, nil, httpGet(t, "/flash/import"))
	if rec.Code != 303 && rec.Code != 401 {
		t.Errorf("GET /flash/import signed out = %d, want a redirect-to-login or 401", rec.Code)
	}
}

func TestImportRequiresCSRF(t *testing.T) {
	s := newServer(t)
	req := httpPost(t, "/flash/import", url.Values{"payload": {"{}"}, "format": {"json"}})
	rec := s.Do(t, s.Alice, req)
	if rec.Code != 403 {
		t.Errorf("POST /flash/import without CSRF = %d, want 403", rec.Code)
	}
}

func TestImportJSONOverHTTP(t *testing.T) {
	s := newServer(t)
	payload := `{
		"deck": {"name": "Spanish travel phrases", "description": "Travel basics"},
		"cards": [{"type": "basic", "front": "Thank you", "back": "Gracias", "tags": ["travel"]}]
	}`
	rec := s.Post(t, s.Alice, "/flash/import", url.Values{"payload": {payload}, "format": {"json"}})
	if rec.Code != 303 {
		t.Fatalf("POST /flash/import (json) = %d, want 303", rec.Code)
	}

	doc := s.Get(t, s.Alice, rec.Header().Get("Location"))
	doc.MustHave(".deck-list")
	body := htmlassert.Text(doc.MustHave("body"))
	if !strings.Contains(body, "Spanish travel phrases") {
		t.Errorf("deck page does not show the imported deck's name: %q", body)
	}
}

func TestImportMarkdownOverHTTP(t *testing.T) {
	s := newServer(t)
	payload := "# Geography\n\n## Card\nFront: The capital of France is {{c1::Paris}}.\nType: cloze\n"
	rec := s.Post(t, s.Alice, "/flash/import", url.Values{"payload": {payload}, "format": {"markdown"}})
	if rec.Code != 303 {
		t.Fatalf("POST /flash/import (markdown) = %d, want 303", rec.Code)
	}

	doc := s.Get(t, s.Alice, rec.Header().Get("Location"))
	body := htmlassert.Text(doc.MustHave("body"))
	if !strings.Contains(body, "Geography") {
		t.Errorf("deck page does not show the imported deck's name: %q", body)
	}
}

// TestImportOverHTMXShowsDeckColorImmediately guards against a regression
// where the HTMX import path rendered the freshly-imported deck's pane
// with no colour class at all (deck-c-), because ImportDeck's returned
// Deck value never got its Color field set and the pane was built
// straight from that value instead of a re-read from the DB.
func TestImportOverHTMXShowsDeckColorImmediately(t *testing.T) {
	s := newServer(t)
	payload := `{
		"deck": {"name": "Spanish travel phrases", "description": "Travel basics"},
		"cards": [{"type": "basic", "front": "Thank you", "back": "Gracias", "tags": ["travel"]}]
	}`
	rec := s.PostHX(t, s.Alice, "/flash/import", url.Values{"payload": {payload}, "format": {"json"}})
	if rec.Code != 201 {
		t.Fatalf("POST /flash/import over HTMX = %d, want 201; body: %s", rec.Code, rec.Body.String())
	}
	doc := htmlassert.Parse(t, rec.Body.String())
	view := doc.MustHave("#deck-detail-view")
	class, _ := htmlassert.Attr(view, "class")
	if !strings.Contains(class, "deck-c-teal") {
		t.Errorf("#deck-detail-view class = %q, want it to contain deck-c-teal", class)
	}
}

func TestImportMalformedWritesNothing(t *testing.T) {
	s := newServer(t)

	rec := s.Post(t, s.Alice, "/flash/import", url.Values{
		"payload": {`{"deck": {"name": "Bad"}, "cards": [{"front": "", "back": "A"}]}`},
		"format":  {"json"},
	})
	if rec.Code != 400 {
		t.Fatalf("POST /flash/import with a blank front = %d, want 400", rec.Code)
	}
	doc := htmlassert.Parse(t, rec.Body.String())
	doc.MustHave(".notice-error")

	after := s.Get(t, s.Alice, "/flash/")
	afterBody := htmlassert.Text(after.MustHave("body"))
	if strings.Contains(afterBody, "Bad") {
		t.Errorf("a deck named %q should not exist after a rejected import", "Bad")
	}
}

// TestImportDuplicateNameOverHTTPWritesNothingAndPreservesInput exercises
// ImportDeck's actual transaction rollback: unlike
// TestImportMalformedWritesNothing (which is rejected by ParseImport before
// any database access), this payload passes parsing and only fails once
// ImportDeck tries to INSERT a duplicate deck name, hitting
// handlers_import.go's post-ImportDeck errors.Is(err, ErrInvalid) branch.
func TestImportDuplicateNameOverHTTPWritesNothingAndPreservesInput(t *testing.T) {
	s := newServer(t)
	if _, err := s.Store.CreateDeck(t.Context(), s.Alice.User.ID, "Existing", ""); err != nil {
		t.Fatal(err)
	}

	payload := `{"deck": {"name": "Existing"}, "cards": [{"front": "Q", "back": "A"}]}`
	rec := s.Post(t, s.Alice, "/flash/import", url.Values{"payload": {payload}, "format": {"json"}})
	if rec.Code != 400 {
		t.Fatalf("POST /flash/import with a duplicate deck name = %d, want 400", rec.Code)
	}
	doc := htmlassert.Parse(t, rec.Body.String())
	doc.MustHave(".notice-error")

	// The textarea re-renders PayloadValue through html/template's text-node
	// escaper, which turns each `"` into `&#34;` but leaves `{`/`}` alone.
	body := rec.Body.String()
	escapedPayload := strings.ReplaceAll(payload, `"`, "&#34;")
	if !strings.Contains(body, escapedPayload) {
		t.Errorf("the pasted payload was not preserved in the re-rendered form")
	}

	decks, err := s.Store.ListDecks(t.Context(), s.Alice.User.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(decks) != 1 {
		t.Errorf("len(decks) = %d, want 1 (only the original 'Existing' deck, nothing partially imported)", len(decks))
	}
}

func TestImportPaneHasTheThreeStepsAndThePrompt(t *testing.T) {
	s := newServer(t)
	doc := s.Get(t, s.Alice, "/flash/import")
	if n := len(doc.QueryAll(".flash-import-steps li")); n != 3 {
		t.Errorf("import pane shows %d steps, want 3", n)
	}
	prompt := doc.MustHave("textarea#import-prompt")
	if !strings.Contains(htmlassert.Text(prompt), "[TOPIC]") {
		t.Error("the prompt box does not contain the AI prompt")
	}
	if _, ok := htmlassert.Attr(prompt, "readonly"); !ok {
		t.Error("the prompt box should be read-only")
	}
	copyBtn := doc.MustHave("button.flash-copy-prompt")
	if target, _ := htmlassert.Attr(copyBtn, "data-copy-target"); target != "import-prompt" {
		t.Errorf("Copy button data-copy-target = %q", target)
	}
	if _, ok := htmlassert.Attr(copyBtn, "hidden"); !ok {
		t.Error("the Copy button must start hidden; flash.js reveals it")
	}
	// The format picker is tucked away; auto-detect is the default.
	doc.MustHave(`details.flash-import-format select[name=format]`)
	doc.MustHave(`textarea[name=payload]`)
}

func TestImportSuccessShowsANotice(t *testing.T) {
	s := newServer(t)
	payload := `{"deck":{"name":"Planets"},"cards":[{"type":"basic","front":"Mars","back":"red"},{"type":"basic","front":"Earth","back":"home"}]}`
	rec := s.PostHX(t, s.Alice, "/flash/import", url.Values{"payload": {payload}, "format": {"auto"}})
	if rec.Code != 201 {
		t.Fatalf("import over HTMX = %d; body: %s", rec.Code, rec.Body.String())
	}
	doc := htmlassert.Parse(t, rec.Body.String())
	notice := doc.MustHave("#deck-detail-view .flash-notice")
	if got := htmlassert.Text(notice); !strings.Contains(got, "Imported 2 cards") {
		t.Errorf("notice = %q, want Imported 2 cards", got)
	}
}
