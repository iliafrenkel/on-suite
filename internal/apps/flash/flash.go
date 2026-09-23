// internal/apps/flash/flash.go
package flash

import (
	"embed"
	"io/fs"
	"net/http"

	"github.com/iliafrenkel/on-suite/internal/platform/app"
	"github.com/iliafrenkel/on-suite/internal/platform/web"
)

//go:embed templates/*.html
var templateFiles embed.FS

//go:embed static/flash.js
var scriptFiles embed.FS

// mediaFetchConcurrency bounds outbound media fetches across all requests,
// the same role internal/apps/reader's imageFetchConcurrency plays for
// article images — without a bound, a deck full of cards with images could
// open many simultaneous connections to one host on first review.
const mediaFetchConcurrency = 4

// App is ON Flash. It is constructed before the platform exists, in the
// registration slice in main, and receives everything it needs in Mount.
type App struct {
	store       *Store
	deps        app.Deps
	mediaClient *MediaClient
	mediaSem    chan struct{}
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

// script serves flash.js, behind the same sign-in requirement as
// every other route here — no page loads it without already being on an
// authenticated Flash page. Mirrors internal/apps/reader's own script
// method for reader.js.
func (a *App) script(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	http.ServeFileFS(w, r, scriptFiles, "static/flash.js")
}

// Mount wires the app up. Everything registered with Handle requires a
// signed-in user; ON Flash has no public routes in F1.
func (a *App) Mount(r *app.Router, deps app.Deps) {
	a.deps = deps
	a.store = NewStore(deps.DB)
	a.mediaClient = NewMediaClient(deps.Version)
	a.mediaSem = make(chan struct{}, mediaFetchConcurrency)

	// Same pattern as internal/apps/paste's Mount: a literal segment (new,
	// edit/{id}) always wins over a same-position wildcard ({id}), so these
	// do not conflict with each other. See paste.go's Mount for the fuller
	// explanation of which shapes would.
	r.HandleFunc("GET /{$}", a.deckIndex)
	r.HandleFunc("GET /new", a.newDeckForm)
	r.HandleFunc("POST /new", a.createDeck)

	// "import" is a literal single segment, the same non-ambiguity shape as
	// "new" and "review" alongside the existing wildcard single-segment
	// routes (/{deckID}) below — literals never conflict with each other or
	// with a same-position wildcard.
	r.HandleFunc("GET /import", a.importForm)
	r.HandleFunc("POST /import", a.importDeck)
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

	// Sharing routes. POST /{deckID}/share is the same wildcard-then-literal
	// shape as /{deckID}/snooze and /{deckID}/unsnooze above and differs from
	// each in its final literal, so it can't collide with them.
	// POST /{deckID}/share/{shareID}/revoke is 4 segments
	// (wildcard-literal-wildcard-literal) — the same shape as
	// /{deckID}/cards/{cardID}/delete and /{deckID}/cards/{cardID}/media
	// below, differing only in its second literal ("share" vs "cards"),
	// which is what actually guarantees no ambiguity between same-shape
	// patterns (see the review-route comment above for the fuller version
	// of this reasoning).
	//
	// The recipient's two actions take the share id from the POST body
	// (share_id), not the URL path, for the same reason review's grade/undo
	// do: a literal-wildcard-literal shape like /shared/{shareID}/adopt
	// would conflict with the existing wildcard-literal-wildcard
	// /{deckID}/cards/{cardID} above — Go's mux flags any (L,W,L) vs (W,L,W)
	// pair of the same length as ambiguous regardless of which words the
	// literals are, confirmed by a startup panic during validation. Making
	// these 2-segment literal-literal routes (POST /shared/adopt, POST
	// /shared/decline) sidesteps the shape entirely, just like
	// /review/grade and /review/undo do.
	r.HandleFunc("POST /{deckID}/share", a.shareDeck)
	r.HandleFunc("POST /{deckID}/share/{shareID}/revoke", a.revokeShareHandler)
	r.HandleFunc("POST /shared/adopt", a.adoptShareHandler)
	r.HandleFunc("POST /shared/decline", a.declineShareHandler)

	// GET /tags/{tagName} is 2 segments (literal "tags", wildcard), a different
	// shape from GET /{deckID} (1 segment) and GET /edit/{deckID} (literal
	// "edit", wildcard) — no ambiguity with either.
	r.HandleFunc("GET /tags/{tagName}", a.tagFilter)

	// Review routes. "review" and "flash.js" are literal single
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
	r.HandleFunc("GET /flash.js", a.script)

	// "stats" is a literal single segment, the same non-ambiguity shape as
	// "new"/"review"/"import"/"tags" alongside the existing wildcard
	// single-segment routes (/{deckID}) above — literals never conflict with
	// each other or with a same-position wildcard, per the precedent already
	// established by those routes.
	r.HandleFunc("GET /stats", a.stats)

	// "media" is a literal single segment, the same non-ambiguity shape as
	// "new"/"review"/"import" alongside the wildcard single-segment routes
	// above. The upload route is 4 segments (wildcard, literal, wildcard,
	// literal) — the same shape as the existing
	// POST /{deckID}/cards/{cardID}/delete, differing only in its final
	// literal ("media" vs "delete"), which is what actually guarantees no
	// ambiguity between the two: a literal-vs-literal mismatch at any one
	// position makes overlap impossible regardless of how the remaining
	// positions compare (see the review-route comment above for the fuller
	// version of this reasoning).
	//
	// The upload route overrides the platform's global 1MB body cap
	// (web.DefaultMaxBodyBytes, applied to every route by the shared
	// middleware stack) with a budget big enough for one image and one
	// audio file in the same multipart request — the same
	// MaxImageFetchBytes+MaxAudioFetchBytes budget uploadCardMedia's own
	// http.MaxBytesReader wrap already uses internally, so the two caps
	// agree. Per csrf.go's own doc comment on DefaultMaxBodyBytes, wrapping
	// one route like this is exactly how an app is meant to need more.
	//
	// Wrapping the handler alone is not enough to actually raise the cap:
	// Stack's own LimitBody(DefaultMaxBodyBytes) runs ahead of the mux, so
	// it has already wrapped r.Body in a 1MB http.MaxBytesReader before
	// this route (or CSRF's own body parsing, upstream of every handler)
	// ever runs — and nesting a bigger MaxBytesReader inside a smaller one
	// cannot loosen it; the first, smaller one still errors once its own
	// count is exceeded. web.RegisterBodyLimit records the same exception
	// against the exact pattern this route registers below, so Stack's mux-
	// aware LimitBody can apply it before CSRF or this handler ever see the
	// body. See app.Router.RegisterBodyLimit's doc comment for the full
	// mechanism.
	r.RegisterBodyLimit("POST /{deckID}/cards/{cardID}/media", MaxImageFetchBytes+MaxAudioFetchBytes)

	r.HandleFunc("GET /media/{hash}", a.media)
	r.Handle("POST /{deckID}/cards/{cardID}/media",
		web.LimitBody(MaxImageFetchBytes+MaxAudioFetchBytes)(http.HandlerFunc(a.uploadCardMedia)))
}
