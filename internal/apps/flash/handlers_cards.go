// internal/apps/flash/handlers_cards.go
package flash

import (
	"context"
	"errors"
	"net/http"
	"strconv"

	"github.com/iliafrenkel/on-suite/internal/platform/render"
	"github.com/iliafrenkel/on-suite/internal/platform/web"
)

func (a *App) cardIDFromPath(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("cardID"), 10, 64)
	if err != nil || id <= 0 {
		a.deps.Errors.Status(w, r, http.StatusNotFound)
		return 0, false
	}
	return id, true
}

// cardDeck loads and owner-checks the deck a card route is nested under.
// Every card handler calls this first, so a request for a deck that is
// missing or belongs to someone else 404s before any card work happens.
func (a *App) cardDeck(w http.ResponseWriter, r *http.Request, userID int64) (Deck, bool) {
	deckID, ok := a.deckIDFromPath(w, r)
	if !ok {
		return Deck{}, false
	}
	d, err := a.store.DeckByID(r.Context(), userID, deckID)
	if err != nil {
		a.fail(w, r, err)
		return Deck{}, false
	}
	return d, true
}

const (
	cardModeView = "view"
	cardModeNew  = "new"
	cardModeEdit = "edit"
)

type cardDetailView struct {
	Mode      string
	Deck      Deck
	Card      Card
	CSRFToken string

	CardTypeValue string
	FrontValue    string
	BackValue     string
	NotesValue    string
	Error         string
}

type cardListItem struct {
	Card Card
}

type cardListFragment struct {
	Items    []cardListItem
	ActiveID int64
	OOB      bool
}

type cardIndexView struct {
	Deck   Deck
	List   cardListFragment
	Detail cardDetailView
	Title  string
	Shell  render.Shell
}

func (a *App) viewCardDetail(r *http.Request, d Deck, c Card) cardDetailView {
	return cardDetailView{Mode: cardModeView, Deck: d, Card: c, CSRFToken: web.CSRFToken(r.Context())}
}

func (a *App) newCardDetail(r *http.Request, d Deck, errMsg, cardType, front, back, notes string) cardDetailView {
	return cardDetailView{
		Mode: cardModeNew, Deck: d, CardTypeValue: cardType, FrontValue: front, BackValue: back, NotesValue: notes,
		Error: errMsg, CSRFToken: web.CSRFToken(r.Context()),
	}
}

func (a *App) editCardDetail(r *http.Request, d Deck, c Card, errMsg, cardType, front, back, notes string) cardDetailView {
	return cardDetailView{
		Mode: cardModeEdit, Deck: d, Card: c, CardTypeValue: cardType, FrontValue: front, BackValue: back, NotesValue: notes,
		Error: errMsg, CSRFToken: web.CSRFToken(r.Context()),
	}
}

func (a *App) cardListItems(ctx context.Context, userID, deckID int64) ([]cardListItem, error) {
	cards, err := a.store.ListCards(ctx, userID, deckID)
	if err != nil {
		return nil, err
	}
	items := make([]cardListItem, 0, len(cards))
	for _, c := range cards {
		items = append(items, cardListItem{Card: c})
	}
	return items, nil
}

func cardPageTitle(d Deck, detail cardDetailView) string {
	switch detail.Mode {
	case cardModeEdit:
		return "Edit card · " + d.Name
	case cardModeNew:
		return "New card · " + d.Name
	default:
		return "Cards · " + d.Name
	}
}

func (a *App) cardIndex(w http.ResponseWriter, r *http.Request) {
	userID, ok := a.userID(w, r)
	if !ok {
		return
	}
	deck, ok := a.cardDeck(w, r, userID)
	if !ok {
		return
	}

	var detail cardDetailView
	if r.PathValue("cardID") != "" {
		id, ok := a.cardIDFromPath(w, r)
		if !ok {
			return
		}
		c, err := a.store.CardByID(r.Context(), userID, deck.ID, id)
		if err != nil {
			a.fail(w, r, err)
			return
		}
		detail = a.viewCardDetail(r, deck, c)
	}
	a.renderCardIndex(w, r, userID, deck, http.StatusOK, detail)
}

func (a *App) renderCardIndex(w http.ResponseWriter, r *http.Request, userID int64, deck Deck, status int, detail cardDetailView) {
	items, err := a.cardListItems(r.Context(), userID, deck.ID)
	if err != nil {
		a.deps.Errors.Internal(w, r, err)
		return
	}
	if web.IsHTMX(r) && !web.IsHTMXHistoryRestore(r) {
		view := cardIndexView{Deck: deck, List: cardListFragment{Items: items, ActiveID: detail.Card.ID, OOB: true}, Detail: detail}
		page := a.deps.Page(r, cardPageTitle(deck, detail))
		view.Title, view.Shell = page.Title, page.Shell
		if err := a.deps.Render.Fragment(w, http.StatusOK, "flash/cards", "card-detail-with-list", view); err != nil {
			a.deps.Errors.Internal(w, r, err)
		}
		return
	}
	view := cardIndexView{Deck: deck, List: cardListFragment{Items: items, ActiveID: detail.Card.ID}, Detail: detail}
	page := a.deps.Page(r, cardPageTitle(deck, detail))
	page.Data = view
	a.render(w, r, status, "flash/cards", page)
}

func (a *App) renderCardDetailWithList(w http.ResponseWriter, r *http.Request, userID int64, deck Deck, status int, detail cardDetailView) {
	items, err := a.cardListItems(r.Context(), userID, deck.ID)
	if err != nil {
		a.deps.Errors.Internal(w, r, err)
		return
	}
	view := cardIndexView{Deck: deck, List: cardListFragment{Items: items, ActiveID: detail.Card.ID, OOB: true}, Detail: detail}
	page := a.deps.Page(r, cardPageTitle(deck, detail))
	view.Title, view.Shell = page.Title, page.Shell
	if err := a.deps.Render.Fragment(w, status, "flash/cards", "card-detail-with-list", view); err != nil {
		a.deps.Errors.Internal(w, r, err)
	}
}

func cardBasePath(deckID int64) string {
	return "/flash/" + strconv.FormatInt(deckID, 10) + "/cards/"
}

func (a *App) newCardForm(w http.ResponseWriter, r *http.Request) {
	userID, ok := a.userID(w, r)
	if !ok {
		return
	}
	deck, ok := a.cardDeck(w, r, userID)
	if !ok {
		return
	}
	a.renderCardIndex(w, r, userID, deck, http.StatusOK, a.newCardDetail(r, deck, "", CardTypeBasic, "", "", ""))
}

func (a *App) createCard(w http.ResponseWriter, r *http.Request) {
	userID, ok := a.userID(w, r)
	if !ok {
		return
	}
	deck, ok := a.cardDeck(w, r, userID)
	if !ok {
		return
	}
	cardType := r.PostFormValue("card_type")
	front := r.PostFormValue("front")
	back := r.PostFormValue("back")
	notes := r.PostFormValue("notes")

	if err := ValidateCard(cardType, front, back); err != nil {
		a.renderCardIndex(w, r, userID, deck, http.StatusBadRequest, a.newCardDetail(r, deck, userMessage(err), cardType, front, back, notes))
		return
	}
	c, err := a.store.CreateCard(r.Context(), userID, deck.ID, cardType, front, back, notes)
	if err != nil {
		if errors.Is(err, ErrInvalid) {
			a.renderCardIndex(w, r, userID, deck, http.StatusBadRequest, a.newCardDetail(r, deck, userMessage(err), cardType, front, back, notes))
			return
		}
		a.fail(w, r, err)
		return
	}

	if !web.IsHTMX(r) {
		http.Redirect(w, r, cardBasePath(deck.ID)+strconv.FormatInt(c.ID, 10), http.StatusSeeOther)
		return
	}
	w.Header().Set("HX-Push-Url", cardBasePath(deck.ID)+strconv.FormatInt(c.ID, 10))
	a.renderCardDetailWithList(w, r, userID, deck, http.StatusCreated, a.viewCardDetail(r, deck, c))
}

func (a *App) editCardForm(w http.ResponseWriter, r *http.Request) {
	userID, ok := a.userID(w, r)
	if !ok {
		return
	}
	deck, ok := a.cardDeck(w, r, userID)
	if !ok {
		return
	}
	id, ok := a.cardIDFromPath(w, r)
	if !ok {
		return
	}
	c, err := a.store.CardByID(r.Context(), userID, deck.ID, id)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	a.renderCardIndex(w, r, userID, deck, http.StatusOK, a.editCardDetail(r, deck, c, "", c.CardType, c.Front, c.Back, c.Notes))
}

func (a *App) updateCard(w http.ResponseWriter, r *http.Request) {
	userID, ok := a.userID(w, r)
	if !ok {
		return
	}
	deck, ok := a.cardDeck(w, r, userID)
	if !ok {
		return
	}
	id, ok := a.cardIDFromPath(w, r)
	if !ok {
		return
	}
	c, err := a.store.CardByID(r.Context(), userID, deck.ID, id)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	cardType := r.PostFormValue("card_type")
	front := r.PostFormValue("front")
	back := r.PostFormValue("back")
	notes := r.PostFormValue("notes")

	if err := ValidateCard(cardType, front, back); err != nil {
		a.renderCardIndex(w, r, userID, deck, http.StatusBadRequest, a.editCardDetail(r, deck, c, userMessage(err), cardType, front, back, notes))
		return
	}
	updated, err := a.store.UpdateCard(r.Context(), userID, deck.ID, id, cardType, front, back, notes)
	if err != nil {
		if errors.Is(err, ErrInvalid) {
			a.renderCardIndex(w, r, userID, deck, http.StatusBadRequest, a.editCardDetail(r, deck, c, userMessage(err), cardType, front, back, notes))
			return
		}
		a.fail(w, r, err)
		return
	}

	if !web.IsHTMX(r) {
		http.Redirect(w, r, cardBasePath(deck.ID)+strconv.FormatInt(id, 10), http.StatusSeeOther)
		return
	}
	w.Header().Set("HX-Push-Url", cardBasePath(deck.ID)+strconv.FormatInt(id, 10))
	a.renderCardDetailWithList(w, r, userID, deck, http.StatusOK, a.viewCardDetail(r, deck, updated))
}

func (a *App) deleteCard(w http.ResponseWriter, r *http.Request) {
	userID, ok := a.userID(w, r)
	if !ok {
		return
	}
	deck, ok := a.cardDeck(w, r, userID)
	if !ok {
		return
	}
	id, ok := a.cardIDFromPath(w, r)
	if !ok {
		return
	}
	if err := a.store.DeleteCard(r.Context(), userID, deck.ID, id); err != nil {
		a.fail(w, r, err)
		return
	}
	a.deps.Log.Info("card deleted", "app", ID, "user_id", userID, "deck_id", deck.ID, "card_id", id)

	if !web.IsHTMX(r) {
		http.Redirect(w, r, cardBasePath(deck.ID), http.StatusSeeOther)
		return
	}
	w.Header().Set("HX-Push-Url", cardBasePath(deck.ID))
	a.renderCardDetailWithList(w, r, userID, deck, http.StatusOK, cardDetailView{Deck: deck})
}
