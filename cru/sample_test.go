package cru

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
)

// sample_gen_test.go, generated from the spec, holds sampleOps, which calls every operation
// from a sampleInput, and sampleCalls, each operation's invented inputs and answer with every
// parameter and every field set.

type sampleOp struct {
	id, method, path, kind, key string
	call                        func(ctx context.Context, c *Client, in *sampleInput) (any, error)
}

type sampleCall struct {
	path  map[string]string
	query string // a JSON object of query parameters, each as the string sent
	body  string // a JSON object of body fields
	data  string // the answer's data
}

type sampleInput struct {
	pathValues map[string]string
	query      map[string]any
	body       json.RawMessage
	opts       []RequestOption
}

func (in *sampleInput) path(name string) string { return in.pathValues[name] }

func (in *sampleInput) pathInt(name string) int64 {
	v, _ := strconv.ParseInt(in.pathValues[name], 10, 64)
	return v
}

func (in *sampleInput) text(name string) (string, bool) {
	switch v := in.query[name].(type) {
	case string:
		return v, true
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64), true
	case bool:
		return strconv.FormatBool(v), true
	}
	return "", false
}

func (in *sampleInput) str(name string) *string {
	if v, ok := in.text(name); ok {
		return &v
	}
	return nil
}

func (in *sampleInput) strValue(name string) string {
	v, _ := in.text(name)
	return v
}

func (in *sampleInput) int(name string) *int64 {
	if v, ok := in.text(name); ok {
		if i, err := strconv.ParseInt(v, 10, 64); err == nil {
			return &i
		}
	}
	return nil
}

func (in *sampleInput) intValue(name string) int64 { return Value(in.int(name)) }

func (in *sampleInput) float(name string) *float64 {
	if v, ok := in.text(name); ok {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			return &f
		}
	}
	return nil
}

func (in *sampleInput) floatValue(name string) float64 { return Value(in.float(name)) }

func (in *sampleInput) bool(name string) *bool {
	if v, ok := in.text(name); ok {
		if b, err := strconv.ParseBool(v); err == nil {
			return &b
		}
	}
	return nil
}

func (in *sampleInput) boolValue(name string) bool { return Value(in.bool(name)) }

func (in *sampleInput) list(name string) []string {
	if v, ok := in.text(name); ok {
		return strings.Split(v, ",")
	}
	return nil
}

func (in *sampleInput) decodeBody(p any) { _ = json.Unmarshal(in.body, p) }

// TestSampleInputValues covers the readers sample_gen_test.go calls only for the parameter
// types the spec has at the time.
func TestSampleInputValues(t *testing.T) {
	in := &sampleInput{query: map[string]any{"i": "7", "f": 1.5, "b": true, "s": "x,y"}}
	if in.intValue("i") != 7 || in.intValue("none") != 0 || in.floatValue("f") != 1.5 || in.floatValue("none") != 0 ||
		!in.boolValue("b") || in.boolValue("none") || in.strValue("s") != "x,y" || len(in.list("s")) != 2 || in.float("s") != nil {
		t.Error("a sample input was misread")
	}
}

var samplePhoto = []byte("\xff\xd8\xff\xe0 an invented photo")

// TestEveryOperation calls every method with every parameter set and checks what it sent (the
// method, the escaped path, each query parameter and each body field), that every field of the
// answer decoded, and that the method passes its RequestOptions on.
func TestEveryOperation(t *testing.T) {
	if len(sampleCalls) != len(sampleOps) {
		t.Fatalf("%d sample calls for %d operations", len(sampleCalls), len(sampleOps))
	}
	for i := range sampleOps {
		op := &sampleOps[i]
		t.Run(op.id, func(t *testing.T) {
			sample, ok := sampleCalls[op.id]
			if !ok {
				t.Fatal("no sample call")
			}
			status, body := sampleAnswer(op, sample)
			s := newServer(t, func(w http.ResponseWriter, _ *http.Request, _ int) {
				if op.kind == "binary" {
					w.Header().Set("Content-Type", "image/jpeg")
					_, _ = w.Write(samplePhoto)
					return
				}
				reply(w, status, body)
			})
			c, _ := partner(t, s)
			in := &sampleInput{pathValues: sample.path, body: json.RawMessage(sample.body)}
			if sample.query != "" {
				if err := json.Unmarshal([]byte(sample.query), &in.query); err != nil {
					t.Fatal(err)
				}
			}
			res, err := op.call(context.Background(), c, in)
			if op.kind == "binary" {
				if got, _ := res.([]byte); err != nil || !bytes.Equal(got, samplePhoto) {
					t.Fatalf("photo %q, %v", got, err)
				}
			} else if problems := checkAnswer(op, status, body, res, err); len(problems) > 0 {
				t.Fatal(strings.Join(problems, "; "))
			}
			reqs := s.requests()
			if len(reqs) != 1 {
				t.Fatalf("%d requests", len(reqs))
			}
			checkSent(t, op, sample, reqs[0])

			var meta ResponseMeta
			in.opts = []RequestOption{WithResponseInto(&meta), WithRequestHeader("X-Trace", "t1"), WithExtraQuery("extra", "1")}
			if _, err := op.call(context.Background(), c, in); err != nil {
				t.Fatalf("with options: %v", err)
			}
			sent := s.requests()[1]
			if meta.StatusCode != status || meta.Attempts != 1 || sent.Header.Get("X-Trace") != "t1" || sent.Query.Get("extra") != "1" {
				t.Errorf("with options: meta %+v, header %q, query %v", meta, sent.Header.Get("X-Trace"), sent.Query)
			}
		})
	}
}

func sampleAnswer(op *sampleOp, sample sampleCall) (int, string) {
	switch op.kind {
	case "empty":
		return http.StatusNoContent, ""
	case "binary":
		return http.StatusOK, ""
	case "page":
		return http.StatusOK, okPaged(sample.data, `{"limit":1,"offset":0,"total":1,"has_more":false}`)
	}
	return http.StatusOK, ok(sample.data)
}

func checkSent(t *testing.T, op *sampleOp, sample sampleCall, got recorded) {
	t.Helper()
	path := op.path
	for name, v := range sample.path {
		path = strings.Replace(path, "{"+name+"}", url.PathEscape(v), 1)
	}
	if got.Method != op.method || got.Path != path {
		t.Errorf("sent %s %s, want %s %s", got.Method, got.Path, op.method, path)
	}
	var query map[string]string
	if sample.query != "" {
		_ = json.Unmarshal([]byte(sample.query), &query)
	}
	for name, want := range query {
		if v := got.Query.Get(name); v != want {
			t.Errorf("query %s = %q, want %q", name, v, want)
		}
	}
	if len(got.Query) != len(query) {
		t.Errorf("sent the query %v, want %v", got.Query, query)
	}
	if sample.body == "" {
		return
	}
	var want, sent map[string]any
	_ = json.Unmarshal([]byte(sample.body), &want)
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
func checkAnswer(op *sampleOp, status int, body string, res any, err error) []string {
	if err != nil {
		return []string{fmt.Sprintf("unexpected %T: %v", err, err)}
	}
	var envelope struct {
		Data map[string]json.RawMessage `json:"data"`
	}
	_ = json.Unmarshal([]byte(body), &envelope)
	switch op.kind {
	case "empty":
		return nil
	case "get":
		m, ok := res.(Model)
		if !ok || len(m.Raw()) == 0 {
			return []string{"no model"}
		}
		return lossless(op.id, res, m.Raw())
	case "list", "page":
		items := reflect.ValueOf(res)
		if op.kind == "page" {
			items = items.Elem().FieldByName("Items")
		}
		var want []json.RawMessage
		_ = json.Unmarshal(envelope.Data[op.key], &want)
		if items.Len() != len(want) {
			return []string{fmt.Sprintf("%d items, want %d", items.Len(), len(want))}
		}
		var out []string
		for i := range want {
			out = append(out, lossless(fmt.Sprintf("%s[%d]", op.id, i), items.Index(i).Interface(), want[i])...)
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
		if ar, ok := result.Interface().(*ActionResult); ok {
			if _, unknown := ar.Value.(*InstructionResult); unknown {
				return []string{"the action " + ar.Action + " was not decoded into its result"}
			}
			return lossless(op.id, ar.Value, ar.Raw())
		}
		m := result.Interface().(Model)
		return lossless(op.id, result.Interface(), m.Raw())
	}
	return []string{"unknown kind " + op.kind}
}

// lossless reports the fields whose typed value differs from what the API sent: a field the SDK
// dropped because its value did not fit, or a required field the API left out.
func lossless(where string, typed any, raw json.RawMessage) []string {
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
