package attachments_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	attachment "github.com/furkanmeclis/traccia/backend/internal/modules/attachments/usecase"
	"github.com/furkanmeclis/traccia/backend/internal/platform/operations"
	"github.com/google/uuid"
	"testing"
	"time"
)

func TestAttachmentCoordinatedStorageRaces(t *testing.T) {
	options := attachment.DefaultOptions()
	options.ChunkBytes = 32
	options.MaxFileBytes = 4096
	var injected *injectedStore
	f := newAttachmentFixtureOptions(t, options, func(s attachment.Store) attachment.Store { injected = &injectedStore{Store: s}; return injected })
	t.Run("late_failed_put_cannot_overwrite_committed_retry", func(t *testing.T) {
		good := bytes.Repeat([]byte{2}, 32)
		bad := bytes.Repeat([]byte{1}, 32)
		u := f.upload(t, good)
		late := make(chan struct{})
		finished := make(chan error, 1)
		injected.put = func(_ context.Context, key string, data []byte) error {
			copyData := append([]byte{}, data...)
			go func() { <-late; finished <- injected.Store.Put(context.Background(), key, copyData) }()
			return fmt.Errorf("simulated canceled put still running remotely")
		}
		in := attachment.ChunkInput{UploadID: u.ID, DraftID: u.DraftID, ExpectedVersion: u.Version, TotalBytes: 32, DataBase64: base64.StdEncoding.EncodeToString(bad), ChunkSHA256: sha(bad)}
		_, e := f.call("attachment_uploads.chunk", uuid.NewString(), f.w, in)
		checkCode(t, e, operations.Internal)
		injected.put = nil
		u = f.chunk(t, u, good)
		close(late)
		if e = <-finished; e != nil {
			t.Fatal(e)
		}
		done := result[attachment.FinalizeOutput](t, f, "attachment_uploads.finalize", attachment.VersionInput{UploadID: u.ID, DraftID: u.DraftID, ExpectedVersion: u.Version})
		if !bytes.Equal(f.download(t, done.Attachment), good) {
			t.Fatal("late put replaced committed bytes")
		}
	})
	t.Run("revocation_during_put_rolls_back_progress", func(t *testing.T) {
		data := []byte{9}
		u := f.upload(t, data)
		entered, release := make(chan struct{}), make(chan struct{})
		injected.put = func(ctx context.Context, key string, data []byte) error {
			close(entered)
			<-release
			return injected.Store.Put(ctx, key, data)
		}
		done := make(chan error, 1)
		go func() {
			_, e := f.call("attachment_uploads.chunk", uuid.NewString(), f.w, attachment.ChunkInput{UploadID: u.ID, DraftID: u.DraftID, ExpectedVersion: 1, TotalBytes: 1, DataBase64: "CQ==", ChunkSHA256: sha(data)})
			done <- e
		}()
		<-entered
		_, e := f.pool.Exec(f.ctx, "UPDATE workspace_members SET active=false WHERE workspace_id=(SELECT id FROM workspaces WHERE uuid=$1) AND user_id=(SELECT id FROM users WHERE uuid=$2)", f.w, f.actor)
		if e != nil {
			t.Fatal(e)
		}
		close(release)
		e = <-done
		if e == nil {
			t.Fatal("revoked actor committed chunk")
		}
		injected.put = nil
		_, e = f.pool.Exec(f.ctx, "UPDATE workspace_members SET active=true WHERE workspace_id=(SELECT id FROM workspaces WHERE uuid=$1) AND user_id=(SELECT id FROM users WHERE uuid=$2)", f.w, f.actor)
		if e != nil {
			t.Fatal(e)
		}
		current := result[attachment.UploadOutput](t, f, "attachment_uploads.get", attachment.UploadInput{UploadID: u.ID, DraftID: u.DraftID}).Upload
		if current.Version != 1 || current.NextOffset != 0 {
			t.Fatal("revoked upload progress persisted")
		}
	})
	t.Run("canceled_storage_put_is_not_a_successful_receipt", func(t *testing.T) {
		data := []byte{6}
		u := f.upload(t, data)
		ctx, cancel := context.WithCancel(f.ctx)
		defer cancel()
		copy := f
		copy.ctx = ctx
		injected.put = func(ctx context.Context, _ string, _ []byte) error { cancel(); return ctx.Err() }
		_, e := copy.call("attachment_uploads.chunk", uuid.NewString(), f.w, attachment.ChunkInput{UploadID: u.ID, DraftID: u.DraftID, ExpectedVersion: 1, TotalBytes: 1, DataBase64: "Bg==", ChunkSHA256: sha(data)})
		checkCode(t, e, operations.Canceled)
		injected.put = nil
		now := result[attachment.UploadOutput](t, f, "attachment_uploads.get", attachment.UploadInput{UploadID: u.ID, DraftID: u.DraftID}).Upload
		if now.Version != 1 || now.NextOffset != 0 {
			t.Fatal("canceled request advanced state")
		}
	})

	t.Run("finalize_and_abort_serialize", func(t *testing.T) {
		u := f.upload(t, []byte{3})
		u = f.chunk(t, u, []byte{3})
		in := attachment.VersionInput{UploadID: u.ID, DraftID: u.DraftID, ExpectedVersion: u.Version}
		start := make(chan struct{})
		done := make(chan error, 2)
		for _, op := range []string{"attachment_uploads.finalize", "attachment_uploads.abort"} {
			go func(op string) { <-start; _, e := f.call(op, uuid.NewString(), f.w, in); done <- e }(op)
		}
		close(start)
		wins := 0
		for i := 0; i < 2; i++ {
			e := <-done
			if e == nil {
				wins++
			} else {
				checkCode(t, e, operations.VersionConflict)
			}
		}
		if wins != 1 {
			t.Fatal("lifecycle winners", wins)
		}
	})
	t.Run("bind_and_expiry_cleanup_never_delete_bound_bytes", func(t *testing.T) {
		data := []byte{4}
		u := f.upload(t, data)
		file := f.complete(t, u, data).Attachment
		issue := f.create(t, f.createTeam(t, "RACE").ID)
		start := make(chan struct{})
		done := make(chan error, 2)
		go func() {
			<-start
			_, e := f.call("attachments.bind", uuid.NewString(), f.w, attachment.BindInput{AttachmentID: file.ID, DraftID: *file.DraftID, IssueID: issue.ID, ExpectedVersion: 1})
			done <- e
		}()
		go func() { <-start; done <- f.attachmentRepo.Reconcile(f.ctx, 100) }()
		close(start)
		for i := 0; i < 2; i++ {
			if e := <-done; e != nil {
				t.Fatal(e)
			}
		}
		if !bytes.Equal(f.download(t, file), data) {
			t.Fatal("cleanup race bytes")
		}
	})
	t.Run("expiry_status_and_owner_history_remain_visible", func(t *testing.T) {
		short := options
		short.UploadTTL = time.Second
		other := newAttachmentFixtureOptions(t, short, nil)
		u := other.upload(t, []byte{1})
		time.Sleep(1100 * time.Millisecond)
		now := result[attachment.UploadOutput](t, other, "attachment_uploads.get", attachment.UploadInput{UploadID: u.ID, DraftID: u.DraftID}).Upload
		if now.State != "expired" || now.Capabilities.UploadChunk {
			t.Fatal("expiry missing")
		}
		if e := other.attachmentRepo.Reconcile(other.ctx, 100); e != nil {
			t.Fatal(e)
		}
		history := result[attachment.UploadHistoryList](t, other, "attachment_uploads.history", attachment.UploadHistoryInput{UploadID: u.ID, DraftID: u.DraftID, Limit: 100})
		if len(history.Items) != 2 || history.Items[1].EventName != "attachment_uploads.expired" {
			t.Fatal("expired history")
		}
	})
}
