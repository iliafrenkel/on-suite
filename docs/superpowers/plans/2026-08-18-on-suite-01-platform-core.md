# ON Suite — Plan 1 of 3: Platform Core

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build the non-HTTP foundation of ON Suite — configuration, SQLite storage with pragmas, a forward-only migration runner, Argon2id password hashing, and the user and session stores — ending with `onsuite user add` creating a real account in a real database.

**Deliverable:** A binary that initialises a data directory, applies migrations, and creates the first administrator account from the command line. No web UI yet; that is Plan 2.

**Sibling plans:** `2026-08-18-on-suite-00-roadmap.md` lists all three plans and every task. Plan 2 (web plumbing and app framework) and Plan 3 (ON Paste and operations) are written after this plan is executed and reviewed, so they can absorb what is learned here.

**Architecture:** One Go module, one binary. The platform under `internal/platform/` owns everything shared and never imports an app. This plan builds three platform packages — `config`, `db`, `auth` — plus the command shell. Nothing below the command layer knows about HTTP, which is what lets `auth` be tested without a server.

**Tech Stack:** Go 1.26, stdlib `net/http` and `flag`, `modernc.org/sqlite v1.56.0` (pure Go, no CGO), `golang.org/x/crypto/argon2`.

**Spec:** `docs/superpowers/specs/2026-08-18-on-suite-platform-design.md`

## This plan's code has been compiled and run

Every Go and SQL block below was extracted from this document, assembled into
a module, and executed before the plan was published. On the toolchain in this
repository (Go 1.26.6, SQLite 3.53.3 via `modernc.org/sqlite` v1.56.0):

- `gofmt -l .` is clean, `go vet ./...` is clean.
- `go test ./... -race` passes in all four packages.
- `CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build ./cmd/onsuite` succeeds.
- The end-to-end sequence works: `user add` on an empty directory, then
  `serve` applying migrations, answering `/healthz`, and leaving no WAL file
  after SIGTERM.

Two defects were found and fixed this way, both now covered by tests:
`flag.Parse` silently mishandling `user add ilia --admin` (see
`parseInterspersed` in Task 7), and a test that omitted its username argument.

This means the code blocks are known-good rather than plausible. If a step
fails when you run it, suspect a transcription slip or a genuine environment
difference before rewriting the design.

## Global Constraints

Every task's requirements implicitly include this section.

- Module path is `github.com/iliafrenkel/on-suite`. Go directive `go 1.26`.
- **`CGO_ENABLED=0` must always hold.** Never add a dependency requiring CGO. Verified working for `linux/arm64`, `linux/amd64`, `darwin/arm64`.
- **Platform dependencies are capped at three:** `modernc.org/sqlite v1.56.0`, `golang.org/x/crypto v0.55.0` and `golang.org/x/term v0.45.0`. All are pure Go and CGO-free. `golang.org/x/net` is permitted as a test-only dependency for HTML parsing. Adding anything else is a spec change, not an implementation decision.
- **No Node, no npm, no JavaScript build step.** `go build ./cmd/onsuite` is the entire build.
- **HTMX is vendored into the repository and embedded.** Never referenced from a CDN — a self-hosted binary must render without public internet access.
- All templates, static assets and `.sql` migrations are embedded with `go:embed`. The binary never reads them from disk.
- App ids are lowercase single words (`paste`). Display names are `ON <Name>` (`ON Paste`). Every table an app owns is prefixed with its id and an underscore (`paste_snippets`).
- Migrations are **forward-only**. There are no down migrations.
- SQLite is opened with `journal_mode=WAL`, `busy_timeout=5000`, `foreign_keys=ON` and `SetMaxOpenConns(1)`.
- **Import boundaries** (enforced by the test in Task 14): apps never import other apps; apps import only `internal/platform/*` and `internal/ui`; the platform never imports an app.
- Every commit must leave `go build ./... && go vet ./... && go test ./... -race` green.

### Testing policy — read this before Task 4

The spec deliberately chooses **tests written after the implementation, but comprehensive**, with one exception: **auth is test-first.**

- **Tasks 4, 6, 11 and 12 are strict TDD.** Write the failing test, run it, watch it fail, then implement. These cover password hashing, sessions, CSRF and the auth middleware. Their sad paths are the ones that never get written if deferred, and every app depends on them.
- **All other tasks are implement-then-test, within the same task and the same commit.** Not "test later" — the task is not complete without its tests.
- Store tests use a real SQLite file in `t.TempDir()`, never a mock or `:memory:`. The interesting bugs are in the SQL.
- Handler tests assert against parsed HTML using `golang.org/x/net/html`, never string comparison of markup. String-comparing HTML produces a suite that breaks on every CSS change and teaches you to ignore failures.

`golang.org/x/net` is a test-only dependency and does not count against the dependency cap.

---

## File Structure

Only the files this plan creates. The full suite layout is in the spec, §4.

| Path | Responsibility |
|---|---|
| `cmd/onsuite/main.go` | Command dispatch (`serve`, `user`, `version`) and usage text. |
| `cmd/onsuite/serve.go` | The `serve` command: build the stack, listen, graceful shutdown. |
| `cmd/onsuite/user.go` | The `user add` command. |
| `internal/platform/config/config.go` | Flags + `ONSUITE_*` env → `Config`. Owns all defaults and path derivation. |
| `internal/platform/db/db.go` | Open SQLite with pragmas; `Checkpoint`, `BackupTo`. |
| `internal/platform/db/migrate.go` | Collect embedded migrations, apply forward-only in a transaction. |
| `internal/platform/auth/password.go` | Argon2id hash and verify, PHC-encoded. No storage knowledge. |
| `internal/platform/auth/store.go` | `users` and `sessions` SQL. No HTTP knowledge. |
| `internal/platform/auth/migrations/0001_identity.sql` | The `users` and `sessions` schema. |

Deliberately absent: anything that renders HTML, and anything that knows what an "app" is. Both arrive in Plan 2.

---

# Phase 1 — Skeleton

## Task 1: Module bootstrap, config, and a server that starts and stops cleanly

**Files:**
- Create: `go.mod`
- Create: `internal/platform/config/config.go`
- Create: `cmd/onsuite/main.go`
- Create: `cmd/onsuite/serve.go`
- Test: `internal/platform/config/config_test.go`
- Test: `cmd/onsuite/serve_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces: `config.Config{Addr, DataDir, TLSDomain, LogLevel string/slog.Level}`; `config.Parse(args []string, getenv func(string) string, errOut io.Writer) (Config, error)`; `(Config).DBPath() string`; `(Config).BackupDir() string`. Package-level `var version string` in `main`, set via ldflags.

- [ ] **Step 1: Initialise the module**

```bash
cd /Users/iliaf/src/WEB/on-suite
go mod init github.com/iliafrenkel/on-suite
```

- [ ] **Step 2: Write the config package**

Create `internal/platform/config/config.go`:

```go
// Package config turns command-line flags and ONSUITE_* environment
// variables into a Config. It owns every default value in the system.
package config

import (
	"flag"
	"fmt"
	"io"
	"log/slog"
	"path/filepath"
	"strings"
)

// Config is the complete runtime configuration of the server.
type Config struct {
	Addr      string     // listen address, e.g. ":8080"
	DataDir   string     // holds onsuite.db and backups/
	TLSDomain string     // non-empty enables built-in Let's Encrypt
	LogLevel  slog.Level // parsed from a name, not a number
}

// Parse resolves configuration in the order: flag, then environment, then
// default. getenv may be nil, in which case only flags and defaults apply.
func Parse(args []string, getenv func(string) string, errOut io.Writer) (Config, error) {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	fs.SetOutput(errOut)

	var (
		c     Config
		level string
	)
	fs.StringVar(&c.Addr, "addr", envOr(getenv, "ONSUITE_ADDR", ":8080"),
		"address to listen on")
	fs.StringVar(&c.DataDir, "data-dir", envOr(getenv, "ONSUITE_DATA_DIR", "./data"),
		"directory holding the database and backups")
	fs.StringVar(&c.TLSDomain, "tls-domain", envOr(getenv, "ONSUITE_TLS_DOMAIN", ""),
		"obtain a Let's Encrypt certificate for this domain and serve HTTPS directly")
	fs.StringVar(&level, "log-level", envOr(getenv, "ONSUITE_LOG_LEVEL", "info"),
		"debug, info, warn or error")

	if err := fs.Parse(args); err != nil {
		return Config{}, err
	}
	if strings.TrimSpace(c.DataDir) == "" {
		return Config{}, fmt.Errorf("data-dir must not be empty")
	}
	lvl, err := parseLevel(level)
	if err != nil {
		return Config{}, err
	}
	c.LogLevel = lvl
	return c, nil
}

// DBPath is the single SQLite file holding all suite data.
func (c Config) DBPath() string { return filepath.Join(c.DataDir, "onsuite.db") }

// BackupDir is where snapshots are written.
func (c Config) BackupDir() string { return filepath.Join(c.DataDir, "backups") }

func envOr(getenv func(string) string, key, def string) string {
	if getenv == nil {
		return def
	}
	if v := getenv(key); v != "" {
		return v
	}
	return def
}

func parseLevel(s string) (slog.Level, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "debug":
		return slog.LevelDebug, nil
	case "info":
		return slog.LevelInfo, nil
	case "warn":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	}
	return 0, fmt.Errorf("invalid log level %q: want debug, info, warn or error", s)
}
```

- [ ] **Step 3: Write the command dispatcher**

Create `cmd/onsuite/main.go`:

```go
// Command onsuite is the single binary serving the whole ON Suite.
package main

import (
	"errors"
	"fmt"
	"io"
	"os"
)

// version is overwritten at build time with -ldflags "-X main.version=v1.2.3".
var version = "dev"

func main() {
	if err := run(os.Args[1:], os.Getenv, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "onsuite:", err)
		os.Exit(1)
	}
}

func run(args []string, getenv func(string) string, errOut io.Writer) error {
	if len(args) == 0 {
		usage(errOut)
		return errors.New("no command given")
	}
	switch args[0] {
	case "serve":
		return serve(args[1:], getenv, errOut)
	case "version":
		fmt.Println("onsuite", version)
		return nil
	case "help", "-h", "--help":
		usage(errOut)
		return nil
	default:
		usage(errOut)
		return fmt.Errorf("unknown command %q", args[0])
	}
}

func usage(w io.Writer) {
	fmt.Fprint(w, `onsuite — the ON Suite server

Usage:
  onsuite serve [flags]     run the server
  onsuite version           print the build version
  onsuite help              show this message

Run "onsuite serve -h" for serve flags.
`)
}
```

- [ ] **Step 4: Write the serve command**

Create `cmd/onsuite/serve.go`:

```go
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/iliafrenkel/on-suite/internal/platform/config"
)

func serve(args []string, getenv func(string) string, errOut io.Writer) error {
	cfg, err := config.Parse(args, getenv, errOut)
	if err != nil {
		return err
	}
	log := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: cfg.LogLevel}))

	if err := os.MkdirAll(cfg.DataDir, 0o750); err != nil {
		return fmt.Errorf("create data dir %s: %w", cfg.DataDir, err)
	}

	mux := http.NewServeMux()
	mux.Handle("GET /healthz", healthzHandler(version, nil))

	srv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	return listenAndServe(context.Background(), srv, log)
}

// listenAndServe runs srv until SIGINT or SIGTERM, then drains in-flight
// requests. It is separated from serve so tests can drive it directly.
func listenAndServe(parent context.Context, srv *http.Server, log *slog.Logger) error {
	ctx, stop := signal.NotifyContext(parent, os.Interrupt, syscall.SIGTERM)
	defer stop()

	errCh := make(chan error, 1)
	go func() {
		log.Info("listening", "addr", srv.Addr, "version", version)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
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
	if err := srv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("shutdown: %w", err)
	}
	log.Info("stopped cleanly")
	return nil
}

// pinger is satisfied by *sql.DB. Task 2 passes a real database in; until
// then healthz reports the process is up and nothing more.
type pinger interface {
	PingContext(context.Context) error
}

func healthzHandler(version string, db pinger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		status, code := "ok", http.StatusOK
		if db != nil {
			ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
			defer cancel()
			if err := db.PingContext(ctx); err != nil {
				status, code = "database unavailable", http.StatusServiceUnavailable
			}
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(code)
		fmt.Fprintf(w, "{\"status\":%q,\"version\":%q}\n", status, version)
	})
}
```

- [ ] **Step 5: Test the config package**

Create `internal/platform/config/config_test.go`:

```go
package config

import (
	"io"
	"log/slog"
	"path/filepath"
	"testing"
)

func TestParsePrecedence(t *testing.T) {
	env := map[string]string{"ONSUITE_ADDR": ":9999", "ONSUITE_LOG_LEVEL": "warn"}
	getenv := func(k string) string { return env[k] }

	tests := []struct {
		name      string
		args      []string
		wantAddr  string
		wantLevel slog.Level
	}{
		{"env used when no flag", nil, ":9999", slog.LevelWarn},
		{"flag beats env", []string{"-addr", ":7000"}, ":7000", slog.LevelWarn},
		{"flag beats env for level", []string{"-log-level", "debug"}, ":9999", slog.LevelDebug},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, err := Parse(tt.args, getenv, io.Discard)
			if err != nil {
				t.Fatalf("Parse: %v", err)
			}
			if c.Addr != tt.wantAddr {
				t.Errorf("Addr = %q, want %q", c.Addr, tt.wantAddr)
			}
			if c.LogLevel != tt.wantLevel {
				t.Errorf("LogLevel = %v, want %v", c.LogLevel, tt.wantLevel)
			}
		})
	}
}

func TestParseDefaults(t *testing.T) {
	c, err := Parse(nil, nil, io.Discard)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if c.Addr != ":8080" {
		t.Errorf("Addr = %q, want :8080", c.Addr)
	}
	if c.DataDir != "./data" {
		t.Errorf("DataDir = %q, want ./data", c.DataDir)
	}
	if c.TLSDomain != "" {
		t.Errorf("TLSDomain = %q, want empty", c.TLSDomain)
	}
	if c.LogLevel != slog.LevelInfo {
		t.Errorf("LogLevel = %v, want info", c.LogLevel)
	}
}

func TestParseRejectsBadInput(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{"unknown log level", []string{"-log-level", "verbose"}},
		{"empty data dir", []string{"-data-dir", ""}},
		{"unknown flag", []string{"-nope"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := Parse(tt.args, nil, io.Discard); err == nil {
				t.Fatal("Parse succeeded, want error")
			}
		})
	}
}

func TestDerivedPaths(t *testing.T) {
	c := Config{DataDir: "/var/lib/onsuite"}
	if got, want := c.DBPath(), filepath.FromSlash("/var/lib/onsuite/onsuite.db"); got != want {
		t.Errorf("DBPath() = %q, want %q", got, want)
	}
	if got, want := c.BackupDir(), filepath.FromSlash("/var/lib/onsuite/backups"); got != want {
		t.Errorf("BackupDir() = %q, want %q", got, want)
	}
}
```

- [ ] **Step 6: Test healthz and graceful shutdown**

Create `cmd/onsuite/serve_test.go`:

```go
package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestHealthzWithoutDatabase(t *testing.T) {
	rec := httptest.NewRecorder()
	healthzHandler("v1.2.3", nil).ServeHTTP(rec, httptest.NewRequest("GET", "/healthz", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var got struct{ Status, Version string }
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("body is not JSON: %v (%q)", err, rec.Body.String())
	}
	if got.Status != "ok" || got.Version != "v1.2.3" {
		t.Errorf("got %+v, want status ok and version v1.2.3", got)
	}
}

type failingPinger struct{}

func (failingPinger) PingContext(context.Context) error { return errors.New("boom") }

func TestHealthzReportsDatabaseFailure(t *testing.T) {
	rec := httptest.NewRecorder()
	healthzHandler("dev", failingPinger{}).ServeHTTP(rec, httptest.NewRequest("GET", "/healthz", nil))

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
}

// TestListenAndServeDrainsInFlightRequests proves shutdown waits for a
// request that is already running rather than cutting it off.
func TestListenAndServeDrainsInFlightRequests(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	if err := ln.Close(); err != nil {
		t.Fatal(err)
	}

	started := make(chan struct{})
	mux := http.NewServeMux()
	mux.HandleFunc("GET /slow", func(w http.ResponseWriter, r *http.Request) {
		close(started)
		time.Sleep(300 * time.Millisecond)
		_, _ = io.WriteString(w, "finished")
	})

	srv := &http.Server{Addr: addr, Handler: mux}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- listenAndServe(ctx, srv, slog.New(slog.DiscardHandler))
	}()

	var resp *http.Response
	respCh := make(chan error, 1)
	go func() {
		var err error
		for range 50 { // wait for the listener to come up
			resp, err = http.Get("http://" + addr + "/slow")
			if err == nil {
				break
			}
			time.Sleep(20 * time.Millisecond)
		}
		respCh <- err
	}()

	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("handler never started")
	}
	cancel() // simulate SIGTERM arriving mid-request

	if err := <-respCh; err != nil {
		t.Fatalf("request failed instead of draining: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if string(body) != "finished" {
		t.Errorf("body = %q, want %q", body, "finished")
	}
	if err := <-done; err != nil {
		t.Errorf("listenAndServe returned %v, want nil", err)
	}
}
```

- [ ] **Step 7: Run the tests**

```bash
go test ./... -race -v -run 'TestParse|TestDerived|TestHealthz|TestListenAndServe'
```

Expected: all PASS. `slog.DiscardHandler` requires Go 1.24+; on this Go 1.26 toolchain it exists.

- [ ] **Step 8: Verify it runs and shuts down by hand**

```bash
go run ./cmd/onsuite serve --data-dir /tmp/onsuite-dev --log-level debug
```

Expected: a JSON log line `{"level":"INFO","msg":"listening","addr":":8080",...}`. In another shell, `curl -s localhost:8080/healthz` returns `{"status":"ok","version":"dev"}`. Ctrl-C logs `shutdown signal received, draining` then `stopped cleanly` and exits 0.

- [ ] **Step 9: Verify the build constraint holds**

```bash
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -o /dev/null ./cmd/onsuite
```

Expected: no output, exit 0.

- [ ] **Step 10: Commit**

```bash
git add go.mod cmd internal
git commit -m "Add module skeleton, configuration, and serve command

Configuration resolves flag, then ONSUITE_* environment, then default,
with all defaults owned by one package. The serve command starts an HTTP
server exposing /healthz and drains in-flight requests on SIGTERM."
```

---
# Phase 2 — Storage

## Task 2: Open SQLite with the pragmas the suite depends on

**Files:**
- Create: `internal/platform/db/db.go`
- Modify: `cmd/onsuite/serve.go` (open the database, pass it to healthz, checkpoint on shutdown)
- Test: `internal/platform/db/db_test.go`

**Interfaces:**
- Consumes: `config.Config.DBPath()` from Task 1.
- Produces: `db.Open(path string) (*sql.DB, error)`; `db.Checkpoint(ctx context.Context, handle *sql.DB) error`; `db.BackupTo(ctx context.Context, handle *sql.DB, dest string) error`.

**Verified behaviour** (probed on this toolchain before writing this plan, do not re-litigate):
- `_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)` in the DSN sets all three; reading them back confirms `wal`, `5000`, `1`.
- `PRAGMA wal_checkpoint(TRUNCATE)` returns one row of three columns: `busy`, `log`, `checkpointed`.
- `VACUUM INTO ?` accepts a bound parameter and **fails** with "output file already exists" rather than overwriting.

- [ ] **Step 1: Add the dependency**

```bash
go get modernc.org/sqlite@v1.56.0
```

- [ ] **Step 2: Write the db package**

Create `internal/platform/db/db.go`:

```go
// Package db opens and maintains the single SQLite database backing the
// whole suite. It contains no application schema; migrations own that.
package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite" // registers the "sqlite" driver; pure Go, no CGO
)

// pragmas are applied per connection by the driver, via the DSN.
//
//	journal_mode(WAL)  readers never block the writer
//	busy_timeout(5000) wait up to 5s for a lock rather than failing at once
//	foreign_keys(1)    SQLite leaves FK enforcement OFF by default
const pragmas = "_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)"

// Open opens the database at path, creating the file and its parent
// directory if they do not exist.
//
// MaxOpenConns(1) serialises every statement in the process. At ON Suite's
// scale the throughput cost is unmeasurable, and it eliminates
// "database is locked" as a class of failure. See spec §6.1: if writes ever
// become a bottleneck the fix is a second, read-only pool, which is a change
// contained entirely within this package.
func Open(path string) (*sql.DB, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return nil, fmt.Errorf("create database directory: %w", err)
	}

	handle, err := sql.Open("sqlite", "file:"+path+"?"+pragmas)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	handle.SetMaxOpenConns(1)
	handle.SetMaxIdleConns(1)
	handle.SetConnMaxLifetime(0) // a single long-lived connection is what we want

	// sql.Open is lazy, so nothing above has touched the file yet. Ping forces
	// it, turning a bad path into an error here instead of at first query.
	if err := handle.Ping(); err != nil {
		_ = handle.Close()
		return nil, fmt.Errorf("ping %s: %w", path, err)
	}
	return handle, nil
}

// Checkpoint folds the write-ahead log back into the main database file and
// truncates it. Call it on shutdown, after writes have stopped, so the data
// directory is left in a tidy state for backup or copy.
func Checkpoint(ctx context.Context, handle *sql.DB) error {
	var busy, logFrames, checkpointed int
	err := handle.QueryRowContext(ctx, "PRAGMA wal_checkpoint(TRUNCATE)").
		Scan(&busy, &logFrames, &checkpointed)
	if err != nil {
		return fmt.Errorf("wal checkpoint: %w", err)
	}
	if busy != 0 {
		return errors.New("wal checkpoint blocked by an active reader")
	}
	return nil
}

// BackupTo writes a consistent snapshot to dest while the database remains
// open and writable.
//
// Copying the file with cp, or with the sqlite3 CLI, is not safe against a
// live writer. VACUUM INTO is: SQLite builds the copy inside a read
// transaction. dest must not already exist.
func BackupTo(ctx context.Context, handle *sql.DB, dest string) error {
	if _, err := os.Stat(dest); err == nil {
		return fmt.Errorf("backup destination %s already exists", dest)
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("stat %s: %w", dest, err)
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o750); err != nil {
		return fmt.Errorf("create backup directory: %w", err)
	}
	if _, err := handle.ExecContext(ctx, "VACUUM INTO ?", dest); err != nil {
		return fmt.Errorf("vacuum into %s: %w", dest, err)
	}
	return nil
}
```

- [ ] **Step 3: Test it, against a real file**

Create `internal/platform/db/db_test.go`:

```go
package db

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
)

// open is the helper every test in this package uses. A real file in a real
// temp directory, never :memory: — WAL mode and VACUUM INTO behave
// differently in memory, so an in-memory test would prove nothing.
func open(t *testing.T) (*sql.DB, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.db")
	handle, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = handle.Close() })
	return handle, path
}

func TestOpenAppliesPragmas(t *testing.T) {
	handle, _ := open(t)

	tests := []struct{ pragma, want string }{
		{"journal_mode", "wal"},
		{"busy_timeout", "5000"},
		{"foreign_keys", "1"},
	}
	for _, tt := range tests {
		var got string
		if err := handle.QueryRow("PRAGMA " + tt.pragma).Scan(&got); err != nil {
			t.Fatalf("read PRAGMA %s: %v", tt.pragma, err)
		}
		if got != tt.want {
			t.Errorf("PRAGMA %s = %q, want %q", tt.pragma, got, tt.want)
		}
	}
}

// TestForeignKeysAreEnforced guards the pragma that is silently off by
// default in SQLite. If this regresses, every ON DELETE CASCADE in the
// suite becomes a no-op and orphan rows accumulate unnoticed.
func TestForeignKeysAreEnforced(t *testing.T) {
	handle, _ := open(t)

	if _, err := handle.Exec(`
		CREATE TABLE parent (id INTEGER PRIMARY KEY);
		CREATE TABLE child (
			id  INTEGER PRIMARY KEY,
			pid INTEGER NOT NULL REFERENCES parent(id) ON DELETE CASCADE
		);
		INSERT INTO parent (id) VALUES (1);
	`); err != nil {
		t.Fatalf("setup: %v", err)
	}

	if _, err := handle.Exec("INSERT INTO child (id, pid) VALUES (1, 999)"); err == nil {
		t.Fatal("insert with a dangling foreign key succeeded, want rejection")
	}

	if _, err := handle.Exec("INSERT INTO child (id, pid) VALUES (2, 1)"); err != nil {
		t.Fatalf("valid insert rejected: %v", err)
	}
	if _, err := handle.Exec("DELETE FROM parent WHERE id = 1"); err != nil {
		t.Fatalf("delete parent: %v", err)
	}
	var children int
	if err := handle.QueryRow("SELECT count(*) FROM child").Scan(&children); err != nil {
		t.Fatal(err)
	}
	if children != 0 {
		t.Errorf("child rows after cascade = %d, want 0", children)
	}
}

func TestOpenRejectsUnusablePath(t *testing.T) {
	// A directory where a file must go: mkdir succeeds, opening cannot.
	dir := t.TempDir()
	if _, err := Open(filepath.Join(dir)); err == nil {
		t.Fatal("Open on a directory succeeded, want error")
	}
}

func TestCheckpoint(t *testing.T) {
	handle, _ := open(t)
	if _, err := handle.Exec("CREATE TABLE t (id INTEGER PRIMARY KEY)"); err != nil {
		t.Fatal(err)
	}
	if err := Checkpoint(context.Background(), handle); err != nil {
		t.Fatalf("Checkpoint: %v", err)
	}
}

func TestBackupToProducesAReadableCopy(t *testing.T) {
	handle, _ := open(t)
	if _, err := handle.Exec(`
		CREATE TABLE t (id INTEGER PRIMARY KEY, s TEXT NOT NULL);
		INSERT INTO t (s) VALUES ('original');
	`); err != nil {
		t.Fatal(err)
	}

	dest := filepath.Join(t.TempDir(), "snap", "backup.db")
	if err := BackupTo(context.Background(), handle, dest); err != nil {
		t.Fatalf("BackupTo: %v", err)
	}

	// The snapshot must be a working database, not just a file that exists.
	restored, err := Open(dest)
	if err != nil {
		t.Fatalf("reopen snapshot: %v", err)
	}
	defer func() { _ = restored.Close() }()

	var s string
	if err := restored.QueryRow("SELECT s FROM t").Scan(&s); err != nil {
		t.Fatalf("query snapshot: %v", err)
	}
	if s != "original" {
		t.Errorf("snapshot content = %q, want %q", s, "original")
	}

	// The source must still be writable afterwards — the whole point of
	// VACUUM INTO over a file copy is that it does not take the DB offline.
	if _, err := handle.Exec("INSERT INTO t (s) VALUES ('after backup')"); err != nil {
		t.Errorf("source not writable after backup: %v", err)
	}
}

func TestBackupToRefusesExistingDestination(t *testing.T) {
	handle, _ := open(t)
	if _, err := handle.Exec("CREATE TABLE t (id INTEGER PRIMARY KEY)"); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(t.TempDir(), "backup.db")

	if err := BackupTo(context.Background(), handle, dest); err != nil {
		t.Fatalf("first BackupTo: %v", err)
	}
	if err := BackupTo(context.Background(), handle, dest); err == nil {
		t.Fatal("second BackupTo overwrote an existing snapshot, want error")
	}
}
```

- [ ] **Step 4: Run the tests**

```bash
go test ./internal/platform/db/ -race -v
```

Expected: 6 tests PASS.

- [ ] **Step 5: Wire the database into serve**

In `cmd/onsuite/serve.go`, add `"github.com/iliafrenkel/on-suite/internal/platform/db"` to the imports and replace the body of `serve` between the `os.MkdirAll` call and the `return listenAndServe(...)` line with:

```go
	handle, err := db.Open(cfg.DBPath())
	if err != nil {
		return err
	}
	defer func() {
		if err := db.Checkpoint(context.Background(), handle); err != nil {
			log.Warn("wal checkpoint on shutdown failed", "error", err)
		}
		if err := handle.Close(); err != nil {
			log.Warn("closing database failed", "error", err)
		}
	}()
	log.Info("database ready", "path", cfg.DBPath())

	mux := http.NewServeMux()
	mux.Handle("GET /healthz", healthzHandler(version, handle))
```

The `defer` runs after `listenAndServe` returns, which is after in-flight requests have drained — so the checkpoint happens when nothing is writing, which is the only time `TRUNCATE` can fully succeed.

- [ ] **Step 6: Verify by hand that healthz now reflects the database**

```bash
go run ./cmd/onsuite serve --data-dir /tmp/onsuite-dev
```

Expected: log line `database ready`, `curl -s localhost:8080/healthz` still returns `{"status":"ok","version":"dev"}`, and `ls /tmp/onsuite-dev` shows `onsuite.db`. After Ctrl-C, `onsuite.db-wal` is gone or zero length.

- [ ] **Step 7: Commit**

```bash
git add go.mod go.sum internal/platform/db cmd/onsuite/serve.go
git commit -m "Open SQLite with WAL, busy timeout and foreign keys enforced

Serialises access with MaxOpenConns(1), which trades write concurrency
this deployment will never need for the elimination of lock contention
errors. Adds a VACUUM INTO snapshot helper, which unlike a file copy is
safe against a live writer, and a WAL checkpoint run once requests have
drained on shutdown."
```

---

## Task 3: Forward-only, namespaced migration runner

**Files:**
- Create: `internal/platform/db/migrate.go`
- Test: `internal/platform/db/migrate_test.go`

**Interfaces:**
- Consumes: `db.Open` from Task 2.
- Produces: `db.Migration{Namespace, ID, Name, SQL string}`; `(Migration).Key() string` returning `"<namespace>:<id>"`; `db.Collect(namespace string, fsys fs.FS) ([]Migration, error)`; `db.Apply(ctx context.Context, handle *sql.DB, ms []Migration) (applied int, err error)`.

**Design notes:**
- Each app owns its own migrations and they are recorded under its id, so `paste/migrations/0001_snippets.sql` is recorded as `paste:0001`. An unregistered app never touches the schema.
- Forward-only by design (spec §6.2). Recovery from a bad migration is restoring a snapshot, which is one file.
- Each migration runs inside its own transaction together with the row recording it, so a failure leaves no half-applied migration and no false record. SQLite runs DDL transactionally, so this genuinely works.

- [ ] **Step 1: Write the migration runner**

Create `internal/platform/db/migrate.go`:

```go
package db

import (
	"context"
	"database/sql"
	"fmt"
	"io/fs"
	"path"
	"regexp"
	"sort"
	"strings"
	"time"
)

// Migration is one forward-only schema change, owned by one namespace.
type Migration struct {
	Namespace string // "platform", or an app id such as "paste"
	ID        string // zero-padded ordinal, e.g. "0001"
	Name      string // human-readable slug from the filename
	SQL       string
}

// Key is how the migration is recorded in schema_migrations.
func (m Migration) Key() string { return m.Namespace + ":" + m.ID }

// filenamePattern matches "0001_identity.sql".
var filenamePattern = regexp.MustCompile(`^(\d{4})_([a-z0-9]+(?:_[a-z0-9]+)*)\.sql$`)

// Collect reads every .sql file at the root of fsys and returns them in
// apply order. Filenames must be NNNN_lower_snake_name.sql.
func Collect(namespace string, fsys fs.FS) ([]Migration, error) {
	if namespace == "" {
		return nil, fmt.Errorf("migration namespace must not be empty")
	}
	entries, err := fs.ReadDir(fsys, ".")
	if err != nil {
		return nil, fmt.Errorf("read migrations for %s: %w", namespace, err)
	}

	var out []Migration
	seen := make(map[string]string) // id -> filename, for duplicate detection
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if path.Ext(name) != ".sql" {
			return nil, fmt.Errorf("%s: unexpected non-SQL file %q in migrations", namespace, name)
		}
		m := filenamePattern.FindStringSubmatch(name)
		if m == nil {
			return nil, fmt.Errorf("%s: migration filename %q must look like 0001_some_name.sql", namespace, name)
		}
		if prev, dup := seen[m[1]]; dup {
			return nil, fmt.Errorf("%s: duplicate migration id %s in %q and %q", namespace, m[1], prev, name)
		}
		seen[m[1]] = name

		body, err := fs.ReadFile(fsys, name)
		if err != nil {
			return nil, fmt.Errorf("%s: read %s: %w", namespace, name, err)
		}
		if strings.TrimSpace(string(body)) == "" {
			return nil, fmt.Errorf("%s: migration %s is empty", namespace, name)
		}
		out = append(out, Migration{
			Namespace: namespace,
			ID:        m[1],
			Name:      m[2],
			SQL:       string(body),
		})
	}

	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

const createMigrationsTable = `
CREATE TABLE IF NOT EXISTS schema_migrations (
	key        TEXT PRIMARY KEY,
	namespace  TEXT NOT NULL,
	id         TEXT NOT NULL,
	name       TEXT NOT NULL,
	applied_at TEXT NOT NULL
) STRICT;`

// Apply runs every migration in ms that has not been applied before, in the
// order given, and returns how many ran.
//
// Each migration and the row recording it share one transaction: a migration
// either applies completely and is recorded, or does neither.
func Apply(ctx context.Context, handle *sql.DB, ms []Migration) (int, error) {
	if _, err := handle.ExecContext(ctx, createMigrationsTable); err != nil {
		return 0, fmt.Errorf("create schema_migrations: %w", err)
	}

	applied, err := appliedKeys(ctx, handle)
	if err != nil {
		return 0, err
	}

	count := 0
	for _, m := range ms {
		if _, done := applied[m.Key()]; done {
			continue
		}
		if err := applyOne(ctx, handle, m); err != nil {
			return count, err
		}
		count++
	}
	return count, nil
}

func appliedKeys(ctx context.Context, handle *sql.DB) (map[string]struct{}, error) {
	rows, err := handle.QueryContext(ctx, "SELECT key FROM schema_migrations")
	if err != nil {
		return nil, fmt.Errorf("read schema_migrations: %w", err)
	}
	defer func() { _ = rows.Close() }()

	keys := make(map[string]struct{})
	for rows.Next() {
		var k string
		if err := rows.Scan(&k); err != nil {
			return nil, fmt.Errorf("scan schema_migrations: %w", err)
		}
		keys[k] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate schema_migrations: %w", err)
	}
	return keys, nil
}

func applyOne(ctx context.Context, handle *sql.DB, m Migration) error {
	tx, err := handle.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin %s: %w", m.Key(), err)
	}
	defer func() { _ = tx.Rollback() }() // no-op once Commit has succeeded

	if _, err := tx.ExecContext(ctx, m.SQL); err != nil {
		return fmt.Errorf("apply %s (%s): %w", m.Key(), m.Name, err)
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO schema_migrations (key, namespace, id, name, applied_at)
		 VALUES (?, ?, ?, ?, ?)`,
		m.Key(), m.Namespace, m.ID, m.Name,
		time.Now().UTC().Format(time.RFC3339Nano),
	); err != nil {
		return fmt.Errorf("record %s: %w", m.Key(), err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit %s: %w", m.Key(), err)
	}
	return nil
}
```

- [ ] **Step 2: Test the runner**

Create `internal/platform/db/migrate_test.go`:

```go
package db

import (
	"context"
	"strings"
	"testing"
	"testing/fstest"
)

func TestCollectOrdersAndParses(t *testing.T) {
	fsys := fstest.MapFS{
		"0002_second.sql":      {Data: []byte("CREATE TABLE b (id INTEGER PRIMARY KEY);")},
		"0001_first_thing.sql": {Data: []byte("CREATE TABLE a (id INTEGER PRIMARY KEY);")},
		"0010_tenth.sql":       {Data: []byte("CREATE TABLE c (id INTEGER PRIMARY KEY);")},
	}
	got, err := Collect("paste", fsys)
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	wantIDs := []string{"0001", "0002", "0010"}
	if len(got) != len(wantIDs) {
		t.Fatalf("got %d migrations, want %d", len(got), len(wantIDs))
	}
	for i, want := range wantIDs {
		if got[i].ID != want {
			t.Errorf("migration %d id = %q, want %q", i, got[i].ID, want)
		}
		if got[i].Namespace != "paste" {
			t.Errorf("migration %d namespace = %q, want paste", i, got[i].Namespace)
		}
	}
	if got[0].Name != "first_thing" {
		t.Errorf("name = %q, want first_thing", got[0].Name)
	}
	if got[0].Key() != "paste:0001" {
		t.Errorf("Key() = %q, want paste:0001", got[0].Key())
	}
}

func TestCollectRejectsBadInput(t *testing.T) {
	tests := []struct {
		name string
		fsys fstest.MapFS
	}{
		{"unnumbered filename", fstest.MapFS{"init.sql": {Data: []byte("SELECT 1;")}}},
		{"too few digits", fstest.MapFS{"1_a.sql": {Data: []byte("SELECT 1;")}}},
		{"uppercase name", fstest.MapFS{"0001_Thing.sql": {Data: []byte("SELECT 1;")}}},
		{"non-sql file", fstest.MapFS{"notes.txt": {Data: []byte("hi")}}},
		{"duplicate id", fstest.MapFS{
			"0001_a.sql": {Data: []byte("SELECT 1;")},
			"0001_b.sql": {Data: []byte("SELECT 1;")},
		}},
		{"empty migration", fstest.MapFS{"0001_a.sql": {Data: []byte("   \n")}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := Collect("platform", tt.fsys); err == nil {
				t.Fatal("Collect succeeded, want error")
			}
		})
	}
}

func TestApplyIsIdempotent(t *testing.T) {
	handle, _ := open(t)
	ctx := context.Background()
	ms := []Migration{
		{Namespace: "platform", ID: "0001", Name: "first",
			SQL: "CREATE TABLE a (id INTEGER PRIMARY KEY);"},
		{Namespace: "platform", ID: "0002", Name: "second",
			SQL: "CREATE TABLE b (id INTEGER PRIMARY KEY);"},
	}

	n, err := Apply(ctx, handle, ms)
	if err != nil {
		t.Fatalf("first Apply: %v", err)
	}
	if n != 2 {
		t.Fatalf("first Apply ran %d migrations, want 2", n)
	}

	// Running the same set again must be a no-op, not an error.
	n, err = Apply(ctx, handle, ms)
	if err != nil {
		t.Fatalf("second Apply: %v", err)
	}
	if n != 0 {
		t.Errorf("second Apply ran %d migrations, want 0", n)
	}

	// A newly added migration runs on its own.
	ms = append(ms, Migration{Namespace: "platform", ID: "0003", Name: "third",
		SQL: "CREATE TABLE c (id INTEGER PRIMARY KEY);"})
	n, err = Apply(ctx, handle, ms)
	if err != nil {
		t.Fatalf("third Apply: %v", err)
	}
	if n != 1 {
		t.Errorf("third Apply ran %d migrations, want 1", n)
	}
}

// TestApplyNamespacesAreIndependent proves two apps can both own a 0001
// without colliding — the property that lets each app ship its own
// migrations without coordinating numbering.
func TestApplyNamespacesAreIndependent(t *testing.T) {
	handle, _ := open(t)
	ctx := context.Background()

	n, err := Apply(ctx, handle, []Migration{
		{Namespace: "paste", ID: "0001", Name: "snippets",
			SQL: "CREATE TABLE paste_snippets (id INTEGER PRIMARY KEY);"},
		{Namespace: "notes", ID: "0001", Name: "nodes",
			SQL: "CREATE TABLE notes_nodes (id INTEGER PRIMARY KEY);"},
	})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if n != 2 {
		t.Errorf("applied %d, want 2", n)
	}

	var keys int
	if err := handle.QueryRow(
		"SELECT count(*) FROM schema_migrations WHERE id = '0001'").Scan(&keys); err != nil {
		t.Fatal(err)
	}
	if keys != 2 {
		t.Errorf("recorded 0001 rows = %d, want 2", keys)
	}
}

// TestApplyRollsBackAFailedMigration is the important one: a broken
// migration must leave neither partial schema nor a record claiming success,
// so that fixing the file and rerunning works.
func TestApplyRollsBackAFailedMigration(t *testing.T) {
	handle, _ := open(t)
	ctx := context.Background()

	_, err := Apply(ctx, handle, []Migration{
		{Namespace: "platform", ID: "0001", Name: "good",
			SQL: "CREATE TABLE good (id INTEGER PRIMARY KEY);"},
		{Namespace: "platform", ID: "0002", Name: "broken",
			SQL: `CREATE TABLE half (id INTEGER PRIMARY KEY);
			      CREATE TABLE half (id INTEGER PRIMARY KEY);`}, // duplicate table
	})
	if err == nil {
		t.Fatal("Apply succeeded on a broken migration, want error")
	}
	if !strings.Contains(err.Error(), "platform:0002") {
		t.Errorf("error %q does not name the failing migration", err)
	}

	// The good migration stays applied.
	var good int
	if err := handle.QueryRow(
		"SELECT count(*) FROM schema_migrations WHERE key = 'platform:0001'").Scan(&good); err != nil {
		t.Fatal(err)
	}
	if good != 1 {
		t.Errorf("platform:0001 recorded %d times, want 1", good)
	}

	// The broken one is neither recorded nor partially applied.
	var bad int
	if err := handle.QueryRow(
		"SELECT count(*) FROM schema_migrations WHERE key = 'platform:0002'").Scan(&bad); err != nil {
		t.Fatal(err)
	}
	if bad != 0 {
		t.Errorf("broken migration recorded %d times, want 0", bad)
	}

	var halfTables int
	if err := handle.QueryRow(
		"SELECT count(*) FROM sqlite_master WHERE type='table' AND name='half'").Scan(&halfTables); err != nil {
		t.Fatal(err)
	}
	if halfTables != 0 {
		t.Errorf("table 'half' exists after rollback, want it absent")
	}
}
```

- [ ] **Step 3: Run the tests**

```bash
go test ./internal/platform/db/ -race -v -run TestCollect -run TestApply
```

Then the whole package:

```bash
go test ./internal/platform/db/ -race
```

Expected: PASS. If `TestApplyRollsBackAFailedMigration` fails, stop — a migration runner that can half-apply is worse than none.

- [ ] **Step 4: Commit**

```bash
git add internal/platform/db
git commit -m "Add forward-only namespaced migration runner

Migrations are collected from an embedded FS per namespace, so each app
ships and numbers its own without coordinating with other apps, and an
unregistered app never touches the schema. Each migration commits together
with the row recording it, so a failure leaves neither partial schema nor a
record claiming success."
```

---
# Phase 3 — Identity

> **Tasks 4 and 6 are strict TDD.** Write the test, run it, watch it fail with the error the plan predicts, then implement. This is the one exception to the project's tests-after policy, and it exists because the sad paths here — malformed hash, expired session, tampered cookie — are exactly the ones that never get written if deferred.

## Task 4: Argon2id password hashing

**Files:**
- Create: `internal/platform/auth/password.go`
- Test: `internal/platform/auth/password_test.go`

**Interfaces:**
- Consumes: nothing. This package knows nothing about storage or HTTP.
- Produces: `auth.HashPassword(plain string) (string, error)` returning a PHC-format string; `auth.VerifyPassword(encoded, plain string) (bool, error)`; `auth.ErrMalformedHash error`; `auth.MinPasswordLength = 12`; `auth.ValidatePassword(plain string) error`.

**Verified behaviour** (this exact implementation was run before the plan was written):
- Output looks like `$argon2id$v=19$m=65536,t=3,p=4$Lns9yAV31TWqeVxz5X/ZXw$HX08uO62wz0M1WIwf2PFr5FqaA8PacG8CTqrmXQiYzQ` — 97 characters with these parameters.
- Two hashes of the same password differ, because the salt is random.
- **An empty password hashes and verifies successfully.** Length policy is therefore a separate concern, which is why `ValidatePassword` exists as its own function rather than being smuggled into `HashPassword`.

- [ ] **Step 1: Write the failing test**

Create `internal/platform/auth/password_test.go`:

```go
package auth

import (
	"errors"
	"strings"
	"testing"
)

func TestHashPasswordRoundTrip(t *testing.T) {
	const pw = "correct horse battery staple"

	encoded, err := HashPassword(pw)
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	if !strings.HasPrefix(encoded, "$argon2id$v=19$") {
		t.Errorf("encoded = %q, want an argon2id PHC string", encoded)
	}
	if strings.Contains(encoded, pw) {
		t.Fatal("encoded hash contains the plaintext password")
	}

	ok, err := VerifyPassword(encoded, pw)
	if err != nil {
		t.Fatalf("VerifyPassword: %v", err)
	}
	if !ok {
		t.Error("correct password did not verify")
	}

	ok, err = VerifyPassword(encoded, "wrong password entirely")
	if err != nil {
		t.Fatalf("VerifyPassword on wrong password returned an error: %v", err)
	}
	if ok {
		t.Error("wrong password verified")
	}
}

// TestHashPasswordUsesARandomSalt guards against the classic mistake of a
// fixed salt, which would make the hashes rainbow-table-able.
func TestHashPasswordUsesARandomSalt(t *testing.T) {
	a, err := HashPassword("same password")
	if err != nil {
		t.Fatal(err)
	}
	b, err := HashPassword("same password")
	if err != nil {
		t.Fatal(err)
	}
	if a == b {
		t.Fatal("two hashes of the same password are identical, salt is not random")
	}
}

func TestHashPasswordHandlesAwkwardInput(t *testing.T) {
	tests := []struct{ name, pw string }{
		{"empty", ""},
		{"unicode", "пароль-סיסמה-🔐"},
		{"very long", strings.Repeat("x", 4096)},
		{"contains dollar signs", "a$b$c$argon2id$"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			encoded, err := HashPassword(tt.pw)
			if err != nil {
				t.Fatalf("HashPassword: %v", err)
			}
			ok, err := VerifyPassword(encoded, tt.pw)
			if err != nil {
				t.Fatalf("VerifyPassword: %v", err)
			}
			if !ok {
				t.Error("password did not verify after round trip")
			}
		})
	}
}

// TestVerifyPasswordRejectsMalformedHashes matters because these values come
// out of the database. A corrupted or hand-edited row must produce an error,
// never a successful login.
func TestVerifyPasswordRejectsMalformedHashes(t *testing.T) {
	tests := []struct{ name, encoded string }{
		{"empty", ""},
		{"not a phc string", "notahash"},
		{"wrong variant", "$argon2i$v=19$m=65536,t=3,p=4$c2FsdA$aGFzaA"},
		{"unsupported version", "$argon2id$v=99$m=65536,t=3,p=4$c2FsdA$aGFzaA"},
		{"bad base64 salt", "$argon2id$v=19$m=65536,t=3,p=4$!!!$aGFzaA"},
		{"zero parameters", "$argon2id$v=19$m=0,t=0,p=0$c2FsdA$aGFzaA"},
		{"missing fields", "$argon2id$v=19$m=65536,t=3,p=4"},
		{"bcrypt hash", "$2y$10$abcdefghijklmnopqrstuv"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ok, err := VerifyPassword(tt.encoded, "any password")
			if ok {
				t.Fatal("malformed hash verified successfully")
			}
			if !errors.Is(err, ErrMalformedHash) {
				t.Errorf("err = %v, want ErrMalformedHash", err)
			}
		})
	}
}

func TestValidatePassword(t *testing.T) {
	tests := []struct {
		name    string
		pw      string
		wantErr bool
	}{
		{"long enough", strings.Repeat("a", MinPasswordLength), false},
		{"one short", strings.Repeat("a", MinPasswordLength-1), true},
		{"empty", "", true},
		{"whitespace only", strings.Repeat(" ", MinPasswordLength+5), true},
		{"unicode counted in runes not bytes", strings.Repeat("é", MinPasswordLength), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidatePassword(tt.pw)
			if tt.wantErr && err == nil {
				t.Error("ValidatePassword accepted an invalid password")
			}
			if !tt.wantErr && err != nil {
				t.Errorf("ValidatePassword rejected a valid password: %v", err)
			}
		})
	}
}
```

- [ ] **Step 2: Run the test and watch it fail**

```bash
go test ./internal/platform/auth/ -run TestHashPassword -v
```

Expected: FAIL to build, with `undefined: HashPassword`, `undefined: VerifyPassword`, `undefined: ErrMalformedHash`, `undefined: MinPasswordLength`, `undefined: ValidatePassword`.

- [ ] **Step 3: Add the dependency and implement**

```bash
go get golang.org/x/crypto@v0.55.0
```

Create `internal/platform/auth/password.go`:

```go
// Package auth owns user identity: password hashing, the users and sessions
// tables, and the HTTP glue that turns a cookie into a known user.
//
// password.go is the innermost layer and deliberately knows nothing about
// storage or HTTP, so it can be tested in isolation.
package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"golang.org/x/crypto/argon2"
)

// MinPasswordLength is deliberately generous rather than clever: for a
// handful of trusted users, length beats composition rules.
const MinPasswordLength = 12

// ErrMalformedHash means a stored hash could not be parsed. Because stored
// hashes come from the database, this is treated as an error rather than as a
// failed login — it signals corruption, not a wrong password.
var ErrMalformedHash = errors.New("auth: malformed password hash")

// hashParams are the Argon2id cost parameters used for new hashes. Existing
// hashes carry their own parameters in their PHC string, so these can be
// raised later without invalidating anything already stored.
type hashParams struct {
	memory  uint32 // KiB
	time    uint32 // iterations
	threads uint8
	keyLen  uint32
	saltLen uint32
}

var defaultHashParams = hashParams{
	memory:  64 * 1024, // 64 MiB
	time:    3,
	threads: 4,
	keyLen:  32,
	saltLen: 16,
}

// ValidatePassword enforces policy on a new password. It is separate from
// HashPassword on purpose: HashPassword will faithfully hash an empty string,
// and the decision about what is acceptable belongs to the caller creating an
// account, not to the hashing primitive.
func ValidatePassword(plain string) error {
	if strings.TrimSpace(plain) == "" {
		return fmt.Errorf("password must not be blank")
	}
	if utf8.RuneCountInString(plain) < MinPasswordLength {
		return fmt.Errorf("password must be at least %d characters", MinPasswordLength)
	}
	return nil
}

// HashPassword returns a PHC-format Argon2id hash, e.g.
//
//	$argon2id$v=19$m=65536,t=3,p=4$<salt>$<key>
//
// The parameters travel with the hash, so VerifyPassword keeps working when
// defaultHashParams changes.
func HashPassword(plain string) (string, error) {
	p := defaultHashParams

	salt := make([]byte, p.saltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("auth: read salt: %w", err)
	}

	key := argon2.IDKey([]byte(plain), salt, p.time, p.memory, p.threads, p.keyLen)

	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, p.memory, p.time, p.threads,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(key),
	), nil
}

// VerifyPassword reports whether plain matches encoded.
//
// A false return with a nil error means "wrong password". A non-nil error
// means the stored hash could not be used at all.
func VerifyPassword(encoded, plain string) (bool, error) {
	// A PHC string starts with '$', so Split yields an empty first element.
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[0] != "" || parts[1] != "argon2id" {
		return false, ErrMalformedHash
	}

	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil {
		return false, ErrMalformedHash
	}
	if version != argon2.Version {
		return false, fmt.Errorf("%w: unsupported version %d", ErrMalformedHash, version)
	}

	var p hashParams
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &p.memory, &p.time, &p.threads); err != nil {
		return false, ErrMalformedHash
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return false, ErrMalformedHash
	}
	want, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil {
		return false, ErrMalformedHash
	}
	if len(salt) == 0 || len(want) == 0 || p.memory == 0 || p.time == 0 || p.threads == 0 {
		return false, ErrMalformedHash
	}

	got := argon2.IDKey([]byte(plain), salt, p.time, p.memory, p.threads, uint32(len(want)))

	// Constant time, so a timing measurement cannot reveal how much of the
	// hash matched.
	return subtle.ConstantTimeCompare(got, want) == 1, nil
}
```

- [ ] **Step 4: Run the tests and watch them pass**

```bash
go test ./internal/platform/auth/ -race -v
```

Expected: all PASS. These tests each run Argon2id at 64 MiB several times, so the package takes a second or two. That is the cost working correctly, not a problem to optimise away.

- [ ] **Step 5: Commit**

```bash
git add go.mod go.sum internal/platform/auth
git commit -m "Add Argon2id password hashing

Hashes are PHC-encoded so cost parameters travel with each hash and can be
raised later without invalidating stored credentials. Comparison is constant
time. A malformed stored hash is an error rather than a failed login, since
it indicates corruption rather than a wrong password, and length policy is a
separate function because the hashing primitive itself will faithfully hash
an empty string."
```

---

## Task 5: The identity schema and the user store

**Files:**
- Create: `internal/platform/auth/migrations/0001_identity.sql`
- Create: `internal/platform/auth/store.go`
- Test: `internal/platform/auth/store_test.go`

**Interfaces:**
- Consumes: `db.Open`, `db.Collect`, `db.Apply` from Tasks 2–3.
- Produces:
  - `auth.Namespace = "platform"` and `auth.Migrations() fs.FS`
  - `auth.User{ID int64; Username string; PasswordHash string; IsAdmin bool; CreatedAt time.Time}`
  - `auth.NewStore(handle *sql.DB) *Store`
  - `(*Store).CreateUser(ctx context.Context, username, passwordHash string, isAdmin bool) (User, error)`
  - `(*Store).UserByUsername(ctx context.Context, username string) (User, error)`
  - `(*Store).UserByID(ctx context.Context, id int64) (User, error)`
  - `(*Store).CountUsers(ctx context.Context) (int, error)`
  - `auth.ErrNotFound`, `auth.ErrDuplicateUsername`, `auth.ErrInvalidUsername`

**Verified behaviour:** `STRICT` tables, `AUTOINCREMENT`, `CHECK (is_admin IN (0,1))`, a `UNIQUE` index on the case-folded username, `RETURNING`, and `ON DELETE CASCADE` from `sessions` to `users` all work on this toolchain (SQLite 3.53.3).

- [ ] **Step 1: Write the migration**

Create `internal/platform/auth/migrations/0001_identity.sql`:

```sql
-- The platform's own schema: who exists, and who is currently logged in.
-- Every app's tables are prefixed with its id; these two are not, because
-- they belong to the platform rather than to any app.

CREATE TABLE users (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    -- username preserves the case the user chose, for display.
    username      TEXT    NOT NULL,
    -- username_fold is the lowercased form and carries the uniqueness
    -- constraint, so "Ilia" and "ilia" cannot both exist.
    username_fold TEXT    NOT NULL UNIQUE,
    password_hash TEXT    NOT NULL,
    is_admin      INTEGER NOT NULL DEFAULT 0 CHECK (is_admin IN (0, 1)),
    created_at    TEXT    NOT NULL
) STRICT;

CREATE TABLE sessions (
    id         TEXT    PRIMARY KEY,
    user_id    INTEGER NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    created_at TEXT    NOT NULL,
    expires_at TEXT    NOT NULL
) STRICT;

-- Expiry sweeps scan by expires_at; timestamps are RFC 3339 in UTC, which
-- sorts correctly as text.
CREATE INDEX sessions_expires_at_idx ON sessions (expires_at);
CREATE INDEX sessions_user_id_idx    ON sessions (user_id);
```

- [ ] **Step 2: Write the store**

Create `internal/platform/auth/store.go`:

```go
package auth

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"regexp"
	"strings"
	"time"
)

// Namespace is the migration namespace the platform's own schema is recorded
// under. Apps use their own id instead.
const Namespace = "platform"

//go:embed migrations/*.sql
var migrationFiles embed.FS

// Migrations returns the platform's schema, rooted so that filenames appear
// at the top level, matching what db.Collect expects.
func Migrations() fs.FS {
	sub, err := fs.Sub(migrationFiles, "migrations")
	if err != nil {
		// Unreachable: the path is a compile-time constant checked by go:embed.
		panic("auth: embedded migrations missing: " + err.Error())
	}
	return sub
}

var (
	ErrNotFound          = errors.New("auth: not found")
	ErrDuplicateUsername = errors.New("auth: username already taken")
	ErrInvalidUsername   = errors.New("auth: invalid username")
)

// usernamePattern keeps usernames URL-safe and unambiguous, which matters
// because they appear in log lines and in admin pages.
var usernamePattern = regexp.MustCompile(`^[a-zA-Z0-9]([a-zA-Z0-9._-]{1,30})[a-zA-Z0-9]$`)

// User is an account. PasswordHash is carried so the login path can verify
// it; nothing renders it.
type User struct {
	ID           int64
	Username     string
	PasswordHash string
	IsAdmin      bool
	CreatedAt    time.Time
}

// Store is all the SQL for identity. It has no HTTP knowledge, which is what
// lets it be tested against a real database with no server running.
type Store struct {
	db *sql.DB
}

func NewStore(handle *sql.DB) *Store { return &Store{db: handle} }

// ValidateUsername reports whether name is acceptable for a new account.
func ValidateUsername(name string) error {
	if !usernamePattern.MatchString(name) {
		return fmt.Errorf("%w: %q must be 3-32 characters of letters, digits, dot, dash or underscore, starting and ending alphanumeric", ErrInvalidUsername, name)
	}
	return nil
}

// fold is the canonical form used for uniqueness and lookup.
func fold(name string) string { return strings.ToLower(strings.TrimSpace(name)) }

// CreateUser inserts an account. passwordHash must already come from
// HashPassword; this function never sees a plaintext password.
func (s *Store) CreateUser(ctx context.Context, username, passwordHash string, isAdmin bool) (User, error) {
	username = strings.TrimSpace(username)
	if err := ValidateUsername(username); err != nil {
		return User{}, err
	}
	if passwordHash == "" {
		return User{}, errors.New("auth: password hash must not be empty")
	}

	now := time.Now().UTC()
	var (
		u         = User{Username: username, PasswordHash: passwordHash, IsAdmin: isAdmin}
		createdAt string
	)
	err := s.db.QueryRowContext(ctx,
		`INSERT INTO users (username, username_fold, password_hash, is_admin, created_at)
		 VALUES (?, ?, ?, ?, ?)
		 RETURNING id, created_at`,
		username, fold(username), passwordHash, boolToInt(isAdmin), formatTime(now),
	).Scan(&u.ID, &createdAt)
	if err != nil {
		if isUniqueViolation(err) {
			return User{}, fmt.Errorf("%w: %s", ErrDuplicateUsername, username)
		}
		return User{}, fmt.Errorf("auth: create user: %w", err)
	}

	u.CreatedAt, err = parseTime(createdAt)
	if err != nil {
		return User{}, err
	}
	return u, nil
}

// UserByUsername looks up an account case-insensitively.
func (s *Store) UserByUsername(ctx context.Context, username string) (User, error) {
	return s.scanUser(s.db.QueryRowContext(ctx,
		`SELECT id, username, password_hash, is_admin, created_at
		 FROM users WHERE username_fold = ?`, fold(username)))
}

func (s *Store) UserByID(ctx context.Context, id int64) (User, error) {
	return s.scanUser(s.db.QueryRowContext(ctx,
		`SELECT id, username, password_hash, is_admin, created_at
		 FROM users WHERE id = ?`, id))
}

// CountUsers lets the serve command warn when a database has no accounts,
// which is the one situation a fresh self-hoster is guaranteed to hit.
func (s *Store) CountUsers(ctx context.Context) (int, error) {
	var n int
	if err := s.db.QueryRowContext(ctx, "SELECT count(*) FROM users").Scan(&n); err != nil {
		return 0, fmt.Errorf("auth: count users: %w", err)
	}
	return n, nil
}

func (s *Store) scanUser(row *sql.Row) (User, error) {
	var (
		u         User
		isAdmin   int
		createdAt string
	)
	switch err := row.Scan(&u.ID, &u.Username, &u.PasswordHash, &isAdmin, &createdAt); {
	case errors.Is(err, sql.ErrNoRows):
		return User{}, ErrNotFound
	case err != nil:
		return User{}, fmt.Errorf("auth: scan user: %w", err)
	}
	u.IsAdmin = isAdmin == 1

	var err error
	if u.CreatedAt, err = parseTime(createdAt); err != nil {
		return User{}, err
	}
	return u, nil
}

// Timestamps are stored as RFC 3339 nanosecond strings in UTC, which sort
// lexically in the same order they sort chronologically.
func formatTime(t time.Time) string { return t.UTC().Format(time.RFC3339Nano) }

func parseTime(s string) (time.Time, error) {
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return time.Time{}, fmt.Errorf("auth: parse timestamp %q: %w", s, err)
	}
	return t.UTC(), nil
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// isUniqueViolation avoids importing the driver package just to read an error
// code. Matching on the message is unattractive but keeps this package free
// of a driver dependency, and the substring is stable in SQLite.
func isUniqueViolation(err error) bool {
	return err != nil && strings.Contains(err.Error(), "UNIQUE constraint failed")
}
```

- [ ] **Step 3: Test the store against a real database**

Create `internal/platform/auth/store_test.go`:

```go
package auth

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/iliafrenkel/on-suite/internal/platform/db"
)

// newStore gives each test its own migrated database on disk. Applying the
// real migration rather than hand-writing the schema means these tests also
// prove the migration is correct.
func newStore(t *testing.T) (*Store, *sql.DB) {
	t.Helper()

	handle, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { _ = handle.Close() })

	ms, err := db.Collect(Namespace, Migrations())
	if err != nil {
		t.Fatalf("db.Collect: %v", err)
	}
	if len(ms) == 0 {
		t.Fatal("no platform migrations were collected")
	}
	if _, err := db.Apply(context.Background(), handle, ms); err != nil {
		t.Fatalf("db.Apply: %v", err)
	}
	return NewStore(handle), handle
}

func TestCreateAndFetchUser(t *testing.T) {
	s, _ := newStore(t)
	ctx := context.Background()

	created, err := s.CreateUser(ctx, "Ilia", "$argon2id$fake", true)
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if created.ID == 0 {
		t.Error("CreateUser returned id 0")
	}
	if created.CreatedAt.IsZero() {
		t.Error("CreateUser returned a zero CreatedAt")
	}
	if !created.IsAdmin {
		t.Error("IsAdmin did not survive the round trip")
	}

	byID, err := s.UserByID(ctx, created.ID)
	if err != nil {
		t.Fatalf("UserByID: %v", err)
	}
	if byID.Username != "Ilia" {
		t.Errorf("Username = %q, want %q — display case must be preserved", byID.Username, "Ilia")
	}
	if byID.PasswordHash != "$argon2id$fake" {
		t.Errorf("PasswordHash = %q, want it preserved verbatim", byID.PasswordHash)
	}
	if !byID.CreatedAt.Equal(created.CreatedAt) {
		t.Errorf("CreatedAt = %v, want %v", byID.CreatedAt, created.CreatedAt)
	}
}

// TestUserLookupIsCaseInsensitive is the behaviour that stops a family member
// failing to log in because they capitalised their own name.
func TestUserLookupIsCaseInsensitive(t *testing.T) {
	s, _ := newStore(t)
	ctx := context.Background()

	if _, err := s.CreateUser(ctx, "Ilia", "$argon2id$fake", false); err != nil {
		t.Fatal(err)
	}
	for _, attempt := range []string{"Ilia", "ilia", "ILIA", "  iLiA  "} {
		if _, err := s.UserByUsername(ctx, attempt); err != nil {
			t.Errorf("UserByUsername(%q): %v", attempt, err)
		}
	}
}

func TestCreateUserRejectsDuplicateRegardlessOfCase(t *testing.T) {
	s, _ := newStore(t)
	ctx := context.Background()

	if _, err := s.CreateUser(ctx, "ilia", "$argon2id$fake", false); err != nil {
		t.Fatal(err)
	}
	_, err := s.CreateUser(ctx, "ILIA", "$argon2id$fake", false)
	if !errors.Is(err, ErrDuplicateUsername) {
		t.Fatalf("err = %v, want ErrDuplicateUsername", err)
	}
}

func TestCreateUserRejectsInvalidInput(t *testing.T) {
	s, _ := newStore(t)
	ctx := context.Background()

	tests := []struct{ name, username string }{
		{"too short", "ab"},
		{"too long", strings.Repeat("a", 33)},
		{"leading dot", ".ilia"},
		{"trailing dash", "ilia-"},
		{"contains space", "il ia"},
		{"contains slash", "ilia/admin"},
		{"empty", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := s.CreateUser(ctx, tt.username, "$argon2id$fake", false); !errors.Is(err, ErrInvalidUsername) {
				t.Errorf("err = %v, want ErrInvalidUsername", err)
			}
		})
	}

	if _, err := s.CreateUser(ctx, "valid.name", "", false); err == nil {
		t.Error("CreateUser accepted an empty password hash")
	}
}

func TestMissingUserIsNotFound(t *testing.T) {
	s, _ := newStore(t)
	ctx := context.Background()

	if _, err := s.UserByUsername(ctx, "nobody"); !errors.Is(err, ErrNotFound) {
		t.Errorf("UserByUsername err = %v, want ErrNotFound", err)
	}
	if _, err := s.UserByID(ctx, 12345); !errors.Is(err, ErrNotFound) {
		t.Errorf("UserByID err = %v, want ErrNotFound", err)
	}
}

func TestCountUsers(t *testing.T) {
	s, _ := newStore(t)
	ctx := context.Background()

	n, err := s.CountUsers(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("CountUsers on a fresh database = %d, want 0", n)
	}
	if _, err := s.CreateUser(ctx, "ilia", "$argon2id$fake", true); err != nil {
		t.Fatal(err)
	}
	if n, err = s.CountUsers(ctx); err != nil || n != 1 {
		t.Errorf("CountUsers = %d (err %v), want 1", n, err)
	}
}
```

- [ ] **Step 4: Run the tests**

```bash
go test ./internal/platform/auth/ -race -v
```

Expected: all PASS, including the Task 4 password tests.

- [ ] **Step 5: Verify the migration is embedded, not read from disk**

This proves the `go:embed` directive is doing its job — a binary that reads migrations from the filesystem would break the single-binary deployment.

```bash
go build -o /tmp/onsuite-embed-check ./cmd/onsuite
strings /tmp/onsuite-embed-check | grep -c 'CREATE TABLE users'
```

Expected: `1` or more. If `0`, the migration is not embedded.

- [ ] **Step 6: Commit**

```bash
git add internal/platform/auth
git commit -m "Add the identity schema and user store

Usernames keep their display case but carry uniqueness on a folded column,
so capitalisation cannot create a second account or block a login. Sessions
cascade on user delete. Timestamps are RFC 3339 UTC text, which sorts
chronologically as text and so needs no special handling in expiry queries.

Store tests apply the real migration rather than a hand-written schema, so
they cover the migration too."
```

---
## Task 6: Sessions with sliding expiry

**Files:**
- Create: `internal/platform/auth/session.go`
- Modify: `internal/platform/auth/store.go` (inject a clock so expiry is testable without sleeping)
- Test: `internal/platform/auth/session_test.go`

**Interfaces:**
- Consumes: the `Store` and error values from Task 5.
- Produces:
  - `auth.SessionTTL = 30 * 24 * time.Hour`, `auth.SessionRenewInterval = 24 * time.Hour`
  - `auth.Session{ID string; UserID int64; CreatedAt, ExpiresAt time.Time}`
  - `(*Store).CreateSession(ctx context.Context, userID int64) (Session, error)`
  - `(*Store).UseSession(ctx context.Context, id string) (Session, error)`
  - `(*Store).DeleteSession(ctx context.Context, id string) error`
  - `(*Store).DeleteUserSessions(ctx context.Context, userID int64) (int64, error)`
  - `(*Store).DeleteExpiredSessions(ctx context.Context) (int64, error)`
  - `(*Store).SetClock(now func() time.Time)` — test seam only

**Design notes:**
- The method is called `UseSession`, not `Session`, because it writes: it slides the expiry forward. A getter that mutates would be a trap, so the name says what it does.
- Sliding expiry with only an `expires_at` column: a session is renewed when it has less than `SessionTTL - SessionRenewInterval` remaining, meaning at most one renewal write per day per session rather than one per request.
- Session ids are 32 bytes from `crypto/rand`, base64url encoded to 43 characters. They are looked up as a primary key, so no constant-time comparison is needed here.

- [ ] **Step 1: Add the clock seam to the store**

In `internal/platform/auth/store.go`, change the `Store` struct and constructor, and use the clock in `CreateUser`:

```go
// Store is all the SQL for identity. It has no HTTP knowledge, which is what
// lets it be tested against a real database with no server running.
type Store struct {
	db  *sql.DB
	now func() time.Time
}

func NewStore(handle *sql.DB) *Store {
	return &Store{db: handle, now: func() time.Time { return time.Now().UTC() }}
}

// SetClock replaces the time source. It exists so expiry behaviour can be
// tested by moving time forward instead of sleeping for thirty days.
func (s *Store) SetClock(now func() time.Time) { s.now = now }
```

Then in `CreateUser`, replace `now := time.Now().UTC()` with `now := s.now()`.

- [ ] **Step 2: Write the failing test**

Create `internal/platform/auth/session_test.go`:

```go
package auth

import (
	"context"
	"errors"
	"testing"
	"time"
)

// clock is a manually advanced time source, so expiry tests are instant and
// deterministic rather than depending on sleeps.
type clock struct{ t time.Time }

func (c *clock) now() time.Time      { return c.t }
func (c *clock) add(d time.Duration) { c.t = c.t.Add(d) }

// newSessionStore returns a migrated store, a fixed clock, and a user to
// attach sessions to.
func newSessionStore(t *testing.T) (*Store, *clock, User) {
	t.Helper()

	s, _ := newStore(t)
	c := &clock{t: time.Date(2026, 8, 18, 12, 0, 0, 0, time.UTC)}
	s.SetClock(c.now)

	u, err := s.CreateUser(context.Background(), "ilia", "$argon2id$fake", true)
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	return s, c, u
}

func TestCreateSession(t *testing.T) {
	s, c, u := newSessionStore(t)
	ctx := context.Background()

	sess, err := s.CreateSession(ctx, u.ID)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if len(sess.ID) < 40 {
		t.Errorf("session id %q is only %d characters, want at least 40", sess.ID, len(sess.ID))
	}
	if sess.UserID != u.ID {
		t.Errorf("UserID = %d, want %d", sess.UserID, u.ID)
	}
	if want := c.t.Add(SessionTTL); !sess.ExpiresAt.Equal(want) {
		t.Errorf("ExpiresAt = %v, want %v", sess.ExpiresAt, want)
	}

	// Ids must not repeat.
	other, err := s.CreateSession(ctx, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if other.ID == sess.ID {
		t.Fatal("two sessions were issued the same id")
	}
}

func TestCreateSessionRejectsUnknownUser(t *testing.T) {
	s, _, _ := newSessionStore(t)
	// The foreign key must reject this; it is the pragma from Task 2 doing its job.
	if _, err := s.CreateSession(context.Background(), 99999); err == nil {
		t.Fatal("CreateSession succeeded for a nonexistent user")
	}
}

func TestUseSessionReturnsAValidSession(t *testing.T) {
	s, _, u := newSessionStore(t)
	ctx := context.Background()

	created, err := s.CreateSession(ctx, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	got, err := s.UseSession(ctx, created.ID)
	if err != nil {
		t.Fatalf("UseSession: %v", err)
	}
	if got.UserID != u.ID {
		t.Errorf("UserID = %d, want %d", got.UserID, u.ID)
	}
}

func TestUseSessionRejectsUnknownAndExpired(t *testing.T) {
	s, c, u := newSessionStore(t)
	ctx := context.Background()

	if _, err := s.UseSession(ctx, "never-issued"); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown session err = %v, want ErrNotFound", err)
	}
	if _, err := s.UseSession(ctx, ""); !errors.Is(err, ErrNotFound) {
		t.Errorf("empty session id err = %v, want ErrNotFound", err)
	}

	created, err := s.CreateSession(ctx, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	c.add(SessionTTL + time.Second)
	if _, err := s.UseSession(ctx, created.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("expired session err = %v, want ErrNotFound", err)
	}
}

// TestUseSessionSlidesExpiryButNotOnEveryRequest covers both halves of the
// sliding-expiry contract: an active session must not expire, and an active
// session must not cause a database write on every single request.
func TestUseSessionSlidesExpiryButNotOnEveryRequest(t *testing.T) {
	s, c, u := newSessionStore(t)
	ctx := context.Background()

	created, err := s.CreateSession(ctx, u.ID)
	if err != nil {
		t.Fatal(err)
	}

	// Used again immediately: too soon to be worth a write.
	got, err := s.UseSession(ctx, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !got.ExpiresAt.Equal(created.ExpiresAt) {
		t.Errorf("expiry moved on an immediate second use: %v then %v", created.ExpiresAt, got.ExpiresAt)
	}

	// Used again after the renew interval: expiry slides forward.
	c.add(SessionRenewInterval + time.Minute)
	got, err = s.UseSession(ctx, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !got.ExpiresAt.After(created.ExpiresAt) {
		t.Errorf("expiry did not slide: still %v after %v elapsed", got.ExpiresAt, SessionRenewInterval)
	}
	if want := c.t.Add(SessionTTL); !got.ExpiresAt.Equal(want) {
		t.Errorf("ExpiresAt = %v, want %v", got.ExpiresAt, want)
	}

	// The slide must be persisted, not just returned.
	reread, err := s.UseSession(ctx, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !reread.ExpiresAt.Equal(got.ExpiresAt) {
		t.Errorf("slid expiry was not persisted: %v then %v", got.ExpiresAt, reread.ExpiresAt)
	}
}

// TestSessionStaysAliveAcrossContinuedUse is the behaviour a user actually
// notices: someone using the suite daily is never logged out.
func TestSessionStaysAliveAcrossContinuedUse(t *testing.T) {
	s, c, u := newSessionStore(t)
	ctx := context.Background()

	created, err := s.CreateSession(ctx, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	for day := range 120 {
		c.add(25 * time.Hour)
		if _, err := s.UseSession(ctx, created.ID); err != nil {
			t.Fatalf("session died on day %d of continuous use: %v", day, err)
		}
	}
}

func TestDeleteSessionRevokesImmediately(t *testing.T) {
	s, _, u := newSessionStore(t)
	ctx := context.Background()

	created, err := s.CreateSession(ctx, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteSession(ctx, created.ID); err != nil {
		t.Fatalf("DeleteSession: %v", err)
	}
	if _, err := s.UseSession(ctx, created.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("deleted session still usable, err = %v", err)
	}
	// Deleting again is not an error; logout must be idempotent.
	if err := s.DeleteSession(ctx, created.ID); err != nil {
		t.Errorf("second DeleteSession: %v", err)
	}
}

func TestDeleteUserSessions(t *testing.T) {
	s, _, u := newSessionStore(t)
	ctx := context.Background()

	for range 3 {
		if _, err := s.CreateSession(ctx, u.ID); err != nil {
			t.Fatal(err)
		}
	}
	n, err := s.DeleteUserSessions(ctx, u.ID)
	if err != nil {
		t.Fatalf("DeleteUserSessions: %v", err)
	}
	if n != 3 {
		t.Errorf("deleted %d sessions, want 3", n)
	}
}

func TestDeleteExpiredSessions(t *testing.T) {
	s, c, u := newSessionStore(t)
	ctx := context.Background()

	stale, err := s.CreateSession(ctx, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	c.add(SessionTTL + time.Hour)
	fresh, err := s.CreateSession(ctx, u.ID)
	if err != nil {
		t.Fatal(err)
	}

	n, err := s.DeleteExpiredSessions(ctx)
	if err != nil {
		t.Fatalf("DeleteExpiredSessions: %v", err)
	}
	if n != 1 {
		t.Errorf("swept %d sessions, want 1", n)
	}
	if _, err := s.UseSession(ctx, fresh.ID); err != nil {
		t.Errorf("sweep removed a live session: %v", err)
	}
	if _, err := s.UseSession(ctx, stale.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("stale session survived the sweep")
	}
}
```

- [ ] **Step 3: Run the test and watch it fail**

```bash
go test ./internal/platform/auth/ -run TestSession -run TestUseSession -run TestCreateSession -run TestDelete -v
```

Expected: FAIL to build with `undefined: SessionTTL`, `undefined: SessionRenewInterval`, `s.CreateSession undefined`, `s.UseSession undefined`, `s.SetClock undefined`.

- [ ] **Step 4: Implement sessions**

Create `internal/platform/auth/session.go`:

```go
package auth

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"time"
)

const (
	// SessionTTL is how long a session lives without being used.
	SessionTTL = 30 * 24 * time.Hour

	// SessionRenewInterval throttles the sliding expiry. A session is only
	// written back once this much of its life has passed, so an active user
	// costs at most one session write per day rather than one per request.
	SessionRenewInterval = 24 * time.Hour

	sessionIDBytes = 32 // 256 bits, base64url encoded to 43 characters
)

// Session is a logged-in browser. It carries no user detail; callers look the
// user up by UserID, so revoking a session never leaves stale user data
// cached in a cookie.
type Session struct {
	ID        string
	UserID    int64
	CreatedAt time.Time
	ExpiresAt time.Time
}

// CreateSession issues a new session for userID.
func (s *Store) CreateSession(ctx context.Context, userID int64) (Session, error) {
	id, err := newSessionID()
	if err != nil {
		return Session{}, err
	}
	now := s.now()
	sess := Session{
		ID:        id,
		UserID:    userID,
		CreatedAt: now,
		ExpiresAt: now.Add(SessionTTL),
	}
	if _, err := s.db.ExecContext(ctx,
		`INSERT INTO sessions (id, user_id, created_at, expires_at) VALUES (?, ?, ?, ?)`,
		sess.ID, sess.UserID, formatTime(sess.CreatedAt), formatTime(sess.ExpiresAt),
	); err != nil {
		return Session{}, fmt.Errorf("auth: create session: %w", err)
	}
	return sess, nil
}

// UseSession fetches a live session and slides its expiry forward if it is
// due for renewal.
//
// It is named "Use" rather than "Get" because it writes. An expired or
// unknown id both yield ErrNotFound: the caller's response is identical
// either way, and distinguishing them would leak whether an id was ever
// valid.
func (s *Store) UseSession(ctx context.Context, id string) (Session, error) {
	if id == "" {
		return Session{}, ErrNotFound
	}

	var sess Session
	var createdAt, expiresAt string
	err := s.db.QueryRowContext(ctx,
		`SELECT id, user_id, created_at, expires_at FROM sessions WHERE id = ?`, id,
	).Scan(&sess.ID, &sess.UserID, &createdAt, &expiresAt)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return Session{}, ErrNotFound
	case err != nil:
		return Session{}, fmt.Errorf("auth: read session: %w", err)
	}

	if sess.CreatedAt, err = parseTime(createdAt); err != nil {
		return Session{}, err
	}
	if sess.ExpiresAt, err = parseTime(expiresAt); err != nil {
		return Session{}, err
	}

	now := s.now()
	if !sess.ExpiresAt.After(now) {
		// Expired. Leave the row for DeleteExpiredSessions rather than
		// deleting on a read path.
		return Session{}, ErrNotFound
	}

	if sess.ExpiresAt.Sub(now) < SessionTTL-SessionRenewInterval {
		renewed := now.Add(SessionTTL)
		if _, err := s.db.ExecContext(ctx,
			`UPDATE sessions SET expires_at = ? WHERE id = ?`, formatTime(renewed), sess.ID,
		); err != nil {
			return Session{}, fmt.Errorf("auth: renew session: %w", err)
		}
		sess.ExpiresAt = renewed
	}
	return sess, nil
}

// DeleteSession revokes one session. It is idempotent, so a logout from an
// already-logged-out browser is not an error.
func (s *Store) DeleteSession(ctx context.Context, id string) error {
	if _, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE id = ?`, id); err != nil {
		return fmt.Errorf("auth: delete session: %w", err)
	}
	return nil
}

// DeleteUserSessions revokes every session belonging to a user — used when a
// password changes, so a stolen cookie stops working.
func (s *Store) DeleteUserSessions(ctx context.Context, userID int64) (int64, error) {
	res, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE user_id = ?`, userID)
	if err != nil {
		return 0, fmt.Errorf("auth: delete user sessions: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("auth: delete user sessions: %w", err)
	}
	return n, nil
}

// DeleteExpiredSessions removes sessions past their expiry. Timestamps are
// RFC 3339 UTC text, so a string comparison is a chronological comparison.
func (s *Store) DeleteExpiredSessions(ctx context.Context) (int64, error) {
	res, err := s.db.ExecContext(ctx,
		`DELETE FROM sessions WHERE expires_at <= ?`, formatTime(s.now()))
	if err != nil {
		return 0, fmt.Errorf("auth: sweep sessions: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("auth: sweep sessions: %w", err)
	}
	return n, nil
}

func newSessionID() (string, error) {
	buf := make([]byte, sessionIDBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("auth: generate session id: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}
```

- [ ] **Step 5: Run the tests and watch them pass**

```bash
go test ./internal/platform/auth/ -race -v
```

Expected: all PASS. `TestSessionStaysAliveAcrossContinuedUse` is the one worth reading the output of — it simulates 120 days of daily use and must never log the user out.

- [ ] **Step 6: Commit**

```bash
git add internal/platform/auth
git commit -m "Add sessions with throttled sliding expiry

A session lives 30 days from last use, but the expiry is only written back
once a day, so an active user costs one session write per day rather than one
per request. Expired and unknown session ids are indistinguishable to the
caller, so a response cannot reveal whether an id was ever valid.

The lookup is named UseSession rather than GetSession because it writes.
Expiry is tested by advancing an injected clock rather than by sleeping."
```

---

## Task 7: `onsuite user add`, and migrations applied on startup

**Files:**
- Create: `cmd/onsuite/user.go`
- Modify: `cmd/onsuite/main.go` (dispatch `user`)
- Modify: `cmd/onsuite/serve.go` (apply migrations, warn when there are no accounts)
- Test: `cmd/onsuite/user_test.go`

**Interfaces:**
- Consumes: everything from Tasks 1–6.
- Produces: `onsuite user add <username> [--admin] [--data-dir DIR]`, and a `serve` command that applies migrations before listening.

**Design notes — why there is no `--password` flag:**
A flag value appears in `ps` output and in shell history, so the password is read from standard input instead. On a terminal it is prompted for twice with echo disabled, via `golang.org/x/term`. When standard input is not a terminal, one line is read, so `onsuite user add ilia < secret.txt` works for scripted setup. This is spec §7.1.

- [ ] **Step 1: Add the dependency**

```bash
go get golang.org/x/term@v0.45.0
```

- [ ] **Step 2: Write the user command**

Create `cmd/onsuite/user.go`:

```go
package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"golang.org/x/term"

	"github.com/iliafrenkel/on-suite/internal/platform/auth"
	"github.com/iliafrenkel/on-suite/internal/platform/config"
	"github.com/iliafrenkel/on-suite/internal/platform/db"
)

func userCmd(args []string, getenv func(string) string, errOut io.Writer) error {
	if len(args) == 0 {
		fmt.Fprint(errOut, "usage: onsuite user add <username> [--admin] [--data-dir DIR]\n")
		return errors.New("user: no subcommand given")
	}
	switch args[0] {
	case "add":
		return userAdd(args[1:], getenv, os.Stdin, os.Stdout, errOut)
	default:
		return fmt.Errorf("user: unknown subcommand %q", args[0])
	}
}

func userAdd(args []string, getenv func(string) string, in *os.File, out, errOut io.Writer) error {
	fs := flag.NewFlagSet("user add", flag.ContinueOnError)
	fs.SetOutput(errOut)
	admin := fs.Bool("admin", false, "grant administrator rights")
	dataDir := fs.String("data-dir", envOrDefault(getenv, "ONSUITE_DATA_DIR", "./data"),
		"directory holding the database")
	positional, err := parseInterspersed(fs, args)
	if err != nil {
		return err
	}
	if len(positional) != 1 {
		return errors.New("user add: exactly one username is required")
	}
	username := positional[0]
	if err := auth.ValidateUsername(username); err != nil {
		return err
	}

	password, err := readPassword(in, out)
	if err != nil {
		return err
	}
	if err := auth.ValidatePassword(password); err != nil {
		return err
	}
	hash, err := auth.HashPassword(password)
	if err != nil {
		return err
	}

	cfg := config.Config{DataDir: *dataDir}
	handle, err := db.Open(cfg.DBPath())
	if err != nil {
		return err
	}
	defer func() { _ = handle.Close() }()

	ctx := context.Background()
	migrations, err := db.Collect(auth.Namespace, auth.Migrations())
	if err != nil {
		return err
	}
	if _, err := db.Apply(ctx, handle, migrations); err != nil {
		return err
	}

	user, err := auth.NewStore(handle).CreateUser(ctx, username, hash, *admin)
	if err != nil {
		return err
	}

	role := "user"
	if user.IsAdmin {
		role = "administrator"
	}
	fmt.Fprintf(out, "Created %s %q (id %d) in %s\n", role, user.Username, user.ID, cfg.DBPath())
	return nil
}

// readPassword takes the password from in, never from a flag: a flag value is
// visible in ps output and in shell history.
//
// On a terminal it prompts twice with echo disabled. Otherwise it reads a
// single line, so "onsuite user add ilia < secret.txt" works unattended.
func readPassword(in *os.File, out io.Writer) (string, error) {
	if !term.IsTerminal(int(in.Fd())) {
		line, err := bufio.NewReader(in).ReadString('\n')
		if err != nil && !errors.Is(err, io.EOF) {
			return "", fmt.Errorf("read password from stdin: %w", err)
		}
		password := strings.TrimRight(line, "\r\n")
		if password == "" {
			return "", errors.New("no password supplied on stdin")
		}
		return password, nil
	}

	fmt.Fprintf(out, "Password (at least %d characters): ", auth.MinPasswordLength)
	first, err := term.ReadPassword(int(in.Fd()))
	fmt.Fprintln(out)
	if err != nil {
		return "", fmt.Errorf("read password: %w", err)
	}

	fmt.Fprint(out, "Repeat password: ")
	second, err := term.ReadPassword(int(in.Fd()))
	fmt.Fprintln(out)
	if err != nil {
		return "", fmt.Errorf("read password confirmation: %w", err)
	}

	if string(first) != string(second) {
		return "", errors.New("passwords do not match")
	}
	return string(first), nil
}

// envOrDefault mirrors config's precedence for the commands that take only a
// data directory and do not need the full Config.
func envOrDefault(getenv func(string) string, key, def string) string {
	if getenv == nil {
		return def
	}
	if v := getenv(key); v != "" {
		return v
	}
	return def
}

// parseInterspersed parses flags that may appear before or after positional
// arguments, and returns the positional ones.
//
// Go's flag package stops parsing at the first non-flag argument. A plain
// fs.Parse would therefore treat "--admin" as a positional argument in
// "onsuite user add ilia --admin" — which is the form the spec itself
// documents — and fail with a confusing "exactly one username is required".
func parseInterspersed(fs *flag.FlagSet, args []string) ([]string, error) {
	var positional []string
	for rest := args; ; {
		if err := fs.Parse(rest); err != nil {
			return nil, err
		}
		rest = fs.Args()
		if len(rest) == 0 {
			return positional, nil
		}
		positional = append(positional, rest[0])
		rest = rest[1:]
	}
}
```

**Why `parseInterspersed` exists.** This was found by compiling this plan's
code and running its tests before the plan was published. `flag.Parse` stops at
the first positional argument, so the natural and documented invocation
`onsuite user add ilia --admin` would silently create a non-admin account —
or rather, fail with a message pointing at the wrong problem. Do not simplify
this back to a plain `fs.Parse`; there is a test below that fails if you do.

- [ ] **Step 3: Dispatch the command**

In `cmd/onsuite/main.go`, add a case to the switch in `run`, before `default`:

```go
	case "user":
		return userCmd(args[1:], getenv, errOut)
```

And add a line to `usage`, after the `serve` line:

```
  onsuite user add <name>   create an account
```

- [ ] **Step 4: Apply migrations on startup**

In `cmd/onsuite/serve.go`, add `"github.com/iliafrenkel/on-suite/internal/platform/auth"` to the imports, and insert this immediately after the `log.Info("database ready", ...)` line:

```go
	migrations, err := db.Collect(auth.Namespace, auth.Migrations())
	if err != nil {
		return err
	}
	applied, err := db.Apply(context.Background(), handle, migrations)
	if err != nil {
		return err
	}
	if applied > 0 {
		log.Info("migrations applied", "count", applied)
	}

	users := auth.NewStore(handle)
	switch n, err := users.CountUsers(context.Background()); {
	case err != nil:
		return err
	case n == 0:
		// The single most likely first-run confusion, so say it plainly
		// rather than leaving an empty login page as the only clue.
		log.Warn("no accounts exist yet; create one with: onsuite user add <name> --admin",
			"data_dir", cfg.DataDir)
	}
```

- [ ] **Step 5: Test the command end to end**

Create `cmd/onsuite/user_test.go`:

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

	"github.com/iliafrenkel/on-suite/internal/platform/auth"
	"github.com/iliafrenkel/on-suite/internal/platform/db"
)

// stdinFrom writes s to a temp file and returns it opened for reading, so the
// non-terminal branch of readPassword can be exercised with a real *os.File.
func stdinFrom(t *testing.T, s string) *os.File {
	t.Helper()
	path := filepath.Join(t.TempDir(), "stdin")
	if err := os.WriteFile(path, []byte(s), 0o600); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = f.Close() })
	return f
}

func TestUserAddCreatesALoginableAccount(t *testing.T) {
	dir := t.TempDir()
	var out bytes.Buffer

	err := userAdd(
		[]string{"ilia", "--admin", "--data-dir", dir}, nil,
		stdinFrom(t, "a-sufficiently-long-password\n"),
		&out, io.Discard,
	)
	if err != nil {
		t.Fatalf("userAdd: %v", err)
	}
	if !strings.Contains(out.String(), "administrator") {
		t.Errorf("output %q does not mention the role", out.String())
	}

	// Reopen the database the way the server would and check the account works.
	handle, err := db.Open(filepath.Join(dir, "onsuite.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = handle.Close() }()

	u, err := auth.NewStore(handle).UserByUsername(context.Background(), "ilia")
	if err != nil {
		t.Fatalf("UserByUsername: %v", err)
	}
	if !u.IsAdmin {
		t.Error("--admin did not take effect")
	}

	ok, err := auth.VerifyPassword(u.PasswordHash, "a-sufficiently-long-password")
	if err != nil {
		t.Fatalf("VerifyPassword: %v", err)
	}
	if !ok {
		t.Error("the stored hash does not verify against the password that was supplied")
	}
}

func TestUserAddRejectsBadInput(t *testing.T) {
	tests := []struct {
		name  string
		args  []string
		stdin string
	}{
		{"password too short", []string{"ilia"}, "short\n"},
		{"empty password", []string{"ilia"}, "\n"},
		{"invalid username", []string{"a"}, "a-sufficiently-long-password\n"},
		{"no username", nil, "a-sufficiently-long-password\n"},
		{"two usernames", []string{"ilia", "extra"}, "a-sufficiently-long-password\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			args := append([]string{"--data-dir", t.TempDir()}, tt.args...)
			err := userAdd(args, nil, stdinFrom(t, tt.stdin), io.Discard, io.Discard)
			if err == nil {
				t.Fatal("userAdd succeeded, want error")
			}
		})
	}
}

func TestUserAddRejectsDuplicate(t *testing.T) {
	dir := t.TempDir()
	args := []string{"--data-dir", dir, "ilia"}
	const pw = "a-sufficiently-long-password\n"

	if err := userAdd(args, nil, stdinFrom(t, pw), io.Discard, io.Discard); err != nil {
		t.Fatalf("first userAdd: %v", err)
	}
	if err := userAdd(args, nil, stdinFrom(t, pw), io.Discard, io.Discard); err == nil {
		t.Fatal("second userAdd with the same name succeeded, want error")
	}
}

// TestUserAddAcceptsFlagsAfterTheUsername covers the invocation form the spec
// documents in §7.1. Go's flag package stops at the first positional
// argument, so this fails unless flags and positionals are parsed
// interspersed.
func TestUserAddAcceptsFlagsAfterTheUsername(t *testing.T) {
	dir := t.TempDir()
	err := userAdd(
		[]string{"ilia", "--admin", "--data-dir", dir}, nil,
		stdinFrom(t, "a-sufficiently-long-password\n"), io.Discard, io.Discard,
	)
	if err != nil {
		t.Fatalf("userAdd with trailing flags: %v", err)
	}

	handle, err := db.Open(filepath.Join(dir, "onsuite.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = handle.Close() }()

	u, err := auth.NewStore(handle).UserByUsername(context.Background(), "ilia")
	if err != nil {
		t.Fatalf("UserByUsername: %v", err)
	}
	if !u.IsAdmin {
		t.Error("--admin after the username was ignored")
	}
}

// TestUserAddDoesNotAcceptAPasswordFlag guards the design decision in spec
// §7.1: a password passed as a flag would be visible in ps and in shell
// history, so the flag must not exist.
func TestUserAddDoesNotAcceptAPasswordFlag(t *testing.T) {
	err := userAdd(
		[]string{"--data-dir", t.TempDir(), "--password", "a-sufficiently-long-password", "ilia"},
		nil, stdinFrom(t, "a-sufficiently-long-password\n"), io.Discard, io.Discard,
	)
	if err == nil {
		t.Fatal("--password was accepted; it must not exist")
	}
}
```

- [ ] **Step 6: Run the whole suite**

```bash
go build ./... && go vet ./... && go test ./... -race
```

Expected: all packages PASS.

- [ ] **Step 7: Verify the deliverable by hand**

This is the point of the whole plan, so do it for real:

```bash
rm -rf /tmp/onsuite-dev && go run ./cmd/onsuite user add ilia --admin --data-dir /tmp/onsuite-dev
```

Expected: prompts `Password (at least 12 characters):` with **no characters echoed**, prompts again to repeat, then prints `Created administrator "ilia" (id 1) in /tmp/onsuite-dev/onsuite.db`.

Confirm mismatched entries are rejected, and that the scripted path works:

```bash
printf 'another-long-password\n' | go run ./cmd/onsuite user add ilia2 --data-dir /tmp/onsuite-dev
```

Expected: `Created user "ilia2" (id 2) ...` with no prompt.

Then check the server sees them:

```bash
go run ./cmd/onsuite serve --data-dir /tmp/onsuite-dev
```

Expected: `database ready`, **no** `no accounts exist yet` warning, and `/healthz` returns 200. Against an empty directory the warning does appear.

- [ ] **Step 8: Commit**

```bash
git add go.mod go.sum cmd/onsuite
git commit -m "Add onsuite user add and apply migrations on startup

Creating the first account from the command line avoids a first-run setup
wizard: the account exists before the server ever listens, so there is no
window in which an unauthenticated endpoint can create an administrator.

The password is read from stdin rather than a flag, because a flag value is
visible in ps output and shell history. On a terminal it is prompted for
twice with echo disabled; otherwise a single line is read so scripted setup
works. A test asserts --password does not exist, so it cannot be added back
by accident.

The server now applies migrations before listening and warns clearly when a
database has no accounts, which is the first-run state every self-hoster
hits."
```

---

# Definition of done for Plan 1

All of the following must hold before Plan 2 starts:

1. `gofmt -l .` is empty, and `go build ./... && go vet ./... && go test ./... -race` is green.
2. `CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build ./cmd/onsuite` succeeds.
3. `go list -m all | grep -v '^github.com/iliafrenkel/on-suite'` shows only `modernc.org/*`, `golang.org/x/crypto`, `golang.org/x/term`, `golang.org/x/sys` and their required indirects — no unexpected dependency crept in.
4. `onsuite user add` creates an account on an empty data directory, prompting without echo.
5. `onsuite serve` applies migrations, reports `database ready`, serves `/healthz`, and drains cleanly on Ctrl-C.
6. Deleting `onsuite.db` and rerunning reproduces the same schema from migrations alone.

Spec success criteria satisfied by this plan: **§11.1** (static CGO-free build) and **§11.2** (`user add` on an empty data directory). Criteria 3–8 are covered by Plans 2 and 3.
