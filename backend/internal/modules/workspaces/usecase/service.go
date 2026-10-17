package usecase

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/furkanmeclis/traccia/backend/internal/platform/authctx"
	"github.com/furkanmeclis/traccia/backend/internal/platform/operations"
	"github.com/google/uuid"
)

const TokenPrefix = "trc_"

var grantableScopes = []string{"workspace.read", "workspace.write", "credentials.read", "credentials.manage"}

type Service struct{ repo Repository }

func New(repo Repository) *Service { return &Service{repo: repo} }

type credentialContextKey struct{}

// WithToolCredential passes untrusted bearer material to the common authorizer.
// It does not grant identity. The registry rechecks storage on each operation.
func WithToolCredential(ctx context.Context, raw string) context.Context {
	return context.WithValue(ctx, credentialContextKey{}, raw)
}
func denied(code operations.Code) error { return &operations.Error{Code: code} }

func (s *Service) access(ctx context.Context, workspace uuid.UUID, channel operations.Channel) (Access, error) {
	if raw, ok := ctx.Value(credentialContextKey{}).(string); ok {
		if !strings.HasPrefix(raw, TokenPrefix) || len(raw) != len(TokenPrefix)+43 {
			return Access{}, denied(operations.Unauthenticated)
		}
		hash := sha256.Sum256([]byte(raw))
		a, err := s.repo.ResolveCredential(ctx, hash[:])
		if err != nil {
			return Access{}, err
		}
		if a.WorkspaceID != workspace {
			return Access{}, denied(operations.Forbidden)
		}
		if string(channel) != a.Audience {
			return Access{}, denied(operations.Forbidden)
		}
		return a, nil
	}
	p, ok := authctx.PrincipalFrom(ctx)
	if !ok || p.UserID == uuid.Nil {
		return Access{}, denied(operations.Unauthenticated)
	}
	if workspace == uuid.Nil {
		if err := s.repo.ActiveActor(ctx, p.UserID); err != nil {
			return Access{}, err
		}
		return Access{UserID: p.UserID}, nil
	}
	return s.repo.Resolve(ctx, workspace, p.UserID)
}
func (s *Service) Authorize(ctx context.Context, req operations.AuthorizationRequest) (operations.Identity, error) {
	if req.Operation.ID == "workspaces.create" {
		if p, ok := authctx.PrincipalFrom(ctx); ok {
			if err := s.repo.ActiveOwner(ctx, p.UserID); err != nil {
				return operations.Identity{}, err
			}
		}
	}
	a, err := s.access(ctx, req.WorkspaceID, req.Channel)
	if err != nil {
		return operations.Identity{}, err
	}
	if a.Audience != "" && !slices.Contains(a.Scopes, req.Operation.Permission) {
		return operations.Identity{}, denied(operations.Forbidden)
	}
	if req.WorkspaceID != uuid.Nil && req.Operation.Effect == operations.Write && a.Role != "owner" {
		return operations.Identity{}, denied(operations.Forbidden)
	}
	if (strings.HasPrefix(req.Operation.Permission, "credentials.") || strings.HasPrefix(req.Operation.ID, "deliveries.")) && a.Role != "owner" {
		return operations.Identity{}, denied(operations.Forbidden)
	}
	if req.Operation.ID == "credentials.create" && a.Audience != "" {
		var input CreateCredentialInput
		if err := json.Unmarshal(req.Input, &input); err != nil {
			return operations.Identity{}, denied(operations.Validation)
		}
		if a.Audience != input.Audience || input.ExpiresAt.After(a.ExpiresAt) {
			return operations.Identity{}, denied(operations.Forbidden)
		}
		for _, grant := range input.Scopes {
			if !slices.Contains(a.Scopes, grant) {
				return operations.Identity{}, denied(operations.Forbidden)
			}
		}
	}
	return operations.Identity{ActorID: a.UserID, WorkspaceID: req.WorkspaceID}, nil
}

func validateName(name string) []operations.FieldError {
	if name != strings.TrimSpace(name) || utf8.RuneCountInString(name) < 1 || utf8.RuneCountInString(name) > 100 || strings.ContainsAny(name, "\x00\r\n") {
		return []operations.FieldError{{Field: "name", Code: "invalid"}}
	}
	return nil
}
func validatePage(in ListInput) []operations.FieldError {
	if in.Limit < 1 || in.Limit > 100 {
		return []operations.FieldError{{Field: "limit", Code: "out_of_range"}}
	}
	if in.After != "" {
		if _, err := uuid.Parse(in.After); err != nil {
			return []operations.FieldError{{Field: "after", Code: "invalid_cursor"}}
		}
	}
	return nil
}
func cursor(after string) uuid.UUID { id, _ := uuid.Parse(after); return id }

func (s *Service) create(ctx context.Context, scope operations.Scope, in CreateInput) (WorkspaceOutput, error) {
	w, err := s.repo.Create(ctx, scope.ActorID(), in.Name)
	if err != nil {
		return WorkspaceOutput{}, err
	}
	if err := s.repo.RecordWorkspaceEvent(ctx, scope.ActorID(), w, "workspaces.created"); err != nil {
		return WorkspaceOutput{}, err
	}
	return s.workspaceOutput(ctx, scope.ActorID(), w)
}
func (s *Service) list(ctx context.Context, scope operations.Scope, in ListInput) (WorkspaceList, error) {
	rows, err := s.repo.List(ctx, scope.ActorID(), cursor(in.After), in.Limit+1)
	out := WorkspaceList{Items: rows}
	if err == nil {
		ownerErr := s.repo.ActiveOwner(ctx, scope.ActorID())
		if ownerErr == nil {
			out.CanCreate = true
		} else {
			var known *operations.Error
			if !errors.As(ownerErr, &known) || known.Code != operations.Forbidden {
				return out, ownerErr
			}
		}
	}
	if len(rows) > int(in.Limit) {
		out.HasMore = true
		out.Items = rows[:in.Limit]
		out.NextCursor = out.Items[len(out.Items)-1].ID.String()
	}
	return out, err
}
func (s *Service) get(ctx context.Context, scope operations.Scope, _ EmptyInput) (WorkspaceOutput, error) {
	w, err := s.repo.Get(ctx, scope.WorkspaceID(), scope.ActorID())
	if err != nil {
		return WorkspaceOutput{}, err
	}
	return s.workspaceOutput(ctx, scope.ActorID(), w)
}
func (s *Service) update(ctx context.Context, scope operations.Scope, in UpdateInput) (WorkspaceOutput, error) {
	w, err := s.repo.Update(ctx, scope.WorkspaceID(), in.Name, in.ExpectedVersion)
	if err != nil {
		return WorkspaceOutput{}, err
	}
	if err := s.repo.RecordWorkspaceEvent(ctx, scope.ActorID(), w, "workspaces.updated"); err != nil {
		return WorkspaceOutput{}, err
	}
	return s.workspaceOutput(ctx, scope.ActorID(), w)
}
func validateCredential(in CreateCredentialInput) []operations.FieldError {
	if details := validateName(in.Name); len(details) > 0 {
		return details
	}
	if in.Audience != "api" && in.Audience != "mcp" {
		return []operations.FieldError{{Field: "audience", Code: "invalid"}}
	}
	if len(in.Scopes) < 1 || len(in.Scopes) > len(grantableScopes) {
		return []operations.FieldError{{Field: "scopes", Code: "invalid"}}
	}
	seen := map[string]bool{}
	for _, scope := range in.Scopes {
		if !slices.Contains(grantableScopes, scope) || seen[scope] {
			return []operations.FieldError{{Field: "scopes", Code: "invalid"}}
		}
		seen[scope] = true
	}
	if in.ExpiresAt.IsZero() {
		return []operations.FieldError{{Field: "expires_at", Code: "invalid"}}
	}
	return nil
}
func (s *Service) createCredential(ctx context.Context, scope operations.Scope, in CreateCredentialInput) (CreateCredentialOutput, error) {
	now := time.Now()
	if !in.ExpiresAt.After(now) || in.ExpiresAt.After(now.Add(365*24*time.Hour)) {
		return CreateCredentialOutput{}, &operations.Error{Code: operations.Validation, Details: []operations.FieldError{{Field: "expires_at", Code: "invalid"}}}
	}
	// Delegation cannot add a scope, switch audience or extend the parent lifetime.
	if raw, ok := ctx.Value(credentialContextKey{}).(string); ok {
		hash := sha256.Sum256([]byte(raw))
		a, err := s.repo.ResolveCredential(ctx, hash[:])
		if err != nil {
			return CreateCredentialOutput{}, err
		}
		if a.WorkspaceID != scope.WorkspaceID() || a.UserID != scope.ActorID() || a.Audience != in.Audience || in.ExpiresAt.After(a.ExpiresAt) {
			return CreateCredentialOutput{}, denied(operations.Forbidden)
		}
		for _, grant := range in.Scopes {
			if !slices.Contains(a.Scopes, grant) {
				return CreateCredentialOutput{}, denied(operations.Forbidden)
			}
		}
	}
	random := make([]byte, 32)
	if _, err := rand.Read(random); err != nil {
		return CreateCredentialOutput{}, err
	}
	raw := TokenPrefix + base64.RawURLEncoding.EncodeToString(random)
	hash := sha256.Sum256([]byte(raw))
	c, err := s.repo.CreateCredential(ctx, scope.WorkspaceID(), scope.ActorID(), in, hash[:])
	if err != nil {
		return CreateCredentialOutput{}, err
	}
	return CreateCredentialOutput{Credential: c, Token: raw}, nil
}
func (s *Service) listCredentials(ctx context.Context, scope operations.Scope, in ListInput) (CredentialList, error) {
	rows, err := s.repo.ListCredentials(ctx, scope.WorkspaceID(), cursor(in.After), in.Limit+1)
	out := CredentialList{Items: rows}
	if len(rows) > int(in.Limit) {
		out.HasMore = true
		out.Items = rows[:in.Limit]
		out.NextCursor = out.Items[len(out.Items)-1].ID.String()
	}
	return out, err
}
func (s *Service) revoke(ctx context.Context, scope operations.Scope, in RevokeInput) (RevokeOutput, error) {
	found, err := s.repo.RevokeCredential(ctx, scope.WorkspaceID(), in.CredentialID)
	if err != nil {
		return RevokeOutput{}, err
	}
	if !found {
		return RevokeOutput{}, denied(operations.NotFound)
	}
	return RevokeOutput{Revoked: true}, nil
}

// Capabilities are a presentation hint computed from the same current policy;
// all mutations still authorize again at execution time.
func (s *Service) workspaceOutput(ctx context.Context, actor uuid.UUID, w Workspace) (WorkspaceOutput, error) {
	a, err := s.repo.Resolve(ctx, w.ID, actor)
	if err != nil {
		return WorkspaceOutput{}, err
	}
	if raw, ok := ctx.Value(credentialContextKey{}).(string); ok {
		hash := sha256.Sum256([]byte(raw))
		a, err = s.repo.ResolveCredential(ctx, hash[:])
		if err != nil {
			return WorkspaceOutput{}, err
		}
	}
	allowed := func(permission string) bool {
		return a.Role == "owner" && (a.Audience == "" || slices.Contains(a.Scopes, permission))
	}
	return WorkspaceOutput{Workspace: w, Capabilities: WorkspaceCapabilities{Update: allowed("workspace.write"), ReadCredentials: allowed("credentials.read"), ManageCredentials: allowed("credentials.manage")}}, nil
}

func (s *Service) activity(ctx context.Context, scope operations.Scope, in ListInput) (ActivityList, error) {
	rows, err := s.repo.ListActivity(ctx, scope.WorkspaceID(), cursor(in.After), in.Limit+1)
	out := ActivityList{Items: rows}
	if len(rows) > int(in.Limit) {
		out.Items = rows[:in.Limit]
		out.HasMore = true
		out.NextCursor = out.Items[len(out.Items)-1].ID.String()
	}
	return out, err
}
func (s *Service) deliveries(ctx context.Context, scope operations.Scope, in ListInput) (DeliveryList, error) {
	rows, err := s.repo.ListDeliveries(ctx, scope.WorkspaceID(), cursor(in.After), in.Limit+1)
	out := DeliveryList{Items: rows}
	if len(rows) > int(in.Limit) {
		out.Items = rows[:in.Limit]
		out.HasMore = true
		out.NextCursor = out.Items[len(out.Items)-1].ID.String()
	}
	return out, err
}
func (s *Service) replayDelivery(ctx context.Context, scope operations.Scope, in ReplayDeliveryInput) (ReplayDeliveryOutput, error) {
	err := s.repo.ReplayDelivery(ctx, scope.WorkspaceID(), in.DeliveryID)
	return ReplayDeliveryOutput{Replayed: err == nil}, err
}
