package usecase

import (
	"encoding/json"
	"github.com/furkanmeclis/traccia/backend/internal/platform/operations"
	"github.com/google/jsonschema-go/jsonschema"
)

// DefinitionSchema returns a fresh closed query-definition schema and its local
// definitions for additive consumers. It does not change released operations.
func DefinitionSchema() (*jsonschema.Schema, error) {
	s, e := jsonschema.For[ViewQuery](nil)
	if e != nil {
		return nil, e
	}
	root := &jsonschema.Schema{Type: "object", Properties: map[string]*jsonschema.Schema{"definition": s}}
	if e = refine("")(root, &jsonschema.Schema{}); e != nil {
		return nil, e
	}
	result := root.Properties["definition"]
	result.Defs = root.Defs
	return result, nil
}

// SnapshotRowSchema returns a fresh metadata-only schema for a known resource.
func SnapshotRowSchema(resource string) (*jsonschema.Schema, error) {
	var s *jsonschema.Schema
	var e error
	switch resource {
	case "issue":
		s, e = jsonschema.For[IssueRow](nil)
	case "project":
		s, e = jsonschema.For[ProjectRow](nil)
	case "team":
		s, e = jsonschema.For[TeamRow](nil)
	default:
		return nil, &operations.Error{Code: operations.Validation}
	}
	if e != nil {
		return nil, e
	}
	if e = refine(resource)(s, &jsonschema.Schema{}); e != nil {
		return nil, e
	}
	return s, nil
}

// ValidateQueryWire applies the same bounded pure query traversal and canonical
// byte accounting to a new consumer without widening existing DTO hooks.
func ValidateQueryWire(value any, raw json.RawMessage) []operations.FieldError {
	return wireLimits(value, raw)
}
