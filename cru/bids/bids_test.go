package bids

import (
	"context"
	"errors"
	"net/http"
	"regexp"
	"strings"
	"testing"

	"github.com/cruworldwine/cru-api-go/cru/core"
	"github.com/cruworldwine/cru-api-go/internal/testkit"
)

// Every fixture in these tests is invented: fake LWINs, names, codes and ids.

var referencePattern = regexp.MustCompile(`^sdk_[0-9a-f]{32}$`)

func float(v float64) *float64 { return &v }
func boolean(v bool) *bool     { return &v }
func text(v string) *string    { return &v }

func TestPathEncoding(t *testing.T) {
	s := testkit.NewServer(t, testkit.Always(200, testkit.OK(`{"bid":`+testkit.BidJSON("x")+`}`)))
	b := newService(testkit.Partner(t, s))
	ctx := context.Background()
	if _, err := b.Get(ctx, "a/b c?d#e"); err != nil {
		t.Fatal(err)
	}
	if got := s.Requests()[0].Path; got != "/bids/a%2Fb%20c%3Fd%23e" {
		t.Errorf("path %s", got)
	}
	_, err := b.Get(ctx, "")
	if !errors.Is(err, core.ErrMissingParameter) || !strings.Contains(err.Error(), "code") {
		t.Errorf("empty code: %v", err)
	}
	for name, call := range map[string]func() error{
		"Update": func() error { _, err := b.Update(ctx, "", nil); return err },
		"Cancel": func() error { _, err := b.Cancel(ctx, "", nil); return err },
		"Renew":  func() error { _, err := b.Renew(ctx, "", nil); return err },
	} {
		if err := call(); !errors.Is(err, core.ErrMissingParameter) {
			t.Errorf("%s with an empty code: %v", name, err)
		}
	}
	if len(s.Requests()) != 1 {
		t.Error("a request was sent with an empty path parameter")
	}
}

func TestBodyOmitsNil(t *testing.T) {
	s := testkit.NewServer(t, testkit.Always(200, testkit.OK(testkit.BidResultJSON("bid.update"))))
	b := newService(testkit.App(t, s))
	if _, err := b.Update(context.Background(), testkit.BidCode, &UpdateParams{Price: float(1250.75), LocalOnly: boolean(false)}); err != nil {
		t.Fatal(err)
	}
	got := s.Requests()[0]
	if got.Body != `{"price":1250.75,"local_only":false}` {
		t.Errorf("body %s", got.Body)
	}
	if got.Header.Get("Content-Type") != "application/json" || got.Method != "PATCH" {
		t.Errorf("%s with Content-Type %q", got.Method, got.Header.Get("Content-Type"))
	}
}

func TestRequiredBodyFieldsAreSent(t *testing.T) {
	s := testkit.NewServer(t, testkit.Always(201, testkit.OK(testkit.BidResultJSON("bid.place"))))
	b := newService(testkit.App(t, s))
	_, err := b.Place(context.Background(), &PlaceParams{LWIN18: testkit.LWIN, Quantity: 2, Price: 1200})
	if err != nil {
		t.Fatal(err)
	}
	if got := s.Requests()[0].Body; got != `{"lwin18":"`+testkit.LWIN+`","quantity":2,"price":1200}` {
		t.Errorf("body %s", got)
	}
}

func TestEmptyWriteBody(t *testing.T) {
	s := testkit.NewServer(t, testkit.Always(200, testkit.OK(testkit.BidResultJSON("bid.renew"))))
	b := newService(testkit.App(t, s))
	ctx := context.Background()
	if _, err := b.Renew(ctx, testkit.BidCode, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Cancel(ctx, testkit.BidCode, &CancelParams{}); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Place(ctx, nil); err != nil {
		t.Fatal(err)
	}
	for _, r := range s.Requests() {
		if r.Body != "{}" {
			t.Errorf("%s %s sent %q, want {}", r.Method, r.Path, r.Body)
		}
	}
}

func TestPartnerWriteGetsReference(t *testing.T) {
	s := testkit.NewServer(t, testkit.Always(201, testkit.OK(testkit.BidResultJSON("bid.place"))))
	b := newService(testkit.Partner(t, s))
	ctx := context.Background()
	params := &PlaceParams{LWIN18: testkit.LWIN, Quantity: 1, Price: 99.5}
	if _, err := b.Place(ctx, params); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Place(ctx, params); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Place(ctx, &PlaceParams{Reference: text("my-ref-1"), LWIN18: testkit.LWIN, Quantity: 1, Price: 99.5}); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Cancel(ctx, testkit.BidCode, nil); err != nil {
		t.Fatal(err)
	}
	reqs := s.Requests()
	first, second := testkit.MustJSON(t, reqs[0].Body), testkit.MustJSON(t, reqs[1].Body)
	if !referencePattern.MatchString(first["reference"].(string)) {
		t.Errorf("generated reference %v", first["reference"])
	}
	if first["reference"] == second["reference"] {
		t.Error("two calls shared a reference")
	}
	if params.Reference != nil {
		t.Error("the caller's params were changed")
	}
	if !strings.HasPrefix(reqs[0].Body, `{"reference":"sdk_`) || first["lwin18"] != testkit.LWIN {
		t.Errorf("body %s", reqs[0].Body)
	}
	if got := testkit.MustJSON(t, reqs[2].Body)["reference"]; got != "my-ref-1" {
		t.Errorf("the caller's reference became %v", got)
	}
	if cancel := testkit.MustJSON(t, reqs[3].Body); len(cancel) != 1 || !referencePattern.MatchString(cancel["reference"].(string)) {
		t.Errorf("a write without params sent %s", reqs[3].Body)
	}
}

func TestReferenceIdenticalAcrossRetries(t *testing.T) {
	s := testkit.NewServer(t, func(w http.ResponseWriter, r *http.Request, n int) {
		switch n {
		case 0:
			testkit.Reply(w, 503, testkit.ErrorBody("SERVICE_UNAVAILABLE", "Try again."))
		case 1:
			testkit.Reply(w, 409, testkit.ErrorBody("REQUEST_IN_PROGRESS", "Still running."))
		default:
			w.Header().Set("Idempotent-Replayed", "true")
			testkit.Reply(w, 201, testkit.OK(testkit.BidResultJSON("bid.place")))
		}
	})
	b := newService(testkit.Partner(t, s))
	res, err := b.Place(context.Background(), &PlaceParams{LWIN18: testkit.LWIN, Quantity: 1, Price: 10})
	if err != nil {
		t.Fatal(err)
	}
	reqs := s.Requests()
	if len(reqs) != 3 {
		t.Fatalf("%d requests, want 3", len(reqs))
	}
	if reqs[0].Body != reqs[1].Body || reqs[1].Body != reqs[2].Body {
		t.Errorf("the body changed across retries:\n%s\n%s\n%s", reqs[0].Body, reqs[1].Body, reqs[2].Body)
	}
	if !res.Replayed || res.Result.Bid.Code != testkit.BidCode {
		t.Errorf("result %+v", res)
	}
}

func TestStagedPlace(t *testing.T) {
	s := testkit.NewServer(t, testkit.Always(202, testkit.OK(testkit.StagedJSON("bid.place"))))
	w, err := newService(testkit.App(t, s)).Place(context.Background(), &PlaceParams{LWIN18: testkit.LWIN, Quantity: 2, Price: 1200.5})
	if err != nil {
		t.Fatal(err)
	}
	if !w.Staged || w.Result != nil || w.Confirmation.Code != testkit.Cnf || w.Confirmation.Action != core.ActionBidPlace {
		t.Errorf("write %+v", w)
	}
}

func TestListQuery(t *testing.T) {
	s := testkit.NewServer(t, testkit.Always(200, testkit.OKPaged(`{"bids":[]}`, `{"limit":50,"offset":0}`)))
	b := newService(testkit.Partner(t, s))
	if _, err := b.List(context.Background(), &ListParams{Status: []string{}}); err != nil || len(s.Requests()[0].Query) != 0 {
		t.Errorf("an empty list was sent: %v, %v", s.Requests()[0].Query, err)
	}
	if _, err := b.List(context.Background(), nil); err != nil || len(s.Requests()[1].Query) != 0 {
		t.Errorf("nil params sent %v, %v", s.Requests()[1].Query, err)
	}
	if _, err := b.List(context.Background(), &ListParams{Status: []string{StatusLive, StatusMatched}, ClientRef: text("ref")}); err != nil {
		t.Fatal(err)
	}
	if q := s.Requests()[2].Query; q.Get("status") != "live,matched" || q.Get("client_ref") != "ref" || len(q) != 2 {
		t.Errorf("query %v", q)
	}
}
