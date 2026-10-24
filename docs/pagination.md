# Pagination

A list that pages with `limit` and `offset` returns a `*cru.Page[T]`: `client.Portfolio.List`,
`client.Offers.List`, `client.Bids.List`, `client.Market.List`, `client.Wines.Search`,
`client.Trades.ListPurchases` and the others marked `*Page[...]` in the
[method table](../README.md#every-method).

## Walk every item

`Page.All` is an iterator over the items of this page and of every page after it, fetched as it
goes:

```go
page, err := client.Portfolio.List(ctx, &cru.PortfolioListParams{Limit: cru.Int(100)})
if err != nil {
	return err
}
for wine, err := range page.All(ctx) {
	if err != nil {
		return err // a page that failed to load; the iteration ends here
	}
	fmt.Println(wine.LWIN18, wine.TotalQuantity)
}
```

Breaking out of the loop fetches no further page. A failed fetch is yielded as the zero item with
its error, and ends the iteration.

## Page by page

| Field or method | What it holds |
|---|---|
| `Items` | this page's items |
| `Limit`, `Offset` | `meta.pagination.limit` and `offset` |
| `Total` | `meta.pagination.total`, or nil when the list does not count its items |
| `HasMore` | `meta.pagination.has_more`; without it, a full page is taken to have more |
| `Data`, `Meta` | the envelope's data, with any keys besides the items, and meta |
| `Next(ctx)` | the page after this one, at `Offset + len(Items)`; nil and no error after the last |

```go
for page != nil {
	for _, bid := range page.Items {
		// ...
	}
	if page, err = page.Next(ctx); err != nil {
		return err
	}
}
```

The next pages are fetched with the first call's parameters and request options; only `offset`
changes. `WithResponseInto` reports the last page fetched.

## Lists that do not page

Some lists answer everything at once and return a slice: `client.Warehouses.List`,
`client.InbondWarehouses.List` and `client.InbondAddresses.List`.
