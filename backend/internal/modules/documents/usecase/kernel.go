package usecase

import (
	"encoding/json"
	"github.com/furkanmeclis/traccia/backend/internal/platform/operations"
	"github.com/google/jsonschema-go/jsonschema"
)

// RefinePlateContent injects a fresh closed Plate schema graph into another
// aggregate without imposing document ownership or revision metadata.
func RefinePlateContent(root *jsonschema.Schema) {
	if root.Defs == nil {
		root.Defs = map[string]*jsonschema.Schema{}
	}
	for name, value := range plateDefinitions() {
		root.Defs[name] = assignSchema(value)
	}
	var visit func(*jsonschema.Schema)
	visit = func(s *jsonschema.Schema) {
		if s == nil {
			return
		}
		for key, child := range s.Properties {
			switch key {
			case "content":
				if child.Type != "string" {
					s.Properties[key] = assignSchema(arraySchema(schemaRef("PlateBlock"), 1, MaxNodes))
					continue
				}
			case "patches":
				s.Properties[key] = assignSchema(arraySchema(schemaRef("PlatePatch"), 1, 100))
				continue
			}
			visit(child)
		}
		if s.Properties["schema_version"] != nil {
			s.Properties["schema_version"] = assignSchema(schemaObject{"type": "integer", "const": SchemaVersion})
			if s.Properties["format"] != nil {
				s.Properties["format"] = assignSchema(schemaObject{"type": "string", "const": Format})
			}
		}
		visit(s.Items)
		for _, c := range s.AnyOf {
			visit(c)
		}
		for _, c := range s.OneOf {
			visit(c)
		}
	}
	visit(root)
	for name, definition := range root.Defs {
		if len(name) < 5 || name[:5] != "Plate" {
			visit(definition)
		}
	}
}

// ValidatePlateWire applies the same bounded preflight as document writes.
func ValidatePlateWire(value any, raw json.RawMessage) []operations.FieldError {
	return wireLimits(value, raw)
}
