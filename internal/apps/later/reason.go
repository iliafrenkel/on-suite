package later

import (
	"context"
	"errors"
	"fmt"
	"mime"
	"net"
	"net/http"
	"strings"

	"github.com/iliafrenkel/on-suite/internal/platform/webfetch"
)

// The reasons a link-only item shows. They're plain English for the
// reader; the technical error goes to the log (#510).
const (
	noArticleReason = "ON Later couldn't find an article on this page. It may be behind a sign-in, or built by scripts."
	notFoundReason  = "The page wasn't found. The link may be broken or the page removed."
	refusedReason   = "The site wouldn't share this page. It may need you to sign in."
)

// fetchReason says why fetching a page failed. res is whatever the fetch
// returned alongside err; it carries the status when the site answered
// with an error.
func fetchReason(res *webfetch.Response, err error) string {
	var dnsErr *net.DNSError
	var netErr net.Error
	switch {
	case errors.Is(err, webfetch.ErrBlockedAddress):
		return "That address is on a private network, so ON Later won't fetch it."
	case errors.Is(err, context.DeadlineExceeded), errors.As(err, &netErr) && netErr.Timeout():
		return "The site took too long to answer."
	case errors.As(err, &dnsErr):
		return "Couldn't find that website. Check the address for typos."
	case res == nil || res.Status < 400:
		return "Couldn't download the page."
	case res.Status == http.StatusUnauthorized, res.Status == http.StatusForbidden:
		return refusedReason
	case res.Status == http.StatusNotFound, res.Status == http.StatusGone:
		return notFoundReason
	case res.Status == http.StatusTooManyRequests:
		return "The site is getting too many requests right now."
	case res.Status >= 500:
		return "The site had a problem sending the page."
	}
	return fmt.Sprintf("The site answered with an error (HTTP %d).", res.Status)
}

// notHTMLReason says why a page that isn't HTML can't be read, naming the
// common kinds of file.
func notHTMLReason(contentType string) string {
	mt, _, _ := mime.ParseMediaType(contentType)
	kind := ""
	switch {
	case mt == "application/pdf":
		kind = "a PDF"
	case strings.HasPrefix(mt, "image/"):
		kind = "an image"
	case strings.HasPrefix(mt, "video/"):
		kind = "a video"
	case strings.HasPrefix(mt, "audio/"):
		kind = "an audio file"
	}
	if kind == "" {
		return "This link isn't a web page, so there's no article to read."
	}
	return "This link isn't a web page (it's " + kind + "), so there's no article to read."
}
