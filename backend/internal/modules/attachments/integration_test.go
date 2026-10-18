package attachments_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	attachment "github.com/furkanmeclis/traccia/backend/internal/modules/attachments/usecase"
	"github.com/furkanmeclis/traccia/backend/internal/platform/authctx"
	"github.com/furkanmeclis/traccia/backend/internal/platform/operations"
	"github.com/furkanmeclis/traccia/backend/internal/platform/storage"
	"github.com/google/uuid"
	"io"
	"strings"
	"sync/atomic"
	"testing"
)

func sha(data []byte) string { s := sha256.Sum256(data); return hex.EncodeToString(s[:]) }
func result[T any](t *testing.T, f attachmentFixture, op string, in any) T {
	t.Helper()
	raw, e := f.call(op, uuid.NewString(), f.w, in)
	return decodeResult[T](t, raw, e)
}
func (f attachmentFixture) upload(t *testing.T, data []byte) attachment.Upload {
	t.Helper()
	return result[attachment.UploadOutput](t, f, "attachment_uploads.create", attachment.CreateInput{DraftID: uuid.New(), Filename: "../İstanbul\r\n🧭.bin", ContentType: "text/html", SizeBytes: int64(len(data)), SHA256: sha(data)}).Upload
}
func (f attachmentFixture) chunk(t *testing.T, u attachment.Upload, data []byte) attachment.Upload {
	t.Helper()
	in := attachment.ChunkInput{UploadID: u.ID, DraftID: u.DraftID, ExpectedVersion: u.Version, Offset: u.NextOffset, TotalBytes: u.SizeBytes, DataBase64: base64.StdEncoding.EncodeToString(data), ChunkSHA256: sha(data)}
	return result[attachment.UploadOutput](t, f, "attachment_uploads.chunk", in).Upload
}
func (f attachmentFixture) complete(t *testing.T, u attachment.Upload, data []byte) attachment.FinalizeOutput {
	t.Helper()
	for offset := 0; offset < len(data); {
		end := min(len(data), offset+int(u.ChunkBytes))
		u = f.chunk(t, u, data[offset:end])
		offset = end
	}
	return result[attachment.FinalizeOutput](t, f, "attachment_uploads.finalize", attachment.VersionInput{UploadID: u.ID, DraftID: u.DraftID, ExpectedVersion: u.Version})
}
func (f attachmentFixture) download(t *testing.T, file attachment.Attachment) []byte {
	t.Helper()
	out := []byte{}
	draft := ""
	if file.DraftID != nil {
		draft = file.DraftID.String()
	}
	for {
		part := result[attachment.DownloadOutput](t, f, "attachments.download_chunk", attachment.DownloadInput{AttachmentID: file.ID, DraftID: draft, FileRevision: file.FileRevision, Offset: int64(len(out)), Limit: 17})
		data, e := base64.StdEncoding.Strict().DecodeString(part.DataBase64)
		if e != nil || sha(data) != part.ChunkSHA256 || part.SHA256 != file.SHA256 || part.FileRevision != file.FileRevision || part.NextOffset != int64(len(out)+len(data)) {
			t.Fatal("download integrity mismatch")
		}
		out = append(out, data...)
		if part.EOF {
			break
		}
		if len(data) == 0 {
			t.Fatal("download made no progress")
		}
	}
	if sha(out) != file.SHA256 || int64(len(out)) != file.SizeBytes {
		t.Fatal("whole file mismatch")
	}
	return out
}
func TestAttachmentDurableTransfer(t *testing.T) {
	options := attachment.DefaultOptions()
	options.ChunkBytes = 32
	options.MaxFileBytes = 4096
	f := newAttachmentFixtureOptions(t, options, nil)
	data := make([]byte, 257)
	for i := range data {
		data[i] = byte(i)
	}
	t.Run("binary_resume_replay_and_final_digest", func(t *testing.T) {
		u := f.upload(t, data)
		first := data[:32]
		key := uuid.NewString()
		in := attachment.ChunkInput{UploadID: u.ID, DraftID: u.DraftID, ExpectedVersion: u.Version, Offset: 0, TotalBytes: u.SizeBytes, DataBase64: base64.StdEncoding.EncodeToString(first), ChunkSHA256: sha(first)}
		raw, e := f.call("attachment_uploads.chunk", key, f.w, in)
		one := decodeResult[attachment.UploadOutput](t, raw, e).Upload
		replay, e := f.call("attachment_uploads.chunk", key, f.w, in)
		if e != nil || !bytes.Equal(raw, replay) {
			t.Fatal("chunk replay mismatch", e)
		}
		in.ChunkSHA256 = sha(bytes.Repeat([]byte{1}, 32))
		in.DataBase64 = base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{1}, 32))
		_, e = f.call("attachment_uploads.chunk", key, f.w, in)
		checkCode(t, e, operations.IdempotencyConflict)
		current := result[attachment.UploadOutput](t, f, "attachment_uploads.get", attachment.UploadInput{UploadID: u.ID, DraftID: u.DraftID}).Upload
		if current.Version != one.Version || current.NextOffset != 32 {
			t.Fatal("resume mismatch")
		}
		for offset := 32; offset < len(data); {
			end := min(len(data), offset+32)
			current = f.chunk(t, current, data[offset:end])
			offset = end
		}
		finalIn := attachment.VersionInput{UploadID: u.ID, DraftID: u.DraftID, ExpectedVersion: current.Version}
		finalKey := uuid.NewString()
		raw, e = f.call("attachment_uploads.finalize", finalKey, f.w, finalIn)
		file := decodeResult[attachment.FinalizeOutput](t, raw, e).Attachment
		again, e := f.call("attachment_uploads.finalize", finalKey, f.w, finalIn)
		if e != nil || !bytes.Equal(raw, again) {
			t.Fatal("finalize replay mismatch", e)
		}
		if !bytes.Equal(f.download(t, file), data) {
			t.Fatal("binary roundtrip")
		}
		if file.Filename != "../İstanbul\r\n🧭.bin" || strings.ContainsAny(file.DownloadFilename, "/\\\r\n") {
			t.Fatal("filename boundary")
		}
		history := result[attachment.UploadHistoryList](t, f, "attachment_uploads.history", attachment.UploadHistoryInput{UploadID: u.ID, DraftID: u.DraftID, Limit: 100})
		if len(history.Items) != 11 {
			t.Fatalf("history %d", len(history.Items))
		}
	})
	t.Run("zero_byte_file", func(t *testing.T) {
		u := f.upload(t, nil)
		done := f.complete(t, u, nil)
		if done.Attachment.SizeBytes != 0 || len(f.download(t, done.Attachment)) != 0 {
			t.Fatal("zero file")
		}
	})
	t.Run("hash_failure_preserves_successful_chunks", func(t *testing.T) {
		u := result[attachment.UploadOutput](t, f, "attachment_uploads.create", attachment.CreateInput{DraftID: uuid.New(), Filename: "wrong.bin", ContentType: "", SizeBytes: 1, SHA256: sha([]byte{2})}).Upload
		u = f.chunk(t, u, []byte{1})
		_, e := f.call("attachment_uploads.finalize", uuid.NewString(), f.w, attachment.VersionInput{UploadID: u.ID, DraftID: u.DraftID, ExpectedVersion: u.Version})
		checkCode(t, e, operations.Validation)
		current := result[attachment.UploadOutput](t, f, "attachment_uploads.get", attachment.UploadInput{UploadID: u.ID, DraftID: u.DraftID}).Upload
		if current.NextOffset != 1 || current.Version != u.Version || current.State != "uploading" {
			t.Fatal("failed finalize lost resumable state")
		}
	})
	t.Run("gap_wrong_total_and_short_chunk_rejected", func(t *testing.T) {
		u := f.upload(t, bytes.Repeat([]byte{1}, 64))
		for _, in := range []attachment.ChunkInput{{UploadID: u.ID, DraftID: u.DraftID, ExpectedVersion: 1, Offset: 1, TotalBytes: 64, DataBase64: base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{1}, 32)), ChunkSHA256: sha(bytes.Repeat([]byte{1}, 32))}, {UploadID: u.ID, DraftID: u.DraftID, ExpectedVersion: 1, TotalBytes: 63, DataBase64: base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{1}, 32)), ChunkSHA256: sha(bytes.Repeat([]byte{1}, 32))}, {UploadID: u.ID, DraftID: u.DraftID, ExpectedVersion: 1, TotalBytes: 64, DataBase64: "AQ==", ChunkSHA256: sha([]byte{1})}} {
			_, e := f.call("attachment_uploads.chunk", uuid.NewString(), f.w, in)
			checkCode(t, e, operations.Validation)
		}
	})
}
func TestAttachmentOwnershipLifecycleAndPrivacy(t *testing.T) {
	f := newAttachmentFixture(t)
	data := []byte("private\x00draft\r\nİ 🧭")
	u := f.upload(t, data)
	done := f.complete(t, u, data)
	file := done.Attachment
	t.Run("wrong_draft_and_actor_do_not_disclose", func(t *testing.T) {
		_, e := f.call("attachments.get", "", f.w, attachment.AttachmentInput{AttachmentID: file.ID, DraftID: uuid.NewString()})
		checkCode(t, e, operations.NotFound)
		other := uuid.New()
		_, e = f.pool.Exec(f.ctx, "INSERT INTO users(uuid,email,password_hash,name,surname,status) VALUES($1,$2,'unused','Other','Actor','active')", other, other.String()+"@example.test")
		if e != nil {
			t.Fatal(e)
		}
		_, e = f.pool.Exec(f.ctx, "INSERT INTO workspace_members(workspace_id,user_id,role,active) SELECT w.id,u.id,'member',true FROM workspaces w,users u WHERE w.uuid=$1 AND u.uuid=$2", f.w, other)
		if e != nil {
			t.Fatal(e)
		}
		copy := f
		copy.ctx = authctx.WithPrincipal(f.ctx, authctx.Principal{UserID: other})
		for _, op := range []string{"attachment_uploads.get", "attachments.get", "attachments.history"} {
			var input any = attachment.UploadInput{UploadID: u.ID, DraftID: u.DraftID}
			if op == "attachments.get" {
				input = attachment.AttachmentInput{AttachmentID: file.ID, DraftID: file.DraftID.String()}
			}
			if op == "attachments.history" {
				input = attachment.HistoryInput{AttachmentID: file.ID, DraftID: file.DraftID.String(), Limit: 100}
			}
			_, e = copy.call(op, "", f.w, input)
			checkCode(t, e, operations.NotFound)
		}
	})
	t.Run("derived_channels_redact_unbound_private_metadata", func(t *testing.T) {
		rows, e := f.pool.Query(f.ctx, "SELECT payload::text FROM outbox_events WHERE workspace_uuid=$1 AND event_name LIKE 'attachment%'", f.w)
		if e != nil {
			t.Fatal(e)
		}
		defer rows.Close()
		for rows.Next() {
			var raw string
			if e = rows.Scan(&raw); e != nil {
				t.Fatal(e)
			}
			for _, secret := range []string{u.DraftID.String(), u.SHA256, "snapshot", "filename", "private"} {
				if strings.Contains(raw, secret) {
					t.Fatalf("unbound event leaked %s", secret)
				}
			}
		}
		if e = rows.Err(); e != nil {
			t.Fatal(e)
		}
		var n int
		if e = f.pool.QueryRow(f.ctx, "SELECT count(*) FROM workspace_history WHERE workspace_uuid=$1 AND event_name LIKE 'attachment%' AND name NOT IN ('Attachment upload','Attachment file')", f.w).Scan(&n); e != nil || n != 0 {
			t.Fatal("private activity leaked", n, e)
		}
	})
	t.Run("bind_remove_separate_version_and_immutable_identity", func(t *testing.T) {
		team := f.createTeam(t, "ATT")
		issue := f.create(t, team.ID)
		bound := result[attachment.AttachmentOutput](t, f, "attachments.bind", attachment.BindInput{AttachmentID: file.ID, DraftID: *file.DraftID, IssueID: issue.ID, ExpectedVersion: file.Version}).Attachment
		if bound.FileRevision != file.FileRevision || bound.Version != 2 || bound.IssueID == nil {
			t.Fatal("identity/version")
		}
		if !bytes.Equal(f.download(t, bound), data) {
			t.Fatal("bound bytes")
		}
		removed := result[attachment.AttachmentOutput](t, f, "attachments.remove", attachment.RemoveInput{AttachmentID: file.ID, ExpectedVersion: bound.Version}).Attachment
		if !removed.Removed || removed.FileRevision != file.FileRevision || removed.Version != 3 {
			t.Fatal("removed identity")
		}
		_, e := f.call("attachments.download_chunk", "", f.w, attachment.DownloadInput{AttachmentID: file.ID, FileRevision: file.FileRevision, Limit: 32})
		checkCode(t, e, operations.Conflict)
		_, e = f.call("attachments.bind", uuid.NewString(), f.w, attachment.BindInput{AttachmentID: file.ID, DraftID: *file.DraftID, IssueID: issue.ID, ExpectedVersion: 3})
		checkCode(t, e, operations.Conflict)
		history := result[attachment.HistoryList](t, f, "attachments.history", attachment.HistoryInput{AttachmentID: file.ID, DraftID: u.DraftID.String(), Limit: 100})
		if len(history.Items) != 3 || history.Items[1].EventName != "attachments.bound" || history.Items[2].EventName != "attachments.removed" {
			t.Fatal("file lifecycle history")
		}
		if e = f.attachmentRepo.Reconcile(f.ctx, 100); e != nil {
			t.Fatal(e)
		}
		var key string
		if e = f.pool.QueryRow(f.ctx, "SELECT f.object_key FROM attachment_file_chunks f JOIN attachments a ON a.id=f.attachment_id WHERE a.uuid=$1 LIMIT 1", file.ID).Scan(&key); e != nil {
			t.Fatal(e)
		}
		exists, e := f.driver.Exists(f.ctx, key)
		if e != nil || !exists {
			t.Fatal("cleanup deleted canonical bound bytes")
		}
	})
}

type injectedStore struct {
	attachment.Store
	fail atomic.Bool
	put  func(context.Context, string, []byte) error
	open func(context.Context, string, int64, int64) (io.ReadCloser, int64, error)
}

func (s *injectedStore) Put(ctx context.Context, key string, data []byte) error {
	if s.fail.Load() {
		return fmt.Errorf("private storage secret must never escape")
	}
	if s.put != nil {
		return s.put(ctx, key, data)
	}
	return s.Store.Put(ctx, key, data)
}
func (s *injectedStore) OpenRange(ctx context.Context, key string, o, n int64) (io.ReadCloser, int64, error) {
	if s.open != nil {
		return s.open(ctx, key, o, n)
	}
	return s.Store.OpenRange(ctx, key, o, n)
}
func TestAttachmentAtomicFailuresAndConcurrency(t *testing.T) {
	options := attachment.DefaultOptions()
	options.ChunkBytes = 32
	options.MaxFileBytes = 4096
	var injected *injectedStore
	f := newAttachmentFixtureOptions(t, options, func(store attachment.Store) attachment.Store {
		injected = &injectedStore{Store: store}
		return injected
	})
	data := bytes.Repeat([]byte{8}, 32)
	t.Run("storage_error_preserves_offset_and_history", func(t *testing.T) {
		u := f.upload(t, data)
		injected.fail.Store(true)
		_, e := f.call("attachment_uploads.chunk", uuid.NewString(), f.w, attachment.ChunkInput{UploadID: u.ID, DraftID: u.DraftID, ExpectedVersion: 1, TotalBytes: 32, DataBase64: base64.StdEncoding.EncodeToString(data), ChunkSHA256: sha(data)})
		checkCode(t, e, operations.Internal)
		injected.fail.Store(false)
		now := result[attachment.UploadOutput](t, f, "attachment_uploads.get", attachment.UploadInput{UploadID: u.ID, DraftID: u.DraftID}).Upload
		if now.Version != 1 || now.NextOffset != 0 {
			t.Fatal("failed storage advanced state")
		}
		f.complete(t, now, data)
	})
	t.Run("forced_sql_rollback_retries_immutable_objects", func(t *testing.T) {
		u := f.upload(t, data)
		u = f.chunk(t, u, data)
		_, e := f.pool.Exec(f.ctx, "CREATE FUNCTION fail_attachment_history() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'forced'; END $$; CREATE TRIGGER force_attachment_history BEFORE INSERT ON attachment_history FOR EACH ROW EXECUTE FUNCTION fail_attachment_history()")
		if e != nil {
			t.Fatal(e)
		}
		in := attachment.VersionInput{UploadID: u.ID, DraftID: u.DraftID, ExpectedVersion: u.Version}
		key := uuid.NewString()
		_, e = f.call("attachment_uploads.finalize", key, f.w, in)
		if e == nil {
			t.Fatal("forced rollback succeeded")
		}
		_, e = f.pool.Exec(f.ctx, "DROP TRIGGER force_attachment_history ON attachment_history; DROP FUNCTION fail_attachment_history()")
		if e != nil {
			t.Fatal(e)
		}
		var count int
		if e = f.pool.QueryRow(f.ctx, "SELECT count(*) FROM attachments a JOIN attachment_uploads u ON a.upload_id=u.id WHERE u.uuid=$1", u.ID).Scan(&count); e != nil || count != 0 {
			t.Fatal("partial file survived", e)
		}
		raw, e := f.call("attachment_uploads.finalize", key, f.w, in)
		file := decodeResult[attachment.FinalizeOutput](t, raw, e).Attachment
		if !bytes.Equal(f.download(t, file), data) {
			t.Fatal("rollback retry bytes")
		}
	})
	t.Run("concurrent_CAS_has_one_winner", func(t *testing.T) {
		u := f.upload(t, data)
		in := attachment.ChunkInput{UploadID: u.ID, DraftID: u.DraftID, ExpectedVersion: 1, TotalBytes: 32, DataBase64: base64.StdEncoding.EncodeToString(data), ChunkSHA256: sha(data)}
		start := make(chan struct{})
		errs := make(chan error, 2)
		for i := 0; i < 2; i++ {
			go func() { <-start; _, e := f.call("attachment_uploads.chunk", uuid.NewString(), f.w, in); errs <- e }()
		}
		close(start)
		success := 0
		for i := 0; i < 2; i++ {
			e := <-errs
			if e == nil {
				success++
			} else {
				checkCode(t, e, operations.VersionConflict)
			}
		}
		if success != 1 {
			t.Fatal("CAS winners", success)
		}
	})
	t.Run("abort_and_cleanup_retains_tombstone_and_late_object_resweep", func(t *testing.T) {
		u := f.upload(t, data)
		u = f.chunk(t, u, data)
		u = result[attachment.UploadOutput](t, f, "attachment_uploads.abort", attachment.VersionInput{UploadID: u.ID, DraftID: u.DraftID, ExpectedVersion: u.Version}).Upload
		if e := f.attachmentRepo.Reconcile(f.ctx, 100); e != nil {
			t.Fatal(e)
		}
		key := storage.AttachmentChunkKey(f.w.String(), u.ID.String(), 0, sha(data))
		if e := injected.Store.Put(f.ctx, key, data); e != nil {
			t.Fatal(e)
		}
		_, e := f.pool.Exec(f.ctx, "UPDATE attachment_uploads SET next_cleanup_at=clock_timestamp() WHERE uuid=$1", u.ID)
		if e != nil {
			t.Fatal(e)
		}
		if e = f.attachmentRepo.Reconcile(f.ctx, 100); e != nil {
			t.Fatal(e)
		}
		exists, e := f.driver.Exists(f.ctx, key)
		if e != nil || exists {
			t.Fatal("late orphan escaped retained resweep")
		}
		now := result[attachment.UploadOutput](t, f, "attachment_uploads.get", attachment.UploadInput{UploadID: u.ID, DraftID: u.DraftID}).Upload
		if now.State != "aborted" || now.Cleanup.NextCheckAt == nil {
			t.Fatal("cleanup retired tombstone")
		}
	})
}
