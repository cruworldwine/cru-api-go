package transport

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
)

// StatusError is an answer with a status of 400 or more, once any retries are spent. Refresh
// marks the OAuth token endpoint's answer to a refresh it refused.
type StatusError struct {
	Response *Response
	Refresh  bool
}

func (e *StatusError) Error() string {
	if e.Refresh {
		return fmt.Sprintf("cru: refreshing the access token: HTTP %d", e.Response.Status)
	}
	return fmt.Sprintf("cru: HTTP %d", e.Response.Status)
}

// ConnectionError is a request that got no answer: the connection failed, or the attempt timed
// out. It wraps the cause.
type ConnectionError struct {
	Method  string
	URL     string
	Err     error
	Timeout bool
}

func (e *ConnectionError) Error() string {
	return fmt.Sprintf("cru: %s %s: %v", e.Method, e.URL, e.Err)
}

// Unwrap returns the cause.
func (e *ConnectionError) Unwrap() error { return e.Err }

func connectionError(method, u string, err error) *ConnectionError {
	var ne net.Error
	timeout := errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &ne) && ne.Timeout())
	var ue *url.Error
	if errors.As(err, &ue) {
		err = ue.Err
	}
	return &ConnectionError{Method: method, URL: u, Err: err, Timeout: timeout}
}
