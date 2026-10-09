package books

import (
	"errors"
	"net/http"
	"strconv"
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
			// The form posted from the address bar's book; say where the panes
			// now stand (the list, once the book is gone).
			w.Header().Set("HX-Replace-Url", target)
			a.renderPanes(w, r, uid, c, open, "")
			return
		}
		http.Redirect(w, r, target, http.StatusSeeOther)
	}
}

func (a *App) start(r *http.Request, userID, id int64) error {
	return a.store.StartReading(r.Context(), userID, id)
}

// formInt reads an optional whole-number field: 0 when it is empty, -1
// when it isn't a number — outside every range the store accepts, so a
// typo comes back as the store's own message (or a 400 for a field no
// person types into).
func formInt(r *http.Request, name string) int {
	s := strings.TrimSpace(r.PostFormValue(name))
	if s == "" {
		return 0
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return -1
	}
	return n
}

func (a *App) finish(r *http.Request, userID, id int64) error {
	return a.store.FinishReading(r.Context(), userID, id, strings.TrimSpace(r.PostFormValue("day")), formInt(r, "rating"))
}

func (a *App) dnf(r *http.Request, userID, id int64) error {
	return a.store.MarkDNF(r.Context(), userID, id, strings.TrimSpace(r.PostFormValue("day")), formInt(r, "at"))
}

func (a *App) setTags(r *http.Request, userID, id int64) error {
	return a.store.SetTags(r.Context(), userID, id, ParseTags(r.PostFormValue("tags")))
}

func (a *App) remove(r *http.Request, userID, id int64) error {
	return a.store.Delete(r.Context(), userID, id)
}
