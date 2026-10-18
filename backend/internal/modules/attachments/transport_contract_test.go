package attachments_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/url"

	"encoding/base64"
	attachment "github.com/furkanmeclis/traccia/backend/internal/modules/attachments/usecase"
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

type attachmentTransport struct {
	attachmentFixture
	mux      *http.ServeMux
	bearer   string
	document contractcoverage.Object
}

var transportCatalog sync.Map

func newAttachmentTransport(t *testing.T, options ...attachment.Options) *attachmentTransport {
	t.Helper()
	var f attachmentFixture
	if len(options) > 0 {
		f = newAttachmentFixtureOptions(t, options[0], nil)
	} else {
		f = newAttachmentFixture(t)
	}
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
	return &attachmentTransport{attachmentFixture: f, mux: mux, bearer: bearer, document: doc}
}
func transportJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
func (f *attachmentTransport) http(t *testing.T, method, path, token, key string, input any, status int, code operations.Code) []byte {
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
func (f *attachmentTransport) client(t *testing.T, serverCtx context.Context) *mcp.ClientSession {
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
			t.Fatalf("%s code=%s want=%s error=%s", name, e.Code, code, raw)
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
func (f *attachmentTransport) invoke(t *testing.T, cs *mcp.ClientSession, name, key string, input map[string]any) []byte {
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
		for _, pair := range [][2]string{{"issueID", "issue_id"}, {"uploadID", "upload_id"}, {"attachmentID", "attachment_id"}} {
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
func (f *attachmentTransport) sql(t *testing.T, q string, args ...any) {
	t.Helper()
	if _, e := f.pool.Exec(f.ctx, q, args...); e != nil {
		t.Fatal(e)
	}
}

func attachmentMap(t *testing.T, v any) map[string]any { return issueMap(t, v) }
func (f *attachmentTransport) op(t *testing.T, cs *mcp.ClientSession, id string, in any) []byte {
	t.Helper()
	for _, c := range f.registry.Catalog() {
		if c.Metadata.ID == id {
			key := ""
			if c.Metadata.Effect == operations.Write {
				key = uuid.NewString()
			}
			return f.invoke(t, cs, c.Metadata.MCPName, key, attachmentMap(t, in))
		}
	}
	t.Fatal("missing operation", id)
	return nil
}
func (f *attachmentTransport) rejectOp(t *testing.T, cs *mcp.ClientSession, id string, in any, status int, code operations.Code) {
	t.Helper()
	for _, c := range f.registry.Catalog() {
		if c.Metadata.ID != id {
			continue
		}
		body := attachmentMap(t, in)
		path := strings.ReplaceAll(c.Metadata.HTTPPath, "{workspaceID}", f.w.String())
		for _, p := range [][2]string{{"uploadID", "upload_id"}, {"attachmentID", "attachment_id"}, {"issueID", "issue_id"}} {
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
func (f *attachmentTransport) begin(t *testing.T, cs *mcp.ClientSession, data []byte) attachment.Upload {
	return transportDecode[attachment.UploadOutput](t, f.op(t, cs, "attachment_uploads.create", attachment.CreateInput{DraftID: uuid.New(), Filename: "../İstanbul\r\n🧭.bin", ContentType: "text/html", SizeBytes: int64(len(data)), SHA256: sha(data)})).Upload
}
func chunkInput(u attachment.Upload, data []byte) attachment.ChunkInput {
	return attachment.ChunkInput{UploadID: u.ID, DraftID: u.DraftID, ExpectedVersion: u.Version, Offset: u.NextOffset, TotalBytes: u.SizeBytes, DataBase64: base64.StdEncoding.EncodeToString(data), ChunkSHA256: sha(data)}
}
func (f *attachmentTransport) finish(t *testing.T, cs *mcp.ClientSession, u attachment.Upload, data []byte) attachment.FinalizeOutput {
	t.Helper()
	for off := 0; off < len(data); {
		end := min(len(data), off+int(u.ChunkBytes))
		u = transportDecode[attachment.UploadOutput](t, f.op(t, cs, "attachment_uploads.chunk", chunkInput(u, data[off:end]))).Upload
		off = end
	}
	return transportDecode[attachment.FinalizeOutput](t, f.op(t, cs, "attachment_uploads.finalize", attachment.VersionInput{UploadID: u.ID, DraftID: u.DraftID, ExpectedVersion: u.Version}))
}
func (f *attachmentTransport) receive(t *testing.T, cs *mcp.ClientSession, file attachment.Attachment, limit int32) []byte {
	t.Helper()
	var out []byte
	draft := ""
	if file.DraftID != nil {
		draft = file.DraftID.String()
	}
	for n := 0; n < 10000; n++ {
		part := transportDecode[attachment.DownloadOutput](t, f.op(t, cs, "attachments.download_chunk", attachment.DownloadInput{AttachmentID: file.ID, DraftID: draft, FileRevision: file.FileRevision, Offset: int64(len(out)), Limit: limit}))
		data, e := base64.StdEncoding.Strict().DecodeString(part.DataBase64)
		if e != nil || part.DataBase64 != base64.StdEncoding.EncodeToString(data) || part.AttachmentID != file.ID || part.FileRevision != file.FileRevision || part.SizeBytes != file.SizeBytes || part.SHA256 != file.SHA256 || part.Offset != int64(len(out)) || part.NextOffset != int64(len(out)+len(data)) || sha(data) != part.ChunkSHA256 || len(data) > int(limit) || part.ContentType != "application/octet-stream" || !strings.HasPrefix(part.ContentDisposition, "attachment;") || strings.ContainsAny(part.DownloadFilename, "/\\\r\n") {
			t.Fatal("download frame integrity")
		}
		out = append(out, data...)
		if part.EOF {
			if int64(len(out)) != file.SizeBytes || sha(out) != file.SHA256 {
				t.Fatal("whole download integrity")
			}
			return out
		}
		if len(data) == 0 {
			t.Fatal("download stalled")
		}
	}
	t.Fatal("download did not terminate")
	return nil
}
func TestAttachmentsTransportInventoryAndBinaryLifecycle(t *testing.T) {
	f := newAttachmentTransport(t)
	cs := f.client(t, f.ctx)
	catalog := f.registry.Catalog()
	n, writes := 0, 0
	for _, c := range catalog {
		if strings.HasPrefix(c.Metadata.ID, "attachment") {
			n++
			if c.Metadata.Effect == operations.Write {
				writes++
			}
		}
	}
	if len(catalog) != 114 || n != 14 || writes != 6 {
		t.Fatalf("inventory %d/%d/%d", len(catalog), n, writes)
	}
	t.Run("configured_limits", func(t *testing.T) {
		v := transportDecode[attachment.LimitsOutput](t, f.op(t, cs, "attachment_uploads.config", attachment.Empty{}))
		if v.Limits.MaxFileBytes != 256<<20 || v.Limits.ChunkBytes != 256<<10 || !v.Capabilities.Manage {
			t.Fatal("limits")
		}
	})
	team := f.createTeam(t, "ATP")
	is := f.create(t, team.ID)
	for _, size := range []int{0, 256 << 10, (256 << 10) + 257} {
		t.Run(fmt.Sprintf("binary_%d", size), func(t *testing.T) {
			data := make([]byte, size)
			for i := range data {
				data[i] = byte(i)
			}
			u := f.begin(t, cs, data)
			current := transportDecode[attachment.UploadOutput](t, f.op(t, cs, "attachment_uploads.get", attachment.UploadInput{UploadID: u.ID, DraftID: u.DraftID})).Upload
			if current.ID != u.ID || current.NextOffset != 0 {
				t.Fatal("initial state")
			}
			done := f.finish(t, cs, u, data)
			file := done.Attachment
			if file.DraftID == nil || file.UploadID == nil || !file.Capabilities.History {
				t.Fatal("private context")
			}
			if !bytes.Equal(f.receive(t, cs, file, 131071), data) {
				t.Fatal("binary mismatch")
			}
			own := transportDecode[attachment.AttachmentOutput](t, f.op(t, cs, "attachments.get", attachment.AttachmentInput{AttachmentID: file.ID, DraftID: u.DraftID.String()})).Attachment
			if own.FileRevision != file.FileRevision || own.Filename != u.Filename {
				t.Fatal("metadata fidelity")
			}
			bound := transportDecode[attachment.AttachmentOutput](t, f.op(t, cs, "attachments.bind", attachment.BindInput{AttachmentID: file.ID, DraftID: u.DraftID, IssueID: is.ID, ExpectedVersion: file.Version})).Attachment
			if bound.Version != file.Version+1 || bound.FileRevision != file.FileRevision {
				t.Fatal("bind identity")
			}
			list := transportDecode[attachment.AttachmentList](t, f.op(t, cs, "attachments.list", attachment.ListInput{IssueID: is.ID, Limit: 100}))
			if len(list.Items) == 0 {
				t.Fatal("missing bound file")
			}
			hist := transportDecode[attachment.HistoryList](t, f.op(t, cs, "attachments.history", attachment.HistoryInput{AttachmentID: file.ID, DraftID: u.DraftID.String(), Limit: 100}))
			if len(hist.Items) != 2 {
				t.Fatal("history lifecycle")
			}
			uh := transportDecode[attachment.UploadHistoryList](t, f.op(t, cs, "attachment_uploads.history", attachment.UploadHistoryInput{UploadID: u.ID, DraftID: u.DraftID, Limit: 100}))
			if len(uh.Items) < 3 {
				t.Fatal("upload history")
			}
			removed := transportDecode[attachment.AttachmentOutput](t, f.op(t, cs, "attachments.remove", attachment.RemoveInput{AttachmentID: file.ID, ExpectedVersion: bound.Version})).Attachment
			if !removed.Removed || removed.DraftID != nil || removed.UploadID != nil || removed.Capabilities.History {
				t.Fatal("removed public view")
			}
			f.rejectOp(t, cs, "attachments.download_chunk", attachment.DownloadInput{AttachmentID: file.ID, DraftID: u.DraftID.String(), FileRevision: file.FileRevision, Limit: 1}, 409, operations.Conflict)
		})
	}
	t.Run("abort_and_complete_upload_list", func(t *testing.T) {
		u := f.begin(t, cs, []byte{1})
		ab := transportDecode[attachment.UploadOutput](t, f.op(t, cs, "attachment_uploads.abort", attachment.VersionInput{UploadID: u.ID, DraftID: u.DraftID, ExpectedVersion: u.Version})).Upload
		if ab.State != "aborted" {
			t.Fatal("abort")
		}
		list := transportDecode[attachment.UploadList](t, f.op(t, cs, "attachment_uploads.list", attachment.UploadListInput{Limit: 100}))
		if len(list.Items) != 4 {
			t.Fatalf("upload list=%d", len(list.Items))
		}
	})
}
func TestAttachmentsTransportStrictErrorsAndAtomicResume(t *testing.T) {
	f := newAttachmentTransport(t)
	cs := f.client(t, f.ctx)
	data := bytes.Repeat([]byte{0, 255, 13, 10}, int(attachment.MaxChunkBytes)/4)
	u := f.begin(t, cs, append(append([]byte{}, data...), 7))
	good := chunkInput(u, data)
	cases := []struct {
		name   string
		change func(*attachment.ChunkInput)
		code   operations.Code
		status int
	}{
		{"gap", func(v *attachment.ChunkInput) { v.Offset = 1 }, operations.Validation, 400}, {"wrong_total", func(v *attachment.ChunkInput) { v.TotalBytes++ }, operations.Validation, 400}, {"wrong_hash", func(v *attachment.ChunkInput) { v.ChunkSHA256 = sha([]byte{2}) }, operations.Validation, 400}, {"short_chunk", func(v *attachment.ChunkInput) { v.DataBase64 = "AA=="; v.ChunkSHA256 = sha([]byte{0}) }, operations.Validation, 400}, {"trailing_decoded_byte", func(v *attachment.ChunkInput) {
			v.DataBase64 = base64.StdEncoding.EncodeToString(append(append([]byte{}, data...), 1))
		}, operations.Validation, 400}, {"noncanonical_base64", func(v *attachment.ChunkInput) { v.DataBase64 = "AB=="; v.ChunkSHA256 = sha([]byte{0}) }, operations.Validation, 400}, {"stale_version", func(v *attachment.ChunkInput) { v.ExpectedVersion = 2 }, operations.VersionConflict, 409}, {"wrong_draft", func(v *attachment.ChunkInput) { v.DraftID = uuid.New() }, operations.NotFound, 404}}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := good
			tc.change(&in)
			f.rejectOp(t, cs, "attachment_uploads.chunk", in, tc.status, tc.code)
			current := transportDecode[attachment.UploadOutput](t, f.op(t, cs, "attachment_uploads.get", attachment.UploadInput{UploadID: u.ID, DraftID: u.DraftID})).Upload
			if current.NextOffset != 0 || current.Version != 1 {
				t.Fatal("error changed progress")
			}
		})
	}
	t.Run("incomplete_finalize", func(t *testing.T) {
		f.rejectOp(t, cs, "attachment_uploads.finalize", attachment.VersionInput{UploadID: u.ID, DraftID: u.DraftID, ExpectedVersion: u.Version}, 409, operations.Conflict)
	})
	t.Run("replay_and_changed_input", func(t *testing.T) {
		key := uuid.NewString()
		first := f.invoke(t, cs, "upload_attachment_chunk", key, attachmentMap(t, good))
		replay := f.invoke(t, cs, "upload_attachment_chunk", key, attachmentMap(t, good))
		if !bytes.Equal(first, replay) {
			t.Fatal("receipt changed")
		}
		bad := good
		bad.Offset = 1
		transportTool(t, f.ctx, cs, "upload_attachment_chunk", f.w, key, bad, operations.IdempotencyConflict)
		u = transportDecode[attachment.UploadOutput](t, first).Upload
	})
	t.Run("resume_then_immutable_revision", func(t *testing.T) {
		done := f.finish(t, cs, u, []byte{7})
		file := done.Attachment
		if !bytes.Equal(f.receive(t, cs, file, 65535), append(append([]byte{}, data...), 7)) {
			t.Fatal("resumed bytes")
		}
		f.rejectOp(t, cs, "attachments.download_chunk", attachment.DownloadInput{AttachmentID: file.ID, DraftID: u.DraftID.String(), FileRevision: uuid.New(), Limit: 1}, 409, operations.Conflict)
	})
	t.Run("closed_create_and_limits", func(t *testing.T) {
		base := attachmentMap(t, attachment.CreateInput{DraftID: uuid.New(), Filename: "x", SizeBytes: 0, SHA256: sha(nil)})
		for _, extra := range []map[string]any{{"object_key": "evil"}, {"size_bytes": attachment.MaxFileBytes + 1}, {"filename": "x\x00"}, {"sha256": strings.Repeat("A", 64)}} {
			in := attachmentMap(t, base)
			for k, v := range extra {
				in[k] = v
			}
			f.rejectOp(t, cs, "attachment_uploads.create", in, 400, operations.Validation)
		}
	})
}
func TestAttachmentsTransportPrivateHistoryAndPublicViews(t *testing.T) {
	f := newAttachmentTransport(t)
	owner := f.client(t, f.ctx)
	data := []byte("private\x00binary\r\nİ🧭")
	u := f.begin(t, owner, data)
	file := f.finish(t, owner, u, data).Attachment
	other := uuid.New()
	f.sql(t, "INSERT INTO users(uuid,email,password_hash,name,surname,status) VALUES($1,$2,'unused','Other','Reader','active')", other, other.String()+"@example.test")
	f.sql(t, "INSERT INTO workspace_members(workspace_id,user_id,role,active) SELECT w.id,u.id,'owner',true FROM workspaces w,users u WHERE w.uuid=$1 AND u.uuid=$2", f.w, other)
	otherCtx := authctx.WithPrincipal(f.ctx, authctx.Principal{UserID: other})
	reader := f.client(t, otherCtx)
	tokens, _ := jwt.NewManager("team-contract-signing-secret", time.Hour, time.Hour)
	otherBearer, _, e := tokens.IssueAccess(jwt.AccessInput{UserID: other})
	if e != nil {
		t.Fatal(e)
	}
	ownerBearer := f.bearer
	t.Run("unbound_other_actor_and_wrong_draft", func(t *testing.T) {
		f.rejectOp(t, owner, "attachments.get", attachment.AttachmentInput{AttachmentID: file.ID, DraftID: uuid.NewString()}, 404, operations.NotFound)
		f.bearer = otherBearer
		defer func() { f.bearer = ownerBearer }()
		f.rejectOp(t, reader, "attachments.get", attachment.AttachmentInput{AttachmentID: file.ID, DraftID: u.DraftID.String()}, 404, operations.NotFound)
		f.rejectOp(t, reader, "attachment_uploads.get", attachment.UploadInput{UploadID: u.ID, DraftID: u.DraftID}, 404, operations.NotFound)
		f.rejectOp(t, reader, "attachments.history", attachment.HistoryInput{AttachmentID: file.ID, DraftID: u.DraftID.String(), Limit: 100}, 404, operations.NotFound)
	})
	t.Run("unbound_activity_is_lossless_only_in_private_history", func(t *testing.T) {
		rows, e := f.pool.Query(f.ctx, "SELECT payload::text FROM outbox_events WHERE workspace_uuid=$1 AND event_name LIKE 'attachment%'", f.w)
		if e != nil {
			t.Fatal(e)
		}
		defer rows.Close()
		for rows.Next() {
			var raw string
			if e := rows.Scan(&raw); e != nil {
				t.Fatal(e)
			}
			for _, secret := range []string{u.DraftID.String(), u.SHA256, "filename", "snapshot"} {
				if strings.Contains(raw, secret) {
					t.Fatal("shared event private leak", secret)
				}
			}
		}
		if e := rows.Err(); e != nil {
			t.Fatal(e)
		}
		hist := transportDecode[attachment.HistoryList](t, f.op(t, owner, "attachments.history", attachment.HistoryInput{AttachmentID: file.ID, DraftID: u.DraftID.String(), Limit: 100}))
		if len(hist.Items) != 1 || hist.Items[0].Snapshot.DraftID == nil || *hist.Items[0].Snapshot.DraftID != u.DraftID || hist.Items[0].Snapshot.Filename != u.Filename {
			t.Fatal("private snapshot lost")
		}
	})
	team := f.createTeam(t, "PVT")
	is := f.create(t, team.ID)
	bound := transportDecode[attachment.AttachmentOutput](t, f.op(t, owner, "attachments.bind", attachment.BindInput{AttachmentID: file.ID, DraftID: u.DraftID, IssueID: is.ID, ExpectedVersion: file.Version})).Attachment
	t.Run("bound_public_reads_hide_context_and_history", func(t *testing.T) {
		f.bearer = otherBearer
		defer func() { f.bearer = ownerBearer }()
		public := transportDecode[attachment.AttachmentOutput](t, f.op(t, reader, "attachments.get", attachment.AttachmentInput{AttachmentID: file.ID})).Attachment
		if public.UploadID != nil || public.DraftID != nil || public.Capabilities.History || public.SHA256 != file.SHA256 {
			t.Fatal("unsafe public view")
		}
		page := transportDecode[attachment.AttachmentList](t, f.op(t, reader, "attachments.list", attachment.ListInput{IssueID: is.ID, Limit: 100}))
		if len(page.Items) != 1 || page.Items[0].DraftID != nil || page.Items[0].UploadID != nil || page.Items[0].Capabilities.History {
			t.Fatal("unsafe public list")
		}
		if !bytes.Equal(f.receive(t, reader, public, 7), data) {
			t.Fatal("bound read bytes")
		}
		f.rejectOp(t, reader, "attachments.history", attachment.HistoryInput{AttachmentID: file.ID, DraftID: u.DraftID.String(), Limit: 100}, 404, operations.NotFound)
		f.rejectOp(t, reader, "attachments.bind", attachment.BindInput{AttachmentID: file.ID, DraftID: u.DraftID, IssueID: is.ID, ExpectedVersion: bound.Version}, 404, operations.NotFound)
	})
	t.Run("owner_needs_correct_draft_after_binding", func(t *testing.T) {
		f.rejectOp(t, owner, "attachments.history", attachment.HistoryInput{AttachmentID: file.ID, DraftID: uuid.NewString(), Limit: 100}, 404, operations.NotFound)
		hist := transportDecode[attachment.HistoryList](t, f.op(t, owner, "attachments.history", attachment.HistoryInput{AttachmentID: file.ID, DraftID: u.DraftID.String(), Limit: 100}))
		if len(hist.Items) != 2 {
			t.Fatal("complete history")
		}
		for _, h := range hist.Items {
			if h.Snapshot.DraftID == nil || *h.Snapshot.DraftID != u.DraftID || h.Snapshot.UploadID == nil || *h.Snapshot.UploadID != u.ID {
				t.Fatal("owner snapshot redacted")
			}
		}
	})
	t.Run("cross_workspace_lookup", func(t *testing.T) {
		otherW := transportDecode[ws.WorkspaceOutput](t, transportTool(t, f.ctx, owner, "create_workspace", uuid.Nil, uuid.NewString(), ws.CreateInput{Name: "Other attachment workspace"}, "")).Workspace.ID
		current := f.w
		f.w = otherW
		defer func() { f.w = current }()
		f.rejectOp(t, owner, "attachments.get", attachment.AttachmentInput{AttachmentID: file.ID, DraftID: u.DraftID.String()}, 404, operations.NotFound)
	})
	t.Run("safe_manager_remove_and_atomic_metadata", func(t *testing.T) {
		f.bearer = otherBearer
		defer func() { f.bearer = ownerBearer }()
		removed := transportDecode[attachment.AttachmentOutput](t, f.op(t, reader, "attachments.remove", attachment.RemoveInput{AttachmentID: file.ID, ExpectedVersion: bound.Version})).Attachment
		if removed.UploadID != nil || removed.DraftID != nil || removed.FileRevision != file.FileRevision {
			t.Fatal("remove private leak/identity")
		}
		var version int64
		if e := f.pool.QueryRow(f.ctx, "SELECT version FROM issues WHERE uuid=$1", is.ID).Scan(&version); e != nil || version != is.Version {
			t.Fatal("attachment changed metadata version", e)
		}
	})
}
func TestAttachmentsTransportCompleteCursors(t *testing.T) {
	options := attachment.DefaultOptions()
	options.ChunkBytes = 1024
	options.MaxFileBytes = 4 << 20
	f := newAttachmentTransport(t, options)
	cs := f.client(t, f.ctx)
	team := f.createTeam(t, "PAG")
	is := f.create(t, team.ID)
	for i := 0; i < 103; i++ {
		u := f.upload(t, nil)
		file := f.complete(t, u, nil).Attachment
		result[attachment.AttachmentOutput](t, f.attachmentFixture, "attachments.bind", attachment.BindInput{AttachmentID: file.ID, DraftID: u.DraftID, IssueID: is.ID, ExpectedVersion: file.Version})
	}
	t.Run("all_upload_pages", func(t *testing.T) {
		seen := map[uuid.UUID]bool{}
		after := ""
		for pages := 0; pages < 20; pages++ {
			p := transportDecode[attachment.UploadList](t, f.op(t, cs, "attachment_uploads.list", attachment.UploadListInput{Limit: 17, After: after}))
			for _, v := range p.Items {
				if seen[v.ID] {
					t.Fatal("duplicate upload")
				}
				seen[v.ID] = true
			}
			if !p.HasMore {
				break
			}
			if p.NextCursor == "" || p.NextCursor == after {
				t.Fatal("cursor stalled")
			}
			after = p.NextCursor
		}
		if len(seen) != 103 {
			t.Fatalf("upload count%d", len(seen))
		}
	})
	t.Run("all_files_after_removed_anchor", func(t *testing.T) {
		seen := map[uuid.UUID]bool{}
		after := ""
		first := true
		for pages := 0; pages < 20; pages++ {
			p := transportDecode[attachment.AttachmentList](t, f.op(t, cs, "attachments.list", attachment.ListInput{IssueID: is.ID, Limit: 19, After: after}))
			for _, v := range p.Items {
				if seen[v.ID] {
					t.Fatal("duplicate file")
				}
				seen[v.ID] = true
				if v.UploadID != nil || v.DraftID != nil {
					t.Fatal("list context leak")
				}
			}
			if !p.HasMore {
				break
			}
			after = p.NextCursor
			if first {
				anchor := p.Items[len(p.Items)-1]
				f.op(t, cs, "attachments.remove", attachment.RemoveInput{AttachmentID: anchor.ID, ExpectedVersion: anchor.Version})
				first = false
			}
		}
		if len(seen) != 103 {
			t.Fatalf("file count%d", len(seen))
		}
	})
	t.Run("over100_history_rows", func(t *testing.T) {
		data := bytes.Repeat([]byte{0, 13, 10, 255}, 106*256)
		u := f.begin(t, cs, data)
		for off := 0; off < len(data); off += 1024 {
			u = transportDecode[attachment.UploadOutput](t, f.op(t, cs, "attachment_uploads.chunk", chunkInput(u, data[off:off+1024]))).Upload
		}
		seen := map[uuid.UUID]bool{}
		after := ""
		for pages := 0; pages < 20; pages++ {
			p := transportDecode[attachment.UploadHistoryList](t, f.op(t, cs, "attachment_uploads.history", attachment.UploadHistoryInput{UploadID: u.ID, DraftID: u.DraftID, Limit: 13, After: after}))
			for _, v := range p.Items {
				if seen[v.ID] {
					t.Fatal("duplicate history")
				}
				seen[v.ID] = true
			}
			if !p.HasMore {
				break
			}
			after = p.NextCursor
		}
		if len(seen) != 107 {
			t.Fatalf("history count%d", len(seen))
		}
		f.rejectOp(t, cs, "attachment_uploads.history", attachment.UploadHistoryInput{UploadID: u.ID, DraftID: u.DraftID, Limit: 13, After: "not-cursor"}, 400, operations.Validation)
	})
}
func TestAttachmentsTransportLiveCredentialsAndExpiry(t *testing.T) {
	f := newAttachmentTransport(t)
	human := f.client(t, f.ctx)
	mint := func(audience string, scopes []string) ws.CreateCredentialOutput {
		return transportDecode[ws.CreateCredentialOutput](t, transportTool(t, f.ctx, human, "create_workspace_credential", f.w, uuid.NewString(), ws.CreateCredentialInput{Name: "Attachment transport", Audience: audience, Scopes: scopes, ExpiresAt: time.Now().Add(time.Hour).UTC().Truncate(time.Second)}, ""))
	}
	credential := mint("mcp", []string{"workspace.read", "workspace.write"})
	writer := f.client(t, ws.WithToolCredential(context.Background(), credential.Token))
	u := f.begin(t, writer, []byte{0, 255, 10})
	file := f.finish(t, writer, u, []byte{0, 255, 10}).Attachment
	t.Run("same_token_is_live_between_download_chunks", func(t *testing.T) {
		transportTool(t, f.ctx, writer, "download_attachment_chunk", f.w, "", attachment.DownloadInput{AttachmentID: file.ID, DraftID: u.DraftID.String(), FileRevision: file.FileRevision, Limit: 1}, "")
		transportTool(t, f.ctx, human, "revoke_workspace_credential", f.w, uuid.NewString(), ws.RevokeInput{CredentialID: credential.Credential.ID}, "")
		transportTool(t, f.ctx, writer, "download_attachment_chunk", f.w, "", attachment.DownloadInput{AttachmentID: file.ID, DraftID: u.DraftID.String(), FileRevision: file.FileRevision, Offset: 1, Limit: 1}, operations.Unauthenticated)
	})
	t.Run("audience_and_read_only_grants", func(t *testing.T) {
		api := mint("api", []string{"workspace.read"})
		wrong := f.client(t, ws.WithToolCredential(context.Background(), api.Token))
		transportTool(t, f.ctx, wrong, "get_attachment_upload_config", f.w, "", attachment.Empty{}, operations.Forbidden)
		read := mint("mcp", []string{"workspace.read"})
		reader := f.client(t, ws.WithToolCredential(context.Background(), read.Token))
		transportTool(t, f.ctx, reader, "create_attachment_upload", f.w, uuid.NewString(), attachment.CreateInput{DraftID: uuid.New(), Filename: "x", SizeBytes: 0, SHA256: sha(nil)}, operations.Forbidden)
	})
	t.Run("expired_upload_and_finalized_unbound", func(t *testing.T) {
		active := f.begin(t, human, []byte{1})
		f.sql(t, "UPDATE attachment_uploads SET version=version+1,expires_at=clock_timestamp()-interval '1 second' WHERE uuid=$1", active.ID)
		active.Version++
		current := transportDecode[attachment.UploadOutput](t, f.op(t, human, "attachment_uploads.get", attachment.UploadInput{UploadID: active.ID, DraftID: active.DraftID})).Upload
		if current.State != "expired" || current.Capabilities.UploadChunk || current.Capabilities.Finalize {
			t.Fatal("expired capabilities")
		}
		f.rejectOp(t, human, "attachment_uploads.chunk", chunkInput(active, []byte{1}), 409, operations.Conflict)
		f.sql(t, "UPDATE attachment_uploads SET version=version+1,expires_at=clock_timestamp()-interval '1 second' WHERE uuid=$1", u.ID)
		f.rejectOp(t, human, "attachments.download_chunk", attachment.DownloadInput{AttachmentID: file.ID, DraftID: u.DraftID.String(), FileRevision: file.FileRevision, Limit: 1}, 409, operations.Conflict)
	})
}
func TestAttachmentsTransportWireBudgetsAndRollback(t *testing.T) {
	f := newAttachmentTransport(t)
	cs := f.client(t, f.ctx)
	t.Run("chunk_decoded_and_raw_request_bounds", func(t *testing.T) {
		u := f.begin(t, cs, []byte{1})
		tooBig := bytes.Repeat([]byte{7}, int(attachment.MaxChunkBytes)+1)
		f.rejectOp(t, cs, "attachment_uploads.chunk", chunkInput(u, tooBig), 400, operations.Validation)
		f.http(t, "POST", fmt.Sprintf("/v1/workspaces/%s/attachment-uploads/%s/chunks", f.w, u.ID), f.bearer, uuid.NewString(), json.RawMessage(`{"padding":"`+strings.Repeat("x", int(attachment.MaxChunkRequestBytes))+`"}`), 400, operations.Validation)
		now := transportDecode[attachment.UploadOutput](t, f.op(t, cs, "attachment_uploads.get", attachment.UploadInput{UploadID: u.ID, DraftID: u.DraftID})).Upload
		if now.Version != u.Version || now.NextOffset != 0 {
			t.Fatal("budget rejection changed upload")
		}
	})
	t.Run("whole_digest_failure_is_atomic_and_resumable", func(t *testing.T) {
		u := transportDecode[attachment.UploadOutput](t, f.op(t, cs, "attachment_uploads.create", attachment.CreateInput{DraftID: uuid.New(), Filename: "wrong.bin", SizeBytes: 1, SHA256: sha([]byte{2})})).Upload
		u = transportDecode[attachment.UploadOutput](t, f.op(t, cs, "attachment_uploads.chunk", chunkInput(u, []byte{1}))).Upload
		f.rejectOp(t, cs, "attachment_uploads.finalize", attachment.VersionInput{UploadID: u.ID, DraftID: u.DraftID, ExpectedVersion: u.Version}, 400, operations.Validation)
		now := transportDecode[attachment.UploadOutput](t, f.op(t, cs, "attachment_uploads.get", attachment.UploadInput{UploadID: u.ID, DraftID: u.DraftID})).Upload
		var n int
		if e := f.pool.QueryRow(f.ctx, "SELECT count(*) FROM attachments a JOIN attachment_uploads u ON u.id=a.upload_id WHERE u.uuid=$1", u.ID).Scan(&n); e != nil || n != 0 || now.Version != u.Version || now.NextOffset != 1 || now.State != "uploading" {
			t.Fatal("failed digest published file/lost progress", e)
		}
	})
	t.Run("outbox_failure_rolls_back_chunk_and_can_retry_exact_command", func(t *testing.T) {
		u := f.begin(t, cs, []byte{0, 255})
		in := chunkInput(u, []byte{0, 255})
		key := uuid.NewString()
		f.sql(t, `CREATE FUNCTION reject_attachment_chunk_transport() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.event_name='attachment_uploads.chunk_uploaded' THEN RAISE EXCEPTION 'forced test rollback';END IF;RETURN NEW;END $$;CREATE TRIGGER reject_attachment_chunk_transport BEFORE INSERT ON outbox_events FOR EACH ROW EXECUTE FUNCTION reject_attachment_chunk_transport()`)
		path := fmt.Sprintf("/v1/workspaces/%s/attachment-uploads/%s/chunks", f.w, u.ID)
		body := attachmentMap(t, in)
		delete(body, "upload_id")
		f.http(t, "POST", path, f.bearer, key, body, 500, operations.Internal)
		transportTool(t, f.ctx, cs, "upload_attachment_chunk", f.w, key, in, operations.Internal)
		now := transportDecode[attachment.UploadOutput](t, f.op(t, cs, "attachment_uploads.get", attachment.UploadInput{UploadID: u.ID, DraftID: u.DraftID})).Upload
		var n int
		if e := f.pool.QueryRow(f.ctx, "SELECT count(*) FROM attachment_chunks c JOIN attachment_uploads u ON u.id=c.upload_id WHERE u.uuid=$1", u.ID).Scan(&n); e != nil || n != 0 || now.Version != 1 || now.NextOffset != 0 {
			t.Fatal("partial chunk commit", e)
		}
		f.sql(t, "DROP TRIGGER reject_attachment_chunk_transport ON outbox_events;DROP FUNCTION reject_attachment_chunk_transport()")
		saved := transportDecode[attachment.UploadOutput](t, f.invoke(t, cs, "upload_attachment_chunk", key, attachmentMap(t, in))).Upload
		if saved.Version != 2 || saved.NextOffset != 2 {
			t.Fatal("exact retry failed")
		}
		done := f.finish(t, cs, saved, nil)
		if !bytes.Equal(f.receive(t, cs, done.Attachment, 1), []byte{0, 255}) {
			t.Fatal("rollback orphan corrupted retry")
		}
	})
}
func TestAttachmentsTransportHTTPRevocationAndReceiptAuth(t *testing.T) {
	f := newAttachmentTransport(t)
	human := f.client(t, f.ctx)
	mint := func(audience string) ws.CreateCredentialOutput {
		return transportDecode[ws.CreateCredentialOutput](t, transportTool(t, f.ctx, human, "create_workspace_credential", f.w, uuid.NewString(), ws.CreateCredentialInput{Name: "HTTP live authorization", Audience: audience, Scopes: []string{"workspace.read", "workspace.write"}, ExpiresAt: time.Now().Add(time.Hour).UTC().Truncate(time.Second)}, ""))
	}
	api := mint("api")
	pat := mint("mcp")
	writer := f.client(t, ws.WithToolCredential(context.Background(), pat.Token))
	key := uuid.NewString()
	input := attachment.CreateInput{DraftID: uuid.New(), Filename: "receipt.bin", SizeBytes: 0, SHA256: sha(nil)}
	path := fmt.Sprintf("/v1/workspaces/%s/attachment-uploads", f.w)
	raw := f.http(t, "POST", path, api.Token, key, input, 201, "")
	sdk := transportTool(t, f.ctx, writer, "create_attachment_upload", f.w, key, input, "")
	if !bytes.Equal(raw, sdk) {
		t.Fatal("cross-channel receipt changed")
	}
	u := transportDecode[attachment.UploadOutput](t, raw).Upload
	done := f.finish(t, writer, u, nil)
	download := fmt.Sprintf("/v1/workspaces/%s/attachments/%s/download-chunk?draft_id=%s&file_revision=%s&offset=0&limit=1", f.w, done.Attachment.ID, u.DraftID, done.Attachment.FileRevision)
	t.Run("http_download_rechecks_live_credential", func(t *testing.T) {
		f.http(t, "GET", download, api.Token, "", nil, 200, "")
		transportTool(t, f.ctx, human, "revoke_workspace_credential", f.w, uuid.NewString(), ws.RevokeInput{CredentialID: api.Credential.ID}, "")
		f.http(t, "GET", download, api.Token, "", nil, 401, operations.Unauthenticated)
		f.http(t, "POST", path, api.Token, key, input, 401, operations.Unauthenticated)
	})
	t.Run("sdk_receipt_replay_rechecks_live_credential", func(t *testing.T) {
		transportTool(t, f.ctx, human, "revoke_workspace_credential", f.w, uuid.NewString(), ws.RevokeInput{CredentialID: pat.Credential.ID}, "")
		transportTool(t, f.ctx, writer, "create_attachment_upload", f.w, key, input, operations.Unauthenticated)
		saved := transportDecode[attachment.UploadOutput](t, f.op(t, human, "attachment_uploads.get", attachment.UploadInput{UploadID: u.ID, DraftID: u.DraftID})).Upload
		if saved.State != "finalized" || saved.AttachmentID == nil {
			t.Fatal("revocation changed successful state")
		}
	})
}
