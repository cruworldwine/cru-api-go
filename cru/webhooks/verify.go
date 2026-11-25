package webhooks //nolint:revive // the package comment is in the generated doc.go, and revive skips generated files

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Tolerance is how far a delivery's webhook-timestamp may be from now, either way, for Verify.
const Tolerance = 5 * time.Minute

const secretPrefix = "whsec_"

// ErrInvalidWebhook is returned, wrapped with the reason, when a request is not a delivery Cru
// signed with the secret given, or is too old. Answer such a request 401 and drop it.
var ErrInvalidWebhook = errors.New("cru: invalid webhook")

// Verify checks a webhook delivery's signature and returns its event.
//
// Cru signs every delivery as Standard Webhooks v1 does: webhook-signature holds v1, and the base64
// HMAC-SHA256 of "{webhook-id}.{webhook-timestamp}.{body}", keyed with the base64-decoded part of
// the signing secret after whsec_. For 24 hours after the secret is rotated there are two
// signatures, separated by a space, and either is accepted. webhook-timestamp must be within
// Tolerance of now.
//
// body is the request body exactly as it arrived, before any JSON decoding; header is the request's
// headers; secret is the webhook's signing secret, whsec_ and a base64 key. Deduplicate on the
// event's ID, and answer 2xx at once: do the work afterwards. Event.SaleCreated, PurchaseCreated and
// TestData decode its data by type.
func Verify(body []byte, header http.Header, secret string) (*Event, error) {
	return VerifyAt(body, header, secret, time.Now(), Tolerance)
}

// VerifyAt is Verify, checking webhook-timestamp against now with the tolerance given.
func VerifyAt(body []byte, header http.Header, secret string, now time.Time, tolerance time.Duration) (*Event, error) {
	id := headerValue(header, "webhook-id")
	timestamp := headerValue(header, "webhook-timestamp")
	signature := headerValue(header, "webhook-signature")
	if id == "" || signature == "" {
		return nil, fmt.Errorf("%w: the webhook-id and webhook-signature headers are required", ErrInvalidWebhook)
	}
	if timestamp == "" || strings.Trim(timestamp, "0123456789") != "" {
		return nil, fmt.Errorf("%w: the webhook-timestamp header is not a Unix time", ErrInvalidWebhook)
	}
	seconds, err := strconv.ParseInt(timestamp, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("%w: the webhook-timestamp header is not a Unix time", ErrInvalidWebhook)
	}
	if age := now.Sub(time.Unix(seconds, 0)); age > tolerance || age < -tolerance {
		return nil, fmt.Errorf("%w: the webhook-timestamp is too far from now: an old or replayed delivery", ErrInvalidWebhook)
	}

	key, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(secret, secretPrefix))
	if err != nil {
		return nil, fmt.Errorf("%w: the secret is not whsec_ and a base64 key", ErrInvalidWebhook)
	}
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(id + "." + timestamp + "."))
	mac.Write(body)
	expected := []byte(base64.StdEncoding.EncodeToString(mac.Sum(nil)))
	matched := false
	for _, candidate := range strings.Split(signature, " ") {
		version, value, ok := strings.Cut(candidate, ",")
		if ok && version == "v1" && hmac.Equal(expected, []byte(value)) {
			matched = true
			break
		}
	}
	if !matched {
		return nil, fmt.Errorf("%w: no signature matches: not signed by Cru with this secret", ErrInvalidWebhook)
	}

	var head map[string]json.RawMessage
	if err := json.Unmarshal(body, &head); err != nil || head == nil {
		return nil, fmt.Errorf("%w: the body is not a JSON object", ErrInvalidWebhook)
	}
	event := new(Event)
	if err := json.Unmarshal(body, event); err != nil {
		return nil, fmt.Errorf("%w: the body is not an event: %w", ErrInvalidWebhook, err)
	}
	return event, nil
}

// SaleCreated decodes a sale.created event's data. It is an error for an event of another type.
func (m *Event) SaleCreated() (*SaleCreatedData, error) {
	out := new(SaleCreatedData)
	return out, m.decodeData(EventTypeSaleCreated, out)
}

// PurchaseCreated decodes a purchase.created event's data. It is an error for an event of another type.
func (m *Event) PurchaseCreated() (*PurchaseCreatedData, error) {
	out := new(PurchaseCreatedData)
	return out, m.decodeData(EventTypePurchaseCreated, out)
}

// TestData decodes a webhook.test event's data. It is an error for an event of another type.
func (m *Event) TestData() (*TestData, error) {
	out := new(TestData)
	return out, m.decodeData(EventTypeWebhookTest, out)
}

func (m *Event) decodeData(want string, into any) error {
	if m == nil || m.Type != want {
		got := ""
		if m != nil {
			got = m.Type
		}
		return fmt.Errorf("cru: the event is %q, not %q", got, want)
	}
	var body struct {
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(m.Raw(), &body); err != nil || len(body.Data) == 0 {
		return fmt.Errorf("cru: the %s event has no data", want)
	}
	return json.Unmarshal(body.Data, into)
}

// headerValue reads a header in any case, including from a map built by hand with lower-case keys.
func headerValue(h http.Header, name string) string {
	if v := h.Get(name); v != "" {
		return v
	}
	for k, vs := range h {
		if strings.EqualFold(k, name) && len(vs) > 0 {
			return vs[0]
		}
	}
	return ""
}
