package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

// edgeSpec is the smallest document that compiles successfully.
const edgeSpec = `{"openapi":"3.1.0","paths":{"/ping":{"get":{"operationId":"ping","responses":{"200":{"description":"ok"}}}}}}`

type edgeRoundTrip func(*http.Request) (*http.Response, error)

func (f edgeRoundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type edgeFailReader struct{ err error }

func (r edgeFailReader) Read([]byte) (int, error) { return 0, r.err }

// edgeFixture loads an arbitrary OpenAPI document and forwards every non-spec
// request to handler.
func edgeFixture(t *testing.T, doc string, handler http.HandlerFunc) (*Controller, *atomic.Int32) {
	t.Helper()
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/spec" {
			io.WriteString(w, doc)
			return
		}
		calls.Add(1)
		handler(w, r)
	}))
	t.Cleanup(srv.Close)
	c, err := New(context.Background(), Config{Services: []Service{{
		ID: "s", OpenAPIURL: srv.URL + "/spec", BaseURL: srv.URL + "/v1", TimeoutMS: 500,
	}}})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return c, &calls
}

const edgeOperations = `{
  "openapi":"3.1.0",
  "paths":{
    "/items/{id}":{
      "post":{
        "operationId":"update",
        "parameters":[
          {"name":"id","in":"path","required":true,"schema":{"type":"string"}},
          {"name":"opt","in":"query","schema":{"type":"string"}},
          {"name":"X-Trace","in":"header","schema":{"type":"string"}}
        ],
        "requestBody":{"required":true,"content":{"application/json":{"schema":{"type":"object","required":["n"],"additionalProperties":false,"properties":{"n":{"type":"integer"}}}}}},
        "responses":{"200":{"description":"ok"}}
      }
    },
    "/no-body":{
      "post":{"operationId":"nobody","responses":{"200":{"description":"ok"}}}
    },
    "/head-only":{
      "head":{"operationId":"headOnly","responses":{"200":{"description":"ok"}}}
    }
  }
}`

func edgeArgs() Arguments {
	return Arguments{ServiceID: "s", OperationID: "update", Parameters: map[string]any{"id": "abc"}, Body: json.RawMessage(`{"n":1}`)}
}

func edgeWantCode(t *testing.T, err error, code string) {
	t.Helper()
	var e *Error
	if !errors.As(err, &e) {
		t.Fatalf("want *Error %s, got %T %v", code, err, err)
	}
	if e.Code != code {
		t.Fatalf("want code %s, got %s (%s)", code, e.Code, e.Message)
	}
}

func TestEdgeErrorAndHelpers(t *testing.T) {
	if got := (&Error{Code: "X", Message: "boom"}).Error(); got != "boom" {
		t.Fatalf("Error() = %q", got)
	}
	empty := &Controller{entries: map[string]entry{}}
	if ids := empty.IDs(); len(ids) != 0 {
		t.Fatalf("IDs = %+v", ids)
	}
	c := &Controller{entries: map[string]entry{"b": {}, "a": {}}}
	if ids := c.IDs(); len(ids) != 2 || ids[0] != "a" || ids[1] != "b" {
		t.Fatalf("IDs = %+v", ids)
	}
	if _, err := c.Document("missing"); err == nil {
		t.Fatal("unknown service should fail")
	} else {
		edgeWantCode(t, err, "SERVICE_NOT_FOUND")
	}

	if _, err := bounded(edgeFailReader{err: errors.New("broken pipe")}); err == nil || err.Error() != "broken pipe" {
		t.Fatalf("bounded read error = %v", err)
	}
	if _, err := bounded(strings.NewReader(strings.Repeat("x", Limit+1))); err == nil {
		t.Fatal("oversize should fail")
	} else {
		edgeWantCode(t, err, "RESPONSE_TOO_LARGE")
	}
	if b, err := bounded(strings.NewReader(strings.Repeat("x", Limit))); err != nil || len(b) != Limit {
		t.Fatalf("exact limit = %d %v", len(b), err)
	}

	for _, value := range []string{"a\\b", "a?b", "a#b", "a\rb", "a\nb", "//double", "./x", "a/./b", "a/..", "..", "."} {
		if safePath(value) {
			t.Errorf("safePath(%q) should be false", value)
		}
	}
	for _, value := range []string{"", "/", "/api/x", "/a/b.c", "/a b", "/{id}"} {
		if !safePath(value) {
			t.Errorf("safePath(%q) should be true", value)
		}
	}

	for _, name := range []string{"Proxy-Authorization", "SEC-Fetch-Site", "Authorization", "Host", "Connection", "Content-Length", "Transfer-Encoding", "Cookie", "Set-Cookie", "Content-Type", "Accept", "Upgrade", "TE", "Trailer", "PROXY-X"} {
		if !blockedHeader(name) {
			t.Errorf("blockedHeader(%q) should be true", name)
		}
	}
	for _, name := range []string{"X-Trace", "Accept-Language", "User-Agent"} {
		if blockedHeader(name) {
			t.Errorf("blockedHeader(%q) should be false", name)
		}
	}
}

func TestEdgeNewConfigValidation(t *testing.T) {
	spec := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, edgeSpec) }))
	defer spec.Close()
	valid := Service{ID: "s", OpenAPIURL: spec.URL + "/spec", BaseURL: spec.URL + "/v1", TimeoutMS: 1000}

	cases := []struct {
		name    string
		config  Config
		message string
	}{
		{"no services", Config{}, "No services configured"},
		{"missing id", Config{Services: []Service{{OpenAPIURL: spec.URL + "/spec", BaseURL: spec.URL}}}, "Missing service_id"},
		{"duplicate id", Config{Services: []Service{valid, valid}}, "Duplicate service_id"},
		{"invalid openapi url", Config{Services: []Service{{ID: "s", OpenAPIURL: "ftp://host/spec", BaseURL: "http://host"}}}, "Invalid service URL"},
		{"userinfo url", Config{Services: []Service{{ID: "s", OpenAPIURL: "http://user:pw@host/spec", BaseURL: "http://host"}}}, "Invalid service URL"},
		{"fragment url", Config{Services: []Service{{ID: "s", OpenAPIURL: "http://host/spec#frag", BaseURL: "http://host"}}}, "Invalid service URL"},
		{"relative url", Config{Services: []Service{{ID: "s", OpenAPIURL: "/spec", BaseURL: "http://host"}}}, "Invalid service URL"},
		{"base query", Config{Services: []Service{{ID: "s", OpenAPIURL: spec.URL + "/spec", BaseURL: spec.URL + "?q=1"}}}, "Invalid base path"},
		{"base escaped", Config{Services: []Service{{ID: "s", OpenAPIURL: spec.URL + "/spec", BaseURL: spec.URL + "/a%2Fb"}}}, "Invalid base path"},
		{"base traversal", Config{Services: []Service{{ID: "s", OpenAPIURL: spec.URL + "/spec", BaseURL: spec.URL + "/a/../b"}}}, "Invalid base path"},
		{"negative timeout", Config{Services: []Service{{ID: "s", OpenAPIURL: spec.URL + "/spec", BaseURL: spec.URL, TimeoutMS: -1}}}, "Timeout must be 1..120000 ms"},
		{"excessive timeout", Config{Services: []Service{{ID: "s", OpenAPIURL: spec.URL + "/spec", BaseURL: spec.URL, TimeoutMS: 120001}}}, "Timeout must be 1..120000 ms"},
		{"load failure", Config{Services: []Service{{ID: "s", OpenAPIURL: "http://127.0.0.1:1/spec", BaseURL: "http://127.0.0.1", TimeoutMS: 50}}}, "service s"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := New(context.Background(), tc.config)
			if err == nil || !strings.Contains(err.Error(), tc.message) {
				t.Fatalf("err = %v, want %q", err, tc.message)
			}
		})
	}

	controller, err := New(context.Background(), Config{Services: []Service{valid}})
	if err != nil {
		t.Fatal(err)
	}
	if ids := controller.IDs(); len(ids) != 1 || ids[0] != "s" {
		t.Fatalf("IDs = %+v", ids)
	}
	if _, err := controller.Document("s"); err != nil {
		t.Fatal(err)
	}

	// A zero timeout takes the documented default instead of failing.
	defaulted := valid
	defaulted.TimeoutMS = 0
	if _, err := New(context.Background(), Config{Services: []Service{defaulted}}); err != nil {
		t.Fatalf("zero timeout: %v", err)
	}
}

func TestEdgeLoadFailures(t *testing.T) {
	client := &http.Client{}
	c := &Controller{client: client}

	// NewRequest failure: a URL that net/http refuses to parse.
	if _, err := c.load(context.Background(), Service{OpenAPIURL: "ht\ntp://bad", TimeoutMS: 100}); err == nil {
		t.Fatal("expected request construction failure")
	}

	closed := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	closedURL := closed.URL
	closed.Close()
	if _, err := c.load(context.Background(), Service{OpenAPIURL: closedURL, TimeoutMS: 100}); err == nil {
		t.Fatal("expected transport failure")
	} else {
		edgeWantCode(t, err, "OPENAPI_UNAVAILABLE")
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/status":
			w.WriteHeader(http.StatusServiceUnavailable)
		case "/oversize":
			io.WriteString(w, strings.Repeat("x", Limit+1))
		default:
			io.WriteString(w, edgeSpec)
		}
	}))
	defer srv.Close()

	if _, err := c.load(context.Background(), Service{OpenAPIURL: srv.URL + "/status", TimeoutMS: 500}); err == nil {
		t.Fatal("non-200 should fail")
	} else {
		edgeWantCode(t, err, "OPENAPI_UNAVAILABLE")
	}
	if _, err := c.load(context.Background(), Service{OpenAPIURL: srv.URL + "/oversize", TimeoutMS: 500}); err == nil {
		t.Fatal("oversize should fail")
	} else {
		edgeWantCode(t, err, "RESPONSE_TOO_LARGE")
	}
	if _, err := c.load(context.Background(), Service{OpenAPIURL: srv.URL + "/ok", TimeoutMS: 500}); err != nil {
		t.Fatalf("load: %v", err)
	}
}

func TestEdgeCompileDocumentRejects(t *testing.T) {
	cases := []struct{ name, doc, code string }{
		{"invalid yaml", "openapi: [", "INVALID_OPENAPI"},
		{"non json compatible", `{"openapi":"3.1.0","paths":{"/x":{"get":{"operationId":"x"}}},"extra":.nan}`, "INVALID_OPENAPI"},
		{"wrong version", `{"openapi":"2.0.0","paths":{}}`, "INVALID_OPENAPI"},
		{"missing paths", `{"openapi":"3.1.0"}`, "INVALID_OPENAPI"},
		{"path without slash", `{"openapi":"3.1.0","paths":{"api/x":{"get":{"operationId":"x"}}}}`, "INVALID_OPENAPI"},
		{"path traversal", `{"openapi":"3.1.0","paths":{"/a/../b":{"get":{"operationId":"x"}}}}`, "INVALID_OPENAPI"},
		{"escaped path", `{"openapi":"3.1.0","paths":{"/a%20b":{"get":{"operationId":"x"}}}}`, "INVALID_OPENAPI"},
		{"external ref", `{"openapi":"3.1.0","paths":{"/x":{"$ref":"https://evil.example/schema"}}}`, "UNSUPPORTED_OPENAPI"},
		{"path not object", `{"openapi":"3.1.0","paths":{"/x":5}}`, "INVALID_OPENAPI"},
		{"operation not object", `{"openapi":"3.1.0","paths":{"/x":{"get":5}}}`, "INVALID_OPENAPI"},
		{"missing operation id", `{"openapi":"3.1.0","paths":{"/x":{"get":{}}}}`, "INVALID_OPENAPI"},
		{"duplicate operation id", `{"openapi":"3.1.0","paths":{"/a":{"get":{"operationId":"same"}},"/b":{"post":{"operationId":"same"}}}}`, "INVALID_OPENAPI"},
		{"parameters not array", `{"openapi":"3.1.0","paths":{"/x":{"get":{"operationId":"x","parameters":"nope"}}}}`, "INVALID_OPENAPI"},
		{"parameter missing name", `{"openapi":"3.1.0","paths":{"/x":{"get":{"operationId":"x","parameters":[{"in":"query","schema":{"type":"string"}}]}}}}`, "INVALID_OPENAPI"},
		{"ambiguous parameter", `{"openapi":"3.1.0","paths":{"/x/{id}":{"get":{"operationId":"x","parameters":[{"name":"id","in":"query","schema":{"type":"string"}},{"name":"id","in":"path","schema":{"type":"string"}}]}}}}`, "UNSUPPORTED_OPENAPI"},
		{"non scalar parameter", `{"openapi":"3.1.0","paths":{"/x":{"get":{"operationId":"x","parameters":[{"name":"q","in":"query","schema":{"type":"object"}}]}}}}`, "UNSUPPORTED_OPENAPI"},
		{"reserved header", `{"openapi":"3.1.0","paths":{"/x":{"get":{"operationId":"x","parameters":[{"name":"Authorization","in":"header","schema":{"type":"string"}}]}}}}`, "UNSUPPORTED_OPENAPI"},
		{"duplicate header", `{"openapi":"3.1.0","paths":{"/x":{"get":{"operationId":"x","parameters":[{"name":"X-A","in":"header","schema":{"type":"string"}},{"name":"x-a","in":"header","schema":{"type":"string"}}]}}}}`, "UNSUPPORTED_OPENAPI"},
		{"unsupported location", `{"openapi":"3.1.0","paths":{"/x":{"get":{"operationId":"x","parameters":[{"name":"c","in":"cookie","schema":{"type":"string"}}]}}}}`, "UNSUPPORTED_OPENAPI"},
		{"style mismatch", `{"openapi":"3.1.0","paths":{"/x":{"get":{"operationId":"x","parameters":[{"name":"q","in":"query","style":"deepObject","schema":{"type":"string"}}]}}}}`, "UNSUPPORTED_OPENAPI"},
		{"allow reserved", `{"openapi":"3.1.0","paths":{"/x":{"get":{"operationId":"x","parameters":[{"name":"q","in":"query","allowReserved":true,"schema":{"type":"string"}}]}}}}`, "UNSUPPORTED_OPENAPI"},
		{"parameter schema error", `{"openapi":"3.1.0","paths":{"/x":{"get":{"operationId":"x","parameters":[{"name":"q","in":"query","schema":{"type":"string","$id":"x"}}]}}}}`, "UNSUPPORTED_OPENAPI"},
		{"undeclared path parameter", `{"openapi":"3.1.0","paths":{"/x/{id}":{"get":{"operationId":"x"}}}}`, "INVALID_OPENAPI"},
		{"malformed template", `{"openapi":"3.1.0","paths":{"/x/{id":{"get":{"operationId":"x"}}}}`, "INVALID_OPENAPI"},
		{"empty template", `{"openapi":"3.1.0","paths":{"/x/{}":{"get":{"operationId":"x"}}}}`, "INVALID_OPENAPI"},
		{"body not json", `{"openapi":"3.1.0","paths":{"/x":{"post":{"operationId":"x","requestBody":{}}}}}`, "UNSUPPORTED_OPENAPI"},
		{"get body", `{"openapi":"3.1.0","paths":{"/x":{"get":{"operationId":"x","requestBody":{"content":{"application/json":{"schema":{"type":"object"}}}}}}}}`, "UNSUPPORTED_OPENAPI"},
		{"body schema error", `{"openapi":"3.1.0","paths":{"/x":{"post":{"operationId":"x","requestBody":{"content":{"application/json":{"schema":{"type":"object","$id":"x"}}}}}}}}`, "UNSUPPORTED_OPENAPI"},
		{"legacy exclusive without bound", `{"openapi":"3.0.3","paths":{"/x":{"get":{"operationId":"x","parameters":[{"name":"q","in":"query","schema":{"type":"number","exclusiveMinimum":true}}]}}}}`, "INVALID_OPENAPI"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := compileDocument(Service{ID: "s", BaseURL: "http://127.0.0.1"}, []byte(tc.doc))
			if err == nil {
				t.Fatal("expected an error")
			}
			edgeWantCode(t, err, tc.code)
		})
	}
}

func TestEdgeCompileDocumentAcceptsIgnoredMethods(t *testing.T) {
	doc := `{"openapi":"3.1.0","paths":{"/x":{"trace":{"operationId":"t"},"get":{"operationId":"g"}}}}`
	e, err := compileDocument(Service{ID: "s", BaseURL: "http://127.0.0.1"}, []byte(doc))
	if err != nil {
		t.Fatalf("compileDocument: %v", err)
	}
	if _, ok := e.operations["g"]; !ok {
		t.Fatalf("operations = %+v", e.operations)
	}
	if _, ok := e.operations["t"]; ok {
		t.Fatal("trace must be ignored")
	}
}

func TestEdgeResolveLimits(t *testing.T) {
	if _, err := (noRemote{}).Load("https://x"); err == nil {
		t.Fatal("external load must be disabled")
	}

	var deep any = map[string]any{"leaf": true}
	for i := 0; i < 82; i++ {
		deep = map[string]any{"n": deep}
	}
	if _, err := resolve(deep, map[string]any{}, nil, 0); err == nil {
		t.Fatal("deep nesting should fail")
	}

	root := map[string]any{"ok": map[string]any{"type": "object"}}
	if _, err := resolve([]any{map[string]any{"$ref": "https://evil"}}, root, nil, 0); err == nil {
		t.Fatal("array ref should fail")
	}
	if _, err := resolve(map[string]any{"a": map[string]any{"$ref": "https://evil"}}, root, nil, 0); err == nil {
		t.Fatal("nested ref should fail")
	}
	resolved, err := resolve(map[string]any{"$ref": "#/ok", "title": "sibling"}, root, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	m, _ := resolved.(map[string]any)
	if m["title"] != "sibling" || m["type"] != "object" {
		t.Fatalf("resolved = %+v", m)
	}
	// Escaped JSON pointer segments are decoded.
	escaped := map[string]any{"a/b": map[string]any{"type": "string"}, "til~de": map[string]any{"type": "number"}}
	first, err := resolve(map[string]any{"$ref": "#/a~1b"}, escaped, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	if first.(map[string]any)["type"] != "string" {
		t.Fatalf("escaped slash: %+v", first)
	}
	second, err := resolve(map[string]any{"$ref": "#/til~0de"}, escaped, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	if second.(map[string]any)["type"] != "number" {
		t.Fatalf("escaped tilde: %+v", second)
	}
}

func TestEdgeCompileSchemaAndNormalize(t *testing.T) {
	for _, value := range []any{nil, "str", 5, []any{}} {
		if _, err := normalizeSchema(value, false); err == nil {
			t.Errorf("normalizeSchema(%v) should fail", value)
		}
	}
	if _, err := normalizeSchema(true, false); err != nil {
		t.Fatalf("boolean schema: %v", err)
	}
	for _, keyword := range []string{"$id", "$schema", "$dynamicRef", "$dynamicAnchor"} {
		if _, err := normalizeSchema(map[string]any{keyword: "x"}, false); err == nil {
			t.Errorf("%s should be rejected", keyword)
		}
	}
	if _, err := normalizeSchema(map[string]any{"properties": 5}, false); err == nil {
		t.Fatal("properties must be a map")
	}
	if _, err := normalizeSchema(map[string]any{"properties": map[string]any{"a": map[string]any{"$id": "x"}}}, false); err == nil {
		t.Fatal("nested property error must propagate")
	}
	if _, err := normalizeSchema(map[string]any{"items": map[string]any{"$id": "x"}}, false); err == nil {
		t.Fatal("items error must propagate")
	}
	if _, err := normalizeSchema(map[string]any{"allOf": "x"}, false); err == nil {
		t.Fatal("allOf must be an array")
	}
	if _, err := normalizeSchema(map[string]any{"allOf": []any{map[string]any{"$id": "x"}}}, false); err == nil {
		t.Fatal("allOf child error must propagate")
	}
	if normalized, err := normalizeSchema(map[string]any{"allOf": []any{map[string]any{"type": "string"}}}, false); err != nil {
		t.Fatalf("allOf: %v", err)
	} else if list, ok := normalized.(map[string]any)["allOf"].([]any); !ok || len(list) != 1 {
		t.Fatalf("allOf = %+v", normalized)
	}
	if _, err := normalizeSchema(map[string]any{"unknownKeyword": true}, false); err == nil {
		t.Fatal("unknown keyword must be rejected")
	}
	if _, err := normalizeSchema(map[string]any{"x-vendor": true}, false); err != nil {
		t.Fatalf("extension keyword: %v", err)
	}

	legacy, err := normalizeSchema(map[string]any{"type": "string", "nullable": true, "exclusiveMinimum": false, "minimum": 3}, true)
	if err != nil {
		t.Fatal(err)
	}
	legacyMap := legacy.(map[string]any)
	if _, exists := legacyMap["nullable"]; exists {
		t.Fatal("nullable should be removed")
	}
	types, ok := legacyMap["type"].([]any)
	if !ok || len(types) != 2 || types[0] != "string" || types[1] != "null" {
		t.Fatalf("legacy type = %+v", legacyMap["type"])
	}
	if _, exists := legacyMap["exclusiveMinimum"]; exists {
		t.Fatal("false exclusiveMinimum should be removed")
	}

	if _, err := compileSchema(map[string]any{"type": "string", "title": func() {}}, "3.1.0"); err == nil {
		t.Fatal("non JSON value should fail resource registration")
	}
	if _, err := compileSchema(map[string]any{"type": "object", "required": "name"}, "3.1.0"); err == nil || !strings.Contains(err.Error(), "compilation") {
		t.Fatalf("invalid schema should fail compilation: %v", err)
	}
	if _, err := compileSchema(map[string]any{"type": "string"}, "3.0.3"); err != nil {
		t.Fatalf("legacy draft compile: %v", err)
	}
}

func TestEdgeSendParameterBoundaries(t *testing.T) {
	// A 204 response has no body; the message falls back to the HTTP status.
	c, calls := edgeFixture(t, edgeOperations, func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("X-Trace"); got != "" {
			t.Errorf("unexpected header %q", got)
		}
		if got := r.URL.Query().Get("opt"); got != "" {
			t.Errorf("unexpected query %q", got)
		}
		w.WriteHeader(http.StatusNoContent)
	})
	result, err := c.Send(context.Background(), edgeArgs())
	if err != nil || !result.OK || result.Status != 204 || result.Message != "HTTP 204" || result.Data != nil {
		t.Fatalf("result = %+v, err = %v", result, err)
	}
	if calls.Load() != 1 {
		t.Fatalf("calls = %d", calls.Load())
	}

	invalid := []struct {
		name   string
		params map[string]any
		body   json.RawMessage
	}{
		{"nil optional", map[string]any{"id": "x", "opt": nil}, json.RawMessage(`{"n":1}`)},
		{"wrong optional type", map[string]any{"id": "x", "opt": 5}, json.RawMessage(`{"n":1}`)},
		{"dot path", map[string]any{"id": "."}, json.RawMessage(`{"n":1}`)},
		{"dotdot path", map[string]any{"id": ".."}, json.RawMessage(`{"n":1}`)},
		{"slash path", map[string]any{"id": "a/b"}, json.RawMessage(`{"n":1}`)},
		{"backslash path", map[string]any{"id": `a\b`}, json.RawMessage(`{"n":1}`)},
		{"percent path", map[string]any{"id": "a%b"}, json.RawMessage(`{"n":1}`)},
		{"crlf path", map[string]any{"id": "a\r\nb"}, json.RawMessage(`{"n":1}`)},
		{"crlf header", map[string]any{"id": "x", "X-Trace": "a\r\nInjected: 1"}, json.RawMessage(`{"n":1}`)},
	}
	for _, tc := range invalid {
		t.Run(tc.name, func(t *testing.T) {
			_, err := c.Send(context.Background(), Arguments{ServiceID: "s", OperationID: "update", Parameters: tc.params, Body: tc.body})
			edgeWantCode(t, err, "INVALID_PARAMETERS")
		})
	}
	// Oversize bodies are reported before schema validation.
	_, err = c.Send(context.Background(), Arguments{ServiceID: "s", OperationID: "update", Parameters: map[string]any{"id": "x"}, Body: json.RawMessage(strings.Repeat("x", Limit+1))})
	edgeWantCode(t, err, "REQUEST_TOO_LARGE")

	// A body on an operation that declares none is rejected.
	_, err = c.Send(context.Background(), Arguments{ServiceID: "s", OperationID: "nobody", Body: json.RawMessage(`{}`)})
	edgeWantCode(t, err, "INVALID_REQUEST_BODY")

	// Malformed and schema-mismatching bodies are rejected.
	_, err = c.Send(context.Background(), Arguments{ServiceID: "s", OperationID: "update", Parameters: map[string]any{"id": "x"}, Body: json.RawMessage(`{`)})
	edgeWantCode(t, err, "INVALID_REQUEST_BODY")
	_, err = c.Send(context.Background(), Arguments{ServiceID: "s", OperationID: "update", Parameters: map[string]any{"id": "x"}, Body: json.RawMessage(`{"n":1,"extra":true}`)})
	edgeWantCode(t, err, "INVALID_REQUEST_BODY")

	// A valid optional query parameter and an injected header both arrive.
	c2, calls2 := edgeFixture(t, edgeOperations, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("opt") != "yes" {
			t.Errorf("query = %q", r.URL.RawQuery)
		}
		if r.Header.Get("X-Trace") != "trace-1" {
			t.Errorf("header = %q", r.Header.Get("X-Trace"))
		}
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"user_message":"ok"}`)
	})
	args := edgeArgs()
	args.Parameters["opt"] = "yes"
	args.Parameters["X-Trace"] = "trace-1"
	if _, err := c2.Send(context.Background(), args); err != nil {
		t.Fatal(err)
	}
	if calls2.Load() != 1 {
		t.Fatalf("calls = %d", calls2.Load())
	}
}

func TestEdgeSendResponseBoundaries(t *testing.T) {
	modes := []struct {
		name, contentType, body, code string
		status                        int
	}{
		{"problem json", "application/problem+json", `{"message":"problem"}`, "", 200},
		{"missing content type", "", `{}`, "INVALID_RESPONSE", 200},
		{"malformed json", "application/json", `{`, "INVALID_RESPONSE", 200},
		{"json suffix only", "application/jsonx", `{}`, "INVALID_RESPONSE", 200},
		{"server error", "application/json", `{"message":"nope","ok":false}`, "", 500},
	}
	for _, mode := range modes {
		t.Run(mode.name, func(t *testing.T) {
			c, _ := edgeFixture(t, edgeOperations, func(w http.ResponseWriter, r *http.Request) {
				if mode.contentType != "" {
					w.Header().Set("Content-Type", mode.contentType)
				}
				w.WriteHeader(mode.status)
				io.WriteString(w, mode.body)
			})
			result, err := c.Send(context.Background(), edgeArgs())
			if mode.code != "" {
				edgeWantCode(t, err, mode.code)
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if mode.status >= 300 {
				if result.OK || result.Error == nil || result.Error.Code != "APP_ERROR" || result.Error.Message != "nope" {
					t.Fatalf("result = %+v", result)
				}
			} else if !result.OK || result.Message != "problem" {
				t.Fatalf("result = %+v", result)
			}
		})
	}

	// HEAD responses have no body and must not require a JSON content type.
	headC, _ := edgeFixture(t, edgeOperations, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodHead {
			t.Errorf("method = %s", r.Method)
		}
		w.WriteHeader(http.StatusOK)
	})
	head, err := headC.Send(context.Background(), Arguments{ServiceID: "s", OperationID: "headOnly"})
	if err != nil || !head.OK || head.Message != "HTTP 200" {
		t.Fatalf("head = %+v, %v", head, err)
	}

	// A body that fails while being read surfaces as an upstream error.
	readErrC, _ := edgeFixture(t, edgeOperations, func(w http.ResponseWriter, r *http.Request) {})
	readErrC.SetHTTPClient(&http.Client{Transport: edgeRoundTrip(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(edgeFailReader{err: errors.New("connection reset")}), Request: r}, nil
	})})
	_, err = readErrC.Send(context.Background(), edgeArgs())
	edgeWantCode(t, err, "UPSTREAM_ERROR")

	// Transport failures that are not deadlines are upstream errors too.
	boomC, _ := edgeFixture(t, edgeOperations, func(w http.ResponseWriter, r *http.Request) {})
	boomC.SetHTTPClient(&http.Client{Transport: edgeRoundTrip(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("connection refused")
	})})
	_, err = boomC.Send(context.Background(), edgeArgs())
	edgeWantCode(t, err, "UPSTREAM_ERROR")
}

func TestEdgeSendTargetAndRequestValidation(t *testing.T) {
	client := &http.Client{}
	// A path that re-escapes differently after parsing is rejected.
	spacePath := &Controller{entries: map[string]entry{"s": {
		config:     Service{ID: "s", BaseURL: "http://127.0.0.1:1", TimeoutMS: 500},
		operations: map[string]operation{"op": {method: "GET", path: "/a b", params: map[string]parameter{}}},
	}}, client: client}
	_, err := spacePath.Send(context.Background(), Arguments{ServiceID: "s", OperationID: "op"})
	edgeWantCode(t, err, "TARGET_NOT_ALLOWED")

	// A method that net/http refuses cannot be turned into a request.
	badMethod := &Controller{entries: map[string]entry{"s": {
		config:     Service{ID: "s", BaseURL: "http://127.0.0.1:1", TimeoutMS: 500},
		operations: map[string]operation{"op": {method: "BAD METHOD", path: "/x", params: map[string]parameter{}}},
	}}, client: client}
	_, err = badMethod.Send(context.Background(), Arguments{ServiceID: "s", OperationID: "op"})
	edgeWantCode(t, err, "INVALID_PARAMETERS")
}

func TestEdgeFromDocument(t *testing.T) {
	bases := []string{
		"://bad", "http://", "ftp://example.com", "http://user:pw@example.com",
		"http://example.com/?q=1", "http://example.com/#frag", "http://example.com/a/../b",
		"http://example.com/a%2Fb",
	}
	for _, base := range bases {
		if _, err := FromDocument("app", base, []byte(edgeSpec)); err == nil {
			t.Errorf("FromDocument(%q) should fail", base)
		} else {
			edgeWantCode(t, err, "INVALID_CONFIG")
		}
	}
	if _, err := FromDocument("app", "http://example.com", []byte("openapi: [")); err == nil {
		t.Fatal("invalid document should fail")
	}

	c, err := FromDocument("app", "http://example.com/v1", []byte(edgeSpec))
	if err != nil {
		t.Fatal(err)
	}
	if ids := c.IDs(); len(ids) != 1 || ids[0] != "app" {
		t.Fatalf("IDs = %+v", ids)
	}
	if _, err := c.Document("app"); err != nil {
		t.Fatal(err)
	}

	var used atomic.Bool
	c.SetHTTPClient(&http.Client{Transport: edgeRoundTrip(func(r *http.Request) (*http.Response, error) {
		used.Store(true)
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"message":"ok"}`)), Request: r}, nil
	})})
	out, err := c.Send(context.Background(), Arguments{ServiceID: "app", OperationID: "ping"})
	if err != nil || !out.OK || !used.Load() {
		t.Fatalf("result = %+v, err = %v, used = %v", out, err, used.Load())
	}

	// A nil client is ignored; the installed client stays in place.
	c.SetHTTPClient(nil)
	if _, err := c.Send(context.Background(), Arguments{ServiceID: "app", OperationID: "ping"}); err != nil {
		t.Fatal("the previously installed client should still be in place")
	}
}

func TestEdgeFromDocumentRejectsRedirect(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Location", "http://169.254.169.254/latest/meta-data")
		w.WriteHeader(http.StatusFound)
	}))
	defer srv.Close()
	c, err := FromDocument("app", srv.URL, []byte(edgeSpec))
	if err != nil {
		t.Fatal(err)
	}
	_, err = c.Send(context.Background(), Arguments{ServiceID: "app", OperationID: "ping"})
	edgeWantCode(t, err, "TARGET_NOT_ALLOWED")
}

func TestEdgeSetHTTPClientDisablesRedirects(t *testing.T) {
	c, _ := edgeFixture(t, edgeOperations, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Location", "/elsewhere")
		w.WriteHeader(http.StatusFound)
	})
	redirects := 0
	c.SetHTTPClient(&http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
		redirects++
		return nil
	}})
	_, err := c.Send(context.Background(), edgeArgs())
	edgeWantCode(t, err, "TARGET_NOT_ALLOWED")
	if redirects != 0 {
		t.Fatalf("redirect policy was not replaced: %d", redirects)
	}
}

func TestEdgeExpandedDocument(t *testing.T) {
	doc := map[string]any{
		"openapi": "3.1.0",
		"paths": map[string]any{
			"/api/x": map[string]any{"post": map[string]any{"operationId": "x", "requestBody": map[string]any{"content": map[string]any{"application/json": map[string]any{"schema": map[string]any{"$ref": "#/components/schemas/A"}}}}}},
		},
		"components": map[string]any{"schemas": map[string]any{"A": map[string]any{"type": "object", "properties": map[string]any{"b": map[string]any{"$ref": "#/components/schemas/B"}}}, "B": map[string]any{"type": "string"}}},
	}
	expanded, err := ExpandedDocument(doc)
	if err != nil {
		t.Fatal(err)
	}
	if expanded["components"] == nil {
		t.Fatalf("expanded = %+v", expanded)
	}

	broken := map[string]any{"a": map[string]any{"$ref": "https://evil.example/schema"}}
	if _, err := ExpandedDocument(broken); err == nil {
		t.Fatal("external reference must fail")
	}
}

func TestEdgeConcurrentSend(t *testing.T) {
	var calls atomic.Int32
	c, _ := edgeFixture(t, edgeOperations, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"message":"ok"}`)
	})
	const workers = 8
	var wg sync.WaitGroup
	wg.Add(workers)
	errs := make(chan error, workers)
	for i := 0; i < workers; i++ {
		go func() {
			defer wg.Done()
			_, err := c.Send(context.Background(), edgeArgs())
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if calls.Load() != workers {
		t.Fatalf("calls = %d", calls.Load())
	}
}
