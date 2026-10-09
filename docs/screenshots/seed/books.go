package main

import (
	"context"
	"time"

	"github.com/iliafrenkel/on-suite/internal/apps/books"
)

// seedBooks gives the demo account a small library across every shelf:
// two books on the go (one on paper, one an audiobook, each with a few
// days of progress), three waiting, two finished and rated (one with a
// review) and one put down part-way, with a few tags and two books of a
// series, so each shelf and the book pane have something to show. Dates
// are offsets from now, so nothing is ever in the future.
func seedBooks(ctx context.Context, st *books.Store, userID int64, now time.Time) error {
	at := func(daysAgo int) time.Time { return now.AddDate(0, 0, -daysAgo) }
	day := func(daysAgo int) string { return at(daysAgo).Local().Format("2006-01-02") }
	type seed struct {
		in       books.BookInput
		tags     []string
		started  int    // days ago; -1 = never
		format   string // of the reading
		progress []int  // one update a day, ending yesterday: pages, or percent for audio
		finished int    // days ago; -1 = not finished
		dnf      bool
		stopped  int // where a DNF stopped; 0 = not said
		rating   int
		review   string
	}
	library := []seed{
		{in: books.BookInput{Title: "Leviathan Wakes", Authors: "James S. A. Corey", Year: 2011, Pages: 592,
			SeriesName: "The Expanse", SeriesNumber: "1",
			Description: "A detective and a ship's officer find the same missing woman at the edge of the solar system."},
			tags: []string{"sf", "space"}, started: 9, format: "paper", progress: []int{48, 120, 205, 260, 344}, finished: -1},
		{in: books.BookInput{Title: "Piranesi", Authors: "Susanna Clarke", Year: 2020, Pages: 272},
			tags: []string{"fantasy"}, started: 3, format: "audio", progress: []int{18, 41}, finished: -1},
		{in: books.BookInput{Title: "The Dispossessed", Subtitle: "An Ambiguous Utopia", Authors: "Ursula K. Le Guin",
			Year: 1974, Pages: 387}, tags: []string{"sf", "classics"}, started: -1, finished: -1},
		{in: books.BookInput{Title: "Project Hail Mary", Authors: "Andy Weir", Year: 2021, Pages: 476},
			tags: []string{"sf", "space"}, started: -1, finished: -1},
		{in: books.BookInput{Title: "Caliban's War", Authors: "James S. A. Corey", Year: 2012, Pages: 595,
			SeriesName: "The Expanse", SeriesNumber: "2"}, tags: []string{"sf", "space"}, started: -1, finished: -1},
		{in: books.BookInput{Title: "A Wizard of Earthsea", Authors: "Ursula K. Le Guin", Year: 1968, Pages: 183,
			SeriesName: "Earthsea", SeriesNumber: "1"}, tags: []string{"fantasy", "classics"},
			started: 70, format: "ebook", finished: 56, rating: 5,
			review: "Short, strange and **wise**. Ged learns that the shadow he runs from is his own.\n\nBetter on a second reading."},
		{in: books.BookInput{Title: "The Remains of the Day", Authors: "Kazuo Ishiguro", Year: 1989, Pages: 258},
			started: 35, format: "paper", finished: 20, rating: 4},
		{in: books.BookInput{Title: "Infinite Jest", Authors: "David Foster Wallace", Year: 1996, Pages: 1079},
			started: 120, format: "paper", finished: 90, dnf: true, stopped: 312},
	}
	for i, b := range library {
		// Added in this order, a day apart, before anything was started.
		st.SetClock(func() time.Time { return at(150 - i) })
		id, err := st.Create(ctx, userID, books.NewBook{BookInput: b.in, Shelf: books.ShelfWant, Tags: b.tags})
		if err != nil {
			return err
		}
		if b.started < 0 {
			continue
		}
		st.SetClock(func() time.Time { return at(b.started) })
		if err := st.StartReading(ctx, userID, id); err != nil {
			return err
		}
		if err := st.SetFormat(ctx, userID, id, b.format); err != nil {
			return err
		}
		for j, value := range b.progress {
			st.SetClock(func() time.Time { return at(len(b.progress) - j) })
			if err := st.RecordProgress(ctx, userID, id, value); err != nil {
				return err
			}
		}
		if b.finished < 0 {
			continue
		}
		st.SetClock(func() time.Time { return at(b.finished) })
		if b.dnf {
			err = st.MarkDNF(ctx, userID, id, day(b.finished), b.stopped)
		} else {
			err = st.FinishReading(ctx, userID, id, day(b.finished), b.rating)
		}
		if err != nil {
			return err
		}
		if b.review != "" {
			if err := st.SetReview(ctx, userID, id, b.review); err != nil {
				return err
			}
		}
	}
	st.SetClock(func() time.Time { return now })
	return nil
}
