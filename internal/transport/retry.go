package transport

import (
	"context"
	"math/rand/v2"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const (
	maxBackoff    = 8 * time.Second
	maxRetryAfter = 30 * time.Second

	// codeRequestInProgress answers a write whose reference an earlier request is still using.
	codeRequestInProgress = "REQUEST_IN_PROGRESS"
)

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

// backoff is 0.5s * 2^(retry-1), at most 8s, with ±25% jitter, unless the API said how long to
// wait.
func backoff(retry int, hint time.Duration, ok bool) time.Duration {
	if ok {
		return min(hint, maxRetryAfter)
	}
	d := maxBackoff
	if retry <= 5 {
		d = min(500*time.Millisecond<<(retry-1), maxBackoff)
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
