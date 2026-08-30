package notes

import (
	"fmt"
	"regexp"
	"strings"
)

// ParsedNode is one bullet of a parsed Markdown outline (spec §14), flat
// and pre-order — the same shape Store.Export produces (export.go) —
// Depth relative to whatever parent the caller eventually attaches these
// under. It carries no id: these are not yet real nodes.
type ParsedNode struct {
	Depth int
	Title string
	Note  string
	Done  bool
	DueOn string
}

// bulletLineRe matches a "- " line and captures its indent (an even
// number of spaces) and everything after the marker. RE2 (Go's regexp
// package), so this can never backtrack into pathological time on
// adversarial input — the same reasoning markdown.go's inline renderer
// already documents for its own patterns.
var bulletLineRe = regexp.MustCompile(`^((?:  )*)- (.*)$`)

// dueSuffixRe matches ExportMarkdown's trailing "@YYYY-MM-DD", greedily:
// the leading .* must consume as much as possible, which is what makes
// this find the rightmost such suffix rather than the first one that
// happens to fit — see parseTitleLine's own doc comment for why order
// matters here.
var dueSuffixRe = regexp.MustCompile(`^(.*) @(\d{4}-\d{2}-\d{2})$`)

const doneSuffix = " [x]"

// ParseMarkdown parses spec §14's outline format: a "- " line per node,
// two spaces of indent per level, an optional "[x]" suffix for done and a
// trailing "@YYYY-MM-DD" for a due date, with the note as unbulleted
// lines indented one level deeper than their bullet. It is shared by
// POST /notes/import (a file, Task 5) and POST /notes/{id}/paste (a
// clipboard block, Task 6) — spec §14's "the same code path, reached from
// the editor instead of from a file" — so it does no I/O of its own:
// Ops.ImportUnder (Task 5) is what turns its result into real nodes.
//
// A malformed line is a parse error rather than a best-effort guess — see
// this task's own design note in the plan for why silently dropping part
// of an upload, the way a corrupted display row is tolerated elsewhere in
// this package, is the wrong instinct for untrusted input.
//
// Only the exact "- " marker (a literal hyphen and one space) is
// recognised, and only an exactly-even number of leading spaces — no
// other Markdown bullet syntax ("* ", "+ ") and no odd indentation are
// accepted, matching exactly what ExportMarkdown ever produces.
func ParseMarkdown(text string) ([]ParsedNode, error) {
	var out []ParsedNode
	lastBullet := -1 // index into out of the most recently seen bullet
	lastDepth := -1  // that bullet's own Depth

	for i, raw := range strings.Split(text, "\n") {
		line := strings.TrimRight(raw, "\r")
		if strings.TrimSpace(line) == "" {
			continue
		}

		if m := bulletLineRe.FindStringSubmatch(line); m != nil {
			depth := len(m[1]) / 2
			if depth > lastDepth+1 {
				return nil, fmt.Errorf("%w: line %d: bullet is indented deeper than its possible parent", ErrInvalid, i+1)
			}
			if len(out) >= MaxImportNodes {
				return nil, fmt.Errorf("%w: import exceeds the %d-bullet limit", ErrInvalid, MaxImportNodes)
			}
			title, done, dueOn := parseTitleLine(m[2])
			out = append(out, ParsedNode{Depth: depth, Title: title, Done: done, DueOn: dueOn})
			lastBullet, lastDepth = len(out)-1, depth
			continue
		}

		if strings.HasPrefix(strings.TrimLeft(line, " "), "- ") {
			// Looks like a bullet, but its indentation was not a clean
			// multiple of two spaces — bulletLineRe above would have
			// matched it otherwise. Reported explicitly rather than
			// falling through to the note-line branch below, which could
			// otherwise silently misfile a badly-indented bullet as plain
			// text.
			return nil, fmt.Errorf("%w: line %d: bullet indentation must be a multiple of two spaces", ErrInvalid, i+1)
		}

		minIndent := (lastDepth + 1) * 2
		indent := leadingSpaces(line)
		if lastBullet < 0 || indent < minIndent {
			return nil, fmt.Errorf("%w: line %d: text is not indented under any bullet", ErrInvalid, i+1)
		}
		noteLine := line[minIndent:]
		if out[lastBullet].Note == "" {
			out[lastBullet].Note = noteLine
		} else {
			out[lastBullet].Note += "\n" + noteLine
		}
	}
	return out, nil
}

func leadingSpaces(s string) int {
	n := 0
	for n < len(s) && s[n] == ' ' {
		n++
	}
	return n
}

// parseTitleLine strips a bullet's optional "[x]" and "@YYYY-MM-DD"
// suffixes. The due-date suffix is stripped first — the reverse of
// ExportMarkdown's own append order (export.go: title, then " [x]", then
// " @date") — because it is the rightmost of the two: a line carrying
// both must have its true trailing suffix removed before the "[x]" check
// looks at what remains, or "title [x] @2026-09-01" would never match the
// due-date pattern at all (the literal "[x]" would sit between the title
// and "$").
//
// Neither check semantically validates its value: an out-of-range date
// like "2026-02-30" parses here exactly as written, and ValidateDue's own
// check runs later, inside the same transaction as node creation
// (Ops.ImportUnder, Task 5, via Ops.SetDue) — duplicating that check here
// would just be the same logic run twice.
func parseTitleLine(s string) (title string, done bool, dueOn string) {
	title = s
	if m := dueSuffixRe.FindStringSubmatch(title); m != nil {
		title, dueOn = m[1], m[2]
	}
	if strings.HasSuffix(title, doneSuffix) {
		title = strings.TrimSuffix(title, doneSuffix)
		done = true
	}
	return title, done, dueOn
}
