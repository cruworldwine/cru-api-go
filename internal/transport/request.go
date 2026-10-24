package transport

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Request is one call to the API.
type Request struct {
	Method string
	// Path is relative to the base URL, such as /bids/BID-1, and may carry a query of its own.
	Path  string
	Query url.Values
	// Body, if not nil, is sent as JSON: a []byte or json.RawMessage as it is, anything else
	// marshalled. A partner's write gets a reference in it when it has none.
	Body any
	// Accept is the Accept header; empty is application/json.
	Accept string
}

// Options are what one call changes: its headers, timeout and retries, parameters the SDK does
// not know yet, and who is told of its answers. The Set methods record a mistake in Err, and Do
// then returns it without sending anything.
type Options struct {
	Header     http.Header
	Timeout    *time.Duration
	MaxRetries *int
	Query      url.Values
	Body       []BodyField
	// Observe, if not nil, is called with every answer the call gets, in order.
	Observe func(*Response)
	Err     error
}

// BodyField is a top-level field set in a call's JSON body.
type BodyField struct {
	Key   string
	Value any
}

// credentialHeaders are the client's own; a call cannot replace them.
var credentialHeaders = map[string]bool{"Authorization": true, "Client-Key": true, "Client-Secret": true}

// SetHeader sends the header key with value, in place of any value the SDK would send. It
// refuses the credential headers, and a header net/http would refuse.
func (o *Options) SetHeader(key, value string) {
	key = http.CanonicalHeaderKey(key)
	if credentialHeaders[key] {
		o.fail(fmt.Errorf("cru: WithRequestHeader cannot set %s: a call uses its client's credentials", key))
		return
	}
	if !validHeader(key, value) {
		o.fail(fmt.Errorf("cru: WithRequestHeader: %q: %q is not a valid header", key, value))
		return
	}
	if o.Header == nil {
		o.Header = http.Header{}
	}
	o.Header.Set(key, value)
}

// SetTimeout limits each attempt to d; 0 or less is no limit.
func (o *Options) SetTimeout(d time.Duration) {
	d = max(d, 0)
	o.Timeout = &d
}

// SetMaxRetries sets how many times the call may be retried; 0 or less is never.
func (o *Options) SetMaxRetries(n int) {
	n = max(n, 0)
	o.MaxRetries = &n
}

// SetQuery sends the query parameter key with value, in place of any value the SDK would send.
func (o *Options) SetQuery(key, value string) {
	if o.Query == nil {
		o.Query = url.Values{}
	}
	o.Query.Set(key, value)
}

// SetBodyField sets the top-level body field key to value; a later one wins.
func (o *Options) SetBodyField(key string, value any) {
	o.Body = append(o.Body, BodyField{key, value})
}

func (o *Options) fail(err error) {
	o.Err = errors.Join(o.Err, err)
}

// validHeader reports whether key is an HTTP token and value has no control characters but tab,
// as net/http requires.
func validHeader(key, value string) bool {
	if key == "" {
		return false
	}
	for _, r := range key {
		if r <= ' ' || r >= 0x7f || strings.ContainsRune(`"(),/:;<=>?@[\]{}`, r) {
			return false
		}
	}
	for _, r := range value {
		if (r < ' ' && r != '\t') || r == 0x7f {
			return false
		}
	}
	return true
}

// prepared is a request encoded once, so a partner write's reference is the same on every
// attempt.
type prepared struct {
	method     string
	url        string
	path       string // the URL's path, for logs
	payload    []byte
	accept     string
	header     http.Header
	retryable  bool
	maxRetries int
	timeout    time.Duration
}

func isWrite(method string) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return false
	}
	return true
}

func (c *Client) prepare(r Request, o *Options) (*prepared, error) {
	write := isWrite(r.Method)
	if len(o.Body) > 0 && !write {
		return nil, fmt.Errorf("cru: WithExtraBodyField: %s %s sends no body", r.Method, r.Path)
	}
	var payload []byte
	if r.Body != nil || len(o.Body) > 0 || (write && c.partner) {
		var err error
		if payload, err = encodeBody(r.Body); err != nil {
			return nil, err
		}
		if len(o.Body) > 0 {
			if payload, err = withFields(payload, o.Body); err != nil {
				return nil, err
			}
		}
		if write && c.partner {
			if payload, err = withReference(payload); err != nil {
				return nil, err
			}
		}
	}
	query := r.Query
	if len(o.Query) > 0 {
		query = url.Values{}
		for k, v := range r.Query {
			query[k] = v
		}
		for k, v := range o.Query {
			query[k] = v
		}
	}
	u := c.baseURL + r.Path
	if q := query.Encode(); q != "" {
		sep := "?"
		if strings.Contains(r.Path, "?") {
			sep = "&"
		}
		u += sep + q
	}
	parsed, err := url.Parse(u)
	if err != nil {
		return nil, fmt.Errorf("cru: %w", err)
	}
	p := &prepared{
		method: r.Method, url: u, path: parsed.Path, payload: payload, accept: r.Accept, header: o.Header,
		retryable: !write || c.partner, maxRetries: c.maxRetries, timeout: c.timeout,
	}
	if p.accept == "" {
		p.accept = "application/json"
	}
	if o.MaxRetries != nil {
		p.maxRetries = *o.MaxRetries
	}
	if o.Timeout != nil {
		p.timeout = *o.Timeout
	}
	return p, nil
}

func (c *Client) headers(p *prepared, token string) http.Header {
	h := http.Header{}
	h.Set("Accept", p.accept)
	h.Set("User-Agent", c.userAgent)
	if p.payload != nil {
		h.Set("Content-Type", "application/json")
	}
	for k, v := range p.header {
		h[k] = v
	}
	if c.partner {
		// Sent as the spec spells them, not canonicalised.
		h["CLIENT-KEY"] = []string{c.clientKey}
		h["CLIENT-SECRET"] = []string{c.clientSecret}
	} else {
		h.Set("Authorization", "Bearer "+token)
	}
	return h
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

// withFields sets fields on a JSON object payload; nil is taken as {}.
func withFields(payload []byte, fields []BodyField) ([]byte, error) {
	b := bytes.TrimSpace(payload)
	f := map[string]json.RawMessage{}
	if len(b) > 0 && string(b) != "null" {
		if b[0] != '{' || json.Unmarshal(b, &f) != nil {
			return nil, errors.New("cru: WithExtraBodyField needs a JSON object body")
		}
	}
	for _, field := range fields {
		v, err := json.Marshal(field.Value)
		if err != nil {
			return nil, fmt.Errorf("cru: encoding the body field %s: %w", field.Key, err)
		}
		f[field.Key] = v
	}
	return json.Marshal(f)
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
