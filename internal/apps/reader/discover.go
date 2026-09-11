package reader

import (
	"bytes"
	"net/url"
	"sort"
	"strings"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// ProbePaths are tried when a page advertises no feed at all. They are the
// paths that actually appear in the wild, in rough order of likelihood, and
// the list is short on purpose: every entry is another request sent to
// somebody's server on the strength of a guess.
var ProbePaths = []string{"/feed", "/feed.xml", "/rss.xml", "/atom.xml", "/index.xml"}

// FeedCandidate is a feed a page advertises.
type FeedCandidate struct {
	URL   string
	Title string
}

// feedLinkTypes are the link types that mean "this is a feed".
//
// application/json is deliberately absent: it appears on rel="alternate" links
// that are not feeds at all, so treating it as one would subscribe people to
// API endpoints. JSON Feed's own type, application/feed+json, is specific
// enough to trust.
var feedLinkTypes = map[string]bool{
	"application/rss+xml":   true,
	"application/atom+xml":  true,
	"application/feed+json": true,
}

// FeedsInPage finds the feeds a web page advertises, best first.
//
// It never fetches. The caller does that through Client, so every guard in the
// threat model applies to the page this reads and to whatever is done with the
// candidates afterwards.
func FeedsInPage(page []byte, pageURL string) []FeedCandidate {
	base, err := url.Parse(pageURL)
	if err != nil {
		base = nil
	}
	doc, err := html.Parse(bytes.NewReader(page))
	if err != nil {
		return nil
	}

	var out []FeedCandidate
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode && n.DataAtom == atom.Link {
			if c, ok := candidateFrom(n, base); ok {
				out = append(out, c)
			}
		}
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(doc)

	// Stable sort so ties keep document order, which is the publisher's own
	// idea of which feed matters most.
	sort.SliceStable(out, func(i, j int) bool {
		return candidateRank(out[i]) < candidateRank(out[j])
	})
	return out
}

// candidateRank puts the feed someone actually meant first.
//
// A WordPress blog advertises its comments feed beside its main one, and
// subscribing to comments because it happened to come second in the HTML is a
// bad default: it is annoying to notice and annoying to undo.
func candidateRank(c FeedCandidate) int {
	hay := strings.ToLower(c.URL + " " + c.Title)
	switch {
	case strings.Contains(hay, "comment"):
		return 2
	default:
		return 0
	}
}

func candidateFrom(n *html.Node, base *url.URL) (FeedCandidate, bool) {
	var rel, typ, href, title string
	for _, a := range n.Attr {
		switch strings.ToLower(a.Key) {
		case "rel":
			rel = strings.ToLower(a.Val)
		case "type":
			typ = strings.ToLower(strings.TrimSpace(a.Val))
		case "href":
			href = strings.TrimSpace(a.Val)
		case "title":
			title = strings.TrimSpace(a.Val)
		}
	}
	if !strings.Contains(rel, "alternate") || !feedLinkTypes[typ] || href == "" {
		return FeedCandidate{}, false
	}

	abs, ok := absoluteHTTPURL(href, base)
	if !ok {
		return FeedCandidate{}, false
	}
	return FeedCandidate{URL: abs, Title: title}, true
}

// absoluteHTTPURL resolves href against the page and accepts http and https
// only — a discovered href is about to be handed to a fetcher, so javascript:,
// data: and file: are refused here rather than relied on being refused later.
func absoluteHTTPURL(href string, base *url.URL) (string, bool) {
	u, err := url.Parse(href)
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
