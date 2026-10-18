package attachments_test

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/furkanmeclis/traccia/backend/internal/database/db"
	module "github.com/furkanmeclis/traccia/backend/internal/modules/storage"
	handler "github.com/furkanmeclis/traccia/backend/internal/modules/storage/handler"
	model "github.com/furkanmeclis/traccia/backend/internal/modules/storage/model"
	explorer "github.com/furkanmeclis/traccia/backend/internal/modules/storage/usecase"
	ws "github.com/furkanmeclis/traccia/backend/internal/modules/workspaces/usecase"
	"github.com/furkanmeclis/traccia/backend/internal/platform/authctx"
	"github.com/furkanmeclis/traccia/backend/internal/platform/jwt"
	"github.com/furkanmeclis/traccia/backend/internal/platform/storage"
	"github.com/google/uuid"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

// A verified platform administrator deliberately has no membership in the file's
// workspace. The shared legacy storage grant must never authorize product bytes.
type attachmentStorageLoader struct{ internal int64 }

func (loader attachmentStorageLoader) LoadPrincipal(_ *http.Request, c jwt.Claims) (authctx.Principal, error) {
	id, e := c.UserUUID()
	return authctx.Principal{UserID: id, UserInternal: loader.internal, IsSuperAdmin: true}, e
}
func TestAttachmentPlatformStorageHTTPFence(t *testing.T) {
	f := newAttachmentFixture(t)
	probeKey := "attachment-fence-probe/" + uuid.NewString() + "/source.bin"
	probeData := []byte("ordinary source")
	if e := f.driver.Upload(f.ctx, storage.File{Body: bytes.NewReader(probeData), Size: int64(len(probeData)), Filename: "source.bin", ContentType: "application/octet-stream"}, probeKey); e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		if e := f.driver.Delete(context.Background(), probeKey); e != nil {
			t.Error("test-owned probe cleanup failed")
		}
	})
	baseline, e := explorer.New(f.driver, nil, nil).Usage(f.ctx)
	if e != nil {
		t.Fatal(e)
	}
	other := uuid.New()
	var otherInternal int64
	if e = f.pool.QueryRow(f.ctx, "INSERT INTO users(uuid,email,password_hash,name,surname,status) VALUES($1,$2,'unused','Other','Workspace','active') RETURNING id", other, other.String()+"@example.test").Scan(&otherInternal); e != nil {
		t.Fatal(e)
	}
	raw, e := f.call("workspaces.create", uuid.NewString(), uuid.Nil, ws.CreateInput{Name: "Other scoped workspace"})
	otherWorkspace := decodeResult[ws.WorkspaceOutput](t, raw, e).Workspace.ID
	if _, e = f.pool.Exec(f.ctx, "INSERT INTO workspace_members(workspace_id,user_id,role) SELECT id,$2,'member' FROM workspaces WHERE uuid=$1", otherWorkspace, otherInternal); e != nil {
		t.Fatal(e)
	}
	data := []byte("private object bytes")
	u := f.upload(t, data)
	file := f.complete(t, u, data).Attachment
	var key string
	if e := f.pool.QueryRow(f.ctx, "SELECT object_key FROM attachment_file_chunks c JOIN attachments a ON a.id=c.attachment_id WHERE a.uuid=$1 LIMIT 1", file.ID).Scan(&key); e != nil {
		t.Fatal(e)
	}
	tokens, e := jwt.NewManager("attachment-storage-test-signing-key", time.Hour, time.Hour)
	if e != nil {
		t.Fatal(e)
	}
	bearer, _, e := tokens.IssueAccess(jwt.AccessInput{UserID: other})
	if e != nil {
		t.Fatal(e)
	}
	mux := http.NewServeMux()
	module.RegisterRoutes(mux, handler.New(explorer.New(f.driver, db.New(f.pool), nil)), tokens, attachmentStorageLoader{internal: otherInternal})
	server := httptest.NewServer(mux)
	defer server.Close()
	request := func(t *testing.T, method, path string, body any) (int, []byte) {
		t.Helper()
		var raw []byte
		if body != nil {
			raw, e = json.Marshal(body)
			if e != nil {
				t.Fatal(e)
			}
		}
		req, e := http.NewRequestWithContext(f.ctx, method, server.URL+path, bytes.NewReader(raw))
		if e != nil {
			t.Fatal(e)
		}
		req.Header.Set("Authorization", "Bearer "+bearer)
		req.Header.Set("Content-Type", "application/json")
		res, e := server.Client().Do(req)
		if e != nil {
			t.Fatal(e)
		}
		defer func() { _ = res.Body.Close() }()
		raw, e = io.ReadAll(io.LimitReader(res.Body, 2<<20))
		if e != nil {
			t.Fatal(e)
		}
		return res.StatusCode, raw
	}
	blocked := func(t *testing.T, method, path string, body any) {
		t.Helper()
		status, raw := request(t, method, path, body)
		if status < 400 || status >= 500 || bytes.Contains(raw, data) {
			t.Fatalf("private platform route not rejected: %s %s status%d", method, path, status)
		}
	}
	t.Run("known_private_key_direct_reads_versions_links_and_shares", func(t *testing.T) {
		for _, path := range []string{"/objects/item", "/objects/download", "/objects/preview", "/versions", "/activity", "/shares", "/links"} {
			blocked(t, "GET", "/v1/platform/storage"+path+"?key="+url.QueryEscape(key), nil)
		}
		blocked(t, "GET", "/v1/platform/storage/objects/download?key="+url.QueryEscape(key)+"&version_id=known-version", nil)
		for path, body := range map[string]any{"/versions/restore": map[string]any{"key": key, "version_id": "known"}, "/versions/delete": map[string]any{"key": key, "version_id": "known"}, "/star": map[string]any{"key": key}, "/links": map[string]any{"key": key, "kind": "public"}, "/shares": map[string]any{"key": key, "user_uuid": uuid.NewString(), "role": "viewer"}, "/uploads/complete": map[string]any{"key": key}} {
			blocked(t, "POST", "/v1/platform/storage"+path, body)
		}
	})
	t.Run("root_and_ancestor_traversal_cannot_read_or_mutate_private_objects", func(t *testing.T) {
		for _, prefix := range []string{"__system/attachments", "/__system/attachments/", "__system/attachments/" + f.w.String() + "/"} {
			blocked(t, "GET", "/v1/platform/storage/objects?prefix="+url.QueryEscape(prefix)+"&recursive=true", nil)
		}
		for _, prefix := range []string{"", "__system/"} {
			status, raw := request(t, "GET", "/v1/platform/storage/objects?prefix="+url.QueryEscape(prefix)+"&recursive=true", nil)
			if status != 200 || strings.Contains(string(raw), "__system/attachments") {
				t.Fatal("ancestor list exposed private namespace", status)
			}
		}
		for _, path := range []string{"/objects/copy", "/objects/move"} {
			blocked(t, "POST", "/v1/platform/storage"+path, map[string]any{"source_key": probeKey, "dest_key": key})
		}
		for _, root := range []string{"", "/", "__system/", "__system/attachments/"} {
			for _, path := range []string{"/objects/copy", "/objects/move"} {
				blocked(t, "POST", "/v1/platform/storage"+path, map[string]any{"source_key": root, "dest_key": "test-destination/"})
				blocked(t, "POST", "/v1/platform/storage"+path, map[string]any{"source_key": key, "dest_key": root})
			}
			blocked(t, "POST", "/v1/platform/storage/objects/delete", map[string]any{"keys": []string{root}})
		}
	})
	t.Run("persisted_auxiliary_rows_cannot_reopen_or_mutate_private_keys", func(t *testing.T) {
		slug := uuid.NewString()
		token := uuid.NewString()
		trash := uuid.New()
		_, e := f.pool.Exec(f.ctx, "INSERT INTO storage_links(object_key,kind,slug) VALUES($1,'public',$2);", key, slug)
		if e != nil {
			t.Fatal(e)
		}
		_, e = f.pool.Exec(f.ctx, "INSERT INTO storage_links(object_key,kind,token_hash) VALUES($1,'signed',$2)", key, sha([]byte(token)))
		if e != nil {
			t.Fatal(e)
		}
		blocked(t, "GET", "/v1/public/storage/"+slug, nil)
		blocked(t, "GET", "/v1/public/storage/s/"+token, nil)
		_, e = f.pool.Exec(f.ctx, "INSERT INTO storage_trash(uuid,object_key,original_key,name,size_bytes,expires_at) VALUES($1,$2,$2,'private',1,now()+interval '1 day')", trash, key)
		if e != nil {
			t.Fatal(e)
		}
		for _, path := range []string{"/objects/restore", "/objects/purge"} {
			blocked(t, "POST", "/v1/platform/storage"+path, map[string]any{"keys": []string{trash.String()}})
		}
		if _, e = f.pool.Exec(f.ctx, "INSERT INTO storage_stars(user_id,object_key) VALUES($1,$2)", otherInternal, key); e != nil {
			t.Fatal(e)
		}
		if _, e = f.pool.Exec(f.ctx, "INSERT INTO storage_shares(user_id,object_key,role) VALUES($1,$2,'viewer')", otherInternal, key); e != nil {
			t.Fatal(e)
		}
		for _, view := range []string{"starred", "shared", "public", "recent"} {
			status, raw := request(t, "GET", "/v1/platform/storage/objects?view="+view, nil)
			if status != 200 || strings.Contains(string(raw), key) {
				t.Fatal("auxiliary metadata exposed private namespace", view, status)
			}
		}
		status, raw := request(t, "GET", "/v1/platform/storage/objects?view=trash", nil)
		if status != 200 || strings.Contains(string(raw), key) {
			t.Fatal("trash metadata exposed private namespace")
		}
	})
	t.Run("private_chunks_do_not_change_platform_usage_or_totals", func(t *testing.T) {
		status, raw := request(t, "GET", "/v1/platform/storage/usage", nil)
		if status != 200 {
			t.Fatal("usage", status)
		}
		var envelope struct {
			Data model.Usage `json:"data"`
		}
		if e = json.Unmarshal(raw, &envelope); e != nil {
			t.Fatal(e)
		}
		if envelope.Data.UsedBytes != baseline.UsedBytes || envelope.Data.TotalFiles != baseline.TotalFiles || envelope.Data.TotalFolders != baseline.TotalFolders {
			t.Fatal("private attachment counted in platform totals")
		}
		if strings.Contains(string(raw), "__system/attachments") {
			t.Fatal("private usage metadata exposed")
		}
	})

	probeBody, _, e := f.driver.Download(f.ctx, probeKey)
	if e != nil {
		t.Fatal("ordinary source mutated")
	}
	probeActual, e := io.ReadAll(probeBody)
	_ = probeBody.Close()
	if e != nil || !bytes.Equal(probeActual, probeData) {
		t.Fatal("ordinary source changed")
	}

	if !bytes.Equal(f.download(t, file), data) {
		t.Fatal("platform request changed private bytes")
	}
	if exists, e := f.driver.Exists(f.ctx, key); e != nil || !exists {
		t.Fatal("platform mutation removed canonical object")
	}
}
