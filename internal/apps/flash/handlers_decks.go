// internal/apps/flash/handlers_decks.go
package flash

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/iliafrenkel/on-suite/internal/platform/auth"
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

	ShareRecipients []auth.Account      // every other account, for the Share dropdown
	SharedWith      []shareWithUsername // this deck's own share offers, for the creator's list

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
	List         deckListFragment
	Detail       deckDetailView
	Title        string
	Shell        render.Shell
	SharedWithMe []shareOfferWithUsername // pending offers addressed to the viewer
}

// shareWithUsername is one row of the creator's "Shared with" list: a
// Share plus the recipient's username, resolved via a.deps.Users since
// Share itself only carries a bare user ID.
type shareWithUsername struct {
	Share
	ToUsername string
}

// shareOfferWithUsername is one row of the recipient's "Shared with me"
// list: a ShareOffer plus the creator's username, resolved the same way
// shareWithUsername resolves the recipient's — the design spec's UI section
// calls for showing who shared a deck, not just its name, since with more
// than one other account on the instance that would otherwise be
// ambiguous.
type shareOfferWithUsername struct {
	ShareOffer
	FromUsername string
}

// usernamesByID loads every account on the instance and returns it two
// ways: the full list, and a lookup from account id to username. Both
// shareContext (the creator's "Shared with" list) and
// sharedWithMeForViewer (the recipient's "Shared with me" list) need this
// same lookup — one keyed by ToUserID, the other by FromUserID.
func (a *App) usernamesByID(ctx context.Context) ([]auth.Account, map[int64]string, error) {
	accounts, err := a.deps.Users.ListAccounts(ctx)
	if err != nil {
		return nil, nil, err
	}
	byID := make(map[int64]string, len(accounts))
	for _, acc := range accounts {
		byID[acc.ID] = acc.Username
	}
	return accounts, byID, nil
}

// shareContext loads everything the deck detail view's Share section
// needs: every other account on the instance (for the dropdown) and this
// deck's own share offers, each paired with its recipient's username (for
// the "Shared with" list).
func (a *App) shareContext(ctx context.Context, userID, deckID int64) ([]auth.Account, []shareWithUsername, error) {
	accounts, byID, err := a.usernamesByID(ctx)
	if err != nil {
		return nil, nil, err
	}
	others := make([]auth.Account, 0, len(accounts))
	for _, acc := range accounts {
		if acc.ID != userID {
			others = append(others, acc)
		}
	}
	shares, err := a.store.SharesForDeck(ctx, userID, deckID)
	if err != nil {
		return nil, nil, err
	}
	withNames := make([]shareWithUsername, len(shares))
	for i, sh := range shares {
		withNames[i] = shareWithUsername{Share: sh, ToUsername: byID[sh.ToUserID]}
	}
	return others, withNames, nil
}

// sharedWithMeForViewer loads userID's pending offers, each paired with the
// sharer's username (for the "Shared with me" list).
func (a *App) sharedWithMeForViewer(ctx context.Context, userID int64) ([]shareOfferWithUsername, error) {
	offers, err := a.store.SharesForRecipient(ctx, userID)
	if err != nil {
		return nil, err
	}
	_, byID, err := a.usernamesByID(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]shareOfferWithUsername, len(offers))
	for i, o := range offers {
		out[i] = shareOfferWithUsername{ShareOffer: o, FromUsername: byID[o.FromUserID]}
	}
	return out, nil
}

func (a *App) viewDeckDetail(r *http.Request, userID int64, d Deck, recipients []auth.Account, sharedWith []shareWithUsername) deckDetailView {
	return deckDetailView{
		Mode: deckModeView, Deck: d, CSRFToken: web.CSRFToken(r.Context()),
		ShareRecipients: recipients, SharedWith: sharedWith,
	}
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
		recipients, shares, err := a.shareContext(r.Context(), userID, d.ID)
		if err != nil {
			a.deps.Errors.Internal(w, r, err)
			return
		}
		detail = a.viewDeckDetail(r, userID, d, recipients, shares)
	}
	a.renderDeckIndex(w, r, userID, http.StatusOK, detail)
}

func (a *App) renderDeckIndex(w http.ResponseWriter, r *http.Request, userID int64, status int, detail deckDetailView) {
	items, err := a.deckListItems(r.Context(), userID)
	if err != nil {
		a.deps.Errors.Internal(w, r, err)
		return
	}
	offers, err := a.sharedWithMeForViewer(r.Context(), userID)
	if err != nil {
		a.deps.Errors.Internal(w, r, err)
		return
	}
	if web.IsHTMX(r) && !web.IsHTMXHistoryRestore(r) {
		view := deckIndexView{List: deckListFragment{Items: items, ActiveID: detail.Deck.ID, OOB: true}, Detail: detail, SharedWithMe: offers}
		page := a.deps.Page(r, deckPageTitle(detail))
		view.Title, view.Shell = page.Title, page.Shell
		if err := a.deps.Render.Fragment(w, http.StatusOK, "flash/decks", "deck-detail-with-list", view); err != nil {
			a.deps.Errors.Internal(w, r, err)
		}
		return
	}
	view := deckIndexView{List: deckListFragment{Items: items, ActiveID: detail.Deck.ID}, Detail: detail, SharedWithMe: offers}
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
	offers, err := a.sharedWithMeForViewer(r.Context(), userID)
	if err != nil {
		a.deps.Errors.Internal(w, r, err)
		return
	}
	view := deckIndexView{List: deckListFragment{Items: items, ActiveID: detail.Deck.ID, OOB: true}, Detail: detail, SharedWithMe: offers}
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
	recipients, shares, err := a.shareContext(r.Context(), userID, d.ID)
	if err != nil {
		a.deps.Errors.Internal(w, r, err)
		return
	}
	a.renderDeckDetailWithList(w, r, userID, http.StatusCreated, a.viewDeckDetail(r, userID, d, recipients, shares))
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
	recipients, shares, err := a.shareContext(r.Context(), userID, updated.ID)
	if err != nil {
		a.deps.Errors.Internal(w, r, err)
		return
	}
	a.renderDeckDetailWithList(w, r, userID, http.StatusOK, a.viewDeckDetail(r, userID, updated, recipients, shares))
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
	recipients, shares, err := a.shareContext(r.Context(), userID, updated.ID)
	if err != nil {
		a.deps.Errors.Internal(w, r, err)
		return
	}
	a.renderDeckDetailWithList(w, r, userID, http.StatusOK, a.viewDeckDetail(r, userID, updated, recipients, shares))
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
	recipients, shares, err := a.shareContext(r.Context(), userID, updated.ID)
	if err != nil {
		a.deps.Errors.Internal(w, r, err)
		return
	}
	a.renderDeckDetailWithList(w, r, userID, http.StatusOK, a.viewDeckDetail(r, userID, updated, recipients, shares))
}
