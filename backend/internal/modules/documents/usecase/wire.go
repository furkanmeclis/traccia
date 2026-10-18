package usecase

import (
	"encoding/json"
	"github.com/furkanmeclis/traccia/backend/internal/platform/operations"
)

// Bound recursive instance traversal before the shared schema validator follows
// references. Budgets are semantic checks, never invented schema keywords.
func wireLimits(value any, raw json.RawMessage) []operations.FieldError {
	if len(raw) > 4<<20 {
		return field("input", "bytes_limit")
	}
	count := 0
	var walk func(any, int) []operations.FieldError
	walk = func(value any, depth int) []operations.FieldError {
		if depth > MaxDepth+8 {
			return field("content", "depth_limit")
		}
		switch node := value.(type) {
		case map[string]any:
			if _, ok := node["type"]; ok {
				count++
			} else if _, ok := node["text"]; ok {
				count++
			}
			if count > MaxNodes {
				return field("content", "node_limit")
			}
			for _, child := range node {
				if errors := walk(child, depth+1); len(errors) > 0 {
					return errors
				}
			}
		case []any:
			for _, child := range node {
				if errors := walk(child, depth); len(errors) > 0 {
					return errors
				}
			}
		}
		return nil
	}
	return walk(value, 0)
}
func (CreateInput) ValidateWire(value any, raw json.RawMessage) []operations.FieldError {
	return wireLimits(value, raw)
}
func (ReplaceInput) ValidateWire(value any, raw json.RawMessage) []operations.FieldError {
	return wireLimits(value, raw)
}
func (PatchInput) ValidateWire(value any, raw json.RawMessage) []operations.FieldError {
	return wireLimits(value, raw)
}
func (RichCreateInput) ValidateWire(value any, raw json.RawMessage) []operations.FieldError {
	return wireLimits(value, raw)
}
