package focus_test

import (
	"reflect"
	"testing"

	"github.com/iliafrenkel/on-suite/internal/apps/focus"
)

func intervals(focusMin, breakMin, longMin, rounds, every int) focus.TimerInput {
	return focus.TimerInput{
		Name: "x", Color: "teal", Kind: focus.KindIntervals, Chime: "bell",
		FocusMinutes: focusMin, BreakMinutes: breakMin, LongBreakMinutes: longMin,
		Rounds: rounds, LongBreakEvery: every,
	}
}

func TestPhasesSingle(t *testing.T) {
	in := focus.TimerInput{Kind: focus.KindSingle, FocusMinutes: 15}
	want := []focus.Phase{{Kind: focus.PhaseFocus, Seconds: 900, Round: 1}}
	if got := focus.Phases(in); !reflect.DeepEqual(got, want) {
		t.Errorf("Phases = %+v, want %+v", got, want)
	}
}

func TestPhasesIntervalsWithLongBreaks(t *testing.T) {
	got := focus.Phases(intervals(50, 10, 30, 4, 2))
	F, B, L := focus.PhaseFocus, focus.PhaseBreak, focus.PhaseLongBreak
	want := []focus.Phase{
		{Kind: F, Seconds: 3000, Round: 1}, {Kind: B, Seconds: 600, Round: 1}, {Kind: F, Seconds: 3000, Round: 2}, {Kind: L, Seconds: 1800, Round: 2},
		{Kind: F, Seconds: 3000, Round: 3}, {Kind: B, Seconds: 600, Round: 3}, {Kind: F, Seconds: 3000, Round: 4},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Phases =\n%+v\nwant\n%+v", got, want)
	}
}

func TestPhasesLongBreakEveryRoundsMeansNoLongBreak(t *testing.T) {
	for _, p := range focus.Phases(intervals(25, 5, 15, 4, 4)) {
		if p.Kind == focus.PhaseLongBreak {
			t.Fatalf("got a long break with long_break_every == rounds: %+v", p)
		}
	}
}

func TestPhasesOneRoundHasNoBreak(t *testing.T) {
	got := focus.Phases(intervals(25, 5, 15, 1, 1))
	want := []focus.Phase{{Kind: focus.PhaseFocus, Seconds: 1500, Round: 1}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Phases = %+v, want %+v", got, want)
	}
}

func TestPhasesNeverEndOnABreak(t *testing.T) {
	for rounds := 1; rounds <= 12; rounds++ {
		for every := 1; every <= rounds; every++ {
			ps := focus.Phases(intervals(25, 5, 15, rounds, every))
			if last := ps[len(ps)-1]; last.Kind != focus.PhaseFocus || last.Round != rounds {
				t.Fatalf("rounds=%d every=%d: last phase %+v", rounds, every, last)
			}
			if len(ps) != 2*rounds-1 {
				t.Fatalf("rounds=%d: %d phases, want %d", rounds, len(ps), 2*rounds-1)
			}
		}
	}
}

func TestFormatLength(t *testing.T) {
	for secs, want := range map[int]string{
		60: "1 min", 900: "15 min", 3540: "59 min", 3600: "1h", 4200: "1h 10m", 12000: "3h 20m", 90: "2 min",
	} {
		if got := focus.FormatLength(secs); got != want {
			t.Errorf("FormatLength(%d) = %q, want %q", secs, got, want)
		}
	}
}

func TestSummary(t *testing.T) {
	tests := []struct {
		in   focus.TimerInput
		want string
	}{
		{focus.TimerInput{Kind: focus.KindSingle, FocusMinutes: 15}, "15 min"},
		{intervals(50, 10, 30, 4, 2), "50 / 10 × 4 · long 30"},
		{intervals(25, 5, 15, 4, 4), "25 / 5 × 4"},
	}
	for _, tt := range tests {
		if got := focus.Summary(tt.in); got != tt.want {
			t.Errorf("Summary(%+v) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestPhaseLabel(t *testing.T) {
	tests := []struct {
		p      focus.Phase
		rounds int
		want   string
	}{
		{focus.Phase{Kind: focus.PhaseFocus, Round: 1}, 0, "Focus"},
		{focus.Phase{Kind: focus.PhaseFocus, Round: 2}, 4, "Focus · round 2 of 4"},
		{focus.Phase{Kind: focus.PhaseBreak, Round: 1}, 4, "Short break"},
		{focus.Phase{Kind: focus.PhaseLongBreak, Round: 2}, 4, "Long break"},
	}
	for _, tt := range tests {
		if got := tt.p.Label(tt.rounds); got != tt.want {
			t.Errorf("Label(%+v, %d) = %q, want %q", tt.p, tt.rounds, got, tt.want)
		}
	}
}

func TestClock(t *testing.T) {
	for _, c := range []struct {
		seconds int
		want    string
	}{
		{0, "00:00"}, {59, "00:59"}, {300, "05:00"}, {3000, "50:00"},
		{3600, "1:00:00"}, {5400, "1:30:00"}, {10800, "3:00:00"},
	} {
		if got := focus.Clock(c.seconds); got != c.want {
			t.Errorf("Clock(%d) = %q, want %q", c.seconds, got, c.want)
		}
	}
}
