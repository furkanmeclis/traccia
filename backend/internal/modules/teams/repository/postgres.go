package repository

import (
	"context"
	"errors"
	"strings"

	"github.com/furkanmeclis/traccia/backend/internal/database/db"
	"github.com/furkanmeclis/traccia/backend/internal/modules/teams/usecase"
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
		return errors.New("team mutation requires operation transaction")
	}
	return nil
}
func mapped(e error, missing operations.Code) error {
	if errors.Is(e, pgx.ErrNoRows) {
		return &operations.Error{Code: missing}
	}
	var p *pgconn.PgError
	if errors.As(e, &p) && p.Code == "23505" {
		return &operations.Error{Code: operations.Conflict}
	}
	return e
}
func cursor(s string) uuid.UUID { id, _ := uuid.Parse(s); return id }
func team(w uuid.UUID, t db.Team) usecase.Team {
	return usecase.Team{ID: t.Uuid, WorkspaceID: w, Name: t.Name, Code: t.Code, Version: t.Version, WorkflowVersion: t.WorkflowVersion, Archived: t.Archived, CreatedAt: t.CreatedAt.Time, UpdatedAt: t.UpdatedAt.Time}
}
func status(w, t uuid.UUID, s db.WorkflowStatus) usecase.Status {
	return usecase.Status{ID: s.Uuid, WorkspaceID: w, TeamID: t, Name: s.Name, Category: s.Category, Color: s.Color, Position: s.Position, Version: s.Version, Archived: s.Archived}
}
func label(w, t uuid.UUID, s db.Label) usecase.Label {
	tid := ""
	if t != uuid.Nil {
		tid = t.String()
	}
	return usecase.Label{ID: s.Uuid, WorkspaceID: w, TeamID: tid, Name: s.Name, Color: s.Color, Version: s.Version, Archived: s.Archived}
}
func (r *Postgres) locked(ctx context.Context, w, t uuid.UUID) (db.Team, error) {
	if e := mutation(ctx); e != nil {
		return db.Team{}, e
	}
	row, e := r.queries(ctx).LockTeam(ctx, db.LockTeamParams{Uuid: w, Uuid_2: t})
	if e == nil && row.Archived {
		return row, &operations.Error{Code: operations.Conflict}
	}
	return row, mapped(e, operations.NotFound)
}
func (r *Postgres) CreateTeam(ctx context.Context, w uuid.UUID, in usecase.CreateTeamInput) (usecase.Team, error) {
	if e := mutation(ctx); e != nil {
		return usecase.Team{}, e
	}
	q := r.queries(ctx)
	t, e := q.CreateTeam(ctx, db.CreateTeamParams{Uuid: w, Name: in.Name, Code: strings.ToUpper(in.Code)})
	if e != nil {
		return usecase.Team{}, mapped(e, operations.NotFound)
	}
	for _, d := range []struct{ name, category, color string }{{"Bekleyen", "backlog", "#64748B"}, {"Başlanmadı", "unstarted", "#94A3B8"}, {"Devam ediyor", "started", "#EAB308"}, {"Tamamlandı", "completed", "#22C55E"}, {"İptal edildi", "canceled", "#EF4444"}} {
		if _, e = q.CreateWorkflowStatus(ctx, db.CreateWorkflowStatusParams{WorkspaceID: t.WorkspaceID, TeamID: t.ID, Name: d.name, Category: d.category, Color: d.color}); e != nil {
			return usecase.Team{}, e
		}
	}
	return team(w, t), nil
}
func (r *Postgres) GetTeam(ctx context.Context, w, t uuid.UUID) (usecase.Team, error) {
	row, e := r.queries(ctx).GetTeam(ctx, db.GetTeamParams{Uuid: w, Uuid_2: t})
	return team(w, row), mapped(e, operations.NotFound)
}
func (r *Postgres) ListTeams(ctx context.Context, w uuid.UUID, in usecase.TeamListInput) ([]usecase.Team, error) {
	rows, e := r.queries(ctx).ListTeams(ctx, db.ListTeamsParams{Uuid: w, Archived: in.Archived, Column3: cursor(in.After), Limit: in.Limit + 1})
	out := make([]usecase.Team, 0, len(rows))
	for _, row := range rows {
		out = append(out, team(w, row))
	}
	return out, e
}
func (r *Postgres) UpdateTeam(ctx context.Context, w uuid.UUID, in usecase.UpdateTeamInput) (usecase.Team, error) {
	t, e := r.locked(ctx, w, in.TeamID)
	if e != nil {
		return usecase.Team{}, e
	}
	row, e := r.queries(ctx).UpdateTeam(ctx, db.UpdateTeamParams{ID: t.ID, Name: in.Name, Version: in.ExpectedVersion})
	return team(w, row), mapped(e, operations.VersionConflict)
}
func (r *Postgres) ArchiveTeam(ctx context.Context, w uuid.UUID, in usecase.ArchiveTeamInput) (usecase.Team, error) {
	t, e := r.locked(ctx, w, in.TeamID)
	if e != nil {
		return usecase.Team{}, e
	}
	row, e := r.queries(ctx).ArchiveTeam(ctx, db.ArchiveTeamParams{ID: t.ID, Version: in.ExpectedVersion})
	return team(w, row), mapped(e, operations.VersionConflict)
}
func (r *Postgres) workflow(ctx context.Context, w, t uuid.UUID, expected int64) (db.Team, int64, error) {
	row, e := r.locked(ctx, w, t)
	if e != nil {
		return row, 0, e
	}
	v, e := r.queries(ctx).BumpTeamWorkflow(ctx, db.BumpTeamWorkflowParams{ID: row.ID, WorkflowVersion: expected})
	return row, v, mapped(e, operations.VersionConflict)
}
func (r *Postgres) ListStatuses(ctx context.Context, w uuid.UUID, in usecase.StatusListInput) ([]usecase.Status, int64, error) {
	// The aggregate version and its page share a PostgreSQL snapshot.
	if _, ambient := dbtx.Tx(ctx); !ambient {
		tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
		if err != nil {
			return nil, 0, err
		}
		defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
		rows, v, err := r.ListStatuses(dbtx.WithTx(ctx, tx), w, in)
		if err != nil {
			return nil, 0, err
		}
		if err = tx.Commit(ctx); err != nil {
			return nil, 0, err
		}
		return rows, v, nil
	}

	q := r.queries(ctx)
	t, e := q.GetTeam(ctx, db.GetTeamParams{Uuid: w, Uuid_2: in.TeamID})
	if e != nil {
		return nil, 0, mapped(e, operations.NotFound)
	}
	rows, e := q.ListWorkflowStatuses(ctx, db.ListWorkflowStatusesParams{TeamID: t.ID, Archived: in.Archived, AfterUuid: cursor(in.After), RowLimit: in.Limit + 1})
	out := make([]usecase.Status, 0, len(rows))
	for _, row := range rows {
		out = append(out, status(w, t.Uuid, row))
	}
	return out, t.WorkflowVersion, e
}
func (r *Postgres) CreateStatus(ctx context.Context, w uuid.UUID, in usecase.CreateStatusInput) (usecase.Status, int64, error) {
	t, v, e := r.workflow(ctx, w, in.TeamID, in.ExpectedWorkflowVersion)
	if e != nil {
		return usecase.Status{}, 0, e
	}
	rows, e := r.queries(ctx).AllWorkflowStatuses(ctx, t.ID)
	if e != nil {
		return usecase.Status{}, 0, e
	}
	if len(rows) >= usecase.MaxWorkflowStatuses {
		return usecase.Status{}, 0, &operations.Error{Code: operations.Conflict}
	}
	row, e := r.queries(ctx).CreateWorkflowStatus(ctx, db.CreateWorkflowStatusParams{WorkspaceID: t.WorkspaceID, TeamID: t.ID, Name: in.Name, Category: in.Category, Color: in.Color})
	return status(w, t.Uuid, row), v, e
}
func (r *Postgres) UpdateStatus(ctx context.Context, w uuid.UUID, in usecase.UpdateStatusInput) (usecase.Status, int64, error) {
	t, v, e := r.workflow(ctx, w, in.TeamID, in.ExpectedWorkflowVersion)
	if e != nil {
		return usecase.Status{}, 0, e
	}
	q := r.queries(ctx)
	old, e := q.GetWorkflowStatus(ctx, db.GetWorkflowStatusParams{TeamID: t.ID, Uuid: in.StatusID})
	if e != nil {
		return usecase.Status{}, 0, mapped(e, operations.NotFound)
	}
	row, e := q.UpdateWorkflowStatus(ctx, db.UpdateWorkflowStatusParams{ID: old.ID, Name: in.Name, Category: in.Category, Color: in.Color, Version: in.ExpectedVersion})
	return status(w, t.Uuid, row), v, mapped(e, operations.VersionConflict)
}
func (r *Postgres) ArchiveStatus(ctx context.Context, w uuid.UUID, in usecase.ArchiveStatusInput) (usecase.Status, int64, error) {
	t, v, e := r.workflow(ctx, w, in.TeamID, in.ExpectedWorkflowVersion)
	if e != nil {
		return usecase.Status{}, 0, e
	}
	q := r.queries(ctx)
	rows, e := q.AllWorkflowStatuses(ctx, t.ID)
	if e != nil {
		return usecase.Status{}, 0, e
	}
	n := 0
	for _, row := range rows {
		if !row.Archived {
			n++
		}
	}
	if n <= 1 {
		return usecase.Status{}, 0, &operations.Error{Code: operations.Conflict}
	}
	old, e := q.GetWorkflowStatus(ctx, db.GetWorkflowStatusParams{TeamID: t.ID, Uuid: in.StatusID})
	if e != nil {
		return usecase.Status{}, 0, mapped(e, operations.NotFound)
	}
	row, e := q.ArchiveWorkflowStatus(ctx, db.ArchiveWorkflowStatusParams{ID: old.ID, Version: in.ExpectedVersion})
	return status(w, t.Uuid, row), v, mapped(e, operations.VersionConflict)
}
func (r *Postgres) Reorder(ctx context.Context, w uuid.UUID, in usecase.ReorderInput) (int64, error) {
	t, v, e := r.workflow(ctx, w, in.TeamID, in.ExpectedWorkflowVersion)
	if e != nil {
		return 0, e
	}
	q := r.queries(ctx)
	rows, e := q.AllWorkflowStatuses(ctx, t.ID)
	if e != nil {
		return 0, e
	}
	active := map[uuid.UUID]db.WorkflowStatus{}
	var archived []db.WorkflowStatus
	for _, row := range rows {
		if row.Archived {
			archived = append(archived, row)
		} else {
			active[row.Uuid] = row
		}
	}
	if len(in.StatusIDs) != len(active) {
		return 0, &operations.Error{Code: operations.Validation}
	}
	for i, id := range in.StatusIDs {
		row, ok := active[cursor(id)]
		if !ok {
			return 0, &operations.Error{Code: operations.Validation}
		}
		delete(active, cursor(id))
		if e = q.RepositionWorkflowStatus(ctx, db.RepositionWorkflowStatusParams{ID: row.ID, Position: int64(i)}); e != nil {
			return 0, e
		}
	}
	for i, row := range archived {
		if e = q.RepositionWorkflowStatus(ctx, db.RepositionWorkflowStatusParams{ID: row.ID, Position: int64(len(in.StatusIDs) + i)}); e != nil {
			return 0, e
		}
	}
	return v, nil
}
func (r *Postgres) CreateLabel(ctx context.Context, w uuid.UUID, in usecase.CreateLabelInput) (usecase.Label, error) {
	if e := mutation(ctx); e != nil {
		return usecase.Label{}, e
	}
	q := r.queries(ctx)
	wid, e := q.WorkspaceInternalID(ctx, w)
	if e != nil {
		return usecase.Label{}, mapped(e, operations.NotFound)
	}
	tid := pgtype.Int8{}
	t := cursor(in.TeamID)
	if t != uuid.Nil {
		row, e := r.locked(ctx, w, t)
		if e != nil {
			return usecase.Label{}, e
		}
		tid = pgtype.Int8{Int64: row.ID, Valid: true}
	}
	row, e := q.CreateLabel(ctx, db.CreateLabelParams{WorkspaceID: wid, TeamID: tid, Name: in.Name, Color: in.Color})
	return label(w, t, row), mapped(e, operations.NotFound)
}
func (r *Postgres) GetLabel(ctx context.Context, w, id uuid.UUID) (usecase.Label, error) {
	row, e := r.queries(ctx).GetLabel(ctx, db.GetLabelParams{Uuid: w, Uuid_2: id})
	return label(w, row.TeamUuid, db.Label{Uuid: row.Uuid, Name: row.Name, Color: row.Color, Version: row.Version, Archived: row.Archived}), mapped(e, operations.NotFound)
}
func (r *Postgres) ListLabels(ctx context.Context, w uuid.UUID, in usecase.LabelListInput) ([]usecase.Label, error) {
	if in.TeamID != "" {
		if _, e := r.GetTeam(ctx, w, cursor(in.TeamID)); e != nil {
			return nil, e
		}
	}
	rows, e := r.queries(ctx).ListLabels(ctx, db.ListLabelsParams{Uuid: w, Archived: in.Archived, Column3: cursor(in.After), TeamUuid: cursor(in.TeamID), RowLimit: in.Limit + 1})
	out := make([]usecase.Label, 0, len(rows))
	for _, row := range rows {
		out = append(out, label(w, row.TeamUuid, db.Label{Uuid: row.Uuid, Name: row.Name, Color: row.Color, Version: row.Version, Archived: row.Archived}))
	}
	return out, e
}
func (r *Postgres) labelWrite(ctx context.Context, w, id uuid.UUID) (db.GetLabelRow, error) {
	if e := mutation(ctx); e != nil {
		return db.GetLabelRow{}, e
	}
	row, e := r.queries(ctx).GetLabel(ctx, db.GetLabelParams{Uuid: w, Uuid_2: id})
	if e != nil {
		return row, mapped(e, operations.NotFound)
	}
	if row.TeamUuid != uuid.Nil {
		if _, e = r.locked(ctx, w, row.TeamUuid); e != nil {
			return row, e
		}
	}
	return row, nil
}
func (r *Postgres) UpdateLabel(ctx context.Context, w uuid.UUID, in usecase.UpdateLabelInput) (usecase.Label, error) {
	old, e := r.labelWrite(ctx, w, in.LabelID)
	if e != nil {
		return usecase.Label{}, e
	}
	row, e := r.queries(ctx).UpdateLabel(ctx, db.UpdateLabelParams{ID: old.ID, Name: in.Name, Color: in.Color, Version: in.ExpectedVersion})
	return label(w, old.TeamUuid, row), mapped(e, operations.VersionConflict)
}
func (r *Postgres) ArchiveLabel(ctx context.Context, w uuid.UUID, in usecase.ArchiveLabelInput) (usecase.Label, error) {
	old, e := r.labelWrite(ctx, w, in.LabelID)
	if e != nil {
		return usecase.Label{}, e
	}
	row, e := r.queries(ctx).ArchiveLabel(ctx, db.ArchiveLabelParams{ID: old.ID, Version: in.ExpectedVersion})
	return label(w, old.TeamUuid, row), mapped(e, operations.VersionConflict)
}

// AllocateIssueNumber must be called within the future issue insertion transaction.
// There is deliberately no transport operation that reserves a number.
func (r *Postgres) AllocateIssueNumber(ctx context.Context, w, t uuid.UUID) (int64, error) {
	if e := mutation(ctx); e != nil {
		return 0, e
	}
	n, e := r.queries(ctx).AllocateTeamIssueNumber(ctx, db.AllocateTeamIssueNumberParams{Uuid: w, Uuid_2: t})
	return n, mapped(e, operations.NotFound)
}

// ValidateAssignment establishes workspace/team ownership for future issue writes.
func (r *Postgres) ValidateAssignment(ctx context.Context, w, t, st uuid.UUID, labels []uuid.UUID) error {
	if e := mutation(ctx); e != nil {
		return e
	}
	team, e := r.locked(ctx, w, t)
	if e != nil {
		return e
	}
	row, e := r.queries(ctx).GetWorkflowStatus(ctx, db.GetWorkflowStatusParams{TeamID: team.ID, Uuid: st})
	if e != nil {
		return mapped(e, operations.NotFound)
	}
	if row.Archived {
		return &operations.Error{Code: operations.Conflict}
	}
	for _, id := range labels {
		locked, e := r.queries(ctx).LockLabel(ctx, db.LockLabelParams{Uuid: w, Uuid_2: id})
		l := label(w, locked.TeamUuid, db.Label{Uuid: locked.Uuid, Name: locked.Name, Color: locked.Color, Version: locked.Version, Archived: locked.Archived})
		if e != nil {
			return mapped(e, operations.NotFound)
		}
		if l.Archived {
			return &operations.Error{Code: operations.Conflict}
		}
		if l.TeamID != "" && l.TeamID != t.String() {
			return &operations.Error{Code: operations.Validation}
		}
	}
	return nil
}

var _ usecase.Repository = (*Postgres)(nil)
