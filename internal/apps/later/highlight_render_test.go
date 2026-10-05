package later_test

import (
	"strings"
	"testing"

	"github.com/iliafrenkel/on-suite/internal/apps/later"
)

// hl builds a highlight over text[start:end] of fragment's ContentText.
func hl(fragment string, id int64, start, end int, comment string) later.Highlight {
	r := []rune(later.ContentText(fragment))
	return later.Highlight{ID: id, Start: start, End: end, Quote: string(r[start:end]), Comment: comment}
}

func TestRenderHighlights(t *testing.T) {
	const one = `class="later-hl later-hl-end" data-highlight-id="1" id="later-h-1"`
	for _, c := range []struct {
		name, in string
		hs       func(in string) []later.Highlight
		want     string
	}{
		{"none", `<p>Hello <em>brave</em> world</p>`,
			func(string) []later.Highlight { return nil },
			`<p>Hello <em>brave</em> world</p>`},
		{"inside one text node", `<p>Hello brave world</p>`,
			func(in string) []later.Highlight { return []later.Highlight{hl(in, 1, 6, 11, "")} },
			`<p>Hello <mark ` + one + `>brave</mark> world</p>`},
		{"across an inline element", `<p>one <em>two</em> three</p>`,
			func(in string) []later.Highlight { return []later.Highlight{hl(in, 1, 2, 10, "")} },
			`<p>on<mark class="later-hl" data-highlight-id="1" id="later-h-1">e </mark>` +
				`<em><mark class="later-hl" data-highlight-id="1">two</mark></em>` +
				`<mark class="later-hl later-hl-end" data-highlight-id="1"> th</mark>ree</p>`},
		{"across paragraphs", `<p>ab</p><p>cd</p>`,
			func(in string) []later.Highlight { return []later.Highlight{hl(in, 1, 1, 3, "")} },
			`<p>a<mark class="later-hl" data-highlight-id="1" id="later-h-1">b</mark></p>` +
				`<p><mark class="later-hl later-hl-end" data-highlight-id="1">c</mark>d</p>`},
		{"multi-byte text", `<p>Привет 👋 שלום</p>`,
			func(in string) []later.Highlight { return []later.Highlight{hl(in, 1, 7, 8, "")} },
			`<p>Привет <mark ` + one + `>👋</mark> שלום</p>`},
		{"entities count as one character", `<p>a &amp; b</p>`,
			func(in string) []later.Highlight { return []later.Highlight{hl(in, 1, 2, 3, "")} },
			`<p>a <mark ` + one + `>&amp;</mark> b</p>`},
		{"commented", `<p>Hello brave world</p>`,
			func(in string) []later.Highlight { return []later.Highlight{hl(in, 1, 6, 11, "why")} },
			`<p>Hello <mark class="later-hl later-hl-commented later-hl-end" data-highlight-id="1" id="later-h-1">brave</mark> world</p>`},
		{"two in one node, given out of order", `<p>Hello brave world</p>`,
			func(in string) []later.Highlight {
				return []later.Highlight{hl(in, 2, 12, 17, ""), hl(in, 1, 0, 5, "")}
			},
			`<p><mark ` + one + `>Hello</mark> brave <mark class="later-hl later-hl-end" data-highlight-id="2" id="later-h-2">world</mark></p>`},
		{"stale quote is not drawn", `<p>Hello brave world</p>`,
			func(string) []later.Highlight { return []later.Highlight{{ID: 1, Start: 6, End: 11, Quote: "brane"}} },
			`<p>Hello brave world</p>`},
		{"overlapping stored highlights: the later one is skipped", `<p>Hello brave world</p>`,
			func(in string) []later.Highlight {
				return []later.Highlight{hl(in, 1, 0, 11, ""), hl(in, 2, 6, 17, "")}
			},
			`<p><mark ` + one + `>Hello brave</mark> world</p>`},
	} {
		t.Run(c.name, func(t *testing.T) {
			text := later.ContentText(c.in)
			got := later.RenderHighlights(c.in, text, c.hs(c.in))
			if got != c.want {
				t.Errorf("RenderHighlights =\n%s\nwant\n%s", got, c.want)
			}
			if later.ContentText(got) != text {
				t.Errorf("drawing changed the text: %q, want %q", later.ContentText(got), text)
			}
		})
	}
}

func TestRenderHighlightsNeverPutsAMarkInATableSection(t *testing.T) {
	// The newlines are text children of <table>/<tbody>; a <mark> there would
	// be foster-parented out of the table by the browser, moving the text and
	// breaking every offset after it.
	in := "<table>\n<tr><td>a</td></tr>\n<tr><td>b</td></tr></table>"
	text := later.ContentText(in) // "\na\nb"
	got := later.RenderHighlights(in, text, []later.Highlight{hl(in, 1, 1, 4, "")})
	if n := strings.Count(got, "<mark"); n != 2 {
		t.Errorf("got %d marks, want 2 (one per cell): %s", n, got)
	}
	if later.ContentText(got) != text {
		t.Errorf("drawing changed the text: %q", later.ContentText(got))
	}
}
