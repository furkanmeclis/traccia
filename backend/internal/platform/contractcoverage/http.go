package contractcoverage

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/furkanmeclis/traccia/backend/internal/platform/operations"
	validator "github.com/santhosh-tekuri/jsonschema/v6"
)

func object(properties Object, required ...string) Object {
	names := make([]any, 0, len(required))
	for _, name := range required {
		names = append(names, name)
	}
	return Object{"type": "object", "additionalProperties": false, "properties": properties, "required": names}
}
func responseMeta() Object {
	return object(Object{"request_id": Object{"type": "string"}}, "request_id")
}

// ErrorCodes describes the actual product writeError mapping and the two HTTP
// adapter exceptions. UNAUTHENTICATED is middleware-only; UNAUTHORIZED is domain.
func ErrorCodes(method string) map[string][]string {
	codes := map[string][]string{"400": {"VALIDATION_ERROR"}, "401": {"UNAUTHORIZED", "UNAUTHENTICATED"}, "403": {"FORBIDDEN"}, "404": {"NOT_FOUND"}, "408": {"CANCELED"}, "409": {"CONFLICT", "IDEMPOTENCY_CONFLICT", "VERSION_CONFLICT"}, "500": {"INTERNAL_ERROR"}, "503": {"OPERATION_UNAVAILABLE"}}
	if method == "POST" || method == "PUT" || method == "PATCH" {
		codes["415"] = []string{"UNSUPPORTED_MEDIA_TYPE"}
	}
	return codes
}

func errorEnvelope(codes []string) Object {
	values := make([]any, len(codes))
	for i, c := range codes {
		values[i] = c
	}
	detail := object(Object{"field": Object{"type": "string"}, "code": Object{"type": "string"}, "message": Object{"type": "string"}}, "message")
	body := object(Object{"code": Object{"type": "string", "enum": values}, "message": Object{"type": "string"}, "details": Object{"type": "array", "items": detail}}, "code", "message")
	return object(Object{"success": Object{"type": "boolean", "const": false}, "error": body, "meta": responseMeta()}, "success", "error", "meta")
}

func checkHTTP(m operations.Metadata, op, doc Object, input, output any, fail func(string, ...any), compare func(string, string, any, any, Object)) {
	id := m.ID
	parameters := map[string]Object{}
	list, _ := op["parameters"].([]any)
	for _, value := range list {
		p, ok := value.(Object)
		if !ok {
			fail("%s invalid HTTP parameter", id)
			continue
		}
		key := fmt.Sprint(p["in"]) + ":" + fmt.Sprint(p["name"])
		if parameters[key] != nil {
			fail("%s duplicate HTTP parameter %s", id, key)
		}
		parameters[key] = p
	}
	consume := func(key string) Object {
		p := parameters[key]
		delete(parameters, key)
		if p == nil {
			fail("%s missing HTTP parameter %s", id, key)
		}
		return p
	}
	for _, part := range strings.Split(m.HTTPPath, "/") {
		if strings.HasPrefix(part, "{") && strings.HasSuffix(part, "}") {
			name := strings.TrimSuffix(strings.TrimPrefix(part, "{"), "}")
			p := consume("path:" + name)
			expected := any(Object{"type": "string", "format": "uuid"})
			if name == "identifier" {
				expected = input.(Object)["properties"].(Object)["identifier"]
			}
			if p != nil && (p["required"] != true || Difference(expected, p["schema"], "parameter") != "") {
				fail("%s HTTP path parameter %s schema mismatch", id, name)
			}
		}
	}
	if m.Idempotent {
		p := consume("header:Idempotency-Key")
		if p != nil {
			expected, _ := Decode([]byte(`{"type":"string","minLength":1,"maxLength":200}`))
			if p["required"] != true || Difference(expected, p["schema"], "idempotency_key") != "" {
				fail("%s HTTP idempotency_key schema mismatch", id)
			}
		}
	}
	expectedBinding := any("empty")
	pathFields := operations.HTTPPathFields(m)
	bindings := Object{}
	for field, name := range pathFields {
		bindings[field] = "path:" + name
	}
	props, _ := input.(Object)["properties"].(Object)
	if m.HTTPMethod == "POST" || m.HTTPMethod == "PUT" || m.HTTPMethod == "PATCH" {
		expectedBinding = "body"
		expectedBody := input
		if len(pathFields) > 0 {
			raw, _ := json.Marshal(input)
			clone, _ := Decode(raw)
			omitHTTPBoundFields(clone, pathFields)
			expectedBody = clone
			bindings["body"] = "body"
			expectedBinding = bindings
		}
		body, _ := op["requestBody"].(Object)
		if body["required"] != true {
			fail("%s HTTP body must be required", id)
		}
		compare(id, "HTTP/body", expectedBody, contentSchema(body), doc)
	} else {
		if op["requestBody"] != nil {
			fail("%s unexpected HTTP body", id)
		}
		for name, property := range props {
			if _, ok := pathFields[name]; ok {
				continue
			}
			if m.HTTPMethod != "GET" {
				fail("%s unmapped input %s", id, name)
				continue
			}
			bindings[name] = "query:" + name
			p := consume("query:" + name)
			if p != nil {
				if p["required"] == true || Difference(property, p["schema"], name) != "" {
					fail("%s HTTP query %s schema mismatch", id, name)
				}
				want := ""
				if name == "limit" {
					want = "20"
				} else if property.(Object)["type"] == "boolean" {
					want = "false"
				}
				if fmt.Sprint(p["x-binding-default"]) != want {
					fail("%s HTTP query %s default mismatch", id, name)
				}
			}
		}
		if len(bindings) > 0 {
			expectedBinding = bindings
		}
	}
	if d := Difference(expectedBinding, op["x-traccia-input-binding"], "binding"); d != "" {
		fail("%s HTTP input mapping mismatch %s", id, d)
	}
	for key := range parameters {
		fail("%s unclassified HTTP parameter %s", id, key)
	}
	responses, _ := op["responses"].(Object)
	success := "200"
	if m.HTTPMethod == "POST" {
		success = "201"
	}
	expectedSuccess := object(Object{"success": Object{"type": "boolean", "const": true}, "data": output, "meta": responseMeta()}, "success", "data", "meta")
	normalized, _ := Normalize(expectedSuccess, expectedSuccess)
	compare(id, "HTTP/response/"+success, normalized, responseSchema(responses[success], doc), doc)
	statuses := ErrorCodes(m.HTTPMethod)
	for status, codes := range statuses {
		expected := errorEnvelope(codes)
		normalized, _ := Normalize(expected, expected)
		compare(id, "HTTP/response/"+status, normalized, responseSchema(responses[status], doc), doc)
	}
	for status := range responses {
		if status != success && statuses[status] == nil {
			fail("%s undocumented implementation status %s", id, status)
		}
	}
}

func contentSchema(value Object) any {
	content, _ := value["content"].(Object)
	media, _ := content["application/json"].(Object)
	return media["schema"]
}
func responseSchema(value any, doc Object) any {
	response, _ := value.(Object)
	if ref, ok := response["$ref"].(string); ok {
		resolved, err := Pointer(doc, ref)
		if err != nil {
			return nil
		}
		response, _ = resolved.(Object)
	}
	return contentSchema(response)
}

// ValidateHTTP validates an actual wire envelope against its documented status.
// The checker separately proves its success payload/error schemas match contracts.
func ValidateHTTP(doc Object, id string, status int, raw []byte) error {
	paths, _ := doc["paths"].(Object)
	for _, item := range paths {
		methods, _ := item.(Object)
		for _, value := range methods {
			op, _ := value.(Object)
			if op["x-operation-id"] != id {
				continue
			}
			responses, _ := op["responses"].(Object)
			schema := responseSchema(responses[fmt.Sprint(status)], doc)
			normalized, err := Normalize(schema, doc)
			if err != nil {
				return err
			}
			return Validate(normalized, raw)
		}
	}
	return fmt.Errorf("%s: operation missing from OpenAPI", id)
}

func Validate(schema any, raw []byte) error {
	document, ok := schema.(Object)
	if !ok {
		return fmt.Errorf("concrete schema required")
	}
	compiler := validator.NewCompiler()
	if err := compiler.AddResource("https://traccia.invalid/coverage.json", document); err != nil {
		return err
	}
	compiled, err := compiler.Compile("https://traccia.invalid/coverage.json")
	if err != nil {
		return err
	}
	value, err := operations.DecodeJSON(raw)
	if err != nil {
		return err
	}
	return compiled.Validate(value)
}

// Composition branches constrain the same JSON instance. Path-owned fields
// must be removed there as well, without changing nested property instances.
func omitHTTPBoundFields(s Object, fields map[string]string) {
	if props, ok := s["properties"].(Object); ok {
		for field := range fields {
			delete(props, field)
		}
	}
	if required, ok := s["required"].([]any); ok {
		kept := []any{}
		for _, field := range required {
			if _, bound := fields[fmt.Sprint(field)]; !bound {
				kept = append(kept, field)
			}
		}
		s["required"] = kept
	}
	for _, key := range []string{"allOf", "anyOf", "oneOf"} {
		if branches, ok := s[key].([]any); ok {
			for _, branch := range branches {
				if obj, ok := branch.(Object); ok {
					omitHTTPBoundFields(obj, fields)
				}
			}
		}
	}
}
