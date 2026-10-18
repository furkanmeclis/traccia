package usecase

import (
	"context"
	"time"

	teams "github.com/furkanmeclis/traccia/backend/internal/modules/teams/usecase"
	"github.com/google/uuid"
)

type Capabilities struct {
	Manage  bool `json:"manage"`
	Restore bool `json:"restore"`
}
type Project struct {
	ID          uuid.UUID `json:"id"`
	WorkspaceID uuid.UUID `json:"workspace_id"`
	Name        string    `json:"name"`
	Summary     string    `json:"summary"`
	LeadUserID  *string   `json:"lead_user_id"`
	StartDate   *string   `json:"start_date"`
	TargetDate  *string   `json:"target_date"`
	Version     int64     `json:"version"`
	Archived    bool      `json:"archived"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}
type ProjectInput struct {
	ProjectID uuid.UUID `json:"project_id"`
}
type PageInput struct {
	Limit    int32  `json:"limit"`
	After    string `json:"after"`
	Archived bool   `json:"archived"`
}
type ProjectPageInput struct {
	ProjectID uuid.UUID `json:"project_id"`
	Limit     int32     `json:"limit"`
	After     string    `json:"after"`
	Archived  bool      `json:"archived"`
}
type ProjectOutput struct {
	Project      Project      `json:"project"`
	Capabilities Capabilities `json:"capabilities"`
}
type ProjectList struct {
	Items        []Project    `json:"items"`
	HasMore      bool         `json:"has_more"`
	NextCursor   string       `json:"next_cursor"`
	Capabilities Capabilities `json:"capabilities"`
}
type ProjectTeams struct {
	Items          []teams.Team `json:"items"`
	HasMore        bool         `json:"has_more"`
	NextCursor     string       `json:"next_cursor"`
	ProjectVersion int64        `json:"project_version"`
	Capabilities   Capabilities `json:"capabilities"`
}
type SetTeamsInput struct {
	ProjectID              uuid.UUID `json:"project_id"`
	TeamIDs                []string  `json:"team_ids"`
	ExpectedProjectVersion int64     `json:"expected_project_version"`
}
type ProjectVersionOutput struct {
	ProjectVersion int64 `json:"project_version"`
}
type ArchiveProjectInput struct {
	ProjectID       uuid.UUID `json:"project_id"`
	ExpectedVersion int64     `json:"expected_version"`
}
type Member struct {
	ID         uuid.UUID `json:"id"`
	Name       string    `json:"name"`
	Surname    string    `json:"surname"`
	Role       string    `json:"role"`
	Active     bool      `json:"active"`
	Assignable bool      `json:"assignable"`
}
type MemberPageInput struct {
	Limit int32  `json:"limit"`
	After string `json:"after"`
}
type MemberList struct {
	Items      []Member `json:"items"`
	HasMore    bool     `json:"has_more"`
	NextCursor string   `json:"next_cursor"`
}
type CreateProjectInput struct {
	Name       string   `json:"name"`
	Summary    string   `json:"summary"`
	LeadUserID *string  `json:"lead_user_id"`
	StartDate  *string  `json:"start_date"`
	TargetDate *string  `json:"target_date"`
	TeamIDs    []string `json:"team_ids"`
}
type UpdateProjectInput struct {
	ProjectID       uuid.UUID `json:"project_id"`
	Name            string    `json:"name"`
	Summary         string    `json:"summary"`
	LeadUserID      *string   `json:"lead_user_id"`
	StartDate       *string   `json:"start_date"`
	TargetDate      *string   `json:"target_date"`
	ExpectedVersion int64     `json:"expected_version"`
}
type Milestone struct {
	ID          uuid.UUID `json:"id"`
	WorkspaceID uuid.UUID `json:"workspace_id"`
	ProjectID   uuid.UUID `json:"project_id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	TargetDate  *string   `json:"target_date"`
	Version     int64     `json:"version"`
	Archived    bool      `json:"archived"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}
type MilestoneInput struct {
	ProjectID   uuid.UUID `json:"project_id"`
	MilestoneID uuid.UUID `json:"milestone_id"`
}
type CreateMilestoneInput struct {
	ProjectID              uuid.UUID `json:"project_id"`
	Name                   string    `json:"name"`
	Description            string    `json:"description"`
	TargetDate             *string   `json:"target_date"`
	ExpectedProjectVersion int64     `json:"expected_project_version"`
}
type UpdateMilestoneInput struct {
	ProjectID              uuid.UUID `json:"project_id"`
	MilestoneID            uuid.UUID `json:"milestone_id"`
	Name                   string    `json:"name"`
	Description            string    `json:"description"`
	TargetDate             *string   `json:"target_date"`
	ExpectedVersion        int64     `json:"expected_version"`
	ExpectedProjectVersion int64     `json:"expected_project_version"`
}
type ArchiveMilestoneInput struct {
	ProjectID              uuid.UUID `json:"project_id"`
	MilestoneID            uuid.UUID `json:"milestone_id"`
	ExpectedVersion        int64     `json:"expected_version"`
	ExpectedProjectVersion int64     `json:"expected_project_version"`
}
type MilestoneOutput struct {
	Milestone      Milestone    `json:"milestone"`
	ProjectVersion int64        `json:"project_version"`
	Capabilities   Capabilities `json:"capabilities"`
}
type MilestoneList struct {
	Items          []Milestone  `json:"items"`
	ProjectVersion int64        `json:"project_version"`
	HasMore        bool         `json:"has_more"`
	NextCursor     string       `json:"next_cursor"`
	Capabilities   Capabilities `json:"capabilities"`
}
type Resource struct {
	ID          uuid.UUID `json:"id"`
	WorkspaceID uuid.UUID `json:"workspace_id"`
	ProjectID   uuid.UUID `json:"project_id"`
	Kind        string    `json:"kind"`
	Title       string    `json:"title"`
	URL         string    `json:"url"`
	Summary     string    `json:"summary"`
	Provider    string    `json:"provider"`
	Reference   string    `json:"reference"`
	Version     int64     `json:"version"`
	Archived    bool      `json:"archived"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}
type ResourceInput struct {
	ProjectID  uuid.UUID `json:"project_id"`
	ResourceID uuid.UUID `json:"resource_id"`
}
type CreateResourceInput struct {
	ProjectID              uuid.UUID `json:"project_id"`
	Kind                   string    `json:"kind"`
	Title                  string    `json:"title"`
	URL                    string    `json:"url"`
	Summary                string    `json:"summary"`
	Provider               string    `json:"provider"`
	Reference              string    `json:"reference"`
	ExpectedProjectVersion int64     `json:"expected_project_version"`
}
type UpdateResourceInput struct {
	ProjectID              uuid.UUID `json:"project_id"`
	ResourceID             uuid.UUID `json:"resource_id"`
	Kind                   string    `json:"kind"`
	Title                  string    `json:"title"`
	URL                    string    `json:"url"`
	Summary                string    `json:"summary"`
	Provider               string    `json:"provider"`
	Reference              string    `json:"reference"`
	ExpectedVersion        int64     `json:"expected_version"`
	ExpectedProjectVersion int64     `json:"expected_project_version"`
}
type ArchiveResourceInput struct {
	ProjectID              uuid.UUID `json:"project_id"`
	ResourceID             uuid.UUID `json:"resource_id"`
	ExpectedVersion        int64     `json:"expected_version"`
	ExpectedProjectVersion int64     `json:"expected_project_version"`
}
type ResourceOutput struct {
	Resource       Resource     `json:"resource"`
	ProjectVersion int64        `json:"project_version"`
	Capabilities   Capabilities `json:"capabilities"`
}
type ResourceList struct {
	Items          []Resource   `json:"items"`
	ProjectVersion int64        `json:"project_version"`
	HasMore        bool         `json:"has_more"`
	NextCursor     string       `json:"next_cursor"`
	Capabilities   Capabilities `json:"capabilities"`
}
type Policy interface {
	CanManage(context.Context, uuid.UUID) (bool, error)
}
type Repository interface {
	CreateProject(context.Context, uuid.UUID, CreateProjectInput) (Project, error)
	GetProject(context.Context, uuid.UUID, uuid.UUID) (Project, error)
	ListProjects(context.Context, uuid.UUID, PageInput) ([]Project, error)
	UpdateProject(context.Context, uuid.UUID, UpdateProjectInput) (Project, error)
	ArchiveProject(context.Context, uuid.UUID, ArchiveProjectInput, bool) (Project, error)
	ListTeams(context.Context, uuid.UUID, ProjectPageInput) (Project, []teams.Team, error)
	SetTeams(context.Context, uuid.UUID, SetTeamsInput) (int64, error)
	ListMembers(context.Context, uuid.UUID, MemberPageInput) ([]Member, error)
	RecordEvent(context.Context, uuid.UUID, uuid.UUID, string, uuid.UUID, uuid.UUID, string, int64) error
	CreateMilestone(context.Context, uuid.UUID, CreateMilestoneInput) (Milestone, int64, error)
	GetMilestone(context.Context, uuid.UUID, MilestoneInput) (Milestone, Project, error)
	ListMilestones(context.Context, uuid.UUID, ProjectPageInput) ([]Milestone, Project, error)
	UpdateMilestone(context.Context, uuid.UUID, UpdateMilestoneInput) (Milestone, int64, error)
	ArchiveMilestone(context.Context, uuid.UUID, ArchiveMilestoneInput, bool) (Milestone, int64, error)
	CreateResource(context.Context, uuid.UUID, CreateResourceInput) (Resource, int64, error)
	GetResource(context.Context, uuid.UUID, ResourceInput) (Resource, Project, error)
	ListResources(context.Context, uuid.UUID, ProjectPageInput) ([]Resource, Project, error)
	UpdateResource(context.Context, uuid.UUID, UpdateResourceInput) (Resource, int64, error)
	ArchiveResource(context.Context, uuid.UUID, ArchiveResourceInput, bool) (Resource, int64, error)
}
