package main

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/iliafrenkel/on-suite/internal/platform/auth"
	"github.com/iliafrenkel/on-suite/internal/platform/config"
	"github.com/iliafrenkel/on-suite/internal/platform/db"
	"github.com/iliafrenkel/on-suite/internal/platform/jobs"
)

// testDB opens a migrated database in dir.
func testDB(t *testing.T, dir string) *sql.DB {
	t.Helper()
	handle, _, _, err := openDatabase(context.Background(), config.Config{DataDir: dir})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = handle.Close() })
	return handle
}

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

// TestBackupCmdSweepsExpiredSessions guards against the gap where
// --backup-interval 0 (the cron-driven setup documented in
// docs/deploy/README.md) disables registerMaintenance's jobs entirely,
// leaving nothing to sweep expired sessions. backupCmd is what such a
// deployment actually runs on a schedule, so it must do the sweep itself.
func TestBackupCmdSweepsExpiredSessions(t *testing.T) {
	dir := seedDatabase(t)

	handle, err := db.Open(filepath.Join(dir, "onsuite.db"))
	if err != nil {
		t.Fatal(err)
	}
	var userID int64
	if err := handle.QueryRow("SELECT id FROM users LIMIT 1").Scan(&userID); err != nil {
		t.Fatal(err)
	}
	expiredAt := time.Now().UTC().Add(-time.Hour).Format(time.RFC3339Nano)
	if _, err := handle.Exec(
		`INSERT INTO sessions (id, user_id, created_at, expires_at) VALUES (?, ?, ?, ?)`,
		"expired-session", userID, expiredAt, expiredAt,
	); err != nil {
		t.Fatal(err)
	}
	if err := handle.Close(); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	if err := backupCmd([]string{"--data-dir", dir}, nil, &out, io.Discard); err != nil {
		t.Fatalf("backupCmd: %v", err)
	}
	if !strings.Contains(out.String(), "Swept 1 expired session") {
		t.Errorf("output %q does not report the sweep", out.String())
	}

	handle, err = db.Open(filepath.Join(dir, "onsuite.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = handle.Close() }()
	var remaining int
	if err := handle.QueryRow("SELECT count(*) FROM sessions").Scan(&remaining); err != nil {
		t.Fatal(err)
	}
	if remaining != 0 {
		t.Errorf("%d sessions remain, want the expired one swept", remaining)
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

// TestBackupCmdRejectsStrayArguments guards against a likely typo — e.g.
// `onsuite backup /srv/out.db` meant as `--out /srv/out.db` — silently
// writing to the default location and reporting success instead of erroring.
func TestBackupCmdRejectsStrayArguments(t *testing.T) {
	dir := seedDatabase(t)

	if err := backupCmd([]string{"--data-dir", dir, "/srv/out.db"}, nil, io.Discard, io.Discard); err == nil {
		t.Fatal("backupCmd with a stray positional argument succeeded, want an error")
	}

	entries, err := os.ReadDir(filepath.Join(dir, "backups"))
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("a rejected backup still wrote %d file(s)", len(entries))
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

// TestLogSnapshotResultDistinguishesPruneFromSnapshotFailure guards against
// an operator reading "snapshot failed" in the log on a night where a
// snapshot was actually written and only the retention cleanup afterward
// failed.
func TestLogSnapshotResultDistinguishesPruneFromSnapshotFailure(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		err      error
		wantText string
	}{
		{
			name:     "snapshot itself failed",
			path:     "",
			err:      errors.New("vacuum into: disk full"),
			wantText: "snapshot failed",
		},
		{
			name:     "snapshot succeeded, only pruning failed",
			path:     "/data/backups/onsuite-20260818T030000Z.db",
			err:      errors.New("backup: wrote /data/backups/onsuite-20260818T030000Z.db but pruning failed: remove old.db: permission denied"),
			wantText: "snapshot written but pruning old snapshots failed",
		},
		{
			name:     "success",
			path:     "/data/backups/onsuite-20260818T030000Z.db",
			err:      nil,
			wantText: "snapshot written",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			log := slog.New(slog.NewTextHandler(&buf, nil))

			logSnapshotResult(log, tt.path, tt.err)

			got := buf.String()
			if !strings.Contains(got, tt.wantText) {
				t.Errorf("log = %q, want it to contain %q", got, tt.wantText)
			}
			if tt.err != nil && strings.Contains(got, "level=INFO") {
				t.Errorf("log = %q, a failure must not be logged at Info", got)
			}
		})
	}
}

func TestRegisterMaintenanceRegistersBothJobsEnabled(t *testing.T) {
	dir := t.TempDir()
	handle := testDB(t, dir)

	reg := jobs.NewRegistry()
	registerMaintenance(reg, handle, auth.NewStore(handle),
		config.Config{DataDir: dir, BackupInterval: time.Hour, BackupKeep: 3},
		slog.New(slog.DiscardHandler))

	got := reg.Snapshot()
	if len(got) != 2 {
		t.Fatalf("registered %d jobs, want 2", len(got))
	}
	for _, s := range got {
		if !s.Enabled || s.Interval != time.Hour {
			t.Errorf("job %q: Enabled = %v, Interval = %s; want enabled at 1h", s.Name, s.Enabled, s.Interval)
		}
	}
	if got[0].Name != "sweep expired sessions" || got[1].Name != "database snapshot" {
		t.Errorf("job names = %q, %q", got[0].Name, got[1].Name)
	}
}

// A zero interval disabled runMaintenance entirely, including the session
// sweep, which `onsuite backup` then takes over. Splitting maintenance into
// two jobs must not quietly give the sweep a schedule of its own.
func TestRegisterMaintenanceDisablesBothJobsWhenTheIntervalIsZero(t *testing.T) {
	dir := t.TempDir()
	handle := testDB(t, dir)

	reg := jobs.NewRegistry()
	registerMaintenance(reg, handle, auth.NewStore(handle),
		config.Config{DataDir: dir, BackupInterval: 0, BackupKeep: 3},
		slog.New(slog.DiscardHandler))

	for _, s := range reg.Snapshot() {
		if s.Enabled {
			t.Errorf("job %q is enabled with --backup-interval 0", s.Name)
		}
	}
}

func TestTheSnapshotJobWritesASnapshot(t *testing.T) {
	dir := t.TempDir()
	handle := testDB(t, dir)

	reg := jobs.NewRegistry()
	registerMaintenance(reg, handle, auth.NewStore(handle),
		config.Config{DataDir: dir, BackupInterval: time.Hour, BackupKeep: 3},
		slog.New(slog.DiscardHandler))
	reg.RunOnceForTest(context.Background(), "database snapshot")

	entries, err := os.ReadDir(filepath.Join(dir, "backups"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("backups directory holds %d files, want 1", len(entries))
	}
	if s := reg.Snapshot()[1]; s.LastErr != "" {
		t.Errorf("LastErr = %q after a good snapshot", s.LastErr)
	}
}
