package books_test

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/iliafrenkel/on-suite/internal/apps/books"
	"github.com/iliafrenkel/on-suite/internal/apptest"
	"github.com/iliafrenkel/on-suite/internal/platform/app"
)

// toolFixture is a mounted Books app with its tools, keyed by name.
type toolFixture struct {
	s     *server
	tools map[string]app.Tool
}

func newToolFixture(t *testing.T) *toolFixture {
	t.Helper()
	a := books.New()
	s := apptest.NewServer(t, a, books.NewStore)
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

func TestBooksToolsDeclared(t *testing.T) {
	tf := newToolFixture(t)
	for _, name := range []string{"books_list", "books_get", "books_stats"} {
		tool, ok := tf.tools[name]
		if !ok {
			t.Fatalf("missing tool %s", name)
		}
		if tool.Scope != "books:read" || !tool.ReadOnly || tool.Input == nil || tool.Description == "" {
			t.Errorf("%s = %+v, want read-only books:read with input and description", name, tool)
		}
	}
	for name, idempotent := range map[string]bool{
		"books_start_reading": true, "books_update_progress": false, "books_finish": true, "books_mark_dnf": true,
	} {
		tool, ok := tf.tools[name]
		if !ok {
			t.Fatalf("missing tool %s", name)
		}
		if tool.Scope != "books:write" || tool.ReadOnly || tool.Idempotent != idempotent || tool.Input == nil || tool.Description == "" {
			t.Errorf("%s = %+v, want books:write, not read-only, idempotent=%v, with input and description", name, tool, idempotent)
		}
	}
	if len(tf.tools) != 7 {
		t.Errorf("%d tools, want 7", len(tf.tools))
	}
}

func TestBooksToolsBadArguments(t *testing.T) {
	tf := newToolFixture(t)
	uid := tf.s.Alice.User.ID
	for _, args := range []string{`{"limit":"x"}`, `not json`} {
		tf.wantToolErr(t, "books_list", uid, args, "The arguments are not valid.")
	}
	for i := range 201 {
		add(t, tf.s, uid, onShelf("Book "+strconv.Itoa(i), books.ShelfWant))
	}
	count := func(args string) int {
		t.Helper()
		out, err := tf.call(t, "books_list", uid, args)
		if err != nil {
			t.Fatalf("%s: %v", args, err)
		}
		var r struct {
			Books []json.RawMessage `json:"books"`
		}
		if err := json.Unmarshal([]byte(out), &r); err != nil {
			t.Fatal(err)
		}
		return len(r.Books)
	}
	if got := count(`{"limit":0}`); got != 50 {
		t.Errorf("limit 0 = %d books, want the default 50", got)
	}
	if got := count(`{"limit":500}`); got != 200 {
		t.Errorf("limit 500 = %d books, want the cap 200", got)
	}
}

func TestBooksListFiltersAndCounts(t *testing.T) {
	tf := newToolFixture(t)
	uid := tf.s.Alice.User.ID
	add(t, tf.s, uid, onShelf("Dune", books.ShelfWant))
	add(t, tf.s, uid, onShelf("Hyperion", books.ShelfWant))
	add(t, tf.s, uid, onShelf("Solaris", books.ShelfRead))
	add(t, tf.s, tf.s.Bob.User.ID, onShelf("Bob's", books.ShelfWant))

	type result struct {
		Counts map[string]int `json:"counts"`
		Books  []struct {
			Title string `json:"title"`
			Shelf string `json:"shelf"`
		} `json:"books"`
	}
	run := func(args string) result {
		t.Helper()
		out, err := tf.call(t, "books_list", uid, args)
		if err != nil {
			t.Fatal(err)
		}
		var r result
		if err := json.Unmarshal([]byte(out), &r); err != nil {
			t.Fatalf("%v in %s", err, out)
		}
		return r
	}

	r := run(`{"shelf":"want"}`)
	if len(r.Books) != 2 {
		t.Errorf("shelf=want = %d books, want 2", len(r.Books))
	}
	for _, b := range r.Books {
		if b.Shelf != "want" || b.Title == "Bob's" {
			t.Errorf("got %+v on the want shelf", b)
		}
	}
	if r.Counts["want"] != 2 || r.Counts["read"] != 1 || r.Counts["all"] != 3 {
		t.Errorf("counts = %v, want want 2, read 1, all 3", r.Counts)
	}
	if got := len(run(`{"limit":1}`).Books); got != 1 {
		t.Errorf("limit 1 = %d books", got)
	}
	if got := len(run(`{}`).Books); got != 3 {
		t.Errorf("no args = %d books, want 3", got)
	}
}

func TestBooksListRejectsBadShelf(t *testing.T) {
	tf := newToolFixture(t)
	_, err := tf.call(t, "books_list", tf.s.Alice.User.ID, `{"shelf":"favourites"}`)
	var te app.ToolError
	if !errors.As(err, &te) {
		t.Fatalf("err = %v, want a ToolError", err)
	}
	for _, shelf := range []string{"want", "reading", "read", "dnf", "all"} {
		if !strings.Contains(te.Msg, shelf) {
			t.Errorf("message %q does not name shelf %s", te.Msg, shelf)
		}
	}
}

func TestBooksGetOtherUsersBookIsNotFound(t *testing.T) {
	tf := newToolFixture(t)
	id := add(t, tf.s, tf.s.Bob.User.ID, onShelf("Secret Title", books.ShelfWant))
	out, err := tf.call(t, "books_get", tf.s.Alice.User.ID, `{"id":`+strconv.FormatInt(id, 10)+`}`)
	var te app.ToolError
	if !errors.As(err, &te) || te.Msg != "No book with that ID on your shelves" {
		t.Fatalf("err = %v, want the not-found ToolError", err)
	}
	if strings.Contains(out, "Secret") {
		t.Errorf("output leaks %q", out)
	}
}

func TestBooksGetReturnsDetailNotesAndQuotes(t *testing.T) {
	tf := newToolFixture(t)
	uid := tf.s.Alice.User.ID
	id := add(t, tf.s, uid, books.NewBook{
		BookInput: books.BookInput{Title: "Solaris", Authors: "Stanislaw Lem", Pages: 204},
		Shelf:     books.ShelfReading, Tags: []string{"scifi"},
	})
	ctx := context.Background()
	if err := tf.s.Store.RecordProgress(ctx, uid, id, 51); err != nil {
		t.Fatal(err)
	}
	if _, err := tf.s.Store.AddNote(ctx, uid, id, books.NoteInput{Page: 10, Body: "Strange ocean"}); err != nil {
		t.Fatal(err)
	}
	if _, err := tf.s.Store.AddQuote(ctx, uid, id, books.QuoteInput{Page: 11, Text: "We don't want other worlds"}); err != nil {
		t.Fatal(err)
	}
	out, err := tf.call(t, "books_get", uid, `{"id":`+strconv.FormatInt(id, 10)+`}`)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"title":"Solaris"`, `"progress":"p. 51/204"`, `"tags":["scifi"]`, "Strange ocean", "We don't want other worlds"} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %s: %s", want, out)
		}
	}
	if strings.Contains(out, `"rating"`) {
		t.Errorf("unrated book shows a rating: %s", out)
	}
}

func TestBooksStatsDefaultsToCurrentYear(t *testing.T) {
	tf := newToolFixture(t)
	uid := tf.s.Alice.User.ID
	ctx := context.Background()
	tf.s.Clock.Set(time.Date(2026, 6, 15, 3, 0, 0, 0, time.UTC))
	id := add(t, tf.s, uid, onShelf("Solaris", books.ShelfReading))
	if err := tf.s.Store.FinishReading(ctx, uid, id, "2026-06-15", 4); err != nil {
		t.Fatal(err)
	}
	if err := tf.s.Store.SetGoal(ctx, uid, 2026, 12); err != nil {
		t.Fatal(err)
	}
	var r struct {
		Year     int   `json:"year"`
		Goal     *int  `json:"goal"`
		Finished int   `json:"finished"`
		ByMonth  []int `json:"by_month"`
		AllTime  struct {
			Finished int `json:"finished"`
		} `json:"all_time"`
	}
	out, err := tf.call(t, "books_stats", uid, `{}`)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(out), &r); err != nil {
		t.Fatal(err)
	}
	if r.Year != 2026 || r.Finished != 1 || r.Goal == nil || *r.Goal != 12 || len(r.ByMonth) != 12 || r.ByMonth[5] != 1 || r.AllTime.Finished != 1 {
		t.Errorf("stats = %s", out)
	}
	out, err = tf.call(t, "books_stats", uid, `{"year":2020}`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, `"year":2020`) || !strings.Contains(out, `"goal":null`) {
		t.Errorf("2020 stats = %s", out)
	}
}
