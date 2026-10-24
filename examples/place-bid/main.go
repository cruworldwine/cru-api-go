// Places a bid. With a partner credential it is placed at once; as a connected app it is staged,
// and confirmed here only once you answer yes to its summary.
//
//	CRU_CLIENT_KEY=cru_live_... CRU_CLIENT_SECRET=... go run ./examples/place-bid 101174520151200750 6 1450
package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"

	cru "github.com/cruworldwine/cru-api-go"
)

func main() {
	if len(os.Args) != 4 {
		log.Fatal("usage: place-bid <lwin18> <quantity> <price>")
	}
	quantity, err := strconv.ParseInt(os.Args[2], 10, 64)
	if err != nil {
		log.Fatal(err)
	}
	price, err := strconv.ParseFloat(os.Args[3], 64)
	if err != nil {
		log.Fatal(err)
	}
	ctx := context.Background()
	client, err := cru.NewClient()
	if err != nil {
		log.Fatal(err)
	}

	w, err := client.Bids.Place(ctx, &cru.BidsPlaceParams{LWIN18: os.Args[1], Quantity: quantity, Price: price})
	var apiErr *cru.APIError
	if errors.As(err, &apiErr) {
		log.Fatalf("refused: %s %s (request %s)", apiErr.Code, apiErr.Message, apiErr.RequestID)
	} else if err != nil {
		log.Fatal(err)
	}

	if w.Staged {
		fmt.Printf("%s Confirm? [y/N] ", w.Confirmation.Preview.Summary)
		answer, _ := bufio.NewReader(os.Stdin).ReadString('\n')
		if strings.TrimSpace(strings.ToLower(answer)) != "y" {
			fmt.Println("Not placed.")
			return
		}
		done, err := client.Confirmations.Confirm(ctx, w.Confirmation.Code)
		if err != nil {
			log.Fatal(err)
		}
		result, ok := done.Result.Value.(*cru.BidResult)
		if !ok {
			log.Fatalf("unexpected result %T", done.Result.Value)
		}
		w = &cru.Write[cru.BidResult]{Result: result}
	}
	bid := w.Result.Bid
	fmt.Printf("Bid %s: %d x %.2f %s, %s, expires %s\n", bid.Code, bid.Quantity, bid.Price, bid.Currency, bid.Status, cru.Value(bid.ExpiresOn))
}
