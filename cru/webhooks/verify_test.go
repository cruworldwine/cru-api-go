package webhooks_test

// Signatures made by the API's own signer (Cru_Api_Model_Webhook_Signer). The secrets are invented
// samples, not keys of any webhook.

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/cruworldwine/cru-api-go/cru/webhooks"
)

const (
	body = `{"id":"evt_6f1c0d2e9a7b4c3d5e8f9a0b","type":"webhook.test","api_version":"1",` +
		`"created_at":"2026-10-19T10:11:01Z","data":{"webhook_id":"whs_3f9a1c7e5b2d4086a1e9c4b7",` +
		`"message":"A test event from Cru. Answer with any 2xx status to show your endpoint works."}}`
	oldSecret = "whsec_c2FtcGxlLXNlY3JldC1ub3QtYS1yZWFsLWtleQ==" //nolint:gosec // an invented sample secret
	newSecret = "whsec_c2Vjb25kLXNhbXBsZS1zZWNyZXQtbm90LXJlYWw=" //nolint:gosec // an invented sample secret
	oldSig    = "v1,fpcbXyd+iRNLUrvep/lKQ+/nbKRqmRkE/jbH+gndpZc="
	newSig    = "v1,I8G3oULkwTjh5XwYduia9NQKJWTNYCaQjyZJMJ+fotc="
	signedAt  = 1792404661
)

var at = time.Unix(signedAt, 0)

func headers(signature string) http.Header {
	h := http.Header{}
	h.Set("webhook-id", "evt_6f1c0d2e9a7b4c3d5e8f9a0b")
	h.Set("webhook-timestamp", "1792404661")
	h.Set("webhook-signature", signature)
	return h
}

func verify(t *testing.T, b string, h http.Header, secret string, now time.Time) (*webhooks.Event, error) {
	t.Helper()
	return webhooks.VerifyAt([]byte(b), h, secret, now, webhooks.Tolerance)
}

func TestVerifyAGenuineDelivery(t *testing.T) {
	event, err := verify(t, body, headers(oldSig), oldSecret, at)
	if err != nil {
		t.Fatal(err)
	}
	if event.ID != "evt_6f1c0d2e9a7b4c3d5e8f9a0b" || event.Type != webhooks.EventTypeWebhookTest || event.APIVersion != "1" {
		t.Fatalf("event = %+v", event)
	}
	data, err := event.TestData()
	if err != nil {
		t.Fatal(err)
	}
	if data.WebhookID != "whs_3f9a1c7e5b2d4086a1e9c4b7" {
		t.Fatalf("data = %+v", data)
	}
	if _, err := event.SaleCreated(); err == nil {
		t.Fatal("SaleCreated decoded a webhook.test")
	}
}

func TestVerifyHeadersInAnyCase(t *testing.T) {
	h := http.Header{
		"webhook-id":        {"evt_6f1c0d2e9a7b4c3d5e8f9a0b"},
		"WEBHOOK-TIMESTAMP": {"1792404661"},
		"Webhook-Signature": {oldSig},
	}
	if _, err := verify(t, body, h, oldSecret, at); err != nil {
		t.Fatal(err)
	}
}

func TestVerifyDuringARotation(t *testing.T) {
	h := headers(newSig + " " + oldSig)
	for _, secret := range []string{newSecret, oldSecret} {
		if _, err := verify(t, body, h, secret, at); err != nil {
			t.Fatalf("%s: %v", secret, err)
		}
	}
	if _, err := verify(t, body, headers(oldSig), newSecret, at); !errors.Is(err, webhooks.ErrInvalidWebhook) {
		t.Fatalf("the new secret alone verified the old signature: %v", err)
	}
}

func TestVerifyRefuses(t *testing.T) {
	cases := map[string]struct {
		body   string
		header http.Header
		secret string
		now    time.Time
		want   string
	}{
		"a changed body":           {strings.Replace(body, "webhook.test", "sale.created", 1), headers(oldSig), oldSecret, at, "no signature matches"},
		"too old":                  {body, headers(oldSig), oldSecret, at.Add(301 * time.Second), "too far from now"},
		"too far ahead":            {body, headers(oldSig), oldSecret, at.Add(-301 * time.Second), "too far from now"},
		"no signature":             {body, headers(""), oldSecret, at, "required"},
		"another version":          {body, headers(strings.Replace(oldSig, "v1,", "v2,", 1)), oldSecret, at, "no signature matches"},
		"a signature of no shape":  {body, headers("fpcbXyd"), oldSecret, at, "no signature matches"},
		"a secret that is not b64": {body, headers(oldSig), "whsec_not base64!", at, "not whsec_ and a base64 key"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := verify(t, c.body, c.header, c.secret, c.now)
			if !errors.Is(err, webhooks.ErrInvalidWebhook) || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("err = %v, want ErrInvalidWebhook and %q", err, c.want)
			}
		})
	}
	for _, ts := range []string{"", "soon", "-1792404661", "1792404661.5", "99999999999999999999"} {
		h := headers(oldSig)
		h.Set("webhook-timestamp", ts)
		if _, err := verify(t, body, h, oldSecret, at); !errors.Is(err, webhooks.ErrInvalidWebhook) {
			t.Fatalf("timestamp %q: err = %v", ts, err)
		}
	}
	h := headers(oldSig)
	h.Del("webhook-id")
	if _, err := verify(t, body, h, oldSecret, at); !errors.Is(err, webhooks.ErrInvalidWebhook) {
		t.Fatalf("no webhook-id: err = %v", err)
	}
}

func TestVerifyAcceptsTheEdgesOfTheTolerance(t *testing.T) {
	for _, d := range []time.Duration{300 * time.Second, -300 * time.Second} {
		if _, err := verify(t, body, headers(oldSig), oldSecret, at.Add(d)); err != nil {
			t.Fatalf("%v: %v", d, err)
		}
	}
	if _, err := webhooks.VerifyAt([]byte(body), headers(oldSig), oldSecret, at.Add(time.Hour), 2*time.Hour); err != nil {
		t.Fatal(err)
	}
}

func TestVerifyUsesTheCurrentTime(t *testing.T) {
	if _, err := webhooks.Verify([]byte(body), headers(oldSig), oldSecret); !errors.Is(err, webhooks.ErrInvalidWebhook) {
		t.Fatalf("a 2026 signature verified now: %v", err)
	}
}

func TestVerifyRefusesASignedBodyThatIsNotAnObject(t *testing.T) {
	key, _ := base64.StdEncoding.DecodeString(strings.TrimPrefix(oldSecret, "whsec_"))
	for _, b := range []string{"not json", "[1]", "null"} {
		mac := hmac.New(sha256.New, key)
		mac.Write([]byte("evt_6f1c0d2e9a7b4c3d5e8f9a0b.1792404661." + b))
		sig := "v1," + base64.StdEncoding.EncodeToString(mac.Sum(nil))
		if _, err := verify(t, b, headers(sig), oldSecret, at); !errors.Is(err, webhooks.ErrInvalidWebhook) {
			t.Fatalf("%q: err = %v", b, err)
		}
	}
}

func TestEventDataByType(t *testing.T) {
	sale := `{"id":"evt_1","type":"sale.created","api_version":"1","created_at":"2026-10-19T10:11:01Z",` +
		`"data":{"sale_ref":"stpo-5531","sales":[{"sale_ref":"stpo-5531","trade_id":"49780","quantity":1}]}}`
	purchase := `{"id":"evt_2","type":"purchase.created","api_version":"1","created_at":"2026-10-19T10:11:01Z",` +
		`"data":{"purchase_ref":"ord-90211","purchases":[{"purchase_ref":"ord-90211","quantity":6}]}}`
	var s, p webhooks.Event
	if err := s.UnmarshalJSON([]byte(sale)); err != nil {
		t.Fatal(err)
	}
	if err := p.UnmarshalJSON([]byte(purchase)); err != nil {
		t.Fatal(err)
	}
	sd, err := s.SaleCreated()
	if err != nil || sd.SaleRef != "stpo-5531" || len(sd.Sales) != 1 || sd.Sales[0].TradeID == nil || *sd.Sales[0].TradeID != "49780" {
		t.Fatalf("sale data = %+v, %v", sd, err)
	}
	pd, err := p.PurchaseCreated()
	if err != nil || pd.PurchaseRef != "ord-90211" || len(pd.Purchases) != 1 || pd.Purchases[0].Quantity != 6 {
		t.Fatalf("purchase data = %+v, %v", pd, err)
	}
	if _, err := p.TestData(); err == nil {
		t.Fatal("TestData decoded a purchase.created")
	}
	var nilEvent *webhooks.Event
	if _, err := nilEvent.SaleCreated(); err == nil {
		t.Fatal("a nil event decoded")
	}
	var empty webhooks.Event
	if err := empty.UnmarshalJSON([]byte(`{"type":"sale.created"}`)); err != nil {
		t.Fatal(err)
	}
	if _, err := empty.SaleCreated(); err == nil {
		t.Fatal("an event with no data decoded")
	}
}

func ExampleVerify() {
	http.HandleFunc("/cru/webhooks", func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body) // the raw bytes, before any JSON decoding
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		event, err := webhooks.Verify(body, r.Header, "whsec_...") // the webhook's signing_secret
		if err != nil {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		// Store event.ID and answer at once; do the work afterwards.
		w.WriteHeader(http.StatusOK)
		if event.Type == webhooks.EventTypeSaleCreated {
			if sale, err := event.SaleCreated(); err == nil {
				_ = sale.SaleRef
			}
		}
	})
}
