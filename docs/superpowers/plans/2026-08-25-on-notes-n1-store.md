# ON Notes N1 — Schema and Store Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build `internal/apps/notes` far enough to hold a user's outline — the
table, and a store that creates, reads, edits, moves and deletes bullets while
keeping the tree structurally sound. No HTTP, no templates, no app registration.

**Architecture:** One table, `notes_nodes`, as an adjacency list with a
contiguous integer `position` among siblings. All tree logic lives in the
store, each operation in one transaction. Reads are recursive CTEs; `Outline`
returns a **flat** slice carrying `Depth`, so nothing downstream has to recurse.
Four invariants (I1–I4) are asserted by tests after every operation, including
a long randomised sequence.

**Tech Stack:** Go 1.26, `database/sql` with `modernc.org/sqlite` (pure Go, no
CGO), SQLite recursive CTEs, `math/rand/v2` for the sequence test. No new
dependencies.

**Verified before writing:** every SQL statement in this plan — the schema, the
`Outline` and `Ancestors` recursive CTEs, `depthOf`, `heightOf`,
`isDescendant`, and the position arithmetic in `Create` and `Move` — was run
against `modernc.org/sqlite v1.56.0`, the version in `go.mod`. The pre-order
`path` ordering, the collapse cutoff, the cross-user isolation, the cycle and
depth guards, the indent/outdent/reorder traces and the `ON DELETE CASCADE`
subtree delete all produce the results asserted in the tests below. If one of
them fails during implementation, the bug is in the Go around the query, not
in the query.

**Spec:** [docs/superpowers/specs/2026-08-25-on-notes-design.md](../specs/2026-08-25-on-notes-design.md).
This plan implements chunk **N1** of §18. Read spec §4 (data model), §5 (tree
operations) and §17 (testing) before starting.

## Global Constraints

- `main` is protected. Work on a branch and open a PR — never commit or push to `main`.
- **No CGO, no new dependencies.** Everything here is stdlib plus `modernc.org/sqlite`, already in `go.mod`.
- **No app may import another app; no platform package may import an app.** Enforced by `internal/arch/arch_test.go`, which discovers packages automatically — nothing to register.
- Every table this app owns is prefixed `notes_`. Migrations are forward-only and live under `internal/apps/notes/migrations/`.
- Timestamps are RFC 3339 with nanoseconds, in UTC, stored as TEXT — the platform convention, and it sorts chronologically as text.
- Errors are wrapped with a `notes: ` prefix. Sentinel errors are compared with `errors.Is`, never by string.
- **`MaxDepth = 64` is the deepest permitted depth**, where a top-level bullet has depth 0.
- **`RootID = 0` is the sentinel for "top level"**, stored as `NULL` in `parent_id`. Node ids come from `AUTOINCREMENT` and start at 1, so 0 is never a real id.
- After every task, the full check must pass:

```bash
gofmt -l . && go vet ./... && go run honnef.co/go/tools/cmd/staticcheck@v0.8.0 ./... && go test ./... -race -count=1
```

## File Structure

| File | Responsibility |
|---|---|
| `internal/apps/notes/migrations/0001_nodes.sql` | The `notes_nodes` table and its one index. |
| `internal/apps/notes/notes.go` | Package doc, `ID`, constants, `Migrations()`, sentinel errors, the `Node` type, `Validate`. No SQL. |
| `internal/apps/notes/store.go` | `Store` and every **read**: `ByID`, `Children`, `Ancestors`, `Outline`, plus row scanning and time formatting. |
| `internal/apps/notes/tree.go` | Every **mutation**: `Create`, `SetText`, `SetCollapsed`, `Delete`, `Move`, `Indent`, `Outdent`, `MoveUp`, `MoveDown`, and the transaction and depth helpers they share. |
| `internal/apps/notes/notes_test.go` | `Validate` and migration tests. No database fixture. |
| `internal/apps/notes/invariants_test.go` | `treeViolations` — the I1–I4 checker — and its own corruption tests. |
| `internal/apps/notes/store_test.go` | The fixture, and tests for the read methods. |
| `internal/apps/notes/tree_test.go` | Tests for the mutations, and the randomised sequence test. |

Reads and writes are split because they have almost nothing in common: reads
are single recursive queries with no transaction, writes are multi-statement
transactions with invariant restoration. Keeping them apart means a change to
move semantics never has to be read alongside the outline query.

**Before Task 1:** branch off `main` (or off `docs/on-notes-spec` if the spec
PR has not merged yet):

```bash
git checkout -b feat/notes-n1-store
```

---

### Task 1: The table, the package skeleton, and validation

**Files:**
- Create: `internal/apps/notes/migrations/0001_nodes.sql`
- Create: `internal/apps/notes/notes.go`
- Test: `internal/apps/notes/notes_test.go`

**Interfaces:**
- Consumes: `db.Open`, `db.Collect`, `db.Apply` from `internal/platform/db`; `auth.Namespace`, `auth.Migrations` from `internal/platform/auth`.
- Produces: `notes.ID`, `notes.MaxDepth`, `notes.MaxTitleRunes`, `notes.MaxNoteRunes`, `notes.RootID`, `notes.Migrations() fs.FS`, `notes.ErrNotFound`, `notes.ErrInvalid`, `notes.ErrCycle`, `notes.ErrTooDeep`, `notes.Node`, `notes.Validate(title, note string) error`.

- [ ] **Step 1: Write the migration**

Create `internal/apps/notes/migrations/0001_nodes.sql`:

```sql
-- ON Notes owns exactly one table, prefixed with its app id so it cannot
-- collide with any other app in the single shared database.
--
-- done_at, due_on, archived_at and share_slug are deliberately absent. Each
-- arrives with the chunk that uses it, in its own forward-only migration, so
-- a chunk is a self-contained change rather than a schema that pretends to
-- know what later chunks will need.

CREATE TABLE notes_nodes (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id    INTEGER NOT NULL REFERENCES users (id)       ON DELETE CASCADE,
    -- NULL means top level. The self-reference deletes a subtree for free,
    -- because the platform opens SQLite with foreign_keys=ON.
    parent_id  INTEGER          REFERENCES notes_nodes (id) ON DELETE CASCADE,
    -- Contiguous within a parent: 0..n-1, no gaps. Invariant I1 in the spec.
    position   INTEGER NOT NULL,
    title      TEXT    NOT NULL,
    -- The secondary line under the bullet. "" when absent, never NULL.
    note       TEXT    NOT NULL,
    collapsed  INTEGER NOT NULL DEFAULT 0,
    created_at TEXT    NOT NULL,
    updated_at TEXT    NOT NULL
) STRICT;

-- Every read is some form of "this user's children of this parent, in order".
CREATE INDEX notes_nodes_user_parent_pos_idx
    ON notes_nodes (user_id, parent_id, position);
```

- [ ] **Step 2: Write the package skeleton**

Create `internal/apps/notes/notes.go`:

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
// package is chunk N1 of that spec: schema and store, with no HTTP.
package notes

import (
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"strings"
	"time"
	"unicode/utf8"
)

// ID is the app id: the URL prefix, the migration namespace, and the prefix on
// every table this app owns.
const ID = "notes"

const (
	// MaxDepth is the deepest permitted depth, counting a top-level bullet as
	// 0. It exists for two reasons: a tree that has somehow acquired a cycle
	// must make a recursive query terminate rather than run forever, and a
	// runaway import must not produce an outline no UI can render. 64 is far
	// past any outline written by hand.
	MaxDepth = 64
	// MaxTitleRunes bounds one bullet's title. A bullet is a line, not a
	// document; anything longer belongs in the note or in a child.
	MaxTitleRunes = 2000
	// MaxNoteRunes bounds the secondary note under a bullet.
	MaxNoteRunes = 10000
	// RootID is the ParentID meaning "top level". Node ids come from
	// AUTOINCREMENT and start at 1, so 0 is never a real id, and callers never
	// have to handle a nullable parent.
	RootID = 0
)

//go:embed migrations/*.sql
var migrationFiles embed.FS

// Migrations returns this app's schema with filenames at the root, which is
// what db.Collect expects.
func Migrations() fs.FS {
	sub, err := fs.Sub(migrationFiles, "migrations")
	if err != nil {
		// Unreachable: the path is a compile-time constant checked by go:embed.
		panic("notes: embedded migrations missing: " + err.Error())
	}
	return sub
}

var (
	// ErrNotFound covers both "no such node" and "not yours". They are
	// deliberately indistinguishable: a distinct error for someone else's
	// node would confirm that it exists.
	ErrNotFound = errors.New("notes: not found")
	// ErrInvalid is text that fails Validate.
	ErrInvalid = errors.New("notes: invalid note")
	// ErrCycle is a move that would place a bullet inside its own subtree.
	// The result is unreachable from any root: still in the table, gone from
	// the outline.
	ErrCycle = errors.New("notes: a note cannot be moved inside itself")
	// ErrTooDeep is a create or move that would pass MaxDepth.
	ErrTooDeep = errors.New("notes: the outline would nest too deeply")
)

// Node is one bullet.
type Node struct {
	ID     int64
	UserID int64
	// ParentID is RootID for a top-level bullet.
	ParentID  int64
	Position  int
	Title     string
	Note      string
	Collapsed bool
	CreatedAt time.Time
	UpdatedAt time.Time

	// Depth and HasChildren are filled in by Outline and are zero elsewhere.
	// Depth is relative to the outline's root: its direct children are 0.
	Depth       int
	HasChildren bool
}

// DisplayTitle is what to show for a bullet saved with no text. An empty
// bullet is legitimate — pressing Enter creates one — so this is a display
// concern, not a validation failure.
func (n Node) DisplayTitle() string {
	if strings.TrimSpace(n.Title) == "" {
		return "Untitled"
	}
	return n.Title
}

// Validate bounds a bullet's user-supplied text. Exported because the handler
// reports these messages back to the user.
//
// Unlike ON Paste, an empty title is valid: the first thing Enter does is
// create a bullet with nothing in it.
func Validate(title, note string) error {
	if !utf8.ValidString(title) {
		return fmt.Errorf("%w: the title is not valid UTF-8", ErrInvalid)
	}
	if !utf8.ValidString(note) {
		return fmt.Errorf("%w: the note is not valid UTF-8", ErrInvalid)
	}
	if utf8.RuneCountInString(title) > MaxTitleRunes {
		return fmt.Errorf("%w: the title is longer than %d characters", ErrInvalid, MaxTitleRunes)
	}
	if utf8.RuneCountInString(note) > MaxNoteRunes {
		return fmt.Errorf("%w: the note is longer than %d characters", ErrInvalid, MaxNoteRunes)
	}
	return nil
}
```

- [ ] **Step 3: Write the failing tests**

Create `internal/apps/notes/notes_test.go`:

```go
package notes_test

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/iliafrenkel/on-suite/internal/apps/notes"
	"github.com/iliafrenkel/on-suite/internal/platform/auth"
	"github.com/iliafrenkel/on-suite/internal/platform/db"
)

func TestValidateAcceptsAnEmptyBullet(t *testing.T) {
	if err := notes.Validate("", ""); err != nil {
		t.Fatalf(`Validate("", "") = %v; an empty bullet is what Enter creates`, err)
	}
}

func TestValidateRejectsBadText(t *testing.T) {
	tests := []struct{ name, title, note string }{
		{"title too long", strings.Repeat("a", notes.MaxTitleRunes+1), ""},
		{"note too long", "", strings.Repeat("a", notes.MaxNoteRunes+1)},
		{"title is not utf-8", "\xff\xfe", ""},
		{"note is not utf-8", "", "\xff\xfe"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := notes.Validate(tc.title, tc.note)
			if !errors.Is(err, notes.ErrInvalid) {
				t.Fatalf("Validate = %v; want ErrInvalid", err)
			}
		})
	}
}

func TestValidateCountsRunesNotBytes(t *testing.T) {
	// MaxTitleRunes Cyrillic characters is twice that many bytes and must
	// still be accepted: the bound is on what a person typed, not on how
	// UTF-8 happens to encode it.
	if err := notes.Validate(strings.Repeat("щ", notes.MaxTitleRunes), ""); err != nil {
		t.Fatalf("Validate(%d Cyrillic runes) = %v; want nil", notes.MaxTitleRunes, err)
	}
}

func TestMigrationsApply(t *testing.T) {
	ctx := context.Background()

	handle, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = handle.Close() })

	platform, err := db.Collect(auth.Namespace, auth.Migrations())
	if err != nil {
		t.Fatal(err)
	}
	appSchema, err := db.Collect(notes.ID, notes.Migrations())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Apply(ctx, handle, append(platform, appSchema...)); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	var name string
	err = handle.QueryRowContext(ctx,
		`SELECT name FROM sqlite_master WHERE type = 'table' AND name = 'notes_nodes'`).Scan(&name)
	if err != nil {
		t.Fatalf("notes_nodes was not created: %v", err)
	}
}
```

- [ ] **Step 4: Run the tests**

```bash
go test ./internal/apps/notes/... -count=1
```

Expected: PASS. (These tests are written after the code they exercise because
Task 1 is scaffolding — from Task 2 on, the test comes first.)

- [ ] **Step 5: Run the arch test and the full check**

```bash
go test ./internal/arch/... -count=1 && gofmt -l . && go vet ./... && go test ./... -race -count=1
```

Expected: PASS, and `gofmt -l .` prints nothing. The arch test discovers
`internal/apps/notes` automatically — there is nothing to register.

- [ ] **Step 6: Commit**

```bash
git add internal/apps/notes
git commit -m "feat(notes): add the notes_nodes schema and package skeleton"
```

---

### Task 2: The store, ByID, the fixture, and the invariant checker

**Files:**
- Create: `internal/apps/notes/store.go`
- Test: `internal/apps/notes/store_test.go`, `internal/apps/notes/invariants_test.go`

**Interfaces:**
- Consumes: `notes.Node`, `notes.ErrNotFound`, `notes.RootID`, `notes.MaxDepth` (Task 1).
- Produces: `notes.NewStore(*sql.DB) *notes.Store`, `(*Store).SetClock(func() time.Time)`, `(*Store).ByID(ctx, userID, id int64) (Node, error)`; package-private `nodeColumns`, `scanNode(rowScanner, ...any) (Node, error)`, `parseTime(string) (time.Time, error)`. In tests: `newFixture(*testing.T) *fixture` with fields `store`, `db`, `alice`, `bob`; `treeViolations([]rawNode) []string`; `checkInvariants(*testing.T, *sql.DB)`.

- [ ] **Step 1: Write the invariant checker and its failing tests**

The checker comes first because every later task asserts with it. It reads the
table directly rather than through the store, so a store bug cannot hide
inside it, and it is a pure function of the rows so it can be tested against
deliberately corrupt input — a checker that has never been seen to fail is not
evidence of anything.

Create `internal/apps/notes/invariants_test.go`:

```go
package notes_test

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"
	"testing"

	"github.com/iliafrenkel/on-suite/internal/apps/notes"
)

// rawNode is a row of notes_nodes as the checker sees it: structure only, no
// text.
type rawNode struct {
	ID       int64
	UserID   int64
	ParentID sql.NullInt64
	Position int
}

// loadRawNodes reads every row in the table, for every user.
func loadRawNodes(t *testing.T, handle *sql.DB) []rawNode {
	t.Helper()

	rows, err := handle.QueryContext(context.Background(),
		`SELECT id, user_id, parent_id, position FROM notes_nodes`)
	if err != nil {
		t.Fatalf("loading nodes: %v", err)
	}
	defer func() { _ = rows.Close() }()

	var out []rawNode
	for rows.Next() {
		var n rawNode
		if err := rows.Scan(&n.ID, &n.UserID, &n.ParentID, &n.Position); err != nil {
			t.Fatalf("scanning node: %v", err)
		}
		out = append(out, n)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("loading nodes: %v", err)
	}
	return out
}

// treeViolations reports every way nodes breaks invariants I1-I4 of the spec:
//
//	I1  the children of a parent occupy positions 0..n-1 exactly
//	I2  a node's parent exists and belongs to the same user
//	I3  no node is its own ancestor
//	I4  no node is deeper than MaxDepth
func treeViolations(nodes []rawNode) []string {
	byID := make(map[int64]rawNode, len(nodes))
	for _, n := range nodes {
		byID[n.ID] = n
	}
	var out []string

	// I2 first: the later checks assume parents resolve.
	for _, n := range nodes {
		if !n.ParentID.Valid {
			continue
		}
		p, ok := byID[n.ParentID.Int64]
		if !ok {
			out = append(out, fmt.Sprintf("I2: node %d has parent %d, which does not exist", n.ID, n.ParentID.Int64))
			continue
		}
		if p.UserID != n.UserID {
			out = append(out, fmt.Sprintf("I2: node %d (user %d) has parent %d (user %d)", n.ID, n.UserID, p.ID, p.UserID))
		}
	}

	// I1: positions are contiguous within each (user, parent) group.
	type group struct{ userID, parentID int64 }
	siblings := make(map[group][]int)
	for _, n := range nodes {
		g := group{userID: n.UserID, parentID: notes.RootID}
		if n.ParentID.Valid {
			g.parentID = n.ParentID.Int64
		}
		siblings[g] = append(siblings[g], n.Position)
	}
	keys := make([]group, 0, len(siblings))
	for g := range siblings {
		keys = append(keys, g)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].userID != keys[j].userID {
			return keys[i].userID < keys[j].userID
		}
		return keys[i].parentID < keys[j].parentID
	})
	for _, g := range keys {
		got := append([]int(nil), siblings[g]...)
		sort.Ints(got)
		for i, p := range got {
			if p != i {
				out = append(out, fmt.Sprintf("I1: user %d, parent %d has positions %v; want 0..%d",
					g.userID, g.parentID, got, len(got)-1))
				break
			}
		}
	}

	// I3 and I4: walking up from any node reaches the top without revisiting
	// a node, in at most MaxDepth steps.
	for _, n := range nodes {
		seen := map[int64]bool{n.ID: true}
		cur, depth := n, 0
		for cur.ParentID.Valid {
			next, ok := byID[cur.ParentID.Int64]
			if !ok {
				break // already reported as an I2 violation
			}
			if seen[next.ID] {
				out = append(out, fmt.Sprintf("I3: node %d is inside its own subtree", n.ID))
				break
			}
			seen[next.ID] = true
			depth++
			if depth > notes.MaxDepth {
				out = append(out, fmt.Sprintf("I4: node %d is deeper than MaxDepth (%d)", n.ID, notes.MaxDepth))
				break
			}
			cur = next
		}
	}
	return out
}

// checkInvariants fails the test if the whole table violates anything.
func checkInvariants(t *testing.T, handle *sql.DB) {
	t.Helper()
	if v := treeViolations(loadRawNodes(t, handle)); len(v) > 0 {
		t.Fatalf("tree invariants violated:\n  %s", strings.Join(v, "\n  "))
	}
}

func TestTreeViolationsAcceptsAHealthyTree(t *testing.T) {
	under := func(id int64) sql.NullInt64 { return sql.NullInt64{Int64: id, Valid: true} }
	nodes := []rawNode{
		{ID: 1, UserID: 1, Position: 0},
		{ID: 2, UserID: 1, Position: 1},
		{ID: 3, UserID: 1, ParentID: under(1), Position: 0},
		{ID: 4, UserID: 1, ParentID: under(1), Position: 1},
		{ID: 5, UserID: 2, Position: 0}, // a second user's separate tree
	}
	if v := treeViolations(nodes); len(v) > 0 {
		t.Fatalf("treeViolations on a healthy tree = %v; want none", v)
	}
}

func TestTreeViolationsCatchesCorruption(t *testing.T) {
	under := func(id int64) sql.NullInt64 { return sql.NullInt64{Int64: id, Valid: true} }
	tests := []struct {
		name  string
		nodes []rawNode
		want  string
	}{
		{
			name:  "gap in sibling positions",
			nodes: []rawNode{{ID: 1, UserID: 1, Position: 0}, {ID: 2, UserID: 1, Position: 2}},
			want:  "I1",
		},
		{
			name:  "two siblings share a position",
			nodes: []rawNode{{ID: 1, UserID: 1, Position: 0}, {ID: 2, UserID: 1, Position: 0}},
			want:  "I1",
		},
		{
			name:  "positions do not start at zero",
			nodes: []rawNode{{ID: 1, UserID: 1, Position: 1}},
			want:  "I1",
		},
		{
			name:  "child of another user's node",
			nodes: []rawNode{{ID: 1, UserID: 1, Position: 0}, {ID: 2, UserID: 2, ParentID: under(1), Position: 0}},
			want:  "I2",
		},
		{
			name:  "parent does not exist",
			nodes: []rawNode{{ID: 2, UserID: 1, ParentID: under(99), Position: 0}},
			want:  "I2",
		},
		{
			name: "two-node cycle",
			nodes: []rawNode{
				{ID: 1, UserID: 1, ParentID: under(2), Position: 0},
				{ID: 2, UserID: 1, ParentID: under(1), Position: 0},
			},
			want: "I3",
		},
		{
			name:  "node is its own parent",
			nodes: []rawNode{{ID: 1, UserID: 1, ParentID: under(1), Position: 0}},
			want:  "I3",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := treeViolations(tc.nodes)
			if len(got) == 0 {
				t.Fatalf("treeViolations found nothing; want a %s violation", tc.want)
			}
			if !strings.Contains(strings.Join(got, "\n"), tc.want) {
				t.Fatalf("treeViolations = %v; want a %s violation", got, tc.want)
			}
		})
	}
}

// chain builds a straight line of nodes, each the child of the one before, so
// the deepest sits at depth len(nodes)-1.
func chain(length int) []rawNode {
	var out []rawNode
	for i := int64(1); i <= int64(length); i++ {
		n := rawNode{ID: i, UserID: 1, Position: 0}
		if i > 1 {
			n.ParentID = sql.NullInt64{Int64: i - 1, Valid: true}
		}
		out = append(out, n)
	}
	return out
}

func TestTreeViolationsCatchesExcessiveDepth(t *testing.T) {
	// One level past what MaxDepth permits.
	got := treeViolations(chain(notes.MaxDepth + 2))
	if len(got) == 0 {
		t.Fatal("treeViolations found nothing; want an I4 violation")
	}
	if !strings.Contains(strings.Join(got, "\n"), "I4") {
		t.Fatalf("treeViolations = %v; want an I4 violation", got)
	}
}

func TestTreeViolationsAcceptsAChainAtExactlyMaxDepth(t *testing.T) {
	// The boundary, so the test above cannot be passing for the wrong reason:
	// MaxDepth+1 nodes put the deepest at exactly MaxDepth, which is legal.
	if v := treeViolations(chain(notes.MaxDepth + 1)); len(v) > 0 {
		t.Fatalf("treeViolations on a chain at exactly MaxDepth = %v; want none", v)
	}
}
```

- [ ] **Step 2: Write the fixture and the failing ByID tests**

Create `internal/apps/notes/store_test.go`:

```go
package notes_test

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"

	"github.com/iliafrenkel/on-suite/internal/apps/notes"
	"github.com/iliafrenkel/on-suite/internal/platform/auth"
	"github.com/iliafrenkel/on-suite/internal/platform/db"
)

// fixture is a migrated database with two users, so every owner-scoping test
// has somebody else to be confused with.
type fixture struct {
	store *notes.Store
	db    *sql.DB
	alice auth.User
	bob   auth.User
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	ctx := context.Background()

	// A real file in a temp dir, not :memory: — the bugs that matter here
	// live in SQL, recursive CTEs and transaction behaviour.
	handle, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = handle.Close() })

	platform, err := db.Collect(auth.Namespace, auth.Migrations())
	if err != nil {
		t.Fatal(err)
	}
	appSchema, err := db.Collect(notes.ID, notes.Migrations())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Apply(ctx, handle, append(platform, appSchema...)); err != nil {
		t.Fatal(err)
	}

	users := auth.NewStore(handle)
	hash, err := auth.HashPassword("a-sufficiently-long-password")
	if err != nil {
		t.Fatal(err)
	}
	alice, err := users.CreateUser(ctx, "alice", hash, true)
	if err != nil {
		t.Fatal(err)
	}
	bob, err := users.CreateUser(ctx, "bob", hash, false)
	if err != nil {
		t.Fatal(err)
	}
	return &fixture{store: notes.NewStore(handle), db: handle, alice: alice, bob: bob}
}

func TestAFreshDatabaseHasNoViolations(t *testing.T) {
	f := newFixture(t)
	checkInvariants(t, f.db)
}

func TestByIDOnAMissingNode(t *testing.T) {
	f := newFixture(t)
	if _, err := f.store.ByID(context.Background(), f.alice.ID, 12345); !errors.Is(err, notes.ErrNotFound) {
		t.Fatalf("ByID on a missing node = %v; want ErrNotFound", err)
	}
}
```

- [ ] **Step 3: Run the tests to verify they fail**

```bash
go test ./internal/apps/notes/... -count=1
```

Expected: FAIL to build, with `undefined: notes.NewStore` and
`f.store.ByID undefined`.

- [ ] **Step 4: Write the store**

Create `internal/apps/notes/store.go`:

```go
package notes

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// Store is all the SQL for ON Notes. It has no HTTP knowledge, so it can be
// tested against a real database on its own.
type Store struct {
	db  *sql.DB
	now func() time.Time
}

func NewStore(handle *sql.DB) *Store {
	return &Store{db: handle, now: func() time.Time { return time.Now().UTC() }}
}

// SetClock replaces the time source, for tests.
func (st *Store) SetClock(now func() time.Time) { st.now = now }

// nodeColumns is every column a Node is scanned from, in scan order. It lives
// in one place because half a dozen queries select exactly this list, and a
// column added to one of them and not the others is a silent scan error.
const nodeColumns = `id, user_id, parent_id, position, title, note, collapsed, created_at, updated_at`

// ByID fetches one of userID's own nodes.
func (st *Store) ByID(ctx context.Context, userID, id int64) (Node, error) {
	n, err := scanNode(st.db.QueryRowContext(ctx,
		`SELECT `+nodeColumns+` FROM notes_nodes WHERE id = ? AND user_id = ?`, id, userID))
	if errors.Is(err, sql.ErrNoRows) {
		return Node{}, ErrNotFound
	}
	return n, err
}

// rowScanner is satisfied by both *sql.Row and *sql.Rows.
type rowScanner interface{ Scan(dest ...any) error }

// scanNode reads the nodeColumns list. extra takes destinations for any
// columns a query appends after that list — Outline's depth and has_children
// — so there is only ever one place that knows the column order.
func scanNode(row rowScanner, extra ...any) (Node, error) {
	var (
		n         Node
		parent    sql.NullInt64
		createdAt string
		updatedAt string
	)
	dest := append([]any{
		&n.ID, &n.UserID, &parent, &n.Position, &n.Title, &n.Note,
		&n.Collapsed, &createdAt, &updatedAt,
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

	if n.CreatedAt, err = parseTime(createdAt); err != nil {
		return Node{}, err
	}
	if n.UpdatedAt, err = parseTime(updatedAt); err != nil {
		return Node{}, err
	}
	return n, nil
}

// Timestamps match the platform's convention: RFC 3339 nanoseconds in UTC,
// which sorts chronologically as text.
func parseTime(s string) (time.Time, error) {
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return time.Time{}, fmt.Errorf("notes: parse timestamp %q: %w", s, err)
	}
	return t.UTC(), nil
}
```

- [ ] **Step 5: Run the tests to verify they pass**

```bash
go test ./internal/apps/notes/... -count=1 -v -run 'TestTreeViolations|TestAFreshDatabase|TestByID'
```

Expected: PASS for all of them, including every `TestTreeViolationsCatchesCorruption` subtest.

- [ ] **Step 6: Commit**

```bash
git add internal/apps/notes
git commit -m "feat(notes): add the store, ByID, and the tree invariant checker"
```

---

### Task 3: Create

**Files:**
- Create: `internal/apps/notes/tree.go`
- Test: `internal/apps/notes/tree_test.go`, `internal/apps/notes/store_test.go` (one helper)

**Interfaces:**
- Consumes: everything from Tasks 1 and 2.
- Produces: `(*Store).Create(ctx, userID, parentID int64, afterPos int, title, note string) (Node, error)`; package-private `(*Store).tx`, `parentArg(int64) any`, `depthOf(ctx, *sql.Tx, userID, id int64) (int, error)`, `countChildren(ctx, *sql.Tx, userID, parentID int64) (int, error)`, `clamp(v, lo, hi int) int`, `formatTime(time.Time) string`. In tests: `(*fixture).mk(*testing.T, parentID int64, title string) notes.Node` and `(*fixture).childTitles(*testing.T, userID, parentID int64) []string`.

- [ ] **Step 1: Write the failing tests**

Append this helper to `internal/apps/notes/store_test.go` — it belongs beside
the fixture it hangs off:

```go
// childTitles reads a parent's children straight from the table, in position
// order. Structural assertions deliberately bypass the store, so a test of
// Create cannot be fooled by a matching bug in a read method.
func (f *fixture) childTitles(t *testing.T, userID, parentID int64) []string {
	t.Helper()

	var parent any
	if parentID != notes.RootID {
		parent = parentID
	}
	rows, err := f.db.QueryContext(context.Background(),
		`SELECT title FROM notes_nodes
		  WHERE user_id = ? AND parent_id IS ? ORDER BY position`, userID, parent)
	if err != nil {
		t.Fatalf("reading child titles: %v", err)
	}
	defer func() { _ = rows.Close() }()

	var out []string
	for rows.Next() {
		var title string
		if err := rows.Scan(&title); err != nil {
			t.Fatalf("scanning title: %v", err)
		}
		out = append(out, title)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("reading child titles: %v", err)
	}
	return out
}
```

Then create `internal/apps/notes/tree_test.go`:

```go
package notes_test

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"testing"

	"github.com/iliafrenkel/on-suite/internal/apps/notes"
)

// mk creates one of alice's bullets, appended last, so that tree-shape tests
// read as tree shapes rather than as error handling. The huge afterPos leans
// on Create's clamping, which therefore gets exercised by every test here.
func (f *fixture) mk(t *testing.T, parentID int64, title string) notes.Node {
	t.Helper()
	n, err := f.store.Create(context.Background(), f.alice.ID, parentID, 1<<30, title, "")
	if err != nil {
		t.Fatalf("Create(%q under %d): %v", title, parentID, err)
	}
	return n
}

func TestCreateAppendsInOrder(t *testing.T) {
	f := newFixture(t)
	f.mk(t, notes.RootID, "first")
	f.mk(t, notes.RootID, "second")
	f.mk(t, notes.RootID, "third")

	got := f.childTitles(t, f.alice.ID, notes.RootID)
	want := []string{"first", "second", "third"}
	if !slices.Equal(got, want) {
		t.Fatalf("top level = %v; want %v", got, want)
	}
	checkInvariants(t, f.db)
}

func TestCreateInsertsBetweenSiblings(t *testing.T) {
	f := newFixture(t)
	f.mk(t, notes.RootID, "first")
	f.mk(t, notes.RootID, "third")

	// afterPos 0 means "after the bullet at position 0".
	if _, err := f.store.Create(context.Background(), f.alice.ID, notes.RootID, 0, "second", ""); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got := f.childTitles(t, f.alice.ID, notes.RootID)
	want := []string{"first", "second", "third"}
	if !slices.Equal(got, want) {
		t.Fatalf("top level = %v; want %v", got, want)
	}
	checkInvariants(t, f.db)
}

func TestCreateInsertsFirstWithAfterPosMinusOne(t *testing.T) {
	f := newFixture(t)
	f.mk(t, notes.RootID, "second")

	if _, err := f.store.Create(context.Background(), f.alice.ID, notes.RootID, -1, "first", ""); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got := f.childTitles(t, f.alice.ID, notes.RootID)
	want := []string{"first", "second"}
	if !slices.Equal(got, want) {
		t.Fatalf("top level = %v; want %v", got, want)
	}
	checkInvariants(t, f.db)
}

func TestCreateUnderAParent(t *testing.T) {
	f := newFixture(t)
	parent := f.mk(t, notes.RootID, "parent")
	child := f.mk(t, parent.ID, "child")

	if child.ParentID != parent.ID {
		t.Errorf("child.ParentID = %d; want %d", child.ParentID, parent.ID)
	}
	if child.Position != 0 {
		t.Errorf("child.Position = %d; want 0", child.Position)
	}
	if got := f.childTitles(t, f.alice.ID, parent.ID); !slices.Equal(got, []string{"child"}) {
		t.Errorf("children of parent = %v; want [child]", got)
	}
	checkInvariants(t, f.db)
}

func TestByIDReturnsTheStoredNode(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	parent := f.mk(t, notes.RootID, "parent")
	child := f.mk(t, parent.ID, "child")

	got, err := f.store.ByID(ctx, f.alice.ID, child.ID)
	if err != nil {
		t.Fatalf("ByID: %v", err)
	}
	if got.Title != "child" || got.ParentID != parent.ID || got.Position != 0 {
		t.Errorf("ByID = {title %q, parent %d, position %d}; want {\"child\", %d, 0}",
			got.Title, got.ParentID, got.Position, parent.ID)
	}
	if got.CreatedAt.IsZero() || got.UpdatedAt.IsZero() {
		t.Error("timestamps are zero; scanNode did not parse them")
	}

	// A top-level node's NULL parent_id must scan back as RootID rather than
	// as some stray id — the whole reason RootID is 0.
	top, err := f.store.ByID(ctx, f.alice.ID, parent.ID)
	if err != nil {
		t.Fatalf("ByID(parent): %v", err)
	}
	if top.ParentID != notes.RootID {
		t.Errorf("top-level ParentID = %d; want RootID (%d)", top.ParentID, notes.RootID)
	}
}

func TestByIDRejectsAnotherUsersNode(t *testing.T) {
	f := newFixture(t)
	n := f.mk(t, notes.RootID, "alice's")

	if _, err := f.store.ByID(context.Background(), f.bob.ID, n.ID); !errors.Is(err, notes.ErrNotFound) {
		t.Fatalf("bob fetching alice's node = %v; want ErrNotFound", err)
	}
}

func TestCreateRejectsAnotherUsersParent(t *testing.T) {
	f := newFixture(t)
	alices := f.mk(t, notes.RootID, "alice's bullet")

	_, err := f.store.Create(context.Background(), f.bob.ID, alices.ID, 0, "bob's child", "")
	if !errors.Is(err, notes.ErrNotFound) {
		t.Fatalf("Create under another user's node = %v; want ErrNotFound", err)
	}
	checkInvariants(t, f.db)
}

func TestCreateRejectsTextThatFailsValidation(t *testing.T) {
	f := newFixture(t)
	long := make([]rune, notes.MaxTitleRunes+1)
	for i := range long {
		long[i] = 'a'
	}
	_, err := f.store.Create(context.Background(), f.alice.ID, notes.RootID, 0, string(long), "")
	if !errors.Is(err, notes.ErrInvalid) {
		t.Fatalf("Create with an oversized title = %v; want ErrInvalid", err)
	}
}

func TestCreateRefusesToPassMaxDepth(t *testing.T) {
	f := newFixture(t)

	// Build a chain down to exactly MaxDepth: MaxDepth+1 bullets, the last of
	// which sits at the deepest permitted depth.
	parent := int64(notes.RootID)
	for i := 0; i <= notes.MaxDepth; i++ {
		parent = f.mk(t, parent, fmt.Sprintf("level %d", i)).ID
	}

	_, err := f.store.Create(context.Background(), f.alice.ID, parent, 0, "one too deep", "")
	if !errors.Is(err, notes.ErrTooDeep) {
		t.Fatalf("Create below MaxDepth = %v; want ErrTooDeep", err)
	}
	checkInvariants(t, f.db)
}
```

- [ ] **Step 2: Run the tests to verify they fail**

```bash
go test ./internal/apps/notes/... -count=1 -run TestCreate
```

Expected: FAIL to build, with `f.store.Create undefined`.

- [ ] **Step 3: Write the mutation file**

Create `internal/apps/notes/tree.go`:

```go
package notes

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// tx runs fn inside a transaction, rolling back unless fn returns nil.
//
// Every mutation goes through it. A tree operation that half-applies leaves an
// outline that no later operation can repair: positions with a gap in them
// make every subsequent clamp land one place off, silently.
func (st *Store) tx(ctx context.Context, fn func(*sql.Tx) error) error {
	handle, err := st.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("notes: begin transaction: %w", err)
	}
	defer func() { _ = handle.Rollback() }() // a no-op after a successful Commit

	if err := fn(handle); err != nil {
		return err
	}
	if err := handle.Commit(); err != nil {
		return fmt.Errorf("notes: commit: %w", err)
	}
	return nil
}

// parentArg converts the RootID sentinel to the NULL the column actually
// stores. It is always paired with "parent_id IS ?", which SQLite treats as =
// for a non-NULL value and as a NULL test otherwise — so one query shape
// covers both top-level and nested nodes, with no branching anywhere.
func parentArg(parentID int64) any {
	if parentID == RootID {
		return nil
	}
	return parentID
}

// Create inserts a new bullet as a child of parentID, which may be RootID.
//
// afterPos is the position of the sibling to insert after: -1 inserts first,
// and anything at or past the last sibling appends. Out-of-range values are
// clamped rather than rejected, because "after the bullet I am looking at" is
// still the caller's intent when the tree has moved underneath them.
func (st *Store) Create(ctx context.Context, userID, parentID int64, afterPos int, title, note string) (Node, error) {
	title = strings.TrimRight(title, " \t")
	if err := Validate(title, note); err != nil {
		return Node{}, err
	}

	out := Node{UserID: userID, ParentID: parentID, Title: title, Note: note}
	err := st.tx(ctx, func(tx *sql.Tx) error {
		if parentID != RootID {
			// depthOf also answers "does this parent exist and is it yours",
			// so there is no separate ownership query.
			depth, err := depthOf(ctx, tx, userID, parentID)
			if err != nil {
				return err
			}
			if depth+1 > MaxDepth {
				return ErrTooDeep
			}
		}

		n, err := countChildren(ctx, tx, userID, parentID)
		if err != nil {
			return err
		}
		idx := clamp(afterPos+1, 0, n)

		if _, err := tx.ExecContext(ctx,
			`UPDATE notes_nodes SET position = position + 1
			  WHERE user_id = ? AND parent_id IS ? AND position >= ?`,
			userID, parentArg(parentID), idx); err != nil {
			return fmt.Errorf("notes: create: shift siblings: %w", err)
		}

		now := st.now()
		out.Position, out.CreatedAt, out.UpdatedAt = idx, now, now
		err = tx.QueryRowContext(ctx,
			`INSERT INTO notes_nodes
			     (user_id, parent_id, position, title, note, collapsed, created_at, updated_at)
			 VALUES (?, ?, ?, ?, ?, 0, ?, ?)
			 RETURNING id`,
			userID, parentArg(parentID), idx, title, note,
			formatTime(now), formatTime(now)).Scan(&out.ID)
		if err != nil {
			return fmt.Errorf("notes: create: %w", err)
		}
		return nil
	})
	if err != nil {
		return Node{}, err
	}
	return out, nil
}

// depthOf reports how deep a node sits, counting a top-level node as 0. It
// returns ErrNotFound for a node that does not exist or is not userID's.
//
// The walk is capped at MaxDepth so that a tree which has somehow acquired a
// cycle returns an answer instead of running forever.
func depthOf(ctx context.Context, tx *sql.Tx, userID, id int64) (int, error) {
	var depth sql.NullInt64
	err := tx.QueryRowContext(ctx,
		`WITH RECURSIVE up(id, parent_id, d) AS (
		     SELECT id, parent_id, 0 FROM notes_nodes WHERE id = ? AND user_id = ?
		   UNION ALL
		     SELECT p.id, p.parent_id, u.d + 1
		       FROM notes_nodes p JOIN up u ON p.id = u.parent_id
		      WHERE u.d < ?
		 )
		 SELECT max(d) FROM up`, id, userID, MaxDepth).Scan(&depth)
	if err != nil {
		return 0, fmt.Errorf("notes: depth of %d: %w", id, err)
	}
	// max() over an empty CTE is one row containing NULL, not zero rows.
	if !depth.Valid {
		return 0, ErrNotFound
	}
	return int(depth.Int64), nil
}

// countChildren counts a parent's direct children.
func countChildren(ctx context.Context, tx *sql.Tx, userID, parentID int64) (int, error) {
	var n int
	err := tx.QueryRowContext(ctx,
		`SELECT count(*) FROM notes_nodes WHERE user_id = ? AND parent_id IS ?`,
		userID, parentArg(parentID)).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("notes: count children of %d: %w", parentID, err)
	}
	return n, nil
}

// clamp bounds v to [lo, hi].
func clamp(v, lo, hi int) int { return min(max(v, lo), hi) }

func formatTime(t time.Time) string { return t.UTC().Format(time.RFC3339Nano) }
```

`errors` is not imported yet: Task 7 is the first operation in this file that
needs `errors.Is`, and an import added before its first use fails `go vet`.

- [ ] **Step 4: Run the tests to verify they pass**

```bash
go test ./internal/apps/notes/... -count=1 -run 'TestCreate|TestByID' -v
```

Expected: PASS — seven `TestCreate*` tests plus the three `TestByID*` tests
(`TestByIDOnAMissingNode` came from Task 2).

- [ ] **Step 5: Run the full check**

```bash
gofmt -l . && go vet ./... && go run honnef.co/go/tools/cmd/staticcheck@v0.8.0 ./... && go test ./... -race -count=1
```

Expected: PASS, and `gofmt -l .` prints nothing.

- [ ] **Step 6: Commit**

```bash
git add internal/apps/notes
git commit -m "feat(notes): create bullets with contiguous sibling positions"
```

---

### Task 4: Children and Ancestors

**Files:**
- Modify: `internal/apps/notes/store.go`
- Test: `internal/apps/notes/store_test.go`

**Interfaces:**
- Produces: `(*Store).Children(ctx, userID, parentID int64) ([]Node, error)`, `(*Store).Ancestors(ctx, userID, id int64) ([]Node, error)`; package-private `collectNodes(*sql.Rows, string) ([]Node, error)`.

- [ ] **Step 1: Write the failing tests**

Append to `internal/apps/notes/store_test.go`:

```go
// titles is the shape of a node slice, for assertions that care about order.
func titles(ns []notes.Node) []string {
	out := make([]string, len(ns))
	for i, n := range ns {
		out[i] = n.Title
	}
	return out
}

func TestChildrenInPositionOrder(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	parent := f.mk(t, notes.RootID, "parent")
	f.mk(t, parent.ID, "a")
	f.mk(t, parent.ID, "b")
	f.mk(t, notes.RootID, "not a child")

	got, err := f.store.Children(ctx, f.alice.ID, parent.ID)
	if err != nil {
		t.Fatalf("Children: %v", err)
	}
	if want := []string{"a", "b"}; !slices.Equal(titles(got), want) {
		t.Fatalf("Children = %v; want %v", titles(got), want)
	}
}

func TestChildrenOfRoot(t *testing.T) {
	f := newFixture(t)
	top := f.mk(t, notes.RootID, "top")
	f.mk(t, top.ID, "nested")

	got, err := f.store.Children(context.Background(), f.alice.ID, notes.RootID)
	if err != nil {
		t.Fatalf("Children: %v", err)
	}
	if want := []string{"top"}; !slices.Equal(titles(got), want) {
		t.Fatalf("Children(RootID) = %v; want %v", titles(got), want)
	}
}

func TestChildrenExcludesAnotherUser(t *testing.T) {
	f := newFixture(t)
	f.mk(t, notes.RootID, "alice's")

	got, err := f.store.Children(context.Background(), f.bob.ID, notes.RootID)
	if err != nil {
		t.Fatalf("Children: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("bob sees %v at the top level; want nothing", titles(got))
	}
}

func TestAncestorsIsTheBreadcrumb(t *testing.T) {
	f := newFixture(t)
	top := f.mk(t, notes.RootID, "top")
	mid := f.mk(t, top.ID, "mid")
	leaf := f.mk(t, mid.ID, "leaf")

	got, err := f.store.Ancestors(context.Background(), f.alice.ID, leaf.ID)
	if err != nil {
		t.Fatalf("Ancestors: %v", err)
	}
	// Outermost first, and the node itself is not included.
	if want := []string{"top", "mid"}; !slices.Equal(titles(got), want) {
		t.Fatalf("Ancestors = %v; want %v", titles(got), want)
	}
}

func TestAncestorsOfATopLevelNodeIsEmpty(t *testing.T) {
	f := newFixture(t)
	top := f.mk(t, notes.RootID, "top")

	got, err := f.store.Ancestors(context.Background(), f.alice.ID, top.ID)
	if err != nil {
		t.Fatalf("Ancestors: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("Ancestors of a top-level node = %v; want nothing", titles(got))
	}
}

func TestAncestorsOfAnotherUsersNodeIsEmpty(t *testing.T) {
	f := newFixture(t)
	top := f.mk(t, notes.RootID, "top")
	leaf := f.mk(t, top.ID, "leaf")

	got, err := f.store.Ancestors(context.Background(), f.bob.ID, leaf.ID)
	if err != nil {
		t.Fatalf("Ancestors: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("bob sees ancestors %v; want nothing", titles(got))
	}
}
```

Add `"slices"` to the imports of `store_test.go`.

- [ ] **Step 2: Run the tests to verify they fail**

```bash
go test ./internal/apps/notes/... -count=1 -run 'TestChildren|TestAncestors'
```

Expected: FAIL to build, with `f.store.Children undefined`.

- [ ] **Step 3: Implement the reads**

Append to `internal/apps/notes/store.go`:

```go
// Children returns a parent's direct children in position order. parentID may
// be RootID for the top level.
func (st *Store) Children(ctx context.Context, userID, parentID int64) ([]Node, error) {
	rows, err := st.db.QueryContext(ctx,
		`SELECT `+nodeColumns+`
		   FROM notes_nodes WHERE user_id = ? AND parent_id IS ?
		  ORDER BY position`, userID, parentArg(parentID))
	if err != nil {
		return nil, fmt.Errorf("notes: children of %d: %w", parentID, err)
	}
	return collectNodes(rows, "children")
}

// Ancestors returns the path from the top level down to id's parent — the
// breadcrumb above a zoomed outline — outermost first. It is empty for a
// top-level node, and empty for a node that does not exist or is not userID's:
// a caller that needs to tell those apart calls ByID.
func (st *Store) Ancestors(ctx context.Context, userID, id int64) ([]Node, error) {
	rows, err := st.db.QueryContext(ctx,
		`WITH RECURSIVE up AS (
		     SELECT `+nodeColumns+`, 0 AS d
		       FROM notes_nodes WHERE id = ? AND user_id = ?
		   UNION ALL
		     SELECT p.id, p.user_id, p.parent_id, p.position, p.title, p.note,
		            p.collapsed, p.created_at, p.updated_at, u.d + 1
		       FROM notes_nodes p JOIN up u ON p.id = u.parent_id
		      WHERE u.d < ?
		 )
		 SELECT `+nodeColumns+` FROM up WHERE d > 0 ORDER BY d DESC`,
		id, userID, MaxDepth)
	if err != nil {
		return nil, fmt.Errorf("notes: ancestors of %d: %w", id, err)
	}
	return collectNodes(rows, "ancestors")
}

// collectNodes drains rows into Nodes and closes them.
func collectNodes(rows *sql.Rows, what string) ([]Node, error) {
	defer func() { _ = rows.Close() }()

	var out []Node
	for rows.Next() {
		n, err := scanNode(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("notes: %s: %w", what, err)
	}
	return out, nil
}
```

- [ ] **Step 4: Run the tests to verify they pass**

```bash
go test ./internal/apps/notes/... -count=1 -run 'TestChildren|TestAncestors' -v
```

Expected: PASS, all six tests.

- [ ] **Step 5: Commit**

```bash
git add internal/apps/notes
git commit -m "feat(notes): read a parent's children and a node's breadcrumb"
```

---

### Task 5: Outline and SetCollapsed

**Files:**
- Modify: `internal/apps/notes/store.go`, `internal/apps/notes/tree.go`
- Test: `internal/apps/notes/store_test.go`, `internal/apps/notes/tree_test.go`

**Interfaces:**
- Produces: `(*Store).Outline(ctx, userID, rootID int64) ([]Node, error)` — a flat slice in document order, with `Depth` relative to `rootID` and `HasChildren` set — and `(*Store).SetCollapsed(ctx, userID, id int64, collapsed bool) error`; package-private `(*Store).update(ctx, query string, args ...any) error`.

`SetCollapsed` lands here rather than with the other text edits in Task 6
because `Outline` is not testable without it: the whole point of the collapse
flag is that it decides where the outline query stops descending.

- [ ] **Step 1: Write the failing tests**

Append to `internal/apps/notes/store_test.go`:

```go
// outlineShape renders an outline the way a person reads one, so a failing
// assertion prints a tree instead of a wall of structs.
func outlineShape(ns []notes.Node) string {
	var b strings.Builder
	for _, n := range ns {
		b.WriteString(strings.Repeat("  ", n.Depth))
		b.WriteString("- ")
		b.WriteString(n.Title)
		if n.HasChildren {
			b.WriteString(" [+]")
		}
		b.WriteString("\n")
	}
	return b.String()
}

// sample builds the tree every Outline test works from — two top-level
// bullets, a and b, where a has two children and a2 has one:
//
//	a
//	├── a1
//	└── a2
//	    └── a2x
//	b
func (f *fixture) sample(t *testing.T) (a, a1, a2, a2x, b notes.Node) {
	t.Helper()
	a = f.mk(t, notes.RootID, "a")
	a1 = f.mk(t, a.ID, "a1")
	a2 = f.mk(t, a.ID, "a2")
	a2x = f.mk(t, a2.ID, "a2x")
	b = f.mk(t, notes.RootID, "b")
	return a, a1, a2, a2x, b
}

func TestOutlineIsFlatDocumentOrder(t *testing.T) {
	f := newFixture(t)
	f.sample(t)

	got, err := f.store.Outline(context.Background(), f.alice.ID, notes.RootID)
	if err != nil {
		t.Fatalf("Outline: %v", err)
	}
	want := "- a [+]\n  - a1\n  - a2 [+]\n    - a2x\n- b\n"
	if outlineShape(got) != want {
		t.Fatalf("Outline =\n%s\nwant\n%s", outlineShape(got), want)
	}
}

func TestOutlineStopsAtACollapsedNode(t *testing.T) {
	f := newFixture(t)
	_, _, a2, _, _ := f.sample(t)

	if err := f.store.SetCollapsed(context.Background(), f.alice.ID, a2.ID, true); err != nil {
		t.Fatalf("SetCollapsed: %v", err)
	}

	got, err := f.store.Outline(context.Background(), f.alice.ID, notes.RootID)
	if err != nil {
		t.Fatalf("Outline: %v", err)
	}
	// a2x is gone, but a2 still advertises that it has children so the
	// expand arrow can be drawn.
	want := "- a [+]\n  - a1\n  - a2 [+]\n- b\n"
	if outlineShape(got) != want {
		t.Fatalf("Outline =\n%s\nwant\n%s", outlineShape(got), want)
	}
}

func TestOutlineZoomsIntoANode(t *testing.T) {
	f := newFixture(t)
	a, _, _, _, _ := f.sample(t)

	got, err := f.store.Outline(context.Background(), f.alice.ID, a.ID)
	if err != nil {
		t.Fatalf("Outline: %v", err)
	}
	// The zoom root's direct children are at depth 0, and b is not in view.
	want := "- a1\n- a2 [+]\n  - a2x\n"
	if outlineShape(got) != want {
		t.Fatalf("Outline(zoomed) =\n%s\nwant\n%s", outlineShape(got), want)
	}
}

func TestOutlineExcludesAnotherUser(t *testing.T) {
	f := newFixture(t)
	f.sample(t)

	got, err := f.store.Outline(context.Background(), f.bob.ID, notes.RootID)
	if err != nil {
		t.Fatalf("Outline: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("bob's outline =\n%s\nwant nothing", outlineShape(got))
	}
}

func TestOutlineOfAnotherUsersNodeIsEmpty(t *testing.T) {
	f := newFixture(t)
	a, _, _, _, _ := f.sample(t)

	got, err := f.store.Outline(context.Background(), f.bob.ID, a.ID)
	if err != nil {
		t.Fatalf("Outline: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("bob zoomed into alice's node and saw\n%s\nwant nothing", outlineShape(got))
	}
}
```

Add `"strings"` to the imports of `store_test.go`.

And append the `SetCollapsed` tests to `internal/apps/notes/tree_test.go`:

```go
func TestSetCollapsedRoundTrips(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	n := f.mk(t, notes.RootID, "parent")

	if err := f.store.SetCollapsed(ctx, f.alice.ID, n.ID, true); err != nil {
		t.Fatalf("SetCollapsed(true): %v", err)
	}
	got, err := f.store.ByID(ctx, f.alice.ID, n.ID)
	if err != nil {
		t.Fatalf("ByID: %v", err)
	}
	if !got.Collapsed {
		t.Fatal("Collapsed = false after SetCollapsed(true)")
	}

	if err := f.store.SetCollapsed(ctx, f.alice.ID, n.ID, false); err != nil {
		t.Fatalf("SetCollapsed(false): %v", err)
	}
	if got, _ = f.store.ByID(ctx, f.alice.ID, n.ID); got.Collapsed {
		t.Fatal("Collapsed = true after SetCollapsed(false)")
	}
}

func TestSetCollapsedRejectsAnotherUsersNode(t *testing.T) {
	f := newFixture(t)
	n := f.mk(t, notes.RootID, "alice's")

	err := f.store.SetCollapsed(context.Background(), f.bob.ID, n.ID, true)
	if !errors.Is(err, notes.ErrNotFound) {
		t.Fatalf("SetCollapsed on another user's node = %v; want ErrNotFound", err)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

```bash
go test ./internal/apps/notes/... -count=1 -run TestOutline
```

Expected: FAIL to build, with `f.store.Outline undefined` and
`f.store.SetCollapsed undefined`.

- [ ] **Step 3: Implement Outline and SetCollapsed**

Append to `internal/apps/notes/store.go`:

```go
// Outline returns everything visible under rootID, in document order, with
// Depth relative to rootID and HasChildren set. rootID may be RootID.
//
// The result is flat rather than nested: the caller renders indentation from
// Depth, which keeps rendering non-recursive and makes this method's output
// trivial to assert in a test.
//
// The walk stops at any collapsed node, so the result is always exactly what
// is on screen — that is why collapsed is stored rather than kept in the
// browser. It is additionally capped at MaxDepth, so a tree that has somehow
// acquired a cycle returns a truncated outline instead of hanging.
//
// The ordering trick is the path column: each row carries its ancestors'
// positions as fixed-width text, so plain lexicographic ORDER BY produces
// pre-order — a parent immediately before its subtree, siblings by position.
func (st *Store) Outline(ctx context.Context, userID, rootID int64) ([]Node, error) {
	rows, err := st.db.QueryContext(ctx,
		`WITH RECURSIVE tree AS (
		     SELECT `+nodeColumns+`, 0 AS depth, printf('%08d', position) AS path
		       FROM notes_nodes
		      WHERE user_id = ? AND parent_id IS ?
		   UNION ALL
		     SELECT c.id, c.user_id, c.parent_id, c.position, c.title, c.note,
		            c.collapsed, c.created_at, c.updated_at,
		            t.depth + 1, t.path || '/' || printf('%08d', c.position)
		       FROM notes_nodes c JOIN tree t ON c.parent_id = t.id
		      WHERE t.collapsed = 0 AND t.depth + 1 <= ?
		 )
		 SELECT `+nodeColumns+`, depth,
		        EXISTS (SELECT 1 FROM notes_nodes k WHERE k.parent_id = tree.id)
		   FROM tree ORDER BY path`,
		userID, parentArg(rootID), MaxDepth)
	if err != nil {
		return nil, fmt.Errorf("notes: outline of %d: %w", rootID, err)
	}
	defer func() { _ = rows.Close() }()

	var out []Node
	for rows.Next() {
		// The EXISTS subquery needs no user_id filter: a child always has the
		// same owner as its parent (invariant I2).
		var (
			depth       int
			hasChildren bool
		)
		n, err := scanNode(rows, &depth, &hasChildren)
		if err != nil {
			return nil, err
		}
		n.Depth, n.HasChildren = depth, hasChildren
		out = append(out, n)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("notes: outline: %w", err)
	}
	return out, nil
}
```

Append to `internal/apps/notes/tree.go`:

```go
// SetCollapsed records whether a bullet's children are hidden. It is stored
// rather than kept in the browser because Outline stops descending at a
// collapsed node: the flag decides what the server sends, not just what the
// page shows.
func (st *Store) SetCollapsed(ctx context.Context, userID, id int64, collapsed bool) error {
	return st.update(ctx,
		`UPDATE notes_nodes SET collapsed = ?, updated_at = ?
		  WHERE id = ? AND user_id = ?`,
		collapsed, formatTime(st.now()), id, userID)
}

// update runs a single-row UPDATE and turns "nothing matched" into
// ErrNotFound, which covers both "no such node" and "not yours".
func (st *Store) update(ctx context.Context, query string, args ...any) error {
	res, err := st.db.ExecContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("notes: update: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("notes: update: %w", err)
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}
```

- [ ] **Step 4: Run the tests to verify they pass**

```bash
go test ./internal/apps/notes/... -count=1 -run 'TestOutline|TestSetCollapsed' -v
```

Expected: PASS, all seven tests. If `TestOutlineIsFlatDocumentOrder` fails with
the right nodes in the wrong order, the `path` expression is wrong — every
segment must be the same width, or lexicographic order stops matching numeric
order.

- [ ] **Step 5: Commit**

```bash
git add internal/apps/notes
git commit -m "feat(notes): render a flat, collapse-aware outline with zoom"
```

---

### Task 6: SetText

**Files:**
- Modify: `internal/apps/notes/tree.go`
- Test: `internal/apps/notes/tree_test.go`

**Interfaces:**
- Produces: `(*Store).SetText(ctx, userID, id int64, title, note string) error`. Reuses the `update` helper from Task 5.

- [ ] **Step 1: Write the failing tests**

Append to `internal/apps/notes/tree_test.go`:

```go
func TestSetTextReplacesBothFields(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	n := f.mk(t, notes.RootID, "before")

	if err := f.store.SetText(ctx, f.alice.ID, n.ID, "after", "a note"); err != nil {
		t.Fatalf("SetText: %v", err)
	}

	got, err := f.store.ByID(ctx, f.alice.ID, n.ID)
	if err != nil {
		t.Fatalf("ByID: %v", err)
	}
	if got.Title != "after" || got.Note != "a note" {
		t.Fatalf("after SetText: title = %q, note = %q; want \"after\", \"a note\"", got.Title, got.Note)
	}
}

func TestSetTextRejectsAnotherUsersNode(t *testing.T) {
	f := newFixture(t)
	n := f.mk(t, notes.RootID, "alice's")

	err := f.store.SetText(context.Background(), f.bob.ID, n.ID, "bob was here", "")
	if !errors.Is(err, notes.ErrNotFound) {
		t.Fatalf("SetText on another user's node = %v; want ErrNotFound", err)
	}
	got, _ := f.store.ByID(context.Background(), f.alice.ID, n.ID)
	if got.Title != "alice's" {
		t.Fatalf("title = %q; bob's write went through", got.Title)
	}
}

func TestSetTextRejectsInvalidText(t *testing.T) {
	f := newFixture(t)
	n := f.mk(t, notes.RootID, "fine")

	err := f.store.SetText(context.Background(), f.alice.ID, n.ID, "\xff\xfe", "")
	if !errors.Is(err, notes.ErrInvalid) {
		t.Fatalf("SetText with invalid UTF-8 = %v; want ErrInvalid", err)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

```bash
go test ./internal/apps/notes/... -count=1 -run TestSetText
```

Expected: FAIL to build, with `f.store.SetText undefined`.

- [ ] **Step 3: Implement SetText**

Append to `internal/apps/notes/tree.go`:

```go
// SetText replaces a bullet's title and note.
//
// Trailing spaces are stripped but leading ones are not: an outline is written
// in prose, and a leading space is sometimes deliberate, while a trailing one
// never is.
func (st *Store) SetText(ctx context.Context, userID, id int64, title, note string) error {
	title = strings.TrimRight(title, " \t")
	if err := Validate(title, note); err != nil {
		return err
	}
	return st.update(ctx,
		`UPDATE notes_nodes SET title = ?, note = ?, updated_at = ?
		  WHERE id = ? AND user_id = ?`,
		title, note, formatTime(st.now()), id, userID)
}
```

- [ ] **Step 4: Run the tests to verify they pass**

```bash
go test ./internal/apps/notes/... -count=1 -run TestSetText -v
```

Expected: PASS, all three tests.

- [ ] **Step 5: Commit**

```bash
git add internal/apps/notes
git commit -m "feat(notes): edit a bullet's title and note"
```

---

### Task 7: Delete

**Files:**
- Modify: `internal/apps/notes/tree.go`
- Test: `internal/apps/notes/tree_test.go`

**Interfaces:**
- Produces: `(*Store).Delete(ctx, userID, id int64) error`.

- [ ] **Step 1: Write the failing tests**

Append to `internal/apps/notes/tree_test.go`:

```go
// countRows is the total number of nodes in the table, for cascade tests.
func countRows(t *testing.T, f *fixture) int {
	t.Helper()
	var n int
	if err := f.db.QueryRowContext(context.Background(),
		`SELECT count(*) FROM notes_nodes`).Scan(&n); err != nil {
		t.Fatalf("counting rows: %v", err)
	}
	return n
}

func TestDeleteClosesTheGap(t *testing.T) {
	f := newFixture(t)
	f.mk(t, notes.RootID, "a")
	b := f.mk(t, notes.RootID, "b")
	f.mk(t, notes.RootID, "c")

	if err := f.store.Delete(context.Background(), f.alice.ID, b.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	got := f.childTitles(t, f.alice.ID, notes.RootID)
	if want := []string{"a", "c"}; !slices.Equal(got, want) {
		t.Fatalf("top level = %v; want %v", got, want)
	}
	checkInvariants(t, f.db)
}

func TestDeleteTakesTheSubtreeWithIt(t *testing.T) {
	f := newFixture(t)
	a, _, _, _, _ := f.sample(t) // a has three descendants; b is separate

	if err := f.store.Delete(context.Background(), f.alice.ID, a.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	if n := countRows(t, f); n != 1 {
		t.Fatalf("%d nodes remain; want 1 (only b) — the cascade did not fire", n)
	}
	checkInvariants(t, f.db)
}

func TestDeleteRejectsAnotherUsersNode(t *testing.T) {
	f := newFixture(t)
	n := f.mk(t, notes.RootID, "alice's")

	err := f.store.Delete(context.Background(), f.bob.ID, n.ID)
	if !errors.Is(err, notes.ErrNotFound) {
		t.Fatalf("Delete of another user's node = %v; want ErrNotFound", err)
	}
	if countRows(t, f) != 1 {
		t.Fatal("bob's delete went through")
	}
}

func TestDeleteOfAMissingNode(t *testing.T) {
	f := newFixture(t)
	err := f.store.Delete(context.Background(), f.alice.ID, 4242)
	if !errors.Is(err, notes.ErrNotFound) {
		t.Fatalf("Delete of a missing node = %v; want ErrNotFound", err)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

```bash
go test ./internal/apps/notes/... -count=1 -run TestDelete
```

Expected: FAIL to build, with `f.store.Delete undefined`.

- [ ] **Step 3: Implement Delete**

Append to `internal/apps/notes/tree.go`:

```go
// Delete removes a bullet and everything under it, then closes the gap it
// left among its siblings.
func (st *Store) Delete(ctx context.Context, userID, id int64) error {
	return st.tx(ctx, func(tx *sql.Tx) error {
		var (
			parent sql.NullInt64
			pos    int
		)
		err := tx.QueryRowContext(ctx,
			`SELECT parent_id, position FROM notes_nodes WHERE id = ? AND user_id = ?`,
			id, userID).Scan(&parent, &pos)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("notes: delete: %w", err)
		}

		// The subtree goes with it: notes_nodes.parent_id is ON DELETE
		// CASCADE, and the platform opens SQLite with foreign_keys=ON.
		if _, err := tx.ExecContext(ctx,
			`DELETE FROM notes_nodes WHERE id = ? AND user_id = ?`, id, userID); err != nil {
			return fmt.Errorf("notes: delete: %w", err)
		}
		if _, err := tx.ExecContext(ctx,
			`UPDATE notes_nodes SET position = position - 1
			  WHERE user_id = ? AND parent_id IS ? AND position > ?`,
			userID, parentArg(parent.Int64), pos); err != nil {
			return fmt.Errorf("notes: delete: close the gap: %w", err)
		}
		return nil
	})
}
```

`parentArg(parent.Int64)` is correct for a top-level node: `Int64` is 0 when
`Valid` is false, and `parentArg(0)` is `nil`.

- [ ] **Step 4: Run the tests to verify they pass**

```bash
go test ./internal/apps/notes/... -count=1 -run TestDelete -v
```

Expected: PASS, all four tests. If `TestDeleteTakesTheSubtreeWithIt` fails with
four rows remaining, foreign keys are off — check the DSN in
`internal/platform/db/db.go`.

- [ ] **Step 5: Commit**

```bash
git add internal/apps/notes
git commit -m "feat(notes): delete a bullet and its subtree"
```

---

### Task 8: Move

**Files:**
- Modify: `internal/apps/notes/tree.go`
- Test: `internal/apps/notes/tree_test.go`

**Interfaces:**
- Produces: `(*Store).Move(ctx, userID, id, newParentID int64, newPos int) error`; package-private `heightOf(ctx, *sql.Tx, userID, id int64) (int, error)` and `isDescendant(ctx, *sql.Tx, userID, candidate, root int64) (bool, error)`.

- [ ] **Step 1: Write the failing tests**

Append to `internal/apps/notes/tree_test.go`:

```go
func TestMoveToAnotherParent(t *testing.T) {
	f := newFixture(t)
	a := f.mk(t, notes.RootID, "a")
	b := f.mk(t, notes.RootID, "b")
	x := f.mk(t, a.ID, "x")

	if err := f.store.Move(context.Background(), f.alice.ID, x.ID, b.ID, 0); err != nil {
		t.Fatalf("Move: %v", err)
	}

	if got := f.childTitles(t, f.alice.ID, a.ID); len(got) != 0 {
		t.Errorf("a still has %v", got)
	}
	if got := f.childTitles(t, f.alice.ID, b.ID); !slices.Equal(got, []string{"x"}) {
		t.Errorf("b has %v; want [x]", got)
	}
	checkInvariants(t, f.db)
}

func TestMoveWithinAParent(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	f.mk(t, notes.RootID, "a")
	b := f.mk(t, notes.RootID, "b")
	c := f.mk(t, notes.RootID, "c")

	// b down one.
	if err := f.store.Move(ctx, f.alice.ID, b.ID, notes.RootID, 2); err != nil {
		t.Fatalf("Move down: %v", err)
	}
	if got := f.childTitles(t, f.alice.ID, notes.RootID); !slices.Equal(got, []string{"a", "c", "b"}) {
		t.Fatalf("after moving b down: %v; want [a c b]", got)
	}
	checkInvariants(t, f.db)

	// c up one, from its new position 1 to 0.
	if err := f.store.Move(ctx, f.alice.ID, c.ID, notes.RootID, 0); err != nil {
		t.Fatalf("Move up: %v", err)
	}
	if got := f.childTitles(t, f.alice.ID, notes.RootID); !slices.Equal(got, []string{"c", "a", "b"}) {
		t.Fatalf("after moving c up: %v; want [c a b]", got)
	}
	checkInvariants(t, f.db)
}

func TestMoveClampsAnOutOfRangePosition(t *testing.T) {
	f := newFixture(t)
	a := f.mk(t, notes.RootID, "a")
	f.mk(t, notes.RootID, "b")

	if err := f.store.Move(context.Background(), f.alice.ID, a.ID, notes.RootID, 99); err != nil {
		t.Fatalf("Move: %v", err)
	}
	if got := f.childTitles(t, f.alice.ID, notes.RootID); !slices.Equal(got, []string{"b", "a"}) {
		t.Fatalf("top level = %v; want [b a]", got)
	}
	checkInvariants(t, f.db)
}

func TestMoveRefusesToNestANodeInsideItself(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	a := f.mk(t, notes.RootID, "a")
	child := f.mk(t, a.ID, "child")
	grandchild := f.mk(t, child.ID, "grandchild")

	for _, tc := range []struct {
		name   string
		target int64
	}{
		{"into itself", a.ID},
		{"into its child", child.ID},
		{"into its grandchild", grandchild.ID},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := f.store.Move(ctx, f.alice.ID, a.ID, tc.target, 0)
			if !errors.Is(err, notes.ErrCycle) {
				t.Fatalf("Move %s = %v; want ErrCycle", tc.name, err)
			}
			checkInvariants(t, f.db)
		})
	}
}

func TestMoveRejectsAnotherUsersNodeOrParent(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	a := f.mk(t, notes.RootID, "alice's a")
	b := f.mk(t, notes.RootID, "alice's b")

	if err := f.store.Move(ctx, f.bob.ID, a.ID, notes.RootID, 0); !errors.Is(err, notes.ErrNotFound) {
		t.Errorf("bob moving alice's node = %v; want ErrNotFound", err)
	}

	// bob owns nothing, so a move of his own (nonexistent) node into alice's
	// parent must also fail.
	if err := f.store.Move(ctx, f.bob.ID, 999, b.ID, 0); !errors.Is(err, notes.ErrNotFound) {
		t.Errorf("bob moving into alice's node = %v; want ErrNotFound", err)
	}
	checkInvariants(t, f.db)
}

func TestMoveCarriesTheSubtree(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	a := f.mk(t, notes.RootID, "a")
	x := f.mk(t, a.ID, "x")
	f.mk(t, x.ID, "x1")
	f.mk(t, x.ID, "x2")
	b := f.mk(t, notes.RootID, "b")

	if err := f.store.Move(ctx, f.alice.ID, x.ID, b.ID, 0); err != nil {
		t.Fatalf("Move: %v", err)
	}

	got, err := f.store.Outline(ctx, f.alice.ID, notes.RootID)
	if err != nil {
		t.Fatalf("Outline: %v", err)
	}
	// "taking its subtree with it" is the method's headline promise, and it
	// is the only path on which heightOf returns anything but zero.
	want := "- a\n- b [+]\n  - x [+]\n    - x1\n    - x2\n"
	if outlineShape(got) != want {
		t.Fatalf("after moving a subtree:\n%s\nwant\n%s", outlineShape(got), want)
	}
	checkInvariants(t, f.db)
}

func TestMoveDoesNotRenumberAnotherUsersTopLevel(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	// A top-level bullet has parent_id NULL for every user alike, so the
	// user_id filter on the two renumbering statements is the only thing
	// keeping alice's move out of bob's outline. A leak there would leave
	// bob's positions contiguous but wrong — exactly the corruption
	// treeViolations is unable to see.
	for _, title := range []string{"bob 0", "bob 1", "bob 2"} {
		if _, err := f.store.Create(ctx, f.bob.ID, notes.RootID, 1<<30, title, ""); err != nil {
			t.Fatalf("Create for bob: %v", err)
		}
	}
	a := f.mk(t, notes.RootID, "alice 0")
	f.mk(t, notes.RootID, "alice 1")
	f.mk(t, notes.RootID, "alice 2")

	if err := f.store.Move(ctx, f.alice.ID, a.ID, notes.RootID, 2); err != nil {
		t.Fatalf("Move: %v", err)
	}

	if got := f.childTitles(t, f.alice.ID, notes.RootID); !slices.Equal(got, []string{"alice 1", "alice 2", "alice 0"}) {
		t.Errorf("alice's top level = %v; want [alice 1 alice 2 alice 0]", got)
	}
	if got := f.childTitles(t, f.bob.ID, notes.RootID); !slices.Equal(got, []string{"bob 0", "bob 1", "bob 2"}) {
		t.Errorf("bob's top level = %v; want [bob 0 bob 1 bob 2] untouched", got)
	}
	checkInvariants(t, f.db)
}

func TestMoveAllowsLandingExactlyAtMaxDepth(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	// A chain whose deepest bullet sits at MaxDepth-1, so a leaf moved under
	// it lands at exactly MaxDepth — the deepest permitted depth. This pins
	// the permitting side of the guard: with >= in place of >, it would fail.
	parent := int64(notes.RootID)
	for i := 0; i < notes.MaxDepth; i++ {
		parent = f.mk(t, parent, fmt.Sprintf("level %d", i)).ID
	}
	leaf := f.mk(t, notes.RootID, "leaf")

	if err := f.store.Move(ctx, f.alice.ID, leaf.ID, parent, 0); err != nil {
		t.Fatalf("Move landing at exactly MaxDepth = %v; want nil", err)
	}
	checkInvariants(t, f.db)
}

func TestMoveRefusesToPassMaxDepth(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	// A chain whose deepest bullet sits at exactly MaxDepth.
	deepest := int64(notes.RootID)
	for i := 0; i <= notes.MaxDepth; i++ {
		deepest = f.mk(t, deepest, fmt.Sprintf("level %d", i)).ID
	}
	// A separate two-level subtree, so moving it needs two more levels.
	top := f.mk(t, notes.RootID, "top")
	f.mk(t, top.ID, "under top")

	if err := f.store.Move(ctx, f.alice.ID, top.ID, deepest, 0); !errors.Is(err, notes.ErrTooDeep) {
		t.Fatalf("Move past MaxDepth = %v; want ErrTooDeep", err)
	}
	checkInvariants(t, f.db)
}
```

- [ ] **Step 2: Run the tests to verify they fail**

```bash
go test ./internal/apps/notes/... -count=1 -run TestMove
```

Expected: FAIL to build, with `f.store.Move undefined`.

- [ ] **Step 3: Implement Move**

Append to `internal/apps/notes/tree.go`:

```go
// Move reparents a bullet, taking its subtree with it, and places it at newPos
// among its new siblings. newPos is clamped into range. newParentID may be
// RootID.
//
// This is the one operation that can corrupt the tree. Moving a bullet inside
// its own subtree detaches a cycle that no outline can reach: the rows are
// still in the table but gone from the app, and nothing in the UI can undo it.
// Both guards below therefore run inside the same transaction as the move,
// not before it.
func (st *Store) Move(ctx context.Context, userID, id, newParentID int64, newPos int) error {
	if id == newParentID {
		return ErrCycle
	}
	return st.tx(ctx, func(tx *sql.Tx) error {
		var (
			oldParent sql.NullInt64
			oldPos    int
		)
		err := tx.QueryRowContext(ctx,
			`SELECT parent_id, position FROM notes_nodes WHERE id = ? AND user_id = ?`,
			id, userID).Scan(&oldParent, &oldPos)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("notes: move: %w", err)
		}

		if newParentID != RootID {
			inside, err := isDescendant(ctx, tx, userID, newParentID, id)
			if err != nil {
				return err
			}
			if inside {
				return ErrCycle
			}
			// depthOf doubles as the ownership check on the new parent.
			parentDepth, err := depthOf(ctx, tx, userID, newParentID)
			if err != nil {
				return err
			}
			height, err := heightOf(ctx, tx, userID, id)
			if err != nil {
				return err
			}
			if parentDepth+1+height > MaxDepth {
				return ErrTooDeep
			}
		}

		// Close the gap the bullet leaves behind.
		//
		// The "id != ?" on this shift and the next is belt and braces rather
		// than load-bearing. This predicate cannot match the moving row in
		// any case, since its position is exactly oldPos; and any increment
		// the next shift applied to it would be overwritten by the final
		// update below. They stay because a future change to either
		// predicate would make them matter, and they cost one comparison.
		if _, err := tx.ExecContext(ctx,
			`UPDATE notes_nodes SET position = position - 1
			  WHERE user_id = ? AND parent_id IS ? AND position > ? AND id != ?`,
			userID, parentArg(oldParent.Int64), oldPos, id); err != nil {
			return fmt.Errorf("notes: move: close the gap: %w", err)
		}

		n, err := countChildren(ctx, tx, userID, newParentID)
		if err != nil {
			return err
		}
		if newParentID == oldParent.Int64 {
			n-- // the bullet is still counted among its own new siblings
		}
		idx := clamp(newPos, 0, n)

		if _, err := tx.ExecContext(ctx,
			`UPDATE notes_nodes SET position = position + 1
			  WHERE user_id = ? AND parent_id IS ? AND position >= ? AND id != ?`,
			userID, parentArg(newParentID), idx, id); err != nil {
			return fmt.Errorf("notes: move: open a gap: %w", err)
		}

		if _, err := tx.ExecContext(ctx,
			`UPDATE notes_nodes SET parent_id = ?, position = ?, updated_at = ?
			  WHERE id = ? AND user_id = ?`,
			parentArg(newParentID), idx, formatTime(st.now()), id, userID); err != nil {
			return fmt.Errorf("notes: move: %w", err)
		}
		return nil
	})
}

// heightOf reports how many levels of descendants a node has: 0 for a leaf.
// It returns ErrNotFound on the same terms as depthOf.
func heightOf(ctx context.Context, tx *sql.Tx, userID, id int64) (int, error) {
	var height sql.NullInt64
	err := tx.QueryRowContext(ctx,
		`WITH RECURSIVE down(id, d) AS (
		     SELECT id, 0 FROM notes_nodes WHERE id = ? AND user_id = ?
		   UNION ALL
		     SELECT c.id, n.d + 1
		       FROM notes_nodes c JOIN down n ON c.parent_id = n.id
		      WHERE n.d < ?
		 )
		 SELECT max(d) FROM down`, id, userID, MaxDepth).Scan(&height)
	if err != nil {
		return 0, fmt.Errorf("notes: height of %d: %w", id, err)
	}
	if !height.Valid {
		return 0, ErrNotFound
	}
	return int(height.Int64), nil
}

// isDescendant reports whether candidate sits anywhere inside root's subtree,
// by walking up from candidate and looking for root. Walking up is bounded by
// MaxDepth; walking down would be bounded only by the size of the subtree.
func isDescendant(ctx context.Context, tx *sql.Tx, userID, candidate, root int64) (bool, error) {
	var found int
	err := tx.QueryRowContext(ctx,
		`WITH RECURSIVE up(id, parent_id, d) AS (
		     SELECT id, parent_id, 0 FROM notes_nodes WHERE id = ? AND user_id = ?
		   UNION ALL
		     SELECT p.id, p.parent_id, u.d + 1
		       FROM notes_nodes p JOIN up u ON p.id = u.parent_id
		      WHERE u.d < ?
		 )
		 SELECT count(*) FROM up WHERE id = ?`,
		candidate, userID, MaxDepth, root).Scan(&found)
	if err != nil {
		return false, fmt.Errorf("notes: cycle check: %w", err)
	}
	return found > 0, nil
}
```

- [ ] **Step 4: Run the tests to verify they pass**

```bash
go test ./internal/apps/notes/... -count=1 -run TestMove -v
```

Expected: PASS, including all three `TestMoveRefusesToNestANodeInsideItself`
subtests.

- [ ] **Step 5: Run the full check**

```bash
gofmt -l . && go vet ./... && go run honnef.co/go/tools/cmd/staticcheck@v0.8.0 ./... && go test ./... -race -count=1
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/apps/notes
git commit -m "feat(notes): move a subtree, refusing cycles and excess depth"
```

---

### Task 9: Indent, Outdent, MoveUp, MoveDown

**Files:**
- Modify: `internal/apps/notes/tree.go`
- Test: `internal/apps/notes/tree_test.go`

**Interfaces:**
- Produces: `(*Store).Indent`, `(*Store).Outdent`, `(*Store).MoveUp`, `(*Store).MoveDown`, each `(ctx, userID, id int64) error`; package-private `(*Store).siblingAt(ctx, userID, parentID int64, pos int) (Node, error)` and the `maxPosition` constant.

- [ ] **Step 1: Write the failing tests**

Append to `internal/apps/notes/tree_test.go`:

```go
func TestIndentBecomesTheLastChildOfThePreviousSibling(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	a := f.mk(t, notes.RootID, "a")
	f.mk(t, a.ID, "existing child")
	b := f.mk(t, notes.RootID, "b")

	if err := f.store.Indent(ctx, f.alice.ID, b.ID); err != nil {
		t.Fatalf("Indent: %v", err)
	}

	got, err := f.store.Outline(ctx, f.alice.ID, notes.RootID)
	if err != nil {
		t.Fatalf("Outline: %v", err)
	}
	want := "- a [+]\n  - existing child\n  - b\n"
	if outlineShape(got) != want {
		t.Fatalf("after Indent:\n%s\nwant\n%s", outlineShape(got), want)
	}
	checkInvariants(t, f.db)
}

func TestIndentTheFirstSiblingDoesNothing(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	a := f.mk(t, notes.RootID, "a")
	f.mk(t, notes.RootID, "b")

	// A keypress with nowhere to go is not an error.
	if err := f.store.Indent(ctx, f.alice.ID, a.ID); err != nil {
		t.Fatalf("Indent of the first sibling = %v; want nil", err)
	}
	if got := f.childTitles(t, f.alice.ID, notes.RootID); !slices.Equal(got, []string{"a", "b"}) {
		t.Fatalf("top level = %v; want [a b]", got)
	}
	checkInvariants(t, f.db)
}

func TestOutdentBecomesTheNextSiblingOfItsParent(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	a := f.mk(t, notes.RootID, "a")
	child := f.mk(t, a.ID, "child")
	f.mk(t, a.ID, "later sibling")
	f.mk(t, notes.RootID, "z")

	if err := f.store.Outdent(ctx, f.alice.ID, child.ID); err != nil {
		t.Fatalf("Outdent: %v", err)
	}

	got, err := f.store.Outline(ctx, f.alice.ID, notes.RootID)
	if err != nil {
		t.Fatalf("Outline: %v", err)
	}
	// child lands directly after a; its former following sibling stays put.
	want := "- a [+]\n  - later sibling\n- child\n- z\n"
	if outlineShape(got) != want {
		t.Fatalf("after Outdent:\n%s\nwant\n%s", outlineShape(got), want)
	}
	checkInvariants(t, f.db)
}

func TestOutdentATopLevelBulletDoesNothing(t *testing.T) {
	f := newFixture(t)
	a := f.mk(t, notes.RootID, "a")

	if err := f.store.Outdent(context.Background(), f.alice.ID, a.ID); err != nil {
		t.Fatalf("Outdent at the top level = %v; want nil", err)
	}
	checkInvariants(t, f.db)
}

func TestMoveUpAndMoveDown(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	a := f.mk(t, notes.RootID, "a")
	f.mk(t, notes.RootID, "b")
	c := f.mk(t, notes.RootID, "c")

	if err := f.store.MoveDown(ctx, f.alice.ID, a.ID); err != nil {
		t.Fatalf("MoveDown: %v", err)
	}
	if got := f.childTitles(t, f.alice.ID, notes.RootID); !slices.Equal(got, []string{"b", "a", "c"}) {
		t.Fatalf("after MoveDown: %v; want [b a c]", got)
	}

	if err := f.store.MoveUp(ctx, f.alice.ID, c.ID); err != nil {
		t.Fatalf("MoveUp: %v", err)
	}
	if got := f.childTitles(t, f.alice.ID, notes.RootID); !slices.Equal(got, []string{"b", "c", "a"}) {
		t.Fatalf("after MoveUp: %v; want [b c a]", got)
	}
	checkInvariants(t, f.db)
}

func TestMoveUpAndDownAtTheEndsDoNothing(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	a := f.mk(t, notes.RootID, "a")
	b := f.mk(t, notes.RootID, "b")

	if err := f.store.MoveUp(ctx, f.alice.ID, a.ID); err != nil {
		t.Fatalf("MoveUp of the first bullet = %v; want nil", err)
	}
	if err := f.store.MoveDown(ctx, f.alice.ID, b.ID); err != nil {
		t.Fatalf("MoveDown of the last bullet = %v; want nil", err)
	}
	if got := f.childTitles(t, f.alice.ID, notes.RootID); !slices.Equal(got, []string{"a", "b"}) {
		t.Fatalf("top level = %v; want [a b] unchanged", got)
	}
	checkInvariants(t, f.db)
}

func TestKeyboardMovesRejectAnotherUsersNode(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	f.mk(t, notes.RootID, "a")
	b := f.mk(t, notes.RootID, "b")

	for name, op := range map[string]func(context.Context, int64, int64) error{
		"Indent":   f.store.Indent,
		"Outdent":  f.store.Outdent,
		"MoveUp":   f.store.MoveUp,
		"MoveDown": f.store.MoveDown,
	} {
		if err := op(ctx, f.bob.ID, b.ID); !errors.Is(err, notes.ErrNotFound) {
			t.Errorf("%s on another user's node = %v; want ErrNotFound", name, err)
		}
	}
	checkInvariants(t, f.db)
}
```

- [ ] **Step 2: Run the tests to verify they fail**

```bash
go test ./internal/apps/notes/... -count=1 -run 'TestIndent|TestOutdent|TestMoveUp|TestKeyboard'
```

Expected: FAIL to build, with `f.store.Indent undefined`.

- [ ] **Step 3: Implement the four keyboard moves**

Append to `internal/apps/notes/tree.go`:

```go
// maxPosition means "past the last sibling". Move clamps, so this expresses
// "append" without a second code path.
const maxPosition = 1 << 30

// Indent makes a bullet the last child of the sibling above it.
//
// A bullet that is already first among its siblings has nowhere to go, and
// that is not an error: the caller is a keypress, not a command, and Tab on
// the first line of an outline should do nothing rather than complain.
//
// Reading the bullet and moving it are two transactions. Between them another
// tab could move the tree, in which case Move clamps to a position that is
// merely surprising — the invariants still hold, because Move re-derives
// everything it changes inside its own transaction.
func (st *Store) Indent(ctx context.Context, userID, id int64) error {
	n, err := st.ByID(ctx, userID, id)
	if err != nil {
		return err
	}
	if n.Position == 0 {
		return nil
	}
	prev, err := st.siblingAt(ctx, userID, n.ParentID, n.Position-1)
	if err != nil {
		return err
	}
	return st.Move(ctx, userID, id, prev.ID, maxPosition)
}

// Outdent makes a bullet the next sibling of its own parent.
//
// Its former following siblings stay where they are. Some outliners instead
// adopt them as children of the outdented bullet; this is the simpler rule and
// the one that never moves a bullet the user was not looking at.
func (st *Store) Outdent(ctx context.Context, userID, id int64) error {
	n, err := st.ByID(ctx, userID, id)
	if err != nil {
		return err
	}
	if n.ParentID == RootID {
		return nil
	}
	parent, err := st.ByID(ctx, userID, n.ParentID)
	if err != nil {
		return err
	}
	return st.Move(ctx, userID, id, parent.ParentID, parent.Position+1)
}

// MoveUp swaps a bullet with the sibling above it, or does nothing if it is
// already first.
func (st *Store) MoveUp(ctx context.Context, userID, id int64) error {
	n, err := st.ByID(ctx, userID, id)
	if err != nil {
		return err
	}
	if n.Position == 0 {
		return nil
	}
	return st.Move(ctx, userID, id, n.ParentID, n.Position-1)
}

// MoveDown swaps a bullet with the sibling below it. A bullet that is already
// last needs no special case: Move clamps the target position back to where
// the bullet already is.
func (st *Store) MoveDown(ctx context.Context, userID, id int64) error {
	n, err := st.ByID(ctx, userID, id)
	if err != nil {
		return err
	}
	return st.Move(ctx, userID, id, n.ParentID, n.Position+1)
}

// siblingAt fetches the child of parentID sitting at a given position.
func (st *Store) siblingAt(ctx context.Context, userID, parentID int64, pos int) (Node, error) {
	n, err := scanNode(st.db.QueryRowContext(ctx,
		`SELECT `+nodeColumns+`
		   FROM notes_nodes WHERE user_id = ? AND parent_id IS ? AND position = ?`,
		userID, parentArg(parentID), pos))
	if errors.Is(err, sql.ErrNoRows) {
		return Node{}, ErrNotFound
	}
	return n, err
}
```

- [ ] **Step 4: Run the tests to verify they pass**

```bash
go test ./internal/apps/notes/... -count=1 -run 'TestIndent|TestOutdent|TestMoveUp|TestKeyboard' -v
```

Expected: PASS, all seven tests.

- [ ] **Step 5: Commit**

```bash
git add internal/apps/notes
git commit -m "feat(notes): add indent, outdent and sibling reordering"
```

---

### Task 10: The randomised operation-sequence test

**Files:**
- Test: `internal/apps/notes/tree_test.go`

**Interfaces:**
- Consumes: every store method, plus `treeViolations` and `loadRawNodes` from Task 2. Produces nothing importable.

- [ ] **Step 1: Write the test**

This is the one test in N1 written after the code, because it exercises the
whole surface rather than driving one behaviour into existence.

Append to `internal/apps/notes/tree_test.go`:

```go
// TestRandomOperationSequencesPreserveInvariants applies a long, deterministic
// sequence of random operations across two users and checks I1-I4 after every
// single one.
//
// The tests above cover one operation on a known tree. Tree bugs are not like
// that: an indent that leaves a gap in position is invisible until the third
// move after it, when a clamp lands one place off and two bullets quietly swap.
// This test exists to catch the sequences nobody would think to write down.
//
// The seed is fixed so that a failure is reproducible, and the operation log is
// printed on failure so the sequence can be replayed by hand.
func TestRandomOperationSequencesPreserveInvariants(t *testing.T) {
	const steps = 500

	f := newFixture(t)
	ctx := context.Background()
	rng := rand.New(rand.NewPCG(0x0175, 0x5eed)) // any fixed pair; reproducibility is the point

	owned := map[int64][]int64{f.alice.ID: nil, f.bob.ID: nil}
	users := []int64{f.alice.ID, f.bob.ID}

	var opLog []string
	fail := func(format string, args ...any) {
		t.Fatalf("%s\n\noperations so far:\n  %s",
			fmt.Sprintf(format, args...), strings.Join(opLog, "\n  "))
	}
	// expected outcomes: nil, or one of the four sentinels. Anything else is
	// a real failure, not a rejected operation.
	record := func(op string, err error) {
		opLog = append(opLog, fmt.Sprintf("%s -> %v", op, err))
		switch {
		case err == nil,
			errors.Is(err, notes.ErrNotFound),
			errors.Is(err, notes.ErrCycle),
			errors.Is(err, notes.ErrTooDeep),
			errors.Is(err, notes.ErrInvalid):
		default:
			fail("%s returned an unexpected error: %v", op, err)
		}
	}

	for step := range steps {
		userID := users[rng.IntN(len(users))]
		mine := owned[userID]

		// A quarter of the time, act at the top level; otherwise pick one of
		// this user's bullets. Deleted ids stay in the slice on purpose, so
		// the ErrNotFound paths get exercised too.
		pick := func() int64 {
			if len(mine) == 0 || rng.IntN(4) == 0 {
				return notes.RootID
			}
			return mine[rng.IntN(len(mine))]
		}

		switch rng.IntN(9) {
		case 0, 1, 2: // weighted, so the tree grows faster than it shrinks
			parent := pick()
			n, err := f.store.Create(ctx, userID, parent, rng.IntN(6)-1,
				fmt.Sprintf("n%d", step), "")
			record(fmt.Sprintf("Create(user=%d, parent=%d)", userID, parent), err)
			if err == nil {
				owned[userID] = append(owned[userID], n.ID)
			}
		case 3:
			id := pick()
			record(fmt.Sprintf("Indent(user=%d, id=%d)", userID, id),
				f.store.Indent(ctx, userID, id))
		case 4:
			id := pick()
			record(fmt.Sprintf("Outdent(user=%d, id=%d)", userID, id),
				f.store.Outdent(ctx, userID, id))
		case 5:
			id := pick()
			record(fmt.Sprintf("MoveUp(user=%d, id=%d)", userID, id),
				f.store.MoveUp(ctx, userID, id))
		case 6:
			id := pick()
			record(fmt.Sprintf("MoveDown(user=%d, id=%d)", userID, id),
				f.store.MoveDown(ctx, userID, id))
		case 7:
			id, parent := pick(), pick()
			record(fmt.Sprintf("Move(user=%d, id=%d, parent=%d)", userID, id, parent),
				f.store.Move(ctx, userID, id, parent, rng.IntN(6)-1))
		case 8:
			id := pick()
			record(fmt.Sprintf("Delete(user=%d, id=%d)", userID, id),
				f.store.Delete(ctx, userID, id))
		}

		if v := treeViolations(loadRawNodes(t, f.db)); len(v) > 0 {
			fail("after step %d: %s", step, strings.Join(v, "; "))
		}
	}

	// A sanity check on the test itself: a run that produced nothing would
	// pass every assertion above while proving nothing.
	if n := len(loadRawNodes(t, f.db)); n < 20 {
		t.Fatalf("the sequence left only %d nodes; the generator is not exercising the store", n)
	}
}
```

Add these imports to `tree_test.go`:

```go
	"math/rand/v2"
	"strings"
```

- [ ] **Step 2: Run the test**

```bash
go test ./internal/apps/notes/... -count=1 -run TestRandomOperationSequences -v
```

Expected: PASS. If it fails, the printed operation log is the reproduction —
copy the failing operation into a new focused test in `tree_test.go` before
fixing anything, so the bug stays covered once the seed changes.

- [ ] **Step 3: Run the test under the race detector a few times**

```bash
go test ./internal/apps/notes/... -race -count=3 -run TestRandomOperationSequences
```

Expected: PASS. `-count=3` re-runs the same deterministic sequence; it is
checking that nothing depends on wall-clock time or map iteration order.

- [ ] **Step 4: Run the full check**

```bash
gofmt -l . && go vet ./... && go run honnef.co/go/tools/cmd/staticcheck@v0.8.0 ./... && go mod tidy && git diff --exit-code go.mod go.sum && go test ./... -race -count=1
```

Expected: everything passes, `gofmt -l .` prints nothing, and `go.mod`/`go.sum`
are unchanged — N1 adds no dependency.

- [ ] **Step 5: Commit and open the PR**

```bash
git add internal/apps/notes
git commit -m "test(notes): check tree invariants across random operation sequences"
git push -u origin feat/notes-n1-store
gh pr create --fill --title "ON Notes N1: schema and store"
```

---

## Definition of done

- [ ] `internal/apps/notes` contains the migration, `notes.go`, `store.go`, `tree.go` and four test files.
- [ ] Every store method in spec §5 exists: `Create`, `SetText`, `Indent`, `Outdent`, `MoveUp`, `MoveDown`, `Move`, `Delete`, `SetCollapsed`, plus the reads `ByID`, `Children`, `Ancestors`, `Outline`.
- [ ] Every mutation test calls `checkInvariants`, and `treeViolations` has its own tests proving it catches I1, I2 and I3 violations.
- [ ] `go test ./internal/arch/...` passes — no import boundary was crossed.
- [ ] `go.mod` and `go.sum` are unchanged.
- [ ] The full check command passes, and the PR is open against `main`.

**Not in N1, by design:** no `app.App` implementation, no routes, no templates,
no line in `registeredApps()`. The package is deliberately dead code that main
does not reference until N2 mounts it. That is what makes N1 reviewable on its
own — the whole PR is a data structure and its proofs.
