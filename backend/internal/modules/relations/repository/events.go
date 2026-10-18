package repository

import (
	"context"
	"encoding/json"
	"github.com/furkanmeclis/traccia/backend/internal/database/db"
	uc "github.com/furkanmeclis/traccia/backend/internal/modules/relations/usecase"
	"github.com/furkanmeclis/traccia/backend/internal/platform/dbtx"
	"github.com/furkanmeclis/traccia/backend/internal/platform/events"
	"github.com/furkanmeclis/traccia/backend/internal/platform/outbox"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
)

func (r *Postgres) record(ctx context.Context, w, actor uuid.UUID, name string, row db.Issue, ref uc.IssueReference, snapshot uc.MutationOutput) error {
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
	ev.EntityUUID = &ref.ID
	ev.Payload = map[string]any{"resource_type": "issues", "resource_id": ref.ID, "issue_id": ref.ID, "identifier": ref.Identifier, "team_id": ref.TeamID, "name": ref.Title, "version": ref.RelationVersion, "relations": snapshot}
	if e = outbox.NewStore(r.pool, r.q).Enqueue(ctx, tx, ev); e != nil {
		return e
	}
	event, e := q.GetOutboxEventByUUID(ctx, ev.EventID)
	if e != nil {
		return e
	}
	raw, e := json.Marshal(snapshot)
	if e != nil {
		return e
	}
	if e = q.InsertIssueRelationHistory(ctx, db.InsertIssueRelationHistoryParams{Uuid: ev.EventID, EventID: event.ID, WorkspaceID: row.WorkspaceID, IssueID: row.ID, ActorUuid: actor, EventName: name, Version: ref.RelationVersion, OccurredAt: now, Snapshot: raw}); e != nil {
		return e
	}
	return q.InsertIssueWorkspaceHistory(ctx, db.InsertIssueWorkspaceHistoryParams{EventID: event.ID, Uuid: ev.EventID, WorkspaceUuid: w, ActorUuid: actor, EventName: name, Name: ref.Title, Version: ref.RelationVersion, OccurredAt: now, ResourceUuid: pgtype.UUID{Bytes: ref.ID, Valid: true}, TeamUuid: pgtype.UUID{Bytes: ref.TeamID, Valid: true}, IssueIdentifier: ref.Identifier})
}
