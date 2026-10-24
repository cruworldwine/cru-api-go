package transport

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestReadsRetryOnRetryableStatuses(t *testing.T) {
	for _, status := range []int{429, 500, 502, 503, 504} {
		s := newServer(t, failing(2, status, apiError("SERVICE_UNAVAILABLE", "Busy."), ok(`{}`)))
		c, sleeps := partner(t, s)
		resp, err := get(c)
		if err != nil || resp.Status != 200 || resp.Attempts != 3 {
			t.Fatalf("%d: %+v, %v", status, resp, err)
		}
		if len(s.requests()) != 3 || len(sleeps.all()) != 2 {
			t.Errorf("%d: %d requests, %d sleeps", status, len(s.requests()), len(sleeps.all()))
		}
	}
}

func TestNoRetryOnOtherStatuses(t *testing.T) {
	for _, tc := range []struct {
		status int
		code   string
	}{{400, "VALIDATION_UNKNOWN_PARAMETER"}, {401, "AUTH_INVALID_CREDENTIALS"}, {404, "BID_NOT_FOUND"},
		{409, "PRICE_CHANGED"}, {410, "CONFIRMATION_EXPIRED"}, {422, "TRADE_REFUSED"}} {
		s := newServer(t, always(tc.status, apiError(tc.code, "No.")))
		c, _ := partner(t, s)
		_, err := get(c)
		var se *StatusError
		if !errors.As(err, &se) || se.Response.Status != tc.status || se.Refresh || len(s.requests()) != 1 {
			t.Errorf("%d %s: %v after %d requests", tc.status, tc.code, err, len(s.requests()))
		}
		if se != nil && (se.Error() != fmt.Sprintf("cru: HTTP %d", tc.status) || !strings.Contains(string(se.Response.Body), tc.code)) {
			t.Errorf("%d: %q", tc.status, se.Error())
		}
	}
}

func TestRetriesRunOut(t *testing.T) {
	s := newServer(t, always(503, apiError("SERVICE_UNAVAILABLE", "Busy.")))
	c, _ := partner(t, s, func(c *Config) { c.MaxRetries = 3 })
	if _, err := get(c); statusOf(err) != 503 || len(s.requests()) != 4 {
		t.Errorf("%v after %d requests", err, len(s.requests()))
	}
	s = newServer(t, always(503, apiError("SERVICE_UNAVAILABLE", "Busy.")))
	c, _ = partner(t, s, func(c *Config) { c.MaxRetries = -1 })
	if _, err := get(c); err == nil || len(s.requests()) != 1 {
		t.Errorf("no retries made %d requests", len(s.requests()))
	}
}

func TestPartnerWritesRetry(t *testing.T) {
	s := newServer(t, failing(1, 502, `<html>Bad gateway</html>`, ok(`{}`)))
	c, _ := partner(t, s)
	if _, err := c.Do(context.Background(), Request{Method: http.MethodDelete, Path: testPath}, nil); err != nil {
		t.Fatal(err)
	}
	if len(s.requests()) != 2 {
		t.Errorf("%d requests, want 2", len(s.requests()))
	}
}

func TestConnectedAppWritesNeverRetry(t *testing.T) {
	for _, status := range []int{429, 500, 503} {
		s := newServer(t, always(status, apiError("SERVICE_UNAVAILABLE", "Busy.")))
		c, sleeps := app(t, s)
		_, err := post(c, map[string]any{"quantity": 1}, func(o *Options) { o.SetMaxRetries(5) })
		if statusOf(err) != status || len(s.requests()) != 1 || len(sleeps.all()) != 0 {
			t.Errorf("%d: %d requests", status, len(s.requests()))
		}
	}
	s := newServer(t, always(409, apiError("REQUEST_IN_PROGRESS", "Running.")))
	c, _ := app(t, s)
	if _, err := c.Do(context.Background(), Request{Method: http.MethodDelete, Path: "/offers/OFFER-TEST-1"}, nil); statusOf(err) != 409 || len(s.requests()) != 1 {
		t.Errorf("%v after %d requests", err, len(s.requests()))
	}
	// A connected app's reads still retry.
	s = newServer(t, failing(1, 500, apiError("INTERNAL_ERROR", "Oops."), ok(`{}`)))
	c, _ = app(t, s)
	if _, err := get(c); err != nil || len(s.requests()) != 2 {
		t.Errorf("read: %v after %d requests", err, len(s.requests()))
	}
}

func TestRequestInProgressRetries(t *testing.T) {
	s := newServer(t, failing(1, 409, apiError("REQUEST_IN_PROGRESS", "Running."), ok(`{}`)))
	c, _ := partner(t, s)
	if _, err := post(c, nil); err != nil || len(s.requests()) != 2 {
		t.Errorf("%v after %d requests", err, len(s.requests()))
	}
	reqs := s.requests()
	if reqs[0].Body != reqs[1].Body || !strings.Contains(reqs[0].Body, `"reference":"sdk_`) {
		t.Errorf("the reference changed across retries: %s, %s", reqs[0].Body, reqs[1].Body)
	}
}

func TestRetryAfter(t *testing.T) {
	withDetails := func(v string) string {
		return `{"status":"ERROR","errors":[{"code":"RATE_LIMIT_EXCEEDED","message":"Slow down.","details":{"retry_after":` + v + `}}]}`
	}
	cases := []struct {
		name   string
		header string
		body   string
		want   time.Duration
	}{
		{"header", "3", apiError("RATE_LIMIT_EXCEEDED", "Slow down."), 3 * time.Second},
		{"decimal header", "1.5", apiError("RATE_LIMIT_EXCEEDED", "Slow down."), 1500 * time.Millisecond},
		{"details", "", withDetails("7"), 7 * time.Second},
		{"details beat the header", "2", withDetails("5"), 5 * time.Second},
		{"capped", "120", apiError("RATE_LIMIT_EXCEEDED", "Slow down."), 30 * time.Second},
	}
	for _, tc := range cases {
		s := newServer(t, func(w http.ResponseWriter, r *http.Request, n int) {
			if n == 0 {
				if tc.header != "" {
					w.Header().Set("Retry-After", tc.header)
				}
				reply(w, 429, tc.body)
				return
			}
			reply(w, 200, ok(`{}`))
		})
		c, sleeps := partner(t, s)
		if _, err := get(c); err != nil {
			t.Fatal(err)
		}
		if got := sleeps.all(); len(got) != 1 || got[0] != tc.want {
			t.Errorf("%s: slept %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestRetryAfterHTTPDate(t *testing.T) {
	at := func(d time.Duration) http.Header {
		return http.Header{"Retry-After": {time.Now().Add(d).UTC().Format(http.TimeFormat)}}
	}
	if d, ok := retryAfter(at(time.Hour), nil); !ok || d != maxRetryAfter {
		t.Errorf("a date an hour away: %v, %v", d, ok)
	}
	if d, ok := retryAfter(at(-time.Hour), nil); !ok || d != 0 {
		t.Errorf("a date in the past: %v, %v", d, ok)
	}
	if d, ok := retryAfter(at(10*time.Second), nil); !ok || d <= 8*time.Second || d > 10*time.Second {
		t.Errorf("a date 10s away: %v, %v", d, ok)
	}
	if _, ok := retryAfter(http.Header{"Retry-After": {"soon"}}, nil); ok {
		t.Error("an unreadable Retry-After was taken")
	}
}

func TestBackoff(t *testing.T) {
	for retry, base := range map[int]time.Duration{
		1: 500 * time.Millisecond, 2: time.Second, 3: 2 * time.Second, 4: 4 * time.Second,
		5: 8 * time.Second, 6: 8 * time.Second, 60: 8 * time.Second,
	} {
		lo, hi := time.Duration(float64(base)*0.75), time.Duration(float64(base)*1.25)
		for range 50 {
			if d := backoff(retry, 0, false); d < lo || d > hi {
				t.Fatalf("retry %d: %v outside [%v, %v]", retry, d, lo, hi)
			}
		}
	}
	if d := backoff(1, 45*time.Second, true); d != 30*time.Second {
		t.Errorf("an API hint is capped at 30s, got %v", d)
	}
}

// flakyTransport fails the first n round trips with a network error.
type flakyTransport struct {
	n     int32
	calls atomic.Int32
	err   error
	next  http.RoundTripper
}

func (f *flakyTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	if f.calls.Add(1) <= f.n {
		if r.Body != nil {
			_, _ = io.Copy(io.Discard, r.Body)
		}
		return nil, f.err
	}
	return f.next.RoundTrip(r)
}

func TestNetworkErrors(t *testing.T) {
	refused := &net.OpError{Op: "dial", Net: "tcp", Err: errors.New("connection refused")}
	s := newServer(t, always(201, ok(`{}`)))

	flaky := &flakyTransport{n: 2, err: refused, next: http.DefaultTransport}
	c, sleeps := partner(t, s, func(c *Config) { c.HTTPClient = &http.Client{Transport: flaky} })
	if _, err := post(c, nil); err != nil {
		t.Fatal(err)
	}
	if flaky.calls.Load() != 3 || len(sleeps.all()) != 2 {
		t.Errorf("%d round trips, %d sleeps", flaky.calls.Load(), len(sleeps.all()))
	}

	flaky = &flakyTransport{n: 1, err: refused, next: http.DefaultTransport}
	c, _ = app(t, s, func(c *Config) { c.HTTPClient = &http.Client{Transport: flaky} })
	_, err := post(c, nil)
	var ce *ConnectionError
	if !errors.As(err, &ce) || ce.Timeout || flaky.calls.Load() != 1 {
		t.Fatalf("connected app write: %v after %d round trips", err, flaky.calls.Load())
	}
	if !errors.Is(err, refused) || ce.Method != "POST" || !strings.HasSuffix(ce.URL, "/api/v1/bids") || !strings.Contains(ce.Error(), "connection refused") {
		t.Errorf("the cause is lost: %#v", ce)
	}
}

func TestAttemptTimeout(t *testing.T) {
	s := newServer(t, func(w http.ResponseWriter, r *http.Request, n int) {
		if n == 0 {
			<-r.Context().Done()
			return
		}
		reply(w, 200, ok(`{}`))
	})
	c, _ := partner(t, s, func(c *Config) { c.Timeout = 50 * time.Millisecond })
	if _, err := get(c); err != nil {
		t.Fatalf("the timed-out attempt was not retried: %v", err)
	}

	s = newServer(t, func(w http.ResponseWriter, r *http.Request, n int) { <-r.Context().Done() })
	c, _ = partner(t, s, func(c *Config) { c.Timeout = time.Hour; c.MaxRetries = 1 })
	_, err := get(c, func(o *Options) { o.SetTimeout(20 * time.Millisecond) })
	var ce *ConnectionError
	if !errors.As(err, &ce) || !ce.Timeout || len(s.requests()) != 2 {
		t.Errorf("want a timeout ConnectionError after 2 attempts, got %v after %d", err, len(s.requests()))
	}
}

func TestCancelledContextStopsRetries(t *testing.T) {
	t.Run("while waiting to retry", func(t *testing.T) {
		s := newServer(t, always(503, apiError("SERVICE_UNAVAILABLE", "Busy.")))
		c, _ := partner(t, s, func(c *Config) { c.MaxRetries = 5 })
		ctx, cancel := context.WithCancel(context.Background())
		c.Sleep = func(ctx context.Context, d time.Duration) error {
			cancel()
			return sleep(ctx, d)
		}
		_, err := c.Do(ctx, Request{Method: http.MethodGet, Path: testPath}, nil)
		if !errors.Is(err, context.Canceled) || len(s.requests()) != 1 {
			t.Errorf("%v after %d requests", err, len(s.requests()))
		}
	})
	t.Run("while waiting after a network error", func(t *testing.T) {
		s := newServer(t, always(200, ok(`{}`)))
		flaky := &flakyTransport{n: 9, err: errors.New("reset"), next: http.DefaultTransport}
		c, _ := partner(t, s, func(c *Config) { c.HTTPClient = &http.Client{Transport: flaky} })
		ctx, cancel := context.WithCancel(context.Background())
		c.Sleep = func(ctx context.Context, d time.Duration) error {
			cancel()
			return ctx.Err()
		}
		if _, err := c.Do(ctx, Request{Method: http.MethodGet, Path: testPath}, nil); !errors.Is(err, context.Canceled) {
			t.Errorf("err = %v", err)
		}
	})
	t.Run("during a request", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		s := newServer(t, func(w http.ResponseWriter, r *http.Request, n int) {
			cancel()
			<-r.Context().Done()
		})
		c, _ := partner(t, s, func(c *Config) { c.MaxRetries = 5 })
		_, err := c.Do(ctx, Request{Method: http.MethodGet, Path: testPath}, nil)
		var ce *ConnectionError
		if !errors.Is(err, context.Canceled) || errors.As(err, &ce) || len(s.requests()) != 1 {
			t.Errorf("%v after %d requests", err, len(s.requests()))
		}
	})
	t.Run("a caller's deadline is not retried", func(t *testing.T) {
		s := newServer(t, func(w http.ResponseWriter, r *http.Request, n int) { <-r.Context().Done() })
		c, _ := partner(t, s, func(c *Config) { c.MaxRetries = 5 })
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
		defer cancel()
		_, err := c.Do(ctx, Request{Method: http.MethodGet, Path: testPath}, nil)
		if !errors.Is(err, context.DeadlineExceeded) || len(s.requests()) != 1 {
			t.Errorf("%v after %d requests", err, len(s.requests()))
		}
	})
}

func TestBadRequestsAreNotSent(t *testing.T) {
	s := newServer(t, always(200, ok(`{}`)))
	c, sleeps := partner(t, s)
	ctx := context.Background()
	for name, r := range map[string]Request{
		"a bad method":             {Method: "BAD METHOD", Path: "/bids"},
		"an unencodable body":      {Method: http.MethodPost, Path: "/bids", Body: func() {}},
		"a body not JSON":          {Method: http.MethodPost, Path: "/bids", Body: []byte(`{bad`)},
		"a path that is not a URL": {Method: http.MethodGet, Path: "/bids/%zz"},
	} {
		_, err := c.Do(ctx, r, nil)
		var ce *ConnectionError
		if err == nil || errors.As(err, &ce) {
			t.Errorf("%s: %v", name, err)
		}
	}
	if len(s.requests()) != 0 || len(sleeps.all()) != 0 {
		t.Errorf("%d requests and %d sleeps", len(s.requests()), len(sleeps.all()))
	}
}

func TestSleepHonoursContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	start := time.Now()
	if err := sleep(ctx, time.Hour); !errors.Is(err, context.Canceled) || time.Since(start) > time.Second {
		t.Errorf("sleep returned %v after %v", err, time.Since(start))
	}
	if err := sleep(context.Background(), time.Millisecond); err != nil {
		t.Error(err)
	}
	if err := sleep(ctx, 0); !errors.Is(err, context.Canceled) {
		t.Errorf("no wait on a cancelled context: %v", err)
	}
}
