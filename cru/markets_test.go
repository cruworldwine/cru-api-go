package cru

import (
	"context"
	"io"
	"net/http"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/cruworldwine/cru-api-go/internal/testkit"
)

func TestEnvironmentPicksTheHost(t *testing.T) {
	for _, tc := range []struct {
		name string
		env  map[string]string
		opts []Option
		want string
	}{
		{"the default", nil, nil, "https://uk.cruworldwine.com/api/v1"},
		{"live", nil, []Option{WithEnvironment(EnvironmentLive)}, "https://uk.cruworldwine.com/api/v1"},
		{"the sandbox", nil, []Option{WithEnvironment(EnvironmentSandbox)}, "https://uk.crusandbox.com/api/v1"},
		{"the sandbox of a market", nil, []Option{WithEnvironment(EnvironmentSandbox), WithMarket("hk")}, "https://hk.crusandbox.com/api/v1"},
		{"in any case", nil, []Option{WithEnvironment("SandBox"), WithMarket("SG")}, "https://sg.crusandbox.com/api/v1"},
		{"live in any case", nil, []Option{WithEnvironment("LIVE"), WithMarket("eu")}, "https://eu.cruworldwine.com/api/v1"},
		{"CRU_ENVIRONMENT", map[string]string{"CRU_ENVIRONMENT": "sandbox"}, nil, "https://uk.crusandbox.com/api/v1"},
		{"CRU_ENVIRONMENT in any case, with CRU_MARKET", map[string]string{"CRU_ENVIRONMENT": "Sandbox", "CRU_MARKET": "ae"}, nil, "https://ae.crusandbox.com/api/v1"},
		{"the option wins over CRU_ENVIRONMENT", map[string]string{"CRU_ENVIRONMENT": "sandbox"}, []Option{WithEnvironment(EnvironmentLive)}, "https://uk.cruworldwine.com/api/v1"},
		{"a new market needs no release", nil, []Option{WithEnvironment(EnvironmentSandbox), WithMarket("zz")}, "https://zz.crusandbox.com/api/v1"},
		{"a base URL wins", nil, []Option{WithEnvironment(EnvironmentSandbox), WithMarket("hk"), WithBaseURL("http://localhost:8080/api/v1/")}, "http://localhost:8080/api/v1"},
		{"CRU_BASE_URL wins", map[string]string{"CRU_ENVIRONMENT": "sandbox", "CRU_BASE_URL": "https://uk.cruworldwine.com/api/v1"}, nil, "https://uk.cruworldwine.com/api/v1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for k, v := range tc.env {
				t.Setenv(k, v)
			}
			c, err := NewClient(append([]Option{WithAccessToken("at")}, tc.opts...)...)
			if err != nil {
				t.Fatal(err)
			}
			if c.BaseURL() != tc.want {
				t.Errorf("base URL %s, want %s", c.BaseURL(), tc.want)
			}
		})
	}
}

func TestEnvironmentErrors(t *testing.T) {
	for _, env := range []Environment{"staging", "test", "prod", "sandbox ", " live", "crusandbox.com"} {
		if _, err := NewClient(WithAccessToken("at"), WithEnvironment(env)); err == nil || !strings.Contains(err.Error(), "live or sandbox") {
			t.Errorf("environment %q: %v, want an error", env, err)
		}
	}
	t.Run("from CRU_ENVIRONMENT", func(t *testing.T) {
		t.Setenv("CRU_ENVIRONMENT", "staging")
		if _, err := NewClient(WithAccessToken("at")); err == nil {
			t.Error("want an error")
		}
	})
	t.Run("with a base URL", func(t *testing.T) {
		if _, err := NewClient(WithAccessToken("at"), WithEnvironment("staging"), WithBaseURL("http://localhost:1/api/v1")); err == nil {
			t.Error("want an error")
		}
	})
	t.Run("an invalid market on the sandbox", func(t *testing.T) {
		if _, err := NewClient(WithAccessToken("at"), WithEnvironment(EnvironmentSandbox), WithMarket("evil.example/#")); err == nil {
			t.Error("want an error")
		}
	})
}

// routeTransport answers each request with what answer returns for it, in memory, and records
// the URLs.
type routeTransport struct {
	mu     sync.Mutex
	urls   []string
	answer func(*http.Request) (int, string)
}

func (rt *routeTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	rt.mu.Lock()
	rt.urls = append(rt.urls, r.URL.String())
	rt.mu.Unlock()
	status, body := rt.answer(r)
	return &http.Response{
		StatusCode: status, Header: http.Header{"Content-Type": {"application/json"}},
		Body: io.NopCloser(strings.NewReader(body)), Request: r,
	}, nil
}

func TestSandboxRefreshesOnTheSandbox(t *testing.T) {
	rt := &routeTransport{answer: func(r *http.Request) (int, string) {
		switch {
		case r.URL.Path == "/api/oauth/token":
			return 200, `{"access_token":"at_new","refresh_token":"rt_new","token_type":"Bearer","expires_in":3600}`
		case r.Header.Get("Authorization") == "Bearer at_new":
			return 200, testkit.OK(`{"pong":true}`)
		}
		return 401, testkit.ErrorBody("AUTH_INVALID_CREDENTIALS", "Expired.")
	}}
	c, err := NewClient(WithAccessToken("at_old"), WithOAuthRefresh("rt_0", "app_test", "", nil),
		WithEnvironment(EnvironmentSandbox), WithMarket("hk"), WithHTTPClient(&http.Client{Transport: rt}))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Ping(context.Background()); err != nil {
		t.Fatal(err)
	}
	want := []string{
		"https://hk.crusandbox.com/api/v1/ping",
		"https://hk.crusandbox.com/api/oauth/token",
		"https://hk.crusandbox.com/api/v1/ping",
	}
	if !slices.Equal(rt.urls, want) {
		t.Errorf("sent to %v, want %v", rt.urls, want)
	}
}

func TestMarkets(t *testing.T) {
	all := Markets()
	codes := make([]string, len(all))
	for i, m := range all {
		codes[i] = m.Code
	}
	if want := []string{"uk", "us", "hk", "sg", "eu", "ae", "cn"}; !slices.Equal(codes, want) {
		t.Fatalf("codes %v, want %v", codes, want)
	}
	if uk := all[0]; uk.Name != "United Kingdom" || uk.Currency != "GBP" || uk.Symbol != "£" ||
		uk.LiveURL != "https://uk.cruworldwine.com/api/v1" || uk.SandboxURL != "https://uk.crusandbox.com/api/v1" {
		t.Errorf("uk %+v", uk)
	}
	for _, m := range all {
		if m.Name == "" || len(m.Currency) != 3 || m.Symbol == "" {
			t.Errorf("%s: %+v", m.Code, m)
		}
		if m.URL(EnvironmentLive) != m.LiveURL || m.URL(EnvironmentSandbox) != m.SandboxURL ||
			m.URL("SANDBOX") != m.SandboxURL || m.URL("staging") != "" || m.URL("") != "" {
			t.Errorf("%s: URL does not pick its environment's", m.Code)
		}
		// The table and NewClient build the same host.
		for _, env := range []Environment{EnvironmentLive, EnvironmentSandbox} {
			c, err := NewClient(WithAccessToken("at"), WithMarket(m.Code), WithEnvironment(env))
			if err != nil || c.BaseURL() != m.URL(env) {
				t.Errorf("%s on %s: client on %v, table %s, %v", m.Code, env, c, m.URL(env), err)
			}
		}
	}

	all[0].Code, all[0].Currency = "xx", "XXX"
	all[1] = MarketInfo{Code: "zz"}
	if again := Markets(); again[0].Code != "uk" || again[0].Currency != "GBP" || again[1].Code != "us" {
		t.Errorf("changing the slice Markets returned changed the table: %+v", again[:2])
	}
	if m, ok := LookupMarket("uk"); !ok || m.Currency != "GBP" {
		t.Errorf("uk after the change: %+v, %v", m, ok)
	}
}

func TestLookupMarket(t *testing.T) {
	for _, tc := range []struct {
		code, currency, symbol string
		ok                     bool
	}{
		{"uk", "GBP", "£", true},
		{"HK", "HKD", "HK$", true},
		{"Sg", "SGD", "SG$", true},
		{"cn", "CNY", "¥", true},
		{"zz", "", "", false},
		{"fr", "", "", false},
		{"", "", "", false},
		{"uk ", "", "", false},
	} {
		m, ok := LookupMarket(tc.code)
		if ok != tc.ok || m.Currency != tc.currency || m.Symbol != tc.symbol {
			t.Errorf("%q: %+v, %v", tc.code, m, ok)
		}
		if ok && m.Code != strings.ToLower(tc.code) {
			t.Errorf("%q: code %q", tc.code, m.Code)
		}
	}
}

func TestClientMarketInfo(t *testing.T) {
	for _, tc := range []struct {
		name string
		env  map[string]string
		opts []Option
		want string // the market's code; "" when MarketInfo reports none
	}{
		{"the default", nil, nil, "uk"},
		{"WithMarket", nil, []Option{WithMarket("HK")}, "hk"},
		{"CRU_MARKET", map[string]string{"CRU_MARKET": "sg"}, nil, "sg"},
		{"on the sandbox", nil, []Option{WithEnvironment(EnvironmentSandbox), WithMarket("ae")}, "ae"},
		{"a market the table lacks", nil, []Option{WithMarket("zz")}, ""},
		{"a live host", nil, []Option{WithBaseURL("https://us.cruworldwine.com/api/v1")}, "us"},
		{"a sandbox host", nil, []Option{WithBaseURL("https://hk.crusandbox.com/api/v1")}, "hk"},
		{"a host in any case, with a port", nil, []Option{WithBaseURL("https://EU.CruSandbox.com:443/api/v1")}, "eu"},
		{"the host wins over the market", nil, []Option{WithMarket("uk"), WithBaseURL("https://cn.cruworldwine.com/api/v1")}, "cn"},
		{"CRU_BASE_URL's host", map[string]string{"CRU_BASE_URL": "https://sg.crusandbox.com/api/v1", "CRU_MARKET": "uk"}, nil, "sg"},
		{"a market's host the table lacks", nil, []Option{WithMarket("uk"), WithBaseURL("https://zz.crusandbox.com/api/v1")}, ""},
		{"another host, with WithMarket", nil, []Option{WithMarket("SG"), WithBaseURL("http://localhost:8080/api/v1")}, "sg"},
		{"another host, with CRU_MARKET", map[string]string{"CRU_MARKET": "us"}, []Option{WithBaseURL("http://localhost:8080/api/v1")}, "us"},
		{"another host alone", nil, []Option{WithBaseURL("http://localhost:8080/api/v1")}, ""},
		{"another host, with a market that is no code", nil, []Option{WithMarket("not a code"), WithBaseURL("http://localhost:8080/api/v1")}, ""},
		{"a host two labels under the domain", nil, []Option{WithBaseURL("https://api.hk.cruworldwine.com/api/v1")}, ""},
		{"the domain itself", nil, []Option{WithBaseURL("https://cruworldwine.com/api/v1")}, ""},
		{"a look-alike domain", nil, []Option{WithBaseURL("https://hkcruworldwine.com/api/v1")}, ""},
		{"a market's host under another domain", nil, []Option{WithBaseURL("https://hk.cruworldwine.com.example.test/api/v1")}, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for k, v := range tc.env {
				t.Setenv(k, v)
			}
			c, err := NewClient(append([]Option{WithAccessToken("at")}, tc.opts...)...)
			if err != nil {
				t.Fatal(err)
			}
			got, ok := c.MarketInfo()
			want, wantOK := LookupMarket(tc.want)
			if ok != wantOK || got != want {
				t.Errorf("MarketInfo %+v, %v; want %+v, %v", got, ok, want, wantOK)
			}
		})
	}
	if got := marketOfBaseURL("http://[::1", "hk"); got != "hk" {
		t.Errorf("an unparsable base URL: %q", got)
	}
}
