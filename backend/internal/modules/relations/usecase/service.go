package usecase

import (
	"context"
	"github.com/furkanmeclis/traccia/backend/internal/platform/operations"
	"github.com/google/uuid"
)

type Service struct {
	repo   Repository
	policy Policy
}

func New(r Repository, p Policy) *Service { return &Service{r, p} }
func (s *Service) capabilities(ctx context.Context, w uuid.UUID, state *State) error {
	manage, e := s.policy.CanManage(ctx, w)
	state.Capabilities = Capabilities{Link: manage && state.Issue.Active, Unlink: manage}
	return e
}
func (s *Service) mutate(ctx context.Context, sc operations.Scope, in Mutation) (MutationOutput, error) {
	out, e := s.repo.Mutate(ctx, sc.WorkspaceID(), sc.ActorID(), in)
	if e == nil {
		e = s.capabilities(ctx, sc.WorkspaceID(), &out.State)
	}
	return out, e
}
func setMutation(in SetInput, kind string) Mutation {
	var target uuid.UUID
	if in.TargetIssueID != nil {
		target, _ = uuid.Parse(*in.TargetIssueID)
	}
	return Mutation{IssueID: in.IssueID, TargetIssueID: target, Kind: kind, Set: true, ExpectedVersions: in.ExpectedVersions}
}
func linkMutation(in LinkInput, kind string, remove bool) Mutation {
	return Mutation{IssueID: in.IssueID, TargetIssueID: in.TargetIssueID, Kind: kind, Remove: remove, ExpectedVersions: in.ExpectedVersions}
}
func (s *Service) setParent(ctx context.Context, sc operations.Scope, in SetInput) (MutationOutput, error) {
	return s.mutate(ctx, sc, setMutation(in, "parent"))
}
func (s *Service) setDuplicate(ctx context.Context, sc operations.Scope, in SetInput) (MutationOutput, error) {
	return s.mutate(ctx, sc, setMutation(in, "duplicate"))
}
func (s *Service) linkBlock(ctx context.Context, sc operations.Scope, in LinkInput) (MutationOutput, error) {
	return s.mutate(ctx, sc, linkMutation(in, "blocks", false))
}
func (s *Service) unlinkBlock(ctx context.Context, sc operations.Scope, in LinkInput) (MutationOutput, error) {
	return s.mutate(ctx, sc, linkMutation(in, "blocks", true))
}
func (s *Service) linkRelated(ctx context.Context, sc operations.Scope, in LinkInput) (MutationOutput, error) {
	return s.mutate(ctx, sc, linkMutation(in, "related", false))
}
func (s *Service) unlinkRelated(ctx context.Context, sc operations.Scope, in LinkInput) (MutationOutput, error) {
	return s.mutate(ctx, sc, linkMutation(in, "related", true))
}
func (s *Service) list(ctx context.Context, sc operations.Scope, in PageInput) (List, error) {
	out, e := s.repo.List(ctx, sc.WorkspaceID(), in)
	if e != nil {
		return out, e
	}
	if len(out.Items) > int(in.Limit) {
		out.Items = out.Items[:in.Limit]
		out.HasMore = true
		out.NextCursor = nextCursor(sc.WorkspaceID(), in, out.Items[len(out.Items)-1].ID)
	}
	e = s.capabilities(ctx, sc.WorkspaceID(), &out.State)
	return out, e
}
func (s *Service) history(ctx context.Context, sc operations.Scope, in HistoryInput) (HistoryList, error) {
	rows, e := s.repo.History(ctx, sc.WorkspaceID(), in)
	out := HistoryList{Items: rows}
	if len(rows) > int(in.Limit) {
		out.Items = rows[:in.Limit]
		out.HasMore = true
		out.NextCursor = nextCursor(sc.WorkspaceID(), in, out.Items[len(out.Items)-1].ID)
	}
	return out, e
}
