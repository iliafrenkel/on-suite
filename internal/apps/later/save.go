package later

import (
	"bytes"
	"context"
	"mime"
	"strings"
	"time"

	xhtml "golang.org/x/net/html"
	"golang.org/x/net/html/atom"

	"github.com/iliafrenkel/on-suite/internal/platform/article"
	"github.com/iliafrenkel/on-suite/internal/platform/favicon"
	"github.com/iliafrenkel/on-suite/internal/platform/webfetch"
)

// ImagePathPrefix is where Later serves stored images, as it appears in
// rewritten HTML.
const ImagePathPrefix = "/later/img/"

func laterImageSrc(hash string) string { return ImagePathPrefix + hash }

// saveTimeout bounds fetch + extract. Saving is synchronous so the user
// knows at once whether the page was readable.
var saveTimeout = 15 * time.Second

// fetchArticle fetches and extracts pageURL. It never fails: anything that
// goes wrong becomes a link-only NewArticle carrying the reason, because a
// saved link the user can paste text into is better than an error.
func (a *App) fetchArticle(ctx context.Context, pageURL string) NewArticle {
	ctx, cancel := context.WithTimeout(ctx, saveTimeout)
	defer cancel()

	n := NewArticle{URL: pageURL}
	res, err := a.client.Get(ctx, pageURL, webfetch.GetOptions{})
	if err != nil {
		a.deps.Log.Info("later page fetch failed", "url", pageURL, "error", err)
		n.ExtractError = fetchReason(res, err)
		n.FaviconURL = favicon.Discover(nil, pageURL)
		return n
	}
	n.Title = pageTitle(res.Body)
	// Trust the page's own <link rel=icon> only when we ended up on the site
	// that was saved: a redirect to another host must not choose that site's
	// icon. Otherwise guess /favicon.ico on the saved site.
	if siteHost(res.FinalURL) == siteHost(pageURL) {
		n.FaviconURL = favicon.Discover(res.Body, res.FinalURL)
	} else {
		n.FaviconURL = favicon.Discover(nil, pageURL)
	}
	if !isHTML(res.ContentType) {
		a.deps.Log.Info("later page is not HTML", "url", pageURL, "content_type", res.ContentType)
		n.ExtractError = notHTMLReason(res.ContentType)
		return n
	}
	ex, err := article.Extract(res.Body, res.FinalURL, laterImageSrc)
	if err != nil {
		a.deps.Log.Info("later article extraction failed", "url", pageURL, "error", err)
		n.ExtractError = noArticleReason
		return n
	}
	if ex.Title != "" {
		n.Title = ex.Title
	}
	n.Byline, n.SiteName, n.Lang = ex.Byline, ex.SiteName, ex.Language
	n.ContentHTML, n.Images = ex.HTML, ex.Images
	return n
}

func isHTML(contentType string) bool {
	mt, _, err := mime.ParseMediaType(contentType)
	return err == nil && (mt == "text/html" || mt == "application/xhtml+xml")
}

// pageTitle is the document's <title>, for link-only items. Best effort.
func pageTitle(body []byte) string {
	doc, err := xhtml.Parse(bytes.NewReader(body))
	if err != nil {
		return ""
	}
	var title string
	var walk func(*xhtml.Node) bool
	walk = func(n *xhtml.Node) bool {
		if n.Type == xhtml.ElementNode && n.DataAtom == atom.Title && n.FirstChild != nil {
			title = strings.TrimSpace(n.FirstChild.Data)
			return true
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			if walk(c) {
				return true
			}
		}
		return false
	}
	walk(doc)
	return strings.Join(strings.Fields(title), " ")
}
