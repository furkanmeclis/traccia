package repository

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/furkanmeclis/traccia/backend/internal/database/db"
	uc "github.com/furkanmeclis/traccia/backend/internal/modules/issues/usecase"
	"github.com/furkanmeclis/traccia/backend/internal/platform/dbtx"
	"github.com/furkanmeclis/traccia/backend/internal/platform/operations"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"sort"
	"strconv"
	"strings"
	"time"
)

type Postgres struct {
	pool *pgxpool.Pool
	q    *db.Queries
}

func New(p *pgxpool.Pool, q *db.Queries) *Postgres { return &Postgres{pool: p, q: q} }
func (r *Postgres) queries(ctx context.Context) *db.Queries {
	if tx, ok := dbtx.Tx(ctx); ok {
		return r.q.WithTx(tx)
	}
	return r.q
}
func mutation(ctx context.Context) error {
	if _, ok := dbtx.Tx(ctx); !ok {
		return errors.New("issue mutation requires operation transaction")
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
func conflict() error { return &operations.Error{Code: operations.Conflict} }
func decode(raw []byte, e error) (uc.Issue, error) {
	if e != nil {
		return uc.Issue{}, mapped(e, operations.NotFound)
	}
	var i uc.Issue
	e = json.Unmarshal(raw, &i)
	return i, e
}
func (r *Postgres) Get(ctx context.Context, w, id uuid.UUID) (uc.Issue, error) {
	return decode(r.queries(ctx).ReadIssue(ctx, db.ReadIssueParams{WorkspaceUuid: w, Uuid: id}))
}
func (r *Postgres) Resolve(ctx context.Context, w uuid.UUID, id string) (uc.Issue, error) {
	cut := strings.LastIndexByte(id, '-')
	if cut < 0 {
		return uc.Issue{}, &operations.Error{Code: operations.Validation}
	}
	number, e := strconv.ParseInt(id[cut+1:], 10, 64)
	if e != nil {
		return uc.Issue{}, &operations.Error{Code: operations.Validation}
	}
	return decode(r.queries(ctx).ResolveIssue(ctx, db.ResolveIssueParams{WorkspaceUuid: w, TeamCode: id[:cut], Number: number}))
}
func ptrID(s *string) uuid.UUID {
	if s == nil {
		return uuid.Nil
	}
	id, _ := uuid.Parse(*s)
	return id
}
func same(a, b *string) bool { return ptrID(a) == ptrID(b) }
func date(v *string) pgtype.Date {
	if v == nil {
		return pgtype.Date{}
	}
	t, _ := time.Parse("2006-01-02", *v)
	return pgtype.Date{Time: t, Valid: true}
}
func effort(v *int32) pgtype.Int4 {
	if v == nil {
		return pgtype.Int4{}
	}
	return pgtype.Int4{Int32: *v, Valid: true}
}
func internal(id int64) pgtype.Int8 { return pgtype.Int8{Int64: id, Valid: id != 0} }
func sortedIDs(ids ...uuid.UUID) []uuid.UUID {
	seen := map[uuid.UUID]bool{}
	out := []uuid.UUID{}
	for _, id := range ids {
		if id != uuid.Nil && !seen[id] {
			out = append(out, id)
			seen[id] = true
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].String() < out[j].String() })
	return out
}

// Discover unlocked, then acquire Projects → Teams → Issues. The final CAS
// rejects any intervening edit before validation or mutation uses discovered refs.
func (r *Postgres) parents(ctx context.Context, w uuid.UUID, old uc.Issue, team uuid.UUID, project *string) (db.Team, map[uuid.UUID]db.Project, error) {
	q := r.queries(ctx)
	projects := map[uuid.UUID]db.Project{}
	for _, id := range sortedIDs(ptrID(old.ProjectID), ptrID(project)) {
		p, e := q.LockProject(ctx, db.LockProjectParams{Uuid: w, Uuid_2: id})
		if e != nil {
			return db.Team{}, nil, mapped(e, operations.NotFound)
		}
		projects[id] = p
	}
	var target db.Team
	for _, id := range sortedIDs(old.TeamID, team) {
		t, e := q.LockTeam(ctx, db.LockTeamParams{Uuid: w, Uuid_2: id})
		if e != nil {
			return target, nil, mapped(e, operations.NotFound)
		}
		if id == team {
			target = t
		}
	}
	return target, projects, nil
}
func (r *Postgres) locked(ctx context.Context, w uuid.UUID, id uuid.UUID, expected int64, discovered int64) (db.Issue, error) {
	row, e := r.queries(ctx).LockIssue(ctx, db.LockIssueParams{Uuid: w, Uuid_2: id})
	if e != nil {
		return row, mapped(e, operations.NotFound)
	}
	if row.Version != expected || row.Version != discovered {
		return row, &operations.Error{Code: operations.VersionConflict}
	}
	return row, nil
}

type refs struct {
	status                       int64
	assignee, project, milestone pgtype.Int8
	labels                       []int64
}

func (r *Postgres) references(ctx context.Context, w uuid.UUID, team db.Team, projects map[uuid.UUID]db.Project, in uc.CreateInput, old uc.Issue, moving bool) (refs, error) {
	q := r.queries(ctx)
	out := refs{labels: []int64{}}
	if team.Archived {
		return out, conflict()
	}
	s, e := q.GetWorkflowStatus(ctx, db.GetWorkflowStatusParams{TeamID: team.ID, Uuid: in.StatusID})
	if e != nil {
		return out, mapped(e, operations.Validation)
	}
	if s.Archived && (in.StatusID != old.StatusID || moving) {
		return out, conflict()
	}
	out.status = s.ID
	if in.ProjectID != nil {
		p := projects[ptrID(in.ProjectID)]
		if p.ID == 0 {
			return out, &operations.Error{Code: operations.Validation}
		}
		retained := same(in.ProjectID, old.ProjectID) && team.Uuid == old.TeamID
		if p.Archived && !retained {
			return out, conflict()
		}
		if !retained {
			if _, e = q.ActiveProjectContribution(ctx, db.ActiveProjectContributionParams{WorkspaceID: team.WorkspaceID, ProjectID: p.ID, TeamID: team.ID}); e != nil {
				return out, mapped(e, operations.Validation)
			}
		}
		out.project = internal(p.ID)
		if in.MilestoneID != nil {
			m, e := q.GetProjectMilestone(ctx, db.GetProjectMilestoneParams{WorkspaceID: team.WorkspaceID, ProjectID: p.ID, Uuid: ptrID(in.MilestoneID)})
			if e != nil {
				return out, mapped(e, operations.Validation)
			}
			if m.Archived && (!retained || !same(in.MilestoneID, old.MilestoneID)) {
				return out, conflict()
			}
			out.milestone = internal(m.ID)
		}
	} else if in.MilestoneID != nil {
		return out, &operations.Error{Code: operations.Validation}
	}
	if in.AssigneeID != nil {
		if same(in.AssigneeID, old.AssigneeID) {
			id, err := q.HistoricalIssueAssignee(ctx, db.HistoricalIssueAssigneeParams{Uuid: w, Uuid_2: ptrID(in.AssigneeID)})
			e = err
			out.assignee = internal(id)
		} else {
			var id int64
			id, e = q.LockProjectLead(ctx, db.LockProjectLeadParams{WorkspaceID: team.WorkspaceID, Uuid: ptrID(in.AssigneeID)})
			out.assignee = internal(id)
		}
		if e != nil {
			return out, mapped(e, operations.Validation)
		}
	}
	existing := map[uuid.UUID]bool{}
	for _, id := range old.LabelIDs {
		u, _ := uuid.Parse(id)
		existing[u] = true
	}
	labels := []uuid.UUID{}
	for _, id := range in.LabelIDs {
		u, _ := uuid.Parse(id)
		labels = append(labels, u)
	}
	for _, id := range sortedIDs(labels...) {
		l, e := q.LockLabel(ctx, db.LockLabelParams{Uuid: w, Uuid_2: id})
		if e != nil {
			return out, mapped(e, operations.Validation)
		}
		if l.TeamID.Valid && l.TeamID.Int64 != team.ID {
			return out, &operations.Error{Code: operations.Validation}
		}
		if l.Archived && !existing[id] {
			return out, conflict()
		}
		out.labels = append(out.labels, l.ID)
	}
	return out, nil
}
func (r *Postgres) labels(ctx context.Context, row db.Issue, ids []int64) error {
	q := r.queries(ctx)
	if e := q.ClearIssueLabels(ctx, db.ClearIssueLabelsParams{WorkspaceID: row.WorkspaceID, IssueID: row.ID}); e != nil {
		return e
	}
	for _, id := range ids {
		if e := q.InsertIssueLabel(ctx, db.InsertIssueLabelParams{WorkspaceID: row.WorkspaceID, IssueID: row.ID, LabelID: id}); e != nil {
			return e
		}
	}
	return nil
}
func (r *Postgres) alias(ctx context.Context, row db.Issue) error {
	return r.queries(ctx).InsertIssueAlias(ctx, db.InsertIssueAliasParams{WorkspaceID: row.WorkspaceID, TeamID: row.TeamID, Number: row.Number, IssueID: row.ID})
}
func (r *Postgres) Create(ctx context.Context, w uuid.UUID, in uc.CreateInput) (uc.Issue, error) {
	if e := mutation(ctx); e != nil {
		return uc.Issue{}, e
	}
	team, projects, e := r.parents(ctx, w, uc.Issue{}, in.TeamID, in.ProjectID)
	if e != nil {
		return uc.Issue{}, e
	}
	refs, e := r.references(ctx, w, team, projects, in, uc.Issue{}, false)
	if e != nil {
		return uc.Issue{}, e
	}
	number, e := r.queries(ctx).AllocateTeamIssueNumber(ctx, db.AllocateTeamIssueNumberParams{Uuid: w, Uuid_2: in.TeamID})
	if e != nil {
		return uc.Issue{}, mapped(e, operations.Conflict)
	}
	row, e := r.queries(ctx).CreateIssue(ctx, db.CreateIssueParams{WorkspaceID: team.WorkspaceID, TeamID: team.ID, Number: number, StatusID: refs.status, Title: in.Title, Priority: in.Priority, AssigneeID: refs.assignee, ProjectID: refs.project, MilestoneID: refs.milestone, StartDate: date(in.StartDate), TargetDate: date(in.TargetDate), Effort: effort(in.Effort)})
	if e != nil {
		return uc.Issue{}, mapped(e, operations.Validation)
	}
	if e = r.alias(ctx, row); e != nil {
		return uc.Issue{}, e
	}
	if e = r.labels(ctx, row, refs.labels); e != nil {
		return uc.Issue{}, e
	}
	return r.Get(ctx, w, row.Uuid)
}
func (r *Postgres) Update(ctx context.Context, w uuid.UUID, in uc.UpdateInput) (uc.Issue, error) {
	if e := mutation(ctx); e != nil {
		return uc.Issue{}, e
	}
	old, e := r.Get(ctx, w, in.IssueID)
	if e != nil {
		return uc.Issue{}, e
	}
	team, projects, e := r.parents(ctx, w, old, old.TeamID, in.ProjectID)
	if e != nil {
		return uc.Issue{}, e
	}
	row, e := r.locked(ctx, w, in.IssueID, in.ExpectedVersion, old.Version)
	if e != nil {
		return uc.Issue{}, e
	}
	refs, e := r.references(ctx, w, team, projects, uc.CreateInput{TeamID: old.TeamID, Title: in.Title, Priority: in.Priority, StatusID: in.StatusID, AssigneeID: in.AssigneeID, ProjectID: in.ProjectID, MilestoneID: in.MilestoneID, LabelIDs: in.LabelIDs}, old, false)
	if e != nil {
		return uc.Issue{}, e
	}
	row, e = r.queries(ctx).UpdateIssue(ctx, db.UpdateIssueParams{ID: row.ID, Title: in.Title, Priority: in.Priority, StatusID: refs.status, AssigneeID: refs.assignee, ProjectID: refs.project, MilestoneID: refs.milestone, StartDate: date(in.StartDate), TargetDate: date(in.TargetDate), Effort: effort(in.Effort), Version: in.ExpectedVersion})
	if e != nil {
		return uc.Issue{}, mapped(e, operations.VersionConflict)
	}
	if e = r.labels(ctx, row, refs.labels); e != nil {
		return uc.Issue{}, e
	}
	return r.Get(ctx, w, row.Uuid)
}
func (r *Postgres) Move(ctx context.Context, w uuid.UUID, in uc.MoveInput) (uc.Issue, error) {
	if e := mutation(ctx); e != nil {
		return uc.Issue{}, e
	}
	old, e := r.Get(ctx, w, in.IssueID)
	if e != nil {
		return uc.Issue{}, e
	}
	team, projects, e := r.parents(ctx, w, old, in.TeamID, in.ProjectID)
	if e != nil {
		return uc.Issue{}, e
	}
	row, e := r.locked(ctx, w, in.IssueID, in.ExpectedVersion, old.Version)
	if e != nil {
		return uc.Issue{}, e
	}
	if old.TeamID == in.TeamID {
		return uc.Issue{}, conflict()
	}
	refs, e := r.references(ctx, w, team, projects, uc.CreateInput{TeamID: in.TeamID, StatusID: in.StatusID, AssigneeID: old.AssigneeID, ProjectID: in.ProjectID, MilestoneID: in.MilestoneID, LabelIDs: in.LabelIDs}, old, true)
	if e != nil {
		return uc.Issue{}, e
	}
	number, e := r.queries(ctx).AllocateTeamIssueNumber(ctx, db.AllocateTeamIssueNumberParams{Uuid: w, Uuid_2: in.TeamID})
	if e != nil {
		return uc.Issue{}, mapped(e, operations.Conflict)
	}
	row, e = r.queries(ctx).MoveIssue(ctx, db.MoveIssueParams{ID: row.ID, TeamID: team.ID, Number: number, StatusID: refs.status, ProjectID: refs.project, MilestoneID: refs.milestone, Version: in.ExpectedVersion})
	if e != nil {
		return uc.Issue{}, mapped(e, operations.VersionConflict)
	}
	if e = r.alias(ctx, row); e != nil {
		return uc.Issue{}, e
	}
	if e = r.labels(ctx, row, refs.labels); e != nil {
		return uc.Issue{}, e
	}
	return r.Get(ctx, w, row.Uuid)
}
func (r *Postgres) Archive(ctx context.Context, w uuid.UUID, in uc.VersionInput, archived bool) (uc.Issue, error) {
	if e := mutation(ctx); e != nil {
		return uc.Issue{}, e
	}
	old, e := r.Get(ctx, w, in.IssueID)
	if e != nil {
		return uc.Issue{}, e
	}
	team, _, e := r.parents(ctx, w, old, old.TeamID, old.ProjectID)
	if e != nil {
		return uc.Issue{}, e
	}
	row, e := r.locked(ctx, w, in.IssueID, in.ExpectedVersion, old.Version)
	if e != nil {
		return uc.Issue{}, e
	}
	if team.Archived {
		return uc.Issue{}, conflict()
	}
	row, e = r.queries(ctx).SetIssueArchived(ctx, db.SetIssueArchivedParams{ID: row.ID, Archived: archived, Version: in.ExpectedVersion})
	if e != nil {
		return uc.Issue{}, mapped(e, operations.VersionConflict)
	}
	return r.Get(ctx, w, row.Uuid)
}
func (r *Postgres) List(ctx context.Context, w uuid.UUID, in uc.PageInput) ([]uc.Issue, error) {
	after, e := uc.CursorAnchor(w, in, in.After)
	if e != nil {
		return nil, e
	}
	if after != uuid.Nil {
		if _, e = r.Get(ctx, w, after); e != nil {
			return nil, &operations.Error{Code: operations.Validation}
		}
	}
	canonical := func(s string) string {
		if id, e := uuid.Parse(s); e == nil {
			return id.String()
		}
		return s
	}
	rows, e := r.queries(ctx).ListIssues(ctx, db.ListIssuesParams{WorkspaceUuid: w, Archived: in.Archived, AfterUuid: after, TeamFilter: canonical(in.TeamID), StatusFilter: canonical(in.StatusID), PriorityFilter: in.Priority, AssigneeFilter: canonical(in.AssigneeID), ProjectFilter: canonical(in.ProjectID), MilestoneFilter: canonical(in.MilestoneID), LabelFilter: canonical(in.LabelID), Search: in.Q, RowLimit: in.Limit + 1})
	if e != nil {
		return nil, e
	}
	out := make([]uc.Issue, 0, len(rows))
	for _, raw := range rows {
		i, e := decode(raw, nil)
		if e != nil {
			return nil, e
		}
		out = append(out, i)
	}
	return out, nil
}
func (r *Postgres) History(ctx context.Context, w uuid.UUID, in uc.HistoryInput) ([]uc.History, error) {
	if _, e := r.Get(ctx, w, in.IssueID); e != nil {
		return nil, e
	}
	after, e := uc.CursorAnchor(w, in, in.After)
	if e != nil {
		return nil, e
	}
	if after != uuid.Nil {
		found, err := r.queries(ctx).IssueHistoryAnchorExists(ctx, db.IssueHistoryAnchorExistsParams{Uuid: w, Uuid_2: in.IssueID, Uuid_3: after})
		if err != nil {
			return nil, err
		}
		if !found {
			return nil, &operations.Error{Code: operations.Validation}
		}
	}
	rows, e := r.queries(ctx).ListIssueHistory(ctx, db.ListIssueHistoryParams{Uuid: w, Uuid_2: in.IssueID, AfterUuid: after, RowLimit: in.Limit + 1})
	if e != nil {
		return nil, e
	}
	out := make([]uc.History, 0, len(rows))
	for _, row := range rows {
		i, e := decode(row.Snapshot, nil)
		if e != nil {
			return nil, e
		}
		out = append(out, uc.History{ID: row.Uuid, EventName: row.EventName, ActorID: row.ActorUuid, WorkspaceID: w, IssueID: in.IssueID, Version: row.Version, OccurredAt: row.OccurredAt.Time, Issue: i})
	}
	return out, nil
}

func (r *Postgres) ActiveTeam(ctx context.Context, w, t uuid.UUID) (bool, error) {
	row, e := r.queries(ctx).GetTeam(ctx, db.GetTeamParams{Uuid: w, Uuid_2: t})
	return !row.Archived, mapped(e, operations.NotFound)
}
