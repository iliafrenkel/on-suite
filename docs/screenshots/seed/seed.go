// Command seed fills a fresh ON Suite data directory with demo content, so
// the documentation screenshots can be regenerated identically. It is local
// tooling, never part of the binary. See docs/screenshots/README.md.
//
// Every date in the content is relative to the now passed to Seed, so the
// screenshots always look fresh: notes.md writes due dates as @DUE+N (N days
// from now, negative for overdue) and the feeds write pubDates as DAY-N (N
// days before now, fractions allowed).
package main

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"time"

	"github.com/iliafrenkel/on-suite/internal/apps/flash"
	"github.com/iliafrenkel/on-suite/internal/apps/notes"
	"github.com/iliafrenkel/on-suite/internal/apps/paste"
	"github.com/iliafrenkel/on-suite/internal/apps/reader"
	"github.com/iliafrenkel/on-suite/internal/platform/app"
	"github.com/iliafrenkel/on-suite/internal/platform/auth"
	"github.com/iliafrenkel/on-suite/internal/platform/config"
	"github.com/iliafrenkel/on-suite/internal/platform/db"
)

//go:embed fixtures
var fixtures embed.FS

// Password is the demo accounts' password. It is a published local demo
// value, never a real credential.
const Password = "demo-password-not-secret"

func dbPath(dataDir string) string { return config.Config{DataDir: dataDir}.DBPath() }

var (
	dueRe = regexp.MustCompile(`@DUE([+-]\d+)`)
	dayRe = regexp.MustCompile(`DAY-(\d+(?:\.\d+)?)`)
)

const day = 24 * time.Hour

// at returns a fixed clock, for stores whose timestamps come from SetClock.
func at(t time.Time) func() time.Time { return func() time.Time { return t } }

// Seed creates the demo accounts and content and returns a live session id
// for "demo". It refuses a data dir that already holds a database.
func Seed(ctx context.Context, dataDir string, now time.Time) (string, error) {
	now = now.UTC()
	if _, err := os.Stat(dbPath(dataDir)); err == nil {
		return "", errors.New("seed: data dir already has a database; use an empty one")
	}
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return "", err
	}
	handle, err := db.Open(dbPath(dataDir))
	if err != nil {
		return "", err
	}
	defer func() { _ = handle.Close() }()

	// The same apps and migrations cmd/onsuite's openDatabase applies.
	reg, err := app.NewRegistry(flash.New(), notes.New(), paste.New(), reader.New())
	if err != nil {
		return "", err
	}
	ms, err := db.Collect(auth.Namespace, auth.Migrations())
	if err != nil {
		return "", err
	}
	appMs, err := reg.Migrations()
	if err != nil {
		return "", err
	}
	if _, err := db.Apply(ctx, handle, append(ms, appMs...)); err != nil {
		return "", err
	}

	users := auth.NewStore(handle)
	users.SetClock(at(now.Add(-60 * day)))
	hash, err := auth.HashPassword(Password)
	if err != nil {
		return "", err
	}
	demo, err := users.CreateUser(ctx, "demo", hash, true)
	if err != nil {
		return "", err
	}
	users.SetClock(at(now.Add(-45 * day)))
	sam, err := users.CreateUser(ctx, "sam", hash, false)
	if err != nil {
		return "", err
	}

	steps := []func() error{
		func() error { return seedPastes(ctx, paste.NewStore(handle), demo.ID, now, dataDir) },
		func() error { return seedNotes(ctx, notes.NewStore(handle), demo.ID, now, dataDir) },
		func() error { return seedReader(ctx, reader.NewStore(handle), demo.ID, now) },
		func() error { return seedFlash(ctx, flash.NewStore(handle), demo.ID, sam.ID, now) },
	}
	for _, step := range steps {
		if err := step(); err != nil {
			return "", err
		}
	}

	// The session runs on the real clock, not now: it has to be live for the
	// server the capture tool talks to, whatever now the content used.
	sess, err := auth.NewStore(handle).CreateSession(ctx, demo.ID)
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(dataDir, "demo-session"), []byte(sess.ID), 0o600); err != nil {
		return "", err
	}
	return sess.ID, nil
}

func seedPastes(ctx context.Context, st *paste.Store, userID int64, now time.Time, dataDir string) error {
	// Oldest first, so the list shows the shared snippet on top.
	for _, p := range []struct {
		file, title, lang string
		age               time.Duration
		share             bool
	}{
		{"league_results.py", "Table tennis league standings", "python", 20 * day, false},
		{"schema.sql", "Reading-list schema", "sql", 12 * day, false},
		{"home-server-compose.yaml", "Home server docker-compose", "yaml", 8 * day, false},
		{"deploy.sh", "Deploy to the Pi", "bash", 5 * day, false},
		{"notes.txt", "Wi-Fi and router notes", "plaintext", 2 * day, false},
		{"home-automation.json", "Home automation config", "json", 1 * day, false},
		{"family-trip-packing-list.md", "Japan trip packing list", "markdown", 6 * time.Hour, false},
		{"retry.go.txt", "Retry with backoff", "go", 3 * time.Hour, true},
	} {
		body, err := fixtures.ReadFile("fixtures/pastes/" + p.file)
		if err != nil {
			return err
		}
		st.SetClock(at(now.Add(-p.age)))
		s, err := st.Create(ctx, userID, p.title, p.lang, string(body))
		if err != nil {
			return fmt.Errorf("paste %s: %w", p.file, err)
		}
		if p.share {
			slug, err := st.Share(ctx, userID, s.ID)
			if err != nil {
				return err
			}
			// paste.go: r.PublicFunc("GET /s/{slug}", ...), mounted at /paste/.
			if err := os.WriteFile(filepath.Join(dataDir, "share-paste"), []byte("/paste/s/"+slug), 0o600); err != nil {
				return err
			}
		}
	}
	return nil
}

func seedNotes(ctx context.Context, st *notes.Store, userID int64, now time.Time, dataDir string) error {
	raw, err := fixtures.ReadFile("fixtures/notes.md")
	if err != nil {
		return err
	}
	text := dueRe.ReplaceAllStringFunc(string(raw), func(m string) string {
		n, _ := strconv.Atoi(dueRe.FindStringSubmatch(m)[1])
		return "@" + now.AddDate(0, 0, n).Format("2006-01-02")
	})
	parsed, err := notes.ParseMarkdown(text)
	if err != nil {
		return fmt.Errorf("notes fixture: %w", err)
	}
	st.SetClock(at(now.Add(-2 * time.Hour)))
	if _, err := st.ImportUnder(ctx, userID, notes.RootID, parsed); err != nil {
		return err
	}

	all, err := st.Outline(ctx, userID, notes.RootID, true, true)
	if err != nil {
		return err
	}
	byTitle := map[string]notes.Node{}
	for _, n := range all {
		byTitle[n.Title] = n
	}
	find := func(title string) (notes.Node, error) {
		n, ok := byTitle[title]
		if !ok {
			return n, fmt.Errorf("seed: notes fixture has no %q", title)
		}
		return n, nil
	}

	// A finished item put away, so the archive page has something in it, and
	// a collapsed bullet, so the outline shows one.
	offsite, err := find("Q3 planning offsite #meeting")
	if err != nil {
		return err
	}
	if err := st.SetArchived(ctx, userID, offsite.ID, true); err != nil {
		return err
	}
	ideas, err := find("Ideas")
	if err != nil {
		return err
	}
	if err := st.SetCollapsed(ctx, userID, ideas.ID, true); err != nil {
		return err
	}

	// Share the "Trip to Japan" subtree for the public-share screenshot.
	trip, err := find("Trip to Japan")
	if err != nil {
		return err
	}
	slug, err := st.Share(ctx, userID, trip.ID)
	if err != nil {
		return err
	}
	// notes app.go: r.PublicFunc("GET /s/{slug}", ...), mounted at /notes/.
	return os.WriteFile(filepath.Join(dataDir, "share-notes"), []byte("/notes/s/"+slug), 0o600)
}

func seedReader(ctx context.Context, st *reader.Store, userID int64, now time.Time) error {
	// Subscribed three weeks ago: the stats only count articles fetched
	// after a subscription was added.
	st.SetClock(at(now.Add(-21 * day)))

	ids := map[string]int64{}
	for _, name := range []string{"Tech", "Science", "Hobbies"} {
		f, err := st.CreateFolder(ctx, userID, name)
		if err != nil {
			return err
		}
		ids[name] = f.ID
	}
	for _, feed := range []struct{ file, folder string }{
		{"tech.xml", "Tech"},
		{"space.xml", "Science"},
		{"racing.xml", "Hobbies"},
		{"books.xml", "Hobbies"},
	} {
		raw, err := fixtures.ReadFile("fixtures/feeds/" + feed.file)
		if err != nil {
			return err
		}
		body := dayRe.ReplaceAllStringFunc(string(raw), func(m string) string {
			n, _ := strconv.ParseFloat(dayRe.FindStringSubmatch(m)[1], 64)
			return now.Add(-time.Duration(n * float64(day))).Format(time.RFC1123Z)
		})
		feedURL := "https://example.com/feeds/" + feed.file
		parsed, err := reader.ParseFeed([]byte(body), feedURL)
		if err != nil {
			return fmt.Errorf("feed %s: %w", feed.file, err)
		}
		folderID := ids[feed.folder]
		sub, err := st.Subscribe(ctx, userID, feedURL, &folderID)
		if err != nil {
			return err
		}
		// A year until the next fetch: the demo server must never poll
		// example.com and replace the fixtures.
		if err := st.SaveFetchResult(ctx, reader.FetchResult{
			FeedID: sub.FeedID, ResolvedURL: feedURL, Title: parsed.Title,
			SiteURL: parsed.SiteURL, Status: 200, FetchedAt: now,
			NextFetchAt: now.AddDate(1, 0, 0),
		}); err != nil {
			return err
		}
		// One item at a time, each "fetched" shortly after it was published,
		// so the stats page shows articles arriving over the fortnight.
		for _, it := range parsed.Items {
			if _, err := st.SaveItems(ctx, sub.FeedID, []reader.ParsedItem{it}, earlier(it.PublishedAt.Add(10*time.Minute), now)); err != nil {
				return err
			}
		}
	}

	// Some history: most articles older than a day are read, a few are
	// starred, so counts and stats look lived-in.
	items, err := st.ItemsForScope(ctx, userID, reader.ScopeAll, 0, reader.FilterAll, "", 200)
	if err != nil {
		return err
	}
	for i, it := range items {
		if now.Sub(it.PublishedAt) > day && i%4 != 0 {
			if err := st.SetRead(ctx, userID, it.ID, true, earlier(it.PublishedAt.Add(5*time.Hour), now)); err != nil {
				return err
			}
		}
		if i == 1 || i == 9 {
			if err := st.SetStarred(ctx, userID, it.ID, true, now); err != nil {
				return err
			}
		}
	}
	_, err = st.BackfillDailyStats(ctx)
	return err
}

func earlier(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}

type deckFixture struct {
	name, desc, color string
	cards             [][2]string // front, back; an empty back makes a cloze card
	tags              []string
}

func seedFlash(ctx context.Context, st *flash.Store, userID, samID int64, now time.Time) error {
	st.SetClock(at(now.Add(-10 * day)))
	decks := []deckFixture{
		{"Japanese travel phrases", "Enough to order ramen and find the station", "coral", [][2]string{
			{"Excuse me", "すみません (sumimasen)"},
			{"Where is the station?", "駅はどこですか (eki wa doko desu ka)"},
			{"Thank you very much", "ありがとうございます (arigatou gozaimasu)"},
			{"The bill, please", "お会計お願いします (okaikei onegaishimasu)"},
			{"Two tickets to Kyoto, please", "京都まで二枚お願いします (Kyouto made nimai onegaishimasu)"},
			{"{{c1::Konnichiwa::greeting}} means hello", ""},
		}, []string{"japanese", "travel"}},
		{"F1 circuits", "Which track is where", "teal", [][2]string{
			{"Albert Park", "Melbourne, Australia"},
			{"Suzuka", "Suzuka, Japan"},
			{"Interlagos", "São Paulo, Brazil"},
			{"Spa-Francorchamps", "Stavelot, Belgium"},
			{"Silverstone", "Northamptonshire, England"},
			{"The {{c1::Monaco}} Grand Prix runs through the streets of Monte Carlo", ""},
		}, []string{"f1"}},
		{"Go standard library", "Which package does what", "blue", [][2]string{
			{"Read a whole file into memory", "os.ReadFile"},
			{"Cancellation and deadlines", "context"},
			{"Sort a slice in place", "slices.Sort"},
			{"Structured logging", "log/slog"},
			{"{{c1::errors.Is}} checks a wrapped error chain", ""},
		}, []string{"go", "programming"}},
	}
	ratings := []int{flash.RatingGood, flash.RatingGood, flash.RatingEasy, flash.RatingHard}
	for _, d := range decks {
		cards, err := addDeck(ctx, st, userID, d)
		if err != nil {
			return err
		}
		// Two reviews each over the past week for all but the last card,
		// spread so every one of the last six days has some: stats,
		// streaks and "due" counts have something to show, and the last
		// card stays new.
		for i, card := range cards[:len(cards)-1] {
			for j, daysAgo := range []int{6 - i%3, 3 - i%3} {
				rating := ratings[(i+j)%len(ratings)]
				if _, err := st.GradeCard(ctx, userID, card.ID, rating, now.AddDate(0, 0, -daysAgo)); err != nil {
					return err
				}
			}
		}
	}

	// A deck sam has offered demo, so the home screen shows a pending gift.
	gift, err := addDeck(ctx, st, samID, deckFixture{"Table tennis serves", "Names for the serves we practise at the club", "green", [][2]string{
		{"Serve with heavy backspin that stays short", "Short backspin serve"},
		{"Serve that curves away with sidespin", "Pendulum serve"},
		{"Fast, long serve to the backhand corner", "Fast long serve"},
	}, []string{"tabletennis"}})
	if err != nil {
		return err
	}
	st.SetClock(at(now.Add(-1 * day)))
	_, err = st.ShareDeck(ctx, samID, gift[0].DeckID, userID)
	return err
}

func addDeck(ctx context.Context, st *flash.Store, userID int64, d deckFixture) ([]flash.Card, error) {
	deck, err := st.CreateDeck(ctx, userID, d.name, d.desc, d.color)
	if err != nil {
		return nil, fmt.Errorf("deck %q: %w", d.name, err)
	}
	var out []flash.Card
	for _, c := range d.cards {
		cardType := flash.CardTypeBasic
		if c[1] == "" {
			cardType = flash.CardTypeCloze
		}
		card, err := st.SaveCardForm(ctx, userID, deck.ID, 0, flash.CardForm{
			CardType: cardType, Front: c[0], Back: c[1], Tags: d.tags,
		})
		if err != nil {
			return nil, fmt.Errorf("card %q: %w", c[0], err)
		}
		out = append(out, card)
	}
	return out, nil
}
