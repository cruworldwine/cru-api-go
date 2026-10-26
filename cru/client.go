package cru

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/cruworldwine/cru-api-go/internal/apijson"
	"github.com/cruworldwine/cru-api-go/internal/caller"
	"github.com/cruworldwine/cru-api-go/internal/transport"
	sdkversion "github.com/cruworldwine/cru-api-go/internal/version"
)

const (
	defaultMarket     = "uk"
	defaultMaxRetries = 2
	defaultTimeout    = 30 * time.Second
)

// Client calls the Cru Trading API. Create one with NewClient; it is safe for concurrent use.
//
// The endpoints are grouped by resource in the embedded Resources: client.Bids,
// client.Offers and so on, each a service from the resource's package.
type Client struct {
	Resources

	tr         *transport.Client
	caller     *caller.Caller
	marketInfo *MarketInfo
}

// Token is what the OAuth token endpoint answered when the client refreshed its access token.
type Token struct {
	AccessToken string
	// RefreshToken is the new refresh token. Refresh tokens rotate: the old one no longer works,
	// so store this one.
	RefreshToken string
	TokenType    string
	// ExpiresIn is the access token's lifetime in seconds.
	ExpiresIn int64
	Scope     string
	// Raw is the token endpoint's answer as it was received.
	Raw json.RawMessage
}

// Option configures a Client.
type Option func(*config)

type config struct {
	clientKey, clientSecret string
	accessToken             string
	refreshToken            string
	oauthClientID           string
	oauthClientSecret       string
	onRefresh               func(Token)
	market                  string
	environment             string
	baseURL                 string
	httpClient              *http.Client
	maxRetries              *int
	timeout                 *time.Duration
	logger                  *slog.Logger
	appInfo                 string
}

// WithClientKey authenticates with a partner credential, sent as the CLIENT-KEY and
// CLIENT-SECRET headers. Its writes run at once. It wins over an access token.
func WithClientKey(key, secret string) Option {
	return func(c *config) { c.clientKey, c.clientSecret = key, secret }
}

// WithAccessToken authenticates as a connected app, with an OAuth access token sent as
// "Authorization: Bearer". Its writes are staged for the customer to confirm.
func WithAccessToken(token string) Option {
	return func(c *config) { c.accessToken = token }
}

// WithOAuthRefresh lets a connected app refresh its access token: on a 401, the client posts
// refreshToken to the market's /api/oauth/token once per call and retries with the new access
// token. clientSecret may be empty for a public client.
//
// Refresh tokens rotate, so onRefresh, if not nil, is called with every new token pair to
// store. It is called while the client holds its token lock, in order, and must not call the
// client.
func WithOAuthRefresh(refreshToken, clientID, clientSecret string, onRefresh func(Token)) Option {
	return func(c *config) {
		c.refreshToken, c.oauthClientID, c.oauthClientSecret, c.onRefresh = refreshToken, clientID, clientSecret, onRefresh
	}
}

// WithMarket picks the market's host: https://{market}.cruworldwine.com/api/v1 on the live
// API, or https://{market}.crusandbox.com/api/v1 on the sandbox (WithEnvironment). It defaults
// to uk. A credential works on its own market only. A market is a code such as uk or hk, in
// any case; NewClient refuses anything else. With a base URL, it only names the market
// Client.MarketInfo reports when the base URL's host is not a market's.
func WithMarket(market string) Option {
	return func(c *config) { c.market = market }
}

// WithBaseURL sets the API's base URL, ending in /api/v1, in place of the market's host in
// the environment. It wins over WithMarket and WithEnvironment.
func WithBaseURL(baseURL string) Option {
	return func(c *config) { c.baseURL = baseURL }
}

// WithHTTPClient sets the HTTP client requests are sent with. It defaults to a client using
// http.DefaultTransport.
//
// The client uses a copy of hc whose redirect policy refuses a redirect to another host, or
// from https to http, since that would send the credentials there; within the same host it
// defers to hc.CheckRedirect.
func WithHTTPClient(hc *http.Client) Option {
	return func(c *config) { c.httpClient = hc }
}

// WithLogger logs each request the client sends to l: every attempt at Debug, with its method,
// path, status, attempt number, duration and request id, and every retry and access token
// refresh at Info. Credentials, tokens, query strings and bodies are never logged. A nil l, the
// default, logs nothing.
func WithLogger(l *slog.Logger) Option {
	return func(c *config) { c.logger = l }
}

// WithAppInfo names the application built on the SDK in the User-Agent header, as
// "name/version (url)" after the SDK's own product, so Cru can tell its traffic apart. version
// and url may be empty.
func WithAppInfo(name, version, url string) Option {
	return func(c *config) { c.appInfo = sdkversion.AppInfo(name, version, url) }
}

// WithMaxRetries sets how many times a failed read, or partner write, is retried. It defaults
// to 2; 0 turns retries off.
func WithMaxRetries(n int) Option {
	return func(c *config) { c.maxRetries = &n }
}

// WithTimeout limits each attempt, retries included, to d. It defaults to 30 seconds; 0 leaves
// attempts unlimited, bounded only by the context and the HTTP client.
func WithTimeout(d time.Duration) Option {
	return func(c *config) { c.timeout = &d }
}

// NewClient returns a client configured by opts.
//
// What opts leave unset is read from environment variables: CRU_CLIENT_KEY and
// CRU_CLIENT_SECRET (unless an access token was given), CRU_ACCESS_TOKEN (unless a key pair was
// given), CRU_MARKET, CRU_ENVIRONMENT and CRU_BASE_URL. It is an error to end up with neither a
// key pair nor an access token, or with an environment other than live or sandbox.
func NewClient(opts ...Option) (*Client, error) {
	var cfg config
	for _, opt := range opts {
		if opt != nil {
			opt(&cfg)
		}
	}
	explicitKey := cfg.clientKey != "" && cfg.clientSecret != ""
	if cfg.accessToken == "" {
		cfg.clientKey = orEnv(cfg.clientKey, "CRU_CLIENT_KEY")
		cfg.clientSecret = orEnv(cfg.clientSecret, "CRU_CLIENT_SECRET")
	}
	if !explicitKey {
		cfg.accessToken = orEnv(cfg.accessToken, "CRU_ACCESS_TOKEN")
	}
	partner := cfg.clientKey != "" && cfg.clientSecret != ""
	if !partner && cfg.accessToken == "" {
		return nil, errors.New("cru: give a client key and secret (WithClientKey or CRU_CLIENT_KEY and CRU_CLIENT_SECRET), or an access token (WithAccessToken or CRU_ACCESS_TOKEN)")
	}

	env, err := environment(orEnv(cfg.environment, "CRU_ENVIRONMENT"))
	if err != nil {
		return nil, err
	}
	market := strings.ToLower(orEnv(cfg.market, "CRU_MARKET"))
	base := strings.TrimRight(orEnv(cfg.baseURL, "CRU_BASE_URL"), "/")
	if base == "" {
		if market == "" {
			market = defaultMarket
		}
		if !isMarketCode(market) {
			return nil, fmt.Errorf("cru: %q is not a market code such as uk", market)
		}
		base = strings.ReplaceAll(environmentURLs[env], "{market}", market)
	} else {
		market = marketOfBaseURL(base, market)
	}
	tc := transport.Config{
		BaseURL: base, Partner: partner, ClientKey: cfg.clientKey, ClientSecret: cfg.clientSecret,
		AccessToken: cfg.accessToken, RefreshToken: cfg.refreshToken, OAuthClientID: cfg.oauthClientID,
		OAuthClientSecret: cfg.oauthClientSecret, HTTPClient: cfg.httpClient, MaxRetries: defaultMaxRetries,
		Timeout: defaultTimeout, UserAgent: sdkversion.UserAgent(cfg.appInfo), Logger: cfg.logger,
	}
	if cfg.onRefresh != nil {
		onRefresh := cfg.onRefresh
		tc.OnRefresh = func(t transport.Token) { onRefresh(Token(t)) }
	}
	if cfg.maxRetries != nil {
		tc.MaxRetries = *cfg.maxRetries
	}
	if cfg.timeout != nil {
		tc.Timeout = *cfg.timeout
	}
	tr, err := transport.New(tc)
	if err != nil {
		return nil, err
	}
	c := &Client{tr: tr, caller: caller.New(tr)}
	if m, ok := LookupMarket(market); ok {
		c.marketInfo = &m
	}
	c.Resources = newResources(c.caller)
	return c, nil
}

func orEnv(v, name string) string {
	if v != "" {
		return v
	}
	return os.Getenv(name)
}

// isMarketCode accepts a DNS label, so a market can only ever pick a host under an environment's
// domain, cruworldwine.com or crusandbox.com.
func isMarketCode(s string) bool {
	if len(s) == 0 || len(s) > 63 || s[0] == '-' || s[len(s)-1] == '-' {
		return false
	}
	for _, r := range s {
		if (r < 'a' || r > 'z') && (r < '0' || r > '9') && r != '-' {
			return false
		}
	}
	return true
}

// BaseURL returns the base URL requests are sent to, such as https://uk.cruworldwine.com/api/v1.
func (c *Client) BaseURL() string { return c.tr.BaseURL() }

// IsPartner reports whether the client uses a partner credential, whose writes run at once,
// rather than a connected app's access token, whose writes are staged.
func (c *Client) IsPartner() bool { return c.tr.Partner() }

// String describes the client without its credentials, so printing one never leaks them.
func (c *Client) String() string {
	kind := "connected app"
	if c.IsPartner() {
		kind = "partner credential"
	}
	return "cru.Client(" + c.BaseURL() + ", " + kind + ")"
}

// GoString is String, so %#v never prints the credentials either.
func (c *Client) GoString() string { return c.String() }

// Response is the envelope of a call made with Client.Do.
type Response struct {
	// StatusCode is the HTTP status.
	StatusCode int
	// Header is the response's headers.
	Header http.Header
	// Status is the envelope's status, OK on success.
	Status string
	// Message is the envelope's message, for people.
	Message string
	// Meta and Data are the envelope's meta and data as they were received.
	Meta json.RawMessage
	Data json.RawMessage
	// Body is the response body as it was received.
	Body []byte
}

// Do calls any endpoint, including one this SDK does not know yet, and returns the envelope.
//
// path is relative to the base URL, such as /bids. body, if not nil, is sent as JSON: a
// []byte or json.RawMessage as it is, anything else marshalled. It is handled like every other
// call: an error answer is an *APIError, reads and partner writes are retried, a partner
// write gets a reference when its body has none, and opts apply.
func (c *Client) Do(ctx context.Context, method, path string, query url.Values, body any, opts ...RequestOption) (*Response, error) {
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	resp, err := caller.Call(ctx, c.caller, caller.Endpoint{Method: strings.ToUpper(method), Path: path, Query: query, Body: body, Opts: opts})
	if err != nil {
		return nil, err
	}
	out := &Response{StatusCode: resp.Status, Header: resp.Header, Body: resp.Body}
	_, f := apijson.Split(resp.Body)
	apijson.Decode(f, "status", &out.Status)
	apijson.Decode(f, "message", &out.Message)
	out.Meta = f["meta"]
	out.Data = f["data"]
	return out, nil
}
