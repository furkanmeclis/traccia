package repository

import (
	"context"
	"errors"
	"sort"
	"time"

	"github.com/furkanmeclis/traccia/backend/internal/database/db"
	uc "github.com/furkanmeclis/traccia/backend/internal/modules/projects/usecase"
	teams "github.com/furkanmeclis/traccia/backend/internal/modules/teams/usecase"
	"github.com/furkanmeclis/traccia/backend/internal/platform/dbtx"
	"github.com/furkanmeclis/traccia/backend/internal/platform/operations"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Postgres struct {
	pool *pgxpool.Pool
	q    *db.Queries
}

func New(p *pgxpool.Pool, q *db.Queries) *Postgres { return &Postgres{p, q} }
func (r *Postgres) queries(ctx context.Context) *db.Queries {
	if tx, ok := dbtx.Tx(ctx); ok {
		return r.q.WithTx(tx)
	}
	return r.q
}
func mutation(ctx context.Context) error {
	if _, ok := dbtx.Tx(ctx); !ok {
		return errors.New("project mutation requires operation transaction")
	}
	return nil
}
func mapped(e error, missing operations.Code) error {
	if errors.Is(e, pgx.ErrNoRows) {
		return &operations.Error{Code: missing}
	}
	var p *pgconn.PgError
	if errors.As(e, &p) {
		switch p.Code {
		case "23505":
			return &operations.Error{Code: operations.Conflict}
		case "23503", "23514":
			return &operations.Error{Code: operations.Validation}
		}
	}
	return e
}
func cursor(s string) uuid.UUID { id, _ := uuid.Parse(s); return id }
func date(s *string) pgtype.Date {
	if s == nil {
		return pgtype.Date{}
	}
	d, _ := time.Parse("2006-01-02", *s)
	return pgtype.Date{Time: d, Valid: true}
}
func dateString(d pgtype.Date) *string {
	if !d.Valid {
		return nil
	}
	s := d.Time.Format("2006-01-02")
	return &s
}
func project(w uuid.UUID, row db.GetProjectRow) uc.Project {
	var lead *string
	if row.LeadUserUuid.Valid {
		v := uuid.UUID(row.LeadUserUuid.Bytes).String()
		lead = &v
	}
	return uc.Project{ID: row.Uuid, WorkspaceID: w, Name: row.Name, Summary: row.Summary, LeadUserID: lead, StartDate: dateString(row.StartDate), TargetDate: dateString(row.TargetDate), Version: row.Version, Archived: row.Archived, CreatedAt: row.CreatedAt.Time, UpdatedAt: row.UpdatedAt.Time}
}
func team(w uuid.UUID, t db.Team) teams.Team {
	return teams.Team{ID: t.Uuid, WorkspaceID: w, Name: t.Name, Code: t.Code, Version: t.Version, WorkflowVersion: t.WorkflowVersion, Archived: t.Archived, CreatedAt: t.CreatedAt.Time, UpdatedAt: t.UpdatedAt.Time}
}

// snapshot pins the aggregate version and every returned row to one database snapshot.
func snapshot[T any](ctx context.Context, r *Postgres, read func(context.Context) (T, error)) (T, error) {
	if _, ok := dbtx.Tx(ctx); ok {
		return read(ctx)
	}
	var zero T
	tx, e := r.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if e != nil {
		return zero, e
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	out, e := read(dbtx.WithTx(ctx, tx))
	if e != nil {
		return zero, e
	}
	if e = tx.Commit(ctx); e != nil {
		return zero, e
	}
	return out, nil
}
func (r *Postgres) GetProject(ctx context.Context, w, p uuid.UUID) (uc.Project, error) {
	row, e := r.queries(ctx).GetProject(ctx, db.GetProjectParams{Uuid: w, Uuid_2: p})
	return project(w, row), mapped(e, operations.NotFound)
}
func (r *Postgres) locked(ctx context.Context, w, p uuid.UUID, active bool) (db.Project, error) {
	if e := mutation(ctx); e != nil {
		return db.Project{}, e
	}
	row, e := r.queries(ctx).LockProject(ctx, db.LockProjectParams{Uuid: w, Uuid_2: p})
	if e != nil {
		return row, mapped(e, operations.NotFound)
	}
	if active && row.Archived {
		return row, &operations.Error{Code: operations.Conflict}
	}
	return row, nil
}
func (r *Postgres) lead(ctx context.Context, w int64, s *string) (pgtype.Int8, error) {
	if s == nil {
		return pgtype.Int8{}, nil
	}
	n, e := r.queries(ctx).LockProjectLead(ctx, db.LockProjectLeadParams{WorkspaceID: w, Uuid: cursor(*s)})
	return pgtype.Int8{Int64: n, Valid: e == nil}, mapped(e, operations.NotFound)
}

// Project precedes team locks everywhere; teams are locked in UUID order. Existing
// inactive references can be retained, while new selections require active rows.
func (r *Postgres) replaceTeams(ctx context.Context, w uuid.UUID, p db.Project, ids []string) error {
	q := r.queries(ctx)
	rows, e := q.AllProjectContributions(ctx, db.AllProjectContributionsParams{WorkspaceID: p.WorkspaceID, ProjectID: p.ID})
	if e != nil {
		return e
	}
	existing := map[uuid.UUID]bool{}
	for _, row := range rows {
		existing[row.Uuid] = !row.ContributionArchived
	}
	ordered := make([]string, len(ids))
	for i, id := range ids {
		ordered[i] = cursor(id).String()
	}
	sort.Strings(ordered)
	internal := make([]int64, 0, len(ordered))
	for _, id := range ordered {
		row, e := q.LockTeam(ctx, db.LockTeamParams{Uuid: w, Uuid_2: cursor(id)})
		if e != nil {
			return mapped(e, operations.NotFound)
		}
		if row.Archived && !existing[row.Uuid] {
			return &operations.Error{Code: operations.Conflict}
		}
		internal = append(internal, row.ID)
	}
	if e = q.DeactivateProjectTeams(ctx, db.DeactivateProjectTeamsParams{WorkspaceID: p.WorkspaceID, ProjectID: p.ID}); e != nil {
		return e
	}
	for _, id := range internal {
		if e = q.UpsertProjectTeam(ctx, db.UpsertProjectTeamParams{WorkspaceID: p.WorkspaceID, ProjectID: p.ID, TeamID: id}); e != nil {
			return e
		}
	}
	return nil
}
func (r *Postgres) CreateProject(ctx context.Context, w uuid.UUID, in uc.CreateProjectInput) (uc.Project, error) {
	if e := mutation(ctx); e != nil {
		return uc.Project{}, e
	}
	q := r.queries(ctx)
	wid, e := q.ProjectInternalWorkspace(ctx, w)
	if e != nil {
		return uc.Project{}, mapped(e, operations.NotFound)
	}
	lead, e := r.lead(ctx, wid, in.LeadUserID)
	if e != nil {
		return uc.Project{}, e
	}
	p, e := q.CreateProject(ctx, db.CreateProjectParams{WorkspaceID: wid, Name: in.Name, Summary: in.Summary, LeadUserID: lead, StartDate: date(in.StartDate), TargetDate: date(in.TargetDate)})
	if e != nil {
		return uc.Project{}, mapped(e, operations.NotFound)
	}
	if e = r.replaceTeams(ctx, w, p, in.TeamIDs); e != nil {
		return uc.Project{}, e
	}
	return r.GetProject(ctx, w, p.Uuid)
}
func (r *Postgres) UpdateProject(ctx context.Context, w uuid.UUID, in uc.UpdateProjectInput) (uc.Project, error) {
	p, e := r.locked(ctx, w, in.ProjectID, true)
	if e != nil {
		return uc.Project{}, e
	}
	old, e := r.GetProject(ctx, w, in.ProjectID)
	if e != nil {
		return uc.Project{}, e
	}
	lead := p.LeadUserID
	same := old.LeadUserID == nil && in.LeadUserID == nil
	if old.LeadUserID != nil && in.LeadUserID != nil {
		same = cursor(*old.LeadUserID) == cursor(*in.LeadUserID)
	}
	if !same {
		lead, e = r.lead(ctx, p.WorkspaceID, in.LeadUserID)
		if e != nil {
			return uc.Project{}, e
		}
	}
	_, e = r.queries(ctx).UpdateProject(ctx, db.UpdateProjectParams{ID: p.ID, Name: in.Name, Summary: in.Summary, LeadUserID: lead, StartDate: date(in.StartDate), TargetDate: date(in.TargetDate), Version: in.ExpectedVersion})
	if e != nil {
		return uc.Project{}, mapped(e, operations.VersionConflict)
	}
	return r.GetProject(ctx, w, in.ProjectID)
}
func (r *Postgres) ArchiveProject(ctx context.Context, w uuid.UUID, in uc.ArchiveProjectInput, archived bool) (uc.Project, error) {
	p, e := r.locked(ctx, w, in.ProjectID, false)
	if e != nil {
		return uc.Project{}, e
	}
	_, e = r.queries(ctx).SetProjectArchived(ctx, db.SetProjectArchivedParams{ID: p.ID, Archived: archived, Version: in.ExpectedVersion})
	if e != nil {
		return uc.Project{}, mapped(e, operations.VersionConflict)
	}
	return r.GetProject(ctx, w, in.ProjectID)
}
func (r *Postgres) ListProjects(ctx context.Context, w uuid.UUID, in uc.PageInput) ([]uc.Project, error) {
	rows, e := r.queries(ctx).ListProjects(ctx, db.ListProjectsParams{Uuid: w, Archived: in.Archived, AfterUuid: cursor(in.After), RowLimit: in.Limit + 1})
	out := make([]uc.Project, 0, len(rows))
	for _, row := range rows {
		out = append(out, project(w, db.GetProjectRow(row)))
	}
	return out, e
}
func (r *Postgres) SetTeams(ctx context.Context, w uuid.UUID, in uc.SetTeamsInput) (int64, error) {
	p, e := r.locked(ctx, w, in.ProjectID, true)
	if e != nil {
		return 0, e
	}
	v, e := r.queries(ctx).BumpProject(ctx, db.BumpProjectParams{ID: p.ID, Version: in.ExpectedProjectVersion})
	if e != nil {
		return 0, mapped(e, operations.VersionConflict)
	}
	e = r.replaceTeams(ctx, w, p, in.TeamIDs)
	return v, e
}
func (r *Postgres) ListTeams(ctx context.Context, w uuid.UUID, in uc.ProjectPageInput) (uc.Project, []teams.Team, error) {
	type result struct {
		p    uc.Project
		rows []teams.Team
	}
	out, e := snapshot(ctx, r, func(ctx context.Context) (result, error) {
		p, e := r.GetProject(ctx, w, in.ProjectID)
		if e != nil {
			return result{}, e
		}
		internal, e := r.queries(ctx).GetProject(ctx, db.GetProjectParams{Uuid: w, Uuid_2: in.ProjectID})
		if e != nil {
			return result{}, e
		}
		rows, e := r.queries(ctx).ListProjectTeams(ctx, db.ListProjectTeamsParams{WorkspaceID: internal.WorkspaceID, ProjectID: internal.ID, Archived: in.Archived, AfterUuid: cursor(in.After), RowLimit: in.Limit + 1})
		list := make([]teams.Team, 0, len(rows))
		for _, row := range rows {
			list = append(list, team(w, row))
		}
		return result{p, list}, e
	})
	return out.p, out.rows, e
}
func (r *Postgres) ListMembers(ctx context.Context, w uuid.UUID, in uc.MemberPageInput) ([]uc.Member, error) {
	rows, e := r.queries(ctx).ListProjectMembers(ctx, db.ListProjectMembersParams{Uuid: w, AfterUuid: cursor(in.After), RowLimit: in.Limit + 1})
	out := make([]uc.Member, 0, len(rows))
	for _, row := range rows {
		out = append(out, uc.Member{ID: row.Uuid, Name: row.Name, Surname: row.Surname, Role: row.Role, Active: row.Active, Assignable: row.Assignable})
	}
	return out, e
}
func milestone(w, p uuid.UUID, row db.ProjectMilestone) uc.Milestone {
	return uc.Milestone{ID: row.Uuid, WorkspaceID: w, ProjectID: p, Name: row.Name, Description: row.Description, TargetDate: dateString(row.TargetDate), Version: row.Version, Archived: row.Archived, CreatedAt: row.CreatedAt.Time, UpdatedAt: row.UpdatedAt.Time}
}
func (r *Postgres) GetMilestone(ctx context.Context, w uuid.UUID, in uc.MilestoneInput) (uc.Milestone, uc.Project, error) {
	type result struct {
		row uc.Milestone
		p   uc.Project
	}
	out, e := snapshot(ctx, r, func(ctx context.Context) (result, error) {
		p, e := r.queries(ctx).GetProject(ctx, db.GetProjectParams{Uuid: w, Uuid_2: in.ProjectID})
		if e != nil {
			return result{}, mapped(e, operations.NotFound)
		}
		row, e := r.queries(ctx).GetProjectMilestone(ctx, db.GetProjectMilestoneParams{WorkspaceID: p.WorkspaceID, ProjectID: p.ID, Uuid: in.MilestoneID})
		return result{milestone(w, in.ProjectID, row), project(w, p)}, mapped(e, operations.NotFound)
	})
	return out.row, out.p, e
}
func (r *Postgres) ListMilestones(ctx context.Context, w uuid.UUID, in uc.ProjectPageInput) ([]uc.Milestone, uc.Project, error) {
	type result struct {
		rows []uc.Milestone
		p    uc.Project
	}
	out, e := snapshot(ctx, r, func(ctx context.Context) (result, error) {
		p, e := r.queries(ctx).GetProject(ctx, db.GetProjectParams{Uuid: w, Uuid_2: in.ProjectID})
		if e != nil {
			return result{}, mapped(e, operations.NotFound)
		}
		rows, e := r.queries(ctx).ListProjectMilestones(ctx, db.ListProjectMilestonesParams{WorkspaceID: p.WorkspaceID, ProjectID: p.ID, Archived: in.Archived, AfterUuid: cursor(in.After), RowLimit: in.Limit + 1})
		list := make([]uc.Milestone, 0, len(rows))
		for _, row := range rows {
			list = append(list, milestone(w, in.ProjectID, row))
		}
		return result{list, project(w, p)}, e
	})
	return out.rows, out.p, e
}
func (r *Postgres) CreateMilestone(ctx context.Context, w uuid.UUID, in uc.CreateMilestoneInput) (uc.Milestone, int64, error) {
	p, e := r.locked(ctx, w, in.ProjectID, true)
	if e != nil {
		return uc.Milestone{}, 0, e
	}
	v, e := r.queries(ctx).BumpProject(ctx, db.BumpProjectParams{ID: p.ID, Version: in.ExpectedProjectVersion})
	if e != nil {
		return uc.Milestone{}, 0, mapped(e, operations.VersionConflict)
	}
	row, e := r.queries(ctx).CreateProjectMilestone(ctx, db.CreateProjectMilestoneParams{WorkspaceID: p.WorkspaceID, ProjectID: p.ID, Name: in.Name, Description: in.Description, TargetDate: date(in.TargetDate)})
	return milestone(w, in.ProjectID, row), v, mapped(e, operations.NotFound)
}
func (r *Postgres) UpdateMilestone(ctx context.Context, w uuid.UUID, in uc.UpdateMilestoneInput) (uc.Milestone, int64, error) {
	p, e := r.locked(ctx, w, in.ProjectID, true)
	if e != nil {
		return uc.Milestone{}, 0, e
	}
	v, e := r.queries(ctx).BumpProject(ctx, db.BumpProjectParams{ID: p.ID, Version: in.ExpectedProjectVersion})
	if e != nil {
		return uc.Milestone{}, 0, mapped(e, operations.VersionConflict)
	}
	old, e := r.queries(ctx).GetProjectMilestone(ctx, db.GetProjectMilestoneParams{WorkspaceID: p.WorkspaceID, ProjectID: p.ID, Uuid: in.MilestoneID})
	if e != nil {
		return uc.Milestone{}, 0, mapped(e, operations.NotFound)
	}
	row, e := r.queries(ctx).UpdateProjectMilestone(ctx, db.UpdateProjectMilestoneParams{ID: old.ID, Name: in.Name, Description: in.Description, TargetDate: date(in.TargetDate), Version: in.ExpectedVersion})
	return milestone(w, in.ProjectID, row), v, mapped(e, operations.VersionConflict)
}
func (r *Postgres) ArchiveMilestone(ctx context.Context, w uuid.UUID, in uc.ArchiveMilestoneInput, archived bool) (uc.Milestone, int64, error) {
	p, e := r.locked(ctx, w, in.ProjectID, true)
	if e != nil {
		return uc.Milestone{}, 0, e
	}
	v, e := r.queries(ctx).BumpProject(ctx, db.BumpProjectParams{ID: p.ID, Version: in.ExpectedProjectVersion})
	if e != nil {
		return uc.Milestone{}, 0, mapped(e, operations.VersionConflict)
	}
	old, e := r.queries(ctx).GetProjectMilestone(ctx, db.GetProjectMilestoneParams{WorkspaceID: p.WorkspaceID, ProjectID: p.ID, Uuid: in.MilestoneID})
	if e != nil {
		return uc.Milestone{}, 0, mapped(e, operations.NotFound)
	}
	row, e := r.queries(ctx).SetProjectMilestoneArchived(ctx, db.SetProjectMilestoneArchivedParams{ID: old.ID, Archived: archived, Version: in.ExpectedVersion})
	return milestone(w, in.ProjectID, row), v, mapped(e, operations.VersionConflict)
}
func resource(w, p uuid.UUID, row db.ProjectResource) uc.Resource {
	return uc.Resource{ID: row.Uuid, WorkspaceID: w, ProjectID: p, Kind: row.Kind, Title: row.Title, URL: row.Url, Summary: row.Summary, Provider: row.Provider, Reference: row.Reference, Version: row.Version, Archived: row.Archived, CreatedAt: row.CreatedAt.Time, UpdatedAt: row.UpdatedAt.Time}
}
func (r *Postgres) GetResource(ctx context.Context, w uuid.UUID, in uc.ResourceInput) (uc.Resource, uc.Project, error) {
	type result struct {
		row uc.Resource
		p   uc.Project
	}
	out, e := snapshot(ctx, r, func(ctx context.Context) (result, error) {
		p, e := r.queries(ctx).GetProject(ctx, db.GetProjectParams{Uuid: w, Uuid_2: in.ProjectID})
		if e != nil {
			return result{}, mapped(e, operations.NotFound)
		}
		row, e := r.queries(ctx).GetProjectResource(ctx, db.GetProjectResourceParams{WorkspaceID: p.WorkspaceID, ProjectID: p.ID, Uuid: in.ResourceID})
		return result{resource(w, in.ProjectID, row), project(w, p)}, mapped(e, operations.NotFound)
	})
	return out.row, out.p, e
}
func (r *Postgres) ListResources(ctx context.Context, w uuid.UUID, in uc.ProjectPageInput) ([]uc.Resource, uc.Project, error) {
	type result struct {
		rows []uc.Resource
		p    uc.Project
	}
	out, e := snapshot(ctx, r, func(ctx context.Context) (result, error) {
		p, e := r.queries(ctx).GetProject(ctx, db.GetProjectParams{Uuid: w, Uuid_2: in.ProjectID})
		if e != nil {
			return result{}, mapped(e, operations.NotFound)
		}
		rows, e := r.queries(ctx).ListProjectResources(ctx, db.ListProjectResourcesParams{WorkspaceID: p.WorkspaceID, ProjectID: p.ID, Archived: in.Archived, AfterUuid: cursor(in.After), RowLimit: in.Limit + 1})
		list := make([]uc.Resource, 0, len(rows))
		for _, row := range rows {
			list = append(list, resource(w, in.ProjectID, row))
		}
		return result{list, project(w, p)}, e
	})
	return out.rows, out.p, e
}
func (r *Postgres) CreateResource(ctx context.Context, w uuid.UUID, in uc.CreateResourceInput) (uc.Resource, int64, error) {
	p, e := r.locked(ctx, w, in.ProjectID, true)
	if e != nil {
		return uc.Resource{}, 0, e
	}
	v, e := r.queries(ctx).BumpProject(ctx, db.BumpProjectParams{ID: p.ID, Version: in.ExpectedProjectVersion})
	if e != nil {
		return uc.Resource{}, 0, mapped(e, operations.VersionConflict)
	}
	row, e := r.queries(ctx).CreateProjectResource(ctx, db.CreateProjectResourceParams{WorkspaceID: p.WorkspaceID, ProjectID: p.ID, Kind: in.Kind, Title: in.Title, Url: in.URL, Summary: in.Summary, Provider: in.Provider, Reference: in.Reference})
	return resource(w, in.ProjectID, row), v, mapped(e, operations.NotFound)
}
func (r *Postgres) UpdateResource(ctx context.Context, w uuid.UUID, in uc.UpdateResourceInput) (uc.Resource, int64, error) {
	p, e := r.locked(ctx, w, in.ProjectID, true)
	if e != nil {
		return uc.Resource{}, 0, e
	}
	v, e := r.queries(ctx).BumpProject(ctx, db.BumpProjectParams{ID: p.ID, Version: in.ExpectedProjectVersion})
	if e != nil {
		return uc.Resource{}, 0, mapped(e, operations.VersionConflict)
	}
	old, e := r.queries(ctx).GetProjectResource(ctx, db.GetProjectResourceParams{WorkspaceID: p.WorkspaceID, ProjectID: p.ID, Uuid: in.ResourceID})
	if e != nil {
		return uc.Resource{}, 0, mapped(e, operations.NotFound)
	}
	row, e := r.queries(ctx).UpdateProjectResource(ctx, db.UpdateProjectResourceParams{ID: old.ID, Kind: in.Kind, Title: in.Title, Url: in.URL, Summary: in.Summary, Provider: in.Provider, Reference: in.Reference, Version: in.ExpectedVersion})
	return resource(w, in.ProjectID, row), v, mapped(e, operations.VersionConflict)
}
func (r *Postgres) ArchiveResource(ctx context.Context, w uuid.UUID, in uc.ArchiveResourceInput, archived bool) (uc.Resource, int64, error) {
	p, e := r.locked(ctx, w, in.ProjectID, true)
	if e != nil {
		return uc.Resource{}, 0, e
	}
	v, e := r.queries(ctx).BumpProject(ctx, db.BumpProjectParams{ID: p.ID, Version: in.ExpectedProjectVersion})
	if e != nil {
		return uc.Resource{}, 0, mapped(e, operations.VersionConflict)
	}
	old, e := r.queries(ctx).GetProjectResource(ctx, db.GetProjectResourceParams{WorkspaceID: p.WorkspaceID, ProjectID: p.ID, Uuid: in.ResourceID})
	if e != nil {
		return uc.Resource{}, 0, mapped(e, operations.NotFound)
	}
	row, e := r.queries(ctx).SetProjectResourceArchived(ctx, db.SetProjectResourceArchivedParams{ID: old.ID, Archived: archived, Version: in.ExpectedVersion})
	return resource(w, in.ProjectID, row), v, mapped(e, operations.VersionConflict)
}

// ValidateAssignment runs before the T06 team/status/label validator, within the
// future issue insertion transaction. Historical links remain; only active links
// are assignable. Project then team lock order is shared with contributor writes.
func (r *Postgres) ValidateAssignment(ctx context.Context, w, t, p, m uuid.UUID) error {
	if e := mutation(ctx); e != nil {
		return e
	}
	if p == uuid.Nil {
		if m != uuid.Nil {
			return &operations.Error{Code: operations.Validation}
		}
		return nil
	}
	pr, e := r.locked(ctx, w, p, true)
	if e != nil {
		return e
	}
	q := r.queries(ctx)
	tm, e := q.LockTeam(ctx, db.LockTeamParams{Uuid: w, Uuid_2: t})
	if e != nil {
		return mapped(e, operations.NotFound)
	}
	if tm.Archived {
		return &operations.Error{Code: operations.Conflict}
	}
	if _, e = q.ActiveProjectContribution(ctx, db.ActiveProjectContributionParams{WorkspaceID: pr.WorkspaceID, ProjectID: pr.ID, TeamID: tm.ID}); e != nil {
		return mapped(e, operations.Validation)
	}
	if m != uuid.Nil {
		row, e := q.GetProjectMilestone(ctx, db.GetProjectMilestoneParams{WorkspaceID: pr.WorkspaceID, ProjectID: pr.ID, Uuid: m})
		if e != nil {
			return mapped(e, operations.NotFound)
		}
		if row.Archived {
			return &operations.Error{Code: operations.Conflict}
		}
	}
	return nil
}

var _ uc.Repository = (*Postgres)(nil)
