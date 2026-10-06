package later

import (
	"context"
	"errors"
	"fmt"
	"net"
	"testing"

	"github.com/iliafrenkel/on-suite/internal/platform/webfetch"
)

func TestFetchReason(t *testing.T) {
	status := func(code int) *webfetch.Response { return &webfetch.Response{Status: code} }
	httpErr := errors.New("webfetch: https://x.example/ returned an error")
	tests := []struct {
		name string
		res  *webfetch.Response
		err  error
		want string
	}{
		{"blocked", nil, fmt.Errorf("webfetch: fetch x: %w", fmt.Errorf("%w: 10.0.0.1", webfetch.ErrBlockedAddress)),
			"That address is on a private network, so ON Later won't fetch it."},
		{"deadline", nil, fmt.Errorf("webfetch: fetch x: %w", context.DeadlineExceeded), "The site took too long to answer."},
		{"dns", nil, fmt.Errorf("webfetch: fetch x: %w", &net.DNSError{Err: "no such host", Name: "x.example", IsNotFound: true}),
			"Couldn't find that website. Check the address for typos."},
		{"401", status(401), httpErr, "The site wouldn't share this page. It may need you to sign in."},
		{"403", status(403), httpErr, "The site wouldn't share this page. It may need you to sign in."},
		{"404", status(404), httpErr, "The page wasn't found. The link may be broken or the page removed."},
		{"410", status(410), httpErr, "The page wasn't found. The link may be broken or the page removed."},
		{"429", status(429), httpErr, "The site is getting too many requests right now."},
		{"503", status(503), httpErr, "The site had a problem sending the page."},
		{"418", status(418), httpErr, "The site answered with an error (HTTP 418)."},
		{"other", nil, errors.New("webfetch: response exceeds 1 byte limit"), "Couldn't download the page."},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := fetchReason(tt.res, tt.err); got != tt.want {
				t.Errorf("fetchReason = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestNotHTMLReason(t *testing.T) {
	tests := map[string]string{
		"application/pdf":          "This link isn't a web page (it's a PDF), so there's no article to read.",
		"image/png":                "This link isn't a web page (it's an image), so there's no article to read.",
		"video/mp4; codecs=avc1":   "This link isn't a web page (it's a video), so there's no article to read.",
		"application/octet-stream": "This link isn't a web page, so there's no article to read.",
		"":                         "This link isn't a web page, so there's no article to read.",
	}
	for ct, want := range tests {
		if got := notHTMLReason(ct); got != want {
			t.Errorf("notHTMLReason(%q) = %q, want %q", ct, got, want)
		}
	}
}
