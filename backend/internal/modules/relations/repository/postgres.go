package repository

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/furkanmeclis/traccia/backend/internal/database/db"
	uc "github.com/furkanmeclis/traccia/backend/internal/modules/relations/usecase"
	"github.com/furkanmeclis/traccia/backend/internal/platform/dbtx"
	"github.com/furkanmeclis/traccia/backend/internal/platform/operations"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"sort"
)

type Postgres struct {
	pool *pgxpool.Pool
	q    *db.Queries
}

func New(p *pgxpool.Pool, q *db.Queries) *Postgres { return &Postgres{p, q} }
func (r *Postgres) queries(ctx context.Context) *db.Queries {
	if tx, ok := dbtx.Tx(ctx); ok {
		return r.q.WithTx(tx)
	}
	return r.q
}
func failure(code operations.Code) error { return &operations.Error{Code: code} }
func mapped(e error) error {
	if errors.Is(e, pgx.ErrNoRows) {
		return failure(operations.NotFound)
	}
	var p *pgconn.PgError
	if errors.As(e, &p) {
		switch p.Code {
		case "23505":
			return failure(operations.Conflict)
		case "23503", "23514":
			return failure(operations.Validation)
		}
	}
	return e
}
func decode[T any](raw []byte, e error) (T, error) {
	var out T
	if e != nil {
		return out, mapped(e)
	}
	if len(raw) == 0 || string(raw) == "null" {
		return out, failure(operations.NotFound)
	}
	e = json.Unmarshal(raw, &out)
	return out, e
}
func (r *Postgres) ref(ctx context.Context, w, id uuid.UUID) (uc.IssueReference, error) {
	raw, e := r.queries(ctx).ReadRelationIssue(ctx, db.ReadRelationIssueParams{WorkspaceUuid: w, Uuid: id})
	return decode[uc.IssueReference](raw, e)
}
func (r *Postgres) state(ctx context.Context, w, id uuid.UUID) (uc.State, error) {
	raw, e := r.queries(ctx).ReadRelationState(ctx, db.ReadRelationStateParams{WorkspaceUuid: w, IssueUuid: id})
	return decode[uc.State](raw, e)
}
func sorted(ids ...uuid.UUID) []uuid.UUID {
	seen := map[uuid.UUID]bool{}
	out := []uuid.UUID{}
	for _, id := range ids {
		if id != uuid.Nil && !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].String() < out[j].String() })
	return out
}

// Serialize the graph predicate before taking any issue locks. This module
// never acquires project/team locks after issue locks.
func (r *Postgres) Mutate(ctx context.Context, w, actor uuid.UUID, in uc.Mutation) (uc.MutationOutput, error) {
	out := uc.MutationOutput{ChangedRelations: []uc.Relation{}, AffectedIssues: []uc.IssueReference{}}
	if _, ok := dbtx.Tx(ctx); !ok {
		return out, errors.New("relationship mutation requires operation transaction")
	}
	q := r.queries(ctx)
	if e := q.LockIssueRelationGraph(ctx, w); e != nil {
		return out, e
	}
	source, e := q.ReadRelationIdentity(ctx, db.ReadRelationIdentityParams{WorkspaceUuid: w, Uuid: in.IssueID})
	if e != nil {
		return out, mapped(e)
	}
	var previous db.IssueRelation
	var oldTarget uuid.UUID
	if in.Set {
		previous, e = q.ReadOutgoingSingleRelation(ctx, db.ReadOutgoingSingleRelationParams{WorkspaceID: source.WorkspaceID, SourceIssueID: source.ID, Kind: in.Kind})
		if e != nil && !errors.Is(e, pgx.ErrNoRows) {
			return out, e
		}
		if e == nil {
			oldTarget, e = q.RelationIssueUUID(ctx, db.RelationIssueUUIDParams{WorkspaceID: source.WorkspaceID, ID: previous.TargetIssueID})
			if e != nil {
				return out, e
			}
		}
	}
	affected := sorted(in.IssueID, in.TargetIssueID, oldTarget)
	expected := map[uuid.UUID]int64{}
	for _, v := range in.ExpectedVersions {
		expected[v.IssueID] = v.ExpectedVersion
	}
	if len(expected) != len(affected) {
		return out, failure(operations.Validation)
	}
	rows := map[uuid.UUID]db.Issue{}
	for _, id := range affected {
		row, err := q.LockIssue(ctx, db.LockIssueParams{Uuid: w, Uuid_2: id})
		if err != nil {
			return out, mapped(err)
		}
		rows[id] = row
		if expected[id] < 1 {
			return out, failure(operations.Validation)
		}
		if err = q.EnsureIssueRelationRevision(ctx, db.EnsureIssueRelationRevisionParams{WorkspaceID: row.WorkspaceID, IssueID: row.ID}); err != nil {
			return out, err
		}
		version, err := q.LockIssueRelationRevision(ctx, db.LockIssueRelationRevisionParams{WorkspaceID: row.WorkspaceID, IssueID: row.ID})
		if err != nil {
			return out, err
		}
		if version != expected[id] {
			return out, failure(operations.VersionConflict)
		}
	}
	if in.Set && oldTarget == in.TargetIssueID {
		return out, failure(operations.Conflict)
	}
	adding := !in.Remove && in.TargetIssueID != uuid.Nil
	if adding {
		for _, id := range []uuid.UUID{in.IssueID, in.TargetIssueID} {
			ref, err := r.ref(ctx, w, id)
			if err != nil {
				return out, err
			}
			if !ref.Active {
				return out, failure(operations.Conflict)
			}
		}
	}
	changed := []uuid.UUID{}
	if in.Set && previous.ID != 0 {
		if e = q.RemoveIssueRelation(ctx, db.RemoveIssueRelationParams{WorkspaceID: source.WorkspaceID, Uuid: previous.Uuid}); e != nil {
			return out, e
		}
		changed = append(changed, previous.Uuid)
	}
	src, dst := rows[in.IssueID].ID, rows[in.TargetIssueID].ID
	if in.Kind == "related" && src > dst {
		src, dst = dst, src
	}
	if in.Remove {
		edge, err := q.ReadActiveIssueRelation(ctx, db.ReadActiveIssueRelationParams{WorkspaceID: source.WorkspaceID, SourceIssueID: src, TargetIssueID: dst, Kind: in.Kind})
		if err != nil {
			return out, mapped(err)
		}
		if e = q.RemoveIssueRelation(ctx, db.RemoveIssueRelationParams{WorkspaceID: source.WorkspaceID, Uuid: edge.Uuid}); e != nil {
			return out, e
		}
		changed = append(changed, edge.Uuid)
	} else if adding {
		if in.Kind != "related" {
			cycle, err := q.IssueRelationWouldCycle(ctx, db.IssueRelationWouldCycleParams{WorkspaceID: source.WorkspaceID, SourceIssueID: src, TargetIssueID: dst, Kind: in.Kind})
			if err != nil {
				return out, err
			}
			if cycle {
				return out, failure(operations.Conflict)
			}
		}
		edge, err := q.InsertIssueRelation(ctx, db.InsertIssueRelationParams{WorkspaceID: source.WorkspaceID, SourceIssueID: src, TargetIssueID: dst, Kind: in.Kind})
		if err != nil {
			return out, mapped(err)
		}
		changed = append(changed, edge.Uuid)
	}
	for _, id := range affected {
		row := rows[id]
		if e = q.BumpIssueRelationRevision(ctx, db.BumpIssueRelationRevisionParams{WorkspaceID: row.WorkspaceID, IssueID: row.ID}); e != nil {
			return out, e
		}
		ref, err := r.ref(ctx, w, id)
		if err != nil {
			return out, err
		}
		out.AffectedIssues = append(out.AffectedIssues, ref)
	}
	for _, id := range changed {
		raw, err := q.ReadIssueRelation(ctx, db.ReadIssueRelationParams{WorkspaceUuid: w, Uuid: id})
		edge, err := decode[uc.Relation](raw, err)
		if err != nil {
			return out, err
		}
		out.ChangedRelations = append(out.ChangedRelations, edge)
	}
	out.State, e = r.state(ctx, w, in.IssueID)
	if e != nil {
		return out, e
	}
	event := map[string]string{"parent": "parent_set", "duplicate": "duplicate_set", "blocks": "block_linked", "related": "related_linked"}[in.Kind]
	if in.Remove {
		event = map[string]string{"blocks": "block_unlinked", "related": "related_unlinked"}[in.Kind]
	}
	for _, ref := range out.AffectedIssues {
		snapshot := out
		snapshot.State, e = r.state(ctx, w, ref.ID)
		if e != nil {
			return out, e
		}
		if e = r.record(ctx, w, actor, "issue_relations."+event, rows[ref.ID], ref, snapshot); e != nil {
			return out, e
		}
	}
	return out, nil
}
func (r *Postgres) List(ctx context.Context, w uuid.UUID, in uc.PageInput) (uc.List, error) {
	var out uc.List
	if _, e := r.ref(ctx, w, in.IssueID); e != nil {
		return out, e
	}
	anchor, e := uc.CursorAnchor(w, in, in.After)
	if e != nil {
		return out, e
	}
	q := r.queries(ctx)
	if anchor != uuid.Nil {
		ok, err := q.RelationAnchorExists(ctx, db.RelationAnchorExistsParams{WorkspaceUuid: w, Uuid: anchor, SourceUuid: in.IssueID})
		if err != nil {
			return out, err
		}
		if !ok {
			return out, failure(operations.Validation)
		}
	}
	// State, recursive duplicate chain and all page references share one statement snapshot.
	raw, e := q.ListIssueRelations(ctx, db.ListIssueRelationsParams{WorkspaceUuid: w, IssueUuid: in.IssueID, Kind: in.Kind, Direction: in.Direction, Removed: in.Removed, AfterUuid: anchor, RowLimit: in.Limit + 1})
	return decode[uc.List](raw, e)
}
func (r *Postgres) History(ctx context.Context, w uuid.UUID, in uc.HistoryInput) ([]uc.History, error) {
	out := []uc.History{}
	if _, e := r.ref(ctx, w, in.IssueID); e != nil {
		return out, e
	}
	anchor, e := uc.CursorAnchor(w, in, in.After)
	if e != nil {
		return out, e
	}
	q := r.queries(ctx)
	if anchor != uuid.Nil {
		ok, err := q.IssueRelationHistoryAnchorExists(ctx, db.IssueRelationHistoryAnchorExistsParams{Uuid: w, Uuid_2: in.IssueID, Uuid_3: anchor})
		if err != nil {
			return out, err
		}
		if !ok {
			return out, failure(operations.Validation)
		}
	}
	rows, e := q.ListIssueRelationHistory(ctx, db.ListIssueRelationHistoryParams{WorkspaceUuid: w, IssueUuid: in.IssueID, AfterUuid: anchor, RowLimit: in.Limit + 1})
	if e != nil {
		return out, e
	}
	for _, row := range rows {
		var snapshot uc.MutationOutput
		if e = json.Unmarshal(row.Snapshot, &snapshot); e != nil {
			return out, e
		}
		out = append(out, uc.History{ID: row.Uuid, WorkspaceID: row.WorkspaceUuid, IssueID: row.IssueUuid, ActorID: row.ActorUuid, EventName: row.EventName, Version: row.Version, OccurredAt: row.OccurredAt.Time, Snapshot: snapshot})
	}
	return out, nil
}
