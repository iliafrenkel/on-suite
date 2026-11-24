package notes_test

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/iliafrenkel/on-suite/internal/apps/notes"
	"github.com/iliafrenkel/on-suite/internal/apptest"
	"github.com/iliafrenkel/on-suite/internal/platform/app"
)

const notFoundMsg = "No note with that ID in your outline"

// toolFixture is a mounted Notes app with its tools, keyed by name.
type toolFixture struct {
	s     *server
	tools map[string]app.Tool
}

func newToolFixture(t *testing.T) *toolFixture {
	t.Helper()
	a := notes.New()
	s := &server{apptest.NewServer(t, a, notes.NewStore)}
	tf := &toolFixture{s: s, tools: map[string]app.Tool{}}
	for _, tool := range a.Tools(app.Deps{}) {
		tf.tools[tool.Name] = tool
	}
	return tf
}

func (tf *toolFixture) call(t *testing.T, name string, userID int64, args string) (string, error) {
	t.Helper()
	tool, ok := tf.tools[name]
	if !ok {
		t.Fatalf("no tool %q", name)
	}
	return tool.Call(context.Background(), userID, json.RawMessage(args))
}

func (tf *toolFixture) mustCall(t *testing.T, name string, userID int64, args string) string {
	t.Helper()
	out, err := tf.call(t, name, userID, args)
	if err != nil {
		t.Fatalf("%s %s: %v", name, args, err)
	}
	return out
}

func (tf *toolFixture) wantToolErr(t *testing.T, name string, userID int64, args, msg string) {
	t.Helper()
	_, err := tf.call(t, name, userID, args)
	var te app.ToolError
	if !errors.As(err, &te) || te.Msg != msg {
		t.Errorf("%s %s: err = %v, want ToolError %q", name, args, err, msg)
	}
}

// add creates a bullet at the end of parentID's children.
func (tf *toolFixture) add(t *testing.T, userID, parentID int64, title, note string) int64 {
	t.Helper()
	n, err := tf.s.Store.Create(context.Background(), userID, parentID, 1<<30, title, note)
	if err != nil {
		t.Fatalf("creating %q: %v", title, err)
	}
	return n.ID
}

type searchResult struct {
	ID    int64  `json:"id"`
	Title string `json:"title"`
	Note  string `json:"note"`
	Done  bool   `json:"done"`
	Due   string `json:"due"`
	Path  string `json:"path"`
}

func (tf *toolFixture) search(t *testing.T, userID int64, args string) []searchResult {
	t.Helper()
	var r struct {
		Results []searchResult `json:"results"`
	}
	if err := json.Unmarshal([]byte(tf.mustCall(t, "notes_search", userID, args)), &r); err != nil {
		t.Fatal(err)
	}
	return r.Results
}

func TestNotesToolsDeclared(t *testing.T) {
	tf := newToolFixture(t)
	for _, name := range []string{"notes_search", "notes_get", "notes_due"} {
		tool, ok := tf.tools[name]
		if !ok {
			t.Fatalf("missing tool %s", name)
		}
		if tool.Scope != "notes:read" || !tool.ReadOnly || tool.Input == nil || tool.Description == "" {
			t.Errorf("%s = %+v, want read-only notes:read with input and description", name, tool)
		}
	}
	for _, name := range []string{"notes_add", "notes_set_done", "notes_set_due"} {
		tool, ok := tf.tools[name]
		if !ok {
			t.Fatalf("missing tool %s", name)
		}
		if tool.Scope != "notes:write" || tool.ReadOnly || tool.Input == nil || tool.Description == "" {
			t.Errorf("%s = %+v, want writing notes:write with input and description", name, tool)
		}
	}
	if !tf.tools["notes_set_done"].Idempotent || !tf.tools["notes_set_due"].Idempotent || tf.tools["notes_add"].Idempotent {
		t.Error("notes_set_done and notes_set_due must be idempotent and notes_add must not be")
	}
	if len(tf.tools) != 6 {
		t.Errorf("%d tools, want 6", len(tf.tools))
	}
}

func TestNotesSearchFindsWithPath(t *testing.T) {
	tf := newToolFixture(t)
	alice, bob := tf.s.Alice.User.ID, tf.s.Bob.User.ID
	groceries := tf.add(t, alice, notes.RootID, "Groceries", "")
	milk := tf.add(t, alice, groceries, "Milk", "")
	tf.add(t, bob, notes.RootID, "Bob's milk", "")

	got := tf.search(t, alice, `{"query":"milk"}`)
	if len(got) != 1 || got[0].ID != milk || got[0].Title != "Milk" || got[0].Path != "Groceries" {
		t.Errorf("results = %+v, want just Milk under Groceries", got)
	}
}

func TestNotesSearchIncludesDoneAndCapsLimit(t *testing.T) {
	tf := newToolFixture(t)
	alice := tf.s.Alice.User.ID
	done := tf.add(t, alice, notes.RootID, "milk done", strings.Repeat("x", 250))
	if err := tf.s.Store.SetDone(context.Background(), alice, done, true); err != nil {
		t.Fatal(err)
	}
	got := tf.search(t, alice, `{"query":"milk"}`)
	if len(got) != 1 || !got[0].Done {
		t.Fatalf("results = %+v, want the done bullet, marked done", got)
	}
	if want := strings.Repeat("x", 200) + "…"; got[0].Note != want {
		t.Errorf("note = %q, want 200 runes and an ellipsis", got[0].Note)
	}

	for i := range 59 {
		tf.add(t, alice, notes.RootID, "milk "+strconv.Itoa(i), "")
	}
	if n := len(tf.search(t, alice, `{"query":"milk","limit":100}`)); n != 50 {
		t.Errorf("limit 100: %d results, want 50", n)
	}
	if n := len(tf.search(t, alice, `{"query":"milk"}`)); n != 20 {
		t.Errorf("no limit: %d results, want 20", n)
	}
	if n := len(tf.search(t, alice, `{"query":"milk","limit":3}`)); n != 3 {
		t.Errorf("limit 3: %d results, want 3", n)
	}
}

func TestNotesSearchNeedsQuery(t *testing.T) {
	tf := newToolFixture(t)
	for _, args := range []string{`{}`, `{"query":"  "}`} {
		tf.wantToolErr(t, "notes_search", tf.s.Alice.User.ID, args, "Give some words to search for.")
	}
	tf.wantToolErr(t, "notes_search", tf.s.Alice.User.ID, `{"query":5}`, "The arguments are not valid.")
}

func TestNotesGetWholeOutline(t *testing.T) {
	tf := newToolFixture(t)
	alice := tf.s.Alice.User.ID
	groceries := tf.add(t, alice, notes.RootID, "Groceries", "")
	milk := tf.add(t, alice, groceries, "Milk", "")
	bread := tf.add(t, alice, groceries, "Bread", "wholemeal\nsliced")
	work := tf.add(t, alice, notes.RootID, "Work", "")
	ctx := context.Background()
	if err := tf.s.Store.SetDone(ctx, alice, milk, true); err != nil {
		t.Fatal(err)
	}
	if err := tf.s.Store.SetDue(ctx, alice, bread, "2026-12-01"); err != nil {
		t.Fatal(err)
	}

	id := strconv.FormatInt
	want := strings.Join([]string{
		"- Groceries [id " + id(groceries, 10) + "]",
		"  - Milk [id " + id(milk, 10) + "] [done]",
		"  - Bread [id " + id(bread, 10) + "] (due 2026-12-01)",
		"    | wholemeal",
		"    | sliced",
		"- Work [id " + id(work, 10) + "]",
	}, "\n")
	if got := tf.mustCall(t, "notes_get", alice, `{}`); got != want {
		t.Errorf("outline =\n%s\nwant\n%s", got, want)
	}
}

func TestNotesGetSubtreeWithPathAndDepth(t *testing.T) {
	tf := newToolFixture(t)
	alice := tf.s.Alice.User.ID
	a := tf.add(t, alice, notes.RootID, "A", "")
	b := tf.add(t, alice, a, "B", "")
	c := tf.add(t, alice, b, "C", "")
	d := tf.add(t, alice, c, "D", "")

	id := func(n int64) string { return strconv.FormatInt(n, 10) }
	want := "Path: A\n- B [id " + id(b) + "]\n  - C [id " + id(c) + "] (+1 more)"
	if got := tf.mustCall(t, "notes_get", alice, `{"id":`+id(b)+`,"depth":1}`); got != want {
		t.Errorf("depth 1 =\n%s\nwant\n%s", got, want)
	}
	want = "Path: A\n- B [id " + id(b) + "]\n  - C [id " + id(c) + "]\n    - D [id " + id(d) + "]"
	if got := tf.mustCall(t, "notes_get", alice, `{"id":`+id(b)+`,"depth":2}`); got != want {
		t.Errorf("depth 2 =\n%s\nwant\n%s", got, want)
	}
	// A top-level bullet has no path line.
	if got := tf.mustCall(t, "notes_get", alice, `{"id":`+id(a)+`,"depth":1}`); strings.Contains(got, "Path:") {
		t.Errorf("top-level bullet has a path: %q", got)
	}
}

func TestNotesGetNotFound(t *testing.T) {
	tf := newToolFixture(t)
	alice, bob := tf.s.Alice.User.ID, tf.s.Bob.User.ID
	bobs := tf.add(t, bob, notes.RootID, "Bob's secret", "")
	archived := tf.add(t, alice, notes.RootID, "Old plan", "")
	child := tf.add(t, alice, archived, "Old step", "")
	if err := tf.s.Store.SetArchived(context.Background(), alice, archived, true); err != nil {
		t.Fatal(err)
	}
	for _, id := range []int64{bobs, 999999, archived, child} {
		args := `{"id":` + strconv.FormatInt(id, 10) + `}`
		tf.wantToolErr(t, "notes_get", alice, args, notFoundMsg)
	}
}

func TestNotesGetBadDepth(t *testing.T) {
	tf := newToolFixture(t)
	alice := tf.s.Alice.User.ID
	tf.add(t, alice, notes.RootID, "Only", "")
	if got := tf.mustCall(t, "notes_get", alice, `{"depth":0}`); !strings.HasPrefix(got, "- Only [id ") {
		t.Errorf("depth 0 = %q, want the default depth to apply", got)
	}
	for _, args := range []string{`{"depth":11}`, `{"depth":-1}`} {
		tf.wantToolErr(t, "notes_get", alice, args, "Give a depth from 1 to 10.")
	}
}

func TestNotesGetCapsLongOutline(t *testing.T) {
	tf := newToolFixture(t)
	alice := tf.s.Alice.User.ID
	note := strings.Repeat("é", 9000)
	for i := range 30 {
		tf.add(t, alice, notes.RootID, "Bullet "+strconv.Itoa(i), note)
	}
	got := tf.mustCall(t, "notes_get", alice, `{}`)
	if !strings.HasSuffix(got, "[Cut at 200,000 characters. Ask for a smaller part.]") {
		t.Errorf("long outline ends %q, want the cut note", got[max(0, len(got)-80):])
	}
}

func TestNotesGetEmpty(t *testing.T) {
	tf := newToolFixture(t)
	if got := tf.mustCall(t, "notes_get", tf.s.Alice.User.ID, `{}`); got != "Your outline is empty." {
		t.Errorf("got %q", got)
	}
}

func TestNotesDueGroups(t *testing.T) {
	tf := newToolFixture(t)
	alice := tf.s.Alice.User.ID
	ctx := context.Background()
	// Local noon, so "today" is the same date in every time zone.
	now := time.Date(2026, 6, 15, 12, 0, 0, 0, time.Local)
	tf.s.Clock.Set(now)

	due := func(title string, parent int64, days int) int64 {
		id := tf.add(t, alice, parent, title, "")
		if err := tf.s.Store.SetDue(ctx, alice, id, now.AddDate(0, 0, days).Format("2006-01-02")); err != nil {
			t.Fatal(err)
		}
		return id
	}
	due("Late", notes.RootID, -1)
	due("Now", notes.RootID, 0)
	finished := due("Finished", notes.RootID, 0)
	if err := tf.s.Store.SetDone(ctx, alice, finished, true); err != nil {
		t.Fatal(err)
	}
	project := tf.add(t, alice, notes.RootID, "Project", "")
	due("Soon", project, 3)
	due("Far", notes.RootID, 30)

	raw := tf.mustCall(t, "notes_due", alice, ``)
	var groups map[string][]struct {
		Title, Due, Path string
	}
	if err := json.Unmarshal([]byte(raw), &groups); err != nil {
		t.Fatal(err)
	}
	for key, title := range map[string]string{"overdue": "Late", "today": "Now", "this_week": "Soon", "later": "Far"} {
		g, ok := groups[key]
		if !ok || len(g) != 1 || g[0].Title != title {
			t.Errorf("%s = %+v (present %v), want just %q", key, g, ok, title)
		}
	}
	if p := groups["this_week"][0]; p.Path != "Project" || p.Due != "2026-06-18" {
		t.Errorf("this_week entry = %+v, want path Project and due 2026-06-18", p)
	}
	if strings.Contains(raw, "Finished") {
		t.Error("a done bullet is listed")
	}

	// Empty groups are [], not null.
	empty := newToolFixture(t)
	if got := empty.mustCall(t, "notes_due", empty.s.Alice.User.ID, `{}`); got != `{"overdue":[],"today":[],"this_week":[],"later":[]}` {
		t.Errorf("empty = %s", got)
	}
}
