package focus

import (
	"strconv"
	"strings"
)

// option is one choice in the colour picker or the chime select.
type option struct {
	Name  string // the stored value
	Label string // what people see (and screen readers hear)
}

var colorOptions = func() []option {
	out := make([]option, len(Colors))
	for i, c := range Colors {
		out[i] = option{Name: c, Label: strings.ToUpper(c[:1]) + c[1:]}
	}
	return out
}()

var chimeLabels = map[string]string{"bell": "Bell", "bowl": "Singing bowl", "soft": "Soft tone", "silent": "Silent"}

var chimeOptions = func() []option {
	out := make([]option, len(Chimes))
	for i, c := range Chimes {
		out[i] = option{Name: c, Label: chimeLabels[c]}
	}
	return out
}()

// formValues is what the form shows: the raw text, so a mistyped number
// comes back exactly as typed.
type formValues struct {
	Name, Color, Kind, Chime                        string
	Focus, Break, LongBreak, Rounds, LongBreakEvery string
	AutoAdvance, KeepHistory                        bool
}

func valuesOf(in TimerInput) formValues {
	return formValues{
		Name: in.Name, Color: in.Color, Kind: string(in.Kind), Chime: in.Chime,
		Focus: strconv.Itoa(in.FocusMinutes), Break: strconv.Itoa(in.BreakMinutes),
		LongBreak: strconv.Itoa(in.LongBreakMinutes), Rounds: strconv.Itoa(in.Rounds),
		LongBreakEvery: strconv.Itoa(in.LongBreakEvery),
		AutoAdvance:    in.AutoAdvance, KeepHistory: in.KeepHistory,
	}
}

// formView is the New/Edit timer page.
type formView struct {
	Action  string // where the form posts
	Heading string
	Submit  string
	Values  formValues
	Errors  FieldErrors
	Colors  []option
	Chimes  []option
}

func newFormView(action, heading, submit string, v formValues, errs FieldErrors) formView {
	return formView{Action: action, Heading: heading, Submit: submit, Values: v, Errors: errs,
		Colors: colorOptions, Chimes: chimeOptions}
}

// editValues is a saved timer as the form shows it. A single timer's
// interval fields show the defaults, so switching to Intervals starts from
// sensible numbers rather than zeros.
func editValues(t Timer) formValues {
	in := t.TimerInput
	if in.Kind == KindSingle {
		d := DefaultInput()
		in.BreakMinutes, in.LongBreakMinutes, in.Rounds, in.LongBreakEvery =
			d.BreakMinutes, d.LongBreakMinutes, d.Rounds, d.LongBreakEvery
	}
	return valuesOf(in)
}

// parseForm reads a posted timer form. It returns the input, the raw
// values to echo back, and an error per number field that isn't a whole
// number. Interval fields are only read for interval timers: they're hidden
// otherwise, so whatever they hold doesn't matter.
func parseForm(get func(string) string) (TimerInput, formValues, FieldErrors) {
	v := formValues{
		Name: get("name"), Color: get("color"), Kind: get("kind"), Chime: get("chime"),
		Focus: get("focus"), Break: get("break"), LongBreak: get("long_break"),
		Rounds: get("rounds"), LongBreakEvery: get("long_break_every"),
		AutoAdvance: get("auto_advance") == "1", KeepHistory: get("keep_history") == "1",
	}
	in := TimerInput{
		Name: v.Name, Color: v.Color, Kind: Kind(v.Kind), Chime: v.Chime,
		AutoAdvance: v.AutoAdvance, KeepHistory: v.KeepHistory,
	}
	errs := FieldErrors{}
	number := func(field, raw string, dst *int) {
		n, err := strconv.Atoi(strings.TrimSpace(raw))
		if err != nil {
			errs[field] = "Enter a whole number."
			return
		}
		*dst = n
	}
	number("focus", v.Focus, &in.FocusMinutes)
	if in.Kind == KindIntervals {
		number("break", v.Break, &in.BreakMinutes)
		number("long_break", v.LongBreak, &in.LongBreakMinutes)
		number("rounds", v.Rounds, &in.Rounds)
		number("long_break_every", v.LongBreakEvery, &in.LongBreakEvery)
	}
	return in, v, errs
}

// merge adds b's messages for fields a has nothing to say about: a
// "whole number" complaint beats the range check of the same field.
func merge(a, b FieldErrors) FieldErrors {
	for k, msg := range b {
		if _, ok := a[k]; !ok {
			a[k] = msg
		}
	}
	return a
}
