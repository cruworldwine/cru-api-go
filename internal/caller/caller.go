// Package caller is how the generated methods call the API. Each method describes its call as an
// Endpoint and hands it to GetOne, GetList, GetPage, DoWrite, WriteOf, DoEmpty or DoBinary, which
// send it through the transport and turn the answer into package core's types: a model, a Page, a
// Write, or an *APIError or *ConnectionError. Package cru makes one Caller for each Client and hands
// it to every service, with NewService.
package caller

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/cruworldwine/cru-api-go/cru/core"
	"github.com/cruworldwine/cru-api-go/internal/apijson"
	"github.com/cruworldwine/cru-api-go/internal/corelink"
	"github.com/cruworldwine/cru-api-go/internal/transport"
)

// Caller sends a Client's calls through its transport. It is safe for concurrent use.
type Caller struct {
	tr *transport.Client
}

// New returns a Caller that sends through tr.
func New(tr *transport.Client) *Caller { return &Caller{tr: tr} }

// Endpoint is one call, as a generated method describes it.
type Endpoint struct {
	Method string
	Path   string
	Query  url.Values
	Body   any
	// Key is the key of data the result is under; empty for data itself.
	Key    string
	Accept string
	Opts   []core.RequestOption
}

// EmptyObject is the body of a write that has no parameters.
var EmptyObject = json.RawMessage("{}")

// Call sends e and turns a failure into core's error types.
func Call(ctx context.Context, c *Caller, e Endpoint) (*transport.Response, error) {
	resp, err := c.tr.Do(ctx, transport.Request{Method: e.Method, Path: e.Path, Query: e.Query, Body: e.Body, Accept: e.Accept},
		options(e.Opts))
	if err != nil {
		return nil, corelink.PublicError(err)
	}
	return resp, nil
}

// options applies a call's RequestOptions, skipping nil ones.
func options(opts []core.RequestOption) *transport.Options {
	o := &transport.Options{}
	for _, opt := range opts {
		if opt != nil {
			opt(o)
		}
	}
	return o
}

func callEnvelope(ctx context.Context, c *Caller, e Endpoint) (*transport.Response, map[string]json.RawMessage, error) {
	resp, err := Call(ctx, c, e)
	if err != nil {
		return nil, nil, err
	}
	raw, f := apijson.Split(resp.Body)
	if raw != nil && f == nil {
		return nil, nil, fmt.Errorf("%w: %s %s answered HTTP %d with a body that is not a JSON object",
			core.ErrUnexpectedResponse, e.Method, e.Path, resp.Status)
	}
	return resp, f, nil
}

// GetOne calls e and decodes data[e.Key], or data itself, into a T.
func GetOne[T any](ctx context.Context, c *Caller, e Endpoint) (*T, error) {
	resp, f, err := callEnvelope(ctx, c, e)
	if err != nil {
		return nil, err
	}
	data, ok := apijson.Data(f, e.Key)
	if !ok {
		return nil, fmt.Errorf("%w: %s %s answered without data.%s (request %s)",
			core.ErrUnexpectedResponse, e.Method, e.Path, e.Key, apijson.RequestID(f, resp.Header))
	}
	v := new(T)
	if err := apijson.Unmarshal(data, v); err != nil {
		return nil, fmt.Errorf("%w: %s %s: %w", core.ErrUnexpectedResponse, e.Method, e.Path, err)
	}
	return v, nil
}

// itemsOf reads data[key] as a list. A missing key, null or {} is an empty list.
func itemsOf[T any](e Endpoint, f map[string]json.RawMessage) ([]T, error) {
	raw, _ := apijson.Data(f, e.Key)
	items, err := apijson.List[T](raw)
	if err != nil {
		return nil, fmt.Errorf("%w: %s %s: data.%s is not a list: %w", core.ErrUnexpectedResponse, e.Method, e.Path, e.Key, err)
	}
	return items, nil
}

// GetList calls e and decodes data[e.Key] into a list of T.
func GetList[T any](ctx context.Context, c *Caller, e Endpoint) ([]T, error) {
	_, f, err := callEnvelope(ctx, c, e)
	if err != nil {
		return nil, err
	}
	return itemsOf[T](e, f)
}

// GetPage calls e and returns the page of T in data[e.Key], which fetches the pages after it with
// e's query and options and the next offset.
func GetPage[T any](ctx context.Context, c *Caller, e Endpoint) (*core.Page[T], error) {
	_, f, err := callEnvelope(ctx, c, e)
	if err != nil {
		return nil, err
	}
	items, err := itemsOf[T](e, f)
	if err != nil {
		return nil, err
	}
	p := &core.Page[T]{Items: items, Data: f["data"]}
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
	corelink.SetNext(p, func(ctx context.Context, offset int) (any, error) {
		next := e
		next.Query = url.Values{}
		for k, v := range e.Query {
			next.Query[k] = v
		}
		next.Query.Set("offset", strconv.Itoa(offset))
		return GetPage[T](ctx, c, next)
	})
	return p, nil
}

// WriteOf calls the write e. A staged answer (202) fills the Write's Confirmation; any other is
// decoded into its Result with result.
func WriteOf[T any](ctx context.Context, c *Caller, e Endpoint, result func(json.RawMessage) *T) (*core.Write[T], error) {
	resp, f, err := callEnvelope(ctx, c, e)
	if err != nil {
		return nil, err
	}
	w := &core.Write[T]{StatusCode: resp.Status, RequestID: apijson.RequestID(f, resp.Header)}
	if resp.Status == http.StatusAccepted {
		var staged core.Staged
		_ = apijson.Unmarshal(f["data"], &staged)
		w.Staged = true
		w.Confirmation = &staged.Confirmation
		return w, nil
	}
	w.Replayed = strings.EqualFold(resp.Header.Get("Idempotent-Replayed"), "true")
	w.Result = result(f["data"])
	return w, nil
}

// DoWrite calls the write e, whose result decodes into a T.
func DoWrite[T any](ctx context.Context, c *Caller, e Endpoint) (*core.Write[T], error) {
	return WriteOf(ctx, c, e, func(data json.RawMessage) *T {
		v := new(T)
		_ = apijson.Unmarshal(data, v)
		return v
	})
}

// DoEmpty calls e, which answers with no body.
func DoEmpty(ctx context.Context, c *Caller, e Endpoint) error {
	_, err := Call(ctx, c, e)
	return err
}

// DoBinary calls e, which answers with a file rather than the envelope, and returns its bytes.
func DoBinary(ctx context.Context, c *Caller, e Endpoint) ([]byte, error) {
	e.Accept = "*/*"
	resp, err := Call(ctx, c, e)
	if err != nil {
		return nil, err
	}
	return resp.Body, nil
}

// ExpandPath fills the template's {name} segments from name, value pairs, escaping each value
// as one path segment. An empty value is core.ErrMissingParameter, wrapped.
func ExpandPath(template string, pairs ...string) (string, error) {
	for i := 0; i+1 < len(pairs); i += 2 {
		if pairs[i+1] == "" {
			return "", fmt.Errorf("%w: %s", core.ErrMissingParameter, pairs[i])
		}
		template = strings.Replace(template, "{"+pairs[i]+"}", url.PathEscape(pairs[i+1]), 1)
	}
	return template, nil
}
