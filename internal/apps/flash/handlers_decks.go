// internal/apps/flash/handlers_decks.go
package flash

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

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
	// Card modes (UI overhaul U2): a deck's cards live in the same pane.
	deckModeCards    = "cards"
	deckModeCard     = "card"
	deckModeCardNew  = "card-new"
	deckModeCardEdit = "card-edit"
	// deckModeGift is a pending share's preview, before it is adopted.
	deckModeGift = "gift"
	// deckModeStats is the stats pane (UI overhaul U6): the old standalone
	// /flash/stats page moved into the home screen's right pane.
	deckModeStats = "stats"
)

// deckDetailView is what the deck detail pane renders, in any mode.
type deckDetailView struct {
	Mode      string
	Deck      Deck
	CSRFToken string

	// Summary and NextLabel are filled in by buildDeckIndex for Mode
	// "view" — the Review button, tiles, and "next cards" line.
	Summary   DeckSummary
	NextLabel string

	// Notice is a one-off success message at the top of the deck view
	// ("Planets is now in your decks").
	Notice string

	ShareOpen bool // render the Share popover open (after a share or revoke)
	Snoozed   bool // edit mode: whether "Take a break" shows End break

	// Gift is set for Mode "gift": a pending share's preview.
	Gift giftView

	// Stats is set for Mode "stats": the stats pane.
	Stats statsView

	// HasDecks is filled in by buildDeckIndex for the empty mode: false
	// shows the first-run welcome, true a "pick a deck" hint.
	HasDecks bool

	ShareRecipients []auth.Account      // every other account, for the Share dropdown
	SharedWith      []shareWithUsername // this deck's own share offers, for the creator's list

	NameValue        string
	DescriptionValue string
	ColorValue       string
	Colors           []deckColorOption
	Error            string

	NewCardsPerDayValue string
	ReviewsPerDayValue  string

	// preload carries data the handler has already fetched for its own
	// pane, so buildDeckIndex does not query it again. Templates never see
	// it.
	preload deckIndexPreload

	PayloadValue    string
	FormatValue     string
	ImportPrompt    string // the AI prompt shown in the import pane
	MarkdownExample string // the "Writing it by hand?" worked example (#302.6)

	// Card modes. Grid is set for "cards", Opened for "card", CardForm for
	// "card-new" and "card-edit".
	Grid     cardGridView
	Opened   openedCardView
	CardForm cardDetailView
}

// deckColorOption is one swatch in the colour picker.
type deckColorOption struct {
	Name  string // a DeckColors entry, the radio value and the deck-c-* class suffix
	Label string // its accessible name, e.g. "Teal"
}

// deckColorOptions lists every DeckColors entry with a label.
func deckColorOptions() []deckColorOption {
	out := make([]deckColorOption, len(DeckColors))
	for i, name := range DeckColors {
		out[i] = deckColorOption{Name: name, Label: upperFirst(name)}
	}
	return out
}

// deckListItem is one row in the deck list: a projection of DeckSummary
// down to what the row shows.
type deckListItem struct {
	Deck      Deck
	CardCount int
	Due       int    // ReviewNow; the badge shows when > 0
	Status    string // the row's second line
	Snoozed   bool
}

func newDeckListItem(s DeckSummary) deckListItem {
	item := deckListItem{Deck: s.Deck, CardCount: s.CardCount, Due: s.ReviewNow, Snoozed: s.Snoozed}
	switch {
	case s.Snoozed:
		item.Status = "taking a break"
	case s.CardCount == 0:
		item.Status = "no cards yet"
	case s.ReviewNow == 0:
		item.Status = "all done"
	case s.CardCount == 1:
		item.Status = "1 card"
	default:
		item.Status = strconv.Itoa(s.CardCount) + " cards"
	}
	return item
}

// giftRow is a pending share shown at the top of the deck list, like a
// present waiting to be opened (UI overhaul spec §6).
type giftRow struct {
	ShareID      int64
	DeckName     string
	DeckColor    string
	FromUsername string
	IsMerge      bool // the recipient already has this deck; only new cards come
	NewCardCount int
	UpToDate     bool // a merge with 0 new cards: the row shows no badge (#346)
}

// giftView is the pane for one gift.
type giftView struct {
	ShareID      int64
	DeckName     string
	Description  string
	Color        string
	FromUsername string
	IsMerge      bool
	// UpToDate is a merge with 0 new cards (#346): the pane says the
	// recipient already has every card and offers only Got it, which
	// adopts the offer so it leaves the list.
	UpToDate  bool
	CardCount int
	Samples   []cardGridItem
	CSRFToken string
}

type deckListFragment struct {
	Items        []deckListItem
	ActiveID     int64
	Gifts        []giftRow
	ActiveGiftID int64
	OOB          bool
}

type deckIndexView struct {
	List     deckListFragment
	Detail   deckDetailView
	Title    string
	Shell    render.Shell
	TotalDue int // sum of every deck's ReviewNow: the "Review all" count
}

// shareWithUsername is one row of the creator's "Shared with" list: one
// recipient's latest non-revoked Share (see SharesForDeck) plus their
// username, resolved via a.deps.Users since Share itself only carries a
// bare user ID.
type shareWithUsername struct {
	Share
	ToUsername  string
	StatusLabel string
}

// shareStatusLabel is how a share's status reads in the Share popover.
func shareStatusLabel(status string) string {
	switch status {
	case ShareStatusPending:
		return "waiting"
	case ShareStatusAdopted:
		return "added"
	case ShareStatusDeclined:
		return "said no thanks"
	default:
		return status
	}
}

// shareOfferWithUsername is one pending offer addressed to the viewer: a
// ShareOffer plus the creator's username, resolved the same way
// shareWithUsername resolves the recipient's — the design spec's UI section
// calls for showing who shared a deck, not just its name, since with more
// than one other account on the instance that would otherwise be
// ambiguous. It backs the gift rows in the deck list and a gift's preview
// pane.
type shareOfferWithUsername struct {
	ShareOffer
	FromUsername string
}

// usernamesByID loads every account on the instance and returns it two
// ways: the full list, and a lookup from account id to username. shareContext
// (the creator's "Shared with" list and the Share dropdown's recipients),
// buildDeckIndex (the recipient's pending gift rows and, for the open deck,
// the inline Share section), shareDeck (validating a share's to_user_id),
// and giftPreview (a gift's "From" username) all need this same lookup.
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

// otherAccounts filters accounts down to everyone but userID — the Share
// dropdown's recipient list.
func otherAccounts(accounts []auth.Account, userID int64) []auth.Account {
	others := make([]auth.Account, 0, len(accounts))
	for _, acc := range accounts {
		if acc.ID != userID {
			others = append(others, acc)
		}
	}
	return others
}

// sharesWithUsernames pairs each of the creator's own shares with its
// recipient's username, resolved from byID (see usernamesByID).
func sharesWithUsernames(shares []Share, byID map[int64]string) []shareWithUsername {
	out := make([]shareWithUsername, len(shares))
	for i, sh := range shares {
		out[i] = shareWithUsername{Share: sh, ToUsername: byID[sh.ToUserID], StatusLabel: shareStatusLabel(sh.Status)}
	}
	return out
}

// offersWithUsernames pairs each pending offer with its sharer's username,
// resolved from byID (see usernamesByID).
func offersWithUsernames(offers []ShareOffer, byID map[int64]string) []shareOfferWithUsername {
	out := make([]shareOfferWithUsername, len(offers))
	for i, o := range offers {
		out[i] = shareOfferWithUsername{ShareOffer: o, FromUsername: byID[o.FromUserID]}
	}
	return out
}

// shareContext loads everything the deck detail view's Share section
// needs: every other account on the instance (for the dropdown) and the
// "Shared with" list (one row per recipient, see SharesForDeck), each row
// paired with its recipient's username.
func (a *App) shareContext(ctx context.Context, userID, deckID int64) ([]auth.Account, []shareWithUsername, error) {
	accounts, byID, err := a.usernamesByID(ctx)
	if err != nil {
		return nil, nil, err
	}
	shares, err := a.store.SharesForDeck(ctx, userID, deckID)
	if err != nil {
		return nil, nil, err
	}
	return otherAccounts(accounts, userID), sharesWithUsernames(shares, byID), nil
}

func (a *App) viewDeckDetail(r *http.Request, userID int64, d Deck, recipients []auth.Account, sharedWith []shareWithUsername) deckDetailView {
	return deckDetailView{
		Mode: deckModeView, Deck: d, CSRFToken: web.CSRFToken(r.Context()),
		ShareRecipients: recipients, SharedWith: sharedWith,
	}
}

func (a *App) newDeckDetail(r *http.Request, errMsg, name, description, color string) deckDetailView {
	if color == "" {
		color = DefaultDeckColor
	}
	return deckDetailView{
		Mode: deckModeNew, NameValue: name, DescriptionValue: description,
		ColorValue: color, Colors: deckColorOptions(),
		Error: errMsg, CSRFToken: web.CSRFToken(r.Context()),
	}
}

func (a *App) editDeckDetail(r *http.Request, d Deck, errMsg, name, description, color, newCardsPerDay, reviewsPerDay string) deckDetailView {
	if color == "" {
		color = d.Color
	}
	return deckDetailView{
		Mode: "edit", Deck: d, NameValue: name, DescriptionValue: description,
		ColorValue: color, Colors: deckColorOptions(),
		NewCardsPerDayValue: newCardsPerDay, ReviewsPerDayValue: reviewsPerDay,
		Error: errMsg, CSRFToken: web.CSRFToken(r.Context()),
		Snoozed: d.IsSnoozed(a.store.now()),
	}
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
	case deckModeCards:
		return "Cards · " + d.Deck.Name
	case deckModeCard:
		return "Card · " + d.Deck.Name
	case deckModeCardNew:
		return "New card · " + d.Deck.Name
	case deckModeCardEdit:
		return "Edit card · " + d.Deck.Name
	case deckModeGift:
		return "Shared with you"
	case deckModeStats:
		return "Stats"
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

// deckIndexPreload is what a handler may hand buildDeckIndex so it can skip
// a query it would otherwise run. Every field is optional; the zero value
// preloads nothing.
type deckIndexPreload struct {
	// summaries is DeckSummaries(userID, now) as the handler loaded it,
	// valid only when haveSummaries is set (a user with no decks has an
	// empty slice, which is still a valid preload). buildDeckIndex then
	// uses this now too, so the list agrees with the handler's pane.
	summaries     []DeckSummary
	now           time.Time
	haveSummaries bool
}

// buildDeckIndex assembles the home screen's whole view model — list,
// pane, toolbar count, gift offers — for both the full-page and HTMX paths,
// so the two can never compute any of it differently. oob marks the list
// for an out-of-band swap (every fragment response sets it).
func (a *App) buildDeckIndex(r *http.Request, userID int64, detail deckDetailView, oob bool) (deckIndexView, error) {
	ctx := r.Context()
	now := a.store.now()
	sums := detail.preload.summaries
	if detail.preload.haveSummaries {
		now = detail.preload.now
	} else {
		var err error
		if sums, err = a.store.DeckSummaries(ctx, userID, now); err != nil {
			return deckIndexView{}, err
		}
	}
	rawOffers, err := a.store.SharesForRecipient(ctx, userID)
	if err != nil {
		return deckIndexView{}, err
	}

	needShareContext := (detail.Mode == deckModeView || detail.Mode == deckModeCards) && detail.Deck.ID != 0 && detail.ShareRecipients == nil
	var rawShares []Share
	if needShareContext {
		rawShares, err = a.store.SharesForDeck(ctx, userID, detail.Deck.ID)
		if err != nil {
			return deckIndexView{}, err
		}
	}

	// The account map is only needed to enrich offers or shares with
	// usernames, and ListAccounts runs a per-account session-count subquery
	// for every row — so it's fetched at most once per render, and only
	// when one of the two actually has rows to enrich.
	var offers []shareOfferWithUsername
	if len(rawOffers) > 0 || needShareContext {
		accounts, byID, err := a.usernamesByID(ctx)
		if err != nil {
			return deckIndexView{}, err
		}
		if len(rawOffers) > 0 {
			offers = offersWithUsernames(rawOffers, byID)
		}
		if needShareContext {
			detail.ShareRecipients = otherAccounts(accounts, userID)
			detail.SharedWith = sharesWithUsernames(rawShares, byID)
		}
	}

	items := make([]deckListItem, 0, len(sums))
	total := 0
	for _, s := range sums {
		items = append(items, newDeckListItem(s))
		total += s.ReviewNow
		if detail.Mode == deckModeView && s.Deck.ID == detail.Deck.ID {
			detail.Deck = s.Deck
			detail.Summary = s
			detail.NextLabel = nextCardsLabel(s, now)
		}
	}
	if detail.Mode == "" {
		detail.HasDecks = len(sums) > 0 || len(offers) > 0
	}

	gifts := make([]giftRow, len(offers))
	for i, o := range offers {
		isMerge := o.PriorAdoptedDeckID != nil
		// A merge offer names the recipient's own adopted copy (which may
		// have been auto-suffixed, #304), not alice's source deck.
		name := o.DeckName
		if isMerge {
			name = o.PriorAdoptedDeckName
		}
		gifts[i] = giftRow{
			ShareID: o.ID, DeckName: name, DeckColor: o.DeckColor, FromUsername: o.FromUsername,
			IsMerge: isMerge, NewCardCount: o.NewCardCount, UpToDate: isMerge && o.NewCardCount == 0,
		}
	}

	return deckIndexView{
		List:     deckListFragment{Items: items, Gifts: gifts, ActiveID: detail.Deck.ID, ActiveGiftID: detail.Gift.ShareID, OOB: oob},
		Detail:   detail,
		TotalDue: total,
	}, nil
}

// renderDeckIndex renders the home screen: the whole page on a normal
// request, or just the pane (plus out-of-band list/toolbar/checkbox) on an
// HTMX one. status is only used for the full page; an HTMX navigation is
// always 200 (an HTMX 4xx would not be swapped in).
func (a *App) renderDeckIndex(w http.ResponseWriter, r *http.Request, userID int64, status int, detail deckDetailView) {
	if web.IsHTMX(r) && !web.IsHTMXHistoryRestore(r) {
		a.renderDeckDetailWithList(w, r, userID, http.StatusOK, detail)
		return
	}
	view, err := a.buildDeckIndex(r, userID, detail, false)
	if err != nil {
		a.deps.Errors.Internal(w, r, err)
		return
	}
	page := a.deps.Page(r, deckPageTitle(detail))
	page.Data = view
	a.render(w, r, status, "flash/decks", page)
}

// renderDeckDetailWithList is the HTMX response for anything that changes
// the pane: the pane itself plus out-of-band copies of everything outside it
// that may have changed with it.
func (a *App) renderDeckDetailWithList(w http.ResponseWriter, r *http.Request, userID int64, status int, detail deckDetailView) {
	view, err := a.buildDeckIndex(r, userID, detail, true)
	if err != nil {
		a.deps.Errors.Internal(w, r, err)
		return
	}
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
	a.renderDeckIndex(w, r, userID, http.StatusOK, a.newDeckDetail(r, "", "", "", ""))
}

func (a *App) createDeck(w http.ResponseWriter, r *http.Request) {
	userID, ok := a.userID(w, r)
	if !ok {
		return
	}
	name := r.PostFormValue("name")
	description := r.PostFormValue("description")
	color := r.PostFormValue("color")
	if color == "" {
		color = DefaultDeckColor
	}
	reject := func(err error) {
		a.renderDeckIndex(w, r, userID, http.StatusBadRequest, a.newDeckDetail(r, userMessage(err), name, description, color))
	}

	if err := ValidateDeck(name, description); err != nil {
		reject(err)
		return
	}
	if !ValidDeckColor(color) {
		reject(fmt.Errorf("%w: pick one of the colours shown", ErrInvalid))
		return
	}
	d, err := a.store.CreateDeck(r.Context(), userID, name, description)
	if err != nil {
		if errors.Is(err, ErrInvalid) {
			reject(err)
			return
		}
		a.deps.Errors.Internal(w, r, err)
		return
	}
	if color != d.Color {
		if d, err = a.store.SetDeckColor(r.Context(), userID, d.ID, color); err != nil {
			a.fail(w, r, err)
			return
		}
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
		a.editDeckDetail(r, d, "", d.Name, d.Description, d.Color, strconv.Itoa(d.NewCardsPerDay), reviewsStr))
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
	color := r.PostFormValue("color")
	if color == "" {
		color = d.Color
	}
	newCardsPerDayStr := r.PostFormValue("new_cards_per_day")
	reviewsPerDayStr := r.PostFormValue("reviews_per_day")

	if err := ValidateDeck(name, description); err != nil {
		a.renderDeckIndex(w, r, userID, http.StatusBadRequest,
			a.editDeckDetail(r, d, userMessage(err), name, description, color, newCardsPerDayStr, reviewsPerDayStr))
		return
	}
	newCardsPerDay, reviewsPerDay, err := parseDeckSettings(newCardsPerDayStr, reviewsPerDayStr)
	if err != nil {
		a.renderDeckIndex(w, r, userID, http.StatusBadRequest,
			a.editDeckDetail(r, d, userMessage(err), name, description, color, newCardsPerDayStr, reviewsPerDayStr))
		return
	}
	if err := ValidateDeckSettings(newCardsPerDay, reviewsPerDay); err != nil {
		a.renderDeckIndex(w, r, userID, http.StatusBadRequest,
			a.editDeckDetail(r, d, userMessage(err), name, description, color, newCardsPerDayStr, reviewsPerDayStr))
		return
	}
	if !ValidDeckColor(color) {
		a.renderDeckIndex(w, r, userID, http.StatusBadRequest,
			a.editDeckDetail(r, d, "Pick one of the colours shown.", name, description, d.Color, newCardsPerDayStr, reviewsPerDayStr))
		return
	}

	_, err = a.store.UpdateDeck(r.Context(), userID, id, name, description)
	if err != nil {
		if errors.Is(err, ErrInvalid) {
			a.renderDeckIndex(w, r, userID, http.StatusBadRequest,
				a.editDeckDetail(r, d, userMessage(err), name, description, color, newCardsPerDayStr, reviewsPerDayStr))
			return
		}
		a.fail(w, r, err)
		return
	}
	updated, err := a.store.UpdateDeckSettings(r.Context(), userID, id, newCardsPerDay, reviewsPerDay)
	if err != nil {
		if errors.Is(err, ErrInvalid) {
			a.renderDeckIndex(w, r, userID, http.StatusBadRequest,
				a.editDeckDetail(r, d, userMessage(err), name, description, color, newCardsPerDayStr, reviewsPerDayStr))
			return
		}
		a.fail(w, r, err)
		return
	}
	if updated.Color != color {
		if updated, err = a.store.SetDeckColor(r.Context(), userID, id, color); err != nil {
			a.fail(w, r, err)
			return
		}
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

// maxSnoozeDays caps a break at a year. The pane only offers a week or a
// month; the cap stops a hand-edited form from pushing snoozed_until to an
// absurd (or overflowed) date.
const maxSnoozeDays = 365

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
	if err != nil || days <= 0 || days > maxSnoozeDays {
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
