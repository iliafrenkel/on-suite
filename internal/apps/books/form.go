package books

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// formValues is what the book form shows: the raw text, so a mistyped
// number comes back exactly as typed. AddTo, FinishedOn and Tags are for a
// new book only; so are OLWork, OLEdition and CoverID, which carry an Open
// Library pick through the form in hidden fields.
type formValues struct {
	Title, Subtitle, Authors, Year, Pages, ISBN string
	SeriesName, SeriesNumber, Description       string
	AddTo, FinishedOn, Tags                     string
	OLWork, OLEdition, CoverID                  string
	CoverURL                                    string // the edit form's image address, echoed back on an error
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

// searchView is the Open Library search on the Add book page.
type searchView struct {
	Query    string
	Searched bool
	Error    string
	Results  []resultView
}

// resultView is one search result.
type resultView struct {
	Title   string
	Byline  string // "Authors · Year"
	Thumb   string // the proxied thumbnail; "" when Open Library has no cover
	PickURL string // the Add book page pre-filled with this result
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
	Search  searchView
	// Cover is the edit form's current cover ("" draws Spine instead).
	Cover string
	Spine string
}

// parseForm reads a posted book form: the input, the raw values to echo
// back, and a message per number field that isn't a whole number.
func parseForm(get func(string) string) (BookInput, formValues, FieldErrors) {
	v := formValues{Title: get("title"), Subtitle: get("subtitle"), Authors: get("authors"),
		Year: get("year"), Pages: get("pages"), ISBN: get("isbn"),
		SeriesName: get("series_name"), SeriesNumber: get("series_number"), Description: get("description"),
		AddTo: get("add_to"), FinishedOn: strings.TrimSpace(get("finished_on")), Tags: get("tags"),
		OLWork: olID(get("ol_work"), 'W'), OLEdition: olID(get("ol_edition"), 'M'), CoverID: coverIDText(get("cover_id")), CoverURL: get("cover_url")}
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

// coverIDText keeps an Open Library cover id: 1–12 digits, not zero.
func coverIDText(s string) string {
	s = strings.TrimSpace(s)
	if s == "" || len(s) > 12 || strings.Trim(s, "0123456789") != "" || strings.TrimLeft(s, "0") == "" {
		return ""
	}
	return s
}

// thumbURL is a search result's proxied thumbnail.
func thumbURL(coverID int64) string {
	if coverID <= 0 {
		return ""
	}
	return "/books/olcover/" + strconv.FormatInt(coverID, 10)
}

// pickURL is the Add book page pre-filled with c. Everything travels in
// the link: the values are only a starting point the person edits anyway.
func pickURL(c Candidate) string {
	v := url.Values{"pick": {"1"}}
	for k, val := range map[string]string{"title": c.Title, "subtitle": c.Subtitle, "authors": c.Authors,
		"year": numText(c.Year), "pages": numText(c.Pages), "isbn": c.ISBN,
		"ol_work": c.WorkID, "ol_edition": c.EditionID} {
		if val != "" {
			v.Set(k, val)
		}
	}
	if c.CoverID > 0 {
		v.Set("cover", strconv.FormatInt(c.CoverID, 10))
	}
	return "/books/new?" + v.Encode()
}

const searchFailed = "Open Library didn't answer. Try again, or fill in the book yourself below."

// search asks Open Library for q. A failure is logged and shown as a
// notice; the page still works.
func (a *App) search(ctx context.Context, q string) searchView {
	sv := searchView{Query: q, Searched: true}
	found, err := a.ol.Search(ctx, q)
	if err != nil {
		a.deps.Log.Info("books open library search failed", "error", err)
		sv.Error = searchFailed
		return sv
	}
	for _, c := range found {
		sv.Results = append(sv.Results, resultView{Title: c.Title, Byline: byline(c.Authors, numText(c.Year)),
			Thumb: thumbURL(c.CoverID), PickURL: pickURL(c)})
	}
	return sv
}

// picked is the form pre-filled from a "Use this" link, with the work's
// description fetched now (a failure leaves it empty).
func (a *App) picked(ctx context.Context, q url.Values) formValues {
	v := formValues{Title: q.Get("title"), Subtitle: q.Get("subtitle"), Authors: q.Get("authors"),
		Year: q.Get("year"), Pages: q.Get("pages"), ISBN: q.Get("isbn"), AddTo: string(ShelfWant),
		OLWork: olID(q.Get("ol_work"), 'W'), OLEdition: olID(q.Get("ol_edition"), 'M'), CoverID: coverIDText(q.Get("cover"))}
	if v.OLWork != "" {
		d, err := a.ol.Description(ctx, v.OLWork)
		if err != nil {
			a.deps.Log.Info("books open library description failed", "work", v.OLWork, "error", err)
		}
		v.Description = d
	}
	return v
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

// newForm is the Add book page: an Open Library search (q), a result
// picked from it (pick), or an empty form.
func (a *App) newForm(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.userID(w, r); !ok {
		return
	}
	q := r.URL.Query()
	v := formValues{AddTo: string(ShelfWant)}
	if q.Get("pick") != "" {
		v = a.picked(r.Context(), q)
	}
	view := a.newBookForm(v, nil)
	if text := strings.TrimSpace(q.Get("q")); text != "" {
		view.Search = a.search(r.Context(), text)
		// Nothing to pick from: start the form with what was typed (spec
		// "Screens → Add book").
		if len(view.Search.Results) == 0 && view.Values.Title == "" && view.Values.ISBN == "" {
			if isbn, ok := ISBN13(text); ok {
				view.Values.ISBN = isbn
			} else {
				view.Values.Title = text
			}
		}
	}
	a.renderForm(w, r, http.StatusOK, view)
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
		FinishedOn: vals.FinishedOn, Tags: ParseTags(vals.Tags),
		OLWorkID: vals.OLWork, OLEditionID: vals.OLEdition})
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
	if coverID, err := strconv.ParseInt(vals.CoverID, 10, 64); err == nil {
		a.saveOLCover(r.Context(), uid, id, coverID)
	}
	http.Redirect(w, r, listCtx{Shelf: shelf}.BookURL(id), http.StatusSeeOther)
}

func editBookForm(id int64, v formValues, errs FieldErrors, c listCtx, cover string) formView {
	return formView{Action: "/books/edit/" + strconv.FormatInt(id, 10), Heading: "Edit book", Submit: "Save",
		Cancel: c.BookURL(id), Values: v, Errors: errs, Ctx: c, Cover: cover, Spine: SpineColor(v.Title)}
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
	a.renderForm(w, r, http.StatusOK, editBookForm(id, valuesOf(b.BookInput), nil, ctxFrom(r.FormValue), coverURL(b.ID, b.CoverVersion)))
}

// update saves the edit form. The owner check comes first, before any
// cover address is fetched; a bad cover is a 422 like any other field, and
// then nothing is saved.
func (a *App) update(w http.ResponseWriter, r *http.Request) {
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
	c := ctxFrom(r.PostFormValue)
	in, vals, errs := parseForm(r.PostFormValue)
	errs = merge(errs, in.Normalize().Validate())
	var change CoverChange
	if len(errs) == 0 { // don't fetch a pasted address for a form that is bouncing anyway
		var msg string
		change, msg = a.readCoverChange(r)
		if msg != "" {
			errs["cover"] = msg
		}
	}
	if len(errs) > 0 {
		a.renderForm(w, r, http.StatusUnprocessableEntity, editBookForm(id, vals, errs, c, coverURL(b.ID, b.CoverVersion)))
		return
	}
	if err := a.store.UpdateWithCover(r.Context(), uid, id, in, change); err != nil {
		a.fail(w, r, err)
		return
	}
	http.Redirect(w, r, c.BookURL(id), http.StatusSeeOther)
}
