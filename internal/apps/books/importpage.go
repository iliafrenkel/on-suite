package books

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"strconv"
)

// importBodyMaxBytes is the import form's body budget: the file and the
// multipart overhead (the suite default is 1 MiB).
const importBodyMaxBytes = MaxImportBytes + 1<<20

// importView is the Import page: the upload form, and after an upload
// either what the import did or why it didn't happen.
type importView struct {
	MaxMB    int
	Error    string
	Done     bool
	Imported string   // "Imported 6 books."
	Skipped  string   // "Skipped 2 books already in your library:"; "" for none
	Titles   []string // the skipped books' titles
}

// importPage is the Import page (spec "Import (B5)").
func (a *App) importPage(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.userID(w, r); !ok {
		return
	}
	a.renderImport(w, r, http.StatusOK, importView{})
}

func (a *App) renderImport(w http.ResponseWriter, r *http.Request, status int, v importView) {
	v.MaxMB = MaxImportBytes >> 20
	page := a.deps.Page(r, "Import from Goodreads")
	page.Data = v
	if err := a.deps.Render.Page(w, status, "books/import", page); err != nil {
		a.deps.Errors.Internal(w, r, err)
	}
}

// importUpload imports an uploaded Goodreads export and shows what it
// did. A file it can't use comes back on the page with the reason (a 422),
// and nothing is imported. A plain form post: no JavaScript needed.
//
// No http.MaxBytesReader here: CSRF.Middleware has already parsed the
// multipart form looking for its token, under this route's own body cap
// (RegisterBodyLimit in Mount). MaxImportBytes is checked against the
// upload's reported size, as ON Reader's OPML import does.
func (a *App) importUpload(w http.ResponseWriter, r *http.Request) {
	uid, ok := a.userID(w, r)
	if !ok {
		return
	}
	refuse := func(msg string) { a.renderImport(w, r, http.StatusUnprocessableEntity, importView{Error: msg}) }
	file, header, err := r.FormFile("file")
	if err != nil {
		refuse("Choose your Goodreads export first.")
		return
	}
	defer func() { _ = file.Close() }()
	if header.Size > MaxImportBytes {
		refuse("That file is larger than " + strconv.Itoa(MaxImportBytes>>20) + " MB.")
		return
	}
	data, err := io.ReadAll(io.LimitReader(file, MaxImportBytes+1))
	if err != nil || len(data) > MaxImportBytes {
		refuse("That upload could not be read.")
		return
	}
	rows, err := ParseGoodreads(bytes.NewReader(data))
	var ie *ImportError
	if errors.As(err, &ie) {
		refuse(ie.Error())
		return
	}
	if err != nil {
		a.fail(w, r, err)
		return
	}
	res, err := a.store.Import(r.Context(), uid, rows)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	a.deps.Log.Info("books imported from goodreads", "imported", res.Imported, "skipped", len(res.Skipped))
	v := importView{Done: true, Imported: "Imported " + countText(res.Imported, "book", "books") + ".", Titles: res.Skipped}
	if n := len(res.Skipped); n > 0 {
		v.Skipped = "Skipped " + countText(n, "book", "books") + " already in your library:"
	}
	a.renderImport(w, r, http.StatusOK, v)
}
