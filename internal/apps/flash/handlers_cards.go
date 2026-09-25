// internal/apps/flash/handlers_cards.go
package flash

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"github.com/iliafrenkel/on-suite/internal/platform/web"
)

// parseTagList splits a comma-separated "tags" form field into trimmed,
// non-empty names, in the order the user typed them.
func parseTagList(raw string) []string {
	var out []string
	for _, part := range strings.Split(raw, ",") {
		if name := strings.TrimSpace(part); name != "" {
			out = append(out, name)
		}
	}
	return out
}

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

	// Query/Tag are the grid filter this form was reached through (both
	// empty when unfiltered). ActionURL/CancelURL already carry it, in the
	// same "precompute the href in Go" style as tagChip.Href.
	Query     string
	Tag       string
	ActionURL string
	CancelURL string

	CardTypeValue string
	FrontValue    string
	BackValue     string
	NotesValue    string
	TagsValue     string
	Error         string
	Notice        string // a success message above the form ("Card saved…")

	// ImageMediaURL/AudioMediaURL are the card's current media, shown by
	// the edit form so the editor can display what is already attached.
	ImageMediaURL string
	AudioMediaURL string
}

// mediaURL is the serving-route path for a card's image/audio hash, or ""
// if hash is nil (no such media attached).
func mediaURL(hash *string) string {
	if hash == nil {
		return ""
	}
	return "/flash/media/" + *hash
}

// tagChip is a tag as rendered in a card's tag-chip list: its filter link is
// precomputed here, in Go, rather than built inline in the template, so the
// name is properly path-escaped (html/template's auto-escaping guards
// against attribute breakout but does not escape "/", which would otherwise
// let a tag like "a/b" produce a broken link to the wrong path).
type tagChip struct {
	Name string
	Href string
}

// tagFilterURL is the cross-deck filter link for a tag named name.
func tagFilterURL(name string) string {
	return "/flash/tags/" + url.PathEscape(name)
}

func (a *App) viewCardDetail(r *http.Request, d Deck, c Card, q, tag string) cardDetailView {
	return cardDetailView{
		Mode: cardModeView, Deck: d, Card: c, Query: q, Tag: tag, CSRFToken: web.CSRFToken(r.Context()),
	}
}

// newCardDetail is the new-card form. q/tag are the filter the user came
// from (the grid's "New card" tile, if any) — the new card itself is not
// filtered, but Cancel/"All cards" need to know where to return to.
func (a *App) newCardDetail(r *http.Request, d Deck, errMsg, cardType, front, back, notes, tags, q, tag string) cardDetailView {
	return cardDetailView{
		Mode: cardModeNew, Deck: d, CardTypeValue: cardType, FrontValue: front, BackValue: back, NotesValue: notes,
		TagsValue: tags, Error: errMsg, Query: q, Tag: tag,
		ActionURL: newCardURL(d.ID, q, tag), CancelURL: cardsURL(d.ID, q, tag),
		CSRFToken: web.CSRFToken(r.Context()),
	}
}

func (a *App) editCardDetail(r *http.Request, d Deck, c Card, errMsg, cardType, front, back, notes, tags, q, tag string) cardDetailView {
	// Saving an edit returns to the same opened card, so Cancel/Back-to-the-
	// card and the form's own action both point at it.
	href := cardURL(d.ID, c.ID, q, tag)
	return cardDetailView{
		Mode: cardModeEdit, Deck: d, Card: c, CardTypeValue: cardType, FrontValue: front, BackValue: back, NotesValue: notes,
		TagsValue: tags, Error: errMsg, Query: q, Tag: tag,
		ActionURL: href, CancelURL: href,
		CSRFToken:     web.CSRFToken(r.Context()),
		ImageMediaURL: mediaURL(c.ImageHash), AudioMediaURL: mediaURL(c.AudioHash),
	}
}

// cardFilterFromQuery reads the grid's ?q= and ?tag= parameters.
func cardFilterFromQuery(r *http.Request) (q, tag string) {
	v := r.URL.Query()
	return strings.TrimSpace(v.Get("q")), normalizeTagName(v.Get("tag"))
}

// loadFilteredCards is the data step shared by cardGrid and openedCard: a
// deck's cards filtered by q and tag, plus the deck's tag map (needed for
// filtering and for a card's own tags). It does none of the extra work
// (status lookup, view assembly) that only the grid itself needs, so
// opening one card doesn't pay for building the whole grid.
func (a *App) loadFilteredCards(ctx context.Context, userID int64, deck Deck, q, tag string) (filtered []Card, tags map[int64][]string, err error) {
	cards, err := a.store.ListCards(ctx, userID, deck.ID)
	if err != nil {
		return nil, nil, err
	}
	tags, err = a.store.CardTagsInDeck(ctx, userID, deck.ID)
	if err != nil {
		return nil, nil, err
	}
	return filterCards(cards, tags, q, tag), tags, nil
}

// cardGrid builds the cards pane for one deck, filtered by q and tag.
func (a *App) cardGrid(ctx context.Context, userID int64, deck Deck, q, tag string) (cardGridView, error) {
	filtered, tags, err := a.loadFilteredCards(ctx, userID, deck, q, tag)
	if err != nil {
		return cardGridView{}, err
	}
	statuses, err := a.store.CardStatuses(ctx, userID, deck.ID, a.store.now())
	if err != nil {
		return cardGridView{}, err
	}

	view := cardGridView{Deck: deck, Query: q, Tag: tag, ClearURL: cardsURL(deck.ID, "", ""), NewCardURL: newCardURL(deck.ID, q, tag)}
	for _, c := range filtered {
		view.Items = append(view.Items, cardGridItem{
			Face:   newCardFace(c, deck, tags[c.ID]),
			Status: statuses[c.ID],
			Href:   cardURL(deck.ID, c.ID, q, tag),
			InPane: true,
		})
	}

	seen := map[string]bool{}
	var names []string
	for _, list := range tags {
		for _, name := range list {
			if !seen[name] {
				seen[name] = true
				names = append(names, name)
			}
		}
	}
	sort.Strings(names)
	if len(names) > 0 {
		view.Pills = append(view.Pills, tagPill{Label: "All", Href: cardsURL(deck.ID, q, ""), Active: tag == ""})
		for _, name := range names {
			view.Pills = append(view.Pills, tagPill{Label: name, Href: cardsURL(deck.ID, q, name), Active: tag == name})
		}
	}
	if tag != "" {
		view.AllDecksTagURL = tagFilterURL(tag)
	}
	return view, nil
}

// openedCard builds the pane for one card opened from the grid. Previous
// and next step through the same filtered order the grid showed; if c is
// not in it (the filter changed, or no filter applies to it), both are
// empty.
func (a *App) openedCard(ctx context.Context, r *http.Request, userID int64, deck Deck, c Card, q, tag string) (openedCardView, error) {
	filtered, tags, err := a.loadFilteredCards(ctx, userID, deck, q, tag)
	if err != nil {
		return openedCardView{}, err
	}
	view := openedCardView{
		Deck:      deck,
		Face:      newCardFace(c, deck, tags[c.ID]),
		BackURL:   cardsURL(deck.ID, q, tag),
		EditURL:   cardEditURL(deck.ID, c.ID, q, tag),
		DeleteURL: cardDeleteURL(deck.ID, c.ID, q, tag),
		CSRFToken: web.CSRFToken(r.Context()),
	}
	for i, fc := range filtered {
		if fc.ID != c.ID {
			continue
		}
		if i > 0 {
			view.PrevURL = cardURL(deck.ID, filtered[i-1].ID, q, tag)
		}
		if i < len(filtered)-1 {
			view.NextURL = cardURL(deck.ID, filtered[i+1].ID, q, tag)
		}
	}
	return view, nil
}

// cardPaneDetail translates a card handler's cardDetailView into the deck
// pane's view: a form stays a form, a viewed card becomes the opened card,
// and anything else (after a delete) is the grid.
func (a *App) cardPaneDetail(r *http.Request, userID int64, deck Deck, cd cardDetailView) (deckDetailView, error) {
	detail := deckDetailView{Deck: deck, CSRFToken: web.CSRFToken(r.Context())}
	switch cd.Mode {
	case cardModeNew:
		detail.Mode, detail.CardForm = deckModeCardNew, cd
	case cardModeEdit:
		detail.Mode, detail.CardForm = deckModeCardEdit, cd
	case cardModeView:
		opened, err := a.openedCard(r.Context(), r, userID, deck, cd.Card, cd.Query, cd.Tag)
		if err != nil {
			return deckDetailView{}, err
		}
		detail.Mode, detail.Opened = deckModeCard, opened
	default:
		grid, err := a.cardGrid(r.Context(), userID, deck, cd.Query, cd.Tag)
		if err != nil {
			return deckDetailView{}, err
		}
		detail.Mode, detail.Grid = deckModeCards, grid
	}
	return detail, nil
}

// renderCardIndex and renderCardDetailWithList keep the names every card
// handler already calls, but now draw the home layout (UI overhaul U2):
// a deck's cards live in the deck pane, not on a page of their own.
func (a *App) renderCardIndex(w http.ResponseWriter, r *http.Request, userID int64, deck Deck, status int, cd cardDetailView) {
	detail, err := a.cardPaneDetail(r, userID, deck, cd)
	if err != nil {
		a.deps.Errors.Internal(w, r, err)
		return
	}
	a.renderDeckIndex(w, r, userID, status, detail)
}

func (a *App) renderCardDetailWithList(w http.ResponseWriter, r *http.Request, userID int64, deck Deck, status int, cd cardDetailView) {
	detail, err := a.cardPaneDetail(r, userID, deck, cd)
	if err != nil {
		a.deps.Errors.Internal(w, r, err)
		return
	}
	a.renderDeckDetailWithList(w, r, userID, status, detail)
}

// cardIndex backs GET /{deckID}/cards/ (the grid) and GET
// /{deckID}/cards/{cardID} (one opened card), both keeping ?q=/?tag=.
func (a *App) cardIndex(w http.ResponseWriter, r *http.Request) {
	userID, ok := a.userID(w, r)
	if !ok {
		return
	}
	deck, ok := a.cardDeck(w, r, userID)
	if !ok {
		return
	}
	q, tag := cardFilterFromQuery(r)
	detail := deckDetailView{Deck: deck, CSRFToken: web.CSRFToken(r.Context())}

	if r.PathValue("cardID") == "" {
		grid, err := a.cardGrid(r.Context(), userID, deck, q, tag)
		if err != nil {
			a.deps.Errors.Internal(w, r, err)
			return
		}
		detail.Mode, detail.Grid = deckModeCards, grid
		a.renderDeckIndex(w, r, userID, http.StatusOK, detail)
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
	opened, err := a.openedCard(r.Context(), r, userID, deck, c, q, tag)
	if err != nil {
		a.deps.Errors.Internal(w, r, err)
		return
	}
	detail.Mode, detail.Opened = deckModeCard, opened
	a.renderDeckIndex(w, r, userID, http.StatusOK, detail)
}

// cardGridFragment backs GET /{deckID}/cards/grid: just the grid tiles plus
// an out-of-band copy of the tag pills, for the search box's live filter.
// The URL bar gets the canonical grid URL (HX-Replace-Url), never this
// route's own.
func (a *App) cardGridFragment(w http.ResponseWriter, r *http.Request) {
	userID, ok := a.userID(w, r)
	if !ok {
		return
	}
	deck, ok := a.cardDeck(w, r, userID)
	if !ok {
		return
	}
	q, tag := cardFilterFromQuery(r)
	grid, err := a.cardGrid(r.Context(), userID, deck, q, tag)
	if err != nil {
		a.deps.Errors.Internal(w, r, err)
		return
	}
	grid.OOB = true
	w.Header().Set("HX-Replace-Url", cardsURL(deck.ID, q, tag))
	if err := a.deps.Render.Fragment(w, http.StatusOK, "flash/decks", "card-grid-fragment", grid); err != nil {
		a.deps.Errors.Internal(w, r, err)
	}
}

func cardBasePath(deckID int64) string {
	return "/flash/" + strconv.FormatInt(deckID, 10) + "/cards/"
}

// cardSavedNotice is shown above the empty form after "Save and add
// another".
const cardSavedNotice = "Card saved. Add the next one."

// newCardFormURL is the new-card form pre-filled for the next card after
// "Save and add another": same type, same tags, and the grid filter the
// user came from (if any), so its Cancel/"All cards"/back link still return
// to the filtered grid. saved adds the "Card saved" notice (saved=1) — the
// no-JS redirect wants it (a fresh page load has nothing else to show it
// happened), but the HTMX push-url must leave it out: the fragment response
// already carries the notice, and reloading that pushed URL later should
// not repeat it.
func newCardFormURL(deckID int64, cardType, tags, q, tag string, saved bool) string {
	v := url.Values{"type": {cardType}}
	if saved {
		v.Set("saved", "1")
	}
	if tags != "" {
		v.Set("tags", tags)
	}
	if q != "" {
		v.Set("q", q)
	}
	if tag != "" {
		v.Set("tag", tag)
	}
	return cardBasePath(deckID) + "new?" + v.Encode()
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
	qv := r.URL.Query()
	cardType := qv.Get("type")
	if !isKnownCardType(cardType) {
		cardType = CardTypeBasic
	}
	q, tag := cardFilterFromQuery(r)
	detail := a.newCardDetail(r, deck, "", cardType, "", "", "", qv.Get("tags"), q, tag)
	if qv.Get("saved") != "" {
		detail.Notice = cardSavedNotice
	}
	a.renderCardIndex(w, r, userID, deck, http.StatusOK, detail)
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
	// First: parses the (possibly multipart) form and checks any files.
	uploads, uploadErr := readCardUploads(w, r)

	cardType := r.PostFormValue("card_type")
	front := r.PostFormValue("front")
	back := r.PostFormValue("back")
	notes := r.PostFormValue("notes")
	tags := r.PostFormValue("tags")
	if cardType == CardTypeCloze {
		// The editor hides Back for fill-in-the-blank; anything typed there
		// before switching type is not part of the card.
		back = ""
	}
	// The form's own action carries the filter the user came from (if any),
	// so a validation error re-renders the same form with Cancel still
	// pointing at the right grid.
	q, tag := cardFilterFromQuery(r)
	reject := func(msg string) {
		a.renderCardIndex(w, r, userID, deck, http.StatusBadRequest, a.newCardDetail(r, deck, msg, cardType, front, back, notes, tags, q, tag))
	}

	if uploadErr != "" {
		reject(uploadErr)
		return
	}
	tagList := parseTagList(tags)
	if err := ValidateCard(cardType, front, back); err != nil {
		reject(userMessage(err))
		return
	}
	if err := ValidateTagNames(tagList); err != nil {
		reject(userMessage(err))
		return
	}
	c, err := a.store.CreateCard(r.Context(), userID, deck.ID, cardType, front, back, notes)
	if err != nil {
		if errors.Is(err, ErrInvalid) {
			reject(userMessage(err))
			return
		}
		a.fail(w, r, err)
		return
	}
	if err := a.store.SetCardTags(r.Context(), userID, c.ID, tagList); err != nil {
		a.fail(w, r, err)
		return
	}
	if err := a.saveCardUploads(r.Context(), userID, deck.ID, c.ID, uploads); err != nil {
		a.fail(w, r, err)
		return
	}

	if r.PostFormValue("next") == "new" {
		// The saved card itself and the final Save both stay unfiltered (the
		// new card may not match the filter), but the follow-on new-card
		// form keeps the filter for its own Cancel/"All cards"/back link and
		// ActionURL, so a validation error on it re-renders with the filter
		// too.
		if !web.IsHTMX(r) {
			http.Redirect(w, r, newCardFormURL(deck.ID, cardType, tags, q, tag, true), http.StatusSeeOther)
			return
		}
		w.Header().Set("HX-Push-Url", newCardFormURL(deck.ID, cardType, tags, q, tag, false))
		next := a.newCardDetail(r, deck, "", cardType, "", "", "", tags, q, tag)
		next.Notice = cardSavedNotice
		a.renderCardDetailWithList(w, r, userID, deck, http.StatusCreated, next)
		return
	}

	if !web.IsHTMX(r) {
		http.Redirect(w, r, cardBasePath(deck.ID)+strconv.FormatInt(c.ID, 10), http.StatusSeeOther)
		return
	}
	w.Header().Set("HX-Push-Url", cardBasePath(deck.ID)+strconv.FormatInt(c.ID, 10))
	saved, err := a.store.CardByID(r.Context(), userID, deck.ID, c.ID)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	a.renderCardDetailWithList(w, r, userID, deck, http.StatusCreated, a.viewCardDetail(r, deck, saved, "", ""))
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
	tags, err := a.store.TagsForCard(r.Context(), userID, c.ID)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	q, tag := cardFilterFromQuery(r)
	a.renderCardIndex(w, r, userID, deck, http.StatusOK, a.editCardDetail(r, deck, c, "", c.CardType, c.Front, c.Back, c.Notes, joinTagNames(tags), q, tag))
}

// joinTagNames renders a card's tags back into the same comma-separated
// shape the form field accepts, so editing a card starts from its current
// tags rather than an empty field.
func joinTagNames(tags []Tag) string {
	names := make([]string, len(tags))
	for i, tg := range tags {
		names[i] = tg.Name
	}
	return strings.Join(names, ", ")
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
	// First: parses the (possibly multipart) form and checks any files.
	uploads, uploadErr := readCardUploads(w, r)

	cardType := r.PostFormValue("card_type")
	front := r.PostFormValue("front")
	back := r.PostFormValue("back")
	notes := r.PostFormValue("notes")
	tags := r.PostFormValue("tags")
	if cardType == CardTypeCloze {
		// The editor hides Back for fill-in-the-blank; anything typed there
		// before switching type is not part of the card.
		back = ""
	}
	// The edit form's action carries the filter the user came from, so a
	// validation error re-renders the same form with it kept.
	q, tag := cardFilterFromQuery(r)

	if uploadErr != "" {
		a.renderCardIndex(w, r, userID, deck, http.StatusBadRequest, a.editCardDetail(r, deck, c, uploadErr, cardType, front, back, notes, tags, q, tag))
		return
	}
	tagList := parseTagList(tags)
	if err := ValidateCard(cardType, front, back); err != nil {
		a.renderCardIndex(w, r, userID, deck, http.StatusBadRequest, a.editCardDetail(r, deck, c, userMessage(err), cardType, front, back, notes, tags, q, tag))
		return
	}
	if err := ValidateTagNames(tagList); err != nil {
		a.renderCardIndex(w, r, userID, deck, http.StatusBadRequest, a.editCardDetail(r, deck, c, userMessage(err), cardType, front, back, notes, tags, q, tag))
		return
	}
	updated, err := a.store.UpdateCard(r.Context(), userID, deck.ID, id, cardType, front, back, notes)
	if err != nil {
		if errors.Is(err, ErrInvalid) {
			a.renderCardIndex(w, r, userID, deck, http.StatusBadRequest, a.editCardDetail(r, deck, c, userMessage(err), cardType, front, back, notes, tags, q, tag))
			return
		}
		a.fail(w, r, err)
		return
	}
	if err := a.store.SetCardTags(r.Context(), userID, updated.ID, tagList); err != nil {
		a.fail(w, r, err)
		return
	}
	if err := a.saveCardUploads(r.Context(), userID, deck.ID, id, uploads); err != nil {
		a.fail(w, r, err)
		return
	}
	updated, err = a.store.CardByID(r.Context(), userID, deck.ID, id)
	if err != nil {
		a.fail(w, r, err)
		return
	}

	if !web.IsHTMX(r) {
		http.Redirect(w, r, cardURL(deck.ID, id, q, tag), http.StatusSeeOther)
		return
	}
	w.Header().Set("HX-Push-Url", cardURL(deck.ID, id, q, tag))
	a.renderCardDetailWithList(w, r, userID, deck, http.StatusOK, a.viewCardDetail(r, deck, updated, q, tag))
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

	// A deleted card can no longer be the reference point for prev/next, so
	// this always goes back to the (filtered) grid, never the next card.
	q, tag := cardFilterFromQuery(r)
	if !web.IsHTMX(r) {
		http.Redirect(w, r, cardsURL(deck.ID, q, tag), http.StatusSeeOther)
		return
	}
	w.Header().Set("HX-Push-Url", cardsURL(deck.ID, q, tag))
	a.renderCardDetailWithList(w, r, userID, deck, http.StatusOK, cardDetailView{Deck: deck, Query: q, Tag: tag})
}
