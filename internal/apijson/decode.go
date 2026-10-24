// Package apijson decodes the API's JSON tolerantly, and reads the envelope every answer comes
// in.
//
// The API adds fields and values without notice, and its numbers sometimes arrive as strings, so
// a model decodes field by field: the generated UnmarshalJSON methods call Split, then Decode,
// DecodeInt or DecodeFloat for each field. A field that is missing, null or does not fit its type
// is left unset, and never fails the rest of the answer. Each model keeps the JSON it came from,
// which Lookup reads for a field the SDK does not know yet.
package apijson

import (
	"bytes"
	"encoding/json"
	"math"
	"strconv"
)

// Split copies b and splits it into its fields. The copy is nil for empty input and JSON null;
// the fields are nil when b is not a JSON object.
func Split(b []byte) (json.RawMessage, map[string]json.RawMessage) {
	b = bytes.TrimSpace(b)
	if len(b) == 0 || string(b) == "null" {
		return nil, nil
	}
	raw := append(json.RawMessage(nil), b...)
	var f map[string]json.RawMessage
	if raw[0] == '{' && json.Unmarshal(raw, &f) != nil {
		f = nil
	}
	return raw, f
}

// Decode sets *dst to the field name of f, when it is there and fits; otherwise *dst is left as
// it is.
func Decode[T any](f map[string]json.RawMessage, name string, dst *T) {
	raw, ok := f[name]
	if !ok {
		return
	}
	var v T
	if json.Unmarshal(raw, &v) == nil {
		*dst = v
	}
}

// DecodeInt sets *dst to the integer field name of f, read as Int reads it.
func DecodeInt(f map[string]json.RawMessage, name string, dst *int64) {
	if v, ok := Int(f[name]); ok {
		*dst = v
	}
}

// DecodeIntPtr sets *dst to a pointer to the integer field name of f, read as Int reads it.
func DecodeIntPtr(f map[string]json.RawMessage, name string, dst **int64) {
	if v, ok := Int(f[name]); ok {
		*dst = &v
	}
}

// DecodeFloat sets *dst to the number field name of f, read as Float reads it.
func DecodeFloat(f map[string]json.RawMessage, name string, dst *float64) {
	if v, ok := Float(f[name]); ok {
		*dst = v
	}
}

// DecodeFloatPtr sets *dst to a pointer to the number field name of f, read as Float reads it.
func DecodeFloatPtr(f map[string]json.RawMessage, name string, dst **float64) {
	if v, ok := Float(f[name]); ok {
		*dst = &v
	}
}

// number reads a JSON number, or a string holding one.
func number(raw json.RawMessage) (json.Number, bool) {
	if len(raw) == 0 {
		return "", false
	}
	var n json.Number
	if json.Unmarshal(raw, &n) != nil || n == "" {
		return "", false
	}
	return n, true
}

// Int reads an integer: 1000, 1000.0, 1e3 and "1000", but not 1000.5 or a value out of range.
func Int(raw json.RawMessage) (int64, bool) {
	n, ok := number(raw)
	if !ok {
		return 0, false
	}
	if i, err := strconv.ParseInt(string(n), 10, 64); err == nil {
		return i, true
	}
	v, err := strconv.ParseFloat(string(n), 64)
	if err != nil || v != math.Trunc(v) || v < math.MinInt64 || v >= math.MaxInt64 {
		return 0, false
	}
	return int64(v), true
}

// Float reads a number, or a string holding one, such as "12.5".
func Float(raw json.RawMessage) (float64, bool) {
	n, ok := number(raw)
	if !ok {
		return 0, false
	}
	v, err := strconv.ParseFloat(string(n), 64)
	return v, err == nil
}

// Lookup decodes the field name of the JSON object raw into v, and reports whether the field
// was there.
func Lookup(raw json.RawMessage, name string, v any) (bool, error) {
	_, f := Split(raw)
	field, ok := f[name]
	if !ok {
		return false, nil
	}
	return true, json.Unmarshal(field, v)
}

// Unmarshal decodes data into v, and does nothing when data is empty.
func Unmarshal(data json.RawMessage, v any) error {
	if len(data) == 0 {
		return nil
	}
	return json.Unmarshal(data, v)
}

// DecodeNew decodes b into a new T and returns the pointer. A value that does not decode leaves
// the T as far as it got.
func DecodeNew[T any](b []byte) any {
	v := new(T)
	_ = json.Unmarshal(b, v)
	return v
}
