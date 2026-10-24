package cru

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"testing"
)

func TestGetUnwrapsItemsKey(t *testing.T) {
	s := newServer(t, func(w http.ResponseWriter, r *http.Request, n int) {
		switch r.URL.Path {
		case "/api/v1/ping":
			reply(w, 200, ok(`{"pong":true,"client":{"environment":"test","key_prefix":"ck_test","organisation":"Example Wines Ltd",
				"label":null,"market":"uk","currency":"GBP","scopes":["bids:read"],"expires_at":null}}`))
		case "/api/v1/bids/" + testBidCode:
			reply(w, 200, ok(`{"bid":`+bidJSON(testBidCode)+`}`))
		case "/api/v1/bids/no-key":
			reply(w, 200, ok(`{"something":{}}`))
		default:
			w.WriteHeader(200)
			_, _ = w.Write([]byte(`not json`))
		}
	})
	c, _ := partner(t, s)
	ctx := context.Background()

	ping, err := c.Ping(ctx)
	if err != nil || !ping.Pong || ping.Client.Market != MarketUK || ping.Client.Label != nil ||
		*ping.Client.Organisation != "Example Wines Ltd" || ping.Client.Scopes[0] != "bids:read" {
		t.Fatalf("ping %+v, %v", ping, err)
	}
	bid, err := c.Bids.Get(ctx, testBidCode)
	if err != nil || bid.Code != testBidCode || bid.Price != 1200.5 || *bid.Vintage != 2015 || bid.ClientRef != nil || bid.Quantity != 2 {
		t.Fatalf("bid %+v, %v", bid, err)
	}
	if _, err := c.Bids.Get(ctx, "no-key"); !errors.Is(err, ErrUnexpectedResponse) || !strings.Contains(err.Error(), "data.bid") {
		t.Errorf("missing data.bid: %v", err)
	}
	if _, err := c.Bids.Get(ctx, "html"); !errors.Is(err, ErrUnexpectedResponse) {
		t.Errorf("a non-JSON answer: %v", err)
	}
}

func TestList(t *testing.T) {
	s := newServer(t, func(w http.ResponseWriter, r *http.Request, n int) {
		switch n {
		case 0:
			reply(w, 200, ok(`{"warehouses":[{"code":"UK_TEST","name":"Test Bond One"},{"code":"HK_TEST","name":"Test Bond Two"}]}`))
		case 1:
			reply(w, 200, ok(`{"warehouses":{}}`))
		default:
			reply(w, 200, ok(`{}`))
		}
	})
	c, _ := partner(t, s)
	got, err := c.Warehouses.List(context.Background())
	if err != nil || len(got) != 2 || got[1].Code != "HK_TEST" || got[1].Name != "Test Bond Two" {
		t.Fatalf("%+v, %v", got, err)
	}
	for range 2 {
		if got, err := c.Warehouses.List(context.Background()); err != nil || len(got) != 0 {
			t.Errorf("an empty list: %+v, %v", got, err)
		}
	}
}

// pagedBids answers GET /bids from a list of 5 bids, limit 2, as the API pages.
func pagedBids(failAt int) func(http.ResponseWriter, *http.Request, int) {
	return func(w http.ResponseWriter, r *http.Request, n int) {
		offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
		if offset == failAt {
			reply(w, 500, apiError("INTERNAL_ERROR", "Oops."))
			return
		}
		var items []string
		for i := offset; i < offset+2 && i < 5; i++ {
			items = append(items, bidJSON(fmt.Sprintf("BID-%d", i)))
		}
		pagination := fmt.Sprintf(`{"limit":2,"offset":%d,"total":5,"has_more":%v}`, offset, offset+2 < 5)
		reply(w, 200, okPaged(`{"bids":[`+strings.Join(items, ",")+`],"summary":{"live":5}}`, pagination))
	}
}

func TestPaging(t *testing.T) {
	s := newServer(t, pagedBids(-1))
	c, _ := partner(t, s, WithMaxRetries(0))
	ctx := context.Background()
	page, err := c.Bids.List(ctx, &BidsListParams{Status: []string{BidStatusLive}, Limit: Int(2)})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 2 || page.Limit != 2 || page.Offset != 0 || page.Total == nil || *page.Total != 5 || !page.HasMore {
		t.Fatalf("first page %+v", page)
	}
	if page.Meta.RequestID != "req_test" || !strings.Contains(string(page.Data), `"summary"`) {
		t.Errorf("meta %+v, data %s", page.Meta, page.Data)
	}

	var codes []string
	for bid, err := range page.All(ctx) {
		if err != nil {
			t.Fatal(err)
		}
		codes = append(codes, bid.Code)
	}
	if strings.Join(codes, " ") != "BID-0 BID-1 BID-2 BID-3 BID-4" {
		t.Errorf("All() gave %v", codes)
	}
	reqs := s.requests()
	if len(reqs) != 3 {
		t.Fatalf("%d requests, want 3", len(reqs))
	}
	for i, want := range []string{"", "2", "4"} {
		if reqs[i].Query.Get("offset") != want || reqs[i].Query.Get("status") != "live" || reqs[i].Query.Get("limit") != "2" {
			t.Errorf("page %d sent %v", i, reqs[i].Query)
		}
	}

	last, err := page.Next(ctx)
	if err != nil {
		t.Fatal(err)
	}
	last, _ = last.Next(ctx)
	if end, err := last.Next(ctx); end != nil || err != nil || last.HasMore || len(last.Items) != 1 {
		t.Errorf("past the last page: %v, %v", end, err)
	}
}

func TestPagingStops(t *testing.T) {
	s := newServer(t, pagedBids(2))
	c, _ := partner(t, s, WithMaxRetries(0))
	page, err := c.Bids.List(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	var n int
	var last error
	for _, err := range page.All(context.Background()) {
		if err != nil {
			last = err
			continue
		}
		n++
	}
	if n != 2 || !errors.Is(last, ErrServer) {
		t.Errorf("%d items, then %v", n, last)
	}

	s = newServer(t, pagedBids(-1))
	c, _ = partner(t, s)
	page, _ = c.Bids.List(context.Background(), nil)
	for range page.All(context.Background()) {
		break
	}
	if len(s.requests()) != 1 {
		t.Errorf("breaking out fetched %d pages", len(s.requests()))
	}
}

func TestPageWithoutHasMore(t *testing.T) {
	s := newServer(t, func(w http.ResponseWriter, r *http.Request, n int) {
		if n == 0 {
			reply(w, 200, okPaged(`{"wines":[{"lwin18":"1"},{"lwin18":"2"}]}`, `{"limit":2,"offset":0}`))
			return
		}
		reply(w, 200, okPaged(`{"wines":[{"lwin18":"3"}]}`, `{"limit":2,"offset":2}`))
	})
	c, _ := partner(t, s)
	page, err := c.Wines.Search(context.Background(), &WinesSearchParams{Q: "exemple"})
	if err != nil || !page.HasMore || page.Total != nil {
		t.Fatalf("a full page without has_more: %+v, %v", page, err)
	}
	next, _ := page.Next(context.Background())
	if next.HasMore || next.Offset != 2 || next.Items[0].LWIN18 != "3" {
		t.Errorf("a short page: %+v", next)
	}
}

func TestWriteResult(t *testing.T) {
	s := newServer(t, func(w http.ResponseWriter, r *http.Request, n int) {
		if n == 1 {
			w.Header().Set("Idempotent-Replayed", "true")
		}
		reply(w, 201, ok(bidResultJSON("bid.place")))
	})
	c, _ := partner(t, s)
	params := &BidsPlaceParams{Reference: String("ref-1"), LWIN18: testLWIN, Quantity: 2, Price: 1200.5}
	w, err := c.Bids.Place(context.Background(), params)
	if err != nil {
		t.Fatal(err)
	}
	if w.Staged || w.Confirmation != nil || w.Replayed || w.StatusCode != 201 || w.RequestID != "req_test" {
		t.Errorf("write %+v", w)
	}
	if w.Result.Action != ActionBidPlace || w.Result.Status != ResultStatusCompleted || w.Result.Bid.Code != testBidCode {
		t.Errorf("result %+v", w.Result)
	}
	if w, _ = c.Bids.Place(context.Background(), params); !w.Replayed {
		t.Error("Idempotent-Replayed: true was not read")
	}
}

func TestStagedWrite(t *testing.T) {
	s := newServer(t, always(202, ok(stagedJSON("bid.place"))))
	c, _ := app(t, s)
	w, err := c.Bids.Place(context.Background(), &BidsPlaceParams{LWIN18: testLWIN, Quantity: 2, Price: 1200.5})
	if err != nil {
		t.Fatal(err)
	}
	if !w.Staged || w.Result != nil || w.StatusCode != 202 || w.Confirmation == nil {
		t.Fatalf("write %+v", w)
	}
	conf := w.Confirmation
	if conf.Code != testCnf || conf.Action != ActionBidPlace || conf.Status != ConfirmationStatusPending || conf.Preview.Summary == "" {
		t.Errorf("confirmation %+v", conf)
	}
	var lwin string
	if found, _ := Field(&conf.Preview, "lwin18", &lwin); !found || lwin != testLWIN {
		t.Error("the preview's own fields are not reachable")
	}
}

func TestConfirmByAction(t *testing.T) {
	cases := map[string]string{
		"bid.place":    bidResultJSON("bid.place"),
		"offer.remove": `{"action":"offer.remove","status":"completed","offer":null,"removed":3,"deleted":true}`,
		"bid.buy":      `{"action":"bid.buy","status":"completed","order":{"number":"100000001","status":"pending","total":2401,"currency":"GBP","pending_payment":true},"lwin18":"` + testLWIN + `","quantity":2,"price":1200.5}`,
		"wine.gift":    `{"action":"wine.gift","status":"completed","gift":{"to":"someone"}}`,
	}
	for action, data := range cases {
		s := newServer(t, always(201, ok(data)))
		c, _ := app(t, s)
		w, err := c.Confirmations.Confirm(context.Background(), testCnf)
		if err != nil {
			t.Fatal(err)
		}
		r := w.Result
		if r.Action != action || r.Status != "completed" || len(r.Raw()) == 0 {
			t.Errorf("%s: %+v", action, r)
		}
		switch v := r.Value.(type) {
		case *BidResult:
			if action != "bid.place" || v.Bid.Code != testBidCode {
				t.Errorf("%s gave a BidResult %+v", action, v)
			}
		case *OfferRemoveResult:
			if action != "offer.remove" || v.Offer != nil || v.Removed != 3 || !v.Deleted {
				t.Errorf("%s gave an OfferRemoveResult %+v", action, v)
			}
		case *BuyResult:
			if action != "bid.buy" || v.Order.Number != "100000001" || !v.Order.PendingPayment || v.Quantity != 2 {
				t.Errorf("%s gave a BuyResult %+v", action, v)
			}
		case *InstructionResult:
			var gift map[string]any
			if found, _ := Field(v, "gift", &gift); action != "wine.gift" || !found || gift["to"] != "someone" {
				t.Errorf("%s gave an InstructionResult %+v", action, v)
			}
		default:
			t.Errorf("%s gave %T", action, v)
		}
	}
}

func TestEmptyAndBinary(t *testing.T) {
	png := []byte("\x89PNG\r\n\x1a\nfake")
	s := newServer(t, func(w http.ResponseWriter, r *http.Request, n int) {
		if strings.HasPrefix(r.URL.Path, "/api/v1/photos/") {
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write(png)
			return
		}
		w.WriteHeader(204)
	})
	c, _ := app(t, s)
	if err := c.Confirmations.Cancel(context.Background(), testCnf); err != nil {
		t.Errorf("204: %v", err)
	}
	got, err := c.Photos.Get(context.Background(), "PHOTO-TEST")
	if err != nil || string(got) != string(png) {
		t.Errorf("photo %q, %v", got, err)
	}
	if accept := s.requests()[1].Header.Get("Accept"); accept != "*/*" {
		t.Errorf("a binary call sent Accept %q", accept)
	}
}
