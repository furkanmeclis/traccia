package repository

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/furkanmeclis/traccia/backend/internal/database/db"
	uc "github.com/furkanmeclis/traccia/backend/internal/modules/searchviews/usecase"
	"github.com/furkanmeclis/traccia/backend/internal/platform/dbtx"
	"github.com/furkanmeclis/traccia/backend/internal/platform/operations"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"strconv"
)

func fingerprint(resource string, in uc.Capture) (string, []byte, error) {
	q, e := json.Marshal(in.Query)
	if e != nil {
		return "", nil, e
	}
	body, e := json.Marshal(struct {
		Resource string
		Query    uc.Query
		TTL      *int32
		Limit    int32
	}{resource, in.Query, in.RequestedTTL, in.Limit})
	if e != nil {
		return "", nil, e
	}
	h := sha256.Sum256(body)
	return hex.EncodeToString(h[:]), q, nil
}
func (r *Postgres) Capture(c context.Context, w, a uuid.UUID, resource string, in uc.Capture, reauthorize func(context.Context) error) (uc.StoredPage, error) {
	tx, e := r.pool.BeginTx(c, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if e != nil {
		return uc.StoredPage{}, e
	}
	defer func() { _ = tx.Rollback(c) }()
	ctx := dbtx.WithTx(c, tx)
	q := r.q.WithTx(tx)
	hash, raw, e := fingerprint(resource, in)
	if e != nil {
		return uc.StoredPage{}, e
	}
	if e = q.LockSearchRequest(ctx, w.String()+a.String()+in.RequestID.String()); e != nil {
		return uc.StoredPage{}, e
	}
	if e = reauthorize(ctx); e != nil {
		return uc.StoredPage{}, e
	}
	found, e := q.FindSearchRequest(ctx, db.FindSearchRequestParams{Uuid: w, ActorUuid: a, RequestUuid: in.RequestID})
	var id uuid.UUID
	if e == nil {
		dead, err := q.SearchSessionExpired(ctx, found.ID)
		if err != nil {
			return uc.StoredPage{}, err
		}
		if dead {
			return uc.StoredPage{}, expired()
		}
		if found.Fingerprint != hash {
			return uc.StoredPage{}, fail(operations.IdempotencyConflict)
		}
		if found.State != "ready" {
			return uc.StoredPage{}, fail(operations.Conflict)
		}
		id = found.Uuid
	} else if errors.Is(e, pgx.ErrNoRows) {
		row, err := q.InsertSearchSession(ctx, db.InsertSearchSessionParams{Uuid: w, ActorUuid: a, RequestUuid: in.RequestID, Resource: resource, Fingerprint: hash, Query: raw, FirstLimit: in.Limit, TtlSeconds: in.TTLSeconds})
		if err != nil {
			return uc.StoredPage{}, mapped(err)
		}
		captured, err := q.CaptureSearchItems(ctx, db.CaptureSearchItemsParams{Query: raw, WorkspaceUuid: w, Resource: resource, WorkspaceID: row.WorkspaceID, SessionID: row.ID})
		if err != nil {
			return uc.StoredPage{}, err
		}
		row, err = q.PublishSearchSession(ctx, db.PublishSearchSessionParams{ID: row.ID, TotalCount: captured.TotalCount, AsOf: captured.AsOf, TtlSeconds: in.TTLSeconds})
		if err != nil {
			return uc.StoredPage{}, err
		}
		id = row.Uuid
	} else {
		return uc.StoredPage{}, e
	}
	out, e := r.readPage(ctx, w, a, resource, uc.PageInput{SessionID: id, Limit: in.Limit})
	if e != nil {
		return out, e
	}
	if e = reauthorize(ctx); e != nil {
		return uc.StoredPage{}, e
	}
	if e = tx.Commit(ctx); e != nil {
		return uc.StoredPage{}, e
	}
	return out, nil
}
func (r *Postgres) Page(c context.Context, w, a uuid.UUID, resource string, in uc.PageInput) (uc.StoredPage, error) {
	tx, e := r.pool.BeginTx(c, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if e != nil {
		return uc.StoredPage{}, e
	}
	defer func() { _ = tx.Rollback(c) }()
	out, e := r.readPage(dbtx.WithTx(c, tx), w, a, resource, in)
	if e != nil {
		return out, e
	}
	e = tx.Commit(c)
	return out, e
}
func (r *Postgres) readPage(c context.Context, w, a uuid.UUID, resource string, in uc.PageInput) (uc.StoredPage, error) {
	q := r.queries(c)
	row, e := q.ReadSearchSession(c, db.ReadSearchSessionParams{Uuid: w, ActorUuid: a, Uuid_2: in.SessionID, Resource: resource})
	if e != nil {
		return uc.StoredPage{}, mapped(e)
	}
	if row.Expired {
		return uc.StoredPage{}, expired()
	}
	var after int64
	if in.After != "" {
		cur, err := uc.DecodeCursor(in.After)
		if err != nil || cur.Workspace != w || cur.Actor != a || cur.Session != row.Uuid || cur.Resource != resource || cur.Fingerprint != row.Fingerprint {
			return uc.StoredPage{}, invalidCursor()
		}
		after, _ = strconv.ParseInt(cur.Ordinal, 10, 64)
		exists, err := q.SearchItemAnchorExists(c, db.SearchItemAnchorExistsParams{SessionID: row.ID, WorkspaceID: row.WorkspaceID, Ordinal: after})
		if err != nil {
			return uc.StoredPage{}, err
		}
		if !exists {
			return uc.StoredPage{}, invalidCursor()
		}
	}
	rows, e := q.ListSearchItems(c, db.ListSearchItemsParams{SessionID: row.ID, WorkspaceID: row.WorkspaceID, AfterOrdinal: after, RowLimit: in.Limit + 1})
	if e != nil {
		return uc.StoredPage{}, e
	}
	out := uc.StoredPage{Session: uc.Session{ID: row.Uuid, RequestID: row.RequestUuid, WorkspaceID: w, Resource: resource, AsOf: row.AsOf.Time, ExpiresAt: row.ExpiresAt.Time, TotalCount: strconv.FormatInt(row.TotalCount, 10)}, Rows: make([][]byte, 0)}
	if e = json.Unmarshal(row.Query, &out.Query); e != nil {
		return out, e
	}
	if len(rows) > int(in.Limit) {
		out.HasMore = true
		rows = rows[:in.Limit]
	}
	for _, v := range rows {
		out.Rows = append(out.Rows, v.Snapshot)
	}
	if out.HasMore {
		out.NextCursor = uc.EncodeCursor(uc.Cursor{Workspace: w, Actor: a, Session: row.Uuid, Resource: resource, Fingerprint: row.Fingerprint, Ordinal: strconv.FormatInt(rows[len(rows)-1].Ordinal, 10)})
	}
	// The shared header lock fences cleanup until commit. Crossing expiry is an
	// explicit failure, never an empty/truncated successful page.
	dead, e := q.SearchSessionExpired(c, row.ID)
	if e != nil {
		return uc.StoredPage{}, e
	}
	if dead {
		return uc.StoredPage{}, expired()
	}
	return out, nil
}
