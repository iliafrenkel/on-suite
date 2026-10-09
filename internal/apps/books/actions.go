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
			a.renderPanes(w, r, uid, c, paneOpts{BookID: id, Banner: ref.Msg})
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
			a.renderPanes(w, r, uid, c, paneOpts{BookID: open})
			return
		}
		http.Redirect(w, r, target, http.StatusSeeOther)
	}
}

// progress records where the reading in progress stands. htmx aims it at
// the progress box, so the answer is the box with the list out of band;
// a value the store refuses comes back inside the box with what was typed
// (spec "Errors"). Nothing typed is refused like any other bad value.
func (a *App) progress(w http.ResponseWriter, r *http.Request) {
	uid, ok := a.userID(w, r)
	if !ok {
		return
	}
	id, ok := a.pathID(w, r)
	if !ok {
		return
	}
	c := ctxFrom(r.PostFormValue)
	typed := strings.TrimSpace(r.PostFormValue("at"))
	at := formInt(r, "at")
	if typed == "" {
		at = -1
	}
	err := a.store.RecordProgress(r.Context(), uid, id, at)
	var ref *Refusal
	switch {
	case errors.As(err, &ref):
		a.renderPanes(w, r, uid, c, paneOpts{BookID: id, ProgressError: ref.Msg, ProgressInput: typed})
		return
	case err != nil:
		a.fail(w, r, err)
		return
	}
	if web.IsHTMX(r) {
		a.renderPanes(w, r, uid, c, paneOpts{BookID: id})
		return
	}
	http.Redirect(w, r, c.BookURL(id), http.StatusSeeOther)
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

func (a *App) setFormat(r *http.Request, userID, id int64) error {
	return a.store.SetFormat(r.Context(), userID, id, r.PostFormValue("format"))
}

func (a *App) setRating(r *http.Request, userID, id int64) error {
	return a.store.SetRating(r.Context(), userID, id, formInt(r, "rating"))
}

func (a *App) setReview(r *http.Request, userID, id int64) error {
	return a.store.SetReview(r.Context(), userID, id, r.PostFormValue("review"))
}

// childID is the path segment name — {rid}, {nid}, {qid}: a reading, note
// or quote of the book. Anything but a positive integer is ErrNotFound, so
// it answers 404, as for one that isn't there.
func childID(r *http.Request, name string) (int64, error) {
	cid, err := strconv.ParseInt(r.PathValue(name), 10, 64)
	if err != nil || cid <= 0 {
		return 0, ErrNotFound
	}
	return cid, nil
}

func (a *App) editReading(r *http.Request, userID, id int64) error {
	rid, err := childID(r, "rid")
	if err != nil {
		return err
	}
	return a.store.UpdateReading(r.Context(), userID, id, rid, ReadingEdit{
		StartedOn:  strings.TrimSpace(r.PostFormValue("started_on")),
		FinishedOn: strings.TrimSpace(r.PostFormValue("finished_on")),
		Format:     r.PostFormValue("format"),
	})
}

func (a *App) deleteReading(r *http.Request, userID, id int64) error {
	rid, err := childID(r, "rid")
	if err != nil {
		return err
	}
	return a.store.DeleteReading(r.Context(), userID, id, rid)
}

// pageField reads a note's or quote's optional page: 0 when empty, -1
// when it isn't a whole number of 1 or more, which the store refuses with
// its own message (decided 2026-10-09).
func pageField(r *http.Request) int {
	s := strings.TrimSpace(r.PostFormValue("page"))
	if s == "" {
		return 0
	}
	n, err := strconv.Atoi(s)
	if err != nil || n < 1 {
		return -1
	}
	return n
}

// entrySave is a note or quote form's save: what was typed, kept to show
// again if the store refuses it, and the store's answer.
type entrySave func(r *http.Request, userID, id int64) (entryDraft, error)

// saveEntry answers a note or quote form. htmx aims it at the section, so
// the answer is the section with the list out of band (renderPanes);
// without JavaScript it is a redirect back to the book. A refusal comes
// back inside the form, opened, with what was typed — a 200 fragment for
// htmx, a 422 page without.
func (a *App) saveEntry(save entrySave) http.HandlerFunc {
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
		d, err := save(r, uid, id)
		var ref *Refusal
		switch {
		case errors.As(err, &ref):
			d.Error = ref.Msg
			a.renderPanes(w, r, uid, c, paneOpts{BookID: id, Draft: d})
			return
		case err != nil:
			a.fail(w, r, err)
			return
		}
		if web.IsHTMX(r) {
			a.renderPanes(w, r, uid, c, paneOpts{BookID: id})
			return
		}
		http.Redirect(w, r, c.BookURL(id), http.StatusSeeOther)
	}
}

// noteDraft is a posted note form, named form.
func noteDraft(r *http.Request, form string) entryDraft {
	return entryDraft{Form: form, Page: strings.TrimSpace(r.PostFormValue("page")), Body: r.PostFormValue("body")}
}

func (a *App) addNote(r *http.Request, userID, id int64) (entryDraft, error) {
	d := noteDraft(r, "note-new")
	_, err := a.store.AddNote(r.Context(), userID, id, NoteInput{Page: pageField(r), Body: d.Body})
	return d, err
}

func (a *App) editNote(r *http.Request, userID, id int64) (entryDraft, error) {
	nid, err := childID(r, "nid")
	if err != nil {
		return entryDraft{}, err
	}
	d := noteDraft(r, "note-"+strconv.FormatInt(nid, 10))
	return d, a.store.UpdateNote(r.Context(), userID, id, nid, NoteInput{Page: pageField(r), Body: d.Body})
}

func (a *App) deleteNote(r *http.Request, userID, id int64) error {
	nid, err := childID(r, "nid")
	if err != nil {
		return err
	}
	return a.store.DeleteNote(r.Context(), userID, id, nid)
}

// quoteDraft is a posted quote form, named form.
func quoteDraft(r *http.Request, form string) entryDraft {
	return entryDraft{Form: form, Page: strings.TrimSpace(r.PostFormValue("page")),
		Body: r.PostFormValue("text"), Comment: r.PostFormValue("comment")}
}

func (a *App) addQuote(r *http.Request, userID, id int64) (entryDraft, error) {
	d := quoteDraft(r, "quote-new")
	_, err := a.store.AddQuote(r.Context(), userID, id, QuoteInput{Page: pageField(r), Text: d.Body, Comment: d.Comment})
	return d, err
}

func (a *App) editQuote(r *http.Request, userID, id int64) (entryDraft, error) {
	qid, err := childID(r, "qid")
	if err != nil {
		return entryDraft{}, err
	}
	d := quoteDraft(r, "quote-"+strconv.FormatInt(qid, 10))
	return d, a.store.UpdateQuote(r.Context(), userID, id, qid, QuoteInput{Page: pageField(r), Text: d.Body, Comment: d.Comment})
}

func (a *App) deleteQuote(r *http.Request, userID, id int64) error {
	qid, err := childID(r, "qid")
	if err != nil {
		return err
	}
	return a.store.DeleteQuote(r.Context(), userID, id, qid)
}

func (a *App) setTags(r *http.Request, userID, id int64) error {
	return a.store.SetTags(r.Context(), userID, id, ParseTags(r.PostFormValue("tags")))
}

func (a *App) remove(r *http.Request, userID, id int64) error {
	return a.store.Delete(r.Context(), userID, id)
}
