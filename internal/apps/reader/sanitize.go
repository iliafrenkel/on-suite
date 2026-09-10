package reader

import (
	"sync"

	"github.com/microcosm-cc/bluemonday"
)

// policy is built once. bluemonday policies are safe for concurrent use, and
// building one per article would show up in a list of forty items.
var policy = sync.OnceValue(buildPolicy)

// buildPolicy is an allowlist, not a denylist. Anything not named here is
// removed, so a markup feature nobody thought about fails closed.
//
// Deliberately excluded:
//
//   - img. Excluded from this default policy so that passing remote images
//     through would never hand a publisher a tracking pixel pointed at this
//     household's IP. img is admitted only by policyWithImages, whose output
//     is always rewritten to the proxy before it reaches a template.
//   - style attributes and <style>. The suite's CSP forbids inline styles, so
//     they would be dead weight even if they were harmless, which they are not.
//   - iframe, object, embed, form, input. Nothing in an article needs them.
//   - svg. It is a scripting surface dressed as an image.
func buildPolicy() *bluemonday.Policy {
	p := bluemonday.NewPolicy()

	p.AllowStandardAttributes()
	p.AllowElements(
		"p", "br", "hr",
		"h1", "h2", "h3", "h4", "h5", "h6",
		"em", "i", "strong", "b", "u", "s", "del", "ins", "sub", "sup", "small", "mark",
		"blockquote", "q", "cite",
		"ul", "ol", "li", "dl", "dt", "dd",
		"pre", "code", "kbd", "samp", "var",
		"table", "thead", "tbody", "tfoot", "tr", "th", "td", "caption",
		"figure", "figcaption", "abbr", "time", "span", "div",
	)

	// Links: http(s) and mailto only, which is what rejects javascript: and
	// data: hrefs. RequireNoFollowOnLinks and friends add the rel; the target
	// is set here because a feed article always opens away from the reader.
	p.AllowAttrs("href").OnElements("a")
	p.AllowURLSchemes("http", "https", "mailto")
	p.RequireNoFollowOnLinks(true)
	p.RequireNoReferrerOnLinks(true)
	p.RequireCrossOriginAnonymous(true)
	p.AddTargetBlankToFullyQualifiedLinks(true)

	// Tables in articles are usually data, and colspan is load-bearing.
	p.AllowAttrs("colspan", "rowspan").OnElements("td", "th")
	p.AllowAttrs("datetime").OnElements("time")
	p.AllowAttrs("title").OnElements("abbr")

	return p
}

// SanitizeHTML returns raw with everything outside the allowlist removed. It
// is the only way publisher HTML is allowed to reach a template.
func SanitizeHTML(raw string) string {
	return policy().Sanitize(raw)
}

// policyWithImages is buildPolicy plus img. It is unexported and used only by
// SanitizeArticleHTML, which rewrites every src to the proxy immediately
// afterwards.
//
// Keeping it separate from SanitizeHTML is the safety property: a caller who
// reaches for the obvious function still cannot emit a remote image, so
// forgetting to rewrite fails closed rather than leaking.
var policyWithImages = sync.OnceValue(func() *bluemonday.Policy {
	p := buildPolicy()
	p.AllowAttrs("src", "alt", "title", "width", "height").OnElements("img")
	// Publishers commonly write site-relative image sources (e.g. "/img/a.png").
	// buildPolicy leaves relative URLs disallowed, which is right for links —
	// every feed item's own link is absolute — but here it would silently drop
	// the src. rewriteImages resolves whatever bluemonday keeps against the
	// article's URL and refuses anything that isn't http(s) afterwards, so
	// allowing relative values through does not weaken the fail-closed guarantee.
	p.AllowRelativeURLs(true)
	// No srcset or sizes: each would be a second list of URLs to rewrite for
	// no benefit at the sizes an article renders at here.
	return p
})
