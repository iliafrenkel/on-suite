package ui_test

import (
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"math"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/iliafrenkel/on-suite/internal/ui"
)

func parseHexColor(t *testing.T, hexStr string) (float64, float64, float64) {
	t.Helper()
	h := strings.TrimPrefix(strings.TrimSpace(hexStr), "#")
	if len(h) != 6 {
		t.Fatalf("expected 6-digit hex color, got %q", hexStr)
	}
	r, _ := strconv.ParseUint(h[0:2], 16, 8)
	g, _ := strconv.ParseUint(h[2:4], 16, 8)
	b, _ := strconv.ParseUint(h[4:6], 16, 8)
	return float64(r) / 255, float64(g) / 255, float64(b) / 255
}

func relLuminance(c float64) float64 {
	if c <= 0.03928 {
		return c / 12.92
	}
	return math.Pow((c+0.055)/1.055, 2.4)
}

func contrastRatio(t *testing.T, fgHex, bgHex string) float64 {
	t.Helper()
	fr, fg, fb := parseHexColor(t, fgHex)
	br, bg, bb := parseHexColor(t, bgHex)
	l1 := 0.2126*relLuminance(fr) + 0.7152*relLuminance(fg) + 0.0722*relLuminance(fb)
	l2 := 0.2126*relLuminance(br) + 0.7152*relLuminance(bg) + 0.0722*relLuminance(bb)
	if l1 < l2 {
		l1, l2 = l2, l1
	}
	return (l1 + 0.05) / (l2 + 0.05)
}

func extractToken(t *testing.T, block, name string) string {
	t.Helper()
	re := regexp.MustCompile(regexp.QuoteMeta(name) + `:\s*(#[0-9a-fA-F]{6})`)
	m := re.FindStringSubmatch(block)
	if m == nil {
		t.Fatalf("token %s not found in CSS block", name)
	}
	return m[1]
}

// TestBraunFolioReviewFixes pins the Critical and Important code-review fixes:
// 1. Bare label selector never forces uppercase/mono onto non-form <label> controls.
// 2. ON Later drop-cap uses :not(:dir(rtl)) and logical float: inline-start.
// 3. Key Light/Dark/Plate token pairings meet WCAG AA 4.5:1+.
// 4. No byte-identical duplicate font files are bundled under static/fonts/.
func TestBraunFolioReviewFixes(t *testing.T) {
	src, err := fs.ReadFile(ui.Static(), "app.css")
	if err != nil {
		t.Fatal(err)
	}
	css := string(src)

	// 1. Bare label rule must not force uppercase or monospace globally.
	bareLabel := regexp.MustCompile(`(?s)\nlabel \{([^}]*)\}`).FindStringSubmatch(css)
	if bareLabel != nil {
		if strings.Contains(bareLabel[1], "text-transform") || strings.Contains(bareLabel[1], "--font-mono") {
			t.Error("bare label selector must not set text-transform or --font-mono (breaks <label> cards and controls)")
		}
	}

	// 2. ON Later drop-cap must guard RTL with :dir(rtl) and use logical float.
	if !strings.Contains(css, ":not(:dir(rtl))") {
		t.Error("ON Later drop-cap must use :not(:dir(rtl)) to protect dir=auto RTL articles")
	}
	if !strings.Contains(css, "float: inline-start;") {
		t.Error("ON Later drop-cap must use logical float: inline-start")
	}

	// 3. WCAG AA contrast checks on Light, Dark, and Login Plate tokens.
	lightBlock := regexp.MustCompile(`(?s)\n:root \{([^}]*)\}`).FindStringSubmatch(css)
	darkBlock := regexp.MustCompile(`(?s)\n:root\[data-theme="dark"\] \{([^}]*)\}`).FindStringSubmatch(css)
	if lightBlock == nil || darkBlock == nil {
		t.Fatal("missing :root or dark theme block")
	}

	checkPair := func(label, fg, bg string, min float64) {
		t.Helper()
		if got := contrastRatio(t, fg, bg); got < min {
			t.Errorf("%s (%s on %s): contrast %.2f:1 < %.1f:1", label, fg, bg, got, min)
		}
	}

	checkPair("light attention on attention-bg",
		extractToken(t, lightBlock[1], "--c-accent-attention"),
		extractToken(t, lightBlock[1], "--c-accent-attention-bg"), 4.5)
	checkPair("light primary white on attention",
		"#ffffff",
		extractToken(t, lightBlock[1], "--c-accent-attention"), 4.5)
	checkPair("dark faint on inset",
		extractToken(t, darkBlock[1], "--c-text-faint"),
		extractToken(t, darkBlock[1], "--c-bg-inset"), 4.5)
	checkPair("light plate accent on plate",
		extractToken(t, lightBlock[1], "--c-plate-accent"),
		extractToken(t, lightBlock[1], "--c-plate"), 4.5)
	checkPair("light plate signal on plate",
		extractToken(t, lightBlock[1], "--c-plate-signal"),
		extractToken(t, lightBlock[1], "--c-plate"), 4.5)

	// 4. No duplicate byte-identical font files bundled.
	seen := map[string]string{}
	entries, err := fs.ReadDir(ui.Static(), "fonts")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		b, err := fs.ReadFile(ui.Static(), "fonts/"+e.Name())
		if err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(b)
		h := hex.EncodeToString(sum[:])
		if prev, dup := seen[h]; dup {
			t.Errorf("duplicate bundled font files: %s and %s have identical SHA-256 %s", prev, e.Name(), h[:12])
		}
		seen[h] = e.Name()
	}
}

func TestBraunFolioPhase2InstrumentApps(t *testing.T) {
	src, err := fs.ReadFile(ui.Static(), "app.css")
	if err != nil {
		t.Fatal(err)
	}
	css := string(src)

	checks := []struct {
		name string
		re   string
	}{
		{"paste code frame recessed with firm border", `(?s)\.snippet-body\s*\{[^}]*background:\s*var\(--c-bg-inset\)[^}]*border:\s*1px solid var\(--c-border-firm\)`},
		{"paste active row has 2px left rail", `(?s)\.snippet-row-active[^{]*\{[^}]*border-left-color:\s*var\(--c-accent\)`},
		{"paste pill uses mono", `(?s)\.snippet-pill\s*\{[^}]*font-family:\s*var\(--font-mono\)`},
		{"flash card text uses display serif with normal case", `(?s)\.flash-card-text\s*\{[^}]*font-family:\s*var\(--font-display\)[^}]*text-transform:\s*none`},
		{"flash show-answer uses chamfered signal primary", `(?s)\.flash-show-answer\s*\{[^}]*clip-path:\s*polygon\(`},
		{"flash grade uses precision radius", `(?s)\.flash-grade\s*\{[^}]*border-radius:\s*var\(--radius\)`},
	}
	for _, c := range checks {
		if !regexp.MustCompile(c.re).MatchString(css) {
			t.Errorf("app.css missing Phase 2 instrument contract: %s", c.name)
		}
	}
}

func TestBraunFolioPhase2EditorialAndOutlinerApps(t *testing.T) {
	src, err := fs.ReadFile(ui.Static(), "app.css")
	if err != nil {
		t.Fatal(err)
	}
	css := string(src)

	checks := []struct {
		name string
		re   string
	}{
		{"notes crumbs use mono", `(?s)\.outline-crumbs\s*\{[^}]*font-family:\s*var\(--font-mono\)`},
		{"notes due group heading uses mono", `(?s)\.notes-due-group h2\s*\{[^}]*font-family:\s*var\(--font-mono\)`},
		{"reader active row has 2px left rail", `(?s)\.reader-row\.is-active a[^{]*\{[^}]*border-left-color:\s*var\(--c-accent\)`},
		{"reader list heading uses mono", `(?s)\.reader-list-head h2\s*\{[^}]*font-family:\s*var\(--font-mono\)`},
		{"reader body has editorial blockquote", `(?s)\.reader-body blockquote\s*\{[^}]*border-left:\s*2px solid var\(--c-accent\)`},
		{"books section heading uses mono", `(?s)\.books-section-head\s*\{[^}]*font-family:\s*var\(--font-mono\)`},
		{"books title uses display serif", `(?s)\.books-book-titles h1\s*\{[^}]*font-family:\s*var\(--font-display\)`},
	}
	for _, c := range checks {
		if !regexp.MustCompile(c.re).MatchString(css) {
			t.Errorf("app.css missing Phase 2 editorial/outliner contract: %s", c.name)
		}
	}
}
