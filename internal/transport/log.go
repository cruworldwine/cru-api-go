package transport

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/cruworldwine/cru-api-go/internal/apijson"
)

// What is logged is chosen field by field: the method, the URL's path, the status, the attempt,
// durations, the request id and error codes. Never a header, the query string or a body, where
// the credentials, tokens and customer data travel.

func (c *Client) logs(ctx context.Context, level slog.Level) bool {
	return c.logger != nil && c.logger.Enabled(ctx, level)
}

func (c *Client) logAttempt(ctx context.Context, p *prepared, attempt int, resp *Response, err error, d time.Duration) {
	if !c.logs(ctx, slog.LevelDebug) {
		return
	}
	attrs := []slog.Attr{
		slog.String("method", p.method), slog.String("path", p.path), slog.Int("attempt", attempt),
		slog.Duration("duration", d),
	}
	if resp != nil {
		attrs = append(attrs, slog.Int("status", resp.Status))
		if id := resp.Header.Get("X-Request-Id"); id != "" {
			attrs = append(attrs, slog.String("request_id", id))
		}
	}
	var ce *ConnectionError
	if errors.As(err, &ce) {
		attrs = append(attrs, slog.String("error", ce.Err.Error()), slog.Bool("timeout", ce.Timeout))
	}
	c.logger.LogAttrs(ctx, slog.LevelDebug, "cru: request", attrs...)
}

func (c *Client) logRetry(ctx context.Context, p *prepared, next int, wait time.Duration, reason ...slog.Attr) {
	if !c.logs(ctx, slog.LevelInfo) {
		return
	}
	attrs := append([]slog.Attr{
		slog.String("method", p.method), slog.String("path", p.path), slog.Int("attempt", next),
		slog.Duration("wait", wait),
	}, reason...)
	c.logger.LogAttrs(ctx, slog.LevelInfo, "cru: retrying request", attrs...)
}

// logRefresh logs a token refresh at Info, with the token endpoint's status and error code only.
func (c *Client) logRefresh(ctx context.Context, start time.Time, resp *Response, err error) {
	if !c.logs(ctx, slog.LevelInfo) {
		return
	}
	attrs := []slog.Attr{slog.Duration("duration", time.Since(start))}
	if resp != nil {
		attrs = append(attrs, slog.Int("status", resp.Status))
		if err != nil {
			code, _ := apijson.FirstError(resp.Body)
			if code == "" {
				_, f := apijson.Split(resp.Body)
				apijson.Decode(f, "error", &code)
			}
			if code != "" {
				attrs = append(attrs, slog.String("code", code))
			}
		}
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
