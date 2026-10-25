// Prints the bids and offers on one wine, and its last trade.
//
//	CRU_CLIENT_KEY=cru_live_... CRU_CLIENT_SECRET=... go run ./examples/market 101174520151200750
package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/cruworldwine/cru-api-go/cru"
	"github.com/cruworldwine/cru-api-go/cru/market"
)

func main() {
	if len(os.Args) != 2 {
		log.Fatal("usage: market <lwin18>")
	}
	ctx := context.Background()
	client, err := cru.NewClient()
	if err != nil {
		log.Fatal(err)
	}

	wine, err := client.Market.GetWine(ctx, os.Args[1], &market.GetWineParams{IncludeLastTrade: cru.Bool(true)})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("%s, prices in %s\n", cru.Value(wine.WineName), wine.Currency)
	for _, bid := range wine.Bids {
		fmt.Printf("  bid    %10.2f x %d  (%s, delivered to %v)\n", bid.Price, bid.Quantity,
			bid.Requirements.ConditionStatus, bid.Requirements.DeliverableWarehouses)
	}
	for _, offer := range wine.Offers {
		fmt.Printf("  offer  %10.2f x %d  (%s, %s, from %s)\n", offer.Price, offer.Quantity,
			cru.Value(offer.ConditionStatus), offer.Status, cru.Value(offer.WarehouseCode))
	}
	if wine.LastTrade != nil {
		fmt.Printf("  last traded at %.2f on %s\n", wine.LastTrade.Price, wine.LastTrade.TradedAt)
	}
}
