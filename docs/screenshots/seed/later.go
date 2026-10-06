package main

import (
	"context"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/iliafrenkel/on-suite/internal/apps/later"
)

// laterFixture is one demo article. body names a file under fixtures/later
// ("" saves it link-only, with extractErr). Times are days before now;
// zero means never. highlights are {quote, comment} pairs, each quote found
// in the article's text.
type laterFixture struct {
	url, title, site, byline, body, extractErr string
	tags                                       []string
	saved, opened, archived                    float64
	note                                       string
	highlights                                 [][2]string
}

// laterFixtures are saved in this order, so "The case for reading slowly"
// is article 1, the one the reading-view shots open (see shotIDs in
// seed_test.go). Nothing here has images or a favicon URL: the demo server
// must never fetch from the network.
var laterFixtures = []laterFixture{
	{
		url: "https://essays.example.com/reading-slowly", title: "The case for reading slowly",
		site: "Quiet Essays", byline: "Mara Lind", body: "reading-slowly.html",
		tags: []string{"essays", "reading"}, saved: 6, opened: 0.5,
		note: "Good reminder to stop skimming. Three marks per article feels like the right limit — any more and nothing stands out.",
		highlights: [][2]string{
			{"Reading slowly is not the same as reading badly.", "This is the whole argument in one line."},
			{"A calm place to read is the first thing slow reading needs.", ""},
			{"Underlining a passage is a small act of attention", "Patience with a pencil in hand. Try it with the next long essay."},
		},
	},
	{
		url: "https://dev.example.com/small-software", title: "Notes on building small software",
		site: "Dev Notes", byline: "Lena Hart", body: "small-software.html",
		tags: []string{"essays", "work"}, saved: 20, opened: 15, archived: 10,
		note: "Re-read when tempted to add another setting. Patience over features.",
		highlights: [][2]string{
			{"Small software is software one person can hold in their head.", "A good test for anything I build at home."},
			{"Keep one way of doing each thing.", ""},
		},
	},
	{
		url: "https://astro.example.net/spring-sky", title: "A beginner's guide to the spring night sky",
		site: "Backyard Astronomy", byline: "Priya Nair", body: "spring-sky.html",
		tags: []string{"science"}, saved: 3,
		highlights: [][2]string{
			{"Give your eyes twenty minutes away from screens and street lights.", "Try this from the back yard on Friday."},
		},
	},
	{
		url: "https://news.example.com/2026/city-libraries", title: "The future of city libraries",
		extractErr: "Couldn't find an article on the page: it asks you to sign in first.",
		tags:       []string{"reading"}, saved: 2,
	},
	{
		url: "https://kitchen.example.org/sourdough-patience", title: "What a sourdough starter taught me about patience",
		site: "Home Kitchen", byline: "Tom Avery", body: "sourdough.html",
		tags: []string{"cooking"}, saved: 1,
	},
	{
		url: "https://club.example.com/short-memory", title: "Why table tennis rewards a short memory",
		site: "Club Notes", byline: "Sam Okafor", body: "short-memory.html",
		tags: []string{"sport"}, saved: 0.2,
	},
}

func seedLater(ctx context.Context, st *later.Store, userID int64, now time.Time) error {
	ago := func(days float64) time.Time { return now.Add(-time.Duration(days * float64(day))) }
	for _, f := range laterFixtures {
		html := ""
		if f.body != "" {
			b, err := fixtures.ReadFile("fixtures/later/" + f.body)
			if err != nil {
				return err
			}
			html = string(b)
		}
		st.SetClock(at(ago(f.saved)))
		art, _, err := st.Save(ctx, userID, later.NewArticle{
			URL: f.url, Title: f.title, SiteName: f.site, Byline: f.byline,
			ContentHTML: html, ExtractError: f.extractErr, Tags: f.tags,
		})
		if err != nil {
			return err
		}
		for _, h := range f.highlights {
			start, ok := runeIndex(art.ContentText, h[0])
			if !ok {
				return fmt.Errorf("seed: %q is not in %q", h[0], f.title)
			}
			end := start + utf8.RuneCountInString(h[0])
			if _, err := st.AddHighlight(ctx, art.ID, art.ContentText, start, end, h[0], h[1]); err != nil {
				return err
			}
		}
		if f.note != "" {
			if err := st.SetNote(ctx, userID, art.ID, f.note); err != nil {
				return err
			}
		}
		if f.opened > 0 {
			st.SetClock(at(ago(f.opened)))
			if err := st.MarkOpened(ctx, userID, art.ID); err != nil {
				return err
			}
		}
		if f.archived > 0 {
			st.SetClock(at(ago(f.archived)))
			if err := st.SetState(ctx, userID, art.ID, later.StateArchived); err != nil {
				return err
			}
		}
	}
	return nil
}

// runeIndex is quote's offset in text in code points, the unit highlight
// offsets use.
func runeIndex(text, quote string) (int, bool) {
	i := strings.Index(text, quote)
	if i < 0 {
		return 0, false
	}
	return utf8.RuneCountInString(text[:i]), true
}
