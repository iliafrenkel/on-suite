package main

import (
	"context"
	"strings"
	"testing"

	"github.com/iliafrenkel/on-suite/internal/platform/config"
)

// TestEveryUserColumnCascadesFromUsers guards the assumption auth.Store's
// DeleteUser (#310) relies on: every table that references a user does so
// with ON DELETE CASCADE, so deleting a user removes everything that
// account owned in one DELETE. A new app (or a new column on an existing
// one) that references users without that cascade leaves orphan rows behind
// instead of failing loudly, so this walks the whole production schema and
// checks every *_user_id / user_id column itself.
func TestEveryUserColumnCascadesFromUsers(t *testing.T) {
	ctx := context.Background()
	handle, _, _, err := openDatabase(ctx, config.Config{DataDir: t.TempDir()})
	if err != nil {
		t.Fatalf("openDatabase: %v", err)
	}
	defer func() { _ = handle.Close() }()

	rows, err := handle.QueryContext(ctx,
		`SELECT name FROM sqlite_master WHERE type = 'table' AND name NOT LIKE 'sqlite_%'`)
	if err != nil {
		t.Fatalf("list tables: %v", err)
	}
	var tables []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("scan table name: %v", err)
		}
		tables = append(tables, name)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("list tables: %v", err)
	}

	for _, table := range tables {
		if table == "users" {
			continue
		}

		colRows, err := handle.QueryContext(ctx, `PRAGMA table_info(`+table+`)`)
		if err != nil {
			t.Fatalf("table_info(%s): %v", table, err)
		}
		var userColumns []string
		for colRows.Next() {
			var (
				cid        int
				name, ctyp string
				notnull    int
				dflt       any
				pk         int
			)
			if err := colRows.Scan(&cid, &name, &ctyp, &notnull, &dflt, &pk); err != nil {
				colRows.Close()
				t.Fatalf("scan table_info(%s): %v", table, err)
			}
			if name == "user_id" || strings.HasSuffix(name, "_user_id") {
				userColumns = append(userColumns, name)
			}
		}
		colRows.Close()
		if err := colRows.Err(); err != nil {
			t.Fatalf("table_info(%s): %v", table, err)
		}
		if len(userColumns) == 0 {
			continue
		}

		fkRows, err := handle.QueryContext(ctx, `PRAGMA foreign_key_list(`+table+`)`)
		if err != nil {
			t.Fatalf("foreign_key_list(%s): %v", table, err)
		}
		type fk struct {
			table, from, to, onDelete string
		}
		var fks []fk
		for fkRows.Next() {
			var (
				id, seq                                      int
				fkTable, from, to, onUpdate, onDelete, match string
			)
			if err := fkRows.Scan(&id, &seq, &fkTable, &from, &to, &onUpdate, &onDelete, &match); err != nil {
				fkRows.Close()
				t.Fatalf("scan foreign_key_list(%s): %v", table, err)
			}
			fks = append(fks, fk{table: fkTable, from: from, to: to, onDelete: onDelete})
		}
		fkRows.Close()
		if err := fkRows.Err(); err != nil {
			t.Fatalf("foreign_key_list(%s): %v", table, err)
		}

		for _, col := range userColumns {
			ok := false
			for _, f := range fks {
				if f.from == col && f.table == "users" && f.to == "id" && strings.EqualFold(f.onDelete, "CASCADE") {
					ok = true
					break
				}
			}
			if !ok {
				t.Errorf("%s.%s references a user but has no ON DELETE CASCADE foreign key to users(id); "+
					"auth.Store.DeleteUser (#310) relies on that cascade to remove everything the account owned",
					table, col)
			}
		}
	}
}
