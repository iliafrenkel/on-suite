package notes_test

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"
	"testing"

	"github.com/iliafrenkel/on-suite/internal/apps/notes"
)

// rawNode is a row of notes_nodes as the checker sees it: structure only, no
// text.
type rawNode struct {
	ID       int64
	UserID   int64
	ParentID sql.NullInt64
	Position int
}

// loadRawNodes reads every row in the table, for every user.
func loadRawNodes(t *testing.T, handle *sql.DB) []rawNode {
	t.Helper()

	rows, err := handle.QueryContext(context.Background(),
		`SELECT id, user_id, parent_id, position FROM notes_nodes`)
	if err != nil {
		t.Fatalf("loading nodes: %v", err)
	}
	defer func() { _ = rows.Close() }()

	var out []rawNode
	for rows.Next() {
		var n rawNode
		if err := rows.Scan(&n.ID, &n.UserID, &n.ParentID, &n.Position); err != nil {
			t.Fatalf("scanning node: %v", err)
		}
		out = append(out, n)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("loading nodes: %v", err)
	}
	return out
}

// treeViolations reports every way nodes breaks invariants I1-I4 of the spec:
//
//	I1  the children of a parent occupy positions 0..n-1 exactly
//	I2  a node's parent exists and belongs to the same user
//	I3  no node is its own ancestor
//	I4  no node is deeper than MaxDepth
func treeViolations(nodes []rawNode) []string {
	byID := make(map[int64]rawNode, len(nodes))
	for _, n := range nodes {
		byID[n.ID] = n
	}
	var out []string

	// I2 first: the later checks assume parents resolve.
	for _, n := range nodes {
		if !n.ParentID.Valid {
			continue
		}
		p, ok := byID[n.ParentID.Int64]
		if !ok {
			out = append(out, fmt.Sprintf("I2: node %d has parent %d, which does not exist", n.ID, n.ParentID.Int64))
			continue
		}
		if p.UserID != n.UserID {
			out = append(out, fmt.Sprintf("I2: node %d (user %d) has parent %d (user %d)", n.ID, n.UserID, p.ID, p.UserID))
		}
	}

	// I1: positions are contiguous within each (user, parent) group.
	type group struct{ userID, parentID int64 }
	siblings := make(map[group][]int)
	for _, n := range nodes {
		g := group{userID: n.UserID, parentID: notes.RootID}
		if n.ParentID.Valid {
			g.parentID = n.ParentID.Int64
		}
		siblings[g] = append(siblings[g], n.Position)
	}
	keys := make([]group, 0, len(siblings))
	for g := range siblings {
		keys = append(keys, g)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].userID != keys[j].userID {
			return keys[i].userID < keys[j].userID
		}
		return keys[i].parentID < keys[j].parentID
	})
	for _, g := range keys {
		got := append([]int(nil), siblings[g]...)
		sort.Ints(got)
		for i, p := range got {
			if p != i {
				out = append(out, fmt.Sprintf("I1: user %d, parent %d has positions %v; want 0..%d",
					g.userID, g.parentID, got, len(got)-1))
				break
			}
		}
	}

	// I3 and I4: walking up from any node reaches the top without revisiting
	// a node, in at most MaxDepth steps.
	for _, n := range nodes {
		seen := map[int64]bool{n.ID: true}
		cur, depth := n, 0
		for cur.ParentID.Valid {
			next, ok := byID[cur.ParentID.Int64]
			if !ok {
				break // already reported as an I2 violation
			}
			if seen[next.ID] {
				out = append(out, fmt.Sprintf("I3: node %d is inside its own subtree", n.ID))
				break
			}
			seen[next.ID] = true
			depth++
			if depth > notes.MaxDepth {
				out = append(out, fmt.Sprintf("I4: node %d is deeper than MaxDepth (%d)", n.ID, notes.MaxDepth))
				break
			}
			cur = next
		}
	}
	return out
}

// checkInvariants fails the test if the whole table violates anything.
func checkInvariants(t *testing.T, handle *sql.DB) {
	t.Helper()
	if v := treeViolations(loadRawNodes(t, handle)); len(v) > 0 {
		t.Fatalf("tree invariants violated:\n  %s", strings.Join(v, "\n  "))
	}
}

func TestTreeViolationsAcceptsAHealthyTree(t *testing.T) {
	under := func(id int64) sql.NullInt64 { return sql.NullInt64{Int64: id, Valid: true} }
	nodes := []rawNode{
		{ID: 1, UserID: 1, Position: 0},
		{ID: 2, UserID: 1, Position: 1},
		{ID: 3, UserID: 1, ParentID: under(1), Position: 0},
		{ID: 4, UserID: 1, ParentID: under(1), Position: 1},
		{ID: 5, UserID: 2, Position: 0}, // a second user's separate tree
	}
	if v := treeViolations(nodes); len(v) > 0 {
		t.Fatalf("treeViolations on a healthy tree = %v; want none", v)
	}
}

func TestTreeViolationsCatchesCorruption(t *testing.T) {
	under := func(id int64) sql.NullInt64 { return sql.NullInt64{Int64: id, Valid: true} }
	tests := []struct {
		name  string
		nodes []rawNode
		want  string
	}{
		{
			name:  "gap in sibling positions",
			nodes: []rawNode{{ID: 1, UserID: 1, Position: 0}, {ID: 2, UserID: 1, Position: 2}},
			want:  "I1",
		},
		{
			name:  "two siblings share a position",
			nodes: []rawNode{{ID: 1, UserID: 1, Position: 0}, {ID: 2, UserID: 1, Position: 0}},
			want:  "I1",
		},
		{
			name:  "positions do not start at zero",
			nodes: []rawNode{{ID: 1, UserID: 1, Position: 1}},
			want:  "I1",
		},
		{
			name:  "child of another user's node",
			nodes: []rawNode{{ID: 1, UserID: 1, Position: 0}, {ID: 2, UserID: 2, ParentID: under(1), Position: 0}},
			want:  "I2",
		},
		{
			name:  "parent does not exist",
			nodes: []rawNode{{ID: 2, UserID: 1, ParentID: under(99), Position: 0}},
			want:  "I2",
		},
		{
			name: "two-node cycle",
			nodes: []rawNode{
				{ID: 1, UserID: 1, ParentID: under(2), Position: 0},
				{ID: 2, UserID: 1, ParentID: under(1), Position: 0},
			},
			want: "I3",
		},
		{
			name:  "node is its own parent",
			nodes: []rawNode{{ID: 1, UserID: 1, ParentID: under(1), Position: 0}},
			want:  "I3",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := treeViolations(tc.nodes)
			if len(got) == 0 {
				t.Fatalf("treeViolations found nothing; want a %s violation", tc.want)
			}
			if !strings.Contains(strings.Join(got, "\n"), tc.want) {
				t.Fatalf("treeViolations = %v; want a %s violation", got, tc.want)
			}
		})
	}
}
