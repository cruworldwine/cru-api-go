package portfolio_test

import (
	"context"
	"fmt"
	"log"

	"github.com/cruworldwine/cru-api-go/cru"
	"github.com/cruworldwine/cru-api-go/cru/portfolio"
)

func ExampleService_List() {
	client, _ := cru.NewClient() // CRU_CLIENT_KEY and CRU_CLIENT_SECRET
	ctx := context.Background()
	page, err := client.Portfolio.List(ctx, &portfolio.ListParams{IsListed: cru.Bool(true)})
	if err != nil {
		log.Fatal(err)
	}
	for wine, err := range page.All(ctx) {
		if err != nil {
			log.Fatal(err)
		}
		for _, line := range wine.Portfolio {
			fmt.Println(cru.Value(wine.WineName), line.Quantity, cru.Value(line.Pricing.ListingPrice), wine.Currency)
		}
	}
}
