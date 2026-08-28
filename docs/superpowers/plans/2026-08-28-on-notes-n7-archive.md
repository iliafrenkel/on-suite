# ON Notes N7 (Archive) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add archiving to ON Notes — spec §13 of
[docs/superpowers/specs/2026-08-25-on-notes-design.md](../specs/2026-08-25-on-notes-design.md).
An `archived_at` column, a `POST /notes/{id}/archive` route that both
archives and restores, and `GET /notes/archive` listing the roots of what
has been put away.

**Architecture:** One nullable TEXT column, exactly like N5's `done_at`.
Archiving is a leaf-level write (`Ops.SetArchived`/`Store.SetArchived`) —
nothing cascades onto descendants. What changes is which nodes three
existing queries are willing to return: `Store.Outline`'s recursive descent
gets a predicate that keeps it from ever walking into an archived node in
the first place, and `Store.Search` / `Store.Due` — which match flatly
against `notes_nodes` rather than descending from a root — gain a shared
recursive CTE that names every id sitting at or under an archived node, so
they can exclude the same set a different way.

**Tech Stack:** Go, `database/sql` against `modernc.org/sqlite`, `html/template`,
HTMX. No new dependencies.

## Global Constraints

- Migrations are forward-only: this chunk adds `archived_at` in its own new
  file, `0004_archive.sql`; `0001`–`0003` are never touched.
- `Store`/`Ops` have no HTTP knowledge — every method here takes plain Go
  values, never `*http.Request`.
- Spec §7's one-write rule: a structural POST that can be issued from a row
  mid-edit must save that row's title and note and perform its own
  operation in a single transaction. Archiving a bullet is exactly such an
  action (it is triggered from the outline's row menu); restoring one is
  not (it is triggered from `/notes/archive`, a page with no bullet being
  edited) — see Task 3's design note for how the single `archive` handler
  honours this distinction.
- `archived_at` is a nullable TEXT timestamp, NULL meaning "not archived" —
  the same representation `done_at` already uses (spec §4).
- Spec §13: an archived node and its subtree disappear from the outline
  (§6), from search results (§12), and from `/notes/due` (§11). Share pages
  are out of scope until N9.
- Spec §13: archiving is independent of done — a node can be either, both,
  or neither. Nothing here ever reads or writes `done_at`, and nothing in
  N5 ever reads or writes `archived_at`.
- No new dependencies ([CONTRIBUTING.md](../../../CONTRIBUTING.md)).
- HTMX is the only JS, and every action must work with JavaScript off first
  — spec §2, §7. Every new form below is a plain `method="post"`/`method="get"`
  form with `hx-*` attributes layered on top, never JS-only behaviour.

---

## File Structure

- `internal/apps/notes/migrations/0004_archive.sql` — new. Adds the column.
- `internal/apps/notes/notes.go` — modify. `Node.Archived bool`.
- `internal/apps/notes/store.go` — modify. `nodeColumns`, `scanNode`, a
  shared `archivedBelowCTE`, and the `Outline`/`Search`/`Due` queries that
  need to exclude archived subtrees. (`Due` lives in `due.go`, not here —
  see below.)
- `internal/apps/notes/tree.go` — modify. `Ops.SetArchived`/`Store.SetArchived`.
- `internal/apps/notes/search.go` — modify. `Store.Search` excludes
  archived subtrees.
- `internal/apps/notes/due.go` — modify. `Store.Due` excludes archived
  subtrees.
- `internal/apps/notes/archive.go` — new. `ArchiveRow`, `archiveView`,
  `Store.Archive`, mirroring `due.go`'s shape.
- `internal/apps/notes/handlers.go` — modify. `archiveList`, `archive`,
  `restore`, `renderArchiveFragment`, `buildArchiveView`.
- `internal/apps/notes/app.go` — modify. Two new routes.
- `internal/apps/notes/templates/archive.html` — new.
- `internal/apps/notes/templates/outline.html` — modify. An "Archive" row
  action; an "Archive" toolbar link.
- `internal/apps/notes/templates/due.html`, `templates/search.html` —
  modify. An "Archive" toolbar link, for reachability from every page.
- `internal/ui/static/app.css` — modify. `.notes-archive-*` rules and one
  new toolbar link.
- `internal/apps/notes/tree_test.go`, `store_test.go`, `search_test.go`,
  `due_test.go`, `handlers_test.go` — modify.
- `internal/apps/notes/archive_test.go` — new.
- `README.md`, `internal/apps/notes/notes.go` (package doc) — modify.

---

### Task 1: Schema, `Node.Archived`, and `SetArchived`

**Files:**
- Create: `internal/apps/notes/migrations/0004_archive.sql`
- Modify: `internal/apps/notes/notes.go`
- Modify: `internal/apps/notes/store.go`
- Modify: `internal/apps/notes/tree.go`
- Test: `internal/apps/notes/tree_test.go`

**Interfaces:**
- Produces: `Node.Archived bool`; `(*Ops).SetArchived(ctx, userID, id int64, archived bool) error`;
  `(*Store).SetArchived(ctx, userID, id int64, archived bool) error`. Every
  later task in this plan calls one of these.

- [ ] **Step 1: Write the migration**

```sql
ALTER TABLE notes_nodes ADD COLUMN archived_at TEXT;
```

Save as `internal/apps/notes/migrations/0004_archive.sql`.

- [ ] **Step 2: Add `Node.Archived`**

In `internal/apps/notes/notes.go`, the `Node` struct currently reads:

```go
	// Done and DueOn are done_at/due_on's Go projections — spec §11. Done
	// hides the underlying timestamp: nothing in this app ever needs to
	// show *when* a bullet was completed, only whether it is. DueOn is the
	// raw 'YYYY-MM-DD' string, or "" for none — a due date is a calendar
	// date, not an instant, so there is no time.Time here either.
	Done      bool
	DueOn     string
	CreatedAt time.Time
	UpdatedAt time.Time
```

Replace it with:

```go
	// Done and DueOn are done_at/due_on's Go projections — spec §11. Done
	// hides the underlying timestamp: nothing in this app ever needs to
	// show *when* a bullet was completed, only whether it is. DueOn is the
	// raw 'YYYY-MM-DD' string, or "" for none — a due date is a calendar
	// date, not an instant, so there is no time.Time here either.
	Done  bool
	DueOn string
	// Archived is archived_at's Go projection — spec §13. Same reasoning as
	// Done: nothing in this app ever needs to show *when* a bullet was put
	// away, only whether it is.
	Archived  bool
	CreatedAt time.Time
	UpdatedAt time.Time
```

Nothing else in `notes.go` changes: archiving has no user-supplied text and
so needs no `Validate`-style function, unlike `ValidateDue`.

- [ ] **Step 3: Extend `nodeColumns` and `scanNode`**

In `internal/apps/notes/store.go`, `nodeColumns` currently reads:

```go
const nodeColumns = `id, user_id, parent_id, position, title, note, collapsed, created_at, updated_at, done_at, due_on`
```

Change it to:

```go
const nodeColumns = `id, user_id, parent_id, position, title, note, collapsed, created_at, updated_at, done_at, due_on, archived_at`
```

`scanNode` currently reads:

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

Change it to:

```go
func scanNode(row rowScanner, extra ...any) (Node, error) {
	var (
		n          Node
		parent     sql.NullInt64
		createdAt  string
		updatedAt  string
		doneAt     sql.NullString
		dueOn      sql.NullString
		archivedAt sql.NullString
	)
	dest := append([]any{
		&n.ID, &n.UserID, &parent, &n.Position, &n.Title, &n.Note,
		&n.Collapsed, &createdAt, &updatedAt, &doneAt, &dueOn, &archivedAt,
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
	n.Archived = archivedAt.Valid

	if n.CreatedAt, err = parseTime(createdAt); err != nil {
		return Node{}, err
	}
	if n.UpdatedAt, err = parseTime(updatedAt); err != nil {
		return Node{}, err
	}
	return n, nil
}
```

- [ ] **Step 4: Write the failing tests for `SetArchived`**

Append to `internal/apps/notes/tree_test.go`, near `TestSetDoneRoundTrips`
and its neighbours:

```go
func TestSetArchivedRoundTrips(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	n := f.mk(t, notes.RootID, "task")

	if err := f.store.SetArchived(ctx, f.alice.ID, n.ID, true); err != nil {
		t.Fatalf("SetArchived(true): %v", err)
	}
	got, err := f.store.ByID(ctx, f.alice.ID, n.ID)
	if err != nil {
		t.Fatalf("ByID: %v", err)
	}
	if !got.Archived {
		t.Fatal("Archived = false after SetArchived(true)")
	}

	if err := f.store.SetArchived(ctx, f.alice.ID, n.ID, false); err != nil {
		t.Fatalf("SetArchived(false): %v", err)
	}
	if got, _ = f.store.ByID(ctx, f.alice.ID, n.ID); got.Archived {
		t.Fatal("Archived = true after SetArchived(false)")
	}
}

// TestSetArchivedDoesNotTouchChildren mirrors TestSetDoneDoesNotTouchChildren:
// archiving a parent does not write archived_at onto its children — hiding
// the subtree is a display decision made in the queries that build the
// outline, search results and due list (Task 2), never a change to the
// child's own row.
func TestSetArchivedDoesNotTouchChildren(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	parent := f.mk(t, notes.RootID, "parent")
	child := f.mk(t, parent.ID, "child")

	if err := f.store.SetArchived(ctx, f.alice.ID, parent.ID, true); err != nil {
		t.Fatalf("SetArchived: %v", err)
	}
	got, err := f.store.ByID(ctx, f.alice.ID, child.ID)
	if err != nil {
		t.Fatalf("ByID: %v", err)
	}
	if got.Archived {
		t.Fatal("the child was marked archived by its parent's SetArchived")
	}
}

// TestSetArchivedIsIndependentOfDone is spec §13: a node can be either,
// both, or neither.
func TestSetArchivedIsIndependentOfDone(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	n := f.mk(t, notes.RootID, "task")

	if err := f.store.SetDone(ctx, f.alice.ID, n.ID, true); err != nil {
		t.Fatal(err)
	}
	if err := f.store.SetArchived(ctx, f.alice.ID, n.ID, true); err != nil {
		t.Fatal(err)
	}

	got, err := f.store.ByID(ctx, f.alice.ID, n.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Done || !got.Archived {
		t.Fatalf("got Done=%v Archived=%v, want both true", got.Done, got.Archived)
	}

	if err := f.store.SetArchived(ctx, f.alice.ID, n.ID, false); err != nil {
		t.Fatal(err)
	}
	if got, _ = f.store.ByID(ctx, f.alice.ID, n.ID); !got.Done || got.Archived {
		t.Fatalf("after un-archiving: Done=%v Archived=%v, want Done true, Archived false", got.Done, got.Archived)
	}
}

func TestSetArchivedRejectsAnotherUsersNode(t *testing.T) {
	f := newFixture(t)
	n := f.mk(t, notes.RootID, "alice's")

	err := f.store.SetArchived(context.Background(), f.bob.ID, n.ID, true)
	if !errors.Is(err, notes.ErrNotFound) {
		t.Fatalf("SetArchived on another user's node = %v; want ErrNotFound", err)
	}
}
```

- [ ] **Step 5: Run the tests to verify they fail**

Run: `go test ./internal/apps/notes/... -run TestSetArchived -v`
Expected: FAIL — `f.store.SetArchived` and `got.Archived` do not exist yet.

- [ ] **Step 6: Implement `Ops.SetArchived`/`Store.SetArchived`**

In `internal/apps/notes/tree.go`, immediately after `Store.SetDue` (which
ends with `func (st *Store) SetDue(ctx context.Context, userID, id int64, due string) error { ... }`)
and before `SetText`, insert:

```go
// SetArchived marks a bullet archived, or restores it — spec §13. Like
// SetDone, this only ever touches the one row: an archived node's subtree
// disappearing from the outline, search and due list (Task 2) is a display
// decision made in the queries that build those views, never something
// recorded on every descendant.
func (o *Ops) SetArchived(ctx context.Context, userID, id int64, archived bool) error {
	archivedAt := sql.NullString{}
	if archived {
		archivedAt = sql.NullString{String: formatTime(o.now()), Valid: true}
	}
	return o.update(ctx, "set archived",
		`UPDATE notes_nodes SET archived_at = ?, updated_at = ? WHERE id = ? AND user_id = ?`,
		archivedAt, formatTime(o.now()), id, userID)
}

// SetArchived marks a bullet archived, or restores it, in a transaction of
// its own. See Ops.SetArchived.
func (st *Store) SetArchived(ctx context.Context, userID, id int64, archived bool) error {
	return st.Do(ctx, func(o *Ops) error { return o.SetArchived(ctx, userID, id, archived) })
}
```

- [ ] **Step 7: Run the tests to verify they pass**

Run: `go test ./internal/apps/notes/... -v`
Expected: PASS. The whole package, not just the new tests — `nodeColumns`
and `scanNode` are shared by every query in the package, so this is the
step that would catch a column-order mistake breaking something unrelated.

- [ ] **Step 8: Commit**

```bash
git add internal/apps/notes/migrations/0004_archive.sql \
        internal/apps/notes/notes.go internal/apps/notes/store.go \
        internal/apps/notes/tree.go internal/apps/notes/tree_test.go
git commit -m "feat(notes): add archived_at and SetArchived"
```

---

### Task 2: Exclude archived subtrees from the outline, search and due list

**Files:**
- Modify: `internal/apps/notes/store.go`
- Modify: `internal/apps/notes/search.go`
- Modify: `internal/apps/notes/due.go`
- Test: `internal/apps/notes/store_test.go`
- Test: `internal/apps/notes/search_test.go`
- Test: `internal/apps/notes/due_test.go`

**Interfaces:**
- Consumes: `Node.Archived` and `(*Store).SetArchived` from Task 1.
- Produces: `Store.Outline`, `Store.Search` and `Store.Due` all now exclude
  an archived node and everything under it, unconditionally — there is no
  "show archived" preference anywhere in the design. Their signatures are
  unchanged; only which rows they return changes. `archivedBelowCTE`, a new
  unexported `const` in `store.go`, is shared by `Search` and `Due`.

Outline reaches every visible node by descending from a root, so it can
simply refuse to descend into an archived node — its recursive step never
finds one to walk further from, so nothing under it can be reached either.
Search and Due instead match flatly against `notes_nodes` (an FTS hit, or a
row with `due_on` set) with no notion of "root" to descend from, so they
need a way to ask "is this id itself archived, or does it sit under a node
that is" for a match that might be arbitrarily deep. `archivedBelowCTE`
answers that once, as a table of ids, so both queries can exclude against
it with a single `NOT IN`.

- [ ] **Step 1: Write the failing tests**

Append to `internal/apps/notes/store_test.go`, near the other `Outline`
tests (`TestOutlineStopsAtACollapsedNode` and neighbours):

```go
// TestOutlineExcludesAnArchivedNodeAndItsSubtree is spec §13: an archived
// node, and everything under it, disappears from the outline.
func TestOutlineExcludesAnArchivedNodeAndItsSubtree(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	archived := f.mk(t, notes.RootID, "put away")
	child := f.mk(t, archived.ID, "child of put away")
	sibling := f.mk(t, notes.RootID, "still visible")

	if err := f.store.SetArchived(ctx, f.alice.ID, archived.ID, true); err != nil {
		t.Fatal(err)
	}

	got, err := f.store.Outline(ctx, f.alice.ID, notes.RootID)
	if err != nil {
		t.Fatal(err)
	}
	var ids []int64
	for _, n := range got {
		ids = append(ids, n.ID)
	}
	if len(ids) != 1 || ids[0] != sibling.ID {
		t.Fatalf("Outline = %v, want only the sibling %d (not %d or its child %d)",
			ids, sibling.ID, archived.ID, child.ID)
	}
}

// TestOutlineHasChildrenIgnoresAnArchivedChild: a parent whose only child
// has been archived must not still claim it has children — the chevron
// would then expand into nothing, contradicting Outline's own promise that
// the result is exactly what is on screen.
func TestOutlineHasChildrenIgnoresAnArchivedChild(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	parent := f.mk(t, notes.RootID, "parent")
	child := f.mk(t, parent.ID, "child")
	if err := f.store.SetArchived(ctx, f.alice.ID, child.ID, true); err != nil {
		t.Fatal(err)
	}

	got, err := f.store.Outline(ctx, f.alice.ID, notes.RootID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != parent.ID {
		t.Fatalf("Outline = %+v, want only the parent", got)
	}
	if got[0].HasChildren {
		t.Error("HasChildren is true, but the only child is archived")
	}
}
```

Append to `internal/apps/notes/search_test.go`:

```go
// TestSearchExcludesArchivedNodes is spec §13's other half of §12: unlike
// done, there is no toggle that can bring an archived hit back.
func TestSearchExcludesArchivedNodes(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	n := f.mk(t, notes.RootID, "put away")
	if err := f.store.SetArchived(ctx, f.alice.ID, n.ID, true); err != nil {
		t.Fatal(err)
	}

	got, err := f.store.Search(ctx, f.alice.ID, "away", false)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("Search = %+v, want none — it is archived", got)
	}
}

// TestSearchExcludesADescendantOfAnArchivedNode: the match itself is not
// archived, but its ancestor is — spec §13's "subtree" applies here too.
func TestSearchExcludesADescendantOfAnArchivedNode(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	parent := f.mk(t, notes.RootID, "put away")
	child := f.mk(t, parent.ID, "buried treasure")
	if err := f.store.SetArchived(ctx, f.alice.ID, parent.ID, true); err != nil {
		t.Fatal(err)
	}

	got, err := f.store.Search(ctx, f.alice.ID, "treasure", false)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("Search = %+v, want none — %d sits under an archived node", got, child.ID)
	}
}
```

Append to `internal/apps/notes/due_test.go`:

```go
// TestDueExcludesArchivedNodes is spec §11 and §13.
func TestDueExcludesArchivedNodes(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	n := f.mk(t, notes.RootID, "put away but due")
	if err := f.store.SetDue(ctx, f.alice.ID, n.ID, "2026-03-05"); err != nil {
		t.Fatal(err)
	}
	if err := f.store.SetArchived(ctx, f.alice.ID, n.ID, true); err != nil {
		t.Fatal(err)
	}

	got, err := f.store.Due(ctx, f.alice.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("Due = %+v, want none — it is archived", got)
	}
}

// TestDueExcludesADescendantOfAnArchivedNode mirrors the search case: the
// due node itself is not archived, but an ancestor is.
func TestDueExcludesADescendantOfAnArchivedNode(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	parent := f.mk(t, notes.RootID, "put away")
	child := f.mk(t, parent.ID, "due")
	if err := f.store.SetDue(ctx, f.alice.ID, child.ID, "2026-03-05"); err != nil {
		t.Fatal(err)
	}
	if err := f.store.SetArchived(ctx, f.alice.ID, parent.ID, true); err != nil {
		t.Fatal(err)
	}

	got, err := f.store.Due(ctx, f.alice.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("Due = %+v, want none — it sits under an archived node", got)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/apps/notes/... -run 'TestOutlineExcludesAnArchived|TestOutlineHasChildrenIgnoresAnArchived|TestSearchExcludesArchived|TestSearchExcludesADescendant|TestDueExcludesArchived|TestDueExcludesADescendant' -v`
Expected: FAIL — every one of these currently returns the archived/buried
node because nothing excludes it yet.

- [ ] **Step 3: Add the shared `archivedBelowCTE` constant**

In `internal/apps/notes/store.go`, immediately after the `childColumns`
variable block (which ends `childColumns = aliasNodeColumns("c")` followed
by the closing `)`), insert:

```go
// archivedBelowCTE names every one of a user's node ids that is archived,
// or sits anywhere under an archived ancestor — spec §13: "an archived
// node and its subtree disappear from ... search results" (§12), and
// "archived nodes are excluded" from /notes/due (§11). Search and Due both
// match against notes_nodes directly rather than descending from a root
// the way Outline does, so unlike Outline — whose recursive walk simply
// never reaches an archived node's children in the first place — they
// need this table of ids to exclude explicitly. It takes two ?
// placeholders, both the caller's user_id.
//
// UNION, not UNION ALL: an id already found cannot be found again, which
// is what makes this safe against a cycle a bug might have introduced —
// with a finite, deduplicated id space, the recursion has nowhere left to
// go once every reachable id is already in it, the same guarantee
// Outline's own MaxDepth cap gives its own descent.
const archivedBelowCTE = `
	archived_below(id) AS (
	    SELECT id FROM notes_nodes WHERE user_id = ? AND archived_at IS NOT NULL
	  UNION
	    SELECT c.id FROM notes_nodes c JOIN archived_below a ON c.parent_id = a.id
	     WHERE c.user_id = ?
	)`
```

- [ ] **Step 4: Exclude archived subtrees from `Store.Outline`**

In `internal/apps/notes/store.go`, `Outline`'s doc comment currently ends
with the paragraph starting "Both the descent and the has_children test
match on the owner as well as on the parent...". Immediately before that
paragraph, insert a new one:

```go
// An archived node, and everything under it, is kept out of tree the same
// way a collapsed node's children are: the recursive step's own WHERE
// clause excludes it, so nothing can ever join onto it as a parent —
// spec §13. This is not a preference the way hideDone's showCompleted is
// (view.go): there is no "show archived" toggle anywhere in the design, so
// exclusion belongs in the query itself rather than in a post-filter that
// would need one.
//
```

The method body currently reads:

```go
func (st *Store) Outline(ctx context.Context, userID, rootID int64) ([]Node, error) {
	rows, err := st.db.QueryContext(ctx,
		`WITH RECURSIVE tree AS (
		     SELECT `+nodeColumns+`, 0 AS depth, printf('%08d', position) AS path
		       FROM notes_nodes
		      WHERE user_id = ? AND parent_id IS ?
		   UNION ALL
		     SELECT `+childColumns+`,
		            t.depth + 1, t.path || '/' || printf('%08d', c.position)
		       FROM notes_nodes c JOIN tree t ON c.parent_id = t.id
		      WHERE c.user_id = t.user_id AND t.collapsed = 0 AND t.depth + 1 <= ?
		 )
		 SELECT `+nodeColumns+`, depth,
		        EXISTS (SELECT 1 FROM notes_nodes k
		                 WHERE k.user_id = tree.user_id AND k.parent_id = tree.id)
		   FROM tree ORDER BY path`,
		userID, parentArg(rootID), MaxDepth)
```

Change the query (only the query string; the rest of the method, including
the `userID, parentArg(rootID), MaxDepth` arguments, is unchanged) to:

```go
func (st *Store) Outline(ctx context.Context, userID, rootID int64) ([]Node, error) {
	rows, err := st.db.QueryContext(ctx,
		`WITH RECURSIVE tree AS (
		     SELECT `+nodeColumns+`, 0 AS depth, printf('%08d', position) AS path
		       FROM notes_nodes
		      WHERE user_id = ? AND parent_id IS ? AND archived_at IS NULL
		   UNION ALL
		     SELECT `+childColumns+`,
		            t.depth + 1, t.path || '/' || printf('%08d', c.position)
		       FROM notes_nodes c JOIN tree t ON c.parent_id = t.id
		      WHERE c.user_id = t.user_id AND c.archived_at IS NULL
		        AND t.collapsed = 0 AND t.depth + 1 <= ?
		 )
		 SELECT `+nodeColumns+`, depth,
		        EXISTS (SELECT 1 FROM notes_nodes k
		                 WHERE k.user_id = tree.user_id AND k.parent_id = tree.id
		                   AND k.archived_at IS NULL)
		   FROM tree ORDER BY path`,
		userID, parentArg(rootID), MaxDepth)
```

- [ ] **Step 5: Exclude archived subtrees from `Store.Search`**

In `internal/apps/notes/search.go`, the doc comment on `Search` currently
ends:

```go
// The query references notes_fts by its real name rather than through a
// table alias: this driver's FTS5 support resolves MATCH and rank against
// an aliased virtual table with "no such column" errors, so both stay
// unaliased while notes_nodes is joined in as n.
func (st *Store) Search(ctx context.Context, userID int64, query string, showCompleted bool) ([]Node, error) {
	q := ftsQuery(query)
	if q == "" {
		return nil, nil
	}
	rows, err := st.db.QueryContext(ctx,
		`SELECT `+aliasNodeColumns("n")+`
		   FROM notes_fts
		   JOIN notes_nodes n ON n.id = notes_fts.rowid
		  WHERE notes_fts MATCH ? AND n.user_id = ? AND (? OR n.done_at IS NULL)
		  ORDER BY rank`,
		q, userID, showCompleted)
	if err != nil {
		return nil, fmt.Errorf("notes: search: %w", err)
	}
	return collectNodes(rows, "search results")
}
```

And the earlier part of the same doc comment currently says:

```go
// Search runs a full-text search over title and note across userID's whole
// tree — spec §12. An empty query (nothing left after ftsQuery) returns no
// rows rather than asking FTS5 to MATCH an empty string, which is a syntax
// error of its own. Results are ordered by FTS5's own relevance rank, and,
// like Store.Due, honour showCompleted: a done bullet that matches is
// excluded unless the preference is on. Archived nodes are not excluded —
// archived_at does not exist until N7 (see this plan's Global Constraints).
```

Replace the whole doc comment and function with:

```go
// Search runs a full-text search over title and note across userID's whole
// tree — spec §12. An empty query (nothing left after ftsQuery) returns no
// rows rather than asking FTS5 to MATCH an empty string, which is a syntax
// error of its own. Results are ordered by FTS5's own relevance rank, and,
// like Store.Due, honour showCompleted: a done bullet that matches is
// excluded unless the preference is on. A node that is archived, or that
// sits under an archived ancestor, is excluded unconditionally — spec
// §13 — via archivedBelowCTE (store.go), shared with Store.Due.
//
// The query references notes_fts by its real name rather than through a
// table alias: this driver's FTS5 support resolves MATCH and rank against
// an aliased virtual table with "no such column" errors, so both stay
// unaliased while notes_nodes is joined in as n.
func (st *Store) Search(ctx context.Context, userID int64, query string, showCompleted bool) ([]Node, error) {
	q := ftsQuery(query)
	if q == "" {
		return nil, nil
	}
	rows, err := st.db.QueryContext(ctx,
		`WITH RECURSIVE `+archivedBelowCTE+`
		 SELECT `+aliasNodeColumns("n")+`
		   FROM notes_fts
		   JOIN notes_nodes n ON n.id = notes_fts.rowid
		  WHERE notes_fts MATCH ? AND n.user_id = ? AND (? OR n.done_at IS NULL)
		    AND n.id NOT IN (SELECT id FROM archived_below)
		  ORDER BY rank`,
		userID, userID, q, userID, showCompleted)
	if err != nil {
		return nil, fmt.Errorf("notes: search: %w", err)
	}
	return collectNodes(rows, "search results")
}
```

Note the argument order: `archivedBelowCTE` contributes the first two `?`
placeholders (both `userID`), then the main query's three (`q`, `userID`,
`showCompleted`) follow in the order they appear.

- [ ] **Step 6: Exclude archived subtrees from `Store.Due`**

In `internal/apps/notes/due.go`, `Due` currently reads:

```go
// Due returns every one of userID's nodes with a due date set, excluding
// done ones — spec §11's "done and archived nodes are excluded". archived_at
// does not exist until N7 (see this plan's Global Constraints), so only
// done is excluded here; see this plan's Global Constraints for why that
// is not a judgment call. Ordered by due_on so GroupByDue only has to
// bucket, never sort.
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

Replace it with:

```go
// Due returns every one of userID's nodes with a due date set, excluding
// done ones and archived ones — spec §11's "done and archived nodes are
// excluded". A node that sits under an archived ancestor is excluded too,
// via archivedBelowCTE (store.go), shared with Store.Search — spec §13's
// subtree rule applies here exactly as it does there. Ordered by due_on so
// GroupByDue only has to bucket, never sort.
func (st *Store) Due(ctx context.Context, userID int64) ([]Node, error) {
	rows, err := st.db.QueryContext(ctx,
		`WITH RECURSIVE `+archivedBelowCTE+`
		 SELECT `+nodeColumns+`
		   FROM notes_nodes
		  WHERE user_id = ? AND due_on IS NOT NULL AND done_at IS NULL
		    AND id NOT IN (SELECT id FROM archived_below)
		  ORDER BY due_on`, userID, userID, userID)
	if err != nil {
		return nil, fmt.Errorf("notes: due nodes: %w", err)
	}
	return collectNodes(rows, "due nodes")
}
```

- [ ] **Step 7: Run the tests to verify they pass**

Run: `go test ./internal/apps/notes/... -v`
Expected: PASS, the whole package.

- [ ] **Step 8: Commit**

```bash
git add internal/apps/notes/store.go internal/apps/notes/search.go \
        internal/apps/notes/due.go internal/apps/notes/store_test.go \
        internal/apps/notes/search_test.go internal/apps/notes/due_test.go
git commit -m "feat(notes): exclude archived subtrees from outline, search and due"
```

---

### Task 3: `Store.Archive` and the `/notes/archive` routes

**Files:**
- Create: `internal/apps/notes/archive.go`
- Create: `internal/apps/notes/archive_test.go`
- Modify: `internal/apps/notes/handlers.go`
- Modify: `internal/apps/notes/app.go`
- Test: `internal/apps/notes/handlers_test.go`

**Interfaces:**
- Consumes: `Node.Archived`, `(*Store).SetArchived`, `(*Store).Ancestors`
  (existing), `outlinePath` (existing, `handlers.go`), `a.mutate`/`mutation`
  (existing, `handlers.go`).
- Produces: `ArchiveRow{Node; Crumbs []Node}`; `archiveView{Rows []ArchiveRow; CSRFToken string}`;
  `(*Store).Archive(ctx, userID int64) ([]Node, error)`; handlers `archiveList`,
  `archive`, `restore`; `POST /notes/{id}/archive` and `GET /notes/archive`.
  Task 4's templates render `archiveView` and post to these two routes.

**Design note — why `archive` forks into `restore` on the field's own value.**
Archiving a bullet happens from a row in the outline (Task 4 adds the menu
button), which may carry unsaved text — the same reason every other
structural operation goes through `mutate`, which saves that text and
performs the operation in one transaction (spec §7). Restoring a bullet
happens from a row on `/notes/archive`, a page with no outline row being
edited at all, and the response it needs back is that page's own list, not
`#outline` — `mutate`'s hard-coded response (an outline fragment, or a
redirect to the outline) cannot serve it. Rather than add a second route
the spec's own route table (§9) does not list, the one `POST
/notes/{id}/archive` handler reads the `archived` field first and only
calls into `mutate` for `archived=1`; `archived=0` calls a separate
`restore` function that talks to the store directly, the same way `prefs`
does for its own cookie write.

- [ ] **Step 1: Write the failing tests for `Store.Archive`**

Create `internal/apps/notes/archive_test.go`:

```go
package notes_test

import (
	"context"
	"testing"

	"github.com/iliafrenkel/on-suite/internal/apps/notes"
)

func TestArchiveListsATopLevelArchivedNode(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	n := f.mk(t, notes.RootID, "put away")
	f.mk(t, notes.RootID, "still out")

	if err := f.store.SetArchived(ctx, f.alice.ID, n.ID, true); err != nil {
		t.Fatal(err)
	}

	got, err := f.store.Archive(ctx, f.alice.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != n.ID {
		t.Fatalf("Archive = %+v, want only %d", got, n.ID)
	}
}

// TestArchiveListsOnlyTheRootOfAnArchivedSubtree is spec §13: "archived
// nodes whose parent is not itself archived — the roots of what was put
// away." Archiving a parent does not write archived_at onto its child
// (Task 1), so the child would independently qualify as "archived" only if
// it were archived in its own right; here it never is, so it must not
// appear as a second entry alongside its already-archived parent.
func TestArchiveListsOnlyTheRootOfAnArchivedSubtree(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	parent := f.mk(t, notes.RootID, "put away")
	f.mk(t, parent.ID, "child")

	if err := f.store.SetArchived(ctx, f.alice.ID, parent.ID, true); err != nil {
		t.Fatal(err)
	}

	got, err := f.store.Archive(ctx, f.alice.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != parent.ID {
		t.Fatalf("Archive = %+v, want only the parent %d", got, parent.ID)
	}
}

// TestArchiveListsANestedNodeWhoseParentIsNotArchived: only the node
// actually marked archived_at is a "root of what was put away" — an
// ancestor further up not being archived is exactly what makes it one.
func TestArchiveListsANestedNodeWhoseParentIsNotArchived(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	parent := f.mk(t, notes.RootID, "not archived")
	child := f.mk(t, parent.ID, "put away")

	if err := f.store.SetArchived(ctx, f.alice.ID, child.ID, true); err != nil {
		t.Fatal(err)
	}

	got, err := f.store.Archive(ctx, f.alice.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != child.ID {
		t.Fatalf("Archive = %+v, want only the child %d", got, child.ID)
	}
}

func TestArchiveDoesNotLeakAnotherUsersNodes(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	bobs := f.mkFor(t, f.bob.ID, notes.RootID, "bob's")
	if err := f.store.SetArchived(ctx, f.bob.ID, bobs.ID, true); err != nil {
		t.Fatal(err)
	}

	got, err := f.store.Archive(ctx, f.alice.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("alice's Archive = %+v, want none", got)
	}
}

func TestArchiveWithNothingArchivedIsEmpty(t *testing.T) {
	f := newFixture(t)
	f.mk(t, notes.RootID, "never archived")

	got, err := f.store.Archive(context.Background(), f.alice.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("Archive = %+v, want none", got)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/apps/notes/... -run TestArchive -v`
Expected: FAIL — `f.store.Archive` does not exist yet.

- [ ] **Step 3: Implement `Store.Archive`**

Create `internal/apps/notes/archive.go`:

```go
package notes

import (
	"context"
	"fmt"
)

// ArchiveRow is one entry on /notes/archive: an archived subtree's root,
// plus its ancestor breadcrumb — the same shape Search and Due rows take,
// for the same reason: a node found outside the context of the tree it
// lives in needs its path spelled out to be legible on its own.
type ArchiveRow struct {
	Node
	Crumbs []Node
}

// archiveView is what /notes/archive renders.
type archiveView struct {
	Rows []ArchiveRow
	// CSRFToken is needed here, unlike searchView and DueGroups, because
	// this page's rows carry a real mutating form — Restore — and every
	// other one is read-only.
	CSRFToken string
}

// Archive returns userID's archived nodes whose own parent is not itself
// archived — spec §13's "the roots of what was put away". A node nested
// under an already-archived ancestor is not listed here in its own right
// even if it happens to carry its own archived_at: restoring the ancestor
// is what brings the whole subtree back, this node included, so it has no
// restore action of its own to be listed for.
//
// The left join tests the parent's own archived_at without excluding a
// top-level node, whose parent_id is NULL and so has no parent row to join
// to at all: p.archived_at is then NULL too, and SQL's "NULL IS NULL" is
// true, which is exactly the "no parent, or a parent that is not archived"
// this needs — a NULL parent_id cannot be mistaken for an archived one.
//
// Ordered by archived_at, most recent first: the spec sets no ordering
// requirement, and "what did I put away most recently" is the natural way
// to scan a list like this one.
func (st *Store) Archive(ctx context.Context, userID int64) ([]Node, error) {
	rows, err := st.db.QueryContext(ctx,
		`SELECT `+aliasNodeColumns("n")+`
		   FROM notes_nodes n
		   LEFT JOIN notes_nodes p ON p.id = n.parent_id AND p.user_id = n.user_id
		  WHERE n.user_id = ? AND n.archived_at IS NOT NULL
		    AND p.archived_at IS NULL
		  ORDER BY n.archived_at DESC`, userID)
	if err != nil {
		return nil, fmt.Errorf("notes: archive: %w", err)
	}
	return collectNodes(rows, "archived nodes")
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/apps/notes/... -run TestArchive -v`
Expected: PASS.

- [ ] **Step 5: Write the failing handler tests**

Append to `internal/apps/notes/handlers_test.go`, near the `Done`/`Due`
handler tests:

```go
func TestArchiveHidesTheBulletAndRedirectsToTheOutline(t *testing.T) {
	s := newServer(t)
	id := s.seed(t, s.alice, notes.RootID, "put away")

	s.submit(t, s.alice, "/notes/"+itoa(id)+"/archive", url.Values{
		"root": {"0"}, "archived": {"1"},
	}, "/notes/")

	n, err := s.store.ByID(context.Background(), s.alice.user.ID, id)
	if err != nil {
		t.Fatal(err)
	}
	if !n.Archived {
		t.Fatal("the bullet is not archived")
	}

	// htmlassert only matches one qualifier per selector part (see its own
	// doc comment), so ".outline-row[data-id=...]" is not a valid selector
	// here — this walks the (unqualified-by-id) rows instead and checks
	// each one's own data-id attribute.
	doc := s.get(t, s.alice, "/notes/")
	for _, row := range doc.QueryAll(".outline-row") {
		if got, _ := htmlassert.Attr(row, "data-id"); got == itoa(id) {
			t.Fatal("the archived bullet still appears in the outline")
		}
	}
}

func TestArchiveRejectsAnUnknownValue(t *testing.T) {
	s := newServer(t)
	id := s.seed(t, s.alice, notes.RootID, "a")

	for _, v := range []string{"", "true", "yes", "2"} {
		rec := s.post(t, s.alice, "/notes/"+itoa(id)+"/archive", url.Values{
			"root": {"0"}, "archived": {v},
		})
		if rec.Code != http.StatusBadRequest {
			t.Errorf("archived=%q gave %d, want 400", v, rec.Code)
		}
	}
}

func TestArchiveOnAnotherUsersBulletIs404(t *testing.T) {
	s := newServer(t)
	id := s.seed(t, s.bob, notes.RootID, "bob's")

	rec := s.post(t, s.alice, "/notes/"+itoa(id)+"/archive", url.Values{
		"root": {"0"}, "archived": {"1"},
	})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

// TestRestoreBringsTheBulletBackAndRedirectsToTheArchivePage: the field's
// own value (archived=0) is what routes this request to restore rather
// than mutate — see archive's design note in this plan.
func TestRestoreBringsTheBulletBackAndRedirectsToTheArchivePage(t *testing.T) {
	s := newServer(t)
	id := s.seed(t, s.alice, notes.RootID, "put away")
	if err := s.store.SetArchived(context.Background(), s.alice.user.ID, id, true); err != nil {
		t.Fatal(err)
	}

	rec := s.post(t, s.alice, "/notes/"+itoa(id)+"/archive", url.Values{"archived": {"0"}})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303; body: %s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Location"); got != "/notes/archive" {
		t.Fatalf("redirected to %q, want /notes/archive", got)
	}

	n, err := s.store.ByID(context.Background(), s.alice.user.ID, id)
	if err != nil {
		t.Fatal(err)
	}
	if n.Archived {
		t.Fatal("the bullet is still archived")
	}
}

func TestRestoreOnAnotherUsersBulletIs404(t *testing.T) {
	s := newServer(t)
	id := s.seed(t, s.bob, notes.RootID, "bob's")
	if err := s.store.SetArchived(context.Background(), s.bob.user.ID, id, true); err != nil {
		t.Fatal(err)
	}

	rec := s.post(t, s.alice, "/notes/"+itoa(id)+"/archive", url.Values{"archived": {"0"}})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestRestoreRespondsWithTheArchiveListFragmentForHTMX(t *testing.T) {
	s := newServer(t)
	id := s.seed(t, s.alice, notes.RootID, "put away")
	if err := s.store.SetArchived(context.Background(), s.alice.user.ID, id, true); err != nil {
		t.Fatal(err)
	}

	rec := s.postHX(t, s.alice, "/notes/"+itoa(id)+"/archive", url.Values{"archived": {"0"}})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "<html") {
		t.Error("an HTMX response carries whole-document chrome")
	}
	if strings.Contains(rec.Body.String(), "put away") {
		t.Error("the restored bullet still appears in the archive list fragment")
	}
}

func TestArchiveListShowsArchivedNodesWithCrumbs(t *testing.T) {
	s := newServer(t)
	ctx := context.Background()
	parent := s.seed(t, s.alice, notes.RootID, "Projects")
	child := s.seed(t, s.alice, parent, "old project")
	if err := s.store.SetArchived(ctx, s.alice.user.ID, child, true); err != nil {
		t.Fatal(err)
	}

	doc := s.get(t, s.alice, "/notes/archive")
	// htmlassert only matches one qualifier per selector part, so the class
	// and the href check are two steps rather than one compound selector.
	title := doc.MustHave("a.notes-archive-title")
	if got, _ := htmlassert.Attr(title, "href"); got != "/notes/"+itoa(child) {
		t.Errorf("archive title href = %q, want /notes/%s", got, itoa(child))
	}
	crumbs := doc.MustHave(".notes-archive-crumbs")
	if !strings.Contains(htmlassert.Text(crumbs), "Projects") {
		t.Errorf("crumbs = %q, want it to mention Projects", htmlassert.Text(crumbs))
	}
}

func TestArchiveListWithNothingArchived(t *testing.T) {
	s := newServer(t)
	s.seed(t, s.alice, notes.RootID, "never archived")

	doc := s.get(t, s.alice, "/notes/archive")
	doc.MustHave("body") // page renders at all
	if strings.Contains(htmlassert.Text(doc.MustHave(".notes")), "never archived") {
		t.Error("an unarchived bullet appears on the archive page")
	}
}

func TestArchiveListRequiresSignIn(t *testing.T) {
	s := newServer(t)
	rec := s.do(t, nil, httptest.NewRequest("GET", "/notes/archive", nil))
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("GET /notes/archive anonymous = %d, want a 303 to the login page", rec.Code)
	}
}
```

Also extend `TestEveryMutationRequiresSignIn`'s `paths` slice — it
currently reads:

```go
	paths := []string{
		"/notes/new",
		"/notes/" + itoa(id) + "/text",
		"/notes/" + itoa(id) + "/indent",
		"/notes/" + itoa(id) + "/outdent",
		"/notes/" + itoa(id) + "/move",
		"/notes/" + itoa(id) + "/collapse",
		"/notes/" + itoa(id) + "/delete",
	}
```

Add the new route:

```go
	paths := []string{
		"/notes/new",
		"/notes/" + itoa(id) + "/text",
		"/notes/" + itoa(id) + "/indent",
		"/notes/" + itoa(id) + "/outdent",
		"/notes/" + itoa(id) + "/move",
		"/notes/" + itoa(id) + "/collapse",
		"/notes/" + itoa(id) + "/delete",
		"/notes/" + itoa(id) + "/archive",
	}
```

- [ ] **Step 6: Run the tests to verify they fail**

Run: `go test ./internal/apps/notes/... -run 'TestArchive|TestRestore|TestEveryMutationRequiresSignIn' -v`
Expected: FAIL — `a.archive`, `a.archiveList` and the `/notes/archive` route
do not exist yet. `TestArchiveListShowsArchivedNodesWithCrumbs` also needs
`templates/archive.html`, which does not exist until Task 4 — that
specific test is expected to keep failing until then; every other test in
this step should pass once this task's handler and route code lands. Note
that in the log and move on; Task 4 closes it.

- [ ] **Step 7: Implement the handlers**

In `internal/apps/notes/handlers.go`, immediately after `dueList` (which
ends `a.render(w, r, http.StatusOK, "notes/due", page)` followed by `}`)
and before `search`, insert:

```go
// archiveList renders every one of the user's archived subtree roots —
// spec §13.
func (a *App) archiveList(w http.ResponseWriter, r *http.Request) {
	userID, ok := a.userID(w, r)
	if !ok {
		return
	}
	view, err := a.buildArchiveView(r.Context(), userID)
	if err != nil {
		a.deps.Errors.Internal(w, r, err)
		return
	}
	view.CSRFToken = web.CSRFToken(r.Context())
	page := a.deps.Page(r, "Archive")
	page.Data = view
	a.render(w, r, http.StatusOK, "notes/archive", page)
}

// renderArchiveFragment re-renders /notes/archive's own list for an HTMX
// restore — the equivalent of renderOutlineFragment, but targeting this
// page's own swap target instead of #outline.
func (a *App) renderArchiveFragment(w http.ResponseWriter, r *http.Request, userID int64) {
	view, err := a.buildArchiveView(r.Context(), userID)
	if err != nil {
		a.deps.Errors.Internal(w, r, err)
		return
	}
	view.CSRFToken = web.CSRFToken(r.Context())
	if err := a.deps.Render.Fragment(w, http.StatusOK, "notes/archive", "archive-list", view); err != nil {
		a.deps.Errors.Internal(w, r, err)
	}
}

// buildArchiveView is the query behind both of the above: the full page and
// the HTMX fragment render exactly the same rows, so they share the one
// place that fetches them.
func (a *App) buildArchiveView(ctx context.Context, userID int64) (archiveView, error) {
	nodes, err := a.store.Archive(ctx, userID)
	if err != nil {
		return archiveView{}, err
	}
	rows := make([]ArchiveRow, len(nodes))
	for i, n := range nodes {
		crumbs, err := a.store.Ancestors(ctx, userID, n.ID)
		if err != nil {
			return archiveView{}, err
		}
		rows[i] = ArchiveRow{Node: n, Crumbs: crumbs}
	}
	return archiveView{Rows: rows}, nil
}

// archive marks a bullet archived, or restores it — spec §13. The field
// names the state to arrive at, exactly like done's and collapsed's, so a
// double submit or a stale page cannot flip it back.
//
// Archiving happens from a row in the outline (Task 4's new menu item),
// which may carry unsaved text in its title or note, so it goes through
// mutate exactly like every other structural op. Restoring happens from a
// row on /notes/archive, which is never mid-edit — there is no outline row
// to save text from, and the response that page needs back is its own
// list, not the outline — so it forks to restore instead. See this task's
// design note.
func (a *App) archive(w http.ResponseWriter, r *http.Request) {
	raw := r.PostFormValue("archived")
	if raw != "0" && raw != "1" {
		a.deps.Errors.Status(w, r, http.StatusBadRequest)
		return
	}
	if raw == "0" {
		a.restore(w, r)
		return
	}

	a.mutate(w, r, func(ctx context.Context, o *Ops, m mutation) error {
		return o.SetArchived(ctx, m.UserID, m.NodeID, true)
	})
}

// restore un-archives a bullet from its row on /notes/archive. See archive.
func (a *App) restore(w http.ResponseWriter, r *http.Request) {
	userID, ok := a.userID(w, r)
	if !ok {
		return
	}
	id, ok := a.nodeID(w, r)
	if !ok {
		return
	}
	if err := a.store.SetArchived(r.Context(), userID, id, false); err != nil {
		a.fail(w, r, err)
		return
	}
	if web.IsHTMX(r) {
		a.renderArchiveFragment(w, r, userID)
		return
	}
	http.Redirect(w, r, "/notes/archive", http.StatusSeeOther)
}
```

- [ ] **Step 8: Register the routes**

In `internal/apps/notes/app.go`, the route-map comment currently reads (in
part):

```go
//	GET  /notes/due          likewise a literal segment; N5's due-date list
//	GET  /notes/search       likewise a literal segment; N6's search
```

Add a line after it:

```go
//	GET  /notes/due          likewise a literal segment; N5's due-date list
//	GET  /notes/search       likewise a literal segment; N6's search
//	GET  /notes/archive      likewise a literal segment; N7's archive list
```

`Mount` currently ends:

```go
	r.HandleFunc("POST /{id}/done", a.done)
	r.HandleFunc("POST /{id}/due", a.due)
}
```

Change it to:

```go
	r.HandleFunc("POST /{id}/done", a.done)
	r.HandleFunc("POST /{id}/due", a.due)
	r.HandleFunc("POST /{id}/archive", a.archive)
}
```

And immediately above that block, `Mount` has:

```go
	r.HandleFunc("GET /due", a.dueList)
	r.HandleFunc("GET /search", a.search)
```

Change it to:

```go
	r.HandleFunc("GET /due", a.dueList)
	r.HandleFunc("GET /search", a.search)
	r.HandleFunc("GET /archive", a.archiveList)
```

- [ ] **Step 9: Run the tests**

Run: `go test ./internal/apps/notes/... -v`
Expected: every test passes except `TestArchiveListShowsArchivedNodesWithCrumbs`
and `TestArchiveListWithNothingArchived`, which fail because
`templates/archive.html` does not exist yet — confirm the failure is
specifically a template-lookup error, not a panic or a wrong status code,
then proceed to Task 4.

- [ ] **Step 10: Commit**

```bash
git add internal/apps/notes/archive.go internal/apps/notes/archive_test.go \
        internal/apps/notes/handlers.go internal/apps/notes/app.go \
        internal/apps/notes/handlers_test.go
git commit -m "feat(notes): add archive and restore routes"
```

---

### Task 4: `archive.html`, the outline's Archive action, and cross-page links

**Files:**
- Create: `internal/apps/notes/templates/archive.html`
- Modify: `internal/apps/notes/templates/outline.html`
- Modify: `internal/apps/notes/templates/due.html`
- Modify: `internal/apps/notes/templates/search.html`
- Modify: `internal/ui/static/app.css`
- Test: `internal/apps/notes/handlers_test.go`

**Interfaces:**
- Consumes: `archiveView`/`ArchiveRow` (Task 3), the existing
  `outline-menu-list` structure and `.notes-toolbar`/`.toolbar-btn`/
  `.notes-toolbar-actions` classes (`outline.html`, `app.css`).
- Produces: the `"archive-list"` named template block that
  `renderArchiveFragment` (Task 3) renders.

- [ ] **Step 1: Write `templates/archive.html`**

Create `internal/apps/notes/templates/archive.html`:

```html
{{define "content"}}
<div class="stack notes">
	<div class="notes-toolbar">
		<form method="get" action="/notes/search" class="notes-search">
			<input type="search" id="notes-search-input" name="q"
			       placeholder="Search…" aria-label="Search notes">
		</form>
		<div class="notes-toolbar-actions">
			<a href="/notes/due" class="toolbar-btn toolbar-btn-nav">Due</a>
			<a href="/notes/" class="toolbar-btn toolbar-btn-nav">All notes</a>
		</div>
	</div>
	<h1>Archive</h1>
	{{/* #archive-list is restore's own swap target, the equivalent of
	     #outline: its content is a named block so an HTMX restore can
	     re-render just this list through Renderer.Fragment. */}}
	<div id="archive-list">
		{{template "archive-list" .Data}}
	</div>
</div>
{{end}}

{{define "archive-list"}}
{{if .Rows}}
<ul class="notes-archive-list">
	{{range .Rows}}
	<li class="notes-archive-item">
		{{if .Crumbs}}
		<span class="notes-archive-crumbs">
			{{range .Crumbs}}<span class="notes-crumb-item" title="{{.DisplayTitle}}">{{.DisplayTitle}}</span> / {{end}}
		</span>
		{{end}}
		<a href="/notes/{{.ID}}" class="notes-archive-title">{{.DisplayTitle}}</a>
		<form method="post" action="/notes/{{.ID}}/archive"
		      hx-post="/notes/{{.ID}}/archive" hx-target="#archive-list" hx-swap="innerHTML">
			<input type="hidden" name="csrf_token" value="{{$.CSRFToken}}">
			<button type="submit" name="archived" value="0" class="toolbar-btn notes-archive-restore">Restore</button>
		</form>
	</li>
	{{end}}
</ul>
{{else}}
<p class="dim">Nothing is archived.</p>
{{end}}
{{end}}
```

- [ ] **Step 2: Add the "Archive" row action to the outline**

In `internal/apps/notes/templates/outline.html`, the row's `"···"` menu
currently has this button, right before the delete button:

```html
						<button type="submit" class="quiet" formaction="/notes/new"
						        hx-post="/notes/new" hx-target="#outline" hx-swap="innerHTML"
						        aria-label="Add a bullet below">
							<span class="outline-menu-label">+ Add bullet</span>
							<span class="outline-menu-shortcut">Enter</span>
						</button>

						<button type="submit" class="quiet outline-menu-delete" formaction="/notes/{{.ID}}/delete"
						        hx-post="/notes/{{.ID}}/delete" hx-target="#outline" hx-swap="innerHTML"
						        hx-confirm="Delete &#8220;{{.DisplayTitle}}&#8221; and everything under it? This cannot be undone."
						        aria-label="Delete">
							<span class="outline-menu-label">&times; Delete</span>
						</button>
```

Insert an "Archive" button between them:

```html
						<button type="submit" class="quiet" formaction="/notes/new"
						        hx-post="/notes/new" hx-target="#outline" hx-swap="innerHTML"
						        aria-label="Add a bullet below">
							<span class="outline-menu-label">+ Add bullet</span>
							<span class="outline-menu-shortcut">Enter</span>
						</button>

						<button type="submit" class="quiet" formaction="/notes/{{.ID}}/archive"
						        hx-post="/notes/{{.ID}}/archive" hx-target="#outline" hx-swap="innerHTML"
						        name="archived" value="1"
						        aria-label="Archive">
							<span class="outline-menu-label">Archive</span>
						</button>

						<button type="submit" class="quiet outline-menu-delete" formaction="/notes/{{.ID}}/delete"
						        hx-post="/notes/{{.ID}}/delete" hx-target="#outline" hx-swap="innerHTML"
						        hx-confirm="Delete &#8220;{{.DisplayTitle}}&#8221; and everything under it? This cannot be undone."
						        aria-label="Delete">
							<span class="outline-menu-label">&times; Delete</span>
						</button>
```

Archiving is reversible (via `/notes/archive`'s Restore button), unlike
deleting, so it gets no `hx-confirm`.

- [ ] **Step 3: Add "Archive" to the outline's own toolbar**

`outline.html`'s toolbar currently reads:

```html
			<div class="notes-toolbar-actions">
				<a href="/notes/due" class="toolbar-btn toolbar-btn-nav">Due</a>
```

Change it to:

```html
			<div class="notes-toolbar-actions">
				<a href="/notes/due" class="toolbar-btn toolbar-btn-nav">Due</a>
				<a href="/notes/archive" class="toolbar-btn toolbar-btn-nav">Archive</a>
```

- [ ] **Step 4: Add "Archive" to the due page's toolbar**

`due.html`'s toolbar currently reads:

```html
		<div class="notes-toolbar">
			<form method="get" action="/notes/search" class="notes-search">
				<input type="search" id="notes-search-input" name="q"
				       placeholder="Search…" aria-label="Search notes">
			</form>
			<a href="/notes/">All notes</a>
		</div>
```

Change it to:

```html
		<div class="notes-toolbar">
			<form method="get" action="/notes/search" class="notes-search">
				<input type="search" id="notes-search-input" name="q"
				       placeholder="Search…" aria-label="Search notes">
			</form>
			<div class="notes-toolbar-actions">
				<a href="/notes/archive" class="toolbar-btn toolbar-btn-nav">Archive</a>
				<a href="/notes/" class="toolbar-btn toolbar-btn-nav">All notes</a>
			</div>
		</div>
```

The bare `<a href="/notes/">All notes</a>` link is deliberately promoted to
`.toolbar-btn.toolbar-btn-nav` here, matching how `archive.html` and
`outline.html` already style their own nav links — see this task's
self-review note on why `search.html`'s single bare link is left as-is.

- [ ] **Step 5: Add "Archive" to the search page's toolbar**

`search.html`'s toolbar currently reads:

```html
	<div class="notes-toolbar">
		<form method="get" action="/notes/search" class="notes-search">
			<input type="search" id="notes-search-input" name="q" value="{{.Data.Query}}"
			       placeholder="Search…" aria-label="Search notes" autofocus>
		</form>
		<a href="/notes/">All notes</a>
	</div>
```

Change it to:

```html
	<div class="notes-toolbar">
		<form method="get" action="/notes/search" class="notes-search">
			<input type="search" id="notes-search-input" name="q" value="{{.Data.Query}}"
			       placeholder="Search…" aria-label="Search notes" autofocus>
		</form>
		<div class="notes-toolbar-actions">
			<a href="/notes/archive" class="toolbar-btn toolbar-btn-nav">Archive</a>
			<a href="/notes/">All notes</a>
		</div>
	</div>
```

- [ ] **Step 6: Add the CSS**

In `internal/ui/static/app.css`, the combined crumb rule currently reads:

```css
.notes-due-crumbs,
.notes-search-crumbs { font-size: var(--fs-sm); color: var(--c-text-faint); }
```

Change it to:

```css
.notes-due-crumbs,
.notes-search-crumbs,
.notes-archive-crumbs { font-size: var(--fs-sm); color: var(--c-text-faint); }
```

Immediately after the existing block:

```css
.notes-search-list { margin: 0; padding: 0; list-style: none; }
.notes-search-item {
	display: flex;
	align-items: baseline;
	gap: var(--s-2);
	padding: var(--s-1) 0;
	border-bottom: 1px solid var(--c-border);
}
.notes-search-title { flex: 1; min-width: 0; color: var(--c-text); text-decoration: none; }
.notes-search-title:hover { color: var(--c-accent); }
```

Add:

```css
.notes-archive-list { margin: 0; padding: 0; list-style: none; }
.notes-archive-item {
	display: flex;
	align-items: baseline;
	gap: var(--s-2);
	padding: var(--s-1) 0;
	border-bottom: 1px solid var(--c-border);
}
.notes-archive-title { flex: 1; min-width: 0; color: var(--c-text); text-decoration: none; }
.notes-archive-title:hover { color: var(--c-accent); }
.notes-archive-restore { flex-shrink: 0; }
```

- [ ] **Step 7: Write the remaining failing tests**

Append to `internal/apps/notes/handlers_test.go`:

```go
// TestOutlineMenuHasAnArchiveAction extends the existing comprehensive-menu
// check (TestOutlineMenuHoldsEveryAction) with this task's new action.
func TestOutlineMenuHasAnArchiveAction(t *testing.T) {
	s := newServer(t)
	id := s.seed(t, s.alice, notes.RootID, "Projects")

	doc := s.get(t, s.alice, "/notes/")
	menu := doc.MustHave(".outline-menu")
	list := doc.MustHave(".outline-menu-list")

	found := false
	for _, n := range doc.QueryAll(`button[formaction=/notes/` + itoa(id) + `/archive]`) {
		if within(n, list) && within(n, menu) {
			found = true
		}
	}
	if !found {
		t.Error("the menu is missing an Archive action")
	}
}

func TestArchiveToolbarHasASearchBox(t *testing.T) {
	s := newServer(t)
	s.get(t, s.alice, "/notes/archive").MustHave("#notes-search-input")
}

func TestOutlineToolbarHasAnArchiveLink(t *testing.T) {
	s := newServer(t)
	doc := s.get(t, s.alice, "/notes/")
	link := doc.MustHave(`a[href="/notes/archive"]`)
	if htmlassert.Text(link) != "Archive" {
		t.Errorf("archive link text = %q, want Archive", htmlassert.Text(link))
	}
}
```

- [ ] **Step 8: Run every test**

Run: `go test ./internal/apps/notes/... -v`
Expected: PASS — including the two tests from Task 3
(`TestArchiveListShowsArchivedNodesWithCrumbs`,
`TestArchiveListWithNothingArchived`) that were left failing at the end of
that task.

- [ ] **Step 9: Static checks and a manual pass**

Run:

```bash
go build ./... && go vet ./...
go run honnef.co/go/tools/cmd/staticcheck@v0.8.0 ./...
```

(`staticcheck` on `$PATH` in this environment silently no-ops; always
invoke it through `go run` as above.)

Then start the app (`go run ./cmd/onsuite serve` or this repo's usual dev
command) and by hand: archive a bullet from the outline's `···` menu,
confirm it disappears from the outline and from search results for text it
contained; visit `/notes/archive`, confirm it is listed with its
breadcrumb; click Restore, confirm it reappears in the outline; repeat the
whole cycle with JavaScript disabled in the browser, confirming every step
still works via full-page navigation.

- [ ] **Step 10: Commit**

```bash
git add internal/apps/notes/templates/archive.html \
        internal/apps/notes/templates/outline.html \
        internal/apps/notes/templates/due.html \
        internal/apps/notes/templates/search.html \
        internal/ui/static/app.css internal/apps/notes/handlers_test.go
git commit -m "feat(notes): add the archive page and reach it from every view"
```

---

### Task 5: Docs

**Files:**
- Modify: `README.md`
- Modify: `internal/apps/notes/notes.go`

**Interfaces:** None — this task changes only prose.

- [ ] **Step 1: Update the package doc comment**

`internal/apps/notes/notes.go` currently opens:

```go
// Package notes implements ON Notes, a hierarchical outliner: one infinite
// tree per user, where every node is a bullet with a title, an optional
// secondary note, and children.
//
// It depends only on internal/platform/*. It never imports another app, and no
// platform package imports it: the whole coupling is the app.App interface plus
// one line in cmd/onsuite/main.go.
//
// The design is in docs/superpowers/specs/2026-08-25-on-notes-design.md. This
// package spans chunk N1 (schema and store) through N6 (search): app.App,
// routes, templates, handlers, the keyboard layer in static/notes.js, the
// inline Markdown renderer in markdown.go, task tracking in tree.go,
// prefs.go and due.go, and full-text search in search.go.
package notes
```

Change the last paragraph to:

```go
// The design is in docs/superpowers/specs/2026-08-25-on-notes-design.md. This
// package spans chunk N1 (schema and store) through N7 (archiving): app.App,
// routes, templates, handlers, the keyboard layer in static/notes.js, the
// inline Markdown renderer in markdown.go, task tracking in tree.go,
// prefs.go and due.go, full-text search in search.go, and archiving in
// archive.go.
package notes
```

- [ ] **Step 2: Update `README.md`**

`README.md` currently reads:

```
Work since then is per-app rather than per-phase. ON Notes is being built in
ten small chunks under
[`docs/superpowers/specs/2026-08-25-on-notes-design.md`](docs/superpowers/specs/2026-08-25-on-notes-design.md);
N1 (schema and store), N2 (the outline), N3 (the keyboard layer), N4
(Markdown), N5 (done + due dates) and N6 (search and tags) are done.
```

Change it to:

```
Work since then is per-app rather than per-phase. ON Notes is being built in
ten small chunks under
[`docs/superpowers/specs/2026-08-25-on-notes-design.md`](docs/superpowers/specs/2026-08-25-on-notes-design.md);
N1 (schema and store), N2 (the outline), N3 (the keyboard layer), N4
(Markdown), N5 (done + due dates), N6 (search and tags) and N7 (archiving)
are done.
```

- [ ] **Step 3: Run the full test suite once more**

Run: `go build ./... && go vet ./... && go test ./... -race`
Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git add README.md internal/apps/notes/notes.go
git commit -m "docs(notes): note N7 (archiving) as done"
```

---

## Self-Review

**Spec coverage.** §13 in full: `archived_at` on the node (Task 1); an
archived node and its subtree disappear from the outline (Task 2), from
search results (Task 2, §12), and — via §11's own sentence — from
`/notes/due` (Task 2); `/notes/archive` lists the roots of what was put
away, with a restore action (Task 3, Task 4); archived is independent of
done (Task 1's `TestSetArchivedIsIndependentOfDone`, and never touching
`done_at` anywhere in this plan). §9's two new routes are both registered
(Task 3). Share pages (§15) are explicitly out of scope — N9 has not
landed, so there is nothing yet to exclude archived nodes from.

**Placeholder scan.** No TBDs; every step carries complete code, exact
before/after text for every edit to an existing file, and exact commands
with expected results.

**Type consistency.** `Node.Archived` (Task 1) is read by `Store.Outline`
only implicitly, through the SQL predicate — the Go struct field itself is
what `ArchiveRow`, the handler tests, and `TestSetArchivedIsIndependentOfDone`
read directly, and its name and type are used identically everywhere.
`archivedBelowCTE` (Task 2) is referenced by name from both `search.go` and
`due.go`, unchanged. `ArchiveRow`/`archiveView` (Task 3) match the shape
`archive.html` (Task 4) ranges over (`.Rows`, `.Crumbs`, `.CSRFToken`) and
the shape `DueRow`/`SearchRow` already established.

**Why Outline filters in SQL but Search/Due filter via a shared CTE.**
Outline already performs a recursive descent from a root; refusing to
step into an archived node is one extra predicate on a join it already
does, and correctly excludes every descendant for free, the same way a
collapsed node's children already never appear. Search and Due are flat
queries with no "root" to descend from — a match can be an arbitrarily
deep node with no archived ancestor visited along the way — so they need
an explicit answer to "is this id under an archived node", which
`archivedBelowCTE` computes once as a reusable set. Both approaches
produce the same visible result; they differ because the two kinds of
query are shaped differently, not because of an inconsistency.

**Why `archive` forks into `restore` rather than being a second route.**
Spec §9 lists exactly one route, `POST /notes/{id}/archive`, for both
directions — mirroring how `done`'s own `done=0`/`done=1` already serves
both "complete" and "un-complete" through one route. The two directions
differ in the response the two different pages need back (`#outline`'s
fragment/redirect vs. `#archive-list`'s), so the fork happens inside the
one handler rather than being expressed as a second route the spec does
not list. This is a judgment call about internal handler shape, not an
extension of the HTTP surface — flagged here in case a reviewer expects a
single, undivided `archive` function the way `done`'s is.

**Why `/notes/archive` gets no keyboard shortcut.** Spec §8's table lists
no binding for archiving, and each existing binding "lands with the chunk
that adds the operation behind it" only when the spec calls for one (the
table entry itself is the authority, not a general expectation that every
new action gets a key). Reachable via the row menu and the toolbar link
instead, consistently with how N5's due-date editing (a due-date input
inside the same menu) also has no dedicated key of its own.

**Why `due.html`'s bare "All notes" link is promoted to `.toolbar-btn` here
but `search.html`'s is left alone.** `due.html` needs a
`.notes-toolbar-actions` wrapper regardless, to hold both the new Archive
link and the existing All-notes link as a group — matching
`outline.html`'s own toolbar shape once it has two nav links side by side.
Giving both links the same `.toolbar-btn` treatment inside that new
wrapper is one extra class on a line already being edited, not a separate
styling decision. `search.html` only needs one new link added beside its
existing bare one; leaving the old one as plain text avoids an edit to a
line Task 4 has no other reason to touch, and either presentation reads
fine for a single pair of text links. This is a coherence nit — a
reviewer noticing all three list pages now render this pair slightly
differently should treat it as no worse than the pre-existing state:
`outline.html` and `due.html`/`archive.html` already differ from
`search.html` in this exact respect before this plan, since `outline.html`
already wraps its own actions in the same way.

## Follow-ups

1. **Archive-list ordering.** `Store.Archive` orders by `archived_at DESC`
   (most recently archived first) — the spec sets no requirement, so this
   is a default that a later chunk or explicit user feedback might revise
   (e.g. an explicit sort control, once there is enough in a real archive
   to want one).
2. **A node under an archived ancestor that is separately marked
   `archived_at` in its own right** is possible (nothing in this plan
   prevents setting `archived_at` on a node whose parent already has it
   set) and is excluded from `/notes/archive`'s own listing by
   `Store.Archive`'s "parent not archived" predicate, exactly as spec §13
   describes — restoring the ancestor brings such a node back into the
   outline, but does not clear its own `archived_at`, so it would then
   need restoring again in its own right to stop being independently
   archived. This is an edge case no test in this plan exercises directly,
   since it requires archiving a node and then separately archiving one of
   its already-archived descendants — an action nothing in the UI offers
   (an archived node has no menu, being invisible in the outline) but
   which the store layer does not prevent. Worth a store-level test if it
   ever becomes reachable from the UI.
3. **Share pages (§15, N9)** will need the same subtree exclusion applied
   there once that chunk exists; this plan does not touch anything under
   N9's future scope.
