package repository

import (
	"context"
	"github.com/furkanmeclis/traccia/backend/internal/database/db"
	uc "github.com/furkanmeclis/traccia/backend/internal/modules/products/usecase"
	"github.com/furkanmeclis/traccia/backend/internal/platform/dbtx"
	"github.com/furkanmeclis/traccia/backend/internal/platform/events"
	"github.com/furkanmeclis/traccia/backend/internal/platform/outbox"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"strings"
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
	pr, e := r.GetProduct(ctx, w, p)
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
	ev.Payload = map[string]any{"resource_type": kind, "resource_id": id, "product_id": p, "name": name, "version": version, "product_version": pr.Version, "product": pr}
	var env pgtype.UUID
	switch kind {
	case "products", "product_projects":
		internal, e := q.GetProduct(ctx, db.GetProductParams{Uuid: w, Uuid_2: p})
		if e != nil {
			return e
		}
		rows, e := q.AllProductProjects(ctx, db.AllProductProjectsParams{WorkspaceID: internal.WorkspaceID, ProductID: internal.ID})
		if e != nil {
			return e
		}
		links := make([]any, 0, len(rows))
		for _, row := range rows {
			links = append(links, map[string]any{"project": project(w, db.ListProductProjectsRow{ID: row.ID, Uuid: row.Uuid, WorkspaceID: row.WorkspaceID, Name: row.Name, Summary: row.Summary, LeadUserID: row.LeadUserID, StartDate: row.StartDate, TargetDate: row.TargetDate, Version: row.Version, Archived: row.Archived, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt, LeadUserUuid: row.LeadUserUuid}), "archived": row.AssociationArchived})
		}
		ev.Payload["projects"] = links
	case "product_environments":
		row, _, e := r.GetEnvironment(ctx, w, uc.EnvironmentInput{ProductID: p, EnvironmentID: id})
		if e != nil {
			return e
		}
		ev.Payload["environment"] = row
		ev.Payload["environment_id"] = id
		env = pgtype.UUID{Bytes: id, Valid: true}
	}
	if e = outbox.NewStore(r.pool, r.q).Enqueue(ctx, tx, ev); e != nil {
		return e
	}
	out, e := q.GetOutboxEventByUUID(ctx, ev.EventID)
	if e != nil {
		return e
	}
	return q.InsertProductHistory(ctx, db.InsertProductHistoryParams{EventID: out.ID, Uuid: ev.EventID, WorkspaceUuid: w, ActorUuid: actor, EventName: event, Name: name, Version: version, OccurredAt: now, ResourceType: kind, ResourceUuid: pgtype.UUID{Bytes: id, Valid: true}, ProductUuid: pgtype.UUID{Bytes: p, Valid: true}, EnvironmentUuid: env})
}
