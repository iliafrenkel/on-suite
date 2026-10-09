package books_test

import (
	"net/http"
	"net/url"
	"testing"

	"github.com/iliafrenkel/on-suite/internal/htmlassert"
)

// goalText is the Stats page's goal line: "2 of 30 · 21 behind".
func goalText(t *testing.T, s *server, path string) string {
	t.Helper()
	return htmlassert.Text(s.Get(t, s.Alice, path).MustHave(".books-goal-text"))
}

func TestSettingChangingAndRemovingAGoal(t *testing.T) {
	s := newServer(t)
	s.Clock.Set(noon("2026-10-10")) // day 283: 30 × 283 ÷ 365 = 23 expected
	uid := s.Alice.User.ID
	finishedOn(t, s, uid, "Dune", 600, "2026-02-01")
	finishedOn(t, s, uid, "Emma", 300, "2026-03-01")

	doc := s.Get(t, s.Alice, "/books/stats")
	doc.MustNotHave(".books-goal-text")
	doc.MustNotHave(".books-goal-edit")
	if v, _ := htmlassert.Attr(doc.MustHave(`.books-goal-form input[name="year"]`), "value"); v != "2026" {
		t.Errorf("goal form year = %q, want 2026", v)
	}

	s.Submit(t, s.Alice, "/books/goal", url.Values{"year": {"2026"}, "target": {"30"}}, "/books/stats?year=2026")
	if got := goalText(t, s, "/books/stats"); got != "2 of 30 · 21 behind" {
		t.Errorf("goal = %q, want 2 of 30 · 21 behind", got)
	}
	s.Submit(t, s.Alice, "/books/goal", url.Values{"year": {"2026"}, "target": {" 2 "}}, "/books/stats?year=2026")
	if got := goalText(t, s, "/books/stats"); got != "2 of 2 · goal reached" {
		t.Errorf("goal = %q, want 2 of 2 · goal reached", got)
	}
	s.Submit(t, s.Alice, "/books/goal/clear", url.Values{"year": {"2026"}}, "/books/stats?year=2026")
	s.Get(t, s.Alice, "/books/stats").MustNotHave(".books-goal-text")
}

func TestGoalsForOtherYears(t *testing.T) {
	s := newServer(t)
	s.Clock.Set(noon("2026-10-10"))
	finishedOn(t, s, s.Alice.User.ID, "Old", 100, "2025-06-01")
	s.Submit(t, s.Alice, "/books/goal", url.Values{"year": {"2025"}, "target": {"3"}}, "/books/stats?year=2025")
	s.Submit(t, s.Alice, "/books/goal", url.Values{"year": {"2027"}, "target": {"10"}}, "/books/stats?year=2027")
	if got := goalText(t, s, "/books/stats?year=2025"); got != "1 of 3 · 2 short" {
		t.Errorf("2025 goal = %q, want 1 of 3 · 2 short", got)
	}
	if got := goalText(t, s, "/books/stats?year=2027"); got != "0 of 10" {
		t.Errorf("2027 goal = %q, want 0 of 10 and no pace yet", got)
	}
	s.Get(t, s.Alice, "/books/stats").MustNotHave(".books-goal-text") // 2026 has none
}

func TestARefusedGoalComesBackInItsForm(t *testing.T) {
	s := newServer(t)
	s.Clock.Set(noon("2026-10-10"))
	s.Submit(t, s.Alice, "/books/goal", url.Values{"year": {"2026"}, "target": {"30"}}, "/books/stats?year=2026")
	for _, typed := range []string{"0", "1001", "abc", ""} {
		rec := s.Post(t, s.Alice, "/books/goal", url.Values{"year": {"2026"}, "target": {typed}})
		if rec.Code != http.StatusUnprocessableEntity {
			t.Fatalf("goal %q = %d, want 422", typed, rec.Code)
		}
		doc := htmlassert.Parse(t, rec.Body.String())
		if got := htmlassert.Text(doc.MustHave("#books-goal-error")); got != "Enter a goal from 1 to 1000 books." {
			t.Errorf("goal %q: message = %q", typed, got)
		}
		if _, open := htmlassert.Attr(doc.MustHave(".books-goal-edit"), "open"); !open {
			t.Errorf("goal %q: the Change goal box is closed, want it open on the message", typed)
		}
		if v, _ := htmlassert.Attr(doc.MustHave("#books-goal-target"), "value"); v != typed {
			t.Errorf("goal %q: the box holds %q, want what was typed", typed, v)
		}
	}
	if got := goalText(t, s, "/books/stats"); got != "0 of 30 · 23 behind" {
		t.Errorf("goal = %q after refusals, want it unchanged", got)
	}
}

func TestGoalFormsRefuseATamperedYear(t *testing.T) {
	s := newServer(t)
	s.Clock.Set(noon("2026-10-10"))
	for _, path := range []string{"/books/goal", "/books/goal/clear"} {
		for _, year := range []string{"1899", "2028", "abc"} {
			rec := s.Post(t, s.Alice, path, url.Values{"year": {year}, "target": {"10"}})
			if rec.Code != http.StatusBadRequest {
				t.Errorf("POST %s year %q = %d, want 400", path, year, rec.Code)
			}
		}
	}
}

func TestGoalsArePerUser(t *testing.T) {
	s := newServer(t)
	s.Clock.Set(noon("2026-10-10"))
	s.Submit(t, s.Bob, "/books/goal", url.Values{"year": {"2026"}, "target": {"30"}}, "/books/stats?year=2026")
	s.Get(t, s.Alice, "/books/stats").MustNotHave(".books-goal-text")
	s.Submit(t, s.Alice, "/books/goal/clear", url.Values{"year": {"2026"}}, "/books/stats?year=2026")
	doc := s.Get(t, s.Bob, "/books/stats")
	if got := htmlassert.Text(doc.MustHave(".books-goal-text")); got != "0 of 30 · 23 behind" {
		t.Errorf("Bob's goal = %q after Alice cleared hers, want it kept", got)
	}
}
