package books

import (
	"errors"
	"net/http"
	"strings"

	"github.com/iliafrenkel/on-suite/internal/platform/web"
)

// change is one thing done to a book from the book pane.
type change func(r *http.Request, userID, id int64) error

// act runs a change and answers with the panes. htmx gets the whole panes
// (shelf counts, the list and the book can all move); without JavaScript
// it is a redirect back to the book — or to the list, once the book is
// gone. A Refusal is the banner over the panes rather than an error page.
func (a *App) act(do change, gone bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		uid, ok := a.userID(w, r)
		if !ok {
			return
		}
		id, ok := a.pathID(w, r)
		if !ok {
			return
		}
		c := ctxFrom(r.PostFormValue)
		err := do(r, uid, id)
		var ref *Refusal
		switch {
		case errors.As(err, &ref):
			a.renderPanes(w, r, uid, c, id, ref.Msg)
			return
		case err != nil:
			a.fail(w, r, err)
			return
		}
		open, target := id, c.BookURL(id)
		if gone {
			open, target = 0, c.ListURL()
		}
		if web.IsHTMX(r) {
			a.renderPanes(w, r, uid, c, open, "")
			return
		}
		http.Redirect(w, r, target, http.StatusSeeOther)
	}
}

func (a *App) start(r *http.Request, userID, id int64) error {
	return a.store.StartReading(r.Context(), userID, id)
}

func (a *App) finish(r *http.Request, userID, id int64) error {
	return a.store.FinishReading(r.Context(), userID, id, strings.TrimSpace(r.PostFormValue("day")))
}

func (a *App) dnf(r *http.Request, userID, id int64) error {
	return a.store.MarkDNF(r.Context(), userID, id, strings.TrimSpace(r.PostFormValue("day")))
}

func (a *App) setTags(r *http.Request, userID, id int64) error {
	return a.store.SetTags(r.Context(), userID, id, ParseTags(r.PostFormValue("tags")))
}

func (a *App) remove(r *http.Request, userID, id int64) error {
	return a.store.Delete(r.Context(), userID, id)
}
