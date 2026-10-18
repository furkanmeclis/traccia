package usecase

import (
	"context"
	"github.com/furkanmeclis/traccia/backend/internal/platform/operations"
	"github.com/google/uuid"
	"strings"
)

type Service struct {
	repo    Repository
	policy  Policy
	options Options
}

func New(repo Repository, policy Policy) *Service { return &Service{repo, policy, DefaultOptions()} }
func NewWithOptions(repo Repository, policy Policy, options Options) (*Service, error) {
	if e := options.Validate(); e != nil {
		return nil, e
	}
	return &Service{repo, policy, options}, nil
}
func (s *Service) manage(ctx context.Context, w uuid.UUID) error {
	yes, e := s.policy.CanManage(ctx, w)
	if e != nil {
		return e
	}
	if !yes {
		return &operations.Error{Code: operations.Forbidden}
	}
	return nil
}
func (s *Service) uploadCaps(ctx context.Context, w uuid.UUID, out *Upload) error {
	manage, e := s.policy.CanManage(ctx, w)
	if e != nil {
		return e
	}
	active := out.State == "uploading"
	out.Capabilities = UploadCapabilities{UploadChunk: manage && active && out.NextOffset < out.SizeBytes, Finalize: manage && active && out.NextOffset == out.SizeBytes, Abort: manage && (active || out.State == "finalized"), Bind: manage && out.State == "finalized"}
	return nil
}
func (s *Service) fileCaps(ctx context.Context, w, actor uuid.UUID, private bool, out *Attachment) error {
	manage, e := s.policy.CanManage(ctx, w)
	if e != nil {
		return e
	}
	out.Capabilities = AttachmentCapabilities{History: private, Download: !out.Removed && (out.State == "finalized" || out.State == "bound"), Bind: manage && out.ActorID == actor && out.State == "finalized" && !out.Removed, Remove: manage && out.State == "bound" && !out.Removed}
	if !private {
		out.UploadID = nil
		out.DraftID = nil
	}
	return nil
}
func (s *Service) config(ctx context.Context, scope operations.Scope, _ Empty) (LimitsOutput, error) {
	manage, e := s.policy.CanManage(ctx, scope.WorkspaceID())
	return LimitsOutput{Limits: s.options.Limits(), Capabilities: GeneralCapabilities{Manage: manage}}, e
}
func (s *Service) create(ctx context.Context, scope operations.Scope, in CreateInput) (UploadOutput, error) {
	if in.SizeBytes > s.options.MaxFileBytes {
		return UploadOutput{}, &operations.Error{Code: operations.Validation, Details: field("size_bytes", "bytes_limit")}
	}
	out, e := s.repo.Create(ctx, scope.WorkspaceID(), scope.ActorID(), in)
	if e == nil {
		e = s.manage(ctx, scope.WorkspaceID())
	}
	if e == nil {
		e = s.uploadCaps(ctx, scope.WorkspaceID(), &out)
	}
	return UploadOutput{out}, e
}
func (s *Service) getUpload(ctx context.Context, scope operations.Scope, in UploadInput) (UploadOutput, error) {
	out, e := s.repo.GetUpload(ctx, scope.WorkspaceID(), scope.ActorID(), in)
	if e == nil {
		e = s.policy.CanRead(ctx, scope.WorkspaceID())
	}
	if e == nil {
		e = s.uploadCaps(ctx, scope.WorkspaceID(), &out)
	}
	return UploadOutput{out}, e
}
func (s *Service) listUploads(ctx context.Context, scope operations.Scope, in UploadListInput) (UploadList, error) {
	rows, e := s.repo.ListUploads(ctx, scope.WorkspaceID(), scope.ActorID(), in)
	out := UploadList{Items: rows}
	if e != nil {
		return out, e
	}
	if len(rows) > int(in.Limit) {
		out.Items = rows[:in.Limit]
		out.HasMore = true
		out.NextCursor = nextCursor(scope.WorkspaceID(), in, out.Items[len(out.Items)-1].ID)
	}
	for i := range out.Items {
		if e = s.uploadCaps(ctx, scope.WorkspaceID(), &out.Items[i]); e != nil {
			return out, e
		}
	}
	return out, s.policy.CanRead(ctx, scope.WorkspaceID())
}
func (s *Service) chunk(ctx context.Context, scope operations.Scope, in ChunkInput) (UploadOutput, error) {
	out, e := s.repo.Chunk(ctx, scope.WorkspaceID(), scope.ActorID(), in)
	if e == nil {
		e = s.manage(ctx, scope.WorkspaceID())
	}
	if e == nil {
		e = s.uploadCaps(ctx, scope.WorkspaceID(), &out)
	}
	return UploadOutput{out}, e
}
func (s *Service) finalize(ctx context.Context, scope operations.Scope, in VersionInput) (FinalizeOutput, error) {
	out, e := s.repo.Finalize(ctx, scope.WorkspaceID(), scope.ActorID(), in)
	if e == nil {
		e = s.manage(ctx, scope.WorkspaceID())
	}
	if e == nil {
		e = s.uploadCaps(ctx, scope.WorkspaceID(), &out.Upload)
	}
	if e == nil {
		e = s.fileCaps(ctx, scope.WorkspaceID(), scope.ActorID(), true, &out.Attachment)
	}
	return out, e
}
func (s *Service) abort(ctx context.Context, scope operations.Scope, in VersionInput) (UploadOutput, error) {
	out, e := s.repo.Abort(ctx, scope.WorkspaceID(), scope.ActorID(), in)
	if e == nil {
		e = s.manage(ctx, scope.WorkspaceID())
	}
	if e == nil {
		e = s.uploadCaps(ctx, scope.WorkspaceID(), &out)
	}
	return UploadOutput{out}, e
}
func (s *Service) get(ctx context.Context, scope operations.Scope, in AttachmentInput) (AttachmentOutput, error) {
	out, e := s.repo.Get(ctx, scope.WorkspaceID(), scope.ActorID(), in)
	if e == nil {
		e = s.policy.CanRead(ctx, scope.WorkspaceID())
	}
	if e == nil {
		e = s.fileCaps(ctx, scope.WorkspaceID(), scope.ActorID(), out.ActorID == scope.ActorID() && out.DraftID != nil && in.DraftID != "" && strings.EqualFold(in.DraftID, out.DraftID.String()), &out)
	}
	return AttachmentOutput{out}, e
}
func (s *Service) bind(ctx context.Context, scope operations.Scope, in BindInput) (AttachmentOutput, error) {
	out, e := s.repo.Bind(ctx, scope.WorkspaceID(), scope.ActorID(), in)
	if e == nil {
		e = s.manage(ctx, scope.WorkspaceID())
	}
	if e == nil {
		e = s.fileCaps(ctx, scope.WorkspaceID(), scope.ActorID(), true, &out)
	}
	return AttachmentOutput{out}, e
}
func (s *Service) remove(ctx context.Context, scope operations.Scope, in RemoveInput) (AttachmentOutput, error) {
	out, e := s.repo.Remove(ctx, scope.WorkspaceID(), scope.ActorID(), in)
	if e == nil {
		e = s.manage(ctx, scope.WorkspaceID())
	}
	if e == nil {
		e = s.fileCaps(ctx, scope.WorkspaceID(), scope.ActorID(), false, &out)
	}
	return AttachmentOutput{out}, e
}
func (s *Service) list(ctx context.Context, scope operations.Scope, in ListInput) (AttachmentList, error) {
	rows, active, e := s.repo.List(ctx, scope.WorkspaceID(), in)
	out := AttachmentList{Items: rows}
	if e != nil {
		return out, e
	}
	if len(rows) > int(in.Limit) {
		out.Items = rows[:in.Limit]
		out.HasMore = true
		out.NextCursor = nextCursor(scope.WorkspaceID(), in, out.Items[len(out.Items)-1].ID)
	}
	manage, e := s.policy.CanManage(ctx, scope.WorkspaceID())
	if e != nil {
		return out, e
	}
	out.Capabilities = ListCapabilities{Bind: manage && active, Remove: manage}
	for i := range out.Items {
		if e = s.fileCaps(ctx, scope.WorkspaceID(), scope.ActorID(), false, &out.Items[i]); e != nil {
			return out, e
		}
	}
	return out, s.policy.CanRead(ctx, scope.WorkspaceID())
}
func (s *Service) download(ctx context.Context, scope operations.Scope, in DownloadInput) (DownloadOutput, error) {
	out, e := s.repo.Download(ctx, scope.WorkspaceID(), scope.ActorID(), in)
	if e == nil {
		e = s.policy.CanRead(ctx, scope.WorkspaceID())
	}
	return out, e
}
func (s *Service) uploadHistory(ctx context.Context, scope operations.Scope, in UploadHistoryInput) (UploadHistoryList, error) {
	rows, e := s.repo.UploadHistory(ctx, scope.WorkspaceID(), scope.ActorID(), in)
	out := UploadHistoryList{Items: rows}
	if e != nil {
		return out, e
	}
	if len(rows) > int(in.Limit) {
		out.Items = rows[:in.Limit]
		out.HasMore = true
		out.NextCursor = nextCursor(scope.WorkspaceID(), in, out.Items[len(out.Items)-1].ID)
	}
	return out, s.policy.CanRead(ctx, scope.WorkspaceID())
}
func (s *Service) history(ctx context.Context, scope operations.Scope, in HistoryInput) (HistoryList, error) {
	rows, e := s.repo.History(ctx, scope.WorkspaceID(), scope.ActorID(), in)
	out := HistoryList{Items: rows}
	if e != nil {
		return out, e
	}
	if len(rows) > int(in.Limit) {
		out.Items = rows[:in.Limit]
		out.HasMore = true
		out.NextCursor = nextCursor(scope.WorkspaceID(), in, out.Items[len(out.Items)-1].ID)
	}
	return out, s.policy.CanRead(ctx, scope.WorkspaceID())
}
