package usecase

import (
	"encoding/json"
	plate "github.com/furkanmeclis/traccia/backend/internal/modules/documents/usecase"
	"github.com/furkanmeclis/traccia/backend/internal/platform/operations"
	"strings"
)

// ValidateContent retains the shared Plate rules and rejects PostgreSQL's
// unsupported NUL character explicitly instead of losing data during storage.
func ValidateContent(content []plate.Node) []operations.FieldError {
	if errors := plate.ValidateContent(content); len(errors) > 0 {
		return errors
	}
	raw, e := json.Marshal(content)
	if e != nil {
		return field("content", "invalid")
	}
	var value any
	if e = json.Unmarshal(raw, &value); e != nil {
		return field("content", "invalid")
	}
	var containsNUL func(any) bool
	containsNUL = func(v any) bool {
		switch x := v.(type) {
		case string:
			return strings.ContainsRune(x, 0)
		case []any:
			for _, c := range x {
				if containsNUL(c) {
					return true
				}
			}
		case map[string]any:
			for _, c := range x {
				if containsNUL(c) {
					return true
				}
			}
		}
		return false
	}
	if containsNUL(value) {
		return field("content", "unsupported_character")
	}
	return nil
}
