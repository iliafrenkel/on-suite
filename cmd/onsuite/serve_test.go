package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestHealthzWithoutDatabase(t *testing.T) {
	rec := httptest.NewRecorder()
	healthzHandler("v1.2.3", nil).ServeHTTP(rec, httptest.NewRequest("GET", "/healthz", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var got struct{ Status, Version string }
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("body is not JSON: %v (%q)", err, rec.Body.String())
	}
	if got.Status != "ok" || got.Version != "v1.2.3" {
		t.Errorf("got %+v, want status ok and version v1.2.3", got)
	}
}

type failingPinger struct{}

func (failingPinger) PingContext(context.Context) error { return errors.New("boom") }

func TestHealthzReportsDatabaseFailure(t *testing.T) {
	rec := httptest.NewRecorder()
	healthzHandler("dev", failingPinger{}).ServeHTTP(rec, httptest.NewRequest("GET", "/healthz", nil))

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
}

// TestListenAndServeDrainsInFlightRequests proves shutdown waits for a
// request that is already running rather than cutting it off.
func TestListenAndServeDrainsInFlightRequests(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	if err := ln.Close(); err != nil {
		t.Fatal(err)
	}

	started := make(chan struct{})
	mux := http.NewServeMux()
	mux.HandleFunc("GET /slow", func(w http.ResponseWriter, r *http.Request) {
		close(started)
		time.Sleep(300 * time.Millisecond)
		_, _ = io.WriteString(w, "finished")
	})

	srv := &http.Server{Addr: addr, Handler: mux}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- listenAndServe(ctx, srv, slog.New(slog.DiscardHandler))
	}()

	var resp *http.Response
	respCh := make(chan error, 1)
	go func() {
		var err error
		for range 50 { // wait for the listener to come up
			resp, err = http.Get("http://" + addr + "/slow")
			if err == nil {
				break
			}
			time.Sleep(20 * time.Millisecond)
		}
		respCh <- err
	}()

	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("handler never started")
	}
	cancel() // simulate SIGTERM arriving mid-request

	if err := <-respCh; err != nil {
		t.Fatalf("request failed instead of draining: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if string(body) != "finished" {
		t.Errorf("body = %q, want %q", body, "finished")
	}
	if err := <-done; err != nil {
		t.Errorf("listenAndServe returned %v, want nil", err)
	}
}
