package paste

import (
	"bytes"
	"fmt"
	"html/template"
	"sync"

	chromahtml "github.com/alecthomas/chroma/v2/formatters/html"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/alecthomas/chroma/v2/styles"
)

// Language is one entry in the language picker.
type Language struct {
	// Value is a Chroma lexer name or alias, or "" meaning detect.
	Value string
	Label string
}

// languageChoices is a curated shortlist. Chroma ships nearly 300 lexers,
// which is an unusable dropdown; these are the ones actually worth offering.
// A test asserts every Value here resolves to a real lexer, so a typo fails
// the build rather than silently falling back to plain text.
var languageChoices = []Language{
	{"", "Detect automatically"},
	{"plaintext", "Plain text"},
	{"bash", "Shell"},
	{"c", "C"},
	{"cpp", "C++"},
	{"css", "CSS"},
	{"diff", "Diff"},
	{"docker", "Dockerfile"},
	{"go", "Go"},
	{"html", "HTML"},
	{"ini", "INI / TOML"},
	{"java", "Java"},
	{"javascript", "JavaScript"},
	{"json", "JSON"},
	{"lua", "Lua"},
	{"markdown", "Markdown"},
	{"nginx", "nginx"},
	{"php", "PHP"},
	{"powershell", "PowerShell"},
	{"python", "Python"},
	{"ruby", "Ruby"},
	{"rust", "Rust"},
	{"sql", "SQL"},
	{"terraform", "Terraform"},
	{"typescript", "TypeScript"},
	{"xml", "XML"},
	{"yaml", "YAML"},
}

// Languages returns the picker contents.
func Languages() []Language {
	out := make([]Language, len(languageChoices))
	copy(out, languageChoices)
	return out
}

// IsLanguage reports whether v is an offered choice. The handler uses this so
// a hand-crafted form post cannot store an arbitrary string.
func IsLanguage(v string) bool {
	for _, l := range languageChoices {
		if l.Value == v {
			return true
		}
	}
	return false
}

// LanguageLabel is the display name for a stored value.
func LanguageLabel(v string) string {
	for _, l := range languageChoices {
		if l.Value == v {
			return l.Label
		}
	}
	return "Plain text"
}

const (
	// Chroma style names. Both are needed because the stylesheet carries a
	// light and a dark variant.
	styleLight = "github"
	styleDark  = "github-dark"
)

var (
	formatterOnce sync.Once
	formatter     *chromahtml.Formatter
)

// htmlFormatter is configured in class mode.
//
// This is not a preference. Chroma's default formatter writes inline style
// attributes, and the suite's Content-Security-Policy forbids inline styles, so
// the default produces a completely unstyled page. Classes plus a served
// stylesheet is the only configuration that works here.
func htmlFormatter() *chromahtml.Formatter {
	formatterOnce.Do(func() {
		formatter = chromahtml.New(
			chromahtml.WithClasses(true),
			chromahtml.WithLineNumbers(true),
			chromahtml.LineNumbersInTable(true),
		)
	})
	return formatter
}

// Highlight renders body as highlighted HTML.
//
// The result is template.HTML, meaning it is inserted without escaping. That is
// safe because Chroma escapes the source it tokenises — `<b>` in a snippet
// becomes `&lt;b&gt;`. TestHighlightEscapesHTML guards exactly that, and it is
// the reason this function is the only place in the suite that returns
// pre-trusted markup.
func Highlight(body, language string) template.HTML {
	lexer := lexers.Get(language)
	if lexer == nil && language == "" {
		lexer = lexers.Analyse(body)
	}
	if lexer == nil {
		lexer = lexers.Fallback
	}

	iterator, err := lexer.Tokenise(nil, body)
	if err != nil {
		// Tokenising cannot fail for well-formed UTF-8, which the store
		// guarantees. Fall back to escaped plain text rather than losing the
		// snippet entirely.
		return plainFallback(body)
	}

	var buf bytes.Buffer
	if err := htmlFormatter().Format(&buf, styles.Get(styleLight), iterator); err != nil {
		return plainFallback(body)
	}
	return template.HTML(buf.String())
}

// plainFallback renders the body as escaped plain text inside a <pre>.
func plainFallback(body string) template.HTML {
	var buf bytes.Buffer
	buf.WriteString(`<pre class="chroma">`)
	buf.WriteString(template.HTMLEscapeString(body))
	buf.WriteString("</pre>")
	return template.HTML(buf.String())
}

// highlightOverrides let the suite's own tokens own the surface, while Chroma
// owns only the token colours. They are emitted after Chroma's rules and so
// win at equal specificity, with no !important needed.
const highlightOverrides = `
/* ON Suite overrides: Chroma colours the tokens, the design system owns the
   surface, so a highlighted block matches every other panel in the suite. */
.chroma { background-color: transparent; }
.chroma .lntable { width: 100%; margin: 0; padding: 0; border: none; border-spacing: 0; }
.chroma .lntd { padding: 0; border: none; vertical-align: top; }
.chroma .lntd:first-child { width: 1%; padding-right: var(--s-3); }
.chroma .lnt { color: var(--c-text-faint); user-select: none; }
`

// HighlightCSS builds the stylesheet for the classes Highlight emits.
//
// It is generated rather than committed so it can never drift from the Chroma
// version in go.mod. The dark variant is scoped under :root[data-theme="dark"]
// rather than @media (prefers-color-scheme: dark): app.css's own theming is a
// real toggle, not the OS preference (see its "Dark mode is a real toggle"
// comment), and data-theme is always present since the server never leaves it
// unset. A prefers-color-scheme query would apply dark token colours whenever
// the OS prefers dark, even while the app itself is explicitly set to light —
// unreadable light-gray-on-white text was exactly that bug.
func HighlightCSS() ([]byte, error) {
	var buf bytes.Buffer

	buf.WriteString("/* Generated at startup by ON Paste from Chroma. Do not edit. */\n")
	if err := htmlFormatter().WriteCSS(&buf, styles.Get(styleLight)); err != nil {
		return nil, fmt.Errorf("paste: write light highlight css: %w", err)
	}

	buf.WriteString("\n:root[data-theme=\"dark\"] {\n")
	if err := htmlFormatter().WriteCSS(&buf, styles.Get(styleDark)); err != nil {
		return nil, fmt.Errorf("paste: write dark highlight css: %w", err)
	}
	buf.WriteString("}\n")

	buf.WriteString(highlightOverrides)
	return buf.Bytes(), nil
}
