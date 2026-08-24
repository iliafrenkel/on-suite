package admin

import (
	"context"
	"fmt"
	"os"
	"runtime"
	"time"
)

// RuntimeInfo is what this process is and how long it has been up.
type RuntimeInfo struct {
	Version    string
	Go         string
	OS         string
	Arch       string
	CPUs       int
	Goroutines int
	HeapInUse  string
	StartedAt  time.Time
	Uptime     time.Duration
}

// runtimeInfo reads the process's own vital signs.
//
// runtime.ReadMemStats stops the world briefly. On a page one person loads
// occasionally that is free; it would not be on a hot path.
func (d Deps) runtimeInfo(now time.Time) RuntimeInfo {
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)

	return RuntimeInfo{
		Version:    d.Version,
		Go:         runtime.Version(),
		OS:         runtime.GOOS,
		Arch:       runtime.GOARCH,
		CPUs:       runtime.NumCPU(),
		Goroutines: runtime.NumGoroutine(),
		HeapInUse:  humanBytes(int64(ms.HeapInuse)),
		StartedAt:  d.Started,
		Uptime:     now.Sub(d.Started).Truncate(time.Second),
	}
}

// DatabaseInfo is the state of the one SQLite file everything lives in.
type DatabaseInfo struct {
	Path        string
	FileSize    string
	WALSize     string
	SHMSize     string
	PageSize    int64
	PageCount   int64
	JournalMode string
	Migrations  []MigrationInfo
}

// MigrationInfo is one applied migration.
type MigrationInfo struct {
	Key       string // "paste:0001"
	Name      string
	AppliedAt string
}

// databaseInfo reads SQLite's own view of itself plus the file sizes on disk.
//
// The WAL and shared-memory files matter as much as the database: a WAL that
// keeps growing is the visible symptom of checkpointing having stopped, and
// nothing else in the system would show it.
func (d Deps) databaseInfo(ctx context.Context) (DatabaseInfo, error) {
	path := d.Config.DBPath()
	info := DatabaseInfo{
		Path:     path,
		FileSize: fileSize(path),
		WALSize:  fileSize(path + "-wal"),
		SHMSize:  fileSize(path + "-shm"),
	}
	if d.DB == nil {
		return info, fmt.Errorf("no database handle")
	}

	if err := d.DB.QueryRowContext(ctx, "PRAGMA page_size").Scan(&info.PageSize); err != nil {
		return info, fmt.Errorf("page_size: %w", err)
	}
	if err := d.DB.QueryRowContext(ctx, "PRAGMA page_count").Scan(&info.PageCount); err != nil {
		return info, fmt.Errorf("page_count: %w", err)
	}
	if err := d.DB.QueryRowContext(ctx, "PRAGMA journal_mode").Scan(&info.JournalMode); err != nil {
		return info, fmt.Errorf("journal_mode: %w", err)
	}

	rows, err := d.DB.QueryContext(ctx,
		`SELECT key, name, applied_at FROM schema_migrations
		  ORDER BY applied_at, key`)
	if err != nil {
		return info, fmt.Errorf("schema_migrations: %w", err)
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		var m MigrationInfo
		if err := rows.Scan(&m.Key, &m.Name, &m.AppliedAt); err != nil {
			return info, fmt.Errorf("scan migration: %w", err)
		}
		info.Migrations = append(info.Migrations, m)
	}
	if err := rows.Err(); err != nil {
		return info, fmt.Errorf("schema_migrations: %w", err)
	}
	return info, nil
}

// SessionInfo is the state of the sessions table. A growing Expired count
// means the sweep job is not running.
type SessionInfo struct {
	Live    int
	Expired int
}

// sessionInfo counts live and expired sessions.
func (d Deps) sessionInfo(ctx context.Context) (SessionInfo, error) {
	if d.Users == nil {
		return SessionInfo{}, fmt.Errorf("no user store")
	}
	live, expired, err := d.Users.SessionCounts(ctx)
	if err != nil {
		return SessionInfo{}, err
	}
	return SessionInfo{Live: live, Expired: expired}, nil
}

// fileSize reports a file's size, or "—" if it is not there. A missing -wal
// file is normal, not an error: SQLite removes it on a clean shutdown.
func fileSize(path string) string {
	st, err := os.Stat(path)
	if err != nil {
		return "—"
	}
	return humanBytes(st.Size())
}
