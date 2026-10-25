package wines

import (
	"context"
	"testing"

	"github.com/cruworldwine/cru-api-go/internal/testkit"
)

func TestRequiredQueryValue(t *testing.T) {
	s := testkit.NewServer(t, testkit.Always(200, testkit.OKPaged(`{"wines":[]}`, `{"limit":20,"offset":0,"has_more":false}`)))
	vintage := int64(2015)
	if _, err := newService(testkit.Partner(t, s)).Search(context.Background(), &SearchParams{Q: "exemple", Vintage: &vintage}); err != nil {
		t.Fatal(err)
	}
	q := s.Requests()[0].Query
	if q.Get("q") != "exemple" || q.Get("vintage") != "2015" || len(q) != 2 {
		t.Errorf("query %v", q)
	}
}
