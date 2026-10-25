package cru_test

import (
	"context"
	"errors"
	"fmt"
	"log"
	"log/slog"
	"os"
	"time"

	"github.com/cruworldwine/cru-api-go/cru"
	"github.com/cruworldwine/cru-api-go/cru/bids"
)

func ExampleNewClient() {
	// A partner credential; its writes run at once.
	client, err := cru.NewClient(cru.WithClientKey("cru_live_...", "..."), cru.WithMarket("uk"))
	if err != nil {
		log.Fatal(err)
	}
	ping, err := client.Ping(context.Background())
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(ping.Client.Market, ping.Client.Currency)
}

func ExampleWithLogger() {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug}))
	client, err := cru.NewClient(
		cru.WithLogger(logger), // attempts at Debug, retries and refreshes at Info; never secrets
		cru.WithAppInfo("cellar-sync", "2.1.0", "https://example.com/cellar-sync"),
	)
	if err != nil {
		log.Fatal(err)
	}
	_, _ = client.Ping(context.Background())
}

func ExampleWithResponseInto() {
	client, _ := cru.NewClient()
	var meta cru.ResponseMeta
	bid, err := client.Bids.Get(context.Background(), "BID-CODE",
		cru.WithResponseInto(&meta),
		cru.WithRequestTimeout(5*time.Second),
		cru.WithRequestHeader("traceparent", "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01"),
	)
	if err != nil {
		log.Fatal(err)
	}
	// Quote the request id to Cru support.
	fmt.Println(bid.Status, meta.StatusCode, meta.RequestID, meta.Attempts)
}

func ExampleWithExtraQuery() {
	client, _ := cru.NewClient()
	// Send a parameter, or a body field, this version of the SDK does not know yet.
	page, err := client.Bids.List(context.Background(), nil, cru.WithExtraQuery("new_filter", "value"))
	if err != nil {
		log.Fatal(err)
	}
	_, err = client.Bids.Update(context.Background(), "BID-CODE", &bids.UpdateParams{Price: cru.Float(1300)},
		cru.WithExtraBodyField("new_field", true))
	fmt.Println(len(page.Items), err)
}

func ExampleAPIError() {
	client, _ := cru.NewClient()
	_, err := client.Bids.Get(context.Background(), "BID-CODE")
	var apiErr *cru.APIError
	switch {
	case errors.Is(err, cru.ErrNotFound):
		fmt.Println("no such bid")
	case errors.As(err, &apiErr):
		// Branch on StatusCode and Code; quote RequestID to Cru support.
		fmt.Println(apiErr.StatusCode, apiErr.Code, apiErr.Message, apiErr.RequestID)
	case err != nil:
		log.Fatal(err) // a *cru.ConnectionError, or the context's error
	}
}
