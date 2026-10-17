package repository

import (
	"context"
	"errors"
	"time"

	"github.com/furkanmeclis/traccia/backend/internal/database/db"
	"github.com/furkanmeclis/traccia/backend/internal/modules/workspaces/usecase"
	"github.com/furkanmeclis/traccia/backend/internal/platform/crypto"
	"github.com/furkanmeclis/traccia/backend/internal/platform/dbtx"
	"github.com/furkanmeclis/traccia/backend/internal/platform/operations"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Postgres struct {
	pool *pgxpool.Pool
	q    *db.Queries
	box  *crypto.SecretBox
}

func New(pool *pgxpool.Pool, q *db.Queries, boxes ...*crypto.SecretBox) *Postgres {
	r := &Postgres{pool: pool, q: q}
	if len(boxes) > 0 {
		r.box = boxes[0]
	}
	return r
}
func (r *Postgres) queries(ctx context.Context) *db.Queries {
	if tx, ok := dbtx.Tx(ctx); ok {
		return r.q.WithTx(tx)
	}
	return r.q
}
func mapMissing(err error, code operations.Code) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return &operations.Error{Code: code}
	}
	return err
}
func workspace(w db.Workspace) usecase.Workspace {
	return usecase.Workspace{ID: w.Uuid, Name: w.Name, CreatedAt: w.CreatedAt.Time, UpdatedAt: w.UpdatedAt.Time, Version: w.Version}
}
func credential(c db.WorkspaceToolCredential) usecase.Credential {
	var revoked *time.Time
	if c.RevokedAt.Valid {
		revoked = &c.RevokedAt.Time
	}
	return usecase.Credential{ID: c.Uuid, Name: c.Name, Audience: c.Audience, Scopes: c.Scopes, ExpiresAt: c.ExpiresAt.Time, RevokedAt: revoked, CreatedAt: c.CreatedAt.Time}
}
func (r *Postgres) ActiveActor(ctx context.Context, id uuid.UUID) error {
	_, err := r.queries(ctx).GetProductActor(ctx, id)
	return mapMissing(err, operations.Unauthenticated)
}
func (r *Postgres) ActiveOwner(ctx context.Context, id uuid.UUID) error {
	_, err := r.queries(ctx).GetProductOwner(ctx, id)
	return mapMissing(err, operations.Forbidden)
}
func (r *Postgres) Resolve(ctx context.Context, w, u uuid.UUID) (usecase.Access, error) {
	row, err := r.queries(ctx).ResolveProductWorkspace(ctx, db.ResolveProductWorkspaceParams{WorkspaceUuid: w, UserUuid: u})
	if err != nil {
		return usecase.Access{}, mapMissing(err, operations.Forbidden)
	}
	return usecase.Access{UserID: u, WorkspaceID: w, Role: row.Role}, nil
}
func (r *Postgres) ResolveCredential(ctx context.Context, hash []byte) (usecase.Access, error) {
	row, err := r.queries(ctx).ResolveWorkspaceToolCredential(ctx, hash)
	if err != nil {
		return usecase.Access{}, mapMissing(err, operations.Unauthenticated)
	}
	return usecase.Access{UserID: row.UserUuid, WorkspaceID: row.WorkspaceUuid, Role: row.Role, Scopes: row.Scopes, Audience: row.Audience, ExpiresAt: row.ExpiresAt.Time}, nil
}
func (r *Postgres) Create(ctx context.Context, user uuid.UUID, name string) (usecase.Workspace, error) {
	tx, ambient := dbtx.Tx(ctx)
	if !ambient {
		var err error
		tx, err = r.pool.Begin(ctx)
		if err != nil {
			return usecase.Workspace{}, err
		}
		defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	}
	q := r.q.WithTx(tx)
	actor, err := q.GetProductOwner(ctx, user)
	if err != nil {
		return usecase.Workspace{}, mapMissing(err, operations.Unauthenticated)
	}
	w, err := q.CreateProductWorkspace(ctx, name)
	if err != nil {
		return usecase.Workspace{}, err
	}
	if err := q.AddProductWorkspaceOwner(ctx, db.AddProductWorkspaceOwnerParams{WorkspaceID: w.ID, UserID: actor.ID}); err != nil {
		return usecase.Workspace{}, err
	}
	if !ambient {
		if err := tx.Commit(ctx); err != nil {
			return usecase.Workspace{}, err
		}
	}
	return workspace(w), nil
}
func (r *Postgres) List(ctx context.Context, user, after uuid.UUID, limit int32) ([]usecase.Workspace, error) {
	actor, err := r.queries(ctx).GetProductActor(ctx, user)
	if err != nil {
		return nil, mapMissing(err, operations.Unauthenticated)
	}
	rows, err := r.queries(ctx).ListProductWorkspaces(ctx, db.ListProductWorkspacesParams{UserID: actor.ID, AfterUuid: after, RowLimit: limit})
	result := make([]usecase.Workspace, 0, len(rows))
	for _, w := range rows {
		result = append(result, workspace(w))
	}
	return result, err
}
func (r *Postgres) Get(ctx context.Context, w, u uuid.UUID) (usecase.Workspace, error) {
	row, err := r.queries(ctx).ResolveProductWorkspace(ctx, db.ResolveProductWorkspaceParams{WorkspaceUuid: w, UserUuid: u})
	if err != nil {
		return usecase.Workspace{}, mapMissing(err, operations.Forbidden)
	}
	return usecase.Workspace{ID: row.Uuid, Name: row.Name, CreatedAt: row.CreatedAt.Time, UpdatedAt: row.UpdatedAt.Time, Version: row.Version}, nil
}
func (r *Postgres) Update(ctx context.Context, w uuid.UUID, name string, expected int64) (usecase.Workspace, error) {
	row, err := r.queries(ctx).UpdateProductWorkspace(ctx, db.UpdateProductWorkspaceParams{WorkspaceUuid: w, Name: name, ExpectedVersion: expected})
	return workspace(row), mapMissing(err, operations.VersionConflict)
}
func (r *Postgres) CreateCredential(ctx context.Context, w, u uuid.UUID, in usecase.CreateCredentialInput, hash []byte) (usecase.Credential, error) {
	// Re-resolve at insertion so no caller can bind a foreign actor to a workspace.
	member, err := r.queries(ctx).ResolveProductWorkspace(ctx, db.ResolveProductWorkspaceParams{WorkspaceUuid: w, UserUuid: u})
	if err != nil {
		return usecase.Credential{}, mapMissing(err, operations.Forbidden)
	}
	actor, err := r.queries(ctx).GetProductActor(ctx, u)
	if err != nil {
		return usecase.Credential{}, mapMissing(err, operations.Unauthenticated)
	}
	row, err := r.queries(ctx).CreateWorkspaceToolCredential(ctx, db.CreateWorkspaceToolCredentialParams{WorkspaceID: member.ID, UserID: actor.ID, Name: in.Name, TokenHash: hash, Audience: in.Audience, Scopes: in.Scopes, ExpiresAt: pgtype.Timestamptz{Time: in.ExpiresAt, Valid: true}})
	return credential(row), err
}
func (r *Postgres) ListCredentials(ctx context.Context, w, after uuid.UUID, limit int32) ([]usecase.Credential, error) {
	rows, err := r.queries(ctx).ListWorkspaceToolCredentials(ctx, db.ListWorkspaceToolCredentialsParams{WorkspaceUuid: w, AfterUuid: after, RowLimit: limit})
	result := make([]usecase.Credential, 0, len(rows))
	for _, c := range rows {
		result = append(result, credential(c))
	}
	return result, err
}
func (r *Postgres) RevokeCredential(ctx context.Context, w, c uuid.UUID) (bool, error) {
	count, err := r.queries(ctx).RevokeWorkspaceToolCredential(ctx, db.RevokeWorkspaceToolCredentialParams{WorkspaceUuid: w, CredentialUuid: c})
	return count > 0, err
}

var _ usecase.Repository = (*Postgres)(nil)
