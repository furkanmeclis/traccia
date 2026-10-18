package relations_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/url"

	issue "github.com/furkanmeclis/traccia/backend/internal/modules/issues/usecase"
	relation "github.com/furkanmeclis/traccia/backend/internal/modules/relations/usecase"
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

type relationTransport struct {
	relationFixture
	mux      *http.ServeMux
	bearer   string
	document contractcoverage.Object
}

var transportCatalog sync.Map

func newRelationTransport(t *testing.T) *relationTransport {
	t.Helper()
	f := newRelationFixture(t)
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
	return &relationTransport{relationFixture: f, mux: mux, bearer: bearer, document: doc}
}
func transportJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
func (f *relationTransport) http(t *testing.T, method, path, token, key string, input any, status int, code operations.Code) []byte {
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
func (f *relationTransport) client(t *testing.T, serverCtx context.Context) *mcp.ClientSession {
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
			doc, err := contractcoverage.Decode(schema)
			if err != nil {
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
func (f *relationTransport) invoke(t *testing.T, cs *mcp.ClientSession, name, key string, input map[string]any) []byte {
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
func (f *relationTransport) sql(t *testing.T, q string, args ...any) {
	t.Helper()
	if _, e := f.pool.Exec(f.ctx, q, args...); e != nil {
		t.Fatal(e)
	}
}
func (f *relationTransport) invokeRelation(t *testing.T, cs *mcp.ClientSession, kind string, source, target uuid.UUID, remove bool) relation.MutationOutput {
	t.Helper()
	op, input := f.input(t, kind, source, target, remove)
	for _, c := range f.registry.Catalog() {
		if c.Metadata.ID == op {
			return transportDecode[relation.MutationOutput](t, f.invoke(t, cs, c.Metadata.MCPName, uuid.NewString(), issueMap(t, input)))
		}
	}
	t.Fatal("missing relation operation")
	return relation.MutationOutput{}
}
func TestRelationsTransportLifecycleCanonicalReciprocalAndExactJSON(t *testing.T) {
	f := newRelationTransport(t)
	cs := f.client(t, f.ctx)
	tools, e := cs.ListTools(f.ctx, nil)
	if e != nil {
		t.Fatal(e)
	}
	discovered := map[string]*mcp.Tool{}
	for _, v := range tools.Tools {
		discovered[v.Name] = v
	}
	count := 0
	for _, c := range f.registry.Catalog() {
		if strings.HasPrefix(c.Metadata.ID, "issue_relations.") {
			count++
			v := discovered[c.Metadata.MCPName]
			if v == nil || v.InputSchema == nil || v.OutputSchema == nil || v.Meta["traccia/error_schema"] == nil || v.Meta["traccia/operation_id"] != c.Metadata.ID {
				t.Fatal("incomplete relation tool discovery")
			}
		}
	}
	if count != 8 || len(f.registry.Catalog()) != 129 {
		t.Fatal("incomplete production catalog")
	}
	a, b := f.createTeam(t, "AAA"), f.createTeam(t, "BBB")
	text := "İstanbul 🧭 e\u0301 漢字 <plate>\"&</plate>"
	i := transportDecode[issue.Output](t, f.invoke(t, cs, "create_issue", "unicode-issue", issueMap(t, issue.CreateInput{TeamID: a.ID, Title: text, Priority: "none", StatusID: f.status(t, a.ID), LabelIDs: []string{}}))).Issue
	j, k := f.create(t, a.ID), f.create(t, a.ID)
	first := f.invokeRelation(t, cs, "parent", i.ID, j.ID, false)
	if first.State.Issue.Title != text {
		t.Fatal("Unicode relation reference changed")
	}
	replaced := f.invokeRelation(t, cs, "parent", i.ID, k.ID, false)
	if len(replaced.AffectedIssues) != 3 || len(replaced.ChangedRelations) != 2 || replaced.ChangedRelations[0].ID != first.ChangedRelations[0].ID || !replaced.ChangedRelations[0].Removed || replaced.State.Parent.ID != k.ID {
		t.Fatal("parent replacement fidelity")
	}
	f.invokeRelation(t, cs, "parent", i.ID, uuid.Nil, false)
	f.invokeRelation(t, cs, "duplicate", i.ID, j.ID, false)
	f.invokeRelation(t, cs, "duplicate", j.ID, k.ID, false)
	list := transportDecode[relation.List](t, f.invoke(t, cs, "list_issue_relations", "", issueMap(t, relation.PageInput{IssueID: i.ID, Direction: "all", Limit: 100})))
	if list.State.DuplicateOf.ID != j.ID || list.State.CanonicalDuplicate.ID != k.ID {
		t.Fatal("derived duplicate chain")
	}
	f.invokeRelation(t, cs, "duplicate", j.ID, uuid.Nil, false)
	list = transportDecode[relation.List](t, f.invoke(t, cs, "list_issue_relations", "", issueMap(t, relation.PageInput{IssueID: i.ID, Direction: "all", Limit: 100})))
	if list.State.CanonicalDuplicate.ID != j.ID || list.State.DuplicateOf.ID != j.ID {
		t.Fatal("duplicate predecessor was rewritten")
	}
	block := f.invokeRelation(t, cs, "blocks", i.ID, j.ID, false)
	inverse := transportDecode[relation.List](t, f.invoke(t, cs, "list_issue_relations", "", issueMap(t, relation.PageInput{IssueID: j.ID, Kind: "blocks", Direction: "incoming", Limit: 100})))
	if len(inverse.Items) != 1 || inverse.Items[0].ID != block.ChangedRelations[0].ID || inverse.Items[0].Source.ID != i.ID {
		t.Fatal("reciprocal blocks read")
	}
	f.invokeRelation(t, cs, "blocks", i.ID, j.ID, true)
	related := f.invokeRelation(t, cs, "related", i.ID, j.ID, false)
	removed := f.invokeRelation(t, cs, "related", j.ID, i.ID, true)
	if removed.ChangedRelations[0].ID != related.ChangedRelations[0].ID || !removed.ChangedRelations[0].Removed {
		t.Fatal("inverse related unlink identity")
	}
	relink := f.invokeRelation(t, cs, "related", j.ID, i.ID, false)
	if relink.ChangedRelations[0].ID == related.ChangedRelations[0].ID {
		t.Fatal("tombstone resurrected")
	}
	raw, e := f.call("issues.get", "", f.w, issue.IssueInput{IssueID: i.ID})
	if decodeResult[issue.Output](t, raw, e).Issue.Version != 1 {
		t.Fatal("relation CAS changed metadata version")
	}
	raw, e = f.call("issues.move", "move", f.w, issue.MoveInput{IssueID: i.ID, TeamID: b.ID, StatusID: f.status(t, b.ID), LabelIDs: []string{}, ExpectedVersion: 1})
	moved := decodeResult[issue.Output](t, raw, e).Issue
	raw, e = f.call("issues.archive", "archive", f.w, issue.VersionInput{IssueID: i.ID, ExpectedVersion: moved.Version})
	archived := decodeResult[issue.Output](t, raw, e).Issue
	state := f.state(t, i.ID)
	if !state.Issue.Archived || state.Issue.Identifier != moved.Identifier || state.Capabilities.Link || !state.Capabilities.Unlink || state.DuplicateOf.ID != j.ID {
		t.Fatal("archived stable reference/capabilities")
	}
	f.invokeRelation(t, cs, "duplicate", i.ID, uuid.Nil, false)
	f.invokeRelation(t, cs, "related", j.ID, i.ID, true)
	raw, e = f.call("issues.restore", "restore", f.w, issue.VersionInput{IssueID: i.ID, ExpectedVersion: archived.Version})
	_ = decodeResult[issue.Output](t, raw, e)
	history := transportDecode[relation.HistoryList](t, f.invoke(t, cs, "history_issue_relations", "", issueMap(t, relation.HistoryInput{IssueID: i.ID, Limit: 100})))
	if len(history.Items) < 10 || history.HasMore {
		t.Fatal("incomplete relation history")
	}
	for n, h := range history.Items {
		if h.ActorID != f.actor || h.WorkspaceID != f.w || h.IssueID != i.ID || h.Version != int64(n+2) || h.Snapshot.State.Issue.RelationVersion != h.Version {
			t.Fatal("canonical relation history context/version")
		}
	}
	if history.Items[0].Snapshot.State.Issue.Identifier != i.Identifier || history.Items[0].Snapshot.State.Parent.ID != j.ID {
		t.Fatal("historical reference snapshot mutated")
	}
	// The separate relation revision must preserve integers above JavaScript's exact range in input, output and history.
	f.sql(t, "UPDATE issue_relation_revisions SET version=9007199254740993 WHERE issue_id IN (SELECT id FROM issues WHERE uuid=$1 OR uuid=$2)", i.ID, k.ID)
	// k already participated in a parent replacement; both revision rows therefore exist.
	in := relation.LinkInput{IssueID: i.ID, TargetIssueID: k.ID, ExpectedVersions: []relation.ExpectedVersion{{IssueID: i.ID, ExpectedVersion: 9007199254740993}, {IssueID: k.ID, ExpectedVersion: 9007199254740993}}}
	body := issueMap(t, in)
	exact := f.invoke(t, cs, "link_issue_block", "bigint", body)
	output := transportDecode[relation.MutationOutput](t, exact)
	if output.State.Issue.RelationVersion != 9007199254740994 || !bytes.Contains(exact, []byte(`"relation_version":9007199254740994`)) {
		t.Fatal("BIGINT relation revision rounded")
	}
	if !bytes.Equal(exact, f.invoke(t, cs, "link_issue_block", "bigint", body)) {
		t.Fatal("exact immutable receipt replay changed")
	}
	changed := issueMap(t, in)
	changed["target_issue_id"] = j.ID.String()
	transportTool(t, f.ctx, cs, "link_issue_block", f.w, "bigint", changed, operations.IdempotencyConflict)
	path := "/v1/workspaces/" + f.w.String() + "/issues/" + i.ID.String() + "/relations/blocks"
	transportTool(t, f.ctx, cs, "link_issue_block", f.w, "stale-bigint", in, operations.VersionConflict)
	staleBody := issueMap(t, in)
	delete(staleBody, "issue_id")
	f.http(t, "POST", path, f.bearer, "stale-bigint-http", staleBody, 409, operations.VersionConflict)
	delete(changed, "issue_id")
	f.http(t, "POST", path, f.bearer, "bigint", changed, 409, operations.IdempotencyConflict)
	raw = f.invoke(t, cs, "history_issue_relations", "", issueMap(t, relation.HistoryInput{IssueID: i.ID, Limit: 100}))
	if !bytes.Contains(raw, []byte(`"version":9007199254740994`)) {
		t.Fatal("history BIGINT revision rounded")
	}
}
func TestRelationsTransportStrictBoundariesAndGraphErrors(t *testing.T) {
	f := newRelationTransport(t)
	cs := f.client(t, f.ctx)
	a := f.createTeam(t, "AAA")
	i, j, k := f.create(t, a.ID), f.create(t, a.ID), f.create(t, a.ID)
	path := "/v1/workspaces/" + f.w.String() + "/issues/" + i.ID.String() + "/relations/blocks"
	input := relation.LinkInput{IssueID: i.ID, TargetIssueID: j.ID, ExpectedVersions: f.revisions(t, i.ID, j.ID)}
	full := issueMap(t, input)
	delete(full, "issue_id")
	for _, name := range []string{"target_issue_id", "expected_versions"} {
		t.Run("omitted-"+name, func(t *testing.T) {
			body := issueMap(t, full)
			delete(body, name)
			f.http(t, "POST", path, f.bearer, uuid.NewString(), body, 400, operations.Validation)
			sdk := issueMap(t, body)
			sdk["issue_id"] = i.ID.String()
			transportTool(t, f.ctx, cs, "link_issue_block", f.w, uuid.NewString(), sdk, operations.Validation)
		})
	}
	for _, name := range []string{"actor_id", "workspace_id", "issue_id", "metadata_version", "unknown"} {
		t.Run("unexpected-"+name, func(t *testing.T) {
			body := issueMap(t, full)
			body[name] = uuid.NewString()
			f.http(t, "POST", path, f.bearer, uuid.NewString(), body, 400, operations.Validation)
		})
	}
	for n, raw := range []string{`{"target_issue_id":"a","target_issue_id":"b"}`, `[]`, `null`, `{"target_issue_id":`, string(transportJSON(t, full)) + ` {}`} {
		t.Run(fmt.Sprintf("json-%d", n), func(t *testing.T) {
			f.http(t, "POST", path, f.bearer, uuid.NewString(), json.RawMessage(raw), 400, operations.Validation)
		})
	}
	versions := []struct {
		name  string
		value any
	}{{"missing-target", []relation.ExpectedVersion{{IssueID: i.ID, ExpectedVersion: 1}}}, {"extra", f.revisions(t, i.ID, j.ID, k.ID)}, {"duplicate", []relation.ExpectedVersion{{IssueID: i.ID, ExpectedVersion: 1}, {IssueID: i.ID, ExpectedVersion: 1}}}, {"zero", []relation.ExpectedVersion{{IssueID: i.ID, ExpectedVersion: 0}, {IssueID: j.ID, ExpectedVersion: 1}}}}
	for _, v := range versions {
		t.Run("versions-"+v.name, func(t *testing.T) {
			body := issueMap(t, full)
			body["expected_versions"] = v.value
			f.http(t, "POST", path, f.bearer, uuid.NewString(), body, 400, operations.Validation)
			body["issue_id"] = i.ID.String()
			transportTool(t, f.ctx, cs, "link_issue_block", f.w, uuid.NewString(), body, operations.Validation)
		})
	}
	base := "/v1/workspaces/" + f.w.String() + "/issues/" + i.ID.String() + "/relations"
	for _, query := range []string{"direction=all&unknown=1", "direction=all&limit=1&limit=2", "direction=all&limit=101", "direction=bad", "direction=all&removed=yes", "direction=all&kind=bad", "direction=all&after=bad"} {
		t.Run("query-"+url.QueryEscape(query), func(t *testing.T) { f.http(t, "GET", base+"?"+query, f.bearer, "", nil, 400, operations.Validation) })
	}
	for _, id := range []string{"AAA-1", uuid.Nil.String(), "not-uuid"} {
		t.Run("invalid-uuid-"+id, func(t *testing.T) {
			f.http(t, "GET", "/v1/workspaces/"+f.w.String()+"/issues/"+id+"/relations?direction=all", f.bearer, "", nil, 400, operations.Validation)
		})
	}
	f.http(t, "GET", base+"?direction=all", "", "", nil, 401, operations.Code("UNAUTHENTICATED"))
	for _, kind := range []string{"parent", "blocks", "duplicate"} {
		t.Run("cycle-"+kind, func(t *testing.T) {
			f.invokeRelation(t, cs, kind, i.ID, j.ID, false)
			op, in := f.input(t, kind, j.ID, i.ID, false)
			for _, c := range f.registry.Catalog() {
				if c.Metadata.ID == op {
					transportTool(t, f.ctx, cs, c.Metadata.MCPName, f.w, uuid.NewString(), in, operations.Conflict)
					body := issueMap(t, in)
					delete(body, "issue_id")
					p := strings.ReplaceAll(strings.ReplaceAll(c.Metadata.HTTPPath, "{workspaceID}", f.w.String()), "{issueID}", j.ID.String())
					f.http(t, "POST", p, f.bearer, uuid.NewString(), body, 409, operations.Conflict)
				}
			}
		})
	}
	for _, name := range []string{"actor_id", "channel", "permission"} {
		t.Run("sdk-wrapper-"+name, func(t *testing.T) {
			args := map[string]any{"workspace_id": f.w.String(), "input": relation.PageInput{IssueID: i.ID, Direction: "all", Limit: 100}, name: f.actor.String()}
			r, e := cs.CallTool(f.ctx, &mcp.CallToolParams{Name: "list_issue_relations", Arguments: json.RawMessage(transportJSON(t, args))})
			if e == nil && (r == nil || !r.IsError) {
				t.Fatal("SDK actor spoof accepted")
			}
		})
	}
	raw, e := f.call("workspaces.create", "foreign", uuid.Nil, ws.CreateInput{Name: "Foreign"})
	foreign := decodeResult[ws.WorkspaceOutput](t, raw, e).Workspace.ID
	for _, id := range []uuid.UUID{i.ID, uuid.New()} {
		t.Run("foreign-"+id.String(), func(t *testing.T) {
			transportTool(t, f.ctx, cs, "list_issue_relations", foreign, "", relation.PageInput{IssueID: id, Direction: "all", Limit: 100}, operations.NotFound)
			f.http(t, "GET", "/v1/workspaces/"+foreign.String()+"/issues/"+id.String()+"/relations?direction=all", f.bearer, "", nil, 404, operations.NotFound)
		})
	}
}
func TestRelationsTransportCredentialRevalidationAndArchivedEscape(t *testing.T) {
	f := newRelationTransport(t)
	human := f.client(t, f.ctx)
	a := f.createTeam(t, "AAA")
	i, j := f.create(t, a.ID), f.create(t, a.ID)
	mint := func(audience string, scopes []string) ws.CreateCredentialOutput {
		return transportDecode[ws.CreateCredentialOutput](t, transportTool(t, f.ctx, human, "create_workspace_credential", f.w, uuid.NewString(), ws.CreateCredentialInput{Name: "Relations acceptance", Audience: audience, Scopes: scopes, ExpiresAt: time.Now().Add(time.Hour).UTC().Truncate(time.Second)}, ""))
	}
	api := mint("api", []string{"workspace.read", "workspace.write"})
	pat := mint("mcp", []string{"workspace.read", "workspace.write"})
	read := mint("mcp", []string{"workspace.read"})
	writer := f.client(t, ws.WithToolCredential(context.Background(), pat.Token))
	reader := f.client(t, ws.WithToolCredential(context.Background(), read.Token))
	wrong := f.client(t, ws.WithToolCredential(context.Background(), api.Token))
	in := relation.PageInput{IssueID: i.ID, Direction: "all", Limit: 100}
	transportTool(t, f.ctx, wrong, "list_issue_relations", f.w, "", in, operations.Forbidden)
	state := transportDecode[relation.List](t, transportTool(t, f.ctx, reader, "list_issue_relations", f.w, "", in, "")).State
	if state.Capabilities.Link || state.Capabilities.Unlink {
		t.Fatal("read-only credential advertises mutation")
	}
	path := "/v1/workspaces/" + f.w.String() + "/issues/" + i.ID.String() + "/relations?direction=all"
	f.http(t, "GET", path, pat.Token, "", nil, 403, operations.Forbidden)
	f.http(t, "GET", path, api.Token, "", nil, 200, "")
	link := relation.LinkInput{IssueID: i.ID, TargetIssueID: j.ID, ExpectedVersions: f.revisions(t, i.ID, j.ID)}
	transportTool(t, f.ctx, reader, "link_issue_block", f.w, "denied", link, operations.Forbidden)
	exact := transportTool(t, f.ctx, writer, "link_issue_block", f.w, "receipt", link, "")
	f.sql(t, "UPDATE workspace_tool_credentials SET created_at=clock_timestamp()-interval '2 hours',expires_at=clock_timestamp()-interval '1 second' WHERE uuid=$1", pat.Credential.ID)
	transportTool(t, f.ctx, writer, "link_issue_block", f.w, "receipt", link, operations.Unauthenticated)
	f.sql(t, "UPDATE workspace_tool_credentials SET expires_at=clock_timestamp()+interval '1 hour' WHERE uuid=$1", pat.Credential.ID)
	if !bytes.Equal(exact, transportTool(t, f.ctx, writer, "link_issue_block", f.w, "receipt", link, "")) {
		t.Fatal("credential receipt replay changed")
	}
	raw, e := f.call("issues.archive", "archive", f.w, issue.VersionInput{IssueID: i.ID, ExpectedVersion: 1})
	_ = decodeResult[issue.Output](t, raw, e)
	fresh := link
	fresh.ExpectedVersions = f.revisions(t, i.ID, j.ID)
	transportTool(t, f.ctx, writer, "link_issue_block", f.w, "inactive", fresh, operations.Conflict)
	remove := fresh
	transportTool(t, f.ctx, writer, "unlink_issue_block", f.w, "escape", remove, "")
	transportTool(t, f.ctx, human, "revoke_workspace_credential", f.w, "revoke", ws.RevokeInput{CredentialID: pat.Credential.ID}, "")
	transportTool(t, f.ctx, writer, "link_issue_block", f.w, "receipt", link, operations.Unauthenticated)
	f.sql(t, "UPDATE users SET status='disabled' WHERE uuid=$1", f.actor)
	f.http(t, "GET", path, api.Token, "", nil, 401, operations.Unauthenticated)
	transportTool(t, f.ctx, reader, "list_issue_relations", f.w, "", in, operations.Unauthenticated)
}
func TestRelationsTransportCompletePagesAndImmutableAnchors(t *testing.T) {
	f := newRelationTransport(t)
	cs := f.client(t, f.ctx)
	a, b := f.createTeam(t, "AAA"), f.createTeam(t, "BBB")
	source := f.create(t, a.ID)
	for n := 0; n < 105; n++ {
		target := f.create(t, a.ID)
		f.invokeRelation(t, cs, "blocks", source.ID, target.ID, false)
	}
	input := relation.PageInput{IssueID: source.ID, Kind: "blocks", Direction: "outgoing", Limit: 100}
	first := transportDecode[relation.List](t, f.invoke(t, cs, "list_issue_relations", "", issueMap(t, input)))
	if len(first.Items) != 100 || !first.HasMore || first.NextCursor == "" {
		t.Fatal("first relation page incomplete")
	}
	anchor := first.Items[99]
	f.invokeRelation(t, cs, "blocks", source.ID, anchor.Target.ID, true)
	raw, e := f.call("issues.move", "move-anchor", f.w, issue.MoveInput{IssueID: anchor.Target.ID, TeamID: b.ID, StatusID: f.status(t, b.ID), LabelIDs: []string{}, ExpectedVersion: 1})
	moved := decodeResult[issue.Output](t, raw, e).Issue
	raw, e = f.call("issues.archive", "archive-anchor", f.w, issue.VersionInput{IssueID: moved.ID, ExpectedVersion: moved.Version})
	_ = decodeResult[issue.Output](t, raw, e)
	input.After = first.NextCursor
	tail := transportDecode[relation.List](t, f.invoke(t, cs, "list_issue_relations", "", issueMap(t, input)))
	if len(tail.Items) != 5 || tail.HasMore || tail.NextCursor != "" {
		t.Fatal("removed moved archived anchor lost relation tail")
	}
	seen := map[uuid.UUID]bool{}
	for _, page := range [][]relation.Relation{first.Items, tail.Items} {
		for _, row := range page {
			if seen[row.ID] {
				t.Fatal("duplicate relation page row")
			}
			seen[row.ID] = true
		}
	}
	if len(seen) != 105 {
		t.Fatal("incomplete relation traversal")
	}
	input.After = ""
	input.Removed = true
	removed := transportDecode[relation.List](t, f.invoke(t, cs, "list_issue_relations", "", issueMap(t, input)))
	if len(removed.Items) != 1 || removed.Items[0].ID != anchor.ID || !removed.Items[0].Target.Archived || removed.Items[0].Target.Identifier != moved.Identifier {
		t.Fatal("removed edge lost stable target reference")
	}
	historyInput := relation.HistoryInput{IssueID: source.ID, Limit: 100}
	history := transportDecode[relation.HistoryList](t, f.invoke(t, cs, "history_issue_relations", "", issueMap(t, historyInput)))
	if len(history.Items) != 100 || !history.HasMore {
		t.Fatal("history first page incomplete")
	}
	historyInput.After = history.NextCursor
	historyTail := transportDecode[relation.HistoryList](t, f.invoke(t, cs, "history_issue_relations", "", issueMap(t, historyInput)))
	if len(historyTail.Items) != 6 || historyTail.HasMore || historyTail.NextCursor != "" {
		t.Fatal("history tail incomplete")
	}
	historyIDs := map[uuid.UUID]bool{}
	for n, row := range append(history.Items, historyTail.Items...) {
		if historyIDs[row.ID] || row.Version != int64(n+2) || row.Snapshot.State.Issue.RelationVersion != row.Version {
			t.Fatal("duplicate or noncanonical history")
		}
		historyIDs[row.ID] = true
	}
	for _, change := range []struct {
		name  string
		input relation.PageInput
	}{{"kind", relation.PageInput{IssueID: source.ID, Kind: "related", Direction: "outgoing", Limit: 100, After: first.NextCursor}}, {"direction", relation.PageInput{IssueID: source.ID, Kind: "blocks", Direction: "incoming", Limit: 100, After: first.NextCursor}}, {"removed", relation.PageInput{IssueID: source.ID, Kind: "blocks", Direction: "outgoing", Removed: true, Limit: 100, After: first.NextCursor}}, {"issue", relation.PageInput{IssueID: anchor.Target.ID, Kind: "blocks", Direction: "outgoing", Limit: 100, After: first.NextCursor}}} {
		t.Run("cursor-scope-"+change.name, func(t *testing.T) {
			transportTool(t, f.ctx, cs, "list_issue_relations", f.w, "", change.input, operations.Validation)
			q := url.Values{}
			fields := issueMap(t, change.input)
			delete(fields, "issue_id")
			for k, v := range fields {
				q.Set(k, fmt.Sprint(v))
			}
			f.http(t, "GET", "/v1/workspaces/"+f.w.String()+"/issues/"+change.input.IssueID.String()+"/relations?"+q.Encode(), f.bearer, "", nil, 400, operations.Validation)
		})
	}
	transportTool(t, f.ctx, cs, "history_issue_relations", f.w, "", relation.HistoryInput{IssueID: source.ID, Limit: 100, After: first.NextCursor}, operations.Validation)
}
