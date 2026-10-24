package apijson

import (
	"bytes"
	"encoding/json"
	"net/http"
	"testing"
)

// The fuzz targets check that no input, however malformed, makes the decoder panic, refuse
// valid JSON, or lose the JSON a model came from.
//
//	go test -run '^$' -fuzz '^FuzzDecode$' -fuzztime 30s ./internal/apijson

var seeds = []string{
	``, `null`, `{}`, `[]`, `"text"`, `12`, `true`, `{"a":`, `{"vintage":"2015"}`, `{"vintage":2015.5}`,
	`{"vintage":1e400}`, `{"quantity":"9223372036854775808"}`, `{"price":"12.5"}`, `{"lines":{}}`,
	`{"lines":[null,1,"x",{}]}`, `{"line":{"id":7,"cost":"1.5","units":"2"}}`, `{"a":{"b":{"c":{"d":[[[[]]]]}}}}`,
	"{\"wine_name\":\"\xff\xfe\"}", `{"lwin18":"100000120151200750","wine_name":"Chateau Exemple","vintage":2015,
	"quantity":6,"price":1200.5,"market_price":null,"is_listed":true,"scopes":["bids:read"],"details":{"k":1},
	"line":{"id":"L1","cost":10,"units":1},"lines":[{"id":"L2"}],"last_trade":{"id":"L3"}}`,
	`{"status":"OK","meta":{"request_id":"r1","pagination":{"limit":1}},"data":{"bids":[{"id":"a"}]}}`,
	`{"errors":[{"code":"RATE_LIMIT_EXCEEDED","details":{"retry_after":7}}]}`,
}

// FuzzDecode decodes any input into a model: valid JSON always decodes and is kept for Raw, and
// a list of models never fails on its elements.
func FuzzDecode(f *testing.F) {
	for _, s := range seeds {
		f.Add([]byte(s))
	}
	f.Fuzz(func(t *testing.T, b []byte) {
		var w wine
		err := json.Unmarshal(b, &w)
		if json.Valid(b) {
			if err != nil {
				t.Fatalf("valid JSON refused: %v", err)
			}
			if !bytes.Equal(bytes.TrimSpace(b), []byte("null")) && len(w.Raw()) == 0 {
				t.Fatal("no Raw kept")
			}
		}
		var lines []line
		_ = json.Unmarshal(b, &lines)
		var v any
		_, _ = Lookup(b, "lwin18", &v)
	})
}

// FuzzNumbers checks the readers of integer and number fields against each other.
func FuzzNumbers(f *testing.F) {
	for _, s := range []string{`1000`, `1000.0`, `1e3`, `"1000"`, `1000.5`, `-0`, `9223372036854775807`,
		`9223372036854775808`, `-9223372036854775808`, `1e19`, `"12.5"`, `"x"`, `null`, `true`, `[1]`, `0x10`, `"1e400"`} {
		f.Add([]byte(s))
	}
	f.Fuzz(func(t *testing.T, b []byte) {
		i, iok := Int(b)
		v, fok := Float(b)
		if iok && (!fok || float64(i) != v) {
			t.Fatalf("%q: int %d, float %v (%v)", b, i, v, fok)
		}
	})
}

// FuzzEnvelope reads any answer as an envelope, as the SDK does before it decodes a model.
func FuzzEnvelope(f *testing.F) {
	for _, s := range seeds {
		f.Add([]byte(s), "bids")
	}
	f.Fuzz(func(t *testing.T, body []byte, key string) {
		_, fields := Split(body)
		raw, _ := Data(fields, key)
		if _, err := List[line](raw); err != nil && json.Valid(raw) && bytes.HasPrefix(bytes.TrimSpace(raw), []byte("[")) {
			t.Fatalf("a valid list %q was refused: %v", raw, err)
		}
		_ = RequestID(fields, http.Header{"X-Request-Id": {"r"}})
		// Split trims Unicode white space, wider than JSON's, so the answer is read as trimmed.
		if code, _ := FirstError(body); code != "" && !json.Valid(bytes.TrimSpace(body)) {
			t.Fatalf("an error code %q read from invalid JSON %q", code, body)
		}
	})
}
