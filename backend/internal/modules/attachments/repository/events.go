package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/furkanmeclis/traccia/backend/internal/database/db"
	uc "github.com/furkanmeclis/traccia/backend/internal/modules/attachments/usecase"
	"github.com/furkanmeclis/traccia/backend/internal/platform/dbtx"
	"github.com/furkanmeclis/traccia/backend/internal/platform/events"
	"github.com/furkanmeclis/traccia/backend/internal/platform/outbox"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
)

func (r *Postgres) enqueue(ctx context.Context, w, actor uuid.UUID, name, resource string, id uuid.UUID, version int64, filename string, payload map[string]any) (events.Event, int64, pgtype.Timestamptz, error) {
	q := r.queries(ctx)
	now, e := q.DatabaseTime(ctx)
	if e != nil {
		return events.Event{}, 0, now, e
	}
	ev := events.New(name)
	ev.WorkspaceID = w
	ev.ActorID = actor
	ev.OccurredAt = now.Time
	ev.EntityType = resource
	ev.EntityUUID = &id
	payload["resource_type"] = resource
	payload["resource_id"] = id
	payload["name"] = filename
	payload["version"] = version
	ev.Payload = payload
	tx, _ := dbtx.Tx(ctx)
	if e = outbox.NewStore(r.pool, r.q).Enqueue(ctx, tx, ev); e != nil {
		return ev, 0, now, e
	}
	event, e := q.GetOutboxEventByUUID(ctx, ev.EventID)
	return ev, event.ID, now, e
}
func (r *Postgres) recordUpload(ctx context.Context, w, actor uuid.UUID, row db.AttachmentUpload, name string, snapshot uc.Upload) error {
	ev, id, now, e := r.enqueue(ctx, w, actor, name, "attachment_uploads", row.Uuid, row.Version, "Attachment upload", map[string]any{"upload_id": row.Uuid})
	if e != nil {
		return e
	}
	raw, e := json.Marshal(snapshot)
	if e != nil {
		return e
	}
	q := r.queries(ctx)
	if e = q.InsertAttachmentUploadHistory(ctx, db.InsertAttachmentUploadHistoryParams{Uuid: ev.EventID, EventID: id, WorkspaceID: row.WorkspaceID, UploadID: row.ID, ActorUuid: actor, EventName: name, Version: row.Version, OccurredAt: now, Snapshot: raw}); e != nil {
		return e
	}
	return q.InsertResourceHistory(ctx, db.InsertResourceHistoryParams{EventID: id, Uuid: ev.EventID, WorkspaceUuid: w, ActorUuid: actor, EventName: name, Name: "Attachment upload", Version: row.Version, OccurredAt: now, ResourceType: "attachment_uploads", ResourceUuid: nullable(row.Uuid)})
}
func (r *Postgres) recordFile(ctx context.Context, w, actor uuid.UUID, row db.AttachmentPublic, name string, snapshot uc.Attachment) error {
	resource, id := "attachments", row.Uuid
	payload := map[string]any{"attachment_id": row.Uuid}
	eventLabel := "Attachment file"
	if row.IssueUuid.Valid {
		eventLabel = row.Filename
		payload["filename"] = row.Filename
		payload["sha256"] = row.Sha256
		payload["size_bytes"] = row.SizeBytes
		payload["file_revision"] = row.FileRevision
		resource = "issues"
		id = uuid.UUID(row.IssueUuid.Bytes)
		payload["issue_id"] = id
		payload["identifier"] = row.IssueIdentifier
		payload["team_id"] = uuid.UUID(row.TeamUuid.Bytes)
		if row.ProjectUuid.Valid {
			payload["project_id"] = uuid.UUID(row.ProjectUuid.Bytes)
		}
	}
	ev, eventID, now, e := r.enqueue(ctx, w, actor, name, resource, id, row.Version, eventLabel, payload)
	if e != nil {
		return e
	}
	raw, e := json.Marshal(snapshot)
	if e != nil {
		return e
	}
	q := r.queries(ctx)
	if e = q.InsertAttachmentHistory(ctx, db.InsertAttachmentHistoryParams{Uuid: ev.EventID, EventID: eventID, WorkspaceID: row.WorkspaceID, AttachmentID: row.ID, ActorUuid: actor, EventName: name, Version: row.Version, OccurredAt: now, Snapshot: raw}); e != nil {
		return e
	}
	if row.IssueUuid.Valid {
		return q.InsertIssueWorkspaceHistory(ctx, db.InsertIssueWorkspaceHistoryParams{EventID: eventID, Uuid: ev.EventID, WorkspaceUuid: w, ActorUuid: actor, EventName: name, Name: row.Filename, Version: row.Version, OccurredAt: now, ResourceUuid: row.IssueUuid, TeamUuid: row.TeamUuid, ProjectUuid: row.ProjectUuid, IssueIdentifier: fmt.Sprint(row.IssueIdentifier)})
	}
	return q.InsertResourceHistory(ctx, db.InsertResourceHistoryParams{EventID: eventID, Uuid: ev.EventID, WorkspaceUuid: w, ActorUuid: actor, EventName: name, Name: "Attachment file", Version: row.Version, OccurredAt: now, ResourceType: "attachments", ResourceUuid: nullable(row.Uuid)})
}
