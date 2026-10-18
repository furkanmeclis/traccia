package documents_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/url"

	doc "github.com/furkanmeclis/traccia/backend/internal/modules/documents/usecase"
	issue "github.com/furkanmeclis/traccia/backend/internal/modules/issues/usecase"
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

type documentTransport struct {
	documentFixture
	mux      *http.ServeMux
	bearer   string
	document contractcoverage.Object
}

var transportCatalog sync.Map

func newDocumentTransport(t *testing.T) *documentTransport {
	t.Helper()
	f := newDocumentFixture(t)
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
	return &documentTransport{documentFixture: f, mux: mux, bearer: bearer, document: doc}
}
func transportJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
func (f *documentTransport) http(t *testing.T, method, path, token, key string, input any, status int, code operations.Code) []byte {
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
func (f *documentTransport) client(t *testing.T, serverCtx context.Context) *mcp.ClientSession {
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
func (f *documentTransport) invoke(t *testing.T, cs *mcp.ClientSession, name, key string, input map[string]any) []byte {
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
		for _, pair := range [][2]string{{"issueID", "issue_id"}, {"documentID", "document_id"}, {"revisionID", "revision_id"}, {"identifier", "identifier"}} {
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
func (f *documentTransport) sql(t *testing.T, q string, args ...any) {
	t.Helper()
	if _, e := f.pool.Exec(f.ctx, q, args...); e != nil {
		t.Fatal(e)
	}
}

func documentContent(t *testing.T, nodes any) []doc.Node {
	t.Helper()
	return transportDecode[[]doc.Node](t, transportJSON(t, nodes))
}
func node(kind string, children ...any) map[string]any {
	if children == nil {
		children = []any{}
	}
	return map[string]any{"id": uuid.NewString(), "type": kind, "children": children}
}
func textNode(text string) map[string]any { return map[string]any{"text": text} }
func pNode(text string) map[string]any    { return node("p", textNode(text)) }
func createMap(owner uuid.UUID, content any) map[string]any {
	return map[string]any{"owner_kind": "issue", "owner_id": owner.String(), "format": "plate", "schema_version": 1, "content": content}
}
func (f *documentTransport) reject(t *testing.T, cs *mcp.ClientSession, name string, input map[string]any, status int, code operations.Code) {
	t.Helper()
	for _, c := range f.registry.Catalog() {
		if c.Metadata.MCPName != name {
			continue
		}
		body := issueMap(t, input)
		path := strings.ReplaceAll(c.Metadata.HTTPPath, "{workspaceID}", f.w.String())
		for _, pair := range [][2]string{{"documentID", "document_id"}, {"revisionID", "revision_id"}} {
			if strings.Contains(path, "{"+pair[0]+"}") {
				path = strings.ReplaceAll(path, "{"+pair[0]+"}", fmt.Sprint(body[pair[1]]))
				delete(body, pair[1])
			}
		}
		var payload any = body
		if c.Metadata.HTTPMethod == "GET" {
			q := url.Values{}
			for k, v := range body {
				q.Set(k, fmt.Sprint(v))
			}
			path += "?" + q.Encode()
			payload = nil
		}
		f.http(t, c.Metadata.HTTPMethod, path, f.bearer, uuid.NewString(), payload, status, code)
		transportTool(t, f.ctx, cs, name, f.w, uuid.NewString(), input, code)
		return
	}
	t.Fatal("unknown rejection tool")
}
func expectContent(t *testing.T, got []doc.Node, want any) {
	t.Helper()
	// Both sides remain typed or UseNumber JSON; no floating point intermediary.
	if !reflect.DeepEqual(got, documentContent(t, want)) {
		t.Fatal("canonical nodes, IDs, marks, or text changed")
	}
}
func TestDocumentsTransportClosedRecursiveRoundtripAndInventory(t *testing.T) {
	f := newDocumentTransport(t)
	cs := f.client(t, f.ctx)
	tools, e := cs.ListTools(f.ctx, nil)
	if e != nil {
		t.Fatal(e)
	}
	found := map[string]*mcp.Tool{}
	for _, tool := range tools.Tools {
		found[tool.Name] = tool
	}
	count := 0
	for _, c := range f.registry.Catalog() {
		if strings.HasPrefix(c.Metadata.ID, "documents.") || c.Metadata.ID == "issues.create_rich" {
			count++
			tool := found[c.Metadata.MCPName]
			if tool == nil || tool.InputSchema == nil || tool.OutputSchema == nil || tool.Meta["traccia/operation_id"] != c.Metadata.ID || tool.Meta["traccia/error_schema"] == nil {
				t.Fatal("incomplete discovery")
			}
		}
	}
	if count != 11 || len(f.registry.Catalog()) != 88 {
		t.Fatal("production catalog incomplete")
	}
	team := f.createTeam(t, "DTR")
	owner, target := f.create(t, team.ID), f.create(t, team.ID)
	project := f.createProject(t, team.ID)
	special := "  İstanbul 🧭 e\u0301 漢字\r\nline\n\r <script>\"&\u2028\u2029" + `\u2028`
	mention := node("mention", textNode(""))
	mention["key"] = "issue:" + target.ID.String()
	mention["target_kind"] = "issue"
	mention["target_id"] = target.ID.String()
	mention["value"] = "Target 🧭"
	link := node("a", textNode("safe <link>"))
	link["url"] = "https://example.test/a?q=%22&x=(y)"
	link["target"] = "_blank"
	para := node("p", map[string]any{"text": special, "bold": false, "italic": true}, mention, link)
	todo := pNode("unchecked")
	todo["listStyleType"] = "todo"
	todo["checked"] = false
	ordered := pNode("ninth")
	ordered["listStyleType"] = "decimal"
	ordered["listStart"] = 9
	ordered["listRestart"] = 9
	code := node("code_block", node("code_line", textNode("const a = '<>&';\r")), node("code_line", textNode("")))
	code["lang"] = "ts"
	cell := node("th", pNode("merged 🧭\r\n"))
	cell["rowSpan"] = 2
	cell["colSpan"] = 2
	cell["background"] = "#abc"
	cell["size"] = json.Number("123.125")
	cell["borders"] = map[string]any{"top": map[string]any{"color": "#abcd", "size": json.Number("0.5"), "style": "solid"}}
	row := node("tr", cell)
	row["size"] = json.Number("24.25")
	table := node("table", row, node("tr"))
	table["marginLeft"] = json.Number("0.125")
	table["colSizes"] = []any{json.Number("123.125"), json.Number("200.5")}
	content := []any{node("h1", textNode("Heading 🧭")), para, todo, ordered, node("blockquote", textNode("inline quote")), code, table, node("hr", textNode(""))}
	t.Run("resolve-absent", func(t *testing.T) {
		raw := f.invoke(t, cs, "resolve_document", "", map[string]any{"owner_kind": "issue", "owner_id": owner.ID.String()})
		if transportDecode[doc.ResolveOutput](t, raw).Document != nil {
			t.Fatal("unexpected document")
		}
	})
	var d doc.Document
	t.Run("create-exact-recursive", func(t *testing.T) {
		d = transportDecode[doc.Output](t, f.invoke(t, cs, "create_document", "mixed-create", createMap(owner.ID, content))).Document
		expectContent(t, d.Content, content)
	})
	t.Run("get-resolve-exact", func(t *testing.T) {
		got := transportDecode[doc.Output](t, f.invoke(t, cs, "get_document", "", map[string]any{"document_id": d.ID.String()})).Document
		expectContent(t, got.Content, content)
		resolved := transportDecode[doc.ResolveOutput](t, f.invoke(t, cs, "resolve_document", "", map[string]any{"owner_kind": "issue", "owner_id": owner.ID.String()}))
		if resolved.Document == nil || resolved.Document.ID != d.ID {
			t.Fatal("resolve mismatch")
		}
	})
	t.Run("project-owner", func(t *testing.T) {
		in := createMap(project.ID, []any{pNode("project")})
		in["owner_kind"] = "project"
		got := transportDecode[doc.Output](t, f.invoke(t, cs, "create_document", uuid.NewString(), in))
		if got.Document.OwnerKind != "project" || got.Document.OwnerID != project.ID {
			t.Fatal("wrong owner")
		}
	})
	t.Run("backlinks", func(t *testing.T) {
		out := transportDecode[doc.BacklinkList](t, f.invoke(t, cs, "list_document_backlinks", "", map[string]any{"target_kind": "issue", "target_id": target.ID.String(), "limit": 100, "after": ""}))
		if len(out.Items) != 1 || out.Items[0].DocumentID != d.ID || out.Items[0].NodeID.String() != mention["id"] {
			t.Fatal("missing real reference")
		}
	})
	for _, format := range []string{"json", "html", "markdown"} {
		t.Run("export-"+format, func(t *testing.T) {
			out := transportDecode[doc.ExportOutput](t, f.invoke(t, cs, "export_document", "", map[string]any{"document_id": d.ID.String(), "format": format}))
			if out.Revision != d.Revision {
				t.Fatal("wrong exported revision")
			}
			switch format {
			case "json":
				var payload struct {
					Content []doc.Node `json:"content"`
				}
				if e := json.Unmarshal([]byte(out.Content), &payload); e != nil {
					t.Fatal(e)
				}
				expectContent(t, payload.Content, content)
			case "html":
				if strings.Contains(out.Content, "<script>") || !strings.Contains(out.Content, "&lt;script&gt;") || !strings.Contains(out.Content, `rel="noopener noreferrer"`) || !strings.Contains(out.Content, "rowspan=\"2\"") {
					t.Fatal("unsafe or incomplete HTML")
				}
			case "markdown":
				if len(out.Losses) == 0 || strings.Contains(out.Content, "<script>") {
					t.Fatal("unsafe or undeclared-loss markdown")
				}
			}
		})
	}
}

func TestDocumentsTransportPatchesRevisionsAndImmutableReplay(t *testing.T) {
	f := newDocumentTransport(t)
	cs := f.client(t, f.ctx)
	team := f.createTeam(t, "DPT")
	owner := f.create(t, team.ID)
	h, a, b, h2, c := node("h1", textNode("one")), pNode("A\r\n"), pNode("B"), node("h1", textNode("two")), pNode("untouched")
	content := []any{h, a, b, h2, c}
	d := transportDecode[doc.Output](t, f.invoke(t, cs, "create_document", "patch-create", createMap(owner.ID, content))).Document
	initial := d
	apply := func(t *testing.T, patch any) doc.Document {
		t.Helper()
		input := map[string]any{"document_id": d.ID.String(), "expected_revision": d.Revision.String(), "format": "plate", "schema_version": 1, "patches": []any{patch}}
		raw := f.invoke(t, cs, "patch_document", uuid.NewString(), input)
		next := transportDecode[doc.Output](t, raw).Document
		unchanged := next.Content[len(next.Content)-1]
		expectContent(t, []doc.Node{unchanged}, []any{c})
		return next
	}
	t.Run("replace-node", func(t *testing.T) {
		replacement := issueMap(t, a)
		replacement["children"] = []any{textNode("changed 🧭\r\n")}
		d = apply(t, map[string]any{"op": "replace_node", "node_id": a["id"], "node": replacement})
		a = replacement
	})
	inserted := pNode("inserted")
	t.Run("insert-node", func(t *testing.T) {
		d = apply(t, map[string]any{"op": "insert_nodes", "parent_id": nil, "before_id": b["id"], "nodes": []any{inserted}})
	})
	t.Run("move-node", func(t *testing.T) {
		d = apply(t, map[string]any{"op": "move_node", "node_id": inserted["id"], "parent_id": nil, "before_id": a["id"]})
	})
	t.Run("remove-node", func(t *testing.T) { d = apply(t, map[string]any{"op": "remove_node", "node_id": inserted["id"]}) })
	t.Run("replace-section", func(t *testing.T) {
		d = apply(t, map[string]any{"op": "replace_section", "heading_id": h["id"], "expected_member_ids": []any{a["id"], b["id"]}, "nodes": []any{pNode("whole section")}})
	})
	t.Run("delta-only-and-outbox", func(t *testing.T) {
		var deltas, checkpoints, events int
		if e := f.pool.QueryRow(f.ctx, "SELECT count(*) FILTER(WHERE kind='patch' AND checkpoint IS NULL),count(*) FILTER(WHERE checkpoint IS NOT NULL) FROM document_revisions WHERE document_id=(SELECT id FROM documents WHERE uuid=$1)", d.ID).Scan(&deltas, &checkpoints); e != nil {
			t.Fatal(e)
		}
		if deltas != 5 || checkpoints != 1 {
			t.Fatal("ordinary patch stored a full checkpoint")
		}
		if e := f.pool.QueryRow(f.ctx, "SELECT count(*) FROM outbox_events WHERE event_name='documents.patched' AND (payload->'Payload') ? 'patches' AND NOT (payload->'Payload') ? 'content'").Scan(&events); e != nil {
			t.Fatal(e)
		}
		if events != 5 {
			t.Fatal("patch outbox lacks delta")
		}
	})
	t.Run("historical-get", func(t *testing.T) {
		old := transportDecode[doc.RevisionOutput](t, f.invoke(t, cs, "get_document_revision", "", map[string]any{"document_id": d.ID.String(), "revision_id": initial.Revision.String()}))
		expectContent(t, old.Document.Content, content)
	})
	t.Run("stale-CAS", func(t *testing.T) {
		f.reject(t, cs, "restore_document", map[string]any{"document_id": d.ID.String(), "expected_revision": initial.Revision.String(), "revision_id": initial.Revision.String()}, 409, operations.VersionConflict)
	})
	t.Run("restore-and-replay", func(t *testing.T) {
		input := map[string]any{"document_id": d.ID.String(), "expected_revision": d.Revision.String(), "revision_id": initial.Revision.String()}
		raw := f.invoke(t, cs, "restore_document", "restore-replay", input)
		d = transportDecode[doc.Output](t, raw).Document
		expectContent(t, d.Content, content)
		if d.Revision == initial.Revision {
			t.Fatal("restore rewrote historical revision")
		}
		if !bytes.Equal(raw, f.invoke(t, cs, "restore_document", "restore-replay", input)) {
			t.Fatal("replay changed")
		}
	})
	t.Run("replace", func(t *testing.T) {
		d = transportDecode[doc.Output](t, f.invoke(t, cs, "replace_document", uuid.NewString(), map[string]any{"document_id": d.ID.String(), "expected_revision": d.Revision.String(), "format": "plate", "schema_version": 1, "content": content})).Document
	})
	t.Run("revisions-traverse", func(t *testing.T) {
		seen := map[uuid.UUID]bool{}
		after := ""
		for {
			out := transportDecode[doc.RevisionList](t, f.invoke(t, cs, "list_document_revisions", "", map[string]any{"document_id": d.ID.String(), "limit": 2, "after": after}))
			for _, v := range out.Items {
				if seen[v.ID] {
					t.Fatal("repeated revision")
				}
				seen[v.ID] = true
			}
			if !out.HasMore {
				break
			}
			if out.NextCursor == "" || out.NextCursor == after {
				t.Fatal("nonprogressing cursor")
			}
			after = out.NextCursor
		}
		if len(seen) != 8 {
			t.Fatalf("revision count %d", len(seen))
		}
	})
}

func TestDocumentsTransportStrictErrorsAndRichAtomicity(t *testing.T) {
	f := newDocumentTransport(t)
	cs := f.client(t, f.ctx)
	team := f.createTeam(t, "DBD")
	owner := f.create(t, team.ID)
	counts := func() string {
		t.Helper()
		var s string
		if e := f.pool.QueryRow(f.ctx, "SELECT jsonb_build_array((SELECT count(*) FROM issues),(SELECT count(*) FROM issue_aliases),(SELECT count(*) FROM issue_history),(SELECT count(*) FROM documents),(SELECT count(*) FROM document_revisions),(SELECT count(*) FROM document_references),(SELECT count(*) FROM workspace_history),(SELECT count(*) FROM outbox_events),(SELECT count(*) FROM operation_receipts),(SELECT sum(next_issue_number) FROM teams))::text").Scan(&s); e != nil {
			t.Fatal(e)
		}
		return s
	}
	bad := map[string]func(map[string]any){
		"unknown-version":  func(in map[string]any) { in["schema_version"] = 2 },
		"unknown-format":   func(in map[string]any) { in["format"] = "html" },
		"unknown-property": func(in map[string]any) { in["unknown"] = true },
		"deep-unknown-property": func(in map[string]any) {
			in["content"] = []any{node("p", map[string]any{"text": "x", "unknown": true})}
		},
		"unknown-node":               func(in map[string]any) { in["content"] = []any{node("script", textNode("x"))} },
		"structural-inline-child":    func(in map[string]any) { in["content"] = []any{node("p", pNode("x"))} },
		"code-line-structural-child": func(in map[string]any) { in["content"] = []any{node("code_block", node("code_line", pNode("x")))} },
		"duplicate-UUID-identity": func(in map[string]any) {
			a := pNode("a")
			b := pNode("b")
			b["id"] = strings.ToUpper(a["id"].(string))
			in["content"] = []any{a, b}
		},
		"unsafe-link": func(in map[string]any) {
			a := node("a", textNode("x"))
			a["url"] = "javascript:alert(1)"
			in["content"] = []any{node("p", a)}
		},
		"unsafe-layout": func(in map[string]any) {
			c := node("td", pNode("x"))
			c["background"] = "red;background:url(javascript:alert(1))"
			in["content"] = []any{node("table", node("tr", c))}
		},
		"layout-number-loss": func(in map[string]any) {
			a := node("table", node("tr", node("td", pNode("x"))))
			a["marginLeft"] = json.Number("0.1000000000000000001")
			in["content"] = []any{a}
		},
		"missing-reference": func(in map[string]any) {
			a := node("mention", textNode(""))
			a["key"] = "issue:" + fmt.Sprint(a["target_id"])
			a["value"] = "absent"
			a["target_kind"] = "issue"
			a["target_id"] = uuid.NewString()
			a["key"] = "issue:" + a["target_id"].(string)
			in["content"] = []any{node("p", a)}
		},
	}
	for name, mutate := range bad {
		t.Run(name, func(t *testing.T) {
			before := counts()
			in := createMap(owner.ID, []any{pNode("valid")})
			mutate(in)
			f.reject(t, cs, "create_document", in, 400, operations.Validation)
			if counts() != before {
				t.Fatal("invalid create changed persistent state")
			}
		})
	}
	rich := map[string]any{"issue": issueMap(t, issue.CreateInput{TeamID: team.ID, StatusID: owner.StatusID, Title: "atomic rich 🧭", Priority: "none", LabelIDs: []string{}}), "format": "plate", "schema_version": 1, "content": []any{pNode("rich\r\n🧭")}}
	t.Run("rich-atomic-success-and-replay", func(t *testing.T) {
		raw := f.invoke(t, cs, "create_rich_issue", "rich-create", rich)
		out := transportDecode[doc.RichOutput](t, raw)
		if out.Document.OwnerID != out.Issue.Issue.ID {
			t.Fatal("rich owner mismatch")
		}
		if !bytes.Equal(raw, f.invoke(t, cs, "create_rich_issue", "rich-create", rich)) {
			t.Fatal("rich replay changed")
		}
	})
	t.Run("rich-half-write-rollback", func(t *testing.T) {
		before := counts()
		f.sql(t, "CREATE FUNCTION transport_reject_document() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'forced independent revision failure'; END $$; CREATE TRIGGER transport_reject_document BEFORE INSERT ON document_revisions FOR EACH ROW EXECUTE FUNCTION transport_reject_document()")
		f.reject(t, cs, "create_rich_issue", rich, 500, operations.Internal)
		if counts() != before {
			t.Fatal("rich rollback leaked issue, number, history, references, receipt, document or outbox")
		}
		f.sql(t, "DROP TRIGGER transport_reject_document ON document_revisions; DROP FUNCTION transport_reject_document()")
	})
	t.Run("cross-workspace-reference-and-owner", func(t *testing.T) {
		raw, e := f.call("workspaces.create", uuid.NewString(), uuid.Nil, ws.CreateInput{Name: "Other document workspace"})
		other := decodeResult[ws.WorkspaceOutput](t, raw, e).Workspace.ID
		second := f.documentFixture
		second.w = other
		ot := second.createTeam(t, "OTH")
		foreign := second.create(t, ot.ID)
		before := counts()
		f.reject(t, cs, "create_document", createMap(foreign.ID, []any{pNode("foreign")}), 404, operations.NotFound)
		m := node("mention", textNode(""))
		m["key"] = "issue:" + foreign.ID.String()
		m["target_kind"] = "issue"
		m["target_id"] = foreign.ID.String()
		m["key"] = "issue:" + foreign.ID.String()
		m["value"] = "foreign"
		f.reject(t, cs, "create_document", createMap(owner.ID, []any{node("p", m)}), 400, operations.Validation)
		if counts() != before {
			t.Fatal("cross-workspace request changed state")
		}
	})
}

func TestDocumentsTransportBudgetDiagnosticsAndEscapedCanonicalBoundary(t *testing.T) {
	f := newDocumentTransport(t)
	cs := f.client(t, f.ctx)
	team := f.createTeam(t, "DLM")
	owner := f.create(t, team.ID)
	base := createMap(owner.ID, []any{pNode("ok")})
	cases := map[string]struct {
		content any
		detail  string
	}{
		"canonical-bytes": {[]any{pNode(strings.Repeat("<", doc.MaxContentBytes))}, "bytes_limit"},
		"node-count": {func() any {
			nodes := []any{}
			for i := 0; i < 1001; i++ {
				nodes = append(nodes, pNode("x"))
			}
			return nodes
		}(), "node_limit"},
		"deep-recursion": {func() any {
			var n any = textNode("x")
			for i := 0; i < 48; i++ {
				n = node("p", n)
			}
			return []any{n}
		}(), "depth_limit"},
		"raw-bytes": {[]any{pNode(strings.Repeat("x", 4*1024*1024))}, "bytes_limit"},
	}
	// HTTP and actual SDK both must return the opted-in actionable error before
	// the recursive schema walker, including structurally invalid deep input.
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			in := issueMap(t, base)
			in["content"] = tc.content
			path := "/v1/workspaces/" + f.w.String() + "/documents"
			request := httptest.NewRequest("POST", path, bytes.NewReader(transportJSON(t, in)))
			request.Header.Set("Authorization", "Bearer "+f.bearer)
			request.Header.Set("Content-Type", "application/json")
			request.Header.Set("Idempotency-Key", uuid.NewString())
			rec := httptest.NewRecorder()
			f.mux.ServeHTTP(rec, request)
			if rec.Code != 400 || !bytes.Contains(rec.Body.Bytes(), []byte(`"code":"`+tc.detail+`"`)) {
				t.Fatalf("HTTP missing %s diagnostic", tc.detail)
			}
			raw := transportTool(t, f.ctx, cs, "create_document", f.w, uuid.NewString(), in, operations.Validation)
			if !bytes.Contains(raw, []byte(`"code":"`+tc.detail+`"`)) {
				t.Fatalf("SDK missing %s diagnostic", tc.detail)
			}
		})
	}
	t.Run("near-canonical-limit-sdk-escape-expansion", func(t *testing.T) {
		// Go's SDK encoding expands <>& to six-byte escapes, while the browser's
		// canonical JSON contains the same three literal bytes.
		special := strings.Repeat("<>&", 170000) + "\u2028\u2029" + `\u2028` + "🧭\r\n"
		content := []any{pNode(special)}
		body := createMap(owner.ID, content)
		if len(transportJSON(t, body)) <= 1<<20 {
			t.Fatal("fixture did not cross old raw budget")
		}
		if len(special) >= doc.MaxContentBytes {
			t.Fatal("fixture too large")
		}
		got := transportDecode[doc.Output](t, f.invoke(t, cs, "create_document", uuid.NewString(), body))
		expectContent(t, got.Document.Content, content)
	})
	t.Run("released-HTTP-one-MiB-limit", func(t *testing.T) {
		path := "/v1/workspaces/" + f.w.String() + "/issues"
		f.http(t, "POST", path, f.bearer, uuid.NewString(), map[string]any{"title": strings.Repeat("x", (1<<20)+1)}, 400, operations.Validation)
	})
}

func TestDocumentsTransportCompletePagesAndRetainedArchivedReferences(t *testing.T) {
	f := newDocumentTransport(t)
	cs := f.client(t, f.ctx)
	team := f.createTeam(t, "DPG")
	owner, target := f.create(t, team.ID), f.create(t, team.ID)
	nodes := []any{}
	for i := 0; i < 127; i++ {
		m := node("mention", textNode(""))
		m["key"] = "issue:" + target.ID.String()
		m["target_kind"] = "issue"
		m["target_id"] = target.ID.String()
		m["value"] = fmt.Sprintf("mention %d 🧭", i)
		nodes = append(nodes, node("p", m))
	}
	d := transportDecode[doc.Output](t, f.invoke(t, cs, "create_document", uuid.NewString(), createMap(owner.ID, nodes))).Document
	t.Run("backlinks-over-100", func(t *testing.T) {
		seen := map[uuid.UUID]bool{}
		after := ""
		for {
			out := transportDecode[doc.BacklinkList](t, f.invoke(t, cs, "list_document_backlinks", "", map[string]any{"target_kind": "issue", "target_id": target.ID.String(), "limit": 37, "after": after}))
			for _, v := range out.Items {
				if seen[v.ID] {
					t.Fatal("duplicate backlink")
				}
				seen[v.ID] = true
			}
			if !out.HasMore {
				break
			}
			if out.NextCursor == after || out.NextCursor == "" {
				t.Fatal("nonprogressing backlink cursor")
			}
			after = out.NextCursor
		}
		if len(seen) != 127 {
			t.Fatalf("backlink traversal %d", len(seen))
		}
	})
	// Materialize enough immutable versions through the same production registry,
	// then independently traverse both real transport bindings without a cap.
	for i := 0; i < 105; i++ {
		raw, e := f.call("documents.replace", uuid.NewString(), f.w, doc.ReplaceInput{DocumentID: d.ID, ExpectedRevision: d.Revision, Format: "plate", SchemaVersion: 1, Content: documentContent(t, nodes)})
		d = decodeResult[doc.Output](t, raw, e).Document
	}
	t.Run("revisions-over-100", func(t *testing.T) {
		seen := map[uuid.UUID]bool{}
		after := ""
		for {
			out := transportDecode[doc.RevisionList](t, f.invoke(t, cs, "list_document_revisions", "", map[string]any{"document_id": d.ID.String(), "limit": 41, "after": after}))
			for _, v := range out.Items {
				if seen[v.ID] {
					t.Fatal("duplicate historical revision")
				}
				seen[v.ID] = true
			}
			if !out.HasMore {
				break
			}
			if out.NextCursor == after || out.NextCursor == "" {
				t.Fatal("nonprogressing revision cursor")
			}
			after = out.NextCursor
		}
		if len(seen) != 106 {
			t.Fatalf("revision traversal %d", len(seen))
		}
	})
	t.Run("archived-target-reference-retained", func(t *testing.T) {
		f.sql(t, "UPDATE issues SET archived=true WHERE uuid=$1", target.ID)
		got := transportDecode[doc.Output](t, f.invoke(t, cs, "get_document", "", map[string]any{"document_id": d.ID.String()}))
		expectContent(t, got.Document.Content, nodes)
		d = transportDecode[doc.Output](t, f.invoke(t, cs, "replace_document", uuid.NewString(), map[string]any{"document_id": d.ID.String(), "expected_revision": d.Revision.String(), "format": "plate", "schema_version": 1, "content": nodes})).Document
		out := transportDecode[doc.BacklinkList](t, f.invoke(t, cs, "list_document_backlinks", "", map[string]any{"target_kind": "issue", "target_id": target.ID.String(), "limit": 100, "after": ""}))
		if len(out.Items) != 100 || !out.HasMore {
			t.Fatal("archive lost retained references")
		}
	})
	t.Run("remove-reference-clears-backlinks", func(t *testing.T) {
		f.invoke(t, cs, "replace_document", uuid.NewString(), map[string]any{"document_id": d.ID.String(), "expected_revision": d.Revision.String(), "format": "plate", "schema_version": 1, "content": []any{pNode("references removed")}})
		out := transportDecode[doc.BacklinkList](t, f.invoke(t, cs, "list_document_backlinks", "", map[string]any{"target_kind": "issue", "target_id": target.ID.String(), "limit": 100, "after": ""}))
		if len(out.Items) != 0 {
			t.Fatal("removed references remain active")
		}
	})
}

func TestDocumentsTransportCredentialRevalidationAndOwnerIsolation(t *testing.T) {
	f := newDocumentTransport(t)
	human := f.client(t, f.ctx)
	team := f.createTeam(t, "DCR")
	owner := f.create(t, team.ID)
	mint := func(audience string, scopes []string) ws.CreateCredentialOutput {
		return transportDecode[ws.CreateCredentialOutput](t, transportTool(t, f.ctx, human, "create_workspace_credential", f.w, uuid.NewString(), ws.CreateCredentialInput{Name: "Document acceptance", Audience: audience, Scopes: scopes, ExpiresAt: time.Now().Add(time.Hour).UTC().Truncate(time.Second)}, ""))
	}
	api := mint("api", []string{"workspace.read", "workspace.write"})
	pat := mint("mcp", []string{"workspace.read", "workspace.write"})
	read := mint("mcp", []string{"workspace.read"})
	writer := f.client(t, ws.WithToolCredential(context.Background(), pat.Token))
	reader := f.client(t, ws.WithToolCredential(context.Background(), read.Token))
	wrong := f.client(t, ws.WithToolCredential(context.Background(), api.Token))
	input := createMap(owner.ID, []any{pNode("credential 🧭")})
	var exact []byte
	var d doc.Document
	t.Run("read-scope-write-denied", func(t *testing.T) {
		transportTool(t, f.ctx, reader, "create_document", f.w, "read-denied", input, operations.Forbidden)
	})
	t.Run("credential-create", func(t *testing.T) {
		exact = transportTool(t, f.ctx, writer, "create_document", f.w, "document-receipt", input, "")
		d = transportDecode[doc.Output](t, exact).Document
	})
	get := map[string]any{"document_id": d.ID.String()}
	path := "/v1/workspaces/" + f.w.String() + "/documents/" + d.ID.String()
	t.Run("audience-and-read-capability", func(t *testing.T) {
		transportTool(t, f.ctx, wrong, "get_document", f.w, "", get, operations.Forbidden)
		f.http(t, "GET", path, pat.Token, "", nil, 403, operations.Forbidden)
		f.http(t, "GET", path, api.Token, "", nil, 200, "")
		out := transportDecode[doc.Output](t, transportTool(t, f.ctx, reader, "get_document", f.w, "", get, ""))
		if out.Document.Capabilities.Manage {
			t.Fatal("read credential advertises write")
		}
	})
	t.Run("expired-receipt-revalidation", func(t *testing.T) {
		f.sql(t, "UPDATE workspace_tool_credentials SET created_at=clock_timestamp()-interval '2 hours',expires_at=clock_timestamp()-interval '1 second' WHERE uuid=$1", pat.Credential.ID)
		transportTool(t, f.ctx, writer, "create_document", f.w, "document-receipt", input, operations.Unauthenticated)
		f.sql(t, "UPDATE workspace_tool_credentials SET expires_at=clock_timestamp()+interval '1 hour' WHERE uuid=$1", pat.Credential.ID)
		if !bytes.Equal(exact, transportTool(t, f.ctx, writer, "create_document", f.w, "document-receipt", input, "")) {
			t.Fatal("restored receipt changed")
		}
	})
	t.Run("archived-owner-readable-write-denied", func(t *testing.T) {
		f.sql(t, "UPDATE issues SET archived=true WHERE uuid=$1", owner.ID)
		out := transportDecode[doc.Output](t, transportTool(t, f.ctx, writer, "get_document", f.w, "", get, ""))
		if out.Document.Capabilities.Manage {
			t.Fatal("archived owner advertises write")
		}
		transportTool(t, f.ctx, writer, "replace_document", f.w, uuid.NewString(), map[string]any{"document_id": d.ID.String(), "expected_revision": d.Revision.String(), "format": "plate", "schema_version": 1, "content": []any{pNode("blocked")}}, operations.Conflict)
	})
	t.Run("revoked-receipt-revalidation", func(t *testing.T) {
		transportTool(t, f.ctx, human, "revoke_workspace_credential", f.w, "revoke-doc", ws.RevokeInput{CredentialID: pat.Credential.ID}, "")
		transportTool(t, f.ctx, writer, "create_document", f.w, "document-receipt", input, operations.Unauthenticated)
	})
	t.Run("disabled-owner-revalidation", func(t *testing.T) {
		f.sql(t, "UPDATE users SET status='disabled' WHERE uuid=$1", f.actor)
		f.http(t, "GET", path, api.Token, "", nil, 401, operations.Unauthenticated)
		transportTool(t, f.ctx, reader, "get_document", f.w, "", get, operations.Unauthenticated)
	})
}
