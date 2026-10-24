package transport

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// trackingTransport counts the response bodies it hands out and the ones closed.
type trackingTransport struct {
	next           http.RoundTripper
	opened, closed atomic.Int32
}

func (t *trackingTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	resp, err := t.next.RoundTrip(r)
	if err != nil {
		return nil, err
	}
	t.opened.Add(1)
	resp.Body = &trackedBody{ReadCloser: resp.Body, closed: &t.closed}
	return resp, nil
}

type trackedBody struct {
	io.ReadCloser
	once   sync.Once
	closed *atomic.Int32
}

func (b *trackedBody) Close() error {
	b.once.Do(func() { b.closed.Add(1) })
	return b.ReadCloser.Close()
}

// exercise makes a call down every path that gets an answer: a success, an error answer, a
// retry, a staged write, a binary answer, an attempt that times out halfway through the body, a
// token refresh and a refused redirect.
func exercise(t *testing.T, rt http.RoundTripper) {
	t.Helper()
	other := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Error("a redirect was followed to another host")
	}))
	defer other.Close()
	var flaky atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/oauth/token":
			reply(w, 200, `{"access_token":"at_2","refresh_token":"rt_2"}`)
		case r.Header.Get("Authorization") == "Bearer at_1":
			reply(w, 401, apiError("AUTH_INVALID_CREDENTIALS", "Expired."))
		case strings.HasSuffix(r.URL.Path, "/MISSING"):
			reply(w, 404, apiError("BID_NOT_FOUND", "No."))
		case strings.HasSuffix(r.URL.Path, "/FLAKY") && flaky.Add(1) == 1:
			reply(w, 503, apiError("SERVICE_UNAVAILABLE", "Busy."))
		case strings.HasSuffix(r.URL.Path, "/SLOW"):
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"status":"OK","data":`)
			w.(http.Flusher).Flush()
			<-r.Context().Done()
		case strings.HasSuffix(r.URL.Path, "/MOVED"):
			http.Redirect(w, r, other.URL+"/api/v1/bids/OK", http.StatusFound)
		case strings.HasPrefix(r.URL.Path, "/api/v1/photos/"):
			_, _ = w.Write([]byte("jpeg"))
		case r.Method == http.MethodPost:
			reply(w, 202, ok(`{"confirmation":{"code":"cnf_test"}}`))
		default:
			reply(w, 200, ok(`{}`))
		}
	}))
	defer srv.Close()

	noSleep := func(ctx context.Context, _ time.Duration) error { return ctx.Err() }
	base := Config{BaseURL: srv.URL + "/api/v1", MaxRetries: 1, Timeout: 200 * time.Millisecond, HTTPClient: &http.Client{Transport: rt}}
	pc := base
	pc.Partner, pc.ClientKey, pc.ClientSecret = true, "ck", "cs"
	p, err := New(pc)
	if err != nil {
		t.Fatal(err)
	}
	p.Sleep = noSleep
	ac := base
	ac.AccessToken, ac.RefreshToken, ac.OAuthClientID = "at_1", "rt_1", "app"
	a, err := New(ac)
	if err != nil {
		t.Fatal(err)
	}
	a.Sleep = noSleep

	ctx := context.Background()
	call := func(c *Client, method, path string) error {
		_, err := c.Do(ctx, Request{Method: method, Path: path}, nil)
		return err
	}
	for path, check := range map[string]func(error) bool{
		"/bids/OK":      func(err error) bool { return err == nil },
		"/bids/MISSING": func(err error) bool { return statusOf(err) == 404 },
		"/bids/FLAKY":   func(err error) bool { return err == nil },
		"/bids/SLOW": func(err error) bool {
			var ce *ConnectionError
			return errors.As(err, &ce) && ce.Timeout
		},
		"/bids/MOVED": func(err error) bool { return errors.Is(err, ErrRedirect) },
		"/photos/p1":  func(err error) bool { return err == nil },
	} {
		if err := call(p, http.MethodGet, path); !check(err) {
			t.Errorf("%s: %v", path, err)
		}
	}
	if err := call(a, http.MethodGet, "/bids/OK"); err != nil {
		t.Error(err)
	}
	if err := call(a, http.MethodPost, "/bids"); err != nil {
		t.Errorf("staged write: %v", err)
	}
}

func TestResponseBodiesAreClosed(t *testing.T) {
	tr := &trackingTransport{next: http.DefaultTransport}
	exercise(t, tr)
	if tr.opened.Load() < 10 || tr.opened.Load() != tr.closed.Load() {
		t.Errorf("%d response bodies opened, %d closed", tr.opened.Load(), tr.closed.Load())
	}
}

func TestNoGoroutineLeaks(t *testing.T) {
	before := runtime.NumGoroutine()
	rt := &http.Transport{}
	exercise(t, rt)
	rt.CloseIdleConnections()
	deadline := time.Now().Add(5 * time.Second)
	for runtime.NumGoroutine() > before && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if n := runtime.NumGoroutine(); n > before {
		stacks := make([]byte, 1<<20)
		stacks = stacks[:runtime.Stack(stacks, true)]
		t.Errorf("%d goroutines left running, %d before:\n%s", n, before, stacks)
	}
}
