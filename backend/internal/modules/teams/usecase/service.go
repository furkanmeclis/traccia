package usecase

import (
	"context"
	"strings"

	"github.com/furkanmeclis/traccia/backend/internal/platform/operations"
	"github.com/google/uuid"
)

type Service struct {
	repo   Repository
	policy Policy
}

func New(repo Repository, policy Policy) *Service { return &Service{repo: repo, policy: policy} }
func (s *Service) caps(ctx context.Context, w uuid.UUID) (Capabilities, error) {
	b, e := s.policy.CanManage(ctx, w)
	return Capabilities{b}, e
}
func (s *Service) event(ctx context.Context, sc operations.Scope, kind string, id, team uuid.UUID, name string, v int64) error {
	return s.repo.RecordEvent(ctx, sc.WorkspaceID(), sc.ActorID(), kind, id, team, name, v)
}
func (s *Service) createTeam(ctx context.Context, sc operations.Scope, in CreateTeamInput) (TeamOutput, error) {
	in.Code = strings.ToUpper(in.Code)
	t, e := s.repo.CreateTeam(ctx, sc.WorkspaceID(), in)
	if e == nil {
		e = s.event(ctx, sc, "teams.created", t.ID, t.ID, t.Name, t.Version)
	}
	return TeamOutput{Team: t, Capabilities: Capabilities{true}}, e
}
func (s *Service) getTeam(ctx context.Context, sc operations.Scope, in TeamInput) (TeamOutput, error) {
	t, e := s.repo.GetTeam(ctx, sc.WorkspaceID(), in.TeamID)
	if e != nil {
		return TeamOutput{}, e
	}
	c, e := s.caps(ctx, sc.WorkspaceID())
	c.Manage = c.Manage && !t.Archived
	return TeamOutput{t, c}, e
}
func (s *Service) listTeams(ctx context.Context, sc operations.Scope, in TeamListInput) (TeamList, error) {
	rows, e := s.repo.ListTeams(ctx, sc.WorkspaceID(), in)
	if e != nil {
		return TeamList{}, e
	}
	out := TeamList{Items: rows}
	if len(rows) > int(in.Limit) {
		out.Items = rows[:in.Limit]
		out.HasMore = true
		out.NextCursor = out.Items[len(out.Items)-1].ID.String()
	}
	out.Capabilities, e = s.caps(ctx, sc.WorkspaceID())
	return out, e
}
func (s *Service) updateTeam(ctx context.Context, sc operations.Scope, in UpdateTeamInput) (TeamOutput, error) {
	t, e := s.repo.UpdateTeam(ctx, sc.WorkspaceID(), in)
	if e == nil {
		e = s.event(ctx, sc, "teams.updated", t.ID, t.ID, t.Name, t.Version)
	}
	return TeamOutput{Team: t, Capabilities: Capabilities{true}}, e
}
func (s *Service) archiveTeam(ctx context.Context, sc operations.Scope, in ArchiveTeamInput) (TeamOutput, error) {
	t, e := s.repo.ArchiveTeam(ctx, sc.WorkspaceID(), in)
	if e == nil {
		e = s.event(ctx, sc, "teams.archived", t.ID, t.ID, t.Name, t.Version)
	}
	return TeamOutput{Team: t, Capabilities: Capabilities{false}}, e
}
func (s *Service) listStatuses(ctx context.Context, sc operations.Scope, in StatusListInput) (StatusList, error) {
	rows, v, e := s.repo.ListStatuses(ctx, sc.WorkspaceID(), in)
	if e != nil {
		return StatusList{}, e
	}
	out := StatusList{Items: rows, WorkflowVersion: v}
	if len(rows) > int(in.Limit) {
		out.Items = rows[:in.Limit]
		out.HasMore = true
		out.NextCursor = out.Items[len(out.Items)-1].ID.String()
	}
	out.Capabilities, e = s.caps(ctx, sc.WorkspaceID())
	if e == nil {
		t, err := s.repo.GetTeam(ctx, sc.WorkspaceID(), in.TeamID)
		if err != nil {
			return StatusList{}, err
		}
		out.Capabilities.Manage = out.Capabilities.Manage && !t.Archived
	}
	return out, e
}
func (s *Service) createStatus(ctx context.Context, sc operations.Scope, in CreateStatusInput) (StatusOutput, error) {
	t, v, e := s.repo.CreateStatus(ctx, sc.WorkspaceID(), in)
	if e == nil {
		e = s.event(ctx, sc, "workflow_statuses.created", t.ID, t.TeamID, t.Name, t.Version)
	}
	return StatusOutput{t, v}, e
}
func (s *Service) updateStatus(ctx context.Context, sc operations.Scope, in UpdateStatusInput) (StatusOutput, error) {
	t, v, e := s.repo.UpdateStatus(ctx, sc.WorkspaceID(), in)
	if e == nil {
		e = s.event(ctx, sc, "workflow_statuses.updated", t.ID, t.TeamID, t.Name, t.Version)
	}
	return StatusOutput{t, v}, e
}
func (s *Service) archiveStatus(ctx context.Context, sc operations.Scope, in ArchiveStatusInput) (StatusOutput, error) {
	t, v, e := s.repo.ArchiveStatus(ctx, sc.WorkspaceID(), in)
	if e == nil {
		e = s.event(ctx, sc, "workflow_statuses.archived", t.ID, t.TeamID, t.Name, t.Version)
	}
	return StatusOutput{t, v}, e
}
func (s *Service) reorder(ctx context.Context, sc operations.Scope, in ReorderInput) (ReorderOutput, error) {
	v, e := s.repo.Reorder(ctx, sc.WorkspaceID(), in)
	if e == nil {
		e = s.event(ctx, sc, "workflow_statuses.reordered", in.TeamID, in.TeamID, "Workflow", v)
	}
	return ReorderOutput{v}, e
}
func (s *Service) createLabel(ctx context.Context, sc operations.Scope, in CreateLabelInput) (LabelOutput, error) {
	t, e := s.repo.CreateLabel(ctx, sc.WorkspaceID(), in)
	if e == nil {
		team, _ := uuid.Parse(t.TeamID)
		e = s.event(ctx, sc, "labels.created", t.ID, team, t.Name, t.Version)
	}
	return LabelOutput{t}, e
}
func (s *Service) getLabel(ctx context.Context, sc operations.Scope, in LabelInput) (LabelOutput, error) {
	t, e := s.repo.GetLabel(ctx, sc.WorkspaceID(), in.LabelID)
	return LabelOutput{t}, e
}
func (s *Service) listLabels(ctx context.Context, sc operations.Scope, in LabelListInput) (LabelList, error) {
	rows, e := s.repo.ListLabels(ctx, sc.WorkspaceID(), in)
	if e != nil {
		return LabelList{}, e
	}
	out := LabelList{Items: rows}
	if len(rows) > int(in.Limit) {
		out.Items = rows[:in.Limit]
		out.HasMore = true
		out.NextCursor = out.Items[len(out.Items)-1].ID.String()
	}
	out.Capabilities, e = s.caps(ctx, sc.WorkspaceID())
	if e == nil && in.TeamID != "" {
		id, _ := uuid.Parse(in.TeamID)
		t, err := s.repo.GetTeam(ctx, sc.WorkspaceID(), id)
		if err != nil {
			return LabelList{}, err
		}
		out.Capabilities.Manage = out.Capabilities.Manage && !t.Archived
	}
	return out, e
}
func (s *Service) updateLabel(ctx context.Context, sc operations.Scope, in UpdateLabelInput) (LabelOutput, error) {
	t, e := s.repo.UpdateLabel(ctx, sc.WorkspaceID(), in)
	if e == nil {
		team, _ := uuid.Parse(t.TeamID)
		e = s.event(ctx, sc, "labels.updated", t.ID, team, t.Name, t.Version)
	}
	return LabelOutput{t}, e
}
func (s *Service) archiveLabel(ctx context.Context, sc operations.Scope, in ArchiveLabelInput) (LabelOutput, error) {
	t, e := s.repo.ArchiveLabel(ctx, sc.WorkspaceID(), in)
	if e == nil {
		team, _ := uuid.Parse(t.TeamID)
		e = s.event(ctx, sc, "labels.archived", t.ID, team, t.Name, t.Version)
	}
	return LabelOutput{t}, e
}
