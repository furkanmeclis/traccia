package mcpbindings

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/furkanmeclis/traccia/backend/internal/platform/operations"
	"github.com/google/jsonschema-go/jsonschema"
	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type testIdentity struct {
	actor, workspace uuid.UUID
	audience         string
	write, active    bool
}
type identityKey struct{}
type testAuthorizer struct{}

func (testAuthorizer) Authorize(ctx context.Context, req operations.AuthorizationRequest) (operations.Identity, error) {
	id, ok := ctx.Value(identityKey{}).(*testIdentity)
	if !ok || !id.active {
		return operations.Identity{}, &operations.Error{Code: operations.Unauthenticated}
	}
	if req.Channel != operations.MCP || id.audience != "mcp" || req.WorkspaceID != id.workspace || req.Operation.Effect == operations.Write && !id.write {
		return operations.Identity{}, &operations.Error{Code: operations.Forbidden}
	}
	return operations.Identity{ActorID: id.actor, WorkspaceID: id.workspace}, nil
}

type echoInput struct {
	Text string         `json:"text"`
	Data map[string]any `json:"data"`
}
type echoOutput struct {
	Text string         `json:"text"`
	Data map[string]any `json:"data"`
}

func echoOperation(t *testing.T, index int, effect operations.Effect, scope operations.ScopeKind, bound bool) *operations.Operation {
	t.Helper()
	var fn operations.Handler[echoInput, echoOutput]
	if bound {
		fn = func(_ context.Context, _ operations.Scope, in echoInput) (echoOutput, error) {
			if in.Text == "fail" {
				return echoOutput{}, errors.New("private provider secret")
			}
			return echoOutput(in), nil
		}
	}
	op, err := operations.New(operations.Metadata{ID: fmt.Sprintf("fixture.op_%d", index), Version: 1, Permission: "fixture.read", Scope: scope, Effect: effect, Idempotent: effect == operations.Write, HTTPMethod: "POST", HTTPPath: fmt.Sprintf("/v1/fixture/%d", index), MCPName: fmt.Sprintf("fixture_%d", index)}, func(echoInput) []operations.FieldError { return nil }, fn, nil)
	if err != nil {
		t.Fatal(err)
	}
	return op
}

type receipt struct{ fingerprint, output []byte }
type testRunner struct {
	receipts map[string]receipt
	calls    int
}

func (r *testRunner) RunMutation(ctx context.Context, m operations.Mutation, run func(context.Context) ([]byte, error)) ([]byte, error) {
	if err := m.Authorize(ctx); err != nil {
		return nil, err
	}
	if old, ok := r.receipts[m.Key]; ok {
		if !bytes.Equal(old.fingerprint, m.Fingerprint) {
			return nil, &operations.Error{Code: operations.IdempotencyConflict}
		}
		return old.output, nil
	}
	output, err := run(ctx)
	if err != nil {
		return nil, err
	}
	if err := m.ValidateResult(output); err != nil {
		return nil, err
	}
	r.calls++
	r.receipts[m.Key] = receipt{m.Fingerprint, output}
	return output, nil
}

type wireCapture struct {
	mu       sync.Mutex
	messages [][]byte
}

func (w *wireCapture) add(message []byte) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.messages = append(w.messages, bytes.Clone(message))
}
func (w *wireCapture) results() [][]byte {
	w.mu.Lock()
	defer w.mu.Unlock()
	return append([][]byte(nil), w.messages...)
}

type captureTransport struct {
	mcp.Transport
	wire *wireCapture
}

func (t captureTransport) Connect(ctx context.Context) (mcp.Connection, error) {
	c, err := t.Transport.Connect(ctx)
	if err != nil {
		return nil, err
	}
	return captureConnection{Connection: c, wire: t.wire}, nil
}

type captureConnection struct {
	mcp.Connection
	wire *wireCapture
}

func (c captureConnection) Write(ctx context.Context, msg jsonrpc.Message) error {
	raw, err := jsonrpc.EncodeMessage(msg)
	if err != nil {
		return err
	}
	c.wire.add(raw)
	return c.Connection.Write(ctx, msg)
}
func connect(t *testing.T, registry *operations.Registry, id *testIdentity, pageSize int) (*mcp.ClientSession, *wireCapture) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	server, err := NewServer(registry, &mcp.ServerOptions{PageSize: pageSize})
	if err != nil {
		t.Fatal(err)
	}
	st, ct := mcp.NewInMemoryTransports()
	wire := &wireCapture{}
	sc, err := server.Connect(context.WithValue(ctx, identityKey{}, id), captureTransport{st, wire}, nil)
	if err != nil {
		t.Fatal(err)
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "1"}, nil)
	cc, err := client.Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := cc.Close(); err != nil {
			t.Error(err)
		}
		if err := sc.Close(); err != nil {
			t.Error(err)
		}
	})
	return cc, wire
}
func registry(t *testing.T, ops ...*operations.Operation) *operations.Registry {
	t.Helper()
	r, err := operations.NewRegistry(testAuthorizer{}, ops...)
	if err != nil {
		t.Fatal(err)
	}
	return r
}
func call(t *testing.T, c *mcp.ClientSession, name string, args string) *mcp.CallToolResult {
	t.Helper()
	res, err := c.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: json.RawMessage(args)})
	if err != nil {
		t.Fatal(err)
	}
	return res
}
func publicCode(t *testing.T, result *mcp.CallToolResult) operations.Code {
	t.Helper()
	if !result.IsError || result.StructuredContent != nil || len(result.Content) != 1 {
		t.Fatalf("invalid tool error: %#v", result)
	}
	content, ok := result.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatal("missing JSON text")
	}
	var e operations.Error
	if err := json.Unmarshal([]byte(content.Text), &e); err != nil {
		t.Fatal(err)
	}
	return e.Code
}
func TestInMemoryExactWireAndValidation(t *testing.T) {
	id := &testIdentity{actor: uuid.New(), workspace: uuid.New(), audience: "mcp", active: true, write: true}
	r := registry(t, echoOperation(t, 1, operations.Read, operations.WorkspaceScope, true))
	c, wire := connect(t, r, id, 2)
	huge := strings.Repeat("Türkçe 日本語 🧭\n", 12000)
	text, err := json.Marshal(huge)
	if err != nil {
		t.Fatal(err)
	}
	args := fmt.Sprintf(`{"workspace_id":%q,"input":{"text":%s,"data":{"number":9007199254740993,"array":[9007199254740993,{"same":9007199254740993}]}}}`, id.workspace, text)
	result := call(t, c, "fixture_1", args)
	if result.IsError {
		t.Fatal(result)
	}
	content := result.Content[0].(*mcp.TextContent).Text
	if !strings.Contains(content, "9007199254740993") {
		t.Fatal("text rounded integer")
	}
	var output echoOutput
	dec := json.NewDecoder(strings.NewReader(content))
	dec.UseNumber()
	if err := dec.Decode(&output); err != nil {
		t.Fatal(err)
	}
	if output.Text != huge || output.Data["number"] != json.Number("9007199254740993") {
		t.Fatal("content changed")
	}
	found := false
	for _, message := range wire.results() {
		var envelope struct {
			Result struct {
				Structured json.RawMessage `json:"structuredContent"`
			} `json:"result"`
		}
		if err := json.Unmarshal(message, &envelope); err != nil {
			t.Fatal(err)
		}
		if len(envelope.Result.Structured) > 0 {
			found = true
			if !bytes.Equal(envelope.Result.Structured, []byte(content)) {
				t.Fatal("wire structured output differs from exact text")
			}
		}
	}
	if !found {
		t.Fatal("no actual wire structured output")
	}
	// Client StructuredContent is decoded into any by the SDK; precision claims
	// above deliberately inspect raw wire bytes and TextContent, not its float64s.
	cases := []string{
		fmt.Sprintf("{\"workspace_id\":%q,\"input\":{\"text\":\"%s\",\"data\":{}}}", id.workspace, string([]byte{0xff})),
		`{"input":{"text":"x","data":{}}}`,
		fmt.Sprintf(`{"workspace_id":%q,"actor_id":"spoof","input":{"text":"x","data":{}}}`, id.workspace),
		fmt.Sprintf(`{"workspace_id":%q,"idempotency_key":"irrelevant","input":{"text":"x","data":{}}}`, id.workspace),
		fmt.Sprintf(`{"workspace_id":%q,"input":{"text":"x","data":{"nested":[{"x":1,"x":2}]}}}`, id.workspace),
		fmt.Sprintf(`{"workspace_id":%q,"workspace_id":%q,"input":{"text":"x","data":{}}}`, id.workspace, id.workspace),
		fmt.Sprintf(`{"workspace_id":%q,"input":{"text":"\ud800","data":{}}}`, id.workspace),
		fmt.Sprintf(`{"workspace_id":%q,"input":{"text":"x","data":{},"unknown":1}}`, id.workspace),
		fmt.Sprintf(`{"workspace_id":%q,"input":{"text":"x","data":{}}}`, uuid.Nil),
	}
	for _, args := range cases {
		if code := publicCode(t, call(t, c, "fixture_1", args)); code != operations.Validation {
			t.Fatalf("got %s", code)
		}
	}
	foreign := fmt.Sprintf(`{"workspace_id":%q,"input":{"text":"x","data":{}}}`, uuid.New())
	if code := publicCode(t, call(t, c, "fixture_1", foreign)); code != operations.Forbidden {
		t.Fatal(code)
	}
	fail := fmt.Sprintf(`{"workspace_id":%q,"input":{"text":"fail","data":{}}}`, id.workspace)
	failed := call(t, c, "fixture_1", fail)
	if code := publicCode(t, failed); code != operations.Internal {
		t.Fatal(code)
	}
	if strings.Contains(failed.Content[0].(*mcp.TextContent).Text, "private") {
		t.Fatal("private error escaped")
	}
	if _, err := c.CallTool(context.Background(), &mcp.CallToolParams{Name: "unknown_tool", Arguments: map[string]any{}}); err == nil {
		t.Fatal("unknown tool must be protocol error")
	}
}

func TestRetryKeysAndCurrentAuthorization(t *testing.T) {
	id := &testIdentity{actor: uuid.New(), workspace: uuid.New(), audience: "mcp", active: true, write: true}
	runner := &testRunner{receipts: map[string]receipt{}}
	r := registry(t, echoOperation(t, 1, operations.Write, operations.WorkspaceScope, true)).WithMutationRunner(runner)
	c, _ := connect(t, r, id, 2)
	args := func(key, text string) string {
		return fmt.Sprintf(`{"workspace_id":%q,"idempotency_key":%q,"input":{"text":%q,"data":{}}}`, id.workspace, key, text)
	}
	for range 2 {
		if call(t, c, "fixture_1", args("draft-1", "same")).IsError {
			t.Fatal("retry failed")
		}
	}
	if runner.calls != 1 {
		t.Fatal("retry reran mutation")
	}
	if code := publicCode(t, call(t, c, "fixture_1", args("draft-1", "changed"))); code != operations.IdempotencyConflict {
		t.Fatal(code)
	}
	for _, key := range []string{"", " leading", "comma,key", "line\nbreak", strings.Repeat("x", 201)} {
		if code := publicCode(t, call(t, c, "fixture_1", args(key, "same"))); code != operations.Validation {
			t.Fatal(code)
		}
	}
	missing := fmt.Sprintf(`{"workspace_id":%q,"input":{"text":"same","data":{}}}`, id.workspace)
	if code := publicCode(t, call(t, c, "fixture_1", missing)); code != operations.Validation {
		t.Fatal(code)
	}
	id.write = false
	if code := publicCode(t, call(t, c, "fixture_1", args("draft-1", "same"))); code != operations.Forbidden {
		t.Fatal(code)
	}
	id.write = true
	id.audience = "api"
	if code := publicCode(t, call(t, c, "fixture_1", args("draft-1", "same"))); code != operations.Forbidden {
		t.Fatal(code)
	}
	id.audience = "mcp"
	id.active = false
	if code := publicCode(t, call(t, c, "fixture_1", args("draft-1", "same"))); code != operations.Unauthenticated {
		t.Fatal(code)
	}
	if runner.calls != 1 {
		t.Fatal("unauthorized replay executed")
	}
}

func TestToolDiscoveryPaginationAndAccountWrapper(t *testing.T) {
	var ops []*operations.Operation
	for index := range 27 {
		ops = append(ops, echoOperation(t, index, operations.Read, operations.AccountScope, true))
	}
	ops = append(ops, echoOperation(t, 28, operations.Read, operations.AccountScope, false))
	r := registry(t, ops...)
	id := &testIdentity{actor: uuid.New(), audience: "mcp", active: true}
	c, _ := connect(t, r, id, 3)
	seen := map[string]bool{}
	cursor := ""
	pages := 0
	for {
		page, err := c.ListTools(context.Background(), &mcp.ListToolsParams{Cursor: cursor})
		if err != nil {
			t.Fatal(err)
		}
		pages++
		for _, tool := range page.Tools {
			if seen[tool.Name] {
				t.Fatal("duplicate tool")
			}
			seen[tool.Name] = true
			if tool.Meta["traccia/error_schema"] == nil {
				t.Fatal("missing error contract")
			}
		}
		if page.NextCursor == "" {
			break
		}
		cursor = page.NextCursor
	}
	if pages != 9 || len(seen) != 27 || seen["fixture_28"] {
		t.Fatalf("pages=%d tools=%d", pages, len(seen))
	}
	good := `{"input":{"text":"ok","data":{}}}`
	if call(t, c, "fixture_0", good).IsError {
		t.Fatal("account tool failed")
	}
	bad := fmt.Sprintf(`{"workspace_id":%q,"input":{"text":"ok","data":{}}}`, uuid.New())
	if code := publicCode(t, call(t, c, "fixture_0", bad)); code != operations.Validation {
		t.Fatal(code)
	}
}

func TestNestedDefinitionReferencesAndStrictJSON(t *testing.T) {
	contract := operations.Contract{Metadata: operations.Metadata{Scope: operations.AccountScope}, Input: json.RawMessage(`{"type":"object","properties":{"node":{"$ref":"#/$defs/node"}},"required":["node"],"additionalProperties":false,"$defs":{"node":{"type":"object","properties":{"value":{"type":"integer"},"next":{"$ref":"#/$defs/node"}},"required":["value"],"additionalProperties":false}}}`)}
	raw, err := argumentSchema(contract)
	if err != nil {
		t.Fatal(err)
	}
	schema, err := compileSchema(raw)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		raw   string
		valid bool
	}{{`{"input":{"node":{"value":9007199254740993,"next":{"value":2}}}}`, true}, {`{"input":{"node":{"value":"bad"}}}`, false}} {
		value, err := operations.DecodeJSON([]byte(tc.raw))
		if err != nil {
			t.Fatal(err)
		}
		if (schema.Validate(value) == nil) != tc.valid {
			t.Fatal("nested reference not enforced")
		}
	}
	for _, raw := range [][]byte{[]byte("{\"input\":\"\xff\"}"), []byte(`{"input":{"text":"\udc00"}}`), []byte(`{"input":{},"input":{}}`)} {
		if _, err := operations.DecodeJSON(raw); err == nil {
			t.Fatal("malformed JSON accepted")
		}
	}
}

type recursiveInput struct {
	Node json.RawMessage `json:"node"`
}

func TestRecursiveDTOThroughSDK(t *testing.T) {
	meta := operations.Metadata{ID: "fixture.recursive", Version: 1, Permission: "fixture.read", Scope: operations.AccountScope, Effect: operations.Read, HTTPMethod: "POST", HTTPPath: "/v1/recursive", MCPName: "recursive"}
	op, err := operations.New(meta, func(recursiveInput) []operations.FieldError { return nil }, func(_ context.Context, _ operations.Scope, in recursiveInput) (recursiveInput, error) { return in, nil }, func(input, output *jsonschema.Schema) error {
		raw := []byte(`{"type":"object","properties":{"node":{"$ref":"#/$defs/node"}},"required":["node"],"additionalProperties":false,"$defs":{"node":{"type":"object","properties":{"value":{"type":"integer"},"next":{"$ref":"#/$defs/node"}},"required":["value"],"additionalProperties":false}}}`)
		if err := json.Unmarshal(raw, input); err != nil {
			return err
		}
		return json.Unmarshal(raw, output)
	})
	if err != nil {
		t.Fatal(err)
	}
	id := &testIdentity{actor: uuid.New(), audience: "mcp", active: true}
	c, _ := connect(t, registry(t, op), id, 2)
	output := call(t, c, "recursive", `{"input":{"node":{"value":9007199254740993,"next":{"value":2}}}}`)
	if output.IsError || !strings.Contains(output.Content[0].(*mcp.TextContent).Text, "9007199254740993") {
		t.Fatal("recursive DTO changed")
	}
	bad := call(t, c, "recursive", `{"input":{"node":{"value":1,"next":{"value":"wrong"}}}}`)
	if code := publicCode(t, bad); code != operations.Validation {
		t.Fatal(code)
	}
}
func TestEmbeddingPreservesLiteralDataAndOriginalResource(t *testing.T) {
	raw := json.RawMessage(`{"type":"object","properties":{"x":{"$ref":"#/$defs/node"}},"$defs":{"node":{"type":"string"}},"enum":[{"$ref":"#/literal"}],"default":{"$ref":"#/literal"},"examples":[{"$ref":"#/literal"}]}`)
	wrapped, err := argumentSchema(operations.Contract{Input: raw})
	if err != nil {
		t.Fatal(err)
	}
	value, err := operations.DecodeJSON(wrapped)
	if err != nil {
		t.Fatal(err)
	}
	schema := value.(map[string]any)["properties"].(map[string]any)["input"].(map[string]any)
	if schema["$id"] != operations.SchemaBaseURI {
		t.Fatal("original resource base missing")
	}
	if schema["default"].(map[string]any)["$ref"] != "#/literal" || schema["enum"].([]any)[0].(map[string]any)["$ref"] != "#/literal" {
		t.Fatal("literal data changed")
	}
	if schema["properties"].(map[string]any)["x"].(map[string]any)["$ref"] != "#/$defs/node" {
		t.Fatal("original reference changed")
	}
}

func TestResourceIDsThroughSDK(t *testing.T) {
	cases := []struct{ name, schema, valid, invalid string }{
		{"root_id", `{"$id":"https://schemas.example.invalid/input","type":"object","properties":{"node":{"$ref":"#/$defs/node"}},"required":["node"],"additionalProperties":false,"$defs":{"node":{"type":"object","properties":{"value":{"type":"integer"}},"required":["value"],"additionalProperties":false}}}`, `{"input":{"node":{"value":9007199254740993}}}`, `{"input":{"node":{"value":"wrong"}}}`},
		{"wrapper_base_id", `{"$id":"https://traccia.invalid/mcp.schema.json","type":"object","properties":{"node":{"$ref":"#/$defs/node"}},"required":["node"],"additionalProperties":false,"$defs":{"node":{"type":"object","properties":{"value":{"type":"integer"}},"required":["value"],"additionalProperties":false}}}`, `{"input":{"node":{"value":9007199254740993}}}`, `{"input":{"node":{"value":"wrong"}}}`},
		{"nested_id", `{"type":"object","properties":{"node":{"$ref":"node.json"}},"required":["node"],"additionalProperties":false,"$defs":{"node":{"$id":"node.json","type":"object","properties":{"value":{"$ref":"#/$defs/integer"},"next":{"$ref":"#"}},"required":["value"],"additionalProperties":false,"$defs":{"integer":{"type":"integer"}}}}}`, `{"input":{"node":{"value":9007199254740993,"next":{"value":2}}}}`, `{"input":{"node":{"value":1,"next":{"value":"wrong"}}}}`},
		{"nested_backref", `{"type":"object","properties":{"node":{"$ref":"node.json"}},"required":["node"],"additionalProperties":false,"$defs":{"integer":{"type":"integer"},"node":{"$id":"node.json","type":"object","properties":{"value":{"$ref":"operation.schema.json#/$defs/integer"}},"required":["value"],"additionalProperties":false}}}`, `{"input":{"node":{"value":9007199254740993}}}`, `{"input":{"node":{"value":"wrong"}}}`},
		{"relative_root_id", `{"$id":"input.json","type":"object","properties":{"node":{"$ref":"#/$defs/integer"}},"required":["node"],"additionalProperties":false,"$defs":{"integer":{"type":"integer"}}}`, `{"input":{"node":9007199254740993}}`, `{"input":{"node":"wrong"}}`},
		{"root_retrieval_alias", `{"$id":"https://schemas.example.invalid/input","type":"object","properties":{"node":{"$ref":"https://traccia.invalid/operation.schema.json#/$defs/node"}},"required":["node"],"additionalProperties":false,"$defs":{"node":{"type":"object","properties":{"value":{"type":"integer"}},"required":["value"],"additionalProperties":false}}}`, `{"input":{"node":{"value":9007199254740993}}}`, `{"input":{"node":{"value":"wrong"}}}`},
		{"nested_retrieval_alias", `{"$id":"https://schemas.example.invalid/input","type":"object","properties":{"node":{"$ref":"node.json"}},"required":["node"],"additionalProperties":false,"$defs":{"integer":{"$anchor":"integer","type":"integer"},"node":{"$id":"node.json","type":"object","properties":{"value":{"$ref":"https://traccia.invalid/operation.schema.json#integer"}},"required":["value"],"additionalProperties":false}}}`, `{"input":{"node":{"value":9007199254740993}}}`, `{"input":{"node":{"value":"wrong"}}}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			meta := operations.Metadata{ID: "fixture.resource", Version: 1, Permission: "fixture.read", Scope: operations.AccountScope, Effect: operations.Read, HTTPMethod: "POST", HTTPPath: "/v1/resource", MCPName: "resource"}
			op, err := operations.New(meta, func(recursiveInput) []operations.FieldError { return nil }, func(_ context.Context, _ operations.Scope, in recursiveInput) (recursiveInput, error) { return in, nil }, func(input, output *jsonschema.Schema) error {
				if err := json.Unmarshal([]byte(tc.schema), input); err != nil {
					return err
				}
				return json.Unmarshal([]byte(tc.schema), output)
			})
			if err != nil {
				t.Fatal(err)
			}
			id := &testIdentity{actor: uuid.New(), audience: "mcp", active: true}
			c, _ := connect(t, registry(t, op), id, 2)
			output := call(t, c, "resource", tc.valid)
			if output.IsError || !strings.Contains(output.Content[0].(*mcp.TextContent).Text, "9007199254740993") {
				t.Fatal("valid resource operation failed")
			}
			if code := publicCode(t, call(t, c, "resource", tc.invalid)); code != operations.Validation {
				t.Fatal(code)
			}
		})
	}
}

func TestDistinctToolResourcesAreIndependent(t *testing.T) {
	var ops []*operations.Operation
	for index, kind := range []string{"integer", "string"} {
		meta := operations.Metadata{ID: fmt.Sprintf("fixture.resource_%d", index), Version: 1, Permission: "fixture.read", Scope: operations.AccountScope, Effect: operations.Read, HTTPMethod: "POST", HTTPPath: fmt.Sprintf("/v1/resource/%d", index), MCPName: fmt.Sprintf("resource_%d", index)}
		op, err := operations.New(meta, func(recursiveInput) []operations.FieldError { return nil }, func(_ context.Context, _ operations.Scope, in recursiveInput) (recursiveInput, error) { return in, nil }, func(input, output *jsonschema.Schema) error {
			raw := []byte(fmt.Sprintf(`{"type":"object","properties":{"node":{"$ref":"#/$defs/value"}},"required":["node"],"additionalProperties":false,"$defs":{"value":{"type":%q}}}`, kind))
			if err := json.Unmarshal(raw, input); err != nil {
				return err
			}
			return json.Unmarshal(raw, output)
		})
		if err != nil {
			t.Fatal(err)
		}
		ops = append(ops, op)
	}
	id := &testIdentity{actor: uuid.New(), audience: "mcp", active: true}
	c, _ := connect(t, registry(t, ops...), id, 2)
	for _, tc := range []struct{ name, good, bad string }{{"resource_0", `{"input":{"node":9007199254740993}}`, `{"input":{"node":"wrong"}}`}, {"resource_1", `{"input":{"node":"ok"}}`, `{"input":{"node":9007199254740993}}`}} {
		if call(t, c, tc.name, tc.good).IsError {
			t.Fatal("valid distinct tool failed")
		}
		if code := publicCode(t, call(t, c, tc.name, tc.bad)); code != operations.Validation {
			t.Fatal(code)
		}
	}
}

func TestRetrievalAliasCanonicalizationPreservesLiteralData(t *testing.T) {
	raw := json.RawMessage(`{"$id":"https://schemas.example.invalid/input","type":"object","properties":{"node":{"$ref":"https://traccia.invalid/operation.schema.json#/$defs/node"}},"$defs":{"node":{"type":"string"}},"default":{"$ref":"https://traccia.invalid/operation.schema.json#literal"},"enum":[{"$ref":"https://traccia.invalid/operation.schema.json#literal"}]}`)
	wrapped, err := argumentSchema(operations.Contract{Input: raw})
	if err != nil {
		t.Fatal(err)
	}
	value, err := operations.DecodeJSON(wrapped)
	if err != nil {
		t.Fatal(err)
	}
	input := value.(map[string]any)["properties"].(map[string]any)["input"].(map[string]any)
	if input["properties"].(map[string]any)["node"].(map[string]any)["$ref"] != "https://schemas.example.invalid/input#/$defs/node" {
		t.Fatal("retrieval alias not canonicalized")
	}
	for _, literal := range []any{input["default"], input["enum"].([]any)[0]} {
		if literal.(map[string]any)["$ref"] != "https://traccia.invalid/operation.schema.json#literal" {
			t.Fatal("literal data rewritten")
		}
	}
}
