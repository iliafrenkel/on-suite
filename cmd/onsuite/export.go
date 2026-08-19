package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/iliafrenkel/on-suite/internal/platform/auth"
	"github.com/iliafrenkel/on-suite/internal/platform/config"
)

// exportDocument is the top-level shape written out. Apps appear under their
// own id, so a future app adds a key without changing this structure.
type exportDocument struct {
	Format     int            `json:"format"`
	ExportedAt time.Time      `json:"exported_at"`
	User       string         `json:"user"`
	Apps       map[string]any `json:"apps"`
}

// exportFormat is bumped only if the shape changes incompatibly, so a reader
// can tell.
const exportFormat = 1

func exportCmd(args []string, getenv func(string) string, out io.Writer, errOut io.Writer) error {
	fs := flag.NewFlagSet("export", flag.ContinueOnError)
	fs.SetOutput(errOut)
	dataDir := fs.String("data-dir", envOrDefault(getenv, "ONSUITE_DATA_DIR", "./data"),
		"directory holding the database")
	outPath := fs.String("out", "", "write to this file instead of standard output")

	positional, err := parseInterspersed(fs, args)
	if err != nil {
		return err
	}
	if len(positional) != 1 {
		return errors.New("export: exactly one username is required")
	}
	username := positional[0]

	cfg := config.Config{DataDir: *dataDir}
	ctx := context.Background()
	handle, registry, _, err := openDatabase(ctx, cfg)
	if err != nil {
		return err
	}
	defer func() { _ = handle.Close() }()
	user, err := auth.NewStore(handle).UserByUsername(ctx, username)
	if err != nil {
		if errors.Is(err, auth.ErrNotFound) {
			return fmt.Errorf("export: no such user %q", username)
		}
		return err
	}

	apps, err := registry.Export(ctx, handle, user.ID)
	if err != nil {
		return err
	}

	doc := exportDocument{
		Format:     exportFormat,
		ExportedAt: time.Now().UTC(),
		User:       user.Username,
		Apps:       apps,
	}

	writer := out
	if *outPath != "" {
		// 0600: an export contains everything the user has written.
		f, err := os.OpenFile(*outPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
		if err != nil {
			return fmt.Errorf("export: create %s: %w", *outPath, err)
		}
		defer func() { _ = f.Close() }()
		writer = f
	}

	enc := json.NewEncoder(writer)
	enc.SetIndent("", "  ")
	// A file a person may read, so do not mangle < > & into escapes.
	enc.SetEscapeHTML(false)
	if err := enc.Encode(doc); err != nil {
		return fmt.Errorf("export: write json: %w", err)
	}

	if *outPath != "" {
		fmt.Fprintf(errOut, "Exported %s to %s\n", user.Username, *outPath)
	}
	return nil
}
