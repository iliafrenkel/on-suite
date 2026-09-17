// internal/apps/flash/handlers_decks.go
package flash

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/iliafrenkel/on-suite/internal/platform/render"
	"github.com/iliafrenkel/on-suite/internal/platform/web"
)

// userID is the signed-in user's id. Handlers registered with Handle are
// guarded, so a missing user is a programming error rather than a bad
// request.
func (a *App) userID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	u, ok := web.UserFrom(r.Context())
	if !ok {
		a.deps.Errors.Status(w, r, http.StatusUnauthorized)
		return 0, false
	}
	return u.ID, true
}

// deckIDFromPath parses the {deckID} wildcard.
func (a *App) deckIDFromPath(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("deckID"), 10, 64)
	if err != nil || id <= 0 {
		a.deps.Errors.Status(w, r, http.StatusNotFound)
		return 0, false
	}
	return id, true
}

// fail maps a store error onto a response. ErrNotFound becomes a 404 whether
// the row is missing or simply someone else's, so the two are
// indistinguishable from outside.
func (a *App) fail(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, ErrNotFound):
		a.deps.Errors.Status(w, r, http.StatusNotFound)
	case errors.Is(err, ErrInvalid):
		a.deps.Errors.Status(w, r, http.StatusBadRequest)
	default:
		a.deps.Errors.Internal(w, r, err)
	}
}

func (a *App) render(w http.ResponseWriter, r *http.Request, status int, name string, page render.Page) {
	if err := a.deps.Render.Page(w, status, name, page); err != nil {
		a.deps.Errors.Internal(w, r, err)
	}
}

// userMessage strips the error's package prefix so the wording reads as a
// sentence to the person who typed the form.
func userMessage(err error) string {
	msg := err.Error()
	for _, prefix := range []string{"flash: invalid input: ", "flash: "} {
		if len(msg) > len(prefix) && msg[:len(prefix)] == prefix {
			return upperFirst(msg[len(prefix):])
		}
	}
	return upperFirst(msg)
}

func upperFirst(s string) string {
	if s == "" {
		return s
	}
	b := []rune(s)
	if b[0] >= 'a' && b[0] <= 'z' {
		b[0] -= 32
	}
	return string(b)
}

// deckPaneMode values. There is no constant for "edit": editDeckDetail
// below sets Mode to the literal "edit" directly, matching deckPageTitle's
// and the template's own "edit" checks.
const (
	deckModeView   = "view"
	deckModeNew    = "new"
	deckModeImport = "import"
)

// deckDetailView is what the deck detail pane renders, in any mode.
type deckDetailView struct {
	Mode      string
	Deck      Deck
	CSRFToken string

	NameValue        string
	DescriptionValue string
	Error            string

	NewCardsPerDayValue string
	ReviewsPerDayValue  string

	PayloadValue string
	FormatValue  string
}

// deckListItem is one row on the deck list page.
type deckListItem struct {
	Deck Deck
}

type deckListFragment struct {
	Items    []deckListItem
	ActiveID int64
	OOB      bool
}

type deckIndexView struct {
	List   deckListFragment
	Detail deckDetailView
	Title  string
	Shell  render.Shell
}

func (a *App) viewDeckDetail(r *http.Request, d Deck) deckDetailView {
	return deckDetailView{Mode: deckModeView, Deck: d, CSRFToken: web.CSRFToken(r.Context())}
}

func (a *App) newDeckDetail(r *http.Request, errMsg, name, description string) deckDetailView {
	return deckDetailView{
		Mode: deckModeNew, NameValue: name, DescriptionValue: description,
		Error: errMsg, CSRFToken: web.CSRFToken(r.Context()),
	}
}

func (a *App) editDeckDetail(r *http.Request, d Deck, errMsg, name, description, newCardsPerDay, reviewsPerDay string) deckDetailView {
	return deckDetailView{
		Mode: "edit", Deck: d, NameValue: name, DescriptionValue: description,
		NewCardsPerDayValue: newCardsPerDay, ReviewsPerDayValue: reviewsPerDay,
		Error: errMsg, CSRFToken: web.CSRFToken(r.Context()),
	}
}

func (a *App) deckListItems(ctx context.Context, userID int64) ([]deckListItem, error) {
	decks, err := a.store.ListDecks(ctx, userID)
	if err != nil {
		return nil, err
	}
	items := make([]deckListItem, 0, len(decks))
	for _, d := range decks {
		items = append(items, deckListItem{Deck: d})
	}
	return items, nil
}

func deckPageTitle(d deckDetailView) string {
	switch d.Mode {
	case deckModeView:
		return d.Deck.Name
	case "edit":
		return "Edit " + d.Deck.Name
	case deckModeNew:
		return "New deck"
	case deckModeImport:
		return "Import"
	default:
		return "Decks"
	}
}

// deckIndex renders the split-view page: the deck list on the left, and
// whichever deck {deckID} selects (or nothing) on the right. It backs both
// GET /{$} (PathValue("deckID") is "") and GET /{deckID}.
func (a *App) deckIndex(w http.ResponseWriter, r *http.Request) {
	userID, ok := a.userID(w, r)
	if !ok {
		return
	}

	var detail deckDetailView
	if r.PathValue("deckID") != "" {
		id, ok := a.deckIDFromPath(w, r)
		if !ok {
			return
		}
		d, err := a.store.DeckByID(r.Context(), userID, id)
		if err != nil {
			a.fail(w, r, err)
			return
		}
		detail = a.viewDeckDetail(r, d)
	}
	a.renderDeckIndex(w, r, userID, http.StatusOK, detail)
}

func (a *App) renderDeckIndex(w http.ResponseWriter, r *http.Request, userID int64, status int, detail deckDetailView) {
	items, err := a.deckListItems(r.Context(), userID)
	if err != nil {
		a.deps.Errors.Internal(w, r, err)
		return
	}
	if web.IsHTMX(r) && !web.IsHTMXHistoryRestore(r) {
		view := deckIndexView{List: deckListFragment{Items: items, ActiveID: detail.Deck.ID, OOB: true}, Detail: detail}
		page := a.deps.Page(r, deckPageTitle(detail))
		view.Title, view.Shell = page.Title, page.Shell
		if err := a.deps.Render.Fragment(w, http.StatusOK, "flash/decks", "deck-detail-with-list", view); err != nil {
			a.deps.Errors.Internal(w, r, err)
		}
		return
	}
	view := deckIndexView{List: deckListFragment{Items: items, ActiveID: detail.Deck.ID}, Detail: detail}
	page := a.deps.Page(r, deckPageTitle(detail))
	page.Data = view
	a.render(w, r, status, "flash/decks", page)
}

func (a *App) renderDeckDetailWithList(w http.ResponseWriter, r *http.Request, userID int64, status int, detail deckDetailView) {
	items, err := a.deckListItems(r.Context(), userID)
	if err != nil {
		a.deps.Errors.Internal(w, r, err)
		return
	}
	view := deckIndexView{List: deckListFragment{Items: items, ActiveID: detail.Deck.ID, OOB: true}, Detail: detail}
	page := a.deps.Page(r, deckPageTitle(detail))
	view.Title, view.Shell = page.Title, page.Shell
	if err := a.deps.Render.Fragment(w, status, "flash/decks", "deck-detail-with-list", view); err != nil {
		a.deps.Errors.Internal(w, r, err)
	}
}

func (a *App) newDeckForm(w http.ResponseWriter, r *http.Request) {
	userID, ok := a.userID(w, r)
	if !ok {
		return
	}
	a.renderDeckIndex(w, r, userID, http.StatusOK, a.newDeckDetail(r, "", "", ""))
}

func (a *App) createDeck(w http.ResponseWriter, r *http.Request) {
	userID, ok := a.userID(w, r)
	if !ok {
		return
	}
	name := r.PostFormValue("name")
	description := r.PostFormValue("description")

	if err := ValidateDeck(name, description); err != nil {
		a.renderDeckIndex(w, r, userID, http.StatusBadRequest, a.newDeckDetail(r, userMessage(err), name, description))
		return
	}
	d, err := a.store.CreateDeck(r.Context(), userID, name, description)
	if err != nil {
		if errors.Is(err, ErrInvalid) {
			a.renderDeckIndex(w, r, userID, http.StatusBadRequest, a.newDeckDetail(r, userMessage(err), name, description))
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
	a.renderDeckDetailWithList(w, r, userID, http.StatusCreated, a.viewDeckDetail(r, d))
}

func (a *App) editDeckForm(w http.ResponseWriter, r *http.Request) {
	userID, ok := a.userID(w, r)
	if !ok {
		return
	}
	id, ok := a.deckIDFromPath(w, r)
	if !ok {
		return
	}
	d, err := a.store.DeckByID(r.Context(), userID, id)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	reviewsStr := ""
	if d.ReviewsPerDay != nil {
		reviewsStr = strconv.Itoa(*d.ReviewsPerDay)
	}
	a.renderDeckIndex(w, r, userID, http.StatusOK,
		a.editDeckDetail(r, d, "", d.Name, d.Description, strconv.Itoa(d.NewCardsPerDay), reviewsStr))
}

// parseDeckSettings turns the edit form's two pace fields into
// UpdateDeckSettings' arguments. An empty reviewsPerDayStr means unlimited.
func parseDeckSettings(newCardsPerDayStr, reviewsPerDayStr string) (int, *int, error) {
	newCardsPerDay, err := strconv.Atoi(strings.TrimSpace(newCardsPerDayStr))
	if err != nil {
		return 0, nil, fmt.Errorf("%w: new cards per day must be a whole number", ErrInvalid)
	}
	var reviewsPerDay *int
	if s := strings.TrimSpace(reviewsPerDayStr); s != "" {
		n, err := strconv.Atoi(s)
		if err != nil {
			return 0, nil, fmt.Errorf("%w: reviews per day must be a whole number, or blank for unlimited", ErrInvalid)
		}
		reviewsPerDay = &n
	}
	return newCardsPerDay, reviewsPerDay, nil
}

func (a *App) updateDeck(w http.ResponseWriter, r *http.Request) {
	userID, ok := a.userID(w, r)
	if !ok {
		return
	}
	id, ok := a.deckIDFromPath(w, r)
	if !ok {
		return
	}
	d, err := a.store.DeckByID(r.Context(), userID, id)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	name := r.PostFormValue("name")
	description := r.PostFormValue("description")
	newCardsPerDayStr := r.PostFormValue("new_cards_per_day")
	reviewsPerDayStr := r.PostFormValue("reviews_per_day")

	if err := ValidateDeck(name, description); err != nil {
		a.renderDeckIndex(w, r, userID, http.StatusBadRequest,
			a.editDeckDetail(r, d, userMessage(err), name, description, newCardsPerDayStr, reviewsPerDayStr))
		return
	}
	newCardsPerDay, reviewsPerDay, err := parseDeckSettings(newCardsPerDayStr, reviewsPerDayStr)
	if err != nil {
		a.renderDeckIndex(w, r, userID, http.StatusBadRequest,
			a.editDeckDetail(r, d, userMessage(err), name, description, newCardsPerDayStr, reviewsPerDayStr))
		return
	}
	if err := ValidateDeckSettings(newCardsPerDay, reviewsPerDay); err != nil {
		a.renderDeckIndex(w, r, userID, http.StatusBadRequest,
			a.editDeckDetail(r, d, userMessage(err), name, description, newCardsPerDayStr, reviewsPerDayStr))
		return
	}

	_, err = a.store.UpdateDeck(r.Context(), userID, id, name, description)
	if err != nil {
		if errors.Is(err, ErrInvalid) {
			a.renderDeckIndex(w, r, userID, http.StatusBadRequest,
				a.editDeckDetail(r, d, userMessage(err), name, description, newCardsPerDayStr, reviewsPerDayStr))
			return
		}
		a.fail(w, r, err)
		return
	}
	updated, err := a.store.UpdateDeckSettings(r.Context(), userID, id, newCardsPerDay, reviewsPerDay)
	if err != nil {
		if errors.Is(err, ErrInvalid) {
			a.renderDeckIndex(w, r, userID, http.StatusBadRequest,
				a.editDeckDetail(r, d, userMessage(err), name, description, newCardsPerDayStr, reviewsPerDayStr))
			return
		}
		a.fail(w, r, err)
		return
	}

	if !web.IsHTMX(r) {
		http.Redirect(w, r, "/flash/"+strconv.FormatInt(id, 10), http.StatusSeeOther)
		return
	}
	w.Header().Set("HX-Push-Url", "/flash/"+strconv.FormatInt(id, 10))
	a.renderDeckDetailWithList(w, r, userID, http.StatusOK, a.viewDeckDetail(r, updated))
}

func (a *App) deleteDeck(w http.ResponseWriter, r *http.Request) {
	userID, ok := a.userID(w, r)
	if !ok {
		return
	}
	id, ok := a.deckIDFromPath(w, r)
	if !ok {
		return
	}
	if err := a.store.DeleteDeck(r.Context(), userID, id); err != nil {
		a.fail(w, r, err)
		return
	}
	a.deps.Log.Info("deck deleted", "app", ID, "user_id", userID, "deck_id", id)

	if !web.IsHTMX(r) {
		http.Redirect(w, r, "/flash/", http.StatusSeeOther)
		return
	}
	w.Header().Set("HX-Push-Url", "/flash/")
	a.renderDeckDetailWithList(w, r, userID, http.StatusOK, deckDetailView{})
}

func (a *App) snoozeDeck(w http.ResponseWriter, r *http.Request) {
	userID, ok := a.userID(w, r)
	if !ok {
		return
	}
	id, ok := a.deckIDFromPath(w, r)
	if !ok {
		return
	}
	days, err := strconv.Atoi(strings.TrimSpace(r.PostFormValue("days")))
	if err != nil || days <= 0 {
		a.deps.Errors.Status(w, r, http.StatusBadRequest)
		return
	}
	until := a.store.now().AddDate(0, 0, days)
	updated, err := a.store.SnoozeDeck(r.Context(), userID, id, until)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	a.deps.Log.Info("deck snoozed", "app", ID, "user_id", userID, "deck_id", id, "days", days)

	if !web.IsHTMX(r) {
		http.Redirect(w, r, "/flash/"+strconv.FormatInt(id, 10), http.StatusSeeOther)
		return
	}
	w.Header().Set("HX-Push-Url", "/flash/"+strconv.FormatInt(id, 10))
	a.renderDeckDetailWithList(w, r, userID, http.StatusOK, a.viewDeckDetail(r, updated))
}

func (a *App) unsnoozeDeck(w http.ResponseWriter, r *http.Request) {
	userID, ok := a.userID(w, r)
	if !ok {
		return
	}
	id, ok := a.deckIDFromPath(w, r)
	if !ok {
		return
	}
	updated, err := a.store.UnsnoozeDeck(r.Context(), userID, id)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	a.deps.Log.Info("deck unsnoozed", "app", ID, "user_id", userID, "deck_id", id)

	if !web.IsHTMX(r) {
		http.Redirect(w, r, "/flash/"+strconv.FormatInt(id, 10), http.StatusSeeOther)
		return
	}
	w.Header().Set("HX-Push-Url", "/flash/"+strconv.FormatInt(id, 10))
	a.renderDeckDetailWithList(w, r, userID, http.StatusOK, a.viewDeckDetail(r, updated))
}
