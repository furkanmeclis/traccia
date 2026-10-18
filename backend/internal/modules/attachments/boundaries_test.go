package attachments_test

import (
	"bytes"
	"context"
	attachment "github.com/furkanmeclis/traccia/backend/internal/modules/attachments/usecase"
	ws "github.com/furkanmeclis/traccia/backend/internal/modules/workspaces/usecase"
	"github.com/furkanmeclis/traccia/backend/internal/platform/operations"
	"github.com/google/uuid"
	"io"
	"strings"
	"sync/atomic"
	"testing"
)

type countedBody struct {
	io.Reader
	closed *atomic.Int32
}

func (b countedBody) Close() error { b.closed.Add(1); return nil }
func TestAttachmentTenantAndStorageBoundaries(t *testing.T) {
	var injected *injectedStore
	f := newAttachmentFixtureOptions(t, attachment.DefaultOptions(), func(s attachment.Store) attachment.Store { injected = &injectedStore{Store: s}; return injected })
	data := []byte("binary\x00body")
	u := f.upload(t, data)
	file := f.complete(t, u, data).Attachment
	t.Run("workspace_and_issue_FK_isolation", func(t *testing.T) {
		raw, e := f.call("workspaces.create", uuid.NewString(), uuid.Nil, ws.CreateInput{Name: "other"})
		other := decodeResult[ws.WorkspaceOutput](t, raw, e).Workspace.ID
		_, e = f.call("attachments.get", "", other, attachment.AttachmentInput{AttachmentID: file.ID, DraftID: file.DraftID.String()})
		checkCode(t, e, operations.NotFound)
		_, e = f.call("attachment_uploads.get", "", other, attachment.UploadInput{UploadID: u.ID, DraftID: u.DraftID})
		checkCode(t, e, operations.NotFound)
		copy := f
		copy.w = other
		issue := copy.create(t, copy.createTeam(t, "OTHER").ID)
		_, e = f.call("attachments.bind", uuid.NewString(), f.w, attachment.BindInput{AttachmentID: file.ID, DraftID: *file.DraftID, IssueID: issue.ID, ExpectedVersion: 1})
		checkCode(t, e, operations.NotFound)
		tx, e := f.pool.Begin(f.ctx)
		if e != nil {
			t.Fatal(e)
		}
		defer func() { _ = tx.Rollback(f.ctx) }()
		_, e = tx.Exec(f.ctx, "UPDATE attachments SET issue_id=(SELECT id FROM issues WHERE uuid=$1),expires_at=NULL,version=version+1 WHERE uuid=$2", issue.ID, file.ID)
		if e == nil {
			t.Fatal("cross-workspace SQL FK bypass")
		}
	})
	t.Run("bounded_read_closes_body_and_rejects_trailing_bytes", func(t *testing.T) {
		var closed atomic.Int32
		injected.open = func(_ context.Context, _ string, _ int64, n int64) (io.ReadCloser, int64, error) {
			return countedBody{bytes.NewReader(bytes.Repeat([]byte{1}, int(n+1))), &closed}, int64(len(data)), nil
		}
		_, e := f.call("attachments.download_chunk", "", f.w, attachment.DownloadInput{AttachmentID: file.ID, DraftID: file.DraftID.String(), FileRevision: file.FileRevision, Limit: 4})
		checkCode(t, e, operations.Internal)
		injected.open = nil
		if closed.Load() != 1 {
			t.Fatal("object body leaked")
		}
		if !bytes.Equal(f.download(t, file), data) {
			t.Fatal("read failure changed canonical file")
		}
	})
	t.Run("metadata_NUL_and_noncanonical_base64_rejected_before_effects", func(t *testing.T) {
		for _, in := range []attachment.CreateInput{{DraftID: uuid.New(), Filename: "a\x00b", ContentType: "", SHA256: sha(nil)}, {DraftID: uuid.New(), Filename: "a", ContentType: "x\x00y", SHA256: sha(nil)}} {
			_, e := f.call("attachment_uploads.create", uuid.NewString(), f.w, in)
			checkCode(t, e, operations.Validation)
		}
		up := f.upload(t, []byte{1})
		for _, encoded := range []string{"AQ", "AR==", "AQ==\n"} {
			_, e := f.call("attachment_uploads.chunk", uuid.NewString(), f.w, attachment.ChunkInput{UploadID: up.ID, DraftID: up.DraftID, ExpectedVersion: 1, TotalBytes: 1, DataBase64: encoded, ChunkSHA256: sha([]byte{1})})
			checkCode(t, e, operations.Validation)
		}
		_, e := f.call("attachments.list", "", f.w, attachment.ListInput{IssueID: uuid.New(), Search: "\x00", Limit: 10})
		checkCode(t, e, operations.Validation)
	})
	t.Run("raw_limit_reports_explicit_error_without_decode_or_mutation", func(t *testing.T) {
		raw := []byte(`{"data_base64":"` + strings.Repeat("A", attachment.MaxChunkRequestBytes) + `"}`)
		_, e := f.registry.Execute(f.ctx, operations.Call{OperationID: "attachment_uploads.chunk", WorkspaceID: f.w, Channel: operations.API, IdempotencyKey: uuid.NewString(), Input: raw})
		checkCode(t, e, operations.Validation)
		op := e.(*operations.Error)
		if len(op.Details) != 1 || op.Details[0].Code != "bytes_limit" {
			t.Fatal("raw limit diagnostics")
		}
	})
}
