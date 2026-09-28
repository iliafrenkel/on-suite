package main

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/iliafrenkel/on-suite/internal/platform/auth"
	"github.com/iliafrenkel/on-suite/internal/platform/config"
)

// The guides must be reachable signed out: someone who cannot sign in is
// exactly who needs them (#309).
func TestBuildStackServesHelpSignedOut(t *testing.T) {
	cfg := config.Config{DataDir: t.TempDir()}
	handle, registry, _, err := openDatabase(context.Background(), cfg)
	if err != nil {
		t.Fatalf("openDatabase: %v", err)
	}
	defer func() { _ = handle.Close() }()

	stack, err := buildStack(stackDeps{
		DB:       handle,
		Users:    auth.NewStore(handle),
		Registry: registry,
		Log:      slog.New(slog.DiscardHandler),
		Version:  "test",
	})
	if err != nil {
		t.Fatalf("buildStack: %v", err)
	}

	for path, want := range map[string]int{
		"/help":                      http.StatusOK,
		"/help/notes":                http.StatusOK,
		"/help/images/dashboard.png": http.StatusOK,
		"/help/no-such-page":         http.StatusNotFound,
		"/help/images/no-such-image": http.StatusNotFound,
	} {
		rec := httptest.NewRecorder()
		stack.ServeHTTP(rec, httptest.NewRequest("GET", path, nil))
		if rec.Code != want {
			t.Errorf("anonymous GET %s = %d, want %d", path, rec.Code, want)
		}
	}
}
