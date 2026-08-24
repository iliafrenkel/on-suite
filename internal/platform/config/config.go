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

// The true compile-time defaults, named so that both Parse and the settings
// descriptor list below can use them. They cannot be read back off the
// FlagSet: envOr folds the environment value into a flag's default before the
// flag is defined, so flag.Flag.DefValue reports the environment value.
const (
	defaultAddr           = ":8080"
	defaultDataDir        = "./data"
	defaultTLSDomain      = ""
	defaultLogLevel       = "info"
	defaultBackupInterval = 24 * time.Hour
	defaultBackupKeep     = 7
	defaultTLSHTTPAddr    = ":80"
	defaultSecureCookies  = false
)

// Source says where a setting's live value came from.
type Source int

const (
	SourceDefault Source = iota
	SourceEnv
	SourceFlag
	// SourceDerived is a value the server computed rather than read: enabling
	// TLS moves the listen address to :443 and forces Secure cookies on.
	SourceDerived
)

func (s Source) String() string {
	switch s {
	case SourceFlag:
		return "flag"
	case SourceEnv:
		return "environment"
	case SourceDerived:
		return "derived"
	default:
		return "default"
	}
}

// Setting is one configurable value with everything needed to explain it:
// what it is called, what it is set to, what it would otherwise have been,
// where the live value came from, and what it does.
//
// No setting is redacted, because none is a secret: every one is an address,
// a path, a duration or a boolean. The platform never accepts a password as a
// flag — `onsuite user add` reads from a terminal with echo disabled. If a
// credential-shaped setting is ever added, redaction here is a prerequisite,
// not a follow-up.
type Setting struct {
	Flag    string // "backup-interval"
	Env     string // "ONSUITE_BACKUP_INTERVAL"
	Value   string // the live value, formatted for display
	Default string // the true compile-time default
	Doc     string // the flag's usage string
	Source  Source
}

// settingSpecs names every setting once: its flag, its environment variable,
// and its true default. collectSettings panics if this list and the FlagSet
// disagree, so a flag added without an entry here fails immediately rather
// than silently vanishing from the admin page.
var settingSpecs = []struct{ flag, env, def string }{
	{"addr", "ONSUITE_ADDR", defaultAddr},
	{"data-dir", "ONSUITE_DATA_DIR", defaultDataDir},
	{"tls-domain", "ONSUITE_TLS_DOMAIN", defaultTLSDomain},
	{"log-level", "ONSUITE_LOG_LEVEL", defaultLogLevel},
	{"backup-interval", "ONSUITE_BACKUP_INTERVAL", defaultBackupInterval.String()},
	{"backup-keep", "ONSUITE_BACKUP_KEEP", strconv.Itoa(defaultBackupKeep)},
	{"tls-http-addr", "ONSUITE_TLS_HTTP_ADDR", defaultTLSHTTPAddr},
	{"secure-cookies", "ONSUITE_SECURE_COOKIES", strconv.FormatBool(defaultSecureCookies)},
}

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

	// settings records how each value above was resolved, for the admin page.
	// It is unexported so a hand-built Config literal reports nothing rather
	// than reporting defaults it never actually applied.
	settings []Setting
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
	fs.StringVar(&c.Addr, "addr", envOr(getenv, "ONSUITE_ADDR", defaultAddr),
		"address to listen on")
	fs.StringVar(&c.DataDir, "data-dir", envOr(getenv, "ONSUITE_DATA_DIR", defaultDataDir),
		"directory holding the database and backups")
	fs.StringVar(&c.TLSDomain, "tls-domain", envOr(getenv, "ONSUITE_TLS_DOMAIN", defaultTLSDomain),
		"obtain a Let's Encrypt certificate for this domain and serve HTTPS directly")
	fs.StringVar(&level, "log-level", envOr(getenv, "ONSUITE_LOG_LEVEL", defaultLogLevel),
		"debug, info, warn or error")
	backupIntervalDefault, err := envDuration(getenv, "ONSUITE_BACKUP_INTERVAL", defaultBackupInterval)
	if err != nil {
		return Config{}, err
	}
	backupKeepDefault, err := envInt(getenv, "ONSUITE_BACKUP_KEEP", defaultBackupKeep)
	if err != nil {
		return Config{}, err
	}
	// Cookie security silently doing nothing on a typo (e.g. "1" or "TRUE")
	// is worse than refusing to start: an operator who thinks they set this
	// would otherwise get non-Secure cookies with no indication anything was
	// wrong.
	secureCookiesDefault, err := envBool(getenv, "ONSUITE_SECURE_COOKIES", defaultSecureCookies)
	if err != nil {
		return Config{}, err
	}

	fs.DurationVar(&c.BackupInterval, "backup-interval", backupIntervalDefault,
		"how often to snapshot the database; 0 disables the internal schedule")
	fs.IntVar(&c.BackupKeep, "backup-keep", backupKeepDefault,
		"how many snapshots to keep")
	fs.StringVar(&c.TLSHTTPAddr, "tls-http-addr", envOr(getenv, "ONSUITE_TLS_HTTP_ADDR", defaultTLSHTTPAddr),
		"plain-HTTP address for ACME challenges and HTTPS redirects; empty to disable")
	secureCookies := fs.Bool("secure-cookies", secureCookiesDefault,
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

	c.settings = collectSettings(fs, getenv, explicit, c)

	return c, nil
}

// collectSettings builds the introspection list. It runs at the end of Parse,
// once every value has been resolved, so Value is what the server will
// actually use rather than what was asked for.
func collectSettings(fs *flag.FlagSet, getenv func(string) string, explicit map[string]bool, c Config) []Setting {
	live := map[string]string{
		"addr":            c.Addr,
		"data-dir":        c.DataDir,
		"tls-domain":      c.TLSDomain,
		"log-level":       strings.ToLower(c.LogLevel.String()),
		"backup-interval": c.BackupInterval.String(),
		"backup-keep":     strconv.Itoa(c.BackupKeep),
		"tls-http-addr":   c.TLSHTTPAddr,
		"secure-cookies":  strconv.FormatBool(c.SecureCookies),
	}

	described := make(map[string]bool, len(settingSpecs))
	for _, spec := range settingSpecs {
		described[spec.flag] = true
	}
	// A flag with no descriptor would be invisible on the admin page, which
	// is exactly the drift this list exists to prevent. Every test in this
	// package calls Parse, so this fires the moment it happens.
	fs.VisitAll(func(f *flag.Flag) {
		if !described[f.Name] {
			panic("config: flag -" + f.Name + " has no entry in settingSpecs")
		}
	})

	out := make([]Setting, 0, len(settingSpecs))
	for _, spec := range settingSpecs {
		f := fs.Lookup(spec.flag)
		if f == nil {
			panic("config: settingSpecs names -" + spec.flag + ", which is not a flag")
		}

		s := Setting{
			Flag:    spec.flag,
			Env:     spec.env,
			Value:   live[spec.flag],
			Default: spec.def,
			Doc:     f.Usage,
			Source:  SourceDefault,
		}
		switch {
		case explicit[spec.flag]:
			s.Source = SourceFlag
		case envOr(getenv, spec.env, "") != "":
			s.Source = SourceEnv
		}

		// TLS computes two values regardless of what was asked for. Calling
		// those "default" while showing a different default is the one
		// genuinely confusing thing this table could say.
		if c.TLSEnabled() {
			switch spec.flag {
			case "addr":
				if s.Source == SourceDefault {
					s.Source = SourceDerived
				}
			case "secure-cookies":
				s.Source = SourceDerived
			}
		}

		out = append(out, s)
	}
	return out
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

// Settings describes every setting and where its live value came from. It is
// empty on a Config that was built as a literal rather than parsed.
func (c Config) Settings() []Setting {
	out := make([]Setting, len(c.settings))
	copy(out, c.settings)
	return out
}

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

func envDuration(getenv func(string) string, key string, def time.Duration) (time.Duration, error) {
	v := envOr(getenv, key, "")
	if v == "" {
		return def, nil
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return 0, fmt.Errorf("invalid %s %q: %w", key, v, err)
	}
	return d, nil
}

func envInt(getenv func(string) string, key string, def int) (int, error) {
	v := envOr(getenv, key, "")
	if v == "" {
		return def, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return 0, fmt.Errorf("invalid %s %q: %w", key, v, err)
	}
	return n, nil
}

// envBool parses a boolean environment variable with strconv.ParseBool,
// which accepts 1/t/T/TRUE/true/True and 0/f/F/FALSE/false/False — unlike a
// bare == "true" comparison, an unrecognised value is an error rather than a
// silent no-op.
func envBool(getenv func(string) string, key string, def bool) (bool, error) {
	v := envOr(getenv, key, "")
	if v == "" {
		return def, nil
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return false, fmt.Errorf("invalid %s %q: want a boolean (true/false/1/0/t/f/...)", key, v)
	}
	return b, nil
}
