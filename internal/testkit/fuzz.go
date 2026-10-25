package testkit

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/cruworldwine/cru-api-go/cru/core"
)

// OddJSON are inputs of odd shapes that seed the fuzz targets, besides the invented answers.
var OddJSON = []string{
	``, `null`, `{}`, `[]`, `"text"`, `12`, `true`, `{"a":`, `{"vintage":"2015"}`, `{"vintage":2015.5}`,
	`{"vintage":1e400}`, `{"quantity":"9223372036854775808"}`, `{"price":"12.5"}`, `{"bids":{}}`,
	`{"bids":[null,1,"x",{}]}`, `{"data":{"bids":"no"}}`, `{"meta":{"pagination":{"limit":"x","total":-1}}}`,
	`{"errors":[{"code":12,"details":[]}]}`, `{"a":{"b":{"c":{"d":[[[[]]]]}}}}`, "{\"wine_name\":\"\xff\xfe\"}",
}

// AddSeeds seeds a fuzz target with OddJSON and the data of each sample answer.
func AddSeeds(f *testing.F, samples map[string]Sample) {
	for _, s := range OddJSON {
		f.Add([]byte(s))
	}
	for _, sample := range samples {
		if sample.Data != "" {
			f.Add([]byte(sample.Data))
		}
	}
}

// Decodes checks one model with b: valid JSON always decodes, the model keeps it for Raw, a list
// of the model decodes without panicking, and a nil model's Raw is nil.
func Decodes[T any, PT interface {
	*T
	core.Model
}](t *testing.T, b []byte) {
	t.Helper()
	v := PT(new(T))
	err := json.Unmarshal(b, v)
	if json.Valid(b) {
		if err != nil {
			t.Fatalf("%T refused valid JSON: %v", v, err)
		}
		if !bytes.Equal(bytes.TrimSpace(b), []byte("null")) && len(v.Raw()) == 0 {
			t.Fatalf("%T kept no Raw", v)
		}
	}
	var list []T
	_ = json.Unmarshal(b, &list)
	if PT(nil).Raw() != nil {
		t.Fatalf("a nil %T has Raw", v)
	}
}
