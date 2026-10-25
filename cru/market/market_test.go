package market

import (
	"context"
	"encoding/json"
	"net/url"
	"sort"
	"testing"

	"github.com/cruworldwine/cru-api-go/cru/core"
	"github.com/cruworldwine/cru-api-go/internal/testkit"
)

func keys(v url.Values) []string {
	var out []string
	for k := range v {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func integer(v int64) *int64 { return &v }
func boolean(v bool) *bool   { return &v }
func text(v string) *string  { return &v }

func TestQueryEncoding(t *testing.T) {
	s := testkit.NewServer(t, testkit.Always(200, testkit.OKPaged(`{"markets":[]}`, `{"limit":10,"offset":0}`)))
	m := newService(testkit.Partner(t, s))
	_, err := m.ListBids(context.Background(), &ListBidsParams{
		LWIN:                 []string{testkit.LWIN, "100000220161200750"},
		Limit:                integer(10),
		MinPrice:             integer(0),
		LandedStockOnly:      boolean(false),
		IncludeMarketPrice:   boolean(true),
		DeliverableWarehouse: []string{"UK_DRAK", "HK_KERRY"},
		WineName:             text("Chateau Exemple & Fils"),
	})
	if err != nil {
		t.Fatal(err)
	}
	q := s.Requests()[0].Query
	want := map[string]string{
		"lwin": testkit.LWIN + ",100000220161200750", "limit": "10", "min_price": "0", "landed_stock_only": "false",
		"include_market_price": "true", "deliverable_warehouse": "UK_DRAK,HK_KERRY", "wine_name": "Chateau Exemple & Fils",
	}
	if got := keys(q); len(got) != len(want) {
		t.Errorf("sent %v; unset values must be dropped", got)
	}
	for k, v := range want {
		if q.Get(k) != v {
			t.Errorf("%s = %q, want %q", k, q.Get(k), v)
		}
	}

	if _, err := m.ListBids(context.Background(), nil); err != nil || len(s.Requests()[1].Query) != 0 {
		t.Errorf("nil params sent %v, %v", s.Requests()[1].Query, err)
	}
}

func TestNullAndMissing(t *testing.T) {
	var wine Wine
	if err := json.Unmarshal([]byte(`{"lwin18":"`+testkit.LWIN+`","wine_name":null,"market_price":null,"bids":[]}`), &wine); err != nil {
		t.Fatal(err)
	}
	if wine.WineName != nil || wine.MarketPrice != nil || wine.LastTrade != nil || wine.Offers != nil || wine.Bids == nil || len(wine.Bids) != 0 {
		t.Errorf("%+v", wine)
	}
	var price any = "unset"
	if found, _ := core.Field(&wine, "market_price", &price); !found || price != nil {
		t.Error("an explicit null is not in Raw")
	}
	if found, _ := core.Field(&wine, "last_trade", &price); found {
		t.Error("a missing field was found in Raw")
	}
	wine = Wine{}
	if err := json.Unmarshal([]byte(`{"market_price":0,"last_trade":{"price":10,"traded_at":"2026-01-01T00:00:00Z"}}`), &wine); err != nil {
		t.Fatal(err)
	}
	if wine.MarketPrice == nil || *wine.MarketPrice != 0 || wine.LastTrade.Price != 10 {
		t.Errorf("a zero is not a null: %+v", wine)
	}
}
