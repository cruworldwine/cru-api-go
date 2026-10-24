package cru

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"
)

const (
	defaultMarket     = "uk"
	defaultMaxRetries = 2
	defaultTimeout    = 30 * time.Second
)

// Client calls the Cru Trading API. Create one with NewClient; it is safe for concurrent use.
//
// The endpoints are grouped by resource in the embedded Resources: client.Bids,
// client.Offers and so on.
type Client struct {
	Resources

	baseURL      string
	tokenURL     string
	partner      bool
	clientKey    string
	clientSecret string
	httpClient   *http.Client
	maxRetries   int
	timeout      time.Duration
	sleep        func(context.Context, time.Duration) error

	mu                sync.RWMutex
	accessToken       string
	refreshToken      string
	oauthClientID     string
	oauthClientSecret string
	onRefresh         func(Token)
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
	baseURL                 string
	httpClient              *http.Client
	maxRetries              *int
	timeout                 *time.Duration
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

// WithMarket picks the market's host, https://{market}.cruworldwine.com/api/v1. It defaults
// to uk. A credential works on its own market only.
func WithMarket(market string) Option {
	return func(c *config) { c.market = market }
}

// WithBaseURL sets the API's base URL, ending in /api/v1, in place of the market's.
func WithBaseURL(baseURL string) Option {
	return func(c *config) { c.baseURL = baseURL }
}

// WithHTTPClient sets the HTTP client requests are sent with. It defaults to a client using
// http.DefaultTransport.
func WithHTTPClient(hc *http.Client) Option {
	return func(c *config) { c.httpClient = hc }
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
// What opts leave unset is read from the environment: CRU_CLIENT_KEY and CRU_CLIENT_SECRET
// (unless an access token was given), CRU_ACCESS_TOKEN (unless a key pair was given),
// CRU_MARKET and CRU_BASE_URL. It is an error to end up with neither a key pair nor an access
// token.
func NewClient(opts ...Option) (*Client, error) {
	var cfg config
	for _, opt := range opts {
		opt(&cfg)
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

	market := orEnv(cfg.market, "CRU_MARKET")
	if market == "" {
		market = defaultMarket
	}
	base := strings.TrimRight(orEnv(cfg.baseURL, "CRU_BASE_URL"), "/")
	if base == "" {
		base = "https://" + market + ".cruworldwine.com/api/v1"
	}
	u, err := url.Parse(base)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return nil, fmt.Errorf("cru: the base URL %q is not an absolute URL", base)
	}

	c := &Client{
		baseURL:           base,
		tokenURL:          u.Scheme + "://" + u.Host + "/api/oauth/token",
		partner:           partner,
		clientKey:         cfg.clientKey,
		clientSecret:      cfg.clientSecret,
		httpClient:        cfg.httpClient,
		maxRetries:        defaultMaxRetries,
		timeout:           defaultTimeout,
		sleep:             sleep,
		accessToken:       cfg.accessToken,
		refreshToken:      cfg.refreshToken,
		oauthClientID:     cfg.oauthClientID,
		oauthClientSecret: cfg.oauthClientSecret,
		onRefresh:         cfg.onRefresh,
	}
	if c.httpClient == nil {
		c.httpClient = &http.Client{}
	}
	if cfg.maxRetries != nil {
		c.maxRetries = max(*cfg.maxRetries, 0)
	}
	if cfg.timeout != nil {
		c.timeout = max(*cfg.timeout, 0)
	}
	c.Resources = newResources(c)
	return c, nil
}

func orEnv(v, name string) string {
	if v != "" {
		return v
	}
	return os.Getenv(name)
}

// BaseURL returns the base URL requests are sent to, such as https://uk.cruworldwine.com/api/v1.
func (c *Client) BaseURL() string { return c.baseURL }

// IsPartner reports whether the client uses a partner credential, whose writes run at once,
// rather than a connected app's access token, whose writes are staged.
func (c *Client) IsPartner() bool { return c.partner }

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
// call: an error answer is an *APIError, reads and partner writes are retried, and a partner
// write gets a reference when its body has none.
func (c *Client) Do(ctx context.Context, method, path string, query url.Values, body any) (*Response, error) {
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	resp, err := c.call(ctx, endpoint{method: strings.ToUpper(method), path: path, query: query, body: body})
	if err != nil {
		return nil, err
	}
	out := &Response{StatusCode: resp.status, Header: resp.header, Body: resp.body}
	_, f := splitObject(resp.body)
	decodeField(f, "status", &out.Status)
	decodeField(f, "message", &out.Message)
	out.Meta = f["meta"]
	out.Data = f["data"]
	return out, nil
}
