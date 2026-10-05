package webfetch

import (
	"crypto/sha256"
	"encoding/hex"
	"net/url"
)

// ResolveHTTPURL resolves raw against base and accepts http and https only —
// javascript:, data: and file: are refused so they never reach a fetcher or
// an img src.
func ResolveHTTPURL(raw string, base *url.URL) (string, bool) {
	if raw == "" {
		return "", false
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", false
	}
	if !u.IsAbs() && base != nil {
		u = base.ResolveReference(u)
	}
	if (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return "", false
	}
	return u.String(), true
}

// URLHash identifies a remote resource (an image, a favicon) by its source
// URL: 128 bits of SHA-256, hex encoded. Content-addressing the URL rather
// than assigning an id lets HTML be rewritten before anything touches a
// database, and lets a proxy route take a hash instead of a URL, so no input
// can make it fetch something this server never saw.
func URLHash(srcURL string) string {
	sum := sha256.Sum256([]byte(srcURL))
	return hex.EncodeToString(sum[:16])
}

// ValidURLHash reports whether s could be a URLHash. Proxy routes check it
// before any database work, so a probe costs nothing.
func ValidURLHash(s string) bool {
	if len(s) != 32 {
		return false
	}
	for _, c := range s {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}
