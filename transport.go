package cru

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	maxBackoff    = 8 * time.Second
	maxRetryAfter = 30 * time.Second
)

type response struct {
	status int
	header http.Header
	body   []byte
}

func isWrite(method string) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return false
	}
	return true
}

// call encodes the request once, so a partner write's reference is the same on every retry.
func (c *Client) call(ctx context.Context, e endpoint) (*response, error) {
	write := isWrite(e.method)
	var payload []byte
	if e.body != nil || (write && c.partner) {
		var err error
		if payload, err = encodeBody(e.body); err != nil {
			return nil, err
		}
		if write && c.partner {
			if payload, err = withReference(payload); err != nil {
				return nil, err
			}
		}
	}
	u := c.baseURL + e.path
	if q := e.query.Encode(); q != "" {
		sep := "?"
		if strings.Contains(e.path, "?") {
			sep = "&"
		}
		u += sep + q
	}
	if _, err := url.Parse(u); err != nil {
		return nil, fmt.Errorf("cru: %w", err)
	}
	accept := e.accept
	if accept == "" {
		accept = "application/json"
	}
	// A connected app's write has no reference to make a retry safe, so it is never retried.
	return c.send(ctx, e.method, u, payload, accept, !write || c.partner)
}

func (c *Client) send(ctx context.Context, method, u string, payload []byte, accept string, retryable bool) (*response, error) {
	refreshed := false
	for attempt := 0; ; {
		token := c.currentToken()
		resp, err := c.attempt(ctx, method, u, payload, c.headers(accept, token, payload != nil))
		if err != nil {
			if ctx.Err() != nil {
				return nil, fmt.Errorf("cru: %s %s: %w", method, u, ctx.Err())
			}
			var ce *ConnectionError
			if !errors.As(err, &ce) || !retryable || attempt >= c.maxRetries {
				return nil, err
			}
			attempt++
			if err := c.sleep(ctx, backoff(attempt, 0, false)); err != nil {
				return nil, fmt.Errorf("cru: %s %s: %w", method, u, err)
			}
			continue
		}
		if resp.status < 400 {
			return resp, nil
		}
		apiErr := newAPIError(resp.status, resp.header, resp.body)
		if resp.status == http.StatusUnauthorized && !refreshed && c.canRefresh() {
			refreshed = true
			if err := c.refresh(ctx, token); err != nil {
				return nil, err
			}
			continue
		}
		inProgress := resp.status == http.StatusConflict && apiErr.Code == ErrorCodeRequestInProgress
		if !retryable || attempt >= c.maxRetries || !(retryStatus(resp.status) || inProgress) {
			return nil, apiErr
		}
		attempt++
		hint, ok := retryAfter(resp.header, apiErr.Details)
		if err := c.sleep(ctx, backoff(attempt, hint, ok)); err != nil {
			return nil, fmt.Errorf("cru: %s %s: %w", method, u, err)
		}
	}
}

func (c *Client) headers(accept, token string, hasBody bool) http.Header {
	h := http.Header{}
	h.Set("Accept", accept)
	h.Set("User-Agent", userAgent)
	if hasBody {
		h.Set("Content-Type", "application/json")
	}
	if c.partner {
		// Sent as the spec spells them, not canonicalised.
		h["CLIENT-KEY"] = []string{c.clientKey}
		h["CLIENT-SECRET"] = []string{c.clientSecret}
	} else {
		h.Set("Authorization", "Bearer "+token)
	}
	return h
}

// attempt sends one request and reads the whole answer. Only a *ConnectionError is retryable.
func (c *Client) attempt(ctx context.Context, method, u string, payload []byte, header http.Header) (*response, error) {
	if c.timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, c.timeout)
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
		return nil, connectionError(method, u, err)
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, connectionError(method, u, err)
	}
	return &response{status: resp.StatusCode, header: resp.Header, body: b}, nil
}

func connectionError(method, u string, err error) *ConnectionError {
	var ne net.Error
	timeout := errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &ne) && ne.Timeout())
	var ue *url.Error
	if errors.As(err, &ue) {
		err = ue.Err
	}
	return &ConnectionError{Method: method, URL: u, Err: err, timeout: timeout}
}

func retryStatus(status int) bool {
	switch status {
	case http.StatusTooManyRequests, http.StatusInternalServerError, http.StatusBadGateway,
		http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return true
	}
	return false
}

// retryAfter reads errors[0].details.retry_after, else the Retry-After header, in seconds.
func retryAfter(header http.Header, details map[string]any) (time.Duration, bool) {
	if v, ok := details["retry_after"].(float64); ok && v >= 0 {
		return seconds(v), true
	}
	if v, err := strconv.ParseFloat(strings.TrimSpace(header.Get("Retry-After")), 64); err == nil && v >= 0 {
		return seconds(v), true
	}
	return 0, false
}

func seconds(v float64) time.Duration {
	return time.Duration(min(v, maxRetryAfter.Seconds()) * float64(time.Second))
}

// backoff is 0.5s * 2^(attempt-1), at most 8s, with ±25% jitter, unless the API said how long
// to wait.
func backoff(attempt int, hint time.Duration, ok bool) time.Duration {
	if ok {
		return min(hint, maxRetryAfter)
	}
	d := maxBackoff
	if attempt <= 5 {
		d = min(500*time.Millisecond<<(attempt-1), maxBackoff)
	}
	return time.Duration(float64(d) * (0.75 + rand.Float64()/2))
}

func sleep(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

func (c *Client) currentToken() string {
	if c.partner {
		return ""
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.accessToken
}

func (c *Client) canRefresh() bool {
	if c.partner {
		return false
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.refreshToken != "" && c.oauthClientID != ""
}

// refresh swaps the access token that was rejected for a new one. When another call has
// already replaced it, that one is used and no second refresh is made.
func (c *Client) refresh(ctx context.Context, rejected string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.accessToken != rejected {
		return nil
	}
	form := url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {c.refreshToken},
		"client_id":     {c.oauthClientID},
	}
	if c.oauthClientSecret != "" {
		form.Set("client_secret", c.oauthClientSecret)
	}
	header := http.Header{}
	header.Set("Accept", "application/json")
	header.Set("Content-Type", "application/x-www-form-urlencoded")
	header.Set("User-Agent", userAgent)
	resp, err := c.attempt(ctx, http.MethodPost, c.tokenURL, []byte(form.Encode()), header)
	if err != nil {
		if ctx.Err() != nil {
			return fmt.Errorf("cru: POST %s: %w", c.tokenURL, ctx.Err())
		}
		return err
	}
	tok := Token{}
	raw, f := splitObject(resp.body)
	tok.Raw = raw
	decodeField(f, "access_token", &tok.AccessToken)
	decodeField(f, "refresh_token", &tok.RefreshToken)
	decodeField(f, "token_type", &tok.TokenType)
	decodeInt(f, "expires_in", &tok.ExpiresIn)
	decodeField(f, "scope", &tok.Scope)
	if resp.status != http.StatusOK || tok.AccessToken == "" {
		return refreshError(resp, f)
	}
	c.accessToken = tok.AccessToken
	if tok.RefreshToken != "" {
		c.refreshToken = tok.RefreshToken
	}
	if c.onRefresh != nil {
		c.onRefresh(tok)
	}
	return nil
}

// refreshError is an authentication error (401) whatever the token endpoint answered, with its
// status in Details["token_status"] and its OAuth error as Code when it sent no Cru code.
func refreshError(resp *response, f map[string]json.RawMessage) *APIError {
	e := newAPIError(resp.status, resp.header, resp.body)
	if e.Code == "" {
		decodeField(f, "error", &e.Code)
	}
	var description string
	decodeField(f, "error_description", &description)
	switch {
	case description != "" && len(e.Errors) == 0:
		e.Message = description
	case len(e.Errors) == 0:
		e.Message = "the access token could not be refreshed"
	}
	e.Message = "refreshing the access token: " + e.Message
	if e.Details == nil {
		e.Details = map[string]any{}
	}
	e.Details["token_status"] = resp.status
	e.StatusCode = http.StatusUnauthorized
	return e
}
