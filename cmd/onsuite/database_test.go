package main

import (
	"context"
	"database/sql"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/iliafrenkel/on-suite/internal/apptest"
	"github.com/iliafrenkel/on-suite/internal/platform/auth"
	"github.com/iliafrenkel/on-suite/internal/platform/config"
)

// tableExists reports whether a table with this exact name exists in the
// database's own schema.
func tableExists(t *testing.T, handle *sql.DB, name string) bool {
	t.Helper()
	var count int
	err := handle.QueryRow(
		`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = ?`, name,
	).Scan(&count)
	if err != nil {
		t.Fatalf("query sqlite_master: %v", err)
	}
	return count > 0
}

func TestOpenDatabaseSkipsMigrationsForADisabledApp(t *testing.T) {
	cfg := config.Config{DataDir: t.TempDir(), DisabledApps: []string{"reader"}}
	handle, registry, _, err := openDatabase(context.Background(), cfg)
	if err != nil {
		t.Fatalf("openDatabase: %v", err)
	}
	defer func() { _ = handle.Close() }()

	if tableExists(t, handle, "reader_feeds") {
		t.Error("reader_feeds table exists even though reader is disabled")
	}
	if !tableExists(t, handle, "paste_snippets") {
		t.Error("paste_snippets table is missing; disabling reader should not affect paste")
	}

	var ids []string
	for _, item := range registry.NavItems() {
		ids = append(ids, item.ID)
	}
	for _, id := range ids {
		if id == "reader" {
			t.Errorf("NavItems() includes disabled app %q: %v", id, ids)
		}
	}
	if len(ids) != 3 {
		t.Errorf("NavItems() = %v, want exactly flash, notes, and paste", ids)
	}
}

// TestDisabledAppIsUnreachableAndOmittedFromExportAndStats proves the two
// user-facing halves of -disable-apps that the implementation plan's Task 3
// never actually exercised at this layer (spec §6): a disabled app's routes
// genuinely 404 through the real HTTP stack, and it is left out of both
// Registry.Export and Registry.Stats.
func TestDisabledAppIsUnreachableAndOmittedFromExportAndStats(t *testing.T) {
	cfg := config.Config{DataDir: t.TempDir(), DisabledApps: []string{"reader"}}
	handle, registry, _, err := openDatabase(context.Background(), cfg)
	if err != nil {
		t.Fatalf("openDatabase: %v", err)
	}
	defer func() { _ = handle.Close() }()

	users := auth.NewStore(handle)

	stack, err := buildStack(stackDeps{
		DB:       handle,
		Users:    users,
		Registry: registry,
		Log:      slog.New(slog.DiscardHandler),
		Version:  "test",
	})
	if err != nil {
		t.Fatalf("buildStack: %v", err)
	}

	rec := httptest.NewRecorder()
	stack.ServeHTTP(rec, httptest.NewRequest("GET", "/reader/", nil))
	if rec.Code != http.StatusNotFound {
		t.Errorf("GET /reader/ = %d, want %d (disabled app must be unreachable)", rec.Code, http.StatusNotFound)
	}

	ctx := context.Background()
	user, err := users.CreateUser(ctx, "tester", apptest.PasswordHash, false)
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	exported, err := registry.Export(ctx, handle, user.ID)
	if err != nil {
		t.Fatalf("Export: %v", err)
	}
	if _, ok := exported["reader"]; ok {
		t.Error("Export includes a \"reader\" key even though reader is disabled")
	}
	if _, ok := exported["paste"]; !ok {
		t.Error("Export is missing \"paste\"; Export appears broken, not just correctly filtering reader")
	}

	for _, stats := range registry.Stats(ctx, handle) {
		if stats.ID == "reader" {
			t.Errorf("Stats() includes disabled app %q", stats.ID)
		}
	}
}

func TestOpenDatabaseReenablingBringsMigrationsUpToDate(t *testing.T) {
	dataDir := t.TempDir()

	cfg := config.Config{DataDir: dataDir, DisabledApps: []string{"reader"}}
	handle, _, _, err := openDatabase(context.Background(), cfg)
	if err != nil {
		t.Fatalf("openDatabase (disabled): %v", err)
	}
	if tableExists(t, handle, "reader_feeds") {
		t.Fatal("reader_feeds should not exist yet")
	}
	if err := handle.Close(); err != nil {
		t.Fatal(err)
	}

	cfg = config.Config{DataDir: dataDir}
	handle, _, applied, err := openDatabase(context.Background(), cfg)
	if err != nil {
		t.Fatalf("openDatabase (re-enabled): %v", err)
	}
	defer func() { _ = handle.Close() }()
	if applied == 0 {
		t.Error("re-enabling reader applied zero migrations; its schema was never created")
	}
	if !tableExists(t, handle, "reader_feeds") {
		t.Error("reader_feeds still missing after re-enabling reader")
	}
}

func TestOpenDatabaseRejectsAnUnknownDisabledApp(t *testing.T) {
	cfg := config.Config{DataDir: t.TempDir(), DisabledApps: []string{"bogus"}}
	if _, _, _, err := openDatabase(context.Background(), cfg); err == nil {
		t.Fatal("openDatabase succeeded with an unknown disabled app, want an error")
	}
}

func TestOpenDatabaseExportBuildsAConfigLiteralSoDisablingNeverApplies(t *testing.T) {
	// export.go/backup.go/user.go each build config.Config{DataDir: ...}
	// directly rather than calling config.Parse, so DisabledApps is always
	// nil there — this documents and locks in that behavior at the
	// openDatabase level, independent of any one command's flag parsing.
	dir := seedDatabase(t)
	cfg := config.Config{DataDir: dir}
	handle, registry, _, err := openDatabase(context.Background(), cfg)
	if err != nil {
		t.Fatalf("openDatabase: %v", err)
	}
	defer func() { _ = handle.Close() }()
	if len(registry.NavItems()) != 4 {
		t.Errorf("NavItems() has %d entries, want 4 (flash, notes, paste, reader)", len(registry.NavItems()))
	}
}
