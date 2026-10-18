package relations_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sync"
	"testing"

	"github.com/furkanmeclis/traccia/backend/internal/database/db"
	issue "github.com/furkanmeclis/traccia/backend/internal/modules/issues/usecase"
	relation "github.com/furkanmeclis/traccia/backend/internal/modules/relations/usecase"
	team "github.com/furkanmeclis/traccia/backend/internal/modules/teams/usecase"
	ws "github.com/furkanmeclis/traccia/backend/internal/modules/workspaces/usecase"
	"github.com/furkanmeclis/traccia/backend/internal/platform/operations"
	"github.com/furkanmeclis/traccia/backend/internal/platform/outbox"
	"github.com/google/uuid"
	"time"
)

func (f relationFixture) state(t *testing.T, id uuid.UUID) relation.State {
	t.Helper()
	raw, e := f.call("issue_relations.list", "", f.w, relation.PageInput{IssueID: id, Direction: "all", Limit: 100})
	return decodeResult[relation.List](t, raw, e).State
}
func (f relationFixture) revisions(t *testing.T, ids ...uuid.UUID) []relation.ExpectedVersion {
	t.Helper()
	seen := map[uuid.UUID]bool{}
	out := []relation.ExpectedVersion{}
	for _, id := range ids {
		if id != uuid.Nil && !seen[id] {
			seen[id] = true
			out = append(out, relation.ExpectedVersion{IssueID: id, ExpectedVersion: f.state(t, id).Issue.RelationVersion})
		}
	}
	return out
}
func (f relationFixture) input(t *testing.T, kind string, source, target uuid.UUID, remove bool) (string, any) {
	t.Helper()
	state := f.state(t, source)
	old := uuid.Nil
	if kind == "parent" && state.Parent != nil {
		old = state.Parent.ID
	}
	if kind == "duplicate" && state.DuplicateOf != nil {
		old = state.DuplicateOf.ID
	}
	versions := f.revisions(t, source, target, old)
	if kind == "parent" || kind == "duplicate" {
		var ptr *string
		if target != uuid.Nil {
			s := target.String()
			ptr = &s
		}
		return "issue_relations.set_" + kind, relation.SetInput{IssueID: source, TargetIssueID: ptr, ExpectedVersions: versions}
	}
	op := map[string]string{"blocks": "link_block", "related": "link_related"}[kind]
	if remove {
		op = map[string]string{"blocks": "unlink_block", "related": "unlink_related"}[kind]
	}
	return "issue_relations." + op, relation.LinkInput{IssueID: source, TargetIssueID: target, ExpectedVersions: versions}
}
func (f relationFixture) mutate(t *testing.T, kind string, source, target uuid.UUID, remove bool) relation.MutationOutput {
	t.Helper()
	op, in := f.input(t, kind, source, target, remove)
	raw, e := f.call(op, uuid.NewString(), f.w, in)
	return decodeResult[relation.MutationOutput](t, raw, e)
}
func TestRelationsLifecycleReplacementCanonicalAndReplay(t *testing.T) {
	f := newRelationFixture(t)
	a := f.createTeam(t, "AAA")
	b := f.createTeam(t, "BBB")
	i, j, k := f.create(t, a.ID), f.create(t, a.ID), f.create(t, a.ID)
	if len(f.registry.Catalog()) != 129 {
		t.Fatal("production registry missing relationship operations")
	}
	first := f.mutate(t, "parent", i.ID, j.ID, false)
	if first.State.Parent == nil || first.State.Parent.ID != j.ID || first.State.Issue.RelationVersion != 2 {
		t.Fatal("parent state")
	}
	replaced := f.mutate(t, "parent", i.ID, k.ID, false)
	if len(replaced.ChangedRelations) != 2 || !replaced.ChangedRelations[0].Removed || replaced.ChangedRelations[0].ID != first.ChangedRelations[0].ID || replaced.State.Parent.ID != k.ID || len(replaced.AffectedIssues) != 3 {
		t.Fatal("parent replacement not atomic")
	}
	f.mutate(t, "parent", i.ID, uuid.Nil, false)
	if f.state(t, i.ID).Parent != nil {
		t.Fatal("parent clear")
	}
	f.mutate(t, "duplicate", i.ID, j.ID, false)
	f.mutate(t, "duplicate", j.ID, k.ID, false)
	state := f.state(t, i.ID)
	if state.DuplicateOf.ID != j.ID || state.CanonicalDuplicate.ID != k.ID {
		t.Fatal("duplicate chain canonical target")
	}
	// Archived references remain readable and removable. Metadata versions do not change.
	raw, e := f.call("issues.archive", "archive", f.w, issue.VersionInput{IssueID: k.ID, ExpectedVersion: 1})
	archived := decodeResult[issue.Output](t, raw, e).Issue
	if !f.state(t, i.ID).CanonicalDuplicate.Archived {
		t.Fatal("canonical archived target hidden")
	}
	f.mutate(t, "duplicate", j.ID, uuid.Nil, false)
	raw, e = f.call("issues.move", "move", f.w, issue.MoveInput{IssueID: i.ID, TeamID: b.ID, StatusID: f.status(t, b.ID), LabelIDs: []string{}, ExpectedVersion: 1})
	moved := decodeResult[issue.Output](t, raw, e).Issue
	if f.state(t, j.ID).Issue.RelationVersion < 2 || f.state(t, i.ID).DuplicateOf.ID != j.ID || f.state(t, i.ID).Issue.Identifier != moved.Identifier {
		t.Fatal("move changed relation identity")
	}
	raw, e = f.call("issues.restore", "restore", f.w, issue.VersionInput{IssueID: k.ID, ExpectedVersion: archived.Version})
	_ = decodeResult[issue.Output](t, raw, e)
	op, in := f.input(t, "related", i.ID, j.ID, false)
	key := uuid.NewString()
	raw, e = f.call(op, key, f.w, in)
	linked := decodeResult[relation.MutationOutput](t, raw, e)
	replay, e := f.call(op, key, f.w, in)
	if e != nil || !bytes.Equal(raw, replay) {
		t.Fatal("frozen receipt replay mismatch")
	}
	changed := in.(relation.LinkInput)
	changed.TargetIssueID = k.ID
	_, e = f.call(op, key, f.w, changed)
	checkCode(t, e, operations.IdempotencyConflict)
	_, e = f.call(op, uuid.NewString(), f.w, in)
	checkCode(t, e, operations.VersionConflict)
	opposite := f.state(t, j.ID)
	if opposite.Issue.ID != j.ID {
		t.Fatal("reciprocal state")
	}
	removed := f.mutate(t, "related", j.ID, i.ID, true)
	if removed.ChangedRelations[0].ID != linked.ChangedRelations[0].ID || !removed.ChangedRelations[0].Removed {
		t.Fatal("inverse unlink used different edge")
	}
	relink := f.mutate(t, "related", j.ID, i.ID, false)
	if relink.ChangedRelations[0].ID == linked.ChangedRelations[0].ID {
		t.Fatal("relink resurrected immutable edge")
	}
	raw, e = f.call("issues.get", "", f.w, issue.IssueInput{IssueID: j.ID})
	if decodeResult[issue.Output](t, raw, e).Issue.Version != 1 {
		t.Fatal("relations modified metadata version")
	}
}
func TestRelationsCyclePredicatesAndDisjointConcurrentClosures(t *testing.T) {
	for _, kind := range []string{"parent", "blocks", "duplicate"} {
		t.Run(kind, func(t *testing.T) {
			f := newRelationFixture(t)
			a := f.createTeam(t, "AAA")
			i, j, k, l := f.create(t, a.ID), f.create(t, a.ID), f.create(t, a.ID), f.create(t, a.ID)
			f.mutate(t, kind, i.ID, j.ID, false)
			op, in := f.input(t, kind, j.ID, i.ID, false)
			_, e := f.call(op, uuid.NewString(), f.w, in)
			checkCode(t, e, operations.Conflict)
			f.mutate(t, kind, k.ID, l.ID, false)
			op1, in1 := f.input(t, kind, j.ID, k.ID, false)
			op2, in2 := f.input(t, kind, l.ID, i.ID, false)
			var wg sync.WaitGroup
			errs := make(chan error, 2)
			for _, c := range []struct {
				op string
				in any
			}{{op1, in1}, {op2, in2}} {
				wg.Add(1)
				go func() { defer wg.Done(); _, e := f.call(c.op, uuid.NewString(), f.w, c.in); errs <- e }()
			}
			wg.Wait()
			close(errs)
			success := 0
			for e := range errs {
				if e == nil {
					success++
				} else {
					checkCode(t, e, operations.Conflict)
				}
			}
			if success != 1 {
				t.Fatalf("disjoint graph closure successes=%d", success)
			}
		})
	}
}
func TestRelationsAtomicHistoryOutboxRevisionsAndProjection(t *testing.T) {
	f := newRelationFixture(t)
	a := f.createTeam(t, "AAA")
	i, j := f.create(t, a.ID), f.create(t, a.ID)
	for _, table := range []string{"issue_relation_history", "outbox_events"} {
		t.Run(table, func(t *testing.T) {
			if _, e := f.pool.Exec(f.ctx, fmt.Sprintf("CREATE FUNCTION fail_relation_write() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'forced relation failure'; END $$; CREATE TRIGGER fail_relation_write BEFORE INSERT ON %s FOR EACH ROW EXECUTE FUNCTION fail_relation_write()", table)); e != nil {
				t.Fatal(e)
			}
			op, in := f.input(t, "blocks", i.ID, j.ID, false)
			_, e := f.call(op, uuid.NewString(), f.w, in)
			if e == nil {
				t.Fatal("forced failure ignored")
			}
			if _, e = f.pool.Exec(f.ctx, fmt.Sprintf("DROP TRIGGER fail_relation_write ON %s; DROP FUNCTION fail_relation_write()", table)); e != nil {
				t.Fatal(e)
			}
			if f.state(t, i.ID).Issue.RelationVersion != 1 || f.state(t, j.ID).Issue.RelationVersion != 1 {
				t.Fatal("rollback leaked revisions")
			}
			var n int
			_ = f.pool.QueryRow(f.ctx, "SELECT count(*) FROM issue_relations").Scan(&n)
			if n != 0 {
				t.Fatal("rollback leaked edge")
			}
		})
	}
	f.mutate(t, "blocks", i.ID, j.ID, false)
	store := outbox.NewStore(f.pool, db.New(f.pool))
	consumers, e := f.workspaceRepo.DurableConsumers()
	if e != nil {
		t.Fatal(e)
	}
	claims, e := store.Claim(f.ctx, 100, time.Minute)
	if e != nil {
		t.Fatal(e)
	}
	for _, claim := range claims {
		if e = store.Process(f.ctx, claim, consumers); e != nil {
			t.Fatal(e)
		}
	}
	rows, e := f.workspaceRepo.ListActivity(f.ctx, f.w, uuid.Nil, 100)
	if e != nil {
		t.Fatal(e)
	}
	projected := 0
	for _, row := range rows {
		if row.EventName == "issue_relations.block_linked" {
			projected++
			if row.IssueID == "" || row.Identifier == "" || row.Version != 2 {
				t.Fatal("projection missing reciprocal issue context")
			}
		}
	}
	if projected != 2 {
		t.Fatalf("projected=%d", projected)
	}
	var event, snapshot []byte
	e = f.pool.QueryRow(f.ctx, "SELECT o.payload,h.snapshot FROM issue_relation_history h JOIN outbox_events o ON o.id=h.event_id LIMIT 1").Scan(&event, &snapshot)
	if e != nil || !json.Valid(event) || !json.Valid(snapshot) {
		t.Fatal("canonical snapshot missing")
	}
	for _, q := range []string{"UPDATE issue_relation_history SET version=version+1", "UPDATE issue_relations SET target_issue_id=source_issue_id", "DELETE FROM issue_relations"} {
		if _, e = f.pool.Exec(f.ctx, q); e == nil {
			t.Fatal("immutable relation/history accepted change")
		}
	}
}
func TestRelationsCrossWorkspaceReferencesAndConstraints(t *testing.T) {
	f := newRelationFixture(t)
	a := f.createTeam(t, "AAA")
	i, j := f.create(t, a.ID), f.create(t, a.ID)
	raw, e := f.call("workspaces.create", "foreign", uuid.Nil, ws.CreateInput{Name: "Foreign"})
	foreign := decodeResult[ws.WorkspaceOutput](t, raw, e).Workspace.ID
	raw, e = f.call("teams.create", "foreign-team", foreign, team.CreateTeamInput{Name: "Foreign", Code: "FOR"})
	ta := decodeResult[team.TeamOutput](t, raw, e).Team
	raw, e = f.call("workflow_statuses.list", "", foreign, team.StatusListInput{TeamID: ta.ID, Limit: 100})
	status := decodeResult[team.StatusList](t, raw, e).Items[0].ID
	raw, e = f.call("issues.create", "foreign-issue", foreign, issue.CreateInput{TeamID: ta.ID, Title: "Foreign issue", Priority: "none", StatusID: status, LabelIDs: []string{}})
	other := decodeResult[issue.Output](t, raw, e).Issue
	for _, id := range []uuid.UUID{other.ID, uuid.New()} {
		in := relation.LinkInput{IssueID: i.ID, TargetIssueID: id, ExpectedVersions: []relation.ExpectedVersion{{IssueID: i.ID, ExpectedVersion: 1}, {IssueID: id, ExpectedVersion: 1}}}
		_, e = f.call("issue_relations.link_block", uuid.NewString(), f.w, in)
		checkCode(t, e, operations.NotFound)
	}
	if _, e = f.pool.Exec(f.ctx, "INSERT INTO issue_relations(workspace_id,source_issue_id,target_issue_id,kind) SELECT a.workspace_id,a.id,b.id,'blocks' FROM issues a CROSS JOIN issues b WHERE a.uuid=$1 AND b.uuid=$2", i.ID, other.ID); e == nil {
		t.Fatal("cross workspace edge FK accepted")
	}
	if _, e = f.pool.Exec(f.ctx, "INSERT INTO issue_relations(workspace_id,source_issue_id,target_issue_id,kind) SELECT workspace_id,id,id,'blocks' FROM issues WHERE uuid=$1", i.ID); e == nil {
		t.Fatal("self edge accepted")
	}
	f.mutate(t, "related", i.ID, j.ID, false)
	if _, e = f.pool.Exec(f.ctx, "INSERT INTO issue_relations(workspace_id,source_issue_id,target_issue_id,kind) SELECT workspace_id,target_issue_id,source_issue_id,'related' FROM issue_relations LIMIT 1"); e == nil {
		t.Fatal("noncanonical related pair accepted")
	}
}
func TestRelationsCompletePaginationHistoryAndRemovedAnchors(t *testing.T) {
	f := newRelationFixture(t)
	a, b := f.createTeam(t, "AAA"), f.createTeam(t, "BBB")
	source := f.create(t, a.ID)
	for n := 0; n < 127; n++ {
		target := f.create(t, a.ID)
		f.mutate(t, "blocks", source.ID, target.ID, false)
	}
	in := relation.PageInput{IssueID: source.ID, Kind: "blocks", Direction: "outgoing", Limit: 100}
	raw, e := f.call("issue_relations.list", "", f.w, in)
	first := decodeResult[relation.List](t, raw, e)
	if len(first.Items) != 100 || !first.HasMore {
		t.Fatal("incomplete first relation page")
	}
	anchor := first.Items[99]
	f.mutate(t, "blocks", source.ID, anchor.Target.ID, true)
	raw, e = f.call("issues.move", "anchor-move", f.w, issue.MoveInput{IssueID: anchor.Target.ID, TeamID: b.ID, StatusID: f.status(t, b.ID), LabelIDs: []string{}, ExpectedVersion: 1})
	moved := decodeResult[issue.Output](t, raw, e).Issue
	raw, e = f.call("issues.archive", "anchor-archive", f.w, issue.VersionInput{IssueID: moved.ID, ExpectedVersion: moved.Version})
	_ = decodeResult[issue.Output](t, raw, e)
	in.After = first.NextCursor
	raw, e = f.call("issue_relations.list", "", f.w, in)
	tail := decodeResult[relation.List](t, raw, e)
	if len(tail.Items) != 27 || tail.HasMore || tail.NextCursor != "" {
		t.Fatal("removed/moved/archived relation anchor lost page")
	}
	seen := map[uuid.UUID]bool{}
	for _, items := range [][]relation.Relation{first.Items, tail.Items} {
		for _, item := range items {
			if seen[item.ID] {
				t.Fatal("duplicate relation page row")
			}
			seen[item.ID] = true
		}
	}
	if len(seen) != 127 {
		t.Fatal("incomplete traversal")
	}
	in.After = ""
	in.Removed = true
	raw, e = f.call("issue_relations.list", "", f.w, in)
	removed := decodeResult[relation.List](t, raw, e)
	if len(removed.Items) != 1 || removed.Items[0].ID != anchor.ID || !removed.Items[0].Target.Archived || removed.Items[0].Target.Identifier != moved.Identifier {
		t.Fatal("tombstone did not retain stable reference")
	}
	historyIn := relation.HistoryInput{IssueID: source.ID, Limit: 100}
	raw, e = f.call("issue_relations.history", "", f.w, historyIn)
	history := decodeResult[relation.HistoryList](t, raw, e)
	if len(history.Items) != 100 || !history.HasMore {
		t.Fatal("history first page")
	}
	historyIn.After = history.NextCursor
	raw, e = f.call("issue_relations.history", "", f.w, historyIn)
	historyTail := decodeResult[relation.HistoryList](t, raw, e)
	if len(historyTail.Items) != 28 || historyTail.HasMore || historyTail.NextCursor != "" {
		t.Fatal("history traversal incomplete")
	}
	for n, h := range append(history.Items, historyTail.Items...) {
		if h.Version != int64(n+2) || h.ActorID != f.actor || h.IssueID != source.ID || h.Snapshot.State.Issue.RelationVersion != h.Version {
			t.Fatal("history canonical aggregate version")
		}
	}
}
func TestRelationsThreeNodeCyclesAndInverseRelatedConcurrency(t *testing.T) {
	f := newRelationFixture(t)
	a := f.createTeam(t, "AAA")
	for _, kind := range []string{"parent", "blocks", "duplicate"} {
		t.Run(kind, func(t *testing.T) {
			i, j, k := f.create(t, a.ID), f.create(t, a.ID), f.create(t, a.ID)
			f.mutate(t, kind, i.ID, j.ID, false)
			f.mutate(t, kind, j.ID, k.ID, false)
			op, in := f.input(t, kind, k.ID, i.ID, false)
			_, e := f.call(op, uuid.NewString(), f.w, in)
			checkCode(t, e, operations.Conflict)
		})
	}
	i, j := f.create(t, a.ID), f.create(t, a.ID)
	op1, in1 := f.input(t, "related", i.ID, j.ID, false)
	op2, in2 := f.input(t, "related", j.ID, i.ID, false)
	errs := make(chan error, 2)
	var wg sync.WaitGroup
	for _, c := range []struct {
		op string
		in any
	}{{op1, in1}, {op2, in2}} {
		wg.Add(1)
		go func() { defer wg.Done(); _, e := f.call(c.op, uuid.NewString(), f.w, c.in); errs <- e }()
	}
	wg.Wait()
	close(errs)
	success := 0
	for e := range errs {
		if e == nil {
			success++
		} else {
			checkCode(t, e, operations.VersionConflict)
		}
	}
	if success != 1 {
		t.Fatal("inverse concurrent related writes created duplicate edge")
	}
	for _, id := range []uuid.UUID{i.ID, j.ID} {
		raw, e := f.call("issue_relations.list", "", f.w, relation.PageInput{IssueID: id, Kind: "related", Direction: "incoming", Limit: 100})
		if len(decodeResult[relation.List](t, raw, e).Items) != 1 {
			t.Fatal("related inverse page inconsistent")
		}
	}
}
