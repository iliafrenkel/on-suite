package paste_test

import (
	"strings"
	"testing"

	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/iliafrenkel/on-suite/internal/apps/paste"
)

// TestEveryOfferedLanguageResolves turns a typo in the curated list into a
// test failure instead of a snippet that silently renders as plain text.
func TestEveryOfferedLanguageResolves(t *testing.T) {
	for _, l := range paste.Languages() {
		if l.Value == "" {
			continue // the detect-automatically entry
		}
		if lexers.Get(l.Value) == nil {
			t.Errorf("language %q (%s) does not resolve to a Chroma lexer", l.Value, l.Label)
		}
		if l.Label == "" {
			t.Errorf("language %q has no label", l.Value)
		}
	}
}

// TestHighlightEmitsNoInlineStyles is the load-bearing test of this task. The
// suite's CSP forbids inline styles, so Chroma's default formatter would
// produce a completely unstyled page. If this fails, the page silently loses
// all highlighting in a browser while every other test still passes.
func TestHighlightEmitsNoInlineStyles(t *testing.T) {
	out := string(paste.Highlight("package main\n\nfunc main() {}\n", "go"))

	if strings.Contains(out, "style=") {
		t.Fatalf("Highlight emitted an inline style attribute, which the CSP blocks:\n%s", out)
	}
	if !strings.Contains(out, "class=") {
		t.Fatalf("Highlight emitted no classes, so the stylesheet cannot colour it:\n%s", out)
	}
}

// TestHighlightEscapesHTML guards the reason Highlight may return
// template.HTML at all.
func TestHighlightEscapesHTML(t *testing.T) {
	const hostile = "var x = \"<script>alert(1)</script>\";\n"
	out := string(paste.Highlight(hostile, "javascript"))

	if strings.Contains(out, "<script>") {
		t.Fatalf("a script tag survived highlighting unescaped:\n%s", out)
	}
	if !strings.Contains(out, "&lt;script&gt;") {
		t.Errorf("the escaped form is missing; output was:\n%s", out)
	}
}

func TestHighlightHandlesAwkwardInput(t *testing.T) {
	tests := []struct{ name, body, language string }{
		{"empty body", "", "go"},
		{"unknown language", "hello\n", "klingon"},
		{"detect from content", "package main\nfunc main() {}\n", ""},
		{"plain text", "just words\n", "plaintext"},
		{"no trailing newline", "x = 1", "python"},
		{"unicode", "s := \"日本語 🎉\"\n", "go"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out := string(paste.Highlight(tt.body, tt.language))
			if strings.Contains(out, "style=") {
				t.Error("inline style attribute in output")
			}
			// It must always produce something renderable.
			if tt.body != "" && out == "" {
				t.Error("Highlight returned nothing for a non-empty body")
			}
		})
	}
}

func TestHighlightCSS(t *testing.T) {
	css, err := paste.HighlightCSS()
	if err != nil {
		t.Fatalf("HighlightCSS: %v", err)
	}
	s := string(css)

	if len(css) < 1000 {
		t.Errorf("stylesheet is only %d bytes, which is too small to be real", len(css))
	}
	if !strings.Contains(s, ".chroma") {
		t.Error("stylesheet does not mention .chroma")
	}
	if !strings.Contains(s, "prefers-color-scheme: dark") {
		t.Error("stylesheet has no dark variant")
	}
	// The overrides must come after Chroma's own rules, or the background
	// fights the design system.
	if strings.Index(s, "ON Suite overrides") < strings.LastIndex(s, "prefers-color-scheme") {
		t.Error("the overrides are not last, so they will not win at equal specificity")
	}
	// Balanced braces, since the dark block is hand-wrapped in a media query.
	if strings.Count(s, "{") != strings.Count(s, "}") {
		t.Errorf("unbalanced braces: %d open, %d close", strings.Count(s, "{"), strings.Count(s, "}"))
	}
}

func TestLanguageLabel(t *testing.T) {
	if got := paste.LanguageLabel("go"); got != "Go" {
		t.Errorf("LanguageLabel(go) = %q", got)
	}
	if got := paste.LanguageLabel("klingon"); got != "Plain text" {
		t.Errorf("LanguageLabel of an unknown value = %q, want a safe default", got)
	}
}

func TestIsLanguage(t *testing.T) {
	for _, v := range []string{"", "go", "plaintext", "yaml"} {
		if !paste.IsLanguage(v) {
			t.Errorf("IsLanguage(%q) = false", v)
		}
	}
	for _, v := range []string{"klingon", "GO", "go ", "'; DROP TABLE"} {
		if paste.IsLanguage(v) {
			t.Errorf("IsLanguage(%q) = true", v)
		}
	}
}
