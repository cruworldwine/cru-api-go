// Package testkit holds what the SDK's unit tests share: a local server that records what it is
// sent, the API's envelope around invented data, a Caller that sends to the server, the harness
// that checks every operation of a resource package (EveryOperation), and the fuzz check every
// model is put through (Decodes). Only tests import it.
//
// Every fixture here is invented: fake LWINs, names, codes and ids.
package testkit

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cruworldwine/cru-api-go/internal/caller"
	"github.com/cruworldwine/cru-api-go/internal/transport"
	"github.com/cruworldwine/cru-api-go/internal/version"
)

// Invented values the fixtures share.
const (
	LWIN    = "100000120151200750"
	BidCode = "BID-TEST-0001"
	Cnf     = "cnf_0000000000000000000000000000000000000000000"
)

// Recorded is a request a Server got.
type Recorded struct {
	Method string
	Path   string // escaped, without /api/v1
	Query  url.Values
	Header http.Header
	Body   string
}

// Server is a local HTTP server that records every request it gets.
type Server struct {
	*httptest.Server
	mu   sync.Mutex
	reqs []Recorded
}

// NewServer serves handle, which gets the 0-based number of the request it answers, until the
// test ends.
func NewServer(t testing.TB, handle func(w http.ResponseWriter, r *http.Request, n int)) *Server {
	t.Helper()
	s := &Server{}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		r.Body = io.NopCloser(bytes.NewReader(b))
		s.mu.Lock()
		n := len(s.reqs)
		s.reqs = append(s.reqs, Recorded{
			Method: r.Method, Path: strings.TrimPrefix(r.URL.EscapedPath(), "/api/v1"),
			Query: r.URL.Query(), Header: r.Header.Clone(), Body: string(b),
		})
		s.mu.Unlock()
		handle(w, r, n)
	}))
	t.Cleanup(s.Close)
	return s
}

// Requests returns the requests the server has got, in order.
func (s *Server) Requests() []Recorded {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Recorded(nil), s.reqs...)
}

// BaseURL is the API's base URL on the server.
func (s *Server) BaseURL() string { return s.URL + "/api/v1" }

// Partner returns a Caller that sends to s with a partner credential (ck_test and cs_test). Its
// retries do not wait. config changes the transport's configuration first.
func Partner(t testing.TB, s *Server, config ...func(*transport.Config)) *caller.Caller {
	t.Helper()
	return newCaller(t, s, transport.Config{Partner: true, ClientKey: "ck_test", ClientSecret: "cs_test"}, config)
}

// App returns a Caller that sends to s as a connected app, with the access token at_test. Its
// retries do not wait. config changes the transport's configuration first.
func App(t testing.TB, s *Server, config ...func(*transport.Config)) *caller.Caller {
	t.Helper()
	return newCaller(t, s, transport.Config{AccessToken: "at_test"}, config)
}

func newCaller(t testing.TB, s *Server, tc transport.Config, config []func(*transport.Config)) *caller.Caller {
	t.Helper()
	tc.BaseURL, tc.MaxRetries, tc.Timeout, tc.UserAgent = s.BaseURL(), 2, 30*time.Second, version.UserAgent("")
	for _, change := range config {
		change(&tc)
	}
	tr, err := transport.New(tc)
	if err != nil {
		t.Fatal(err)
	}
	tr.Sleep = func(ctx context.Context, _ time.Duration) error { return ctx.Err() }
	return caller.New(tr)
}

// FailingTransport fails every round trip with Err, as a connection that never answers.
type FailingTransport struct{ Err error }

// RoundTrip implements http.RoundTripper.
func (f FailingTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.Body != nil {
		_ = r.Body.Close()
	}
	return nil, f.Err
}

// Meta is the envelope's meta, with extra fields after request_id.
func Meta(extra string) string {
	m := `{"version":"1.0.0","timestamp":"2026-01-01T00:00:00Z","request_id":"req_test"`
	if extra != "" {
		m += "," + extra
	}
	return m + "}"
}

// OK is a successful envelope around data.
func OK(data string) string {
	return `{"status":"OK","message":"Done.","meta":` + Meta("") + `,"data":` + data + `}`
}

// OKPaged is a successful envelope around data, with meta.pagination.
func OKPaged(data, pagination string) string {
	return `{"status":"OK","message":"Done.","meta":` + Meta(`"pagination":`+pagination) + `,"data":` + data + `}`
}

// ErrorBody is an error envelope with one error.
func ErrorBody(code, message string) string {
	return `{"status":"ERROR","message":"It failed.","meta":` + Meta("") + `,"data":null,"errors":[{"code":"` + code + `","message":"` + message + `"}]}`
}

// Reply answers with status and the JSON body.
func Reply(w http.ResponseWriter, status int, body string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = io.WriteString(w, body)
}

// Always answers every request with status and body.
func Always(status int, body string) func(http.ResponseWriter, *http.Request, int) {
	return func(w http.ResponseWriter, _ *http.Request, _ int) { Reply(w, status, body) }
}

// BidJSON is an invented bid, as GET /bids/{code} answers it.
func BidJSON(code string) string {
	return `{"code":"` + code + `","client_ref":null,"lwin18":"` + LWIN + `","wine_name":"Chateau Exemple 2015",
		"vintage":2015,"pack_size":"6x75cl","product_url":null,"price":1200.5,"currency":"GBP","bid_currency":"GBP",
		"quantity":2,"quantity_matched":0,"quantity_open":2,"order_numbers":[],"order_urls":[],"status":"live",
		"renewable":false,"market":"uk","local_only":false,"auto_transfer":false,"expires_on":"2027-01-01",
		"created_at":"2026-01-01T00:00:00Z","updated_at":null}`
}

// BidResultJSON is a bid write's result for action, around BidJSON(BidCode).
func BidResultJSON(action string) string {
	return `{"action":"` + action + `","status":"completed","bid":` + BidJSON(BidCode) + `}`
}

// StagedJSON is a staged write's data for action, with the confirmation code Cnf.
func StagedJSON(action string) string {
	return `{"confirmation":{"code":"` + Cnf + `","action":"` + action + `","status":"pending",
		"expires_at":"2026-01-01T00:15:00Z","preview":{"summary":"Bid 1,200.50 GBP for 2 units.","lwin18":"` + LWIN + `","price":1200.5}}}`
}

// MustJSON decodes s, a JSON object.
func MustJSON(t testing.TB, s string) map[string]any {
	t.Helper()
	var v map[string]any
	if err := json.Unmarshal([]byte(s), &v); err != nil {
		t.Fatalf("not JSON: %v: %s", err, s)
	}
	return v
}
