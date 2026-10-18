package repository

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/furkanmeclis/traccia/backend/internal/database/db"
	uc "github.com/furkanmeclis/traccia/backend/internal/modules/comments/usecase"
	plate "github.com/furkanmeclis/traccia/backend/internal/modules/documents/usecase"
	"github.com/furkanmeclis/traccia/backend/internal/platform/dbtx"
	"github.com/furkanmeclis/traccia/backend/internal/platform/operations"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Postgres struct {
	pool *pgxpool.Pool
	q    *db.Queries
}

func New(pool *pgxpool.Pool, q *db.Queries) *Postgres { return &Postgres{pool, q} }
func (r *Postgres) queries(ctx context.Context) *db.Queries {
	if tx, ok := dbtx.Tx(ctx); ok {
		return r.q.WithTx(tx)
	}
	return r.q
}
func failure(code operations.Code) error { return &operations.Error{Code: code} }
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
func nullable(id uuid.UUID) pgtype.UUID { return pgtype.UUID{Bytes: id, Valid: id != uuid.Nil} }
func (r *Postgres) owner(ctx context.Context, w uuid.UUID, row db.IssueComment) (db.DocumentOwner, error) {
	o, e := r.queries(ctx).ReadDocumentOwnerByID(ctx, db.ReadDocumentOwnerByIDParams{WorkspaceUuid: w, Kind: "issue", ID: row.IssueID})
	return o, mapped(e)
}
func (r *Postgres) Active(ctx context.Context, w, id uuid.UUID) (bool, error) {
	o, e := r.queries(ctx).ReadDocumentOwner(ctx, db.ReadDocumentOwnerParams{WorkspaceUuid: w, Kind: "issue", Uuid: id})
	return o.Active.Bool, mapped(e)
}
func (r *Postgres) Get(ctx context.Context, w, id, revision uuid.UUID) (uc.Comment, uc.Revision, error) {
	row, e := r.queries(ctx).ReadComment(ctx, db.ReadCommentParams{Uuid: w, Uuid_2: id})
	if e != nil {
		return uc.Comment{}, uc.Revision{}, mapped(e)
	}
	o, e := r.owner(ctx, w, row)
	if e != nil {
		return uc.Comment{}, uc.Revision{}, e
	}
	if revision == uuid.Nil {
		revision = row.HeadRevision
	}
	return r.materialize(ctx, w, row, o, revision)
}
func revisionDTO(doc uuid.UUID, row db.CommentRevision) uc.Revision {
	var parent *string
	if row.ParentRevision.Valid {
		s := uuid.UUID(row.ParentRevision.Bytes).String()
		parent = &s
	}
	return uc.Revision{ID: row.Uuid, CommentID: doc, ParentRevision: parent, Kind: row.Kind, Channel: row.Channel, Deleted: row.Deleted, SourceRevision: uuidPointer(row.SourceRevision), ActorID: row.ActorUuid, OccurredAt: row.OccurredAt.Time}
}
func (r *Postgres) materialize(ctx context.Context, w uuid.UUID, row db.IssueComment, o db.DocumentOwner, revision uuid.UUID) (uc.Comment, uc.Revision, error) {
	q := r.queries(ctx)
	target, e := q.ReadCommentRevision(ctx, db.ReadCommentRevisionParams{WorkspaceID: row.WorkspaceID, CommentID: row.ID, Uuid: revision})
	if e != nil {
		return uc.Comment{}, uc.Revision{}, mapped(e)
	}
	journal, e := q.ReconstructCommentJournal(ctx, db.ReconstructCommentJournalParams{WorkspaceID: row.WorkspaceID, CommentID: row.ID, RevisionUuid: revision})
	if e != nil {
		return uc.Comment{}, uc.Revision{}, e
	}
	var content []plate.Node
	for _, part := range journal {
		if len(part.Checkpoint) > 0 {
			e = json.Unmarshal(part.Checkpoint, &content)
		} else {
			var patches []plate.Patch
			e = json.Unmarshal(part.Patches, &patches)
			if e == nil {
				content, e = plate.ApplyPatches(content, patches)
			}
		}
		if e != nil {
			return uc.Comment{}, uc.Revision{}, e
		}
	}
	if len(content) == 0 {
		return uc.Comment{}, uc.Revision{}, errors.New("comment journal has no checkpoint")
	}
	return uc.Comment{ID: row.Uuid, WorkspaceID: w, IssueID: o.Uuid, AuthorID: row.AuthorUuid, CreatedChannel: row.CreatedChannel, Deleted: target.Deleted, Format: row.Format, SchemaVersion: target.SchemaVersion, Revision: revision, Content: content, CreatedAt: row.CreatedAt.Time, UpdatedAt: target.OccurredAt.Time}, revisionDTO(row.Uuid, target), nil
}
func (r *Postgres) Mutate(ctx context.Context, w, actor uuid.UUID, in uc.Mutation) (uc.Comment, error) {
	if _, ok := dbtx.Tx(ctx); !ok {
		return uc.Comment{}, errors.New("comment mutation requires operation transaction")
	}
	q := r.queries(ctx)
	var row db.IssueComment
	var o db.DocumentOwner
	var e error
	newRevision := uuid.New()
	var parent uuid.UUID
	if in.Kind == "create" {
		o, e = q.ReadDocumentOwner(ctx, db.ReadDocumentOwnerParams{WorkspaceUuid: w, Kind: "issue", Uuid: in.IssueID})
	} else {
		row, e = q.ReadComment(ctx, db.ReadCommentParams{Uuid: w, Uuid_2: in.CommentID})
		if e == nil {
			o, e = r.owner(ctx, w, row)
		}
	}
	if e != nil {
		return uc.Comment{}, mapped(e)
	}
	if in.Kind != "create" && row.HeadRevision != in.ExpectedRevision {
		return uc.Comment{}, failure(operations.VersionConflict)
	}
	content := in.Content
	if in.Kind == "patch" || in.Kind == "restore" || in.Kind == "delete" {
		source := row.HeadRevision
		if in.Kind == "restore" {
			source = in.SourceRevision
		}
		old, oldRevision, err := r.materialize(ctx, w, row, o, source)
		if err != nil {
			return uc.Comment{}, err
		}
		if in.Kind == "restore" && oldRevision.Deleted {
			return uc.Comment{}, failure(operations.Conflict)
		}
		content = old.Content
		if in.Kind == "patch" {
			content, e = plate.ApplyPatches(content, in.Patches)
			if e != nil {
				return uc.Comment{}, e
			}
		}
	}
	if details := uc.ValidateContent(content); len(details) > 0 {
		return uc.Comment{}, &operations.Error{Code: operations.Validation, Details: details}
	}
	if e = r.validateReferences(ctx, w, content); e != nil {
		return uc.Comment{}, e
	}
	if e = r.lockGraph(ctx, w, o, content); e != nil {
		return uc.Comment{}, e
	}
	oldOwner := o
	o, e = q.ReadDocumentOwner(ctx, db.ReadDocumentOwnerParams{WorkspaceUuid: w, Kind: "issue", Uuid: o.Uuid})
	if e != nil {
		return uc.Comment{}, mapped(e)
	}
	if o.TeamUuid != oldOwner.TeamUuid {
		return uc.Comment{}, failure(operations.VersionConflict)
	}
	if !o.Active.Bool && in.Kind != "delete" {
		return uc.Comment{}, failure(operations.Conflict)
	}
	if in.Kind == "create" {
		params := db.InsertCommentParams{Uuid: uuid.New(), WorkspaceID: o.WorkspaceID, IssueID: o.ID, AuthorUuid: actor, CreatedChannel: in.Channel, HeadRevision: newRevision}
		row, e = q.InsertComment(ctx, params)
	} else {
		row, e = q.LockComment(ctx, db.LockCommentParams{Uuid: w, Uuid_2: in.CommentID})
		if e == nil && row.HeadRevision != in.ExpectedRevision {
			return uc.Comment{}, failure(operations.VersionConflict)
		}
		if row.Deleted && in.Kind != "restore" {
			return uc.Comment{}, failure(operations.Conflict)
		}
		parent = row.HeadRevision
	}
	if e != nil {
		return uc.Comment{}, mapped(e)
	}
	if in.Kind != "create" {
		row, e = q.AdvanceComment(ctx, db.AdvanceCommentParams{WorkspaceID: row.WorkspaceID, ID: row.ID, HeadRevision: newRevision, Deleted: in.Kind == "delete"})
		if e != nil {
			return uc.Comment{}, e
		}
	}
	now, e := q.DatabaseTime(ctx)
	if e != nil {
		return uc.Comment{}, e
	}
	out := uc.Comment{ID: row.Uuid, WorkspaceID: w, IssueID: o.Uuid, AuthorID: row.AuthorUuid, CreatedChannel: row.CreatedChannel, Deleted: row.Deleted, Format: plate.Format, SchemaVersion: plate.SchemaVersion, Revision: newRevision, Content: content, CreatedAt: row.CreatedAt.Time, UpdatedAt: now.Time}
	if e = r.record(ctx, w, actor, o, row, in, parent, out, now); e != nil {
		return uc.Comment{}, e
	}
	if e = r.references(ctx, w, row, content); e != nil {
		return uc.Comment{}, e
	}
	return out, nil
}

func uuidPointer(v pgtype.UUID) *string {
	if !v.Valid {
		return nil
	}
	s := uuid.UUID(v.Bytes).String()
	return &s
}
