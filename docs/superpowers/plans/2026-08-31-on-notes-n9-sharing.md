# ON Notes N9 — Public Sharing Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let a bullet and its subtree be published as a read-only page reachable with no sign-in, via an unguessable link that the owner can revoke — spec §15.

**Architecture:** A `share_slug` column on `notes_nodes`, made unique by its own index (§4). Minting and revoking go through the same `Ops`/`Do` transaction machinery every other structural operation already uses, so a share or unshare that fires from a mid-edit outline row saves that row's pending text in the same write. The public page is a new, deliberately minimal template and a `Router.PublicFunc` route — the app's only public route — that never imports outline.html's editing markup, so a mutating control cannot leak onto it by accident.

**Tech Stack:** Go, `database/sql` against SQLite (`modernc.org/sqlite`), `html/template`, HTMX for the existing authenticated UI (the new public page has no JS need at all), `crypto/rand` for the slug.

## Global Constraints

- `share_slug` is nullable and made unique by a **separate index**, not an inline `UNIQUE` — SQLite rejects `UNIQUE` on a column added via `ALTER TABLE` on a `STRICT` table (spec §4, verified against SQLite already for this project).
- Re-sharing always mints a **fresh** slug; the old one must never work again (spec §15, "the same rule ON Paste already applies to snippets").
- The shared page requires no login, has **no breadcrumb** above the shared root, and **no link leads anywhere into the owner's private tree** (spec §15).
- Archived and completed **descendants** are excluded from the shared page (spec §15, §13). Per this plan's own resolved design question, the shared **root** itself is not filtered by its own archived/done state — sharing behaves the same way a direct zoom to an archived node already does (a live view, not a hard 404), and it is only the recursive descent beneath it that excludes archived/done nodes.
- The shared page always shows the **full subtree, fully expanded** — collapse state (`collapsed`) is not honoured, per this plan's resolved design question. This mirrors `Store.Export`'s own unfiltered descent, not `Store.Outline`'s collapse-aware one.
- A share slug is a **revocable credential**: the shared page must never be cached (`Cache-Control: no-store`) or indexed (`X-Robots-Tag: noindex`) — the same rule ON Paste's own `viewShared`/`rawShared` already apply.
- Apps never import each other (`internal/arch/arch_test.go`); `internal/apps/paste`'s sharing code (`store.go`'s `Share`/`Unshare`/`ByShareSlug`/`newShareSlug`, `paste.go`'s route registration, `handlers.go`'s `share`/`unshare`/`viewShared`) is this plan's direct precedent and is read for pattern only, never imported.
- Every existing invariant (I1–I4) is untouched by this chunk: sharing writes one column and never restructures the tree.

---

### Task 1: Schema, `Node.ShareSlug`, and the mint/revoke/lookup store surface

**Files:**
- Create: `internal/apps/notes/migrations/0005_share.sql`
- Modify: `internal/apps/notes/notes.go` (add `Node.ShareSlug`, `Node.Shared()`, the `shareSlugBytes` constant)
- Modify: `internal/apps/notes/store.go` (`nodeColumns`, `scanNode`)
- Modify: `internal/apps/notes/export.go` (`exportedNode` gains `ShareSlug`, `App.Export` populates it)
- Create: `internal/apps/notes/share.go` (`Ops.Share`, `Ops.Unshare`, `Store.Share`, `Store.Unshare`, `Store.ByShareSlug`, `newShareSlug`)
- Test: `internal/apps/notes/share_test.go`
- Test: `internal/apps/notes/export_test.go` (one addition)

**Interfaces:**
- Consumes: `Ops.update(ctx, op, query string, args ...any) error` (tree.go), `Store.Do(ctx, fn) error` (tree.go), `formatTime`/`parseTime` (store.go), `nodeColumns`/`scanNode`/`collectNodes` (store.go), `ErrNotFound` (notes.go).
- Produces (used by Task 2, 3, 4):
  - `Node.ShareSlug string`, `Node.Shared() bool`
  - `func (o *Ops) Share(ctx context.Context, userID, id int64) (string, error)`
  - `func (o *Ops) Unshare(ctx context.Context, userID, id int64) error`
  - `func (st *Store) Share(ctx context.Context, userID, id int64) (string, error)`
  - `func (st *Store) Unshare(ctx context.Context, userID, id int64) error`
  - `func (st *Store) ByShareSlug(ctx context.Context, slug string) (Node, error)`

- [ ] **Step 1: Write the migration**

```sql
-- internal/apps/notes/migrations/0005_share.sql
-- SQLite refuses UNIQUE on a column added via ALTER TABLE on a STRICT
-- table ("Cannot add a UNIQUE column"), verified against SQLite for this
-- project (spec §4) — the same reason paste_snippets' own share_slug is
-- UNIQUE inline instead: that table's column arrived in its original
-- CREATE TABLE, this one does not. The index below permits many NULLs,
-- since SQLite treats NULLs as distinct, so every not-yet-shared node can
-- carry one.
ALTER TABLE notes_nodes ADD COLUMN share_slug TEXT;
CREATE UNIQUE INDEX notes_nodes_share_slug_idx ON notes_nodes (share_slug);
```

- [ ] **Step 2: Run the existing test suite to confirm the migration applies cleanly**

Run: `go test ./internal/apps/notes/... -run TestMigrations -v`

If there is no test named exactly that, run the package's full suite instead — every store test opens a fresh migrated database, so a broken migration fails everything:

Run: `go test ./internal/apps/notes/... -v 2>&1 | tail -30`

Expected: builds and runs (some tests may not exist yet — that's fine at this point; there must be no migration/SQL error).

- [ ] **Step 3: Add `Node.ShareSlug`, `Node.Shared()`, and `shareSlugBytes` to notes.go**

In `internal/apps/notes/notes.go`, add to the `const` block (after `MaxPasteTextBytes`):

```go
	// shareSlugBytes is 16 bytes, 128 bits, base64url encoded to 22
	// characters — spec §15: "an unguessable slug". Mirrors
	// internal/apps/paste/store.go's own shareSlugBytes exactly; apps
	// never import each other, so this is an independent constant with
	// the same justification, not a shared symbol.
	shareSlugBytes = 16
```

In the `Node` struct, add a field after `Archived`:

```go
	// ShareSlug is "" when the bullet is not shared — spec §15. Unlike
	// Done/DueOn/Archived it is not itself an *_at timestamp's Go
	// projection: the slug's actual value is what a visitor's URL must
	// match, so it is carried as-is rather than reduced to a bool.
	ShareSlug string
```

Add a method after `DisplayTitleHTML`:

```go
// Shared reports whether the bullet currently has a live public link.
func (n Node) Shared() bool { return n.ShareSlug != "" }
```

- [ ] **Step 4: Add `share_slug` to `nodeColumns` and `scanNode` in store.go**

In `internal/apps/notes/store.go`, change:

```go
const nodeColumns = `id, user_id, parent_id, position, title, note, collapsed, created_at, updated_at, done_at, due_on, archived_at`
```

to:

```go
const nodeColumns = `id, user_id, parent_id, position, title, note, collapsed, created_at, updated_at, done_at, due_on, archived_at, share_slug`
```

In `scanNode`, add a `shareSlug sql.NullString` local and scan destination, and set `n.ShareSlug`:

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
		shareSlug  sql.NullString
	)
	dest := append([]any{
		&n.ID, &n.UserID, &parent, &n.Position, &n.Title, &n.Note,
		&n.Collapsed, &createdAt, &updatedAt, &doneAt, &dueOn, &archivedAt, &shareSlug,
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
	n.ShareSlug = shareSlug.String

	if n.CreatedAt, err = parseTime(createdAt); err != nil {
		return Node{}, err
	}
	if n.UpdatedAt, err = parseTime(updatedAt); err != nil {
		return Node{}, err
	}
	return n, nil
}
```

`parentColumns`/`childColumns` are computed from `nodeColumns` by `aliasNodeColumns`, so they pick up `share_slug` automatically — no change needed there.

- [ ] **Step 5: Run the package build to catch every query whose column count now mismatches**

Run: `go build ./internal/apps/notes/...`

Expected: builds cleanly. `nodeColumns`/`childColumns`/`parentColumns` are always selected as a single interpolated list immediately followed by the query's own extra columns (`depth`, `has_children`, ...), so widening the list does not shift any existing `extra` scan destination — only `scanNode` itself needed a change.

- [ ] **Step 6: Add `ShareSlug` to the JSON export**

In `internal/apps/notes/export.go`, `exportedNode` is documented as "the whole row" (spec §14, "JSON is the safety net") — a share link is part of that row, and omitting it would silently lose a live share on restore from backup. Add a field:

```go
type exportedNode struct {
	ID        int64     `json:"id"`
	ParentID  int64     `json:"parent_id"`
	Position  int       `json:"position"`
	Title     string    `json:"title"`
	Note      string    `json:"note"`
	Collapsed bool      `json:"collapsed"`
	Done      bool      `json:"done"`
	DueOn     string    `json:"due_on,omitempty"`
	Archived  bool      `json:"archived"`
	// ShareSlug is included because this type documents itself as "the
	// whole row" — spec §14. This is the account owner's own backup, not
	// something handed to anyone else, so including a live credential
	// here is not a new exposure.
	ShareSlug string    `json:"share_slug,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}
```

And in `App.Export`, add `ShareSlug: n.ShareSlug` to the literal:

```go
		out.Nodes = append(out.Nodes, exportedNode{
			ID: n.ID, ParentID: n.ParentID, Position: n.Position,
			Title: n.Title, Note: n.Note, Collapsed: n.Collapsed,
			Done: n.Done, DueOn: n.DueOn, Archived: n.Archived,
			ShareSlug: n.ShareSlug,
			CreatedAt: n.CreatedAt, UpdatedAt: n.UpdatedAt,
		})
```

- [ ] **Step 7: Write the failing test for the JSON export addition**

Append to `internal/apps/notes/export_test.go` (package `notes_test`; check the file's existing imports — it already imports `encoding/json`, `context`, `testing`, and `"github.com/iliafrenkel/on-suite/internal/apps/notes"` for the other export tests):

```go
// TestJSONExportIncludesTheShareSlug pins exportedNode's own claim to be
// "the whole row" (its doc comment, export.go) now that the row has a
// share_slug column.
func TestJSONExportIncludesTheShareSlug(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	n := f.mk(t, notes.RootID, "shared")
	slug, err := f.store.Share(ctx, f.alice.ID, n.ID)
	if err != nil {
		t.Fatal(err)
	}

	out, err := notes.New().Export(ctx, f.db, f.alice.ID)
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(out)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), slug) {
		t.Errorf("JSON export does not contain the share slug %q:\n%s", slug, data)
	}
}
```

Check the existing export_test.go for whether `strings` is already imported and whether the fixture exposes `f.db`; if the file's fixture instead calls the store through an already-open `*sql.DB` field with a different name (check the other `TestNotesImplementsTheExporterInterface`-style test in the same file for the exact call shape used against `notes.New().Export`), match that exact call rather than inventing a new one. Add `"strings"` to imports if it is not already there.

- [ ] **Step 8: Run it to verify it fails**

Run: `go test ./internal/apps/notes/... -run TestJSONExportIncludesTheShareSlug -v`
Expected: FAIL — `f.store.Share` and `notes.New().Export`'s output containing a share slug do not exist yet.

- [ ] **Step 9: Create share.go with the mint/revoke/lookup surface**

```go
// internal/apps/notes/share.go
package notes

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
)

// Share mints a fresh, unguessable slug for id and returns it — spec §15.
//
// Re-sharing always generates a new slug rather than reusing the old one,
// so a link that was revoked can never come back — the same rule ON
// Paste already applies to snippets (internal/apps/paste/store.go's own
// Share).
func (o *Ops) Share(ctx context.Context, userID, id int64) (string, error) {
	slug, err := newShareSlug()
	if err != nil {
		return "", err
	}
	if err := o.update(ctx, "share",
		`UPDATE notes_nodes SET share_slug = ?, updated_at = ? WHERE id = ? AND user_id = ?`,
		slug, formatTime(o.now()), id, userID); err != nil {
		return "", err
	}
	return slug, nil
}

// Unshare revokes id's public link.
func (o *Ops) Unshare(ctx context.Context, userID, id int64) error {
	return o.update(ctx, "unshare",
		`UPDATE notes_nodes SET share_slug = NULL, updated_at = ? WHERE id = ? AND user_id = ?`,
		formatTime(o.now()), id, userID)
}

// Share mints a fresh slug for id, in a transaction of its own. See
// Ops.Share.
func (st *Store) Share(ctx context.Context, userID, id int64) (string, error) {
	var slug string
	err := st.Do(ctx, func(o *Ops) error {
		s, err := o.Share(ctx, userID, id)
		slug = s
		return err
	})
	return slug, err
}

// Unshare revokes id's public link, in a transaction of its own. See
// Ops.Unshare.
func (st *Store) Unshare(ctx context.Context, userID, id int64) error {
	return st.Do(ctx, func(o *Ops) error { return o.Unshare(ctx, userID, id) })
}

// ByShareSlug fetches a node by its public slug, with no owner check —
// possessing the slug is the authorisation, exactly as
// internal/apps/paste/store.go's own ByShareSlug documents. "" and any
// slug nobody has minted both answer ErrNotFound, indistinguishably.
func (st *Store) ByShareSlug(ctx context.Context, slug string) (Node, error) {
	if slug == "" {
		return Node{}, ErrNotFound
	}
	n, err := scanNode(st.db.QueryRowContext(ctx,
		`SELECT `+nodeColumns+` FROM notes_nodes WHERE share_slug = ?`, slug))
	if errors.Is(err, sql.ErrNoRows) {
		return Node{}, ErrNotFound
	}
	return n, err
}

// newShareSlug mirrors internal/apps/paste/store.go's own newShareSlug
// exactly; apps never import each other, so this is an independent
// implementation with the same justification, not a shared symbol.
func newShareSlug() (string, error) {
	buf := make([]byte, shareSlugBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("notes: generate share slug: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}
```

- [ ] **Step 10: Write the store-level tests**

Create `internal/apps/notes/share_test.go`:

```go
package notes_test

import (
	"context"
	"errors"
	"testing"

	"github.com/iliafrenkel/on-suite/internal/apps/notes"
)

func TestShareMintsAnUnguessableSlug(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	n := f.mk(t, notes.RootID, "shared")

	slug, err := f.store.Share(ctx, f.alice.ID, n.ID)
	if err != nil {
		t.Fatalf("Share: %v", err)
	}
	if len(slug) < 20 {
		t.Errorf("slug %q is too short to be unguessable", slug)
	}

	got, err := f.store.ByID(ctx, f.alice.ID, n.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.ShareSlug != slug {
		t.Errorf("ByID's ShareSlug = %q, want %q", got.ShareSlug, slug)
	}
	if !got.Shared() {
		t.Error("Shared() is false right after Share")
	}
}

func TestByShareSlugFindsTheSharedNode(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	n := f.mk(t, notes.RootID, "shared")
	slug, err := f.store.Share(ctx, f.alice.ID, n.ID)
	if err != nil {
		t.Fatal(err)
	}

	got, err := f.store.ByShareSlug(ctx, slug)
	if err != nil {
		t.Fatalf("ByShareSlug: %v", err)
	}
	if got.ID != n.ID {
		t.Errorf("ByShareSlug returned node %d, want %d", got.ID, n.ID)
	}
	if got.UserID != f.alice.ID {
		t.Errorf("ByShareSlug's UserID = %d, want %d", got.UserID, f.alice.ID)
	}
}

func TestByShareSlugRejectsEmptyAndUnknown(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	for _, slug := range []string{"", "never-minted"} {
		if _, err := f.store.ByShareSlug(ctx, slug); !errors.Is(err, notes.ErrNotFound) {
			t.Errorf("ByShareSlug(%q) = %v, want ErrNotFound", slug, err)
		}
	}
}

func TestUnshareClearsTheSlug(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	n := f.mk(t, notes.RootID, "shared")
	slug, err := f.store.Share(ctx, f.alice.ID, n.ID)
	if err != nil {
		t.Fatal(err)
	}

	if err := f.store.Unshare(ctx, f.alice.ID, n.ID); err != nil {
		t.Fatalf("Unshare: %v", err)
	}

	got, err := f.store.ByID(ctx, f.alice.ID, n.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Shared() {
		t.Error("the node is still shared after Unshare")
	}
	if _, err := f.store.ByShareSlug(ctx, slug); !errors.Is(err, notes.ErrNotFound) {
		t.Errorf("ByShareSlug(revoked slug) = %v, want ErrNotFound", err)
	}
}

// TestReSharingMintsAFreshSlugAndKillsTheOldOne is the point of choosing a
// revocable share model — spec §15.
func TestReSharingMintsAFreshSlugAndKillsTheOldOne(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	n := f.mk(t, notes.RootID, "shared")

	first, err := f.store.Share(ctx, f.alice.ID, n.ID)
	if err != nil {
		t.Fatal(err)
	}
	second, err := f.store.Share(ctx, f.alice.ID, n.ID)
	if err != nil {
		t.Fatal(err)
	}
	if second == first {
		t.Fatal("re-sharing reissued the same slug")
	}
	if _, err := f.store.ByShareSlug(ctx, first); !errors.Is(err, notes.ErrNotFound) {
		t.Error("the old slug still resolves after re-sharing")
	}
	got, err := f.store.ByShareSlug(ctx, second)
	if err != nil || got.ID != n.ID {
		t.Errorf("ByShareSlug(second) = %+v, %v", got, err)
	}
}

func TestShareRejectsAnotherUsersNode(t *testing.T) {
	f := newFixture(t)
	n := f.mk(t, notes.RootID, "bob's") // seeded as alice by newFixture's mk; use mkFor for bob

	_, err := f.store.Share(context.Background(), f.bob.ID, n.ID)
	if !errors.Is(err, notes.ErrNotFound) {
		t.Fatalf("Share on another user's node = %v, want ErrNotFound", err)
	}
}

func TestUnshareRejectsAnotherUsersNode(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	n := f.mkFor(t, f.bob.ID, notes.RootID, "bob's")
	if _, err := f.store.Share(ctx, f.bob.ID, n.ID); err != nil {
		t.Fatal(err)
	}

	err := f.store.Unshare(ctx, f.alice.ID, n.ID)
	if !errors.Is(err, notes.ErrNotFound) {
		t.Fatalf("Unshare on another user's node = %v, want ErrNotFound", err)
	}
}
```

Before running, check `internal/apps/notes/tree_test.go` (or wherever `newFixture`/`f.mk`/`f.mkFor` are defined) for `mkFor`'s exact signature — it is referenced elsewhere in this package's tests per this plan's own Global Constraints on reusing existing helpers. If the fixture has no `mkFor(t, userID, parentID, title)` helper, use whatever existing helper creates a node for a specific user (e.g. a fixture method already used by `TestSetArchivedRejectsAnotherUsersNode`'s sibling tests for "another user's node") and adjust `TestShareRejectsAnotherUsersNode` and `TestUnshareRejectsAnotherUsersNode` to call it instead — do not invent a new fixture method in this test file.

- [ ] **Step 11: Run the new tests to verify they fail correctly, then pass**

Run: `go test ./internal/apps/notes/... -run 'TestShare|TestUnshare|TestByShareSlug|TestReSharing|TestJSONExportIncludesTheShareSlug' -v`

Expected first: FAIL (methods don't exist / compile errors are fine as "fails" at this stage if Step 9 hasn't landed yet — but since these steps are written in order, after Step 9 the package must build). After Step 9, expected: every test above PASSes.

- [ ] **Step 12: Run the full package suite and `go vet`**

Run: `go build ./... && go vet ./... && go test ./internal/apps/notes/... -race`

Expected: PASS, no vet warnings.

- [ ] **Step 13: Commit**

```bash
git add internal/apps/notes/migrations/0005_share.sql internal/apps/notes/notes.go \
        internal/apps/notes/store.go internal/apps/notes/export.go \
        internal/apps/notes/share.go internal/apps/notes/share_test.go \
        internal/apps/notes/export_test.go
git commit -m "feat(notes): add share_slug and the mint/revoke/lookup store surface"
```

---

### Task 2: The read-only subtree query and its row type

**Files:**
- Modify: `internal/apps/notes/share.go` (add `Store.SharedSubtree`, `sharedRow`, `nestShared`, `sharedView`)
- Test: `internal/apps/notes/share_test.go` (append)

**Interfaces:**
- Consumes: `Node`, `nodeColumns`, `childColumns`, `scanNode`, `MaxDepth` (store.go/notes.go), `Render` (markdown.go — already used by `nest` in view.go for `RenderedTitle`/`RenderedNote`).
- Produces (used by Task 4):
  - `func (st *Store) SharedSubtree(ctx context.Context, userID, rootID int64) ([]Node, error)`
  - `type sharedRow struct { Node; Children []*sharedRow; RenderedTitle, RenderedNote template.HTML }`
  - `func nestShared(flat []Node) []*sharedRow`
  - `type sharedView struct { Root Node; Rows []*sharedRow }`

- [ ] **Step 1: Write the failing tests**

Append to `internal/apps/notes/share_test.go`:

```go
func TestSharedSubtreeExcludesArchivedAndDoneDescendants(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	root := f.mk(t, notes.RootID, "root")
	kept := f.mk(t, root.ID, "kept")
	archived := f.mk(t, root.ID, "archived")
	done := f.mk(t, root.ID, "done")

	if err := f.store.SetArchived(ctx, f.alice.ID, archived.ID, true); err != nil {
		t.Fatal(err)
	}
	if err := f.store.SetDone(ctx, f.alice.ID, done.ID, true); err != nil {
		t.Fatal(err)
	}

	got, err := f.store.SharedSubtree(ctx, f.alice.ID, root.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != kept.ID {
		t.Fatalf("SharedSubtree = %+v, want only %q", got, kept.ID)
	}
}

func TestSharedSubtreeIgnoresCollapsedState(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	root := f.mk(t, notes.RootID, "root")
	child := f.mk(t, root.ID, "child")
	f.mk(t, child.ID, "grandchild")

	if err := f.store.SetCollapsed(ctx, f.alice.ID, child.ID, true); err != nil {
		t.Fatal(err)
	}

	got, err := f.store.SharedSubtree(ctx, f.alice.ID, root.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("SharedSubtree = %+v, want 2 rows despite the collapsed child", got)
	}
}

func TestSharedSubtreeDoesNotIncludeTheRootItself(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	root := f.mk(t, notes.RootID, "root")
	child := f.mk(t, root.ID, "child")

	got, err := f.store.SharedSubtree(ctx, f.alice.ID, root.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != child.ID {
		t.Fatalf("SharedSubtree = %+v, want only the child %d, not the root", got, child.ID)
	}
}

func TestSharedSubtreeOrdersPreOrder(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	root := f.mk(t, notes.RootID, "root")
	a := f.mk(t, root.ID, "a")
	f.mk(t, a.ID, "a1")
	f.mk(t, root.ID, "b")

	got, err := f.store.SharedSubtree(ctx, f.alice.ID, root.ID)
	if err != nil {
		t.Fatal(err)
	}
	var titles []string
	for _, n := range got {
		titles = append(titles, n.Title)
	}
	want := []string{"a", "a1", "b"}
	if !equalStringsShare(titles, want) {
		t.Fatalf("order = %v, want %v", titles, want)
	}
}

func equalStringsShare(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
```

Check whether `internal/apps/notes/tree_test.go` (or wherever store-level tests already live in package `notes_test`) already exports an `equalStrings` helper usable here; the handler tests' `equalStrings` (in `handlers_test.go`) is in the same `notes_test` package, so if it is visible from this file too, delete `equalStringsShare` above and call `equalStrings` directly instead — do not keep a duplicate. (`handlers_test.go` and `share_test.go` are both `package notes_test` in the same directory, so a top-level function defined in one is visible in the other; confirm this by checking `handlers_test.go`'s package clause before deciding.)

- [ ] **Step 2: Run to verify they fail**

Run: `go test ./internal/apps/notes/... -run TestSharedSubtree -v`
Expected: FAIL — `Store.SharedSubtree` does not exist yet.

- [ ] **Step 3: Implement `SharedSubtree`, `sharedRow`, `nestShared`, `sharedView`**

Append to `internal/apps/notes/share.go` (add `"html/template"` to the import block):

```go
// SharedSubtree returns every non-archived, non-done descendant of rootID,
// in pre-order with Depth relative to rootID — the query behind
// GET /notes/s/{slug} (Task 4). rootID is never RootID here: a slug always
// names one real, already-shared node, unlike Outline/Export which also
// handle the implicit top level.
//
// Unlike Outline (store.go) this does not stop at a collapsed node — per
// this plan's resolved design question, a share link means "here's
// everything under this bullet," and the page has no interactivity to
// un-collapse anything anyway. Unlike Export (export.go) it DOES exclude
// archived and done descendants — spec §13/§15 — because unlike a backup,
// a share page is something the owner is showing another person, and
// showing them "finished" or "put away" content there is exactly what
// those two states mean to hide elsewhere in the app. The root itself is
// deliberately not filtered here: the caller already resolved it via
// ByShareSlug, and per this plan's own resolved design question, sharing
// stays live even if the root is later archived or done, the same way a
// direct zoom to an archived node already renders rather than 404s.
//
// The owner-matching mirrors Outline's and Export's own, for the same
// defence-in-depth reason given on both: parent_id is a plain foreign key,
// not a composite (parent_id, user_id) one, so matching user_id at every
// step of the descent is what keeps a broken invariant I2 from leaking
// another household's bullets onto a share page.
func (st *Store) SharedSubtree(ctx context.Context, userID, rootID int64) ([]Node, error) {
	rows, err := st.db.QueryContext(ctx,
		`WITH RECURSIVE tree AS (
		     SELECT `+nodeColumns+`, 0 AS depth, printf('%08d', position) AS path
		       FROM notes_nodes
		      WHERE user_id = ? AND parent_id = ? AND archived_at IS NULL AND done_at IS NULL
		   UNION ALL
		     SELECT `+childColumns+`,
		            t.depth + 1, t.path || '/' || printf('%08d', c.position)
		       FROM notes_nodes c JOIN tree t ON c.parent_id = t.id
		      WHERE c.user_id = t.user_id AND c.archived_at IS NULL AND c.done_at IS NULL
		        AND t.depth + 1 <= ?
		 )
		 SELECT `+nodeColumns+`, depth FROM tree ORDER BY path`,
		userID, rootID, MaxDepth)
	if err != nil {
		return nil, fmt.Errorf("notes: shared subtree of %d: %w", rootID, err)
	}
	defer func() { _ = rows.Close() }()

	var out []Node
	for rows.Next() {
		var depth int
		n, err := scanNode(rows, &depth)
		if err != nil {
			return nil, err
		}
		n.Depth = depth
		out = append(out, n)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("notes: shared subtree: %w", err)
	}
	return out, nil
}

// sharedRow is one bullet on the public share page — a small, deliberate
// subset of outline.html's outlineRow (view.go): no RootID, no
// CSRFToken, no Last, no Overdue, because the shared page has no forms,
// no move buttons and no due-date urgency styling at all. Duplicating
// nest's own tree-building shape here (nestShared below) rather than
// generalising nest() to serve both is a deliberate YAGNI call: the two
// row types diverge enough (outlineRow carries five fields sharedRow has
// no use for) that a shared generic would need type parameters or extra
// options for a single caller, which is not simpler than two ~15-line
// functions.
type sharedRow struct {
	Node
	Children      []*sharedRow
	RenderedTitle template.HTML
	RenderedNote  template.HTML
}

// nestShared turns SharedSubtree's flat pre-order slice into the tree the
// shared page template renders as nested <ul>s — see nest (view.go),
// whose ordering assumption (a parent immediately precedes its subtree,
// depth rises by at most one row to the next) this relies on identically.
func nestShared(flat []Node) []*sharedRow {
	var top []*sharedRow
	open := make([]*sharedRow, 0, MaxDepth+1)

	for _, n := range flat {
		row := &sharedRow{Node: n, RenderedTitle: Render(n.Title), RenderedNote: Render(n.Note)}

		switch d := n.Depth; {
		case d == 0:
			top = append(top, row)
			open = open[:0]
		case d > 0 && d <= len(open):
			parent := open[d-1]
			parent.Children = append(parent.Children, row)
			open = open[:d]
		default:
			continue
		}
		open = append(open, row)
	}
	return top
}

// sharedView is what templates/shared.html renders.
type sharedView struct {
	Root Node
	Rows []*sharedRow
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/apps/notes/... -run TestSharedSubtree -v`
Expected: PASS.

- [ ] **Step 5: Run the full package suite**

Run: `go build ./... && go vet ./... && go test ./internal/apps/notes/... -race`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/apps/notes/share.go internal/apps/notes/share_test.go
git commit -m "feat(notes): add the fully-expanded, archive/done-filtered subtree query"
```

---

### Task 3: `mutateThen`, the share/unshare routes, and the outline UI

**Files:**
- Modify: `internal/apps/notes/handlers.go` (`mutate` → thin wrapper around new `mutateThen`; add `share`, `unshare`)
- Modify: `internal/apps/notes/app.go` (routes + route-map comment)
- Modify: `internal/apps/notes/templates/outline.html` (row-menu Share/Unshare buttons; zoomed banner showing the link)
- Test: `internal/apps/notes/handlers_test.go` (append)

**Interfaces:**
- Consumes: `Ops.Share`/`Ops.Unshare` (Task 1), `mutation` struct, `formID`, `outlinePath`, `renderOutlineFragment`, `web.IsHTMX`, `web.CSRFToken` (all handlers.go, pre-existing).
- Produces (used nowhere later in this plan, but is the deliverable Task 4's banner display depends on being visible on `/notes/{id}`): `POST /notes/{id}/share`, `POST /notes/{id}/unshare`; `outlineView.ShareURL string` (empty unless zoomed to a shared node).

- [ ] **Step 1: Write the failing handler tests**

Append to `internal/apps/notes/handlers_test.go`:

```go
func TestShareMintsALinkAndRedirectsToTheSharedNode(t *testing.T) {
	s := newServer(t)
	id := s.seed(t, s.Alice, notes.RootID, "shared")

	s.Submit(t, s.Alice, "/notes/"+itoa(id)+"/share", url.Values{"root": {"0"}}, "/notes/"+itoa(id))

	got, err := s.Store.ByID(context.Background(), s.Alice.User.ID, id)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Shared() {
		t.Fatal("the bullet is not shared after POST .../share")
	}
}

func TestShareSavesTheFocusedTextInTheSameTransaction(t *testing.T) {
	s := newServer(t)
	id := s.seed(t, s.Alice, notes.RootID, "old title")

	s.Submit(t, s.Alice, "/notes/"+itoa(id)+"/share", url.Values{
		"root": {"0"}, "focus_id": {itoa(id)}, "title": {"new title"}, "note": {"a note"},
	}, "/notes/"+itoa(id))

	got, err := s.Store.ByID(context.Background(), s.Alice.User.ID, id)
	if err != nil {
		t.Fatal(err)
	}
	if got.Title != "new title" || got.Note != "a note" {
		t.Fatalf("got Title=%q Note=%q, want the text saved alongside the share", got.Title, got.Note)
	}
}

func TestUnshareRevokesTheLinkAndRedirectsToTheSharedNode(t *testing.T) {
	s := newServer(t)
	id := s.seed(t, s.Alice, notes.RootID, "shared")
	if _, err := s.Store.Share(context.Background(), s.Alice.User.ID, id); err != nil {
		t.Fatal(err)
	}

	s.Submit(t, s.Alice, "/notes/"+itoa(id)+"/unshare", url.Values{"root": {"0"}}, "/notes/"+itoa(id))

	got, err := s.Store.ByID(context.Background(), s.Alice.User.ID, id)
	if err != nil {
		t.Fatal(err)
	}
	if got.Shared() {
		t.Fatal("the bullet is still shared after POST .../unshare")
	}
}

func TestShareOnAnotherUsersBulletIs404(t *testing.T) {
	s := newServer(t)
	id := s.seed(t, s.Bob, notes.RootID, "bob's")

	rec := s.Post(t, s.Alice, "/notes/"+itoa(id)+"/share", url.Values{"root": {"0"}})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestUnshareOnAnotherUsersBulletIs404(t *testing.T) {
	s := newServer(t)
	id := s.seed(t, s.Bob, notes.RootID, "bob's")
	if _, err := s.Store.Share(context.Background(), s.Bob.User.ID, id); err != nil {
		t.Fatal(err)
	}

	rec := s.Post(t, s.Alice, "/notes/"+itoa(id)+"/unshare", url.Values{"root": {"0"}})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestShareRespondsWithTheOutlineFragmentForHTMX(t *testing.T) {
	s := newServer(t)
	id := s.seed(t, s.Alice, notes.RootID, "shared")

	rec := s.PostHX(t, s.Alice, "/notes/"+itoa(id)+"/share", url.Values{"root": {"0"}})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}
}

func TestOutlineMenuHasAShareAction(t *testing.T) {
	s := newServer(t)
	s.seed(t, s.Alice, notes.RootID, "a bullet")

	doc := s.Get(t, s.Alice, "/notes/")
	found := false
	for _, btn := range doc.QueryAll("button.quiet") {
		if htmlassert.Text(btn) == "Share" {
			found = true
		}
	}
	if !found {
		t.Fatal(`the outline menu has no "Share" action`)
	}
}

func TestOutlineMenuShowsStopSharingWhenAlreadyShared(t *testing.T) {
	s := newServer(t)
	id := s.seed(t, s.Alice, notes.RootID, "a bullet")
	if _, err := s.Store.Share(context.Background(), s.Alice.User.ID, id); err != nil {
		t.Fatal(err)
	}

	doc := s.Get(t, s.Alice, "/notes/")
	var texts []string
	for _, btn := range doc.QueryAll("button.quiet") {
		texts = append(texts, htmlassert.Text(btn))
	}
	if !containsShare(texts, "Stop sharing") {
		t.Fatalf("menu button texts = %v, want one reading %q", texts, "Stop sharing")
	}
	if containsShare(texts, "Share") {
		t.Fatalf("menu button texts = %v, still shows plain %q for an already-shared bullet", texts, "Share")
	}
}

func containsShare(texts []string, want string) bool {
	for _, t := range texts {
		if t == want {
			return true
		}
	}
	return false
}

func TestZoomedBannerShowsTheShareLinkWhenShared(t *testing.T) {
	s := newServer(t)
	id := s.seed(t, s.Alice, notes.RootID, "shared")
	slug, err := s.Store.Share(context.Background(), s.Alice.User.ID, id)
	if err != nil {
		t.Fatal(err)
	}

	doc := s.Get(t, s.Alice, "/notes/"+itoa(id))
	link := doc.MustHave(".notice a")
	if got, _ := htmlassert.Attr(link, "href"); got != "/notes/s/"+slug {
		t.Errorf("share link href = %q, want %q", got, "/notes/s/"+slug)
	}
	doc.MustHave("[data-copy-link]")
}

func TestZoomedBannerShowsNoShareLinkWhenNotShared(t *testing.T) {
	s := newServer(t)
	id := s.seed(t, s.Alice, notes.RootID, "not shared")

	doc := s.Get(t, s.Alice, "/notes/"+itoa(id))
	doc.MustNotHave(".notice")
}

func TestTopLevelOutlineShowsNoShareBanner(t *testing.T) {
	s := newServer(t)
	s.seed(t, s.Alice, notes.RootID, "a bullet")

	doc := s.Get(t, s.Alice, "/notes/")
	doc.MustNotHave(".notice")
}
```

Also extend the existing `TestEveryMutationRequiresSignIn`'s `paths` slice (handlers_test.go) to include the two new routes, matching its existing entries' shape exactly:

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
		"/notes/" + itoa(id) + "/paste",
		"/notes/" + itoa(id) + "/share",
		"/notes/" + itoa(id) + "/unshare",
	}
```

- [ ] **Step 2: Run to verify the new tests fail**

Run: `go test ./internal/apps/notes/... -run 'TestShare|TestUnshare|TestOutlineMenuHasAShareAction|TestOutlineMenuShowsStopSharing|TestZoomedBanner|TestTopLevelOutlineShowsNoShareBanner' -v`
Expected: FAIL / compile errors — the routes and template markup don't exist yet.

- [ ] **Step 3: Refactor `mutate` into `mutateThen` in handlers.go**

Replace the existing `mutate` function with:

```go
// mutate is spec §7's single write. It sends a plain (non-HTMX) request
// back to the zoom it came from — see mutateThen for the one pair of
// routes that need something else.
func (a *App) mutate(w http.ResponseWriter, r *http.Request, op func(context.Context, *Ops, mutation) error) {
	a.mutateThen(w, r, op, false)
}

// mutateThen is mutate with the plain-request redirect target made
// explicit. Every route before share and unshare (this task) wants
// mutate's original behaviour — redirectToSelf false, sending a JS-off
// browser back to the zoom the request came from (root). share and
// unshare are the only two operations where that is the wrong place to
// land: the whole point of sharing is to show the resulting link, and the
// only page that shows it is the shared node's own zoomed view (its
// banner, this task's outline.html change) — not whatever page the row
// happened to be sitting on when Share was clicked.
//
// This does not change the HTMX branch: an HTMX swap only ever replaces
// #outline's own content inside the page the browser already has, so
// rendering some other root's rows into it would contradict that page's
// still-unchanged breadcrumb and heading. An HTMX-driven share therefore
// still refreshes root's own outline in place, exactly like every other
// row action — which is also why this task's Share/Unshare buttons carry
// no hx-post/hx-target at all (see outline.html): without a real
// navigation, there is no page left carrying a banner for the newly
// shared link to appear on.
//
// op must reach the database only through the *Ops it is handed — see
// mutate's own original doc comment for why (SetMaxOpenConns(1)); that
// requirement is unchanged here.
func (a *App) mutateThen(w http.ResponseWriter, r *http.Request, op func(context.Context, *Ops, mutation) error, redirectToSelf bool) {
	userID, ok := a.userID(w, r)
	if !ok {
		return
	}

	root, ok := formID(r, "root")
	if !ok {
		a.deps.Errors.Status(w, r, http.StatusBadRequest)
		return
	}
	focusID, ok := formID(r, "focus_id")
	if !ok {
		a.deps.Errors.Status(w, r, http.StatusBadRequest)
		return
	}
	title, note := r.PostFormValue("title"), r.PostFormValue("note")

	var nodeID int64 = RootID
	if raw := r.PathValue("id"); raw != "" {
		id, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || id <= 0 {
			a.deps.Errors.Status(w, r, http.StatusNotFound)
			return
		}
		nodeID = id
	}

	m := mutation{UserID: userID, NodeID: nodeID, FocusID: focusID, Root: root}
	ctx := r.Context()

	err := a.store.Do(ctx, func(o *Ops) error {
		if m.FocusID != RootID {
			if err := o.SetText(ctx, m.UserID, m.FocusID, title, note); err != nil {
				return err
			}
		}
		return op(ctx, o, m)
	})
	if err != nil {
		a.fail(w, r, err)
		return
	}

	if web.IsHTMX(r) {
		a.renderOutlineFragment(w, r, userID, root, showCompletedFrom(r))
		return
	}
	target := root
	if redirectToSelf {
		target = nodeID
	}
	http.Redirect(w, r, outlinePath(target), http.StatusSeeOther)
}
```

- [ ] **Step 4: Add the `share`/`unshare` handlers to handlers.go**

Add after `paste` (the last function in the file):

```go
// share mints a fresh, unguessable link for a bullet and its subtree —
// spec §15. It goes through mutateThen exactly like every other row-menu
// action, so any unsaved text in the row's own title/note field is saved
// in the same transaction, and redirects to the shared node's own zoomed
// page (redirectToSelf: true) rather than back to root — see mutateThen's
// own doc comment for why.
//
// Re-sharing an already-shared bullet mints a new slug, so a revoked
// link can never come back — the same rule ON Paste already applies to
// snippets (internal/apps/paste/store.go's own Share).
func (a *App) share(w http.ResponseWriter, r *http.Request) {
	a.mutateThen(w, r, func(ctx context.Context, o *Ops, m mutation) error {
		_, err := o.Share(ctx, m.UserID, m.NodeID)
		return err
	}, true)
}

// unshare revokes a bullet's public link. See share for why this also
// goes through mutateThen with redirectToSelf: true, rather than forking
// to a separate non-mutate path the way archive's restore direction does
// (archive.go's restore lives on a different page — /notes/archive —
// with no row text of its own to save; share and unshare both fire from
// the same outline row, so both need mutate's text-saving behaviour).
func (a *App) unshare(w http.ResponseWriter, r *http.Request) {
	a.mutateThen(w, r, func(ctx context.Context, o *Ops, m mutation) error {
		return o.Unshare(ctx, m.UserID, m.NodeID)
	}, true)
}
```

- [ ] **Step 5: Register the routes in app.go**

In `internal/apps/notes/app.go`, add two lines to `Mount` after `r.HandleFunc("POST /{id}/paste", a.paste)`:

```go
	r.HandleFunc("POST /{id}/share", a.share)
	r.HandleFunc("POST /{id}/unshare", a.unshare)
```

Update the route-map comment above `Mount` to add:

```go
//	POST /notes/{id}/share, POST /notes/{id}/unshare
//	                         N9's share links
```

placed in the list alongside the other `/{id}/...` mutation routes it already documents.

- [ ] **Step 6: Wire `outlineView.ShareURL` and the row's `ShareSlug` into renderOutline**

In `internal/apps/notes/view.go`, add a field to `outlineView`:

```go
	// ShareURL is "/notes/s/{slug}" when Zoomed and Root is shared, ""
	// otherwise. Computed once here so the template does not concatenate
	// a path from user-controlled data itself.
	ShareURL string
```

In `internal/apps/notes/handlers.go`'s `renderOutline`, after `view.Root, view.Zoomed, view.Crumbs = root, true, crumbs`, add:

```go
		if root.Shared() {
			view.ShareURL = "/notes/s/" + root.ShareSlug
		}
```

- [ ] **Step 7: Add the row-menu Share/Unshare buttons to outline.html**

In `internal/apps/notes/templates/outline.html`, inside the `outline-menu-list` block, add a button after the Archive button and before the Delete button:

```html
						{{if .ShareSlug}}
						<button type="submit" class="quiet" formaction="/notes/{{.ID}}/unshare"
						        aria-label="Stop sharing">
							<span class="outline-menu-label">Stop sharing</span>
						</button>
						{{else}}
						<button type="submit" class="quiet" formaction="/notes/{{.ID}}/share"
						        aria-label="Share">
							<span class="outline-menu-label">Share</span>
						</button>
						{{end}}
```

This button deliberately carries no `hx-post`/`hx-target` — see `mutateThen`'s doc comment for why: without a real page navigation there is nowhere for the resulting link to be shown, so this is a plain form submission (via `formaction`) even when JS is on, unlike every other button in this menu.

- [ ] **Step 8: Add the zoomed banner showing the link**

In the same file, in the `{{if .Data.Zoomed}}` block, after the existing archived-banner block (`{{if .Data.Root.Archived}}...{{end}}`), add:

```html
	{{if .Data.Root.Shared}}
	<div class="notice row">
		<span>Anyone with this link can read this bullet and its subtree: <a href="{{.Data.ShareURL}}">{{.Data.ShareURL}}</a></span>
		<button type="button" class="button" data-copy-link="{{.Data.ShareURL}}">Copy link</button>
	</div>
	{{end}}
```

`.notice`/`.notice.row` and `[data-copy-link]`'s click-to-copy behaviour already exist platform-wide (`internal/ui/static/app.css`, `internal/ui/static/theme.js`) — ON Paste's own `templates/view.html` uses the identical block, so no CSS or JS changes are needed for this step.

- [ ] **Step 9: Run the tests**

Run: `go test ./internal/apps/notes/... -run 'TestShare|TestUnshare|TestOutlineMenuHasAShareAction|TestOutlineMenuShowsStopSharing|TestZoomedBanner|TestTopLevelOutlineShowsNoShareBanner|TestEveryMutationRequiresSignIn' -v`
Expected: PASS.

- [ ] **Step 10: Run the full suite**

Run: `go build ./... && go vet ./... && go test ./... -race`
Expected: PASS. This touches `handlers.go`'s shared `mutate` function, used by every existing structural route, so the full suite (not just this package) must stay green.

- [ ] **Step 11: Commit**

```bash
git add internal/apps/notes/handlers.go internal/apps/notes/app.go \
        internal/apps/notes/view.go internal/apps/notes/templates/outline.html \
        internal/apps/notes/handlers_test.go
git commit -m "feat(notes): add share/unshare routes and outline UI"
```

---

### Task 4: The public share page

**Files:**
- Create: `internal/apps/notes/templates/shared.html`
- Modify: `internal/apps/notes/handlers.go` (add `viewShared`)
- Modify: `internal/apps/notes/app.go` (register the one `Public` route + route-map comment)
- Modify: `internal/ui/static/app.css` (new `.notes-shared-*` rules)
- Test: `internal/apps/notes/handlers_test.go` (append)

**Interfaces:**
- Consumes: `Store.ByShareSlug`, `Store.SharedSubtree`, `nestShared`, `sharedView` (Tasks 1–2), `Router.PublicFunc` (`internal/platform/app/router.go`, pre-existing).
- Produces: `GET /notes/s/{slug}` — the app's only public route.

- [ ] **Step 1: Write the failing tests**

Append to `internal/apps/notes/handlers_test.go`:

```go
func TestSharedPageIsReadableWhileSignedOut(t *testing.T) {
	s := newServer(t)
	ctx := context.Background()
	id := s.seed(t, s.Alice, notes.RootID, "Shared root")
	slug, err := s.Store.Share(ctx, s.Alice.User.ID, id)
	if err != nil {
		t.Fatal(err)
	}

	// nil session: no cookies at all.
	doc := s.Get(t, nil, "/notes/s/"+slug)
	if got := htmlassert.Text(doc.MustHave("h1")); got != "Shared root" {
		t.Errorf("title = %q", got)
	}
}

func TestSharedPageShowsTheSubtree(t *testing.T) {
	s := newServer(t)
	ctx := context.Background()
	root := s.seed(t, s.Alice, notes.RootID, "root")
	s.seed(t, s.Alice, root, "child one")
	slug, err := s.Store.Share(ctx, s.Alice.User.ID, root)
	if err != nil {
		t.Fatal(err)
	}

	doc := s.Get(t, nil, "/notes/s/"+slug)
	if !strings.Contains(doc.Text(), "child one") {
		t.Error("the shared subtree's child is not on the shared page")
	}
}

func TestSharedPageExcludesArchivedAndDoneDescendants(t *testing.T) {
	s := newServer(t)
	ctx := context.Background()
	root := s.seed(t, s.Alice, notes.RootID, "root")
	kept := s.seed(t, s.Alice, root, "kept")
	archived := s.seed(t, s.Alice, root, "put away")
	done := s.seed(t, s.Alice, root, "finished")
	if err := s.Store.SetArchived(ctx, s.Alice.User.ID, archived, true); err != nil {
		t.Fatal(err)
	}
	if err := s.Store.SetDone(ctx, s.Alice.User.ID, done, true); err != nil {
		t.Fatal(err)
	}
	slug, err := s.Store.Share(ctx, s.Alice.User.ID, root)
	if err != nil {
		t.Fatal(err)
	}

	doc := s.Get(t, nil, "/notes/s/"+slug)
	if !strings.Contains(doc.Text(), "kept") {
		t.Error("the non-archived, non-done child is missing")
	}
	if strings.Contains(doc.Text(), "put away") {
		t.Error("the archived child leaked onto the shared page")
	}
	if strings.Contains(doc.Text(), "finished") {
		t.Error("the done child leaked onto the shared page")
	}
	_ = kept
}

func TestSharedPageShowsTheFullSubtreeRegardlessOfCollapse(t *testing.T) {
	s := newServer(t)
	ctx := context.Background()
	root := s.seed(t, s.Alice, notes.RootID, "root")
	child := s.seed(t, s.Alice, root, "collapsed child")
	s.seed(t, s.Alice, child, "grandchild")
	if err := s.Store.SetCollapsed(ctx, s.Alice.User.ID, child, true); err != nil {
		t.Fatal(err)
	}
	slug, err := s.Store.Share(ctx, s.Alice.User.ID, root)
	if err != nil {
		t.Fatal(err)
	}

	doc := s.Get(t, nil, "/notes/s/"+slug)
	if !strings.Contains(doc.Text(), "grandchild") {
		t.Error("the shared page hid content behind a collapsed node")
	}
}

func TestSharedPageStaysUpIfTheRootIsLaterArchivedOrDone(t *testing.T) {
	s := newServer(t)
	ctx := context.Background()
	id := s.seed(t, s.Alice, notes.RootID, "root")
	slug, err := s.Store.Share(ctx, s.Alice.User.ID, id)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Store.SetArchived(ctx, s.Alice.User.ID, id, true); err != nil {
		t.Fatal(err)
	}

	rec := s.Do(t, nil, httptest.NewRequest("GET", "/notes/s/"+slug, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("shared page after the root was archived = %d, want 200", rec.Code)
	}
}

func TestSharedPageHasNoLinksIntoThePrivateTree(t *testing.T) {
	s := newServer(t)
	ctx := context.Background()
	root := s.seed(t, s.Alice, notes.RootID, "root")
	s.seed(t, s.Alice, root, "child")
	slug, err := s.Store.Share(ctx, s.Alice.User.ID, root)
	if err != nil {
		t.Fatal(err)
	}

	doc := s.Get(t, nil, "/notes/s/"+slug)
	for _, a := range doc.QueryAll("a") {
		href, _ := htmlassert.Attr(a, "href")
		if strings.HasPrefix(href, "/notes/") && !strings.HasPrefix(href, "/notes/s/") {
			t.Errorf("the shared page links into the private tree: %q", href)
		}
	}
	doc.MustNotHave("form")
}

func TestSharedPageIsNotCachedOrIndexed(t *testing.T) {
	s := newServer(t)
	ctx := context.Background()
	id := s.seed(t, s.Alice, notes.RootID, "root")
	slug, err := s.Store.Share(ctx, s.Alice.User.ID, id)
	if err != nil {
		t.Fatal(err)
	}

	rec := s.Do(t, nil, httptest.NewRequest("GET", "/notes/s/"+slug, nil))
	if cc := rec.Header().Get("Cache-Control"); cc != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store", cc)
	}
	if rt := rec.Header().Get("X-Robots-Tag"); rt != "noindex" {
		t.Errorf("X-Robots-Tag = %q, want noindex", rt)
	}
}

func TestUnknownShareSlugIs404(t *testing.T) {
	s := newServer(t)
	for _, slug := range []string{"never-minted", ""} {
		rec := s.Do(t, nil, httptest.NewRequest("GET", "/notes/s/"+slug, nil))
		if rec.Code != http.StatusNotFound {
			t.Errorf("GET /notes/s/%q = %d, want 404", slug, rec.Code)
		}
	}
}

func TestUnshareKillsTheSharedPage(t *testing.T) {
	s := newServer(t)
	ctx := context.Background()
	id := s.seed(t, s.Alice, notes.RootID, "root")
	slug, err := s.Store.Share(ctx, s.Alice.User.ID, id)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Store.Unshare(ctx, s.Alice.User.ID, id); err != nil {
		t.Fatal(err)
	}

	rec := s.Do(t, nil, httptest.NewRequest("GET", "/notes/s/"+slug, nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("GET a revoked share link = %d, want 404", rec.Code)
	}
}
```

`strings` and `httptest` are already imported by this file (see its existing tests); `htmlassert` is already imported too.

- [ ] **Step 2: Run to verify they fail**

Run: `go test ./internal/apps/notes/... -run 'TestSharedPage|TestUnknownShareSlugIs404|TestUnshareKillsTheSharedPage' -v`
Expected: FAIL — route not registered / template not found.

- [ ] **Step 3: Create templates/shared.html**

```html
{{define "content"}}
<div class="stack notes">
	<h1>{{.Data.Root.DisplayTitleHTML}}</h1>
	{{with .Data.Root.Note}}<p class="dim">{{.}}</p>{{end}}
	{{if .Data.Rows}}
	{{template "shared-rows" .Data.Rows}}
	{{else}}
	<p class="dim">Nothing here.</p>
	{{end}}
</div>
{{end}}

{{/* shared-rows is deliberately not outline-rows (outline.html): no dot,
     no chevron, no menu, no forms — spec §15: "no link leads anywhere
     into the owner's private tree", and the way to guarantee a mutating
     control can never appear here is for this template not to contain
     one at all. */}}
{{define "shared-rows"}}
<ul class="notes-shared-list">
	{{range .}}
	<li class="notes-shared-item{{if .Done}} notes-shared-done{{end}}">
		<span class="notes-shared-title">{{.RenderedTitle}}{{if .DueOn}} <span class="notes-shared-due">@{{.DueOn}}</span>{{end}}</span>
		{{with .RenderedNote}}<div class="notes-shared-note">{{.}}</div>{{end}}
		{{with .Children}}{{template "shared-rows" .}}{{end}}
	</li>
	{{end}}
</ul>
{{end}}
```

- [ ] **Step 4: Add the CSS**

In `internal/ui/static/app.css`, add after the `.notes-archive-restore { flex-shrink: 0; }` rule:

```css
/* The public share page (notes/templates/shared.html) has no forms, no
 * dots and no menu, so it reuses none of .outline-list's flex layout —
 * that layout exists to align a row's menu, chevron and dot, none of
 * which this page has. Indentation and the nesting guide line reuse the
 * same margin/border-left trick as .outline-item .outline-list above,
 * simplified to one level since there is no dot to align a guide line
 * under. */
.notes-shared-list { margin: 0; padding: 0; list-style: none; }
.notes-shared-list .notes-shared-list {
	margin-left: var(--s-3);
	padding-left: var(--s-3);
	border-left: 1px solid var(--c-border);
}
.notes-shared-item { padding: var(--s-1) 0; }
.notes-shared-title { color: var(--c-text); }
.notes-shared-done .notes-shared-title { color: var(--c-text-dim); text-decoration: line-through; }
.notes-shared-due { font-size: var(--fs-sm); color: var(--c-text-dim); }
.notes-shared-note { font-size: var(--fs-sm); color: var(--c-text-dim); margin-top: 0.125rem; }
```

- [ ] **Step 5: Add the `viewShared` handler to handlers.go**

```go
// viewShared renders a shared bullet and its subtree for anyone holding
// its slug — spec §15: no login, no breadcrumb, no link into the owner's
// private tree. It uses its own template and its own row type (sharedRow,
// share.go) rather than reusing outline.html's outlineRow and its forms —
// the way to guarantee a public page can never carry a mutating control
// is for the template not to contain one at all, the same reasoning ON
// Paste's own shared.html documents.
//
// The root's own archived/done state is deliberately not checked here —
// see SharedSubtree's doc comment and this plan's resolved design
// question: a share link stays live even if its root is later archived
// or marked done, the same way a direct zoom to an archived node already
// renders instead of 404ing.
//
// A share slug is a revocable credential, so this page must never be
// cached or indexed: a crawler or a shared cache holding onto it after
// the owner unshares would defeat the revocation — the same concern ON
// Paste's own viewShared documents.
func (a *App) viewShared(w http.ResponseWriter, r *http.Request) {
	root, err := a.store.ByShareSlug(r.Context(), r.PathValue("slug"))
	if err != nil {
		a.fail(w, r, err)
		return
	}

	flat, err := a.store.SharedSubtree(r.Context(), root.UserID, root.ID)
	if err != nil {
		a.deps.Errors.Internal(w, r, err)
		return
	}

	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Robots-Tag", "noindex")

	page := a.deps.Page(r, root.DisplayTitle())
	page.Data = sharedView{Root: root, Rows: nestShared(flat)}
	a.render(w, r, http.StatusOK, "notes/shared", page)
}
```

- [ ] **Step 6: Register the public route in app.go**

In `internal/apps/notes/app.go`'s `Mount`, add after the last `r.HandleFunc(...)` call:

```go
	// The one public route in this app — spec §9/§15. Every other route in
	// this file goes through HandleFunc, which requires sign-in; this is
	// the sole deliberate exception, the same asymmetry
	// internal/apps/paste/paste.go's own Mount documents for its three
	// public routes.
	r.PublicFunc("GET /s/{slug}", a.viewShared)
```

Update the route-map comment above `Mount` to add:

```go
//	GET  /notes/s/{slug}     N9's public share page — the app's only
//	                         Public route
```

- [ ] **Step 7: Run the tests**

Run: `go test ./internal/apps/notes/... -run 'TestSharedPage|TestUnknownShareSlugIs404|TestUnshareKillsTheSharedPage' -v`
Expected: PASS.

- [ ] **Step 8: Run the full suite**

Run: `go build ./... && go vet ./... && go test ./... -race`
Expected: PASS.

- [ ] **Step 9: Commit**

```bash
git add internal/apps/notes/templates/shared.html internal/apps/notes/handlers.go \
        internal/apps/notes/app.go internal/ui/static/app.css \
        internal/apps/notes/handlers_test.go
git commit -m "feat(notes): add the public share page"
```

---

### Task 5: Docs

**Files:**
- Modify: `internal/apps/notes/notes.go` (package doc comment)
- Modify: `README.md`

**Interfaces:** None — this task changes only prose.

- [ ] **Step 1: Update notes.go's package doc comment**

Re-read the current top-of-file comment first (fix commits since N8 may have touched it). Update the chunk range and the file list — it currently ends "...N8 (export and import): app.App, routes, templates, handlers, the keyboard layer in static/notes.js, the inline Markdown renderer in markdown.go, task tracking in tree.go, prefs.go and due.go, full-text search in search.go, archiving in archive.go, and Markdown/JSON export plus Markdown import in export.go and import.go." Change it to:

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
// package spans chunk N1 (schema and store) through N9 (public sharing):
// app.App, routes, templates, handlers, the keyboard layer in
// static/notes.js, the inline Markdown renderer in markdown.go, task
// tracking in tree.go, prefs.go and due.go, full-text search in
// search.go, archiving in archive.go, Markdown/JSON export plus Markdown
// import in export.go and import.go, and public read-only sharing in
// share.go.
package notes
```

(Adjust to match whatever the file actually says by the time this task runs, preserving its exact style — the point is the chunk range and the new `share.go` mention, not a verbatim replacement if the surrounding prose has since changed.)

- [ ] **Step 2: Update README.md**

Re-read both spots first (other work may have touched them). In the top summary paragraph, the ON Notes sentence currently ends "...done/due tracking with a cross-tree due-date view, and full-text search with ancestor breadcrumbs." Extend it:

```markdown
Of the four apps the "ON" prefix is reserved for, two are built and registered
today. **ON Paste** holds snippets of code or text, with syntax highlighting
and shareable links. **ON Notes** is a hierarchical outliner — one infinite
tree per account, with zoom, collapse, every structural operation, a full
keyboard layer, inline Markdown (bold, links, `#tags`), done/due tracking
with a cross-tree due-date view, full-text search with ancestor
breadcrumbs, archiving, Markdown/JSON export and import, and public
read-only share links. ON Reader and ON Flash are future work: the
platform and app framework are ready for them, but no code exists yet.
```

In the Status section, the sentence currently reads "N1 (schema and store), N2 (the outline), N3 (the keyboard layer), N4 (Markdown), N5 (done + due dates), N6 (search and tags), N7 (archiving) and N8 (export and import) are done." Change to:

```markdown
Work since then is per-app rather than per-phase. ON Notes is being built in
ten small chunks under
[`docs/superpowers/specs/2026-08-25-on-notes-design.md`](docs/superpowers/specs/2026-08-25-on-notes-design.md);
N1 (schema and store), N2 (the outline), N3 (the keyboard layer), N4
(Markdown), N5 (done + due dates), N6 (search and tags), N7 (archiving),
N8 (export and import) and N9 (public sharing) are done.
```

- [ ] **Step 3: Run the full suite one last time**

Run: `go build ./... && go vet ./... && go test ./... -race`
Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git add internal/apps/notes/notes.go README.md
git commit -m "docs(notes): note N9 (public sharing) as done"
```

---

## Self-Review

**1. Spec coverage.**

- §4 (schema: `share_slug`, unique index) — Task 1, Step 1.
- §9 (routes: `POST /{id}/share`, `POST /{id}/unshare`, `GET /s/{slug}` as the only `Public` route) — Task 3 Step 5, Task 4 Step 6.
- §13/§15 (archived and completed nodes excluded from share pages; root itself stays live — this plan's resolved design question) — `SharedSubtree`'s doc comment and filtering, Task 2.
- §15 (unguessable slug, re-share mints fresh, no login, no breadcrumb, no link into the private tree) — Task 1 (`newShareSlug`, `Share`'s re-mint behaviour), Task 4 (`shared.html`'s row markup, `TestSharedPageHasNoLinksIntoThePrivateTree`).
- §17 (store tests against a real SQLite temp file, handler tests via htmlassert, ownership cases as 404) — every task's test steps use the existing `newFixture`/`newServer` harnesses, which already do this; `TestShareOnAnotherUsersBulletIs404`/`TestUnshareOnAnotherUsersBulletIs404`/`TestShareRejectsAnotherUsersNode`/`TestUnshareRejectsAnotherUsersNode` cover ownership.
- §14's "JSON is the safety net" — extended in Task 1, Step 6 to also carry `share_slug`, so a whole-account JSON export/restore does not silently drop a live share.

No spec requirement in §15 (or the routes it implies in §9) is left without a task.

**2. Placeholder scan.** No "TBD"/"add validation"/"similar to Task N" phrasing appears in any step; every code block is complete, runnable code. The two places this plan asks the implementer to re-check current file contents before editing (Task 1 Step 10's fixture-helper check, Task 5's doc-comment re-read) are deliberate "the file may have moved since this was written" notes, not missing content — the code shown is still the actual, complete change to make.

**3. Type consistency.** `Node.ShareSlug string` / `Node.Shared() bool` (Task 1) are used identically in Task 2 (`sharedRow` embeds `Node`), Task 3 (`{{.ShareSlug}}`/`{{.Data.Root.Shared}}` in outline.html, `outlineView.ShareURL`), and Task 4 (`sharedView.Root.DisplayTitle()`). `Store.Share`/`Store.Unshare`/`Ops.Share`/`Ops.Unshare` signatures introduced in Task 1 are called with the same argument order (`ctx, userID, id`) everywhere they are used in Tasks 3–4. `Store.SharedSubtree(ctx, userID, rootID int64) ([]Node, error)` (Task 2) is called with `root.UserID, root.ID` in Task 4's `viewShared` — matching order. `mutateThen`'s new fourth parameter (`redirectToSelf bool`) is `false` from `mutate`'s wrapper and `true` from both `share` and `unshare` — no third caller needed it, so no other existing route's behavior changes.

**Why this plan modifies `mutate`, a function every existing route depends on.** `mutate`'s hard-coded redirect-to-`root` is exactly right for every route before this chunk — none of them has anywhere better to send a JS-off browser. Share is the first operation whose whole point is to show the caller something that only exists at a *different* URL than the one they submitted from. Rather than duplicating `mutate`'s text-saving transaction logic in a separate function (which would let the two silently drift, e.g. if a future fix changes how `focus_id` is validated), this plan turns `mutate` into a one-line wrapper around a new `mutateThen` that takes the one axis of variation as an explicit parameter. Every other caller's behavior is provably unchanged: `mutate(w, r, op)` is now defined as `mutateThen(w, r, op, false)`, which reproduces the original function's body exactly for `redirectToSelf == false`. Task 3 Step 10 runs the *whole* suite (not just this package) specifically because of this shared-function risk.

**Why the shared page ignores collapse but respects archived/done, and why the root is exempt from both.** These are the plan's own resolved design questions, not spec text alone — documented once, in full, on `SharedSubtree`'s doc comment (Task 2) and cross-referenced from `viewShared`'s (Task 4) rather than repeated. A future reader who only sees the handler is pointed to the one place the reasoning lives.

**Why Share/Unshare's row-menu buttons carry no `hx-post`.** Every other button in that menu is HTMX-enhanced because refreshing `#outline` in place is exactly what should happen after, say, an indent. Share is different: the value of clicking it is seeing the resulting link, and an in-place `#outline` refresh at the *current* zoom would leave the user exactly where they started with no visible change (the row's own menu doesn't show the link inline — only the zoomed banner does). A plain, un-enhanced submit is what makes the browser actually navigate to `/notes/{id}`, where that banner lives. This is documented on `mutateThen`'s doc comment (Task 3) and referenced, not repeated, from the button's own inline template comment.

## Follow-ups

Not part of this plan; noted for a future backlog pass once N9 has been used for a while.

- **No inline "shared" indicator on a row that is not the current zoom root.** If a deeply-nested bullet is shared, there is no visual cue on the outline row itself (only the zoomed banner shows it) — a user who forgets which bullet they shared has to open each one's menu to check. A due-chip-style inline badge would close this, but was deliberately left out of this plan to keep Task 3's UI surface small; see the "Share UI placement" design question this plan resolved.
- **Sharing an ancestor does not cascade any visible state to already-shared descendants.** Two independently shared nodes, one nested inside the other, both keep working exactly as before — this is consistent with the plan's per-node model, but was never spelled out in the spec and is worth a one-line spec addendum if it ever causes confusion.
- **The shared page has no equivalent of ON Paste's `/raw` route.** Not needed for this format (there is no separate "plain text" rendering of an outline the way there is for a code snippet), but worth naming as a deliberate omission rather than a gap nobody noticed.
