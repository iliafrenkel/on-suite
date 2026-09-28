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

// TestBuildStackServesHelpForEveryRegisteredApp guards the user menu's Help
// link (#309): it opens /help/<active-app>, so every app ID this binary
// registers — plus "admin", which jobsadmin/usermgmt/admin all set as
// ActiveApp — needs its own guide page. The IDs come from registeredApps()
// itself, not a hard-coded list, so a new app with no guide fails this test
// rather than 404ing for real users.
func TestBuildStackServesHelpForEveryRegisteredApp(t *testing.T) {
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

	ids := []string{"admin"}
	for _, a := range registeredApps() {
		ids = append(ids, a.Meta().ID)
	}

	for _, id := range ids {
		rec := httptest.NewRecorder()
		stack.ServeHTTP(rec, httptest.NewRequest("GET", "/help/"+id, nil))
		if rec.Code != http.StatusOK {
			t.Errorf("GET /help/%s = %d, want 200 (the Help menu item links here)", id, rec.Code)
		}
	}
}
