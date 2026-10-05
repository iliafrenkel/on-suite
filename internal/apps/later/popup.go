package later

import (
	"errors"
	"fmt"
	"net/http"
)

// popupView is what templates/popup.html draws; at most one of URL
// (offer to save), AlreadyID, Error and Done is set.
type popupView struct {
	URL       string
	AlreadyID int64
	Error     string
	Done      bool
	Saved     *savedView
}

func (a *App) renderPopup(w http.ResponseWriter, r *http.Request, status int, v popupView) {
	page := a.deps.Page(r, "Save to ON Later")
	page.Data = v
	if err := a.deps.Render.Page(w, status, "later/popup", page); err != nil {
		a.deps.Errors.Internal(w, r, err)
	}
}

// popup is the bookmarklet's window. It only ever reads: saving is the POST
// the Save button makes.
func (a *App) popup(w http.ResponseWriter, r *http.Request) {
	userID, ok := a.userID(w, r)
	if !ok {
		return
	}
	pageURL, err := NormalizeURL(r.URL.Query().Get("url"))
	if err != nil {
		a.renderPopup(w, r, http.StatusOK, popupView{Error: badURLMessage})
		return
	}
	existing, err := a.store.ArticleByURL(r.Context(), userID, pageURL)
	switch {
	case err == nil:
		a.renderPopup(w, r, http.StatusOK, popupView{AlreadyID: existing.ID})
	case errors.Is(err, ErrNotFound):
		a.renderPopup(w, r, http.StatusOK, popupView{URL: pageURL})
	default:
		a.fail(w, r, err)
	}
}

// popupDone is where the popup's save lands; the page closes itself.
func (a *App) popupDone(w http.ResponseWriter, r *http.Request) {
	userID, ok := a.userID(w, r)
	if !ok {
		return
	}
	saved := a.savedNote(r, userID)
	if saved != nil {
		saved.NewTab = true
	}
	a.renderPopup(w, r, http.StatusOK, popupView{Done: true, Saved: saved})
}

func popupDoneURL(id int64, existing bool) string {
	if existing {
		return fmt.Sprintf("/later/popup/done?saved=%d&existing=1", id)
	}
	return fmt.Sprintf("/later/popup/done?saved=%d", id)
}
