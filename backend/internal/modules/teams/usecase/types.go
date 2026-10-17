package usecase

import (
	"context"
	"time"

	"github.com/google/uuid"
)

type Capabilities struct {
	Manage bool `json:"manage"`
}
type Team struct {
	ID              uuid.UUID `json:"id"`
	WorkspaceID     uuid.UUID `json:"workspace_id"`
	Name            string    `json:"name"`
	Code            string    `json:"code"`
	Version         int64     `json:"version"`
	WorkflowVersion int64     `json:"workflow_version"`
	Archived        bool      `json:"archived"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}
type Status struct {
	ID          uuid.UUID `json:"id"`
	WorkspaceID uuid.UUID `json:"workspace_id"`
	TeamID      uuid.UUID `json:"team_id"`
	Name        string    `json:"name"`
	Category    string    `json:"category"`
	Color       string    `json:"color"`
	Position    int64     `json:"position"`
	Version     int64     `json:"version"`
	Archived    bool      `json:"archived"`
}
type Label struct {
	ID          uuid.UUID `json:"id"`
	WorkspaceID uuid.UUID `json:"workspace_id"`
	TeamID      string    `json:"team_id"`
	Name        string    `json:"name"`
	Color       string    `json:"color"`
	Version     int64     `json:"version"`
	Archived    bool      `json:"archived"`
}
type PageInput struct {
	Limit    int32  `json:"limit"`
	After    string `json:"after"`
	Archived bool   `json:"archived"`
}
type TeamInput struct {
	TeamID uuid.UUID `json:"team_id"`
}
type TeamListInput = PageInput
type CreateTeamInput struct {
	Name string `json:"name"`
	Code string `json:"code"`
}
type UpdateTeamInput struct {
	TeamID          uuid.UUID `json:"team_id"`
	Name            string    `json:"name"`
	ExpectedVersion int64     `json:"expected_version"`
}
type ArchiveTeamInput struct {
	TeamID          uuid.UUID `json:"team_id"`
	ExpectedVersion int64     `json:"expected_version"`
}
type TeamOutput struct {
	Team         Team         `json:"team"`
	Capabilities Capabilities `json:"capabilities"`
}
type TeamList struct {
	Items        []Team       `json:"items"`
	HasMore      bool         `json:"has_more"`
	NextCursor   string       `json:"next_cursor"`
	Capabilities Capabilities `json:"capabilities"`
}
type StatusListInput struct {
	TeamID   uuid.UUID `json:"team_id"`
	Limit    int32     `json:"limit"`
	After    string    `json:"after"`
	Archived bool      `json:"archived"`
}
type CreateStatusInput struct {
	TeamID                  uuid.UUID `json:"team_id"`
	Name                    string    `json:"name"`
	Category                string    `json:"category"`
	Color                   string    `json:"color"`
	ExpectedWorkflowVersion int64     `json:"expected_workflow_version"`
}
type UpdateStatusInput struct {
	TeamID                  uuid.UUID `json:"team_id"`
	StatusID                uuid.UUID `json:"status_id"`
	Name                    string    `json:"name"`
	Category                string    `json:"category"`
	Color                   string    `json:"color"`
	ExpectedVersion         int64     `json:"expected_version"`
	ExpectedWorkflowVersion int64     `json:"expected_workflow_version"`
}
type ArchiveStatusInput struct {
	TeamID                  uuid.UUID `json:"team_id"`
	StatusID                uuid.UUID `json:"status_id"`
	ExpectedVersion         int64     `json:"expected_version"`
	ExpectedWorkflowVersion int64     `json:"expected_workflow_version"`
}
type ReorderInput struct {
	TeamID                  uuid.UUID `json:"team_id"`
	StatusIDs               []string  `json:"status_ids"`
	ExpectedWorkflowVersion int64     `json:"expected_workflow_version"`
}
type StatusOutput struct {
	Status          Status `json:"status"`
	WorkflowVersion int64  `json:"workflow_version"`
}
type ReorderOutput struct {
	WorkflowVersion int64 `json:"workflow_version"`
}
type StatusList struct {
	Items           []Status     `json:"items"`
	HasMore         bool         `json:"has_more"`
	NextCursor      string       `json:"next_cursor"`
	WorkflowVersion int64        `json:"workflow_version"`
	Capabilities    Capabilities `json:"capabilities"`
}
type LabelListInput struct {
	TeamID   string `json:"team_id"`
	Limit    int32  `json:"limit"`
	After    string `json:"after"`
	Archived bool   `json:"archived"`
}
type CreateLabelInput struct {
	TeamID string `json:"team_id"`
	Name   string `json:"name"`
	Color  string `json:"color"`
}
type LabelInput struct {
	LabelID uuid.UUID `json:"label_id"`
}
type UpdateLabelInput struct {
	LabelID         uuid.UUID `json:"label_id"`
	Name            string    `json:"name"`
	Color           string    `json:"color"`
	ExpectedVersion int64     `json:"expected_version"`
}
type ArchiveLabelInput struct {
	LabelID         uuid.UUID `json:"label_id"`
	ExpectedVersion int64     `json:"expected_version"`
}
type LabelOutput struct {
	Label Label `json:"label"`
}
type LabelList struct {
	Items        []Label      `json:"items"`
	HasMore      bool         `json:"has_more"`
	NextCursor   string       `json:"next_cursor"`
	Capabilities Capabilities `json:"capabilities"`
}
type Repository interface {
	CreateTeam(context.Context, uuid.UUID, CreateTeamInput) (Team, error)
	GetTeam(context.Context, uuid.UUID, uuid.UUID) (Team, error)
	ListTeams(context.Context, uuid.UUID, TeamListInput) ([]Team, error)
	UpdateTeam(context.Context, uuid.UUID, UpdateTeamInput) (Team, error)
	ArchiveTeam(context.Context, uuid.UUID, ArchiveTeamInput) (Team, error)
	ListStatuses(context.Context, uuid.UUID, StatusListInput) ([]Status, int64, error)
	CreateStatus(context.Context, uuid.UUID, CreateStatusInput) (Status, int64, error)
	UpdateStatus(context.Context, uuid.UUID, UpdateStatusInput) (Status, int64, error)
	ArchiveStatus(context.Context, uuid.UUID, ArchiveStatusInput) (Status, int64, error)
	Reorder(context.Context, uuid.UUID, ReorderInput) (int64, error)
	CreateLabel(context.Context, uuid.UUID, CreateLabelInput) (Label, error)
	GetLabel(context.Context, uuid.UUID, uuid.UUID) (Label, error)
	ListLabels(context.Context, uuid.UUID, LabelListInput) ([]Label, error)
	UpdateLabel(context.Context, uuid.UUID, UpdateLabelInput) (Label, error)
	ArchiveLabel(context.Context, uuid.UUID, ArchiveLabelInput) (Label, error)
	RecordEvent(context.Context, uuid.UUID, uuid.UUID, string, uuid.UUID, uuid.UUID, string, int64) error
}
type Policy interface {
	CanManage(context.Context, uuid.UUID) (bool, error)
}
