// internal/apps/flash/handlers_import.go
package flash

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"github.com/iliafrenkel/on-suite/internal/platform/web"
)

func (a *App) importDeckDetail(r *http.Request, errMsg, payload, format string) deckDetailView {
	if format == "" {
		format = "auto"
	}
	return deckDetailView{
		Mode: deckModeImport, PayloadValue: payload, FormatValue: format, ImportPrompt: importPrompt,
		MarkdownExample: importMarkdownExample,
		Error:           errMsg, CSRFToken: web.CSRFToken(r.Context()),
	}
}

func (a *App) importForm(w http.ResponseWriter, r *http.Request) {
	userID, ok := a.userID(w, r)
	if !ok {
		return
	}
	a.renderDeckIndex(w, r, userID, http.StatusOK, a.importDeckDetail(r, "", "", "auto"))
}

func (a *App) importDeck(w http.ResponseWriter, r *http.Request) {
	userID, ok := a.userID(w, r)
	if !ok {
		return
	}
	payload := r.PostFormValue("payload")
	format := r.PostFormValue("format")

	parsed, err := ParseImport(payload, format)
	if err != nil {
		a.renderDeckIndex(w, r, userID, http.StatusBadRequest, a.importDeckDetail(r, userMessage(err), payload, format))
		return
	}
	d, err := a.store.ImportDeck(r.Context(), userID, parsed.Name, parsed.Description, parsed.Cards)
	if err != nil {
		if errors.Is(err, ErrInvalid) {
			a.renderDeckIndex(w, r, userID, http.StatusBadRequest, a.importDeckDetail(r, userMessage(err), payload, format))
			return
		}
		a.deps.Errors.Internal(w, r, err)
		return
	}

	if !web.IsHTMX(r) {
		http.Redirect(w, r, "/flash/"+strconv.FormatInt(d.ID, 10), http.StatusSeeOther)
		return
	}
	w.Header().Set("HX-Push-Url", "/flash/"+strconv.FormatInt(d.ID, 10))
	view, err := a.viewDeckDetailWithShareContext(r, userID, d)
	if err != nil {
		a.deps.Errors.Internal(w, r, err)
		return
	}
	view.Notice = fmt.Sprintf("Imported %d card%s into “%s”.", len(parsed.Cards), plural(len(parsed.Cards)), d.Name)
	a.renderDeckDetailWithList(w, r, userID, http.StatusCreated, view)
}
