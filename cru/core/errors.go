package core

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/cruworldwine/cru-api-go/internal/apijson"
	"github.com/cruworldwine/cru-api-go/internal/transport"
)

// APIError is an error the API answered with.
//
// Branch on StatusCode and Code, or match a kind with errors.Is (ErrNotFound, ErrPriceChanged
// and the other Err values). Message is for people and may change. Errors holds every entry the
// API sent, and RequestID is what Cru support needs to find the request.
type APIError struct {
	// StatusCode is the HTTP status.
	StatusCode int
	// Code is the first error's code, such as PRICE_CHANGED: one of the ErrorCode constants, or
	// a code added since this SDK was released.
	Code string
	// Message is the first error's message, else the envelope's.
	Message string
	// Field is the query parameter or body field the first error is about, if any.
	Field string
	// Details is the first error's structured context, such as required_scope or retry_after.
	Details map[string]any
	// Errors is every entry the API sent.
	Errors []ErrorEntry
	// RequestID is meta.request_id, else the X-Request-Id header.
	RequestID string
	// Header is the response's headers.
	Header http.Header
	// Body is the response body as it was received.
	Body []byte
}

func (e *APIError) Error() string {
	var b strings.Builder
	b.WriteString("cru: ")
	b.WriteString(strconv.Itoa(e.StatusCode))
	if e.Code != "" {
		b.WriteString(" " + e.Code)
	}
	b.WriteString(": " + e.Message)
	if e.RequestID != "" {
		b.WriteString(" (request " + e.RequestID + ")")
	}
	return b.String()
}

// Is reports whether the error is of the kind target names: ErrNotFound, ErrValidation and so
// on. It lets errors.Is match an *APIError against those values.
func (e *APIError) Is(target error) bool {
	k, ok := target.(*kindError)
	return ok && k.match(e)
}

type kindError struct {
	msg   string
	match func(*APIError) bool
}

func (k *kindError) Error() string { return k.msg }

func byStatus(msg string, status int) error {
	return &kindError{msg, func(e *APIError) bool { return e.StatusCode == status }}
}

func byCode(msg, code string) error {
	return &kindError{msg, func(e *APIError) bool { return e.Code == code }}
}

// Kinds of *APIError, for errors.Is. They are only ever matched, never returned themselves.
var (
	// ErrBadRequest matches a 400: VALIDATION_UNKNOWN_PARAMETER or VALIDATION_MALFORMED_BODY.
	ErrBadRequest = byStatus("cru: bad request", http.StatusBadRequest)
	// ErrAuthentication matches a 401: the credentials are missing or wrong, the access token
	// has expired, or it could not be refreshed.
	ErrAuthentication = byStatus("cru: authentication failed", http.StatusUnauthorized)
	// ErrPermissionDenied matches a 403: a scope, store, address or account check refused the call.
	ErrPermissionDenied = byStatus("cru: permission denied", http.StatusForbidden)
	// ErrNotFound matches a 404.
	ErrNotFound = byStatus("cru: not found", http.StatusNotFound)
	// ErrMethodNotAllowed matches a 405.
	ErrMethodNotAllowed = byStatus("cru: method not allowed", http.StatusMethodNotAllowed)
	// ErrConflict matches a 409, PRICE_CHANGED and REQUEST_IN_PROGRESS among them.
	ErrConflict = byStatus("cru: conflict", http.StatusConflict)
	// ErrGone matches a 410: the confirmation code has expired or was cancelled.
	ErrGone = byStatus("cru: gone", http.StatusGone)
	// ErrUnprocessable matches a 422: a value was refused (VALIDATION_*), or Cru could not
	// carry the instruction out (TRADE_REFUSED).
	ErrUnprocessable = byStatus("cru: unprocessable", http.StatusUnprocessableEntity)
	// ErrRateLimited matches a 429.
	ErrRateLimited = byStatus("cru: rate limited", http.StatusTooManyRequests)
	// ErrServer matches any 5xx.
	ErrServer error = &kindError{"cru: server error", func(e *APIError) bool { return e.StatusCode >= 500 }}

	// ErrValidation matches any VALIDATION_* code, whatever the status.
	ErrValidation error = &kindError{"cru: validation failed", func(e *APIError) bool {
		return strings.HasPrefix(e.Code, "VALIDATION_")
	}}
	// ErrPriceChanged matches PRICE_CHANGED: the price moved since the preview. Preview it again.
	ErrPriceChanged = byCode("cru: price changed", ErrorCodePriceChanged)
	// ErrRequestInProgress matches REQUEST_IN_PROGRESS: an earlier request with this reference
	// is still running.
	ErrRequestInProgress = byCode("cru: request in progress", ErrorCodeRequestInProgress)
	// ErrTradeRefused matches TRADE_REFUSED: Cru could not carry the instruction out. The
	// message gives the reason, worded so it can be shown to a customer.
	ErrTradeRefused = byCode("cru: trade refused", ErrorCodeTradeRefused)
)

// ErrMissingParameter is returned, wrapped, when a required path parameter is empty. No request
// is sent.
var ErrMissingParameter = errors.New("cru: missing parameter")

// ErrUnexpectedResponse is returned, wrapped, when a successful answer cannot be read: it is not
// the JSON envelope, or lacks the key the data is under.
var ErrUnexpectedResponse = errors.New("cru: unexpected response")

// publicError turns the transport's errors into *APIError and *ConnectionError.
func publicError(err error) error {
	var se *transport.StatusError
	if errors.As(err, &se) {
		if se.Refresh {
			return refreshError(se.Response)
		}
		return newAPIError(se.Response.Status, se.Response.Header, se.Response.Body)
	}
	var ce *transport.ConnectionError
	if errors.As(err, &ce) {
		return &ConnectionError{Method: ce.Method, URL: ce.URL, Err: ce.Err, timeout: ce.Timeout}
	}
	return err
}

func newAPIError(status int, header http.Header, body []byte) *APIError {
	e := &APIError{StatusCode: status, Header: header, Body: body}
	_, f := apijson.Split(body)
	var message string
	apijson.Decode(f, "message", &message)
	apijson.Decode(f, "errors", &e.Errors)
	if len(e.Errors) > 0 {
		first := e.Errors[0]
		e.Code = first.Code
		e.Message = first.Message
		e.Details = first.Details
		if first.Field != nil {
			e.Field = *first.Field
		}
	}
	if e.Message == "" {
		e.Message = message
	}
	if e.Message == "" {
		e.Message = fmt.Sprintf("HTTP %d", status)
	}
	e.RequestID = apijson.RequestID(f, header)
	return e
}

// refreshError is an authentication error (401) whatever the token endpoint answered, with its
// status in Details["token_status"] and its OAuth error as Code when it sent no Cru code.
func refreshError(resp *transport.Response) *APIError {
	e := newAPIError(resp.Status, resp.Header, resp.Body)
	_, f := apijson.Split(resp.Body)
	if e.Code == "" {
		apijson.Decode(f, "error", &e.Code)
	}
	var description string
	apijson.Decode(f, "error_description", &description)
	switch {
	case description != "" && len(e.Errors) == 0:
		e.Message = description
	case len(e.Errors) == 0:
		e.Message = "the access token could not be refreshed"
	}
	e.Message = "refreshing the access token: " + e.Message
	if e.Details == nil {
		e.Details = map[string]any{}
	}
	e.Details["token_status"] = resp.Status
	e.StatusCode = http.StatusUnauthorized
	return e
}

// ConnectionError is a request that got no answer: the connection failed, or the attempt timed
// out. It wraps the cause. A context the caller cancelled is returned as the context's error
// instead.
type ConnectionError struct {
	// Method and URL are the request's.
	Method string
	URL    string
	// Err is the cause.
	Err     error
	timeout bool
}

func (e *ConnectionError) Error() string {
	return fmt.Sprintf("cru: %s %s: %v", e.Method, e.URL, e.Err)
}

// Unwrap returns the cause.
func (e *ConnectionError) Unwrap() error { return e.Err }

// Timeout reports whether the attempt timed out.
func (e *ConnectionError) Timeout() bool { return e.timeout }
