package ui_test

import (
	"io/fs"
	"regexp"
	"strings"
	"testing"

	"github.com/iliafrenkel/on-suite/internal/ui"
)

// cssRule is one style rule from app.css: its comma-separated selectors,
// its declarations with whitespace collapsed, and the @media (or other
// at-rule) prelude it sits in, "" at the top level.
type cssRule struct {
	selectors []string
	body      string
	media     string
}

// cssSheet is a small, order-insensitive view of app.css for contract
// tests: look a selector up and check its declarations, wherever the rule
// sits in the file and whatever order its declarations are in. It is not a
// full CSS parser; app.css has no braces inside strings or nested rules,
// which is all it needs.
type cssSheet struct {
	t     *testing.T
	rules []cssRule
}

var (
	cssComment = regexp.MustCompile(`(?s)/\*.*?\*/`)
	cssSpace   = regexp.MustCompile(`\s+`)
)

func loadAppCSS(t *testing.T) *cssSheet {
	t.Helper()
	src, err := fs.ReadFile(ui.Static(), "app.css")
	if err != nil {
		t.Fatal(err)
	}
	return parseCSS(t, string(src))
}

func parseCSS(t *testing.T, css string) *cssSheet {
	t.Helper()
	css = cssComment.ReplaceAllString(css, "")
	s := &cssSheet{t: t}
	var atStack []string
	start := 0
	for i := 0; i < len(css); i++ {
		switch css[i] {
		case '{':
			prelude := strings.TrimSpace(css[start:i])
			start = i + 1
			if strings.HasPrefix(prelude, "@") && !strings.HasPrefix(prelude, "@font-face") {
				atStack = append(atStack, norm(prelude))
				continue
			}
			end := strings.IndexByte(css[i+1:], '}')
			if end < 0 {
				t.Fatalf("app.css: unclosed rule %q", prelude)
			}
			media := ""
			if len(atStack) > 0 {
				media = atStack[len(atStack)-1]
			}
			var sels []string
			for _, sel := range strings.Split(prelude, ",") {
				sels = append(sels, norm(sel))
			}
			s.rules = append(s.rules, cssRule{selectors: sels, body: norm(css[i+1 : i+1+end]), media: media})
			i += 1 + end
			start = i + 1
		case '}':
			// A rule's own } is skipped above, so this closes an at-rule.
			if len(atStack) > 0 {
				atStack = atStack[:len(atStack)-1]
			}
			start = i + 1
		}
	}
	if len(s.rules) == 0 {
		t.Fatal("app.css: no rules parsed")
	}
	return s
}

func norm(s string) string { return strings.TrimSpace(cssSpace.ReplaceAllString(s, " ")) }

// rulesFor returns every rule, in any at-rule, that lists selector as one
// of its comma-separated selectors.
func (s *cssSheet) rulesFor(selector string) []cssRule {
	selector = norm(selector)
	var out []cssRule
	for _, r := range s.rules {
		for _, sel := range r.selectors {
			if sel == selector {
				out = append(out, r)
				break
			}
		}
	}
	return out
}

// declsFor joins the declarations of every top-level rule for selector, so
// a token can be looked up across several :root blocks.
func (s *cssSheet) declsFor(selector string) string {
	var parts []string
	for _, r := range s.rulesFor(selector) {
		if r.media == "" {
			parts = append(parts, r.body)
		}
	}
	return strings.Join(parts, "; ")
}

// mustDeclare fails unless some rule for selector inside media ("" for top
// level) contains every one of decls, e.g. "border-left: 2px solid var(--c-accent)".
func (s *cssSheet) mustDeclare(media, selector string, decls ...string) {
	s.t.Helper()
	rules := s.rulesFor(selector)
	if len(rules) == 0 {
		s.t.Errorf("app.css: no rule for %q", selector)
		return
	}
	for _, d := range decls {
		d = norm(d)
		found := false
		for _, r := range rules {
			if r.media == norm(media) && strings.Contains(r.body, d) {
				found = true
				break
			}
		}
		if !found {
			where := "at the top level"
			if media != "" {
				where = "in " + media
			}
			s.t.Errorf("app.css: %s does not declare %q %s", selector, d, where)
		}
	}
}

func TestCSSSheetFindsRulesRegardlessOfOrder(t *testing.T) {
	s := parseCSS(t, `
/* a comment { with braces } */
.a, .b:hover { color: red;
	background: blue }
@media (max-width: 780px) {
	.a { order: -1; }
}
@font-face { font-family: X; src: url("/x.woff2"); }
:root { --x: #ffffff; }
:root { --y: #000000; }
`)
	s.mustDeclare("", ".b:hover", "background: blue", "color: red")
	s.mustDeclare("@media (max-width: 780px)", ".a", "order: -1")
	if got := s.declsFor(":root"); !strings.Contains(got, "--x: #ffffff") || !strings.Contains(got, "--y: #000000") {
		t.Errorf("declsFor(:root) = %q, want both blocks", got)
	}
	if n := len(s.rulesFor(".c")); n != 0 {
		t.Errorf("rulesFor(.c) = %d rules, want 0", n)
	}
}
