package books_test

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/iliafrenkel/on-suite/internal/htmlassert"
)

// formatIcon is the icon drawn before the format select: the markup
// between the control's opening tag and the select.
func formatIcon(body string) string {
	_, after, _ := strings.Cut(body, `class="books-format-control">`)
	icon, _, _ := strings.Cut(after, "<select")
	return strings.TrimSpace(icon)
}

func TestTheFormatSelectSetsTheReadingsFormat(t *testing.T) {
	s := newServer(t)
	id := readingBook(t, s, "Dune", 600)
	page := fmt.Sprintf("/books/b/%d?shelf=reading", id)
	doc := s.Get(t, s.Alice, page)
	doc.MustNotHave(".books-format-pill") // a control, not a tag (spec "Sections")
	form := doc.MustHave("form.books-format")
	for name, want := range map[string]string{
		"action": fmt.Sprintf("/books/format/%d", id), "hx-post": fmt.Sprintf("/books/format/%d", id),
		"hx-trigger": "change", "hx-target": "#books-book",
	} {
		if v, _ := htmlassert.Attr(form, name); v != want {
			t.Errorf("format form %s = %q, want %q", name, v, want)
		}
	}
	doc.MustHave("form.books-format noscript") // its Save button, without JavaScript
	var values []string
	for _, o := range doc.QueryAll("select#books-format option") {
		v, _ := htmlassert.Attr(o, "value")
		values = append(values, v)
	}
	if fmt.Sprint(values) != "[paper ebook audio ]" {
		t.Errorf("format options = %q", values)
	}
	if got := attr(t, doc, "select#books-format option[selected]", "value"); got != "" {
		t.Errorf("selected format = %q, want Not set", got)
	}
	if icon := formatIcon(s.Do(t, s.Alice, httptestGet(page)).Body.String()); icon != "" {
		t.Errorf("icon while the format isn't set = %q, want none", icon)
	}

	s.Submit(t, s.Alice, fmt.Sprintf("/books/format/%d", id), url.Values{"shelf": {"reading"}, "format": {"audio"}}, page)
	doc = s.Get(t, s.Alice, page)
	if got := attr(t, doc, "select#books-format option[selected]", "value"); got != "audio" {
		t.Errorf("selected format after choosing audio = %q", got)
	}
	if got := htmlassert.Text(doc.MustHave(".books-progress-of")); got != "%" {
		t.Errorf("progress unit after audio = %q, want %%", got)
	}

	if rec := s.Post(t, s.Alice, fmt.Sprintf("/books/format/%d", id), url.Values{"format": {"vinyl"}}); rec.Code != http.StatusBadRequest {
		t.Errorf("format vinyl = %d, want 400", rec.Code)
	}
	if rec := s.Post(t, s.Bob, fmt.Sprintf("/books/format/%d", id), url.Values{"format": {"paper"}}); rec.Code != http.StatusNotFound {
		t.Errorf("Bob's format = %d, want 404", rec.Code)
	}
}

func TestEachFormatHasItsOwnIcon(t *testing.T) {
	s := newServer(t)
	id := readingBook(t, s, "Dune", 600)
	seen := map[string]string{}
	for _, f := range []string{"paper", "ebook", "audio"} {
		if err := s.Store.SetFormat(context.Background(), s.Alice.User.ID, id, f); err != nil {
			t.Fatal(err)
		}
		icon := formatIcon(s.Do(t, s.Alice, httptestGet(fmt.Sprintf("/books/b/%d", id))).Body.String())
		if !strings.HasPrefix(icon, "<svg") {
			t.Errorf("%s: icon = %q, want an svg", f, icon)
		}
		if other, ok := seen[icon]; ok {
			t.Errorf("%s draws the same icon as %s", f, other)
		}
		seen[icon] = f
	}
}

func TestFinishOffersARatingAndDNFAPlace(t *testing.T) {
	s := newServer(t)
	id := readingBook(t, s, "Dune", 600)
	ctx := context.Background()
	if err := s.Store.SetRating(ctx, s.Alice.User.ID, id, 3); err != nil {
		t.Fatal(err)
	}
	if err := s.Store.RecordProgress(ctx, s.Alice.User.ID, id, 200); err != nil {
		t.Fatal(err)
	}
	doc := s.Get(t, s.Alice, fmt.Sprintf("/books/b/%d", id))
	if got := strings.Join(texts(doc, "details.books-close summary"), "|"); got != "Finish…|Did not finish…" {
		t.Errorf("close buttons = %q, want Finish… and Did not finish…", got)
	}
	if n := len(doc.QueryAll("select#books-finish-rating option")); n != 6 {
		t.Errorf("%d rating options, want No rating and 1–5", n)
	}
	if got := attr(t, doc, "select#books-finish-rating option[selected]", "value"); got != "3" {
		t.Errorf("selected rating = %q, want the book's 3", got)
	}
	// Both popovers share a name so the browser keeps one open at a time;
	// they are drawn at the same spot.
	for _, d := range doc.QueryAll("details.books-close") {
		if v, _ := htmlassert.Attr(d, "name"); v != "books-close" {
			t.Errorf("close popover name = %q, want books-close (one open at a time)", v)
		}
	}
	for _, f := range doc.QueryAll("details.books-close form") {
		if _, ok := htmlassert.Attr(f, "novalidate"); !ok {
			t.Errorf("close form %q lacks novalidate", htmlassert.Text(f))
		}
	}
	at := doc.MustHave("input#books-dnf-at")
	if v, _ := htmlassert.Attr(at, "max"); v != "600" {
		t.Errorf("DNF page max = %q, want 600", v)
	}
	if v, _ := htmlassert.Attr(at, "value"); v != "200" {
		t.Errorf("DNF page = %q, want the current page 200", v)
	}
}
