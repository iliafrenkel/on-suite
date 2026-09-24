package db

import "time"

// TimeLayout is how every timestamp in the database is stored: UTC, with
// exactly nine fractional-second digits, so every value is 30 characters
// (for years 0000-9999) and text order is time order. SQLite has no time
// type, and ORDER BY, <, <=, min() and max() on these TEXT columns compare
// bytes.
//
// time.RFC3339Nano, which this suite used before #356, trims trailing
// fractional zeros: "…:05.244Z" is shorter than "…:05.244124Z", and since
// 'Z' sorts after every digit it compared as the later of the two although
// it is 124µs earlier. Date-only columns ("2006-01-02") are already fixed
// width and do not use this.
const TimeLayout = "2006-01-02T15:04:05.000000000Z07:00"

// FormatTime renders t for storage. It always converts to UTC first, so the
// zone is always the literal "Z" and the width is always 30 (for years
// 0000-9999; years >=10000 or negative give 31 characters, which the
// migrations' GLOB already requires a 4-digit year to exclude).
func FormatTime(t time.Time) string { return t.UTC().Format(TimeLayout) }

// ParseTime reads a stored timestamp back, in UTC. It accepts TimeLayout and
// also any RFC 3339 value with zero to nine fractional digits — the legacy
// RFC3339Nano strings every row held before #356's migrations rewrote them —
// because time.Parse accepts a fraction of any length after the seconds
// whatever the layout says. The error is time.Parse's own; callers wrap it
// with their package's prefix.
func ParseTime(s string) (time.Time, error) {
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return time.Time{}, err
	}
	return t.UTC(), nil
}
