package warehouses

import (
	"context"
	"testing"

	"github.com/cruworldwine/cru-api-go/internal/testkit"
)

func TestInbondPathEncoding(t *testing.T) {
	s := testkit.NewServer(t, testkit.Always(200, testkit.OK(`{"inbond_warehouse":{"inbond_address_id":42,"name":"Bond Test"}}`)))
	w, err := newInbondService(testkit.Partner(t, s)).Get(context.Background(), 42)
	if err != nil || s.Requests()[0].Path != "/warehouses/inbond/42" || w.InbondAddressID != 42 || w.Name != "Bond Test" {
		t.Errorf("path %s: %+v, %v", s.Requests()[0].Path, w, err)
	}
}
