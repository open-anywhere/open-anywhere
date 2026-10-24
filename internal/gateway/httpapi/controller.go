// Package openapi executes registered HTTP operations from an immutable OpenAPI snapshot.
package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"go.yaml.in/yaml/v3"
)

const Limit = 1 << 20

type Error struct {
	Code    string
	Message string
}

func (e *Error) Error() string           { return e.Message }
func failure(code, message string) error { return &Error{code, message} }

type Config struct {
	Services []Service `json:"services"`
}
type Service struct {
	ID         string `json:"service_id"`
	OpenAPIURL string `json:"openapi_url"`
	BaseURL    string `json:"base_url"`
	TimeoutMS  int    `json:"timeout_ms"`
}
type Arguments struct {
	ServiceID   string          `json:"service_id"`
	OperationID string          `json:"operationId"`
	Parameters  map[string]any  `json:"parameters,omitempty"`
	Body        json.RawMessage `json:"requestBody,omitempty"`
}
type Result struct {
	OK      bool         `json:"ok"`
	Status  int          `json:"status"`
	Data    any          `json:"data"`
	Message string       `json:"message"`
	Error   *ResultError `json:"error,omitempty"`
}
type ResultError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}
type parameter struct {
	name, location string
	required       bool
	schema         *jsonschema.Schema
}
type operation struct {
	method, path string
	params       map[string]parameter
	body         *jsonschema.Schema
	bodyRequired bool
}
type entry struct {
	config     Service
	document   map[string]any
	operations map[string]operation
}
type Controller struct {
	entries map[string]entry
	client  *http.Client
}

// New loads all documents before publishing the controller. Requests never mutate its cache.
func New(ctx context.Context, config Config) (*Controller, error) {
	c := &Controller{entries: map[string]entry{}, client: &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
	if len(config.Services) == 0 {
		return nil, failure("INVALID_CONFIG", "No services configured")
	}
	for _, s := range config.Services {
		if s.ID == "" {
			return nil, failure("INVALID_CONFIG", "Missing service_id")
		}
		if _, exists := c.entries[s.ID]; exists {
			return nil, failure("INVALID_CONFIG", "Duplicate service_id")
		}
		for _, raw := range []string{s.OpenAPIURL, s.BaseURL} {
			u, err := url.Parse(raw)
			if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.Fragment != "" {
				return nil, failure("INVALID_CONFIG", "Invalid service URL")
			}
		}
		base, _ := url.Parse(s.BaseURL)
		if base.RawQuery != "" || strings.Contains(base.EscapedPath(), "%") || !safePath(base.Path) {
			return nil, failure("INVALID_CONFIG", "Invalid base path")
		}
		if s.TimeoutMS == 0 {
			s.TimeoutMS = 10000
		}
		if s.TimeoutMS < 1 || s.TimeoutMS > 120000 {
			return nil, failure("INVALID_CONFIG", "Timeout must be 1..120000 ms")
		}
		e, err := c.load(ctx, s)
		if err != nil {
			return nil, fmt.Errorf("service %s: %w", s.ID, err)
		}
		c.entries[s.ID] = e
	}
	return c, nil
}
func (c *Controller) IDs() []string {
	ids := make([]string, 0, len(c.entries))
	for id := range c.entries {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}
func (c *Controller) Document(id string) (map[string]any, error) {
	e, ok := c.entries[id]
	if !ok {
		return nil, failure("SERVICE_NOT_FOUND", "Unknown service: "+id)
	}
	// Return a copy so callers cannot change the schema used for execution.
	b, _ := json.Marshal(e.document)
	var doc map[string]any
	_ = json.Unmarshal(b, &doc)
	return doc, nil
}
func bounded(r io.Reader) ([]byte, error) {
	b, e := io.ReadAll(io.LimitReader(r, Limit+1))
	if e != nil {
		return nil, e
	}
	if len(b) > Limit {
		return nil, failure("RESPONSE_TOO_LARGE", "Response exceeds 1 MiB")
	}
	return b, nil
}
func object(v any) map[string]any { m, _ := v.(map[string]any); return m }
func text(v any) string           { s, _ := v.(string); return s }
func boolean(v any) bool          { b, _ := v.(bool); return b }
func safePath(s string) bool {
	if strings.ContainsAny(s, "\\?#\r\n") || strings.HasPrefix(s, "//") {
		return false
	}
	for _, part := range strings.Split(s, "/") {
		if part == "." || part == ".." {
			return false
		}
	}
	return true
}

var placeholder = regexp.MustCompile(`\{([^{}]+)\}`)

func blockedHeader(name string) bool {
	n := strings.ToLower(name)
	if strings.HasPrefix(n, "proxy-") || strings.HasPrefix(n, "sec-") {
		return true
	}
	switch n {
	case "authorization", "host", "connection", "content-length", "transfer-encoding", "cookie", "set-cookie", "content-type", "accept", "upgrade", "te", "trailer":
		return true
	}
	return false
}
func (c *Controller) load(parent context.Context, s Service) (entry, error) {
	e := entry{config: s, operations: map[string]operation{}}
	ctx, cancel := context.WithTimeout(parent, time.Duration(s.TimeoutMS)*time.Millisecond)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, "GET", s.OpenAPIURL, nil)
	if err != nil {
		return e, err
	}
	res, err := c.client.Do(req)
	if err != nil {
		return e, failure("OPENAPI_UNAVAILABLE", "Cannot fetch OpenAPI")
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return e, failure("OPENAPI_UNAVAILABLE", fmt.Sprintf("OpenAPI HTTP %d", res.StatusCode))
	}
	b, err := bounded(res.Body)
	if err != nil {
		return e, err
	}
	return compileDocument(s, b)
}

func compileDocument(s Service, b []byte) (entry, error) {
	e := entry{config: s, operations: map[string]operation{}}
	var err error
	var raw map[string]any
	if err = yaml.Unmarshal(b, &raw); err != nil {
		return e, failure("INVALID_OPENAPI", "Invalid YAML/JSON")
	}
	// Normalize YAML scalar numbers to the JSON value model expected by the validator.
	b, err = json.Marshal(raw)
	if err != nil {
		return e, failure("INVALID_OPENAPI", "Document is not JSON-compatible")
	}
	if err = json.Unmarshal(b, &raw); err != nil {
		return e, err
	}
	e.document = raw
	version := text(raw["openapi"])
	if !(strings.HasPrefix(version, "3.0.") || strings.HasPrefix(version, "3.1.")) || object(raw["paths"]) == nil {
		return e, failure("INVALID_OPENAPI", "OpenAPI 3.0/3.1 paths required")
	}
	for path, itemRaw := range object(raw["paths"]) {
		if !strings.HasPrefix(path, "/") || !safePath(path) || strings.Contains(path, "%") {
			return e, failure("INVALID_OPENAPI", "Invalid operation path")
		}
		resolved, err := resolve(itemRaw, raw, nil, 0)
		if err != nil {
			return e, err
		}
		item := object(resolved)
		if item == nil {
			return e, failure("INVALID_OPENAPI", "path must be an object")
		}
		for method, opRaw := range item {
			switch method {
			case "get", "post", "put", "patch", "delete", "head", "options":
			default:
				continue
			}
			spec := object(opRaw)
			if spec == nil {
				return e, failure("INVALID_OPENAPI", "operation must be an object")
			}
			id := text(spec["operationId"])
			if id == "" {
				return e, failure("INVALID_OPENAPI", "Missing operationId")
			}
			if _, exists := e.operations[id]; exists {
				return e, failure("INVALID_OPENAPI", "Duplicate operationId")
			}
			op := operation{method: strings.ToUpper(method), path: path, params: map[string]parameter{}}
			merged := map[string]map[string]any{}
			for _, listRaw := range []any{item["parameters"], spec["parameters"]} {
				if listRaw == nil {
					continue
				}
				list, ok := listRaw.([]any)
				if !ok {
					return e, failure("INVALID_OPENAPI", "parameters must be an array")
				}
				for _, v := range list {
					p := object(v)
					merged[text(p["in"])+":"+text(p["name"])] = p
				}
			}
			headers := map[string]bool{}
			for _, p := range merged {
				name, loc := text(p["name"]), text(p["in"])
				schema := object(p["schema"])
				if name == "" || schema == nil {
					return e, failure("INVALID_OPENAPI", "Parameter name/schema required")
				}
				if _, exists := op.params[name]; exists {
					return e, failure("UNSUPPORTED_OPENAPI", "Ambiguous parameter name")
				}
				switch text(schema["type"]) {
				case "string", "integer", "number", "boolean":
				default:
					return e, failure("UNSUPPORTED_OPENAPI", "Only scalar parameters supported")
				}
				style := "simple"
				switch loc {
				case "path":
				case "query":
					style = "form"
				case "header":
					if blockedHeader(name) || headers[strings.ToLower(name)] {
						return e, failure("UNSUPPORTED_OPENAPI", "Reserved or duplicated header")
					}
					headers[strings.ToLower(name)] = true
				default:
					return e, failure("UNSUPPORTED_OPENAPI", "Parameter location unsupported")
				}
				if (p["style"] != nil && p["style"] != style) || boolean(p["allowReserved"]) {
					return e, failure("UNSUPPORTED_OPENAPI", "Parameter serialization unsupported")
				}
				validator, err := compileSchema(schema, version)
				if err != nil {
					return e, err
				}
				op.params[name] = parameter{name, loc, boolean(p["required"]) || loc == "path", validator}
			}
			for _, match := range placeholder.FindAllStringSubmatch(path, -1) {
				if op.params[match[1]].location != "path" {
					return e, failure("INVALID_OPENAPI", "Undeclared path parameter")
				}
			}
			if strings.ContainsAny(placeholder.ReplaceAllString(path, ""), "{}") {
				return e, failure("INVALID_OPENAPI", "Malformed path template")
			}
			if rb, exists := spec["requestBody"]; exists {
				body := object(rb)
				media := object(object(body["content"])["application/json"])
				schema, ok := media["schema"]
				if !ok {
					return e, failure("UNSUPPORTED_OPENAPI", "Only JSON request bodies supported")
				}
				if method == "get" || method == "head" {
					return e, failure("UNSUPPORTED_OPENAPI", "GET/HEAD body unsupported")
				}
				op.body, err = compileSchema(schema, version)
				if err != nil {
					return e, err
				}
				op.bodyRequired = boolean(body["required"])
			}
			e.operations[id] = op
		}
	}
	return e, nil
}

// resolve expands only document-local refs and rejects cycles and external fetches.
func resolve(v any, root map[string]any, stack []string, depth int) (any, error) {
	if depth > 80 {
		return nil, failure("INVALID_OPENAPI", "Reference nesting too deep")
	}
	switch x := v.(type) {
	case []any:
		out := make([]any, len(x))
		for i, item := range x {
			r, e := resolve(item, root, stack, depth+1)
			if e != nil {
				return nil, e
			}
			out[i] = r
		}
		return out, nil
	case map[string]any:
		if value, ok := x["$ref"]; ok {
			ref := text(value)
			if !strings.HasPrefix(ref, "#/") {
				return nil, failure("UNSUPPORTED_OPENAPI", "Only local references supported")
			}
			for _, s := range stack {
				if s == ref {
					return nil, failure("UNSUPPORTED_OPENAPI", "Recursive references unsupported")
				}
			}
			var target any = root
			for _, key := range strings.Split(ref[2:], "/") {
				key = strings.ReplaceAll(strings.ReplaceAll(key, "~1", "/"), "~0", "~")
				m := object(target)
				var exists bool
				target, exists = m[key]
				if !exists {
					return nil, failure("INVALID_OPENAPI", "Unresolved reference")
				}
			}
			copy := map[string]any{}
			for k, v := range object(target) {
				copy[k] = v
			}
			for k, v := range x {
				if k != "$ref" {
					copy[k] = v
				}
			}
			return resolve(copy, root, append(append([]string{}, stack...), ref), depth+1)
		}
		copy := map[string]any{}
		for k, item := range x {
			r, e := resolve(item, root, stack, depth+1)
			if e != nil {
				return nil, e
			}
			copy[k] = r
		}
		return copy, nil
	default:
		return v, nil
	}
}

type noRemote struct{}

func (noRemote) Load(string) (any, error) {
	return nil, failure("UNSUPPORTED_OPENAPI", "External schema fetch disabled")
}
func compileSchema(v any, version string) (*jsonschema.Schema, error) {
	normalized, err := normalizeSchema(v, strings.HasPrefix(version, "3.0."))
	if err != nil {
		return nil, err
	}
	c := jsonschema.NewCompiler()
	c.UseLoader(noRemote{})
	c.AssertFormat()
	if strings.HasPrefix(version, "3.0.") {
		c.DefaultDraft(jsonschema.Draft7)
	} else {
		c.DefaultDraft(jsonschema.Draft2020)
	}
	if err = c.AddResource("https://schema.invalid/request", normalized); err != nil {
		return nil, failure("INVALID_OPENAPI", "Invalid schema")
	}
	schema, err := c.Compile("https://schema.invalid/request")
	if err != nil {
		return nil, failure("INVALID_OPENAPI", "Schema compilation failed: "+err.Error())
	}
	return schema, nil
}

// Normalize only schema positions, never example/default values or property names.
func normalizeSchema(v any, legacy bool) (any, error) {
	if b, ok := v.(bool); ok {
		return b, nil
	}
	m := object(v)
	if m == nil {
		return nil, failure("INVALID_OPENAPI", "Schema must be an object or boolean")
	}
	out := map[string]any{}
	for k, value := range m {
		switch k {
		case "$id", "$schema", "$dynamicRef", "$dynamicAnchor":
			return nil, failure("UNSUPPORTED_OPENAPI", "Schema keyword unsupported: "+k)
		case "properties", "patternProperties", "$defs", "definitions", "dependentSchemas":
			props := object(value)
			if props == nil {
				return nil, failure("INVALID_OPENAPI", "Invalid schema map")
			}
			n := map[string]any{}
			for name, s := range props {
				child, e := normalizeSchema(s, legacy)
				if e != nil {
					return nil, e
				}
				n[name] = child
			}
			out[k] = n
		case "items", "additionalProperties", "not", "if", "then", "else", "contains", "propertyNames", "unevaluatedProperties", "unevaluatedItems":
			n, e := normalizeSchema(value, legacy)
			if e != nil {
				return nil, e
			}
			out[k] = n
		case "allOf", "anyOf", "oneOf", "prefixItems":
			list, ok := value.([]any)
			if !ok {
				return nil, failure("INVALID_OPENAPI", "Invalid schema array")
			}
			n := []any{}
			for _, s := range list {
				child, e := normalizeSchema(s, legacy)
				if e != nil {
					return nil, e
				}
				n = append(n, child)
			}
			out[k] = n
		default:
			allowed := " type enum const required minimum maximum exclusiveMinimum exclusiveMaximum multipleOf minLength maxLength pattern format minItems maxItems uniqueItems minContains maxContains minProperties maxProperties dependentRequired title description default examples example deprecated readOnly writeOnly nullable discriminator xml externalDocs "
			if !strings.Contains(allowed, " "+k+" ") && !strings.HasPrefix(k, "x-") {
				return nil, failure("UNSUPPORTED_OPENAPI", "Schema keyword unsupported: "+k)
			}
			out[k] = value
		}
	}
	if legacy {
		delete(out, "nullable")
		if boolean(m["nullable"]) {
			if t, ok := m["type"].(string); ok {
				out["type"] = []any{t, "null"}
			}
		}
		for _, pair := range [][2]string{{"exclusiveMinimum", "minimum"}, {"exclusiveMaximum", "maximum"}} {
			if b, ok := m[pair[0]].(bool); ok {
				delete(out, pair[0])
				if b {
					bound, exists := out[pair[1]]
					if !exists {
						return nil, failure("INVALID_OPENAPI", "Exclusive bound missing")
					}
					out[pair[0]] = bound
					delete(out, pair[1])
				}
			}
		}
	}
	return out, nil
}
func (c *Controller) Send(parent context.Context, a Arguments) (Result, error) {
	e, ok := c.entries[a.ServiceID]
	if !ok {
		return Result{}, failure("SERVICE_NOT_FOUND", "Unknown service")
	}
	op, ok := e.operations[a.OperationID]
	if !ok {
		return Result{}, failure("OPERATION_NOT_FOUND", "Unknown operationId")
	}
	for k := range a.Parameters {
		if _, ok := op.params[k]; !ok {
			return Result{}, failure("INVALID_PARAMETERS", "Unknown parameter: "+k)
		}
	}
	path := op.path
	query := url.Values{}
	headers := http.Header{"Accept": []string{"application/json"}}
	for name, p := range op.params {
		value, present := a.Parameters[name]
		if !present {
			if p.required {
				return Result{}, failure("INVALID_PARAMETERS", "Missing parameter: "+name)
			}
			continue
		}
		if value == nil || p.schema.Validate(value) != nil {
			return Result{}, failure("INVALID_PARAMETERS", "Invalid parameter: "+name)
		}
		str := fmt.Sprint(value)
		switch p.location {
		case "path":
			if str == "." || str == ".." || strings.ContainsAny(str, "/\\%\r\n") {
				return Result{}, failure("INVALID_PARAMETERS", "Invalid path value")
			}
			path = strings.ReplaceAll(path, "{"+name+"}", url.PathEscape(str))
		case "query":
			query.Set(name, str)
		case "header":
			if strings.ContainsAny(str, "\r\n") {
				return Result{}, failure("INVALID_PARAMETERS", "Invalid header")
			}
			headers.Set(name, str)
		}
	}
	if op.bodyRequired && len(a.Body) == 0 {
		return Result{}, failure("INVALID_REQUEST_BODY", "Required body missing")
	}
	if len(a.Body) > Limit {
		return Result{}, failure("REQUEST_TOO_LARGE", "Body exceeds 1 MiB")
	}
	if len(a.Body) > 0 {
		if op.body == nil {
			return Result{}, failure("INVALID_REQUEST_BODY", "Operation declares no body")
		}
		var value any
		if json.Unmarshal(a.Body, &value) != nil || op.body.Validate(value) != nil {
			return Result{}, failure("INVALID_REQUEST_BODY", "Body does not match schema")
		}
		headers.Set("Content-Type", "application/json")
	}
	base, _ := url.Parse(e.config.BaseURL)
	rawPath := strings.TrimRight(base.EscapedPath(), "/") + path
	target, err := url.Parse(base.Scheme + "://" + base.Host + rawPath)
	if err != nil || target.Host != base.Host || target.EscapedPath() != rawPath {
		return Result{}, failure("TARGET_NOT_ALLOWED", "Invalid target path")
	}
	target.RawQuery = query.Encode()
	ctx, cancel := context.WithTimeout(parent, time.Duration(e.config.TimeoutMS)*time.Millisecond)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, op.method, target.String(), bytes.NewReader(a.Body))
	if err != nil {
		return Result{}, failure("INVALID_PARAMETERS", "Cannot build request")
	}
	req.Header = headers
	res, err := c.client.Do(req)
	if err != nil {
		return Result{}, networkError(err, ctx)
	}
	defer res.Body.Close()
	if res.StatusCode >= 300 && res.StatusCode < 400 {
		return Result{}, failure("TARGET_NOT_ALLOWED", "Redirects are not followed")
	}
	var data any
	if res.StatusCode != 204 && op.method != "HEAD" {
		media, _, _ := mime.ParseMediaType(res.Header.Get("Content-Type"))
		if media != "application/json" && !(strings.HasPrefix(media, "application/") && strings.HasSuffix(media, "+json")) {
			return Result{}, failure("INVALID_RESPONSE", "Expected JSON")
		}
		b, err := bounded(res.Body)
		if err != nil {
			var te *Error
			if errors.As(err, &te) {
				return Result{}, err
			}
			return Result{}, networkError(err, ctx)
		}
		if json.Unmarshal(b, &data) != nil {
			return Result{}, failure("INVALID_RESPONSE", "Malformed JSON response")
		}
	}
	m := object(data)
	message := text(m["user_message"])
	if message == "" {
		message = text(m["message"])
	}
	if message == "" {
		message = fmt.Sprintf("HTTP %d", res.StatusCode)
	}
	result := Result{OK: res.StatusCode >= 200 && res.StatusCode < 300 && m["ok"] != false, Status: res.StatusCode, Data: data, Message: message}
	if !result.OK {
		result.Error = &ResultError{"APP_ERROR", message}
	}
	return result, nil
}
func networkError(err error, ctx context.Context) error {
	if errors.Is(ctx.Err(), context.DeadlineExceeded) || errors.Is(err, context.DeadlineExceeded) {
		return failure("TIMEOUT", "Application request timed out")
	}
	return failure("UPSTREAM_ERROR", "Application request failed")
}

// FromDocument compiles a registered artifact without fetching a remote specification.
func FromDocument(id, baseURL string, document []byte) (*Controller, error) {
	return FromDocumentWithTimeout(id, baseURL, document, 10000)
}

// FromDocumentWithTimeout is FromDocument with an explicit per-request timeout.
// A non-positive timeout falls back to ten seconds.
func FromDocumentWithTimeout(id, baseURL string, document []byte, timeoutMS int) (*Controller, error) {
	u, err := url.Parse(baseURL)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.RawQuery != "" || u.Fragment != "" || !safePath(u.Path) || strings.Contains(u.EscapedPath(), "%") {
		return nil, failure("INVALID_CONFIG", "Invalid application base URL")
	}
	switch {
	case timeoutMS <= 0:
		timeoutMS = 10000
	case timeoutMS > 120000:
		timeoutMS = 120000
	}
	e, err := compileDocument(Service{ID: id, BaseURL: baseURL, TimeoutMS: timeoutMS}, document)
	if err != nil {
		return nil, err
	}
	return &Controller{entries: map[string]entry{id: e}, client: &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}, nil
}

// ExpandedDocument resolves the supported local reference subset for operation views.
func ExpandedDocument(doc map[string]any) (map[string]any, error) {
	v, err := resolve(doc, doc, nil, 0)
	if err != nil {
		return nil, err
	}
	return object(v), nil
}
func (c *Controller) SetHTTPClient(client *http.Client) {
	if client == nil {
		return
	}
	copy := *client
	copy.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	c.client = &copy
}
