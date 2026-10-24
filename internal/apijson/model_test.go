package apijson

import "encoding/json"

// wine and line are shaped as the generated models are: a field of every kind, decoded field by
// field with the functions the generated UnmarshalJSON methods call.
type wine struct {
	LWIN18    string         `json:"lwin18"`
	Name      *string        `json:"wine_name,omitempty"`
	Vintage   *int64         `json:"vintage,omitempty"`
	Quantity  int64          `json:"quantity"`
	Price     float64        `json:"price"`
	Market    *float64       `json:"market_price,omitempty"`
	Listed    bool           `json:"is_listed"`
	Scopes    []string       `json:"scopes,omitempty"`
	Details   map[string]any `json:"details,omitempty"`
	Anything  any            `json:"anything,omitempty"`
	Line      line           `json:"line"`
	Lines     []line         `json:"lines,omitempty"`
	LastTrade *line          `json:"last_trade,omitempty"`

	raw json.RawMessage
}

func (m *wine) Raw() json.RawMessage { return m.raw }

func (m *wine) UnmarshalJSON(b []byte) error {
	raw, f := Split(b)
	if raw == nil {
		return nil
	}
	*m = wine{raw: raw}
	Decode(f, "lwin18", &m.LWIN18)
	Decode(f, "wine_name", &m.Name)
	DecodeIntPtr(f, "vintage", &m.Vintage)
	DecodeInt(f, "quantity", &m.Quantity)
	DecodeFloat(f, "price", &m.Price)
	DecodeFloatPtr(f, "market_price", &m.Market)
	Decode(f, "is_listed", &m.Listed)
	Decode(f, "scopes", &m.Scopes)
	Decode(f, "details", &m.Details)
	Decode(f, "anything", &m.Anything)
	Decode(f, "line", &m.Line)
	Decode(f, "lines", &m.Lines)
	Decode(f, "last_trade", &m.LastTrade)
	return nil
}

type line struct {
	ID    string   `json:"id"`
	Cost  *float64 `json:"cost,omitempty"`
	Units int64    `json:"units"`

	raw json.RawMessage
}

func (m *line) Raw() json.RawMessage { return m.raw }

func (m *line) UnmarshalJSON(b []byte) error {
	raw, f := Split(b)
	if raw == nil {
		return nil
	}
	*m = line{raw: raw}
	Decode(f, "id", &m.ID)
	DecodeFloatPtr(f, "cost", &m.Cost)
	DecodeInt(f, "units", &m.Units)
	return nil
}
