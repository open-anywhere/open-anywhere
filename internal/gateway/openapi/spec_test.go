package openapi

import (
	"encoding/json"
	"strings"
	"testing"
)

const validSpec = `openapi: 3.1.0
servers:
  - url: /
paths:
  /api/roll:
    post:
      operationId: rollDice
      responses:
        "200":
          description: ok
  /api/results:
    get:
      operationId: listResults
      responses:
        "200":
          description: ok
`

func TestParseIndexesOperations(t *testing.T) {
	spec, err := Parse([]byte(validSpec))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	operation, ok := spec.Lookup("rollDice")
	if !ok {
		t.Fatal("rollDice not indexed")
	}
	if operation.Method != "POST" || operation.Path != "/api/roll" {
		t.Fatalf("operation = %+v", operation)
	}
	if operations := spec.Operations(); len(operations) != 2 || operations[0].OperationID != "listResults" {
		t.Fatalf("operations = %+v", operations)
	}
	var nilSpec *Spec
	if _, ok := nilSpec.Lookup("rollDice"); ok {
		t.Fatal("nil spec should not resolve operations")
	}
	if nilSpec.Operations() != nil {
		t.Fatal("nil spec should have no operations")
	}
}

func TestParseRejectsInvalidDocuments(t *testing.T) {
	tests := map[string]string{
		"bad yaml":          "openapi: [",
		"wrong version":     "openapi: 3.0.0\npaths:\n  /api/x:\n    get:\n      operationId: x\n",
		"no paths":          "openapi: 3.1.0\n",
		"non-api path":      "openapi: 3.1.0\npaths:\n  /health:\n    get:\n      operationId: health\n",
		"missing operation": "openapi: 3.1.0\npaths:\n  /api/x:\n    get:\n      description: no id\n",
		"duplicate id":      "openapi: 3.1.0\npaths:\n  /api/a:\n    get:\n      operationId: same\n  /api/b:\n    get:\n      operationId: same\n",
		"absolute server":   "openapi: 3.1.0\nservers:\n  - url: https://example.com\npaths:\n  /api/x:\n    get:\n      operationId: x\n",
		"no operations":     "openapi: 3.1.0\npaths:\n  /api/x:\n    parameters: []\n",
	}
	for name, document := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := Parse([]byte(document)); err == nil {
				t.Fatal("expected an error")
			}
		})
	}
}

func TestParseRejectsNonObjectPath(t *testing.T) {
	document := "openapi: 3.1.0\npaths:\n  /api/x: nope\n"
	_, err := Parse([]byte(document))
	if err == nil || !strings.Contains(err.Error(), "must be an object") {
		t.Fatalf("err = %v", err)
	}
}

func TestParseRejectsNonObjectOperation(t *testing.T) {
	document := "openapi: 3.1.0\npaths:\n  /api/x:\n    get: nope\n"
	_, err := Parse([]byte(document))
	if err == nil || !strings.Contains(err.Error(), "must be an object") {
		t.Fatalf("err = %v", err)
	}
}

func TestParseRejectsNonArrayServers(t *testing.T) {
	document := "openapi: 3.1.0\nservers: nope\npaths:\n  /api/x:\n    get:\n      operationId: x\n"
	_, err := Parse([]byte(document))
	if err == nil || !strings.Contains(err.Error(), "servers must be an array") {
		t.Fatalf("err = %v", err)
	}
}

func TestParseRejectsMalformedServerEntry(t *testing.T) {
	document := "openapi: 3.1.0\nservers:\n  - nope\npaths:\n  /api/x:\n    get:\n      operationId: x\n"
	_, err := Parse([]byte(document))
	if err == nil || !strings.Contains(err.Error(), "server entry") {
		t.Fatalf("err = %v", err)
	}
}

func TestSpecJSONRoundTrip(t *testing.T) {
	spec, err := Parse([]byte(validSpec))
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(spec)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var restored Spec
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	operation, ok := restored.Lookup("rollDice")
	if !ok || operation.Method != "POST" || operation.Path != "/api/roll" {
		t.Fatalf("operation = %+v, %v", operation, ok)
	}
	if operations := restored.Operations(); len(operations) != 2 {
		t.Fatalf("operations = %+v", operations)
	}
	if err := json.Unmarshal([]byte("null"), &restored); err != nil {
		t.Fatalf("null spec: %v", err)
	}
	if len(restored.Operations()) != 0 {
		t.Fatal("null spec should have no operations")
	}
}

func TestParseOperationMetadata(t *testing.T) {
	document := `openapi: 3.1.0
paths:
  /api/items/{id}:
    parameters:
      - name: id
        in: path
        required: true
        description: 条目 ID
        schema:
          type: string
      - name: ignored
        in: header
        schema:
          type: string
    post:
      operationId: updateItem
      summary: 更新条目
      description: 修改条目名称并返回最新状态
      parameters:
        - name: id
          in: path
          required: true
          description: 覆盖后的说明
          schema:
            type: integer
        - name: verbose
          in: query
          schema:
            type: boolean
      requestBody:
        required: true
        description: 新的条目字段
        content:
          application/json:
            schema:
              type: object
              properties:
                name:
                  type: string
      responses:
        "200":
          description: ok
`
	spec, err := Parse([]byte(document))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	operation, ok := spec.Lookup("updateItem")
	if !ok {
		t.Fatal("updateItem not indexed")
	}
	if operation.Summary != "更新条目" || operation.Description != "修改条目名称并返回最新状态" {
		t.Fatalf("operation = %+v", operation)
	}
	if len(operation.Parameters) != 3 {
		t.Fatalf("parameters = %+v", operation.Parameters)
	}
	if operation.Parameters[0].Name != "id" || operation.Parameters[0].Description != "覆盖后的说明" || operation.Parameters[0].Schema["type"] != "integer" {
		t.Fatalf("path parameter = %+v", operation.Parameters[0])
	}
	if operation.Parameters[2].Name != "verbose" || operation.Parameters[2].In != "query" {
		t.Fatalf("query parameter = %+v", operation.Parameters[2])
	}
	if operation.RequestBody == nil || !operation.RequestBody.Required || operation.RequestBody.Description != "新的条目字段" {
		t.Fatalf("body = %+v", operation.RequestBody)
	}
	if operation.RequestBody.Schema["type"] != "object" {
		t.Fatalf("body schema = %+v", operation.RequestBody.Schema)
	}

	data, err := json.Marshal(spec)
	if err != nil {
		t.Fatal(err)
	}
	var restored Spec
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatal(err)
	}
	restoredOperation, _ := restored.Lookup("updateItem")
	if len(restoredOperation.Parameters) != 3 || restoredOperation.RequestBody == nil {
		t.Fatalf("restored = %+v", restoredOperation)
	}
}

func TestParseRejectsMalformedOperationMetadata(t *testing.T) {
	tests := map[string]string{
		"bad path parameters":  "openapi: 3.1.0\npaths:\n  /api/x:\n    parameters: nope\n    get:\n      operationId: x\n",
		"bad parameter entry":  "openapi: 3.1.0\npaths:\n  /api/x:\n    get:\n      operationId: x\n      parameters:\n        - nope\n",
		"unnamed parameter":    "openapi: 3.1.0\npaths:\n  /api/x:\n    get:\n      operationId: x\n      parameters:\n        - in: query\n",
		"bad operation params": "openapi: 3.1.0\npaths:\n  /api/x:\n    get:\n      operationId: x\n      parameters: nope\n",
		"bad request body":     "openapi: 3.1.0\npaths:\n  /api/x:\n    get:\n      operationId: x\n      requestBody: nope\n",
	}
	for name, document := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := Parse([]byte(document)); err == nil {
				t.Fatal("expected an error")
			}
		})
	}
}

func TestFullDocumentAndReferencesPersist(t *testing.T) {
	source := `{"openapi":"3.1.0","paths":{"/api/x":{"post":{"operationId":"update","requestBody":{"content":{"application/json":{"schema":{"$ref":"#/components/schemas/Payload"}}}},"responses":{"200":{"description":"ok"}}}}},"components":{"schemas":{"Payload":{"type":"object","required":["count"],"properties":{"count":{"type":"integer"}}}}}}`
	spec, err := Parse([]byte(source))
	if err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(spec)
	if err != nil {
		t.Fatal(err)
	}
	var restored Spec
	if err = json.Unmarshal(b, &restored); err != nil {
		t.Fatal(err)
	}
	doc, err := restored.Document()
	if err != nil || doc["components"] == nil {
		t.Fatal(doc, err)
	}
	op, _ := restored.Lookup("update")
	if op.RequestBody.Schema["type"] != "object" {
		t.Fatal("unresolved body")
	}
}
