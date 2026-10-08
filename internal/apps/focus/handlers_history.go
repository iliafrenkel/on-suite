package focus

import (
	"fmt"
	"net/http"
	"strconv"
)

// sessionsPerPage is the History page's page size (spec: "History").
const sessionsPerPage = 50

// chartDays is how many days the History chart covers.
const chartDays = 30

type totalTile struct{ Label, Value string }

// chartBar is one already-positioned bar: html/template can't do
// arithmetic. Mirrors ON Flash's chartBar (PATTERNS.md, "Cross-app
// mirroring").
type chartBar struct {
	X, Y, Width, Height float64
	Label               string
}

type chartView struct {
	Bars          []chartBar
	Width, Height float64
	Peak          string
	Empty         bool
}

// dayRow is the chart's text alternative.
type dayRow struct{ Day, Focus string }

// timerRow is one line of "This month by timer". Pct is its share of the
// largest row, 0–100, the SVG bar's width.
type timerRow struct {
	Name, Color, Total string
	Pct                float64
}

type sessionRow struct {
	ID          int64
	When        string
	Name, Color string
	Focus       string
	Rounds      int
	Completed   bool
}

type historyView struct {
	Totals             []totalTile
	Chart              chartView
	Days               []dayRow
	Timers             []timerRow
	Sessions           []sessionRow
	Page               int
	NewerURL, OlderURL string
}

// buildChart lays out the daily focus as bars of minutes, the way ON
// Flash's buildChart lays out reviews.
func buildChart(days []DayTotal) chartView {
	const (
		height = 120.0
		barW   = 8.0
		barGap = 2.0
	)
	out := chartView{Height: height, Width: float64(len(days)) * (barW + barGap)}
	peak := 0
	for _, d := range days {
		peak = max(peak, d.Seconds)
	}
	out.Peak, out.Empty = FormatFocus(peak), peak == 0
	for i, d := range days {
		h := 0.0
		if peak > 0 {
			h = float64(d.Seconds) / float64(peak) * height
		}
		out.Bars = append(out.Bars, chartBar{
			X: float64(i) * (barW + barGap), Y: height - h, Width: barW, Height: h,
			Label: fmt.Sprintf("%s: %s", d.Day.Format("2 Jan"), FormatFocus(d.Seconds)),
		})
	}
	return out
}

func newTimerRows(totals []TimerTotal) []timerRow {
	var out []timerRow
	for _, t := range totals {
		pct := 0.0
		if totals[0].Seconds > 0 {
			pct = float64(t.Seconds) / float64(totals[0].Seconds) * 100
		}
		out = append(out, timerRow{Name: t.Name, Color: t.Color, Total: FormatFocus(t.Seconds), Pct: pct})
	}
	return out
}

// history is the History page (spec: "History").
func (a *App) history(w http.ResponseWriter, r *http.Request) {
	userID, ok := a.userID(w, r)
	if !ok {
		return
	}
	page, err := strconv.Atoi(r.URL.Query().Get("page"))
	if err != nil || page < 1 {
		page = 1
	}
	ctx, now := r.Context(), a.store.now()

	totals, err := a.store.Totals(ctx, userID, now)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	days, err := a.store.Daily(ctx, userID, chartDays, now)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	byTimer, err := a.store.ByTimer(ctx, userID, startOfMonth(now))
	if err != nil {
		a.fail(w, r, err)
		return
	}
	sessions, more, err := a.store.RecentSessions(ctx, userID, page, sessionsPerPage)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	// Past the end — a typed ?page=99, or the last session on this page
	// just deleted (#552): go to the last page that has sessions.
	if len(sessions) == 0 && page > 1 {
		n, err := a.store.SessionCount(ctx, userID)
		if err != nil {
			a.fail(w, r, err)
			return
		}
		http.Redirect(w, r, historyURL((n+sessionsPerPage-1)/sessionsPerPage), http.StatusSeeOther)
		return
	}

	view := historyView{
		Totals: []totalTile{
			{"Today", FormatFocus(totals.Today)},
			{"This week", FormatFocus(totals.Week)},
			{"This month", FormatFocus(totals.Month)},
			{"This year", FormatFocus(totals.Year)},
		},
		Chart:  buildChart(days),
		Timers: newTimerRows(byTimer),
		Page:   page,
	}
	for _, d := range days {
		view.Days = append(view.Days, dayRow{Day: d.Day.Format("Mon 2 Jan"), Focus: FormatFocus(d.Seconds)})
	}
	for _, s := range sessions {
		view.Sessions = append(view.Sessions, sessionRow{
			ID: s.ID, When: s.StartedAt.Local().Format("Mon 2 Jan 2006 · 15:04"),
			Name: s.TimerName, Color: s.Color, Focus: FormatFocus(s.FocusSeconds),
			Rounds: s.RoundsDone, Completed: s.Completed,
		})
	}
	if page > 1 {
		view.NewerURL = historyURL(page - 1)
	}
	if more {
		view.OlderURL = historyURL(page + 1)
	}
	a.render(w, r, http.StatusOK, "focus/history", "History", view)
}
