package repository

import (
	"context"
	"github.com/furkanmeclis/traccia/backend/internal/database/db"
	uc "github.com/furkanmeclis/traccia/backend/internal/modules/comments/usecase"
	plate "github.com/furkanmeclis/traccia/backend/internal/modules/documents/usecase"
	"github.com/furkanmeclis/traccia/backend/internal/platform/operations"
	"github.com/google/uuid"
)

func (r *Postgres) List(ctx context.Context, w uuid.UUID, in uc.ListInput) ([]uc.Comment, error) {
	out := []uc.Comment{}
	q := r.queries(ctx)
	o, e := q.ReadDocumentOwner(ctx, db.ReadDocumentOwnerParams{WorkspaceUuid: w, Kind: "issue", Uuid: in.IssueID})
	if e != nil {
		return out, mapped(e)
	}
	anchor, e := plate.CursorAnchor(w, in, in.After)
	if e != nil {
		return out, e
	}
	if anchor != uuid.Nil {
		ok, e := q.CommentAnchorExists(ctx, db.CommentAnchorExistsParams{Uuid: w, Uuid_2: in.IssueID, Uuid_3: anchor})
		if e != nil {
			return out, e
		}
		if !ok {
			return out, failure(operations.Validation)
		}
	}
	rows, e := q.ListIssueComments(ctx, db.ListIssueCommentsParams{WorkspaceUuid: w, IssueUuid: in.IssueID, IncludeDeleted: in.IncludeDeleted, AfterUuid: anchor, RowLimit: in.Limit + 1})
	if e != nil {
		return out, e
	}
	for _, row := range rows {
		c, _, e := r.materialize(ctx, w, row, o, row.HeadRevision)
		if e != nil {
			return out, e
		}
		out = append(out, c)
	}
	return out, nil
}
