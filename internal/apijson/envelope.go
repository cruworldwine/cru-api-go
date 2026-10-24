package apijson

import (
	"encoding/json"
	"net/http"
)

// The API answers with an envelope: {"status", "message", "meta", "data", "errors"}. These read
// the parts of it the SDK needs before, or without, decoding a model.

// Data returns the envelope's data, or data[key] when key is not empty. ok is false when data
// is not an object holding key.
func Data(f map[string]json.RawMessage, key string) (json.RawMessage, bool) {
	data := f["data"]
	if key == "" {
		return data, true
	}
	_, inner := Split(data)
	v, ok := inner[key]
	return v, ok
}

// List decodes raw as a list of T. Null, an absent value and {} (as PHP encodes an empty list)
// are an empty list; anything else that is not a list is an error.
func List[T any](raw json.RawMessage) ([]T, error) {
	var items []T
	if err := Unmarshal(raw, &items); err != nil {
		if _, obj := Split(raw); obj != nil && len(obj) == 0 {
			return nil, nil
		}
		return nil, err
	}
	return items, nil
}

// RequestID returns the envelope's meta.request_id, else the X-Request-Id header.
func RequestID(f map[string]json.RawMessage, header http.Header) string {
	var id string
	if raw, ok := f["meta"]; ok {
		_, meta := Split(raw)
		Decode(meta, "request_id", &id)
	}
	if id == "" {
		id = header.Get("X-Request-Id")
	}
	return id
}

// FirstError returns the code and details of an error answer's first entry in errors, empty
// when there is none.
func FirstError(body []byte) (code string, details map[string]any) {
	_, f := Split(body)
	var entries []json.RawMessage
	Decode(f, "errors", &entries)
	if len(entries) == 0 {
		return "", nil
	}
	_, first := Split(entries[0])
	Decode(first, "code", &code)
	Decode(first, "details", &details)
	return code, details
}
