package later

import (
	"fmt"
	"net/http"

	"github.com/iliafrenkel/on-suite/internal/platform/web"
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
