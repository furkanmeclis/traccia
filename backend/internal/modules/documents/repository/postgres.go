package repository

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/furkanmeclis/traccia/backend/internal/database/db"
	uc "github.com/furkanmeclis/traccia/backend/internal/modules/documents/usecase"
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
func (r *Postgres) owner(ctx context.Context, w uuid.UUID, row db.Document) (db.DocumentOwner, error) {
	kind, id := "issue", row.IssueID.Int64
	if row.ProjectID.Valid {
		kind, id = "project", row.ProjectID.Int64
	}
	o, e := r.queries(ctx).ReadDocumentOwnerByID(ctx, db.ReadDocumentOwnerByIDParams{WorkspaceUuid: w, Kind: kind, ID: id})
	return o, mapped(e)
}
func (r *Postgres) OwnerActive(ctx context.Context, w uuid.UUID, kind string, id uuid.UUID) (bool, error) {
	o, e := r.queries(ctx).ReadDocumentOwner(ctx, db.ReadDocumentOwnerParams{WorkspaceUuid: w, Kind: kind, Uuid: id})
	return o.Active.Bool, mapped(e)
}
func (r *Postgres) Resolve(ctx context.Context, w uuid.UUID, in uc.ResolveInput) (*uc.Document, bool, error) {
	q := r.queries(ctx)
	o, e := q.ReadDocumentOwner(ctx, db.ReadDocumentOwnerParams{WorkspaceUuid: w, Kind: in.OwnerKind, Uuid: in.OwnerID})
	if e != nil {
		return nil, false, mapped(e)
	}
	row, e := q.ResolveOwnerDocument(ctx, db.ResolveOwnerDocumentParams{WorkspaceUuid: w, Kind: in.OwnerKind, Uuid: in.OwnerID})
	if errors.Is(e, pgx.ErrNoRows) {
		return nil, o.Active.Bool, nil
	}
	if e != nil {
		return nil, false, e
	}
	d, _, e := r.materialize(ctx, w, row, o, row.HeadRevision)
	return &d, o.Active.Bool, e
}
func (r *Postgres) Get(ctx context.Context, w, id, revision uuid.UUID) (uc.Document, uc.Revision, error) {
	row, e := r.queries(ctx).ReadDocument(ctx, db.ReadDocumentParams{Uuid: w, Uuid_2: id})
	if e != nil {
		return uc.Document{}, uc.Revision{}, mapped(e)
	}
	o, e := r.owner(ctx, w, row)
	if e != nil {
		return uc.Document{}, uc.Revision{}, e
	}
	if revision == uuid.Nil {
		revision = row.HeadRevision
	}
	return r.materialize(ctx, w, row, o, revision)
}
func revisionDTO(doc uuid.UUID, row db.DocumentRevision) uc.Revision {
	var parent *string
	if row.ParentRevision.Valid {
		s := uuid.UUID(row.ParentRevision.Bytes).String()
		parent = &s
	}
	return uc.Revision{ID: row.Uuid, DocumentID: doc, ParentRevision: parent, Kind: row.Kind, ActorID: row.ActorUuid, OccurredAt: row.OccurredAt.Time}
}
func (r *Postgres) materialize(ctx context.Context, w uuid.UUID, row db.Document, o db.DocumentOwner, revision uuid.UUID) (uc.Document, uc.Revision, error) {
	q := r.queries(ctx)
	target, e := q.ReadDocumentRevision(ctx, db.ReadDocumentRevisionParams{WorkspaceID: row.WorkspaceID, DocumentID: row.ID, Uuid: revision})
	if e != nil {
		return uc.Document{}, uc.Revision{}, mapped(e)
	}
	journal, e := q.ReconstructDocumentJournal(ctx, db.ReconstructDocumentJournalParams{WorkspaceID: row.WorkspaceID, DocumentID: row.ID, RevisionUuid: revision})
	if e != nil {
		return uc.Document{}, uc.Revision{}, e
	}
	var content []uc.Node
	for _, part := range journal {
		if len(part.Checkpoint) > 0 {
			e = json.Unmarshal(part.Checkpoint, &content)
		} else {
			var patches []uc.Patch
			e = json.Unmarshal(part.Patches, &patches)
			if e == nil {
				content, e = uc.ApplyPatches(content, patches)
			}
		}
		if e != nil {
			return uc.Document{}, uc.Revision{}, e
		}
	}
	if len(content) == 0 {
		return uc.Document{}, uc.Revision{}, errors.New("document journal has no checkpoint")
	}
	return uc.Document{ID: row.Uuid, WorkspaceID: w, OwnerKind: o.Kind, OwnerID: o.Uuid, Format: row.Format, SchemaVersion: target.SchemaVersion, Revision: revision, Content: content, CreatedAt: row.CreatedAt.Time, UpdatedAt: target.OccurredAt.Time}, revisionDTO(row.Uuid, target), nil
}
func (r *Postgres) lockOwner(ctx context.Context, w uuid.UUID, o db.DocumentOwner) (db.DocumentOwner, error) {
	q := r.queries(ctx)
	var e error
	if o.Kind == "issue" {
		_, e = q.LockIssue(ctx, db.LockIssueParams{Uuid: w, Uuid_2: o.Uuid})
	} else {
		_, e = q.LockProject(ctx, db.LockProjectParams{Uuid: w, Uuid_2: o.Uuid})
	}
	if e != nil {
		return o, mapped(e)
	}
	o, e = q.ReadDocumentOwner(ctx, db.ReadDocumentOwnerParams{WorkspaceUuid: w, Kind: o.Kind, Uuid: o.Uuid})
	if e == nil && !o.Active.Bool {
		e = failure(operations.Conflict)
	}
	return o, mapped(e)
}
func (r *Postgres) Mutate(ctx context.Context, w, actor uuid.UUID, in uc.Mutation) (uc.Document, error) {
	if _, ok := dbtx.Tx(ctx); !ok {
		return uc.Document{}, errors.New("document mutation requires operation transaction")
	}
	q := r.queries(ctx)
	var row db.Document
	var o db.DocumentOwner
	var e error
	newRevision := uuid.New()
	var parent uuid.UUID
	if in.Kind == "create" {
		o, e = q.ReadDocumentOwner(ctx, db.ReadDocumentOwnerParams{WorkspaceUuid: w, Kind: in.OwnerKind, Uuid: in.OwnerID})
	} else {
		row, e = q.ReadDocument(ctx, db.ReadDocumentParams{Uuid: w, Uuid_2: in.DocumentID})
		if e == nil {
			o, e = r.owner(ctx, w, row)
		}
	}
	if e != nil {
		return uc.Document{}, mapped(e)
	}
	content := in.Content
	if in.Kind == "patch" || in.Kind == "restore" {
		source := row.HeadRevision
		if in.Kind == "restore" {
			source = in.SourceRevision
		}
		old, _, err := r.materialize(ctx, w, row, o, source)
		if err != nil {
			return uc.Document{}, err
		}
		content = old.Content
		if in.Kind == "patch" {
			content, e = uc.ApplyPatches(content, in.Patches)
			if e != nil {
				return uc.Document{}, e
			}
		}
	}
	if details := uc.ValidateContent(content); len(details) > 0 {
		return uc.Document{}, &operations.Error{Code: operations.Validation, Details: details}
	}
	if e = r.validateReferences(ctx, w, content); e != nil {
		return uc.Document{}, e
	}
	if e = r.lockGraph(ctx, w, o, content, nil); e != nil {
		return uc.Document{}, e
	}
	o, e = r.lockOwner(ctx, w, o)
	if e != nil {
		return uc.Document{}, e
	}
	if in.Kind == "create" {
		params := db.InsertDocumentParams{Uuid: uuid.New(), WorkspaceID: o.WorkspaceID, HeadRevision: newRevision}
		if o.Kind == "issue" {
			params.IssueID = pgtype.Int8{Int64: o.ID, Valid: true}
		} else {
			params.ProjectID = pgtype.Int8{Int64: o.ID, Valid: true}
		}
		row, e = q.InsertDocument(ctx, params)
	} else {
		row, e = q.LockDocument(ctx, db.LockDocumentParams{Uuid: w, Uuid_2: in.DocumentID})
		if e == nil && row.HeadRevision != in.ExpectedRevision {
			return uc.Document{}, failure(operations.VersionConflict)
		}
		parent = row.HeadRevision
	}
	if e != nil {
		return uc.Document{}, mapped(e)
	}
	if in.Kind != "create" {
		row, e = q.AdvanceDocument(ctx, db.AdvanceDocumentParams{WorkspaceID: row.WorkspaceID, ID: row.ID, HeadRevision: newRevision})
		if e != nil {
			return uc.Document{}, e
		}
	}
	now, e := q.DatabaseTime(ctx)
	if e != nil {
		return uc.Document{}, e
	}
	out := uc.Document{ID: row.Uuid, WorkspaceID: w, OwnerKind: o.Kind, OwnerID: o.Uuid, Format: uc.Format, SchemaVersion: uc.SchemaVersion, Revision: newRevision, Content: content, CreatedAt: row.CreatedAt.Time, UpdatedAt: now.Time}
	if e = r.record(ctx, w, actor, o, row, in, parent, out, now); e != nil {
		return uc.Document{}, e
	}
	if e = r.references(ctx, w, row, content); e != nil {
		return uc.Document{}, e
	}
	return out, nil
}
