package cru_test

import (
	"context"
	"errors"
	"fmt"
	"log"

	cru "github.com/cruworldwine/cru-api-go"
)

func ExampleNewClient() {
	// A partner credential; its writes run at once.
	client, err := cru.NewClient(cru.WithClientKey("ck_live_...", "cs_live_..."), cru.WithMarket("uk"))
	if err != nil {
		log.Fatal(err)
	}
	ping, err := client.Ping(context.Background())
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(ping.Client.Market, ping.Client.Currency)
}

func ExampleBidsService_Place() {
	client, _ := cru.NewClient() // CRU_CLIENT_KEY and CRU_CLIENT_SECRET
	w, err := client.Bids.Place(context.Background(), &cru.BidsPlaceParams{
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

func ExampleConfirmationsService_Confirm() {
	// A connected app: its writes are staged until the customer agrees.
	client, _ := cru.NewClient(cru.WithAccessToken("..."),
		cru.WithOAuthRefresh("...", "app_id", "", func(t cru.Token) {
			// Store t.AccessToken and t.RefreshToken: refresh tokens rotate.
		}))
	ctx := context.Background()
	w, err := client.Offers.Update(ctx, "OFFER-CODE", &cru.OffersUpdateParams{Price: cru.Float(1350)})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(w.Confirmation.Preview.Summary) // show it, and confirm only on a yes
	done, err := client.Confirmations.Confirm(ctx, w.Confirmation.Code)
	if err != nil {
		log.Fatal(err)
	}
	if res, ok := done.Result.Value.(*cru.OfferResult); ok && res.Offer != nil {
		fmt.Println("listed", res.Offer.LWIN18)
	}
}

func ExamplePage_All() {
	client, _ := cru.NewClient()
	ctx := context.Background()
	page, err := client.Portfolio.List(ctx, &cru.PortfolioListParams{StockType: []string{cru.StockTypeCru}, Limit: cru.Int(100)})
	if err != nil {
		log.Fatal(err)
	}
	for wine, err := range page.All(ctx) {
		if err != nil {
			log.Fatal(err)
		}
		fmt.Println(wine.LWIN18, wine.TotalQuantity)
	}
}
