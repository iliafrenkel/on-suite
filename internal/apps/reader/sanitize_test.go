package reader_test

import (
	"strings"
	"testing"

	"github.com/iliafrenkel/on-suite/internal/apps/reader"
)

// The corpus is the point of this file. Each case names the attack it stands
// for, so a future change that regresses one says which one.
func TestSanitizeHTMLStripsHostileMarkup(t *testing.T) {
	cases := []struct {
		name     string
		in       string
		mustNot  []string
		mustHave []string
	}{
		{
			name:     "script element",
			in:       `<p>hi</p><script>alert(1)</script>`,
			mustNot:  []string{"<script", "alert(1)"},
			mustHave: []string{"<p>hi</p>"},
		},
		{
			name:    "event handler attribute",
			in:      `<p onclick="steal()">hi</p>`,
			mustNot: []string{"onclick", "steal"},
		},
		{
			name:    "javascript href",
			in:      `<a href="javascript:alert(1)">click</a>`,
			mustNot: []string{"javascript:"},
		},
		{
			name:    "data uri href",
			in:      `<a href="data:text/html;base64,PHNjcmlwdD4=">click</a>`,
			mustNot: []string{"data:text/html"},
		},
		{
			name:    "inline style, which the CSP forbids anyway",
			in:      `<p style="position:fixed;top:0">hi</p>`,
			mustNot: []string{"style="},
		},
		{
			name:    "svg payload",
			in:      `<svg><script>alert(1)</script></svg>`,
			mustNot: []string{"<svg", "<script"},
		},
		{
			name:    "iframe",
			in:      `<iframe src="https://evil.example/"></iframe>`,
			mustNot: []string{"<iframe"},
		},
		{
			name:    "form and input",
			in:      `<form action="https://evil.example/"><input name="p"></form>`,
			mustNot: []string{"<form", "<input"},
		},
		{
			name:     "R1 strips images entirely, proxy arrives in R3",
			in:       `<p>a</p><img src="https://tracker.example/px.gif">`,
			mustNot:  []string{"<img", "tracker.example"},
			mustHave: []string{"<p>a</p>"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := reader.SanitizeHTML(tc.in)
			for _, bad := range tc.mustNot {
				if strings.Contains(strings.ToLower(got), strings.ToLower(bad)) {
					t.Errorf("output still contains %q\ngot: %s", bad, got)
				}
			}
			for _, want := range tc.mustHave {
				if !strings.Contains(got, want) {
					t.Errorf("output lost %q\ngot: %s", want, got)
				}
			}
		})
	}
}

func TestSanitizeHTMLKeepsReadableProse(t *testing.T) {
	in := `<h2>Heading</h2><p>Some <em>emphasis</em>, a <strong>bold</strong> bit,` +
		` a <a href="https://example.com/x">link</a> and <code>code</code>.</p>` +
		`<blockquote><p>quoted</p></blockquote><ul><li>one</li></ul><pre>fenced</pre>`

	got := reader.SanitizeHTML(in)
	for _, want := range []string{"<h2>", "<em>", "<strong>", "<code>", "<blockquote>", "<ul>", "<li>", "<pre>", `href="https://example.com/x"`} {
		if !strings.Contains(got, want) {
			t.Errorf("sanitizer dropped %q from ordinary prose\ngot: %s", want, got)
		}
	}
}

func TestSanitizeHTMLHardensOutboundLinks(t *testing.T) {
	got := reader.SanitizeHTML(`<a href="https://example.com/x">link</a>`)

	// Check that all three rel tokens are present (order may vary)
	for _, token := range []string{"nofollow", "noopener", "noreferrer"} {
		if !strings.Contains(got, token) {
			t.Errorf("outbound link missing rel=%q\ngot: %s", token, got)
		}
	}
	if !strings.Contains(got, `target="_blank"`) {
		t.Errorf("outbound link does not open in a new tab\ngot: %s", got)
	}
}
