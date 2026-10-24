package cru

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// RequestOption changes one call. Every method takes any number of them after its other
// arguments:
//
//	bid, err := client.Bids.Get(ctx, code, cru.WithRequestTimeout(5*time.Second))
//
// A nil RequestOption is ignored.
type RequestOption func(*requestOptions)

type requestOptions struct {
	header     http.Header
	timeout    *time.Duration
	maxRetries *int
	query      url.Values
	body       []bodyField
	meta       *ResponseMeta
	attempts   int
	err        error
}

type bodyField struct {
	key   string
	value any
}

// credentialHeaders are the client's own; a call cannot replace them.
var credentialHeaders = map[string]bool{"Authorization": true, "Client-Key": true, "Client-Secret": true}

// WithRequestHeader sends the header key with value on this call, replacing any value the SDK
// would send for it, such as a tracing header. The credential headers (Authorization,
// CLIENT-KEY and CLIENT-SECRET) cannot be set this way, and the call fails if one is: create a
// Client for each credential instead.
func WithRequestHeader(key, value string) RequestOption {
	return func(o *requestOptions) {
		key = http.CanonicalHeaderKey(key)
		if credentialHeaders[key] {
			o.fail(fmt.Errorf("cru: WithRequestHeader cannot set %s: a call uses its client's credentials", key))
			return
		}
		if !validHeader(key, value) {
			o.fail(fmt.Errorf("cru: WithRequestHeader: %q: %q is not a valid header", key, value))
			return
		}
		if o.header == nil {
			o.header = http.Header{}
		}
		o.header.Set(key, value)
	}
}

// WithRequestTimeout limits each attempt of this call to d, in place of the client's
// WithTimeout; 0 leaves attempts unlimited. To bound the whole call, retries included, give it
// a context with a deadline.
func WithRequestTimeout(d time.Duration) RequestOption {
	d = max(d, 0)
	return func(o *requestOptions) { o.timeout = &d }
}

// WithRequestMaxRetries sets how many times this call is retried, in place of the client's
// WithMaxRetries; 0 turns retries off. A connected app's writes are never retried, whatever it
// says.
func WithRequestMaxRetries(n int) RequestOption {
	n = max(n, 0)
	return func(o *requestOptions) { o.maxRetries = &n }
}

// WithExtraQuery sends the query parameter key with value on this call, replacing any value the
// SDK would send for it. Use it for a parameter this version of the SDK does not know yet.
func WithExtraQuery(key, value string) RequestOption {
	return func(o *requestOptions) {
		if o.query == nil {
			o.query = url.Values{}
		}
		o.query.Set(key, value)
	}
}

// WithExtraBodyField sets the top-level field key of this call's JSON body to value, encoded with
// encoding/json, replacing any value the SDK would send for it. Use it for a field this version
// of the SDK does not know yet. It applies to writes only: a read sends no body, and the call
// fails if it is given one.
func WithExtraBodyField(key string, value any) RequestOption {
	return func(o *requestOptions) {
		o.body = append(o.body, bodyField{key, value})
	}
}

// WithResponseInto fills dst with what the API answered this call: its status, headers and
// request id, which Cru support needs to find it. It is filled from the call's last answer, a
// success or an error answer, and left untouched when no answer came. For a list walked with
// Page.All or Page.Next, it describes the last page fetched. Give each call its own ResponseMeta.
func WithResponseInto(dst *ResponseMeta) RequestOption {
	return func(o *requestOptions) { o.meta = dst }
}

// ResponseMeta describes the answer to one call. Fill one with WithResponseInto.
type ResponseMeta struct {
	// StatusCode is the HTTP status.
	StatusCode int
	// Header is the response's headers.
	Header http.Header
	// RequestID is meta.request_id, else the X-Request-Id header. Quote it to Cru support.
	RequestID string
	// Attempts is how many requests the call sent, retries included.
	Attempts int
}

func newRequestOptions(opts []RequestOption) *requestOptions {
	o := &requestOptions{}
	for _, opt := range opts {
		if opt != nil {
			opt(o)
		}
	}
	return o
}

func (o *requestOptions) fail(err error) {
	o.err = errors.Join(o.err, err)
}

// validHeader reports whether key is an HTTP token and value has no control characters but tab,
// as net/http requires; a header it would refuse fails the call before anything is sent.
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

// record fills the caller's ResponseMeta from the call's last answer.
func (o *requestOptions) record(resp *response, err error) {
	if o.meta == nil {
		return
	}
	if resp != nil {
		_, f := splitObject(resp.body)
		*o.meta = ResponseMeta{StatusCode: resp.status, Header: resp.header, RequestID: requestID(resp, f), Attempts: o.attempts}
		return
	}
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		*o.meta = ResponseMeta{StatusCode: apiErr.StatusCode, Header: apiErr.Header, RequestID: apiErr.RequestID, Attempts: o.attempts}
	}
}

// withFields sets the extra fields on a JSON object payload; nil is taken as {}.
func withFields(payload []byte, fields []bodyField) ([]byte, error) {
	raw, f := splitObject(payload)
	switch {
	case raw == nil:
		f = map[string]json.RawMessage{}
	case f == nil:
		return nil, errors.New("cru: WithExtraBodyField needs a JSON object body")
	}
	for _, field := range fields {
		v, err := json.Marshal(field.value)
		if err != nil {
			return nil, fmt.Errorf("cru: encoding the body field %s: %w", field.key, err)
		}
		f[field.key] = v
	}
	return json.Marshal(f)
}
