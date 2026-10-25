package offers_test

import (
	"context"
	"fmt"
	"log"

	"github.com/cruworldwine/cru-api-go/cru"
	"github.com/cruworldwine/cru-api-go/cru/offers"
)

func ExampleService_Update() {
	client, _ := cru.NewClient() // CRU_CLIENT_KEY and CRU_CLIENT_SECRET
	w, err := client.Offers.Update(context.Background(), "OFFER-CODE", &offers.UpdateParams{
		Reference: cru.String("reprice-job-17"), // your own idempotency key, kept across retries
		Price:     cru.Float(1350),
	})
	if err != nil {
		log.Fatal(err)
	}
	if w.Replayed {
		fmt.Println("an earlier request with this reference already did it")
	}
	if w.Result != nil && w.Result.Offer != nil {
		fmt.Println(w.Result.Offer.LWIN18, w.Result.Offer.TotalQuantity)
	}
}
