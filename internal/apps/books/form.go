package books

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
)

// formValues is what the book form shows: the raw text, so a mistyped
// number comes back exactly as typed. AddTo, FinishedOn and Tags are for a
// new book only.
type formValues struct {
	Title, Subtitle, Authors, Year, Pages, ISBN string
	SeriesName, SeriesNumber, Description       string
	AddTo, FinishedOn, Tags                     string
}

func valuesOf(in BookInput) formValues {
	return formValues{Title: in.Title, Subtitle: in.Subtitle, Authors: in.Authors,
		Year: numText(in.Year), Pages: numText(in.Pages), ISBN: in.ISBN,
		SeriesName: in.SeriesName, SeriesNumber: in.SeriesNumber, Description: in.Description}
}

func numText(n int) string {
	if n == 0 {
		return ""
	}
	return strconv.Itoa(n)
}

// formView is the Add/Edit book page.
type formView struct {
	New     bool
	Action  string
	Heading string
	Submit  string
	Cancel  string
	Values  formValues
	Errors  FieldErrors
	Today   string // the latest finish date the form allows
	Ctx     listCtx
}

// parseForm reads a posted book form: the input, the raw values to echo
// back, and a message per number field that isn't a whole number.
func parseForm(get func(string) string) (BookInput, formValues, FieldErrors) {
	v := formValues{Title: get("title"), Subtitle: get("subtitle"), Authors: get("authors"),
		Year: get("year"), Pages: get("pages"), ISBN: get("isbn"),
		SeriesName: get("series_name"), SeriesNumber: get("series_number"), Description: get("description"),
		AddTo: get("add_to"), FinishedOn: strings.TrimSpace(get("finished_on")), Tags: get("tags")}
	in := BookInput{Title: v.Title, Subtitle: v.Subtitle, Authors: v.Authors, ISBN: v.ISBN,
		SeriesName: v.SeriesName, SeriesNumber: v.SeriesNumber, Description: v.Description}
	errs := FieldErrors{}
	number := func(field, raw string, dst *int) {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			return
		}
		n, err := strconv.Atoi(raw)
		if err != nil || n <= 0 {
			errs[field] = "Enter a whole number, or leave it empty."
			return
		}
		*dst = n
	}
	number("year", v.Year, &in.Year)
	number("pages", v.Pages, &in.Pages)
	return in, v, errs
}

// merge adds b's messages for fields a has nothing to say about: a "whole
// number" complaint beats the range check of the same field.
func merge(a, b FieldErrors) FieldErrors {
	for k, msg := range b {
		if _, ok := a[k]; !ok {
			a[k] = msg
		}
	}
	return a
}

func (a *App) renderForm(w http.ResponseWriter, r *http.Request, status int, v formView) {
	page := a.deps.Page(r, v.Heading)
	page.Data = v
	if err := a.deps.Render.Page(w, status, "books/form", page); err != nil {
		a.deps.Errors.Internal(w, r, err)
	}
}

func (a *App) newBookForm(v formValues, errs FieldErrors) formView {
	return formView{New: true, Action: "/books/new", Heading: "Add a book", Submit: "Add book",
		Cancel: "/books/", Values: v, Errors: errs, Today: a.store.Today()}
}

func (a *App) newForm(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.userID(w, r); !ok {
		return
	}
	a.renderForm(w, r, http.StatusOK, a.newBookForm(formValues{AddTo: string(ShelfWant)}, nil))
}

func (a *App) create(w http.ResponseWriter, r *http.Request) {
	uid, ok := a.userID(w, r)
	if !ok {
		return
	}
	in, vals, errs := parseForm(r.PostFormValue)
	errs = merge(errs, in.Normalize().Validate())
	shelf, _ := ParseShelf(vals.AddTo)
	if len(errs) > 0 {
		a.renderForm(w, r, http.StatusUnprocessableEntity, a.newBookForm(vals, errs))
		return
	}
	id, err := a.store.Create(r.Context(), uid, NewBook{BookInput: in, Shelf: shelf,
		FinishedOn: vals.FinishedOn, Tags: ParseTags(vals.Tags)})
	var verr *ValidationError
	var ref *Refusal
	switch {
	case errors.As(err, &verr):
		a.renderForm(w, r, http.StatusUnprocessableEntity, a.newBookForm(vals, verr.Fields))
		return
	case errors.As(err, &ref):
		a.renderForm(w, r, http.StatusUnprocessableEntity, a.newBookForm(vals, FieldErrors{"finished_on": ref.Msg}))
		return
	case errors.Is(err, ErrInvalid):
		a.renderForm(w, r, http.StatusUnprocessableEntity, a.newBookForm(vals, FieldErrors{"add_to": "Pick where the book goes."}))
		return
	case err != nil:
		a.fail(w, r, err)
		return
	}
	http.Redirect(w, r, listCtx{Shelf: shelf}.BookURL(id), http.StatusSeeOther)
}

func editBookForm(id int64, v formValues, errs FieldErrors, c listCtx) formView {
	return formView{Action: "/books/edit/" + strconv.FormatInt(id, 10), Heading: "Edit book", Submit: "Save",
		Cancel: c.BookURL(id), Values: v, Errors: errs, Ctx: c}
}

func (a *App) editForm(w http.ResponseWriter, r *http.Request) {
	uid, ok := a.userID(w, r)
	if !ok {
		return
	}
	id, ok := a.pathID(w, r)
	if !ok {
		return
	}
	b, err := a.store.Get(r.Context(), uid, id)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	a.renderForm(w, r, http.StatusOK, editBookForm(id, valuesOf(b.BookInput), nil, ctxFrom(r.FormValue)))
}

func (a *App) update(w http.ResponseWriter, r *http.Request) {
	uid, ok := a.userID(w, r)
	if !ok {
		return
	}
	id, ok := a.pathID(w, r)
	if !ok {
		return
	}
	c := ctxFrom(r.PostFormValue)
	in, vals, errs := parseForm(r.PostFormValue)
	errs = merge(errs, in.Normalize().Validate())
	if len(errs) > 0 {
		// Someone else's book is a 404 even when the form is wrong too.
		if _, err := a.store.Get(r.Context(), uid, id); err != nil {
			a.fail(w, r, err)
			return
		}
		a.renderForm(w, r, http.StatusUnprocessableEntity, editBookForm(id, vals, errs, c))
		return
	}
	if err := a.store.Update(r.Context(), uid, id, in); err != nil {
		a.fail(w, r, err)
		return
	}
	http.Redirect(w, r, c.BookURL(id), http.StatusSeeOther)
}
