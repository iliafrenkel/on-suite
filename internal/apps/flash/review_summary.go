package flash

import (
	"fmt"
	"strings"
)

// reviewSummary is the end-of-session screen (UI overhaul spec §5).
type reviewSummary struct {
	Reviewed int
	Segments []summarySegment
	Legend   string
	Streak   int
	NextName string // "" when no other deck has cards to review
	NextDue  int
	NextURL  string
}

// summarySegment is one coloured part of the summary's breakdown bar, in
// SVG user units of a 100-wide viewBox. Geometry is computed here because
// the CSP forbids style="" and templates can't do arithmetic.
type summarySegment struct {
	Class    string
	X, Width int
}

// summaryParts is the breakdown's fixed order and wording: best first.
func summaryParts(t ReviewTally) []struct {
	class, label string
	n            int
} {
	return []struct {
		class, label string
		n            int
	}{
		{"flash-seg-good", "got it", t.Good},
		{"flash-seg-easy", "easy", t.Easy},
		{"flash-seg-hard", "hard", t.Hard},
		{"flash-seg-again", "forgot", t.Again},
	}
}

// buildSummarySegments lays the four rating counts end to end across 100
// units. Edges are rounded from the running total, so the widths always
// add up to exactly 100.
func buildSummarySegments(t ReviewTally) []summarySegment {
	total := t.Good + t.Easy + t.Hard + t.Again
	if total == 0 {
		return nil
	}
	var out []summarySegment
	cum, x := 0, 0
	for _, p := range summaryParts(t) {
		if p.n == 0 {
			continue
		}
		cum += p.n
		end := (cum*100 + total/2) / total
		out = append(out, summarySegment{Class: p.class, X: x, Width: end - x})
		x = end
	}
	return out
}

// summaryLegend is the breakdown in words, e.g. "9 got it · 2 hard".
func summaryLegend(t ReviewTally) string {
	var parts []string
	for _, p := range summaryParts(t) {
		if p.n > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", p.n, p.label))
		}
	}
	return strings.Join(parts, " · ")
}

// progressPercent is done/total as a rounded percentage, for the review
// screen's progress bar.
func progressPercent(done, total int) int {
	if total <= 0 {
		return 0
	}
	return (done*100 + total/2) / total
}
