package main

import (
	"context"
	"time"

	"github.com/iliafrenkel/on-suite/internal/apps/focus"
)

// seedFocus gives the demo account the timers the spec's mockups show.
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
	for _, in := range []focus.TimerInput{deep, reflection, reading, pomodoro} {
		if _, err := st.CreateTimer(ctx, userID, in); err != nil {
			return err
		}
	}
	return nil
}
