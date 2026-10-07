package focus

import (
	"fmt"
	"strconv"
)

// PhaseKind is what a phase of a running timer is for.
type PhaseKind string

// The three kinds of phase.
const (
	PhaseFocus     PhaseKind = "focus"
	PhaseBreak     PhaseKind = "break"
	PhaseLongBreak PhaseKind = "long_break"
)

// Phase is one stretch of a running timer. Round is the focus round, or
// for a break the round just finished. The JSON tags are what the running
// page (F2) reads.
type Phase struct {
	Kind    PhaseKind `json:"kind"`
	Seconds int       `json:"seconds"`
	Round   int       `json:"round"`
}

// Phases expands a timer into the phases it runs through (spec: "Phase
// list"). A single timer is one focus phase. Intervals alternate focus and
// breaks, with a long break after every LongBreakEvery-th round, and never
// end on a break — so LongBreakEvery == Rounds means no long break at all.
func Phases(in TimerInput) []Phase {
	if in.Kind != KindIntervals {
		return []Phase{{Kind: PhaseFocus, Seconds: in.FocusMinutes * 60, Round: 1}}
	}
	out := make([]Phase, 0, 2*in.Rounds-1)
	for round := 1; round <= in.Rounds; round++ {
		if round > 1 {
			prev := round - 1
			if in.LongBreakEvery > 0 && prev%in.LongBreakEvery == 0 {
				out = append(out, Phase{Kind: PhaseLongBreak, Seconds: in.LongBreakMinutes * 60, Round: prev})
			} else {
				out = append(out, Phase{Kind: PhaseBreak, Seconds: in.BreakMinutes * 60, Round: prev})
			}
		}
		out = append(out, Phase{Kind: PhaseFocus, Seconds: in.FocusMinutes * 60, Round: round})
	}
	return out
}

// TotalSeconds is the whole length of a run, breaks included.
func TotalSeconds(phases []Phase) int {
	total := 0
	for _, p := range phases {
		total += p.Seconds
	}
	return total
}

// FormatLength renders a duration the way the tiles show it: "15 min"
// under an hour (rounded up to a whole minute), "1h" or "3h 20m" above.
func FormatLength(seconds int) string {
	minutes := (seconds + 59) / 60
	if minutes < 60 {
		return strconv.Itoa(minutes) + " min"
	}
	h, m := minutes/60, minutes%60
	if m == 0 {
		return fmt.Sprintf("%dh", h)
	}
	return fmt.Sprintf("%dh %dm", h, m)
}

// Summary is a tile's second line: "15 min", "25 / 5 × 4", or with a long
// break that actually happens, "50 / 10 × 4 · long 30".
func Summary(in TimerInput) string {
	if in.Kind != KindIntervals {
		return strconv.Itoa(in.FocusMinutes) + " min"
	}
	s := fmt.Sprintf("%d / %d × %d", in.FocusMinutes, in.BreakMinutes, in.Rounds)
	if in.LongBreakEvery < in.Rounds {
		s += fmt.Sprintf(" · long %d", in.LongBreakMinutes)
	}
	return s
}

// Label names a phase for people. rounds is the timer's round count, 0 for
// a single timer (whose one phase is just "Focus").
func (p Phase) Label(rounds int) string {
	switch p.Kind {
	case PhaseBreak:
		return "Short break"
	case PhaseLongBreak:
		return "Long break"
	}
	if rounds == 0 {
		return "Focus"
	}
	return fmt.Sprintf("Focus · round %d of %d", p.Round, rounds)
}
