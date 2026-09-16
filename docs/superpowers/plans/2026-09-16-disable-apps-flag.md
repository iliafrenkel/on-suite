# Disable-apps flag Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let a deployment turn off one or more registered apps at startup via a `-disable-apps` flag / `ONSUITE_DISABLE_APPS` env var, with zero data loss and no other code path affected.

**Architecture:** A new `Config.DisabledApps []string` setting, parsed the same way every other `config.Config` field is. A new pure function `filterApps` in `cmd/onsuite/main.go` that takes the full `registeredApps()` list and the disabled-IDs list and returns the apps that should be part of this run's `app.Registry`. That filtering happens in exactly one place — `openDatabase` in `cmd/onsuite/database.go`, the single choke point every command already goes through — so nav, routes, migrations, admin stats, and scheduled jobs all inherit the filtering for free, because `app.Registry` already derives every one of those purely from the apps it was constructed with.

**Tech Stack:** Go standard library only (`flag`, `strings`) — no new dependencies.

## Global Constraints

- Full check must stay green on every commit: `gofmt -l .` (empty), `go vet ./...`, `go run honnef.co/go/tools/cmd/staticcheck@v0.8.0 ./...`, `go mod tidy && git diff --exit-code go.mod go.sum`, `go test ./... -race -count=1`.
- Never commit or push directly to `main` — this work happens on `spec/disable-apps-flag` (already checked out) and ends in a PR.
- Follow the spec exactly: [docs/superpowers/specs/2026-09-16-disable-apps-flag-design.md](../specs/2026-09-16-disable-apps-flag-design.md).
  - `-disable-apps` / `ONSUITE_DISABLE_APPS`: comma-separated app IDs, default empty.
  - Filtering happens only for `serve` (the only command that calls `config.Parse`); `export`, `backup`, and `user add` build their own `config.Config{DataDir: ...}` literal and are unaffected — always see every registered app.
  - A disabled app: no nav entry, no routes, no further migrations (existing tables/data untouched), no admin stats, no scheduled jobs.
  - Unknown app ID in `-disable-apps` is a startup error naming the bad ID and the known IDs. Disabling every registered app is also a startup error.
  - A duplicate ID in the list is not an error.

---

### Task 1: `Config.DisabledApps` setting

**Files:**
- Modify: `internal/platform/config/config.go`
- Test: `internal/platform/config/config_test.go`

**Interfaces:**
- Produces: `Config.DisabledApps []string` — the cleaned (trimmed, empty entries dropped) list of app IDs to disable, in the order given. Consumed by Task 2's `filterApps`.

- [ ] **Step 1: Write the failing tests**

Add to `internal/platform/config/config_test.go`:

```go
func TestParseDisabledApps(t *testing.T) {
	tests := []struct {
		name string
		args []string
		env  map[string]string
		want []string
	}{
		{"default is empty", nil, nil, nil},
		{"single app", []string{"-disable-apps", "reader"}, nil, []string{"reader"}},
		{"multiple apps", []string{"-disable-apps", "notes,reader"}, nil, []string{"notes", "reader"}},
		{"whitespace and empty entries are cleaned", []string{"-disable-apps", " notes , ,reader "}, nil, []string{"notes", "reader"}},
		{"env used when no flag", nil, map[string]string{"ONSUITE_DISABLE_APPS": "paste"}, []string{"paste"}},
		{"flag beats env", []string{"-disable-apps", "reader"}, map[string]string{"ONSUITE_DISABLE_APPS": "paste"}, []string{"reader"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			getenv := func(k string) string { return tt.env[k] }
			c, err := Parse(tt.args, getenv, io.Discard)
			if err != nil {
				t.Fatalf("Parse: %v", err)
			}
			if !slices.Equal(c.DisabledApps, tt.want) {
				t.Errorf("DisabledApps = %v, want %v", c.DisabledApps, tt.want)
			}
		})
	}
}
```

This needs a new import in `config_test.go`: add `"slices"` to the import block.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/platform/config/... -run TestParseDisabledApps -v`
Expected: FAIL — `c.DisabledApps` undefined (compile error), since the field doesn't exist yet.

- [ ] **Step 3: Add the setting**

In `internal/platform/config/config.go`:

Add a default constant next to the other `default*` constants (around line 27):

```go
	defaultDisableApps    = ""
```

Add to `settingSpecs` (after the `secure-cookies` entry, around line 86):

```go
	{"disable-apps", "ONSUITE_DISABLE_APPS", defaultDisableApps},
```

Add the field to `Config` (after `SecureCookies bool`, around line 111):

```go
	// DisabledApps is the set of app IDs (Meta.ID) excluded from this run's
	// app.Registry: no nav entry, no routes, no further migrations, no
	// admin stats, no scheduled jobs — see cmd/onsuite's filterApps. Only
	// `serve` ever sets this; export/backup/user build a Config literal
	// directly and always see every registered app.
	DisabledApps []string
```

In `Parse`, add flag parsing (after the `secureCookies := fs.Bool(...)` block, around line 161, before `fs.Parse`):

```go
	disableAppsRaw := fs.String("disable-apps", envOr(getenv, "ONSUITE_DISABLE_APPS", defaultDisableApps),
		"comma-separated app IDs to disable for this run (e.g. \"notes,reader\")")
```

After `fs.Parse(args)` succeeds and before the existing validation block (around line 164), add:

```go
	c.DisabledApps = splitCleanList(*disableAppsRaw)
```

Add the helper near `envOr` (around line 296), and add `"strings"` is already imported:

```go
// splitCleanList splits a comma-separated flag value into trimmed, non-empty
// entries, in order. "" becomes nil; " notes , ,reader " becomes
// ["notes" "reader"].
func splitCleanList(raw string) []string {
	var out []string
	for _, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}
```

In `collectSettings`, add to the `live` map (around line 216):

```go
		"disable-apps":    strings.Join(c.DisabledApps, ","),
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/platform/config/... -run TestParseDisabledApps -v`
Expected: PASS

- [ ] **Step 5: Update `TestEverySettingIsDescribed`**

In `internal/platform/config/config_test.go`, `TestEverySettingIsDescribed`'s `want` slice (around line 314-317) must list every flag in registration order — add `"disable-apps"` at the end:

```go
	want := []string{
		"addr", "data-dir", "tls-domain", "log-level",
		"backup-interval", "backup-keep", "tls-http-addr", "secure-cookies",
		"disable-apps",
	}
```

- [ ] **Step 6: Run the full config package test suite**

Run: `go test ./internal/platform/config/... -race -count=1 -v`
Expected: PASS, all tests including `TestEverySettingIsDescribed` and `TestSettingsOnAHandBuiltConfigIsEmptyRatherThanWrong`.

- [ ] **Step 7: Commit**

```bash
git add internal/platform/config/config.go internal/platform/config/config_test.go
git commit -m "feat(config): add -disable-apps / ONSUITE_DISABLE_APPS setting"
```

---

### Task 2: `filterApps` and wiring it into the registry

**Files:**
- Modify: `cmd/onsuite/main.go`
- Modify: `cmd/onsuite/database.go`
- Test: `cmd/onsuite/main_test.go` (new)

**Interfaces:**
- Consumes: `Config.DisabledApps []string` (Task 1); `app.App`'s `Meta() app.Meta` (existing, `internal/platform/app/app.go`).
- Produces: `filterApps(all []app.App, disabled []string) ([]app.App, error)` — used by `openDatabase` (this task) and nothing else.

This task's tests use the existing `fakeHomeApp` type already defined in `cmd/onsuite/stack_test.go` (same package `main`, so it's visible here with no new fixture needed):

```go
type fakeHomeApp struct{ id, name string }

func (f fakeHomeApp) Meta() app.Meta {
	return app.Meta{ID: f.id, Name: f.name, Summary: "does things", Order: 0}
}
func (f fakeHomeApp) Migrations() fs.FS { return fstest.MapFS{} }
func (f fakeHomeApp) Mount(r *app.Router, d app.Deps) {
	r.HandleFunc("GET /{$}", func(w http.ResponseWriter, req *http.Request) {})
}
```

- [ ] **Step 1: Write the failing tests**

Create `cmd/onsuite/main_test.go`:

```go
package main

import (
	"strings"
	"testing"
)

func appIDs(apps []app.App) []string {
	ids := make([]string, len(apps))
	for i, a := range apps {
		ids[i] = a.Meta().ID
	}
	return ids
}

func TestFilterAppsKeepsEverythingWhenNothingDisabled(t *testing.T) {
	all := []app.App{
		fakeHomeApp{id: "notes", name: "ON Notes"},
		fakeHomeApp{id: "paste", name: "ON Paste"},
	}
	got, err := filterApps(all, nil)
	if err != nil {
		t.Fatalf("filterApps: %v", err)
	}
	if want := "notes,paste"; strings.Join(appIDs(got), ",") != want {
		t.Errorf("got %v, want %s", appIDs(got), want)
	}
}

func TestFilterAppsRemovesDisabledAppsInOrder(t *testing.T) {
	all := []app.App{
		fakeHomeApp{id: "notes", name: "ON Notes"},
		fakeHomeApp{id: "paste", name: "ON Paste"},
		fakeHomeApp{id: "reader", name: "ON Reader"},
	}
	got, err := filterApps(all, []string{"paste"})
	if err != nil {
		t.Fatalf("filterApps: %v", err)
	}
	if want := "notes,reader"; strings.Join(appIDs(got), ",") != want {
		t.Errorf("got %v, want %s", appIDs(got), want)
	}
}

func TestFilterAppsTreatsADuplicateDisabledIDAsHarmless(t *testing.T) {
	all := []app.App{
		fakeHomeApp{id: "notes", name: "ON Notes"},
		fakeHomeApp{id: "paste", name: "ON Paste"},
	}
	got, err := filterApps(all, []string{"paste", "paste"})
	if err != nil {
		t.Fatalf("filterApps: %v", err)
	}
	if want := "notes"; strings.Join(appIDs(got), ",") != want {
		t.Errorf("got %v, want %s", appIDs(got), want)
	}
}

func TestFilterAppsRejectsAnUnknownID(t *testing.T) {
	all := []app.App{fakeHomeApp{id: "notes", name: "ON Notes"}}
	_, err := filterApps(all, []string{"bogus"})
	if err == nil {
		t.Fatal("filterApps succeeded, want an error for an unknown app ID")
	}
	if !strings.Contains(err.Error(), "bogus") || !strings.Contains(err.Error(), "notes") {
		t.Errorf("error %q should name the bad ID and the known IDs", err)
	}
}

func TestFilterAppsRejectsDisablingEverything(t *testing.T) {
	all := []app.App{
		fakeHomeApp{id: "notes", name: "ON Notes"},
		fakeHomeApp{id: "paste", name: "ON Paste"},
	}
	_, err := filterApps(all, []string{"notes", "paste"})
	if err == nil {
		t.Fatal("filterApps succeeded, want an error when every app is disabled")
	}
}
```

This needs `"github.com/iliafrenkel/on-suite/internal/platform/app"` imported — `stack_test.go` already imports it, but each `_test.go` file needs its own import block, so add it to `main_test.go`'s imports too:

```go
import (
	"strings"
	"testing"

	"github.com/iliafrenkel/on-suite/internal/platform/app"
)
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./cmd/onsuite/... -run TestFilterApps -v`
Expected: FAIL — `filterApps` undefined (compile error).

- [ ] **Step 3: Implement `filterApps`**

In `cmd/onsuite/main.go`, add after `registeredApps()`:

```go
// filterApps returns the apps that should be part of this run's registry:
// every registered app, minus the ones named in disabled. It is how
// -disable-apps/ONSUITE_DISABLE_APPS takes effect — see openDatabase, the
// only caller.
//
// An unknown ID is almost certainly a typo, so it is a startup error rather
// than a silent no-op. A duplicate ID is harmless (disabled is treated as a
// set) and not an error. Ending up with zero apps is also an error: a binary
// with nothing registered is never an intentional configuration.
func filterApps(all []app.App, disabled []string) ([]app.App, error) {
	if len(disabled) == 0 {
		return all, nil
	}

	off := make(map[string]bool, len(disabled))
	known := make([]string, len(all))
	for i, a := range all {
		known[i] = a.Meta().ID
	}
	for _, id := range disabled {
		found := false
		for _, k := range known {
			if k == id {
				found = true
				break
			}
		}
		if !found {
			return nil, fmt.Errorf("disable-apps: unknown app %q (known apps: %s)", id, strings.Join(known, ", "))
		}
		off[id] = true
	}

	kept := make([]app.App, 0, len(all))
	for _, a := range all {
		if !off[a.Meta().ID] {
			kept = append(kept, a)
		}
	}
	if len(kept) == 0 {
		return nil, fmt.Errorf("disable-apps: disables every registered app (%s); refusing to run with none", strings.Join(known, ", "))
	}
	return kept, nil
}
```

Add `"strings"` to `main.go`'s import block (it currently imports `"errors"`, `"fmt"`, `"io"`, `"os"`, plus the app packages).

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./cmd/onsuite/... -run TestFilterApps -v`
Expected: PASS, all five tests.

- [ ] **Step 5: Wire `filterApps` into `openDatabase`**

In `cmd/onsuite/database.go`, change:

```go
	registry, err := app.NewRegistry(registeredApps()...)
	if err != nil {
		return nil, nil, 0, err
	}
```

to:

```go
	apps, err := filterApps(registeredApps(), cfg.DisabledApps)
	if err != nil {
		return nil, nil, 0, err
	}
	registry, err := app.NewRegistry(apps...)
	if err != nil {
		return nil, nil, 0, err
	}
```

- [ ] **Step 6: Run the full `cmd/onsuite` test suite**

Run: `go test ./cmd/onsuite/... -race -count=1`
Expected: PASS — every existing command (`serve`, `export`, `backup`, `user add`) still builds its registry the same way it did before, since none of them ever sets `cfg.DisabledApps`.

- [ ] **Step 7: Commit**

```bash
git add cmd/onsuite/main.go cmd/onsuite/database.go cmd/onsuite/main_test.go
git commit -m "feat(cmd): filter registry apps by -disable-apps in openDatabase"
```

---

### Task 3: End-to-end proof against the real database and app set

**Files:**
- Test: `cmd/onsuite/database_test.go` (new)

**Interfaces:**
- Consumes: `openDatabase(ctx context.Context, cfg config.Config) (*sql.DB, *app.Registry, int, error)` (existing, `cmd/onsuite/database.go`); `registeredApps()` (existing, `cmd/onsuite/main.go`, returns `notes.New()`, `paste.New()`, `reader.New()`).

This task proves the feature against the real, production `registeredApps()` list (not fakes), so it catches anything Task 2's fakes couldn't — real migrations actually not running, real table names actually absent.

- [ ] **Step 1: Write the failing tests**

Create `cmd/onsuite/database_test.go`:

```go
package main

import (
	"context"
	"database/sql"
	"testing"

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
	if len(ids) != 2 {
		t.Errorf("NavItems() = %v, want exactly notes and paste", ids)
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
	if len(registry.NavItems()) != 3 {
		t.Errorf("NavItems() has %d entries, want 3 (notes, paste, reader)", len(registry.NavItems()))
	}
}
```

`seedDatabase` already exists in `cmd/onsuite/export_test.go` (same package), so no new fixture is needed for the last test.

Unlike Tasks 1-2, `openDatabase` is already wired correctly by the end of
Task 2 — there is no new production code in this task. These tests are
regression coverage against the real `notes`/`paste`/`reader` app set (not
Task 2's fakes), so they are expected to **pass immediately**, proving the
fakes-based unit tests generalize to the real migrations and table names.

- [ ] **Step 2: Run the tests and confirm they pass**

Run: `go test ./cmd/onsuite/... -run TestOpenDatabase -v`
Expected: PASS, all four tests. If any fails, that means Task 2's filtering
doesn't hold up against a real app's actual `Migrations()`/table names —
diagnose against `internal/apps/reader/migrations` and
`cmd/onsuite/database.go` and fix there before continuing.

- [ ] **Step 3: Run the full repository check**

Run:
```bash
gofmt -l .
go vet ./...
go run honnef.co/go/tools/cmd/staticcheck@v0.8.0 ./...
go mod tidy && git diff --exit-code go.mod go.sum
go test ./... -race -count=1
```
Expected: `gofmt -l .` prints nothing; every other command exits 0.

- [ ] **Step 4: Commit**

```bash
git add cmd/onsuite/database_test.go
git commit -m "test(cmd): prove -disable-apps against the real app set and migrations"
```

---

## Self-Review Notes

- **Spec coverage:** §3 (config setting) → Task 1. §4 (filtering, validation, export/backup/user unaffected) → Tasks 2–3. §5 (re-enabling catches up migrations) → Task 3's `TestOpenDatabaseReenablingBringsMigrationsUpToDate`. §6 (testing) → covered across all three tasks' test files.
- **Type consistency:** `filterApps(all []app.App, disabled []string) ([]app.App, error)` is defined once in Task 2 and used exactly that way in `database.go`; no other task redefines its signature.
- **No admin-template changes needed:** `internal/platform/admin/admin.go` already renders `Config.Settings()` generically (`rep.Settings = d.Config.Settings()`), so the new setting appears on the admin page with no template edit.
