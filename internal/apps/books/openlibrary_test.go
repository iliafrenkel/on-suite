package books_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/iliafrenkel/on-suite/internal/apps/books"
	"github.com/iliafrenkel/on-suite/internal/platform/webfetch"
)

// searchJSON is a trimmed real search.json answer for "piranesi", plus a
// doc with no title (skipped) and one with only ISBN-10s and an absurd
// year (kept, cleaned).
const searchJSON = `{"numFound":3,"docs":[
 {"author_name":["Susanna Clarke"],"cover_edition_key":"OL28300471M","cover_i":10226290,
  "first_publish_year":2020,"isbn":["1635575648","9781526622440","9781635575637"],
  "key":"/works/OL20893680W","number_of_pages_median":272,"title":"Piranesi"},
 {"key":"/works/OL1W","title":"  "},
 {"author_name":["A. Writer","B. Writer"],"key":"/works/OL2W","title":"Old  Book",
  "subtitle":"A Story","first_publish_year":-5,"isbn":["0306406152"]}
]}`

// fakeOpenLibrary stands in for both openlibrary.org and
// covers.openlibrary.org. "broken" fails, "slow" answers late, anything
// else unknown finds nothing.
func fakeOpenLibrary(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /search.json", func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Query().Get("q") {
		case "broken":
			http.Error(w, "down", http.StatusServiceUnavailable)
		case "garbled":
			_, _ = w.Write([]byte(`{"docs":[`))
		case "slow":
			time.Sleep(300 * time.Millisecond)
			_, _ = w.Write([]byte(`{"docs":[]}`))
		case "piranesi":
			_, _ = w.Write([]byte(searchJSON))
		default:
			_, _ = w.Write([]byte(`{"docs":[]}`))
		}
	})
	mux.HandleFunc("GET /works/OL20893680W.json", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"title":"Piranesi","description":"Piranesi's house is no ordinary building.\r\nIts rooms are infinite."}`))
	})
	mux.HandleFunc("GET /works/OL27448W.json", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"title":"The Lord of the Rings","description":{"type":"/type/text","value":"An epic."}}`))
	})
	mux.HandleFunc("GET /works/OL3W.json", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"description":"**From the *New York Times* bestselling author.**\r\n\r\nFor fans of *Circe*.  \r\n\r\n----------\r\nAlso contained in:\r\n[The Collection](/works/OL4W)\r\n\r\n([source][1])\r\n\r\n  [1]: https://example.com/piranesi"}`))
	})
	png := func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg") // a lie the sniffing must see through
		_, _ = w.Write(onePNG)
	}
	mux.HandleFunc("GET /b/id/10226290-S.jpg", png)
	mux.HandleFunc("GET /b/id/10226290-M.jpg", png)
	mux.HandleFunc("GET /b/id/666-M.jpg", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write([]byte("<html><body>not an image</body></html>"))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

// testWebClient is a webfetch client allowed to reach httptest's loopback.
func testWebClient() *webfetch.Client {
	c := webfetch.New(webfetch.Config{UserAgent: "test"})
	c.DenyAddr = func(string) error { return nil }
	return c
}

func newOpenLibrary(t *testing.T) *books.OpenLibrary {
	t.Helper()
	srv := fakeOpenLibrary(t)
	return &books.OpenLibrary{Web: testWebClient(), Base: srv.URL, Covers: srv.URL, Timeout: 5 * time.Second}
}

func TestSearchMapsResults(t *testing.T) {
	ol := newOpenLibrary(t)
	got, err := ol.Search(context.Background(), " piranesi ")
	if err != nil {
		t.Fatal(err)
	}
	want := []books.Candidate{
		{WorkID: "OL20893680W", EditionID: "OL28300471M", Title: "Piranesi", Authors: "Susanna Clarke",
			Year: 2020, Pages: 272, ISBN: "9781526622440", CoverID: 10226290},
		{WorkID: "OL2W", Title: "Old Book", Subtitle: "A Story", Authors: "A. Writer, B. Writer",
			ISBN: "9780306406157"},
	}
	if len(got) != len(want) {
		t.Fatalf("Search = %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("result %d = %+v\nwant %+v", i, got[i], want[i])
		}
	}
}

func TestSearchAsksForTheRightThings(t *testing.T) {
	var gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		_, _ = w.Write([]byte(`{"docs":[]}`))
	}))
	defer srv.Close()
	ol := &books.OpenLibrary{Web: testWebClient(), Base: srv.URL, Covers: srv.URL, Timeout: time.Second}
	if _, err := ol.Search(context.Background(), "le guin"); err != nil {
		t.Fatal(err)
	}
	for _, part := range []string{"q=le+guin", "limit=10",
		"fields=key%2Ctitle%2Csubtitle%2Cauthor_name%2Cfirst_publish_year%2Cnumber_of_pages_median%2Cisbn%2Ccover_i%2Ccover_edition_key"} {
		if !strings.Contains(gotQuery, part) {
			t.Errorf("query %q lacks %q", gotQuery, part)
		}
	}
}

func TestSearchWithNothingTypedAsksNothing(t *testing.T) {
	ol := &books.OpenLibrary{Web: testWebClient(), Base: "http://127.0.0.1:1", Timeout: time.Second}
	if got, err := ol.Search(context.Background(), "   "); err != nil || got != nil {
		t.Errorf("Search(blank) = %v, %v; want nothing and no error", got, err)
	}
}

func TestSearchFailures(t *testing.T) {
	ol := newOpenLibrary(t)
	ol.Timeout = 100 * time.Millisecond
	for _, q := range []string{"broken", "garbled", "slow"} {
		if _, err := ol.Search(context.Background(), q); err == nil {
			t.Errorf("Search(%q) succeeded, want an error", q)
		}
	}
	if got, err := ol.Search(context.Background(), "nothing like it"); err != nil || len(got) != 0 {
		t.Errorf("Search with no hits = %v, %v; want none, no error", got, err)
	}
}

func TestDescription(t *testing.T) {
	ol := newOpenLibrary(t)
	ctx := context.Background()
	if got, err := ol.Description(ctx, "OL20893680W"); err != nil || got != "Piranesi's house is no ordinary building.\nIts rooms are infinite." {
		t.Errorf("string description = %q, %v", got, err)
	}
	if got, err := ol.Description(ctx, "OL27448W"); err != nil || got != "An epic." {
		t.Errorf("object description = %q, %v", got, err)
	}
	want := "From the New York Times bestselling author.\n\nFor fans of Circe.\n\nAlso contained in:\nThe Collection\n\n(source)"
	if got, err := ol.Description(ctx, "OL3W"); err != nil || got != want {
		t.Errorf("Markdown description = %q, %v\nwant %q", got, err, want)
	}
	if _, err := ol.Description(ctx, "OL404W"); err == nil {
		t.Error("missing work: want an error")
	}
	if _, err := ol.Description(ctx, "../../etc"); !errors.Is(err, books.ErrInvalid) {
		t.Errorf("bad work id = %v, want ErrInvalid", err)
	}
}

func TestCover(t *testing.T) {
	ol := newOpenLibrary(t)
	ctx := context.Background()
	ct, data, err := ol.Cover(ctx, 10226290, "M")
	if err != nil || ct != "image/png" || len(data) != len(onePNG) {
		t.Errorf("Cover = %q, %d bytes, %v; want the PNG, sniffed", ct, len(data), err)
	}
	if _, _, err := ol.Cover(ctx, 666, "M"); err == nil {
		t.Error("HTML posing as a cover: want an error")
	}
	if _, _, err := ol.Cover(ctx, 0, "M"); !errors.Is(err, books.ErrInvalid) {
		t.Errorf("cover id 0 = %v, want ErrInvalid", err)
	}
	if _, _, err := ol.Cover(ctx, 10226290, "XL"); !errors.Is(err, books.ErrInvalid) {
		t.Errorf("size XL = %v, want ErrInvalid", err)
	}
}

func TestTheRealGuardStillApplies(t *testing.T) {
	srv := fakeOpenLibrary(t)
	ol := &books.OpenLibrary{Web: webfetch.New(webfetch.Config{UserAgent: "test"}), Base: srv.URL, Covers: srv.URL, Timeout: time.Second}
	if _, err := ol.Search(context.Background(), "piranesi"); !errors.Is(err, webfetch.ErrBlockedAddress) {
		t.Errorf("Search against loopback with the real guard = %v, want ErrBlockedAddress", err)
	}
}
