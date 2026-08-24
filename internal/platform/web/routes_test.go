package web_test

import (
	"net/http"
	"testing"

	"github.com/iliafrenkel/on-suite/internal/platform/web"
)

func TestRecorderHandleRegistersAndRecordsInOneStep(t *testing.T) {
	rec := web.NewRecorder()
	mux := http.NewServeMux()
	called := false
	rec.Handle(mux, "GET /healthz", true, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		called = true
	}))

	got := rec.Routes()
	if len(got) != 1 {
		t.Fatalf("Routes() has %d entries, want 1", len(got))
	}
	if got[0].Pattern != "GET /healthz" || !got[0].Public || got[0].Owner != web.PlatformOwner {
		t.Errorf("Routes()[0] = %+v", got[0])
	}

	// The handler must actually be mounted, not merely described.
	req, _ := http.NewRequest("GET", "/healthz", nil)
	h, _ := mux.Handler(req)
	h.ServeHTTP(nil, req)
	if !called {
		t.Error("the recorded handler was not registered on the mux")
	}
}

func TestRoutesAreSortedByOwnerThenPattern(t *testing.T) {
	rec := web.NewRecorder()
	rec.Add(web.Route{Pattern: "GET /paste/new", Owner: "paste"})
	rec.Add(web.Route{Pattern: "GET /login", Owner: web.PlatformOwner, Public: true})
	rec.Add(web.Route{Pattern: "GET /paste/{$}", Owner: "paste"})

	var order []string
	for _, r := range rec.Routes() {
		order = append(order, r.Pattern)
	}
	want := []string{"GET /login", "GET /paste/new", "GET /paste/{$}"}
	if len(order) != 3 || order[0] != want[0] {
		t.Fatalf("Routes() order = %v, want platform first then apps: %v", order, want)
	}
}

func TestANilRecorderIsANoOp(t *testing.T) {
	var rec *web.Recorder
	rec.Add(web.Route{Pattern: "GET /x"}) // must not panic
	if got := rec.Routes(); got != nil {
		t.Errorf("nil recorder Routes() = %v, want nil", got)
	}
}
