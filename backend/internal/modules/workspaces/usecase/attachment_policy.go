package usecase

import (
	"context"
	"crypto/sha256"
	"github.com/furkanmeclis/traccia/backend/internal/platform/operations"
	"github.com/google/uuid"
)

// CanRead rechecks the current product grant after bounded external byte reads.
func (s *Service) CanRead(ctx context.Context, w uuid.UUID) error {
	channel := operations.API
	if raw, ok := ctx.Value(credentialContextKey{}).(string); ok {
		hash := sha256.Sum256([]byte(raw))
		access, e := s.repo.ResolveCredential(ctx, hash[:])
		if e != nil {
			return e
		}
		channel = operations.Channel(access.Audience)
	}
	_, e := s.Authorize(ctx, operations.AuthorizationRequest{Operation: operations.Metadata{ID: "attachments.read", Effect: operations.Read, Permission: "workspace.read"}, WorkspaceID: w, Channel: channel})
	return e
}
