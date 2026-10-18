package searchviews_test

import (
	"bytes"
	"encoding/json"
	"github.com/furkanmeclis/traccia/backend/internal/database/db"
	repo "github.com/furkanmeclis/traccia/backend/internal/modules/searchviews/repository"
	uc "github.com/furkanmeclis/traccia/backend/internal/modules/searchviews/usecase"
	"github.com/furkanmeclis/traccia/backend/internal/platform/authctx"
	"github.com/furkanmeclis/traccia/backend/internal/platform/operations"
	"github.com/google/uuid"
	"strings"
	"testing"
	"time"
)

func TestQueryRetryIdentityAcrossDefaultsAndConcurrentCapture(t *testing.T) {
	f := newSearchFixture(t)
	f.createTeam(t, "TTL")
	in := uc.TeamCreateInput{RequestID: uuid.New(), Query: uc.TeamQuery(query()), Limit: 100}
	first, e := f.call("team_queries.create", "", f.w, in)
	if e != nil {
		t.Fatal(e)
	}
	ops, e := uc.New(repo.New(f.pool, db.New(f.pool)), f.workspace).WithDefaultTTL(3600).Operations()
	if e != nil {
		t.Fatal(e)
	}
	reg, e := f.workspace.Registry(ops...)
	if e != nil {
		t.Fatal(e)
	}
	body, _ := json.Marshal(in)
	again, e := reg.Execute(f.ctx, operations.Call{OperationID: "team_queries.create", Channel: operations.API, WorkspaceID: f.w, Input: body})
	if e != nil || !bytes.Equal(first, again) {
		t.Fatalf("default change altered replay: %v", e)
	}
	ttl := int32(3600)
	in.TTLSeconds = &ttl
	_, e = f.call("team_queries.create", "", f.w, in)
	checkCode(t, e, operations.IdempotencyConflict)
	in.RequestID = uuid.New()
	in.TTLSeconds = nil
	done := make(chan struct {
		raw []byte
		e   error
	}, 2)
	for range 2 {
		go func() {
			b, e := f.call("team_queries.create", "", f.w, in)
			done <- struct {
				raw []byte
				e   error
			}{b, e}
		}()
	}
	a, b := <-done, <-done
	if a.e != nil || b.e != nil || !bytes.Equal(a.raw, b.raw) {
		t.Fatalf("concurrent capture: %v %v", a.e, b.e)
	}
	var n int
	if e = f.pool.QueryRow(f.ctx, "SELECT count(*) FROM search_query_sessions WHERE request_uuid=$1", in.RequestID).Scan(&n); e != nil || n != 1 {
		t.Fatal(n, e)
	}
}
func TestPrivateColumnsConcurrentFirstSetAndRollback(t *testing.T) {
	f := newSearchFixture(t)
	in := uc.ColumnsSetInput{Resource: "issue", Columns: []uc.Column{{Key: "title", Width: 240}}}
	done := make(chan error, 2)
	for range 2 {
		go func() { _, e := f.call("column_preferences.set", uuid.NewString(), f.w, in); done <- e }()
	}
	a, b := <-done, <-done
	if a == nil && b == nil || a != nil && b != nil {
		t.Fatalf("first CAS %v/%v", a, b)
	}
	if a != nil {
		checkCode(t, a, operations.VersionConflict)
	} else {
		checkCode(t, b, operations.VersionConflict)
	}
	var before int
	if e := f.pool.QueryRow(f.ctx, "SELECT count(*) FROM outbox_events").Scan(&before); e != nil {
		t.Fatal(e)
	}
	if _, e := f.pool.Exec(f.ctx, `CREATE FUNCTION fail_preference_history() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'forced preference rollback';END $$; CREATE TRIGGER forced_preference_rollback BEFORE INSERT ON private_preference_history FOR EACH ROW EXECUTE FUNCTION fail_preference_history()`); e != nil {
		t.Fatal(e)
	}
	q := uc.IssueQuery(query())
	view := uc.ViewCreateInput{Name: "retained\r\nİ private", Definition: uc.ViewQuery{Resource: "issue", Issue: &q}, Columns: in.Columns}
	key := uuid.NewString()
	_, e := f.call("saved_views.create", key, f.w, view)
	checkCode(t, e, operations.Internal)
	var views, events, receipts int
	if e = f.pool.QueryRow(f.ctx, "SELECT (SELECT count(*) FROM saved_views),(SELECT count(*) FROM outbox_events),(SELECT count(*) FROM operation_receipts WHERE operation_id='saved_views.create')").Scan(&views, &events, &receipts); e != nil || views != 0 || events != before || receipts != 0 {
		t.Fatal(views, events, receipts, e)
	}
	if _, e = f.pool.Exec(f.ctx, "DROP TRIGGER forced_preference_rollback ON private_preference_history;DROP FUNCTION fail_preference_history()"); e != nil {
		t.Fatal(e)
	}
	raw, e := f.call("saved_views.create", key, f.w, view)
	saved := decodeResult[uc.ViewOutput](t, raw, e)
	if saved.View.Name != view.Name {
		t.Fatal("name changed")
	}
	again, e := f.call("saved_views.create", key, f.w, view)
	if e != nil || !bytes.Equal(raw, again) {
		t.Fatal("receipt mismatch", e)
	}
	var payload string
	if e = f.pool.QueryRow(f.ctx, "SELECT payload::text FROM outbox_events WHERE event_name='saved_views.created'").Scan(&payload); e != nil {
		t.Fatal(e)
	}
	if strings.Contains(payload, "retained") || strings.Contains(payload, "columns") || strings.Contains(payload, "definition") {
		t.Fatal("private payload leaked")
	}
}
func waitQueryLock(t *testing.T, f searchFixture) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		var n int
		if e := f.pool.QueryRow(f.ctx, "SELECT count(*) FROM pg_stat_activity WHERE application_name=current_setting('application_name') AND wait_event_type='Lock'").Scan(&n); e != nil {
			t.Fatal(e)
		}
		if n > 0 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("expected blocked query")
}
func TestQueryCaptureReauthorizesAfterRequestLock(t *testing.T) {
	f := newSearchFixture(t)
	in := uc.TeamCreateInput{RequestID: uuid.New(), Query: uc.TeamQuery(query()), Limit: 100}
	tx, e := f.pool.Begin(f.ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer func() { _ = tx.Rollback(f.ctx) }()
	identity := f.w.String() + f.actor.String() + in.RequestID.String()
	if e = db.New(tx).LockSearchRequest(f.ctx, identity); e != nil {
		t.Fatal(e)
	}
	done := make(chan error, 1)
	go func() { _, e := f.call("team_queries.create", "", f.w, in); done <- e }()
	waitQueryLock(t, f)
	if _, e = f.pool.Exec(f.ctx, "UPDATE workspace_members SET active=false WHERE workspace_id=(SELECT id FROM workspaces WHERE uuid=$1) AND user_id=(SELECT id FROM users WHERE uuid=$2)", f.w, f.actor); e != nil {
		t.Fatal(e)
	}
	if e = tx.Commit(f.ctx); e != nil {
		t.Fatal(e)
	}
	select {
	case e = <-done:
		checkCode(t, e, operations.Forbidden)
	case <-time.After(5 * time.Second):
		t.Fatal("blocked capture")
	}
	var n int
	if e = f.pool.QueryRow(f.ctx, "SELECT count(*) FROM search_query_sessions").Scan(&n); e != nil || n != 0 {
		t.Fatal(n, e)
	}
}
func otherActor(t *testing.T, f searchFixture, role string) searchFixture {
	t.Helper()
	id := uuid.New()
	if _, e := f.pool.Exec(f.ctx, "INSERT INTO users(uuid,email,password_hash,name,surname,status) VALUES($1,$2,'unused','Other','Member','active')", id, id.String()+"@example.test"); e != nil {
		t.Fatal(e)
	}
	if _, e := f.pool.Exec(f.ctx, "INSERT INTO workspace_members(workspace_id,user_id,role) SELECT w.id,u.id,$3 FROM workspaces w,users u WHERE w.uuid=$1 AND u.uuid=$2", f.w, id, role); e != nil {
		t.Fatal(e)
	}
	f.actor = id
	f.ctx = authctx.WithPrincipal(f.ctx, authctx.Principal{UserID: id})
	return f
}
func TestQueryReadMemberAndPrivateActorBoundaries(t *testing.T) {
	f := newSearchFixture(t)
	f.createTeam(t, "MEM")
	member := otherActor(t, f, "member")
	raw, e := member.call("team_queries.create", "", f.w, uc.TeamCreateInput{RequestID: uuid.New(), Query: uc.TeamQuery(query()), Limit: 100})
	page := decodeResult[uc.TeamPage](t, raw, e)
	if len(page.Items) != 1 {
		t.Fatal(page)
	}
	_, e = f.call("team_queries.get", "", f.w, uc.PageInput{SessionID: page.Session.ID, Limit: 100})
	checkCode(t, e, operations.NotFound)
	_, e = member.call("column_preferences.set", uuid.NewString(), f.w, uc.ColumnsSetInput{Resource: "team", Columns: []uc.Column{{Key: "name", Width: 240}}})
	checkCode(t, e, operations.Forbidden)
	q := uc.TeamQuery(query())
	raw, e = f.call("saved_views.create", uuid.NewString(), f.w, uc.ViewCreateInput{Name: "Owner private", Definition: uc.ViewQuery{Resource: "team", Team: &q}, Columns: []uc.Column{{Key: "name", Width: 240}}})
	view := decodeResult[uc.ViewOutput](t, raw, e).View
	owner := otherActor(t, f, "owner")
	_, e = owner.call("saved_views.get", "", f.w, uc.ViewInput{ViewID: view.ID})
	checkCode(t, e, operations.NotFound)
}

func TestSavedViewRechecksManageAfterBlockedUpdate(t *testing.T) {
	f := newSearchFixture(t)
	q := uc.TeamQuery(query())
	definition := uc.ViewQuery{Resource: "team", Team: &q}
	columns := []uc.Column{{Key: "name", Width: 240}}
	raw, e := f.call("saved_views.create", uuid.NewString(), f.w, uc.ViewCreateInput{Name: "Original private", Definition: definition, Columns: columns})
	view := decodeResult[uc.ViewOutput](t, raw, e).View
	tx, e := f.pool.Begin(f.ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer func() { _ = tx.Rollback(f.ctx) }()
	if _, e = db.New(tx).LockSavedView(f.ctx, db.LockSavedViewParams{Uuid: f.w, ActorUuid: f.actor, Uuid_2: view.ID}); e != nil {
		t.Fatal(e)
	}
	done := make(chan error, 1)
	go func() {
		_, e := f.call("saved_views.update", uuid.NewString(), f.w, uc.ViewUpdateInput{ViewID: view.ID, ExpectedRevision: view.Revision, Name: "Caller retained change", Definition: definition, Columns: columns})
		done <- e
	}()
	waitQueryLock(t, f)
	if _, e = f.pool.Exec(f.ctx, "UPDATE workspace_members SET role='member' WHERE workspace_id=(SELECT id FROM workspaces WHERE uuid=$1) AND user_id=(SELECT id FROM users WHERE uuid=$2)", f.w, f.actor); e != nil {
		t.Fatal(e)
	}
	if e = tx.Commit(f.ctx); e != nil {
		t.Fatal(e)
	}
	select {
	case e = <-done:
		checkCode(t, e, operations.Forbidden)
	case <-time.After(5 * time.Second):
		t.Fatal("blocked preference update")
	}
	raw, e = f.call("saved_views.get", "", f.w, uc.ViewInput{ViewID: view.ID})
	after := decodeResult[uc.ViewOutput](t, raw, e).View
	if after.Name != view.Name || after.Revision != view.Revision || after.Capabilities.Manage {
		t.Fatal("revoked update escaped transaction")
	}
	var revisions, receipts int
	if e = f.pool.QueryRow(f.ctx, "SELECT (SELECT count(*) FROM private_preference_history),(SELECT count(*) FROM operation_receipts WHERE operation_id='saved_views.update')").Scan(&revisions, &receipts); e != nil || revisions != 1 || receipts != 0 {
		t.Fatal(revisions, receipts, e)
	}
}
