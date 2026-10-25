package cru

import "github.com/cruworldwine/cru-api-go/cru/core"

// APIError is an error the API answered with: its HTTP status, the first error's code, message,
// field and details, every entry the API sent, and the request id Cru support needs. Branch on
// StatusCode and Code, or match a kind with errors.Is (ErrNotFound, ErrPriceChanged and the other
// Err values). It is core.APIError.
type APIError = core.APIError

// ConnectionError is a request that got no answer: the connection failed, or the attempt timed
// out (Timeout). It wraps the cause. A context the caller cancelled is returned as the context's
// error instead. It is core.ConnectionError.
type ConnectionError = core.ConnectionError

// Kinds of *APIError, for errors.Is. They are only ever matched, never returned themselves, and
// are core's values: errors.Is(err, cru.ErrNotFound) and errors.Is(err, core.ErrNotFound) agree.
var (
	// ErrBadRequest matches a 400: VALIDATION_UNKNOWN_PARAMETER or VALIDATION_MALFORMED_BODY.
	ErrBadRequest = core.ErrBadRequest
	// ErrAuthentication matches a 401: the credentials are missing or wrong, the access token
	// has expired, or it could not be refreshed.
	ErrAuthentication = core.ErrAuthentication
	// ErrPermissionDenied matches a 403: a scope, store, address or account check refused the call.
	ErrPermissionDenied = core.ErrPermissionDenied
	// ErrNotFound matches a 404.
	ErrNotFound = core.ErrNotFound
	// ErrMethodNotAllowed matches a 405.
	ErrMethodNotAllowed = core.ErrMethodNotAllowed
	// ErrConflict matches a 409, PRICE_CHANGED and REQUEST_IN_PROGRESS among them.
	ErrConflict = core.ErrConflict
	// ErrGone matches a 410: the confirmation code has expired or was cancelled.
	ErrGone = core.ErrGone
	// ErrUnprocessable matches a 422: a value was refused (VALIDATION_*), or Cru could not
	// carry the instruction out (TRADE_REFUSED).
	ErrUnprocessable = core.ErrUnprocessable
	// ErrRateLimited matches a 429.
	ErrRateLimited = core.ErrRateLimited
	// ErrServer matches any 5xx.
	ErrServer = core.ErrServer

	// ErrValidation matches any VALIDATION_* code, whatever the status.
	ErrValidation = core.ErrValidation
	// ErrPriceChanged matches PRICE_CHANGED: the price moved since the preview. Preview it again.
	ErrPriceChanged = core.ErrPriceChanged
	// ErrRequestInProgress matches REQUEST_IN_PROGRESS: an earlier request with this reference
	// is still running.
	ErrRequestInProgress = core.ErrRequestInProgress
	// ErrTradeRefused matches TRADE_REFUSED: Cru could not carry the instruction out. The
	// message gives the reason, worded so it can be shown to a customer.
	ErrTradeRefused = core.ErrTradeRefused
)

// ErrMissingParameter is returned, wrapped, when a required path parameter is empty. No request
// is sent. It is core.ErrMissingParameter.
var ErrMissingParameter = core.ErrMissingParameter

// ErrUnexpectedResponse is returned, wrapped, when a successful answer cannot be read: it is not
// the JSON envelope, or lacks the key the data is under. It is core.ErrUnexpectedResponse.
var ErrUnexpectedResponse = core.ErrUnexpectedResponse
