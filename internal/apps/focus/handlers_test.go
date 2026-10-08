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

	// Each tile's ⋯ menu: Duplicate and Delete are forms with the CSRF field.
	for _, tile := range tiles {
		id, _ := htmlassert.Attr(tile, "data-id")
		for _, action := range []string{"duplicate", "delete"} {
			doc.MustHave(`.focus-menu form[action="/focus/timers/` + id + `/` + action + `"] input[name="csrf_token"]`)
		}
	}
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
	for _, action := range []string{"duplicate", "delete"} {
		if rec := s.Post(t, s.Alice, "/focus/timers/abc/"+action, url.Values{}); rec.Code != http.StatusNotFound {
			t.Errorf("non-numeric id on %s = %d, want 404", action, rec.Code)
		}
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
	ts, err := s.Store.Timers(context.Background(), uid)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(names(ts), ""); got != "AB" {
		t.Errorf("order after rejected requests = %s, want AB", got)
	}
}

// runConfig is the JSON the running page embeds for focus.js.
type runConfig struct {
	UserID      int64  `json:"userId"`
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

	for _, src := range []string{"/focus/session.js", "/focus/chimes.js", "/focus/record.js", "/focus/focus.js"} {
		doc.MustHave(`script[src="` + src + `"]`)
	}
	root := doc.MustHave("#focus-runner")
	if class, _ := htmlassert.Attr(root, "class"); !strings.Contains(class, "focus-runner") ||
		!strings.Contains(class, "swatch-c-blue") || strings.Contains(class, "focus-runner-single") {
		t.Errorf("runner class = %q", class)
	}

	cfg := runConfigOf(t, doc)
	if cfg.UserID != s.Alice.User.ID {
		t.Errorf("config userId = %d, want %d", cfg.UserID, s.Alice.User.ID)
	}
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

func TestIndexHasTheResumeBannerSlot(t *testing.T) {
	s := newServer(t)
	// Both with and without timers: a session can outlive its timer's tile.
	for _, seed := range []bool{false, true} {
		if seed {
			seedTimer(t, s, s.Alice.User.ID, single("Reading", 30))
		}
		doc := s.Get(t, s.Alice, "/focus/")
		doc.MustHave(`script[src="/focus/session.js"]`)
		doc.MustHave(`script[src="/focus/record.js"]`)
		banner := doc.MustHave("[data-focus-resume]")
		if _, ok := htmlassert.Attr(banner, "hidden"); !ok {
			t.Errorf("seed=%v: the banner should start hidden", seed)
		}
		if got, _ := htmlassert.Attr(banner, "data-user-id"); got != itoa(s.Alice.User.ID) {
			t.Errorf("seed=%v: banner data-user-id = %q, want %q", seed, got, itoa(s.Alice.User.ID))
		}
		doc.MustHave("[data-focus-resume] [data-focus-resume-text]")
		for _, sel := range []string{"[data-focus-resume] button[data-focus-resume-end]", "[data-focus-resume] button[data-focus-resume-retry]"} {
			if _, ok := htmlassert.Attr(doc.MustHave(sel), "hidden"); !ok {
				t.Errorf("seed=%v: %s should start hidden", seed, sel)
			}
		}
	}
}

func TestScriptsAreServed(t *testing.T) {
	s := newServer(t)
	for _, name := range []string{
		"home.js",
		"session.js",
		"chimes.js",
		"record.js",
		"focus.js",
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

func TestIndexLinksToHistory(t *testing.T) {
	s := newServer(t)
	doc := s.Get(t, s.Alice, "/focus/")
	doc.MustHave(`.focus-toolbar a[href="/focus/history"]`)
}

func TestTodayStripOnlyOnceThereIsHistory(t *testing.T) {
	s := newServer(t)
	s.Clock.Set(localAt(10, 7, 12, 0))
	uid := s.Alice.User.ID
	seedTimer(t, s, uid, single("Reading", 30))

	doc := s.Get(t, s.Alice, "/focus/")
	doc.MustHave("[data-focus-today]")
	doc.MustNotHave(".focus-today")

	// Only an old session: the strip shows, with nothing today.
	seedSession(t, s, uid, sessionInput("old", "Reading", localAt(9, 1, 9, 0), 30))
	doc = s.Get(t, s.Alice, "/focus/")
	if got := htmlassert.Text(doc.MustHave(".focus-today")); got != "Today 0m focused 0 sessions This week 0m" {
		t.Errorf("strip = %q", got)
	}

	seedSession(t, s, uid, sessionInput("a", "Reading", localAt(10, 7, 8, 0), 30))
	seedSession(t, s, uid, sessionInput("b", "Reading", localAt(10, 6, 8, 0), 45))
	seedSession(t, s, s.Bob.User.ID, sessionInput("z", "Bob's", localAt(10, 7, 9, 0), 60))
	doc = s.Get(t, s.Alice, "/focus/")
	if got := htmlassert.Text(doc.MustHave(".focus-today")); got != "Today 30m focused 1 session This week 1h 15m" {
		t.Errorf("strip = %q", got)
	}
}

func TestTodayFragment(t *testing.T) {
	s := newServer(t)
	s.Clock.Set(localAt(10, 7, 12, 0))
	rec := s.Do(t, s.Alice, httptestGet("/focus/today"))
	if rec.Code != http.StatusOK || strings.TrimSpace(rec.Body.String()) != "" {
		t.Errorf("no history: %d %q, want 200 and an empty body", rec.Code, rec.Body.String())
	}
	seedSession(t, s, s.Alice.User.ID, sessionInput("a", "Reading", localAt(10, 7, 8, 0), 30))
	rec = s.Do(t, s.Alice, httptestGet("/focus/today"))
	doc := htmlassert.Parse(t, rec.Body.String())
	if got := htmlassert.Text(doc.MustHave(".focus-today")); got != "Today 30m focused 1 session This week 30m" {
		t.Errorf("fragment = %q", got)
	}
	if strings.Contains(rec.Body.String(), "<html") {
		t.Error("the fragment must not be a whole page")
	}
}

func TestRunPageDoneScreenCanRetry(t *testing.T) {
	s := newServer(t)
	tm := seedTimer(t, s, s.Alice.User.ID, single("Reading", 30))
	doc := s.Get(t, s.Alice, "/focus/run/"+itoa(tm.ID))
	status := doc.MustHave("[data-focus-done] [data-focus-done-status]")
	if role, _ := htmlassert.Attr(status, "role"); role != "status" {
		t.Errorf("done status role = %q, want status", role)
	}
	retry := doc.MustHave("[data-focus-done] button[data-focus-retry]")
	if _, ok := htmlassert.Attr(retry, "hidden"); !ok {
		t.Error("Retry should start hidden")
	}
}

// The running page embeds the timer as JSON inside <script>; a name that
// looks like markup must not end the script early or break the JSON.
func TestRunPageConfigEscapesTheName(t *testing.T) {
	s := newServer(t)
	name := `</script>"&<b>`
	tm := seedTimer(t, s, s.Alice.User.ID, single(name, 15))
	rec := s.Do(t, s.Alice, httptestGet("/focus/run/"+itoa(tm.ID)))
	if strings.Contains(rec.Body.String(), `</script>"&<b>`) {
		t.Error("the raw name appears unescaped in the page")
	}
	cfg := runConfigOf(t, htmlassert.Parse(t, rec.Body.String()))
	if cfg.Name != name {
		t.Errorf("config name = %q, want %q", cfg.Name, name)
	}
}

// The scripts must load in dependency order: each uses what the one
// before it defines.
func TestScriptOrder(t *testing.T) {
	s := newServer(t)
	tm := seedTimer(t, s, s.Alice.User.ID, single("Reading", 30))
	for path, want := range map[string]string{
		"/focus/run/" + itoa(tm.ID): "/focus/session.js /focus/chimes.js /focus/record.js /focus/focus.js",
		"/focus/":                   "/focus/session.js /focus/record.js /focus/home.js",
	} {
		var got []string
		for _, n := range s.Get(t, s.Alice, path).QueryAll("script[src]") {
			if src, _ := htmlassert.Attr(n, "src"); strings.HasPrefix(src, "/focus/") {
				got = append(got, src)
			}
		}
		if strings.Join(got, " ") != want {
			t.Errorf("%s scripts = %v, want %s", path, got, want)
		}
	}
}

func TestMoveTimerFromTheMenu(t *testing.T) {
	s := newServer(t)
	uid := s.Alice.User.ID
	seedTimer(t, s, uid, single("A", 5))
	b := seedTimer(t, s, uid, single("B", 5))
	c := seedTimer(t, s, uid, single("C", 5))
	order := func() string {
		t.Helper()
		ts, err := s.Store.Timers(context.Background(), uid)
		if err != nil {
			t.Fatal(err)
		}
		return strings.Join(names(ts), "")
	}

	s.Submit(t, s.Alice, "/focus/timers/"+itoa(b.ID)+"/move", url.Values{"direction": {"earlier"}}, "/focus/")
	if got := order(); got != "BAC" {
		t.Errorf("B earlier: %s, want BAC", got)
	}
	s.Submit(t, s.Alice, "/focus/timers/"+itoa(c.ID)+"/move", url.Values{"direction": {"later"}}, "/focus/")
	if got := order(); got != "BAC" {
		t.Errorf("last moved later: %s, want BAC unchanged", got)
	}

	if rec := s.Post(t, s.Alice, "/focus/timers/"+itoa(b.ID)+"/move", url.Values{"direction": {"sideways"}}); rec.Code != http.StatusBadRequest {
		t.Errorf("unknown direction = %d, want 400", rec.Code)
	}
	if rec := s.Post(t, s.Bob, "/focus/timers/"+itoa(b.ID)+"/move", url.Values{"direction": {"later"}}); rec.Code != http.StatusNotFound {
		t.Errorf("bob = %d, want 404", rec.Code)
	}
	if rec := s.Post(t, s.Alice, "/focus/timers/abc/move", url.Values{"direction": {"later"}}); rec.Code != http.StatusNotFound {
		t.Errorf("non-numeric id = %d, want 404", rec.Code)
	}
}

// moveDirections is the Move items a tile's ⋯ menu offers, in order.
func moveDirections(doc *htmlassert.Doc, id int64) []string {
	var out []string
	for _, n := range doc.QueryAll(`.focus-menu form[action="/focus/timers/` + itoa(id) + `/move"] input[name="direction"]`) {
		v, _ := htmlassert.Attr(n, "value")
		out = append(out, v)
	}
	return out
}

func TestTileMenusOfferMovesExceptAtTheEnds(t *testing.T) {
	s := newServer(t)
	uid := s.Alice.User.ID
	a, b, c := seedTimer(t, s, uid, single("A", 5)), seedTimer(t, s, uid, single("B", 5)), seedTimer(t, s, uid, single("C", 5))
	doc := s.Get(t, s.Alice, "/focus/")
	for _, tc := range []struct {
		id   int64
		want string
	}{{a.ID, "later"}, {b.ID, "earlier later"}, {c.ID, "earlier"}} {
		if got := strings.Join(moveDirections(doc, tc.id), " "); got != tc.want {
			t.Errorf("timer %d moves = %q, want %q", tc.id, got, tc.want)
		}
	}
	doc.MustHave(`.focus-menu form[action="/focus/timers/` + itoa(b.ID) + `/move"] input[name="csrf_token"]`)

	// A lone timer has nowhere to move.
	s2 := newServer(t)
	only := seedTimer(t, s2, s2.Alice.User.ID, single("Only", 5))
	if got := moveDirections(s2.Get(t, s2.Alice, "/focus/"), only.ID); len(got) != 0 {
		t.Errorf("a lone timer offers %v, want no moves", got)
	}
}

func TestHomeAdvertisesItsShortcuts(t *testing.T) {
	s := newServer(t)
	uid := s.Alice.User.ID
	for i := 1; i <= 10; i++ {
		seedTimer(t, s, uid, single("T"+strconv.Itoa(i), 5))
	}
	doc := s.Get(t, s.Alice, "/focus/")

	// Check for the home page indicator - use findEl since htmlassert doesn't support
	// combined class+attribute selectors
	findEl(t, doc, ".focus-page", map[string]string{"data-focus-home": ""})
	findEl(t, doc, `.focus-toolbar a[href="/focus/new"]`, map[string]string{"aria-keyshortcuts": "N"})

	plays := doc.QueryAll(".focus-play")
	if len(plays) != 10 {
		t.Fatalf("%d ▶ buttons, want 10", len(plays))
	}
	for i, p := range plays[:9] {
		key := strconv.Itoa(i + 1)
		if got, _ := htmlassert.Attr(p, "aria-keyshortcuts"); got != key {
			t.Errorf("▶ %d aria-keyshortcuts = %q, want %q", i+1, got, key)
		}
		if got, _ := htmlassert.Attr(p, "title"); got != "Start ("+key+")" {
			t.Errorf("▶ %d title = %q, want %q", i+1, got, "Start ("+key+")")
		}
	}
	if _, ok := htmlassert.Attr(plays[9], "aria-keyshortcuts"); ok {
		t.Error("the 10th ▶ has a shortcut; only 1–9 exist")
	}

	// History loads home.js too, but has no shortcuts.
	s.Get(t, s.Alice, "/focus/history").MustNotHave("[data-focus-home]")
}
