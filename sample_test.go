package cru

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"testing"
)

// sampleCall is one operation's invented inputs and answer, generated from the spec into
// sample_gen_test.go with every parameter and every field set.
type sampleCall struct {
	path  map[string]string
	query string // a JSON object of query parameters, each as the string sent
	body  string // a JSON object of body fields
	data  string // the answer's data
}

var samplePhoto = []byte("\xff\xd8\xff\xe0 an invented photo")

// TestEveryOperation calls every method with every parameter set and checks what it sent (the
// method, the escaped path, each query parameter and each body field) and that every field of
// the answer decoded.
func TestEveryOperation(t *testing.T) {
	if len(sampleCalls) != len(replayOps) {
		t.Fatalf("%d sample calls for %d operations", len(sampleCalls), len(replayOps))
	}
	for i := range replayOps {
		op := &replayOps[i]
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
			in := &replayInput{pathValues: sample.path, body: json.RawMessage(sample.body)}
			if sample.query != "" {
				if err := json.Unmarshal([]byte(sample.query), &in.query); err != nil {
					t.Fatal(err)
				}
			}
			var meta ResponseMeta
			res, err := op.call(context.Background(), c, in)
			if op.kind == "binary" {
				if got, _ := res.([]byte); err != nil || !bytes.Equal(got, samplePhoto) {
					t.Fatalf("photo %q, %v", got, err)
				}
			} else if problems := check(op, &capture{Status: status, Body: json.RawMessage(body)}, res, err); len(problems) > 0 {
				t.Fatal(strings.Join(problems, "; "))
			}
			reqs := s.requests()
			if len(reqs) != 1 {
				t.Fatalf("%d requests", len(reqs))
			}
			checkSent(t, op, sample, reqs[0])

			// Every method passes its options on.
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

// TestReplayInputValues covers the readers replay_gen_test.go calls only for the parameter
// types the spec has at the time.
func TestReplayInputValues(t *testing.T) {
	in := &replayInput{query: map[string]any{"i": "7", "f": 1.5, "b": true, "s": "x,y"}}
	if in.intValue("i") != 7 || in.intValue("none") != 0 || in.floatValue("f") != 1.5 || in.floatValue("none") != 0 ||
		!in.boolValue("b") || in.boolValue("none") || in.strValue("s") != "x,y" || len(in.list("s")) != 2 || in.float("s") != nil {
		t.Error("a replay input was misread")
	}
}

func sampleAnswer(op *replayOp, sample sampleCall) (int, string) {
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

func checkSent(t *testing.T, op *replayOp, sample sampleCall, got recorded) {
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
