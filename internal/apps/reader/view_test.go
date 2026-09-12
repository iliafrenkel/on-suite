package reader

import (
	"testing"
	"time"
)

// TestBuildLineFlagsReconstructedDays guards against the backlog line chart
// silently mixing backfilled zeros with measured values: buildChart's
// Reconstructed field has a template caveat, and buildLine's must too, or a
// fresh install's ~90 days of backfilled history plot as a flat zero line
// with no explanation before jumping to today's real backlog.
func TestBuildLineFlagsReconstructedDays(t *testing.T) {
	day := func(offset int, backlog int, reconstructed bool) DayStat {
		return DayStat{
			Day:           time.Now().AddDate(0, 0, offset),
			Backlog:       backlog,
			Reconstructed: reconstructed,
		}
	}
	value := func(d DayStat) int { return d.Backlog }

	t.Run("flags when any day is reconstructed", func(t *testing.T) {
		days := []DayStat{
			day(-2, 0, true),
			day(-1, 0, true),
			day(0, 5, false),
		}
		out := buildLine("Backlog", days, value)
		if !out.Reconstructed {
			t.Error("buildLine did not flag a series containing a reconstructed day")
		}
	})

	t.Run("does not flag an all-measured series", func(t *testing.T) {
		days := []DayStat{
			day(-2, 3, false),
			day(-1, 4, false),
			day(0, 5, false),
		}
		out := buildLine("Backlog", days, value)
		if out.Reconstructed {
			t.Error("buildLine flagged a series with no reconstructed days")
		}
	})
}
