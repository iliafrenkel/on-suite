// internal/apps/flash/flash.go
package flash

import (
	"embed"
	"io/fs"

	"github.com/iliafrenkel/on-suite/internal/platform/app"
)

//go:embed templates/*.html
var templateFiles embed.FS

// App is ON Flash. It is constructed before the platform exists, in the
// registration slice in main, and receives everything it needs in Mount.
type App struct {
	store *Store
	deps  app.Deps
}

// New returns the app for registration.
func New() *App { return &App{} }

func (a *App) Meta() app.Meta {
	return app.Meta{
		ID:      ID,
		Name:    "ON Flash",
		Summary: "Create and review flash card decks.",
		Order:   40,
	}
}

func (a *App) Migrations() fs.FS { return Migrations() }

func (a *App) Templates() fs.FS {
	sub, err := fs.Sub(templateFiles, "templates")
	if err != nil {
		// Unreachable: a compile-time constant path checked by go:embed.
		panic("flash: embedded templates missing: " + err.Error())
	}
	return sub
}

// Mount wires the app up. Everything registered with Handle requires a
// signed-in user; ON Flash has no public routes in F1.
func (a *App) Mount(r *app.Router, deps app.Deps) {
	a.deps = deps
	a.store = NewStore(deps.DB)

	// Same pattern as internal/apps/paste's Mount: a literal segment (new,
	// edit/{id}) always wins over a same-position wildcard ({id}), so these
	// do not conflict with each other. See paste.go's Mount for the fuller
	// explanation of which shapes would.
	r.HandleFunc("GET /{$}", a.deckIndex)
	r.HandleFunc("GET /new", a.newDeckForm)
	r.HandleFunc("POST /new", a.createDeck)
	r.HandleFunc("GET /{deckID}", a.deckIndex)
	r.HandleFunc("GET /edit/{deckID}", a.editDeckForm)
	r.HandleFunc("POST /{deckID}", a.updateDeck)
	r.HandleFunc("POST /{deckID}/delete", a.deleteDeck)

	// Card routes, nested under a deck. Same non-ambiguity reasoning as the
	// deck routes above: "new" and "edit/{cardID}" are literal at the
	// position where a same-shape pattern below has a wildcard, so they
	// resolve deterministically; "cards/{cardID}" and
	// "cards/{cardID}/delete" differ in segment count, and
	// "edit/{cardID}" (GET) vs "{cardID}/delete" (POST) differ in method,
	// so neither pair can collide the way paste.go's own comment warns
	// about.
	r.HandleFunc("GET /{deckID}/cards/{$}", a.cardIndex)
	r.HandleFunc("GET /{deckID}/cards/new", a.newCardForm)
	r.HandleFunc("POST /{deckID}/cards/new", a.createCard)
	r.HandleFunc("GET /{deckID}/cards/{cardID}", a.cardIndex)
	r.HandleFunc("GET /{deckID}/cards/edit/{cardID}", a.editCardForm)
	r.HandleFunc("POST /{deckID}/cards/{cardID}", a.updateCard)
	r.HandleFunc("POST /{deckID}/cards/{cardID}/delete", a.deleteCard)

	// GET /tags/{tagName} is 2 segments (literal "tags", wildcard), a different
	// shape from GET /{deckID} (1 segment) and GET /edit/{deckID} (literal
	// "edit", wildcard) — no ambiguity with either.
	r.HandleFunc("GET /tags/{tagName}", a.tagFilter)
}
