# Braun Folio — Visual Redesign Phase 2 (Remaining 5 Apps)

**Date:** 2026-11-23  
**Status:** Draft for review  
**Scope:** Tailored Braun Folio CSS polish for **ON Paste**, **ON Flash**, **ON Notes**, **ON Reader**, and **ON Books** on branch `feat/braun-folio-design`.

---

## 1. Goal & Approach

Phase 1 established the Braun Folio visual system, self-hosted IBM Plex + Source Serif 4 typography, architectural shell/Home/Login, and the two flagship poles (**ON Focus** as a Dieter Rams precision instrument and **ON Later** as a Quanta editorial reading room).

Phase 2 brings the remaining 5 apps up to the same visual craft — **purely through CSS in `internal/ui/static/app.css`** so every existing HTMX swap target, keyboard shortcut, and JS hook stays untouched and zero-risk.

---

## 2. Per-App Visual Specifications

### 2.1 ON Paste — Dieter Rams Code Instrument
- **Split-View Ledger:** `.paste-list-pane` sits on `--c-bg-subtle` with a crisp `1px` right hairline. Active snippet rows (`.snippet-row-active`) use a `2px` Verdigris left indicator rail (`border-left: 2px solid var(--c-accent)`) and `--c-accent-bg` wash.
- **Metadata & Pills:** `.snippet-pill` and `.snippet-date` use `IBM Plex Mono` (`--fs-xs`, `2px` precision radius, uppercase tracked language tag).
- **Code Frame:** `.snippet-body` is framed as a recessed instrument well (`background: var(--c-bg-inset); border: 1px solid var(--c-border-firm); border-radius: var(--radius);`), paired with `.paste-detail-header` (`IBM Plex Serif` title + monospaced metadata).

### 2.2 ON Flash — Precision Study Instrument
- **Deck Grid & Mastery Bars:** Deck cards and rows use crisp `1px solid var(--c-border)` hairlines (`--c-border-firm` on hover), `2px` corners, and square-capped mastery progress bars.
- **Review Card:** `.flash-card` sits on `--c-bg-subtle` framed by `1px solid var(--c-border-firm)` with a top deck-swatch accent rule (`border-top: 3px solid var(--swatch, var(--c-accent))`) and `IBM Plex Serif` display typography for card prompts/answers (never uppercase/mono — preserving case sensitivity).
- **FSRS Grade Keys & Stats:** Grade buttons (`.flash-grade`) use crisp `2px` borders and tabular `IBM Plex Mono` interval readouts; stat tiles use hairline borders and tabular `IBM Plex Mono` numerals.

### 2.3 ON Notes — Quiet Architectural Outliner
- **Toolbar & Breadcrumbs:** Hairline-divided `.notes-toolbar` and monospaced uppercase `.outline-crumbs`.
- **Guide Rails & Chips:** Crisp `1px` vertical indent guide lines (`.outline-item .outline-list`) with `2px` monospaced metadata chips (`.outline-due-chip`, `.outline-share-chip`, `.outline-children-badge`) contrasting cleanly against `Source Serif 4` `.outline-text`.

### 2.4 ON Reader — Quanta Editorial Magazine
- **Three-Pane Chrome:** Crisp `1px` hairline dividers between feed, list, and reading panes; subtle `--c-bg-subtle` navigation rails with a `2px` Verdigris active rail on selected feeds/items.
- **Editorial Reading Surface:** Harmonizes directly with ON Later — `IBM Plex Serif` article headline, double-ruled monospaced feed/author/date metadata bar, calm `Source Serif 4` reading prose (`line-height: 1.72`), and Verdigris hairline italic blockquotes.

### 2.5 ON Books — Editorial Library Monograph
- **Titles & Shelves:** `IBM Plex Serif` book titles (`--font-display`) paired with monospaced author/year/shelf metadata and crisp hairline shelf dividers.
- **Covers, Stats & Quotes:** Framed book covers (`1px solid var(--c-border-firm)`, `2px` radius), architectural reading-goal/stat plates with tabular `IBM Plex Mono` numerals, calm `Source Serif 4` reading notes/reviews, and Verdigris/Vermilion left-accented pull-quote blocks (`.books-quote`).

---

## 3. Constraints & Verification

- **Strict CSS-only scope:** No template rewrites, no new Go dependencies, no inline styles, `internal/ui` remains a zero-import leaf.
- **Accessibility:** All pairings maintain WCAG AA 4.5:1+ contrast in Light and Dark modes; all `<label>`-based interactive controls keep their own typography and spacing; `prefers-reduced-motion` preserved.
- **Tests:** Add contract assertions in `internal/ui/braun_folio_test.go` pinning Phase 2 styling rules across all 5 apps, and run `gofmt`, `go vet`, `staticcheck@v0.8.2`, `go build`, and targeted/full Go test suites.
