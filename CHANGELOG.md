# Changelog

All notable changes to this module are recorded here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and versions follow
[Semantic Versioning](https://semver.org/spec/v2.0.0.html). While the major version is 0, a minor
release may change the API incompatibly, and says so here.

## [Unreleased]

## [0.2.0] - 2026-10-26

It changes the API incompatibly (see **Changed (breaking)**), hence 0.2.0 rather than 0.1.1.

### Changed (breaking)

- **Each resource is a package of its own, under `cru/`, and names no longer repeat their
  package.** A resource's package is the first segment of its paths: `bids`, `offers`, `market`,
  `portfolio`, `trades`, `wines`, `warehouses` (with `/warehouses/inbond`), `addresses`
  (`/addresses/inbond`), `confirmations` and `photos`. Each holds the resource's service, the
  parameters of its methods, the models it owns and its known values. `cru/core` holds what they
  share: `Page`, `Write`, `APIError`, `ConnectionError` and the `Err` kinds, `RequestOption` and the
  per-call options, `ResponseMeta`, `Model`, `Field`, the envelope's models and the known values
  several resources use. Package `cru` keeps `NewClient`, the `Client` with a field per service,
  its options, `Ping`, `Do`, `Version` and the pointer helpers, and names core's errors, request
  options, `ResponseMeta`, `Model`, `Field` and shared known values again, as the same types and
  values, so `cru.APIError`, `errors.Is(err, cru.ErrNotFound)` and `cru.CurrencyGBP` work as
  before. Calls read as before, `client.Bids.List(ctx, params)`; the names of their parameters and
  models change:
  - a leading word that names the package, singular or plural, is dropped, unless nothing, or only
    `Error`, would be left: `cru.BidsListParams` is `bids.ListParams`, `cru.MarketBid` is
    `market.Bid`, `cru.PortfolioWine` is `portfolio.Wine`, `cru.BidResult` is `bids.Result`,
    `cru.WineDetail` is `wines.Detail` and `cru.BidStatusLive` is `bids.StatusLive`, while
    `warehouses.Warehouse` and `confirmations.ConfirmationError` keep their names;
  - a service is named by the words of its resource that do not name its package: `cru.BidsService`
    is `bids.Service`, `cru.InbondWarehousesService` is `warehouses.InbondService`, and its
    `cru.InbondWarehousesSuggestParams` are `warehouses.InbondSuggestParams`;
  - `cru.Page[T]` and `cru.Write[T]` are `core.Page[T]` and `core.Write[T]`, as Go 1.23 cannot
    alias a generic type; `cru.StagedConfirmation` is `core.StagedConfirmation`, and
    `cru.ActionResult` and `cru.InstructionResult` are `confirmations.ActionResult` and
    `confirmations.InstructionResult`;
  - the known values only one resource uses are in its package: `bids.Status*`,
    `portfolio.LocationType*`, `trades.PurchaseUnitStatus*` and `trades.SaleStatus*`.

  ```go
  import (
  	"github.com/cruworldwine/cru-api-go/cru"
  	"github.com/cruworldwine/cru-api-go/cru/bids"
  )

  w, err := client.Bids.Place(ctx, &bids.PlaceParams{LWIN18: lwin, Quantity: 2, Price: 1200.5})
  ```
- **The package moved to `github.com/cruworldwine/cru-api-go/cru`.** Change the import path from
  `github.com/cruworldwine/cru-api-go` to `github.com/cruworldwine/cru-api-go/cru`, with the
  packages of the resources you use under it, as the entry above says:

  ```go
  import "github.com/cruworldwine/cru-api-go/cru"
  ```

  The repository root now holds only folders and project files:
  - `cru/` is the package users import, with a package per resource and `cru/core` under it:
    the generated API surface and the hand-written public types;
  - `internal/transport` (the HTTP pipeline: requests, credentials, per-call options, retries,
    references, the OAuth refresh, the redirect guard, logging), `internal/apijson` (tolerant
    decoding and the envelope), `internal/apiquery` (query encoding) and `internal/version`
    (Version and the User-Agent) hold the implementation, each with its own tests;
  - `test/integration` holds the black-box tests: the capture replay and a live smoke test;
  - `api/openapi.yaml` is the spec copy, formerly `spec/openapi.yaml`;
  - `docs/` has guides, and `hack/` the scripts behind a new Makefile, whose targets CI runs.

### Added

- The sandbox. `WithEnvironment(cru.EnvironmentSandbox)`, or `CRU_ENVIRONMENT=sandbox`, sends
  the client to `https://{market}.crusandbox.com/api/v1`, where no real money or wine changes
  hands, with credentials issued for the sandbox. `cru.EnvironmentLive`, the default, is
  `https://{market}.cruworldwine.com/api/v1`. Both are read in any case, and `NewClient` refuses
  any other value. A base URL still wins over the market and the environment.
- The table of markets, generated from the spec. `cru.Markets()` lists them, uk first, and
  `cru.LookupMarket(code)` finds one in any case; each is a `cru.MarketInfo` with its code, name,
  currency (ISO 4217), symbol and base URL in each environment (`LiveURL`, `SandboxURL` and
  `URL(env)`). `Client.MarketInfo()` returns the client's market: the one it was made for, or,
  with a base URL, the market whose host that is, else the one `WithMarket` or `CRU_MARKET`
  named. A valid market code the table lacks still picks its host.
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
  and the two latest releases, on Linux and macOS, coverage of each package and a short fuzz
  run), a release workflow, an API compatibility check on pull requests, Dependabot, issue and
  pull request templates, `CONTRIBUTING.md` and `SECURITY.md`.
- A Makefile (`make help` lists its targets): `generate`, `verify-generated`, `fmt`, `vet`,
  `lint`, `test`, `race`, `cover`, `fuzz`, `vuln`, `integration`, `examples`, `tidy`, `apidiff`
  and `ci`, with the tools pinned by version and run with `go run`.
- Guides in `docs/`: authentication, configuration, pagination, writes and confirmations,
  errors, compatibility and architecture.

### Changed

- `Value(p)` reads a nullable field. `examples/` has three programs, and the README a table of
  every method. (Each resource, first given a file of its own, is now a package of its own: see
  Changed (breaking).)
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

[Unreleased]: https://github.com/cruworldwine/cru-api-go/compare/v0.2.0...HEAD
[0.2.0]: https://github.com/cruworldwine/cru-api-go/compare/v0.1.0...v0.2.0
[0.1.0]: https://github.com/cruworldwine/cru-api-go/releases/tag/v0.1.0
