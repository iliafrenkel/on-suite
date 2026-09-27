package main

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/iliafrenkel/on-suite/internal/platform/auth"
	"github.com/iliafrenkel/on-suite/internal/platform/config"
	"github.com/iliafrenkel/on-suite/internal/platform/jobs"
)

func TestBuildStackMountsTheJobsPage(t *testing.T) {
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
		Jobs:     jobs.NewRegistry(),
		Log:      slog.New(slog.DiscardHandler),
		Version:  "test",
	})
	if err != nil {
		t.Fatalf("buildStack: %v", err)
	}

	for _, path := range []string{"/admin/jobs", "/admin/jobs/table"} {
		rec := httptest.NewRecorder()
		stack.ServeHTTP(rec, httptest.NewRequest("GET", path, nil))
		if rec.Code != http.StatusSeeOther || !strings.HasPrefix(rec.Header().Get("Location"), "/login") {
			t.Errorf("anonymous GET %s = %d, Location %q; want a 303 to /login from a mounted, guarded route", path, rec.Code, rec.Header().Get("Location"))
		}
	}
}
