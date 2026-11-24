package web

import (
	"net"
	"net/http"
	"strings"
)

// ClientIP is the best available identifier for rate limiting: login, the
// /account password check and the OAuth endpoints all key on it.
//
// The documented deployment puts a reverse proxy (Caddy) on the same host in
// front of the binary, so every request arrives from loopback and RemoteAddr
// alone would put the whole internet in one budget. When the peer is
// loopback, the right-most X-Forwarded-For entry is the one our own proxy
// appended — the address it actually saw — so that is the client. Entries to
// its left are whatever the client claimed and are never read. From any
// other peer the header is ignored entirely: anyone can send it, and
// trusting it would make every limiter trivially bypassable (spec
// 2026-11-24-ai-access-mcp-design.md, "Rate limits").
func ClientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	if ip := net.ParseIP(host); ip == nil || !ip.IsLoopback() {
		return host
	}
	values := r.Header.Values("X-Forwarded-For")
	if len(values) == 0 {
		return host
	}
	last := values[len(values)-1]
	if i := strings.LastIndexByte(last, ','); i >= 0 {
		last = last[i+1:]
	}
	last = strings.TrimSpace(last)
	if net.ParseIP(last) == nil {
		return host
	}
	return last
}
