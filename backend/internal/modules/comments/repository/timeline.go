package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/furkanmeclis/traccia/backend/internal/database/db"
	attachment "github.com/furkanmeclis/traccia/backend/internal/modules/attachments/usecase"
	uc "github.com/furkanmeclis/traccia/backend/internal/modules/comments/usecase"
	plate "github.com/furkanmeclis/traccia/backend/internal/modules/documents/usecase"
	"github.com/furkanmeclis/traccia/backend/internal/platform/operations"
	"github.com/google/uuid"
)

func (r *Postgres) Timeline(ctx context.Context, w uuid.UUID, in uc.TimelineInput) ([]uc.TimelineItem, error) {
	out := []uc.TimelineItem{}
	q := r.queries(ctx)
	if _, e := q.ReadDocumentOwner(ctx, db.ReadDocumentOwnerParams{WorkspaceUuid: w, Kind: "issue", Uuid: in.IssueID}); e != nil {
		return out, mapped(e)
	}
	anchor, e := plate.CursorAnchor(w, in, in.After)
	if e != nil {
		return out, e
	}
	if anchor != uuid.Nil {
		ok, e := q.TimelineAnchorExists(ctx, db.TimelineAnchorExistsParams{WorkspaceUuid: w, IssueUuid: nullable(in.IssueID), Uuid: anchor})
		if e != nil {
			return out, e
		}
		if !ok {
			return out, failure(operations.Validation)
		}
	}
	rows, e := q.ListIssueTimeline(ctx, db.ListIssueTimelineParams{WorkspaceUuid: w, IssueUuid: nullable(in.IssueID), AfterUuid: anchor, RowLimit: in.Limit + 1})
	if e != nil {
		return out, e
	}
	for _, row := range rows {
		v := uc.TimelineItem{ID: row.Uuid, WorkspaceID: w, IssueID: in.IssueID, EventName: row.EventName, ActorID: row.ActorUuid, Channel: row.SourceChannel, Name: row.Name, Version: row.Version, OccurredAt: row.OccurredAt.Time}
		switch {
		case len(row.IssueSnapshot) > 0:
			v.Detail.Kind = "issue"
			v.Detail.Issue = &uc.IssueDetail{}
			e = json.Unmarshal(row.IssueSnapshot, &v.Detail.Issue.Snapshot)
		case len(row.RelationSnapshot) > 0:
			v.Detail.Kind = "relation"
			v.Detail.Relation = &uc.RelationDetail{}
			e = json.Unmarshal(row.RelationSnapshot, &v.Detail.Relation.Snapshot)
		case row.DocumentUuid.Valid && row.DocumentRevision.Valid:
			v.Detail.Kind = "document"
			v.Detail.Document = &uc.DocumentDetail{DocumentID: uuid.UUID(row.DocumentUuid.Bytes), Revision: uuid.UUID(row.DocumentRevision.Bytes), Kind: row.DocumentKind.String, SchemaVersion: row.DocumentSchemaVersion.Int32}
		case row.CommentUuid.Valid && row.CommentRevision.Valid:
			v.Detail.Kind = "comment"
			v.Detail.Comment = &uc.CommentDetail{CommentID: uuid.UUID(row.CommentUuid.Bytes), Revision: uuid.UUID(row.CommentRevision.Bytes), Kind: row.CommentKind.String, Deleted: row.CommentDeleted.Bool, AuthorID: uuid.UUID(row.CommentAuthor.Bytes), CreatedChannel: row.CommentCreatedChannel.String}
		case len(row.AttachmentSnapshot) > 0 && (row.EventName == "attachments.bound" || row.EventName == "attachments.removed"):
			var file attachment.Attachment
			e = json.Unmarshal(row.AttachmentSnapshot, &file)
			if e == nil && (file.WorkspaceID != w || file.IssueID == nil || *file.IssueID != in.IssueID.String()) {
				e = fmt.Errorf("attachment event ownership mismatch")
			}
			v.Detail.Kind = "attachment"
			v.Detail.Attachment = &uc.AttachmentDetail{AttachmentID: file.ID, FileRevision: file.FileRevision, Version: file.Version, Filename: file.Filename, Size: file.SizeBytes, SHA256: file.SHA256, Removed: file.Removed}
		default:
			v.Detail.Kind = "legacy"
			v.Detail.Legacy = &uc.LegacyDetail{ResourceType: row.ResourceType, ResourceID: uuidPointer(row.ResourceUuid)}
		}
		if e != nil {
			return out, e
		}
		out = append(out, v)
	}
	return out, nil
}
