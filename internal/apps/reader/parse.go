package reader

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/mmcdole/gofeed"
)

// ParsedFeed is one fetched feed document, normalised and sanitized. Nothing
// downstream ever sees a gofeed type: this package's own shapes are what the
// store and the templates consume.
type ParsedFeed struct {
	Title   string
	SiteURL string
	Items   []ParsedItem
}

// ParsedItem is one article, with both bodies already sanitized.
type ParsedItem struct {
	GUID        string
	URL         string
	Title       string
	Author      string
	PublishedAt time.Time
	SummaryHTML string
	ContentHTML string
	// Images maps a proxy hash to the absolute publisher URL it stands for,
	// for every image in SummaryHTML and ContentHTML. SaveItems persists it;
	// nothing else in this package writes to the database.
	Images map[string]string
}

// ParseFeed turns feed bytes into normalised items.
//
// It never touches the network. gofeed can fetch a URL itself; using only its
// parser is deliberate, because every outbound request in this app must go
// through Client and its guards.
func ParseFeed(body []byte, feedURL string) (ParsedFeed, error) {
	parsed, err := gofeed.NewParser().Parse(bytes.NewReader(body))
	if err != nil {
		return ParsedFeed{}, fmt.Errorf("reader: parse feed %s: %w", feedURL, err)
	}

	out := ParsedFeed{
		Title:   strings.TrimSpace(parsed.Title),
		SiteURL: strings.TrimSpace(parsed.Link),
	}
	for _, item := range parsed.Items {
		if item == nil {
			continue
		}
		// The item's own URL is the base for relative image sources, falling
		// back to the feed's site URL for items that carry no link.
		base := strings.TrimSpace(item.Link)
		if base == "" {
			base = out.SiteURL
		}
		summary, summaryImages := SanitizeArticleHTML(item.Description, base)
		content, contentImages := SanitizeArticleHTML(item.Content, base)

		images := summaryImages
		if images == nil {
			images = map[string]string{}
		}
		for h, u := range contentImages {
			images[h] = u
		}
		if len(images) == 0 {
			images = nil
		}

		out.Items = append(out.Items, ParsedItem{
			GUID:        itemGUID(item),
			URL:         strings.TrimSpace(item.Link),
			Title:       strings.TrimSpace(item.Title),
			Author:      itemAuthor(item),
			PublishedAt: itemPublished(item),
			SummaryHTML: summary,
			ContentHTML: content,
			Images:      images,
		})
	}
	return out, nil
}

// itemGUID picks the most stable identifier available.
//
// The order matters more than it looks: an unstable id means every poll
// re-inserts every item, so the fallback hashes only fields that do not change
// between two fetches of the same document.
func itemGUID(item *gofeed.Item) string {
	if g := strings.TrimSpace(item.GUID); g != "" {
		return g
	}
	if l := strings.TrimSpace(item.Link); l != "" {
		return l
	}
	sum := sha256.Sum256([]byte(strings.TrimSpace(item.Title) + "\x00" + rawDate(item)))
	return "sha256:" + hex.EncodeToString(sum[:])
}

// rawDate is the publisher's own date string, not a parsed time, so the
// synthesised GUID does not move if date parsing later improves.
func rawDate(item *gofeed.Item) string {
	if item.Published != "" {
		return item.Published
	}
	return item.Updated
}

func itemAuthor(item *gofeed.Item) string {
	if len(item.Authors) > 0 && item.Authors[0] != nil {
		if n := strings.TrimSpace(item.Authors[0].Name); n != "" {
			return n
		}
	}
	return ""
}

// itemPublished prefers the publication date and falls back to the update
// date. gofeed has already tried a long list of formats; an item with neither
// gets a zero time, which the store replaces with the fetch time so it still
// sorts sensibly.
func itemPublished(item *gofeed.Item) time.Time {
	if item.PublishedParsed != nil {
		return item.PublishedParsed.UTC()
	}
	if item.UpdatedParsed != nil {
		return item.UpdatedParsed.UTC()
	}
	return time.Time{}
}
