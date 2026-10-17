package contractcoverage

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/furkanmeclis/traccia/backend/internal/platform/operations"
	"gopkg.in/yaml.v3"
)

type Route struct{ OperationID, Method, Path string }
type Tool struct {
	Name                 string
	Input, Output, Error any
	OperationID          string
	Version              int
}
type Exclusion struct {
	Method string `json:"method"`
	Path   string `json:"path"`
	Reason string `json:"reason"`
}
type Surfaces struct {
	Contracts  []operations.Contract
	Routes     []Route
	Tools      []Tool
	OpenAPI    Object
	Exclusions []Exclusion
}

func ReadOpenAPI(path string) (Object, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var doc Object
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return nil, err
	}
	encoded, err := json.Marshal(doc)
	if err != nil {
		return nil, err
	}
	return Decode(encoded)
}

func ReadExclusions(path string) ([]Exclusion, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var out []Exclusion
	err = json.Unmarshal(raw, &out)
	return out, err
}

// Check requires the observed product routes, discovered tools, and documented
// operations to be bijections with the application registry. Exclusions identify
// pre-product platform HTTP operations only and cannot hide product annotations.
func Check(s Surfaces) []string {
	var failures []string
	fail := func(format string, args ...any) { failures = append(failures, fmt.Sprintf(format, args...)) }
	routes := map[string]Route{}
	for _, route := range s.Routes {
		if _, ok := routes[route.OperationID]; ok {
			fail("%s HTTP duplicate operation", route.OperationID)
		}
		routes[route.OperationID] = route
	}
	tools := map[string]Tool{}
	for _, tool := range s.Tools {
		if _, ok := tools[tool.Name]; ok {
			fail("%s MCP duplicate tool", tool.Name)
		}
		tools[tool.Name] = tool
	}
	catalog := map[string]operations.Contract{}
	for _, contract := range s.Contracts {
		catalog[contract.Metadata.ID] = contract
	}
	for _, route := range s.Routes {
		if _, ok := catalog[route.OperationID]; !ok {
			fail("%s HTTP operation absent from catalog", route.OperationID)
		}
	}
	names := map[string]bool{}
	for _, c := range s.Contracts {
		names[c.Metadata.MCPName] = true
	}
	for _, tool := range s.Tools {
		if !names[tool.Name] {
			fail("%s MCP operation absent from catalog", tool.Name)
		}
	}
	exclusions := map[string]bool{}
	for _, e := range s.Exclusions {
		key := e.Method + " " + e.Path
		if strings.TrimSpace(e.Reason) == "" {
			fail("%s exclusion requires reason", key)
		}
		if exclusions[key] {
			fail("%s duplicate exclusion", key)
		}
		exclusions[key] = true
	}
	paths, _ := s.OpenAPI["paths"].(Object)
	documented := map[string]Object{}
	usedExclusions := map[string]bool{}
	for path, item := range paths {
		methods, ok := item.(Object)
		if !ok {
			fail("%s invalid OpenAPI path item", path)
			continue
		}
		for method, value := range methods {
			upper := strings.ToUpper(method)
			if !isMethod(upper) {
				continue
			}
			op, ok := value.(Object)
			if !ok {
				fail("%s %s invalid OpenAPI operation", upper, path)
				continue
			}
			id, _ := op["x-operation-id"].(string)
			key := upper + " " + path
			if id == "" {
				if exclusions[key] {
					usedExclusions[key] = true
				} else {
					fail("%s unclassified HTTP operation; add product contract or justified legacy exclusion", key)
				}
				continue
			}
			if exclusions[key] {
				fail("%s product operation cannot be excluded", id)
			}
			if _, ok := catalog[id]; !ok {
				fail("%s OpenAPI operation absent from catalog", id)
			}
			if _, ok := documented[id]; ok {
				fail("%s duplicate OpenAPI operation", id)
			}
			// Keep observed method/path separate from descriptive operation metadata.
			clone := Object{}
			for k, v := range op {
				clone[k] = v
			}
			clone["observed_method"] = upper
			clone["observed_path"] = path
			documented[id] = clone
		}
	}
	for key := range exclusions {
		if !usedExclusions[key] {
			fail("%s stale or product exclusion", key)
		}
	}
	compare := func(id, label string, expected any, actual any, root Object) {
		base := operations.SchemaBaseURI
		if label == "MCP/input" {
			base = "https://traccia.invalid/mcp.schema.json"
		}
		normalized, err := normalizeAt(actual, root, base)
		if err != nil {
			fail("%s %s: %v", id, label, err)
			return
		}
		if err := Closed(normalized, label); err != nil {
			fail("%s %v", id, err)
			return
		}
		if d := Difference(expected, normalized, label); d != "" {
			fail("%s schema mismatch %s", id, d)
		}
	}
	for _, contract := range s.Contracts {
		m := contract.Metadata
		id := m.ID
		if !contract.Available {
			fail("%s handler unavailable", id)
		}
		in, err := Decode(contract.Input)
		if err != nil {
			fail("%s input schema: %v", id, err)
			continue
		}
		input, err := Normalize(in, in)
		if err != nil {
			fail("%s input schema: %v", id, err)
			continue
		}
		out, err := Decode(contract.Output)
		if err != nil {
			fail("%s output schema: %v", id, err)
			continue
		}
		output, err := Normalize(out, out)
		if err != nil {
			fail("%s output schema: %v", id, err)
			continue
		}
		er, err := Decode(contract.Error)
		if err != nil {
			fail("%s error schema: %v", id, err)
			continue
		}
		domainError, err := Normalize(er, er)
		if err != nil {
			fail("%s error schema: %v", id, err)
			continue
		}
		for label, schema := range map[string]any{"input": input, "output": output, "error": domainError} {
			if err := Closed(schema, label); err != nil {
				fail("%s catalog %v", id, err)
			}
		}
		route, ok := routes[id]
		if !ok {
			fail("%s missing HTTP operation", id)
		} else if route.Method != m.HTTPMethod || route.Path != m.HTTPPath {
			fail("%s HTTP method/path mismatch: %s %s", id, route.Method, route.Path)
		}
		tool, ok := tools[m.MCPName]
		if !ok {
			fail("%s missing MCP operation %s", id, m.MCPName)
		} else {
			if tool.OperationID != id || tool.Version != m.Version {
				fail("%s MCP identity/version mismatch", id)
			}
			wrapper, ok := tool.Input.(Object)
			if !ok {
				fail("%s MCP input wrapper missing", id)
			} else {
				expected := Object{"type": "object", "additionalProperties": false, "properties": Object{"input": input}, "required": []any{"input"}}
				props := expected["properties"].(Object)
				required := expected["required"].([]any)
				if m.Scope == operations.WorkspaceScope {
					props["workspace_id"] = Object{"type": "string", "format": "uuid"}
					required = append(required, "workspace_id")
				}
				if m.Idempotent {
					props["idempotency_key"] = Object{"type": "string", "minLength": json.Number("1"), "maxLength": json.Number("200")}
					required = append(required, "idempotency_key")
				}
				expected["required"] = required
				normalized, _ := Normalize(expected, expected)
				compare(id, "MCP/input", normalized, wrapper, wrapper)
			}
			compare(id, "MCP/output", output, tool.Output, asObject(tool.Output))
			compare(id, "MCP/error", domainError, tool.Error, asObject(tool.Error))
		}
		op, ok := documented[id]
		if !ok {
			fail("%s missing OpenAPI operation", id)
			continue
		}
		if op["observed_method"] != m.HTTPMethod || op["observed_path"] != m.HTTPPath {
			fail("%s OpenAPI method/path mismatch", id)
		}
		if op["operationId"] != strings.ReplaceAll(id, ".", "_") || fmt.Sprint(op["x-operation-version"]) != fmt.Sprint(m.Version) {
			fail("%s OpenAPI identity/version mismatch", id)
		}
		compare(id, "HTTP/input", input, op["x-traccia-input-schema"], s.OpenAPI)
		compare(id, "HTTP/error", domainError, op["x-traccia-error-schema"], s.OpenAPI)
		checkHTTP(m, op, s.OpenAPI, input, output, fail, compare)
	}
	sort.Strings(failures)
	return failures
}

func asObject(value any) Object { object, _ := value.(Object); return object }
func isMethod(method string) bool {
	switch method {
	case "GET", "POST", "PUT", "PATCH", "DELETE", "HEAD", "OPTIONS", "TRACE":
		return true
	}
	return false
}
