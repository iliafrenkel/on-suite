package admin

import "testing"

func TestAppliedAtLabelKeepsThePagesTimestampText(t *testing.T) {
	cases := map[string]string{
		"2026-09-25T12:00:05.000000000Z": "2026-09-25T12:00:05Z",
		"2026-09-25T12:00:05.500000000Z": "2026-09-25T12:00:05.5Z",
		"2026-09-25T12:00:05.244124000Z": "2026-09-25T12:00:05.244124Z",
		"2026-09-25T12:00:05.244124Z":    "2026-09-25T12:00:05.244124Z", // legacy, as stored before #356
		"not a time":                     "not a time",
	}
	for stored, want := range cases {
		if got := appliedAtLabel(stored); got != want {
			t.Errorf("appliedAtLabel(%q) = %q, want %q", stored, got, want)
		}
	}
}
