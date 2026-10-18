package products_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/url"

	catalog "github.com/furkanmeclis/traccia/backend/internal/modules/products/usecase"
	module "github.com/furkanmeclis/traccia/backend/internal/modules/workspaces"
	ws "github.com/furkanmeclis/traccia/backend/internal/modules/workspaces/usecase"
	"github.com/furkanmeclis/traccia/backend/internal/platform/authctx"
	"github.com/furkanmeclis/traccia/backend/internal/platform/contractcoverage"
	"github.com/furkanmeclis/traccia/backend/internal/platform/jwt"
	"github.com/furkanmeclis/traccia/backend/internal/platform/mcpbindings"
	"github.com/furkanmeclis/traccia/backend/internal/platform/operations"
	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"net/http"
	"net/http/httptest"

	"strings"
	"sync"
	"testing"
	"time"
)

// Only the test boundary supplies a verified owner; SDK sessions exercise the
// production binding and credential checks without claiming an OAuth network flow.
type transportLoader struct{}

func (transportLoader) LoadPrincipal(_ *http.Request, c jwt.Claims) (authctx.Principal, error) {
	id, e := c.UserUUID()
	return authctx.Principal{UserID: id}, e
}

type productTransport struct {
	productFixture
	mux      *http.ServeMux
	bearer   string
	document contractcoverage.Object
}

var transportCatalog sync.Map

func newProductTransport(t *testing.T) *productTransport {
	t.Helper()
	f := newProductFixture(t)
	tokens, e := jwt.NewManager("team-contract-signing-secret", time.Hour, time.Hour)
	if e != nil {
		t.Fatal(e)
	}
	bearer, _, e := tokens.IssueAccess(jwt.AccessInput{UserID: f.actor})
	if e != nil {
		t.Fatal(e)
	}
	mux := http.NewServeMux()
	if e = module.RegisterRegistryRoutes(mux, f.registry, tokens, transportLoader{}); e != nil {
		t.Fatal(e)
	}
	doc, e := contractcoverage.ReadOpenAPI("../../../docs/openapi.yaml")
	if e != nil {
		t.Fatal(e)
	}
	return &productTransport{productFixture: f, mux: mux, bearer: bearer, document: doc}
}
func transportJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
func (f *productTransport) http(t *testing.T, method, path, token, key string, input any, status int, code operations.Code) []byte {
	t.Helper()
	var body []byte
	if input != nil {
		if raw, ok := input.(json.RawMessage); ok {
			body = raw
		} else {
			body = transportJSON(t, input)
		}
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
	if token != "" && res.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("missing private result cache policy")
	}
	return result.Data
}
func (f *productTransport) client(t *testing.T, serverCtx context.Context) *mcp.ClientSession {
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
	transportCatalog.Store(cs, f.registry.Catalog())
	t.Cleanup(func() { transportCatalog.Delete(cs) })
	return cs
}
func transportTool(t *testing.T, ctx context.Context, cs *mcp.ClientSession, name string, w uuid.UUID, key string, input any, code operations.Code) []byte {
	t.Helper()
	args := map[string]any{"input": input}
	if w != uuid.Nil {
		args["workspace_id"] = w.String()
	}
	if key != "" {
		args["idempotency_key"] = key
	}
	result, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: json.RawMessage(transportJSON(t, args))})
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
	catalog, _ := transportCatalog.Load(cs)
	for _, c := range catalog.([]operations.Contract) {
		if c.Metadata.MCPName == name {
			schema := c.Output
			if code != "" {
				schema = c.Error
			}
			var doc contractcoverage.Object
			if err := json.Unmarshal(schema, &doc); err != nil {
				t.Fatal(err)
			}
			if err := contractcoverage.Validate(doc, raw); err != nil {
				t.Fatalf("%s SDK text violates schema: %v", name, err)
			}
		}
	}
	// Content text is the exact application JSON. StructuredContent is decoded by
	// the SDK into any and is deliberately never remarshaled as fidelity evidence.
	return raw
}
func transportDecode[T any](t *testing.T, raw []byte) T {
	t.Helper()
	var out T
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	return out
}
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

// invoke exercises the HTTP decoder and the SDK's closed tool wrapper with the
// same application input. Writes replay one receipt across both transports.
func (f *productTransport) invoke(t *testing.T, cs *mcp.ClientSession, name, key string, input map[string]any) []byte {
	t.Helper()
	for _, c := range f.registry.Catalog() {
		if c.Metadata.MCPName != name {
			continue
		}
		path := strings.ReplaceAll(c.Metadata.HTTPPath, "{workspaceID}", f.w.String())
		body := map[string]any{}
		for k, v := range input {
			body[k] = v
		}
		for _, pair := range [][2]string{{"productID", "product_id"}, {"environmentID", "environment_id"}} {
			if strings.Contains(path, "{"+pair[0]+"}") {
				path = strings.ReplaceAll(path, "{"+pair[0]+"}", body[pair[1]].(string))
				delete(body, pair[1])
			}
		}
		var payload any = body
		status := 200
		if c.Metadata.HTTPMethod == "POST" {
			status = 201
		}
		if c.Metadata.HTTPMethod == "GET" {
			q := url.Values{}
			for k, v := range body {
				q.Set(k, fmt.Sprint(v))
			}
			path += "?" + q.Encode()
			payload = nil
		}
		raw := f.http(t, c.Metadata.HTTPMethod, path, f.bearer, key, payload, status, "")
		sdk := transportTool(t, f.ctx, cs, name, f.w, key, input, "")
		if !bytes.Equal(raw, sdk) {
			t.Fatalf("%s HTTP/SDK application JSON differs", name)
		}
		return raw
	}
	t.Fatalf("missing tool %s", name)
	return nil
}
func TestProductsTransportLifecycleAndContracts(t *testing.T) {
	f := newProductTransport(t)
	cs := f.client(t, f.ctx)
	tools, e := cs.ListTools(f.ctx, nil)
	if e != nil {
		t.Fatal(e)
	}
	names := map[string]*mcp.Tool{}
	for _, v := range tools.Tools {
		names[v.Name] = v
	}
	count := 0
	for _, c := range f.registry.Catalog() {
		if strings.HasPrefix(c.Metadata.ID, "products.") || strings.HasPrefix(c.Metadata.ID, "product_") {
			count++
			v := names[c.Metadata.MCPName]
			if v == nil || v.InputSchema == nil || v.OutputSchema == nil || v.Meta["traccia/error_schema"] == nil || v.Meta["traccia/operation_id"] != c.Metadata.ID {
				t.Fatal("incomplete catalog discovery")
			}
		}
	}
	if count != 14 {
		t.Fatalf("catalog count=%d", count)
	}
	text := "İstanbul 🧭 e\u0301 漢字\n<plate>\"&</plate>"
	in := map[string]any{"name": "Ürün 🧭", "summary": text, "repository_url": "http://localhost:3000/repo", "repository_provider": "GitHub", "repository_reference": "refs/heads/çalışma"}
	p := transportDecode[catalog.ProductOutput](t, f.invoke(t, cs, "create_product", "create", in)).Product
	pid := p.ID.String()
	if p.Summary != text || p.RepositoryReference != "refs/heads/çalışma" {
		t.Fatal("product fidelity")
	}
	var n int
	if e = f.pool.QueryRow(f.ctx, "SELECT count(*) FROM workspace_history WHERE product_uuid=$1", p.ID).Scan(&n); e != nil || n != 1 {
		t.Fatalf("create history=%d err=%v", n, e)
	}
	in["name"] = "Altered"
	transportTool(t, f.ctx, cs, "create_product", f.w, "create", in, operations.IdempotencyConflict)
	f.http(t, "POST", "/v1/workspaces/"+f.w.String()+"/products", f.bearer, "create", in, 409, operations.IdempotencyConflict)
	f.invoke(t, cs, "get_product", "", map[string]any{"product_id": pid})
	f.invoke(t, cs, "list_products", "", map[string]any{"limit": 100, "after": "", "q": "Ürün", "archived": false, "project_id": ""})
	updated := transportDecode[catalog.ProductOutput](t, f.invoke(t, cs, "update_product", "update", map[string]any{"product_id": pid, "name": "Ürün güncel", "summary": text, "repository_url": nil, "repository_provider": nil, "repository_reference": nil, "expected_version": 1})).Product
	if updated.RepositoryURL != "" || updated.RepositoryProvider != "" || updated.RepositoryReference != "" {
		t.Fatal("references did not clear")
	}
	child := map[string]any{"product_id": pid, "name": "Yerel 🧭", "kind": "local", "url": "http://127.0.0.1:3000/çalışma", "deploy_url": "http://localhost:8080/deploy", "deploy_provider": "Custom", "deploy_reference": "refs/heads/ana", "summary": text, "expected_product_version": 2}
	env := transportDecode[catalog.EnvironmentOutput](t, f.invoke(t, cs, "create_product_environment", "env-create", child))
	eid := env.Environment.ID.String()
	if env.Environment.Summary != text || env.ProductVersion != 3 {
		t.Fatal("environment fidelity/version")
	}
	f.invoke(t, cs, "get_product_environment", "", map[string]any{"product_id": pid, "environment_id": eid})
	f.invoke(t, cs, "list_product_environment", "", map[string]any{"product_id": pid, "limit": 100, "after": "", "q": "Yerel", "kind": "local", "archived": false})
	child["environment_id"] = eid
	child["expected_version"] = 1
	child["expected_product_version"] = 3
	child["deploy_url"] = nil
	child["deploy_provider"] = nil
	child["deploy_reference"] = nil
	env = transportDecode[catalog.EnvironmentOutput](t, f.invoke(t, cs, "update_product_environment", "env-update", child))
	if env.Environment.DeployURL != "" || env.Environment.DeployProvider != "" || env.Environment.DeployReference != "" || env.ProductVersion != 4 {
		t.Fatal("deploy clear/version")
	}
	transportTool(t, f.ctx, cs, "update_product_environment", f.w, "stale-parent", child, operations.VersionConflict)
	stale := map[string]any{}
	for k, v := range child {
		if k != "product_id" && k != "environment_id" {
			stale[k] = v
		}
	}
	f.http(t, "PATCH", "/v1/workspaces/"+f.w.String()+"/products/"+pid+"/environments/"+eid, f.bearer, "stale-parent-api", stale, 409, operations.VersionConflict)
	child["expected_product_version"] = 4
	transportTool(t, f.ctx, cs, "update_product_environment", f.w, "stale-child", child, operations.VersionConflict)
	arch := map[string]any{"product_id": pid, "environment_id": eid, "expected_version": 2, "expected_product_version": 4}
	f.invoke(t, cs, "archive_product_environment", "env-archive", arch)
	archived := transportDecode[catalog.ProductOutput](t, f.invoke(t, cs, "archive_product", "archive", map[string]any{"product_id": pid, "expected_version": 5}))
	if !archived.Capabilities.Restore || archived.Capabilities.Manage {
		t.Fatal("owner restore absent")
	}
	arch["expected_version"] = 3
	arch["expected_product_version"] = 6
	transportTool(t, f.ctx, cs, "restore_product_environment", f.w, "blocked-child", arch, operations.Conflict)
	transportTool(t, f.ctx, cs, "archive_product_environment", f.w, "blocked-archive", arch, operations.Conflict)
	child["expected_version"] = 3
	child["expected_product_version"] = 6
	transportTool(t, f.ctx, cs, "update_product_environment", f.w, "blocked-update", child, operations.Conflict)
	transportTool(t, f.ctx, cs, "create_product_environment", f.w, "blocked-create", map[string]any{"product_id": pid, "name": "Blocked", "kind": "local", "url": "http://localhost:3000", "deploy_url": "", "deploy_provider": "", "deploy_reference": "", "summary": "", "expected_product_version": 6}, operations.Conflict)
	transportTool(t, f.ctx, cs, "set_product_projects", f.w, "blocked-links", map[string]any{"product_id": pid, "project_ids": []string{}, "expected_product_version": 6}, operations.Conflict)
	blocked := transportDecode[catalog.EnvironmentOutput](t, f.invoke(t, cs, "get_product_environment", "", map[string]any{"product_id": pid, "environment_id": eid}))
	if blocked.Capabilities.Restore || blocked.Capabilities.Manage || blocked.ProductVersion != 6 {
		t.Fatal("archived parent child capabilities/version")
	}
	f.invoke(t, cs, "restore_product", "restore", map[string]any{"product_id": pid, "expected_version": 6})
	retained := transportDecode[catalog.EnvironmentOutput](t, f.invoke(t, cs, "get_product_environment", "", map[string]any{"product_id": pid, "environment_id": eid}))
	if !retained.Environment.Archived || !retained.Capabilities.Restore || retained.ProductVersion != 7 {
		t.Fatal("parent restore changed independent archive")
	}
	arch["expected_product_version"] = 7
	f.invoke(t, cs, "restore_product_environment", "env-restore", arch)
	team := f.createTeam(t, "TP")
	project := f.createProject(t, team.ID)
	f.invoke(t, cs, "set_product_projects", "set", map[string]any{"product_id": pid, "project_ids": []string{project.ID.String()}, "expected_product_version": 8})
	links := transportDecode[catalog.ProductProjects](t, f.invoke(t, cs, "list_product_projects", "", map[string]any{"product_id": pid, "limit": 100, "after": "", "archived": false}))
	if len(links.Items) != 1 || links.ProductVersion != 9 {
		t.Fatal("link/version")
	}
	// Referenced project archival is distinct from removing the association.
	transportTool(t, f.ctx, cs, "archive_project", f.w, "archive-project", map[string]any{"project_id": project.ID.String(), "expected_version": 1}, "")
	links = transportDecode[catalog.ProductProjects](t, f.invoke(t, cs, "list_product_projects", "", map[string]any{"product_id": pid, "limit": 100, "after": "", "archived": false}))
	if len(links.Items) != 1 || !links.Items[0].Archived {
		t.Fatal("historical archived project hidden")
	}
	f.invoke(t, cs, "set_product_projects", "retain", map[string]any{"product_id": pid, "project_ids": []string{project.ID.String()}, "expected_product_version": 9})
	f.invoke(t, cs, "set_product_projects", "remove", map[string]any{"product_id": pid, "project_ids": []string{}, "expected_product_version": 10})
	links = transportDecode[catalog.ProductProjects](t, f.invoke(t, cs, "list_product_projects", "", map[string]any{"product_id": pid, "limit": 100, "after": "", "archived": true}))
	if len(links.Items) != 1 {
		t.Fatal("removed link history hidden")
	}
	transportTool(t, f.ctx, cs, "set_product_projects", f.w, "reject-inactive", map[string]any{"product_id": pid, "project_ids": []string{project.ID.String()}, "expected_product_version": 11}, operations.Conflict)
	if e = f.pool.QueryRow(f.ctx, "SELECT count(*) FROM workspace_history WHERE product_uuid=$1", p.ID).Scan(&n); e != nil || n != 11 {
		t.Fatalf("cross-channel history effects=%d want=11 err=%v", n, e)
	}
	const large int64 = 9007199254740993
	f.sql(t, "UPDATE products SET version=$2 WHERE uuid=$1", p.ID, large)
	raw := f.invoke(t, cs, "get_product", "", map[string]any{"product_id": pid})
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var exact map[string]any
	if e = dec.Decode(&exact); e != nil {
		t.Fatal(e)
	}
	if exact["product"].(map[string]any)["version"].(json.Number).String() != "9007199254740993" {
		t.Fatal("BIGINT rounded")
	}
	if bytes.Contains(raw, []byte("connection_status")) || bytes.Contains(raw, []byte("connected")) {
		t.Fatal("catalog claims live connectivity")
	}
}
func (f *productTransport) sql(t *testing.T, q string, args ...any) {
	t.Helper()
	if _, e := f.pool.Exec(f.ctx, q, args...); e != nil {
		t.Fatal(e)
	}
}

func TestProductsTransportValidationAndOwnership(t *testing.T) {
	f := newProductTransport(t)
	cs := f.client(t, f.ctx)
	p := f.createProduct(t, "First")
	q := f.createProduct(t, "Other")
	env := f.createEnvironment(t, p, "Child")
	base := "/v1/workspaces/" + f.w.String() + "/products"
	path := base + "/" + p.ID.String()
	for _, raw := range []string{`{"name":"A","name":"B","expected_version":2}`, `{"product_id":"` + p.ID.String() + `","name":"A","expected_version":2}`, `{"actor_id":"` + f.actor.String() + `","name":"A","expected_version":2}`, `{"name":`, `[]`} {
		f.http(t, "PATCH", path, f.bearer, uuid.NewString(), json.RawMessage(raw), 400, operations.Validation)
	}
	for _, query := range []string{"unknown=1", "limit=1&limit=2", "limit=101", "archived=yes", "kind=local", "after=bad"} {
		f.http(t, "GET", base+"?"+query, f.bearer, "", nil, 400, operations.Validation)
	}
	f.http(t, "GET", base+"/bad-uuid", f.bearer, "", nil, 400, operations.Validation)
	f.http(t, "GET", path, "", "", nil, 401, operations.Code("UNAUTHENTICATED"))
	wrong := map[string]any{"product_id": q.ID.String(), "environment_id": env.Environment.ID.String()}
	transportTool(t, f.ctx, cs, "get_product_environment", f.w, "", wrong, operations.NotFound)
	f.http(t, "GET", base+"/"+q.ID.String()+"/environments/"+env.Environment.ID.String(), f.bearer, "", nil, 404, operations.NotFound)
	for _, u := range []string{"javascript:alert(1)", "file:///a", "https://user:pass@example.test/", "http://localhost/?access_token=secret", "https://example.test/?X-Amz-Signature=secret"} {
		in := map[string]any{"name": "Invalid", "summary": "", "repository_url": u, "repository_provider": "", "repository_reference": ""}
		transportTool(t, f.ctx, cs, "create_product", f.w, uuid.NewString(), in, operations.Validation)
		f.http(t, "POST", base, f.bearer, uuid.NewString(), in, 400, operations.Validation)
	}
	result, e := cs.CallTool(f.ctx, &mcp.CallToolParams{Name: "get_product", Arguments: json.RawMessage(`{"workspace_id":"` + f.w.String() + `","input":{"product_id":"` + p.ID.String() + `"},"actor_id":"` + f.actor.String() + `"}`)})
	if e == nil && (result == nil || !result.IsError) {
		t.Fatal("spoofed SDK wrapper accepted")
	}
	// A different principal can read but cannot restore an archived product.
	member := uuid.New()
	f.sql(t, "INSERT INTO users(uuid,email,password_hash,name,surname,status) VALUES($1,$2,'unused','Member','User','active')", member, member.String()+"@example.test")
	f.sql(t, "INSERT INTO workspace_members(workspace_id,user_id,role) SELECT w.id,u.id,'member' FROM workspaces w,users u WHERE w.uuid=$1 AND u.uuid=$2", f.w, member)
	f.invoke(t, cs, "archive_product", "archive", map[string]any{"product_id": p.ID.String(), "expected_version": 2})
	tokens, e := jwt.NewManager("team-contract-signing-secret", time.Hour, time.Hour)
	if e != nil {
		t.Fatal(e)
	}
	bearer, _, e := tokens.IssueAccess(jwt.AccessInput{UserID: member})
	if e != nil {
		t.Fatal(e)
	}
	out := transportDecode[catalog.ProductOutput](t, f.http(t, "GET", path, bearer, "", nil, 200, ""))
	if out.Capabilities.Manage || out.Capabilities.Restore {
		t.Fatal("member restore capability")
	}
	f.http(t, "POST", path+"/restore", bearer, "denied", map[string]any{"expected_version": 3}, 403, operations.Forbidden)
	memberClient := f.client(t, authctx.WithPrincipal(context.Background(), authctx.Principal{UserID: member}))
	transportTool(t, f.ctx, memberClient, "restore_product", f.w, "denied-sdk", map[string]any{"product_id": p.ID.String(), "expected_version": 3}, operations.Forbidden)
	other := transportDecode[ws.WorkspaceOutput](t, transportTool(t, f.ctx, cs, "create_workspace", uuid.Nil, "other", ws.CreateInput{Name: "Other workspace"}, "")).Workspace.ID
	transportTool(t, f.ctx, cs, "get_product", other, "", map[string]any{"product_id": p.ID.String()}, operations.NotFound)
	f.http(t, "GET", "/v1/workspaces/"+other.String()+"/products/"+p.ID.String(), f.bearer, "", nil, 404, operations.NotFound)
	transportTool(t, f.ctx, memberClient, "get_product", other, "", map[string]any{"product_id": p.ID.String()}, operations.Forbidden)
}

func TestProductsTransportCurrentCredentials(t *testing.T) {
	f := newProductTransport(t)
	human := f.client(t, f.ctx)
	p := f.createProduct(t, "Credentials")
	mint := func(audience string, scopes []string) ws.CreateCredentialOutput {
		return transportDecode[ws.CreateCredentialOutput](t, transportTool(t, f.ctx, human, "create_workspace_credential", f.w, uuid.NewString(), ws.CreateCredentialInput{Name: "Catalog acceptance", Audience: audience, Scopes: scopes, ExpiresAt: time.Now().Add(time.Hour).UTC().Truncate(time.Second)}, ""))
	}
	api := mint("api", []string{"workspace.read", "workspace.write"})
	pat := mint("mcp", []string{"workspace.read", "workspace.write"})
	read := mint("mcp", []string{"workspace.read"})
	client := f.client(t, ws.WithToolCredential(context.Background(), pat.Token))
	readClient := f.client(t, ws.WithToolCredential(context.Background(), read.Token))
	apiClient := f.client(t, ws.WithToolCredential(context.Background(), api.Token))
	get := map[string]any{"product_id": p.ID.String()}
	path := "/v1/workspaces/" + f.w.String() + "/products/" + p.ID.String()
	transportTool(t, f.ctx, apiClient, "get_product", f.w, "", get, operations.Forbidden)
	f.http(t, "GET", path, pat.Token, "", nil, 403, operations.Forbidden)
	f.http(t, "GET", path, api.Token, "", nil, 200, "")
	limited := transportDecode[catalog.ProductOutput](t, transportTool(t, f.ctx, readClient, "get_product", f.w, "", get, ""))
	if limited.Capabilities.Manage || limited.Capabilities.Restore {
		t.Fatal("read credential write capability")
	}
	edit := map[string]any{"product_id": p.ID.String(), "name": "Authorized", "summary": "", "repository_url": "", "repository_provider": "", "repository_reference": "", "expected_version": 1}
	transportTool(t, f.ctx, readClient, "update_product", f.w, "read-write", edit, operations.Forbidden)
	transportTool(t, f.ctx, client, "update_product", f.w, "auth-replay", edit, "")
	replay := func(code operations.Code) {
		transportTool(t, f.ctx, client, "update_product", f.w, "auth-replay", edit, code)
	}
	f.sql(t, "UPDATE workspace_tool_credentials SET created_at=clock_timestamp()-interval '2 hours',expires_at=clock_timestamp()-interval '1 second' WHERE uuid=$1", pat.Credential.ID)
	replay(operations.Unauthenticated)
	f.sql(t, "UPDATE workspace_tool_credentials SET expires_at=clock_timestamp()+interval '1 hour' WHERE uuid=$1", pat.Credential.ID)
	replay("")
	f.sql(t, "UPDATE workspace_members SET active=false WHERE workspace_id=(SELECT id FROM workspaces WHERE uuid=$1)", f.w)
	replay(operations.Unauthenticated)
	f.sql(t, "UPDATE workspace_members SET active=true WHERE workspace_id=(SELECT id FROM workspaces WHERE uuid=$1)", f.w)
	replay("")
	f.sql(t, "UPDATE workspace_members SET role='member' WHERE workspace_id=(SELECT id FROM workspaces WHERE uuid=$1)", f.w)
	replay(operations.Forbidden)
	f.sql(t, "UPDATE workspace_members SET role='owner' WHERE workspace_id=(SELECT id FROM workspaces WHERE uuid=$1)", f.w)
	replay("")
	transportTool(t, f.ctx, human, "revoke_workspace_credential", f.w, "revoke", ws.RevokeInput{CredentialID: pat.Credential.ID}, "")
	replay(operations.Unauthenticated)
	body := map[string]any{"name": "API", "summary": "", "repository_url": "", "repository_provider": "", "repository_reference": "", "expected_version": 2}
	f.http(t, "PATCH", path, api.Token, "api-replay", body, 200, "")
	other := transportDecode[ws.WorkspaceOutput](t, transportTool(t, f.ctx, human, "create_workspace", uuid.Nil, "foreign", ws.CreateInput{Name: "Credential foreign workspace"}, "")).Workspace.ID
	f.http(t, "GET", "/v1/workspaces/"+other.String()+"/products/"+p.ID.String(), api.Token, "", nil, 403, operations.Forbidden)
	f.sql(t, "UPDATE users SET status='disabled' WHERE uuid=$1", f.actor)
	f.http(t, "PATCH", path, api.Token, "api-replay", body, 401, operations.Unauthenticated)
	transportTool(t, f.ctx, readClient, "get_product", f.w, "", get, operations.Unauthenticated)
}

// Cursor scopes are public hashes. Replacing an anchor must still be checked
// against immutable database ownership, independently of the hash metadata.
func forgedAnchor(t *testing.T, cursor string, id uuid.UUID) string {
	t.Helper()
	raw, e := base64.RawURLEncoding.DecodeString(cursor)
	if e != nil {
		t.Fatal(e)
	}
	var c map[string]any
	if e = json.Unmarshal(raw, &c); e != nil {
		t.Fatal(e)
	}
	c["anchor"] = id.String()
	return base64.RawURLEncoding.EncodeToString(transportJSON(t, c))
}
func TestProductsTransportCompletePaginationAndAnchors(t *testing.T) {
	f := newProductTransport(t)
	cs := f.client(t, f.ctx)
	p := f.createProduct(t, "Aggregate")
	q := f.createProduct(t, "Sibling")
	foreignEnv := f.createEnvironment(t, q, "Foreign")
	f.sql(t, "INSERT INTO products(workspace_id,name,archived) SELECT id,'Needle '||n,n>130 FROM workspaces CROSS JOIN generate_series(1,155)n WHERE uuid=$1", f.w)
	f.sql(t, "INSERT INTO product_environments(workspace_id,product_id,name,kind,url,archived) SELECT workspace_id,id,'Needle '||n,CASE WHEN n>155 THEN 'custom' ELSE 'local' END,'http://localhost:3000/'||n,n>130 AND n<=155 FROM products CROSS JOIN generate_series(1,164)n WHERE uuid=$1", p.ID)
	f.sql(t, "INSERT INTO projects(workspace_id,name) SELECT id,'Linked '||n FROM workspaces CROSS JOIN generate_series(1,155)n WHERE uuid=$1", f.w)
	f.sql(t, "INSERT INTO product_projects(workspace_id,product_id,project_id,archived) SELECT p.workspace_id,p.id,j.id,j.name ~ '^Linked (13[1-9]|14[0-9]|15[0-5])$' FROM products p JOIN projects j ON j.workspace_id=p.workspace_id WHERE p.uuid=$1", p.ID)
	var filterProject uuid.UUID
	if e := f.pool.QueryRow(f.ctx, "SELECT uuid FROM projects ORDER BY uuid LIMIT 1").Scan(&filterProject); e != nil {
		t.Fatal(e)
	}
	f.sql(t, "INSERT INTO product_projects(workspace_id,product_id,project_id) SELECT p.workspace_id,p.id,j.id FROM products p JOIN projects j ON j.workspace_id=p.workspace_id WHERE p.name LIKE 'Needle %' AND j.uuid=$1", filterProject)
	type page struct {
		Items []struct {
			ID uuid.UUID `json:"id"`
		} `json:"items"`
		HasMore        bool   `json:"has_more"`
		Next           string `json:"next_cursor"`
		ProductVersion int64  `json:"product_version"`
	}
	cases := []struct {
		name, tool string
		input      map[string]any
		count      int
	}{
		{"products-active", "list_products", map[string]any{"q": "Needle", "archived": false, "project_id": ""}, 130},
		{"products-archived", "list_products", map[string]any{"q": "Needle", "archived": true, "project_id": ""}, 25},
		{"products-project", "list_products", map[string]any{"q": "Needle", "archived": false, "project_id": filterProject.String()}, 130},
		{"env-local", "list_product_environment", map[string]any{"product_id": p.ID.String(), "q": "Needle", "kind": "local", "archived": false}, 130},
		{"env-archived", "list_product_environment", map[string]any{"product_id": p.ID.String(), "q": "Needle", "kind": "local", "archived": true}, 25},
		{"env-custom", "list_product_environment", map[string]any{"product_id": p.ID.String(), "q": "Needle", "kind": "custom", "archived": false}, 9},
		{"links-active", "list_product_projects", map[string]any{"product_id": p.ID.String(), "archived": false}, 130},
		{"links-removed", "list_product_projects", map[string]any{"product_id": p.ID.String(), "archived": true}, 25},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			seen := map[uuid.UUID]bool{}
			c.input["limit"] = 100
			c.input["after"] = ""
			for i := 0; ; i++ {
				if i > 3 {
					t.Fatal("cursor stalled")
				}
				out := transportDecode[page](t, f.invoke(t, cs, c.tool, "", c.input))
				if c.tool != "list_products" && out.ProductVersion != 1 {
					t.Fatal("snapshot product version")
				}
				for _, r := range out.Items {
					if seen[r.ID] {
						t.Fatal("duplicate page row")
					}
					seen[r.ID] = true
				}
				if !out.HasMore {
					if out.Next != "" {
						t.Fatal("terminal cursor")
					}
					break
				}
				if len(out.Items) != 100 || out.Next == "" || out.Next == c.input["after"] {
					t.Fatal("dropped/stalled page")
				}
				c.input["after"] = out.Next
			}
			if len(seen) != c.count {
				t.Fatalf("rows=%d want=%d", len(seen), c.count)
			}
		})
	}
	// Archive and rename the exact anchor through the real transports, then
	// continue the original active/search cursor without losing following rows.
	prodIn := map[string]any{"q": "Needle", "archived": false, "project_id": "", "limit": 1, "after": ""}
	first := transportDecode[page](t, f.invoke(t, cs, "list_products", "", prodIn))
	anchor := first.Items[0].ID
	f.invoke(t, cs, "update_product", "anchor-rename", map[string]any{"product_id": anchor.String(), "name": "Renamed anchor", "summary": "", "repository_url": "", "repository_provider": "", "repository_reference": "", "expected_version": 1})
	f.invoke(t, cs, "archive_product", "anchor-archive", map[string]any{"product_id": anchor.String(), "expected_version": 2})
	prodIn["after"] = first.Next
	prodIn["limit"] = 100
	remaining := 0
	for i := 0; ; i++ {
		if i > 3 {
			t.Fatal("anchor cursor stalled")
		}
		out := transportDecode[page](t, f.invoke(t, cs, "list_products", "", prodIn))
		remaining += len(out.Items)
		if !out.HasMore {
			break
		}
		prodIn["after"] = out.Next
	}
	if remaining != 129 {
		t.Fatalf("archived/renamed anchor remaining=%d", remaining)
	}
	envIn := map[string]any{"product_id": p.ID.String(), "q": "Needle", "kind": "local", "archived": false, "limit": 1, "after": ""}
	ef := transportDecode[page](t, f.invoke(t, cs, "list_product_environment", "", envIn))
	f.invoke(t, cs, "archive_product_environment", "env-anchor-archive", map[string]any{"product_id": p.ID.String(), "environment_id": ef.Items[0].ID.String(), "expected_version": 1, "expected_product_version": 1})
	envIn["after"] = ef.Next
	envIn["limit"] = 100
	remaining = 0
	for i := 0; ; i++ {
		if i > 3 {
			t.Fatal("environment cursor stalled")
		}
		out := transportDecode[page](t, f.invoke(t, cs, "list_product_environment", "", envIn))
		if out.ProductVersion != 2 {
			t.Fatal("environment snapshot stale")
		}
		remaining += len(out.Items)
		if !out.HasMore {
			break
		}
		envIn["after"] = out.Next
	}
	if remaining != 129 {
		t.Fatalf("archived environment anchor remaining=%d", remaining)
	}
	envIn["after"] = ef.Next
	envIn["kind"] = "custom"
	transportTool(t, f.ctx, cs, "list_product_environment", f.w, "", envIn, operations.Validation)
	envIn["kind"] = "local"
	// An anchor may cease matching filters, but must actually belong to the
	// current aggregate. Neither unknown nor sibling anchors may be forged in.
	envBase := "/v1/workspaces/" + f.w.String() + "/products/" + p.ID.String() + "/environments"
	for _, id := range []uuid.UUID{foreignEnv.Environment.ID, uuid.New()} {
		envIn["after"] = forgedAnchor(t, ef.Next, id)
		transportTool(t, f.ctx, cs, "list_product_environment", f.w, "", envIn, operations.Validation)
		values := url.Values{}
		for k, v := range envIn {
			if k != "product_id" {
				values.Set(k, fmt.Sprint(v))
			}
		}
		f.http(t, "GET", envBase+"?"+values.Encode(), f.bearer, "", nil, 400, operations.Validation)
	}
	other := transportDecode[ws.WorkspaceOutput](t, transportTool(t, f.ctx, cs, "create_workspace", uuid.Nil, "other", ws.CreateInput{Name: "Foreign anchors"}, "")).Workspace.ID
	var foreign uuid.UUID
	if e := f.pool.QueryRow(f.ctx, "INSERT INTO products(workspace_id,name) SELECT id,'Foreign' FROM workspaces WHERE uuid=$1 RETURNING uuid", other).Scan(&foreign); e != nil {
		t.Fatal(e)
	}
	for _, id := range []uuid.UUID{foreign, uuid.New()} {
		prodIn["after"] = forgedAnchor(t, first.Next, id)
		transportTool(t, f.ctx, cs, "list_products", f.w, "", prodIn, operations.Validation)
		values := url.Values{}
		for k, v := range prodIn {
			values.Set(k, fmt.Sprint(v))
		}
		f.http(t, "GET", "/v1/workspaces/"+f.w.String()+"/products?"+values.Encode(), f.bearer, "", nil, 400, operations.Validation)
	}
	// Removing an association retains its historical anchor identity.
	linkIn := map[string]any{"product_id": p.ID.String(), "archived": false, "limit": 1, "after": ""}
	lf := transportDecode[page](t, f.invoke(t, cs, "list_product_projects", "", linkIn))
	all := transportDecode[page](t, f.invoke(t, cs, "list_product_projects", "", map[string]any{"product_id": p.ID.String(), "archived": false, "limit": 100, "after": ""}))
	ids := []string{}
	for _, r := range all.Items {
		if r.ID != lf.Items[0].ID {
			ids = append(ids, r.ID.String())
		}
	}
	if all.HasMore {
		tail := transportDecode[page](t, f.invoke(t, cs, "list_product_projects", "", map[string]any{"product_id": p.ID.String(), "archived": false, "limit": 100, "after": all.Next}))
		for _, r := range tail.Items {
			ids = append(ids, r.ID.String())
		}
	}
	f.invoke(t, cs, "set_product_projects", "remove-link-anchor", map[string]any{"product_id": p.ID.String(), "project_ids": ids, "expected_product_version": 2})
	linkIn["after"] = lf.Next
	linkIn["limit"] = 100
	remaining = 0
	for i := 0; ; i++ {
		if i > 3 {
			t.Fatal("link cursor stalled")
		}
		out := transportDecode[page](t, f.invoke(t, cs, "list_product_projects", "", linkIn))
		if out.ProductVersion != 3 {
			t.Fatal("link snapshot stale")
		}
		remaining += len(out.Items)
		if !out.HasMore {
			break
		}
		linkIn["after"] = out.Next
	}
	if remaining != 129 {
		t.Fatalf("removed anchor remaining=%d", remaining)
	}
	for _, id := range []uuid.UUID{foreign, uuid.New()} {
		linkIn["after"] = forgedAnchor(t, lf.Next, id)
		transportTool(t, f.ctx, cs, "list_product_projects", f.w, "", linkIn, operations.Validation)
	}
}
