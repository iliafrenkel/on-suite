package main

import (
	"context"
	"fmt"
	"time"

	"github.com/iliafrenkel/on-suite/internal/apps/focus"
)

// seedFocus gives the demo account the timers the spec's mockups show and
// about three weeks of sessions, so the History page and the today strip
// have something to say.
func seedFocus(ctx context.Context, st *focus.Store, userID int64, now time.Time) error {
	st.SetClock(func() time.Time { return now })
	deep := focus.TimerInput{
		Name: "Deep work", Color: "teal", Kind: focus.KindIntervals,
		FocusMinutes: 50, BreakMinutes: 10, LongBreakMinutes: 30, Rounds: 4, LongBreakEvery: 2,
		AutoAdvance: true, KeepHistory: true, Chime: "bowl",
	}
	pomodoro := focus.TimerInput{
		Name: "Pomodoro", Color: "amber", Kind: focus.KindIntervals,
		FocusMinutes: 25, BreakMinutes: 5, LongBreakMinutes: 15, Rounds: 4, LongBreakEvery: 4,
		AutoAdvance: true, KeepHistory: true, Chime: "bell",
	}
	reflection := focus.DefaultInput()
	reflection.Name, reflection.Color, reflection.FocusMinutes, reflection.Chime = "Daily Reflection", "coral", 15, "soft"
	reading := focus.DefaultInput()
	reading.Name, reading.Color, reading.FocusMinutes = "Reading", "purple", 30
	// Deep work must stay ID 1: shots.go opens /focus/run/1.
	byName := map[string]focus.Timer{}
	for _, in := range []focus.TimerInput{deep, reflection, reading, pomodoro} {
		tm, err := st.CreateTimer(ctx, userID, in)
		if err != nil {
			return err
		}
		byName[tm.Name] = tm
	}
	return seedFocusSessions(ctx, st, userID, now, byName)
}

// seedFocusSessions records 21 days of sessions ending a few hours before
// now: Deep work most days (2–4 rounds), a reflection most evenings,
// Reading every other day and a Pomodoro now and then, with every sixth
// day off. Times are offsets from now, so the seed never records a session
// in the future, whatever time of day it runs.
func seedFocusSessions(ctx context.Context, st *focus.Store, userID int64, now time.Time, timers map[string]focus.Timer) error {
	n := 0
	add := func(name string, start time.Time, focusMin, roundsDone int, length time.Duration, completed bool) error {
		tm := timers[name]
		n++
		_, _, err := st.RecordSession(ctx, userID, focus.SessionInput{
			ClientID: fmt.Sprintf("demo-%03d", n), TimerID: tm.ID, TimerName: tm.Name, Color: tm.Color,
			StartedAt: start, EndedAt: start.Add(length),
			FocusSeconds: focusMin * 60, RoundsDone: roundsDone, Completed: completed,
		})
		return err
	}
	for d := 0; d < 21; d++ {
		if d%6 == 5 {
			continue // a day off
		}
		day := now.Add(-time.Duration(d) * 24 * time.Hour)
		rounds := 2 + d%3
		if err := add("Deep work", day.Add(-9*time.Hour), rounds*50, rounds,
			time.Duration(rounds)*time.Hour, rounds == 4); err != nil {
			return err
		}
		if d%2 == 0 {
			if err := add("Reading", day.Add(-5*time.Hour), 30, 1, 30*time.Minute, true); err != nil {
				return err
			}
		}
		if d%4 == 1 {
			if err := add("Pomodoro", day.Add(-12*time.Hour), 100, 4, 125*time.Minute, true); err != nil {
				return err
			}
		}
		if d > 0 {
			if err := add("Daily Reflection", day.Add(-2*time.Hour), 15, 1, 15*time.Minute, true); err != nil {
				return err
			}
		}
	}
	return nil
}
