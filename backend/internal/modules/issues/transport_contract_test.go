package issues_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"github.com/furkanmeclis/traccia/backend/internal/config"
	"github.com/furkanmeclis/traccia/backend/internal/database/db"
	attachmentsrepo "github.com/furkanmeclis/traccia/backend/internal/modules/attachments/repository"
	attachments "github.com/furkanmeclis/traccia/backend/internal/modules/attachments/usecase"
	commentsrepo "github.com/furkanmeclis/traccia/backend/internal/modules/comments/repository"
	comments "github.com/furkanmeclis/traccia/backend/internal/modules/comments/usecase"
	docsrepo "github.com/furkanmeclis/traccia/backend/internal/modules/documents/repository"
	docs "github.com/furkanmeclis/traccia/backend/internal/modules/documents/usecase"
	productsrepo "github.com/furkanmeclis/traccia/backend/internal/modules/products/repository"
	products "github.com/furkanmeclis/traccia/backend/internal/modules/products/usecase"
	projectsrepo "github.com/furkanmeclis/traccia/backend/internal/modules/projects/repository"
	projects "github.com/furkanmeclis/traccia/backend/internal/modules/projects/usecase"
	searchrepo "github.com/furkanmeclis/traccia/backend/internal/modules/searchviews/repository"
	searchviews "github.com/furkanmeclis/traccia/backend/internal/modules/searchviews/usecase"
	teamsrepo "github.com/furkanmeclis/traccia/backend/internal/modules/teams/repository"
	teams "github.com/furkanmeclis/traccia/backend/internal/modules/teams/usecase"
	"github.com/furkanmeclis/traccia/backend/internal/platform/storage"
	"github.com/furkanmeclis/traccia/backend/internal/product"
	"net/url"

	issue "github.com/furkanmeclis/traccia/backend/internal/modules/issues/usecase"
	relationsrepo "github.com/furkanmeclis/traccia/backend/internal/modules/relations/repository"
	relations "github.com/furkanmeclis/traccia/backend/internal/modules/relations/usecase"
	module "github.com/furkanmeclis/traccia/backend/internal/modules/workspaces"
	ws "github.com/furkanmeclis/traccia/backend/internal/modules/workspaces/usecase"
	"github.com/furkanmeclis/traccia/backend/internal/platform/authctx"
	"github.com/furkanmeclis/traccia/backend/internal/platform/contractcoverage"
	"github.com/furkanmeclis/traccia/backend/internal/platform/jwt"
	"github.com/furkanmeclis/traccia/backend/internal/platform/mcpbindings"
	"github.com/furkanmeclis/traccia/backend/internal/platform/operations"
	"github.com/furkanmeclis/traccia/backend/internal/platform/outbox"
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

type issueTransport struct {
	issueFixture
	mux      *http.ServeMux
	bearer   string
	document contractcoverage.Object
}

var transportCatalog sync.Map

func newIssueTransport(t *testing.T) *issueTransport {
	t.Helper()
	f := newIssueFixture(t)
	attachmentConfig, e := config.Load()
	if e != nil {
		t.Fatal("attachment configuration failed")
	}
	attachmentDriver, e := storage.NewFromConfig(f.ctx, attachmentConfig.Storage)
	if e != nil {
		t.Fatal("attachment storage initialization failed")
	}
	attachmentStore, e := attachmentsrepo.NewObjectStore(attachmentDriver)
	if e != nil {
		t.Fatal(e)
	}
	attachmentRepo, e := attachmentsrepo.New(f.pool, db.New(f.pool), attachmentStore, attachments.DefaultOptions())
	if e != nil {
		t.Fatal(e)
	}
	reg, err := product.Registry(f.workspace, teams.New(teamsrepo.New(f.pool, db.New(f.pool)), f.workspace), projects.New(projectsrepo.New(f.pool, db.New(f.pool)), f.workspace), products.New(productsrepo.New(f.pool, db.New(f.pool)), f.workspace), issue.New(f.repo, f.workspace), relations.New(relationsrepo.New(f.pool, db.New(f.pool)), f.workspace), docs.New(docsrepo.New(f.pool, db.New(f.pool)), f.workspace, issue.New(f.repo, f.workspace)), attachments.New(attachmentRepo, f.workspace), comments.New(commentsrepo.New(f.pool, db.New(f.pool)), f.workspace), searchviews.New(searchrepo.New(f.pool, db.New(f.pool)), f.workspace))
	if err != nil {
		t.Fatal(err)
	}
	f.registry = reg
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
	return &issueTransport{issueFixture: f, mux: mux, bearer: bearer, document: doc}
}
func transportJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
func (f *issueTransport) http(t *testing.T, method, path, token, key string, input any, status int, code operations.Code) []byte {
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
func (f *issueTransport) client(t *testing.T, serverCtx context.Context) *mcp.ClientSession {
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
func (f *issueTransport) invoke(t *testing.T, cs *mcp.ClientSession, name, key string, input map[string]any) []byte {
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
		for _, pair := range [][2]string{{"issueID", "issue_id"}, {"identifier", "identifier"}} {
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
func issueMap(t *testing.T, value any) map[string]any {
	t.Helper()
	d := json.NewDecoder(bytes.NewReader(transportJSON(t, value)))
	d.UseNumber()
	var out map[string]any
	if e := d.Decode(&out); e != nil {
		t.Fatal(e)
	}
	return out
}
func (f *issueTransport) sql(t *testing.T, q string, args ...any) {
	t.Helper()
	if _, e := f.pool.Exec(f.ctx, q, args...); e != nil {
		t.Fatal(e)
	}
}
func TestIssuesTransportLifecycleExactReplayAndAliases(t *testing.T) {
	f := newIssueTransport(t)
	cs := f.client(t, f.ctx)
	listed, e := cs.ListTools(f.ctx, nil)
	if e != nil {
		t.Fatal(e)
	}
	names := map[string]*mcp.Tool{}
	for _, v := range listed.Tools {
		names[v.Name] = v
	}
	count := 0
	for _, c := range f.registry.Catalog() {
		if strings.HasPrefix(c.Metadata.ID, "issues.") {
			count++
			v := names[c.Metadata.MCPName]
			if v == nil || v.InputSchema == nil || v.OutputSchema == nil || v.Meta["traccia/error_schema"] == nil || v.Meta["traccia/operation_id"] != c.Metadata.ID {
				t.Fatal("incomplete SDK discovery")
			}
		}
	}
	if count != 10 || len(f.registry.Catalog()) != 129 {
		t.Fatalf("issues=%d total=%d", count, len(f.registry.Catalog()))
	}
	a, b := f.createTeam(t, "AAA"), f.createTeam(t, "BBB")
	text := "İstanbul 🧭 e\u0301 漢字 <plate>\"&</plate>"
	create := issueMap(t, issue.CreateInput{TeamID: a.ID, Title: text, Priority: "urgent", StatusID: f.status(t, a.ID), LabelIDs: []string{}})
	frozen := transportJSON(t, create)
	raw := f.invoke(t, cs, "create_issue", "create", create)
	i := transportDecode[issue.Output](t, raw).Issue
	if i.Title != text || i.Number != 1 || i.AssigneeID != nil || i.LabelIDs == nil {
		t.Fatal("content/null fidelity")
	}
	if !bytes.Equal(raw, f.invoke(t, cs, "create_issue", "create", transportDecode[map[string]any](t, frozen))) {
		t.Fatal("receipt bytes changed")
	}
	changed := issueMap(t, create)
	changed["title"] = "Changed"
	transportTool(t, f.ctx, cs, "create_issue", f.w, "create", changed, operations.IdempotencyConflict)
	f.http(t, "POST", "/v1/workspaces/"+f.w.String()+"/issues", f.bearer, "create", changed, 409, operations.IdempotencyConflict)
	var n int
	if e = f.pool.QueryRow(f.ctx, "SELECT count(*) FROM issues").Scan(&n); e != nil || n != 1 {
		t.Fatalf("duplicate issues=%d err=%v", n, e)
	}
	aliases := []string{i.Identifier}
	for _, target := range []uuid.UUID{b.ID, a.ID} {
		moved := transportDecode[issue.Output](t, f.invoke(t, cs, "move_issue", uuid.NewString(), issueMap(t, issue.MoveInput{IssueID: i.ID, TeamID: target, StatusID: f.status(t, target), LabelIDs: []string{}, ExpectedVersion: i.Version})))
		if moved.Issue.ID != i.ID || moved.Issue.Identifier == i.Identifier {
			t.Fatal("move identity/number allocation")
		}
		i = moved.Issue
		aliases = append(aliases, i.Identifier)
	}
	if i.Number != 2 {
		t.Fatal("move back reused old number")
	}
	for _, alias := range aliases {
		out := transportDecode[issue.Output](t, f.invoke(t, cs, "resolve_issue", "", map[string]any{"identifier": alias}))
		if out.Issue.ID != i.ID || out.Issue.Identifier != i.Identifier {
			t.Fatal("old alias no longer resolves stable UUID")
		}
	}

	for _, alias := range aliases {
		encoded := strings.ReplaceAll(alias, "-", "%2D")
		out := transportDecode[issue.Output](t, f.http(t, "GET", "/v1/workspaces/"+f.w.String()+"/issue-identifiers/"+encoded, f.bearer, "", nil, 200, ""))
		if out.Issue.ID != i.ID {
			t.Fatal("encoded alias changed identity")
		}
	}
	edit := metadata(i)
	edit.Title = text + " güncel"
	zero := int32(0)
	edit.Effort = &zero
	start, target := "2026-10-18", "2026-10-19"
	edit.StartDate = &start
	edit.TargetDate = &target
	i = transportDecode[issue.Output](t, f.invoke(t, cs, "update_issue", "edit", issueMap(t, edit))).Issue
	transportTool(t, f.ctx, cs, "update_issue", f.w, "stale", edit, operations.VersionConflict)
	body := issueMap(t, edit)
	delete(body, "issue_id")
	f.http(t, "PATCH", "/v1/workspaces/"+f.w.String()+"/issues/"+i.ID.String(), f.bearer, "stale-http", body, 409, operations.VersionConflict)
	edit = metadata(i)
	edit.Effort = nil
	edit.StartDate = nil
	edit.TargetDate = nil
	i = transportDecode[issue.Output](t, f.invoke(t, cs, "update_issue", "clear", issueMap(t, edit))).Issue
	if i.Effort != nil || i.StartDate != nil || i.TargetDate != nil {
		t.Fatal("explicit null did not clear")
	}
	archived := transportDecode[issue.Output](t, f.invoke(t, cs, "archive_issue", "archive", issueMap(t, issue.VersionInput{IssueID: i.ID, ExpectedVersion: i.Version})))
	if archived.Capabilities.Manage || !archived.Capabilities.Restore {
		t.Fatal("archive capabilities")
	}
	i = transportDecode[issue.Output](t, f.invoke(t, cs, "restore_issue", "restore", issueMap(t, issue.VersionInput{IssueID: i.ID, ExpectedVersion: archived.Issue.Version}))).Issue
	f.invoke(t, cs, "get_issue", "", map[string]any{"issue_id": i.ID.String()})
	history := transportDecode[issue.HistoryList](t, f.invoke(t, cs, "history_issue", "", issueMap(t, issue.HistoryInput{IssueID: i.ID, Limit: 100})))
	expected := []string{"issues.created", "issues.moved", "issues.moved", "issues.updated", "issues.updated", "issues.archived", "issues.restored"}
	if len(history.Items) != len(expected) || history.HasMore {
		t.Fatalf("history rows=%d", len(history.Items))
	}
	for j, h := range history.Items {
		if h.ActorID != f.actor || h.WorkspaceID != f.w || h.IssueID != i.ID || h.EventName != expected[j] || h.Version != int64(j+1) || h.Issue.Version != h.Version {
			t.Fatal("canonical history context/version")
		}
	}
	if history.Items[0].Issue.Title != text || history.Items[0].Issue.Identifier != aliases[0] {
		t.Fatal("historical snapshot mutated")
	}
	if e = f.pool.QueryRow(f.ctx, "SELECT count(*) FROM outbox_events o JOIN issue_history h ON h.event_id=o.id WHERE h.issue_id=(SELECT id FROM issues WHERE uuid=$1)", i.ID).Scan(&n); e != nil || n != len(expected) {
		t.Fatalf("atomic outbox/history rows=%d err=%v", n, e)
	}

	store := outbox.NewStore(f.pool, db.New(f.pool))
	consumers, e := f.workspaceRepo.DurableConsumers()
	if e != nil {
		t.Fatal(e)
	}
	claims, e := store.Claim(f.ctx, 100, time.Minute)
	if e != nil {
		t.Fatal(e)
	}
	for _, claim := range claims {
		if e = store.Process(f.ctx, claim, consumers); e != nil {
			t.Fatal(e)
		}
	}
	rows, e := f.workspaceRepo.ListActivity(f.ctx, f.w, uuid.Nil, 100)
	if e != nil {
		t.Fatal(e)
	}
	projected := 0
	for _, row := range rows {
		if row.ResourceType == "issues" {
			projected++
			if row.IssueID != i.ID.String() || row.ResourceID != i.ID || row.Identifier == "" {
				t.Fatal("durable activity missing issue context")
			}
		}
	}
	if projected != len(expected) {
		t.Fatalf("durable projection rows=%d", projected)
	}
	f.sql(t, "UPDATE teams SET next_issue_number=9007199254740993 WHERE uuid=$1", b.ID)
	exact := f.invoke(t, cs, "create_issue", "big", issueMap(t, issue.CreateInput{TeamID: b.ID, Title: "Exact bigint", Priority: "none", StatusID: f.status(t, b.ID), LabelIDs: []string{}}))
	bi := transportDecode[issue.Output](t, exact).Issue
	if bi.Number != 9007199254740993 || !bytes.Contains(exact, []byte(`"number":9007199254740993`)) {
		t.Fatal("BIGINT rounded in transport")
	}
	f.invoke(t, cs, "resolve_issue", "", map[string]any{"identifier": bi.Identifier})
	f.sql(t, "UPDATE issues SET version=9007199254740993 WHERE uuid=$1", bi.ID)
	version := f.invoke(t, cs, "get_issue", "", map[string]any{"issue_id": bi.ID.String()})
	if !bytes.Contains(version, []byte(`"version":9007199254740993`)) {
		t.Fatal("BIGINT version rounded")
	}

}
func TestIssuesTransportStrictInputs(t *testing.T) {
	f := newIssueTransport(t)
	cs := f.client(t, f.ctx)
	a := f.createTeam(t, "AAA")
	i := f.create(t, a.ID)
	base := "/v1/workspaces/" + f.w.String() + "/issues"
	path := base + "/" + i.ID.String()
	full := issueMap(t, metadata(i))
	delete(full, "issue_id")
	for _, name := range []string{"title", "priority", "status_id", "assignee_id", "project_id", "milestone_id", "start_date", "target_date", "effort", "label_ids", "expected_version"} {
		t.Run("omitted-"+name, func(t *testing.T) {
			body := issueMap(t, full)
			delete(body, name)
			f.http(t, "PATCH", path, f.bearer, uuid.NewString(), body, 400, operations.Validation)
			sdk := issueMap(t, body)
			sdk["issue_id"] = i.ID.String()
			transportTool(t, f.ctx, cs, "update_issue", f.w, uuid.NewString(), sdk, operations.Validation)
		})
	}
	for _, name := range []string{"actor_id", "workspace_id", "issue_id", "description", "unknown"} {
		t.Run("unexpected-"+name, func(t *testing.T) {
			body := issueMap(t, full)
			body[name] = uuid.NewString()
			f.http(t, "PATCH", path, f.bearer, uuid.NewString(), body, 400, operations.Validation)
		})
	}
	for j, raw := range []string{`{"title":"a","title":"b"}`, `[]`, `null`, `{"title":`, string(transportJSON(t, full)) + ` {}`} {
		t.Run(fmt.Sprintf("json-%d", j), func(t *testing.T) {
			f.http(t, "PATCH", path, f.bearer, uuid.NewString(), json.RawMessage(raw), 400, operations.Validation)
		})
	}
	for _, query := range []string{"unknown=1", "limit=1&limit=2", "limit=101", "archived=yes", "after=bad", "assignee_id=null", "project_id=null", "priority=invalid"} {
		t.Run("query-"+url.QueryEscape(query), func(t *testing.T) { f.http(t, "GET", base+"?"+query, f.bearer, "", nil, 400, operations.Validation) })
	}

	for _, bad := range []string{"aaa-1", "AAA-0", "AAA-01", "AAA-90071992547409933333", "AAA%2F1", "AAA%00-1"} {
		t.Run("bad-identifier-"+bad, func(t *testing.T) {
			f.http(t, "GET", "/v1/workspaces/"+f.w.String()+"/issue-identifiers/"+bad, f.bearer, "", nil, 400, operations.Validation)
		})
	}
	f.http(t, "GET", "/v1/workspaces/"+f.w.String()+"/issue-identifiers/"+i.Identifier+"?identifier="+i.Identifier, f.bearer, "", nil, 400, operations.Validation)
	for _, bad := range []string{"AAA-1", "bad-uuid", uuid.Nil.String()} {
		t.Run("bad-uuid-"+bad, func(t *testing.T) { f.http(t, "GET", base+"/"+bad, f.bearer, "", nil, 400, operations.Validation) })
	}
	f.http(t, "GET", path, "", "", nil, 401, operations.Code("UNAUTHENTICATED"))
	for _, name := range []string{"actor_id", "channel", "permission"} {
		t.Run("sdk-wrapper-"+name, func(t *testing.T) {
			args := map[string]any{"workspace_id": f.w.String(), "input": map[string]any{"issue_id": i.ID.String()}, name: f.actor.String()}
			r, e := cs.CallTool(f.ctx, &mcp.CallToolParams{Name: "get_issue", Arguments: json.RawMessage(transportJSON(t, args))})
			if e == nil && (r == nil || !r.IsError) {
				t.Fatal("SDK wrapper spoof accepted")
			}
		})
	}
}
func TestIssuesTransportCredentialsRevalidatedOnReplay(t *testing.T) {
	f := newIssueTransport(t)
	human := f.client(t, f.ctx)
	a := f.createTeam(t, "AAA")
	i := f.create(t, a.ID)
	mint := func(audience string, scopes []string) ws.CreateCredentialOutput {
		return transportDecode[ws.CreateCredentialOutput](t, transportTool(t, f.ctx, human, "create_workspace_credential", f.w, uuid.NewString(), ws.CreateCredentialInput{Name: "Issue acceptance", Audience: audience, Scopes: scopes, ExpiresAt: time.Now().Add(time.Hour).UTC().Truncate(time.Second)}, ""))
	}
	api := mint("api", []string{"workspace.read", "workspace.write"})
	pat := mint("mcp", []string{"workspace.read", "workspace.write"})
	read := mint("mcp", []string{"workspace.read"})
	client := f.client(t, ws.WithToolCredential(context.Background(), pat.Token))
	reader := f.client(t, ws.WithToolCredential(context.Background(), read.Token))
	wrong := f.client(t, ws.WithToolCredential(context.Background(), api.Token))
	get := map[string]any{"issue_id": i.ID.String()}
	path := "/v1/workspaces/" + f.w.String() + "/issues/" + i.ID.String()
	transportTool(t, f.ctx, wrong, "get_issue", f.w, "", get, operations.Forbidden)
	f.http(t, "GET", path, pat.Token, "", nil, 403, operations.Forbidden)
	f.http(t, "GET", path, api.Token, "", nil, 200, "")
	out := transportDecode[issue.Output](t, transportTool(t, f.ctx, reader, "get_issue", f.w, "", get, ""))
	if out.Capabilities.Manage || out.Capabilities.Move || out.Capabilities.Restore {
		t.Fatal("read-only credential advertises write")
	}
	edit := metadata(i)
	edit.Title = "Credential edit"
	transportTool(t, f.ctx, reader, "update_issue", f.w, "denied", edit, operations.Forbidden)
	transportTool(t, f.ctx, client, "update_issue", f.w, "receipt", edit, "")
	f.sql(t, "UPDATE workspace_tool_credentials SET created_at=clock_timestamp()-interval '2 hours',expires_at=clock_timestamp()-interval '1 second' WHERE uuid=$1", pat.Credential.ID)
	transportTool(t, f.ctx, client, "update_issue", f.w, "receipt", edit, operations.Unauthenticated)
	f.sql(t, "UPDATE workspace_tool_credentials SET expires_at=clock_timestamp()+interval '1 hour' WHERE uuid=$1", pat.Credential.ID)
	transportTool(t, f.ctx, client, "update_issue", f.w, "receipt", edit, "")
	transportTool(t, f.ctx, human, "revoke_workspace_credential", f.w, "revoke", ws.RevokeInput{CredentialID: pat.Credential.ID}, "")
	transportTool(t, f.ctx, client, "update_issue", f.w, "receipt", edit, operations.Unauthenticated)
	f.sql(t, "UPDATE users SET status='disabled' WHERE uuid=$1", f.actor)
	f.http(t, "GET", path, api.Token, "", nil, 401, operations.Unauthenticated)
	transportTool(t, f.ctx, reader, "get_issue", f.w, "", get, operations.Unauthenticated)
}
func TestIssuesTransportCompletePaginationAndImmutableAnchor(t *testing.T) {
	f := newIssueTransport(t)
	cs := f.client(t, f.ctx)
	a, b := f.createTeam(t, "AAA"), f.createTeam(t, "BBB")
	s := f.status(t, a.ID)
	// Real mutations populate the history and immutable aliases as production does.
	for j := 0; j < 137; j++ {
		f.invoke(t, cs, "create_issue", fmt.Sprintf("seed-%d", j), issueMap(t, issue.CreateInput{TeamID: a.ID, Title: fmt.Sprintf("Needle %03d", j), Priority: "high", StatusID: s, LabelIDs: []string{}}))
	}
	base := issueMap(t, issue.PageInput{Limit: 100, Q: "Needle", TeamID: strings.ToUpper(a.ID.String()), AssigneeID: "unassigned", ProjectID: "unassigned", MilestoneID: "unassigned", Priority: "high"})
	first := transportDecode[issue.List](t, f.invoke(t, cs, "list_issue", "", base))
	if len(first.Items) != 100 || !first.HasMore || first.NextCursor == "" {
		t.Fatal("first page incomplete")
	}
	anchor := first.Items[99]
	f.invoke(t, cs, "move_issue", "anchor-move", issueMap(t, issue.MoveInput{IssueID: anchor.ID, TeamID: b.ID, StatusID: f.status(t, b.ID), LabelIDs: []string{}, ExpectedVersion: 1}))
	base["after"] = first.NextCursor
	tail := transportDecode[issue.List](t, f.invoke(t, cs, "list_issue", "", base))
	if len(tail.Items) != 37 || tail.HasMore || tail.NextCursor != "" {
		t.Fatalf("moved anchor remaining=%d", len(tail.Items))
	}
	seen := map[uuid.UUID]bool{}
	for _, page := range [][]issue.Issue{first.Items, tail.Items} {
		for _, v := range page {
			if seen[v.ID] {
				t.Fatal("duplicate pagination row")
			}
			seen[v.ID] = true
		}
	}
	if len(seen) != 137 {
		t.Fatal("incomplete traversal")
	}
	base["after"] = ""
	base["limit"] = 1
	p := transportDecode[issue.List](t, f.invoke(t, cs, "list_issue", "", base))
	f.invoke(t, cs, "archive_issue", "anchor-archive", issueMap(t, issue.VersionInput{IssueID: p.Items[0].ID, ExpectedVersion: 1}))
	base["after"] = p.NextCursor
	base["limit"] = 100
	remaining := 0
	for j := 0; ; j++ {
		if j > 3 {
			t.Fatal("cursor stalled")
		}
		p = transportDecode[issue.List](t, f.invoke(t, cs, "list_issue", "", base))
		remaining += len(p.Items)
		if !p.HasMore {
			break
		}
		base["after"] = p.NextCursor
	}
	if remaining != 135 {
		t.Fatalf("archived anchor remaining=%d", remaining)
	}
	// Hash metadata is public; a fabricated UUID anchor must still be rejected.
	raw, e := base64.RawURLEncoding.DecodeString(first.NextCursor)
	if e != nil {
		t.Fatal(e)
	}
	var cursor map[string]any
	if e = json.Unmarshal(raw, &cursor); e != nil {
		t.Fatal(e)
	}
	cursor["anchor"] = uuid.NewString()
	base["after"] = base64.RawURLEncoding.EncodeToString(transportJSON(t, cursor))
	transportTool(t, f.ctx, cs, "list_issue", f.w, "", base, operations.Validation)
	for _, filter := range []string{"", "unassigned"} {
		t.Run("nullable-"+filter, func(t *testing.T) {
			in := issueMap(t, issue.PageInput{Limit: 100, AssigneeID: filter, ProjectID: filter, MilestoneID: filter})
			out := transportDecode[issue.List](t, f.invoke(t, cs, "list_issue", "", in))
			if len(out.Items) != 100 || !out.HasMore {
				t.Fatal("nullable filter lost rows")
			}
		})
	}

	project := f.createProject(t, a.ID, b.ID)
	milestone := transportDecode[projects.MilestoneOutput](t, transportTool(t, f.ctx, cs, "create_project_milestone", f.w, "filter-milestone", projects.CreateMilestoneInput{ProjectID: project.ID, Name: "Filter milestone", ExpectedProjectVersion: project.Version}, "")).Milestone
	label := transportDecode[teams.LabelOutput](t, transportTool(t, f.ctx, cs, "create_label", f.w, "filter-label", teams.CreateLabelInput{Name: "Filter label", Color: "#333333"}, "")).Label
	f.sql(t, "UPDATE issues SET project_id=(SELECT id FROM projects WHERE uuid=$1),milestone_id=(SELECT id FROM project_milestones WHERE uuid=$2),assignee_id=(SELECT id FROM users WHERE uuid=$3)", project.ID, milestone.ID, f.actor)
	f.sql(t, "INSERT INTO issue_labels(workspace_id,issue_id,label_id) SELECT i.workspace_id,i.id,l.id FROM issues i JOIN labels l ON l.workspace_id=i.workspace_id WHERE l.uuid=$1", label.ID)
	filters := []struct {
		name, value string
		count       int
	}{
		{"team_id", a.ID.String(), 135}, {"status_id", s.String(), 135}, {"assignee_id", f.actor.String(), 136}, {"project_id", project.ID.String(), 136}, {"milestone_id", milestone.ID.String(), 136}, {"label_id", label.ID.String(), 136}, {"priority", "high", 136},
	}
	for _, filter := range filters {
		t.Run("complete-filter-"+filter.name, func(t *testing.T) {
			in := issueMap(t, issue.PageInput{Limit: 100})
			in[filter.name] = strings.ToUpper(filter.value)
			if filter.name == "priority" {
				in[filter.name] = filter.value
			}
			seen := map[uuid.UUID]bool{}
			for j := 0; ; j++ {
				if j > 2 {
					t.Fatal("filter cursor stalled")
				}
				out := transportDecode[issue.List](t, f.invoke(t, cs, "list_issue", "", in))
				for _, row := range out.Items {
					if seen[row.ID] {
						t.Fatal("duplicate filtered row")
					}
					seen[row.ID] = true
				}
				if !out.HasMore {
					if out.NextCursor != "" {
						t.Fatal("terminal filter cursor")
					}
					break
				}
				if len(out.Items) != 100 {
					t.Fatal("short filter page")
				}
				in["after"] = out.NextCursor
			}
			if len(seen) != filter.count {
				t.Fatalf("filter rows=%d want=%d", len(seen), filter.count)
			}
		})
	}
	// Search remains user text: a UUID-looking search must not be normalized to an ID filter.
	in := issueMap(t, issue.PageInput{Limit: 100, Q: strings.ToUpper(a.ID.String())})
	out := transportDecode[issue.List](t, f.invoke(t, cs, "list_issue", "", in))
	if len(out.Items) != 0 {
		t.Fatal("text search treated UUID as team filter")
	}
}
func TestIssuesTransportReferenceBoundariesAndRetainedHistory(t *testing.T) {
	f := newIssueTransport(t)
	cs := f.client(t, f.ctx)
	a, b := f.createTeam(t, "AAA"), f.createTeam(t, "BBB")
	p := f.createProject(t, a.ID, b.ID)
	other := f.createProject(t, b.ID)
	milestone := transportDecode[projects.MilestoneOutput](t, transportTool(t, f.ctx, cs, "create_project_milestone", f.w, "milestone", projects.CreateMilestoneInput{ProjectID: p.ID, Name: "Milestone", ExpectedProjectVersion: p.Version}, "")).Milestone
	label := transportDecode[teams.LabelOutput](t, transportTool(t, f.ctx, cs, "create_label", f.w, "label", teams.CreateLabelInput{Name: "Shared", Color: "#111111"}, "")).Label
	local := transportDecode[teams.LabelOutput](t, transportTool(t, f.ctx, cs, "create_label", f.w, "local-label", teams.CreateLabelInput{Name: "Team B", Color: "#222222", TeamID: b.ID.String()}, "")).Label
	member := uuid.New()
	f.sql(t, "INSERT INTO users(uuid,email,password_hash,name,surname,status) VALUES($1,$2,'unused','Assignee','User','active')", member, member.String()+"@example.test")
	f.sql(t, "INSERT INTO workspace_members(workspace_id,user_id,role) SELECT w.id,u.id,'member' FROM workspaces w,users u WHERE w.uuid=$1 AND u.uuid=$2", f.w, member)
	pid, mid, assignee := p.ID.String(), milestone.ID.String(), member.String()
	input := issue.CreateInput{TeamID: a.ID, Title: "Retained refs", Priority: "normal", StatusID: f.status(t, a.ID), ProjectID: &pid, MilestoneID: &mid, AssigneeID: &assignee, LabelIDs: []string{label.ID.String()}}
	i := transportDecode[issue.Output](t, f.invoke(t, cs, "create_issue", "retained", issueMap(t, input))).Issue
	cases := []struct {
		name, field string
		value       any
		code        operations.Code
	}{
		{"wrong-status", "status_id", f.status(t, b.ID).String(), operations.Validation}, {"wrong-team-label", "label_ids", []string{local.ID.String()}, operations.Validation}, {"noncontributing-project", "project_id", other.ID.String(), operations.Validation}, {"unknown-assignee", "assignee_id", uuid.NewString(), operations.Validation}, {"unknown-project", "project_id", uuid.NewString(), operations.NotFound}, {"unknown-milestone", "milestone_id", uuid.NewString(), operations.Validation}, {"unknown-label", "label_ids", []string{uuid.NewString()}, operations.Validation},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			in := issueMap(t, input)
			in[c.field] = c.value
			transportTool(t, f.ctx, cs, "create_issue", f.w, uuid.NewString(), in, c.code)
			status := 400
			if c.code == operations.NotFound {
				status = 404
			}
			f.http(t, "POST", "/v1/workspaces/"+f.w.String()+"/issues", f.bearer, uuid.NewString(), in, status, c.code)
		})
	}
	f.sql(t, "UPDATE workspace_members SET active=false WHERE user_id=(SELECT id FROM users WHERE uuid=$1)", member)
	f.sql(t, "UPDATE projects SET archived=true WHERE uuid=$1", p.ID)
	f.sql(t, "UPDATE project_milestones SET archived=true WHERE uuid=$1", milestone.ID)
	f.sql(t, "UPDATE labels SET archived=true WHERE uuid=$1", label.ID)
	f.sql(t, "UPDATE workflow_statuses SET archived=true WHERE uuid=$1", i.StatusID)
	edit := metadata(i)
	edit.Title = "Keep historical refs"
	i = transportDecode[issue.Output](t, f.invoke(t, cs, "update_issue", "keep", issueMap(t, edit))).Issue
	if i.AssigneeID == nil || *i.AssigneeID != assignee || i.ProjectID == nil || *i.ProjectID != pid || len(i.LabelIDs) != 1 {
		t.Fatal("unchanged inactive refs lost")
	}
	for _, field := range []string{"status_id", "project_id", "label_ids"} {
		t.Run("new-archived-"+field, func(t *testing.T) {
			fresh := issue.CreateInput{TeamID: a.ID, Title: "New", Priority: "none", StatusID: f.status(t, a.ID), LabelIDs: []string{}}
			in := issueMap(t, fresh)
			switch field {
			case "status_id":
				in[field] = i.StatusID.String()
			case "project_id":
				in[field] = pid
			case "label_ids":
				in[field] = []string{label.ID.String()}
			}
			transportTool(t, f.ctx, cs, "create_issue", f.w, uuid.NewString(), in, operations.Conflict)
		})
	}
	fresh := issueMap(t, issue.CreateInput{TeamID: a.ID, Title: "Inactive new assignee", Priority: "none", StatusID: f.status(t, a.ID), AssigneeID: &assignee, LabelIDs: []string{}})
	transportTool(t, f.ctx, cs, "create_issue", f.w, "inactive-new", fresh, operations.Validation)
	f.sql(t, "UPDATE teams SET archived=true WHERE uuid=$1", a.ID)
	out := transportDecode[issue.Output](t, f.invoke(t, cs, "get_issue", "", map[string]any{"issue_id": i.ID.String()}))
	if !out.Capabilities.Move || out.Capabilities.Manage {
		t.Fatal("archived source capabilities")
	}
	moved := transportDecode[issue.Output](t, f.invoke(t, cs, "move_issue", "escape", issueMap(t, issue.MoveInput{IssueID: i.ID, TeamID: b.ID, StatusID: f.status(t, b.ID), LabelIDs: []string{}, ExpectedVersion: i.Version})))
	if !moved.Capabilities.Manage || moved.Issue.ProjectID != nil || moved.Issue.MilestoneID != nil {
		t.Fatal("explicit move compatibility/clear")
	}
	foreign := transportDecode[ws.WorkspaceOutput](t, transportTool(t, f.ctx, cs, "create_workspace", uuid.Nil, "foreign", ws.CreateInput{Name: "Foreign"}, "")).Workspace.ID
	for _, id := range []uuid.UUID{i.ID, uuid.New()} {
		transportTool(t, f.ctx, cs, "get_issue", foreign, "", map[string]any{"issue_id": id.String()}, operations.NotFound)
		f.http(t, "GET", "/v1/workspaces/"+foreign.String()+"/issues/"+id.String(), f.bearer, "", nil, 404, operations.NotFound)
	}
	transportTool(t, f.ctx, cs, "resolve_issue", foreign, "", map[string]any{"identifier": i.Identifier}, operations.NotFound)
}
