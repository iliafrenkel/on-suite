// Package help serves the user guides (docs/user) at /help, rendered from
// Markdown once at startup (#309, spec
// docs/superpowers/specs/2026-09-27-documentation-design.md).
//
// The same Markdown reads correctly on GitHub: relative page links and
// images are rewritten here to their /help URLs. It is the only importer of
// goldmark (see TestGoldmarkIsContained in internal/arch).
package help

import (
	"bytes"
	"errors"
	"fmt"
	"html/template"
	"io/fs"
	"net/url"
	"path"
	"strconv"
	"strings"
	"unicode"

	"github.com/microcosm-cc/bluemonday"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
)

// order is the sidebar, and the complete list of pages: Load fails on a
// page missing from the FS or an .md file missing from here.
var order = []struct{ Slug, Label string }{
	{"index", "Welcome"},
	{"paste", "ON Paste"},
	{"notes", "ON Notes"},
	{"reader", "ON Reader"},
	{"later", "ON Later"},
	{"books", "ON Books"},
	{"flash", "ON Flash"},
	{"focus", "ON Focus"},
	{"admin", "Administration"},
}

// Page is one rendered user guide.
type Page struct {
	Slug, Label string
	// Title is the guide's first H1, used only to check every guide starts
	// with one (TestTheRealGuidesLoad); the handler titles pages from Label,
	// not this field.
	Title string
	HTML  template.HTML
}

// Pages holds every rendered guide, cached at startup.
type Pages struct {
	list   []Page
	bySlug map[string]Page
	images fs.FS
}

// Load renders every guide in fsys once, sanitises the HTML and caches it.
// fsys must contain exactly the pages listed in order, one .md file each,
// and an images/ directory.
func Load(fsys fs.FS) (*Pages, error) {
	listed := map[string]bool{}
	for _, e := range order {
		listed[e.Slug+".md"] = true
	}
	mds, err := fs.Glob(fsys, "*.md")
	if err != nil {
		return nil, err
	}
	for _, m := range mds {
		if !listed[m] {
			return nil, fmt.Errorf("help: %s is not in the page order", m)
		}
	}

	md := goldmark.New(
		goldmark.WithExtensions(
			extension.NewTable(
				extension.WithTableCellAlignMethod(extension.TableCellAlignAttribute),
			),
			extension.Strikethrough,
			extension.Linkify,
		),
		goldmark.WithParserOptions(
			parser.WithAutoHeadingID(),
			parser.WithASTTransformers(util.Prioritized(linkRewriter{}, 100)),
		),
	)
	policy := sanitizer()

	p := &Pages{bySlug: map[string]Page{}}
	for _, e := range order {
		src, err := fs.ReadFile(fsys, e.Slug+".md")
		if err != nil {
			return nil, fmt.Errorf("help: %w", err)
		}
		var buf bytes.Buffer
		ctx := parser.NewContext(parser.WithIDs(newGithubIDs()))
		if err := md.Convert(src, &buf, parser.WithContext(ctx)); err != nil {
			return nil, fmt.Errorf("help: render %s: %w", e.Slug, err)
		}
		if bytes.Contains(buf.Bytes(), []byte("raw HTML omitted")) {
			return nil, fmt.Errorf("help: %s contains raw HTML; guides must be plain Markdown", e.Slug)
		}
		pg := Page{
			Slug: e.Slug, Label: e.Label, Title: firstHeading(src),
			HTML: template.HTML(policy.SanitizeBytes(buf.Bytes())), // #nosec G203 -- sanitised
		}
		p.list = append(p.list, pg)
		p.bySlug[e.Slug] = pg
	}
	if fi, err := fs.Stat(fsys, "images"); err != nil {
		return nil, fmt.Errorf("help: images directory missing: %w", err)
	} else if !fi.IsDir() {
		return nil, errors.New("help: images is not a directory")
	}
	if p.images, err = fs.Sub(fsys, "images"); err != nil {
		return nil, err
	}
	return p, nil
}

// Get returns the page for slug. "" and "index" both return the index page.
func (p *Pages) Get(slug string) (Page, bool) {
	if slug == "" {
		slug = "index"
	}
	pg, ok := p.bySlug[slug]
	return pg, ok
}

// List returns every page in sidebar order.
func (p *Pages) List() []Page { return p.list }

// Images returns the guides' images, rooted at images/.
func (p *Pages) Images() fs.FS { return p.images }

func sanitizer() *bluemonday.Policy {
	pol := bluemonday.UGCPolicy()
	pol.AllowAttrs("id").OnElements("h1", "h2", "h3", "h4", "h5", "h6")
	pol.AllowAttrs("align").Matching(bluemonday.SpaceSeparatedTokens).OnElements("th", "td")
	return pol
}

// firstHeading returns the guide's first top-level (# ) heading, skipping
// any that appear inside a fenced (```) code block — a guide showing, say,
// "# not a title" as an example must not be mistaken for its real title.
func firstHeading(src []byte) string {
	inFence := false
	for _, line := range strings.Split(string(src), "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "```") {
			inFence = !inFence
			continue
		}
		if inFence {
			continue
		}
		if strings.HasPrefix(line, "# ") {
			return strings.TrimSpace(strings.TrimPrefix(line, "# "))
		}
	}
	return ""
}

// linkRewriter rewrites every link and image destination from a
// GitHub-relative reference to its /help URL.
type linkRewriter struct{}

func (linkRewriter) Transform(doc *ast.Document, _ text.Reader, _ parser.Context) {
	_ = ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		switch l := n.(type) {
		case *ast.Link:
			l.Destination = []byte(rewriteLink(string(l.Destination)))
		case *ast.Image:
			l.Destination = []byte(rewriteLink(string(l.Destination)))
		}
		return ast.WalkContinue, nil
	})
}

// rewriteLink maps a GitHub-relative link to its /help URL. Anything that
// isn't a relative .md page or images/ file is returned unchanged.
func rewriteLink(dest string) string {
	if dest == "" || strings.HasPrefix(dest, "#") || strings.HasPrefix(dest, "/") {
		return dest
	}
	u, err := url.Parse(dest)
	if err != nil || u.Scheme != "" || u.Host != "" {
		return dest
	}
	clean := path.Clean(u.Path)
	if clean == "." || strings.HasPrefix(clean, "..") {
		return dest
	}
	var out string
	switch {
	case strings.HasPrefix(clean, "images/"):
		out = "/help/" + clean
	case strings.HasSuffix(clean, ".md") && !strings.Contains(clean, "/"):
		name := strings.TrimSuffix(clean, ".md")
		out = "/help/" + name
		if name == "index" {
			out = "/help"
		}
	default:
		return dest
	}
	if u.Fragment != "" {
		out += "#" + u.Fragment
	}
	return out
}

// githubIDs implements goldmark's parser.IDs using the same heading-anchor
// algorithm as githubSlug in docs/links_test.go, so headings get the exact
// anchors GitHub uses: lowercase; keep letters, digits, -, _; space -> -;
// duplicates get a -1, -2, ... suffix. Keeping this in sync with
// githubSlug is covered by TestHeadingsGetTheSameAnchorsGitHubUses here and
// TestGithubSlug in docs/links_test.go, run against the same cases.
type githubIDs struct {
	seen map[string]int
}

func newGithubIDs() *githubIDs {
	return &githubIDs{seen: map[string]int{}}
}

func (g *githubIDs) Generate(value []byte, kind ast.NodeKind) []byte {
	return g.dedupe(githubSlug(string(value)))
}

func (g *githubIDs) Put(value []byte) {
	g.seen[string(value)]++
}

func (g *githubIDs) dedupe(s string) []byte {
	n := g.seen[s]
	g.seen[s]++
	if n == 0 {
		return []byte(s)
	}
	return []byte(s + "-" + strconv.Itoa(n))
}

// githubSlug reproduces GitHub's heading anchor algorithm closely enough
// for our headings: lowercase, drop everything but letters, digits,
// spaces, hyphens and underscores, spaces to hyphens. Kept identical to
// docs/links_test.go's githubSlug.
func githubSlug(heading string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(heading) {
		switch {
		case unicode.IsLetter(r), unicode.IsDigit(r), r == '-', r == '_':
			b.WriteRune(r)
		case r == ' ':
			b.WriteRune('-')
		}
	}
	return b.String()
}
