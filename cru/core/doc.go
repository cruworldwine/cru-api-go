// Package core holds what the Cru Trading API's resource packages share, and programs may name:
//
//   - Page, one page of a list, and Write, what a write did, with its StagedConfirmation;
//   - APIError and ConnectionError, and the kinds errors.Is matches them against: ErrNotFound,
//     ErrPriceChanged and the other Err values;
//   - RequestOption and the options that change one call, such as WithRequestTimeout and
//     WithResponseInto, which fills a ResponseMeta;
//   - Model and Field, the envelope's models (Meta, Pagination, ErrorEntry), and the known values
//     more than one resource uses, such as CurrencyGBP and ActionBidPlace.
//
// Programs start with package cru, which names the parts of core they use most again, so code
// reads cru.APIError, cru.ErrNotFound, cru.WithRequestTimeout and cru.CurrencyGBP: each is the same
// type or value as core's. Page and Write are generic and are named here only, as in
// *core.Page[bids.OwnBid].
package core
