package books

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/iliafrenkel/on-suite/internal/platform/webfetch"
)

// MaxCandidates is how many search results Open Library is asked for.
const MaxCandidates = 10

// coverTimeout bounds one image fetch: an Open Library cover (which usually
// redirects twice, to archive.org storage) or a pasted image address.
var coverTimeout = 10 * time.Second

// maxJSONBytes bounds a search or work answer; ten trimmed results are a
// few kilobytes.
const maxJSONBytes = 1 << 20

// searchFields is what search.json is asked to return — only what a
// Candidate needs.
const searchFields = "key,title,subtitle,author_name,first_publish_year,number_of_pages_median,isbn,cover_i,cover_edition_key"

// OpenLibrary is ON Books' client for openlibrary.org (search, work
// descriptions) and covers.openlibrary.org. Every request goes through Web,
// so the suite's SSRF guard and size caps apply (spec "Architecture").
// Base and Covers are fields so tests can point them at httptest.
type OpenLibrary struct {
	Web     *webfetch.Client
	Base    string        // "https://openlibrary.org"
	Covers  string        // "https://covers.openlibrary.org"
	Timeout time.Duration // per search or description request
}

// Candidate is one search result, cleaned up and ready to pre-fill the
// book form. Zero and "" mean Open Library didn't say.
type Candidate struct {
	WorkID, EditionID string
	Title, Subtitle   string
	Authors           string
	Year, Pages       int
	ISBN              string // ISBN-13
	CoverID           int64
}

type olDoc struct {
	Key              string   `json:"key"`
	Title            string   `json:"title"`
	Subtitle         string   `json:"subtitle"`
	AuthorName       []string `json:"author_name"`
	FirstPublishYear int      `json:"first_publish_year"`
	Pages            int      `json:"number_of_pages_median"`
	ISBN             []string `json:"isbn"`
	CoverI           int64    `json:"cover_i"`
	CoverEditionKey  string   `json:"cover_edition_key"`
}

// candidate cleans one doc; a doc with no title is no use and is skipped.
func (d olDoc) candidate() (Candidate, bool) {
	title := oneLine(d.Title)
	if title == "" {
		return Candidate{}, false
	}
	c := Candidate{
		WorkID:    olID(strings.TrimPrefix(d.Key, "/works/"), 'W'),
		EditionID: olID(d.CoverEditionKey, 'M'),
		Title:     title,
		Subtitle:  oneLine(d.Subtitle),
		Authors:   oneLine(strings.Join(d.AuthorName, ", ")),
		Year:      d.FirstPublishYear,
		Pages:     d.Pages,
		ISBN:      pickISBN(d.ISBN),
		CoverID:   d.CoverI,
	}
	if c.Year < 1 || c.Year > MaxYear {
		c.Year = 0
	}
	if c.Pages < 1 || c.Pages > MaxPages {
		c.Pages = 0
	}
	if c.CoverID < 0 {
		c.CoverID = 0
	}
	return c, true
}

// pickISBN prefers an ISBN-13 Open Library lists itself, then any ISBN-10
// it lists, converted.
func pickISBN(list []string) string {
	for _, s := range list {
		if len(s) == 13 {
			if v, ok := ISBN13(s); ok {
				return v
			}
		}
	}
	for _, s := range list {
		if v, ok := ISBN13(s); ok {
			return v
		}
	}
	return ""
}

// Search asks Open Library for books matching q — a title, an author, or
// an ISBN. Blank q asks nothing. A failure (timeout, error status, bad
// JSON) is an error; finding nothing is not.
func (o *OpenLibrary) Search(ctx context.Context, q string) ([]Candidate, error) {
	q = strings.TrimSpace(q)
	if q == "" {
		return nil, nil
	}
	ctx, cancel := context.WithTimeout(ctx, o.Timeout)
	defer cancel()
	v := url.Values{"q": {q}, "fields": {searchFields}, "limit": {strconv.Itoa(MaxCandidates)}}
	res, err := o.Web.Get(ctx, o.Base+"/search.json?"+v.Encode(),
		webfetch.GetOptions{Accept: "application/json", MaxBytes: maxJSONBytes})
	if err != nil {
		return nil, fmt.Errorf("books: open library search: %w", err)
	}
	var body struct {
		Docs []olDoc `json:"docs"`
	}
	if err := json.Unmarshal(res.Body, &body); err != nil {
		return nil, fmt.Errorf("books: open library search: %w", err)
	}
	out := []Candidate{}
	for _, d := range body.Docs {
		if c, ok := d.candidate(); ok {
			out = append(out, c)
		}
	}
	return out, nil
}

// Description returns a work's description as plain text ("" when it has
// none). Open Library stores it either as a string or as
// {"type": "/type/text", "value": "…"}.
func (o *OpenLibrary) Description(ctx context.Context, workID string) (string, error) {
	if olID(workID, 'W') == "" {
		return "", ErrInvalid
	}
	ctx, cancel := context.WithTimeout(ctx, o.Timeout)
	defer cancel()
	res, err := o.Web.Get(ctx, o.Base+"/works/"+workID+".json",
		webfetch.GetOptions{Accept: "application/json", MaxBytes: maxJSONBytes})
	if err != nil {
		return "", fmt.Errorf("books: open library work: %w", err)
	}
	var body struct {
		Description json.RawMessage `json:"description"`
	}
	if err := json.Unmarshal(res.Body, &body); err != nil {
		return "", fmt.Errorf("books: open library work: %w", err)
	}
	text := plainDescription(descriptionText(body.Description))
	if r := []rune(text); len(r) > MaxDescriptionRunes {
		text = strings.TrimSpace(string(r[:MaxDescriptionRunes]))
	}
	return text, nil
}

// Open Library descriptions are Markdown: emphasis, links, reference
// links with their definitions, and "----------" rules before an "Also
// contained in" list. A book's description here is plain text (spec "Data
// model"), so plainDescription keeps the words and drops the markup.
var (
	mdTrailing = regexp.MustCompile(`[ \t]+\n`)
	mdRefDef   = regexp.MustCompile(`(?m)^[ \t]*\[[^\]\n]+\]:[ \t]*\S.*$`) // [1]: https://…
	mdRule     = regexp.MustCompile(`(?m)^[ \t]*[-*_]{3,}[ \t]*$`)         // ----------
	mdLink     = regexp.MustCompile(`\[([^\]\n]+)\]\([^)\n]*\)`)           // [text](url)
	mdRefLink  = regexp.MustCompile(`\[([^\]\n]+)\]\[[^\]\n]*\]`)          // [text][1]
	mdItalic   = regexp.MustCompile(`\*([^*\n]+)\*`)                       // *text*
	mdBlank    = regexp.MustCompile(`\n{3,}`)
)

func plainDescription(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = mdTrailing.ReplaceAllString(s, "\n")
	s = mdRefDef.ReplaceAllString(s, "")
	s = mdRule.ReplaceAllString(s, "")
	s = mdLink.ReplaceAllString(s, "$1")
	s = mdRefLink.ReplaceAllString(s, "$1")
	s = strings.NewReplacer("**", "", "__", "").Replace(s)
	s = mdItalic.ReplaceAllString(s, "$1")
	s = mdBlank.ReplaceAllString(s, "\n\n")
	return strings.TrimSpace(s)
}

func descriptionText(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var obj struct {
		Value string `json:"value"`
	}
	if json.Unmarshal(raw, &obj) == nil {
		return obj.Value
	}
	return ""
}

// Cover fetches one Open Library cover by its id: size "S" for a search
// thumbnail, "M" for a book's stored cover. The content type is sniffed
// from the bytes (webfetch.GetImage), never taken from the server.
// default=false makes a missing cover a 404 instead of a blank placeholder.
func (o *OpenLibrary) Cover(ctx context.Context, coverID int64, size string) (string, []byte, error) {
	if coverID <= 0 || (size != "S" && size != "M") {
		return "", nil, ErrInvalid
	}
	ctx, cancel := context.WithTimeout(ctx, coverTimeout)
	defer cancel()
	return o.Web.GetImage(ctx, fmt.Sprintf("%s/b/id/%d-%s.jpg?default=false", o.Covers, coverID, size), MaxCoverBytes)
}
