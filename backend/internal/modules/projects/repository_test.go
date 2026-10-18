package projects_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/furkanmeclis/traccia/backend/internal/database/db"
	project "github.com/furkanmeclis/traccia/backend/internal/modules/projects/usecase"
	team "github.com/furkanmeclis/traccia/backend/internal/modules/teams/usecase"
	ws "github.com/furkanmeclis/traccia/backend/internal/modules/workspaces/usecase"
	"github.com/furkanmeclis/traccia/backend/internal/platform/dbtx"
	"github.com/furkanmeclis/traccia/backend/internal/platform/operations"
	"github.com/furkanmeclis/traccia/backend/internal/platform/outbox"
	"github.com/google/uuid"
)

func TestProjectLifecycleReceiptsHistoryAndProjection(t *testing.T) {
	f := newProjectFixture(t)
	a := f.createTeam(t, "AA")
	b := f.createTeam(t, "BB")
	p := f.createProject(t, a.ID, b.ID)
	raw, e := f.call("project_milestones.create", uuid.NewString(), f.w, project.CreateMilestoneInput{ProjectID: p.ID, Name: "İlk sürüm 🚀", Description: "Scope", ExpectedProjectVersion: 1})
	m := decodeResult[project.MilestoneOutput](t, raw, e)
	key := uuid.NewString()
	input := project.CreateResourceInput{ProjectID: p.ID, Kind: "document", Title: "Design source", URL: "https://docs.example.test/source", Summary: "Architecture notes", Provider: "Docs", Reference: "source-v1", ExpectedProjectVersion: 2}
	const n = 8
	var wg sync.WaitGroup
	outputs := make(chan []byte, n)
	errs := make(chan error, n)
	for range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r, e := f.call("project_resources.create", key, f.w, input)
			if e != nil {
				errs <- e
			} else {
				outputs <- r
			}
		}()
	}
	wg.Wait()
	close(errs)
	close(outputs)
	for e := range errs {
		t.Fatal(e)
	}
	var first []byte
	for r := range outputs {
		if first == nil {
			first = r
		} else if !bytes.Equal(first, r) {
			t.Fatal("receipt replay changed output")
		}
	}
	resource := decodeResult[project.ResourceOutput](t, first, nil)
	input.Title = "Changed"
	_, e = f.call("project_resources.create", key, f.w, input)
	checkCode(t, e, operations.IdempotencyConflict)
	_, e = f.call("project_milestones.update", uuid.NewString(), f.w, project.UpdateMilestoneInput{ProjectID: p.ID, MilestoneID: m.Milestone.ID, Name: "Wrong CAS", ExpectedVersion: 99, ExpectedProjectVersion: 3})
	checkCode(t, e, operations.VersionConflict)
	raw, e = f.call("projects.get", "", f.w, project.ProjectInput{ProjectID: p.ID})
	current := decodeResult[project.ProjectOutput](t, raw, e)
	if current.Project.Version != 3 {
		t.Fatal("failed child CAS bumped aggregate")
	}
	raw, e = f.call("projects.archive", uuid.NewString(), f.w, project.ArchiveProjectInput{ProjectID: p.ID, ExpectedVersion: 3})
	archived := decodeResult[project.ProjectOutput](t, raw, e)
	if archived.Capabilities.Manage || !archived.Capabilities.Restore {
		t.Fatal("project restore capability missing")
	}
	_, e = f.call("project_resources.archive", uuid.NewString(), f.w, project.ArchiveResourceInput{ProjectID: p.ID, ResourceID: resource.Resource.ID, ExpectedVersion: 1, ExpectedProjectVersion: 4})
	checkCode(t, e, operations.Conflict)
	raw, e = f.call("projects.restore", uuid.NewString(), f.w, project.ArchiveProjectInput{ProjectID: p.ID, ExpectedVersion: 4})
	restored := decodeResult[project.ProjectOutput](t, raw, e)
	if restored.Project.Version != 5 || !restored.Capabilities.Manage || restored.Capabilities.Restore {
		t.Fatal("project restore state wrong")
	}
	raw, e = f.call("project_milestones.archive", uuid.NewString(), f.w, project.ArchiveMilestoneInput{ProjectID: p.ID, MilestoneID: m.Milestone.ID, ExpectedVersion: 1, ExpectedProjectVersion: 5})
	ma := decodeResult[project.MilestoneOutput](t, raw, e)
	if !ma.Capabilities.Restore || ma.ProjectVersion != 6 {
		t.Fatal("milestone restore unavailable")
	}
	raw, e = f.call("project_milestones.list", "", f.w, project.ProjectPageInput{ProjectID: p.ID, Limit: 1, Archived: true})
	list := decodeResult[project.MilestoneList](t, raw, e)
	if list.Capabilities.Manage || !list.Capabilities.Restore || len(list.Items) != 1 {
		t.Fatal("archived child list capabilities wrong")
	}
	raw, e = f.call("project_milestones.restore", uuid.NewString(), f.w, project.ArchiveMilestoneInput{ProjectID: p.ID, MilestoneID: m.Milestone.ID, ExpectedVersion: 2, ExpectedProjectVersion: 6})
	mr := decodeResult[project.MilestoneOutput](t, raw, e)
	if mr.Milestone.Version != 3 || mr.ProjectVersion != 7 {
		t.Fatal("restore version wrong")
	}
	raw, e = f.call("project_teams.set", uuid.NewString(), f.w, project.SetTeamsInput{ProjectID: p.ID, TeamIDs: []string{b.ID.String()}, ExpectedProjectVersion: 7})
	decodeResult[project.ProjectVersionOutput](t, raw, e)
	tx, e := f.pool.Begin(f.ctx)
	if e != nil {
		t.Fatal(e)
	}
	if e = f.repo.ValidateAssignment(dbtx.WithTx(f.ctx, tx), f.w, b.ID, p.ID, m.Milestone.ID); e != nil {
		t.Fatal(e)
	}
	_ = tx.Rollback(f.ctx)
	tx, _ = f.pool.Begin(f.ctx)
	e = f.repo.ValidateAssignment(dbtx.WithTx(f.ctx, tx), f.w, a.ID, p.ID, m.Milestone.ID)
	checkCode(t, e, operations.Validation)
	_ = tx.Rollback(f.ctx)
	var removed int
	if e = f.pool.QueryRow(f.ctx, "SELECT count(*) FROM project_teams WHERE archived").Scan(&removed); e != nil || removed != 1 {
		t.Fatal("contributor history removed", e)
	}
	var resources int
	if e = f.pool.QueryRow(f.ctx, "SELECT count(*) FROM project_resources").Scan(&resources); e != nil || resources != 1 {
		t.Fatal("duplicate resource", e)
	}
	for _, table := range []string{"workspace_history", "outbox_events", "event_deliveries", "operation_receipts"} {
		var count int
		if e = f.pool.QueryRow(f.ctx, "SELECT count(*) FROM "+table).Scan(&count); e != nil || count != 11 {
			t.Fatalf("%s count=%d %v", table, count, e)
		}
	}
	var snapshot string
	if e = f.pool.QueryRow(f.ctx, "SELECT payload->'Payload'->'resource'->>'url' FROM outbox_events WHERE event_name='project_resources.created'").Scan(&snapshot); e != nil || snapshot != input.URL {
		t.Fatal("resource snapshot missing", e)
	}
	consumers, e := f.workspaceRepo.DurableConsumers()
	if e != nil {
		t.Fatal(e)
	}
	worker := outbox.NewWorker(outbox.NewStore(f.pool, db.New(f.pool)), consumers, nil)
	for range 11 {
		if _, e = worker.Drain(f.ctx); e != nil {
			t.Fatal(e)
		}
	}
	raw, e = f.call("activity.list", "", f.w, ws.ListInput{Limit: 100})
	activity := decodeResult[ws.ActivityList](t, raw, e)
	if len(activity.Items) != 11 || activity.Items[0].ProjectID != p.ID.String() || activity.Items[0].ResourceType != "project_teams" {
		t.Fatalf("project activity %s", raw)
	}
	// A canonical history failure rolls back parent CAS, resource and receipt.
	if _, e = f.pool.Exec(f.ctx, "CREATE FUNCTION reject_project_history() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.event_name='project_resources.created' THEN RAISE EXCEPTION 'synthetic history failure'; END IF; RETURN NEW; END $$; CREATE TRIGGER reject_project_history BEFORE INSERT ON workspace_history FOR EACH ROW EXECUTE FUNCTION reject_project_history()"); e != nil {
		t.Fatal(e)
	}
	input.Title = "Rollback"
	input.ExpectedProjectVersion = 8
	_, e = f.call("project_resources.create", uuid.NewString(), f.w, input)
	if e == nil {
		t.Fatal("history failure ignored")
	}
	raw, e = f.call("projects.get", "", f.w, project.ProjectInput{ProjectID: p.ID})
	after := decodeResult[project.ProjectOutput](t, raw, e)
	if after.Project.Version != 8 {
		t.Fatal("failed history changed project version")
	}
	for _, table := range []string{"workspace_history", "outbox_events", "event_deliveries", "operation_receipts"} {
		var count int
		if e = f.pool.QueryRow(f.ctx, "SELECT count(*) FROM "+table).Scan(&count); e != nil || count != 11 {
			t.Fatalf("rollback %s count=%d %v", table, count, e)
		}
	}
}

func TestProjectCompositeOwnershipAndAssignment(t *testing.T) {
	f := newProjectFixture(t)
	a := f.createTeam(t, "AA")
	p := f.createProject(t, a.ID)
	q := f.createProject(t, a.ID)
	raw, e := f.call("project_milestones.create", uuid.NewString(), f.w, project.CreateMilestoneInput{ProjectID: p.ID, Name: "Owned milestone", ExpectedProjectVersion: 1})
	m := decodeResult[project.MilestoneOutput](t, raw, e)
	tx, _ := f.pool.Begin(f.ctx)
	e = f.repo.ValidateAssignment(dbtx.WithTx(f.ctx, tx), f.w, a.ID, q.ID, m.Milestone.ID)
	checkCode(t, e, operations.NotFound)
	_ = tx.Rollback(f.ctx)
	tx, _ = f.pool.Begin(f.ctx)
	e = f.repo.ValidateAssignment(dbtx.WithTx(f.ctx, tx), f.w, a.ID, uuid.Nil, m.Milestone.ID)
	checkCode(t, e, operations.Validation)
	_ = tx.Rollback(f.ctx)
	_, e = f.call("project_milestones.update", uuid.NewString(), f.w, project.UpdateMilestoneInput{ProjectID: q.ID, MilestoneID: m.Milestone.ID, Name: "Wrong owner", ExpectedVersion: 1, ExpectedProjectVersion: 1})
	checkCode(t, e, operations.NotFound)
	raw, e = f.call("workspaces.create", uuid.NewString(), uuid.Nil, ws.CreateInput{Name: "Other workspace"})
	other := decodeResult[ws.WorkspaceOutput](t, raw, e)
	var wid, pid, tid, otherID int64
	_ = f.pool.QueryRow(f.ctx, "SELECT workspace_id,id FROM projects WHERE uuid=$1", p.ID).Scan(&wid, &pid)
	_ = f.pool.QueryRow(f.ctx, "SELECT id FROM teams WHERE uuid=$1", a.ID).Scan(&tid)
	_ = f.pool.QueryRow(f.ctx, "SELECT id FROM workspaces WHERE uuid=$1", other.Workspace.ID).Scan(&otherID)
	foreignLead := uuid.New()
	if _, e = f.pool.Exec(f.ctx, "INSERT INTO users(uuid,email,password_hash,name,surname,status) VALUES($1,$2,'unused','Foreign','Lead','active')", foreignLead, foreignLead.String()+"@example.test"); e != nil {
		t.Fatal(e)
	}
	if _, e = f.pool.Exec(f.ctx, "INSERT INTO workspace_members(workspace_id,user_id,role) SELECT $1,id,'member' FROM users WHERE uuid=$2", wid, foreignLead); e != nil {
		t.Fatal(e)
	}
	var otherProjectID int64
	_ = f.pool.QueryRow(f.ctx, "SELECT id FROM projects WHERE uuid=$1", q.ID).Scan(&otherProjectID)
	queries := []struct {
		sql  string
		args []any
	}{
		{"INSERT INTO project_teams(workspace_id,project_id,team_id) VALUES($1,$2,$3)", []any{otherID, pid, tid}},
		{"INSERT INTO project_milestones(workspace_id,project_id,name) VALUES($1,$2,'foreign')", []any{otherID, pid}},
		{"INSERT INTO project_resources(workspace_id,project_id,kind,title,url) VALUES($1,$2,'link','foreign','https://example.test')", []any{otherID, pid}},
		{"INSERT INTO projects(workspace_id,name,lead_user_id) SELECT $1,'foreign',user_id FROM workspace_members WHERE workspace_id=$2 AND user_id=(SELECT id FROM users WHERE uuid=$3)", []any{otherID, wid, foreignLead}},
		{"UPDATE projects SET workspace_id=$1 WHERE id=$2", []any{otherID, pid}},
		{"UPDATE project_teams SET project_id=$1 WHERE workspace_id=$2 AND project_id=$3", []any{otherProjectID, wid, pid}},
	}
	for _, query := range queries {
		if _, e = f.pool.Exec(f.ctx, query.sql, query.args...); e == nil {
			t.Fatal("database accepted foreign identity", query.sql)
		}
	}
	// Future issue tables can bind all three references by composite foreign keys.
	_, e = f.pool.Exec(f.ctx, "CREATE TABLE issue_assignment_probe (workspace_id BIGINT NOT NULL,project_id BIGINT NOT NULL,team_id BIGINT NOT NULL,milestone_id BIGINT,FOREIGN KEY(workspace_id,project_id,team_id) REFERENCES project_teams(workspace_id,project_id,team_id), FOREIGN KEY(workspace_id,project_id,milestone_id) REFERENCES project_milestones(workspace_id,project_id,id))")
	if e != nil {
		t.Fatal(e)
	}
	var mid, qid int64
	_ = f.pool.QueryRow(f.ctx, "SELECT id FROM project_milestones WHERE uuid=$1", m.Milestone.ID).Scan(&mid)
	_ = f.pool.QueryRow(f.ctx, "SELECT id FROM projects WHERE uuid=$1", q.ID).Scan(&qid)
	if _, e = f.pool.Exec(f.ctx, "INSERT INTO issue_assignment_probe VALUES($1,$2,$3,$4)", wid, pid, tid, mid); e != nil {
		t.Fatal(e)
	}
	if _, e = f.pool.Exec(f.ctx, "INSERT INTO issue_assignment_probe VALUES($1,$2,$3,$4)", wid, qid, tid, mid); e == nil {
		t.Fatal("future issue FK accepted wrong milestone project")
	}
	_, e = f.call("projects.get", "", other.Workspace.ID, project.ProjectInput{ProjectID: p.ID})
	checkCode(t, e, operations.NotFound)
	_, e = f.call("projects.create", uuid.NewString(), other.Workspace.ID, project.CreateProjectInput{Name: "Foreign team", TeamIDs: []string{a.ID.String()}})
	checkCode(t, e, operations.NotFound)
}

func TestProjectConcurrentAggregateCAS(t *testing.T) {
	f := newProjectFixture(t)
	a := f.createTeam(t, "AA")
	p := f.createProject(t, a.ID)
	const n = 12
	var wg sync.WaitGroup
	errs := make(chan error, n)
	for i := range n {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, e := f.call("project_milestones.create", uuid.NewString(), f.w, project.CreateMilestoneInput{ProjectID: p.ID, Name: fmt.Sprintf("Milestone %d", i), ExpectedProjectVersion: 1})
			errs <- e
		}(i)
	}
	wg.Wait()
	close(errs)
	success, conflicts := 0, 0
	for e := range errs {
		if e == nil {
			success++
		} else {
			checkCode(t, e, operations.VersionConflict)
			conflicts++
		}
	}
	if success != 1 || conflicts != n-1 {
		t.Fatalf("success=%d conflicts=%d", success, conflicts)
	}
	var count, version int
	if e := f.pool.QueryRow(f.ctx, "SELECT count(*) FROM project_milestones").Scan(&count); e != nil || count != 1 {
		t.Fatal("concurrent milestone count", e)
	}
	if e := f.pool.QueryRow(f.ctx, "SELECT version FROM projects WHERE uuid=$1", p.ID).Scan(&version); e != nil || version != 2 {
		t.Fatal("aggregate increment mismatch", e)
	}
}

func TestProjectLeadRetentionAndReferenceLocks(t *testing.T) {
	f := newProjectFixture(t)
	a := f.createTeam(t, "AA")
	b := f.createTeam(t, "BB")
	lead := f.actor.String()
	raw, e := f.call("projects.create", uuid.NewString(), f.w, project.CreateProjectInput{Name: "Lead selection", LeadUserID: &lead, TeamIDs: []string{a.ID.String()}})
	p := decodeResult[project.ProjectOutput](t, raw, e).Project
	// Deactivate a separate lead without revoking the acting owner.
	candidate := uuid.New()
	_, e = f.pool.Exec(f.ctx, "INSERT INTO users(uuid,email,password_hash,name,surname,status) VALUES($1,$2,'unused','Candidate','Member','active');", candidate, candidate.String()+"@example.test")
	if e != nil {
		t.Fatal(e)
	}
	_, e = f.pool.Exec(f.ctx, "INSERT INTO workspace_members(workspace_id,user_id,role) SELECT w.id,u.id,'member' FROM workspaces w,users u WHERE w.uuid=$1 AND u.uuid=$2", f.w, candidate)
	if e != nil {
		t.Fatal(e)
	}
	c := candidate.String()
	raw, e = f.call("projects.update", uuid.NewString(), f.w, project.UpdateProjectInput{ProjectID: p.ID, Name: p.Name, LeadUserID: &c, ExpectedVersion: 1})
	decodeResult[project.ProjectOutput](t, raw, e)
	_, e = f.pool.Exec(f.ctx, "UPDATE workspace_members SET active=false WHERE user_id=(SELECT id FROM users WHERE uuid=$1)", candidate)
	if e != nil {
		t.Fatal(e)
	}
	equivalentLead := strings.ToUpper(c)
	raw, e = f.call("projects.update", uuid.NewString(), f.w, project.UpdateProjectInput{ProjectID: p.ID, Name: "Unrelated rename", LeadUserID: &equivalentLead, ExpectedVersion: 2})
	retained := decodeResult[project.ProjectOutput](t, raw, e)
	if retained.Project.LeadUserID == nil || *retained.Project.LeadUserID != c {
		t.Fatal("inactive lead lost")
	}
	_, e = f.call("projects.create", uuid.NewString(), f.w, project.CreateProjectInput{Name: "Inactive lead denied", LeadUserID: &c, TeamIDs: []string{a.ID.String()}})
	checkCode(t, e, operations.NotFound)
	_, e = f.call("teams.archive", uuid.NewString(), f.w, team.ArchiveTeamInput{TeamID: a.ID, ExpectedVersion: 1})
	if e != nil {
		t.Fatal(e)
	}
	raw, e = f.call("project_teams.set", uuid.NewString(), f.w, project.SetTeamsInput{ProjectID: p.ID, TeamIDs: []string{a.ID.String(), b.ID.String()}, ExpectedProjectVersion: 3})
	decodeResult[project.ProjectVersionOutput](t, raw, e)
	_, e = f.call("projects.create", uuid.NewString(), f.w, project.CreateProjectInput{Name: "Archived team denied", TeamIDs: []string{a.ID.String()}})
	checkCode(t, e, operations.Conflict)
	// The resolver holds project then team locks through the surrounding issue tx.
	tx, e := f.pool.Begin(f.ctx)
	if e != nil {
		t.Fatal(e)
	}
	if e = f.repo.ValidateAssignment(dbtx.WithTx(f.ctx, tx), f.w, b.ID, p.ID, uuid.Nil); e != nil {
		t.Fatal(e)
	}
	lockedCtx, cancel := context.WithTimeout(f.ctx, 150*time.Millisecond)
	_, e = f.pool.Exec(lockedCtx, "UPDATE teams SET archived=true WHERE uuid=$1", b.ID)
	cancel()
	if e == nil {
		t.Fatal("team archive bypassed assignment lock")
	}
	_ = tx.Rollback(f.ctx)
	// Newly selected member rows are share-locked against deactivation.
	tx, e = f.pool.Begin(f.ctx)
	if e != nil {
		t.Fatal(e)
	}
	q := db.New(tx)
	var wid int64
	_ = tx.QueryRow(f.ctx, "SELECT id FROM workspaces WHERE uuid=$1", f.w).Scan(&wid)
	if _, e = q.LockProjectLead(f.ctx, db.LockProjectLeadParams{WorkspaceID: wid, Uuid: f.actor}); e != nil {
		t.Fatal(e)
	}
	lockedCtx, cancel = context.WithTimeout(f.ctx, 150*time.Millisecond)
	_, e = f.pool.Exec(lockedCtx, "UPDATE workspace_members SET active=false WHERE workspace_id=$1 AND user_id=(SELECT id FROM users WHERE uuid=$2)", wid, f.actor)
	cancel()
	if e == nil {
		t.Fatal("lead deactivation bypassed member lock")
	}
	_ = tx.Rollback(f.ctx)
}

func TestProjectScopedPaginationAndDates(t *testing.T) {
	f := newProjectFixture(t)
	a := f.createTeam(t, "AA")
	p := f.createProject(t, a.ID)
	other := f.createProject(t, a.ID)
	_, e := f.pool.Exec(f.ctx, "INSERT INTO project_milestones(workspace_id,project_id,name,target_date) SELECT workspace_id,id,'Milestone '||n,'2026-03-29'::date FROM projects CROSS JOIN generate_series(1,125) n WHERE uuid=$1", p.ID)
	if e != nil {
		t.Fatal(e)
	}
	after := ""
	seen := map[uuid.UUID]bool{}
	for {
		raw, e := f.call("project_milestones.list", "", f.w, project.ProjectPageInput{ProjectID: p.ID, Limit: 20, After: after})
		out := decodeResult[project.MilestoneList](t, raw, e)
		if out.ProjectVersion != 1 {
			t.Fatal("snapshot version")
		}
		for _, m := range out.Items {
			if seen[m.ID] || m.TargetDate == nil || *m.TargetDate != "2026-03-29" {
				t.Fatal("duplicate or timezone date corruption")
			}
			seen[m.ID] = true
		}
		if !out.HasMore {
			break
		}
		if out.NextCursor == after || out.NextCursor == "" {
			t.Fatal("cursor did not advance")
		}
		after = out.NextCursor
	}
	if len(seen) != 125 {
		t.Fatal("incomplete child pagination", len(seen))
	}
	raw, e := f.call("project_milestones.list", "", f.w, project.ProjectPageInput{ProjectID: other.ID, Limit: 20, After: after})
	foreign := decodeResult[project.MilestoneList](t, raw, e)
	if len(foreign.Items) != 0 {
		t.Fatal("foreign project cursor crossed scope")
	}
	_, e = f.pool.Exec(f.ctx, "INSERT INTO users(email,password_hash,name,surname,status) SELECT 'page-'||n||'@example.test','unused',CASE WHEN n=1 THEN '' ELSE 'Page '||n END,'Member','active' FROM generate_series(1,125) n")
	if e != nil {
		t.Fatal(e)
	}
	_, e = f.pool.Exec(f.ctx, "INSERT INTO workspace_members(workspace_id,user_id,role) SELECT w.id,u.id,'member' FROM workspaces w,users u WHERE w.uuid=$1 AND u.email LIKE 'page-%'", f.w)
	if e != nil {
		t.Fatal(e)
	}
	count := 0
	after = ""
	for {
		raw, e := f.call("workspace_members.list", "", f.w, project.MemberPageInput{Limit: 20, After: after})
		var fields map[string]json.RawMessage
		if e != nil {
			t.Fatal(e)
		}
		_ = json.Unmarshal(raw, &fields)
		out := decodeResult[project.MemberList](t, raw, e)
		count += len(out.Items)
		for _, m := range out.Items {
			if !m.Assignable || !m.Active {
				t.Fatal("member capabilities")
			}
		}
		if !out.HasMore {
			break
		}
		after = out.NextCursor
	}
	if count != 126 {
		t.Fatal("incomplete member discovery", count)
	}
	start, target := "2026-03-29", "2026-03-28"
	_, e = f.call("projects.update", uuid.NewString(), f.w, project.UpdateProjectInput{ProjectID: p.ID, Name: p.Name, StartDate: &start, TargetDate: &target, ExpectedVersion: 1})
	checkCode(t, e, operations.Validation)
	_, e = f.pool.Exec(f.ctx, "UPDATE projects SET start_date='2026-03-29',target_date='2026-03-28' WHERE uuid=$1", p.ID)
	if e == nil {
		t.Fatal("database date order check missing")
	}
	// The ambient transaction is mandatory for reference validation.
	if e = f.repo.ValidateAssignment(f.ctx, f.w, a.ID, p.ID, uuid.Nil); e == nil {
		t.Fatal("assignment accepted outside transaction")
	}
}

func TestProjectSnapshotVersionConsistency(t *testing.T) {
	f := newProjectFixture(t)
	a := f.createTeam(t, "AA")
	p := f.createProject(t, a.ID)
	raw, e := f.call("project_milestones.create", uuid.NewString(), f.w, project.CreateMilestoneInput{ProjectID: p.ID, Name: "Snapshot", ExpectedProjectVersion: 1})
	initial := decodeResult[project.MilestoneOutput](t, raw, e)
	failures := make(chan error, 1)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := int64(1); i <= 30; i++ {
			_, e := f.call("project_milestones.update", uuid.NewString(), f.w, project.UpdateMilestoneInput{ProjectID: p.ID, MilestoneID: initial.Milestone.ID, Name: "Snapshot", ExpectedVersion: i, ExpectedProjectVersion: i + 1})
			if e != nil {
				failures <- e
				return
			}
		}
	}()
	for range 80 {
		raw, e := f.call("project_milestones.list", "", f.w, project.ProjectPageInput{ProjectID: p.ID, Limit: 10})
		out := decodeResult[project.MilestoneList](t, raw, e)
		if len(out.Items) != 1 || out.Items[0].Version+1 != out.ProjectVersion {
			t.Fatalf("mixed snapshot %s", raw)
		}
	}
	<-done
	select {
	case e := <-failures:
		t.Fatal(e)
	default:
	}
}
