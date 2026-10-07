package focus_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/iliafrenkel/on-suite/internal/apps/focus"
	"github.com/iliafrenkel/on-suite/internal/apptest"
	"github.com/iliafrenkel/on-suite/internal/htmlassert"
)

type server = apptest.Server[*focus.Store]

// newServer mounts ON Focus with its own store as the test's handle.
func newServer(t *testing.T) *server {
	t.Helper()
	return apptest.NewServer(t, focus.New(), focus.NewStore)
}

func TestFocusRequiresSignIn(t *testing.T) {
	s := newServer(t)
	rec := s.Do(t, nil, httptest.NewRequest("GET", "/focus/", nil))
	if rec.Code != http.StatusSeeOther {
		t.Errorf("GET /focus/ anonymous = %d, want a 303 to the login page", rec.Code)
	}
}

func TestIndexShowsTheEmptyState(t *testing.T) {
	s := newServer(t)
	doc := s.Get(t, s.Alice, "/focus/")
	doc.MustHave(".focus-page")
	doc.MustHave(".focus-empty")
	doc.MustHave(`a[href="/focus/new"]`)
}

func seedTimer(t *testing.T, s *server, userID int64, in focus.TimerInput) focus.Timer {
	t.Helper()
	tm, err := s.Store.CreateTimer(context.Background(), userID, in)
	if err != nil {
		t.Fatal(err)
	}
	return tm
}

func single(name string, minutes int) focus.TimerInput {
	in := focus.DefaultInput()
	in.Name, in.FocusMinutes = name, minutes
	return in
}

func TestIndexShowsTilesInOrder(t *testing.T) {
	s := newServer(t)
	deep := validIntervals() // Deep work, blue, 50/10 × 4, long 30 every 2
	seedTimer(t, s, s.Alice.User.ID, single("Daily Reflection", 15))
	deepTimer := seedTimer(t, s, s.Alice.User.ID, deep)
	seedTimer(t, s, s.Bob.User.ID, single("Bob's timer", 5))

	doc := s.Get(t, s.Alice, "/focus/")
	doc.MustNotHave(".focus-empty")
	tiles := doc.QueryAll(".focus-tile")
	if len(tiles) != 2 {
		t.Fatalf("got %d tiles, want 2", len(tiles))
	}
	var gotNames []string
	for _, n := range doc.QueryAll(".focus-tile-name") {
		gotNames = append(gotNames, htmlassert.Text(n))
	}
	if strings.Join(gotNames, "|") != "Daily Reflection|Deep work" {
		t.Errorf("tile names = %v", gotNames)
	}
	if class, _ := htmlassert.Attr(tiles[1], "class"); !strings.Contains(class, "swatch-c-blue") {
		t.Errorf("Deep work tile class = %q, want swatch-c-blue", class)
	}
	summaries := doc.QueryAll(".focus-tile-summary")
	if got := htmlassert.Text(summaries[1]); got != "50 / 10 × 4 · long 30" {
		t.Errorf("summary = %q", got)
	}
	pills := doc.QueryAll(".focus-pill")
	if got := htmlassert.Text(pills[0]); got != "15 min" {
		t.Errorf("single pill = %q, want 15 min", got)
	}
	if got := htmlassert.Text(pills[1]); got != "4h 10m" { // 4×50 + 10 + 30 + 10
		t.Errorf("intervals pill = %q, want 4h 10m", got)
	}
	doc.MustHave(`a[href="/focus/run/` + itoa(deepTimer.ID) + `"]`)
}

func TestDuplicateFromTheHomePage(t *testing.T) {
	s := newServer(t)
	tm := seedTimer(t, s, s.Alice.User.ID, single("Reading", 30))
	s.Submit(t, s.Alice, "/focus/timers/"+itoa(tm.ID)+"/duplicate", url.Values{}, "/focus/")
	ts, err := s.Store.Timers(context.Background(), s.Alice.User.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(ts) != 2 || ts[1].Name != "Reading (copy)" {
		t.Errorf("after duplicate: %v", names(ts))
	}
}

func TestDeleteFromTheHomePage(t *testing.T) {
	s := newServer(t)
	tm := seedTimer(t, s, s.Alice.User.ID, single("Reading", 30))
	s.Submit(t, s.Alice, "/focus/timers/"+itoa(tm.ID)+"/delete", url.Values{}, "/focus/")
	ts, err := s.Store.Timers(context.Background(), s.Alice.User.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(ts) != 0 {
		t.Errorf("after delete: %v", names(ts))
	}
}

func TestDuplicateAndDeleteAreNotFoundForSomeoneElse(t *testing.T) {
	s := newServer(t)
	tm := seedTimer(t, s, s.Alice.User.ID, single("Mine", 30))
	for _, action := range []string{"duplicate", "delete"} {
		rec := s.Post(t, s.Bob, "/focus/timers/"+itoa(tm.ID)+"/"+action, url.Values{})
		if rec.Code != http.StatusNotFound {
			t.Errorf("bob %s = %d, want 404", action, rec.Code)
		}
	}
	if rec := s.Post(t, s.Alice, "/focus/timers/abc/delete", url.Values{}); rec.Code != http.StatusNotFound {
		t.Errorf("non-numeric id = %d, want 404", rec.Code)
	}
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }

func httptestGet(path string) *http.Request { return httptest.NewRequest("GET", path, nil) }

func TestReorderSavesTheNewOrder(t *testing.T) {
	s := newServer(t)
	uid := s.Alice.User.ID
	a, b, c := seedTimer(t, s, uid, single("A", 5)), seedTimer(t, s, uid, single("B", 5)), seedTimer(t, s, uid, single("C", 5))
	ids := itoa(c.ID) + "," + itoa(a.ID) + "," + itoa(b.ID)
	rec := s.PostHX(t, s.Alice, "/focus/timers/order", url.Values{"ids": {ids}})
	if rec.Code != http.StatusNoContent {
		t.Fatalf("POST order = %d, want 204; body %s", rec.Code, rec.Body.String())
	}
	ts, _ := s.Store.Timers(context.Background(), uid)
	if got := strings.Join(names(ts), ""); got != "CAB" {
		t.Errorf("order = %s, want CAB", got)
	}
}

func TestReorderRejectsBadIDs(t *testing.T) {
	s := newServer(t)
	uid := s.Alice.User.ID
	a, b := seedTimer(t, s, uid, single("A", 5)), seedTimer(t, s, uid, single("B", 5))
	theirs := seedTimer(t, s, s.Bob.User.ID, single("Bob", 5))
	for _, ids := range []string{"", "x,y", itoa(a.ID), itoa(a.ID) + "," + itoa(theirs.ID), itoa(a.ID) + "," + itoa(b.ID) + ",-1"} {
		rec := s.PostHX(t, s.Alice, "/focus/timers/order", url.Values{"ids": {ids}})
		if rec.Code != http.StatusBadRequest {
			t.Errorf("ids %q = %d, want 400", ids, rec.Code)
		}
	}
}

// runConfig is the JSON the running page embeds for focus.js.
type runConfig struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	Color       string `json:"color"`
	Chime       string `json:"chime"`
	AutoAdvance bool   `json:"autoAdvance"`
	KeepHistory bool   `json:"keepHistory"`
	Rounds      int    `json:"rounds"`
	Phases      []struct {
		Kind    string `json:"kind"`
		Seconds int    `json:"seconds"`
		Round   int    `json:"round"`
		Label   string `json:"label"`
	} `json:"phases"`
}

func runConfigOf(t *testing.T, doc *htmlassert.Doc) runConfig {
	t.Helper()
	node := doc.MustHave("script#focus-run-config")
	if typ, _ := htmlassert.Attr(node, "type"); typ != "application/json" {
		t.Fatalf("config script type = %q", typ)
	}
	var cfg runConfig
	if err := json.Unmarshal([]byte(htmlassert.Text(node)), &cfg); err != nil {
		t.Fatalf("config JSON: %v\n%s", err, htmlassert.Text(node))
	}
	return cfg
}

func TestRunPageForAnIntervalsTimer(t *testing.T) {
	s := newServer(t)
	tm := seedTimer(t, s, s.Alice.User.ID, validIntervals()) // Deep work, blue, bowl, 50/10 × 4, long 30 every 2
	doc := s.Get(t, s.Alice, "/focus/run/"+itoa(tm.ID))

	for _, src := range []string{"/focus/session.js", "/focus/chimes.js", "/focus/focus.js"} {
		doc.MustHave(`script[src="` + src + `"]`)
	}
	root := doc.MustHave("#focus-runner")
	if class, _ := htmlassert.Attr(root, "class"); !strings.Contains(class, "focus-runner") ||
		!strings.Contains(class, "swatch-c-blue") || strings.Contains(class, "focus-runner-single") {
		t.Errorf("runner class = %q", class)
	}

	cfg := runConfigOf(t, doc)
	if cfg.ID != tm.ID || cfg.Name != "Deep work" || cfg.Color != "blue" || cfg.Chime != "bowl" ||
		!cfg.AutoAdvance || !cfg.KeepHistory || cfg.Rounds != 4 || len(cfg.Phases) != 7 {
		t.Fatalf("config = %+v", cfg)
	}
	if p := cfg.Phases[0]; p.Kind != "focus" || p.Seconds != 3000 || p.Round != 1 || p.Label != "Focus · round 1 of 4" {
		t.Errorf("phase 0 = %+v", p)
	}
	if p := cfg.Phases[1]; p.Kind != "break" || p.Seconds != 600 || p.Round != 1 || p.Label != "Short break" {
		t.Errorf("phase 1 = %+v", p)
	}
	if p := cfg.Phases[3]; p.Kind != "long_break" || p.Seconds != 1800 || p.Round != 2 || p.Label != "Long break" {
		t.Errorf("phase 3 = %+v", p)
	}

	if got := htmlassert.Text(doc.MustHave("[data-focus-digits]")); got != "50:00" {
		t.Errorf("digits = %q, want 50:00", got)
	}
	if got := htmlassert.Text(doc.MustHave("[data-focus-phase]")); got != "Focus · round 1 of 4" {
		t.Errorf("phase label = %q", got)
	}
	if n := len(doc.QueryAll(".focus-dot")); n != 4 {
		t.Errorf("%d dots, want 4", n)
	}
	for _, sel := range []string{
		"[data-focus-pause]", "[data-focus-skip]", "[data-focus-restart]", "[data-focus-next]",
		"[data-focus-exit]", "[data-focus-fullscreen]", "[data-focus-sound-hint]", "[data-focus-done]",
		"circle[data-focus-bar]", "dialog#focus-run-dialog",
	} {
		doc.MustHave(sel)
	}
	// Hidden until the script shows them.
	for _, sel := range []string{"[data-focus-waiting]", "[data-focus-sound-hint]", "[data-focus-done]", "[data-focus-fullscreen]"} {
		if _, ok := htmlassert.Attr(doc.MustHave(sel), "hidden"); !ok {
			t.Errorf("%s should start hidden", sel)
		}
	}
}

func TestRunPageForASingleTimer(t *testing.T) {
	s := newServer(t)
	in := single("Daily Reflection", 15)
	in.Color, in.AutoAdvance, in.KeepHistory = "coral", false, false
	tm := seedTimer(t, s, s.Alice.User.ID, in)
	doc := s.Get(t, s.Alice, "/focus/run/"+itoa(tm.ID))

	if class, _ := htmlassert.Attr(doc.MustHave("#focus-runner"), "class"); !strings.Contains(class, "focus-runner-single") {
		t.Errorf("runner class = %q, want focus-runner-single", class)
	}
	cfg := runConfigOf(t, doc)
	if cfg.Rounds != 0 || len(cfg.Phases) != 1 || cfg.Phases[0].Seconds != 900 || cfg.AutoAdvance || cfg.KeepHistory {
		t.Errorf("config = %+v", cfg)
	}
	if got := htmlassert.Text(doc.MustHave("[data-focus-digits]")); got != "15:00" {
		t.Errorf("digits = %q, want 15:00", got)
	}
	// Spec: single timers have no phase label, no dots and no Skip.
	doc.MustNotHave("[data-focus-phase]")
	doc.MustNotHave(".focus-dot")
	doc.MustNotHave("[data-focus-skip]")
	doc.MustHave("[data-focus-pause]")
	doc.MustHave("[data-focus-restart]")
}

func TestRunPageIsNotFoundForSomeoneElse(t *testing.T) {
	s := newServer(t)
	tm := seedTimer(t, s, s.Alice.User.ID, single("Mine", 15))
	if rec := s.Do(t, s.Bob, httptestGet("/focus/run/"+itoa(tm.ID))); rec.Code != http.StatusNotFound {
		t.Errorf("bob run page = %d, want 404", rec.Code)
	}
}

func TestScriptsAreServed(t *testing.T) {
	s := newServer(t)
	for _, name := range []string{
		"home.js",
		"session.js",
		"chimes.js",
	} {
		rec := s.Do(t, s.Alice, httptestGet("/focus/"+name))
		if rec.Code != http.StatusOK {
			t.Errorf("GET /focus/%s = %d, want 200", name, rec.Code)
			continue
		}
		if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/javascript") {
			t.Errorf("GET /focus/%s Content-Type = %q", name, ct)
		}
	}
}
