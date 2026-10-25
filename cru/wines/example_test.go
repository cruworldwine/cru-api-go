package wines_test

import (
	"context"
	"fmt"
	"log"

	"github.com/cruworldwine/cru-api-go/cru"
	"github.com/cruworldwine/cru-api-go/cru/wines"
)

func ExampleService_Search() {
	client, _ := cru.NewClient() // CRU_CLIENT_KEY and CRU_CLIENT_SECRET
	page, err := client.Wines.Search(context.Background(), &wines.SearchParams{Q: "lafite", Vintage: cru.Int(2016)})
	if err != nil {
		log.Fatal(err)
	}
	for _, wine := range page.Items {
		fmt.Println(wine.LWIN18, cru.Value(wine.WineName), cru.Value(wine.PackSize), len(wine.Bids), len(wine.Offers))
	}
}
