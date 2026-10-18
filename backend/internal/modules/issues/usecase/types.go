package usecase

import (
	"context"
	"github.com/google/uuid"
	"time"
)

type Capabilities struct {
	Move    bool `json:"move"`
	Manage  bool `json:"manage"`
	Restore bool `json:"restore"`
}
type Issue struct {
	ID          uuid.UUID `json:"id"`
	WorkspaceID uuid.UUID `json:"workspace_id"`
	TeamID      uuid.UUID `json:"team_id"`
	Identifier  string    `json:"identifier"`
	Number      int64     `json:"number"`
	Title       string    `json:"title"`
	Priority    string    `json:"priority"`
	StatusID    uuid.UUID `json:"status_id"`
	AssigneeID  *string   `json:"assignee_id"`
	ProjectID   *string   `json:"project_id"`
	MilestoneID *string   `json:"milestone_id"`
	StartDate   *string   `json:"start_date"`
	TargetDate  *string   `json:"target_date"`
	Effort      *int32    `json:"effort"`
	LabelIDs    []string  `json:"label_ids"`
	Version     int64     `json:"version"`
	Archived    bool      `json:"archived"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}
type CreateInput struct {
	TeamID      uuid.UUID `json:"team_id"`
	Title       string    `json:"title"`
	Priority    string    `json:"priority"`
	StatusID    uuid.UUID `json:"status_id"`
	AssigneeID  *string   `json:"assignee_id"`
	ProjectID   *string   `json:"project_id"`
	MilestoneID *string   `json:"milestone_id"`
	StartDate   *string   `json:"start_date"`
	TargetDate  *string   `json:"target_date"`
	Effort      *int32    `json:"effort"`
	LabelIDs    []string  `json:"label_ids"`
}
type UpdateInput struct {
	IssueID         uuid.UUID `json:"issue_id"`
	ExpectedVersion int64     `json:"expected_version"`
	Title           string    `json:"title"`
	Priority        string    `json:"priority"`
	StatusID        uuid.UUID `json:"status_id"`
	AssigneeID      *string   `json:"assignee_id"`
	ProjectID       *string   `json:"project_id"`
	MilestoneID     *string   `json:"milestone_id"`
	StartDate       *string   `json:"start_date"`
	TargetDate      *string   `json:"target_date"`
	Effort          *int32    `json:"effort"`
	LabelIDs        []string  `json:"label_ids"`
}
type MoveInput struct {
	IssueID         uuid.UUID `json:"issue_id"`
	TeamID          uuid.UUID `json:"team_id"`
	ExpectedVersion int64     `json:"expected_version"`
	StatusID        uuid.UUID `json:"status_id"`
	ProjectID       *string   `json:"project_id"`
	MilestoneID     *string   `json:"milestone_id"`
	LabelIDs        []string  `json:"label_ids"`
}
type IssueInput struct {
	IssueID uuid.UUID `json:"issue_id"`
}
type VersionInput struct {
	IssueID         uuid.UUID `json:"issue_id"`
	ExpectedVersion int64     `json:"expected_version"`
}
type ResolveInput struct {
	Identifier string `json:"identifier"`
}
type PageInput struct {
	Limit       int32  `json:"limit"`
	After       string `json:"after"`
	Archived    bool   `json:"archived"`
	Q           string `json:"q"`
	TeamID      string `json:"team_id"`
	StatusID    string `json:"status_id"`
	Priority    string `json:"priority"`
	AssigneeID  string `json:"assignee_id"`
	ProjectID   string `json:"project_id"`
	MilestoneID string `json:"milestone_id"`
	LabelID     string `json:"label_id"`
}
type HistoryInput struct {
	IssueID uuid.UUID `json:"issue_id"`
	Limit   int32     `json:"limit"`
	After   string    `json:"after"`
}
type Output struct {
	Issue        Issue        `json:"issue"`
	Capabilities Capabilities `json:"capabilities"`
}
type List struct {
	Items        []Issue      `json:"items"`
	HasMore      bool         `json:"has_more"`
	NextCursor   string       `json:"next_cursor"`
	Capabilities Capabilities `json:"capabilities"`
}
type History struct {
	ID          uuid.UUID `json:"id"`
	EventName   string    `json:"event_name"`
	ActorID     uuid.UUID `json:"actor_id"`
	WorkspaceID uuid.UUID `json:"workspace_id"`
	IssueID     uuid.UUID `json:"issue_id"`
	Version     int64     `json:"version"`
	OccurredAt  time.Time `json:"occurred_at"`
	Issue       Issue     `json:"issue"`
}
type HistoryList struct {
	Items      []History `json:"items"`
	HasMore    bool      `json:"has_more"`
	NextCursor string    `json:"next_cursor"`
}
type Policy interface {
	CanManage(context.Context, uuid.UUID) (bool, error)
}
type Repository interface {
	ActiveTeam(context.Context, uuid.UUID, uuid.UUID) (bool, error)
	Create(context.Context, uuid.UUID, CreateInput) (Issue, error)
	Get(context.Context, uuid.UUID, uuid.UUID) (Issue, error)
	Resolve(context.Context, uuid.UUID, string) (Issue, error)
	List(context.Context, uuid.UUID, PageInput) ([]Issue, error)
	Update(context.Context, uuid.UUID, UpdateInput) (Issue, error)
	Move(context.Context, uuid.UUID, MoveInput) (Issue, error)
	Archive(context.Context, uuid.UUID, VersionInput, bool) (Issue, error)
	History(context.Context, uuid.UUID, HistoryInput) ([]History, error)
	RecordEvent(context.Context, uuid.UUID, uuid.UUID, string, Issue) error
}
