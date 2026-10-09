package books

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// parseYear reads the Stats page's year: "" is this year; anything but a
// whole number from MinYear to next year is not ok. Next year is there so
// its goal can be set ahead (decided 2026-10-09 while planning B4).
func parseYear(s string, thisYear int) (int, bool) {
	if s == "" {
		return thisYear, true
	}
	y, err := strconv.Atoi(s)
	if err != nil || y < MinYear || y > thisYear+1 {
		return 0, false
	}
	return y, true
}

// statsURL is the Stats page for a year.
func statsURL(year int) string { return "/books/stats?year=" + strconv.Itoa(year) }

// stats is the Stats page (spec "Stats (B4)"), for ?year= or this year.
// A year it can't show is a 404, as a book that isn't there is.
func (a *App) stats(w http.ResponseWriter, r *http.Request) {
	uid, ok := a.userID(w, r)
	if !ok {
		return
	}
	year, ok := parseYear(r.URL.Query().Get("year"), a.store.now().Local().Year())
	if !ok {
		a.deps.Errors.Status(w, r, http.StatusNotFound)
		return
	}
	a.renderStats(w, r, uid, year, http.StatusOK, goalDraft{})
}

// goalDraft is a goal the store refused, to show again in the goal form
// with its message and what was typed (spec "Errors": an inline message).
type goalDraft struct{ Error, Value string }

// renderStats draws the Stats page for year; a refused goal comes back
// in its form, with status 422.
func (a *App) renderStats(w http.ResponseWriter, r *http.Request, userID int64, year, status int, d goalDraft) {
	ctx := r.Context()
	s, err := a.store.Stats(ctx, userID, year)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	g, err := a.store.Goal(ctx, userID, year)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	today := a.store.now().Local()
	v := viewStats(s, today)
	v.Goal = viewGoal(g, today)
	if d.Error != "" {
		v.Goal.Error, v.Goal.Value = d.Error, d.Value
	}
	page := a.deps.Page(r, "Reading stats")
	page.Data = v
	if err := a.deps.Render.Page(w, status, "books/stats", page); err != nil {
		a.deps.Errors.Internal(w, r, err)
	}
}

// goalYear reads a goal form's year; one the Stats page can't show is a
// tampered form, a 400.
func (a *App) goalYear(w http.ResponseWriter, r *http.Request) (int, bool) {
	year, ok := parseYear(r.PostFormValue("year"), a.store.now().Local().Year())
	if !ok {
		a.deps.Errors.Status(w, r, http.StatusBadRequest)
	}
	return year, ok
}

// setGoal sets or changes the year's goal from the Stats page and goes
// back to it; a target the store refuses comes back inline, with what was
// typed. A plain form post: the page works without JavaScript.
func (a *App) setGoal(w http.ResponseWriter, r *http.Request) {
	uid, ok := a.userID(w, r)
	if !ok {
		return
	}
	year, ok := a.goalYear(w, r)
	if !ok {
		return
	}
	typed := strings.TrimSpace(r.PostFormValue("target"))
	target, err := strconv.Atoi(typed)
	if err != nil {
		target = -1 // refused with the store's own message
	}
	err = a.store.SetGoal(r.Context(), uid, year, target)
	var ref *Refusal
	switch {
	case errors.As(err, &ref):
		a.renderStats(w, r, uid, year, http.StatusUnprocessableEntity, goalDraft{Error: ref.Msg, Value: typed})
		return
	case err != nil:
		a.fail(w, r, err)
		return
	}
	http.Redirect(w, r, statsURL(year), http.StatusSeeOther)
}

// clearGoal removes the year's goal and goes back to the Stats page.
func (a *App) clearGoal(w http.ResponseWriter, r *http.Request) {
	uid, ok := a.userID(w, r)
	if !ok {
		return
	}
	year, ok := a.goalYear(w, r)
	if !ok {
		return
	}
	if err := a.store.ClearGoal(r.Context(), uid, year); err != nil {
		a.fail(w, r, err)
		return
	}
	http.Redirect(w, r, statsURL(year), http.StatusSeeOther)
}

// goalView is a year's goal: on the Stats page with its form, and as the
// read-only card at the top of the Reading shelf.
type goalView struct {
	Year    int
	Target  int    // 0: no goal
	Text    string // "12 of 30"
	Pace    string // "2 ahead", "goal reached"; "" for none
	Percent int    // the bar, 0–100
	Value   string // the target box
	Max     int
	Error   string
}

// viewGoal draws g as of today, a local time.
func viewGoal(g Goal, today time.Time) goalView {
	v := goalView{Year: g.Year, Target: g.Target, Max: MaxGoal}
	if g.Target > 0 {
		v.Text = strconv.Itoa(g.Done) + " of " + strconv.Itoa(g.Target)
		v.Pace = g.Pace(today)
		v.Percent = min(100, g.Done*100/g.Target)
		v.Value = strconv.Itoa(g.Target)
	}
	return v
}

// statsView is the Stats page.
type statsView struct {
	Year     int
	Years    []yearLink // the year picker
	Goal     goalView
	Tiles    []statTile // the year's numbers
	Formats  []statTile // the year's finished readings by format
	Lengths  []lengthLine
	Months   chartView
	AllTiles []statTile
	PerYear  chartView
}

// yearLink is one year in the picker.
type yearLink struct {
	Year    int
	URL     string
	Current bool
}

type statTile struct{ Label, Value string }

// lengthLine is the year's longest or shortest book.
type lengthLine struct{ Label, Title, URL, Pages string }

// viewStats draws s as of today, a local time.
func viewStats(s Stats, today time.Time) statsView {
	y := s.Year
	v := statsView{Year: y.Year,
		Tiles:    statTiles(y.Finished, y.Pages, y.Rating),
		AllTiles: statTiles(s.All.Finished, s.All.Pages, s.All.Rating)}
	// The picker runs from the first year with any reading to next year
	// (decided 2026-10-09 while planning B4), and takes in a year typed
	// into the address bar before that.
	for year := min(s.All.First, y.Year); year <= today.Year()+1; year++ {
		v.Years = append(v.Years, yearLink{Year: year, URL: statsURL(year), Current: year == y.Year})
	}
	for _, f := range y.Formats {
		v.Formats = append(v.Formats, statTile{Label: formatLabels[f.Format], Value: strconv.Itoa(f.N)})
	}
	if y.Longest.ID != 0 {
		v.Lengths = append(v.Lengths, lengthOf("Longest", y.Longest))
	}
	if y.Shortest.ID != 0 && y.Shortest.ID != y.Longest.ID {
		v.Lengths = append(v.Lengths, lengthOf("Shortest", y.Shortest))
	}
	var months []chartPoint
	for i, n := range y.ByMonth {
		m := time.Month(i + 1)
		months = append(months, chartPoint{Tick: m.String()[:3], Name: m.String(), N: n})
	}
	v.Months = buildChart(months)
	var years []chartPoint
	for _, c := range s.All.ByYear {
		years = append(years, chartPoint{Tick: yearTick(c.Year), Name: strconv.Itoa(c.Year), N: c.N})
	}
	v.PerYear = buildChart(years)
	return v
}

// statTiles are a span's three headline numbers.
func statTiles(finished, pages int, rating float64) []statTile {
	return []statTile{
		{Label: "Books finished", Value: strconv.Itoa(finished)},
		{Label: "Pages read", Value: groupDigits(pages)},
		{Label: "Average rating", Value: ratingText(rating)},
	}
}

// ratingText is an average rating to one decimal place, "—" for none.
func ratingText(r float64) string {
	if r == 0 {
		return "—"
	}
	return strconv.FormatFloat(r, 'f', 1, 64) + " ★"
}

// groupDigits writes n with thousands separators: 12345 → "12,345".
func groupDigits(n int) string {
	s := strconv.Itoa(n)
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "," + s[i:]
	}
	return s
}

func lengthOf(label string, b BookPages) lengthLine {
	return lengthLine{Label: label, Title: b.Title, URL: listCtx{Shelf: ShelfAll}.BookURL(b.ID),
		Pages: groupDigits(b.Pages) + " pages"}
}

// yearTick labels a year's bar "’24": a slot is a twelfth of the chart
// or less, and at phone width "2024" doesn't fit in one. The tooltip and
// the text alternative have the whole year.
func yearTick(year int) string { return fmt.Sprintf("’%02d", year%100) }

// chartPoint is one bar's data: its tick (the label under it), its name
// in the tooltip and the text alternative, and how many books it stands
// for.
type chartPoint struct {
	Tick, Name string
	N          int
}

// chartBar is one already-positioned bar: html/template can't do
// arithmetic. Mirrors ON Focus's chartBar (PATTERNS.md, "Cross-app
// mirroring").
type chartBar struct {
	X, Y, Width, Height float64
	Label               string
}

type chartView struct {
	Bars          []chartBar
	Ticks         []string     // under the bars, one per slot
	Points        []chartPoint // the text alternative
	Width, Height float64
	Peak          int
	Empty         bool
}

// minSlots is the fewest bar slots a chart has: twelve, a year's months,
// so a first year of reading isn't one bar the width of the page.
const minSlots = 12

// buildChart lays out one bar per point, each in an equal slot so the
// ticks below (an equal-width flex row) line up with them; with fewer
// points than minSlots the slots left over stay empty. Mirrors ON Focus's
// buildChart.
func buildChart(points []chartPoint) chartView {
	const (
		height = 100.0
		slot   = 10.0
		barW   = 7.0
	)
	slots := max(len(points), minSlots)
	out := chartView{Points: points, Height: height, Width: float64(slots) * slot}
	for _, p := range points {
		out.Peak = max(out.Peak, p.N)
	}
	out.Empty = out.Peak == 0
	for i, p := range points {
		h := 0.0
		if out.Peak > 0 {
			h = float64(p.N) / float64(out.Peak) * height
		}
		out.Bars = append(out.Bars, chartBar{
			X: float64(i)*slot + (slot-barW)/2, Y: height - h, Width: barW, Height: h,
			Label: p.Name + ": " + countText(p.N, "book", "books"),
		})
		out.Ticks = append(out.Ticks, p.Tick)
	}
	for len(out.Ticks) < slots {
		out.Ticks = append(out.Ticks, "")
	}
	return out
}
