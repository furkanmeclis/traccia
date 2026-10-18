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

func New(r Repository, p Policy) *Service { return &Service{repo: r, policy: p} }
func (s *Service) output(ctx context.Context, w uuid.UUID, i Issue) (Output, error) {
	b, e := s.policy.CanManage(ctx, w)
	if e != nil {
		return Output{}, e
	}
	active, e := s.repo.ActiveTeam(ctx, w, i.TeamID)
	move := b && !i.Archived
	b = b && active
	return Output{Issue: i, Capabilities: Capabilities{Move: move, Manage: b && !i.Archived, Restore: b && i.Archived}}, e
}
func (s *Service) changed(ctx context.Context, sc operations.Scope, event string, i Issue, e error) (Output, error) {
	if e != nil {
		return Output{}, e
	}
	if e = s.repo.RecordEvent(ctx, sc.WorkspaceID(), sc.ActorID(), event, i); e != nil {
		return Output{}, e
	}
	return s.output(ctx, sc.WorkspaceID(), i)
}
func (s *Service) create(ctx context.Context, sc operations.Scope, in CreateInput) (Output, error) {
	i, e := s.repo.Create(ctx, sc.WorkspaceID(), in)
	return s.changed(ctx, sc, "issues.created", i, e)
}
func (s *Service) update(ctx context.Context, sc operations.Scope, in UpdateInput) (Output, error) {
	i, e := s.repo.Update(ctx, sc.WorkspaceID(), in)
	return s.changed(ctx, sc, "issues.updated", i, e)
}
func (s *Service) move(ctx context.Context, sc operations.Scope, in MoveInput) (Output, error) {
	i, e := s.repo.Move(ctx, sc.WorkspaceID(), in)
	return s.changed(ctx, sc, "issues.moved", i, e)
}
func (s *Service) archive(ctx context.Context, sc operations.Scope, in VersionInput) (Output, error) {
	i, e := s.repo.Archive(ctx, sc.WorkspaceID(), in, true)
	return s.changed(ctx, sc, "issues.archived", i, e)
}
func (s *Service) restore(ctx context.Context, sc operations.Scope, in VersionInput) (Output, error) {
	i, e := s.repo.Archive(ctx, sc.WorkspaceID(), in, false)
	return s.changed(ctx, sc, "issues.restored", i, e)
}
func (s *Service) get(ctx context.Context, sc operations.Scope, in IssueInput) (Output, error) {
	i, e := s.repo.Get(ctx, sc.WorkspaceID(), in.IssueID)
	if e != nil {
		return Output{}, e
	}
	return s.output(ctx, sc.WorkspaceID(), i)
}
func (s *Service) resolve(ctx context.Context, sc operations.Scope, in ResolveInput) (Output, error) {
	i, e := s.repo.Resolve(ctx, sc.WorkspaceID(), in.Identifier)
	if e != nil {
		return Output{}, e
	}
	return s.output(ctx, sc.WorkspaceID(), i)
}
func (s *Service) list(ctx context.Context, sc operations.Scope, in PageInput) (List, error) {
	rows, e := s.repo.List(ctx, sc.WorkspaceID(), in)
	if e != nil {
		return List{}, e
	}
	b, e := s.policy.CanManage(ctx, sc.WorkspaceID())
	out := List{Items: rows, Capabilities: Capabilities{Manage: b}}
	if len(rows) > int(in.Limit) {
		out.Items = rows[:in.Limit]
		out.HasMore = true
		out.NextCursor = nextCursor(sc.WorkspaceID(), in, out.Items[len(out.Items)-1].ID)
	}
	return out, e
}
func (s *Service) history(ctx context.Context, sc operations.Scope, in HistoryInput) (HistoryList, error) {
	rows, e := s.repo.History(ctx, sc.WorkspaceID(), in)
	if e != nil {
		return HistoryList{}, e
	}
	out := HistoryList{Items: rows}
	if len(rows) > int(in.Limit) {
		out.Items = rows[:in.Limit]
		out.HasMore = true
		out.NextCursor = nextCursor(sc.WorkspaceID(), in, out.Items[len(out.Items)-1].ID)
	}
	return out, nil
}
