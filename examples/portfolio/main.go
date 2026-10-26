// Prints every line of your portfolio, with what each listed line would receive when it sells.
//
//	CRU_CLIENT_KEY=cru_live_... CRU_CLIENT_SECRET=... go run ./examples/portfolio
package main

import (
	"context"
	"fmt"
	"log"

	"github.com/cruworldwine/cru-api-go/cru"
	"github.com/cruworldwine/cru-api-go/cru/portfolio"
)

func main() {
	ctx := context.Background()
	client, err := cru.NewClient()
	if err != nil {
		log.Fatal(err)
	}

	page, err := client.Portfolio.List(ctx, &portfolio.ListParams{Limit: cru.Int(100)})
	if err != nil {
		log.Fatal(err)
	}
	for wine, err := range page.All(ctx) {
		if err != nil {
			log.Fatal(err)
		}
		fmt.Printf("%s (%s): %d units\n", cru.Value(wine.WineName), wine.LWIN18, wine.TotalQuantity)
		for _, line := range wine.Portfolio {
			where := cru.Value(line.Warehouse.CurrentLocation)
			if !line.IsListed {
				fmt.Printf("  %s  %d x %s, %s, at %s\n", line.PortfolioID, line.Quantity, line.StockType, line.StockStatus, where)
				continue
			}
			fmt.Printf("  %s  %d x listed at %.2f %s, you receive %.2f, at %s\n", line.PortfolioID, line.Quantity,
				cru.Value(line.Pricing.ListingPrice), wine.Currency, cru.Value(line.Pricing.NetProceeds), where)
		}
	}
}
