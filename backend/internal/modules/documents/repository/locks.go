package repository

import (
	"context"
	"github.com/furkanmeclis/traccia/backend/internal/database/db"
	uc "github.com/furkanmeclis/traccia/backend/internal/modules/documents/usecase"
	issue "github.com/furkanmeclis/traccia/backend/internal/modules/issues/usecase"
	"github.com/google/uuid"
	"sort"
)

func sortedIDs(ids map[uuid.UUID]bool) []uuid.UUID {
	out := make([]uuid.UUID, 0, len(ids))
	for id := range ids {
		if id != uuid.Nil {
			out = append(out, id)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].String() < out[j].String() })
	return out
}

// Include owners and reference targets in the same phases. Explicit locks also
// cover the implicit KEY SHARE locks acquired by scoped reference foreign keys.
func (r *Postgres) lockGraph(ctx context.Context, w uuid.UUID, owner db.DocumentOwner, content []uc.Node, rich *issue.CreateInput) error {
	projects, issues := map[uuid.UUID]bool{}, map[uuid.UUID]bool{}
	switch owner.Kind {
	case "project":
		projects[owner.Uuid] = true
	case "issue":
		issues[owner.Uuid] = true
	}
	for _, ref := range uc.References(content) {
		switch ref.TargetKind {
		case "project":
			projects[ref.TargetID] = true
		case "issue":
			issues[ref.TargetID] = true
		}
	}
	if rich != nil && rich.ProjectID != nil {
		id, _ := uuid.Parse(*rich.ProjectID)
		projects[id] = true
	}
	q := r.queries(ctx)
	for _, id := range sortedIDs(projects) {
		if _, e := q.LockProject(ctx, db.LockProjectParams{Uuid: w, Uuid_2: id}); e != nil {
			return mapped(e)
		}
	}
	if rich != nil {
		if _, e := q.LockTeam(ctx, db.LockTeamParams{Uuid: w, Uuid_2: rich.TeamID}); e != nil {
			return mapped(e)
		}
	}
	for _, id := range sortedIDs(issues) {
		if _, e := q.LockIssue(ctx, db.LockIssueParams{Uuid: w, Uuid_2: id}); e != nil {
			return mapped(e)
		}
	}
	return nil
}
func (r *Postgres) PrepareRich(ctx context.Context, w uuid.UUID, in issue.CreateInput, content []uc.Node) error {
	if e := r.lockGraph(ctx, w, db.DocumentOwner{}, content, &in); e != nil {
		return e
	}
	return r.validateReferences(ctx, w, content)
}
