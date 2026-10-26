package portfolio

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/cruworldwine/cru-api-go/cru/core"
	"github.com/cruworldwine/cru-api-go/internal/testkit"
)

func decode[T any](t *testing.T, s string) *T {
	t.Helper()
	v := new(T)
	if err := json.Unmarshal([]byte(s), v); err != nil {
		t.Fatalf("decoding failed: %v", err)
	}
	return v
}

func TestNestedRaw(t *testing.T) {
	wine := decode[Wine](t, `{"lwin18":"`+testkit.LWIN+`","currency":"GBP","total_quantity":3,"portfolio":[
		{"portfolio_id":"P-1","stock_type":"cru","quantity":3,"pricing":{"cost":100,"listing_price":null,"tax":7},
		 "warehouse":{"location_type":"vault","current_location":null},"future":"yes"}]}`)
	line := wine.Portfolio[0]
	if line.PortfolioID != "P-1" || line.Quantity != 3 || line.StockType != core.StockTypeCru {
		t.Fatalf("line %+v", line)
	}
	if *line.Pricing.Cost != 100 || line.Pricing.ListingPrice != nil || line.Warehouse.LocationType == nil || *line.Warehouse.LocationType != "vault" {
		t.Errorf("pricing %+v, warehouse %+v", line.Pricing, line.Warehouse)
	}
	if !strings.Contains(string(line.Raw()), `"future":"yes"`) || !strings.Contains(string(line.Pricing.Raw()), `"tax":7`) {
		t.Error("a nested model lost its raw JSON")
	}
}

func TestWrongTypesLeaveZero(t *testing.T) {
	// PHP encodes an empty object as [].
	line := decode[Line](t, `{"portfolio_id":"P-1","pricing":[],"warehouse":[],"photo_urls":{}}`)
	if line.PortfolioID != "P-1" || line.Pricing.Cost != nil || line.PhotoURLs != nil {
		t.Errorf("%+v", line)
	}
	if string(line.Pricing.Raw()) != "[]" {
		t.Errorf("raw of a non-object: %s", line.Pricing.Raw())
	}
	if line.Warehouse.LocationType != nil || LocationTypeCru != "cru" {
		t.Errorf("warehouse %+v", line.Warehouse)
	}
}
