package caller_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"testing"

	"github.com/cruworldwine/cru-api-go/cru/bids"
	"github.com/cruworldwine/cru-api-go/cru/core"
	"github.com/cruworldwine/cru-api-go/cru/warehouses"
	"github.com/cruworldwine/cru-api-go/internal/caller"
	"github.com/cruworldwine/cru-api-go/internal/testkit"
	"github.com/cruworldwine/cru-api-go/internal/transport"
)

// The generic calls are tested with the bids and warehouses models, which the generated code
// decodes them into.

var referencePattern = regexp.MustCompile(`^sdk_[0-9a-f]{32}$`)

func get(path, key string) caller.Endpoint {
	return caller.Endpoint{Method: http.MethodGet, Path: path, Key: key}
}

func TestGetOne(t *testing.T) {
	s := testkit.NewServer(t, func(w http.ResponseWriter, r *http.Request, n int) {
		switch r.URL.Path {
		case "/api/v1/bids/" + testkit.BidCode:
			testkit.Reply(w, 200, testkit.OK(`{"bid":`+testkit.BidJSON(testkit.BidCode)+`}`))
		case "/api/v1/bid":
			testkit.Reply(w, 200, testkit.OK(testkit.BidJSON("BID-DATA")))
		case "/api/v1/bids/no-key":
			testkit.Reply(w, 200, testkit.OK(`{"something":{}}`))
		case "/api/v1/bids/not-a-bid":
			testkit.Reply(w, 200, testkit.OK(`{"bid":"text"}`))
		default:
			w.WriteHeader(200)
			_, _ = w.Write([]byte(`not json`))
		}
	})
	c := testkit.Partner(t, s)
	ctx := context.Background()

	bid, err := caller.GetOne[bids.OwnBid](ctx, c, get("/bids/"+testkit.BidCode, "bid"))
	if err != nil || bid.Code != testkit.BidCode || bid.Price != 1200.5 || *bid.Vintage != 2015 || bid.ClientRef != nil || bid.Quantity != 2 {
		t.Fatalf("bid %+v, %v", bid, err)
	}
	if bid, err := caller.GetOne[bids.OwnBid](ctx, c, get("/bid", "")); err != nil || bid.Code != "BID-DATA" {
		t.Errorf("data itself: %+v, %v", bid, err)
	}
	if _, err := caller.GetOne[bids.OwnBid](ctx, c, get("/bids/no-key", "bid")); !errors.Is(err, core.ErrUnexpectedResponse) || !strings.Contains(err.Error(), "data.bid") {
		t.Errorf("missing data.bid: %v", err)
	}
	if _, err := caller.GetOne[bids.OwnBid](ctx, c, get("/bids/html", "bid")); !errors.Is(err, core.ErrUnexpectedResponse) {
		t.Errorf("a non-JSON answer: %v", err)
	}
	if _, err := caller.GetOne[[]string](ctx, c, get("/bids/not-a-bid", "bid")); !errors.Is(err, core.ErrUnexpectedResponse) {
		t.Errorf("data.bid of the wrong shape: %v", err)
	}
}

func TestGetList(t *testing.T) {
	s := testkit.NewServer(t, func(w http.ResponseWriter, r *http.Request, n int) {
		switch n {
		case 0:
			testkit.Reply(w, 200, testkit.OK(`{"warehouses":[{"code":"UK_TEST","name":"Test Bond One"},{"code":"HK_TEST","name":"Test Bond Two"}]}`))
		case 1:
			testkit.Reply(w, 200, testkit.OK(`{"warehouses":{}}`))
		case 2:
			testkit.Reply(w, 200, testkit.OK(`{}`))
		case 3:
			testkit.Reply(w, 200, testkit.OK(`{"warehouses":"none"}`))
		default:
			testkit.Reply(w, 500, testkit.ErrorBody("INTERNAL_ERROR", "Oops."))
		}
	})
	c := testkit.Partner(t, s, func(c *transport.Config) { c.MaxRetries = 0 })
	e := get("/warehouses", "warehouses")
	got, err := caller.GetList[warehouses.Warehouse](context.Background(), c, e)
	if err != nil || len(got) != 2 || got[1].Code != "HK_TEST" || got[1].Name != "Test Bond Two" {
		t.Fatalf("%+v, %v", got, err)
	}
	for range 2 {
		if got, err := caller.GetList[warehouses.Warehouse](context.Background(), c, e); err != nil || len(got) != 0 {
			t.Errorf("an empty list: %+v, %v", got, err)
		}
	}
	if _, err := caller.GetList[warehouses.Warehouse](context.Background(), c, e); !errors.Is(err, core.ErrUnexpectedResponse) {
		t.Errorf("not a list: %v", err)
	}
	if _, err := caller.GetList[warehouses.Warehouse](context.Background(), c, e); !errors.Is(err, core.ErrServer) {
		t.Errorf("an error answer: %v", err)
	}
}

func TestGetPage(t *testing.T) {
	s := testkit.NewServer(t, func(w http.ResponseWriter, r *http.Request, n int) {
		switch n {
		case 0:
			testkit.Reply(w, 200, testkit.OKPaged(`{"bids":[`+testkit.BidJSON("BID-1")+`,{"code":"BID-2","quantity":"lots","price":{}}],"new_key":1}`,
				`{"limit":"2","offset":0.0,"total":3}`))
		case 1:
			testkit.Reply(w, 200, testkit.OKPaged(`{"bids":[`+testkit.BidJSON("BID-3")+`]}`, `{"limit":2,"offset":2,"has_more":false}`))
		case 2:
			testkit.Reply(w, 200, testkit.OK(`{"bids":"none"}`))
		default:
			testkit.Reply(w, 200, testkit.OK(`{"bids":[]}`))
		}
	})
	c := testkit.Partner(t, s)
	ctx := context.Background()
	e := caller.Endpoint{Method: http.MethodGet, Path: "/bids", Key: "bids", Query: map[string][]string{"status": {"live"}}}
	page, err := caller.GetPage[bids.OwnBid](ctx, c, e)
	if err != nil {
		t.Fatal(err)
	}
	// A field that does not fit is left unset, and the rest of the answer decodes.
	if len(page.Items) != 2 || page.Items[1].Code != "BID-2" || page.Items[1].Quantity != 0 || page.Limit != 2 ||
		*page.Total != 3 || !page.HasMore || !strings.Contains(string(page.Data), "new_key") {
		t.Errorf("first page %+v", page)
	}
	next, err := page.Next(ctx)
	if err != nil || len(next.Items) != 1 || next.HasMore || next.Offset != 2 {
		t.Fatalf("second page %+v, %v", next, err)
	}
	if got := s.Requests()[1].Query; got.Get("offset") != "2" || got.Get("status") != "live" || len(e.Query) != 1 {
		t.Errorf("the next page sent %v; the first call's query became %v", got, e.Query)
	}
	if _, err = caller.GetPage[bids.OwnBid](ctx, c, e); !errors.Is(err, core.ErrUnexpectedResponse) {
		t.Errorf("not a list: %v", err)
	}
	page, err = caller.GetPage[bids.OwnBid](ctx, c, e)
	if err != nil || len(page.Items) != 0 || page.HasMore || page.Limit != 0 {
		t.Errorf("a page without pagination: %+v, %v", page, err)
	}
}

func TestWriteResult(t *testing.T) {
	s := testkit.NewServer(t, func(w http.ResponseWriter, r *http.Request, n int) {
		if n == 1 {
			w.Header().Set("Idempotent-Replayed", "true")
		}
		testkit.Reply(w, 201, testkit.OK(testkit.BidResultJSON("bid.place")))
	})
	c := testkit.Partner(t, s)
	e := caller.Endpoint{Method: http.MethodPost, Path: "/bids", Body: &bids.PlaceParams{LWIN18: testkit.LWIN, Quantity: 2, Price: 1200.5}}
	w, err := caller.DoWrite[bids.Result](context.Background(), c, e)
	if err != nil {
		t.Fatal(err)
	}
	if w.Staged || w.Confirmation != nil || w.Replayed || w.StatusCode != 201 || w.RequestID != "req_test" {
		t.Errorf("write %+v", w)
	}
	if w.Result.Action != core.ActionBidPlace || w.Result.Status != core.ResultStatusCompleted || w.Result.Bid.Code != testkit.BidCode {
		t.Errorf("result %+v", w.Result)
	}
	if w, _ = caller.DoWrite[bids.Result](context.Background(), c, e); !w.Replayed {
		t.Error("Idempotent-Replayed: true was not read")
	}
	if !referencePattern.MatchString(testkit.MustJSON(t, s.Requests()[0].Body)["reference"].(string)) {
		t.Errorf("a partner write without a reference sent %s", s.Requests()[0].Body)
	}
}

func TestStagedWrite(t *testing.T) {
	s := testkit.NewServer(t, testkit.Always(202, testkit.OK(testkit.StagedJSON("bid.place"))))
	c := testkit.App(t, s)
	w, err := caller.DoWrite[bids.Result](context.Background(), c, caller.Endpoint{Method: http.MethodPost, Path: "/bids", Body: caller.EmptyObject})
	if err != nil {
		t.Fatal(err)
	}
	if !w.Staged || w.Result != nil || w.StatusCode != 202 || w.Confirmation == nil {
		t.Fatalf("write %+v", w)
	}
	conf := w.Confirmation
	if conf.Code != testkit.Cnf || conf.Action != core.ActionBidPlace || conf.Status != core.ConfirmationStatusPending || conf.Preview.Summary == "" {
		t.Errorf("confirmation %+v", conf)
	}
	var lwin string
	if found, _ := core.Field(&conf.Preview, "lwin18", &lwin); !found || lwin != testkit.LWIN {
		t.Error("the preview's own fields are not reachable")
	}
	if got := s.Requests()[0].Body; got != "{}" {
		t.Errorf("EmptyObject was sent as %q", got)
	}
}

func TestWriteOf(t *testing.T) {
	s := testkit.NewServer(t, func(w http.ResponseWriter, r *http.Request, n int) {
		if n == 0 {
			testkit.Reply(w, 200, testkit.OK(`{"action":"wine.gift"}`))
			return
		}
		testkit.Reply(w, 404, testkit.ErrorBody("CONFIRMATION_NOT_FOUND", "No."))
	})
	c := testkit.App(t, s)
	e := caller.Endpoint{Method: http.MethodPost, Path: "/confirmations/x", Body: caller.EmptyObject}
	var seen json.RawMessage
	w, err := caller.WriteOf(context.Background(), c, e, func(data json.RawMessage) *string {
		seen = data
		action := "decoded"
		return &action
	})
	if err != nil || *w.Result != "decoded" || string(seen) != `{"action":"wine.gift"}` {
		t.Errorf("result %v from %s, %v", w, seen, err)
	}
	if _, err := caller.WriteOf(context.Background(), c, e, func(json.RawMessage) *string { return nil }); !errors.Is(err, core.ErrNotFound) {
		t.Errorf("an error answer: %v", err)
	}
}

func TestEmptyAndBinary(t *testing.T) {
	png := []byte("\x89PNG\r\n\x1a\nfake")
	s := testkit.NewServer(t, func(w http.ResponseWriter, r *http.Request, n int) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/api/v1/photos/missing"):
			testkit.Reply(w, 404, testkit.ErrorBody("NOT_FOUND", "No photo."))
		case strings.HasPrefix(r.URL.Path, "/api/v1/photos/"):
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write(png)
		default:
			w.WriteHeader(204)
		}
	})
	c := testkit.App(t, s)
	ctx := context.Background()
	if err := caller.DoEmpty(ctx, c, caller.Endpoint{Method: http.MethodDelete, Path: "/confirmations/" + testkit.Cnf}); err != nil {
		t.Errorf("204: %v", err)
	}
	got, err := caller.DoBinary(ctx, c, get("/photos/PHOTO-TEST", ""))
	if err != nil || string(got) != string(png) {
		t.Errorf("photo %q, %v", got, err)
	}
	if accept := s.Requests()[1].Header.Get("Accept"); accept != "*/*" {
		t.Errorf("a binary call sent Accept %q", accept)
	}
	if _, err := caller.DoBinary(ctx, c, get("/photos/missing", "")); !errors.Is(err, core.ErrNotFound) {
		t.Errorf("a missing photo: %v", err)
	}
}

func TestExpandPath(t *testing.T) {
	got, err := caller.ExpandPath("/bids/{code}/renew", "code", "a/b c?d#e")
	if err != nil || got != "/bids/a%2Fb%20c%3Fd%23e/renew" {
		t.Errorf("%s, %v", got, err)
	}
	if got, err := caller.ExpandPath("/warehouses/inbond/{id}", "id", "42"); err != nil || got != "/warehouses/inbond/42" {
		t.Errorf("%s, %v", got, err)
	}
	if _, err := caller.ExpandPath("/bids/{code}", "code", ""); !errors.Is(err, core.ErrMissingParameter) || !strings.Contains(err.Error(), "code") {
		t.Errorf("empty code: %v", err)
	}
}

func TestCall(t *testing.T) {
	s := testkit.NewServer(t, testkit.Always(200, testkit.OK(`{}`)))
	c := testkit.Partner(t, s)
	resp, err := caller.Call(context.Background(), c, caller.Endpoint{Method: http.MethodGet, Path: "/things",
		Opts: []core.RequestOption{nil, core.WithExtraQuery("a", "1")}})
	if err != nil || resp.Status != 200 || s.Requests()[0].Query.Get("a") != "1" {
		t.Errorf("%v, %v; query %v", resp, err, s.Requests()[0].Query)
	}
	_, err = caller.Call(context.Background(), c, caller.Endpoint{Method: http.MethodGet, Path: "/things",
		Opts: []core.RequestOption{core.WithExtraBodyField("x", 1)}})
	if err == nil || len(s.Requests()) != 1 {
		t.Errorf("a refused option: %v, %d requests", err, len(s.Requests()))
	}
}

func TestNewService(t *testing.T) {
	s := testkit.NewServer(t, testkit.Always(200, testkit.OK(`{"bid":`+testkit.BidJSON(testkit.BidCode)+`}`)))
	svc := caller.NewService[bids.Service](testkit.Partner(t, s))
	if bid, err := svc.Get(context.Background(), testkit.BidCode); err != nil || bid.Code != testkit.BidCode {
		t.Errorf("a registered service: %+v, %v", bid, err)
	}
	defer func() {
		if r := recover(); r == nil || !strings.Contains(fmt.Sprint(r), "caller_test.unregistered") {
			t.Errorf("an unregistered service: %v", r)
		}
	}()
	caller.NewService[unregistered](testkit.Partner(t, s))
}

type unregistered struct{}
