package cru

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"testing"
)

var referencePattern = regexp.MustCompile(`^sdk_[0-9a-f]{32}$`)

func keys(v url.Values) []string {
	var out []string
	for k := range v {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func TestQueryEncoding(t *testing.T) {
	s := newServer(t, always(200, okPaged(`{"markets":[]}`, `{"limit":10,"offset":0}`)))
	c, _ := partner(t, s)
	_, err := c.Market.ListBids(context.Background(), &MarketListBidsParams{
		LWIN:                 []string{testLWIN, "100000220161200750"},
		Limit:                Int(10),
		MinPrice:             Int(0),
		LandedStockOnly:      Bool(false),
		IncludeMarketPrice:   Bool(true),
		DeliverableWarehouse: []string{"UK_DRAK", "HK_KERRY"},
		WineName:             String("Chateau Exemple & Fils"),
	})
	if err != nil {
		t.Fatal(err)
	}
	q := s.requests()[0].Query
	want := map[string]string{
		"lwin": testLWIN + ",100000220161200750", "limit": "10", "min_price": "0", "landed_stock_only": "false",
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

	if _, err := c.Market.ListBids(context.Background(), nil); err != nil || len(s.requests()[1].Query) != 0 {
		t.Errorf("nil params sent %v, %v", s.requests()[1].Query, err)
	}
	if _, err := c.Bids.List(context.Background(), &BidsListParams{Status: []string{}}); err != nil || len(s.requests()[2].Query) != 0 {
		t.Errorf("an empty list was sent: %v, %v", s.requests()[2].Query, err)
	}
}

func TestRequiredQueryValue(t *testing.T) {
	s := newServer(t, always(200, okPaged(`{"wines":[]}`, `{"limit":20,"offset":0,"has_more":false}`)))
	c, _ := partner(t, s)
	if _, err := c.Wines.Search(context.Background(), &WinesSearchParams{Q: "exemple", Vintage: Int(2015)}); err != nil {
		t.Fatal(err)
	}
	q := s.requests()[0].Query
	if q.Get("q") != "exemple" || q.Get("vintage") != "2015" || len(q) != 2 {
		t.Errorf("query %v", q)
	}
}

func TestPathEncoding(t *testing.T) {
	s := newServer(t, func(w http.ResponseWriter, r *http.Request, n int) {
		if strings.HasPrefix(r.URL.Path, "/api/v1/warehouses/inbond/") {
			reply(w, 200, ok(`{"inbond_warehouse":{"inbond_address_id":42,"name":"Bond Test"}}`))
			return
		}
		reply(w, 200, ok(`{"bid":`+bidJSON("x")+`}`))
	})
	c, _ := partner(t, s)
	ctx := context.Background()
	if _, err := c.Bids.Get(ctx, "a/b c?d#e"); err != nil {
		t.Fatal(err)
	}
	if got := s.requests()[0].Path; got != "/bids/a%2Fb%20c%3Fd%23e" {
		t.Errorf("path %s", got)
	}
	if _, err := c.InbondWarehouses.Get(ctx, 42); err != nil || s.requests()[1].Path != "/warehouses/inbond/42" {
		t.Errorf("path %s, %v", s.requests()[1].Path, err)
	}

	_, err := c.Bids.Get(ctx, "")
	if !errors.Is(err, ErrMissingParameter) || !strings.Contains(err.Error(), "code") {
		t.Errorf("empty code: %v", err)
	}
	if err := c.Confirmations.Cancel(ctx, ""); !errors.Is(err, ErrMissingParameter) {
		t.Errorf("empty code: %v", err)
	}
	if len(s.requests()) != 2 {
		t.Error("a request was sent with an empty path parameter")
	}
}

func TestBodyOmitsNil(t *testing.T) {
	s := newServer(t, always(200, ok(bidResultJSON("bid.update"))))
	c, _ := app(t, s)
	if _, err := c.Bids.Update(context.Background(), testBidCode, &BidsUpdateParams{Price: Float(1250.75), LocalOnly: Bool(false)}); err != nil {
		t.Fatal(err)
	}
	got := s.requests()[0]
	if got.Body != `{"price":1250.75,"local_only":false}` {
		t.Errorf("body %s", got.Body)
	}
	if got.Header.Get("Content-Type") != "application/json" || got.Method != "PATCH" {
		t.Errorf("%s with Content-Type %q", got.Method, got.Header.Get("Content-Type"))
	}
}

func TestRequiredBodyFieldsAreSent(t *testing.T) {
	s := newServer(t, always(201, ok(bidResultJSON("bid.place"))))
	c, _ := app(t, s)
	_, err := c.Bids.Place(context.Background(), &BidsPlaceParams{LWIN18: testLWIN, Quantity: 2, Price: 1200})
	if err != nil {
		t.Fatal(err)
	}
	if got := s.requests()[0].Body; got != `{"lwin18":"`+testLWIN+`","quantity":2,"price":1200}` {
		t.Errorf("body %s", got)
	}
}

func TestEmptyWriteBody(t *testing.T) {
	s := newServer(t, func(w http.ResponseWriter, r *http.Request, n int) {
		if r.Method == http.MethodDelete && strings.HasPrefix(r.URL.Path, "/api/v1/confirmations/") {
			w.WriteHeader(204)
			return
		}
		reply(w, 200, ok(bidResultJSON("bid.renew")))
	})
	c, _ := app(t, s)
	ctx := context.Background()
	if _, err := c.Bids.Renew(ctx, testBidCode, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Bids.Cancel(ctx, testBidCode, &BidsCancelParams{}); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Confirmations.Confirm(ctx, testCnf); err != nil {
		t.Fatal(err)
	}
	if err := c.Confirmations.Cancel(ctx, testCnf); err != nil {
		t.Fatal(err)
	}
	reqs := s.requests()
	for i, want := range []string{"{}", "{}", "{}", ""} {
		if reqs[i].Body != want {
			t.Errorf("%s %s sent %q, want %q", reqs[i].Method, reqs[i].Path, reqs[i].Body, want)
		}
	}
}

func TestPartnerWriteGetsReference(t *testing.T) {
	s := newServer(t, always(201, ok(bidResultJSON("bid.place"))))
	c, _ := partner(t, s)
	ctx := context.Background()
	params := &BidsPlaceParams{LWIN18: testLWIN, Quantity: 1, Price: 99.5}
	if _, err := c.Bids.Place(ctx, params); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Bids.Place(ctx, params); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Bids.Place(ctx, &BidsPlaceParams{Reference: String("my-ref-1"), LWIN18: testLWIN, Quantity: 1, Price: 99.5}); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Bids.Cancel(ctx, testBidCode, nil); err != nil {
		t.Fatal(err)
	}
	reqs := s.requests()
	first, second := mustJSON(t, reqs[0].Body), mustJSON(t, reqs[1].Body)
	if !referencePattern.MatchString(first["reference"].(string)) {
		t.Errorf("generated reference %v", first["reference"])
	}
	if first["reference"] == second["reference"] {
		t.Error("two calls shared a reference")
	}
	if params.Reference != nil {
		t.Error("the caller's params were changed")
	}
	if !strings.HasPrefix(reqs[0].Body, `{"reference":"sdk_`) || first["lwin18"] != testLWIN {
		t.Errorf("body %s", reqs[0].Body)
	}
	if got := mustJSON(t, reqs[2].Body)["reference"]; got != "my-ref-1" {
		t.Errorf("the caller's reference became %v", got)
	}
	if cancel := mustJSON(t, reqs[3].Body); len(cancel) != 1 || !referencePattern.MatchString(cancel["reference"].(string)) {
		t.Errorf("a write without params sent %s", reqs[3].Body)
	}
}

func TestReferenceIdenticalAcrossRetries(t *testing.T) {
	s := newServer(t, func(w http.ResponseWriter, r *http.Request, n int) {
		switch n {
		case 0:
			reply(w, 503, apiError("SERVICE_UNAVAILABLE", "Try again."))
		case 1:
			reply(w, 409, apiError("REQUEST_IN_PROGRESS", "Still running."))
		default:
			w.Header().Set("Idempotent-Replayed", "true")
			reply(w, 201, ok(bidResultJSON("bid.place")))
		}
	})
	c, _ := partner(t, s)
	res, err := c.Bids.Place(context.Background(), &BidsPlaceParams{LWIN18: testLWIN, Quantity: 1, Price: 10})
	if err != nil {
		t.Fatal(err)
	}
	reqs := s.requests()
	if len(reqs) != 3 {
		t.Fatalf("%d requests, want 3", len(reqs))
	}
	if reqs[0].Body != reqs[1].Body || reqs[1].Body != reqs[2].Body {
		t.Errorf("the body changed across retries:\n%s\n%s\n%s", reqs[0].Body, reqs[1].Body, reqs[2].Body)
	}
	if !res.Replayed {
		t.Error("Idempotent-Replayed was not read")
	}
}
