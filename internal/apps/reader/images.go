package reader

import (
	"github.com/iliafrenkel/on-suite/internal/platform/article"
	"github.com/iliafrenkel/on-suite/internal/platform/webfetch"
)

// ImagePathPrefix is where the proxy is mounted, as it appears in rewritten
// HTML. It is a constant rather than built from ID so that grepping for the
// literal finds both the route and the rewriter.
const ImagePathPrefix = "/reader/img/"

// ImageHash identifies an image by its source URL.
//
// 128 bits of SHA-256, hex encoded. Content-addressing the URL rather than
// assigning an id is what lets rewriting happen in parse.go, which never
// touches the database — see the plan's note on deviating from the spec here.
func ImageHash(srcURL string) string {
	return webfetch.URLHash(srcURL)
}

func readerImageSrc(hash string) string { return ImagePathPrefix + hash }

// SanitizeArticleHTML is article.SanitizeWithImages bound to Reader's proxy.
func SanitizeArticleHTML(raw, baseURL string) (string, map[string]string) {
	return article.SanitizeWithImages(raw, baseURL, readerImageSrc)
}
