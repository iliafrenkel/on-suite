// internal/apps/flash/handlers_share.go
package flash

import (
	"fmt"
	"net/http"
	"strconv"

	"github.com/iliafrenkel/on-suite/internal/platform/web"
)

// shareIDFromPath parses the {shareID} wildcard, used by the creator's
// revoke action, which operates on a share row nested under its deck.
func (a *App) shareIDFromPath(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("shareID"), 10, 64)
	if err != nil || id <= 0 {
		a.deps.Errors.Status(w, r, http.StatusNotFound)
		return 0, false
	}
	return id, true
}

// shareIDFromForm parses the share_id POST field, used by the recipient's
// adopt/decline actions — taken from the body rather than the path so
// these routes stay a 2-segment literal shape (see flash.go's Mount for
// why a 3-segment /shared/{shareID}/... shape would conflict with
// /{deckID}/cards/{cardID}).
func (a *App) shareIDFromForm(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PostFormValue("share_id"), 10, 64)
	if err != nil || id <= 0 {
		a.deps.Errors.Status(w, r, http.StatusBadRequest)
		return 0, false
	}
	return id, true
}

// shareDeck handles POST /{deckID}/share: the creator offers deckID to
// to_user_id.
func (a *App) shareDeck(w http.ResponseWriter, r *http.Request) {
	userID, ok := a.userID(w, r)
	if !ok {
		return
	}
	deckID, ok := a.deckIDFromPath(w, r)
	if !ok {
		return
	}
	toUserID, err := strconv.ParseInt(r.PostFormValue("to_user_id"), 10, 64)
	if err != nil {
		a.deps.Errors.Status(w, r, http.StatusBadRequest)
		return
	}
	// The store has no FK from flash_shares to the accounts table (and
	// couldn't enforce auth even if it did), so a tampered to_user_id has to
	// be caught here: reject anything that isn't one of the accounts this
	// handler already has access to before it ever reaches the store.
	_, byID, err := a.usernamesByID(r.Context())
	if err != nil {
		a.deps.Errors.Internal(w, r, err)
		return
	}
	if _, ok := byID[toUserID]; !ok {
		a.deps.Errors.Status(w, r, http.StatusBadRequest)
		return
	}

	if _, err := a.store.ShareDeck(r.Context(), userID, deckID, toUserID); err != nil {
		a.fail(w, r, err)
		return
	}
	a.deps.Log.Info("deck shared", "app", ID, "user_id", userID, "deck_id", deckID, "to_user_id", toUserID)

	d, err := a.store.DeckByID(r.Context(), userID, deckID)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	recipients, shares, err := a.shareContext(r.Context(), userID, deckID)
	if err != nil {
		a.deps.Errors.Internal(w, r, err)
		return
	}
	if web.IsHTMX(r) {
		w.Header().Set("HX-Push-Url", "/flash/"+strconv.FormatInt(d.ID, 10))
	}
	view := a.viewDeckDetail(r, userID, d, recipients, shares)
	view.ShareOpen = true
	a.renderDeckDetailWithList(w, r, userID, http.StatusOK, view)
}

// revokeShareHandler handles POST /{deckID}/share/{shareID}/revoke.
func (a *App) revokeShareHandler(w http.ResponseWriter, r *http.Request) {
	userID, ok := a.userID(w, r)
	if !ok {
		return
	}
	deckID, ok := a.deckIDFromPath(w, r)
	if !ok {
		return
	}
	shareID, ok := a.shareIDFromPath(w, r)
	if !ok {
		return
	}

	if err := a.store.RevokeShare(r.Context(), userID, deckID, shareID); err != nil {
		a.fail(w, r, err)
		return
	}
	a.deps.Log.Info("share revoked", "app", ID, "user_id", userID, "share_id", shareID)

	d, err := a.store.DeckByID(r.Context(), userID, deckID)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	recipients, shares, err := a.shareContext(r.Context(), userID, deckID)
	if err != nil {
		a.deps.Errors.Internal(w, r, err)
		return
	}
	if web.IsHTMX(r) {
		w.Header().Set("HX-Push-Url", "/flash/"+strconv.FormatInt(d.ID, 10))
	}
	view := a.viewDeckDetail(r, userID, d, recipients, shares)
	view.ShareOpen = true
	a.renderDeckDetailWithList(w, r, userID, http.StatusOK, view)
}

// declineShareHandler handles POST /shared/decline: the recipient
// dismisses a pending offer without adopting it.
func (a *App) declineShareHandler(w http.ResponseWriter, r *http.Request) {
	userID, ok := a.userID(w, r)
	if !ok {
		return
	}
	shareID, ok := a.shareIDFromForm(w, r)
	if !ok {
		return
	}
	if err := a.store.DeclineShare(r.Context(), userID, shareID); err != nil {
		a.fail(w, r, err)
		return
	}
	a.deps.Log.Info("share declined", "app", ID, "user_id", userID, "share_id", shareID)
	if web.IsHTMX(r) {
		w.Header().Set("HX-Push-Url", "/flash/")
	}
	a.renderDeckDetailWithList(w, r, userID, http.StatusOK, deckDetailView{})
}

// giftPreview backs GET /shared/{shareID}: a pending share's preview in the
// deck pane. Anything but the recipient's own pending share is a 404.
func (a *App) giftPreview(w http.ResponseWriter, r *http.Request) {
	userID, ok := a.userID(w, r)
	if !ok {
		return
	}
	shareID, ok := a.shareIDFromPath(w, r)
	if !ok {
		return
	}
	p, err := a.store.SharePreview(r.Context(), userID, shareID)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	_, byID, err := a.usernamesByID(r.Context())
	if err != nil {
		a.deps.Errors.Internal(w, r, err)
		return
	}
	gift := giftView{
		ShareID: shareID, DeckName: p.Deck.Name, Description: p.Deck.Description, Color: p.Deck.Color,
		FromUsername: byID[p.Offer.FromUserID], IsMerge: p.Offer.PriorAdoptedDeckID != nil,
		CardCount: p.CardCount, CSRFToken: web.CSRFToken(r.Context()),
	}
	for _, c := range p.Samples {
		gift.Samples = append(gift.Samples, cardGridItem{Face: newCardFace(c, p.Deck, nil)})
	}
	a.renderDeckIndex(w, r, userID, http.StatusOK, deckDetailView{Mode: deckModeGift, Gift: gift})
}

// plural is "s" unless n is 1.
func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

// adoptShareHandler handles POST /shared/adopt: the recipient adopts
// (first time) or merges (re-share) a pending offer.
func (a *App) adoptShareHandler(w http.ResponseWriter, r *http.Request) {
	userID, ok := a.userID(w, r)
	if !ok {
		return
	}
	shareID, ok := a.shareIDFromForm(w, r)
	if !ok {
		return
	}
	wasMerge, newCards := false, 0
	if offers, err := a.store.SharesForRecipient(r.Context(), userID); err == nil {
		for _, o := range offers {
			if o.ID == shareID {
				wasMerge, newCards = o.PriorAdoptedDeckID != nil, o.NewCardCount
			}
		}
	}
	d, err := a.store.AdoptShare(r.Context(), userID, shareID)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	a.deps.Log.Info("share adopted", "app", ID, "user_id", userID, "share_id", shareID, "deck_id", d.ID)

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
	view := a.viewDeckDetail(r, userID, d, recipients, shares)
	view.Notice = "“" + d.Name + "” is now in your decks."
	if wasMerge {
		view.Notice = fmt.Sprintf("%d new card%s added to “%s”.", newCards, plural(newCards), d.Name)
	}
	a.renderDeckDetailWithList(w, r, userID, http.StatusOK, view)
}
