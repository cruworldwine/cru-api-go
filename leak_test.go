package cru

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
// retry, a staged write, a photo, an attempt that times out halfway through the body, a token
// refresh and a refused redirect.
func exercise(t *testing.T, transport http.RoundTripper) {
	t.Helper()
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
			reply(w, 202, ok(stagedJSON("bid.place")))
		default:
			reply(w, 200, ok(`{"bid":`+bidJSON(testBidCode)+`}`))
		}
	}))
	defer srv.Close()

	noSleep := func(ctx context.Context, _ time.Duration) error { return ctx.Err() }
	p, err := NewClient(WithClientKey("ck", "cs"), WithBaseURL(srv.URL+"/api/v1"), WithHTTPClient(&http.Client{Transport: transport}))
	if err != nil {
		t.Fatal(err)
	}
	p.sleep = noSleep
	a, err := NewClient(WithAccessToken("at_1"), WithOAuthRefresh("rt_1", "app", "", nil), WithBaseURL(srv.URL+"/api/v1"),
		WithHTTPClient(&http.Client{Transport: transport}))
	if err != nil {
		t.Fatal(err)
	}
	a.sleep = noSleep

	ctx := context.Background()
	for code, check := range map[string]func(error) bool{
		"OK":      func(err error) bool { return err == nil },
		"MISSING": func(err error) bool { return errors.Is(err, ErrNotFound) },
		"FLAKY":   func(err error) bool { return err == nil },
		"SLOW": func(err error) bool {
			var ce *ConnectionError
			return errors.As(err, &ce) && ce.Timeout()
		},
		"MOVED": func(err error) bool { return errors.Is(err, errRedirect) },
	} {
		if _, err := p.Bids.Get(ctx, code, WithRequestTimeout(200*time.Millisecond), WithRequestMaxRetries(1)); !check(err) {
			t.Errorf("%s: %v", code, err)
		}
	}
	if _, err := p.Photos.Get(ctx, "p1"); err != nil {
		t.Error(err)
	}
	if _, err := a.Bids.Get(ctx, "OK"); err != nil {
		t.Error(err)
	}
	if w, err := a.Bids.Place(ctx, &BidsPlaceParams{LWIN18: testLWIN, Quantity: 1, Price: 1}); err != nil || !w.Staged {
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
	transport := &http.Transport{}
	exercise(t, transport)
	transport.CloseIdleConnections()
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
