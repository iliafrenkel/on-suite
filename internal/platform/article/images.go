package article

import (
	"net/url"
	"strings"

	"github.com/iliafrenkel/on-suite/internal/platform/webfetch"
	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// ImageSrc builds the src an image is served from, given its URLHash. Each
// app passes its own, so Reader's images stay under /reader/img/ and
// Later's under /later/img/.
type ImageSrc func(hash string) string

// SanitizeWithImages sanitizes publisher HTML and rewrites every image to
// src(hash), returning the HTML and a hash→absolute-source map for the
// caller to persist.
//
// It fails closed. If the fragment cannot be parsed after sanitizing — which
// should be impossible, since bluemonday parsed it with the same library — the
// result is SanitizeHTML's output, which has no images at all. Losing an image
// is a cosmetic failure; keeping a remote one is the tracking leak this whole
// mechanism exists to prevent.
func SanitizeWithImages(raw, baseURL string, src ImageSrc) (string, map[string]string) {
	if strings.TrimSpace(raw) == "" {
		return "", nil
	}
	absolutized, err := absolutizeImageSources(raw, baseURL)
	if err != nil {
		// Fall through with the original markup; bluemonday will still run
		// on it below, it just may drop a relative img src it otherwise
		// could have resolved. Parsing raw HTML with html.ParseFragment
		// essentially never fails, so this is a belt-and-suspenders path.
		absolutized = raw
	}
	clean := policyWithImages().Sanitize(absolutized)
	if !strings.Contains(clean, "<img") {
		return clean, nil
	}
	out, images, err := rewriteImages(clean, baseURL, src)
	if err != nil {
		return SanitizeHTML(raw), nil
	}
	return out, images
}

// absolutizeImageSources rewrites every <img src> in the raw fragment to an
// absolute http(s) URL before bluemonday ever sees it, so policyWithImages
// never needs to allow relative URLs through — that switch is policy-wide in
// bluemonday, not per-element, and would just as happily let a relative
// <a href> survive sanitizing unresolved. Doing the absolutizing here instead
// keeps "links are always absolute" true for policyWithImages exactly as it
// already is for the default policy.
//
// An img whose src cannot be resolved to a valid http(s) absolute URL has its
// src attribute removed outright, so bluemonday's later pass drops the image
// the same way it does today — this is not a new fail-closed path, just an
// earlier one.
func absolutizeImageSources(fragment, baseURL string) (string, error) {
	base, _ := url.Parse(baseURL)
	return mutateHTMLFragment(fragment, func(n *html.Node) {
		if n.Type == html.ElementNode && n.DataAtom == atom.Img {
			absolutizeOneImageSrc(n, base)
		}
	})
}

func absolutizeOneImageSrc(n *html.Node, base *url.URL) {
	var attrs []html.Attribute
	var src string
	for _, a := range n.Attr {
		if a.Key == "src" {
			src = strings.TrimSpace(a.Val)
			continue
		}
		attrs = append(attrs, a)
	}

	abs, ok := webfetch.ResolveHTTPURL(src, base)
	if !ok {
		n.Attr = attrs
		return
	}
	n.Attr = append(attrs, html.Attribute{Key: "src", Val: abs})
}

// rewriteImages replaces every img src with src(hash).
func rewriteImages(fragment, baseURL string, src ImageSrc) (string, map[string]string, error) {
	base, _ := url.Parse(baseURL)
	images := map[string]string{}
	out, err := mutateHTMLFragment(fragment, func(n *html.Node) {
		if n.Type == html.ElementNode && n.DataAtom == atom.Img {
			rewriteOneImage(n, base, images, src)
		}
	})
	if err != nil {
		return "", nil, err
	}
	return out, images, nil
}

func rewriteOneImage(n *html.Node, base *url.URL, images map[string]string, imgSrc ImageSrc) {
	var attrs []html.Attribute
	var src string
	for _, a := range n.Attr {
		if a.Key == "src" {
			src = strings.TrimSpace(a.Val)
			continue
		}
		attrs = append(attrs, a)
	}

	abs, ok := webfetch.ResolveHTTPURL(src, base)
	if !ok {
		// No usable source. Keep the element (its alt text is still worth
		// something) but with nothing to load.
		n.Attr = attrs
		return
	}

	hash := webfetch.URLHash(abs)
	images[hash] = abs
	n.Attr = append(attrs,
		html.Attribute{Key: "src", Val: imgSrc(hash)},
		// Lazy loading matters here: an article with thirty images would
		// otherwise ask the image endpoint for thirty outbound fetches at once.
		html.Attribute{Key: "loading", Val: "lazy"},
		html.Attribute{Key: "decoding", Val: "async"},
	)
}

// mutateHTMLFragment parses an HTML fragment, runs fn on every node in its tree,
// and renders the transformed nodes back to an HTML string.
func mutateHTMLFragment(fragment string, fn func(n *html.Node)) (string, error) {
	ctx := &html.Node{Type: html.ElementNode, Data: "body", DataAtom: atom.Body}
	nodes, err := html.ParseFragment(strings.NewReader(fragment), ctx)
	if err != nil {
		return "", err
	}
	for _, n := range nodes {
		walkNodeTree(n, fn)
	}
	var buf strings.Builder
	for _, n := range nodes {
		if err := html.Render(&buf, n); err != nil {
			return "", err
		}
	}
	return buf.String(), nil
}

func walkNodeTree(n *html.Node, fn func(*html.Node)) {
	fn(n)
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		walkNodeTree(c, fn)
	}
}
