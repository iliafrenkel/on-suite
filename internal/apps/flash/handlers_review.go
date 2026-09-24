// internal/apps/flash/handlers_review.go
package flash

import (
	"context"
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

// reviewScope resolves the optional ?deck= scope to the deck it names — nil
// means every deck. A malformed id, or a deck that isn't userID's, writes a
// 404 and returns false; grade and undo call it before touching anything,
// so a bad scope never half-applies a grade.
func (a *App) reviewScope(w http.ResponseWriter, r *http.Request, userID int64) (*Deck, bool) {
	deckID, ok := a.deckScopeFromQuery(w, r)
	if !ok || deckID == nil {
		return nil, ok
	}
	d, err := a.store.DeckByID(r.Context(), userID, *deckID)
	if err != nil {
		a.fail(w, r, err)
		return nil, false
	}
	return &d, true
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

// reviewCardView is the card on screen.
type reviewCardView struct {
	Face     cardFace
	IsNew    bool
	DeckName string
}

// celebrationColors are the deck colours the summary's little burst of
// cards uses — any fixed handful of DeckColors will do.
var celebrationColors = []string{"teal", "amber", "pink", "blue", "purple", "green"}

// reviewBreak is what a snoozed deck's own review page shows in place of
// the end-of-session summary: the deck is resting, not finished.
type reviewBreak struct {
	DeckID int64
	Until  string // "2 Jan", the deck pane's own format
}

// reviewView is what templates/review.html's "review-body" block renders,
// both as a full page and as an HTMX fragment after grading or undoing.
type reviewView struct {
	Current          *reviewCardView // nil once the queue is empty
	Summary          *reviewSummary  // set when Current and Break are both nil
	Break            *reviewBreak    // set instead of Summary on a snoozed deck's own review page
	DeckScope        string          // "" (all decks) or a deck id, threaded into every form action
	LastGradedCardID int64           // 0 = nothing to undo yet
	CSRFToken        string

	ScopeName string // the deck's name, or "All decks"
	StopURL   string // the deck pane, or the home screen for Review all
	Color     string // deck colour for the progress bar; "" uses the accent

	// Progress: Done cards graded today in this scope, Total = Done plus
	// what is still queued, Position = which card this is (Done + 1).
	Done, Position, Total, ProgressPct int

	CelebrationColors []string
}

// review backs GET /review: the cross-deck queue, or one deck's queue if
// ?deck= names it.
func (a *App) review(w http.ResponseWriter, r *http.Request) {
	userID, ok := a.userID(w, r)
	if !ok {
		return
	}
	deck, ok := a.reviewScope(w, r, userID)
	if !ok {
		return
	}
	a.renderReview(w, r, userID, deck, http.StatusOK, 0)
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
	d, err := a.store.DeckByID(r.Context(), userID, id)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	a.renderReview(w, r, userID, &d, http.StatusOK, 0)
}

// renderReview draws the review screen for deck (nil = every deck). The
// caller has already loaded and ownership-checked deck, so it is not looked
// up again here (#333).
func (a *App) renderReview(w http.ResponseWriter, r *http.Request, userID int64, deck *Deck, status int, lastGradedCardID int64) {
	ctx := r.Context()
	now := a.store.now()
	var deckID *int64
	if deck != nil {
		deckID = &deck.ID
	}
	front, err := a.store.QueueFront(ctx, userID, deckID, now)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	tally, err := a.store.TodayTally(ctx, userID, deckID, now)
	if err != nil {
		a.deps.Errors.Internal(w, r, err)
		return
	}

	view := reviewView{
		DeckScope:         deckScopeString(deckID),
		LastGradedCardID:  lastGradedCardID,
		CSRFToken:         web.CSRFToken(ctx),
		ScopeName:         "All decks",
		StopURL:           "/flash/",
		Done:              tally.Reviewed,
		Total:             tally.Reviewed + front.Remaining,
		CelebrationColors: celebrationColors,
	}
	view.ProgressPct = progressPercent(view.Done, view.Total)
	if deck != nil {
		view.ScopeName, view.StopURL, view.Color = deck.Name, "/flash/"+strconv.FormatInt(deck.ID, 10), deck.Color
	}

	switch {
	case front.HasHead:
		qc := front.Head
		tags, err := a.store.TagsForCard(ctx, userID, qc.Card.ID)
		if err != nil {
			a.fail(w, r, err)
			return
		}
		names := make([]string, len(tags))
		for i, tg := range tags {
			names[i] = tg.Name
		}
		view.Current = &reviewCardView{Face: newCardFace(qc.Card, qc.Deck, names), IsNew: qc.IsNew, DeckName: qc.Deck.Name}
		view.Position = view.Done + 1
		if deck == nil {
			view.Color = qc.Deck.Color
		}
	case deck != nil && deck.IsSnoozed(now):
		view.Break = &reviewBreak{DeckID: deck.ID, Until: deck.SnoozedUntil.Format("2 Jan")}
	default:
		summary, err := a.reviewSummaryFor(ctx, userID, deckID, tally, now)
		if err != nil {
			a.deps.Errors.Internal(w, r, err)
			return
		}
		view.Summary = &summary
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

// reviewSummaryFor builds the end-of-session screen: today's tally for the
// scope, the account-wide streak, and — for a one-deck review — the other
// deck with the most cards waiting, if any.
func (a *App) reviewSummaryFor(ctx context.Context, userID int64, deckID *int64, tally ReviewTally, now time.Time) (reviewSummary, error) {
	s := reviewSummary{
		Reviewed: tally.Reviewed,
		Segments: buildSummarySegments(tally),
		Legend:   summaryLegend(tally),
	}
	streak, err := a.store.Streak(ctx, userID, now)
	if err != nil {
		return reviewSummary{}, err
	}
	s.Streak = streak

	sums, err := a.store.DeckSummaries(ctx, userID, now)
	if err != nil {
		return reviewSummary{}, err
	}
	for _, d := range sums {
		if deckID != nil && d.Deck.ID == *deckID {
			continue
		}
		if d.ReviewNow > s.NextDue {
			s.NextName, s.NextDue = d.Deck.Name, d.ReviewNow
			s.NextURL = "/flash/review/" + strconv.FormatInt(d.Deck.ID, 10)
		}
	}
	return s, nil
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
	deck, ok := a.reviewScope(w, r, userID)
	if !ok {
		return
	}

	rating, err := strconv.Atoi(r.PostFormValue("rating"))
	if err != nil || rating < RatingAgain || rating > RatingEasy {
		a.deps.Errors.Status(w, r, http.StatusBadRequest)
		return
	}
	if _, err := a.store.GradeCard(r.Context(), userID, cardID, rating, a.store.now()); err != nil {
		a.fail(w, r, err)
		return
	}
	a.renderReview(w, r, userID, deck, http.StatusOK, cardID)
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
	deck, ok := a.reviewScope(w, r, userID)
	if !ok {
		return
	}

	_, undone, err := a.store.UndoLastGrade(r.Context(), userID, cardID, a.store.now())
	if err != nil {
		a.fail(w, r, err)
		return
	}
	if !undone {
		a.deps.Errors.Status(w, r, http.StatusBadRequest)
		return
	}
	a.renderReview(w, r, userID, deck, http.StatusOK, 0)
}
