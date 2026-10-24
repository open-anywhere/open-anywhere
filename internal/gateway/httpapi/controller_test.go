package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const inventory = `{"openapi":"%s","info":{"title":"Inventory","version":"1"},"paths":{"/items/{id}":{"post":{"operationId":"updateItem","parameters":[{"name":"id","in":"path","required":true,"schema":{"type":"string"}},{"name":"verbose","in":"query","schema":{"type":"boolean"}}],"requestBody":{"required":true,"content":{"application/json":{"schema":{"$ref":"#/components/schemas/Item"}}}},"responses":{"200":{"description":"ok"}}}}},"components":{"schemas":{"Item":{"type":"object","required":["count"],"additionalProperties":false,"properties":{"count":{"type":"integer","minimum":0}}}}}}`

func fixture(t *testing.T, version string, handler http.HandlerFunc) (*Controller, *atomic.Int32) {
	t.Helper()
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/spec" {
			fmt.Fprintf(w, inventory, version)
			return
		}
		calls.Add(1)
		handler(w, r)
	}))
	t.Cleanup(srv.Close)
	c, err := New(context.Background(), Config{[]Service{{ID: "inventory", OpenAPIURL: srv.URL + "/spec", BaseURL: srv.URL + "/v1", TimeoutMS: 100}}})
	if err != nil {
		t.Fatal(err)
	}
	return c, &calls
}
func args() Arguments {
	return Arguments{ServiceID: "inventory", OperationID: "updateItem", Parameters: map[string]any{"id": "two words", "verbose": true}, Body: json.RawMessage(`{"count":2}`)}
}
func requireCode(t *testing.T, err error, code string) {
	t.Helper()
	var e *Error
	if !errors.As(err, &e) || e.Code != code {
		t.Fatalf("want %s, got %v", code, err)
	}
}
func TestVersionsAndExecution(t *testing.T) {
	for _, version := range []string{"3.0.3", "3.1.0"} {
		t.Run(version, func(t *testing.T) {
			c, _ := fixture(t, version, func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "POST" || r.URL.EscapedPath() != "/v1/items/two%20words" || r.URL.Query().Get("verbose") != "true" {
					t.Errorf("wrong request %s %s", r.Method, r.URL)
				}
				var body map[string]any
				_ = json.NewDecoder(r.Body).Decode(&body)
				if body["count"] != float64(2) {
					t.Error(body)
				}
				w.Header().Set("Content-Type", "application/json")
				fmt.Fprint(w, `{"message":"saved"}`)
			})
			doc, err := c.Document("inventory")
			if err != nil {
				t.Fatal(err)
			}
			doc["openapi"] = "bad"
			copy, _ := c.Document("inventory")
			if copy["openapi"] != version {
				t.Fatal("cache mutated")
			}
			out, err := c.Send(context.Background(), args())
			if err != nil || !out.OK || out.Status != 200 {
				t.Fatalf("%+v %v", out, err)
			}
		})
	}
}
func TestInvalidInputNeverExecutes(t *testing.T) {
	c, calls := fixture(t, "3.0.3", func(w http.ResponseWriter, r *http.Request) { t.Error("should not call upstream") })
	cases := []struct {
		name, code string
		mutate     func(*Arguments)
	}{
		{"unknown service", "SERVICE_NOT_FOUND", func(a *Arguments) { a.ServiceID = "other" }},
		{"unknown operation", "OPERATION_NOT_FOUND", func(a *Arguments) { a.OperationID = "other" }},
		{"missing path", "INVALID_PARAMETERS", func(a *Arguments) { delete(a.Parameters, "id") }},
		{"unknown parameter", "INVALID_PARAMETERS", func(a *Arguments) { a.Parameters["extra"] = true }},
		{"traversal", "INVALID_PARAMETERS", func(a *Arguments) { a.Parameters["id"] = "../private" }},
		{"encoded slash", "INVALID_PARAMETERS", func(a *Arguments) { a.Parameters["id"] = "%2f" }},
		{"type", "INVALID_REQUEST_BODY", func(a *Arguments) { a.Body = json.RawMessage(`{"count":"2"}`) }},
		{"extra body", "INVALID_REQUEST_BODY", func(a *Arguments) { a.Body = json.RawMessage(`{"count":2,"x":1}`) }},
		{"missing body", "INVALID_REQUEST_BODY", func(a *Arguments) { a.Body = nil }},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			a := args()
			tt.mutate(&a)
			_, err := c.Send(context.Background(), a)
			requireCode(t, err, tt.code)
		})
	}
	if calls.Load() != 0 {
		t.Fatal(calls.Load())
	}
}
func TestResponseBoundaries(t *testing.T) {
	for _, mode := range []string{"conflict", "redirect", "nonjson", "oversize", "timeout"} {
		t.Run(mode, func(t *testing.T) {
			c, calls := fixture(t, "3.0.3", func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				switch mode {
				case "conflict":
					w.WriteHeader(409)
					fmt.Fprint(w, `{"message":"not ready"}`)
				case "redirect":
					w.Header().Set("Location", "/private")
					w.WriteHeader(302)
				case "nonjson":
					w.Header().Set("Content-Type", "text/plain")
					fmt.Fprint(w, "bad")
				case "oversize":
					fmt.Fprint(w, strings.Repeat("x", Limit+1))
				case "timeout":
					time.Sleep(200 * time.Millisecond)
				}
			})
			out, err := c.Send(context.Background(), args())
			if mode == "conflict" {
				if err != nil || out.OK || out.Status != 409 {
					t.Fatalf("%+v %v", out, err)
				}
			} else {
				requireCode(t, err, map[string]string{"redirect": "TARGET_NOT_ALLOWED", "nonjson": "INVALID_RESPONSE", "oversize": "RESPONSE_TOO_LARGE", "timeout": "TIMEOUT"}[mode])
			}
			if calls.Load() != 1 {
				t.Fatal("unexpected retry")
			}
		})
	}
}
func TestSchemaSemantics(t *testing.T) {
	for _, version := range []string{"3.0.3", "3.1.0"} {
		var schema any
		raw := `{"type":"object","properties":{"name":{"type":"string","nullable":true},"value":{"type":"number","minimum":1,"exclusiveMinimum":true}},"required":["name","value"]}`
		if version == "3.1.0" {
			raw = `{"type":"object","properties":{"name":{"type":["string","null"]},"value":{"type":"number","exclusiveMinimum":1}},"required":["name","value"]}`
		}
		_ = json.Unmarshal([]byte(raw), &schema)
		compiled, err := compileSchema(schema, version)
		if err != nil {
			t.Fatal(err)
		}
		if err = compiled.Validate(map[string]any{"name": nil, "value": 2.0}); err != nil {
			t.Fatal(err)
		}
		if compiled.Validate(map[string]any{"name": nil, "value": 1.0}) == nil {
			t.Fatal("exclusive bound not enforced")
		}
	}
}
func TestReferenceAndConfigErrors(t *testing.T) {
	for _, ref := range []string{"https://evil.test/schema", "#/missing", "#/loop"} {
		_, err := resolve(map[string]any{"$ref": ref}, map[string]any{"loop": map[string]any{"$ref": "#/loop"}}, nil, 0)
		if err == nil {
			t.Fatal(ref)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_, err := New(ctx, Config{[]Service{{ID: "x", OpenAPIURL: "file:///etc/passwd", BaseURL: "http://localhost"}}})
	requireCode(t, err, "INVALID_CONFIG")
}
