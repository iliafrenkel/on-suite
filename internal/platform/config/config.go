// Package config turns command-line flags and ONSUITE_* environment
// variables into a Config. It owns every default value in the system.
package config

import (
	"flag"
	"fmt"
	"io"
	"log/slog"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Config is the complete runtime configuration of the server.
type Config struct {
	Addr      string     // listen address, e.g. ":8080"
	DataDir   string     // holds onsuite.db and backups/
	TLSDomain string     // non-empty enables built-in Let's Encrypt
	LogLevel  slog.Level // parsed from a name, not a number

	// BackupInterval is how often the server snapshots itself. Zero disables
	// the internal schedule, for anyone who would rather drive backups from
	// cron or a systemd timer.
	BackupInterval time.Duration
	// BackupKeep is how many snapshots to retain.
	BackupKeep int

	// TLSHTTPAddr is where the plain-HTTP listener runs when built-in TLS is
	// enabled. It answers ACME HTTP-01 challenges and redirects to HTTPS.
	// Empty disables it, leaving only TLS-ALPN-01 on the HTTPS port.
	TLSHTTPAddr string
	// SecureCookies marks session and CSRF cookies Secure. It is implied by
	// TLSDomain and must be set explicitly when a TLS-terminating proxy is in
	// front, because from this process's point of view that traffic is plain
	// HTTP.
	SecureCookies bool
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
	fs.DurationVar(&c.BackupInterval, "backup-interval",
		envDuration(getenv, "ONSUITE_BACKUP_INTERVAL", 24*time.Hour),
		"how often to snapshot the database; 0 disables the internal schedule")
	fs.IntVar(&c.BackupKeep, "backup-keep",
		envInt(getenv, "ONSUITE_BACKUP_KEEP", 7),
		"how many snapshots to keep")
	fs.StringVar(&c.TLSHTTPAddr, "tls-http-addr", envOr(getenv, "ONSUITE_TLS_HTTP_ADDR", ":80"),
		"plain-HTTP address for ACME challenges and HTTPS redirects; empty to disable")
	secureCookies := fs.Bool("secure-cookies", envOr(getenv, "ONSUITE_SECURE_COOKIES", "") == "true",
		"mark cookies Secure; implied by -tls-domain, set this behind an HTTPS proxy")

	if err := fs.Parse(args); err != nil {
		return Config{}, err
	}
	if strings.TrimSpace(c.DataDir) == "" {
		return Config{}, fmt.Errorf("data-dir must not be empty")
	}
	if c.BackupInterval < 0 {
		return Config{}, fmt.Errorf("backup-interval must not be negative")
	}
	if c.BackupInterval > 0 && c.BackupInterval < time.Minute {
		// A snapshot every few seconds would fill the disk and pointlessly
		// hold a read transaction open. Almost certainly a typo.
		return Config{}, fmt.Errorf("backup-interval %s is too short; use at least 1m or 0 to disable", c.BackupInterval)
	}
	if c.BackupKeep < 1 {
		return Config{}, fmt.Errorf("backup-keep must be at least 1")
	}
	lvl, err := parseLevel(level)
	if err != nil {
		return Config{}, err
	}
	c.LogLevel = lvl

	// Which flags were given explicitly, so a TLS-aware default for the listen
	// address does not override a deliberate choice.
	explicit := make(map[string]bool)
	fs.Visit(func(f *flag.Flag) { explicit[f.Name] = true })

	if c.TLSDomain != "" && !explicit["addr"] && envOr(getenv, "ONSUITE_ADDR", "") == "" {
		// Serving your own certificates on port 8080 is almost never what
		// anyone means.
		c.Addr = ":443"
	}

	// TLS implies Secure cookies; the flag can also switch them on alone, for
	// a proxy that terminates TLS upstream.
	c.SecureCookies = *secureCookies || c.TLSDomain != ""

	return c, nil
}

// DBPath is the single SQLite file holding all suite data.
func (c Config) DBPath() string { return filepath.Join(c.DataDir, "onsuite.db") }

// BackupDir is where snapshots are written.
func (c Config) BackupDir() string { return filepath.Join(c.DataDir, "backups") }

// TLSEnabled reports whether the binary obtains its own certificates.
func (c Config) TLSEnabled() bool { return c.TLSDomain != "" }

// TLSCacheDir is where Let's Encrypt certificates are stored. It lives inside
// the data directory so the whole persistent state is still one tree.
func (c Config) TLSCacheDir() string { return filepath.Join(c.DataDir, "certs") }

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

func envDuration(getenv func(string) string, key string, def time.Duration) time.Duration {
	v := envOr(getenv, key, "")
	if v == "" {
		return def
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		// An unparseable value falls back to the default; the flag equivalent
		// still reports a parse error, which is where a typo will be noticed.
		return def
	}
	return d
}

func envInt(getenv func(string) string, key string, def int) int {
	v := envOr(getenv, key, "")
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return n
}
