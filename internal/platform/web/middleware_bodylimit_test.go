package web

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

// echoBodyLen reads the whole body and reports how many bytes it read, or
// the error MaxBytesReader produced once the cap was exceeded.
func echoBodyLen(w http.ResponseWriter, r *http.Request) {
	n, err := io.Copy(io.Discard, r.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusRequestEntityTooLarge)
		return
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(strconv.FormatInt(n, 10)))
}

// bodyLimitTestMux builds a real *http.ServeMux with two routes, "POST
// /small" and "POST /big", so limitBodyForStack has real patterns to
// resolve via the mux's own Handler method — the same routing on which the
// real production mechanism depends.
func bodyLimitTestMux() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /test-bodylimit-small", echoBodyLen)
	mux.HandleFunc("POST /test-bodylimit-big", echoBodyLen)
	return mux
}

// postBody sends a request with an n-byte body through h and returns the
// recorded response.
func postBody(t *testing.T, h http.Handler, path string, n int) *httptest.ResponseRecorder {
	t.Helper()
	body := strings.NewReader(strings.Repeat("a", n))
	req := httptest.NewRequest(http.MethodPost, path, body)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// TestBodyLimitForStackHonorsPerRouteOverride proves RegisterBodyLimit and
// limitBodyForStack in isolation, without going through any app: an
// override registered for one route raises its cap without affecting a
// sibling route left at the platform default, and the override itself
// still rejects a body larger than the raised cap. Pattern names are
// namespaced to this test (test-bodylimit-*) because bodyLimitByPattern is
// a package-level map shared with any other test that calls
// RegisterBodyLimit in the same test binary run.
func TestBodyLimitForStackHonorsPerRouteOverride(t *testing.T) {
	const bigPattern = "POST /test-bodylimit-big"
	const overrideMax = DefaultMaxBodyBytes + 4096

	RegisterBodyLimit(bigPattern, overrideMax)

	mux := bodyLimitTestMux()
	// This is exactly what Stack installs in place of a flat
	// LimitBody(DefaultMaxBodyBytes) — see Stack's own call to
	// limitBodyForStack(h) where h is the mux passed to it.
	h := limitBodyForStack(mux)(mux)

	t.Run("small route stays at the platform default", func(t *testing.T) {
		rec := postBody(t, h, "/test-bodylimit-small", DefaultMaxBodyBytes-1)
		if rec.Code != http.StatusOK {
			t.Fatalf("body under the default = %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("small route rejects a body over the default", func(t *testing.T) {
		rec := postBody(t, h, "/test-bodylimit-small", DefaultMaxBodyBytes+4096)
		if rec.Code == http.StatusOK {
			t.Fatalf("body over the default on /small should have been rejected, got 200: %s", rec.Body.String())
		}
	})

	t.Run("big route's override raises its own cap", func(t *testing.T) {
		// Between the default and the registered override: would be
		// rejected without the override in effect.
		rec := postBody(t, h, "/test-bodylimit-big", DefaultMaxBodyBytes+2048)
		if rec.Code != http.StatusOK {
			t.Fatalf("body between default and override on /big = %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("big route's override still has a ceiling", func(t *testing.T) {
		// Over both the default and the registered override: the override
		// raises the cap, it does not remove it.
		rec := postBody(t, h, "/test-bodylimit-big", overrideMax+4096)
		if rec.Code == http.StatusOK {
			t.Fatalf("body over the override on /big should have been rejected, got 200: %s", rec.Body.String())
		}
	})
}

// TestBodyLimitFallsBackWhenNotAMux confirms limitBodyForStack's fallback
// path: when Stack is given something other than a real *http.ServeMux, it
// behaves exactly like the old flat LimitBody(DefaultMaxBodyBytes), with no
// per-route override possible.
func TestBodyLimitFallsBackWhenNotAMux(t *testing.T) {
	inner := http.HandlerFunc(echoBodyLen)
	h := limitBodyForStack(inner)(inner)

	rec := postBody(t, h, "/anything", DefaultMaxBodyBytes-1)
	if rec.Code != http.StatusOK {
		t.Fatalf("body under the default = %d: %s", rec.Code, rec.Body.String())
	}

	rec = postBody(t, h, "/anything", DefaultMaxBodyBytes+4096)
	if rec.Code == http.StatusOK {
		t.Fatalf("body over the default should have been rejected, got 200: %s", rec.Body.String())
	}
}
