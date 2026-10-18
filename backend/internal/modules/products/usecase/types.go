package usecase

import (
	"context"
	projects "github.com/furkanmeclis/traccia/backend/internal/modules/projects/usecase"
	"github.com/google/uuid"
	"time"
)

type Capabilities struct {
	Manage  bool `json:"manage"`
	Restore bool `json:"restore"`
}
type Product struct {
	ID                  uuid.UUID `json:"id"`
	WorkspaceID         uuid.UUID `json:"workspace_id"`
	Name                string    `json:"name"`
	Summary             string    `json:"summary"`
	RepositoryURL       string    `json:"repository_url"`
	RepositoryProvider  string    `json:"repository_provider"`
	RepositoryReference string    `json:"repository_reference"`
	Version             int64     `json:"version"`
	Archived            bool      `json:"archived"`
	CreatedAt           time.Time `json:"created_at"`
	UpdatedAt           time.Time `json:"updated_at"`
}
type Environment struct {
	ID              uuid.UUID `json:"id"`
	WorkspaceID     uuid.UUID `json:"workspace_id"`
	ProductID       uuid.UUID `json:"product_id"`
	Name            string    `json:"name"`
	Kind            string    `json:"kind"`
	URL             string    `json:"url"`
	DeployURL       string    `json:"deploy_url"`
	DeployProvider  string    `json:"deploy_provider"`
	DeployReference string    `json:"deploy_reference"`
	Summary         string    `json:"summary"`
	Version         int64     `json:"version"`
	Archived        bool      `json:"archived"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}
type ProductInput struct {
	ProductID uuid.UUID `json:"product_id"`
}
type EnvironmentInput struct {
	ProductID     uuid.UUID `json:"product_id"`
	EnvironmentID uuid.UUID `json:"environment_id"`
}
type PageInput struct {
	Limit     int32  `json:"limit"`
	After     string `json:"after"`
	Archived  bool   `json:"archived"`
	Q         string `json:"q"`
	ProjectID string `json:"project_id"`
}
type ProductPageInput struct {
	ProductID uuid.UUID `json:"product_id"`
	Limit     int32     `json:"limit"`
	After     string    `json:"after"`
	Archived  bool      `json:"archived"`
}
type EnvironmentPageInput struct {
	ProductID uuid.UUID `json:"product_id"`
	Limit     int32     `json:"limit"`
	After     string    `json:"after"`
	Archived  bool      `json:"archived"`
	Q         string    `json:"q"`
	Kind      string    `json:"kind"`
}
type ProductOutput struct {
	Product      Product      `json:"product"`
	Capabilities Capabilities `json:"capabilities"`
}
type ProductList struct {
	Items        []Product    `json:"items"`
	HasMore      bool         `json:"has_more"`
	NextCursor   string       `json:"next_cursor"`
	Capabilities Capabilities `json:"capabilities"`
}
type EnvironmentOutput struct {
	Environment    Environment  `json:"environment"`
	ProductVersion int64        `json:"product_version"`
	Capabilities   Capabilities `json:"capabilities"`
}
type EnvironmentList struct {
	Items          []Environment `json:"items"`
	HasMore        bool          `json:"has_more"`
	NextCursor     string        `json:"next_cursor"`
	ProductVersion int64         `json:"product_version"`
	Capabilities   Capabilities  `json:"capabilities"`
}
type ProductProjects struct {
	Items          []projects.Project `json:"items"`
	HasMore        bool               `json:"has_more"`
	NextCursor     string             `json:"next_cursor"`
	ProductVersion int64              `json:"product_version"`
	Capabilities   Capabilities       `json:"capabilities"`
}
type SetProjectsInput struct {
	ProductID              uuid.UUID `json:"product_id"`
	ProjectIDs             []string  `json:"project_ids"`
	ExpectedProductVersion int64     `json:"expected_product_version"`
}
type ProductVersionOutput struct {
	ProductVersion int64 `json:"product_version"`
}
type CreateProductInput struct {
	Name                string `json:"name"`
	Summary             string `json:"summary"`
	RepositoryURL       string `json:"repository_url"`
	RepositoryProvider  string `json:"repository_provider"`
	RepositoryReference string `json:"repository_reference"`
}
type UpdateProductInput struct {
	ProductID           uuid.UUID `json:"product_id"`
	Name                string    `json:"name"`
	Summary             string    `json:"summary"`
	RepositoryURL       *string   `json:"repository_url"`
	RepositoryProvider  *string   `json:"repository_provider"`
	RepositoryReference *string   `json:"repository_reference"`
	ExpectedVersion     int64     `json:"expected_version"`
}
type ArchiveProductInput struct {
	ProductID       uuid.UUID `json:"product_id"`
	ExpectedVersion int64     `json:"expected_version"`
}
type CreateEnvironmentInput struct {
	ProductID              uuid.UUID `json:"product_id"`
	Name                   string    `json:"name"`
	Kind                   string    `json:"kind"`
	URL                    string    `json:"url"`
	DeployURL              string    `json:"deploy_url"`
	DeployProvider         string    `json:"deploy_provider"`
	DeployReference        string    `json:"deploy_reference"`
	Summary                string    `json:"summary"`
	ExpectedProductVersion int64     `json:"expected_product_version"`
}
type UpdateEnvironmentInput struct {
	ProductID              uuid.UUID `json:"product_id"`
	EnvironmentID          uuid.UUID `json:"environment_id"`
	Name                   string    `json:"name"`
	Kind                   string    `json:"kind"`
	URL                    string    `json:"url"`
	DeployURL              *string   `json:"deploy_url"`
	DeployProvider         *string   `json:"deploy_provider"`
	DeployReference        *string   `json:"deploy_reference"`
	Summary                string    `json:"summary"`
	ExpectedVersion        int64     `json:"expected_version"`
	ExpectedProductVersion int64     `json:"expected_product_version"`
}
type ArchiveEnvironmentInput struct {
	ProductID              uuid.UUID `json:"product_id"`
	EnvironmentID          uuid.UUID `json:"environment_id"`
	ExpectedVersion        int64     `json:"expected_version"`
	ExpectedProductVersion int64     `json:"expected_product_version"`
}
type Policy interface {
	CanManage(context.Context, uuid.UUID) (bool, error)
}
type Repository interface {
	CreateProduct(context.Context, uuid.UUID, CreateProductInput) (Product, error)
	GetProduct(context.Context, uuid.UUID, uuid.UUID) (Product, error)
	ListProducts(context.Context, uuid.UUID, PageInput) ([]Product, error)
	UpdateProduct(context.Context, uuid.UUID, UpdateProductInput) (Product, error)
	ArchiveProduct(context.Context, uuid.UUID, ArchiveProductInput, bool) (Product, error)
	ListProjects(context.Context, uuid.UUID, ProductPageInput) (Product, []projects.Project, error)
	SetProjects(context.Context, uuid.UUID, SetProjectsInput) (int64, error)
	CreateEnvironment(context.Context, uuid.UUID, CreateEnvironmentInput) (Environment, int64, error)
	GetEnvironment(context.Context, uuid.UUID, EnvironmentInput) (Environment, Product, error)
	ListEnvironments(context.Context, uuid.UUID, EnvironmentPageInput) ([]Environment, Product, error)
	UpdateEnvironment(context.Context, uuid.UUID, UpdateEnvironmentInput) (Environment, int64, error)
	ArchiveEnvironment(context.Context, uuid.UUID, ArchiveEnvironmentInput, bool) (Environment, int64, error)
	RecordEvent(context.Context, uuid.UUID, uuid.UUID, string, uuid.UUID, uuid.UUID, string, int64) error
}
