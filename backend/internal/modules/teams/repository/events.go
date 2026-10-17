package repository

import (
	"context"
	"strings"

	"github.com/furkanmeclis/traccia/backend/internal/database/db"
	"github.com/furkanmeclis/traccia/backend/internal/platform/dbtx"
	"github.com/furkanmeclis/traccia/backend/internal/platform/events"
	"github.com/furkanmeclis/traccia/backend/internal/platform/outbox"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
)

func (r *Postgres) RecordEvent(ctx context.Context, w, actor uuid.UUID, event string, id, team uuid.UUID, name string, version int64) error {
	if e := mutation(ctx); e != nil {
		return e
	}
	tx, _ := dbtx.Tx(ctx)
	q := r.queries(ctx)
	now, e := q.DatabaseTime(ctx)
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
	ev.Payload = map[string]any{"resource_type": kind, "resource_id": id, "team_id": team, "name": name, "version": version}
	// The immutable envelope retains the actual configuration snapshot. History
	// carries its searchable identity/name and links to this exact event.
	switch kind {
	case "teams":
		row, err := q.GetTeam(ctx, db.GetTeamParams{Uuid: w, Uuid_2: id})
		if err != nil {
			return err
		}
		ev.Payload["team"] = teamSnapshot(w, row)
		if event == "teams.created" {
			rows, err := q.AllWorkflowStatuses(ctx, row.ID)
			if err != nil {
				return err
			}
			snapshots := make([]any, 0, len(rows))
			for _, s := range rows {
				snapshots = append(snapshots, status(w, team, s))
			}
			ev.Payload["statuses"] = snapshots
		}
	case "workflow_statuses":
		t, err := q.GetTeam(ctx, db.GetTeamParams{Uuid: w, Uuid_2: team})
		if err != nil {
			return err
		}
		ev.Payload["workflow_version"] = t.WorkflowVersion
		if event == "workflow_statuses.reordered" {
			rows, err := q.AllWorkflowStatuses(ctx, t.ID)
			if err != nil {
				return err
			}
			snapshots := make([]any, 0, len(rows))
			for _, s := range rows {
				snapshots = append(snapshots, status(w, team, s))
			}
			ev.Payload["statuses"] = snapshots
		} else {
			row, err := q.GetWorkflowStatus(ctx, db.GetWorkflowStatusParams{TeamID: t.ID, Uuid: id})
			if err != nil {
				return err
			}
			ev.Payload["status"] = status(w, team, row)
		}
	case "labels":
		row, err := r.GetLabel(ctx, w, id)
		if err != nil {
			return err
		}
		ev.Payload["label"] = row
	}

	if e = outbox.NewStore(r.pool, r.q).Enqueue(ctx, tx, ev); e != nil {
		return e
	}
	row, e := q.GetOutboxEventByUUID(ctx, ev.EventID)
	if e != nil {
		return e
	}
	return q.InsertResourceHistory(ctx, db.InsertResourceHistoryParams{EventID: row.ID, Uuid: ev.EventID, WorkspaceUuid: w, ActorUuid: actor, EventName: event, Name: name, Version: version, OccurredAt: now, ResourceType: kind, ResourceUuid: pgtype.UUID{Bytes: id, Valid: true}, TeamUuid: pgtype.UUID{Bytes: team, Valid: team != uuid.Nil}})
}

func teamSnapshot(w uuid.UUID, row db.Team) any { return team(w, row) }
