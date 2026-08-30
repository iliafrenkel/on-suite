package notes

import (
	"context"
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

// pairsPrefixRe matches a line's leading run of complete two-space pairs —
// the same unit bulletLineRe's own "(?:  )*" group consumes. escapeNoteLine
// (export.go) and unescapeNoteLine below both anchor on this same prefix,
// which is what makes them exact inverses of each other regardless of how
// deeply indented the note's own bullet is.
var pairsPrefixRe = regexp.MustCompile(`^(?:  )*`)

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
//
// A note-continuation line is run through unescapeNoteLine before it is
// stored, reversing escapeNoteLine's (export.go) backslash escape for a
// note line that would otherwise look like a bullet — so the caller sees
// the user's original note text, byte for byte, never the escaped form
// that actually sat on disk.
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
		// Only minIndent characters are stripped, never more: indentation
		// beyond the bullet's own minimum is the user's content, not
		// structural whitespace, so it survives into the note verbatim.
		noteLine := unescapeNoteLine(line[minIndent:])
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

// unescapeNoteLine reverses escapeNoteLine (export.go): a note-continuation
// line whose content, after its own leading two-space-pair run, starts
// with a backslash had exactly one such backslash inserted by
// ExportMarkdown — stripping it recovers the user's original text, byte
// for byte. A line that was never escaped (its content, at that same
// position, starts with neither "- " nor "\") is returned unchanged.
func unescapeNoteLine(line string) string {
	prefix := pairsPrefixRe.FindString(line)
	rest := line[len(prefix):]
	if strings.HasPrefix(rest, "\\") {
		return prefix + rest[1:]
	}
	return line
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

// ImportUnder inserts parsed as new children of parentID, appended after
// whatever is already there, in one transaction — spec §14: import and
// paste-into-a-bullet (Task 6) share this exact code path. Returns how
// many nodes were created.
//
// parsed's Depth values are trusted never to skip a level: ParseMarkdown
// (Task 4) already rejects that shape at parse time, so unlike nest
// (view.go), which tolerates and drops a corrupted display row, this
// never needs to guess at one — the default branch below exists only as a
// guard against parsed arriving from some future second producer that
// does not go through ParseMarkdown, not as an expected path today.
//
// Depth and node-count bounds are Create's and ParseMarkdown's own —
// MaxDepth via Create's existing ErrTooDeep, MaxImportNodes already
// enforced by ParseMarkdown before this ever runs — so nothing here
// duplicates either check.
func (o *Ops) ImportUnder(ctx context.Context, userID, parentID int64, parsed []ParsedNode) (int, error) {
	open := make([]int64, 0, MaxDepth+1) // open[d] is the id of the ancestor at depth d
	created := 0

	for _, p := range parsed {
		parent := parentID
		switch {
		case p.Depth == 0:
			open = open[:0]
		case p.Depth > 0 && p.Depth <= len(open):
			parent = open[p.Depth-1]
			open = open[:p.Depth]
		default:
			return created, fmt.Errorf("%w: malformed parsed node at depth %d", ErrInvalid, p.Depth)
		}

		n, err := o.Create(ctx, userID, parent, maxPosition, p.Title, p.Note)
		if err != nil {
			return created, err
		}
		if p.Done {
			if err := o.SetDone(ctx, userID, n.ID, true); err != nil {
				return created, err
			}
		}
		if p.DueOn != "" {
			if err := o.SetDue(ctx, userID, n.ID, p.DueOn); err != nil {
				return created, err
			}
		}
		open = append(open, n.ID)
		created++
	}
	return created, nil
}

// ImportUnder inserts parsed as new children of parentID, in a
// transaction of its own. See Ops.ImportUnder.
func (st *Store) ImportUnder(ctx context.Context, userID, parentID int64, parsed []ParsedNode) (int, error) {
	var created int
	err := st.Do(ctx, func(o *Ops) error {
		var err error
		created, err = o.ImportUnder(ctx, userID, parentID, parsed)
		return err
	})
	return created, err
}
