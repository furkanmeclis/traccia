package comments_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/url"

	"crypto/sha256"
	attachment "github.com/furkanmeclis/traccia/backend/internal/modules/attachments/usecase"
	comment "github.com/furkanmeclis/traccia/backend/internal/modules/comments/usecase"
	doc "github.com/furkanmeclis/traccia/backend/internal/modules/documents/usecase"
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

type commentTransport struct {
	commentFixture
	mux      *http.ServeMux
	bearer   string
	document contractcoverage.Object
}

var transportCatalog sync.Map

func newCommentTransport(t *testing.T) *commentTransport {
	t.Helper()
	f := newCommentFixture(t)
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
	return &commentTransport{commentFixture: f, mux: mux, bearer: bearer, document: doc}
}
func transportJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
func (f *commentTransport) http(t *testing.T, method, path, token, key string, input any, status int, code operations.Code) []byte {
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
func (f *commentTransport) client(t *testing.T, serverCtx context.Context) *mcp.ClientSession {
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
func (f *commentTransport) invoke(t *testing.T, cs *mcp.ClientSession, name, key string, input map[string]any) []byte {
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
		for _, pair := range [][2]string{{"issueID", "issue_id"}, {"commentID", "comment_id"}, {"revisionID", "revision_id"}, {"documentID", "document_id"}, {"attachmentID", "attachment_id"}, {"uploadID", "upload_id"}} {
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
func (f *commentTransport) sql(t *testing.T, q string, args ...any) {
	t.Helper()
	if _, e := f.pool.Exec(f.ctx, q, args...); e != nil {
		t.Fatal(e)
	}
}

func commentMap(t *testing.T, v any) map[string]any { return issueMap(t, v) }
func (f *commentTransport) op(t *testing.T, cs *mcp.ClientSession, id string, in any) []byte {
	t.Helper()
	for _, c := range f.registry.Catalog() {
		if c.Metadata.ID == id {
			key := ""
			if c.Metadata.Effect == operations.Write {
				key = uuid.NewString()
			}
			return f.invoke(t, cs, c.Metadata.MCPName, key, commentMap(t, in))
		}
	}
	t.Fatal("missing operation", id)
	return nil
}
func (f *commentTransport) rejectOp(t *testing.T, cs *mcp.ClientSession, id string, in any, status int, code operations.Code) {
	t.Helper()
	for _, c := range f.registry.Catalog() {
		if c.Metadata.ID != id {
			continue
		}
		body := commentMap(t, in)
		path := strings.ReplaceAll(c.Metadata.HTTPPath, "{workspaceID}", f.w.String())
		for _, p := range [][2]string{{"commentID", "comment_id"}, {"revisionID", "revision_id"}, {"documentID", "document_id"}, {"uploadID", "upload_id"}, {"attachmentID", "attachment_id"}, {"issueID", "issue_id"}} {
			if strings.Contains(path, "{"+p[0]+"}") {
				path = strings.ReplaceAll(path, "{"+p[0]+"}", fmt.Sprint(body[p[1]]))
				delete(body, p[1])
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
		key := ""
		if c.Metadata.Effect == operations.Write {
			key = uuid.NewString()
		}
		transportTool(t, f.ctx, cs, c.Metadata.MCPName, f.w, key, in, code)
		return
	}
	t.Fatal("missing operation", id)
}
func tcNode(kind string, children ...any) map[string]any {
	if children == nil {
		children = []any{}
	}
	return map[string]any{"id": uuid.NewString(), "type": kind, "children": children}
}
func tcP(text string) map[string]any { return tcNode("p", map[string]any{"text": text}) }
func tcContent(t *testing.T, v any) []doc.Node {
	return transportDecode[[]doc.Node](t, transportJSON(t, v))
}
func tcCreate(issueID uuid.UUID, nodes []doc.Node) comment.CreateInput {
	return comment.CreateInput{IssueID: issueID, Format: "plate", SchemaVersion: 1, Content: nodes}
}
func (f *commentTransport) makeComment(t *testing.T, cs *mcp.ClientSession, id uuid.UUID, nodes []doc.Node) comment.Comment {
	return transportDecode[comment.Output](t, f.op(t, cs, "comments.create", tcCreate(id, nodes))).Comment
}
func tcMixed(t *testing.T, actor, issueID, projectID uuid.UUID) []doc.Node {
	p := tcP("  Türkçe 👩🏽‍💻 e\u0301\r\nline\n\r <script>alert(1)</script> ")
	p["listStyleType"] = "todo"
	p["checked"] = false
	p["indent"] = 0
	for _, target := range []struct {
		kind string
		id   uuid.UUID
	}{{"user", actor}, {"issue", issueID}, {"project", projectID}} {
		m := tcNode("mention", map[string]any{"text": ""})
		m["key"] = target.kind + ":" + target.id.String()
		m["value"] = "İ 🧭"
		m["target_kind"] = target.kind
		m["target_id"] = target.id.String()
		p["children"] = append(p["children"].([]any), m)
	}
	link := tcNode("a", map[string]any{"text": "link"})
	link["url"] = "https://example.test/?q=%3Ctag%3E"
	link["target"] = "_blank"
	p["children"] = append(p["children"].([]any), link)
	cell := tcNode("th", tcP("merged\r\n表"))
	cell["colSpan"] = 2
	cell["rowSpan"] = 2
	cell["background"] = "#abc"
	cell["size"] = 12.5
	cell["borders"] = map[string]any{"left": map[string]any{"color": "#123456", "style": "solid", "size": 0.5}}
	table := tcNode("table", tcNode("tr", cell), tcNode("tr"))
	table["colSizes"] = []any{10.25, 99.5}
	table["marginLeft"] = 1.5
	code := tcNode("code_block", tcNode("code_line", map[string]any{"text": "a\r\nb"}), tcNode("code_line", map[string]any{"text": ""}))
	code["lang"] = "go"
	return tcContent(t, []any{tcNode("h1", map[string]any{"text": "Heading 🧭"}), p, table, code, tcNode("hr", map[string]any{"text": ""}), tcNode("h2", map[string]any{"text": "Next"}), tcP("untouched")})
}
func TestCommentsTransportClosedRoundtripJournalAndInventory(t *testing.T) {
	f := newCommentTransport(t)
	cs := f.client(t, f.ctx)
	catalog := f.registry.Catalog()
	ops, writes := 0, 0
	for _, c := range catalog {
		if strings.HasPrefix(c.Metadata.ID, "comments.") || c.Metadata.ID == "issue_timeline.list" {
			ops++
			if c.Metadata.Effect == operations.Write {
				writes++
			}
		}
	}
	if len(catalog) != 114 || ops != 12 || writes != 5 {
		t.Fatalf("inventory%d/%d/%d", len(catalog), ops, writes)
	}
	team := f.createTeam(t, "TCM")
	is := f.create(t, team.ID)
	project := f.createProject(t, team.ID)
	initial := tcMixed(t, f.actor, is.ID, project.ID)
	var c comment.Comment
	t.Run("all_closed_nodes_uuid_text_layout_roundtrip", func(t *testing.T) {
		c = f.makeComment(t, cs, is.ID, initial)
		if c.AuthorID != f.actor || c.CreatedChannel != "api" || !bytes.Equal(transportJSON(t, c.Content), transportJSON(t, initial)) {
			t.Fatal("canonical fidelity/provenance")
		}
		got := transportDecode[comment.Output](t, f.op(t, cs, "comments.get", comment.CommentInput{CommentID: c.ID})).Comment
		if !reflect.DeepEqual(got.Content, initial) || got.Revision != c.Revision {
			t.Fatal("full get changed")
		}
	})
	original := c
	var patched, replaced, deleted comment.Comment
	t.Run("node_patch_delta_and_unchanged_subtrees", func(t *testing.T) {
		id := commentMap(t, initial[1])["id"].(string)
		next := paragraph(id, "node patch\r\nİ")
		in := comment.PatchInput{CommentID: c.ID, ExpectedRevision: c.Revision, Format: "plate", SchemaVersion: 1, Patches: []doc.Patch{{Value: &doc.ReplaceNode{Op: "replace_node", NodeID: id, Node: next}}}}
		patched = transportDecode[comment.Output](t, f.op(t, cs, "comments.patch", in)).Comment
		for i := range initial {
			if i != 1 && !reflect.DeepEqual(initial[i], patched.Content[i]) {
				t.Fatal("untouched node changed", i)
			}
		}
		var checkpoint bool
		var patches int
		if e := f.pool.QueryRow(f.ctx, "SELECT checkpoint IS NULL,jsonb_array_length(patches) FROM comment_revisions WHERE uuid=$1", patched.Revision).Scan(&checkpoint, &patches); e != nil || !checkpoint || patches != 1 {
			t.Fatal("patch not delta-only", e)
		}
		c = patched
	})
	t.Run("replace_and_stale_cas", func(t *testing.T) {
		replaced = transportDecode[comment.Output](t, f.op(t, cs, "comments.replace", comment.ReplaceInput{CommentID: c.ID, ExpectedRevision: c.Revision, Format: "plate", SchemaVersion: 1, Content: initial})).Comment
		f.rejectOp(t, cs, "comments.delete", comment.DeleteInput{CommentID: c.ID, ExpectedRevision: c.Revision}, 409, operations.VersionConflict)
		c = replaced
	})
	t.Run("content_retaining_tombstone", func(t *testing.T) {
		deleted = transportDecode[comment.Output](t, f.op(t, cs, "comments.delete", comment.DeleteInput{CommentID: c.ID, ExpectedRevision: c.Revision})).Comment
		if !deleted.Deleted || !reflect.DeepEqual(deleted.Content, c.Content) || deleted.AuthorID != original.AuthorID || deleted.CreatedChannel != original.CreatedChannel {
			t.Fatal("tombstone content/provenance")
		}
		hidden := transportDecode[comment.List](t, f.op(t, cs, "comments.list", comment.ListInput{IssueID: is.ID, Limit: 100}))
		shown := transportDecode[comment.List](t, f.op(t, cs, "comments.list", comment.ListInput{IssueID: is.ID, IncludeDeleted: true, Limit: 100}))
		if len(hidden.Items) != 0 || len(shown.Items) != 1 || !shown.Items[0].Deleted {
			t.Fatal("deleted listing")
		}
		c = deleted
	})
	t.Run("independent_historical_get_and_restore_appends", func(t *testing.T) {
		old := transportDecode[comment.RevisionOutput](t, f.op(t, cs, "comments.get_revision", comment.RevisionInput{CommentID: c.ID, RevisionID: original.Revision}))
		if !reflect.DeepEqual(old.Comment.Content, initial) || old.Comment.Deleted || old.Revision.Channel != "api" {
			t.Fatal("history changed")
		}
		f.rejectOp(t, cs, "comments.restore", comment.RestoreInput{CommentID: c.ID, ExpectedRevision: c.Revision, RevisionID: deleted.Revision}, 409, operations.Conflict)
		c = transportDecode[comment.Output](t, f.op(t, cs, "comments.restore", comment.RestoreInput{CommentID: c.ID, ExpectedRevision: c.Revision, RevisionID: original.Revision})).Comment
		if c.Deleted || c.Revision == original.Revision || c.Revision == deleted.Revision || !reflect.DeepEqual(c.Content, initial) {
			t.Fatal("restore overwrote source")
		}
		hist := transportDecode[comment.RevisionList](t, f.op(t, cs, "comments.revisions", comment.RevisionsInput{CommentID: c.ID, Limit: 100}))
		if len(hist.Items) != 5 || hist.Items[4].SourceRevision == nil || *hist.Items[4].SourceRevision != original.Revision.String() {
			t.Fatal("complete revision chain")
		}
	})
	t.Run("typed_mentions_and_backlinks", func(t *testing.T) {
		for _, target := range []struct {
			kind string
			id   uuid.UUID
		}{{"user", f.actor}, {"issue", is.ID}, {"project", project.ID}} {
			out := transportDecode[comment.BacklinkList](t, f.op(t, cs, "comments.backlinks", comment.BacklinksInput{TargetKind: target.kind, TargetID: target.id, Limit: 100}))
			if len(out.Items) != 1 || out.Items[0].CommentID != c.ID || out.Items[0].IssueID != is.ID || out.Items[0].Revision != c.Revision {
				t.Fatal("scoped backlink")
			}
		}
	})
	for _, format := range []string{"json", "html", "markdown"} {
		t.Run("derived_export_"+format, func(t *testing.T) {
			out := transportDecode[comment.ExportOutput](t, f.op(t, cs, "comments.export", comment.ExportInput{CommentID: c.ID, Format: format}))
			if out.CommentID != c.ID || out.Revision != c.Revision {
				t.Fatal("export identity")
			}
			if format == "json" {
				var body struct {
					Format        string     `json:"format"`
					SchemaVersion int32      `json:"schema_version"`
					Content       []doc.Node `json:"content"`
				}
				if e := json.Unmarshal([]byte(out.Content), &body); e != nil || body.Format != "plate" || body.SchemaVersion != 1 || !reflect.DeepEqual(body.Content, initial) || len(out.Losses) != 0 {
					t.Fatal("canonical export loss", e)
				}
			} else if len(out.Losses) == 0 || strings.Contains(out.Content, "<script>") {
				t.Fatal("unsafe or unreported derived export")
			}
		})
	}
	t.Run("canonical_timeline_separate_from_metadata_version", func(t *testing.T) {
		out := transportDecode[comment.TimelineList](t, f.op(t, cs, "issue_timeline.list", comment.TimelineInput{IssueID: is.ID, Limit: 100}))
		count := 0
		for _, v := range out.Items {
			if v.Detail.Kind == "comment" {
				count++
				if v.Detail.Comment.CommentID != c.ID || v.ActorID != f.actor || v.Channel != "api" {
					t.Fatal("timeline provenance")
				}
			}
		}
		if count != 5 {
			t.Fatalf("timeline count%d", count)
		}
		var version int64
		if e := f.pool.QueryRow(f.ctx, "SELECT version FROM issues WHERE uuid=$1", is.ID).Scan(&version); e != nil || version != is.Version {
			t.Fatal("metadata version changed", e)
		}
	})
}
func TestCommentsTransportChannelsAuthorAndIsolation(t *testing.T) {
	f := newCommentTransport(t)
	owner := f.client(t, f.ctx)
	team := f.createTeam(t, "CHA")
	is := f.create(t, team.ID)
	nodes := []doc.Node{paragraph(uuid.NewString(), "original\r\nİ 🧭")}
	in := tcCreate(is.ID, nodes)
	key := uuid.NewString()
	raw := transportTool(t, f.ctx, owner, "create_comment", f.w, key, in, "")
	c := transportDecode[comment.Output](t, raw).Comment
	t.Run("sdk_first_then_http_receipt_preserves_original_channel", func(t *testing.T) {
		body := commentMap(t, in)
		delete(body, "issue_id")
		http := f.http(t, "POST", fmt.Sprintf("/v1/workspaces/%s/issues/%s/comments", f.w, is.ID), f.bearer, key, body, 201, "")
		if !bytes.Equal(http, raw) || c.CreatedChannel != "mcp" {
			t.Fatal("cross-channel creation provenance")
		}
		var n int
		if e := f.pool.QueryRow(f.ctx, "SELECT count(*) FROM comment_revisions WHERE comment_id=(SELECT id FROM issue_comments WHERE uuid=$1)", c.ID).Scan(&n); e != nil || n != 1 {
			t.Fatal("receipt reexecuted", e)
		}
	})
	member := actorFixture(t, f.commentFixture, "member")
	reader := f.client(t, member.ctx)
	tokens, _ := jwt.NewManager("team-contract-signing-secret", time.Hour, time.Hour)
	memberBearer, _, _ := tokens.IssueAccess(jwt.AccessInput{UserID: member.actor})
	originalBearer := f.bearer
	t.Run("member_reads_but_cannot_create_or_moderate", func(t *testing.T) {
		f.bearer = memberBearer
		defer func() { f.bearer = originalBearer }()
		got := transportDecode[comment.Output](t, f.op(t, reader, "comments.get", comment.CommentInput{CommentID: c.ID})).Comment
		if got.Capabilities.Manage || got.Capabilities.Delete || got.Capabilities.Restore {
			t.Fatal("member write caps")
		}
		f.rejectOp(t, reader, "comments.create", in, 403, operations.Forbidden)
		f.rejectOp(t, reader, "comments.delete", comment.DeleteInput{CommentID: c.ID, ExpectedRevision: c.Revision}, 403, operations.Forbidden)
	})
	other := actorFixture(t, f.commentFixture, "owner")
	editor := f.client(t, other.ctx)
	otherBearer, _, _ := tokens.IssueAccess(jwt.AccessInput{UserID: other.actor})
	t.Run("moderator_edit_actor_does_not_rewrite_author", func(t *testing.T) {
		f.bearer = otherBearer
		defer func() { f.bearer = originalBearer }()
		c = transportDecode[comment.Output](t, f.op(t, editor, "comments.replace", comment.ReplaceInput{CommentID: c.ID, ExpectedRevision: c.Revision, Format: "plate", SchemaVersion: 1, Content: []doc.Node{paragraph(uuid.NewString(), "moderated")}})).Comment
		if c.AuthorID != f.actor || c.CreatedChannel != "mcp" {
			t.Fatal("author rewritten")
		}
		v := transportDecode[comment.RevisionOutput](t, f.op(t, editor, "comments.get_revision", comment.RevisionInput{CommentID: c.ID, RevisionID: c.Revision}))
		if v.Revision.ActorID != other.actor || v.Revision.Channel != "api" || v.Comment.AuthorID != f.actor {
			t.Fatal("editor audit")
		}
	})
	t.Run("workspace_and_revision_identity_isolation", func(t *testing.T) {
		otherComment := f.makeComment(t, owner, is.ID, nodes)
		f.rejectOp(t, owner, "comments.get_revision", comment.RevisionInput{CommentID: c.ID, RevisionID: otherComment.Revision}, 404, operations.NotFound)
		f.rejectOp(t, owner, "comments.restore", comment.RestoreInput{CommentID: c.ID, ExpectedRevision: c.Revision, RevisionID: otherComment.Revision}, 404, operations.NotFound)
		w := transportDecode[ws.WorkspaceOutput](t, transportTool(t, f.ctx, owner, "create_workspace", uuid.Nil, uuid.NewString(), ws.CreateInput{Name: "Isolated comments"}, "")).Workspace.ID
		old := f.w
		f.w = w
		defer func() { f.w = old }()
		f.rejectOp(t, owner, "comments.get", comment.CommentInput{CommentID: c.ID}, 404, operations.NotFound)
		f.rejectOp(t, owner, "issue_timeline.list", comment.TimelineInput{IssueID: is.ID, Limit: 100}, 404, operations.NotFound)
	})
	t.Run("read_only_and_revoked_real_credentials", func(t *testing.T) {
		mint := func(scopes []string) ws.CreateCredentialOutput {
			return transportDecode[ws.CreateCredentialOutput](t, transportTool(t, f.ctx, owner, "create_workspace_credential", f.w, uuid.NewString(), ws.CreateCredentialInput{Name: "Comment transport", Audience: "mcp", Scopes: scopes, ExpiresAt: time.Now().Add(time.Hour).UTC().Truncate(time.Second)}, ""))
		}
		read := mint([]string{"workspace.read"})
		readCS := f.client(t, ws.WithToolCredential(context.Background(), read.Token))
		transportTool(t, f.ctx, readCS, "delete_comment", f.w, uuid.NewString(), comment.DeleteInput{CommentID: c.ID, ExpectedRevision: c.Revision}, operations.Forbidden)
		write := mint([]string{"workspace.read", "workspace.write"})
		writeCS := f.client(t, ws.WithToolCredential(context.Background(), write.Token))
		createKey := uuid.NewString()
		transportTool(t, f.ctx, writeCS, "create_comment", f.w, createKey, in, "")
		transportTool(t, f.ctx, owner, "revoke_workspace_credential", f.w, uuid.NewString(), ws.RevokeInput{CredentialID: write.Credential.ID}, "")
		transportTool(t, f.ctx, writeCS, "create_comment", f.w, createKey, in, operations.Unauthenticated)
	})
}
func TestCommentsTransportClosedErrorsAndSectionPatches(t *testing.T) {
	f := newCommentTransport(t)
	cs := f.client(t, f.ctx)
	team := f.createTeam(t, "ERR")
	is := f.create(t, team.ID)
	initial := []doc.Node{paragraph(uuid.NewString(), "base")}
	base := commentMap(t, tcCreate(is.ID, initial))
	cases := []struct {
		name   string
		change func(map[string]any)
	}{{"unknown_version", func(v map[string]any) { v["schema_version"] = 2 }}, {"opaque_node", func(v map[string]any) {
		v["content"] = []any{map[string]any{"type": "opaque", "id": uuid.NewString(), "children": []any{map[string]any{"text": "x"}}}}
	}}, {"unknown_property", func(v map[string]any) { n := tcP("x"); n["secret_payload"] = map[string]any{}; v["content"] = []any{n} }}, {"duplicate_uuid_case_identity", func(v map[string]any) {
		n := tcP("a")
		m := tcP("b")
		m["id"] = strings.ToUpper(n["id"].(string))
		v["content"] = []any{n, m}
	}}, {"unsafe_link", func(v map[string]any) {
		link := tcNode("a", map[string]any{"text": "x"})
		link["url"] = "javascript:alert(1)"
		v["content"] = []any{tcNode("p", link)}
	}}, {"cross_workspace_reference", func(v map[string]any) { v["content"] = mentionContent("issue", uuid.New()) }}, {"spoofed_author_channel", func(v map[string]any) { v["created_channel"] = "mcp"; v["author_id"] = uuid.NewString() }}, {"node_budget", func(v map[string]any) {
		nodes := make([]any, doc.MaxNodes+1)
		for i := range nodes {
			nodes[i] = map[string]any{"text": "x"}
		}
		v["content"] = []any{tcNode("p", nodes...)}
	}}, {"deep_malformed_cycle_shape", func(v map[string]any) {
		var n any = map[string]any{"text": "x"}
		for i := 0; i < 48; i++ {
			n = tcNode("p", n)
		}
		v["content"] = []any{n}
	}}, {"canonical_bytes", func(v map[string]any) { v["content"] = []any{tcP(strings.Repeat("界", 180000))} }}}
	cases = append(cases, struct {
		name   string
		change func(map[string]any)
	}{"nul_content", func(v map[string]any) { v["content"] = []any{tcP("bad\x00text")} }})
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			v := commentMap(t, base)
			tc.change(v)
			f.rejectOp(t, cs, "comments.create", v, 400, operations.Validation)
			var n int
			if e := f.pool.QueryRow(f.ctx, "SELECT (SELECT count(*) FROM issue_comments)+(SELECT count(*) FROM operation_receipts WHERE operation_id LIKE 'comments.%')").Scan(&n); e != nil || n != 0 {
				t.Fatal("invalid create mutated", e)
			}
		})
	}
	t.Run("raw_http_limit_before_recursive_validation", func(t *testing.T) {
		v := commentMap(t, base)
		delete(v, "issue_id")
		raw := transportJSON(t, v)
		body := append(append([]byte{}, raw...), bytes.Repeat([]byte(" "), 4<<20)...)
		f.http(t, "POST", fmt.Sprintf("/v1/workspaces/%s/issues/%s/comments", f.w, is.ID), f.bearer, uuid.NewString(), json.RawMessage(body), 400, operations.Validation)
	})
	t.Run("literal_backslash_u0000_is_text", func(t *testing.T) {
		nodes := []doc.Node{paragraph(uuid.NewString(), `literal \u0000 text`)}
		c := f.makeComment(t, cs, is.ID, nodes)
		if !reflect.DeepEqual(c.Content, nodes) {
			t.Fatal("literal escape changed")
		}
	})
	t.Run("section_patch_guard_and_delta", func(t *testing.T) {
		h, p, h2, end := tcNode("h1", map[string]any{"text": "A"}), tcP("one"), tcNode("h1", map[string]any{"text": "B"}), tcP("two")
		nodes := tcContent(t, []any{h, p, h2, end})
		c := f.makeComment(t, cs, is.ID, nodes)
		patch := doc.ReplaceSection{Op: "replace_section", HeadingID: h["id"].(string), ExpectedMemberIDs: []string{p["id"].(string)}, Nodes: tcContent(t, []any{tcP("changed\r\n")})}
		in := comment.PatchInput{CommentID: c.ID, ExpectedRevision: c.Revision, Format: "plate", SchemaVersion: 1, Patches: []doc.Patch{{Value: &patch}}}
		changed := transportDecode[comment.Output](t, f.op(t, cs, "comments.patch", in)).Comment
		if !reflect.DeepEqual(changed.Content[2:], nodes[2:]) {
			t.Fatal("outside section changed")
		}
		in.ExpectedRevision = changed.Revision
		f.rejectOp(t, cs, "comments.patch", in, 400, operations.Validation)
		got := transportDecode[comment.Output](t, f.op(t, cs, "comments.get", comment.CommentInput{CommentID: c.ID})).Comment
		if got.Revision != changed.Revision {
			t.Fatal("rejected stale section mutated")
		}
	})
}
func (f *commentTransport) tcVersions(t *testing.T, ids ...uuid.UUID) []relation.ExpectedVersion {
	t.Helper()
	out := []relation.ExpectedVersion{}
	for _, id := range ids {
		var v int64
		if e := f.pool.QueryRow(f.ctx, "SELECT (document->>'relation_version')::bigint FROM relation_issue_reference WHERE workspace_uuid=$1 AND uuid=$2", f.w, id).Scan(&v); e != nil {
			t.Fatal(e)
		}
		out = append(out, relation.ExpectedVersion{IssueID: id, ExpectedVersion: v})
	}
	return out
}
func TestCommentsTransportTypedTimelineAllAggregates(t *testing.T) {
	f := newCommentTransport(t)
	cs := f.client(t, f.ctx)
	team := f.createTeam(t, "LIN")
	a, b := f.create(t, team.ID), f.create(t, team.ID)
	c := f.makeComment(t, cs, a.ID, []doc.Node{paragraph(uuid.NewString(), "comment")})
	f.op(t, cs, "documents.create", doc.CreateInput{OwnerKind: "issue", OwnerID: a.ID, Format: "plate", SchemaVersion: 1, Content: []doc.Node{paragraph(uuid.NewString(), "document")}})
	for _, kind := range []string{"parent", "duplicate", "blocks", "related"} {
		t.Run("relation_"+kind+"_both_endpoints", func(t *testing.T) {
			var in any
			var id string
			if kind == "parent" || kind == "duplicate" {
				target := b.ID.String()
				in = relation.SetInput{IssueID: a.ID, TargetIssueID: &target, ExpectedVersions: f.tcVersions(t, a.ID, b.ID)}
				id = "issue_relations.set_" + kind
			} else {
				in = relation.LinkInput{IssueID: a.ID, TargetIssueID: b.ID, ExpectedVersions: f.tcVersions(t, a.ID, b.ID)}
				id = map[string]string{"blocks": "issue_relations.link_block", "related": "issue_relations.link_related"}[kind]
			}
			f.op(t, cs, id, in)
			for _, target := range []uuid.UUID{a.ID, b.ID} {
				list := transportDecode[comment.TimelineList](t, f.op(t, cs, "issue_timeline.list", comment.TimelineInput{IssueID: target, Limit: 100}))
				found := false
				for _, row := range list.Items {
					if row.IssueID != target {
						t.Fatal("timeline issue scope")
					}
					if row.Detail.Relation != nil {
						for _, edge := range row.Detail.Relation.Snapshot.ChangedRelations {
							if edge.Kind == kind {
								found = true
							}
						}
					}
				}
				if !found {
					t.Fatal("relation endpoint event absent")
				}
			}
		})
	}
	var file attachment.Attachment
	var upload attachment.Upload
	t.Run("prebind_private_history_excluded_then_safe_bound_lifecycle", func(t *testing.T) {
		upload = transportDecode[attachment.UploadOutput](t, f.op(t, cs, "attachment_uploads.create", attachment.CreateInput{DraftID: uuid.New(), Filename: "public.bin", SizeBytes: 0, SHA256: fmt.Sprintf("%x", sha256.Sum256(nil))})).Upload
		file = transportDecode[attachment.FinalizeOutput](t, f.op(t, cs, "attachment_uploads.finalize", attachment.VersionInput{UploadID: upload.ID, DraftID: upload.DraftID, ExpectedVersion: upload.Version})).Attachment
		before := transportDecode[comment.TimelineList](t, f.op(t, cs, "issue_timeline.list", comment.TimelineInput{IssueID: a.ID, Limit: 100}))
		for _, row := range before.Items {
			if row.Detail.Attachment != nil {
				t.Fatal("private unbound event in timeline")
			}
		}
		file = transportDecode[attachment.AttachmentOutput](t, f.op(t, cs, "attachments.bind", attachment.BindInput{AttachmentID: file.ID, DraftID: upload.DraftID, IssueID: a.ID, ExpectedVersion: file.Version})).Attachment
		f.op(t, cs, "attachments.remove", attachment.RemoveInput{AttachmentID: file.ID, ExpectedVersion: file.Version})
	})
	t.Run("bounded_legacy_unknown_channel_no_opaque_payload", func(t *testing.T) {
		f.sql(t, "WITH e AS(INSERT INTO outbox_events(uuid,workspace_uuid,actor_uuid,event_name,payload) VALUES($1,$2,$3,'issues.legacy_note','{\"opaque_private\":\"do not leak\"}') RETURNING id,uuid,occurred_at) INSERT INTO workspace_history(event_id,uuid,workspace_uuid,actor_uuid,event_name,name,version,occurred_at,resource_type,resource_uuid,issue_uuid) SELECT e.id,e.uuid,$2,$3,'issues.legacy_note','Legacy',1,e.occurred_at,'issues',$4,$4 FROM e", uuid.New(), f.w, f.actor, a.ID)
	})
	t.Run("six_concrete_variants_and_private_context_redacted", func(t *testing.T) {
		raw := f.op(t, cs, "issue_timeline.list", comment.TimelineInput{IssueID: a.ID, Limit: 100})
		list := transportDecode[comment.TimelineList](t, raw)
		counts := map[string]int{}
		for _, row := range list.Items {
			counts[row.Detail.Kind]++
			if row.Detail.Kind == "legacy" && row.Channel != "unknown" {
				t.Fatal("legacy provenance guessed")
			}
		}
		for _, kind := range []string{"issue", "document", "comment", "relation", "attachment", "legacy"} {
			if counts[kind] == 0 {
				t.Fatal("missing concrete variant", kind)
			}
		}
		for _, secret := range []string{"draft_id", "upload_id", upload.DraftID.String(), upload.ID.String()} {
			if bytes.Contains(raw, []byte(secret)) {
				t.Fatal("timeline leaked private upload", secret)
			}
		}
	})
	t.Run("deleted_anchor_and_issue_move_keep_immutable_timeline", func(t *testing.T) {
		first := transportDecode[comment.TimelineList](t, f.op(t, cs, "issue_timeline.list", comment.TimelineInput{IssueID: a.ID, Limit: 2}))
		f.op(t, cs, "comments.delete", comment.DeleteInput{CommentID: c.ID, ExpectedRevision: c.Revision})
		nextTeam := f.createTeam(t, "MOV")
		f.op(t, cs, "issues.move", issue.MoveInput{IssueID: a.ID, ExpectedVersion: a.Version, TeamID: nextTeam.ID, StatusID: f.status(t, nextTeam.ID), LabelIDs: []string{}})
		page := transportDecode[comment.TimelineList](t, f.op(t, cs, "issue_timeline.list", comment.TimelineInput{IssueID: a.ID, Limit: 100, After: first.NextCursor}))
		if len(page.Items) == 0 {
			t.Fatal("anchor lost after move/delete")
		}
		other := f.create(t, team.ID)
		f.rejectOp(t, cs, "issue_timeline.list", comment.TimelineInput{IssueID: other.ID, Limit: 2, After: first.NextCursor}, 400, operations.Validation)
	})
}

func TestCommentsTransportCompleteCursorTraversal(t *testing.T) {
	f := newCommentTransport(t)
	cs := f.client(t, f.ctx)
	team := f.createTeam(t, "PAG")
	is := f.create(t, team.ID)
	comments := []comment.Comment{}
	for n := 0; n < 103; n++ {
		comments = append(comments, f.makeComment(t, cs, is.ID, mentionContent("user", f.actor)))
	}
	firstPage := transportDecode[comment.List](t, f.op(t, cs, "comments.list", comment.ListInput{IssueID: is.ID, IncludeDeleted: false, Limit: 10}))
	// Keep the opaque cursor after tombstoning its last row.
	deleted := transportDecode[comment.Output](t, f.op(t, cs, "comments.delete", comment.DeleteInput{CommentID: comments[9].ID, ExpectedRevision: comments[9].Revision})).Comment
	c := comments[0]
	for n := 0; n < 102; n++ {
		c = transportDecode[comment.Output](t, f.op(t, cs, "comments.replace", comment.ReplaceInput{CommentID: c.ID, ExpectedRevision: c.Revision, Format: "plate", SchemaVersion: 1, Content: mentionContent("user", f.actor)})).Comment
	}
	t.Run("comments_after_deleted_anchor", func(t *testing.T) {
		seen := map[uuid.UUID]bool{}
		after := ""
		count := 0
		for {
			out := transportDecode[comment.List](t, f.op(t, cs, "comments.list", comment.ListInput{IssueID: is.ID, IncludeDeleted: true, Limit: 10, After: after}))
			for _, row := range out.Items {
				if seen[row.ID] {
					t.Fatal("repeated comment")
				}
				seen[row.ID] = true
				count++
			}
			if !out.HasMore {
				break
			}
			if out.NextCursor == "" || out.NextCursor == after {
				t.Fatal("stalled cursor")
			}
			after = out.NextCursor
		}
		if count != 103 || !seen[deleted.ID] {
			t.Fatalf("comments %d", count)
		}
		out := transportDecode[comment.List](t, f.op(t, cs, "comments.list", comment.ListInput{IssueID: is.ID, IncludeDeleted: false, Limit: 100, After: firstPage.NextCursor}))
		if len(out.Items) == 0 {
			t.Fatal("deleted anchor lost")
		}
	})
	t.Run("historical_revisions_over_100", func(t *testing.T) {
		seen := map[uuid.UUID]bool{}
		after := ""
		for {
			out := transportDecode[comment.RevisionList](t, f.op(t, cs, "comments.revisions", comment.RevisionsInput{CommentID: c.ID, Limit: 17, After: after}))
			for _, r := range out.Items {
				if seen[r.ID] {
					t.Fatal("repeated revision")
				}
				seen[r.ID] = true
			}
			if !out.HasMore {
				break
			}
			if out.NextCursor == after || out.NextCursor == "" {
				t.Fatal("stalled revisions")
			}
			after = out.NextCursor
		}
		if len(seen) != 103 {
			t.Fatalf("revisions %d", len(seen))
		}
	})
	t.Run("backlinks_over_100", func(t *testing.T) {
		seen := map[uuid.UUID]bool{}
		after := ""
		for {
			out := transportDecode[comment.BacklinkList](t, f.op(t, cs, "comments.backlinks", comment.BacklinksInput{TargetKind: "user", TargetID: f.actor, Limit: 13, After: after}))
			for _, r := range out.Items {
				if seen[r.ID] {
					t.Fatal("repeated backlink")
				}
				seen[r.ID] = true
			}
			if !out.HasMore {
				break
			}
			if out.NextCursor == after || out.NextCursor == "" {
				t.Fatal("stalled backlinks")
			}
			after = out.NextCursor
		}
		if len(seen) != 102 {
			t.Fatalf("backlinks %d", len(seen))
		}
	})
	t.Run("timeline_over_200_no_duplicates", func(t *testing.T) {
		seen := map[uuid.UUID]bool{}
		after := ""
		count := 0
		for {
			out := transportDecode[comment.TimelineList](t, f.op(t, cs, "issue_timeline.list", comment.TimelineInput{IssueID: is.ID, Limit: 19, After: after}))
			for _, r := range out.Items {
				if seen[r.ID] {
					t.Fatal("repeated timeline")
				}
				seen[r.ID] = true
				if r.Detail.Kind == "comment" {
					count++
				}
			}
			if !out.HasMore {
				break
			}
			if out.NextCursor == after || out.NextCursor == "" {
				t.Fatal("stalled timeline")
			}
			after = out.NextCursor
		}
		if count != 206 {
			t.Fatalf("comment timeline %d", count)
		}
	})
}

func TestCommentsTransportRollbackAndArchivedEscape(t *testing.T) {
	f := newCommentTransport(t)
	cs := f.client(t, f.ctx)
	is := f.create(t, f.createTeam(t, "ATM").ID)
	c := f.makeComment(t, cs, is.ID, []doc.Node{paragraph(uuid.NewString(), "before")})
	original := c
	counts := func() [6]int64 {
		t.Helper()
		var a [6]int64
		if e := f.pool.QueryRow(f.ctx, "SELECT (SELECT count(*) FROM issue_comments),(SELECT count(*) FROM comment_revisions),(SELECT count(*) FROM comment_references),(SELECT count(*) FROM workspace_history),(SELECT count(*) FROM outbox_events),(SELECT count(*) FROM operation_receipts)").Scan(&a[0], &a[1], &a[2], &a[3], &a[4], &a[5]); e != nil {
			t.Fatal(e)
		}
		return a
	}
	t.Run("actual_http_sdk_failure_rolls_back_then_same_key_retries", func(t *testing.T) {
		before := counts()
		if _, e := f.pool.Exec(f.ctx, "CREATE FUNCTION fail_transport_history() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'forced'; END $$; CREATE TRIGGER fail_transport_history BEFORE INSERT ON workspace_history FOR EACH ROW EXECUTE FUNCTION fail_transport_history()"); e != nil {
			t.Fatal(e)
		}
		in := comment.ReplaceInput{CommentID: c.ID, ExpectedRevision: c.Revision, Format: "plate", SchemaVersion: 1, Content: mentionContent("user", f.actor)}
		key := uuid.NewString()
		v := commentMap(t, in)
		delete(v, "comment_id")
		f.http(t, "POST", fmt.Sprintf("/v1/workspaces/%s/comments/%s/replace", f.w, c.ID), f.bearer, key, v, 500, operations.Internal)
		transportTool(t, f.ctx, cs, "replace_comment", f.w, key, in, operations.Internal)
		if counts() != before {
			t.Fatal("failed operation left durable effects/receipt")
		}
		got := transportDecode[comment.Output](t, f.op(t, cs, "comments.get", comment.CommentInput{CommentID: c.ID})).Comment
		if got.Revision != c.Revision {
			t.Fatal("failed write advanced head")
		}
		if _, e := f.pool.Exec(f.ctx, "DROP TRIGGER fail_transport_history ON workspace_history; DROP FUNCTION fail_transport_history()"); e != nil {
			t.Fatal(e)
		}
		c = transportDecode[comment.Output](t, f.invoke(t, cs, "replace_comment", key, commentMap(t, in))).Comment
	})
	t.Run("archived_mentions_and_owner_delete_escape", func(t *testing.T) {
		target := f.create(t, f.createTeam(t, "REF").ID)
		f.op(t, cs, "issues.archive", issue.VersionInput{IssueID: target.ID, ExpectedVersion: target.Version})
		c = transportDecode[comment.Output](t, f.op(t, cs, "comments.replace", comment.ReplaceInput{CommentID: c.ID, ExpectedRevision: c.Revision, Format: "plate", SchemaVersion: 1, Content: mentionContent("issue", target.ID)})).Comment
		f.op(t, cs, "issues.archive", issue.VersionInput{IssueID: is.ID, ExpectedVersion: is.Version})
		got := transportDecode[comment.Output](t, f.op(t, cs, "comments.get", comment.CommentInput{CommentID: c.ID})).Comment
		if !got.Capabilities.Delete || got.Capabilities.Restore || got.Capabilities.Replace {
			t.Fatal("archived owner capabilities")
		}
		f.rejectOp(t, cs, "comments.create", tcCreate(is.ID, c.Content), 409, operations.Conflict)
		f.rejectOp(t, cs, "comments.replace", comment.ReplaceInput{CommentID: c.ID, ExpectedRevision: c.Revision, Format: "plate", SchemaVersion: 1, Content: c.Content}, 409, operations.Conflict)
		c = transportDecode[comment.Output](t, f.op(t, cs, "comments.delete", comment.DeleteInput{CommentID: c.ID, ExpectedRevision: c.Revision})).Comment
		if !c.Deleted {
			t.Fatal("delete escape")
		}
		f.rejectOp(t, cs, "comments.restore", comment.RestoreInput{CommentID: c.ID, ExpectedRevision: c.Revision, RevisionID: original.Revision}, 409, operations.Conflict)
		old := transportDecode[comment.RevisionOutput](t, f.op(t, cs, "comments.get_revision", comment.RevisionInput{CommentID: c.ID, RevisionID: original.Revision}))
		if old.Comment.Deleted || !reflect.DeepEqual(old.Comment.Content, original.Content) {
			t.Fatal("archive changed historical content")
		}
	})
}
