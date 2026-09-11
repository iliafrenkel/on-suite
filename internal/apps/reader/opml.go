package reader

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"strings"
)

// MaxOPMLBytes bounds an uploaded subscription list (POST
// /reader/import). web.DefaultMaxBodyBytes already caps every request body
// at 1 MiB before this app's handler ever runs
// (internal/platform/web/middleware.go); a multipart file part carries
// almost no encoding overhead of its own, so 768 KiB of file content leaves
// comfortable headroom under that ceiling for the multipart boundaries and
// the request's other fields — matching ON Notes' MaxImportFileBytes. A
// thousand feeds of OPML is well under this; anything larger is a mistake
// or an attack, and either way is better refused than parsed.
const MaxOPMLBytes = 768 << 10

// ErrNotOPML means the uploaded bytes were not an OPML document.
var ErrNotOPML = errors.New("reader: not an OPML document")

// OPMLEntry is one subscription from an OPML file, flattened to this app's one
// level of folders.
type OPMLEntry struct {
	FeedURL string
	Title   string
	SiteURL string
	// Folder is the top-level folder name, empty for a root subscription.
	Folder string
}

// opmlDoc mirrors the parts of OPML this app uses. Everything else in the
// format — head metadata, window geometry, expansion state — is ignored on
// purpose: it describes another reader's window, not these subscriptions.
type opmlDoc struct {
	XMLName xml.Name `xml:"opml"`
	// Version is written on export and ignored on import: plenty of files in
	// the wild omit it, and refusing them would help nobody.
	Version string        `xml:"version,attr"`
	Head    opmlHead      `xml:"head"`
	Body    opmlBodyOuter `xml:"body"`
}

type opmlHead struct {
	Title string `xml:"title"`
}

type opmlBodyOuter struct {
	Outlines []opmlOutline `xml:"outline"`
}

type opmlOutline struct {
	Text     string        `xml:"text,attr"`
	Title    string        `xml:"title,attr"`
	Type     string        `xml:"type,attr"`
	XMLURL   string        `xml:"xmlUrl,attr"`
	HTMLURL  string        `xml:"htmlUrl,attr"`
	Children []opmlOutline `xml:"outline"`
}

// name is the outline's display name. OPML 2.0 says text; OPML 1.0 said title;
// exporters in the wild emit either, so both are read and neither is required.
func (o opmlOutline) name() string {
	if t := strings.TrimSpace(o.Text); t != "" {
		return t
	}
	return strings.TrimSpace(o.Title)
}

// ParseOPML reads a subscription list.
//
// It is deliberately forgiving about everything except the one thing that
// matters: an outline with an xmlUrl is a subscription, and an outline without
// one is a folder or a bookmark. Real exports are full of attributes this app
// has no use for, and refusing them would mean refusing most real files.
func ParseOPML(data []byte) ([]OPMLEntry, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		return nil, fmt.Errorf("%w: empty document", ErrNotOPML)
	}

	var doc opmlDoc
	if err := xml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrNotOPML, err)
	}
	// Unmarshal into a struct with an XMLName tag fails on a different root
	// element, so reaching here means the root really was <opml>.

	var out []OPMLEntry
	for _, top := range doc.Body.Outlines {
		if url := strings.TrimSpace(top.XMLURL); url != "" {
			// A subscription at the root of the document.
			out = append(out, entryFrom(top, ""))
			continue
		}
		// A folder. Everything below it flattens into it, however deeply the
		// exporting reader nested things: losing a feed because this app only
		// nests one level would be much worse than losing its sub-folder.
		folder := top.name()
		out = append(out, collectOutlines(top.Children, folder)...)
	}
	return out, nil
}

func collectOutlines(outlines []opmlOutline, folder string) []OPMLEntry {
	var out []OPMLEntry
	for _, o := range outlines {
		if url := strings.TrimSpace(o.XMLURL); url != "" {
			out = append(out, entryFrom(o, folder))
		}
		out = append(out, collectOutlines(o.Children, folder)...)
	}
	return out
}

func entryFrom(o opmlOutline, folder string) OPMLEntry {
	return OPMLEntry{
		FeedURL: strings.TrimSpace(o.XMLURL),
		Title:   o.name(),
		SiteURL: strings.TrimSpace(o.HTMLURL),
		Folder:  folder,
	}
}

// BuildOPML renders one user's subscriptions as OPML 2.0.
//
// encoding/xml does the escaping, which is the reason this builds a struct and
// marshals it rather than writing the document with fmt: a feed title
// containing an ampersand or a quote is ordinary, and hand-written XML gets
// that wrong eventually.
func BuildOPML(t Tree, title string) ([]byte, error) {
	doc := opmlDoc{
		XMLName: xml.Name{Local: "opml"},
		Version: "2.0",
		Head:    opmlHead{Title: title},
	}

	for _, s := range t.Root {
		doc.Body.Outlines = append(doc.Body.Outlines, subscriptionOutline(s))
	}
	for _, f := range t.Folders {
		folder := opmlOutline{Text: f.Name}
		for _, s := range f.Subs {
			folder.Children = append(folder.Children, subscriptionOutline(s))
		}
		doc.Body.Outlines = append(doc.Body.Outlines, folder)
	}

	body, err := xml.MarshalIndent(doc, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("reader: build OPML: %w", err)
	}

	var buf bytes.Buffer
	buf.WriteString(xml.Header)
	buf.Write(body)
	buf.WriteByte('\n')
	return buf.Bytes(), nil
}

func subscriptionOutline(s Subscription) opmlOutline {
	return opmlOutline{
		Type:    "rss",
		Text:    s.DisplayName(),
		XMLURL:  s.FeedURL,
		HTMLURL: s.SiteURL,
	}
}
