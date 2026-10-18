package usecase

import (
	issue "github.com/furkanmeclis/traccia/backend/internal/modules/issues/usecase"
	relation "github.com/furkanmeclis/traccia/backend/internal/modules/relations/usecase"
	"github.com/google/uuid"
	"time"
)

// Timeline details reference immutable revisions; content is read losslessly
// through the corresponding get_revision operation.
type IssueDetail struct {
	Snapshot issue.Issue `json:"snapshot"`
}
type DocumentDetail struct {
	DocumentID    uuid.UUID `json:"document_id"`
	Revision      uuid.UUID `json:"revision"`
	Kind          string    `json:"kind"`
	SchemaVersion int32     `json:"schema_version"`
}
type CommentDetail struct {
	CommentID      uuid.UUID `json:"comment_id"`
	Revision       uuid.UUID `json:"revision"`
	Kind           string    `json:"kind"`
	Deleted        bool      `json:"deleted"`
	AuthorID       uuid.UUID `json:"author_id"`
	CreatedChannel string    `json:"created_channel"`
}
type RelationDetail struct {
	Snapshot relation.MutationOutput `json:"snapshot"`
}
type AttachmentDetail struct {
	AttachmentID uuid.UUID `json:"attachment_id"`
	FileRevision uuid.UUID `json:"file_revision"`
	Version      int64     `json:"version"`
	Filename     string    `json:"filename"`
	Size         int64     `json:"size"`
	SHA256       string    `json:"sha256"`
	Removed      bool      `json:"removed"`
}
type LegacyDetail struct {
	ResourceType string  `json:"resource_type"`
	ResourceID   *string `json:"resource_id"`
}

// Exactly one detail is non-null, selected by kind in the closed schema.
type TimelineDetail struct {
	Kind       string            `json:"kind"`
	Issue      *IssueDetail      `json:"issue"`
	Document   *DocumentDetail   `json:"document"`
	Comment    *CommentDetail    `json:"comment"`
	Relation   *RelationDetail   `json:"relation"`
	Attachment *AttachmentDetail `json:"attachment"`
	Legacy     *LegacyDetail     `json:"legacy"`
}
type TimelineItem struct {
	ID          uuid.UUID      `json:"id"`
	WorkspaceID uuid.UUID      `json:"workspace_id"`
	IssueID     uuid.UUID      `json:"issue_id"`
	EventName   string         `json:"event_name"`
	ActorID     uuid.UUID      `json:"actor_id"`
	Channel     string         `json:"channel"`
	Name        string         `json:"name"`
	Version     int64          `json:"version"`
	OccurredAt  time.Time      `json:"occurred_at"`
	Detail      TimelineDetail `json:"detail"`
}
type TimelineList struct {
	Items      []TimelineItem `json:"items"`
	HasMore    bool           `json:"has_more"`
	NextCursor string         `json:"next_cursor"`
}
