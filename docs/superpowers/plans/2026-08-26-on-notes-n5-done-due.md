# ON Notes N5 — Done + Due Dates Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A bullet can be marked done (struck through, hidden with its whole subtree unless "show completed" is on) and given a due date (a chip that turns red once it's past), plus one cross-tree `/notes/due` view grouped Overdue / Today / This week / Later.

**Architecture:** Two new nullable columns, `done_at` and `due_on`, added by their own forward-only migration. Both are per-bullet scalars — like title and note — so `done`/`due` are handled exactly like `setText`: they go through the existing `mutate` seam for the one-write guarantee, with their own two routes. "Show completed" is a small, server-set preference cookie (spec §11 — unlike the platform's own theme/font cookies, which `theme.js` writes) that filters a bullet and its whole subtree out of what `Store.Outline` already returned, entirely in Go — no SQL change, no risk to N1–N4's existing `Outline` test coverage. `/notes/due` is one new query plus a pure grouping function.

**Tech Stack:** Go (`database/sql`, `time`), SQLite (`ALTER TABLE ... ADD COLUMN`), the existing HTMX/notes.js progressive-enhancement machinery from N2–N4.

## Global Constraints

- No CGO, no new Go dependencies, no Node/npm/JS build step.
- Migrations are forward-only: `done_at`/`due_on` arrive as `ALTER TABLE ... ADD COLUMN` in their own numbered migration, never rewriting `0001_nodes.sql`.
- CSP: `script-src 'self'` with no `unsafe-inline`; `internal/ui/static/app.css` is the only stylesheet, and "apps must not introduce new colors" — use existing `--c-*` tokens only.
- `main` is branch-protected: work on a branch, open a PR, never push directly.
- The local `staticcheck` binary can silently exit 0 while printing internal-error noise; always run `go run honnef.co/go/tools/cmd/staticcheck@v0.8.0 ./...` instead of the bare `staticcheck` on `$PATH`.
- `internal/htmlassert` supports only descendant combinators and one qualifier per selector part.
- Every store operation takes `userID` and folds it into its `WHERE` clause — another user's node is *not found*, never *forbidden* (spec §5).
- Store tests run against a real SQLite file in a temp dir, per the existing `newFixture` convention in `store_test.go` — not `:memory:`.
- **Comparison is against the server's local date** (spec §11): a single-household deployment in one timezone, so per-user timezones are explicitly out of scope.
- **Completing a parent does not complete its children** (spec §11): hiding a done bullet's subtree is a display decision, not a data change — a child's own `done_at` is never touched by its parent's.
- **Archived nodes are excluded from `/notes/due`, per spec §11 — but `archived_at` does not exist until N7.** This chunk excludes only done nodes; excluding archived ones is a follow-up for whichever of N5/N7 lands second (see "Follow-ups" below). This is not a judgment call — the column simply doesn't exist yet.

## File Structure

- Create: `internal/apps/notes/migrations/0002_done_due.sql` — the two new columns.
- Modify: `internal/apps/notes/notes.go` — `Node.Done`/`Node.DueOn`, `ValidateDue`.
- Modify: `internal/apps/notes/store.go` — `nodeColumns`, `scanNode`.
- Modify: `internal/apps/notes/tree.go` — `Ops.SetDone`/`Store.SetDone`, `Ops.SetDue`/`Store.SetDue`.
- Modify: `internal/apps/notes/view.go` — `outlineRow.Overdue`, `nest`'s new `today` parameter, `hideDone`; `internal/apps/notes/view_test.go` covers `hideDone`.
- Create: `internal/apps/notes/prefs.go` — the show-completed cookie.
- Create: `internal/apps/notes/due.go` — `DueRow`, `DueGroups`, `GroupByDue`, `Store.Due`; `internal/apps/notes/due_test.go`.
- Create: `internal/apps/notes/templates/due.html`.
- Modify: `internal/apps/notes/templates/outline.html` — the done checkbox, the due-date input and chip, the toolbar (show-completed toggle, Due link).
- Modify: `internal/ui/static/app.css` — strikethrough, the due chip, the due-list page, the toolbar.
- Modify: `internal/apps/notes/handlers.go` — `done`, `due`, `prefs`, `dueList` handlers; `renderOutline`/`renderOutlineFragment` gain the show-completed filter and today's date.
- Modify: `internal/apps/notes/app.go` — four new routes.
- Modify: `internal/apps/notes/static/notes.js` — `Cmd+Enter` toggles done.
- Modify: `README.md`, `internal/apps/notes/notes.go` (package doc).

---

### Task 1: Schema and store layer

**Files:**
- Create: `internal/apps/notes/migrations/0002_done_due.sql`
- Modify: `internal/apps/notes/notes.go`
- Modify: `internal/apps/notes/store.go`
- Modify: `internal/apps/notes/tree.go`
- Modify: `internal/apps/notes/notes_test.go`
- Modify: `internal/apps/notes/tree_test.go`

**Interfaces:**
- Produces: `Node.Done bool`, `Node.DueOn string` (`""` = none); `ValidateDue(due string) error`; `(*Ops).SetDone`/`(*Store).SetDone(ctx, userID, id int64, done bool) error`; `(*Ops).SetDue`/`(*Store).SetDue(ctx, userID, id int64, due string) error`.

This task is pure store layer — no HTTP, no templates — and has its own complete TDD cycle, exactly like N1's.

- [ ] **Step 1: Write the failing tests**

Add to `internal/apps/notes/notes_test.go` (after the existing `TestValidate*` tests):

```go
func TestValidateDueAcceptsEmpty(t *testing.T) {
	if err := notes.ValidateDue(""); err != nil {
		t.Fatalf(`ValidateDue("") = %v; empty means "no due date"`, err)
	}
}

func TestValidateDueAcceptsAWellFormedDate(t *testing.T) {
	if err := notes.ValidateDue("2026-03-05"); err != nil {
		t.Fatalf("ValidateDue(2026-03-05) = %v", err)
	}
}

func TestValidateDueRejectsBadInput(t *testing.T) {
	for _, due := range []string{
		"not-a-date",
		"2026-13-01",  // no month 13
		"2026-02-30",  // February has no 30th; time.Parse would normalise
		                // this into March rather than reject it — the round
		                // trip in ValidateDue is what catches that
		"03/05/2026",  // wrong format entirely
		"2026-3-5",    // not zero-padded
	} {
		if err := notes.ValidateDue(due); !errors.Is(err, notes.ErrInvalid) {
			t.Errorf("ValidateDue(%q) = %v; want ErrInvalid", due, err)
		}
	}
}
```

Add to `internal/apps/notes/tree_test.go` (near `TestSetCollapsedRoundTrips`):

```go
func TestSetDoneRoundTrips(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	n := f.mk(t, notes.RootID, "task")

	if err := f.store.SetDone(ctx, f.alice.ID, n.ID, true); err != nil {
		t.Fatalf("SetDone(true): %v", err)
	}
	got, err := f.store.ByID(ctx, f.alice.ID, n.ID)
	if err != nil {
		t.Fatalf("ByID: %v", err)
	}
	if !got.Done {
		t.Fatal("Done = false after SetDone(true)")
	}

	if err := f.store.SetDone(ctx, f.alice.ID, n.ID, false); err != nil {
		t.Fatalf("SetDone(false): %v", err)
	}
	if got, _ = f.store.ByID(ctx, f.alice.ID, n.ID); got.Done {
		t.Fatal("Done = true after SetDone(false)")
	}
}

// TestSetDoneDoesNotTouchChildren is spec §11: completing a parent does not
// complete its children — that is a display decision (hideDone, Task 3),
// never a change to the child's own row.
func TestSetDoneDoesNotTouchChildren(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	parent := f.mk(t, notes.RootID, "parent")
	child := f.mk(t, parent.ID, "child")

	if err := f.store.SetDone(ctx, f.alice.ID, parent.ID, true); err != nil {
		t.Fatalf("SetDone: %v", err)
	}
	got, err := f.store.ByID(ctx, f.alice.ID, child.ID)
	if err != nil {
		t.Fatalf("ByID: %v", err)
	}
	if got.Done {
		t.Fatal("the child was marked done by its parent's SetDone")
	}
}

func TestSetDoneRejectsAnotherUsersNode(t *testing.T) {
	f := newFixture(t)
	n := f.mk(t, notes.RootID, "alice's")

	err := f.store.SetDone(context.Background(), f.bob.ID, n.ID, true)
	if !errors.Is(err, notes.ErrNotFound) {
		t.Fatalf("SetDone on another user's node = %v; want ErrNotFound", err)
	}
}

func TestSetDuePersistsAndClears(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	n := f.mk(t, notes.RootID, "task")

	if err := f.store.SetDue(ctx, f.alice.ID, n.ID, "2026-03-05"); err != nil {
		t.Fatalf("SetDue: %v", err)
	}
	got, err := f.store.ByID(ctx, f.alice.ID, n.ID)
	if err != nil {
		t.Fatalf("ByID: %v", err)
	}
	if got.DueOn != "2026-03-05" {
		t.Fatalf("DueOn = %q, want 2026-03-05", got.DueOn)
	}

	if err := f.store.SetDue(ctx, f.alice.ID, n.ID, ""); err != nil {
		t.Fatalf("SetDue(clear): %v", err)
	}
	if got, _ = f.store.ByID(ctx, f.alice.ID, n.ID); got.DueOn != "" {
		t.Fatalf("DueOn = %q after clearing, want empty", got.DueOn)
	}
}

func TestSetDueRejectsBadFormat(t *testing.T) {
	f := newFixture(t)
	n := f.mk(t, notes.RootID, "task")

	err := f.store.SetDue(context.Background(), f.alice.ID, n.ID, "not-a-date")
	if !errors.Is(err, notes.ErrInvalid) {
		t.Fatalf("SetDue(bad format) = %v; want ErrInvalid", err)
	}
}

func TestSetDueRejectsAnotherUsersNode(t *testing.T) {
	f := newFixture(t)
	n := f.mk(t, notes.RootID, "alice's")

	err := f.store.SetDue(context.Background(), f.bob.ID, n.ID, "2026-03-05")
	if !errors.Is(err, notes.ErrNotFound) {
		t.Fatalf("SetDue on another user's node = %v; want ErrNotFound", err)
	}
}
```

- [ ] **Step 2: Run the tests to see them fail**

Run: `go test ./internal/apps/notes/... -run 'ValidateDue|SetDone|SetDue' -v`
Expected: FAIL — none of these symbols exist yet.

- [ ] **Step 3: Add the migration**

Create `internal/apps/notes/migrations/0002_done_due.sql`:

```sql
-- N5: done and due dates — spec §11. Both are nullable; a bullet is neither
-- done nor due by default. Verified against SQLite, not assumed: a bare
-- ALTER TABLE ... ADD COLUMN with no default applies cleanly to the
-- STRICT notes_nodes table for a nullable column (see the design spec's
-- §4, which verifies this for done_at/due_on/archived_at together).

ALTER TABLE notes_nodes ADD COLUMN done_at TEXT;
-- 'YYYY-MM-DD' — a date, not an instant.
ALTER TABLE notes_nodes ADD COLUMN due_on TEXT;
```

- [ ] **Step 4: Add `Node.Done`/`Node.DueOn` and `ValidateDue`**

Modify `internal/apps/notes/notes.go`. In the `Node` struct, add two fields after `Collapsed`:

```go
type Node struct {
	ID     int64
	UserID int64
	// ParentID is RootID for a top-level bullet.
	ParentID  int64
	Position  int
	Title     string
	Note      string
	Collapsed bool
	// Done and DueOn are done_at/due_on's Go projections — spec §11. Done
	// hides the underlying timestamp: nothing in this app ever needs to
	// show *when* a bullet was completed, only whether it is. DueOn is the
	// raw 'YYYY-MM-DD' string, or "" for none — a due date is a calendar
	// date, not an instant, so there is no time.Time here either.
	Done      bool
	DueOn     string
	CreatedAt time.Time
	UpdatedAt time.Time

	// Depth and HasChildren are filled in by Outline and are zero elsewhere.
	// Depth is relative to the outline's root: its direct children are 0.
	Depth       int
	HasChildren bool
}
```

Add `ValidateDue` after `Validate`:

```go
// ValidateDue bounds a due date's format — spec §11: due_on is a date, not
// an instant. "" clears it. The round trip through Format catches what
// Parse alone would not: time.Parse silently normalises an impossible date
// like 2026-02-30 into March 2nd rather than rejecting it, and a normalised
// date does not equal the string it was parsed from.
func ValidateDue(due string) error {
	if due == "" {
		return nil
	}
	t, err := time.Parse("2006-01-02", due)
	if err != nil || t.Format("2006-01-02") != due {
		return fmt.Errorf("%w: due date must be YYYY-MM-DD", ErrInvalid)
	}
	return nil
}
```

- [ ] **Step 5: Scan the two new columns**

Modify `internal/apps/notes/store.go`. Change `nodeColumns`:

```go
const nodeColumns = `id, user_id, parent_id, position, title, note, collapsed, created_at, updated_at, done_at, due_on`
```

Change `scanNode`:

```go
func scanNode(row rowScanner, extra ...any) (Node, error) {
	var (
		n         Node
		parent    sql.NullInt64
		createdAt string
		updatedAt string
		doneAt    sql.NullString
		dueOn     sql.NullString
	)
	dest := append([]any{
		&n.ID, &n.UserID, &parent, &n.Position, &n.Title, &n.Note,
		&n.Collapsed, &createdAt, &updatedAt, &doneAt, &dueOn,
	}, extra...)

	err := row.Scan(dest...)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return Node{}, sql.ErrNoRows // translated to ErrNotFound by the caller
	case err != nil:
		return Node{}, fmt.Errorf("notes: scan: %w", err)
	}
	// Valid is false for a top-level node, and Int64 is then 0, which is
	// exactly RootID.
	n.ParentID = parent.Int64
	n.Done = doneAt.Valid
	// sql.NullString's String is "" when Valid is false, which is already
	// DueOn's own "none" sentinel — no extra branch needed.
	n.DueOn = dueOn.String

	if n.CreatedAt, err = parseTime(createdAt); err != nil {
		return Node{}, err
	}
	if n.UpdatedAt, err = parseTime(updatedAt); err != nil {
		return Node{}, err
	}
	return n, nil
}
```

- [ ] **Step 6: Add `SetDone` and `SetDue`**

Modify `internal/apps/notes/tree.go`. Add these after `SetCollapsed`'s `Store` wrapper and before `SetText`'s `Ops` method:

```go
// SetDone marks a bullet done or not. Completing a parent does not complete
// its children — spec §11 — so this only ever touches the one row; hiding
// a done bullet's subtree is a display decision made in view.go, not
// something recorded here.
func (o *Ops) SetDone(ctx context.Context, userID, id int64, done bool) error {
	doneAt := sql.NullString{}
	if done {
		doneAt = sql.NullString{String: formatTime(o.now()), Valid: true}
	}
	return o.update(ctx, "set done",
		`UPDATE notes_nodes SET done_at = ?, updated_at = ? WHERE id = ? AND user_id = ?`,
		doneAt, formatTime(o.now()), id, userID)
}

// SetDone marks a bullet done or not, in a transaction of its own. See
// Ops.SetDone.
func (st *Store) SetDone(ctx context.Context, userID, id int64, done bool) error {
	return st.Do(ctx, func(o *Ops) error { return o.SetDone(ctx, userID, id, done) })
}

// SetDue sets or clears a bullet's due date. due is "" to clear, or a
// validated 'YYYY-MM-DD' string — see ValidateDue.
func (o *Ops) SetDue(ctx context.Context, userID, id int64, due string) error {
	if err := ValidateDue(due); err != nil {
		return err
	}
	var arg any
	if due != "" {
		arg = due
	}
	return o.update(ctx, "set due",
		`UPDATE notes_nodes SET due_on = ?, updated_at = ? WHERE id = ? AND user_id = ?`,
		arg, formatTime(o.now()), id, userID)
}

// SetDue sets or clears a bullet's due date, in a transaction of its own.
// See Ops.SetDue.
func (st *Store) SetDue(ctx context.Context, userID, id int64, due string) error {
	return st.Do(ctx, func(o *Ops) error { return o.SetDue(ctx, userID, id, due) })
}
```

- [ ] **Step 7: Run the tests**

Run: `go test ./internal/apps/notes/... -v`
Expected: PASS, all of it — old and new.

- [ ] **Step 8: Full check**

Run: `gofmt -l . && go build ./... && go vet ./... && go run honnef.co/go/tools/cmd/staticcheck@v0.8.0 ./... && go test ./... -race`
Expected: clean.

- [ ] **Step 9: Commit**

```bash
git add internal/apps/notes/migrations/0002_done_due.sql internal/apps/notes/notes.go internal/apps/notes/store.go internal/apps/notes/tree.go internal/apps/notes/notes_test.go internal/apps/notes/tree_test.go
git commit -m "notes: add done_at/due_on columns and their store operations"
```

---

### Task 2: Marking a bullet done

**Files:**
- Modify: `internal/apps/notes/handlers.go`
- Modify: `internal/apps/notes/app.go`
- Modify: `internal/apps/notes/templates/outline.html`
- Modify: `internal/ui/static/app.css`
- Modify: `internal/apps/notes/handlers_test.go`

**Interfaces:**
- Consumes: `(*Ops).SetDone` (Task 1), `a.mutate` (existing).
- Produces: `(*App).done`, mounted at `POST /notes/{id}/done`. `outlineRow.Done`/`.Node.Done` (already available via the embedded `Node` — no new field needed here).

`outlineRow` embeds `Node` (`type outlineRow struct { Node; ... }`), so once Task 1 adds `Done` to `Node`, every row already exposes `.Done` to the template with no change to `view.go` at all.

- [ ] **Step 1: Write the failing tests**

Add to `internal/apps/notes/handlers_test.go`:

```go
func TestDoneTogglesTheBullet(t *testing.T) {
	s := newServer(t)
	id := s.seed(t, s.alice, notes.RootID, "task")

	s.submit(t, s.alice, "/notes/"+itoa(id)+"/done", url.Values{
		"root": {"0"}, "done": {"1"},
	}, "/notes/")

	n, err := s.store.ByID(context.Background(), s.alice.user.ID, id)
	if err != nil {
		t.Fatal(err)
	}
	if !n.Done {
		t.Fatal("the bullet is not done")
	}

	s.submit(t, s.alice, "/notes/"+itoa(id)+"/done", url.Values{
		"root": {"0"}, "done": {"0"},
	}, "/notes/")

	n, err = s.store.ByID(context.Background(), s.alice.user.ID, id)
	if err != nil {
		t.Fatal(err)
	}
	if n.Done {
		t.Fatal("the bullet is still done")
	}
}

func TestDoneRejectsAnUnknownValue(t *testing.T) {
	s := newServer(t)
	id := s.seed(t, s.alice, notes.RootID, "a")

	for _, v := range []string{"", "true", "yes", "2"} {
		rec := s.post(t, s.alice, "/notes/"+itoa(id)+"/done", url.Values{
			"root": {"0"}, "done": {v},
		})
		if rec.Code != http.StatusBadRequest {
			t.Errorf("done=%q gave %d, want 400", v, rec.Code)
		}
	}
}

func TestDoneOnAnotherUsersBulletIs404(t *testing.T) {
	s := newServer(t)
	id := s.seed(t, s.bob, notes.RootID, "bob's")

	rec := s.post(t, s.alice, "/notes/"+itoa(id)+"/done", url.Values{
		"root": {"0"}, "done": {"1"},
	})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

// TestDoneBulletRendersStruckThrough covers the row-level CSS hook rather
// than CSS itself, which no Go test can see: the row carries a class the
// stylesheet keys off, and the checkbox reflects the current state.
func TestDoneBulletRendersStruckThrough(t *testing.T) {
	s := newServer(t)
	id := s.seed(t, s.alice, notes.RootID, "task")
	if err := s.store.SetDone(context.Background(), s.alice.user.ID, id, true); err != nil {
		t.Fatal(err)
	}

	doc := s.get(t, s.alice, "/notes/")
	row := doc.MustHave(".outline-row-done")
	if got, _ := htmlassert.Attr(row, "data-id"); got != itoa(id) {
		t.Errorf("the done row is %q, want %d", got, id)
	}
	btn := doc.MustHave(`button[formaction=/notes/` + itoa(id) + `/done]`)
	if got, _ := htmlassert.Attr(btn, "value"); got != "0" {
		t.Errorf("a done bullet's toggle sends value=%q, want 0 (mark not done)", got)
	}
}

func TestEveryMutationRequiresSignInIncludesDoneAndDue(t *testing.T) {
	s := newServer(t)
	id := s.seed(t, s.alice, notes.RootID, "a")

	for _, path := range []string{
		"/notes/" + itoa(id) + "/done",
		"/notes/" + itoa(id) + "/due",
	} {
		req := httptest.NewRequest("POST", path, strings.NewReader(""))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		rec := s.do(t, nil, req)
		if rec.Code != http.StatusForbidden {
			t.Errorf("POST %s anonymous and tokenless = %d, want 403 from the CSRF check", path, rec.Code)
		}
	}
}
```

- [ ] **Step 2: Run the tests to see them fail**

Run: `go test ./internal/apps/notes/... -run 'TestDone|IncludesDoneAndDue' -v`
Expected: FAIL — no `/done` route exists yet.

- [ ] **Step 3: Add the handler and route**

Modify `internal/apps/notes/handlers.go`. Add after `collapse`:

```go
// done marks a bullet done or not. The field names the state to arrive at,
// exactly like collapsed's, so a double submit or a stale page cannot flip
// it back.
func (a *App) done(w http.ResponseWriter, r *http.Request) {
	raw := r.PostFormValue("done")
	if raw != "0" && raw != "1" {
		a.deps.Errors.Status(w, r, http.StatusBadRequest)
		return
	}
	done := raw == "1"

	a.mutate(w, r, func(ctx context.Context, o *Ops, m mutation) error {
		return o.SetDone(ctx, m.UserID, m.NodeID, done)
	})
}
```

Modify `internal/apps/notes/app.go`. Add one route to `Mount`, and update the route-map comment (this task only adds `/{id}/done` — Tasks 3–5 each add their own line and their own routes, one at a time):

```go
//	GET  /notes/notes.js     a literal segment, so it outranks {id} even
//	                         though "notes.js" is not a valid id
//	POST /notes/new          a literal segment, and literals outrank {id}
//	POST /notes/{id}/text    and the eight other mutations: two segments
//	                         deeper than the zoom URL, so no pattern in this
//	                         list is a prefix of another
func (a *App) Mount(r *app.Router, deps app.Deps) {
	a.deps = deps
	a.store = NewStore(deps.DB)

	r.HandleFunc("GET /{$}", a.outline)
	r.HandleFunc("GET /{id}", a.outlineZoomed)
	r.HandleFunc("GET /notes.js", a.script)
	r.HandleFunc("POST /new", a.create)
	r.HandleFunc("POST /{id}/text", a.setText)
	r.HandleFunc("POST /{id}/indent", a.indent)
	r.HandleFunc("POST /{id}/outdent", a.outdent)
	r.HandleFunc("POST /{id}/move", a.move)
	r.HandleFunc("POST /{id}/collapse", a.collapse)
	r.HandleFunc("POST /{id}/delete", a.remove)
	r.HandleFunc("POST /{id}/done", a.done)
}
```

- [ ] **Step 4: Add the checkbox and strikethrough**

Modify `internal/apps/notes/templates/outline.html`. Change the row wrapper to carry a done class:

```html
<div class="outline-row{{if .Done}} outline-row-done{{end}}" data-id="{{.ID}}">
```

Add the done checkbox right after the opening `<form>` tag's hidden fields and before the `{{if .HasChildren}}` chevron block:

```html
				<button type="submit" class="outline-done quiet"
				        formaction="/notes/{{.ID}}/done"
				        hx-post="/notes/{{.ID}}/done" hx-target="#outline" hx-swap="innerHTML"
				        name="done" value="{{if .Done}}0{{else}}1{{end}}"
				        aria-pressed="{{if .Done}}true{{else}}false{{end}}"
				        aria-label="{{if .Done}}Mark not done{{else}}Mark done{{end}}"
				        >{{if .Done}}&#9745;{{else}}&#9744;{{end}}</button>

```

(It sits before the chevron, so the row reads checkbox → chevron → dot → text.)

- [ ] **Step 5: Add the CSS**

Modify `internal/ui/static/app.css`. Add after the `.outline-chevron, .outline-dot { ... }` rule block:

```css
.outline-done {
	flex: none;
	width: 1.1rem;
	line-height: 1.7;
	text-align: center;
	padding: 0;
	border: none;
	background: none;
	color: var(--c-text-faint);
}
.outline-done:hover { color: var(--c-text); }

/* A done bullet is struck through everywhere its text appears — the
 * rendered overlay and the raw input both, since text-decoration on an
 * ancestor is not reliably painted through a form control in every
 * browser, unlike plain inline text. */
.outline-row-done .outline-title-rendered,
.outline-row-done .outline-note-rendered,
.outline-row-done input.outline-title,
.outline-row-done input.outline-note { text-decoration: line-through; }
```

- [ ] **Step 6: Run the tests**

Run: `go test ./internal/apps/notes/... -v`
Expected: PASS, all of it.

- [ ] **Step 7: Full check**

Run: `gofmt -l . && go build ./... && go vet ./... && go run honnef.co/go/tools/cmd/staticcheck@v0.8.0 ./... && go test ./... -race`
Expected: clean.

- [ ] **Step 8: Commit**

```bash
git add internal/apps/notes/handlers.go internal/apps/notes/app.go internal/apps/notes/templates/outline.html internal/ui/static/app.css internal/apps/notes/handlers_test.go
git commit -m "notes: mark a bullet done, struck through"
```

---

### Task 3: Due dates on the bullet

**Files:**
- Modify: `internal/apps/notes/view.go`
- Modify: `internal/apps/notes/handlers.go`
- Modify: `internal/apps/notes/app.go`
- Modify: `internal/apps/notes/templates/outline.html`
- Modify: `internal/ui/static/app.css`
- Modify: `internal/apps/notes/handlers_test.go`

**Interfaces:**
- Consumes: `(*Ops).SetDue` (Task 1).
- Produces: `(*App).due`, mounted at `POST /notes/{id}/due`. `outlineRow.Overdue bool`; `nest(flat []Node, root int64, csrfToken, today string) []*outlineRow` — the signature every later caller of `nest` must match.

- [ ] **Step 1: Write the failing tests**

Add to `internal/apps/notes/handlers_test.go`:

```go
func TestDueSetsAndClearsTheChip(t *testing.T) {
	s := newServer(t)
	id := s.seed(t, s.alice, notes.RootID, "task")

	s.submit(t, s.alice, "/notes/"+itoa(id)+"/due", url.Values{
		"root": {"0"}, "due": {"2026-03-05"},
	}, "/notes/")

	doc := s.get(t, s.alice, "/notes/")
	chip := doc.MustHave(".outline-due-chip")
	if got := htmlassert.Text(chip); got != "2026-03-05" {
		t.Errorf("chip text = %q, want 2026-03-05", got)
	}

	s.submit(t, s.alice, "/notes/"+itoa(id)+"/due", url.Values{
		"root": {"0"}, "due": {""},
	}, "/notes/")
	s.get(t, s.alice, "/notes/").MustNotHave(".outline-due-chip")
}

func TestDueRejectsBadFormat(t *testing.T) {
	s := newServer(t)
	id := s.seed(t, s.alice, notes.RootID, "task")

	rec := s.post(t, s.alice, "/notes/"+itoa(id)+"/due", url.Values{
		"root": {"0"}, "due": {"not-a-date"},
	})
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

func TestDueOnAnotherUsersBulletIs404(t *testing.T) {
	s := newServer(t)
	id := s.seed(t, s.bob, notes.RootID, "bob's")

	rec := s.post(t, s.alice, "/notes/"+itoa(id)+"/due", url.Values{
		"root": {"0"}, "due": {"2026-03-05"},
	})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

// TestOverdueChipIsMarked doesn't depend on the real clock: it sets a due
// date far enough in the past (year 2000) that it will read as overdue for
// the entire lifetime of this test suite.
func TestOverdueChipIsMarked(t *testing.T) {
	s := newServer(t)
	id := s.seed(t, s.alice, notes.RootID, "task")
	if err := s.store.SetDue(context.Background(), s.alice.user.ID, id, "2000-01-01"); err != nil {
		t.Fatal(err)
	}

	doc := s.get(t, s.alice, "/notes/")
	doc.MustHave(".outline-due-overdue")
}

func TestAFutureDueChipIsNotMarkedOverdue(t *testing.T) {
	s := newServer(t)
	id := s.seed(t, s.alice, notes.RootID, "task")
	future := time.Now().AddDate(1, 0, 0).Format("2006-01-02")
	if err := s.store.SetDue(context.Background(), s.alice.user.ID, id, future); err != nil {
		t.Fatal(err)
	}

	s.get(t, s.alice, "/notes/").MustNotHave(".outline-due-overdue")
}
```

Add `"time"` to `handlers_test.go`'s import block if it is not already present.

- [ ] **Step 2: Run the tests to see them fail**

Run: `go test ./internal/apps/notes/... -run 'Due|Overdue' -v`
Expected: FAIL — no chip, no due-date input yet.

- [ ] **Step 3: Add the `due` handler and route**

Modify `internal/apps/notes/handlers.go`. Add after `done` (from Task 2):

```go
// due sets or clears a bullet's due date. An empty value clears it, which is
// what a native <input type="date">'s own clear affordance sends — there is
// no separate "remove due date" control. ValidateDue's error already maps
// to a 400 through fail, so there is nothing to check ahead of mutate here.
func (a *App) due(w http.ResponseWriter, r *http.Request) {
	due := r.PostFormValue("due")
	a.mutate(w, r, func(ctx context.Context, o *Ops, m mutation) error {
		return o.SetDue(ctx, m.UserID, m.NodeID, due)
	})
}
```

Modify `internal/apps/notes/app.go`. Add one route to `Mount`, right after `/{id}/done`:

```go
	r.HandleFunc("POST /{id}/due", a.due)
```

- [ ] **Step 4: Add `Overdue` and thread `today` through `nest`**

Modify `internal/apps/notes/view.go`. Add a field to `outlineRow`, after `RenderedNote`:

```go
	// Overdue is DueOn set and in the past, relative to the day nest was
	// called — spec §11: comparison is against the server's local date.
	// Computed once here, not in the template, for the same reason
	// RenderedTitle/RenderedNote are: a recursive block gets one argument.
	Overdue bool
```

Change `nest`'s signature and row construction:

```go
func nest(flat []Node, root int64, csrfToken, today string) []*outlineRow {
	var top []*outlineRow

	open := make([]*outlineRow, 0, MaxDepth+1)

	for _, n := range flat {
		row := &outlineRow{
			Node: n, RootID: root, CSRFToken: csrfToken,
			RenderedTitle: Render(n.Title),
			RenderedNote:  Render(n.Note),
			Overdue:       n.DueOn != "" && n.DueOn < today,
		}
```

(The rest of `nest` is unchanged.)

- [ ] **Step 5: Update `nest`'s two callers**

Modify `internal/apps/notes/handlers.go`. Add `"time"` to the import block:

```go
import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/iliafrenkel/on-suite/internal/platform/render"
	"github.com/iliafrenkel/on-suite/internal/platform/web"
)
```

In `renderOutline`, change:

```go
	view.Rows = nest(flat, rootID, view.CSRFToken)
```

to:

```go
	view.Rows = nest(flat, rootID, view.CSRFToken, time.Now().Format("2006-01-02"))
```

In `renderOutlineFragment`, change the same line the same way:

```go
	view.Rows = nest(flat, rootID, view.CSRFToken, time.Now().Format("2006-01-02"))
```

- [ ] **Step 6: Add the due-date input and the chip**

Modify `internal/apps/notes/templates/outline.html`. Add the chip right after the closing `</span>` of `outline-text` (the one wrapping the two `.outline-field` spans) and before `<span class="outline-actions">`:

```html
				{{$row := .}}
				{{with .DueOn}}
				<a class="outline-due-chip{{if $row.Overdue}} outline-due-overdue{{end}}"
				   href="/notes/due">{{.}}</a>
				{{end}}
```

(`$row` captures the row before `{{with .DueOn}}` rebinds `.` to the string value — without it, `.Overdue` inside the block would be looked up on the string, not the row.)

Add the due-date input as the first control inside `<span class="outline-actions">`, before the move-up button:

```html
					<input type="date" class="outline-due-input" name="due" value="{{.DueOn}}"
					       aria-label="Due date"
					       hx-post="/notes/{{.ID}}/due" hx-target="#outline" hx-swap="innerHTML"
					       hx-trigger="change">
```

- [ ] **Step 7: Add the CSS**

Modify `internal/ui/static/app.css`. Add after the strikethrough rule from Task 2:

```css
.outline-due-chip {
	flex: none;
	font-size: var(--fs-sm);
	color: var(--c-text-dim);
	text-decoration: none;
	white-space: nowrap;
}
.outline-due-chip:hover { color: var(--c-accent); }
.outline-due-overdue { color: var(--c-danger); }

.outline-due-input {
	font: inherit;
	font-size: var(--fs-sm);
	color: var(--c-text-faint);
	background: none;
	border: none;
	width: 6.5rem;
}
```

- [ ] **Step 8: Run the tests**

Run: `go test ./internal/apps/notes/... -v`
Expected: PASS, all of it.

- [ ] **Step 9: Full check**

Run: `gofmt -l . && go build ./... && go vet ./... && go run honnef.co/go/tools/cmd/staticcheck@v0.8.0 ./... && go test ./... -race`
Expected: clean.

- [ ] **Step 10: Commit**

```bash
git add internal/apps/notes/view.go internal/apps/notes/handlers.go internal/apps/notes/app.go internal/apps/notes/templates/outline.html internal/ui/static/app.css internal/apps/notes/handlers_test.go
git commit -m "notes: due dates, with a chip that reddens once past"
```

---

### Task 4: Show completed

**Files:**
- Create: `internal/apps/notes/prefs.go`
- Modify: `internal/apps/notes/view.go`
- Modify: `internal/apps/notes/handlers.go`
- Modify: `internal/apps/notes/app.go`
- Modify: `internal/apps/notes/templates/outline.html`
- Modify: `internal/ui/static/app.css`
- Modify: `internal/apps/notes/handlers_test.go`
- Modify: `internal/apps/notes/view_test.go` (the file already exists, from N2)

**Interfaces:**
- Consumes: `nest`'s `today`-taking signature (Task 3).
- Produces: `ShowCompletedCookie` (exported const), `showCompletedFrom(r) bool`; `hideDone(flat []Node, showCompleted bool) []Node`; `outlineView.ShowCompleted bool`; `(*App).prefs`, mounted at `POST /notes/prefs`; `renderOutlineFragment` gains a fifth parameter, `showCompleted bool` — every caller must be updated.

**A cookie staleness trap to avoid:** `prefs` sets the new cookie value on the *response* via `http.SetCookie`, but the *incoming* request `r` still carries whatever value the browser sent — reading the cookie back from `r` immediately after setting it would render the *old* setting. That is why `renderOutlineFragment` takes `showCompleted` as an explicit parameter instead of reading the cookie itself: `mutate`'s call passes the freshly-read cookie (nothing changed it this request), and `prefs`'s call passes the value it just computed directly, never reading it back from `r`.

- [ ] **Step 1: Write the failing tests**

Add to `internal/apps/notes/view.go`'s test file. Since `hideDone` is unexported, this goes in `internal/apps/notes/view_test.go`, which is already `package notes` (not `notes_test`) for exactly this reason — see its own file comment from N2.

```go
func TestHideDoneHidesADoneNodeAndItsSubtree(t *testing.T) {
	flat := []Node{
		{ID: 1, Title: "done", Done: true, Depth: 0},
		{ID: 2, Title: "child of done", Depth: 1},
		{ID: 3, Title: "grandchild", Depth: 2},
		{ID: 4, Title: "sibling", Depth: 0},
	}
	got := hideDone(flat, false)
	if len(got) != 1 || got[0].ID != 4 {
		t.Fatalf("hideDone(false) = %+v, want only the sibling", got)
	}
}

func TestHideDoneShowsEverythingWhenOn(t *testing.T) {
	flat := []Node{
		{ID: 1, Title: "done", Done: true, Depth: 0},
		{ID: 2, Title: "child of done", Depth: 1},
	}
	got := hideDone(flat, true)
	if len(got) != 2 {
		t.Fatalf("hideDone(true) = %+v, want everything", got)
	}
}

// TestHideDoneHandlesConsecutiveDoneSubtrees: two separate done subtrees
// back to back must not let one's skip swallow the other's sibling.
func TestHideDoneHandlesConsecutiveDoneSubtrees(t *testing.T) {
	flat := []Node{
		{ID: 1, Title: "done A", Done: true, Depth: 0},
		{ID: 2, Title: "child of A", Depth: 1},
		{ID: 3, Title: "between", Depth: 0},
		{ID: 4, Title: "done B", Done: true, Depth: 0},
		{ID: 5, Title: "child of B", Depth: 1},
		{ID: 6, Title: "after", Depth: 0},
	}
	got := hideDone(flat, false)
	var ids []int64
	for _, n := range got {
		ids = append(ids, n.ID)
	}
	want := []int64{3, 6}
	if len(ids) != len(want) || ids[0] != want[0] || ids[1] != want[1] {
		t.Fatalf("hideDone left %v, want %v", ids, want)
	}
}
```

Add to `internal/apps/notes/handlers_test.go`:

```go
// TestShowCompletedHidesAndReveals is spec §11 end to end: a done bullet's
// whole subtree disappears from the outline until the preference is on.
func TestShowCompletedHidesAndReveals(t *testing.T) {
	s := newServer(t)
	ctx := context.Background()
	parent := s.seed(t, s.alice, notes.RootID, "parent")
	s.seed(t, s.alice, parent, "child")
	if err := s.store.SetDone(ctx, s.alice.user.ID, parent, true); err != nil {
		t.Fatal(err)
	}

	doc := s.get(t, s.alice, "/notes/")
	if strings.Contains(doc.Text(), "child") {
		t.Error("a done bullet's child is visible with show-completed off")
	}
	doc.MustNotHave(`input[name=title]`) // the done parent itself is gone too

	req := httptest.NewRequest("GET", "/notes/", nil)
	req.AddCookie(&http.Cookie{Name: notes.ShowCompletedCookie, Value: "1"})
	rec := s.do(t, s.alice, req)
	if !strings.Contains(rec.Body.String(), "child") {
		t.Error("show-completed=1 still hides the done bullet's child")
	}
}

func TestPrefsTogglesTheCookie(t *testing.T) {
	s := newServer(t)

	rec := s.post(t, s.alice, "/notes/prefs", url.Values{
		"root": {"0"}, "show_completed": {"1"},
	})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303", rec.Code)
	}
	var got *http.Cookie
	for _, c := range rec.Result().Cookies() {
		if c.Name == notes.ShowCompletedCookie {
			got = c
		}
	}
	if got == nil || got.Value != "1" {
		t.Fatalf("show-completed cookie = %+v, want value 1", got)
	}
}

// TestPrefsRespondsWithTheFreshValueOverHTMX guards the staleness trap: the
// fragment this returns must reflect the setting just toggled, not whatever
// the request's own (pre-toggle) cookie said.
func TestPrefsRespondsWithTheFreshValueOverHTMX(t *testing.T) {
	s := newServer(t)
	ctx := context.Background()
	parent := s.seed(t, s.alice, notes.RootID, "parent")
	s.seed(t, s.alice, parent, "child")
	if err := s.store.SetDone(ctx, s.alice.user.ID, parent, true); err != nil {
		t.Fatal(err)
	}

	rec := s.postHX(t, s.alice, "/notes/prefs", url.Values{
		"root": {"0"}, "show_completed": {"1"},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "child") {
		t.Error("toggling show-completed on did not reveal the child in the same response")
	}
}

func TestPrefsRejectsAnUnknownValue(t *testing.T) {
	s := newServer(t)
	for _, v := range []string{"", "true", "2"} {
		rec := s.post(t, s.alice, "/notes/prefs", url.Values{
			"root": {"0"}, "show_completed": {v},
		})
		if rec.Code != http.StatusBadRequest {
			t.Errorf("show_completed=%q gave %d, want 400", v, rec.Code)
		}
	}
}
```

- [ ] **Step 2: Run the tests to see them fail**

Run: `go test ./internal/apps/notes/... -run 'HideDone|ShowCompleted|Prefs' -v`
Expected: FAIL — `hideDone`, `ShowCompletedCookie` and `/notes/prefs` do not exist yet.

- [ ] **Step 3: Add `hideDone`**

Modify `internal/apps/notes/view.go`. Add after `markLast`:

```go
// hideDone drops a done node and everything under it, unless showCompleted
// is true — spec §11. Completing a parent does not complete its children
// (Task 1), so a child's own Done never matters once an ancestor's already
// hides it: this walks flat in the pre-order Outline guarantees, skipping
// everything deeper than the most recently hidden node until depth returns
// to that node's own level or shallower.
func hideDone(flat []Node, showCompleted bool) []Node {
	if showCompleted {
		return flat
	}
	var out []Node
	skipBelow := -1
	for _, n := range flat {
		if skipBelow >= 0 && n.Depth > skipBelow {
			continue
		}
		skipBelow = -1
		if n.Done {
			skipBelow = n.Depth
			continue
		}
		out = append(out, n)
	}
	return out
}
```

- [ ] **Step 4: Add the cookie helper**

Create `internal/apps/notes/prefs.go`:

```go
package notes

import "net/http"

// ShowCompletedCookie holds whether done bullets should render — spec §11.
// Unlike the platform's theme/font cookies (written client-side by
// theme.js), this one is set server-side by POST /notes/prefs: it changes
// what the outline query returns, not just how an already-loaded page
// looks, and spec §7 requires every behaviour here to work with JavaScript
// off — a plain form, not a JS cookie write, is what makes that true.
const ShowCompletedCookie = "onsuite_notes_show_completed"

// showCompletedFrom reads the preference, defaulting to false: a fresh
// browser sees completed bullets hidden, matching the outline's own default
// of showing what is still open.
func showCompletedFrom(r *http.Request) bool {
	c, err := r.Cookie(ShowCompletedCookie)
	return err == nil && c.Value == "1"
}
```

- [ ] **Step 5: Wire it into rendering and add the `prefs` handler**

Modify `internal/apps/notes/view.go`. Add a field to `outlineView`, keeping every existing field and its comment exactly as they are:

```go
// outlineView is what the outline template renders.
type outlineView struct {
	// Root is the node the outline is zoomed to. At the top level it is the
	// zero Node, whose ID is RootID — so the template can write
	// .Root.ID into a hidden field without a special case.
	Root   Node
	Zoomed bool
	// Crumbs are Root's ancestors, outermost first. Empty unless Zoomed.
	Crumbs []Node
	// Rows is the visible outline, nested. Empty means the outline shows one
	// empty bullet instead — spec §6.
	Rows      []*outlineRow
	CSRFToken string
	// ShowCompleted is spec §11's preference, read once per request so the
	// toolbar's toggle button can show its own opposite action.
	ShowCompleted bool
}
```

Modify `internal/apps/notes/handlers.go`. In `renderOutline`, change:

```go
	view := outlineView{CSRFToken: web.CSRFToken(r.Context())}
```

to:

```go
	showCompleted := showCompletedFrom(r)
	view := outlineView{CSRFToken: web.CSRFToken(r.Context()), ShowCompleted: showCompleted}
```

and change:

```go
	flat, err := a.store.Outline(r.Context(), userID, rootID)
	if err != nil {
		a.deps.Errors.Internal(w, r, err)
		return
	}
	view.Rows = nest(flat, rootID, view.CSRFToken, time.Now().Format("2006-01-02"))
```

to:

```go
	flat, err := a.store.Outline(r.Context(), userID, rootID)
	if err != nil {
		a.deps.Errors.Internal(w, r, err)
		return
	}
	flat = hideDone(flat, showCompleted)
	view.Rows = nest(flat, rootID, view.CSRFToken, time.Now().Format("2006-01-02"))
```

Change `renderOutlineFragment`'s signature and body — it now takes `showCompleted` explicitly rather than assuming the request's own cookie is still current (see this task's header note on why). Its doc comment above the signature is unchanged; only the code from `func` onward is shown here:

```go
func (a *App) renderOutlineFragment(w http.ResponseWriter, r *http.Request, userID, rootID int64, showCompleted bool) {
	flat, err := a.store.Outline(r.Context(), userID, rootID)
	if err != nil {
		a.deps.Errors.Internal(w, r, err)
		return
	}
	flat = hideDone(flat, showCompleted)
	view := outlineView{
		CSRFToken: web.CSRFToken(r.Context()),
		Root:      Node{ID: rootID},
	}
	view.Rows = nest(flat, rootID, view.CSRFToken, time.Now().Format("2006-01-02"))
	if err := a.deps.Render.Fragment(w, http.StatusOK, "notes/outline", "outline-body", view); err != nil {
		a.deps.Errors.Internal(w, r, err)
	}
}
```

Update `mutate`'s call to it — a request that isn't `prefs` never changes this cookie, so reading it fresh from `r` is already correct:

```go
	if web.IsHTMX(r) {
		a.renderOutlineFragment(w, r, userID, root, showCompletedFrom(r))
		return
	}
```

Add the `prefs` handler, after `done`/`due`:

```go
// prefs sets the show-completed preference — spec §11. This is a plain POST
// rather than a JS cookie write, and picks up the platform's CSRF
// protection for exactly that reason (see prefs.go).
func (a *App) prefs(w http.ResponseWriter, r *http.Request) {
	raw := r.PostFormValue("show_completed")
	if raw != "0" && raw != "1" {
		a.deps.Errors.Status(w, r, http.StatusBadRequest)
		return
	}
	root, ok := formID(r, "root")
	if !ok {
		a.deps.Errors.Status(w, r, http.StatusBadRequest)
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     ShowCompletedCookie,
		Value:    raw,
		Path:     "/notes/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})

	if web.IsHTMX(r) {
		userID, ok := a.userID(w, r)
		if !ok {
			return
		}
		// The value just computed, not showCompletedFrom(r): r still
		// carries whatever the browser sent on this request, before the
		// SetCookie above, which the browser will only start sending back
		// on its *next* one.
		a.renderOutlineFragment(w, r, userID, root, raw == "1")
		return
	}
	http.Redirect(w, r, outlinePath(root), http.StatusSeeOther)
}
```

- [ ] **Step 6: Register the route**

Modify `internal/apps/notes/app.go`. Add to `Mount`, and add a line to the route-map comment above it, alongside the `GET /notes/notes.js` line:

```go
//	POST /notes/prefs        likewise a literal segment; N5's show-completed
//	                         toggle
```

```go
	r.HandleFunc("POST /prefs", a.prefs)
```

(Placed with the other literal-segment routes, alongside `POST /new`.)

- [ ] **Step 7: Add the toolbar and toggle button**

Modify `internal/apps/notes/templates/outline.html`. Replace the start of the `content` block:

```html
{{define "content"}}
<div class="stack notes">
	{{if .Data.Zoomed}}
```

with:

```html
{{define "content"}}
<div class="stack notes">
	<div class="notes-toolbar">
		<form method="post" action="/notes/prefs" class="notes-show-completed"
		      hx-post="/notes/prefs" hx-target="#outline" hx-swap="innerHTML">
			<input type="hidden" name="csrf_token" value="{{.Data.CSRFToken}}">
			<input type="hidden" name="root" value="{{.Data.Root.ID}}">
			<button type="submit" class="quiet"
			        name="show_completed" value="{{if .Data.ShowCompleted}}0{{else}}1{{end}}">
				{{if .Data.ShowCompleted}}Hide completed{{else}}Show completed{{end}}
			</button>
		</form>
	</div>
	{{if .Data.Zoomed}}
```

- [ ] **Step 8: Add the CSS**

Modify `internal/ui/static/app.css`. Add after the due-date CSS from Task 3:

```css
.notes-toolbar {
	display: flex;
	justify-content: flex-end;
	font-size: var(--fs-sm);
}
```

- [ ] **Step 9: Run the tests**

Run: `go test ./internal/apps/notes/... -v`
Expected: PASS, all of it.

- [ ] **Step 10: Full check**

Run: `gofmt -l . && go build ./... && go vet ./... && go run honnef.co/go/tools/cmd/staticcheck@v0.8.0 ./... && go test ./... -race`
Expected: clean.

- [ ] **Step 11: Commit**

```bash
git add internal/apps/notes/prefs.go internal/apps/notes/view.go internal/apps/notes/handlers.go internal/apps/notes/app.go internal/apps/notes/templates/outline.html internal/ui/static/app.css internal/apps/notes/handlers_test.go internal/apps/notes/view_test.go
git commit -m "notes: hide done bullets and their subtree unless show-completed is on"
```

---

### Task 5: `/notes/due`

**Files:**
- Create: `internal/apps/notes/due.go`
- Create: `internal/apps/notes/due_test.go`
- Create: `internal/apps/notes/templates/due.html`
- Modify: `internal/apps/notes/handlers.go`
- Modify: `internal/apps/notes/app.go`
- Modify: `internal/apps/notes/templates/outline.html`
- Modify: `internal/ui/static/app.css`
- Modify: `internal/apps/notes/handlers_test.go`

**Interfaces:**
- Consumes: `Store.Ancestors` (existing, N1).
- Produces: `DueRow{Node; Crumbs []Node; Overdue bool}` (exported — see Step 1's note on why); `DueGroups{Overdue, Today, ThisWeek, Later []DueRow}`; `GroupByDue(rows []DueRow, today time.Time) DueGroups`; `(DueGroups).Sections() []dueSection`; `(*Store).Due(ctx, userID) ([]Node, error)`; `(*App).dueList`, mounted at `GET /notes/due`.

- [ ] **Step 1: Write the failing tests for grouping and the store query**

Create `internal/apps/notes/due_test.go`:

```go
package notes_test

import (
	"context"
	"testing"
	"time"

	"github.com/iliafrenkel/on-suite/internal/apps/notes"
)

func TestDueReturnsOnlyNodesWithADueDate(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	withDue := f.mk(t, notes.RootID, "has a date")
	f.mk(t, notes.RootID, "no date")

	if err := f.store.SetDue(ctx, f.alice.ID, withDue.ID, "2026-03-05"); err != nil {
		t.Fatal(err)
	}

	got, err := f.store.Due(ctx, f.alice.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != withDue.ID {
		t.Fatalf("Due = %+v, want only the one with a date", got)
	}
}

func TestDueExcludesDoneNodes(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	n := f.mk(t, notes.RootID, "done but due")
	if err := f.store.SetDue(ctx, f.alice.ID, n.ID, "2026-03-05"); err != nil {
		t.Fatal(err)
	}
	if err := f.store.SetDone(ctx, f.alice.ID, n.ID, true); err != nil {
		t.Fatal(err)
	}

	got, err := f.store.Due(ctx, f.alice.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("Due = %+v, want none — it is done", got)
	}
}

func TestDueDoesNotLeakAnotherUsersNodes(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	bobs := f.mk(t, notes.RootID, "bob's")
	if err := f.store.SetDue(ctx, f.bob.ID, bobs.ID, "2026-03-05"); err != nil {
		t.Fatal(err)
	}

	got, err := f.store.Due(ctx, f.alice.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("alice's Due = %+v, want none", got)
	}
}
```

Grouping is exercised through `notes_test` (external) since `DueRow`/`GroupByDue` are exported. Append this table-driven test to `due_test.go` as well:

```go
func TestGroupByDueBucketsRelativeToToday(t *testing.T) {
	today := time.Date(2026, 3, 10, 0, 0, 0, 0, time.UTC)
	rows := []struct {
		id  int64
		due string
	}{
		{1, "2026-03-01"}, // overdue
		{2, "2026-03-10"}, // today
		{3, "2026-03-14"}, // this week (today + 4 days)
		{4, "2026-03-16"}, // this week (today + 6 days, inclusive boundary)
		{5, "2026-03-17"}, // later (today + 7 days)
	}
	var in []notes.DueRow
	for _, r := range rows {
		in = append(in, notes.DueRow{Node: notes.Node{ID: r.id, DueOn: r.due}})
	}

	got := notes.GroupByDue(in, today)
	check := func(name string, group []notes.DueRow, wantID int64) {
		if len(group) != 1 || group[0].ID != wantID {
			t.Errorf("%s = %+v, want exactly id %d", name, group, wantID)
		}
	}
	check("Overdue", got.Overdue, 1)
	check("Today", got.Today, 2)
	if len(got.ThisWeek) != 2 || got.ThisWeek[0].ID != 3 || got.ThisWeek[1].ID != 4 {
		t.Errorf("ThisWeek = %+v, want ids 3 and 4", got.ThisWeek)
	}
	check("Later", got.Later, 5)

	if !got.Overdue[0].Overdue {
		t.Error("the overdue row's own Overdue field is false")
	}
	if got.Today[0].Overdue {
		t.Error("today's row is marked Overdue")
	}
}
```

`DueRow` is capitalised here because this test lives in `notes_test` (external test package, like every other `_test.go` file in this package except `view_test.go`) — the type must be exported for it to construct one. Name it `DueRow` (exported) when you write `due.go` in Step 3, not `dueRow`.

- [ ] **Step 2: Run the tests to see them fail**

Run: `go test ./internal/apps/notes/... -run 'Due|GroupByDue' -v`
Expected: FAIL — `Store.Due`, `DueRow` and `GroupByDue` do not exist yet.

- [ ] **Step 3: Add `due.go`**

Create `internal/apps/notes/due.go`:

```go
package notes

import (
	"fmt"
	"time"
)

// DueRow is one entry in /notes/due: a node plus its ancestor breadcrumb,
// outermost first — spec §11 says each hit shows its ancestor path so a
// result three levels deep is legible on its own. Overdue is set by
// GroupByDue, once, rather than computed in the template — the same reason
// outlineRow.Overdue exists.
type DueRow struct {
	Node
	Crumbs  []Node
	Overdue bool
}

// DueGroups is /notes/due's four buckets, spec §11's Overdue / Today / This
// week / Later — in that display order.
type DueGroups struct {
	Overdue, Today, ThisWeek, Later []DueRow
}

type dueSection struct {
	Title string
	Rows  []DueRow
}

// Sections lists the four groups in display order, for the template to
// range over instead of hardcoding four separate blocks.
func (g DueGroups) Sections() []dueSection {
	return []dueSection{
		{"Overdue", g.Overdue},
		{"Today", g.Today},
		{"This week", g.ThisWeek},
		{"Later", g.Later},
	}
}

// GroupByDue buckets rows, which must already have DueOn set (Store.Due
// only returns those), against today — spec §11: comparison is against the
// server's local date, a single-household deployment in one timezone. "This
// week" runs through the sixth day from today inclusive: a week that starts
// today, not a calendar week, so what counts as "this week" does not jump
// around depending on what day it is.
func GroupByDue(rows []DueRow, today time.Time) DueGroups {
	todayStr := today.Format("2006-01-02")
	weekEnd := today.AddDate(0, 0, 6).Format("2006-01-02")

	var g DueGroups
	for _, row := range rows {
		switch {
		case row.DueOn < todayStr:
			row.Overdue = true
			g.Overdue = append(g.Overdue, row)
		case row.DueOn == todayStr:
			g.Today = append(g.Today, row)
		case row.DueOn <= weekEnd:
			g.ThisWeek = append(g.ThisWeek, row)
		default:
			g.Later = append(g.Later, row)
		}
	}
	return g
}

// Due returns every one of userID's nodes with a due date set, excluding
// done ones — spec §11's "done and archived nodes are excluded". archived_at
// does not exist until N7, so only done is excluded here; see this plan's
// Global Constraints for why that is not a judgment call. Ordered by due_on
// so GroupByDue only has to bucket, never sort.
func (st *Store) Due(ctx context.Context, userID int64) ([]Node, error) {
	rows, err := st.db.QueryContext(ctx,
		`SELECT `+nodeColumns+`
		   FROM notes_nodes
		  WHERE user_id = ? AND due_on IS NOT NULL AND done_at IS NULL
		  ORDER BY due_on`, userID)
	if err != nil {
		return nil, fmt.Errorf("notes: due nodes: %w", err)
	}
	return collectNodes(rows, "due nodes")
}
```

`Due` needs `"context"` too — add it to the import block:

```go
import (
	"context"
	"fmt"
	"time"
)
```

- [ ] **Step 4: Add the handler, route, and template**

Modify `internal/apps/notes/handlers.go`. Add after `prefs`:

```go
// dueList renders every one of the user's due bullets, grouped by urgency —
// spec §11.
func (a *App) dueList(w http.ResponseWriter, r *http.Request) {
	userID, ok := a.userID(w, r)
	if !ok {
		return
	}
	nodes, err := a.store.Due(r.Context(), userID)
	if err != nil {
		a.deps.Errors.Internal(w, r, err)
		return
	}

	rows := make([]DueRow, len(nodes))
	for i, n := range nodes {
		crumbs, err := a.store.Ancestors(r.Context(), userID, n.ID)
		if err != nil {
			a.deps.Errors.Internal(w, r, err)
			return
		}
		rows[i] = DueRow{Node: n, Crumbs: crumbs}
	}

	page := a.deps.Page(r, "Due")
	page.Data = GroupByDue(rows, time.Now())
	a.render(w, r, http.StatusOK, "notes/due", page)
}
```

Modify `internal/apps/notes/app.go`. Add to `Mount`, alongside `GET /notes.js`, and add a line to the route-map comment above it:

```go
//	GET  /notes/due          likewise a literal segment; N5's due-date list
```

```go
	r.HandleFunc("GET /due", a.dueList)
```

Create `internal/apps/notes/templates/due.html`:

```html
{{define "content"}}
<div class="stack notes">
	<h1>Due</h1>
	{{range .Data.Sections}}
	{{if .Rows}}
	<section class="notes-due-group">
		<h2>{{.Title}}</h2>
		<ul class="notes-due-list">
			{{range .Rows}}
			<li class="notes-due-item">
				{{if .Crumbs}}
				<span class="notes-due-crumbs">
					{{range .Crumbs}}{{.DisplayTitle}} / {{end}}
				</span>
				{{end}}
				<a href="/notes/{{.ID}}" class="notes-due-title">{{.DisplayTitle}}</a>
				<span class="outline-due-chip{{if .Overdue}} outline-due-overdue{{end}}">{{.DueOn}}</span>
			</li>
			{{end}}
		</ul>
	</section>
	{{end}}
	{{end}}
	{{if not (or .Data.Overdue .Data.Today .Data.ThisWeek .Data.Later)}}
	<p class="dim">Nothing is due.</p>
	{{end}}
</div>
{{end}}
```

- [ ] **Step 5: Link to it from the outline**

Modify `internal/apps/notes/templates/outline.html`. Add the link into the toolbar added in Task 4, right before the show-completed form:

```html
	<div class="notes-toolbar">
		<a href="/notes/due">Due</a>
		<form method="post" action="/notes/prefs" class="notes-show-completed"
```

- [ ] **Step 6: Add the CSS**

Modify `internal/ui/static/app.css`. Add after the toolbar rule from Task 4:

```css
.notes-due-group + .notes-due-group { margin-top: var(--s-4); }
.notes-due-group h2 {
	font-size: var(--fs-sm);
	color: var(--c-text-dim);
	text-transform: uppercase;
	letter-spacing: 0.04em;
}
.notes-due-list { margin: 0; padding: 0; list-style: none; }
.notes-due-item {
	display: flex;
	align-items: baseline;
	gap: var(--s-2);
	padding: var(--s-1) 0;
	border-bottom: 1px solid var(--c-border);
}
.notes-due-crumbs { font-size: var(--fs-sm); color: var(--c-text-faint); }
.notes-due-title { flex: 1; min-width: 0; color: var(--c-text); text-decoration: none; }
.notes-due-title:hover { color: var(--c-accent); }
```

- [ ] **Step 7: Handler tests**

Add to `internal/apps/notes/handlers_test.go`:

```go
func TestDueListGroupsAcrossTheWholeTree(t *testing.T) {
	s := newServer(t)
	ctx := context.Background()
	parent := s.seed(t, s.alice, notes.RootID, "Projects")
	child := s.seed(t, s.alice, parent, "AtBudget")
	if err := s.store.SetDue(ctx, s.alice.user.ID, child, "2000-01-01"); err != nil {
		t.Fatal(err)
	}

	doc := s.get(t, s.alice, "/notes/due")
	doc.MustHave(".outline-due-overdue")
	if !strings.Contains(doc.Text(), "Projects") {
		t.Error("the due bullet's ancestor breadcrumb is missing")
	}
	link := doc.MustHave(`a[href=/notes/` + itoa(child) + `]`)
	if got := htmlassert.Text(link); got != "AtBudget" {
		t.Errorf("due list link text = %q, want AtBudget", got)
	}
}

func TestDueListRendersAnotherUsersNodesNowhere(t *testing.T) {
	s := newServer(t)
	ctx := context.Background()
	bobs := s.seed(t, s.bob, notes.RootID, "bob's task")
	if err := s.store.SetDue(ctx, s.bob.user.ID, bobs, "2000-01-01"); err != nil {
		t.Fatal(err)
	}

	if strings.Contains(s.get(t, s.alice, "/notes/due").Text(), "bob's task") {
		t.Error("another user's due bullet is on the page")
	}
}

func TestDueListRequiresSignIn(t *testing.T) {
	s := newServer(t)
	rec := s.do(t, nil, httptest.NewRequest("GET", "/notes/due", nil))
	if rec.Code != http.StatusSeeOther {
		t.Errorf("GET /notes/due anonymous = %d, want a 303 to the login page", rec.Code)
	}
}
```

- [ ] **Step 8: Run the tests**

Run: `go test ./internal/apps/notes/... -v`
Expected: PASS, all of it.

- [ ] **Step 9: Full check**

Run: `gofmt -l . && go build ./... && go vet ./... && go run honnef.co/go/tools/cmd/staticcheck@v0.8.0 ./... && go test ./... -race`
Expected: clean.

- [ ] **Step 10: Commit**

```bash
git add internal/apps/notes/due.go internal/apps/notes/due_test.go internal/apps/notes/templates/due.html internal/apps/notes/handlers.go internal/apps/notes/app.go internal/apps/notes/templates/outline.html internal/ui/static/app.css internal/apps/notes/handlers_test.go
git commit -m "notes: add /notes/due, grouped Overdue / Today / This week / Later"
```

---

### Task 6: Keyboard — Cmd+Enter toggles done

**Files:**
- Modify: `internal/apps/notes/static/notes.js`

**Interfaces:**
- Consumes: the `done` button rendered in Task 2 (`button.outline-done`), the existing `handleKeydown`/`click`/`collapseButton`-style helpers from N3.

- [ ] **Step 1: Add the binding**

Modify `internal/apps/notes/static/notes.js`. Add a helper alongside `collapseButton`:

```js
	function doneButton(row) { return row.querySelector("button.outline-done"); }
```

In `handleKeydown`, add a check immediately before the `Cmd+.` (collapse) check — it must come before the plain `Enter && !shiftKey` check below it, or a `Cmd+Enter` press would fall through to `splitAndCreate` instead:

```js
		if ((e.metaKey || e.ctrlKey) && e.key === "Enter") {
			e.preventDefault();
			click(doneButton(row));
			return;
		}
		if ((e.metaKey || e.ctrlKey) && e.key === ".") {
			e.preventDefault();
			click(collapseButton(row));
			return;
		}
```

- [ ] **Step 2: Manual QA**

`notes.js` has no automated tests, by design (spec §17) — every request it issues is one Task 2's handler tests already cover. With `go run ./cmd/onsuite serve` running:

1. Focus a bullet's title. Press `Cmd+Enter` (macOS) or `Ctrl+Enter` (elsewhere). Confirm the bullet strikes through and the page shows it as done — the same effect as clicking its checkbox.
2. Press it again. Confirm it returns to not-done.
3. Confirm plain `Enter` (no modifier) still splits and creates a new bullet as it did before this task — this is the regression this ordering exists to prevent.

- [ ] **Step 3: Commit**

```bash
git add internal/apps/notes/static/notes.js
git commit -m "notes: bind Cmd+Enter to toggle done"
```

---

### Task 7: Docs and final verification

**Files:**
- Modify: `README.md`
- Modify: `internal/apps/notes/notes.go`

- [ ] **Step 1: Update README.md**

Replace, in the intro paragraph:

> **ON Notes** is a hierarchical outliner — one infinite tree per account, with zoom, collapse, every structural operation, a full keyboard layer and inline Markdown (bold, links, `#tags`). Due dates and search are still being built out.

with:

> **ON Notes** is a hierarchical outliner — one infinite tree per account, with zoom, collapse, every structural operation, a full keyboard layer, inline Markdown (bold, links, `#tags`), and done/due tracking with a cross-tree due-date view. Search is still being built out.

Replace, in the Status section:

> Work since then is per-app rather than per-phase. ON Notes is being built in
> ten small chunks under
> [`docs/superpowers/plans/2026-08-25-on-notes-design.md`](docs/superpowers/specs/2026-08-25-on-notes-design.md);
> N1 (schema and store), N2 (the outline), N3 (the keyboard layer) and N4
> (Markdown) are done.

with:

> Work since then is per-app rather than per-phase. ON Notes is being built in
> ten small chunks under
> [`docs/superpowers/plans/2026-08-25-on-notes-design.md`](docs/superpowers/specs/2026-08-25-on-notes-design.md);
> N1 (schema and store), N2 (the outline), N3 (the keyboard layer), N4
> (Markdown) and N5 (done + due dates) are done.

- [ ] **Step 2: Update the package doc comment**

Modify `internal/apps/notes/notes.go`:

```go
// The design is in docs/superpowers/specs/2026-08-25-on-notes-design.md. This
// package spans chunk N1 (schema and store) through N5 (done and due dates):
// app.App, routes, templates, handlers, the keyboard layer in
// static/notes.js, the inline Markdown renderer in markdown.go, and task
// tracking in tree.go, prefs.go and due.go.
```

- [ ] **Step 3: Final full check**

Run: `gofmt -l . && go build ./... && go vet ./... && go run honnef.co/go/tools/cmd/staticcheck@v0.8.0 ./... && go test ./... -race`
Expected: clean.

- [ ] **Step 4: Manual browser pass**

With `go run ./cmd/onsuite serve` running, in one sitting: create a few bullets, due-date one in the past and one in the future, mark one done, confirm strikethrough and disappearance/reappearance under "show completed", visit `/notes/due` and confirm grouping and breadcrumbs, and exercise `Cmd+Enter` from the keyboard.

- [ ] **Step 5: Commit**

```bash
git add README.md internal/apps/notes/notes.go
git commit -m "notes: mark N5 (done + due dates) done"
```

---

## Self-review notes for the executing agent

- **`renderOutlineFragment` takes `showCompleted` as an explicit parameter, unlike `renderOutline`, which reads the cookie itself.** This asymmetry is deliberate, not an inconsistency to "fix" by making both read the cookie internally: `prefs`'s HTMX response needs the *just-toggled* value, and `r`'s own cookie still carries the *pre-toggle* one (`http.SetCookie` only affects what the browser sends on its *next* request). Every other caller of `renderOutlineFragment` (`mutate`) simply passes `showCompletedFrom(r)`, since nothing about the current request changes that cookie.
- **`hideDone` is a pure Go function over the flat, already-fetched `[]Node`, not a SQL change to `Store.Outline`.** This was a deliberate choice over adding a `showCompleted` parameter to `Outline`'s own recursive CTE: the flat, depth-annotated list `Outline` already returns is exactly what a linear pre-order subtree-skip needs, and doing it in Go leaves N1–N4's extensive existing `Outline` test suite (and its signature, used at every call site in `store_test.go`) completely untouched.
- **A node whose only children are all done-and-hidden still renders its expand chevron** (`HasChildren` is not adjusted for what `hideDone` will later hide). This is an accepted, minor rough edge, not a bug: `HasChildren` is computed by `Store.Outline` before `hideDone` ever runs, and threading "will this child survive hideDone" back into that SQL would meaningfully complicate the query for a rare case (a bullet whose children are *all* done) whose worst outcome is a chevron that toggles with no visible change.
- **`Ancestors` is deliberately not filtered by done state**, in `/notes/due`'s breadcrumbs or anywhere else. Zooming into a specific node by id already bypasses `hideDone` entirely (it only ever governs a parent's listing of its own children) — filtering ancestors would be an inconsistent, unrequested addition on top of that.
- **The due-date `<input type="date">` and the always-visible chip are two separate elements, not one.** The input lives in the hover-only `.outline-actions` (editing is a secondary action, like indent/move); the chip is always visible next to the title, because the whole point of an at-a-glance due indicator is that it doesn't require hovering to see.

## Follow-ups to file as issues

1. `/notes/due` excludes done nodes but not archived ones — spec §11 asks for both, but `archived_at` does not exist until N7. Whichever of N5/N7 lands second should add `AND archived_at IS NULL` to `Store.Due`'s query.
2. A node whose only children are all done and hidden still shows an expand chevron that reveals nothing when clicked (see self-review notes). Worth a look if it turns out to bother real usage.
3. The due-date input has no explicit min/max and no client-side validation beyond what the browser's native date picker offers; malformed input past that reaches `ValidateDue` and 400s, which is correct but produces a generic error page rather than inline feedback next to the field. Revisit once the app has any established pattern for inline form errors — it doesn't have one yet.
