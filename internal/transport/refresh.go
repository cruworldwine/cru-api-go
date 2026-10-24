package transport

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"time"

	"github.com/cruworldwine/cru-api-go/internal/apijson"
)

// Token is what the OAuth token endpoint answered a refresh with.
type Token struct {
	AccessToken  string
	RefreshToken string
	TokenType    string
	ExpiresIn    int64
	Scope        string
	Raw          json.RawMessage
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

// refresh swaps the access token that was rejected for a new one. It holds the token lock while
// it asks, so concurrent calls rejected with the same token make one refresh between them: the
// others find the token already replaced and use the new one. A refused refresh is a
// *StatusError with Refresh set.
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
		c.logRefresh(ctx, start, nil, err)
		if ctx.Err() != nil {
			return fmt.Errorf("cru: POST %s: %w", c.tokenURL, ctx.Err())
		}
		return err
	}
	tok := Token{}
	raw, f := apijson.Split(resp.Body)
	tok.Raw = raw
	apijson.Decode(f, "access_token", &tok.AccessToken)
	apijson.Decode(f, "refresh_token", &tok.RefreshToken)
	apijson.Decode(f, "token_type", &tok.TokenType)
	apijson.DecodeInt(f, "expires_in", &tok.ExpiresIn)
	apijson.Decode(f, "scope", &tok.Scope)
	if resp.Status != http.StatusOK || tok.AccessToken == "" {
		e := &StatusError{Response: resp, Refresh: true}
		c.logRefresh(ctx, start, resp, e)
		return e
	}
	c.logRefresh(ctx, start, resp, nil)
	c.accessToken = tok.AccessToken
	if tok.RefreshToken != "" {
		c.refreshToken = tok.RefreshToken
	}
	if c.onRefresh != nil {
		c.onRefresh(tok)
	}
	return nil
}
