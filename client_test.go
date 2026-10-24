package cru

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"testing"
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
	s := newServer(t, always(200, ok(`{"pong":true,"client":{}}`)))
	c, _ := app(t, s)
	if _, err := c.Ping(context.Background()); err != nil {
		t.Fatal(err)
	}
	h := s.requests()[0].Header
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

func TestEnvironmentFallback(t *testing.T) {
	t.Run("key pair", func(t *testing.T) {
		t.Setenv("CRU_CLIENT_KEY", "ck_env")
		t.Setenv("CRU_CLIENT_SECRET", "cs_env")
		c, err := NewClient()
		if err != nil || !c.IsPartner() || c.clientKey != "ck_env" || c.clientSecret != "cs_env" {
			t.Fatalf("got %+v, %v", c, err)
		}
	})
	t.Run("token", func(t *testing.T) {
		t.Setenv("CRU_ACCESS_TOKEN", "at_env")
		c, err := NewClient()
		if err != nil || c.IsPartner() || c.currentToken() != "at_env" {
			t.Fatalf("got %+v, %v", c, err)
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
		c, _ := NewClient(WithClientKey("ck_opt", "cs_opt"), WithMarket("sg"))
		if c.clientKey != "ck_opt" || c.BaseURL() != "https://sg.cruworldwine.com/api/v1" {
			t.Errorf("key %s, base URL %s", c.clientKey, c.BaseURL())
		}
	})
	t.Run("an explicit token is not overridden by an environment key pair", func(t *testing.T) {
		t.Setenv("CRU_CLIENT_KEY", "ck_env")
		t.Setenv("CRU_CLIENT_SECRET", "cs_env")
		c, _ := NewClient(WithAccessToken("at_opt"))
		if c.IsPartner() || c.currentToken() != "at_opt" {
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
	if c.BaseURL() != "http://localhost:8080/api/v1" || c.tokenURL != "http://localhost:8080/api/oauth/token" {
		t.Errorf("base %s, token %s", c.BaseURL(), c.tokenURL)
	}
}

func TestDo(t *testing.T) {
	s := newServer(t, func(w http.ResponseWriter, r *http.Request, n int) {
		if r.URL.Path == "/api/v1/missing" {
			reply(w, 404, apiError("NOT_FOUND", "No such endpoint."))
			return
		}
		w.Header().Set("X-Request-Id", "req_hdr")
		reply(w, 201, ok(`{"thing":{"id":1}}`))
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
	got := s.requests()[0]
	if got.Method != "POST" || got.Query.Get("kind") != "new" || got.Query.Get("limit") != "5" {
		t.Errorf("request %+v", got)
	}
	body := mustJSON(t, got.Body)
	if body["name"] != "x" || !strings.HasPrefix(body["reference"].(string), "sdk_") {
		t.Errorf("body %s", got.Body)
	}

	_, err = c.Do(ctx, "GET", "/things", nil, nil)
	if err != nil || s.requests()[1].Body != "" {
		t.Errorf("a GET sent a body %q, %v", s.requests()[1].Body, err)
	}
	_, err = c.Do(ctx, "GET", "/missing", nil, nil)
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Code != ErrorCodeNotFound {
		t.Errorf("err = %v", err)
	}
	raw := []byte(`{"reference":"mine-1","x":1}`)
	if _, err := c.Do(ctx, "PUT", "/things", nil, raw); err != nil || s.requests()[3].Body != string(raw) {
		t.Errorf("raw body sent as %q, %v", s.requests()[3].Body, err)
	}
}

func TestConcurrentUse(t *testing.T) {
	s := newServer(t, always(200, ok(`{"bid":`+bidJSON(testBidCode)+`}`)))
	c, _ := app(t, s)
	var wg sync.WaitGroup
	for range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := c.Bids.Get(context.Background(), testBidCode); err != nil {
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
		s := newServer(t, always(200, ok(`{"pong":true}`)))
		c, _ := partner(t, s, tc.opt)
		if _, err := c.Ping(context.Background()); err != nil {
			t.Fatal(err)
		}
		ua := s.requests()[0].Header.Get("User-Agent")
		if loc := base.FindStringIndex(ua); loc == nil || ua[loc[1]:] != tc.suffix {
			t.Errorf("%s: User-Agent %q", tc.name, ua)
		}
	}
	if v := goVersion(); strings.HasPrefix(v, "go") || strings.Contains(v, " ") || v == "" {
		t.Errorf("goVersion() = %q", v)
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
	other := newServer(t, always(200, ok(`{"pong":true}`)))
	s := newServer(t, func(w http.ResponseWriter, r *http.Request, n int) {
		switch r.URL.Path {
		case "/api/v1/away":
			http.Redirect(w, r, other.URL+"/api/v1/ping", http.StatusTemporaryRedirect)
		case "/api/v1/here":
			http.Redirect(w, r, "/api/v1/ping", http.StatusTemporaryRedirect)
		default:
			reply(w, 200, ok(`{"pong":true}`))
		}
	})
	c, sleeps := partner(t, s)
	ctx := context.Background()
	_, err := c.Do(ctx, "GET", "/away", nil, nil)
	var ce *ConnectionError
	if !errors.Is(err, errRedirect) || errors.As(err, &ce) || len(other.requests()) != 0 || len(sleeps.all()) != 0 {
		t.Errorf("a redirect to another host: %v; %d requests there, %d retries", err, len(other.requests()), len(sleeps.all()))
	}
	if _, err := c.Do(ctx, "GET", "/here", nil, nil); err != nil {
		t.Fatalf("a redirect on the same host: %v", err)
	}
	if got := s.requests()[2]; got.Path != "/ping" || got.Header.Get("Client-Secret") != "cs_test" {
		t.Errorf("the redirect went to %s with secret %q", got.Path, got.Header.Get("Client-Secret"))
	}

	called := false
	guarded := guardRedirects(&http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
		called = true
		return nil
	}})
	from := &http.Request{URL: mustURL(t, "https://uk.cruworldwine.com/api/v1/a")}
	for to, want := range map[string]error{
		"http://uk.cruworldwine.com/api/v1/b":     errRedirect,
		"https://evil.example/api/v1/b":           errRedirect,
		"https://UK.cruworldwine.com/api/v1/b":    nil,
		"https://uk.cruworldwine.com:8443/api/v1": errRedirect,
	} {
		called = false
		err := guarded.CheckRedirect(&http.Request{URL: mustURL(t, to)}, []*http.Request{from})
		if !errors.Is(err, want) || (want == nil && !called) {
			t.Errorf("%s: %v, the client's own policy called: %v", to, err, called)
		}
	}
	via := make([]*http.Request, maxRedirects)
	for i := range via {
		via[i] = from
	}
	if err := guardRedirects(nil).CheckRedirect(&http.Request{URL: from.URL}, via); err == nil {
		t.Error("an endless redirect loop was followed")
	}
}

func mustURL(t *testing.T, s string) *url.URL {
	t.Helper()
	u, err := url.Parse(s)
	if err != nil {
		t.Fatal(err)
	}
	return u
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
