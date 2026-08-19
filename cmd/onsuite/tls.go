package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"golang.org/x/crypto/acme/autocert"

	"github.com/iliafrenkel/on-suite/internal/platform/config"
)

// serveAutocert runs srv over HTTPS with certificates obtained automatically
// from Let's Encrypt, plus a plain-HTTP listener for challenges and redirects.
//
// This exists so the promise the project was built on — one binary, no other
// software — is literally true for anyone who wants it that way. Running behind
// Caddy or nginx is still the recommended setup, and is what serveHTTP does.
func serveAutocert(parent context.Context, cfg config.Config, srv *http.Server, log *slog.Logger) error {
	if err := os.MkdirAll(cfg.TLSCacheDir(), 0o700); err != nil {
		return fmt.Errorf("create certificate cache %s: %w", cfg.TLSCacheDir(), err)
	}

	manager := &autocert.Manager{
		Prompt: autocert.AcceptTOS,
		// Only this host. Without a policy, autocert would attempt a
		// certificate for any name presented to it, which is an easy way to be
		// rate-limited by Let's Encrypt.
		HostPolicy: autocert.HostWhitelist(cfg.TLSDomain),
		Cache:      autocert.DirCache(cfg.TLSCacheDir()),
	}
	srv.TLSConfig = manager.TLSConfig()

	ctx, stop := signal.NotifyContext(parent, os.Interrupt, syscall.SIGTERM)
	defer stop()

	// The HTTP listener answers ACME HTTP-01 and redirects everything else.
	// It is optional: TLS-ALPN-01 works on the HTTPS port alone, which matters
	// on a host where port 80 is unavailable.
	var redirect *http.Server
	if cfg.TLSHTTPAddr != "" {
		redirect = &http.Server{
			Addr:              cfg.TLSHTTPAddr,
			Handler:           manager.HTTPHandler(nil),
			ReadHeaderTimeout: 10 * time.Second,
		}
		go func() {
			log.Info("http listener started", "addr", redirect.Addr, "purpose", "acme challenge and https redirect")
			if err := redirect.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
				// Not fatal: TLS-ALPN-01 can still get a certificate, and the
				// HTTPS listener is the one that serves the suite.
				log.Error("http listener failed", "error", err, "addr", redirect.Addr)
			}
		}()
	}

	errCh := make(chan error, 1)
	go func() {
		log.Info("listening", "addr", srv.Addr, "tls_domain", cfg.TLSDomain, "version", version)
		if err := srv.ListenAndServeTLS("", ""); err != nil && !errors.Is(err, http.ErrServerClosed) {
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

	if redirect != nil {
		if err := redirect.Shutdown(shutdownCtx); err != nil {
			log.Warn("shutting down the http listener failed", "error", err)
		}
	}
	if err := srv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("shutdown: %w", err)
	}
	log.Info("stopped cleanly")
	return nil
}
