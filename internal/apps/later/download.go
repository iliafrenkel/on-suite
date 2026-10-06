package later

import (
	"net/http"
	"strings"
)

// imageLink maps a snapshot's /later/img/{hash} src to the image's source
// URL, for Markdown's image links; "" when the hash isn't the article's.
func imageLink(sources map[string]string) func(src string) string {
	return func(src string) string {
		if hash, ok := strings.CutPrefix(src, ImagePathPrefix); ok {
			return sources[hash]
		}
		return ""
	}
}

// articleMarkdown is the "Download as Markdown" file (spec: "Export"):
// title, where it came from, the note, the highlights with their comments,
// then the article. The note and comments go in as written — they are the
// user's own words and may already be Markdown — while everything taken
// from the page is escaped.
func articleMarkdown(art Article, tags []string, hls []Highlight, sources map[string]string) string {
	parts := []string{"# " + mdEscape(mdOneLine(art.Title))}

	meta := []string{"Source: <" + art.URL + ">"}
	if b := mdOneLine(art.Byline); b != "" {
		meta = append(meta, "Author: "+mdEscape(b))
	}
	meta = append(meta, "Saved: "+art.SavedAt.Local().Format("2 January 2006"))
	if len(tags) > 0 {
		meta = append(meta, "Tags: "+mdEscape(strings.Join(tags, ", ")))
	}
	parts = append(parts, strings.Join(meta, "\\\n"))

	if art.Note != "" {
		parts = append(parts, "## Note", art.Note)
	}
	if len(hls) > 0 {
		parts = append(parts, "## Highlights")
		for _, h := range hls {
			parts = append(parts, mdPrefix(mdParagraph(mdEscape(h.Quote)), "> ", ">"))
			if h.Comment != "" {
				parts = append(parts, h.Comment)
			}
		}
	}
	if body := htmlToMarkdown(art.ContentHTML, imageLink(sources), 2); body != "" {
		parts = append(parts, "## Article", body)
	}
	return strings.Join(parts, "\n\n") + "\n"
}

// markdown downloads one article as later-article.md: a generic name, as
// everywhere in the suite, never one built from the title. Downloading
// isn't reading, so it doesn't mark the article opened.
func (a *App) markdown(w http.ResponseWriter, r *http.Request) {
	userID, ok := a.userID(w, r)
	if !ok {
		return
	}
	id, ok := a.pathID(w, r)
	if !ok {
		return
	}
	art, err := a.store.Article(r.Context(), userID, id)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	tags, err := a.store.ArticleTags(r.Context(), userID, art.ID)
	if err != nil {
		a.deps.Errors.Internal(w, r, err)
		return
	}
	hls, err := a.store.Highlights(r.Context(), art.ID)
	if err != nil {
		a.deps.Errors.Internal(w, r, err)
		return
	}
	sources, err := a.store.ImageSources(r.Context(), art.ID)
	if err != nil {
		a.deps.Errors.Internal(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="later-article.md"`)
	_, _ = w.Write([]byte(articleMarkdown(art, tags, hls, sources)))
}
