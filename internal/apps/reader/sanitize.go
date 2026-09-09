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
//   - img. R1 has no image proxy, and passing remote images through would hand
//     every publisher a tracking pixel pointed at this household's IP. R3 adds
//     img together with the proxy that makes it safe.
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
