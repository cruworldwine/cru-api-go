package apijson

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func decodeWine(t *testing.T, s string) *wine {
	t.Helper()
	var w wine
	if err := json.Unmarshal([]byte(s), &w); err != nil {
		t.Fatalf("decoding %s: %v", s, err)
	}
	return &w
}

func TestSplit(t *testing.T) {
	for in, want := range map[string]struct {
		raw    string
		fields int
	}{
		``: {"", -1}, `  `: {"", -1}, `null`: {"", -1}, ` {"a":1,"b":null} `: {`{"a":1,"b":null}`, 2},
		`{}`: {`{}`, 0}, `[1]`: {`[1]`, -1}, `"x"`: {`"x"`, -1}, `{bad`: {`{bad`, -1},
	} {
		raw, f := Split([]byte(in))
		if string(raw) != want.raw || (want.fields < 0) != (f == nil) || (f != nil && len(f) != want.fields) {
			t.Errorf("Split(%q) = %q, %v", in, raw, f)
		}
	}
	in := []byte(`{"a":1}`)
	raw, _ := Split(in)
	in[1] = 'X'
	if string(raw) != `{"a":1}` {
		t.Error("Split did not copy its input")
	}
}

func TestEveryKindOfField(t *testing.T) {
	w := decodeWine(t, `{"lwin18":"100000120151200750","wine_name":"Chateau Exemple","vintage":"2015","quantity":1000.0,
		"price":"1200.50","market_price":0,"is_listed":true,"scopes":["a"],"details":{"k":[1]},"anything":[1,"x"],
		"line":{"id":"L1","cost":null,"units":1e1,"future":"yes"},"lines":[{"id":"L2"},null],"last_trade":{"id":"L3"},
		"new_field":{"a":1}}`)
	if w.LWIN18 != "100000120151200750" || *w.Name != "Chateau Exemple" || *w.Vintage != 2015 || w.Quantity != 1000 ||
		w.Price != 1200.5 || w.Market == nil || *w.Market != 0 || !w.Listed || w.Scopes[0] != "a" || w.Details["k"] == nil ||
		w.Anything == nil {
		t.Errorf("%+v", w)
	}
	if w.Line.ID != "L1" || w.Line.Cost != nil || w.Line.Units != 10 || !strings.Contains(string(w.Line.Raw()), `"future":"yes"`) {
		t.Errorf("nested %+v", w.Line)
	}
	if len(w.Lines) != 2 || w.Lines[0].ID != "L2" || w.LastTrade == nil || w.LastTrade.ID != "L3" {
		t.Errorf("lines %+v, last trade %+v", w.Lines, w.LastTrade)
	}
	var extra map[string]int
	if found, err := Lookup(w.Raw(), "new_field", &extra); !found || err != nil || extra["a"] != 1 {
		t.Errorf("Lookup(new_field) = %v, %v, %v", found, err, extra)
	}
	if found, err := Lookup(w.Raw(), "absent", &extra); found || err != nil {
		t.Error("Lookup found an absent field")
	}
	var n int
	if found, err := Lookup(w.Raw(), "wine_name", &n); !found || err == nil {
		t.Error("Lookup into the wrong type did not fail")
	}
	if found, _ := Lookup(nil, "x", &n); found {
		t.Error("Lookup in nothing found something")
	}
}

func TestBadFieldsLeaveZero(t *testing.T) {
	w := decodeWine(t, `{"lwin18":42,"wine_name":["a"],"vintage":12.5,"quantity":"lots","price":{},"market_price":"x",
		"is_listed":"yes","scopes":"none","details":[],"line":[],"lines":{}}`)
	if w.LWIN18 != "" || w.Name != nil || w.Vintage != nil || w.Quantity != 0 || w.Price != 0 || w.Market != nil ||
		w.Listed || w.Scopes != nil || w.Details != nil || w.Lines != nil || w.Line.ID != "" {
		t.Errorf("%+v", w)
	}
	if string(w.Line.Raw()) != "[]" {
		t.Errorf("raw of a non-object: %s", w.Line.Raw())
	}
	before := wine{LWIN18: "kept"}
	if err := json.Unmarshal([]byte("null"), &before); err != nil || before.LWIN18 != "kept" {
		t.Error("decoding null must leave the model alone")
	}
}

func TestNumbers(t *testing.T) {
	ints := map[string]int64{`1000`: 1000, `1000.0`: 1000, `1e3`: 1000, `"1000"`: 1000, `-0`: 0,
		`9223372036854775807`: 9223372036854775807, `-9223372036854775808`: -9223372036854775808}
	for in, want := range ints {
		if got, ok := Int(json.RawMessage(in)); !ok || got != want {
			t.Errorf("Int(%s) = %d, %v", in, got, ok)
		}
	}
	for _, in := range []string{``, `1000.5`, `9223372036854775808`, `1e19`, `"x"`, `null`, `true`, `[1]`, `""`, `"1e400"`} {
		if got, ok := Int(json.RawMessage(in)); ok {
			t.Errorf("Int(%s) = %d, want none", in, got)
		}
	}
	for in, want := range map[string]float64{`12.5`: 12.5, `"12.5"`: 12.5, `1e3`: 1000, `-0.25`: -0.25} {
		if got, ok := Float(json.RawMessage(in)); !ok || got != want {
			t.Errorf("Float(%s) = %v, %v", in, got, ok)
		}
	}
	for _, in := range []string{``, `"x"`, `null`, `{}`, `"1e400"`} {
		if got, ok := Float(json.RawMessage(in)); ok {
			t.Errorf("Float(%s) = %v, want none", in, got)
		}
	}
}

func TestUnmarshalAndDecodeNew(t *testing.T) {
	var w wine
	if err := Unmarshal(nil, &w); err != nil || w.raw != nil {
		t.Errorf("Unmarshal of nothing: %v", err)
	}
	if err := Unmarshal(json.RawMessage(`{"lwin18":"1"}`), &w); err != nil || w.LWIN18 != "1" {
		t.Errorf("Unmarshal: %+v, %v", w, err)
	}
	if err := Unmarshal(json.RawMessage(`{`), &w); err == nil {
		t.Error("Unmarshal of invalid JSON did not fail")
	}
	got, ok := DecodeNew[line]([]byte(`{"id":"L1"}`)).(*line)
	if !ok || got.ID != "L1" {
		t.Errorf("DecodeNew = %+v", got)
	}
	if bad, ok := DecodeNew[line]([]byte(`{`)).(*line); !ok || bad.ID != "" {
		t.Errorf("DecodeNew of invalid JSON = %+v", bad)
	}
}

func TestEnvelope(t *testing.T) {
	_, f := Split([]byte(`{"status":"OK","meta":{"request_id":"req_meta"},"data":{"bids":[{"id":"a"}],"one":{"id":"b"}}}`))
	if data, ok := Data(f, ""); !ok || !strings.HasPrefix(string(data), `{"bids"`) {
		t.Errorf("Data = %s", data)
	}
	if one, ok := Data(f, "one"); !ok || string(one) != `{"id":"b"}` {
		t.Errorf("Data(one) = %s", one)
	}
	if _, ok := Data(f, "missing"); ok {
		t.Error("Data found a missing key")
	}
	raw, _ := Data(f, "bids")
	if items, err := List[line](raw); err != nil || len(items) != 1 || items[0].ID != "a" {
		t.Errorf("List = %+v, %v", items, err)
	}
	for _, empty := range []string{``, `null`, `{}`, `[]`} {
		if items, err := List[line](json.RawMessage(empty)); err != nil || len(items) != 0 {
			t.Errorf("List(%q) = %+v, %v", empty, items, err)
		}
	}
	for _, bad := range []string{`{"a":1}`, `"x"`, `3`} {
		if _, err := List[line](json.RawMessage(bad)); err == nil {
			t.Errorf("List(%q) did not fail", bad)
		}
	}
	header := http.Header{"X-Request-Id": {"req_header"}}
	if id := RequestID(f, header); id != "req_meta" {
		t.Errorf("RequestID = %q", id)
	}
	_, noMeta := Split([]byte(`{"meta":{},"data":null}`))
	if id := RequestID(noMeta, header); id != "req_header" {
		t.Errorf("RequestID without meta = %q", id)
	}
	if id := RequestID(nil, http.Header{}); id != "" {
		t.Errorf("RequestID of nothing = %q", id)
	}
}

func TestFirstError(t *testing.T) {
	code, details := FirstError([]byte(`{"errors":[{"code":"RATE_LIMIT_EXCEEDED","details":{"retry_after":7}},{"code":"NEXT"}]}`))
	if code != "RATE_LIMIT_EXCEEDED" || details["retry_after"] != 7.0 {
		t.Errorf("FirstError = %q, %v", code, details)
	}
	for _, body := range []string{``, `<html>`, `{"errors":[]}`, `{"errors":"x"}`, `{"errors":[null]}`} {
		if code, details := FirstError([]byte(body)); code != "" || details != nil {
			t.Errorf("FirstError(%q) = %q, %v", body, code, details)
		}
	}
}
