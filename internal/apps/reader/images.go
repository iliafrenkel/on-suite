package reader

import (
	"crypto/sha256"
	"encoding/hex"
	"net/url"
	"strings"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
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
	sum := sha256.Sum256([]byte(srcURL))
	return hex.EncodeToString(sum[:16])
}

// SanitizeArticleHTML sanitizes publisher HTML and rewrites every image to the
// proxy, returning the HTML and a hash→absolute-source map for the caller to
// persist.
//
// It fails closed. If the fragment cannot be parsed after sanitizing — which
// should be impossible, since bluemonday parsed it with the same library — the
// result is SanitizeHTML's output, which has no images at all. Losing an image
// is a cosmetic failure; keeping a remote one is the tracking leak this whole
// mechanism exists to prevent.
func SanitizeArticleHTML(raw, baseURL string) (string, map[string]string) {
	if strings.TrimSpace(raw) == "" {
		return "", nil
	}
	clean := policyWithImages().Sanitize(raw)
	if !strings.Contains(clean, "<img") {
		return clean, nil
	}
	out, images, err := rewriteImages(clean, baseURL)
	if err != nil {
		return SanitizeHTML(raw), nil
	}
	return out, images
}

// rewriteImages replaces every img src with a proxy path.
func rewriteImages(fragment, baseURL string) (string, map[string]string, error) {
	base, err := url.Parse(baseURL)
	if err != nil {
		base = nil
	}

	ctx := &html.Node{Type: html.ElementNode, Data: "body", DataAtom: atom.Body}
	nodes, err := html.ParseFragment(strings.NewReader(fragment), ctx)
	if err != nil {
		return "", nil, err
	}

	images := map[string]string{}
	for _, n := range nodes {
		walkImages(n, base, images)
	}

	var buf strings.Builder
	for _, n := range nodes {
		if err := html.Render(&buf, n); err != nil {
			return "", nil, err
		}
	}
	return buf.String(), images, nil
}

// walkImages rewrites in place. An img whose source cannot be resolved to an
// absolute http(s) URL loses its src entirely rather than keeping it.
func walkImages(n *html.Node, base *url.URL, images map[string]string) {
	if n.Type == html.ElementNode && n.DataAtom == atom.Img {
		rewriteOneImage(n, base, images)
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		walkImages(c, base, images)
	}
}

func rewriteOneImage(n *html.Node, base *url.URL, images map[string]string) {
	var attrs []html.Attribute
	var src string
	for _, a := range n.Attr {
		if a.Key == "src" {
			src = strings.TrimSpace(a.Val)
			continue
		}
		attrs = append(attrs, a)
	}

	abs, ok := absoluteImageURL(src, base)
	if !ok {
		// No usable source. Keep the element (its alt text is still worth
		// something) but with nothing to load.
		n.Attr = attrs
		return
	}

	hash := ImageHash(abs)
	images[hash] = abs
	n.Attr = append(attrs,
		html.Attribute{Key: "src", Val: ImagePathPrefix + hash},
		// Lazy loading matters here: an article with thirty images would
		// otherwise ask the proxy for thirty outbound fetches at once.
		html.Attribute{Key: "loading", Val: "lazy"},
		html.Attribute{Key: "decoding", Val: "async"},
	)
}

// absoluteImageURL resolves a src against the article's own URL and accepts
// only http and https. Everything else — javascript:, data:, ftp:, empty —
// is refused, which is what makes the fail-closed test pass.
func absoluteImageURL(src string, base *url.URL) (string, bool) {
	if src == "" {
		return "", false
	}
	u, err := url.Parse(src)
	if err != nil {
		return "", false
	}
	if !u.IsAbs() && base != nil {
		u = base.ResolveReference(u)
	}
	if u.Scheme != "http" && u.Scheme != "https" || u.Host == "" {
		return "", false
	}
	return u.String(), true
}
