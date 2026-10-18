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
func (s *Service) caps(ctx context.Context, w uuid.UUID, archived, parentArchived bool) (Capabilities, error) {
	b, e := s.policy.CanManage(ctx, w)
	return Capabilities{Manage: b && !archived && !parentArchived, Restore: b && archived && !parentArchived}, e
}
func (s *Service) projectOutput(ctx context.Context, w uuid.UUID, p Project) (ProjectOutput, error) {
	c, e := s.caps(ctx, w, p.Archived, false)
	return ProjectOutput{p, c}, e
}
func (s *Service) event(ctx context.Context, sc operations.Scope, event string, id, p uuid.UUID, name string, v int64) error {
	return s.repo.RecordEvent(ctx, sc.WorkspaceID(), sc.ActorID(), event, id, p, name, v)
}
func (s *Service) createProject(ctx context.Context, sc operations.Scope, in CreateProjectInput) (ProjectOutput, error) {
	p, e := s.repo.CreateProject(ctx, sc.WorkspaceID(), in)
	if e != nil {
		return ProjectOutput{}, e
	}
	if e = s.event(ctx, sc, "projects.created", p.ID, p.ID, p.Name, p.Version); e != nil {
		return ProjectOutput{}, e
	}
	return s.projectOutput(ctx, sc.WorkspaceID(), p)
}
func (s *Service) getProject(ctx context.Context, sc operations.Scope, in ProjectInput) (ProjectOutput, error) {
	p, e := s.repo.GetProject(ctx, sc.WorkspaceID(), in.ProjectID)
	if e != nil {
		return ProjectOutput{}, e
	}
	return s.projectOutput(ctx, sc.WorkspaceID(), p)
}
func (s *Service) updateProject(ctx context.Context, sc operations.Scope, in UpdateProjectInput) (ProjectOutput, error) {
	p, e := s.repo.UpdateProject(ctx, sc.WorkspaceID(), in)
	if e != nil {
		return ProjectOutput{}, e
	}
	if e = s.event(ctx, sc, "projects.updated", p.ID, p.ID, p.Name, p.Version); e != nil {
		return ProjectOutput{}, e
	}
	return s.projectOutput(ctx, sc.WorkspaceID(), p)
}
func (s *Service) archiveProject(ctx context.Context, sc operations.Scope, in ArchiveProjectInput) (ProjectOutput, error) {
	return s.projectArchive(ctx, sc, in, true)
}
func (s *Service) restoreProject(ctx context.Context, sc operations.Scope, in ArchiveProjectInput) (ProjectOutput, error) {
	return s.projectArchive(ctx, sc, in, false)
}
func (s *Service) projectArchive(ctx context.Context, sc operations.Scope, in ArchiveProjectInput, archived bool) (ProjectOutput, error) {
	p, e := s.repo.ArchiveProject(ctx, sc.WorkspaceID(), in, archived)
	if e != nil {
		return ProjectOutput{}, e
	}
	act := "restored"
	if archived {
		act = "archived"
	}
	if e = s.event(ctx, sc, "projects."+act, p.ID, p.ID, p.Name, p.Version); e != nil {
		return ProjectOutput{}, e
	}
	return s.projectOutput(ctx, sc.WorkspaceID(), p)
}
func (s *Service) listProjects(ctx context.Context, sc operations.Scope, in PageInput) (ProjectList, error) {
	rows, e := s.repo.ListProjects(ctx, sc.WorkspaceID(), in)
	if e != nil {
		return ProjectList{}, e
	}
	out := ProjectList{Items: rows}
	if len(rows) > int(in.Limit) {
		out.Items = rows[:in.Limit]
		out.HasMore = true
		out.NextCursor = out.Items[len(out.Items)-1].ID.String()
	}
	out.Capabilities, e = s.caps(ctx, sc.WorkspaceID(), false, false)
	return out, e
}
func (s *Service) listTeams(ctx context.Context, sc operations.Scope, in ProjectPageInput) (ProjectTeams, error) {
	p, rows, e := s.repo.ListTeams(ctx, sc.WorkspaceID(), in)
	if e != nil {
		return ProjectTeams{}, e
	}
	out := ProjectTeams{Items: rows, ProjectVersion: p.Version}
	if len(rows) > int(in.Limit) {
		out.Items = rows[:in.Limit]
		out.HasMore = true
		out.NextCursor = out.Items[len(out.Items)-1].ID.String()
	}
	out.Capabilities, e = s.caps(ctx, sc.WorkspaceID(), p.Archived, false)
	return out, e
}
func (s *Service) setTeams(ctx context.Context, sc operations.Scope, in SetTeamsInput) (ProjectVersionOutput, error) {
	v, e := s.repo.SetTeams(ctx, sc.WorkspaceID(), in)
	if e == nil {
		e = s.event(ctx, sc, "project_teams.updated", in.ProjectID, in.ProjectID, "Teams", v)
	}
	return ProjectVersionOutput{v}, e
}
func (s *Service) listMembers(ctx context.Context, sc operations.Scope, in MemberPageInput) (MemberList, error) {
	rows, e := s.repo.ListMembers(ctx, sc.WorkspaceID(), in)
	if e != nil {
		return MemberList{}, e
	}
	out := MemberList{Items: rows}
	if len(rows) > int(in.Limit) {
		out.Items = rows[:in.Limit]
		out.HasMore = true
		out.NextCursor = out.Items[len(out.Items)-1].ID.String()
	}
	return out, nil
}
func (s *Service) milestoneOutput(ctx context.Context, w uuid.UUID, row Milestone, p Project) (MilestoneOutput, error) {
	c, e := s.caps(ctx, w, row.Archived, p.Archived)
	return MilestoneOutput{row, p.Version, c}, e
}
func (s *Service) getMilestone(ctx context.Context, sc operations.Scope, in MilestoneInput) (MilestoneOutput, error) {
	row, p, e := s.repo.GetMilestone(ctx, sc.WorkspaceID(), in)
	if e != nil {
		return MilestoneOutput{}, e
	}
	return s.milestoneOutput(ctx, sc.WorkspaceID(), row, p)
}
func (s *Service) listMilestones(ctx context.Context, sc operations.Scope, in ProjectPageInput) (MilestoneList, error) {
	rows, p, e := s.repo.ListMilestones(ctx, sc.WorkspaceID(), in)
	if e != nil {
		return MilestoneList{}, e
	}
	out := MilestoneList{Items: rows, ProjectVersion: p.Version}
	if len(rows) > int(in.Limit) {
		out.Items = rows[:in.Limit]
		out.HasMore = true
		out.NextCursor = out.Items[len(out.Items)-1].ID.String()
	}
	out.Capabilities, e = s.caps(ctx, sc.WorkspaceID(), in.Archived, p.Archived)
	return out, e
}
func (s *Service) createMilestone(ctx context.Context, sc operations.Scope, in CreateMilestoneInput) (MilestoneOutput, error) {
	row, v, e := s.repo.CreateMilestone(ctx, sc.WorkspaceID(), in)
	if e != nil {
		return MilestoneOutput{}, e
	}
	if e = s.event(ctx, sc, "project_milestones.created", row.ID, row.ProjectID, row.Name, row.Version); e != nil {
		return MilestoneOutput{}, e
	}
	return s.milestoneOutput(ctx, sc.WorkspaceID(), row, Project{Version: v})
}
func (s *Service) updateMilestone(ctx context.Context, sc operations.Scope, in UpdateMilestoneInput) (MilestoneOutput, error) {
	row, v, e := s.repo.UpdateMilestone(ctx, sc.WorkspaceID(), in)
	if e != nil {
		return MilestoneOutput{}, e
	}
	if e = s.event(ctx, sc, "project_milestones.updated", row.ID, row.ProjectID, row.Name, row.Version); e != nil {
		return MilestoneOutput{}, e
	}
	return s.milestoneOutput(ctx, sc.WorkspaceID(), row, Project{Version: v})
}
func (s *Service) archiveMilestone(ctx context.Context, sc operations.Scope, in ArchiveMilestoneInput) (MilestoneOutput, error) {
	return s.milestoneArchive(ctx, sc, in, true)
}
func (s *Service) restoreMilestone(ctx context.Context, sc operations.Scope, in ArchiveMilestoneInput) (MilestoneOutput, error) {
	return s.milestoneArchive(ctx, sc, in, false)
}
func (s *Service) milestoneArchive(ctx context.Context, sc operations.Scope, in ArchiveMilestoneInput, archived bool) (MilestoneOutput, error) {
	row, v, e := s.repo.ArchiveMilestone(ctx, sc.WorkspaceID(), in, archived)
	if e != nil {
		return MilestoneOutput{}, e
	}
	act := "restored"
	if archived {
		act = "archived"
	}
	if e = s.event(ctx, sc, "project_milestones."+act, row.ID, row.ProjectID, row.Name, row.Version); e != nil {
		return MilestoneOutput{}, e
	}
	return s.milestoneOutput(ctx, sc.WorkspaceID(), row, Project{Version: v})
}
func (s *Service) resourceOutput(ctx context.Context, w uuid.UUID, row Resource, p Project) (ResourceOutput, error) {
	c, e := s.caps(ctx, w, row.Archived, p.Archived)
	return ResourceOutput{row, p.Version, c}, e
}
func (s *Service) getResource(ctx context.Context, sc operations.Scope, in ResourceInput) (ResourceOutput, error) {
	row, p, e := s.repo.GetResource(ctx, sc.WorkspaceID(), in)
	if e != nil {
		return ResourceOutput{}, e
	}
	return s.resourceOutput(ctx, sc.WorkspaceID(), row, p)
}
func (s *Service) listResources(ctx context.Context, sc operations.Scope, in ProjectPageInput) (ResourceList, error) {
	rows, p, e := s.repo.ListResources(ctx, sc.WorkspaceID(), in)
	if e != nil {
		return ResourceList{}, e
	}
	out := ResourceList{Items: rows, ProjectVersion: p.Version}
	if len(rows) > int(in.Limit) {
		out.Items = rows[:in.Limit]
		out.HasMore = true
		out.NextCursor = out.Items[len(out.Items)-1].ID.String()
	}
	out.Capabilities, e = s.caps(ctx, sc.WorkspaceID(), in.Archived, p.Archived)
	return out, e
}
func (s *Service) createResource(ctx context.Context, sc operations.Scope, in CreateResourceInput) (ResourceOutput, error) {
	row, v, e := s.repo.CreateResource(ctx, sc.WorkspaceID(), in)
	if e != nil {
		return ResourceOutput{}, e
	}
	if e = s.event(ctx, sc, "project_resources.created", row.ID, row.ProjectID, row.Title, row.Version); e != nil {
		return ResourceOutput{}, e
	}
	return s.resourceOutput(ctx, sc.WorkspaceID(), row, Project{Version: v})
}
func (s *Service) updateResource(ctx context.Context, sc operations.Scope, in UpdateResourceInput) (ResourceOutput, error) {
	row, v, e := s.repo.UpdateResource(ctx, sc.WorkspaceID(), in)
	if e != nil {
		return ResourceOutput{}, e
	}
	if e = s.event(ctx, sc, "project_resources.updated", row.ID, row.ProjectID, row.Title, row.Version); e != nil {
		return ResourceOutput{}, e
	}
	return s.resourceOutput(ctx, sc.WorkspaceID(), row, Project{Version: v})
}
func (s *Service) archiveResource(ctx context.Context, sc operations.Scope, in ArchiveResourceInput) (ResourceOutput, error) {
	return s.resourceArchive(ctx, sc, in, true)
}
func (s *Service) restoreResource(ctx context.Context, sc operations.Scope, in ArchiveResourceInput) (ResourceOutput, error) {
	return s.resourceArchive(ctx, sc, in, false)
}
func (s *Service) resourceArchive(ctx context.Context, sc operations.Scope, in ArchiveResourceInput, archived bool) (ResourceOutput, error) {
	row, v, e := s.repo.ArchiveResource(ctx, sc.WorkspaceID(), in, archived)
	if e != nil {
		return ResourceOutput{}, e
	}
	act := "restored"
	if archived {
		act = "archived"
	}
	if e = s.event(ctx, sc, "project_resources."+act, row.ID, row.ProjectID, row.Title, row.Version); e != nil {
		return ResourceOutput{}, e
	}
	return s.resourceOutput(ctx, sc.WorkspaceID(), row, Project{Version: v})
}
