// internal/apps/flash/media.go
package flash

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

// urlHash identifies a URL-attached media row: the same source URL always
// hashes to the same row, so an image or clip reused across cards (or
// imports) is fetched and stored once.
func urlHash(rawURL string) string {
	sum := sha256.Sum256([]byte(rawURL))
	return hex.EncodeToString(sum[:])
}

// contentHash identifies an uploaded media row by its own bytes, so
// uploading the exact same file twice reuses one row instead of storing it
// twice.
func contentHash(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// validMediaHash reports whether s could be one of our hashes: a full
// SHA-256 digest, 64 lowercase hex characters. Checked before any database
// work, so a probe against the serving route costs nothing.
func validMediaHash(s string) bool {
	if len(s) != 64 {
		return false
	}
	for _, c := range s {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

// contentTypeMatchesKind reports whether a sniffed content type is
// acceptable for kind ("image" or "audio") — "image/*" for an image,
// "audio/*" for audio. An unknown kind never matches.
func contentTypeMatchesKind(contentType, kind string) bool {
	switch kind {
	case MediaKindImage:
		return strings.HasPrefix(contentType, "image/")
	case MediaKindAudio:
		return strings.HasPrefix(contentType, "audio/")
	default:
		return false
	}
}
