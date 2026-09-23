package flash

import (
	"reflect"
	"testing"
)

func TestBuildSummarySegments(t *testing.T) {
	got := buildSummarySegments(ReviewTally{Reviewed: 12, Good: 9, Hard: 2, Again: 1})
	want := []summarySegment{
		{Class: "flash-seg-good", X: 0, Width: 75},
		{Class: "flash-seg-hard", X: 75, Width: 17},
		{Class: "flash-seg-again", X: 92, Width: 8},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("segments = %+v, want %+v", got, want)
	}

	sum := 0
	for _, s := range buildSummarySegments(ReviewTally{Good: 1, Easy: 1, Hard: 1}) {
		sum += s.Width
	}
	if sum != 100 {
		t.Errorf("thirds sum to %d, want exactly 100 (no gap at the end)", sum)
	}
	if segs := buildSummarySegments(ReviewTally{}); len(segs) != 0 {
		t.Errorf("empty tally = %+v, want no segments", segs)
	}
}

func TestSummaryLegend(t *testing.T) {
	if got := summaryLegend(ReviewTally{Good: 2, Easy: 1, Again: 1}); got != "2 got it · 1 easy · 1 forgot" {
		t.Errorf("legend = %q", got)
	}
	if got := summaryLegend(ReviewTally{}); got != "" {
		t.Errorf("empty legend = %q", got)
	}
}

func TestProgressPercent(t *testing.T) {
	for _, tt := range []struct{ done, total, want int }{
		{0, 0, 0}, {0, 5, 0}, {1, 3, 33}, {2, 3, 67}, {5, 5, 100},
	} {
		if got := progressPercent(tt.done, tt.total); got != tt.want {
			t.Errorf("progressPercent(%d, %d) = %d, want %d", tt.done, tt.total, got, tt.want)
		}
	}
}
