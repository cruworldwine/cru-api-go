package cru

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestWithRequestHeader(t *testing.T) {
	s := newServer(t, always(200, ok(`{"bid":`+bidJSON(testBidCode)+`}`)))
	c, _ := partner(t, s)
	ctx := context.Background()
	if _, err := c.Bids.Get(ctx, testBidCode, WithRequestHeader("traceparent", "00-abc-01"), WithRequestHeader("User-Agent", "mine")); err != nil {
		t.Fatal(err)
	}
	h := s.requests()[0].Header
	if h.Get("Traceparent") != "00-abc-01" || h.Get("User-Agent") != "mine" || h.Get("Client-Key") != "ck_test" {
		t.Errorf("headers %v", h)
	}
	for _, name := range []string{"Authorization", "client-key", "CLIENT-SECRET"} {
		_, err := c.Bids.Get(ctx, testBidCode, WithRequestHeader(name, "stolen"))
		if err == nil || !strings.Contains(err.Error(), "credentials") {
			t.Errorf("%s: %v", name, err)
		}
	}
	for _, h := range [][2]string{{"", "v"}, {"Bad Name", "v"}, {"X-Ok", "line\r\nX-Injected: 1"}, {"X-Ok", "nul\x00"}} {
		if _, err := c.Bids.Get(ctx, testBidCode, WithRequestHeader(h[0], h[1])); err == nil {
			t.Errorf("header %q: %q was sent", h[0], h[1])
		}
	}
	if len(s.requests()) != 1 {
		t.Errorf("a refused header still sent %d requests", len(s.requests())-1)
	}
}

func TestWithRequestTimeout(t *testing.T) {
	s := newServer(t, func(w http.ResponseWriter, r *http.Request, n int) {
		if n == 0 {
			<-r.Context().Done()
			return
		}
		reply(w, 200, ok(`{"bid":`+bidJSON(testBidCode)+`}`))
	})
	c, _ := partner(t, s, WithTimeout(time.Hour))
	_, err := c.Bids.Get(context.Background(), testBidCode, WithRequestTimeout(20*time.Millisecond), WithRequestMaxRetries(0))
	var ce *ConnectionError
	if !errors.As(err, &ce) || !ce.Timeout() || len(s.requests()) != 1 {
		t.Fatalf("want one timed-out attempt, got %v after %d", err, len(s.requests()))
	}
	if _, err := c.Bids.Get(context.Background(), testBidCode, WithRequestTimeout(-time.Second)); err != nil {
		t.Errorf("a negative timeout is no timeout: %v", err)
	}
}

func TestWithRequestMaxRetries(t *testing.T) {
	for _, tc := range []struct {
		n, want int
	}{{0, 1}, {-3, 1}, {4, 5}} {
		s := newServer(t, always(503, apiError("SERVICE_UNAVAILABLE", "Busy.")))
		c, _ := partner(t, s)
		_, err := c.Bids.Get(context.Background(), testBidCode, WithRequestMaxRetries(tc.n))
		if !errors.Is(err, ErrServer) || len(s.requests()) != tc.want {
			t.Errorf("WithRequestMaxRetries(%d): %v after %d requests, want %d", tc.n, err, len(s.requests()), tc.want)
		}
	}
	s := newServer(t, always(503, apiError("SERVICE_UNAVAILABLE", "Busy.")))
	c, _ := app(t, s)
	_, _ = c.Bids.Place(context.Background(), &BidsPlaceParams{LWIN18: testLWIN, Quantity: 1, Price: 1}, WithRequestMaxRetries(5))
	if len(s.requests()) != 1 {
		t.Errorf("a connected app's write was retried: %d requests", len(s.requests()))
	}
}

func TestWithExtraQuery(t *testing.T) {
	s := newServer(t, func(w http.ResponseWriter, r *http.Request, n int) {
		reply(w, 200, okPaged(`{"bids":[`+bidJSON(fmt.Sprintf("BID-%d", n))+`]}`, fmt.Sprintf(`{"limit":1,"offset":%d,"has_more":%t}`, n, n == 0)))
	})
	c, _ := partner(t, s)
	ctx := context.Background()
	page, err := c.Bids.List(ctx, &BidsListParams{Limit: Int(1), ClientRef: String("ref")},
		WithExtraQuery("limit", "9"), WithExtraQuery("new_filter", "a b"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := page.Next(ctx); err != nil {
		t.Fatal(err)
	}
	for i, r := range s.requests() {
		if r.Query.Get("limit") != "9" || r.Query.Get("new_filter") != "a b" || r.Query.Get("client_ref") != "ref" {
			t.Errorf("request %d: query %v", i, r.Query)
		}
	}
	if got := s.requests()[1].Query.Get("offset"); got != "1" {
		t.Errorf("the next page's offset %q", got)
	}
}

func TestWithExtraBodyField(t *testing.T) {
	s := newServer(t, func(w http.ResponseWriter, r *http.Request, n int) {
		if r.Method == http.MethodDelete {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		reply(w, 200, ok(bidResultJSON("bid.place")))
	})
	ctx := context.Background()
	c, _ := partner(t, s)

	_, err := c.Bids.Place(ctx, &BidsPlaceParams{LWIN18: testLWIN, Quantity: 1, Price: 10},
		WithExtraBodyField("price", 12.5), WithExtraBodyField("new_field", map[string]any{"a": []int{1}}))
	if err != nil {
		t.Fatal(err)
	}
	body := mustJSON(t, s.requests()[0].Body)
	if body["price"] != 12.5 || body["lwin18"] != testLWIN || !strings.HasPrefix(body["reference"].(string), "sdk_") {
		t.Errorf("body %v", body)
	}
	if nf, _ := body["new_field"].(map[string]any); nf == nil || len(nf["a"].([]any)) != 1 {
		t.Errorf("new_field %v", body["new_field"])
	}

	if _, err := c.Bids.Cancel(ctx, testBidCode, nil, WithExtraBodyField("reference", "mine-1")); err != nil {
		t.Fatal(err)
	}
	if body := mustJSON(t, s.requests()[1].Body); body["reference"] != "mine-1" {
		t.Errorf("an extra reference was not used: %v", body)
	}

	a, _ := app(t, s)
	if err := a.Confirmations.Cancel(ctx, testCnf, WithExtraBodyField("reason", "changed my mind")); err != nil {
		t.Fatal(err)
	}
	if got := s.requests()[2].Body; got != `{"reason":"changed my mind"}` {
		t.Errorf("a write without a body sent %s", got)
	}

	sent := len(s.requests())
	for name, call := range map[string]func() error{
		"a read": func() error {
			_, err := c.Bids.Get(ctx, testBidCode, WithExtraBodyField("x", 1))
			return err
		},
		"an unencodable value": func() error {
			_, err := c.Bids.Cancel(ctx, testBidCode, nil, WithExtraBodyField("x", func() {}))
			return err
		},
		"a body that is not an object": func() error {
			_, err := c.Do(ctx, "POST", "/things", nil, []byte(`[1,2]`), WithExtraBodyField("x", 1))
			return err
		},
	} {
		if err := call(); err == nil {
			t.Errorf("%s: want an error", name)
		}
	}
	if len(s.requests()) != sent {
		t.Errorf("a refused call was sent")
	}
}

func TestWithResponseInto(t *testing.T) {
	ctx := context.Background()
	t.Run("success, after a retry", func(t *testing.T) {
		s := newServer(t, func(w http.ResponseWriter, r *http.Request, n int) {
			w.Header().Set("X-Request-Id", "req_header")
			if n == 0 {
				reply(w, 503, apiError("SERVICE_UNAVAILABLE", "Busy."))
				return
			}
			reply(w, 200, ok(`{"bid":`+bidJSON(testBidCode)+`}`))
		})
		c, _ := partner(t, s)
		var meta ResponseMeta
		if _, err := c.Bids.Get(ctx, testBidCode, WithResponseInto(&meta)); err != nil {
			t.Fatal(err)
		}
		if meta.StatusCode != 200 || meta.RequestID != "req_test" || meta.Attempts != 2 || meta.Header.Get("X-Request-Id") != "req_header" {
			t.Errorf("meta %+v", meta)
		}
	})
	t.Run("the header's request id without an envelope", func(t *testing.T) {
		s := newServer(t, func(w http.ResponseWriter, r *http.Request, n int) {
			w.Header().Set("X-Request-Id", "req_photo")
			_, _ = w.Write([]byte("jpeg"))
		})
		c, _ := partner(t, s)
		var meta ResponseMeta
		if _, err := c.Photos.Get(ctx, "p1", WithResponseInto(&meta)); err != nil || meta.RequestID != "req_photo" {
			t.Errorf("meta %+v, %v", meta, err)
		}
	})
	t.Run("an error answer", func(t *testing.T) {
		s := newServer(t, always(404, apiError("BID_NOT_FOUND", "No such bid.")))
		c, _ := partner(t, s)
		var meta ResponseMeta
		if _, err := c.Bids.Get(ctx, testBidCode, WithResponseInto(&meta)); !errors.Is(err, ErrNotFound) {
			t.Fatal(err)
		}
		if meta.StatusCode != 404 || meta.RequestID != "req_test" || meta.Attempts != 1 {
			t.Errorf("meta %+v", meta)
		}
	})
	t.Run("no answer", func(t *testing.T) {
		s := newServer(t, always(200, ok(`{}`)))
		c, _ := partner(t, s, WithHTTPClient(&http.Client{Transport: &flakyTransport{n: 9, err: errors.New("refused")}}), WithMaxRetries(0))
		meta := ResponseMeta{StatusCode: -1}
		if _, err := c.Bids.Get(ctx, testBidCode, WithResponseInto(&meta)); err == nil || meta.StatusCode != -1 {
			t.Errorf("meta %+v, %v", meta, err)
		}
	})
}

func TestNilOptionsAreIgnored(t *testing.T) {
	s := newServer(t, always(200, ok(`{"bid":`+bidJSON(testBidCode)+`}`)))
	c, err := NewClient(nil, WithAccessToken("at"), WithBaseURL(s.URL+"/api/v1"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Bids.Get(context.Background(), testBidCode, nil); err != nil {
		t.Fatal(err)
	}
}
