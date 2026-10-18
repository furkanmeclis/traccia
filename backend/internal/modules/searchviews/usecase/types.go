package usecase

import (
	"context"
	"github.com/google/uuid"
	"time"
)

const (
	MaxDepth   = 8
	MaxNodes   = 128
	MaxSorts   = 5
	MaxPage    = 100
	MaxSet     = 100
	MaxText    = 256
	DefaultTTL = 1800
	MinTTL     = 60
	MaxTTL     = 86400
)

// Filter is a closed recursive tagged union. Only the fields belonging to its
// kind are present on the wire; the published schema describes every branch.
type Filter struct {
	Kind     string      `json:"kind"`
	Children []Filter    `json:"-"`
	Child    *Filter     `json:"-"`
	Field    string      `json:"field,omitempty"`
	Op       string      `json:"op,omitempty"`
	IDs      []uuid.UUID `json:"ids,omitempty"`
	Values   []string    `json:"values,omitempty"`
	Value    string      `json:"value,omitempty"`
	Number   *int32      `json:"number,omitempty"`
}
type Sort struct {
	Field     string `json:"field"`
	Direction string `json:"direction"`
	Nulls     string `json:"nulls"`
}
type Query struct {
	Version  int32   `json:"version"`
	Archived string  `json:"archived"`
	Search   string  `json:"search"`
	Filter   *Filter `json:"filter"`
	Sort     []Sort  `json:"sort"`
}
type IssueQuery Query
type ProjectQuery Query
type TeamQuery Query
type IssueCreateInput struct {
	RequestID  uuid.UUID  `json:"request_id"`
	Query      IssueQuery `json:"query"`
	TTLSeconds *int32     `json:"ttl_seconds"`
	Limit      int32      `json:"limit"`
}
type ProjectCreateInput struct {
	RequestID  uuid.UUID    `json:"request_id"`
	Query      ProjectQuery `json:"query"`
	TTLSeconds *int32       `json:"ttl_seconds"`
	Limit      int32        `json:"limit"`
}
type TeamCreateInput struct {
	RequestID  uuid.UUID `json:"request_id"`
	Query      TeamQuery `json:"query"`
	TTLSeconds *int32    `json:"ttl_seconds"`
	Limit      int32     `json:"limit"`
}
type PageInput struct {
	SessionID uuid.UUID `json:"session_id"`
	Limit     int32     `json:"limit"`
	After     string    `json:"after"`
}
type Session struct {
	ID          uuid.UUID `json:"id"`
	RequestID   uuid.UUID `json:"request_id"`
	WorkspaceID uuid.UUID `json:"workspace_id"`
	Resource    string    `json:"resource"`
	AsOf        time.Time `json:"as_of"`
	ExpiresAt   time.Time `json:"expires_at"`
	TotalCount  string    `json:"total_count"`
}
type IssueRow struct {
	ID          uuid.UUID `json:"id"`
	WorkspaceID uuid.UUID `json:"workspace_id"`
	TeamID      uuid.UUID `json:"team_id"`
	Identifier  string    `json:"identifier"`
	Number      string    `json:"number"`
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
	Version     string    `json:"version"`
	Archived    bool      `json:"archived"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}
type ProjectRow struct {
	ID          uuid.UUID `json:"id"`
	WorkspaceID uuid.UUID `json:"workspace_id"`
	Name        string    `json:"name"`
	Summary     string    `json:"summary"`
	LeadUserID  *string   `json:"lead_user_id"`
	TeamIDs     []string  `json:"team_ids"`
	StartDate   *string   `json:"start_date"`
	TargetDate  *string   `json:"target_date"`
	Version     string    `json:"version"`
	Archived    bool      `json:"archived"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}
type TeamRow struct {
	ID              uuid.UUID `json:"id"`
	WorkspaceID     uuid.UUID `json:"workspace_id"`
	Name            string    `json:"name"`
	Code            string    `json:"code"`
	Version         string    `json:"version"`
	WorkflowVersion string    `json:"workflow_version"`
	Archived        bool      `json:"archived"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}
type IssuePage struct {
	Session    Session    `json:"session"`
	Query      IssueQuery `json:"query"`
	Items      []IssueRow `json:"items"`
	HasMore    bool       `json:"has_more"`
	NextCursor string     `json:"next_cursor"`
}
type ProjectPage struct {
	Session    Session      `json:"session"`
	Query      ProjectQuery `json:"query"`
	Items      []ProjectRow `json:"items"`
	HasMore    bool         `json:"has_more"`
	NextCursor string       `json:"next_cursor"`
}
type TeamPage struct {
	Session    Session   `json:"session"`
	Query      TeamQuery `json:"query"`
	Items      []TeamRow `json:"items"`
	HasMore    bool      `json:"has_more"`
	NextCursor string    `json:"next_cursor"`
}
type Limits struct {
	MaxRawInputBytes  int32 `json:"max_raw_input_bytes"`
	MaxInputBytes     int32 `json:"max_input_bytes"`
	DefaultTTLSeconds int32 `json:"default_ttl_seconds"`
	MinTTLSeconds     int32 `json:"min_ttl_seconds"`
	MaxTTLSeconds     int32 `json:"max_ttl_seconds"`
	MaxDepth          int32 `json:"max_depth"`
	MaxNodes          int32 `json:"max_nodes"`
	MaxSorts          int32 `json:"max_sorts"`
	MaxPage           int32 `json:"max_page"`
	MaxSet            int32 `json:"max_set"`
	MaxText           int32 `json:"max_text"`
}
type ManageCaps struct {
	Manage bool `json:"manage"`
}
type ConfigInput struct{}
type ConfigOutput struct {
	Limits          Limits     `json:"limits"`
	Capabilities    ManageCaps `json:"capabilities"`
	InputBytePolicy string     `json:"input_byte_policy"`
	TextMatching    string     `json:"text_matching"`
	TextOrdering    string     `json:"text_ordering"`
}
type Column struct {
	Key   string `json:"key"`
	Width int32  `json:"width"`
}

// ViewQuery carries exactly one concrete query, selected by resource.
type ViewQuery struct {
	Resource string        `json:"resource"`
	Issue    *IssueQuery   `json:"issue"`
	Project  *ProjectQuery `json:"project"`
	Team     *TeamQuery    `json:"team"`
}
type ViewCaps struct {
	Manage  bool `json:"manage"`
	Update  bool `json:"update"`
	Delete  bool `json:"delete"`
	Restore bool `json:"restore"`
}
type SavedView struct {
	ID           uuid.UUID `json:"id"`
	WorkspaceID  uuid.UUID `json:"workspace_id"`
	ActorID      uuid.UUID `json:"actor_id"`
	Revision     uuid.UUID `json:"revision"`
	Name         string    `json:"name"`
	Definition   ViewQuery `json:"definition"`
	Columns      []Column  `json:"columns"`
	Deleted      bool      `json:"deleted"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
	Capabilities ViewCaps  `json:"capabilities"`
}
type ViewCreateInput struct {
	Name       string    `json:"name"`
	Definition ViewQuery `json:"definition"`
	Columns    []Column  `json:"columns"`
}
type ViewInput struct {
	ViewID uuid.UUID `json:"view_id"`
}
type ViewUpdateInput struct {
	ViewID           uuid.UUID `json:"view_id"`
	ExpectedRevision uuid.UUID `json:"expected_revision"`
	Name             string    `json:"name"`
	Definition       ViewQuery `json:"definition"`
	Columns          []Column  `json:"columns"`
}
type ViewVersionInput struct {
	ViewID           uuid.UUID `json:"view_id"`
	ExpectedRevision uuid.UUID `json:"expected_revision"`
}
type ViewListInput struct {
	Resource       string `json:"resource"`
	IncludeDeleted bool   `json:"include_deleted"`
	Limit          int32  `json:"limit"`
	After          string `json:"after"`
}
type ViewOutput struct {
	View SavedView `json:"view"`
}
type ViewList struct {
	Items        []SavedView `json:"items"`
	HasMore      bool        `json:"has_more"`
	NextCursor   string      `json:"next_cursor"`
	Capabilities ManageCaps  `json:"capabilities"`
}
type ColumnsInput struct {
	Resource string `json:"resource"`
}
type ColumnsSetInput struct {
	Resource         string     `json:"resource"`
	ExpectedRevision *uuid.UUID `json:"expected_revision"`
	Columns          []Column   `json:"columns"`
}
type ColumnPreferences struct {
	Resource     string     `json:"resource"`
	Revision     *uuid.UUID `json:"revision"`
	Columns      []Column   `json:"columns"`
	Capabilities ManageCaps `json:"capabilities"`
}
type Capture struct {
	RequestedTTL *int32
	RequestID    uuid.UUID
	Query        Query
	TTLSeconds   int32
	Limit        int32
}
type StoredPage struct {
	Session    Session
	Query      Query
	Rows       [][]byte
	HasMore    bool
	NextCursor string
}
type ViewMutation struct {
	Kind                     string
	ViewID, ExpectedRevision uuid.UUID
	Name                     string
	Definition               ViewQuery
	Columns                  []Column
}
type Policy interface {
	CanManage(context.Context, uuid.UUID) (bool, error)
	CanRead(context.Context, uuid.UUID) error
}
type Repository interface {
	Capture(context.Context, uuid.UUID, uuid.UUID, string, Capture, func(context.Context) error) (StoredPage, error)
	Page(context.Context, uuid.UUID, uuid.UUID, string, PageInput) (StoredPage, error)
	GetView(context.Context, uuid.UUID, uuid.UUID, uuid.UUID) (SavedView, error)
	ListViews(context.Context, uuid.UUID, uuid.UUID, ViewListInput) ([]SavedView, error)
	MutateView(context.Context, uuid.UUID, uuid.UUID, ViewMutation) (SavedView, error)
	GetColumns(context.Context, uuid.UUID, uuid.UUID, string) (ColumnPreferences, error)
	SetColumns(context.Context, uuid.UUID, uuid.UUID, ColumnsSetInput) (ColumnPreferences, error)
}
