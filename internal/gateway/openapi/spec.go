// Package openapi parses and validates the application's OpenAPI contract and
// indexes operations for the generic send-request MCP tool.
package openapi

import (
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// Parameter describes one path or query parameter declared by an operation.
type Parameter struct {
	Name        string         `json:"name"`
	In          string         `json:"in"`
	Required    bool           `json:"required,omitempty"`
	Description string         `json:"description,omitempty"`
	Schema      map[string]any `json:"schema,omitempty"`
}

// RequestBody describes the JSON request body accepted by an operation.
type RequestBody struct {
	Required    bool           `json:"required,omitempty"`
	Description string         `json:"description,omitempty"`
	Schema      map[string]any `json:"schema,omitempty"`
}

// Operation describes one HTTP operation declared by the application.
type Operation struct {
	OperationID string       `json:"operationId"`
	Method      string       `json:"method"`
	Path        string       `json:"path"`
	Summary     string       `json:"summary,omitempty"`
	Description string       `json:"description,omitempty"`
	Parameters  []Parameter  `json:"parameters,omitempty"`
	RequestBody *RequestBody `json:"request_body,omitempty"`
}

// Spec is the parsed subset of an OpenAPI 3.1 document the control plane
// needs.
type Spec struct {
	operations map[string]Operation
}

// Lookup returns the operation with the given ID.
func (s *Spec) Lookup(operationID string) (Operation, bool) {
	if s == nil {
		return Operation{}, false
	}
	operation, ok := s.operations[operationID]
	return operation, ok
}

// Operations returns all declared operations ordered by operation ID.
func (s *Spec) Operations() []Operation {
	if s == nil {
		return nil
	}
	result := make([]Operation, 0, len(s.operations))
	for _, operation := range s.operations {
		result = append(result, operation)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].OperationID < result[j].OperationID })
	return result
}

// MarshalJSON persists the indexed operations.
func (s Spec) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Operations []Operation `json:"operations"`
	}{Operations: s.Operations()})
}

// UnmarshalJSON restores the indexed operations from a database record.
func (s *Spec) UnmarshalJSON(data []byte) error {
	var payload struct {
		Operations []Operation `json:"operations"`
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		return err
	}
	s.operations = make(map[string]Operation, len(payload.Operations))
	for _, operation := range payload.Operations {
		if operation.OperationID == "" {
			continue
		}
		s.operations[operation.OperationID] = operation
	}
	return nil
}

var methods = map[string]bool{
	"get": true, "put": true, "post": true, "delete": true,
	"options": true, "head": true, "patch": true, "trace": true,
}

// Parse validates data as an OpenAPI 3.1 document limited to /api/* business
// operations and indexes its operation IDs.
func Parse(data []byte) (*Spec, error) {
	var document map[string]any
	if err := yaml.Unmarshal(data, &document); err != nil {
		return nil, fmt.Errorf("openapi: invalid YAML: %w", err)
	}
	version, _ := document["openapi"].(string)
	if !strings.HasPrefix(version, "3.1") {
		return nil, fmt.Errorf("openapi: version must be 3.1, got %q", version)
	}
	if servers, ok := document["servers"]; ok {
		if err := validateServers(servers); err != nil {
			return nil, err
		}
	}
	paths, ok := document["paths"].(map[string]any)
	if !ok || len(paths) == 0 {
		return nil, fmt.Errorf("openapi: paths must be a non-empty object")
	}
	spec := &Spec{operations: make(map[string]Operation)}
	for path, rawItem := range paths {
		if !strings.HasPrefix(path, "/api/") {
			return nil, fmt.Errorf("openapi: path %q must start with /api/", path)
		}
		item, ok := rawItem.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("openapi: path %q must be an object", path)
		}
		var common []Parameter
		if rawParameters, exists := item["parameters"]; exists {
			parsed, parseErr := parseParameters(rawParameters)
			if parseErr != nil {
				return nil, fmt.Errorf("openapi: path %q: %w", path, parseErr)
			}
			common = parsed
		}
		for method, rawOperation := range item {
			if !methods[strings.ToLower(method)] {
				continue
			}
			operation, ok := rawOperation.(map[string]any)
			if !ok {
				return nil, fmt.Errorf("openapi: operation %s %s must be an object", strings.ToUpper(method), path)
			}
			id, _ := operation["operationId"].(string)
			if strings.TrimSpace(id) == "" {
				return nil, fmt.Errorf("openapi: operation %s %s has no operationId", strings.ToUpper(method), path)
			}
			if existing, exists := spec.operations[id]; exists {
				return nil, fmt.Errorf("openapi: duplicate operationId %q at %s %s and %s %s", id, existing.Method, existing.Path, strings.ToUpper(method), path)
			}
			parameters := common
			if rawParameters, exists := operation["parameters"]; exists {
				own, parseErr := parseParameters(rawParameters)
				if parseErr != nil {
					return nil, fmt.Errorf("openapi: operation %s %s: %w", strings.ToUpper(method), path, parseErr)
				}
				parameters = mergeParameters(common, own)
			}
			body, parseErr := parseRequestBody(operation["requestBody"])
			if parseErr != nil {
				return nil, fmt.Errorf("openapi: operation %s %s: %w", strings.ToUpper(method), path, parseErr)
			}
			summary, _ := operation["summary"].(string)
			description, _ := operation["description"].(string)
			spec.operations[id] = Operation{
				OperationID: id,
				Method:      strings.ToUpper(method),
				Path:        path,
				Summary:     strings.TrimSpace(summary),
				Description: strings.TrimSpace(description),
				Parameters:  parameters,
				RequestBody: body,
			}
		}
	}
	if len(spec.operations) == 0 {
		return nil, fmt.Errorf("openapi: no operations declared under /api/")
	}
	return spec, nil
}

func validateServers(raw any) error {
	list, ok := raw.([]any)
	if !ok {
		return fmt.Errorf("openapi: servers must be an array")
	}
	for _, entry := range list {
		server, ok := entry.(map[string]any)
		if !ok {
			return fmt.Errorf("openapi: server entry must be an object")
		}
		value, _ := server["url"].(string)
		parsed, err := url.Parse(value)
		if err != nil || value == "" {
			return fmt.Errorf("openapi: server url %q is invalid", value)
		}
		if parsed.IsAbs() || parsed.Host != "" {
			return fmt.Errorf("openapi: server url %q must be relative", value)
		}
	}
	return nil
}

func parseParameters(raw any) ([]Parameter, error) {
	list, ok := raw.([]any)
	if !ok {
		return nil, fmt.Errorf("parameters must be an array")
	}
	parameters := make([]Parameter, 0, len(list))
	for _, entry := range list {
		item, ok := entry.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("parameter must be an object")
		}
		name, _ := item["name"].(string)
		location, _ := item["in"].(string)
		if location != "path" && location != "query" {
			// send-request only supports path and query parameters.
			continue
		}
		if strings.TrimSpace(name) == "" {
			return nil, fmt.Errorf("parameter has no name")
		}
		required, _ := item["required"].(bool)
		description, _ := item["description"].(string)
		schema, _ := item["schema"].(map[string]any)
		parameters = append(parameters, Parameter{
			Name:        name,
			In:          location,
			Required:    required,
			Description: strings.TrimSpace(description),
			Schema:      schema,
		})
	}
	return parameters, nil
}

func mergeParameters(base, overrides []Parameter) []Parameter {
	merged := append([]Parameter(nil), base...)
	for _, override := range overrides {
		replaced := false
		for index := range merged {
			if merged[index].Name == override.Name && merged[index].In == override.In {
				merged[index] = override
				replaced = true
				break
			}
		}
		if !replaced {
			merged = append(merged, override)
		}
	}
	return merged
}

func parseRequestBody(raw any) (*RequestBody, error) {
	if raw == nil {
		return nil, nil
	}
	item, ok := raw.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("requestBody must be an object")
	}
	body := &RequestBody{}
	body.Required, _ = item["required"].(bool)
	body.Description, _ = item["description"].(string)
	body.Description = strings.TrimSpace(body.Description)
	content, _ := item["content"].(map[string]any)
	if media, ok := content["application/json"].(map[string]any); ok {
		body.Schema, _ = media["schema"].(map[string]any)
	}
	if body.Schema == nil && body.Description == "" && !body.Required {
		return nil, nil
	}
	return body, nil
}
