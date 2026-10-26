package confirmations

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/cruworldwine/cru-api-go/cru/bids"
	"github.com/cruworldwine/cru-api-go/cru/core"
	"github.com/cruworldwine/cru-api-go/cru/market"
	"github.com/cruworldwine/cru-api-go/cru/offers"
	"github.com/cruworldwine/cru-api-go/internal/testkit"
)

func TestConfirmByAction(t *testing.T) {
	cases := map[string]string{
		"bid.place":    testkit.BidResultJSON("bid.place"),
		"offer.remove": `{"action":"offer.remove","status":"completed","offer":null,"removed":3,"deleted":true}`,
		"bid.buy":      `{"action":"bid.buy","status":"completed","order":{"number":"100000001","status":"pending","total":2401,"currency":"GBP","pending_payment":true},"lwin18":"` + testkit.LWIN + `","quantity":2,"price":1200.5}`,
		"wine.gift":    `{"action":"wine.gift","status":"completed","gift":{"to":"someone"}}`,
	}
	for action, data := range cases {
		s := testkit.NewServer(t, testkit.Always(201, testkit.OK(data)))
		w, err := newService(testkit.App(t, s)).Confirm(context.Background(), testkit.Cnf)
		if err != nil {
			t.Fatal(err)
		}
		r := w.Result
		if r.Action != action || r.Status != "completed" || len(r.Raw()) == 0 {
			t.Errorf("%s: %+v", action, r)
		}
		switch v := r.Value.(type) {
		case *bids.Result:
			if action != "bid.place" || v.Bid.Code != testkit.BidCode {
				t.Errorf("%s gave a bids.Result %+v", action, v)
			}
		case *offers.RemoveResult:
			if action != "offer.remove" || v.Offer != nil || v.Removed != 3 || !v.Deleted {
				t.Errorf("%s gave an offers.RemoveResult %+v", action, v)
			}
		case *market.BuyResult:
			if action != "bid.buy" || v.Order.Number != "100000001" || !v.Order.PendingPayment || v.Quantity != 2 {
				t.Errorf("%s gave a market.BuyResult %+v", action, v)
			}
		case *InstructionResult:
			var gift map[string]any
			if found, _ := core.Field(v, "gift", &gift); action != "wine.gift" || !found || gift["to"] != "someone" {
				t.Errorf("%s gave an InstructionResult %+v", action, v)
			}
		default:
			t.Errorf("%s gave %T", action, v)
		}
	}
	var nilResult *ActionResult
	if nilResult.Raw() != nil {
		t.Error("Raw of a nil result")
	}
}

func TestEmptyWriteBody(t *testing.T) {
	s := testkit.NewServer(t, func(w http.ResponseWriter, r *http.Request, n int) {
		if r.Method == http.MethodDelete {
			w.WriteHeader(204)
			return
		}
		testkit.Reply(w, 200, testkit.OK(testkit.BidResultJSON("bid.place")))
	})
	c := newService(testkit.App(t, s))
	ctx := context.Background()
	if _, err := c.Confirm(ctx, testkit.Cnf); err != nil {
		t.Fatal(err)
	}
	if err := c.Cancel(ctx, testkit.Cnf); err != nil {
		t.Fatal(err)
	}
	reqs := s.Requests()
	for i, want := range []string{"{}", ""} {
		if reqs[i].Body != want {
			t.Errorf("%s %s sent %q, want %q", reqs[i].Method, reqs[i].Path, reqs[i].Body, want)
		}
	}
}

func TestEmptyCode(t *testing.T) {
	s := testkit.NewServer(t, testkit.Always(204, ""))
	c := newService(testkit.App(t, s))
	ctx := context.Background()
	if err := c.Cancel(ctx, ""); !errors.Is(err, core.ErrMissingParameter) || !strings.Contains(err.Error(), "code") {
		t.Errorf("Cancel: %v", err)
	}
	if _, err := c.Confirm(ctx, ""); !errors.Is(err, core.ErrMissingParameter) {
		t.Errorf("Confirm: %v", err)
	}
	if _, err := c.Get(ctx, ""); !errors.Is(err, core.ErrMissingParameter) {
		t.Errorf("Get: %v", err)
	}
	if len(s.Requests()) != 0 {
		t.Error("a request was sent with an empty code")
	}
}

func TestWrongTypesLeaveZero(t *testing.T) {
	var conf Confirmation
	if err := json.Unmarshal([]byte(`{"action":"bid.place","result":[],"error":null}`), &conf); err != nil {
		t.Fatal(err)
	}
	if conf.Result != nil || conf.Error != nil || conf.Action != core.ActionBidPlace {
		t.Errorf("%+v", conf)
	}
}
