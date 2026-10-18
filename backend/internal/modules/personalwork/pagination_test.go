package personalwork_test

import (
	"context"
	"github.com/furkanmeclis/traccia/backend/internal/database/db"
	repo "github.com/furkanmeclis/traccia/backend/internal/modules/personalwork/repository"
	pw "github.com/furkanmeclis/traccia/backend/internal/modules/personalwork/usecase"
	"github.com/furkanmeclis/traccia/backend/internal/platform/operations"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func seedIssues(t *testing.T, f personalFixture, n int) {
	t.Helper()
	team := f.createTeam(t, "BULK")
	status := f.status(t, team.ID)
	if _, e := f.pool.Exec(f.ctx, `INSERT INTO issues(workspace_id,team_id,number,status_id,title,priority) SELECT w.id,t.id,g,s.id,'row '||lpad(g::text,5,'0'),'normal' FROM workspaces w JOIN teams t ON t.workspace_id=w.id JOIN workflow_statuses s ON s.team_id=t.id AND s.workspace_id=w.id CROSS JOIN generate_series(1,$4::int) g WHERE w.uuid=$1 AND t.uuid=$2 AND s.uuid=$3`, f.w, team.ID, status, n); e != nil {
		t.Fatal(e)
	}
}
func TestPersonalCompleteManifestAndFavoriteTombstoneCursors(t *testing.T) {
	f := newPersonalFixture(t)
	other := secondWorkspace(t, f)
	zero := secondWorkspace(t, f)
	seedIssues(t, f, 6103)
	g := f
	g.w = other
	seedIssues(t, g, 103)
	in := pw.PortfolioCreateInput{RequestID: uuid.New(), WorkspaceIDs: []uuid.UUID{zero, other, f.w}, Definition: query(), Limit: 100}
	raw, e := f.call("personal_portfolios.create", "", f.w, in)
	page := decodeResult[pw.PortfolioOutput](t, raw, e)
	if page.Session.TotalCount != "6206" || len(page.Session.Workspaces) != 3 {
		t.Fatal(page.Session)
	}
	counts := map[uuid.UUID]string{}
	for _, w := range page.Session.Workspaces {
		counts[w.ID] = w.TotalCount
	}
	if counts[zero] != "0" || counts[f.w] != "6103" || counts[other] != "103" {
		t.Fatal(counts)
	}
	if _, e = f.pool.Exec(f.ctx, "UPDATE issues SET title='changed',archived=true,version=version+1 WHERE workspace_id IN (SELECT id FROM workspaces WHERE uuid=ANY($1::uuid[]))", []uuid.UUID{f.w, other}); e != nil {
		t.Fatal(e)
	}
	seen := map[uuid.UUID]bool{}
	for {
		for _, row := range page.Items {
			if row.Issue == nil || row.Issue.Archived || row.Issue.Title == "changed" || seen[row.Issue.ID] {
				t.Fatal("mutable or duplicate snapshot")
			}
			seen[row.Issue.ID] = true
		}
		if !page.HasMore {
			break
		}
		raw, e = f.call("personal_portfolios.get", "", f.w, pw.PortfolioGetInput{SessionID: page.Session.ID, After: page.NextCursor, Limit: 100})
		page = decodeResult[pw.PortfolioOutput](t, raw, e)
	}
	if len(seen) != 6206 {
		t.Fatal(len(seen))
	}
	rows, e := f.pool.Query(f.ctx, "SELECT i.uuid FROM issues i JOIN workspaces w ON w.id=i.workspace_id WHERE w.uuid=$1 ORDER BY i.uuid LIMIT 107", f.w)
	if e != nil {
		t.Fatal(e)
	}
	ids := []uuid.UUID{}
	for rows.Next() {
		var id uuid.UUID
		if e = rows.Scan(&id); e != nil {
			t.Fatal(e)
		}
		ids = append(ids, id)
	}
	rows.Close()
	for _, id := range ids {
		_, e = f.call("favorites.set", uuid.NewString(), f.w, pw.FavoriteSetInput{Target: pw.Target{Kind: "issue", ID: id}, Active: true})
		if e != nil {
			t.Fatal(e)
		}
	}
	raw, e = f.call("favorites.list", "", f.w, pw.FavoriteListInput{Limit: 100})
	first := decodeResult[pw.FavoriteListOutput](t, raw, e)
	if len(first.Items) != 100 || !first.HasMore {
		t.Fatal(first)
	}
	anchor := first.Items[99]
	_, e = f.call("favorites.set", uuid.NewString(), f.w, pw.FavoriteSetInput{Target: anchor.Target, Active: false, ExpectedRevision: &anchor.Revision})
	if e != nil {
		t.Fatal(e)
	}
	raw, e = f.call("favorites.list", "", f.w, pw.FavoriteListInput{Limit: 100, After: first.NextCursor})
	last := decodeResult[pw.FavoriteListOutput](t, raw, e)
	if len(last.Items) != 7 || last.HasMore {
		t.Fatal(last)
	}
}
func TestPersonalCleanupBoundedRollbackAndPermanentExpiry(t *testing.T) {
	f := newPersonalFixture(t)
	seedIssues(t, f, 2107)
	r := repo.New(f.pool, db.New(f.pool))
	input := pw.PortfolioCreateInput{RequestID: uuid.New(), WorkspaceIDs: []uuid.UUID{f.w}, Definition: query(), Limit: 100}
	p, e := r.Capture(f.ctx, f.w, f.actor, pw.Capture{Input: input, EffectiveTTL: 1}, func(context.Context, []uuid.UUID, bool) error { return nil })
	if e != nil || p.Session.TotalCount != "2107" {
		t.Fatal(e)
	}
	time.Sleep(1100 * time.Millisecond)
	if _, e = f.pool.Exec(f.ctx, `CREATE FUNCTION fail_personal_cleanup() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'forced cleanup rollback';END $$;CREATE TRIGGER fail_personal_cleanup BEFORE DELETE ON personal_portfolio_items FOR EACH ROW EXECUTE FUNCTION fail_personal_cleanup()`); e != nil {
		t.Fatal(e)
	}
	if _, e = r.Cleanup(f.ctx, 1000); e == nil {
		t.Fatal("cleanup failure missing")
	}
	if _, e = f.pool.Exec(f.ctx, "DROP TRIGGER fail_personal_cleanup ON personal_portfolio_items;DROP FUNCTION fail_personal_cleanup()"); e != nil {
		t.Fatal(e)
	}
	for _, want := range []int64{1000, 1000, 107, 0} {
		n, e := r.Cleanup(f.ctx, 1000)
		if e != nil || n != want {
			t.Fatal(n, want, e)
		}
	}
	var state string
	var count int64
	var empty bool
	if e = f.pool.QueryRow(f.ctx, "SELECT state,total_count,query IS NULL FROM personal_portfolio_sessions WHERE uuid=$1", p.Session.ID).Scan(&state, &count, &empty); e != nil || state != "expired" || count != 2107 || !empty {
		t.Fatal(state, strconv.FormatInt(count, 10), empty, e)
	}
	_, e = f.call("personal_portfolios.create", "", f.w, input)
	if e == nil {
		t.Fatal("expired request silently recaptured")
	}
}

type pausePortfolioPage struct {
	armed            atomic.Bool
	entered, release chan struct{}
}

func (p *pausePortfolioPage) TraceQueryStart(c context.Context, _ *pgx.Conn, d pgx.TraceQueryStartData) context.Context {
	if strings.HasPrefix(d.SQL, "-- name: ListPortfolioItems") && p.armed.CompareAndSwap(true, false) {
		close(p.entered)
		select {
		case <-p.release:
		case <-c.Done():
		}
	}
	return c
}
func (*pausePortfolioPage) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {}
func TestPersonalPageExpiryCleanupFence(t *testing.T) {
	f := newPersonalFixture(t)
	seedIssues(t, f, 1)
	r := repo.New(f.pool, db.New(f.pool))
	auth := func(context.Context, []uuid.UUID, bool) error { return nil }
	page, e := r.Capture(f.ctx, f.w, f.actor, pw.Capture{Input: pw.PortfolioCreateInput{RequestID: uuid.New(), WorkspaceIDs: []uuid.UUID{f.w}, Definition: query(), Limit: 100}, EffectiveTTL: 1}, auth)
	if e != nil {
		t.Fatal(e)
	}
	tracer := &pausePortfolioPage{entered: make(chan struct{}), release: make(chan struct{})}
	tracer.armed.Store(true)
	cfg := f.pool.Config()
	cfg.ConnConfig.Tracer = tracer
	pool, e := pgxpool.NewWithConfig(f.ctx, cfg)
	if e != nil {
		t.Fatal(e)
	}
	defer pool.Close()
	done := make(chan error, 1)
	go func() {
		_, e := repo.New(pool, db.New(pool)).Page(f.ctx, f.w, f.actor, pw.PortfolioGetInput{SessionID: page.Session.ID, Limit: 100}, auth)
		done <- e
	}()
	select {
	case <-tracer.entered:
	case <-time.After(time.Second):
		t.Fatal("header not acquired")
	}
	delay := time.Until(page.Session.ExpiresAt) + 20*time.Millisecond
	if delay > 0 {
		time.Sleep(delay)
	}
	n, e := r.Cleanup(f.ctx, 1000)
	if e != nil || n != 0 {
		t.Fatal("cleanup crossed active page", n, e)
	}
	close(tracer.release)
	select {
	case e = <-done:
		checkCode(t, e, operations.Conflict)
	case <-time.After(3 * time.Second):
		t.Fatal("page stuck")
	}
	n, e = r.Cleanup(f.ctx, 1000)
	if e != nil || n != 1 {
		t.Fatal(n, e)
	}
}
