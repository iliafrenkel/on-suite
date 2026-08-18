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
