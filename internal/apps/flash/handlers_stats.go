// internal/apps/flash/handlers_stats.go
package flash

import (
	"fmt"
	"net/http"
)

// statTile is one of the plain-number tiles at the top of the stats page —
// the same minimal shape ON Reader's own stats page tiles use
// (internal/apps/reader/view.go), re-declared here since apps don't share
// code.
type statTile struct {
	Label string
	Value string
}

// chartBar is one already-positioned bar. Geometry is computed in Go
// because html/template cannot do arithmetic — mirrors
// internal/apps/reader/view.go's chartBar exactly.
type chartBar struct {
	X, Y, Width, Height float64
	Label               string
}

type chartView struct {
	Title  string
	Bars   []chartBar
	Width  float64
	Height float64
	Max    int
	Empty  bool
}

// buildChart lays out one series of daily counts, mirroring
// internal/apps/reader/view.go's buildChart exactly.
func buildChart(title string, days []DayCount, value func(DayCount) int) chartView {
	const (
		height = 120.0
		barW   = 3.0
		barGap = 2.0
	)
	out := chartView{Title: title, Height: height}
	if len(days) == 0 {
		out.Empty = true
		return out
	}
	for _, d := range days {
		if v := value(d); v > out.Max {
			out.Max = v
		}
	}
	if out.Max == 0 {
		out.Empty = true
	}
	out.Width = float64(len(days)) * (barW + barGap)
	for i, d := range days {
		v := value(d)
		h := 0.0
		if out.Max > 0 {
			h = float64(v) / float64(out.Max) * height
		}
		out.Bars = append(out.Bars, chartBar{
			X: float64(i) * (barW + barGap), Y: height - h, Width: barW, Height: h,
			Label: fmt.Sprintf("%s: %d", d.Day.Format("2 Jan"), v),
		})
	}
	return out
}

// statsView is the stats pane's data.
type statsView struct {
	Tiles []statTile
	Chart chartView
	Days  []DayCount
	Rows  []deckLoadRow
}

// deckLoadRow is one deck's line on the stats pane.
type deckLoadRow struct {
	Deck        Deck
	Mastered    int
	Due         int
	Reviews     int // reviews in the last 30 days
	CardCount   int
	MasteredPct int // Mastered / CardCount, for the SVG bar's width
	Snoozed     bool
}

func (a *App) stats(w http.ResponseWriter, r *http.Request) {
	userID, ok := a.userID(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	now := a.store.now()

	streak, err := a.store.Streak(ctx, userID, now)
	if err != nil {
		a.deps.Errors.Internal(w, r, err)
		return
	}
	rate, err := a.store.RetentionRate(ctx, userID, now.AddDate(0, 0, -29))
	if err != nil {
		a.deps.Errors.Internal(w, r, err)
		return
	}
	mastered, err := a.store.CardsMastered(ctx, userID)
	if err != nil {
		a.deps.Errors.Internal(w, r, err)
		return
	}
	due, err := a.store.CardsDueToday(ctx, userID, now)
	if err != nil {
		a.deps.Errors.Internal(w, r, err)
		return
	}
	days, err := a.store.DailyReviewCounts(ctx, userID, 30, now)
	if err != nil {
		a.deps.Errors.Internal(w, r, err)
		return
	}
	perDeck, err := a.store.PerDeckLoad(ctx, userID, now)
	if err != nil {
		a.deps.Errors.Internal(w, r, err)
		return
	}

	sums, err := a.store.DeckSummaries(ctx, userID, now)
	if err != nil {
		a.deps.Errors.Internal(w, r, err)
		return
	}
	cardCounts := make(map[int64]int, len(sums))
	reviewNow := make(map[int64]int, len(sums))
	for _, s := range sums {
		cardCounts[s.Deck.ID] = s.CardCount
		reviewNow[s.Deck.ID] = s.ReviewNow
	}
	rows := make([]deckLoadRow, len(perDeck))
	for i, l := range perDeck {
		n := cardCounts[l.Deck.ID]
		rows[i] = deckLoadRow{
			// Due comes from DeckSummary.ReviewNow, not PerDeckLoad's own Due,
			// so this row's badge matches the deck list's badge on the same
			// screen — capped by the daily review budget (plus today's
			// allowed new cards) and zeroed while the deck is snoozed.
			Deck: l.Deck, Mastered: l.Mastered, Due: reviewNow[l.Deck.ID], Reviews: l.ReviewsLast30Days,
			CardCount: n, MasteredPct: progressPercent(l.Mastered, n), Snoozed: l.Snoozed,
		}
	}

	view := statsView{
		Tiles: []statTile{
			{Label: "Streak", Value: fmt.Sprintf("%d day(s)", streak)},
			{Label: "Retention (30 days)", Value: fmt.Sprintf("%.0f%%", rate*100)},
			{Label: "Cards mastered", Value: fmt.Sprintf("%d", mastered)},
			{Label: "Cards due", Value: fmt.Sprintf("%d", due)},
		},
		Chart: buildChart("Reviews per day", days, func(d DayCount) int { return d.Count }),
		Days:  days,
		Rows:  rows,
	}
	a.renderDeckIndex(w, r, userID, http.StatusOK, deckDetailView{Mode: deckModeStats, Stats: view})
}
