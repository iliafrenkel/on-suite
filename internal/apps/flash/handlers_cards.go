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

	CardTypeValue string
	FrontValue    string
	BackValue     string
	NotesValue    string
	TagsValue     string
	Error         string
	MediaError    string
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

func (a *App) viewCardDetail(r *http.Request, d Deck, c Card) cardDetailView {
	return cardDetailView{
		Mode: cardModeView, Deck: d, Card: c, CSRFToken: web.CSRFToken(r.Context()),
	}
}

// viewCardDetailWithMediaError is viewCardDetail plus an error message from
// a failed media upload, so the card page can show both the card and why
// the attachment attempt just failed.
func (a *App) viewCardDetailWithMediaError(r *http.Request, userID int64, d Deck, c Card, errMsg string) cardDetailView {
	v := a.viewCardDetail(r, d, c)
	v.MediaError = errMsg
	return v
}

func (a *App) newCardDetail(r *http.Request, d Deck, errMsg, cardType, front, back, notes, tags string) cardDetailView {
	return cardDetailView{
		Mode: cardModeNew, Deck: d, CardTypeValue: cardType, FrontValue: front, BackValue: back, NotesValue: notes,
		TagsValue: tags, Error: errMsg, CSRFToken: web.CSRFToken(r.Context()),
	}
}

func (a *App) editCardDetail(r *http.Request, d Deck, c Card, errMsg, cardType, front, back, notes, tags string) cardDetailView {
	return cardDetailView{
		Mode: cardModeEdit, Deck: d, Card: c, CardTypeValue: cardType, FrontValue: front, BackValue: back, NotesValue: notes,
		TagsValue: tags, Error: errMsg, CSRFToken: web.CSRFToken(r.Context()),
		ImageMediaURL: mediaURL(c.ImageHash), AudioMediaURL: mediaURL(c.AudioHash),
	}
}

// cardFilterFromQuery reads the grid's ?q= and ?tag= parameters.
func cardFilterFromQuery(r *http.Request) (q, tag string) {
	v := r.URL.Query()
	return strings.TrimSpace(v.Get("q")), normalizeTagName(v.Get("tag"))
}

// cardGrid builds the cards pane for one deck, filtered by q and tag. It
// also returns the filtered cards and the deck's tag map, which openedCard
// needs for previous/next and the card's own tags.
func (a *App) cardGrid(ctx context.Context, userID int64, deck Deck, q, tag string) (cardGridView, []Card, map[int64][]string, error) {
	cards, err := a.store.ListCards(ctx, userID, deck.ID)
	if err != nil {
		return cardGridView{}, nil, nil, err
	}
	tags, err := a.store.CardTagsInDeck(ctx, userID, deck.ID)
	if err != nil {
		return cardGridView{}, nil, nil, err
	}
	statuses, err := a.store.CardStatuses(ctx, userID, deck.ID, a.store.now())
	if err != nil {
		return cardGridView{}, nil, nil, err
	}
	filtered := filterCards(cards, tags, q, tag)

	view := cardGridView{Deck: deck, Query: q, Tag: tag, ClearURL: cardsURL(deck.ID, "", "")}
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
	return view, filtered, tags, nil
}

// openedCard builds the pane for one card opened from the grid. Previous
// and next step through the same filtered order the grid showed; if c is
// not in it (the filter changed, or no filter applies to it), both are
// empty.
func (a *App) openedCard(ctx context.Context, r *http.Request, userID int64, deck Deck, c Card, q, tag string) (openedCardView, error) {
	_, filtered, tags, err := a.cardGrid(ctx, userID, deck, q, tag)
	if err != nil {
		return openedCardView{}, err
	}
	view := openedCardView{
		Deck:      deck,
		Face:      newCardFace(c, deck, tags[c.ID]),
		BackURL:   cardsURL(deck.ID, q, tag),
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
		opened, err := a.openedCard(r.Context(), r, userID, deck, cd.Card, "", "")
		if err != nil {
			return deckDetailView{}, err
		}
		opened.MediaError = cd.MediaError
		detail.Mode, detail.Opened = deckModeCard, opened
	default:
		grid, _, _, err := a.cardGrid(r.Context(), userID, deck, "", "")
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
		grid, _, _, err := a.cardGrid(r.Context(), userID, deck, q, tag)
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
	grid, _, _, err := a.cardGrid(r.Context(), userID, deck, q, tag)
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
// "Save and add another" without JavaScript: same type, same tags, and the
// saved notice.
func newCardFormURL(deckID int64, cardType, tags string) string {
	v := url.Values{"saved": {"1"}, "type": {cardType}}
	if tags != "" {
		v.Set("tags", tags)
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
	q := r.URL.Query()
	cardType := q.Get("type")
	if !isKnownCardType(cardType) {
		cardType = CardTypeBasic
	}
	detail := a.newCardDetail(r, deck, "", cardType, "", "", "", q.Get("tags"))
	if q.Get("saved") != "" {
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
	reject := func(msg string) {
		a.renderCardIndex(w, r, userID, deck, http.StatusBadRequest, a.newCardDetail(r, deck, msg, cardType, front, back, notes, tags))
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
		if !web.IsHTMX(r) {
			http.Redirect(w, r, newCardFormURL(deck.ID, cardType, tags), http.StatusSeeOther)
			return
		}
		w.Header().Set("HX-Push-Url", cardBasePath(deck.ID)+"new")
		next := a.newCardDetail(r, deck, "", cardType, "", "", "", tags)
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
	a.renderCardDetailWithList(w, r, userID, deck, http.StatusCreated, a.viewCardDetail(r, deck, saved))
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
	a.renderCardIndex(w, r, userID, deck, http.StatusOK, a.editCardDetail(r, deck, c, "", c.CardType, c.Front, c.Back, c.Notes, joinTagNames(tags)))
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

	if uploadErr != "" {
		a.renderCardIndex(w, r, userID, deck, http.StatusBadRequest, a.editCardDetail(r, deck, c, uploadErr, cardType, front, back, notes, tags))
		return
	}
	tagList := parseTagList(tags)
	if err := ValidateCard(cardType, front, back); err != nil {
		a.renderCardIndex(w, r, userID, deck, http.StatusBadRequest, a.editCardDetail(r, deck, c, userMessage(err), cardType, front, back, notes, tags))
		return
	}
	if err := ValidateTagNames(tagList); err != nil {
		a.renderCardIndex(w, r, userID, deck, http.StatusBadRequest, a.editCardDetail(r, deck, c, userMessage(err), cardType, front, back, notes, tags))
		return
	}
	updated, err := a.store.UpdateCard(r.Context(), userID, deck.ID, id, cardType, front, back, notes)
	if err != nil {
		if errors.Is(err, ErrInvalid) {
			a.renderCardIndex(w, r, userID, deck, http.StatusBadRequest, a.editCardDetail(r, deck, c, userMessage(err), cardType, front, back, notes, tags))
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
