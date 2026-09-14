// internal/ui/toolbar_icons_test.go
package ui_test

import (
	"strings"
	"testing"

	"github.com/iliafrenkel/on-suite/internal/ui"
)

func TestToolbarIconForKnownNames(t *testing.T) {
	names := []string{
		"more", "plus", "refresh", "import", "export", "folder",
		"keyboard", "stats", "close", "check", "star-filled",
		"star-outline", "external", "doc", "inbox",
	}
	seen := map[string]bool{}
	for _, name := range names {
		got := string(ui.ToolbarIconFor(name))
		if !strings.Contains(got, "<svg") {
			t.Errorf("ToolbarIconFor(%q) = %q, want it to contain <svg", name, got)
		}
		if !strings.Contains(got, `class="toolbar-icon"`) {
			t.Errorf("ToolbarIconFor(%q) is missing class=\"toolbar-icon\"", name)
		}
		if seen[got] {
			t.Errorf("ToolbarIconFor(%q) duplicates an earlier icon", name)
		}
		seen[got] = true
	}
}

func TestToolbarIconForUnknownNameIsEmpty(t *testing.T) {
	if got := ui.ToolbarIconFor("no-such-icon"); got != "" {
		t.Errorf("ToolbarIconFor(unknown) = %q, want empty", got)
	}
}
