# Writes and confirmations

A write returns a `*core.Write[T]`, from package `github.com/cruworldwine/cru-api-go/cru/core`,
with `T` the write's result in the resource's package: `client.Bids.Place` returns a
`*core.Write[bids.Result]`. What it holds depends on the credential.

| Field | Partner credential | Connected app |
|---|---|---|
| `Staged` | false | true: the API answered 202 |
| `Result` | what the write did, such as a `*bids.Result` | nil |
| `Confirmation` | nil | the staged instruction, with its code and preview |
| `Replayed` | true when this is the stored answer to an earlier request with the same reference | false |
| `StatusCode`, `RequestID` | the answer's | the answer's |

## A partner credential's writes run at once

```go
w, err := client.Bids.Place(ctx, &bids.PlaceParams{
	LWIN18:    "100000120151200750",
	Quantity:  2,
	Price:     1200.50,
	ClientRef: cru.String("order-17"),
})
if err != nil {
	return err
}
fmt.Println(w.Result.Bid.Code, w.Result.Bid.Status)
```

### References and retries

Every partner write carries a `reference`, its idempotency key, kept by Cru for good. Leave
`Reference` nil and the SDK sends `sdk_` and 32 hex characters, generated once per call and the
same on every retry, so a retry can never act twice. Give your own when you retry across
processes, such as from a job queue:

```go
w, err := client.Offers.Update(ctx, code, &offers.UpdateParams{
	Reference: cru.String("reprice-" + jobID),
	Price:     cru.Float(1350),
})
if w != nil && w.Replayed {
	// an earlier request with this reference already did it; this is its answer
}
```

A request still running with the same reference answers 409 `REQUEST_IN_PROGRESS`, which the SDK
waits out and retries.

## A connected app's writes are staged

Nothing happens until the customer agrees. Show them the preview, and confirm only on their yes:

```go
w, err := client.Offers.Update(ctx, code, &offers.UpdateParams{Price: cru.Float(1350)})
if err != nil {
	return err
}
fmt.Println(w.Confirmation.Preview.Summary) // "List 6 bottles at 1,350.00 GBP each ..."

done, err := client.Confirmations.Confirm(ctx, w.Confirmation.Code)
if err != nil {
	return err
}
switch r := done.Result.Value.(type) {
case *offers.Result:
	fmt.Println("listed", r.Offer.LWIN18)
case *confirmations.InstructionResult: // an action this SDK does not know; see done.Result.Raw()
}
```

`client.Confirmations.Get` reads a confirmation's state and `client.Confirmations.Cancel`
withdraws it. A code that expired or was cancelled answers 410 (`ErrGone`). A connected app's
writes are never retried: they carry no reference.

`Confirm` returns a `*core.Write[confirmations.ActionResult]`. `ActionResult.Value` is chosen by
the action that ran: a `*bids.Result` for `bid.place`, an `*offers.Result` for `offer.list` and so
on, as `confirmations.Service.Confirm`'s doc comment lists.

## Prices that move

Buying an offer or selling into a bid quotes a price. When it moved since your preview, the write
fails with `PRICE_CHANGED` (`errors.Is(err, cru.ErrPriceChanged)`): preview again and ask again.
A trade Cru cannot carry out fails with `TRADE_REFUSED` (`ErrTradeRefused`), whose message can be
shown to the customer. See [errors](errors.md).
