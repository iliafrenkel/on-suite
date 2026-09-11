package reader

import (
	"bytes"
	"errors"
	"fmt"
	"net/url"
	"strings"

	readability "github.com/go-shiori/go-readability"
)

// ErrNotExtractable means the page had no article in it worth showing: a
// listing page, a login wall, a fetch that returned something that is not
// HTML, or a body so short that the feed's own summary is certainly better.
var ErrNotExtractable = errors.New("reader: no article found in page")

// minExtractedText is the shortest extraction worth storing.
//
// Readability will happily return a sentence from a page that is really a
// paywall notice or a navigation menu with nothing to read. Below this
// length, showing the publisher's own feed summary is the better answer, so
// extraction reports failure rather than replacing good content with worse.
const minExtractedText = 100

// Extracted is one article pulled out of its page, already sanitized and with
// its images pointed at the proxy.
type Extracted struct {
	HTML  string
	Title string
	// Images maps proxy hash to absolute publisher URL, exactly as
	// ParsedItem.Images does, so the store persists both the same way.
	Images map[string]string
	// TextLength is the extracted plain-text length, kept for the log line
	// that explains why a given page did or did not extract well.
	TextLength int
}

// ExtractArticle pulls the readable body out of a fetched page.
//
// It is the only place in this module that imports go-readability. That
// containment is deliberate: the library brings two effectively unmaintained
// transitive modules, and keeping it behind one function means R4 can be
// removed without touching anything R1-R3 built.
//
// It never fetches. The caller does that through Client, so every guard in the
// threat model still applies to the page this reads.
func ExtractArticle(body []byte, pageURL string) (Extracted, error) {
	if len(bytes.TrimSpace(body)) == 0 {
		return Extracted{}, fmt.Errorf("%w: empty body", ErrNotExtractable)
	}

	u, err := url.Parse(pageURL)
	if err != nil {
		return Extracted{}, fmt.Errorf("reader: parse page URL %q: %w", pageURL, err)
	}

	article, err := readability.FromReader(bytes.NewReader(body), u)
	if err != nil {
		// A parse failure here is an ordinary outcome for a page that is not
		// an article, so it becomes ErrNotExtractable rather than propagating
		// the library's own error to a handler.
		return Extracted{}, fmt.Errorf("%w: %v", ErrNotExtractable, err)
	}
	if strings.TrimSpace(article.Content) == "" || article.Length < minExtractedText {
		return Extracted{}, fmt.Errorf("%w: %d characters of text", ErrNotExtractable, article.Length)
	}

	// Readability has already resolved relative image sources against the page
	// URL. Passing the URL again is belt and braces, and costs nothing.
	clean, images := SanitizeArticleHTML(article.Content, pageURL)
	if strings.TrimSpace(clean) == "" {
		return Extracted{}, fmt.Errorf("%w: nothing survived sanitizing", ErrNotExtractable)
	}

	return Extracted{
		HTML:       clean,
		Title:      strings.TrimSpace(article.Title),
		Images:     images,
		TextLength: article.Length,
	}, nil
}
