package core_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/cruworldwine/cru-api-go/cru/bids"
	"github.com/cruworldwine/cru-api-go/cru/core"
	"github.com/cruworldwine/cru-api-go/cru/wines"
	"github.com/cruworldwine/cru-api-go/internal/caller"
	"github.com/cruworldwine/cru-api-go/internal/testkit"
)

// pagedBids answers GET /bids from a list of 5 bids, limit 2, as the API pages.
func pagedBids(failAt int) func(http.ResponseWriter, *http.Request, int) {
	return func(w http.ResponseWriter, r *http.Request, n int) {
		offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
		if offset == failAt {
			testkit.Reply(w, 500, testkit.ErrorBody("INTERNAL_ERROR", "Oops."))
			return
		}
		var items []string
		for i := offset; i < offset+2 && i < 5; i++ {
			items = append(items, testkit.BidJSON(fmt.Sprintf("BID-%d", i)))
		}
		pagination := fmt.Sprintf(`{"limit":2,"offset":%d,"total":5,"has_more":%v}`, offset, offset+2 < 5)
		testkit.Reply(w, 200, testkit.OKPaged(`{"bids":[`+strings.Join(items, ",")+`],"summary":{"live":5}}`, pagination))
	}
}

func TestPaging(t *testing.T) {
	s := testkit.NewServer(t, pagedBids(-1))
	ctx := context.Background()
	page, err := bidsOn(testkit.Partner(t, s, noRetries)).List(ctx, &bids.ListParams{Status: []string{bids.StatusLive}, Limit: int64Ptr(2)})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 2 || page.Limit != 2 || page.Offset != 0 || page.Total == nil || *page.Total != 5 || !page.HasMore {
		t.Fatalf("first page %+v", page)
	}
	if page.Meta.RequestID != "req_test" || !strings.Contains(string(page.Data), `"summary"`) {
		t.Errorf("meta %+v, data %s", page.Meta, page.Data)
	}

	var codes []string
	for bid, err := range page.All(ctx) {
		if err != nil {
			t.Fatal(err)
		}
		codes = append(codes, bid.Code)
	}
	if strings.Join(codes, " ") != "BID-0 BID-1 BID-2 BID-3 BID-4" {
		t.Errorf("All() gave %v", codes)
	}
	reqs := s.Requests()
	if len(reqs) != 3 {
		t.Fatalf("%d requests, want 3", len(reqs))
	}
	for i, want := range []string{"", "2", "4"} {
		if reqs[i].Query.Get("offset") != want || reqs[i].Query.Get("status") != "live" || reqs[i].Query.Get("limit") != "2" {
			t.Errorf("page %d sent %v", i, reqs[i].Query)
		}
	}

	last, err := page.Next(ctx)
	if err != nil {
		t.Fatal(err)
	}
	last, _ = last.Next(ctx)
	if end, err := last.Next(ctx); end != nil || err != nil || last.HasMore || len(last.Items) != 1 {
		t.Errorf("past the last page: %v, %v", end, err)
	}
}

func TestPagingStops(t *testing.T) {
	s := testkit.NewServer(t, pagedBids(2))
	page, err := bidsOn(testkit.Partner(t, s, noRetries)).List(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	var n int
	var last error
	for _, err := range page.All(context.Background()) {
		if err != nil {
			last = err
			continue
		}
		n++
	}
	if n != 2 || !errors.Is(last, core.ErrServer) {
		t.Errorf("%d items, then %v", n, last)
	}

	s = testkit.NewServer(t, pagedBids(-1))
	page, _ = bidsOn(testkit.Partner(t, s)).List(context.Background(), nil)
	for _, err := range page.All(context.Background()) {
		if err != nil {
			t.Fatal(err)
		}
		break
	}
	if len(s.Requests()) != 1 {
		t.Errorf("breaking out fetched %d pages", len(s.Requests()))
	}
}

func TestPageWithoutHasMore(t *testing.T) {
	s := testkit.NewServer(t, func(w http.ResponseWriter, r *http.Request, n int) {
		if n == 0 {
			testkit.Reply(w, 200, testkit.OKPaged(`{"wines":[{"lwin18":"1"},{"lwin18":"2"}]}`, `{"limit":2,"offset":0}`))
			return
		}
		testkit.Reply(w, 200, testkit.OKPaged(`{"wines":[{"lwin18":"3"}]}`, `{"limit":2,"offset":2}`))
	})
	search := caller.NewService[wines.Service](testkit.Partner(t, s))
	page, err := search.Search(context.Background(), &wines.SearchParams{Q: "exemple"})
	if err != nil || !page.HasMore || page.Total != nil {
		t.Fatalf("a full page without has_more: %+v, %v", page, err)
	}
	next, _ := page.Next(context.Background())
	if next.HasMore || next.Offset != 2 || next.Items[0].LWIN18 != "3" {
		t.Errorf("a short page: %+v", next)
	}
}

func TestPageWithoutNext(t *testing.T) {
	var nilPage *core.Page[bids.OwnBid]
	if next, err := nilPage.Next(context.Background()); next != nil || err != nil {
		t.Errorf("a nil page's next: %v, %v", next, err)
	}
	// A page made by hand has no next page to fetch, whatever it says.
	page := &core.Page[bids.OwnBid]{Items: []bids.OwnBid{{Code: "BID-1"}}, HasMore: true}
	var codes []string
	for bid, err := range page.All(context.Background()) {
		if err != nil {
			t.Fatal(err)
		}
		codes = append(codes, bid.Code)
	}
	if len(codes) != 1 {
		t.Errorf("All() gave %v", codes)
	}
}
