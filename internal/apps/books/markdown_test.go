package books_test

import (
	"testing"

	"github.com/iliafrenkel/on-suite/internal/apps/books"
)

func TestRenderReview(t *testing.T) {
	tests := []struct {
		name, in, want string
	}{
		{"empty", "", ""},
		{"paragraphs and line breaks", "One\ntwo\n\n\nThree", "<p>One<br>two</p><p>Three</p>"},
		{"inline", "**Loved** it, *mostly* — ~~not~~ `x*y`",
			"<p><strong>Loved</strong> it, <em>mostly</em> — <s>not</s> <code>x*y</code></p>"},
		{"link", "[OL](https://openlibrary.org/works/OL1W)",
			`<p><a href="https://openlibrary.org/works/OL1W" target="_blank" rel="noopener noreferrer">OL</a></p>`},
		{"bare link", "see https://example.com/a_(b) too",
			`<p>see <a href="https://example.com/a_(b)" target="_blank" rel="noopener noreferrer">https://example.com/a_(b)</a> too</p>`},
		{"no javascript links", "[x](javascript:alert(1))", "<p>[x](javascript:alert(1))</p>"},
		{"uppercase javascript link", "[x](JAVASCRIPT:alert(1))", "<p>[x](JAVASCRIPT:alert(1))</p>"},
		{"quote in a url stays in the attribute", `[x](https://a.example/"onmouseover=alert(1))`,
			`<p><a href="https://a.example/&#34;onmouseover=alert(1" target="_blank" rel="noopener noreferrer">x</a>)</p>`},
		{"html is text", "<script>alert(1)</script> & co", "<p>&lt;script&gt;alert(1)&lt;/script&gt; &amp; co</p>"},
		{"windows line ends", "a\r\n\r\nb", "<p>a</p><p>b</p>"},
	}
	for _, tt := range tests {
		if got := string(books.RenderReview(tt.in)); got != tt.want {
			t.Errorf("%s: RenderReview(%q) =\n %s\nwant\n %s", tt.name, tt.in, got, tt.want)
		}
	}
}
