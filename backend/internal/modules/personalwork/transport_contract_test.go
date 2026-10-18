package personalwork_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/furkanmeclis/traccia/backend/internal/database/db"
	personalrepo "github.com/furkanmeclis/traccia/backend/internal/modules/personalwork/repository"
	pw "github.com/furkanmeclis/traccia/backend/internal/modules/personalwork/usecase"
	search "github.com/furkanmeclis/traccia/backend/internal/modules/searchviews/usecase"
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
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// Tests mount the production router and official SDK; only the identity loader
// stands in for a verified login. No network OAuth flow is claimed.
type transportLoader struct{}

func (transportLoader) LoadPrincipal(_ *http.Request, c jwt.Claims) (authctx.Principal, error) {
	id, e := c.UserUUID()
	return authctx.Principal{UserID: id}, e
}

type personalTransport struct {
	personalFixture
	mux      *http.ServeMux
	bearer   string
	document contractcoverage.Object
}

var transportCatalog sync.Map

func newPersonalTransport(t *testing.T) *personalTransport {
	t.Helper()
	f := newPersonalFixture(t)
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
	return &personalTransport{personalFixture: f, mux: mux, bearer: bearer, document: doc}
}
func transportJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
func (f *personalTransport) http(t *testing.T, method, path, token, key string, input any, status int, code operations.Code) []byte {
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
func (f *personalTransport) client(t *testing.T, serverCtx context.Context) *mcp.ClientSession {
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
func (f *personalTransport) route(t *testing.T, id string, in any) (operations.Contract, string, any) {
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
func (f *personalTransport) paired(t *testing.T, cs *mcp.ClientSession, id, key string, in any, status int, code operations.Code) []byte {
	t.Helper()
	c, path, body := f.route(t, id, in)
	raw := f.http(t, c.Metadata.HTTPMethod, path, f.bearer, key, body, status, code)
	sdk := transportTool(t, f.ctx, cs, c.Metadata.MCPName, f.w, key, in, code)
	if code == "" && !bytes.Equal(raw, sdk) {
		t.Fatalf("%s exact HTTP/SDK JSON differs", id)
	}
	return raw
}
func (f *personalTransport) op(t *testing.T, cs *mcp.ClientSession, id string, in any) []byte {
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
func (f *personalTransport) reject(t *testing.T, cs *mcp.ClientSession, id string, in any, status int, code operations.Code) {
	t.Helper()
	c, _, _ := f.route(t, id, in)
	key := ""
	if c.Metadata.Effect == operations.Write {
		key = uuid.NewString()
	}
	f.paired(t, cs, id, key, in, status, code)
}
func (f *personalTransport) sql(t *testing.T, q string, args ...any) {
	t.Helper()
	if _, e := f.pool.Exec(f.ctx, q, args...); e != nil {
		t.Fatal(e)
	}
}
func (f *personalTransport) actorTransport(t *testing.T, role string) *personalTransport {
	t.Helper()
	other := otherActor(t, f.personalFixture, role)
	copy := *f
	copy.personalFixture = other
	copy.mount(t)
	return &copy
}
func (f *personalTransport) mount(t *testing.T) {
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
func portfolioInput(ids ...uuid.UUID) pw.PortfolioCreateInput {
	return pw.PortfolioCreateInput{RequestID: uuid.New(), WorkspaceIDs: ids, Definition: query(), Limit: 100}
}
func TestPersonalTransportInventoryFavoritesAndPrivateHistory(t *testing.T) {
	f := newPersonalTransport(t)
	cs := f.client(t, f.ctx)
	t.Run("five_additive_bindings_and_explicit_limits", func(t *testing.T) {
		writes := 0
		for _, c := range f.registry.Catalog() {
			if c.Metadata.Effect == operations.Write {
				writes++
			}
		}
		if len(f.registry.Catalog()) != 134 || writes != 70 {
			t.Fatalf("inventory %d/%d", len(f.registry.Catalog()), writes)
		}
		cfg := transportDecode[pw.ConfigOutput](t, f.op(t, cs, "personal_work.config", pw.ConfigInput{}))
		if cfg.Limits.MaxWorkspaces != 32 || cfg.Limits.MaxInputBytes != 131072 || cfg.Limits.MaxRawInputBytes != 1048576 || cfg.InputBytePolicy != "json_stringify_utf8_domain" {
			t.Fatal("config limits")
		}
	})
	team := f.createTeam(t, "HTTP")
	issue := f.create(t, team.ID)
	project := f.createProject(t, team.ID)
	targets := []pw.Target{{Kind: "workspace", ID: f.w}, {Kind: "team", ID: team.ID}, {Kind: "project", ID: project.ID}, {Kind: "issue", ID: issue.ID}}
	for _, target := range targets {
		t.Run(target.Kind+"_immutable_identity_CAS_receipt", func(t *testing.T) {
			in := pw.FavoriteSetInput{Target: target, Active: true}
			key := uuid.NewString()
			raw := f.paired(t, cs, "favorites.set", key, in, 201, "")
			fav := transportDecode[pw.FavoriteSetOutput](t, raw).Favorite
			if fav.ActorID != f.actor || fav.WorkspaceID != f.w || fav.Target != target || !fav.Active || fav.Metadata.Label == "" {
				t.Fatal("favorite identity")
			}
			again := f.paired(t, cs, "favorites.set", key, in, 201, "")
			if !bytes.Equal(raw, again) {
				t.Fatal("receipt replay changed")
			}
			f.reject(t, cs, "favorites.set", in, 409, operations.VersionConflict)
			in.ExpectedRevision = &fav.Revision
			in.Active = false
			off := transportDecode[pw.FavoriteSetOutput](t, f.op(t, cs, "favorites.set", in)).Favorite
			if off.ID != fav.ID || off.Active || off.Revision == fav.Revision {
				t.Fatal("tombstone identity")
			}
			f.reject(t, cs, "favorites.set", in, 409, operations.VersionConflict)
			in.ExpectedRevision = &off.Revision
			in.Active = true
			live := transportDecode[pw.FavoriteSetOutput](t, f.op(t, cs, "favorites.set", in)).Favorite
			if live.ID != fav.ID || !live.Active {
				t.Fatal("reactivation changed identity")
			}
		})
	}
	t.Run("private_list_and_public_payload_redaction", func(t *testing.T) {
		list := transportDecode[pw.FavoriteListOutput](t, f.op(t, cs, "favorites.list", pw.FavoriteListInput{Limit: 100}))
		if len(list.Items) != 4 || !list.Capabilities.Manage {
			t.Fatal("list")
		}
		other := f.actorTransport(t, "owner")
		oc := other.client(t, other.ctx)
		empty := transportDecode[pw.FavoriteListOutput](t, other.op(t, oc, "favorites.list", pw.FavoriteListInput{Limit: 100}))
		if len(empty.Items) != 0 {
			t.Fatal("other actor favorites leaked")
		}
		rows, e := f.pool.Query(f.ctx, "SELECT h.snapshot,o.payload->'Payload' FROM personal_favorite_history h JOIN outbox_events o ON o.id=h.event_id")
		if e != nil {
			t.Fatal(e)
		}
		defer rows.Close()
		n := 0
		for rows.Next() {
			var snapshot, payload []byte
			if e = rows.Scan(&snapshot, &payload); e != nil {
				t.Fatal(e)
			}
			var private pw.Favorite
			var public map[string]any
			if e = json.Unmarshal(snapshot, &private); e != nil {
				t.Fatal(e)
			}
			if e = json.Unmarshal(payload, &public); e != nil {
				t.Fatal(e)
			}
			if private.ActorID != f.actor || private.Metadata.Label == "" || len(public) != 5 || public["name"] != "Private preference" || public["target"] != nil || public["active"] != nil || public["label"] != nil {
				t.Fatal("private/public history boundary")
			}
			n++
		}
		if e = rows.Err(); e != nil || n != 12 {
			t.Fatal("history count", n, e)
		}
	})
}
func TestPersonalTransportSelectedScopeSnapshotAndRevocation(t *testing.T) {
	f := newPersonalTransport(t)
	cs := f.client(t, f.ctx)
	anchorTeam := f.createTeam(t, "ANCHOR")
	f.create(t, anchorTeam.ID)
	other := secondWorkspace(t, f.personalFixture)
	selected := f.personalFixture
	selected.w = other
	team := selected.createTeam(t, "SELECTED")
	created := selected.create(t, team.ID)
	in := portfolioInput(other)
	page := transportDecode[pw.PortfolioOutput](t, f.op(t, cs, "personal_portfolios.create", in))
	t.Run("omitted_anchor_exact_selection_and_zero_scope", func(t *testing.T) {
		if page.Session.TotalCount != "1" || len(page.Session.Workspaces) != 1 || page.Session.Workspaces[0].ID != other || len(page.Items) != 1 || page.Items[0].Issue.ID != created.ID || page.Items[0].WorkspaceID != other || page.Session.AnchorWorkspaceID != f.w {
			t.Fatal("anchor silently included or scope omitted")
		}
		replay := transportDecode[pw.PortfolioOutput](t, f.op(t, cs, "personal_portfolios.create", in))
		if !reflect.DeepEqual(page, replay) {
			t.Fatal("read replay changed")
		}
		zero := secondWorkspace(t, f.personalFixture)
		v := portfolioInput(other, zero)
		p := transportDecode[pw.PortfolioOutput](t, f.op(t, cs, "personal_portfolios.create", v))
		if len(p.Session.Workspaces) != 2 || p.Session.TotalCount != "1" {
			t.Fatal("zero scope omitted")
		}
		var n int
		if e := f.pool.QueryRow(f.ctx, "SELECT count(*) FROM operation_receipts WHERE operation_id LIKE 'personal_portfolios.%'").Scan(&n); e != nil || n != 0 {
			t.Fatal("read business receipt", n, e)
		}
	})
	t.Run("selected_membership_revoked_whole_page_and_replay_no_payload", func(t *testing.T) {
		f.sql(t, "UPDATE workspace_members SET active=false WHERE workspace_id=(SELECT id FROM workspaces WHERE uuid=$1) AND user_id=(SELECT id FROM users WHERE uuid=$2)", other, f.actor)
		f.reject(t, cs, "personal_portfolios.get", pw.PortfolioGetInput{SessionID: page.Session.ID, Limit: 100}, 403, operations.Forbidden)
		f.reject(t, cs, "personal_portfolios.create", in, 403, operations.Forbidden)
		c, path, body := f.route(t, "personal_portfolios.get", pw.PortfolioGetInput{SessionID: page.Session.ID, Limit: 100})
		req := httptest.NewRequest(c.Metadata.HTTPMethod, path, nil)
		req.Header.Set("Authorization", "Bearer "+f.bearer)
		res := httptest.NewRecorder()
		f.mux.ServeHTTP(res, req)
		_ = body
		var v struct {
			Data  json.RawMessage `json:"data"`
			Error struct {
				Details []operations.FieldError `json:"details"`
			} `json:"error"`
		}
		if e := json.Unmarshal(res.Body.Bytes(), &v); e != nil {
			t.Fatal(e)
		}
		if len(v.Data) > 0 && string(v.Data) != "null" {
			t.Fatal("revocation leaked payload")
		}
		if len(v.Error.Details) != 1 || v.Error.Details[0].Code != "scope_changed" {
			t.Fatal("missing explicit scope_changed")
		}
	})
	t.Run("unknown_and_unauthorized_scope_generic_initial_denial", func(t *testing.T) {
		f.reject(t, cs, "personal_portfolios.create", portfolioInput(uuid.New()), 403, operations.Forbidden)
		f.reject(t, cs, "personal_portfolios.create", portfolioInput(other), 403, operations.Forbidden)
	})
}
func TestPersonalTransportMemberAndScopedCredential(t *testing.T) {
	f := newPersonalTransport(t)
	other := secondWorkspace(t, f.personalFixture)
	member := f.actorTransport(t, "member")
	cs := member.client(t, member.ctx)
	t.Run("member_read_post_no_key_write_denied", func(t *testing.T) {
		page := transportDecode[pw.PortfolioOutput](t, member.op(t, cs, "personal_portfolios.create", portfolioInput(f.w)))
		member.op(t, cs, "personal_portfolios.get", pw.PortfolioGetInput{SessionID: page.Session.ID, Limit: 100})
		cfg := transportDecode[pw.ConfigOutput](t, member.op(t, cs, "personal_work.config", pw.ConfigInput{}))
		if cfg.Capabilities.Manage {
			t.Fatal("member write cap")
		}
		member.reject(t, cs, "favorites.set", pw.FavoriteSetInput{Target: pw.Target{Kind: "workspace", ID: f.w}, Active: true}, 403, operations.Forbidden)
		member.reject(t, cs, "personal_portfolios.create", portfolioInput(other), 403, operations.Forbidden)
		owner := f.client(t, f.ctx)
		f.reject(t, owner, "personal_portfolios.get", pw.PortfolioGetInput{SessionID: page.Session.ID, Limit: 100}, 404, operations.NotFound)
	})
	t.Run("workspace_readonly_MCP_credential_cannot_aggregate_other_scopes", func(t *testing.T) {
		raw, e := f.call("credentials.create", uuid.NewString(), f.w, ws.CreateCredentialInput{Name: "Private transport readonly", Audience: "mcp", Scopes: []string{"workspace.read"}, ExpiresAt: time.Now().Add(time.Hour)})
		token := decodeResult[ws.CreateCredentialOutput](t, raw, e).Token
		ctx := ws.WithToolCredential(context.Background(), token)
		sdk := f.client(t, ctx)
		transportTool(t, f.ctx, sdk, "create_personal_portfolio", f.w, "", portfolioInput(f.w), "")
		transportTool(t, f.ctx, sdk, "create_personal_portfolio", f.w, "", portfolioInput(other), operations.Forbidden)
		transportTool(t, f.ctx, sdk, "set_personal_favorite", f.w, uuid.NewString(), pw.FavoriteSetInput{Target: pw.Target{Kind: "workspace", ID: f.w}, Active: true}, operations.Forbidden)
	})
}

func TestPersonalTransportTypedResourcesAndAssignedConjunction(t *testing.T) {
	f := newPersonalTransport(t)
	cs := f.client(t, f.ctx)
	team := f.createTeam(t, "RESOURCE")
	project := f.createProject(t, team.ID)
	mine := f.create(t, team.ID)
	foreign := f.create(t, team.ID)
	f.create(t, team.ID)
	other := otherActor(t, f.personalFixture, "member")
	f.sql(t, "UPDATE issues SET assignee_id=(SELECT id FROM users WHERE uuid=$2),version=version+1 WHERE uuid=$1", mine.ID, f.actor)
	f.sql(t, "UPDATE issues SET assignee_id=(SELECT id FROM users WHERE uuid=$2),version=version+1 WHERE uuid=$1", foreign.ID, other.actor)
	for _, resource := range []string{"issue", "project", "team"} {
		t.Run(resource+"_closed_typed_row", func(t *testing.T) {
			in := portfolioInput(f.w)
			base := search.Query{Version: 1, Archived: "any", Search: "", Sort: []search.Sort{}}
			switch resource {
			case "project":
				v := search.ProjectQuery(base)
				in.Definition = search.ViewQuery{Resource: resource, Project: &v}
			case "team":
				v := search.TeamQuery(base)
				in.Definition = search.ViewQuery{Resource: resource, Team: &v}
			}
			page := transportDecode[pw.PortfolioOutput](t, f.op(t, cs, "personal_portfolios.create", in))
			if page.Session.Resource != resource || page.Definition.Resource != resource {
				t.Fatal("resource mismatch")
			}
			for _, row := range page.Items {
				if row.WorkspaceID != f.w || row.Resource != resource {
					t.Fatal("row scope")
				}
				if (row.Issue != nil) != (resource == "issue") || (row.Project != nil) != (resource == "project") || (row.Team != nil) != (resource == "team") {
					t.Fatal("nonclosed row")
				}
			}
			if resource == "project" && (len(page.Items) != 1 || page.Items[0].Project.ID != project.ID) {
				t.Fatal("project omitted")
			}
			if resource == "team" && (len(page.Items) != 1 || page.Items[0].Team.ID != team.ID) {
				t.Fatal("team omitted")
			}
			if resource != "issue" {
				in.RequestID = uuid.New()
				in.AssignedToMe = true
				f.reject(t, cs, "personal_portfolios.create", in, 400, operations.Validation)
			}
		})
	}
	t.Run("server_actor_conjuncts_adversarial_ANY_NOT_filter", func(t *testing.T) {
		in := portfolioInput(f.w)
		in.AssignedToMe = true
		p := transportDecode[pw.PortfolioOutput](t, f.op(t, cs, "personal_portfolios.create", in))
		if len(p.Items) != 1 || p.Items[0].Issue.ID != mine.ID {
			t.Fatal("assigned actor not serverowned")
		}
		in.RequestID = uuid.New()
		in.Definition.Issue.Filter = &search.Filter{Kind: "any", Children: []search.Filter{{Kind: "uuid", Field: "assignee_id", Op: "in", IDs: []uuid.UUID{other.actor}}, {Kind: "not", Child: &search.Filter{Kind: "uuid", Field: "id", Op: "in", IDs: []uuid.UUID{mine.ID}}}}}
		p = transportDecode[pw.PortfolioOutput](t, f.op(t, cs, "personal_portfolios.create", in))
		if p.Session.TotalCount != "0" {
			t.Fatal("caller filter bypassed assigned actor")
		}
	})
}
func TestPersonalTransportFullCursorImmutableAndFavoriteAnchors(t *testing.T) {
	f := newPersonalTransport(t)
	cs := f.client(t, f.ctx)
	second := secondWorkspace(t, f.personalFixture)
	third := secondWorkspace(t, f.personalFixture)
	zero := secondWorkspace(t, f.personalFixture)
	seedIssues(t, f.personalFixture, 107)
	b := f.personalFixture
	b.w = second
	seedIssues(t, b, 53)
	b.w = third
	seedIssues(t, b, 51)
	in := portfolioInput(third, f.w, zero, second)
	first := transportDecode[pw.PortfolioOutput](t, f.op(t, cs, "personal_portfolios.create", in))
	if first.Session.TotalCount != "211" || len(first.Session.Workspaces) != 4 || len(first.Items) != 100 || !first.HasMore {
		t.Fatal("incomplete first manifest")
	}
	expected := map[uuid.UUID]string{f.w: "107", second: "53", third: "51", zero: "0"}
	for _, w := range first.Session.Workspaces {
		if expected[w.ID] != w.TotalCount {
			t.Fatal("wrong per-scope count")
		}
	}
	t.Run("all_pages_immutable_after_live_archive_and_count_mutation", func(t *testing.T) {
		f.sql(t, "UPDATE issues SET title='MUTATED',archived=true,version=version+1 WHERE workspace_id IN(SELECT id FROM workspaces WHERE uuid=ANY($1::uuid[]))", []uuid.UUID{f.w, second, third})
		page := first
		seen := map[uuid.UUID]bool{}
		for {
			for _, row := range page.Items {
				if row.Issue == nil || row.Issue.Title == "MUTATED" || row.Issue.Archived || seen[row.Issue.ID] {
					t.Fatal("snapshot mutated/duplicate")
				}
				seen[row.Issue.ID] = true
			}
			if !page.HasMore {
				break
			}
			page = transportDecode[pw.PortfolioOutput](t, f.op(t, cs, "personal_portfolios.get", pw.PortfolioGetInput{SessionID: first.Session.ID, Limit: 100, After: page.NextCursor}))
			if !reflect.DeepEqual(page.Session, first.Session) {
				t.Fatal("manifest changed")
			}
		}
		if len(seen) != 211 {
			t.Fatal("truncated snapshot", len(seen))
		}
		refresh := in
		refresh.RequestID = uuid.New()
		p := transportDecode[pw.PortfolioOutput](t, f.op(t, cs, "personal_portfolios.create", refresh))
		if p.Items[0].Issue.Title != "MUTATED" || !p.Items[0].Issue.Archived {
			t.Fatal("explicit refresh stale")
		}
	})
	t.Run("foreign_session_cursor_and_actor_rejected", func(t *testing.T) {
		v := portfolioInput(f.w)
		p := transportDecode[pw.PortfolioOutput](t, f.op(t, cs, "personal_portfolios.create", v))
		f.reject(t, cs, "personal_portfolios.get", pw.PortfolioGetInput{SessionID: p.Session.ID, Limit: 100, After: first.NextCursor}, 400, operations.Validation)
		other := f.actorTransport(t, "owner")
		oc := other.client(t, other.ctx)
		other.reject(t, oc, "personal_portfolios.get", pw.PortfolioGetInput{SessionID: first.Session.ID, Limit: 100}, 404, operations.NotFound)
	})
	t.Run("107_favorites_tombstoned_anchor_fullcursor", func(t *testing.T) {
		rows, e := f.pool.Query(f.ctx, "SELECT uuid FROM issues WHERE workspace_id=(SELECT id FROM workspaces WHERE uuid=$1) ORDER BY uuid", f.w)
		if e != nil {
			t.Fatal(e)
		}
		ids := []uuid.UUID{}
		for rows.Next() {
			var id uuid.UUID
			if e = rows.Scan(&id); e != nil {
				t.Fatal(e)
			}
			ids = append(ids, id)
		}
		rows.Close()
		if len(ids) != 107 {
			t.Fatal("seed count")
		}
		for _, id := range ids {
			f.op(t, cs, "favorites.set", pw.FavoriteSetInput{Target: pw.Target{Kind: "issue", ID: id}, Active: true})
		}
		p := transportDecode[pw.FavoriteListOutput](t, f.op(t, cs, "favorites.list", pw.FavoriteListInput{Limit: 100}))
		if len(p.Items) != 100 || !p.HasMore {
			t.Fatal("favorites cap")
		}
		anchor := p.Items[99]
		f.op(t, cs, "favorites.set", pw.FavoriteSetInput{Target: anchor.Target, Active: false, ExpectedRevision: &anchor.Revision})
		tail := transportDecode[pw.FavoriteListOutput](t, f.op(t, cs, "favorites.list", pw.FavoriteListInput{Limit: 100, After: p.NextCursor}))
		if len(tail.Items) != 7 || tail.HasMore {
			t.Fatal("removed anchor broke cursor")
		}
		f.reject(t, cs, "favorites.list", pw.FavoriteListInput{IncludeInactive: true, Limit: 100, After: p.NextCursor}, 400, operations.Validation)
	})
}

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
func canonicalBoundaryInput(t *testing.T, makeInput func(search.Query) map[string]any) (map[string]any, map[string]any) {
	t.Helper()
	q, _ := query().QueryValue()
	q.Search = "literal \\u2028 \\u2029"
	nodes := make([]search.Filter, 127)
	for i := range nodes {
		nodes[i] = search.Filter{Kind: "text", Field: "title", Op: "eq", Value: "x<>&\u2028\u2029"}
	}
	q.Filter = &search.Filter{Kind: "all", Children: nodes}
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
	copyNodes := append([]search.Filter(nil), nodes...)
	overQ := q
	overQ.Filter = &search.Filter{Kind: "all", Children: copyNodes}
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
func (f *personalTransport) rawAdapter(t *testing.T, id, key string, in any, code operations.Code) []byte {
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
func TestPersonalTransportClosedBudgetsAndNativeByteParity(t *testing.T) {
	f := newPersonalTransport(t)
	cs := f.client(t, f.ctx)
	snapshot := func() string {
		var s string
		if e := f.pool.QueryRow(f.ctx, "SELECT json_build_array((SELECT count(*) FROM personal_portfolio_sessions),(SELECT count(*) FROM personal_portfolio_items),(SELECT count(*) FROM personal_favorites),(SELECT count(*) FROM personal_favorite_history),(SELECT count(*) FROM operation_receipts),(SELECT count(*) FROM outbox_events))::text").Scan(&s); e != nil {
			t.Fatal(e)
		}
		return s
	}
	mutate := func(change func(map[string]any)) map[string]any {
		m := searchMap(t, portfolioInput(f.w))
		change(m)
		return m
	}
	cases := map[string]any{
		"unknown_actor_override": mutate(func(m map[string]any) { m["actor_id"] = uuid.NewString() }),
		"duplicate_scope":        mutate(func(m map[string]any) { m["workspace_ids"] = []string{f.w.String(), strings.ToUpper(f.w.String())} }),
		"33_scopes": mutate(func(m map[string]any) {
			ids := []string{}
			for range 33 {
				ids = append(ids, uuid.NewString())
			}
			m["workspace_ids"] = ids
		}),
		"missing_required_null":     mutate(func(m map[string]any) { delete(m["definition"].(map[string]any), "project") }),
		"wrong_correlated_resource": mutate(func(m map[string]any) { m["definition"].(map[string]any)["resource"] = "project" }),
		"bad_ttl":                   mutate(func(m map[string]any) { m["ttl_seconds"] = 59 }),
		"page_over100":              mutate(func(m map[string]any) { m["limit"] = 101 }),
	}
	for name, in := range cases {
		t.Run(name, func(t *testing.T) {
			before := snapshot()
			f.reject(t, cs, "personal_portfolios.create", in, 400, operations.Validation)
			if snapshot() != before {
				t.Fatal("rejected input mutated state")
			}
		})
	}
	t.Run("depth_and_nodes_budget_before_recursive_schema", func(t *testing.T) {
		base := map[string]any{"kind": "null", "field": "assignee_id", "op": "is_null"}
		deep := any(base)
		for range 8 {
			deep = map[string]any{"kind": "not", "child": deep}
		}
		many := []any{}
		for range 128 {
			many = append(many, base)
		}
		for _, filter := range []any{deep, map[string]any{"kind": "all", "children": many}} {
			in := mutate(func(m map[string]any) { m["definition"].(map[string]any)["issue"].(map[string]any)["filter"] = filter })
			before := snapshot()
			f.reject(t, cs, "personal_portfolios.create", in, 400, operations.Validation)
			if snapshot() != before {
				t.Fatal("AST overflow published")
			}
		}
	})
	t.Run("canonical128KiB_HTML_Unicode_and_unescaped_adapter_parity", func(t *testing.T) {
		request := uuid.NewString()
		makeInput := func(q search.Query) map[string]any {
			v := portfolioInput(f.w)
			v.RequestID = uuid.MustParse(request)
			typed := search.IssueQuery(q)
			v.Definition = search.ViewQuery{Resource: "issue", Issue: &typed}
			return searchMap(t, v)
		}
		good, bad := canonicalBoundaryInput(t, makeInput)
		raw := independentCanonicalJSON(t, good)
		if len(raw) > 131072 || len(transportJSON(t, good)) <= 131072 {
			t.Fatal("encoding negative control")
		}
		c, path, _ := f.route(t, "personal_portfolios.create", good)
		a := f.http(t, c.Metadata.HTTPMethod, path, f.bearer, "", json.RawMessage(raw), 201, "")
		b := transportTool(t, f.ctx, cs, c.Metadata.MCPName, f.w, "", good, "")
		native := f.rawAdapter(t, "personal_portfolios.create", "", good, "")
		if !bytes.Equal(a, b) || !bytes.Equal(a, native) {
			t.Fatal("native/SDK/HTTP byte parity")
		}
		t.Logf("canonical accepted=%d rejected=%d GoEncoded=%d", len(raw), len(independentCanonicalJSON(t, bad)), len(transportJSON(t, good)))
		bad["request_id"] = uuid.NewString()
		before := snapshot()
		f.http(t, "POST", path, f.bearer, "", json.RawMessage(independentCanonicalJSON(t, bad)), 400, operations.Validation)
		transportTool(t, f.ctx, cs, c.Metadata.MCPName, f.w, "", bad, operations.Validation)
		f.rawAdapter(t, "personal_portfolios.create", "", bad, operations.Validation)
		if snapshot() != before {
			t.Fatal("canonical overflow published")
		}
	})
	t.Run("raw1MiB_is_separate_from_canonical_budget", func(t *testing.T) {
		in := portfolioInput(f.w)
		raw := append(transportJSON(t, in), bytes.Repeat([]byte(" "), 1<<20)...)
		_, path, _ := f.route(t, "personal_portfolios.create", in)
		before := snapshot()
		f.http(t, "POST", path, f.bearer, "", json.RawMessage(raw), 400, operations.Validation)
		checkCode(t, f.registry.ValidateWireInput("personal_portfolios.create", raw), operations.Validation)
		if snapshot() != before {
			t.Fatal("raw overflow published")
		}
	})
}

func TestPersonalTransportCurrentAuthorizationAfterLockWait(t *testing.T) {
	f := newPersonalTransport(t)
	cs := f.client(t, f.ctx)
	other := secondWorkspace(t, f.personalFixture)
	for _, channel := range []string{"HTTP", "SDK"} {
		t.Run(channel+"_capture_rechecks_after_advisory_wait", func(t *testing.T) {
			f.sql(t, "UPDATE workspace_members SET active=true WHERE workspace_id=(SELECT id FROM workspaces WHERE uuid=$1) AND user_id=(SELECT id FROM users WHERE uuid=$2)", other, f.actor)
			in := portfolioInput(other)
			lock, e := f.pool.Begin(f.ctx)
			if e != nil {
				t.Fatal(e)
			}
			defer func() { _ = lock.Rollback(f.ctx) }()
			if e = db.New(lock).LockPersonalRequest(f.ctx, "portfolio"+f.w.String()+f.actor.String()+in.RequestID.String()); e != nil {
				t.Fatal(e)
			}
			type response struct {
				status int
				raw    []byte
				err    error
			}
			done := make(chan response, 1)
			if channel == "HTTP" {
				_, path, body := f.route(t, "personal_portfolios.create", in)
				wire := transportJSON(t, body)
				go func() {
					req := httptest.NewRequest("POST", path, bytes.NewReader(wire))
					req.Header.Set("Authorization", "Bearer "+f.bearer)
					req.Header.Set("Content-Type", "application/json")
					rec := httptest.NewRecorder()
					f.mux.ServeHTTP(rec, req)
					done <- response{status: rec.Code, raw: rec.Body.Bytes()}
				}()
			} else {
				args := transportJSON(t, map[string]any{"workspace_id": f.w, "input": in})
				go func() {
					r, e := cs.CallTool(f.ctx, &mcp.CallToolParams{Name: "create_personal_portfolio", Arguments: json.RawMessage(args)})
					out := response{err: e}
					if e == nil && len(r.Content) == 1 {
						if c, ok := r.Content[0].(*mcp.TextContent); ok {
							out.raw = []byte(c.Text)
							if r.IsError {
								out.status = 403
							}
						}
					}
					done <- out
				}()
			}
			waitPersonalLock(t, f.personalFixture)
			f.sql(t, "UPDATE workspace_members SET active=false WHERE workspace_id=(SELECT id FROM workspaces WHERE uuid=$1) AND user_id=(SELECT id FROM users WHERE uuid=$2)", other, f.actor)
			if e = lock.Commit(f.ctx); e != nil {
				t.Fatal(e)
			}
			select {
			case out := <-done:
				if out.err != nil || out.status != 403 {
					t.Fatal("wait bypass", out.status, out.err)
				}
				var failure struct {
					Code  operations.Code `json:"code"`
					Data  json.RawMessage `json:"data"`
					Error struct {
						Code operations.Code `json:"code"`
					} `json:"error"`
				}
				if e = json.Unmarshal(out.raw, &failure); e != nil {
					t.Fatal(e)
				}
				if channel == "HTTP" {
					failure.Code = failure.Error.Code
				}
				if failure.Code != operations.Forbidden || (len(failure.Data) > 0 && string(failure.Data) != "null") {
					t.Fatal("authorization leaked")
				}
			case <-time.After(5 * time.Second):
				t.Fatal("wait did not finish")
			}
			var n int
			if e = f.pool.QueryRow(f.ctx, "SELECT count(*) FROM personal_portfolio_sessions WHERE request_uuid=$1", in.RequestID).Scan(&n); e != nil || n != 0 {
				t.Fatal("revoked capture published", n, e)
			}
		})
	}
}
func TestPersonalTransportExpiryCleanupAndTombstone(t *testing.T) {
	f := newPersonalTransport(t)
	cs := f.client(t, f.ctx)
	seedIssues(t, f.personalFixture, 103)
	in := portfolioInput(f.w)
	r := personalrepo.New(f.pool, db.New(f.pool))
	auth := func(ctx context.Context, ids []uuid.UUID, _ bool) error {
		for _, id := range ids {
			_, e := f.workspace.Authorize(ctx, operations.AuthorizationRequest{Operation: operations.Metadata{ID: "personal_portfolios.read", Effect: operations.Read, Permission: "workspace.read"}, WorkspaceID: id, Channel: operations.API})
			if e != nil {
				return e
			}
		}
		return nil
	}
	// The test repository uses a one-second expiry solely to advance the clock.
	// Public transport still enforces advertised minimum60 seconds elsewhere.
	p, e := r.Capture(f.ctx, f.w, f.actor, pw.Capture{Input: in, EffectiveTTL: 1}, auth)
	if e != nil {
		t.Fatal(e)
	}
	f.op(t, cs, "personal_portfolios.get", pw.PortfolioGetInput{SessionID: p.Session.ID, Limit: 100})
	time.Sleep(1100 * time.Millisecond)
	t.Run("HTTP_and_SDK_explicit_expired_no_rebind", func(t *testing.T) {
		f.reject(t, cs, "personal_portfolios.get", pw.PortfolioGetInput{SessionID: p.Session.ID, Limit: 100}, 409, operations.Conflict)
		f.reject(t, cs, "personal_portfolios.create", in, 409, operations.Conflict)
	})
	t.Run("bounded_actual_item_drain_and_permanent_request_tombstone", func(t *testing.T) {
		removed := int64(0)
		for range 8 {
			n, e := r.Cleanup(f.ctx, 17)
			if e != nil || n > 17 {
				t.Fatal("cleanup bound", n, e)
			}
			removed += n
			if n == 0 {
				break
			}
		}
		if removed != 103 {
			t.Fatal("cleanup truncation", removed)
		}
		var state string
		var compact bool
		var count int64
		if e = f.pool.QueryRow(f.ctx, "SELECT state,query IS NULL,total_count FROM personal_portfolio_sessions WHERE uuid=$1", p.Session.ID).Scan(&state, &compact, &count); e != nil || state != "expired" || !compact || count != 103 {
			t.Fatal("tombstone lost", state, compact, count, e)
		}
		f.reject(t, cs, "personal_portfolios.create", in, 409, operations.Conflict)
		fresh := in
		fresh.RequestID = uuid.New()
		newPage := transportDecode[pw.PortfolioOutput](t, f.op(t, cs, "personal_portfolios.create", fresh))
		if newPage.Session.ID == p.Session.ID || newPage.Session.TotalCount != "103" {
			t.Fatal("explicit refresh identity")
		}
	})
}
