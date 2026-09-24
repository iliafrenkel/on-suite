package db

import (
	"context"
	"math/rand/v2"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestFormatTimeIsFixedWidthUTC(t *testing.T) {
	melbourne := time.FixedZone("AEST", 10*60*60)
	cases := []struct {
		in   time.Time
		want string
	}{
		{time.Date(2026, 9, 25, 12, 0, 5, 0, time.UTC), "2026-09-25T12:00:05.000000000Z"},
		{time.Date(2026, 9, 25, 12, 0, 5, 500_000_000, time.UTC), "2026-09-25T12:00:05.500000000Z"},
		{time.Date(2026, 9, 25, 12, 0, 5, 244_000_000, time.UTC), "2026-09-25T12:00:05.244000000Z"},
		{time.Date(2026, 9, 25, 12, 0, 5, 244_124_000, time.UTC), "2026-09-25T12:00:05.244124000Z"},
		{time.Date(2026, 9, 25, 12, 0, 5, 123_456_789, time.UTC), "2026-09-25T12:00:05.123456789Z"},
		// Not UTC on the way in: stored as the same instant in UTC.
		{time.Date(2026, 9, 25, 22, 0, 5, 1, melbourne), "2026-09-25T12:00:05.000000001Z"},
	}
	for _, tc := range cases {
		got := FormatTime(tc.in)
		if got != tc.want {
			t.Errorf("FormatTime(%v) = %q, want %q", tc.in, got, tc.want)
		}
		if len(got) != 30 {
			t.Errorf("FormatTime(%v) is %d characters, want 30", tc.in, len(got))
		}
	}
}

func TestParseTimeRoundTripsFormatTime(t *testing.T) {
	r := rand.New(rand.NewPCG(356, 1))
	base := time.Date(2026, 9, 25, 0, 0, 0, 0, time.UTC)
	for range 1000 {
		in := base.Add(time.Duration(r.Int64N(int64(365 * 24 * time.Hour))))
		got, err := ParseTime(FormatTime(in))
		if err != nil {
			t.Fatalf("ParseTime(FormatTime(%v)): %v", in, err)
		}
		if !got.Equal(in) || got.Location() != time.UTC {
			t.Fatalf("round trip of %v = %v, want the same instant in UTC", in, got)
		}
	}
}

// TestParseTimeAcceptsLegacyRFC3339Nano pins that rows written before #356
// (and any a migration somehow missed) still read back as the right
// instant: time.Parse accepts any fraction length after the seconds.
func TestParseTimeAcceptsLegacyRFC3339Nano(t *testing.T) {
	cases := map[string]time.Time{
		"2026-09-25T12:00:05Z":           time.Date(2026, 9, 25, 12, 0, 5, 0, time.UTC),
		"2026-09-25T12:00:05.5Z":         time.Date(2026, 9, 25, 12, 0, 5, 500_000_000, time.UTC),
		"2026-09-25T12:00:05.244Z":       time.Date(2026, 9, 25, 12, 0, 5, 244_000_000, time.UTC),
		"2026-09-25T12:00:05.244124Z":    time.Date(2026, 9, 25, 12, 0, 5, 244_124_000, time.UTC),
		"2026-09-25T12:00:05.123456789Z": time.Date(2026, 9, 25, 12, 0, 5, 123_456_789, time.UTC),
		"2026-09-25T22:00:05+10:00":      time.Date(2026, 9, 25, 12, 0, 5, 0, time.UTC),
	}
	for in, want := range cases {
		got, err := ParseTime(in)
		if err != nil {
			t.Errorf("ParseTime(%q): %v", in, err)
			continue
		}
		if !got.Equal(want) || got.Location() != time.UTC {
			t.Errorf("ParseTime(%q) = %v, want %v in UTC", in, got, want)
		}
		if FormatTime(got) != FormatTime(want) {
			t.Errorf("ParseTime(%q) re-formats as %q, want %q", in, FormatTime(got), FormatTime(want))
		}
	}
}

func TestParseTimeRejectsGarbage(t *testing.T) {
	for _, in := range []string{"", "garbage", "2026-09-25", "2026-09-25 12:00:05"} {
		if _, err := ParseTime(in); err == nil {
			t.Errorf("ParseTime(%q) succeeded, want an error", in)
		}
	}
}

// TestFormatTimeSortsAsText is the property #356 is about: for times inside
// one second — where every flake and mis-ordering came from — byte order of
// the stored strings is time order. The first case is the exact pair from
// the issue, which RFC3339Nano got backwards.
func TestFormatTimeSortsAsText(t *testing.T) {
	a := time.Date(2026, 9, 25, 12, 0, 12, 244_000_000, time.UTC)
	b := time.Date(2026, 9, 25, 12, 0, 12, 244_124_000, time.UTC)
	if !(a.Format(time.RFC3339Nano) > b.Format(time.RFC3339Nano)) {
		t.Fatal("setup: RFC3339Nano no longer mis-orders this pair; the regression below proves nothing")
	}
	if !(FormatTime(a) < FormatTime(b)) {
		t.Errorf("FormatTime(%v) = %q does not sort before FormatTime(%v) = %q", a, FormatTime(a), b, FormatTime(b))
	}

	r := rand.New(rand.NewPCG(356, 2))
	second := time.Date(2026, 9, 25, 12, 0, 12, 0, time.UTC)
	for round := range 200 {
		times := make([]time.Time, 20)
		for i := range times {
			ns := r.Int64N(int64(time.Second))
			switch r.IntN(4) { // make whole, milli- and microsecond values common
			case 0:
				ns = 0
			case 1:
				ns -= ns % int64(time.Millisecond)
			case 2:
				ns -= ns % int64(time.Microsecond)
			}
			times[i] = second.Add(time.Duration(ns))
		}
		byText := slices.Clone(times)
		slices.SortFunc(byText, func(x, y time.Time) int { return strings.Compare(FormatTime(x), FormatTime(y)) })
		byTime := slices.Clone(times)
		slices.SortFunc(byTime, func(x, y time.Time) int { return x.Compare(y) })
		for i := range byText {
			if !byText[i].Equal(byTime[i]) {
				t.Fatalf("round %d: text order %v != time order %v", round, byText, byTime)
			}
		}
	}
}

func TestApplyRecordsAppliedAtInTimeLayout(t *testing.T) {
	handle, _ := open(t)
	ctx := context.Background()
	if _, err := Apply(ctx, handle, []Migration{
		{Namespace: "platform", ID: "0001", Name: "first", SQL: "CREATE TABLE a (id INTEGER PRIMARY KEY);"},
	}); err != nil {
		t.Fatal(err)
	}
	var appliedAt string
	if err := handle.QueryRow(
		"SELECT applied_at FROM schema_migrations WHERE key = 'platform:0001'").Scan(&appliedAt); err != nil {
		t.Fatal(err)
	}
	if len(appliedAt) != 30 || !strings.HasSuffix(appliedAt, "Z") {
		t.Errorf("applied_at = %q, want a 30-character TimeLayout value", appliedAt)
	}
	if _, err := time.Parse(TimeLayout, appliedAt); err != nil {
		t.Errorf("applied_at %q does not parse as TimeLayout: %v", appliedAt, err)
	}
}
