package web

import (
	"net/http"
	"sort"
	"sync"
)

// PlatformOwner labels routes the platform registers for itself, as opposed
// to routes belonging to an app.
const PlatformOwner = "platform"

// Route is one registered HTTP route.
type Route struct {
	// Pattern is the full ServeMux pattern, e.g. "GET /paste/{id}".
	Pattern string
	// Public is true only when the route was registered without the
	// authentication guard.
	Public bool
	// Owner is an app id, or PlatformOwner.
	Owner string
}

// Recorder collects every route registered anywhere in the process.
//
// Routing in this suite is default-deny, and the admin page's route map is
// where that claim becomes checkable at runtime. A map that silently omitted
// the platform's own routes — /login, /static/, /healthz — would be worse
// than no map, because those are the first routes an auditor looks for. So
// both the app router and the platform's own registrations record here.
//
// A nil *Recorder is a usable no-op, so tests that do not care can pass one.
type Recorder struct {
	mu     sync.Mutex
	routes []Route
}

func NewRecorder() *Recorder { return &Recorder{} }

// Add records a route.
func (rec *Recorder) Add(rt Route) {
	if rec == nil {
		return
	}
	rec.mu.Lock()
	defer rec.mu.Unlock()
	rec.routes = append(rec.routes, rt)
}

// Handle registers a platform route on mux and records it in one step, so a
// route cannot be served without appearing on the map.
func (rec *Recorder) Handle(mux *http.ServeMux, pattern string, public bool, h http.Handler) {
	mux.Handle(pattern, h)
	rec.Add(Route{Pattern: pattern, Public: public, Owner: PlatformOwner})
}

// Routes returns everything recorded, platform routes first and each group
// sorted by pattern.
func (rec *Recorder) Routes() []Route {
	if rec == nil {
		return nil
	}
	rec.mu.Lock()
	defer rec.mu.Unlock()

	out := make([]Route, len(rec.routes))
	copy(out, rec.routes)
	sort.SliceStable(out, func(i, j int) bool {
		li, lj := ownerRank(out[i].Owner), ownerRank(out[j].Owner)
		if li != lj {
			return li < lj
		}
		if out[i].Owner != out[j].Owner {
			return out[i].Owner < out[j].Owner
		}
		return out[i].Pattern < out[j].Pattern
	})
	return out
}

// ownerRank puts the platform above the apps. Sorting owners alphabetically
// would bury it in the middle of the app list.
func ownerRank(owner string) int {
	if owner == PlatformOwner {
		return 0
	}
	return 1
}
