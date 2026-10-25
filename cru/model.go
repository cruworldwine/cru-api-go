package cru

import "github.com/cruworldwine/cru-api-go/cru/core"

// Model is implemented by every object the API answers with: Raw returns the JSON it was decoded
// from, with every field the API sent. It is core.Model.
type Model = core.Model

// Field decodes the field name of a model's raw JSON into v: use it for a field this SDK does
// not know yet. It reports whether the field was there.
func Field(m Model, name string, v any) (bool, error) { return core.Field(m, name, v) }
