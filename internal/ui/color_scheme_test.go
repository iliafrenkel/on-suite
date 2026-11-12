package ui_test

import (
	"io/fs"
	"regexp"
	"testing"

	"github.com/iliafrenkel/on-suite/internal/ui"
)

// TestThemeBlocksSetColorScheme keeps native UI (scrollbars, form controls)
// in step with the theme toggle (#616). Chrome only darkens its scrollbars
// when color-scheme says so; it does not infer it from the background the
// way Firefox and Safari do.
func TestThemeBlocksSetColorScheme(t *testing.T) {
	src, err := fs.ReadFile(ui.Static(), "app.css")
	if err != nil {
		t.Fatal(err)
	}
	cases := map[string]*regexp.Regexp{
		"light": regexp.MustCompile(`(?s)\n:root \{[^}]*color-scheme:\s*light;`),
		"dark":  regexp.MustCompile(`(?s)\n:root\[data-theme="dark"\] \{[^}]*color-scheme:\s*dark;`),
	}
	for theme, re := range cases {
		if !re.Match(src) {
			t.Errorf("app.css: %s theme block does not set color-scheme: %s", theme, theme)
		}
	}
}
