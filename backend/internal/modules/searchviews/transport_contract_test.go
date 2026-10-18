package searchviews_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/furkanmeclis/traccia/backend/internal/database/db"
	sr "github.com/furkanmeclis/traccia/backend/internal/modules/searchviews/repository"
	uc "github.com/furkanmeclis/traccia/backend/internal/modules/searchviews/usecase"
	module "github.com/furkanmeclis/traccia/backend/internal/modules/workspaces"
	ws "github.com/furkanmeclis/traccia/backend/internal/modules/workspaces/usecase"
	"github.com/furkanmeclis/traccia/backend/internal/platform/authctx"
	"github.com/furkanmeclis/traccia/backend/internal/platform/contractcoverage"
	"github.com/furkanmeclis/traccia/backend/internal/platform/jwt"
	"github.com/furkanmeclis/traccia/backend/internal/platform/mcpbindings"
	"github.com/furkanmeclis/traccia/backend/internal/platform/operations"
	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Tests mount the production router and official SDK; only the identity loader
// stands in for a verified login. No network OAuth flow is claimed.
type transportLoader struct{}

func (transportLoader) LoadPrincipal(_ *http.Request, c jwt.Claims) (authctx.Principal, error) {
	id, e := c.UserUUID()
	return authctx.Principal{UserID: id}, e
}

type searchTransport struct {
	searchFixture
	mux      *http.ServeMux
	bearer   string
	document contractcoverage.Object
}

var transportCatalog sync.Map

func newSearchTransport(t *testing.T) *searchTransport {
	t.Helper()
	f := newSearchFixture(t)
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
	return &searchTransport{searchFixture: f, mux: mux, bearer: bearer, document: doc}
}
func transportJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
func (f *searchTransport) http(t *testing.T, method, path, token, key string, input any, status int, code operations.Code) []byte {
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
	var matchedContract *operations.Contract
	score := -1
	for _, contract := range f.registry.Catalog() {
		if contract.Metadata.HTTPMethod != method || !matchContractPath(contract.Metadata.HTTPPath, req.URL.Path) {
			continue
		}
		specificity := 0
		for _, part := range strings.Split(contract.Metadata.HTTPPath, "/") {
			if !strings.HasPrefix(part, "{") {
				specificity += len(part)
			}
		}
		if specificity > score {
			candidate := contract
			matchedContract = &candidate
			score = specificity
		}
	}
	if matchedContract != nil {
		if err := contractcoverage.ValidateHTTP(f.document, matchedContract.Metadata.ID, res.Code, res.Body.Bytes()); err != nil {
			t.Fatalf("%s response violates documented contract: %v", matchedContract.Metadata.ID, err)
		}
	}

	// Do not include response bodies: credential results contain one-time secrets.
	if res.Code != status {
		var failed struct {
			Error json.RawMessage `json:"error"`
		}
		_ = json.Unmarshal(res.Body.Bytes(), &failed)
		t.Fatalf("%s %s status=%d want=%d error=%s", method, path, res.Code, status, failed.Error)
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
func (f *searchTransport) client(t *testing.T, serverCtx context.Context) *mcp.ClientSession {
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

func searchMap(t *testing.T, v any) map[string]any {
	t.Helper()
	d := json.NewDecoder(bytes.NewReader(transportJSON(t, v)))
	d.UseNumber()
	var out map[string]any
	if e := d.Decode(&out); e != nil {
		t.Fatal(e)
	}
	return out
}
func (f *searchTransport) route(t *testing.T, id string, in any) (operations.Contract, string, any) {
	t.Helper()
	for _, c := range f.registry.Catalog() {
		if c.Metadata.ID != id {
			continue
		}
		body := searchMap(t, in)
		path := strings.ReplaceAll(c.Metadata.HTTPPath, "{workspaceID}", f.w.String())
		for field, name := range operations.HTTPPathFields(c.Metadata) {
			path = strings.ReplaceAll(path, "{"+name+"}", fmt.Sprint(body[field]))
			delete(body, field)
		}
		if c.Metadata.HTTPMethod == "GET" {
			q := url.Values{}
			for k, v := range body {
				q.Set(k, fmt.Sprint(v))
			}
			return c, path + "?" + q.Encode(), nil
		}
		return c, path, body
	}
	t.Fatal("missing operation", id)
	return operations.Contract{}, "", nil
}
func (f *searchTransport) paired(t *testing.T, cs *mcp.ClientSession, id, key string, in any, status int, code operations.Code) []byte {
	t.Helper()
	c, path, body := f.route(t, id, in)
	raw := f.http(t, c.Metadata.HTTPMethod, path, f.bearer, key, body, status, code)
	sdk := transportTool(t, f.ctx, cs, c.Metadata.MCPName, f.w, key, in, code)
	if code == "" && !bytes.Equal(raw, sdk) {
		t.Fatalf("%s exact HTTP/SDK JSON differs", id)
	}
	return raw
}
func (f *searchTransport) op(t *testing.T, cs *mcp.ClientSession, id string, in any) []byte {
	t.Helper()
	c, _, _ := f.route(t, id, in)
	key := ""
	if c.Metadata.Effect == operations.Write {
		key = uuid.NewString()
	}
	status := 200
	if c.Metadata.HTTPMethod == "POST" {
		status = 201
	}
	return f.paired(t, cs, id, key, in, status, "")
}
func (f *searchTransport) reject(t *testing.T, cs *mcp.ClientSession, id string, in any, status int, code operations.Code) {
	t.Helper()
	c, _, _ := f.route(t, id, in)
	key := ""
	if c.Metadata.Effect == operations.Write {
		key = uuid.NewString()
	}
	f.paired(t, cs, id, key, in, status, code)
}
func (f *searchTransport) sql(t *testing.T, q string, args ...any) {
	t.Helper()
	if _, e := f.pool.Exec(f.ctx, q, args...); e != nil {
		t.Fatal(e)
	}
}
func (f *searchTransport) actorTransport(t *testing.T, role string) *searchTransport {
	t.Helper()
	other := otherActor(t, f.searchFixture, role)
	copy := *f
	copy.searchFixture = other
	copy.mount(t)
	return &copy
}
func (f *searchTransport) mount(t *testing.T) {
	t.Helper()
	tokens, e := jwt.NewManager("team-contract-signing-secret", time.Hour, time.Hour)
	if e != nil {
		t.Fatal(e)
	}
	f.bearer, _, e = tokens.IssueAccess(jwt.AccessInput{UserID: f.actor})
	if e != nil {
		t.Fatal(e)
	}
	f.mux = http.NewServeMux()
	if e = module.RegisterRegistryRoutes(f.mux, f.registry, tokens, transportLoader{}); e != nil {
		t.Fatal(e)
	}
}
func captureBody(q uc.Query) map[string]any {
	return map[string]any{"request_id": uuid.NewString(), "query": q, "ttl_seconds": nil, "limit": 100}
}
func viewBody(resource, name string) map[string]any {
	d := map[string]any{"resource": resource, "issue": nil, "project": nil, "team": nil}
	d[resource] = query()
	column := "name"
	if resource == "issue" {
		column = "title"
	}
	return map[string]any{"name": name, "definition": d, "columns": []uc.Column{{Key: column, Width: 240}}}
}
func TestSearchTransportInventoryAndPersonalLifecycle(t *testing.T) {
	f := newSearchTransport(t)
	cs := f.client(t, f.ctx)
	var n, writes, allWrites int
	for _, c := range f.registry.Catalog() {
		if c.Metadata.Effect == operations.Write {
			allWrites++
		}
		if strings.HasPrefix(c.Metadata.ID, "saved_views.") || strings.HasPrefix(c.Metadata.ID, "column_preferences.") || strings.HasSuffix(c.Metadata.ID, "_queries.create") || strings.HasSuffix(c.Metadata.ID, "_queries.get") || c.Metadata.ID == "search_queries.config" {
			n++
			if c.Metadata.Effect == operations.Write {
				writes++
			}
			if c.Metadata.Effect == operations.Read && c.Metadata.Idempotent {
				t.Fatal("read acquired receipt", c.Metadata.ID)
			}
		}
	}
	if len(f.registry.Catalog()) != 134 || n != 15 || writes != 5 || allWrites != 70 {
		t.Fatalf("inventory %d/%d/%d/%d", len(f.registry.Catalog()), n, writes, allWrites)
	}
	config := transportDecode[uc.ConfigOutput](t, f.op(t, cs, "search_queries.config", uc.ConfigInput{}))
	if !config.Capabilities.Manage || config.Limits.MaxPage != 100 || config.Limits.MaxNodes != 128 || config.Limits.MaxDepth != 8 || config.Limits.MaxInputBytes != 131072 || config.Limits.MaxRawInputBytes != 1<<20 || config.InputBytePolicy != "json_stringify_utf8_domain" || config.TextMatching != "unicode_default_casefold_literal_no_normalization" {
		t.Fatal("config drift")
	}
	team := f.createTeam(t, "WIR")
	f.create(t, team.ID)
	f.createProject(t, team.ID)
	for _, resource := range []string{"issue", "project", "team"} {
		t.Run(resource, func(t *testing.T) {
			first := searchMap(t, transportDecode[map[string]any](t, f.op(t, cs, resource+"_queries.create", captureBody(query()))))
			session := first["session"].(map[string]any)
			if session["total_count"] != "1" {
				t.Fatal(session)
			}
			again := f.op(t, cs, resource+"_queries.get", map[string]any{"session_id": session["id"], "limit": 100, "after": ""})
			if !bytes.Equal(transportJSON(t, first), again) {
				var normalized any
				_ = json.Unmarshal(again, &normalized)
				if !reflect.DeepEqual(first, searchMap(t, normalized)) {
					t.Fatal("snapshot changed")
				}
			}
			name := "  private " + resource + " İ 👩🏽‍💻 e\u0301\r\n %_\\  "
			body := viewBody(resource, name)
			key := uuid.NewString()
			view := transportDecode[uc.ViewOutput](t, f.paired(t, cs, "saved_views.create", key, body, 201, "")).View
			if view.Name != name || view.ActorID != f.actor || !view.Capabilities.Update {
				t.Fatal("view fidelity/caps")
			}
			f.op(t, cs, "saved_views.get", uc.ViewInput{ViewID: view.ID})
			listing := transportDecode[uc.ViewList](t, f.op(t, cs, "saved_views.list", uc.ViewListInput{Resource: resource, Limit: 100}))
			if len(listing.Items) != 1 || listing.Items[0].ID != view.ID {
				t.Fatal("private list")
			}
			update := viewBody(resource, name+" updated")
			update["view_id"] = view.ID
			update["expected_revision"] = view.Revision
			revised := transportDecode[uc.ViewOutput](t, f.op(t, cs, "saved_views.update", update)).View
			if revised.Revision == view.Revision || revised.Name != name+" updated" {
				t.Fatal("view CAS update")
			}
			f.reject(t, cs, "saved_views.update", update, 409, operations.VersionConflict)
			// Both inactive null slots are required. Neither a fabricated path-owned body
			// property nor a missing null may be accepted by the actual HTTP decoder.
			_, path, payload := f.route(t, "saved_views.update", update)
			bad := searchMap(t, payload)
			bad["view_id"] = view.ID
			f.http(t, "POST", path, f.bearer, uuid.NewString(), bad, 400, operations.Validation)
			missing := viewBody(resource, name)
			def := missing["definition"].(map[string]any)
			for _, r := range []string{"issue", "project", "team"} {
				if r != resource {
					delete(def, r)
					break
				}
			}
			f.reject(t, cs, "saved_views.create", missing, 400, operations.Validation)
			removed := transportDecode[uc.ViewOutput](t, f.op(t, cs, "saved_views.delete", uc.ViewVersionInput{ViewID: view.ID, ExpectedRevision: revised.Revision})).View
			if !removed.Deleted || removed.Capabilities.Delete || !removed.Capabilities.Restore {
				t.Fatal("delete caps")
			}
			restored := transportDecode[uc.ViewOutput](t, f.op(t, cs, "saved_views.restore", uc.ViewVersionInput{ViewID: view.ID, ExpectedRevision: removed.Revision})).View
			if restored.Deleted || restored.Name != revised.Name || restored.Revision == removed.Revision {
				t.Fatal("restore content")
			}
			empty := transportDecode[uc.ColumnPreferences](t, f.op(t, cs, "column_preferences.get", uc.ColumnsInput{Resource: resource}))
			if empty.Revision != nil || empty.Columns == nil {
				t.Fatal("first null CAS")
			}
			col := uc.ColumnsSetInput{Resource: resource, Columns: []uc.Column{{Key: "name", Width: 800}}}
			if resource == "issue" {
				col.Columns[0].Key = "title"
			}
			set := transportDecode[uc.ColumnPreferences](t, f.op(t, cs, "column_preferences.set", col))
			if set.Revision == nil {
				t.Fatal("missing revision")
			}
			f.reject(t, cs, "column_preferences.set", col, 409, operations.VersionConflict)
			col.ExpectedRevision = set.Revision
			col.Columns[0].Width = 80
			f.op(t, cs, "column_preferences.set", col)
		})
	}
	activity := f.op(t, cs, "activity.list", ws.ListInput{Limit: 100})
	for _, secret := range []string{"private issue", "private project", "private team", `"filter"`, `"columns"`, `"definition"`} {
		if bytes.Contains(activity, []byte(secret)) {
			t.Fatal("workspace activity leaked personal view data")
		}
	}
	var leaked int
	if e := f.pool.QueryRow(f.ctx, `SELECT count(*) FROM outbox_events WHERE event_name IN ('saved_views.created','saved_views.updated','saved_views.deleted','saved_views.restored','column_preferences.set') AND (payload::text LIKE '%private issue%' OR payload ? 'definition' OR payload ? 'columns' OR payload ? 'filter')`).Scan(&leaked); e != nil || leaked != 0 {
		t.Fatal("public outbox leak", leaked, e)
	}
	var snapshots int
	if e := f.pool.QueryRow(f.ctx, "SELECT count(*) FROM private_preference_history WHERE snapshot::text LIKE '%private%'").Scan(&snapshots); e != nil || snapshots != 12 {
		t.Fatal("owner snapshots lost", snapshots, e)
	}
	var reads int
	if e := f.pool.QueryRow(f.ctx, "SELECT count(*) FROM operation_receipts WHERE operation_id LIKE '%_queries.create'").Scan(&reads); e != nil || reads != 0 {
		t.Fatal("logical reads created business receipt", reads, e)
	}
}
func TestSearchTransportImmutableRowsAndPrivateViewCursors(t *testing.T) {
	f := newSearchTransport(t)
	cs := f.client(t, f.ctx)
	seedIssues(t, f.searchFixture, 207)
	q := query()
	q.Sort = []uc.Sort{{Field: "title", Direction: "asc", Nulls: "last"}, {Field: "id", Direction: "asc", Nulls: "last"}}
	first := transportDecode[uc.IssuePage](t, f.op(t, cs, "issue_queries.create", captureBody(q)))
	if first.Session.TotalCount != "207" || len(first.Items) != 100 || !first.HasMore {
		t.Fatal("manifest count")
	}
	f.sql(t, "UPDATE issues SET title='changed after capture',archived=true,version=version+1 WHERE workspace_id=(SELECT id FROM workspaces WHERE uuid=$1)", f.w)
	seen := map[uuid.UUID]bool{}
	page := first
	previous := ""
	pages := 0
	for {
		pages++
		for _, row := range page.Items {
			if seen[row.ID] || row.Archived || row.Title == "changed after capture" || row.Title < previous || row.Version != "1" {
				t.Fatal("live hydration, duplicate or order drift")
			}
			seen[row.ID] = true
			previous = row.Title
		}
		if !page.HasMore {
			break
		}
		page = transportDecode[uc.IssuePage](t, f.op(t, cs, "issue_queries.get", uc.PageInput{SessionID: first.Session.ID, After: page.NextCursor, Limit: 100}))
		if page.Session != first.Session || !reflect.DeepEqual(page.Query, first.Query) {
			t.Fatal("capture metadata drift")
		}
	}
	if len(seen) != 207 || pages != 3 {
		t.Fatal("truncated manifest", len(seen), pages)
	}
	reread := transportDecode[uc.IssuePage](t, f.op(t, cs, "issue_queries.get", uc.PageInput{SessionID: first.Session.ID, Limit: 100}))
	if !reflect.DeepEqual(reread, first) {
		t.Fatal("first immutable page changed")
	}
	bad := first.NextCursor + "="
	f.reject(t, cs, "issue_queries.get", uc.PageInput{SessionID: first.Session.ID, After: bad, Limit: 100}, 400, operations.Validation)
	fresh := transportDecode[uc.IssuePage](t, f.op(t, cs, "issue_queries.create", captureBody(query())))
	if fresh.Session.TotalCount != "0" {
		t.Fatal("new capture ignored active scope")
	}
	f.reject(t, cs, "issue_queries.get", uc.PageInput{SessionID: fresh.Session.ID, After: first.NextCursor, Limit: 100}, 400, operations.Validation)
	// Exercise real service writes to seed personal view pagination; each remains
	// private while reads traverse actual HTTP and SDK without a capped scan.
	var views []uc.SavedView
	for n := 0; n < 107; n++ {
		raw, e := f.call("saved_views.create", uuid.NewString(), f.w, viewBody("issue", fmt.Sprintf("hidden-view-%03d", n)))
		views = append(views, decodeResult[uc.ViewOutput](t, raw, e).View)
	}
	list := transportDecode[uc.ViewList](t, f.op(t, cs, "saved_views.list", uc.ViewListInput{Resource: "issue", Limit: 100}))
	if len(list.Items) != 100 || !list.HasMore {
		t.Fatal("view first page")
	}
	anchor := list.Items[99]
	f.op(t, cs, "saved_views.delete", uc.ViewVersionInput{ViewID: anchor.ID, ExpectedRevision: anchor.Revision})
	rest := transportDecode[uc.ViewList](t, f.op(t, cs, "saved_views.list", uc.ViewListInput{Resource: "issue", Limit: 100, After: list.NextCursor}))
	if len(rest.Items) != 7 || rest.HasMore {
		t.Fatal("removed anchor broke cursor")
	}
	f.reject(t, cs, "saved_views.list", uc.ViewListInput{Resource: "project", Limit: 100, After: list.NextCursor}, 400, operations.Validation)
	other := f.actorTransport(t, "owner")
	otherCS := other.client(t, other.ctx)
	other.reject(t, otherCS, "saved_views.list", uc.ViewListInput{Resource: "issue", Limit: 100, After: list.NextCursor}, 400, operations.Validation)
	other.reject(t, otherCS, "saved_views.get", uc.ViewInput{ViewID: views[0].ID}, 404, operations.NotFound)
	private := transportDecode[uc.ViewList](t, other.op(t, otherCS, "saved_views.list", uc.ViewListInput{Resource: "any", Limit: 100}))
	if len(private.Items) != 0 {
		t.Fatal("other owner sees private views")
	}
	other.reject(t, otherCS, "issue_queries.get", uc.PageInput{SessionID: first.Session.ID, Limit: 100}, 404, operations.NotFound)
}
func TestSearchTransportReadOnlyAuthorityAndRetryIdentity(t *testing.T) {
	f := newSearchTransport(t)
	f.createTeam(t, "RDO")
	owner := f.client(t, f.ctx)
	member := f.actorTransport(t, "member")
	cs := member.client(t, member.ctx)
	request := captureBody(query())
	first := member.paired(t, cs, "team_queries.create", "", request, 201, "")
	config := transportDecode[uc.ConfigOutput](t, member.op(t, cs, "search_queries.config", uc.ConfigInput{}))
	if config.Capabilities.Manage {
		t.Fatal("member write bypass")
	}
	member.reject(t, cs, "saved_views.create", viewBody("team", "blocked"), 403, operations.Forbidden)
	member.reject(t, cs, "column_preferences.set", uc.ColumnsSetInput{Resource: "team", Columns: []uc.Column{{Key: "name", Width: 80}}}, 403, operations.Forbidden)
	// A distinct service resolving a new configured default must replay the exact
	// original request, while an explicit TTL change is a different request.
	configured := *member
	ops, e := uc.New(sr.New(f.pool, db.New(f.pool)), f.workspace).WithDefaultTTL(3600).Operations()
	if e != nil {
		t.Fatal(e)
	}
	configured.registry, e = f.workspace.Registry(ops...)
	if e != nil {
		t.Fatal(e)
	}
	configured.mount(t)
	configuredCS := configured.client(t, member.ctx)
	again := configured.paired(t, configuredCS, "team_queries.create", "", request, 201, "")
	if !bytes.Equal(first, again) {
		t.Fatal("default config changed request replay")
	}
	changed := searchMap(t, request)
	changed["ttl_seconds"] = 3600
	configured.reject(t, configuredCS, "team_queries.create", changed, 409, operations.IdempotencyConflict)
	var receipts int
	if e = f.pool.QueryRow(f.ctx, "SELECT count(*) FROM operation_receipts WHERE operation_id IN ('team_queries.create','saved_views.create','column_preferences.set')").Scan(&receipts); e != nil || receipts != 0 {
		t.Fatal("read/rejected write receipts", receipts, e)
	}
	// Real read-only MCP credential can publish/read a capture. Revocation is
	// checked again for every page and repeated capture, not frozen in a session.
	credential := transportDecode[ws.CreateCredentialOutput](t, transportTool(t, f.ctx, owner, "create_workspace_credential", f.w, uuid.NewString(), ws.CreateCredentialInput{Name: "Query read", Audience: "mcp", Scopes: []string{"workspace.read"}, ExpiresAt: time.Now().UTC().Add(time.Hour).Truncate(time.Second)}, ""))
	read := f.client(t, ws.WithToolCredential(context.Background(), credential.Token))
	page := transportDecode[uc.TeamPage](t, transportTool(t, f.ctx, read, "create_team_query", f.w, "", captureBody(query()), ""))
	if page.Session.TotalCount != "1" {
		t.Fatal("read credential capture")
	}
	transportTool(t, f.ctx, read, "create_saved_view", f.w, uuid.NewString(), viewBody("team", "forbidden"), operations.Forbidden)
	_, e = f.call("credentials.revoke", uuid.NewString(), f.w, ws.RevokeInput{CredentialID: credential.Credential.ID})
	if e != nil {
		t.Fatal(e)
	}
	transportTool(t, f.ctx, read, "get_team_query", f.w, "", uc.PageInput{SessionID: page.Session.ID, Limit: 100}, operations.Unauthenticated)
	// Membership revocation also fences live HTTP pages for an existing capture.
	f.sql(t, "UPDATE workspace_members SET active=false WHERE workspace_id=(SELECT id FROM workspaces WHERE uuid=$1) AND user_id=(SELECT id FROM users WHERE uuid=$2)", f.w, member.actor)
	p := transportDecode[uc.TeamPage](t, first)
	member.reject(t, cs, "team_queries.get", uc.PageInput{SessionID: p.Session.ID, Limit: 100}, 403, operations.Forbidden)
}
func TestSearchTransportClosedASTBudgetsAndNoPartialPublication(t *testing.T) {
	f := newSearchTransport(t)
	cs := f.client(t, f.ctx)
	f.createTeam(t, "BAD")
	baseline := func() (int, int, int) {
		var sessions, views, receipts int
		if e := f.pool.QueryRow(f.ctx, "SELECT (SELECT count(*) FROM search_query_sessions),(SELECT count(*) FROM saved_views),(SELECT count(*) FROM operation_receipts)").Scan(&sessions, &views, &receipts); e != nil {
			t.Fatal(e)
		}
		return sessions, views, receipts
	}
	a, b, c := baseline()
	cases := []struct {
		name  string
		alter func(map[string]any)
	}{
		{"unknown_root", func(in map[string]any) { in["unknown"] = true }},
		{"unsupported_version", func(in map[string]any) { q := searchMap(t, in["query"]); q["version"] = 2; in["query"] = q }},
		{"foreign_resource_field", func(in map[string]any) {
			q := searchMap(t, in["query"])
			q["filter"] = map[string]any{"kind": "text", "field": "summary", "op": "eq", "value": "x"}
			in["query"] = q
		}},
		{"unknown_branch_member", func(in map[string]any) {
			q := searchMap(t, in["query"])
			q["filter"] = map[string]any{"kind": "text", "field": "title", "op": "eq", "value": "x", "number": 0}
			in["query"] = q
		}},
		{"empty_group", func(in map[string]any) {
			q := searchMap(t, in["query"])
			q["filter"] = map[string]any{"kind": "all", "children": []any{}}
			in["query"] = q
		}},
		{"depth_nine", func(in map[string]any) {
			q := searchMap(t, in["query"])
			var leaf any = map[string]any{"kind": "text", "field": "title", "op": "eq", "value": "x"}
			for range 8 {
				leaf = map[string]any{"kind": "not", "child": leaf}
			}
			q["filter"] = leaf
			in["query"] = q
		}},
		{"nodes_129", func(in map[string]any) {
			q := searchMap(t, in["query"])
			nodes := []any{}
			for range 128 {
				nodes = append(nodes, map[string]any{"kind": "text", "field": "title", "op": "eq", "value": "x"})
			}
			q["filter"] = map[string]any{"kind": "all", "children": nodes}
			in["query"] = q
		}},

		{"missing_kind_depth", func(in map[string]any) {
			q := searchMap(t, in["query"])
			var leaf any = map[string]any{"field": "title", "op": "eq", "value": "x"}
			for range 8 {
				leaf = map[string]any{"child": leaf}
			}
			q["filter"] = leaf
			in["query"] = q
		}},
		{"missing_kind_nodes", func(in map[string]any) {
			q := searchMap(t, in["query"])
			nodes := []any{}
			for range 128 {
				nodes = append(nodes, map[string]any{"field": "title", "op": "eq", "value": "x"})
			}
			q["filter"] = map[string]any{"children": nodes}
			in["query"] = q
		}},
		{"text_257_unicode", func(in map[string]any) {
			q := searchMap(t, in["query"])
			q["search"] = strings.Repeat("🧭", 257)
			in["query"] = q
		}},
		{"duplicate_set_identity", func(in map[string]any) {
			q := searchMap(t, in["query"])
			id := uuid.NewString()
			q["filter"] = map[string]any{"kind": "uuid", "field": "id", "op": "in", "ids": []string{id, strings.ToUpper(id)}}
			in["query"] = q
		}},
		{"year_zero", func(in map[string]any) {
			q := searchMap(t, in["query"])
			q["filter"] = map[string]any{"kind": "date", "field": "start_date", "op": "eq", "value": "0000-01-01"}
			in["query"] = q
		}},
		{"null_boolean_opaque", func(in map[string]any) { q := searchMap(t, in["query"]); q["filter"] = true; in["query"] = q }},
		{"nul_search", func(in map[string]any) {
			q := searchMap(t, in["query"])
			q["search"] = "before\x00after"
			in["query"] = q
		}},
		{"limit_101", func(in map[string]any) { in["limit"] = 101 }},
		{"ttl_too_small", func(in map[string]any) { in["ttl_seconds"] = 59 }},
		{"byte_budget", func(in map[string]any) {
			q := searchMap(t, in["query"])
			nodes := []any{}
			for range 127 {
				nodes = append(nodes, map[string]any{"kind": "text", "field": "title", "op": "eq", "value": strings.Repeat("🧭", 256)})
			}
			q["filter"] = map[string]any{"kind": "all", "children": nodes}
			in["query"] = q
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := captureBody(query())
			tc.alter(in)
			f.reject(t, cs, "issue_queries.create", in, 400, operations.Validation)
			aa, bb, cc := baseline()
			if aa != a || bb != b || cc != c {
				t.Fatal("invalid input published state/receipt")
			}
		})
	}
	// Raw budget is transport representation bytes, not trimmed canonical JSON.
	// This valid tiny input padded above the advertised byte limit is rejected
	// before schema validation; normal and escaped text remain exact otherwise.
	raw := append(transportJSON(t, captureBody(query())), bytes.Repeat([]byte(" "), 1<<20)...)
	f.http(t, "POST", "/v1/workspaces/"+f.w.String()+"/search/issues/sessions", f.bearer, "", json.RawMessage(raw), 400, operations.Validation)
	aa, bb, cc := baseline()
	if aa != a || bb != b || cc != c {
		t.Fatal("raw limit mutated state")
	}
	// The exact maximum AST depth/node count is accepted, with literal escaped
	// Unicode/backslash text unchanged in the immutable returned query.
	q := query()
	q.Search = "\\u2028 %_\\ İ e\u0301\r\n"
	leaf := &uc.Filter{Kind: "text", Field: "title", Op: "eq", Value: "literal <>& \u2028\u2029 \\u2028"}
	for range 7 {
		leaf = &uc.Filter{Kind: "not", Child: leaf}
	}
	q.Filter = leaf
	page := transportDecode[uc.IssuePage](t, f.op(t, cs, "issue_queries.create", captureBody(q)))
	if !reflect.DeepEqual(uc.Query(page.Query), q) {
		t.Fatal("query text/AST rewritten")
	}
	t.Run("nodes_128_accepted", func(t *testing.T) {
		q := query()
		nodes := make([]uc.Filter, 127)
		for i := range nodes {
			nodes[i] = uc.Filter{Kind: "text", Field: "title", Op: "eq", Value: "literal <>&"}
		}
		q.Filter = &uc.Filter{Kind: "all", Children: nodes}
		page := transportDecode[uc.IssuePage](t, f.op(t, cs, "issue_queries.create", captureBody(q)))
		if !reflect.DeepEqual(uc.Query(page.Query), q) {
			t.Fatal("max-node query rewritten")
		}
	})

}
func TestSearchTransportLiteralUnicodeExpiryAndDecimalMetadata(t *testing.T) {
	f := newSearchTransport(t)
	cs := f.client(t, f.ctx)
	seedIssues(t, f.searchFixture, 11)
	titles := []string{"I", "i", "İ", "i\u0307", "ı", "Straße", "STRASSE", "é", "e\u0301", "%_\\", "line"}
	for n, title := range titles {
		f.sql(t, "UPDATE issues SET title=$2 WHERE workspace_id=(SELECT id FROM workspaces WHERE uuid=$1) AND number=$3", f.w, title, n+1)
	}
	for _, tc := range []struct{ name, value, count string }{{"ascii", "I", "2"}, {"dotted", "İ", "2"}, {"dotless", "ı", "1"}, {"sharp_s", "STRASSE", "2"}, {"composed", "É", "1"}, {"decomposed", "e\u0301", "1"}, {"wildcards_literal", "%_\\", "1"}, {"CRLF_literal", "line\r\n", "0"}} {
		t.Run(tc.name, func(t *testing.T) {
			q := query()
			q.Filter = &uc.Filter{Kind: "text", Field: "title", Op: "eq", Value: tc.value}
			p := transportDecode[uc.IssuePage](t, f.op(t, cs, "issue_queries.create", captureBody(q)))
			if p.Session.TotalCount != tc.count || p.Query.Filter.Value != tc.value {
				t.Fatal("folding/count/text mismatch", p.Session.TotalCount)
			}
		})
	}
	f.sql(t, "UPDATE issues SET version=9007199254740993 WHERE workspace_id=(SELECT id FROM workspaces WHERE uuid=$1) AND number=1", f.w)
	q := query()
	q.Filter = &uc.Filter{Kind: "text", Field: "title", Op: "eq", Value: "I"}
	large := transportDecode[uc.IssuePage](t, f.op(t, cs, "issue_queries.create", captureBody(q)))
	found := false
	for _, row := range large.Items {
		if row.Version == "9007199254740993" {
			found = true
		}
	}
	if !found {
		t.Fatal("int64 metadata rounded")
	}
	// Workspace scope belongs to both URL and authorization, never only a
	// globally unique session identifier from a different workspace.
	raw, e := f.call("workspaces.create", uuid.NewString(), uuid.Nil, ws.CreateInput{Name: "Other query tenant"})
	otherW := decodeResult[ws.WorkspaceOutput](t, raw, e).Workspace.ID
	other := *f
	other.w = otherW
	other.reject(t, cs, "issue_queries.get", uc.PageInput{SessionID: large.Session.ID, Limit: 100}, 404, operations.NotFound)
	empty := transportDecode[uc.IssuePage](t, other.op(t, cs, "issue_queries.create", captureBody(query())))
	if empty.Session.TotalCount != "0" || empty.Session.WorkspaceID != otherW {
		t.Fatal("cross workspace source leak")
	}
	expired := seedExpiredManifest(t, f.searchFixture, 11)
	f.reject(t, cs, "issue_queries.get", uc.PageInput{SessionID: expired, Limit: 100}, 409, operations.Conflict)
	cleanup := sr.New(f.pool, db.New(f.pool))
	n, e := cleanup.Cleanup(f.ctx, 1000)
	if e != nil || n != 11 {
		t.Fatal("expiry cleanup", n, e)
	}
	var request uuid.UUID
	var state string
	var cleared bool
	if e = f.pool.QueryRow(f.ctx, "SELECT request_uuid,state,query IS NULL FROM search_query_sessions WHERE uuid=$1", expired).Scan(&request, &state, &cleared); e != nil || state != "expired" || !cleared {
		t.Fatal("expired identity compaction", state, cleared, e)
	}
	in := captureBody(query())
	in["request_id"] = request
	f.reject(t, cs, "issue_queries.create", in, 409, operations.Conflict)
	f.reject(t, cs, "issue_queries.get", uc.PageInput{SessionID: expired, Limit: 100}, 409, operations.Conflict)
	var headers int
	if e = f.pool.QueryRow(f.ctx, "SELECT count(*) FROM search_query_sessions WHERE uuid=$1", expired).Scan(&headers); e != nil || headers != 1 {
		t.Fatal("expired identity rebound/lost", headers, e)
	}
}

// boundaryInput fills legal text leaves until the Go-encoded complete domain
// input is within five bytes of the advertised limit. Each appended '<' costs
// six encoded bytes but one unescaped HTTP byte; paths remain part of domain input.
func legacyEncodedBoundaryInput(t *testing.T, makeInput func(uc.Query) map[string]any) (map[string]any, map[string]any) {
	t.Helper()
	q := query()
	nodes := make([]uc.Filter, 127)
	for i := range nodes {
		nodes[i] = uc.Filter{Kind: "text", Field: "title", Op: "eq", Value: "x"}
	}
	q.Filter = &uc.Filter{Kind: "all", Children: nodes}
	base := len(transportJSON(t, makeInput(q)))
	room := (131072 - base) / 6
	if room < 1 || room > 127*254 {
		t.Fatal("invalid boundary fixture")
	}
	for i := range q.Filter.Children {
		n := min(room, 254)
		q.Filter.Children[i].Value += strings.Repeat("<", n)
		room -= n
	}
	in := makeInput(q)
	raw := transportJSON(t, in)
	if len(raw) > 131072 || len(raw) < 131067 {
		t.Fatalf("boundary bytes %d", len(raw))
	}
	overQ := q
	copyNodes := append([]uc.Filter(nil), q.Filter.Children...)
	overQ.Filter = &uc.Filter{Kind: "all", Children: copyNodes}
	for i := range copyNodes {
		if len(copyNodes[i].Value) < 256 {
			copyNodes[i].Value += "<"
			break
		}
	}
	over := makeInput(overQ)
	if len(transportJSON(t, over)) <= 131072 {
		t.Fatal("over-limit fixture")
	}
	return in, over
}
func TestSearchTransportEncodedDomainBoundaryAndPathParity(t *testing.T) {
	f := newSearchTransport(t)
	cs := f.client(t, f.ctx)
	// Fixed request identity keeps size stable during boundary construction.
	request := uuid.NewString()
	makeRead := func(q uc.Query) map[string]any {
		return map[string]any{"request_id": request, "query": q, "ttl_seconds": nil, "limit": 100}
	}
	in, over := canonicalBoundaryInput(t, makeRead)
	rawHTTP := func(in any) []byte {
		var b bytes.Buffer
		enc := json.NewEncoder(&b)
		enc.SetEscapeHTML(false)
		if e := enc.Encode(searchMap(t, in)); e != nil {
			t.Fatal(e)
		}
		return bytes.TrimSpace(b.Bytes())
	}
	raw := rawHTTP(in)
	if len(raw) >= len(transportJSON(t, in)) {
		t.Fatal("HTML encoding control ineffective")
	}
	t.Logf("read canonical=%d Go-encoded=%d HTTP-body=%d over canonical=%d", len(independentCanonicalJSON(t, in)), len(transportJSON(t, in)), len(raw), len(independentCanonicalJSON(t, over)))
	_, path, _ := f.route(t, "issue_queries.create", in)
	first := f.http(t, "POST", path, f.bearer, "", json.RawMessage(raw), 201, "")
	sdk := transportTool(t, f.ctx, cs, "create_issue_query", f.w, "", in, "")
	if !bytes.Equal(first, sdk) {
		t.Fatal("near-limit read channel parity")
	}
	if low := f.rawAdapter(t, "issue_queries.create", "", in, ""); !bytes.Equal(low, first) {
		t.Fatal("canonical native adapter read parity")
	}
	over["request_id"] = uuid.NewString()
	f.http(t, "POST", path, f.bearer, "", json.RawMessage(rawHTTP(over)), 400, operations.Validation)
	transportTool(t, f.ctx, cs, "create_issue_query", f.w, "", over, operations.Validation)
	f.rawAdapter(t, "issue_queries.create", "", over, operations.Validation)
	created := transportDecode[uc.ViewOutput](t, f.op(t, cs, "saved_views.create", viewBody("issue", "boundary"))).View
	makeUpdate := func(q uc.Query) map[string]any {
		body := viewBody("issue", "boundary")
		body["definition"].(map[string]any)["issue"] = q
		body["view_id"] = created.ID
		body["expected_revision"] = created.Revision
		return body
	}
	update, tooLarge := canonicalBoundaryInput(t, makeUpdate)
	_, updatePath, body := f.route(t, "saved_views.update", update)
	wire := rawHTTP(body)
	key := uuid.NewString()
	t.Logf("update canonical incl path=%d Go-encoded=%d HTTP-body excluding path=%d over canonical=%d", len(independentCanonicalJSON(t, update)), len(transportJSON(t, update)), len(wire), len(independentCanonicalJSON(t, tooLarge)))
	rawView := f.http(t, "POST", updatePath, f.bearer, key, json.RawMessage(wire), 201, "")
	sdkView := transportTool(t, f.ctx, cs, "update_saved_view", f.w, key, update, "")
	if !bytes.Equal(rawView, sdkView) {
		t.Fatal("path-bound domain byte parity")
	}
	if low := f.rawAdapter(t, "saved_views.update", key, update, ""); !bytes.Equal(low, rawView) {
		t.Fatal("canonical native adapter path-write parity")
	}
	fresh := transportDecode[uc.ViewOutput](t, rawView).View
	tooLarge["expected_revision"] = fresh.Revision
	_, _, badBody := f.route(t, "saved_views.update", tooLarge)
	f.http(t, "POST", updatePath, f.bearer, uuid.NewString(), json.RawMessage(rawHTTP(badBody)), 400, operations.Validation)
	transportTool(t, f.ctx, cs, "update_saved_view", f.w, uuid.NewString(), tooLarge, operations.Validation)
	f.rawAdapter(t, "saved_views.update", uuid.NewString(), tooLarge, operations.Validation)
	reread := transportDecode[uc.ViewOutput](t, f.op(t, cs, "saved_views.get", uc.ViewInput{ViewID: created.ID})).View
	if reread.Revision != fresh.Revision {
		t.Fatal("oversize update partially committed")
	}
	var count int
	if e := f.pool.QueryRow(f.ctx, "SELECT count(*) FROM search_query_sessions WHERE request_uuid=$1", over["request_id"]).Scan(&count); e != nil || count != 0 {
		t.Fatal("oversize read published", count, e)
	}
}
func TestSearchTransportUnescapedAdapterBudgetParity(t *testing.T) {
	f := newSearchTransport(t)
	request := uuid.NewString()
	_, over := legacyEncodedBoundaryInput(t, func(q uc.Query) map[string]any {
		return map[string]any{"request_id": request, "query": q, "ttl_seconds": nil, "limit": 100}
	})
	// Native external JSON need not share Go client's HTML escaping. Feed those
	// exact bytes to the two registry entry points used by the MCP binding.
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	if e := enc.Encode(searchMap(t, over)); e != nil {
		t.Fatal(e)
	}
	raw := bytes.TrimSpace(b.Bytes())
	if len(raw) >= 131072 || len(transportJSON(t, over)) <= 131072 {
		t.Fatal("ineffective external encoding control")
	}
	httpRaw := f.http(t, "POST", "/v1/workspaces/"+f.w.String()+"/search/issues/sessions", f.bearer, "", json.RawMessage(raw), 201, "")
	preflight := f.registry.ValidateWireInput("issue_queries.create", raw)
	sdkRaw, execute := f.registry.Execute(f.ctx, operations.Call{OperationID: "issue_queries.create", Channel: operations.MCP, WorkspaceID: f.w, Input: raw})
	t.Logf("actual unescaped adapter input=%d Go-encoded domain=%d preflight_error=%v execute_error=%v", len(raw), len(transportJSON(t, over)), preflight, execute)
	if preflight != nil || execute != nil || !bytes.Equal(httpRaw, sdkRaw) {
		t.Fatal("canonical small input must pass exactly across HTTP and unescaped MCP adapter", preflight, execute)
	}
}

// The expected byte count is independently written from decoded JSON values.
// No production measurement helper is used. Fixture numbers are exact integers;
// strings use JSON.stringify escapes, preserving HTML, U+2028/U+2029 and UTF-8.
func independentCanonicalJSON(t *testing.T, v any) []byte {
	t.Helper()
	var out strings.Builder
	var write func(any)
	stringValue := func(value string) {
		out.WriteByte('"')
		for _, r := range value {
			switch r {
			case '"', '\\':
				out.WriteByte('\\')
				out.WriteRune(r)
			case '\b':
				out.WriteString(`\b`)
			case '\f':
				out.WriteString(`\f`)
			case '\n':
				out.WriteString(`\n`)
			case '\r':
				out.WriteString(`\r`)
			case '\t':
				out.WriteString(`\t`)
			default:
				if r < 32 {
					fmt.Fprintf(&out, `\u%04x`, r)
				} else {
					out.WriteRune(r)
				}
			}
		}
		out.WriteByte('"')
	}
	write = func(value any) {
		switch x := value.(type) {
		case nil:
			out.WriteString("null")
		case bool:
			if x {
				out.WriteString("true")
			} else {
				out.WriteString("false")
			}
		case string:
			stringValue(x)
		case json.Number:
			n, e := x.Int64()
			if e != nil {
				t.Fatal("non-integer boundary fixture", e)
			}
			out.WriteString(strconv.FormatInt(n, 10))
		case []any:
			out.WriteByte('[')
			for i, child := range x {
				if i > 0 {
					out.WriteByte(',')
				}
				write(child)
			}
			out.WriteByte(']')
		case map[string]any:
			out.WriteByte('{')
			keys := make([]string, 0, len(x))
			for k := range x {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for i, k := range keys {
				if i > 0 {
					out.WriteByte(',')
				}
				stringValue(k)
				out.WriteByte(':')
				write(x[k])
			}
			out.WriteByte('}')
		default:
			t.Fatalf("unsupported expected JSON value %T", value)
		}
	}
	write(searchMap(t, v))
	raw := []byte(out.String())
	if !json.Valid(raw) {
		t.Fatal("invalid expected JSON")
	}
	return raw
}
func canonicalBoundaryInput(t *testing.T, makeInput func(uc.Query) map[string]any) (map[string]any, map[string]any) {
	t.Helper()
	q := query()
	q.Search = "literal \\u2028 \\u2029"
	nodes := make([]uc.Filter, 127)
	for i := range nodes {
		nodes[i] = uc.Filter{Kind: "text", Field: "title", Op: "eq", Value: "x<>&\u2028\u2029"}
	}
	q.Filter = &uc.Filter{Kind: "all", Children: nodes}
	base := len(independentCanonicalJSON(t, makeInput(q)))
	room := (131072 - base) / 4
	if room < 1 || room > 127*250 {
		t.Fatal("ineffective canonical fixture", base, room)
	}
	for i := range nodes {
		n := min(room, 250)
		nodes[i].Value += strings.Repeat("🧭", n)
		room -= n
	}
	in := makeInput(q)
	size := len(independentCanonicalJSON(t, in))
	if size > 131072 || size < 131069 {
		t.Fatal("canonical boundary", size)
	}
	copyNodes := append([]uc.Filter(nil), nodes...)
	overQ := q
	overQ.Filter = &uc.Filter{Kind: "all", Children: copyNodes}
	for i := range copyNodes {
		if len([]rune(copyNodes[i].Value)) < 256 {
			copyNodes[i].Value += "🧭"
			break
		}
	}
	over := makeInput(overQ)
	if len(independentCanonicalJSON(t, over)) <= 131072 {
		t.Fatal("ineffective canonical over-limit fixture")
	}
	return in, over
}
func (f *searchTransport) rawAdapter(t *testing.T, id, key string, in any, code operations.Code) []byte {
	t.Helper()
	raw := independentCanonicalJSON(t, in)
	preflight := f.registry.ValidateWireInput(id, raw)
	out, execute := f.registry.Execute(f.ctx, operations.Call{OperationID: id, Channel: operations.MCP, WorkspaceID: f.w, IdempotencyKey: key, Input: raw})
	if code == "" {
		if preflight != nil || execute != nil {
			t.Fatal("native raw adapter rejected valid canonical domain", preflight, execute)
		}
	} else {
		checkCode(t, preflight, code)
		checkCode(t, execute, code)
	}
	return out
}
