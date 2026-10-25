package core_test

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/cruworldwine/cru-api-go/cru/bids"
	"github.com/cruworldwine/cru-api-go/cru/core"
	"github.com/cruworldwine/cru-api-go/internal/caller"
	"github.com/cruworldwine/cru-api-go/internal/testkit"
	"github.com/cruworldwine/cru-api-go/internal/transport"
)

// These tests reach core as a call does: through a Caller, with a real transport, to a local
// server. The transport's failures must reach callers as *APIError, *ConnectionError or the
// context's error.

// bidsOn returns the bids service on c.
func bidsOn(c *caller.Caller) *bids.Service { return caller.NewService[bids.Service](c) }

func noRetries(c *transport.Config) { c.MaxRetries = 0 }

func TestRefreshErrors(t *testing.T) {
	for _, tc := range []struct {
		name, body, code, message string
	}{
		{"an OAuth refusal", `{"error":"invalid_grant","error_description":"The refresh token is invalid."}`,
			"invalid_grant", "refreshing the access token: The refresh token is invalid."},
		{"a refusal without a description", `{"error":"invalid_client"}`,
			"invalid_client", "refreshing the access token: the access token could not be refreshed"},
		{"a Cru error envelope", testkit.ErrorBody("AUTH_CLIENT_SUSPENDED", "This app is suspended."),
			"AUTH_CLIENT_SUSPENDED", "refreshing the access token: This app is suspended."},
	} {
		s := testkit.NewServer(t, func(w http.ResponseWriter, r *http.Request, n int) {
			if r.URL.Path == "/api/oauth/token" {
				testkit.Reply(w, 400, tc.body)
				return
			}
			testkit.Reply(w, 401, testkit.ErrorBody("AUTH_INVALID_CREDENTIALS", "Expired."))
		})
		c := testkit.App(t, s, func(c *transport.Config) {
			c.AccessToken, c.RefreshToken, c.OAuthClientID = "at_old", "rt_0", "app_test"
		})
		_, err := bidsOn(c).Get(context.Background(), testkit.BidCode)
		var apiErr *core.APIError
		if !errors.Is(err, core.ErrAuthentication) || !errors.As(err, &apiErr) {
			t.Fatalf("%s: want an authentication error, got %v", tc.name, err)
		}
		if apiErr.StatusCode != 401 || apiErr.Code != tc.code || apiErr.Message != tc.message || apiErr.Details["token_status"] != 400 {
			t.Errorf("%s: %+v", tc.name, apiErr)
		}
	}
}

func TestConnectionErrors(t *testing.T) {
	refused := &net.OpError{Op: "dial", Net: "tcp", Err: errors.New("connection refused")}
	s := testkit.NewServer(t, testkit.Always(200, testkit.OK(`{}`)))
	c := testkit.Partner(t, s, noRetries, func(c *transport.Config) {
		c.HTTPClient = &http.Client{Transport: testkit.FailingTransport{Err: refused}}
	})
	_, err := bidsOn(c).Get(context.Background(), testkit.BidCode)
	var ce *core.ConnectionError
	if !errors.As(err, &ce) || ce.Timeout() || !errors.Is(err, refused) || ce.Method != "GET" ||
		!strings.HasSuffix(ce.URL, "/bids/"+testkit.BidCode) || !strings.Contains(ce.Error(), "connection refused") {
		t.Errorf("%#v", err)
	}

	s = testkit.NewServer(t, func(w http.ResponseWriter, r *http.Request, n int) { <-r.Context().Done() })
	c = testkit.Partner(t, s, noRetries, func(c *transport.Config) { c.Timeout = 20 * time.Millisecond })
	if _, err := bidsOn(c).Get(context.Background(), testkit.BidCode); !errors.As(err, &ce) || !ce.Timeout() {
		t.Errorf("a timed-out attempt: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := bidsOn(c).Get(ctx, testkit.BidCode); !errors.Is(err, context.Canceled) || errors.As(err, &ce) {
		t.Errorf("a cancelled call: %v", err)
	}
}

func errorFor(t *testing.T, status int, body string, header http.Header) *core.APIError {
	t.Helper()
	s := testkit.NewServer(t, func(w http.ResponseWriter, r *http.Request, n int) {
		for k, v := range header {
			w.Header()[k] = v
		}
		testkit.Reply(w, status, body)
	})
	_, err := bidsOn(testkit.Partner(t, s, noRetries)).Get(context.Background(), testkit.BidCode)
	var apiErr *core.APIError
	if !errors.As(fmt.Errorf("wrapped: %w", err), &apiErr) {
		t.Fatalf("HTTP %d: want an *APIError, got %T %v", status, err, err)
	}
	return apiErr
}

func TestErrorKinds(t *testing.T) {
	all := []error{core.ErrBadRequest, core.ErrAuthentication, core.ErrPermissionDenied, core.ErrNotFound,
		core.ErrMethodNotAllowed, core.ErrConflict, core.ErrGone, core.ErrUnprocessable, core.ErrRateLimited,
		core.ErrServer, core.ErrValidation, core.ErrPriceChanged, core.ErrRequestInProgress, core.ErrTradeRefused}
	cases := []struct {
		status int
		code   string
		want   []error
	}{
		{400, "VALIDATION_UNKNOWN_PARAMETER", []error{core.ErrBadRequest, core.ErrValidation}},
		{400, "VALIDATION_MALFORMED_BODY", []error{core.ErrBadRequest, core.ErrValidation}},
		{401, "AUTH_INVALID_CREDENTIALS", []error{core.ErrAuthentication}},
		{403, "AUTH_INSUFFICIENT_SCOPE", []error{core.ErrPermissionDenied}},
		{403, "AUTH_STORE_NOT_ALLOWED", []error{core.ErrPermissionDenied}},
		{404, "BID_NOT_FOUND", []error{core.ErrNotFound}},
		{405, "METHOD_NOT_ALLOWED", []error{core.ErrMethodNotAllowed}},
		{409, "PRICE_CHANGED", []error{core.ErrConflict, core.ErrPriceChanged}},
		{409, "REQUEST_IN_PROGRESS", []error{core.ErrConflict, core.ErrRequestInProgress}},
		{409, "CONFIRMATION_LIMIT_REACHED", []error{core.ErrConflict}},
		{410, "CONFIRMATION_EXPIRED", []error{core.ErrGone}},
		{422, "VALIDATION_REQUIRED_FIELD", []error{core.ErrUnprocessable, core.ErrValidation}},
		{422, "VALIDATION_INVALID_VALUE", []error{core.ErrUnprocessable, core.ErrValidation}},
		{422, "TRADE_REFUSED", []error{core.ErrUnprocessable, core.ErrTradeRefused}},
		{429, "RATE_LIMIT_EXCEEDED", []error{core.ErrRateLimited}},
		{500, "INTERNAL_ERROR", []error{core.ErrServer}},
		{503, "SERVICE_UNAVAILABLE", []error{core.ErrServer}},
		{418, "SOMETHING_NEW", nil},
	}
	for _, tc := range cases {
		e := errorFor(t, tc.status, testkit.ErrorBody(tc.code, "Refused."), nil)
		if e.StatusCode != tc.status || e.Code != tc.code || e.Message != "Refused." {
			t.Errorf("HTTP %d: %+v", tc.status, e)
		}
		for _, kind := range all {
			want := false
			for _, w := range tc.want {
				want = want || errors.Is(w, kind)
			}
			if got := errors.Is(e, kind); got != want {
				t.Errorf("HTTP %d %s: errors.Is(%v) = %v", tc.status, tc.code, kind, got)
			}
		}
	}
}

func TestErrorFields(t *testing.T) {
	body := `{"status":"ERROR","message":"The request was refused.","meta":` + testkit.Meta("") + `,"data":null,"errors":[
		{"code":"AUTH_INSUFFICIENT_SCOPE","message":"This needs bids:write.","field":"scope","details":{"required_scope":"bids:write","allowed_stores":[1,2]},"extra":true},
		{"code":"SOMETHING_ELSE","message":"And this."}]}`
	e := errorFor(t, 403, body, nil)
	if e.Code != core.ErrorCodeAuthInsufficientScope || e.Message != "This needs bids:write." || e.Field != "scope" {
		t.Errorf("first error %+v", e)
	}
	if e.Details["required_scope"] != "bids:write" || len(e.Errors) != 2 || e.Errors[1].Code != "SOMETHING_ELSE" {
		t.Errorf("details %v, errors %+v", e.Details, e.Errors)
	}
	if e.RequestID != "req_test" || string(e.Body) != body {
		t.Errorf("request id %q", e.RequestID)
	}
	if got := e.Error(); got != "cru: 403 AUTH_INSUFFICIENT_SCOPE: This needs bids:write. (request req_test)" {
		t.Errorf("Error() = %s", got)
	}
	var extra bool
	if found, err := core.Field(&e.Errors[0], "extra", &extra); !found || err != nil || !extra {
		t.Error("an error entry's unknown field is not in Raw")
	}
}

func TestErrorFallbacks(t *testing.T) {
	e := errorFor(t, 502, `<html>Bad gateway</html>`, http.Header{"X-Request-Id": {"req_from_header"}})
	if e.StatusCode != 502 || e.Code != "" || e.Message != "HTTP 502" || e.RequestID != "req_from_header" {
		t.Errorf("a non-JSON error: %+v", e)
	}
	if !errors.Is(e, core.ErrServer) || string(e.Body) != `<html>Bad gateway</html>` {
		t.Error("a non-JSON error lost its kind or body")
	}
	e = errorFor(t, 404, `{"status":"ERROR","message":"Nothing here.","meta":{},"data":null,"errors":[]}`, nil)
	if e.Message != "Nothing here." || e.Code != "" || !errors.Is(e, core.ErrNotFound) {
		t.Errorf("envelope message fallback: %+v", e)
	}
	if e.Error() != "cru: 404: Nothing here." {
		t.Errorf("Error() = %s", e.Error())
	}
}

func TestErrorKindsAreNotReturned(t *testing.T) {
	if errors.Is(core.ErrNotFound, core.ErrServer) || errors.Is(&core.APIError{StatusCode: 404}, core.ErrMissingParameter) {
		t.Error("kinds matched each other")
	}
	if core.ErrNotFound.Error() != "cru: not found" {
		t.Error(core.ErrNotFound.Error())
	}
}

// TestOtherErrorsPassThrough checks an error that is not the transport's, such as a request
// option's, reaches the caller as it is.
func TestOtherErrorsPassThrough(t *testing.T) {
	s := testkit.NewServer(t, testkit.Always(200, testkit.OK(`{}`)))
	_, err := bidsOn(testkit.Partner(t, s)).Get(context.Background(), testkit.BidCode, core.WithRequestHeader("Authorization", "x"))
	var apiErr *core.APIError
	var ce *core.ConnectionError
	if err == nil || errors.As(err, &apiErr) || errors.As(err, &ce) || !strings.Contains(err.Error(), "credentials") {
		t.Errorf("a refused option: %T %v", err, err)
	}
}
