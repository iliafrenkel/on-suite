# ON Notes phone density Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make the ON Notes outline readable at 4+ levels on a 375px phone and make the zoom dot easy to tap, without changing desktop.

**Architecture:** Pure CSS in `internal/ui/static/app.css`, inside one new `@media (max-width: 780px)` block in the Notes outline section. No template or JS changes. CSS contracts are pinned with the existing `loadAppCSS` / `mustDeclare` helpers (`internal/ui/cssrules_test.go`); geometry is verified in the browser at 375×812.

**Tech Stack:** CSS, Go tests (`internal/ui`), local `onsuite serve` + browser pane.

**Spec:** `docs/superpowers/specs/2026-11-24-notes-mobile-density-design.md`

## Global Constraints

- Every new rule sits inside `@media (max-width: 780px)` — written exactly so; `mustDeclare` matches the normalized prelude. Nothing at top level changes, so desktop is untouched.
- No template changes: markup order in `outline.html` matters for #57 (Enter → append button) and #74 (due-form association).
- No horizontal page scroll at 375px, including with the `…` menu open.
- Full check from AGENTS.md stays green on every commit.

## Review Focus

- Leaf rows (no children) render an empty `<span class="outline-chevron">` spacer: their dot must line up with sibling dots that have a real chevron button.
- The `…` menu opened on the deepest row must stay inside the viewport (right-anchored, 16rem min-width).
- Tapping just above/below a dot (within the new hit area) zooms; tapping the chevron still collapses; tapping text still edits.
- Collapsed bullets (`.outline-dot-full`, halo SVG) keep the same hit area.
- Zoomed view (`/notes/{id}`) uses the same rows — check it renders the same way.

---

### Task 1: Phone indent and `…` menu at the row end

**Files:**
- Modify: `internal/ui/static/app.css` (new phone block after the `.outline-item .outline-list` rule, ~line 1411)
- Create: `internal/ui/notes_phone_test.go`

**Interfaces:**
- Produces: the `@media (max-width: 780px)` block in the Notes outline section that Tasks 2–3 add to.

- [ ] **Step 1: Write the failing test** in `internal/ui/notes_phone_test.go` (package `ui_test`):

```go
func TestNotesOutlineIsDenserOnPhones(t *testing.T) {
	css := loadAppCSS(t)
	const phone = "@media (max-width: 780px)"
	// 20px per level; the guide line runs under the parent's chevron
	// (chevron is 1.25rem wide, so its centre is 0.625rem in).
	css.mustDeclare(phone, ".outline-item .outline-list", "margin-left: 0.625rem", "padding-left: 0.625rem")
	// The menu moves after the text; its drop-down opens from the right edge.
	css.mustDeclare(phone, ".outline-menu", "order: 1")
	css.mustDeclare(phone, ".outline-menu-list", "left: auto", "right: 0")
}
```

- [ ] **Step 2: Run it, expect FAIL** (`no rule … in @media (max-width: 780px)`)

Run: `go test ./internal/ui/ -run TestNotesOutlineIsDenserOnPhones -v`

- [ ] **Step 3: Add the phone block** with those three rules, and a comment explaining the chevron-not-dot guide line and why `order` is used instead of moving markup (#57/#74).

- [ ] **Step 4: Run the test, expect PASS.** Same command.

- [ ] **Step 5: Verify in the browser at 375×812** (`go build -o <scratch>/onsuite ./cmd/onsuite`, serve, sign in, import a 4-level outline via Import, `resize_window` mobile). Measure with `getBoundingClientRect` on each level's `.outline-text`:
  - level-4 text width ≥ 150px (was 0px)
  - guide line x ≈ parent chevron centre (±2px)
  - leaf-row dots align with sibling dots
  - open the deepest row's `…` menu: `document.documentElement.scrollWidth === innerWidth`
  - reset with `resize_window` desktop and confirm desktop indent is still 3.625rem.

- [ ] **Step 6: Commit**

```bash
git add internal/ui/static/app.css internal/ui/notes_phone_test.go
git commit -m "feat(notes): denser outline on phones, row menu at the end"
```

### Task 2: Finger-sized zoom target on the dot

**Files:**
- Modify: `internal/ui/static/app.css` (same phone block)
- Modify: `internal/ui/notes_phone_test.go`

**Interfaces:**
- Consumes: Task 1's phone block.

- [ ] **Step 1: Write the failing test**

```go
func TestNotesDotHasFingerSizedTargetOnPhones(t *testing.T) {
	css := loadAppCSS(t)
	const phone = "@media (max-width: 780px)"
	css.mustDeclare(phone, ".outline-dot", "position: relative")
	css.mustDeclare(phone, ".outline-dot::before", `content: ""`, "position: absolute")
}
```

- [ ] **Step 2: Run it, expect FAIL.** `go test ./internal/ui/ -run TestNotesDotHasFingerSizedTargetOnPhones -v`

- [ ] **Step 3: Add the hit area.** An invisible `::before` on `.outline-dot` that extends ~0.5rem above and below (≈43px tall) and only into the 0.25rem gaps on each side, so it never covers the chevron or the text. Geometry is the builder's call within those limits.

- [ ] **Step 4: Run the test, expect PASS.**

- [ ] **Step 5: Verify in the browser at 375×812** with `document.elementFromPoint`:
  - points 8px above and below a dot's centre resolve to that `.outline-dot` (closest('a.outline-dot'))
  - the chevron's centre still resolves to `button.outline-chevron`
  - the first character of the text still resolves inside `.outline-text`
  - same checks on a collapsed bullet (`.outline-dot-full`)
  - actually tap a dot (click via ref) and confirm the URL becomes `/notes/{id}`.

- [ ] **Step 6: Commit**

```bash
git add internal/ui/static/app.css internal/ui/notes_phone_test.go
git commit -m "feat(notes): finger-sized zoom target on the bullet dot on phones"
```

### Task 3: Outline text one step smaller on phones

**Pending Ilia's decision (see hand-off):** outline titles are already 15px (`--fs-base`); one step down is `--fs-sm` (13px). Skip this task if Ilia drops the font change.

**Files:**
- Modify: `internal/ui/static/app.css` (same phone block)
- Modify: `internal/ui/notes_phone_test.go`

- [ ] **Step 1: Write the failing test**

```go
func TestNotesOutlineTextIsSmallerOnPhones(t *testing.T) {
	css := loadAppCSS(t)
	const phone = "@media (max-width: 780px)"
	css.mustDeclare(phone, ".outline-text", "font-size: var(--fs-sm)")
	css.mustDeclare(phone, "input.outline-note", "font-size: var(--fs-xs)")
	css.mustDeclare(phone, ".outline-note-rendered", "font-size: var(--fs-xs)")
}
```

- [ ] **Step 2: Run it, expect FAIL.**

- [ ] **Step 3: Add the rules.** `input.outline-title` already uses `font: inherit`, so it follows `.outline-text`; the note rules set `--fs-sm` explicitly and need their own phone override. Row control heights (`1.7rem`) stay, so check the dot still centres on the first text line.

- [ ] **Step 4: Run the test, expect PASS.**

- [ ] **Step 5: Verify in the browser at 375×812:** computed title 13px, note 12px; the dot's vertical centre is within 2px of the first text line's centre.

- [ ] **Step 6: Commit**

```bash
git add internal/ui/static/app.css internal/ui/notes_phone_test.go
git commit -m "feat(notes): one step smaller outline text on phones"
```

### Task 4: Full check and before/after proof

- [ ] **Step 1: Run the full check** from AGENTS.md (`gofmt -l .`, `go vet ./...`, staticcheck v0.8.2, `go mod tidy` diff, `go test ./... -race -count=1`). Expected: all clean.
- [ ] **Step 2: Screenshot** the 4-level outline at 375×812 (top-level and zoomed into level 1) and the deepest row's open menu, for the PR body alongside the "before" table from the spec.
