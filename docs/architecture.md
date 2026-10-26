# Architecture

## Package layout

```
cru-api-go/
├── cru/                 package cru, the front door: Client, its options, Ping, Do
│   ├── core/            what the resources share: Page, Write, errors, request options, envelope models
│   ├── bids/            one package per resource: its service, parameters, models and known values
│   ├── offers/
│   ├── market/
│   ├── portfolio/
│   ├── trades/
│   ├── wines/
│   ├── warehouses/      the Cru warehouses (Service) and the inbond ones (InbondService)
│   ├── addresses/       your inbond addresses (InbondService)
│   ├── confirmations/   confirming a staged write, and the action's typed result
│   └── photos/
├── internal/
│   ├── caller/          how a generated method calls: Endpoint, GetOne, GetPage, DoWrite, ...
│   ├── corelink/        how the internal packages reach what core keeps unexported
│   ├── transport/       the HTTP pipeline
│   ├── apijson/         tolerant JSON decoding and the envelope
│   ├── apiquery/        query string encoding
│   ├── version/         Version and the User-Agent
│   └── testkit/         what the unit tests share; only tests import it
├── test/integration/    black-box tests through the public API
├── examples/            programs to run: portfolio, market, place-bid
├── api/openapi.yaml     the copy of the spec the code was generated from
├── docs/                these guides
└── hack/                scripts the Makefile runs
```

A resource's package is the first segment of its paths: `/warehouses` and `/warehouses/inbond`
share package `warehouses`, and `/addresses/inbond` is package `addresses`. Names do not repeat
their package: a leading word naming it is dropped, so `MarketBid` is `market.Bid`,
`BidsListParams` is `bids.ListParams` and `BidStatusLive` is `bids.StatusLive`, unless nothing,
or only `Error`, would be left (`warehouses.Warehouse`, `confirmations.ConfirmationError`). A
service is named by the words of its resource that do not name the package: `bids.Service`,
`warehouses.InbondService`.

The imports go one way, so there is no cycle:

- `cru` imports every resource package, `core` and the internal packages, and nothing imports it;
- a resource package imports `core` and the internal packages, and another resource package only
  where one's models hold the other's: `offers` imports `portfolio` (an offer is a
  `portfolio.Wine`), `wines` imports `market` (a wine's `market.Bid`, `market.Offer` and
  `market.LastTrade`), and `confirmations` imports the packages whose results it decodes;
- `core` imports only the internal packages below it;
- `internal/caller` imports `core`, and `core` sets `internal/corelink`, which imports nothing of
  the SDK's.

| Package | Holds | Knows of the API's models? |
|---|---|---|
| `cru` | `Client` and its options, `Ping`, `Do` and `Response`, `Version`, `String`, `Int`, `Float`, `Bool` and `Value`; and core's errors, request options, `ResponseMeta`, `Model`, `Field` and shared known values again, as the same types and values | `Ping` only |
| `cru/<resource>` | the resource's service and methods, their parameter structs, the models it owns and its known values (`bids.StatusLive`) | yes |
| `cru/core` | `Page`, `Write`, `APIError` and its kinds, `ConnectionError`, `RequestOption` and the per-call options, `ResponseMeta`, `Model` and `Field`, the envelope's models, `StagedConfirmation`, and the known values more than one resource uses | the envelope's |
| `internal/caller` | `Endpoint`, the generic calls a generated method makes (`GetOne`, `GetList`, `GetPage`, `DoWrite`, `WriteOf`, `DoEmpty`, `DoBinary`), `ExpandPath`, and the registry that lets `cru` make each resource's service | no |
| `internal/corelink` | the two functions `core` lends the internal packages: turning the transport's errors into core's, and giving a `Page` its next page | no |
| `internal/transport` | building a request (URL, query, JSON body, headers, credentials), the per-call options, retries and back-off, the partner write's reference, the OAuth refresh, the redirect guard, logging | no: it answers with status, headers and body |
| `internal/apijson` | `Split` and the `Decode` family the generated models decode with, `Lookup` for `Field`, and reading the envelope (data, lists, request id, first error) | no |
| `internal/apiquery` | writing parameters into the query string | no |
| `internal/version` | `Version` and the User-Agent | no |
| `internal/testkit` | the test server, invented fixtures, `EveryOperation` and `Decodes`, which every package's generated tests run | no |

The internal packages are free to change between versions; `cru`, `cru/core` and the resource
packages are the API, and changes to them follow [the versioning policy](compatibility.md#versioning).

Three small seams keep the internal plumbing out of the exported API:

- **A Client hands each service its `*caller.Caller`** without the resource packages exporting a
  constructor: each registers one (`caller.Register`) when it is initialised, and the generated
  `cru/services.go` makes the services with `caller.NewService`.
- **A `RequestOption` is opaque**: a function over an unexported alias of the transport's options,
  which `internal/caller` applies.
- **`core` keeps its errors' and pages' insides unexported**; `internal/corelink` holds the two
  functions `core` sets when it is initialised, so `internal/caller` can build a `*core.APIError`
  or `*core.ConnectionError` and give a `*core.Page` its next page.

## The life of a request

Take `client.Bids.Get(ctx, code, cru.WithRequestTimeout(5*time.Second))`:

1. **The generated method** (`cru/bids/bids.go`) escapes `code` into the path, `/bids/{code}`,
   with `caller.ExpandPath`, and describes the call as a `caller.Endpoint`: method, path, query
   (from the params struct, through `internal/apiquery`), body, the key the result is under in
   `data` (`bid`), and the request options. It hands it to `caller.GetOne[bids.OwnBid]`.
2. **The request options** are applied to a `transport.Options` by `internal/caller`: each
   `RequestOption` is a function that sets a header, a timeout, retries, an extra query parameter
   or body field, or an observer for `WithResponseInto`. One that cannot apply records an error,
   and nothing is sent.
3. **The transport** (`internal/transport`) encodes the request once: the URL and query, the
   JSON body with any extra fields and, for a partner's write, a `reference` when it has none,
   so every attempt sends the same bytes. It then sends attempts:
   - each with the User-Agent, the credentials (`CLIENT-KEY` and `CLIENT-SECRET`, or the bearer
     token) and the call's headers, under the attempt timeout;
   - through a copy of the `*http.Client` that refuses a redirect to another host;
   - logging each attempt at Debug, never with a credential, query string or body;
   - on a 401, refreshing the access token once (concurrent calls share one refresh) and trying
     again;
   - on a network error, a timeout, 429, 5xx or `REQUEST_IN_PROGRESS`, when the call may be
     retried, waiting (back-off, or as long as the API says) and trying again.

   It answers with the raw response, or a `*transport.StatusError` (an answer of 400 or more)
   or a `*transport.ConnectionError`.
4. **The caller** (`internal/caller`) has `core` turn those into `*core.APIError` (its fields read
   from the error envelope) or `*core.ConnectionError`, then reads the envelope with
   `internal/apijson` and decodes `data.bid` into a `bids.OwnBid`. For a list that pages it builds
   a `*core.Page`, whose `Next` sends the same endpoint with the next offset; for a write, a
   `*core.Write`.
5. **The model** (`cru/bids/bids.go`) decodes field by field, with `apijson.Split` and the
   `Decode` family: a field that does not fit is left unset, and the JSON is kept for `Raw` and
   `Field`.

## How code generation works

The API's surface is generated from its OpenAPI spec, by the generator in Cru's API repository
(`docs/api/tools/sdkgen`, with `gen_go.py` for Go). `make generate`, with `CRU_API_REPO` naming
that checkout, writes:

- into each resource package, `cru/<resource>/`: `doc.go` (the package comment), a file per
  resource with its service, methods, parameter structs and the models it owns (such as
  `bids.go`, or `warehouses.go` and `inbond.go`), `values.go` (its known values) and
  `sample_gen_test.go` (an invented call and answer for every operation, which
  `TestEveryOperation` checks, and `FuzzDecodeModels` over its models);
- into `cru/core/`: `models.go` (the envelope's models and `StagedConfirmation`), `values.go`
  (the known values more than one resource uses) and `models_gen_test.go`;
- into `cru/`: `ping.go`, `services.go` (`Resources`, the field per service a `Client` embeds),
  `values.go` (core's known values named again), `markets_gen.go` (each environment's base URL
  template, from the spec's `servers`, and the markets, from `x-cru-markets`, which the
  hand-written `markets.go` serves) and `sample_gen_test.go`;
- `test/integration/replay_gen_test.go`, which calls each operation from a captured request;
- `api/openapi.yaml`, the copy of the spec it generated from, and the method table in the README.

Each generated file starts with `// Code generated by sdkgen ... DO NOT EDIT.` Generation is
deterministic: `make verify-generated` regenerates and fails if anything changed, which catches a
generated file edited by hand.

## Where to change what

| To change | Edit |
|---|---|
| an endpoint, parameter, model, field, known value or doc comment | the spec, in Cru's API repository, then `make generate` |
| how every method or model is generated, or which package a resource is in | `gen_go.py`, in Cru's API repository, then `make generate` |
| a market, or an environment's host | the spec's `x-cru-markets` or `servers`, then `make generate`; a new environment also needs `cru/markets.go` and `gen_go.py` |
| client options, `Do`, environments and market lookups, the names `cru` gives core's types again | `cru/` (the hand-written files: `client.go`, `cru.go`, `doc.go`, `errors.go`, `markets.go`, `options.go`, `model.go`) |
| errors, request options, paging, writes | `cru/core/` (the hand-written files: `errors.go`, `options.go`, `page.go`, `write.go`, `model.go`, `link.go`, `doc.go`) |
| a confirmed action's result | `cru/confirmations/action.go` |
| how a generated method's call is sent and decoded | `internal/caller` |
| retries, timeouts, the refresh, redirects, headers, logging | `internal/transport` |
| how a field is decoded | `internal/apijson` |
| how a query parameter is written | `internal/apiquery` |
| the version or the User-Agent | `internal/version` |

A file that starts with `Code generated` is never edited by hand. Each package has its own tests,
in its own folder, and `make cover` holds each to 85% or more by its own tests alone.
