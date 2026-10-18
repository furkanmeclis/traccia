package usecase

import (
	"encoding/json"
	plate "github.com/furkanmeclis/traccia/backend/internal/modules/documents/usecase"
	"github.com/furkanmeclis/traccia/backend/internal/platform/operations"
)

func (CreateInput) ValidateWire(v any, raw json.RawMessage) []operations.FieldError {
	return plate.ValidatePlateWire(v, raw)
}
func (ReplaceInput) ValidateWire(v any, raw json.RawMessage) []operations.FieldError {
	return plate.ValidatePlateWire(v, raw)
}
func (PatchInput) ValidateWire(v any, raw json.RawMessage) []operations.FieldError {
	return plate.ValidatePlateWire(v, raw)
}
