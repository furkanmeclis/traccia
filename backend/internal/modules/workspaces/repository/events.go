package repository

import (
	"context"
	"errors"
	"fmt"
	"github.com/furkanmeclis/traccia/backend/internal/database/db"
	"github.com/furkanmeclis/traccia/backend/internal/modules/workspaces/usecase"
	"github.com/furkanmeclis/traccia/backend/internal/platform/dbtx"
	"github.com/furkanmeclis/traccia/backend/internal/platform/events"
	"github.com/furkanmeclis/traccia/backend/internal/platform/operations"
	"github.com/furkanmeclis/traccia/backend/internal/platform/outbox"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"time"
)

func (r *Postgres) RecordWorkspaceEvent(ctx context.Context, actor uuid.UUID, w usecase.Workspace, name string) error {
	tx, ok := dbtx.Tx(ctx)
	if !ok {
		return errors.New("workspace event requires mutation transaction")
	}
	q := r.queries(ctx)
	now, err := q.DatabaseTime(ctx)
	if err != nil {
		return err
	}
	ev := events.New(name)
	ev.WorkspaceID = w.ID
	ev.ActorID = actor
	ev.OccurredAt = now.Time
	ev.Payload = map[string]any{"name": w.Name, "version": w.Version}
	if err := outbox.NewStore(r.pool, r.q).Enqueue(ctx, tx, ev); err != nil {
		return err
	}
	row, err := q.GetOutboxEventByUUID(ctx, ev.EventID)
	if err != nil {
		return err
	}
	return q.InsertWorkspaceHistory(ctx, db.InsertWorkspaceHistoryParams{EventID: row.ID, Uuid: ev.EventID, WorkspaceUuid: w.ID, ActorUuid: actor, EventName: name, Name: w.Name, Version: w.Version, OccurredAt: now})
}

// DurableConsumers is the shared production handler catalog. Delivery registration
// lives in migration 33, so API and worker never race to discover subscribers.
func (r *Postgres) DurableConsumers() (*outbox.Registry, error) {
	return outbox.NewRegistry(r.ActivityConsumer())
}
func (r *Postgres) ActivityConsumer() outbox.Consumer {
	return outbox.Consumer{Name: "workspace.activity", Version: 1, Handle: r.projectActivity}
}
func (r *Postgres) projectActivity(ctx context.Context, ev events.Event) error {
	if _, ok := dbtx.Tx(ctx); !ok {
		return errors.New("activity projection requires delivery transaction")
	}
	switch ev.Name {
	case "workspaces.created", "workspaces.updated", "teams.created", "teams.updated", "teams.archived", "workflow_statuses.created", "workflow_statuses.updated", "workflow_statuses.archived", "workflow_statuses.reordered", "labels.created", "labels.updated", "labels.archived":
	default:
		return errors.New("activity event is unsupported")
	}
	n, err := r.queries(ctx).ProjectWorkspaceActivity(ctx, db.ProjectWorkspaceActivityParams{Uuid: ev.EventID, WorkspaceUuid: ev.WorkspaceID})
	if err != nil {
		return err
	}
	if n == 0 {
		// A committed projection can safely be observed again; absence of canonical
		// history must never be acknowledged as successful processing.
		found, err := r.queries(ctx).WorkspaceActivityExists(ctx, db.WorkspaceActivityExistsParams{Uuid: ev.EventID, WorkspaceUuid: ev.WorkspaceID})
		if err != nil {
			return err
		}
		if !found {
			return errors.New("canonical workspace history is missing")
		}
	}
	return nil
}
func (r *Postgres) ListActivity(ctx context.Context, w, after uuid.UUID, limit int32) ([]usecase.Activity, error) {
	rows, err := r.queries(ctx).ListWorkspaceActivity(ctx, db.ListWorkspaceActivityParams{WorkspaceUuid: w, AfterUuid: after, RowLimit: limit})
	result := make([]usecase.Activity, 0, len(rows))
	for _, row := range rows {
		result = append(result, usecase.Activity{ID: row.Uuid, WorkspaceID: row.WorkspaceUuid, ActorID: row.ActorUuid, EventName: row.EventName, Name: row.Name, Version: row.Version, ResourceType: row.ResourceType, ResourceID: resourceID(row.ResourceUuid, row.WorkspaceUuid), TeamID: nullableUUID(row.TeamUuid), OccurredAt: row.OccurredAt.Time})
	}
	return result, err
}
func nullableTime(t pgtype.Timestamptz) *time.Time {
	if !t.Valid {
		return nil
	}
	return &t.Time
}
func (r *Postgres) ListDeliveries(ctx context.Context, w, after uuid.UUID, limit int32) ([]usecase.Delivery, error) {
	rows, err := r.queries(ctx).ListEventDeliveries(ctx, db.ListEventDeliveriesParams{WorkspaceUuid: w, AfterUuid: after, RowLimit: limit})
	result := make([]usecase.Delivery, 0, len(rows))
	for _, row := range rows {
		result = append(result, usecase.Delivery{ID: row.Uuid, EventID: row.EventUuid, EventName: row.EventName, Consumer: row.ConsumerName, ConsumerVersion: row.ConsumerVersion, Status: row.Status, Attempts: row.Attempts, AvailableAt: row.AvailableAt.Time, LeaseUntil: nullableTime(row.LeaseUntil), SucceededAt: nullableTime(row.SucceededAt), ErrorCode: safeDeliveryCode(row.ErrorCode)})
	}
	return result, err
}
func safeDeliveryCode(code string) string {
	switch code {
	case "", "consumer_failed", "consumer_unavailable", "invalid_event", "lease_expired":
		return code
	default:
		return "consumer_failed"
	}
}
func (r *Postgres) ReplayDelivery(ctx context.Context, w, id uuid.UUID) error {
	if _, ok := dbtx.Tx(ctx); !ok {
		return fmt.Errorf("delivery replay requires mutation transaction")
	}
	_, err := r.queries(ctx).ReplayEventDelivery(ctx, db.ReplayEventDeliveryParams{DeliveryUuid: id, WorkspaceUuid: w})
	if errors.Is(err, pgx.ErrNoRows) {
		return &operations.Error{Code: operations.Conflict}
	}
	return err
}

func resourceID(id pgtype.UUID, fallback uuid.UUID) uuid.UUID {
	if id.Valid {
		return id.Bytes
	}
	return fallback
}
func nullableUUID(id pgtype.UUID) string {
	if !id.Valid {
		return ""
	}
	return uuid.UUID(id.Bytes).String()
}
