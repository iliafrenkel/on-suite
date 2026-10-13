package ui_test

import (
	"strings"
	"testing"

	"github.com/iliafrenkel/on-suite/internal/ui"
)

func TestIconForKnownApps(t *testing.T) {
	for _, id := range []string{"paste", "notes", "reader", "flash", "admin", "later", "focus", "books"} {
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
	for _, id := range []string{"paste", "notes", "reader", "flash", "admin", "later", "focus", "books"} {
		svg := string(ui.IconFor(id))
		if seen[svg] {
			t.Errorf("icon for %q duplicates an earlier icon", id)
		}
		seen[svg] = true
	}
}

func TestIconStrokeWidthIsConsistent(t *testing.T) {
	for _, id := range []string{"paste", "notes", "reader", "admin", "flash", "later", "focus", "books"} {
		got := string(ui.IconFor(id))
		if strings.Contains(got, `stroke-width="1.8"`) {
			t.Errorf("IconFor(%q) still uses stroke-width 1.8, want the shared 1.5 line-icon weight", id)
		}
		if !strings.Contains(got, `stroke-width="1.5"`) {
			t.Errorf("IconFor(%q) has no stroke-width=1.5 stroke", id)
		}
	}
}

func TestAppColorClassForKnownApps(t *testing.T) {
	want := map[string]string{
		"notes":  "swatch-c-amber",
		"paste":  "swatch-c-blue",
		"reader": "swatch-c-coral",
		"later":  "swatch-c-purple",
		"books":  "swatch-c-green",
		"flash":  "swatch-c-pink",
		"focus":  "swatch-c-teal",
		"admin":  "swatch-c-gray",
	}
	for id, w := range want {
		if got := ui.AppColorClass(id); got != w {
			t.Errorf("AppColorClass(%q) = %q, want %q", id, got, w)
		}
	}
}

func TestAppColorClassFallsBackToGray(t *testing.T) {
	if got := ui.AppColorClass("some-future-app"); got != "swatch-c-gray" {
		t.Errorf("AppColorClass(unknown) = %q, want swatch-c-gray", got)
	}
}

// Icons take their colour from CSS (the swatch class on their container),
// so none may hard-code the suite accent any more. var(--c-accent-bg) as a
// CSS fallback is fine; the bare accent is not.
func TestIconsDrawInCurrentColor(t *testing.T) {
	for _, id := range []string{"paste", "notes", "reader", "flash", "admin", "later", "focus", "books", "some-future-app"} {
		got := string(ui.IconFor(id))
		if strings.Contains(got, "var(--c-accent)") {
			t.Errorf("IconFor(%q) still uses var(--c-accent)", id)
		}
		if !strings.Contains(got, "currentColor") {
			t.Errorf("IconFor(%q) has no currentColor", id)
		}
	}
}
