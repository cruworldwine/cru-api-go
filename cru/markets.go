package cru

import (
	"fmt"
	"net/url"
	"slices"
	"strings"
)

// Environment is one of the API's environments: live, or the sandbox. WithEnvironment picks one.
type Environment string

const (
	// EnvironmentLive is the real API, at https://{market}.cruworldwine.com/api/v1. It is the
	// default.
	EnvironmentLive Environment = "live"
	// EnvironmentSandbox is the sandbox, at https://{market}.crusandbox.com/api/v1, for building
	// and testing: no real money or wine changes hands there. It takes credentials issued for the
	// sandbox, and has the same markets as live.
	EnvironmentSandbox Environment = "sandbox"
)

// WithEnvironment picks the environment whose host the client calls: EnvironmentLive, the
// default, or EnvironmentSandbox. It is read in any case; NewClient refuses any other value. A
// base URL (WithBaseURL or CRU_BASE_URL) wins over it.
func WithEnvironment(env Environment) Option {
	return func(c *config) { c.environment = string(env) }
}

// MarketInfo is one of Cru's markets. The SDK carries the table of them, so a program knows a
// market's currency without a call.
type MarketInfo struct {
	// Code is the market's code, as WithMarket takes it: "uk", "hk" and so on.
	Code string
	// Name is the market's name, such as "United Kingdom".
	Name string
	// Currency is the ISO 4217 code of the currency every amount on the market's host is in,
	// such as "GBP".
	Currency string
	// Symbol is the currency's symbol as Cru's website shows it, such as "£" or "HK$".
	Symbol string
	// LiveURL is the market's base URL on the live API, such as
	// https://uk.cruworldwine.com/api/v1.
	LiveURL string
	// SandboxURL is the market's base URL on the sandbox, such as
	// https://uk.crusandbox.com/api/v1.
	SandboxURL string
}

// URL returns the market's base URL in env, read in any case: LiveURL or SandboxURL, and "" for
// another value.
func (m MarketInfo) URL(env Environment) string {
	switch Environment(strings.ToLower(string(env))) {
	case EnvironmentLive:
		return m.LiveURL
	case EnvironmentSandbox:
		return m.SandboxURL
	}
	return ""
}

// Markets returns every market, in the order the API's spec lists them, uk first. Each call
// returns a new slice, which the caller may change.
func Markets() []MarketInfo { return slices.Clone(markets) }

// LookupMarket returns the market whose code is code, in any case, and whether there is one.
func LookupMarket(code string) (MarketInfo, bool) {
	code = strings.ToLower(code)
	for _, m := range markets {
		if m.Code == code {
			return m, true
		}
	}
	return MarketInfo{}, false
}

// MarketInfo returns the market the client calls, and whether it is known.
//
// Without a base URL, it is the market the client was made for: WithMarket, CRU_MARKET or uk.
// With one, it is the market whose host the base URL is on in either environment, such as hk for
// https://hk.crusandbox.com/api/v1; for any other host, the market WithMarket or CRU_MARKET gave;
// and unknown when neither names one. A market code missing from Markets is unknown too.
func (c *Client) MarketInfo() (MarketInfo, bool) {
	if c.marketInfo == nil {
		return MarketInfo{}, false
	}
	return *c.marketInfo, true
}

// environment returns the environment s names, in any case; "" is live.
func environment(s string) (Environment, error) {
	if s == "" {
		return EnvironmentLive, nil
	}
	env := Environment(strings.ToLower(s))
	if _, ok := environmentURLs[env]; !ok {
		return "", fmt.Errorf("cru: %q is not an environment: use live or sandbox", s)
	}
	return env, nil
}

// marketOfBaseURL returns the code of the market a base URL is for: its host's first label when
// the host is {code}.<domain> for the domain of an environment's template, else explicit.
func marketOfBaseURL(base, explicit string) string {
	u, err := url.Parse(base)
	if err != nil {
		return explicit
	}
	host := strings.ToLower(u.Hostname())
	for _, template := range environmentURLs {
		// The generator makes every template https://{market}.<domain>/<path>.
		_, rest, _ := strings.Cut(template, "{market}")
		domain, _, _ := strings.Cut(rest, "/") // ".<domain>"
		if code, ok := strings.CutSuffix(host, domain); ok && isMarketCode(code) {
			return code
		}
	}
	return explicit
}
