package books

// Step is one progress row as the pages-read maths sees it: where the
// reading stood, in the unit it was recorded in, and the local day
// (YYYY-MM-DD) it was recorded on.
type Step struct {
	Unit  Unit
	Value int
	Day   string
}

// ReadingLog is one reading as the pages-read maths sees it.
type ReadingLog struct {
	Pages      int    // the book's page count; 0 when unknown
	Status     Status // how it ended; StatusReading while it goes on
	FinishedOn string // YYYY-MM-DD; "" while reading, and for an undated reading
	Steps      []Step // its progress, oldest first
}

// PagesRead is how many pages one reading covered on each local day (spec
// "Derived values"). Each progress row counts only the pages above the
// highest page the reading had reached so far (its high-water mark,
// starting at 0), dated by the day the row was recorded; the mark then
// rises to it. Going backwards counts nothing, and neither does climbing
// back up to the old mark, so a corrected typo — 100, 250, 150, 180 —
// counts once: 250 pages, not 280 (decided 2026-10-09 while planning B4).
// A repeated value counts nothing either. Finishing with a page count adds
// the pages from the mark to the end on the finish date. A percentage
// converts through the page count, and counts no pages without one. Days
// with nothing are absent.
func PagesRead(r ReadingLog) map[string]int {
	out := map[string]int{}
	high := 0
	for _, s := range r.Steps {
		page := Progress{Unit: s.Unit, Value: s.Value}.In(UnitPage, r.Pages)
		if page > high {
			out[s.Day] += page - high
			high = page
		}
	}
	if r.Status == StatusFinished && r.FinishedOn != "" && r.Pages > high {
		out[r.FinishedOn] += r.Pages - high
	}
	return out
}
