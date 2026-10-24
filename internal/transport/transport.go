// Package transport sends the SDK's HTTP requests.
//
// It builds each request (URL, query, JSON body, headers and credentials), merges in the call's
// own options, and sends it: retrying with back-off what is safe to retry, keeping a partner
// write's reference the same on every attempt, refreshing an expired OAuth access token once,
// refusing redirects that would carry the credentials elsewhere, and logging every attempt
// without a secret. It answers with the raw response, and knows nothing of the API's models:
// package cru decodes them and turns a failed Response into its own error types.
package transport

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/cruworldwine/cru-api-go/internal/apijson"
)

// Config configures a Client. Credentials are a partner's key and secret, or a connected app's
// access token with, optionally, what it takes to refresh it.
type Config struct {
	// BaseURL is the API's base URL, such as https://uk.cruworldwine.com/api/v1.
	BaseURL string
	// Partner selects the partner credential, ClientKey and ClientSecret, over AccessToken.
	Partner                 bool
	ClientKey, ClientSecret string
	AccessToken             string
	// RefreshToken, OAuthClientID and OAuthClientSecret refresh the access token on a 401;
	// OnRefresh, if not nil, is handed every new token pair.
	RefreshToken      string
	OAuthClientID     string
	OAuthClientSecret string
	OnRefresh         func(Token)
	// HTTPClient sends the requests; nil is a client using http.DefaultTransport. The Client
	// uses a copy whose redirect policy keeps the credentials on their host.
	HTTPClient *http.Client
	// MaxRetries is how many times a call may be retried, and Timeout limits each attempt
	// (0 for no limit).
	MaxRetries int
	Timeout    time.Duration
	UserAgent  string
	// Logger, if not nil, gets every attempt at Debug and every retry and refresh at Info.
	Logger *slog.Logger
}

// Client sends requests to the API. It is safe for concurrent use.
type Client struct {
	baseURL      string
	tokenURL     string
	partner      bool
	clientKey    string
	clientSecret string
	httpClient   *http.Client
	maxRetries   int
	timeout      time.Duration
	userAgent    string
	logger       *slog.Logger

	// Sleep waits before a retry, or returns the context's error. Tests replace it.
	Sleep func(context.Context, time.Duration) error

	mu                sync.RWMutex
	accessToken       string
	refreshToken      string
	oauthClientID     string
	oauthClientSecret string
	onRefresh         func(Token)
}

// New returns a Client for cfg. The base URL must be absolute.
func New(cfg Config) (*Client, error) {
	u, err := url.Parse(cfg.BaseURL)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return nil, fmt.Errorf("cru: the base URL %q is not an absolute URL", cfg.BaseURL)
	}
	return &Client{
		baseURL:           strings.TrimRight(cfg.BaseURL, "/"),
		tokenURL:          u.Scheme + "://" + u.Host + "/api/oauth/token",
		partner:           cfg.Partner,
		clientKey:         cfg.ClientKey,
		clientSecret:      cfg.ClientSecret,
		httpClient:        guardRedirects(cfg.HTTPClient),
		maxRetries:        max(cfg.MaxRetries, 0),
		timeout:           max(cfg.Timeout, 0),
		userAgent:         cfg.UserAgent,
		logger:            cfg.Logger,
		Sleep:             sleep,
		accessToken:       cfg.AccessToken,
		refreshToken:      cfg.RefreshToken,
		oauthClientID:     cfg.OAuthClientID,
		oauthClientSecret: cfg.OAuthClientSecret,
		onRefresh:         cfg.OnRefresh,
	}, nil
}

// BaseURL returns the base URL requests are sent to.
func (c *Client) BaseURL() string { return c.baseURL }

// Partner reports whether the client sends a partner credential rather than an access token.
func (c *Client) Partner() bool { return c.partner }

// Response is an answer, as it was received.
type Response struct {
	Status int
	Header http.Header
	Body   []byte
	// Attempts is how many requests the call had sent when this answer came, retries included.
	Attempts int
}

// Do sends r with the call's options o, which may be nil, and returns the answer.
//
// An answer with a status of 400 or more, once retries are spent, is a *StatusError; a request
// that got no answer is a *ConnectionError; a cancelled context is returned as its error. A
// read, or a partner's write, is retried; a connected app's write never is, as it has no
// reference to make a second attempt safe.
func (c *Client) Do(ctx context.Context, r Request, o *Options) (*Response, error) {
	if o == nil {
		o = &Options{}
	}
	if o.Err != nil {
		return nil, o.Err
	}
	p, err := c.prepare(r, o)
	if err != nil {
		return nil, err
	}
	return c.send(ctx, p, o)
}

func (c *Client) send(ctx context.Context, p *prepared, o *Options) (*Response, error) {
	refreshed := false
	attempts := 0
	for retries := 0; ; {
		token := c.currentToken()
		attempts++
		start := time.Now()
		resp, err := c.attempt(ctx, p.method, p.url, p.payload, c.headers(p, token), p.timeout)
		c.logAttempt(ctx, p, attempts, resp, err, time.Since(start))
		if err != nil {
			if ctx.Err() != nil {
				return nil, fmt.Errorf("cru: %s %s: %w", p.method, p.url, ctx.Err())
			}
			var ce *ConnectionError
			if !errors.As(err, &ce) || !p.retryable || retries >= p.maxRetries {
				return nil, err
			}
			retries++
			wait := backoff(retries, 0, false)
			c.logRetry(ctx, p, attempts+1, wait, slog.String("error", ce.Err.Error()))
			if err := c.Sleep(ctx, wait); err != nil {
				return nil, fmt.Errorf("cru: %s %s: %w", p.method, p.url, err)
			}
			continue
		}
		resp.Attempts = attempts
		if o.Observe != nil {
			o.Observe(resp)
		}
		if resp.Status < 400 {
			return resp, nil
		}
		if resp.Status == http.StatusUnauthorized && !refreshed && c.canRefresh() {
			refreshed = true
			if err := c.refresh(ctx, token, p.timeout); err != nil {
				return nil, err
			}
			continue
		}
		code, details := apijson.FirstError(resp.Body)
		inProgress := resp.Status == http.StatusConflict && code == codeRequestInProgress
		if !p.retryable || retries >= p.maxRetries || (!retryStatus(resp.Status) && !inProgress) {
			return nil, &StatusError{Response: resp}
		}
		retries++
		hint, ok := retryAfter(resp.Header, details)
		wait := backoff(retries, hint, ok)
		c.logRetry(ctx, p, attempts+1, wait, slog.Int("status", resp.Status), slog.String("code", code))
		if err := c.Sleep(ctx, wait); err != nil {
			return nil, fmt.Errorf("cru: %s %s: %w", p.method, p.url, err)
		}
	}
}

// attempt sends one request and reads the whole answer. Only a *ConnectionError is retryable.
func (c *Client) attempt(ctx context.Context, method, u string, payload []byte, header http.Header, timeout time.Duration) (*Response, error) {
	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}
	var body io.Reader
	if payload != nil {
		body = bytes.NewReader(payload)
	}
	req, err := http.NewRequestWithContext(ctx, method, u, body)
	if err != nil {
		return nil, fmt.Errorf("cru: %w", err)
	}
	req.Header = header
	resp, err := c.httpClient.Do(req)
	if err != nil {
		if errors.Is(err, ErrRedirect) {
			return nil, fmt.Errorf("cru: %s %s: %w", method, u, ErrRedirect)
		}
		return nil, connectionError(method, u, err)
	}
	defer func() { _ = resp.Body.Close() }()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, connectionError(method, u, err)
	}
	return &Response{Status: resp.StatusCode, Header: resp.Header, Body: b}, nil
}
