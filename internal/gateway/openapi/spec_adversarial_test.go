package openapi

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/open-anywhere/open-anywhere/internal/gateway/httpapi"
)

// executableSpec is a valid contract with one forwardable operation.
const executableSpec = `openapi: 3.1.0
paths:
  /api/echo:
    post:
      operationId: echo
      parameters:
        - name: tag
          in: query
          schema:
            type: string
      requestBody:
        required: true
        content:
          application/json:
            schema:
              type: object
              required: [n]
              additionalProperties: false
              properties:
                n:
                  type: integer
      responses:
        "200":
          description: ok
`

func TestSpecUnmarshalAndNilEdges(t *testing.T) {
	var spec Spec
	if err := json.Unmarshal([]byte("[]"), &spec); err == nil {
		t.Fatal("non-object JSON payload should fail")
	}
	if err := json.Unmarshal([]byte(`{"operations":[{"operationId":""},{"operationId":"kept","method":"GET"}]}`), &spec); err != nil {
		t.Fatal(err)
	}
	if operations := spec.Operations(); len(operations) != 1 || operations[0].OperationID != "kept" {
		t.Fatalf("operations = %+v", operations)
	}
	if spec.document != nil {
		t.Fatalf("document = %s", spec.document)
	}

	empty := &Spec{}
	if _, err := empty.Document(); err == nil {
		t.Fatal("missing document should fail")
	}
	if _, err := empty.Execute(context.Background(), "http://127.0.0.1", "op", nil, nil, nil); err == nil {
		t.Fatal("missing document should prevent execution")
	}
	var nilSpec *Spec
	if _, err := nilSpec.Document(); err == nil {
		t.Fatal("nil spec should fail")
	}
	if _, err := nilSpec.Execute(context.Background(), "http://127.0.0.1", "op", nil, nil, nil); err == nil {
		t.Fatal("nil spec should prevent execution")
	}
}

func TestParseAdditionalAdversarialInputs(t *testing.T) {
	cases := map[string]string{
		"non json compatible value": `openapi: 3.1.0
paths:
  /api/x:
    get:
      operationId: x
extra: .nan
`,
		"unexpanded recursive component": `openapi: 3.1.0
paths:
  /api/x:
    get:
      operationId: x
components:
  schemas:
    Loop:
      $ref: '#/components/schemas/Loop'
`,
		"trace operation not object": `openapi: 3.1.0
paths:
  /api/x:
    trace: nope
`,
		"trace operation without id": `openapi: 3.1.0
paths:
  /api/x:
    trace:
      description: no id
`,
		"trace operation with blank id": `openapi: 3.1.0
paths:
  /api/x:
    trace:
      operationId: "   "
`,
		"duplicate id across ignored method": `openapi: 3.1.0
paths:
  /api/x:
    get:
      operationId: same
    trace:
      operationId: same
`,
		"path parameters not array": `openapi: 3.1.0
paths:
  /api/ok:
    get:
      operationId: ok
  /api/bad:
    parameters: nope
    trace:
      operationId: ignored
`,
		"path parameter entry not object": `openapi: 3.1.0
paths:
  /api/ok:
    get:
      operationId: ok
  /api/bad:
    parameters:
      - nope
    trace:
      operationId: ignored
`,
		"blank parameter name": `openapi: 3.1.0
paths:
  /api/x:
    get:
      operationId: x
      parameters:
        - name: "   "
          in: query
          schema:
            type: string
`,
		"operation parameters not array": `openapi: 3.1.0
paths:
  /api/x:
    trace:
      operationId: x
      parameters: nope
`,
		"request body not object": `openapi: 3.1.0
paths:
  /api/x:
    trace:
      operationId: x
      requestBody: nope
`,
		"empty server url": `openapi: 3.1.0
servers:
  - url: ""
paths:
  /api/x:
    get:
      operationId: x
`,
		"malformed server url": `openapi: 3.1.0
servers:
  - url: "%zz"
paths:
  /api/x:
    get:
      operationId: x
`,
	}
	for name, document := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := Parse([]byte(document)); err == nil {
				t.Fatal("expected an error")
			}
		})
	}
}

func TestParseSkipsUnsupportedParameterLocations(t *testing.T) {
	document := `openapi: 3.1.0
paths:
  /api/ok:
    get:
      operationId: ok
  /api/x:
    parameters:
      - name: cookie_value
        in: cookie
        schema:
          type: string
    trace:
      operationId: traced
`
	spec, err := Parse([]byte(document))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	traced, ok := spec.Lookup("traced")
	if !ok {
		t.Fatal("trace operation was not indexed")
	}
	if len(traced.Parameters) != 0 {
		t.Fatalf("cookie parameter should be skipped: %+v", traced.Parameters)
	}
	if operations := spec.Operations(); len(operations) != 2 || operations[0].OperationID != "ok" {
		t.Fatalf("operations = %+v", operations)
	}
}

func TestParseAcceptsEmptyRequestBodyMetadata(t *testing.T) {
	document := `openapi: 3.1.0
paths:
  /api/x:
    trace:
      operationId: x
      requestBody: {}
`
	spec, err := Parse([]byte(document))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	operation, _ := spec.Lookup("x")
	if operation.RequestBody != nil {
		t.Fatalf("empty request body metadata = %+v", operation.RequestBody)
	}
}

func TestExecuteBoundaries(t *testing.T) {
	spec, err := Parse([]byte(executableSpec))
	if err != nil {
		t.Fatal(err)
	}

	if _, err := spec.Execute(context.Background(), "://bad", "echo", nil, nil, nil); err == nil {
		t.Fatal("invalid base URL should fail")
	}
	if _, err := spec.Execute(context.Background(), "http://127.0.0.1", "echo", nil, map[string]any{"n": math.NaN()}, nil); err == nil {
		t.Fatal("non JSON body should fail before sending")
	}
	if _, err := spec.Execute(context.Background(), "http://127.0.0.1", "missing", nil, nil, nil); err == nil {
		t.Fatal("unknown operation should fail")
	}

	var (
		seenPath  string
		seenQuery string
		seenBody  map[string]any
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenPath = r.URL.Path
		seenQuery = r.URL.Query().Get("tag")
		_ = json.NewDecoder(r.Body).Decode(&seenBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"message":"echoed","ok":true}`))
	}))
	defer srv.Close()

	result, err := spec.Execute(context.Background(), srv.URL, "echo", map[string]any{"tag": "v"}, map[string]any{"n": 2}, srv.Client())
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !result.OK || result.Status != http.StatusOK || result.Message != "echoed" {
		t.Fatalf("result = %+v", result)
	}
	if seenPath != "/api/echo" || seenQuery != "v" || seenBody["n"] != float64(2) {
		t.Fatalf("request = %q %q %+v", seenPath, seenQuery, seenBody)
	}

	// A nil client must not disable execution.
	result, err = spec.Execute(context.Background(), srv.URL, "echo", nil, map[string]any{"n": 3}, nil)
	if err != nil || !result.OK {
		t.Fatalf("nil client: %+v %v", result, err)
	}
}

func TestExecuteRejectsSchemaMismatch(t *testing.T) {
	spec, err := Parse([]byte(executableSpec))
	if err != nil {
		t.Fatal(err)
	}
	_, err = spec.Execute(context.Background(), "http://127.0.0.1:1", "echo", nil, map[string]any{"n": "not a number"}, nil)
	var apiErr *httpapi.Error
	if !errors.As(err, &apiErr) || apiErr.Code != "INVALID_REQUEST_BODY" {
		t.Fatalf("err = %v", err)
	}
}
