package later_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/iliafrenkel/on-suite/internal/apps/later"
	"github.com/iliafrenkel/on-suite/internal/htmlassert"
	"golang.org/x/net/html"
)

func seedHello(t *testing.T, s *server) later.Article {
	t.Helper()
	return seed(t, s, s.Alice.User.ID, later.NewArticle{
		URL: "https://blog.example/hello", Title: "Hello", ContentHTML: helloHTML,
	})
}

func attr(t *testing.T, doc *htmlassert.Doc, selector, name string) string {
	t.Helper()
	v, _ := htmlassert.Attr(doc.MustHave(selector), name)
	return v
}

// rawText is a node's text with its whitespace intact (htmlassert.Text
// collapses it, which would hide a lost newline).
func rawText(n *html.Node) string {
	var b strings.Builder
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == html.TextNode {
			b.WriteString(c.Data)
		}
	}
	return b.String()
}

func TestArticleHasTheNotesPanel(t *testing.T) {
	s := newServer(t)
	a := seedReadable(t, s)
	doc := s.Get(t, s.Alice, articlePath(a, ""))

	doc.MustHave("input#later-notes-open")
	if got := attr(t, doc, "input#later-notes-open", "type"); got != "checkbox" {
		t.Errorf("toggle type = %q, want checkbox", got)
	}
	doc.MustHave("label.later-notes-toggle")
	if got := attr(t, doc, "label.later-notes-toggle", "for"); got != "later-notes-open" {
		t.Errorf("toggle label for = %q", got)
	}
	if got := strings.Join(strings.Fields(htmlassert.Text(doc.MustHave("label.later-notes-toggle"))), " "); got != "Notes · 0" {
		t.Errorf("toggle text = %q, want %q", got, "Notes · 0")
	}
	doc.MustHave("aside#later-notes")
	doc.MustHave("aside#later-notes form[action=" + articlePath(a, "/note") + "]")
	doc.MustHave("aside#later-notes textarea#later-note-panel")
	if got := attr(t, doc, "aside#later-notes textarea#later-note-panel", "name"); got != "note" {
		t.Errorf("panel textarea name = %q", got)
	}
	doc.MustHave("aside#later-notes input[value=panel]")
	doc.MustHave("li.later-notes-empty")
}

func TestArticleHasTheEndOfArticleNoteBox(t *testing.T) {
	s := newServer(t)
	a := seedReadable(t, s)
	doc := s.Get(t, s.Alice, articlePath(a, ""))

	path := articlePath(a, "/note")
	doc.MustHave(".later-note-end form[action=" + path + "]")
	doc.MustHave(".later-note-end textarea#later-note-end")
	doc.MustHave(".later-note-end input[value=end]")

	for _, sel := range []string{
		".later-note-end form", "aside#later-notes form",
	} {
		if got := attr(t, doc, sel, "hx-post"); got != path {
			t.Errorf("%s hx-post = %q, want %q", sel, got, path)
		}
		if got := attr(t, doc, sel, "hx-trigger"); got != "change, submit" {
			t.Errorf("%s hx-trigger = %q", sel, got)
		}
		if got := attr(t, doc, sel, "hx-swap"); got != "none" {
			t.Errorf("%s hx-swap = %q", sel, got)
		}
	}
}

func TestNoteIsShownInBothCopies(t *testing.T) {
	s := newServer(t)
	a := seedReadable(t, s)
	if err := s.Store.SetNote(context.Background(), s.Alice.User.ID, a.ID, "Remember this"); err != nil {
		t.Fatal(err)
	}
	doc := s.Get(t, s.Alice, articlePath(a, ""))
	for _, id := range []string{"later-note-panel", "later-note-end"} {
		if got := htmlassert.Text(doc.MustHave("textarea#" + id)); got != "Remember this" {
			t.Errorf("#%s = %q, want the note", id, got)
		}
	}
}

func TestNotesPanelListsHighlightsInTextOrder(t *testing.T) {
	s := newServer(t)
	a := seedHello(t, s)
	ctx := context.Background()
	world, err := s.Store.AddHighlight(ctx, a.ID, a.ContentText, 16, 21, "world", "Big")
	if err != nil {
		t.Fatal(err)
	}
	brave, err := s.Store.AddHighlight(ctx, a.ID, a.ContentText, 6, 11, "brave", "")
	if err != nil {
		t.Fatal(err)
	}
	doc := s.Get(t, s.Alice, articlePath(a, ""))

	if got := htmlassert.Text(doc.MustHave("#later-notes-count")); got != "2" {
		t.Errorf("count = %q, want 2", got)
	}
	items := doc.QueryAll("li.later-notes-item")
	if len(items) != 2 {
		t.Fatalf("got %d items, want 2", len(items))
	}
	for i, want := range []struct {
		id    int64
		quote string
	}{{brave.ID, "brave"}, {world.ID, "world"}} {
		if got, _ := htmlassert.Attr(items[i], "data-highlight-id"); got != strconv.FormatInt(want.id, 10) {
			t.Errorf("item %d id = %q, want %d", i, got, want.id)
		}
		if got := strings.TrimSpace(htmlassert.Text(items[i])); !strings.HasPrefix(got, want.quote) {
			t.Errorf("item %d text = %q, want it to start with %q", i, got, want.quote)
		}
	}
	doc.MustHave("a[href=#later-h-" + strconv.FormatInt(brave.ID, 10) + "]")
	doc.MustHave("a[href=#later-h-" + strconv.FormatInt(world.ID, 10) + "]")
	if got := htmlassert.Text(doc.MustHave("p.later-notes-comment")); got != "Big" {
		t.Errorf("comment = %q, want Big", got)
	}
	if n := len(doc.QueryAll("p.later-notes-comment")); n != 1 {
		t.Errorf("got %d comments, want 1 (only world has one)", n)
	}
	doc.MustNotHave("li.later-notes-empty")
}

func TestNotesPanelMarksAStaleHighlight(t *testing.T) {
	s := newServer(t)
	a := seedHello(t, s)
	id, err := s.Store.InsertHighlightForTest(context.Background(), a.ID, 6, 11, "nope")
	if err != nil {
		t.Fatal(err)
	}
	doc := s.Get(t, s.Alice, articlePath(a, ""))
	doc.MustHave("span.later-notes-quote")
	doc.MustHave(".later-notes-stale")
	if got := htmlassert.Text(doc.MustHave(".later-notes-stale")); got != "Not found in the text" {
		t.Errorf("stale text = %q", got)
	}
	doc.MustNotHave("a[href=#later-h-" + strconv.FormatInt(id, 10) + "]")
}

func TestSavingTheNoteOverHTMXRefreshesTheOtherCopy(t *testing.T) {
	s := newServer(t)
	a := seedReadable(t, s)
	rec := s.PostHX(t, s.Alice, articlePath(a, "/note"), url.Values{"note": {" Hi\r\nthere "}, "copy": {"panel"}})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	got, err := s.Store.Article(context.Background(), s.Alice.User.ID, a.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Note != "Hi\nthere" {
		t.Errorf("stored note = %q, want %q", got.Note, "Hi\nthere")
	}
	doc := htmlassert.Parse(t, rec.Body.String())
	end := doc.MustHave("textarea#later-note-end")
	if v, _ := htmlassert.Attr(end, "hx-swap-oob"); v != "true" {
		t.Errorf("end textarea hx-swap-oob = %q, want true", v)
	}
	if text := rawText(end); text != "Hi\nthere" {
		t.Errorf("end textarea = %q, want the saved note", text)
	}
	status := doc.MustHave("span#later-note-status-panel")
	if v, _ := htmlassert.Attr(status, "hx-swap-oob"); v != "true" {
		t.Errorf("status hx-swap-oob = %q, want true", v)
	}
	if text := htmlassert.Text(status); text != "Saved" {
		t.Errorf("status = %q, want Saved", text)
	}
	doc.MustNotHave("#later-note-panel")
}

func TestSavingTheNoteWithoutJSRedirects(t *testing.T) {
	s := newServer(t)
	a := seedReadable(t, s)
	rec := s.Post(t, s.Alice, articlePath(a, "/note"), url.Values{"note": {"x"}, "copy": {"end"}})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != articlePath(a, "") {
		t.Errorf("Location = %q, want %q", loc, articlePath(a, ""))
	}
}

func TestNoteOfAnotherUserIs404(t *testing.T) {
	s := newServer(t)
	a := seedReadable(t, s)
	rec := s.Post(t, s.Bob, articlePath(a, "/note"), url.Values{"note": {"x"}, "copy": {"end"}})
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
	got, err := s.Store.Article(context.Background(), s.Alice.User.ID, a.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Note != "" {
		t.Errorf("Bob changed Alice's note to %q", got.Note)
	}
}

func TestLinkOnlyArticleCanHaveANote(t *testing.T) {
	s := newServer(t)
	a := seedLinkOnly(t, s)
	doc := s.Get(t, s.Alice, articlePath(a, ""))
	doc.MustHave(".later-note-end textarea#later-note-end")

	rec := s.Post(t, s.Alice, articlePath(a, "/note"), url.Values{"note": {"why I saved it"}, "copy": {"end"}})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303", rec.Code)
	}
	got, err := s.Store.Article(context.Background(), s.Alice.User.ID, a.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Note != "why I saved it" {
		t.Errorf("note = %q", got.Note)
	}
}

// postHighlight posts a highlight over the given code points of a's text.
func postHighlight(t *testing.T, s *server, a later.Article, start, end int, comment string) *httptest.ResponseRecorder {
	t.Helper()
	r := []rune(a.ContentText)
	return s.PostHX(t, s.Alice, articlePath(a, "/highlights"), url.Values{
		"start": {strconv.Itoa(start)}, "end": {strconv.Itoa(end)},
		"quote": {string(r[start:end])}, "comment": {comment},
	})
}

func storedHighlights(t *testing.T, s *server, a later.Article) []later.Highlight {
	t.Helper()
	hs, err := s.Store.Highlights(context.Background(), a.ID)
	if err != nil {
		t.Fatal(err)
	}
	return hs
}

func TestAddHighlightSwapsTheBodyAndRefreshesThePanel(t *testing.T) {
	s := newServer(t)
	a := seedHello(t, s)
	rec := postHighlight(t, s, a, 6, 11, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "<html") {
		t.Error("response is a whole page, want a fragment")
	}
	doc := htmlassert.Parse(t, rec.Body.String())
	doc.MustHave("div#later-body")
	doc.MustHave("div#later-body mark[data-highlight-id]")
	if got := htmlassert.Text(doc.MustHave("div#later-body mark")); got != "brave" {
		t.Errorf("mark = %q, want brave", got)
	}
	if got := attr(t, doc, "span#later-notes-count", "hx-swap-oob"); got != "true" {
		t.Errorf("count hx-swap-oob = %q, want true", got)
	}
	if got := htmlassert.Text(doc.MustHave("span#later-notes-count")); got != "1" {
		t.Errorf("count = %q, want 1", got)
	}
	if got := attr(t, doc, "ol#later-notes-list", "hx-swap-oob"); got != "true" {
		t.Errorf("list hx-swap-oob = %q, want true", got)
	}
	if got := htmlassert.Text(doc.MustHave("ol#later-notes-list li.later-notes-item")); !strings.Contains(got, "brave") {
		t.Errorf("list item = %q, want the quote", got)
	}
}

func TestAddHighlightWithAComment(t *testing.T) {
	s := newServer(t)
	a := seedHello(t, s)
	rec := postHighlight(t, s, a, 6, 11, "Why?")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	hs := storedHighlights(t, s, a)
	if len(hs) != 1 || hs[0].Comment != "Why?" {
		t.Fatalf("stored = %+v, want one highlight with comment Why?", hs)
	}
	doc := htmlassert.Parse(t, rec.Body.String())
	doc.MustHave("mark.later-hl-commented")
	if got := htmlassert.Text(doc.MustHave("p.later-notes-comment")); got != "Why?" {
		t.Errorf("comment = %q, want Why?", got)
	}
}

func assertHighlightRejected(t *testing.T, rec *httptest.ResponseRecorder, want string) {
	t.Helper()
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/plain") {
		t.Errorf("Content-Type = %q, want text/plain", ct)
	}
	if got := rec.Body.String(); got != want {
		t.Errorf("body = %q, want %q", got, want)
	}
}

func TestAddHighlightRefusesAnOverlap(t *testing.T) {
	s := newServer(t)
	a := seedHello(t, s)
	if rec := postHighlight(t, s, a, 6, 15, ""); rec.Code != http.StatusOK {
		t.Fatalf("first status = %d, want 200", rec.Code)
	}
	assertHighlightRejected(t, postHighlight(t, s, a, 12, 21, ""), "That overlaps one of your highlights. Select a different passage.")
	if n := len(storedHighlights(t, s, a)); n != 1 {
		t.Errorf("got %d highlights, want 1", n)
	}
}

func TestAddHighlightRefusesAMismatchedQuote(t *testing.T) {
	s := newServer(t)
	a := seedHello(t, s)
	rec := s.PostHX(t, s.Alice, articlePath(a, "/highlights"), url.Values{
		"start": {"6"}, "end": {"11"}, "quote": {"brane"},
	})
	assertHighlightRejected(t, rec, "Couldn't highlight that selection. Try selecting it again.")
	if n := len(storedHighlights(t, s, a)); n != 0 {
		t.Errorf("got %d highlights, want 0", n)
	}
}

func TestAddHighlightRefusesGarbageOffsets(t *testing.T) {
	s := newServer(t)
	a := seedHello(t, s)
	rec := s.PostHX(t, s.Alice, articlePath(a, "/highlights"), url.Values{
		"start": {"abc"}, "end": {"11"}, "quote": {"brave"},
	})
	assertHighlightRejected(t, rec, "Couldn't highlight that selection. Try selecting it again.")
}

func TestAddHighlightOnALinkOnlyArticleIs422(t *testing.T) {
	s := newServer(t)
	a := seedLinkOnly(t, s)
	rec := s.PostHX(t, s.Alice, articlePath(a, "/highlights"), url.Values{
		"start": {"0"}, "end": {"3"}, "quote": {"abc"},
	})
	assertHighlightRejected(t, rec, "Couldn't highlight that selection. Try selecting it again.")
}

func TestAddHighlightOnAnotherUsersArticleIs404(t *testing.T) {
	s := newServer(t)
	a := seedHello(t, s)
	rec := s.PostHX(t, s.Bob, articlePath(a, "/highlights"), url.Values{
		"start": {"6"}, "end": {"11"}, "quote": {"brave"},
	})
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
	if n := len(storedHighlights(t, s, a)); n != 0 {
		t.Errorf("got %d highlights, want 0", n)
	}
}

func TestAddHighlightWithoutHTMXRedirects(t *testing.T) {
	s := newServer(t)
	a := seedHello(t, s)
	rec := s.Post(t, s.Alice, articlePath(a, "/highlights"), url.Values{
		"start": {"6"}, "end": {"11"}, "quote": {"brave"},
	})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != articlePath(a, "") {
		t.Errorf("Location = %q, want %q", loc, articlePath(a, ""))
	}
	if n := len(storedHighlights(t, s, a)); n != 1 {
		t.Errorf("got %d highlights, want 1", n)
	}
}

func TestArticleHasTheHighlightPopover(t *testing.T) {
	s := newServer(t)
	a := seedHello(t, s)
	doc := s.Get(t, s.Alice, articlePath(a, ""))
	doc.MustHave("script[src=/later/highlight.js]")
	form := "form#later-hl-new"
	doc.MustHave(form)
	for name, want := range map[string]string{
		"hx-post":   articlePath(a, "/highlights"),
		"hx-target": "#later-body",
		"hx-swap":   "outerHTML",
	} {
		if got := attr(t, doc, form, name); got != want {
			t.Errorf("%s = %q, want %q", name, got, want)
		}
	}
	if _, ok := htmlassert.Attr(doc.MustHave(form), "hidden"); !ok {
		t.Error("popover is not hidden")
	}
	doc.MustHave(form + " input[name=start]")
	doc.MustHave(form + " input[name=end]")
	doc.MustHave(form + " input[name=quote]")
	doc.MustHave(form + " textarea[name=comment]")

	lo := seedLinkOnly(t, s)
	doc = s.Get(t, s.Alice, articlePath(lo, ""))
	doc.MustNotHave(form)
}

func TestHighlightScriptIsServed(t *testing.T) {
	s := newServer(t)
	rec := s.Do(t, s.Alice, httptest.NewRequest("GET", "/later/highlight.js", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "text/javascript; charset=utf-8" {
		t.Errorf("Content-Type = %q", ct)
	}
	if !strings.Contains(rec.Body.String(), "later-hl-new") {
		t.Error("script does not mention later-hl-new")
	}
}
