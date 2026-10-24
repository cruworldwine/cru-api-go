package cru

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
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

func (c *Client) callEnvelope(ctx context.Context, e endpoint) (*response, map[string]json.RawMessage, error) {
	resp, err := c.call(ctx, e)
	if err != nil {
		return nil, nil, err
	}
	raw, f := splitObject(resp.body)
	if raw != nil && f == nil {
		return nil, nil, fmt.Errorf("%w: %s %s answered HTTP %d with a body that is not a JSON object",
			ErrUnexpectedResponse, e.method, e.path, resp.status)
	}
	return resp, f, nil
}

func requestID(resp *response, f map[string]json.RawMessage) string {
	var meta Meta
	decodeField(f, "meta", &meta)
	if meta.RequestID != "" {
		return meta.RequestID
	}
	return resp.header.Get("X-Request-Id")
}

func unmarshal(data json.RawMessage, v any) error {
	if len(data) == 0 {
		return nil
	}
	return json.Unmarshal(data, v)
}

func getOne[T any](ctx context.Context, c *Client, e endpoint) (*T, error) {
	resp, f, err := c.callEnvelope(ctx, e)
	if err != nil {
		return nil, err
	}
	data := f["data"]
	if e.key != "" {
		_, inner := splitObject(data)
		var ok bool
		if data, ok = inner[e.key]; !ok {
			return nil, fmt.Errorf("%w: %s %s answered without data.%s (request %s)",
				ErrUnexpectedResponse, e.method, e.path, e.key, requestID(resp, f))
		}
	}
	v := new(T)
	if err := unmarshal(data, v); err != nil {
		return nil, fmt.Errorf("%w: %s %s: %w", ErrUnexpectedResponse, e.method, e.path, err)
	}
	return v, nil
}

// itemsOf reads data[key] as a list. A missing key, null or {} is an empty list.
func itemsOf[T any](e endpoint, data json.RawMessage) ([]T, error) {
	_, inner := splitObject(data)
	raw, ok := inner[e.key]
	if !ok {
		return nil, nil
	}
	var items []T
	if err := json.Unmarshal(raw, &items); err != nil {
		if _, obj := splitObject(raw); obj != nil && len(obj) == 0 {
			return nil, nil
		}
		return nil, fmt.Errorf("%w: %s %s: data.%s is not a list: %w", ErrUnexpectedResponse, e.method, e.path, e.key, err)
	}
	return items, nil
}

func getList[T any](ctx context.Context, c *Client, e endpoint) ([]T, error) {
	_, f, err := c.callEnvelope(ctx, e)
	if err != nil {
		return nil, err
	}
	return itemsOf[T](e, f["data"])
}

func getPage[T any](ctx context.Context, c *Client, e endpoint) (*Page[T], error) {
	_, f, err := c.callEnvelope(ctx, e)
	if err != nil {
		return nil, err
	}
	items, err := itemsOf[T](e, f["data"])
	if err != nil {
		return nil, err
	}
	p := &Page[T]{Items: items, Data: f["data"]}
	decodeField(f, "meta", &p.Meta)
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
	w := &Write[T]{StatusCode: resp.status, RequestID: requestID(resp, f)}
	if resp.status == http.StatusAccepted {
		var staged Staged
		_ = unmarshal(f["data"], &staged)
		w.Staged = true
		w.Confirmation = &staged.Confirmation
		return w, nil
	}
	w.Replayed = strings.EqualFold(resp.header.Get("Idempotent-Replayed"), "true")
	w.Result = result(f["data"])
	return w, nil
}

func doWrite[T any](ctx context.Context, c *Client, e endpoint) (*Write[T], error) {
	return writeOf(ctx, c, e, func(data json.RawMessage) *T {
		v := new(T)
		_ = unmarshal(data, v)
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
	return resp.body, nil
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

type queryValue interface {
	string | int64 | float64 | bool
}

func formatQuery[T queryValue](v T) string {
	switch x := any(v).(type) {
	case int64:
		return strconv.FormatInt(x, 10)
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(x)
	case string:
		return x
	}
	return fmt.Sprint(v)
}

func setQuery[T queryValue](q url.Values, name string, v *T) {
	if v != nil {
		q.Set(name, formatQuery(*v))
	}
}

// setList sends a list comma-separated; an empty one is not sent.
func setList[T queryValue](q url.Values, name string, v []T) {
	if len(v) == 0 {
		return
	}
	parts := make([]string, len(v))
	for i, x := range v {
		parts[i] = formatQuery(x)
	}
	q.Set(name, strings.Join(parts, ","))
}

func encodeBody(body any) ([]byte, error) {
	switch b := body.(type) {
	case nil:
		return nil, nil
	case json.RawMessage:
		return b, nil
	case []byte:
		return b, nil
	}
	b, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("cru: encoding the request body: %w", err)
	}
	return b, nil
}

// withReference adds a reference to a JSON object that has none, as its first key.
func withReference(payload []byte) ([]byte, error) {
	b := bytes.TrimSpace(payload)
	if len(b) == 0 || string(b) == "null" {
		b = []byte("{}")
	}
	if b[0] != '{' {
		return payload, nil
	}
	var f map[string]json.RawMessage
	if err := json.Unmarshal(b, &f); err != nil {
		return nil, fmt.Errorf("cru: the request body is not a valid JSON object: %w", err)
	}
	current, ok := f["reference"]
	if ok && string(bytes.TrimSpace(current)) != "null" {
		return b, nil
	}
	ref, _ := json.Marshal(newReference())
	if ok {
		f["reference"] = ref
		return json.Marshal(f)
	}
	out := append([]byte(`{"reference":`), ref...)
	if len(f) > 0 {
		out = append(out, ',')
	}
	return append(out, b[1:]...), nil
}

func newReference() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return "sdk_" + hex.EncodeToString(b[:])
}
