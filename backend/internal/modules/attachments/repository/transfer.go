package repository

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"github.com/furkanmeclis/traccia/backend/internal/database/db"
	uc "github.com/furkanmeclis/traccia/backend/internal/modules/attachments/usecase"
	"github.com/furkanmeclis/traccia/backend/internal/platform/operations"
	"github.com/furkanmeclis/traccia/backend/internal/platform/storage"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"io"
)

func digest(data []byte) string { s := sha256.Sum256(data); return hex.EncodeToString(s[:]) }
func (r *Postgres) Chunk(ctx context.Context, w, actor uuid.UUID, in uc.ChunkInput) (uc.Upload, error) {
	row, e := r.ownedLock(ctx, w, actor, uc.VersionInput{UploadID: in.UploadID, DraftID: in.DraftID, ExpectedVersion: in.ExpectedVersion})
	if e != nil {
		return uc.Upload{}, e
	}
	if row.State != "uploading" {
		return uc.Upload{}, failure(operations.Conflict)
	}
	data, fields := uc.DecodeChunk(in)
	if len(fields) > 0 {
		return uc.Upload{}, &operations.Error{Code: operations.Validation, Details: fields}
	}
	expected := min(int64(row.ChunkBytes), row.SizeBytes-row.NextOffset)
	if in.Offset != row.NextOffset || in.TotalBytes != row.SizeBytes || int64(len(data)) != expected {
		return uc.Upload{}, invalid("offset", "mismatch")
	}
	key := storage.AttachmentChunkKey(w.String(), row.Uuid.String(), in.Offset, in.ChunkSHA256)
	if e = r.store.Put(ctx, key, data); e != nil {
		return uc.Upload{}, storageFailure(ctx)
	}
	q := r.queries(ctx)
	if e = q.InsertAttachmentChunk(ctx, db.InsertAttachmentChunkParams{WorkspaceID: row.WorkspaceID, UploadID: row.ID, OffsetBytes: in.Offset, SizeBytes: int32(len(data)), Sha256: in.ChunkSHA256, ObjectKey: key}); e != nil {
		return uc.Upload{}, mapped(e)
	}
	now, e := r.now(ctx)
	if e != nil {
		return uc.Upload{}, e
	}
	row, e = q.AdvanceAttachmentUploadChunk(ctx, db.AdvanceAttachmentUploadChunkParams{WorkspaceID: row.WorkspaceID, ID: row.ID, NextOffset: row.NextOffset + int64(len(data)), ExpiresAt: stamp(now.Add(r.options.UploadTTL))})
	if e != nil {
		return uc.Upload{}, mapped(e)
	}
	out, e := r.publicUpload(ctx, row)
	if e == nil {
		e = r.recordUpload(ctx, w, actor, row, "attachment_uploads.chunk_uploaded", out)
	}
	return out, e
}

// readExact closes each bounded object body immediately, including every error path.
func (r *Postgres) readExact(ctx context.Context, key string, offset, length, total int64) ([]byte, error) {
	body, actual, e := r.store.OpenRange(ctx, key, offset, length)
	if e != nil {
		if body != nil {
			_ = body.Close()
		}
		return nil, storageFailure(ctx)
	}
	if body == nil {
		return nil, storageFailure(ctx)
	}
	data, readErr := io.ReadAll(io.LimitReader(body, length+1))
	closeErr := body.Close()
	if readErr != nil || closeErr != nil || actual != total || int64(len(data)) != length {
		return nil, storageFailure(ctx)
	}
	return data, nil
}

type manifestChunk struct {
	Key    string `json:"key"`
	Offset int64  `json:"offset"`
	Size   int32  `json:"size"`
	SHA256 string `json:"sha256"`
}
type manifest struct {
	Format string          `json:"format"`
	Size   int64           `json:"size"`
	SHA256 string          `json:"sha256"`
	Chunks []manifestChunk `json:"chunks"`
}

func (r *Postgres) Finalize(ctx context.Context, w, actor uuid.UUID, in uc.VersionInput) (uc.FinalizeOutput, error) {
	var out uc.FinalizeOutput
	row, e := r.ownedLock(ctx, w, actor, in)
	if e != nil {
		return out, e
	}
	if row.State != "uploading" || row.NextOffset != row.SizeBytes {
		return out, failure(operations.Conflict)
	}
	q := r.queries(ctx)
	chunks, e := q.ListAttachmentChunks(ctx, db.ListAttachmentChunksParams{WorkspaceID: row.WorkspaceID, UploadID: row.ID})
	if e != nil {
		return out, e
	}
	if len(chunks) > uc.MaxChunks {
		return out, storageFailure(ctx)
	}
	m := manifest{Format: "traccia-attachment-chunks-v1", Size: row.SizeBytes, SHA256: row.Sha256, Chunks: make([]manifestChunk, 0, len(chunks))}
	whole := sha256.New()
	offset := int64(0)
	for _, c := range chunks {
		if c.OffsetBytes != offset || int64(c.SizeBytes) != min(int64(row.ChunkBytes), row.SizeBytes-offset) {
			return out, storageFailure(ctx)
		}
		data, e := r.readExact(ctx, c.ObjectKey, 0, int64(c.SizeBytes), int64(c.SizeBytes))
		if e != nil {
			return out, e
		}
		if digest(data) != c.Sha256 {
			return out, storageFailure(ctx)
		}
		_, _ = whole.Write(data)
		key := storage.AttachmentFinalChunkKey(w.String(), row.Uuid.String(), c.OffsetBytes, c.Sha256)
		if e = r.store.Put(ctx, key, data); e != nil {
			return out, storageFailure(ctx)
		}
		m.Chunks = append(m.Chunks, manifestChunk{key, c.OffsetBytes, c.SizeBytes, c.Sha256})
		offset += int64(c.SizeBytes)
	}
	if offset != row.SizeBytes {
		return out, invalid("size_bytes", "mismatch")
	}
	if hex.EncodeToString(whole.Sum(nil)) != row.Sha256 {
		return out, invalid("sha256", "mismatch")
	}
	raw, e := json.Marshal(m)
	if e != nil {
		return out, e
	}
	key := storage.AttachmentManifestKey(w.String(), row.Uuid.String(), row.Sha256)
	if e = r.store.Put(ctx, key, raw); e != nil {
		return out, storageFailure(ctx)
	}
	now, e := r.now(ctx)
	if e != nil {
		return out, e
	}
	expiry := stamp(now.Add(r.options.FinalizedRetention))
	file, e := q.CreateAttachment(ctx, db.CreateAttachmentParams{WorkspaceID: row.WorkspaceID, ID: row.ID, ManifestKey: key, ExpiresAt: expiry})
	if e != nil {
		return out, mapped(e)
	}
	for i, c := range chunks {
		if e = q.InsertAttachmentFileChunk(ctx, db.InsertAttachmentFileChunkParams{WorkspaceID: row.WorkspaceID, AttachmentID: file.ID, UploadChunkID: c.ID, ObjectKey: m.Chunks[i].Key}); e != nil {
			return out, mapped(e)
		}
	}
	row, e = q.FinalizeAttachmentUpload(ctx, db.FinalizeAttachmentUploadParams{WorkspaceID: row.WorkspaceID, ID: row.ID, AttachmentID: pgtype.Int8{Int64: file.ID, Valid: true}, ExpiresAt: expiry})
	if e != nil {
		return out, mapped(e)
	}
	out.Upload, e = r.publicUpload(ctx, row)
	if e != nil {
		return out, e
	}
	p, f, e := r.publicFile(ctx, w, file.Uuid)
	out.Attachment = f
	if e == nil {
		e = r.recordUpload(ctx, w, actor, row, "attachment_uploads.finalized", out.Upload)
	}
	if e == nil {
		e = r.recordFile(ctx, w, actor, p, "attachments.finalized", f)
	}
	return out, e
}
func (r *Postgres) Abort(ctx context.Context, w, actor uuid.UUID, in uc.VersionInput) (uc.Upload, error) {
	row, e := r.ownedLock(ctx, w, actor, in)
	if e != nil {
		return uc.Upload{}, e
	}
	if row.State != "uploading" && row.State != "finalized" {
		return uc.Upload{}, failure(operations.Conflict)
	}
	row, e = r.queries(ctx).TombstoneAttachmentUpload(ctx, db.TombstoneAttachmentUploadParams{WorkspaceID: row.WorkspaceID, ID: row.ID, State: "aborted"})
	if e != nil {
		return uc.Upload{}, mapped(e)
	}
	out, e := r.publicUpload(ctx, row)
	if e == nil {
		e = r.recordUpload(ctx, w, actor, row, "attachment_uploads.aborted", out)
	}
	return out, e
}
func (r *Postgres) Download(ctx context.Context, w, actor uuid.UUID, in uc.DownloadInput) (uc.DownloadOutput, error) {
	var out uc.DownloadOutput
	file, e := r.Get(ctx, w, actor, uc.AttachmentInput{AttachmentID: in.AttachmentID, DraftID: in.DraftID})
	if e != nil {
		return out, e
	}
	if file.Removed || (file.State != "finalized" && file.State != "bound") {
		return out, failure(operations.Conflict)
	}
	if file.FileRevision != in.FileRevision {
		return out, failure(operations.Conflict)
	}
	if in.Offset > file.SizeBytes {
		return out, invalid("offset", "invalid")
	}
	p, _, e := r.publicFile(ctx, w, in.AttachmentID)
	if e != nil {
		return out, e
	}
	end := min(file.SizeBytes, in.Offset+int64(in.Limit))
	data := make([]byte, 0, end-in.Offset)
	if end > in.Offset {
		chunks, e := r.queries(ctx).ReadFinalAttachmentChunkRange(ctx, db.ReadFinalAttachmentChunkRangeParams{WorkspaceID: p.WorkspaceID, AttachmentID: p.ID, StartOffset: in.Offset, EndOffset: end})
		if e != nil {
			return out, e
		}
		cursor := in.Offset
		for _, c := range chunks {
			start := max(cursor, c.OffsetBytes)
			stop := min(end, c.OffsetBytes+int64(c.SizeBytes))
			if start != cursor || stop <= start {
				return out, storageFailure(ctx)
			}
			part, e := r.readExact(ctx, c.ObjectKey, start-c.OffsetBytes, stop-start, int64(c.SizeBytes))
			if e != nil {
				return out, e
			}
			data = append(data, part...)
			cursor = stop
		}
		if cursor != end {
			return out, storageFailure(ctx)
		}
	}
	out = uc.DownloadOutput{AttachmentID: file.ID, FileRevision: file.FileRevision, Filename: file.Filename, DownloadFilename: file.DownloadFilename, ContentType: "application/octet-stream", ContentDisposition: uc.ContentDisposition(file.Filename), SizeBytes: file.SizeBytes, SHA256: file.SHA256, Offset: in.Offset, DataBase64: base64.StdEncoding.EncodeToString(data), ChunkSHA256: digest(data), NextOffset: end, EOF: end == file.SizeBytes}
	return out, nil
}
