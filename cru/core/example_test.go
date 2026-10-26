package core_test

import (
	"context"
	"fmt"
	"log"

	"github.com/cruworldwine/cru-api-go/cru"
	"github.com/cruworldwine/cru-api-go/cru/portfolio"
)

func ExamplePage_All() {
	client, _ := cru.NewClient()
	ctx := context.Background()
	page, err := client.Portfolio.List(ctx, &portfolio.ListParams{StockType: []string{cru.StockTypeCru}, Limit: cru.Int(100)})
	if err != nil {
		log.Fatal(err)
	}
	for wine, err := range page.All(ctx) { // fetches the next pages as it goes
		if err != nil {
			log.Fatal(err)
		}
		fmt.Println(wine.LWIN18, wine.TotalQuantity)
	}
}
