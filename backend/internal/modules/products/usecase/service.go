package usecase

import (
	"context"

	"github.com/furkanmeclis/traccia/backend/internal/platform/operations"
	"github.com/google/uuid"
)

type Service struct {
	repo   Repository
	policy Policy
}

func New(r Repository, p Policy) *Service { return &Service{r, p} }
func (s *Service) caps(ctx context.Context, w uuid.UUID, archived, parentArchived bool) (Capabilities, error) {
	b, e := s.policy.CanManage(ctx, w)
	return Capabilities{Manage: b && !archived && !parentArchived, Restore: b && archived && !parentArchived}, e
}
func (s *Service) productOutput(ctx context.Context, w uuid.UUID, p Product) (ProductOutput, error) {
	c, e := s.caps(ctx, w, p.Archived, false)
	return ProductOutput{p, c}, e
}
func (s *Service) event(ctx context.Context, sc operations.Scope, event string, id, p uuid.UUID, name string, v int64) error {
	return s.repo.RecordEvent(ctx, sc.WorkspaceID(), sc.ActorID(), event, id, p, name, v)
}
func (s *Service) createProduct(ctx context.Context, sc operations.Scope, in CreateProductInput) (ProductOutput, error) {
	p, e := s.repo.CreateProduct(ctx, sc.WorkspaceID(), in)
	if e != nil {
		return ProductOutput{}, e
	}
	if e = s.event(ctx, sc, "products.created", p.ID, p.ID, p.Name, p.Version); e != nil {
		return ProductOutput{}, e
	}
	return s.productOutput(ctx, sc.WorkspaceID(), p)
}
func (s *Service) getProduct(ctx context.Context, sc operations.Scope, in ProductInput) (ProductOutput, error) {
	p, e := s.repo.GetProduct(ctx, sc.WorkspaceID(), in.ProductID)
	if e != nil {
		return ProductOutput{}, e
	}
	return s.productOutput(ctx, sc.WorkspaceID(), p)
}
func (s *Service) updateProduct(ctx context.Context, sc operations.Scope, in UpdateProductInput) (ProductOutput, error) {
	p, e := s.repo.UpdateProduct(ctx, sc.WorkspaceID(), in)
	if e != nil {
		return ProductOutput{}, e
	}
	if e = s.event(ctx, sc, "products.updated", p.ID, p.ID, p.Name, p.Version); e != nil {
		return ProductOutput{}, e
	}
	return s.productOutput(ctx, sc.WorkspaceID(), p)
}
func (s *Service) archiveProduct(ctx context.Context, sc operations.Scope, in ArchiveProductInput) (ProductOutput, error) {
	return s.productArchive(ctx, sc, in, true)
}
func (s *Service) restoreProduct(ctx context.Context, sc operations.Scope, in ArchiveProductInput) (ProductOutput, error) {
	return s.productArchive(ctx, sc, in, false)
}
func (s *Service) productArchive(ctx context.Context, sc operations.Scope, in ArchiveProductInput, archived bool) (ProductOutput, error) {
	p, e := s.repo.ArchiveProduct(ctx, sc.WorkspaceID(), in, archived)
	if e != nil {
		return ProductOutput{}, e
	}
	act := "restored"
	if archived {
		act = "archived"
	}
	if e = s.event(ctx, sc, "products."+act, p.ID, p.ID, p.Name, p.Version); e != nil {
		return ProductOutput{}, e
	}
	return s.productOutput(ctx, sc.WorkspaceID(), p)
}
func (s *Service) listProducts(ctx context.Context, sc operations.Scope, in PageInput) (ProductList, error) {
	rows, e := s.repo.ListProducts(ctx, sc.WorkspaceID(), in)
	if e != nil {
		return ProductList{}, e
	}
	out := ProductList{Items: rows}
	if len(rows) > int(in.Limit) {
		out.Items = rows[:in.Limit]
		out.HasMore = true
		out.NextCursor = nextCursor(sc.WorkspaceID(), in, out.Items[len(out.Items)-1].ID)
	}
	out.Capabilities, e = s.caps(ctx, sc.WorkspaceID(), false, false)
	return out, e
}
func (s *Service) listProjects(ctx context.Context, sc operations.Scope, in ProductPageInput) (ProductProjects, error) {
	p, rows, e := s.repo.ListProjects(ctx, sc.WorkspaceID(), in)
	if e != nil {
		return ProductProjects{}, e
	}
	out := ProductProjects{Items: rows, ProductVersion: p.Version}
	if len(rows) > int(in.Limit) {
		out.Items = rows[:in.Limit]
		out.HasMore = true
		out.NextCursor = nextCursor(sc.WorkspaceID(), in, out.Items[len(out.Items)-1].ID)
	}
	out.Capabilities, e = s.caps(ctx, sc.WorkspaceID(), p.Archived, false)
	return out, e
}
func (s *Service) setProjects(ctx context.Context, sc operations.Scope, in SetProjectsInput) (ProductVersionOutput, error) {
	v, e := s.repo.SetProjects(ctx, sc.WorkspaceID(), in)
	if e == nil {
		e = s.event(ctx, sc, "product_projects.updated", in.ProductID, in.ProductID, "Projects", v)
	}
	return ProductVersionOutput{v}, e
}
func (s *Service) environmentOutput(ctx context.Context, w uuid.UUID, row Environment, p Product) (EnvironmentOutput, error) {
	c, e := s.caps(ctx, w, row.Archived, p.Archived)
	return EnvironmentOutput{row, p.Version, c}, e
}
func (s *Service) getEnvironment(ctx context.Context, sc operations.Scope, in EnvironmentInput) (EnvironmentOutput, error) {
	row, p, e := s.repo.GetEnvironment(ctx, sc.WorkspaceID(), in)
	if e != nil {
		return EnvironmentOutput{}, e
	}
	return s.environmentOutput(ctx, sc.WorkspaceID(), row, p)
}
func (s *Service) listEnvironments(ctx context.Context, sc operations.Scope, in EnvironmentPageInput) (EnvironmentList, error) {
	rows, p, e := s.repo.ListEnvironments(ctx, sc.WorkspaceID(), in)
	if e != nil {
		return EnvironmentList{}, e
	}
	out := EnvironmentList{Items: rows, ProductVersion: p.Version}
	if len(rows) > int(in.Limit) {
		out.Items = rows[:in.Limit]
		out.HasMore = true
		out.NextCursor = nextCursor(sc.WorkspaceID(), in, out.Items[len(out.Items)-1].ID)
	}
	out.Capabilities, e = s.caps(ctx, sc.WorkspaceID(), in.Archived, p.Archived)
	return out, e
}
func (s *Service) createEnvironment(ctx context.Context, sc operations.Scope, in CreateEnvironmentInput) (EnvironmentOutput, error) {
	row, v, e := s.repo.CreateEnvironment(ctx, sc.WorkspaceID(), in)
	if e != nil {
		return EnvironmentOutput{}, e
	}
	if e = s.event(ctx, sc, "product_environments.created", row.ID, row.ProductID, row.Name, row.Version); e != nil {
		return EnvironmentOutput{}, e
	}
	return s.environmentOutput(ctx, sc.WorkspaceID(), row, Product{Version: v})
}
func (s *Service) updateEnvironment(ctx context.Context, sc operations.Scope, in UpdateEnvironmentInput) (EnvironmentOutput, error) {
	row, v, e := s.repo.UpdateEnvironment(ctx, sc.WorkspaceID(), in)
	if e != nil {
		return EnvironmentOutput{}, e
	}
	if e = s.event(ctx, sc, "product_environments.updated", row.ID, row.ProductID, row.Name, row.Version); e != nil {
		return EnvironmentOutput{}, e
	}
	return s.environmentOutput(ctx, sc.WorkspaceID(), row, Product{Version: v})
}
func (s *Service) archiveEnvironment(ctx context.Context, sc operations.Scope, in ArchiveEnvironmentInput) (EnvironmentOutput, error) {
	return s.environmentArchive(ctx, sc, in, true)
}
func (s *Service) restoreEnvironment(ctx context.Context, sc operations.Scope, in ArchiveEnvironmentInput) (EnvironmentOutput, error) {
	return s.environmentArchive(ctx, sc, in, false)
}
func (s *Service) environmentArchive(ctx context.Context, sc operations.Scope, in ArchiveEnvironmentInput, archived bool) (EnvironmentOutput, error) {
	row, v, e := s.repo.ArchiveEnvironment(ctx, sc.WorkspaceID(), in, archived)
	if e != nil {
		return EnvironmentOutput{}, e
	}
	act := "restored"
	if archived {
		act = "archived"
	}
	if e = s.event(ctx, sc, "product_environments."+act, row.ID, row.ProductID, row.Name, row.Version); e != nil {
		return EnvironmentOutput{}, e
	}
	return s.environmentOutput(ctx, sc.WorkspaceID(), row, Product{Version: v})
}
