package books_test

import (
	"maps"
	"testing"

	"github.com/iliafrenkel/on-suite/internal/apps/books"
)

func page(day string, n int) books.Step {
	return books.Step{Unit: books.UnitPage, Value: n, Day: day}
}

func percent(day string, n int) books.Step {
	return books.Step{Unit: books.UnitPercent, Value: n, Day: day}
}

func TestPagesRead(t *testing.T) {
	const d1, d2, d3 = "2026-03-01", "2026-03-02", "2026-03-03"
	tests := []struct {
		name string
		r    books.ReadingLog
		want map[string]int
	}{
		{"no progress", books.ReadingLog{Pages: 300, Status: books.StatusReading}, map[string]int{}},
		{"deltas from page 0, by day",
			books.ReadingLog{Pages: 300, Status: books.StatusReading, Steps: []books.Step{page(d1, 40), page(d1, 70), page(d2, 120)}},
			map[string]int{d1: 70, d2: 50}},
		{"going backwards counts nothing; climbing back up to the old mark counts nothing either",
			books.ReadingLog{Pages: 300, Status: books.StatusReading, Steps: []books.Step{page(d1, 100), page(d2, 60), page(d3, 90)}},
			map[string]int{d1: 100}},
		{"a corrected typo counts once: 100, 250, 150, 180 is 250 pages, not 280",
			books.ReadingLog{Pages: 600, Status: books.StatusReading, Steps: []books.Step{page(d1, 100), page(d1, 250), page(d2, 150), page(d3, 180)}},
			map[string]int{d1: 250}},
		{"back, then up past the old mark: only the pages above it count",
			books.ReadingLog{Pages: 300, Status: books.StatusReading, Steps: []books.Step{page(d1, 100), page(d2, 60), page(d3, 130)}},
			map[string]int{d1: 100, d3: 30}},
		{"finishing after going back adds the pages from the mark, not from the last row",
			books.ReadingLog{Pages: 300, Status: books.StatusFinished, FinishedOn: d3, Steps: []books.Step{page(d1, 280), page(d2, 200)}},
			map[string]int{d1: 280, d3: 20}},
		{"a repeated value adds nothing (#576)",
			books.ReadingLog{Pages: 300, Status: books.StatusReading, Steps: []books.Step{page(d1, 100), page(d2, 100)}},
			map[string]int{d1: 100}},
		{"finishing adds the remainder on the finish date",
			books.ReadingLog{Pages: 300, Status: books.StatusFinished, FinishedOn: d3, Steps: []books.Step{page(d1, 250)}},
			map[string]int{d1: 250, d3: 50}},
		{"finishing with no progress is the whole book",
			books.ReadingLog{Pages: 300, Status: books.StatusFinished, FinishedOn: d2},
			map[string]int{d2: 300}},
		{"finishing at the last page adds nothing more",
			books.ReadingLog{Pages: 300, Status: books.StatusFinished, FinishedOn: d2, Steps: []books.Step{page(d1, 300)}},
			map[string]int{d1: 300}},
		{"finishing without a page count adds nothing",
			books.ReadingLog{Status: books.StatusFinished, FinishedOn: d2},
			map[string]int{}},
		{"an undated finish adds nothing",
			books.ReadingLog{Pages: 300, Status: books.StatusFinished},
			map[string]int{}},
		{"did not finish: the pages read, no remainder",
			books.ReadingLog{Pages: 300, Status: books.StatusDNF, FinishedOn: d2, Steps: []books.Step{page(d1, 80)}},
			map[string]int{d1: 80}},
		{"percent converts through the page count",
			books.ReadingLog{Pages: 400, Status: books.StatusFinished, FinishedOn: d3, Steps: []books.Step{percent(d1, 25), percent(d2, 50)}},
			map[string]int{d1: 100, d2: 100, d3: 200}},
		{"percent without a page count counts nothing",
			books.ReadingLog{Status: books.StatusReading, Steps: []books.Step{percent(d1, 25), percent(d2, 50)}},
			map[string]int{}},
		{"a format change mid-reading: pages, then percent",
			books.ReadingLog{Pages: 200, Status: books.StatusReading, Steps: []books.Step{page(d1, 50), percent(d2, 50)}},
			map[string]int{d1: 50, d2: 50}},
	}
	for _, tt := range tests {
		if got := books.PagesRead(tt.r); !maps.Equal(got, tt.want) {
			t.Errorf("%s: PagesRead = %v, want %v", tt.name, got, tt.want)
		}
	}
}

func TestPagesReadCountsEachReadingFromTheStart(t *testing.T) {
	// A re-read is its own reading: it starts again from page 0, so its
	// pages count again rather than against the first reading's last page.
	first := books.ReadingLog{Pages: 100, Status: books.StatusFinished, FinishedOn: "2025-05-01",
		Steps: []books.Step{page("2025-04-01", 60)}}
	again := books.ReadingLog{Pages: 100, Status: books.StatusReading,
		Steps: []books.Step{page("2026-02-01", 30)}}
	if got := books.PagesRead(first); !maps.Equal(got, map[string]int{"2025-04-01": 60, "2025-05-01": 40}) {
		t.Errorf("first reading = %v", got)
	}
	if got := books.PagesRead(again); !maps.Equal(got, map[string]int{"2026-02-01": 30}) {
		t.Errorf("re-read = %v, want 30 pages from page 0", got)
	}
}
