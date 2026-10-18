package usecase

import (
	"context"
	plate "github.com/furkanmeclis/traccia/backend/internal/modules/documents/usecase"
	"github.com/google/uuid"
	"time"
)

type Capabilities struct {
	Manage  bool `json:"manage"`
	Replace bool `json:"replace"`
	Patch   bool `json:"patch"`
	Delete  bool `json:"delete"`
	Restore bool `json:"restore"`
}
type Comment struct {
	ID             uuid.UUID    `json:"id"`
	WorkspaceID    uuid.UUID    `json:"workspace_id"`
	IssueID        uuid.UUID    `json:"issue_id"`
	AuthorID       uuid.UUID    `json:"author_id"`
	CreatedChannel string       `json:"created_channel"`
	Revision       uuid.UUID    `json:"revision"`
	Format         string       `json:"format"`
	SchemaVersion  int32        `json:"schema_version"`
	Content        []plate.Node `json:"content"`
	Deleted        bool         `json:"deleted"`
	CreatedAt      time.Time    `json:"created_at"`
	UpdatedAt      time.Time    `json:"updated_at"`
	Capabilities   Capabilities `json:"capabilities"`
}
type Output struct {
	Comment Comment `json:"comment"`
}
type CreateInput struct {
	IssueID       uuid.UUID    `json:"issue_id"`
	Format        string       `json:"format"`
	SchemaVersion int32        `json:"schema_version"`
	Content       []plate.Node `json:"content"`
}
type CommentInput struct {
	CommentID uuid.UUID `json:"comment_id"`
}
type ReplaceInput struct {
	CommentID        uuid.UUID    `json:"comment_id"`
	ExpectedRevision uuid.UUID    `json:"expected_revision"`
	Format           string       `json:"format"`
	SchemaVersion    int32        `json:"schema_version"`
	Content          []plate.Node `json:"content"`
}
type PatchInput struct {
	CommentID        uuid.UUID     `json:"comment_id"`
	ExpectedRevision uuid.UUID     `json:"expected_revision"`
	Format           string        `json:"format"`
	SchemaVersion    int32         `json:"schema_version"`
	Patches          []plate.Patch `json:"patches"`
}
type DeleteInput struct {
	CommentID        uuid.UUID `json:"comment_id"`
	ExpectedRevision uuid.UUID `json:"expected_revision"`
}
type RestoreInput struct {
	CommentID        uuid.UUID `json:"comment_id"`
	ExpectedRevision uuid.UUID `json:"expected_revision"`
	RevisionID       uuid.UUID `json:"revision_id"`
}
type ListInput struct {
	IssueID        uuid.UUID `json:"issue_id"`
	IncludeDeleted bool      `json:"include_deleted"`
	Limit          int32     `json:"limit"`
	After          string    `json:"after"`
}
type ListCapabilities struct {
	Manage bool `json:"manage"`
	Create bool `json:"create"`
}
type List struct {
	Items        []Comment        `json:"items"`
	Capabilities ListCapabilities `json:"capabilities"`
	HasMore      bool             `json:"has_more"`
	NextCursor   string           `json:"next_cursor"`
}
type RevisionsInput struct {
	CommentID uuid.UUID `json:"comment_id"`
	Limit     int32     `json:"limit"`
	After     string    `json:"after"`
}
type RevisionInput struct {
	CommentID  uuid.UUID `json:"comment_id"`
	RevisionID uuid.UUID `json:"revision_id"`
}
type RevisionCapabilities struct {
	Restore bool `json:"restore"`
}
type Revision struct {
	ID             uuid.UUID            `json:"id"`
	CommentID      uuid.UUID            `json:"comment_id"`
	ParentRevision *string              `json:"parent_revision"`
	SourceRevision *string              `json:"source_revision"`
	Kind           string               `json:"kind"`
	ActorID        uuid.UUID            `json:"actor_id"`
	Channel        string               `json:"channel"`
	Deleted        bool                 `json:"deleted"`
	OccurredAt     time.Time            `json:"occurred_at"`
	Capabilities   RevisionCapabilities `json:"capabilities"`
}
type RevisionOutput struct {
	Comment  Comment  `json:"comment"`
	Revision Revision `json:"revision"`
}
type RevisionList struct {
	Items      []Revision `json:"items"`
	HasMore    bool       `json:"has_more"`
	NextCursor string     `json:"next_cursor"`
}
type ExportInput struct {
	CommentID uuid.UUID `json:"comment_id"`
	Format    string    `json:"format"`
}
type ExportOutput struct {
	CommentID       uuid.UUID `json:"comment_id"`
	Revision        uuid.UUID `json:"revision"`
	Format          string    `json:"format"`
	Content         string    `json:"content"`
	Losses          []string  `json:"losses"`
	RendererVersion int32     `json:"renderer_version"`
}
type BacklinksInput struct {
	TargetKind string    `json:"target_kind"`
	TargetID   uuid.UUID `json:"target_id"`
	Limit      int32     `json:"limit"`
	After      string    `json:"after"`
}
type Backlink struct {
	ID        uuid.UUID `json:"id"`
	CommentID uuid.UUID `json:"comment_id"`
	IssueID   uuid.UUID `json:"issue_id"`
	NodeID    uuid.UUID `json:"node_id"`
	Revision  uuid.UUID `json:"revision"`
	Value     string    `json:"value"`
}
type BacklinkList struct {
	Items      []Backlink `json:"items"`
	HasMore    bool       `json:"has_more"`
	NextCursor string     `json:"next_cursor"`
}
type TimelineInput struct {
	IssueID uuid.UUID `json:"issue_id"`
	Limit   int32     `json:"limit"`
	After   string    `json:"after"`
}
type Policy interface {
	CanManage(context.Context, uuid.UUID) (bool, error)
}
type Mutation struct {
	CommentID, IssueID, ExpectedRevision, SourceRevision uuid.UUID
	Kind, Channel                                        string
	Content                                              []plate.Node
	Patches                                              []plate.Patch
}
type Repository interface {
	Get(context.Context, uuid.UUID, uuid.UUID, uuid.UUID) (Comment, Revision, error)
	Active(context.Context, uuid.UUID, uuid.UUID) (bool, error)
	Mutate(context.Context, uuid.UUID, uuid.UUID, Mutation) (Comment, error)
	List(context.Context, uuid.UUID, ListInput) ([]Comment, error)
	Revisions(context.Context, uuid.UUID, RevisionsInput) ([]Revision, error)
	Backlinks(context.Context, uuid.UUID, BacklinksInput) ([]Backlink, error)
	Timeline(context.Context, uuid.UUID, TimelineInput) ([]TimelineItem, error)
}
