package cru

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"testing"
)

// The fuzz targets check that no answer, however malformed, makes the tolerant decoder panic or
// fail a field it cannot read. Seeds are the invented sample answers and a few odd shapes.
//
//	go test -run '^$' -fuzz '^FuzzDecodeModels$' -fuzztime 30s

var oddJSON = []string{
	``, `null`, `{}`, `[]`, `"text"`, `12`, `true`, `{"a":`, `{"vintage":"2015"}`, `{"vintage":2015.5}`,
	`{"vintage":1e400}`, `{"quantity":"9223372036854775808"}`, `{"price":"12.5"}`, `{"bids":{}}`,
	`{"bids":[null,1,"x",{}]}`, `{"data":{"bids":"no"}}`, `{"meta":{"pagination":{"limit":"x","total":-1}}}`,
	`{"errors":[{"code":12,"details":[]}]}`, `{"a":{"b":{"c":{"d":[[[[]]]]}}}}`, "{\"wine_name\":\"\xff\xfe\"}",
}

func addSeeds(f *testing.F) {
	for _, s := range oddJSON {
		f.Add([]byte(s))
	}
	for _, sample := range sampleCalls {
		if sample.data != "" {
			f.Add([]byte(sample.data))
		}
	}
}

// decodes checks one model: valid JSON always decodes, and the model keeps it for Raw.
func decodes[T any, PT interface {
	*T
	Model
}](t *testing.T, b []byte) {
	v := PT(new(T))
	err := json.Unmarshal(b, v)
	trimmed := bytes.TrimSpace(b)
	if json.Valid(b) {
		if err != nil {
			t.Fatalf("%T refused valid JSON: %v", v, err)
		}
		if !bytes.Equal(trimmed, []byte("null")) && len(v.Raw()) == 0 {
			t.Fatalf("%T kept no Raw", v)
		}
	}
	var list []T
	_ = json.Unmarshal(b, &list)
}

func FuzzDecodeModels(f *testing.F) {
	addSeeds(f)
	f.Fuzz(func(t *testing.T, b []byte) {
		decodes[PortfolioWine](t, b)
		decodes[MarketWine](t, b)
		decodes[MarketOfferDetail](t, b)
		decodes[OwnBid](t, b)
		decodes[BidResult](t, b)
		decodes[Purchase](t, b)
		decodes[Sale](t, b)
		decodes[Confirmation](t, b)
		decodes[StagedConfirmation](t, b)
		decodes[Ping](t, b)
		decodes[Meta](t, b)
		decodes[ErrorEntry](t, b)
		r := newActionResult(b)
		if r == nil || r.Value == nil {
			t.Fatal("no action result")
		}
	})
}

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
	for i, s := range oddJSON {
		f.Add(uint8(i), []byte(s))
	}
	for _, sample := range sampleCalls {
		if sample.data != "" {
			f.Add(uint8(0), []byte(ok(sample.data)))
			f.Add(uint8(2), []byte(ok(sample.data)))
		}
	}
	f.Add(uint8(4), []byte(apiError("VALIDATION_INVALID_VALUE", "No.")))
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
		_, err = c.Bids.Get(ctx, testBidCode)
		check(err)
		_, err = c.Warehouses.List(ctx)
		check(err)
		w, err := c.Bids.Place(ctx, &BidsPlaceParams{LWIN18: testLWIN, Quantity: 1, Price: 1})
		check(err)
		if err == nil && w.Staged && w.Confirmation == nil {
			t.Fatal("staged without a confirmation")
		}
		_, err = c.Confirmations.Confirm(ctx, testCnf)
		check(err)
		_, err = c.Do(ctx, "GET", "/anything", nil, nil)
		check(err)
	})
}

// FuzzNumbers checks the readers of integer and number fields against each other.
func FuzzNumbers(f *testing.F) {
	for _, s := range []string{`1000`, `1000.0`, `1e3`, `"1000"`, `1000.5`, `-0`, `9223372036854775807`,
		`9223372036854775808`, `-9223372036854775808`, `1e19`, `"12.5"`, `"x"`, `null`, `true`, `[1]`, `0x10`, `"1e400"`} {
		f.Add([]byte(s))
	}
	f.Fuzz(func(t *testing.T, b []byte) {
		i, iok := intOf(b)
		v, fok := floatOf(b)
		if iok && (!fok || float64(i) != v) {
			t.Fatalf("%q: int %d, float %v (%v)", b, i, v, fok)
		}
	})
}

// FuzzRequestBody checks that a body given to Client.Do, with a reference added and extra
// fields set, is either refused or sent as a valid JSON object holding both.
func FuzzRequestBody(f *testing.F) {
	for _, s := range []string{`{}`, `{"a":1}`, `{"reference":null}`, `{"reference":"mine"}`, ` {"a":[1,{"b":2}]} `,
		`[1]`, `null`, ``, `{"a":1}x`, `{"reference":1,"reference":null}`, `{"reference":null}`} {
		f.Add([]byte(s))
	}
	f.Fuzz(func(t *testing.T, payload []byte) {
		out, err := withReference(payload)
		if err == nil && len(bytes.TrimSpace(out)) > 0 && bytes.TrimSpace(out)[0] == '{' {
			var fields map[string]json.RawMessage
			if json.Unmarshal(out, &fields) != nil {
				t.Fatalf("%q became invalid JSON %q", payload, out)
			}
			if ref, ok := fields["reference"]; !ok || string(ref) == "null" {
				t.Fatalf("%q has no reference in %q", payload, out)
			}
		}
		withExtra, err := withFields(payload, []bodyField{{"extra", []int{1}}})
		if err != nil {
			return
		}
		var fields map[string]json.RawMessage
		if json.Unmarshal(withExtra, &fields) != nil || string(fields["extra"]) != "[1]" {
			t.Fatalf("%q with an extra field became %q", payload, withExtra)
		}
	})
}
