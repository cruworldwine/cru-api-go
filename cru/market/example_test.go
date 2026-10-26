package market_test

import (
	"context"
	"errors"
	"fmt"
	"log"

	"github.com/cruworldwine/cru-api-go/cru"
	"github.com/cruworldwine/cru-api-go/cru/market"
)

func ExampleService_SellToBid() {
	client, _ := cru.NewClient() // CRU_CLIENT_KEY and CRU_CLIENT_SECRET
	w, err := client.Market.SellToBid(context.Background(), "BID-CODE", &market.SellToBidParams{
		StockType:   cru.StockTypeCru,
		PortfolioID: cru.String("PORTFOLIO-..."),
		Quantity:    2,
		MinPrice:    cru.Float(1150), // refuse a lower price, rather than sell at it
	})
	switch {
	case errors.Is(err, cru.ErrPriceChanged):
		fmt.Println("the bid moved; look again")
	case err != nil:
		log.Fatal(err)
	case w.Staged:
		fmt.Println("confirm with", w.Confirmation.Code)
	default:
		fmt.Println("sold", w.Result.Sold, "at", w.Result.Price, w.Result.Currency)
	}
}
