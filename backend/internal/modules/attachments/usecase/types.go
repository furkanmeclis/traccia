package usecase

import (
	"context"
	"github.com/google/uuid"
	"io"
	"time"
)

const MaxFileBytes int64 = 256 << 20
const MaxChunkBytes int32 = 256 << 10
const DefaultUploadTTL = 24 * time.Hour
const DefaultFinalizedRetention = 7 * 24 * time.Hour
const BlobFallbackMaxBytes int64 = MaxFileBytes

type Limits struct {
	MaxChunkRequestBytes      int64 `json:"max_chunk_request_bytes"`
	MaxFileBytes              int64 `json:"max_file_bytes"`
	ChunkBytes                int32 `json:"chunk_bytes"`
	UploadInactivitySeconds   int64 `json:"upload_inactivity_seconds"`
	FinalizedRetentionSeconds int64 `json:"finalized_retention_seconds"`
	BlobFallbackMaxBytes      int64 `json:"blob_fallback_max_bytes"`
}
type GeneralCapabilities struct {
	Manage bool `json:"manage"`
}
type LimitsOutput struct {
	Capabilities GeneralCapabilities `json:"capabilities"`
	Limits       Limits              `json:"limits"`
}
type Empty struct{}
type UploadCapabilities struct {
	UploadChunk bool `json:"upload_chunk"`
	Finalize    bool `json:"finalize"`
	Abort       bool `json:"abort"`
	Bind        bool `json:"bind"`
}
type Cleanup struct {
	State       string  `json:"state"`
	LastError   string  `json:"last_error"`
	NextCheckAt *string `json:"next_check_at"`
}
type Upload struct {
	ID           uuid.UUID          `json:"id"`
	WorkspaceID  uuid.UUID          `json:"workspace_id"`
	ActorID      uuid.UUID          `json:"actor_id"`
	DraftID      uuid.UUID          `json:"draft_id"`
	Filename     string             `json:"filename"`
	ContentType  string             `json:"content_type"`
	SizeBytes    int64              `json:"size_bytes"`
	SHA256       string             `json:"sha256"`
	ChunkBytes   int32              `json:"chunk_bytes"`
	NextOffset   int64              `json:"next_offset"`
	Version      int64              `json:"version"`
	State        string             `json:"state"`
	ExpiresAt    *string            `json:"expires_at"`
	AttachmentID *string            `json:"attachment_id"`
	CreatedAt    time.Time          `json:"created_at"`
	UpdatedAt    time.Time          `json:"updated_at"`
	Cleanup      Cleanup            `json:"cleanup"`
	Capabilities UploadCapabilities `json:"capabilities"`
}
type AttachmentCapabilities struct {
	History  bool `json:"history"`
	Download bool `json:"download"`
	Bind     bool `json:"bind"`
	Remove   bool `json:"remove"`
}
type Attachment struct {
	State            string                 `json:"state"`
	ID               uuid.UUID              `json:"id"`
	WorkspaceID      uuid.UUID              `json:"workspace_id"`
	UploadID         *uuid.UUID             `json:"upload_id"`
	ActorID          uuid.UUID              `json:"actor_id"`
	DraftID          *uuid.UUID             `json:"draft_id"`
	FileRevision     uuid.UUID              `json:"file_revision"`
	Version          int64                  `json:"version"`
	Filename         string                 `json:"filename"`
	DownloadFilename string                 `json:"download_filename"`
	ContentType      string                 `json:"content_type"`
	SizeBytes        int64                  `json:"size_bytes"`
	SHA256           string                 `json:"sha256"`
	IssueID          *string                `json:"issue_id"`
	Removed          bool                   `json:"removed"`
	FinalizedAt      time.Time              `json:"finalized_at"`
	ExpiresAt        *string                `json:"expires_at"`
	DownloadPath     string                 `json:"download_path"`
	Capabilities     AttachmentCapabilities `json:"capabilities"`
}
type UploadOutput struct {
	Upload Upload `json:"upload"`
}
type AttachmentOutput struct {
	Attachment Attachment `json:"attachment"`
}
type FinalizeOutput struct {
	Upload     Upload     `json:"upload"`
	Attachment Attachment `json:"attachment"`
}
type CreateInput struct {
	DraftID     uuid.UUID `json:"draft_id"`
	Filename    string    `json:"filename"`
	ContentType string    `json:"content_type"`
	SizeBytes   int64     `json:"size_bytes"`
	SHA256      string    `json:"sha256"`
}
type UploadInput struct {
	UploadID uuid.UUID `json:"upload_id"`
	DraftID  uuid.UUID `json:"draft_id"`
}
type VersionInput struct {
	UploadID        uuid.UUID `json:"upload_id"`
	DraftID         uuid.UUID `json:"draft_id"`
	ExpectedVersion int64     `json:"expected_version"`
}
type ChunkInput struct {
	UploadID        uuid.UUID `json:"upload_id"`
	DraftID         uuid.UUID `json:"draft_id"`
	ExpectedVersion int64     `json:"expected_version"`
	Offset          int64     `json:"offset"`
	TotalBytes      int64     `json:"total_bytes"`
	DataBase64      string    `json:"data_base64"`
	ChunkSHA256     string    `json:"chunk_sha256"`
}
type UploadListInput struct {
	DraftID string `json:"draft_id"`
	State   string `json:"state"`
	Limit   int32  `json:"limit"`
	After   string `json:"after"`
}
type UploadList struct {
	Items      []Upload `json:"items"`
	HasMore    bool     `json:"has_more"`
	NextCursor string   `json:"next_cursor"`
}
type AttachmentInput struct {
	AttachmentID uuid.UUID `json:"attachment_id"`
	DraftID      string    `json:"draft_id"`
}
type BindInput struct {
	AttachmentID    uuid.UUID `json:"attachment_id"`
	DraftID         uuid.UUID `json:"draft_id"`
	IssueID         uuid.UUID `json:"issue_id"`
	ExpectedVersion int64     `json:"expected_version"`
}
type RemoveInput struct {
	AttachmentID    uuid.UUID `json:"attachment_id"`
	ExpectedVersion int64     `json:"expected_version"`
}
type ListInput struct {
	IssueID uuid.UUID `json:"issue_id"`
	Removed bool      `json:"removed"`
	Search  string    `json:"search"`
	Limit   int32     `json:"limit"`
	After   string    `json:"after"`
}
type ListCapabilities struct {
	Bind   bool `json:"bind"`
	Remove bool `json:"remove"`
}
type AttachmentList struct {
	Items        []Attachment     `json:"items"`
	HasMore      bool             `json:"has_more"`
	NextCursor   string           `json:"next_cursor"`
	Capabilities ListCapabilities `json:"capabilities"`
}
type DownloadInput struct {
	AttachmentID uuid.UUID `json:"attachment_id"`
	DraftID      string    `json:"draft_id"`
	FileRevision uuid.UUID `json:"file_revision"`
	Offset       int64     `json:"offset"`
	Limit        int32     `json:"limit"`
}
type DownloadOutput struct {
	AttachmentID       uuid.UUID `json:"attachment_id"`
	FileRevision       uuid.UUID `json:"file_revision"`
	Filename           string    `json:"filename"`
	DownloadFilename   string    `json:"download_filename"`
	ContentType        string    `json:"content_type"`
	ContentDisposition string    `json:"content_disposition"`
	SizeBytes          int64     `json:"size_bytes"`
	SHA256             string    `json:"sha256"`
	Offset             int64     `json:"offset"`
	DataBase64         string    `json:"data_base64"`
	ChunkSHA256        string    `json:"chunk_sha256"`
	NextOffset         int64     `json:"next_offset"`
	EOF                bool      `json:"eof"`
}
type UploadHistoryInput struct {
	UploadID uuid.UUID `json:"upload_id"`
	DraftID  uuid.UUID `json:"draft_id"`
	Limit    int32     `json:"limit"`
	After    string    `json:"after"`
}
type HistoryInput struct {
	AttachmentID uuid.UUID `json:"attachment_id"`
	DraftID      string    `json:"draft_id"`
	Limit        int32     `json:"limit"`
	After        string    `json:"after"`
}
type UploadHistory struct {
	ID         uuid.UUID `json:"id"`
	ActorID    uuid.UUID `json:"actor_id"`
	EventName  string    `json:"event_name"`
	Version    int64     `json:"version"`
	OccurredAt time.Time `json:"occurred_at"`
	Snapshot   Upload    `json:"snapshot"`
}
type History struct {
	ID         uuid.UUID  `json:"id"`
	ActorID    uuid.UUID  `json:"actor_id"`
	EventName  string     `json:"event_name"`
	Version    int64      `json:"version"`
	OccurredAt time.Time  `json:"occurred_at"`
	Snapshot   Attachment `json:"snapshot"`
}
type UploadHistoryList struct {
	Items      []UploadHistory `json:"items"`
	HasMore    bool            `json:"has_more"`
	NextCursor string          `json:"next_cursor"`
}
type HistoryList struct {
	Items      []History `json:"items"`
	HasMore    bool      `json:"has_more"`
	NextCursor string    `json:"next_cursor"`
}

// Store supports bounded object writes and precise range reads. Keys are server
// authority and never appear in public request or response types.
type Store interface {
	Put(context.Context, string, []byte) error
	OpenRange(context.Context, string, int64, int64) (io.ReadCloser, int64, error)
	DeletePrefix(context.Context, string) error
}
type Policy interface {
	CanRead(context.Context, uuid.UUID) error
	CanManage(context.Context, uuid.UUID) (bool, error)
}
type Options struct {
	MaxFileBytes       int64
	ChunkBytes         int32
	UploadTTL          time.Duration
	FinalizedRetention time.Duration
	CleanupInterval    time.Duration
}
