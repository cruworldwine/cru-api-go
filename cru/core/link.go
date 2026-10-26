package core

import (
	"context"

	"github.com/cruworldwine/cru-api-go/internal/corelink"
)

// The SDK's internal packages build core's errors and pages through corelink, so that what they
// need stays unexported here.
func init() {
	corelink.PublicError = publicError
	corelink.SetNext = func(page any, next func(ctx context.Context, offset int) (any, error)) {
		page.(interface {
			setNext(func(ctx context.Context, offset int) (any, error))
		}).setNext(next)
	}
}
