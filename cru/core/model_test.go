package core_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/cruworldwine/cru-api-go/cru/core"
	"github.com/cruworldwine/cru-api-go/internal/testkit"
)

func TestField(t *testing.T) {
	var conf core.StagedConfirmation
	if err := json.Unmarshal([]byte(testkit.StagedJSON("bid.place")), &struct {
		Confirmation *core.StagedConfirmation `json:"confirmation"`
	}{&conf}); err != nil {
		t.Fatal(err)
	}
	if conf.Code != testkit.Cnf || conf.Action != core.ActionBidPlace || conf.Status != core.ConfirmationStatusPending {
		t.Fatalf("confirmation %+v", conf)
	}
	var lwin string
	if found, err := core.Field(&conf.Preview, "lwin18", &lwin); !found || err != nil || lwin != testkit.LWIN {
		t.Errorf("Field(lwin18) = %v, %v, %q", found, err, lwin)
	}
	if found, _ := core.Field(&conf.Preview, "absent", &lwin); found {
		t.Error("Field found an absent field")
	}
	var price string
	if found, err := core.Field(&conf.Preview, "price", &price); !found || err == nil {
		t.Errorf("a field of another type: %v, %v", found, err)
	}
	var nilModel *core.Meta
	if found, err := core.Field(nilModel, "version", new(string)); found || err != nil {
		t.Error("Field of a nil model")
	}
	if found, err := core.Field(nil, "version", new(string)); found || err != nil {
		t.Error("Field of no model")
	}
}

func TestEnvelopeModels(t *testing.T) {
	var meta core.Meta
	raw := testkit.Meta(`"pagination":{"limit":"50","offset":0.0,"total":2,"has_more":true},"new":1`)
	if err := json.Unmarshal([]byte(raw), &meta); err != nil {
		t.Fatal(err)
	}
	pg := meta.Pagination
	if meta.RequestID != "req_test" || pg == nil || pg.Limit != 50 || pg.Offset != 0 || *pg.Total != 2 || !*pg.HasMore {
		t.Errorf("meta %+v, pagination %+v", meta, pg)
	}
	if !strings.Contains(string(meta.Raw()), `"new":1`) || !strings.Contains(string(pg.Raw()), `"limit":"50"`) {
		t.Error("an envelope model lost its JSON")
	}
	var entry core.ErrorEntry
	if err := json.Unmarshal([]byte(`{"code":"TRADE_REFUSED","message":"No.","field":null,"details":{"a":1}}`), &entry); err != nil {
		t.Fatal(err)
	}
	if entry.Code != core.ErrorCodeTradeRefused || entry.Field != nil || entry.Details["a"] != 1.0 {
		t.Errorf("entry %+v", entry)
	}
}
