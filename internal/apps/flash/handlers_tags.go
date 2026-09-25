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
	// Normalized once, up front: CardsByTag matches on the normalized name
	// regardless, but the heading/title must show the same form it actually
	// matched against, not the raw path value (#290).
	tagName := normalizeTagName(r.PathValue("tagName"))
	cards, err := a.store.CardsByTag(r.Context(), userID, tagName)
	if err != nil {
		a.deps.Errors.Internal(w, r, err)
		return
	}

	items := make([]cardGridItem, 0, len(cards))
	for _, cd := range cards {
		d := Deck{ID: cd.Card.DeckID, Color: cd.DeckColor}
		items = append(items, cardGridItem{
			Face:     newCardFace(cd.Card, d, nil),
			Href:     cardURL(d.ID, cd.Card.ID, "", ""),
			DeckName: cd.DeckName,
		})
	}

	page := a.deps.Page(r, "Tag: "+tagName)
	page.Data = tagFilterView{TagName: tagName, Items: items}
	a.render(w, r, http.StatusOK, "flash/tag-filter", page)
}
