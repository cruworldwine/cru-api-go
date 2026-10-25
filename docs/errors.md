# Errors

A call fails in one of three ways.

| Error | When |
|---|---|
| `*cru.APIError` | the API answered with an error |
| `*cru.ConnectionError` | no answer came: the connection failed, or the attempt timed out (`Timeout()`) |
| the context's error | the context was cancelled or its deadline passed |

The error types and kinds are declared in package `core`, and package `cru` names them again:
`cru.APIError` is `core.APIError` and `cru.ErrNotFound` is `core.ErrNotFound`, so `errors.As`
and `errors.Is` work with either.

Some calls fail before anything is sent: a required path parameter that is empty wraps
`cru.ErrMissingParameter`, and a request option that cannot apply (such as a credential header in
`WithRequestHeader`) says why. A successful answer that cannot be read, because it is not the
JSON envelope or lacks the key its data is under, wraps `cru.ErrUnexpectedResponse`.

## Matching a kind

```go
_, err := client.Bids.Get(ctx, code)
switch {
case errors.Is(err, cru.ErrNotFound):
case errors.Is(err, cru.ErrPriceChanged): // preview again
case errors.Is(err, cru.ErrValidation):   // any VALIDATION_* code
}
```

| Kind | Matches |
|---|---|
| `ErrBadRequest` | 400: `VALIDATION_UNKNOWN_PARAMETER`, `VALIDATION_MALFORMED_BODY` |
| `ErrAuthentication` | 401: credentials missing or wrong, the token expired, or could not be refreshed |
| `ErrPermissionDenied` | 403: a scope, store, address or account check |
| `ErrNotFound` | 404 |
| `ErrMethodNotAllowed` | 405 |
| `ErrConflict` | 409, `PRICE_CHANGED` and `REQUEST_IN_PROGRESS` among them |
| `ErrGone` | 410: a confirmation code that expired or was cancelled |
| `ErrUnprocessable` | 422: a value refused, or `TRADE_REFUSED` |
| `ErrRateLimited` | 429 |
| `ErrServer` | any 5xx |
| `ErrValidation` | any `VALIDATION_*` code, whatever the status |
| `ErrPriceChanged` | `PRICE_CHANGED`: the price moved since the preview |
| `ErrRequestInProgress` | `REQUEST_IN_PROGRESS`: an earlier request with this reference is running |
| `ErrTradeRefused` | `TRADE_REFUSED`: Cru could not carry the instruction out |

The kinds are only ever matched, never returned themselves. The codes are the `cru.ErrorCode*`
constants; a code added since this SDK was released is still in `Code`.

## Reading the details

```go
var apiErr *cru.APIError
if errors.As(err, &apiErr) {
	log.Println(apiErr.StatusCode, apiErr.Code, apiErr.Message, apiErr.Field, apiErr.Details, apiErr.RequestID)
}
```

`Message` is for people and may change: branch on `StatusCode` and `Code`. `Details` holds the
first error's structured context (`required_scope`, `allowed_stores`, `retry_after`), `Errors`
every entry the API sent, and `Header` and `Body` the answer as it came. Quote `RequestID` to
Cru support.

## What is retried

Errors the SDK retries by itself (network errors, timeouts, 429, 500, 502, 503, 504 and
`REQUEST_IN_PROGRESS`, for reads and partner writes) reach you only once the retries are spent.
See [configuration](configuration.md#retries).
