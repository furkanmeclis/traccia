package teams_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/furkanmeclis/traccia/backend/internal/database/db"
	repo "github.com/furkanmeclis/traccia/backend/internal/modules/teams/repository"
	team "github.com/furkanmeclis/traccia/backend/internal/modules/teams/usecase"
	wr "github.com/furkanmeclis/traccia/backend/internal/modules/workspaces/repository"
	ws "github.com/furkanmeclis/traccia/backend/internal/modules/workspaces/usecase"
	"github.com/furkanmeclis/traccia/backend/internal/platform/authctx"
	"github.com/furkanmeclis/traccia/backend/internal/platform/crypto"
	"github.com/furkanmeclis/traccia/backend/internal/platform/dbtx"
	"github.com/furkanmeclis/traccia/backend/internal/platform/operations"
	"github.com/furkanmeclis/traccia/backend/internal/platform/outbox"
	"github.com/furkanmeclis/traccia/backend/internal/product"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type teamFixture struct {
	pool          *pgxpool.Pool
	ctx           context.Context
	actor, w      uuid.UUID
	registry      *operations.Registry
	repo          *repo.Postgres
	workspace     *ws.Service
	workspaceRepo *wr.Postgres
}

func isolatedDatabase(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL required for team database acceptance")
	}
	ctx := context.Background()
	admin, e := pgxpool.New(ctx, dsn)
	if e != nil {
		t.Fatal("database configuration failed")
	}
	schema := "team_test_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if _, e = admin.Exec(ctx, "CREATE SCHEMA "+schema); e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { _, _ = admin.Exec(ctx, "DROP SCHEMA "+schema+" CASCADE"); admin.Close() })
	cfg, e := pgxpool.ParseConfig(dsn)
	if e != nil {
		t.Fatal("database configuration failed")
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = schema + ",public"
	cfg.ConnConfig.RuntimeParams["application_name"] = schema
	cfg.MaxConns = 40
	pool, e := pgxpool.NewWithConfig(ctx, cfg)
	if e != nil {
		t.Fatal("database connection failed")
	}
	t.Cleanup(pool.Close)
	files, e := filepath.Glob("../../../migrations/*.up.sql")
	if e != nil || len(files) == 0 {
		t.Fatal("migrations unavailable")
	}
	for _, file := range files {
		raw, e := os.ReadFile(file)
		if e != nil {
			t.Fatal(e)
		}
		if _, e = pool.Exec(ctx, string(raw)); e != nil {
			t.Fatalf("migration %s: %v", filepath.Base(file), e)
		}
	}
	for _, suffix := range []string{"down", "up"} {
		raw, e := os.ReadFile("../../../migrations/000036_product_catalog." + suffix + ".sql")
		if e != nil {
			t.Fatal(e)
		}
		if _, e = pool.Exec(ctx, string(raw)); e != nil {
			t.Fatal(e)
		}
	}
	return pool
}
func newTeamFixture(t *testing.T) teamFixture {
	t.Helper()
	p := isolatedDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	t.Cleanup(cancel)
	actor := uuid.New()
	if _, e := p.Exec(ctx, "INSERT INTO users(uuid,email,password_hash,name,surname,status) VALUES($1,$2,'unused','Team','Owner','active')", actor, actor.String()+"@example.test"); e != nil {
		t.Fatal(e)
	}
	if _, e := db.New(p).BootstrapProductOwner(ctx, actor); e != nil {
		t.Fatal(e)
	}
	ctx = authctx.WithPrincipal(ctx, authctx.Principal{UserID: actor})
	box, _ := crypto.NewSecretBox("12345678901234567890123456789012")
	wrp := wr.New(p, db.New(p), box)
	wsvc := ws.New(wrp)
	r := repo.New(p, db.New(p))
	reg, e := product.Registry(wsvc, team.New(r, wsvc))
	if e != nil {
		t.Fatal(e)
	}
	f := teamFixture{pool: p, ctx: ctx, actor: actor, registry: reg, repo: r, workspace: wsvc, workspaceRepo: wrp}
	raw, e := f.call("workspaces.create", uuid.NewString(), uuid.Nil, ws.CreateInput{Name: "Team tests"})
	if e != nil {
		t.Fatal(e)
	}
	var out ws.WorkspaceOutput
	if e = json.Unmarshal(raw, &out); e != nil {
		t.Fatal(e)
	}
	f.w = out.Workspace.ID
	return f
}
func (f teamFixture) call(op, key string, w uuid.UUID, in any) ([]byte, error) {
	raw, e := json.Marshal(in)
	if e != nil {
		return nil, e
	}
	return f.registry.Execute(f.ctx, operations.Call{OperationID: op, Channel: operations.API, WorkspaceID: w, IdempotencyKey: key, Input: raw})
}
func (f teamFixture) createTeam(t *testing.T, code string) team.Team {
	t.Helper()
	raw, e := f.call("teams.create", uuid.NewString(), f.w, team.CreateTeamInput{Name: "Team " + code, Code: code})
	if e != nil {
		t.Fatal(e)
	}
	var out team.TeamOutput
	if e = json.Unmarshal(raw, &out); e != nil {
		t.Fatal(e)
	}
	return out.Team
}
func checkCode(t *testing.T, e error, code operations.Code) {
	t.Helper()
	var op *operations.Error
	if !errors.As(e, &op) || op.Code != code {
		t.Fatalf("wanted %s got %v", code, e)
	}
}
func TestConcurrentCountersRollbackAndIsolation(t *testing.T) {
	f := newTeamFixture(t)
	a := f.createTeam(t, "AA")
	b := f.createTeam(t, "BB")
	if _, e := f.repo.AllocateIssueNumber(f.ctx, f.w, a.ID); e == nil {
		t.Fatal("allocator accepted absent transaction")
	}
	const n = 32
	values := make(chan int64, n)
	fail := make(chan error, n)
	var wg sync.WaitGroup
	for range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			tx, e := f.pool.Begin(f.ctx)
			if e != nil {
				fail <- e
				return
			}
			defer func() { _ = tx.Rollback(f.ctx) }()
			v, e := f.repo.AllocateIssueNumber(dbtx.WithTx(f.ctx, tx), f.w, a.ID)
			if e == nil {
				e = tx.Commit(f.ctx)
			}
			if e != nil {
				fail <- e
			} else {
				values <- v
			}
		}()
	}
	wg.Wait()
	close(fail)
	for e := range fail {
		t.Fatal(e)
	}
	close(values)
	seen := map[int64]bool{}
	for v := range values {
		if seen[v] || v < 1 || v > n {
			t.Fatalf("duplicate/out of range %d", v)
		}
		seen[v] = true
	}
	if len(seen) != n {
		t.Fatal("missing allocation")
	}
	tx, e := f.pool.Begin(f.ctx)
	if e != nil {
		t.Fatal(e)
	}
	v, e := f.repo.AllocateIssueNumber(dbtx.WithTx(f.ctx, tx), f.w, a.ID)
	if e != nil || v != n+1 {
		t.Fatalf("rollback allocation %d %v", v, e)
	}
	_ = tx.Rollback(f.ctx)
	tx, _ = f.pool.Begin(f.ctx)
	ctx := dbtx.WithTx(f.ctx, tx)
	v, e = f.repo.AllocateIssueNumber(ctx, f.w, a.ID)
	if e != nil || v != n+1 {
		t.Fatalf("rollback consumed number %d %v", v, e)
	}
	v, e = f.repo.AllocateIssueNumber(ctx, f.w, b.ID)
	if e != nil || v != 1 {
		t.Fatalf("team counter mixed %d %v", v, e)
	}
	_, e = f.repo.AllocateIssueNumber(ctx, uuid.New(), a.ID)
	checkCode(t, e, operations.NotFound)
	_ = tx.Rollback(f.ctx)
	// Counters are bigint throughout the generated query and repository.
	if _, e = f.pool.Exec(f.ctx, "UPDATE teams SET next_issue_number=3000000000 WHERE uuid=$1", a.ID); e != nil {
		t.Fatal(e)
	}
	tx, _ = f.pool.Begin(f.ctx)
	v, e = f.repo.AllocateIssueNumber(dbtx.WithTx(f.ctx, tx), f.w, a.ID)
	if e != nil || v != 3000000000 {
		t.Fatalf("bigint allocation %d %v", v, e)
	}
	_ = tx.Rollback(f.ctx)
	_, e = f.call("teams.archive", uuid.NewString(), f.w, team.ArchiveTeamInput{TeamID: a.ID, ExpectedVersion: 1})
	if e != nil {
		t.Fatal(e)
	}
	tx, _ = f.pool.Begin(f.ctx)
	_, e = f.repo.AllocateIssueNumber(dbtx.WithTx(f.ctx, tx), f.w, a.ID)
	checkCode(t, e, operations.NotFound)
	_ = tx.Rollback(f.ctx)
}
func TestTeamRetriesHistoryAndProjection(t *testing.T) {
	f := newTeamFixture(t)
	key := uuid.NewString()
	in := team.CreateTeamInput{Name: "Retry", Code: "re"}
	const n = 8
	raws := make(chan []byte, n)
	errs := make(chan error, n)
	var wg sync.WaitGroup
	for range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			raw, e := f.call("teams.create", key, f.w, in)
			if e != nil {
				errs <- e
			} else {
				raws <- raw
			}
		}()
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		t.Fatal(e)
	}
	close(raws)
	var first []byte
	for raw := range raws {
		if first == nil {
			first = raw
		} else if !bytes.Equal(first, raw) {
			t.Fatal("retry changed result")
		}
	}
	var out team.TeamOutput
	_ = json.Unmarshal(first, &out)
	if out.Team.Code != "RE" {
		t.Fatal("code normalization missing")
	}
	_, e := f.call("teams.create", key, f.w, team.CreateTeamInput{Name: "Changed", Code: "RE"})
	checkCode(t, e, operations.IdempotencyConflict)
	_, e = f.call("teams.create", uuid.NewString(), f.w, in)
	checkCode(t, e, operations.Conflict)
	for _, table := range []string{"workspace_history", "outbox_events", "event_deliveries", "operation_receipts"} {
		var count int
		if e = f.pool.QueryRow(f.ctx, "SELECT count(*) FROM "+table).Scan(&count); e != nil || count != 2 {
			t.Fatalf("%s count=%d %v", table, count, e)
		}
	}
	consumers, e := f.workspaceRepo.DurableConsumers()
	if e != nil {
		t.Fatal(e)
	}
	worker := outbox.NewWorker(outbox.NewStore(f.pool, db.New(f.pool)), consumers, nil)
	for range 2 {
		if _, e = worker.Drain(f.ctx); e != nil {
			t.Fatal(e)
		}
	}
	raw, e := f.call("activity.list", "", f.w, ws.ListInput{Limit: 100})
	if e != nil {
		t.Fatal(e)
	}
	var activity ws.ActivityList
	_ = json.Unmarshal(raw, &activity)
	if len(activity.Items) != 2 || activity.Items[0].ResourceType != "teams" || activity.Items[0].ResourceID != out.Team.ID || activity.Items[0].TeamID != out.Team.ID.String() {
		t.Fatalf("resource projection %s", raw)
	}
	// Failed canonical history must roll back team, defaults and receipt.
	if _, e = f.pool.Exec(f.ctx, "CREATE FUNCTION fail_team_history() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.event_name='teams.created' THEN RAISE EXCEPTION 'synthetic history failure'; END IF; RETURN NEW; END $$; CREATE TRIGGER reject_test_history BEFORE INSERT ON workspace_history FOR EACH ROW EXECUTE FUNCTION fail_team_history()"); e != nil {
		t.Fatal(e)
	}
	_, e = f.call("teams.create", uuid.NewString(), f.w, team.CreateTeamInput{Name: "Rollback", Code: "RB"})
	if e == nil {
		t.Fatal("history failure ignored")
	}
	var count int
	if e = f.pool.QueryRow(f.ctx, "SELECT count(*) FROM teams WHERE code='RB'").Scan(&count); e != nil || count != 0 {
		t.Fatal("history failure left mutation")
	}
}
func TestWorkflowAtomicReorderCASAndScope(t *testing.T) {
	f := newTeamFixture(t)
	a := f.createTeam(t, "AA")
	b := f.createTeam(t, "BB")
	rows, v, e := f.repo.ListStatuses(f.ctx, f.w, team.StatusListInput{TeamID: a.ID, Limit: 100})
	if e != nil || len(rows) != 5 || v != 1 {
		t.Fatalf("defaults %d %d %v", len(rows), v, e)
	}
	ids := []string{}
	for i := len(rows) - 1; i >= 0; i-- {
		ids = append(ids, rows[i].ID.String())
	}
	in := team.ReorderInput{TeamID: a.ID, StatusIDs: ids, ExpectedWorkflowVersion: 1}
	results := make(chan error, 2)
	for range 2 {
		go func() { _, e := f.call("workflow_statuses.reorder", uuid.NewString(), f.w, in); results <- e }()
	}
	success := 0
	for range 2 {
		e := <-results
		if e == nil {
			success++
		} else {
			checkCode(t, e, operations.VersionConflict)
		}
	}
	if success != 1 {
		t.Fatal("concurrent reorder CAS")
	}
	rows, v, e = f.repo.ListStatuses(f.ctx, f.w, team.StatusListInput{TeamID: a.ID, Limit: 100})
	if e != nil || v != 2 {
		t.Fatal(e)
	}
	for i, row := range rows {
		if row.ID.String() != ids[i] || row.Position != int64(i) || row.Version != 2 {
			t.Fatal("partial reorder")
		}
	}
	in.ExpectedWorkflowVersion = 2
	in.StatusIDs[0] = in.StatusIDs[1]
	_, e = f.call("workflow_statuses.reorder", uuid.NewString(), f.w, in)
	checkCode(t, e, operations.Validation)
	other, _, _ := f.repo.ListStatuses(f.ctx, f.w, team.StatusListInput{TeamID: b.ID, Limit: 100})
	_, e = f.call("workflow_statuses.update", uuid.NewString(), f.w, team.UpdateStatusInput{TeamID: a.ID, StatusID: other[0].ID, Name: "Wrong", Category: "backlog", Color: "#ffffff", ExpectedVersion: 1, ExpectedWorkflowVersion: 2})
	checkCode(t, e, operations.NotFound)
	_, after, _ := f.repo.ListStatuses(f.ctx, f.w, team.StatusListInput{TeamID: a.ID, Limit: 100})
	if after != 2 {
		t.Fatal("failed write advanced workflow version")
	}
	// Direct database composite FK rejects sibling workspace linkage.
	_, e = f.pool.Exec(f.ctx, "INSERT INTO workflow_statuses(workspace_id,team_id,name,category,color,position) SELECT w.id,t.id,'Invalid','started','#ffffff',99 FROM workspaces w CROSS JOIN teams t WHERE w.uuid=$1 AND t.uuid=$2", uuid.New(), a.ID)
	if e == nil { // Empty INSERT selects zero rows; use actual foreign workspace below.
		raw, err := f.call("workspaces.create", uuid.NewString(), uuid.Nil, ws.CreateInput{Name: "Other"})
		if err != nil {
			t.Fatal(err)
		}
		var out ws.WorkspaceOutput
		_ = json.Unmarshal(raw, &out)
		_, e = f.pool.Exec(f.ctx, "INSERT INTO workflow_statuses(workspace_id,team_id,name,category,color,position) SELECT w.id,t.id,'Invalid','started','#ffffff',99 FROM workspaces w CROSS JOIN teams t WHERE w.uuid=$1 AND t.uuid=$2", out.Workspace.ID, a.ID)
	}
	if e == nil {
		t.Fatal("cross-workspace status FK accepted")
	}
}
func TestWorkflowPageSnapshotDuringConcurrentReorder(t *testing.T) {
	f := newTeamFixture(t)
	a := f.createTeam(t, "SN")
	rows, _, e := f.repo.ListStatuses(f.ctx, f.w, team.StatusListInput{TeamID: a.ID, Limit: 100})
	if e != nil {
		t.Fatal(e)
	}
	ids := make([]string, len(rows))
	for i, row := range rows {
		ids[i] = row.ID.String()
	}
	done := make(chan error, 1)
	go func() {
		for i := int64(1); i <= 30; i++ {
			_, e := f.call("workflow_statuses.reorder", uuid.NewString(), f.w, team.ReorderInput{TeamID: a.ID, StatusIDs: ids, ExpectedWorkflowVersion: i})
			if e != nil {
				done <- e
				return
			}
		}
		done <- nil
	}()
	for range 100 {
		rows, v, e := f.repo.ListStatuses(f.ctx, f.w, team.StatusListInput{TeamID: a.ID, Limit: 2})
		if e != nil {
			t.Fatal(e)
		}
		for _, row := range rows {
			if row.Version != v {
				t.Fatalf("mixed snapshot row=%d aggregate=%d", row.Version, v)
			}
		}
	}
	if e := <-done; e != nil {
		t.Fatal(e)
	}
}
func TestAssignmentScopesAndSharedLabelArchiveLock(t *testing.T) {
	f := newTeamFixture(t)
	a := f.createTeam(t, "AA")
	b := f.createTeam(t, "BB")
	rows, _, e := f.repo.ListStatuses(f.ctx, f.w, team.StatusListInput{TeamID: a.ID, Limit: 100})
	if e != nil {
		t.Fatal(e)
	}
	create := func(tid string) team.Label {
		raw, e := f.call("labels.create", uuid.NewString(), f.w, team.CreateLabelInput{TeamID: tid, Name: uuid.NewString(), Color: "#123abc"})
		if e != nil {
			t.Fatal(e)
		}
		var out team.LabelOutput
		_ = json.Unmarshal(raw, &out)
		return out.Label
	}
	shared, own, other := create(""), create(a.ID.String()), create(b.ID.String())
	tx, e := f.pool.Begin(f.ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer func() { _ = tx.Rollback(f.ctx) }()
	ctx := dbtx.WithTx(f.ctx, tx)
	if e = f.repo.ValidateAssignment(ctx, f.w, a.ID, rows[0].ID, []uuid.UUID{shared.ID, own.ID}); e != nil {
		t.Fatal(e)
	}
	checkCode(t, f.repo.ValidateAssignment(ctx, f.w, a.ID, rows[0].ID, []uuid.UUID{other.ID}), operations.Validation)
	checkCode(t, f.repo.ValidateAssignment(ctx, f.w, a.ID, rows[0].ID, []uuid.UUID{uuid.New()}), operations.NotFound)
	archive := make(chan error, 1)
	go func() {
		_, e := f.call("labels.archive", uuid.NewString(), f.w, team.ArchiveLabelInput{LabelID: shared.ID, ExpectedVersion: 1})
		archive <- e
	}()
	deadline := time.Now().Add(3 * time.Second)
	blocked := false
	for time.Now().Before(deadline) {
		var n int
		e = f.pool.QueryRow(f.ctx, "SELECT count(*) FROM pg_stat_activity WHERE application_name=current_setting('application_name') AND wait_event_type='Lock' AND query LIKE '%UPDATE labels SET archived%'").Scan(&n)
		if e != nil {
			t.Fatal(e)
		}
		if n > 0 {
			blocked = true
			break
		}
		select {
		case e := <-archive:
			t.Fatalf("archive bypassed assignment lock: %v", e)
		default:
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !blocked {
		t.Fatal("archive did not reach label row lock")
	}
	if e = tx.Commit(f.ctx); e != nil {
		t.Fatal(e)
	}
	if e = <-archive; e != nil {
		t.Fatal(e)
	}
	tx, e = f.pool.BeginTx(f.ctx, pgx.TxOptions{})
	if e != nil {
		t.Fatal(e)
	}
	defer func() { _ = tx.Rollback(f.ctx) }()
	checkCode(t, f.repo.ValidateAssignment(dbtx.WithTx(f.ctx, tx), f.w, a.ID, rows[0].ID, []uuid.UUID{shared.ID}), operations.Conflict)
}
func TestConcurrentNormalizedCodesAndStableIdentity(t *testing.T) {
	f := newTeamFixture(t)
	errs := make(chan error, 2)
	for _, code := range []string{"qa", "QA"} {
		go func(code string) {
			_, e := f.call("teams.create", uuid.NewString(), f.w, team.CreateTeamInput{Name: "Concurrent", Code: code})
			errs <- e
		}(code)
	}
	good := 0
	for range 2 {
		e := <-errs
		if e == nil {
			good++
		} else {
			checkCode(t, e, operations.Conflict)
		}
	}
	if good != 1 {
		t.Fatal("normalized code race")
	}
	var count int
	if e := f.pool.QueryRow(f.ctx, "SELECT count(*) FROM teams WHERE code='QA'").Scan(&count); e != nil || count != 1 {
		t.Fatal("duplicate team code")
	}
	if _, e := f.pool.Exec(f.ctx, "UPDATE teams SET code='CHANGED' WHERE code='QA'"); e == nil {
		t.Fatal("stable code changed")
	}
	raw, e := f.call("workspaces.create", uuid.NewString(), uuid.Nil, ws.CreateInput{Name: "Other"})
	if e != nil {
		t.Fatal(e)
	}
	var w ws.WorkspaceOutput
	_ = json.Unmarshal(raw, &w)
	if _, e = f.call("teams.create", uuid.NewString(), w.Workspace.ID, team.CreateTeamInput{Name: "Other QA", Code: "qa"}); e != nil {
		t.Fatal("code should be workspace-local", e)
	}
}
func TestWorkflowCursorTraversesMoreThanOneHundred(t *testing.T) {
	f := newTeamFixture(t)
	a := f.createTeam(t, "PG")
	for i := 0; i < 110; i++ {
		_, e := f.call("workflow_statuses.create", uuid.NewString(), f.w, team.CreateStatusInput{TeamID: a.ID, Name: fmt.Sprintf("Extra %d", i), Category: "started", Color: "#123456", ExpectedWorkflowVersion: int64(i + 1)})
		if e != nil {
			t.Fatal(e)
		}
	}
	seen := map[uuid.UUID]bool{}
	after := ""
	for {
		raw, e := f.call("workflow_statuses.list", "", f.w, team.StatusListInput{TeamID: a.ID, Limit: 17, After: after})
		if e != nil {
			t.Fatal(e)
		}
		var page team.StatusList
		_ = json.Unmarshal(raw, &page)
		if page.WorkflowVersion != 111 {
			t.Fatal("workflow version")
		}
		for _, row := range page.Items {
			if seen[row.ID] {
				t.Fatal("duplicate cursor item")
			}
			seen[row.ID] = true
		}
		if !page.HasMore {
			break
		}
		after = page.NextCursor
	}
	if len(seen) != 115 {
		t.Fatalf("truncated workflow: %d", len(seen))
	}
}

func TestTeamConfigurationCursorsSurviveAnchorArchive(t *testing.T) {
	f := newTeamFixture(t)
	for _, code := range []string{"AA", "BB", "CC"} {
		f.createTeam(t, code)
	}
	raw, e := f.call("teams.list", "", f.w, team.TeamListInput{Limit: 1})
	if e != nil {
		t.Fatal(e)
	}
	var first team.TeamList
	if e = json.Unmarshal(raw, &first); e != nil {
		t.Fatal(e)
	}
	if len(first.Items) != 1 || !first.HasMore {
		t.Fatal("team cursor fixture")
	}
	_, e = f.call("teams.archive", uuid.NewString(), f.w, team.ArchiveTeamInput{TeamID: first.Items[0].ID, ExpectedVersion: 1})
	if e != nil {
		t.Fatal(e)
	}
	raw, e = f.call("teams.list", "", f.w, team.TeamListInput{Limit: 100, After: first.NextCursor})
	if e != nil {
		t.Fatal(e)
	}
	var rest team.TeamList
	_ = json.Unmarshal(raw, &rest)
	if len(rest.Items) != 2 || rest.HasMore {
		t.Fatalf("archived team anchor truncated traversal: %s", raw)
	}
	// Status positions remain scoped to their owning team after the anchor archives.
	owner := rest.Items[0]
	raw, e = f.call("workflow_statuses.list", "", f.w, team.StatusListInput{TeamID: owner.ID, Limit: 1})
	if e != nil {
		t.Fatal(e)
	}
	var statuses team.StatusList
	_ = json.Unmarshal(raw, &statuses)
	if len(statuses.Items) != 1 || !statuses.HasMore {
		t.Fatal("workflow cursor fixture")
	}
	_, e = f.call("workflow_statuses.archive", uuid.NewString(), f.w, team.ArchiveStatusInput{TeamID: owner.ID, StatusID: statuses.Items[0].ID, ExpectedVersion: 1, ExpectedWorkflowVersion: 1})
	if e != nil {
		t.Fatal(e)
	}
	raw, e = f.call("workflow_statuses.list", "", f.w, team.StatusListInput{TeamID: owner.ID, Limit: 100, After: statuses.NextCursor})
	if e != nil {
		t.Fatal(e)
	}
	var remaining team.StatusList
	_ = json.Unmarshal(raw, &remaining)
	if len(remaining.Items) != 4 || remaining.WorkflowVersion != 2 {
		t.Fatalf("archived workflow anchor truncated traversal: %s", raw)
	}
	raw, e = f.call("workflow_statuses.list", "", f.w, team.StatusListInput{TeamID: rest.Items[1].ID, Limit: 100, After: statuses.NextCursor})
	if e != nil {
		t.Fatal(e)
	}
	_ = json.Unmarshal(raw, &remaining)
	if len(remaining.Items) != 0 {
		t.Fatal("foreign-team status cursor crossed scope")
	}
	for i := range 3 {
		_, e = f.call("labels.create", uuid.NewString(), f.w, team.CreateLabelInput{Name: fmt.Sprintf("Label %d", i), Color: "#123456"})
		if e != nil {
			t.Fatal(e)
		}
	}
	raw, e = f.call("labels.list", "", f.w, team.LabelListInput{Limit: 1})
	if e != nil {
		t.Fatal(e)
	}
	var labels team.LabelList
	_ = json.Unmarshal(raw, &labels)
	if len(labels.Items) != 1 || !labels.HasMore {
		t.Fatal("label cursor fixture")
	}
	_, e = f.call("labels.archive", uuid.NewString(), f.w, team.ArchiveLabelInput{LabelID: labels.Items[0].ID, ExpectedVersion: 1})
	if e != nil {
		t.Fatal(e)
	}
	raw, e = f.call("labels.list", "", f.w, team.LabelListInput{Limit: 100, After: labels.NextCursor})
	if e != nil {
		t.Fatal(e)
	}
	var remainingLabels team.LabelList
	_ = json.Unmarshal(raw, &remainingLabels)
	if len(remainingLabels.Items) != 2 || remainingLabels.HasMore {
		t.Fatalf("archived label anchor truncated traversal: %s", raw)
	}
	raw, e = f.call("labels.list", "", f.w, team.LabelListInput{TeamID: owner.ID.String(), Limit: 100, After: labels.NextCursor})
	if e != nil {
		t.Fatal(e)
	}
	_ = json.Unmarshal(raw, &remainingLabels)
	if len(remainingLabels.Items) != 0 {
		t.Fatal("shared-label cursor crossed team scope")
	}
}
