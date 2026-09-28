package docs

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"unicode"
)

// linkRe matches Markdown links and images: [text](target) / ![alt](target).
// An optional quoted title after the target is ignored.
var linkRe = regexp.MustCompile(`!?\[[^\]]*\]\(\s*<?([^)\s>]+)>?(?:\s+"[^"]*")?\s*\)`)

// htmlRefRe matches src="..." and href="..." / srcset="..." in raw HTML,
// which README.md uses for <picture> and <img>.
var htmlRefRe = regexp.MustCompile(`(?:src|href|srcset)="([^"]+)"`)

// refDefRe matches Markdown reference-style link definitions: [ref]: target
var refDefRe = regexp.MustCompile(`(?m)^ {0,3}\[[^\]]+\]:\s*<?([^\s>]+)>?`)

// headingRe matches ATX headings.
var headingRe = regexp.MustCompile(`(?m)^#{1,6}\s+(.+?)\s*#*\s*$`)

// fenceRe strips fenced code blocks so links inside examples aren't checked.
var fenceRe = regexp.MustCompile("(?ms)^```.*?^```")

// headingLinkRe matches a link inside a heading, so it can be slugged by its
// text alone.
var headingLinkRe = regexp.MustCompile(`\[([^\]]*)\]\([^)]*\)`)

// htmlTagRe matches a raw HTML tag; inlineCodeRe an inline code span, which
// may show a tag without being one.
var (
	htmlTagRe    = regexp.MustCompile(`<[a-zA-Z/][^>]*>`)
	inlineCodeRe = regexp.MustCompile("`[^`]*`")
)

// repoBlobPrefix is the GitHub address of a file on main. The user guides
// link to the rest of the repository this way, because in-app help can only
// follow links inside docs/user; the test still checks those files exist.
const repoBlobPrefix = "https://github.com/iliafrenkel/on-suite/blob/main/"

// githubSlug reproduces GitHub's heading anchor algorithm closely enough
// for our headings: lowercase, drop everything but letters, digits,
// spaces, hyphens and underscores, spaces to hyphens.
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

func anchors(t *testing.T, path string) map[string]bool {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]bool{}
	seen := map[string]int{}
	for _, m := range headingRe.FindAllStringSubmatch(fenceRe.ReplaceAllString(string(body), ""), -1) {
		// Strip inline code/emphasis markers and link syntax before slugging.
		text := headingLinkRe.ReplaceAllString(m[1], "$1")
		text = strings.NewReplacer("`", "", "*", "").Replace(text)
		s := githubSlug(text)
		if n := seen[s]; n > 0 {
			out[s+"-"+strconv.Itoa(n)] = true
		} else {
			out[s] = true
		}
		seen[s]++
	}
	return out
}

// documents lists every Markdown file whose links we promise are good.
func documents(t *testing.T) []string {
	t.Helper()
	files := []string{"../README.md", "../CONTRIBUTING.md"}
	for _, dir := range []string{"user", "developers", "self-hosting", "screenshots"} {
		err := filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			// The seed's fixtures are demo content, not documentation.
			if d.IsDir() && filepath.ToSlash(p) == "screenshots/seed/fixtures" {
				return filepath.SkipDir
			}
			if !d.IsDir() && strings.HasSuffix(p, ".md") {
				files = append(files, p)
			}
			return nil
		})
		if err != nil && !os.IsNotExist(err) {
			t.Fatal(err)
		}
	}
	return files
}

func TestEveryRelativeLinkResolves(t *testing.T) {
	for _, doc := range documents(t) {
		body, err := os.ReadFile(doc)
		if err != nil {
			t.Fatal(err)
		}
		text := fenceRe.ReplaceAllString(string(body), "")
		var targets []string
		for _, m := range linkRe.FindAllStringSubmatch(text, -1) {
			targets = append(targets, m[1])
		}
		for _, m := range htmlRefRe.FindAllStringSubmatch(text, -1) {
			// srcset may hold "a.png 1x, b.png 2x"; take each URL.
			for _, part := range strings.Split(m[1], ",") {
				targets = append(targets, strings.Fields(part)[0])
			}
		}
		for _, m := range refDefRe.FindAllStringSubmatch(text, -1) {
			targets = append(targets, m[1])
		}
		for _, target := range targets {
			checkTarget(t, doc, target)
		}
	}
}

func checkTarget(t *testing.T, doc, target string) {
	t.Helper()
	resolved, frag, check, err := resolveTarget(doc, target)
	if err != nil {
		t.Errorf("%s: %v", doc, err)
		return
	}
	if !check {
		return
	}
	if resolved != doc {
		if _, err := os.Stat(resolved); err != nil {
			t.Errorf("%s: link %q: %v", doc, target, err)
			return
		}
	}
	if frag != "" && strings.HasSuffix(resolved, ".md") {
		if !anchors(t, resolved)[frag] {
			t.Errorf("%s: link %q: no heading with anchor #%s in %s", doc, target, frag, resolved)
		}
	}
}

// resolveTarget works out which file a link in doc (a path relative to
// docs/) points at, and the #fragment, without touching the disk. check is
// false for links the test doesn't follow (other websites, mailto:). err
// reports a link that breaks the docs' rules whether or not it exists.
func resolveTarget(doc, target string) (resolved, frag string, check bool, err error) {
	if rest, ok := strings.CutPrefix(target, repoBlobPrefix); ok {
		path, frag, _ := strings.Cut(rest, "#")
		if path == "" {
			return "", "", false, fmt.Errorf("%q names no file in the repository", target)
		}
		return filepath.Join("..", path), frag, true, nil
	}
	if strings.Contains(target, "://") || strings.HasPrefix(target, "mailto:") {
		return "", "", false, nil
	}
	if strings.HasPrefix(target, "/") {
		return "", "", false, fmt.Errorf("absolute path link %q; use a relative link", target)
	}
	path, frag, _ := strings.Cut(target, "#")
	resolved = doc
	if path != "" {
		resolved = filepath.Join(filepath.Dir(doc), path)
	}
	if inUserDocs(doc) && (strings.HasPrefix(path, "../") || !inUserDocs(resolved)) {
		return "", "", false, fmt.Errorf("%q leaves docs/user; in-app help can't follow it — use an https:// GitHub URL", target)
	}
	return resolved, frag, true, nil
}

// inUserDocs reports whether a cleaned path relative to docs/ is inside
// docs/user.
func inUserDocs(path string) bool {
	p := filepath.ToSlash(path)
	return p == "user" || strings.HasPrefix(p, "user/")
}

func TestResolveTarget(t *testing.T) {
	for _, tc := range []struct {
		doc, target  string
		wantResolved string
		wantFrag     string
		wantCheck    bool
		wantErr      bool
	}{
		{doc: "developers/index.md", target: "testing.md#the-architecture-test", wantResolved: "developers/testing.md", wantFrag: "the-architecture-test", wantCheck: true},
		{doc: "developers/index.md", target: "#next", wantResolved: "developers/index.md", wantFrag: "next", wantCheck: true},
		{doc: "developers/index.md", target: "../self-hosting/deploying.md", wantResolved: "self-hosting/deploying.md", wantCheck: true},
		{doc: "developers/index.md", target: "https://example.com/x"},
		{doc: "developers/index.md", target: "mailto:someone@example.com"},
		{doc: "developers/index.md", target: "/docs/user/index.md", wantErr: true},
		// GitHub links to this repository's main branch are checked like
		// relative ones, from any document.
		{doc: "user/admin.md", target: repoBlobPrefix + "docs/self-hosting/deploying.md#backups", wantResolved: "../docs/self-hosting/deploying.md", wantFrag: "backups", wantCheck: true},
		{doc: "../README.md", target: repoBlobPrefix + "LICENSE", wantResolved: "../LICENSE", wantCheck: true},
		{doc: "user/admin.md", target: repoBlobPrefix, wantErr: true},
		// Other branches and other GitHub pages aren't.
		{doc: "user/admin.md", target: "https://github.com/iliafrenkel/on-suite/blob/dev/README.md"},
		{doc: "user/admin.md", target: "https://github.com/iliafrenkel/on-suite/releases"},
		// The user guides may only link within docs/user.
		{doc: "user/notes.md", target: "images/notes-outline.png", wantResolved: "user/images/notes-outline.png", wantCheck: true},
		{doc: "user/notes.md", target: "index.md#signing-in", wantResolved: "user/index.md", wantFrag: "signing-in", wantCheck: true},
		{doc: "user/notes.md", target: "../self-hosting/deploying.md", wantErr: true},
		{doc: "user/notes.md", target: "images/../../self-hosting/deploying.md", wantErr: true},
		{doc: "user/notes.md", target: "images/../../../README.md", wantErr: true},
	} {
		resolved, frag, check, err := resolveTarget(tc.doc, tc.target)
		if (err != nil) != tc.wantErr {
			t.Errorf("resolveTarget(%q, %q) error = %v, want error %v", tc.doc, tc.target, err, tc.wantErr)
			continue
		}
		if tc.wantErr {
			continue
		}
		if filepath.ToSlash(resolved) != tc.wantResolved || frag != tc.wantFrag || check != tc.wantCheck {
			t.Errorf("resolveTarget(%q, %q) = %q, %q, %v; want %q, %q, %v",
				tc.doc, tc.target, resolved, frag, check, tc.wantResolved, tc.wantFrag, tc.wantCheck)
		}
	}
}

func TestUserDocsHaveNoRawHTML(t *testing.T) {
	// goldmark drops raw HTML by default, so it would silently vanish in
	// /help. Guides stay plain Markdown.
	for _, doc := range documents(t) {
		if !inUserDocs(doc) {
			continue
		}
		body, err := os.ReadFile(doc)
		if err != nil {
			t.Fatal(err)
		}
		text := fenceRe.ReplaceAllString(string(body), "")
		text = inlineCodeRe.ReplaceAllString(text, "")
		if m := htmlTagRe.FindString(text); m != "" {
			t.Errorf("%s contains raw HTML %q", doc, m)
		}
	}
}

func TestGithubSlug(t *testing.T) {
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
