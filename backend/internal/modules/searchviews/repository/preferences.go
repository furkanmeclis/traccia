package repository

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/furkanmeclis/traccia/backend/internal/database/db"
	uc "github.com/furkanmeclis/traccia/backend/internal/modules/searchviews/usecase"
	"github.com/furkanmeclis/traccia/backend/internal/platform/dbtx"
	"github.com/furkanmeclis/traccia/backend/internal/platform/events"
	"github.com/furkanmeclis/traccia/backend/internal/platform/operations"
	"github.com/furkanmeclis/traccia/backend/internal/platform/outbox"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

func viewDTO(w uuid.UUID, row db.SavedView) (uc.SavedView, error) {
	v := uc.SavedView{ID: row.Uuid, WorkspaceID: w, ActorID: row.ActorUuid, Revision: row.Revision, Name: row.Name, Deleted: row.Deleted, CreatedAt: row.CreatedAt.Time, UpdatedAt: row.UpdatedAt.Time}
	if e := json.Unmarshal(row.Definition, &v.Definition); e != nil {
		return v, e
	}
	e := json.Unmarshal(row.Columns, &v.Columns)
	return v, e
}
func (r *Postgres) GetView(c context.Context, w, a, id uuid.UUID) (uc.SavedView, error) {
	row, e := r.queries(c).ReadSavedView(c, db.ReadSavedViewParams{Uuid: w, ActorUuid: a, Uuid_2: id})
	if e != nil {
		return uc.SavedView{}, mapped(e)
	}
	return viewDTO(w, row)
}
func (r *Postgres) ListViews(c context.Context, w, a uuid.UUID, in uc.ViewListInput) ([]uc.SavedView, error) {
	anchor, e := uc.ViewAnchor(w, a, in)
	if e != nil {
		return nil, invalidCursor()
	}
	q := r.queries(c)
	if anchor != uuid.Nil {
		ok, e := q.SavedViewAnchorExists(c, db.SavedViewAnchorExistsParams{Uuid: w, ActorUuid: a, Uuid_2: anchor})
		if e != nil {
			return nil, e
		}
		if !ok {
			return nil, invalidCursor()
		}
	}
	rows, e := q.ListSavedViews(c, db.ListSavedViewsParams{Uuid: w, ActorUuid: a, Resource: in.Resource, IncludeDeleted: in.IncludeDeleted, AfterUuid: anchor, RowLimit: in.Limit + 1})
	out := make([]uc.SavedView, 0, len(rows))
	if e != nil {
		return out, e
	}
	for _, row := range rows {
		v, e := viewDTO(w, row)
		if e != nil {
			return nil, e
		}
		out = append(out, v)
	}
	return out, nil
}
func (r *Postgres) MutateView(c context.Context, w, a uuid.UUID, in uc.ViewMutation) (uc.SavedView, error) {
	if _, ok := dbtx.Tx(c); !ok {
		return uc.SavedView{}, errors.New("private preference requires operation transaction")
	}
	q := r.queries(c)
	var row db.SavedView
	var e error
	if in.Kind == "create" {
		definition, _ := json.Marshal(in.Definition)
		columns, _ := json.Marshal(in.Columns)
		row, e = q.InsertSavedView(c, db.InsertSavedViewParams{Uuid: w, ActorUuid: a, Revision: uuid.New(), Name: in.Name, Resource: in.Definition.Resource, Definition: definition, Columns: columns})
	} else {
		row, e = q.LockSavedView(c, db.LockSavedViewParams{Uuid: w, ActorUuid: a, Uuid_2: in.ViewID})
		if e != nil {
			return uc.SavedView{}, mapped(e)
		}
		if row.Revision != in.ExpectedRevision {
			return uc.SavedView{}, fail(operations.VersionConflict)
		}
		if (row.Deleted && in.Kind != "restore") || (!row.Deleted && in.Kind == "restore") {
			return uc.SavedView{}, fail(operations.Conflict)
		}
		if in.Kind == "update" {
			row.Name = in.Name
			row.Resource = in.Definition.Resource
			row.Definition, _ = json.Marshal(in.Definition)
			row.Columns, _ = json.Marshal(in.Columns)
		}
		row, e = q.AdvanceSavedView(c, db.AdvanceSavedViewParams{ID: row.ID, Revision: uuid.New(), Name: row.Name, Resource: row.Resource, Definition: row.Definition, Columns: row.Columns, Deleted: in.Kind == "delete"})
	}
	if e != nil {
		return uc.SavedView{}, mapped(e)
	}
	out, e := viewDTO(w, row)
	if e != nil {
		return out, e
	}
	snapshot, e := json.Marshal(out)
	if e != nil {
		return out, e
	}
	e = r.record(c, w, a, row.WorkspaceID, row.Uuid, row.ID, row.Revision, row.Version, "saved_views", in.Kind, snapshot)
	return out, e
}
func columnsDTO(row db.ColumnPreference) (uc.ColumnPreferences, error) {
	v := uc.ColumnPreferences{Resource: row.Resource, Revision: &row.Revision}
	e := json.Unmarshal(row.Columns, &v.Columns)
	return v, e
}
func (r *Postgres) GetColumns(c context.Context, w, a uuid.UUID, resource string) (uc.ColumnPreferences, error) {
	row, e := r.queries(c).ReadColumnPreferences(c, db.ReadColumnPreferencesParams{Uuid: w, ActorUuid: a, Resource: resource})
	if errors.Is(e, pgx.ErrNoRows) {
		return uc.ColumnPreferences{Resource: resource, Columns: []uc.Column{}}, nil
	}
	if e != nil {
		return uc.ColumnPreferences{}, e
	}
	return columnsDTO(row)
}
func (r *Postgres) SetColumns(c context.Context, w, a uuid.UUID, in uc.ColumnsSetInput) (uc.ColumnPreferences, error) {
	if _, ok := dbtx.Tx(c); !ok {
		return uc.ColumnPreferences{}, errors.New("private preference requires operation transaction")
	}
	q := r.queries(c)
	columns, _ := json.Marshal(in.Columns)
	var row db.ColumnPreference
	var e error
	if in.ExpectedRevision == nil {
		row, e = q.InsertColumnPreferences(c, db.InsertColumnPreferencesParams{Uuid: w, ActorUuid: a, Resource: in.Resource, Revision: uuid.New(), Columns: columns})
		if errors.Is(e, pgx.ErrNoRows) {
			return uc.ColumnPreferences{}, fail(operations.VersionConflict)
		}
	} else {
		row, e = q.LockColumnPreferences(c, db.LockColumnPreferencesParams{Uuid: w, ActorUuid: a, Resource: in.Resource})
		if errors.Is(e, pgx.ErrNoRows) {
			return uc.ColumnPreferences{}, fail(operations.VersionConflict)
		}
		if e == nil {
			if row.Revision != *in.ExpectedRevision {
				return uc.ColumnPreferences{}, fail(operations.VersionConflict)
			}
			row, e = q.AdvanceColumnPreferences(c, db.AdvanceColumnPreferencesParams{ID: row.ID, Revision: uuid.New(), Columns: columns})
		}
	}
	if e != nil {
		return uc.ColumnPreferences{}, e
	}
	out, e := columnsDTO(row)
	if e != nil {
		return out, e
	}
	snapshot, e := json.Marshal(out)
	if e != nil {
		return out, e
	}
	e = r.record(c, w, a, row.WorkspaceID, row.Uuid, row.ID, row.Revision, row.Version, "column_preferences", "set", snapshot)
	return out, e
}
func (r *Postgres) record(c context.Context, w, a uuid.UUID, workspaceID int64, id uuid.UUID, internalID int64, revision uuid.UUID, version int64, resource, kind string, snapshot []byte) error {
	tx, _ := dbtx.Tx(c)
	q := r.queries(c)
	now, e := q.DatabaseTime(c)
	if e != nil {
		return e
	}
	suffix := map[string]string{"create": "created", "update": "updated", "delete": "deleted", "restore": "restored", "set": "set"}[kind]
	name := resource + "." + suffix
	ev := events.New(name)
	ev.WorkspaceID = w
	ev.ActorID = a
	ev.OccurredAt = now.Time
	ev.EntityType = resource
	ev.EntityUUID = &id
	// Public derived channels contain no personal view name, filter, columns or
	// request body. Full snapshots remain only in the actor-scoped journal.
	ev.Payload = map[string]any{"resource_type": resource, "resource_id": id, "revision": revision, "version": version, "name": "Private preference"}
	if e = outbox.NewStore(r.pool, r.q).Enqueue(c, tx, ev); e != nil {
		return e
	}
	stored, e := q.GetOutboxEventByUUID(c, ev.EventID)
	if e != nil {
		return e
	}
	h := db.InsertPrivatePreferenceHistoryParams{WorkspaceID: workspaceID, ActorUuid: a, Revision: revision, Version: version, Kind: kind, Snapshot: snapshot, EventID: stored.ID, OccurredAt: now}
	if resource == "saved_views" {
		h.ViewID = pgtype.Int8{Int64: internalID, Valid: true}
	} else {
		h.ColumnPreferenceID = pgtype.Int8{Int64: internalID, Valid: true}
	}
	if e = q.InsertPrivatePreferenceHistory(c, h); e != nil {
		return e
	}
	return q.InsertPreferenceWorkspaceHistory(c, db.InsertPreferenceWorkspaceHistoryParams{EventID: stored.ID, Uuid: ev.EventID, WorkspaceUuid: w, ActorUuid: a, EventName: name, Version: version, OccurredAt: now, ResourceType: resource, ResourceUuid: pgtype.UUID{Bytes: id, Valid: true}})
}
