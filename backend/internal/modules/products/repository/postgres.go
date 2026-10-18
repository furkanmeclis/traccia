package repository

import (
	"context"
	"errors"
	"sort"

	"github.com/furkanmeclis/traccia/backend/internal/database/db"
	uc "github.com/furkanmeclis/traccia/backend/internal/modules/products/usecase"
	projects "github.com/furkanmeclis/traccia/backend/internal/modules/projects/usecase"
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

func New(p *pgxpool.Pool, q *db.Queries) *Postgres { return &Postgres{p, q} }
func (r *Postgres) queries(ctx context.Context) *db.Queries {
	if tx, ok := dbtx.Tx(ctx); ok {
		return r.q.WithTx(tx)
	}
	return r.q
}
func mutation(ctx context.Context) error {
	if _, ok := dbtx.Tx(ctx); !ok {
		return errors.New("product mutation requires operation transaction")
	}
	return nil
}
func mapped(e error, missing operations.Code) error {
	if errors.Is(e, pgx.ErrNoRows) {
		return &operations.Error{Code: missing}
	}
	var p *pgconn.PgError
	if errors.As(e, &p) {
		switch p.Code {
		case "23505":
			return &operations.Error{Code: operations.Conflict}
		case "23503", "23514":
			return &operations.Error{Code: operations.Validation}
		}
	}
	return e
}
func dateString(d pgtype.Date) *string {
	if !d.Valid {
		return nil
	}
	s := d.Time.Format("2006-01-02")
	return &s
}

// snapshot pins the aggregate version and every returned row to one database snapshot.
func snapshot[T any](ctx context.Context, r *Postgres, read func(context.Context) (T, error)) (T, error) {
	if _, ok := dbtx.Tx(ctx); ok {
		return read(ctx)
	}
	var zero T
	tx, e := r.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if e != nil {
		return zero, e
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	out, e := read(dbtx.WithTx(ctx, tx))
	if e != nil {
		return zero, e
	}
	if e = tx.Commit(ctx); e != nil {
		return zero, e
	}
	return out, nil
}

func product(w uuid.UUID, row db.Product) uc.Product {
	return uc.Product{ID: row.Uuid, WorkspaceID: w, Name: row.Name, Summary: row.Summary, RepositoryURL: row.RepositoryUrl, RepositoryProvider: row.RepositoryProvider, RepositoryReference: row.RepositoryReference, Version: row.Version, Archived: row.Archived, CreatedAt: row.CreatedAt.Time, UpdatedAt: row.UpdatedAt.Time}
}
func environment(w, p uuid.UUID, row db.ProductEnvironment) uc.Environment {
	return uc.Environment{ID: row.Uuid, WorkspaceID: w, ProductID: p, Name: row.Name, Kind: row.Kind, URL: row.Url, DeployURL: row.DeployUrl, DeployProvider: row.DeployProvider, DeployReference: row.DeployReference, Summary: row.Summary, Version: row.Version, Archived: row.Archived, CreatedAt: row.CreatedAt.Time, UpdatedAt: row.UpdatedAt.Time}
}
func project(w uuid.UUID, row db.ListProductProjectsRow) projects.Project {
	var lead *string
	if row.LeadUserUuid.Valid {
		v := uuid.UUID(row.LeadUserUuid.Bytes).String()
		lead = &v
	}
	return projects.Project{ID: row.Uuid, WorkspaceID: w, Name: row.Name, Summary: row.Summary, LeadUserID: lead, StartDate: dateString(row.StartDate), TargetDate: dateString(row.TargetDate), Version: row.Version, Archived: row.Archived, CreatedAt: row.CreatedAt.Time, UpdatedAt: row.UpdatedAt.Time}
}
func (r *Postgres) GetProduct(ctx context.Context, w, p uuid.UUID) (uc.Product, error) {
	row, e := r.queries(ctx).GetProduct(ctx, db.GetProductParams{Uuid: w, Uuid_2: p})
	return product(w, row), mapped(e, operations.NotFound)
}
func (r *Postgres) locked(ctx context.Context, w, p uuid.UUID, active bool) (db.Product, error) {
	if e := mutation(ctx); e != nil {
		return db.Product{}, e
	}
	row, e := r.queries(ctx).LockProduct(ctx, db.LockProductParams{Uuid: w, Uuid_2: p})
	if e != nil {
		return row, mapped(e, operations.NotFound)
	}
	if active && row.Archived {
		return row, &operations.Error{Code: operations.Conflict}
	}
	return row, nil
}
func (r *Postgres) CreateProduct(ctx context.Context, w uuid.UUID, in uc.CreateProductInput) (uc.Product, error) {
	if e := mutation(ctx); e != nil {
		return uc.Product{}, e
	}
	q := r.queries(ctx)
	wid, e := q.ProductInternalWorkspace(ctx, w)
	if e != nil {
		return uc.Product{}, mapped(e, operations.NotFound)
	}
	row, e := q.CreateProduct(ctx, db.CreateProductParams{WorkspaceID: wid, Name: in.Name, Summary: in.Summary, RepositoryUrl: in.RepositoryURL, RepositoryProvider: in.RepositoryProvider, RepositoryReference: in.RepositoryReference})
	return product(w, row), mapped(e, operations.NotFound)
}
func (r *Postgres) UpdateProduct(ctx context.Context, w uuid.UUID, in uc.UpdateProductInput) (uc.Product, error) {
	p, e := r.locked(ctx, w, in.ProductID, true)
	if e != nil {
		return uc.Product{}, e
	}
	row, e := r.queries(ctx).UpdateProduct(ctx, db.UpdateProductParams{ID: p.ID, Name: in.Name, Summary: in.Summary, RepositoryUrl: referenceValue(in.RepositoryURL), RepositoryProvider: referenceValue(in.RepositoryProvider), RepositoryReference: referenceValue(in.RepositoryReference), Version: in.ExpectedVersion})
	return product(w, row), mapped(e, operations.VersionConflict)
}
func (r *Postgres) ArchiveProduct(ctx context.Context, w uuid.UUID, in uc.ArchiveProductInput, archived bool) (uc.Product, error) {
	p, e := r.locked(ctx, w, in.ProductID, false)
	if e != nil {
		return uc.Product{}, e
	}
	row, e := r.queries(ctx).SetProductArchived(ctx, db.SetProductArchivedParams{ID: p.ID, Archived: archived, Version: in.ExpectedVersion})
	return product(w, row), mapped(e, operations.VersionConflict)
}
func (r *Postgres) ListProducts(ctx context.Context, w uuid.UUID, in uc.PageInput) ([]uc.Product, error) {
	after, e := uc.CursorAnchor(w, in, in.After)
	if e != nil {
		return nil, e
	}
	if after != uuid.Nil {
		if _, e = r.queries(ctx).GetProduct(ctx, db.GetProductParams{Uuid: w, Uuid_2: after}); e != nil {
			return nil, mapped(e, operations.Validation)
		}
	}
	projectID := uuid.Nil
	if in.ProjectID != "" {
		projectID, _ = uuid.Parse(in.ProjectID)
	}
	rows, e := r.queries(ctx).ListProducts(ctx, db.ListProductsParams{WorkspaceUuid: w, Archived: in.Archived, AfterUuid: after, Search: in.Q, ProjectUuid: projectID, RowLimit: in.Limit + 1})
	out := make([]uc.Product, 0, len(rows))
	for _, row := range rows {
		out = append(out, product(w, row))
	}
	return out, e
}

// Product precedes projects in every mutation. Project UUID ordering matches
// project→team ordering in the project module; archived references are retained.
func (r *Postgres) SetProjects(ctx context.Context, w uuid.UUID, in uc.SetProjectsInput) (int64, error) {
	p, e := r.locked(ctx, w, in.ProductID, true)
	if e != nil {
		return 0, e
	}
	q := r.queries(ctx)
	v, e := q.BumpProduct(ctx, db.BumpProductParams{ID: p.ID, Version: in.ExpectedProductVersion})
	if e != nil {
		return 0, mapped(e, operations.VersionConflict)
	}
	rows, e := q.AllProductProjects(ctx, db.AllProductProjectsParams{WorkspaceID: p.WorkspaceID, ProductID: p.ID})
	if e != nil {
		return 0, e
	}
	existing := map[uuid.UUID]bool{}
	for _, row := range rows {
		existing[row.Uuid] = !row.AssociationArchived
	}
	ordered := make([]string, len(in.ProjectIDs))
	for i, id := range in.ProjectIDs {
		u, _ := uuid.Parse(id)
		ordered[i] = u.String()
	}
	sort.Strings(ordered)
	internal := make([]int64, 0, len(ordered))
	for _, id := range ordered {
		u, _ := uuid.Parse(id)
		pr, e := q.LockProject(ctx, db.LockProjectParams{Uuid: w, Uuid_2: u})
		if e != nil {
			return 0, mapped(e, operations.NotFound)
		}
		if pr.Archived && !existing[pr.Uuid] {
			return 0, &operations.Error{Code: operations.Conflict}
		}
		internal = append(internal, pr.ID)
	}
	if e = q.DeactivateProductProjects(ctx, db.DeactivateProductProjectsParams{WorkspaceID: p.WorkspaceID, ProductID: p.ID}); e != nil {
		return 0, e
	}
	for _, id := range internal {
		if e = q.UpsertProductProject(ctx, db.UpsertProductProjectParams{WorkspaceID: p.WorkspaceID, ProductID: p.ID, ProjectID: id}); e != nil {
			return 0, e
		}
	}
	return v, nil
}
func (r *Postgres) ListProjects(ctx context.Context, w uuid.UUID, in uc.ProductPageInput) (uc.Product, []projects.Project, error) {
	type result struct {
		p    uc.Product
		rows []projects.Project
	}
	out, e := snapshot(ctx, r, func(ctx context.Context) (result, error) {
		after, e := uc.CursorAnchor(w, in, in.After)
		if e != nil {
			return result{}, e
		}
		p, e := r.queries(ctx).GetProduct(ctx, db.GetProductParams{Uuid: w, Uuid_2: in.ProductID})
		if e != nil {
			return result{}, mapped(e, operations.NotFound)
		}
		if after != uuid.Nil {
			found, e := r.queries(ctx).ProductProjectAnchorExists(ctx, db.ProductProjectAnchorExistsParams{WorkspaceID: p.WorkspaceID, ProductID: p.ID, Uuid: after})
			if e != nil {
				return result{}, e
			}
			if !found {
				return result{}, &operations.Error{Code: operations.Validation}
			}
		}
		rows, e := r.queries(ctx).ListProductProjects(ctx, db.ListProductProjectsParams{WorkspaceID: p.WorkspaceID, ProductID: p.ID, Archived: in.Archived, AfterUuid: after, RowLimit: in.Limit + 1})
		list := make([]projects.Project, 0, len(rows))
		for _, row := range rows {
			list = append(list, project(w, row))
		}
		return result{p: product(w, p), rows: list}, e
	})
	return out.p, out.rows, e
}
func (r *Postgres) GetEnvironment(ctx context.Context, w uuid.UUID, in uc.EnvironmentInput) (uc.Environment, uc.Product, error) {
	type result struct {
		p   uc.Product
		row uc.Environment
	}
	out, e := snapshot(ctx, r, func(ctx context.Context) (result, error) {
		p, e := r.queries(ctx).GetProduct(ctx, db.GetProductParams{Uuid: w, Uuid_2: in.ProductID})
		if e != nil {
			return result{}, mapped(e, operations.NotFound)
		}
		row, e := r.queries(ctx).GetProductEnvironment(ctx, db.GetProductEnvironmentParams{WorkspaceID: p.WorkspaceID, ProductID: p.ID, Uuid: in.EnvironmentID})
		return result{p: product(w, p), row: environment(w, in.ProductID, row)}, mapped(e, operations.NotFound)
	})
	return out.row, out.p, e
}
func (r *Postgres) ListEnvironments(ctx context.Context, w uuid.UUID, in uc.EnvironmentPageInput) ([]uc.Environment, uc.Product, error) {
	type result struct {
		p    uc.Product
		rows []uc.Environment
	}
	out, e := snapshot(ctx, r, func(ctx context.Context) (result, error) {
		after, e := uc.CursorAnchor(w, in, in.After)
		if e != nil {
			return result{}, e
		}
		p, e := r.queries(ctx).GetProduct(ctx, db.GetProductParams{Uuid: w, Uuid_2: in.ProductID})
		if e != nil {
			return result{}, mapped(e, operations.NotFound)
		}
		if after != uuid.Nil {
			if _, e := r.queries(ctx).GetProductEnvironment(ctx, db.GetProductEnvironmentParams{WorkspaceID: p.WorkspaceID, ProductID: p.ID, Uuid: after}); e != nil {
				return result{}, mapped(e, operations.Validation)
			}
		}
		rows, e := r.queries(ctx).ListProductEnvironments(ctx, db.ListProductEnvironmentsParams{WorkspaceID: p.WorkspaceID, ProductID: p.ID, Archived: in.Archived, AfterUuid: after, Search: in.Q, Kind: in.Kind, RowLimit: in.Limit + 1})
		list := make([]uc.Environment, 0, len(rows))
		for _, row := range rows {
			list = append(list, environment(w, in.ProductID, row))
		}
		return result{p: product(w, p), rows: list}, e
	})
	return out.rows, out.p, e
}
func (r *Postgres) CreateEnvironment(ctx context.Context, w uuid.UUID, in uc.CreateEnvironmentInput) (uc.Environment, int64, error) {
	p, e := r.locked(ctx, w, in.ProductID, true)
	if e != nil {
		return uc.Environment{}, 0, e
	}
	q := r.queries(ctx)
	v, e := q.BumpProduct(ctx, db.BumpProductParams{ID: p.ID, Version: in.ExpectedProductVersion})
	if e != nil {
		return uc.Environment{}, 0, mapped(e, operations.VersionConflict)
	}
	row, e := q.CreateProductEnvironment(ctx, db.CreateProductEnvironmentParams{WorkspaceID: p.WorkspaceID, ProductID: p.ID, Name: in.Name, Kind: in.Kind, Url: in.URL, DeployUrl: in.DeployURL, DeployProvider: in.DeployProvider, DeployReference: in.DeployReference, Summary: in.Summary})
	return environment(w, in.ProductID, row), v, mapped(e, operations.NotFound)
}
func (r *Postgres) UpdateEnvironment(ctx context.Context, w uuid.UUID, in uc.UpdateEnvironmentInput) (uc.Environment, int64, error) {
	p, e := r.locked(ctx, w, in.ProductID, true)
	if e != nil {
		return uc.Environment{}, 0, e
	}
	q := r.queries(ctx)
	v, e := q.BumpProduct(ctx, db.BumpProductParams{ID: p.ID, Version: in.ExpectedProductVersion})
	if e != nil {
		return uc.Environment{}, 0, mapped(e, operations.VersionConflict)
	}
	old, e := q.GetProductEnvironment(ctx, db.GetProductEnvironmentParams{WorkspaceID: p.WorkspaceID, ProductID: p.ID, Uuid: in.EnvironmentID})
	if e != nil {
		return uc.Environment{}, 0, mapped(e, operations.NotFound)
	}
	row, e := q.UpdateProductEnvironment(ctx, db.UpdateProductEnvironmentParams{ID: old.ID, Version: in.ExpectedVersion, Name: in.Name, Kind: in.Kind, Url: in.URL, DeployUrl: referenceValue(in.DeployURL), DeployProvider: referenceValue(in.DeployProvider), DeployReference: referenceValue(in.DeployReference), Summary: in.Summary})
	return environment(w, in.ProductID, row), v, mapped(e, operations.VersionConflict)
}
func (r *Postgres) ArchiveEnvironment(ctx context.Context, w uuid.UUID, in uc.ArchiveEnvironmentInput, archived bool) (uc.Environment, int64, error) {
	p, e := r.locked(ctx, w, in.ProductID, true)
	if e != nil {
		return uc.Environment{}, 0, e
	}
	q := r.queries(ctx)
	v, e := q.BumpProduct(ctx, db.BumpProductParams{ID: p.ID, Version: in.ExpectedProductVersion})
	if e != nil {
		return uc.Environment{}, 0, mapped(e, operations.VersionConflict)
	}
	old, e := q.GetProductEnvironment(ctx, db.GetProductEnvironmentParams{WorkspaceID: p.WorkspaceID, ProductID: p.ID, Uuid: in.EnvironmentID})
	if e != nil {
		return uc.Environment{}, 0, mapped(e, operations.NotFound)
	}
	row, e := q.SetProductEnvironmentArchived(ctx, db.SetProductEnvironmentArchivedParams{ID: old.ID, Archived: archived, Version: in.ExpectedVersion})
	return environment(w, in.ProductID, row), v, mapped(e, operations.VersionConflict)
}

// ValidateContext resolves server-selected catalog identities inside a future
// issue/widget transaction. Call before project→team validation to preserve lock
// order. The caller must obtain these IDs from the trusted installation context.
func (r *Postgres) ValidateContext(ctx context.Context, w, p, env uuid.UUID) error {
	if e := mutation(ctx); e != nil {
		return e
	}
	if p == uuid.Nil {
		if env != uuid.Nil {
			return &operations.Error{Code: operations.Validation}
		}
		return nil
	}
	pr, e := r.locked(ctx, w, p, true)
	if e != nil {
		return e
	}
	if env == uuid.Nil {
		return nil
	}
	row, e := r.queries(ctx).GetProductEnvironment(ctx, db.GetProductEnvironmentParams{WorkspaceID: pr.WorkspaceID, ProductID: pr.ID, Uuid: env})
	if e != nil {
		return mapped(e, operations.NotFound)
	}
	if row.Archived {
		return &operations.Error{Code: operations.Conflict}
	}
	return nil
}

// Explicit null clears optional public reference metadata. The operation schema
// still requires the field, so omitted values cannot silently discard data.
func referenceValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
