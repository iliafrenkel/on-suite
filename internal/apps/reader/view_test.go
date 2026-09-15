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

func TestViewTreeHideReadFiltersZeroUnreadFeedsAndEmptyFolders(t *testing.T) {
	folder := TreeFolder{
		Folder: Folder{ID: 1, Name: "Blogs"},
		Subs: []Subscription{
			{ID: 10, Title: "Read feed"},
			{ID: 11, Title: "Unread feed"},
		},
	}
	emptyFolder := TreeFolder{
		Folder: Folder{ID: 2, Name: "All caught up"},
		Subs: []Subscription{
			{ID: 12, Title: "Also read"},
		},
	}
	tree := Tree{
		Folders: []TreeFolder{folder, emptyFolder},
		Root: []Subscription{
			{ID: 20, Title: "Root read"},
			{ID: 21, Title: "Root unread"},
		},
	}
	counts := Counts{BySub: map[int64]int{
		10: 0, 11: 3, 12: 0, 20: 0, 21: 2,
	}}

	t.Run("hideRead false keeps everything", func(t *testing.T) {
		out := viewTree(tree, 0, ScopeAll, counts, false)
		if len(out.Folders) != 2 || len(out.Folders[0].Subs) != 2 || len(out.Folders[1].Subs) != 1 {
			t.Fatalf("hideRead=false changed the tree shape: %+v", out.Folders)
		}
		if len(out.Root) != 2 {
			t.Fatalf("hideRead=false changed root: %+v", out.Root)
		}
	})

	t.Run("hideRead true drops zero-unread subs and empty folders", func(t *testing.T) {
		out := viewTree(tree, 0, ScopeAll, counts, true)
		if len(out.Folders) != 1 || out.Folders[0].ID != 1 {
			t.Fatalf("empty folder was not dropped: %+v", out.Folders)
		}
		if len(out.Folders[0].Subs) != 1 || out.Folders[0].Subs[0].ID != 11 {
			t.Fatalf("read feed was not filtered from the surviving folder: %+v", out.Folders[0].Subs)
		}
		if len(out.Root) != 1 || out.Root[0].ID != 21 {
			t.Fatalf("root feeds were not filtered: %+v", out.Root)
		}
	})

	t.Run("hideRead true keeps the active feed even at zero unread", func(t *testing.T) {
		out := viewTree(tree, 10, ScopeAll, counts, true)
		if len(out.Folders) != 1 || len(out.Folders[0].Subs) != 2 {
			t.Fatalf("active read feed was hidden: %+v", out.Folders)
		}
	})

	t.Run("Empty reflects real subscriptions, not the filtered view", func(t *testing.T) {
		out := viewTree(tree, 0, ScopeAll, counts, true)
		if out.Empty {
			t.Error("Empty is true even though real subscriptions exist, just all currently read")
		}
	})
}
