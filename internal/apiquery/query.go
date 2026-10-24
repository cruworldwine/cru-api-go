// Package apiquery encodes a call's parameters into its query string, the way the API reads
// them: a nil optional parameter is left out, numbers are written in full (1200.5, never
// 1.2005e+03), and a list is sent once, comma-separated.
package apiquery

import (
	"net/url"
	"strconv"
	"strings"
)

// Value is a type a query parameter can hold.
type Value interface {
	string | int64 | float64 | bool
}

// Format writes v as the API reads it.
func Format[T Value](v T) string {
	switch x := any(v).(type) {
	case int64:
		return strconv.FormatInt(x, 10)
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(x)
	}
	return any(v).(string)
}

// Set sets the parameter name to *v, or leaves it out when v is nil.
func Set[T Value](q url.Values, name string, v *T) {
	if v != nil {
		q.Set(name, Format(*v))
	}
}

// SetList sets the parameter name to the values of v, comma-separated, or leaves it out when v
// is empty.
func SetList[T Value](q url.Values, name string, v []T) {
	if len(v) == 0 {
		return
	}
	parts := make([]string, len(v))
	for i, x := range v {
		parts[i] = Format(x)
	}
	q.Set(name, strings.Join(parts, ","))
}
