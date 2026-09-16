// internal/ui/toolbar_icons_test.go
package ui_test

import (
	"os"
	"path/filepath"
	"regexp"
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

var ticonCallPattern = regexp.MustCompile(`\{\{\s*ticon\s+"([^"]+)"`)

// TestEveryTiconCallResolves greps every .html template in the repo for
// {{ticon "..."}} calls and asserts each name resolves to a real icon.
//
// ToolbarIconFor deliberately has no fallback glyph for an unknown name — see
// its own doc comment — so a typo'd name renders as nothing at all, with no
// build error and no test failure, unless something ties template usage to
// the map. This is that something (issue #249).
func TestEveryTiconCallResolves(t *testing.T) {
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	// Fail loudly rather than silently scanning nothing.
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatalf("cannot find the module root from %s: %v", root, err)
	}

	seen := map[string]bool{}
	err = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		if filepath.Ext(path) != ".html" {
			return nil
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, m := range ticonCallPattern.FindAllSubmatch(body, -1) {
			name := string(m[1])
			seen[name] = true
			if ui.ToolbarIconFor(name) == "" {
				t.Errorf("%s calls {{ticon %q}}, which does not resolve to a known icon", path, name)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(seen) == 0 {
		t.Fatal("found no {{ticon \"...\"}} calls in any .html file; the scan is broken")
	}
}
