package repository

import (
	"context"
	"errors"
	"fmt"
	"github.com/furkanmeclis/traccia/backend/internal/database/db"
	uc "github.com/furkanmeclis/traccia/backend/internal/modules/attachments/usecase"
	"github.com/furkanmeclis/traccia/backend/internal/platform/dbtx"
	"github.com/furkanmeclis/traccia/backend/internal/platform/operations"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"time"
)

type Postgres struct {
	pool    *pgxpool.Pool
	q       *db.Queries
	store   uc.Store
	options uc.Options
}

func New(pool *pgxpool.Pool, q *db.Queries, store uc.Store, options uc.Options) (*Postgres, error) {
	if e := options.Validate(); e != nil {
		return nil, e
	}
	if store == nil {
		return nil, errors.New("attachment storage required")
	}
	return &Postgres{pool, q, store, options}, nil
}
func (r *Postgres) queries(ctx context.Context) *db.Queries {
	if tx, ok := dbtx.Tx(ctx); ok {
		return r.q.WithTx(tx)
	}
	return r.q
}
func failure(code operations.Code) error { return &operations.Error{Code: code} }
func invalid(field, code string) error {
	return &operations.Error{Code: operations.Validation, Details: []operations.FieldError{{Field: field, Code: code}}}
}
func mapped(e error) error {
	if errors.Is(e, pgx.ErrNoRows) {
		return failure(operations.NotFound)
	}
	var p *pgconn.PgError
	if errors.As(e, &p) {
		switch p.Code {
		case "23505":
			return failure(operations.Conflict)
		case "23503", "23514":
			return failure(operations.Validation)
		}
	}
	return e
}
func requireTx(ctx context.Context) error {
	if _, ok := dbtx.Tx(ctx); !ok {
		return errors.New("attachment mutation requires operation transaction")
	}
	return nil
}
func stamp(t time.Time) pgtype.Timestamptz { return pgtype.Timestamptz{Time: t, Valid: true} }
func nullable(id uuid.UUID) pgtype.UUID    { return pgtype.UUID{Bytes: id, Valid: id != uuid.Nil} }
func timeString(t pgtype.Timestamptz) *string {
	if !t.Valid {
		return nil
	}
	s := t.Time.UTC().Format(time.RFC3339Nano)
	return &s
}
func idString(id pgtype.UUID) *string {
	if !id.Valid {
		return nil
	}
	s := uuid.UUID(id.Bytes).String()
	return &s
}
func stateAt(state string, expiry pgtype.Timestamptz, now time.Time) string {
	if (state == "uploading" || state == "finalized") && expiry.Valid && !expiry.Time.After(now) {
		return "expired"
	}
	return state
}
func uploadDTO(row db.AttachmentUploadPublic, now time.Time) uc.Upload {
	return uc.Upload{ID: row.Uuid, WorkspaceID: row.WorkspaceUuid, ActorID: row.ActorUuid, DraftID: row.DraftUuid, Filename: row.Filename, ContentType: row.ContentType, SizeBytes: row.SizeBytes, SHA256: row.Sha256, ChunkBytes: row.ChunkBytes, NextOffset: row.NextOffset, Version: row.Version, State: stateAt(row.State, row.ExpiresAt, now), ExpiresAt: timeString(row.ExpiresAt), AttachmentID: idString(row.AttachmentUuid), CreatedAt: row.CreatedAt.Time, UpdatedAt: row.UpdatedAt.Time, Cleanup: uc.Cleanup{State: row.CleanupState, LastError: row.LastCleanupError, NextCheckAt: timeString(row.NextCleanupAt)}}
}
func fileDTO(row db.AttachmentPublic, now time.Time) uc.Attachment {
	return uc.Attachment{ID: row.Uuid, WorkspaceID: row.WorkspaceUuid, UploadID: &row.UploadUuid, ActorID: row.ActorUuid, DraftID: &row.DraftUuid, FileRevision: row.FileRevision, Version: row.Version, State: stateAt(row.UploadState, row.UploadExpiresAt, now), Filename: row.Filename, DownloadFilename: uc.SafeFilename(row.Filename), ContentType: row.ContentType, SizeBytes: row.SizeBytes, SHA256: row.Sha256, IssueID: idString(row.IssueUuid), Removed: row.Removed, FinalizedAt: row.FinalizedAt.Time, ExpiresAt: timeString(row.ExpiresAt), DownloadPath: fmt.Sprintf("/v1/workspaces/%s/attachments/%s/download-chunk", row.WorkspaceUuid, row.Uuid)}
}
func (r *Postgres) now(ctx context.Context) (time.Time, error) {
	n, e := r.queries(ctx).DatabaseTime(ctx)
	return n.Time, e
}
func (r *Postgres) publicUpload(ctx context.Context, row db.AttachmentUpload) (uc.Upload, error) {
	p, e := r.queries(ctx).ReadAttachmentUpload(ctx, db.ReadAttachmentUploadParams{WorkspaceID: row.WorkspaceID, ID: row.ID})
	if e != nil {
		return uc.Upload{}, mapped(e)
	}
	now, e := r.now(ctx)
	return uploadDTO(p, now), e
}
func (r *Postgres) publicFile(ctx context.Context, w, id uuid.UUID) (db.AttachmentPublic, uc.Attachment, error) {
	p, e := r.queries(ctx).ReadAttachment(ctx, db.ReadAttachmentParams{WorkspaceUuid: w, Uuid: id})
	if e != nil {
		return p, uc.Attachment{}, mapped(e)
	}
	now, e := r.now(ctx)
	return p, fileDTO(p, now), e
}
func (r *Postgres) ownedLock(ctx context.Context, w, actor uuid.UUID, in uc.VersionInput) (db.AttachmentUpload, error) {
	if e := requireTx(ctx); e != nil {
		return db.AttachmentUpload{}, e
	}
	row, e := r.queries(ctx).LockOwnedAttachmentUpload(ctx, db.LockOwnedAttachmentUploadParams{Uuid: w, Uuid_2: actor, Uuid_3: in.UploadID, DraftUuid: in.DraftID})
	if e != nil {
		return row, mapped(e)
	}
	if row.Version != in.ExpectedVersion {
		return row, failure(operations.VersionConflict)
	}
	now, e := r.now(ctx)
	if e == nil && stateAt(row.State, row.ExpiresAt, now) != row.State {
		e = failure(operations.Conflict)
	}
	return row, e
}
func (r *Postgres) Create(ctx context.Context, w, actor uuid.UUID, in uc.CreateInput) (uc.Upload, error) {
	if e := requireTx(ctx); e != nil {
		return uc.Upload{}, e
	}
	now, e := r.now(ctx)
	if e != nil {
		return uc.Upload{}, e
	}
	row, e := r.queries(ctx).CreateAttachmentUpload(ctx, db.CreateAttachmentUploadParams{Uuid: w, Uuid_2: actor, DraftUuid: in.DraftID, Filename: in.Filename, ContentType: in.ContentType, SizeBytes: in.SizeBytes, Sha256: in.SHA256, ChunkBytes: r.options.ChunkBytes, ExpiresAt: stamp(now.Add(r.options.UploadTTL))})
	if e != nil {
		return uc.Upload{}, mapped(e)
	}
	out, e := r.publicUpload(ctx, row)
	if e == nil {
		e = r.recordUpload(ctx, w, actor, row, "attachment_uploads.created", out)
	}
	return out, e
}
func (r *Postgres) GetUpload(ctx context.Context, w, actor uuid.UUID, in uc.UploadInput) (uc.Upload, error) {
	p, e := r.queries(ctx).ReadOwnedAttachmentUpload(ctx, db.ReadOwnedAttachmentUploadParams{WorkspaceUuid: w, ActorUuid: actor, Uuid: in.UploadID, DraftUuid: in.DraftID})
	if e != nil {
		return uc.Upload{}, mapped(e)
	}
	now, e := r.now(ctx)
	return uploadDTO(p, now), e
}
func (r *Postgres) Get(ctx context.Context, w, actor uuid.UUID, in uc.AttachmentInput) (uc.Attachment, error) {
	p, out, e := r.publicFile(ctx, w, in.AttachmentID)
	if e != nil {
		return out, e
	}
	if !p.IssueID.Valid && (p.ActorUuid != actor || in.DraftID == "" || !sameDraft(in.DraftID, p.DraftUuid)) {
		return uc.Attachment{}, failure(operations.NotFound)
	}
	return out, nil
}
func sameDraft(s string, id uuid.UUID) bool { v, e := uuid.Parse(s); return e == nil && v == id }
func storageError() error {
	return &operations.Error{Code: operations.Internal}
}

var _ uc.Repository = (*Postgres)(nil)

func storageFailure(ctx context.Context) error {
	if e := ctx.Err(); e != nil {
		return e
	}
	return storageError()
}
