package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/furkanmeclis/traccia/backend/internal/database/db"
	uc "github.com/furkanmeclis/traccia/backend/internal/modules/personalwork/usecase"
	"github.com/furkanmeclis/traccia/backend/internal/platform/dbtx"
	"github.com/furkanmeclis/traccia/backend/internal/platform/events"
	"github.com/furkanmeclis/traccia/backend/internal/platform/operations"
	"github.com/furkanmeclis/traccia/backend/internal/platform/outbox"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

func text(v any) (*string, error) {
	switch x := v.(type) {
	case nil:
		return nil, nil
	case string:
		return &x, nil
	case pgtype.Text:
		if x.Valid {
			return &x.String, nil
		}
		return nil, nil
	default:
		return nil, fmt.Errorf("invalid target text type")
	}
}
func metadata(label, identifier any, archived bool) (uc.TargetMetadata, error) {
	l, e := text(label)
	if e != nil {
		return uc.TargetMetadata{}, e
	}
	if l == nil {
		return uc.TargetMetadata{}, errors.New("favorite target label missing")
	}
	i, e := text(identifier)
	return uc.TargetMetadata{Label: *l, Identifier: i, Archived: archived}, e
}
func favoriteDTO(w uuid.UUID, row db.PersonalFavorite, m uc.TargetMetadata) uc.Favorite {
	return uc.Favorite{ID: row.Uuid, WorkspaceID: w, ActorID: row.ActorUuid, Target: uc.Target{Kind: row.Kind, ID: row.TargetUuid}, Metadata: m, Revision: row.Revision, Active: row.Active, CreatedAt: row.CreatedAt.Time, UpdatedAt: row.UpdatedAt.Time}
}
func (r *Postgres) ListFavorites(ctx context.Context, w, a uuid.UUID, v uc.FavoriteListInput) ([]uc.Favorite, error) {
	q := r.queries(ctx)
	anchor := uuid.Nil
	if v.After != "" {
		cur, e := uc.DecodeCursor(v.After)
		if e != nil || cur.Workspace != w || cur.Actor != a || cur.IncludeInactive != v.IncludeInactive || cur.Session == uuid.Nil || cur.Ordinal != "" || cur.Fingerprint != "" {
			return nil, invalidCursor()
		}
		anchor = cur.Session
		ok, e := q.PersonalFavoriteAnchorExists(ctx, db.PersonalFavoriteAnchorExistsParams{Uuid: w, ActorUuid: a, Uuid_2: anchor})
		if e != nil {
			return nil, e
		}
		if !ok {
			return nil, invalidCursor()
		}
	}
	rows, e := q.ListPersonalFavorites(ctx, db.ListPersonalFavoritesParams{WorkspaceUuid: w, ActorUuid: a, IncludeInactive: v.IncludeInactive, AfterUuid: anchor, RowLimit: v.Limit + 1})
	if e != nil {
		return nil, e
	}
	out := make([]uc.Favorite, 0, len(rows))
	for _, row := range rows {
		m, e := metadata(row.Label, row.Identifier, row.Archived)
		if e != nil {
			return nil, e
		}
		out = append(out, favoriteDTO(w, db.PersonalFavorite{Uuid: row.Uuid, ActorUuid: row.ActorUuid, Kind: row.Kind, TargetUuid: row.TargetUuid, Revision: row.Revision, Active: row.Active, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt}, m))
	}
	return out, nil
}
func (r *Postgres) SetFavorite(ctx context.Context, w, a uuid.UUID, v uc.FavoriteSetInput) (uc.Favorite, error) {
	tx, ok := dbtx.Tx(ctx)
	if !ok {
		return uc.Favorite{}, errors.New("favorite requires operation transaction")
	}
	q := r.queries(ctx)
	if e := q.LockPersonalRequest(ctx, "favorite"+w.String()+a.String()+v.Target.Kind+v.Target.ID.String()); e != nil {
		return uc.Favorite{}, e
	}
	target, e := q.ReadFavoriteTarget(ctx, db.ReadFavoriteTargetParams{WorkspaceUuid: w, Kind: v.Target.Kind, TargetUuid: v.Target.ID})
	if e != nil {
		return uc.Favorite{}, mapped(e)
	}
	m, e := metadata(target.Label, target.Identifier, target.Archived)
	if e != nil {
		return uc.Favorite{}, e
	}
	var row db.PersonalFavorite
	if v.ExpectedRevision == nil {
		row, e = q.InsertPersonalFavorite(ctx, db.InsertPersonalFavoriteParams{ActorUuid: a, Revision: uuid.New(), WorkspaceUuid: w, Kind: v.Target.Kind, TargetUuid: v.Target.ID})
		if errors.Is(e, pgx.ErrNoRows) {
			return uc.Favorite{}, fail(operations.VersionConflict)
		}
	} else {
		row, e = q.LockPersonalFavorite(ctx, db.LockPersonalFavoriteParams{Uuid: w, ActorUuid: a, Kind: v.Target.Kind, TargetUuid: v.Target.ID})
		if e != nil {
			return uc.Favorite{}, mapped(e)
		}
		if row.Revision != *v.ExpectedRevision {
			return uc.Favorite{}, fail(operations.VersionConflict)
		}
		row, e = q.AdvancePersonalFavorite(ctx, db.AdvancePersonalFavoriteParams{ID: row.ID, Active: v.Active, Revision: uuid.New()})
	}
	if e != nil {
		return uc.Favorite{}, e
	}
	out := favoriteDTO(w, row, m)
	snapshot, e := json.Marshal(out)
	if e != nil {
		return out, e
	}
	now, e := q.DatabaseTime(ctx)
	if e != nil {
		return out, e
	}
	ev := events.New("favorites.set")
	ev.WorkspaceID = w
	ev.ActorID = a
	ev.EntityType = "favorites"
	ev.EntityUUID = &row.Uuid
	ev.OccurredAt = now.Time
	// Target identities, labels, and active state are private to the actor.
	ev.Payload = map[string]any{"resource_type": "favorites", "resource_id": row.Uuid, "revision": row.Revision, "version": row.Version, "name": "Private preference"}
	if e = outbox.NewStore(r.pool, r.q).Enqueue(ctx, tx, ev); e != nil {
		return out, e
	}
	stored, e := q.GetOutboxEventByUUID(ctx, ev.EventID)
	if e != nil {
		return out, e
	}
	if e = q.InsertPersonalFavoriteHistory(ctx, db.InsertPersonalFavoriteHistoryParams{WorkspaceID: row.WorkspaceID, ActorUuid: a, FavoriteID: row.ID, Revision: row.Revision, Version: row.Version, Snapshot: snapshot, EventID: stored.ID, OccurredAt: now}); e != nil {
		return out, e
	}
	e = q.InsertPreferenceWorkspaceHistory(ctx, db.InsertPreferenceWorkspaceHistoryParams{EventID: stored.ID, Uuid: ev.EventID, WorkspaceUuid: w, ActorUuid: a, EventName: "favorites.set", Version: row.Version, OccurredAt: now, ResourceType: "favorites", ResourceUuid: pgtype.UUID{Bytes: row.Uuid, Valid: true}})
	return out, e
}
