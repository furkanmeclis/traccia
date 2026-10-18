package usecase

import (
	"context"
	"github.com/google/uuid"
	"time"
)

type IssueReference struct {
	ID              uuid.UUID `json:"id"`
	WorkspaceID     uuid.UUID `json:"workspace_id"`
	TeamID          uuid.UUID `json:"team_id"`
	Identifier      string    `json:"identifier"`
	Title           string    `json:"title"`
	Archived        bool      `json:"archived"`
	Active          bool      `json:"active"`
	RelationVersion int64     `json:"relation_version"`
}
type Capabilities struct {
	Link   bool `json:"link"`
	Unlink bool `json:"unlink"`
}
type State struct {
	Issue              IssueReference  `json:"issue"`
	Parent             *IssueReference `json:"parent"`
	DuplicateOf        *IssueReference `json:"duplicate_of"`
	CanonicalDuplicate *IssueReference `json:"canonical_duplicate"`
	Capabilities       Capabilities    `json:"capabilities"`
}
type Relation struct {
	ID          uuid.UUID      `json:"id"`
	WorkspaceID uuid.UUID      `json:"workspace_id"`
	Kind        string         `json:"kind"`
	Source      IssueReference `json:"source"`
	Target      IssueReference `json:"target"`
	Version     int64          `json:"version"`
	Removed     bool           `json:"removed"`
	CreatedAt   time.Time      `json:"created_at"`
	RemovedAt   *time.Time     `json:"removed_at"`
}
type ExpectedVersion struct {
	IssueID         uuid.UUID `json:"issue_id"`
	ExpectedVersion int64     `json:"expected_version"`
}
type SetInput struct {
	IssueID          uuid.UUID         `json:"issue_id"`
	TargetIssueID    *string           `json:"target_issue_id"`
	ExpectedVersions []ExpectedVersion `json:"expected_versions"`
}
type LinkInput struct {
	IssueID          uuid.UUID         `json:"issue_id"`
	TargetIssueID    uuid.UUID         `json:"target_issue_id"`
	ExpectedVersions []ExpectedVersion `json:"expected_versions"`
}
type PageInput struct {
	IssueID   uuid.UUID `json:"issue_id"`
	Kind      string    `json:"kind"`
	Direction string    `json:"direction"`
	Removed   bool      `json:"removed"`
	Limit     int32     `json:"limit"`
	After     string    `json:"after"`
}
type HistoryInput struct {
	IssueID uuid.UUID `json:"issue_id"`
	Limit   int32     `json:"limit"`
	After   string    `json:"after"`
}
type MutationOutput struct {
	State            State            `json:"state"`
	ChangedRelations []Relation       `json:"changed_relations"`
	AffectedIssues   []IssueReference `json:"affected_issues"`
}
type List struct {
	State      State      `json:"state"`
	Items      []Relation `json:"items"`
	HasMore    bool       `json:"has_more"`
	NextCursor string     `json:"next_cursor"`
}
type History struct {
	ID          uuid.UUID      `json:"id"`
	WorkspaceID uuid.UUID      `json:"workspace_id"`
	IssueID     uuid.UUID      `json:"issue_id"`
	ActorID     uuid.UUID      `json:"actor_id"`
	EventName   string         `json:"event_name"`
	Version     int64          `json:"version"`
	OccurredAt  time.Time      `json:"occurred_at"`
	Snapshot    MutationOutput `json:"snapshot"`
}
type HistoryList struct {
	Items      []History `json:"items"`
	HasMore    bool      `json:"has_more"`
	NextCursor string    `json:"next_cursor"`
}
type Policy interface {
	CanManage(context.Context, uuid.UUID) (bool, error)
}
type Mutation struct {
	IssueID, TargetIssueID uuid.UUID
	Kind                   string
	Remove, Set            bool
	ExpectedVersions       []ExpectedVersion
}
type Repository interface {
	Mutate(context.Context, uuid.UUID, uuid.UUID, Mutation) (MutationOutput, error)
	List(context.Context, uuid.UUID, PageInput) (List, error)
	History(context.Context, uuid.UUID, HistoryInput) ([]History, error)
}
