package config

import (
	"io"
	"log/slog"
	"path/filepath"
	"testing"
	"time"
)

func TestParsePrecedence(t *testing.T) {
	env := map[string]string{"ONSUITE_ADDR": ":9999", "ONSUITE_LOG_LEVEL": "warn"}
	getenv := func(k string) string { return env[k] }

	tests := []struct {
		name      string
		args      []string
		wantAddr  string
		wantLevel slog.Level
	}{
		{"env used when no flag", nil, ":9999", slog.LevelWarn},
		{"flag beats env", []string{"-addr", ":7000"}, ":7000", slog.LevelWarn},
		{"flag beats env for level", []string{"-log-level", "debug"}, ":9999", slog.LevelDebug},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, err := Parse(tt.args, getenv, io.Discard)
			if err != nil {
				t.Fatalf("Parse: %v", err)
			}
			if c.Addr != tt.wantAddr {
				t.Errorf("Addr = %q, want %q", c.Addr, tt.wantAddr)
			}
			if c.LogLevel != tt.wantLevel {
				t.Errorf("LogLevel = %v, want %v", c.LogLevel, tt.wantLevel)
			}
		})
	}
}

func TestParseDefaults(t *testing.T) {
	c, err := Parse(nil, nil, io.Discard)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if c.Addr != ":8080" {
		t.Errorf("Addr = %q, want :8080", c.Addr)
	}
	if c.DataDir != "./data" {
		t.Errorf("DataDir = %q, want ./data", c.DataDir)
	}
	if c.TLSDomain != "" {
		t.Errorf("TLSDomain = %q, want empty", c.TLSDomain)
	}
	if c.LogLevel != slog.LevelInfo {
		t.Errorf("LogLevel = %v, want info", c.LogLevel)
	}
}

func TestParseRejectsBadInput(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{"unknown log level", []string{"-log-level", "verbose"}},
		{"empty data dir", []string{"-data-dir", ""}},
		{"unknown flag", []string{"-nope"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := Parse(tt.args, nil, io.Discard); err == nil {
				t.Fatal("Parse succeeded, want error")
			}
		})
	}
}

func TestParseBackupSettings(t *testing.T) {
	c, err := Parse(nil, nil, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if c.BackupInterval != 24*time.Hour {
		t.Errorf("default BackupInterval = %v, want 24h", c.BackupInterval)
	}
	if c.BackupKeep != 7 {
		t.Errorf("default BackupKeep = %d, want 7", c.BackupKeep)
	}

	c, err = Parse([]string{"-backup-interval", "0", "-backup-keep", "30"}, nil, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if c.BackupInterval != 0 {
		t.Errorf("BackupInterval = %v, want 0 (disabled)", c.BackupInterval)
	}
	if c.BackupKeep != 30 {
		t.Errorf("BackupKeep = %d, want 30", c.BackupKeep)
	}

	getenv := func(k string) string {
		return map[string]string{"ONSUITE_BACKUP_INTERVAL": "6h", "ONSUITE_BACKUP_KEEP": "4"}[k]
	}
	if c, err = Parse(nil, getenv, io.Discard); err != nil {
		t.Fatal(err)
	}
	if c.BackupInterval != 6*time.Hour || c.BackupKeep != 4 {
		t.Errorf("env values ignored: %v, %d", c.BackupInterval, c.BackupKeep)
	}
}

func TestParseRejectsBadBackupSettings(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{"negative interval", []string{"-backup-interval", "-1h"}},
		{"absurdly short interval", []string{"-backup-interval", "2s"}},
		{"keep of zero", []string{"-backup-keep", "0"}},
		{"negative keep", []string{"-backup-keep", "-3"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := Parse(tt.args, nil, io.Discard); err == nil {
				t.Fatal("Parse accepted it")
			}
		})
	}
}

// TestParseSecureCookies guards a security-relevant flag: a value
// strconv.ParseBool doesn't recognize must be a loud error, not a silent
// "cookies stay non-Secure."
func TestParseSecureCookies(t *testing.T) {
	tests := []struct {
		name string
		env  string
		want bool
	}{
		{"unset", "", false},
		{"true", "true", true},
		{"1", "1", true},
		{"TRUE", "TRUE", true},
		{"t", "t", true},
		{"false", "false", false},
		{"0", "0", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			getenv := func(k string) string {
				if k == "ONSUITE_SECURE_COOKIES" {
					return tt.env
				}
				return ""
			}
			c, err := Parse(nil, getenv, io.Discard)
			if err != nil {
				t.Fatalf("Parse: %v", err)
			}
			if c.SecureCookies != tt.want {
				t.Errorf("SecureCookies = %v, want %v", c.SecureCookies, tt.want)
			}
		})
	}

	getenv := func(k string) string {
		if k == "ONSUITE_SECURE_COOKIES" {
			return "yes" // not one of strconv.ParseBool's accepted spellings
		}
		return ""
	}
	if _, err := Parse(nil, getenv, io.Discard); err == nil {
		t.Fatal(`Parse accepted ONSUITE_SECURE_COOKIES="yes", want an error`)
	}

	// The flag itself still parses with flag.Bool's own rules, unaffected by
	// this change.
	c, err := Parse([]string{"-secure-cookies"}, nil, io.Discard)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if !c.SecureCookies {
		t.Error("-secure-cookies flag did not set SecureCookies")
	}
}

// TestParseRejectsBadBackupEnv guards the same typo-visibility fix for
// ONSUITE_BACKUP_INTERVAL and ONSUITE_BACKUP_KEEP: an unparseable value must
// fail Parse, not silently fall back to the default.
func TestParseRejectsBadBackupEnv(t *testing.T) {
	tests := []struct {
		name string
		env  map[string]string
	}{
		{"bad interval", map[string]string{"ONSUITE_BACKUP_INTERVAL": "not-a-duration"}},
		{"bad keep", map[string]string{"ONSUITE_BACKUP_KEEP": "not-a-number"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			getenv := func(k string) string { return tt.env[k] }
			if _, err := Parse(nil, getenv, io.Discard); err == nil {
				t.Fatal("Parse accepted a malformed env value, want an error")
			}
		})
	}
}

func TestDerivedPaths(t *testing.T) {
	c := Config{DataDir: "/var/lib/onsuite"}
	if got, want := c.DBPath(), filepath.FromSlash("/var/lib/onsuite/onsuite.db"); got != want {
		t.Errorf("DBPath() = %q, want %q", got, want)
	}
	if got, want := c.BackupDir(), filepath.FromSlash("/var/lib/onsuite/backups"); got != want {
		t.Errorf("BackupDir() = %q, want %q", got, want)
	}
}

func settingFor(t *testing.T, c Config, flag string) Setting {
	t.Helper()
	for _, s := range c.Settings() {
		if s.Flag == flag {
			return s
		}
	}
	t.Fatalf("Settings() has no entry for -%s", flag)
	return Setting{}
}

func TestSettingsReportDefaultsWhenNothingIsSet(t *testing.T) {
	c, err := Parse(nil, nil, io.Discard)
	if err != nil {
		t.Fatal(err)
	}

	s := settingFor(t, c, "addr")
	if s.Value != ":8080" || s.Default != ":8080" {
		t.Errorf("addr Value/Default = %q/%q, want :8080/:8080", s.Value, s.Default)
	}
	if s.Source != SourceDefault {
		t.Errorf("addr Source = %v, want SourceDefault", s.Source)
	}
	if s.Env != "ONSUITE_ADDR" {
		t.Errorf("addr Env = %q", s.Env)
	}
	if s.Doc == "" {
		t.Error("addr Doc is empty; the flag's usage string should be carried through")
	}
}

func TestSettingsReportTheEnvironmentAsTheSource(t *testing.T) {
	env := func(k string) string {
		if k == "ONSUITE_ADDR" {
			return ":9999"
		}
		return ""
	}
	c, err := Parse(nil, env, io.Discard)
	if err != nil {
		t.Fatal(err)
	}

	s := settingFor(t, c, "addr")
	if s.Value != ":9999" {
		t.Errorf("addr Value = %q, want :9999", s.Value)
	}
	if s.Default != ":8080" {
		t.Errorf("addr Default = %q; the default must survive the environment overriding it", s.Default)
	}
	if s.Source != SourceEnv {
		t.Errorf("addr Source = %v, want SourceEnv", s.Source)
	}
}

func TestAnExplicitFlagBeatsTheEnvironmentInTheReportedSource(t *testing.T) {
	env := func(k string) string {
		if k == "ONSUITE_ADDR" {
			return ":9999"
		}
		return ""
	}
	c, err := Parse([]string{"-addr", ":7777"}, env, io.Discard)
	if err != nil {
		t.Fatal(err)
	}

	s := settingFor(t, c, "addr")
	if s.Value != ":7777" || s.Source != SourceFlag {
		t.Errorf("addr = %q from %v, want :7777 from SourceFlag", s.Value, s.Source)
	}
}

func TestTLSDerivedValuesAreReportedAsDerived(t *testing.T) {
	c, err := Parse([]string{"-tls-domain", "example.com"}, nil, io.Discard)
	if err != nil {
		t.Fatal(err)
	}

	addr := settingFor(t, c, "addr")
	if addr.Value != ":443" || addr.Source != SourceDerived {
		t.Errorf("addr = %q from %v, want :443 from SourceDerived", addr.Value, addr.Source)
	}
	secure := settingFor(t, c, "secure-cookies")
	if secure.Value != "true" || secure.Source != SourceDerived {
		t.Errorf("secure-cookies = %q from %v, want true from SourceDerived", secure.Value, secure.Source)
	}
}

func TestEverySettingIsDescribed(t *testing.T) {
	c, err := Parse(nil, nil, io.Discard)
	if err != nil {
		t.Fatal(err)
	}

	want := []string{
		"addr", "data-dir", "tls-domain", "log-level",
		"backup-interval", "backup-keep", "tls-http-addr", "secure-cookies",
	}
	got := c.Settings()
	if len(got) != len(want) {
		t.Fatalf("Settings() has %d entries, want %d", len(got), len(want))
	}
	for i, flag := range want {
		if got[i].Flag != flag {
			t.Errorf("Settings()[%d].Flag = %q, want %q", i, got[i].Flag, flag)
		}
		if got[i].Doc == "" {
			t.Errorf("-%s has no Doc", flag)
		}
	}
}

func TestSettingsOnAHandBuiltConfigIsEmptyRatherThanWrong(t *testing.T) {
	// Commands like `onsuite backup` build a Config literal without parsing
	// flags. Reporting made-up settings for one would be worse than none.
	if got := (Config{DataDir: "./data"}).Settings(); len(got) != 0 {
		t.Errorf("Settings() on a literal Config returned %d entries, want 0", len(got))
	}
}
