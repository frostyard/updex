// Package httpclient builds the default *http.Client every updex-created
// client falls back to when a caller supplies none, so the HTTPS-to-HTTP
// redirect downgrade refusal is enforced everywhere without callers having
// to opt in per package.
package httpclient

import (
	"errors"
	"net/http"
	"strings"
	"time"
)

// New returns an *http.Client with the given timeout whose CheckRedirect
// policy refuses a redirect that downgrades an HTTPS request to HTTP, and
// otherwise matches net/http's default 10-redirect cap.
func New(timeout time.Duration) *http.Client {
	return &http.Client{
		Timeout:       timeout,
		CheckRedirect: checkSecureRedirect,
	}
}

func checkSecureRedirect(req *http.Request, via []*http.Request) error {
	if len(via) >= 10 {
		return errors.New("stopped after 10 redirects")
	}
	if len(via) > 0 &&
		strings.EqualFold(via[len(via)-1].URL.Scheme, "https") &&
		strings.EqualFold(req.URL.Scheme, "http") {
		return errors.New("refusing redirect downgrade from https to http")
	}
	return nil
}
