package transport

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
)

const maxRedirects = 10

// ErrRedirect is returned, wrapped, for a redirect the client refused to follow.
var ErrRedirect = errors.New("cru: refusing a redirect to another host, or from https to http, which would send the credentials there")

// guardRedirects returns a copy of hc, or of a default client, that never follows a redirect to
// another host or from https to http. net/http drops Authorization and cookies on such a
// redirect, but would forward CLIENT-KEY and CLIENT-SECRET. On the same host it defers to hc's
// own policy.
func guardRedirects(hc *http.Client) *http.Client {
	guarded := &http.Client{}
	if hc != nil {
		*guarded = *hc
	}
	next := guarded.CheckRedirect
	guarded.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		first := via[0].URL
		if !strings.EqualFold(req.URL.Host, first.Host) || (first.Scheme == "https" && req.URL.Scheme != "https") {
			return ErrRedirect
		}
		if next != nil {
			return next(req, via)
		}
		if len(via) >= maxRedirects {
			return fmt.Errorf("cru: stopped after %d redirects", maxRedirects)
		}
		return nil
	}
	return guarded
}
