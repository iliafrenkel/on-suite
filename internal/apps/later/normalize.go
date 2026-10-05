package later

import (
	"fmt"
	"net/url"
	"strings"
)

// trackingParams are dropped so the same article saved from two newsletters
// is one article. Exactly these, plus any utm_* parameter.
var trackingParams = map[string]bool{
	"fbclid": true, "gclid": true, "mc_cid": true, "mc_eid": true, "ref_src": true,
}

// NormalizeURL is the form a URL is stored and de-duplicated in: http(s)
// only, lower-case scheme and host, no fragment, no tracking parameters,
// no credentials, remaining query parameters in a stable (sorted) order.
func NormalizeURL(raw string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	u.Scheme = strings.ToLower(u.Scheme)
	if (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return "", fmt.Errorf("%w: %q is not an http(s) address", ErrInvalid, raw)
	}
	u.Host = strings.ToLower(u.Host)
	u.Fragment = ""
	u.RawFragment = ""
	u.User = nil

	q := u.Query()
	for k := range q {
		if strings.HasPrefix(k, "utm_") || trackingParams[k] {
			q.Del(k)
		}
	}
	u.RawQuery = q.Encode() // sorted by key
	return u.String(), nil
}
