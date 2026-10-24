package cru

import (
	"bytes"
	"encoding/json"
	"math"
	"strconv"
)

// Model is implemented by every object the API answers with.
type Model interface {
	// Raw returns the JSON the model was decoded from, with every field the API sent,
	// including fields added since this SDK was released. It is nil for a model that was not
	// decoded from JSON.
	Raw() json.RawMessage
}

// Field decodes the field name of a model's raw JSON into v: use it for a field this SDK does
// not know yet. It reports whether the field was there.
func Field(m Model, name string, v any) (bool, error) {
	if m == nil {
		return false, nil
	}
	_, f := splitObject(m.Raw())
	raw, ok := f[name]
	if !ok {
		return false, nil
	}
	return true, json.Unmarshal(raw, v)
}

// splitObject copies b and splits it into its fields. The copy is nil for JSON null; the
// fields are nil when b is not an object.
func splitObject(b []byte) (json.RawMessage, map[string]json.RawMessage) {
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

// The decode helpers set *dst only when the field is present and fits, so one bad field never
// fails the model it is in.

func decodeField[T any](f map[string]json.RawMessage, name string, dst *T) {
	raw, ok := f[name]
	if !ok {
		return
	}
	var v T
	if json.Unmarshal(raw, &v) == nil {
		*dst = v
	}
}

func decodeInt(f map[string]json.RawMessage, name string, dst *int64) {
	if v, ok := intOf(f[name]); ok {
		*dst = v
	}
}

func decodeIntPtr(f map[string]json.RawMessage, name string, dst **int64) {
	if v, ok := intOf(f[name]); ok {
		*dst = &v
	}
}

func decodeFloat(f map[string]json.RawMessage, name string, dst *float64) {
	if v, ok := floatOf(f[name]); ok {
		*dst = v
	}
}

func decodeFloatPtr(f map[string]json.RawMessage, name string, dst **float64) {
	if v, ok := floatOf(f[name]); ok {
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

// intOf accepts 1000, 1000.0, 1e3 and "1000", but not 1000.5.
func intOf(raw json.RawMessage) (int64, bool) {
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

func floatOf(raw json.RawMessage) (float64, bool) {
	n, ok := number(raw)
	if !ok {
		return 0, false
	}
	v, err := strconv.ParseFloat(string(n), 64)
	return v, err == nil
}

func decodeAs[T any](b []byte) any {
	v := new(T)
	_ = json.Unmarshal(b, v)
	return v
}
