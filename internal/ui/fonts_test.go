package ui_test

import (
	"io/fs"
	"regexp"
	"strings"
	"testing"

	"github.com/iliafrenkel/on-suite/internal/ui"
)

// TestTypographyRolesAndBundledFonts pins the Braun Folio four-role typography
// contract: UI chrome uses --font-ui, display headings use --font-display,
// long-form reading surfaces use --font-body, instrument/code uses --font-mono,
// retired data-font/data-palette selectors are absent, chamfered primary
// buttons keep an unclipped focus-visible ring, and every declared self-hosted
// font file exists in the embedded static FS.
func TestTypographyRolesAndBundledFonts(t *testing.T) {
	src, err := fs.ReadFile(ui.Static(), "app.css")
	if err != nil {
		t.Fatal(err)
	}
	css := string(src)

	if strings.Contains(css, `[data-font=`) || strings.Contains(css, `[data-palette=`) {
		t.Error("app.css must not contain retired data-font or data-palette selectors")
	}

	sheet := parseCSS(t, css)
	sheet.mustDeclare("", "body", "var(--font-ui)")
	sheet.mustDeclare("", "h1", "font-family: var(--font-display)")
	sheet.mustDeclare("", ".reader-body", "font-family: var(--font-body)")
	sheet.mustDeclare("", ".books-quote-text", "font-family: var(--font-body)")
	sheet.mustDeclare("", ".help-body", "font-family: var(--font-body)")
	sheet.mustDeclare("", "code", "font-family: var(--font-mono)")
	// clip-path cuts outlines, so the chamfered primary uses an inset ring.
	sheet.mustDeclare("", ".primary:focus-visible", "box-shadow: inset")

	urlRe := regexp.MustCompile(`url\("/static/(fonts/[^"]+)"\)`)
	matches := urlRe.FindAllStringSubmatch(css, -1)
	if len(matches) == 0 {
		t.Fatal("app.css declares no self-hosted font URLs")
	}
	for _, m := range matches {
		if _, err := fs.Stat(ui.Static(), m[1]); err != nil {
			t.Errorf("missing bundled font file %q: %v", m[1], err)
		}
	}
}
