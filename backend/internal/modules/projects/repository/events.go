package repository

import (
	"context"
	"strings"

	"github.com/furkanmeclis/traccia/backend/internal/database/db"
	uc "github.com/furkanmeclis/traccia/backend/internal/modules/projects/usecase"
	"github.com/furkanmeclis/traccia/backend/internal/platform/dbtx"
	"github.com/furkanmeclis/traccia/backend/internal/platform/events"
	"github.com/furkanmeclis/traccia/backend/internal/platform/outbox"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
)

func (r *Postgres) RecordEvent(ctx context.Context, w, actor uuid.UUID, event string, id, p uuid.UUID, name string, version int64) error {
	if e := mutation(ctx); e != nil {
		return e
	}
	q := r.queries(ctx)
	tx, _ := dbtx.Tx(ctx)
	now, e := q.DatabaseTime(ctx)
	if e != nil {
		return e
	}
	pr, e := r.GetProject(ctx, w, p)
	if e != nil {
		return e
	}
	kind := strings.Split(event, ".")[0]
	ev := events.New(event)
	ev.WorkspaceID = w
	ev.ActorID = actor
	ev.OccurredAt = now.Time
	ev.EntityType = kind
	ev.EntityUUID = &id
	ev.Payload = map[string]any{"resource_type": kind, "resource_id": id, "project_id": p, "name": name, "version": version, "project_version": pr.Version, "project": pr}
	switch kind {
	case "projects", "project_teams":
		internal, e := q.GetProject(ctx, db.GetProjectParams{Uuid: w, Uuid_2: p})
		if e != nil {
			return e
		}
		rows, e := q.AllProjectContributions(ctx, db.AllProjectContributionsParams{WorkspaceID: internal.WorkspaceID, ProjectID: internal.ID})
		if e != nil {
			return e
		}
		contributors := make([]any, 0, len(rows))
		for _, row := range rows {
			contributors = append(contributors, map[string]any{"team": team(w, db.Team{Uuid: row.Uuid, Name: row.Name, Code: row.Code, Version: row.Version, WorkflowVersion: row.WorkflowVersion, Archived: row.Archived, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt}), "archived": row.ContributionArchived})
		}
		ev.Payload["contributors"] = contributors
	case "project_milestones":
		row, _, e := r.GetMilestone(ctx, w, uc.MilestoneInput{ProjectID: p, MilestoneID: id})
		if e != nil {
			return e
		}
		ev.Payload["milestone"] = row
	case "project_resources":
		row, _, e := r.GetResource(ctx, w, uc.ResourceInput{ProjectID: p, ResourceID: id})
		if e != nil {
			return e
		}
		ev.Payload["resource"] = row
	}
	if e = outbox.NewStore(r.pool, r.q).Enqueue(ctx, tx, ev); e != nil {
		return e
	}
	out, e := q.GetOutboxEventByUUID(ctx, ev.EventID)
	if e != nil {
		return e
	}
	return q.InsertProjectHistory(ctx, db.InsertProjectHistoryParams{EventID: out.ID, Uuid: ev.EventID, WorkspaceUuid: w, ActorUuid: actor, EventName: event, Name: name, Version: version, OccurredAt: now, ResourceType: kind, ResourceUuid: pgtype.UUID{Bytes: id, Valid: true}, ProjectUuid: pgtype.UUID{Bytes: p, Valid: true}})
}
