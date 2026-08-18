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
