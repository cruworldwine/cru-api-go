package cru

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"testing"
)

// syncBuffer is a bytes.Buffer safe for the handler and the test to share.
type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

func (s *syncBuffer) entries(t *testing.T) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(s.String()), "\n") {
		if line == "" {
			continue
		}
		var e map[string]any
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			t.Fatalf("not a JSON log line: %s", line)
		}
		out = append(out, e)
	}
	return out
}

func newLogger(level slog.Level) (*slog.Logger, *syncBuffer) {
	buf := &syncBuffer{}
	return slog.New(slog.NewJSONHandler(buf, &slog.HandlerOptions{Level: level, AddSource: true})), buf
}

// TestLoggingNeverLeaksSecrets drives every path that logs, with a secret in every place one
// could hide, and checks that none of them reaches the log.
func TestLoggingNeverLeaksSecrets(t *testing.T) {
	secrets := map[string]string{ //nolint:gosec // invented values, there to prove they are never logged
		"client key":         "ck_SECRET_KEY_0001",
		"client secret":      "cs_SECRET_VALUE_0002",
		"old access token":   "at_SECRET_OLD_0003",
		"old refresh token":  "rt_SECRET_OLD_0004",
		"new access token":   "at_SECRET_NEW_0005",
		"new refresh token":  "rt_SECRET_NEW_0006",
		"app secret":         "app_SECRET_0007",
		"request body":       "body_SECRET_0008",
		"response body":      "answer_SECRET_0009",
		"query string":       "query_SECRET_0010",
		"error description":  "desc_SECRET_0011",
		"extra header value": "header_SECRET_0012",
	}
	logger, buf := newLogger(slog.LevelDebug)
	ctx := context.Background()

	var mu sync.Mutex
	valid, refresh := "not issued yet", secrets["old refresh token"]
	refuse := false
	s := newServer(t, func(w http.ResponseWriter, r *http.Request, n int) {
		mu.Lock()
		defer mu.Unlock()
		w.Header().Set("X-Request-Id", "req_log")
		switch {
		case r.URL.Path == "/api/oauth/token":
			_ = r.ParseForm()
			if refuse || r.PostForm.Get("refresh_token") != refresh {
				reply(w, 400, `{"error":"invalid_grant","error_description":"`+secrets["error description"]+`"}`)
				return
			}
			valid, refresh = secrets["new access token"], secrets["new refresh token"]
			reply(w, 200, `{"access_token":"`+valid+`","refresh_token":"`+refresh+`","token_type":"Bearer","expires_in":3600}`)
		case r.Header.Get("Authorization") != "" && r.Header.Get("Authorization") != "Bearer "+valid:
			reply(w, 401, apiError("AUTH_INVALID_CREDENTIALS", "Expired."))
		case n == 0:
			reply(w, 503, apiError("SERVICE_UNAVAILABLE", secrets["response body"]))
		default:
			reply(w, 200, ok(`{"bid":`+bidJSON(testBidCode)+`,"note":"`+secrets["response body"]+`"}`))
		}
	})

	p, _ := partner(t, s, WithClientKey(secrets["client key"], secrets["client secret"]), WithLogger(logger))
	if _, err := p.Bids.Place(ctx, &BidsPlaceParams{LWIN18: testLWIN, Quantity: 1, Price: 1, ClientRef: String(secrets["request body"])},
		WithRequestHeader("X-Extra", secrets["extra header value"])); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Do(ctx, "GET", "/bids", url.Values{"client_ref": {secrets["query string"]}}, nil, WithExtraQuery("q", secrets["query string"])); err != nil {
		t.Fatal(err)
	}

	a, _ := newClient(t, s, WithAccessToken(secrets["old access token"]), WithLogger(logger),
		WithOAuthRefresh(secrets["old refresh token"], "app_test", secrets["app secret"], nil))
	if _, err := a.Bids.Get(ctx, testBidCode); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	valid, refuse = "rotated elsewhere", true
	mu.Unlock()
	if _, err := a.Bids.Get(ctx, testBidCode); !errors.Is(err, ErrAuthentication) {
		t.Fatalf("a refused refresh: %v", err)
	}

	broken, _ := partner(t, s, WithClientKey(secrets["client key"], secrets["client secret"]), WithLogger(logger),
		WithHTTPClient(&http.Client{Transport: &flakyTransport{n: 1, err: errors.New("connection reset"), next: http.DefaultTransport}}))
	if _, err := broken.Bids.Get(ctx, testBidCode); err != nil {
		t.Fatal(err)
	}

	out := buf.String()
	for what, secret := range secrets {
		if strings.Contains(out, secret) {
			t.Errorf("the log holds the %s", what)
		}
	}
	for _, want := range []string{
		`"msg":"cru: request"`, `"msg":"cru: retrying request"`, `"msg":"cru: access token refreshed"`,
		`"msg":"cru: access token refresh failed"`, `"code":"invalid_grant"`, `"error":"connection reset"`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("the log lacks %s", want)
		}
	}
}

func TestLoggingAttempts(t *testing.T) {
	s := newServer(t, failing(1, 503, apiError("SERVICE_UNAVAILABLE", "Busy."), ok(`{"bid":`+bidJSON(testBidCode)+`}`)))
	logger, buf := newLogger(slog.LevelDebug)
	c, _ := partner(t, s, WithLogger(logger))
	if _, err := c.Bids.Get(context.Background(), testBidCode+"/x", WithExtraQuery("a", "b")); err != nil {
		t.Fatal(err)
	}
	entries := buf.entries(t)
	if len(entries) != 3 {
		t.Fatalf("%d entries, want an attempt, a retry and an attempt:\n%s", len(entries), buf)
	}
	first, retry, second := entries[0], entries[1], entries[2]
	if first["level"] != "DEBUG" || first["method"] != "GET" || first["path"] != "/api/v1/bids/"+testBidCode+"/x" ||
		first["attempt"] != 1.0 || first["status"] != 503.0 || first["duration"] == nil {
		t.Errorf("first attempt %v", first)
	}
	if retry["level"] != "INFO" || retry["attempt"] != 2.0 || retry["status"] != 503.0 || retry["code"] != "SERVICE_UNAVAILABLE" || retry["wait"] == nil {
		t.Errorf("retry %v", retry)
	}
	if second["attempt"] != 2.0 || second["status"] != 200.0 {
		t.Errorf("second attempt %v", second)
	}

	info, buf := newLogger(slog.LevelInfo)
	s = newServer(t, always(200, ok(`{"bid":`+bidJSON(testBidCode)+`}`)))
	c, _ = partner(t, s, WithLogger(info))
	if _, err := c.Bids.Get(context.Background(), testBidCode); err != nil || buf.String() != "" {
		t.Errorf("an Info logger got the Debug attempt: %q, %v", buf.String(), err)
	}
}
