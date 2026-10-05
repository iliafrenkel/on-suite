package later

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"

	"github.com/iliafrenkel/on-suite/internal/platform/web"
)

const (
	overlapMessage = "That overlaps one of your highlights. Select a different passage."
	badSpanMessage = "Couldn't highlight that selection. Try selecting it again."
)

// noteSavedView answers a note save: the copy that didn't post, refreshed,
// and a "Saved" next to the one that did.
type noteSavedView struct {
	D     articleView
	Copy  string // "panel" | "end": the copy that posted
	Other string
}

// setNote saves the article note from either of its two copies (the Notes
// panel and the box after the article; spec: "Notes").
func (a *App) setNote(w http.ResponseWriter, r *http.Request) {
	userID, ok := a.userID(w, r)
	if !ok {
		return
	}
	id, ok := a.pathID(w, r)
	if !ok {
		return
	}
	if err := a.store.SetNote(r.Context(), userID, id, r.PostFormValue("note")); err != nil {
		a.fail(w, r, err)
		return
	}
	if !web.IsHTMX(r) {
		http.Redirect(w, r, fmt.Sprintf("/later/a/%d", id), http.StatusSeeOther)
		return
	}
	art, err := a.store.Article(r.Context(), userID, id)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	v := noteSavedView{D: articleView{ID: art.ID, Note: art.Note}, Copy: "end", Other: "panel"}
	if r.PostFormValue("copy") == "panel" {
		v.Copy, v.Other = "panel", "end"
	}
	if err := a.deps.Render.Fragment(w, http.StatusOK, "later/article", "later-note-saved", v); err != nil {
		a.deps.Errors.Internal(w, r, err)
	}
}

// addHighlight saves a highlight the reader selected (static/highlight.js
// computes the offsets) and answers with the redrawn article.
func (a *App) addHighlight(w http.ResponseWriter, r *http.Request) {
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
	start, err1 := strconv.Atoi(r.PostFormValue("start"))
	end, err2 := strconv.Atoi(r.PostFormValue("end"))
	if err1 != nil || err2 != nil {
		a.highlightRejected(w, ErrInvalid)
		return
	}
	if _, err := a.store.AddHighlight(r.Context(), art.ID, art.ContentText, start, end,
		r.PostFormValue("quote"), r.PostFormValue("comment")); err != nil {
		if errors.Is(err, ErrInvalid) || errors.Is(err, ErrOverlap) {
			a.highlightRejected(w, err)
			return
		}
		a.fail(w, r, err)
		return
	}
	a.renderHighlightSwap(w, r, userID, art)
}

// highlightRejected answers 422 with a line the popover shows as text
// (spec: "Highlight rejections ... show a short notice by the popover").
func (a *App) highlightRejected(w http.ResponseWriter, err error) {
	msg := badSpanMessage
	if errors.Is(err, ErrOverlap) {
		msg = overlapMessage
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusUnprocessableEntity)
	_, _ = io.WriteString(w, msg)
}

// renderHighlightSwap answers a highlight change: the redrawn body, plus the
// Notes panel's list and count out of band. Without HTMX it goes back to the
// article.
func (a *App) renderHighlightSwap(w http.ResponseWriter, r *http.Request, userID int64, art Article) {
	if !web.IsHTMX(r) {
		http.Redirect(w, r, fmt.Sprintf("/later/a/%d", art.ID), http.StatusSeeOther)
		return
	}
	view, err := a.buildArticleView(r, userID, art, "")
	if err != nil {
		a.fail(w, r, err)
		return
	}
	view.OOB = true
	if err := a.deps.Render.Fragment(w, http.StatusOK, "later/article", "later-highlight-swap", view); err != nil {
		a.deps.Errors.Internal(w, r, err)
	}
}

// highlightID parses the "highlight" form field; anything but a positive
// integer is a 404, like a highlight that isn't there.
func (a *App) highlightID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PostFormValue("highlight"), 10, 64)
	if err != nil || id <= 0 {
		a.deps.Errors.Status(w, r, http.StatusNotFound)
		return 0, false
	}
	return id, true
}

// changeHighlight loads the user's article and the highlight id, runs op on
// them, and answers with the redrawn article.
func (a *App) changeHighlight(op func(r *http.Request, art Article, hid int64) error) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
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
		hid, ok := a.highlightID(w, r)
		if !ok {
			return
		}
		if err := op(r, art, hid); err != nil {
			a.fail(w, r, err)
			return
		}
		a.renderHighlightSwap(w, r, userID, art)
	}
}
