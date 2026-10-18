package repository

import (
	"context"
	"github.com/furkanmeclis/traccia/backend/internal/database/db"
	uc "github.com/furkanmeclis/traccia/backend/internal/modules/attachments/usecase"
	"github.com/furkanmeclis/traccia/backend/internal/platform/operations"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
)

// Bind discovers and locks the current team before the issue, then revalidates
// that discovery. Upload and file locks follow; no parent lock is acquired later.
func (r *Postgres) Bind(ctx context.Context, w, actor uuid.UUID, in uc.BindInput) (uc.Attachment, error) {
	if e := requireTx(ctx); e != nil {
		return uc.Attachment{}, e
	}
	q := r.queries(ctx)
	discovered, e := q.ReadDocumentOwner(ctx, db.ReadDocumentOwnerParams{WorkspaceUuid: w, Kind: "issue", Uuid: in.IssueID})
	if e != nil {
		return uc.Attachment{}, mapped(e)
	}
	if _, e = q.LockTeam(ctx, db.LockTeamParams{Uuid: w, Uuid_2: discovered.TeamUuid}); e != nil {
		return uc.Attachment{}, mapped(e)
	}
	_, e = q.LockIssue(ctx, db.LockIssueParams{Uuid: w, Uuid_2: in.IssueID})
	if e != nil {
		return uc.Attachment{}, mapped(e)
	}
	owner, e := q.ReadDocumentOwner(ctx, db.ReadDocumentOwnerParams{WorkspaceUuid: w, Kind: "issue", Uuid: in.IssueID})
	if e != nil {
		return uc.Attachment{}, mapped(e)
	}
	if !owner.Active.Bool || owner.TeamUuid != discovered.TeamUuid {
		return uc.Attachment{}, failure(operations.Conflict)
	}
	p, _, e := r.publicFile(ctx, w, in.AttachmentID)
	if e != nil {
		return uc.Attachment{}, e
	}
	if p.ActorUuid != actor || p.DraftUuid != in.DraftID {
		return uc.Attachment{}, failure(operations.NotFound)
	}
	u, e := q.LockAttachmentUpload(ctx, db.LockAttachmentUploadParams{WorkspaceID: p.WorkspaceID, ID: p.UploadID})
	if e != nil {
		return uc.Attachment{}, mapped(e)
	}
	f, e := q.LockAttachment(ctx, db.LockAttachmentParams{WorkspaceID: p.WorkspaceID, ID: p.ID})
	if e != nil {
		return uc.Attachment{}, mapped(e)
	}
	now, e := r.now(ctx)
	if e != nil {
		return uc.Attachment{}, e
	}
	if f.Version != in.ExpectedVersion {
		return uc.Attachment{}, failure(operations.VersionConflict)
	}
	if f.IssueID.Valid || f.Removed || stateAt(u.State, u.ExpiresAt, now) != "finalized" {
		return uc.Attachment{}, failure(operations.Conflict)
	}
	if _, e = q.BindAttachment(ctx, db.BindAttachmentParams{WorkspaceID: p.WorkspaceID, ID: p.ID, IssueID: pgtype.Int8{Int64: owner.ID, Valid: true}}); e != nil {
		return uc.Attachment{}, mapped(e)
	}
	u, e = q.BindAttachmentUpload(ctx, db.BindAttachmentUploadParams{WorkspaceID: p.WorkspaceID, ID: p.UploadID})
	if e != nil {
		return uc.Attachment{}, mapped(e)
	}
	up, e := r.publicUpload(ctx, u)
	if e != nil {
		return uc.Attachment{}, e
	}
	p, out, e := r.publicFile(ctx, w, in.AttachmentID)
	if e == nil {
		e = r.recordUpload(ctx, w, actor, u, "attachment_uploads.bound", up)
	}
	if e == nil {
		e = r.recordFile(ctx, w, actor, p, "attachments.bound", out)
	}
	return out, e
}
func (r *Postgres) Remove(ctx context.Context, w, actor uuid.UUID, in uc.RemoveInput) (uc.Attachment, error) {
	if e := requireTx(ctx); e != nil {
		return uc.Attachment{}, e
	}
	p, _, e := r.publicFile(ctx, w, in.AttachmentID)
	if e != nil {
		return uc.Attachment{}, e
	}
	if !p.IssueUuid.Valid {
		return uc.Attachment{}, failure(operations.NotFound)
	}
	q := r.queries(ctx)
	if _, e = q.LockIssue(ctx, db.LockIssueParams{Uuid: w, Uuid_2: uuid.UUID(p.IssueUuid.Bytes)}); e != nil {
		return uc.Attachment{}, mapped(e)
	}
	_, e = q.LockAttachmentUpload(ctx, db.LockAttachmentUploadParams{WorkspaceID: p.WorkspaceID, ID: p.UploadID})
	if e != nil {
		return uc.Attachment{}, mapped(e)
	}
	f, e := q.LockAttachment(ctx, db.LockAttachmentParams{WorkspaceID: p.WorkspaceID, ID: p.ID})
	if e != nil {
		return uc.Attachment{}, mapped(e)
	}
	if f.Version != in.ExpectedVersion {
		return uc.Attachment{}, failure(operations.VersionConflict)
	}
	if f.Removed {
		return uc.Attachment{}, failure(operations.Conflict)
	}
	if _, e = q.RemoveAttachment(ctx, db.RemoveAttachmentParams{WorkspaceID: p.WorkspaceID, ID: p.ID}); e != nil {
		return uc.Attachment{}, mapped(e)
	}
	u, e := q.RemoveBoundAttachmentUpload(ctx, db.RemoveBoundAttachmentUploadParams{WorkspaceID: p.WorkspaceID, ID: p.UploadID})
	if e != nil {
		return uc.Attachment{}, mapped(e)
	}
	up, e := r.publicUpload(ctx, u)
	if e != nil {
		return uc.Attachment{}, e
	}
	p, out, e := r.publicFile(ctx, w, in.AttachmentID)
	if e == nil {
		e = r.recordUpload(ctx, w, actor, u, "attachment_uploads.removed", up)
	}
	if e == nil {
		e = r.recordFile(ctx, w, actor, p, "attachments.removed", out)
	}
	return out, e
}
