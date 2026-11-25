package later_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"sync/atomic"
	"testing"

	"github.com/iliafrenkel/on-suite/internal/apps/later"
)

type writeResult struct {
	Notice       string   `json:"notice"`
	ID           int64    `json:"id"`
	Title        string   `json:"title"`
	State        string   `json:"state"`
	Tags         []string `json:"tags"`
	AlreadySaved bool     `json:"already_saved"`
	LinkOnly     bool     `json:"link_only"`
	Reason       string   `json:"reason"`
}

func decodeWrite(t *testing.T, out string) writeResult {
	t.Helper()
	var r writeResult
	if err := json.Unmarshal([]byte(out), &r); err != nil {
		t.Fatalf("result %q: %v", out, err)
	}
	return r
}

func itoa(id int64) string { return strconv.FormatInt(id, 10) }

func TestLaterSaveFetchesAndTags(t *testing.T) {
	tf := newToolFixture(t)
	tf.app.AllowPrivateFetchesForTest()
	origin := pageOrigin(t, "text/html; charset=utf-8", articlePage)
	alice := tf.s.Alice.User.ID

	r := decodeWrite(t, tf.mustCall(t, "later_save", alice, `{"url":"`+origin.URL+`/essay","tags":["Recipes","#quick"]}`))
	if r.Notice == "" || r.Title == "" || r.AlreadySaved || r.LinkOnly || r.State != "unread" {
		t.Errorf("result = %+v", r)
	}
	if !slices.Equal(r.Tags, []string{"quick", "recipes"}) {
		t.Errorf("tags = %v", r.Tags)
	}
	art, err := tf.s.Store.Article(context.Background(), alice, r.ID)
	if err != nil || art.Content != later.ContentExtracted {
		t.Fatalf("article = %+v, %v", art, err)
	}
}

func TestLaterSaveAgainMergesTags(t *testing.T) {
	tf := newToolFixture(t)
	tf.app.AllowPrivateFetchesForTest()
	var hits atomic.Int32
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(articlePage))
	}))
	t.Cleanup(origin.Close)
	alice := tf.s.Alice.User.ID

	first := decodeWrite(t, tf.mustCall(t, "later_save", alice, `{"url":"`+origin.URL+`/essay","tags":["Recipes","#quick"]}`))
	before := hits.Load()
	r := decodeWrite(t, tf.mustCall(t, "later_save", alice, `{"url":"`+origin.URL+`/essay#x","tags":["travel"]}`))
	if !r.AlreadySaved || r.ID != first.ID {
		t.Errorf("result = %+v, want already_saved for %d", r, first.ID)
	}
	if !slices.Equal(r.Tags, []string{"quick", "recipes", "travel"}) {
		t.Errorf("tags = %v", r.Tags)
	}
	if hits.Load() != before {
		t.Errorf("origin hit %d more times, want no refetch", hits.Load()-before)
	}
}

func TestLaterSaveLinkOnlyAndBadURL(t *testing.T) {
	tf := newToolFixture(t)
	tf.app.AllowPrivateFetchesForTest()
	origin := httptest.NewServer(http.NotFoundHandler())
	t.Cleanup(origin.Close)
	alice := tf.s.Alice.User.ID

	r := decodeWrite(t, tf.mustCall(t, "later_save", alice, `{"url":"`+origin.URL+`/gone"}`))
	if !r.LinkOnly || r.AlreadySaved {
		t.Errorf("result = %+v, want link_only", r)
	}
	if r.Reason == "" {
		t.Error("link-only result has no reason")
	}
	tf.wantToolErr(t, "later_save", alice, `{"url":"not a url"}`,
		"That doesn't look like a web address. It needs to start with http:// or https://.")
}

func TestLaterSaveRefusesPrivateAddresses(t *testing.T) {
	tf := newToolFixture(t) // no AllowPrivateFetchesForTest: the real guard
	alice := tf.s.Alice.User.ID

	r := decodeWrite(t, tf.mustCall(t, "later_save", alice, `{"url":"http://127.0.0.1:1/x"}`))
	if !r.LinkOnly {
		t.Fatalf("result = %+v, want link_only", r)
	}
	const want = "That address is on a private network, so ON Later won't fetch it."
	if r.Reason != want {
		t.Errorf("reason = %q, want %q", r.Reason, want)
	}
	art, err := tf.s.Store.Article(context.Background(), alice, r.ID)
	if err != nil || art.ExtractError != want {
		t.Errorf("ExtractError = %q, %v, want %q", art.ExtractError, err, want)
	}
}

func TestLaterTagKeepsExistingAtTheCap(t *testing.T) {
	tf := newToolFixture(t)
	alice := tf.s.Alice.User.ID
	var have []string
	for i := range later.MaxTags {
		have = append(have, "t"+strconv.Itoa(100+i))
	}
	art := tf.save(t, alice, later.NewArticle{URL: "https://a.example.com/1", Title: "One", ContentHTML: "<p>one</p>", Tags: have})

	tf.mustCall(t, "later_tag", alice, `{"id":`+itoa(art.ID)+`,"add":["a"]}`)
	got, err := tf.s.Store.ArticleTags(context.Background(), alice, art.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != later.MaxTags {
		t.Fatalf("got %d tags, want %d", len(got), later.MaxTags)
	}
	for _, tag := range have {
		if !slices.Contains(got, tag) {
			t.Errorf("original tag %q was dropped", tag)
		}
	}
}

func TestLaterSetState(t *testing.T) {
	tf := newToolFixture(t)
	ctx := context.Background()
	alice, bob := tf.s.Alice.User.ID, tf.s.Bob.User.ID
	art := tf.save(t, alice, later.NewArticle{URL: "https://a.example.com/1", Title: "One", ContentHTML: "<p>one</p>"})
	bobs := tf.save(t, bob, later.NewArticle{URL: "https://b.example.com/1", Title: "Bob's", ContentHTML: "<p>b</p>"})
	set := func(id int64, state string) string {
		return `{"id":` + itoa(id) + `,"state":"` + state + `"}`
	}

	for range 2 {
		r := decodeWrite(t, tf.mustCall(t, "later_set_state", alice, set(art.ID, "archived")))
		if r.State != "archived" || r.ID != art.ID {
			t.Fatalf("archived: %+v", r)
		}
	}
	if got, _ := tf.s.Store.Article(ctx, alice, art.ID); got.State != later.StateArchived {
		t.Errorf("state = %q", got.State)
	}
	if err := tf.s.Store.SetProgress(ctx, alice, art.ID, 0.5); err != nil {
		t.Fatal(err)
	}
	tf.mustCall(t, "later_set_state", alice, set(art.ID, "unread"))
	if got, _ := tf.s.Store.Article(ctx, alice, art.ID); got.State != later.StateUnread || got.Progress != 0 {
		t.Errorf("after unread: state %q progress %v", got.State, got.Progress)
	}
	tf.wantToolErr(t, "later_set_state", alice, set(art.ID, "reading"), "An article becomes reading when it's opened. Use unread or archived.")
	tf.wantToolErr(t, "later_set_state", alice, set(art.ID, "done"), "State must be unread or archived.")
	tf.wantToolErr(t, "later_set_state", alice, set(bobs.ID, "archived"), laterNotFoundMsg)
	if got, _ := tf.s.Store.Article(ctx, bob, bobs.ID); got.State != later.StateUnread {
		t.Errorf("Bob's article changed to %q", got.State)
	}
}

func TestLaterTag(t *testing.T) {
	tf := newToolFixture(t)
	alice, bob := tf.s.Alice.User.ID, tf.s.Bob.User.ID
	art := tf.save(t, alice, later.NewArticle{URL: "https://a.example.com/1", Title: "One", ContentHTML: "<p>one</p>", Tags: []string{"recipes"}})
	bobs := tf.save(t, bob, later.NewArticle{URL: "https://b.example.com/1", Title: "Bob's", ContentHTML: "<p>b</p>", Tags: []string{"mine"}})
	call := func(rest string) string { return `{"id":` + itoa(art.ID) + rest + `}` }

	tags := func(args string) []string {
		var r struct {
			ID   int64    `json:"id"`
			Tags []string `json:"tags"`
		}
		if err := json.Unmarshal([]byte(tf.mustCall(t, "later_tag", alice, args)), &r); err != nil || r.ID != art.ID {
			t.Fatalf("%s: %+v, %v", args, r, err)
		}
		return r.Tags
	}

	if got := tags(call(`,"add":["Travel"]`)); !slices.Equal(got, []string{"recipes", "travel"}) {
		t.Errorf("add = %v", got)
	}
	for range 2 {
		if got := tags(call(`,"remove":["#recipes"]`)); !slices.Equal(got, []string{"travel"}) {
			t.Errorf("remove = %v", got)
		}
	}
	if got := tags(call(`,"add":["a"],"remove":["travel"]`)); !slices.Equal(got, []string{"a"}) {
		t.Errorf("add and remove = %v", got)
	}
	tf.wantToolErr(t, "later_tag", alice, call(``), "Give tags to add or remove.")
	tf.wantToolErr(t, "later_tag", alice, `{"id":`+itoa(bobs.ID)+`,"add":["x"]}`, laterNotFoundMsg)
	if got, _ := tf.s.Store.ArticleTags(context.Background(), bob, bobs.ID); !slices.Equal(got, []string{"mine"}) {
		t.Errorf("Bob's tags = %v", got)
	}
}
