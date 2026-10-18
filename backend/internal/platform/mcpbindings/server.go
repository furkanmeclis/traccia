// Package mcpbindings binds shared application contracts to the MCP SDK.
package mcpbindings

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/furkanmeclis/traccia/backend/internal/platform/operations"
	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	validator "github.com/santhosh-tekuri/jsonschema/v6"
)

// NewServer owns a fresh server, preventing AddTool from replacing existing tools.
// Connect using verified context or WithToolCredential bearer material. Arguments
// never establish identity. Construction opens no listener and grants no access.
func NewServer(registry *operations.Registry, options *mcp.ServerOptions) (*mcp.Server, error) {
	if registry == nil {
		return nil, errors.New("mcpbindings: registry required")
	}
	type binding struct {
		tool    *mcp.Tool
		handler mcp.ToolHandler
	}
	var bindings []binding
	names := map[string]bool{}
	for _, contract := range registry.Catalog() {
		if !contract.Available {
			continue
		}
		if names[contract.Metadata.MCPName] {
			return nil, errors.New("mcpbindings: duplicate tool name")
		}
		names[contract.Metadata.MCPName] = true
		schema, err := argumentSchema(contract)
		if err != nil {
			return nil, fmt.Errorf("mcpbindings: %s input: %w", contract.Metadata.ID, err)
		}
		arguments, err := compileSchema(schema)
		if err != nil {
			return nil, fmt.Errorf("mcpbindings: %s wrapper: %w", contract.Metadata.ID, err)
		}
		errorSchema, err := compileSchema(contract.Error)
		if err != nil {
			return nil, fmt.Errorf("mcpbindings: %s error: %w", contract.Metadata.ID, err)
		}
		tool := &mcp.Tool{Name: contract.Metadata.MCPName, Description: contract.Metadata.ID, InputSchema: json.RawMessage(schema), OutputSchema: json.RawMessage(contract.Output), Meta: mcp.Meta{"traccia/error_schema": json.RawMessage(contract.Error), "traccia/operation_id": contract.Metadata.ID, "traccia/contract_version": contract.Metadata.Version}}
		bindings = append(bindings, binding{tool: tool, handler: handler(registry, contract, arguments, errorSchema)})
	}
	server := mcp.NewServer(&mcp.Implementation{Name: "traccia", Version: "1"}, options)
	for _, b := range bindings {
		server.AddTool(b.tool, b.handler)
	}
	return server, nil
}

func handler(registry *operations.Registry, contract operations.Contract, arguments, errorSchema *validator.Schema) mcp.ToolHandler {
	return func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		fail := func(err error) (*mcp.CallToolResult, error) { return errorResult(err, errorSchema) }
		if req == nil || req.Params == nil {
			return fail(&operations.Error{Code: operations.Validation})
		}
		value, err := operations.DecodeJSON(req.Params.Arguments)
		if err != nil {
			return fail(&operations.Error{Code: operations.Validation})
		}
		var args struct {
			WorkspaceID string          `json:"workspace_id"`
			Key         string          `json:"idempotency_key"`
			Input       json.RawMessage `json:"input"`
		}
		if err := json.Unmarshal(req.Params.Arguments, &args); err != nil {
			return fail(&operations.Error{Code: operations.Validation})
		}
		if err := registry.ValidateWireInput(contract.Metadata.ID, args.Input); err != nil {
			return fail(err)
		}
		if err := arguments.Validate(value); err != nil {
			return fail(&operations.Error{Code: operations.Validation})
		}
		workspace := uuid.Nil
		if contract.Metadata.Scope == operations.WorkspaceScope {
			workspace, err = uuid.Parse(args.WorkspaceID)
			if err != nil || workspace == uuid.Nil {
				return fail(&operations.Error{Code: operations.Validation})
			}
		}
		if contract.Metadata.Effect == operations.Write && (len(args.Key) < 1 || len(args.Key) > 200 || strings.TrimSpace(args.Key) != args.Key || strings.ContainsAny(args.Key, ",\r\n\x00")) {
			return fail(&operations.Error{Code: operations.Validation})
		}
		output, err := registry.Execute(ctx, operations.Call{OperationID: contract.Metadata.ID, Channel: operations.MCP, WorkspaceID: workspace, IdempotencyKey: args.Key, Input: args.Input})
		if err != nil {
			return fail(err)
		}
		return &mcp.CallToolResult{StructuredContent: json.RawMessage(output), Content: []mcp.Content{&mcp.TextContent{Text: string(output)}}}, nil
	}
}

func errorResult(err error, schema *validator.Schema) (*mcp.CallToolResult, error) {
	public := &operations.Error{Code: operations.Internal}
	var known *operations.Error
	if errors.As(err, &known) && known != nil {
		public = &operations.Error{Code: known.Code, Details: known.Details}
	}
	raw, marshalErr := json.Marshal(public)
	if marshalErr != nil {
		return nil, errors.New("mcpbindings: cannot encode public error")
	}
	value, parseErr := operations.DecodeJSON(raw)
	if parseErr != nil || schema.Validate(value) != nil {
		raw = []byte(`{"code":"INTERNAL_ERROR"}`)
	}
	return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: string(raw)}}}, nil
}

func argumentSchema(contract operations.Contract) ([]byte, error) {
	input, err := operations.DecodeJSON(contract.Input)
	if err != nil {
		return nil, err
	}
	// Make the original DTO root an independent resource before embedding it.
	// This preserves local pointers, recursive refs, relative nested $id values,
	// and references crossing back from a nested resource to the original root.
	// Rewriting all fragments would instead resolve them in the wrong resource.
	root, ok := input.(map[string]any)
	if !ok {
		return nil, errors.New("mcpbindings: input schema must be an object")
	}
	base, err := url.Parse(operations.SchemaBaseURI)
	if err != nil {
		return nil, err
	}
	if id, ok := root["$id"].(string); ok {
		reference, err := url.Parse(id)
		if err != nil {
			return nil, fmt.Errorf("mcpbindings: invalid schema resource id: %w", err)
		}
		root["$id"] = base.ResolveReference(reference).String()
	} else {
		root["$id"] = base.String()
	}
	canonical, err := url.Parse(root["$id"].(string))
	if err != nil {
		return nil, err
	}
	if err := canonicalizeRetrievalRefs(root, base, canonical); err != nil {
		return nil, err
	}
	properties := map[string]any{"input": input}
	required := []string{"input"}
	if contract.Metadata.Scope == operations.WorkspaceScope {
		properties["workspace_id"] = map[string]any{"type": "string", "format": "uuid"}
		required = append(required, "workspace_id")
	}
	if contract.Metadata.Effect == operations.Write {
		properties["idempotency_key"] = map[string]any{"type": "string", "minLength": 1, "maxLength": 200}
		required = append(required, "idempotency_key")
	}
	return json.Marshal(map[string]any{"$schema": "https://json-schema.org/draft/2020-12/schema", "type": "object", "additionalProperties": false, "properties": properties, "required": required})
}

// The original compiler registers the root under both its retrieval URI and
// canonical $id. Embedded resources expose only the canonical identity, so refs
// to that retrieval alias must point at the canonical root instead. Local refs
// within named resources retain their original bases and fragments.
func canonicalizeRetrievalRefs(value any, parent, canonical *url.URL) error {
	if canonical.String() == operations.SchemaBaseURI {
		return nil
	}
	schema, ok := value.(map[string]any)
	if !ok {
		return nil
	}
	base := parent
	if id, ok := schema["$id"].(string); ok {
		parsed, err := url.Parse(id)
		if err != nil {
			return err
		}
		base = parent.ResolveReference(parsed)
	}
	for _, key := range []string{"$ref", "$dynamicRef"} {
		if ref, ok := schema[key].(string); ok {
			parsed, err := url.Parse(ref)
			if err != nil {
				return err
			}
			target := base.ResolveReference(parsed)
			resource := *target
			resource.Fragment = ""
			resource.RawFragment = ""
			if resource.String() == operations.SchemaBaseURI {
				replacement := *canonical
				replacement.Fragment = target.Fragment
				replacement.RawFragment = target.RawFragment
				schema[key] = replacement.String()
			}
		}
	}
	// Visit only schema positions. Literal enum/default/example objects are data.
	for _, key := range []string{"$defs", "definitions", "properties", "patternProperties", "dependentSchemas"} {
		if children, ok := schema[key].(map[string]any); ok {
			for _, child := range children {
				if err := canonicalizeRetrievalRefs(child, base, canonical); err != nil {
					return err
				}
			}
		}
	}
	for _, key := range []string{"additionalProperties", "unevaluatedProperties", "propertyNames", "items", "additionalItems", "unevaluatedItems", "contains", "not", "if", "then", "else", "contentSchema"} {
		if err := canonicalizeRetrievalRefs(schema[key], base, canonical); err != nil {
			return err
		}
	}
	for _, key := range []string{"allOf", "anyOf", "oneOf", "prefixItems"} {
		if children, ok := schema[key].([]any); ok {
			for _, child := range children {
				if err := canonicalizeRetrievalRefs(child, base, canonical); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

type offlineLoader struct{}

func (offlineLoader) Load(string) (any, error) {
	return nil, errors.New("mcpbindings: external schemas disabled")
}
func compileSchema(raw []byte) (*validator.Schema, error) {
	value, err := operations.DecodeJSON(raw)
	if err != nil {
		return nil, err
	}
	compiler := validator.NewCompiler()
	compiler.UseLoader(offlineLoader{})
	// The wrapper has no public resource identity. Use a fresh private base so
	// a valid DTO resource named mcp.schema.json cannot collide with its parent.
	location := "urn:traccia:mcp:" + uuid.NewString()
	if err := compiler.AddResource(location, value); err != nil {
		return nil, err
	}
	return compiler.Compile(location)
}
