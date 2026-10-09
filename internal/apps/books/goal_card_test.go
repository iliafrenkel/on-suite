package books_test

import (
	"context"
	"testing"

	"github.com/iliafrenkel/on-suite/internal/htmlassert"
)

// setGoal sets one of userID's goals through the store.
func setGoal(t *testing.T, s *server, userID int64, year, target int) {
	t.Helper()
	if err := s.Store.SetGoal(context.Background(), userID, year, target); err != nil {
		t.Fatal(err)
	}
}

func TestReadingShelfShowsThisYearsGoal(t *testing.T) {
	s := newServer(t)
	s.Clock.Set(noon("2026-10-10")) // 23 of 30 expected by now
	uid := s.Alice.User.ID
	finishedOn(t, s, uid, "Dune", 600, "2026-02-01")
	finishedOn(t, s, uid, "Emma", 300, "2026-03-01")
	setGoal(t, s, uid, 2026, 30)

	for _, doc := range []*htmlassert.Doc{
		s.Get(t, s.Alice, "/books/"),
		htmlassert.Parse(t, hx(t, s, "/books/?shelf=reading", "books-list")), // a shelf switch brings it too
	} {
		card := doc.MustHave("#books-list .books-goal-card")
		if href, _ := htmlassert.Attr(card, "href"); href != "/books/stats" {
			t.Errorf("goal card links to %q, want the Stats page", href)
		}
		if got := htmlassert.Text(doc.MustHave(".books-goal-card-text")); got != "2 of 30 · 21 behind" {
			t.Errorf("goal card = %q, want 2 of 30 · 21 behind", got)
		}
		if got := htmlassert.Text(doc.MustHave(".books-goal-card-head")); got != "2026 goal" {
			t.Errorf("goal card head = %q", got)
		}
		doc.MustNotHave(".books-goal-card form") // read-only: the goal is set on the Stats page
	}
}

func TestGoalCardIsOnlyOnTheReadingShelf(t *testing.T) {
	s := newServer(t)
	s.Clock.Set(noon("2026-10-10"))
	setGoal(t, s, s.Alice.User.ID, 2026, 30)
	for _, path := range []string{"/books/?shelf=want", "/books/?shelf=read", "/books/?shelf=all",
		"/books/?shelf=reading&tag=sf", "/books/?shelf=reading&series=Dune"} {
		s.Get(t, s.Alice, path).MustNotHave(".books-goal-card")
	}
}

func TestGoalCardNeedsAGoalForThisYear(t *testing.T) {
	s := newServer(t)
	s.Clock.Set(noon("2026-10-10"))
	s.Get(t, s.Alice, "/books/").MustNotHave(".books-goal-card")
	setGoal(t, s, s.Alice.User.ID, 2027, 30)
	setGoal(t, s, s.Alice.User.ID, 2025, 30)
	s.Get(t, s.Alice, "/books/").MustNotHave(".books-goal-card")
	setGoal(t, s, s.Bob.User.ID, 2026, 30)
	s.Get(t, s.Alice, "/books/").MustNotHave(".books-goal-card")
}
