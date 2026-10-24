package cru

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/cruworldwine/cru-api-go/internal/apijson"
	"github.com/cruworldwine/cru-api-go/internal/transport"
)

// endpoint is one call, as the generated methods describe it.
type endpoint struct {
	method string
	path   string
	query  url.Values
	body   any
	key    string // the key of data the result is under; empty for data itself
	accept string
	opts   []RequestOption
}

// emptyObject is the body of a write that has no parameters.
var emptyObject = json.RawMessage("{}")

// call sends e and turns a failure into the SDK's error types.
func (c *Client) call(ctx context.Context, e endpoint) (*transport.Response, error) {
	resp, err := c.tr.Do(ctx, transport.Request{Method: e.method, Path: e.path, Query: e.query, Body: e.body, Accept: e.accept},
		requestOptions(e.opts))
	if err != nil {
		return nil, publicError(err)
	}
	return resp, nil
}

// publicError turns the transport's errors into *APIError and *ConnectionError.
func publicError(err error) error {
	var se *transport.StatusError
	if errors.As(err, &se) {
		if se.Refresh {
			return refreshError(se.Response)
		}
		return newAPIError(se.Response.Status, se.Response.Header, se.Response.Body)
	}
	var ce *transport.ConnectionError
	if errors.As(err, &ce) {
		return &ConnectionError{Method: ce.Method, URL: ce.URL, Err: ce.Err, timeout: ce.Timeout}
	}
	return err
}

func (c *Client) callEnvelope(ctx context.Context, e endpoint) (*transport.Response, map[string]json.RawMessage, error) {
	resp, err := c.call(ctx, e)
	if err != nil {
		return nil, nil, err
	}
	raw, f := apijson.Split(resp.Body)
	if raw != nil && f == nil {
		return nil, nil, fmt.Errorf("%w: %s %s answered HTTP %d with a body that is not a JSON object",
			ErrUnexpectedResponse, e.method, e.path, resp.Status)
	}
	return resp, f, nil
}

func getOne[T any](ctx context.Context, c *Client, e endpoint) (*T, error) {
	resp, f, err := c.callEnvelope(ctx, e)
	if err != nil {
		return nil, err
	}
	data, ok := apijson.Data(f, e.key)
	if !ok {
		return nil, fmt.Errorf("%w: %s %s answered without data.%s (request %s)",
			ErrUnexpectedResponse, e.method, e.path, e.key, apijson.RequestID(f, resp.Header))
	}
	v := new(T)
	if err := apijson.Unmarshal(data, v); err != nil {
		return nil, fmt.Errorf("%w: %s %s: %w", ErrUnexpectedResponse, e.method, e.path, err)
	}
	return v, nil
}

// itemsOf reads data[key] as a list. A missing key, null or {} is an empty list.
func itemsOf[T any](e endpoint, f map[string]json.RawMessage) ([]T, error) {
	raw, _ := apijson.Data(f, e.key)
	items, err := apijson.List[T](raw)
	if err != nil {
		return nil, fmt.Errorf("%w: %s %s: data.%s is not a list: %w", ErrUnexpectedResponse, e.method, e.path, e.key, err)
	}
	return items, nil
}

func getList[T any](ctx context.Context, c *Client, e endpoint) ([]T, error) {
	_, f, err := c.callEnvelope(ctx, e)
	if err != nil {
		return nil, err
	}
	return itemsOf[T](e, f)
}

func getPage[T any](ctx context.Context, c *Client, e endpoint) (*Page[T], error) {
	_, f, err := c.callEnvelope(ctx, e)
	if err != nil {
		return nil, err
	}
	items, err := itemsOf[T](e, f)
	if err != nil {
		return nil, err
	}
	p := &Page[T]{Items: items, Data: f["data"]}
	apijson.Decode(f, "meta", &p.Meta)
	if pg := p.Meta.Pagination; pg != nil {
		p.Limit, p.Offset = int(pg.Limit), int(pg.Offset)
		if pg.Total != nil {
			total := int(*pg.Total)
			p.Total = &total
		}
		if pg.HasMore != nil {
			p.HasMore = *pg.HasMore
		} else {
			p.HasMore = len(items) > 0 && len(items) == p.Limit
		}
	}
	p.next = func(ctx context.Context, offset int) (*Page[T], error) {
		next := e
		next.query = url.Values{}
		for k, v := range e.query {
			next.query[k] = v
		}
		next.query.Set("offset", strconv.Itoa(offset))
		return getPage[T](ctx, c, next)
	}
	return p, nil
}

func writeOf[T any](ctx context.Context, c *Client, e endpoint, result func(json.RawMessage) *T) (*Write[T], error) {
	resp, f, err := c.callEnvelope(ctx, e)
	if err != nil {
		return nil, err
	}
	w := &Write[T]{StatusCode: resp.Status, RequestID: apijson.RequestID(f, resp.Header)}
	if resp.Status == http.StatusAccepted {
		var staged Staged
		_ = apijson.Unmarshal(f["data"], &staged)
		w.Staged = true
		w.Confirmation = &staged.Confirmation
		return w, nil
	}
	w.Replayed = strings.EqualFold(resp.Header.Get("Idempotent-Replayed"), "true")
	w.Result = result(f["data"])
	return w, nil
}

func doWrite[T any](ctx context.Context, c *Client, e endpoint) (*Write[T], error) {
	return writeOf(ctx, c, e, func(data json.RawMessage) *T {
		v := new(T)
		_ = apijson.Unmarshal(data, v)
		return v
	})
}

func doConfirm(ctx context.Context, c *Client, e endpoint) (*Write[ActionResult], error) {
	return writeOf(ctx, c, e, newActionResult)
}

func doEmpty(ctx context.Context, c *Client, e endpoint) error {
	_, err := c.call(ctx, e)
	return err
}

func doBinary(ctx context.Context, c *Client, e endpoint) ([]byte, error) {
	e.accept = "*/*"
	resp, err := c.call(ctx, e)
	if err != nil {
		return nil, err
	}
	return resp.Body, nil
}

// expandPath fills the template's {name} segments from name, value pairs, escaping each value
// as one path segment.
func expandPath(template string, pairs ...string) (string, error) {
	for i := 0; i+1 < len(pairs); i += 2 {
		if pairs[i+1] == "" {
			return "", fmt.Errorf("%w: %s", ErrMissingParameter, pairs[i])
		}
		template = strings.Replace(template, "{"+pairs[i]+"}", url.PathEscape(pairs[i+1]), 1)
	}
	return template, nil
}
