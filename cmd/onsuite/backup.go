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
	"github.com/iliafrenkel/on-suite/internal/platform/jobs"
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

	positional, err := parseInterspersed(fs, args)
	if err != nil {
		return err
	}
	if len(positional) != 0 {
		return fmt.Errorf("backup: unexpected argument(s): %s", strings.Join(positional, " "))
	}

	cfg := config.Config{DataDir: *dataDir}
	ctx := context.Background()
	handle, _, _, err := openDatabase(ctx, cfg)
	if err != nil {
		return err
	}
	defer func() { _ = handle.Close() }()

	// A deployment that drives backups from an external cron job runs with
	// --backup-interval 0, which disables registerMaintenance's session
	// sweep entirely. This command is what such a deployment
	// actually invokes on a schedule, so it takes over session hygiene in
	// that case too: without it, the sessions table would grow forever.
	if swept, err := auth.NewStore(handle).DeleteExpiredSessions(ctx); err != nil {
		return fmt.Errorf("backup: sweep expired sessions: %w", err)
	} else if swept > 0 {
		fmt.Fprintf(out, "Swept %d expired sessions\n", swept)
	}

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

// registerMaintenance registers the server's two housekeeping jobs.
//
// Both take cfg.BackupInterval, so --backup-interval 0 disables both, exactly
// as the single runMaintenance ticker did before: a deployment that drives
// snapshots from cron or a systemd timer runs `onsuite backup`, which sweeps
// sessions itself (see backupCmd). Giving the sweep a schedule of its own
// would be a behaviour change, not a refactor.
//
// Naming the two halves is the point of the split: the admin page can say
// which one last failed, which a single "maintenance" ticker could not.
func registerMaintenance(
	reg *jobs.Registry,
	handle *sql.DB,
	users *auth.Store,
	cfg config.Config,
	log *slog.Logger,
) {
	if cfg.BackupInterval <= 0 {
		log.Info("internal maintenance schedule disabled")
	} else {
		log.Info("maintenance scheduled",
			"interval", cfg.BackupInterval.String(), "keep", cfg.BackupKeep)
	}

	reg.Register(
		"sweep expired sessions",
		"Deletes sessions past their expiry, so the sessions table does not grow forever.",
		cfg.BackupInterval,
		func(ctx context.Context) error {
			swept, err := users.DeleteExpiredSessions(ctx)
			if err != nil {
				log.Error("sweeping expired sessions failed", "error", err)
				return err
			}
			if swept > 0 {
				log.Info("expired sessions swept", "count", swept)
			}
			return nil
		},
	)

	reg.Register(
		"database snapshot",
		"Writes a consistent copy of the database into the backups directory and prunes old snapshots.",
		cfg.BackupInterval,
		func(ctx context.Context) error {
			path, err := writeSnapshot(ctx, handle, cfg.BackupDir(), cfg.BackupKeep, time.Now().UTC())
			logSnapshotResult(log, path, err)
			return err
		},
	)
}

// logSnapshotResult reports the outcome of writeSnapshot. path is only set
// once the snapshot itself has succeeded, so an error alongside a non-empty
// path means pruning failed after a good backup was already written — that
// must not be logged the same way as a failed snapshot, or an operator
// reading logs would wrongly conclude no backup exists.
func logSnapshotResult(log *slog.Logger, path string, err error) {
	switch {
	case err != nil && path == "":
		log.Error("snapshot failed", "error", err)
	case err != nil:
		log.Error("snapshot written but pruning old snapshots failed", "error", err, "path", path)
	default:
		log.Info("snapshot written", "path", path)
	}
}
