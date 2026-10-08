package focus_test

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"golang.org/x/net/html"

	"github.com/iliafrenkel/on-suite/internal/apps/focus"
	"github.com/iliafrenkel/on-suite/internal/htmlassert"
)

// findEl returns the first element matched by selector whose attributes
// include every key/value in want ("" value = attribute merely present).
func findEl(t *testing.T, doc *htmlassert.Doc, selector string, want map[string]string) *html.Node {
	t.Helper()
	for _, n := range doc.QueryAll(selector) {
		ok := true
		for k, v := range want {
			got, present := htmlassert.Attr(n, k)
			if !present || (v != "" && got != v) {
				ok = false
				break
			}
		}
		if ok {
			return n
		}
	}
	t.Fatalf("no element matches %q with attributes %v", selector, want)
	return nil
}

func formValues(kind string) url.Values {
	return url.Values{
		"name": {"Deep work"}, "color": {"purple"}, "kind": {kind},
		"focus": {"50"}, "break": {"10"}, "long_break": {"30"}, "rounds": {"4"}, "long_break_every": {"2"},
		"auto_advance": {"1"}, "chime": {"bowl"}, "keep_history": {"1"},
	}
}

func TestNewFormStartsWithDefaults(t *testing.T) {
	s := newServer(t)
	doc := s.Get(t, s.Alice, "/focus/new")
	findEl(t, doc, "form.focus-form", map[string]string{"action": "/focus/timers"})
	findEl(t, doc, "input[name=kind]", map[string]string{"value": "single", "checked": ""})
	findEl(t, doc, "input[name=color]", map[string]string{"value": "teal", "checked": ""})
	findEl(t, doc, "input[name=auto_advance]", map[string]string{"checked": ""})
	findEl(t, doc, "input[name=keep_history]", map[string]string{"checked": ""})
	if v, _ := htmlassert.Attr(doc.MustHave(`input[name=focus]`), "value"); v != "25" {
		t.Errorf("focus default = %q, want 25", v)
	}
	if v, _ := htmlassert.Attr(doc.MustHave(`input[name=rounds]`), "value"); v != "4" {
		t.Errorf("rounds default = %q, want 4", v)
	}
	findEl(t, doc, "option", map[string]string{"value": "bell", "selected": ""})
	if n := len(doc.QueryAll(`input[name=color]`)); n != 8 {
		t.Errorf("%d colour swatches, want 8", n)
	}
}

func TestCreateIntervalsTimer(t *testing.T) {
	s := newServer(t)
	s.Submit(t, s.Alice, "/focus/timers", formValues("intervals"), "/focus/")
	ts, err := s.Store.Timers(context.Background(), s.Alice.User.ID)
	if err != nil || len(ts) != 1 {
		t.Fatalf("timers = %v, %v", ts, err)
	}
	want := focus.TimerInput{
		Name: "Deep work", Color: "purple", Kind: focus.KindIntervals,
		FocusMinutes: 50, BreakMinutes: 10, LongBreakMinutes: 30, Rounds: 4, LongBreakEvery: 2,
		AutoAdvance: true, KeepHistory: true, Chime: "bowl",
	}
	if ts[0].TimerInput != want {
		t.Errorf("stored %+v, want %+v", ts[0].TimerInput, want)
	}
}

func TestCreateSingleTimerIgnoresHiddenIntervalFields(t *testing.T) {
	s := newServer(t)
	v := formValues("single")
	v.Set("rounds", "not a number") // hidden for single timers
	v.Del("auto_advance")
	v.Del("keep_history")
	s.Submit(t, s.Alice, "/focus/timers", v, "/focus/")
	ts, _ := s.Store.Timers(context.Background(), s.Alice.User.ID)
	if len(ts) != 1 || ts[0].Kind != focus.KindSingle || ts[0].Rounds != 0 || ts[0].KeepHistory || ts[0].AutoAdvance {
		t.Errorf("stored %+v", ts)
	}
}

func TestCreateRejectsBadInputAndKeepsWhatWasTyped(t *testing.T) {
	s := newServer(t)
	v := formValues("intervals")
	v.Set("name", "")
	v.Set("rounds", "lots")
	v.Set("long_break", "90")
	rec := s.Post(t, s.Alice, "/focus/timers", v)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("POST = %d, want 422", rec.Code)
	}
	doc := htmlassert.Parse(t, rec.Body.String())
	for id, want := range map[string]string{
		"focus-name-error":       "Give the timer a name.",
		"focus-rounds-error":     "Enter a whole number.",
		"focus-long_break-error": "Long break length must be between 1 and 60 minutes.",
	} {
		if got := htmlassert.Text(doc.MustHave("#" + id)); got != want {
			t.Errorf("#%s = %q, want %q", id, got, want)
		}
	}
	if v, _ := htmlassert.Attr(doc.MustHave(`input[name=rounds]`), "value"); v != "lots" {
		t.Errorf("rounds echoed as %q, want what was typed", v)
	}
	findEl(t, doc, "input[name=color]", map[string]string{"value": "purple", "checked": ""})
	if ts, _ := s.Store.Timers(context.Background(), s.Alice.User.ID); len(ts) != 0 {
		t.Errorf("stored a timer from invalid input: %v", ts)
	}
}

func TestEditFormShowsTheTimer(t *testing.T) {
	s := newServer(t)
	tm := seedTimer(t, s, s.Alice.User.ID, validIntervals())
	doc := s.Get(t, s.Alice, "/focus/timers/"+itoa(tm.ID)+"/edit")
	findEl(t, doc, "form.focus-form", map[string]string{"action": "/focus/timers/" + itoa(tm.ID)})
	if v, _ := htmlassert.Attr(doc.MustHave(`input[name=name]`), "value"); v != "Deep work" {
		t.Errorf("name = %q", v)
	}
	findEl(t, doc, "input[name=kind]", map[string]string{"value": "intervals", "checked": ""})
	findEl(t, doc, "input[name=color]", map[string]string{"value": "blue", "checked": ""})
	findEl(t, doc, "option", map[string]string{"value": "bowl", "selected": ""})
}

func TestEditFormForASingleTimerOffersDefaultIntervals(t *testing.T) {
	s := newServer(t)
	tm := seedTimer(t, s, s.Alice.User.ID, single("Reading", 30))
	doc := s.Get(t, s.Alice, "/focus/timers/"+itoa(tm.ID)+"/edit")
	if v, _ := htmlassert.Attr(doc.MustHave(`input[name=break]`), "value"); v != "5" {
		t.Errorf("break = %q, want the default 5", v)
	}
}

func TestUpdateTimer(t *testing.T) {
	s := newServer(t)
	tm := seedTimer(t, s, s.Alice.User.ID, single("Reading", 30))
	v := formValues("single")
	v.Set("name", "Evening reading")
	v.Set("focus", "45")
	s.Submit(t, s.Alice, "/focus/timers/"+itoa(tm.ID), v, "/focus/")
	got, err := s.Store.Timer(context.Background(), s.Alice.User.ID, tm.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "Evening reading" || got.FocusMinutes != 45 || got.Color != "purple" {
		t.Errorf("updated = %+v", got.TimerInput)
	}
}

func TestUpdateRejectsBadInput(t *testing.T) {
	s := newServer(t)
	tm := seedTimer(t, s, s.Alice.User.ID, single("Reading", 30))
	v := formValues("single")
	v.Set("focus", "0")
	rec := s.Post(t, s.Alice, "/focus/timers/"+itoa(tm.ID), v)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("POST = %d, want 422", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "Focus length must be between 1 and 180 minutes.") {
		t.Error("missing the focus error message")
	}
}

func TestEditAndUpdateAreNotFoundForSomeoneElse(t *testing.T) {
	s := newServer(t)
	tm := seedTimer(t, s, s.Alice.User.ID, single("Mine", 30))
	rec := s.Do(t, s.Bob, httptestGet("/focus/timers/"+itoa(tm.ID)+"/edit"))
	if rec.Code != http.StatusNotFound {
		t.Errorf("bob edit form = %d, want 404", rec.Code)
	}
	if rec := s.Post(t, s.Bob, "/focus/timers/"+itoa(tm.ID), formValues("single")); rec.Code != http.StatusNotFound {
		t.Errorf("bob update = %d, want 404", rec.Code)
	}
}

func TestFormHasAChimePreview(t *testing.T) {
	s := newServer(t)
	for _, path := range []string{"/focus/new", "/focus/timers/" + itoa(seedTimer(t, s, s.Alice.User.ID, single("Reading", 30)).ID) + "/edit"} {
		doc := s.Get(t, s.Alice, path)
		doc.MustHave(`script[src="/focus/chimes.js"]`)
		// Hidden until chimes.js shows it: it does nothing without JavaScript.
		findEl(t, doc, "button", map[string]string{"data-focus-chime-preview": "focus-chime", "type": "button", "hidden": ""})
	}
}
