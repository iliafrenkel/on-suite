package notes

import (
	"context"
	"database/sql"
	"fmt"
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
