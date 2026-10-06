package main

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/iliafrenkel/on-suite/internal/apps/flash"
	"github.com/iliafrenkel/on-suite/internal/apps/later"
	"github.com/iliafrenkel/on-suite/internal/apps/notes"
	"github.com/iliafrenkel/on-suite/internal/apps/paste"
	"github.com/iliafrenkel/on-suite/internal/apps/reader"
	"github.com/iliafrenkel/on-suite/internal/platform/auth"
	"github.com/iliafrenkel/on-suite/internal/platform/db"
)

func TestSeedFillsEveryApp(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	now := time.Date(2026, 9, 28, 9, 30, 0, 0, time.UTC)

	sid, err := Seed(ctx, dir, now)
	if err != nil {
		t.Fatalf("Seed: %v", err)
	}
	if sid == "" {
		t.Fatal("no session id")
	}
	if b, err := os.ReadFile(filepath.Join(dir, "demo-session")); err != nil || string(b) != sid {
		t.Fatalf("demo-session = %q, %v; want %q", b, err, sid)
	}
	sharePaste := readPath(t, dir, "share-paste", "/paste/s/")
	shareNotes := readPath(t, dir, "share-notes", "/notes/s/")

	handle, err := db.Open(dbPath(dir))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = handle.Close() }()

	// Accounts: demo is an admin with a live session, sam a regular user,
	// and both sign in with the published demo password.
	users := auth.NewStore(handle)
	demo, err := users.UserByUsername(ctx, "demo")
	if err != nil || !demo.IsAdmin {
		t.Fatalf("demo user = %+v, %v; want an admin", demo, err)
	}
	if ok, err := auth.VerifyPassword(demo.PasswordHash, Password); err != nil || !ok {
		t.Errorf("demo password does not verify: %v, %v", ok, err)
	}
	sam, err := users.UserByUsername(ctx, "sam")
	if err != nil || sam.IsAdmin {
		t.Errorf("sam user = %+v, %v; want a regular user", sam, err)
	}
	if sess, err := users.UseSession(ctx, sid); err != nil || sess.UserID != demo.ID {
		t.Fatalf("session = %+v, %v; want a live session for demo", sess, err)
	}

	// Paste: several snippets, the shared one reachable by its public slug.
	pastes := paste.NewStore(handle)
	if s, _ := pastes.List(ctx, demo.ID, 100); len(s) < 8 {
		t.Errorf("pastes = %d, want >= 8", len(s))
	}
	if s, err := pastes.ByShareSlug(ctx, strings.TrimPrefix(sharePaste, "/paste/s/")); err != nil || s.UserID != demo.ID {
		t.Errorf("shared paste = %+v, %v; want one of demo's snippets", s, err)
	}

	// Notes: the top-level outline, with done items, due dates relative to
	// now, and the shared subtree reachable by its slug.
	ns := notes.NewStore(handle)
	top, _ := ns.Children(ctx, demo.ID, notes.RootID)
	if len(top) < 3 {
		t.Errorf("top-level notes = %d, want >= 3", len(top))
	}
	outline, err := ns.Outline(ctx, demo.ID, notes.RootID, true, true)
	if err != nil {
		t.Fatal(err)
	}
	var done, overdue, dueSoon int
	for _, n := range outline {
		if n.Done {
			done++
		}
		switch {
		case n.DueOn == "":
		case n.DueOn < now.Format("2006-01-02"):
			overdue++
		case n.DueOn <= now.AddDate(0, 0, 7).Format("2006-01-02"):
			dueSoon++
		}
		if strings.Contains(n.Title, "@DUE") {
			t.Errorf("unsubstituted due placeholder in %q", n.Title)
		}
	}
	if len(outline) < 30 || done < 3 || overdue < 1 || dueSoon < 2 {
		t.Errorf("outline: %d nodes, %d done, %d overdue, %d due this week; want a lived-in outline",
			len(outline), done, overdue, dueSoon)
	}
	if n, err := ns.ByShareSlug(ctx, strings.TrimPrefix(shareNotes, "/notes/s/")); err != nil || n.Title != "Trip to Japan" {
		t.Errorf("shared note = %+v, %v; want Trip to Japan", n, err)
	}
	if archived, err := ns.Archive(ctx, demo.ID, ""); err != nil || len(archived) < 1 {
		t.Errorf("archived notes = %d, %v; want >= 1", len(archived), err)
	}

	// Reader: folders of feeds with fresh articles, some read, one starred,
	// and none of the feeds due for a poll any time soon.
	rs := reader.NewStore(handle)
	rs.SetClock(func() time.Time { return now }) // DailyStats reads the store clock
	tree, err := rs.Tree(ctx, demo.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(tree.Folders) < 2 {
		t.Errorf("reader folders = %d, want >= 2", len(tree.Folders))
	}
	all, err := rs.ItemsForScope(ctx, demo.ID, reader.ScopeAll, 0, reader.FilterAll, "", 200)
	if err != nil {
		t.Fatal(err)
	}
	unread, _ := rs.ItemsForScope(ctx, demo.ID, reader.ScopeAll, 0, reader.FilterUnread, "", 200)
	starred, _ := rs.ItemsForScope(ctx, demo.ID, reader.ScopeStarred, 0, reader.FilterAll, "", 200)
	if len(all) < 24 || len(unread) == 0 || len(unread) == len(all) || len(starred) < 1 {
		t.Errorf("reader items: %d total, %d unread, %d starred; want a mix", len(all), len(unread), len(starred))
	}
	for _, it := range all {
		if it.PublishedAt.After(now) || it.PublishedAt.Before(now.AddDate(0, 0, -30)) {
			t.Errorf("item %q published %v; want within the month before now", it.Title, it.PublishedAt)
		}
	}
	if due, err := rs.DueFeeds(ctx, now.AddDate(0, 6, 0), 100); err != nil || len(due) != 0 {
		t.Errorf("due feeds six months on = %d, %v; want none (the demo must never poll example.com)", len(due), err)
	}
	if days, err := rs.DailyStats(ctx, demo.ID, 14); err != nil || !anyReadDay(days) {
		t.Errorf("reader daily stats = %+v, %v; want some reading history", days, err)
	}

	// Later: articles in every state, one link-only, highlights and a
	// note — and nothing for the image job to fetch: the demo must never
	// reach the network.
	ls := later.NewStore(handle)
	counts, err := ls.Counts(ctx, demo.ID, "")
	if err != nil || counts[later.StateUnread] < 3 || counts[later.StateReading] < 1 || counts[later.StateArchived] < 1 {
		t.Errorf("later counts = %v, %v; want unread >= 3, reading >= 1, archived >= 1", counts, err)
	}
	if hl, err := ls.Highlights(ctx, 1); err != nil || len(hl) < 3 {
		t.Errorf("later article 1 highlights = %d, %v; want >= 3", len(hl), err)
	}
	if imgs, err := ls.ImagesToFetch(ctx, 100); err != nil || len(imgs) != 0 {
		t.Errorf("later images to fetch = %d, %v; want none", len(imgs), err)
	}

	// Flash: decks with cards, a review history and a streak.
	fs := flash.NewStore(handle)
	decks, _ := fs.ListDecks(ctx, demo.ID)
	if len(decks) < 3 {
		t.Errorf("decks = %d, want >= 3", len(decks))
	}
	for _, d := range decks {
		if cards, err := fs.ListCards(ctx, demo.ID, d.ID); err != nil || len(cards) < 3 {
			t.Errorf("deck %q cards = %d, %v; want >= 3", d.Name, len(cards), err)
		}
	}
	if streak, err := fs.Streak(ctx, demo.ID, now); err != nil || streak < 3 {
		t.Errorf("flash streak = %d, %v; want >= 3", streak, err)
	}
}

// shotIDs is what each seeded row ID used in a URL in
// ../capture/shots.go must be, keyed by app. The seed hands out IDs in
// fixture order, so reordering or adding fixtures can shift them; this map
// makes that a test failure instead of a silently wrong screenshot. Change
// it together with shots.go.
var shotIDs = map[string]map[int64]string{
	"paste":  {3: "Home server docker-compose", 7: "Japan trip packing list", 8: "Retry with backoff"},
	"notes":  {25: "Before we go"},
	"reader": {10: "The Orionids peak this month: how to watch"},
	"flash":  {1: "Japanese travel phrases", 2: "F1 circuits"},
	"later":  {1: "The case for reading slowly"},
}

// shotIDRe finds the seeded IDs in shots.go URLs: /paste/3, /notes/25,
// /reader/item/10, /flash/2/cards/, /flash/review/2, /later/a/1.
var shotIDRe = regexp.MustCompile(`URL: "/(paste|notes|reader/item|flash(?:/review)?|later/a)/(\d+)`)

func TestShotIDsPointAtTheIntendedItems(t *testing.T) {
	src, err := os.ReadFile("../capture/shots.go")
	if err != nil {
		t.Fatal(err)
	}
	matches := shotIDRe.FindAllStringSubmatch(string(src), -1)
	if len(matches) == 0 {
		t.Fatal("found no seeded IDs in shots.go; has the URL format changed?")
	}
	for _, m := range matches {
		appID := strings.SplitN(m[1], "/", 2)[0]
		id, _ := strconv.ParseInt(m[2], 10, 64)
		if _, ok := shotIDs[appID][id]; !ok {
			t.Errorf("shots.go uses %s ID %d, which shotIDs doesn't list; add what it should show", appID, id)
		}
	}

	ctx := context.Background()
	dir := t.TempDir()
	if _, err := Seed(ctx, dir, time.Date(2026, 9, 28, 9, 30, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	handle, err := db.Open(dbPath(dir))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = handle.Close() }()
	demo, err := auth.NewStore(handle).UserByUsername(ctx, "demo")
	if err != nil {
		t.Fatal(err)
	}

	lookup := map[string]func(int64) (string, error){
		"paste": func(id int64) (string, error) {
			s, err := paste.NewStore(handle).ByID(ctx, demo.ID, id)
			return s.Title, err
		},
		"notes": func(id int64) (string, error) {
			n, err := notes.NewStore(handle).ByID(ctx, demo.ID, id)
			return n.Title, err
		},
		"reader": func(id int64) (string, error) {
			it, err := reader.NewStore(handle).Item(ctx, demo.ID, id)
			return it.Title, err
		},
		"later": func(id int64) (string, error) {
			a, err := later.NewStore(handle).Article(ctx, demo.ID, id)
			return a.Title, err
		},
		"flash": func(id int64) (string, error) {
			d, err := flash.NewStore(handle).DeckByID(ctx, demo.ID, id)
			return d.Name, err
		},
	}
	for appID, ids := range shotIDs {
		for id, want := range ids {
			if got, err := lookup[appID](id); err != nil || got != want {
				t.Errorf("%s ID %d = %q, %v; shots.go expects %q", appID, id, got, err, want)
			}
		}
	}
}

func TestSeedRefusesANonEmptyDataDir(t *testing.T) {
	dir := t.TempDir()
	if _, err := Seed(context.Background(), dir, time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err := Seed(context.Background(), dir, time.Now()); err == nil {
		t.Fatal("second Seed into the same dir succeeded; want an error")
	}
}

func readPath(t *testing.T, dir, name, prefix string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil || !strings.HasPrefix(string(b), prefix) || len(b) <= len(prefix) {
		t.Fatalf("%s = %q, %v; want a URL path under %s", name, b, err, prefix)
	}
	return string(b)
}

func anyReadDay(days []reader.DayStat) bool {
	for _, d := range days {
		if d.Read > 0 {
			return true
		}
	}
	return false
}
