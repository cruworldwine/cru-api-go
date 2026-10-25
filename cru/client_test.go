package cru

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cruworldwine/cru-api-go/cru/core"
	"github.com/cruworldwine/cru-api-go/cru/portfolio"
	"github.com/cruworldwine/cru-api-go/internal/testkit"
	"github.com/cruworldwine/cru-api-go/internal/transport"
)

type captureTransport struct {
	mu   sync.Mutex
	reqs []*http.Request
}

func (c *captureTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	c.mu.Lock()
	c.reqs = append(c.reqs, r)
	c.mu.Unlock()
	return &http.Response{
		StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}},
		Body: http.NoBody, Request: r,
	}, nil
}

func TestPartnerHeaders(t *testing.T) {
	rt := &captureTransport{}
	c, err := NewClient(WithClientKey("ck_test", "cs_test"), WithHTTPClient(&http.Client{Transport: rt}))
	if err != nil {
		t.Fatal(err)
	}
	_, _ = c.Ping(context.Background())
	h := rt.reqs[0].Header
	// The spec's spelling is kept on the wire, not canonicalised to Client-Key.
	if got := h["CLIENT-KEY"]; len(got) != 1 || got[0] != "ck_test" { //nolint:staticcheck // the wire spelling, on purpose
		t.Errorf("CLIENT-KEY = %v", got)
	}
	if got := h["CLIENT-SECRET"]; len(got) != 1 || got[0] != "cs_test" { //nolint:staticcheck // the wire spelling, on purpose
		t.Errorf("CLIENT-SECRET = %v", got)
	}
	if h.Get("Authorization") != "" {
		t.Error("a partner sent Authorization")
	}
	if !strings.HasPrefix(h.Get("User-Agent"), "cruworldwine-go/"+Version+" go/") || h.Get("Accept") != "application/json" {
		t.Errorf("User-Agent %q, Accept %q", h.Get("User-Agent"), h.Get("Accept"))
	}
	if got := rt.reqs[0].URL.String(); got != "https://uk.cruworldwine.com/api/v1/ping" {
		t.Errorf("URL = %s", got)
	}
	if !c.IsPartner() {
		t.Error("IsPartner is false")
	}
}

func TestConnectedAppHeaders(t *testing.T) {
	s := testkit.NewServer(t, testkit.Always(200, testkit.OK(`{"pong":true,"client":{}}`)))
	c, _ := app(t, s)
	if _, err := c.Ping(context.Background()); err != nil {
		t.Fatal(err)
	}
	h := s.Requests()[0].Header
	if h.Get("Authorization") != "Bearer at_test" {
		t.Errorf("Authorization = %q", h.Get("Authorization"))
	}
	if h.Get("Client-Key") != "" || h.Get("Client-Secret") != "" {
		t.Error("a connected app sent a client key")
	}
	if c.IsPartner() {
		t.Error("IsPartner is true")
	}
}

func TestPartnerWinsOverToken(t *testing.T) {
	c, err := NewClient(WithClientKey("ck", "cs"), WithAccessToken("at"))
	if err != nil || !c.IsPartner() {
		t.Fatalf("partner = %v, %v", c != nil && c.IsPartner(), err)
	}
}

// sent returns the client opts make, and the headers its first request carries.
func sent(t *testing.T, opts ...Option) (*Client, http.Header) {
	t.Helper()
	rt := &captureTransport{}
	c, err := NewClient(append(opts, WithHTTPClient(&http.Client{Transport: rt}))...)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Ping(context.Background()); err != nil {
		t.Fatal(err)
	}
	return c, rt.reqs[0].Header
}

// onWire reads a header as the client spelled it, before any server canonicalised it.
func onWire(h http.Header, name string) string {
	if v := h[name]; len(v) == 1 {
		return v[0]
	}
	return ""
}

func TestEnvironmentFallback(t *testing.T) {
	t.Run("key pair", func(t *testing.T) {
		t.Setenv("CRU_CLIENT_KEY", "ck_env")
		t.Setenv("CRU_CLIENT_SECRET", "cs_env")
		c, h := sent(t)
		if !c.IsPartner() || onWire(h, "CLIENT-KEY") != "ck_env" || onWire(h, "CLIENT-SECRET") != "cs_env" {
			t.Fatalf("got %v, headers %v", c, h)
		}
	})
	t.Run("token", func(t *testing.T) {
		t.Setenv("CRU_ACCESS_TOKEN", "at_env")
		c, h := sent(t)
		if c.IsPartner() || h.Get("Authorization") != "Bearer at_env" {
			t.Fatalf("got %v, headers %v", c, h)
		}
	})
	t.Run("market and base URL", func(t *testing.T) {
		t.Setenv("CRU_ACCESS_TOKEN", "at_env")
		t.Setenv("CRU_MARKET", "hk")
		c, _ := NewClient()
		if c.BaseURL() != "https://hk.cruworldwine.com/api/v1" {
			t.Errorf("base URL %s", c.BaseURL())
		}
		t.Setenv("CRU_BASE_URL", "https://sandbox.example.test/api/v1/")
		c, _ = NewClient()
		if c.BaseURL() != "https://sandbox.example.test/api/v1" {
			t.Errorf("base URL %s", c.BaseURL())
		}
	})
	t.Run("options win", func(t *testing.T) {
		t.Setenv("CRU_CLIENT_KEY", "ck_env")
		t.Setenv("CRU_CLIENT_SECRET", "cs_env")
		t.Setenv("CRU_MARKET", "hk")
		c, h := sent(t, WithClientKey("ck_opt", "cs_opt"), WithMarket("sg"))
		if onWire(h, "CLIENT-KEY") != "ck_opt" || c.BaseURL() != "https://sg.cruworldwine.com/api/v1" {
			t.Errorf("key %s, base URL %s", onWire(h, "CLIENT-KEY"), c.BaseURL())
		}
	})
	t.Run("an explicit token is not overridden by an environment key pair", func(t *testing.T) {
		t.Setenv("CRU_CLIENT_KEY", "ck_env")
		t.Setenv("CRU_CLIENT_SECRET", "cs_env")
		c, h := sent(t, WithAccessToken("at_opt"))
		if c.IsPartner() || h.Get("Authorization") != "Bearer at_opt" {
			t.Error("the environment's key pair replaced the access token")
		}
	})
}

func TestNewClientErrors(t *testing.T) {
	if _, err := NewClient(); err == nil {
		t.Error("no credentials: want an error")
	}
	if _, err := NewClient(WithClientKey("ck", "")); err == nil {
		t.Error("a key without a secret: want an error")
	}
	if _, err := NewClient(WithAccessToken("at"), WithBaseURL("not a url")); err == nil {
		t.Error("a relative base URL: want an error")
	}
}

func TestBaseURLPerMarket(t *testing.T) {
	for market, want := range map[string]string{
		"":   "https://uk.cruworldwine.com/api/v1",
		"uk": "https://uk.cruworldwine.com/api/v1",
		"us": "https://us.cruworldwine.com/api/v1",
		"sg": "https://sg.cruworldwine.com/api/v1",
		"eu": "https://eu.cruworldwine.com/api/v1",
	} {
		c, _ := NewClient(WithAccessToken("at"), WithMarket(market))
		if c.BaseURL() != want {
			t.Errorf("market %q: %s, want %s", market, c.BaseURL(), want)
		}
	}
	c, _ := NewClient(WithAccessToken("at"), WithMarket("hk"), WithBaseURL("http://localhost:8080/api/v1/"))
	if c.BaseURL() != "http://localhost:8080/api/v1" {
		t.Errorf("base %s", c.BaseURL())
	}
}

func TestDo(t *testing.T) {
	s := testkit.NewServer(t, func(w http.ResponseWriter, r *http.Request, n int) {
		if r.URL.Path == "/api/v1/missing" {
			testkit.Reply(w, 404, testkit.ErrorBody("NOT_FOUND", "No such endpoint."))
			return
		}
		w.Header().Set("X-Request-Id", "req_hdr")
		testkit.Reply(w, 201, testkit.OK(`{"thing":{"id":1}}`))
	})
	c, _ := partner(t, s)
	ctx := context.Background()

	resp, err := c.Do(ctx, "post", "/things?kind=new", url.Values{"limit": {"5"}}, map[string]any{"name": "x"})
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 201 || resp.Status != "OK" || resp.Message != "Done." || resp.Header.Get("X-Request-Id") != "req_hdr" {
		t.Errorf("response %+v", resp)
	}
	if string(resp.Data) != `{"thing":{"id":1}}` || !strings.Contains(string(resp.Meta), "req_test") || len(resp.Body) == 0 {
		t.Errorf("data %s, meta %s", resp.Data, resp.Meta)
	}
	got := s.Requests()[0]
	if got.Method != "POST" || got.Query.Get("kind") != "new" || got.Query.Get("limit") != "5" {
		t.Errorf("request %+v", got)
	}
	body := testkit.MustJSON(t, got.Body)
	if body["name"] != "x" || !strings.HasPrefix(body["reference"].(string), "sdk_") {
		t.Errorf("body %s", got.Body)
	}

	_, err = c.Do(ctx, "GET", "/things", nil, nil)
	if err != nil || s.Requests()[1].Body != "" {
		t.Errorf("a GET sent a body %q, %v", s.Requests()[1].Body, err)
	}
	_, err = c.Do(ctx, "GET", "/missing", nil, nil)
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Code != ErrorCodeNotFound {
		t.Errorf("err = %v", err)
	}
	raw := []byte(`{"reference":"mine-1","x":1}`)
	if _, err := c.Do(ctx, "PUT", "/things", nil, raw); err != nil || s.Requests()[3].Body != string(raw) {
		t.Errorf("raw body sent as %q, %v", s.Requests()[3].Body, err)
	}
	if _, err := c.Do(ctx, "GET", "no-slash", nil, nil); err != nil || s.Requests()[4].Path != "/no-slash" {
		t.Errorf("a path without a leading slash went to %s, %v", s.Requests()[4].Path, err)
	}
}

func TestConcurrentUse(t *testing.T) {
	s := testkit.NewServer(t, testkit.Always(200, testkit.OK(`{"bid":`+testkit.BidJSON(testkit.BidCode)+`}`)))
	c, _ := app(t, s)
	var wg sync.WaitGroup
	for range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := c.Bids.Get(context.Background(), testkit.BidCode); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
}

func TestUserAgent(t *testing.T) {
	base := regexp.MustCompile(`^cruworldwine-go/` + regexp.QuoteMeta(Version) + ` go/[0-9][^ ()]* \(` +
		regexp.QuoteMeta(runtime.GOOS) + `; ` + regexp.QuoteMeta(runtime.GOARCH) + `\)`)
	for _, tc := range []struct {
		name   string
		opt    Option
		suffix string
	}{
		{"the SDK alone", nil, ""},
		{"an app", WithAppInfo("Cellar Sync", "2.1.0", "https://example.test/cellar"), " Cellar Sync/2.1.0 (https://example.test/cellar)"},
		{"a name alone", WithAppInfo("cellar", "", ""), " cellar"},
		{"no name", WithAppInfo("", "1.0", "https://example.test"), ""},
		{"no header injection", WithAppInfo("evil\r\nX-Injected: 1", "1\n", ""), " evilX-Injected: 1/1"},
	} {
		s := testkit.NewServer(t, testkit.Always(200, testkit.OK(`{"pong":true}`)))
		c, _ := partner(t, s, tc.opt)
		if _, err := c.Ping(context.Background()); err != nil {
			t.Fatal(err)
		}
		ua := s.Requests()[0].Header.Get("User-Agent")
		if loc := base.FindStringIndex(ua); loc == nil || ua[loc[1]:] != tc.suffix {
			t.Errorf("%s: User-Agent %q", tc.name, ua)
		}
	}
}

func TestMarketCodes(t *testing.T) {
	for _, market := range []string{"evil.example/#", "uk.evil.example", "u k", "-uk", "uk-", "uk/", strings.Repeat("a", 64)} {
		if _, err := NewClient(WithAccessToken("at"), WithMarket(market)); err == nil {
			t.Errorf("market %q: want an error", market)
		}
	}
	t.Setenv("CRU_MARKET", "attacker.example:443/")
	if _, err := NewClient(WithAccessToken("at")); err == nil {
		t.Error("an invalid CRU_MARKET: want an error")
	}
	c, err := NewClient(WithAccessToken("at"), WithMarket("HK"))
	if err != nil || c.BaseURL() != "https://hk.cruworldwine.com/api/v1" {
		t.Errorf("market HK: %v, %v", c, err)
	}
	if c, err := NewClient(WithAccessToken("at"), WithBaseURL("http://localhost:1/api/v1")); err != nil || c.BaseURL() != "http://localhost:1/api/v1" {
		t.Errorf("a base URL wins over the market: %v, %v", c, err)
	}
}

func TestRedirects(t *testing.T) {
	other := testkit.NewServer(t, testkit.Always(200, testkit.OK(`{"pong":true}`)))
	s := testkit.NewServer(t, func(w http.ResponseWriter, r *http.Request, n int) {
		switch r.URL.Path {
		case "/api/v1/away":
			http.Redirect(w, r, other.BaseURL()+"/ping", http.StatusTemporaryRedirect)
		case "/api/v1/here":
			http.Redirect(w, r, "/api/v1/ping", http.StatusTemporaryRedirect)
		default:
			testkit.Reply(w, 200, testkit.OK(`{"pong":true}`))
		}
	})
	c, sleeps := partner(t, s)
	ctx := context.Background()
	_, err := c.Do(ctx, "GET", "/away", nil, nil)
	var ce *ConnectionError
	var apiErr *APIError
	if !errors.Is(err, transport.ErrRedirect) || errors.As(err, &ce) || errors.As(err, &apiErr) ||
		len(other.Requests()) != 0 || len(sleeps.all()) != 0 {
		t.Errorf("a redirect to another host: %v; %d requests there, %d retries", err, len(other.Requests()), len(sleeps.all()))
	}
	if _, err := c.Do(ctx, "GET", "/here", nil, nil); err != nil {
		t.Fatalf("a redirect on the same host: %v", err)
	}
	if got := s.Requests()[2]; got.Path != "/ping" || got.Header.Get("Client-Secret") != "cs_test" {
		t.Errorf("the redirect went to %s with secret %q", got.Path, got.Header.Get("Client-Secret"))
	}
}

func TestPrintingHidesCredentials(t *testing.T) {
	for _, c := range []*Client{
		must(NewClient(WithClientKey("ck_SECRET_1", "cs_SECRET_2"))),
		must(NewClient(WithAccessToken("at_SECRET_3"), WithOAuthRefresh("rt_SECRET_4", "app", "as_SECRET_5", nil))),
	} {
		out := fmt.Sprintf("%v %+v %#v %s", c, c, c, c)
		if strings.Contains(out, "SECRET") || !strings.Contains(out, "uk.cruworldwine.com") {
			t.Errorf("printed as %s", out)
		}
	}
}

func must[T any](v T, err error) T {
	if err != nil {
		panic(err)
	}
	return v
}

// TestWithLogger checks the logger reaches the transport; internal/transport tests what it logs.
func TestWithLogger(t *testing.T) {
	var buf strings.Builder
	var mu sync.Mutex
	logger := slog.New(slog.NewTextHandler(writerFunc(func(p []byte) (int, error) {
		mu.Lock()
		defer mu.Unlock()
		return buf.Write(p)
	}), &slog.HandlerOptions{Level: slog.LevelDebug}))
	s := testkit.NewServer(t, testkit.Always(200, testkit.OK(`{"pong":true}`)))
	c, _ := partner(t, s, WithLogger(logger))
	if _, err := c.Ping(context.Background()); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if out := buf.String(); !strings.Contains(out, "cru: request") || !strings.Contains(out, "path=/api/v1/ping") || strings.Contains(out, "cs_test") {
		t.Errorf("log: %s", out)
	}
}

type writerFunc func([]byte) (int, error)

func (f writerFunc) Write(p []byte) (int, error) { return f(p) }

func TestRefreshHandsOverTokens(t *testing.T) {
	s := testkit.NewServer(t, func(w http.ResponseWriter, r *http.Request, n int) {
		switch {
		case r.URL.Path == "/api/oauth/token":
			testkit.Reply(w, 200, `{"access_token":"at_new","refresh_token":"rt_new","token_type":"Bearer","expires_in":3600,"scope":"bids:read"}`)
		case r.Header.Get("Authorization") == "Bearer at_new":
			testkit.Reply(w, 200, testkit.OK(`{"bid":`+testkit.BidJSON(testkit.BidCode)+`}`))
		default:
			testkit.Reply(w, 401, testkit.ErrorBody("AUTH_INVALID_CREDENTIALS", "Expired."))
		}
	})
	var got []Token
	c, _ := newClient(t, s, WithAccessToken("at_old"), WithOAuthRefresh("rt_0", "app_test", "", func(tok Token) { got = append(got, tok) }))
	if _, err := c.Bids.Get(context.Background(), testkit.BidCode); err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].AccessToken != "at_new" || got[0].RefreshToken != "rt_new" || got[0].ExpiresIn != 3600 ||
		got[0].TokenType != "Bearer" || got[0].Scope != "bids:read" || len(got[0].Raw) == 0 {
		t.Errorf("onRefresh got %+v", got)
	}
}

// TestClientOptionsReachTheTransport checks the options NewClient passes on: core and
// internal/transport test what they do.
func TestClientOptionsReachTheTransport(t *testing.T) {
	s := testkit.NewServer(t, func(w http.ResponseWriter, r *http.Request, n int) {
		if n == 0 {
			<-r.Context().Done()
			return
		}
		testkit.Reply(w, 503, testkit.ErrorBody("SERVICE_UNAVAILABLE", "Busy."))
	})
	c, sleeps := partner(t, s, WithTimeout(20*time.Millisecond), WithMaxRetries(0))
	var ce *ConnectionError
	if _, err := c.Ping(context.Background()); !errors.As(err, &ce) || !ce.Timeout() || len(sleeps.all()) != 0 {
		t.Errorf("WithTimeout and WithMaxRetries(0): %v after %d retries", err, len(sleeps.all()))
	}
	c, sleeps = partner(t, s, WithMaxRetries(1), WithHTTPClient(&http.Client{}))
	if _, err := c.Ping(context.Background()); !errors.Is(err, ErrServer) || len(sleeps.all()) != 1 {
		t.Errorf("WithMaxRetries(1): %v after %d retries", err, len(sleeps.all()))
	}
}

func TestNilOptionsAreIgnored(t *testing.T) {
	s := testkit.NewServer(t, testkit.Always(200, testkit.OK(`{"pong":true}`)))
	c, err := NewClient(nil, WithAccessToken("at"), WithBaseURL(s.BaseURL()))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Ping(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
}

// TestCoreNamedAgain checks that package cru's names for core's types, values and options are
// core's own, so code may use either.
func TestCoreNamedAgain(t *testing.T) {
	for i, pair := range [][2]error{
		{ErrBadRequest, core.ErrBadRequest}, {ErrAuthentication, core.ErrAuthentication},
		{ErrPermissionDenied, core.ErrPermissionDenied}, {ErrNotFound, core.ErrNotFound},
		{ErrMethodNotAllowed, core.ErrMethodNotAllowed}, {ErrConflict, core.ErrConflict}, {ErrGone, core.ErrGone},
		{ErrUnprocessable, core.ErrUnprocessable}, {ErrRateLimited, core.ErrRateLimited}, {ErrServer, core.ErrServer},
		{ErrValidation, core.ErrValidation}, {ErrPriceChanged, core.ErrPriceChanged},
		{ErrRequestInProgress, core.ErrRequestInProgress}, {ErrTradeRefused, core.ErrTradeRefused},
		{ErrMissingParameter, core.ErrMissingParameter}, {ErrUnexpectedResponse, core.ErrUnexpectedResponse},
	} {
		if !errors.Is(pair[0], pair[1]) {
			t.Errorf("error %d is not core's", i)
		}
	}
	if !errors.Is(&APIError{StatusCode: 404}, core.ErrNotFound) || CurrencyGBP != core.CurrencyGBP || StockTypeCru != "cru" {
		t.Error("a type or value differs from core's")
	}

	s := testkit.NewServer(t, func(w http.ResponseWriter, r *http.Request, n int) {
		w.Header().Set("X-Request-Id", "req_header")
		testkit.Reply(w, 200, testkit.OK(`{"thing":true}`))
	})
	c, _ := partner(t, s)
	var meta ResponseMeta
	opts := []RequestOption{WithRequestHeader("X-Trace", "t1"), WithRequestTimeout(time.Minute), WithRequestMaxRetries(0),
		WithExtraQuery("extra", "1"), WithExtraBodyField("field", 2), WithResponseInto(&meta)}
	if _, err := c.Do(context.Background(), "POST", "/things", nil, nil, opts...); err != nil {
		t.Fatal(err)
	}
	got := s.Requests()[0]
	if got.Header.Get("X-Trace") != "t1" || got.Query.Get("extra") != "1" || testkit.MustJSON(t, got.Body)["field"] != 2.0 ||
		meta.StatusCode != 200 || meta.RequestID != "req_test" {
		t.Errorf("sent %+v, meta %+v", got, meta)
	}

	var line portfolio.Line
	if err := json.Unmarshal([]byte(`{"portfolio_id":"P-1","future":"yes"}`), &line); err != nil {
		t.Fatal(err)
	}
	var future string
	var model Model = &line
	if found, err := Field(model, "future", &future); !found || err != nil || future != "yes" {
		t.Errorf("Field: %v, %v, %q", found, err, future)
	}
}

func TestValueReadsNullableFields(t *testing.T) {
	var w portfolio.Wine
	if err := json.Unmarshal([]byte(`{"lwin18":"1","wine_name":"Example 2016","vintage":null}`), &w); err != nil {
		t.Fatal(err)
	}
	if Value(w.WineName) != "Example 2016" || Value(w.Vintage) != 0 || Value(w.MarketPrice) != 0 {
		t.Errorf("Value: %q %d %v", Value(w.WineName), Value(w.Vintage), Value(w.MarketPrice))
	}
	if *String("a") != "a" || *Int(1) != 1 || *Float(1.5) != 1.5 || !*Bool(true) {
		t.Error("a pointer helper lost its value")
	}
}
