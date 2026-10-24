package cru

import "encoding/json"

// Write is what a write did.
//
// A partner credential's write runs at once: Staged is false and Result holds what it did. A
// connected app's write is staged: Staged is true, nothing has happened yet, Result is nil and
// Confirmation holds the code to pass to Confirmations.Confirm once the customer agrees.
type Write[T any] struct {
	// Result is what the write did; nil when it was staged.
	Result *T
	// Confirmation is the staged instruction; nil when the write ran.
	Confirmation *StagedConfirmation
	// Staged is true when the API answered 202: the write waits for confirmation.
	Staged bool
	// Replayed is true when this is the stored answer to an earlier request with the same
	// reference (the Idempotent-Replayed header), not a new action.
	Replayed bool
	// StatusCode is the HTTP status: 200, 201, or 202 when staged.
	StatusCode int
	// RequestID is meta.request_id, else the X-Request-Id header.
	RequestID string
}

// ActionResult is what a confirmed instruction did, as Confirmations.Confirm returns it.
//
// Action names the action that ran, and Value holds its typed result, chosen by Action: a
// *BidResult for bid.place, an *OfferResult for offer.list and so on, as Confirmations.Confirm
// lists. For an action this SDK does not know, Value is an *InstructionResult, and Raw holds
// everything the API sent.
type ActionResult struct {
	// Action is the action that ran, such as bid.place: one of the Action constants, or one
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
