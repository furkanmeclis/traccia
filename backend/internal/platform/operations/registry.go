package operations

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
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
	runner     MutationRunner
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
	identity, err := r.authorizer.Authorize(ctx, AuthorizationRequest{Operation: meta, WorkspaceID: call.WorkspaceID, Channel: call.Channel, Input: call.Input})
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
	scope := Scope{channel: call.Channel, actorID: identity.ActorID, workspaceID: identity.WorkspaceID}
	if !meta.Idempotent {
		return op.run(ctx, scope, call.Input)
	}
	run, err := op.prepare(call.Input)
	if err != nil {
		return nil, err
	}
	if len(call.IdempotencyKey) < 1 || len(call.IdempotencyKey) > 200 || strings.TrimSpace(call.IdempotencyKey) != call.IdempotencyKey || strings.ContainsAny(call.IdempotencyKey, "\r\n\x00") {
		return nil, failure(Validation, nil)
	}
	if r.runner == nil {
		return nil, failure(Unavailable, nil)
	}
	value, err := decodeJSON(call.Input)
	if err != nil {
		return nil, failure(Validation, err)
	}
	canonical, err := json.Marshal(value)
	if err != nil {
		return nil, failure(Validation, err)
	}
	fingerprint := sha256.Sum256(lengthDelimited([]byte(meta.ID), []byte(fmt.Sprint(meta.Version)), []byte(call.WorkspaceID.String()), canonical))
	authorize := func(txctx context.Context) error {
		current, err := r.authorizer.Authorize(txctx, AuthorizationRequest{Operation: meta, WorkspaceID: call.WorkspaceID, Channel: call.Channel, Input: call.Input})
		if err != nil {
			return err
		}
		if current != identity {
			return failure(Forbidden, nil)
		}
		return nil
	}
	output, err := r.runner.RunMutation(ctx, Mutation{Metadata: meta, Scope: scope, Key: call.IdempotencyKey, Fingerprint: fingerprint[:], Authorize: authorize, ValidateResult: op.validateResult}, func(txctx context.Context) ([]byte, error) { return run(txctx, scope) })
	if err != nil {
		return nil, publicError(err)
	}
	return output, nil
}

// WithMutationRunner returns a new registry binding the durable application boundary.
func (r *Registry) WithMutationRunner(runner MutationRunner) *Registry {
	copy := *r
	copy.runner = runner
	return &copy
}

// LengthDelimited avoids ambiguity between adjacent fingerprint fields.
func lengthDelimited(fields ...[]byte) []byte {
	var out []byte
	for _, field := range fields {
		out = binary.BigEndian.AppendUint64(out, uint64(len(field)))
		out = append(out, field...)
	}
	return out
}

// ValidateWireInput applies an opted-in instance budget before a transport walks
// recursive schemas. It grants no identity, permission or execution capability.
func (r *Registry) ValidateWireInput(id string, raw json.RawMessage) error {
	op := r.operations[id]
	if op == nil || op.wire == nil {
		return nil
	}
	if op.wireBytes > 0 && int64(len(raw)) > op.wireBytes {
		return &Error{Code: Validation, Details: []FieldError{{Field: "input", Code: "bytes_limit"}}}
	}
	value, e := decodeJSON(raw)
	if e != nil {
		return failure(Validation, e)
	}
	if details := op.wire(value, raw); len(details) > 0 {
		return &Error{Code: Validation, Details: details}
	}
	return nil
}

// InputReadError retains released operation errors and adds actionable byte
// diagnostics only for inputs that explicitly implement a wire budget.
func (r *Registry) InputReadError(id string) error {
	out := &Error{Code: Validation}
	if op := r.operations[id]; op != nil && op.wire != nil {
		out.Details = []FieldError{{Field: "input", Code: "bytes_limit"}}
	}
	return out
}

// InputReadLimit allows explicitly bounded document inputs to account for JSON
// escape expansion while preserving the released transport limit elsewhere.
func (r *Registry) InputReadLimit(id string) int64 {
	if op := r.operations[id]; op != nil && op.wire != nil {
		if op.wireBytes > 0 {
			return op.wireBytes
		}
		return 4 << 20
	}
	return 1 << 20
}
