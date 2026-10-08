package article

import (
	"bytes"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"

	readability "github.com/go-shiori/go-readability"
)

// ErrNotExtractable means the page had no article in it worth showing: a
// listing page, a login wall, a fetch that returned something that is not
// HTML, or a body so short that any summary the caller already has is
// certainly better.
var ErrNotExtractable = errors.New("article: no article found in page")

// minExtractedText is the shortest extraction worth storing.
//
// Readability will happily return a sentence from a page that is really a
// paywall notice or a navigation menu with nothing to read. Below this
// length, keeping whatever the caller already has is the better answer, so
// extraction reports failure rather than replacing good content with worse.
const minExtractedText = 100

// Extracted is one article pulled out of its page, already sanitized and with
// its images rewritten through the caller's ImageSrc.
type Extracted struct {
	HTML string
	// Title is what readability found on the page. Callers decide whether to
	// use it; Reader deliberately keeps the feed's own title, since a page's
	// own <title> is often noisier (site name suffixes, ad-driven rewrites).
	Title string
	// Images maps image hash to absolute publisher URL, for the caller
	// to persist.
	Images map[string]string
	// Byline and SiteName are what readability found, for callers that show
	// them (ON Later does; ON Reader doesn't).
	Byline   string
	SiteName string
	// Language is the page's <html lang>, cleaned by languageTag: a BCP 47
	// tag such as "ru" or "he-IL", or "" when the page didn't say or said
	// something that isn't one. It's publisher-controlled, so callers can
	// put it in a lang attribute but shouldn't trust it further than that.
	Language string
	// TextLength is the extracted plain-text length, kept for the log line
	// that explains why a given page did or did not extract well.
	TextLength int
}

// Extract pulls the readable body out of a fetched page.
//
// It is the only place in this module that imports go-readability. That
// containment is deliberate: the library brings two effectively unmaintained
// transitive modules, and keeping it behind one function means extraction can
// be removed or replaced without touching its callers.
//
// It never fetches. The caller does that through webfetch, so every guard in
// the threat model still applies to the page this reads.
func Extract(body []byte, pageURL string, src ImageSrc) (Extracted, error) {
	if len(bytes.TrimSpace(body)) == 0 {
		return Extracted{}, fmt.Errorf("%w: empty body", ErrNotExtractable)
	}

	u, err := url.Parse(pageURL)
	if err != nil {
		return Extracted{}, fmt.Errorf("article: parse page URL %q: %w", pageURL, err)
	}

	page, err := readability.FromReader(bytes.NewReader(body), u)
	if err != nil {
		// A parse failure here is an ordinary outcome for a page that is not
		// an article, so it becomes ErrNotExtractable rather than propagating
		// the library's own error to a handler.
		return Extracted{}, fmt.Errorf("%w: %v", ErrNotExtractable, err)
	}
	if strings.TrimSpace(page.Content) == "" || page.Length < minExtractedText {
		return Extracted{}, fmt.Errorf("%w: %d characters of text", ErrNotExtractable, page.Length)
	}

	// Readability has already resolved relative image sources against the page
	// URL. Passing the URL again is belt and braces, and costs nothing.
	clean, images := SanitizeWithImages(page.Content, pageURL, src)
	if strings.TrimSpace(clean) == "" {
		return Extracted{}, fmt.Errorf("%w: nothing survived sanitizing", ErrNotExtractable)
	}

	return Extracted{
		HTML:       clean,
		Title:      strings.TrimSpace(page.Title),
		Images:     images,
		Byline:     strings.TrimSpace(page.Byline),
		SiteName:   strings.TrimSpace(page.SiteName),
		Language:   languageTag(page.Language),
		TextLength: page.Length,
	}, nil
}

// langTagPattern is the shape of a BCP 47 language tag, loosely: a 2-3
// letter primary subtag and up to a few more subtags of letters and digits.
// It doesn't check the subtags against the registry; a browser ignores a
// well-formed tag it doesn't know.
var langTagPattern = regexp.MustCompile(`^[A-Za-z]{2,3}(-[A-Za-z0-9]{1,8}){0,4}$`)

// languageTag cleans a page's lang attribute into a tag, or "" if it isn't
// one. "en_US" is a common mistake for "en-US" and is read as that.
func languageTag(raw string) string {
	tag := strings.ReplaceAll(strings.TrimSpace(raw), "_", "-")
	if len(tag) > 35 || !langTagPattern.MatchString(tag) {
		return ""
	}
	return tag
}
