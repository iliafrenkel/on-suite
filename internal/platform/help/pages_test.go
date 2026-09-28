package help

import (
	"regexp"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/iliafrenkel/on-suite/docs"
)

// hrefFragRe matches an href with a fragment: href="/help/slug#frag" or
// href="#frag" (target group empty for the latter).
var hrefFragRe = regexp.MustCompile(`href="(/help(?:/[a-z0-9-]+)?)?#([a-zA-Z0-9_-]+)"`)

// idRe matches an id="..." attribute on a rendered heading.
var idRe = regexp.MustCompile(`id="([a-zA-Z0-9_-]+)"`)

func extractIDs(html string) map[string]bool {
	out := map[string]bool{}
	for _, m := range idRe.FindAllStringSubmatch(html, -1) {
		out[m[1]] = true
	}
	return out
}

func TestRewriteLink(t *testing.T) {
	for in, want := range map[string]string{
		"notes.md":                    "/help/notes",
		"notes.md#keyboard-shortcuts": "/help/notes#keyboard-shortcuts",
		"index.md":                    "/help",
		"index.md#signing-in":         "/help#signing-in",
		"images/notes-outline.png":    "/help/images/notes-outline.png",
		"#quick-tour":                 "#quick-tour",
		"https://example.com/a.md":    "https://example.com/a.md",
		"/already/absolute":           "/already/absolute",
		"mailto:x@example.com":        "mailto:x@example.com",
		"":                            "",
		"./notes.md":                  "/help/notes",
		"./images/a.png":              "/help/images/a.png",
		"images/../../x.png":          "images/../../x.png",
		"sub/notes.md":                "sub/notes.md",
	} {
		if got := rewriteLink(in); got != want {
			t.Errorf("rewriteLink(%q) = %q, want %q", in, got, want)
		}
	}
}

func minimalFS() fstest.MapFS {
	fsys := fstest.MapFS{"images/a.png": {Data: []byte("\x89PNG")}}
	for _, e := range order {
		fsys[e.Slug+".md"] = &fstest.MapFile{Data: []byte("# " + e.Label + "\n\nSee [notes](notes.md) and ![a](images/a.png).\n")}
	}
	return fsys
}

func TestLoadRendersEveryPageInOrder(t *testing.T) {
	p, err := Load(minimalFS())
	if err != nil {
		t.Fatal(err)
	}
	list := p.List()
	if len(list) != len(order) {
		t.Fatalf("List() has %d pages, want %d", len(list), len(order))
	}
	for i, pg := range list {
		if pg.Slug != order[i].Slug || pg.Label != order[i].Label {
			t.Errorf("page %d = %s/%s, want %s/%s", i, pg.Slug, pg.Label, order[i].Slug, order[i].Label)
		}
		html := string(pg.HTML)
		if !strings.Contains(html, `href="/help/notes"`) || !strings.Contains(html, `src="/help/images/a.png"`) {
			t.Errorf("%s: links not rewritten: %s", pg.Slug, html)
		}
	}
	if idx, ok := p.Get(""); !ok || idx.Slug != "index" {
		t.Errorf(`Get("") = %v, %v; want index`, idx.Slug, ok)
	}
	if _, ok := p.Get("nope"); ok {
		t.Error(`Get("nope") found a page`)
	}
}

func TestLoadFailsOnAMissingOrUnlistedPage(t *testing.T) {
	missing := minimalFS()
	delete(missing, "flash.md")
	if _, err := Load(missing); err == nil {
		t.Error("Load succeeded with flash.md missing")
	}
	extra := minimalFS()
	extra["secret.md"] = &fstest.MapFile{Data: []byte("# Secret\n")}
	if _, err := Load(extra); err == nil {
		t.Error("Load succeeded with an unlisted page; add it to order or delete it")
	}
}

func TestSanitisingStripsScriptsAndStyles(t *testing.T) {
	fsys := minimalFS()
	fsys["index.md"] = &fstest.MapFile{Data: []byte("# Welcome\n\n[x](javascript:alert(1))\n\n| a | b |\n|:--|--:|\n| 1 | 2 |\n")}
	p, err := Load(fsys)
	if err != nil {
		t.Fatal(err)
	}
	html := string(p.List()[0].HTML)
	for _, bad := range []string{"javascript:", "style=", "<script"} {
		if strings.Contains(html, bad) {
			t.Errorf("rendered HTML contains %q: %s", bad, html)
		}
	}
	if !strings.Contains(html, "<table>") || !strings.Contains(html, `align="right"`) {
		t.Errorf("GFM table alignment lost: %s", html)
	}
}

// TestGithubSlugMatchesDocsLinksTest runs the same cases as
// docs/links_test.go's TestGithubSlug against this package's githubSlug, so
// the two copies of GitHub's heading-anchor algorithm are kept in sync.
func TestGithubSlugMatchesDocsLinksTest(t *testing.T) {
	for in, want := range map[string]string{
		"Sharing a note publicly": "sharing-a-note-publicly",
		"Keyboard shortcuts":      "keyboard-shortcuts",
		"Import & export (OPML)":  "import--export-opml",
		"What's `#tag` syntax?":   "whats-tag-syntax",
	} {
		if got := githubSlug(strings.ReplaceAll(in, "`", "")); got != want {
			t.Errorf("githubSlug(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestHeadingsGetTheSameAnchorsGitHubUses(t *testing.T) {
	fsys := minimalFS()
	fsys["index.md"] = &fstest.MapFile{Data: []byte("# Welcome\n\n## Sharing a note publicly\n\n## Import & export (OPML)\n")}
	p, err := Load(fsys)
	if err != nil {
		t.Fatal(err)
	}
	html := string(p.List()[0].HTML)
	for _, id := range []string{`id="sharing-a-note-publicly"`, `id="import--export-opml"`} {
		if !strings.Contains(html, id) {
			t.Errorf("missing %s in %s", id, html)
		}
	}
}

func TestTheRealGuidesLoad(t *testing.T) {
	p, err := Load(docs.User())
	if err != nil {
		t.Fatalf("Load(docs.User()): %v", err)
	}
	for _, pg := range p.List() {
		if pg.Title == "" || len(pg.HTML) == 0 {
			t.Errorf("%s: empty title or body", pg.Slug)
		}
	}
}

func TestLoadRejectsRawHTML(t *testing.T) {
	fsys := minimalFS()
	fsys["index.md"] = &fstest.MapFile{Data: []byte("# Welcome\n\nSome <b>x</b> text.\n")}
	if _, err := Load(fsys); err == nil {
		t.Error("Load succeeded with raw HTML in a guide; guides must be plain Markdown")
	}
}

func TestLoadFailsOnMissingImagesDir(t *testing.T) {
	fsys := minimalFS()
	delete(fsys, "images/a.png")
	if _, err := Load(fsys); err == nil {
		t.Error("Load succeeded without an images/ directory")
	}
}

// TestCrossPageAnchorsResolveInApp proves that every /help/<page>#<frag> or
// in-page #<frag> link rendered from the real guides points at an id that
// actually exists in the rendered HTML of its target page. This is the
// in-app equivalent of docs/links_test.go's GitHub-side anchor check.
func TestCrossPageAnchorsResolveInApp(t *testing.T) {
	p, err := Load(docs.User())
	if err != nil {
		t.Fatalf("Load(docs.User()): %v", err)
	}
	ids := map[string]map[string]bool{}
	for _, pg := range p.List() {
		ids[pg.Slug] = extractIDs(string(pg.HTML))
	}
	linkRe := hrefFragRe
	for _, pg := range p.List() {
		html := string(pg.HTML)
		for _, m := range linkRe.FindAllStringSubmatch(html, -1) {
			target, frag := m[1], m[2]
			slug := pg.Slug
			if target != "" {
				slug = strings.TrimPrefix(target, "/help/")
				if target == "/help" {
					slug = "index"
				}
			}
			if !ids[slug][frag] {
				t.Errorf("%s: link to %q#%s: no such id in page %q (have %v)", pg.Slug, target, frag, slug, ids[slug])
			}
		}
	}
}
