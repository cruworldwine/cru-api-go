package cru

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
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

// request is one call, encoded once, so a partner write's reference is the same on every retry.
type request struct {
	method  string
	url     string
	path    string // the URL's path, for logs
	payload []byte
	accept  string
	header  http.Header // the call's own headers, from WithRequestHeader
	// retryable is false for a connected app's write, which has no reference to make a retry safe.
	retryable  bool
	maxRetries int
	timeout    time.Duration
}

func isWrite(method string) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return false
	}
	return true
}

func (c *Client) call(ctx context.Context, e endpoint) (*response, error) {
	o := newRequestOptions(e.opts)
	if o.err != nil {
		return nil, o.err
	}
	write := isWrite(e.method)
	if len(o.body) > 0 && !write {
		return nil, fmt.Errorf("cru: WithExtraBodyField: %s %s sends no body", e.method, e.path)
	}
	var payload []byte
	if e.body != nil || len(o.body) > 0 || (write && c.partner) {
		var err error
		if payload, err = encodeBody(e.body); err != nil {
			return nil, err
		}
		if len(o.body) > 0 {
			if payload, err = withFields(payload, o.body); err != nil {
				return nil, err
			}
		}
		if write && c.partner {
			if payload, err = withReference(payload); err != nil {
				return nil, err
			}
		}
	}
	query := e.query
	if len(o.query) > 0 {
		query = url.Values{}
		for k, v := range e.query {
			query[k] = v
		}
		for k, v := range o.query {
			query[k] = v
		}
	}
	u := c.baseURL + e.path
	if q := query.Encode(); q != "" {
		sep := "?"
		if strings.Contains(e.path, "?") {
			sep = "&"
		}
		u += sep + q
	}
	parsed, err := url.Parse(u)
	if err != nil {
		return nil, fmt.Errorf("cru: %w", err)
	}
	r := request{
		method: e.method, url: u, path: parsed.Path, payload: payload, accept: e.accept, header: o.header,
		retryable: !write || c.partner, maxRetries: c.maxRetries, timeout: c.timeout,
	}
	if r.accept == "" {
		r.accept = "application/json"
	}
	if o.maxRetries != nil {
		r.maxRetries = *o.maxRetries
	}
	if o.timeout != nil {
		r.timeout = *o.timeout
	}
	resp, err := c.send(ctx, r, o)
	o.record(resp, err)
	return resp, err
}

func (c *Client) send(ctx context.Context, r request, o *requestOptions) (*response, error) {
	refreshed := false
	for attempt := 0; ; {
		token := c.currentToken()
		o.attempts++
		start := time.Now()
		resp, err := c.attempt(ctx, r.method, r.url, r.payload, c.headers(r, token), r.timeout)
		c.logAttempt(ctx, r, o.attempts, resp, err, time.Since(start))
		if err != nil {
			if ctx.Err() != nil {
				return nil, fmt.Errorf("cru: %s %s: %w", r.method, r.url, ctx.Err())
			}
			var ce *ConnectionError
			if !errors.As(err, &ce) || !r.retryable || attempt >= r.maxRetries {
				return nil, err
			}
			attempt++
			wait := backoff(attempt, 0, false)
			c.logRetry(ctx, r, o.attempts+1, wait, slog.String("error", ce.Err.Error()))
			if err := c.sleep(ctx, wait); err != nil {
				return nil, fmt.Errorf("cru: %s %s: %w", r.method, r.url, err)
			}
			continue
		}
		if resp.status < 400 {
			return resp, nil
		}
		apiErr := newAPIError(resp.status, resp.header, resp.body)
		if resp.status == http.StatusUnauthorized && !refreshed && c.canRefresh() {
			refreshed = true
			if err := c.refresh(ctx, token, r.timeout); err != nil {
				return nil, err
			}
			continue
		}
		inProgress := resp.status == http.StatusConflict && apiErr.Code == ErrorCodeRequestInProgress
		if !r.retryable || attempt >= r.maxRetries || (!retryStatus(resp.status) && !inProgress) {
			return nil, apiErr
		}
		attempt++
		hint, ok := retryAfter(resp.header, apiErr.Details)
		wait := backoff(attempt, hint, ok)
		c.logRetry(ctx, r, o.attempts+1, wait, slog.Int("status", resp.status), slog.String("code", apiErr.Code))
		if err := c.sleep(ctx, wait); err != nil {
			return nil, fmt.Errorf("cru: %s %s: %w", r.method, r.url, err)
		}
	}
}

func (c *Client) headers(r request, token string) http.Header {
	h := http.Header{}
	h.Set("Accept", r.accept)
	h.Set("User-Agent", c.userAgent)
	if r.payload != nil {
		h.Set("Content-Type", "application/json")
	}
	for k, v := range r.header {
		h[k] = v
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
func (c *Client) attempt(ctx context.Context, method, u string, payload []byte, header http.Header, timeout time.Duration) (*response, error) {
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
		if errors.Is(err, errRedirect) {
			return nil, fmt.Errorf("cru: %s %s: %w", method, u, errRedirect)
		}
		return nil, connectionError(method, u, err)
	}
	defer func() { _ = resp.Body.Close() }()
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

// retryAfter reads errors[0].details.retry_after, else the Retry-After header, in seconds or as
// an HTTP date.
func retryAfter(header http.Header, details map[string]any) (time.Duration, bool) {
	if v, ok := details["retry_after"].(float64); ok && v >= 0 {
		return seconds(v), true
	}
	h := strings.TrimSpace(header.Get("Retry-After"))
	if v, err := strconv.ParseFloat(h, 64); err == nil && v >= 0 {
		return seconds(v), true
	}
	if t, err := http.ParseTime(h); err == nil {
		return min(max(time.Until(t), 0), maxRetryAfter), true
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
	return time.Duration(float64(d) * (0.75 + rand.Float64()/2)) //nolint:gosec // jitter needs no cryptographic randomness
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

func (c *Client) logs(ctx context.Context, level slog.Level) bool {
	return c.logger != nil && c.logger.Enabled(ctx, level)
}

// logAttempt logs one attempt at Debug. Only the method, the URL's path, the status and the
// request id are logged: never a header, the query string or a body.
func (c *Client) logAttempt(ctx context.Context, r request, attempt int, resp *response, err error, d time.Duration) {
	if !c.logs(ctx, slog.LevelDebug) {
		return
	}
	attrs := []slog.Attr{
		slog.String("method", r.method), slog.String("path", r.path), slog.Int("attempt", attempt),
		slog.Duration("duration", d),
	}
	if resp != nil {
		attrs = append(attrs, slog.Int("status", resp.status))
		if id := resp.header.Get("X-Request-Id"); id != "" {
			attrs = append(attrs, slog.String("request_id", id))
		}
	}
	var ce *ConnectionError
	if errors.As(err, &ce) {
		attrs = append(attrs, slog.String("error", ce.Err.Error()), slog.Bool("timeout", ce.Timeout()))
	}
	c.logger.LogAttrs(ctx, slog.LevelDebug, "cru: request", attrs...)
}

func (c *Client) logRetry(ctx context.Context, r request, next int, wait time.Duration, reason ...slog.Attr) {
	if !c.logs(ctx, slog.LevelInfo) {
		return
	}
	attrs := append([]slog.Attr{
		slog.String("method", r.method), slog.String("path", r.path), slog.Int("attempt", next),
		slog.Duration("wait", wait),
	}, reason...)
	c.logger.LogAttrs(ctx, slog.LevelInfo, "cru: retrying request", attrs...)
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
func (c *Client) refresh(ctx context.Context, rejected string, timeout time.Duration) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.accessToken != rejected {
		if c.logs(ctx, slog.LevelDebug) {
			c.logger.LogAttrs(ctx, slog.LevelDebug, "cru: access token already refreshed by another call")
		}
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
	header.Set("User-Agent", c.userAgent)
	start := time.Now()
	resp, err := c.attempt(ctx, http.MethodPost, c.tokenURL, []byte(form.Encode()), header, timeout)
	if err != nil {
		c.logRefresh(ctx, start, 0, "", err)
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
		e := refreshError(resp, f)
		c.logRefresh(ctx, start, resp.status, e.Code, e)
		return e
	}
	c.logRefresh(ctx, start, resp.status, "", nil)
	c.accessToken = tok.AccessToken
	if tok.RefreshToken != "" {
		c.refreshToken = tok.RefreshToken
	}
	if c.onRefresh != nil {
		c.onRefresh(tok)
	}
	return nil
}

// logRefresh logs a token refresh at Info, with the token endpoint's status and error code only.
func (c *Client) logRefresh(ctx context.Context, start time.Time, status int, code string, err error) {
	if !c.logs(ctx, slog.LevelInfo) {
		return
	}
	attrs := []slog.Attr{slog.Duration("duration", time.Since(start))}
	if status != 0 {
		attrs = append(attrs, slog.Int("status", status))
	}
	if code != "" {
		attrs = append(attrs, slog.String("code", code))
	}
	var ce *ConnectionError
	if errors.As(err, &ce) {
		attrs = append(attrs, slog.String("error", ce.Err.Error()))
	}
	msg := "cru: access token refreshed"
	if err != nil {
		msg = "cru: access token refresh failed"
	}
	c.logger.LogAttrs(ctx, slog.LevelInfo, msg, attrs...)
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
