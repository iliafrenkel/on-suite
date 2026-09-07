package notes

import (
	"html/template"
	"strings"
	"testing"
)

func TestMatchSpansFindsACaseInsensitiveMatch(t *testing.T) {
	spans := matchSpans("Buy Milk", []string{"milk"})
	if len(spans) != 1 || spans[0].start != 4 || spans[0].end != 8 {
		t.Fatalf("matchSpans = %+v, want one span covering \"Milk\"", spans)
	}
}

func TestMatchSpansPicksTheLongestOverlappingTerm(t *testing.T) {
	// "cat" and "category" both start at the same position; wrapping "cat"
	// alone first would leave "egory" behind as unmatched, or double-wrap.
	spans := matchSpans("a category", []string{"cat", "category"})
	if len(spans) != 1 || spans[0].end-spans[0].start != len("category") {
		t.Fatalf("matchSpans = %+v, want one span covering the whole word", spans)
	}
}

func TestMatchSpansReturnsNoneForEmptyTerms(t *testing.T) {
	if spans := matchSpans("anything", nil); spans != nil {
		t.Fatalf("matchSpans with no terms = %+v, want nil", spans)
	}
}

func TestHighlightWrapsAPlainTextMatch(t *testing.T) {
	got := highlight(template.HTML("buy milk today"), []string{"milk"})
	if !strings.Contains(string(got), `<mark class="notes-search-hit">milk</mark>`) {
		t.Errorf("highlight = %q, want a wrapped mark", got)
	}
}

// TestHighlightNeverMatchesInsideATag is the whole reason this runs on the
// rendered tree rather than as a string replace: an href is not visible
// text, and a false match there must never become a mark.
func TestHighlightNeverMatchesInsideATag(t *testing.T) {
	rendered := template.HTML(`<a href="https://milk.example.com">buy stuff</a>`)
	got := highlight(rendered, []string{"milk"})
	if strings.Contains(string(got), "<mark") {
		t.Errorf("highlight = %q, matched inside the href", got)
	}
	if !strings.Contains(string(got), `href="https://milk.example.com"`) {
		t.Errorf("highlight = %q, corrupted the href", got)
	}
}

// TestHighlightWorksAroundExistingTags proves a match spanning across what
// was already <strong>/<em>/etc. still gets wrapped without breaking that
// markup — Render's own output, not raw Markdown source, is what this sees.
func TestHighlightWorksAroundExistingTags(t *testing.T) {
	rendered := template.HTML(`buy <strong>whole milk</strong> today`)
	got := highlight(rendered, []string{"milk"})
	if !strings.Contains(string(got), `<strong>whole <mark class="notes-search-hit">milk</mark></strong>`) {
		t.Errorf("highlight = %q, want the mark nested inside strong", got)
	}
}

func TestHighlightWithNoTermsReturnsInputUnchanged(t *testing.T) {
	rendered := template.HTML(`<strong>hi</strong>`)
	if got := highlight(rendered, nil); got != rendered {
		t.Errorf("highlight with no terms = %q, want the input unchanged", got)
	}
}

func TestHighlightPlainTextEscapesAndWraps(t *testing.T) {
	got := highlightPlainText(`<b>milk</b> & eggs`, []string{"milk"})
	want := `&lt;b&gt;<mark class="notes-search-hit">milk</mark>&lt;/b&gt; &amp; eggs`
	if string(got) != want {
		t.Errorf("highlightPlainText = %q, want %q", got, want)
	}
}

func TestHighlightPlainTextWithNoMatchJustEscapes(t *testing.T) {
	got := highlightPlainText("plain <text>", nil)
	if string(got) != "plain &lt;text&gt;" {
		t.Errorf("highlightPlainText = %q, want escaped with no marks", got)
	}
}

func TestNoteSnippetIsEmptyWithNoMatch(t *testing.T) {
	if got := noteSnippet("nothing relevant here", []string{"milk"}); got != "" {
		t.Errorf("noteSnippet with no match = %q, want empty", got)
	}
}

func TestNoteSnippetHighlightsAndTruncates(t *testing.T) {
	long := strings.Repeat("x", 80) + " milk " + strings.Repeat("y", 80)
	got := noteSnippet(long, []string{"milk"})
	s := string(got)
	if !strings.Contains(s, `<mark class="notes-search-hit">milk</mark>`) {
		t.Errorf("noteSnippet = %q, want the match highlighted", s)
	}
	if !strings.HasPrefix(s, "…") || !strings.HasSuffix(s, "…") {
		t.Errorf("noteSnippet = %q, want ellipses on both sides", s)
	}
	if len(s) >= len(long) {
		t.Errorf("noteSnippet did not truncate: got %d bytes from a %d-byte note", len(s), len(long))
	}
}

func TestNoteSnippetKeepsShortNoteWholeWithNoEllipsis(t *testing.T) {
	got := noteSnippet("short milk note", []string{"milk"})
	s := string(got)
	if strings.Contains(s, "…") {
		t.Errorf("noteSnippet = %q, a short note should not be truncated", s)
	}
}
