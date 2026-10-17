package usecase

import (
	"context"
	"time"

	"github.com/google/uuid"
)

type Workspace struct {
	Version   int64     `json:"version"`
	ID        uuid.UUID `json:"id"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}
type Credential struct {
	ID        uuid.UUID  `json:"id"`
	Name      string     `json:"name"`
	Audience  string     `json:"audience"`
	Scopes    []string   `json:"scopes"`
	ExpiresAt time.Time  `json:"expires_at"`
	RevokedAt *time.Time `json:"revoked_at"`
	CreatedAt time.Time  `json:"created_at"`
}

// Access is loaded from authoritative storage for every operation, never from a client grant.
type Access struct {
	UserID      uuid.UUID
	WorkspaceID uuid.UUID
	Role        string
	Scopes      []string
	Audience    string
	ExpiresAt   time.Time
}
type Repository interface {
	RecordWorkspaceEvent(context.Context, uuid.UUID, Workspace, string) error
	ListActivity(context.Context, uuid.UUID, uuid.UUID, int32) ([]Activity, error)
	ListDeliveries(context.Context, uuid.UUID, uuid.UUID, int32) ([]Delivery, error)
	ReplayDelivery(context.Context, uuid.UUID, uuid.UUID) error
	ActiveActor(context.Context, uuid.UUID) error
	ActiveOwner(context.Context, uuid.UUID) error
	Resolve(context.Context, uuid.UUID, uuid.UUID) (Access, error)
	ResolveCredential(context.Context, []byte) (Access, error)
	Create(context.Context, uuid.UUID, string) (Workspace, error)
	List(context.Context, uuid.UUID, uuid.UUID, int32) ([]Workspace, error)
	Get(context.Context, uuid.UUID, uuid.UUID) (Workspace, error)
	Update(context.Context, uuid.UUID, string, int64) (Workspace, error)
	CreateCredential(context.Context, uuid.UUID, uuid.UUID, CreateCredentialInput, []byte) (Credential, error)
	ListCredentials(context.Context, uuid.UUID, uuid.UUID, int32) ([]Credential, error)
	RevokeCredential(context.Context, uuid.UUID, uuid.UUID) (bool, error)
}

type CreateInput struct {
	Name string `json:"name"`
}
type UpdateInput struct {
	Name            string `json:"name"`
	ExpectedVersion int64  `json:"expected_version"`
}
type EmptyInput struct{}
type ListInput struct {
	Limit int32  `json:"limit"`
	After string `json:"after"`
}
type WorkspaceCapabilities struct {
	Update            bool `json:"update"`
	ReadCredentials   bool `json:"read_credentials"`
	ManageCredentials bool `json:"manage_credentials"`
}
type WorkspaceOutput struct {
	Workspace    Workspace             `json:"workspace"`
	Capabilities WorkspaceCapabilities `json:"capabilities"`
}
type WorkspaceList struct {
	CanCreate  bool        `json:"can_create"`
	Items      []Workspace `json:"items"`
	HasMore    bool        `json:"has_more"`
	NextCursor string      `json:"next_cursor"`
}
type CreateCredentialInput struct {
	Name      string    `json:"name"`
	Audience  string    `json:"audience"`
	Scopes    []string  `json:"scopes"`
	ExpiresAt time.Time `json:"expires_at"`
}
type CreateCredentialOutput struct {
	Credential Credential `json:"credential"`
	Token      string     `json:"token"`
}
type CredentialList struct {
	Items      []Credential `json:"items"`
	HasMore    bool         `json:"has_more"`
	NextCursor string       `json:"next_cursor"`
}
type RevokeInput struct {
	CredentialID uuid.UUID `json:"credential_id"`
}
type RevokeOutput struct {
	Revoked bool `json:"revoked"`
}

type Activity struct {
	ID          uuid.UUID `json:"id"`
	EventName   string    `json:"event_name"`
	ActorID     uuid.UUID `json:"actor_id"`
	WorkspaceID uuid.UUID `json:"workspace_id"`
	Name        string    `json:"name"`
	Version     int64     `json:"version"`
	OccurredAt  time.Time `json:"occurred_at"`
}
type ActivityList struct {
	Items      []Activity `json:"items"`
	HasMore    bool       `json:"has_more"`
	NextCursor string     `json:"next_cursor"`
}
type Delivery struct {
	ID              uuid.UUID  `json:"id"`
	EventID         uuid.UUID  `json:"event_id"`
	EventName       string     `json:"event_name"`
	Consumer        string     `json:"consumer"`
	ConsumerVersion int32      `json:"consumer_version"`
	Status          string     `json:"status"`
	Attempts        int32      `json:"attempts"`
	AvailableAt     time.Time  `json:"available_at"`
	LeaseUntil      *time.Time `json:"lease_until"`
	SucceededAt     *time.Time `json:"succeeded_at"`
	ErrorCode       string     `json:"error_code"`
}
type DeliveryList struct {
	Items      []Delivery `json:"items"`
	HasMore    bool       `json:"has_more"`
	NextCursor string     `json:"next_cursor"`
}
type ReplayDeliveryInput struct {
	DeliveryID uuid.UUID `json:"delivery_id"`
}
type ReplayDeliveryOutput struct {
	Replayed bool `json:"replayed"`
}
