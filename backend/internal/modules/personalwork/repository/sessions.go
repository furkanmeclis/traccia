package repository

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/furkanmeclis/traccia/backend/internal/database/db"
	uc "github.com/furkanmeclis/traccia/backend/internal/modules/personalwork/usecase"
	search "github.com/furkanmeclis/traccia/backend/internal/modules/searchviews/usecase"
	"github.com/furkanmeclis/traccia/backend/internal/platform/dbtx"
	"github.com/furkanmeclis/traccia/backend/internal/platform/operations"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"strconv"
)

func fingerprint(in uc.PortfolioCreateInput) (string, []byte, error) {
	q, _ := in.Definition.QueryValue()
	raw, e := json.Marshal(q)
	if e != nil {
		return "", nil, e
	}
	body, e := json.Marshal(in)
	if e != nil {
		return "", nil, e
	}
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:]), raw, nil
}
func (r *Postgres) Capture(c context.Context, w, a uuid.UUID, in uc.Capture, auth func(context.Context, []uuid.UUID, bool) error) (uc.PortfolioOutput, error) {
	tx, e := r.pool.BeginTx(c, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if e != nil {
		return uc.PortfolioOutput{}, e
	}
	defer func() { _ = tx.Rollback(c) }()
	ctx := dbtx.WithTx(c, tx)
	q := r.q.WithTx(tx)
	hash, raw, e := fingerprint(in.Input)
	if e != nil {
		return uc.PortfolioOutput{}, e
	}
	if e = q.LockPersonalRequest(ctx, "portfolio"+w.String()+a.String()+in.Input.RequestID.String()); e != nil {
		return uc.PortfolioOutput{}, e
	}
	found, e := q.FindPortfolioRequest(ctx, db.FindPortfolioRequestParams{Uuid: w, ActorUuid: a, RequestUuid: in.Input.RequestID})
	replay := e == nil
	if e != nil && !errors.Is(e, pgx.ErrNoRows) {
		return uc.PortfolioOutput{}, e
	}
	ids := in.Input.WorkspaceIDs
	if replay {
		ids = found.WorkspaceUuids
	}
	if e = auth(ctx, ids, replay); e != nil {
		return uc.PortfolioOutput{}, e
	}
	var id uuid.UUID
	if replay {
		dead, err := q.PortfolioExpired(ctx, found.ID)
		if err != nil {
			return uc.PortfolioOutput{}, err
		}
		if dead {
			return uc.PortfolioOutput{}, expired()
		}
		if found.Fingerprint != hash {
			return uc.PortfolioOutput{}, fail(operations.IdempotencyConflict)
		}
		if found.State != "ready" {
			return uc.PortfolioOutput{}, fail(operations.Conflict)
		}
		id = found.Uuid
	} else {
		row, err := q.InsertPortfolioSession(ctx, db.InsertPortfolioSessionParams{Uuid: w, ActorUuid: a, RequestUuid: in.Input.RequestID, Resource: in.Input.Definition.Resource, Fingerprint: hash, Query: raw, FirstLimit: in.Input.Limit, WorkspaceUuids: ids, AssignedToMe: in.Input.AssignedToMe, TtlSeconds: in.EffectiveTTL})
		if err != nil {
			return uc.PortfolioOutput{}, mapped(err)
		}
		captured, err := q.CapturePortfolioItems(ctx, db.CapturePortfolioItemsParams{WorkspaceUuids: ids, Query: raw, Resource: row.Resource, AssignedToMe: row.AssignedToMe, ActorUuid: a, WorkspaceID: row.WorkspaceID, SessionID: row.ID})
		if err != nil {
			return uc.PortfolioOutput{}, err
		}
		row, err = q.PublishPortfolioSession(ctx, db.PublishPortfolioSessionParams{ID: row.ID, TotalCount: captured.TotalCount, WorkspaceSnapshots: captured.WorkspaceSnapshots, AsOf: captured.AsOf, TtlSeconds: in.EffectiveTTL})
		if err != nil {
			return uc.PortfolioOutput{}, err
		}
		id = row.Uuid
	}
	out, e := r.readPage(ctx, w, a, uc.PortfolioGetInput{SessionID: id, Limit: in.Input.Limit}, auth)
	if e != nil {
		return out, e
	}
	if e = tx.Commit(ctx); e != nil {
		return uc.PortfolioOutput{}, e
	}
	return out, nil
}
func (r *Postgres) Page(c context.Context, w, a uuid.UUID, in uc.PortfolioGetInput, auth func(context.Context, []uuid.UUID, bool) error) (uc.PortfolioOutput, error) {
	tx, e := r.pool.BeginTx(c, pgx.TxOptions{})
	if e != nil {
		return uc.PortfolioOutput{}, e
	}
	defer func() { _ = tx.Rollback(c) }()
	out, e := r.readPage(dbtx.WithTx(c, tx), w, a, in, auth)
	if e != nil {
		return uc.PortfolioOutput{}, e
	}
	if e = tx.Commit(c); e != nil {
		return uc.PortfolioOutput{}, e
	}
	return out, nil
}
func (r *Postgres) readPage(ctx context.Context, w, a uuid.UUID, in uc.PortfolioGetInput, auth func(context.Context, []uuid.UUID, bool) error) (uc.PortfolioOutput, error) {
	q := r.queries(ctx)
	row, e := q.ReadPortfolioSession(ctx, db.ReadPortfolioSessionParams{Uuid: w, ActorUuid: a, Uuid_2: in.SessionID})
	if e != nil {
		return uc.PortfolioOutput{}, mapped(e)
	}
	if e = auth(ctx, row.WorkspaceUuids, true); e != nil {
		return uc.PortfolioOutput{}, e
	}
	dead, e := q.PortfolioExpired(ctx, row.ID)
	if e != nil {
		return uc.PortfolioOutput{}, e
	}
	if dead {
		return uc.PortfolioOutput{}, expired()
	}
	after := int64(0)
	if in.After != "" {
		cur, err := uc.DecodeCursor(in.After)
		if err != nil || cur.Workspace != w || cur.Actor != a || cur.Session != row.Uuid || cur.Fingerprint != row.Fingerprint {
			return uc.PortfolioOutput{}, invalidCursor()
		}
		after, err = strconv.ParseInt(cur.Ordinal, 10, 64)
		if err != nil || after < 1 {
			return uc.PortfolioOutput{}, invalidCursor()
		}
		ok, err := q.PortfolioAnchorExists(ctx, db.PortfolioAnchorExistsParams{SessionID: row.ID, WorkspaceID: row.WorkspaceID, Ordinal: after})
		if err != nil {
			return uc.PortfolioOutput{}, err
		}
		if !ok {
			return uc.PortfolioOutput{}, invalidCursor()
		}
	}
	rows, e := q.ListPortfolioItems(ctx, db.ListPortfolioItemsParams{SessionID: row.ID, WorkspaceID: row.WorkspaceID, AfterOrdinal: after, RowLimit: in.Limit + 1})
	if e != nil {
		return uc.PortfolioOutput{}, e
	}
	out := uc.PortfolioOutput{Session: uc.PortfolioSession{ID: row.Uuid, RequestID: row.RequestUuid, AnchorWorkspaceID: w, Resource: row.Resource, AsOf: row.AsOf.Time, ExpiresAt: row.ExpiresAt.Time, TotalCount: strconv.FormatInt(row.TotalCount, 10)}, AssignedToMe: row.AssignedToMe, Items: make([]uc.PortfolioRow, 0)}
	if e = json.Unmarshal(row.WorkspaceSnapshots, &out.Session.Workspaces); e != nil {
		return uc.PortfolioOutput{}, e
	}
	var query search.Query
	if e = json.Unmarshal(row.Query, &query); e != nil {
		return uc.PortfolioOutput{}, e
	}
	out.Definition.Resource = row.Resource
	switch row.Resource {
	case "issue":
		v := search.IssueQuery(query)
		out.Definition.Issue = &v
	case "project":
		v := search.ProjectQuery(query)
		out.Definition.Project = &v
	case "team":
		v := search.TeamQuery(query)
		out.Definition.Team = &v
	}
	if len(rows) > int(in.Limit) {
		out.HasMore = true
		rows = rows[:in.Limit]
	}
	for _, v := range rows {
		item := uc.PortfolioRow{WorkspaceID: v.SourceWorkspaceUuid, Resource: row.Resource}
		switch row.Resource {
		case "issue":
			item.Issue = &search.IssueRow{}
			e = json.Unmarshal(v.Snapshot, item.Issue)
		case "project":
			item.Project = &search.ProjectRow{}
			e = json.Unmarshal(v.Snapshot, item.Project)
		case "team":
			item.Team = &search.TeamRow{}
			e = json.Unmarshal(v.Snapshot, item.Team)
		}
		if e != nil {
			return uc.PortfolioOutput{}, e
		}
		out.Items = append(out.Items, item)
	}
	if out.HasMore {
		out.NextCursor = uc.EncodeCursor(uc.Cursor{Workspace: w, Actor: a, Session: row.Uuid, Fingerprint: row.Fingerprint, Ordinal: strconv.FormatInt(rows[len(rows)-1].Ordinal, 10)})
	}
	if e = auth(ctx, row.WorkspaceUuids, true); e != nil {
		return uc.PortfolioOutput{}, e
	}
	dead, e = q.PortfolioExpired(ctx, row.ID)
	if e != nil {
		return uc.PortfolioOutput{}, e
	}
	if dead {
		return uc.PortfolioOutput{}, expired()
	}
	return out, nil
}
