package attachments_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	attachment "github.com/furkanmeclis/traccia/backend/internal/modules/attachments/usecase"
	"github.com/google/uuid"
	"runtime"
	"sync/atomic"
	"testing"
	"time"
)

// Exercise the advertised maximum through actual MinIO without ever allocating
// the logical file. Finalize heap growth is observed independently of total IO.
func TestAttachmentMaximumFileBoundedMemory(t *testing.T) {
	f := newAttachmentFixture(t)
	block := bytes.Repeat([]byte{0, 255, 13, 10}, int(attachment.MaxChunkBytes)/4)
	h := sha256.New()
	chunks := int(attachment.MaxFileBytes / int64(len(block)))
	for i := 0; i < chunks; i++ {
		_, _ = h.Write(block)
	}
	whole := hex.EncodeToString(h.Sum(nil))
	u := result[attachment.UploadOutput](t, f, "attachment_uploads.create", attachment.CreateInput{DraftID: uuid.New(), Filename: "maximum.bin", ContentType: "application/octet-stream", SizeBytes: attachment.MaxFileBytes, SHA256: whole}).Upload
	encoded := base64.StdEncoding.EncodeToString(block)
	blockHash := sha(block)
	for i := 0; i < chunks; i++ {
		u = result[attachment.UploadOutput](t, f, "attachment_uploads.chunk", attachment.ChunkInput{UploadID: u.ID, DraftID: u.DraftID, ExpectedVersion: u.Version, Offset: u.NextOffset, TotalBytes: u.SizeBytes, DataBase64: encoded, ChunkSHA256: blockHash}).Upload
	}
	runtime.GC()
	var before runtime.MemStats
	runtime.ReadMemStats(&before)
	var peak atomic.Uint64
	peak.Store(before.HeapAlloc)
	stop, done := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(done)
		tick := time.NewTicker(time.Millisecond)
		defer tick.Stop()
		for {
			select {
			case <-stop:
				return
			case <-tick.C:
				var mem runtime.MemStats
				runtime.ReadMemStats(&mem)
				for old := peak.Load(); mem.HeapAlloc > old && !peak.CompareAndSwap(old, mem.HeapAlloc); old = peak.Load() {
				}
			}
		}
	}()
	raw, e := f.call("attachment_uploads.finalize", uuid.NewString(), f.w, attachment.VersionInput{UploadID: u.ID, DraftID: u.DraftID, ExpectedVersion: u.Version})
	close(stop)
	<-done
	file := decodeResult[attachment.FinalizeOutput](t, raw, e).Attachment
	if max := peak.Load(); max > before.HeapAlloc+128<<20 {
		t.Fatalf("finalize retained logical file: heap grew %d bytes", max-before.HeapAlloc)
	}
	downloaded := sha256.New()
	offset := int64(0)
	for {
		part := result[attachment.DownloadOutput](t, f, "attachments.download_chunk", attachment.DownloadInput{AttachmentID: file.ID, DraftID: file.DraftID.String(), FileRevision: file.FileRevision, Offset: offset, Limit: attachment.MaxChunkBytes})
		data, e := base64.StdEncoding.Strict().DecodeString(part.DataBase64)
		if e != nil || len(data) > int(attachment.MaxChunkBytes) || sha(data) != part.ChunkSHA256 {
			t.Fatal("chunk integrity")
		}
		_, _ = downloaded.Write(data)
		offset += int64(len(data))
		if part.NextOffset != offset {
			t.Fatal("offset")
		}
		if part.EOF {
			break
		}
	}
	if offset != attachment.MaxFileBytes || hex.EncodeToString(downloaded.Sum(nil)) != whole {
		t.Fatal("maximum file reconstructed incorrectly")
	}
	t.Logf("logical file %d bytes; observed finalize heap growth %d bytes", offset, peak.Load()-before.HeapAlloc)
}
