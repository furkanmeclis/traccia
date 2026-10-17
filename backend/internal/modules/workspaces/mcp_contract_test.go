package workspaces_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/furkanmeclis/traccia/backend/internal/database/db"
	module "github.com/furkanmeclis/traccia/backend/internal/modules/workspaces"
	"github.com/furkanmeclis/traccia/backend/internal/modules/workspaces/repository"
	"github.com/furkanmeclis/traccia/backend/internal/modules/workspaces/usecase"
	"github.com/furkanmeclis/traccia/backend/internal/platform/authctx"
	"github.com/furkanmeclis/traccia/backend/internal/platform/contractcoverage"
	"github.com/furkanmeclis/traccia/backend/internal/platform/crypto"
	"github.com/furkanmeclis/traccia/backend/internal/platform/events"
	"github.com/furkanmeclis/traccia/backend/internal/platform/jwt"
	"github.com/furkanmeclis/traccia/backend/internal/platform/mcpbindings"
	"github.com/furkanmeclis/traccia/backend/internal/platform/operations"
	"github.com/furkanmeclis/traccia/backend/internal/platform/outbox"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// This fixture supplies a verified principal only at the test server boundary.
// It makes no claim to implement an external OAuth authentication flow.
type contractFixture struct {
	ctx      context.Context
	pool     *pgxpool.Pool
	actor    uuid.UUID
	registry *operations.Registry
	repo     *repository.Postgres
	mux      *http.ServeMux
	bearer   string
	document contractcoverage.Object
}

func newContractFixture(t *testing.T) *contractFixture {
	t.Helper()
	pool := isolatedDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	t.Cleanup(cancel)
	actor := uuid.New()
	if _, err := pool.Exec(ctx, "INSERT INTO users(uuid,email,password_hash,name,surname,status) VALUES($1,$2,'unused','Contract','Owner','active')", actor, actor.String()+"@example.test"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.New(pool).BootstrapProductOwner(ctx, actor); err != nil {
		t.Fatal(err)
	}
	box, err := crypto.NewSecretBox("12345678901234567890123456789012")
	if err != nil {
		t.Fatal(err)
	}
	repo := repository.New(pool, db.New(pool), box)
	registry, err := usecase.New(repo).Registry()
	if err != nil {
		t.Fatal(err)
	}
	tokens, err := jwt.NewManager("contract-test-signing-secret", time.Hour, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	bearer, _, err := tokens.IssueAccess(jwt.AccessInput{UserID: actor})
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	if err := module.RegisterRegistryRoutes(mux, registry, tokens, loader{}); err != nil {
		t.Fatal(err)
	}
	document, err := contractcoverage.ReadOpenAPI("../../../docs/openapi.yaml")
	if err != nil {
		t.Fatal(err)
	}
	return &contractFixture{document: document, ctx: authctx.WithPrincipal(ctx, authctx.Principal{UserID: actor}), pool: pool, actor: actor, registry: registry, repo: repo, mux: mux, bearer: bearer}
}
func contractJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
func (f *contractFixture) http(t *testing.T, method, path, token, key string, input any, status int, code operations.Code) []byte {
	t.Helper()
	var body []byte
	if input != nil {
		body = contractJSON(t, input)
	}
	req := httptest.NewRequest(method, path, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if key != "" {
		req.Header.Set("Idempotency-Key", key)
	}
	res := httptest.NewRecorder()
	f.mux.ServeHTTP(res, req)
	// Validate the actual envelope against the generated OpenAPI operation.
	for _, contract := range f.registry.Catalog() {
		if contract.Metadata.HTTPMethod != method {
			continue
		}
		matched := matchContractPath(contract.Metadata.HTTPPath, req.URL.Path)
		if matched {
			if err := contractcoverage.ValidateHTTP(f.document, contract.Metadata.ID, res.Code, res.Body.Bytes()); err != nil {
				t.Fatalf("%s response violates documented contract", contract.Metadata.ID)
			}
			break
		}
	}
	// Do not include response bodies: credential results contain one-time secrets.
	if res.Code != status {
		t.Fatalf("%s %s status=%d want=%d", method, path, res.Code, status)
	}
	var result struct {
		Success bool            `json:"success"`
		Data    json.RawMessage `json:"data"`
		Error   struct {
			Code operations.Code `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Error.Code != code || result.Success != (code == "") {
		t.Fatalf("HTTP success=%v error=%s want=%s", result.Success, result.Error.Code, code)
	}
	if res.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("missing private result cache policy")
	}
	return result.Data
}
func (f *contractFixture) client(t *testing.T, serverCtx context.Context) *mcp.ClientSession {
	t.Helper()
	server, err := mcpbindings.NewServer(f.registry, nil)
	if err != nil {
		t.Fatal(err)
	}
	st, ct := mcp.NewInMemoryTransports()
	ss, err := server.Connect(serverCtx, st, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ss.Close() })
	client := mcp.NewClient(&mcp.Implementation{Name: "acceptance", Version: "1"}, nil)
	cs, err := client.Connect(f.ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cs.Close() })
	return cs
}
func toolCall(t *testing.T, ctx context.Context, cs *mcp.ClientSession, name string, w uuid.UUID, key string, input any, code operations.Code) []byte {
	t.Helper()
	args := map[string]any{"input": input}
	if w != uuid.Nil {
		args["workspace_id"] = w.String()
	}
	if key != "" {
		args["idempotency_key"] = key
	}
	result, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: json.RawMessage(contractJSON(t, args))})
	if err != nil {
		t.Fatalf("%s protocol error: %v", name, err)
	}
	if len(result.Content) != 1 {
		t.Fatalf("%s content count=%d", name, len(result.Content))
	}
	content, ok := result.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("%s missing JSON text", name)
	}
	raw := []byte(content.Text)
	if !json.Valid(raw) {
		t.Fatalf("%s invalid JSON result", name)
	}
	if result.IsError != (code != "") {
		t.Fatalf("%s error=%v want=%s", name, result.IsError, code)
	}
	if code != "" {
		var e struct {
			Code operations.Code `json:"code"`
		}
		if err := json.Unmarshal(raw, &e); err != nil {
			t.Fatal(err)
		}
		if e.Code != code {
			t.Fatalf("%s code=%s want=%s", name, e.Code, code)
		}
	}
	// Content text is the exact application JSON. StructuredContent is decoded by
	// the SDK into any and is deliberately never remarshaled as fidelity evidence.
	return raw
}
func decodeContract[T any](t *testing.T, raw []byte) T {
	t.Helper()
	var out T
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	return out
}
func (f *contractFixture) create(t *testing.T, cs *mcp.ClientSession, key string) uuid.UUID {
	raw := toolCall(t, f.ctx, cs, "create_workspace", uuid.Nil, key, usecase.CreateInput{Name: key}, "")
	return decodeContract[usecase.WorkspaceOutput](t, raw).Workspace.ID
}
func (f *contractFixture) mint(t *testing.T, cs *mcp.ClientSession, w uuid.UUID, audience string, scopes []string) usecase.CreateCredentialOutput {
	input := usecase.CreateCredentialInput{Name: "Acceptance credential", Audience: audience, Scopes: scopes, ExpiresAt: time.Now().UTC().Add(time.Hour).Truncate(time.Second)}
	return decodeContract[usecase.CreateCredentialOutput](t, toolCall(t, f.ctx, cs, "create_workspace_credential", w, uuid.NewString(), input, ""))
}

func TestMCPProductionContractsAndHTTPRetries(t *testing.T) {
	f := newContractFixture(t)
	cs := f.client(t, f.ctx)
	tools, err := cs.ListTools(f.ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	catalog := f.registry.Catalog()
	if len(tools.Tools) != 10 || len(catalog) != 10 {
		t.Fatal("production ten-operation discovery incomplete")
	}
	byName := map[string]*mcp.Tool{}
	for _, tool := range tools.Tools {
		byName[tool.Name] = tool
	}
	for _, c := range catalog {
		tool := byName[c.Metadata.MCPName]
		if tool == nil || tool.Meta["traccia/operation_id"] != c.Metadata.ID || tool.InputSchema == nil || tool.OutputSchema == nil || tool.Meta["traccia/error_schema"] == nil {
			t.Fatalf("discovery contract missing for %s", c.Metadata.ID)
		}
	}
	body := usecase.CreateInput{Name: "Ortak çalışma 🐾"}
	raw := f.http(t, "POST", "/v1/workspaces", f.bearer, "cross-create", body, 201, "")
	replay := toolCall(t, f.ctx, cs, "create_workspace", uuid.Nil, "cross-create", body, "")
	if !bytes.Equal(raw, replay) {
		t.Fatal("cross-channel create bytes changed")
	}
	w := decodeContract[usecase.WorkspaceOutput](t, raw).Workspace.ID
	path := "/v1/workspaces/" + w.String()
	toolCall(t, f.ctx, cs, "create_workspace", uuid.Nil, "cross-create", usecase.CreateInput{Name: "Changed"}, operations.IdempotencyConflict)
	f.http(t, "POST", "/v1/workspaces", f.bearer, "cross-create", usecase.CreateInput{Name: "Changed"}, 409, operations.IdempotencyConflict)
	get := toolCall(t, f.ctx, cs, "get_workspace", w, "", usecase.EmptyInput{}, "")
	if !bytes.Equal(get, f.http(t, "GET", path, f.bearer, "", nil, 200, "")) {
		t.Fatal("workspace get differs")
	}
	toolCall(t, f.ctx, cs, "list_workspaces", uuid.Nil, "", usecase.ListInput{Limit: 1}, "")
	// The mounted adapter must enforce its header and query contract before dispatch.
	f.http(t, "PATCH", path, f.bearer, "", usecase.UpdateInput{Name: "Missing key", ExpectedVersion: 1}, 400, operations.Validation)
	f.http(t, "GET", "/v1/workspaces?limit=1&limit=2", f.bearer, "", nil, 400, operations.Validation)
	f.http(t, "GET", "/v1/workspaces?unexpected=value", f.bearer, "", nil, 400, operations.Validation)
	for _, raw := range []json.RawMessage{
		json.RawMessage(`{"name":"One","name":"Two","expected_version":1}`),
		json.RawMessage(`{"name":"\uD800","expected_version":1}`),
		json.RawMessage(`{"name":"Unknown","expected_version":1,"actor_id":"00000000-0000-0000-0000-000000000001"}`),
	} {
		f.http(t, "PATCH", path, f.bearer, uuid.NewString(), raw, 400, operations.Validation)
	}
	edit := usecase.UpdateInput{Name: "Düzenleme 🚀", ExpectedVersion: 1}
	update := toolCall(t, f.ctx, cs, "update_workspace", w, "cross-update", edit, "")
	if !bytes.Equal(update, f.http(t, "PATCH", path, f.bearer, "cross-update", edit, 200, "")) {
		t.Fatal("update retry differs")
	}
	f.http(t, "PATCH", path, f.bearer, "stale-http", edit, 409, operations.VersionConflict)
	toolCall(t, f.ctx, cs, "update_workspace", w, "stale-mcp", edit, operations.VersionConflict)
	f.http(t, "PATCH", path, f.bearer, "cross-update", usecase.UpdateInput{Name: "Changed", ExpectedVersion: 1}, 409, operations.IdempotencyConflict)
	credential := usecase.CreateCredentialInput{Name: "Secret replay", Audience: "mcp", Scopes: []string{"workspace.read"}, ExpiresAt: time.Now().UTC().Add(time.Hour).Truncate(time.Second)}
	secret := f.http(t, "POST", path+"/credentials", f.bearer, "cross-secret", credential, 201, "")
	if !bytes.Equal(secret, toolCall(t, f.ctx, cs, "create_workspace_credential", w, "cross-secret", credential, "")) {
		t.Fatal("sensitive cross-channel replay differs")
	}
	minted := decodeContract[usecase.CreateCredentialOutput](t, secret)
	var stored []byte
	var encrypted bool
	if err := f.pool.QueryRow(f.ctx, "SELECT result,encrypted FROM operation_receipts WHERE idempotency_key='cross-secret'").Scan(&stored, &encrypted); err != nil {
		t.Fatal(err)
	}
	if !encrypted || bytes.Contains(stored, []byte(minted.Token)) {
		t.Fatal("sensitive receipt stored plaintext")
	}
	listed := toolCall(t, f.ctx, cs, "list_workspace_credentials", w, "", usecase.ListInput{Limit: 100}, "")
	if !bytes.Equal(listed, f.http(t, "GET", path+"/credentials?limit=100", f.bearer, "", nil, 200, "")) || bytes.Contains(listed, []byte(minted.Token)) {
		t.Fatal("credential list differs or leaks secret")
	}
	revoked := toolCall(t, f.ctx, cs, "revoke_workspace_credential", w, "cross-revoke", usecase.RevokeInput{CredentialID: minted.Credential.ID}, "")
	if !bytes.Equal(revoked, f.http(t, "DELETE", path+"/credentials/"+minted.Credential.ID.String(), f.bearer, "cross-revoke", nil, 200, "")) {
		t.Fatal("credential path or retry mapping differs")
	}
	// Use a real failed consumer receipt, rather than replaying an artificial success.
	consumers, err := outbox.NewRegistry(outbox.Consumer{Name: "workspace.activity", Version: 1, Handle: func(context.Context, events.Event) error { return fmt.Errorf("synthetic consumer failure") }})
	if err != nil {
		t.Fatal(err)
	}
	store := outbox.NewStore(f.pool, db.New(f.pool))
	worker := outbox.NewWorker(store, consumers, nil)
	if n, err := worker.Drain(f.ctx); err != nil || n != 0 {
		t.Fatalf("failed-consumer drain=%d err=%v", n, err)
	}
	deliveries := decodeContract[usecase.DeliveryList](t, toolCall(t, f.ctx, cs, "list_workspace_deliveries", w, "", usecase.ListInput{Limit: 100}, ""))
	var failed uuid.UUID
	for _, d := range deliveries.Items {
		if d.Attempts == 1 && d.ErrorCode == "consumer_failed" {
			failed = d.ID
		}
	}
	if failed == uuid.Nil {
		t.Fatal("real failed delivery missing")
	}
	replayInput := usecase.ReplayDeliveryInput{DeliveryID: failed}
	reset := f.http(t, "POST", path+"/deliveries/replay", f.bearer, "cross-replay", replayInput, 201, "")
	if !bytes.Equal(reset, toolCall(t, f.ctx, cs, "replay_workspace_delivery", w, "cross-replay", replayInput, "")) {
		t.Fatal("delivery replay differs")
	}
	production, err := f.repo.DurableConsumers()
	if err != nil {
		t.Fatal(err)
	}
	worker = outbox.NewWorker(store, production, nil)
	for range 2 {
		if n, err := worker.Drain(f.ctx); err != nil || n != 1 {
			t.Fatalf("activity drain=%d err=%v", n, err)
		}
	}
	for _, pair := range []struct{ tool, suffix string }{{"list_workspace_activity", "/activity"}, {"list_workspace_deliveries", "/deliveries"}} {
		actual := toolCall(t, f.ctx, cs, pair.tool, w, "", usecase.ListInput{Limit: 1}, "")
		expected := f.http(t, "GET", path+pair.suffix+"?limit=1", f.bearer, "", nil, 200, "")
		if !bytes.Equal(actual, expected) {
			t.Fatalf("%s route/query mapping differs", pair.tool)
		}
	}
	for _, table := range []string{"workspace_history", "outbox_events", "event_deliveries", "workspace_activity"} {
		var count int
		if err := f.pool.QueryRow(f.ctx, "SELECT count(*) FROM "+table).Scan(&count); err != nil || count != 2 {
			t.Fatalf("%s count=%d want=2 err=%v", table, count, err)
		}
	}
}

func TestMCPCurrentAuthorizationOnExistingSessions(t *testing.T) {
	f := newContractFixture(t)
	human := f.client(t, f.ctx)
	w := f.create(t, human, "Authorization")
	other := f.create(t, human, "Other workspace")
	path := "/v1/workspaces/" + w.String()
	production, err := f.repo.DurableConsumers()
	if err != nil {
		t.Fatal(err)
	}
	worker := outbox.NewWorker(outbox.NewStore(f.pool, db.New(f.pool)), production, nil)
	for range 2 {
		if n, err := worker.Drain(f.ctx); err != nil || n != 1 {
			t.Fatalf("cursor fixture projection=%d err=%v", n, err)
		}
	}
	for _, c := range []struct{ tool, suffix, query string }{
		{"list_workspace_activity", "/activity", "SELECT uuid FROM workspace_history WHERE workspace_uuid=$1 LIMIT 1"},
		{"list_workspace_deliveries", "/deliveries", "SELECT uuid FROM event_deliveries WHERE workspace_uuid=$1 LIMIT 1"},
	} {
		var foreign uuid.UUID
		if err := f.pool.QueryRow(f.ctx, c.query, other).Scan(&foreign); err != nil {
			t.Fatal(err)
		}
		input := usecase.ListInput{Limit: 100, After: foreign.String()}
		raw := toolCall(t, f.ctx, human, c.tool, w, "", input, "")
		if !bytes.Equal(raw, f.http(t, "GET", path+c.suffix+"?limit=100&after="+foreign.String(), f.bearer, "", nil, 200, "")) {
			t.Fatal("foreign cursor transport mismatch")
		}
		var page struct {
			Items   []json.RawMessage `json:"items"`
			HasMore bool              `json:"has_more"`
		}
		if err := json.Unmarshal(raw, &page); err != nil {
			t.Fatal(err)
		}
		if len(page.Items) != 0 || page.HasMore {
			t.Fatal("cursor resolved across workspace boundary")
		}
	}
	all := []string{"workspace.read", "workspace.write", "credentials.read", "credentials.manage"}
	a := f.mint(t, human, w, "mcp", all)
	b := f.mint(t, human, w, "mcp", all)
	api := f.mint(t, human, w, "api", all)
	ctx := context.Background()
	ac := f.client(t, usecase.WithToolCredential(ctx, a.Token))
	bc := f.client(t, usecase.WithToolCredential(ctx, b.Token))
	apic := f.client(t, usecase.WithToolCredential(ctx, api.Token))
	get := func(cs *mcp.ClientSession, code operations.Code) {
		t.Helper()
		toolCall(t, f.ctx, cs, "get_workspace", w, "", usecase.EmptyInput{}, code)
	}
	get(ac, "")
	get(bc, "")
	get(apic, operations.Forbidden)
	f.http(t, "GET", path, a.Token, "", nil, 403, operations.Forbidden)
	f.http(t, "GET", path, api.Token, "", nil, 200, "")
	toolCall(t, f.ctx, ac, "get_workspace", other, "", usecase.EmptyInput{}, operations.Forbidden)
	f.http(t, "GET", "/v1/workspaces/"+other.String(), api.Token, "", nil, 403, operations.Forbidden)
	toolCall(t, f.ctx, ac, "list_workspaces", uuid.Nil, "", usecase.ListInput{Limit: 20}, operations.Forbidden)
	toolCall(t, f.ctx, ac, "create_workspace", uuid.Nil, "pat-account", usecase.CreateInput{Name: "Denied"}, operations.Forbidden)
	limited := f.mint(t, human, w, "mcp", []string{"workspace.read"})
	lc := f.client(t, usecase.WithToolCredential(ctx, limited.Token))
	restricted := decodeContract[usecase.WorkspaceOutput](t, toolCall(t, f.ctx, lc, "get_workspace", w, "", usecase.EmptyInput{}, ""))
	if restricted.Capabilities.Update || restricted.Capabilities.ReadCredentials || restricted.Capabilities.ManageCredentials {
		t.Fatal("read token gained owner capabilities")
	}
	toolCall(t, f.ctx, lc, "update_workspace", w, "limited-write", usecase.UpdateInput{Name: "Denied", ExpectedVersion: 1}, operations.Forbidden)
	toolCall(t, f.ctx, lc, "list_workspace_credentials", w, "", usecase.ListInput{Limit: 20}, operations.Forbidden)
	f.http(t, "DELETE", path+"/credentials/"+a.Credential.ID.String(), f.bearer, "revoke-a", nil, 200, "")
	get(ac, operations.Unauthenticated)
	get(bc, "")
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := f.pool.Exec(f.ctx, sql, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec("UPDATE workspace_tool_credentials SET created_at=clock_timestamp()-interval '1 hour', expires_at=clock_timestamp()-interval '1 second' WHERE uuid=$1", b.Credential.ID)
	get(bc, operations.Unauthenticated)
	exec("UPDATE workspace_tool_credentials SET expires_at=clock_timestamp()+interval '1 hour' WHERE uuid=$1", b.Credential.ID)
	get(bc, "")
	exec("UPDATE workspace_members SET role='member' WHERE workspace_id=(SELECT id FROM workspaces WHERE uuid=$1)", w)
	get(bc, "")
	toolCall(t, f.ctx, bc, "list_workspace_deliveries", w, "", usecase.ListInput{Limit: 20}, operations.Forbidden)
	toolCall(t, f.ctx, bc, "update_workspace", w, "demoted", usecase.UpdateInput{Name: "Denied", ExpectedVersion: 1}, operations.Forbidden)
	exec("UPDATE workspace_members SET role='owner',active=false WHERE workspace_id=(SELECT id FROM workspaces WHERE uuid=$1)", w)
	get(bc, operations.Unauthenticated)
	toolCall(t, f.ctx, human, "get_workspace", w, "", usecase.EmptyInput{}, operations.Forbidden)
	exec("UPDATE workspace_members SET active=true WHERE workspace_id=(SELECT id FROM workspaces WHERE uuid=$1)", w)
	get(bc, "")
	exec("UPDATE users SET status='disabled' WHERE uuid=$1", f.actor)
	get(bc, operations.Unauthenticated)
	toolCall(t, f.ctx, human, "get_workspace", w, "", usecase.EmptyInput{}, operations.Forbidden)
	f.http(t, "GET", path, api.Token, "", nil, 401, operations.Unauthenticated)
}

func TestMCPHTTPCompletePaginationBeyondOneHundred(t *testing.T) {
	f := newContractFixture(t)
	cs := f.client(t, f.ctx)
	w := f.create(t, cs, "Pagination target")
	path := "/v1/workspaces/" + w.String()
	// Seed through the production repository/service without browser round trips.
	for i := range 125 {
		if _, err := f.repo.Create(f.ctx, f.actor, fmt.Sprintf("Workspace %03d", i)); err != nil {
			t.Fatal(err)
		}
		hash := sha256.Sum256([]byte(fmt.Sprintf("fixture-credential-%d", i)))
		if _, err := f.repo.CreateCredential(f.ctx, w, f.actor, usecase.CreateCredentialInput{Name: fmt.Sprintf("Credential %03d", i), Audience: "mcp", Scopes: []string{"workspace.read"}, ExpiresAt: time.Now().Add(time.Hour)}, hash[:]); err != nil {
			t.Fatal(err)
		}
		input := contractJSON(t, usecase.UpdateInput{Name: fmt.Sprintf("Revision %03d", i), ExpectedVersion: int64(i + 1)})
		if _, err := f.registry.Execute(f.ctx, operations.Call{OperationID: "workspaces.update", Channel: operations.API, WorkspaceID: w, IdempotencyKey: fmt.Sprintf("seed-%d", i), Input: input}); err != nil {
			t.Fatal(err)
		}
	}
	production, err := f.repo.DurableConsumers()
	if err != nil {
		t.Fatal(err)
	}
	worker := outbox.NewWorker(outbox.NewStore(f.pool, db.New(f.pool)), production, nil)
	for range 126 {
		if n, err := worker.Drain(f.ctx); err != nil || n != 1 {
			t.Fatalf("fixture projection=%d err=%v", n, err)
		}
	}
	cases := []struct {
		tool, path, query string
		workspace         uuid.UUID
		want              int
	}{
		{"list_workspaces", "/v1/workspaces", "SELECT uuid FROM workspaces ORDER BY uuid", uuid.Nil, 126},
		{"list_workspace_credentials", path + "/credentials", "SELECT c.uuid FROM workspace_tool_credentials c JOIN workspaces w ON w.id=c.workspace_id WHERE w.uuid=$1 ORDER BY c.uuid", w, 125},
		{"list_workspace_activity", path + "/activity", "SELECT h.uuid FROM workspace_activity a JOIN workspace_history h ON h.id=a.id WHERE a.workspace_uuid=$1 ORDER BY a.id DESC", w, 126},
		{"list_workspace_deliveries", path + "/deliveries", "SELECT uuid FROM event_deliveries WHERE workspace_uuid=$1 ORDER BY id DESC", w, 126},
	}
	for _, c := range cases {
		t.Run(c.tool, func(t *testing.T) {
			expected := []uuid.UUID{}
			var args []any
			if c.workspace != uuid.Nil {
				args = []any{c.workspace}
			}
			rows, err := f.pool.Query(f.ctx, c.query, args...)
			if err != nil {
				t.Fatal(err)
			}
			for rows.Next() {
				var id uuid.UUID
				if err := rows.Scan(&id); err != nil {
					t.Fatal(err)
				}
				expected = append(expected, id)
			}
			rows.Close()
			if err := rows.Err(); err != nil {
				t.Fatal(err)
			}
			if len(expected) != c.want {
				t.Fatalf("fixture count=%d want=%d", len(expected), c.want)
			}
			observed := []uuid.UUID{}
			after := ""
			pages := 0
			for {
				pages++
				if pages > 3 {
					t.Fatal("pagination did not terminate")
				}
				input := usecase.ListInput{Limit: 100, After: after}
				raw := toolCall(t, f.ctx, cs, c.tool, c.workspace, "", input, "")
				httpRaw := f.http(t, "GET", c.path+"?limit=100&after="+url.QueryEscape(after), f.bearer, "", nil, 200, "")
				if !bytes.Equal(raw, httpRaw) {
					t.Fatal("HTTP/MCP page bytes differ")
				}
				var page struct {
					Items []struct {
						ID uuid.UUID `json:"id"`
					} `json:"items"`
					HasMore    bool   `json:"has_more"`
					NextCursor string `json:"next_cursor"`
				}
				if err := json.Unmarshal(raw, &page); err != nil {
					t.Fatal(err)
				}
				for _, item := range page.Items {
					observed = append(observed, item.ID)
				}
				if !page.HasMore {
					if page.NextCursor != "" {
						t.Fatal("terminal cursor not empty")
					}
					break
				}
				if len(page.Items) != 100 || page.NextCursor == "" || page.NextCursor == after {
					t.Fatal("incomplete or stalled page")
				}
				after = page.NextCursor
			}
			if pages != 2 || !reflect.DeepEqual(expected, observed) {
				t.Fatal("full pagination skipped, repeated or reordered IDs")
			}
			f.http(t, "GET", c.path+"?limit=101", f.bearer, "", nil, 400, operations.Validation)
			toolCall(t, f.ctx, cs, c.tool, c.workspace, "", usecase.ListInput{Limit: 101}, operations.Validation)
		})
	}
}

// Match metadata templates solely to select the documented response contract;
// requests themselves always pass through the production ServeMux.
func matchContractPath(template, path string) bool {
	a, b := strings.Split(template, "/"), strings.Split(path, "/")
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if strings.HasPrefix(a[i], "{") {
			continue
		}
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
