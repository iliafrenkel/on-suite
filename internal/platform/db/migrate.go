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
