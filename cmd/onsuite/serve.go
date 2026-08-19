package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/iliafrenkel/on-suite/internal/platform/auth"
	"github.com/iliafrenkel/on-suite/internal/platform/config"
	"github.com/iliafrenkel/on-suite/internal/platform/db"
)

func serve(args []string, getenv func(string) string, errOut io.Writer) error {
	cfg, err := config.Parse(args, getenv, errOut)
	if err != nil {
		return err
	}
	log := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: cfg.LogLevel}))

	if err := os.MkdirAll(cfg.DataDir, 0o750); err != nil {
		return fmt.Errorf("create data dir %s: %w", cfg.DataDir, err)
	}

	handle, registry, applied, err := openDatabase(context.Background(), cfg)
	if err != nil {
		return err
	}
	defer func() {
		if err := db.Checkpoint(context.Background(), handle); err != nil {
			log.Warn("wal checkpoint on shutdown failed", "error", err)
		}
		if err := handle.Close(); err != nil {
			log.Warn("closing database failed", "error", err)
		}
	}()
	log.Info("database ready", "path", cfg.DBPath())

	if applied > 0 {
		log.Info("migrations applied", "count", applied)
	}

	users := auth.NewStore(handle)
	switch n, err := users.CountUsers(context.Background()); {
	case err != nil:
		return err
	case n == 0:
		// The single most likely first-run confusion, so say it plainly
		// rather than leaving an empty login page as the only clue.
		log.Warn("no accounts exist yet; create one with: onsuite user add <name> --admin",
			"data_dir", cfg.DataDir)
	}

	handler, err := buildStack(stackDeps{
		DB:       handle,
		Users:    users,
		Registry: registry,
		Log:      log,
		Version:  version,
		Secure:   cfg.SecureCookies,
	})
	if err != nil {
		return err
	}

	srv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	maintenanceCtx, stopMaintenance := context.WithCancel(context.Background())
	defer stopMaintenance()
	go runMaintenance(maintenanceCtx, handle, users, cfg, log)

	if cfg.TLSEnabled() {
		return serveAutocert(context.Background(), cfg, srv, log)
	}
	return serveHTTP(context.Background(), srv, log)
}

// serveHTTP runs srv until SIGINT or SIGTERM, then drains in-flight
// requests. It is separated from serve so tests can drive it directly.
func serveHTTP(parent context.Context, srv *http.Server, log *slog.Logger) error {
	ctx, stop := signal.NotifyContext(parent, os.Interrupt, syscall.SIGTERM)
	defer stop()

	errCh := make(chan error, 1)
	go func() {
		log.Info("listening", "addr", srv.Addr, "version", version)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		log.Info("shutdown signal received, draining")
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("shutdown: %w", err)
	}
	log.Info("stopped cleanly")
	return nil
}

// pinger is satisfied by *sql.DB. It is an interface rather than a concrete
// type so healthz can be tested without a database.
type pinger interface {
	PingContext(context.Context) error
}

func healthzHandler(version string, db pinger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		status, code := "ok", http.StatusOK
		if db != nil {
			ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
			defer cancel()
			if err := db.PingContext(ctx); err != nil {
				status, code = "database unavailable", http.StatusServiceUnavailable
			}
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(code)
		fmt.Fprintf(w, "{\"status\":%q,\"version\":%q}\n", status, version)
	})
}
