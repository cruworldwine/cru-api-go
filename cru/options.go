package cru

import (
	"time"

	"github.com/cruworldwine/cru-api-go/cru/core"
)

// RequestOption changes one call. Every method takes any number of them after its other
// arguments:
//
//	bid, err := client.Bids.Get(ctx, code, cru.WithRequestTimeout(5*time.Second))
//
// A nil RequestOption is ignored. It is core.RequestOption.
type RequestOption = core.RequestOption

// ResponseMeta describes the answer to one call: its status, headers, request id and number of
// attempts. Fill one with WithResponseInto. It is core.ResponseMeta.
type ResponseMeta = core.ResponseMeta

// WithRequestHeader sends the header key with value on this call, replacing any value the SDK
// would send for it, such as a tracing header. The credential headers (Authorization,
// CLIENT-KEY and CLIENT-SECRET) cannot be set this way, and the call fails if one is: create a
// Client for each credential instead. So does a header net/http would refuse.
func WithRequestHeader(key, value string) RequestOption { return core.WithRequestHeader(key, value) }

// WithRequestTimeout limits each attempt of this call to d, in place of the client's
// WithTimeout; 0 leaves attempts unlimited. To bound the whole call, retries included, give it
// a context with a deadline.
func WithRequestTimeout(d time.Duration) RequestOption { return core.WithRequestTimeout(d) }

// WithRequestMaxRetries sets how many times this call is retried, in place of the client's
// WithMaxRetries; 0 turns retries off. A connected app's writes are never retried, whatever it
// says.
func WithRequestMaxRetries(n int) RequestOption { return core.WithRequestMaxRetries(n) }

// WithExtraQuery sends the query parameter key with value on this call, replacing any value the
// SDK would send for it. Use it for a parameter this version of the SDK does not know yet.
func WithExtraQuery(key, value string) RequestOption { return core.WithExtraQuery(key, value) }

// WithExtraBodyField sets the top-level field key of this call's JSON body to value, encoded with
// encoding/json, replacing any value the SDK would send for it. Use it for a field this version
// of the SDK does not know yet. It applies to writes only: a read sends no body, and the call
// fails if it is given one.
func WithExtraBodyField(key string, value any) RequestOption {
	return core.WithExtraBodyField(key, value)
}

// WithResponseInto fills dst with what the API answered this call: its status, headers and
// request id, which Cru support needs to find it. It is filled from the call's last answer, a
// success or an error answer, and left untouched when no answer came. For a list walked with
// Page.All or Page.Next, it describes the last page fetched. Give each call its own ResponseMeta.
func WithResponseInto(dst *ResponseMeta) RequestOption { return core.WithResponseInto(dst) }
