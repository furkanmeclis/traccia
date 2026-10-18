package repository

import (
	"context"
	"encoding/json"
	"github.com/furkanmeclis/traccia/backend/internal/database/db"
	uc "github.com/furkanmeclis/traccia/backend/internal/modules/issues/usecase"
	"github.com/furkanmeclis/traccia/backend/internal/platform/dbtx"
	"github.com/furkanmeclis/traccia/backend/internal/platform/events"
	"github.com/furkanmeclis/traccia/backend/internal/platform/outbox"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
)

func (r *Postgres) RecordEvent(ctx context.Context, w, actor uuid.UUID, name string, i uc.Issue) error {
	if e := mutation(ctx); e != nil {
		return e
	}
	q := r.queries(ctx)
	tx, _ := dbtx.Tx(ctx)
	now, e := q.DatabaseTime(ctx)
	if e != nil {
		return e
	}
	ev := events.New(name)
	ev.WorkspaceID = w
	ev.ActorID = actor
	ev.OccurredAt = now.Time
	ev.EntityType = "issues"
	ev.EntityUUID = &i.ID
	ev.Payload = map[string]any{"resource_type": "issues", "resource_id": i.ID, "issue_id": i.ID, "identifier": i.Identifier, "team_id": i.TeamID, "project_id": i.ProjectID, "name": i.Title, "version": i.Version, "issue": i}
	if e = outbox.NewStore(r.pool, r.q).Enqueue(ctx, tx, ev); e != nil {
		return e
	}
	event, e := q.GetOutboxEventByUUID(ctx, ev.EventID)
	if e != nil {
		return e
	}
	row, e := q.LockIssue(ctx, db.LockIssueParams{Uuid: w, Uuid_2: i.ID})
	if e != nil {
		return e
	}
	raw, e := json.Marshal(i)
	if e != nil {
		return e
	}
	if e = q.InsertIssueHistory(ctx, db.InsertIssueHistoryParams{Uuid: ev.EventID, EventID: event.ID, WorkspaceID: row.WorkspaceID, IssueID: row.ID, ActorUuid: actor, EventName: name, Version: i.Version, OccurredAt: now, Snapshot: raw}); e != nil {
		return e
	}
	var project pgtype.UUID
	if i.ProjectID != nil {
		project = pgtype.UUID{Bytes: ptrID(i.ProjectID), Valid: true}
	}
	return q.InsertIssueWorkspaceHistory(ctx, db.InsertIssueWorkspaceHistoryParams{EventID: event.ID, Uuid: ev.EventID, WorkspaceUuid: w, ActorUuid: actor, EventName: name, Name: i.Title, Version: i.Version, OccurredAt: now, ResourceUuid: pgtype.UUID{Bytes: i.ID, Valid: true}, TeamUuid: pgtype.UUID{Bytes: i.TeamID, Valid: true}, ProjectUuid: project, IssueIdentifier: i.Identifier})
}
