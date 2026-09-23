// internal/apps/flash/handlers_tags.go
package flash

import "net/http"

// tagFilterView is the cross-deck tag page: every card carrying one tag, as
// mini cards striped in their own deck's colour.
type tagFilterView struct {
	TagName string
	Items   []cardGridItem
}

// tagFilter renders the cross-deck filter page for one tag.
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

	decks := map[int64]Deck{}
	items := make([]cardGridItem, 0, len(cards))
	for _, c := range cards {
		d, ok := decks[c.DeckID]
		if !ok {
			if d, err = a.store.DeckByID(r.Context(), userID, c.DeckID); err != nil {
				a.deps.Errors.Internal(w, r, err)
				return
			}
			decks[c.DeckID] = d
		}
		items = append(items, cardGridItem{
			Face:     newCardFace(c, d, nil),
			Href:     cardURL(d.ID, c.ID, "", ""),
			DeckName: d.Name,
		})
	}

	page := a.deps.Page(r, "Tag: "+tagName)
	page.Data = tagFilterView{TagName: tagName, Items: items}
	a.render(w, r, http.StatusOK, "flash/tag-filter", page)
}
