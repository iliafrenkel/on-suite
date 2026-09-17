// internal/apps/flash/handlers_review.go
package flash

import (
	"net/http"
	"strconv"
	"time"

	"github.com/iliafrenkel/on-suite/internal/platform/web"
)

// deckScopeFromQuery parses the optional ?deck= query parameter used to
// scope a review session to one deck; its absence means "every deck."
func (a *App) deckScopeFromQuery(w http.ResponseWriter, r *http.Request) (*int64, bool) {
	raw := r.URL.Query().Get("deck")
	if raw == "" {
		return nil, true
	}
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || id <= 0 {
		a.deps.Errors.Status(w, r, http.StatusNotFound)
		return nil, false
	}
	return &id, true
}

func deckScopeString(deckID *int64) string {
	if deckID == nil {
		return ""
	}
	return strconv.FormatInt(*deckID, 10)
}

// cardIDFromForm parses the card_id form field grade/undo use in place of a
// path wildcard — see flash.go's Mount comment on why these two routes take
// the card id from the POST body rather than the URL path.
func (a *App) cardIDFromForm(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PostFormValue("card_id"), 10, 64)
	if err != nil || id <= 0 {
		a.deps.Errors.Status(w, r, http.StatusNotFound)
		return 0, false
	}
	return id, true
}

type reviewCardView struct {
	Card  Card
	Deck  Deck
	IsNew bool
}

// reviewView is what templates/review.html's "review-body" block renders,
// both as a full page and as an HTMX fragment after grading or undoing.
type reviewView struct {
	Current          *reviewCardView
	DeckScope        string // "" (all decks) or a deck id, threaded into every form action below
	LastGradedCardID int64  // 0 = nothing to undo yet
	CSRFToken        string
}

// review backs GET /review: the cross-deck queue, or one deck's queue if
// ?deck= names it.
func (a *App) review(w http.ResponseWriter, r *http.Request) {
	userID, ok := a.userID(w, r)
	if !ok {
		return
	}
	deckID, ok := a.deckScopeFromQuery(w, r)
	if !ok {
		return
	}
	if deckID != nil {
		if _, err := a.store.DeckByID(r.Context(), userID, *deckID); err != nil {
			a.fail(w, r, err)
			return
		}
	}
	a.renderReview(w, r, userID, deckID, http.StatusOK, 0)
}

// deckReview backs GET /review/{deckID}: always scoped to the deck named in
// the path, regardless of any ?deck= query value.
func (a *App) deckReview(w http.ResponseWriter, r *http.Request) {
	userID, ok := a.userID(w, r)
	if !ok {
		return
	}
	id, ok := a.deckIDFromPath(w, r)
	if !ok {
		return
	}
	if _, err := a.store.DeckByID(r.Context(), userID, id); err != nil {
		a.fail(w, r, err)
		return
	}
	a.renderReview(w, r, userID, &id, http.StatusOK, 0)
}

func (a *App) renderReview(w http.ResponseWriter, r *http.Request, userID int64, deckID *int64, status int, lastGradedCardID int64) {
	queue, err := a.store.DueQueue(r.Context(), userID, deckID, time.Now())
	if err != nil {
		a.deps.Errors.Internal(w, r, err)
		return
	}
	view := reviewView{
		DeckScope:        deckScopeString(deckID),
		LastGradedCardID: lastGradedCardID,
		CSRFToken:        web.CSRFToken(r.Context()),
	}
	if len(queue) > 0 {
		view.Current = &reviewCardView{Card: queue[0].Card, Deck: queue[0].Deck, IsNew: queue[0].IsNew}
	}

	if web.IsHTMX(r) {
		if err := a.deps.Render.Fragment(w, status, "flash/review", "review-body", view); err != nil {
			a.deps.Errors.Internal(w, r, err)
		}
		return
	}
	page := a.deps.Page(r, "Review")
	page.Data = view
	a.render(w, r, status, "flash/review", page)
}

func (a *App) gradeCardHandler(w http.ResponseWriter, r *http.Request) {
	userID, ok := a.userID(w, r)
	if !ok {
		return
	}
	cardID, ok := a.cardIDFromForm(w, r)
	if !ok {
		return
	}
	deckID, ok := a.deckScopeFromQuery(w, r)
	if !ok {
		return
	}

	rating, err := strconv.Atoi(r.PostFormValue("rating"))
	if err != nil || rating < RatingAgain || rating > RatingEasy {
		a.deps.Errors.Status(w, r, http.StatusBadRequest)
		return
	}
	if _, err := a.store.GradeCard(r.Context(), userID, cardID, rating, time.Now()); err != nil {
		a.fail(w, r, err)
		return
	}
	a.renderReview(w, r, userID, deckID, http.StatusOK, cardID)
}

func (a *App) undoGradeHandler(w http.ResponseWriter, r *http.Request) {
	userID, ok := a.userID(w, r)
	if !ok {
		return
	}
	cardID, ok := a.cardIDFromForm(w, r)
	if !ok {
		return
	}
	deckID, ok := a.deckScopeFromQuery(w, r)
	if !ok {
		return
	}

	_, undone, err := a.store.UndoLastGrade(r.Context(), userID, cardID, time.Now())
	if err != nil {
		a.fail(w, r, err)
		return
	}
	if !undone {
		a.deps.Errors.Status(w, r, http.StatusBadRequest)
		return
	}
	a.renderReview(w, r, userID, deckID, http.StatusOK, 0)
}
