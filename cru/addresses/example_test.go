package addresses_test

import (
	"context"
	"fmt"
	"log"

	"github.com/cruworldwine/cru-api-go/cru"
	"github.com/cruworldwine/cru-api-go/cru/addresses"
)

func ExampleInbondService_Add() {
	client, _ := cru.NewClient() // CRU_CLIENT_KEY and CRU_CLIENT_SECRET
	ctx := context.Background()
	// The warehouse outside Cru your wine is held in, by its inbond_address_id.
	inbond, err := client.InbondWarehouses.List(ctx)
	if err != nil || len(inbond) == 0 {
		log.Fatal("no inbond warehouse: ", err)
	}
	w, err := client.InbondAddresses.Add(ctx, &addresses.InbondAddParams{
		InbondAddressID: inbond[0].InbondAddressID,
		AccountNumber:   "ACC-1234",
		Nickname:        cru.String("Main bond"),
	})
	if err != nil {
		log.Fatal(err)
	}
	if w.Result != nil {
		fmt.Println(w.Result.InbondAddress.InbondAddressID, w.Result.InbondAddress.Pending)
	}
}
