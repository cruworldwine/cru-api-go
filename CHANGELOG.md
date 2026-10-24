# Changelog

All notable changes to this module are recorded here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and versions follow
[Semantic Versioning](https://semver.org/spec/v2.0.0.html). While the major version is 0, a minor
release may change the API incompatibly, and says so here.

## [Unreleased]

The next tag is **v0.1.1**: `Version` in `cru.go` is already `"0.1.1"`. It ships everything in
this section, including the per-resource layout already on main since v0.1.0.

### Added

- Per-call options. Every method, and `Client.Do`, takes trailing `opts ...RequestOption`:
  - `WithRequestHeader(key, value)` sends a header on that call;
  - `WithRequestTimeout(d)` and `WithRequestMaxRetries(n)` override the client's per-attempt
    timeout and retry count;
  - `WithExtraQuery(key, value)` and `WithExtraBodyField(key, value)` send a query parameter or
    body field this version of the SDK does not know yet;
  - `WithResponseInto(*ResponseMeta)` reports the answer's status, headers, request id and number
    of attempts, for a successful call as well as a failed one.
- `WithLogger(*slog.Logger)`. Each attempt is logged at Debug (method, path, status, attempt,
  duration, request id), and each retry and access token refresh at Info. Credentials, tokens,
  query strings and bodies are never logged.
- `WithAppInfo(name, version, url)` names the application built on the SDK in the User-Agent.
- `Client.String` and `Client.GoString`, which print a client without its credentials.
- A `Retry-After` header given as an HTTP date is honoured, as well as one in seconds.
- Examples for logging, `ResponseMeta` and parameters the SDK does not know yet.
- Tests that call every method with every parameter and check every field of its answer, fuzz
  tests for the tolerant decoder and the request bodies, and checks that every response body is
  closed and no goroutine is left running.
- CI (gofmt, go vet, golangci-lint, govulncheck, tests with the race detector on Go 1.23, 1.24
  and the two latest releases, on Linux and macOS, coverage and a short fuzz run), a release
  workflow, an API compatibility check on pull requests, Dependabot, issue and pull request
  templates, `CONTRIBUTING.md` and `SECURITY.md`.

### Changed

- Each resource is its own file, with its service, methods, parameter structs and the models it
  returns (`portfolio.go`, `bids.go`, `offers.go`, ...); `values.go` holds the known-value
  constants and `envelope.go` the envelope's models. No type, method or field changed. `Value(p)`
  reads a nullable field. `examples/` has three programs, and the README a table of every method.
- **Breaking, for method values only:** every method's signature gains the trailing variadic
  `opts ...RequestOption`. Calls compile unchanged. Code that stores a method in a variable of
  the old function type, or declares an interface with the old signatures, needs the new
  parameter added.
- The User-Agent is now `cruworldwine-go/<Version> go/<Go version> (<GOOS>; <GOARCH>)`, followed
  by the application named with `WithAppInfo`.
- An answer that cannot be decoded wraps the decoding error as well as `ErrUnexpectedResponse`.

### Security

- A redirect to another host, or from https to http, is refused rather than followed. Following
  it would have sent the `CLIENT-KEY` and `CLIENT-SECRET` headers there: net/http drops only
  `Authorization` and cookies on such a redirect. The client follows redirects through a copy of
  the `*http.Client` given to `WithHTTPClient`, so that client's own policy is unchanged.
- `WithMarket` and `CRU_MARKET` must be a market code such as `uk`. A value such as
  `evil.example/#` could otherwise have pointed the client, and its credentials, at another host.
- `WithRequestHeader` cannot replace the credential headers, and a header net/http would refuse
  fails the call before anything is sent.

## [0.1.0] - 2026-10-24

### Added

- The first release, for Cru Trading API v1: a typed method for every endpoint and a typed model
  for every answer, generated from the OpenAPI spec, on a hand-written client.
- Partner credentials and OAuth connected apps, with the access token refreshed once on a 401
  and the rotated tokens handed to a callback.
- Retries with exponential back-off and `Retry-After`, with a partner write's reference kept
  the same on every retry; a connected app's staged writes and `Confirmations.Confirm`.
- `*APIError` with `errors.Is` kinds, `*ConnectionError`, `Page.All` and `Client.Do`.
- Models that never fail on fields or values added later, and keep their JSON for `Raw` and
  `Field`.

[Unreleased]: https://github.com/cruworldwine/cru-api-go/compare/v0.1.0...HEAD
[0.1.0]: https://github.com/cruworldwine/cru-api-go/releases/tag/v0.1.0
