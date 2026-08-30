package notes

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// Export returns every node in rootID's subtree, in pre-order with Depth
// relative to rootID — spec §14: an export is a full data dump, not a
// display view, so unlike Outline (store.go) it does not stop at a
// collapsed node, does not exclude an archived one, and does not honour
// the show-completed preference. rootID may be RootID for the whole tree.
// It is empty both for a root with nothing under it and for a root that
// does not exist or is not userID's: a caller that needs to tell those
// apart calls ByID.
//
// The recursive descent's owner-matching mirrors Outline's own, for the
// same reason given there: parent_id is a plain foreign key, not a
// composite (parent_id, user_id) one, so matching on user_id at every step
// is what keeps a broken invariant I2 from leaking another household's
// bullets into someone's export.
func (st *Store) Export(ctx context.Context, userID, rootID int64) ([]Node, error) {
	rows, err := st.db.QueryContext(ctx,
		`WITH RECURSIVE tree AS (
		     SELECT `+nodeColumns+`, 0 AS depth, printf('%08d', position) AS path
		       FROM notes_nodes
		      WHERE user_id = ? AND parent_id IS ?
		   UNION ALL
		     SELECT `+childColumns+`,
		            t.depth + 1, t.path || '/' || printf('%08d', c.position)
		       FROM notes_nodes c JOIN tree t ON c.parent_id = t.id
		      WHERE c.user_id = t.user_id AND t.depth + 1 <= ?
		 )
		 SELECT `+nodeColumns+`, depth FROM tree ORDER BY path`,
		userID, parentArg(rootID), MaxDepth)
	if err != nil {
		return nil, fmt.Errorf("notes: export: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []Node
	for rows.Next() {
		var depth int
		n, err := scanNode(rows, &depth)
		if err != nil {
			return nil, err
		}
		n.Depth = depth
		out = append(out, n)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("notes: export: %w", err)
	}
	return out, nil
}

// ExportMarkdown renders flat (pre-order, depth-tagged) nodes as spec
// §14's Markdown outline format: a "- " line per node, two spaces of
// indent per level, an optional "[x]" suffix for done, a trailing
// "@YYYY-MM-DD" for a due date, and the note as unbulleted lines indented
// one level deeper. It is Store.Export's own output shape, so a whole-tree
// or a subtree export both feed it directly. ParseMarkdown (import.go) is
// its exact inverse — see that function's own doc comment for why suffix
// order must be stripped in the reverse of the order this appends them.
//
// A note line that would itself look like a bullet once indented (see
// escapeNoteLine below) is escaped with a leading backslash before being
// written — the only place this function's output is not a byte-for-byte
// copy of the note's own text. Every other line, including every title, is
// written completely unescaped, exactly as before this scheme existed.
func ExportMarkdown(flat []Node) string {
	var b strings.Builder
	for _, n := range flat {
		indent := strings.Repeat("  ", n.Depth)
		b.WriteString(indent)
		b.WriteString("- ")
		b.WriteString(n.Title)
		if n.Done {
			b.WriteString(" [x]")
		}
		if n.DueOn != "" {
			b.WriteString(" @")
			b.WriteString(n.DueOn)
		}
		b.WriteString("\n")
		if n.Note != "" {
			for _, line := range strings.Split(n.Note, "\n") {
				b.WriteString(indent)
				b.WriteString("  ")
				b.WriteString(escapeNoteLine(line))
				b.WriteString("\n")
			}
		}
	}
	return b.String()
}

// escapeNoteLine escapes one line of a note so that, once ExportMarkdown
// has indented it to sit under its bullet, it can never be mistaken for a
// bullet marker (bulletLineRe, import.go) on re-import. unescapeNoteLine
// (import.go) is its exact inverse.
//
// Without this, a note whose text is itself bullet-shaped breaks
// round-tripping two different ways, both filed against this exact
// function: a note reading "  - x" exports one level deeper than its own
// bullet and makes ParseMarkdown reject the WHOLE file with "bullet is
// indented deeper than its possible parent" (a 400 for the entire
// upload/paste, not just this one node); a note reading "- shopping list
// item" round-trips as TEXT but gets silently reparsed as a CHILD BULLET
// instead, changing the tree shape with no error at all.
//
// A line only needs escaping when, after its own leading run of complete
// two-space pairs — the same unit bulletLineRe's "(?:  )*" group consumes —
// the very next thing is either:
//   - "- ", bulletLineRe's own marker: left alone, ExportMarkdown's added
//     indent would make the combined line match it; or
//   - "\", the escape character this function itself uses: left alone,
//     unescapeNoteLine would mistake it for one of its own insertions and
//     strip a character of the user's actual text.
//
// Either case is fixed by inserting exactly one backslash right after that
// leading pair-run: "  - x" becomes "  \- x", and "\odd" becomes "\\odd".
// A line matching neither case — the overwhelming majority — is returned
// completely unchanged.
func escapeNoteLine(line string) string {
	prefix := pairsPrefixRe.FindString(line)
	rest := line[len(prefix):]
	if strings.HasPrefix(rest, "- ") || strings.HasPrefix(rest, "\\") {
		return prefix + "\\" + rest
	}
	return line
}

// exportedNode is the JSON on-disk shape of one node — the whole row,
// unlike the Markdown format (ExportMarkdown, Task 3), which round-trips
// only title, note, done and due — spec §14: "JSON is the safety net."
// ParentID is RootID (0) for a top-level node, Node's own convention,
// rather than a JSON null — there is no re-import path for this format
// (unlike paste's own JSON export, this one exists solely for onsuite
// export's whole-account backup), so nothing needs to parse it back.
type exportedNode struct {
	ID        int64     `json:"id"`
	ParentID  int64     `json:"parent_id"`
	Position  int       `json:"position"`
	Title     string    `json:"title"`
	Note      string    `json:"note"`
	Collapsed bool      `json:"collapsed"`
	Done      bool      `json:"done"`
	DueOn     string    `json:"due_on,omitempty"`
	Archived  bool      `json:"archived"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type exportPayload struct {
	Nodes []exportedNode `json:"nodes"`
}

// Export implements app.Exporter, joining ON Notes to onsuite export's
// whole-account JSON backup (spec §14) — distinct from GET /notes/export's
// Markdown download (Task 3), which is a different format for a different
// purpose. It takes the database directly, like every Exporter, so backup
// works from the command line without building the HTTP stack.
func (a *App) Export(ctx context.Context, handle *sql.DB, userID int64) (any, error) {
	nodes, err := NewStore(handle).Export(ctx, userID, RootID)
	if err != nil {
		return nil, err
	}
	out := exportPayload{Nodes: make([]exportedNode, 0, len(nodes))}
	for _, n := range nodes {
		out.Nodes = append(out.Nodes, exportedNode{
			ID: n.ID, ParentID: n.ParentID, Position: n.Position,
			Title: n.Title, Note: n.Note, Collapsed: n.Collapsed,
			Done: n.Done, DueOn: n.DueOn, Archived: n.Archived,
			CreatedAt: n.CreatedAt, UpdatedAt: n.UpdatedAt,
		})
	}
	return out, nil
}
