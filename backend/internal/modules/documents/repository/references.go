package repository

import (
	"context"
	"github.com/furkanmeclis/traccia/backend/internal/database/db"
	uc "github.com/furkanmeclis/traccia/backend/internal/modules/documents/usecase"
	"github.com/furkanmeclis/traccia/backend/internal/platform/operations"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
)

func (r *Postgres) reference(ctx context.Context, w uuid.UUID, ref uc.Reference) (db.InsertDocumentReferenceParams, error) {
	q := r.queries(ctx)
	out := db.InsertDocumentReferenceParams{NodeUuid: ref.NodeID, Value: ref.Value}
	var id int64
	var e error
	switch ref.TargetKind {
	case "user":
		id, e = q.ResolveDocumentUserReference(ctx, db.ResolveDocumentUserReferenceParams{Uuid: w, Uuid_2: ref.TargetID})
		out.UserID = pgtype.Int8{Int64: id, Valid: true}
	case "issue":
		id, e = q.ResolveDocumentIssueReference(ctx, db.ResolveDocumentIssueReferenceParams{Uuid: w, Uuid_2: ref.TargetID})
		out.IssueID = pgtype.Int8{Int64: id, Valid: true}
	case "project":
		id, e = q.ResolveDocumentProjectReference(ctx, db.ResolveDocumentProjectReferenceParams{Uuid: w, Uuid_2: ref.TargetID})
		out.ProjectID = pgtype.Int8{Int64: id, Valid: true}
	default:
		e = failure(operations.Validation)
	}
	if e != nil {
		return out, &operations.Error{Code: operations.Validation, Details: []operations.FieldError{{Field: "content", Code: "invalid_reference"}}}
	}
	return out, nil
}
func (r *Postgres) validateReferences(ctx context.Context, w uuid.UUID, content []uc.Node) error {
	for _, ref := range uc.References(content) {
		if _, e := r.reference(ctx, w, ref); e != nil {
			return e
		}
	}
	return nil
}
func (r *Postgres) references(ctx context.Context, w uuid.UUID, row db.Document, content []uc.Node) error {
	q := r.queries(ctx)
	old, e := q.ListCurrentDocumentReferences(ctx, db.ListCurrentDocumentReferencesParams{WorkspaceID: row.WorkspaceID, DocumentID: row.ID})
	if e != nil {
		return e
	}
	current := map[uuid.UUID]db.DocumentReferencePublic{}
	for _, ref := range old {
		current[ref.NodeUuid] = ref
	}
	for _, ref := range uc.References(content) {
		previous, exists := current[ref.NodeID]
		delete(current, ref.NodeID)
		if exists && previous.TargetKind == ref.TargetKind && previous.TargetUuid == ref.TargetID {
			if e = q.UpdateDocumentReference(ctx, db.UpdateDocumentReferenceParams{WorkspaceID: row.WorkspaceID, Uuid: previous.Uuid, RevisionUuid: row.HeadRevision, Value: ref.Value}); e != nil {
				return e
			}
			continue
		}
		if exists {
			if e = q.DeactivateDocumentReference(ctx, db.DeactivateDocumentReferenceParams{WorkspaceID: row.WorkspaceID, Uuid: previous.Uuid}); e != nil {
				return e
			}
		}
		params, err := r.reference(ctx, w, ref)
		if err != nil {
			return err
		}
		params.WorkspaceID = row.WorkspaceID
		params.DocumentID = row.ID
		params.RevisionUuid = row.HeadRevision
		if e = q.InsertDocumentReference(ctx, params); e != nil {
			return mapped(e)
		}
	}
	for _, ref := range current {
		if e = q.DeactivateDocumentReference(ctx, db.DeactivateDocumentReferenceParams{WorkspaceID: row.WorkspaceID, Uuid: ref.Uuid}); e != nil {
			return e
		}
	}
	return nil
}
func (r *Postgres) Revisions(ctx context.Context, w uuid.UUID, in uc.RevisionsInput) ([]uc.Revision, error) {
	out := []uc.Revision{}
	q := r.queries(ctx)
	row, e := q.ReadDocument(ctx, db.ReadDocumentParams{Uuid: w, Uuid_2: in.DocumentID})
	if e != nil {
		return out, mapped(e)
	}
	anchor, e := uc.CursorAnchor(w, in, in.After)
	if e != nil {
		return out, e
	}
	if anchor != uuid.Nil {
		ok, err := q.DocumentRevisionAnchorExists(ctx, db.DocumentRevisionAnchorExistsParams{WorkspaceID: row.WorkspaceID, DocumentID: row.ID, Uuid: anchor})
		if err != nil {
			return out, err
		}
		if !ok {
			return out, failure(operations.Validation)
		}
	}
	rows, e := q.ListDocumentRevisions(ctx, db.ListDocumentRevisionsParams{WorkspaceID: row.WorkspaceID, DocumentID: row.ID, AfterUuid: anchor, RowLimit: in.Limit + 1})
	if e != nil {
		return out, e
	}
	for _, revision := range rows {
		out = append(out, revisionDTO(row.Uuid, revision))
	}
	return out, nil
}
func (r *Postgres) Backlinks(ctx context.Context, w uuid.UUID, in uc.BacklinksInput) ([]uc.Backlink, error) {
	out := []uc.Backlink{}
	if _, e := r.reference(ctx, w, uc.Reference{TargetKind: in.TargetKind, TargetID: in.TargetID}); e != nil {
		return out, e
	}
	anchor, e := uc.CursorAnchor(w, in, in.After)
	if e != nil {
		return out, e
	}
	q := r.queries(ctx)
	if anchor != uuid.Nil {
		ok, err := q.DocumentBacklinkAnchorExists(ctx, db.DocumentBacklinkAnchorExistsParams{WorkspaceUuid: w, TargetKind: in.TargetKind, TargetUuid: in.TargetID, Uuid: anchor})
		if err != nil {
			return out, err
		}
		if !ok {
			return out, failure(operations.Validation)
		}
	}
	rows, e := q.ListDocumentBacklinks(ctx, db.ListDocumentBacklinksParams{WorkspaceUuid: w, TargetKind: in.TargetKind, TargetUuid: in.TargetID, AfterUuid: anchor, RowLimit: in.Limit + 1})
	if e != nil {
		return out, e
	}
	for _, ref := range rows {
		out = append(out, uc.Backlink{ID: ref.Uuid, DocumentID: ref.DocumentUuid, OwnerKind: ref.OwnerKind, OwnerID: ref.OwnerUuid, NodeID: ref.NodeUuid, Revision: ref.RevisionUuid, Value: ref.Value})
	}
	return out, nil
}
