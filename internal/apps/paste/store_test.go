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
		wantLabel string
	}{
		{"", 0, "0 lines"},
		{"one\n", 1, "1 line"},
		{"one", 1, "1 line"},
		{"one\ntwo\n", 2, "2 lines"},
		{"one\ntwo\nthree", 3, "3 lines"},
	}
	for _, tt := range tests {
		s := paste.Snippet{Body: tt.body}
		if got := s.Lines(); got != tt.wantLines {
			t.Errorf("Lines(%q) = %d, want %d", tt.body, got, tt.wantLines)
		}
		if got := s.LinesLabel(); got != tt.wantLabel {
			t.Errorf("LinesLabel(%q) = %q, want %q", tt.body, got, tt.wantLabel)
		}
	}

	if got := (paste.Snippet{Title: "  "}).DisplayTitle(); got != "Untitled" {
		t.Errorf("DisplayTitle for a blank title = %q, want Untitled", got)
	}
	if got := (paste.Snippet{Title: "Real"}).DisplayTitle(); got != "Real" {
		t.Errorf("DisplayTitle = %q", got)
	}
}

func TestStatsOnAnEmptyDatabase(t *testing.T) {
	f := newFixture(t)

	got, err := f.store.Stats(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 5 {
		t.Fatalf("Stats() returned %d stats, want 5", len(got))
	}
	if got[0].Label != "Snippets" || got[0].Value != "0" {
		t.Errorf("Stats()[0] = %+v, want Snippets 0", got[0])
	}
	if got[4].Label != "Newest" || got[4].Value != "never" {
		t.Errorf("Stats()[4] = %+v; an empty table must not render a zero timestamp", got[4])
	}
}

func TestStatsCountsSnippetsAndShares(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	first, err := f.store.Create(ctx, f.alice.ID, "one", "go", "package main")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.Create(ctx, f.bob.ID, "two", "go", "package main // longer"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.Share(ctx, f.alice.ID, first.ID); err != nil {
		t.Fatal(err)
	}

	got, err := f.store.Stats(ctx)
	if err != nil {
		t.Fatal(err)
	}
	by := map[string]string{}
	for _, s := range got {
		by[s.Label] = s.Value
	}
	if by["Snippets"] != "2" {
		t.Errorf("Snippets = %q, want 2", by["Snippets"])
	}
	if by["Shared"] != "1" {
		t.Errorf("Shared = %q, want 1", by["Shared"])
	}
	if by["Newest"] == "never" {
		t.Error("Newest is 'never' with two snippets stored")
	}
	if by["Total size"] == "" || by["Largest"] == "" {
		t.Errorf("size stats are empty: %v", by)
	}
}
