package issues_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/furkanmeclis/traccia/backend/internal/database/db"
	issue "github.com/furkanmeclis/traccia/backend/internal/modules/issues/usecase"
	project "github.com/furkanmeclis/traccia/backend/internal/modules/projects/usecase"
	team "github.com/furkanmeclis/traccia/backend/internal/modules/teams/usecase"
	ws "github.com/furkanmeclis/traccia/backend/internal/modules/workspaces/usecase"
	"github.com/furkanmeclis/traccia/backend/internal/platform/dbtx"
	"github.com/furkanmeclis/traccia/backend/internal/platform/operations"
	"github.com/furkanmeclis/traccia/backend/internal/platform/outbox"
	"github.com/google/uuid"
	"strings"
	"sync"
	"testing"
	"time"
)

func (f issueFixture) status(t *testing.T, teamID uuid.UUID) uuid.UUID {
	t.Helper()
	raw, e := f.call("workflow_statuses.list", "", f.w, team.StatusListInput{TeamID: teamID, Limit: 100})
	out := decodeResult[team.StatusList](t, raw, e)
	if len(out.Items) == 0 {
		t.Fatal("missing seeded status")
	}
	return out.Items[0].ID
}
func (f issueFixture) create(t *testing.T, teamID uuid.UUID) issue.Issue {
	t.Helper()
	raw, e := f.call("issues.create", uuid.NewString(), f.w, issue.CreateInput{TeamID: teamID, Title: "İstanbul issue 🧭", Priority: "normal", StatusID: f.status(t, teamID), LabelIDs: []string{}})
	return decodeResult[issue.Output](t, raw, e).Issue
}
func metadata(i issue.Issue) issue.UpdateInput {
	return issue.UpdateInput{IssueID: i.ID, ExpectedVersion: i.Version, Title: i.Title, Priority: i.Priority, StatusID: i.StatusID, AssigneeID: i.AssigneeID, ProjectID: i.ProjectID, MilestoneID: i.MilestoneID, StartDate: i.StartDate, TargetDate: i.TargetDate, Effort: i.Effort, LabelIDs: i.LabelIDs}
}
func TestIssueLifecycleAliasesHistoryAndCAS(t *testing.T) {
	f := newIssueFixture(t)
	a := f.createTeam(t, "AAA")
	b := f.createTeam(t, "BBB")
	i := f.create(t, a.ID)
	original := i.Identifier
	for _, target := range []uuid.UUID{b.ID, a.ID} {
		raw, e := f.call("issues.move", uuid.NewString(), f.w, issue.MoveInput{IssueID: i.ID, TeamID: target, StatusID: f.status(t, target), LabelIDs: []string{}, ExpectedVersion: i.Version})
		i = decodeResult[issue.Output](t, raw, e).Issue
	}
	if i.ID == uuid.Nil || i.Identifier == original || i.Number != 2 {
		t.Fatalf("move back did not allocate fresh alias: %+v", i)
	}
	raw, e := f.call("issues.resolve", "", f.w, issue.ResolveInput{Identifier: original})
	resolved := decodeResult[issue.Output](t, raw, e)
	if resolved.Issue.ID != i.ID || resolved.Issue.Identifier != i.Identifier {
		t.Fatal("alias changed stable identity")
	}
	in := metadata(i)
	in.Title = "Edited"
	raw, e = f.call("issues.update", uuid.NewString(), f.w, in)
	updated := decodeResult[issue.Output](t, raw, e).Issue
	_, e = f.call("issues.update", uuid.NewString(), f.w, in)
	checkCode(t, e, operations.VersionConflict)
	raw, e = f.call("issues.archive", uuid.NewString(), f.w, issue.VersionInput{IssueID: i.ID, ExpectedVersion: updated.Version})
	archived := decodeResult[issue.Output](t, raw, e)
	if archived.Capabilities.Manage || !archived.Capabilities.Restore {
		t.Fatal("archive caps")
	}
	raw, e = f.call("issues.restore", uuid.NewString(), f.w, issue.VersionInput{IssueID: i.ID, ExpectedVersion: archived.Issue.Version})
	restored := decodeResult[issue.Output](t, raw, e).Issue
	raw, e = f.call("issues.history", "", f.w, issue.HistoryInput{IssueID: i.ID, Limit: 2})
	h := decodeResult[issue.HistoryList](t, raw, e)
	if !h.HasMore || len(h.Items) != 2 || h.Items[0].Issue.Identifier != original || h.Items[0].Issue.Title != i.Title {
		t.Fatalf("history not immutable %+v", h)
	}
	count := len(h.Items)
	for h.HasMore {
		raw, e = f.call("issues.history", "", f.w, issue.HistoryInput{IssueID: i.ID, Limit: 2, After: h.NextCursor})
		h = decodeResult[issue.HistoryList](t, raw, e)
		count += len(h.Items)
	}
	if int64(count) != restored.Version {
		t.Fatalf("history %d version %d", count, restored.Version)
	}
	_, e = f.pool.Exec(f.ctx, "UPDATE issue_aliases SET number=number+10")
	if e == nil {
		t.Fatal("alias mutable")
	}
	_, e = f.pool.Exec(f.ctx, "UPDATE issue_history SET version=version+10")
	if e == nil {
		t.Fatal("history mutable")
	}
}
func TestIssueConcurrentCreateReplayAndAtomicRollback(t *testing.T) {
	f := newIssueFixture(t)
	a := f.createTeam(t, "AAA")
	in := issue.CreateInput{TeamID: a.ID, Title: "Concurrent", Priority: "none", StatusID: f.status(t, a.ID), LabelIDs: []string{}}
	key := uuid.NewString()
	var wg sync.WaitGroup
	results := make(chan []byte, 12)
	errs := make(chan error, 12)
	for range 12 {
		wg.Add(1)
		go func() { defer wg.Done(); raw, e := f.call("issues.create", key, f.w, in); results <- raw; errs <- e }()
	}
	wg.Wait()
	close(results)
	close(errs)
	for e := range errs {
		if e != nil {
			t.Fatal(e)
		}
	}
	var first []byte
	for raw := range results {
		if first == nil {
			first = raw
		}
		if !bytes.Equal(first, raw) {
			t.Fatal("replay mismatch")
		}
	}
	changed := in
	changed.Title = "Changed"
	_, e := f.call("issues.create", key, f.w, changed)
	checkCode(t, e, operations.IdempotencyConflict)
	for _, table := range []string{"issue_history", "outbox_events"} {
		t.Run(table, func(t *testing.T) {
			_, e = f.pool.Exec(f.ctx, fmt.Sprintf("CREATE FUNCTION fail_issue_write() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'forced issue failure'; END $$; CREATE TRIGGER fail_issue_write BEFORE INSERT ON %s FOR EACH ROW EXECUTE FUNCTION fail_issue_write()", table))
			if e != nil {
				t.Fatal(e)
			}
			_, e = f.call("issues.create", uuid.NewString(), f.w, in)
			if e == nil {
				t.Fatal("forced failure ignored")
			}
			_, e = f.pool.Exec(f.ctx, fmt.Sprintf("DROP TRIGGER fail_issue_write ON %s; DROP FUNCTION fail_issue_write()", table))
			if e != nil {
				t.Fatal(e)
			}
		})
	}
	var count int
	_ = f.pool.QueryRow(f.ctx, "SELECT count(*) FROM issues").Scan(&count)
	if count != 1 {
		t.Fatalf("rollback leaked %d issues", count)
	}
	next := f.create(t, a.ID)
	if next.Number != 2 {
		t.Fatalf("rollback consumed number %d", next.Number)
	}
}
func TestIssueOpposingMovesAndConcurrentCAS(t *testing.T) {
	f := newIssueFixture(t)
	a := f.createTeam(t, "AAA")
	b := f.createTeam(t, "BBB")
	ia := f.create(t, a.ID)
	ib := f.create(t, b.ID)
	sa, sb := f.status(t, a.ID), f.status(t, b.ID)
	errs := make(chan error, 2)
	var wg sync.WaitGroup
	for _, in := range []issue.MoveInput{{IssueID: ia.ID, TeamID: b.ID, StatusID: sb, LabelIDs: []string{}, ExpectedVersion: 1}, {IssueID: ib.ID, TeamID: a.ID, StatusID: sa, LabelIDs: []string{}, ExpectedVersion: 1}} {
		wg.Add(1)
		go func() { defer wg.Done(); _, e := f.call("issues.move", uuid.NewString(), f.w, in); errs <- e }()
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		if e != nil {
			t.Fatal(e)
		}
	}
	raw, e := f.call("issues.get", "", f.w, issue.IssueInput{IssueID: ia.ID})
	i := decodeResult[issue.Output](t, raw, e).Issue
	in := metadata(i)
	errs = make(chan error, 8)
	for range 8 {
		wg.Add(1)
		go func() { defer wg.Done(); _, e := f.call("issues.update", uuid.NewString(), f.w, in); errs <- e }()
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
		t.Fatalf("CAS successes %d", success)
	}
}
func TestIssuePaginationMutableAnchorsAndNullableFilters(t *testing.T) {
	f := newIssueFixture(t)
	a := f.createTeam(t, "AAA")
	b := f.createTeam(t, "BBB")
	for range 7 {
		f.create(t, a.ID)
	}
	in := issue.PageInput{Limit: 2, TeamID: strings.ToUpper(a.ID.String()), AssigneeID: "unassigned"}
	raw, e := f.call("issues.list", "", f.w, in)
	page := decodeResult[issue.List](t, raw, e)
	if !page.HasMore || len(page.Items) != 2 {
		t.Fatal("uppercase team/null filter")
	}
	anchor := page.Items[1]
	_, e = f.call("issues.move", uuid.NewString(), f.w, issue.MoveInput{IssueID: anchor.ID, TeamID: b.ID, StatusID: f.status(t, b.ID), LabelIDs: []string{}, ExpectedVersion: anchor.Version})
	if e != nil {
		t.Fatal(e)
	}
	seen := len(page.Items)
	for page.HasMore {
		in.After = page.NextCursor
		raw, e = f.call("issues.list", "", f.w, in)
		page = decodeResult[issue.List](t, raw, e)
		seen += len(page.Items)
	}
	if seen != 7 {
		t.Fatalf("moved anchor truncated page: %d", seen)
	}
	in.After = ""
	in.TeamID = ""
	raw, e = f.call("issues.list", "", f.w, in)
	page = decodeResult[issue.List](t, raw, e)
	anchor = page.Items[1]
	_, e = f.call("issues.archive", uuid.NewString(), f.w, issue.VersionInput{IssueID: anchor.ID, ExpectedVersion: anchor.Version})
	if e != nil {
		t.Fatal(e)
	}
	in.After = page.NextCursor
	raw, e = f.call("issues.list", "", f.w, in)
	page = decodeResult[issue.List](t, raw, e)
	if len(page.Items) != 2 {
		t.Fatal("archived anchor truncated")
	}
}
func TestIssueValidationAndCrossWorkspaceBoundary(t *testing.T) {
	f := newIssueFixture(t)
	a := f.createTeam(t, "AAA")
	i := f.create(t, a.ID)
	in := metadata(i)
	for _, title := range []string{"", " x", "line\nline", "line\u2028line", strings.Repeat("界", 256)} {
		in.Title = title
		_, e := f.call("issues.update", uuid.NewString(), f.w, in)
		checkCode(t, e, operations.Validation)
	}
	raw, e := f.call("workspaces.create", uuid.NewString(), uuid.Nil, map[string]any{"name": "Other"})
	var out struct {
		Workspace struct {
			ID uuid.UUID `json:"id"`
		} `json:"workspace"`
	}
	if e != nil {
		t.Fatal(e)
	}
	_ = json.Unmarshal(raw, &out)
	_, e = f.call("issues.get", "", out.Workspace.ID, issue.IssueInput{IssueID: i.ID})
	checkCode(t, e, operations.NotFound)
	_, e = f.call("issues.resolve", "", out.Workspace.ID, issue.ResolveInput{Identifier: i.Identifier})
	checkCode(t, e, operations.NotFound)
	_, e = f.pool.Exec(f.ctx, "UPDATE issues SET team_id=(SELECT t.id FROM teams t JOIN workspaces w ON w.id=t.workspace_id WHERE w.uuid=$1 LIMIT 1) WHERE uuid=$2", out.Workspace.ID, i.ID)
	if e == nil {
		t.Fatal("cross workspace team fk accepted")
	}
}
func TestIssueRetainedHistoricalReferencesAndStrictMove(t *testing.T) {
	f := newIssueFixture(t)
	a := f.createTeam(t, "AAA")
	b := f.createTeam(t, "BBB")
	p := f.createProject(t, a.ID, b.ID)
	pid := p.ID.String()
	raw, e := f.call("project_milestones.create", uuid.NewString(), f.w, project.CreateMilestoneInput{ProjectID: p.ID, Name: "Milestone", ExpectedProjectVersion: p.Version})
	m := decodeResult[project.MilestoneOutput](t, raw, e).Milestone
	mid := m.ID.String()
	raw, e = f.call("labels.create", uuid.NewString(), f.w, team.CreateLabelInput{Name: "Shared", Color: "#111111"})
	label := decodeResult[team.LabelOutput](t, raw, e).Label
	in := issue.CreateInput{TeamID: a.ID, Title: "Retain history", Priority: "high", StatusID: f.status(t, a.ID), ProjectID: &pid, MilestoneID: &mid, LabelIDs: []string{label.ID.String()}}
	raw, e = f.call("issues.create", uuid.NewString(), f.w, in)
	i := decodeResult[issue.Output](t, raw, e).Issue
	for _, query := range []string{"UPDATE projects SET archived=true", "UPDATE project_milestones SET archived=true", "UPDATE project_teams SET archived=true", "UPDATE labels SET archived=true", "UPDATE workflow_statuses SET archived=true WHERE uuid=$1"} {
		var args []any
		if strings.Contains(query, "$1") {
			args = []any{i.StatusID}
		}
		if _, e = f.pool.Exec(f.ctx, query, args...); e != nil {
			t.Fatal(e)
		}
	}
	update := metadata(i)
	update.Title = "Keep inactive historical refs"
	raw, e = f.call("issues.update", uuid.NewString(), f.w, update)
	i = decodeResult[issue.Output](t, raw, e).Issue
	_, e = f.call("issues.create", uuid.NewString(), f.w, in)
	checkCode(t, e, operations.Conflict)
	raw, e = f.call("issues.archive", uuid.NewString(), f.w, issue.VersionInput{IssueID: i.ID, ExpectedVersion: i.Version})
	i = decodeResult[issue.Output](t, raw, e).Issue
	raw, e = f.call("issues.restore", uuid.NewString(), f.w, issue.VersionInput{IssueID: i.ID, ExpectedVersion: i.Version})
	i = decodeResult[issue.Output](t, raw, e).Issue
	if i.ProjectID == nil || i.MilestoneID == nil || *i.ProjectID != pid || *i.MilestoneID != mid {
		t.Fatal("restore discarded inactive historical refs")
	}
	move := issue.MoveInput{IssueID: i.ID, TeamID: b.ID, StatusID: f.status(t, b.ID), ProjectID: &pid, MilestoneID: &mid, LabelIDs: []string{label.ID.String()}, ExpectedVersion: i.Version}
	_, e = f.call("issues.move", uuid.NewString(), f.w, move)
	checkCode(t, e, operations.Conflict)
	move.ProjectID = nil
	move.MilestoneID = nil
	raw, e = f.call("issues.move", uuid.NewString(), f.w, move)
	i = decodeResult[issue.Output](t, raw, e).Issue
	if len(i.LabelIDs) != 1 {
		t.Fatal("retained shared label lost")
	}
	raw, e = f.call("issues.archive", uuid.NewString(), f.w, issue.VersionInput{IssueID: i.ID, ExpectedVersion: i.Version})
	i = decodeResult[issue.Output](t, raw, e).Issue
	raw, e = f.call("issues.restore", uuid.NewString(), f.w, issue.VersionInput{IssueID: i.ID, ExpectedVersion: i.Version})
	i = decodeResult[issue.Output](t, raw, e).Issue
	if _, e = f.pool.Exec(f.ctx, "UPDATE teams SET archived=true WHERE uuid=$1", b.ID); e != nil {
		t.Fatal(e)
	}
	raw, e = f.call("issues.get", "", f.w, issue.IssueInput{IssueID: i.ID})
	out := decodeResult[issue.Output](t, raw, e)
	if out.Capabilities.Manage || out.Capabilities.Restore {
		t.Fatal("inactive parent caps")
	}
	_, e = f.call("issues.update", uuid.NewString(), f.w, metadata(i))
	checkCode(t, e, operations.Conflict)
	if !out.Capabilities.Move {
		t.Fatal("archived source move capability missing")
	}
	raw, e = f.call("issues.move", uuid.NewString(), f.w, issue.MoveInput{IssueID: i.ID, TeamID: a.ID, StatusID: in.StatusID, LabelIDs: []string{}, ExpectedVersion: i.Version})
	checkCode(t, e, operations.Conflict) // archived target status rejects a new reference
	_ = raw
	_, e = f.pool.Exec(f.ctx, "UPDATE workflow_statuses SET archived=false WHERE uuid=$1", in.StatusID)
	if e != nil {
		t.Fatal(e)
	}
	raw, e = f.call("issues.move", uuid.NewString(), f.w, issue.MoveInput{IssueID: i.ID, TeamID: a.ID, StatusID: in.StatusID, LabelIDs: []string{}, ExpectedVersion: i.Version})
	moved := decodeResult[issue.Output](t, raw, e)
	if !moved.Capabilities.Manage {
		t.Fatal("move out of archived source failed")
	}
}
func TestIssueMembershipRetentionAndForeignKeys(t *testing.T) {
	f := newIssueFixture(t)
	a := f.createTeam(t, "AAA")
	p := f.createProject(t, a.ID)
	other := f.createProject(t, a.ID)
	pid := p.ID.String()
	raw, e := f.call("project_milestones.create", uuid.NewString(), f.w, project.CreateMilestoneInput{ProjectID: p.ID, Name: "Milestone", ExpectedProjectVersion: p.Version})
	m := decodeResult[project.MilestoneOutput](t, raw, e).Milestone
	mid := m.ID.String()
	assignee := f.actor.String()
	in := issue.CreateInput{TeamID: a.ID, Title: "Assignee", Priority: "none", StatusID: f.status(t, a.ID), AssigneeID: &assignee, ProjectID: &pid, MilestoneID: &mid, LabelIDs: []string{}}
	raw, e = f.call("issues.create", uuid.NewString(), f.w, in)
	i := decodeResult[issue.Output](t, raw, e).Issue
	// User deactivation does not revoke the still-active principal's workspace membership resolver.
	_, e = f.pool.Exec(f.ctx, "UPDATE workspace_members SET active=false WHERE user_id=(SELECT id FROM users WHERE uuid=$1)", f.actor)
	if e != nil {
		t.Fatal(e)
	}
	// Exercise the repository under an ambient transaction; transport correctly rejects inactive actors.
	tx, e := f.pool.Begin(f.ctx)
	if e != nil {
		t.Fatal(e)
	}
	ctx := dbtx.WithTx(f.ctx, tx)
	updated, e := f.repo.Update(ctx, f.w, metadata(i))
	if e != nil {
		t.Fatal(e)
	}
	if updated.AssigneeID == nil {
		t.Fatal("historical assignee lost")
	}
	_ = tx.Rollback(f.ctx)
	_, e = f.pool.Exec(f.ctx, "UPDATE workspace_members SET active=true WHERE user_id=(SELECT id FROM users WHERE uuid=$1)", f.actor)
	if e != nil {
		t.Fatal(e)
	}
	_, e = f.pool.Exec(f.ctx, "UPDATE issues SET project_id=(SELECT id FROM projects WHERE uuid=$1) WHERE uuid=$2", other.ID, i.ID)
	if e == nil {
		t.Fatal("cross project milestone accepted")
	}
	_, e = f.pool.Exec(f.ctx, "UPDATE issues SET project_id=NULL WHERE uuid=$1", i.ID)
	if e == nil {
		t.Fatal("milestone without project accepted")
	}
	// Resolve target from a real second workspace; tenant FKs must reject it.
	raw, e = f.call("workspaces.create", uuid.NewString(), uuid.Nil, map[string]any{"name": "Other FK"})
	w := decodeResult[ws.WorkspaceOutput](t, raw, e).Workspace.ID
	raw, e = f.call("teams.create", uuid.NewString(), w, team.CreateTeamInput{Name: "Foreign", Code: "FOR"})
	foreign := decodeResult[team.TeamOutput](t, raw, e).Team
	_, e = f.pool.Exec(f.ctx, "UPDATE issues SET team_id=(SELECT id FROM teams WHERE uuid=$1) WHERE uuid=$2", foreign.ID, i.ID)
	if e == nil {
		t.Fatal("foreign team accepted")
	}
	raw, e = f.call("labels.create", uuid.NewString(), w, team.CreateLabelInput{Name: "Foreign label", Color: "#111111"})
	l := decodeResult[team.LabelOutput](t, raw, e).Label
	_, e = f.pool.Exec(f.ctx, "INSERT INTO issue_labels(workspace_id,issue_id,label_id) SELECT i.workspace_id,i.id,l.id FROM issues i CROSS JOIN labels l WHERE i.uuid=$1 AND l.uuid=$2", i.ID, l.ID)
	if e == nil {
		t.Fatal("foreign label accepted")
	}
}
func TestIssueDurableActivityProjectionAndCanonicalSnapshots(t *testing.T) {
	f := newIssueFixture(t)
	a := f.createTeam(t, "AAA")
	i := f.create(t, a.ID)
	in := metadata(i)
	in.Title = "Second version"
	raw, e := f.call("issues.update", uuid.NewString(), f.w, in)
	i = decodeResult[issue.Output](t, raw, e).Issue
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
	count := 0
	for _, row := range rows {
		if row.ResourceType == "issues" {
			count++
			if row.IssueID != i.ID.String() || row.Identifier != i.Identifier || row.ResourceID != i.ID {
				t.Fatalf("activity context incomplete %+v", row)
			}
		}
	}
	if count != 2 {
		t.Fatalf("issue activity count %d", count)
	}
	var snapshot []byte
	e = f.pool.QueryRow(f.ctx, "SELECT snapshot FROM issue_history WHERE version=1").Scan(&snapshot)
	if e != nil {
		t.Fatal(e)
	}
	var old issue.Issue
	_ = json.Unmarshal(snapshot, &old)
	if old.Title == i.Title || old.Version != 1 {
		t.Fatal("history overwritten")
	}
	var event []byte
	e = f.pool.QueryRow(f.ctx, "SELECT o.payload FROM outbox_events o JOIN issue_history h ON h.event_id=o.id WHERE h.version=1").Scan(&event)
	if e != nil {
		t.Fatal(e)
	}
	if !bytes.Contains(event, []byte(old.Title)) {
		t.Fatal("canonical outbox snapshot missing")
	}
}
