package usecase

import (
	"context"
	issue "github.com/furkanmeclis/traccia/backend/internal/modules/issues/usecase"
	"github.com/google/uuid"
	"time"
)

const Format = "plate"
const SchemaVersion = 1
const MaxNodes = 2000
const MaxDepth = 32
const MaxContentBytes = 512 * 1024

type Capabilities struct {
	Manage bool `json:"manage"`
}
type Document struct {
	ID            uuid.UUID    `json:"id"`
	WorkspaceID   uuid.UUID    `json:"workspace_id"`
	OwnerKind     string       `json:"owner_kind"`
	OwnerID       uuid.UUID    `json:"owner_id"`
	Format        string       `json:"format"`
	SchemaVersion int32        `json:"schema_version"`
	Revision      uuid.UUID    `json:"revision"`
	Content       []Node       `json:"content"`
	CreatedAt     time.Time    `json:"created_at"`
	UpdatedAt     time.Time    `json:"updated_at"`
	Capabilities  Capabilities `json:"capabilities"`
}
type Output struct {
	Document Document `json:"document"`
}
type ResolveOutput struct {
	Document     *Document    `json:"document"`
	Capabilities Capabilities `json:"capabilities"`
}
type ResolveInput struct {
	OwnerKind string    `json:"owner_kind"`
	OwnerID   uuid.UUID `json:"owner_id"`
}
type CreateInput struct {
	OwnerKind     string    `json:"owner_kind"`
	OwnerID       uuid.UUID `json:"owner_id"`
	Format        string    `json:"format"`
	SchemaVersion int32     `json:"schema_version"`
	Content       []Node    `json:"content"`
}
type DocumentInput struct {
	DocumentID uuid.UUID `json:"document_id"`
}
type ReplaceInput struct {
	DocumentID       uuid.UUID `json:"document_id"`
	ExpectedRevision uuid.UUID `json:"expected_revision"`
	Format           string    `json:"format"`
	SchemaVersion    int32     `json:"schema_version"`
	Content          []Node    `json:"content"`
}
type PatchInput struct {
	DocumentID       uuid.UUID `json:"document_id"`
	ExpectedRevision uuid.UUID `json:"expected_revision"`
	Format           string    `json:"format"`
	SchemaVersion    int32     `json:"schema_version"`
	Patches          []Patch   `json:"patches"`
}
type RevisionsInput struct {
	DocumentID uuid.UUID `json:"document_id"`
	Limit      int32     `json:"limit"`
	After      string    `json:"after"`
}
type RevisionInput struct {
	DocumentID uuid.UUID `json:"document_id"`
	RevisionID uuid.UUID `json:"revision_id"`
}
type RestoreInput struct {
	DocumentID       uuid.UUID `json:"document_id"`
	ExpectedRevision uuid.UUID `json:"expected_revision"`
	RevisionID       uuid.UUID `json:"revision_id"`
}
type Revision struct {
	ID             uuid.UUID `json:"id"`
	DocumentID     uuid.UUID `json:"document_id"`
	ParentRevision *string   `json:"parent_revision"`
	Kind           string    `json:"kind"`
	ActorID        uuid.UUID `json:"actor_id"`
	OccurredAt     time.Time `json:"occurred_at"`
}
type RevisionOutput struct {
	Document Document `json:"document"`
	Revision Revision `json:"revision"`
}
type RevisionList struct {
	Items      []Revision `json:"items"`
	HasMore    bool       `json:"has_more"`
	NextCursor string     `json:"next_cursor"`
}
type ExportInput struct {
	DocumentID uuid.UUID `json:"document_id"`
	Format     string    `json:"format"`
}
type ExportOutput struct {
	DocumentID      uuid.UUID `json:"document_id"`
	Revision        uuid.UUID `json:"revision"`
	Format          string    `json:"format"`
	Content         string    `json:"content"`
	Losses          []string  `json:"losses"`
	RendererVersion int32     `json:"renderer_version"`
}
type RichCreateInput struct {
	Issue         issue.CreateInput `json:"issue"`
	Format        string            `json:"format"`
	SchemaVersion int32             `json:"schema_version"`
	Content       []Node            `json:"content"`
}
type RichOutput struct {
	Issue    issue.Output `json:"issue"`
	Document Document     `json:"document"`
}
type BacklinksInput struct {
	TargetKind string    `json:"target_kind"`
	TargetID   uuid.UUID `json:"target_id"`
	Limit      int32     `json:"limit"`
	After      string    `json:"after"`
}
type Backlink struct {
	ID         uuid.UUID `json:"id"`
	DocumentID uuid.UUID `json:"document_id"`
	OwnerKind  string    `json:"owner_kind"`
	OwnerID    uuid.UUID `json:"owner_id"`
	NodeID     uuid.UUID `json:"node_id"`
	Revision   uuid.UUID `json:"revision"`
	Value      string    `json:"value"`
}
type BacklinkList struct {
	Items      []Backlink `json:"items"`
	HasMore    bool       `json:"has_more"`
	NextCursor string     `json:"next_cursor"`
}
type Policy interface {
	CanManage(context.Context, uuid.UUID) (bool, error)
}

type Mutation struct {
	DocumentID, OwnerID, ExpectedRevision, SourceRevision uuid.UUID
	OwnerKind, Kind                                       string
	Content                                               []Node
	Patches                                               []Patch
}
type Reference struct {
	NodeID     uuid.UUID
	TargetKind string
	TargetID   uuid.UUID
	Value      string
}
type Repository interface {
	PrepareRich(context.Context, uuid.UUID, issue.CreateInput, []Node) error
	Resolve(context.Context, uuid.UUID, ResolveInput) (*Document, bool, error)
	OwnerActive(context.Context, uuid.UUID, string, uuid.UUID) (bool, error)
	Get(context.Context, uuid.UUID, uuid.UUID, uuid.UUID) (Document, Revision, error)
	Mutate(context.Context, uuid.UUID, uuid.UUID, Mutation) (Document, error)
	Revisions(context.Context, uuid.UUID, RevisionsInput) ([]Revision, error)
	Backlinks(context.Context, uuid.UUID, BacklinksInput) ([]Backlink, error)
}

func References(content []Node) []Reference {
	out := []Reference{}
	var walk func([]Node)
	walk = func(nodes []Node) {
		for _, node := range nodes {
			if mention, ok := node.Value.(*Mention); ok {
				id, _ := canonicalID(mention.ID)
				target, _ := canonicalID(mention.TargetID)
				out = append(out, Reference{NodeID: id, TargetKind: mention.TargetKind, TargetID: target, Value: mention.Value})
			}
			if element := node.element(); element != nil {
				walk(element.Children)
			}
		}
	}
	walk(content)
	return out
}
