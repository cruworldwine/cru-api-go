package cru

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func decode[T any](t *testing.T, s string) *T {
	t.Helper()
	v := new(T)
	if err := json.Unmarshal([]byte(s), v); err != nil {
		t.Fatalf("decoding failed: %v", err)
	}
	return v
}

func TestUnknownFieldsAndValues(t *testing.T) {
	bid := decode[OwnBid](t, strings.Replace(bidJSON(testBidCode), `"status":"live"`,
		`"status":"paused","colour":"red","new_object":{"a":[1,2]}`, 1))
	if bid.Status != "paused" || bid.Code != testBidCode {
		t.Errorf("an unknown enum value: %+v", bid)
	}
	var colour string
	if found, err := Field(bid, "colour", &colour); !found || err != nil || colour != "red" {
		t.Errorf("Field(colour) = %v, %v, %q", found, err, colour)
	}
	if found, _ := Field(bid, "absent", &colour); found {
		t.Error("Field found an absent field")
	}
	if !strings.Contains(string(bid.Raw()), `"new_object":{"a":[1,2]}`) {
		t.Errorf("Raw lost the unknown field: %s", bid.Raw())
	}
}

func TestNestedRaw(t *testing.T) {
	wine := decode[PortfolioWine](t, `{"lwin18":"`+testLWIN+`","currency":"GBP","total_quantity":3,"portfolio":[
		{"portfolio_id":"P-1","stock_type":"cru","quantity":3,"pricing":{"cost":100,"listing_price":null,"tax":7},
		 "warehouse":{"location_type":"vault","current_location":null},"future":"yes"}]}`)
	line := wine.Portfolio[0]
	if line.PortfolioID != "P-1" || line.Quantity != 3 || line.StockType != StockTypeCru {
		t.Fatalf("line %+v", line)
	}
	if *line.Pricing.Cost != 100 || line.Pricing.ListingPrice != nil || line.Warehouse.LocationType == nil || *line.Warehouse.LocationType != "vault" {
		t.Errorf("pricing %+v, warehouse %+v", line.Pricing, line.Warehouse)
	}
	if !strings.Contains(string(line.Raw()), `"future":"yes"`) || !strings.Contains(string(line.Pricing.Raw()), `"tax":7`) {
		t.Error("a nested model lost its raw JSON")
	}
}

func TestNumberTolerance(t *testing.T) {
	bid := decode[OwnBid](t, `{"code":"x","quantity":1000.0,"quantity_matched":"7","quantity_open":2.5,
		"vintage":1e3,"price":"1200.50","status":"live"}`)
	if bid.Quantity != 1000 || bid.QuantityMatched != 7 || bid.Vintage == nil || *bid.Vintage != 1000 || bid.Price != 1200.5 {
		t.Errorf("numbers: %+v", bid)
	}
	if bid.QuantityOpen != 0 || bid.Status != "live" || bid.Code != "x" {
		t.Errorf("a non-integral integer must be left unset and the rest decoded: %+v", bid)
	}
	bid = decode[OwnBid](t, `{"vintage":12.5}`)
	if bid.Vintage != nil {
		t.Error("a non-integral optional integer must be nil")
	}
}

func TestWrongTypesLeaveZero(t *testing.T) {
	bid := decode[OwnBid](t, `{"code":42,"renewable":"yes","order_numbers":"none","wine_name":["a"],"lwin18":"`+testLWIN+`"}`)
	if bid.Code != "" || bid.Renewable || bid.OrderNumbers != nil || bid.WineName != nil || bid.LWIN18 != testLWIN {
		t.Errorf("%+v", bid)
	}
	// PHP encodes an empty object as [].
	line := decode[PortfolioLine](t, `{"portfolio_id":"P-1","pricing":[],"warehouse":[],"photo_urls":{}}`)
	if line.PortfolioID != "P-1" || line.Pricing.Cost != nil || line.PhotoURLs != nil {
		t.Errorf("%+v", line)
	}
	if string(line.Pricing.Raw()) != "[]" {
		t.Errorf("raw of a non-object: %s", line.Pricing.Raw())
	}
	conf := decode[Confirmation](t, `{"action":"bid.place","result":[],"error":null}`)
	if conf.Result != nil || conf.Error != nil || conf.Action != ActionBidPlace {
		t.Errorf("%+v", conf)
	}
}

func TestNullAndMissing(t *testing.T) {
	wine := decode[MarketWine](t, `{"lwin18":"`+testLWIN+`","wine_name":null,"market_price":null,"bids":[]}`)
	if wine.WineName != nil || wine.MarketPrice != nil || wine.LastTrade != nil || wine.Offers != nil || wine.Bids == nil || len(wine.Bids) != 0 {
		t.Errorf("%+v", wine)
	}
	var price any = "unset"
	if found, _ := Field(wine, "market_price", &price); !found || price != nil {
		t.Error("an explicit null is not in Raw")
	}
	if found, _ := Field(wine, "last_trade", &price); found {
		t.Error("a missing field was found in Raw")
	}
	wine = decode[MarketWine](t, `{"market_price":0,"last_trade":{"price":10,"traded_at":"2026-01-01T00:00:00Z"}}`)
	if wine.MarketPrice == nil || *wine.MarketPrice != 0 || wine.LastTrade.Price != 10 {
		t.Errorf("a zero is not a null: %+v", wine)
	}

	var before OwnBid
	before.Code = "kept"
	if err := json.Unmarshal([]byte("null"), &before); err != nil || before.Code != "kept" {
		t.Error("decoding null must leave the model alone")
	}
	var nilBid *OwnBid
	if nilBid.Raw() != nil {
		t.Error("Raw of a nil model")
	}
	if found, err := Field(nilBid, "code", new(string)); found || err != nil {
		t.Error("Field of a nil model")
	}
}

func TestMarshalUsesAPINames(t *testing.T) {
	bid := decode[OwnBid](t, bidJSON(testBidCode))
	b, err := json.Marshal(bid)
	if err != nil || !strings.Contains(string(b), `"lwin18":"`+testLWIN+`"`) || strings.Contains(string(b), "raw") {
		t.Errorf("%s, %v", b, err)
	}
}

func TestWholeResponseSurvivesABadField(t *testing.T) {
	s := newServer(t, always(200, okPaged(`{"bids":[`+bidJSON("BID-1")+`,{"code":"BID-2","quantity":"lots","price":{}}],"new_key":1}`,
		`{"limit":"50","offset":0.0,"total":2}`)))
	c, _ := partner(t, s)
	page, err := c.Bids.List(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 2 || page.Items[1].Code != "BID-2" || page.Items[1].Quantity != 0 || page.Limit != 50 || *page.Total != 2 {
		t.Errorf("%+v", page)
	}
}

func TestLosslessCheckFlagsLoss(t *testing.T) {
	raw := `{"code":"x","quantity":2.5,"price":10,"status":"live"}`
	bid := decode[OwnBid](t, raw)
	problems := lossless("bid", bid, json.RawMessage(raw))
	joined := strings.Join(problems, " ")
	if !strings.Contains(joined, "bid.quantity: value lost") || !strings.Contains(joined, "bid.lwin18: required but absent") {
		t.Errorf("the replay test's check missed a loss: %v", problems)
	}
	if got := lossless("bid", decode[OwnBid](t, bidJSON("y")), json.RawMessage(bidJSON("y"))); len(got) != 0 {
		t.Errorf("a faithful decode was flagged: %v", got)
	}
}
