package focus_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/iliafrenkel/on-suite/internal/apps/focus"
)

// Melbourne is UTC+11 in October 2026 (summer time began 4 Oct), so the
// UTC date changes at 11 am local. 7 Oct 2026 is a Wednesday; its week
// began Monday 5 Oct.

// localAt is a wall-clock time on the pinned local zone (main_test.go).
func localAt(month time.Month, day, hour, minute int) time.Time {
	return time.Date(2026, month, day, hour, minute, 0, 0, time.Local)
}

// statsFixture records sessions around Wednesday 7 Oct 2026, noon local:
//
//	Wed 7 Oct 08:00, 30m   today, week, month, year
//	Wed 7 Oct 00:30, 5m    today — Tue 13:30 UTC, the previous UTC day
//	Tue 6 Oct 20:00, 20m   week, month, year
//	Sun 4 Oct 10:00, 15m   month, year (the week starts Monday)
//	Wed 30 Sep 10:00, 25m  year
//	Mon 7 Sep 23:30, 10m   year; one day before the 30-day window
//	31 Dec 2025 23:00, 10m none
//
// plus one of Bob's today, which never counts for Alice.
func statsFixture(t *testing.T) (*fixture, time.Time) {
	t.Helper()
	f := newFixture(t)
	now := localAt(10, 7, 12, 0)
	f.now = now
	ctx := context.Background()
	add := func(userID int64, id, name, color string, start time.Time, minutes int) {
		t.Helper()
		in := sessionInput(id, name, start, minutes)
		in.Color = color
		if _, _, err := f.store.RecordSession(ctx, userID, in); err != nil {
			t.Fatal(err)
		}
	}
	add(f.alice.ID, "a", "Deep work", "blue", localAt(10, 7, 8, 0), 30)
	add(f.alice.ID, "b", "Reading", "green", localAt(10, 7, 0, 30), 5)
	add(f.alice.ID, "c", "Deep work", "blue", localAt(10, 6, 20, 0), 20)
	add(f.alice.ID, "d", "Reading", "amber", localAt(10, 4, 10, 0), 15)
	add(f.alice.ID, "e", "Deep work", "blue", localAt(9, 30, 10, 0), 25)
	add(f.alice.ID, "f", "Deep work", "blue", localAt(9, 7, 23, 30), 10)
	add(f.alice.ID, "g", "Deep work", "blue", time.Date(2025, 12, 31, 23, 0, 0, 0, time.Local), 10)
	add(f.bob.ID, "a", "Bob's", "teal", localAt(10, 7, 9, 0), 60)
	return f, now
}

func TestTotalsFollowTheLocalCalendar(t *testing.T) {
	f, now := statsFixture(t)
	got, err := f.store.Totals(context.Background(), f.alice.ID, now)
	if err != nil {
		t.Fatal(err)
	}
	want := focus.Totals{
		Today: 35 * 60, TodaySessions: 2,
		Week:  55 * 60,
		Month: 70 * 60,
		Year:  105 * 60,
		Any:   true,
	}
	if got != want {
		t.Errorf("Totals = %+v\nwant     %+v", got, want)
	}
}

func TestTotalsForSomeoneWithNoHistory(t *testing.T) {
	f := newFixture(t)
	got, err := f.store.Totals(context.Background(), f.alice.ID, f.now)
	if err != nil {
		t.Fatal(err)
	}
	if got != (focus.Totals{}) {
		t.Errorf("Totals = %+v, want zero", got)
	}
}

func TestTotalsWeekReachesBackIntoLastYear(t *testing.T) {
	// Fri 1 Jan 2027: the week began Mon 28 Dec 2026, the year today.
	f := newFixture(t)
	now := time.Date(2027, 1, 1, 12, 0, 0, 0, time.Local)
	f.now = now
	in := sessionInput("dec", "Reading", time.Date(2026, 12, 29, 9, 0, 0, 0, time.Local), 40)
	if _, _, err := f.store.RecordSession(context.Background(), f.alice.ID, in); err != nil {
		t.Fatal(err)
	}
	got, err := f.store.Totals(context.Background(), f.alice.ID, now)
	if err != nil {
		t.Fatal(err)
	}
	if got.Week != 40*60 || got.Year != 0 || got.Month != 0 || got.Today != 0 {
		t.Errorf("Totals = %+v; want 40m this week and nothing this year", got)
	}
}

func TestDailyHasEveryDayOldestFirst(t *testing.T) {
	f, now := statsFixture(t)
	days, err := f.store.Daily(context.Background(), f.alice.ID, 30, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(days) != 30 {
		t.Fatalf("%d days, want 30", len(days))
	}
	first, last := days[0].Day, days[29].Day
	if !first.Equal(localAt(9, 8, 0, 0)) || !last.Equal(localAt(10, 7, 0, 0)) {
		t.Errorf("window %v – %v, want 8 Sep – 7 Oct local midnights", first, last)
	}
	want := map[string]int{"2026-10-07": 35 * 60, "2026-10-06": 20 * 60, "2026-10-04": 15 * 60, "2026-09-30": 25 * 60}
	for _, d := range days {
		key := d.Day.Format("2006-01-02")
		if d.Seconds != want[key] {
			t.Errorf("%s: %d s, want %d", key, d.Seconds, want[key])
		}
	}
}

func TestByTimerThisMonthLargestFirst(t *testing.T) {
	f, _ := statsFixture(t)
	got, err := f.store.ByTimer(context.Background(), f.alice.ID, localAt(10, 1, 0, 0))
	if err != nil {
		t.Fatal(err)
	}
	want := []focus.TimerTotal{
		{Name: "Deep work", Color: "blue", Seconds: 50 * 60},
		// Reading's newest session this month is green, its older one amber.
		{Name: "Reading", Color: "green", Seconds: 20 * 60},
	}
	if len(got) != len(want) {
		t.Fatalf("ByTimer = %+v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("row %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestRecentSessionsPages(t *testing.T) {
	f, _ := statsFixture(t)
	ctx := context.Background()
	var all []string
	for page := 1; ; page++ {
		got, more, err := f.store.RecentSessions(ctx, f.alice.ID, page, 3)
		if err != nil {
			t.Fatal(err)
		}
		for _, s := range got {
			all = append(all, s.ClientID)
		}
		if !more {
			break
		}
		if page > 5 {
			t.Fatal("paging never ends")
		}
	}
	// Newest first by start time, Alice's only.
	if got := fmt.Sprint(all); got != "[a b c d e f g]" {
		t.Errorf("sessions = %s", got)
	}
	empty, more, err := f.store.RecentSessions(ctx, f.alice.ID, 9, 3)
	if err != nil || len(empty) != 0 || more {
		t.Errorf("page past the end: %d sessions, more %v, err %v", len(empty), more, err)
	}
}

func TestFormatFocus(t *testing.T) {
	for seconds, want := range map[int]string{
		0: "0m", 59: "0m", 60: "1m", 45 * 60: "45m", 3600: "1h", 3600 + 40*60 + 30: "1h 40m", 25 * 3600: "25h",
	} {
		if got := focus.FormatFocus(seconds); got != want {
			t.Errorf("FormatFocus(%d) = %q, want %q", seconds, got, want)
		}
	}
}
