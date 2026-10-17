package usecase

import (
	"github.com/furkanmeclis/traccia/backend/internal/platform/operations"
	"github.com/google/jsonschema-go/jsonschema"
	"github.com/google/uuid"
)

// Registry is the common entry point for HTTP, future MCP, workers and other adapters.
func (s *Service) Registry() (*operations.Registry, error) {
	var bindings []*operations.Operation
	add := func(op *operations.Operation, err error) error {
		if err != nil {
			return err
		}
		bindings = append(bindings, op)
		return nil
	}
	meta := func(id, permission, path, method, tool string, scope operations.ScopeKind, effect operations.Effect) operations.Metadata {
		return operations.Metadata{ID: id, Version: 2, Idempotent: effect == operations.Write, SensitiveResult: id == "credentials.create", Permission: permission, Scope: scope, Effect: effect, HTTPPath: path, HTTPMethod: method, MCPName: tool}
	}
	nameValidation := func(in CreateInput) []operations.FieldError { return validateName(in.Name) }
	emptyValidation := func(EmptyInput) []operations.FieldError { return nil }
	if err := add(operations.New(meta("workspaces.create", "workspaces.create", "/v1/workspaces", "POST", "create_workspace", operations.AccountScope, operations.Write), nameValidation, s.create, refineSchemas)); err != nil {
		return nil, err
	}
	if err := add(operations.New(meta("workspaces.list", "workspaces.list", "/v1/workspaces", "GET", "list_workspaces", operations.AccountScope, operations.Read), validatePage, s.list, refineSchemas)); err != nil {
		return nil, err
	}
	if err := add(operations.New(meta("workspaces.get", "workspace.read", "/v1/workspaces/{workspaceID}", "GET", "get_workspace", operations.WorkspaceScope, operations.Read), emptyValidation, s.get, refineSchemas)); err != nil {
		return nil, err
	}
	if err := add(operations.New(meta("workspaces.update", "workspace.write", "/v1/workspaces/{workspaceID}", "PATCH", "update_workspace", operations.WorkspaceScope, operations.Write), func(in UpdateInput) []operations.FieldError {
		if in.ExpectedVersion < 1 {
			return []operations.FieldError{{Field: "expected_version", Code: "invalid"}}
		}
		return validateName(in.Name)
	}, s.update, refineSchemas)); err != nil {
		return nil, err
	}
	if err := add(operations.New(meta("credentials.create", "credentials.manage", "/v1/workspaces/{workspaceID}/credentials", "POST", "create_workspace_credential", operations.WorkspaceScope, operations.Write), validateCredential, s.createCredential, refineSchemas)); err != nil {
		return nil, err
	}
	if err := add(operations.New(meta("credentials.list", "credentials.read", "/v1/workspaces/{workspaceID}/credentials", "GET", "list_workspace_credentials", operations.WorkspaceScope, operations.Read), validatePage, s.listCredentials, refineSchemas)); err != nil {
		return nil, err
	}
	validateRevoke := func(in RevokeInput) []operations.FieldError {
		if in.CredentialID == uuid.Nil {
			return []operations.FieldError{{Field: "credential_id", Code: "invalid"}}
		}
		return nil
	}
	if err := add(operations.New(meta("credentials.revoke", "credentials.manage", "/v1/workspaces/{workspaceID}/credentials/{credentialID}", "DELETE", "revoke_workspace_credential", operations.WorkspaceScope, operations.Write), validateRevoke, s.revoke, refineSchemas)); err != nil {
		return nil, err
	}
	r, err := operations.NewRegistry(s, bindings...)
	if err != nil {
		return nil, err
	}
	if runner, ok := s.repo.(operations.MutationRunner); ok {
		r = r.WithMutationRunner(runner)
	}
	return r, nil
}

var _ operations.Authorizer = (*Service)(nil)

// uuid.UUID marshals as a JSON string despite its underlying [16]byte type.
// Make its public wire representation explicit rather than publishing an array schema.
func refineSchemas(input, output *jsonschema.Schema) error {
	var visit func(*jsonschema.Schema)
	visit = func(schema *jsonschema.Schema) {
		if schema == nil {
			return
		}
		for key, child := range schema.Properties {
			if key == "id" || key == "credential_id" {
				*child = jsonschema.Schema{Type: "string", Format: "uuid"}
			}
			if key == "name" {
				low, high := 1, 100
				child.MinLength = &low
				child.MaxLength = &high
			}
			if key == "version" || key == "expected_version" {
				low := float64(1)
				child.Minimum = &low
			}
			if key == "limit" {
				low, high := float64(1), float64(100)
				child.Minimum = &low
				child.Maximum = &high
			}
			if key == "created_at" || key == "updated_at" || key == "expires_at" || key == "revoked_at" {
				child.Format = "date-time"
			}
			if key == "scopes" {
				low, high := 1, 4
				child.MinItems = &low
				child.MaxItems = &high
				child.UniqueItems = true
				if child.Items != nil {
					child.Items.Enum = []any{"workspace.read", "workspace.write", "credentials.read", "credentials.manage"}
				}
			}
			if key == "audience" {
				child.Enum = []any{"api", "mcp"}
			}
			visit(child)
		}
		visit(schema.Items)
		for _, child := range schema.AnyOf {
			visit(child)
		}
	}
	visit(input)
	visit(output)
	return nil
}
