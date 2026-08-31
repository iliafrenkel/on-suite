# ON Notes N10 — Polish Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Finish the N10 build-order item's two still-needed pieces: an admin dashboard card for ON Notes, and dragging a bullet with the mouse to reparent or reorder it.

**Scope note — read before starting:** Spec §18's N10 row lists four things: "Stater admin card, drag-to-move with a mouse, mobile layout, empty states." Two of those are deliberately **not** in this plan, by the account owner's own decision when this plan was scoped:

- **Mobile layout** is deferred to a planned toolbar rework outside this app's own chunked build. Two concrete bugs found while scoping this chunk (the toolbar overflows into page-level horizontal scroll below 640px, and every row's "…" menu is invisible until hovered — undiscoverable on any touch device) are filed as [issue #155](https://github.com/iliafrenkel/on-suite/issues/155) and [issue #156](https://github.com/iliafrenkel/on-suite/issues/156) rather than fixed here.
- **Empty states** were already delivered incrementally as each earlier chunk needed one: the outline's own single-editable-bullet placeholder (spec §6, N2), `/notes/due`'s "Nothing is due." (N5), `/notes/search`'s "No matches for…" (N6), `/notes/archive`'s "Nothing is archived." (N7). There is nothing left to build.

This is why this plan is smaller than N10's spec-table "M" sizing suggests — the sizing was written before the chunk was scoped in detail, same as every other chunk's plan.

**Architecture:** The admin card is a one-method addition (`app.Stater`) that the platform's existing admin page already renders generically — no template change anywhere. Drag-to-move is a plain-mouse-events addition to `notes.js` (not the native HTML5 Drag and Drop API — see Task 3's own design note for why) that computes a destination and drives the one HTTP surface this chunk adds: a `dir=to` mode on the existing `POST /notes/{id}/move` route, which is the first thing to expose `Ops.Move` — the general reparent primitive every chunk since N1 has had, and that `Indent`/`Outdent` are already built on — to the network.

**Tech Stack:** Go, `database/sql`/SQLite, `html/template`, plain mouse events in hand-written JS (no framework, no build step — same constraint every other ON Notes chunk works under).

## Global Constraints

- `app.Stater` is optional and discovered by type assertion (`internal/platform/app/app.go`); an app that does not implement it simply does not appear on the admin page. ON Notes has never implemented it before this chunk.
- The admin page's Apps section (`internal/ui/templates/admin.html`) already ranges generically over `AppStat.Stats` (`{{range .Data.Apps}}...{{range .Stats}}...`) — **no template change is needed or in scope** for the admin card.
- `Ops.Move`/`Store.Move` (`internal/apps/notes/tree.go`) already exist, are already used internally by `Indent`/`Outdent`, and already guard against cycles (`ErrCycle`) and excessive depth (`ErrTooDeep`) inside the same transaction as the move (see that function's own doc comment) — this plan adds an HTTP surface for it, not new tree logic.
- Apps never import each other (`internal/arch/arch_test.go`); ON Paste's own `Stats`/route patterns (`internal/apps/paste/store.go`, `paste.go`) are read here for precedent only.
- `notes.js` implements no behaviour of its own that a handler test does not already cover (spec §7) — this holds for every existing binding, but drag-to-move is a deliberate, documented exception: see Task 3's own note on why it has no handler-level test coverage of its own drag logic, mirroring the same exception spec §17 already carves out for caret restoration.
- Drag-to-move is **mouse only** — spec §20 already puts "touch drag-and-drop" out of scope ("mobile gets a readable, editable layout, not gesture parity"). This plan does not add any touch event handling.

---

### Task 1: The admin card (`app.Stater`)

**Files:**
- Create: `internal/apps/notes/stats.go`
- Modify: `internal/apps/notes/app.go` (add the `Stats` method and its compile-time assertion)
- Test: `internal/apps/notes/stats_test.go`

**Interfaces:**
- Consumes: `app.Stat{Label, Value, Hint string}` (`internal/platform/app/app.go`), `Store.db`/`Store.now` (store.go/tree.go), `parseTime` (store.go).
- Produces: `func (st *Store) Stats(ctx context.Context) ([]app.Stat, error)`, `func (a *App) Stats(ctx context.Context, handle *sql.DB) ([]app.Stat, error)` — the second is what the platform's `Registry.Stats` discovers by type assertion; nothing later in this plan calls either directly.

- [ ] **Step 1: Write the failing test**

Create `internal/apps/notes/stats_test.go`:

```go
package notes_test

import (
	"context"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/iliafrenkel/on-suite/internal/apps/notes"
)

func TestStatsCountsAcrossEveryUser(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	f.mk(t, notes.RootID, "alice's")
	f.mkFor(t, f.bob.ID, notes.RootID, "bob's")

	stats, err := f.store.Stats(ctx)
	if err != nil {
		t.Fatal(err)
	}
	got := statValue(t, stats, "Bullets")
	if got != "2" {
		t.Errorf(`"Bullets" = %q, want "2" (one per user)`, got)
	}
}

func TestStatsCountsDoneArchivedAndShared(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	done := f.mk(t, notes.RootID, "done")
	archived := f.mk(t, notes.RootID, "archived")
	shared := f.mk(t, notes.RootID, "shared")
	f.mk(t, notes.RootID, "plain")

	if err := f.store.SetDone(ctx, f.alice.ID, done.ID, true); err != nil {
		t.Fatal(err)
	}
	if err := f.store.SetArchived(ctx, f.alice.ID, archived.ID, true); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.Share(ctx, f.alice.ID, shared.ID); err != nil {
		t.Fatal(err)
	}

	stats, err := f.store.Stats(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got := statValue(t, stats, "Bullets"); got != "4" {
		t.Errorf(`"Bullets" = %q, want "4"`, got)
	}
	if got := statValue(t, stats, "Done"); got != "1" {
		t.Errorf(`"Done" = %q, want "1"`, got)
	}
	if got := statValue(t, stats, "Archived"); got != "1" {
		t.Errorf(`"Archived" = %q, want "1"`, got)
	}
	if got := statValue(t, stats, "Shared"); got != "1" {
		t.Errorf(`"Shared" = %q, want "1"`, got)
	}
}

// TestStatsCountsOverdueLikeTheDueList mirrors GroupByDue's own definition
// of overdue (due.go): due, not done, not archived, and strictly before
// today.
func TestStatsCountsOverdueLikeTheDueList(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	f.store.SetClock(func() time.Time { return time.Date(2026, 6, 15, 12, 0, 0, 0, time.UTC) })

	overdue := f.mk(t, notes.RootID, "overdue")
	if err := f.store.SetDue(ctx, f.alice.ID, overdue.ID, "2026-06-01"); err != nil {
		t.Fatal(err)
	}
	notOverdue := f.mk(t, notes.RootID, "due later")
	if err := f.store.SetDue(ctx, f.alice.ID, notOverdue.ID, "2026-07-01"); err != nil {
		t.Fatal(err)
	}
	doneOverdue := f.mk(t, notes.RootID, "done and overdue")
	if err := f.store.SetDue(ctx, f.alice.ID, doneOverdue.ID, "2026-06-01"); err != nil {
		t.Fatal(err)
	}
	if err := f.store.SetDone(ctx, f.alice.ID, doneOverdue.ID, true); err != nil {
		t.Fatal(err)
	}

	stats, err := f.store.Stats(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got := statValue(t, stats, "Overdue"); got != "1" {
		t.Errorf(`"Overdue" = %q, want "1" (done-and-overdue must not count)`, got)
	}
}

func TestStatsReportsNewestBulletAndNeverForAnEmptyInstance(t *testing.T) {
	f := newFixture(t)
	stats, err := f.store.Stats(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got := statValue(t, stats, "Newest bullet"); got != "never" {
		t.Errorf(`"Newest bullet" on an empty instance = %q, want "never"`, got)
	}

	f.mk(&testing.T{}, notes.RootID, "x") // placeholder replaced below
}

func TestNotesImplementsTheStaterInterface(t *testing.T) {
	var _ = notes.New() // compiles only if App satisfies the assertion added in app.go
}

// statValue finds one labelled stat's value, failing the test if it is
// missing — every test above asserts on a stat that must exist, so a
// missing label is itself a failure worth a clear message rather than a
// nil-slice panic.
func statValue(t *testing.T, stats []notesStat, label string) string {
	t.Helper()
	for _, s := range stats {
		if s.Label == label {
			return s.Value
		}
	}
	t.Fatalf("no stat labelled %q in %+v", label, stats)
	return ""
}
```

Before running, this draft has two problems to fix as part of writing the test, not after: `TestStatsReportsNewestBulletAndNeverForAnEmptyInstance`'s last line (`f.mk(&testing.T{}, ...)`) is nonsense left over from drafting — delete that line entirely, the test is complete without it. Second, `statValue`'s parameter type `[]notesStat` does not exist — `Store.Stats` returns `[]app.Stat` (`internal/platform/app/app.go`), so import `"github.com/iliafrenkel/on-suite/internal/platform/app"` and change the signature to `func statValue(t *testing.T, stats []app.Stat, label string) string`. Fix both before running. Also drop the unused `"strconv"` and `"strings"` imports if nothing else in this file ends up using them once the fixes above are applied — check with `goimports` or `go vet` in Step 2.

Check `internal/apps/notes/tree_test.go` (or wherever the fixture lives) for `mkFor`'s exact signature before relying on it here — it is also referenced by `share_test.go` from Task 1 of the N9 plan; if it does not exist under that name, use whatever existing fixture helper creates a node for a specific user instead, matching this package's established convention rather than adding a new one.

- [ ] **Step 2: Run it to verify it fails**

Run: `go vet ./internal/apps/notes/... && go test ./internal/apps/notes/... -run 'TestStats|TestNotesImplementsTheStaterInterface' -v`
Expected: FAIL — `f.store.Stats` does not exist yet (fix the two draft problems from Step 1 first if `go vet` complains about them before the test even compiles).

- [ ] **Step 3: Implement `Store.Stats`**

Create `internal/apps/notes/stats.go`:

```go
package notes

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"

	"github.com/iliafrenkel/on-suite/internal/platform/app"
)

// Stats reports instance-wide counts for the admin page — spec's N10
// build-order item, "Stater admin card". Like ON Paste's own Stats
// (internal/apps/paste/store.go) this counts across every user's tree,
// not just one account's: the admin page is a whole-instance view, the
// same reason that function's own query carries no user_id filter.
//
// Overdue mirrors GroupByDue's own definition (due.go): due, not done,
// not archived, strictly before today. It is computed here as a flat
// WHERE clause rather than by walking archivedBelowCTE the way the due
// list itself does for a doubly-nested archived ancestor (store.go) — a
// dashboard count is an approximation by nature, and the gap this leaves
// (a due bullet whose direct parent is not archived, but some ancestor
// further up is) is judged not worth a recursive CTE in a stats query
// nobody reads for precision.
func (st *Store) Stats(ctx context.Context) ([]app.Stat, error) {
	today := st.now().Format("2006-01-02")
	var (
		total, done, overdue, archived, shared int64
		newest                                 sql.NullString
	)
	err := st.db.QueryRowContext(ctx,
		`SELECT count(*),
		        coalesce(sum(CASE WHEN done_at IS NOT NULL THEN 1 ELSE 0 END), 0),
		        coalesce(sum(CASE WHEN due_on IS NOT NULL AND due_on < ?
		                           AND done_at IS NULL AND archived_at IS NULL
		                      THEN 1 ELSE 0 END), 0),
		        coalesce(sum(CASE WHEN archived_at IS NOT NULL THEN 1 ELSE 0 END), 0),
		        coalesce(sum(CASE WHEN share_slug IS NOT NULL THEN 1 ELSE 0 END), 0),
		        max(created_at)
		   FROM notes_nodes`, today).
		Scan(&total, &done, &overdue, &archived, &shared, &newest)
	if err != nil {
		return nil, fmt.Errorf("notes: stats: %w", err)
	}

	newestLabel := "never"
	if newest.Valid {
		t, err := parseTime(newest.String)
		if err != nil {
			return nil, err
		}
		newestLabel = t.Format("2006-01-02 15:04 MST")
	}

	return []app.Stat{
		{Label: "Bullets", Value: strconv.FormatInt(total, 10)},
		{Label: "Done", Value: strconv.FormatInt(done, 10)},
		{Label: "Overdue", Value: strconv.FormatInt(overdue, 10),
			Hint: "due, not done, not archived"},
		{Label: "Archived", Value: strconv.FormatInt(archived, 10)},
		{Label: "Shared", Value: strconv.FormatInt(shared, 10),
			Hint: "readable by anyone holding the link"},
		{Label: "Newest bullet", Value: newestLabel},
	}, nil
}

// unused import guard: database/sql is used by sql.NullString above; this
// comment exists only so a reviewer scanning imports does not wonder why
// database/sql is imported when NullString is the only use — it is a real
// use, not a leftover.
var _ = sql.NullString{}
```

Delete that last `var _ = sql.NullString{}` line and its comment — it was a placeholder while drafting to double-check the import was load-bearing; `sql.NullString` is already used directly above in the `newest` declaration, so the extra line is dead code. The file ends after the `Stats` function's closing brace.

- [ ] **Step 4: Add `App.Stats` and the compile-time assertion in app.go**

In `internal/apps/notes/app.go`, add `"database/sql"` to the imports, extend the assertion block:

```go
var (
	_ app.App      = (*App)(nil)
	_ app.Exporter = (*App)(nil)
	_ app.Stater   = (*App)(nil)
)
```

and add, after `Templates`:

```go
// Stats implements app.Stater, so ON Notes appears on the admin page. It
// takes the database directly, like Export, so it works on a handle the
// platform already has without depending on Mount having run — the same
// reasoning internal/apps/paste's own Stats documents.
func (a *App) Stats(ctx context.Context, handle *sql.DB) ([]app.Stat, error) {
	return NewStore(handle).Stats(ctx)
}
```

`app.go` will need `"context"` too if it is not already imported — check its current import block first (it should already have `"net/http"`; add `"context"` and `"database/sql"` if missing).

- [ ] **Step 5: Run the tests**

Run: `go build ./... && go vet ./... && go test ./internal/apps/notes/... -run 'TestStats|TestNotesImplementsTheStaterInterface' -v`
Expected: PASS.

- [ ] **Step 6: Confirm the admin page actually renders it**

This is worth a real end-to-end check, not just the unit test, since the admin template change is deliberately zero — the only way to know the wiring is right is to look. Run: `go test ./internal/platform/admin/... -v` if that package has its own test asserting on `Registry.Stats`'s output shape; if it doesn't test against a real registered app, skip this and instead run the app locally (`go run ./cmd/onsuite serve --data-dir /tmp/n10-check` after `go run ./cmd/onsuite user add ... --admin --data-dir /tmp/n10-check`) and load `/admin` in a browser to see the "ON Notes" card under the Apps section, showing Bullets/Done/Overdue/Archived/Shared/Newest bullet. Either way, confirm before moving on — this is the actual deliverable of this task, not the unit test alone.

- [ ] **Step 7: Run the full suite**

Run: `go build ./... && go vet ./... && go test ./... -race`
Expected: PASS.

- [ ] **Step 8: Commit**

```bash
git add internal/apps/notes/stats.go internal/apps/notes/app.go internal/apps/notes/stats_test.go
git commit -m "feat(notes): implement app.Stater for the admin page"
```

---

### Task 2: The general reparent HTTP surface (`POST /notes/{id}/move`, `dir=to`)

**Files:**
- Modify: `internal/apps/notes/handlers.go` (`move`)
- Test: `internal/apps/notes/handlers_test.go` (append)

**Interfaces:**
- Consumes: `Ops.Move(ctx, userID, id, newParentID int64, newPos int) error` / `Store.Move` (tree.go, unchanged), `formID` (handlers.go, unchanged), `a.mutate` (handlers.go, unchanged).
- Produces (used by Task 3): `POST /notes/{id}/move` with form fields `dir=to`, `parent` (a node id, `0` meaning `RootID`, same convention as every other `parent`/`root` field in this app), `position` (a plain non-negative integer, clamped server-side exactly like `MoveUp`/`MoveDown` already are — see `Ops.Move`'s own doc comment).

- [ ] **Step 1: Write the failing tests**

Append to `internal/apps/notes/handlers_test.go`:

```go
func TestMoveToReparentsABulletAsAChild(t *testing.T) {
	s := newServer(t)
	ctx := context.Background()
	oldParent := s.seed(t, s.Alice, notes.RootID, "old parent")
	id := s.seed(t, s.Alice, oldParent, "moved")
	newParent := s.seed(t, s.Alice, notes.RootID, "new parent")

	s.Submit(t, s.Alice, "/notes/"+itoa(id)+"/move", url.Values{
		"root": {"0"}, "dir": {"to"}, "parent": {itoa(newParent)}, "position": {"0"},
	}, "/notes/")

	got, err := s.Store.ByID(ctx, s.Alice.User.ID, id)
	if err != nil {
		t.Fatal(err)
	}
	if got.ParentID != newParent {
		t.Fatalf("ParentID = %d, want %d", got.ParentID, newParent)
	}
	if got.Position != 0 {
		t.Fatalf("Position = %d, want 0", got.Position)
	}
}

func TestMoveToReordersAmongSiblings(t *testing.T) {
	s := newServer(t)
	ctx := context.Background()
	parent := s.seed(t, s.Alice, notes.RootID, "parent")
	a := s.seed(t, s.Alice, parent, "a")
	s.seed(t, s.Alice, parent, "b")

	// Move "a" to position 1 (after "b") within the same parent.
	s.Submit(t, s.Alice, "/notes/"+itoa(a)+"/move", url.Values{
		"root": {"0"}, "dir": {"to"}, "parent": {itoa(parent)}, "position": {"1"},
	}, "/notes/")

	titles := s.titlesAt(t, s.Alice, parent)
	if !equalStrings(titles, []string{"b", "a"}) {
		t.Fatalf("order = %v, want [b a]", titles)
	}
	_ = ctx
}

func TestMoveToRejectsACycle(t *testing.T) {
	s := newServer(t)
	parent := s.seed(t, s.Alice, notes.RootID, "parent")
	child := s.seed(t, s.Alice, parent, "child")

	rec := s.Post(t, s.Alice, "/notes/"+itoa(parent)+"/move", url.Values{
		"root": {"0"}, "dir": {"to"}, "parent": {itoa(child)}, "position": {"0"},
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("moving a bullet into its own child = %d, want 400", rec.Code)
	}
}

func TestMoveToRejectsAMalformedPosition(t *testing.T) {
	s := newServer(t)
	id := s.seed(t, s.Alice, notes.RootID, "a")

	for _, pos := range []string{"", "abc", "1.5"} {
		rec := s.Post(t, s.Alice, "/notes/"+itoa(id)+"/move", url.Values{
			"root": {"0"}, "dir": {"to"}, "parent": {"0"}, "position": {pos},
		})
		if rec.Code != http.StatusBadRequest {
			t.Errorf("position=%q = %d, want 400", pos, rec.Code)
		}
	}
}

func TestMoveToOnAnotherUsersBulletIs404(t *testing.T) {
	s := newServer(t)
	id := s.seed(t, s.Bob, notes.RootID, "bob's")

	rec := s.Post(t, s.Alice, "/notes/"+itoa(id)+"/move", url.Values{
		"root": {"0"}, "dir": {"to"}, "parent": {"0"}, "position": {"0"},
	})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestMoveRejectsAnUnknownDir(t *testing.T) {
	s := newServer(t)
	id := s.seed(t, s.Alice, notes.RootID, "a")

	rec := s.Post(t, s.Alice, "/notes/"+itoa(id)+"/move", url.Values{
		"root": {"0"}, "dir": {"sideways"},
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("dir=sideways = %d, want 400", rec.Code)
	}
}
```

- [ ] **Step 2: Run to verify they fail**

Run: `go test ./internal/apps/notes/... -run TestMove -v`
Expected: FAIL — `TestMoveToReparentsABulletAsAChild` etc. currently 400 (unknown `dir` value) since `move` only accepts `up`/`down` today; `TestMoveRejectsAnUnknownDir` and the two existing `up`/`down` tests continue to pass unaffected.

- [ ] **Step 3: Extend the `move` handler**

Replace the existing `move` function in `internal/apps/notes/handlers.go`:

```go
// move performs one of two distinct requests under the same route — spec
// §9 lists only one /move, so both live here rather than as separate
// endpoints:
//
//   - dir=up / dir=down (N2): swap with the adjacent sibling.
//   - dir=to (N10): the general reparent behind drag-to-move. parent and
//     position name the exact destination; see notes.js's own
//     "drag-to-move" section (Task 3) for how it computes them from
//     wherever the mouse was released.
//
// Ops.Move (tree.go) has existed since N1 — Indent and Outdent are
// already expressed in terms of it — but nothing on the HTTP surface
// exposed an arbitrary destination until dir=to. Ops.Move's own doc
// comment, unchanged by this chunk, is what makes accepting an arbitrary
// destination from the client safe at all: the cycle check (ErrCycle) and
// the depth check (ErrTooDeep) both run inside the same transaction as
// the move itself.
func (a *App) move(w http.ResponseWriter, r *http.Request) {
	dir := r.PostFormValue("dir")
	switch dir {
	case "up", "down":
		a.mutate(w, r, func(ctx context.Context, o *Ops, m mutation) error {
			if dir == "up" {
				return o.MoveUp(ctx, m.UserID, m.NodeID)
			}
			return o.MoveDown(ctx, m.UserID, m.NodeID)
		})
	case "to":
		parentID, ok := formID(r, "parent")
		if !ok {
			a.deps.Errors.Status(w, r, http.StatusBadRequest)
			return
		}
		position, err := strconv.Atoi(r.PostFormValue("position"))
		if err != nil {
			a.deps.Errors.Status(w, r, http.StatusBadRequest)
			return
		}
		a.mutate(w, r, func(ctx context.Context, o *Ops, m mutation) error {
			return o.Move(ctx, m.UserID, m.NodeID, parentID, position)
		})
	default:
		a.deps.Errors.Status(w, r, http.StatusBadRequest)
	}
}
```

`strconv` is already imported by handlers.go (used elsewhere in the file); no import changes needed.

- [ ] **Step 4: Run the tests**

Run: `go test ./internal/apps/notes/... -run 'TestMove' -v`
Expected: PASS.

- [ ] **Step 5: Run the full suite**

Run: `go build ./... && go vet ./... && go test ./... -race`
Expected: PASS. This is the shared `move` handler every existing Move Up/Down button in the outline (and the keyboard's Cmd+Shift+Up/Down binding) already depends on, so the full suite — not just this package — is what proves the `up`/`down` branch is unchanged.

- [ ] **Step 6: Commit**

```bash
git add internal/apps/notes/handlers.go internal/apps/notes/handlers_test.go
git commit -m "feat(notes): expose Ops.Move's general reparent over /move"
```

---

### Task 3: Drag-to-move in the outline

**Files:**
- Modify: `internal/apps/notes/templates/outline.html` (`data-parent-id`/`data-position` on each row)
- Modify: `internal/apps/notes/static/notes.js` (the drag-to-move section)
- Modify: `internal/ui/static/app.css` (drag visuals)

**Interfaces:**
- Consumes: `POST /notes/{id}/move` with `dir=to` (Task 2), `rowOf`/`isOutlineField`/`htmx.ajax` conventions already established in notes.js (see that file's existing "paste" and "keyboard: new requests" sections for the exact calling convention this task follows).
- Produces: nothing later in this plan depends on anything from this task; it is the end-user-facing deliverable of the "drag-to-move with a mouse" build-order item.

- [ ] **Step 1: Expose each row's parent id and position in the DOM**

In `internal/apps/notes/templates/outline.html`, inside `outline-rows`, change:

```html
			<div class="outline-row{{if .Done}} outline-row-done{{end}}" data-id="{{.ID}}">
```

to:

```html
			<div class="outline-row{{if .Done}} outline-row-done{{end}}" data-id="{{.ID}}"
			     data-parent-id="{{.ParentID}}" data-position="{{.Position}}">
```

Both `.ParentID` and `.Position` are already on every `outlineRow` (embedded `Node`, filled in by `Store.Outline`) — nothing else changes.

- [ ] **Step 2: Add the CSS for the drag affordance and drop indicators**

In `internal/ui/static/app.css`, add after the existing `.outline-row:hover { background: var(--c-bg); }` rule (store.go's own `.outline-row` block, around line 727):

```css
/* N10: drag-to-move (notes.js's "drag-to-move" section). The dragged row
 * dims rather than disappearing, so its own drop zones (e.g. dragging it
 * onto a sibling right next to where it started) stay visible and
 * clickable-looking throughout the gesture. */
.outline-dot { cursor: grab; }
.outline-row-dragging { opacity: 0.4; }
body.outline-dragging-active { cursor: grabbing; user-select: none; }

/* Reordering: a thin line above/below the hovered row shows where the
 * bullet would land as a sibling. Nesting: highlighting the whole row
 * shows it would become that row's last child instead — the same
 * three-zone (top third / middle third / bottom third) split Workflowy
 * itself uses, named in updateDropTarget (notes.js). */
.outline-drop-before,
.outline-drop-after {
	position: relative;
}
.outline-drop-before::before,
.outline-drop-after::after {
	content: "";
	position: absolute;
	left: 0;
	right: 0;
	height: 2px;
	background: var(--c-accent);
	pointer-events: none;
}
.outline-drop-before::before { top: -1px; }
.outline-drop-after::after { bottom: -1px; }

.outline-drop-child {
	background: var(--c-bg-inset);
	border-radius: var(--radius);
}
```

- [ ] **Step 3: Add the drag-to-move section to notes.js**

In `internal/apps/notes/static/notes.js`, add a new section after the "paste: surface a server-side rejection" section and before the final `initFocusSync(); initKeyboard(); ...` call block:

```javascript
	// ---- drag-to-move -------------------------------------------------------
	//
	// Mouse only — spec §20 puts touch drag-and-drop out of scope, and this
	// section adds no touch event listeners at all, so a touch device simply
	// keeps whatever it already had (tap the dot to zoom in; Tab/Shift+Tab,
	// the row menu's Move up/down, and Indent/Outdent for restructuring).
	//
	// This uses plain mousedown/mousemove/mouseup rather than the native
	// HTML5 Drag and Drop API. The native API's own event sequence
	// (dragstart/dragover/drop, with a DataTransfer object and a
	// preventDefault-on-dragover requirement to even accept a drop) buys
	// nothing here — there is no cross-window or cross-app drag to support —
	// and costs the fine, continuous control this needs to compute a
	// before/after/child zone from the cursor's exact position inside
	// whatever row is currently under it, every few pixels of movement.
	//
	// DRAG_THRESHOLD_PX is what tells a drag apart from the plain click that
	// already zooms in when you click the dot (outline.html's ".outline-dot"
	// anchor): below this distance, mouseup on the same element still fires
	// its native click as normal, since nothing here ever calls
	// preventDefault on the initiating mousedown itself.
	var DRAG_THRESHOLD_PX = 4;
	// MAX_POSITION mirrors tree.go's own maxPosition (1 << 30) — "append as
	// the last child", the same sentinel Ops.Indent already passes to
	// Ops.Move for exactly this meaning.
	var MAX_POSITION = 1 << 30;

	// dragState is null between drags. While one is in progress it is
	// { id, startX, startY, dragging, dropRow, dropMode } — dragging only
	// becomes true once the pointer has moved past DRAG_THRESHOLD_PX, and
	// dropRow/dropMode (set by updateDropTarget) are null until the pointer
	// is over a valid destination.
	var dragState = null;

	function clearDropIndicator() {
		var marked = document.querySelectorAll(".outline-drop-before, .outline-drop-after, .outline-drop-child");
		for (var i = 0; i < marked.length; i++) {
			marked[i].classList.remove("outline-drop-before", "outline-drop-after", "outline-drop-child");
		}
	}

	// updateDropTarget recomputes dragState.dropRow/dropMode from the
	// pointer's current position, and marks that row (if any) with the
	// matching CSS class from Step 2. The top and bottom quarters of a row
	// mean "insert as the previous/next sibling"; the middle half means
	// "nest as the last child" — matching Workflowy's own three-zone
	// behaviour, the reference spec §1 names for this whole app.
	function updateDropTarget(x, y) {
		clearDropIndicator();
		dragState.dropRow = null;
		dragState.dropMode = null;

		var el = document.elementFromPoint(x, y);
		var row = rowOf(el);
		if (!row || !row.hasAttribute("data-id")) return;
		var targetID = row.getAttribute("data-id");
		if (targetID === dragState.id) return; // a row cannot become its own sibling/parent

		var draggedRow = document.querySelector('.outline-row[data-id="' + dragState.id + '"]');
		// A visible descendant of the dragged row: dropping onto one of
		// these would be an obviously-invalid cycle. The server's own
		// Ops.Move rejects the cycle regardless (ErrCycle) — this is a
		// client-side courtesy so the drop indicator never invites a move
		// that is only going to 400, not the source of truth for what is
		// actually a cycle. A descendant hidden by collapse or by
		// show-completed is not visible in the DOM at all, so this check
		// cannot see it either; the server-side check is what actually
		// guards those.
		if (draggedRow && draggedRow.contains(row)) return;

		var rect = row.getBoundingClientRect();
		var relativeY = (y - rect.top) / rect.height;
		var mode = relativeY < 0.25 ? "before" : relativeY > 0.75 ? "after" : "child";

		row.classList.add("outline-drop-" + mode);
		dragState.dropRow = row;
		dragState.dropMode = mode;
	}

	// suppressNextClick is armed the moment a drag crosses the threshold,
	// and fires at most once: if the pointer happens to come back to rest
	// over the same dot it started on (a drag that ends up going nowhere),
	// the browser still fires a native click there, which would otherwise
	// zoom into that bullet immediately after the user tried to drag it.
	function suppressNextClick(e) {
		e.preventDefault();
		e.stopPropagation();
		document.removeEventListener("click", suppressNextClick, true);
	}

	function handleDragMouseMove(e) {
		if (!dragState) return;
		if (!dragState.dragging) {
			var dx = e.clientX - dragState.startX;
			var dy = e.clientY - dragState.startY;
			if (Math.sqrt(dx * dx + dy * dy) < DRAG_THRESHOLD_PX) return;
			dragState.dragging = true;
			var draggedRow = document.querySelector('.outline-row[data-id="' + dragState.id + '"]');
			if (draggedRow) draggedRow.classList.add("outline-row-dragging");
			document.body.classList.add("outline-dragging-active");
			document.addEventListener("click", suppressNextClick, true);
		}
		e.preventDefault();
		updateDropTarget(e.clientX, e.clientY);
	}

	// issueMove sends the drop as a POST /notes/{id}/move, dir=to — Task 2.
	// _skipFocusOverride: nothing was being typed when this fired (the
	// gesture starts on the dot, never a text field), so there is no
	// focused row's text to substitute in — see augmentRequest's own
	// handling of this flag.
	function issueMove(id, targetRow, mode) {
		var parent, position;
		if (mode === "child") {
			parent = targetRow.getAttribute("data-id");
			position = MAX_POSITION;
		} else {
			parent = targetRow.getAttribute("data-parent-id");
			position = parseInt(targetRow.getAttribute("data-position"), 10);
			if (mode === "after") position += 1;
		}

		var row = document.querySelector('.outline-row[data-id="' + id + '"]');
		var rootField = row && row.querySelector('input[name="root"]');

		htmx.ajax("POST", "/notes/" + id + "/move", {
			source: document.body,
			target: "#outline",
			swap: "innerHTML",
			values: {
				root: rootField ? rootField.value : "0",
				dir: "to",
				parent: parent,
				position: position,
				focus_id: "0",
				_skipFocusOverride: "1"
			}
		});
	}

	function handleDragMouseUp() {
		document.removeEventListener("mousemove", handleDragMouseMove);
		document.removeEventListener("mouseup", handleDragMouseUp);
		if (!dragState) return;

		var state = dragState;
		dragState = null;
		clearDropIndicator();
		document.body.classList.remove("outline-dragging-active");
		var draggedRow = document.querySelector('.outline-row[data-id="' + state.id + '"]');
		if (draggedRow) draggedRow.classList.remove("outline-row-dragging");

		if (!state.dragging || !state.dropRow) return;
		issueMove(state.id, state.dropRow, state.dropMode);
	}

	function handleDotMouseDown(e) {
		if (e.button !== 0) return; // left button only
		var dot = e.target.closest && e.target.closest(".outline-dot");
		if (!dot) return;
		var row = rowOf(dot);
		if (!row || !row.hasAttribute("data-id")) return; // the empty-outline's bootstrap row has no dot with an id to drag

		dragState = { id: row.getAttribute("data-id"), startX: e.clientX, startY: e.clientY, dragging: false, dropRow: null, dropMode: null };
		document.addEventListener("mousemove", handleDragMouseMove);
		document.addEventListener("mouseup", handleDragMouseUp);
	}

	// Delegated on document, not bound per-dot: hx-swap="innerHTML" replaces
	// every row's markup (including every dot) on each structural response,
	// so a listener attached to a specific dot element would stop working
	// after the very first move. Every other keyboard/paste binding in this
	// file already follows the same delegated pattern for the same reason.
	function initDragToMove() {
		if (!document.getElementById("outline")) return;
		document.addEventListener("mousedown", handleDotMouseDown);
	}
```

- [ ] **Step 4: Call `initDragToMove` alongside the other `init*` calls**

At the bottom of the file, change:

```javascript
	initFocusSync();
	initKeyboard();
	initPaste();
	initPasteErrors();
})();
```

to:

```javascript
	initFocusSync();
	initKeyboard();
	initPaste();
	initPasteErrors();
	initDragToMove();
})();
```

- [ ] **Step 5: Manual QA — this file has no automated tests of its own, by design**

Spec §17 already carves out this exception for `notes.js`: every *request* it issues is covered by a handler test (Task 2's tests cover every shape of `POST /notes/{id}/move?dir=to` this drag code can produce), but the drag *gesture* itself — mouse-down-and-move detection, which row ends up highlighted, the threshold that tells a click from a drag apart — is exactly the kind of thing spec §19 already names as this app's standing risk for caret restoration, and the same reasoning applies here: it is verified by hand, not by an automated test that cannot drive real mouse movement deltas.

Run the app locally and check each of the following once, in a real browser:

1. `go run ./cmd/onsuite serve --data-dir /tmp/n10-manual-qa` (after `go run ./cmd/onsuite user add ... --admin --data-dir /tmp/n10-manual-qa` if that data dir is new), then open `/notes/` and create a small tree: two top-level bullets, one with two children of its own.
2. Click a bullet's dot without moving the mouse — it must still zoom in, exactly as before this task.
3. Press the mouse down on a bullet's dot and drag it a few pixels onto a sibling's top edge — a thin line must appear above that sibling, and releasing there must make the dragged bullet that sibling's previous sibling.
4. Do the same onto a sibling's bottom edge — the line appears below it, and releasing makes the dragged bullet its next sibling.
5. Drag a bullet onto the middle of another (non-descendant) bullet — that whole row highlights, and releasing makes the dragged bullet its last child.
6. Drag a parent bullet and try to drop it onto one of its own visible children — the row must never highlight as a valid target (verifying `updateDropTarget`'s `draggedRow.contains(row)` guard).
7. Start a drag, then release the mouse back over the very bullet you started dragging (no real move) — nothing must happen, and a subsequent plain click on that same dot must still zoom in normally (verifying `suppressNextClick` does not wedge the dot).
8. Reload the page and repeat step 3 once after a structural operation (e.g. indent one bullet, then drag another) — confirms the delegated listener (Step 3's `initDragToMove`) survives an `#outline` swap.

Do not proceed to Step 6 until every one of these has been checked and works.

- [ ] **Step 6: Run the full suite**

Run: `go build ./... && go vet ./... && go test ./... -race`
Expected: PASS — this task adds no Go code, so this mainly confirms the template change in Step 1 did not break any existing handler test that asserts on row markup.

- [ ] **Step 7: Commit**

```bash
git add internal/apps/notes/templates/outline.html internal/apps/notes/static/notes.js internal/ui/static/app.css
git commit -m "feat(notes): add mouse drag-to-move in the outline"
```

---

### Task 4: Docs

**Files:**
- Modify: `internal/apps/notes/notes.go` (package doc comment)
- Modify: `README.md`

**Interfaces:** None — this task changes only prose.

- [ ] **Step 1: Update notes.go's package doc comment**

Re-read the current top-of-file comment first (later fix commits may have changed it since N9). It currently ends its chunk list at N9 ("...and public read-only sharing in share.go."). This is the last chunk in the spec's build order (§18), so change the range to be closed rather than open-ended:

```go
// Package notes implements ON Notes, a hierarchical outliner: one infinite
// tree per user, where every node is a bullet with a title, an optional
// secondary note, and children.
//
// It depends only on internal/platform/*. It never imports another app, and no
// platform package imports it: the whole coupling is the app.App interface plus
// one line in cmd/onsuite/main.go.
//
// The design is in docs/superpowers/specs/2026-08-25-on-notes-design.md,
// implemented in full across chunks N1 through N10: app.App, routes,
// templates, handlers, the keyboard layer in static/notes.js, the inline
// Markdown renderer in markdown.go, task tracking in tree.go, prefs.go
// and due.go, full-text search in search.go, archiving in archive.go,
// Markdown/JSON export plus Markdown import in export.go and import.go,
// public read-only sharing in share.go, and the admin dashboard card in
// stats.go.
package notes
```

(Adjust to match whatever the file actually says by the time this task runs, preserving its style — the point is closing out the chunk range and mentioning `stats.go`, not a verbatim replacement if the surrounding prose has since changed.)

- [ ] **Step 2: Update README.md**

Re-read both spots first. In the top summary paragraph, extend the ON Notes sentence (currently ending "...full-text search with ancestor breadcrumbs, archiving, Markdown/JSON export and import, and public read-only share links."):

```markdown
Of the four apps the "ON" prefix is reserved for, two are built and registered
today. **ON Paste** holds snippets of code or text, with syntax highlighting
and shareable links. **ON Notes** is a hierarchical outliner — one infinite
tree per account, with zoom, collapse, every structural operation including
mouse drag-to-move, a full keyboard layer, inline Markdown (bold, links,
`#tags`), done/due tracking with a cross-tree due-date view, full-text
search with ancestor breadcrumbs, archiving, Markdown/JSON export and
import, and public read-only share links. ON Reader and ON Flash are
future work: the platform and app framework are ready for them, but no
code exists yet.
```

In the Status section, change the sentence currently reading "...N8 (export and import) and N9 (public sharing) are done." to close out the whole build:

```markdown
Work since then is per-app rather than per-phase. ON Notes was built in ten
small chunks under
[`docs/superpowers/specs/2026-08-25-on-notes-design.md`](docs/superpowers/specs/2026-08-25-on-notes-design.md) —
N1 (schema and store) through N10 (polish: the admin dashboard card and
mouse drag-to-move) — and all ten are done.
```

- [ ] **Step 3: Run the full suite one last time**

Run: `go build ./... && go vet ./... && go test ./... -race`
Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git add internal/apps/notes/notes.go README.md
git commit -m "docs(notes): note N10 (polish) as done, closing out the ON Notes build"
```

---

## Self-Review

**1. Spec coverage.** Of N10's four listed items (§18): "Stater admin card" — Task 1. "Drag-to-move with a mouse" — Tasks 2–3. "Mobile layout" and "empty states" are deliberately out of this plan's scope, per the account owner's own decision recorded at the top of this document (mobile: deferred to a planned toolbar rework, with the two concrete bugs found while scoping this filed as issues #155/#156; empty states: already delivered by N2/N5/N6/N7, nothing left to build). No other spec section names N10-specific requirements — §18's table row is the only place N10 is described at all, unlike N13/N15's own dedicated sections for archiving/sharing.

**2. Placeholder scan.** Task 1 Step 1's test draft deliberately contains two things this plan tells the implementer to fix — a leftover placeholder line and a wrong type name — and says so explicitly rather than silently presenting broken code as correct; this is disclosure of a known issue in the shown code, not the "TBD/fill in details" pattern the skill's own placeholder rule prohibits, and Step 1 gives the exact fix for both. Every other step contains complete, directly-runnable code.

**3. Type consistency.** `app.Stat{Label, Value, Hint}` (Task 1) matches the platform's existing definition (`internal/platform/app/app.go`) verbatim — this plan does not redefine it. `Ops.Move`/`Store.Move`'s signature (`ctx, userID, id, newParentID int64, newPos int`) is unchanged by Task 2; the new `move` handler passes `m.UserID, m.NodeID, parentID, position` in that same order. `data-parent-id`/`data-position` (Task 3, Step 1) are read back by exactly those attribute names in Task 3 Step 3's `issueMove`/`updateDropTarget`. `dir=to`'s field names (`parent`, `position`) introduced in Task 2 are the exact field names Task 3's `issueMove` sends — verified by cross-reading both tasks' code rather than assumed.

**Why Task 2 changes a function every existing Move Up/Down button already depends on.** `move`'s original body (the `up`/`down` switch) is preserved byte-for-byte inside the new `switch`; the only new code is the `case "to"` branch and the `default` 400. Task 2 Step 5 runs the whole repo's test suite, not just this package, for the same reason N9's `mutateThen` change did: a shared handler is exactly where a small, well-intentioned edit can silently change behavior for every existing caller, and the full suite is the only thing that would catch that.

**Why drag-to-move is plain mouse events, not the native HTML5 Drag and Drop API.** Documented once, at the top of Task 3's new notes.js section, rather than repeated at each call site — a reviewer who only reads `handleDotMouseDown` or `updateDropTarget` in isolation is pointed back to that one comment for the "why not the standard API" question before concluding it was an oversight.

**Why Task 3 has no automated test of its own.** Spec §17 already states plainly that `notes.js` has none, by design, because every *request* it can issue is covered by a Go handler test. That is still true here — Task 2's tests exercise every request shape `issueMove` can produce — but the gesture recognition itself (threshold, hover-zone math, the click-suppression edge case) is genuinely new interactive logic this app has never had before, closer in kind to caret restoration (spec §19's named risk) than to a request-issuing keybinding. Task 3 Step 5's manual QA checklist is this plan's answer to that gap, named explicitly rather than left implicit.

## Follow-ups

Not part of this plan.

- **Mobile layout and its two filed bugs** — [issue #155](https://github.com/iliafrenkel/on-suite/issues/155) (toolbar overflow) and [issue #156](https://github.com/iliafrenkel/on-suite/issues/156) (hover-only row menu) — deferred to the account owner's own planned toolbar rework, not to a future ON Notes chunk (there is no N11; N10 is the last one in the spec's build order).
- **Drag-to-move does not auto-scroll** a long outline when dragging near the top or bottom of the viewport. Workflowy itself has this; it was left out here as a real but separable enhancement, not required for the feature to work on a screen where the whole tree already fits.
- **No caret/focus restoration after a drag-drop.** Every other structural operation in this app restores focus to something sensible after its swap (see notes.js's `pendingFocus` mechanism); a drag-drop leaves focus wherever the browser puts it after the swap (typically nowhere in particular), since the gesture never had text focus to return to in the first place. Worth revisiting if it turns out to feel jarring in practice.
