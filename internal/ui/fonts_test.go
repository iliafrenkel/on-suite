package ui_test

import (
	"io/fs"
	"regexp"
	"testing"

	"github.com/iliafrenkel/on-suite/internal/ui"
)

// TestMixedFontKeepsChromeSans pins the Mixed pairing (#617): the page is
// set in --font-ui, and Mixed changes only --font-body, so menus, toolbars
// and buttons stay sans while reading text turns serif.
func TestMixedFontKeepsChromeSans(t *testing.T) {
	src, err := fs.ReadFile(ui.Static(), "app.css")
	if err != nil {
		t.Fatal(err)
	}
	body := regexp.MustCompile(`(?s)\nbody \{[^}]*font:[^;]*var\(--font-ui\);`)
	if !body.Match(src) {
		t.Error("app.css: body must use --font-ui, so only reading surfaces follow --font-body")
	}
	mixed := regexp.MustCompile(`(?s)\[data-font="mixed"\] \{([^}]*)\}`).FindSubmatch(src)
	if mixed == nil {
		t.Fatal(`app.css: no [data-font="mixed"] block`)
	}
	if !regexp.MustCompile(`--font-body:[^;]*serif`).Match(mixed[1]) {
		t.Error("Mixed must set --font-body to a serif")
	}
	if regexp.MustCompile(`--font-ui:`).Match(mixed[1]) {
		t.Error("Mixed must not change --font-ui")
	}
}
