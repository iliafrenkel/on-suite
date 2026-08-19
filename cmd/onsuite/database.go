package main

import (
	"context"
	"database/sql"

	"github.com/iliafrenkel/on-suite/internal/platform/app"
	"github.com/iliafrenkel/on-suite/internal/platform/auth"
	"github.com/iliafrenkel/on-suite/internal/platform/config"
	"github.com/iliafrenkel/on-suite/internal/platform/db"
)

// openDatabase opens the database and brings the schema up to date, including
// every registered app's migrations, and returns the registry alongside it.
//
// Every command that touches the database goes through here. Without it each
// command decides for itself which migrations to apply, and they disagree: the
// original user-add applied only the platform's schema and export applied none,
// so exporting a fresh database failed with "no such table: paste_snippets".
func openDatabase(ctx context.Context, cfg config.Config) (*sql.DB, *app.Registry, int, error) {
	registry, err := app.NewRegistry(registeredApps()...)
	if err != nil {
		return nil, nil, 0, err
	}

	handle, err := db.Open(cfg.DBPath())
	if err != nil {
		return nil, nil, 0, err
	}

	migrations, err := db.Collect(auth.Namespace, auth.Migrations())
	if err != nil {
		_ = handle.Close()
		return nil, nil, 0, err
	}
	appMigrations, err := registry.Migrations()
	if err != nil {
		_ = handle.Close()
		return nil, nil, 0, err
	}

	applied, err := db.Apply(ctx, handle, append(migrations, appMigrations...))
	if err != nil {
		_ = handle.Close()
		return nil, nil, 0, err
	}
	return handle, registry, applied, nil
}
