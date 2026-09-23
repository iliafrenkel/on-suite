package flash_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/iliafrenkel/on-suite/internal/apps/flash"
	"github.com/iliafrenkel/on-suite/internal/apptest"
	"github.com/iliafrenkel/on-suite/internal/htmlassert"
)

func newServer(t *testing.T) *apptest.Server[*flash.Store] {
	t.Helper()
	return apptest.NewServer(t, flash.New(), flash.NewStore)
}

func TestFlashRequiresSignIn(t *testing.T) {
	s := newServer(t)
	rec := s.Do(t, nil, httpGet(t, "/flash/"))
	if rec.Code != 303 && rec.Code != 401 {
		t.Errorf("GET /flash/ signed out = %d, want a redirect-to-login or 401", rec.Code)
	}
}

func TestCreateDeckRequiresCSRF(t *testing.T) {
	s := newServer(t)
	req := httpPost(t, "/flash/new", url.Values{"name": {"Spanish"}})
	rec := s.Do(t, s.Alice, req)
	if rec.Code != 403 {
		t.Errorf("POST /flash/new without CSRF = %d, want 403", rec.Code)
	}
}

func TestCreateAndViewDeck(t *testing.T) {
	s := newServer(t)
	s.Submit(t, s.Alice, "/flash/new", url.Values{"name": {"Spanish"}, "description": {"Travel"}}, "/flash/1")

	doc := s.Get(t, s.Alice, "/flash/")
	doc.MustHave(".deck-list")
	row := doc.MustHave(`a.deck-row`)
	if got := htmlassert.Text(row); got == "" {
		t.Error("the new deck does not appear in the list")
	}
}

func TestCreateDeckValidation(t *testing.T) {
	s := newServer(t)
	rec := s.Post(t, s.Alice, "/flash/new", url.Values{"name": {"  "}})
	if rec.Code != 400 {
		t.Errorf("POST /flash/new with a blank name = %d, want 400", rec.Code)
	}
	doc := htmlassert.Parse(t, rec.Body.String())
	doc.MustHave(".notice-error")
}

func TestViewingSomeoneElsesDeckIs404(t *testing.T) {
	s := newServer(t)
	deck, err := s.Store.CreateDeck(t.Context(), s.Alice.User.ID, "alice's", "")
	if err != nil {
		t.Fatal(err)
	}
	rec := s.Do(t, s.Bob, httpGet(t, "/flash/"+itoa(deck.ID)))
	if rec.Code != 404 {
		t.Errorf("GET another user's deck = %d, want 404", rec.Code)
	}
}

func TestDeleteDeckRequiresCSRFAndPOST(t *testing.T) {
	s := newServer(t)
	deck, err := s.Store.CreateDeck(t.Context(), s.Alice.User.ID, "doomed", "")
	if err != nil {
		t.Fatal(err)
	}
	req := httpPost(t, "/flash/"+itoa(deck.ID)+"/delete", url.Values{})
	rec := s.Do(t, s.Alice, req)
	if rec.Code != 403 {
		t.Errorf("delete without CSRF = %d, want 403", rec.Code)
	}
}

func TestUpdateDeckSettingsOverHTTP(t *testing.T) {
	s := newServer(t)
	deck, err := s.Store.CreateDeck(t.Context(), s.Alice.User.ID, "Spanish", "")
	if err != nil {
		t.Fatal(err)
	}
	s.Submit(t, s.Alice, "/flash/"+itoa(deck.ID), url.Values{
		"name": {"Spanish"}, "description": {""},
		"new_cards_per_day": {"5"}, "reviews_per_day": {"30"},
	}, "/flash/"+itoa(deck.ID))

	updated, err := s.Store.DeckByID(t.Context(), s.Alice.User.ID, deck.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.NewCardsPerDay != 5 {
		t.Errorf("NewCardsPerDay = %d, want 5", updated.NewCardsPerDay)
	}
	if updated.ReviewsPerDay == nil || *updated.ReviewsPerDay != 30 {
		t.Errorf("ReviewsPerDay = %v, want 30", updated.ReviewsPerDay)
	}
}

func TestUpdateDeckSettingsRejectsNegativeOverHTTP(t *testing.T) {
	s := newServer(t)
	deck, err := s.Store.CreateDeck(t.Context(), s.Alice.User.ID, "Spanish", "")
	if err != nil {
		t.Fatal(err)
	}
	rec := s.Post(t, s.Alice, "/flash/"+itoa(deck.ID), url.Values{
		"name": {"Spanish"}, "description": {""},
		"new_cards_per_day": {"-1"}, "reviews_per_day": {""},
	})
	if rec.Code != 400 {
		t.Errorf("negative new_cards_per_day = %d, want 400", rec.Code)
	}
}

func TestUpdateDeckSettingsRejectsNegativeWithoutPartiallyWriting(t *testing.T) {
	s := newServer(t)
	deck, err := s.Store.CreateDeck(t.Context(), s.Alice.User.ID, "Spanish", "")
	if err != nil {
		t.Fatal(err)
	}
	rec := s.Post(t, s.Alice, "/flash/"+itoa(deck.ID), url.Values{
		"name": {"Renamed"}, "description": {"new description"},
		"new_cards_per_day": {"-1"}, "reviews_per_day": {""},
	})
	if rec.Code != 400 {
		t.Fatalf("negative new_cards_per_day = %d, want 400", rec.Code)
	}

	// The rejected pace settings must not leave the name/description change
	// committed: validation happens before either store write now, so a
	// rejected request writes nothing at all.
	unchanged, err := s.Store.DeckByID(t.Context(), s.Alice.User.ID, deck.ID)
	if err != nil {
		t.Fatal(err)
	}
	if unchanged.Name != "Spanish" {
		t.Errorf("Name = %q after rejected settings update, want unchanged %q", unchanged.Name, "Spanish")
	}
	if unchanged.Description != "" {
		t.Errorf("Description = %q after rejected settings update, want unchanged empty", unchanged.Description)
	}
}

func TestSnoozeAndUnsnoozeDeckOverHTTP(t *testing.T) {
	s := newServer(t)
	deck, err := s.Store.CreateDeck(t.Context(), s.Alice.User.ID, "Spanish", "")
	if err != nil {
		t.Fatal(err)
	}
	s.Submit(t, s.Alice, "/flash/"+itoa(deck.ID)+"/snooze", url.Values{"days": {"7"}}, "/flash/"+itoa(deck.ID))

	snoozed, err := s.Store.DeckByID(t.Context(), s.Alice.User.ID, deck.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !snoozed.IsSnoozed(time.Now()) {
		t.Error("deck is not snoozed after posting /snooze")
	}

	s.Submit(t, s.Alice, "/flash/"+itoa(deck.ID)+"/unsnooze", url.Values{}, "/flash/"+itoa(deck.ID))
	unsnoozed, err := s.Store.DeckByID(t.Context(), s.Alice.User.ID, deck.ID)
	if err != nil {
		t.Fatal(err)
	}
	if unsnoozed.IsSnoozed(time.Now()) {
		t.Error("deck is still snoozed after posting /unsnooze")
	}
}

// TestSnoozedDeckPaneAndListRow checks how a snoozed deck actually renders:
// the pane offers an Unsnooze form and hides the "Take a break" section and
// the Review CTA, and the deck's list row is marked and says so.
func TestSnoozedDeckPaneAndListRow(t *testing.T) {
	s := newServer(t)
	deck, err := s.Store.CreateDeck(t.Context(), s.Alice.User.ID, "Spanish", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Store.CreateCard(t.Context(), s.Alice.User.ID, deck.ID, flash.CardTypeBasic, "hola", "hello", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Store.SnoozeDeck(t.Context(), s.Alice.User.ID, deck.ID, time.Now().Add(7*24*time.Hour)); err != nil {
		t.Fatal(err)
	}

	doc := s.Get(t, s.Alice, "/flash/"+itoa(deck.ID))

	form := doc.MustHave(`form[action="/flash/` + itoa(deck.ID) + `/unsnooze"]`)
	if got := htmlassert.Text(form); !strings.Contains(got, "End break") {
		t.Errorf("unsnooze form text = %q, want it to contain End break", got)
	}

	doc.MustNotHave(".flash-review-cta")

	for _, section := range doc.QueryAll(".flash-deck-section") {
		if strings.Contains(htmlassert.Text(section), "Take a break") {
			t.Error("the pane still shows the \"Take a break\" section on a snoozed deck")
		}
	}

	row := doc.MustHave("#deck-list a")
	class, _ := htmlassert.Attr(row, "class")
	if !strings.Contains(class, "deck-row-snoozed") {
		t.Errorf("deck row class = %q, want it to contain deck-row-snoozed", class)
	}
	if got := htmlassert.Text(row); !strings.Contains(got, "taking a break") {
		t.Errorf("deck row text = %q, want it to say taking a break", got)
	}
	if n := doc.Query("#deck-list .flash-due-badge"); n != nil {
		t.Error("a snoozed deck's row still shows a due badge")
	}
}

func TestSnoozeDeckRequiresCSRF(t *testing.T) {
	s := newServer(t)
	deck, err := s.Store.CreateDeck(t.Context(), s.Alice.User.ID, "Spanish", "")
	if err != nil {
		t.Fatal(err)
	}
	req := httpPost(t, "/flash/"+itoa(deck.ID)+"/snooze", url.Values{"days": {"7"}})
	rec := s.Do(t, s.Alice, req)
	if rec.Code != 403 {
		t.Errorf("snooze without CSRF = %d, want 403", rec.Code)
	}
}

func TestCreateDeckWithColor(t *testing.T) {
	s := newServer(t)
	s.Submit(t, s.Alice, "/flash/new", url.Values{"name": {"Planets"}, "description": {""}, "color": {"purple"}}, "/flash/1")
	d, err := s.Store.DeckByID(t.Context(), s.Alice.User.ID, 1)
	if err != nil {
		t.Fatal(err)
	}
	if d.Color != "purple" {
		t.Errorf("Color = %q, want purple", d.Color)
	}
}

func TestCreateDeckWithoutColorIsTeal(t *testing.T) {
	s := newServer(t)
	s.Submit(t, s.Alice, "/flash/new", url.Values{"name": {"Planets"}}, "/flash/1")
	d, err := s.Store.DeckByID(t.Context(), s.Alice.User.ID, 1)
	if err != nil {
		t.Fatal(err)
	}
	if d.Color != flash.DefaultDeckColor {
		t.Errorf("Color = %q, want the default", d.Color)
	}
}

func TestCreateDeckRejectsUnknownColor(t *testing.T) {
	s := newServer(t)
	rec := s.Post(t, s.Alice, "/flash/new", url.Values{"name": {"Planets"}, "color": {"chartreuse"}})
	if rec.Code != 400 {
		t.Fatalf("unknown colour = %d, want 400", rec.Code)
	}
	htmlassert.Parse(t, rec.Body.String()).MustHave(".notice-error")
	if decks, _ := s.Store.ListDecks(t.Context(), s.Alice.User.ID); len(decks) != 0 {
		t.Errorf("a rejected create left %d decks behind", len(decks))
	}
}

func TestUpdateDeckChangesColor(t *testing.T) {
	s := newServer(t)
	deck, err := s.Store.CreateDeck(t.Context(), s.Alice.User.ID, "Spanish", "")
	if err != nil {
		t.Fatal(err)
	}
	s.Submit(t, s.Alice, "/flash/"+itoa(deck.ID), url.Values{
		"name": {"Spanish"}, "description": {""}, "color": {"amber"},
		"new_cards_per_day": {"20"}, "reviews_per_day": {""},
	}, "/flash/"+itoa(deck.ID))
	d, err := s.Store.DeckByID(t.Context(), s.Alice.User.ID, deck.ID)
	if err != nil {
		t.Fatal(err)
	}
	if d.Color != "amber" {
		t.Errorf("Color = %q, want amber", d.Color)
	}
}

func TestNewDeckFormHasEverySwatch(t *testing.T) {
	s := newServer(t)
	doc := s.Get(t, s.Alice, "/flash/new")
	swatches := doc.QueryAll(`input[name=color]`)
	if len(swatches) != len(flash.DeckColors) {
		t.Fatalf("new deck form has %d colour inputs, want %d", len(swatches), len(flash.DeckColors))
	}
	checked := doc.MustHave(`input[value=teal]`)
	if _, ok := htmlassert.Attr(checked, "checked"); !ok {
		t.Error("the default colour's swatch is not pre-checked")
	}
}

func TestDeckListShowsColorAndDueBadge(t *testing.T) {
	s := newServer(t)
	deck, err := s.Store.CreateDeck(t.Context(), s.Alice.User.ID, "Spanish", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Store.SetDeckColor(t.Context(), s.Alice.User.ID, deck.ID, "blue"); err != nil {
		t.Fatal(err)
	}
	for _, front := range []string{"hola", "adiós"} {
		if _, err := s.Store.CreateCard(t.Context(), s.Alice.User.ID, deck.ID, flash.CardTypeBasic, front, "x", ""); err != nil {
			t.Fatal(err)
		}
	}

	doc := s.Get(t, s.Alice, "/flash/")
	doc.MustHave(`#deck-list a.deck-row`)
	doc.MustHave(`#deck-list .deck-c-blue`)
	badge := doc.MustHave(`#deck-list .flash-due-badge`)
	if got := htmlassert.Text(badge); got != "2" {
		t.Errorf("due badge = %q, want 2", got)
	}
	reviewAll := doc.MustHave(`#flash-review-all`)
	if !strings.Contains(htmlassert.Text(reviewAll), "2") {
		t.Errorf("Review all = %q, want it to show the 2 cards due", htmlassert.Text(reviewAll))
	}
}

func TestDeckPaneShowsReviewButtonWithCount(t *testing.T) {
	s := newServer(t)
	deck, err := s.Store.CreateDeck(t.Context(), s.Alice.User.ID, "Spanish", "")
	if err != nil {
		t.Fatal(err)
	}
	for _, front := range []string{"hola", "adiós", "gracias"} {
		if _, err := s.Store.CreateCard(t.Context(), s.Alice.User.ID, deck.ID, flash.CardTypeBasic, front, "x", ""); err != nil {
			t.Fatal(err)
		}
	}

	doc := s.Get(t, s.Alice, "/flash/"+itoa(deck.ID))
	cta := doc.MustHave(`a.flash-review-cta`)
	if href, _ := htmlassert.Attr(cta, "href"); href != "/flash/review/"+itoa(deck.ID) {
		t.Errorf("Review button href = %q", href)
	}
	if got := htmlassert.Text(cta); !strings.Contains(got, "Review 3 cards") {
		t.Errorf("Review button text = %q, want it to say Review 3 cards", got)
	}
	doc.MustNotHave(`.flash-all-done`)
	if tiles := doc.QueryAll(`.flash-deck-tiles .flash-stat-tile`); len(tiles) != 4 {
		t.Errorf("deck pane has %d tiles, want 4", len(tiles))
	}
}

func TestDeckPaneShowsAllDoneWhenNothingIsDue(t *testing.T) {
	s := newServer(t)
	deck, err := s.Store.CreateDeck(t.Context(), s.Alice.User.ID, "Spanish", "")
	if err != nil {
		t.Fatal(err)
	}
	card, err := s.Store.CreateCard(t.Context(), s.Alice.User.ID, deck.ID, flash.CardTypeBasic, "hola", "hello", "")
	if err != nil {
		t.Fatal(err)
	}
	// Easy on a new card schedules it days out, so nothing is due now.
	if _, err := s.Store.GradeCard(t.Context(), s.Alice.User.ID, card.ID, flash.RatingEasy, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}

	doc := s.Get(t, s.Alice, "/flash/"+itoa(deck.ID))
	doc.MustNotHave(`a.flash-review-cta`)
	done := doc.MustHave(`.flash-all-done`)
	if !strings.Contains(htmlassert.Text(done), "All done for today") {
		t.Errorf("all-done panel = %q", htmlassert.Text(done))
	}
}

func TestDeckPaneForAnEmptyDeckOffersToAddCards(t *testing.T) {
	s := newServer(t)
	deck, err := s.Store.CreateDeck(t.Context(), s.Alice.User.ID, "Spanish", "")
	if err != nil {
		t.Fatal(err)
	}
	doc := s.Get(t, s.Alice, "/flash/"+itoa(deck.ID))
	add := doc.MustHave(`a.flash-add-first-card`)
	if href, _ := htmlassert.Attr(add, "href"); href != "/flash/"+itoa(deck.ID)+"/cards/new" {
		t.Errorf("add-cards href = %q", href)
	}
}

func TestFirstRunShowsWelcome(t *testing.T) {
	s := newServer(t)
	doc := s.Get(t, s.Alice, "/flash/")
	welcome := doc.MustHave(`.flash-welcome`)
	if !strings.Contains(htmlassert.Text(welcome), "Make your first deck") {
		t.Errorf("welcome text = %q", htmlassert.Text(welcome))
	}
	doc.MustHave(`.flash-welcome a[href="/flash/new"]`)
	doc.MustHave(`.flash-welcome a[href="/flash/import"]`)
}

func TestHomeWithDecksButNoneSelectedAsksToPick(t *testing.T) {
	s := newServer(t)
	if _, err := s.Store.CreateDeck(t.Context(), s.Alice.User.ID, "Spanish", ""); err != nil {
		t.Fatal(err)
	}
	doc := s.Get(t, s.Alice, "/flash/")
	doc.MustNotHave(`.flash-welcome`)
	doc.MustHave(`.flash-pane-hint`)
}

func TestHomeToolbarLinks(t *testing.T) {
	s := newServer(t)
	doc := s.Get(t, s.Alice, "/flash/")
	for _, sel := range []string{
		`.flash-home-toolbar a[href="/flash/new"]`,
		`.flash-home-toolbar a[href="/flash/import"]`,
		`.flash-home-toolbar a[href="/flash/review"]`,
		`.flash-home-toolbar a[href="/flash/stats"]`,
	} {
		doc.MustHave(sel)
	}
}

func TestDeckFragmentCarriesOutOfBandListAndToolbar(t *testing.T) {
	s := newServer(t)
	rec := s.PostHX(t, s.Alice, "/flash/new", url.Values{"name": {"Spanish"}, "color": {"green"}})
	if rec.Code != 201 {
		t.Fatalf("create over HTMX = %d, want 201; body: %s", rec.Code, rec.Body.String())
	}
	doc := htmlassert.Parse(t, rec.Body.String())
	for _, id := range []string{"#deck-list", "#flash-review-all", "#flash-detail-open"} {
		n := doc.MustHave(id)
		if _, ok := htmlassert.Attr(n, "hx-swap-oob"); !ok {
			t.Errorf("%s in the fragment is not marked hx-swap-oob", id)
		}
	}
	doc.MustHave(`#deck-list .deck-c-green`)
}

func httpGet(t *testing.T, path string) *http.Request {
	t.Helper()
	return httptest.NewRequest(http.MethodGet, path, nil)
}

func httpPost(t *testing.T, path string, form url.Values) *http.Request {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return req
}

func itoa(id int64) string { return strconv.FormatInt(id, 10) }

func TestTakeABreakLivesInTheEditPane(t *testing.T) {
	s := newServer(t)
	deck, err := s.Store.CreateDeck(t.Context(), s.Alice.User.ID, "Spanish", "")
	if err != nil {
		t.Fatal(err)
	}
	view := s.Get(t, s.Alice, "/flash/"+itoa(deck.ID))
	view.MustNotHave(`form[action="/flash/` + itoa(deck.ID) + `/snooze"]`)

	edit := s.Get(t, s.Alice, "/flash/edit/"+itoa(deck.ID))
	if n := len(edit.QueryAll(`form[action="/flash/` + itoa(deck.ID) + `/snooze"]`)); n != 2 {
		t.Errorf("edit pane has %d snooze forms, want 2 (1 week, 1 month)", n)
	}
}

func TestSnoozedDeckShowsABreakBanner(t *testing.T) {
	s := newServer(t)
	deck, err := s.Store.CreateDeck(t.Context(), s.Alice.User.ID, "Spanish", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Store.CreateCard(t.Context(), s.Alice.User.ID, deck.ID, flash.CardTypeBasic, "hola", "hello", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Store.SnoozeDeck(t.Context(), s.Alice.User.ID, deck.ID, time.Now().Add(72*time.Hour)); err != nil {
		t.Fatal(err)
	}
	doc := s.Get(t, s.Alice, "/flash/"+itoa(deck.ID))
	banner := doc.MustHave(".flash-break-banner")
	if !strings.Contains(htmlassert.Text(banner), "Taking a break until") {
		t.Errorf("banner = %q", htmlassert.Text(banner))
	}
	doc.MustHave(`.flash-break-banner form[action="/flash/` + itoa(deck.ID) + `/unsnooze"]`)
	doc.MustNotHave("a.flash-review-cta")

	edit := s.Get(t, s.Alice, "/flash/edit/"+itoa(deck.ID))
	edit.MustHave(`form[action="/flash/` + itoa(deck.ID) + `/unsnooze"]`)
}
