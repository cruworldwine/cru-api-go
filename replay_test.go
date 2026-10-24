package cru

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
)

// The replay test serves real API answers, captured into $CRU_CAPTURES/captures/*.json, back to
// the SDK. The captures are read at run time only: they hold customer data and never belong in
// this repository.

type replayOp struct {
	id, method, path, kind, key string
	call                        func(ctx context.Context, c *Client, in *replayInput) (any, error)
}

type replayInput struct {
	pathValues map[string]string
	query      map[string]any
	body       json.RawMessage
	opts       []RequestOption
}

func (in *replayInput) path(name string) string { return in.pathValues[name] }

func (in *replayInput) pathInt(name string) int64 {
	v, _ := strconv.ParseInt(in.pathValues[name], 10, 64)
	return v
}

func (in *replayInput) text(name string) (string, bool) {
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

func (in *replayInput) str(name string) *string {
	if v, ok := in.text(name); ok {
		return &v
	}
	return nil
}

func (in *replayInput) strValue(name string) string {
	v, _ := in.text(name)
	return v
}

func (in *replayInput) int(name string) *int64 {
	if v, ok := in.text(name); ok {
		if i, err := strconv.ParseInt(v, 10, 64); err == nil {
			return &i
		}
	}
	return nil
}

func (in *replayInput) intValue(name string) int64 {
	if v := in.int(name); v != nil {
		return *v
	}
	return 0
}

func (in *replayInput) float(name string) *float64 {
	if v, ok := in.text(name); ok {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			return &f
		}
	}
	return nil
}

func (in *replayInput) floatValue(name string) float64 {
	if v := in.float(name); v != nil {
		return *v
	}
	return 0
}

func (in *replayInput) bool(name string) *bool {
	if v, ok := in.text(name); ok {
		if b, err := strconv.ParseBool(v); err == nil {
			return &b
		}
	}
	return nil
}

func (in *replayInput) boolValue(name string) bool {
	if v := in.bool(name); v != nil {
		return *v
	}
	return false
}

func (in *replayInput) list(name string) []string {
	if v, ok := in.text(name); ok {
		return strings.Split(v, ",")
	}
	return nil
}

// decodeBody fills the params from the captured body. A value of the wrong type, sent to test
// the API's validation, is left out: the captured answer is served all the same.
func (in *replayInput) decodeBody(p any) {
	_ = json.Unmarshal(in.body, p)
}

type capture struct {
	Request struct {
		Method string          `json:"method"`
		Path   string          `json:"path"`
		Query  json.RawMessage `json:"query"`
		Body   json.RawMessage `json:"body"`
		Grant  string          `json:"grant"`
	} `json:"request"`
	Status int             `json:"status"`
	Body   json.RawMessage `json:"body"`
}

func segments(path string) []string { return strings.Split(strings.Trim(path, "/"), "/") }

// matchOp finds the operation a captured request was made to. A literal segment beats a
// template one, so /warehouses/inbond is not taken for /warehouses/{code}.
func matchOp(method, path string) (*replayOp, map[string]string) {
	got := segments(path)
	var best *replayOp
	var bestValues map[string]string
	bestLiterals := -1
	for i := range replayOps {
		op := &replayOps[i]
		want := segments(op.path)
		if op.method != method || len(want) != len(got) {
			continue
		}
		values, literals, ok := map[string]string{}, 0, true
		for j, w := range want {
			if strings.HasPrefix(w, "{") {
				v, err := url.PathUnescape(got[j])
				if err != nil {
					v = got[j]
				}
				values[strings.Trim(w, "{}")] = v
			} else if w == got[j] {
				literals++
			} else {
				ok = false
				break
			}
		}
		if ok && literals > bestLiterals {
			best, bestValues, bestLiterals = op, values, literals
		}
	}
	return best, bestValues
}

func TestReplayCaptures(t *testing.T) {
	dir := os.Getenv("CRU_CAPTURES")
	if dir == "" {
		t.Skip("set CRU_CAPTURES to a run directory to replay its captures")
	}
	files, err := filepath.Glob(filepath.Join(dir, "captures", "*.json"))
	if err != nil || len(files) == 0 {
		t.Fatalf("no captures under %s", dir)
	}
	sort.Strings(files)

	var mu sync.Mutex
	var current *capture
	var gotMethod, gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		gotMethod, gotPath = r.Method, strings.TrimPrefix(r.URL.EscapedPath(), "/api/v1")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(current.Status)
		if body := strings.TrimSpace(string(current.Body)); body != "" && body != "null" {
			_, _ = w.Write(current.Body)
		}
	}))
	defer srv.Close()

	clients := map[string]*Client{}
	for grant, opt := range map[string]Option{"oauth": WithAccessToken("test-access-token"), "partner": WithClientKey("test-key", "test-secret")} {
		c, err := NewClient(opt, WithBaseURL(srv.URL+"/api/v1"), WithMaxRetries(0))
		if err != nil {
			t.Fatal(err)
		}
		clients[grant] = c
	}

	var typed, raw, passed int
	byOp := map[string]int{}
	for _, file := range files {
		b, err := os.ReadFile(file) //nolint:gosec // the captures the developer named in CRU_CAPTURES
		if err != nil {
			t.Fatal(err)
		}
		var cp capture
		if err := json.Unmarshal(b, &cp); err != nil {
			t.Errorf("%s: not a capture: %v", filepath.Base(file), err)
			continue
		}
		mu.Lock()
		current = &cp
		mu.Unlock()

		c := clients["partner"]
		if cp.Request.Grant == "oauth" {
			c = clients["oauth"]
		}
		in := &replayInput{body: cp.Request.Body}
		_ = json.Unmarshal(cp.Request.Query, &in.query) // [] when there is none
		op, values := matchOp(cp.Request.Method, cp.Request.Path)
		in.pathValues = values

		var problems []string
		if op == nil {
			raw++
			problems = replayRaw(c, &cp, in)
		} else {
			typed++
			byOp[op.id]++
			res, err := op.call(context.Background(), c, in)
			problems = check(op, &cp, res, err)
			mu.Lock()
			if gotMethod != cp.Request.Method || !samePath(gotPath, cp.Request.Path) {
				problems = append(problems, fmt.Sprintf("sent %s to a different path than the capture's", gotMethod))
			}
			mu.Unlock()
		}
		if len(problems) > 0 {
			name := "(no operation)"
			if op != nil {
				name = op.id
			}
			t.Errorf("%s %s HTTP %d: %s", filepath.Base(file), name, cp.Status, strings.Join(problems, "; "))
			continue
		}
		passed++
	}
	t.Logf("replayed %d captures: %d through typed methods (%d operations), %d through Client.Do; %d passed",
		len(files), typed, len(byOp), raw, passed)
}

func samePath(got, want string) bool {
	g, err1 := url.PathUnescape(got)
	w, err2 := url.PathUnescape(want)
	return err1 == nil && err2 == nil && g == w
}

func wantCode(cp *capture) string {
	var body struct {
		Errors []struct {
			Code string `json:"code"`
		} `json:"errors"`
	}
	_ = json.Unmarshal(cp.Body, &body)
	if len(body.Errors) == 0 {
		return ""
	}
	return body.Errors[0].Code
}

func checkError(cp *capture, err error) []string {
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		return []string{fmt.Sprintf("want an *APIError, got %T", err)}
	}
	var out []string
	if apiErr.StatusCode != cp.Status {
		out = append(out, fmt.Sprintf("status %d, want %d", apiErr.StatusCode, cp.Status))
	}
	if want := wantCode(cp); apiErr.Code != want {
		out = append(out, fmt.Sprintf("code %q, want %q", apiErr.Code, want))
	}
	if apiErr.RequestID == "" {
		out = append(out, "no request id")
	}
	return out
}

func replayRaw(c *Client, cp *capture, in *replayInput) []string {
	query := url.Values{}
	for k := range in.query {
		query.Set(k, in.strValue(k))
	}
	resp, err := c.Do(context.Background(), cp.Request.Method, cp.Request.Path, query, nil)
	if cp.Status >= 400 {
		return checkError(cp, err)
	}
	if err != nil {
		return []string{fmt.Sprintf("unexpected %T", err)}
	}
	if resp.StatusCode != cp.Status {
		return []string{fmt.Sprintf("status %d", resp.StatusCode)}
	}
	return nil
}

func check(op *replayOp, cp *capture, res any, err error) []string {
	if cp.Status >= 400 {
		return checkError(cp, err)
	}
	if err != nil {
		var apiErr *APIError
		if errors.As(err, &apiErr) {
			return []string{fmt.Sprintf("unexpected APIError %d %s", apiErr.StatusCode, apiErr.Code)}
		}
		return []string{fmt.Sprintf("unexpected %T", err)}
	}
	var envelope struct {
		Data map[string]json.RawMessage `json:"data"`
	}
	_ = json.Unmarshal(cp.Body, &envelope)
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
		if cp.Status == http.StatusAccepted {
			conf, _ := w.FieldByName("Confirmation").Interface().(*StagedConfirmation)
			if !w.FieldByName("Staged").Bool() || conf == nil || conf.Code == "" || conf.Preview.Summary == "" {
				return []string{"not staged with a confirmation"}
			}
			return lossless(op.id, conf, conf.Raw())
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
	case "binary":
		if b, ok := res.([]byte); !ok || len(b) == 0 {
			return []string{"no bytes"}
		}
		return nil
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
