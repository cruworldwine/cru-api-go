package cru

import "github.com/cruworldwine/cru-api-go/internal/version"

// Version is this SDK's version, sent in the User-Agent header.
const Version = version.Version

// String returns a pointer to v, for an optional parameter.
func String(v string) *string { return &v }

// Int returns a pointer to v, for an optional parameter.
func Int(v int64) *int64 { return &v }

// Float returns a pointer to v, for an optional parameter.
func Float(v float64) *float64 { return &v }

// Bool returns a pointer to v, for an optional parameter.
func Bool(v bool) *bool { return &v }

// Value returns what p points to, or the zero value when p is nil, so a nullable field reads
// without a nil check: cru.Value(wine.WineName).
func Value[T any](p *T) T {
	if p == nil {
		var zero T
		return zero
	}
	return *p
}
