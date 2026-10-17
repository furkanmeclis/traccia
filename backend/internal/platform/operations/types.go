// Package operations defines transport-independent application contracts.
// A declared operation is not available until it has a typed handler binding.
package operations

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"
)

type ScopeKind string

const (
	AccountScope   ScopeKind = "account"
	WorkspaceScope ScopeKind = "workspace"
)

type Effect string

const (
	Read  Effect = "read"
	Write Effect = "write"
)

type Channel string

const (
	Panel Channel = "panel"
	API   Channel = "api"
	MCP   Channel = "mcp"
)

// Metadata describes intended mappings, not registered HTTP routes or MCP tools.
// Version changes when an operation's input, output or semantics break compatibility.
type Metadata struct {
	ID              string    `json:"id"`
	Version         int       `json:"version"`
	Scope           ScopeKind `json:"scope"`
	Effect          Effect    `json:"effect"`
	Permission      string    `json:"permission"`
	HTTPMethod      string    `json:"http_method"`
	HTTPPath        string    `json:"http_path"`
	MCPName         string    `json:"mcp_name"`
	Idempotent      bool      `json:"idempotent,omitempty"`
	SensitiveResult bool      `json:"sensitive_result,omitempty"`
}

// Contract contains concrete JSON Schema 2020-12 documents inferred from DTOs.
type Contract struct {
	Metadata  Metadata        `json:"metadata"`
	Input     json.RawMessage `json:"input_schema"`
	Output    json.RawMessage `json:"output_schema"`
	Error     json.RawMessage `json:"error_schema"`
	Available bool            `json:"available"`
}

// AuthorizationRequest carries a requested workspace, never a membership grant.
type AuthorizationRequest struct {
	Operation   Metadata
	WorkspaceID uuid.UUID
	Channel     Channel
	Input       json.RawMessage
}

// Identity is returned only after verifying the caller, token status and requested
// permission. Workspace membership must be checked for every workspace call.
type Identity struct {
	ActorID     uuid.UUID
	WorkspaceID uuid.UUID
}

// Authorizer resolves authenticated identity from ctx, not request JSON. Returning
// an Identity asserts permission in the requested scope. Implementations must not
// turn an unverified client-selected workspace or actor ID into a grant.
type Authorizer interface {
	Authorize(context.Context, AuthorizationRequest) (Identity, error)
}

// Scope is constructed by Registry after authorization. Private fields prevent
// transport code from directly substituting request fields for verified scope.
type Scope struct {
	actorID     uuid.UUID
	workspaceID uuid.UUID
}

func (s Scope) ActorID() uuid.UUID     { return s.actorID }
func (s Scope) WorkspaceID() uuid.UUID { return s.workspaceID }

type Call struct {
	OperationID    string
	Channel        Channel
	WorkspaceID    uuid.UUID
	Input          json.RawMessage
	IdempotencyKey string
}

// MutationRunner owns one atomic transaction containing authorization, receipt,
// handler, and validated output. It must never commit a pending claim.
type MutationRunner interface {
	RunMutation(context.Context, Mutation, func(context.Context) ([]byte, error)) ([]byte, error)
}
type Mutation struct {
	Metadata       Metadata
	Scope          Scope
	Key            string
	Fingerprint    []byte
	Authorize      func(context.Context) error
	ValidateResult func([]byte) error
}
