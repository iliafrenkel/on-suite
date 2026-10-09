package main

import (
	"context"
	"time"

	"github.com/iliafrenkel/on-suite/internal/apps/books"
)

// seedBooks gives the demo account a small library across every shelf:
// two books on the go, two waiting, two finished and one put down, with a
// few tags and a series, so each shelf and the book pane have something to
// show. Dates are offsets from now, so nothing is ever in the future.
func seedBooks(ctx context.Context, st *books.Store, userID int64, now time.Time) error {
	at := func(daysAgo int) time.Time { return now.AddDate(0, 0, -daysAgo) }
	day := func(daysAgo int) string { return at(daysAgo).Local().Format("2006-01-02") }
	type seed struct {
		in       books.BookInput
		tags     []string
		started  int // days ago; -1 = never
		finished int // days ago; -1 = not finished
		dnf      bool
	}
	library := []seed{
		{books.BookInput{Title: "Leviathan Wakes", Authors: "James S. A. Corey", Year: 2011, Pages: 592,
			SeriesName: "The Expanse", SeriesNumber: "1",
			Description: "A detective and a ship's officer find the same missing woman at the edge of the solar system."},
			[]string{"sf", "space"}, 9, -1, false},
		{books.BookInput{Title: "Piranesi", Authors: "Susanna Clarke", Year: 2020, Pages: 272},
			[]string{"fantasy"}, 3, -1, false},
		{books.BookInput{Title: "The Dispossessed", Subtitle: "An Ambiguous Utopia", Authors: "Ursula K. Le Guin",
			Year: 1974, Pages: 387}, []string{"sf", "classics"}, -1, -1, false},
		{books.BookInput{Title: "Project Hail Mary", Authors: "Andy Weir", Year: 2021, Pages: 476},
			[]string{"sf", "space"}, -1, -1, false},
		{books.BookInput{Title: "A Wizard of Earthsea", Authors: "Ursula K. Le Guin", Year: 1968, Pages: 183,
			SeriesName: "Earthsea", SeriesNumber: "1"}, []string{"fantasy", "classics"}, 70, 56, false},
		{books.BookInput{Title: "The Remains of the Day", Authors: "Kazuo Ishiguro", Year: 1989, Pages: 258},
			nil, 35, 20, false},
		{books.BookInput{Title: "Infinite Jest", Authors: "David Foster Wallace", Year: 1996, Pages: 1079},
			nil, 120, 90, true},
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
		if b.finished < 0 {
			continue
		}
		st.SetClock(func() time.Time { return at(b.finished) })
		if b.dnf {
			err = st.MarkDNF(ctx, userID, id, day(b.finished), 0)
		} else {
			err = st.FinishReading(ctx, userID, id, day(b.finished), 0)
		}
		if err != nil {
			return err
		}
	}
	st.SetClock(func() time.Time { return now })
	return nil
}
