# Braun Folio Phase 2 (Remaining 5 Apps) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans (selected by user for this branch) or superpowers:subagent-driven-development to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Apply tailored **Braun Folio** CSS craft to **ON Paste**, **ON Flash**, **ON Notes**, **ON Reader**, and **ON Books** in `internal/ui/static/app.css` without altering any template DOM hooks, HTMX targets, or JS behavior.

**Architecture:** All visual refinements live inside the existing per-app sections of `internal/ui/static/app.css`, composing the Braun Folio tokens established in Phase 1 (`--c-*`, `--font-display`, `--font-ui`, `--font-body`, `--font-mono`, `--radius`, `--chamfer-sm`). Contract tests in `internal/ui/braun_folio_test.go` pin every new rule and preserve contrast and case-sensitivity invariants.

**Tech Stack:** Pure CSS custom properties (`internal/ui/static/app.css`), Go tests (`internal/ui/braun_folio_test.go`).

**Spec:** [docs/superpowers/specs/2026-11-23-braun-folio-phase2-apps-design.md](../specs/2026-11-23-braun-folio-phase2-apps-design.md)

## Global Constraints

- CSS-only changes in `internal/ui/static/app.css` + Go contract tests in `internal/ui/braun_folio_test.go`.
- Zero template modifications, zero new Go dependencies, zero inline styles, `internal/ui` remains a zero-import leaf.
- Preserve case sensitivity on ON Flash cards (`.flash-card` must never inherit uppercase or monospace).
- Maintain WCAG AA 4.5:1+ contrast in Light and Dark themes across all 5 apps.
- Commit on `feat/braun-folio-design` using Conventional Commits.

## Review Focus

1. **ON Flash card case sensitivity**: Styling `.flash-card` with `--font-display` must keep `text-transform: none` so language/code flashcards never lose case distinctions (pinned in Task 1).
2. **ON Paste / ON Reader active-row contrast**: Active rows using `--c-accent-bg` must keep metadata at `--c-text-dim` (not `--c-text-faint`, which falls below 4.5:1 on `--c-accent-bg`) (pinned in Tasks 1 & 2).
3. **Compact uppercase section headings**: `.notes-due-group h2`, `.reader-list-head h2`, and `.books-section-head` must explicitly use `--font-mono` (not the global `h2` display serif) so small uppercase UI labels stay crisp (pinned in Task 2).
4. **ON Flash FSRS grade contrast**: Tightening `.flash-grade` to `2px` corners must preserve the existing per-grade `--flash-grade-*-bg` / `--flash-grade-*-text` WCAG pairings in both Light and Dark modes (pinned in Task 1).
5. **Responsive / narrow-viewport safety**: Adding hairline borders to `.paste-list-pane` or `.reader-panes` must not cause horizontal overflow on narrow viewports (`<= 640px`) (verified in Tasks 1 & 2).

---

### Task 1: Instrument Apps — ON Paste & ON Flash

**Files:**
- Modify: `internal/ui/static/app.css:945-1140,3150-3680`
- Test: `internal/ui/braun_folio_test.go`

**Interfaces:**
- Consumes: Phase 1 Braun Folio tokens (`--c-bg-inset`, `--c-bg-subtle`, `--c-border-firm`, `--c-accent`, `--c-accent-attention`, `--font-display`, `--font-mono`, `--radius`, `--chamfer-sm`).
- Produces: Braun Folio instrument styling for ON Paste split-view/code frames and ON Flash decks/review cards/FSRS grade keys.

- [ ] **Step 1: Add failing test `TestBraunFolioPhase2InstrumentApps` in `internal/ui/braun_folio_test.go`**

```go
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
		{"flash show-answer uses chamfered signal primary", `(?s)\.flash-show-answer\s*\{[^}]*clip-path:\s*polygon\(`},
		{"flash grade uses precision radius", `(?s)\.flash-grade\s*\{[^}]*border-radius:\s*var\(--radius\)`},
	}
	for _, c := range checks {
		if !regexp.MustCompile(c.re).MatchString(css) {
			t.Errorf("app.css missing Phase 2 instrument contract: %s", c.name)
		}
	}
}
```

- [ ] **Step 2: Run test to verify RED**

Run: `go test ./internal/ui/... -run TestBraunFolioPhase2InstrumentApps -v -count=1`
Expected: FAIL on all 5 assertions.

- [ ] **Step 3: Style ON Paste and ON Flash in `internal/ui/static/app.css`**

1. **ON Paste (`app.css:961-1135`):**
   - `.snippet-body`: `background: var(--c-bg-inset); border: 1px solid var(--c-border-firm); border-radius: var(--radius); padding: var(--s-3) var(--s-4);`
   - `.snippet-pill`: `font-family: var(--font-mono); border-radius: var(--radius); letter-spacing: 0.05em; text-transform: uppercase;`
   - `.snippet-date`: `font-family: var(--font-mono);`
   - `.snippet-row`: `border-left: 2px solid transparent; border-radius: 0;`
   - `.snippet-row-active, .snippet-row-active:hover`: `border-left-color: var(--c-accent); background: var(--c-accent-bg);` (keeping `.snippet-date` at `--c-text-dim`).
2. **ON Flash (`app.css:3580-3665` + deck/card rules):**
   - `.flash-card-inner` / `.flash-card-face`: crisp `1px solid var(--c-border-firm)` border, `border-top: 3px solid var(--swatch, var(--c-accent))`, `border-radius: var(--radius)`, `.flash-card-text` using `font-family: var(--font-display); text-transform: none;`.
   - `.flash-show-answer`: chamfered Braun Signal Vermilion (`background: var(--c-accent-attention); font-family: var(--font-mono); font-size: var(--fs-sm); letter-spacing: 0.08em; text-transform: uppercase; border-radius: 0; clip-path: polygon(0 0, calc(100% - var(--chamfer-sm)) 0, 100% var(--chamfer-sm), 100% 100%, 0 100%);`).
   - `.flash-grade`: `border-radius: var(--radius);` with `font-family: var(--font-mono);` on `.flash-grade kbd` and interval readouts, preserving all 4 `--flash-grade-*` color pairings.

- [ ] **Step 4: Run tests to verify GREEN**

Run: `go test ./internal/ui/... ./internal/apps/paste/... -count=1`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/ui/static/app.css internal/ui/braun_folio_test.go
git commit -m "feat(ui): apply Braun Folio instrument polish to ON Paste and ON Flash"
```

---

### Task 2: Outliner & Editorial Apps — ON Notes, ON Reader & ON Books

**Files:**
- Modify: `internal/ui/static/app.css:1310-1690,2625-2850,4620-4850`
- Test: `internal/ui/braun_folio_test.go`

**Interfaces:**
- Consumes: Phase 1 Braun Folio tokens (`--font-display`, `--font-body`, `--font-mono`, `--c-accent`, `--c-border-firm`, `--radius`).
- Produces: Braun Folio styling for ON Notes outliner chrome/chips, ON Reader three-pane/editorial reading view, and ON Books library monograph titles/covers/quotes.

- [ ] **Step 1: Add failing test `TestBraunFolioPhase2EditorialAndOutlinerApps` in `internal/ui/braun_folio_test.go`**

```go
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
		{"reader active row has 2px left rail", `(?s)\.reader-row\.is-active a[^{]*\{[^}]*border-left-color:\s*var\(--c-accent\)`},
		{"reader list heading uses mono", `(?s)\.reader-list-head h2\s*\{[^}]*font-family:\s*var\(--font-mono\)`},
		{"books section heading uses mono", `(?s)\.books-section-head\s*\{[^}]*font-family:\s*var\(--font-mono\)`},
		{"books title uses display serif", `(?s)\.books-book-titles h1\s*\{[^}]*font-family:\s*var\(--font-display\)`},
	}
	for _, c := range checks {
		if !regexp.MustCompile(c.re).MatchString(css) {
			t.Errorf("app.css missing Phase 2 editorial/outliner contract: %s", c.name)
		}
	}
}
```

- [ ] **Step 2: Run test to verify RED**

Run: `go test ./internal/ui/... -run TestBraunFolioPhase2EditorialAndOutlinerApps -v -count=1`
Expected: FAIL on all 5 assertions.

- [ ] **Step 3: Style ON Notes, ON Reader, and ON Books in `internal/ui/static/app.css`**

1. **ON Notes:**
   - `.outline-crumbs`: `font-family: var(--font-mono); font-size: var(--fs-xs); letter-spacing: 0.05em; text-transform: uppercase;`
   - `.outline-due-chip`, `.outline-share-chip`, `.outline-children-badge`: `font-family: var(--font-mono); border-radius: var(--radius);`
   - `.notes-due-group h2`: `font-family: var(--font-mono);`
2. **ON Reader:**
   - `.reader-list-head h2`: `font-family: var(--font-mono); letter-spacing: 0.08em;`
   - `.reader-row a`: `border-left: 2px solid transparent; border-radius: 0;`
   - `.reader-row.is-active a, .reader-row.is-active a:hover`: `border-left-color: var(--c-accent); background: var(--c-accent-bg);` (keeping `.meta` at `--c-text-dim`).
   - `.reader-row .meta`: `font-family: var(--font-mono);`
   - `.reader-body` & `.reader-article` header: `IBM Plex Serif` headline, hairline-framed monospaced meta, `Source Serif 4` prose (`line-height: 1.72`), and Verdigris hairline italic blockquotes matching ON Later.
3. **ON Books:**
   - `.books-item-title`, `.books-book-titles h1`: `font-family: var(--font-display); font-weight: 500;`
   - `.books-section-head`, `.books-item-date`, `.books-pill`: `font-family: var(--font-mono);` (and `border-radius: var(--radius);` on `.books-pill`).
   - `.books-spine` & cover frames: `border-radius: var(--radius);` with crisp `1px solid var(--c-border-firm)` framing.
   - `.books-quote-text`: editorial left border `border-left: 2px solid var(--c-accent); padding-left: var(--s-4); font-style: italic;`.

- [ ] **Step 4: Run tests to verify GREEN**

Run: `go test ./internal/ui/... ./internal/arch/... -count=1`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/ui/static/app.css internal/ui/braun_folio_test.go
git commit -m "feat(ui): apply Braun Folio polish to ON Notes, ON Reader, and ON Books"
```

---

### Task 3: Full Verification & Fresh Phase 2 Code Review

- [ ] **Step 1: Run `gofmt`, `go vet`, `staticcheck@v0.8.2`, `go build`, and unit tests**

```bash
gofmt -l .
go vet ./...
go run honnef.co/go/tools/cmd/staticcheck@v0.8.2 ./...
go build ./cmd/onsuite
go test ./internal/ui/... ./internal/arch/... ./internal/platform/... ./docs/... -count=1
```

- [ ] **Step 2: Dispatch fresh `pro` code reviewer over the Phase 2 diff (`5e4a935..HEAD`), fix any Critical/Important findings via TDD, and present final handoff.**
