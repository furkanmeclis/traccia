package repository

import (
	"context"
	"encoding/json"
	"github.com/furkanmeclis/traccia/backend/internal/database/db"
	uc "github.com/furkanmeclis/traccia/backend/internal/modules/attachments/usecase"
	"github.com/furkanmeclis/traccia/backend/internal/platform/operations"
	"github.com/google/uuid"
)

func anchorError(found bool, e error) error {
	if e != nil {
		return e
	}
	if !found {
		return invalid("after", "invalid")
	}
	return nil
}
func (r *Postgres) ListUploads(ctx context.Context, w, actor uuid.UUID, in uc.UploadListInput) ([]uc.Upload, error) {
	out := []uc.Upload{}
	anchor, e := uc.CursorAnchor(w, in, in.After)
	if e != nil {
		return out, e
	}
	draft := uuid.Nil
	if in.DraftID != "" {
		draft, _ = uuid.Parse(in.DraftID)
	}
	q := r.queries(ctx)
	if anchor != uuid.Nil {
		ok, e := q.AttachmentUploadAnchorExists(ctx, db.AttachmentUploadAnchorExistsParams{WorkspaceUuid: w, ActorUuid: actor, Uuid: anchor, DraftFilter: draft})
		if e = anchorError(ok, e); e != nil {
			return out, e
		}
	}
	stateFilter := in.State
	if stateFilter == "" {
		stateFilter = "any"
	}
	rows, e := q.ListAttachmentUploads(ctx, db.ListAttachmentUploadsParams{WorkspaceUuid: w, ActorUuid: actor, AfterUuid: anchor, DraftFilter: draft, StateFilter: stateFilter, RowLimit: in.Limit + 1})
	if e != nil {
		return out, e
	}
	now, e := r.now(ctx)
	if e != nil {
		return out, e
	}
	for _, row := range rows {
		out = append(out, uploadDTO(row, now))
	}
	return out, nil
}
func (r *Postgres) List(ctx context.Context, w uuid.UUID, in uc.ListInput) ([]uc.Attachment, bool, error) {
	out := []uc.Attachment{}
	q := r.queries(ctx)
	owner, e := q.ReadDocumentOwner(ctx, db.ReadDocumentOwnerParams{WorkspaceUuid: w, Kind: "issue", Uuid: in.IssueID})
	if e != nil {
		return out, false, mapped(e)
	}
	anchor, e := uc.CursorAnchor(w, in, in.After)
	if e != nil {
		return out, false, e
	}
	if anchor != uuid.Nil {
		ok, e := q.AttachmentAnchorExists(ctx, db.AttachmentAnchorExistsParams{WorkspaceUuid: w, IssueUuid: nullable(in.IssueID), Uuid: anchor})
		if e = anchorError(ok, e); e != nil {
			return out, false, e
		}
	}
	rows, e := q.ListIssueAttachments(ctx, db.ListIssueAttachmentsParams{WorkspaceUuid: w, IssueUuid: nullable(in.IssueID), Removed: in.Removed, Search: in.Search, AfterUuid: anchor, RowLimit: in.Limit + 1})
	if e != nil {
		return out, false, e
	}
	now, e := r.now(ctx)
	if e != nil {
		return out, false, e
	}
	for _, row := range rows {
		out = append(out, fileDTO(row, now))
	}
	return out, owner.Active.Bool, nil
}
func (r *Postgres) UploadHistory(ctx context.Context, w, actor uuid.UUID, in uc.UploadHistoryInput) ([]uc.UploadHistory, error) {
	out := []uc.UploadHistory{}
	q := r.queries(ctx)
	p, e := q.ReadOwnedAttachmentUpload(ctx, db.ReadOwnedAttachmentUploadParams{WorkspaceUuid: w, ActorUuid: actor, Uuid: in.UploadID, DraftUuid: in.DraftID})
	if e != nil {
		return out, mapped(e)
	}
	anchor, e := uc.CursorAnchor(w, in, in.After)
	if e != nil {
		return out, e
	}
	if anchor != uuid.Nil {
		ok, e := q.AttachmentUploadHistoryAnchorExists(ctx, db.AttachmentUploadHistoryAnchorExistsParams{WorkspaceID: p.WorkspaceID, UploadID: p.ID, Uuid: anchor})
		if e = anchorError(ok, e); e != nil {
			return out, e
		}
	}
	rows, e := q.ListAttachmentUploadHistory(ctx, db.ListAttachmentUploadHistoryParams{WorkspaceID: p.WorkspaceID, UploadID: p.ID, AfterUuid: anchor, RowLimit: in.Limit + 1})
	if e != nil {
		return out, e
	}
	for _, row := range rows {
		item := uc.UploadHistory{ID: row.Uuid, ActorID: row.ActorUuid, EventName: row.EventName, Version: row.Version, OccurredAt: row.OccurredAt.Time}
		if e = json.Unmarshal(row.Snapshot, &item.Snapshot); e != nil {
			return out, e
		}
		out = append(out, item)
	}
	return out, nil
}
func (r *Postgres) History(ctx context.Context, w, actor uuid.UUID, in uc.HistoryInput) ([]uc.History, error) {
	out := []uc.History{}
	if _, e := r.Get(ctx, w, actor, uc.AttachmentInput{AttachmentID: in.AttachmentID, DraftID: in.DraftID}); e != nil {
		return out, e
	}
	p, _, e := r.publicFile(ctx, w, in.AttachmentID)
	if e != nil {
		return out, e
	}
	if p.ActorUuid != actor || in.DraftID == "" || !sameDraft(in.DraftID, p.DraftUuid) {
		return out, failure(operations.NotFound)
	}
	anchor, e := uc.CursorAnchor(w, in, in.After)
	if e != nil {
		return out, e
	}
	q := r.queries(ctx)
	if anchor != uuid.Nil {
		ok, e := q.AttachmentHistoryAnchorExists(ctx, db.AttachmentHistoryAnchorExistsParams{WorkspaceID: p.WorkspaceID, AttachmentID: p.ID, Uuid: anchor})
		if e = anchorError(ok, e); e != nil {
			return out, e
		}
	}
	rows, e := q.ListAttachmentHistory(ctx, db.ListAttachmentHistoryParams{WorkspaceID: p.WorkspaceID, AttachmentID: p.ID, AfterUuid: anchor, RowLimit: in.Limit + 1})
	if e != nil {
		return out, e
	}
	for _, row := range rows {
		item := uc.History{ID: row.Uuid, ActorID: row.ActorUuid, EventName: row.EventName, Version: row.Version, OccurredAt: row.OccurredAt.Time}
		if e = json.Unmarshal(row.Snapshot, &item.Snapshot); e != nil {
			return out, e
		}
		out = append(out, item)
	}
	return out, nil
}
