package testkit

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/cruworldwine/cru-api-go/cru/core"
	"github.com/cruworldwine/cru-api-go/internal/caller"
)

// Each package's sample_gen_test.go, generated from the spec, holds sampleOps, which calls every
// operation of the package from an Input, and sampleCalls, each operation's invented inputs and
// answer with every parameter and every field set. EveryOperation runs them.

// Op is one operation, and how a test calls it.
type Op struct {
	ID, Method, Path, Kind, Key string
	Call                        func(ctx context.Context, c *caller.Caller, in *Input) (any, error)
	// Unwrap reads a confirmation's result: its typed value, the JSON it came from, and whether
	// its action was decoded into its own result. Only a confirm operation has one.
	Unwrap func(result any) (value any, raw json.RawMessage, decoded bool)
}

// Sample is an operation's invented call and answer.
type Sample struct {
	Path  map[string]string
	Query string // a JSON object of query parameters, each as the string sent
	Body  string // a JSON object of body fields
	Data  string // the answer's data
}

// Input is what an Op's call reads its arguments from.
type Input struct {
	PathValues map[string]string
	Query      map[string]any
	Body       json.RawMessage
	Opts       []core.RequestOption
}

// Path reads the path parameter name.
func (in *Input) Path(name string) string { return in.PathValues[name] }

// PathInt reads the integer path parameter name.
func (in *Input) PathInt(name string) int64 {
	v, _ := strconv.ParseInt(in.PathValues[name], 10, 64)
	return v
}

func (in *Input) text(name string) (string, bool) {
	switch v := in.Query[name].(type) {
	case string:
		return v, true
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64), true
	case bool:
		return strconv.FormatBool(v), true
	}
	return "", false
}

// Str reads an optional string query parameter.
func (in *Input) Str(name string) *string {
	if v, ok := in.text(name); ok {
		return &v
	}
	return nil
}

// StrValue reads a required string query parameter.
func (in *Input) StrValue(name string) string {
	v, _ := in.text(name)
	return v
}

// Int reads an optional integer query parameter.
func (in *Input) Int(name string) *int64 {
	if v, ok := in.text(name); ok {
		if i, err := strconv.ParseInt(v, 10, 64); err == nil {
			return &i
		}
	}
	return nil
}

// IntValue reads a required integer query parameter.
func (in *Input) IntValue(name string) int64 { return value(in.Int(name)) }

// Float reads an optional number query parameter.
func (in *Input) Float(name string) *float64 {
	if v, ok := in.text(name); ok {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			return &f
		}
	}
	return nil
}

// FloatValue reads a required number query parameter.
func (in *Input) FloatValue(name string) float64 { return value(in.Float(name)) }

// Bool reads an optional boolean query parameter.
func (in *Input) Bool(name string) *bool {
	if v, ok := in.text(name); ok {
		if b, err := strconv.ParseBool(v); err == nil {
			return &b
		}
	}
	return nil
}

// BoolValue reads a required boolean query parameter.
func (in *Input) BoolValue(name string) bool { return value(in.Bool(name)) }

// List reads a comma-separated list query parameter.
func (in *Input) List(name string) []string {
	if v, ok := in.text(name); ok {
		return strings.Split(v, ",")
	}
	return nil
}

// DecodeBody fills p, a params struct, from the body.
func (in *Input) DecodeBody(p any) { _ = json.Unmarshal(in.Body, p) }

func value[T any](p *T) T {
	if p == nil {
		var zero T
		return zero
	}
	return *p
}

// SamplePhoto is the binary answer EveryOperation serves a binary operation.
var SamplePhoto = []byte("\xff\xd8\xff\xe0 an invented photo")

// EveryOperation calls every operation in ops with every parameter set and checks what it sent
// (the method, the escaped path, each query parameter and each body field), that every field of
// the answer decoded, and that the method passes its RequestOptions on.
func EveryOperation(t *testing.T, ops []Op, samples map[string]Sample) {
	t.Helper()
	if len(samples) != len(ops) {
		t.Fatalf("%d sample calls for %d operations", len(samples), len(ops))
	}
	for i := range ops {
		op := &ops[i]
		t.Run(op.ID, func(t *testing.T) {
			sample, ok := samples[op.ID]
			if !ok {
				t.Fatal("no sample call")
			}
			status, body := answer(op, sample)
			s := NewServer(t, func(w http.ResponseWriter, _ *http.Request, _ int) {
				if op.Kind == "binary" {
					w.Header().Set("Content-Type", "image/jpeg")
					_, _ = w.Write(SamplePhoto)
					return
				}
				Reply(w, status, body)
			})
			c := Partner(t, s)
			in := &Input{PathValues: sample.Path, Body: json.RawMessage(sample.Body)}
			if sample.Query != "" {
				if err := json.Unmarshal([]byte(sample.Query), &in.Query); err != nil {
					t.Fatal(err)
				}
			}
			res, err := op.Call(context.Background(), c, in)
			if op.Kind == "binary" {
				if got, _ := res.([]byte); err != nil || !bytes.Equal(got, SamplePhoto) {
					t.Fatalf("photo %q, %v", got, err)
				}
			} else if problems := checkAnswer(op, status, body, res, err); len(problems) > 0 {
				t.Fatal(strings.Join(problems, "; "))
			}
			reqs := s.Requests()
			if len(reqs) != 1 {
				t.Fatalf("%d requests", len(reqs))
			}
			checkSent(t, op, sample, reqs[0])

			var meta core.ResponseMeta
			in.Opts = []core.RequestOption{core.WithResponseInto(&meta), core.WithRequestHeader("X-Trace", "t1"), core.WithExtraQuery("extra", "1")}
			if _, err := op.Call(context.Background(), c, in); err != nil {
				t.Fatalf("with options: %v", err)
			}
			sent := s.Requests()[1]
			if meta.StatusCode != status || meta.Attempts != 1 || sent.Header.Get("X-Trace") != "t1" || sent.Query.Get("extra") != "1" {
				t.Errorf("with options: meta %+v, header %q, query %v", meta, sent.Header.Get("X-Trace"), sent.Query)
			}
		})
	}
}

func answer(op *Op, sample Sample) (int, string) {
	switch op.Kind {
	case "empty":
		return http.StatusNoContent, ""
	case "binary":
		return http.StatusOK, ""
	case "page":
		return http.StatusOK, OKPaged(sample.Data, `{"limit":1,"offset":0,"total":1,"has_more":false}`)
	}
	return http.StatusOK, OK(sample.Data)
}

func checkSent(t *testing.T, op *Op, sample Sample, got Recorded) {
	t.Helper()
	path := op.Path
	for name, v := range sample.Path {
		path = strings.Replace(path, "{"+name+"}", url.PathEscape(v), 1)
	}
	if got.Method != op.Method || got.Path != path {
		t.Errorf("sent %s %s, want %s %s", got.Method, got.Path, op.Method, path)
	}
	var query map[string]string
	if sample.Query != "" {
		_ = json.Unmarshal([]byte(sample.Query), &query)
	}
	for name, want := range query {
		if v := got.Query.Get(name); v != want {
			t.Errorf("query %s = %q, want %q", name, v, want)
		}
	}
	if len(got.Query) != len(query) {
		t.Errorf("sent the query %v, want %v", got.Query, query)
	}
	if sample.Body == "" {
		return
	}
	var want, sent map[string]any
	_ = json.Unmarshal([]byte(sample.Body), &want)
	if err := json.Unmarshal([]byte(got.Body), &sent); err != nil {
		t.Fatalf("the body %q is not a JSON object: %v", got.Body, err)
	}
	for name, v := range want {
		if !reflect.DeepEqual(sent[name], v) {
			t.Errorf("body field %s = %v, want %v", name, sent[name], v)
		}
	}
}

// checkAnswer reports how the typed result of a successful call differs from the answer.
func checkAnswer(op *Op, status int, body string, res any, err error) []string {
	if err != nil {
		return []string{fmt.Sprintf("unexpected %T: %v", err, err)}
	}
	var envelope struct {
		Data map[string]json.RawMessage `json:"data"`
	}
	_ = json.Unmarshal([]byte(body), &envelope)
	switch op.Kind {
	case "empty":
		return nil
	case "get":
		m, ok := res.(core.Model)
		if !ok || len(m.Raw()) == 0 {
			return []string{"no model"}
		}
		return Lossless(op.ID, res, m.Raw())
	case "list", "page":
		items := reflect.ValueOf(res)
		if op.Kind == "page" {
			items = items.Elem().FieldByName("Items")
		}
		var want []json.RawMessage
		_ = json.Unmarshal(envelope.Data[op.Key], &want)
		if items.Len() != len(want) {
			return []string{fmt.Sprintf("%d items, want %d", items.Len(), len(want))}
		}
		var out []string
		for i := range want {
			out = append(out, Lossless(fmt.Sprintf("%s[%d]", op.ID, i), items.Index(i).Interface(), want[i])...)
		}
		return out
	case "write", "confirm":
		w := reflect.ValueOf(res).Elem()
		if status == http.StatusAccepted {
			return []string{"a sample answer is never staged"}
		}
		result := w.FieldByName("Result")
		if result.IsNil() {
			return []string{"no result"}
		}
		if op.Unwrap != nil {
			value, raw, decoded := op.Unwrap(result.Interface())
			if !decoded {
				return []string{"the action was not decoded into its result"}
			}
			return Lossless(op.ID, value, raw)
		}
		m := result.Interface().(core.Model)
		return Lossless(op.ID, result.Interface(), m.Raw())
	}
	return []string{"unknown kind " + op.Kind}
}

// Lossless reports the fields whose typed value differs from what the API sent: a field the SDK
// dropped because its value did not fit, or a required field the API left out.
func Lossless(where string, typed any, raw json.RawMessage) []string {
	b, err := json.Marshal(typed)
	if err != nil {
		return []string{where + ": " + err.Error()}
	}
	var t, r any
	_ = json.Unmarshal(b, &t)
	_ = json.Unmarshal(raw, &r)
	var out []string
	compare(where, t, r, &out)
	return out
}

func compare(where string, t, r any, out *[]string) {
	switch tv := t.(type) {
	case map[string]any:
		rv, ok := r.(map[string]any)
		if !ok {
			*out = append(*out, where+": not an object")
			return
		}
		for k, v := range tv {
			rvk, present := rv[k]
			if !present {
				if v != nil {
					*out = append(*out, where+"."+k+": required but absent")
				}
				continue
			}
			compare(where+"."+k, v, rvk, out)
		}
	case []any:
		rv, ok := r.([]any)
		if !ok || len(rv) != len(tv) {
			*out = append(*out, where+": list differs")
			return
		}
		for i := range tv {
			compare(fmt.Sprintf("%s[%d]", where, i), tv[i], rv[i], out)
		}
	default:
		if !reflect.DeepEqual(t, r) {
			*out = append(*out, where+": value lost in decoding")
		}
	}
}
