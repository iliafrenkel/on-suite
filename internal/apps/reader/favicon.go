package reader

import (
	"bytes"
	"net/url"
	"strings"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// DiscoverFavicon returns the best favicon URL for a site, given page HTML
// that may already be in hand.
//
// It never fetches anything itself — the caller decides whether fetching
// pageHTML was worth doing, which is the whole point: this function exists
// so that a poll cycle or an add-feed request that already has a page's
// bytes in memory (or has none at all) can still get a favicon URL for free.
//
// It returns the first <link rel="icon"> or <link rel="shortcut icon"> found
// in pageHTML, resolved against siteURL. If pageHTML is empty, unparsable,
// or has no such link, it falls back to "<origin>/favicon.ico" — the
// decades-old convention every browser itself falls back to. It returns ""
// only if siteURL itself does not parse into an absolute http(s) URL.
func DiscoverFavicon(pageHTML []byte, siteURL string) string {
	base, err := url.Parse(siteURL)
	if err != nil || (base.Scheme != "http" && base.Scheme != "https") || base.Host == "" {
		return ""
	}

	if len(pageHTML) > 0 {
		if href, ok := faviconLinkInPage(pageHTML, base); ok {
			return href
		}
	}

	fallback := *base
	fallback.Path = "/favicon.ico"
	fallback.RawQuery = ""
	fallback.Fragment = ""
	return fallback.String()
}

// faviconLinkInPage walks the parsed page the same way FeedsInPage
// (discover.go) does, looking for the first <link> whose rel identifies it
// as a favicon.
func faviconLinkInPage(page []byte, base *url.URL) (string, bool) {
	doc, err := html.Parse(bytes.NewReader(page))
	if err != nil {
		return "", false
	}

	var found string
	var ok bool
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if ok {
			return
		}
		if n.Type == html.ElementNode && n.DataAtom == atom.Link {
			var rel, href string
			for _, a := range n.Attr {
				switch strings.ToLower(a.Key) {
				case "rel":
					rel = strings.ToLower(a.Val)
				case "href":
					href = strings.TrimSpace(a.Val)
				}
			}
			if isIconRel(rel) && href != "" {
				if abs, resolved := resolveAbsoluteHTTPURL(href, base); resolved {
					found, ok = abs, true
					return
				}
			}
		}
		for child := n.FirstChild; child != nil && !ok; child = child.NextSibling {
			walk(child)
		}
	}
	walk(doc)
	return found, ok
}

// isIconRel matches "icon" and "shortcut icon" (two whitespace-separated
// tokens, "shortcut" and "icon") without also matching "apple-touch-icon",
// which is a single token and never equals "icon".
func isIconRel(rel string) bool {
	for _, tok := range strings.Fields(rel) {
		if tok == "icon" {
			return true
		}
	}
	return false
}
