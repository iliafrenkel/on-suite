# ON Suite — Plan 3 of 3: ON Paste and Operations

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build ON Paste as the first real application on the platform, then make the whole thing deployable — backups, TLS, packaging and CI.

**Deliverable:** The complete spec. A snippet can be created, viewed with syntax highlighting, listed, deleted, shared via a revocable unguessable link readable while signed out, fetched as plain text, and exported as JSON. The binary can back itself up, obtain its own certificate, and ships as a signed release.

**Architecture:** ON Paste is a package under `internal/apps/paste` implementing `app.App`. It imports only `internal/platform/*`, owns every table prefixed `paste_`, ships its own migrations and templates, and is registered by one line in `cmd/onsuite/main.go`. Nothing in the platform learns that it exists.

**Tech Stack:** unchanged, plus exactly one app dependency: `github.com/alecthomas/chroma/v2 v2.27.0` for syntax highlighting.

**Spec:** `docs/superpowers/specs/2026-08-18-on-suite-platform-design.md`
**Previous plans:** `...-01-platform-core.md` and `...-02-web-and-apps.md`, both executed.

## This plan's code has been compiled and run

Every Go, SQL, HTML and CSS block below was extracted from this document,
applied on top of the executed Plan 2 tree, and run before publishing. On this
toolchain (Go 1.26.6, SQLite 3.53.3, Chroma 2.27.0):

- `gofmt -l .` clean, `go vet ./...` clean, `staticcheck ./...` clean,
  `go test ./... -race` green in all eleven packages.
- `CGO_ENABLED=0` cross-compiles for linux/amd64, linux/arm64 and darwin/arm64.
- Exactly five direct dependencies, as this plan claims.
- The whole spec was driven end to end against a real server: ON Paste appearing
  in the nav without any platform code naming it, a snippet created and rendered
  with `class="chroma"` and **zero** inline style attributes, `<b>` in the body
  coming back escaped, a 22-character share slug readable while signed out, raw
  text served as `text/plain` with `nosniff`, revocation returning 404,
  re-sharing minting a different slug while the old one stayed dead, a snapshot
  written, and an export containing `"shared": true` with no slug anywhere in it.

Four defects were found and fixed this way:

| Defect | Fix |
|---|---|
| Each command applied a different set of migrations — `serve` all, `user add` only the platform's, `export` none — so exporting a fresh database failed with `no such table: paste_snippets` | `openDatabase` in Task 19 Step 1; every command now goes through it |
| An escaping test asserted on `onerror=alert(1)`, which correctly-escaped output still contains as inert text, so it failed on working code | Assert on the unescaped tag instead |
| A comment line beginning `// go:embed`, committed in Plan 2, is read as a malformed directive (SA9009) and would make CI red on arrival | Task 23 Step 1 |
| `viewModel.Highlight` was written as a placeholder type that could not compile | Declared as `template.HTML` |

The migration one is the interesting defect: three commands had each grown
their own copy of "open the database and set up the schema", and the copies had
drifted. It only surfaced because a fourth command was added and the tests ran
against a genuinely empty directory.

## Global Constraints

Plans 1 and 2 constraints all still apply. In addition:

- **One new dependency, and only in ON Paste:** `github.com/alecthomas/chroma/v2 v2.27.0`. Direct dependencies end at five.
- **Chroma must be used in class mode.** Its default HTML formatter emits inline `style=` attributes, which Plan 2's Content-Security-Policy blocks. `chromahtml.WithClasses(true)` plus a generated stylesheet is the only working configuration. This was verified before the plan was written; see Task 16.
- **Every owner query is scoped by `user_id` in SQL**, and a snippet belonging to someone else returns `ErrNotFound`, never a 403. A 403 would confirm the snippet exists.
- **Public routes are declared with `Router.Public` and there are exactly three.** Any fourth is a design change.

### Route map — read before writing any handler

`ServeMux` **panics at startup** when two patterns of the same method and segment count are ambiguous. The set below was verified to register cleanly; the naive alternatives were verified to panic.

| Method | Pattern | Access | Purpose |
|---|---|---|---|
| GET | `/paste/{$}` | signed in | list your snippets |
| GET | `/paste/new` | signed in | create form |
| POST | `/paste/new` | signed in | create |
| GET | `/paste/{id}` | signed in | view your snippet |
| GET | `/paste/raw/{id}` | signed in | your snippet as text |
| POST | `/paste/{id}/delete` | signed in | delete |
| POST | `/paste/{id}/share` | signed in | mint a share link |
| POST | `/paste/{id}/unshare` | signed in | revoke it |
| GET | `/paste/s/{slug}` | **public** | shared view |
| GET | `/paste/s/{slug}/raw` | **public** | shared text |
| GET | `/paste/highlight.css` | **public** | generated highlight stylesheet |

Why it is shaped this way, since it looks arbitrary until you hit the panic:

- `GET /paste/{id}/raw` would collide with `GET /paste/s/{slug}` — both are two-segment GETs and `/paste/s/raw` matches both. Hence `GET /paste/raw/{id}`, which is literal-first like `/paste/s/...` and so unambiguous.
- The `POST` actions on `/paste/{id}/...` are safe despite being two-segment wildcard-first, because **conflict detection considers the method** and no public GET shares that shape. Verified.
- `GET /paste/new` and `GET /paste/highlight.css` are literals, which beat `{id}` at the same depth. That is allowed and intended.

## File Structure

| Path | Responsibility |
|---|---|
| `internal/apps/paste/paste.go` | `Meta`, `Migrations`, `Templates`, `Mount`. The only file the platform touches. |
| `internal/apps/paste/store.go` | All snippet SQL. No HTTP, no Chroma. |
| `internal/apps/paste/handlers.go` | HTTP handlers. |
| `internal/apps/paste/highlight.go` | Chroma wrapper and the generated stylesheet. |
| `internal/apps/paste/export.go` | JSON export. |
| `internal/apps/paste/migrations/0001_snippets.sql` | The `paste_snippets` schema. |
| `internal/apps/paste/templates/*.html` | List, new, view, shared. |
| `cmd/onsuite/backup.go` | The `backup` command and the nightly job. |
| `cmd/onsuite/export.go` | The `export` command. |
| `cmd/onsuite/tls.go` | Reverse-proxy and autocert serving modes. |
| `docs/deploy/onsuite.service` | systemd unit. |
| `docs/deploy/README.md` | Deployment guide. |
| `.goreleaser.yaml`, `Dockerfile`, `.github/workflows/ci.yml` | Packaging and CI. |

---

# Task 15: The snippet schema and store

**Files:**
- Create: `internal/apps/paste/migrations/0001_snippets.sql`
- Create: `internal/apps/paste/store.go`
- Test: `internal/apps/paste/store_test.go`

**Interfaces:**
- Consumes: `db.Open`, `db.Collect`, `db.Apply`, `auth.Store` (Plan 1).
- Produces:
  - `paste.ID = "paste"`, `paste.MaxTitleRunes = 120`, `paste.MaxBodyBytes = 512 << 10`
  - `paste.Migrations() fs.FS`
  - `paste.Snippet{ID, UserID int64; Title, Language, Body, ShareSlug string; CreatedAt time.Time}` with `(Snippet).Shared() bool` and `(Snippet).Lines() int`
  - `paste.NewStore(handle *sql.DB) *Store`, `(*Store).SetClock(func() time.Time)`
  - `(*Store).Create(ctx, userID int64, title, language, body string) (Snippet, error)`
  - `(*Store).ByID(ctx, userID, id int64) (Snippet, error)` — owner-scoped
  - `(*Store).ByShareSlug(ctx, slug string) (Snippet, error)`
  - `(*Store).List(ctx, userID int64, limit int) ([]Snippet, error)`
  - `(*Store).Delete(ctx, userID, id int64) error`
  - `(*Store).Share(ctx, userID, id int64) (string, error)`
  - `(*Store).Unshare(ctx, userID, id int64) error`
  - `paste.ErrNotFound`, `paste.ErrInvalid`

**Verified schema behaviour** (probed before writing; do not re-litigate):
- `share_slug TEXT UNIQUE` permits **many NULLs** — SQLite treats NULLs as distinct — so unshared snippets coexist while shared slugs stay unique.
- A NULL `share_slug` scans into `sql.NullString{Valid: false}`.
- `ON DELETE CASCADE` from `paste_snippets.user_id` removes a user's snippets with them.
- An owner-scoped `WHERE id = ? AND user_id = ?` returns zero rows for another user's snippet.

- [ ] **Step 1: Write the migration**

Create `internal/apps/paste/migrations/0001_snippets.sql`:

```sql
-- ON Paste owns exactly one table, prefixed with its app id so it cannot
-- collide with any other app in the single shared database.

CREATE TABLE paste_snippets (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id    INTEGER NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    title      TEXT    NOT NULL,
    -- language is a Chroma lexer name, or "" meaning detect from content.
    language   TEXT    NOT NULL,
    body       TEXT    NOT NULL,
    -- share_slug is NULL until the snippet is shared, and is replaced with a
    -- fresh value on each re-share so a revoked link can never come back.
    -- UNIQUE permits many NULLs: SQLite treats NULLs as distinct.
    share_slug TEXT    UNIQUE,
    created_at TEXT    NOT NULL
) STRICT;

-- The list page is the only hot query: a user's snippets, newest first.
CREATE INDEX paste_snippets_user_created_idx
    ON paste_snippets (user_id, created_at DESC);
```

- [ ] **Step 2: Write the store**

Create `internal/apps/paste/store.go`:

```go
// Package paste implements ON Paste, a place to keep and share snippets of
// code or text.
//
// It depends only on internal/platform/*. It never imports another app, and no
// platform package imports it: the whole coupling is the app.App interface plus
// one line in cmd/onsuite/main.go.
package paste

import (
	"context"
	"crypto/rand"
	"database/sql"
	"embed"
	"encoding/base64"
	"errors"
	"fmt"
	"io/fs"
	"strings"
	"time"
	"unicode/utf8"
)

// ID is the app id: the URL prefix, the migration namespace, and the prefix on
// every table this app owns.
const ID = "paste"

const (
	// MaxTitleRunes bounds a title so the list page stays readable.
	MaxTitleRunes = 120
	// MaxBodyBytes bounds a snippet. Half a megabyte is a very large snippet
	// and still well inside the platform's 1 MiB request limit, leaving room
	// for the form's other fields and percent-encoding.
	MaxBodyBytes = 512 << 10
	// shareSlugBytes is 16 bytes, 128 bits, base64url encoded to 22
	// characters. The slug is the only protection on a shared snippet, so it
	// must be unguessable rather than merely unique.
	shareSlugBytes = 16
	// DefaultListLimit caps the list page.
	DefaultListLimit = 200
)

//go:embed migrations/*.sql
var migrationFiles embed.FS

// Migrations returns this app's schema with filenames at the root, which is
// what db.Collect expects.
func Migrations() fs.FS {
	sub, err := fs.Sub(migrationFiles, "migrations")
	if err != nil {
		// Unreachable: the path is a compile-time constant checked by go:embed.
		panic("paste: embedded migrations missing: " + err.Error())
	}
	return sub
}

var (
	// ErrNotFound covers both "no such snippet" and "not yours". They are
	// deliberately indistinguishable: returning a 403 for someone else's
	// snippet would confirm that it exists.
	ErrNotFound = errors.New("paste: not found")
	ErrInvalid  = errors.New("paste: invalid snippet")
)

// Snippet is one stored snippet.
type Snippet struct {
	ID        int64
	UserID    int64
	Title     string
	Language  string // Chroma lexer name, or "" for detect-from-content
	Body      string
	ShareSlug string // "" when not shared
	CreatedAt time.Time
}

// Shared reports whether a public link currently exists.
func (s Snippet) Shared() bool { return s.ShareSlug != "" }

// Lines counts the lines in the body, for display.
func (s Snippet) Lines() int {
	if s.Body == "" {
		return 0
	}
	return strings.Count(strings.TrimSuffix(s.Body, "\n"), "\n") + 1
}

// DisplayTitle is what to show when a snippet was saved without a title.
func (s Snippet) DisplayTitle() string {
	if strings.TrimSpace(s.Title) == "" {
		return "Untitled"
	}
	return s.Title
}

// Store is all the SQL for ON Paste. It has no HTTP knowledge and does not
// import Chroma, so it can be tested against a real database on its own.
type Store struct {
	db  *sql.DB
	now func() time.Time
}

func NewStore(handle *sql.DB) *Store {
	return &Store{db: handle, now: func() time.Time { return time.Now().UTC() }}
}

// SetClock replaces the time source, for tests.
func (st *Store) SetClock(now func() time.Time) { st.now = now }

// Validate checks a snippet's user-supplied fields. Exported because the
// handler reports these messages back to the user.
func Validate(title, body string) error {
	if strings.TrimSpace(body) == "" {
		return fmt.Errorf("%w: the snippet is empty", ErrInvalid)
	}
	if len(body) > MaxBodyBytes {
		return fmt.Errorf("%w: the snippet is larger than %d KiB", ErrInvalid, MaxBodyBytes>>10)
	}
	if !utf8.ValidString(body) {
		return fmt.Errorf("%w: the snippet is not valid UTF-8", ErrInvalid)
	}
	if utf8.RuneCountInString(title) > MaxTitleRunes {
		return fmt.Errorf("%w: the title is longer than %d characters", ErrInvalid, MaxTitleRunes)
	}
	if !utf8.ValidString(title) {
		return fmt.Errorf("%w: the title is not valid UTF-8", ErrInvalid)
	}
	return nil
}

// Create stores a new snippet. It is never shared to begin with.
func (st *Store) Create(ctx context.Context, userID int64, title, language, body string) (Snippet, error) {
	title = strings.TrimSpace(title)
	if err := Validate(title, body); err != nil {
		return Snippet{}, err
	}

	s := Snippet{
		UserID:    userID,
		Title:     title,
		Language:  language,
		Body:      body,
		CreatedAt: st.now(),
	}
	err := st.db.QueryRowContext(ctx,
		`INSERT INTO paste_snippets (user_id, title, language, body, share_slug, created_at)
		 VALUES (?, ?, ?, ?, NULL, ?)
		 RETURNING id`,
		s.UserID, s.Title, s.Language, s.Body, formatTime(s.CreatedAt),
	).Scan(&s.ID)
	if err != nil {
		return Snippet{}, fmt.Errorf("paste: create: %w", err)
	}
	return s, nil
}

// ByID fetches one of userID's own snippets.
func (st *Store) ByID(ctx context.Context, userID, id int64) (Snippet, error) {
	return scanSnippet(st.db.QueryRowContext(ctx,
		`SELECT id, user_id, title, language, body, share_slug, created_at
		 FROM paste_snippets WHERE id = ? AND user_id = ?`, id, userID))
}

// ByShareSlug fetches a snippet by its public slug, with no owner check —
// possessing the slug is the authorisation.
func (st *Store) ByShareSlug(ctx context.Context, slug string) (Snippet, error) {
	if slug == "" {
		return Snippet{}, ErrNotFound
	}
	return scanSnippet(st.db.QueryRowContext(ctx,
		`SELECT id, user_id, title, language, body, share_slug, created_at
		 FROM paste_snippets WHERE share_slug = ?`, slug))
}

// List returns userID's snippets, newest first. Bodies are included because
// the list shows a preview; at this scale that is cheaper than a second query.
func (st *Store) List(ctx context.Context, userID int64, limit int) ([]Snippet, error) {
	if limit <= 0 || limit > DefaultListLimit {
		limit = DefaultListLimit
	}
	rows, err := st.db.QueryContext(ctx,
		`SELECT id, user_id, title, language, body, share_slug, created_at
		 FROM paste_snippets WHERE user_id = ?
		 ORDER BY created_at DESC, id DESC
		 LIMIT ?`, userID, limit)
	if err != nil {
		return nil, fmt.Errorf("paste: list: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []Snippet
	for rows.Next() {
		s, err := scanSnippetRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("paste: list: %w", err)
	}
	return out, nil
}

// Delete removes one of userID's own snippets.
func (st *Store) Delete(ctx context.Context, userID, id int64) error {
	res, err := st.db.ExecContext(ctx,
		`DELETE FROM paste_snippets WHERE id = ? AND user_id = ?`, id, userID)
	if err != nil {
		return fmt.Errorf("paste: delete: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("paste: delete: %w", err)
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// Share mints a fresh slug and returns it.
//
// Re-sharing always generates a new slug rather than reusing the old one, so a
// link that was revoked stays dead even if sharing is turned back on.
func (st *Store) Share(ctx context.Context, userID, id int64) (string, error) {
	slug, err := newShareSlug()
	if err != nil {
		return "", err
	}
	res, err := st.db.ExecContext(ctx,
		`UPDATE paste_snippets SET share_slug = ? WHERE id = ? AND user_id = ?`,
		slug, id, userID)
	if err != nil {
		return "", fmt.Errorf("paste: share: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return "", fmt.Errorf("paste: share: %w", err)
	}
	if n == 0 {
		return "", ErrNotFound
	}
	return slug, nil
}

// Unshare revokes the public link.
func (st *Store) Unshare(ctx context.Context, userID, id int64) error {
	res, err := st.db.ExecContext(ctx,
		`UPDATE paste_snippets SET share_slug = NULL WHERE id = ? AND user_id = ?`,
		id, userID)
	if err != nil {
		return fmt.Errorf("paste: unshare: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("paste: unshare: %w", err)
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// rowScanner is satisfied by both *sql.Row and *sql.Rows.
type rowScanner interface {
	Scan(dest ...any) error
}

func scanSnippet(row *sql.Row) (Snippet, error) {
	s, err := scanSnippetRow(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Snippet{}, ErrNotFound
	}
	return s, err
}

func scanSnippetRow(row rowScanner) (Snippet, error) {
	var (
		s         Snippet
		slug      sql.NullString
		createdAt string
	)
	err := row.Scan(&s.ID, &s.UserID, &s.Title, &s.Language, &s.Body, &slug, &createdAt)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return Snippet{}, sql.ErrNoRows // translated by scanSnippet
	case err != nil:
		return Snippet{}, fmt.Errorf("paste: scan: %w", err)
	}
	s.ShareSlug = slug.String // "" when NULL, since Valid is false

	if s.CreatedAt, err = parseTime(createdAt); err != nil {
		return Snippet{}, err
	}
	return s, nil
}

func newShareSlug() (string, error) {
	buf := make([]byte, shareSlugBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("paste: generate share slug: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

// Timestamps match the platform's convention: RFC 3339 nanoseconds in UTC,
// which sorts chronologically as text.
func formatTime(t time.Time) string { return t.UTC().Format(time.RFC3339Nano) }

func parseTime(s string) (time.Time, error) {
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return time.Time{}, fmt.Errorf("paste: parse timestamp %q: %w", s, err)
	}
	return t.UTC(), nil
}
```

- [ ] **Step 3: Test the store against a real database**

Create `internal/apps/paste/store_test.go`:

```go
package paste_test

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/iliafrenkel/on-suite/internal/apps/paste"
	"github.com/iliafrenkel/on-suite/internal/platform/auth"
	"github.com/iliafrenkel/on-suite/internal/platform/db"
)

// fixture is a migrated database with two users, so every owner-scoping test
// has somebody else to be confused with.
type fixture struct {
	store *paste.Store
	db    *sql.DB
	alice auth.User
	bob   auth.User
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	ctx := context.Background()

	handle, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = handle.Close() })

	// Apply the platform schema and this app's, exactly as the server does.
	migrations, err := db.Collect(auth.Namespace, auth.Migrations())
	if err != nil {
		t.Fatal(err)
	}
	appMigrations, err := db.Collect(paste.ID, paste.Migrations())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Apply(ctx, handle, append(migrations, appMigrations...)); err != nil {
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
	return &fixture{store: paste.NewStore(handle), db: handle, alice: alice, bob: bob}
}

func TestCreateAndFetch(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	created, err := f.store.Create(ctx, f.alice.ID, "  My snippet  ", "go", "package main\n")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if created.ID == 0 {
		t.Error("Create returned id 0")
	}
	if created.Title != "My snippet" {
		t.Errorf("Title = %q; whitespace should be trimmed", created.Title)
	}
	if created.Shared() {
		t.Error("a new snippet is shared; it must start private")
	}
	if created.CreatedAt.IsZero() {
		t.Error("CreatedAt is zero")
	}

	got, err := f.store.ByID(ctx, f.alice.ID, created.ID)
	if err != nil {
		t.Fatalf("ByID: %v", err)
	}
	if got.Body != "package main\n" || got.Language != "go" {
		t.Errorf("round trip lost data: %+v", got)
	}
	if !got.CreatedAt.Equal(created.CreatedAt) {
		t.Errorf("CreatedAt = %v, want %v", got.CreatedAt, created.CreatedAt)
	}
}

// TestOwnerScoping is the security property of this store: another signed-in
// user must not be able to reach your snippet, and must not be told it exists.
func TestOwnerScoping(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	s, err := f.store.Create(ctx, f.alice.ID, "alice's", "go", "secret\n")
	if err != nil {
		t.Fatal(err)
	}

	if _, err := f.store.ByID(ctx, f.bob.ID, s.ID); !errors.Is(err, paste.ErrNotFound) {
		t.Errorf("ByID as another user = %v, want ErrNotFound", err)
	}
	if err := f.store.Delete(ctx, f.bob.ID, s.ID); !errors.Is(err, paste.ErrNotFound) {
		t.Errorf("Delete as another user = %v, want ErrNotFound", err)
	}
	if _, err := f.store.Share(ctx, f.bob.ID, s.ID); !errors.Is(err, paste.ErrNotFound) {
		t.Errorf("Share as another user = %v, want ErrNotFound", err)
	}
	if err := f.store.Unshare(ctx, f.bob.ID, s.ID); !errors.Is(err, paste.ErrNotFound) {
		t.Errorf("Unshare as another user = %v, want ErrNotFound", err)
	}

	// And it is genuinely still there for its owner.
	if _, err := f.store.ByID(ctx, f.alice.ID, s.ID); err != nil {
		t.Errorf("the owner lost access: %v", err)
	}
}

func TestListIsNewestFirstAndOwnerScoped(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	base := time.Date(2026, 8, 18, 12, 0, 0, 0, time.UTC)
	for i, title := range []string{"first", "second", "third"} {
		f.store.SetClock(func() time.Time { return base.Add(time.Duration(i) * time.Minute) })
		if _, err := f.store.Create(ctx, f.alice.ID, title, "text", "body\n"); err != nil {
			t.Fatal(err)
		}
	}
	f.store.SetClock(func() time.Time { return base })
	if _, err := f.store.Create(ctx, f.bob.ID, "bob's", "text", "body\n"); err != nil {
		t.Fatal(err)
	}

	got, err := f.store.List(ctx, f.alice.ID, 0)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	var titles []string
	for _, s := range got {
		titles = append(titles, s.Title)
	}
	if want := "third,second,first"; strings.Join(titles, ",") != want {
		t.Errorf("List order = %q, want %q", strings.Join(titles, ","), want)
	}
	for _, s := range got {
		if s.UserID != f.alice.ID {
			t.Errorf("List returned another user's snippet: %+v", s)
		}
	}
}

func TestListLimit(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	for range 5 {
		if _, err := f.store.Create(ctx, f.alice.ID, "t", "text", "body\n"); err != nil {
			t.Fatal(err)
		}
	}
	got, err := f.store.List(ctx, f.alice.ID, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Errorf("List(limit 2) returned %d", len(got))
	}
	// A silly limit falls back to the default rather than returning nothing.
	if got, err = f.store.List(ctx, f.alice.ID, -1); err != nil || len(got) != 5 {
		t.Errorf("List(limit -1) = %d snippets, %v; want 5", len(got), err)
	}
}

// TestShareMintsAFreshSlugEachTime is the property that makes revocation
// meaningful: a link that was revoked must not come back.
func TestShareAndUnshare(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	s, err := f.store.Create(ctx, f.alice.ID, "shared", "go", "body\n")
	if err != nil {
		t.Fatal(err)
	}

	slug, err := f.store.Share(ctx, f.alice.ID, s.ID)
	if err != nil {
		t.Fatalf("Share: %v", err)
	}
	if len(slug) < 20 {
		t.Errorf("slug %q is too short to be unguessable", slug)
	}

	// Anyone with the slug can read it, with no user id involved.
	byslug, err := f.store.ByShareSlug(ctx, slug)
	if err != nil {
		t.Fatalf("ByShareSlug: %v", err)
	}
	if byslug.ID != s.ID {
		t.Errorf("ByShareSlug returned snippet %d, want %d", byslug.ID, s.ID)
	}
	if !byslug.Shared() {
		t.Error("Shared() is false for a shared snippet")
	}

	if err := f.store.Unshare(ctx, f.alice.ID, s.ID); err != nil {
		t.Fatalf("Unshare: %v", err)
	}
	if _, err := f.store.ByShareSlug(ctx, slug); !errors.Is(err, paste.ErrNotFound) {
		t.Errorf("the revoked slug still resolves: %v", err)
	}

	// Re-sharing must not resurrect the old link.
	second, err := f.store.Share(ctx, f.alice.ID, s.ID)
	if err != nil {
		t.Fatal(err)
	}
	if second == slug {
		t.Error("re-sharing reused the revoked slug")
	}
	if _, err := f.store.ByShareSlug(ctx, slug); !errors.Is(err, paste.ErrNotFound) {
		t.Error("the old slug resolves again after re-sharing")
	}
	if _, err := f.store.ByShareSlug(ctx, second); err != nil {
		t.Errorf("the new slug does not resolve: %v", err)
	}
}

func TestManyUnsharedSnippetsCoexist(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	// share_slug is UNIQUE, so this would fail if unshared rows stored ""
	// instead of NULL.
	for range 5 {
		if _, err := f.store.Create(ctx, f.alice.ID, "t", "text", "body\n"); err != nil {
			t.Fatalf("a second unshared snippet was rejected: %v", err)
		}
	}
}

func TestByShareSlugRejectsEmptyAndUnknown(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	for _, slug := range []string{"", "never-minted"} {
		if _, err := f.store.ByShareSlug(ctx, slug); !errors.Is(err, paste.ErrNotFound) {
			t.Errorf("ByShareSlug(%q) = %v, want ErrNotFound", slug, err)
		}
	}
}

func TestDelete(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	s, err := f.store.Create(ctx, f.alice.ID, "doomed", "text", "body\n")
	if err != nil {
		t.Fatal(err)
	}
	if err := f.store.Delete(ctx, f.alice.ID, s.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := f.store.ByID(ctx, f.alice.ID, s.ID); !errors.Is(err, paste.ErrNotFound) {
		t.Error("the snippet survived deletion")
	}
	// Deleting again is ErrNotFound rather than a silent success, so a
	// double-submitted form does not look like it worked twice.
	if err := f.store.Delete(ctx, f.alice.ID, s.ID); !errors.Is(err, paste.ErrNotFound) {
		t.Errorf("second Delete = %v, want ErrNotFound", err)
	}
}

// TestDeletingAUserRemovesTheirSnippets exercises the cascade, which only
// works because Plan 1 turned foreign keys on.
func TestDeletingAUserRemovesTheirSnippets(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	if _, err := f.store.Create(ctx, f.bob.ID, "bob's", "text", "body\n"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.ExecContext(ctx, "DELETE FROM users WHERE id = ?", f.bob.ID); err != nil {
		t.Fatal(err)
	}
	got, err := f.store.List(ctx, f.bob.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("%d snippets survived their owner", len(got))
	}
}

func TestValidate(t *testing.T) {
	tests := []struct {
		name    string
		title   string
		body    string
		wantErr bool
	}{
		{"ordinary", "hello", "some text\n", false},
		{"empty title is allowed", "", "some text\n", false},
		{"empty body", "t", "", true},
		{"whitespace-only body", "t", "   \n\t\n", true},
		{"title at the limit", strings.Repeat("a", paste.MaxTitleRunes), "x\n", false},
		{"title over the limit", strings.Repeat("a", paste.MaxTitleRunes+1), "x\n", true},
		{"unicode title counted in runes", strings.Repeat("é", paste.MaxTitleRunes), "x\n", false},
		{"body over the limit", "t", strings.Repeat("x", paste.MaxBodyBytes+1), true},
		{"invalid utf-8 body", "t", string([]byte{0xff, 0xfe}), true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := paste.Validate(tt.title, tt.body)
			if tt.wantErr && !errors.Is(err, paste.ErrInvalid) {
				t.Errorf("Validate = %v, want ErrInvalid", err)
			}
			if !tt.wantErr && err != nil {
				t.Errorf("Validate rejected a valid snippet: %v", err)
			}
		})
	}
}

func TestCreateRejectsInvalidInput(t *testing.T) {
	f := newFixture(t)
	if _, err := f.store.Create(context.Background(), f.alice.ID, "t", "go", ""); !errors.Is(err, paste.ErrInvalid) {
		t.Errorf("Create with an empty body = %v, want ErrInvalid", err)
	}
}

func TestSnippetHelpers(t *testing.T) {
	tests := []struct {
		body      string
		wantLines int
	}{
		{"", 0},
		{"one\n", 1},
		{"one", 1},
		{"one\ntwo\n", 2},
		{"one\ntwo\nthree", 3},
	}
	for _, tt := range tests {
		if got := (paste.Snippet{Body: tt.body}).Lines(); got != tt.wantLines {
			t.Errorf("Lines(%q) = %d, want %d", tt.body, got, tt.wantLines)
		}
	}

	if got := (paste.Snippet{Title: "  "}).DisplayTitle(); got != "Untitled" {
		t.Errorf("DisplayTitle for a blank title = %q, want Untitled", got)
	}
	if got := (paste.Snippet{Title: "Real"}).DisplayTitle(); got != "Real" {
		t.Errorf("DisplayTitle = %q", got)
	}
}
```

- [ ] **Step 4: Run the tests**

```bash
go test ./internal/apps/paste/ -race -v
```

Expected: all PASS. The app is not registered yet, so nothing else changes.

- [ ] **Step 5: Commit**

```bash
git add internal/apps/paste
git commit -m "Add the ON Paste snippet schema and store

Every owner query is scoped by user_id in SQL and returns ErrNotFound for
someone else's snippet rather than a 403, because a 403 would confirm that the
snippet exists.

share_slug is NULL until shared and UNIQUE, which works because SQLite treats
NULLs as distinct. Re-sharing mints a fresh slug instead of reusing the old
one, so a revoked link stays dead even if sharing is turned back on.

The store imports neither net/http nor Chroma, so it is testable against a
real database with no server and no highlighting involved."
```

---
# Task 16: The app, syntax highlighting, create and view

**Files:**
- Create: `internal/apps/paste/paste.go`
- Create: `internal/apps/paste/highlight.go`
- Create: `internal/apps/paste/handlers.go`
- Create: `internal/apps/paste/templates/new.html`, `internal/apps/paste/templates/view.html`
- Modify: `internal/ui/templates/base.html` (add a `head` block)
- Modify: `internal/platform/app/app.go` (fix a nil-pointer panic)
- Modify: `cmd/onsuite/main.go` (register the app)
- Modify: `internal/ui/static/app.css` (snippet styling)
- Test: `internal/apps/paste/highlight_test.go`, `internal/apps/paste/handlers_test.go`, `internal/platform/app/app_test.go`

**Interfaces:**
- Consumes: `app.App`, `app.Router`, `app.Deps`, `render.Page`, `web.Errors`, `web.UserFrom`, `web.CSRFToken`.
- Produces:
  - `paste.New() *paste.App` implementing `app.App` plus `Templates() fs.FS`
  - `paste.Languages() []paste.Language`, `paste.Language{Value, Label string}`, `paste.IsLanguage(string) bool`
  - `paste.Highlight(body, language string) template.HTML`
  - `paste.HighlightCSS() ([]byte, error)`

**Two verified facts that shape this task:**

1. **Chroma's default formatter emits inline `style=` attributes**, which Plan 2's CSP blocks — the page would render entirely unstyled with a console full of violations. `chromahtml.WithClasses(true)` plus a served stylesheet is the only configuration that works. Confirmed by running both.
2. **Chroma escapes HTML in the source it highlights.** `<b>there</b>` inside a snippet comes out as `&lt;b&gt;`. That is what makes it safe to return `template.HTML`, and there is a test below that will catch it if it ever stops being true.

**Two defects in Plan 2 that this task fixes**, both found while building the first real app:

- `Registry.Mount` calls `Render.AddApp` with whatever `Templates()` returns. An app that does not implement it gets a **nil-pointer dereference inside `fs.Glob`** rather than a readable error. Verified: `fs.Glob(nil, "*.html")` panics.
- `base.html` gives a page no way to add anything to `<head>`. ON Paste needs one stylesheet link. This is the first genuine platform gap the app framework has exposed, and the spec says to treat that as a defect worth naming — see the note at the end of this task.

- [ ] **Step 1: Add the dependency**

```bash
go get github.com/alecthomas/chroma/v2@v2.27.0
go mod tidy
```

- [ ] **Step 2: Fix the nil-templates panic**

In `internal/platform/app/app.go`, inside `Mount`, replace:

```go
		if err := deps.Render.AddApp(m.ID, appTemplates(a)); err != nil {
			return err
		}
```

with:

```go
		templates := appTemplates(a)
		if templates == nil {
			// Without this, fs.Glob dereferences a nil interface and the
			// server dies with a runtime panic instead of saying what is wrong.
			return fmt.Errorf("app: %q must implement Templates() fs.FS returning a non-nil filesystem", m.ID)
		}
		if err := deps.Render.AddApp(m.ID, templates); err != nil {
			return err
		}
```

- [ ] **Step 3: Add the `head` block to the layout**

In `internal/ui/templates/base.html`, insert the block immediately before `</head>`:

```html
<script src="{{asset "htmx.min.js"}}" defer></script>
{{block "head" .}}{{end}}
</head>
```

- [ ] **Step 4: Write the highlighter**

Create `internal/apps/paste/highlight.go`:

```go
package paste

import (
	"bytes"
	"fmt"
	"html/template"
	"sync"

	chromahtml "github.com/alecthomas/chroma/v2/formatters/html"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/alecthomas/chroma/v2/styles"
)

// Language is one entry in the language picker.
type Language struct {
	// Value is a Chroma lexer name or alias, or "" meaning detect.
	Value string
	Label string
}

// languageChoices is a curated shortlist. Chroma ships nearly 300 lexers,
// which is an unusable dropdown; these are the ones actually worth offering.
// A test asserts every Value here resolves to a real lexer, so a typo fails
// the build rather than silently falling back to plain text.
var languageChoices = []Language{
	{"", "Detect automatically"},
	{"plaintext", "Plain text"},
	{"bash", "Shell"},
	{"c", "C"},
	{"cpp", "C++"},
	{"css", "CSS"},
	{"diff", "Diff"},
	{"docker", "Dockerfile"},
	{"go", "Go"},
	{"html", "HTML"},
	{"ini", "INI / TOML"},
	{"java", "Java"},
	{"javascript", "JavaScript"},
	{"json", "JSON"},
	{"lua", "Lua"},
	{"markdown", "Markdown"},
	{"nginx", "nginx"},
	{"php", "PHP"},
	{"powershell", "PowerShell"},
	{"python", "Python"},
	{"ruby", "Ruby"},
	{"rust", "Rust"},
	{"sql", "SQL"},
	{"terraform", "Terraform"},
	{"typescript", "TypeScript"},
	{"xml", "XML"},
	{"yaml", "YAML"},
}

// Languages returns the picker contents.
func Languages() []Language {
	out := make([]Language, len(languageChoices))
	copy(out, languageChoices)
	return out
}

// IsLanguage reports whether v is an offered choice. The handler uses this so
// a hand-crafted form post cannot store an arbitrary string.
func IsLanguage(v string) bool {
	for _, l := range languageChoices {
		if l.Value == v {
			return true
		}
	}
	return false
}

// LanguageLabel is the display name for a stored value.
func LanguageLabel(v string) string {
	for _, l := range languageChoices {
		if l.Value == v {
			return l.Label
		}
	}
	return "Plain text"
}

const (
	// Chroma style names. Both are needed because the stylesheet carries a
	// light and a dark variant.
	styleLight = "github"
	styleDark  = "github-dark"
)

var (
	formatterOnce sync.Once
	formatter     *chromahtml.Formatter
)

// htmlFormatter is configured in class mode.
//
// This is not a preference. Chroma's default formatter writes inline style
// attributes, and the suite's Content-Security-Policy forbids inline styles, so
// the default produces a completely unstyled page. Classes plus a served
// stylesheet is the only configuration that works here.
func htmlFormatter() *chromahtml.Formatter {
	formatterOnce.Do(func() {
		formatter = chromahtml.New(
			chromahtml.WithClasses(true),
			chromahtml.WithLineNumbers(true),
			chromahtml.LineNumbersInTable(true),
		)
	})
	return formatter
}

// Highlight renders body as highlighted HTML.
//
// The result is template.HTML, meaning it is inserted without escaping. That is
// safe because Chroma escapes the source it tokenises — `<b>` in a snippet
// becomes `&lt;b&gt;`. TestHighlightEscapesHTML guards exactly that, and it is
// the reason this function is the only place in the suite that returns
// pre-trusted markup.
func Highlight(body, language string) template.HTML {
	lexer := lexers.Get(language)
	if lexer == nil && language == "" {
		lexer = lexers.Analyse(body)
	}
	if lexer == nil {
		lexer = lexers.Fallback
	}

	iterator, err := lexer.Tokenise(nil, body)
	if err != nil {
		// Tokenising cannot fail for well-formed UTF-8, which the store
		// guarantees. Fall back to escaped plain text rather than losing the
		// snippet entirely.
		return plainFallback(body)
	}

	var buf bytes.Buffer
	if err := htmlFormatter().Format(&buf, styles.Get(styleLight), iterator); err != nil {
		return plainFallback(body)
	}
	return template.HTML(buf.String())
}

// plainFallback renders the body as escaped plain text inside a <pre>.
func plainFallback(body string) template.HTML {
	var buf bytes.Buffer
	buf.WriteString(`<pre class="chroma">`)
	buf.WriteString(template.HTMLEscapeString(body))
	buf.WriteString("</pre>")
	return template.HTML(buf.String())
}

// highlightOverrides let the suite's own tokens own the surface, while Chroma
// owns only the token colours. They are emitted after Chroma's rules and so
// win at equal specificity, with no !important needed.
const highlightOverrides = `
/* ON Suite overrides: Chroma colours the tokens, the design system owns the
   surface, so a highlighted block matches every other panel in the suite. */
.chroma { background-color: transparent; }
.chroma .lntable { width: 100%; margin: 0; padding: 0; border: none; border-spacing: 0; }
.chroma .lntd { padding: 0; border: none; vertical-align: top; }
.chroma .lntd:first-child { width: 1%; padding-right: var(--s-3); }
.chroma .lnt { color: var(--c-text-faint); user-select: none; }
`

// HighlightCSS builds the stylesheet for the classes Highlight emits.
//
// It is generated rather than committed so it can never drift from the Chroma
// version in go.mod. The dark variant is wrapped in a preference query, and
// because it comes second it wins at equal specificity in dark mode.
func HighlightCSS() ([]byte, error) {
	var buf bytes.Buffer

	buf.WriteString("/* Generated at startup by ON Paste from Chroma. Do not edit. */\n")
	if err := htmlFormatter().WriteCSS(&buf, styles.Get(styleLight)); err != nil {
		return nil, fmt.Errorf("paste: write light highlight css: %w", err)
	}

	buf.WriteString("\n@media (prefers-color-scheme: dark) {\n")
	if err := htmlFormatter().WriteCSS(&buf, styles.Get(styleDark)); err != nil {
		return nil, fmt.Errorf("paste: write dark highlight css: %w", err)
	}
	buf.WriteString("}\n")

	buf.WriteString(highlightOverrides)
	return buf.Bytes(), nil
}
```

- [ ] **Step 5: Write the app**

Create `internal/apps/paste/paste.go`:

```go
package paste

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"io/fs"
	"net/http"

	"github.com/iliafrenkel/on-suite/internal/platform/app"
)

//go:embed templates/*.html
var templateFiles embed.FS

// App is ON Paste. It is constructed before the platform exists, in the
// registration slice in main, and receives everything it needs in Mount.
type App struct {
	store *Store
	deps  app.Deps

	css     []byte
	cssETag string
}

// New returns the app for registration.
func New() *App { return &App{} }

func (a *App) Meta() app.Meta {
	return app.Meta{
		ID:      ID,
		Name:    "ON Paste",
		Summary: "Keep and share snippets of code or text.",
		Order:   20,
	}
}

func (a *App) Migrations() fs.FS { return Migrations() }

func (a *App) Templates() fs.FS {
	sub, err := fs.Sub(templateFiles, "templates")
	if err != nil {
		// Unreachable: a compile-time constant path checked by go:embed.
		panic("paste: embedded templates missing: " + err.Error())
	}
	return sub
}

// Mount wires the app up. Everything registered with Handle requires a signed
// in user; the three Public routes are the deliberate exceptions.
func (a *App) Mount(r *app.Router, deps app.Deps) {
	a.deps = deps
	a.store = NewStore(deps.DB)

	css, err := HighlightCSS()
	if err != nil {
		// At startup, with no request in flight, there is nothing to degrade
		// to: the app cannot render a snippet without its stylesheet.
		panic("paste: generating the highlight stylesheet failed: " + err.Error())
	}
	a.css = css
	sum := sha256.Sum256(css)
	a.cssETag = `"` + hex.EncodeToString(sum[:])[:16] + `"`

	// See the route map at the top of this plan before changing any pattern:
	// several obvious-looking alternatives make ServeMux panic at startup.
	r.HandleFunc("GET /{$}", a.list)
	r.HandleFunc("GET /new", a.newForm)
	r.HandleFunc("POST /new", a.create)
	r.HandleFunc("GET /{id}", a.view)
	r.HandleFunc("GET /raw/{id}", a.raw)
	r.HandleFunc("POST /{id}/delete", a.delete)
	r.HandleFunc("POST /{id}/share", a.share)
	r.HandleFunc("POST /{id}/unshare", a.unshare)

	// The stylesheet must load on the shared page too, where nobody is signed
	// in, so it is public. It contains no user data.
	r.PublicFunc("GET /highlight.css", a.highlightCSS)
	r.PublicFunc("GET /s/{slug}", a.viewShared)
	r.PublicFunc("GET /s/{slug}/raw", a.rawShared)
}

// highlightCSS serves the generated stylesheet. It revalidates rather than
// caching for a fixed period, so upgrading Chroma takes effect on the next
// request instead of whenever a max-age happens to lapse.
func (a *App) highlightCSS(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/css; charset=utf-8")
	w.Header().Set("ETag", a.cssETag)
	w.Header().Set("Cache-Control", "no-cache")

	if match := r.Header.Get("If-None-Match"); match == a.cssETag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	if _, err := w.Write(a.css); err != nil {
		a.deps.Log.Error("writing the highlight stylesheet failed", "error", err)
	}
}
```

- [ ] **Step 6: Write the create and view handlers**

Create `internal/apps/paste/handlers.go`:

```go
package paste

import (
	"errors"
	"html/template"
	"net/http"
	"strconv"

	"github.com/iliafrenkel/on-suite/internal/platform/render"
	"github.com/iliafrenkel/on-suite/internal/platform/web"
)

// viewModel is what the view and shared templates render. It carries the
// already-highlighted body so a template never calls into Chroma.
type viewModel struct {
	Snippet   Snippet
	Highlight template.HTML
	Language  string
	ShareURL  string
	RawURL    string
	// Owner is true on the owner's own view, false on a shared page, which is
	// what decides whether the destructive controls render at all.
	Owner bool
}

// userID is the signed-in user's id. Handlers registered with Handle are
// guarded, so a missing user is a programming error rather than a bad request.
func (a *App) userID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	u, ok := web.UserFrom(r.Context())
	if !ok {
		a.deps.Errors.Status(w, r, http.StatusUnauthorized)
		return 0, false
	}
	return u.ID, true
}

// snippetID parses the {id} wildcard.
func (a *App) snippetID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		a.deps.Errors.Status(w, r, http.StatusNotFound)
		return 0, false
	}
	return id, true
}

// fail maps a store error onto a response. ErrNotFound becomes a 404 whether
// the snippet is missing or simply someone else's, so the two are
// indistinguishable from outside.
func (a *App) fail(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, ErrNotFound):
		a.deps.Errors.Status(w, r, http.StatusNotFound)
	case errors.Is(err, ErrInvalid):
		a.deps.Errors.Status(w, r, http.StatusBadRequest)
	default:
		a.deps.Errors.Internal(w, r, err)
	}
}

func (a *App) newForm(w http.ResponseWriter, r *http.Request) {
	a.renderNew(w, r, http.StatusOK, "", "", "", "")
}

func (a *App) create(w http.ResponseWriter, r *http.Request) {
	userID, ok := a.userID(w, r)
	if !ok {
		return
	}

	title := r.PostFormValue("title")
	language := r.PostFormValue("language")
	body := r.PostFormValue("body")

	// Reject a language that is not on the offered list, so a hand-crafted
	// post cannot store an arbitrary string that later renders as plain text
	// with no explanation.
	if !IsLanguage(language) {
		a.renderNew(w, r, http.StatusBadRequest, "That is not a language I know.", title, language, body)
		return
	}
	if err := Validate(title, body); err != nil {
		a.renderNew(w, r, http.StatusBadRequest, userMessage(err), title, language, body)
		return
	}

	s, err := a.store.Create(r.Context(), userID, title, language, body)
	if err != nil {
		if errors.Is(err, ErrInvalid) {
			a.renderNew(w, r, http.StatusBadRequest, userMessage(err), title, language, body)
			return
		}
		a.deps.Errors.Internal(w, r, err)
		return
	}

	http.Redirect(w, r, "/paste/"+strconv.FormatInt(s.ID, 10), http.StatusSeeOther)
}

func (a *App) view(w http.ResponseWriter, r *http.Request) {
	userID, ok := a.userID(w, r)
	if !ok {
		return
	}
	id, ok := a.snippetID(w, r)
	if !ok {
		return
	}

	s, err := a.store.ByID(r.Context(), userID, id)
	if err != nil {
		a.fail(w, r, err)
		return
	}

	page := a.deps.Page(r, s.DisplayTitle())
	page.Data = viewModel{
		Snippet:   s,
		Highlight: Highlight(s.Body, s.Language),
		Language:  LanguageLabel(s.Language),
		RawURL:    "/paste/raw/" + strconv.FormatInt(s.ID, 10),
		ShareURL:  shareURL(s),
		Owner:     true,
	}
	a.render(w, r, http.StatusOK, "paste/view", page)
}

// renderNew draws the create form, carrying back whatever the user typed so a
// validation failure never loses their snippet.
func (a *App) renderNew(w http.ResponseWriter, r *http.Request, status int, message, title, language, body string) {
	page := a.deps.Page(r, "New snippet")
	page.Data = map[string]any{
		"Error":     message,
		"Title":     title,
		"Language":  language,
		"Body":      body,
		"Languages": Languages(),
	}
	a.render(w, r, status, "paste/new", page)
}

func (a *App) render(w http.ResponseWriter, r *http.Request, status int, name string, page render.Page) {
	if err := a.deps.Render.Page(w, status, name, page); err != nil {
		a.deps.Errors.Internal(w, r, err)
	}
}

// shareURL is the public path for a shared snippet, or "" when it is private.
func shareURL(s Snippet) string {
	if !s.Shared() {
		return ""
	}
	return "/paste/s/" + s.ShareSlug
}

// userMessage strips the error's package prefix so the wording reads as a
// sentence to the person who typed the form.
func userMessage(err error) string {
	msg := err.Error()
	for _, prefix := range []string{"paste: invalid snippet: ", "paste: "} {
		if len(msg) > len(prefix) && msg[:len(prefix)] == prefix {
			return upperFirst(msg[len(prefix):])
		}
	}
	return upperFirst(msg)
}

func upperFirst(s string) string {
	if s == "" {
		return s
	}
	b := []rune(s)
	if b[0] >= 'a' && b[0] <= 'z' {
		b[0] -= 32
	}
	return string(b)
}
```

Handlers `list`, `raw`, `delete`, `share`, `unshare`, `viewShared` and `rawShared` are referenced by `Mount` and arrive in Tasks 17 and 18. To keep this task compiling on its own, add temporary stubs at the bottom of `handlers.go`:

```go
// Stubs replaced in Tasks 17 and 18. They exist so this task compiles and its
// routes are registered in the shape the route map specifies.
func (a *App) list(w http.ResponseWriter, r *http.Request) {
	a.deps.Errors.Status(w, r, http.StatusNotFound)
}
func (a *App) raw(w http.ResponseWriter, r *http.Request)        { a.deps.Errors.Status(w, r, http.StatusNotFound) }
func (a *App) delete(w http.ResponseWriter, r *http.Request)     { a.deps.Errors.Status(w, r, http.StatusNotFound) }
func (a *App) share(w http.ResponseWriter, r *http.Request)      { a.deps.Errors.Status(w, r, http.StatusNotFound) }
func (a *App) unshare(w http.ResponseWriter, r *http.Request)    { a.deps.Errors.Status(w, r, http.StatusNotFound) }
func (a *App) viewShared(w http.ResponseWriter, r *http.Request) { a.deps.Errors.Status(w, r, http.StatusNotFound) }
func (a *App) rawShared(w http.ResponseWriter, r *http.Request)  { a.deps.Errors.Status(w, r, http.StatusNotFound) }
```

- [ ] **Step 7: Write the templates**

Create `internal/apps/paste/templates/new.html`:

```html
{{define "content"}}
<div class="measure stack">
	<h1>New snippet</h1>

	{{with .Data.Error}}
	<div class="notice notice-error" role="alert">{{.}}</div>
	{{end}}

	<form method="post" action="/paste/new" class="stack">
		<input type="hidden" name="csrf_token" value="{{.Shell.CSRFToken}}">

		<div class="field">
			<label for="title">Title <span class="faint">(optional)</span></label>
			<input id="title" name="title" type="text" value="{{.Data.Title}}"
			       autocomplete="off" spellcheck="false">
		</div>

		<div class="field">
			<label for="language">Language</label>
			<select id="language" name="language">
				{{range .Data.Languages}}
				<option value="{{.Value}}"{{if eq .Value $.Data.Language}} selected{{end}}>{{.Label}}</option>
				{{end}}
			</select>
		</div>

		<div class="field">
			<label for="body">Snippet</label>
			<textarea id="body" name="body" rows="20" required
			          spellcheck="false" autocapitalize="none">{{.Data.Body}}</textarea>
		</div>

		<div class="row">
			<button type="submit">Save snippet</button>
			<a href="/paste/">Cancel</a>
		</div>
	</form>
</div>
{{end}}
```

Create `internal/apps/paste/templates/view.html`:

```html
{{define "head"}}
<link rel="stylesheet" href="/paste/highlight.css">
{{end}}

{{define "content"}}
{{$s := .Data.Snippet}}
<div class="stack">
	<div class="snippet-head">
		<h1>{{$s.DisplayTitle}}</h1>
		<p class="faint">
			{{.Data.Language}} · {{$s.Lines}} lines · saved {{$s.CreatedAt.Format "2 Jan 2006 15:04"}}
		</p>
	</div>

	{{if .Data.Owner}}
	<div class="row snippet-actions">
		<a class="button" href="{{.Data.RawURL}}">Raw</a>

		{{if $s.Shared}}
		<form method="post" action="/paste/{{$s.ID}}/unshare">
			<input type="hidden" name="csrf_token" value="{{.Shell.CSRFToken}}">
			<button type="submit" class="quiet">Stop sharing</button>
		</form>
		{{else}}
		<form method="post" action="/paste/{{$s.ID}}/share">
			<input type="hidden" name="csrf_token" value="{{.Shell.CSRFToken}}">
			<button type="submit" class="quiet">Share</button>
		</form>
		{{end}}

		<form method="post" action="/paste/{{$s.ID}}/delete">
			<input type="hidden" name="csrf_token" value="{{.Shell.CSRFToken}}">
			<button type="submit" class="quiet danger">Delete</button>
		</form>
	</div>

	{{with .Data.ShareURL}}
	<div class="notice">
		Anyone with this link can read this snippet:
		<code>{{.}}</code>
	</div>
	{{end}}
	{{end}}

	<div class="scroll-x snippet-body">{{.Data.Highlight}}</div>
</div>
{{end}}
```

- [ ] **Step 8: Add the snippet styling**

Append to `internal/ui/static/app.css`:

```css
/* ---- Snippets ---------------------------------------------------------- */

.snippet-head h1 { margin-bottom: var(--s-1); }
.snippet-head p { margin: 0; }

.snippet-actions { flex-wrap: wrap; }
.snippet-actions form { display: inline; }

button.danger { color: var(--c-danger); }
button.danger:hover { background: var(--c-danger-bg); }

.snippet-body {
	border: var(--border);
	border-radius: var(--radius);
	background: var(--c-bg-subtle);
	padding: var(--s-2) var(--s-3);
	font-size: var(--fs-sm);
}

.snippet-body pre { margin: 0; padding: 0; border: none; background: none; }

.notice code {
	word-break: break-all;
	background: var(--c-bg-inset);
	padding: 1px var(--s-1);
	border-radius: var(--radius);
}
```

- [ ] **Step 9: Register the app**

In `cmd/onsuite/main.go`, fill in the registration slice and add the import:

```go
func registeredApps() []app.App {
	return []app.App{
		paste.New(),
	}
}
```

```go
	"github.com/iliafrenkel/on-suite/internal/apps/paste"
	"github.com/iliafrenkel/on-suite/internal/platform/app"
```

- [ ] **Step 10: Test the highlighter**

Create `internal/apps/paste/highlight_test.go`:

```go
package paste_test

import (
	"strings"
	"testing"

	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/iliafrenkel/on-suite/internal/apps/paste"
)

// TestEveryOfferedLanguageResolves turns a typo in the curated list into a
// test failure instead of a snippet that silently renders as plain text.
func TestEveryOfferedLanguageResolves(t *testing.T) {
	for _, l := range paste.Languages() {
		if l.Value == "" {
			continue // the detect-automatically entry
		}
		if lexers.Get(l.Value) == nil {
			t.Errorf("language %q (%s) does not resolve to a Chroma lexer", l.Value, l.Label)
		}
		if l.Label == "" {
			t.Errorf("language %q has no label", l.Value)
		}
	}
}

// TestHighlightEmitsNoInlineStyles is the load-bearing test of this task. The
// suite's CSP forbids inline styles, so Chroma's default formatter would
// produce a completely unstyled page. If this fails, the page silently loses
// all highlighting in a browser while every other test still passes.
func TestHighlightEmitsNoInlineStyles(t *testing.T) {
	out := string(paste.Highlight("package main\n\nfunc main() {}\n", "go"))

	if strings.Contains(out, "style=") {
		t.Fatalf("Highlight emitted an inline style attribute, which the CSP blocks:\n%s", out)
	}
	if !strings.Contains(out, "class=") {
		t.Fatalf("Highlight emitted no classes, so the stylesheet cannot colour it:\n%s", out)
	}
}

// TestHighlightEscapesHTML guards the reason Highlight may return
// template.HTML at all.
func TestHighlightEscapesHTML(t *testing.T) {
	const hostile = "var x = \"<script>alert(1)</script>\";\n"
	out := string(paste.Highlight(hostile, "javascript"))

	if strings.Contains(out, "<script>") {
		t.Fatalf("a script tag survived highlighting unescaped:\n%s", out)
	}
	if !strings.Contains(out, "&lt;script&gt;") {
		t.Errorf("the escaped form is missing; output was:\n%s", out)
	}
}

func TestHighlightHandlesAwkwardInput(t *testing.T) {
	tests := []struct{ name, body, language string }{
		{"empty body", "", "go"},
		{"unknown language", "hello\n", "klingon"},
		{"detect from content", "package main\nfunc main() {}\n", ""},
		{"plain text", "just words\n", "plaintext"},
		{"no trailing newline", "x = 1", "python"},
		{"unicode", "s := \"日本語 🎉\"\n", "go"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out := string(paste.Highlight(tt.body, tt.language))
			if strings.Contains(out, "style=") {
				t.Error("inline style attribute in output")
			}
			// It must always produce something renderable.
			if tt.body != "" && out == "" {
				t.Error("Highlight returned nothing for a non-empty body")
			}
		})
	}
}

func TestHighlightCSS(t *testing.T) {
	css, err := paste.HighlightCSS()
	if err != nil {
		t.Fatalf("HighlightCSS: %v", err)
	}
	s := string(css)

	if len(css) < 1000 {
		t.Errorf("stylesheet is only %d bytes, which is too small to be real", len(css))
	}
	if !strings.Contains(s, ".chroma") {
		t.Error("stylesheet does not mention .chroma")
	}
	if !strings.Contains(s, "prefers-color-scheme: dark") {
		t.Error("stylesheet has no dark variant")
	}
	// The overrides must come after Chroma's own rules, or the background
	// fights the design system.
	if strings.Index(s, "ON Suite overrides") < strings.LastIndex(s, "prefers-color-scheme") {
		t.Error("the overrides are not last, so they will not win at equal specificity")
	}
	// Balanced braces, since the dark block is hand-wrapped in a media query.
	if strings.Count(s, "{") != strings.Count(s, "}") {
		t.Errorf("unbalanced braces: %d open, %d close", strings.Count(s, "{"), strings.Count(s, "}"))
	}
}

func TestLanguageLabel(t *testing.T) {
	if got := paste.LanguageLabel("go"); got != "Go" {
		t.Errorf("LanguageLabel(go) = %q", got)
	}
	if got := paste.LanguageLabel("klingon"); got != "Plain text" {
		t.Errorf("LanguageLabel of an unknown value = %q, want a safe default", got)
	}
}

func TestIsLanguage(t *testing.T) {
	for _, v := range []string{"", "go", "plaintext", "yaml"} {
		if !paste.IsLanguage(v) {
			t.Errorf("IsLanguage(%q) = false", v)
		}
	}
	for _, v := range []string{"klingon", "GO", "go ", "'; DROP TABLE"} {
		if paste.IsLanguage(v) {
			t.Errorf("IsLanguage(%q) = true", v)
		}
	}
}
```

- [ ] **Step 11: Test create and view through the real stack**

Create `internal/apps/paste/handlers_test.go`. This builds the whole server the way `buildStack` does, so the tests exercise CSRF, authentication, the router's default-deny and the templates together.

```go
package paste_test

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"github.com/iliafrenkel/on-suite/internal/apps/paste"
	"github.com/iliafrenkel/on-suite/internal/htmlassert"
	"github.com/iliafrenkel/on-suite/internal/platform/app"
	"github.com/iliafrenkel/on-suite/internal/platform/auth"
	"github.com/iliafrenkel/on-suite/internal/platform/db"
	"github.com/iliafrenkel/on-suite/internal/platform/render"
	"github.com/iliafrenkel/on-suite/internal/platform/web"
	"github.com/iliafrenkel/on-suite/internal/ui"
)

const testPassword = "a-sufficiently-long-password"

// server is the whole stack over a real database, with two accounts.
type server struct {
	handler http.Handler
	store   *paste.Store
	alice   *session
	bob     *session
}

// session holds one signed-in browser's cookies.
type session struct {
	user    auth.User
	cookies []*http.Cookie
}

func newServer(t *testing.T) *server {
	t.Helper()
	ctx := context.Background()

	handle, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = handle.Close() })

	registry, err := app.NewRegistry(paste.New())
	if err != nil {
		t.Fatal(err)
	}

	migrations, err := db.Collect(auth.Namespace, auth.Migrations())
	if err != nil {
		t.Fatal(err)
	}
	appMigrations, err := registry.Migrations()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Apply(ctx, handle, append(migrations, appMigrations...)); err != nil {
		t.Fatal(err)
	}

	users := auth.NewStore(handle)
	assets, err := web.NewAssets(ui.Static(), "/static")
	if err != nil {
		t.Fatal(err)
	}
	rend, err := render.NewRenderer(render.Options{Layouts: ui.Templates(), AssetURL: assets.URL})
	if err != nil {
		t.Fatal(err)
	}

	log := slog.New(slog.DiscardHandler)
	errs := web.NewErrors(rend, log)
	csrf := web.NewCSRF(false, errs)
	authn := web.NewAuth(web.AuthOptions{
		Users: users, Render: rend, Errors: errs, CSRF: csrf, Log: log, Secure: false,
	})

	mux := http.NewServeMux()
	authn.Routes(mux)
	if err := registry.Mount(mux, app.Deps{
		DB: handle, Render: rend, Users: users, Errors: errs, Log: log,
	}, authn.RequireUser); err != nil {
		t.Fatalf("Mount: %v", err)
	}
	mux.Handle("/", http.HandlerFunc(errs.NotFound))

	s := &server{
		handler: web.Chain(mux, csrf.Middleware, authn.LoadUser),
		store:   paste.NewStore(handle),
	}

	hash, err := auth.HashPassword(testPassword)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"alice", "bob"} {
		u, err := users.CreateUser(ctx, name, hash, false)
		if err != nil {
			t.Fatal(err)
		}
		sess := s.logIn(t, u)
		if name == "alice" {
			s.alice = sess
		} else {
			s.bob = sess
		}
	}
	return s
}

// do issues a request carrying a session's cookies.
func (s *server) do(t *testing.T, sess *session, req *http.Request) *httptest.ResponseRecorder {
	t.Helper()
	if sess != nil {
		for _, c := range sess.cookies {
			req.AddCookie(c)
		}
	}
	rec := httptest.NewRecorder()
	s.handler.ServeHTTP(rec, req)
	return rec
}

// post submits a form with the session's CSRF token attached.
func (s *server) post(t *testing.T, sess *session, path string, form url.Values) *httptest.ResponseRecorder {
	t.Helper()
	form.Set(web.CSRFFormField, s.csrfToken(t, sess))
	req := httptest.NewRequest("POST", path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return s.do(t, sess, req)
}

func (s *server) csrfToken(t *testing.T, sess *session) string {
	t.Helper()
	for _, c := range sess.cookies {
		if c.Name == web.CSRFCookieName {
			return c.Value
		}
	}
	t.Fatal("session has no CSRF cookie")
	return ""
}

// logIn performs a real login so the tests use genuine session cookies.
func (s *server) logIn(t *testing.T, u auth.User) *session {
	t.Helper()

	page := s.do(t, nil, httptest.NewRequest("GET", "/login", nil))
	var csrfCookie *http.Cookie
	for _, c := range page.Result().Cookies() {
		if c.Name == web.CSRFCookieName {
			csrfCookie = c
		}
	}
	if csrfCookie == nil {
		t.Fatal("GET /login issued no CSRF cookie")
	}

	form := url.Values{
		"username":        {u.Username},
		"password":        {testPassword},
		web.CSRFFormField: {csrfCookie.Value},
	}
	req := httptest.NewRequest("POST", "/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := s.do(t, &session{cookies: []*http.Cookie{csrfCookie}}, req)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("login for %s = %d", u.Username, rec.Code)
	}
	return &session{user: u, cookies: rec.Result().Cookies()}
}

// createSnippet is the shortcut other tests use.
func (s *server) createSnippet(t *testing.T, sess *session, title, language, body string) int64 {
	t.Helper()
	rec := s.post(t, sess, "/paste/new", url.Values{
		"title": {title}, "language": {language}, "body": {body},
	})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("create = %d, want 303; body: %s", rec.Code, rec.Body.String())
	}
	loc := rec.Header().Get("Location")
	var id int64
	if _, err := fmtSscan(loc, &id); err != nil {
		t.Fatalf("cannot read an id out of Location %q: %v", loc, err)
	}
	return id
}

// fmtSscan pulls the trailing id out of "/paste/12".
func fmtSscan(location string, id *int64) (int, error) {
	idx := strings.LastIndex(location, "/")
	if idx < 0 {
		return 0, errNoID
	}
	var n int64
	for _, r := range location[idx+1:] {
		if r < '0' || r > '9' {
			return 0, errNoID
		}
		n = n*10 + int64(r-'0')
	}
	if n == 0 {
		return 0, errNoID
	}
	*id = n
	return 1, nil
}

var errNoID = errorString("no snippet id in the redirect")

type errorString string

func (e errorString) Error() string { return string(e) }

// ---- tests ----------------------------------------------------------------

// TestPasteRequiresSignIn confirms the default-deny router is doing its job for
// a real app, not just the fake one in the framework's own tests.
func TestPasteRequiresSignIn(t *testing.T) {
	s := newServer(t)
	for _, path := range []string{"/paste/", "/paste/new", "/paste/1", "/paste/raw/1"} {
		rec := s.do(t, nil, httptest.NewRequest("GET", path, nil))
		if rec.Code != http.StatusSeeOther {
			t.Errorf("GET %s anonymous = %d, want a 303 to the login page", path, rec.Code)
		}
	}
}

func TestNewFormRenders(t *testing.T) {
	s := newServer(t)
	rec := s.do(t, s.alice, httptest.NewRequest("GET", "/paste/new", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	doc := htmlassert.Parse(t, rec.Body.String())
	doc.MustHave("textarea[name=body]")
	doc.MustHave("input[name=title]")
	doc.MustHave("select[name=language]")
	doc.MustHave("input[name=" + web.CSRFFormField + "]")

	// The active app must be marked in the nav, which the router sets.
	nav := doc.MustHave(`nav.shell-nav a[aria-current=page]`)
	if got := htmlassert.Text(nav); got != "ON Paste" {
		t.Errorf("the marked nav item is %q, want ON Paste", got)
	}
	if n := len(doc.QueryAll("select[name=language] option")); n < 10 {
		t.Errorf("the language picker has only %d options", n)
	}
}

func TestCreateThenView(t *testing.T) {
	s := newServer(t)
	id := s.createSnippet(t, s.alice, "My config", "yaml", "key: value\nother: 2\n")

	rec := s.do(t, s.alice, httptest.NewRequest("GET", "/paste/"+itoa(id), nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("view = %d", rec.Code)
	}
	doc := htmlassert.Parse(t, rec.Body.String())

	if got := htmlassert.Text(doc.MustHave("h1")); got != "My config" {
		t.Errorf("title = %q", got)
	}
	if text := doc.Text(); !strings.Contains(text, "key") {
		t.Errorf("the body is not on the page: %q", text)
	}
	// Highlighted, and via classes rather than inline styles.
	doc.MustHave(".chroma")
	if strings.Contains(rec.Body.String(), "style=") {
		t.Error("the page contains an inline style attribute, which the CSP blocks")
	}
	// The stylesheet the highlighting depends on must be linked.
	if href, _ := htmlassert.Attr(doc.MustHave("link[href=/paste/highlight.css]"), "href"); href == "" {
		t.Error("the highlight stylesheet is not linked")
	}
	// A private snippet offers sharing and shows no link yet.
	doc.MustHave(`form[action=/paste/` + itoa(id) + `/share]`)
	if strings.Contains(doc.Text(), "Anyone with this link") {
		t.Error("a private snippet advertises a share link")
	}
}

// TestViewingSomeoneElsesSnippetIs404 — not a 403, which would confirm it
// exists.
func TestViewingSomeoneElsesSnippetIs404(t *testing.T) {
	s := newServer(t)
	id := s.createSnippet(t, s.alice, "alice's", "go", "secret\n")

	rec := s.do(t, s.bob, httptest.NewRequest("GET", "/paste/"+itoa(id), nil))
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "secret") {
		t.Error("the snippet body leaked to another user")
	}
}

func TestCreateRejectsBadInput(t *testing.T) {
	s := newServer(t)

	tests := []struct {
		name string
		form url.Values
	}{
		{"empty body", url.Values{"title": {"t"}, "language": {"go"}, "body": {""}}},
		{"whitespace body", url.Values{"title": {"t"}, "language": {"go"}, "body": {"  \n"}}},
		{"unknown language", url.Values{"title": {"t"}, "language": {"klingon"}, "body": {"x\n"}}},
		{"oversized title", url.Values{"title": {strings.Repeat("a", paste.MaxTitleRunes + 1)}, "language": {"go"}, "body": {"x\n"}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := s.post(t, s.alice, "/paste/new", tt.form)
			if rec.Code == http.StatusSeeOther {
				t.Fatal("the snippet was created")
			}
			doc := htmlassert.Parse(t, rec.Body.String())
			doc.MustHave(".notice-error")
			// The form must come back populated, or the user loses their work.
			doc.MustHave("textarea[name=body]")
		})
	}
}

// TestCreatePreservesTypedInputOnFailure: losing a long snippet to a
// validation error would be the single most annoying possible bug here.
func TestCreatePreservesTypedInputOnFailure(t *testing.T) {
	s := newServer(t)
	const body = "a long snippet the user does not want to retype\n"

	rec := s.post(t, s.alice, "/paste/new", url.Values{
		"title": {strings.Repeat("a", paste.MaxTitleRunes + 1)},
		"language": {"go"}, "body": {body},
	})
	doc := htmlassert.Parse(t, rec.Body.String())
	if got := htmlassert.Text(doc.MustHave("textarea[name=body]")); !strings.Contains(got, "does not want to retype") {
		t.Errorf("the body was not carried back: %q", got)
	}
}

func TestCreateRequiresCSRF(t *testing.T) {
	s := newServer(t)
	form := url.Values{"title": {"t"}, "language": {"go"}, "body": {"x\n"}}
	req := httptest.NewRequest("POST", "/paste/new", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	rec := s.do(t, s.alice, req) // no token in the form
	if rec.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403", rec.Code)
	}
}

func TestViewRejectsNonNumericAndMissingIDs(t *testing.T) {
	s := newServer(t)
	for _, path := range []string{"/paste/abc", "/paste/0", "/paste/-1", "/paste/99999"} {
		rec := s.do(t, s.alice, httptest.NewRequest("GET", path, nil))
		if rec.Code != http.StatusNotFound {
			t.Errorf("GET %s = %d, want 404", path, rec.Code)
		}
	}
}

// TestSnippetBodyIsEscapedInTheView is the XSS test. A snippet is arbitrary
// text from a user and is rendered through template.HTML, so this must hold.
func TestSnippetBodyIsEscapedInTheView(t *testing.T) {
	s := newServer(t)
	const hostile = "<script>alert('xss')</script>\n"
	id := s.createSnippet(t, s.alice, "<img src=x onerror=alert(1)>", "html", hostile)

	rec := s.do(t, s.alice, httptest.NewRequest("GET", "/paste/"+itoa(id), nil))
	body := rec.Body.String()

	if strings.Contains(body, "<script>alert('xss')</script>") {
		t.Fatal("the snippet body was rendered as live markup")
	}
	// Assert on the tag rather than the payload: html/template escapes the
	// angle brackets, so the inner text survives as inert text and looking for
	// it would fail on correct output.
	if strings.Contains(body, "<img src=x") {
		t.Fatal("the title was rendered as live markup")
	}
	// It must still be visible as text.
	if !strings.Contains(htmlassert.Parse(t, body).Text(), "alert('xss')") {
		t.Error("the escaped snippet disappeared from the page")
	}
}

func TestHighlightStylesheetIsServedAndCacheable(t *testing.T) {
	s := newServer(t)

	// Public: it must load on the shared page, where nobody is signed in.
	rec := s.do(t, nil, httptest.NewRequest("GET", "/paste/highlight.css", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/css") {
		t.Errorf("Content-Type = %q", ct)
	}
	if !strings.Contains(rec.Body.String(), ".chroma") {
		t.Error("the stylesheet has no .chroma rules")
	}

	etag := rec.Header().Get("ETag")
	if etag == "" {
		t.Fatal("no ETag")
	}
	req := httptest.NewRequest("GET", "/paste/highlight.css", nil)
	req.Header.Set("If-None-Match", etag)
	rec = s.do(t, nil, req)
	if rec.Code != http.StatusNotModified {
		t.Errorf("revalidation = %d, want 304", rec.Code)
	}
}

func itoa(n int64) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}
```

- [ ] **Step 12: Test the nil-templates fix**

Add to `internal/platform/app/app_test.go`:

```go
// TestMountRejectsAnAppWithNoTemplates covers a defect found while building
// ON Paste: Mount used to hand a nil filesystem to fs.Glob, which panics with
// a nil-pointer dereference instead of saying what is wrong.
func TestMountRejectsAnAppWithNoTemplates(t *testing.T) {
	f := newFake("paste", "ON Paste", 0)
	f.templates = nil

	reg, err := app.NewRegistry(f)
	if err != nil {
		t.Fatal(err)
	}

	assets, err := web.NewAssets(ui.Static(), "/static")
	if err != nil {
		t.Fatal(err)
	}
	rend, err := render.NewRenderer(render.Options{Layouts: ui.Templates(), AssetURL: assets.URL})
	if err != nil {
		t.Fatal(err)
	}

	// Must be an error, and must not panic.
	err = reg.Mount(http.NewServeMux(), app.Deps{Render: rend}, func(h http.Handler) http.Handler { return h })
	if err == nil {
		t.Fatal("Mount accepted an app with no templates")
	}
	if !strings.Contains(err.Error(), "Templates()") {
		t.Errorf("error %q does not explain what is missing", err)
	}
}
```

`app_test.go` needs `"net/http"`, `"strings"`, `"github.com/iliafrenkel/on-suite/internal/platform/render"`, `"github.com/iliafrenkel/on-suite/internal/platform/web"` and `"github.com/iliafrenkel/on-suite/internal/ui"` in its imports if they are not already present.

- [ ] **Step 13: Run everything**

```bash
gofmt -l . && go vet ./... && go test ./... -race
```

Expected: all PASS, including the architecture test — ON Paste imports only `internal/platform/*`, so no boundary is crossed.

- [ ] **Step 14: Look at it**

```bash
go run ./cmd/onsuite serve --data-dir /tmp/onsuite-dev
```

Sign in. Expected: **ON Paste now appears in the nav and on the dashboard**, without any platform code mentioning it — that is the framework working. Create a snippet with some Go in it and confirm:

- The code is coloured, and the line numbers are dimmer than the code.
- Switching your OS between light and dark appearance and reloading changes the highlight palette.
- The browser console has **no** Content-Security-Policy violations. If highlighting is uncoloured and the console complains about inline styles, `WithClasses(true)` was lost.
- `curl -sI localhost:8080/paste/highlight.css` returns `text/css` and an `ETag`.

- [ ] **Step 15: Commit**

```bash
git add go.mod go.sum internal cmd
git commit -m "Add ON Paste: the app, syntax highlighting, create and view

Chroma is used in class mode with a generated stylesheet, because its default
formatter writes inline style attributes that the suite's CSP blocks — the
default configuration produces a completely unstyled page. The stylesheet is
generated at startup rather than committed, so it cannot drift from the Chroma
version in go.mod, and it carries a light and a dark variant.

Highlight returns template.HTML, which is safe only because Chroma escapes the
source it tokenises. Two tests guard that: one asserts no inline styles are
emitted, the other that a script tag in a snippet comes back escaped.

Also fixes two platform defects the first real app exposed. Registry.Mount
handed a nil filesystem to fs.Glob when an app did not implement Templates(),
panicking with a nil-pointer dereference instead of a readable error. And
base.html gave a page no way to add to <head>, which a page needing its own
stylesheet must have; it now has a head block."
```

**On that second fix.** The spec says an app forcing a platform change means the platform got something wrong, and that it is worth treating as a defect. This is the first instance, and it is a small one: the layout had no extension point for page-specific `<head>` content. That is a genuine omission rather than app-specific leakage — any app with its own stylesheet, a feed link, or an Open Graph tag needs it — and the fix adds no knowledge of ON Paste to the platform. Worth recording as the framework passing its first real test with one gap found.

---
# Task 17: List and delete

**Files:**
- Modify: `internal/apps/paste/handlers.go` (replace the `list` and `delete` stubs)
- Create: `internal/apps/paste/templates/list.html`
- Modify: `internal/ui/static/app.css`
- Test: `internal/apps/paste/handlers_test.go` (add cases)

**Interfaces:**
- Consumes: `(*Store).List`, `(*Store).Delete` from Task 15.
- Produces: no new exported API. Internally, `listItem{Snippet Snippet; Preview, Language string}`.

- [ ] **Step 1: Replace the list and delete stubs**

In `internal/apps/paste/handlers.go`, delete the `list` and `delete` stubs and add:

```go
// listItem is one row on the list page. The preview is computed here rather
// than in the template so the truncation rules are testable.
type listItem struct {
	Snippet  Snippet
	Preview  string
	Language string
}

const previewRunes = 100

func (a *App) list(w http.ResponseWriter, r *http.Request) {
	userID, ok := a.userID(w, r)
	if !ok {
		return
	}

	snippets, err := a.store.List(r.Context(), userID, 0)
	if err != nil {
		a.deps.Errors.Internal(w, r, err)
		return
	}

	items := make([]listItem, 0, len(snippets))
	for _, s := range snippets {
		items = append(items, listItem{
			Snippet:  s,
			Preview:  preview(s.Body, previewRunes),
			Language: LanguageLabel(s.Language),
		})
	}

	page := a.deps.Page(r, "Snippets")
	page.Data = map[string]any{"Items": items}
	a.render(w, r, http.StatusOK, "paste/list", page)
}

func (a *App) delete(w http.ResponseWriter, r *http.Request) {
	userID, ok := a.userID(w, r)
	if !ok {
		return
	}
	id, ok := a.snippetID(w, r)
	if !ok {
		return
	}

	if err := a.store.Delete(r.Context(), userID, id); err != nil {
		a.fail(w, r, err)
		return
	}
	a.deps.Log.Info("snippet deleted", "app", ID, "user_id", userID, "snippet_id", id)

	// Back to the list: the snippet the user was looking at no longer exists,
	// so there is nowhere else sensible to land.
	http.Redirect(w, r, "/paste/", http.StatusSeeOther)
}

// preview is a single-line excerpt for the list page. Newlines and runs of
// whitespace collapse to single spaces so a row cannot grow to the height of
// the whole snippet.
func preview(body string, limit int) string {
	collapsed := strings.Join(strings.Fields(body), " ")
	runes := []rune(collapsed)
	if len(runes) <= limit {
		return collapsed
	}
	return strings.TrimRight(string(runes[:limit]), " ") + "…"
}
```

Add `"strings"` to the imports of `handlers.go`.

- [ ] **Step 2: Write the list template**

Create `internal/apps/paste/templates/list.html`:

```html
{{define "content"}}
<div class="stack">
	<div class="row list-head">
		<h1>Snippets</h1>
		<a class="button" href="/paste/new">New snippet</a>
	</div>

	{{if .Data.Items}}
	<ul class="snippet-list">
		{{range .Data.Items}}
		{{$s := .Snippet}}
		<li>
			<div class="row snippet-row-head">
				<a href="/paste/{{$s.ID}}">{{$s.DisplayTitle}}</a>
				{{if $s.Shared}}<span class="tag" title="Shared with a public link">shared</span>{{end}}
			</div>
			<p class="snippet-preview faint">{{.Preview}}</p>
			<p class="faint">
				{{.Language}} · {{$s.Lines}} lines · {{$s.CreatedAt.Format "2 Jan 2006 15:04"}}
			</p>
		</li>
		{{end}}
	</ul>
	{{else}}
	<p class="dim">No snippets yet. <a href="/paste/new">Save your first one.</a></p>
	{{end}}
</div>
{{end}}
```

- [ ] **Step 3: Style the list**

Append to `internal/ui/static/app.css`:

```css
/* ---- Snippet list ------------------------------------------------------ */

.list-head { justify-content: space-between; }
.list-head h1 { margin: 0; }

.snippet-list { margin: 0; padding: 0; list-style: none; }

.snippet-list li {
	padding: var(--s-3) 0;
	border-bottom: var(--border);
}

.snippet-list li:last-child { border-bottom: none; }
.snippet-list a { font-weight: 600; }
.snippet-list p { margin: var(--s-1) 0 0; }

.snippet-preview {
	font-family: var(--font-mono);
	overflow: hidden;
	text-overflow: ellipsis;
	white-space: nowrap;
}

.tag {
	padding: 0 var(--s-1);
	border: 1px solid var(--c-border-firm);
	border-radius: var(--radius);
	color: var(--c-text-dim);
	font-size: var(--fs-sm);
}
```

- [ ] **Step 4: Test list and delete**

Add to `internal/apps/paste/handlers_test.go`:

```go
func TestListShowsOnlyYourSnippetsNewestFirst(t *testing.T) {
	s := newServer(t)

	s.createSnippet(t, s.alice, "alice one", "go", "package one\n")
	s.createSnippet(t, s.alice, "alice two", "go", "package two\n")
	s.createSnippet(t, s.bob, "bob's secret", "go", "package bob\n")

	rec := s.do(t, s.alice, httptest.NewRequest("GET", "/paste/", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	doc := htmlassert.Parse(t, rec.Body.String())

	links := doc.QueryAll("ul.snippet-list li a")
	if len(links) != 2 {
		t.Fatalf("the list has %d entries, want 2 (alice's only)", len(links))
	}
	if got := htmlassert.Text(links[0]); got != "alice two" {
		t.Errorf("first entry = %q, want the newest", got)
	}
	if text := doc.Text(); strings.Contains(text, "bob") {
		t.Error("another user's snippet appeared in the list")
	}
}

func TestListWhenEmpty(t *testing.T) {
	s := newServer(t)
	rec := s.do(t, s.alice, httptest.NewRequest("GET", "/paste/", nil))

	doc := htmlassert.Parse(t, rec.Body.String())
	doc.MustNotHave("ul.snippet-list")
	if !strings.Contains(doc.Text(), "No snippets yet") {
		t.Errorf("no empty-state message: %q", doc.Text())
	}
	// The way to get started must still be offered.
	doc.MustHave(`a[href=/paste/new]`)
}

// TestListPreviewIsOneLine: a snippet is arbitrarily tall, and a list row
// must not be.
func TestListPreviewIsOneLine(t *testing.T) {
	s := newServer(t)
	s.createSnippet(t, s.alice, "tall", "text",
		strings.Repeat("a line of text that goes on\n", 40))

	rec := s.do(t, s.alice, httptest.NewRequest("GET", "/paste/", nil))
	doc := htmlassert.Parse(t, rec.Body.String())

	got := htmlassert.Text(doc.MustHave(".snippet-preview"))
	if strings.Contains(got, "\n") {
		t.Error("the preview contains a newline")
	}
	if len([]rune(got)) > 120 {
		t.Errorf("the preview is %d characters, which is too long: %q", len([]rune(got)), got)
	}
	if !strings.HasSuffix(got, "…") {
		t.Errorf("a truncated preview should be marked with an ellipsis: %q", got)
	}
}

func TestDelete(t *testing.T) {
	s := newServer(t)
	id := s.createSnippet(t, s.alice, "doomed", "go", "package main\n")

	rec := s.post(t, s.alice, "/paste/"+itoa(id)+"/delete", url.Values{})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("delete = %d, want 303", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "/paste/" {
		t.Errorf("Location = %q, want /paste/", loc)
	}

	rec = s.do(t, s.alice, httptest.NewRequest("GET", "/paste/"+itoa(id), nil))
	if rec.Code != http.StatusNotFound {
		t.Errorf("the snippet is still viewable: %d", rec.Code)
	}
}

// TestDeleteSomeoneElsesSnippetFails is the one that would matter most if it
// regressed.
func TestDeleteSomeoneElsesSnippetFails(t *testing.T) {
	s := newServer(t)
	id := s.createSnippet(t, s.alice, "alice's", "go", "package main\n")

	rec := s.post(t, s.bob, "/paste/"+itoa(id)+"/delete", url.Values{})
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}

	// And it is genuinely still there.
	rec = s.do(t, s.alice, httptest.NewRequest("GET", "/paste/"+itoa(id), nil))
	if rec.Code != http.StatusOK {
		t.Error("the owner's snippet was destroyed by another user's request")
	}
}

func TestDeleteRequiresCSRFAndPOST(t *testing.T) {
	s := newServer(t)
	id := s.createSnippet(t, s.alice, "safe", "go", "package main\n")

	// No token.
	req := httptest.NewRequest("POST", "/paste/"+itoa(id)+"/delete", nil)
	if rec := s.do(t, s.alice, req); rec.Code != http.StatusForbidden {
		t.Errorf("delete without a CSRF token = %d, want 403", rec.Code)
	}

	// A GET must not delete: the route is POST-only, so ServeMux refuses it.
	rec := s.do(t, s.alice, httptest.NewRequest("GET", "/paste/"+itoa(id)+"/delete", nil))
	if rec.Code == http.StatusSeeOther {
		t.Error("a GET performed the deletion")
	}

	if rec := s.do(t, s.alice, httptest.NewRequest("GET", "/paste/"+itoa(id), nil)); rec.Code != http.StatusOK {
		t.Error("the snippet was deleted by a request that should have been refused")
	}
}
```

- [ ] **Step 5: Run and verify**

```bash
gofmt -l . && go vet ./... && go test ./... -race
```

Then by hand: create two snippets, confirm the list shows both newest-first with one-line previews, delete one from its view page and land back on the list.

- [ ] **Step 6: Commit**

```bash
git add internal
git commit -m "Add the ON Paste list and delete

The list shows only the signed-in user's snippets, newest first, with a
single-line preview computed in Go rather than the template so the truncation
rules are testable — an untruncated preview would make a row as tall as the
whole snippet.

Deleting someone else's snippet returns 404 rather than 403, and a test
confirms the snippet survives the attempt. Deletion is POST-only and
CSRF-checked."
```

---

# Task 18: Sharing, revocation, public view and raw text

**Files:**
- Modify: `internal/apps/paste/handlers.go` (replace the remaining stubs)
- Create: `internal/apps/paste/templates/shared.html`
- Test: `internal/apps/paste/handlers_test.go` (add cases)

**Interfaces:**
- Consumes: `(*Store).Share`, `(*Store).Unshare`, `(*Store).ByShareSlug` from Task 15.
- Produces: no new exported API.

**Design notes:**
- The shared page renders through a **different template** to the owner's view, not the same one with the controls hidden. A hidden control is one template edit away from being visible to the public; a template that has no controls in it cannot leak them.
- Raw responses are `text/plain; charset=utf-8`. Combined with the global `X-Content-Type-Options: nosniff` from Plan 2, a snippet containing HTML cannot be coaxed into rendering as a page on this origin.
- Sharing and unsharing are POSTs and redirect back to the snippet, so the user sees the new state.

- [ ] **Step 1: Replace the remaining stubs**

In `internal/apps/paste/handlers.go`, delete the `raw`, `share`, `unshare`, `viewShared` and `rawShared` stubs and add:

```go
func (a *App) share(w http.ResponseWriter, r *http.Request) {
	userID, ok := a.userID(w, r)
	if !ok {
		return
	}
	id, ok := a.snippetID(w, r)
	if !ok {
		return
	}

	slug, err := a.store.Share(r.Context(), userID, id)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	// The slug itself is a credential, so it is not written to the log.
	a.deps.Log.Info("snippet shared", "app", ID, "user_id", userID, "snippet_id", id)
	_ = slug

	http.Redirect(w, r, "/paste/"+strconv.FormatInt(id, 10), http.StatusSeeOther)
}

func (a *App) unshare(w http.ResponseWriter, r *http.Request) {
	userID, ok := a.userID(w, r)
	if !ok {
		return
	}
	id, ok := a.snippetID(w, r)
	if !ok {
		return
	}

	if err := a.store.Unshare(r.Context(), userID, id); err != nil {
		a.fail(w, r, err)
		return
	}
	a.deps.Log.Info("snippet unshared", "app", ID, "user_id", userID, "snippet_id", id)

	http.Redirect(w, r, "/paste/"+strconv.FormatInt(id, 10), http.StatusSeeOther)
}

// viewShared renders a snippet for anyone holding its slug. Note the separate
// template: the owner's view carries delete and unshare controls, and the way
// to guarantee those never reach a public page is for the public template not
// to contain them at all.
func (a *App) viewShared(w http.ResponseWriter, r *http.Request) {
	s, err := a.store.ByShareSlug(r.Context(), r.PathValue("slug"))
	if err != nil {
		a.fail(w, r, err)
		return
	}

	page := a.deps.Page(r, s.DisplayTitle())
	page.Data = viewModel{
		Snippet:   s,
		Highlight: Highlight(s.Body, s.Language),
		Language:  LanguageLabel(s.Language),
		RawURL:    "/paste/s/" + s.ShareSlug + "/raw",
		Owner:     false,
	}
	a.render(w, r, http.StatusOK, "paste/shared", page)
}

// raw serves the owner's own snippet as plain text.
func (a *App) raw(w http.ResponseWriter, r *http.Request) {
	userID, ok := a.userID(w, r)
	if !ok {
		return
	}
	id, ok := a.snippetID(w, r)
	if !ok {
		return
	}

	s, err := a.store.ByID(r.Context(), userID, id)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	a.writeRaw(w, r, s)
}

// rawShared serves a shared snippet as plain text, so it can be piped straight
// into a terminal.
func (a *App) rawShared(w http.ResponseWriter, r *http.Request) {
	s, err := a.store.ByShareSlug(r.Context(), r.PathValue("slug"))
	if err != nil {
		a.fail(w, r, err)
		return
	}
	a.writeRaw(w, r, s)
}

// writeRaw sends a snippet body verbatim.
//
// text/plain plus the platform's global nosniff header means a snippet that
// happens to contain HTML cannot be coaxed into executing as a page on this
// origin. Content-Disposition names the download without forcing one, so a
// browser still shows it and curl still prints it.
func (a *App) writeRaw(w http.ResponseWriter, r *http.Request, s Snippet) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Content-Disposition", "inline")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)

	if _, err := io.WriteString(w, s.Body); err != nil {
		a.deps.Log.Error("writing a raw snippet failed", "error", err, "snippet_id", s.ID)
	}
}
```

Add `"io"` to the imports of `handlers.go`.

- [ ] **Step 2: Write the shared template**

Create `internal/apps/paste/templates/shared.html`:

```html
{{define "head"}}
<link rel="stylesheet" href="/paste/highlight.css">
{{end}}

{{define "content"}}
{{$s := .Data.Snippet}}
<div class="stack">
	<div class="snippet-head">
		<h1>{{$s.DisplayTitle}}</h1>
		<p class="faint">
			{{.Data.Language}} · {{$s.Lines}} lines · shared from ON Paste
		</p>
	</div>

	{{/* Deliberately no delete, share or unshare controls: this template is
	     reachable by anyone with the link, and a control that is not written
	     here cannot be exposed by a later edit. */}}
	<div class="row snippet-actions">
		<a class="button" href="{{.Data.RawURL}}">Raw</a>
	</div>

	<div class="scroll-x snippet-body">{{.Data.Highlight}}</div>
</div>
{{end}}
```

- [ ] **Step 3: Test sharing end to end**

Add to `internal/apps/paste/handlers_test.go`:

```go
// shareAndGetSlug shares a snippet and returns its public slug, read back from
// the store so the test does not have to scrape it out of the page.
func (s *server) shareAndGetSlug(t *testing.T, sess *session, id int64) string {
	t.Helper()

	rec := s.post(t, sess, "/paste/"+itoa(id)+"/share", url.Values{})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("share = %d, want 303", rec.Code)
	}
	snippet, err := s.store.ByID(context.Background(), sess.user.ID, id)
	if err != nil {
		t.Fatal(err)
	}
	if !snippet.Shared() {
		t.Fatal("the snippet is not shared after sharing it")
	}
	return snippet.ShareSlug
}

func TestSharedSnippetIsReadableWhileSignedOut(t *testing.T) {
	s := newServer(t)
	id := s.createSnippet(t, s.alice, "Shared config", "yaml", "key: value\n")
	slug := s.shareAndGetSlug(t, s.alice, id)

	// nil session: no cookies at all.
	rec := s.do(t, nil, httptest.NewRequest("GET", "/paste/s/"+slug, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("anonymous shared view = %d, want 200", rec.Code)
	}
	doc := htmlassert.Parse(t, rec.Body.String())

	if got := htmlassert.Text(doc.MustHave("h1")); got != "Shared config" {
		t.Errorf("title = %q", got)
	}
	if !strings.Contains(doc.Text(), "key") {
		t.Error("the snippet body is not on the shared page")
	}
	doc.MustHave(".chroma") // highlighted for anonymous readers too
}

// TestSharedPageOffersNoDestructiveControls is why the shared view has its own
// template rather than reusing the owner's with conditionals.
func TestSharedPageOffersNoDestructiveControls(t *testing.T) {
	s := newServer(t)
	id := s.createSnippet(t, s.alice, "Shared", "go", "package main\n")
	slug := s.shareAndGetSlug(t, s.alice, id)

	rec := s.do(t, nil, httptest.NewRequest("GET", "/paste/s/"+slug, nil))
	doc := htmlassert.Parse(t, rec.Body.String())

	doc.MustNotHave(`form[action=/paste/` + itoa(id) + `/delete]`)
	doc.MustNotHave(`form[action=/paste/` + itoa(id) + `/unshare]`)
	doc.MustNotHave(".shell-user") // nobody is signed in
	for _, word := range []string{"Delete", "Stop sharing"} {
		if strings.Contains(doc.Text(), word) {
			t.Errorf("the shared page offers %q", word)
		}
	}
}

// TestUnshareKillsTheLink is the point of choosing a revocable share model.
func TestUnshareKillsTheLink(t *testing.T) {
	s := newServer(t)
	id := s.createSnippet(t, s.alice, "Shared", "go", "package main\n")
	slug := s.shareAndGetSlug(t, s.alice, id)

	if rec := s.do(t, nil, httptest.NewRequest("GET", "/paste/s/"+slug, nil)); rec.Code != http.StatusOK {
		t.Fatal("the link did not work before revoking it")
	}

	rec := s.post(t, s.alice, "/paste/"+itoa(id)+"/unshare", url.Values{})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("unshare = %d", rec.Code)
	}

	if rec := s.do(t, nil, httptest.NewRequest("GET", "/paste/s/"+slug, nil)); rec.Code != http.StatusNotFound {
		t.Errorf("the revoked link still works: %d", rec.Code)
	}
	if rec := s.do(t, nil, httptest.NewRequest("GET", "/paste/s/"+slug+"/raw", nil)); rec.Code != http.StatusNotFound {
		t.Errorf("the revoked raw link still works: %d", rec.Code)
	}

	// Re-sharing must produce a different link, leaving the old one dead.
	second := s.shareAndGetSlug(t, s.alice, id)
	if second == slug {
		t.Fatal("re-sharing reissued the revoked slug")
	}
	if rec := s.do(t, nil, httptest.NewRequest("GET", "/paste/s/"+slug, nil)); rec.Code != http.StatusNotFound {
		t.Error("the old link came back to life after re-sharing")
	}
}

func TestUnsharedSnippetIsNotPubliclyReachable(t *testing.T) {
	s := newServer(t)
	id := s.createSnippet(t, s.alice, "Private", "go", "top secret\n")

	// The owner's own URL must not work for an anonymous visitor.
	rec := s.do(t, nil, httptest.NewRequest("GET", "/paste/"+itoa(id), nil))
	if rec.Code != http.StatusSeeOther {
		t.Errorf("anonymous access to a private snippet = %d, want a redirect to login", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "top secret") {
		t.Fatal("a private snippet leaked to an anonymous request")
	}

	// And a guessed share URL must not either.
	for _, slug := range []string{itoa(id), "aaaaaaaaaaaaaaaaaaaaaa", ""} {
		rec := s.do(t, nil, httptest.NewRequest("GET", "/paste/s/"+slug, nil))
		if rec.Code == http.StatusOK {
			t.Errorf("GET /paste/s/%q returned 200", slug)
		}
	}
}

func TestShareStateIsShownToTheOwner(t *testing.T) {
	s := newServer(t)
	id := s.createSnippet(t, s.alice, "Shared", "go", "package main\n")
	slug := s.shareAndGetSlug(t, s.alice, id)

	rec := s.do(t, s.alice, httptest.NewRequest("GET", "/paste/"+itoa(id), nil))
	doc := htmlassert.Parse(t, rec.Body.String())

	// The link is shown so it can be copied, and the control now revokes.
	if got := htmlassert.Text(doc.MustHave(".notice code")); !strings.Contains(got, slug) {
		t.Errorf("the share link is not displayed; got %q", got)
	}
	doc.MustHave(`form[action=/paste/` + itoa(id) + `/unshare]`)
	doc.MustNotHave(`form[action=/paste/` + itoa(id) + `/share]`)
}

func TestRawIsPlainTextForOwnerAndShared(t *testing.T) {
	s := newServer(t)
	const body = "line one\nline two\n"
	id := s.createSnippet(t, s.alice, "Raw", "text", body)
	slug := s.shareAndGetSlug(t, s.alice, id)

	cases := []struct {
		name    string
		path    string
		session *session
	}{
		{"owner", "/paste/raw/" + itoa(id), s.alice},
		{"shared, signed out", "/paste/s/" + slug + "/raw", nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := s.do(t, tc.session, httptest.NewRequest("GET", tc.path, nil))
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d", rec.Code)
			}
			if got := rec.Body.String(); got != body {
				t.Errorf("body = %q, want the snippet verbatim %q", got, body)
			}
			if ct := rec.Header().Get("Content-Type"); ct != "text/plain; charset=utf-8" {
				t.Errorf("Content-Type = %q, want text/plain; charset=utf-8", ct)
			}
			// No HTML anywhere: this is the response a terminal consumes.
			if strings.Contains(rec.Body.String(), "<html") {
				t.Error("the raw response contains markup")
			}
		})
	}
}

// TestRawOfHTMLIsNotServedAsHTML: a snippet containing a page must not become
// one on this origin.
func TestRawOfHTMLIsNotServedAsHTML(t *testing.T) {
	s := newServer(t)
	id := s.createSnippet(t, s.alice, "evil", "html",
		"<html><body><script>alert(1)</script></body></html>\n")

	rec := s.do(t, s.alice, httptest.NewRequest("GET", "/paste/raw/"+itoa(id), nil))
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/plain") {
		t.Fatalf("Content-Type = %q; the browser could render this as a page", ct)
	}
	// The body is verbatim on purpose — nosniff plus text/plain is what makes
	// that safe, so assert the header rather than mangling the content.
	if !strings.Contains(rec.Body.String(), "<script>") {
		t.Error("the raw response altered the snippet")
	}
}

func TestRawOfAnotherUsersSnippetIs404(t *testing.T) {
	s := newServer(t)
	id := s.createSnippet(t, s.alice, "alice's", "go", "secret\n")

	rec := s.do(t, s.bob, httptest.NewRequest("GET", "/paste/raw/"+itoa(id), nil))
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "secret") {
		t.Error("the snippet leaked")
	}
}

func TestShareRequiresCSRF(t *testing.T) {
	s := newServer(t)
	id := s.createSnippet(t, s.alice, "t", "go", "package main\n")

	for _, action := range []string{"share", "unshare"} {
		req := httptest.NewRequest("POST", "/paste/"+itoa(id)+"/"+action, nil)
		if rec := s.do(t, s.alice, req); rec.Code != http.StatusForbidden {
			t.Errorf("%s without a CSRF token = %d, want 403", action, rec.Code)
		}
	}
}

Finally, pin the public surface. This asserts behaviour — what an anonymous
visitor can actually reach — rather than reading registration data back out of
the platform, which would need an accessor added for a test's sake:

```go
// TestPublicSurfaceIsExactlyThreeRoutes is the backstop on the whole sharing
// design. If a future change makes a fourth path anonymous, this fails first.
func TestPublicSurfaceIsExactlyThreeRoutes(t *testing.T) {
	s := newServer(t)
	id := s.createSnippet(t, s.alice, "shared", "go", "package main\n")
	slug := s.shareAndGetSlug(t, s.alice, id)

	reachable := []string{
		"/paste/highlight.css",
		"/paste/s/" + slug,
		"/paste/s/" + slug + "/raw",
	}
	blocked := []string{
		"/paste/",
		"/paste/new",
		"/paste/" + itoa(id),
		"/paste/raw/" + itoa(id),
	}

	for _, path := range reachable {
		if rec := s.do(t, nil, httptest.NewRequest("GET", path, nil)); rec.Code != http.StatusOK {
			t.Errorf("public path %s = %d, want 200", path, rec.Code)
		}
	}
	for _, path := range blocked {
		if rec := s.do(t, nil, httptest.NewRequest("GET", path, nil)); rec.Code == http.StatusOK {
			t.Errorf("%s is reachable anonymously and must not be", path)
		}
	}
}
```

- [ ] **Step 4: Run everything**

```bash
gofmt -l . && go vet ./... && go test ./... -race
```

- [ ] **Step 5: Verify sharing by hand, in a private window**

```bash
go run ./cmd/onsuite serve --data-dir /tmp/onsuite-dev
```

1. Create a snippet, open it, press Share. The link appears.
2. Copy the link into a **private browsing window** — the real test, since your normal window has a session. It must render, highlighted, with no nav user menu and no Delete button.
3. `curl` the same link with `/raw` appended. You should get exactly the snippet text.
4. Back in your session, press Stop sharing. Reload the private window: 404.
5. Press Share again and confirm the new link differs from the old one, and that the old one still 404s.

- [ ] **Step 6: Commit**

```bash
git add internal
git commit -m "Add snippet sharing, revocation, public view and raw text

A snippet is private until shared, and sharing mints a fresh unguessable slug
each time, so a revoked link stays dead even if sharing is turned back on.

The public page renders through its own template rather than the owner's with
the controls hidden. A hidden control is one template edit away from being
public; a control that is not written in the file cannot leak.

Raw responses are text/plain, which together with the platform's global nosniff
header means a snippet containing a whole HTML page cannot execute as one on
this origin. A test asserts the public surface is exactly three paths and that
everything else refuses an anonymous visitor."
```

---
# Task 19: JSON export

**Files:**
- Modify: `internal/platform/app/app.go` (add the optional `Exporter` interface and `Registry.Export`)
- Modify: `internal/apps/paste/store.go` (add `All`)
- Create: `internal/apps/paste/export.go`
- Create: `cmd/onsuite/export.go`
- Modify: `cmd/onsuite/main.go` (dispatch `export`)
- Test: `internal/apps/paste/export_test.go`, `cmd/onsuite/export_test.go`

**Interfaces:**
- Produces:
  - `app.Exporter` interface: `Export(ctx context.Context, handle *sql.DB, userID int64) (any, error)`
  - `(*Registry).Export(ctx context.Context, handle *sql.DB, userID int64) (map[string]any, error)`
  - `(*paste.Store).All(ctx context.Context, userID int64) ([]Snippet, error)`
  - `onsuite export <username> [--data-dir DIR] [--out FILE]`

**Design notes:**
- `Exporter` is **optional**, discovered by type assertion exactly like `Templates()`. An app that cannot export simply does not implement it, and no interface grows a method every app must stub out.
- Export runs from the CLI against the database directly. It never builds the HTTP stack, so it works on a stopped server.
- **Share slugs are excluded.** A slug is a credential: anyone holding it can read the snippet. An export is for data portability and may be emailed or synced, so it records *whether* a snippet was shared, not the secret that shares it. Restoring a byte-identical database is what `onsuite backup` is for.

- [ ] **Step 1: Unify how commands open the database**

Right now each command decides for itself which migrations to apply, and they
disagree: `serve` applies the platform's and every app's, `user add` applies only
the platform's, and a new command would naturally apply none. Exporting from a
fresh data directory therefore fails with `no such table: paste_snippets`. This
was found by running this plan's own tests before publishing it.

Create `cmd/onsuite/database.go`:

```go
package main

import (
	"context"
	"database/sql"

	"github.com/iliafrenkel/on-suite/internal/platform/app"
	"github.com/iliafrenkel/on-suite/internal/platform/auth"
	"github.com/iliafrenkel/on-suite/internal/platform/config"
	"github.com/iliafrenkel/on-suite/internal/platform/db"
)

// openDatabase opens the database and brings the schema up to date, including
// every registered app's migrations, and returns the registry alongside it.
//
// Every command that touches the database goes through here. Without it each
// command decides for itself which migrations to apply, and they disagree: the
// original user-add applied only the platform's schema and export applied none,
// so exporting a fresh database failed with "no such table: paste_snippets".
func openDatabase(ctx context.Context, cfg config.Config) (*sql.DB, *app.Registry, int, error) {
	registry, err := app.NewRegistry(registeredApps()...)
	if err != nil {
		return nil, nil, 0, err
	}

	handle, err := db.Open(cfg.DBPath())
	if err != nil {
		return nil, nil, 0, err
	}

	migrations, err := db.Collect(auth.Namespace, auth.Migrations())
	if err != nil {
		_ = handle.Close()
		return nil, nil, 0, err
	}
	appMigrations, err := registry.Migrations()
	if err != nil {
		_ = handle.Close()
		return nil, nil, 0, err
	}

	applied, err := db.Apply(ctx, handle, append(migrations, appMigrations...))
	if err != nil {
		_ = handle.Close()
		return nil, nil, 0, err
	}
	return handle, registry, applied, nil
}
```

Then rewire the two existing callers.

In `cmd/onsuite/user.go`, replace the open-and-migrate block with:

```go
	ctx := context.Background()
	handle, _, _, err := openDatabase(ctx, cfg)
	if err != nil {
		return err
	}
	defer func() { _ = handle.Close() }()
```

and drop the now-unused `db` import.

In `cmd/onsuite/serve.go`, replace `handle, err := db.Open(cfg.DBPath())` with:

```go
	handle, registry, applied, err := openDatabase(context.Background(), cfg)
	if err != nil {
		return err
	}
```

then delete the separate registry-and-migration block that follows the
`database ready` log line, keeping only:

```go
	if applied > 0 {
		log.Info("migrations applied", "count", applied)
	}
```

The `app` import is no longer needed in `serve.go`. `db` still is, for
`db.Checkpoint`.

- [ ] **Step 2: Add the exporter interface to the platform**

In `internal/platform/app/app.go`, add after the `templated` interface:

```go
// Exporter is implemented by apps that can dump one user's data as JSON. It is
// optional and discovered by type assertion, so an app that has nothing to
// export does not have to stub out a method.
//
// It takes the database rather than using Mount's Deps, so exporting works
// from the command line without building the HTTP stack.
type Exporter interface {
	Export(ctx context.Context, handle *sql.DB, userID int64) (any, error)
}

// Export collects every registered app's data for one user, keyed by app id.
// Apps that do not implement Exporter are skipped silently — that is a design
// choice, not a failure.
func (reg *Registry) Export(ctx context.Context, handle *sql.DB, userID int64) (map[string]any, error) {
	out := make(map[string]any)
	for _, a := range reg.apps {
		e, ok := a.(Exporter)
		if !ok {
			continue
		}
		id := a.Meta().ID
		data, err := e.Export(ctx, handle, userID)
		if err != nil {
			return nil, fmt.Errorf("export %s: %w", id, err)
		}
		out[id] = data
	}
	return out, nil
}
```

Add `"context"` to `app.go`'s imports.

- [ ] **Step 3: Add an unlimited fetch to the store**

In `internal/apps/paste/store.go`, add after `List`:

```go
// All returns every one of userID's snippets, oldest first.
//
// It exists separately from List because List is capped for the web page,
// whereas an export must be complete or it is not an export.
func (st *Store) All(ctx context.Context, userID int64) ([]Snippet, error) {
	rows, err := st.db.QueryContext(ctx,
		`SELECT id, user_id, title, language, body, share_slug, created_at
		 FROM paste_snippets WHERE user_id = ?
		 ORDER BY created_at ASC, id ASC`, userID)
	if err != nil {
		return nil, fmt.Errorf("paste: all: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []Snippet
	for rows.Next() {
		s, err := scanSnippetRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("paste: all: %w", err)
	}
	return out, nil
}
```

- [ ] **Step 4: Implement the export**

Create `internal/apps/paste/export.go`:

```go
package paste

import (
	"context"
	"database/sql"
	"time"
)

// exportedSnippet is the on-disk shape of a snippet.
//
// It deliberately omits share_slug. A slug is a credential — anyone holding it
// can read the snippet — and an export is a portable file that may be copied
// or emailed. Whether a snippet was shared is recorded; the secret that shares
// it is not. Use onsuite backup for a restorable copy.
type exportedSnippet struct {
	ID        int64     `json:"id"`
	Title     string    `json:"title"`
	Language  string    `json:"language"`
	Body      string    `json:"body"`
	Shared    bool      `json:"shared"`
	CreatedAt time.Time `json:"created_at"`
}

type exportPayload struct {
	Snippets []exportedSnippet `json:"snippets"`
}

// Export implements app.Exporter.
func (a *App) Export(ctx context.Context, handle *sql.DB, userID int64) (any, error) {
	snippets, err := NewStore(handle).All(ctx, userID)
	if err != nil {
		return nil, err
	}

	out := exportPayload{Snippets: make([]exportedSnippet, 0, len(snippets))}
	for _, s := range snippets {
		out.Snippets = append(out.Snippets, exportedSnippet{
			ID:        s.ID,
			Title:     s.Title,
			Language:  s.Language,
			Body:      s.Body,
			Shared:    s.Shared(),
			CreatedAt: s.CreatedAt,
		})
	}
	return out, nil
}
```

- [ ] **Step 5: Add the export command**

Create `cmd/onsuite/export.go`:

```go
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/iliafrenkel/on-suite/internal/platform/auth"
	"github.com/iliafrenkel/on-suite/internal/platform/config"
)

// exportDocument is the top-level shape written out. Apps appear under their
// own id, so a future app adds a key without changing this structure.
type exportDocument struct {
	Format     int            `json:"format"`
	ExportedAt time.Time      `json:"exported_at"`
	User       string         `json:"user"`
	Apps       map[string]any `json:"apps"`
}

// exportFormat is bumped only if the shape changes incompatibly, so a reader
// can tell.
const exportFormat = 1

func exportCmd(args []string, getenv func(string) string, out io.Writer, errOut io.Writer) error {
	fs := flag.NewFlagSet("export", flag.ContinueOnError)
	fs.SetOutput(errOut)
	dataDir := fs.String("data-dir", envOrDefault(getenv, "ONSUITE_DATA_DIR", "./data"),
		"directory holding the database")
	outPath := fs.String("out", "", "write to this file instead of standard output")

	positional, err := parseInterspersed(fs, args)
	if err != nil {
		return err
	}
	if len(positional) != 1 {
		return errors.New("export: exactly one username is required")
	}
	username := positional[0]

	cfg := config.Config{DataDir: *dataDir}
	ctx := context.Background()
	handle, registry, _, err := openDatabase(ctx, cfg)
	if err != nil {
		return err
	}
	defer func() { _ = handle.Close() }()
	user, err := auth.NewStore(handle).UserByUsername(ctx, username)
	if err != nil {
		if errors.Is(err, auth.ErrNotFound) {
			return fmt.Errorf("export: no such user %q", username)
		}
		return err
	}

	apps, err := registry.Export(ctx, handle, user.ID)
	if err != nil {
		return err
	}

	doc := exportDocument{
		Format:     exportFormat,
		ExportedAt: time.Now().UTC(),
		User:       user.Username,
		Apps:       apps,
	}

	writer := out
	if *outPath != "" {
		// 0600: an export contains everything the user has written.
		f, err := os.OpenFile(*outPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
		if err != nil {
			return fmt.Errorf("export: create %s: %w", *outPath, err)
		}
		defer func() { _ = f.Close() }()
		writer = f
	}

	enc := json.NewEncoder(writer)
	enc.SetIndent("", "  ")
	// A file a person may read, so do not mangle < > & into escapes.
	enc.SetEscapeHTML(false)
	if err := enc.Encode(doc); err != nil {
		return fmt.Errorf("export: write json: %w", err)
	}

	if *outPath != "" {
		fmt.Fprintf(errOut, "Exported %s to %s\n", user.Username, *outPath)
	}
	return nil
}
```

- [ ] **Step 6: Dispatch it**

In `cmd/onsuite/main.go`, add a case before `default`:

```go
	case "export":
		return exportCmd(args[1:], getenv, os.Stdout, errOut)
```

and a usage line after the `user add` line:

```
  onsuite export <name>     write a user's data as JSON
```

- [ ] **Step 7: Test the export**

Create `internal/apps/paste/export_test.go`:

```go
package paste_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/iliafrenkel/on-suite/internal/apps/paste"
	"github.com/iliafrenkel/on-suite/internal/platform/app"
)

func TestExportImplementsTheInterface(t *testing.T) {
	// A compile-time assertion would be enough, but a failing test names the
	// problem more clearly than a type error in an unrelated file.
	var a any = paste.New()
	if _, ok := a.(app.Exporter); !ok {
		t.Fatal("*paste.App does not implement app.Exporter")
	}
}

func TestExportContainsEverythingExceptTheSlug(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	one, err := f.store.Create(ctx, f.alice.ID, "first", "go", "package one\n")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.Create(ctx, f.alice.ID, "second", "yaml", "key: value\n"); err != nil {
		t.Fatal(err)
	}
	slug, err := f.store.Share(ctx, f.alice.ID, one.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.Create(ctx, f.bob.ID, "bob's", "go", "package bob\n"); err != nil {
		t.Fatal(err)
	}

	payload, err := paste.New().Export(ctx, f.db, f.alice.ID)
	if err != nil {
		t.Fatalf("Export: %v", err)
	}

	encoded, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("the payload does not marshal: %v", err)
	}
	text := string(encoded)

	for _, want := range []string{"package one", "key: value", "first", "second"} {
		if !strings.Contains(text, want) {
			t.Errorf("the export is missing %q", want)
		}
	}
	if strings.Contains(text, "package bob") {
		t.Error("the export contains another user's snippet")
	}
	// The slug is a credential and must not be written to a portable file.
	if strings.Contains(text, slug) {
		t.Error("the export leaked a share slug")
	}
	if !strings.Contains(text, `"shared":true`) {
		t.Error("the export does not record that a snippet was shared")
	}
}

func TestExportOfAUserWithNothingIsAnEmptyList(t *testing.T) {
	f := newFixture(t)

	payload, err := paste.New().Export(context.Background(), f.db, f.bob.ID)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	// A null would be awkward for a consumer; an empty array is not.
	if got := string(encoded); !strings.Contains(got, `"snippets":[]`) {
		t.Errorf("export = %s, want an empty snippets array", got)
	}
}
```

Create `cmd/onsuite/export_test.go`:

```go
package main

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// seedDatabase creates a data directory with one account, using the real
// user-add path.
func seedDatabase(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	err := userAdd([]string{"ilia", "--data-dir", dir}, nil,
		stdinFrom(t, "a-sufficiently-long-password\n"), io.Discard, io.Discard)
	if err != nil {
		t.Fatalf("userAdd: %v", err)
	}
	return dir
}

func TestExportCmdWritesJSONToStdout(t *testing.T) {
	dir := seedDatabase(t)

	var out, errOut bytes.Buffer
	if err := exportCmd([]string{"ilia", "--data-dir", dir}, nil, &out, &errOut); err != nil {
		t.Fatalf("exportCmd: %v", err)
	}

	var doc struct {
		Format     int            `json:"format"`
		User       string         `json:"user"`
		ExportedAt string         `json:"exported_at"`
		Apps       map[string]any `json:"apps"`
	}
	if err := json.Unmarshal(out.Bytes(), &doc); err != nil {
		t.Fatalf("output is not JSON: %v\n%s", err, out.String())
	}
	if doc.Format != exportFormat {
		t.Errorf("format = %d, want %d", doc.Format, exportFormat)
	}
	if doc.User != "ilia" {
		t.Errorf("user = %q", doc.User)
	}
	if doc.ExportedAt == "" {
		t.Error("exported_at is empty")
	}
	if _, ok := doc.Apps["paste"]; !ok {
		t.Errorf("no paste key in apps: %v", doc.Apps)
	}
}

func TestExportCmdWritesToAFile(t *testing.T) {
	dir := seedDatabase(t)
	target := filepath.Join(t.TempDir(), "dump.json")

	var out, errOut bytes.Buffer
	if err := exportCmd([]string{"ilia", "--data-dir", dir, "--out", target}, nil, &out, &errOut); err != nil {
		t.Fatalf("exportCmd: %v", err)
	}
	if out.Len() != 0 {
		t.Errorf("stdout got %d bytes when writing to a file", out.Len())
	}
	if !strings.Contains(errOut.String(), target) {
		t.Errorf("no confirmation naming the file: %q", errOut.String())
	}

	info, err := os.Stat(target)
	if err != nil {
		t.Fatal(err)
	}
	// An export holds everything the user has written.
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("file mode = %o, want 600", perm)
	}
	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if !json.Valid(data) {
		t.Error("the file does not contain valid JSON")
	}
}

func TestExportCmdRejectsBadInput(t *testing.T) {
	dir := seedDatabase(t)
	tests := []struct {
		name string
		args []string
	}{
		{"no username", []string{"--data-dir", dir}},
		{"two usernames", []string{"ilia", "someone", "--data-dir", dir}},
		{"unknown user", []string{"nobody", "--data-dir", dir}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := exportCmd(tt.args, nil, io.Discard, io.Discard); err == nil {
				t.Fatal("exportCmd succeeded, want an error")
			}
		})
	}
}
```

- [ ] **Step 8: Run and verify**

```bash
gofmt -l . && go vet ./... && go test ./... -race
```

By hand, against the development database:

```bash
go run ./cmd/onsuite export ilia --data-dir /tmp/onsuite-dev
```

Expected: indented JSON with a `paste.snippets` array containing your snippets, and **no share slugs anywhere**. Confirm with:

```bash
go run ./cmd/onsuite export ilia --data-dir /tmp/onsuite-dev | python3 -m json.tool > /dev/null && echo "valid JSON"
```

- [ ] **Step 9: Commit**

```bash
git add internal cmd
git commit -m "Add per-user JSON export

app.Exporter is optional and found by type assertion, like Templates(), so an
app with nothing to export does not stub out a method. It takes the database
rather than Mount's Deps, so exporting works against a stopped server.

Share slugs are excluded. A slug is a credential, and an export is a portable
file that may be copied or emailed; the export records whether a snippet was
shared, not the secret that shares it. A byte-identical restorable copy is what
onsuite backup produces instead."
```

---

# Task 20: Backup, retention, and periodic maintenance

**Files:**
- Create: `cmd/onsuite/backup.go`
- Modify: `cmd/onsuite/main.go` (dispatch `backup`)
- Modify: `cmd/onsuite/serve.go` (start the maintenance loop)
- Modify: `internal/platform/config/config.go` (two new flags)
- Test: `cmd/onsuite/backup_test.go`, `internal/platform/config/config_test.go`

**Interfaces:**
- Consumes: `db.BackupTo` (Plan 1 Task 2), `(*auth.Store).DeleteExpiredSessions` (Plan 1 Task 6).
- Produces:
  - `config.Config.BackupInterval time.Duration`, `config.Config.BackupKeep int`
  - `onsuite backup [--data-dir DIR] [--out FILE]`
  - `snapshotName(now time.Time) string`, `writeSnapshot(ctx, handle, dir string, keep int, now time.Time) (string, error)`, `pruneSnapshots(dir string, keep int) (int, error)`
  - `runMaintenance(ctx, handle, users, cfg, log)` — the periodic loop

**A gap this task closes.** Plan 1 built `DeleteExpiredSessions` and **nothing ever calls it**. Expired sessions are correctly refused at login, so this is not a security hole, but the rows accumulate forever. The maintenance loop added here sweeps them on the same schedule as the backup.

- [ ] **Step 1: Add the configuration**

In `internal/platform/config/config.go`, add the fields to `Config`:

```go
	// BackupInterval is how often the server snapshots itself. Zero disables
	// the internal schedule, for anyone who would rather drive backups from
	// cron or a systemd timer.
	BackupInterval time.Duration
	// BackupKeep is how many snapshots to retain.
	BackupKeep int
```

Register the flags inside `Parse`, alongside the others:

```go
	fs.DurationVar(&c.BackupInterval, "backup-interval",
		envDuration(getenv, "ONSUITE_BACKUP_INTERVAL", 24*time.Hour),
		"how often to snapshot the database; 0 disables the internal schedule")
	fs.IntVar(&c.BackupKeep, "backup-keep",
		envInt(getenv, "ONSUITE_BACKUP_KEEP", 7),
		"how many snapshots to keep")
```

Validate after `fs.Parse`, next to the existing `DataDir` check:

```go
	if c.BackupInterval < 0 {
		return Config{}, fmt.Errorf("backup-interval must not be negative")
	}
	if c.BackupInterval > 0 && c.BackupInterval < time.Minute {
		// A snapshot every few seconds would fill the disk and pointlessly
		// hold a read transaction open. Almost certainly a typo.
		return Config{}, fmt.Errorf("backup-interval %s is too short; use at least 1m or 0 to disable", c.BackupInterval)
	}
	if c.BackupKeep < 1 {
		return Config{}, fmt.Errorf("backup-keep must be at least 1")
	}
```

And the two typed environment helpers at the bottom of the file:

```go
func envDuration(getenv func(string) string, key string, def time.Duration) time.Duration {
	v := envOr(getenv, key, "")
	if v == "" {
		return def
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		// An unparseable value falls back to the default; the flag equivalent
		// still reports a parse error, which is where a typo will be noticed.
		return def
	}
	return d
}

func envInt(getenv func(string) string, key string, def int) int {
	v := envOr(getenv, key, "")
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return n
}
```

Add `"strconv"` and `"time"` to `config.go`'s imports.

- [ ] **Step 2: Write the backup command and the maintenance loop**

Create `cmd/onsuite/backup.go`:

```go
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"database/sql"

	"github.com/iliafrenkel/on-suite/internal/platform/auth"
	"github.com/iliafrenkel/on-suite/internal/platform/config"
	"github.com/iliafrenkel/on-suite/internal/platform/db"
)

// snapshotPrefix and snapshotSuffix bracket a generated snapshot name. Only
// files matching both are ever considered for pruning, so nothing else in the
// directory can be deleted by accident.
const (
	snapshotPrefix = "onsuite-"
	snapshotSuffix = ".db"
)

func backupCmd(args []string, getenv func(string) string, out, errOut io.Writer) error {
	fs := flag.NewFlagSet("backup", flag.ContinueOnError)
	fs.SetOutput(errOut)
	dataDir := fs.String("data-dir", envOrDefault(getenv, "ONSUITE_DATA_DIR", "./data"),
		"directory holding the database")
	outPath := fs.String("out", "", "write the snapshot here instead of the backups directory")
	keep := fs.Int("keep", 0, "prune the backups directory to this many snapshots; 0 keeps everything")

	if _, err := parseInterspersed(fs, args); err != nil {
		return err
	}

	cfg := config.Config{DataDir: *dataDir}
	ctx := context.Background()
	handle, _, _, err := openDatabase(ctx, cfg)
	if err != nil {
		return err
	}
	defer func() { _ = handle.Close() }()

	if *outPath != "" {
		if err := db.BackupTo(ctx, handle, *outPath); err != nil {
			return err
		}
		fmt.Fprintf(out, "Wrote %s\n", *outPath)
		return nil
	}

	path, err := writeSnapshot(ctx, handle, cfg.BackupDir(), *keep, time.Now().UTC())
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "Wrote %s\n", path)
	return nil
}

// snapshotName is the filename for a snapshot taken at now.
//
// Colons are legal on Linux but awkward on other filesystems and in shell
// commands, so the timestamp is compacted rather than RFC 3339.
func snapshotName(now time.Time) string {
	return snapshotPrefix + now.UTC().Format("20060102T150405Z") + snapshotSuffix
}

// writeSnapshot takes a consistent snapshot into dir, then prunes to keep if
// keep is positive.
func writeSnapshot(ctx context.Context, handle *sql.DB, dir string, keep int, now time.Time) (string, error) {
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return "", fmt.Errorf("backup: create %s: %w", dir, err)
	}

	path := filepath.Join(dir, snapshotName(now))
	if err := db.BackupTo(ctx, handle, path); err != nil {
		return "", err
	}
	if keep > 0 {
		if _, err := pruneSnapshots(dir, keep); err != nil {
			// The snapshot succeeded, which is the part that matters; report
			// the pruning failure without pretending the backup failed.
			return path, fmt.Errorf("backup: wrote %s but pruning failed: %w", path, err)
		}
	}
	return path, nil
}

// pruneSnapshots deletes the oldest snapshots until keep remain, and returns
// how many it removed.
//
// It only ever considers files whose names it generated itself. A stray file in
// the backups directory — a manual copy, an unrelated archive — is left alone.
func pruneSnapshots(dir string, keep int) (int, error) {
	if keep < 1 {
		return 0, fmt.Errorf("backup: keep must be at least 1")
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return 0, nil
		}
		return 0, fmt.Errorf("backup: read %s: %w", dir, err)
	}

	var names []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if strings.HasPrefix(name, snapshotPrefix) && strings.HasSuffix(name, snapshotSuffix) {
			names = append(names, name)
		}
	}
	// The timestamp format sorts chronologically as text, so a lexical sort is
	// an age sort and no file needs to be stat'ed.
	sort.Strings(names)

	if len(names) <= keep {
		return 0, nil
	}

	removed := 0
	for _, name := range names[:len(names)-keep] {
		if err := os.Remove(filepath.Join(dir, name)); err != nil {
			return removed, fmt.Errorf("backup: remove %s: %w", name, err)
		}
		removed++
	}
	return removed, nil
}

// runMaintenance snapshots the database and sweeps expired sessions on a
// timer, until ctx is cancelled.
//
// It runs one interval after startup rather than immediately, so restarting the
// server repeatedly does not fill the backups directory.
func runMaintenance(
	ctx context.Context,
	handle *sql.DB,
	users *auth.Store,
	cfg config.Config,
	log *slog.Logger,
) {
	if cfg.BackupInterval <= 0 {
		log.Info("internal backup schedule disabled")
		return
	}

	ticker := time.NewTicker(cfg.BackupInterval)
	defer ticker.Stop()

	log.Info("maintenance scheduled",
		"interval", cfg.BackupInterval.String(), "keep", cfg.BackupKeep)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			maintain(ctx, handle, users, cfg, log)
		}
	}
}

// maintain is one maintenance pass. Every failure is logged and swallowed: a
// backup problem must not take the server down while it is serving requests.
func maintain(
	ctx context.Context,
	handle *sql.DB,
	users *auth.Store,
	cfg config.Config,
	log *slog.Logger,
) {
	swept, err := users.DeleteExpiredSessions(ctx)
	if err != nil {
		log.Error("sweeping expired sessions failed", "error", err)
	} else if swept > 0 {
		log.Info("expired sessions swept", "count", swept)
	}

	path, err := writeSnapshot(ctx, handle, cfg.BackupDir(), cfg.BackupKeep, time.Now().UTC())
	if err != nil {
		log.Error("snapshot failed", "error", err, "path", path)
		return
	}
	log.Info("snapshot written", "path", path)
}
```

- [ ] **Step 3: Dispatch the command and start the loop**

In `cmd/onsuite/main.go`, add a case:

```go
	case "backup":
		return backupCmd(args[1:], getenv, os.Stdout, errOut)
```

and a usage line:

```
  onsuite backup            write a database snapshot
```

In `cmd/onsuite/serve.go`, start the loop just before `return listenAndServe(...)`, and stop it when the server stops:

```go
	maintenanceCtx, stopMaintenance := context.WithCancel(context.Background())
	defer stopMaintenance()
	go runMaintenance(maintenanceCtx, handle, users, cfg, log)

	return listenAndServe(context.Background(), srv, log)
```

The `defer` fires after `listenAndServe` returns, which is after requests have drained — so a snapshot can never start while the database is being closed.

- [ ] **Step 4: Test it**

Create `cmd/onsuite/backup_test.go`:

```go
package main

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/iliafrenkel/on-suite/internal/platform/db"
)

func TestSnapshotNameSortsChronologically(t *testing.T) {
	early := snapshotName(time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC))
	later := snapshotName(time.Date(2026, 12, 31, 23, 59, 59, 0, time.UTC))

	if early >= later {
		t.Errorf("%q should sort before %q", early, later)
	}
	if !strings.HasPrefix(early, snapshotPrefix) || !strings.HasSuffix(early, snapshotSuffix) {
		t.Errorf("name %q does not match the prune pattern", early)
	}
	// Colons would be awkward in shell commands and on other filesystems.
	if strings.Contains(early, ":") {
		t.Errorf("name %q contains a colon", early)
	}
}

func TestBackupCmdWritesARestorableSnapshot(t *testing.T) {
	dir := seedDatabase(t)

	var out bytes.Buffer
	if err := backupCmd([]string{"--data-dir", dir}, nil, &out, io.Discard); err != nil {
		t.Fatalf("backupCmd: %v", err)
	}

	entries, err := os.ReadDir(filepath.Join(dir, "backups"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("%d files in backups/, want 1", len(entries))
	}
	if !strings.Contains(out.String(), entries[0].Name()) {
		t.Errorf("output %q does not name the snapshot", out.String())
	}

	// The snapshot must be a working database with the account in it, not
	// merely a file that exists.
	snapshot := filepath.Join(dir, "backups", entries[0].Name())
	handle, err := db.Open(snapshot)
	if err != nil {
		t.Fatalf("the snapshot does not open: %v", err)
	}
	defer func() { _ = handle.Close() }()

	var users int
	if err := handle.QueryRow("SELECT count(*) FROM users").Scan(&users); err != nil {
		t.Fatalf("querying the snapshot: %v", err)
	}
	if users != 1 {
		t.Errorf("the snapshot has %d users, want 1", users)
	}
}

func TestBackupCmdOutFlag(t *testing.T) {
	dir := seedDatabase(t)
	target := filepath.Join(t.TempDir(), "explicit.db")

	if err := backupCmd([]string{"--data-dir", dir, "--out", target}, nil, io.Discard, io.Discard); err != nil {
		t.Fatalf("backupCmd: %v", err)
	}
	if _, err := os.Stat(target); err != nil {
		t.Errorf("the snapshot was not written to --out: %v", err)
	}
	// Refusing to overwrite is db.BackupTo's contract; confirm it survives here.
	if err := backupCmd([]string{"--data-dir", dir, "--out", target}, nil, io.Discard, io.Discard); err == nil {
		t.Error("backupCmd overwrote an existing snapshot")
	}
}

func TestPruneSnapshotsKeepsTheNewest(t *testing.T) {
	dir := t.TempDir()

	var names []string
	for day := 1; day <= 5; day++ {
		name := snapshotName(time.Date(2026, 8, day, 3, 0, 0, 0, time.UTC))
		names = append(names, name)
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	removed, err := pruneSnapshots(dir, 2)
	if err != nil {
		t.Fatalf("pruneSnapshots: %v", err)
	}
	if removed != 3 {
		t.Errorf("removed %d, want 3", removed)
	}
	for _, gone := range names[:3] {
		if _, err := os.Stat(filepath.Join(dir, gone)); err == nil {
			t.Errorf("%s should have been pruned", gone)
		}
	}
	for _, kept := range names[3:] {
		if _, err := os.Stat(filepath.Join(dir, kept)); err != nil {
			t.Errorf("%s should have been kept", kept)
		}
	}
}

// TestPruneSnapshotsLeavesStrangersAlone is the important one: this function
// deletes files, so it must only ever delete files it made.
func TestPruneSnapshotsLeavesStrangersAlone(t *testing.T) {
	dir := t.TempDir()

	strangers := []string{
		"important-notes.txt",
		"onsuite.db",                 // the live database, were dir misconfigured
		"manual-copy-before-upgrade", // no prefix, no suffix
		"onsuite-notes.md",           // right prefix, wrong suffix
	}
	for _, name := range strangers {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("keep me"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	for day := 1; day <= 4; day++ {
		name := snapshotName(time.Date(2026, 8, day, 3, 0, 0, 0, time.UTC))
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	if _, err := pruneSnapshots(dir, 1); err != nil {
		t.Fatal(err)
	}
	for _, name := range strangers {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Errorf("pruning deleted an unrelated file %q", name)
		}
	}
}

func TestPruneSnapshotsEdgeCases(t *testing.T) {
	dir := t.TempDir()

	// Nothing to do, and no error.
	if removed, err := pruneSnapshots(dir, 3); err != nil || removed != 0 {
		t.Errorf("empty directory: removed %d, err %v", removed, err)
	}
	// A missing directory is not an error: nothing has been backed up yet.
	if removed, err := pruneSnapshots(filepath.Join(dir, "absent"), 3); err != nil || removed != 0 {
		t.Errorf("missing directory: removed %d, err %v", removed, err)
	}
	// keep must be sane, because this function deletes things.
	if _, err := pruneSnapshots(dir, 0); err == nil {
		t.Error("pruneSnapshots(0) was allowed")
	}
}

func TestWriteSnapshotPrunesAsItGoes(t *testing.T) {
	dir := seedDatabase(t)
	handle, err := db.Open(filepath.Join(dir, "onsuite.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = handle.Close() }()

	backups := filepath.Join(dir, "backups")
	base := time.Date(2026, 8, 18, 3, 0, 0, 0, time.UTC)
	for i := range 5 {
		if _, err := writeSnapshot(context.Background(), handle, backups, 3, base.Add(time.Duration(i)*24*time.Hour)); err != nil {
			t.Fatalf("writeSnapshot %d: %v", i, err)
		}
	}

	entries, err := os.ReadDir(backups)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 3 {
		t.Errorf("%d snapshots retained, want 3", len(entries))
	}
}
```

Add to `internal/platform/config/config_test.go`:

```go
func TestParseBackupSettings(t *testing.T) {
	c, err := Parse(nil, nil, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if c.BackupInterval != 24*time.Hour {
		t.Errorf("default BackupInterval = %v, want 24h", c.BackupInterval)
	}
	if c.BackupKeep != 7 {
		t.Errorf("default BackupKeep = %d, want 7", c.BackupKeep)
	}

	c, err = Parse([]string{"-backup-interval", "0", "-backup-keep", "30"}, nil, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if c.BackupInterval != 0 {
		t.Errorf("BackupInterval = %v, want 0 (disabled)", c.BackupInterval)
	}
	if c.BackupKeep != 30 {
		t.Errorf("BackupKeep = %d, want 30", c.BackupKeep)
	}

	getenv := func(k string) string {
		return map[string]string{"ONSUITE_BACKUP_INTERVAL": "6h", "ONSUITE_BACKUP_KEEP": "4"}[k]
	}
	if c, err = Parse(nil, getenv, io.Discard); err != nil {
		t.Fatal(err)
	}
	if c.BackupInterval != 6*time.Hour || c.BackupKeep != 4 {
		t.Errorf("env values ignored: %v, %d", c.BackupInterval, c.BackupKeep)
	}
}

func TestParseRejectsBadBackupSettings(t *testing.T) {
	tests := []struct{ name string; args []string }{
		{"negative interval", []string{"-backup-interval", "-1h"}},
		{"absurdly short interval", []string{"-backup-interval", "2s"}},
		{"keep of zero", []string{"-backup-keep", "0"}},
		{"negative keep", []string{"-backup-keep", "-3"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := Parse(tt.args, nil, io.Discard); err == nil {
				t.Fatal("Parse accepted it")
			}
		})
	}
}
```

Add `"time"` to `config_test.go`'s imports.

- [ ] **Step 5: Verify by hand**

```bash
go run ./cmd/onsuite backup --data-dir /tmp/onsuite-dev
ls -l /tmp/onsuite-dev/backups/
```

Expected: one `onsuite-<timestamp>.db`. Then confirm the maintenance loop announces itself and can be turned off:

```bash
go run ./cmd/onsuite serve --data-dir /tmp/onsuite-dev --backup-interval 1m --backup-keep 2
```

Expected: a `maintenance scheduled` log line, then a `snapshot written` line about a minute later, and `backups/` never holding more than two files. With `--backup-interval 0` the log says the schedule is disabled and no snapshots appear.

- [ ] **Step 6: Commit**

```bash
git add internal cmd
git commit -m "Add snapshot backups, retention and periodic maintenance

Snapshots use VACUUM INTO, so they are consistent without taking the database
offline, and are named with a compact UTC timestamp that sorts chronologically
as text — which means pruning is a lexical sort and never stats a file.

Pruning only considers files matching the name it generates itself, so a manual
copy or an unrelated file in the backups directory is never deleted. A test
puts strangers in the directory and asserts they survive.

The maintenance loop also sweeps expired sessions, closing a gap from Plan 1:
DeleteExpiredSessions existed and nothing called it, so rows accumulated
forever. Failures in a pass are logged and swallowed, because a backup problem
must not stop a server that is otherwise serving fine."
```

---
# Task 21: TLS — behind a proxy, or on its own

**Files:**
- Create: `cmd/onsuite/tls.go`
- Modify: `cmd/onsuite/serve.go` (choose the serving mode)
- Modify: `internal/platform/config/config.go` (cache directory, HTTP address, TLS-aware default port)
- Test: `cmd/onsuite/tls_test.go`, `internal/platform/config/config_test.go`

**Interfaces:**
- Produces:
  - `config.Config.TLSHTTPAddr string`, `(Config).TLSCacheDir() string`
  - `(Config).TLSEnabled() bool`
  - `serveHTTP(ctx, srv, log) error` and `serveAutocert(ctx, cfg, srv, log) error` in package `main`

**Design notes:**
- `autocert` lives in `golang.org/x/crypto`, already a dependency. **This task adds nothing to `go.mod`.**
- The HTTP listener does double duty: it answers the ACME HTTP-01 challenge and redirects everything else to HTTPS. `autocert` also supports TLS-ALPN-01 on 443 alone, so a host that cannot expose port 80 still works — set `--tls-http-addr ""` to skip the HTTP listener entirely.
- **`Secure` on cookies is derived from `TLSDomain`.** That was already true from Plan 2, and it is why a plain-HTTP dev server can still log in: a `Secure` cookie is never sent over `http://`. Behind a TLS-terminating proxy the site is HTTPS but `TLSDomain` is empty, so cookies are *not* marked `Secure`. That is a real limitation, and Step 2 adds an explicit flag for it rather than leaving it implicit.

- [ ] **Step 1: Add the configuration**

In `internal/platform/config/config.go`, add to `Config`:

```go
	// TLSHTTPAddr is where the plain-HTTP listener runs when built-in TLS is
	// enabled. It answers ACME HTTP-01 challenges and redirects to HTTPS.
	// Empty disables it, leaving only TLS-ALPN-01 on the HTTPS port.
	TLSHTTPAddr string
	// SecureCookies marks session and CSRF cookies Secure. It is implied by
	// TLSDomain and must be set explicitly when a TLS-terminating proxy is in
	// front, because from this process's point of view that traffic is plain
	// HTTP.
	SecureCookies bool
```

Register the flags in `Parse`:

```go
	fs.StringVar(&c.TLSHTTPAddr, "tls-http-addr", envOr(getenv, "ONSUITE_TLS_HTTP_ADDR", ":80"),
		"plain-HTTP address for ACME challenges and HTTPS redirects; empty to disable")
	secureCookies := fs.Bool("secure-cookies", envOr(getenv, "ONSUITE_SECURE_COOKIES", "") == "true",
		"mark cookies Secure; implied by -tls-domain, set this behind an HTTPS proxy")
```

After `fs.Parse`, add the derived values:

```go
	// Which flags were given explicitly, so a TLS-aware default for the listen
	// address does not override a deliberate choice.
	explicit := make(map[string]bool)
	fs.Visit(func(f *flag.Flag) { explicit[f.Name] = true })

	if c.TLSDomain != "" && !explicit["addr"] && envOr(getenv, "ONSUITE_ADDR", "") == "" {
		// Serving your own certificates on port 8080 is almost never what
		// anyone means.
		c.Addr = ":443"
	}

	// TLS implies Secure cookies; the flag can also switch them on alone, for
	// a proxy that terminates TLS upstream.
	c.SecureCookies = *secureCookies || c.TLSDomain != ""
```

And two helpers:

```go
// TLSEnabled reports whether the binary obtains its own certificates.
func (c Config) TLSEnabled() bool { return c.TLSDomain != "" }

// TLSCacheDir is where Let's Encrypt certificates are stored. It lives inside
// the data directory so the whole persistent state is still one tree.
func (c Config) TLSCacheDir() string { return filepath.Join(c.DataDir, "certs") }
```

- [ ] **Step 2: Write the serving modes**

Create `cmd/onsuite/tls.go`:

```go
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"golang.org/x/crypto/acme/autocert"

	"github.com/iliafrenkel/on-suite/internal/platform/config"
)

// serveAutocert runs srv over HTTPS with certificates obtained automatically
// from Let's Encrypt, plus a plain-HTTP listener for challenges and redirects.
//
// This exists so the promise the project was built on — one binary, no other
// software — is literally true for anyone who wants it that way. Running behind
// Caddy or nginx is still the recommended setup, and is what serveHTTP does.
func serveAutocert(parent context.Context, cfg config.Config, srv *http.Server, log *slog.Logger) error {
	if err := os.MkdirAll(cfg.TLSCacheDir(), 0o700); err != nil {
		return fmt.Errorf("create certificate cache %s: %w", cfg.TLSCacheDir(), err)
	}

	manager := &autocert.Manager{
		Prompt: autocert.AcceptTOS,
		// Only this host. Without a policy, autocert would attempt a
		// certificate for any name presented to it, which is an easy way to be
		// rate-limited by Let's Encrypt.
		HostPolicy: autocert.HostWhitelist(cfg.TLSDomain),
		Cache:      autocert.DirCache(cfg.TLSCacheDir()),
	}
	srv.TLSConfig = manager.TLSConfig()

	ctx, stop := signal.NotifyContext(parent, os.Interrupt, syscall.SIGTERM)
	defer stop()

	// The HTTP listener answers ACME HTTP-01 and redirects everything else.
	// It is optional: TLS-ALPN-01 works on the HTTPS port alone, which matters
	// on a host where port 80 is unavailable.
	var redirect *http.Server
	if cfg.TLSHTTPAddr != "" {
		redirect = &http.Server{
			Addr:              cfg.TLSHTTPAddr,
			Handler:           manager.HTTPHandler(nil),
			ReadHeaderTimeout: 10 * time.Second,
		}
		go func() {
			log.Info("http listener started", "addr", redirect.Addr, "purpose", "acme challenge and https redirect")
			if err := redirect.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
				// Not fatal: TLS-ALPN-01 can still get a certificate, and the
				// HTTPS listener is the one that serves the suite.
				log.Error("http listener failed", "error", err, "addr", redirect.Addr)
			}
		}()
	}

	errCh := make(chan error, 1)
	go func() {
		log.Info("listening", "addr", srv.Addr, "tls_domain", cfg.TLSDomain, "version", version)
		if err := srv.ListenAndServeTLS("", ""); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		log.Info("shutdown signal received, draining")
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	if redirect != nil {
		if err := redirect.Shutdown(shutdownCtx); err != nil {
			log.Warn("shutting down the http listener failed", "error", err)
		}
	}
	if err := srv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("shutdown: %w", err)
	}
	log.Info("stopped cleanly")
	return nil
}
```

Rename the existing `listenAndServe` in `serve.go` to `serveHTTP` so the two modes read as a pair, and update its one call site and its test. Its body does not change.

- [ ] **Step 3: Choose the mode in serve**

In `cmd/onsuite/serve.go`, replace the final `return listenAndServe(context.Background(), srv, log)` with:

```go
	if cfg.TLSEnabled() {
		return serveAutocert(context.Background(), cfg, srv, log)
	}
	return serveHTTP(context.Background(), srv, log)
```

and pass the derived cookie setting to the stack, replacing `Secure: cfg.TLSDomain != ""`:

```go
		Secure:   cfg.SecureCookies,
```

- [ ] **Step 4: Test the configuration logic**

The autocert path cannot be tested without reaching Let's Encrypt, so test the decisions around it rather than the certificate dance itself.

Create `cmd/onsuite/tls_test.go`:

```go
package main

import (
	"io"
	"testing"

	"github.com/iliafrenkel/on-suite/internal/platform/config"
)

func TestTLSModeSelection(t *testing.T) {
	tests := []struct {
		name        string
		args        []string
		wantTLS     bool
		wantAddr    string
		wantSecure  bool
	}{
		{"plain http by default", nil, false, ":8080", false},
		{"tls domain switches mode and port", []string{"-tls-domain", "on.example.com"}, true, ":443", true},
		{"an explicit address wins", []string{"-tls-domain", "on.example.com", "-addr", ":8443"}, true, ":8443", true},
		{"secure cookies behind a proxy", []string{"-secure-cookies"}, false, ":8080", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := config.Parse(tt.args, nil, io.Discard)
			if err != nil {
				t.Fatalf("Parse: %v", err)
			}
			if cfg.TLSEnabled() != tt.wantTLS {
				t.Errorf("TLSEnabled() = %v, want %v", cfg.TLSEnabled(), tt.wantTLS)
			}
			if cfg.Addr != tt.wantAddr {
				t.Errorf("Addr = %q, want %q", cfg.Addr, tt.wantAddr)
			}
			if cfg.SecureCookies != tt.wantSecure {
				t.Errorf("SecureCookies = %v, want %v", cfg.SecureCookies, tt.wantSecure)
			}
		})
	}
}

// TestTLSCacheDirIsInsideTheDataDirectory keeps the promise that the data
// directory is the entire persistent state: certificates included.
func TestTLSCacheDirIsInsideTheDataDirectory(t *testing.T) {
	cfg, err := config.Parse([]string{"-data-dir", "/var/lib/onsuite"}, nil, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := cfg.TLSCacheDir(), "/var/lib/onsuite/certs"; got != want {
		t.Errorf("TLSCacheDir() = %q, want %q", got, want)
	}
}
```

- [ ] **Step 5: Verify what can be verified locally**

A real certificate needs a public hostname, so check the decisions and the refusal paths:

```bash
go run ./cmd/onsuite serve --data-dir /tmp/onsuite-dev --tls-domain example.invalid --tls-http-addr ""
```

Expected: it logs `listening` with `addr=:443` and `tls_domain=example.invalid`. Unless you are root it will then fail to bind port 443 — that is the correct behaviour and confirms the mode switch. Try `--addr :8443` to see it bind, and confirm `/tmp/onsuite-dev/certs` is created with mode 700.

Plain HTTP must be unaffected:

```bash
go run ./cmd/onsuite serve --data-dir /tmp/onsuite-dev
```

Expected: as before, and login still works — the cookies are not `Secure`, which is what makes `http://localhost` usable.

- [ ] **Step 6: Commit**

```bash
git add internal cmd
git commit -m "Add built-in TLS alongside the reverse-proxy path

autocert lives in golang.org/x/crypto, already a dependency, so obtaining
certificates adds nothing to go.mod. HostPolicy pins the one hostname, since an
open policy invites Let's Encrypt rate limits. The plain-HTTP listener answers
ACME HTTP-01 and redirects to HTTPS, and can be disabled entirely because
TLS-ALPN-01 works on the HTTPS port alone.

Setting -tls-domain also moves the default listen address to :443 unless an
address was given explicitly, and marks cookies Secure. Behind a
TLS-terminating proxy the process only sees plain HTTP, so -secure-cookies now
exists to set that independently rather than leaving it impossible."
```

---

# Task 22: Packaging and deployment documentation

**Files:**
- Create: `.goreleaser.yaml`, `Dockerfile`, `.dockerignore`
- Create: `docs/deploy/onsuite.service`, `docs/deploy/README.md`
- Modify: `README.md`

**Interfaces:** none. No Go code changes in this task.

- [ ] **Step 1: Write the goreleaser configuration**

Create `.goreleaser.yaml`:

```yaml
# goreleaser cross-compiles the binary for every target and publishes a release
# with checksums. CGO is off everywhere, which is only possible because the
# SQLite driver is pure Go.
version: 2

before:
  hooks:
    - go mod tidy
    - go test ./... -race

builds:
  - id: onsuite
    main: ./cmd/onsuite
    binary: onsuite
    env:
      - CGO_ENABLED=0
    flags:
      - -trimpath
    ldflags:
      # -s -w drop the symbol table and DWARF data; the version is stamped in.
      - -s -w -X main.version={{ .Version }}
    goos:
      - linux
      - darwin
    goarch:
      - amd64
      - arm64

archives:
  - id: onsuite
    formats: [tar.gz]
    name_template: >-
      {{ .ProjectName }}_{{ .Version }}_{{ .Os }}_{{ .Arch }}
    files:
      - README.md
      - LICENSE
      - docs/deploy/**

checksum:
  name_template: checksums.txt

changelog:
  use: git
  sort: asc
  filters:
    exclude:
      - '^docs:'
      - '^test:'
      - Merge pull request
```

- [ ] **Step 2: Write the Dockerfile**

Create `Dockerfile`:

```dockerfile
# Docker is not the primary way to run ON Suite — a single binary and a data
# directory is — but the option costs almost nothing to maintain.
FROM golang:1.26-alpine AS build

WORKDIR /src
# Copy the module files first so dependency download is cached separately from
# the source.
COPY go.mod go.sum ./
RUN go mod download

COPY . .
ARG VERSION=docker
RUN CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X main.version=${VERSION}" -o /out/onsuite ./cmd/onsuite

# scratch, not alpine: the binary is static, so there is nothing else to ship.
FROM scratch

# Root certificates, needed only if built-in TLS is used — autocert has to
# verify Let's Encrypt's own certificate.
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=build /out/onsuite /onsuite

# An unprivileged numeric user; scratch has no /etc/passwd to name one in.
USER 65532:65532

VOLUME ["/data"]
EXPOSE 8080

ENTRYPOINT ["/onsuite"]
CMD ["serve", "--data-dir", "/data", "--addr", ":8080"]
```

Create `.dockerignore`:

```
.git
.github
dist
docs
data
*.db
*.db-wal
*.db-shm
```

- [ ] **Step 3: Write the systemd unit**

Create `docs/deploy/onsuite.service`:

```ini
# /etc/systemd/system/onsuite.service
[Unit]
Description=ON Suite
Documentation=https://github.com/iliafrenkel/on-suite
After=network-online.target
Wants=network-online.target

[Service]
Type=exec
ExecStart=/usr/local/bin/onsuite serve --data-dir /var/lib/onsuite --addr 127.0.0.1:8080

# DynamicUser gives the service its own transient account, and StateDirectory
# creates /var/lib/onsuite owned by it. Neither needs a user to be created by
# hand, and nothing outside the state directory is writable.
DynamicUser=yes
StateDirectory=onsuite

# The process only ever needs its own data directory.
ProtectSystem=strict
ProtectHome=yes
PrivateTmp=yes
PrivateDevices=yes
NoNewPrivileges=yes
ProtectKernelTunables=yes
ProtectKernelModules=yes
ProtectControlGroups=yes
RestrictAddressFamilies=AF_INET AF_INET6
RestrictNamespaces=yes
LockPersonality=yes
MemoryDenyWriteExecute=yes
SystemCallFilter=@system-service
SystemCallErrorNumber=EPERM

# SIGTERM makes the server drain in-flight requests and checkpoint the WAL, so
# give it room to finish rather than killing it mid-request.
KillSignal=SIGTERM
TimeoutStopSec=30
Restart=on-failure
RestartSec=5s

[Install]
WantedBy=multi-user.target
```

**If you use built-in TLS** rather than a reverse proxy, ports 80 and 443 are privileged, so add:

```ini
AmbientCapabilities=CAP_NET_BIND_SERVICE
CapabilityBoundingSet=CAP_NET_BIND_SERVICE
```

and change `ExecStart` to `... serve --data-dir /var/lib/onsuite --tls-domain on.example.com`.

- [ ] **Step 4: Write the deployment guide**

Create `docs/deploy/README.md`:

```markdown
# Deploying ON Suite

ON Suite is one static binary plus one data directory. The data directory holds
the database, the backups and, if you use built-in TLS, the certificates —
copying it is a complete backup of the system.

## Build

```bash
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build \
  -trimpath -ldflags "-s -w -X main.version=$(git describe --tags --always)" \
  -o onsuite ./cmd/onsuite
```

`CGO_ENABLED=0` works because the SQLite driver is pure Go. The result has no
dynamic library dependencies and runs on any kernel of the right architecture.

## Install

```bash
scp onsuite server:/tmp/onsuite
ssh server 'sudo install -m 0755 /tmp/onsuite /usr/local/bin/onsuite'
scp docs/deploy/onsuite.service server:/tmp/
ssh server 'sudo install -m 0644 /tmp/onsuite.service /etc/systemd/system/ && sudo systemctl daemon-reload'
```

Create the first account before starting the service, so there is never a
window in which the suite is running with no accounts:

```bash
sudo -u root onsuite user add ilia --admin --data-dir /var/lib/onsuite
sudo systemctl enable --now onsuite
```

The command prompts for a password without echoing it. Note that with
`DynamicUser=yes` systemd owns `/var/lib/onsuite`; if you create the account
before the first start, `chown` the directory to match or simply run
`onsuite user add` again afterwards with the service stopped.

## Choose a TLS story

**Behind a reverse proxy (recommended).** The service listens on
`127.0.0.1:8080` and the proxy terminates TLS. Because the process itself only
sees plain HTTP, pass `--secure-cookies` so session cookies are still marked
`Secure`. A minimal Caddy configuration:

```
on.example.com {
	reverse_proxy 127.0.0.1:8080
}
```

**On its own.** Drop the proxy and let the binary obtain its own certificate:

```bash
onsuite serve --data-dir /var/lib/onsuite --tls-domain on.example.com
```

The listen address defaults to `:443`, and a plain-HTTP listener on `:80`
answers ACME challenges and redirects to HTTPS. If port 80 is unavailable, pass
`--tls-http-addr ""`; certificates are then obtained over TLS-ALPN-01 on 443
alone. Both ports are privileged, so the unit needs
`AmbientCapabilities=CAP_NET_BIND_SERVICE`.

## Backups

The server snapshots itself every 24 hours by default, keeping 7 snapshots in
`<data-dir>/backups`:

```bash
onsuite serve --backup-interval 24h --backup-keep 7 ...
```

Set `--backup-interval 0` to disable that and drive it externally instead:

```bash
onsuite backup --data-dir /var/lib/onsuite --keep 30
```

Snapshots use SQLite's `VACUUM INTO`, so they are consistent without taking the
database offline. **Copying `onsuite.db` with `cp` while the server runs is not
safe** — use the command.

To restore: stop the service, replace `onsuite.db` with a snapshot, delete any
`onsuite.db-wal` and `onsuite.db-shm` beside it, start the service.

```bash
sudo systemctl stop onsuite
sudo cp /var/lib/onsuite/backups/onsuite-20260818T030000Z.db /var/lib/onsuite/onsuite.db
sudo rm -f /var/lib/onsuite/onsuite.db-wal /var/lib/onsuite/onsuite.db-shm
sudo systemctl start onsuite
```

Snapshots are not encrypted and contain everything every user has written. Send
them somewhere private.

## Upgrades

Replace the binary and restart. Migrations are forward-only and run
automatically at startup; there is no separate migration step.

```bash
sudo install -m 0755 /tmp/onsuite /usr/local/bin/onsuite
sudo systemctl restart onsuite
```

**Take a snapshot first.** There are no down migrations, so rolling back a
schema change means restoring a backup.

## Adding people

```bash
onsuite user add sasha --data-dir /var/lib/onsuite
```

There is no public sign-up page, by design. Omit `--admin` for an ordinary
account.

## Exporting your data

```bash
onsuite export ilia --data-dir /var/lib/onsuite --out ilia.json
```

Plain JSON, readable without this software. Share links are deliberately
excluded, because a share link is a credential; use a snapshot if you need a
restorable copy.

## Checking on it

```bash
curl -s localhost:8080/healthz          # version and a database ping
journalctl -u onsuite -f                # structured JSON logs
```

## Docker, if you prefer

```bash
docker build --build-arg VERSION=$(git describe --tags --always) -t onsuite .
docker run -d --name onsuite -p 8080:8080 -v onsuite-data:/data onsuite
docker exec -it onsuite /onsuite user add ilia --admin --data-dir /data
```
```

- [ ] **Step 5: Update the root README**

Replace the README's content with a description of the finished suite. Keep whatever the Plan 1 and Plan 2 executions already put there that is still accurate, and make sure it covers: what ON Suite is, the four planned apps and which exist, a quick start (`go build`, `user add`, `serve`), where the design documents and plans live, and a pointer to `docs/deploy/README.md`. Mention that adding an app is a package under `internal/apps` plus one line in `registeredApps`.

- [ ] **Step 6: Verify the packaging**

```bash
docker build --build-arg VERSION=test -t onsuite:test . && docker run --rm onsuite:test version
```

Expected: `onsuite test`. Then check the image really is minimal:

```bash
docker image inspect onsuite:test --format '{{ .Size }}' | awk '{print $1/1024/1024 " MB"}'
```

Expected: roughly 25–40 MB, almost all of it the binary.

If goreleaser is installed, check the configuration without publishing:

```bash
goreleaser check
goreleaser build --snapshot --clean --single-target
```

Expected: `check` passes and a binary appears under `dist/`. If goreleaser is not installed, skip this — CI in Task 23 validates the configuration on every push.

- [ ] **Step 7: Commit**

```bash
git add .goreleaser.yaml Dockerfile .dockerignore docs/deploy README.md
git commit -m "Add packaging, a systemd unit and a deployment guide

The systemd unit uses DynamicUser and StateDirectory, so no account has to be
created by hand and nothing outside the state directory is writable. SIGTERM
with a 30 second timeout lets the server drain requests and checkpoint the WAL
instead of being killed mid-request.

The Docker image is scratch-based with root certificates copied in, which are
needed only when the binary obtains its own certificates. Docker is not the
primary path but the option is nearly free.

The deployment guide covers the two TLS stories, and documents the one
non-obvious consequence of terminating TLS upstream: the process sees plain
HTTP, so --secure-cookies has to be passed explicitly. It also states plainly
that copying onsuite.db with cp while the server runs is not safe."
```

---

# Task 23: Continuous integration

**Files:**
- Create: `.github/workflows/ci.yml`
- Create: `.github/workflows/release.yml`

**Interfaces:** none.

- [ ] **Step 1: Fix the one staticcheck finding, before CI can go red on it**

This is the first time staticcheck runs over the whole project, and there is
exactly one finding — in code committed back in Plan 2. Fix it first, or Task 23
lands with CI already failing.

In `internal/platform/web/assets_test.go`, the comment above
`TestRealAssetsAreEmbedded` has a line beginning `// go:embed`, which both the
toolchain and staticcheck read as a malformed compiler directive (SA9009).
Replace the comment with:

```go
// TestRealAssetsAreEmbedded checks the actual embedded tree, so a missing
// embed directive or a deleted file fails the build rather than the page.
//
// The word "go:embed" is deliberately not written at the start of a comment
// line here: the toolchain and staticcheck both read "// go:embed" as a
// malformed directive (SA9009).
```

Confirm it is the only finding:

```bash
go run honnef.co/go/tools/cmd/staticcheck@latest ./...
```

Expected: no output. **Use `@latest`, not a locally installed staticcheck** — a
staticcheck built with an older Go toolchain refuses to analyse a `go 1.26`
module and reports a compile error rather than any real finding. Version 0.7.0
handles Go 1.26; anything older may not.

- [ ] **Step 1: Write the CI workflow**

Create `.github/workflows/ci.yml`:

```yaml
name: CI

on:
  push:
    branches: [main]
  pull_request:

# Cancel superseded runs on the same branch rather than paying for both.
concurrency:
  group: ci-${{ github.ref }}
  cancel-in-progress: true

permissions:
  contents: read

jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4

      - uses: actions/setup-go@v5
        with:
          # Read the version from go.mod so there is one source of truth.
          go-version-file: go.mod
          cache: true

      - name: gofmt
        run: |
          unformatted=$(gofmt -l .)
          if [ -n "$unformatted" ]; then
            echo "These files are not gofmt-clean:"
            echo "$unformatted"
            gofmt -d .
            exit 1
          fi

      - name: go vet
        run: go vet ./...

      - name: staticcheck
        # Run rather than installed as a module dependency: this project caps
        # its dependencies deliberately, and a linter is not part of the
        # program. The cost is that the version floats.
        run: go run honnef.co/go/tools/cmd/staticcheck@latest ./...

      - name: go mod tidy is clean
        run: |
          go mod tidy
          git diff --exit-code go.mod go.sum

      - name: test
        # The architecture test that enforces import boundaries runs here too.
        run: go test ./... -race -count=1

  build:
    runs-on: ubuntu-latest
    strategy:
      fail-fast: false
      matrix:
        include:
          - goos: linux
            goarch: amd64
          - goos: linux
            goarch: arm64
          - goos: darwin
            goarch: arm64
    steps:
      - uses: actions/checkout@v4

      - uses: actions/setup-go@v5
        with:
          go-version-file: go.mod
          cache: true

      - name: build
        # CGO_ENABLED=0 is the constraint the whole deployment story rests on.
        # If a dependency ever needs CGO, this is where it must be noticed.
        env:
          CGO_ENABLED: '0'
          GOOS: ${{ matrix.goos }}
          GOARCH: ${{ matrix.goarch }}
        run: go build -trimpath -o /dev/null ./cmd/onsuite

  docker:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - name: build the image
        run: docker build --build-arg VERSION=ci -t onsuite:ci .
      - name: the image runs
        run: docker run --rm onsuite:ci version
```

- [ ] **Step 2: Write the release workflow**

Create `.github/workflows/release.yml`:

```yaml
name: Release

on:
  push:
    tags: ['v*']

permissions:
  contents: write # required to create the release and upload artefacts

jobs:
  release:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
        with:
          # goreleaser builds the changelog from history, so a shallow clone
          # produces an empty one.
          fetch-depth: 0

      - uses: actions/setup-go@v5
        with:
          go-version-file: go.mod
          cache: true

      - uses: goreleaser/goreleaser-action@v6
        with:
          version: '~> v2'
          args: release --clean
        env:
          GITHUB_TOKEN: ${{ secrets.GITHUB_TOKEN }}
```

- [ ] **Step 3: Verify locally what can be verified**

The workflow steps are all runnable by hand — do that rather than pushing to find out:

```bash
gofmt -l . && go vet ./... && go run honnef.co/go/tools/cmd/staticcheck@latest ./...
```

Expected: no output from any of them. Fix anything staticcheck reports; this is the first time it has run over the whole project, so expect a few findings on the earlier plans' code.

```bash
go mod tidy && git diff --exit-code go.mod go.sum && echo "go.mod is tidy"
```

```bash
for t in linux/amd64 linux/arm64 darwin/arm64; do
  CGO_ENABLED=0 GOOS=${t%/*} GOARCH=${t#*/} go build -trimpath -o /dev/null ./cmd/onsuite && echo "OK $t"
done
```

```bash
go test ./... -race -count=1
```

- [ ] **Step 4: Commit and push**

```bash
git add .github
git commit -m "Add CI and release workflows

CI checks gofmt, vet, staticcheck, that go.mod is tidy, and the race-enabled
test suite — which includes the architecture test enforcing import boundaries.
It then cross-compiles for all three release targets with CGO_ENABLED=0, since
that constraint is what makes the single-binary deployment possible and this is
where its loss would be noticed. The Docker image is built and run so it cannot
rot unnoticed.

staticcheck runs via go run with an explicit version rather than becoming a
module dependency, because this project caps its dependencies and a linter is
not part of the program.

Releases are cut by goreleaser on a v* tag, with a full clone so the changelog
is not empty."
```

Then push and confirm the run is green on GitHub before tagging anything.

---

# Definition of done for Plan 3, and for the whole spec

1. `gofmt -l .` empty; `go vet ./...`, `staticcheck ./...` and `go test ./... -race` all clean.
2. `CGO_ENABLED=0` builds succeed for linux/amd64, linux/arm64 and darwin/arm64.
3. `go list -m all` shows exactly five direct dependencies: `modernc.org/sqlite`, `golang.org/x/crypto`, `golang.org/x/term`, `golang.org/x/net`, `github.com/alecthomas/chroma/v2`.
4. CI is green on `main`.

Spec success criteria (§11), all eight:

| # | Criterion | Where |
|---|---|---|
| 1 | Static CGO-free build, Go toolchain only | Plan 1, re-checked here |
| 2 | `user add` on an empty data directory | Plan 1 |
| 3 | Log in, land on the shell, app switcher | Plan 2 |
| 4 | ON Paste create, view, list, delete, highlighted | Tasks 16–17 |
| 5 | Unguessable public link readable signed out; only declared routes public | Task 18 |
| 6 | `onsuite backup` produces a snapshot that restores | Task 20 |
| 7 | Architecture test passes; `go test ./... -race` green in CI | Task 23 |
| 8 | Runs on Linux ARM64 under systemd, both TLS paths | Tasks 21–22 |

**Beyond the spec, deliberately:** raw text endpoints and JSON export. Both were decided during planning and are recorded in the roadmap.

**Still deferred, and still on purpose:** everything in spec §10 — full-text search, a background job scheduler, uploads, metrics, Alpine.js, multi-tenant hardening. ON Notes is the next project and gets its own spec.
