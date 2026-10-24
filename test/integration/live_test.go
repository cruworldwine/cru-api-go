package integration

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/cruworldwine/cru-api-go/cru"
)

// TestLive is a smoke test against the real API, with the credentials in the environment. Every
// call it makes only reads.
func TestLive(t *testing.T) {
	if os.Getenv("CRU_LIVE") != "1" {
		t.Skip("set CRU_LIVE=1, with credentials in the environment, to call the real API")
	}
	client, err := cru.NewClient(cru.WithAppInfo("cru-api-go-integration", cru.Version, ""))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	var meta cru.ResponseMeta
	ping, err := client.Ping(ctx, cru.WithResponseInto(&meta))
	if err != nil {
		t.Fatal(err)
	}
	if !ping.Pong || ping.Client.Market == "" || meta.RequestID == "" {
		t.Errorf("ping %+v, request id %q", ping, meta.RequestID)
	}
	t.Logf("%s on %s (%s), request %s", client, ping.Client.Market, ping.Client.Currency, meta.RequestID)

	warehouses, err := client.Warehouses.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("%d warehouses", len(warehouses))

	page, err := client.Market.List(ctx, &cru.MarketListParams{Limit: cru.Int(1)})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) > 1 {
		t.Errorf("asked for 1 wine, got %d", len(page.Items))
	}
}
