package web

import (
	"net"
	"net/http"
	"net/netip"
	"strings"
)

// ClientIP is the best available identifier for the client: logs record it,
// and RateKey derives the rate limiters' key from it.
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

// RateKey is ClientIP as a rate limiter should count it: login, the /account
// password check and the OAuth endpoints all key on it. An IPv6 client is
// keyed by its /64, the block a single connection or server is normally
// given: per address, rotating within it would reset the budget at will
// (#629). IPv4 (including IPv4-mapped IPv6) and loopback stay per address.
func RateKey(r *http.Request) string {
	ip := ClientIP(r)
	addr, err := netip.ParseAddr(ip)
	if err != nil {
		return ip
	}
	addr = addr.Unmap()
	if addr.Is4() || addr.IsLoopback() {
		return addr.String()
	}
	return netip.PrefixFrom(addr, 64).Masked().String()
}
