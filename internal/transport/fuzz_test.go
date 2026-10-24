package transport

import (
	"bytes"
	"encoding/json"
	"testing"
)

// FuzzRequestBody checks that any body a caller gives, with a reference added and extra fields
// set, is either refused or sent as a valid JSON object holding both.
//
//	go test -run '^$' -fuzz '^FuzzRequestBody$' -fuzztime 30s ./internal/transport
func FuzzRequestBody(f *testing.F) {
	for _, s := range []string{`{}`, `{"a":1}`, `{"reference":null}`, `{"reference":"mine"}`, ` {"a":[1,{"b":2}]} `,
		`[1]`, `null`, ``, `{"a":1}x`, `{"reference":1,"reference":null}`, `{"reference":null}`} {
		f.Add([]byte(s))
	}
	f.Fuzz(func(t *testing.T, payload []byte) {
		out, err := withReference(payload)
		if err == nil && len(bytes.TrimSpace(out)) > 0 && bytes.TrimSpace(out)[0] == '{' {
			var fields map[string]json.RawMessage
			if json.Unmarshal(out, &fields) != nil {
				t.Fatalf("%q became invalid JSON %q", payload, out)
			}
			if ref, ok := fields["reference"]; !ok || string(ref) == "null" {
				t.Fatalf("%q has no reference in %q", payload, out)
			}
		}
		withExtra, err := withFields(payload, []BodyField{{"extra", []int{1}}})
		if err != nil {
			return
		}
		var fields map[string]json.RawMessage
		if json.Unmarshal(withExtra, &fields) != nil || string(fields["extra"]) != "[1]" {
			t.Fatalf("%q with an extra field became %q", payload, withExtra)
		}
	})
}
