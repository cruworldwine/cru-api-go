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
`CRU_MARKET` and `CRU_BASE_URL`, so `cru.NewClient()` alone works when they are set.

## Typed models

```go
page, err := client.Portfolio.List(ctx, &cru.PortfolioListParams{IsListed: cru.Bool(true)})
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
`cru.WithRequestTimeout(5*time.Second)`.

## Documentation

- [Authentication](docs/authentication.md): partner credentials, connected apps and the OAuth
  refresh, the environment.
- [Configuration](docs/configuration.md): client and request options, timeouts, retries, logging,
  request ids, the User-Agent.
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

## Contributing

[CONTRIBUTING.md](CONTRIBUTING.md) covers the layout, the Makefile, testing, generated code and
releases.

## License

Apache License 2.0. See [LICENSE](LICENSE).
