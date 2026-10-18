package usecase

import (
	"context"
	"time"

	search "github.com/furkanmeclis/traccia/backend/internal/modules/searchviews/usecase"
	"github.com/furkanmeclis/traccia/backend/internal/platform/operations"
	"github.com/google/uuid"
)

const MaxWorkspaces = 32

type ConfigInput struct{}
type Capabilities struct {
	Manage bool `json:"manage"`
}
type Limits struct {
	MaxWorkspaces     int32 `json:"max_workspaces"`
	MaxPage           int32 `json:"max_page"`
	DefaultTTLSeconds int32 `json:"default_ttl_seconds"`
	MinTTLSeconds     int32 `json:"min_ttl_seconds"`
	MaxTTLSeconds     int32 `json:"max_ttl_seconds"`
	MaxRawInputBytes  int32 `json:"max_raw_input_bytes"`
	MaxInputBytes     int32 `json:"max_input_bytes"`
}
type ConfigOutput struct {
	Limits          Limits       `json:"limits"`
	Capabilities    Capabilities `json:"capabilities"`
	InputBytePolicy string       `json:"input_byte_policy"`
	TextMatching    string       `json:"text_matching"`
	TextOrdering    string       `json:"text_ordering"`
}
type Target struct {
	Kind string    `json:"kind"`
	ID   uuid.UUID `json:"id"`
}
type TargetMetadata struct {
	Label      string  `json:"label"`
	Identifier *string `json:"identifier"`
	Archived   bool    `json:"archived"`
}
type Favorite struct {
	ID          uuid.UUID      `json:"id"`
	WorkspaceID uuid.UUID      `json:"workspace_id"`
	ActorID     uuid.UUID      `json:"actor_id"`
	Target      Target         `json:"target"`
	Metadata    TargetMetadata `json:"metadata"`
	Revision    uuid.UUID      `json:"revision"`
	Active      bool           `json:"active"`
	CreatedAt   time.Time      `json:"created_at"`
	UpdatedAt   time.Time      `json:"updated_at"`
}
type FavoriteListInput struct {
	IncludeInactive bool   `json:"include_inactive"`
	Limit           int32  `json:"limit"`
	After           string `json:"after"`
}
type FavoriteListOutput struct {
	Items        []Favorite   `json:"items"`
	HasMore      bool         `json:"has_more"`
	NextCursor   string       `json:"next_cursor"`
	Capabilities Capabilities `json:"capabilities"`
}
type FavoriteSetInput struct {
	Target           Target     `json:"target"`
	Active           bool       `json:"active"`
	ExpectedRevision *uuid.UUID `json:"expected_revision"`
}
type FavoriteSetOutput struct {
	Favorite     Favorite     `json:"favorite"`
	Capabilities Capabilities `json:"capabilities"`
}

// Portfolio definitions reuse the closed query language without extending any
// released search-session resource enum or changing its wire contracts.
type PortfolioCreateInput struct {
	RequestID    uuid.UUID        `json:"request_id"`
	WorkspaceIDs []uuid.UUID      `json:"workspace_ids"`
	Definition   search.ViewQuery `json:"definition"`
	AssignedToMe bool             `json:"assigned_to_me"`
	TTLSeconds   *int32           `json:"ttl_seconds"`
	Limit        int32            `json:"limit"`
}
type PortfolioGetInput struct {
	SessionID uuid.UUID `json:"session_id"`
	Limit     int32     `json:"limit"`
	After     string    `json:"after"`
}
type WorkspaceSnapshot struct {
	ID         uuid.UUID `json:"id"`
	Name       string    `json:"name"`
	TotalCount string    `json:"total_count"`
}
type PortfolioSession struct {
	ID                uuid.UUID           `json:"id"`
	RequestID         uuid.UUID           `json:"request_id"`
	AnchorWorkspaceID uuid.UUID           `json:"anchor_workspace_id"`
	Resource          string              `json:"resource"`
	Workspaces        []WorkspaceSnapshot `json:"workspaces"`
	AsOf              time.Time           `json:"as_of"`
	ExpiresAt         time.Time           `json:"expires_at"`
	TotalCount        string              `json:"total_count"`
}
type PortfolioRow struct {
	WorkspaceID uuid.UUID          `json:"workspace_id"`
	Resource    string             `json:"resource"`
	Issue       *search.IssueRow   `json:"issue"`
	Project     *search.ProjectRow `json:"project"`
	Team        *search.TeamRow    `json:"team"`
}
type PortfolioOutput struct {
	Session      PortfolioSession `json:"session"`
	Definition   search.ViewQuery `json:"definition"`
	AssignedToMe bool             `json:"assigned_to_me"`
	Items        []PortfolioRow   `json:"items"`
	HasMore      bool             `json:"has_more"`
	NextCursor   string           `json:"next_cursor"`
}
type Policy interface {
	Authorize(context.Context, operations.AuthorizationRequest) (operations.Identity, error)
	CanManage(context.Context, uuid.UUID) (bool, error)
}

type Capture struct {
	Input        PortfolioCreateInput
	EffectiveTTL int32
}
type StoredPortfolio struct {
	Output       PortfolioOutput
	WorkspaceIDs []uuid.UUID
}
type Repository interface {
	ListFavorites(context.Context, uuid.UUID, uuid.UUID, FavoriteListInput) ([]Favorite, error)
	SetFavorite(context.Context, uuid.UUID, uuid.UUID, FavoriteSetInput) (Favorite, error)
	Capture(context.Context, uuid.UUID, uuid.UUID, Capture, func(context.Context, []uuid.UUID, bool) error) (PortfolioOutput, error)
	Page(context.Context, uuid.UUID, uuid.UUID, PortfolioGetInput, func(context.Context, []uuid.UUID, bool) error) (PortfolioOutput, error)
}
