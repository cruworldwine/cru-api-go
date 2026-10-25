package core_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/cruworldwine/cru-api-go/cru/bids"
	"github.com/cruworldwine/cru-api-go/cru/confirmations"
	"github.com/cruworldwine/cru-api-go/cru/core"
	"github.com/cruworldwine/cru-api-go/cru/photos"
	"github.com/cruworldwine/cru-api-go/internal/caller"
	"github.com/cruworldwine/cru-api-go/internal/testkit"
	"github.com/cruworldwine/cru-api-go/internal/transport"
)

func TestWithRequestHeader(t *testing.T) {
	s := testkit.NewServer(t, testkit.Always(200, testkit.OK(`{"bid":`+testkit.BidJSON(testkit.BidCode)+`}`)))
	b := bidsOn(testkit.Partner(t, s))
	ctx := context.Background()
	if _, err := b.Get(ctx, testkit.BidCode, core.WithRequestHeader("traceparent", "00-abc-01"), core.WithRequestHeader("User-Agent", "mine")); err != nil {
		t.Fatal(err)
	}
	h := s.Requests()[0].Header
	if h.Get("Traceparent") != "00-abc-01" || h.Get("User-Agent") != "mine" || h.Get("Client-Key") != "ck_test" {
		t.Errorf("headers %v", h)
	}
	for _, name := range []string{"Authorization", "client-key", "CLIENT-SECRET"} {
		_, err := b.Get(ctx, testkit.BidCode, core.WithRequestHeader(name, "stolen"))
		if err == nil || !strings.Contains(err.Error(), "credentials") {
			t.Errorf("%s: %v", name, err)
		}
	}
	for _, h := range [][2]string{{"", "v"}, {"Bad Name", "v"}, {"X-Ok", "line\r\nX-Injected: 1"}, {"X-Ok", "nul\x00"}} {
		if _, err := b.Get(ctx, testkit.BidCode, core.WithRequestHeader(h[0], h[1])); err == nil {
			t.Errorf("header %q: %q was sent", h[0], h[1])
		}
	}
	if len(s.Requests()) != 1 {
		t.Errorf("a refused header still sent %d requests", len(s.Requests())-1)
	}
}

func TestWithRequestTimeout(t *testing.T) {
	s := testkit.NewServer(t, func(w http.ResponseWriter, r *http.Request, n int) {
		if n == 0 {
			<-r.Context().Done()
			return
		}
		testkit.Reply(w, 200, testkit.OK(`{"bid":`+testkit.BidJSON(testkit.BidCode)+`}`))
	})
	b := bidsOn(testkit.Partner(t, s, func(c *transport.Config) { c.Timeout = time.Hour }))
	_, err := b.Get(context.Background(), testkit.BidCode, core.WithRequestTimeout(20*time.Millisecond), core.WithRequestMaxRetries(0))
	var ce *core.ConnectionError
	if !errors.As(err, &ce) || !ce.Timeout() || len(s.Requests()) != 1 {
		t.Fatalf("want one timed-out attempt, got %v after %d", err, len(s.Requests()))
	}
	if _, err := b.Get(context.Background(), testkit.BidCode, core.WithRequestTimeout(-time.Second)); err != nil {
		t.Errorf("a negative timeout is no timeout: %v", err)
	}
}

func TestWithRequestMaxRetries(t *testing.T) {
	for _, tc := range []struct {
		n, want int
	}{{0, 1}, {-3, 1}, {4, 5}} {
		s := testkit.NewServer(t, testkit.Always(503, testkit.ErrorBody("SERVICE_UNAVAILABLE", "Busy.")))
		_, err := bidsOn(testkit.Partner(t, s)).Get(context.Background(), testkit.BidCode, core.WithRequestMaxRetries(tc.n))
		if !errors.Is(err, core.ErrServer) || len(s.Requests()) != tc.want {
			t.Errorf("WithRequestMaxRetries(%d): %v after %d requests, want %d", tc.n, err, len(s.Requests()), tc.want)
		}
	}
	s := testkit.NewServer(t, testkit.Always(503, testkit.ErrorBody("SERVICE_UNAVAILABLE", "Busy.")))
	_, _ = bidsOn(testkit.App(t, s)).Place(context.Background(), &bids.PlaceParams{LWIN18: testkit.LWIN, Quantity: 1, Price: 1}, core.WithRequestMaxRetries(5))
	if len(s.Requests()) != 1 {
		t.Errorf("a connected app's write was retried: %d requests", len(s.Requests()))
	}
}

func TestWithExtraQuery(t *testing.T) {
	s := testkit.NewServer(t, func(w http.ResponseWriter, r *http.Request, n int) {
		testkit.Reply(w, 200, testkit.OKPaged(`{"bids":[`+testkit.BidJSON(fmt.Sprintf("BID-%d", n))+`]}`,
			fmt.Sprintf(`{"limit":1,"offset":%d,"has_more":%t}`, n, n == 0)))
	})
	ctx := context.Background()
	page, err := bidsOn(testkit.Partner(t, s)).List(ctx, &bids.ListParams{Limit: int64Ptr(1), ClientRef: stringPtr("ref")},
		core.WithExtraQuery("limit", "9"), core.WithExtraQuery("new_filter", "a b"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := page.Next(ctx); err != nil {
		t.Fatal(err)
	}
	for i, r := range s.Requests() {
		if r.Query.Get("limit") != "9" || r.Query.Get("new_filter") != "a b" || r.Query.Get("client_ref") != "ref" {
			t.Errorf("request %d: query %v", i, r.Query)
		}
	}
	if got := s.Requests()[1].Query.Get("offset"); got != "1" {
		t.Errorf("the next page's offset %q", got)
	}
}

func TestWithExtraBodyField(t *testing.T) {
	s := testkit.NewServer(t, func(w http.ResponseWriter, r *http.Request, n int) {
		if r.Method == http.MethodDelete {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		testkit.Reply(w, 200, testkit.OK(testkit.BidResultJSON("bid.place")))
	})
	ctx := context.Background()
	c := testkit.Partner(t, s)
	b := bidsOn(c)

	_, err := b.Place(ctx, &bids.PlaceParams{LWIN18: testkit.LWIN, Quantity: 1, Price: 10},
		core.WithExtraBodyField("price", 12.5), core.WithExtraBodyField("new_field", map[string]any{"a": []int{1}}))
	if err != nil {
		t.Fatal(err)
	}
	body := testkit.MustJSON(t, s.Requests()[0].Body)
	if body["price"] != 12.5 || body["lwin18"] != testkit.LWIN || !strings.HasPrefix(body["reference"].(string), "sdk_") {
		t.Errorf("body %v", body)
	}
	if nf, _ := body["new_field"].(map[string]any); nf == nil || len(nf["a"].([]any)) != 1 {
		t.Errorf("new_field %v", body["new_field"])
	}

	if _, err := b.Cancel(ctx, testkit.BidCode, nil, core.WithExtraBodyField("reference", "mine-1")); err != nil {
		t.Fatal(err)
	}
	if body := testkit.MustJSON(t, s.Requests()[1].Body); body["reference"] != "mine-1" {
		t.Errorf("an extra reference was not used: %v", body)
	}

	confirm := caller.NewService[confirmations.Service](testkit.App(t, s))
	if err := confirm.Cancel(ctx, testkit.Cnf, core.WithExtraBodyField("reason", "changed my mind")); err != nil {
		t.Fatal(err)
	}
	if got := s.Requests()[2].Body; got != `{"reason":"changed my mind"}` {
		t.Errorf("a write without a body sent %s", got)
	}

	sent := len(s.Requests())
	for name, call := range map[string]func() error{
		"a read": func() error {
			_, err := b.Get(ctx, testkit.BidCode, core.WithExtraBodyField("x", 1))
			return err
		},
		"an unencodable value": func() error {
			_, err := b.Cancel(ctx, testkit.BidCode, nil, core.WithExtraBodyField("x", func() {}))
			return err
		},
		"a body that is not an object": func() error {
			_, err := caller.Call(ctx, c, caller.Endpoint{Method: "POST", Path: "/things", Body: []byte(`[1,2]`),
				Opts: []core.RequestOption{core.WithExtraBodyField("x", 1)}})
			return err
		},
	} {
		if err := call(); err == nil {
			t.Errorf("%s: want an error", name)
		}
	}
	if len(s.Requests()) != sent {
		t.Errorf("a refused call was sent")
	}
}

func TestWithResponseInto(t *testing.T) {
	ctx := context.Background()
	t.Run("success, after a retry", func(t *testing.T) {
		s := testkit.NewServer(t, func(w http.ResponseWriter, r *http.Request, n int) {
			w.Header().Set("X-Request-Id", "req_header")
			if n == 0 {
				testkit.Reply(w, 503, testkit.ErrorBody("SERVICE_UNAVAILABLE", "Busy."))
				return
			}
			testkit.Reply(w, 200, testkit.OK(`{"bid":`+testkit.BidJSON(testkit.BidCode)+`}`))
		})
		var meta core.ResponseMeta
		if _, err := bidsOn(testkit.Partner(t, s)).Get(ctx, testkit.BidCode, core.WithResponseInto(&meta)); err != nil {
			t.Fatal(err)
		}
		if meta.StatusCode != 200 || meta.RequestID != "req_test" || meta.Attempts != 2 || meta.Header.Get("X-Request-Id") != "req_header" {
			t.Errorf("meta %+v", meta)
		}
	})
	t.Run("the header's request id without an envelope", func(t *testing.T) {
		s := testkit.NewServer(t, func(w http.ResponseWriter, r *http.Request, n int) {
			w.Header().Set("X-Request-Id", "req_photo")
			_, _ = w.Write([]byte("jpeg"))
		})
		var meta core.ResponseMeta
		photo := caller.NewService[photos.Service](testkit.Partner(t, s))
		if _, err := photo.Get(ctx, "p1", core.WithResponseInto(&meta)); err != nil || meta.RequestID != "req_photo" {
			t.Errorf("meta %+v, %v", meta, err)
		}
	})
	t.Run("an error answer", func(t *testing.T) {
		s := testkit.NewServer(t, testkit.Always(404, testkit.ErrorBody("BID_NOT_FOUND", "No such bid.")))
		var meta core.ResponseMeta
		if _, err := bidsOn(testkit.Partner(t, s)).Get(ctx, testkit.BidCode, core.WithResponseInto(&meta)); !errors.Is(err, core.ErrNotFound) {
			t.Fatal(err)
		}
		if meta.StatusCode != 404 || meta.RequestID != "req_test" || meta.Attempts != 1 {
			t.Errorf("meta %+v", meta)
		}
	})
	t.Run("no answer", func(t *testing.T) {
		s := testkit.NewServer(t, testkit.Always(200, testkit.OK(`{}`)))
		c := testkit.Partner(t, s, noRetries, func(c *transport.Config) {
			c.HTTPClient = &http.Client{Transport: testkit.FailingTransport{Err: errors.New("refused")}}
		})
		meta := core.ResponseMeta{StatusCode: -1}
		if _, err := bidsOn(c).Get(ctx, testkit.BidCode, core.WithResponseInto(&meta)); err == nil || meta.StatusCode != -1 {
			t.Errorf("meta %+v, %v", meta, err)
		}
	})
}

func TestNilOptionsAreIgnored(t *testing.T) {
	s := testkit.NewServer(t, testkit.Always(200, testkit.OK(`{"bid":`+testkit.BidJSON(testkit.BidCode)+`}`)))
	if _, err := bidsOn(testkit.App(t, s)).Get(context.Background(), testkit.BidCode, nil); err != nil {
		t.Fatal(err)
	}
}

func int64Ptr(v int64) *int64    { return &v }
func stringPtr(v string) *string { return &v }
