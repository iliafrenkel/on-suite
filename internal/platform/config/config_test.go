package config

import (
	"io"
	"log/slog"
	"path/filepath"
	"testing"
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

func TestDerivedPaths(t *testing.T) {
	c := Config{DataDir: "/var/lib/onsuite"}
	if got, want := c.DBPath(), filepath.FromSlash("/var/lib/onsuite/onsuite.db"); got != want {
		t.Errorf("DBPath() = %q, want %q", got, want)
	}
	if got, want := c.BackupDir(), filepath.FromSlash("/var/lib/onsuite/backups"); got != want {
		t.Errorf("BackupDir() = %q, want %q", got, want)
	}
}
