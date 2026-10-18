package personalwork_test

import (
	"encoding/json"
	"errors"
	"github.com/furkanmeclis/traccia/backend/internal/database/db"
	pw "github.com/furkanmeclis/traccia/backend/internal/modules/personalwork/usecase"
	ws "github.com/furkanmeclis/traccia/backend/internal/modules/workspaces/usecase"
	"github.com/furkanmeclis/traccia/backend/internal/platform/authctx"
	"github.com/furkanmeclis/traccia/backend/internal/platform/operations"
	"github.com/google/uuid"
	"testing"
	"time"
)

func otherActor(t *testing.T, f personalFixture, role string) personalFixture {
	t.Helper()
	id := uuid.New()
	if _, e := f.pool.Exec(f.ctx, "INSERT INTO users(uuid,email,password_hash,name,surname,status) VALUES($1,$2,'unused','Other','Actor','active')", id, id.String()+"@example.test"); e != nil {
		t.Fatal(e)
	}
	if _, e := f.pool.Exec(f.ctx, "INSERT INTO workspace_members(workspace_id,user_id,role) SELECT w.id,u.id,$3 FROM workspaces w,users u WHERE w.uuid=$1 AND u.uuid=$2", f.w, id, role); e != nil {
		t.Fatal(e)
	}
	f.actor = id
	f.ctx = authctx.WithPrincipal(f.ctx, authctx.Principal{UserID: id})
	return f
}
func secondWorkspace(t *testing.T, f personalFixture) uuid.UUID {
	t.Helper()
	b, e := f.call("workspaces.create", uuid.NewString(), uuid.Nil, ws.CreateInput{Name: "Selected scope"})
	return decodeResult[ws.WorkspaceOutput](t, b, e).Workspace.ID
}
func TestPersonalScopeAndCredentialIsolation(t *testing.T) {
	f := newPersonalFixture(t)
	other := secondWorkspace(t, f)
	in := pw.PortfolioCreateInput{RequestID: uuid.New(), WorkspaceIDs: []uuid.UUID{other}, Definition: query(), Limit: 100}
	raw, e := f.call("personal_portfolios.create", "", f.w, in)
	page := decodeResult[pw.PortfolioOutput](t, raw, e)
	member := otherActor(t, f, "member")
	memberIn := in
	memberIn.RequestID = uuid.New()
	memberIn.WorkspaceIDs = []uuid.UUID{f.w}
	raw, e = member.call("personal_portfolios.create", "", f.w, memberIn)
	_ = decodeResult[pw.PortfolioOutput](t, raw, e)
	_, e = member.call("personal_portfolios.get", "", f.w, pw.PortfolioGetInput{SessionID: page.Session.ID, Limit: 100})
	checkCode(t, e, operations.NotFound)
	_, e = member.call("favorites.set", uuid.NewString(), f.w, pw.FavoriteSetInput{Target: pw.Target{Kind: "workspace", ID: f.w}, Active: true})
	checkCode(t, e, operations.Forbidden)
	_, e = member.call("personal_portfolios.create", "", f.w, in)
	checkCode(t, e, operations.Forbidden)
	in.RequestID = uuid.New()
	in.WorkspaceIDs = []uuid.UUID{uuid.New()}
	_, e = member.call("personal_portfolios.create", "", f.w, in)
	checkCode(t, e, operations.Forbidden)
	raw, e = f.call("credentials.create", uuid.NewString(), f.w, ws.CreateCredentialInput{Name: "Scoped readonly", Audience: "mcp", Scopes: []string{"workspace.read"}, ExpiresAt: time.Now().Add(time.Hour)})
	token := decodeResult[ws.CreateCredentialOutput](t, raw, e).Token
	ctx := ws.WithToolCredential(f.ctx, token)
	memberIn.RequestID = uuid.New()
	body, _ := json.Marshal(memberIn)
	_, e = f.registry.Execute(ctx, operations.Call{OperationID: "personal_portfolios.create", Channel: operations.MCP, WorkspaceID: f.w, Input: body})
	if e != nil {
		t.Fatal(e)
	}
	memberIn.RequestID = uuid.New()
	memberIn.WorkspaceIDs = []uuid.UUID{other}
	body, _ = json.Marshal(memberIn)
	_, e = f.registry.Execute(ctx, operations.Call{OperationID: "personal_portfolios.create", Channel: operations.MCP, WorkspaceID: f.w, Input: body})
	checkCode(t, e, operations.Forbidden)
	if _, e = f.pool.Exec(f.ctx, "UPDATE workspace_members SET active=false WHERE workspace_id=(SELECT id FROM workspaces WHERE uuid=$1) AND user_id=(SELECT id FROM users WHERE uuid=$2)", other, f.actor); e != nil {
		t.Fatal(e)
	}
	raw, e = f.call("personal_portfolios.get", "", f.w, pw.PortfolioGetInput{SessionID: page.Session.ID, Limit: 100})
	checkCode(t, e, operations.Forbidden)
	if raw != nil {
		t.Fatal("scope failure returned payload")
	}
	var op *operations.Error
	if !errors.As(e, &op) || len(op.Details) != 1 || op.Details[0].Code != "scope_changed" {
		t.Fatal(e)
	}
}
func waitPersonalLock(t *testing.T, f personalFixture) {
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
	t.Fatal("request did not wait")
}
func TestPersonalCaptureRechecksSelectedScopeAfterWait(t *testing.T) {
	f := newPersonalFixture(t)
	other := secondWorkspace(t, f)
	in := pw.PortfolioCreateInput{RequestID: uuid.New(), WorkspaceIDs: []uuid.UUID{other}, Definition: query(), Limit: 100}
	tx, e := f.pool.Begin(f.ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer func() { _ = tx.Rollback(f.ctx) }()
	if e = db.New(tx).LockPersonalRequest(f.ctx, "portfolio"+f.w.String()+f.actor.String()+in.RequestID.String()); e != nil {
		t.Fatal(e)
	}
	done := make(chan error, 1)
	go func() { _, e := f.call("personal_portfolios.create", "", f.w, in); done <- e }()
	waitPersonalLock(t, f)
	if _, e = f.pool.Exec(f.ctx, "UPDATE workspace_members SET active=false WHERE workspace_id=(SELECT id FROM workspaces WHERE uuid=$1) AND user_id=(SELECT id FROM users WHERE uuid=$2)", other, f.actor); e != nil {
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
	if e = f.pool.QueryRow(f.ctx, "SELECT count(*) FROM personal_portfolio_sessions").Scan(&n); e != nil || n != 0 {
		t.Fatal(n, e)
	}
}

func TestPersonalPageAndFavoriteReauthorizeAfterLock(t *testing.T) {
	f := newPersonalFixture(t)
	other := secondWorkspace(t, f)
	in := pw.PortfolioCreateInput{RequestID: uuid.New(), WorkspaceIDs: []uuid.UUID{other}, Definition: query(), Limit: 100}
	raw, e := f.call("personal_portfolios.create", "", f.w, in)
	page := decodeResult[pw.PortfolioOutput](t, raw, e)
	tx, e := f.pool.Begin(f.ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer func() { _ = tx.Rollback(f.ctx) }()
	if _, e = tx.Exec(f.ctx, "SELECT id FROM personal_portfolio_sessions WHERE uuid=$1 FOR UPDATE", page.Session.ID); e != nil {
		t.Fatal(e)
	}
	type result struct {
		raw []byte
		e   error
	}
	done := make(chan result, 1)
	go func() {
		b, e := f.call("personal_portfolios.get", "", f.w, pw.PortfolioGetInput{SessionID: page.Session.ID, Limit: 100})
		done <- result{b, e}
	}()
	waitPersonalLock(t, f)
	if _, e = f.pool.Exec(f.ctx, "UPDATE workspace_members SET active=false WHERE workspace_id=(SELECT id FROM workspaces WHERE uuid=$1) AND user_id=(SELECT id FROM users WHERE uuid=$2)", other, f.actor); e != nil {
		t.Fatal(e)
	}
	if e = tx.Commit(f.ctx); e != nil {
		t.Fatal(e)
	}
	select {
	case out := <-done:
		checkCode(t, out.e, operations.Forbidden)
		if out.raw != nil {
			t.Fatal("blocked page leaked payload")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("page stuck")
	}
	set := pw.FavoriteSetInput{Target: pw.Target{Kind: "workspace", ID: f.w}, Active: true}
	raw, e = f.call("favorites.set", uuid.NewString(), f.w, set)
	favorite := decodeResult[pw.FavoriteSetOutput](t, raw, e).Favorite
	lock, e := f.pool.Begin(f.ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer func() { _ = lock.Rollback(f.ctx) }()
	if e = db.New(lock).LockPersonalRequest(f.ctx, "favorite"+f.w.String()+f.actor.String()+set.Target.Kind+set.Target.ID.String()); e != nil {
		t.Fatal(e)
	}
	set.Active = false
	set.ExpectedRevision = &favorite.Revision
	key := uuid.NewString()
	go func() { b, e := f.call("favorites.set", key, f.w, set); done <- result{b, e} }()
	waitPersonalLock(t, f)
	if _, e = f.pool.Exec(f.ctx, "UPDATE workspace_members SET role='member' WHERE workspace_id=(SELECT id FROM workspaces WHERE uuid=$1) AND user_id=(SELECT id FROM users WHERE uuid=$2)", f.w, f.actor); e != nil {
		t.Fatal(e)
	}
	if e = lock.Commit(f.ctx); e != nil {
		t.Fatal(e)
	}
	select {
	case out := <-done:
		checkCode(t, out.e, operations.Forbidden)
	case <-time.After(5 * time.Second):
		t.Fatal("favorite stuck")
	}
	raw, e = f.call("favorites.list", "", f.w, pw.FavoriteListInput{Limit: 100})
	retained := decodeResult[pw.FavoriteListOutput](t, raw, e)
	if len(retained.Items) != 1 || retained.Items[0].Revision != favorite.Revision || !retained.Items[0].Active || retained.Capabilities.Manage {
		t.Fatal("revoked mutation escaped")
	}
	var history, receipts int
	if e = f.pool.QueryRow(f.ctx, "SELECT (SELECT count(*) FROM personal_favorite_history),(SELECT count(*) FROM operation_receipts WHERE idempotency_key=$1)", key).Scan(&history, &receipts); e != nil || history != 1 || receipts != 0 {
		t.Fatal(history, receipts, e)
	}
}
