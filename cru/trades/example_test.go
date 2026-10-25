package trades_test

import (
	"context"
	"fmt"
	"log"

	"github.com/cruworldwine/cru-api-go/cru"
	"github.com/cruworldwine/cru-api-go/cru/trades"
)

func ExampleService_ListSales() {
	client, _ := cru.NewClient() // CRU_CLIENT_KEY and CRU_CLIENT_SECRET
	ctx := context.Background()
	page, err := client.Trades.ListSales(ctx, &trades.ListSalesParams{Status: []string{trades.SaleStatusTransferNeeded}})
	if err != nil {
		log.Fatal(err)
	}
	for sale, err := range page.All(ctx) {
		if err != nil {
			log.Fatal(err)
		}
		fmt.Println(sale.LWIN18, sale.Quantity, cru.Value(sale.NetProceeds), sale.Currency, cru.Value(sale.DeliverBy))
	}
}
