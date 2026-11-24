package web

import (
	"net/http/httptest"
	"testing"
)

func TestClientIP(t *testing.T) {
	cases := []struct {
		name, remote, xff, want string
	}{
		{"loopback peer trusts the right-most forwarded entry", "127.0.0.1:5000", "1.1.1.1, 2.2.2.2", "2.2.2.2"},
		{"IPv6 loopback peer", "[::1]:5000", "2001:db8::7", "2001:db8::7"},
		{"entry is trimmed", "127.0.0.1:5000", "1.1.1.1 ,  2.2.2.2  ", "2.2.2.2"},
		{"non-loopback peer ignores the header", "192.0.2.9:5000", "1.1.1.1, 2.2.2.2", "192.0.2.9"},
		{"loopback with an invalid entry falls back", "127.0.0.1:5000", "1.1.1.1, not-an-ip", "127.0.0.1"},
		{"loopback with an empty right-most entry falls back", "127.0.0.1:5000", "1.1.1.1, ", "127.0.0.1"},
		{"loopback without the header", "127.0.0.1:5000", "", "127.0.0.1"},
		{"RemoteAddr without a port", "192.0.2.9", "", "192.0.2.9"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := httptest.NewRequest("GET", "/", nil)
			r.RemoteAddr = c.remote
			if c.xff != "" {
				r.Header.Set("X-Forwarded-For", c.xff)
			}
			if got := ClientIP(r); got != c.want {
				t.Errorf("ClientIP = %q, want %q", got, c.want)
			}
		})
	}
}

// When several X-Forwarded-For headers arrive, the proxy's own append is in
// the last one, so the right-most entry across all of them is the one to use.
func TestClientIPMultipleHeaders(t *testing.T) {
	r := httptest.NewRequest("GET", "/", nil)
	r.RemoteAddr = "127.0.0.1:5000"
	r.Header.Add("X-Forwarded-For", "1.1.1.1")
	r.Header.Add("X-Forwarded-For", "3.3.3.3")
	if got := ClientIP(r); got != "3.3.3.3" {
		t.Errorf("ClientIP = %q, want 3.3.3.3", got)
	}
}
