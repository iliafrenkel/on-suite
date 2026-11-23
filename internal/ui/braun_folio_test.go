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

func extractToken(t *testing.T, decls, name string) string {
	t.Helper()
	re := regexp.MustCompile(regexp.QuoteMeta(name) + `:\s*(#[0-9a-fA-F]{6})`)
	m := re.FindStringSubmatch(decls)
	if m == nil {
		t.Fatalf("token %s not found", name)
	}
	return m[1]
}

// TestBraunFolioReviewFixes pins the Critical and Important code-review fixes:
// 1. No rule listing bare `label` forces uppercase/mono onto <label> controls.
// 2. ON Later drop-cap uses :not(:dir(rtl)) and logical float: inline-start.
// 3. Key Light/Dark/Plate token pairings meet WCAG AA 4.5:1+.
// 4. No byte-identical duplicate font files are bundled under static/fonts/.
func TestBraunFolioReviewFixes(t *testing.T) {
	css := loadAppCSS(t)

	// 1. Every rule that lists bare `label` among its selectors, in any
	// position and inside any @media, stays clear of uppercase and mono.
	for _, r := range css.rulesFor("label") {
		if strings.Contains(r.body, "text-transform") || strings.Contains(r.body, "--font-mono") {
			t.Errorf("rule %v sets text-transform or --font-mono on bare label (breaks <label> cards and controls)", r.selectors)
		}
	}

	// 2. The drop-cap rule itself guards RTL and floats logically.
	dropCap := false
	for _, r := range css.rules {
		for _, sel := range r.selectors {
			if strings.Contains(sel, ".later-article-body") && strings.Contains(sel, "::first-letter") {
				dropCap = true
				if !strings.Contains(sel, ":not(:dir(rtl))") {
					t.Errorf("drop-cap selector %q must use :not(:dir(rtl)) to protect dir=auto RTL articles", sel)
				}
				if !strings.Contains(r.body, "float: inline-start") {
					t.Errorf("drop-cap rule %q must use logical float: inline-start", sel)
				}
			}
		}
	}
	if !dropCap {
		t.Error("app.css: no ON Later drop-cap rule (.later-article-body ... ::first-letter)")
	}

	// 3. WCAG AA contrast checks on Light, Dark, and Login Plate tokens.
	light := css.declsFor(":root")
	dark := css.declsFor(`:root[data-theme="dark"]`)
	checkPair := func(label, fg, bg string, min float64) {
		t.Helper()
		if got := contrastRatio(t, fg, bg); got < min {
			t.Errorf("%s (%s on %s): contrast %.2f:1 < %.1f:1", label, fg, bg, got, min)
		}
	}
	checkPair("light attention on attention-bg",
		extractToken(t, light, "--c-accent-attention"),
		extractToken(t, light, "--c-accent-attention-bg"), 4.5)
	checkPair("light primary white on attention",
		"#ffffff",
		extractToken(t, light, "--c-accent-attention"), 4.5)
	checkPair("dark faint on inset",
		extractToken(t, dark, "--c-text-faint"),
		extractToken(t, dark, "--c-bg-inset"), 4.5)
	checkPair("light plate accent on plate",
		extractToken(t, light, "--c-plate-accent"),
		extractToken(t, light, "--c-plate"), 4.5)
	checkPair("light plate signal on plate",
		extractToken(t, light, "--c-plate-signal"),
		extractToken(t, light, "--c-plate"), 4.5)

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
	css := loadAppCSS(t)

	// ON Paste: hairline code frame on the subtle surface (keeps Chroma
	// Light at AA), 2px active rail, dim date kept readable, mono pills.
	css.mustDeclare("", ".snippet-body", "background: var(--c-bg-subtle)", "border: 1px solid var(--c-border-firm)")
	css.mustDeclare("", ".snippet-row-active", "border-left-color: var(--c-accent)")
	css.mustDeclare("", ".snippet-row-active .snippet-date", "color: var(--c-text-dim)")
	css.mustDeclare("", ".snippet-pill", "font-family: var(--font-mono)")

	// ON Flash: display-serif cards in normal case, precision radii, and a
	// chamfered Show answer with an inset focus ring clip-path can't cut.
	css.mustDeclare("", ".flash-card-text", "font-family: var(--font-display)", "text-transform: none")
	css.mustDeclare("", ".flash-editor-face", "border-radius: var(--radius)")
	css.mustDeclare("", ".flash-show-answer", "clip-path: polygon(")
	css.mustDeclare("", ".flash-show-answer:focus-visible", "box-shadow: inset")
	css.mustDeclare("", ".flash-show-answer:hover", "filter: brightness(1.08)")
	css.mustDeclare("", ".flash-grade", "border-radius: var(--radius)")

	// Chroma Light keyword (#cf222e) keeps >= 4.5:1 on Light --c-bg-subtle.
	light := css.declsFor(":root")
	if got := contrastRatio(t, "#cf222e", extractToken(t, light, "--c-bg-subtle")); got < 4.5 {
		t.Errorf("Chroma Light keyword (#cf222e) on --c-bg-subtle: %.2f:1 < 4.5:1", got)
	}

	// ON Flash FSRS grade pairings stay >= 4.5:1 in Light and Dark.
	dark := css.declsFor(`:root[data-theme="dark"]`)
	for _, g := range []string{"again", "hard", "good", "easy"} {
		if got := contrastRatio(t, extractToken(t, light, "--flash-grade-"+g+"-text"), extractToken(t, light, "--flash-grade-"+g+"-bg")); got < 4.5 {
			t.Errorf("light flash-grade-%s contrast %.2f:1 < 4.5:1", g, got)
		}
		if got := contrastRatio(t, extractToken(t, dark, "--flash-grade-"+g+"-text"), extractToken(t, dark, "--flash-grade-"+g+"-bg")); got < 4.5 {
			t.Errorf("dark flash-grade-%s contrast %.2f:1 < 4.5:1", g, got)
		}
	}
}

func TestBraunFolioPhase2EditorialAndOutlinerApps(t *testing.T) {
	css := loadAppCSS(t)

	// ON Notes: mono crumbs and due-group headings.
	css.mustDeclare("", ".outline-crumbs", "font-family: var(--font-mono)")
	css.mustDeclare("", ".notes-due-group h2", "font-family: var(--font-mono)")

	// ON Reader: 2px active rails, readable active meta, mono list heading,
	// editorial blockquotes.
	css.mustDeclare("", ".reader-node.is-active", "border-left: 2px solid var(--c-accent)")
	css.mustDeclare("", ".reader-row.is-active a", "border-left-color: var(--c-accent)")
	css.mustDeclare("", ".reader-row.is-active .meta", "color: var(--c-text-dim)")
	css.mustDeclare("", ".reader-list-head h2", "font-family: var(--font-mono)")
	css.mustDeclare("", ".reader-body blockquote", "border-left: 2px solid var(--c-accent)")

	// ON Books: mono section labels, display-serif titles, hairline covers.
	css.mustDeclare("", ".books-side-head", "font-family: var(--font-mono)")
	css.mustDeclare("", ".books-section-head", "font-family: var(--font-mono)")
	css.mustDeclare("", ".books-item-title", "font-family: var(--font-display)")
	css.mustDeclare("", ".books-book-titles h1", "font-family: var(--font-display)")
	css.mustDeclare("", ".books-cover", "border: 1px solid var(--c-border-firm)")
}

// TestLoginFormComesFirstOnPhones pins the narrow-screen login layout: the
// form pane moves above the art panel and the decorative figure is hidden,
// so Sign in is on the first screen.
func TestLoginFormComesFirstOnPhones(t *testing.T) {
	css := loadAppCSS(t)
	const phone = "@media (max-width: 780px)"
	css.mustDeclare(phone, ".login-form-pane", "order: -1")
	css.mustDeclare(phone, ".login-art-figure", "display: none")
}

// TestHomeGridLeavesUnfilledCellsEmpty pins the home directory's hairlines:
// each card draws its own ring, so a row the apps don't fill shows the page,
// not a block of border colour from the grid's own background.
func TestHomeGridLeavesUnfilledCellsEmpty(t *testing.T) {
	css := loadAppCSS(t)
	for _, r := range css.rulesFor(".home-grid") {
		if strings.Contains(r.body, "background") {
			t.Errorf(".home-grid sets a background (%q); an unfilled cell would show it", r.body)
		}
	}
	css.mustDeclare("", ".app-card", "box-shadow: 0 0 0 1px var(--c-border)")
}

// TestFieldFocusBackgroundIsOnlyADefault pins how a focused field lifts to
// the subtle surface: at one element plus :focus, so it beats the base field
// rule but never a component's own field background. As
// input[type="text"]:focus it outweighed input.outline-title
// { background: none } and turned ON Notes' focused bullet near-white
// against the page.
func TestFieldFocusBackgroundIsOnlyADefault(t *testing.T) {
	css := loadAppCSS(t)
	for _, sel := range []string{`input[type="text"]:focus`, `input[type="url"]:focus`} {
		for _, r := range css.rulesFor(sel) {
			if strings.Contains(r.body, "background") {
				t.Errorf("%s sets a background (%q); the [type] makes it outweigh component fields, use input:where([type=...]):focus", sel, r.body)
			}
		}
	}
	css.mustDeclare("", `input:where([type="text"], [type="password"], [type="url"], [type="number"]):focus`,
		"background: var(--c-bg-subtle)")
	css.mustDeclare("", "input.outline-title", "background: none")
}

// TestOneKbdStyle pins a single suite-wide kbd rule. A second, top-level
// kbd rule (once the Notes help panel's) silently restyled every key cap,
// the Focus legend's included.
func TestOneKbdStyle(t *testing.T) {
	css := loadAppCSS(t)
	n := 0
	for _, r := range css.rulesFor("kbd") {
		if r.media == "" && len(r.selectors) == 1 {
			n++
		}
	}
	if n != 1 {
		t.Errorf("app.css has %d top-level kbd rules, want 1", n)
	}
	css.mustDeclare("", "kbd", "background: var(--c-bg-inset)", "border-radius: var(--radius)")
}

// TestPhoneBreadcrumbsGetTheirOwnRow pins the phone shell bar: squeezed
// between the logo and the user menu the crumbs shrank to "/ /" and a
// scrollbar, so under 640px they take a full row with no divider.
func TestPhoneBreadcrumbsGetTheirOwnRow(t *testing.T) {
	css := loadAppCSS(t)
	const phone = "@media (max-width: 640px)"
	css.mustDeclare(phone, ".shell-crumbs", "flex: 1 0 100%", "order: 1", "border-left: none")
	css.mustDeclare(phone, ".shell-user", "margin-left: auto")
}

// TestLinkButtonsAreInlineBlock pins the zero-weight default that lets an
// <a class="button"> in running text keep its vertical padding.
func TestLinkButtonsAreInlineBlock(t *testing.T) {
	loadAppCSS(t).mustDeclare("", ":where(a.button)", "display: inline-block")
}

// TestNotesChipsAreMonoEverywhere pins the outline's chips and the shared
// page's due date in mono, with no earlier --font-ui rule left to be
// silently overridden.
func TestNotesChipsAreMonoEverywhere(t *testing.T) {
	css := loadAppCSS(t)
	for _, sel := range []string{".outline-share-chip", ".outline-children-badge", ".outline-due-chip", ".notes-shared-due"} {
		for _, r := range css.rulesFor(sel) {
			if strings.Contains(r.body, "var(--font-ui)") {
				t.Errorf("%s has a --font-ui rule %v; it should be mono", sel, r.selectors)
			}
		}
		css.mustDeclare("", sel, "font-family: var(--font-mono)")
	}
}

// TestNoPreBraunLeftoversInFlashAndReader pins the stats tiles on the shared
// radius and the dark-mode ink on Flash's accent controls at the Braun
// #151412, which keeps 4.5:1+ on both dark accents.
func TestNoPreBraunLeftoversInFlashAndReader(t *testing.T) {
	css := loadAppCSS(t)
	for _, sel := range []string{".reader-stat-tile", ".flash-stat-tile", ".books-stat-tile"} {
		css.mustDeclare("", sel, "border-radius: var(--radius)")
	}
	dark := css.declsFor(`:root[data-theme="dark"]`)
	for _, sel := range []string{
		`:root[data-theme="dark"] .flash-review-cta`,
		`:root[data-theme="dark"] .flash-big-btn-primary`,
		`:root[data-theme="dark"] .flash-type-toggle input:checked + label`,
		`:root[data-theme="dark"] .flash-import-steps li::before`,
	} {
		css.mustDeclare("", sel, "color: #151412")
	}
	for _, tok := range []string{"--c-accent", "--c-accent-attention"} {
		if got := contrastRatio(t, "#151412", extractToken(t, dark, tok)); got < 4.5 {
			t.Errorf("#151412 on dark %s: %.2f:1 < 4.5:1", tok, got)
		}
	}
}
