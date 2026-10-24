package cru

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"testing"
)

func errorFor(t *testing.T, status int, body string, header http.Header) *APIError {
	t.Helper()
	s := newServer(t, func(w http.ResponseWriter, r *http.Request, n int) {
		for k, v := range header {
			w.Header()[k] = v
		}
		reply(w, status, body)
	})
	c, _ := partner(t, s, WithMaxRetries(0))
	_, err := c.Bids.Get(context.Background(), testBidCode)
	var apiErr *APIError
	if !errors.As(fmt.Errorf("wrapped: %w", err), &apiErr) {
		t.Fatalf("HTTP %d: want an *APIError, got %T %v", status, err, err)
	}
	return apiErr
}

func TestErrorKinds(t *testing.T) {
	all := []error{ErrBadRequest, ErrAuthentication, ErrPermissionDenied, ErrNotFound, ErrMethodNotAllowed,
		ErrConflict, ErrGone, ErrUnprocessable, ErrRateLimited, ErrServer, ErrValidation, ErrPriceChanged,
		ErrRequestInProgress, ErrTradeRefused}
	cases := []struct {
		status int
		code   string
		want   []error
	}{
		{400, "VALIDATION_UNKNOWN_PARAMETER", []error{ErrBadRequest, ErrValidation}},
		{400, "VALIDATION_MALFORMED_BODY", []error{ErrBadRequest, ErrValidation}},
		{401, "AUTH_INVALID_CREDENTIALS", []error{ErrAuthentication}},
		{403, "AUTH_INSUFFICIENT_SCOPE", []error{ErrPermissionDenied}},
		{403, "AUTH_STORE_NOT_ALLOWED", []error{ErrPermissionDenied}},
		{404, "BID_NOT_FOUND", []error{ErrNotFound}},
		{405, "METHOD_NOT_ALLOWED", []error{ErrMethodNotAllowed}},
		{409, "PRICE_CHANGED", []error{ErrConflict, ErrPriceChanged}},
		{409, "REQUEST_IN_PROGRESS", []error{ErrConflict, ErrRequestInProgress}},
		{409, "CONFIRMATION_LIMIT_REACHED", []error{ErrConflict}},
		{410, "CONFIRMATION_EXPIRED", []error{ErrGone}},
		{422, "VALIDATION_REQUIRED_FIELD", []error{ErrUnprocessable, ErrValidation}},
		{422, "VALIDATION_INVALID_VALUE", []error{ErrUnprocessable, ErrValidation}},
		{422, "TRADE_REFUSED", []error{ErrUnprocessable, ErrTradeRefused}},
		{429, "RATE_LIMIT_EXCEEDED", []error{ErrRateLimited}},
		{500, "INTERNAL_ERROR", []error{ErrServer}},
		{503, "SERVICE_UNAVAILABLE", []error{ErrServer}},
		{418, "SOMETHING_NEW", nil},
	}
	for _, tc := range cases {
		e := errorFor(t, tc.status, apiError(tc.code, "Refused."), nil)
		if e.StatusCode != tc.status || e.Code != tc.code || e.Message != "Refused." {
			t.Errorf("HTTP %d: %+v", tc.status, e)
		}
		for _, kind := range all {
			want := false
			for _, w := range tc.want {
				want = want || w == kind
			}
			if got := errors.Is(e, kind); got != want {
				t.Errorf("HTTP %d %s: errors.Is(%v) = %v", tc.status, tc.code, kind, got)
			}
		}
	}
}

func TestErrorFields(t *testing.T) {
	body := `{"status":"ERROR","message":"The request was refused.","meta":` + meta("") + `,"data":null,"errors":[
		{"code":"AUTH_INSUFFICIENT_SCOPE","message":"This needs bids:write.","field":"scope","details":{"required_scope":"bids:write","allowed_stores":[1,2]},"extra":true},
		{"code":"SOMETHING_ELSE","message":"And this."}]}`
	e := errorFor(t, 403, body, nil)
	if e.Code != ErrorCodeAuthInsufficientScope || e.Message != "This needs bids:write." || e.Field != "scope" {
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
	if found, err := Field(&e.Errors[0], "extra", &extra); !found || err != nil || !extra {
		t.Error("an error entry's unknown field is not in Raw")
	}
}

func TestErrorFallbacks(t *testing.T) {
	e := errorFor(t, 502, `<html>Bad gateway</html>`, http.Header{"X-Request-Id": {"req_from_header"}})
	if e.StatusCode != 502 || e.Code != "" || e.Message != "HTTP 502" || e.RequestID != "req_from_header" {
		t.Errorf("a non-JSON error: %+v", e)
	}
	if !errors.Is(e, ErrServer) || string(e.Body) != `<html>Bad gateway</html>` {
		t.Error("a non-JSON error lost its kind or body")
	}
	e = errorFor(t, 404, `{"status":"ERROR","message":"Nothing here.","meta":{},"data":null,"errors":[]}`, nil)
	if e.Message != "Nothing here." || e.Code != "" || !errors.Is(e, ErrNotFound) {
		t.Errorf("envelope message fallback: %+v", e)
	}
	if e.Error() != "cru: 404: Nothing here." {
		t.Errorf("Error() = %s", e.Error())
	}
}

func TestErrorKindsAreNotReturned(t *testing.T) {
	if errors.Is(ErrNotFound, ErrServer) || errors.Is(&APIError{StatusCode: 404}, ErrMissingParameter) {
		t.Error("kinds matched each other")
	}
	if ErrNotFound.Error() != "cru: not found" {
		t.Error(ErrNotFound.Error())
	}
}
