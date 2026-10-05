package later_test

import (
	"context"
	"net/http"
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
