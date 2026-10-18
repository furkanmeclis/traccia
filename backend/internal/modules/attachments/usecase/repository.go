package usecase

import (
	"context"
	"github.com/google/uuid"
)

type Repository interface {
	Create(context.Context, uuid.UUID, uuid.UUID, CreateInput) (Upload, error)
	GetUpload(context.Context, uuid.UUID, uuid.UUID, UploadInput) (Upload, error)
	ListUploads(context.Context, uuid.UUID, uuid.UUID, UploadListInput) ([]Upload, error)
	Chunk(context.Context, uuid.UUID, uuid.UUID, ChunkInput) (Upload, error)
	Finalize(context.Context, uuid.UUID, uuid.UUID, VersionInput) (FinalizeOutput, error)
	Abort(context.Context, uuid.UUID, uuid.UUID, VersionInput) (Upload, error)
	Get(context.Context, uuid.UUID, uuid.UUID, AttachmentInput) (Attachment, error)
	Bind(context.Context, uuid.UUID, uuid.UUID, BindInput) (Attachment, error)
	Remove(context.Context, uuid.UUID, uuid.UUID, RemoveInput) (Attachment, error)
	List(context.Context, uuid.UUID, ListInput) ([]Attachment, bool, error)
	Download(context.Context, uuid.UUID, uuid.UUID, DownloadInput) (DownloadOutput, error)
	UploadHistory(context.Context, uuid.UUID, uuid.UUID, UploadHistoryInput) ([]UploadHistory, error)
	History(context.Context, uuid.UUID, uuid.UUID, HistoryInput) ([]History, error)
}
