package repository

import (
	"context"
	"encoding/json"
	"github.com/furkanmeclis/traccia/backend/internal/database/db"
	uc "github.com/furkanmeclis/traccia/backend/internal/modules/comments/usecase"
	plate "github.com/furkanmeclis/traccia/backend/internal/modules/documents/usecase"
	"github.com/furkanmeclis/traccia/backend/internal/platform/dbtx"
	"github.com/furkanmeclis/traccia/backend/internal/platform/events"
	"github.com/furkanmeclis/traccia/backend/internal/platform/outbox"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
)

func (r *Postgres) record(ctx context.Context, w, actor uuid.UUID, o db.DocumentOwner, row db.IssueComment, in uc.Mutation, parent uuid.UUID, comment uc.Comment, now pgtype.Timestamptz) error {
	q := r.queries(ctx)
	tx, _ := dbtx.Tx(ctx)
	name := map[string]string{"create": "comments.created", "replace": "comments.replaced", "patch": "comments.patched", "restore": "comments.restored", "delete": "comments.deleted"}[in.Kind]
	ev := events.New(name)
	ev.WorkspaceID = w
	ev.ActorID = actor
	ev.OccurredAt = now.Time
	resource := o.Kind + "s"
	ev.EntityType = resource
	ev.EntityUUID = &o.Uuid
	payload := map[string]any{"resource_type": resource, "resource_id": o.Uuid, "name": o.Name, "version": row.Version, "comment_id": row.Uuid, "revision": row.HeadRevision, "parent_revision": parent, "schema_version": plate.SchemaVersion, "kind": in.Kind, "author_id": row.AuthorUuid, "created_channel": row.CreatedChannel, "channel": in.Channel, "deleted": row.Deleted}
	if o.Kind == "issue" {
		payload["issue_id"] = o.Uuid
		payload["identifier"] = o.Identifier
		payload["team_id"] = o.TeamUuid
	}
	if o.ProjectUuid.Valid {
		payload["project_id"] = uuid.UUID(o.ProjectUuid.Bytes)
	}
	var checkpoint []byte
	patches := []byte("[]")
	var e error
	if in.Kind == "patch" {
		patches, e = json.Marshal(in.Patches)
		payload["patches"] = in.Patches
	} else {
		checkpoint, e = json.Marshal(comment.Content)
		payload["content"] = comment.Content
	}
	if e != nil {
		return e
	}
	ev.Payload = payload
	if e = outbox.NewStore(r.pool, r.q).Enqueue(ctx, tx, ev); e != nil {
		return e
	}
	event, e := q.GetOutboxEventByUUID(ctx, ev.EventID)
	if e != nil {
		return e
	}
	_, e = q.InsertCommentRevision(ctx, db.InsertCommentRevisionParams{Uuid: row.HeadRevision, WorkspaceID: row.WorkspaceID, CommentID: row.ID, ParentRevision: nullable(parent), SourceRevision: nullable(in.SourceRevision), Version: row.Version, SchemaVersion: plate.SchemaVersion, Kind: in.Kind, Channel: in.Channel, Deleted: row.Deleted, Checkpoint: checkpoint, Patches: patches, ActorUuid: actor, EventID: event.ID, OccurredAt: now})
	if e != nil {
		return mapped(e)
	}
	return q.InsertIssueWorkspaceHistory(ctx, db.InsertIssueWorkspaceHistoryParams{EventID: event.ID, Uuid: ev.EventID, WorkspaceUuid: w, ActorUuid: actor, EventName: name, Name: o.Name, Version: row.Version, OccurredAt: now, ResourceUuid: nullable(o.Uuid), TeamUuid: nullable(o.TeamUuid), ProjectUuid: o.ProjectUuid, IssueIdentifier: o.Identifier})
}
