package warehouses_test

import (
	"context"
	"fmt"
	"log"

	"github.com/cruworldwine/cru-api-go/cru"
	"github.com/cruworldwine/cru-api-go/cru/warehouses"
)

func ExampleService_List() {
	client, _ := cru.NewClient() // CRU_CLIENT_KEY and CRU_CLIENT_SECRET
	list, err := client.Warehouses.List(context.Background())
	if err != nil {
		log.Fatal(err)
	}
	// The Cru warehouses that can sell wine you hold in bond elsewhere.
	var sellers []warehouses.Warehouse
	for _, w := range list {
		if w.SellsYourInbondWine {
			sellers = append(sellers, w)
		}
	}
	for _, w := range sellers {
		fmt.Println(w.Code, w.Name, cru.Value(w.Address.Country))
	}
}
