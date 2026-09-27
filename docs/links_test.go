package docs

import (
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

// headingRe matches ATX headings.
var headingRe = regexp.MustCompile(`(?m)^#{1,6}\s+(.+?)\s*#*\s*$`)

// fenceRe strips fenced code blocks so links inside examples aren't checked.
var fenceRe = regexp.MustCompile("(?ms)^```.*?^```")

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
		text := regexp.MustCompile(`\[([^\]]*)\]\([^)]*\)`).ReplaceAllString(m[1], "$1")
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
		for _, target := range targets {
			checkTarget(t, doc, target)
		}
	}
}

func checkTarget(t *testing.T, doc, target string) {
	t.Helper()
	if strings.Contains(target, "://") || strings.HasPrefix(target, "mailto:") {
		return
	}
	if strings.HasPrefix(target, "/") {
		t.Errorf("%s: absolute path link %q; use a relative link", doc, target)
		return
	}
	path, frag, _ := strings.Cut(target, "#")
	inUser := strings.HasPrefix(filepath.ToSlash(doc), "user/")
	if inUser && strings.HasPrefix(path, "../") {
		t.Errorf("%s: %q leaves docs/user; in-app help can't follow it — use an https:// GitHub URL", doc, target)
		return
	}
	resolved := doc
	if path != "" {
		resolved = filepath.Join(filepath.Dir(doc), path)
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

func TestUserDocsHaveNoRawHTML(t *testing.T) {
	// goldmark drops raw HTML by default, so it would silently vanish in
	// /help. Guides stay plain Markdown.
	tag := regexp.MustCompile(`<[a-zA-Z/][^>]*>`)
	for _, doc := range documents(t) {
		if !strings.HasPrefix(filepath.ToSlash(doc), "user/") {
			continue
		}
		body, _ := os.ReadFile(doc)
		text := fenceRe.ReplaceAllString(string(body), "")
		text = regexp.MustCompile("`[^`]*`").ReplaceAllString(text, "")
		if m := tag.FindString(text); m != "" {
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
