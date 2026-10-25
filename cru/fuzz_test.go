package cru

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"testing"

	"github.com/cruworldwine/cru-api-go/cru/bids"
	"github.com/cruworldwine/cru-api-go/internal/testkit"
)

// FuzzResponse here drives the whole response path of a Client; each package's
// FuzzDecodeModels, in its sample_gen_test.go, its models, and internal/apijson the decoder.
//
//	go test -run '^$' -fuzz '^FuzzResponse$' -fuzztime 30s ./cru

// staticTransport answers every request with the same status and body, in memory.
type staticTransport struct {
	status int
	body   []byte
}

func (s staticTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.Body != nil {
		_ = r.Body.Close()
	}
	return &http.Response{
		StatusCode: s.status, Header: http.Header{"Content-Type": {"application/json"}, "Retry-After": {"1"}},
		Body: io.NopCloser(bytes.NewReader(s.body)), Request: r,
	}, nil
}

var fuzzStatuses = []int{200, 201, 202, 204, 400, 401, 404, 409, 410, 422, 429, 500, 503}

// FuzzResponse sends any body, with any of the API's statuses, through the whole response path
// of each kind of method: a page, one model, a list, a write, a confirmation and Client.Do.
func FuzzResponse(f *testing.F) {
	for i, s := range testkit.OddJSON {
		f.Add(uint8(i), []byte(s))
	}
	for _, data := range []string{
		`{"bids":[` + testkit.BidJSON(testkit.BidCode) + `]}`, `{"bid":` + testkit.BidJSON(testkit.BidCode) + `}`,
		`{"warehouses":[{"code":"UK_TEST","name":"Test Bond One"}]}`, testkit.BidResultJSON("bid.place"),
		testkit.StagedJSON("bid.place"), `{"pong":true,"client":{"market":"uk"}}`,
	} {
		f.Add(uint8(0), []byte(testkit.OK(data)))
		f.Add(uint8(2), []byte(testkit.OK(data)))
	}
	f.Add(uint8(4), []byte(testkit.ErrorBody("VALIDATION_INVALID_VALUE", "No.")))
	f.Fuzz(func(t *testing.T, which uint8, body []byte) {
		status := fuzzStatuses[int(which)%len(fuzzStatuses)]
		c, err := NewClient(WithClientKey("ck", "cs"), WithMaxRetries(0),
			WithHTTPClient(&http.Client{Transport: staticTransport{status, body}}))
		if err != nil {
			t.Fatal(err)
		}
		ctx := context.Background()
		check := func(err error) {
			var apiErr *APIError
			if err == nil || errors.Is(err, ErrUnexpectedResponse) {
				return
			}
			if !errors.As(err, &apiErr) || apiErr.StatusCode != status || apiErr.Error() == "" {
				t.Fatalf("HTTP %d: %v", status, err)
			}
		}
		page, err := c.Portfolio.List(ctx, nil)
		check(err)
		if err == nil && page == nil {
			t.Fatal("no page and no error")
		}
		_, err = c.Bids.Get(ctx, testkit.BidCode)
		check(err)
		_, err = c.Warehouses.List(ctx)
		check(err)
		w, err := c.Bids.Place(ctx, &bids.PlaceParams{LWIN18: testkit.LWIN, Quantity: 1, Price: 1})
		check(err)
		if err == nil && w.Staged && w.Confirmation == nil {
			t.Fatal("staged without a confirmation")
		}
		done, err := c.Confirmations.Confirm(ctx, testkit.Cnf)
		check(err)
		if err == nil && !done.Staged && (done.Result == nil || done.Result.Value == nil) {
			t.Fatal("confirmed without an action result")
		}
		_, err = c.Do(ctx, "GET", "/anything", nil, nil)
		check(err)
	})
}
