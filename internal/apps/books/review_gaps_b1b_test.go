package books_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/iliafrenkel/on-suite/internal/apps/books"
	"github.com/iliafrenkel/on-suite/internal/apptest"
	"github.com/iliafrenkel/on-suite/internal/htmlassert"
)

func TestOLIDBoundaries(t *testing.T) {
	tests := []struct {
		name string
		in   string
		kind byte
		want string
	}{
		{"work, 1 digit", "OL1W", 'W', "OL1W"},
		{"work, 12 digits", "OL123456789012W", 'W', "OL123456789012W"},
		{"work, 13 digits", "OL1234567890123W", 'W', ""},
		{"edition, 12 digits", "OL123456789012M", 'M', "OL123456789012M"},
		{"edition, 13 digits", "OL1234567890123M", 'M', ""},
		{"leading zero", "OL0123W", 'W', ""},
		{"zero", "OL0W", 'W', ""},
		{"no digits", "OLW", 'W', ""},
		{"edition key asked for as a work", "OL28300471M", 'W', ""},
		{"work key asked for as an edition", "OL20893680W", 'M', ""},
		{"lower case", "ol1w", 'W', ""},
		{"a path", "../../etc", 'W', ""},
		{"trailing junk", "OL1W/x", 'W', ""},
		{"empty", "", 'W', ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := books.OLIDForTest(tt.in, tt.kind); got != tt.want {
				t.Errorf("olID(%q, %q) = %q, want %q", tt.in, tt.kind, got, tt.want)
			}
		})
	}
}

func TestCoverIDTextBoundaries(t *testing.T) {
	tests := []struct{ in, want string }{
		{"1", "1"},
		{"10226290", "10226290"},
		{"123456789012", "123456789012"}, // 12 digits
		{"1234567890123", ""},            // 13 digits
		{" 42 ", "42"},
		{"0", ""},
		{"000", ""},
		{"", ""},
		{"-5", ""},
		{"12a", ""},
		{"1e5", ""},
		{"1.5", ""},
	}
	for _, tt := range tests {
		if got := books.CoverIDTextForTest(tt.in); got != tt.want {
			t.Errorf("coverIDText(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

// olServer is a one-off Open Library for a test; mux paths decide what it
// answers.
func olServer(t *testing.T, routes map[string]http.HandlerFunc) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	for pattern, h := range routes {
		mux.HandleFunc(pattern, h)
	}
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func clientFor(srv *httptest.Server) *books.OpenLibrary {
	return &books.OpenLibrary{Web: testWebClient(), Base: srv.URL, Covers: srv.URL, Timeout: 5 * time.Second}
}

func TestDescriptionIsCutAtTheLimit(t *testing.T) {
	long := strings.Repeat("é", books.MaxDescriptionRunes+50)
	exact := strings.Repeat("é", books.MaxDescriptionRunes)
	srv := olServer(t, map[string]http.HandlerFunc{
		"GET /works/OL1W.json": func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprintf(w, `{"description":%q}`, long)
		},
		"GET /works/OL2W.json": func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprintf(w, `{"description":{"type":"/type/text","value":%q}}`, exact)
		},
	})
	ol := clientFor(srv)
	got, err := ol.Description(context.Background(), "OL1W")
	if err != nil || got != exact {
		t.Errorf("long description: %d runes, %v; want it cut to %d", len([]rune(got)), err, books.MaxDescriptionRunes)
	}
	got, err = ol.Description(context.Background(), "OL2W")
	if err != nil || got != exact {
		t.Errorf("description of exactly the limit: %d runes, %v; want it whole", len([]rune(got)), err)
	}
	// What the client returns must pass the form's own check.
	if errs := (books.BookInput{Title: "x", Description: got}).Normalize().Validate(); errs != nil {
		t.Errorf("a fetched description fails Validate: %v", errs)
	}
}

func TestDescriptionFailures(t *testing.T) {
	srv := olServer(t, map[string]http.HandlerFunc{
		"GET /works/OL500W.json": func(w http.ResponseWriter, r *http.Request) { http.Error(w, "down", http.StatusInternalServerError) },
		"GET /works/OL503W.json": func(w http.ResponseWriter, r *http.Request) { http.Error(w, "down", http.StatusServiceUnavailable) },
		"GET /works/OL9W.json":   func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(`{"description":`)) },
		"GET /works/OL8W.json":   func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(`<html>nope</html>`)) },
		"GET /works/OL7W.json":   func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(`{"title":"No description"}`)) },
	})
	ol := clientFor(srv)
	for _, id := range []string{"OL500W", "OL503W", "OL404W", "OL9W", "OL8W"} {
		if got, err := ol.Description(context.Background(), id); err == nil || got != "" {
			t.Errorf("Description(%s) = %q, %v; want an error and no text", id, got, err)
		}
	}
	if got, err := ol.Description(context.Background(), "OL7W"); err != nil || got != "" {
		t.Errorf("work with no description = %q, %v; want empty, no error", got, err)
	}
}

func TestSearchCleansNonPositiveNumbers(t *testing.T) {
	srv := olServer(t, map[string]http.HandlerFunc{
		"GET /search.json": func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{"docs":[
			 {"key":"/works/OL1W","title":"Negative cover","cover_i":-7,"number_of_pages_median":-3,"first_publish_year":2001},
			 {"key":"/works/OL2W","title":"Zeros","cover_i":0,"number_of_pages_median":0,"first_publish_year":0},
			 {"key":"/works/OL3W","title":"Too many pages","number_of_pages_median":100001,"first_publish_year":10000},
			 {"key":"/works/OL4W","title":"At the limits","cover_i":1,"number_of_pages_median":100000,"first_publish_year":9999}]}`))
		},
	})
	got, err := clientFor(srv).Search(context.Background(), "x")
	if err != nil || len(got) != 4 {
		t.Fatalf("Search = %+v, %v; want 4 results", got, err)
	}
	want := []struct {
		title       string
		cover       int64
		pages, year int
	}{
		{"Negative cover", 0, 0, 2001},
		{"Zeros", 0, 0, 0},
		{"Too many pages", 0, 0, 0},
		{"At the limits", 1, 100000, 9999},
	}
	for i, w := range want {
		c := got[i]
		if c.Title != w.title || c.CoverID != w.cover || c.Pages != w.pages || c.Year != w.year {
			t.Errorf("%s: cover %d, pages %d, year %d; want %d, %d, %d", w.title, c.CoverID, c.Pages, c.Year, w.cover, w.pages, w.year)
		}
	}
}

// A cover fetch gives up at the client's own bound (coverTimeout), with no
// help from the caller's context.
func TestCoverFetchGivesUpAtItsOwnTimeout(t *testing.T) {
	books.SetCoverTimeoutForTest(t, 150*time.Millisecond)
	release := make(chan struct{})
	srv := olServer(t, map[string]http.HandlerFunc{
		"GET /b/id/1-M.jpg": func(w http.ResponseWriter, r *http.Request) {
			select {
			case <-release:
			case <-r.Context().Done():
			}
		},
	})
	defer close(release)
	start := time.Now()
	if _, _, err := clientFor(srv).Cover(context.Background(), 1, "M"); err == nil {
		t.Fatal("Cover from a hanging server succeeded, want an error")
	}
	if d := time.Since(start); d > 3*time.Second {
		t.Errorf("Cover took %v to give up, want about the client's 150ms bound", d)
	}
}

// olTestServer is the app with an Open Library of the test's making.
func olTestServer(t *testing.T, routes map[string]http.HandlerFunc) *server {
	t.Helper()
	a := books.New()
	s := apptest.NewServer(t, a, books.NewStore)
	a.UseOpenLibraryForTest(olServer(t, routes).URL)
	return s
}

func TestAFailedDescriptionFetchLeavesItEmpty(t *testing.T) {
	s := olTestServer(t, map[string]http.HandlerFunc{
		"GET /works/OL500W.json": func(w http.ResponseWriter, r *http.Request) { http.Error(w, "down", http.StatusInternalServerError) },
		"GET /works/OL9W.json":   func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(`{`)) },
	})
	for _, work := range []string{"OL500W", "OL9W", "OL404W"} {
		q := url.Values{"pick": {"1"}, "title": {"Piranesi"}, "authors": {"Susanna Clarke"}, "ol_work": {work}}
		doc := s.Get(t, s.Alice, "/books/new?"+q.Encode()) // still a 200
		if got := htmlassert.Text(doc.MustHave("textarea#books-description")); got != "" {
			t.Errorf("%s: description = %q, want empty", work, got)
		}
		if got := attr(t, doc, "input#books-title", "value"); got != "Piranesi" {
			t.Errorf("%s: title = %q; the rest of the pick must still fill the form", work, got)
		}
		if got := attr(t, doc, `input[name="ol_work"]`, "value"); got != work {
			t.Errorf("%s: ol_work = %q, want it kept", work, got)
		}
	}
}

func TestThumbnailHeaders(t *testing.T) {
	s, _ := newServerWithOL(t)
	rec := get(t, s, s.Alice, "/books/olcover/10226290")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET thumbnail = %d", rec.Code)
	}
	if got := rec.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Errorf("X-Content-Type-Options = %q, want nosniff", got)
	}
	if got, want := rec.Header().Get("Content-Length"), fmt.Sprint(len(onePNG)); got != want {
		t.Errorf("Content-Length = %q, want %s", got, want)
	}
}

func TestAThumbnailThatFailsUpstreamIsNotFound(t *testing.T) {
	s := olTestServer(t, map[string]http.HandlerFunc{
		"GET /b/id/500-S.jpg": func(w http.ResponseWriter, r *http.Request) { http.Error(w, "down", http.StatusInternalServerError) },
		"GET /b/id/503-S.jpg": func(w http.ResponseWriter, r *http.Request) { http.Error(w, "down", http.StatusServiceUnavailable) },
		"GET /b/id/666-S.jpg": func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "image/jpeg")
			_, _ = w.Write([]byte("<html>not an image</html>"))
		},
		"GET /b/id/7-S.jpg": func(w http.ResponseWriter, r *http.Request) { // a real image, but not a cover type
			w.Header().Set("Content-Type", "image/svg+xml")
			_, _ = w.Write([]byte(`<svg xmlns="http://www.w3.org/2000/svg"/>`))
		},
	})
	for _, id := range []string{"500", "503", "666", "7", "404"} {
		rec := get(t, s, s.Alice, "/books/olcover/"+id)
		if rec.Code != http.StatusNotFound {
			t.Errorf("thumbnail %s = %d, want 404", id, rec.Code)
		}
		if rec.Header().Get("Cache-Control") == "private, max-age=86400" {
			t.Errorf("thumbnail %s: a failure was marked cacheable", id)
		}
	}
}

// Eight thumbnails asked for at once reach Open Library four at a time.
func TestThumbnailsAreFetchedFourAtATime(t *testing.T) {
	const requests = 8
	var inFlight, peak atomic.Int32
	arrived := make(chan struct{}, requests)
	release := make(chan struct{})
	var once sync.Once
	open := func() { once.Do(func() { close(release) }) }
	defer open() // never leave handlers blocked, whichever way the test ends
	s := olTestServer(t, map[string]http.HandlerFunc{
		"GET /b/id/{id}": func(w http.ResponseWriter, r *http.Request) {
			n := inFlight.Add(1)
			for {
				p := peak.Load()
				if n <= p || peak.CompareAndSwap(p, n) {
					break
				}
			}
			arrived <- struct{}{}
			select {
			case <-release:
			case <-r.Context().Done():
			}
			inFlight.Add(-1)
			_, _ = w.Write(onePNG)
		},
	})
	var wg sync.WaitGroup
	codes := make(chan int, requests)
	for i := 0; i < requests; i++ {
		wg.Add(1)
		go func() { // no t.Fatal-capable helpers off the test goroutine
			defer wg.Done()
			req := httptest.NewRequest("GET", fmt.Sprintf("/books/olcover/%d", i+1), nil)
			codes <- s.Do(t, s.Alice, req).Code
		}()
	}
	for i := 0; i < 4; i++ { // the first four get through
		select {
		case <-arrived:
		case <-time.After(5 * time.Second):
			t.Fatalf("only %d thumbnails reached Open Library, want 4", i)
		}
	}
	select { // and a fifth waits its turn
	case <-arrived:
		t.Error("a fifth thumbnail reached Open Library while four were in flight")
	case <-time.After(300 * time.Millisecond):
	}
	open()
	wg.Wait()
	close(codes)
	for code := range codes {
		if code != http.StatusOK {
			t.Errorf("a thumbnail answered %d, want 200", code)
		}
	}
	if got := peak.Load(); got != 4 {
		t.Errorf("peak concurrent upstream fetches = %d, want 4", got)
	}
}

// A picked book that bounces keeps its Open Library identity: the hidden
// fields, the cover note, and on the resubmit the stored ids and source.
func TestAPickedBookKeepsItsIdentityThroughA422(t *testing.T) {
	s, _ := newServerWithOL(t)
	form := url.Values{"title": {""}, "authors": {"Susanna Clarke"}, "add_to": {"want"},
		"ol_work": {"OL20893680W"}, "ol_edition": {"OL28300471M"}, "cover_id": {"10226290"}}
	rec := s.Post(t, s.Alice, "/books/new", form)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("POST without a title = %d, want 422", rec.Code)
	}
	doc := htmlassert.Parse(t, rec.Body.String())
	doc.MustHave("#books-title-error")
	for name, want := range map[string]string{"ol_work": "OL20893680W", "ol_edition": "OL28300471M", "cover_id": "10226290"} {
		if got := attr(t, doc, `input[name="`+name+`"]`, "value"); got != want {
			t.Errorf("hidden %s = %q after the 422, want %q", name, got, want)
		}
	}
	if got := attr(t, doc, ".books-picked img", "src"); got != "/books/olcover/10226290" {
		t.Errorf("picked cover = %q after the 422", got)
	}

	form.Set("title", "Piranesi")
	s.Submit(t, s.Alice, "/books/new", form, "/books/b/1?shelf=want")
	var work, edition string
	if err := s.Store.DBForTest().QueryRow(`SELECT ol_work_id, ol_edition_id FROM books_books WHERE id = 1`).Scan(&work, &edition); err != nil {
		t.Fatal(err)
	}
	if work != "OL20893680W" || edition != "OL28300471M" {
		t.Errorf("stored ol ids = %q, %q; want the picked ones", work, edition)
	}
	if got := coverSource(t, s, 1); got != books.CoverFromOL {
		t.Errorf("cover source = %q, want %q", got, books.CoverFromOL)
	}
}

func TestEditFormBodyOverTheLimitIsRejected(t *testing.T) {
	s := newServer(t)
	id := add(t, s, s.Alice.User.ID, titled("Piranesi", "", books.ShelfWant))
	path := fmt.Sprintf("/books/edit/%d", id)

	// Just inside the budget is fine: a long description in a small form.
	ok := details("Piranesi, edited")
	ok.Set("description", strings.Repeat("a", 5000))
	if rec := postMultipart(t, s, s.Alice, path, ok, "", nil); rec.Code != http.StatusSeeOther {
		t.Fatalf("an ordinary edit = %d, want 303", rec.Code)
	}

	big := details("Too big")
	big.Set("description", strings.Repeat("a", books.EditFormMaxBytes+1))
	rec := postMultipart(t, s, s.Alice, path, big, "", nil)
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("body over editFormMaxBytes = %d, want 413", rec.Code)
	}
	b, _ := s.Store.Get(context.Background(), s.Alice.User.ID, id)
	if b.Title != "Piranesi, edited" {
		t.Errorf("title = %q after the oversize post, want the earlier edit untouched", b.Title)
	}
}

func TestHTMLAtTheCoverAddressSaysSoAndKeepsTheAddress(t *testing.T) {
	s, ol := newServerWithOL(t)
	id := add(t, s, s.Alice.User.ID, titled("Piranesi", "", books.ShelfWant))
	address := ol + "/b/id/666-M.jpg" // HTML, not an image
	form := details("Piranesi, edited")
	form.Set("cover_url", address)
	rec := postMultipart(t, s, s.Alice, fmt.Sprintf("/books/edit/%d", id), form, "", nil)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("HTML address = %d, want 422", rec.Code)
	}
	doc := htmlassert.Parse(t, rec.Body.String())
	if got := htmlassert.Text(doc.MustHave("#books-cover-error")); !strings.Contains(got, "JPEG, PNG, GIF or WebP") {
		t.Errorf("cover message = %q, want it to name the image types", got)
	}
	if got := attr(t, doc, `input[name="cover_url"]`, "value"); got != address {
		t.Errorf("cover_url echoed as %q, want %q", got, address)
	}
	if got := attr(t, doc, `input[name="title"]`, "value"); got != "Piranesi, edited" {
		t.Errorf("title echoed as %q, want what was typed", got)
	}
	if b, _ := s.Store.Get(context.Background(), s.Alice.User.ID, id); b.Title != "Piranesi" || b.CoverVersion != "" {
		t.Errorf("after the refused edit: title %q, cover %q; want nothing saved", b.Title, b.CoverVersion)
	}
}

func TestTheHTMXPanesShowTheCoverOnlyToItsOwner(t *testing.T) {
	s := newServer(t)
	uid := s.Alice.User.ID
	id := add(t, s, uid, titled("Piranesi", "", books.ShelfWant))
	if err := s.Store.SetCover(context.Background(), uid, id, "image/png", onePNG, books.CoverUpload); err != nil {
		t.Fatal(err)
	}
	c, _ := s.Store.Cover(context.Background(), uid, id)
	want := fmt.Sprintf("/books/cover/%d?v=%s", id, c.Version)
	path := fmt.Sprintf("/books/b/%d?shelf=want", id)

	for _, target := range []string{"books-book", "books-panes"} {
		doc := htmlassert.Parse(t, hx(t, s, path, target))
		if got := attr(t, doc, "img.books-cover", "src"); got != want {
			t.Errorf("target %s: cover src = %q, want %q", target, got, want)
		}
	}
	// The list partial draws the mini cover too.
	list := htmlassert.Parse(t, hx(t, s, "/books/?shelf=want", "books-list"))
	if got := attr(t, list, "img.books-mini-cover", "src"); got != want {
		t.Errorf("list mini cover = %q, want %q", got, want)
	}

	req := httptest.NewRequest("GET", path, nil)
	req.Header.Set("HX-Request", "true")
	req.Header.Set("HX-Target", "books-book")
	rec := s.Do(t, s.Bob, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("Bob's htmx GET = %d, want 404", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "/books/cover/") || strings.Contains(rec.Body.String(), "Piranesi") {
		t.Error("Bob's response mentions Alice's book or its cover")
	}
	bobList := htmlassert.Parse(t, hx2(t, s, s.Bob, "/books/?shelf=want", "books-list"))
	bobList.MustNotHave("img.books-mini-cover")
}

// hx2 is hx for another user's session.
func hx2(t *testing.T, s *server, sess *apptest.Session, path, target string) string {
	t.Helper()
	req := httptest.NewRequest("GET", path, nil)
	req.Header.Set("HX-Request", "true")
	req.Header.Set("HX-Target", target)
	rec := s.Do(t, sess, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("htmx GET %s = %d", path, rec.Code)
	}
	return rec.Body.String()
}
