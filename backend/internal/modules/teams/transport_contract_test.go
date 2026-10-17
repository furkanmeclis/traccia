package teams_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	team "github.com/furkanmeclis/traccia/backend/internal/modules/teams/usecase"
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
	"net/url"
	"reflect"
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

type teamTransport struct {
	teamFixture
	mux      *http.ServeMux
	bearer   string
	document contractcoverage.Object
}

var transportCatalog sync.Map

func newTeamTransport(t *testing.T) *teamTransport {
	t.Helper()
	f := newTeamFixture(t)
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
	return &teamTransport{teamFixture: f, mux: mux, bearer: bearer, document: doc}
}
func transportJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
func (f *teamTransport) http(t *testing.T, method, path, token, key string, input any, status int, code operations.Code) []byte {
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
func (f *teamTransport) client(t *testing.T, serverCtx context.Context) *mcp.ClientSession {
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

func TestTeamsTransportLifecycleAndContracts(t *testing.T) {
	f := newTeamTransport(t)
	cs := f.client(t, f.ctx)
	base := "/v1/workspaces/" + f.w.String()
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
		if strings.HasPrefix(c.Metadata.ID, "teams.") || strings.HasPrefix(c.Metadata.ID, "labels.") || strings.HasPrefix(c.Metadata.ID, "workflow_statuses.") {
			count++
			v := byName[c.Metadata.MCPName]
			if v == nil || v.OutputSchema == nil || v.InputSchema == nil || v.Meta["traccia/error_schema"] == nil || v.Meta["traccia/operation_id"] != c.Metadata.ID {
				t.Fatal("incomplete discovery")
			}
		}
	}
	if count != 15 {
		t.Fatalf("operations=%d", count)
	}
	in := team.CreateTeamInput{Name: "Tasarım 🐾", Code: "UX"}
	raw := f.http(t, "POST", base+"/teams", f.bearer, "cross-team", in, 201, "")
	if !bytes.Equal(raw, transportTool(t, f.ctx, cs, "create_team", f.w, "cross-team", in, "")) {
		t.Fatal("cross-channel receipt differs")
	}
	a := transportDecode[team.TeamOutput](t, raw).Team
	path := base + "/teams/" + a.ID.String()
	for _, table := range []string{"workspace_history", "outbox_events", "operation_receipts"} {
		var n int
		if e = f.pool.QueryRow(f.ctx, "SELECT count(*) FROM "+table).Scan(&n); e != nil || n != 2 {
			t.Fatalf("%s=%d %v", table, n, e)
		}
	}
	transportTool(t, f.ctx, cs, "create_team", f.w, "cross-team", team.CreateTeamInput{Name: "Changed", Code: "UX"}, operations.IdempotencyConflict)
	transportTool(t, f.ctx, cs, "get_team", f.w, "", team.TeamInput{TeamID: a.ID}, "")
	f.http(t, "GET", path, f.bearer, "", nil, 200, "")
	edit := team.UpdateTeamInput{TeamID: a.ID, Name: "Yeni 🚀", ExpectedVersion: 1}
	raw = transportTool(t, f.ctx, cs, "update_team", f.w, "update-team", edit, "")
	if !bytes.Equal(raw, f.http(t, "PATCH", path, f.bearer, "update-team", map[string]any{"name": edit.Name, "expected_version": 1}, 200, "")) {
		t.Fatal("update retry differs")
	}
	transportTool(t, f.ctx, cs, "update_team", f.w, "stale-mcp", edit, operations.VersionConflict)
	f.http(t, "PATCH", path, f.bearer, "stale-http", map[string]any{"name": "Stale", "expected_version": 1}, 409, operations.VersionConflict)
	defaults := transportDecode[team.StatusList](t, transportTool(t, f.ctx, cs, "list_workflow_statuses", f.w, "", team.StatusListInput{TeamID: a.ID, Limit: 100}, ""))
	cats := map[string]bool{}
	for _, s := range defaults.Items {
		cats[s.Category] = true
	}
	if len(defaults.Items) != 5 || len(cats) != 5 || defaults.WorkflowVersion != 1 {
		t.Fatal("default workflow missing")
	}
	sp := path + "/workflow-statuses"
	custom := transportDecode[team.StatusOutput](t, f.http(t, "POST", sp, f.bearer, "custom-status", map[string]any{"name": "Review", "category": "started", "color": "#123456", "expected_workflow_version": 1}, 201, ""))
	transportTool(t, f.ctx, cs, "update_workflow_status", f.w, "update-status", team.UpdateStatusInput{TeamID: a.ID, StatusID: custom.Status.ID, Name: "İnceleme", Category: "started", Color: "#abcdef", ExpectedVersion: 1, ExpectedWorkflowVersion: 2}, "")
	ids := []string{custom.Status.ID.String()}
	for _, s := range defaults.Items {
		ids = append(ids, s.ID.String())
	}
	transportTool(t, f.ctx, cs, "reorder_workflow_statuses", f.w, "reorder-status", team.ReorderInput{TeamID: a.ID, StatusIDs: ids, ExpectedWorkflowVersion: 3}, "")
	transportTool(t, f.ctx, cs, "archive_workflow_status", f.w, "archive-status", team.ArchiveStatusInput{TeamID: a.ID, StatusID: custom.Status.ID, ExpectedVersion: 3, ExpectedWorkflowVersion: 4}, "")
	for i, tid := range []string{"", a.ID.String()} {
		in := team.CreateLabelInput{TeamID: tid, Name: "Etiket", Color: "#123456"}
		key := fmt.Sprintf("label-%d", i)
		raw := f.http(t, "POST", base+"/labels", f.bearer, key, in, 201, "")
		if !bytes.Equal(raw, transportTool(t, f.ctx, cs, "create_label", f.w, key, in, "")) {
			t.Fatal("label receipt differs")
		}
		label := transportDecode[team.LabelOutput](t, raw).Label
		lp := base + "/labels/" + label.ID.String()
		transportTool(t, f.ctx, cs, "get_label", f.w, "", team.LabelInput{LabelID: label.ID}, "")
		transportTool(t, f.ctx, cs, "list_labels", f.w, "", team.LabelListInput{TeamID: tid, Limit: 100}, "")
		f.http(t, "PATCH", lp, f.bearer, key+"-update", map[string]any{"name": "Updated", "color": "#ffffff", "expected_version": 1}, 200, "")
		transportTool(t, f.ctx, cs, "archive_label", f.w, key+"-archive", team.ArchiveLabelInput{LabelID: label.ID, ExpectedVersion: 2}, "")
	}
	transportTool(t, f.ctx, cs, "list_teams", f.w, "", team.TeamListInput{Limit: 100}, "")
	transportTool(t, f.ctx, cs, "archive_team", f.w, "archive-team", team.ArchiveTeamInput{TeamID: a.ID, ExpectedVersion: 2}, "")
	f.http(t, "GET", base+"/teams?archived=true", f.bearer, "", nil, 200, "")
	for _, body := range []json.RawMessage{json.RawMessage(`{"name":"One","name":"Two","expected_version":2}`), json.RawMessage(`{"name":"One","expected_version":2,"code":"NEW"}`), json.RawMessage(`{"name":"One","expected_version":2,"team_id":"` + a.ID.String() + `"}`)} {
		f.http(t, "PATCH", path, f.bearer, uuid.NewString(), body, 400, operations.Validation)
	}
	for _, q := range []string{"limit=1&limit=2", "unknown=1", "team_id=" + a.ID.String(), "archived=yes", "limit=101"} {
		f.http(t, "GET", base+"/teams?"+q, f.bearer, "", nil, 400, operations.Validation)
	}
	f.http(t, "GET", base+"/teams/not-a-uuid", f.bearer, "", nil, 400, operations.Validation)
	transportTool(t, f.ctx, cs, "create_team", f.w, "unknown", map[string]any{"name": "One", "code": "AB", "actor_id": f.actor.String()}, operations.Validation)
	transportTool(t, f.ctx, cs, "create_team", f.w, "duplicate-json", json.RawMessage(`{"name":"One","name":"Two","code":"AB"}`), operations.Validation)
	transportTool(t, f.ctx, cs, "list_teams", f.w, "", team.TeamListInput{Limit: 101}, operations.Validation)
}

func TestTeamsTransportLargeWorkflowAndCompletePagination(t *testing.T) {
	f := newTeamTransport(t)
	cs := f.client(t, f.ctx)
	a := f.createTeam(t, "PG")
	base := "/v1/workspaces/" + f.w.String()
	sp := base + "/teams/" + a.ID.String() + "/workflow-statuses"
	for _, sql := range []string{"INSERT INTO teams(workspace_id,name,code) SELECT id,'Page '||n,'P'||n FROM workspaces CROSS JOIN generate_series(1,125) n WHERE uuid=$1", "INSERT INTO labels(workspace_id,name,color) SELECT id,'Label '||n,'#123456' FROM workspaces CROSS JOIN generate_series(1,125) n WHERE uuid=$1"} {
		if _, e := f.pool.Exec(f.ctx, sql, f.w); e != nil {
			t.Fatal(e)
		}
	}
	if _, e := f.pool.Exec(f.ctx, "INSERT INTO workflow_statuses(workspace_id,team_id,name,category,color,position) SELECT workspace_id,id,'Status '||n,'started','#123456',n FROM teams CROSS JOIN generate_series(5,499) n WHERE uuid=$1", a.ID); e != nil {
		t.Fatal(e)
	}
	cases := []struct {
		tool, path, query string
		count             int
		input             func(string) any
	}{
		{"list_teams", base + "/teams", "SELECT uuid FROM teams ORDER BY uuid", 126, func(s string) any { return team.TeamListInput{Limit: 100, After: s} }},
		{"list_labels", base + "/labels", "SELECT uuid FROM labels ORDER BY uuid", 125, func(s string) any { return team.LabelListInput{Limit: 100, After: s} }},
		{"list_workflow_statuses", sp, "SELECT uuid FROM workflow_statuses ORDER BY position", 500, func(s string) any { return team.StatusListInput{TeamID: a.ID, Limit: 100, After: s} }}}
	var statusIDs []string
	for _, c := range cases {
		t.Run(c.tool, func(t *testing.T) {
			var expected []string
			rows, e := f.pool.Query(f.ctx, c.query)
			if e != nil {
				t.Fatal(e)
			}
			for rows.Next() {
				var id uuid.UUID
				if e = rows.Scan(&id); e != nil {
					t.Fatal(e)
				}
				expected = append(expected, id.String())
			}
			rows.Close()
			if rows.Err() != nil || len(expected) != c.count {
				t.Fatal("invalid fixture")
			}
			var actual []string
			after := ""
			for pages := 0; ; pages++ {
				if pages > 6 {
					t.Fatal("cursor did not terminate")
				}
				raw := transportTool(t, f.ctx, cs, c.tool, f.w, "", c.input(after), "")
				if !bytes.Equal(raw, f.http(t, "GET", c.path+"?limit=100&after="+url.QueryEscape(after), f.bearer, "", nil, 200, "")) {
					t.Fatal("page differs")
				}
				var page struct {
					Items []struct {
						ID uuid.UUID `json:"id"`
					} `json:"items"`
					More bool   `json:"has_more"`
					Next string `json:"next_cursor"`
				}
				if e = json.Unmarshal(raw, &page); e != nil {
					t.Fatal(e)
				}
				for _, v := range page.Items {
					actual = append(actual, v.ID.String())
				}
				if !page.More {
					if page.Next != "" {
						t.Fatal("terminal cursor")
					}
					break
				}
				if len(page.Items) != 100 || page.Next == "" || page.Next == after {
					t.Fatal("stalled cursor")
				}
				after = page.Next
			}
			if !reflect.DeepEqual(expected, actual) {
				t.Fatal("skipped or duplicate resource")
			}
			if c.tool == "list_workflow_statuses" {
				statusIDs = actual
			}
		})
	}
	reverse := func() {
		for i, j := 0, len(statusIDs)-1; i < j; i, j = i+1, j-1 {
			statusIDs[i], statusIDs[j] = statusIDs[j], statusIDs[i]
		}
	}
	reverse()
	raw := f.http(t, "POST", sp+"/reorder", f.bearer, "large-http", map[string]any{"status_ids": statusIDs, "expected_workflow_version": 1}, 201, "")
	if !bytes.Equal(raw, transportTool(t, f.ctx, cs, "reorder_workflow_statuses", f.w, "large-http", team.ReorderInput{TeamID: a.ID, StatusIDs: statusIDs, ExpectedWorkflowVersion: 1}, "")) {
		t.Fatal("500-status receipt differs")
	}
	reverse()
	transportTool(t, f.ctx, cs, "reorder_workflow_statuses", f.w, "large-sdk", team.ReorderInput{TeamID: a.ID, StatusIDs: statusIDs, ExpectedWorkflowVersion: 2}, "")
	rows, err := f.pool.Query(f.ctx, "SELECT uuid,position,version FROM workflow_statuses ORDER BY position")
	if err != nil {
		t.Fatal(err)
	}
	index := 0
	for rows.Next() {
		var id uuid.UUID
		var position, version int64
		if err = rows.Scan(&id, &position, &version); err != nil {
			t.Fatal(err)
		}
		if index >= 500 || id.String() != statusIDs[index] || position != int64(index) || version != 3 {
			t.Fatal("full reorder state differs")
		}
		index++
	}
	rows.Close()
	if rows.Err() != nil || index != 500 {
		t.Fatal("reorder state incomplete")
	}

	bad := append([]string(nil), statusIDs...)
	bad[1] = bad[0]
	transportTool(t, f.ctx, cs, "reorder_workflow_statuses", f.w, "duplicate", team.ReorderInput{TeamID: a.ID, StatusIDs: bad, ExpectedWorkflowVersion: 3}, operations.Validation)
	transportTool(t, f.ctx, cs, "reorder_workflow_statuses", f.w, "incomplete", team.ReorderInput{TeamID: a.ID, StatusIDs: statusIDs[:499], ExpectedWorkflowVersion: 3}, operations.Validation)
}

func TestTeamsTransportCurrentCredentialAuthorizationAndIsolation(t *testing.T) {
	f := newTeamTransport(t)
	human := f.client(t, f.ctx)
	a := f.createTeam(t, "AU")
	b := f.createTeam(t, "BU")
	base := "/v1/workspaces/" + f.w.String()
	path := base + "/teams/" + a.ID.String()
	mint := func(audience string, scopes []string) ws.CreateCredentialOutput {
		return transportDecode[ws.CreateCredentialOutput](t, transportTool(t, f.ctx, human, "create_workspace_credential", f.w, uuid.NewString(), ws.CreateCredentialInput{Name: "Transport", Audience: audience, Scopes: scopes, ExpiresAt: time.Now().Add(time.Hour).UTC().Truncate(time.Second)}, ""))
	}
	api := mint("api", []string{"workspace.read", "workspace.write"})
	pat := mint("mcp", []string{"workspace.read", "workspace.write"})
	read := mint("mcp", []string{"workspace.read"})
	apiRead := mint("api", []string{"workspace.read"})
	f.http(t, "PATCH", path, apiRead.Token, "api-read-write", map[string]any{"name": "Denied", "expected_version": 1}, 403, operations.Forbidden)
	client := f.client(t, ws.WithToolCredential(context.Background(), pat.Token))
	apiClient := f.client(t, ws.WithToolCredential(context.Background(), api.Token))
	readClient := f.client(t, ws.WithToolCredential(context.Background(), read.Token))
	transportTool(t, f.ctx, apiClient, "get_team", f.w, "", team.TeamInput{TeamID: a.ID}, operations.Forbidden)
	f.http(t, "GET", path, pat.Token, "", nil, 403, operations.Forbidden)
	f.http(t, "GET", path, api.Token, "", nil, 200, "")
	limited := transportDecode[team.TeamOutput](t, transportTool(t, f.ctx, readClient, "get_team", f.w, "", team.TeamInput{TeamID: a.ID}, ""))
	if limited.Capabilities.Manage {
		t.Fatal("read token obtained manage")
	}
	transportTool(t, f.ctx, readClient, "update_team", f.w, "read-write", team.UpdateTeamInput{TeamID: a.ID, Name: "Denied", ExpectedVersion: 1}, operations.Forbidden)
	other := transportDecode[ws.WorkspaceOutput](t, transportTool(t, f.ctx, human, "create_workspace", uuid.Nil, "other", ws.CreateInput{Name: "Other"}, "")).Workspace.ID
	transportTool(t, f.ctx, client, "get_team", other, "", team.TeamInput{TeamID: a.ID}, operations.Forbidden)
	f.http(t, "GET", "/v1/workspaces/"+other.String()+"/teams/"+a.ID.String(), api.Token, "", nil, 403, operations.Forbidden)
	transportTool(t, f.ctx, human, "get_team", other, "", team.TeamInput{TeamID: a.ID}, operations.NotFound)
	statuses := transportDecode[team.StatusList](t, transportTool(t, f.ctx, human, "list_workflow_statuses", f.w, "", team.StatusListInput{TeamID: b.ID, Limit: 100}, ""))
	transportTool(t, f.ctx, human, "update_workflow_status", f.w, "wrong-team", team.UpdateStatusInput{TeamID: a.ID, StatusID: statuses.Items[0].ID, Name: "Wrong", Category: "started", Color: "#123456", ExpectedVersion: 1, ExpectedWorkflowVersion: 1}, operations.NotFound)
	foreign := transportDecode[team.TeamOutput](t, transportTool(t, f.ctx, human, "create_team", other, "foreign", team.CreateTeamInput{Name: "Foreign", Code: "FR"}, "")).Team.ID
	for _, target := range []struct {
		tool, path string
		input      any
	}{{"list_teams", base + "/teams", team.TeamListInput{Limit: 100, After: foreign.String()}}, {"list_workflow_statuses", path + "/workflow-statuses", team.StatusListInput{TeamID: a.ID, Limit: 100, After: statuses.Items[0].ID.String()}}} {
		raw := transportTool(t, f.ctx, human, target.tool, f.w, "", target.input, "")
		inputJSON := transportJSON(t, target.input)
		var cursorInput struct {
			After string `json:"after"`
		}
		if err := json.Unmarshal(inputJSON, &cursorInput); err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(raw, f.http(t, "GET", target.path+"?limit=100&after="+url.QueryEscape(cursorInput.After), f.bearer, "", nil, 200, "")) {
			t.Fatal("foreign cursor transport differs")
		}
		var page struct {
			Items []json.RawMessage `json:"items"`
			More  bool              `json:"has_more"`
		}
		if e := json.Unmarshal(raw, &page); e != nil || len(page.Items) != 0 || page.More {
			t.Fatalf("%s foreign cursor crossed boundary", target.tool)
		}
	}
	edit := team.UpdateTeamInput{TeamID: a.ID, Name: "Authorized", ExpectedVersion: 1}
	transportTool(t, f.ctx, client, "update_team", f.w, "auth-replay", edit, "")
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, e := f.pool.Exec(f.ctx, sql, args...); e != nil {
			t.Fatal(e)
		}
	}
	replay := func(code operations.Code) {
		t.Helper()
		transportTool(t, f.ctx, client, "update_team", f.w, "auth-replay", edit, code)
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
	// API receipt replay must also recheck current token status.
	httpEdit := map[string]any{"name": "API authorized", "expected_version": 2}
	f.http(t, "PATCH", path, api.Token, "api-replay", httpEdit, 200, "")
	exec("UPDATE users SET status='disabled' WHERE uuid=$1", f.actor)
	f.http(t, "PATCH", path, api.Token, "api-replay", httpEdit, 401, operations.Unauthenticated)
}
