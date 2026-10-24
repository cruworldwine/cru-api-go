// Package cru is a client for the Cru Trading API, Cru World Wine's partner REST API (v1).
//
// # Credentials
//
// A partner credential (WithClientKey) sends the CLIENT-KEY and CLIENT-SECRET headers and its
// writes run at once. A connected app (WithAccessToken) sends an OAuth bearer token and its
// writes are staged: they answer with a confirmation code, and nothing happens until
// Confirmations.Confirm is called with it once the customer agrees. WithOAuthRefresh lets the
// client refresh an expired access token by itself.
//
// Unset options fall back to the environment: CRU_CLIENT_KEY, CRU_CLIENT_SECRET,
// CRU_ACCESS_TOKEN, CRU_MARKET and CRU_BASE_URL.
//
// # Calls
//
// Every method takes a context.Context first. Methods are grouped by resource, as in
// client.Bids.Place or client.Offers.List. Each call that takes parameters has a params struct
// (BidsPlaceParams, OffersListParams, ...); required parameters are values and optional ones
// are pointers, set with String, Int, Float and Bool. A nil params pointer sends none.
//
// Lists that page return a *Page; Page.All walks every page. Writes return a *Write, whose
// Result holds what a partner credential's write did and whose Confirmation holds a connected
// app's staged instruction. Client.Do calls any endpoint, including ones this SDK does not
// know yet, and returns the envelope.
//
// # Retries and idempotency
//
// Reads, and a partner credential's writes, are retried on network errors, timeouts, 429, 500,
// 502, 503, 504 and 409 REQUEST_IN_PROGRESS, up to WithMaxRetries times (2 by default), with
// exponential backoff that honours Retry-After. A partner write is sent with a reference, its
// idempotency key; the SDK generates one ("sdk_" and 32 hex characters) when the params leave it
// unset and sends the same one on every retry, so a retry can never act twice. A connected
// app's writes are never retried.
//
// # Models and change
//
// The API adds fields and enum values without notice, and the models are built to survive it:
//
//   - Unknown fields are ignored, and every model keeps the JSON it was decoded from, reachable
//     with Raw (or Field, for one field).
//   - Fields documented with known values (x-extensible-enum) are plain strings. The known
//     values are constants, such as CurrencyGBP, but any other value decodes as it is.
//   - A field that is required and not nullable has a plain Go type. A field that is nullable
//     or optional is a pointer, nil when the API sent null or left it out. Arrays are slices
//     and objects without a fixed shape are maps, both nil when null or absent.
//   - integer is int64 and number is float64. An integer that arrives as 1000.0 or "1000", or a
//     number that arrives as "12.5", is converted. A value that does not fit its field is left
//     at the zero value (nil for a pointer) instead of failing the response, and is still in
//     Raw. Dates and times are strings, as the API sends them (RFC 3339).
//
// # Errors
//
// An error answer is an *APIError with the HTTP status, the first error's code, message, field
// and details, and every entry the API sent. Match a kind with errors.Is, as in
// errors.Is(err, cru.ErrNotFound) or errors.Is(err, cru.ErrPriceChanged), and read the details
// with errors.As. A request that never got an answer is a *ConnectionError.
package cru

// Version is this SDK's version, sent in the User-Agent header.
const Version = "0.1.1"

const userAgent = "cruworldwine-go/" + Version

// String returns a pointer to v, for an optional parameter.
func String(v string) *string { return &v }

// Int returns a pointer to v, for an optional parameter.
func Int(v int64) *int64 { return &v }

// Float returns a pointer to v, for an optional parameter.
func Float(v float64) *float64 { return &v }

// Bool returns a pointer to v, for an optional parameter.
func Bool(v bool) *bool { return &v }

// Value returns what p points to, or the zero value when p is nil, so a nullable field reads
// without a nil check: cru.Value(wine.WineName).
func Value[T any](p *T) T {
	if p == nil {
		var zero T
		return zero
	}
	return *p
}
