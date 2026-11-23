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

	checks := []struct {
		name string
		re   string
	}{
		{"body uses --font-ui", `(?s)\nbody \{[^}]*var\(--font-ui\)`},
		{"headings use --font-display", `(?s)\nh1[^{]*\{[^}]*var\(--font-display\)`},
		{"reading surfaces use --font-body", `(?s)font-family:\s*var\(--font-body\)`},
		{"code uses --font-mono", `(?s)code,\s*pre,\s*kbd\s*\{[^}]*var\(--font-mono\)`},
		{"primary button has unclipped focus-visible", `(?s)\.primary:focus-visible[^{]*\{[^}]*box-shadow:`},
	}
	for _, c := range checks {
		if !regexp.MustCompile(c.re).MatchString(css) {
			t.Errorf("app.css missing contract: %s", c.name)
		}
	}

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
