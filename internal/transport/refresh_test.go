package transport

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// oauthServer accepts only the current access token and rotates both tokens on every refresh.
type oauthServer struct {
	*testServer
	mu       sync.Mutex
	valid    string
	refresh  string
	issued   int
	refuse   bool
	forms    []map[string]string
	apiCalls atomic.Int32
}

func newOAuthServer(t *testing.T, access, refresh string) *oauthServer {
	o := &oauthServer{valid: access, refresh: refresh}
	o.testServer = newServer(t, func(w http.ResponseWriter, r *http.Request, _ int) {
		o.mu.Lock()
		defer o.mu.Unlock()
		if r.URL.Path == "/api/oauth/token" {
			_ = r.ParseForm()
			form := map[string]string{"content-type": r.Header.Get("Content-Type"), "user-agent": r.Header.Get("User-Agent")}
			for k := range r.PostForm {
				form[k] = r.PostForm.Get(k)
			}
			o.forms = append(o.forms, form)
			if o.refuse || r.PostForm.Get("refresh_token") != o.refresh {
				reply(w, 400, `{"error":"invalid_grant","error_description":"The refresh token is invalid."}`)
				return
			}
			o.issued++
			o.valid = fmt.Sprintf("at_%d", o.issued)
			o.refresh = fmt.Sprintf("rt_%d", o.issued)
			reply(w, 200, fmt.Sprintf(`{"access_token":%q,"refresh_token":%q,"token_type":"Bearer","expires_in":3600,"scope":"bids:read"}`, o.valid, o.refresh))
			return
		}
		o.apiCalls.Add(1)
		if r.Header.Get("Authorization") != "Bearer "+o.valid {
			reply(w, 401, apiError("AUTH_INVALID_CREDENTIALS", "The access token has expired."))
			return
		}
		reply(w, 200, ok(`{}`))
	})
	return o
}

func (o *oauthServer) tokenForms() []map[string]string {
	o.mu.Lock()
	defer o.mu.Unlock()
	return append([]map[string]string(nil), o.forms...)
}

func (o *oauthServer) expire() {
	o.mu.Lock()
	o.valid = "expired"
	o.mu.Unlock()
}

func withRefresh(access, refresh, clientID, secret string, onRefresh func(Token)) func(*Config) {
	return func(c *Config) {
		c.AccessToken, c.RefreshToken, c.OAuthClientID, c.OAuthClientSecret, c.OnRefresh = access, refresh, clientID, secret, onRefresh
	}
}

func TestRefreshAndRotation(t *testing.T) {
	o := newOAuthServer(t, "at_new_unused", "rt_0")
	var got []Token
	c, _ := newClient(t, o.testServer, withRefresh("at_old", "rt_0", "app_test", "app_secret", func(tok Token) { got = append(got, tok) }))

	resp, err := get(c)
	if err != nil {
		t.Fatal(err)
	}
	if resp.Attempts != 2 {
		t.Errorf("the refreshed call made %d attempts, want 2", resp.Attempts)
	}
	if len(got) != 1 || got[0].AccessToken != "at_1" || got[0].RefreshToken != "rt_1" || got[0].ExpiresIn != 3600 ||
		got[0].TokenType != "Bearer" || got[0].Scope != "bids:read" || len(got[0].Raw) == 0 {
		t.Fatalf("OnRefresh got %+v", got)
	}
	form := o.tokenForms()[0]
	if form["grant_type"] != "refresh_token" || form["refresh_token"] != "rt_0" || form["client_id"] != "app_test" ||
		form["client_secret"] != "app_secret" || form["content-type"] != "application/x-www-form-urlencoded" ||
		form["user-agent"] != "cruworldwine-go/test" {
		t.Errorf("token request %v", form)
	}

	// The next call uses the new access token, and the next refresh the rotated refresh token.
	if _, err := get(c); err != nil || len(got) != 1 {
		t.Fatalf("a valid token was refreshed again: %v", err)
	}
	o.expire()
	if _, err := get(c); err != nil {
		t.Fatal(err)
	}
	if forms := o.tokenForms(); len(forms) != 2 || forms[1]["refresh_token"] != "rt_1" || len(got) != 2 || got[1].RefreshToken != "rt_2" {
		t.Errorf("rotation: forms %v, tokens %+v", forms, got)
	}
	if c.currentToken() != "at_2" {
		t.Errorf("the client kept %s", c.currentToken())
	}
}

func TestRefreshWithoutClientSecretOrCallback(t *testing.T) {
	o := newOAuthServer(t, "x", "rt_0")
	c, _ := newClient(t, o.testServer, withRefresh("at_old", "rt_0", "app_public", "", nil))
	if _, err := get(c); err != nil {
		t.Fatal(err)
	}
	if _, sent := o.tokenForms()[0]["client_secret"]; sent {
		t.Error("a public client sent client_secret")
	}
}

func TestRefreshKeepsTheRefreshTokenWhenNoneComes(t *testing.T) {
	s := newServer(t, func(w http.ResponseWriter, r *http.Request, n int) {
		switch {
		case r.URL.Path == "/api/oauth/token":
			reply(w, 200, `{"access_token":"at_new"}`)
		case r.Header.Get("Authorization") == "Bearer at_new":
			reply(w, 200, ok(`{}`))
		default:
			reply(w, 401, apiError("AUTH_INVALID_CREDENTIALS", "Expired."))
		}
	})
	c, _ := newClient(t, s, withRefresh("at_old", "rt_keep", "app", "", nil))
	if _, err := get(c); err != nil || c.refreshToken != "rt_keep" {
		t.Errorf("%v; refresh token %q", err, c.refreshToken)
	}
}

func TestFailedRefresh(t *testing.T) {
	o := newOAuthServer(t, "x", "rt_0")
	o.mu.Lock()
	o.refuse = true
	o.mu.Unlock()
	called := false
	c, _ := newClient(t, o.testServer, withRefresh("at_old", "rt_0", "app_test", "", func(Token) { called = true }))
	_, err := get(c)
	var se *StatusError
	if !errors.As(err, &se) || !se.Refresh || se.Response.Status != 400 {
		t.Fatalf("want the token endpoint's refusal, got %v", err)
	}
	if se.Error() != "cru: refreshing the access token: HTTP 400" {
		t.Errorf("Error() = %s", se.Error())
	}
	if called || o.apiCalls.Load() != 1 || c.currentToken() != "at_old" {
		t.Errorf("callback %v, %d API calls, token %s", called, o.apiCalls.Load(), c.currentToken())
	}
}

func TestRefreshConnectionFailure(t *testing.T) {
	s := newServer(t, func(w http.ResponseWriter, r *http.Request, n int) {
		if r.URL.Path == "/api/oauth/token" {
			<-r.Context().Done()
			return
		}
		reply(w, 401, apiError("AUTH_INVALID_CREDENTIALS", "Expired."))
	})
	c, _ := newClient(t, s, withRefresh("at_old", "rt_0", "app", "", nil), func(c *Config) { c.Timeout = 20 * time.Millisecond })
	_, err := get(c)
	var ce *ConnectionError
	if !errors.As(err, &ce) || !ce.Timeout {
		t.Errorf("a token endpoint that never answers: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	s = newServer(t, func(w http.ResponseWriter, r *http.Request, n int) {
		if r.URL.Path == "/api/oauth/token" {
			cancel()
			<-r.Context().Done()
			return
		}
		reply(w, 401, apiError("AUTH_INVALID_CREDENTIALS", "Expired."))
	})
	c, _ = newClient(t, s, withRefresh("at_old", "rt_0", "app", "", nil))
	if _, err := c.Do(ctx, Request{Method: http.MethodGet, Path: testPath}, nil); !errors.Is(err, context.Canceled) {
		t.Errorf("a cancelled refresh: %v", err)
	}
}

func TestRefreshAtMostOncePerCall(t *testing.T) {
	s := newServer(t, func(w http.ResponseWriter, r *http.Request, n int) {
		if r.URL.Path == "/api/oauth/token" {
			reply(w, 200, `{"access_token":"at_also_rejected","refresh_token":"rt_1"}`)
			return
		}
		reply(w, 401, apiError("AUTH_INVALID_CREDENTIALS", "No."))
	})
	c, _ := newClient(t, s, withRefresh("at_old", "rt_0", "app_test", "", nil))
	if _, err := get(c); statusOf(err) != 401 {
		t.Fatalf("err = %v", err)
	}
	var paths []string
	for _, r := range s.requests() {
		paths = append(paths, r.Path)
	}
	if fmt.Sprint(paths) != "[/bids/BID-TEST-0001 /api/oauth/token /bids/BID-TEST-0001]" {
		t.Errorf("requests %v", paths)
	}
}

func TestNoRefreshWithoutRefreshToken(t *testing.T) {
	for name, mod := range map[string]func(*Config){
		"no refresh token": withRefresh("at_old", "", "app_test", "", nil),
		"no client id":     withRefresh("at_old", "rt_0", "", "", nil),
		"partner": func(c *Config) {
			withRefresh("", "rt_0", "app_test", "", nil)(c)
			asPartner(c)
		},
	} {
		o := newOAuthServer(t, "x", "rt_0")
		c, _ := newClient(t, o.testServer, mod)
		if _, err := get(c); statusOf(err) != 401 || len(o.tokenForms()) != 0 {
			t.Errorf("%s: %v, %d refreshes", name, err, len(o.tokenForms()))
		}
	}
}

func TestConcurrentCallsRefreshOnce(t *testing.T) {
	o := newOAuthServer(t, "x", "rt_0")
	var refreshes atomic.Int32
	logger, _ := newLogger()
	c, _ := newClient(t, o.testServer, withRefresh("at_old", "rt_0", "app_test", "", func(Token) { refreshes.Add(1) }),
		func(c *Config) { c.Logger = logger })
	var wg sync.WaitGroup
	for range 10 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := get(c); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if refreshes.Load() != 1 || len(o.tokenForms()) != 1 {
		t.Errorf("%d refreshes, %d token requests; a rotated refresh token works once", refreshes.Load(), len(o.tokenForms()))
	}
}
