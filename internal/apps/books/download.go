package books

import (
	"net/http"
	"slices"
	"strconv"
	"strings"
)

// exportMarkdown is the whole library as one Markdown file (spec "Export
// (B5)"): a section per book, by title — its details, readings, rating and
// description, review, notes and quotes (the description too, decided
// 2026-10-10 while planning B5). today is the date it says it was made
// ("10 Oct 2026").
func exportMarkdown(p exportPayload, today string) string {
	var b strings.Builder
	b.WriteString("# My books\n\nExported from ON Books on " + today + ": " + countText(len(p.Books), "book", "books") + ".\n")
	if len(p.Goals) > 0 {
		var goals []string
		for _, g := range p.Goals {
			goals = append(goals, strconv.Itoa(g.Year)+": "+countText(g.Target, "book", "books"))
		}
		b.WriteString("\nReading goals — " + strings.Join(goals, " · ") + ".\n")
	}
	list := slices.Clone(p.Books)
	slices.SortStableFunc(list, func(x, y exportedBook) int {
		return strings.Compare(strings.ToLower(x.Title), strings.ToLower(y.Title))
	})
	for _, bk := range list {
		writeBook(&b, bk)
	}
	return b.String()
}

func writeBook(b *strings.Builder, bk exportedBook) {
	b.WriteString("\n## " + bk.Title + "\n\n")
	if bk.Subtitle != "" {
		b.WriteString("*" + bk.Subtitle + "*\n\n")
	}
	item := func(label, value string) {
		if value != "" {
			b.WriteString("- " + label + ": " + value + "\n")
		}
	}
	item("Author", bk.Authors)
	item("Series", seriesText(bk.SeriesName, bk.SeriesNumber))
	item("First published", numText(bk.Year))
	item("Pages", numText(bk.Pages))
	item("ISBN", bk.ISBN13)
	item("Shelf", bk.Shelf.Label())
	item("Tags", strings.Join(bk.Tags, ", "))
	if bk.Rating > 0 {
		item("Rating", stars(bk.Rating)+" ("+strconv.Itoa(bk.Rating)+" of 5)")
	}
	item("Added", bk.AddedAt.Local().Format("2 Jan 2006"))
	if bk.Description != "" {
		b.WriteString("\n### Description\n\n" + bk.Description + "\n")
	}

	if len(bk.Readings) > 0 {
		b.WriteString("\n### Readings\n\n")
		for i := len(bk.Readings) - 1; i >= 0; i-- { // newest first, as the book pane
			rd := bk.Readings[i]
			parts := []string{statusLabels[rd.Status]}
			if rd.Format != "" {
				parts = append(parts, formatLabels[rd.Format])
			}
			parts = append(parts, readingDates(Reading{StartedOn: rd.StartedOn, FinishedOn: rd.FinishedOn}))
			b.WriteString("- " + strings.Join(parts, " · ") + "\n")
		}
	}
	if bk.Review != "" {
		b.WriteString("\n### Review\n\n" + bk.Review + "\n")
	}
	if len(bk.Notes) > 0 {
		b.WriteString("\n### Notes\n")
		for _, n := range bk.Notes {
			head := n.CreatedAt.Local().Format("2 Jan 2006")
			if n.Page > 0 {
				head += " · " + pageText(n.Page)
			}
			b.WriteString("\n**" + head + "**\n\n" + n.Body + "\n")
		}
	}
	if len(bk.Quotes) > 0 {
		b.WriteString("\n### Quotes\n")
		for _, q := range bk.Quotes {
			b.WriteString("\n> " + strings.ReplaceAll(q.Text, "\n", "\n> ") + "\n")
			if q.Page > 0 {
				b.WriteString("\n— " + pageText(q.Page) + "\n")
			}
			if q.Comment != "" {
				b.WriteString("\n" + q.Comment + "\n")
			}
		}
	}
}

// download is the sidebar's Export as Markdown: the whole library as
// books-export.md. The name is generic on purpose, as notes-export.md is:
// never derived from a title.
func (a *App) download(w http.ResponseWriter, r *http.Request) {
	uid, ok := a.userID(w, r)
	if !ok {
		return
	}
	p, err := a.store.Export(r.Context(), uid)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="books-export.md"`)
	_, _ = w.Write([]byte(exportMarkdown(p, ShowDay(a.store.Today()))))
}
