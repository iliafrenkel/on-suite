package notes_test

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"testing"

	"github.com/iliafrenkel/on-suite/internal/apps/notes"
)

type addResult struct {
	ID       int64  `json:"id"`
	Title    string `json:"title"`
	ParentID int64  `json:"parent_id"`
	Path     string `json:"path"`
}

func (tf *toolFixture) addTool(t *testing.T, userID int64, args string) addResult {
	t.Helper()
	var r addResult
	if err := json.Unmarshal([]byte(tf.mustCall(t, "notes_add", userID, args)), &r); err != nil {
		t.Fatal(err)
	}
	return r
}

func (tf *toolFixture) node(t *testing.T, userID, id int64) notes.Node {
	t.Helper()
	n, err := tf.s.Store.ByID(context.Background(), userID, id)
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func (tf *toolFixture) childTitles(t *testing.T, userID, parentID int64) []string {
	t.Helper()
	kids, err := tf.s.Store.Children(context.Background(), userID, parentID)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, k := range kids {
		out = append(out, k.Title)
	}
	return out
}

func i64(n int64) string { return strconv.FormatInt(n, 10) }

func TestNotesAddTopLevelAndUnderParent(t *testing.T) {
	tf := newToolFixture(t)
	alice := tf.s.Alice.User.ID
	groceries := tf.add(t, alice, notes.RootID, "Groceries", "")
	tf.add(t, alice, groceries, "Eggs", "")

	top := tf.addTool(t, alice, `{"text":"Buy milk"}`)
	if top.Title != "Buy milk" || top.ParentID != 0 || top.Path != "" || top.ID == 0 {
		t.Errorf("top-level result = %+v", top)
	}
	if raw := tf.mustCall(t, "notes_add", alice, `{"text":"Another","parent_id":0}`); strings.Contains(raw, "parent_id") || strings.Contains(raw, "path") {
		t.Errorf("top-level result has parent_id or path: %s", raw)
	}
	under := tf.addTool(t, alice, `{"text":"Bread","parent_id":`+i64(groceries)+`}`)
	if under.ParentID != groceries || under.Path != "Groceries" {
		t.Errorf("child result = %+v", under)
	}
	deeper := tf.addTool(t, alice, `{"text":"Rye","parent_id":`+i64(under.ID)+`}`)
	if deeper.Path != "Groceries › Bread" {
		t.Errorf("deeper path = %q", deeper.Path)
	}
	if got := strings.Join(tf.childTitles(t, alice, groceries), ","); got != "Eggs,Bread" {
		t.Errorf("Groceries children = %s, want Eggs,Bread", got)
	}
	if got := strings.Join(tf.childTitles(t, alice, notes.RootID), ","); got != "Groceries,Buy milk,Another" {
		t.Errorf("top level = %s", got)
	}
}

func TestNotesAddSplitsNote(t *testing.T) {
	tf := newToolFixture(t)
	alice := tf.s.Alice.User.ID
	r := tf.addTool(t, alice, `{"text":"  Call Sam \nabout the trip\nand the car\n"}`)
	n := tf.node(t, alice, r.ID)
	if n.Title != "Call Sam" || n.Note != "about the trip\nand the car" || r.Title != "Call Sam" {
		t.Errorf("title %q note %q", n.Title, n.Note)
	}
	r = tf.addTool(t, alice, `{"text":"Plan\r\nline one\r\nline two"}`)
	if n := tf.node(t, alice, r.ID); n.Title != "Plan" || n.Note != "line one\nline two" {
		t.Errorf("CRLF: title %q note %q", n.Title, n.Note)
	}
	r = tf.addTool(t, alice, `{"text":"Shopping list: milk, bread"}`)
	if n := tf.node(t, alice, r.ID); n.Title != "Shopping list: milk, bread" || n.Note != "" {
		t.Errorf("one line: title %q note %q", n.Title, n.Note)
	}
}

func TestNotesAddRefusals(t *testing.T) {
	tf := newToolFixture(t)
	alice, bob := tf.s.Alice.User.ID, tf.s.Bob.User.ID
	bobs := tf.add(t, bob, notes.RootID, "Bob's secret", "")
	archived := tf.add(t, alice, notes.RootID, "Old plan", "")
	child := tf.add(t, alice, archived, "Old step", "")
	if err := tf.s.Store.SetArchived(context.Background(), alice, archived, true); err != nil {
		t.Fatal(err)
	}

	tf.wantToolErr(t, "notes_add", alice, `{"text":"   "}`, "Give the note some text.")
	tf.wantToolErr(t, "notes_add", alice, `{"text":"\n\nonly a note"}`, "Give the note some text.")
	long := strings.Repeat("x", 2001)
	tf.wantToolErr(t, "notes_add", alice, `{"text":"`+long+`"}`, "The title is longer than 2000 characters.")
	for _, p := range []int64{bobs, archived, child, 999999} {
		tf.wantToolErr(t, "notes_add", alice, `{"text":"Sneaky","parent_id":`+i64(p)+`}`, notFoundMsg)
	}

	// Nothing was created anywhere. Archived bullets drop out of Children's
	// view or not; either way the counts below must be what we started with.
	if got := tf.childTitles(t, bob, bobs); len(got) != 0 {
		t.Errorf("Bob's bullet got children: %v", got)
	}
	if got := tf.childTitles(t, alice, child); len(got) != 0 {
		t.Errorf("child of an archived bullet got children: %v", got)
	}
	if got := tf.childTitles(t, alice, archived); len(got) != 1 {
		t.Errorf("archived bullet has children %v, want just its original one", got)
	}
	if got := tf.childTitles(t, alice, notes.RootID); len(got) > 1 {
		t.Errorf("Alice's top level = %v, nothing should have been added", got)
	}
}

func TestNotesAddTooDeep(t *testing.T) {
	tf := newToolFixture(t)
	alice := tf.s.Alice.User.ID
	parent := int64(notes.RootID)
	for range notes.MaxDepth + 1 { // a top-level bullet is depth 0
		parent = tf.add(t, alice, parent, "level", "")
	}
	tf.wantToolErr(t, "notes_add", alice, `{"text":"too far","parent_id":`+i64(parent)+`}`, "That would nest the outline too deeply.")
}

func TestNotesSetDone(t *testing.T) {
	tf := newToolFixture(t)
	alice := tf.s.Alice.User.ID
	parent := tf.add(t, alice, notes.RootID, "Trip", "")
	kid := tf.add(t, alice, parent, "Book flights", "")
	args := func(done string) string { return `{"id":` + i64(parent) + `,"done":` + done + `}` }

	for range 2 { // the second call changes nothing and is not an error
		var r struct {
			ID    int64  `json:"id"`
			Title string `json:"title"`
			Done  bool   `json:"done"`
		}
		if err := json.Unmarshal([]byte(tf.mustCall(t, "notes_set_done", alice, args("true"))), &r); err != nil {
			t.Fatal(err)
		}
		if r.ID != parent || r.Title != "Trip" || !r.Done || !tf.node(t, alice, parent).Done {
			t.Errorf("done result = %+v", r)
		}
	}
	if tf.node(t, alice, kid).Done {
		t.Error("ticking a parent ticked its child")
	}
	if out := tf.mustCall(t, "notes_set_done", alice, args("false")); !strings.Contains(out, `"done":false`) || tf.node(t, alice, parent).Done {
		t.Errorf("un-tick: %s", out)
	}
	tf.wantToolErr(t, "notes_set_done", alice, `{"id":`+i64(parent)+`}`, "Say whether the note is done, true or false.")
}

func TestNotesSetDoneNotFound(t *testing.T) {
	tf := newToolFixture(t)
	alice, bob := tf.s.Alice.User.ID, tf.s.Bob.User.ID
	bobs := tf.add(t, bob, notes.RootID, "Bob's secret", "")
	archived := tf.add(t, alice, notes.RootID, "Old plan", "")
	child := tf.add(t, alice, archived, "Old step", "")
	if err := tf.s.Store.SetArchived(context.Background(), alice, archived, true); err != nil {
		t.Fatal(err)
	}
	for _, id := range []int64{bobs, 999999, archived, child} {
		tf.wantToolErr(t, "notes_set_done", alice, `{"id":`+i64(id)+`,"done":true}`, notFoundMsg)
	}
	if tf.node(t, bob, bobs).Done {
		t.Error("Bob's node was ticked")
	}
	if tf.node(t, alice, archived).Done || tf.node(t, alice, child).Done {
		t.Error("an archived bullet or its child was ticked")
	}
}
