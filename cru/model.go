package cru

import (
	"encoding/json"

	"github.com/cruworldwine/cru-api-go/internal/apijson"
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
	return apijson.Lookup(m.Raw(), name, v)
}
