package usecase

import (
	"context"
	"errors"
	"github.com/furkanmeclis/traccia/backend/internal/platform/dbtx"
	"github.com/furkanmeclis/traccia/backend/internal/platform/operations"
	"github.com/google/jsonschema-go/jsonschema"
)

// CreateInTransaction composes rich issue creation with the document service.
// It preserves the existing metadata create behavior and requires the caller's
// already-authorized shared mutation transaction.
func (s *Service) CreateInTransaction(ctx context.Context, scope operations.Scope, in CreateInput) (Output, error) {
	if _, ok := dbtx.Tx(ctx); !ok {
		return Output{}, errors.New("issue composition requires mutation transaction")
	}
	return s.create(ctx, scope, in)
}
func ValidateCreateInput(in CreateInput) []operations.FieldError { return valid(in) }
func RefineCreateSchema(input *jsonschema.Schema) error          { return refine(input, &jsonschema.Schema{}) }
