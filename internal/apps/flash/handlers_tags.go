// internal/apps/flash/handlers_tags.go
package flash

import "net/http"

type tagFilterItem struct {
	Card Card
	Deck Deck
}

type tagFilterView struct {
	TagName string
	Items   []tagFilterItem
}

// tagFilter is Flash's cross-deck view: every card tagged tagName, regardless
// of which deck it lives in.
func (a *App) tagFilter(w http.ResponseWriter, r *http.Request) {
	userID, ok := a.userID(w, r)
	if !ok {
		return
	}
	tagName := r.PathValue("tagName")
	cards, err := a.store.CardsByTag(r.Context(), userID, tagName)
	if err != nil {
		a.deps.Errors.Internal(w, r, err)
		return
	}

	items := make([]tagFilterItem, 0, len(cards))
	for _, c := range cards {
		d, err := a.store.DeckByID(r.Context(), userID, c.DeckID)
		if err != nil {
			a.deps.Errors.Internal(w, r, err)
			return
		}
		items = append(items, tagFilterItem{Card: c, Deck: d})
	}

	page := a.deps.Page(r, "Tag: "+tagName)
	page.Data = tagFilterView{TagName: tagName, Items: items}
	a.render(w, r, http.StatusOK, "flash/tag-filter", page)
}
