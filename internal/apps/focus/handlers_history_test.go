package focus_test

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/iliafrenkel/on-suite/internal/apps/focus"
	"github.com/iliafrenkel/on-suite/internal/htmlassert"
)

// seedSession records in for userID through the server's own store.
func seedSession(t *testing.T, s *server, userID int64, in focus.SessionInput) focus.Session {
	t.Helper()
	got, _, err := s.Store.RecordSession(context.Background(), userID, in)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func texts(doc *htmlassert.Doc, selector string) []string {
	var out []string
	for _, n := range doc.QueryAll(selector) {
		out = append(out, htmlassert.Text(n))
	}
	return out
}

func TestHistoryShowsTotalsChartTimersAndSessions(t *testing.T) {
	s := newServer(t)
	s.Clock.Set(localAt(10, 7, 12, 0)) // Wednesday noon
	uid := s.Alice.User.ID
	deep := sessionInput("a", "Deep work", localAt(10, 7, 8, 0), 30)
	deep.Color = "blue"
	seedSession(t, s, uid, deep)
	early := sessionInput("b", "Reading", localAt(10, 6, 20, 0), 20)
	early.Completed = false
	seedSession(t, s, uid, early)
	seedSession(t, s, uid, sessionInput("c", "Reading", localAt(9, 30, 10, 0), 25))
	seedSession(t, s, s.Bob.User.ID, sessionInput("z", "Bob's", localAt(10, 7, 9, 0), 60))

	doc := s.Get(t, s.Alice, "/focus/history")
	doc.MustHave(`a[href="/focus/"]`)
	doc.MustHave(`script[src="/focus/home.js"]`)

	if got := strings.Join(texts(doc, ".focus-total .value"), "|"); got != "30m|50m|50m|1h 15m" {
		t.Errorf("totals = %s, want today|week|month|year = 30m|50m|50m|1h 15m", got)
	}
	if got := strings.Join(texts(doc, ".focus-total .label"), "|"); got != "Today|This week|This month|This year" {
		t.Errorf("total labels = %s", got)
	}

	if bars := doc.QueryAll(".focus-chart rect"); len(bars) != 30 {
		t.Errorf("%d chart bars, want 30", len(bars))
	}
	if got := strings.Join(texts(doc, ".focus-by-timer-name"), "|"); got != "Deep work|Reading" {
		t.Errorf("this month by timer = %s", got)
	}

	rows := doc.QueryAll(".focus-sessions tbody tr")
	if len(rows) != 3 {
		t.Fatalf("%d session rows, want 3 (Bob's must not show)", len(rows))
	}
	if got := strings.Join(texts(doc, ".focus-session-timer"), "|"); got != "Deep work|Reading|Reading" {
		t.Errorf("sessions newest first = %s", got)
	}
	if n := len(doc.QueryAll(".focus-early")); n != 1 {
		t.Errorf("%d stopped-early markers, want 1", n)
	}
	if !strings.Contains(htmlassert.Text(rows[0]), "Wed 7 Oct 2026 · 08:00") {
		t.Errorf("first row = %q, want the local start time", htmlassert.Text(rows[0]))
	}
	form := doc.MustHave(".focus-sessions form")
	if action, _ := htmlassert.Attr(form, "action"); !strings.HasPrefix(action, "/focus/sessions/") {
		t.Errorf("delete form action = %q", action)
	}
	if _, ok := htmlassert.Attr(form, "data-focus-confirm"); !ok {
		t.Error("deleting a session should ask first")
	}
	doc.MustHave("#focus-confirm-dialog")
	doc.MustNotHave(".focus-pager a")
}

func TestHistoryPagesOlderSessions(t *testing.T) {
	s := newServer(t)
	s.Clock.Set(localAt(10, 7, 12, 0))
	for i := 0; i < 51; i++ {
		start := localAt(10, 7, 11, 0).Add(-time.Duration(i) * time.Hour)
		seedSession(t, s, s.Alice.User.ID, sessionInput(fmt.Sprintf("s%02d", i), "Reading", start, 30))
	}
	first := s.Get(t, s.Alice, "/focus/history")
	if n := len(first.QueryAll(".focus-sessions tbody tr")); n != 50 {
		t.Errorf("page 1 has %d rows, want 50", n)
	}
	first.MustHave(`.focus-pager a[href="/focus/history?page=2"]`)

	second := s.Get(t, s.Alice, "/focus/history?page=2")
	if n := len(second.QueryAll(".focus-sessions tbody tr")); n != 1 {
		t.Errorf("page 2 has %d rows, want 1", n)
	}
	second.MustHave(`.focus-pager a[href="/focus/history"]`)
	if v, _ := htmlassert.Attr(second.MustHave(`.focus-sessions input[name="page"]`), "value"); v != "2" {
		t.Errorf("delete form page = %q, want 2", v)
	}

	junk := s.Get(t, s.Alice, "/focus/history?page=abc")
	if n := len(junk.QueryAll(".focus-sessions tbody tr")); n != 50 {
		t.Errorf("?page=abc has %d rows, want page 1's 50", n)
	}
}

func TestHistoryEmptyState(t *testing.T) {
	s := newServer(t)
	doc := s.Get(t, s.Alice, "/focus/history")
	doc.MustHave(".focus-empty")
	doc.MustNotHave(".focus-sessions")
	doc.MustHave(".focus-chart .empty")
	if got := strings.Join(texts(doc, ".focus-total .value"), "|"); got != "0m|0m|0m|0m" {
		t.Errorf("totals = %s", got)
	}
}

// redirectOf GETs path as Alice and returns where a 303 sends her.
func redirectOf(t *testing.T, s *server, path string) string {
	t.Helper()
	rec := s.Do(t, s.Alice, httptestGet(path))
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("GET %s = %d, want 303", path, rec.Code)
	}
	return rec.Header().Get("Location")
}

func TestHistoryPastTheLastPageGoesToTheLastPage(t *testing.T) {
	s := newServer(t)
	s.Clock.Set(localAt(10, 7, 12, 0))
	if got := redirectOf(t, s, "/focus/history?page=3"); got != "/focus/history" {
		t.Errorf("no sessions, page 3 → %q, want /focus/history", got)
	}
	for i := 0; i < 51; i++ {
		start := localAt(10, 7, 11, 0).Add(-time.Duration(i) * time.Hour)
		seedSession(t, s, s.Alice.User.ID, sessionInput(fmt.Sprintf("s%02d", i), "Reading", start, 30))
	}
	if got := redirectOf(t, s, "/focus/history?page=9"); got != "/focus/history?page=2" {
		t.Errorf("51 sessions, page 9 → %q, want /focus/history?page=2", got)
	}
	// Page 2 itself still renders.
	s.Get(t, s.Alice, "/focus/history?page=2").MustHave(".focus-sessions")
}

func TestDeletingTheLastSessionOnAPageLandsOnTheOneBefore(t *testing.T) {
	s := newServer(t)
	s.Clock.Set(localAt(10, 7, 12, 0))
	var oldest focus.Session
	for i := 0; i < 51; i++ {
		start := localAt(10, 7, 11, 0).Add(-time.Duration(i) * time.Hour)
		oldest = seedSession(t, s, s.Alice.User.ID, sessionInput(fmt.Sprintf("s%02d", i), "Reading", start, 30))
	}
	// The 51st (oldest) session is alone on page 2.
	s.Submit(t, s.Alice, "/focus/sessions/"+itoa(oldest.ID)+"/delete", url.Values{"page": {"2"}}, "/focus/history?page=2")
	if got := redirectOf(t, s, "/focus/history?page=2"); got != "/focus/history" {
		t.Errorf("emptied page 2 → %q, want /focus/history", got)
	}
}

func TestSessionCount(t *testing.T) {
	s := newServer(t)
	s.Clock.Set(localAt(10, 7, 12, 0))
	ctx := context.Background()
	seedSession(t, s, s.Alice.User.ID, sessionInput("a", "Reading", localAt(10, 7, 8, 0), 30))
	seedSession(t, s, s.Alice.User.ID, sessionInput("b", "Reading", localAt(10, 6, 8, 0), 30))
	seedSession(t, s, s.Bob.User.ID, sessionInput("z", "Bob's", localAt(10, 7, 9, 0), 30))
	if n, err := s.Store.SessionCount(ctx, s.Alice.User.ID); err != nil || n != 2 {
		t.Errorf("Alice SessionCount = %d, %v; want 2", n, err)
	}
}
