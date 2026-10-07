package focus

import (
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"
)

// Kind is a timer's shape.
type Kind string

// The two kinds of timer (spec: "Timer kinds").
const (
	KindSingle    Kind = "single"
	KindIntervals Kind = "intervals"
)

// Colors is the palette a timer's owner picks from, in picker order. It
// mirrors ON Flash's DeckColors (apps never import each other); the
// swatch-c-* classes in app.css are the shared part.
var Colors = []string{"teal", "blue", "purple", "pink", "coral", "amber", "green", "gray"}

// DefaultColor must match migrations/0001_timers.sql's column default.
const DefaultColor = "teal"

// Chimes are the phase-end sounds the running page knows how to play.
var Chimes = []string{"bell", "bowl", "soft", "silent"}

// DefaultChime must match migrations/0001_timers.sql's column default.
const DefaultChime = "bell"

// MaxNameRunes caps a timer's name, counted in characters, not bytes.
const MaxNameRunes = 80

// Limits, in minutes or rounds.
const (
	maxFocusMinutes = 180
	maxBreakMinutes = 60
	maxRounds       = 12
)

// TimerInput is everything a person sets on a timer. Interval fields are
// ignored (and cleared by Normalize) for single timers.
type TimerInput struct {
	Name             string
	Color            string
	Kind             Kind
	FocusMinutes     int
	BreakMinutes     int
	LongBreakMinutes int
	Rounds           int
	LongBreakEvery   int
	AutoAdvance      bool
	KeepHistory      bool
	Chime            string
}

// DefaultInput is what the New timer form starts with. The interval
// fields are filled in so switching the form to Intervals shows sensible
// numbers.
func DefaultInput() TimerInput {
	return TimerInput{
		Color: DefaultColor, Kind: KindSingle,
		FocusMinutes: 25, BreakMinutes: 5, LongBreakMinutes: 15, Rounds: 4, LongBreakEvery: 4,
		AutoAdvance: true, KeepHistory: true, Chime: DefaultChime,
	}
}

// Normalize trims the name and clears interval fields on a single timer.
func (in TimerInput) Normalize() TimerInput {
	in.Name = strings.TrimSpace(in.Name)
	if in.Kind == KindSingle {
		in.BreakMinutes, in.LongBreakMinutes, in.Rounds, in.LongBreakEvery = 0, 0, 0, 0
	}
	return in
}

// FieldErrors maps a form field name to the message shown next to it.
type FieldErrors map[string]string

// Validate checks normalized input against the spec's limits. It returns
// nil when everything is fine.
func (in TimerInput) Validate() FieldErrors {
	errs := FieldErrors{}
	switch n := utf8.RuneCountInString(in.Name); {
	case n == 0:
		errs["name"] = "Give the timer a name."
	case n > MaxNameRunes:
		errs["name"] = fmt.Sprintf("Keep the name to %d characters or fewer.", MaxNameRunes)
	}
	if !slices.Contains(Colors, in.Color) {
		errs["color"] = "Pick one of the colours shown."
	}
	if in.Kind != KindSingle && in.Kind != KindIntervals {
		errs["kind"] = "Pick Single block or Intervals."
	}
	if in.FocusMinutes < 1 || in.FocusMinutes > maxFocusMinutes {
		errs["focus"] = fmt.Sprintf("Focus length must be between 1 and %d minutes.", maxFocusMinutes)
	}
	if in.Kind == KindIntervals {
		if in.BreakMinutes < 1 || in.BreakMinutes > maxBreakMinutes {
			errs["break"] = fmt.Sprintf("Break length must be between 1 and %d minutes.", maxBreakMinutes)
		}
		if in.LongBreakMinutes < 1 || in.LongBreakMinutes > maxBreakMinutes {
			errs["long_break"] = fmt.Sprintf("Long break length must be between 1 and %d minutes.", maxBreakMinutes)
		}
		if in.Rounds < 1 || in.Rounds > maxRounds {
			errs["rounds"] = fmt.Sprintf("Rounds must be between 1 and %d.", maxRounds)
		}
		if in.LongBreakEvery < 1 || (in.Rounds >= 1 && in.LongBreakEvery > in.Rounds) {
			errs["long_break_every"] = "Long break every must be between 1 and the number of rounds."
		}
	}
	if !slices.Contains(Chimes, in.Chime) {
		errs["chime"] = "Pick one of the chimes listed."
	}
	if len(errs) == 0 {
		return nil
	}
	return errs
}

// ValidationError is ErrInvalid with a message per form field, so a
// handler can re-render the form with each message next to its field.
type ValidationError struct{ Fields FieldErrors }

func (e *ValidationError) Error() string { return "focus: invalid timer" }

func (e *ValidationError) Unwrap() error { return ErrInvalid }
