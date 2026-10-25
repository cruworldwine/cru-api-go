package testkit

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/cruworldwine/cru-api-go/cru/bids"
	"github.com/cruworldwine/cru-api-go/cru/core"
	"github.com/cruworldwine/cru-api-go/cru/warehouses"
	"github.com/cruworldwine/cru-api-go/internal/caller"
)

// TestInputValues covers the readers the generated sample tables call only for the parameter
// types the spec has at the time.
func TestInputValues(t *testing.T) {
	in := &Input{Query: query(t, `{"i":"7","f":1.5,"b":true,"s":"x,y"}`), PathValues: map[string]string{"code": "c", "id": "7"}}
	if in.IntValue("i") != 7 || in.IntValue("none") != 0 || in.FloatValue("f") != 1.5 || in.FloatValue("none") != 0 ||
		!in.BoolValue("b") || in.BoolValue("none") || in.StrValue("s") != "x,y" || len(in.List("s")) != 2 || in.Float("s") != nil ||
		in.Int("s") != nil || in.Bool("s") != nil || in.Str("none") != nil || in.List("none") != nil ||
		in.Path("code") != "c" || in.PathInt("id") != 7 {
		t.Error("an input was misread")
	}
}

func query(t *testing.T, s string) map[string]any {
	t.Helper()
	var q map[string]any
	if err := json.Unmarshal([]byte(s), &q); err != nil {
		t.Fatal(err)
	}
	return q
}

func endpoint(in *Input, method, template, key string, body any) (caller.Endpoint, error) {
	path, err := caller.ExpandPath(template, "code", in.Path("code"))
	return caller.Endpoint{Method: method, Path: path, Key: key, Body: body, Opts: in.Opts}, err
}

// TestEveryOperationHarness runs the harness the generated tests use over an operation of every
// kind.
func TestEveryOperationHarness(t *testing.T) {
	bid := BidJSON(BidCode)
	ops := []Op{
		{ID: "get", Method: "GET", Path: "/bids/{code}", Kind: "get", Key: "bid",
			Call: func(ctx context.Context, c *caller.Caller, in *Input) (any, error) {
				e, _ := endpoint(in, "GET", "/bids/{code}", "bid", nil)
				return caller.GetOne[bids.OwnBid](ctx, c, e)
			}},
		{ID: "list", Method: "GET", Path: "/warehouses", Kind: "list", Key: "warehouses",
			Call: func(ctx context.Context, c *caller.Caller, in *Input) (any, error) {
				return caller.GetList[warehouses.Warehouse](ctx, c, caller.Endpoint{Method: "GET", Path: "/warehouses", Key: "warehouses", Opts: in.Opts})
			}},
		{ID: "page", Method: "GET", Path: "/bids", Kind: "page", Key: "bids",
			Call: func(ctx context.Context, c *caller.Caller, in *Input) (any, error) {
				q := map[string][]string{"status": {strings.Join(in.List("status"), ",")}, "limit": {in.StrValue("limit")}}
				if in.Int("limit") == nil {
					return nil, errors.New("limit is not a number")
				}
				return caller.GetPage[bids.OwnBid](ctx, c, caller.Endpoint{Method: "GET", Path: "/bids", Query: q, Key: "bids", Opts: in.Opts})
			}},
		{ID: "write", Method: "PATCH", Path: "/bids/{code}", Kind: "write",
			Call: func(ctx context.Context, c *caller.Caller, in *Input) (any, error) {
				p := &bids.UpdateParams{}
				in.DecodeBody(p)
				e, _ := endpoint(in, "PATCH", "/bids/{code}", "", p)
				return caller.DoWrite[bids.Result](ctx, c, e)
			}},
		{ID: "confirm", Method: "POST", Path: "/confirmations/{code}", Kind: "confirm",
			Call: func(ctx context.Context, c *caller.Caller, in *Input) (any, error) {
				e, _ := endpoint(in, "POST", "/confirmations/{code}", "", caller.EmptyObject)
				return caller.WriteOf(ctx, c, e, func(data json.RawMessage) *json.RawMessage { return &data })
			},
			Unwrap: func(result any) (any, json.RawMessage, bool) {
				var r bids.Result
				raw := *result.(*json.RawMessage)
				return &r, raw, json.Unmarshal(raw, &r) == nil
			}},
		{ID: "empty", Method: "DELETE", Path: "/confirmations/{code}", Kind: "empty",
			Call: func(ctx context.Context, c *caller.Caller, in *Input) (any, error) {
				e, _ := endpoint(in, "DELETE", "/confirmations/{code}", "", nil)
				return nil, caller.DoEmpty(ctx, c, e)
			}},
		{ID: "binary", Method: "GET", Path: "/photos/{code}", Kind: "binary",
			Call: func(ctx context.Context, c *caller.Caller, in *Input) (any, error) {
				e, _ := endpoint(in, "GET", "/photos/{code}", "", nil)
				return caller.DoBinary(ctx, c, e)
			}},
	}
	code := map[string]string{"code": "a code/1"}
	EveryOperation(t, ops, map[string]Sample{
		"get":     {Path: code, Data: `{"bid":` + bid + `}`},
		"list":    {Data: `{"warehouses":[{"code":"UK_TEST","name":"Test Bond One","address":{},"sells_your_inbond_wine":true,"currency":"GBP"}]}`},
		"page":    {Query: `{"status":"live,matched","limit":"7"}`, Data: `{"bids":[` + bid + `]}`},
		"write":   {Path: code, Body: `{"reference":"ref-1","price":12.5}`, Data: BidResultJSON("bid.update")},
		"confirm": {Path: code, Data: BidResultJSON("bid.place")},
		"empty":   {Path: code},
		"binary":  {Path: code},
	})
}

func TestCheckAnswerProblems(t *testing.T) {
	model := &bids.OwnBid{}
	for _, tc := range []struct {
		name   string
		op     Op
		status int
		res    any
		err    error
	}{
		{"an error", Op{Kind: "get"}, 200, nil, errors.New("refused")},
		{"no model", Op{Kind: "get"}, 200, "not a model", nil},
		{"a short list", Op{Kind: "list", Key: "bids"}, 200, []bids.OwnBid{}, nil},
		{"a staged answer", Op{Kind: "write"}, 202, &core.Write[bids.Result]{}, nil},
		{"no result", Op{Kind: "write"}, 200, &core.Write[bids.Result]{}, nil},
		{"an action not decoded", Op{Kind: "confirm", Unwrap: func(any) (any, json.RawMessage, bool) { return nil, nil, false }},
			200, &core.Write[bids.Result]{Result: &bids.Result{}}, nil},
		{"an unknown kind", Op{Kind: "stream"}, 200, model, nil},
	} {
		if got := checkAnswer(&tc.op, tc.status, OK(`{"bids":[{}]}`), tc.res, tc.err); len(got) == 0 {
			t.Errorf("%s: no problem reported", tc.name)
		}
	}
	if got := checkAnswer(&Op{Kind: "empty"}, 204, "", nil, nil); got != nil {
		t.Errorf("an empty answer: %v", got)
	}
}

func TestLossless(t *testing.T) {
	for _, tc := range []struct {
		typed any
		raw   string
		want  string
	}{
		{map[string]any{"a": 1}, `[1]`, "x: not an object"},
		{map[string]any{"a": []int{1, 2}}, `{"a":[1]}`, "x.a: list differs"},
		{map[string]any{"a": 1}, `{}`, "x.a: required but absent"},
		{map[string]any{"a": nil}, `{}`, ""},
		{map[string]any{"a": 1}, `{"a":2}`, "x.a: value lost in decoding"},
		{func() {}, `{}`, "x: json: unsupported type: func()"},
	} {
		if got := strings.Join(Lossless("x", tc.typed, json.RawMessage(tc.raw)), "; "); got != tc.want {
			t.Errorf("%v against %s: %q, want %q", tc.typed, tc.raw, got, tc.want)
		}
	}
}

// TestDecodes runs the check every package's FuzzDecodeModels makes over its seeds.
func TestDecodes(t *testing.T) {
	for _, s := range append(OddJSON, BidJSON(BidCode)) {
		Decodes[bids.OwnBid](t, []byte(s))
	}
}

func TestServerHelpers(t *testing.T) {
	s := NewServer(t, func(w http.ResponseWriter, r *http.Request, n int) {
		Always(418, ErrorBody("TEAPOT", "No."))(w, r, n)
	})
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, s.BaseURL()+"/x?y=1", strings.NewReader(`{"a":1}`))
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	got := s.Requests()
	if resp.StatusCode != 418 || len(got) != 1 || got[0].Path != "/x" || got[0].Query.Get("y") != "1" || MustJSON(t, got[0].Body)["a"] != 1.0 {
		t.Errorf("%d, %+v", resp.StatusCode, got)
	}
	req, err = http.NewRequestWithContext(context.Background(), http.MethodPost, s.BaseURL()+"/x", strings.NewReader("{}"))
	if err != nil {
		t.Fatal(err)
	}
	if resp, err := (&http.Client{Transport: FailingTransport{Err: errors.New("down")}}).Do(req); err == nil {
		_ = resp.Body.Close()
		t.Error("FailingTransport answered")
	}
	staged := MustJSON(t, StagedJSON("bid.place"))
	if staged["confirmation"].(map[string]any)["code"] != Cnf {
		t.Errorf("staged %v", staged)
	}
}
