<p align="center">
  <a href="https://developers.cruworldwine.com">
    <img src="https://uk.cruworldwine.com/skin/frontend/cru/default/images/cru-world-wine-logo.svg" alt="Cru World Wine" width="200">
  </a>
</p>

# Cru Trading API for Go

[![CI](https://github.com/cruworldwine/cru-api-go/actions/workflows/ci.yml/badge.svg)](https://github.com/cruworldwine/cru-api-go/actions/workflows/ci.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/cruworldwine/cru-api-go/cru.svg)](https://pkg.go.dev/github.com/cruworldwine/cru-api-go/cru)
[![Release](https://img.shields.io/github/v/tag/cruworldwine/cru-api-go.svg?sort=semver&label=release)](https://github.com/cruworldwine/cru-api-go/tags)
[![Go version](https://img.shields.io/github/go-mod/go-version/cruworldwine/cru-api-go.svg)](https://github.com/cruworldwine/cru-api-go/blob/main/go.mod)
[![License](https://img.shields.io/github/license/cruworldwine/cru-api-go.svg)](https://github.com/cruworldwine/cru-api-go/blob/main/LICENSE)

The Go SDK for the [Cru Trading API](https://developers.cruworldwine.com), Cru World Wine's
partner REST API: read Cru's live bid and offer books for every wine, manage your own bids and
offers, see your portfolio, purchases and sales, and trade, by buying an offer or selling into a
bid. Every endpoint is a typed method and every answer a typed model, built to keep working as the
API adds fields and values; retries, idempotency, OAuth refresh and staged writes are handled for
you.

## Installation

```
go get github.com/cruworldwine/cru-api-go/cru
```

Go 1.23 or later; standard library only.

```go
import "github.com/cruworldwine/cru-api-go/cru"
```

## Connecting

With a partner credential, whose writes run at once:

```go
client, err := cru.NewClient(cru.WithClientKey("cru_live_...", secret), cru.WithMarket("uk"))
if err != nil {
	log.Fatal(err)
}
p, err := client.Ping(ctx)
fmt.Println(p.Client.Market, p.Client.Currency, p.Client.Scopes)
```

As a connected app (OAuth 2.0), whose writes the customer confirms:

```go
client, err := cru.NewClient(
	cru.WithAccessToken(accessToken),
	cru.WithOAuthRefresh(refreshToken, clientID, clientSecret, func(t cru.Token) {
		save(t.AccessToken, t.RefreshToken)
	}),
)
```

Unset options fall back to `CRU_CLIENT_KEY`, `CRU_CLIENT_SECRET`, `CRU_ACCESS_TOKEN`,
`CRU_MARKET`, `CRU_ENVIRONMENT` and `CRU_BASE_URL`, so `cru.NewClient()` alone works when they
are set.

## Sandbox

The sandbox, at `https://{market}.crusandbox.com/api/v1`, is for building and testing: no real
money or wine changes hands there. It has the same markets as the live API and takes credentials
issued for the sandbox:

```go
client, err := cru.NewClient(
	cru.WithClientKey("cru_test_...", secret),
	cru.WithEnvironment(cru.EnvironmentSandbox), // or CRU_ENVIRONMENT=sandbox
	cru.WithMarket("hk"),                        // https://hk.crusandbox.com/api/v1
)
```

`cru.Markets()` and `cru.LookupMarket("hk")` give each market's name, currency and symbol, and
`client.MarketInfo()` the client's own, without a call.

## Typed models

Each resource is a package of its own under `cru/`, named after its path: `bids`, `offers`,
`market`, `portfolio`, `trades`, `wines`, `warehouses`, `addresses`, `confirmations` and `photos`.
It holds the parameters of the resource's methods and the models they return:

```go
import "github.com/cruworldwine/cru-api-go/cru/portfolio"

page, err := client.Portfolio.List(ctx, &portfolio.ListParams{IsListed: cru.Bool(true)})
if err != nil {
	return err
}
for wine, err := range page.All(ctx) { // fetches the next pages as it goes
	if err != nil {
		return err
	}
	for _, line := range wine.Portfolio {
		fmt.Println(cru.Value(wine.WineName), line.Quantity, line.StockType,
			cru.Value(line.Pricing.ListingPrice), wine.Currency)
	}
}
```

Parameters go in one struct per call: required ones are values, optional ones pointers set with
`cru.String`, `cru.Int`, `cru.Float` and `cru.Bool`. Nullable fields are pointers, read with
`cru.Value`. Every method also takes trailing request options, such as
`cru.WithRequestTimeout(5*time.Second)`. What every resource shares, `Page` and `Write`, is in
package `core`: the page above is a `*core.Page[portfolio.Wine]`. The errors, request options and
known values shared across resources are in `core` too, and package `cru` names them again, so
code reads `cru.APIError`, `cru.ErrNotFound` and `cru.CurrencyGBP`.

## Documentation

- [Authentication](docs/authentication.md): partner credentials, connected apps and the OAuth
  refresh, the environment.
- [Configuration](docs/configuration.md): client and request options, markets and the sandbox,
  timeouts, retries, logging, request ids, the User-Agent.
- [Pagination](docs/pagination.md): `Page`, `Page.All` and `Next`.
- [Writes and confirmations](docs/writes-and-confirmations.md): results, references and
  idempotency, staged writes and confirming them.
- [Errors](docs/errors.md): `APIError`, its kinds and details, connection errors.
- [Compatibility](docs/compatibility.md): models that survive API changes, `Raw` and `Field`,
  versioning and supported Go versions.
- [Architecture](docs/architecture.md): the package layout, the life of a request, code
  generation, and where to change what.
- The API reference is on [pkg.go.dev](https://pkg.go.dev/github.com/cruworldwine/cru-api-go/cru),
  and the API's own at [developers.cruworldwine.com](https://developers.cruworldwine.com).

## Examples

[`examples/`](examples) has programs to run: `portfolio` prints your portfolio, `market` the bids
and offers on a wine, and `place-bid` places a bid and, as a connected app, confirms it.

```
CRU_CLIENT_KEY=cru_live_... CRU_CLIENT_SECRET=... go run ./examples/portfolio
```

## Versioning

The module follows semantic versioning. While its major version is 0, a minor version may break
the API, and [CHANGELOG.md](CHANGELOG.md) says how; a patch version never does. The latest minor
version receives fixes ([SECURITY.md](SECURITY.md)). The SDK supports the two latest Go releases,
and Go 1.23 as its floor.

## Every method

Generated from the spec ([`api/openapi.yaml`](api/openapi.yaml)); each method's doc comment has
the full description. Every method also takes trailing `...cru.RequestOption`.

<!-- sdkgen:methods -->
| Method | Endpoint | Returns |
|---|---|---|
| `client.Ping(ctx)` | `GET /ping` | `*cru.Ping` |
| `client.Portfolio.List(ctx, *portfolio.ListParams)` | `GET /portfolio` | `*core.Page[portfolio.Wine]` |
| `client.Offers.List(ctx, *offers.ListParams)` | `GET /offers` | `*core.Page[portfolio.Wine]` |
| `client.Offers.Create(ctx, *offers.CreateParams)` | `POST /offers` | `*core.Write[offers.Result]` |
| `client.Offers.Get(ctx, code)` | `GET /offers/{code}` | `*portfolio.Wine` |
| `client.Offers.Update(ctx, code, *offers.UpdateParams)` | `PATCH /offers/{code}` | `*core.Write[offers.Result]` |
| `client.Offers.Remove(ctx, code, *offers.RemoveParams)` | `DELETE /offers/{code}` | `*core.Write[offers.RemoveResult]` |
| `client.Bids.List(ctx, *bids.ListParams)` | `GET /bids` | `*core.Page[bids.OwnBid]` |
| `client.Bids.Place(ctx, *bids.PlaceParams)` | `POST /bids` | `*core.Write[bids.Result]` |
| `client.Bids.Get(ctx, code)` | `GET /bids/{code}` | `*bids.OwnBid` |
| `client.Bids.Update(ctx, code, *bids.UpdateParams)` | `PATCH /bids/{code}` | `*core.Write[bids.Result]` |
| `client.Bids.Cancel(ctx, code, *bids.CancelParams)` | `DELETE /bids/{code}` | `*core.Write[bids.Result]` |
| `client.Bids.Renew(ctx, code, *bids.RenewParams)` | `POST /bids/{code}/renew` | `*core.Write[bids.Result]` |
| `client.Market.List(ctx, *market.ListParams)` | `GET /market` | `*core.Page[market.Wine]` |
| `client.Market.GetWine(ctx, lwin18, *market.GetWineParams)` | `GET /market/wines/{lwin18}` | `*market.Wine` |
| `client.Market.ListBids(ctx, *market.ListBidsParams)` | `GET /market/bids` | `*core.Page[market.Wine]` |
| `client.Market.GetBid(ctx, code)` | `GET /market/bids/{code}` | `*market.BidDetail` |
| `client.Market.GetWineBids(ctx, lwin18, *market.GetWineBidsParams)` | `GET /market/wines/{lwin18}/bids` | `*market.Wine` |
| `client.Market.ListOffers(ctx, *market.ListOffersParams)` | `GET /market/offers` | `*core.Page[market.Wine]` |
| `client.Market.GetOffer(ctx, code)` | `GET /market/offers/{code}` | `*market.OfferDetail` |
| `client.Market.GetWineOffers(ctx, lwin18, *market.GetWineOffersParams)` | `GET /market/wines/{lwin18}/offers` | `*market.Wine` |
| `client.Market.BuyOffer(ctx, code, *market.BuyOfferParams)` | `POST /market/offers/{code}/buy` | `*core.Write[market.BuyResult]` |
| `client.Market.SellToBid(ctx, code, *market.SellToBidParams)` | `POST /market/bids/{code}/sell` | `*core.Write[market.SellResult]` |
| `client.Wines.Search(ctx, *wines.SearchParams)` | `GET /wines` | `*core.Page[wines.Wine]` |
| `client.Wines.Get(ctx, lwin18)` | `GET /wines/{lwin18}` | `*wines.Detail` |
| `client.Trades.ListPurchases(ctx, *trades.ListPurchasesParams)` | `GET /trades/purchases` | `*core.Page[trades.Purchase]` |
| `client.Trades.ListSales(ctx, *trades.ListSalesParams)` | `GET /trades/sales` | `*core.Page[trades.Sale]` |
| `client.Warehouses.List(ctx)` | `GET /warehouses` | `[]warehouses.Warehouse` |
| `client.Warehouses.Get(ctx, code)` | `GET /warehouses/{code}` | `*warehouses.Warehouse` |
| `client.InbondWarehouses.List(ctx)` | `GET /warehouses/inbond` | `[]warehouses.InbondWarehouse` |
| `client.InbondWarehouses.Suggest(ctx, *warehouses.InbondSuggestParams)` | `POST /warehouses/inbond` | `*core.Write[warehouses.InbondWarehouseResult]` |
| `client.InbondWarehouses.Get(ctx, id)` | `GET /warehouses/inbond/{id}` | `*warehouses.InbondWarehouse` |
| `client.InbondAddresses.List(ctx)` | `GET /addresses/inbond` | `[]addresses.InbondAddress` |
| `client.InbondAddresses.Add(ctx, *addresses.InbondAddParams)` | `POST /addresses/inbond` | `*core.Write[addresses.InbondAddressResult]` |
| `client.InbondAddresses.Remove(ctx, id, *addresses.InbondRemoveParams)` | `DELETE /addresses/inbond/{id}` | `*core.Write[addresses.InbondAddressResult]` |
| `client.Confirmations.Get(ctx, code)` | `GET /confirmations/{code}` | `*confirmations.Confirmation` |
| `client.Confirmations.Confirm(ctx, code)` | `POST /confirmations/{code}` | `*core.Write[confirmations.ActionResult]` |
| `client.Confirmations.Cancel(ctx, code)` | `DELETE /confirmations/{code}` | `error only` |
| `client.Photos.Get(ctx, id)` | `GET /photos/{id}` | `[]byte` |
<!-- /sdkgen:methods -->

## Contributing

[CONTRIBUTING.md](CONTRIBUTING.md) covers the layout, the Makefile, testing, generated code and
releases.

## License

Apache License 2.0. See [LICENSE](LICENSE).
