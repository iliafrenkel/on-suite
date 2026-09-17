// internal/apps/flash/flash.go
package flash

import (
	"embed"
	"io/fs"
	"net/http"

	"github.com/iliafrenkel/on-suite/internal/platform/app"
)

//go:embed templates/*.html
var templateFiles embed.FS

//go:embed static/flash-review.js
var scriptFiles embed.FS

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

// script serves flash-review.js, behind the same sign-in requirement as
// every other route here — no page loads it without already being on an
// authenticated Flash page. Mirrors internal/apps/reader's own script
// method for reader.js.
func (a *App) script(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	http.ServeFileFS(w, r, scriptFiles, "static/flash-review.js")
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

	// Two more literal-suffix POST routes on a deck, same non-ambiguity shape
	// as /{deckID}/delete above: a wildcard followed by a distinct literal
	// word, so none of the three can be confused with each other.
	r.HandleFunc("POST /{deckID}/snooze", a.snoozeDeck)
	r.HandleFunc("POST /{deckID}/unsnooze", a.unsnoozeDeck)

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

	// Review routes. "review" and "flash-review.js" are literal single
	// segments alongside the existing wildcard single-segment routes
	// (/{deckID}, /tags/{tagName}'s "tags" is also a literal single segment at
	// this same position but a different word) — literals never conflict with
	// each other or with a same-position wildcard, per the precedent already
	// established by /new and /tags/{tagName} above.
	//
	// The per-deck review route is GET /review/{deckID}, not GET
	// /{deckID}/review: the latter shape is the exact ambiguity paste.go's own
	// Mount warns about, just one level removed — a wildcard-then-literal GET
	// pattern here genuinely conflicts with the existing literal-then-wildcard
	// GET /edit/{deckID} above, since "/flash/edit/review" matches both (as
	// GET /edit/{deckID} with deckID="review", and as GET /{deckID}/review
	// with deckID="edit") and neither is more specific than the other —
	// confirmed by a startup panic during plan validation. GET
	// /review/{deckID} sidesteps this entirely: it is a literal-then-wildcard
	// shape like /edit/{deckID}, just with a different literal, so the two
	// families never collide.
	//
	// Grading and undo take the card id from the POST body (card_id), not the
	// URL path, so their routes are POST /review/grade and POST /review/undo —
	// 2-segment, literal-then-literal. A first attempt used POST
	// /review/{cardID}/grade (3-segment, literal-wildcard-literal) and it
	// structurally conflicted with the existing POST /{deckID}/cards/{cardID}
	// (3-segment, wildcard-literal-wildcard): Go's mux flags any (L,W,L) vs
	// (W,L,W) pair of the same length as ambiguous regardless of which words
	// the literals are, since the path built from the first pattern's own
	// literals — here "/review/cards/grade" — always satisfies both patterns'
	// wildcard slots too, again confirmed by a startup panic during
	// validation. Ending a route in a literal that differs from every other
	// same-length pattern's final literal (as /review/grade and /review/undo
	// do against /{deckID}/delete, /{deckID}/snooze, /{deckID}/unsnooze) is
	// what actually guarantees safety: a literal-vs-literal mismatch at any
	// one position makes overlap impossible no matter how the remaining
	// positions compare.
	r.HandleFunc("GET /review", a.review)
	r.HandleFunc("GET /review/{deckID}", a.deckReview)
	r.HandleFunc("POST /review/grade", a.gradeCardHandler)
	r.HandleFunc("POST /review/undo", a.undoGradeHandler)
	r.HandleFunc("GET /flash-review.js", a.script)
}
