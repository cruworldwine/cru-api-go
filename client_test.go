package cru

import (
	"context"
	"errors"
	"net/http"
	"net/url"
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
	if got := h["CLIENT-KEY"]; len(got) != 1 || got[0] != "ck_test" {
		t.Errorf("CLIENT-KEY = %v", got)
	}
	if got := h["CLIENT-SECRET"]; len(got) != 1 || got[0] != "cs_test" {
		t.Errorf("CLIENT-SECRET = %v", got)
	}
	if h.Get("Authorization") != "" {
		t.Error("a partner sent Authorization")
	}
	if h.Get("User-Agent") != "cruworldwine-go/"+Version || h.Get("Accept") != "application/json" {
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

	if _, err := c.Do(ctx, "GET", "/things", nil, nil); err != nil || s.requests()[1].Body != "" {
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
