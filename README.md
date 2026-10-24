<p align="center">
  <a href="https://developers.cruworldwine.com">
    <img src="https://uk.cruworldwine.com/skin/frontend/cru/default/images/cru-world-wine-logo.svg" alt="Cru World Wine" width="200">
  </a>
</p>

<p align="center">
  <a href="https://github.com/cruworldwine/cru-api-go/actions/workflows/ci.yml"><img src="https://github.com/cruworldwine/cru-api-go/actions/workflows/ci.yml/badge.svg" alt="CI"></a>
  <a href="https://pkg.go.dev/github.com/cruworldwine/cru-api-go"><img src="https://pkg.go.dev/badge/github.com/cruworldwine/cru-api-go.svg" alt="Go Reference"></a>
</p>

# Cru Trading API for Go

The Go SDK for the [Cru Trading API](https://developers.cruworldwine.com), Cru World Wine's
partner REST API. Read Cru's live bid and offer books for every wine, manage your own bids and
offers, see your portfolio, purchases and sales, and trade: buy an offer or sell into a bid.

## Installation

```
go get github.com/cruworldwine/cru-api-go
```

Go 1.23 or later; standard library only. The package is `cru`.

## Connecting

There are two ways to connect. Each answers on one market's host,
`https://{market}.cruworldwine.com/api/v1`, where market is `uk` (the default), `us`, `hk`, `sg`,
`eu`, `ae` or `cn`. A credential works on its own market only.

### With a partner credential

Cru issues a partner credential: a key (`cru_live_...`) and a secret, which is shown once. Keep
both out of source control. A partner credential's writes run at once.

```go
import cru "github.com/cruworldwine/cru-api-go"

client, err := cru.NewClient(cru.WithClientKey("cru_live_...", secret), cru.WithMarket("uk"))
```

### As a connected app (OAuth 2.0)

An app that acts for a Cru customer gets their tokens with the OAuth 2.0 authorization code flow
with PKCE (S256), at `/api/oauth/authorize` and `/api/oauth/token` on the market's host. The SDK
uses the access token and, given the refresh token and your app's client id, refreshes it once when
it expires. A refresh token works once, so store the new tokens the callback hands you. A
connected app's writes are staged: nothing happens until the customer confirms them.

```go
client, err := cru.NewClient(
	cru.WithAccessToken(accessToken),
	cru.WithOAuthRefresh(refreshToken, clientID, clientSecret, func(t cru.Token) {
		save(t.AccessToken, t.RefreshToken)
	}),
)
```

### From the environment

Unset options fall back to `CRU_CLIENT_KEY`, `CRU_CLIENT_SECRET`, `CRU_ACCESS_TOKEN`,
`CRU_MARKET` and `CRU_BASE_URL`, so `cru.NewClient()` alone works when they are set. Other
options: `WithBaseURL`, `WithHTTPClient`, `WithMaxRetries` (default 2), `WithTimeout` (per
attempt, default 30s), `WithLogger` and `WithAppInfo`. A client is safe for concurrent use;
create one per credential and share it.

`WithAppInfo` names your application in the User-Agent, after the SDK's own
`cruworldwine-go/<version> go/<Go version> (<os>; <arch>)`:

```go
client, err := cru.NewClient(cru.WithAppInfo("cellar-sync", "2.1.0", "https://example.com/cellar-sync"))
```

### Check the connection

```go
p, err := client.Ping(ctx)
if err != nil {
	log.Fatal(err)
}
fmt.Println(p.Client.Market, p.Client.Currency, p.Client.Scopes)
```

## Typed models

Every endpoint is a typed method, and every answer decodes into a typed model: a struct per API
object, with a field per API field and the API's descriptions as its doc comments. Nullable
fields are pointers; `cru.Value(p)` reads one, or gives its zero value when it is nil.

```go
page, err := client.Portfolio.List(ctx, &cru.PortfolioListParams{IsListed: cru.Bool(true)})
if err != nil {
	return err
}
for _, wine := range page.Items { // []cru.PortfolioWine
	for _, line := range wine.Portfolio { // []cru.PortfolioLine
		fmt.Println(cru.Value(wine.WineName), line.Quantity, line.StockType,
			cru.Value(line.Pricing.ListingPrice), cru.Value(line.Pricing.NetProceeds), wine.Currency)
	}
}
```

Each resource is one file: its service, methods, parameter structs and the models it returns.
For example, `portfolio.go` has `PortfolioService`, `PortfolioListParams`, `PortfolioWine`,
`PortfolioLine`, `LinePricing` and `LineWarehouse`. `market.go`, `bids.go`, `offers.go`,
`trades.go`, `wines.go`, `warehouses.go`, `inbond_warehouses.go`, `inbond_addresses.go`,
`confirmations.go`, `photos.go` and `ping.go` hold the rest. `values.go` has the known values of
fields such as `StockType` and `Currency`, as constants.

[`examples/`](examples) has programs to run: `portfolio` prints your portfolio, `market` the
bids and offers on a wine, and `place-bid` places a bid and, as a connected app, confirms it.

## Calls

Every method takes a `context.Context` first and is grouped by resource: `client.Bids`,
`client.Offers`, `client.Market`, `client.Portfolio`, `client.Trades`, `client.Wines`,
`client.Warehouses`, `client.InbondWarehouses`, `client.InbondAddresses`, `client.Photos`,
`client.Confirmations`, and `client.Ping`. [Every method](#every-method) lists them all.

Parameters go in one struct per call. Required ones are values; optional ones are pointers, set
with `cru.String`, `cru.Int`, `cru.Float` and `cru.Bool`. A nil field, or nil params, sends nothing.

```go
bid, err := client.Bids.Get(ctx, code)

w, err := client.Bids.Place(ctx, &cru.BidsPlaceParams{
	LWIN18:    "100000120151200750",
	Quantity:  2,
	Price:     1200.50,
	ClientRef: cru.String("order-17"),
})
```

### Paging

```go
page, err := client.Portfolio.List(ctx, &cru.PortfolioListParams{Limit: cru.Int(100)})
for wine, err := range page.All(ctx) { // fetches the next pages as it goes
	if err != nil {
		return err
	}
	fmt.Println(wine.LWIN18, wine.TotalQuantity)
}
```

A `*cru.Page[T]` also has `Items`, `Limit`, `Offset`, `Total` (nil when not counted), `HasMore`,
the raw `Data`, `Meta`, and `Next(ctx)`, which returns nil after the last page.

### Writes and confirmations

A write returns a `*cru.Write[T]`. For a partner credential, `Result` holds what it did and
`Replayed` says whether it was the stored answer to an earlier request with the same reference.
For a connected app, `Staged` is true and `Confirmation` holds the code:

```go
w, err := client.Offers.Update(ctx, code, &cru.OffersUpdateParams{Price: cru.Float(1350)})
// show w.Confirmation.Preview.Summary to the customer; on their yes:
done, err := client.Confirmations.Confirm(ctx, w.Confirmation.Code)
switch r := done.Result.Value.(type) {
case *cru.OfferResult:
	// ...
case *cru.InstructionResult: // an action this SDK does not know; see done.Result.Raw()
}
```

A partner write needs a `reference`, its idempotency key. Leave `Reference` nil and the SDK sends
`sdk_` and 32 hex characters, the same on every retry, so a retry can never act twice.

### Per-call options

Every method takes trailing `cru.RequestOption`s, which change that call only:

```go
bid, err := client.Bids.Get(ctx, code,
	cru.WithRequestTimeout(5*time.Second),             // each attempt, in place of WithTimeout
	cru.WithRequestMaxRetries(0),                      // in place of WithMaxRetries
	cru.WithRequestHeader("traceparent", traceparent), // any header but the credentials
)
```

`WithExtraQuery(key, value)` and `WithExtraBodyField(key, value)` send a query parameter or body
field this version of the SDK does not know yet, and replace one it does.

### Request ids and `ResponseMeta`

Every answer carries a request id, which Cru support needs to find the call. An error has it in
`APIError.RequestID` and a write in `Write.RequestID`; for any call, `WithResponseInto` fills a
`cru.ResponseMeta` with the status, the headers, the request id and how many attempts were made:

```go
var meta cru.ResponseMeta
page, err := client.Portfolio.List(ctx, nil, cru.WithResponseInto(&meta))
log.Println(meta.StatusCode, meta.RequestID, meta.Attempts)
```

### Errors

```go
_, err := client.Bids.Get(ctx, code)
switch {
case errors.Is(err, cru.ErrNotFound):
case errors.Is(err, cru.ErrPriceChanged): // preview again
case errors.Is(err, cru.ErrValidation):   // any VALIDATION_* code
}
var apiErr *cru.APIError
if errors.As(err, &apiErr) {
	log.Println(apiErr.StatusCode, apiErr.Code, apiErr.Field, apiErr.Details, apiErr.RequestID)
}
```

The kinds are `ErrBadRequest`, `ErrAuthentication`, `ErrPermissionDenied`, `ErrNotFound`,
`ErrMethodNotAllowed`, `ErrConflict`, `ErrGone`, `ErrUnprocessable`, `ErrRateLimited`,
`ErrServer`, and by code `ErrValidation`, `ErrPriceChanged`, `ErrRequestInProgress` and
`ErrTradeRefused`. The codes themselves are the `ErrorCode*` constants. A request that got no
answer is a `*cru.ConnectionError` (`Timeout()` tells a timeout); a cancelled context comes back
as the context's error.

### Retries

Reads and partner writes retry on network errors, timeouts, 429, 500, 502, 503, 504 and 409
`REQUEST_IN_PROGRESS`, with backoff of 0.5s doubling to 8s (±25% jitter), or as long as
`Retry-After` or `details.retry_after` says, up to 30s. A connected app's writes are never
retried. On a 401, a connected app with a refresh token refreshes once and retries.

The client never follows a redirect to another host, or from https to http, since that would
send the credentials there.

### Logging and debugging

Give the client a `*slog.Logger` to see what it sends:

```go
logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug}))
client, err := cru.NewClient(cru.WithLogger(logger))
```

Each attempt is logged at Debug, with its method, path, status, attempt number, duration and
request id, and each retry and access token refresh at Info. The client key and secret, tokens,
query strings and request and response bodies are never logged. Without `WithLogger`, nothing is.

## Built for an API that changes

- Unknown fields never fail decoding. Every model keeps the JSON it came from: `m.Raw()`, or
  `cru.Field(m, "new_field", &v)` for one field.
- Fields with known values are plain strings; the known values are constants (`cru.CurrencyGBP`,
  `cru.BidStatusLive`, `cru.ActionBidPlace`, ...), and any other value decodes as it is.
- Required, non-nullable fields are plain values. Nullable or optional fields are pointers, nil
  for null or absent. Lists are slices and free-form objects maps, nil when null or absent.
- `integer` is `int64` and `number` is `float64`. `1000.0` and `"1000"` decode into an int64; a
  value that does not fit its field is left unset, never failing the response, and stays in `Raw`.
- Dates and times are strings, as the API sends them.

For an endpoint this SDK does not know yet, `client.Do(ctx, method, path, query, body)` returns
the envelope (`StatusCode`, `Header`, `Status`, `Message`, `Meta`, `Data`, `Body`), with the same
errors, retries and references as every other call.

## Every method

Generated from the spec; each method's doc comment has the full description. Every method also
takes trailing `...cru.RequestOption`.

<!-- sdkgen:methods -->
| Method | Endpoint | Returns |
|---|---|---|
| `client.Ping(ctx)` | `GET /ping` | `*Ping` |
| `client.Portfolio.List(ctx, *PortfolioListParams)` | `GET /portfolio` | `*Page[PortfolioWine]` |
| `client.Offers.List(ctx, *OffersListParams)` | `GET /offers` | `*Page[PortfolioWine]` |
| `client.Offers.Create(ctx, *OffersCreateParams)` | `POST /offers` | `*Write[OfferResult]` |
| `client.Offers.Get(ctx, code)` | `GET /offers/{code}` | `*PortfolioWine` |
| `client.Offers.Update(ctx, code, *OffersUpdateParams)` | `PATCH /offers/{code}` | `*Write[OfferResult]` |
| `client.Offers.Remove(ctx, code, *OffersRemoveParams)` | `DELETE /offers/{code}` | `*Write[OfferRemoveResult]` |
| `client.Bids.List(ctx, *BidsListParams)` | `GET /bids` | `*Page[OwnBid]` |
| `client.Bids.Place(ctx, *BidsPlaceParams)` | `POST /bids` | `*Write[BidResult]` |
| `client.Bids.Get(ctx, code)` | `GET /bids/{code}` | `*OwnBid` |
| `client.Bids.Update(ctx, code, *BidsUpdateParams)` | `PATCH /bids/{code}` | `*Write[BidResult]` |
| `client.Bids.Cancel(ctx, code, *BidsCancelParams)` | `DELETE /bids/{code}` | `*Write[BidResult]` |
| `client.Bids.Renew(ctx, code, *BidsRenewParams)` | `POST /bids/{code}/renew` | `*Write[BidResult]` |
| `client.Market.List(ctx, *MarketListParams)` | `GET /market` | `*Page[MarketWine]` |
| `client.Market.GetWine(ctx, lwin18, *MarketGetWineParams)` | `GET /market/wines/{lwin18}` | `*MarketWine` |
| `client.Market.ListBids(ctx, *MarketListBidsParams)` | `GET /market/bids` | `*Page[MarketWine]` |
| `client.Market.GetBid(ctx, code)` | `GET /market/bids/{code}` | `*MarketBidDetail` |
| `client.Market.GetWineBids(ctx, lwin18, *MarketGetWineBidsParams)` | `GET /market/wines/{lwin18}/bids` | `*MarketWine` |
| `client.Market.ListOffers(ctx, *MarketListOffersParams)` | `GET /market/offers` | `*Page[MarketWine]` |
| `client.Market.GetOffer(ctx, code)` | `GET /market/offers/{code}` | `*MarketOfferDetail` |
| `client.Market.GetWineOffers(ctx, lwin18, *MarketGetWineOffersParams)` | `GET /market/wines/{lwin18}/offers` | `*MarketWine` |
| `client.Market.BuyOffer(ctx, code, *MarketBuyOfferParams)` | `POST /market/offers/{code}/buy` | `*Write[BuyResult]` |
| `client.Market.SellToBid(ctx, code, *MarketSellToBidParams)` | `POST /market/bids/{code}/sell` | `*Write[SellResult]` |
| `client.Wines.Search(ctx, *WinesSearchParams)` | `GET /wines` | `*Page[Wine]` |
| `client.Wines.Get(ctx, lwin18)` | `GET /wines/{lwin18}` | `*WineDetail` |
| `client.Trades.ListPurchases(ctx, *TradesListPurchasesParams)` | `GET /trades/purchases` | `*Page[Purchase]` |
| `client.Trades.ListSales(ctx, *TradesListSalesParams)` | `GET /trades/sales` | `*Page[Sale]` |
| `client.Warehouses.List(ctx)` | `GET /warehouses` | `[]Warehouse` |
| `client.Warehouses.Get(ctx, code)` | `GET /warehouses/{code}` | `*Warehouse` |
| `client.InbondWarehouses.List(ctx)` | `GET /warehouses/inbond` | `[]InbondWarehouse` |
| `client.InbondWarehouses.Suggest(ctx, *InbondWarehousesSuggestParams)` | `POST /warehouses/inbond` | `*Write[InbondWarehouseResult]` |
| `client.InbondWarehouses.Get(ctx, id)` | `GET /warehouses/inbond/{id}` | `*InbondWarehouse` |
| `client.InbondAddresses.List(ctx)` | `GET /addresses/inbond` | `[]InbondAddress` |
| `client.InbondAddresses.Add(ctx, *InbondAddressesAddParams)` | `POST /addresses/inbond` | `*Write[InbondAddressResult]` |
| `client.InbondAddresses.Remove(ctx, id, *InbondAddressesRemoveParams)` | `DELETE /addresses/inbond/{id}` | `*Write[InbondAddressResult]` |
| `client.Confirmations.Get(ctx, code)` | `GET /confirmations/{code}` | `*Confirmation` |
| `client.Confirmations.Confirm(ctx, code)` | `POST /confirmations/{code}` | `*Write[ActionResult]` |
| `client.Confirmations.Cancel(ctx, code)` | `DELETE /confirmations/{code}` | `error only` |
| `client.Photos.Get(ctx, id)` | `GET /photos/{id}` | `[]byte` |
<!-- /sdkgen:methods -->

## Running the tests

The tests need no credentials and make no network calls: every call goes to a local test server.

```
go vet ./... && go test -race ./...
```

- `client_test.go`, `encode_test.go`, `retry_test.go`, `refresh_test.go`, `errors_test.go`,
  `unwrap_test.go`, `model_test.go`, `options_test.go` and `log_test.go`: authentication, the
  environment, the User-Agent, query, path and body encoding, references kept across retries,
  retries and back-off, context cancellation, redirects, the OAuth refresh, every error kind,
  staged writes and confirmations, paging, per-call options, logging that never holds a secret,
  and fields and values this version does not know.
- `sample_test.go`: calls every method with every parameter set, from invented samples generated
  with the code, and checks what it sent and that every field of the answer decoded.
- `fuzz_test.go`: fuzz targets for the tolerant decoder, the response path and request bodies;
  their seeds run with the tests. See [CONTRIBUTING.md](CONTRIBUTING.md) to fuzz.
- `leak_test.go`: every response body is closed and no goroutine is left running.
- `example_test.go`: usage examples, compiled with the tests and shown on pkg.go.dev.
- `replay_test.go`: replays real API answers through the method that asks for each one and checks
  every decoded field against what the API sent. It runs when `CRU_CAPTURES` names a capture run
  (a directory with `captures/*.json`), and is skipped otherwise. Captures hold customer data, so
  keep them out of the repository.

## Versioning and support

The module follows [semantic versioning](https://semver.org). While its major version is 0, a
minor version may change the API incompatibly; [CHANGELOG.md](CHANGELOG.md) says what changed in
each version and what to do about it, and a patch version never breaks. The latest minor version
receives fixes, security fixes included ([SECURITY.md](SECURITY.md)).

The SDK supports the two latest Go releases, and Go 1.23 as its floor; CI tests all of them.

## Development

The resource files, `services.go`, `values.go`, `envelope.go`, `replay_gen_test.go`,
`sample_gen_test.go` and the method table above are generated from the OpenAPI spec
(`spec/openapi.yaml` is the copy they were generated from); each generated file starts with
`DO NOT EDIT`. The rest is written by hand. Cru regenerates them from the API's own repository
after each API change:

```
uv run --quiet --with pyyaml==6.0.3 python docs/api/tools/sdkgen/generate.py --go <this checkout>
```

[CONTRIBUTING.md](CONTRIBUTING.md) covers building, testing, fuzzing, linting and releases.

## License

Apache License 2.0. See [LICENSE](LICENSE).
