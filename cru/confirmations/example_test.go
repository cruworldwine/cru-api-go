package confirmations_test

import (
	"context"
	"fmt"
	"log"

	"github.com/cruworldwine/cru-api-go/cru"
	"github.com/cruworldwine/cru-api-go/cru/confirmations"
	"github.com/cruworldwine/cru-api-go/cru/offers"
)

func ExampleService_Confirm() {
	// A connected app: its writes are staged until the customer agrees.
	client, _ := cru.NewClient(cru.WithAccessToken("..."),
		cru.WithOAuthRefresh("...", "app_id", "", func(t cru.Token) {
			// Store t.AccessToken and t.RefreshToken: refresh tokens rotate.
		}))
	ctx := context.Background()
	w, err := client.Offers.Update(ctx, "OFFER-CODE", &offers.UpdateParams{Price: cru.Float(1350)})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(w.Confirmation.Preview.Summary) // show it, and confirm only on a yes
	done, err := client.Confirmations.Confirm(ctx, w.Confirmation.Code)
	if err != nil {
		log.Fatal(err)
	}
	switch r := done.Result.Value.(type) {
	case *offers.Result:
		fmt.Println("listed", r.Offer.LWIN18)
	case *confirmations.InstructionResult: // an action this SDK does not know
		fmt.Println(r.Action, string(done.Result.Raw()))
	}
}
