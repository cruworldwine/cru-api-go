package confirmations //nolint:revive // the package comment is in the generated doc.go, and revive skips generated files

import "encoding/json"

// ActionResult is what a confirmed instruction did, as Service.Confirm returns it.
//
// Action names the action that ran, and Value holds its typed result, chosen by Action: a
// *bids.Result for bid.place, an *offers.Result for offer.list and so on, as Service.Confirm
// lists. For an action this SDK does not know, Value is an *InstructionResult, and Raw holds
// everything the API sent.
type ActionResult struct {
	// Action is the action that ran, such as bid.place: one of the cru.Action constants, or one
	// added since this SDK was released.
	Action string
	// Status is completed.
	Status string
	// Value is the action's typed result.
	Value any
	raw   json.RawMessage
}

// Raw returns the JSON the API sent for the result.
func (r *ActionResult) Raw() json.RawMessage {
	if r == nil {
		return nil
	}
	return r.raw
}

func newActionResult(data json.RawMessage) *ActionResult {
	var head InstructionResult
	_ = json.Unmarshal(data, &head)
	r := &ActionResult{Action: head.Action, Status: head.Status, raw: head.raw, Value: &head}
	if decode, ok := actionResults[head.Action]; ok {
		r.Value = decode(data)
	}
	return r
}
