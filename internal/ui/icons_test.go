package ui_test

import (
	"strings"
	"testing"

	"github.com/iliafrenkel/on-suite/internal/ui"
)

func TestIconForKnownApps(t *testing.T) {
	for _, id := range []string{"paste", "notes", "reader", "flash", "admin"} {
		got := string(ui.IconFor(id))
		if !strings.Contains(got, "<svg") {
			t.Errorf("IconFor(%q) = %q, want it to contain <svg", id, got)
		}
		if !strings.Contains(got, "viewBox") {
			t.Errorf("IconFor(%q) has no viewBox", id)
		}
	}
}

func TestIconForUnknownAppFallsBackToTile(t *testing.T) {
	got := string(ui.IconFor("some-future-app"))
	if !strings.Contains(got, "<svg") {
		t.Errorf("IconFor(unknown) = %q, want it to contain <svg", got)
	}
	// The fallback must not be empty, and must differ from a known icon.
	if got == string(ui.IconFor("paste")) {
		t.Error("fallback icon is identical to the paste icon")
	}
}

func TestIconForIsDistinctPerApp(t *testing.T) {
	seen := map[string]bool{}
	for _, id := range []string{"paste", "notes", "reader", "flash", "admin"} {
		svg := string(ui.IconFor(id))
		if seen[svg] {
			t.Errorf("icon for %q duplicates an earlier icon", id)
		}
		seen[svg] = true
	}
}

func TestIconStrokeWidthIsConsistent(t *testing.T) {
	for _, id := range []string{"paste", "notes", "reader", "admin", "flash"} {
		got := string(ui.IconFor(id))
		if strings.Contains(got, `stroke-width="1.8"`) {
			t.Errorf("IconFor(%q) still uses stroke-width 1.8, want the shared 1.5 line-icon weight", id)
		}
		if !strings.Contains(got, `stroke-width="1.5"`) {
			t.Errorf("IconFor(%q) has no stroke-width=1.5 stroke", id)
		}
	}
}
