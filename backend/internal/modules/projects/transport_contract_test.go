package projects_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/url"

	project "github.com/furkanmeclis/traccia/backend/internal/modules/projects/usecase"
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

type projectTransport struct {
	projectFixture
	mux      *http.ServeMux
	bearer   string
	document contractcoverage.Object
}

var transportCatalog sync.Map

func newProjectTransport(t *testing.T) *projectTransport {
	t.Helper()
	f := newProjectFixture(t)
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
	return &projectTransport{projectFixture: f, mux: mux, bearer: bearer, document: doc}
}
func transportJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
func (f *projectTransport) http(t *testing.T, method, path, token, key string, input any, status int, code operations.Code) []byte {
	t.Helper()
	var body []byte
	if input != nil {
		body = transportJSON(t, input)
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
func (f *projectTransport) client(t *testing.T, serverCtx context.Context) *mcp.ClientSession {
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
func (f *projectTransport) invoke(t *testing.T, cs *mcp.ClientSession, name, key string, input map[string]any) []byte {
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
		for _, pair := range [][2]string{{"projectID", "project_id"}, {"milestoneID", "milestone_id"}, {"resourceID", "resource_id"}} {
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
func object(t *testing.T, raw []byte) map[string]any {
	t.Helper()
	return transportDecode[map[string]any](t, raw)
}
func TestProjectsTransportLifecycleAndContracts(t *testing.T) {
	f := newProjectTransport(t)
	cs := f.client(t, f.ctx)
	tools, e := cs.ListTools(f.ctx, nil)
	if e != nil {
		t.Fatal(e)
	}
	byName := map[string]*mcp.Tool{}
	for _, v := range tools.Tools {
		byName[v.Name] = v
	}
	count := 0
	for _, c := range f.registry.Catalog() {
		if strings.HasPrefix(c.Metadata.ID, "projects.") || strings.HasPrefix(c.Metadata.ID, "project_") || c.Metadata.ID == "workspace_members.list" {
			count++
			v := byName[c.Metadata.MCPName]
			if v == nil || v.InputSchema == nil || v.OutputSchema == nil || v.Meta["traccia/error_schema"] == nil || v.Meta["traccia/operation_id"] != c.Metadata.ID {
				t.Fatal("incomplete project discovery")
			}
		}
	}
	if count != 21 {
		t.Fatalf("project operation count=%d", count)
	}
	team := f.createTeam(t, "TR")
	text := "İstanbul 🧭 e\u0301 漢字\n<plate>\"&</plate>"
	input := map[string]any{"name": "Proje 🧭", "summary": text, "team_ids": []string{team.ID.String()}, "lead_user_id": f.actor.String(), "start_date": "2026-01-01", "target_date": "2026-12-31"}
	raw := f.invoke(t, cs, "create_project", "transport-create", input)
	p := transportDecode[project.ProjectOutput](t, raw).Project
	pid := p.ID.String()
	if p.Summary != text || p.LeadUserID == nil || *p.LeadUserID != f.actor.String() {
		t.Fatal("project content changed")
	}
	var effects int
	if e = f.pool.QueryRow(f.ctx, "SELECT count(*) FROM workspace_history WHERE project_uuid=$1", p.ID).Scan(&effects); e != nil || effects != 1 {
		t.Fatalf("history effects=%d %v", effects, e)
	}
	input["name"] = "Changed"
	transportTool(t, f.ctx, cs, "create_project", f.w, "transport-create", input, operations.IdempotencyConflict)
	f.invoke(t, cs, "get_project", "", map[string]any{"project_id": pid})
	f.invoke(t, cs, "list_projects", "", map[string]any{"limit": 100, "after": "", "archived": false})
	f.invoke(t, cs, "list_workspace_members", "", map[string]any{"limit": 100, "after": ""})
	f.invoke(t, cs, "list_project_teams", "", map[string]any{"project_id": pid, "limit": 100, "after": "", "archived": false})
	f.invoke(t, cs, "set_project_teams", "teams", map[string]any{"project_id": pid, "team_ids": []string{team.ID.String()}, "expected_project_version": 1})
	update := map[string]any{"project_id": pid, "name": "Yeni 🧭", "summary": text, "lead_user_id": nil, "start_date": nil, "target_date": nil, "expected_version": 2}
	cleared := transportDecode[project.ProjectOutput](t, f.invoke(t, cs, "update_project", "project-update", update)).Project
	if cleared.LeadUserID != nil || cleared.StartDate != nil || cleared.TargetDate != nil {
		t.Fatal("nullable project fields did not clear")
	}
	transportTool(t, f.ctx, cs, "update_project", f.w, "stale-project", update, operations.VersionConflict)
	pv := int64(3)
	for _, kind := range []string{"milestone", "resource"} {
		plural := kind + "s"
		name := "create_project_" + kind
		in := map[string]any{"project_id": pid, "expected_project_version": pv}
		if kind == "milestone" {
			in["name"] = "Hedef 🧭"
			in["description"] = text
			in["target_date"] = "2026-12-31"
		} else {
			in["kind"] = "document"
			in["title"] = "Kaynak 🧭"
			in["url"] = "https://example.test/çalışma?q=%E2%9C%93#bölüm"
			in["summary"] = text
			in["provider"] = "GitHub 🧭"
			in["reference"] = "refs/heads/çalışma"
		}
		result := object(t, f.invoke(t, cs, name, kind+"-create", in))
		pv++
		row := result[kind].(map[string]any)
		id := row["id"].(string)
		if kind == "milestone" && row["description"] != text || kind == "resource" && row["summary"] != text {
			t.Fatal("child content changed")
		}
		ids := map[string]any{"project_id": pid, kind + "_id": id}
		f.invoke(t, cs, "get_project_"+kind, "", ids)
		f.invoke(t, cs, "list_project_"+plural, "", map[string]any{"project_id": pid, "limit": 100, "after": "", "archived": false})
		in[kind+"_id"] = id
		in["expected_version"] = 1
		in["expected_project_version"] = pv
		if kind == "milestone" {
			in["target_date"] = nil
		} else {
			in["kind"] = "repository"
			in["url"] = "https://github.com/example/project/tree/main"
		}
		f.invoke(t, cs, "update_project_"+kind, kind+"-update", in)
		pv++
		transportTool(t, f.ctx, cs, "update_project_"+kind, f.w, kind+"-stale", in, operations.VersionConflict)
		in["expected_project_version"] = pv
		transportTool(t, f.ctx, cs, "update_project_"+kind, f.w, kind+"-stale-child", in, operations.VersionConflict)
		arch := map[string]any{"project_id": pid, kind + "_id": id, "expected_version": 2, "expected_project_version": pv}
		f.invoke(t, cs, "archive_project_"+kind, kind+"-archive", arch)
		pv++
		list := object(t, f.invoke(t, cs, "list_project_"+plural, "", map[string]any{"project_id": pid, "limit": 100, "after": "", "archived": true}))
		if !list["capabilities"].(map[string]any)["restore"].(bool) {
			t.Fatal("archived child restore missing")
		}
		arch["expected_version"] = 3
		arch["expected_project_version"] = pv
		f.invoke(t, cs, "restore_project_"+kind, kind+"-restore", arch)
		pv++
	}
	archived := transportDecode[project.ProjectOutput](t, f.invoke(t, cs, "archive_project", "project-archive", map[string]any{"project_id": pid, "expected_version": pv}))
	pv++
	if !archived.Capabilities.Restore || archived.Capabilities.Manage {
		t.Fatal("archived project capabilities")
	}
	f.invoke(t, cs, "restore_project", "project-restore", map[string]any{"project_id": pid, "expected_version": pv})
	base := "/v1/workspaces/" + f.w.String() + "/projects"
	for _, body := range []json.RawMessage{json.RawMessage(`{"name":"A","name":"B","expected_version":1}`), json.RawMessage(`{"project_id":"` + pid + `","name":"A","expected_version":1}`), json.RawMessage(`{"actor_id":"` + f.actor.String() + `","name":"A","expected_version":1}`)} {
		f.http(t, "PATCH", base+"/"+pid, f.bearer, uuid.NewString(), body, 400, operations.Validation)
	}
	for _, q := range []string{"limit=1&limit=2", "unknown=1", "project_id=" + pid, "archived=yes", "limit=101"} {
		f.http(t, "GET", base+"?"+q, f.bearer, "", nil, 400, operations.Validation)
	}
	f.http(t, "GET", base+"/not-a-uuid", f.bearer, "", nil, 400, operations.Validation)
	for _, u := range []string{"javascript:alert(1)", "data:text/html,x", "file:///tmp/a", "https://user:pass@example.test/a"} {
		transportTool(t, f.ctx, cs, "create_project_resource", f.w, uuid.NewString(), map[string]any{"project_id": pid, "kind": "link", "title": "Bad", "url": u, "summary": "", "provider": "", "reference": "", "expected_project_version": pv + 1}, operations.Validation)
	}
	tooMany := make([]string, project.MaxContributingTeams+1)
	for i := range tooMany {
		tooMany[i] = uuid.NewString()
	}
	f.http(t, "PUT", base+"/"+pid+"/teams", f.bearer, "too-many-contributors", map[string]any{"team_ids": tooMany, "expected_project_version": pv + 1}, 400, operations.Validation)
	transportTool(t, f.ctx, cs, "set_project_teams", f.w, "too-many-sdk", map[string]any{"project_id": pid, "team_ids": tooMany, "expected_project_version": pv + 1}, operations.Validation)
	for _, dates := range [][2]string{{"2026-02-30", "2026-12-31"}, {"2026-12-31", "2026-01-01"}} {
		transportTool(t, f.ctx, cs, "create_project", f.w, uuid.NewString(), map[string]any{"name": "Bad dates", "summary": "", "team_ids": []string{team.ID.String()}, "lead_user_id": nil, "start_date": dates[0], "target_date": dates[1]}, operations.Validation)
	}
	result, err := cs.CallTool(f.ctx, &mcp.CallToolParams{Name: "get_project", Arguments: json.RawMessage(`{"workspace_id":"` + f.w.String() + `","input":{"project_id":"` + pid + `"},"actor_id":"` + f.actor.String() + `"}`)})
	if err == nil && (result == nil || !result.IsError) {
		t.Fatal("SDK wrapper accepted unknown field")
	}
}

func TestProjectsTransportCurrentCredentials(t *testing.T) {
	f := newProjectTransport(t)
	human := f.client(t, f.ctx)
	team := f.createTeam(t, "AU")
	p := f.createProject(t, team.ID)
	mint := func(audience string, scopes []string) ws.CreateCredentialOutput {
		return transportDecode[ws.CreateCredentialOutput](t, transportTool(t, f.ctx, human, "create_workspace_credential", f.w, uuid.NewString(), ws.CreateCredentialInput{Name: "Transport", Audience: audience, Scopes: scopes, ExpiresAt: time.Now().Add(time.Hour).UTC().Truncate(time.Second)}, ""))
	}
	api := mint("api", []string{"workspace.read", "workspace.write"})
	pat := mint("mcp", []string{"workspace.read", "workspace.write"})
	read := mint("mcp", []string{"workspace.read"})
	client := f.client(t, ws.WithToolCredential(context.Background(), pat.Token))
	readClient := f.client(t, ws.WithToolCredential(context.Background(), read.Token))
	apiClient := f.client(t, ws.WithToolCredential(context.Background(), api.Token))
	get := project.ProjectInput{ProjectID: p.ID}
	path := "/v1/workspaces/" + f.w.String() + "/projects/" + p.ID.String()
	transportTool(t, f.ctx, apiClient, "get_project", f.w, "", get, operations.Forbidden)
	f.http(t, "GET", path, pat.Token, "", nil, 403, operations.Forbidden)
	f.http(t, "GET", path, api.Token, "", nil, 200, "")
	limited := transportDecode[project.ProjectOutput](t, transportTool(t, f.ctx, readClient, "get_project", f.w, "", get, ""))
	if limited.Capabilities.Manage || limited.Capabilities.Restore {
		t.Fatal("read PAT obtained write capability")
	}
	edit := project.UpdateProjectInput{ProjectID: p.ID, Name: "Authorized", ExpectedVersion: 1}
	transportTool(t, f.ctx, readClient, "update_project", f.w, "read-write", edit, operations.Forbidden)
	transportTool(t, f.ctx, client, "update_project", f.w, "auth-replay", edit, "")
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, e := f.pool.Exec(f.ctx, sql, args...); e != nil {
			t.Fatal(e)
		}
	}
	replay := func(code operations.Code) {
		transportTool(t, f.ctx, client, "update_project", f.w, "auth-replay", edit, code)
	}
	exec("UPDATE workspace_tool_credentials SET created_at=clock_timestamp()-interval '2 hours',expires_at=clock_timestamp()-interval '1 second' WHERE uuid=$1", pat.Credential.ID)
	replay(operations.Unauthenticated)
	exec("UPDATE workspace_tool_credentials SET expires_at=clock_timestamp()+interval '1 hour' WHERE uuid=$1", pat.Credential.ID)
	replay("")
	exec("UPDATE workspace_members SET active=false WHERE workspace_id=(SELECT id FROM workspaces WHERE uuid=$1)", f.w)
	replay(operations.Unauthenticated)
	exec("UPDATE workspace_members SET active=true WHERE workspace_id=(SELECT id FROM workspaces WHERE uuid=$1)", f.w)
	replay("")
	transportTool(t, f.ctx, human, "revoke_workspace_credential", f.w, "revoke", ws.RevokeInput{CredentialID: pat.Credential.ID}, "")
	replay(operations.Unauthenticated)
	other := transportDecode[ws.WorkspaceOutput](t, transportTool(t, f.ctx, human, "create_workspace", uuid.Nil, "other", ws.CreateInput{Name: "Other"}, "")).Workspace.ID
	var foreign uuid.UUID
	if e := f.pool.QueryRow(f.ctx, "INSERT INTO projects(workspace_id,name) SELECT id,'Foreign cursor' FROM workspaces WHERE uuid=$1 RETURNING uuid", other).Scan(&foreign); e != nil {
		t.Fatal(e)
	}
	foreignPage := transportDecode[project.ProjectList](t, f.invoke(t, human, "list_projects", "", map[string]any{"limit": 100, "after": foreign.String(), "archived": false}))
	if len(foreignPage.Items) != 0 || foreignPage.HasMore {
		t.Fatal("foreign workspace cursor crossed scope")
	}
	transportTool(t, f.ctx, human, "get_project", other, "", get, operations.NotFound)
	f.http(t, "GET", "/v1/workspaces/"+other.String()+"/projects/"+p.ID.String(), api.Token, "", nil, 403, operations.Forbidden)
	body := map[string]any{"name": "API", "summary": "", "lead_user_id": nil, "start_date": nil, "target_date": nil, "expected_version": 2}
	f.http(t, "PATCH", path, api.Token, "api-replay", body, 200, "")
	exec("UPDATE users SET status='disabled' WHERE uuid=$1", f.actor)
	f.http(t, "PATCH", path, api.Token, "api-replay", body, 401, operations.Unauthenticated)
}

func TestProjectsTransportCompletePagination(t *testing.T) {
	f := newProjectTransport(t)
	cs := f.client(t, f.ctx)
	team := f.createTeam(t, "PG")
	p := f.createProject(t, team.ID)
	queries := []string{
		"INSERT INTO projects(workspace_id,name) SELECT id,'Project '||n FROM workspaces CROSS JOIN generate_series(1,125)n WHERE uuid=$1",
		"INSERT INTO teams(workspace_id,name,code) SELECT id,'Page '||n,'P'||n FROM workspaces CROSS JOIN generate_series(1,125)n WHERE uuid=$1",
		"INSERT INTO users(email,password_hash,name,surname,status) SELECT 'page-'||n||'@example.test','unused','','','active' FROM generate_series(1,125)n",
		"INSERT INTO workspace_members(workspace_id,user_id,role) SELECT w.id,u.id,'member' FROM workspaces w,users u WHERE w.uuid=$1 AND u.email LIKE 'page-%'",
		"INSERT INTO project_milestones(workspace_id,project_id,name) SELECT workspace_id,id,'Milestone '||n FROM projects CROSS JOIN generate_series(1,125)n WHERE uuid=$1",
		"INSERT INTO project_resources(workspace_id,project_id,kind,title,url) SELECT workspace_id,id,'link','Resource '||n,'https://example.test/source/'||n FROM projects CROSS JOIN generate_series(1,125)n WHERE uuid=$1",
		"INSERT INTO project_teams(workspace_id,project_id,team_id) SELECT p.workspace_id,p.id,t.id FROM projects p JOIN teams t ON t.workspace_id=p.workspace_id WHERE p.uuid=$1 AND t.uuid<>$2",
	}
	for i, q := range queries {
		var args []any
		if i == 6 {
			args = []any{p.ID, team.ID}
		} else if i >= 4 {
			args = []any{p.ID}
		} else if i != 2 {
			args = []any{f.w}
		}
		if _, e := f.pool.Exec(f.ctx, q, args...); e != nil {
			t.Fatal(e)
		}
	}
	cases := []struct {
		tool   string
		count  int
		parent bool
	}{{"list_projects", 126, false}, {"list_workspace_members", 126, false}, {"list_project_teams", 126, true}, {"list_project_milestones", 125, true}, {"list_project_resources", 125, true}}
	for _, c := range cases {
		t.Run(c.tool, func(t *testing.T) {
			seen := map[string]bool{}
			after := ""
			for pages := 0; ; pages++ {
				if pages > 3 {
					t.Fatal("cursor stalled")
				}
				in := map[string]any{"limit": 100, "after": after}
				if c.parent {
					in["project_id"] = p.ID.String()
					in["archived"] = false
				} else if c.tool == "list_projects" {
					in["archived"] = false
				}
				raw := f.invoke(t, cs, c.tool, "", in)
				var page struct {
					Items []struct {
						ID string `json:"id"`
					}
					HasMore bool   `json:"has_more"`
					Next    string `json:"next_cursor"`
				}
				if e := json.Unmarshal(raw, &page); e != nil {
					t.Fatal(e)
				}
				for _, r := range page.Items {
					if seen[r.ID] {
						t.Fatal("duplicate page row")
					}
					seen[r.ID] = true
				}
				if !page.HasMore {
					if page.Next != "" {
						t.Fatal("terminal cursor")
					}
					break
				}
				if len(page.Items) != 100 || page.Next == "" || page.Next == after {
					t.Fatal("incomplete page")
				}
				after = page.Next
			}
			if len(seen) != c.count {
				t.Fatalf("got %d rows want %d", len(seen), c.count)
			}
		})
	}
	rows, e := f.pool.Query(f.ctx, "SELECT uuid FROM teams ORDER BY uuid")
	if e != nil {
		t.Fatal(e)
	}
	var ids []string
	for rows.Next() {
		var id string
		if e = rows.Scan(&id); e != nil {
			t.Fatal(e)
		}
		ids = append(ids, id)
	}
	rows.Close()
	if rows.Err() != nil {
		t.Fatal(rows.Err())
	}
	f.invoke(t, cs, "set_project_teams", "large-contributors", map[string]any{"project_id": p.ID.String(), "team_ids": ids, "expected_project_version": 1})
}

func TestProjectsTransportOwnershipHistoryCapabilitiesAndIntegers(t *testing.T) {
	f := newProjectTransport(t)
	cs := f.client(t, f.ctx)
	a := f.createTeam(t, "AA")
	b := f.createTeam(t, "BB")
	p := f.createProject(t, a.ID)
	q := f.createProject(t, b.ID)
	exec := func(query string, args ...any) {
		t.Helper()
		if _, e := f.pool.Exec(f.ctx, query, args...); e != nil {
			t.Fatal(e)
		}
	}
	candidate := uuid.New()
	exec("INSERT INTO users(uuid,email,password_hash,name,surname,status) VALUES($1,$2,'unused','Candidate','Member','active')", candidate, candidate.String()+"@example.test")
	exec("INSERT INTO workspace_members(workspace_id,user_id,role) SELECT w.id,u.id,'member' FROM workspaces w,users u WHERE w.uuid=$1 AND u.uuid=$2", f.w, candidate)
	c := candidate.String()
	f.invoke(t, cs, "update_project", "assign-lead", map[string]any{"project_id": p.ID.String(), "name": "Lead", "summary": "", "lead_user_id": c, "start_date": nil, "target_date": nil, "expected_version": 1})
	exec("UPDATE workspace_members SET active=false WHERE user_id=(SELECT id FROM users WHERE uuid=$1)", candidate)
	retained := transportDecode[project.ProjectOutput](t, f.invoke(t, cs, "update_project", "retain-lead", map[string]any{"project_id": p.ID.String(), "name": "Rename", "summary": "", "lead_user_id": strings.ToUpper(c), "start_date": nil, "target_date": nil, "expected_version": 2}))
	if retained.Project.LeadUserID == nil || *retained.Project.LeadUserID != c {
		t.Fatal("historical lead lost")
	}
	transportTool(t, f.ctx, cs, "create_project", f.w, "inactive-lead", project.CreateProjectInput{Name: "Denied", LeadUserID: &c, TeamIDs: []string{a.ID.String()}}, operations.NotFound)
	// Archived contributors remain visible and may be retained, while new
	// projects cannot acquire them.
	transportTool(t, f.ctx, cs, "archive_team", f.w, "archive-contributor", map[string]any{"team_id": a.ID.String(), "expected_version": 1}, "")
	f.invoke(t, cs, "set_project_teams", "retain-contributor", map[string]any{"project_id": p.ID.String(), "team_ids": []string{a.ID.String(), b.ID.String()}, "expected_project_version": 3})
	transportTool(t, f.ctx, cs, "create_project", f.w, "archived-contributor", project.CreateProjectInput{Name: "Denied", TeamIDs: []string{a.ID.String()}}, operations.Conflict)
	historical := transportDecode[project.ProjectTeams](t, f.invoke(t, cs, "list_project_teams", "", map[string]any{"project_id": p.ID.String(), "limit": 100, "after": "", "archived": false}))
	foundHistorical := false
	for _, row := range historical.Items {
		if row.ID == a.ID && row.Archived {
			foundHistorical = true
		}
	}
	if !foundHistorical {
		t.Fatal("archived contributor disappeared")
	}
	m := transportDecode[project.MilestoneOutput](t, f.invoke(t, cs, "create_project_milestone", "owned-m", map[string]any{"project_id": p.ID.String(), "name": "Owned", "description": "", "target_date": nil, "expected_project_version": 4}))
	r := transportDecode[project.ResourceOutput](t, f.invoke(t, cs, "create_project_resource", "owned-r", map[string]any{"project_id": p.ID.String(), "kind": "link", "title": "Source", "url": "https://example.test/source", "summary": "", "provider": "", "reference": "", "expected_project_version": 5}))
	for _, item := range []struct{ name, path, key, id string }{{"milestone", "milestones", "milestone_id", m.Milestone.ID.String()}, {"resource", "resources", "resource_id", r.Resource.ID.String()}} {
		in := map[string]any{"project_id": q.ID.String(), item.key: item.id}
		transportTool(t, f.ctx, cs, "get_project_"+item.name, f.w, "", in, operations.NotFound)
		f.http(t, "GET", "/v1/workspaces/"+f.w.String()+"/projects/"+q.ID.String()+"/"+item.path+"/"+item.id, f.bearer, "", nil, 404, operations.NotFound)
		page := object(t, f.invoke(t, cs, "list_project_"+item.path, "", map[string]any{"project_id": q.ID.String(), "limit": 100, "after": item.id, "archived": false}))
		if len(page["items"].([]any)) != 0 || page["has_more"].(bool) {
			t.Fatal("foreign project cursor crossed scope")
		}
	}
	f.invoke(t, cs, "archive_project_milestone", "arch-m", map[string]any{"project_id": p.ID.String(), "milestone_id": m.Milestone.ID.String(), "expected_version": 1, "expected_project_version": 6})
	filter := object(t, f.invoke(t, cs, "list_project_milestones", "", map[string]any{"project_id": p.ID.String(), "limit": 100, "after": m.Milestone.ID.String(), "archived": false}))
	if len(filter["items"].([]any)) != 0 {
		t.Fatal("different filter cursor crossed scope")
	}
	f.invoke(t, cs, "archive_project", "arch-parent", map[string]any{"project_id": p.ID.String(), "expected_version": 7})
	children := transportDecode[project.MilestoneList](t, f.invoke(t, cs, "list_project_milestones", "", map[string]any{"project_id": p.ID.String(), "limit": 100, "after": "", "archived": true}))
	if children.Capabilities.Restore || children.Capabilities.Manage {
		t.Fatal("archived parent permits child restoration")
	}
	exec("UPDATE workspace_members SET active=true WHERE user_id=(SELECT id FROM users WHERE uuid=$1)", candidate)
	tokens, e := jwt.NewManager("team-contract-signing-secret", time.Hour, time.Hour)
	if e != nil {
		t.Fatal(e)
	}
	memberToken, _, e := tokens.IssueAccess(jwt.AccessInput{UserID: candidate})
	if e != nil {
		t.Fatal(e)
	}
	member := transportDecode[project.ProjectOutput](t, f.http(t, "GET", "/v1/workspaces/"+f.w.String()+"/projects/"+p.ID.String(), memberToken, "", nil, 200, ""))
	if member.Capabilities.Restore || member.Capabilities.Manage {
		t.Fatal("member can restore archived project")
	}
	// Use the SDK's exact JSON text, never remarshal StructuredContent:any: the
	// latter's float64 decoding cannot demonstrate BIGINT fidelity.
	const large int64 = 9007199254740993
	exec("UPDATE projects SET version=$2 WHERE uuid=$1", q.ID, large)
	raw := f.invoke(t, cs, "get_project", "", map[string]any{"project_id": q.ID.String()})
	got := transportDecode[project.ProjectOutput](t, raw)
	if got.Project.Version != large || !bytes.Contains(raw, []byte(`"version":9007199254740993`)) {
		t.Fatal("BIGINT rounded on transport")
	}
}

func TestProjectsTransportCursorSurvivesAnchorArchive(t *testing.T) {
	f := newProjectTransport(t)
	cs := f.client(t, f.ctx)
	if _, e := f.pool.Exec(f.ctx, "INSERT INTO projects(workspace_id,name) SELECT id,'Cursor '||n FROM workspaces CROSS JOIN generate_series(1,3)n WHERE uuid=$1", f.w); e != nil {
		t.Fatal(e)
	}
	first := transportDecode[project.ProjectList](t, f.invoke(t, cs, "list_projects", "", map[string]any{"limit": 1, "after": "", "archived": false}))
	if len(first.Items) != 1 || !first.HasMore {
		t.Fatal("invalid cursor fixture")
	}
	f.invoke(t, cs, "archive_project", "archive-anchor", map[string]any{"project_id": first.Items[0].ID.String(), "expected_version": 1})
	second := transportDecode[project.ProjectList](t, f.invoke(t, cs, "list_projects", "", map[string]any{"limit": 100, "after": first.NextCursor, "archived": false}))
	if len(second.Items) != 2 {
		t.Fatalf("archiving cursor anchor truncated live project traversal: remaining=%d want=2", len(second.Items))
	}
}

func TestProjectsTransportChildCursorSurvivesAnchorArchive(t *testing.T) {
	for _, kind := range []string{"milestone", "resource"} {
		t.Run(kind, func(t *testing.T) {
			f := newProjectTransport(t)
			cs := f.client(t, f.ctx)
			team := f.createTeam(t, "CA")
			p := f.createProject(t, team.ID)
			query := "INSERT INTO project_milestones(workspace_id,project_id,name) SELECT workspace_id,id,'Cursor '||n FROM projects CROSS JOIN generate_series(1,3)n WHERE uuid=$1"
			if kind == "resource" {
				query = "INSERT INTO project_resources(workspace_id,project_id,kind,title,url) SELECT workspace_id,id,'link','Cursor '||n,'https://example.test/'||n FROM projects CROSS JOIN generate_series(1,3)n WHERE uuid=$1"
			}
			if _, e := f.pool.Exec(f.ctx, query, p.ID); e != nil {
				t.Fatal(e)
			}
			first := object(t, f.invoke(t, cs, "list_project_"+kind+"s", "", map[string]any{"project_id": p.ID.String(), "limit": 1, "after": "", "archived": false}))
			items := first["items"].([]any)
			if len(items) != 1 || !first["has_more"].(bool) {
				t.Fatal("invalid child cursor fixture")
			}
			id := items[0].(map[string]any)["id"].(string)
			f.invoke(t, cs, "archive_project_"+kind, "archive-child-anchor", map[string]any{"project_id": p.ID.String(), kind + "_id": id, "expected_version": 1, "expected_project_version": 1})
			second := object(t, f.invoke(t, cs, "list_project_"+kind+"s", "", map[string]any{"project_id": p.ID.String(), "limit": 100, "after": first["next_cursor"], "archived": false}))
			if second["project_version"].(float64) != 2 {
				t.Fatal("child list omitted changed aggregate version")
			}
			if len(second["items"].([]any)) != 2 {
				t.Fatalf("archiving %s cursor anchor truncated traversal: remaining=%d want=2", kind, len(second["items"].([]any)))
			}
		})
	}
}
