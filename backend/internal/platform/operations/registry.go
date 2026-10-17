package operations

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/google/uuid"
)

var identifier = regexp.MustCompile(`^[a-z][a-z0-9_]*(\.[a-z][a-z0-9_]*)+$`)
var toolName = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

// Registry is immutable: construct it once with explicit operation bindings.
type Registry struct {
	authorizer Authorizer
	operations map[string]*Operation
}

func NewRegistry(authorizer Authorizer, operations ...*Operation) (*Registry, error) {
	if authorizer == nil {
		return nil, errors.New("operations: authorizer required")
	}
	r := &Registry{authorizer: authorizer, operations: make(map[string]*Operation)}
	httpMappings, mcpMappings := map[string]bool{}, map[string]bool{}
	for _, op := range operations {
		if op == nil {
			return nil, errors.New("operations: nil operation")
		}
		m := op.contract.Metadata
		if !identifier.MatchString(m.ID) || m.Version < 1 || strings.TrimSpace(m.Permission) == "" {
			return nil, fmt.Errorf("operations: invalid identity/version/permission for %q", m.ID)
		}
		if m.Scope != AccountScope && m.Scope != WorkspaceScope {
			return nil, fmt.Errorf("operations: invalid scope for %q", m.ID)
		}
		if m.Effect != Read && m.Effect != Write {
			return nil, fmt.Errorf("operations: invalid effect for %q", m.ID)
		}
		switch m.HTTPMethod {
		case "GET", "POST", "PUT", "PATCH", "DELETE":
		default:
			return nil, fmt.Errorf("operations: invalid HTTP method for %q", m.ID)
		}
		if !strings.HasPrefix(m.HTTPPath, "/v1/") || strings.ContainsAny(m.HTTPPath, "?# \t\n") || !toolName.MatchString(m.MCPName) {
			return nil, fmt.Errorf("operations: missing or invalid channel mapping for %q", m.ID)
		}
		httpKey := m.HTTPMethod + " " + m.HTTPPath
		if _, exists := r.operations[m.ID]; exists || httpMappings[httpKey] || mcpMappings[m.MCPName] {
			return nil, fmt.Errorf("operations: duplicate operation or channel mapping for %q", m.ID)
		}
		r.operations[m.ID] = op
		httpMappings[httpKey], mcpMappings[m.MCPName] = true, true
	}
	return r, nil
}

// Catalog returns owned schema copies, sorted by ID. Available is a binding flag,
// not a claim that HTTP, MCP or production authorization is wired.
func (r *Registry) Catalog() []Contract {
	result := make([]Contract, 0, len(r.operations))
	for _, op := range r.operations {
		c := op.contract
		c.Input = slices.Clone(c.Input)
		c.Output = slices.Clone(c.Output)
		c.Error = slices.Clone(c.Error)
		result = append(result, c)
	}
	slices.SortFunc(result, func(a, b Contract) int { return strings.Compare(a.Metadata.ID, b.Metadata.ID) })
	return result
}

func (r *Registry) Execute(ctx context.Context, call Call) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, publicError(err)
	}
	op, ok := r.operations[call.OperationID]
	if !ok {
		return nil, failure(NotFound, nil)
	}
	if call.Channel != Panel && call.Channel != API && call.Channel != MCP {
		return nil, failure(Validation, nil)
	}
	meta := op.contract.Metadata
	if (meta.Scope == WorkspaceScope && call.WorkspaceID == uuid.Nil) || (meta.Scope == AccountScope && call.WorkspaceID != uuid.Nil) {
		return nil, failure(Validation, nil)
	}
	identity, err := r.authorizer.Authorize(ctx, AuthorizationRequest{Operation: meta, WorkspaceID: call.WorkspaceID, Channel: call.Channel})
	if err != nil {
		return nil, publicError(err)
	}
	if identity.ActorID == uuid.Nil {
		return nil, failure(Unauthenticated, nil)
	}
	if identity.WorkspaceID != call.WorkspaceID {
		return nil, failure(Forbidden, nil)
	}
	if op.run == nil {
		return nil, failure(Unavailable, nil)
	}
	if err := ctx.Err(); err != nil {
		return nil, publicError(err)
	}
	return op.run(ctx, Scope{actorID: identity.ActorID, workspaceID: identity.WorkspaceID}, call.Input)
}
