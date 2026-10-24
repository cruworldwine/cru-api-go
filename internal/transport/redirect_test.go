package transport

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"testing"
)

func TestRedirects(t *testing.T) {
	other := newServer(t, always(200, ok(`{}`)))
	s := newServer(t, func(w http.ResponseWriter, r *http.Request, n int) {
		switch r.URL.Path {
		case "/api/v1/away":
			http.Redirect(w, r, other.URL+"/api/v1/ping", http.StatusTemporaryRedirect)
		case "/api/v1/here":
			http.Redirect(w, r, "/api/v1/ping", http.StatusTemporaryRedirect)
		default:
			reply(w, 200, ok(`{}`))
		}
	})
	c, sleeps := partner(t, s)
	ctx := context.Background()
	_, err := c.Do(ctx, Request{Method: http.MethodGet, Path: "/away"}, nil)
	var ce *ConnectionError
	if !errors.Is(err, ErrRedirect) || errors.As(err, &ce) || len(other.requests()) != 0 || len(sleeps.all()) != 0 {
		t.Errorf("a redirect to another host: %v; %d requests there, %d retries", err, len(other.requests()), len(sleeps.all()))
	}
	if _, err := c.Do(ctx, Request{Method: http.MethodGet, Path: "/here"}, nil); err != nil {
		t.Fatalf("a redirect on the same host: %v", err)
	}
	if got := s.requests()[2]; got.Path != "/ping" || got.Header.Get("Client-Secret") != "cs_test" {
		t.Errorf("the redirect went to %s with secret %q", got.Path, got.Header.Get("Client-Secret"))
	}
}

func TestRedirectPolicy(t *testing.T) {
	mustURL := func(s string) *url.URL {
		u, err := url.Parse(s)
		if err != nil {
			t.Fatal(err)
		}
		return u
	}
	called := false
	guarded := guardRedirects(&http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
		called = true
		return nil
	}})
	from := &http.Request{URL: mustURL("https://uk.cruworldwine.com/api/v1/a")}
	for to, want := range map[string]error{
		"http://uk.cruworldwine.com/api/v1/b":     ErrRedirect,
		"https://evil.example/api/v1/b":           ErrRedirect,
		"https://UK.cruworldwine.com/api/v1/b":    nil,
		"https://uk.cruworldwine.com:8443/api/v1": ErrRedirect,
	} {
		called = false
		err := guarded.CheckRedirect(&http.Request{URL: mustURL(to)}, []*http.Request{from})
		if !errors.Is(err, want) || (want == nil && !called) {
			t.Errorf("%s: %v, the client's own policy called: %v", to, err, called)
		}
	}
	via := make([]*http.Request, maxRedirects)
	for i := range via {
		via[i] = from
	}
	if err := guardRedirects(nil).CheckRedirect(&http.Request{URL: from.URL}, via); err == nil {
		t.Error("an endless redirect loop was followed")
	}
	if err := guardRedirects(nil).CheckRedirect(&http.Request{URL: from.URL}, via[:1]); err != nil {
		t.Errorf("one redirect on the same host: %v", err)
	}
}
