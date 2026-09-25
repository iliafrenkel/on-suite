// internal/apps/flash/handlers_cards_test.go
package flash_test

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/iliafrenkel/on-suite/internal/apps/flash"
	"github.com/iliafrenkel/on-suite/internal/apptest"
	"github.com/iliafrenkel/on-suite/internal/htmlassert"
)

func TestCreateAndViewCard(t *testing.T) {
	s := newServer(t)
	deck, err := s.Store.CreateDeck(t.Context(), s.Alice.User.ID, "Spanish", "", flash.DefaultDeckColor)
	if err != nil {
		t.Fatal(err)
	}

	s.Submit(t, s.Alice,
		"/flash/"+itoa(deck.ID)+"/cards/new",
		url.Values{"card_type": {flash.CardTypeBasic}, "front": {"hola"}, "back": {"hello"}},
		"/flash/"+itoa(deck.ID)+"/cards/1")

	doc := s.Get(t, s.Alice, "/flash/"+itoa(deck.ID)+"/cards/")
	doc.MustHave("#card-grid")
}

func TestCreateCardValidation(t *testing.T) {
	s := newServer(t)
	deck, err := s.Store.CreateDeck(t.Context(), s.Alice.User.ID, "Spanish", "", flash.DefaultDeckColor)
	if err != nil {
		t.Fatal(err)
	}
	rec := s.Post(t, s.Alice, "/flash/"+itoa(deck.ID)+"/cards/new",
		url.Values{"card_type": {flash.CardTypeBasic}, "front": {"hola"}})
	if rec.Code != 400 {
		t.Errorf("creating a basic card with no back = %d, want 400", rec.Code)
	}
}

func TestCreatingACardInSomeoneElsesDeckIs404(t *testing.T) {
	s := newServer(t)
	deck, err := s.Store.CreateDeck(t.Context(), s.Alice.User.ID, "alice's", "", flash.DefaultDeckColor)
	if err != nil {
		t.Fatal(err)
	}
	rec := s.PostHX(t, s.Bob, "/flash/"+itoa(deck.ID)+"/cards/new",
		url.Values{"card_type": {flash.CardTypeBasic}, "front": {"a"}, "back": {"b"}})
	if rec.Code != 404 {
		t.Errorf("creating a card in someone else's deck = %d, want 404", rec.Code)
	}
}

func TestCardsModeRendersInsideTheHomeLayout(t *testing.T) {
	s := newServer(t)
	deck, err := s.Store.CreateDeck(t.Context(), s.Alice.User.ID, "Spanish", "", flash.DefaultDeckColor)
	if err != nil {
		t.Fatal(err)
	}
	for _, front := range []string{"hola", "adiós"} {
		if _, err := s.Store.CreateCard(t.Context(), s.Alice.User.ID, deck.ID, flash.CardTypeBasic, front, "x", ""); err != nil {
			t.Fatal(err)
		}
	}
	doc := s.Get(t, s.Alice, "/flash/"+itoa(deck.ID)+"/cards/")
	doc.MustHave("#deck-list")
	doc.MustHave(`#deck-list a.deck-row-active`)
	tiles := doc.QueryAll("#card-grid .flash-mini-card")
	if len(tiles) != 3 {
		t.Fatalf("grid has %d tiles, want 3 (New card + 2 cards)", len(tiles))
	}
	if href, _ := htmlassert.Attr(tiles[0], "href"); href != "/flash/"+itoa(deck.ID)+"/cards/new" {
		t.Errorf("first tile href = %q, want the New card form", href)
	}
	// htmlassert has no compound selectors (a.class[attr]); select by class,
	// then check the attribute.
	active := doc.MustHave(`.flash-deck-toolbar .toolbar-btn-active`)
	if href, _ := htmlassert.Attr(active, "href"); href != "/flash/"+itoa(deck.ID)+"/cards/" {
		t.Errorf("active toolbar button href = %q, want the Cards button", href)
	}
}

func TestCardGridStatusLabels(t *testing.T) {
	s := newServer(t)
	deck, err := s.Store.CreateDeck(t.Context(), s.Alice.User.ID, "Spanish", "", flash.DefaultDeckColor)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Store.CreateCard(t.Context(), s.Alice.User.ID, deck.ID, flash.CardTypeBasic, "fresh", "x", ""); err != nil {
		t.Fatal(err)
	}
	graded, err := s.Store.CreateCard(t.Context(), s.Alice.User.ID, deck.ID, flash.CardTypeBasic, "graded", "x", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Store.GradeCard(t.Context(), s.Alice.User.ID, graded.ID, flash.RatingAgain, time.Now().UTC().Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	doc := s.Get(t, s.Alice, "/flash/"+itoa(deck.ID)+"/cards/")
	var labels []string
	for _, n := range doc.QueryAll("#card-grid .flash-mini-status") {
		labels = append(labels, htmlassert.Text(n))
	}
	if strings.Join(labels, ",") != "due,new" { // ListCards is newest first
		t.Errorf("status labels = %v, want [due new]", labels)
	}
}

func TestCardsSearchAndTagFilter(t *testing.T) {
	s := newServer(t)
	deck, err := s.Store.CreateDeck(t.Context(), s.Alice.User.ID, "Spanish", "", flash.DefaultDeckColor)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct{ front, tags string }{{"hola", "greetings"}, {"pan", "food"}, {"agua", "food"}} {
		s.Post(t, s.Alice, "/flash/"+itoa(deck.ID)+"/cards/new",
			url.Values{"card_type": {"basic"}, "front": {c.front}, "back": {"x"}, "tags": {c.tags}})
	}
	count := func(path string) int {
		return len(s.Get(t, s.Alice, path).QueryAll("#card-grid .flash-mini-card")) - 1 // minus the New card tile
	}
	base := "/flash/" + itoa(deck.ID) + "/cards/"
	if n := count(base + "?q=HOL"); n != 1 {
		t.Errorf("q=HOL shows %d cards, want 1", n)
	}
	if n := count(base + "?tag=food"); n != 2 {
		t.Errorf("tag=food shows %d cards, want 2", n)
	}
	if n := count(base + "?tag=food&q=agua"); n != 1 {
		t.Errorf("tag=food&q=agua shows %d cards, want 1", n)
	}

	doc := s.Get(t, s.Alice, base+"?tag=food")
	active := doc.MustHave("#card-tag-pills .flash-pill-active")
	if htmlassert.Text(active) != "food" {
		t.Errorf("active pill = %q, want food", htmlassert.Text(active))
	}
	allDecks := doc.MustHave(`#card-tag-pills .flash-all-decks-link`)
	if href, _ := htmlassert.Attr(allDecks, "href"); href != "/flash/tags/food" {
		t.Errorf("all-decks link href = %q", href)
	}

	empty := s.Get(t, s.Alice, base+"?q=zzz")
	empty.MustHave("#card-grid .flash-grid-empty")
}

func TestCardGridFragmentForLiveSearch(t *testing.T) {
	s := newServer(t)
	deck, err := s.Store.CreateDeck(t.Context(), s.Alice.User.ID, "Spanish", "", flash.DefaultDeckColor)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Store.CreateCard(t.Context(), s.Alice.User.ID, deck.ID, flash.CardTypeBasic, "hola", "hello", ""); err != nil {
		t.Fatal(err)
	}
	req := httpGet(t, "/flash/"+itoa(deck.ID)+"/cards/grid?q=hol")
	req.Header.Set("HX-Request", "true")
	rec := s.Do(t, s.Alice, req)
	if rec.Code != 200 {
		t.Fatalf("grid fragment = %d; body: %s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("HX-Replace-Url"); got != "/flash/"+itoa(deck.ID)+"/cards/?q=hol" {
		t.Errorf("HX-Replace-Url = %q", got)
	}
	doc := htmlassert.Parse(t, rec.Body.String())
	doc.MustHave("#card-grid")
	pills := doc.MustHave("#card-tag-pills")
	if _, ok := htmlassert.Attr(pills, "hx-swap-oob"); !ok {
		t.Error("#card-tag-pills in the grid fragment is not marked hx-swap-oob")
	}
	doc.MustNotHave("#deck-list")
}

func TestOpenedCardIsFlippable(t *testing.T) {
	s := newServer(t)
	deck, err := s.Store.CreateDeck(t.Context(), s.Alice.User.ID, "Spanish", "", flash.DefaultDeckColor)
	if err != nil {
		t.Fatal(err)
	}
	c, err := s.Store.CreateCard(t.Context(), s.Alice.User.ID, deck.ID, flash.CardTypeBasic, "hola", "hello", "informal")
	if err != nil {
		t.Fatal(err)
	}
	doc := s.Get(t, s.Alice, "/flash/"+itoa(deck.ID)+"/cards/"+itoa(c.ID))
	flip := doc.MustHave(".flash-viewer input.flash-flip")
	id, _ := htmlassert.Attr(flip, "id")
	label := doc.MustHave(".flash-viewer label.flash-card")
	if f, _ := htmlassert.Attr(label, "for"); f != id {
		t.Errorf("card label for=%q, checkbox id=%q; they must match for the flip to work", f, id)
	}
	if got := htmlassert.Text(doc.MustHave(".flash-card-front")); !strings.Contains(got, "hola") {
		t.Errorf("front = %q", got)
	}
	if got := htmlassert.Text(doc.MustHave(".flash-card-back")); !strings.Contains(got, "hello") || !strings.Contains(got, "informal") {
		t.Errorf("back = %q, want the answer and the note", got)
	}
	edit := doc.MustHave(`a.flash-card-edit`)
	if href, _ := htmlassert.Attr(edit, "href"); href != "/flash/"+itoa(deck.ID)+"/cards/edit/"+itoa(c.ID) {
		t.Errorf("Edit card href = %q", href)
	}
}

func TestOpenedClozeCardShowsBlankThenAnswer(t *testing.T) {
	s := newServer(t)
	deck, err := s.Store.CreateDeck(t.Context(), s.Alice.User.ID, "Geography", "", flash.DefaultDeckColor)
	if err != nil {
		t.Fatal(err)
	}
	c, err := s.Store.CreateCard(t.Context(), s.Alice.User.ID, deck.ID, flash.CardTypeCloze, "The capital of France is {{c1::Paris}}.", "", "")
	if err != nil {
		t.Fatal(err)
	}
	doc := s.Get(t, s.Alice, "/flash/"+itoa(deck.ID)+"/cards/"+itoa(c.ID))
	front := doc.MustHave(".flash-card-front")
	if strings.Contains(htmlassert.Text(front), "Paris") {
		t.Errorf("front shows the answer: %q", htmlassert.Text(front))
	}
	doc.MustHave(".flash-card-front .flash-blank")
	if got := htmlassert.Text(doc.MustHave(".flash-card-back mark.flash-fill")); got != "Paris" {
		t.Errorf("filled answer = %q, want Paris", got)
	}
}

func TestOpenedCardPrevNextFollowTheFilter(t *testing.T) {
	s := newServer(t)
	deck, err := s.Store.CreateDeck(t.Context(), s.Alice.User.ID, "Spanish", "", flash.DefaultDeckColor)
	if err != nil {
		t.Fatal(err)
	}
	// Card ids are 1, 2, 3 in creation order (a fresh database).
	for _, c := range []struct{ front, tags string }{{"one", "food"}, {"two", "other"}, {"three", "food"}} {
		rec := s.Post(t, s.Alice, "/flash/"+itoa(deck.ID)+"/cards/new",
			url.Values{"card_type": {"basic"}, "front": {c.front}, "back": {"x"}, "tags": {c.tags}})
		if rec.Code != 303 {
			t.Fatalf("create: %d", rec.Code)
		}
	}
	// ListCards is newest first: three(3), two(2), one(1). Filtered by food:
	// three, one.
	doc := s.Get(t, s.Alice, "/flash/"+itoa(deck.ID)+"/cards/3?tag=food")
	doc.MustNotHave(".flash-card-prev")
	next := doc.MustHave("a.flash-card-next")
	if href, _ := htmlassert.Attr(next, "href"); href != "/flash/"+itoa(deck.ID)+"/cards/1?tag=food" {
		t.Errorf("next href = %q, want card 1 (skipping untagged card 2)", href)
	}
	back := doc.MustHave("a.flash-card-back-to-grid")
	if href, _ := htmlassert.Attr(back, "href"); href != "/flash/"+itoa(deck.ID)+"/cards/?tag=food" {
		t.Errorf("All cards href = %q, want the filtered grid", href)
	}
}

func TestDeleteCardReturnsToTheGrid(t *testing.T) {
	s := newServer(t)
	deck, err := s.Store.CreateDeck(t.Context(), s.Alice.User.ID, "Spanish", "", flash.DefaultDeckColor)
	if err != nil {
		t.Fatal(err)
	}
	c, err := s.Store.CreateCard(t.Context(), s.Alice.User.ID, deck.ID, flash.CardTypeBasic, "hola", "hello", "")
	if err != nil {
		t.Fatal(err)
	}

	rec := s.PostHX(t, s.Alice, "/flash/"+itoa(deck.ID)+"/cards/"+itoa(c.ID)+"/delete", url.Values{})
	if rec.Code != 200 {
		t.Fatalf("delete card = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("HX-Push-Url"); got != "/flash/"+itoa(deck.ID)+"/cards/" {
		t.Errorf("HX-Push-Url = %q, want the deck's cards grid URL", got)
	}
	doc := htmlassert.Parse(t, rec.Body.String())
	doc.MustHave("#card-grid")
	doc.MustNotHave(".flash-viewer")
	if _, err := s.Store.CardByID(t.Context(), s.Alice.User.ID, deck.ID, c.ID); err == nil {
		t.Error("card still exists after delete")
	}
}

func TestCardEditFormRendersPrefilledValues(t *testing.T) {
	s := newServer(t)
	deck, err := s.Store.CreateDeck(t.Context(), s.Alice.User.ID, "Spanish", "", flash.DefaultDeckColor)
	if err != nil {
		t.Fatal(err)
	}
	c, err := s.Store.CreateCard(t.Context(), s.Alice.User.ID, deck.ID, flash.CardTypeBasic, "hola", "hello", "informal")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Store.SetCardTags(t.Context(), s.Alice.User.ID, c.ID, []string{"basics", "greetings"}); err != nil {
		t.Fatal(err)
	}

	doc := s.Get(t, s.Alice, "/flash/"+itoa(deck.ID)+"/cards/edit/"+itoa(c.ID))
	doc.MustHave("#card-detail-edit")

	front := doc.MustHave("#card-front-" + itoa(c.ID))
	if got := htmlassert.Text(front); got != "hola" {
		t.Errorf("front value = %q, want hola", got)
	}
	back := doc.MustHave("#card-back-" + itoa(c.ID))
	if got := htmlassert.Text(back); got != "hello" {
		t.Errorf("back value = %q, want hello", got)
	}
	notes := doc.MustHave("#card-notes-" + itoa(c.ID))
	if got := htmlassert.Text(notes); got != "informal" {
		t.Errorf("notes value = %q, want informal", got)
	}
	tags := doc.MustHave("#card-tags-" + itoa(c.ID))
	if got, _ := htmlassert.Attr(tags, "value"); got != "basics, greetings" {
		t.Errorf("tags value = %q, want %q", got, "basics, greetings")
	}
}

// filteredThreeCards creates three cards (ids 1,2,3 in a fresh deck) with
// "one" and "three" tagged "food", mirroring
// TestOpenedCardPrevNextFollowTheFilter's fixture, and returns the deck.
func filteredThreeCards(t *testing.T, s *apptest.Server[*flash.Store]) flash.Deck {
	t.Helper()
	deck, err := s.Store.CreateDeck(t.Context(), s.Alice.User.ID, "Spanish", "", flash.DefaultDeckColor)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct{ front, tags string }{{"one", "food"}, {"two", "other"}, {"three", "food"}} {
		rec := s.Post(t, s.Alice, "/flash/"+itoa(deck.ID)+"/cards/new",
			url.Values{"card_type": {"basic"}, "front": {c.front}, "back": {"x"}, "tags": {c.tags}})
		if rec.Code != 303 {
			t.Fatalf("create: %d", rec.Code)
		}
	}
	return deck
}

func TestEditLinkAndEditFormKeepTheFilter(t *testing.T) {
	s := newServer(t)
	deck := filteredThreeCards(t, s)

	doc := s.Get(t, s.Alice, "/flash/"+itoa(deck.ID)+"/cards/3?tag=food")
	edit := doc.MustHave("a.flash-card-edit")
	if href, _ := htmlassert.Attr(edit, "href"); href != "/flash/"+itoa(deck.ID)+"/cards/edit/3?tag=food" {
		t.Errorf("Edit card href = %q, want the filter kept", href)
	}

	form := s.Get(t, s.Alice, "/flash/"+itoa(deck.ID)+"/cards/edit/3?tag=food")
	editForm := form.MustHave("#card-detail-edit")
	if action, _ := htmlassert.Attr(editForm, "action"); action != "/flash/"+itoa(deck.ID)+"/cards/3?tag=food" {
		t.Errorf("edit form action = %q, want the filter kept", action)
	}
	back := form.MustHave("#card-detail-edit a.toolbar-btn")
	if href, _ := htmlassert.Attr(back, "href"); href != "/flash/"+itoa(deck.ID)+"/cards/3?tag=food" {
		t.Errorf("Back to the card href = %q, want the filter kept", href)
	}
}

func TestSaveEditKeepsFilterOverHTMX(t *testing.T) {
	s := newServer(t)
	deck := filteredThreeCards(t, s)

	rec := s.PostHX(t, s.Alice, "/flash/"+itoa(deck.ID)+"/cards/3?tag=food",
		url.Values{"card_type": {"basic"}, "front": {"tres"}, "back": {"x"}, "tags": {"food"}})
	if rec.Code != 200 {
		t.Fatalf("save edit = %d; body: %s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("HX-Push-Url"); got != "/flash/"+itoa(deck.ID)+"/cards/3?tag=food" {
		t.Errorf("HX-Push-Url = %q, want the filter kept", got)
	}
	doc := htmlassert.Parse(t, rec.Body.String())
	next := doc.MustHave("a.flash-card-next")
	if href, _ := htmlassert.Attr(next, "href"); href != "/flash/"+itoa(deck.ID)+"/cards/1?tag=food" {
		t.Errorf("next href = %q, want it to stay within the filtered set", href)
	}
}

func TestSaveEditKeepsFilterWithoutJS(t *testing.T) {
	s := newServer(t)
	deck := filteredThreeCards(t, s)

	s.Submit(t, s.Alice, "/flash/"+itoa(deck.ID)+"/cards/3?tag=food",
		url.Values{"card_type": {"basic"}, "front": {"tres"}, "back": {"x"}, "tags": {"food"}},
		"/flash/"+itoa(deck.ID)+"/cards/3?tag=food")
}

func TestEditValidationErrorKeepsFilter(t *testing.T) {
	s := newServer(t)
	deck := filteredThreeCards(t, s)

	rec := s.Post(t, s.Alice, "/flash/"+itoa(deck.ID)+"/cards/3?tag=food",
		url.Values{"card_type": {"basic"}, "front": {"tres"}})
	if rec.Code != 400 {
		t.Fatalf("save edit with no back = %d, want 400; body: %s", rec.Code, rec.Body.String())
	}
	doc := htmlassert.Parse(t, rec.Body.String())
	editForm := doc.MustHave("#card-detail-edit")
	if action, _ := htmlassert.Attr(editForm, "action"); action != "/flash/"+itoa(deck.ID)+"/cards/3?tag=food" {
		t.Errorf("re-rendered edit form action = %q, want the filter kept", action)
	}
}

func TestCancelEditReturnsToTheFilteredCard(t *testing.T) {
	s := newServer(t)
	deck := filteredThreeCards(t, s)

	doc := s.Get(t, s.Alice, "/flash/"+itoa(deck.ID)+"/cards/edit/3?tag=food")
	cancel := doc.QueryAll(`a[href="/flash/` + itoa(deck.ID) + `/cards/3?tag=food"]`)
	if len(cancel) == 0 {
		t.Error("no Cancel/back link to the filtered card")
	}
}

func TestDeleteCardKeepsFilterAndReturnsToTheFilteredGrid(t *testing.T) {
	s := newServer(t)
	deck := filteredThreeCards(t, s)

	doc := s.Get(t, s.Alice, "/flash/"+itoa(deck.ID)+"/cards/3?tag=food")
	del := doc.MustHave(".flash-deck-toolbar form")
	if action, _ := htmlassert.Attr(del, "action"); action != "/flash/"+itoa(deck.ID)+"/cards/3/delete?tag=food" {
		t.Errorf("delete form action = %q, want the filter kept", action)
	}

	rec := s.PostHX(t, s.Alice, "/flash/"+itoa(deck.ID)+"/cards/3/delete?tag=food", url.Values{})
	if rec.Code != 200 {
		t.Fatalf("delete card = %d; body: %s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("HX-Push-Url"); got != "/flash/"+itoa(deck.ID)+"/cards/?tag=food" {
		t.Errorf("HX-Push-Url = %q, want the filtered grid, not the next card", got)
	}
	resultDoc := htmlassert.Parse(t, rec.Body.String())
	resultDoc.MustHave("#card-grid")
	resultDoc.MustNotHave(".flash-viewer")
}

func TestDeleteCardKeepsFilterWithoutJS(t *testing.T) {
	s := newServer(t)
	deck := filteredThreeCards(t, s)

	s.Submit(t, s.Alice, "/flash/"+itoa(deck.ID)+"/cards/3/delete?tag=food", url.Values{},
		"/flash/"+itoa(deck.ID)+"/cards/?tag=food")
}

func TestNewCardTileCarriesTheFilterForCancel(t *testing.T) {
	s := newServer(t)
	deck := filteredThreeCards(t, s)

	doc := s.Get(t, s.Alice, "/flash/"+itoa(deck.ID)+"/cards/?tag=food")
	newTile := doc.MustHave("a.flash-mini-new")
	href, _ := htmlassert.Attr(newTile, "href")
	if href != "/flash/"+itoa(deck.ID)+"/cards/new?tag=food" {
		t.Errorf("New card tile href = %q, want the filter kept", href)
	}

	form := s.Get(t, s.Alice, href)
	newForm := form.MustHave("#card-detail-new")
	if action, _ := htmlassert.Attr(newForm, "action"); action != "/flash/"+itoa(deck.ID)+"/cards/new?tag=food" {
		t.Errorf("new card form action = %q; the new card itself is not filtered, but should still carry the origin", action)
	}
	cancel := form.QueryAll(`a[href="/flash/` + itoa(deck.ID) + `/cards/?tag=food"]`)
	if len(cancel) == 0 {
		t.Error("no Cancel/All cards link to the filtered grid")
	}
}

func TestUnfilteredCardFlowStaysUnfiltered(t *testing.T) {
	s := newServer(t)
	deck, err := s.Store.CreateDeck(t.Context(), s.Alice.User.ID, "Spanish", "", flash.DefaultDeckColor)
	if err != nil {
		t.Fatal(err)
	}
	c, err := s.Store.CreateCard(t.Context(), s.Alice.User.ID, deck.ID, flash.CardTypeBasic, "hola", "hello", "")
	if err != nil {
		t.Fatal(err)
	}

	doc := s.Get(t, s.Alice, "/flash/"+itoa(deck.ID)+"/cards/"+itoa(c.ID))
	edit := doc.MustHave("a.flash-card-edit")
	if href, _ := htmlassert.Attr(edit, "href"); href != "/flash/"+itoa(deck.ID)+"/cards/edit/"+itoa(c.ID) {
		t.Errorf("Edit card href = %q, want no filter suffix", href)
	}

	gridDoc := s.Get(t, s.Alice, "/flash/"+itoa(deck.ID)+"/cards/")
	newTile := gridDoc.MustHave("a.flash-mini-new")
	if href, _ := htmlassert.Attr(newTile, "href"); href != "/flash/"+itoa(deck.ID)+"/cards/new" {
		t.Errorf("New card tile href = %q, want no filter suffix", href)
	}

	rec := s.PostHX(t, s.Alice, "/flash/"+itoa(deck.ID)+"/cards/"+itoa(c.ID)+"/delete", url.Values{})
	if got := rec.Header().Get("HX-Push-Url"); got != "/flash/"+itoa(deck.ID)+"/cards/" {
		t.Errorf("HX-Push-Url = %q, want no filter suffix", got)
	}
}

// TestSavingANewCardFromAFilteredFormStaysUnfiltered guards the user
// decision in #322: a new card is never filtered by the grid the user came
// from (it may not even match it), so saving one opens it unfiltered, both
// over HTMX and without JS.
func TestSavingANewCardFromAFilteredFormStaysUnfiltered(t *testing.T) {
	s := newServer(t)
	deck := filteredThreeCards(t, s)

	rec := s.PostHX(t, s.Alice, "/flash/"+itoa(deck.ID)+"/cards/new?tag=food",
		url.Values{"card_type": {"basic"}, "front": {"four"}, "back": {"x"}})
	if rec.Code != http.StatusCreated {
		t.Fatalf("create card = %d; body: %s", rec.Code, rec.Body.String())
	}
	wantPush := "/flash/" + itoa(deck.ID) + "/cards/4"
	if got := rec.Header().Get("HX-Push-Url"); got != wantPush {
		t.Errorf("HX-Push-Url = %q, want %q (no filter)", got, wantPush)
	}
	doc := htmlassert.Parse(t, rec.Body.String())
	assertNoFilterOnOpenedCard(t, doc)
}

func TestSavingANewCardFromAFilteredFormStaysUnfilteredWithoutJS(t *testing.T) {
	s := newServer(t)
	deck := filteredThreeCards(t, s)

	want := "/flash/" + itoa(deck.ID) + "/cards/4"
	s.Submit(t, s.Alice, "/flash/"+itoa(deck.ID)+"/cards/new?tag=food",
		url.Values{"card_type": {"basic"}, "front": {"four"}, "back": {"x"}}, want)

	doc := s.Get(t, s.Alice, want)
	assertNoFilterOnOpenedCard(t, doc)
}

// assertNoFilterOnOpenedCard checks that none of an opened card's next,
// prev, back or edit links carry a ?q=/?tag= suffix.
func assertNoFilterOnOpenedCard(t *testing.T, doc *htmlassert.Doc) {
	t.Helper()
	for _, sel := range []string{"a.flash-card-edit", "a.flash-card-back-to-grid", "a.flash-card-next", "a.flash-card-prev"} {
		el := doc.Query(sel)
		if el == nil {
			continue
		}
		href, _ := htmlassert.Attr(el, "href")
		if strings.Contains(href, "tag=") || strings.Contains(href, "q=") {
			t.Errorf("%s href = %q, want no filter", sel, href)
		}
	}
}

// TestSaveEditKeepsFilterEncodedThroughMultipart guards #322's filter-carry
// against special characters: q/tag values that contain quotes, "&", "#"
// and non-ASCII must survive an edit save (sent as multipart/form-data, the
// way the real editor posts) URL-encoded on the pushed URL, and properly
// HTML-escaped wherever they're rendered as an href.
func TestSaveEditKeepsFilterEncodedThroughMultipart(t *testing.T) {
	s := newServer(t)
	deck, err := s.Store.CreateDeck(t.Context(), s.Alice.User.ID, "Spanish", "", flash.DefaultDeckColor)
	if err != nil {
		t.Fatal(err)
	}
	postCardForm(t, s, s.Alice, "/flash/"+itoa(deck.ID)+"/cards/new",
		url.Values{"card_type": {"basic"}, "front": {"one"}, "back": {"x"}}, nil)

	q := `o "x" & #é`
	tag := "a&b c"
	filter := url.Values{"q": {q}, "tag": {tag}}.Encode()
	path := "/flash/" + itoa(deck.ID) + "/cards/1?" + filter

	rec := postCardForm(t, s, s.Alice, path,
		url.Values{"card_type": {"basic"}, "front": {"one"}, "back": {"y"}}, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("save edit = %d; body: %s", rec.Code, rec.Body.String())
	}

	wantPush := "/flash/" + itoa(deck.ID) + "/cards/1?" + filter
	if got := rec.Header().Get("HX-Push-Url"); got != wantPush {
		t.Errorf("HX-Push-Url = %q, want %q", got, wantPush)
	}

	body := rec.Body.String()
	// The raw HTML must escape "&" as an entity wherever the filter appears
	// in an href (html/template's auto-escaping), or the "&tag=" separator
	// between q and tag would silently break.
	if !strings.Contains(body, "q=o&#43;%22x%22&#43;%26&#43;%23%C3%A9&amp;tag=a%26b&#43;c") {
		t.Errorf("rendered hrefs don't contain the html-escaped encoded filter; body: %s", body)
	}

	doc := htmlassert.Parse(t, rec.Body.String())
	back := doc.MustHave("a.flash-card-back-to-grid")
	if href, _ := htmlassert.Attr(back, "href"); href != "/flash/"+itoa(deck.ID)+"/cards/?"+filter {
		t.Errorf("back-to-grid href = %q, want the filter kept encoded", href)
	}
	edit := doc.MustHave("a.flash-card-edit")
	if href, _ := htmlassert.Attr(edit, "href"); href != "/flash/"+itoa(deck.ID)+"/cards/edit/1?"+filter {
		t.Errorf("edit href = %q, want the filter kept encoded", href)
	}
}

// TestSaveEditKeepsFilterEncodedWithoutJS is the no-JS counterpart: the
// redirect Location must carry the same URL-encoded filter.
func TestSaveEditKeepsFilterEncodedWithoutJS(t *testing.T) {
	s := newServer(t)
	deck, err := s.Store.CreateDeck(t.Context(), s.Alice.User.ID, "Spanish", "", flash.DefaultDeckColor)
	if err != nil {
		t.Fatal(err)
	}
	postCardForm(t, s, s.Alice, "/flash/"+itoa(deck.ID)+"/cards/new",
		url.Values{"card_type": {"basic"}, "front": {"one"}, "back": {"x"}}, nil)

	q := `o "x" & #é`
	tag := "a&b c"
	filter := url.Values{"q": {q}, "tag": {tag}}.Encode()
	path := "/flash/" + itoa(deck.ID) + "/cards/1?" + filter

	wantLocation := "/flash/" + itoa(deck.ID) + "/cards/1?" + filter
	s.Submit(t, s.Alice, path,
		url.Values{"card_type": {"basic"}, "front": {"one"}, "back": {"z"}}, wantLocation)
}

// TestOpenedCardShowsTagsOnce guards against the back face's plain tag pills
// duplicating the clickable tag links already shown below the card: an
// opened card (not Static) should show the tags exactly once, as links.
func TestOpenedCardShowsTagsOnce(t *testing.T) {
	s := newServer(t)
	deck, err := s.Store.CreateDeck(t.Context(), s.Alice.User.ID, "Spanish", "", flash.DefaultDeckColor)
	if err != nil {
		t.Fatal(err)
	}
	c, err := s.Store.CreateCard(t.Context(), s.Alice.User.ID, deck.ID, flash.CardTypeBasic, "hola", "hello", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Store.SetCardTags(t.Context(), s.Alice.User.ID, c.ID, []string{"greetings"}); err != nil {
		t.Fatal(err)
	}

	doc := s.Get(t, s.Alice, "/flash/"+itoa(deck.ID)+"/cards/"+itoa(c.ID))
	doc.MustNotHave(".flash-card-back .flash-pill")
	link := doc.MustHave(".flash-tag-links a.flash-pill")
	if got := htmlassert.Text(link); got != "greetings" {
		t.Errorf("tag link text = %q, want greetings", got)
	}
}

func TestDeckPaneCardsButtonSwapsThePane(t *testing.T) {
	s := newServer(t)
	deck, err := s.Store.CreateDeck(t.Context(), s.Alice.User.ID, "Spanish", "", flash.DefaultDeckColor)
	if err != nil {
		t.Fatal(err)
	}
	doc := s.Get(t, s.Alice, "/flash/"+itoa(deck.ID))
	btn := doc.MustHave(`.flash-deck-toolbar a[href="/flash/` + itoa(deck.ID) + `/cards/"]`)
	if target, _ := htmlassert.Attr(btn, "hx-target"); target != "#deck-detail" {
		t.Errorf("Cards button hx-target = %q, want #deck-detail", target)
	}
}
