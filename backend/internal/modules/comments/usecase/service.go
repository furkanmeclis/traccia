package usecase

import (
	"context"
	plate "github.com/furkanmeclis/traccia/backend/internal/modules/documents/usecase"
	"github.com/furkanmeclis/traccia/backend/internal/platform/operations"
	"github.com/google/uuid"
)

type Service struct {
	repo   Repository
	policy Policy
}

func New(repo Repository, policy Policy) *Service { return &Service{repo, policy} }
func (s *Service) caps(ctx context.Context, w uuid.UUID, c *Comment) error {
	manage, e := s.policy.CanManage(ctx, w)
	if e != nil {
		return e
	}
	active, e := s.repo.Active(ctx, w, c.IssueID)
	if e != nil {
		return e
	}
	c.Capabilities = Capabilities{Manage: manage, Replace: manage && active && !c.Deleted, Patch: manage && active && !c.Deleted, Delete: manage && !c.Deleted, Restore: manage && active}
	return nil
}
func (s *Service) get(ctx context.Context, scope operations.Scope, in CommentInput) (Output, error) {
	c, _, e := s.repo.Get(ctx, scope.WorkspaceID(), in.CommentID, uuid.Nil)
	if e == nil {
		e = s.caps(ctx, scope.WorkspaceID(), &c)
	}
	return Output{c}, e
}
func (s *Service) write(ctx context.Context, scope operations.Scope, in Mutation) (Output, error) {
	manage, e := s.policy.CanManage(ctx, scope.WorkspaceID())
	if e != nil {
		return Output{}, e
	}
	if !manage {
		return Output{}, &operations.Error{Code: operations.Forbidden}
	}
	in.Channel = string(scope.Channel())
	if in.Channel != "api" && in.Channel != "mcp" && in.Channel != "panel" {
		in.Channel = "unknown"
	}
	c, e := s.repo.Mutate(ctx, scope.WorkspaceID(), scope.ActorID(), in)
	if e == nil {
		e = s.caps(ctx, scope.WorkspaceID(), &c)
	}
	if e == nil && !c.Capabilities.Manage {
		return Output{}, &operations.Error{Code: operations.Forbidden}
	}
	return Output{c}, e
}
func (s *Service) create(ctx context.Context, scope operations.Scope, in CreateInput) (Output, error) {
	return s.write(ctx, scope, Mutation{IssueID: in.IssueID, Kind: "create", Content: in.Content})
}
func (s *Service) replace(ctx context.Context, scope operations.Scope, in ReplaceInput) (Output, error) {
	return s.write(ctx, scope, Mutation{CommentID: in.CommentID, ExpectedRevision: in.ExpectedRevision, Kind: "replace", Content: in.Content})
}
func (s *Service) patch(ctx context.Context, scope operations.Scope, in PatchInput) (Output, error) {
	return s.write(ctx, scope, Mutation{CommentID: in.CommentID, ExpectedRevision: in.ExpectedRevision, Kind: "patch", Patches: in.Patches})
}
func (s *Service) remove(ctx context.Context, scope operations.Scope, in DeleteInput) (Output, error) {
	return s.write(ctx, scope, Mutation{CommentID: in.CommentID, ExpectedRevision: in.ExpectedRevision, Kind: "delete"})
}
func (s *Service) restore(ctx context.Context, scope operations.Scope, in RestoreInput) (Output, error) {
	return s.write(ctx, scope, Mutation{CommentID: in.CommentID, ExpectedRevision: in.ExpectedRevision, SourceRevision: in.RevisionID, Kind: "restore"})
}
func (s *Service) list(ctx context.Context, scope operations.Scope, in ListInput) (List, error) {
	rows, e := s.repo.List(ctx, scope.WorkspaceID(), in)
	out := List{Items: rows}
	if e != nil {
		return out, e
	}
	manage, e := s.policy.CanManage(ctx, scope.WorkspaceID())
	if e != nil {
		return out, e
	}
	active, e := s.repo.Active(ctx, scope.WorkspaceID(), in.IssueID)
	if e != nil {
		return out, e
	}
	out.Capabilities = ListCapabilities{Manage: manage, Create: manage && active}
	if len(rows) > int(in.Limit) {
		out.Items = rows[:in.Limit]
		out.HasMore = true
		out.NextCursor = nextCursor(scope.WorkspaceID(), in, out.Items[len(out.Items)-1].ID)
	}
	for i := range out.Items {
		if e = s.caps(ctx, scope.WorkspaceID(), &out.Items[i]); e != nil {
			return out, e
		}
	}
	return out, nil
}
func (s *Service) revisions(ctx context.Context, scope operations.Scope, in RevisionsInput) (RevisionList, error) {
	rows, e := s.repo.Revisions(ctx, scope.WorkspaceID(), in)
	out := RevisionList{Items: rows}
	if e != nil {
		return out, e
	}
	c, _, e := s.repo.Get(ctx, scope.WorkspaceID(), in.CommentID, uuid.Nil)
	if e != nil {
		return out, e
	}
	if e = s.caps(ctx, scope.WorkspaceID(), &c); e != nil {
		return out, e
	}
	if len(rows) > int(in.Limit) {
		out.Items = rows[:in.Limit]
		out.HasMore = true
		out.NextCursor = nextCursor(scope.WorkspaceID(), in, out.Items[len(out.Items)-1].ID)
	}
	for i := range out.Items {
		out.Items[i].Capabilities.Restore = c.Capabilities.Restore && !out.Items[i].Deleted
	}
	return out, nil
}
func (s *Service) getRevision(ctx context.Context, scope operations.Scope, in RevisionInput) (RevisionOutput, error) {
	c, r, e := s.repo.Get(ctx, scope.WorkspaceID(), in.CommentID, in.RevisionID)
	if e == nil {
		e = s.caps(ctx, scope.WorkspaceID(), &c)
		r.Capabilities.Restore = c.Capabilities.Restore && !r.Deleted
	}
	return RevisionOutput{c, r}, e
}
func (s *Service) export(ctx context.Context, scope operations.Scope, in ExportInput) (ExportOutput, error) {
	c, _, e := s.repo.Get(ctx, scope.WorkspaceID(), in.CommentID, uuid.Nil)
	if e != nil {
		return ExportOutput{}, e
	}
	v, e := plate.RenderExport(plate.Document{ID: c.ID, Revision: c.Revision, Content: c.Content, Format: plate.Format, SchemaVersion: plate.SchemaVersion}, in.Format)
	return ExportOutput{CommentID: c.ID, Revision: v.Revision, Format: v.Format, Content: v.Content, Losses: v.Losses, RendererVersion: v.RendererVersion}, e
}
func (s *Service) backlinks(ctx context.Context, scope operations.Scope, in BacklinksInput) (BacklinkList, error) {
	rows, e := s.repo.Backlinks(ctx, scope.WorkspaceID(), in)
	out := BacklinkList{Items: rows}
	if len(rows) > int(in.Limit) {
		out.Items = rows[:in.Limit]
		out.HasMore = true
		out.NextCursor = nextCursor(scope.WorkspaceID(), in, out.Items[len(out.Items)-1].ID)
	}
	return out, e
}
func (s *Service) timeline(ctx context.Context, scope operations.Scope, in TimelineInput) (TimelineList, error) {
	rows, e := s.repo.Timeline(ctx, scope.WorkspaceID(), in)
	out := TimelineList{Items: rows}
	if len(rows) > int(in.Limit) {
		out.Items = rows[:in.Limit]
		out.HasMore = true
		out.NextCursor = nextCursor(scope.WorkspaceID(), in, out.Items[len(out.Items)-1].ID)
	}
	return out, e
}
