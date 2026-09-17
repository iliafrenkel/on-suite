# ON Flash F1 — Core Deck/Card CRUD Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Stand up ON Flash as a registered app with full CRUD for decks and cards (Basic + Cloze, with an optional post-answer notes field and per-card tags), with no scheduling, import, media, or sharing yet — those are later phases (F2–F6, see [docs/superpowers/specs/2026-09-17-on-flash-features.md](../specs/2026-09-17-on-flash-features.md)).

**Architecture:** A new package `internal/apps/flash` implementing `app.App`, following ON Paste's proven shape exactly (`internal/apps/paste`): one `Store` type per app, raw SQL with owner-scoped queries, a two-pane list/detail HTMX page per resource (decks, and cards nested under a deck), server-rendered `html/template`, PRG on non-HTMX submits. Tags are a lightweight many-to-many with a small cross-deck filter view.

**Tech Stack:** Go, `database/sql` + SQLite (`internal/platform/db`), `html/template` via `internal/platform/render`, htmx for partial updates, no JS build step.

## Global Constraints

- App ID: `flash`. Display name: `ON Flash`. `Meta().Order = 40` (after `notes`=10, `paste`=20, `reader`=30).
- Every table is prefixed `flash_`, uses `STRICT`, and scopes rows by `user_id INTEGER NOT NULL REFERENCES users (id) ON DELETE CASCADE` (mirrors `internal/apps/paste/migrations/0001_snippets.sql`).
- Timestamps are `TEXT`, `RFC3339Nano` UTC, via the same `formatTime`/`parseTime` convention as `internal/apps/paste/store.go:379-389`.
- Apps never import each other; `internal/apps/flash` imports only `internal/platform/*` (enforced by `internal/arch/arch_test.go`).
- Every handler test uses `internal/apptest.NewServer` and `internal/htmlassert`, matching `internal/apps/paste/handlers_test.go`.
- Full check (`gofmt -l .`, `go vet ./...`, `staticcheck`, `go mod tidy` diff, `go test ./... -race -count=1`) must stay green after every task; run it at the end of every task, not just the last one.
- Visual polish (icons, animations, mobile breakpoints) is out of scope for F1 — reuse the existing utility classes already in `internal/ui/static/app.css` (`stack`, `field`, `notice`, `notice-error`, `dim`, `row`, `toolbar-btn`, `list-head`) rather than adding new CSS. A dedicated UI-polish phase (mirroring ON Reader's UI-polish and keyboard-shortcut phases) comes later, not in F1.

---

### Task 1: Deck domain & store

**Files:**
- Create: `internal/apps/flash/store.go`
- Create: `internal/apps/flash/deck.go`
- Create: `internal/apps/flash/migrations/0001_decks.sql`
- Test: `internal/apps/flash/deck_test.go`

**Interfaces:**
- Produces: `flash.ID = "flash"`; `flash.Migrations() fs.FS`; `flash.NewStore(handle *sql.DB) *flash.Store`; `(*Store).SetClock(now func() time.Time)`; `flash.Deck{ID, UserID, Name, Description int64/string, CreatedAt time.Time}`; `flash.ErrNotFound`, `flash.ErrInvalid` (both `errors.New`, wrapped with `%w`); `flash.ValidateDeck(name, description string) error`; `(*Store).CreateDeck(ctx, userID int64, name, description string) (Deck, error)`; `(*Store).UpdateDeck(ctx, userID, id int64, name, description string) (Deck, error)`; `(*Store).DeckByID(ctx, userID, id int64) (Deck, error)`; `(*Store).ListDecks(ctx, userID int64) ([]Deck, error)`; `(*Store).DeleteDeck(ctx, userID, id int64) error`.
- Consumes: nothing (first task).

- [ ] **Step 1: Write the failing store test**

```go
// internal/apps/flash/deck_test.go
package flash_test

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/iliafrenkel/on-suite/internal/apps/flash"
	"github.com/iliafrenkel/on-suite/internal/apptest"
	"github.com/iliafrenkel/on-suite/internal/platform/auth"
	"github.com/iliafrenkel/on-suite/internal/platform/db"
)

// fixture is a migrated database with two users, so every owner-scoping test
// has somebody else to be confused with.
type fixture struct {
	store *flash.Store
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

	migrations, err := db.Collect(auth.Namespace, auth.Migrations())
	if err != nil {
		t.Fatal(err)
	}
	appMigrations, err := db.Collect(flash.ID, flash.Migrations())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Apply(ctx, handle, append(migrations, appMigrations...)); err != nil {
		t.Fatal(err)
	}

	users := auth.NewStore(handle)
	alice, err := users.CreateUser(ctx, "alice", apptest.PasswordHash, true)
	if err != nil {
		t.Fatal(err)
	}
	bob, err := users.CreateUser(ctx, "bob", apptest.PasswordHash, false)
	if err != nil {
		t.Fatal(err)
	}
	return &fixture{store: flash.NewStore(handle), db: handle, alice: alice, bob: bob}
}

func TestCreateAndFetchDeck(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	created, err := f.store.CreateDeck(ctx, f.alice.ID, "  Spanish  ", "Travel phrases")
	if err != nil {
		t.Fatalf("CreateDeck: %v", err)
	}
	if created.ID == 0 {
		t.Error("CreateDeck returned id 0")
	}
	if created.Name != "Spanish" {
		t.Errorf("Name = %q; whitespace should be trimmed", created.Name)
	}
	if created.CreatedAt.IsZero() {
		t.Error("CreatedAt is zero")
	}

	got, err := f.store.DeckByID(ctx, f.alice.ID, created.ID)
	if err != nil {
		t.Fatalf("DeckByID: %v", err)
	}
	if got.Description != "Travel phrases" {
		t.Errorf("round trip lost data: %+v", got)
	}
}

func TestCreateDeckRejectsDuplicateName(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	if _, err := f.store.CreateDeck(ctx, f.alice.ID, "Spanish", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.CreateDeck(ctx, f.alice.ID, "Spanish", ""); !errors.Is(err, flash.ErrInvalid) {
		t.Errorf("second CreateDeck with the same name = %v, want ErrInvalid", err)
	}
	// Another user may still use the same name.
	if _, err := f.store.CreateDeck(ctx, f.bob.ID, "Spanish", ""); err != nil {
		t.Errorf("bob could not create a deck named the same as alice's: %v", err)
	}
}

func TestUpdateDeck(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	created, err := f.store.CreateDeck(ctx, f.alice.ID, "Original", "")
	if err != nil {
		t.Fatal(err)
	}
	updated, err := f.store.UpdateDeck(ctx, f.alice.ID, created.ID, "  Renamed  ", "new description")
	if err != nil {
		t.Fatalf("UpdateDeck: %v", err)
	}
	if updated.Name != "Renamed" || updated.Description != "new description" {
		t.Errorf("UpdateDeck = %+v", updated)
	}
	if !updated.CreatedAt.Equal(created.CreatedAt) {
		t.Errorf("CreatedAt changed: got %v, want %v", updated.CreatedAt, created.CreatedAt)
	}
}

func TestUpdateDeckRejectsSomeoneElsesDeck(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	created, err := f.store.CreateDeck(ctx, f.alice.ID, "alice's", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.UpdateDeck(ctx, f.bob.ID, created.ID, "hijacked", ""); !errors.Is(err, flash.ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}

// TestDeckOwnerScoping is the security property of this store.
func TestDeckOwnerScoping(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	d, err := f.store.CreateDeck(ctx, f.alice.ID, "alice's", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.DeckByID(ctx, f.bob.ID, d.ID); !errors.Is(err, flash.ErrNotFound) {
		t.Errorf("DeckByID as another user = %v, want ErrNotFound", err)
	}
	if err := f.store.DeleteDeck(ctx, f.bob.ID, d.ID); !errors.Is(err, flash.ErrNotFound) {
		t.Errorf("DeleteDeck as another user = %v, want ErrNotFound", err)
	}
	if _, err := f.store.DeckByID(ctx, f.alice.ID, d.ID); err != nil {
		t.Errorf("the owner lost access: %v", err)
	}
}

func TestListDecksIsNewestFirstAndOwnerScoped(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	base := time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC)
	for i, name := range []string{"first", "second", "third"} {
		f.store.SetClock(func() time.Time { return base.Add(time.Duration(i) * time.Minute) })
		if _, err := f.store.CreateDeck(ctx, f.alice.ID, name, ""); err != nil {
			t.Fatal(err)
		}
	}
	f.store.SetClock(func() time.Time { return base })
	if _, err := f.store.CreateDeck(ctx, f.bob.ID, "bob's", ""); err != nil {
		t.Fatal(err)
	}

	got, err := f.store.ListDecks(ctx, f.alice.ID)
	if err != nil {
		t.Fatalf("ListDecks: %v", err)
	}
	var names []string
	for _, d := range got {
		names = append(names, d.Name)
	}
	if want := "third,second,first"; strings.Join(names, ",") != want {
		t.Errorf("ListDecks order = %q, want %q", strings.Join(names, ","), want)
	}
}

func TestDeleteDeck(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	d, err := f.store.CreateDeck(ctx, f.alice.ID, "doomed", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := f.store.DeleteDeck(ctx, f.alice.ID, d.ID); err != nil {
		t.Fatalf("DeleteDeck: %v", err)
	}
	if _, err := f.store.DeckByID(ctx, f.alice.ID, d.ID); !errors.Is(err, flash.ErrNotFound) {
		t.Error("the deck survived deletion")
	}
	if err := f.store.DeleteDeck(ctx, f.alice.ID, d.ID); !errors.Is(err, flash.ErrNotFound) {
		t.Errorf("second DeleteDeck = %v, want ErrNotFound", err)
	}
}

func TestValidateDeck(t *testing.T) {
	tests := []struct {
		name    string
		deck    string
		wantErr bool
	}{
		{"ordinary", "Spanish", false},
		{"empty name", "", true},
		{"whitespace-only name", "   ", true},
		{"name at the limit", strings.Repeat("a", flash.MaxDeckNameRunes), false},
		{"name over the limit", strings.Repeat("a", flash.MaxDeckNameRunes+1), true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := flash.ValidateDeck(tt.deck, "")
			if tt.wantErr && !errors.Is(err, flash.ErrInvalid) {
				t.Errorf("ValidateDeck = %v, want ErrInvalid", err)
			}
			if !tt.wantErr && err != nil {
				t.Errorf("ValidateDeck rejected a valid deck: %v", err)
			}
		})
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/apps/flash/... -run TestCreateAndFetchDeck -v`
Expected: FAIL — `package flash does not exist` / compile error, since none of `store.go`/`deck.go`/the migration exist yet.

- [ ] **Step 3: Write the migration**

```sql
-- internal/apps/flash/migrations/0001_decks.sql
CREATE TABLE flash_decks (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id     INTEGER NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    name        TEXT    NOT NULL,
    description TEXT    NOT NULL DEFAULT '',
    created_at  TEXT    NOT NULL
) STRICT;

CREATE INDEX flash_decks_user_created_idx
    ON flash_decks (user_id, created_at DESC);

-- A user cannot have two decks with the same name; this is what turns a
-- second CreateDeck("Spanish", ...) into a friendly ErrInvalid instead of a
-- silent duplicate the list page cannot tell apart.
CREATE UNIQUE INDEX flash_decks_user_name_idx
    ON flash_decks (user_id, name);
```

- [ ] **Step 4: Write `store.go`**

```go
// Package flash implements ON Flash, a flash-card app for learning anything
// by generating cards elsewhere (an LLM, mostly) and reviewing them here.
//
// It depends only on internal/platform/*. It never imports another app, and
// no platform package imports it: the whole coupling is the app.App
// interface plus one line in cmd/onsuite/main.go.
package flash

import (
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"strings"
	"time"
)

// ID is the app id: the URL prefix, the migration namespace, and the prefix
// on every table this app owns.
const ID = "flash"

//go:embed migrations/*.sql
var migrationFiles embed.FS

// Migrations returns this app's schema with filenames at the root, which is
// what db.Collect expects.
func Migrations() fs.FS {
	sub, err := fs.Sub(migrationFiles, "migrations")
	if err != nil {
		// Unreachable: the path is a compile-time constant checked by go:embed.
		panic("flash: embedded migrations missing: " + err.Error())
	}
	return sub
}

var (
	// ErrNotFound covers both "no such row" and "not yours". They are
	// deliberately indistinguishable: returning a 403 for someone else's
	// deck or card would confirm that it exists.
	ErrNotFound = errors.New("flash: not found")
	ErrInvalid  = errors.New("flash: invalid input")
)

// Store is all the SQL for ON Flash. It has no HTTP knowledge, so it can be
// tested against a real database on its own.
type Store struct {
	db  *sql.DB
	now func() time.Time
}

// NewStore wraps an open database handle.
func NewStore(handle *sql.DB) *Store {
	return &Store{db: handle, now: func() time.Time { return time.Now().UTC() }}
}

// SetClock replaces the time source, for tests.
func (st *Store) SetClock(now func() time.Time) { st.now = now }

// rowScanner is satisfied by both *sql.Row and *sql.Rows.
type rowScanner interface {
	Scan(dest ...any) error
}

// isUniqueViolation avoids importing the driver package just to read an error
// code. Matching on the message is unattractive but keeps this package free
// of a driver dependency, and the substring is stable in SQLite. Mirrors
// internal/platform/auth/store.go's own isUniqueViolation; that package
// cannot be imported here for this alone (apps never import each other, and
// this is platform-internal), so it is an independent implementation with
// the same justification.
func isUniqueViolation(err error) bool {
	return err != nil && strings.Contains(err.Error(), "UNIQUE constraint failed")
}

// Timestamps match the platform's convention: RFC 3339 nanoseconds in UTC,
// which sorts chronologically as text.
func formatTime(t time.Time) string { return t.UTC().Format(time.RFC3339Nano) }

func parseTime(s string) (time.Time, error) {
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return time.Time{}, fmt.Errorf("flash: parse timestamp %q: %w", s, err)
	}
	return t.UTC(), nil
}
```

- [ ] **Step 5: Write `deck.go`**

```go
// internal/apps/flash/deck.go
package flash

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	// MaxDeckNameRunes bounds a deck name so the list page stays readable.
	MaxDeckNameRunes = 120
	// MaxDeckDescriptionRunes bounds the optional description.
	MaxDeckDescriptionRunes = 500
)

// Deck is one flash-card deck.
type Deck struct {
	ID          int64
	UserID      int64
	Name        string
	Description string
	CreatedAt   time.Time
}

// ValidateDeck checks a deck's user-supplied fields. Exported because the
// handler reports these messages back to the user.
func ValidateDeck(name, description string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("%w: the deck needs a name", ErrInvalid)
	}
	if utf8.RuneCountInString(name) > MaxDeckNameRunes {
		return fmt.Errorf("%w: the name is longer than %d characters", ErrInvalid, MaxDeckNameRunes)
	}
	if !utf8.ValidString(name) {
		return fmt.Errorf("%w: the name is not valid UTF-8", ErrInvalid)
	}
	if utf8.RuneCountInString(description) > MaxDeckDescriptionRunes {
		return fmt.Errorf("%w: the description is longer than %d characters", ErrInvalid, MaxDeckDescriptionRunes)
	}
	if !utf8.ValidString(description) {
		return fmt.Errorf("%w: the description is not valid UTF-8", ErrInvalid)
	}
	return nil
}

// CreateDeck stores a new deck.
func (st *Store) CreateDeck(ctx context.Context, userID int64, name, description string) (Deck, error) {
	name = strings.TrimSpace(name)
	if err := ValidateDeck(name, description); err != nil {
		return Deck{}, err
	}

	d := Deck{UserID: userID, Name: name, Description: description, CreatedAt: st.now()}
	err := st.db.QueryRowContext(ctx,
		`INSERT INTO flash_decks (user_id, name, description, created_at)
		 VALUES (?, ?, ?, ?)
		 RETURNING id`,
		d.UserID, d.Name, d.Description, formatTime(d.CreatedAt),
	).Scan(&d.ID)
	if err != nil {
		if isUniqueViolation(err) {
			return Deck{}, fmt.Errorf("%w: you already have a deck named %q", ErrInvalid, name)
		}
		return Deck{}, fmt.Errorf("flash: create deck: %w", err)
	}
	return d, nil
}

// UpdateDeck overwrites userID's own deck's editable fields.
func (st *Store) UpdateDeck(ctx context.Context, userID, id int64, name, description string) (Deck, error) {
	name = strings.TrimSpace(name)
	if err := ValidateDeck(name, description); err != nil {
		return Deck{}, err
	}

	res, err := st.db.ExecContext(ctx,
		`UPDATE flash_decks SET name = ?, description = ? WHERE id = ? AND user_id = ?`,
		name, description, id, userID)
	if err != nil {
		if isUniqueViolation(err) {
			return Deck{}, fmt.Errorf("%w: you already have a deck named %q", ErrInvalid, name)
		}
		return Deck{}, fmt.Errorf("flash: update deck: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return Deck{}, fmt.Errorf("flash: update deck: %w", err)
	}
	if n == 0 {
		return Deck{}, ErrNotFound
	}
	return st.DeckByID(ctx, userID, id)
}

// DeckByID fetches one of userID's own decks.
func (st *Store) DeckByID(ctx context.Context, userID, id int64) (Deck, error) {
	return scanDeck(st.db.QueryRowContext(ctx,
		`SELECT id, user_id, name, description, created_at
		 FROM flash_decks WHERE id = ? AND user_id = ?`, id, userID))
}

// ListDecks returns userID's decks, newest first.
func (st *Store) ListDecks(ctx context.Context, userID int64) ([]Deck, error) {
	rows, err := st.db.QueryContext(ctx,
		`SELECT id, user_id, name, description, created_at
		 FROM flash_decks WHERE user_id = ?
		 ORDER BY created_at DESC, id DESC`, userID)
	if err != nil {
		return nil, fmt.Errorf("flash: list decks: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []Deck
	for rows.Next() {
		d, err := scanDeckRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("flash: list decks: %w", err)
	}
	return out, nil
}

// DeleteDeck removes one of userID's own decks. Its cards go with it via
// ON DELETE CASCADE once Task 3 adds flash_cards.
func (st *Store) DeleteDeck(ctx context.Context, userID, id int64) error {
	res, err := st.db.ExecContext(ctx,
		`DELETE FROM flash_decks WHERE id = ? AND user_id = ?`, id, userID)
	if err != nil {
		return fmt.Errorf("flash: delete deck: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("flash: delete deck: %w", err)
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func scanDeck(row *sql.Row) (Deck, error) {
	d, err := scanDeckRow(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Deck{}, ErrNotFound
	}
	return d, err
}

func scanDeckRow(row rowScanner) (Deck, error) {
	var (
		d         Deck
		createdAt string
	)
	err := row.Scan(&d.ID, &d.UserID, &d.Name, &d.Description, &createdAt)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return Deck{}, sql.ErrNoRows // translated by scanDeck
	case err != nil:
		return Deck{}, fmt.Errorf("flash: scan deck: %w", err)
	}
	if d.CreatedAt, err = parseTime(createdAt); err != nil {
		return Deck{}, err
	}
	return d, nil
}
```

- [ ] **Step 6: Run the test to verify it passes**

Run: `go test ./internal/apps/flash/... -v`
Expected: PASS (all `TestXxxDeck` tests).

- [ ] **Step 7: Commit**

```bash
git checkout -b feat/flash-f1-deck-card-crud
git add internal/apps/flash/store.go internal/apps/flash/deck.go internal/apps/flash/migrations/0001_decks.sql internal/apps/flash/deck_test.go
git commit -m "feat(flash): add deck domain and store"
```

---

### Task 2: App scaffold, deck handlers, templates, and registration

**Files:**
- Create: `internal/apps/flash/flash.go`
- Create: `internal/apps/flash/handlers_decks.go`
- Create: `internal/apps/flash/templates/decks.html`
- Modify: `cmd/onsuite/main.go` (add `flash.New()` to `registeredApps()`)
- Test: `internal/apps/flash/handlers_decks_test.go`

**Interfaces:**
- Consumes: `flash.ID`, `flash.NewStore`, `flash.Deck`, `flash.ValidateDeck`, `flash.ErrNotFound`, `flash.ErrInvalid`, and the `Store` deck methods from Task 1.
- Produces: `flash.App` (implements `app.App`); `flash.New() *App`; routes `GET /flash/{$}`, `GET /flash/new`, `POST /flash/new`, `GET /flash/{deckID}`, `GET /flash/edit/{deckID}`, `POST /flash/{deckID}`, `POST /flash/{deckID}/delete`. Later tasks add card routes under `/flash/{deckID}/cards/...` and reuse `a.userID`, `a.fail`, `a.render` from this task.

- [ ] **Step 1: Write the failing handler test**

```go
// internal/apps/flash/handlers_decks_test.go
package flash_test

import (
	"net/url"
	"testing"

	"github.com/iliafrenkel/on-suite/internal/apps/flash"
	"github.com/iliafrenkel/on-suite/internal/apptest"
	"github.com/iliafrenkel/on-suite/internal/htmlassert"
)

func newServer(t *testing.T) *apptest.Server[*flash.Store] {
	t.Helper()
	return apptest.NewServer(t, flash.New(), flash.NewStore)
}

func TestFlashRequiresSignIn(t *testing.T) {
	s := newServer(t)
	rec := s.Do(t, nil, httpGet(t, "/flash/"))
	if rec.Code != 303 && rec.Code != 401 {
		t.Errorf("GET /flash/ signed out = %d, want a redirect-to-login or 401", rec.Code)
	}
}

func TestCreateDeckRequiresCSRF(t *testing.T) {
	s := newServer(t)
	req := httpPost(t, "/flash/new", url.Values{"name": {"Spanish"}})
	rec := s.Do(t, s.Alice, req)
	if rec.Code != 403 {
		t.Errorf("POST /flash/new without CSRF = %d, want 403", rec.Code)
	}
}

func TestCreateAndViewDeck(t *testing.T) {
	s := newServer(t)
	s.Submit(t, s.Alice, "/flash/new", url.Values{"name": {"Spanish"}, "description": {"Travel"}}, "")

	doc := s.Get(t, s.Alice, "/flash/")
	doc.MustHave(".deck-list")
	row := doc.MustHave(`a[href^="/flash/"]`)
	if got := htmlassert.Text(row); got == "" {
		t.Error("the new deck does not appear in the list")
	}
}

func TestCreateDeckValidation(t *testing.T) {
	s := newServer(t)
	rec := s.Post(t, s.Alice, "/flash/new", url.Values{"name": {"  "}})
	if rec.Code != 400 {
		t.Errorf("POST /flash/new with a blank name = %d, want 400", rec.Code)
	}
	doc := htmlassert.Parse(t, rec.Body.String())
	doc.MustHave(".notice-error")
}

func TestViewingSomeoneElsesDeckIs404(t *testing.T) {
	s := newServer(t)
	deck, err := s.Store.CreateDeck(t.Context(), s.Alice.User.ID, "alice's", "")
	if err != nil {
		t.Fatal(err)
	}
	rec := s.Do(t, s.Bob, httpGet(t, "/flash/"+itoa(deck.ID)))
	if rec.Code != 404 {
		t.Errorf("GET another user's deck = %d, want 404", rec.Code)
	}
}

func TestDeleteDeckRequiresCSRFAndPOST(t *testing.T) {
	s := newServer(t)
	deck, err := s.Store.CreateDeck(t.Context(), s.Alice.User.ID, "doomed", "")
	if err != nil {
		t.Fatal(err)
	}
	req := httpPost(t, "/flash/"+itoa(deck.ID)+"/delete", url.Values{})
	rec := s.Do(t, s.Alice, req)
	if rec.Code != 403 {
		t.Errorf("delete without CSRF = %d, want 403", rec.Code)
	}
}
```

Add these two tiny local test helpers to the same file (kept local, matching the "app-specific test shortcuts stay local" convention — see `internal/apps/paste/handlers_test.go`):

```go
func httpGet(t *testing.T, path string) *http.Request {
	t.Helper()
	return httptest.NewRequest(http.MethodGet, path, nil)
}

func httpPost(t *testing.T, path string, form url.Values) *http.Request {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return req
}

func itoa(id int64) string { return strconv.FormatInt(id, 10) }
```

Add the matching imports to the top of the file: `"net/http"`, `"net/http/httptest"`, `"strconv"`, `"strings"`.

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/apps/flash/... -run TestCreateAndViewDeck -v`
Expected: FAIL — compile error, `flash.New` and the routes do not exist yet.

- [ ] **Step 3: Write `flash.go`**

```go
// internal/apps/flash/flash.go
package flash

import (
	"embed"
	"io/fs"

	"github.com/iliafrenkel/on-suite/internal/platform/app"
)

//go:embed templates/*.html
var templateFiles embed.FS

// App is ON Flash. It is constructed before the platform exists, in the
// registration slice in main, and receives everything it needs in Mount.
type App struct {
	store *Store
	deps  app.Deps
}

// New returns the app for registration.
func New() *App { return &App{} }

func (a *App) Meta() app.Meta {
	return app.Meta{
		ID:      ID,
		Name:    "ON Flash",
		Summary: "Create and review flash card decks.",
		Order:   40,
	}
}

func (a *App) Migrations() fs.FS { return Migrations() }

func (a *App) Templates() fs.FS {
	sub, err := fs.Sub(templateFiles, "templates")
	if err != nil {
		// Unreachable: a compile-time constant path checked by go:embed.
		panic("flash: embedded templates missing: " + err.Error())
	}
	return sub
}

// Mount wires the app up. Everything registered with Handle requires a
// signed-in user; ON Flash has no public routes in F1.
func (a *App) Mount(r *app.Router, deps app.Deps) {
	a.deps = deps
	a.store = NewStore(deps.DB)

	// Same pattern as internal/apps/paste's Mount: a literal segment (new,
	// edit/{id}) always wins over a same-position wildcard ({id}), so these
	// do not conflict with each other. See paste.go's Mount for the fuller
	// explanation of which shapes would.
	r.HandleFunc("GET /{$}", a.deckIndex)
	r.HandleFunc("GET /new", a.newDeckForm)
	r.HandleFunc("POST /new", a.createDeck)
	r.HandleFunc("GET /{deckID}", a.deckIndex)
	r.HandleFunc("GET /edit/{deckID}", a.editDeckForm)
	r.HandleFunc("POST /{deckID}", a.updateDeck)
	r.HandleFunc("POST /{deckID}/delete", a.deleteDeck)
}
```

`flash.go` ends at `Mount`'s closing brace — it has no `net/http` handler bodies of its own; `a.render` and the actual deck handlers are defined in `handlers_decks.go` in Step 4. Drop the `"net/http"` import from `flash.go`'s import block above, since nothing in this file uses it directly (`app.Router`'s `HandleFunc` takes the already-typed `a.deckIndex` etc., no `http` package reference needed here).

- [ ] **Step 4: Write `handlers_decks.go`**

```go
// internal/apps/flash/handlers_decks.go
package flash

import (
	"context"
	"errors"
	"net/http"
	"strconv"

	"github.com/iliafrenkel/on-suite/internal/platform/render"
	"github.com/iliafrenkel/on-suite/internal/platform/web"
)

// userID is the signed-in user's id. Handlers registered with Handle are
// guarded, so a missing user is a programming error rather than a bad
// request.
func (a *App) userID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	u, ok := web.UserFrom(r.Context())
	if !ok {
		a.deps.Errors.Status(w, r, http.StatusUnauthorized)
		return 0, false
	}
	return u.ID, true
}

// deckIDFromPath parses the {deckID} wildcard.
func (a *App) deckIDFromPath(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("deckID"), 10, 64)
	if err != nil || id <= 0 {
		a.deps.Errors.Status(w, r, http.StatusNotFound)
		return 0, false
	}
	return id, true
}

// fail maps a store error onto a response. ErrNotFound becomes a 404 whether
// the row is missing or simply someone else's, so the two are
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

func (a *App) render(w http.ResponseWriter, r *http.Request, status int, name string, page render.Page) {
	if err := a.deps.Render.Page(w, status, name, page); err != nil {
		a.deps.Errors.Internal(w, r, err)
	}
}

// userMessage strips the error's package prefix so the wording reads as a
// sentence to the person who typed the form.
func userMessage(err error) string {
	msg := err.Error()
	for _, prefix := range []string{"flash: invalid input: ", "flash: "} {
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

// deckPaneMode values. There is no constant for "edit": editDeckDetail
// below sets Mode to the literal "edit" directly, matching deckPageTitle's
// and the template's own "edit" checks.
const (
	deckModeView = "view"
	deckModeNew  = "new"
)

// deckDetailView is what the deck detail pane renders, in any mode.
type deckDetailView struct {
	Mode      string
	Deck      Deck
	CSRFToken string

	NameValue        string
	DescriptionValue string
	Error            string
}

// deckListItem is one row on the deck list page.
type deckListItem struct {
	Deck Deck
}

type deckListFragment struct {
	Items    []deckListItem
	ActiveID int64
	OOB      bool
}

type deckIndexView struct {
	List   deckListFragment
	Detail deckDetailView
	Title  string
	Shell  render.Shell
}

func (a *App) viewDeckDetail(r *http.Request, d Deck) deckDetailView {
	return deckDetailView{Mode: deckModeView, Deck: d, CSRFToken: web.CSRFToken(r.Context())}
}

func (a *App) newDeckDetail(r *http.Request, errMsg, name, description string) deckDetailView {
	return deckDetailView{
		Mode: deckModeNew, NameValue: name, DescriptionValue: description,
		Error: errMsg, CSRFToken: web.CSRFToken(r.Context()),
	}
}

func (a *App) editDeckDetail(r *http.Request, d Deck, errMsg, name, description string) deckDetailView {
	return deckDetailView{
		Mode: "edit", Deck: d, NameValue: name, DescriptionValue: description,
		Error: errMsg, CSRFToken: web.CSRFToken(r.Context()),
	}
}

func (a *App) deckListItems(ctx context.Context, userID int64) ([]deckListItem, error) {
	decks, err := a.store.ListDecks(ctx, userID)
	if err != nil {
		return nil, err
	}
	items := make([]deckListItem, 0, len(decks))
	for _, d := range decks {
		items = append(items, deckListItem{Deck: d})
	}
	return items, nil
}

func deckPageTitle(d deckDetailView) string {
	switch d.Mode {
	case deckModeView:
		return d.Deck.Name
	case "edit":
		return "Edit " + d.Deck.Name
	case deckModeNew:
		return "New deck"
	default:
		return "Decks"
	}
}

// deckIndex renders the split-view page: the deck list on the left, and
// whichever deck {deckID} selects (or nothing) on the right. It backs both
// GET /{$} (PathValue("deckID") is "") and GET /{deckID}.
func (a *App) deckIndex(w http.ResponseWriter, r *http.Request) {
	userID, ok := a.userID(w, r)
	if !ok {
		return
	}

	var detail deckDetailView
	if r.PathValue("deckID") != "" {
		id, ok := a.deckIDFromPath(w, r)
		if !ok {
			return
		}
		d, err := a.store.DeckByID(r.Context(), userID, id)
		if err != nil {
			a.fail(w, r, err)
			return
		}
		detail = a.viewDeckDetail(r, d)
	}
	a.renderDeckIndex(w, r, userID, http.StatusOK, detail)
}

func (a *App) renderDeckIndex(w http.ResponseWriter, r *http.Request, userID int64, status int, detail deckDetailView) {
	items, err := a.deckListItems(r.Context(), userID)
	if err != nil {
		a.deps.Errors.Internal(w, r, err)
		return
	}
	if web.IsHTMX(r) && !web.IsHTMXHistoryRestore(r) {
		view := deckIndexView{List: deckListFragment{Items: items, ActiveID: detail.Deck.ID, OOB: true}, Detail: detail}
		page := a.deps.Page(r, deckPageTitle(detail))
		view.Title, view.Shell = page.Title, page.Shell
		if err := a.deps.Render.Fragment(w, http.StatusOK, "flash/decks", "deck-detail-with-list", view); err != nil {
			a.deps.Errors.Internal(w, r, err)
		}
		return
	}
	view := deckIndexView{List: deckListFragment{Items: items, ActiveID: detail.Deck.ID}, Detail: detail}
	page := a.deps.Page(r, deckPageTitle(detail))
	page.Data = view
	a.render(w, r, status, "flash/decks", page)
}

func (a *App) renderDeckDetailWithList(w http.ResponseWriter, r *http.Request, userID int64, status int, detail deckDetailView) {
	items, err := a.deckListItems(r.Context(), userID)
	if err != nil {
		a.deps.Errors.Internal(w, r, err)
		return
	}
	view := deckIndexView{List: deckListFragment{Items: items, ActiveID: detail.Deck.ID, OOB: true}, Detail: detail}
	page := a.deps.Page(r, deckPageTitle(detail))
	view.Title, view.Shell = page.Title, page.Shell
	if err := a.deps.Render.Fragment(w, status, "flash/decks", "deck-detail-with-list", view); err != nil {
		a.deps.Errors.Internal(w, r, err)
	}
}

func (a *App) newDeckForm(w http.ResponseWriter, r *http.Request) {
	userID, ok := a.userID(w, r)
	if !ok {
		return
	}
	a.renderDeckIndex(w, r, userID, http.StatusOK, a.newDeckDetail(r, "", "", ""))
}

func (a *App) createDeck(w http.ResponseWriter, r *http.Request) {
	userID, ok := a.userID(w, r)
	if !ok {
		return
	}
	name := r.PostFormValue("name")
	description := r.PostFormValue("description")

	if err := ValidateDeck(name, description); err != nil {
		a.renderDeckIndex(w, r, userID, http.StatusBadRequest, a.newDeckDetail(r, userMessage(err), name, description))
		return
	}
	d, err := a.store.CreateDeck(r.Context(), userID, name, description)
	if err != nil {
		if errors.Is(err, ErrInvalid) {
			a.renderDeckIndex(w, r, userID, http.StatusBadRequest, a.newDeckDetail(r, userMessage(err), name, description))
			return
		}
		a.deps.Errors.Internal(w, r, err)
		return
	}

	if !web.IsHTMX(r) {
		http.Redirect(w, r, "/flash/"+strconv.FormatInt(d.ID, 10), http.StatusSeeOther)
		return
	}
	w.Header().Set("HX-Push-Url", "/flash/"+strconv.FormatInt(d.ID, 10))
	a.renderDeckDetailWithList(w, r, userID, http.StatusCreated, a.viewDeckDetail(r, d))
}

func (a *App) editDeckForm(w http.ResponseWriter, r *http.Request) {
	userID, ok := a.userID(w, r)
	if !ok {
		return
	}
	id, ok := a.deckIDFromPath(w, r)
	if !ok {
		return
	}
	d, err := a.store.DeckByID(r.Context(), userID, id)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	a.renderDeckIndex(w, r, userID, http.StatusOK, a.editDeckDetail(r, d, "", d.Name, d.Description))
}

func (a *App) updateDeck(w http.ResponseWriter, r *http.Request) {
	userID, ok := a.userID(w, r)
	if !ok {
		return
	}
	id, ok := a.deckIDFromPath(w, r)
	if !ok {
		return
	}
	d, err := a.store.DeckByID(r.Context(), userID, id)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	name := r.PostFormValue("name")
	description := r.PostFormValue("description")

	if err := ValidateDeck(name, description); err != nil {
		a.renderDeckIndex(w, r, userID, http.StatusBadRequest, a.editDeckDetail(r, d, userMessage(err), name, description))
		return
	}
	updated, err := a.store.UpdateDeck(r.Context(), userID, id, name, description)
	if err != nil {
		if errors.Is(err, ErrInvalid) {
			a.renderDeckIndex(w, r, userID, http.StatusBadRequest, a.editDeckDetail(r, d, userMessage(err), name, description))
			return
		}
		a.fail(w, r, err)
		return
	}

	if !web.IsHTMX(r) {
		http.Redirect(w, r, "/flash/"+strconv.FormatInt(id, 10), http.StatusSeeOther)
		return
	}
	w.Header().Set("HX-Push-Url", "/flash/"+strconv.FormatInt(id, 10))
	a.renderDeckDetailWithList(w, r, userID, http.StatusOK, a.viewDeckDetail(r, updated))
}

func (a *App) deleteDeck(w http.ResponseWriter, r *http.Request) {
	userID, ok := a.userID(w, r)
	if !ok {
		return
	}
	id, ok := a.deckIDFromPath(w, r)
	if !ok {
		return
	}
	if err := a.store.DeleteDeck(r.Context(), userID, id); err != nil {
		a.fail(w, r, err)
		return
	}
	a.deps.Log.Info("deck deleted", "app", ID, "user_id", userID, "deck_id", id)

	if !web.IsHTMX(r) {
		http.Redirect(w, r, "/flash/", http.StatusSeeOther)
		return
	}
	w.Header().Set("HX-Push-Url", "/flash/")
	a.renderDeckDetailWithList(w, r, userID, http.StatusOK, deckDetailView{})
}
```

- [ ] **Step 5: Write `templates/decks.html`**

```html
{{define "deck-detail-body"}}
{{if eq .Mode "view"}}{{template "deck-detail-view" .}}
{{else if eq .Mode "edit"}}{{template "deck-detail-edit" .}}
{{else if eq .Mode "new"}}{{template "deck-detail-new" .}}
{{else}}{{template "deck-detail-empty" .}}
{{end}}
{{end}}

{{define "deck-detail-with-list"}}<title>{{if .Title}}{{.Title}} · {{end}}ON Suite</title><span id="shell-crumb-tail" hx-swap-oob="true">{{template "shell-crumb-tail" (dict "Title" .Title "Shell" .Shell)}}</span>{{template "deck-detail-body" .Detail}}{{template "deck-list-items" .List}}{{end}}

{{define "deck-detail-empty"}}
<p class="dim">Select a deck to view it.</p>
{{end}}

{{define "deck-detail-view"}}
<div class="stack" id="deck-detail-view">
	<div class="row">
		<h1>{{.Deck.Name}}</h1>
	</div>
	{{with .Deck.Description}}<p>{{.}}</p>{{end}}
	<div class="row">
		<a class="toolbar-btn" href="/flash/edit/{{.Deck.ID}}" hx-get="/flash/edit/{{.Deck.ID}}" hx-target="#deck-detail" hx-push-url="true">Edit</a>
		<a class="toolbar-btn" href="/flash/{{.Deck.ID}}/cards/" hx-get="/flash/{{.Deck.ID}}/cards/" hx-push-url="true">View cards</a>
		<form method="post" action="/flash/{{.Deck.ID}}/delete" hx-post="/flash/{{.Deck.ID}}/delete" hx-target="#deck-detail" hx-swap="innerHTML"
		      data-confirm="Delete “{{.Deck.Name}}”? This also deletes every card in it.">
			<input type="hidden" name="{{csrfField}}" value="{{.CSRFToken}}">
			<button type="submit" class="toolbar-btn danger">Delete</button>
		</form>
	</div>
</div>
{{end}}

{{define "deck-detail-new"}}
<form class="stack" id="deck-detail-new" method="post" action="/flash/new" hx-post="/flash/new" hx-target="#deck-detail" hx-swap="innerHTML">
	<input type="hidden" name="{{csrfField}}" value="{{.CSRFToken}}">
	<h1>New deck</h1>
	{{with .Error}}<div class="notice notice-error" role="alert">{{.}}</div>{{end}}
	<div class="field">
		<label for="deck-name">Name</label>
		<input id="deck-name" type="text" name="name" value="{{.NameValue}}" autofocus required>
	</div>
	<div class="field">
		<label for="deck-description">Description</label>
		<textarea id="deck-description" name="description" rows="3">{{.DescriptionValue}}</textarea>
	</div>
	<div class="row">
		<button type="submit" class="toolbar-btn toolbar-btn-active">Save</button>
		<a class="toolbar-btn" href="/flash/" hx-get="/flash/" hx-target="#deck-detail" hx-push-url="true">Cancel</a>
	</div>
</form>
{{end}}

{{define "deck-detail-edit"}}
<form class="stack" id="deck-detail-edit" method="post" action="/flash/{{.Deck.ID}}" hx-post="/flash/{{.Deck.ID}}" hx-target="#deck-detail" hx-swap="innerHTML">
	<input type="hidden" name="{{csrfField}}" value="{{.CSRFToken}}">
	<h1>Edit deck</h1>
	{{with .Error}}<div class="notice notice-error" role="alert">{{.}}</div>{{end}}
	<div class="field">
		<label for="deck-name-{{.Deck.ID}}">Name</label>
		<input id="deck-name-{{.Deck.ID}}" type="text" name="name" value="{{.NameValue}}" autofocus required>
	</div>
	<div class="field">
		<label for="deck-description-{{.Deck.ID}}">Description</label>
		<textarea id="deck-description-{{.Deck.ID}}" name="description" rows="3">{{.DescriptionValue}}</textarea>
	</div>
	<div class="row">
		<button type="submit" class="toolbar-btn toolbar-btn-active">Save</button>
		<a class="toolbar-btn" href="/flash/{{.Deck.ID}}" hx-get="/flash/{{.Deck.ID}}" hx-target="#deck-detail" hx-push-url="true">Cancel</a>
	</div>
</form>
{{end}}

{{define "deck-list-items"}}
<ul class="deck-list" id="deck-list"{{if .OOB}} hx-swap-oob="true"{{end}}>
	{{if .Items}}
	{{range .Items}}
	{{$d := .Deck}}
	<li>
		<a class="deck-row{{if eq $d.ID $.ActiveID}} deck-row-active{{end}}" href="/flash/{{$d.ID}}" hx-get="/flash/{{$d.ID}}" hx-target="#deck-detail" hx-push-url="true">
			{{$d.Name}}
		</a>
	</li>
	{{end}}
	{{else}}
	<li class="dim">No decks yet.</li>
	{{end}}
</ul>
{{end}}

{{define "content"}}
<div class="stack">
	<div class="row list-head">
		<h2>Decks</h2>
		<a class="toolbar-btn" href="/flash/new" hx-get="/flash/new" hx-target="#deck-detail" hx-push-url="true">New deck</a>
	</div>
	{{template "deck-list-items" .Data.List}}
	<div id="deck-detail">
		{{template "deck-detail-body" .Data.Detail}}
	</div>
</div>
{{end}}
```

- [ ] **Step 6: Register the app in `cmd/onsuite/main.go`**

```go
// cmd/onsuite/main.go — add the import and the registration line.
import (
	...
	"github.com/iliafrenkel/on-suite/internal/apps/flash"
	"github.com/iliafrenkel/on-suite/internal/apps/notes"
	"github.com/iliafrenkel/on-suite/internal/apps/paste"
	"github.com/iliafrenkel/on-suite/internal/apps/reader"
	"github.com/iliafrenkel/on-suite/internal/platform/app"
)
```

```go
func registeredApps() []app.App {
	return []app.App{
		flash.New(),
		notes.New(),
		paste.New(),
		reader.New(),
	}
}
```

- [ ] **Step 7: Run the tests to verify they pass**

Run: `go test ./internal/apps/flash/... -v && go build ./cmd/onsuite`
Expected: PASS, and the binary builds.

- [ ] **Step 8: Run the architecture test**

Run: `go test ./internal/arch/...`
Expected: PASS — confirms `internal/apps/flash` does not cross the app-boundary rules.

- [ ] **Step 9: Commit**

```bash
git add internal/apps/flash/flash.go internal/apps/flash/handlers_decks.go internal/apps/flash/templates/decks.html internal/apps/flash/handlers_decks_test.go cmd/onsuite/main.go
git commit -m "feat(flash): register the app and add deck CRUD handlers"
```

---

### Task 3: Card domain & store

**Files:**
- Create: `internal/apps/flash/card.go`
- Create: `internal/apps/flash/migrations/0002_cards.sql`
- Test: `internal/apps/flash/card_test.go`

**Interfaces:**
- Consumes: `flash.Deck`, `Store.CreateDeck`/`DeckByID` from Tasks 1–2 (a card test creates its parent deck first).
- Produces: `flash.CardTypeBasic = "basic"`, `flash.CardTypeCloze = "cloze"`; `flash.Card{ID, DeckID, UserID, CardType, Front, Back, Notes int64/string, CreatedAt time.Time}`; `flash.ValidateCard(cardType, front, back string) error`; `(*Store).CreateCard(ctx, userID, deckID int64, cardType, front, back, notes string) (Card, error)`; `(*Store).UpdateCard(ctx, userID, deckID, id int64, cardType, front, back, notes string) (Card, error)`; `(*Store).CardByID(ctx, userID, deckID, id int64) (Card, error)`; `(*Store).ListCards(ctx, userID, deckID int64) ([]Card, error)`; `(*Store).DeleteCard(ctx, userID, deckID, id int64) error`. Task 4's handlers and Task 5's tag wiring consume these exact names.

- [ ] **Step 1: Write the failing store test**

```go
// internal/apps/flash/card_test.go
package flash_test

import (
	"context"
	"errors"
	"testing"

	"github.com/iliafrenkel/on-suite/internal/apps/flash"
)

func TestCreateAndFetchCard(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	deck, err := f.store.CreateDeck(ctx, f.alice.ID, "Spanish", "")
	if err != nil {
		t.Fatal(err)
	}

	created, err := f.store.CreateCard(ctx, f.alice.ID, deck.ID, flash.CardTypeBasic, "hola", "hello", "greeting")
	if err != nil {
		t.Fatalf("CreateCard: %v", err)
	}
	if created.ID == 0 {
		t.Error("CreateCard returned id 0")
	}

	got, err := f.store.CardByID(ctx, f.alice.ID, deck.ID, created.ID)
	if err != nil {
		t.Fatalf("CardByID: %v", err)
	}
	if got.Front != "hola" || got.Back != "hello" || got.Notes != "greeting" {
		t.Errorf("round trip lost data: %+v", got)
	}
}

func TestCreateCardRejectsUnknownDeck(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	if _, err := f.store.CreateCard(ctx, f.alice.ID, 999999, flash.CardTypeBasic, "a", "b", ""); !errors.Is(err, flash.ErrNotFound) {
		t.Errorf("CreateCard into a missing deck = %v, want ErrNotFound", err)
	}
}

func TestCreateCardRejectsSomeoneElsesDeck(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	deck, err := f.store.CreateDeck(ctx, f.alice.ID, "alice's", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.CreateCard(ctx, f.bob.ID, deck.ID, flash.CardTypeBasic, "a", "b", ""); !errors.Is(err, flash.ErrNotFound) {
		t.Errorf("CreateCard into another user's deck = %v, want ErrNotFound", err)
	}
}

func TestValidateCard(t *testing.T) {
	tests := []struct {
		name     string
		cardType string
		front    string
		back     string
		wantErr  bool
	}{
		{"ordinary basic", flash.CardTypeBasic, "q", "a", false},
		{"basic missing back", flash.CardTypeBasic, "q", "", true},
		{"basic missing front", flash.CardTypeBasic, "", "a", true},
		{"unknown type", "essay", "q", "a", true},
		{"cloze needs markers", flash.CardTypeCloze, "no markers here", "", true},
		{"ordinary cloze", flash.CardTypeCloze, "The capital of France is {{c1::Paris}}.", "", false},
		{"cloze must not carry a back", flash.CardTypeCloze, "{{c1::x}}", "unexpected", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := flash.ValidateCard(tt.cardType, tt.front, tt.back)
			if tt.wantErr && !errors.Is(err, flash.ErrInvalid) {
				t.Errorf("ValidateCard = %v, want ErrInvalid", err)
			}
			if !tt.wantErr && err != nil {
				t.Errorf("ValidateCard rejected a valid card: %v", err)
			}
		})
	}
}

func TestListCardsIsScopedToItsDeck(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	deckA, err := f.store.CreateDeck(ctx, f.alice.ID, "A", "")
	if err != nil {
		t.Fatal(err)
	}
	deckB, err := f.store.CreateDeck(ctx, f.alice.ID, "B", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.CreateCard(ctx, f.alice.ID, deckA.ID, flash.CardTypeBasic, "a1", "x", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.CreateCard(ctx, f.alice.ID, deckB.ID, flash.CardTypeBasic, "b1", "x", ""); err != nil {
		t.Fatal(err)
	}

	got, err := f.store.ListCards(ctx, f.alice.ID, deckA.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Front != "a1" {
		t.Errorf("ListCards(deckA) = %+v, want exactly a1", got)
	}
}

// TestCardOwnerScoping mirrors TestDeckOwnerScoping: bob must not reach
// alice's card even by guessing the right deck id.
func TestCardOwnerScoping(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	deck, err := f.store.CreateDeck(ctx, f.alice.ID, "alice's", "")
	if err != nil {
		t.Fatal(err)
	}
	card, err := f.store.CreateCard(ctx, f.alice.ID, deck.ID, flash.CardTypeBasic, "secret", "x", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.CardByID(ctx, f.bob.ID, deck.ID, card.ID); !errors.Is(err, flash.ErrNotFound) {
		t.Errorf("CardByID as another user = %v, want ErrNotFound", err)
	}
	if err := f.store.DeleteCard(ctx, f.bob.ID, deck.ID, card.ID); !errors.Is(err, flash.ErrNotFound) {
		t.Errorf("DeleteCard as another user = %v, want ErrNotFound", err)
	}
}

func TestDeletingADeckRemovesItsCards(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	deck, err := f.store.CreateDeck(ctx, f.alice.ID, "doomed", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.CreateCard(ctx, f.alice.ID, deck.ID, flash.CardTypeBasic, "a", "b", ""); err != nil {
		t.Fatal(err)
	}
	if err := f.store.DeleteDeck(ctx, f.alice.ID, deck.ID); err != nil {
		t.Fatal(err)
	}
	got, err := f.store.ListCards(ctx, f.alice.ID, deck.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("%d cards survived their deck", len(got))
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/apps/flash/... -run TestCreateAndFetchCard -v`
Expected: FAIL — `flash.Card`, `flash.CardTypeBasic`, `Store.CreateCard` etc. do not exist yet.

- [ ] **Step 3: Write the migration**

```sql
-- internal/apps/flash/migrations/0002_cards.sql
CREATE TABLE flash_cards (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    deck_id    INTEGER NOT NULL REFERENCES flash_decks (id) ON DELETE CASCADE,
    user_id    INTEGER NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    card_type  TEXT    NOT NULL,
    front      TEXT    NOT NULL,
    back       TEXT    NOT NULL DEFAULT '',
    notes      TEXT    NOT NULL DEFAULT '',
    created_at TEXT    NOT NULL
) STRICT;

CREATE INDEX flash_cards_deck_created_idx
    ON flash_cards (deck_id, created_at DESC);
```

- [ ] **Step 4: Write `card.go`**

```go
// internal/apps/flash/card.go
package flash

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	// CardTypeBasic is a plain front/back card.
	CardTypeBasic = "basic"
	// CardTypeCloze is a fill-in-the-blank card: Front carries the
	// {{c1::...}}-style markers, Back is unused (F1 stores and validates
	// this shape; parsing the markers for review is F2's concern).
	CardTypeCloze = "cloze"

	// MaxCardFieldBytes bounds Front, Back, and Notes independently.
	MaxCardFieldBytes = 16 << 10
)

// Card is one flash card belonging to a deck.
type Card struct {
	ID        int64
	DeckID    int64
	UserID    int64
	CardType  string
	Front     string
	Back      string
	Notes     string
	CreatedAt time.Time
}

func isKnownCardType(t string) bool {
	return t == CardTypeBasic || t == CardTypeCloze
}

// ValidateCard checks a card's user-supplied fields against the rules for
// its type. Exported because the handler reports these messages back to the
// user.
func ValidateCard(cardType, front, back string) error {
	if !isKnownCardType(cardType) {
		return fmt.Errorf("%w: %q is not a card type I know", ErrInvalid, cardType)
	}
	if strings.TrimSpace(front) == "" {
		return fmt.Errorf("%w: the card needs a front", ErrInvalid)
	}
	if !utf8.ValidString(front) || len(front) > MaxCardFieldBytes {
		return fmt.Errorf("%w: the front is invalid or too long", ErrInvalid)
	}
	if !utf8.ValidString(back) || len(back) > MaxCardFieldBytes {
		return fmt.Errorf("%w: the back is invalid or too long", ErrInvalid)
	}

	switch cardType {
	case CardTypeBasic:
		if strings.TrimSpace(back) == "" {
			return fmt.Errorf("%w: a basic card needs a back", ErrInvalid)
		}
	case CardTypeCloze:
		if !strings.Contains(front, "{{") || !strings.Contains(front, "}}") {
			return fmt.Errorf("%w: a cloze card's front needs at least one {{...}} deletion", ErrInvalid)
		}
		if strings.TrimSpace(back) != "" {
			return fmt.Errorf("%w: a cloze card's answer comes from its front; leave the back empty", ErrInvalid)
		}
	}
	return nil
}

func validateCardNotes(notes string) error {
	if !utf8.ValidString(notes) || len(notes) > MaxCardFieldBytes {
		return fmt.Errorf("%w: the notes are invalid or too long", ErrInvalid)
	}
	return nil
}

// CreateCard stores a new card in one of userID's own decks.
func (st *Store) CreateCard(ctx context.Context, userID, deckID int64, cardType, front, back, notes string) (Card, error) {
	if err := ValidateCard(cardType, front, back); err != nil {
		return Card{}, err
	}
	if err := validateCardNotes(notes); err != nil {
		return Card{}, err
	}
	if _, err := st.DeckByID(ctx, userID, deckID); err != nil {
		return Card{}, err
	}

	c := Card{DeckID: deckID, UserID: userID, CardType: cardType, Front: front, Back: back, Notes: notes, CreatedAt: st.now()}
	err := st.db.QueryRowContext(ctx,
		`INSERT INTO flash_cards (deck_id, user_id, card_type, front, back, notes, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)
		 RETURNING id`,
		c.DeckID, c.UserID, c.CardType, c.Front, c.Back, c.Notes, formatTime(c.CreatedAt),
	).Scan(&c.ID)
	if err != nil {
		return Card{}, fmt.Errorf("flash: create card: %w", err)
	}
	return c, nil
}

// UpdateCard overwrites userID's own card's editable fields.
func (st *Store) UpdateCard(ctx context.Context, userID, deckID, id int64, cardType, front, back, notes string) (Card, error) {
	if err := ValidateCard(cardType, front, back); err != nil {
		return Card{}, err
	}
	if err := validateCardNotes(notes); err != nil {
		return Card{}, err
	}

	res, err := st.db.ExecContext(ctx,
		`UPDATE flash_cards SET card_type = ?, front = ?, back = ?, notes = ?
		 WHERE id = ? AND deck_id = ? AND user_id = ?`,
		cardType, front, back, notes, id, deckID, userID)
	if err != nil {
		return Card{}, fmt.Errorf("flash: update card: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return Card{}, fmt.Errorf("flash: update card: %w", err)
	}
	if n == 0 {
		return Card{}, ErrNotFound
	}
	return st.CardByID(ctx, userID, deckID, id)
}

// CardByID fetches one of userID's own cards, scoped to its deck.
func (st *Store) CardByID(ctx context.Context, userID, deckID, id int64) (Card, error) {
	return scanCard(st.db.QueryRowContext(ctx,
		`SELECT id, deck_id, user_id, card_type, front, back, notes, created_at
		 FROM flash_cards WHERE id = ? AND deck_id = ? AND user_id = ?`, id, deckID, userID))
}

// ListCards returns userID's cards in one deck, newest first.
func (st *Store) ListCards(ctx context.Context, userID, deckID int64) ([]Card, error) {
	rows, err := st.db.QueryContext(ctx,
		`SELECT id, deck_id, user_id, card_type, front, back, notes, created_at
		 FROM flash_cards WHERE deck_id = ? AND user_id = ?
		 ORDER BY created_at DESC, id DESC`, deckID, userID)
	if err != nil {
		return nil, fmt.Errorf("flash: list cards: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []Card
	for rows.Next() {
		c, err := scanCardRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("flash: list cards: %w", err)
	}
	return out, nil
}

// DeleteCard removes one of userID's own cards.
func (st *Store) DeleteCard(ctx context.Context, userID, deckID, id int64) error {
	res, err := st.db.ExecContext(ctx,
		`DELETE FROM flash_cards WHERE id = ? AND deck_id = ? AND user_id = ?`, id, deckID, userID)
	if err != nil {
		return fmt.Errorf("flash: delete card: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("flash: delete card: %w", err)
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func scanCard(row *sql.Row) (Card, error) {
	c, err := scanCardRow(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Card{}, ErrNotFound
	}
	return c, err
}

func scanCardRow(row rowScanner) (Card, error) {
	var (
		c         Card
		createdAt string
	)
	err := row.Scan(&c.ID, &c.DeckID, &c.UserID, &c.CardType, &c.Front, &c.Back, &c.Notes, &createdAt)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return Card{}, sql.ErrNoRows // translated by scanCard
	case err != nil:
		return Card{}, fmt.Errorf("flash: scan card: %w", err)
	}
	if c.CreatedAt, err = parseTime(createdAt); err != nil {
		return Card{}, err
	}
	return c, nil
}
```

`CreateCard` deliberately calls `st.DeckByID(ctx, userID, deckID)` first: that single owner-scoped lookup is what turns "deck missing" and "someone else's deck" into the same `ErrNotFound`, satisfying `TestCreateCardRejectsUnknownDeck` and `TestCreateCardRejectsSomeoneElsesDeck` together — this is the "owner-matching re-checked at every descent step" pattern from `internal/apps/notes/store.go`'s `Outline`, applied one level deep.

- [ ] **Step 5: Run the test to verify it passes**

Run: `go test ./internal/apps/flash/... -v`
Expected: PASS (all deck and card tests).

- [ ] **Step 6: Commit**

```bash
git add internal/apps/flash/card.go internal/apps/flash/migrations/0002_cards.sql internal/apps/flash/card_test.go
git commit -m "feat(flash): add card domain and store, scoped through its deck"
```

---

### Task 4: Card handlers and templates

**Files:**
- Create: `internal/apps/flash/handlers_cards.go`
- Create: `internal/apps/flash/templates/cards.html`
- Modify: `internal/apps/flash/flash.go` (add card routes to `Mount`, add card template registration if `AddApp` needs the file listed — it does not: `Templates()` embeds the whole `templates/*.html` directory, so a new file is picked up automatically)
- Test: `internal/apps/flash/handlers_cards_test.go`

**Interfaces:**
- Consumes: `a.userID`, `a.fail`, `a.render`, `userMessage` from Task 2; `Store` card methods from Task 3.
- Produces: routes `GET /flash/{deckID}/cards/{$}`, `GET /flash/{deckID}/cards/new`, `POST /flash/{deckID}/cards/new`, `GET /flash/{deckID}/cards/{cardID}`, `GET /flash/{deckID}/cards/edit/{cardID}`, `POST /flash/{deckID}/cards/{cardID}`, `POST /flash/{deckID}/cards/{cardID}/delete`. Task 5 extends the card form/view with a `Tags` field on top of this.

- [ ] **Step 1: Write the failing handler test**

```go
// internal/apps/flash/handlers_cards_test.go
package flash_test

import (
	"net/url"
	"testing"

	"github.com/iliafrenkel/on-suite/internal/apps/flash"
)

func TestCreateAndViewCard(t *testing.T) {
	s := newServer(t)
	deck, err := s.Store.CreateDeck(t.Context(), s.Alice.User.ID, "Spanish", "")
	if err != nil {
		t.Fatal(err)
	}

	s.Submit(t, s.Alice,
		"/flash/"+itoa(deck.ID)+"/cards/new",
		url.Values{"card_type": {flash.CardTypeBasic}, "front": {"hola"}, "back": {"hello"}},
		"")

	doc := s.Get(t, s.Alice, "/flash/"+itoa(deck.ID)+"/cards/")
	doc.MustHave(".card-list")
}

func TestCreateCardValidation(t *testing.T) {
	s := newServer(t)
	deck, err := s.Store.CreateDeck(t.Context(), s.Alice.User.ID, "Spanish", "")
	if err != nil {
		t.Fatal(err)
	}
	rec := s.Post(t, s.Alice, "/flash/"+itoa(deck.ID)+"/cards/new",
		url.Values{"card_type": {flash.CardTypeBasic}, "front": {"hola"}})
	if rec.Code != 400 {
		t.Errorf("creating a basic card with no back = %d, want 400", rec.Code)
	}
}

func TestCreatingACardInSomeoneElsesDeckIs404(t *testing.T) {
	s := newServer(t)
	deck, err := s.Store.CreateDeck(t.Context(), s.Alice.User.ID, "alice's", "")
	if err != nil {
		t.Fatal(err)
	}
	rec := s.PostHX(t, s.Bob, "/flash/"+itoa(deck.ID)+"/cards/new",
		url.Values{"card_type": {flash.CardTypeBasic}, "front": {"a"}, "back": {"b"}})
	if rec.Code != 404 {
		t.Errorf("creating a card in someone else's deck = %d, want 404", rec.Code)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/apps/flash/... -run TestCreateAndViewCard -v`
Expected: FAIL — the card routes and `handlers_cards.go` do not exist yet.

- [ ] **Step 3: Write `handlers_cards.go`**

```go
// internal/apps/flash/handlers_cards.go
package flash

import (
	"context"
	"errors"
	"net/http"
	"strconv"

	"github.com/iliafrenkel/on-suite/internal/platform/render"
	"github.com/iliafrenkel/on-suite/internal/platform/web"
)

func (a *App) cardIDFromPath(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("cardID"), 10, 64)
	if err != nil || id <= 0 {
		a.deps.Errors.Status(w, r, http.StatusNotFound)
		return 0, false
	}
	return id, true
}

// cardDeck loads and owner-checks the deck a card route is nested under.
// Every card handler calls this first, so a request for a deck that is
// missing or belongs to someone else 404s before any card work happens.
func (a *App) cardDeck(w http.ResponseWriter, r *http.Request, userID int64) (Deck, bool) {
	deckID, ok := a.deckIDFromPath(w, r)
	if !ok {
		return Deck{}, false
	}
	d, err := a.store.DeckByID(r.Context(), userID, deckID)
	if err != nil {
		a.fail(w, r, err)
		return Deck{}, false
	}
	return d, true
}

const (
	cardModeView = "view"
	cardModeNew  = "new"
	cardModeEdit = "edit"
)

type cardDetailView struct {
	Mode      string
	Deck      Deck
	Card      Card
	CSRFToken string

	CardTypeValue string
	FrontValue    string
	BackValue     string
	NotesValue    string
	Error         string
}

type cardListItem struct {
	Card Card
}

type cardListFragment struct {
	Items    []cardListItem
	ActiveID int64
	OOB      bool
}

type cardIndexView struct {
	Deck   Deck
	List   cardListFragment
	Detail cardDetailView
	Title  string
	Shell  render.Shell
}

func (a *App) viewCardDetail(r *http.Request, d Deck, c Card) cardDetailView {
	return cardDetailView{Mode: cardModeView, Deck: d, Card: c, CSRFToken: web.CSRFToken(r.Context())}
}

func (a *App) newCardDetail(r *http.Request, d Deck, errMsg, cardType, front, back, notes string) cardDetailView {
	return cardDetailView{
		Mode: cardModeNew, Deck: d, CardTypeValue: cardType, FrontValue: front, BackValue: back, NotesValue: notes,
		Error: errMsg, CSRFToken: web.CSRFToken(r.Context()),
	}
}

func (a *App) editCardDetail(r *http.Request, d Deck, c Card, errMsg, cardType, front, back, notes string) cardDetailView {
	return cardDetailView{
		Mode: cardModeEdit, Deck: d, Card: c, CardTypeValue: cardType, FrontValue: front, BackValue: back, NotesValue: notes,
		Error: errMsg, CSRFToken: web.CSRFToken(r.Context()),
	}
}

func (a *App) cardListItems(ctx context.Context, userID, deckID int64) ([]cardListItem, error) {
	cards, err := a.store.ListCards(ctx, userID, deckID)
	if err != nil {
		return nil, err
	}
	items := make([]cardListItem, 0, len(cards))
	for _, c := range cards {
		items = append(items, cardListItem{Card: c})
	}
	return items, nil
}

func cardPageTitle(d Deck, detail cardDetailView) string {
	switch detail.Mode {
	case cardModeEdit:
		return "Edit card · " + d.Name
	case cardModeNew:
		return "New card · " + d.Name
	default:
		return "Cards · " + d.Name
	}
}

func (a *App) cardIndex(w http.ResponseWriter, r *http.Request) {
	userID, ok := a.userID(w, r)
	if !ok {
		return
	}
	deck, ok := a.cardDeck(w, r, userID)
	if !ok {
		return
	}

	var detail cardDetailView
	if r.PathValue("cardID") != "" {
		id, ok := a.cardIDFromPath(w, r)
		if !ok {
			return
		}
		c, err := a.store.CardByID(r.Context(), userID, deck.ID, id)
		if err != nil {
			a.fail(w, r, err)
			return
		}
		detail = a.viewCardDetail(r, deck, c)
	}
	a.renderCardIndex(w, r, userID, deck, http.StatusOK, detail)
}

func (a *App) renderCardIndex(w http.ResponseWriter, r *http.Request, userID int64, deck Deck, status int, detail cardDetailView) {
	items, err := a.cardListItems(r.Context(), userID, deck.ID)
	if err != nil {
		a.deps.Errors.Internal(w, r, err)
		return
	}
	if web.IsHTMX(r) && !web.IsHTMXHistoryRestore(r) {
		view := cardIndexView{Deck: deck, List: cardListFragment{Items: items, ActiveID: detail.Card.ID, OOB: true}, Detail: detail}
		page := a.deps.Page(r, cardPageTitle(deck, detail))
		view.Title, view.Shell = page.Title, page.Shell
		if err := a.deps.Render.Fragment(w, http.StatusOK, "flash/cards", "card-detail-with-list", view); err != nil {
			a.deps.Errors.Internal(w, r, err)
		}
		return
	}
	view := cardIndexView{Deck: deck, List: cardListFragment{Items: items, ActiveID: detail.Card.ID}, Detail: detail}
	page := a.deps.Page(r, cardPageTitle(deck, detail))
	page.Data = view
	a.render(w, r, status, "flash/cards", page)
}

func (a *App) renderCardDetailWithList(w http.ResponseWriter, r *http.Request, userID int64, deck Deck, status int, detail cardDetailView) {
	items, err := a.cardListItems(r.Context(), userID, deck.ID)
	if err != nil {
		a.deps.Errors.Internal(w, r, err)
		return
	}
	view := cardIndexView{Deck: deck, List: cardListFragment{Items: items, ActiveID: detail.Card.ID, OOB: true}, Detail: detail}
	page := a.deps.Page(r, cardPageTitle(deck, detail))
	view.Title, view.Shell = page.Title, page.Shell
	if err := a.deps.Render.Fragment(w, status, "flash/cards", "card-detail-with-list", view); err != nil {
		a.deps.Errors.Internal(w, r, err)
	}
}

func cardBasePath(deckID int64) string {
	return "/flash/" + strconv.FormatInt(deckID, 10) + "/cards/"
}

func (a *App) newCardForm(w http.ResponseWriter, r *http.Request) {
	userID, ok := a.userID(w, r)
	if !ok {
		return
	}
	deck, ok := a.cardDeck(w, r, userID)
	if !ok {
		return
	}
	a.renderCardIndex(w, r, userID, deck, http.StatusOK, a.newCardDetail(r, deck, "", CardTypeBasic, "", "", ""))
}

func (a *App) createCard(w http.ResponseWriter, r *http.Request) {
	userID, ok := a.userID(w, r)
	if !ok {
		return
	}
	deck, ok := a.cardDeck(w, r, userID)
	if !ok {
		return
	}
	cardType := r.PostFormValue("card_type")
	front := r.PostFormValue("front")
	back := r.PostFormValue("back")
	notes := r.PostFormValue("notes")

	if err := ValidateCard(cardType, front, back); err != nil {
		a.renderCardIndex(w, r, userID, deck, http.StatusBadRequest, a.newCardDetail(r, deck, userMessage(err), cardType, front, back, notes))
		return
	}
	c, err := a.store.CreateCard(r.Context(), userID, deck.ID, cardType, front, back, notes)
	if err != nil {
		if errors.Is(err, ErrInvalid) {
			a.renderCardIndex(w, r, userID, deck, http.StatusBadRequest, a.newCardDetail(r, deck, userMessage(err), cardType, front, back, notes))
			return
		}
		a.fail(w, r, err)
		return
	}

	if !web.IsHTMX(r) {
		http.Redirect(w, r, cardBasePath(deck.ID)+strconv.FormatInt(c.ID, 10), http.StatusSeeOther)
		return
	}
	w.Header().Set("HX-Push-Url", cardBasePath(deck.ID)+strconv.FormatInt(c.ID, 10))
	a.renderCardDetailWithList(w, r, userID, deck, http.StatusCreated, a.viewCardDetail(r, deck, c))
}

func (a *App) editCardForm(w http.ResponseWriter, r *http.Request) {
	userID, ok := a.userID(w, r)
	if !ok {
		return
	}
	deck, ok := a.cardDeck(w, r, userID)
	if !ok {
		return
	}
	id, ok := a.cardIDFromPath(w, r)
	if !ok {
		return
	}
	c, err := a.store.CardByID(r.Context(), userID, deck.ID, id)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	a.renderCardIndex(w, r, userID, deck, http.StatusOK, a.editCardDetail(r, deck, c, "", c.CardType, c.Front, c.Back, c.Notes))
}

func (a *App) updateCard(w http.ResponseWriter, r *http.Request) {
	userID, ok := a.userID(w, r)
	if !ok {
		return
	}
	deck, ok := a.cardDeck(w, r, userID)
	if !ok {
		return
	}
	id, ok := a.cardIDFromPath(w, r)
	if !ok {
		return
	}
	c, err := a.store.CardByID(r.Context(), userID, deck.ID, id)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	cardType := r.PostFormValue("card_type")
	front := r.PostFormValue("front")
	back := r.PostFormValue("back")
	notes := r.PostFormValue("notes")

	if err := ValidateCard(cardType, front, back); err != nil {
		a.renderCardIndex(w, r, userID, deck, http.StatusBadRequest, a.editCardDetail(r, deck, c, userMessage(err), cardType, front, back, notes))
		return
	}
	updated, err := a.store.UpdateCard(r.Context(), userID, deck.ID, id, cardType, front, back, notes)
	if err != nil {
		if errors.Is(err, ErrInvalid) {
			a.renderCardIndex(w, r, userID, deck, http.StatusBadRequest, a.editCardDetail(r, deck, c, userMessage(err), cardType, front, back, notes))
			return
		}
		a.fail(w, r, err)
		return
	}

	if !web.IsHTMX(r) {
		http.Redirect(w, r, cardBasePath(deck.ID)+strconv.FormatInt(id, 10), http.StatusSeeOther)
		return
	}
	w.Header().Set("HX-Push-Url", cardBasePath(deck.ID)+strconv.FormatInt(id, 10))
	a.renderCardDetailWithList(w, r, userID, deck, http.StatusOK, a.viewCardDetail(r, deck, updated))
}

func (a *App) deleteCard(w http.ResponseWriter, r *http.Request) {
	userID, ok := a.userID(w, r)
	if !ok {
		return
	}
	deck, ok := a.cardDeck(w, r, userID)
	if !ok {
		return
	}
	id, ok := a.cardIDFromPath(w, r)
	if !ok {
		return
	}
	if err := a.store.DeleteCard(r.Context(), userID, deck.ID, id); err != nil {
		a.fail(w, r, err)
		return
	}
	a.deps.Log.Info("card deleted", "app", ID, "user_id", userID, "deck_id", deck.ID, "card_id", id)

	if !web.IsHTMX(r) {
		http.Redirect(w, r, cardBasePath(deck.ID), http.StatusSeeOther)
		return
	}
	w.Header().Set("HX-Push-Url", cardBasePath(deck.ID))
	a.renderCardDetailWithList(w, r, userID, deck, http.StatusOK, cardDetailView{Deck: deck})
}
```

- [ ] **Step 4: Add the card routes to `Mount` in `flash.go`**

```go
// Append inside Mount, after the existing deck routes. Same non-ambiguity
// reasoning as the deck routes: "new" and "edit/{cardID}" are literal at the
// position where a same-shape pattern below has a wildcard, so they resolve
// deterministically; "cards/{cardID}" and "cards/{cardID}/delete" differ in
// segment count, and "edit/{cardID}" (GET) vs "{cardID}/delete" (POST)
// differ in method, so neither pair can collide the way paste.go's own
// comment warns about.
r.HandleFunc("GET /{deckID}/cards/{$}", a.cardIndex)
r.HandleFunc("GET /{deckID}/cards/new", a.newCardForm)
r.HandleFunc("POST /{deckID}/cards/new", a.createCard)
r.HandleFunc("GET /{deckID}/cards/{cardID}", a.cardIndex)
r.HandleFunc("GET /{deckID}/cards/edit/{cardID}", a.editCardForm)
r.HandleFunc("POST /{deckID}/cards/{cardID}", a.updateCard)
r.HandleFunc("POST /{deckID}/cards/{cardID}/delete", a.deleteCard)
```

- [ ] **Step 5: Write `templates/cards.html`**

```html
{{define "card-detail-body"}}
{{if eq .Mode "view"}}{{template "card-detail-view" .}}
{{else if eq .Mode "edit"}}{{template "card-detail-edit" .}}
{{else if eq .Mode "new"}}{{template "card-detail-new" .}}
{{else}}{{template "card-detail-empty" .}}
{{end}}
{{end}}

{{define "card-detail-with-list"}}<title>{{if .Title}}{{.Title}} · {{end}}ON Suite</title><span id="shell-crumb-tail" hx-swap-oob="true">{{template "shell-crumb-tail" (dict "Title" .Title "Shell" .Shell)}}</span>{{template "card-detail-body" .Detail}}{{template "card-list-items" .List}}{{end}}

{{define "card-detail-empty"}}
<p class="dim">Select a card to view it.</p>
{{end}}

{{define "card-detail-view"}}
<div class="stack" id="card-detail-view">
	<p class="faint">{{.Card.CardType}}</p>
	<h1>{{.Card.Front}}</h1>
	{{if eq .Card.CardType "basic"}}<p>{{.Card.Back}}</p>{{end}}
	{{with .Card.Notes}}<p class="dim">{{.}}</p>{{end}}
	<div class="row">
		<a class="toolbar-btn" href="{{$.Deck.ID}}/cards/edit/{{.Card.ID}}" hx-get="/flash/{{$.Deck.ID}}/cards/edit/{{.Card.ID}}" hx-target="#card-detail" hx-push-url="true">Edit</a>
		<form method="post" action="/flash/{{$.Deck.ID}}/cards/{{.Card.ID}}/delete" hx-post="/flash/{{$.Deck.ID}}/cards/{{.Card.ID}}/delete" hx-target="#card-detail" hx-swap="innerHTML"
		      data-confirm="Delete this card? This cannot be undone.">
			<input type="hidden" name="{{csrfField}}" value="{{.CSRFToken}}">
			<button type="submit" class="toolbar-btn danger">Delete</button>
		</form>
	</div>
</div>
{{end}}

{{define "card-form-fields"}}
<div class="field">
	<label for="card-type-{{.IDSuffix}}">Type</label>
	<select id="card-type-{{.IDSuffix}}" name="card_type">
		<option value="basic"{{if eq .CardTypeValue "basic"}} selected{{end}}>Basic</option>
		<option value="cloze"{{if eq .CardTypeValue "cloze"}} selected{{end}}>Cloze</option>
	</select>
</div>
<div class="field">
	<label for="card-front-{{.IDSuffix}}">Front</label>
	<textarea id="card-front-{{.IDSuffix}}" name="front" rows="3" required>{{.FrontValue}}</textarea>
</div>
<div class="field">
	<label for="card-back-{{.IDSuffix}}">Back (leave empty for a cloze card)</label>
	<textarea id="card-back-{{.IDSuffix}}" name="back" rows="3">{{.BackValue}}</textarea>
</div>
<div class="field">
	<label for="card-notes-{{.IDSuffix}}">Notes (shown after the answer)</label>
	<textarea id="card-notes-{{.IDSuffix}}" name="notes" rows="3">{{.NotesValue}}</textarea>
</div>
{{end}}

{{define "card-detail-new"}}
<form class="stack" id="card-detail-new" method="post" action="/flash/{{.Deck.ID}}/cards/new" hx-post="/flash/{{.Deck.ID}}/cards/new" hx-target="#card-detail" hx-swap="innerHTML">
	<input type="hidden" name="{{csrfField}}" value="{{.CSRFToken}}">
	<h1>New card</h1>
	{{with .Error}}<div class="notice notice-error" role="alert">{{.}}</div>{{end}}
	{{template "card-form-fields" (dict "IDSuffix" "new" "CardTypeValue" .CardTypeValue "FrontValue" .FrontValue "BackValue" .BackValue "NotesValue" .NotesValue)}}
	<div class="row">
		<button type="submit" class="toolbar-btn toolbar-btn-active">Save</button>
		<a class="toolbar-btn" href="/flash/{{.Deck.ID}}/cards/" hx-get="/flash/{{.Deck.ID}}/cards/" hx-target="#card-detail" hx-push-url="true">Cancel</a>
	</div>
</form>
{{end}}

{{define "card-detail-edit"}}
<form class="stack" id="card-detail-edit" method="post" action="/flash/{{.Deck.ID}}/cards/{{.Card.ID}}" hx-post="/flash/{{.Deck.ID}}/cards/{{.Card.ID}}" hx-target="#card-detail" hx-swap="innerHTML">
	<input type="hidden" name="{{csrfField}}" value="{{.CSRFToken}}">
	<h1>Edit card</h1>
	{{with .Error}}<div class="notice notice-error" role="alert">{{.}}</div>{{end}}
	{{template "card-form-fields" (dict "IDSuffix" .Card.ID "CardTypeValue" .CardTypeValue "FrontValue" .FrontValue "BackValue" .BackValue "NotesValue" .NotesValue)}}
	<div class="row">
		<button type="submit" class="toolbar-btn toolbar-btn-active">Save</button>
		<a class="toolbar-btn" href="/flash/{{.Deck.ID}}/cards/{{.Card.ID}}" hx-get="/flash/{{.Deck.ID}}/cards/{{.Card.ID}}" hx-target="#card-detail" hx-push-url="true">Cancel</a>
	</div>
</form>
{{end}}

{{define "card-list-items"}}
<ul class="card-list" id="card-list"{{if .OOB}} hx-swap-oob="true"{{end}}>
	{{if .Items}}
	{{range .Items}}
	{{$c := .Card}}
	<li>
		<a class="card-row{{if eq $c.ID $.ActiveID}} card-row-active{{end}}" href="{{$c.ID}}" hx-get="/flash/{{$c.DeckID}}/cards/{{$c.ID}}" hx-target="#card-detail" hx-push-url="true">
			{{$c.Front}}
		</a>
	</li>
	{{end}}
	{{else}}
	<li class="dim">No cards yet.</li>
	{{end}}
</ul>
{{end}}

{{define "content"}}
<div class="stack">
	<div class="row list-head">
		<h2>{{.Data.Deck.Name}}</h2>
		<a class="toolbar-btn" href="/flash/{{.Data.Deck.ID}}" hx-get="/flash/{{.Data.Deck.ID}}" hx-push-url="true">Back to deck</a>
		<a class="toolbar-btn" href="/flash/{{.Data.Deck.ID}}/cards/new" hx-get="/flash/{{.Data.Deck.ID}}/cards/new" hx-target="#card-detail" hx-push-url="true">New card</a>
	</div>
	{{template "card-list-items" .Data.List}}
	<div id="card-detail">
		{{template "card-detail-body" .Data.Detail}}
	</div>
</div>
{{end}}
```

- [ ] **Step 6: Run the tests to verify they pass**

Run: `go test ./internal/apps/flash/... -v`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/apps/flash/handlers_cards.go internal/apps/flash/templates/cards.html internal/apps/flash/flash.go internal/apps/flash/handlers_cards_test.go
git commit -m "feat(flash): add card CRUD handlers nested under a deck"
```

---

### Task 5: Tags — store, card-form wiring, and a cross-deck filter view

**Files:**
- Create: `internal/apps/flash/tag.go`
- Create: `internal/apps/flash/migrations/0003_tags.sql`
- Modify: `internal/apps/flash/handlers_cards.go` (thread tags through card create/edit)
- Modify: `internal/apps/flash/templates/cards.html` (tags field + chips)
- Modify: `internal/apps/flash/flash.go` (add the tag-filter route)
- Test: `internal/apps/flash/tag_test.go`
- Test: `internal/apps/flash/handlers_tags_test.go`

**Interfaces:**
- Consumes: `Store.CreateCard`/`UpdateCard`/`CardByID`/`ListCards` from Task 3; `a.userID`, `a.fail`, `a.render`, `cardBasePath` from Tasks 2 and 4.
- Produces: `flash.Tag{ID, UserID int64, Name string}`; `(*Store).SetCardTags(ctx, userID, cardID int64, names []string) error` (creates any tag that does not exist yet for that user, replaces the card's tag set); `(*Store).TagsForCard(ctx, userID, cardID int64) ([]Tag, error)`; `(*Store).CardsByTag(ctx, userID int64, tagName string) ([]Card, error)` (cross-deck); route `GET /flash/tags/{tagName}`.

- [ ] **Step 1: Write the failing store test**

```go
// internal/apps/flash/tag_test.go
package flash_test

import (
	"context"
	"sort"
	"testing"

	"github.com/iliafrenkel/on-suite/internal/apps/flash"
)

func tagNames(tags []flash.Tag) []string {
	names := make([]string, len(tags))
	for i, tg := range tags {
		names[i] = tg.Name
	}
	sort.Strings(names)
	return names
}

func TestSetAndFetchCardTags(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	deck, err := f.store.CreateDeck(ctx, f.alice.ID, "Spanish", "")
	if err != nil {
		t.Fatal(err)
	}
	card, err := f.store.CreateCard(ctx, f.alice.ID, deck.ID, flash.CardTypeBasic, "hola", "hello", "")
	if err != nil {
		t.Fatal(err)
	}

	if err := f.store.SetCardTags(ctx, f.alice.ID, card.ID, []string{"greetings", "beginner"}); err != nil {
		t.Fatalf("SetCardTags: %v", err)
	}
	got, err := f.store.TagsForCard(ctx, f.alice.ID, card.ID)
	if err != nil {
		t.Fatalf("TagsForCard: %v", err)
	}
	if want := []string{"beginner", "greetings"}; !equalStrings(tagNames(got), want) {
		t.Errorf("TagsForCard = %v, want %v", tagNames(got), want)
	}

	// Replacing the set drops "beginner" entirely.
	if err := f.store.SetCardTags(ctx, f.alice.ID, card.ID, []string{"greetings"}); err != nil {
		t.Fatal(err)
	}
	got, err = f.store.TagsForCard(ctx, f.alice.ID, card.ID)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"greetings"}; !equalStrings(tagNames(got), want) {
		t.Errorf("TagsForCard after replace = %v, want %v", tagNames(got), want)
	}
}

func TestSetCardTagsRejectsSomeoneElsesCard(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	deck, err := f.store.CreateDeck(ctx, f.alice.ID, "alice's", "")
	if err != nil {
		t.Fatal(err)
	}
	card, err := f.store.CreateCard(ctx, f.alice.ID, deck.ID, flash.CardTypeBasic, "a", "b", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := f.store.SetCardTags(ctx, f.bob.ID, card.ID, []string{"hijacked"}); err == nil {
		t.Error("bob was able to tag alice's card")
	}
}

func TestCardsByTagIsCrossDeckAndOwnerScoped(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	deckA, err := f.store.CreateDeck(ctx, f.alice.ID, "A", "")
	if err != nil {
		t.Fatal(err)
	}
	deckB, err := f.store.CreateDeck(ctx, f.alice.ID, "B", "")
	if err != nil {
		t.Fatal(err)
	}
	cardA, err := f.store.CreateCard(ctx, f.alice.ID, deckA.ID, flash.CardTypeBasic, "a1", "x", "")
	if err != nil {
		t.Fatal(err)
	}
	cardB, err := f.store.CreateCard(ctx, f.alice.ID, deckB.ID, flash.CardTypeBasic, "b1", "x", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := f.store.SetCardTags(ctx, f.alice.ID, cardA.ID, []string{"hard"}); err != nil {
		t.Fatal(err)
	}
	if err := f.store.SetCardTags(ctx, f.alice.ID, cardB.ID, []string{"hard"}); err != nil {
		t.Fatal(err)
	}

	got, err := f.store.CardsByTag(ctx, f.alice.ID, "hard")
	if err != nil {
		t.Fatalf("CardsByTag: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("CardsByTag(hard) returned %d cards, want 2 across both decks", len(got))
	}

	bobsGot, err := f.store.CardsByTag(ctx, f.bob.ID, "hard")
	if err != nil {
		t.Fatal(err)
	}
	if len(bobsGot) != 0 {
		t.Errorf("bob can see alice's tagged cards: %+v", bobsGot)
	}
}

func equalStrings(a, b []string) bool {
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

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/apps/flash/... -run TestSetAndFetchCardTags -v`
Expected: FAIL — `flash.Tag`, `SetCardTags`, `TagsForCard`, `CardsByTag` do not exist yet.

- [ ] **Step 3: Write the migration**

```sql
-- internal/apps/flash/migrations/0003_tags.sql
CREATE TABLE flash_tags (
    id      INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id INTEGER NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    name    TEXT    NOT NULL
) STRICT;

CREATE UNIQUE INDEX flash_tags_user_name_idx ON flash_tags (user_id, name);

CREATE TABLE flash_card_tags (
    card_id INTEGER NOT NULL REFERENCES flash_cards (id) ON DELETE CASCADE,
    tag_id  INTEGER NOT NULL REFERENCES flash_tags (id) ON DELETE CASCADE,
    PRIMARY KEY (card_id, tag_id)
) STRICT, WITHOUT ROWID;
```

- [ ] **Step 4: Write `tag.go`**

```go
// internal/apps/flash/tag.go
package flash

import (
	"context"
	"fmt"
	"strings"
)

// MaxTagNameRunes bounds a tag name.
const MaxTagNameRunes = 40

// Tag is one of userID's tags. Names are unique per user, not globally, so
// two accounts can each have their own "hard" tag without colliding.
type Tag struct {
	ID     int64
	UserID int64
	Name   string
}

// normalizeTagName trims and lowercases, so "Hard", "hard ", and "hard" are
// the same tag rather than three near-duplicates cluttering the filter list.
func normalizeTagName(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}

// SetCardTags replaces cardID's whole tag set with names, creating any tag
// that does not exist yet for userID. It fails with ErrNotFound if the card
// is not userID's own, via the same DeckByID-style ownership check as
// CreateCard: cardOwnerCheck below.
func (st *Store) SetCardTags(ctx context.Context, userID, cardID int64, names []string) error {
	if _, err := st.cardOwnerCheck(ctx, userID, cardID); err != nil {
		return err
	}

	tx, err := st.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("flash: set card tags: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, `DELETE FROM flash_card_tags WHERE card_id = ?`, cardID); err != nil {
		return fmt.Errorf("flash: set card tags: %w", err)
	}

	seen := make(map[string]bool)
	for _, raw := range names {
		name := normalizeTagName(raw)
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		if len([]rune(name)) > MaxTagNameRunes {
			return fmt.Errorf("%w: tag %q is longer than %d characters", ErrInvalid, name, MaxTagNameRunes)
		}

		var tagID int64
		err := tx.QueryRowContext(ctx, `SELECT id FROM flash_tags WHERE user_id = ? AND name = ?`, userID, name).Scan(&tagID)
		if err != nil {
			if err := tx.QueryRowContext(ctx,
				`INSERT INTO flash_tags (user_id, name) VALUES (?, ?) RETURNING id`, userID, name,
			).Scan(&tagID); err != nil {
				return fmt.Errorf("flash: set card tags: create tag %q: %w", name, err)
			}
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO flash_card_tags (card_id, tag_id) VALUES (?, ?)`, cardID, tagID); err != nil {
			return fmt.Errorf("flash: set card tags: link tag %q: %w", name, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("flash: set card tags: %w", err)
	}
	return nil
}

// cardOwnerCheck confirms cardID belongs to userID, returning its deck id.
// A thin wrapper so tag methods do not need CardByID's full column list.
func (st *Store) cardOwnerCheck(ctx context.Context, userID, cardID int64) (int64, error) {
	var deckID int64
	err := st.db.QueryRowContext(ctx,
		`SELECT deck_id FROM flash_cards WHERE id = ? AND user_id = ?`, cardID, userID).Scan(&deckID)
	if err != nil {
		return 0, ErrNotFound
	}
	return deckID, nil
}

// TagsForCard returns cardID's tags, alphabetically.
func (st *Store) TagsForCard(ctx context.Context, userID, cardID int64) ([]Tag, error) {
	if _, err := st.cardOwnerCheck(ctx, userID, cardID); err != nil {
		return nil, err
	}
	rows, err := st.db.QueryContext(ctx,
		`SELECT t.id, t.user_id, t.name
		 FROM flash_tags t
		 JOIN flash_card_tags ct ON ct.tag_id = t.id
		 WHERE ct.card_id = ?
		 ORDER BY t.name ASC`, cardID)
	if err != nil {
		return nil, fmt.Errorf("flash: tags for card: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []Tag
	for rows.Next() {
		var tg Tag
		if err := rows.Scan(&tg.ID, &tg.UserID, &tg.Name); err != nil {
			return nil, fmt.Errorf("flash: tags for card: %w", err)
		}
		out = append(out, tg)
	}
	return out, rows.Err()
}

// CardsByTag returns every one of userID's cards, across every deck, that
// carries tagName. This is Flash's cross-deck filter.
func (st *Store) CardsByTag(ctx context.Context, userID int64, tagName string) ([]Card, error) {
	name := normalizeTagName(tagName)
	rows, err := st.db.QueryContext(ctx,
		`SELECT c.id, c.deck_id, c.user_id, c.card_type, c.front, c.back, c.notes, c.created_at
		 FROM flash_cards c
		 JOIN flash_card_tags ct ON ct.card_id = c.id
		 JOIN flash_tags t ON t.id = ct.tag_id
		 WHERE c.user_id = ? AND t.user_id = ? AND t.name = ?
		 ORDER BY c.created_at DESC, c.id DESC`, userID, userID, name)
	if err != nil {
		return nil, fmt.Errorf("flash: cards by tag: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []Card
	for rows.Next() {
		c, err := scanCardRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}
```

- [ ] **Step 5: Run the store tests to verify they pass**

Run: `go test ./internal/apps/flash/... -run 'TestSetAndFetchCardTags|TestSetCardTagsRejectsSomeoneElsesCard|TestCardsByTagIsCrossDeckAndOwnerScoped' -v`
Expected: PASS.

- [ ] **Step 6: Wire tags into the card form — write the failing handler test first**

```go
// internal/apps/flash/handlers_tags_test.go
package flash_test

import (
	"net/url"
	"testing"
)

func TestCreatingACardSetsItsTags(t *testing.T) {
	s := newServer(t)
	deck, err := s.Store.CreateDeck(t.Context(), s.Alice.User.ID, "Spanish", "")
	if err != nil {
		t.Fatal(err)
	}
	s.Submit(t, s.Alice, "/flash/"+itoa(deck.ID)+"/cards/new",
		url.Values{"card_type": {"basic"}, "front": {"hola"}, "back": {"hello"}, "tags": {"greetings, beginner"}}, "")

	cards, err := s.Store.ListCards(t.Context(), s.Alice.User.ID, deck.ID)
	if err != nil || len(cards) != 1 {
		t.Fatalf("ListCards = %v, %v", cards, err)
	}
	tags, err := s.Store.TagsForCard(t.Context(), s.Alice.User.ID, cards[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(tags) != 2 {
		t.Errorf("TagsForCard = %v, want 2 tags", tags)
	}
}

func TestTagFilterViewListsCardsAcrossDecks(t *testing.T) {
	s := newServer(t)
	deckA, _ := s.Store.CreateDeck(t.Context(), s.Alice.User.ID, "A", "")
	deckB, _ := s.Store.CreateDeck(t.Context(), s.Alice.User.ID, "B", "")
	s.Submit(t, s.Alice, "/flash/"+itoa(deckA.ID)+"/cards/new",
		url.Values{"card_type": {"basic"}, "front": {"a1"}, "back": {"x"}, "tags": {"hard"}}, "")
	s.Submit(t, s.Alice, "/flash/"+itoa(deckB.ID)+"/cards/new",
		url.Values{"card_type": {"basic"}, "front": {"b1"}, "back": {"x"}, "tags": {"hard"}}, "")

	doc := s.Get(t, s.Alice, "/flash/tags/hard")
	items := doc.QueryAll(".tag-filter-item")
	if len(items) != 2 {
		t.Errorf("GET /flash/tags/hard shows %d cards, want 2", len(items))
	}
}
```

- [ ] **Step 7: Run the test to verify it fails**

Run: `go test ./internal/apps/flash/... -run TestCreatingACardSetsItsTags -v`
Expected: FAIL — the `tags` form field is not read yet, and `GET /flash/tags/{tagName}` does not exist.

- [ ] **Step 8: Thread tags through `handlers_cards.go`**

Add a `Tags string` (comma-separated, as typed in the form) field to `cardDetailView`, and a `parseTagList` helper:

```go
// Add to the cardDetailView struct in handlers_cards.go:
//   TagsValue string
```

```go
// Add near the top of handlers_cards.go.
func parseTagList(raw string) []string {
	var out []string
	for _, part := range strings.Split(raw, ",") {
		if name := strings.TrimSpace(part); name != "" {
			out = append(out, name)
		}
	}
	return out
}
```

(Add `"strings"` to the file's import block.)

Update `newCardDetail` and `editCardDetail` to accept and set `TagsValue`, and update every call site in `newCardForm`, `createCard`, `editCardForm`, and `updateCard` to pass a tags string through — for example, `createCard` becomes:

```go
func (a *App) createCard(w http.ResponseWriter, r *http.Request) {
	userID, ok := a.userID(w, r)
	if !ok {
		return
	}
	deck, ok := a.cardDeck(w, r, userID)
	if !ok {
		return
	}
	cardType := r.PostFormValue("card_type")
	front := r.PostFormValue("front")
	back := r.PostFormValue("back")
	notes := r.PostFormValue("notes")
	tags := r.PostFormValue("tags")

	if err := ValidateCard(cardType, front, back); err != nil {
		a.renderCardIndex(w, r, userID, deck, http.StatusBadRequest, a.newCardDetail(r, deck, userMessage(err), cardType, front, back, notes, tags))
		return
	}
	c, err := a.store.CreateCard(r.Context(), userID, deck.ID, cardType, front, back, notes)
	if err != nil {
		if errors.Is(err, ErrInvalid) {
			a.renderCardIndex(w, r, userID, deck, http.StatusBadRequest, a.newCardDetail(r, deck, userMessage(err), cardType, front, back, notes, tags))
			return
		}
		a.fail(w, r, err)
		return
	}
	if err := a.store.SetCardTags(r.Context(), userID, c.ID, parseTagList(tags)); err != nil {
		a.fail(w, r, err)
		return
	}

	if !web.IsHTMX(r) {
		http.Redirect(w, r, cardBasePath(deck.ID)+strconv.FormatInt(c.ID, 10), http.StatusSeeOther)
		return
	}
	w.Header().Set("HX-Push-Url", cardBasePath(deck.ID)+strconv.FormatInt(c.ID, 10))
	a.renderCardDetailWithList(w, r, userID, deck, http.StatusCreated, a.viewCardDetail(r, deck, c))
}
```

Apply the equivalent additions to `newCardForm` (pass `""` for tags), `editCardForm` (load `a.store.TagsForCard` and join the names with `", "` to prefill the field), `updateCard` (same `SetCardTags` call as `createCard`, after a successful `UpdateCard`), and `viewCardDetail` (add a `Tags []Tag` field, populated via `a.store.TagsForCard`, so the view mode can render chips).

- [ ] **Step 9: Add the tag-filter view — view model, handler, and route**

Add to `handlers_cards.go` (or a new small `handlers_tags.go` if you prefer keeping the file split symmetric with `tag.go` — either is fine, just keep it in one place):

```go
type tagFilterItem struct {
	Card Card
	Deck Deck
}

type tagFilterView struct {
	TagName string
	Items   []tagFilterItem
}

func (a *App) tagFilter(w http.ResponseWriter, r *http.Request) {
	userID, ok := a.userID(w, r)
	if !ok {
		return
	}
	tagName := r.PathValue("tagName")
	cards, err := a.store.CardsByTag(r.Context(), userID, tagName)
	if err != nil {
		a.deps.Errors.Internal(w, r, err)
		return
	}

	items := make([]tagFilterItem, 0, len(cards))
	for _, c := range cards {
		d, err := a.store.DeckByID(r.Context(), userID, c.DeckID)
		if err != nil {
			a.deps.Errors.Internal(w, r, err)
			return
		}
		items = append(items, tagFilterItem{Card: c, Deck: d})
	}

	page := a.deps.Page(r, "Tag: "+tagName)
	page.Data = tagFilterView{TagName: tagName, Items: items}
	a.render(w, r, http.StatusOK, "flash/tag-filter", page)
}
```

Register the route in `Mount` (`flash.go`), grouped with the deck routes since it is top-level like them:

```go
// GET /tags/{tagName} is 2 segments (literal "tags", wildcard), a different
// shape from GET /{deckID} (1 segment) and GET /edit/{deckID} (literal
// "edit", wildcard) — no ambiguity with either.
r.HandleFunc("GET /tags/{tagName}", a.tagFilter)
```

- [ ] **Step 10: Write `templates/tag-filter.html`**

```html
{{define "content"}}
<div class="stack">
	<h1>Tag: {{.Data.TagName}}</h1>
	<ul class="tag-filter-list">
		{{if .Data.Items}}
		{{range .Data.Items}}
		<li class="tag-filter-item">
			<a href="/flash/{{.Deck.ID}}/cards/{{.Card.ID}}">{{.Card.Front}}</a>
			<span class="faint">in {{.Deck.Name}}</span>
		</li>
		{{end}}
		{{else}}
		<li class="dim">No cards carry this tag.</li>
		{{end}}
	</ul>
</div>
{{end}}
```

- [ ] **Step 11: Add the tags input to the card form in `templates/cards.html`**

Add one more field inside `card-form-fields` (after the notes field):

```html
<div class="field">
	<label for="card-tags-{{.IDSuffix}}">Tags (comma-separated)</label>
	<input id="card-tags-{{.IDSuffix}}" type="text" name="tags" value="{{.TagsValue}}">
</div>
```

Update the two `{{template "card-form-fields" (dict ...)}}` calls in `card-detail-new` and `card-detail-edit` to also pass `"TagsValue" .TagsValue`. Add chips to `card-detail-view` so tags are visible without opening edit mode:

```html
{{with .Tags}}
<ul class="tag-list">
	{{range .}}<li><a href="/flash/tags/{{.Name}}">{{.Name}}</a></li>{{end}}
</ul>
{{end}}
```

(placed just after the `.Card.Notes` block).

- [ ] **Step 12: Run every flash test**

Run: `go test ./internal/apps/flash/... -v`
Expected: PASS — every deck, card, and tag test from Tasks 1–5.

- [ ] **Step 13: Run the full check**

Run:
```bash
gofmt -l .
go vet ./...
go run honnef.co/go/tools/cmd/staticcheck@v0.8.0 ./...
go mod tidy && git diff --exit-code go.mod go.sum
go test ./... -race -count=1
```
Expected: every command is clean/green, matching [AGENTS.md](../../../AGENTS.md)'s definition of the full check.

- [ ] **Step 14: Commit**

```bash
git add internal/apps/flash/tag.go internal/apps/flash/migrations/0003_tags.sql internal/apps/flash/handlers_cards.go internal/apps/flash/templates/cards.html internal/apps/flash/templates/tag-filter.html internal/apps/flash/flash.go internal/apps/flash/tag_test.go internal/apps/flash/handlers_tags_test.go
git commit -m "feat(flash): add per-card tags and a cross-deck tag filter view"
```

---

## After this plan

F1 leaves ON Flash with full deck/card CRUD, card notes, and tags, but no scheduling, review session, import, media, or sharing — those are F2 (review engine + FSRS), F3 (import), F4 (media), F5 (sharing), and F6 (stats), each its own plan against [docs/superpowers/specs/2026-09-17-on-flash-features.md](../specs/2026-09-17-on-flash-features.md).
