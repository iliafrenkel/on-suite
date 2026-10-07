package focus_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/iliafrenkel/on-suite/internal/apps/focus"
)

func validSingle() focus.TimerInput {
	in := focus.DefaultInput()
	in.Name = "Daily Reflection"
	in.FocusMinutes = 15
	return in
}

func validIntervals() focus.TimerInput {
	return focus.TimerInput{
		Name: "Deep work", Color: "blue", Kind: focus.KindIntervals,
		FocusMinutes: 50, BreakMinutes: 10, LongBreakMinutes: 30, Rounds: 4, LongBreakEvery: 2,
		AutoAdvance: true, KeepHistory: true, Chime: "bowl",
	}
}

func TestDefaultInput(t *testing.T) {
	in := focus.DefaultInput()
	if in.Kind != focus.KindSingle || in.Color != "teal" || in.Chime != "bell" ||
		in.FocusMinutes != 25 || in.BreakMinutes != 5 || in.LongBreakMinutes != 15 ||
		in.Rounds != 4 || in.LongBreakEvery != 4 || !in.AutoAdvance || !in.KeepHistory || in.Name != "" {
		t.Errorf("DefaultInput() = %+v", in)
	}
}

func TestNormalizeTrimsTheNameAndClearsIntervalsForSingle(t *testing.T) {
	in := focus.DefaultInput()
	in.Name = "  Reading \n"
	got := in.Normalize()
	if got.Name != "Reading" {
		t.Errorf("Name = %q, want %q", got.Name, "Reading")
	}
	if got.BreakMinutes != 0 || got.LongBreakMinutes != 0 || got.Rounds != 0 || got.LongBreakEvery != 0 {
		t.Errorf("single timer kept interval fields: %+v", got)
	}
	iv := validIntervals().Normalize()
	if iv.Rounds != 4 || iv.BreakMinutes != 10 {
		t.Errorf("Normalize cleared an intervals timer's fields: %+v", iv)
	}
}

func TestValidateAcceptsGoodInput(t *testing.T) {
	for name, in := range map[string]focus.TimerInput{"single": validSingle(), "intervals": validIntervals()} {
		if errs := in.Normalize().Validate(); errs != nil {
			t.Errorf("%s: Validate() = %v, want nil", name, errs)
		}
	}
}

func TestValidateRejectsBadInput(t *testing.T) {
	tests := []struct {
		name  string
		edit  func(*focus.TimerInput)
		field string
	}{
		{"empty name", func(in *focus.TimerInput) { in.Name = "   " }, "name"},
		{"long name", func(in *focus.TimerInput) { in.Name = strings.Repeat("é", 81) }, "name"},
		{"unknown colour", func(in *focus.TimerInput) { in.Color = "Teal" }, "color"},
		{"unknown kind", func(in *focus.TimerInput) { in.Kind = "loop" }, "kind"},
		{"focus zero", func(in *focus.TimerInput) { in.FocusMinutes = 0 }, "focus"},
		{"focus too long", func(in *focus.TimerInput) { in.FocusMinutes = 181 }, "focus"},
		{"break zero", func(in *focus.TimerInput) { in.BreakMinutes = 0 }, "break"},
		{"break too long", func(in *focus.TimerInput) { in.BreakMinutes = 61 }, "break"},
		{"long break too long", func(in *focus.TimerInput) { in.LongBreakMinutes = 61 }, "long_break"},
		{"rounds zero", func(in *focus.TimerInput) { in.Rounds = 0 }, "rounds"},
		{"rounds too many", func(in *focus.TimerInput) { in.Rounds = 13 }, "rounds"},
		{"long every zero", func(in *focus.TimerInput) { in.LongBreakEvery = 0 }, "long_break_every"},
		{"long every past rounds", func(in *focus.TimerInput) { in.LongBreakEvery = 5 }, "long_break_every"},
		{"unknown chime", func(in *focus.TimerInput) { in.Chime = "gong" }, "chime"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in := validIntervals()
			tt.edit(&in)
			errs := in.Normalize().Validate()
			if errs[tt.field] == "" {
				t.Errorf("Validate() = %v, want an error for %q", errs, tt.field)
			}
		})
	}
}

func TestValidateAllowsEightyCharacterNames(t *testing.T) {
	in := validSingle()
	in.Name = strings.Repeat("é", 80) // 80 runes, 160 bytes
	if errs := in.Normalize().Validate(); errs != nil {
		t.Errorf("Validate() = %v, want nil", errs)
	}
}

func TestValidateIgnoresIntervalFieldsOnSingleTimers(t *testing.T) {
	in := validSingle()
	in.Rounds = 99 // hidden in the form; Normalize clears it
	if errs := in.Normalize().Validate(); errs != nil {
		t.Errorf("Validate() = %v, want nil", errs)
	}
}

func TestValidationErrorIsErrInvalid(t *testing.T) {
	var err error = &focus.ValidationError{Fields: focus.FieldErrors{"name": "x"}}
	if !errors.Is(err, focus.ErrInvalid) {
		t.Error("ValidationError does not unwrap to ErrInvalid")
	}
}
