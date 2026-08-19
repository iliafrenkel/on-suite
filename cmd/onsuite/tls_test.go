package main

import (
	"io"
	"testing"

	"github.com/iliafrenkel/on-suite/internal/platform/config"
)

func TestTLSModeSelection(t *testing.T) {
	tests := []struct {
		name       string
		args       []string
		wantTLS    bool
		wantAddr   string
		wantSecure bool
	}{
		{"plain http by default", nil, false, ":8080", false},
		{"tls domain switches mode and port", []string{"-tls-domain", "on.example.com"}, true, ":443", true},
		{"an explicit address wins", []string{"-tls-domain", "on.example.com", "-addr", ":8443"}, true, ":8443", true},
		{"secure cookies behind a proxy", []string{"-secure-cookies"}, false, ":8080", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := config.Parse(tt.args, nil, io.Discard)
			if err != nil {
				t.Fatalf("Parse: %v", err)
			}
			if cfg.TLSEnabled() != tt.wantTLS {
				t.Errorf("TLSEnabled() = %v, want %v", cfg.TLSEnabled(), tt.wantTLS)
			}
			if cfg.Addr != tt.wantAddr {
				t.Errorf("Addr = %q, want %q", cfg.Addr, tt.wantAddr)
			}
			if cfg.SecureCookies != tt.wantSecure {
				t.Errorf("SecureCookies = %v, want %v", cfg.SecureCookies, tt.wantSecure)
			}
		})
	}
}

// TestTLSCacheDirIsInsideTheDataDirectory keeps the promise that the data
// directory is the entire persistent state: certificates included.
func TestTLSCacheDirIsInsideTheDataDirectory(t *testing.T) {
	cfg, err := config.Parse([]string{"-data-dir", "/var/lib/onsuite"}, nil, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := cfg.TLSCacheDir(), "/var/lib/onsuite/certs"; got != want {
		t.Errorf("TLSCacheDir() = %q, want %q", got, want)
	}
}
