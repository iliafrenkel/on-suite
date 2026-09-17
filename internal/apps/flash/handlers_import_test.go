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
