package core

// Write is what a write did.
//
// A partner credential's write runs at once: Staged is false and Result holds what it did. A
// connected app's write is staged: Staged is true, nothing has happened yet, Result is nil and
// Confirmation holds the code to pass to client.Confirmations.Confirm once the customer agrees.
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
