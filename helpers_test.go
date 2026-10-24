package cru

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

// Every fixture in these tests is invented: fake LWINs, names, codes and ids.

const (
	testLWIN    = "100000120151200750"
	testBidCode = "BID-TEST-0001"
	testCnf     = "cnf_0000000000000000000000000000000000000000000"
)

func TestMain(m *testing.M) {
	for _, name := range []string{"CRU_CLIENT_KEY", "CRU_CLIENT_SECRET", "CRU_ACCESS_TOKEN", "CRU_MARKET", "CRU_BASE_URL"} {
		os.Unsetenv(name)
	}
	os.Exit(m.Run())
}

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

func newClient(t *testing.T, s *testServer, opts ...Option) (*Client, *sleepLog) {
	t.Helper()
	c, err := NewClient(append([]Option{WithBaseURL(s.URL + "/api/v1")}, opts...)...)
	if err != nil {
		t.Fatal(err)
	}
	log := &sleepLog{}
	c.sleep = func(ctx context.Context, d time.Duration) error {
		log.mu.Lock()
		log.d = append(log.d, d)
		log.mu.Unlock()
		return ctx.Err()
	}
	return c, log
}

func partner(t *testing.T, s *testServer, opts ...Option) (*Client, *sleepLog) {
	return newClient(t, s, append([]Option{WithClientKey("ck_test", "cs_test")}, opts...)...)
}

func app(t *testing.T, s *testServer, opts ...Option) (*Client, *sleepLog) {
	return newClient(t, s, append([]Option{WithAccessToken("at_test")}, opts...)...)
}

func meta(extra string) string {
	m := `{"version":"1.0.0","timestamp":"2026-01-01T00:00:00Z","request_id":"req_test"`
	if extra != "" {
		m += "," + extra
	}
	return m + "}"
}

func ok(data string) string {
	return `{"status":"OK","message":"Done.","meta":` + meta("") + `,"data":` + data + `}`
}

func okPaged(data, pagination string) string {
	return `{"status":"OK","message":"Done.","meta":` + meta(`"pagination":`+pagination) + `,"data":` + data + `}`
}

func apiError(code, message string) string {
	return `{"status":"ERROR","message":"It failed.","meta":` + meta("") + `,"data":null,"errors":[{"code":"` + code + `","message":"` + message + `"}]}`
}

func reply(w http.ResponseWriter, status int, body string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = io.WriteString(w, body)
}

func always(status int, body string) func(http.ResponseWriter, *http.Request, int) {
	return func(w http.ResponseWriter, _ *http.Request, _ int) { reply(w, status, body) }
}

func bidJSON(code string) string {
	return `{"code":"` + code + `","client_ref":null,"lwin18":"` + testLWIN + `","wine_name":"Chateau Exemple 2015",
		"vintage":2015,"pack_size":"6x75cl","product_url":null,"price":1200.5,"currency":"GBP","bid_currency":"GBP",
		"quantity":2,"quantity_matched":0,"quantity_open":2,"order_numbers":[],"order_urls":[],"status":"live",
		"renewable":false,"market":"uk","local_only":false,"auto_transfer":false,"expires_on":"2027-01-01",
		"created_at":"2026-01-01T00:00:00Z","updated_at":null}`
}

func bidResultJSON(action string) string {
	return `{"action":"` + action + `","status":"completed","bid":` + bidJSON(testBidCode) + `}`
}

func stagedJSON(action string) string {
	return `{"confirmation":{"code":"` + testCnf + `","action":"` + action + `","status":"pending",
		"expires_at":"2026-01-01T00:15:00Z","preview":{"summary":"Bid 1,200.50 GBP for 2 units.","lwin18":"` + testLWIN + `","price":1200.5}}}`
}

func mustJSON(t *testing.T, s string) map[string]any {
	t.Helper()
	var v map[string]any
	if err := json.Unmarshal([]byte(s), &v); err != nil {
		t.Fatalf("not JSON: %v: %s", err, s)
	}
	return v
}
