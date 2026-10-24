package transport

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"
)

// Every fixture in these tests is invented.

const testPath = "/bids/BID-TEST-0001"

type recorded struct {
	Method string
	Path   string // escaped, without /api/v1
	Query  url.Values
	Header http.Header
	Body   string
}

type testServer struct {
	*httptest.Server
	mu   sync.Mutex
	reqs []recorded
}

// newServer serves handle, which gets the 0-based number of the request it answers.
func newServer(t *testing.T, handle func(w http.ResponseWriter, r *http.Request, n int)) *testServer {
	t.Helper()
	s := &testServer{}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		r.Body = io.NopCloser(bytes.NewReader(b))
		s.mu.Lock()
		n := len(s.reqs)
		s.reqs = append(s.reqs, recorded{
			Method: r.Method, Path: strings.TrimPrefix(r.URL.EscapedPath(), "/api/v1"),
			Query: r.URL.Query(), Header: r.Header.Clone(), Body: string(b),
		})
		s.mu.Unlock()
		handle(w, r, n)
	}))
	t.Cleanup(s.Close)
	return s
}

func (s *testServer) requests() []recorded {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]recorded(nil), s.reqs...)
}

type sleepLog struct {
	mu sync.Mutex
	d  []time.Duration
}

func (l *sleepLog) all() []time.Duration {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]time.Duration(nil), l.d...)
}

// newClient returns a client for s that records its waits instead of sleeping. cfg starts as
// the SDK's defaults, with no credentials.
func newClient(t *testing.T, s *testServer, mods ...func(*Config)) (*Client, *sleepLog) {
	t.Helper()
	cfg := Config{BaseURL: s.URL + "/api/v1", MaxRetries: 2, Timeout: 30 * time.Second, UserAgent: "cruworldwine-go/test"}
	for _, m := range mods {
		m(&cfg)
	}
	c, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	log := &sleepLog{}
	c.Sleep = func(ctx context.Context, d time.Duration) error {
		log.mu.Lock()
		log.d = append(log.d, d)
		log.mu.Unlock()
		return ctx.Err()
	}
	return c, log
}

func asPartner(c *Config) { c.Partner, c.ClientKey, c.ClientSecret = true, "ck_test", "cs_test" }

func asApp(c *Config) { c.AccessToken = "at_test" }

func partner(t *testing.T, s *testServer, mods ...func(*Config)) (*Client, *sleepLog) {
	return newClient(t, s, append([]func(*Config){asPartner}, mods...)...)
}

func app(t *testing.T, s *testServer, mods ...func(*Config)) (*Client, *sleepLog) {
	return newClient(t, s, append([]func(*Config){asApp}, mods...)...)
}

func get(c *Client, opts ...func(*Options)) (*Response, error) {
	return c.Do(context.Background(), Request{Method: http.MethodGet, Path: testPath}, options(opts...))
}

func post(c *Client, body any, opts ...func(*Options)) (*Response, error) {
	return c.Do(context.Background(), Request{Method: http.MethodPost, Path: "/bids", Body: body}, options(opts...))
}

func options(opts ...func(*Options)) *Options {
	o := &Options{}
	for _, opt := range opts {
		opt(o)
	}
	return o
}

func ok(data string) string {
	return `{"status":"OK","message":"Done.","meta":{"request_id":"req_test"},"data":` + data + `}`
}

func apiError(code, message string) string {
	return `{"status":"ERROR","message":"It failed.","meta":{"request_id":"req_test"},"data":null,"errors":[{"code":"` + code + `","message":"` + message + `"}]}`
}

func reply(w http.ResponseWriter, status int, body string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = io.WriteString(w, body)
}

func always(status int, body string) func(http.ResponseWriter, *http.Request, int) {
	return func(w http.ResponseWriter, _ *http.Request, _ int) { reply(w, status, body) }
}

func failing(times int, status int, body string, then string) func(http.ResponseWriter, *http.Request, int) {
	return func(w http.ResponseWriter, _ *http.Request, n int) {
		if n < times {
			reply(w, status, body)
			return
		}
		reply(w, 200, then)
	}
}

// statusOf is the status of a *StatusError, else 0.
func statusOf(err error) int {
	var se *StatusError
	if errors.As(err, &se) {
		return se.Response.Status
	}
	return 0
}

func mustJSON(t *testing.T, s string) map[string]any {
	t.Helper()
	var v map[string]any
	if err := json.Unmarshal([]byte(s), &v); err != nil {
		t.Fatalf("not JSON: %v: %s", err, s)
	}
	return v
}
