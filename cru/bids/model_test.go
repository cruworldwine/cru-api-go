package bids

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/cruworldwine/cru-api-go/cru/core"
	"github.com/cruworldwine/cru-api-go/internal/testkit"
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
	bid := decode[OwnBid](t, strings.Replace(testkit.BidJSON(testkit.BidCode), `"status":"live"`,
		`"status":"paused","colour":"red","new_object":{"a":[1,2]}`, 1))
	if bid.Status != "paused" || bid.Code != testkit.BidCode {
		t.Errorf("an unknown enum value: %+v", bid)
	}
	var colour string
	if found, err := core.Field(bid, "colour", &colour); !found || err != nil || colour != "red" {
		t.Errorf("Field(colour) = %v, %v, %q", found, err, colour)
	}
	if found, _ := core.Field(bid, "absent", &colour); found {
		t.Error("Field found an absent field")
	}
	if !strings.Contains(string(bid.Raw()), `"new_object":{"a":[1,2]}`) {
		t.Errorf("Raw lost the unknown field: %s", bid.Raw())
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
	bid := decode[OwnBid](t, `{"code":42,"renewable":"yes","order_numbers":"none","wine_name":["a"],"lwin18":"`+testkit.LWIN+`"}`)
	if bid.Code != "" || bid.Renewable || bid.OrderNumbers != nil || bid.WineName != nil || bid.LWIN18 != testkit.LWIN {
		t.Errorf("%+v", bid)
	}
	result := decode[Result](t, `{"action":"bid.place","bid":[]}`)
	if result.Action != core.ActionBidPlace || result.Bid.Code != "" || string(result.Bid.Raw()) != "[]" {
		t.Errorf("a bid that is not an object: %+v", result)
	}
}

func TestNullAndMissing(t *testing.T) {
	var before OwnBid
	before.Code = "kept"
	if err := json.Unmarshal([]byte("null"), &before); err != nil || before.Code != "kept" {
		t.Error("decoding null must leave the model alone")
	}
	var nilBid *OwnBid
	if nilBid.Raw() != nil {
		t.Error("Raw of a nil model")
	}
	if found, err := core.Field(nilBid, "code", new(string)); found || err != nil {
		t.Error("Field of a nil model")
	}
}

func TestMarshalUsesAPINames(t *testing.T) {
	bid := decode[OwnBid](t, testkit.BidJSON(testkit.BidCode))
	b, err := json.Marshal(bid)
	if err != nil || !strings.Contains(string(b), `"lwin18":"`+testkit.LWIN+`"`) || strings.Contains(string(b), "raw") {
		t.Errorf("%s, %v", b, err)
	}
}

func TestWholeResponseSurvivesABadField(t *testing.T) {
	s := testkit.NewServer(t, testkit.Always(200, testkit.OKPaged(`{"bids":[`+testkit.BidJSON("BID-1")+`,{"code":"BID-2","quantity":"lots","price":{}}],"new_key":1}`,
		`{"limit":"50","offset":0.0,"total":2}`)))
	page, err := newService(testkit.Partner(t, s)).List(context.Background(), nil)
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
	problems := testkit.Lossless("bid", bid, json.RawMessage(raw))
	joined := strings.Join(problems, " ")
	if !strings.Contains(joined, "bid.quantity: value lost") || !strings.Contains(joined, "bid.lwin18: required but absent") {
		t.Errorf("the replay test's check missed a loss: %v", problems)
	}
	if got := testkit.Lossless("bid", decode[OwnBid](t, testkit.BidJSON("y")), json.RawMessage(testkit.BidJSON("y"))); len(got) != 0 {
		t.Errorf("a faithful decode was flagged: %v", got)
	}
}
