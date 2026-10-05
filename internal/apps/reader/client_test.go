package reader_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/iliafrenkel/on-suite/internal/apps/reader"
	"github.com/iliafrenkel/on-suite/internal/platform/webfetch"
)

// Reader's client identifies itself and asks for feeds by default. These are
// the Config values reader.NewClient hands webfetch; webfetch's own tests pin
// that the Config is honoured, this pins that Reader supplies the right one.
func TestNewClientSendsReadersUserAgentAndFeedAccept(t *testing.T) {
	var gotUA, gotAccept string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUA = r.Header.Get("User-Agent")
		gotAccept = r.Header.Get("Accept")
	}))
	defer srv.Close()

	c := reader.NewClient("v9.9.9")
	c.DenyAddr = func(string) error { return nil }
	if _, err := c.Get(context.Background(), srv.URL, webfetch.GetOptions{}); err != nil {
		t.Fatalf("Get: %v", err)
	}

	const wantUA = "onsuite/v9.9.9 (ON Reader; +https://github.com/iliafrenkel/on-suite)"
	if gotUA != wantUA {
		t.Errorf("User-Agent = %q, want %q", gotUA, wantUA)
	}
	const wantAccept = "application/atom+xml, application/rss+xml, application/xml;q=0.9, text/xml;q=0.8, */*;q=0.5"
	if gotAccept != wantAccept {
		t.Errorf("Accept = %q, want %q", gotAccept, wantAccept)
	}
}

// The unmodified client — no injected DenyAddr — must refuse private
// addresses: this is the guard Reader ships with.
func TestDefaultClientRefusesPrivateAddresses(t *testing.T) {
	_, err := reader.NewClient("v9.9.9").Get(context.Background(), "http://127.0.0.1:1/", webfetch.GetOptions{})
	if !errors.Is(err, webfetch.ErrBlockedAddress) {
		t.Errorf("error = %v, want ErrBlockedAddress", err)
	}
}
