package transport

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"
)

var referencePattern = regexp.MustCompile(`^sdk_[0-9a-f]{32}$`)

func TestCredentialHeaders(t *testing.T) {
	s := newServer(t, always(200, ok(`{}`)))
	p, _ := partner(t, s)
	if _, err := get(p); err != nil {
		t.Fatal(err)
	}
	h := s.requests()[0].Header
	if h.Get("Client-Key") != "ck_test" || h.Get("Client-Secret") != "cs_test" || h.Get("Authorization") != "" {
		t.Errorf("partner headers %v", h)
	}
	if h.Get("User-Agent") != "cruworldwine-go/test" || h.Get("Accept") != "application/json" || h.Get("Content-Type") != "" {
		t.Errorf("User-Agent %q, Accept %q, Content-Type %q", h.Get("User-Agent"), h.Get("Accept"), h.Get("Content-Type"))
	}
	if !p.Partner() || p.BaseURL() != s.URL+"/api/v1" {
		t.Errorf("Partner %v, BaseURL %s", p.Partner(), p.BaseURL())
	}

	a, _ := app(t, s)
	if _, err := a.Do(context.Background(), Request{Method: http.MethodGet, Path: "/photos/1", Accept: "*/*"}, nil); err != nil {
		t.Fatal(err)
	}
	h = s.requests()[1].Header
	if h.Get("Authorization") != "Bearer at_test" || h.Get("Client-Key") != "" || h.Get("Accept") != "*/*" || a.Partner() {
		t.Errorf("connected app headers %v", h)
	}
}

// The spec spells the partner headers CLIENT-KEY and CLIENT-SECRET; they go on the wire so.
func TestCredentialHeaderSpelling(t *testing.T) {
	c, err := New(Config{BaseURL: "https://api.example.test/api/v1", Partner: true, ClientKey: "ck", ClientSecret: "cs"})
	if err != nil {
		t.Fatal(err)
	}
	h := c.headers(&prepared{accept: "application/json"}, "")
	if got := h["CLIENT-KEY"]; len(got) != 1 || got[0] != "ck" { //nolint:staticcheck // the wire spelling, on purpose
		t.Errorf("CLIENT-KEY = %v", got)
	}
	if got := h["CLIENT-SECRET"]; len(got) != 1 || got[0] != "cs" { //nolint:staticcheck // the wire spelling, on purpose
		t.Errorf("CLIENT-SECRET = %v", got)
	}
}

func TestNewRefusesARelativeBaseURL(t *testing.T) {
	for _, base := range []string{"", "not a url", "/api/v1", "http://[::1"} {
		if _, err := New(Config{BaseURL: base}); err == nil {
			t.Errorf("%q: want an error", base)
		}
	}
	c, err := New(Config{BaseURL: "http://localhost:8080/api/v1/"})
	if err != nil || c.BaseURL() != "http://localhost:8080/api/v1" || c.tokenURL != "http://localhost:8080/api/oauth/token" {
		t.Errorf("base %s, token %s, %v", c.BaseURL(), c.tokenURL, err)
	}
}

func TestQueryAndPath(t *testing.T) {
	s := newServer(t, always(200, ok(`{}`)))
	c, _ := partner(t, s)
	ctx := context.Background()
	_, err := c.Do(ctx, Request{Method: http.MethodGet, Path: "/things?kind=new", Query: url.Values{"limit": {"5"}}},
		options(func(o *Options) { o.SetQuery("limit", "9"); o.SetQuery("extra", "a b") }))
	if err != nil {
		t.Fatal(err)
	}
	q := s.requests()[0].Query
	if q.Get("kind") != "new" || q.Get("limit") != "9" || q.Get("extra") != "a b" || len(q) != 3 {
		t.Errorf("query %v", q)
	}
}

func TestPartnerWriteGetsReference(t *testing.T) {
	s := newServer(t, always(201, ok(`{}`)))
	c, _ := partner(t, s)
	body := map[string]any{"lwin18": "100000120151200750"}
	for range 2 {
		if _, err := post(c, body); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := post(c, map[string]any{"reference": "my-ref-1"}); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Do(context.Background(), Request{Method: http.MethodDelete, Path: testPath}, nil); err != nil {
		t.Fatal(err)
	}
	reqs := s.requests()
	first, second := mustJSON(t, reqs[0].Body), mustJSON(t, reqs[1].Body)
	if !referencePattern.MatchString(first["reference"].(string)) || first["reference"] == second["reference"] {
		t.Errorf("references %v, %v", first["reference"], second["reference"])
	}
	if _, changed := body["reference"]; changed || !strings.HasPrefix(reqs[0].Body, `{"reference":"sdk_`) {
		t.Errorf("body %s; the caller's body changed: %v", reqs[0].Body, changed)
	}
	if got := mustJSON(t, reqs[2].Body)["reference"]; got != "my-ref-1" {
		t.Errorf("the caller's reference became %v", got)
	}
	if del := mustJSON(t, reqs[3].Body); len(del) != 1 || reqs[3].Header.Get("Content-Type") != "application/json" {
		t.Errorf("a write without a body sent %s", reqs[3].Body)
	}

	a, _ := app(t, s)
	if _, err := a.Do(context.Background(), Request{Method: http.MethodDelete, Path: testPath}, nil); err != nil || s.requests()[4].Body != "" {
		t.Errorf("a connected app's bodiless write sent %q, %v", s.requests()[4].Body, err)
	}
}

func TestWithReference(t *testing.T) {
	for in, want := range map[string]string{
		``:                  `{"reference":"*"}`,
		`null`:              `{"reference":"*"}`,
		`{}`:                `{"reference":"*"}`,
		` {"a":1} `:         `{"reference":"*","a":1}`,
		`{"reference":"r"}`: `{"reference":"r"}`,
		`[1,2]`:             `[1,2]`,
	} {
		got, err := withReference([]byte(in))
		if err != nil {
			t.Fatal(err)
		}
		norm := regexp.MustCompile(`sdk_[0-9a-f]{32}`).ReplaceAllString(string(got), "*")
		if norm != want {
			t.Errorf("withReference(%q) = %s, want %s", in, got, want)
		}
	}
	got, _ := withReference([]byte(`{"reference":null,"b":2}`))
	var v map[string]any
	if json.Unmarshal(got, &v) != nil || !referencePattern.MatchString(v["reference"].(string)) || v["b"] != 2.0 {
		t.Errorf("a null reference became %s", got)
	}
	if _, err := withReference([]byte(`{bad`)); err == nil {
		t.Error("invalid JSON: want an error")
	}
}

func TestBodyEncoding(t *testing.T) {
	raw := json.RawMessage(`{"x":1}`)
	for _, tc := range []struct {
		in   any
		want string
	}{{nil, ""}, {raw, `{"x":1}`}, {[]byte(`[1]`), `[1]`}, {map[string]int{"a": 1}, `{"a":1}`}} {
		got, err := encodeBody(tc.in)
		if err != nil || string(got) != tc.want {
			t.Errorf("encodeBody(%v) = %s, %v", tc.in, got, err)
		}
	}
	if _, err := encodeBody(make(chan int)); err == nil {
		t.Error("an unencodable body: want an error")
	}
}

func TestExtraBodyFields(t *testing.T) {
	s := newServer(t, always(200, ok(`{}`)))
	p, _ := partner(t, s)
	_, err := post(p, map[string]any{"price": 10, "lwin18": "1"}, func(o *Options) {
		o.SetBodyField("price", 12.5)
		o.SetBodyField("new_field", map[string]any{"a": []int{1}})
	})
	if err != nil {
		t.Fatal(err)
	}
	body := mustJSON(t, s.requests()[0].Body)
	if body["price"] != 12.5 || body["lwin18"] != "1" || !referencePattern.MatchString(body["reference"].(string)) || body["new_field"] == nil {
		t.Errorf("body %v", body)
	}
	if _, err := post(p, nil, func(o *Options) { o.SetBodyField("reference", "mine-1") }); err != nil {
		t.Fatal(err)
	}
	if got := mustJSON(t, s.requests()[1].Body)["reference"]; got != "mine-1" {
		t.Errorf("an extra reference was not used: %v", got)
	}
	a, _ := app(t, s)
	if _, err := a.Do(context.Background(), Request{Method: http.MethodDelete, Path: "/confirmations/cnf"},
		options(func(o *Options) { o.SetBodyField("reason", "changed my mind") })); err != nil {
		t.Fatal(err)
	}
	if got := s.requests()[2].Body; got != `{"reason":"changed my mind"}` {
		t.Errorf("a write without a body sent %s", got)
	}

	sent := len(s.requests())
	for name, call := range map[string]func() error{
		"a read": func() error {
			_, err := get(p, func(o *Options) { o.SetBodyField("x", 1) })
			return err
		},
		"an unencodable value": func() error {
			_, err := post(p, nil, func(o *Options) { o.SetBodyField("x", func() {}) })
			return err
		},
		"a body that is not an object": func() error {
			_, err := post(p, []byte(`[1,2]`), func(o *Options) { o.SetBodyField("x", 1) })
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

func TestOptionHeaders(t *testing.T) {
	s := newServer(t, always(200, ok(`{}`)))
	c, _ := partner(t, s)
	if _, err := get(c, func(o *Options) {
		o.SetHeader("traceparent", "00-abc-01")
		o.SetHeader("User-Agent", "mine")
	}); err != nil {
		t.Fatal(err)
	}
	h := s.requests()[0].Header
	if h.Get("Traceparent") != "00-abc-01" || h.Get("User-Agent") != "mine" || h.Get("Client-Key") != "ck_test" {
		t.Errorf("headers %v", h)
	}
	for _, kv := range [][2]string{
		{"Authorization", "stolen"}, {"client-key", "stolen"}, {"CLIENT-SECRET", "stolen"},
		{"", "v"}, {"Bad Name", "v"}, {"X-Ok", "line\r\nX-Injected: 1"}, {"X-Ok", "nul\x00"},
	} {
		if _, err := get(c, func(o *Options) { o.SetHeader(kv[0], kv[1]) }); err == nil {
			t.Errorf("header %q: %q was sent", kv[0], kv[1])
		}
	}
	if len(s.requests()) != 1 {
		t.Errorf("a refused header still sent %d requests", len(s.requests())-1)
	}
}

func TestOptionOverrides(t *testing.T) {
	s := newServer(t, always(503, apiError("SERVICE_UNAVAILABLE", "Busy.")))
	c, _ := partner(t, s)
	for _, tc := range []struct{ n, want int }{{0, 1}, {-3, 1}, {4, 5}} {
		before := len(s.requests())
		_, err := get(c, func(o *Options) { o.SetMaxRetries(tc.n) })
		if statusOf(err) != 503 || len(s.requests())-before != tc.want {
			t.Errorf("SetMaxRetries(%d): %d requests, want %d", tc.n, len(s.requests())-before, tc.want)
		}
	}
	o := &Options{}
	o.SetTimeout(-time.Second)
	if *o.Timeout != 0 {
		t.Errorf("a negative timeout became %v", *o.Timeout)
	}
}

func TestObserveSeesEveryAnswer(t *testing.T) {
	s := newServer(t, failing(1, 503, apiError("SERVICE_UNAVAILABLE", "Busy."), ok(`{}`)))
	c, _ := partner(t, s)
	var seen []int
	resp, err := get(c, func(o *Options) {
		o.Observe = func(r *Response) { seen = append(seen, r.Status*10+r.Attempts) }
	})
	if err != nil || resp.Attempts != 2 || len(seen) != 2 || seen[0] != 5031 || seen[1] != 2002 {
		t.Errorf("observed %v, %v", seen, err)
	}
}
