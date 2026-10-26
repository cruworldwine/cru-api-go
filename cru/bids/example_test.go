package bids_test

import (
	"context"
	"errors"
	"fmt"
	"log"

	"github.com/cruworldwine/cru-api-go/cru"
	"github.com/cruworldwine/cru-api-go/cru/bids"
)

func ExampleService_List() {
	client, _ := cru.NewClient() // CRU_CLIENT_KEY and CRU_CLIENT_SECRET
	ctx := context.Background()
	page, err := client.Bids.List(ctx, &bids.ListParams{Status: []string{bids.StatusLive, bids.StatusMatched}, Limit: cru.Int(50)})
	if err != nil {
		log.Fatal(err)
	}
	for bid, err := range page.All(ctx) {
		if err != nil {
			log.Fatal(err)
		}
		fmt.Println(bid.Code, bid.Status, bid.QuantityOpen, bid.Price, bid.Currency)
	}
}

func ExampleService_Place() {
	client, _ := cru.NewClient() // CRU_CLIENT_KEY and CRU_CLIENT_SECRET
	w, err := client.Bids.Place(context.Background(), &bids.PlaceParams{
		LWIN18:    "100000120151200750",
		Quantity:  2,
		Price:     1200.50,
		LocalOnly: cru.Bool(true),
	})
	switch {
	case errors.Is(err, cru.ErrTradeRefused):
		var apiErr *cru.APIError
		errors.As(err, &apiErr)
		fmt.Println("refused:", apiErr.Message)
	case err != nil:
		log.Fatal(err)
	case w.Staged:
		fmt.Println("confirm with", w.Confirmation.Code)
	default:
		fmt.Println("placed", w.Result.Bid.Code)
	}
}
