// Package corelink lets the SDK's internal packages reach what package core keeps unexported, so
// that core's exported API holds only what programs may name. Package core sets these when it is
// initialised, which is before any package importing it runs.
package corelink

import "context"

var (
	// PublicError turns the transport's errors into *core.APIError and *core.ConnectionError, and
	// returns any other error as it is.
	PublicError func(error) error

	// SetNext gives page, a *core.Page, the function that fetches the page after it: a page of
	// the same type, at offset.
	SetNext func(page any, next func(ctx context.Context, offset int) (any, error))
)
